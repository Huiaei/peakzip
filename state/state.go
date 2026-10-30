// Package state manages task state for resumable decompression,
// persisting progress to a JSON file on disk.
//
// Sets are stored as maps for O(1) lookups, but are serialised as sorted
// JSON arrays so that existing state files written by older versions still
// load correctly.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// CrossVolProgress tracks the readiness of fragments for a cross-volume file.
type CrossVolProgress struct {
	Needed map[string]struct{}
	Ready  map[string]struct{}
}

// TaskState holds the complete state of a decompression task.
type TaskState struct {
	mu                  sync.Mutex
	TaskID              string
	VolumesReady        map[string]struct{}
	VolumesDone         map[string]struct{}
	FilesDone           map[string]struct{}
	CrossVolumeProgress map[string]CrossVolProgress
	DeleteQueue         []string // FIFO, order matters
}

// NewState creates a new empty TaskState with the given task ID.
func NewState(taskID string) *TaskState {
	return &TaskState{
		TaskID:              taskID,
		VolumesReady:        make(map[string]struct{}),
		VolumesDone:         make(map[string]struct{}),
		FilesDone:           make(map[string]struct{}),
		CrossVolumeProgress: make(map[string]CrossVolProgress),
		DeleteQueue:         make([]string, 0),
	}
}

// sortedKeys returns the map keys in ascending order for deterministic output.
func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// MarshalJSON serialises TaskState using the historic array format.
func (s *TaskState) MarshalJSON() ([]byte, error) {
	aux := struct {
		TaskID              string                      `json:"task_id"`
		VolumesReady        []string                    `json:"volumes_ready"`
		VolumesDone         []string                    `json:"volumes_done"`
		FilesDone           []string                    `json:"files_done"`
		CrossVolumeProgress map[string]CrossVolProgress `json:"cross_volume_progress"`
		DeleteQueue         []string                    `json:"delete_queue"`
	}{
		TaskID:              s.TaskID,
		VolumesReady:        sortedKeys(s.VolumesReady),
		VolumesDone:         sortedKeys(s.VolumesDone),
		FilesDone:           sortedKeys(s.FilesDone),
		CrossVolumeProgress: s.CrossVolumeProgress,
		DeleteQueue:         s.DeleteQueue,
	}
	return json.Marshal(aux)
}

// UnmarshalJSON parses TaskState from the historic array format.
func (s *TaskState) UnmarshalJSON(data []byte) error {
	var aux struct {
		TaskID              string                      `json:"task_id"`
		VolumesReady        []string                    `json:"volumes_ready"`
		VolumesDone         []string                    `json:"volumes_done"`
		FilesDone           []string                    `json:"files_done"`
		CrossVolumeProgress map[string]CrossVolProgress `json:"cross_volume_progress"`
		DeleteQueue         []string                    `json:"delete_queue"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	s.TaskID = aux.TaskID
	s.VolumesReady = make(map[string]struct{}, len(aux.VolumesReady))
	for _, v := range aux.VolumesReady {
		s.VolumesReady[v] = struct{}{}
	}
	s.VolumesDone = make(map[string]struct{}, len(aux.VolumesDone))
	for _, v := range aux.VolumesDone {
		s.VolumesDone[v] = struct{}{}
	}
	s.FilesDone = make(map[string]struct{}, len(aux.FilesDone))
	for _, f := range aux.FilesDone {
		s.FilesDone[f] = struct{}{}
	}
	if aux.CrossVolumeProgress != nil {
		s.CrossVolumeProgress = aux.CrossVolumeProgress
	} else {
		s.CrossVolumeProgress = make(map[string]CrossVolProgress)
	}
	if aux.DeleteQueue != nil {
		s.DeleteQueue = aux.DeleteQueue
	} else {
		s.DeleteQueue = make([]string, 0)
	}
	return nil
}

// MarshalJSON serialises CrossVolProgress as sorted arrays.
// Value receiver: map values are not addressable, so a pointer receiver
// would never be picked up when marshalling a map[string]CrossVolProgress.
func (p CrossVolProgress) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Needed []string `json:"needed"`
		Ready  []string `json:"ready"`
	}{
		Needed: sortedKeys(p.Needed),
		Ready:  sortedKeys(p.Ready),
	})
}

// UnmarshalJSON parses CrossVolProgress from arrays.
func (p *CrossVolProgress) UnmarshalJSON(data []byte) error {
	var aux struct {
		Needed []string `json:"needed"`
		Ready  []string `json:"ready"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.Needed = make(map[string]struct{}, len(aux.Needed))
	for _, n := range aux.Needed {
		p.Needed[n] = struct{}{}
	}
	p.Ready = make(map[string]struct{}, len(aux.Ready))
	for _, r := range aux.Ready {
		p.Ready[r] = struct{}{}
	}
	return nil
}

// LoadState reads a task state from the given file path.
// If the file does not exist, an error is returned.
func LoadState(path string) (*TaskState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state file %q: %w", path, err)
	}
	var s TaskState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state file %q: %w", path, err)
	}
	return &s, nil
}

// SaveState writes the task state to the given file path atomically.
func SaveState(s *TaskState, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	// Write to a temp file and rename for atomicity.
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp state file: %w", err)
	}
	return nil
}

// StateFilePath returns the canonical path for the task state file in the given directory.
func StateFilePath(dir string) string {
	return filepath.Join(dir, ".task_state.json")
}

// MarkVolumeReady marks a volume as ready (hash-verified and waiting for extraction).
func (s *TaskState) MarkVolumeReady(volume string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.VolumesReady == nil {
		s.VolumesReady = make(map[string]struct{})
	}
	s.VolumesReady[volume] = struct{}{}
}

// MarkVolumeDone marks a volume as fully processed.
func (s *TaskState) MarkVolumeDone(volume string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.VolumesReady, volume)
	if s.VolumesDone == nil {
		s.VolumesDone = make(map[string]struct{})
	}
	s.VolumesDone[volume] = struct{}{}
}

// MarkFileDone marks a file as fully extracted and verified.
func (s *TaskState) MarkFileDone(file string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FilesDone == nil {
		s.FilesDone = make(map[string]struct{})
	}
	s.FilesDone[file] = struct{}{}
}

// AddToCrossVolumeProgress adds a fragment as ready for a cross-volume file.
func (s *TaskState) AddToCrossVolumeProgress(file, fragment string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.CrossVolumeProgress[file]
	if !ok {
		p = CrossVolProgress{
			Ready: make(map[string]struct{}),
		}
	}
	if p.Ready == nil {
		p.Ready = make(map[string]struct{})
	}
	p.Ready[fragment] = struct{}{}
	s.CrossVolumeProgress[file] = p
}

// SetCrossVolumeNeeded sets the list of fragments needed for a cross-volume file.
func (s *TaskState) SetCrossVolumeNeeded(file string, needed []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.CrossVolumeProgress[file]
	if !ok {
		p = CrossVolProgress{Ready: make(map[string]struct{})}
	}
	m := make(map[string]struct{}, len(needed))
	for _, n := range needed {
		m[n] = struct{}{}
	}
	p.Needed = m
	s.CrossVolumeProgress[file] = p
}

// CrossVolumeFragmentsReady returns how many fragments of a cross-volume
// file have been seen as ready. Reads ts.mu, unlike raw field access.
func (s *TaskState) CrossVolumeFragmentsReady(file string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.CrossVolumeProgress[file].Ready)
}

// CrossVolumeFragmentsNeeded returns how many fragments a cross-volume file
// requires before it can be scheduled.
func (s *TaskState) CrossVolumeFragmentsNeeded(file string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.CrossVolumeProgress[file].Needed)
}

// IsVolumeDone returns true if the volume has been fully processed.
func (s *TaskState) IsVolumeDone(volume string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.VolumesDone[volume]
	return ok
}

// IsVolumeReady returns true if the volume has been hash-verified and queued.
func (s *TaskState) IsVolumeReady(volume string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.VolumesReady[volume]
	return ok
}

// IsFileDone returns true if the file has been fully extracted and verified.
func (s *TaskState) IsFileDone(file string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.FilesDone[file]
	return ok
}

// AddToDeleteQueue adds a volume path to the delete queue.
func (s *TaskState) AddToDeleteQueue(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.DeleteQueue {
		if p == path {
			return
		}
	}
	s.DeleteQueue = append(s.DeleteQueue, path)
}

// DeleteQueueCount returns the number of items in the delete queue.
func (s *TaskState) DeleteQueueCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.DeleteQueue)
}

// TakeFromDeleteQueue removes and returns the first item from the delete queue.
func (s *TaskState) TakeFromDeleteQueue() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.DeleteQueue) == 0 {
		return "", false
	}
	path := s.DeleteQueue[0]
	s.DeleteQueue = s.DeleteQueue[1:]
	return path, true
}

// VolumeDoneCount returns the number of fully processed volumes.
func (s *TaskState) VolumeDoneCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.VolumesDone)
}

// VolumeProcessed returns the number of volumes that have been handled
// in some way (ready or done). This is NOT the total volume count of the
// task; use the manifest for that.
func (s *TaskState) VolumeProcessed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.VolumesDone) + len(s.VolumesReady)
}
