package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndSave(t *testing.T) {
	tmp := t.TempDir()

	m := &Manifest{
		ManifestVersion:      1,
		ProjectName:          "testproject",
		TotalVolumes:         3,
		VolumeSizeLimit:      100 * 1024 * 1024,
		CompressionAlgorithm: "LZMA2",
		Encryption: EncryptionConfig{
			Enabled: false,
		},
		Files: []FileEntry{
			{
				Path:   "file1.txt",
				Size:   1024,
				Hash:   "a1b2c3d4e5f60718",
				Volume: 1,
			},
			{
				Path:   "file2.txt",
				Size:   2048,
				Hash:   "1122334455667788",
				Volume: 2,
			},
		},
		Volumes: []VolumeEntry{
			{
				LogicalNumber: 1,
				Filename:      "testproject-001.7z",
				Size:          1048576,
				Hash:          "deadbeefcafebabe",
				Type:          "data",
				Contains:      []string{"file1.txt"},
			},
			{
				LogicalNumber: 2,
				Filename:      "testproject-002.7z",
				Size:          1048576,
				Hash:          "beefcafedeadbeef",
				Type:          "data",
				Contains:      []string{"file2.txt"},
			},
		},
	}

	path := filepath.Join(tmp, "manifest.json")
	if err := Save(m, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest file not created: %v", err)
	}
}

func TestLoadManifest(t *testing.T) {
	tmp := t.TempDir()

	original := &Manifest{
		ManifestVersion:      1,
		ProjectName:          "loadtest",
		TotalVolumes:         2,
		VolumeSizeLimit:      50000000,
		CompressionAlgorithm: "LZMA2",
		Encryption: EncryptionConfig{
			Enabled: false,
		},
		Files: []FileEntry{
			{Path: "a.txt", Size: 100, Hash: "hasha", Volume: 1},
			{Path: "b.txt", Size: 200, Hash: "hashb", Volume: 2},
		},
		Volumes: []VolumeEntry{
			{LogicalNumber: 1, Filename: "vol-001.7z", Size: 1024, Type: "data"},
			{LogicalNumber: 2, Filename: "vol-002.7z", Size: 2048, Type: "data"},
		},
	}

	path := filepath.Join(tmp, "manifest.json")
	if err := Save(original, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.ProjectName != "loadtest" {
		t.Errorf("ProjectName: got %q, want %q", loaded.ProjectName, "loadtest")
	}
	if loaded.TotalVolumes != 2 {
		t.Errorf("TotalVolumes: got %d, want %d", loaded.TotalVolumes, 2)
	}
	if len(loaded.Files) != 2 {
		t.Errorf("Files count: got %d, want %d", len(loaded.Files), 2)
	}
	if len(loaded.Volumes) != 2 {
		t.Errorf("Volumes count: got %d, want %d", len(loaded.Volumes), 2)
	}
}

func TestLoadNonExistent(t *testing.T) {
	_, err := Load("/nonexistent/manifest.json")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.json")
	os.WriteFile(path, []byte("{invalid json"), 0o644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCrossVolumeFile(t *testing.T) {
	tmp := t.TempDir()

	m := &Manifest{
		ManifestVersion: 1,
		ProjectName:     "crossvol",
		TotalVolumes:    3,
		VolumeSizeLimit: 10000,
		Encryption:      EncryptionConfig{Enabled: false},
		Files: []FileEntry{
			{
				Path:          "bigfile.bin",
				Size:          25000,
				Hash:          "bighash123",
				Volume:        1,
				IsCrossVolume: true,
				VolumeStart:   1,
				VolumeEnd:     3,
				Fragments: []FileFragment{
					{Volume: 1, Offset: 0, Length: 10000},
					{Volume: 2, Offset: 0, Length: 10000},
					{Volume: 3, Offset: 0, Length: 5000},
				},
			},
		},
		Volumes: []VolumeEntry{
			{LogicalNumber: 1, Filename: "cv-001.7z", Size: 10000, Type: "data"},
			{LogicalNumber: 2, Filename: "cv-002.7z", Size: 10000, Type: "data"},
			{LogicalNumber: 3, Filename: "cv-003.7z", Size: 5000, Type: "data"},
		},
	}

	path := filepath.Join(tmp, "manifest.json")
	if err := Save(m, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	f := loaded.Files[0]
	if !f.IsCrossVolume {
		t.Error("expected IsCrossVolume to be true")
	}
	if f.VolumeStart != 1 || f.VolumeEnd != 3 {
		t.Errorf("volume range: got %d-%d, want 1-3", f.VolumeStart, f.VolumeEnd)
	}
	if len(f.Fragments) != 3 {
		t.Errorf("expected 3 fragments, got %d", len(f.Fragments))
	}
	totalLen := int64(0)
	for _, frag := range f.Fragments {
		totalLen += frag.Length
	}
	if totalLen != f.Size {
		t.Errorf("fragment total: got %d, want %d", totalLen, f.Size)
	}
}

func TestEncryptionConfig(t *testing.T) {
	tmp := t.TempDir()

	m := &Manifest{
		ManifestVersion: 1,
		ProjectName:     "encrtest",
		TotalVolumes:    1,
		VolumeSizeLimit: 1048576,
		Encryption: EncryptionConfig{
			Enabled: true,
			Method:  "AES-256",
		},
		Files:   []FileEntry{},
		Volumes: []VolumeEntry{},
	}

	path := filepath.Join(tmp, "manifest.json")
	if err := Save(m, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !loaded.Encryption.Enabled {
		t.Error("expected encryption to be enabled")
	}
	if loaded.Encryption.Method != EncryptionMethod {
		t.Errorf("method: got %q, want %q", loaded.Encryption.Method, EncryptionMethod)
	}
	// The dead Salt / KDF fields must no longer be serialised.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	for _, forbidden := range []string{"salt", "kdf"} {
		if json.Valid(raw) && strings.Contains(string(raw), forbidden) {
			t.Errorf("manifest JSON must not contain %q", forbidden)
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	tmp := t.TempDir()

	original := &Manifest{
		ManifestVersion:      1,
		ProjectName:          "roundtrip",
		TotalVolumes:         5,
		VolumeSizeLimit:      200000000,
		CompressionAlgorithm: "LZMA2",
		Encryption: EncryptionConfig{
			Enabled: true,
			Method:  "AES-256",
		},
		Files: []FileEntry{
			{
				Path:          "dir/file.txt",
				Size:          4096,
				Hash:          "abcdefgh12345678",
				Volume:        2,
				IsCrossVolume: false,
			},
		},
		Volumes: []VolumeEntry{
			{
				LogicalNumber: 1,
				Filename:      "rt-001.7z",
				Size:          1048576,
				Hash:          "fedcba0987654321",
				Type:          "data",
				Contains:      []string{"dir/file.txt"},
			},
		},
	}

	path := filepath.Join(tmp, "manifest.json")
	if err := Save(original, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Read raw JSON and re-parse to verify field names.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if _, ok := rawMap["manifest_version"]; !ok {
		t.Error("missing manifest_version field")
	}
	if _, ok := rawMap["project_name"]; !ok {
		t.Error("missing project_name field")
	}
	if _, ok := rawMap["encryption"]; !ok {
		t.Error("missing encryption field")
	}
}
