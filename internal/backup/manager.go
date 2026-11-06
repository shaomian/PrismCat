package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrBusy = errors.New("backup_busy")
var ErrNotFound = errors.New("backup_not_found")

type Job struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	Progress  Progress  `json:"progress"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
	Error     string    `json:"error,omitempty"`
}

type Manager struct {
	mu           sync.Mutex
	source       Source
	dir          string
	active       *Job
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	closed       bool
	terminal     map[string]Job
	importJob    *importRecord
	importing    bool
	importCancel context.CancelFunc
}

func NewManager(source Source, directory string) *Manager {
	return &Manager{source: source, dir: directory, terminal: map[string]Job{}}
}

func (m *Manager) Create() (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.active != nil || m.importing {
		return Job{}, ErrBusy
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return Job{}, err
	}
	job := Job{ID: uuid.NewString(), State: "running", CreatedAt: time.Now().UTC(), Progress: Progress{Phase: "snapshot"}}
	if err := writeJSON(m.metadata(job.ID), job); err != nil {
		return Job{}, err
	}
	m.active = &job
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		_, err := Create(ctx, m.source, m.filename(job.ID), job.ID, "online", func(progress Progress) {
			m.mu.Lock()
			m.active.Progress = progress
			m.mu.Unlock()
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		finished := *m.active
		finished.State = "completed"
		if err != nil {
			finished.State = "failed"
			finished.Error = failureCode(err)
		} else if info, statErr := os.Stat(m.filename(job.ID)); statErr == nil {
			finished.Size = info.Size()
		}
		if writeErr := writeJSON(m.metadata(job.ID), finished); writeErr != nil {
			// Keep terminal state available in memory until the next attempt.
			finished.State = "failed"
			finished.Error = "backup_status_save_failed"
			_ = os.Remove(m.filename(job.ID))
			m.terminal[job.ID] = finished
		}
		m.active = nil
		m.cancel = nil
	}()
	return job, nil
}

func (m *Manager) List() ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := os.ReadDir(m.dir)
	if os.IsNotExist(err) {
		return []Job{}, nil
	}
	if err != nil {
		return nil, err
	}
	jobs := []Job{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		job, err := m.getLocked(id)
		if err != nil {
			continue
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	return jobs, nil
}

func (m *Manager) getLocked(id string) (Job, error) {
	if _, err := uuid.Parse(id); err != nil || strings.ToLower(id) != id || len(id) != 36 {
		return Job{}, ErrNotFound
	}
	if m.active != nil && m.active.ID == id {
		return *m.active, nil
	}
	if job, ok := m.terminal[id]; ok {
		return job, nil
	}
	file, err := os.Open(m.metadata(id))
	if err != nil {
		return Job{}, ErrNotFound
	}
	defer file.Close()
	var job Job
	if err = json.NewDecoder(file).Decode(&job); err != nil || job.ID != id {
		return Job{}, ErrNotFound
	}
	if job.State == "running" {
		job.State = "failed"
		job.Error = "backup_interrupted"
		// Only reclaim directories belonging to this persisted interrupted job.
		// Offline exports and other instances may also use the backup directory.
		if names, err := filepath.Glob(filepath.Join(m.dir, ".prismcat-backup-"+id+"-*")); err == nil {
			for _, name := range names {
				_ = os.RemoveAll(name)
			}
		}
	}
	return job, nil
}

func (m *Manager) Get(id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getLocked(id)
}
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, err := m.getLocked(id)
	if err != nil {
		return err
	}
	if job.State == "running" {
		return ErrBusy
	}
	if err = os.Remove(m.filename(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = os.Remove(m.metadata(id)); err == nil {
		delete(m.terminal, id)
	}
	return err
}
func (m *Manager) Open(id string) (*os.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, err := m.getLocked(id)
	if err != nil {
		return nil, err
	}
	if job.State != "completed" {
		return nil, ErrBusy
	}
	info, err := os.Lstat(m.filename(id))
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrNotFound
	}
	return os.Open(m.filename(id))
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	if m.importCancel != nil {
		m.importCancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}
func (m *Manager) filename(id string) string {
	return filepath.Join(m.dir, "prismcat-backup-"+id+".zip")
}
func (m *Manager) metadata(id string) string { return filepath.Join(m.dir, id+".json") }

func failureCode(err error) string {
	if errors.Is(err, context.Canceled) {
		return "backup_interrupted"
	}
	message := err.Error()
	if strings.Contains(message, "missing body") {
		return "backup_body_missing"
	}
	if strings.Contains(message, "body hash mismatch") {
		return "backup_body_corrupt"
	}
	if strings.Contains(message, "insufficient disk space") || strings.Contains(message, "no space left") {
		return "backup_space"
	}
	return "backup_failed"
}
