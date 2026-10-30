// Package hasher computes xxHash-64 checksums for files and byte slices.
package hasher

import (
	"fmt"
	"io"
	"os"

	"github.com/cespare/xxhash/v2"
)

// FileHash reads the entire file and returns its xxHash-64 as a 16-character hex string.
func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	h := xxhash.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return fmt.Sprintf("%016x", h.Sum64()), nil
}

// BytesHash computes the xxHash-64 of a byte slice and returns it as a 16-character hex string.
func BytesHash(data []byte) string {
	return fmt.Sprintf("%016x", xxhash.Sum64(data))
}

// FileHashPartial computes the xxHash-64 of a range within a file.
// It reads 'length' bytes starting at byte 'offset'.
func FileHashPartial(path string, offset, length int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return "", fmt.Errorf("seek %q: %w", path, err)
		}
	}

	h := xxhash.New()
	_, err = io.CopyN(h, f, length)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("hash partial %q: %w", path, err)
	}
	return fmt.Sprintf("%016x", h.Sum64()), nil
}
