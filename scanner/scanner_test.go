package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanDirectory(t *testing.T) {
	// Create a temp directory with test files.
	tmp := t.TempDir()

	// Create files.
	files := map[string]string{
		"a.txt":          "hello",
		"b.txt":          "world",
		"sub/c.txt":      "nested",
		"sub/deep/d.txt": "deep nested",
	}
	for rel, content := range files {
		path := filepath.Join(tmp, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(content), 0o644)
	}

	// Create a symlink that should be skipped.
	os.Symlink(filepath.Join(tmp, "a.txt"), filepath.Join(tmp, "link.txt"))

	result, _, err := ScanDirectory(tmp)
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// We should find 4 regular files (symlink is skipped).
	if len(result) != 4 {
		t.Fatalf("expected 4 files, got %d: %v", len(result), result)
	}

	// Check that all paths are relative and use forward slashes.
	for _, f := range result {
		if filepath.IsAbs(f.Path) {
			t.Errorf("path should be relative, got: %s", f.Path)
		}
		if f.Size == 0 {
			t.Errorf("file %s has zero size", f.Path)
		}
	}
}

func TestScanDirectoryEmpty(t *testing.T) {
	tmp := t.TempDir()
	result, _, err := ScanDirectory(tmp)
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 files, got %d", len(result))
	}
}

func TestScanDirectoryNonExistent(t *testing.T) {
	_, _, err := ScanDirectory("/nonexistent/path/xyz")
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestScanDirectoryFileNotDir(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "file.txt")
	os.WriteFile(filePath, []byte("data"), 0o644)

	_, _, err := ScanDirectory(filePath)
	if err == nil {
		t.Fatal("expected error for file instead of directory")
	}
}

func TestSortBySizeDescending(t *testing.T) {
	files := []FileInfo{
		{Path: "small.txt", Size: 100},
		{Path: "large.txt", Size: 1000000},
		{Path: "medium.txt", Size: 5000},
	}

	SortBySizeDescending(files)

	if files[0].Size != 1000000 {
		t.Errorf("expected largest first, got: %s", files[0].Path)
	}
	if files[2].Size != 100 {
		t.Errorf("expected smallest last, got: %s", files[2].Path)
	}
}

func TestSortBySizeDescendingStable(t *testing.T) {
	files := []FileInfo{
		{Path: "a.txt", Size: 1000},
		{Path: "b.txt", Size: 1000},
		{Path: "c.txt", Size: 500},
	}

	SortBySizeDescending(files)

	// Same-size files should be in alphabetical order.
	if files[0].Path != "a.txt" || files[1].Path != "b.txt" {
		t.Errorf("same-size files should be sorted by name: %v, %v",
			files[0].Path, files[1].Path)
	}
}

func TestScanDirectorySkipsSpecialFiles(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "normal.txt"), []byte("data"), 0o644)

	// Create a directory (should not appear as a file).
	os.MkdirAll(filepath.Join(tmp, "subdir"), 0o755)

	result, _, err := ScanDirectory(tmp)
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Should only find 1 file.
	if len(result) != 1 {
		t.Errorf("expected 1 file, got %d", len(result))
	}
	if result[0].Path != "normal.txt" {
		t.Errorf("expected normal.txt, got: %s", result[0].Path)
	}
}
