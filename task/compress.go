package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"peakzip/archiver"
	"peakzip/hasher"
	"peakzip/logger"
	"peakzip/manifest"
	"peakzip/scanner"
)

// RunCompress executes the full compression workflow:
//  1. Scan source directory
//  2. Generate manifest (plan volumes)
//  3. Compress each volume using 7z
//  4. Calculate hashes and update manifest
//  5. Create manifest volume
//  6. Clean up temp files
func RunCompress(ctx context.Context, cfg CompressTask, progress chan<- ProgressUpdate, log *logger.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.VolumeSize <= 0 {
		return fmt.Errorf("分卷大小必须为正数")
	}
	if cfg.ProjectName == "" {
		return fmt.Errorf("项目名称不能为空")
	}

	// Normalise to absolute paths: volume files are written from within
	// SourceDir (see compressVolume), so a relative OutputDir would be
	// resolved against the wrong working directory.
	if err := makeAbsolute(&cfg.SourceDir, &cfg.OutputDir); err != nil {
		return err
	}

	arch := &archiver.Archiver{}
	defer arch.Close()

	// Ensure output directory exists.
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return fmt.Errorf("创建输出目录失败：%w", err)
	}

	// Step 1: Scan
	sendProgress(progress, ProgressUpdate{
		Phase:   "scan",
		Message: "正在扫描源目录...",
	})
	log.Info(fmt.Sprintf("正在扫描源目录：%s", cfg.SourceDir))

	files, warnings, err := scanner.ScanDirectory(cfg.SourceDir)
	if err != nil {
		return fmt.Errorf("扫描失败：%w", err)
	}
	if len(warnings) > 0 {
		log.Warn(fmt.Sprintf("扫描过程产生 %d 个警告", len(warnings)))
		for _, warning := range warnings {
			log.Warn(warning)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("源目录中没有可压缩的文件")
	}
	log.Info(fmt.Sprintf("共发现 %d 个文件", len(files)))

	// Step 2: Generate manifest
	sendProgress(progress, ProgressUpdate{
		Phase:   "plan",
		Message: "正在规划分卷分配...",
	})

	enc := &manifest.EncryptionConfig{}
	if cfg.Password != "" {
		enc.Enabled = true
		enc.Method = manifest.EncryptionMethod
	}

	m, err := manifest.Generate(cfg.ProjectName, files, cfg.VolumeSize, enc)
	if err != nil {
		return fmt.Errorf("清单生成失败：%w", err)
	}
	log.Info(fmt.Sprintf("清单已生成：%d 个卷，%d 个文件", m.TotalVolumes, len(m.Files)))

	// Identify cross-volume files by their starting volume.
	cvStartVols := make(map[int64]*manifest.FileEntry)
	for i := range m.Files {
		if m.Files[i].IsCrossVolume {
			cvStartVols[m.Files[i].VolumeStart] = &m.Files[i]
		}
	}

	// Step 3: Compress each volume
	sendProgress(progress, ProgressUpdate{
		Phase:        "compress",
		Message:      fmt.Sprintf("正在压缩卷文件（共 %d 个）...", m.TotalVolumes),
		TotalVolumes: m.TotalVolumes,
	})

	tmpDir, err := os.MkdirTemp("", "peakzip-compress-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败：%w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Track cross-volume files that have already been compressed.
	cvDone := make(map[string]bool)

	for vi := 0; vi < m.TotalVolumes; vi++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		sendProgress(progress, ProgressUpdate{
			Phase:         "compress",
			Message:       fmt.Sprintf("正在压缩卷 %d/%d", vi+1, m.TotalVolumes),
			Progress:      float64(vi) / float64(m.TotalVolumes),
			CurrentVolume: vi + 1,
			TotalVolumes:  m.TotalVolumes,
		})

		if err := compressVolume(ctx, arch, cfg, m, vi, tmpDir, log, cvStartVols, cvDone); err != nil {
			log.Error(fmt.Sprintf("压缩卷 %d 失败：%v", vi, err))
			return fmt.Errorf("压缩卷 %d 失败：%w", vi, err)
		}
	}

	// Step 4: Calculate hashes for all volumes
	sendProgress(progress, ProgressUpdate{
		Phase:   "hash",
		Message: "正在计算卷哈希...",
	})

	for vi := range m.Volumes {
		if err := ctx.Err(); err != nil {
			return err
		}

		volPath := filepath.Join(cfg.OutputDir, m.Volumes[vi].Filename)
		info, err := os.Stat(volPath)
		if err != nil {
			// For cross-volume fragments, the filename includes a part suffix.
			// Try common suffix patterns.
			for _, suffix := range []string{".001", ".002", ".003", ".004", ".005"} {
				vp := volPath + suffix
				if info2, err2 := os.Stat(vp); err2 == nil {
					info = info2
					m.Volumes[vi].Filename = filepath.Base(vp)
					volPath = vp
					break
				}
			}
			if err != nil {
				log.Warn(fmt.Sprintf("未找到卷文件：%s", volPath))
				continue
			}
		}
		m.Volumes[vi].Size = info.Size()

		hash, err := hasher.FileHash(volPath)
		if err != nil {
			log.Warn(fmt.Sprintf("卷 %d 哈希计算失败：%v", vi, err))
			continue
		}
		m.Volumes[vi].Hash = hash
	}

	// Step 5: Calculate hashes for files
	sendProgress(progress, ProgressUpdate{
		Phase:   "hash",
		Message: "正在计算文件哈希...",
	})

	for fi := range m.Files {
		if err := ctx.Err(); err != nil {
			return err
		}

		srcPath := filepath.Join(cfg.SourceDir, m.Files[fi].Path)
		h, err := hasher.FileHash(srcPath)
		if err != nil {
			log.Warn(fmt.Sprintf("文件 %s 哈希计算失败：%v", m.Files[fi].Path, err))
			continue
		}
		m.Files[fi].Hash = h
	}

	// Step 6: Save manifest JSON
	manifestJSONPath := filepath.Join(cfg.OutputDir, cfg.ProjectName+".manifest.json")
	if err := manifest.Save(m, manifestJSONPath); err != nil {
		return fmt.Errorf("保存清单失败：%w", err)
	}
	log.Info(fmt.Sprintf("清单已保存至：%s", manifestJSONPath))

	// Step 7: Create manifest volume.
	// The index in the name is the data volume count, so it cannot collide
	// with data volume 0 (named "%s.000-%d.7z").
	sendProgress(progress, ProgressUpdate{
		Phase:   "manifest",
		Message: "正在创建清单卷...",
	})

	manifestVolName := fmt.Sprintf("%s.%03d-%d.7z", cfg.ProjectName, m.TotalVolumes, m.TotalVolumes)
	m.Volumes = append(m.Volumes, manifest.VolumeEntry{
		LogicalNumber: int64(m.TotalVolumes),
		Filename:      manifestVolName,
		Type:          "manifest",
	})
	m.TotalVolumes++

	manifestVolPath := filepath.Join(cfg.OutputDir, manifestVolName)
	encArgs := archiver.EncryptionArgs("")
	if cfg.EncryptManifest && cfg.Password != "" {
		encArgs = archiver.EncryptionArgs(cfg.Password)
	}
	args := []string{"a", "-t7z"}
	args = append(args, encArgs...)
	// Store the manifest under its bare name so it is easy to find after
	// extracting the manifest volume.
	args = append(args, manifestVolPath, filepath.Base(manifestJSONPath))
	if err := arch.RunInDir(ctx, cfg.OutputDir, args...); err != nil {
		return fmt.Errorf("创建清单卷失败：%w", err)
	}
	log.Info(fmt.Sprintf("清单卷已创建：%s", manifestVolPath))

	// Re-save manifest with the manifest volume entry.
	if err := manifest.Save(m, manifestJSONPath); err != nil {
		return fmt.Errorf("保存最终清单失败：%w", err)
	}

	sendProgress(progress, ProgressUpdate{
		Phase:    "done",
		Message:  "压缩完成！",
		Progress: 1.0,
	})
	log.Info("压缩任务完成。")
	return nil
}

// compressVolume compresses a single logical volume.
func compressVolume(ctx context.Context, arch *archiver.Archiver, cfg CompressTask, m *manifest.Manifest, volIdx int, tmpDir string, log *logger.Logger, cvStartVols map[int64]*manifest.FileEntry, cvDone map[string]bool) error {
	vol := &m.Volumes[volIdx]

	// Check if this volume starts a cross-volume file.
	if cvFile, ok := cvStartVols[int64(volIdx)]; ok {
		// Compress the entire cross-volume file using 7z split mode. The
		// source is passed relative to SourceDir and 7z is run from there,
		// so the archive records the file path instead of a bare basename.
		baseName := fmt.Sprintf("%s.%03d-%d.7z", cfg.ProjectName, cvFile.VolumeStart, m.TotalVolumes)
		basePath := filepath.Join(cfg.OutputDir, baseName)

		encArgs := archiver.EncryptionArgs(cfg.Password)
		args := []string{"a", "-t7z"}
		args = append(args, encArgs...)
		args = append(args, fmt.Sprintf("-v%d", cfg.VolumeSize), basePath, cvFile.Path)

		if err := arch.RunInDir(ctx, cfg.SourceDir, args...); err != nil {
			return fmt.Errorf("压缩跨卷文件 %s 失败：%w", cvFile.Path, err)
		}

		// 7z produces: basePath.001, basePath.002, ...
		// Record each resulting part name on the volume it occupies so that
		// decompression locates parts via the manifest instead of guessing.
		partCount := 0
		for pi := 0; pi < int(cvFile.VolumeEnd-cvFile.VolumeStart)+1; pi++ {
			partSuffix := fmt.Sprintf(".%03d", pi+1)
			partPath := filepath.Join(cfg.OutputDir, baseName+partSuffix)
			if _, err := os.Stat(partPath); err != nil {
				continue
			}
			partCount++
			if vi, ok := volumeIndex(m, cvFile.VolumeStart+int64(pi)); ok {
				m.Volumes[vi].Filename = baseName + partSuffix
			}
		}
		if partCount == 0 {
			return fmt.Errorf("跨卷文件 %s 压缩后未产生任何分片", cvFile.Path)
		}

		// Mark this cross-volume file as done.
		cvDone[cvFile.Path] = true
		log.Info(fmt.Sprintf("跨卷文件 %s 已压缩为 %d 个分片", cvFile.Path, partCount))
		return nil
	}

	// Check if this volume is a continuation of a cross-volume file (already done).
	for _, cvFile := range cvStartVols {
		if volIdx >= int(cvFile.VolumeStart) && volIdx <= int(cvFile.VolumeEnd) && cvDone[cvFile.Path] {
			// Already compressed as part of the cross-volume compression.
			if m.Volumes[volIdx].Filename == "" {
				baseName := fmt.Sprintf("%s.%03d-%d.7z", cfg.ProjectName, cvFile.VolumeStart, m.TotalVolumes)
				partNum := volIdx - int(cvFile.VolumeStart) + 1
				m.Volumes[volIdx].Filename = fmt.Sprintf("%s.%03d", baseName, partNum)
			}
			return nil
		}
	}

	// Independent volume: gather the files assigned to this volume.
	// Paths stay relative to cfg.SourceDir so that 7z records them verbatim;
	// an absolute path would be stored as a bare basename and the directory
	// layout would be lost on extraction.
	var relPaths []string
	for _, f := range m.Files {
		if f.IsCrossVolume {
			continue
		}
		if f.Volume == int64(volIdx) {
			relPaths = append(relPaths, f.Path)
		}
	}

	if len(relPaths) == 0 {
		log.Debug(fmt.Sprintf("卷 %d 为空，跳过", volIdx))
		return nil
	}

	// Sort file paths for deterministic ordering.
	sort.Strings(relPaths)

	// Build the volume filename.
	volFilename := fmt.Sprintf("%s.%03d-%d.7z", cfg.ProjectName, volIdx, m.TotalVolumes)
	vol.Filename = volFilename
	volPath := filepath.Join(cfg.OutputDir, volFilename)

	encArgs := archiver.EncryptionArgs(cfg.Password)

	// Use a response file for large file lists.
	if len(relPaths) > 200 {
		listFile := filepath.Join(tmpDir, fmt.Sprintf("vlist_%03d.txt", volIdx))
		content := strings.Join(relPaths, "\n")
		if err := os.WriteFile(listFile, []byte(content), 0o644); err != nil {
			return fmt.Errorf("写入文件列表失败：%w", err)
		}
		defer os.Remove(listFile)

		args := []string{"a", "-t7z"}
		args = append(args, encArgs...)
		args = append(args, volPath, "@"+listFile)
		return arch.RunInDir(ctx, cfg.SourceDir, args...)
	}

	args := []string{"a", "-t7z"}
	args = append(args, encArgs...)
	args = append(args, volPath)
	args = append(args, relPaths...)
	return arch.RunInDir(ctx, cfg.SourceDir, args...)
}

// volumeIndex finds the index of a volume by its logical number.
func volumeIndex(m *manifest.Manifest, num int64) (int, bool) {
	for i := range m.Volumes {
		if m.Volumes[i].LogicalNumber == num {
			return i, true
		}
	}
	return -1, false
}

// sendProgress non-blockingly sends a progress update to the channel.
func sendProgress(ch chan<- ProgressUpdate, update ProgressUpdate) {
	select {
	case ch <- update:
	default:
	}
}
