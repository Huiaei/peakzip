// Package archiver wraps the 7z executable for compression and extraction
// operations. When the system does not have 7z installed, it falls back to
// an embedded copy that is extracted to a temporary directory at runtime.
//
// Every public operation takes a context so that long-running 7z subprocesses
// can be cancelled (e.g. when the user aborts a task).
package archiver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"peakzip/sevenzip"
)

// Archiver wraps the 7z executable for compression and extraction operations.
// It is safe for concurrent use by multiple goroutines.
type Archiver struct {
	// SevenZipPath is the full path to the 7z executable. If empty,
	// it is auto-detected via PATH or the embedded fallback.
	SevenZipPath string

	// TempDir holds the path to the directory where the embedded 7z was
	// extracted. It is automatically managed and cleaned up when Close is called.
	TempDir string

	mu           sync.Mutex
	embeddedPath string
}

// SevenZip returns the resolved path to the 7z executable.
// It checks in this order:
//  1. User-specified SevenZipPath
//  2. System 7z in PATH or common locations
//  3. Embedded 7z (extracted to temp dir if needed)
func (a *Archiver) SevenZip() string {
	if a.SevenZipPath != "" {
		return a.SevenZipPath
	}
	return a.resolvePath()
}

// resolvePath finds the 7z executable, falling back to the embedded version.
func (a *Archiver) resolvePath() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.embeddedPath != "" {
		return a.embeddedPath
	}

	// Try to find system 7z first.
	if p := FindSevenZip(); p != "7z" {
		// Found a real path (not just the plain name fallback).
		a.embeddedPath = p
		return p
	}

	// Check if 7z is actually available in PATH.
	if p, err := exec.LookPath("7z"); err == nil {
		a.embeddedPath = p
		return p
	}

	// Fall back to embedded 7z.
	if sevenzip.HasEmbedded() {
		path, err := sevenzip.ExtractTo(a.TempDir)
		if err != nil {
			// If extraction fails, return the plain name as last resort.
			return "7z"
		}
		if a.TempDir == "" {
			a.TempDir = filepath.Dir(path)
		}
		a.embeddedPath = path
		return path
	}

	return "7z"
}

// Close cleans up the temporary directory containing the embedded 7z binary.
// Call this when the archiver is no longer needed.
func (a *Archiver) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.TempDir != "" {
		sevenzip.Cleanup(a.TempDir)
		a.TempDir = ""
	}
}

// FindSevenZip searches common locations for the 7z executable.
// Returns "7z" as a fallback name if not found.
func FindSevenZip() string {
	candidates := []string{"7z", "7za", "7z.exe", "7za.exe"}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	// Check common Windows installation paths.
	if runtime.GOOS == "windows" {
		windowsPaths := []string{
			filepath.Join(os.Getenv("ProgramFiles"), "7-Zip", "7z.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "7-Zip", "7z.exe"),
			filepath.Join(os.Getenv("SystemDrive"), "7z.exe"),
		}
		for _, p := range windowsPaths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	// Fall back to plain name (will fail if not in PATH).
	return "7z"
}

// run executes the 7z command with the given arguments, honouring ctx
// cancellation by terminating the subprocess.
func (a *Archiver) run(ctx context.Context, args ...string) error {
	return a.runIn(ctx, "", args...)
}

// runIn is run with an explicit working directory. Passing a directory lets
// callers hand 7z relative paths, which preserves the source directory
// structure inside the archive (7z stores only the basename of an absolute
// path).
func (a *Archiver) runIn(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, a.SevenZip(), args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("7z command cancelled: %w", ctxErr)
		}
		return fmt.Errorf("7z command failed (%s %v): %w\nstderr: %s",
			a.SevenZip(), args, err, stderr.String())
	}
	return nil
}

// Run executes the 7z command with the given arguments (exported for external use).
func (a *Archiver) Run(ctx context.Context, args ...string) error {
	return a.run(ctx, args...)
}

// RunInDir executes the 7z command with the given arguments from within dir.
// Use this when the archive should record paths relative to dir, so that the
// directory layout is preserved instead of being flattened to basenames.
func (a *Archiver) RunInDir(ctx context.Context, dir string, args ...string) error {
	return a.runIn(ctx, dir, args...)
}

// EncryptionArgs returns 7z switches for encrypted archives.
// Exported for use by the task package.
func EncryptionArgs(password string) []string {
	if password == "" {
		return nil
	}
	return []string{"-p" + password, "-mhe=on"}
}

// CompressFile compresses a single file into a 7z archive.
func (a *Archiver) CompressFile(ctx context.Context, output, input, password string) error {
	args := []string{"a", "-t7z"}
	args = append(args, EncryptionArgs(password)...)
	args = append(args, output, input)
	return a.run(ctx, args...)
}

// CompressFiles compresses multiple files into a single 7z archive.
// If the file list is too long for the command line, a response file is used.
func (a *Archiver) CompressFiles(ctx context.Context, output string, inputs []string, password string) error {
	// Use response file if there are many inputs to avoid command-line length limits.
	if len(inputs) > 200 {
		return a.compressFilesWithResponseFile(ctx, output, inputs, password)
	}

	args := []string{"a", "-t7z"}
	args = append(args, EncryptionArgs(password)...)
	args = append(args, output)
	args = append(args, inputs...)
	return a.run(ctx, args...)
}

// compressFilesWithResponseFile uses a 7z response file (@listfile) to
// pass a long list of input paths.
func (a *Archiver) compressFilesWithResponseFile(ctx context.Context, output string, inputs []string, password string) error {
	listFile, err := os.CreateTemp("", "peakzip-list-*.txt")
	if err != nil {
		return fmt.Errorf("create temp list file: %w", err)
	}
	listPath := listFile.Name()
	defer os.Remove(listPath)

	for _, in := range inputs {
		if _, err := fmt.Fprintln(listFile, in); err != nil {
			listFile.Close()
			return fmt.Errorf("write list file: %w", err)
		}
	}
	listFile.Close()

	args := []string{"a", "-t7z"}
	args = append(args, EncryptionArgs(password)...)
	args = append(args, output, "@"+listPath)
	return a.run(ctx, args...)
}

// Extract extracts an archive (or split archive) to the destination directory.
// For split archives (e.g. archive.001), 7z automatically finds sibling parts.
func (a *Archiver) Extract(ctx context.Context, archive, destDir, password string) error {
	args := []string{"x"}
	if password != "" {
		args = append(args, "-p"+password)
	}
	args = append(args, "-y", "-o"+destDir, archive)
	return a.run(ctx, args...)
}

// TestArchive checks the integrity of an archive without extracting.
func (a *Archiver) TestArchive(ctx context.Context, archive, password string) error {
	args := []string{"t"}
	if password != "" {
		args = append(args, "-p"+password)
	}
	args = append(args, archive)
	return a.run(ctx, args...)
}
