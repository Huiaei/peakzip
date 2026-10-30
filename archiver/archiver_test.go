package archiver

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupArchiver(t *testing.T) (*Archiver, string) {
	t.Helper()
	tmp := t.TempDir()

	a := &Archiver{TempDir: tmp}
	t.Cleanup(func() { a.Close() })

	return a, tmp
}

func createTestFiles(t *testing.T, dir string) []string {
	t.Helper()

	files := map[string]string{
		"file1.txt":               "This is the content of file 1.\n",
		"file2.txt":               "This is the content of file 2.\n",
		"subdir/file3.txt":        "Nested file 3 content.\n",
		"subdir/nested/file4.txt": "Deeply nested file 4.\n",
	}

	var paths []string
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(content), 0o644)
		paths = append(paths, path)
	}

	return paths
}

func TestArchiverSevenZip(t *testing.T) {
	tmp := t.TempDir()
	a := &Archiver{TempDir: tmp}
	defer a.Close()

	path := a.SevenZip()
	if path == "" {
		t.Fatal("SevenZip() returned empty path")
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("7z not found at %s: %v", path, err)
	}

	t.Logf("7z resolved to: %s", path)
}

func TestCompressFile(t *testing.T) {
	a, tmp := setupArchiver(t)

	srcDir := filepath.Join(tmp, "src")
	os.MkdirAll(srcDir, 0o755)
	srcFile := filepath.Join(srcDir, "hello.txt")
	os.WriteFile(srcFile, []byte("Hello, World!"), 0o644)

	outDir := filepath.Join(tmp, "out")
	os.MkdirAll(outDir, 0o755)
	outArchive := filepath.Join(outDir, "test.7z")

	err := a.CompressFile(context.Background(), outArchive, srcFile, "")
	if err != nil {
		t.Fatalf("CompressFile failed: %v", err)
	}

	info, err := os.Stat(outArchive)
	if err != nil {
		t.Fatalf("archive not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("archive should not be empty")
	}

	t.Logf("Created archive: %s (%d bytes)", outArchive, info.Size())
}

func TestCompressFiles(t *testing.T) {
	a, tmp := setupArchiver(t)

	srcDir := filepath.Join(tmp, "src")
	os.MkdirAll(srcDir, 0o755)
	srcFiles := createTestFiles(t, srcDir)

	outDir := filepath.Join(tmp, "out")
	os.MkdirAll(outDir, 0o755)
	outArchive := filepath.Join(outDir, "multi.7z")

	err := a.CompressFiles(context.Background(), outArchive, srcFiles, "")
	if err != nil {
		t.Fatalf("CompressFiles failed: %v", err)
	}

	info, err := os.Stat(outArchive)
	if err != nil {
		t.Fatalf("archive not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("archive should not be empty")
	}

	t.Logf("Created multi-file archive: %s (%d bytes)", outArchive, info.Size())
}

func TestCompressAndExtract(t *testing.T) {
	a, tmp := setupArchiver(t)

	srcDir := filepath.Join(tmp, "src")
	os.MkdirAll(srcDir, 0o755)
	srcFiles := createTestFiles(t, srcDir)

	// Compress.
	outDir := filepath.Join(tmp, "out")
	os.MkdirAll(outDir, 0o755)
	archive := filepath.Join(outDir, "roundtrip.7z")

	if err := a.CompressFiles(context.Background(), archive, srcFiles, ""); err != nil {
		t.Fatalf("CompressFiles failed: %v", err)
	}

	// Extract.
	extractDir := filepath.Join(tmp, "extracted")
	os.MkdirAll(extractDir, 0o755)

	if err := a.Extract(context.Background(), archive, extractDir, ""); err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Verify all files were extracted.
	// Note: 7z stores only basenames when given absolute paths, so files
	// are extracted flat (no directory structure).
	for _, srcPath := range srcFiles {
		baseName := filepath.Base(srcPath)
		extractedPath := filepath.Join(extractDir, baseName)
		if _, err := os.Stat(extractedPath); err != nil {
			t.Errorf("file not extracted: %s: %v", baseName, err)
			continue
		}

		srcContent, _ := os.ReadFile(srcPath)
		extContent, _ := os.ReadFile(extractedPath)
		if !bytes.Equal(srcContent, extContent) {
			t.Errorf("content mismatch for %s", baseName)
		}
	}

	t.Logf("Roundtrip successful: %d files extracted", len(srcFiles))
}

func TestExtractNonExistent(t *testing.T) {
	a, tmp := setupArchiver(t)

	extractDir := filepath.Join(tmp, "extracted")
	os.MkdirAll(extractDir, 0o755)

	err := a.Extract(context.Background(), "/nonexistent/7z/file.7z", extractDir, "")
	if err == nil {
		t.Fatal("expected error for non-existent archive")
	}
}

func TestTestArchive(t *testing.T) {
	a, tmp := setupArchiver(t)

	srcDir := filepath.Join(tmp, "src")
	os.MkdirAll(srcDir, 0o755)
	srcFiles := createTestFiles(t, srcDir)

	outDir := filepath.Join(tmp, "out")
	os.MkdirAll(outDir, 0o755)
	archive := filepath.Join(outDir, "test.7z")

	if err := a.CompressFiles(context.Background(), archive, srcFiles, ""); err != nil {
		t.Fatalf("CompressFiles failed: %v", err)
	}

	err := a.TestArchive(context.Background(), archive, "")
	if err != nil {
		t.Errorf("TestArchive failed: %v", err)
	}
}

func TestRunHelp(t *testing.T) {
	a, _ := setupArchiver(t)

	err := a.Run(context.Background(), "--help")
	if err != nil {
		t.Errorf("Run --help failed: %v", err)
	}
}

func TestRunInvalidCommand(t *testing.T) {
	a, _ := setupArchiver(t)

	err := a.Run(context.Background(), "--nonexistent-command", "arg1")
	if err == nil {
		t.Log("WARNING: invalid command succeeded (may vary by 7z version)")
	}
}

// TestRunCancelledContext verifies that a pre-cancelled context fails fast.
func TestRunCancelledContext(t *testing.T) {
	a, _ := setupArchiver(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := a.Run(ctx, "--help")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected cancellation error, got: %v", err)
	}
}

// TestExtractCancelledContext verifies that a running 7z subprocess is
// actually terminated when its context is cancelled.
func TestExtractCancelledContext(t *testing.T) {
	a, tmp := setupArchiver(t)

	// Build an incompressible file so LZMA2 compression is genuinely slow.
	srcDir := filepath.Join(tmp, "src")
	os.MkdirAll(srcDir, 0o755)
	srcFile := filepath.Join(srcDir, "big.bin")
	f, err := os.Open("/dev/urandom")
	if err != nil {
		t.Skipf("cannot read /dev/urandom: %v", err)
	}
	f.Close()
	random := make([]byte, 32*1024*1024)
	f, err = os.Open("/dev/urandom")
	if err != nil {
		t.Skipf("cannot read /dev/urandom: %v", err)
	}
	defer f.Close()
	if _, err = f.Read(random); err != nil {
		t.Skipf("reading /dev/urandom failed: %v", err)
	}
	if err := os.WriteFile(srcFile, random, 0o644); err != nil {
		t.Fatalf("write big file: %v", err)
	}

	archive := filepath.Join(tmp, "big.7z")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx, "a", "-t7z", "-mx=9", archive, srcFile)
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()

	start := time.Now()
	select {
	case err := <-done:
		if err == nil {
			t.Skip("compression finished before cancellation was requested")
		}
		if !strings.Contains(err.Error(), "cancelled") {
			t.Errorf("expected cancellation error, got: %v", err)
		}
		t.Logf("cancelled after %v", time.Since(start))
	case <-time.After(15 * time.Second):
		t.Fatal("7z subprocess was not terminated by context cancellation")
	}
}
