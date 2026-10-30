// Package manifest generates and parses the JSON manifest that describes
// the project files, volume layout, encryption settings, and hashes.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"

	"peakzip/scanner"
)

// ManifestVersion is the current manifest schema version.
const ManifestVersion = 1

// EncryptionMethod is the cipher actually used by the bundled 7-Zip engine.
// 7-Zip encrypts with AES-256 (CBC); the password is derived internally by 7z,
// so no salt or KDF is configured or stored by this application.
const EncryptionMethod = "AES-256"

// EncryptionConfig describes the encryption parameters recorded for the project.
type EncryptionConfig struct {
	Enabled bool   `json:"enabled"`
	Method  string `json:"method,omitempty"`
}

// FileFragment describes one fragment of a cross-volume file within a single volume.
type FileFragment struct {
	Volume int64 `json:"volume"`
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
}

// FileEntry describes a single file in the manifest.
type FileEntry struct {
	Path          string         `json:"path"`
	Size          int64          `json:"size"`
	Hash          string         `json:"hash,omitempty"`
	Volume        int64          `json:"volume,omitempty"`
	IsCrossVolume bool           `json:"is_cross_volume"`
	VolumeStart   int64          `json:"volume_start,omitempty"`
	VolumeEnd     int64          `json:"volume_end,omitempty"`
	Fragments     []FileFragment `json:"fragments,omitempty"`
}

// VolumeEntry describes a single logical volume in the manifest.
type VolumeEntry struct {
	LogicalNumber int64    `json:"logical_number"`
	Filename      string   `json:"filename"`
	Size          int64    `json:"size"`
	Hash          string   `json:"hash,omitempty"`
	Type          string   `json:"type"` // "manifest" or "data"
	Contains      []string `json:"contains,omitempty"`
}

// Manifest is the top-level document describing the project.
type Manifest struct {
	ManifestVersion      int              `json:"manifest_version"`
	ProjectName          string           `json:"project_name"`
	TotalVolumes         int              `json:"total_volumes"`
	VolumeSizeLimit      int64            `json:"volume_size_limit"`
	CompressionAlgorithm string           `json:"compression_algorithm"`
	Encryption           EncryptionConfig `json:"encryption"`
	Files                []FileEntry      `json:"files"`
	Volumes              []VolumeEntry    `json:"volumes"`
}

// Generate builds a Manifest from a list of scanned files.
// Files are sorted by size descending, then allocated into logical volumes
// respecting the given volume size limit. Files larger than the limit become
// cross-volume files that occupy consecutive volumes.
func Generate(projectName string, files []scanner.FileInfo, volumeSize int64, encryption *EncryptionConfig) (*Manifest, error) {
	if volumeSize <= 0 {
		return nil, fmt.Errorf("volume size must be positive, got %d", volumeSize)
	}
	if encryption == nil {
		encryption = &EncryptionConfig{}
	}

	sorted := make([]scanner.FileInfo, len(files))
	copy(sorted, files)
	scanner.SortBySizeDescending(sorted)

	var fileEntries []FileEntry
	var volumeEntries []VolumeEntry

	// Single counter for assigning sequential volume numbers.
	nextVol := 0

	// Current group for independent (non-cross-volume) files.
	var groupFiles []string
	var groupSize int64

	flushGroup := func() {
		if len(groupFiles) == 0 {
			return
		}
		volumeEntries = append(volumeEntries, VolumeEntry{
			LogicalNumber: int64(nextVol),
			Type:          "data",
			Contains:      groupFiles,
		})
		nextVol++
		groupFiles = nil
		groupSize = 0
	}

	for _, f := range sorted {
		if f.Size > volumeSize {
			// Cross-volume file: allocate consecutive volumes, one per fragment.
			flushGroup()
			startVol := nextVol
			numFrags := int((f.Size + volumeSize - 1) / volumeSize)
			fragments := make([]FileFragment, 0, numFrags)
			offset := int64(0)

			for i := 0; i < numFrags; i++ {
				length := volumeSize
				if offset+length > f.Size {
					length = f.Size - offset
				}
				fragments = append(fragments, FileFragment{
					Volume: int64(nextVol),
					Offset: 0,
					Length: length,
				})
				volumeEntries = append(volumeEntries, VolumeEntry{
					LogicalNumber: int64(nextVol),
					Type:          "data",
					Contains:      []string{f.Path},
				})
				nextVol++
				offset += length
			}

			fileEntries = append(fileEntries, FileEntry{
				Path:          f.Path,
				Size:          f.Size,
				IsCrossVolume: true,
				VolumeStart:   int64(startVol),
				VolumeEnd:     int64(nextVol - 1),
				Fragments:     fragments,
			})
		} else {
			// Independent file: try to fit into current group.
			if groupSize > 0 && groupSize+f.Size > volumeSize {
				flushGroup()
			}
			groupSize += f.Size
			groupFiles = append(groupFiles, f.Path)
			fileEntries = append(fileEntries, FileEntry{
				Path:          f.Path,
				Size:          f.Size,
				Volume:        int64(nextVol),
				IsCrossVolume: false,
			})
		}
	}
	flushGroup()

	return &Manifest{
		ManifestVersion:      ManifestVersion,
		ProjectName:          projectName,
		TotalVolumes:         nextVol,
		VolumeSizeLimit:      volumeSize,
		CompressionAlgorithm: "LZMA2",
		Encryption:           *encryption,
		Files:                fileEntries,
		Volumes:              volumeEntries,
	}, nil
}

// Load reads and parses a manifest JSON file.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %q: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %q: %w", path, err)
	}
	if m.ManifestVersion != ManifestVersion {
		return nil, fmt.Errorf("unsupported manifest version %d (expected %d)", m.ManifestVersion, ManifestVersion)
	}
	return &m, nil
}

// Save serializes the manifest to a JSON file.
func Save(m *Manifest, path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write manifest %q: %w", path, err)
	}
	return nil
}

// DataVolumeCount returns the number of data volumes, excluding the manifest volume.
func (m *Manifest) DataVolumeCount() int {
	n := 0
	for i := range m.Volumes {
		if m.Volumes[i].Type == "manifest" {
			continue
		}
		n++
	}
	return n
}

// FindVolumeByNumber returns the VolumeEntry at the given logical number,
// or nil if not found.
func (m *Manifest) FindVolumeByNumber(num int64) *VolumeEntry {
	for i := range m.Volumes {
		if m.Volumes[i].LogicalNumber == num {
			return &m.Volumes[i]
		}
	}
	return nil
}

// FindFilesForVolume returns all file entries that belong to the given volume.
func (m *Manifest) FindFilesForVolume(num int64) []FileEntry {
	var result []FileEntry
	for _, f := range m.Files {
		if f.IsCrossVolume {
			if num >= f.VolumeStart && num <= f.VolumeEnd {
				result = append(result, f)
			}
		} else if f.Volume == num {
			result = append(result, f)
		}
	}
	return result
}
