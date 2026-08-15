package store

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestTaskLifecycle(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nested", "tasks.json"))
	project, err := s.AddProject("Release")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.AddToProject("Write release notes", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 {
		t.Fatalf("first ID = %d, want 1", first.ID)
	}
	if _, err := s.AddToProject("Ship release", project.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDone(first.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename(first.ID, "Publish release notes"); err != nil {
		t.Fatal(err)
	}

	open, err := s.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].Title != "Ship release" {
		t.Fatalf("unexpected open tasks: %#v", open)
	}
	all, err := s.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || !all[1].Done || all[1].CompletedAt == nil || all[1].Title != "Publish release notes" {
		t.Fatalf("unexpected tasks: %#v", all)
	}
	if err := s.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDone(999, true); err == nil {
		t.Fatal("expected missing-task error")
	}
	if err := s.Rename(999, "missing"); err == nil {
		t.Fatal("expected missing-task rename error")
	}
	if err := s.Rename(2, "  "); err == nil {
		t.Fatal("accepted empty task title")
	}
}

func TestConcurrentAddsDoNotLoseTasks(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "tasks.json"))
	project, err := s.AddProject("Parallel")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.AddToProject("parallel task", project.ID); err != nil {
				t.Errorf("add: %v", err)
			}
		}()
	}
	wg.Wait()
	tasks, err := s.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 20 {
		t.Fatalf("got %d tasks, want 20", len(tasks))
	}
}

func TestParseID(t *testing.T) {
	for _, input := range []string{"0", "-1", "abc"} {
		if _, err := ParseID(input); err == nil {
			t.Errorf("ParseID(%q) succeeded", input)
		}
	}
	if id, err := ParseID("#42"); err != nil || id != 42 {
		t.Fatalf("ParseID: %d, %v", id, err)
	}
}

func TestProjectsOwnTasks(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "tasks.json"))
	project, err := s.AddProject("Learning Go")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.AddToProject("Learn channels", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.ProjectID != project.ID {
		t.Fatalf("ProjectID = %d, want %d", task.ProjectID, project.ID)
	}
	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Name != "Learning Go" {
		t.Fatalf("projects: %#v", projects)
	}
	if _, err := s.AddProject("learning go"); err == nil {
		t.Fatal("duplicate project was accepted")
	}
}

func TestWorkTimer(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "tasks.json"))
	project, err := s.AddProject("Work")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.AddToProject("Timed task", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartWork(task.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].WorkStartedAt == nil {
		t.Fatal("timer did not start")
	}
	if tasks[0].WorkDuration(time.Now()) < 0 {
		t.Fatal("negative duration")
	}
	if err := s.StopWork(task.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err = s.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].WorkStartedAt != nil {
		t.Fatal("timer did not stop")
	}
}

func TestExplicitProgress(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "tasks.json"))
	project, err := s.AddProject("Progress")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.AddToProject("Measured task", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Progress != 0 {
		t.Fatalf("initial progress = %d, want 0", task.Progress)
	}
	if err := s.SetProgress(task.ID, 45); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Progress != 45 || tasks[0].Done {
		t.Fatalf("task after 45%%: %#v", tasks[0])
	}
	if err := s.SetProgress(task.ID, 100); err != nil {
		t.Fatal(err)
	}
	tasks, err = s.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Progress != 100 || !tasks[0].Done {
		t.Fatalf("task after 100%%: %#v", tasks[0])
	}
	if err := s.SetProgress(task.ID, 101); err == nil {
		t.Fatal("accepted progress over 100")
	}
}

func TestTaskState(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "tasks.json"))
	project, err := s.AddProject("States")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.AddToProject("Blocked task", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.State != "today" {
		t.Fatalf("initial state = %q", task.State)
	}
	if err := s.SetState(task.ID, "blocked"); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].State != "blocked" {
		t.Fatalf("state = %q", tasks[0].State)
	}
	if err := s.SetState(task.ID, "later"); err == nil {
		t.Fatal("accepted invalid state")
	}
}
