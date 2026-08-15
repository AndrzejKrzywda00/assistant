package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Task struct {
	ID              int        `json:"id"`
	ProjectID       int        `json:"project_id"`
	Title           string     `json:"title"`
	Done            bool       `json:"done"`
	CreatedAt       time.Time  `json:"created_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	WorkStartedAt   *time.Time `json:"work_started_at,omitempty"`
	CapturedSeconds int64      `json:"captured_seconds,omitempty"`
	Progress        int        `json:"progress"`
	State           string     `json:"state"`
}

type Project struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type data struct {
	Version       int       `json:"version"`
	NextID        int       `json:"next_id"`
	Tasks         []Task    `json:"tasks"`
	NextProjectID int       `json:"next_project_id"`
	Projects      []Project `json:"projects"`
}

type Store struct{ path string }

func New(path string) *Store { return &Store{path: path} }

func NewDefault() (*Store, error) {
	if path := os.Getenv("ASSISTANT_DATA_PATH"); path != "" {
		return New(path), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("locate user config directory: %w", err)
	}
	return New(filepath.Join(dir, "assistant", "tasks.json")), nil
}

func (s *Store) Path() string { return s.path }

func ParseID(value string) (int, error) {
	id, err := strconv.Atoi(strings.TrimPrefix(value, "#"))
	if err != nil || id < 1 {
		return 0, fmt.Errorf("invalid task id %q", value)
	}
	return id, nil
}

func (s *Store) List(includeDone bool) ([]Task, error) {
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	result := make([]Task, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		if includeDone || !t.Done {
			result = append(result, t)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Done != result[j].Done {
			return !result[i].Done
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (s *Store) Projects() ([]Project, error) {
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	return append([]Project(nil), d.Projects...), nil
}

func (s *Store) AddProject(name string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, errors.New("project name cannot be empty")
	}
	var project Project
	err := s.withLock(func() error {
		d, err := s.load()
		if err != nil {
			return err
		}
		for _, existing := range d.Projects {
			if strings.EqualFold(existing.Name, name) {
				return fmt.Errorf("project %q already exists", name)
			}
		}
		project = Project{ID: d.NextProjectID, Name: name, CreatedAt: time.Now().UTC()}
		d.NextProjectID++
		d.Projects = append(d.Projects, project)
		return s.save(d)
	})
	return project, err
}

func (s *Store) Add(title string) (Task, error) {
	return Task{}, errors.New("a project is required; use AddToProject")
}

func (s *Store) AddToProject(title string, projectID int) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, errors.New("task title cannot be empty")
	}
	var t Task
	err := s.withLock(func() error {
		d, err := s.load()
		if err != nil {
			return err
		}
		found := false
		for _, p := range d.Projects {
			if p.ID == projectID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("project #%d not found", projectID)
		}
		t = Task{ID: d.NextID, ProjectID: projectID, Title: title, State: "today", CreatedAt: time.Now().UTC()}
		d.NextID++
		d.Tasks = append(d.Tasks, t)
		return s.save(d)
	})
	return t, err
}

func (s *Store) SetState(id int, state string) error {
	state = strings.ToLower(strings.TrimSpace(state))
	if state != "today" && state != "blocked" && state != "waiting" {
		return errors.New("state must be today, blocked, or waiting")
	}
	return s.withLock(func() error {
		d, err := s.load()
		if err != nil {
			return err
		}
		for i := range d.Tasks {
			if d.Tasks[i].ID == id {
				d.Tasks[i].State = state
				return s.save(d)
			}
		}
		return fmt.Errorf("task #%d not found", id)
	})
}

func (s *Store) SetDone(id int, done bool) error {
	return s.withLock(func() error {
		d, err := s.load()
		if err != nil {
			return err
		}
		for i := range d.Tasks {
			if d.Tasks[i].ID == id {
				d.Tasks[i].Done = done
				if done {
					d.Tasks[i].Progress = 100
				} else {
					d.Tasks[i].Progress = 0
				}
				if done {
					now := time.Now().UTC()
					d.Tasks[i].CompletedAt = &now
				} else {
					d.Tasks[i].CompletedAt = nil
				}
				return s.save(d)
			}
		}
		return fmt.Errorf("task #%d not found", id)
	})
}

func (s *Store) SetProgress(id, progress int) error {
	if progress < 0 || progress > 100 {
		return errors.New("progress must be between 0 and 100")
	}
	return s.withLock(func() error {
		d, err := s.load()
		if err != nil {
			return err
		}
		for i := range d.Tasks {
			if d.Tasks[i].ID == id {
				d.Tasks[i].Progress = progress
				d.Tasks[i].Done = progress == 100
				if d.Tasks[i].Done {
					now := time.Now().UTC()
					d.Tasks[i].CompletedAt = &now
				} else {
					d.Tasks[i].CompletedAt = nil
				}
				return s.save(d)
			}
		}
		return fmt.Errorf("task #%d not found", id)
	})
}

func (s *Store) Rename(id int, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("task title cannot be empty")
	}
	return s.withLock(func() error {
		d, err := s.load()
		if err != nil {
			return err
		}
		for i := range d.Tasks {
			if d.Tasks[i].ID == id {
				d.Tasks[i].Title = title
				return s.save(d)
			}
		}
		return fmt.Errorf("task #%d not found", id)
	})
}

func (s *Store) StartWork(id int) error {
	return s.withLock(func() error {
		d, err := s.load()
		if err != nil {
			return err
		}
		for i := range d.Tasks {
			if d.Tasks[i].ID == id {
				if d.Tasks[i].WorkStartedAt == nil {
					now := time.Now().UTC()
					d.Tasks[i].WorkStartedAt = &now
					return s.save(d)
				}
				return nil
			}
		}
		return fmt.Errorf("task #%d not found", id)
	})
}

func (s *Store) StopWork(id int) error {
	return s.withLock(func() error {
		d, err := s.load()
		if err != nil {
			return err
		}
		for i := range d.Tasks {
			if d.Tasks[i].ID == id {
				if d.Tasks[i].WorkStartedAt != nil {
					elapsed := time.Since(*d.Tasks[i].WorkStartedAt).Seconds()
					if elapsed > 0 {
						d.Tasks[i].CapturedSeconds += int64(elapsed)
					}
					d.Tasks[i].WorkStartedAt = nil
					return s.save(d)
				}
				return nil
			}
		}
		return fmt.Errorf("task #%d not found", id)
	})
}

func (t Task) WorkDuration(now time.Time) time.Duration {
	duration := time.Duration(t.CapturedSeconds) * time.Second
	if t.WorkStartedAt != nil {
		duration += now.Sub(*t.WorkStartedAt)
	}
	if duration < 0 {
		return 0
	}
	return duration
}

func (s *Store) Delete(id int) error {
	return s.withLock(func() error {
		d, err := s.load()
		if err != nil {
			return err
		}
		for i := range d.Tasks {
			if d.Tasks[i].ID == id {
				d.Tasks = append(d.Tasks[:i], d.Tasks[i+1:]...)
				return s.save(d)
			}
		}
		return fmt.Errorf("task #%d not found", id)
	})
}

// withLock serializes updates made by the TUI, scripts, and parallel coding agents.
func (s *Store) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open data lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock data: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func (s *Store) load() (data, error) {
	d := defaultData()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("read tasks: %w", err)
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return d, fmt.Errorf("decode %s: %w", s.path, err)
	}
	if len(d.Projects) == 0 {
		// Legacy task files predate projects. Preserve their tasks under a real,
		// persisted migration project; new databases remain project-free.
		if len(d.Tasks) > 0 {
			d.Projects = []Project{{ID: 1, Name: "Imported", CreatedAt: time.Now().UTC()}}
			d.NextProjectID = 2
		}
	}
	if d.NextProjectID < 1 {
		d.NextProjectID = 1
		for _, p := range d.Projects {
			if p.ID >= d.NextProjectID {
				d.NextProjectID = p.ID + 1
			}
		}
	}
	for i := range d.Tasks {
		if d.Tasks[i].ProjectID == 0 && len(d.Projects) > 0 {
			d.Tasks[i].ProjectID = d.Projects[0].ID
		}
		if d.Tasks[i].Done && d.Tasks[i].Progress == 0 {
			d.Tasks[i].Progress = 100
		}
		if d.Tasks[i].State == "" {
			d.Tasks[i].State = "today"
		}
	}
	if d.NextID < 1 {
		d.NextID = 1
		for _, t := range d.Tasks {
			if t.ID >= d.NextID {
				d.NextID = t.ID + 1
			}
		}
	}
	return d, nil
}

func defaultData() data {
	return data{Version: 2, NextID: 1, Tasks: []Task{}, NextProjectID: 1, Projects: []Project{}}
}

func (s *Store) save(d data) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tasks: %w", err)
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".tasks-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary data file: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace data file: %w", err)
	}
	ok = true
	return nil
}
