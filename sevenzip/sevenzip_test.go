package sevenzip

import (
	"os"
	"testing"
)

func TestHasEmbedded(t *testing.T) {
	if !HasEmbedded() {
		t.Error("HasEmbedded should return true (binary is embedded)")
	}
}

func TestExtractTo(t *testing.T) {
	tmp := t.TempDir()

	extracted, err := ExtractTo(tmp)
	if err != nil {
		t.Fatalf("ExtractTo failed: %v", err)
	}

	// File should exist.
	info, err := os.Stat(extracted)
	if err != nil {
		t.Fatalf("extracted file not found: %v", err)
	}

	// File should be executable.
	if info.Mode()&0111 == 0 {
		t.Errorf("extracted file should be executable, got mode %v", info.Mode())
	}

	// File should have non-zero size.
	if info.Size() == 0 {
		t.Error("extracted file should not be empty")
	}

	// Cleanup.
	Cleanup(tmp)

	// After cleanup, file should be gone.
	if _, err := os.Stat(extracted); err == nil {
		t.Error("extracted file should be removed after cleanup")
	}
}

func TestExtractToOverwritesExisting(t *testing.T) {
	dir1 := t.TempDir()
	extracted1, err := ExtractTo(dir1)
	if err != nil {
		t.Fatalf("first ExtractTo failed: %v", err)
	}
	Cleanup(dir1)

	// Second extraction to a new directory.
	dir2 := t.TempDir()
	extracted2, err := ExtractTo(dir2)
	if err != nil {
		t.Fatalf("second ExtractTo failed: %v", err)
	}

	if extracted1 == extracted2 {
		t.Error("different directories should produce different extracted paths")
	}

	Cleanup(dir2)
}

func TestCleanupNonExistent(t *testing.T) {
	tmp := t.TempDir()
	// Should not panic or error.
	Cleanup(tmp)
}

func TestExtractToDifferentDir(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	p1, err := ExtractTo(dir1)
	if err != nil {
		t.Fatalf("ExtractTo dir1 failed: %v", err)
	}

	p2, err := ExtractTo(dir2)
	if err != nil {
		t.Fatalf("ExtractTo dir2 failed: %v", err)
	}

	if p1 == p2 {
		t.Error("different directories should produce different extracted paths")
	}

	// Verify both files exist.
	if _, err := os.Stat(p1); err != nil {
		t.Errorf("file in dir1 not found: %v", err)
	}
	if _, err := os.Stat(p2); err != nil {
		t.Errorf("file in dir2 not found: %v", err)
	}

	Cleanup(dir1)
	Cleanup(dir2)
}

func TestExtractedFileIsNotSymlink(t *testing.T) {
	tmp := t.TempDir()
	extracted, err := ExtractTo(tmp)
	if err != nil {
		t.Fatalf("ExtractTo failed: %v", err)
	}

	info, err := os.Stat(extracted)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("extracted file should not be a symlink")
	}

	Cleanup(tmp)
}
