package task

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"peakzip/archiver"
	"peakzip/hasher"
	"peakzip/logger"
	"peakzip/manifest"
	"peakzip/state"
)

// makeAbsolute replaces each path with its absolute form.
func makeAbsolute(paths ...*string) error {
	for _, p := range paths {
		abs, err := filepath.Abs(*p)
		if err != nil {
			return fmt.Errorf("解析路径 %q 失败：%w", *p, err)
		}
		*p = abs
	}
	return nil
}

// RunDecompress executes the full decompression workflow:
//  1. Load manifest
//  2. Initialize state (check for resume)
//  3. Periodically scan download directory
//  4. Process ready volumes (extract, verify, move)
//  5. Delete volumes if requested
//  6. Clean up
func RunDecompress(ctx context.Context, cfg DecompressTask, progress chan<- ProgressUpdate, log *logger.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := makeAbsolute(&cfg.ManifestPath, &cfg.DownloadDir, &cfg.ExtractDir); err != nil {
		return err
	}
	rc := cfg.ResourceConfig.Defaults()

	arch := &archiver.Archiver{}
	defer arch.Close()

	// Step 1: Load manifest
	sendProgress(progress, ProgressUpdate{
		Phase:   "load",
		Message: "正在加载清单...",
	})

	var manifestJSONPath string

	// If ManifestPath ends with .7z, it is a manifest volume - extract it first.
	if filepath.Ext(cfg.ManifestPath) == ".7z" {
		tmpExtract, err := os.MkdirTemp("", "peakzip-manifest-*")
		if err != nil {
			return fmt.Errorf("创建临时目录失败：%w", err)
		}
		defer os.RemoveAll(tmpExtract)

		if err := arch.Extract(ctx, cfg.ManifestPath, tmpExtract, cfg.Password); err != nil {
			return fmt.Errorf("解压清单卷失败：%w", err)
		}

		// Find the manifest JSON in the extracted directory.
		entries, err := os.ReadDir(tmpExtract)
		if err != nil {
			return fmt.Errorf("读取解压后的清单目录失败：%w", err)
		}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) == ".json" {
				manifestJSONPath = filepath.Join(tmpExtract, entry.Name())
				break
			}
		}
		if manifestJSONPath == "" {
			return fmt.Errorf("清单卷中未找到 manifest JSON 文件")
		}
	} else {
		manifestJSONPath = cfg.ManifestPath
	}

	m, err := manifest.Load(manifestJSONPath)
	if err != nil {
		return fmt.Errorf("加载清单失败：%w", err)
	}
	log.Info(fmt.Sprintf("清单已加载：项目=%s，数据卷=%d，文件=%d", m.ProjectName, m.DataVolumeCount(), len(m.Files)))

	// Step 2: Initialize state
	statePath := state.StateFilePath(cfg.ExtractDir)
	taskID := hasher.BytesHash([]byte(m.ProjectName + fmt.Sprintf("%d", m.TotalVolumes)))

	var ts *state.TaskState
	loadedState, err := state.LoadState(statePath)
	if err == nil && loadedState.TaskID == taskID {
		ts = loadedState
		log.Info(fmt.Sprintf("已恢复进度：%d 个卷已完成", ts.VolumeDoneCount()))
	} else {
		ts = state.NewState(taskID)
		log.Info("开始新的解压任务")
	}

	// Initialize cross-volume progress for all cross-volume files.
	for i := range m.Files {
		if !m.Files[i].IsCrossVolume {
			continue
		}
		needed := make([]string, 0, len(m.Files[i].Fragments))
		for _, frag := range m.Files[i].Fragments {
			needed = append(needed, fmt.Sprintf("vol_%d", frag.Volume))
		}
		ts.SetCrossVolumeNeeded(m.Files[i].Path, needed)
	}

	// Ensure output directory exists.
	if err := os.MkdirAll(cfg.ExtractDir, 0o755); err != nil {
		return fmt.Errorf("创建解压目录失败：%w", err)
	}

	// Temp directory for extraction.
	tmpDir := filepath.Join(cfg.ExtractDir, ".tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("创建临时目录失败：%w", err)
	}

	// Resolve every data volume path once.
	totalVolumes := m.DataVolumeCount()
	volPaths := make(map[int64]string, totalVolumes)
	for i := range m.Volumes {
		v := &m.Volumes[i]
		if v.Type == "manifest" {
			continue
		}
		volPaths[v.LogicalNumber] = volumeFilePath(m, v, cfg.DownloadDir, totalVolumes)
	}

	// Step 3: Setup volume scanner and extractor
	sendProgress(progress, ProgressUpdate{
		Phase:        "scan",
		Message:      "正在扫描下载目录...",
		TotalVolumes: totalVolumes,
	})

	// Track volume statuses.
	volStatus := make(map[int64]VolumeStatus, totalVolumes)
	var statusMu sync.Mutex

	// Seed the in-memory statuses from the resumed state.
	for num, path := range volPaths {
		if !ts.IsVolumeDone(path) {
			continue
		}
		statusMu.Lock()
		volStatus[num] = VolumeStatusDone
		statusMu.Unlock()
	}

	// Track cross-volume readiness and scheduling.
	cvReady := make(map[string]bool)
	cvScheduled := make(map[string]bool)
	var cvMu sync.Mutex

	// Extraction worker pool.
	workerCount := rc.MaxWorkers
	extractCh := make(chan *extractJob, workerCount*2)

	// Delete queue worker.
	deleteCh := make(chan string, 100)

	go deleteWorker(ctx, deleteCh, *rc, log)

	// Start extraction workers.
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			extractWorker(ctx, workerID, arch, cfg, ts, deleteCh, extractCh, volStatus, &statusMu, log)
		}(i)
	}

	// Start volume scanner ticker.
	scanInterval := time.Duration(rc.ScanIntervalMS) * time.Millisecond
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	stableWindow := time.Duration(rc.StableWindowSec) * time.Second
	lastCheck := make(map[string]time.Time)

	// Initial scan.
	scanAndScheduleVolumes(ctx, m, cfg, ts, volPaths, volStatus, &statusMu, cvReady, cvScheduled, &cvMu, lastCheck, stableWindow, extractCh, log)

	reportProgress := func() bool {
		statusMu.Lock()
		done, allDone := countProgress(volPaths, volStatus)
		statusMu.Unlock()

		overallProgress := 0.0
		if totalVolumes > 0 {
			overallProgress = float64(done) / float64(totalVolumes)
			if overallProgress > 1.0 {
				overallProgress = 1.0
			}
		}
		sendProgress(progress, ProgressUpdate{
			Phase:         "scan",
			Message:       fmt.Sprintf("等待卷文件... (%d/%d 已完成)", done, totalVolumes),
			Progress:      overallProgress,
			CurrentVolume: done,
			TotalVolumes:  totalVolumes,
		})
		return allDone
	}

	completeTask := func() {
		sendProgress(progress, ProgressUpdate{Phase: "cleanup", Message: "正在清理...", Progress: 1.0})
		if err := state.SaveState(ts, statePath); err != nil {
			log.Error(fmt.Sprintf("保存最终状态失败：%v", err))
		}
		os.RemoveAll(tmpDir)
		os.Remove(statePath)
		sendProgress(progress, ProgressUpdate{Phase: "done", Message: "解压完成！", Progress: 1.0})
		log.Info("解压任务完成。")
		close(extractCh)
		close(deleteCh)
		wg.Wait()
	}

	// Main loop.
	for {
		if reportProgress() {
			completeTask()
			return nil
		}
		select {
		case <-ctx.Done():
			log.Info("解压已由用户取消")
			if err := state.SaveState(ts, statePath); err != nil {
				log.Error(fmt.Sprintf("保存状态失败：%v", err))
			}
			os.RemoveAll(tmpDir)
			close(extractCh)
			close(deleteCh)
			wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			scanAndScheduleVolumes(ctx, m, cfg, ts, volPaths, volStatus, &statusMu, cvReady, cvScheduled, &cvMu, lastCheck, stableWindow, extractCh, log)
		}
	}
}

// scanAndScheduleVolumes scans the download directory for volume files and
// schedules any that are ready for extraction. It returns the number of
// newly scheduled extraction jobs.
func scanAndScheduleVolumes(ctx context.Context, m *manifest.Manifest, cfg DecompressTask, ts *state.TaskState,
	volPaths map[int64]string,
	volStatus map[int64]VolumeStatus, statusMu *sync.Mutex,
	cvReady, cvScheduled map[string]bool, cvMu *sync.Mutex,
	lastCheck map[string]time.Time, stableWindow time.Duration,
	extractCh chan<- *extractJob, log *logger.Logger) int {

	scheduled := 0

	// Map each volume number to the cross-volume file that owns it, if any.
	cvOwners := make(map[int64]*manifest.FileEntry, len(m.Files))
	for i := range m.Files {
		f := &m.Files[i]
		if !f.IsCrossVolume {
			continue
		}
		for n := f.VolumeStart; n <= f.VolumeEnd; n++ {
			cvOwners[n] = f
		}
	}

	for _, v := range m.Volumes {
		if v.Type == "manifest" {
			continue
		}

		statusMu.Lock()
		currentStatus := volStatus[v.LogicalNumber]
		statusMu.Unlock()

		// VolumeStatusReady is set immediately before scheduling, so re-seeing
		// Ready, Extracting or Done means this volume is already handled.
		if currentStatus == VolumeStatusReady || currentStatus == VolumeStatusExtracting || currentStatus == VolumeStatusDone {
			continue
		}

		volPath, ok := volPaths[v.LogicalNumber]
		if !ok {
			continue
		}

		// Resume: volumes already recorded as done by a previous run.
		if ts.IsVolumeDone(volPath) {
			statusMu.Lock()
			volStatus[v.LogicalNumber] = VolumeStatusDone
			statusMu.Unlock()
			log.Info(fmt.Sprintf("卷 %d 已完成（断点恢复），跳过", v.LogicalNumber))
			continue
		}

		// Check if volume file exists.
		info, err := os.Stat(volPath)
		if err != nil {
			log.Debug(fmt.Sprintf("卷 %d 尚未下载：%s", v.LogicalNumber, volPath))
			continue
		}

		// Wait until the file has stopped changing, so that a partially
		// downloaded volume is not hashed and extracted.
		lastTime, hasLast := lastCheck[volPath]
		if !hasLast {
			lastCheck[volPath] = info.ModTime()
			continue
		}
		if info.ModTime().After(lastTime) {
			lastCheck[volPath] = info.ModTime()
			continue
		}
		if stableWindow > 0 && time.Since(info.ModTime()) < stableWindow {
			continue
		}

		// File is stable - compute hash.
		log.Debug(fmt.Sprintf("正在计算卷 %d 哈希：%s", v.LogicalNumber, volPath))
		fileHash, err := hasher.FileHash(volPath)
		if err != nil {
			log.Error(fmt.Sprintf("卷 %d 哈希计算失败：%v", v.LogicalNumber, err))
			continue
		}

		// Verify against manifest.
		if v.Hash != "" && v.Hash != fileHash {
			log.Error(fmt.Sprintf("卷 %d 哈希不匹配：期望 %s，实际 %s", v.LogicalNumber, v.Hash, fileHash))
			continue
		}

		// Volume is ready.
		ts.MarkVolumeReady(volPath)
		statusMu.Lock()
		volStatus[v.LogicalNumber] = VolumeStatusReady
		statusMu.Unlock()
		log.Info(fmt.Sprintf("卷 %d 已就绪，可解压", v.LogicalNumber))

		// Is this volume part of a cross-volume file?
		if f, isPart := cvOwners[v.LogicalNumber]; isPart {
			fragKey := fmt.Sprintf("vol_%d", v.LogicalNumber)
			ts.AddToCrossVolumeProgress(f.Path, fragKey)

			allReady := ts.CrossVolumeFragmentsReady(f.Path) >= ts.CrossVolumeFragmentsNeeded(f.Path)

			// Atomically decide whether this worker owns the scheduling.
			cvMu.Lock()
			if allReady {
				cvReady[f.Path] = true
			}
			alreadyScheduled := cvScheduled[f.Path]
			if allReady && !alreadyScheduled {
				cvScheduled[f.Path] = true
			}
			cvMu.Unlock()

			if !allReady || alreadyScheduled {
				continue
			}

			basePath, found := crossVolumeBasePath(m, f, cfg.DownloadDir)
			if !found {
				log.Error(fmt.Sprintf("无法定位跨卷文件 %s 的分片", f.Path))
				continue
			}

			job := &extractJob{
				crossVolumeFile: f,
				archivePath:     basePath,
				volumeNumber:    f.VolumeStart,
				manifest:        m,
			}
			select {
			case extractCh <- job:
				scheduled++
				log.Info(fmt.Sprintf("跨卷文件 %s 的所有分片已就绪，加入解压队列", f.Path))
			case <-ctx.Done():
				return scheduled
			}
			continue
		}

		// Independent volume - schedule directly.
		job := &extractJob{
			crossVolumeFile: nil,
			archivePath:     volPath,
			volumeNumber:    v.LogicalNumber,
			manifest:        m,
		}
		select {
		case extractCh <- job:
			scheduled++
		case <-ctx.Done():
			return scheduled
		}
	}

	return scheduled
}

// countProgress counts finished data volumes and reports whether every data
// volume is finished. Caller must hold statusMu.
func countProgress(volPaths map[int64]string, volStatus map[int64]VolumeStatus) (done int, allDone bool) {
	for num := range volPaths {
		if volStatus[num] == VolumeStatusDone {
			done++
		}
	}
	return done, done == len(volPaths)
}

// volumeFilePath resolves the on-disk path of a single volume.
func volumeFilePath(m *manifest.Manifest, v *manifest.VolumeEntry, downloadDir string, totalVolumes int) string {
	if v.Filename != "" {
		return filepath.Join(downloadDir, v.Filename)
	}
	return filepath.Join(downloadDir, fmt.Sprintf("%s.%03d-%d.7z", m.ProjectName, v.LogicalNumber, totalVolumes))
}

// crossVolumeBasePath resolves the on-disk base path of a split
// cross-volume archive from the manifest, stripping the trailing ".NNN"
// part suffix. The manifest is authoritative here: the volume index in the
// name is the data volume count at compression time, which differs from
// Manifest.TotalVolumes once the manifest volume has been appended.
func crossVolumeBasePath(m *manifest.Manifest, f *manifest.FileEntry, downloadDir string) (string, bool) {
	ve := m.FindVolumeByNumber(f.VolumeStart)
	if ve == nil || ve.Filename == "" {
		return "", false
	}
	idx := strings.LastIndex(ve.Filename, ".")
	if idx <= 0 {
		return "", false
	}
	part := ve.Filename[idx+1:]
	if len(part) != 3 {
		return "", false
	}
	for i := 0; i < len(part); i++ {
		if part[i] < '0' || part[i] > '9' {
			return "", false
		}
	}
	return filepath.Join(downloadDir, ve.Filename[:idx]), true
}

// extractJob represents a single extraction task.
type extractJob struct {
	crossVolumeFile *manifest.FileEntry
	archivePath     string
	volumeNumber    int64
	manifest        *manifest.Manifest
}

// jobVolumeNumbers lists the logical volume numbers covered by a job.
func jobVolumeNumbers(job *extractJob) []int64 {
	if job.crossVolumeFile == nil {
		return []int64{job.volumeNumber}
	}
	cv := job.crossVolumeFile
	nums := make([]int64, 0, cv.VolumeEnd-cv.VolumeStart+1)
	for n := cv.VolumeStart; n <= cv.VolumeEnd; n++ {
		nums = append(nums, n)
	}
	return nums
}

// jobVolumePaths lists the on-disk paths of the volumes covered by a job.
func jobVolumePaths(m *manifest.Manifest, nums []int64, downloadDir string) []string {
	paths := make([]string, 0, len(nums))
	for _, n := range nums {
		ve := m.FindVolumeByNumber(n)
		if ve == nil || ve.Filename == "" {
			continue
		}
		paths = append(paths, filepath.Join(downloadDir, ve.Filename))
	}
	return paths
}

// extractWorker processes extraction jobs from the channel.
func extractWorker(ctx context.Context, workerID int, arch *archiver.Archiver, cfg DecompressTask, ts *state.TaskState, deleteCh chan<- string,
	jobs <-chan *extractJob, volStatus map[int64]VolumeStatus, statusMu *sync.Mutex, log *logger.Logger) {

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}
			if err := processExtractJob(ctx, workerID, arch, cfg, ts, deleteCh, job, volStatus, statusMu, log); err != nil {
				log.Error(fmt.Sprintf("工作线程 %d：解压失败：%v", workerID, err))
			}
		}
	}
}

// processExtractJob handles a single extraction job.
func processExtractJob(ctx context.Context, workerID int, arch *archiver.Archiver,
	cfg DecompressTask, ts *state.TaskState, deleteCh chan<- string, job *extractJob,
	volStatus map[int64]VolumeStatus, statusMu *sync.Mutex, log *logger.Logger) error {

	m := job.manifest
	volNum := job.volumeNumber
	tmpDir := filepath.Join(cfg.ExtractDir, ".tmp", fmt.Sprintf("vol_%03d", volNum))

	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("为卷 %d 创建临时目录失败：%w", volNum, err)
	}
	defer os.RemoveAll(tmpDir)

	// Mark as extracting.
	statusMu.Lock()
	volStatus[volNum] = VolumeStatusExtracting
	statusMu.Unlock()

	log.Info(fmt.Sprintf("工作线程 %d：正在解压卷 %d", workerID, volNum))

	// Extract the archive. For split cross-volume archives, 7z needs
	// the first part (base.001) specified explicitly; the base path alone is
	// insufficient because it does not exist as a file on disk.
	extractPath := job.archivePath
	if job.crossVolumeFile != nil {
		extractPath = extractPath + ".001"
	}
	if err := arch.Extract(ctx, extractPath, tmpDir, cfg.Password); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		statusMu.Lock()
		volStatus[volNum] = VolumeStatusError
		statusMu.Unlock()
		return fmt.Errorf("解压存档 %s 失败：%w", job.archivePath, err)
	}

	// Locate the extracted files.
	var filesToMove []string
	if job.crossVolumeFile != nil {
		// Cross-volume file: the extracted file should be in tmpDir.
		cvRel := job.crossVolumeFile.Path
		extractedPath := filepath.Join(tmpDir, cvRel)
		if _, err := os.Stat(extractedPath); err != nil {
			// Try to find it.
			found := false
			filepath.Walk(tmpDir, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil || !info.Mode().IsRegular() {
					return nil
				}
				rel, relErr := filepath.Rel(tmpDir, path)
				if relErr != nil {
					return nil
				}
				if filepath.ToSlash(rel) == cvRel {
					extractedPath = path
					found = true
					return filepath.SkipAll
				}
				return nil
			})
			if !found {
				return fmt.Errorf("跨卷文件 %s 未找到解压后的文件", cvRel)
			}
		}
		filesToMove = append(filesToMove, extractedPath)
	} else {
		// Independent volume: find all extracted files.
		filepath.Walk(tmpDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if info.Mode().IsRegular() {
				filesToMove = append(filesToMove, path)
			}
			return nil
		})
	}

	// Verify and move each file.
	moved := 0
	for _, fPath := range filesToMove {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, err := filepath.Rel(tmpDir, fPath)
		if err != nil {
			log.Warn(fmt.Sprintf("无法计算相对路径 %s：%v", fPath, err))
			continue
		}
		relPath = filepath.ToSlash(relPath)

		// Find the file entry in the manifest.
		var expectedHash string
		for fi := range m.Files {
			if m.Files[fi].Path == relPath {
				expectedHash = m.Files[fi].Hash
				break
			}
		}
		if expectedHash == "" {
			log.Warn(fmt.Sprintf("文件 %s 不在清单中，仍继续移动", relPath))
		}

		// Verify hash.
		if expectedHash != "" {
			fileHash, err := hasher.FileHash(fPath)
			if err != nil {
				log.Warn(fmt.Sprintf("文件 %s 哈希计算失败：%v", relPath, err))
				continue
			}
			if fileHash != expectedHash {
				log.Error(fmt.Sprintf("文件 %s 哈希不匹配：期望 %s，实际 %s", relPath, expectedHash, fileHash))
				continue
			}
		}

		// Move to final directory.
		destPath := filepath.Join(cfg.ExtractDir, relPath)
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			log.Error(fmt.Sprintf("创建目录 %s 失败：%v", destDir, err))
			continue
		}

		if err := os.Rename(fPath, destPath); err != nil {
			// Rename might fail across filesystems; use copy+delete.
			if err2 := copyFile(fPath, destPath); err2 != nil {
				log.Error(fmt.Sprintf("移动文件 %s 至 %s 失败：%v", fPath, destPath, err2))
				continue
			}
		}

		// Mark file as done.
		ts.MarkFileDone(relPath)
		moved++
		log.Info(fmt.Sprintf("文件 %s 已解压并校验通过", relPath))
	}

	if moved == 0 && len(filesToMove) > 0 {
		statusMu.Lock()
		volStatus[volNum] = VolumeStatusError
		statusMu.Unlock()
		return fmt.Errorf("卷 %d 没有成功移动任何文件", volNum)
	}

	// Mark every volume this job covered as done, in the persistent state
	// and in the in-memory status table.
	nums := jobVolumeNumbers(job)
	paths := jobVolumePaths(m, nums, cfg.DownloadDir)
	for _, p := range paths {
		ts.MarkVolumeDone(p)
	}
	statusMu.Lock()
	for _, n := range nums {
		volStatus[n] = VolumeStatusDone
	}
	statusMu.Unlock()

	// Add to delete queue if requested.
	if cfg.DeleteWhenDone {
		for _, p := range paths {
			ts.AddToDeleteQueue(p)
			select {
			case deleteCh <- p:
			default:
			}
		}
	}

	// Save state periodically.
	if ts.VolumeDoneCount()%5 == 0 {
		statePath := state.StateFilePath(cfg.ExtractDir)
		if err := state.SaveState(ts, statePath); err != nil {
			log.Warn(fmt.Sprintf("保存状态失败：%v", err))
		}
	}

	return nil
}

// deleteWorker processes the delete queue.
func deleteWorker(ctx context.Context, deleteCh <-chan string, rc ResourceConfig, log *logger.Logger) {
	deleteInterval := time.Duration(rc.DeleteIntervalMS) * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		case path, ok := <-deleteCh:
			if !ok {
				return
			}
			if !removeWithRetry(ctx, path, 3, log) {
				continue
			}
			log.Info(fmt.Sprintf("已删除卷文件：%s", path))
			if deleteInterval > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(deleteInterval):
				}
			}
		}
	}
}

// removeWithRetry tries to delete a path, backing off between attempts.
func removeWithRetry(ctx context.Context, path string, attempts int, log *logger.Logger) bool {
	for attempt := 1; attempt <= attempts; attempt++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		err := os.Remove(path)
		if err == nil || os.IsNotExist(err) {
			return true
		}
		log.Warn(fmt.Sprintf("删除 %s 失败（第 %d 次）：%v", path, attempt, err))
		if attempt == attempts {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return false
}

// copyFile streams src to dst and then removes src. Streaming keeps memory
// use flat regardless of file size, unlike os.ReadFile/os.WriteFile.
func copyFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()

	dstF, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dstF, srcF); err != nil {
		dstF.Close()
		return err
	}
	// Sync before removing the source so the copy is durable.
	if err := dstF.Sync(); err != nil {
		dstF.Close()
		return err
	}
	if err := dstF.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
