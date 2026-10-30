package hasher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileHash(t *testing.T) {
	tmp := t.TempDir()
	content := []byte("hello world")
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, content, 0o644)

	hash, err := FileHash(path)
	if err != nil {
		t.Fatalf("FileHash failed: %v", err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
	if len(hash) != 16 {
		t.Errorf("hash should be 16 hex chars, got %d: %s", len(hash), hash)
	}
}

func TestFileHashDeterministic(t *testing.T) {
	tmp := t.TempDir()
	content := []byte("test content for hashing")
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, content, 0o644)

	h1, _ := FileHash(path)
	h2, _ := FileHash(path)
	if h1 != h2 {
		t.Errorf("hashes should be deterministic: %s != %s", h1, h2)
	}
}

func TestFileHashDifferentContent(t *testing.T) {
	tmp := t.TempDir()
	p1 := filepath.Join(tmp, "a.txt")
	p2 := filepath.Join(tmp, "b.txt")
	os.WriteFile(p1, []byte("content A"), 0o644)
	os.WriteFile(p2, []byte("content B"), 0o644)

	h1, _ := FileHash(p1)
	h2, _ := FileHash(p2)
	if h1 == h2 {
		t.Error("different content should produce different hashes")
	}
}

func TestFileHashEmpty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.txt")
	os.WriteFile(path, []byte{}, 0o644)

	hash, err := FileHash(path)
	if err != nil {
		t.Fatalf("FileHash failed on empty file: %v", err)
	}
	if hash == "" {
		t.Error("hash of empty file should not be empty")
	}
}

func TestFileHashLarge(t *testing.T) {
	tmp := t.TempDir()
	content := make([]byte, 1<<20) // 1 MB
	path := filepath.Join(tmp, "large.bin")
	os.WriteFile(path, content, 0o644)

	hash, err := FileHash(path)
	if err != nil {
		t.Fatalf("FileHash failed on large file: %v", err)
	}
	if hash == "" {
		t.Error("hash of large file should not be empty")
	}
}

func TestFileHashNonExistent(t *testing.T) {
	_, err := FileHash("/nonexistent/file.txt")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestBytesHash(t *testing.T) {
	data := []byte("hello world")
	hash := BytesHash(data)

	if hash == "" {
		t.Error("BytesHash should not return empty string")
	}
	if len(hash) != 16 {
		t.Errorf("hash should be 16 hex chars, got %d", len(hash))
	}
}

func TestBytesHashDeterministic(t *testing.T) {
	data := []byte("test data")
	h1 := BytesHash(data)
	h2 := BytesHash(data)
	if h1 != h2 {
		t.Error("BytesHash should be deterministic")
	}
}

func TestBytesHashDifferent(t *testing.T) {
	h1 := BytesHash([]byte("data A"))
	h2 := BytesHash([]byte("data B"))
	if h1 == h2 {
		t.Error("different data should produce different hashes")
	}
}

func TestFileHashPartial(t *testing.T) {
	tmp := t.TempDir()
	content := []byte("0123456789")
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, content, 0o644)

	// Hash first 5 bytes.
	h1, err := FileHashPartial(path, 0, 5)
	if err != nil {
		t.Fatalf("FileHashPartial failed: %v", err)
	}

	// Hash last 5 bytes.
	h2, err := FileHashPartial(path, 5, 5)
	if err != nil {
		t.Fatalf("FileHashPartial failed: %v", err)
	}

	if h1 == h2 {
		t.Error("different offsets should produce different hashes")
	}
}
