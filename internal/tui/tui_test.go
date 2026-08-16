package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrzejKrzywda00/assistant/internal/store"
)

func TestResponsiveCellsHaveRequestedWidth(t *testing.T) {
	for _, width := range []int{8, 20, 52} {
		if got := len([]rune(fitCell("a title that may be truncated", width))); got != width {
			t.Fatalf("fitCell width = %d, want %d", got, width)
		}
		if got := len([]rune(stripANSI(responsiveFocusBar(width, 50)))); got != width {
			t.Fatalf("focus bar width = %d, want %d", got, width)
		}
	}
}

func TestIntegerLayoutHelpers(t *testing.T) {
	if got := clampInt(100, 16, 28); got != 28 {
		t.Fatalf("clamp high = %d", got)
	}
	if got := clampInt(4, 16, 28); got != 16 {
		t.Fatalf("clamp low = %d", got)
	}
	if got := minInt(4, 9); got != 4 {
		t.Fatalf("min = %d", got)
	}
	if got := maxInt(4, 9); got != 9 {
		t.Fatalf("max = %d", got)
	}
}

func TestSpaceCountsSpanAllProjects(t *testing.T) {
	s := store.New(filepath.Join(t.TempDir(), "tasks.json"))
	first, err := s.AddProject("First")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AddProject("Second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToProject("Today", first.ID); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.AddToProject("Blocked", second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(blocked.ID, "blocked"); err != nil {
		t.Fatal(err)
	}

	a := &app{store: s, project: 0}
	if err := a.refresh(); err != nil {
		t.Fatal(err)
	}
	if a.spaceCounts != [3]int{1, 1, 0} {
		t.Fatalf("space counts = %v", a.spaceCounts)
	}
	a.project = 1
	if err := a.refresh(); err != nil {
		t.Fatal(err)
	}
	if a.spaceCounts != [3]int{1, 1, 0} {
		t.Fatalf("counts changed with project: %v", a.spaceCounts)
	}
}

func TestEmptyOutlineShowsBothSectionsAndAddPrompt(t *testing.T) {
	a := &app{}
	rows := a.mainPaneRows(8, 50, time.Now())
	plain := stripANSI(strings.Join(rows, "\n"))
	for _, expected := range []string{"PENDING (0)", "press a to add one", "DONE (0)"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("outline missing %q:\n%s", expected, plain)
		}
	}
}
