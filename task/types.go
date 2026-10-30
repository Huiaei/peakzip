// Package task implements the core compression and decompression task
// scheduling logic, coordinating scanner, manifest, archiver, hasher,
// state, and logger packages.
package task

// ProgressUpdate describes the current task state for UI display.
type ProgressUpdate struct {
	// Phase is a short identifier: "scan", "plan", "compress", "extract",
	// "hash", "delete", "cleanup", "done".
	Phase string
	// Message is a human-readable description of the current operation.
	Message string
	// Progress is the overall progress as a fraction (0.0 to 1.0).
	Progress float64
	// CurrentVolume is the index of the volume currently being processed (1-based).
	CurrentVolume int
	// TotalVolumes is the total number of volumes in the task.
	TotalVolumes int
}

// ResourceConfig controls concurrency and resource usage during extraction.
type ResourceConfig struct {
	// MaxWorkers is the maximum number of concurrent extraction workers.
	// 0 means default (4).
	MaxWorkers int
	// DeleteIntervalMS is the delay between individual file deletions (milliseconds).
	// 0 means default (1000).
	DeleteIntervalMS int
	// ScanIntervalMS is the interval between download directory scans (milliseconds).
	// 0 means default (10000).
	ScanIntervalMS int
	// StableWindowSec is how long a file must be unchanged before its hash is checked.
	// 0 means default (30).
	StableWindowSec int
}

// Defaults fills zero-value fields with sensible defaults.
func (rc *ResourceConfig) Defaults() *ResourceConfig {
	if rc.MaxWorkers <= 0 {
		rc.MaxWorkers = 4
	}
	if rc.DeleteIntervalMS <= 0 {
		rc.DeleteIntervalMS = 1000
	}
	if rc.ScanIntervalMS <= 0 {
		rc.ScanIntervalMS = 10000
	}
	if rc.StableWindowSec <= 0 {
		rc.StableWindowSec = 30
	}
	return rc
}

// CompressTask holds the configuration for a compression task.
type CompressTask struct {
	// ProjectName is the base name for all volume files.
	ProjectName string
	// SourceDir is the root directory to scan and compress.
	SourceDir string
	// OutputDir is where volume files and the manifest will be written.
	OutputDir string
	// VolumeSize is the maximum size of each volume in bytes.
	VolumeSize int64
	// Password is the encryption password. Empty means no encryption.
	Password string
	// EncryptManifest controls whether the manifest volume is also encrypted.
	EncryptManifest bool
}

// DecompressTask holds the configuration for a decompression task.
type DecompressTask struct {
	// ManifestPath is the path to the manifest JSON file or manifest volume.
	ManifestPath string
	// DownloadDir is where the volume files are located.
	DownloadDir string
	// ExtractDir is where extracted files will be placed.
	ExtractDir string
	// Password is the decryption password.
	Password string
	// DeleteWhenDone controls whether volume files are deleted after extraction.
	DeleteWhenDone bool
	// ResourceConfig controls concurrency and scan intervals.
	ResourceConfig ResourceConfig
}

// VolumeStatus represents the current state of a volume during decompression.
type VolumeStatus int

const (
	VolumeStatusUnknown     VolumeStatus = iota
	VolumeStatusMissing                  // File not found on disk
	VolumeStatusDownloading              // File exists but size doesn't match or is still changing
	VolumeStatusReady                    // Hash verified, ready for extraction
	VolumeStatusExtracting               // Currently being extracted
	VolumeStatusDone                     // Successfully extracted
	VolumeStatusError                    // Hash mismatch or extraction failure
)

// String returns a human-readable string for the volume status.
func (s VolumeStatus) String() string {
	switch s {
	case VolumeStatusMissing:
		return "MISSING"
	case VolumeStatusDownloading:
		return "DOWNLOADING"
	case VolumeStatusReady:
		return "READY"
	case VolumeStatusExtracting:
		return "EXTRACTING"
	case VolumeStatusDone:
		return "DONE"
	case VolumeStatusError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}
