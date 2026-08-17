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
	if got != "d̶o̶n̶e̶ t̶a̶s̶k̶  " {
		t.Fatalf("unicodeStrike() = %q", got)
	}
	if strings.Count(got, "\u0336") != 8 {
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
