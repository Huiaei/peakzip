package task

import (
	"path/filepath"
	"testing"

	"peakzip/manifest"
)

func TestHumanSize(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{5 * 1024 * 1024 * 1024, "5.0 GB"},
		{2*1024*1024*1024 + 300*1024*1024, "2.3 GB"},
	}
	for _, c := range cases {
		if got := humanSize(c.n); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestEstimatePeakSpace(t *testing.T) {
	// 3 volumes: 100MB, 100MB, 50MB; one cross-volume file spanning vols 0-1.
	m := &manifest.Manifest{
		Volumes: []manifest.VolumeEntry{
			{LogicalNumber: 0, Type: "data", Size: 100 * 1024 * 1024},
			{LogicalNumber: 1, Type: "data", Size: 100 * 1024 * 1024},
			{LogicalNumber: 2, Type: "data", Size: 50 * 1024 * 1024},
		},
		Files: []manifest.FileEntry{
			{
				Path:          "big.bin",
				IsCrossVolume: true,
				VolumeStart:   0,
				VolumeEnd:     1,
				Fragments: []manifest.FileFragment{
					{Volume: 0, Length: 100 * 1024 * 1024},
					{Volume: 1, Length: 50 * 1024 * 1024},
				},
			},
			{Path: "a.txt", Size: 10 * 1024 * 1024, Volume: 2},
		},
	}

	// volumeTotal = 100+100+50 = 250MB
	// maxSingleVolumeOutput = max(vol0:100MB, vol1:50MB, vol2:10MB) = 100MB
	// peak = 250 + 100 + 2GB
	want := int64(250*1024*1024 + 100*1024*1024 + keepFreeBytes)
	if got := estimatePeakSpace(m, false); got != want {
		t.Errorf("estimatePeakSpace = %d, want %d", got, want)
	}
}

func TestEstimatePeakSpaceEmpty(t *testing.T) {
	m := &manifest.Manifest{}
	if got := estimatePeakSpace(m, false); got != keepFreeBytes {
		t.Errorf("estimatePeakSpace(empty) = %d, want %d", got, keepFreeBytes)
	}
}

func TestNearestExistingDir(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "not", "created", "yet", "deep")
	got, err := nearestExistingDir(deep)
	if err != nil {
		t.Fatalf("nearestExistingDir(%q) error: %v", deep, err)
	}
	if got != root {
		t.Errorf("nearestExistingDir(%q) = %q, want %q", deep, got, root)
	}

	// An existing directory resolves to itself.
	got, err = nearestExistingDir(root)
	if err != nil {
		t.Fatalf("nearestExistingDir(%q) error: %v", root, err)
	}
	if got != root {
		t.Errorf("nearestExistingDir(%q) = %q, want %q", root, got, root)
	}
}
