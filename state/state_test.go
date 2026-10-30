package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewState(t *testing.T) {
	s := NewState("test-task")
	if s.TaskID != "test-task" {
		t.Errorf("TaskID: got %q, want %q", s.TaskID, "test-task")
	}
	if len(s.VolumesDone) != 0 {
		t.Errorf("VolumesDone should be empty, got %v", s.VolumesDone)
	}
	if len(s.FilesDone) != 0 {
		t.Errorf("FilesDone should be empty, got %v", s.FilesDone)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmp := t.TempDir()

	s := NewState("save-test")
	s.MarkVolumeDone("vol-001.7z")
	s.MarkVolumeDone("vol-002.7z")
	s.MarkFileDone("file1.txt")
	s.MarkFileDone("file2.txt")
	s.AddToCrossVolumeProgress("bigfile.bin", "fragment-1")
	s.SetCrossVolumeNeeded("bigfile.bin", []string{"vol-003.7z", "vol-004.7z"})
	s.AddToDeleteQueue("vol-001.7z")

	path := filepath.Join(tmp, "task_state.json")
	if err := SaveState(s, path); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if loaded.TaskID != "save-test" {
		t.Errorf("TaskID: got %q, want %q", loaded.TaskID, "save-test")
	}

	// Check volumes done.
	if !loaded.IsVolumeDone("vol-001.7z") {
		t.Error("vol-001.7z should be done")
	}
	if !loaded.IsVolumeDone("vol-002.7z") {
		t.Error("vol-002.7z should be done")
	}
	if loaded.IsVolumeDone("vol-003.7z") {
		t.Error("vol-003.7z should not be done")
	}

	// Check files done.
	if !loaded.IsFileDone("file1.txt") {
		t.Error("file1.txt should be done")
	}
	if !loaded.IsFileDone("file2.txt") {
		t.Error("file2.txt should be done")
	}
	if loaded.IsFileDone("file3.txt") {
		t.Error("file3.txt should not be done")
	}

	// Check cross volume progress.
	cvp, ok := loaded.CrossVolumeProgress["bigfile.bin"]
	if !ok {
		t.Fatal("bigfile.bin should have cross volume progress")
	}
	if len(cvp.Needed) != 2 {
		t.Errorf("needed: got %d, want 2", len(cvp.Needed))
	}
	if len(cvp.Ready) != 1 {
		t.Errorf("ready: got %d, want 1", len(cvp.Ready))
	}

	// Check delete queue.
	if loaded.DeleteQueueCount() != 1 {
		t.Errorf("delete queue count: got %d, want 1", loaded.DeleteQueueCount())
	}

	// Verify JSON was written.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(data) == 0 {
		t.Error("state file should not be empty")
	}
}

// TestJSONArrayFormat ensures the map-backed sets still serialise as JSON
// arrays, preserving compatibility with state files from older versions.
func TestJSONArrayFormat(t *testing.T) {
	s := NewState("fmt")
	s.MarkVolumeReady("a.7z")
	s.MarkVolumeReady("b.7z")
	s.MarkVolumeDone("c.7z")
	s.MarkFileDone("f.txt")
	s.AddToCrossVolumeProgress("big.bin", "vol_1")
	s.SetCrossVolumeNeeded("big.bin", []string{"vol_1", "vol_2"})
	s.AddToDeleteQueue("c.7z")

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("produced invalid JSON")
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal envelope failed: %v", err)
	}

	for _, key := range []string{"volumes_ready", "volumes_done", "files_done", "delete_queue"} {
		raw, ok := doc[key]
		if !ok {
			t.Fatalf("missing key %s", key)
		}
		if len(raw) == 0 || raw[0] != '[' {
			t.Errorf("key %s must be a JSON array, got %s", key, string(raw))
		}
	}

	cvpRaw, ok := doc["cross_volume_progress"]
	if !ok {
		t.Fatal("missing cross_volume_progress")
	}
	var cvpObj map[string]struct {
		Needed json.RawMessage
		Ready  json.RawMessage
	}
	if err := json.Unmarshal(cvpRaw, &cvpObj); err != nil {
		t.Fatalf("Unmarshal cross_volume_progress failed: %v", err)
	}
	cvp, ok := cvpObj["big.bin"]
	if !ok {
		t.Fatal("missing big.bin entry")
	}
	for name, raw := range map[string]json.RawMessage{"needed": cvp.Needed, "ready": cvp.Ready} {
		if len(raw) == 0 || raw[0] != '[' {
			t.Errorf("cross_volume_progress.big.bin.%s must be an array, got %s", name, string(raw))
		}
	}
}

func TestLoadNonExistent(t *testing.T) {
	_, err := LoadState("/nonexistent/state.json")
	if err == nil {
		t.Fatal("expected error for non-existent state file")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.json")
	os.WriteFile(path, []byte("{bad json"), 0o644)

	_, err := LoadState(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestLoadLegacyStateFile verifies that a state file written in the original
// array format by an older version is still readable.
func TestLoadLegacyStateFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".task_state.json")
	legacy := "{\"task_id\":\"legacy\",\"volumes_ready\":[\"a.7z\"],\"volumes_done\":[\"b.7z\",\"c.7z\"],\"files_done\":[\"x.txt\"],\"cross_volume_progress\":{\"big.bin\":{\"needed\":[\"vol_1\",\"vol_2\"],\"ready\":[\"vol_1\"]}},\"delete_queue\":[\"b.7z\"]}"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if s.TaskID != "legacy" {
		t.Errorf("TaskID: got %q", s.TaskID)
	}
	if !s.IsVolumeReady("a.7z") {
		t.Error("a.7z should be ready")
	}
	if !s.IsVolumeDone("b.7z") || !s.IsVolumeDone("c.7z") {
		t.Error("b.7z and c.7z should be done")
	}
	if !s.IsFileDone("x.txt") {
		t.Error("x.txt should be done")
	}
	cvp := s.CrossVolumeProgress["big.bin"]
	if len(cvp.Needed) != 2 || len(cvp.Ready) != 1 {
		t.Errorf("cross-volume progress: needed=%d ready=%d", len(cvp.Needed), len(cvp.Ready))
	}
	if s.DeleteQueueCount() != 1 {
		t.Errorf("delete queue count: got %d", s.DeleteQueueCount())
	}
}

func TestMarkVolumeReady(t *testing.T) {
	s := NewState("test")
	s.MarkVolumeReady("vol-001.7z")

	if len(s.VolumesReady) != 1 {
		t.Errorf("VolumesReady count: got %d, want 1", len(s.VolumesReady))
	}
	if _, ok := s.VolumesReady["vol-001.7z"]; !ok {
		t.Error("vol-001.7z should be in VolumesReady")
	}

	// Idempotent.
	s.MarkVolumeReady("vol-001.7z")
	if len(s.VolumesReady) != 1 {
		t.Errorf("duplicate MarkVolumeReady must not grow the set, got %d", len(s.VolumesReady))
	}
}

func TestMarkVolumeDone(t *testing.T) {
	s := NewState("test")
	s.MarkVolumeReady("vol-001.7z")
	s.MarkVolumeDone("vol-001.7z")

	// Should be moved from ready to done.
	if len(s.VolumesReady) != 0 {
		t.Errorf("VolumesReady should be empty after done, got %d", len(s.VolumesReady))
	}
	if len(s.VolumesDone) != 1 {
		t.Errorf("VolumesDone count: got %d, want 1", len(s.VolumesDone))
	}
	if !s.IsVolumeDone("vol-001.7z") {
		t.Error("vol-001.7z should be done")
	}
}

func TestMarkFileDone(t *testing.T) {
	s := NewState("test")
	s.MarkFileDone("file1.txt")
	s.MarkFileDone("file2.txt")

	if len(s.FilesDone) != 2 {
		t.Errorf("FilesDone count: got %d, want 2", len(s.FilesDone))
	}
	s.MarkFileDone("file1.txt")
	if len(s.FilesDone) != 2 {
		t.Errorf("duplicate MarkFileDone must not grow the set, got %d", len(s.FilesDone))
	}
}

func TestTakeFromDeleteQueue(t *testing.T) {
	s := NewState("test")
	s.AddToDeleteQueue("a.7z")
	s.AddToDeleteQueue("b.7z")
	s.AddToDeleteQueue("c.7z")

	items := make([]string, 0)
	for {
		item, ok := s.TakeFromDeleteQueue()
		if !ok {
			break
		}
		items = append(items, item)
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// Order should be FIFO.
	expected := []string{"a.7z", "b.7z", "c.7z"}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("items[%d]: got %q, want %q", i, items[i], exp)
		}
	}
}

func TestAddToDeleteQueueDedups(t *testing.T) {
	s := NewState("test")
	s.AddToDeleteQueue("a.7z")
	s.AddToDeleteQueue("a.7z")
	if s.DeleteQueueCount() != 1 {
		t.Errorf("delete queue should dedupe, got %d", s.DeleteQueueCount())
	}
}

func TestTakeFromEmptyDeleteQueue(t *testing.T) {
	s := NewState("test")
	_, ok := s.TakeFromDeleteQueue()
	if ok {
		t.Fatal("expected no item from empty queue")
	}
}

func TestVolumeDoneCount(t *testing.T) {
	s := NewState("test")
	if s.VolumeDoneCount() != 0 {
		t.Errorf("initial count: got %d, want 0", s.VolumeDoneCount())
	}

	s.MarkVolumeDone("v1.7z")
	s.MarkVolumeDone("v2.7z")

	if s.VolumeDoneCount() != 2 {
		t.Errorf("after 2 done: got %d, want 2", s.VolumeDoneCount())
	}
}

func TestVolumeProcessed(t *testing.T) {
	s := NewState("test")
	if s.VolumeProcessed() != 0 {
		t.Errorf("initial processed: got %d, want 0", s.VolumeProcessed())
	}

	s.MarkVolumeReady("v1.7z")
	s.MarkVolumeDone("v2.7z")
	s.MarkVolumeDone("v3.7z")

	if s.VolumeProcessed() != 3 {
		t.Errorf("after 1 ready + 2 done: got %d, want 3", s.VolumeProcessed())
	}

	// Ready volumes move to done without changing the processed count.
	s.MarkVolumeDone("v1.7z")
	if s.VolumeProcessed() != 3 {
		t.Errorf("after moving ready to done: got %d, want 3", s.VolumeProcessed())
	}
}

func TestSetCrossVolumeNeeded(t *testing.T) {
	s := NewState("test")
	s.SetCrossVolumeNeeded("big.bin", []string{"v1.7z", "v2.7z", "v3.7z"})

	cvp := s.CrossVolumeProgress["big.bin"]
	if len(cvp.Needed) == 0 {
		t.Fatal("expected cross volume progress to be set")
	}
	if len(cvp.Needed) != 3 {
		t.Errorf("needed count: got %d, want 3", len(cvp.Needed))
	}
	if _, ok := cvp.Needed["v1.7z"]; !ok {
		t.Error("v1.7z should be in needed")
	}
}

func TestAddToCrossVolumeProgressIdempotent(t *testing.T) {
	s := NewState("test")
	s.AddToCrossVolumeProgress("big.bin", "vol_1")
	s.AddToCrossVolumeProgress("big.bin", "vol_1")
	if len(s.CrossVolumeProgress["big.bin"].Ready) != 1 {
		t.Errorf("Ready should contain 1 fragment, got %d", len(s.CrossVolumeProgress["big.bin"].Ready))
	}
}

func TestStateFilePath(t *testing.T) {
	dir := "/tmp/testdir"
	expected := filepath.Join(dir, ".task_state.json")
	result := StateFilePath(dir)
	if result != expected {
		t.Errorf("StateFilePath: got %q, want %q", result, expected)
	}
}
