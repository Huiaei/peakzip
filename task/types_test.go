package task

import (
	"testing"
)

func TestResourceConfigDefaults(t *testing.T) {
	rc := &ResourceConfig{}
	rc.Defaults()

	if rc.MaxWorkers != 4 {
		t.Errorf("MaxWorkers: got %d, want 4", rc.MaxWorkers)
	}
	if rc.DeleteIntervalMS != 1000 {
		t.Errorf("DeleteIntervalMS: got %d, want 1000", rc.DeleteIntervalMS)
	}
	if rc.ScanIntervalMS != 10000 {
		t.Errorf("ScanIntervalMS: got %d, want 10000", rc.ScanIntervalMS)
	}
	if rc.StableWindowSec != 30 {
		t.Errorf("StableWindowSec: got %d, want 30", rc.StableWindowSec)
	}
}

func TestResourceConfigDefaultsPreservesExplicit(t *testing.T) {
	rc := &ResourceConfig{
		MaxWorkers:       8,
		DeleteIntervalMS: 500,
		ScanIntervalMS:   5000,
		StableWindowSec:  60,
	}
	rc.Defaults()

	if rc.MaxWorkers != 8 {
		t.Errorf("MaxWorkers: got %d, want 8", rc.MaxWorkers)
	}
	if rc.DeleteIntervalMS != 500 {
		t.Errorf("DeleteIntervalMS: got %d, want 500", rc.DeleteIntervalMS)
	}
	if rc.ScanIntervalMS != 5000 {
		t.Errorf("ScanIntervalMS: got %d, want 5000", rc.ScanIntervalMS)
	}
	if rc.StableWindowSec != 60 {
		t.Errorf("StableWindowSec: got %d, want 60", rc.StableWindowSec)
	}
}

func TestResourceConfigDefaultsPartial(t *testing.T) {
	rc := &ResourceConfig{
		MaxWorkers: 16,
	}
	rc.Defaults()

	if rc.MaxWorkers != 16 {
		t.Errorf("MaxWorkers should be preserved: got %d, want 16", rc.MaxWorkers)
	}
	if rc.DeleteIntervalMS != 1000 {
		t.Errorf("DeleteIntervalMS should default: got %d, want 1000", rc.DeleteIntervalMS)
	}
	if rc.ScanIntervalMS != 10000 {
		t.Errorf("ScanIntervalMS should default: got %d, want 10000", rc.ScanIntervalMS)
	}
	if rc.StableWindowSec != 30 {
		t.Errorf("StableWindowSec should default: got %d, want 30", rc.StableWindowSec)
	}
}

func TestResourceConfigNegativeValues(t *testing.T) {
	rc := &ResourceConfig{
		MaxWorkers:       -1,
		DeleteIntervalMS: -100,
		ScanIntervalMS:   -5000,
		StableWindowSec:  -30,
	}
	rc.Defaults()

	if rc.MaxWorkers != 4 {
		t.Errorf("negative MaxWorkers should default: got %d, want 4", rc.MaxWorkers)
	}
	if rc.DeleteIntervalMS != 1000 {
		t.Errorf("negative DeleteIntervalMS should default: got %d, want 1000", rc.DeleteIntervalMS)
	}
	if rc.ScanIntervalMS != 10000 {
		t.Errorf("negative ScanIntervalMS should default: got %d, want 10000", rc.ScanIntervalMS)
	}
	if rc.StableWindowSec != 30 {
		t.Errorf("negative StableWindowSec should default: got %d, want 30", rc.StableWindowSec)
	}
}

func TestVolumeStatusString(t *testing.T) {
	tests := []struct {
		status VolumeStatus
		want   string
	}{
		{VolumeStatusUnknown, "UNKNOWN"},
		{VolumeStatusMissing, "MISSING"},
		{VolumeStatusDownloading, "DOWNLOADING"},
		{VolumeStatusReady, "READY"},
		{VolumeStatusExtracting, "EXTRACTING"},
		{VolumeStatusDone, "DONE"},
		{VolumeStatusError, "ERROR"},
		{VolumeStatus(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		got := tt.status.String()
		if got != tt.want {
			t.Errorf("VolumeStatus(%d).String(): got %q, want %q",
				tt.status, got, tt.want)
		}
	}
}

func TestCompressTask(t *testing.T) {
	ct := CompressTask{
		ProjectName:     "myproject",
		SourceDir:       "/home/user/project",
		OutputDir:       "/home/user/output",
		VolumeSize:      100 * 1024 * 1024,
		Password:        "secret",
		EncryptManifest: true,
	}

	if ct.ProjectName != "myproject" {
		t.Errorf("ProjectName: got %q", ct.ProjectName)
	}
	if ct.VolumeSize != 100*1024*1024 {
		t.Errorf("VolumeSize: got %d", ct.VolumeSize)
	}
	if !ct.EncryptManifest {
		t.Error("EncryptManifest should be true")
	}
}

func TestDecompressTask(t *testing.T) {
	dt := DecompressTask{
		ManifestPath:   "/home/user/output/manifest.json",
		DownloadDir:    "/home/user/downloads",
		ExtractDir:     "/home/user/restored",
		Password:       "secret",
		DeleteWhenDone: true,
		ResourceConfig: ResourceConfig{
			MaxWorkers: 4,
		},
	}

	if dt.DeleteWhenDone != true {
		t.Error("DeleteWhenDone should be true")
	}
	if dt.ResourceConfig.MaxWorkers != 4 {
		t.Errorf("MaxWorkers: got %d, want 4", dt.ResourceConfig.MaxWorkers)
	}
}

func TestProgressUpdate(t *testing.T) {
	pu := ProgressUpdate{
		Phase:         "compress",
		Message:       "Compressing volume 2 of 10",
		Progress:      0.2,
		CurrentVolume: 2,
		TotalVolumes:  10,
	}

	if pu.Phase != "compress" {
		t.Errorf("Phase: got %q", pu.Phase)
	}
	if pu.Progress != 0.2 {
		t.Errorf("Progress: got %f", pu.Progress)
	}
}
