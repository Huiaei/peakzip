package task

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"peakzip/hasher"
	"peakzip/logger"
	"peakzip/manifest"
	"peakzip/state"
)

// These integration tests exercise the real compress -> manifest ->
// decompress pipeline against the 7-Zip executable on PATH.

func TestMain(m *testing.M) {
	if p, err := exec.LookPath("7z"); err != nil {
		fmt.Fprintln(os.Stderr, "7z not found on PATH, skipping package:", err)
		os.Exit(0)
	} else {
		fmt.Fprintf(os.Stderr, "using 7z: %s", p)
	}
	os.Exit(m.Run())
}

func testResourceConfig() ResourceConfig {
	return ResourceConfig{
		MaxWorkers:       2,
		ScanIntervalMS:   100,
		StableWindowSec:  1,
		DeleteIntervalMS: 1,
	}
}

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New("", logger.LevelDebug)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

// writeIncompressible writes size bytes of random data to path.
func writeIncompressible(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	f, err := os.Open("/dev/urandom")
	if err != nil {
		t.Skipf("cannot read /dev/urandom: %v", err)
	}
	defer f.Close()
	b := make([]byte, size)
	if _, err := io.ReadFull(f, b); err != nil {
		t.Skipf("reading /dev/urandom: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runCompress(t *testing.T, cfg CompressTask) {
	t.Helper()
	ctx := context.Background()
	if err := RunCompress(ctx, cfg, make(chan ProgressUpdate, 4), testLogger(t)); err != nil {
		t.Fatalf("RunCompress failed: %v", err)
	}
}

func runDecompress(t *testing.T, cfg DecompressTask, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := RunDecompress(ctx, cfg, make(chan ProgressUpdate, 4), testLogger(t)); err != nil {
		t.Fatalf("RunDecompress failed: %v", err)
	}
}

func manifestVolumeName(m *manifest.Manifest) string {
	for i := range m.Volumes {
		if m.Volumes[i].Type == "manifest" {
			return m.Volumes[i].Filename
		}
	}
	return ""
}

func walkFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}

// compareTrees verifies every regular file under a exists under b with
// identical content and an identical xxHash.
func compareTrees(t *testing.T, a, b string) {
	t.Helper()
	aFiles := walkFiles(t, a)
	if len(aFiles) == 0 {
		t.Fatal("source tree contains no regular files")
	}
	for _, rel := range aFiles {
		aPath := filepath.Join(a, filepath.FromSlash(rel))
		bPath := filepath.Join(b, filepath.FromSlash(rel))

		aData, err := os.ReadFile(aPath)
		if err != nil {
			t.Fatalf("read %s: %v", aPath, err)
			continue
		}
		bData, err := os.ReadFile(bPath)
		if err != nil {
			t.Errorf("missing restored file %s: %v", bPath, err)
			continue
		}
		if !bytes.Equal(aData, bData) {
			t.Errorf("content mismatch for %s (%d vs %d bytes)", rel, len(aData), len(bData))
			continue
		}
		aHash, err := hasher.FileHash(aPath)
		if err != nil {
			t.Errorf("hash %s: %v", aPath, err)
			continue
		}
		bHash, err := hasher.FileHash(bPath)
		if err != nil {
			t.Errorf("hash %s: %v", bPath, err)
			continue
		}
		if aHash != bHash {
			t.Errorf("hash mismatch for %s: %s != %s", rel, aHash, bHash)
		}
	}
}

func TestCompressDecompressRoundTrip(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	out := filepath.Join(root, "out")
	restored := filepath.Join(root, "restored")
	t.Cleanup(func() { os.RemoveAll(root) })

	writeIncompressible(t, filepath.Join(src, "big.bin"), 1<<20)
	os.MkdirAll(filepath.Join(src, "docs", "deep"), 0o755)
	os.WriteFile(filepath.Join(src, "docs", "readme.txt"), []byte("hello peakzip"), 0o644)
	os.WriteFile(filepath.Join(src, "docs", "deep", "nested.log"), []byte("nested content"), 0o644)
	os.WriteFile(filepath.Join(src, ".hidden"), []byte("hidden"), 0o644)
	os.MkdirAll(filepath.Join(src, "emptydir"), 0o755)

	runCompress(t, CompressTask{
		ProjectName: "rtproj",
		SourceDir:   src,
		OutputDir:   out,
		VolumeSize:  10 << 20,
	})

	manifestPath := filepath.Join(out, "rtproj.manifest.json")
	_, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	runDecompress(t, DecompressTask{
		ManifestPath:   manifestPath,
		DownloadDir:    out,
		ExtractDir:     restored,
		ResourceConfig: testResourceConfig(),
	}, 90*time.Second)

	compareTrees(t, src, restored)
}

func TestCompressWithEncryption(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	out := filepath.Join(root, "out")
	restored := filepath.Join(root, "restored")
	t.Cleanup(func() { os.RemoveAll(root) })

	os.MkdirAll(src, 0o755)
	writeIncompressible(t, filepath.Join(src, "secret.bin"), 1<<18)
	os.MkdirAll(filepath.Join(src, "private"), 0o755)
	os.WriteFile(filepath.Join(src, "private", "note.txt"), []byte("do not read"), 0o644)

	const password = "p@$$w0rd & " + "'" + "| ;"
	runCompress(t, CompressTask{
		ProjectName:     "encproj",
		SourceDir:       src,
		OutputDir:       out,
		VolumeSize:      1 << 20,
		Password:        password,
		EncryptManifest: true,
	})

	manifestPath := filepath.Join(out, "encproj.manifest.json")
	m, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if !m.Encryption.Enabled || m.Encryption.Method != manifest.EncryptionMethod {
		t.Errorf("encryption config: got %+v", m.Encryption)
	}

	runDecompress(t, DecompressTask{
		ManifestPath:   manifestPath,
		DownloadDir:    out,
		ExtractDir:     restored,
		Password:       password,
		ResourceConfig: testResourceConfig(),
	}, 90*time.Second)

	compareTrees(t, src, restored)
}

func TestCrossVolumeFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	out := filepath.Join(root, "out")
	restored := filepath.Join(root, "restored")
	t.Cleanup(func() { os.RemoveAll(root) })

	os.MkdirAll(filepath.Join(src, "media"), 0o755)
	big := filepath.Join(src, "media", "large.bin")
	writeIncompressible(t, big, 1<<20)
	os.WriteFile(filepath.Join(src, "small.txt"), []byte("small"), 0o644)

	const volSize = 100 << 10
	runCompress(t, CompressTask{
		ProjectName: "cvproj",
		SourceDir:   src,
		OutputDir:   out,
		VolumeSize:  volSize,
	})

	m, err := manifest.Load(filepath.Join(out, "cvproj.manifest.json"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	var cv *manifest.FileEntry
	for i := range m.Files {
		if m.Files[i].IsCrossVolume {
			cv = &m.Files[i]
			break
		}
	}
	if cv == nil {
		t.Fatal("expected at least one cross-volume file")
	}
	if cv.Path != "media/large.bin" {
		t.Errorf("cross-volume path: got %q, want media/large.bin", cv.Path)
	}
	if len(cv.Fragments) < 3 {
		t.Fatalf("expected >=3 fragments, got %d", len(cv.Fragments))
	}
	for i, frag := range cv.Fragments {
		ve := m.FindVolumeByNumber(frag.Volume)
		if ve == nil {
			t.Fatalf("fragment %d: no volume entry for logical number %d", i, frag.Volume)
		}
		if ve.Filename == "" {
			t.Errorf("fragment %d: volume %d has no filename", i, frag.Volume)
		}
		expectedSuffix := fmt.Sprintf(".%03d", i+1)
		if !strings.HasSuffix(ve.Filename, expectedSuffix) {
			t.Errorf("fragment %d: filename %q should end with %s", i, ve.Filename, expectedSuffix)
		}
	}
	if cv.VolumeEnd != cv.VolumeStart+int64(len(cv.Fragments)-1) {
		t.Errorf("VolumeEnd=%d, want %d", cv.VolumeEnd, cv.VolumeStart+int64(len(cv.Fragments)-1))
	}
	if m.DataVolumeCount() < len(cv.Fragments) {
		t.Errorf("DataVolumeCount: got %d, want >= %d", m.DataVolumeCount(), len(cv.Fragments))
	}

	runDecompress(t, DecompressTask{
		ManifestPath:   filepath.Join(out, "cvproj.manifest.json"),
		DownloadDir:    out,
		ExtractDir:     restored,
		ResourceConfig: testResourceConfig(),
	}, 180*time.Second)

	compareTrees(t, src, restored)

	partCount := 0
	for i := range m.Volumes {
		if strings.HasSuffix(m.Volumes[i].Filename, ".7z") {
			continue
		}
		partCount++
	}
	if partCount != len(cv.Fragments) {
		t.Errorf("part files on disk: got %d, want %d", partCount, len(cv.Fragments))
	}
}

func TestCompressCancelledContext(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	out := filepath.Join(root, "out")
	t.Cleanup(func() { os.RemoveAll(root) })

	os.MkdirAll(src, 0o755)
	writeIncompressible(t, filepath.Join(src, "big.bin"), 10<<20)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunCompress(ctx, CompressTask{
			ProjectName: "cancelproj",
			SourceDir:   src,
			OutputDir:   out,
			VolumeSize:  1 << 20,
		}, make(chan ProgressUpdate, 4), testLogger(t))
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Skip("compression finished before cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled error, got: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("compression not cancelled within timeout")
	}
}

func copyFileToDir(t *testing.T, src, dstDir string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	dst := filepath.Join(dstDir, filepath.Base(src))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func TestResumeAfterInterruption(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	out := filepath.Join(root, "out")
	restored := filepath.Join(root, "restored")
	t.Cleanup(func() { os.RemoveAll(root) })

	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("file a content"), 0o644)
	os.WriteFile(filepath.Join(src, "b.txt"), []byte("file b content"), 0o644)
	os.WriteFile(filepath.Join(src, "c.txt"), []byte("file c content"), 0o644)

	const volSize = 20
	runCompress(t, CompressTask{
		ProjectName: "resumeproj",
		SourceDir:   src,
		OutputDir:   out,
		VolumeSize:  volSize,
	})

	m, err := manifest.Load(filepath.Join(out, "resumeproj.manifest.json"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if m.DataVolumeCount() < 2 {
		t.Fatalf("need at least 2 data volumes, got %d", m.DataVolumeCount())
	}

	partial := filepath.Join(root, "partial")
	os.MkdirAll(partial, 0o755)
	for i := range m.Volumes {
		if m.Volumes[i].Type == "manifest" {
			continue
		}
		if m.Volumes[i].LogicalNumber == 0 {
			continue
		}
		os.Rename(filepath.Join(out, m.Volumes[i].Filename),
			filepath.Join(out, "."+m.Volumes[i].Filename))
	}
	copyFileToDir(t, filepath.Join(out, m.Volumes[0].Filename), partial)
	copyFileToDir(t, filepath.Join(out, "resumeproj.manifest.json"), partial)

	firstCfg := DecompressTask{
		ManifestPath: filepath.Join(partial, "resumeproj.manifest.json"),
		DownloadDir:  partial,
		ExtractDir:   restored,
		ResourceConfig: ResourceConfig{
			MaxWorkers:       2,
			ScanIntervalMS:   100,
			StableWindowSec:  1,
			DeleteIntervalMS: 1,
		},
	}

	ctx1, cancel1 := context.WithTimeout(context.Background(), 1*time.Second)
	afterCancel := make(chan error, 1)
	go func() { afterCancel <- RunDecompress(ctx1, firstCfg, make(chan ProgressUpdate, 4), testLogger(t)) }()
	time.Sleep(300 * time.Millisecond)
	cancel1()

	select {
	case err := <-afterCancel:
		if err == nil {
			t.Fatal("first run should have been interrupted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first run did not respond to cancellation")
	}

	st, err := state.LoadState(state.StateFilePath(restored))
	if err != nil {
		t.Fatalf("state file not persisted on cancel: %v", err)
	}
	if st.TaskID == "" {
		t.Fatal("saved state has empty TaskID")
	}

	for i := range m.Volumes {
		if m.Volumes[i].Type == "manifest" {
			continue
		}
		if m.Volumes[i].LogicalNumber == 0 {
			continue
		}
		hiddenPath := filepath.Join(out, "."+m.Volumes[i].Filename)
		normalPath := filepath.Join(partial, m.Volumes[i].Filename)
		if err := os.Rename(hiddenPath, normalPath); err != nil {
			t.Fatalf("move %s -> %s: %v", hiddenPath, normalPath, err)
		}
	}

	secondCfg := DecompressTask{
		ManifestPath:   filepath.Join(partial, "resumeproj.manifest.json"),
		DownloadDir:    partial,
		ExtractDir:     restored,
		ResourceConfig: testResourceConfig(),
	}
	runDecompress(t, secondCfg, 90*time.Second)

	compareTrees(t, src, restored)
}
