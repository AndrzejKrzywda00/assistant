package tui

import (
	"fmt"
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

func TestFocusPresentsTaskContentInKeyOrder(t *testing.T) {
	task := store.Task{
		ID:      1,
		Title:   "Document API",
		Content: map[string]string{"owner": "Ada", "link": "https://example.com"},
	}
	a := &app{tasks: []store.Task{task}, focusTaskID: task.ID}
	plain := stripANSI(strings.Join(a.responsiveFocusRows(20, 60, time.Now()), "\n"))
	link := strings.Index(plain, "link: https://example.com")
	owner := strings.Index(plain, "owner: Ada")
	if link < 0 || owner < 0 {
		t.Fatalf("focus content missing:\n%s", plain)
	}
	if link > owner {
		t.Fatalf("focus content is not sorted by key:\n%s", plain)
	}
}

func TestFocusPresentsTaskPriority(t *testing.T) {
	task := store.Task{ID: 1, Title: "Urgent work", Priority: "P0"}
	a := &app{tasks: []store.Task{task}, focusTaskID: task.ID}
	plain := stripANSI(strings.Join(a.responsiveFocusRows(20, 60, time.Now()), "\n"))
	if !strings.Contains(plain, "PRIORITY  P0") {
		t.Fatalf("focus priority missing:\n%s", plain)
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

func TestSpaceCountsAreScopedToActiveProject(t *testing.T) {
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
	if a.spaceCounts != [3]int{1, 0, 0} {
		t.Fatalf("space counts = %v", a.spaceCounts)
	}
	a.project = 1
	if err := a.refresh(); err != nil {
		t.Fatal(err)
	}
	if a.spaceCounts != [3]int{0, 1, 0} {
		t.Fatalf("space counts did not follow active project: %v", a.spaceCounts)
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

func TestBlockedAndWaitingUseSingleSection(t *testing.T) {
	for _, tc := range []struct {
		space int
		title string
	}{{1, "BLOCKED (0)"}, {2, "WAITING (0)"}} {
		a := &app{space: tc.space}
		plain := stripANSI(strings.Join(a.mainPaneRows(8, 50, time.Now()), "\n"))
		if !strings.Contains(plain, tc.title) {
			t.Fatalf("space %d missing %q:\n%s", tc.space, tc.title, plain)
		}
		if strings.Contains(plain, "PENDING") || strings.Contains(plain, "DONE") {
			t.Fatalf("space %d still has Today sections:\n%s", tc.space, plain)
		}
	}
}

func TestOutlineSeparatesProjectTitleFromPendingSection(t *testing.T) {
	a := &app{}
	rows := a.mainPaneRows(8, 50, time.Now())
	if got := strings.TrimSpace(stripANSI(rows[1])); got != "" {
		t.Fatalf("row after project title = %q, want blank", got)
	}
	if got := stripANSI(rows[2]); !strings.Contains(got, "PENDING (0)") {
		t.Fatalf("next row = %q, want pending section", got)
	}
}

func TestUnicodeStrikePreservesTerminalWidth(t *testing.T) {
	got := unicodeStrike("done task  ")
	if got != "d̶o̶n̶e̶ ̶t̶a̶s̶k̶  " {
		t.Fatalf("unicodeStrike() = %q", got)
	}
	if strings.Count(got, "\u0336") != 9 {
		t.Fatalf("unicodeStrike() added the wrong number of overlays: %q", got)
	}
}

func TestProjectNameAlwaysWrapsInFull(t *testing.T) {
	projectName := "A project name long enough to wrap"
	a := &app{
		projects:     []store.Project{{Name: projectName}},
		sidebar:      3,
		projectFocus: true,
	}
	expanded := stripANSI(strings.Join(a.sidebarPaneRows(20, 14), "\n"))
	for _, word := range strings.Fields(projectName) {
		if !strings.Contains(expanded, word) {
			t.Fatalf("selected project omitted %q:\n%s", word, expanded)
		}
	}

	a.projectFocus = false
	unselected := stripANSI(strings.Join(a.sidebarPaneRows(20, 14), "\n"))
	for _, word := range strings.Fields(projectName) {
		if !strings.Contains(unselected, word) {
			t.Fatalf("unselected project omitted %q:\n%s", word, unselected)
		}
	}
}

func TestTaskTitleAlwaysWrapsInFull(t *testing.T) {
	title := "A task title long enough to wrap across several rows"
	a := &app{
		projects: []store.Project{{ID: 1, Name: "Project"}},
		tasks:    []store.Task{{ID: 1, ProjectID: 1, Title: title, State: "today"}},
	}
	rendered := stripANSI(strings.Join(a.mainPaneRows(20, 18, time.Now()), "\n"))
	for _, word := range strings.Fields(title) {
		if !strings.Contains(rendered, word) {
			t.Fatalf("wrapped task omitted %q:\n%s", word, rendered)
		}
	}
}

func TestOverflowingTaskListShowsScrollbar(t *testing.T) {
	tasks := make([]store.Task, 12)
	for i := range tasks {
		tasks[i] = store.Task{ID: i + 1, ProjectID: 1, Title: fmt.Sprintf("Task %d", i+1), State: "today"}
	}
	a := &app{
		projects: []store.Project{{ID: 1, Name: "Project"}},
		tasks:    tasks,
		current:  len(tasks) - 1,
	}
	rows := a.mainPaneRows(7, 24, time.Now())
	if a.taskScroll == 0 {
		t.Fatal("selecting the last task did not scroll the viewport")
	}
	thumbs := 0
	for _, row := range rows[1:] {
		plain := []rune(stripANSI(row))
		if len(plain) != 24 {
			t.Fatalf("scrollbar row width = %d, want 24: %q", len(plain), string(plain))
		}
		if plain[len(plain)-1] == '┃' {
			thumbs++
		} else if plain[len(plain)-1] != '│' {
			t.Fatalf("rightmost cell = %q, want scrollbar", plain[len(plain)-1])
		}
	}
	if thumbs == 0 {
		t.Fatal("scrollbar thumb is missing")
	}
}

func TestShortTaskListDoesNotShowScrollbar(t *testing.T) {
	a := &app{
		projects: []store.Project{{ID: 1, Name: "Project"}},
		tasks:    []store.Task{{ID: 1, ProjectID: 1, Title: "Task", State: "today"}},
	}
	rows := a.mainPaneRows(10, 24, time.Now())
	for _, row := range rows[1:] {
		last := []rune(stripANSI(row))[23]
		if last == '│' || last == '┃' {
			t.Fatalf("non-overflowing list unexpectedly shows a scrollbar: %q", stripANSI(row))
		}
	}
}

func TestProjectsHaveVerticalSpacing(t *testing.T) {
	a := &app{
		projects: []store.Project{{Name: "First"}, {Name: "Second"}},
	}
	rows := a.sidebarPaneRows(20, 18)
	firstRow, secondRow := -1, -1
	for i, row := range rows {
		plain := strings.TrimSpace(stripANSI(row))
		if strings.HasSuffix(plain, "First") {
			firstRow = i
		}
		if strings.HasSuffix(plain, "Second") {
			secondRow = i
		}
	}
	if firstRow < 0 || secondRow < 0 {
		t.Fatalf("projects missing from sidebar:\n%s", stripANSI(strings.Join(rows, "\n")))
	}
	if secondRow != firstRow+2 {
		t.Fatalf("project rows = %d and %d, want one blank row between them", firstRow, secondRow)
	}
	if got := strings.TrimSpace(stripANSI(rows[firstRow+1])); got != "" {
		t.Fatalf("row between projects = %q, want blank", got)
	}
}

func TestActiveProjectIsUnderlined(t *testing.T) {
	a := &app{
		projects:     []store.Project{{Name: "Personal OS"}, {Name: "Release"}},
		project:      1,
		sidebar:      0,
		projectFocus: true,
	}
	rendered := strings.Join(a.sidebarPaneRows(20, 18), "\n")
	rows := stripANSI(rendered)
	if !strings.Contains(rows, "Release") || !strings.Contains(rendered, "\x1b[4mRelease\x1b[24m") {
		t.Fatalf("active project underline missing:\n%q", rendered)
	}
	if strings.Contains(rows, "┌") || strings.Contains(rows, "└") {
		t.Fatalf("active project still has a frame:\n%s", rows)
	}
}

func TestProjectCursorMatchesSpaceCursorStyle(t *testing.T) {
	a := &app{
		projects:     []store.Project{{Name: "First"}, {Name: "Second"}},
		project:      0,
		sidebar:      4,
		projectFocus: true,
	}
	rendered := strings.Join(a.sidebarPaneRows(20, 18), "\n")
	if !strings.Contains(stripANSI(rendered), "▸ Second") {
		t.Fatalf("project cursor arrow missing:\n%q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[48;5;22m") {
		t.Fatalf("project cursor highlight missing:\n%q", rendered)
	}
}

func TestOnlyActivePaneHeaderIsBold(t *testing.T) {
	header := "Project: Personal OS"
	if got := boldPaneHeader(header, false); got != header {
		t.Fatalf("inactive header changed: %q", got)
	}
	got := boldPaneHeader(header, true)
	if got != "\x1b[1m"+header+"\x1b[22m" {
		t.Fatalf("active header is not bold: %q", got)
	}
	if stripANSI(got) != header {
		t.Fatalf("boldness changed header content: %q", stripANSI(got))
	}
}

func TestBrowsingProjectsDoesNotActivateUntilSelection(t *testing.T) {
	a := &app{
		projects: []store.Project{{Name: "First"}, {Name: "Second"}},
		project:  0,
		sidebar:  3,
		space:    2,
	}
	a.moveSidebar(1)
	if a.sidebar != 4 || a.project != 0 {
		t.Fatalf("browsing changed active project: sidebar=%d project=%d", a.sidebar, a.project)
	}
	a.projectFocus = true
	a.activateSidebarSelection()
	if a.project != 1 {
		t.Fatalf("selecting did not activate project: %d", a.project)
	}
	if a.space != 0 {
		t.Fatalf("selecting project opened space %d, want Today", a.space)
	}
	if !a.projectFocus {
		t.Fatal("selecting a project left the Projects sidebar")
	}
}

func TestProjectFooterDoesNotAdvertiseFocus(t *testing.T) {
	a := &app{sidebar: 3}
	footer := stripANSI(a.footerForWidth(100))
	if strings.Contains(strings.ToLower(footer), "focus") {
		t.Fatalf("project footer still advertises focus: %q", footer)
	}

	a.sidebar = 0
	footer = stripANSI(a.footerForWidth(100))
	if !strings.Contains(strings.ToLower(footer), "focus") {
		t.Fatalf("task footer does not advertise focus: %q", footer)
	}
}

func TestFocusAvailabilityMatchesSidebarSelection(t *testing.T) {
	a := &app{}
	for sidebar := 0; sidebar < 3; sidebar++ {
		a.sidebar = sidebar
		if !a.focusAvailable() {
			t.Fatalf("focus unavailable for space %d", sidebar)
		}
	}
	for sidebar := 3; sidebar < 6; sidebar++ {
		a.sidebar = sidebar
		if a.focusAvailable() {
			t.Fatalf("focus available for project %d", sidebar)
		}
	}
	a.sidebar = 0
	a.projectFocus = true
	if a.focusAvailable() {
		t.Fatal("focus available while Projects pane is selected")
	}
}

func TestProjectTopBarDoesNotAdvertiseFocus(t *testing.T) {
	a := &app{sidebar: 3}
	if top := stripANSI(a.topBar()); strings.Contains(strings.ToLower(top), "focus") {
		t.Fatalf("project top bar still advertises focus: %q", top)
	}
	a.sidebar = 0
	a.projectFocus = true
	if top := stripANSI(a.topBar()); strings.Contains(strings.ToLower(top), "focus") {
		t.Fatalf("Projects-pane top bar still advertises focus: %q", top)
	}
	a.projectFocus = false
	if top := stripANSI(a.topBar()); !strings.Contains(strings.ToLower(top), "focus") {
		t.Fatalf("space top bar does not advertise focus: %q", top)
	}
}

func TestTopBarPaddingOverwritesPreviousFrame(t *testing.T) {
	a := &app{sidebar: 0}
	withFocus := padVisible(a.topBar(), 60)
	a.projectFocus = true
	withoutFocus := padVisible(a.topBar(), 60)
	if got := len([]rune(stripANSI(withFocus))); got != 60 {
		t.Fatalf("space top bar width = %d, want 60", got)
	}
	if got := len([]rune(stripANSI(withoutFocus))); got != 60 {
		t.Fatalf("project top bar width = %d, want 60", got)
	}
	if strings.Contains(strings.ToLower(stripANSI(withoutFocus)), "focus") {
		t.Fatalf("padded project top bar retained focus: %q", stripANSI(withoutFocus))
	}
}

func TestBlockedAndDeleteControlsAreUnambiguous(t *testing.T) {
	a := &app{sidebar: 0}
	footer := a.footerForWidth(100)
	plain := stripANSI(footer)
	if !strings.Contains(plain, "b blocked") || strings.Contains(plain, "m move") {
		t.Fatalf("unexpected task controls: %q", plain)
	}
	if !strings.Contains(footer, "\x1b[31md") {
		t.Fatalf("delete control is not red: %q", footer)
	}
	prompt := deletePrompt("Delete task?")
	if !strings.HasPrefix(prompt, "\x1b[31m") {
		t.Fatalf("delete prompt is not red: %q", prompt)
	}
	projectPrompt := deleteProjectPrompt("Personal OS")
	if !strings.Contains(projectPrompt, "\x1b[97m\"Personal OS\"\x1b[31m") {
		t.Fatalf("quoted project name is not white: %q", projectPrompt)
	}
}
