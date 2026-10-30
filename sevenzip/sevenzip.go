// Package sevenzip provides an embedded 7z executable that is extracted to
// a temporary directory at runtime. It serves as a fallback when the system
// does not have 7z installed.
package sevenzip

import (
	"fmt"
	"os"
	"path/filepath"
)

// ExtractTo extracts the embedded 7z binary to a temporary directory and
// returns the path to the extracted executable.
func ExtractTo(dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "peakzip-7z-*")
		if err != nil {
			return "", fmt.Errorf("create temp dir: %w", err)
		}
	}

	exeName := binaryName()
	exePath := filepath.Join(dir, exeName)

	// If the file already exists, return the path.
	if _, err := os.Stat(exePath); err == nil {
		return exePath, nil
	}

	data := binaryData()
	if len(data) == 0 {
		return "", fmt.Errorf("no embedded binary available")
	}

	if err := os.WriteFile(exePath, data, 0o755); err != nil {
		return "", fmt.Errorf("write binary: %w", err)
	}

	return exePath, nil
}

// Cleanup removes the temporary directory containing the extracted 7z binary.
// If dir is empty, it does nothing.
func Cleanup(dir string) {
	if dir != "" {
		os.RemoveAll(dir)
	}
}

// HasEmbedded returns true if an embedded 7z binary is available.
func HasEmbedded() bool {
	return len(binaryData()) > 0
}
