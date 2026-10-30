// Package scanner recursively scans a directory tree and returns a list of files
// with their sizes, skipping symlinks and special files.
package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// FileInfo holds information about a file discovered during scanning.
type FileInfo struct {
	// Path is the file path relative to the scan root, using forward slashes.
	Path string
	// Size is the file size in bytes.
	Size int64
}

// SortBySizeDescending sorts files by size descending, then by path ascending.
func SortBySizeDescending(files []FileInfo) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Size != files[j].Size {
			return files[i].Size > files[j].Size
		}
		return files[i].Path < files[j].Path
	})
}

// ScanDirectory recursively walks the directory tree rooted at root and
// collects all regular files. Symbolic links and special files (sockets,
// device files, etc.) are skipped.
//
// It returns three values:
//   - the files found,
//   - human-readable warnings for entries that could not be inspected
//     (permission denied, vanished files, ...). A scan with warnings is still
//     a successful scan: the remaining files are usable.
//   - a fatal error, which means the root itself is unusable and the scan
//     produced nothing worth reporting.
func ScanDirectory(root string) (files []FileInfo, warnings []string, err error) {
	// Verify the root exists and is a directory.
	info, err := os.Stat(root)
	if err != nil {
		return nil, nil, fmt.Errorf("stat root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("root %q is not a directory", root)
	}

	err = filepath.Walk(root, func(path string, d fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			// Unreadable entry: record a warning and skip it rather than
			// aborting the whole scan.
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			warnings = append(warnings, fmt.Sprintf("skipped unreadable entry %q: %v", rel, walkErr))
			return nil
		}
		if !d.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			warnings = append(warnings, fmt.Sprintf("skipped entry with uncomputable relative path %q: %v", path, relErr))
			return nil
		}
		files = append(files, FileInfo{
			Path: filepath.ToSlash(rel),
			Size: d.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, warnings, fmt.Errorf("walk root %q: %w", root, err)
	}

	return files, warnings, nil
}
