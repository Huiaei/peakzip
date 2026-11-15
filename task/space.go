package task

import (
	"fmt"
	"os"
	"path/filepath"

	"peakzip/manifest"
)

// keepFreeBytes is the safety margin reserved on the target filesystem so
// that extraction never fills the disk completely.
const keepFreeBytes int64 = 2 * 1024 * 1024 * 1024 // 2 GiB

// estimatePeakSpace computes the worst-case peak disk usage for a
// decompression task.
//
// The conservative model is:
//   - all volume files must be present on disk before extraction starts
//     (volumeTotal),
//   - at most one volume is being extracted at a time, and its extracted
//     output must coexist with the volume files (maxSingleVolumeOutput),
//   - plus a fixed safety margin (keepFreeBytes).
//
// If DeleteWhenDone is set, the volume files are removed after extraction,
// but the conservative model still accounts for them all up front because
// they are needed until their volume is processed.
func estimatePeakSpace(m *manifest.Manifest, deleteWhenDone bool) int64 {
	var volumeTotal int64
	var maxSingleVolumeOutput int64

	for _, v := range m.Volumes {
		if v.Type != "data" {
			continue
		}
		volumeTotal += v.Size

		// Estimate the extracted output of the largest single volume.
		// Files in this volume are either independent files fully contained
		// in the volume, or a fragment of a cross-volume file.
		var volOutput int64
		for _, f := range m.Files {
			if f.IsCrossVolume {
				if v.LogicalNumber >= f.VolumeStart && v.LogicalNumber <= f.VolumeEnd {
					// The whole cross-volume file is extracted by 7z, but
					// only the fragment in this volume is "new" output.
					// Use the fragment length as the output estimate.
					for _, frag := range f.Fragments {
						if frag.Volume == v.LogicalNumber {
							volOutput += frag.Length
						}
					}
				}
			} else if f.Volume == v.LogicalNumber {
				volOutput += f.Size
			}
		}
		if volOutput > maxSingleVolumeOutput {
			maxSingleVolumeOutput = volOutput
		}
	}

	// Peak = all volumes + largest single-volume output + safety margin.
	// This is an upper bound; actual peak is lower because volumes are
	// extracted and deleted sequentially.
	return volumeTotal + maxSingleVolumeOutput + keepFreeBytes
}

// checkSpaceBeforeExtract verifies that the target filesystem has enough
// free space for the worst-case extraction, failing fast before any file
// is modified.
func checkSpaceBeforeExtract(cfg DecompressTask, m *manifest.Manifest) error {
	needed := estimatePeakSpace(m, cfg.DeleteWhenDone)
	target, err := nearestExistingDir(cfg.ExtractDir)
	if err != nil {
		return fmt.Errorf("检查磁盘空间失败：%w", err)
	}
	avail, err := availableSpace(target)
	if err != nil {
		return fmt.Errorf("检查磁盘空间失败：%w", err)
	}
	if avail < needed {
		return fmt.Errorf("磁盘空间不足：解压预计需要约 %s，可用 %s。请释放空间或改用更大分区",
			humanSize(needed), humanSize(avail))
	}
	return nil
}

// nearestExistingDir returns dir itself when it already exists as a
// directory, otherwise the closest existing ancestor directory. The
// pre-flight space check runs before the extraction directory is created, and
// statfs on a missing path fails with ENOENT, so resolving the ancestor
// reports the space of the filesystem that will actually host the output
// instead of turning a normal "not created yet" into a hard error.
func nearestExistingDir(dir string) (string, error) {
	p := filepath.Clean(dir)
	for {
		fi, err := os.Stat(p)
		if err == nil && fi.IsDir() {
			return p, nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", fmt.Errorf("无法确定 %s 所在的文件系统：没有已存在的上级目录", dir)
		}
		p = parent
	}
}

// humanSize formats a byte count as a human-readable string.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
