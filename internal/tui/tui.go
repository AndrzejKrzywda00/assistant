package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/AndrzejKrzywda00/assistant/internal/auth"
	"github.com/AndrzejKrzywda00/assistant/internal/store"
)

var errQuit = fmt.Errorf("quit")

const (
	background = "\x1b[48;5;234m\x1b[38;5;252m"
	resetBG    = "\x1b[0m" + background
)

type app struct {
	store        *store.Store
	in           *os.File
	out          io.Writer
	reader       *bufio.Reader
	tasks        []store.Task
	projects     []store.Project
	project      int
	current      int
	status       string
	keys         chan keyResult
	projectFocus bool
	focusTaskID  int
	sidebar      int
	view         int
	viewCounts   [3]int
}

type keyResult struct {
	key string
	err error
}

func Run(s *store.Store, in *os.File, out io.Writer) error {
	if !isTerminal(in.Fd()) {
		return fmt.Errorf("interactive mode needs a terminal; use 'assistant help' for scriptable commands")
	}
	state, err := makeRaw(in.Fd())
	if err != nil {
		return fmt.Errorf("enable keyboard mode: %w", err)
	}
	defer restore(in.Fd(), state)

	a := &app{store: s, in: in, out: out, reader: bufio.NewReader(in)}
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l"+background+"\x1b[2J")
	defer fmt.Fprint(out, "\x1b[?25h\x1b[?1049l")
	if err := a.login(); err == errQuit {
		return nil
	} else if err != nil {
		return err
	}
	a.keys = make(chan keyResult)
	go func() {
		for {
			key, err := a.readKey()
			a.keys <- keyResult{key, err}
			if err != nil {
				return
			}
		}
	}()
	return a.loop()
}

func (a *app) login() error {
	m := auth.New(a.store.Path())
	exists, err := m.Exists()
	if err != nil {
		return err
	}
	if !exists {
		name, quit, err := a.nameScreen()
		if err != nil {
			return err
		}
		if quit {
			return errQuit
		}
		greeting := name + ", let's get productive today"
		first, quit, err := a.pinScreen("Create a 4-digit PIN", greeting)
		if err != nil {
			return err
		}
		if quit {
			return errQuit
		}
		second, quit, err := a.pinScreen("Confirm your new PIN", greeting)
		if err != nil {
			return err
		}
		if quit {
			return errQuit
		}
		if first != second {
			a.status = "PINs did not match — try again"
			return a.login()
		}
		return m.SetProfile(first, name)
	}
	name, err := m.Name()
	if err != nil {
		return err
	}
	for {
		greeting := "Let's get productive today"
		if name != "" {
			greeting = name + ", let's get productive today"
		}
		pin, quit, err := a.pinScreen("Enter your 4-digit PIN", greeting)
		if err != nil {
			return err
		}
		if quit {
			return errQuit
		}
		ok, err := m.Verify(pin)
		if err != nil {
			return err
		}
		if ok {
			a.status = ""
			if name == "" {
				name, quit, err = a.nameScreen()
				if err != nil {
					return err
				}
				if quit {
					return errQuit
				}
				if err := m.SetName(name); err != nil {
					return err
				}
			}
			return nil
		}
		a.status = "Incorrect PIN — try again"
	}
}

func (a *app) nameScreen() (string, bool, error) {
	var value []rune
	for {
		rows, cols := terminalSize(a.in.Fd())
		width, height := 62, 15
		top, left := (rows-height)/2+1, (cols-width)/2+1
		if top < 1 {
			top = 1
		}
		if left < 1 {
			left = 1
		}
		target := a.out
		var frame bytes.Buffer
		a.out = &frame
		fmt.Fprint(a.out, background+"\x1b[H")
		a.at(top, left, "╭"+strings.Repeat("─", width-2)+"╮")
		for row := 1; row < height-1; row++ {
			a.at(top+row, left, "│"+strings.Repeat(" ", width-2)+"│")
		}
		a.at(top+height-1, left, "╰"+strings.Repeat("─", width-2)+"╯")
		a.centerAt(top+3, cols, "\x1b[1;38;5;48mA S S I S T A N T"+resetBG)
		a.centerAt(top+6, cols, "\x1b[1;38;5;255mWhat should I call you?"+resetBG)
		shown := string(value)
		if shown == "" {
			shown = "Your name"
		}
		a.centerAt(top+9, cols, "\x1b[4m  "+shown+"  "+resetBG)
		a.centerAt(top+12, cols, "\x1b[2mType your name  ·  Enter to continue  ·  Esc to quit"+resetBG)
		a.out = target
		_, _ = target.Write(frame.Bytes())

		key, err := a.readKey()
		if err != nil {
			return "", false, err
		}
		switch key {
		case "escape", "ctrl-c":
			return "", true, nil
		case "enter":
			name := strings.TrimSpace(string(value))
			if name != "" {
				return name, false, nil
			}
		case "backspace":
			if len(value) > 0 {
				value = value[:len(value)-1]
			}
		default:
			runes := []rune(key)
			if len(runes) == 1 && runes[0] >= 32 && len(value) < 32 {
				value = append(value, runes[0])
			}
		}
	}
}

func (a *app) pinScreen(hint string, subtitle string) (string, bool, error) {
	digits := ""
	for len(digits) < 4 {
		rows, cols := terminalSize(a.in.Fd())
		width, height := 62, 17
		if cols < width+4 {
			width = cols - 4
		}
		if width < 40 {
			width = 40
		}
		top, left := (rows-height)/2+1, (cols-width)/2+1
		if top < 1 {
			top = 1
		}
		if left < 1 {
			left = 1
		}
		target := a.out
		var frame bytes.Buffer
		a.out = &frame
		fmt.Fprint(a.out, background+"\x1b[H")
		a.at(top, left, "╭"+strings.Repeat("─", width-2)+"╮")
		for row := 1; row < height-1; row++ {
			a.at(top+row, left, "│"+strings.Repeat(" ", width-2)+"│")
		}
		a.at(top+height-1, left, "╰"+strings.Repeat("─", width-2)+"╯")
		a.centerAt(top+3, cols, "\x1b[1;38;5;48mA S S I S T A N T"+resetBG)
		a.centerAt(top+6, cols, "\x1b[1;38;5;255m"+subtitle+resetBG)
		a.centerAt(top+8, cols, "\x1b[2m"+hint+resetBG)
		pinCells := ""
		for i := 0; i < 4; i++ {
			if i < len(digits) {
				pinCells += "\x1b[38;5;48m[ ● ]" + resetBG + "  "
			} else {
				pinCells += "[   ]  "
			}
		}
		a.centerAt(top+11, cols, pinCells)
		if a.status != "" {
			a.centerAt(top+13, cols, "\x1b[31m"+sanitize(a.status)+resetBG)
		}
		a.centerAt(top+15, cols, "\x1b[2mType four digits  ·  Esc to quit"+resetBG)
		a.out = target
		_, _ = target.Write(frame.Bytes())
		key, err := a.readKey()
		if err != nil {
			return "", false, err
		}
		if key == "escape" || key == "ctrl-c" {
			return "", true, nil
		}
		if key == "backspace" && len(digits) > 0 {
			digits = digits[:len(digits)-1]
		}
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			digits += key
		}
	}
	return digits, false, nil
}

func (a *app) at(row, col int, value string) {
	fmt.Fprintf(a.out, "\x1b[%d;%dH%s", row, col, value)
}

func (a *app) centerAt(row, cols int, value string) {
	plain := stripANSI(value)
	col := (cols-len([]rune(plain)))/2 + 1
	if col < 1 {
		col = 1
	}
	a.at(row, col, value)
}

func stripANSI(value string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range value {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

func (a *app) loop() error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := a.refresh(); err != nil {
			return err
		}
		a.draw()
		var key string
		select {
		case <-ticker.C:
			continue
		case result := <-a.keys:
			if result.err != nil {
				return result.err
			}
			key = result.key
		}
		switch key {
		case "q", "ctrl-c":
			return nil
		case "escape":
			if a.focusTaskID != 0 {
				a.stopFocus()
			}
		case "A":
			if a.focusTaskID != 0 {
				a.stopFocus()
			}
		case "j", "down":
			if a.focusTaskID != 0 {
				break
			}
			if a.projectFocus {
				a.moveSidebar(1)
			} else {
				a.move(1)
			}
		case "k", "up":
			if a.focusTaskID != 0 {
				break
			}
			if a.projectFocus {
				a.moveSidebar(-1)
			} else {
				a.move(-1)
			}
		case "g":
			if a.projectFocus {
				a.sidebar = 0
				a.applySidebarSelection()
			} else {
				a.current = 0
			}
		case "G":
			if a.projectFocus {
				a.sidebar = 2 + len(a.projects)
				a.applySidebarSelection()
			} else if len(a.tasks) > 0 {
				a.current = len(a.tasks) - 1
			}
		case "a":
			if a.focusTaskID != 0 {
				break
			}
			project, cancelled, err := a.projectForNewTask()
			if err != nil {
				return err
			}
			if cancelled {
				break
			}
			title, cancelled, err := a.prompt("New task: ")
			if err != nil {
				return err
			}
			if !cancelled && strings.TrimSpace(title) != "" {
				_, err = a.store.AddToProject(title, project.ID)
				a.setResult(err, "Task added")
			}
		case "h", "left":
			a.projectFocus = true
		case "l", "right":
			a.projectFocus = false
		case "p":
			if a.focusTaskID != 0 {
				if err := a.promptProgress(); err != nil {
					return err
				}
				break
			}
			name, cancelled, err := a.prompt("New project: ")
			if err != nil {
				return err
			}
			if !cancelled && strings.TrimSpace(name) != "" {
				project, err := a.store.AddProject(name)
				a.setResult(err, "Project created")
				if err == nil {
					if err := a.refresh(); err != nil {
						return err
					}
					for i := range a.projects {
						if a.projects[i].ID == project.ID {
							a.project = i
						}
					}
				}
			}
		case " ":
			if a.focusTaskID != 0 {
				break
			}
			if a.projectFocus {
				break
			}
			if t, ok := a.selected(); ok {
				err := a.store.SetDone(t.ID, !t.Done)
				a.setResult(err, toggleMessage(t.Done))
				if err == nil {
					if err := a.refresh(); err != nil {
						return err
					}
					a.selectTask(t.ID)
				}
			}
		case "enter":
			if a.focusTaskID != 0 {
				break
			}
			if a.projectFocus {
				a.applySidebarSelection()
				a.projectFocus = false
				break
			}
			if t, ok := a.selected(); ok {
				title, cancelled, err := a.promptWithValue("Edit task: ", t.Title)
				if err != nil {
					return err
				}
				if !cancelled {
					a.setResult(a.store.Rename(t.ID, title), "Task updated")
				}
			}
		case "d":
			if a.focusTaskID != 0 {
				break
			}
			if a.projectFocus {
				a.status = "Project deletion is not available yet"
				break
			}
			if t, ok := a.selected(); ok {
				yes, _, err := a.prompt(fmt.Sprintf("Delete #%d? Type y: ", t.ID))
				if err != nil {
					return err
				}
				if strings.EqualFold(strings.TrimSpace(yes), "y") {
					a.setResult(a.store.Delete(t.ID), "Task deleted")
				}
			}
		case "t":
			if a.focusTaskID == 0 && !a.projectFocus {
				a.setTaskState("today")
			}
		case "m":
			if a.focusTaskID == 0 && !a.projectFocus {
				a.cycleTaskState()
			}
		case "?":
			a.status = "h/← projects · l/→ tasks · j/k select · enter edit · space done · q quit"
		}
		if key == "f" && a.focusTaskID == 0 && !a.projectFocus {
			if task, ok := a.selected(); ok {
				a.focusTaskID = task.ID
			}
		}
	}
}

func (a *app) stopFocus() {
	if a.focusTaskID == 0 {
		return
	}
	a.focusTaskID = 0
}

func (a *app) promptProgress() error {
	task, ok := a.focusTask()
	if !ok {
		return nil
	}
	value, cancelled, err := a.prompt(fmt.Sprintf("Progress [%d] (0-100): ", task.Progress))
	if err != nil || cancelled {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}
	progress, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		a.status = "Progress must be a number from 0 to 100"
		return nil
	}
	if err := a.store.SetProgress(task.ID, progress); err != nil {
		a.status = err.Error()
	}
	return nil
}

func (a *app) focusTask() (store.Task, bool) {
	for _, task := range a.tasks {
		if task.ID == a.focusTaskID {
			return task, true
		}
	}
	return store.Task{}, false
}

func (a *app) refresh() error {
	projects, err := a.store.Projects()
	if err != nil {
		return err
	}
	a.projects = projects
	if a.project >= len(projects) {
		a.project = len(projects) - 1
	}
	if a.project < 0 {
		a.project = 0
	}
	// The outline always retains completed tasks in place; completing work must
	// never make it appear deleted.
	tasks, err := a.store.List(true)
	if err != nil {
		return err
	}
	projectID := a.activeProject().ID
	states := []string{"today", "blocked", "waiting"}
	for i := range a.viewCounts {
		a.viewCounts[i] = 0
	}
	for _, task := range tasks {
		if task.ProjectID != projectID || task.Done {
			continue
		}
		for i, state := range states {
			if task.State == state {
				a.viewCounts[i]++
			}
		}
	}
	a.tasks = a.tasks[:0]
	for _, task := range tasks {
		if task.ProjectID == projectID && task.State == states[a.view] {
			a.tasks = append(a.tasks, task)
		}
	}
	if a.current >= len(a.tasks) {
		a.current = len(a.tasks) - 1
	}
	if a.current < 0 {
		a.current = 0
	}
	return nil
}

func (a *app) moveSidebar(delta int) {
	total := 3 + len(a.projects)
	if total == 0 {
		return
	}
	a.sidebar = (a.sidebar + delta + total) % total
	a.applySidebarSelection()
}

func (a *app) applySidebarSelection() {
	if a.sidebar < 3 {
		a.view = a.sidebar
	} else if len(a.projects) > 0 {
		a.project = a.sidebar - 3
	}
	a.current = 0
}

func (a *app) cycleTaskState() {
	task, ok := a.selected()
	if !ok {
		return
	}
	next := map[string]string{"today": "blocked", "blocked": "waiting", "waiting": "today"}[task.State]
	if next == "" {
		next = "today"
	}
	a.setResult(a.store.SetState(task.ID, next), "Moved task to "+next)
}

func (a *app) setTaskState(state string) {
	if task, ok := a.selected(); ok {
		a.setResult(a.store.SetState(task.ID, state), "Moved task to "+state)
	}
}

func (a *app) activeProject() store.Project {
	if len(a.projects) == 0 {
		return store.Project{Name: "No project"}
	}
	return a.projects[a.project]
}

func (a *app) projectForNewTask() (store.Project, bool, error) {
	label := "Project (new name creates it): "
	if len(a.projects) > 0 {
		label = fmt.Sprintf("Project [%s]: ", a.activeProject().Name)
	}
	name, cancelled, err := a.prompt(label)
	if err != nil || cancelled {
		return store.Project{}, cancelled, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if len(a.projects) == 0 {
			a.status = "A project name is required"
			return a.projectForNewTask()
		}
		return a.activeProject(), false, nil
	}
	for i, project := range a.projects {
		if strings.EqualFold(project.Name, name) {
			a.project = i
			return project, false, nil
		}
	}
	project, err := a.store.AddProject(name)
	if err != nil {
		return store.Project{}, false, err
	}
	if err := a.refresh(); err != nil {
		return store.Project{}, false, err
	}
	for i := range a.projects {
		if a.projects[i].ID == project.ID {
			a.project = i
		}
	}
	return project, false, nil
}

func (a *app) changeProject(delta int) {
	if len(a.projects) == 0 {
		return
	}
	a.project = (a.project + delta + len(a.projects)) % len(a.projects)
	a.current = 0
}

func (a *app) draw() {
	// Compose the complete frame in memory and publish it in one terminal write.
	// This avoids visible partial frames and the flash caused by clearing first.
	target := a.out
	var frame bytes.Buffer
	a.out = &frame
	defer func() {
		a.out = target
		_, _ = target.Write(frame.Bytes())
	}()

	open := 0
	for _, t := range a.tasks {
		if !t.Done {
			open++
		}
	}
	now := time.Now()
	project := a.activeProject()
	fmt.Fprint(a.out, background+"\x1b[H")
	if a.focusTaskID == 0 {
		fmt.Fprint(a.out, "\x1b[48;5;252m\x1b[30m A · OUTLINE "+resetBG+"  \x1b[48;5;238m F · FOCUS "+resetBG+"\r\n\r\n")
	} else {
		fmt.Fprint(a.out, "\x1b[48;5;238m A · OUTLINE "+resetBG+"  \x1b[48;5;252m\x1b[30m F · FOCUS "+resetBG+"\r\n\r\n")
	}
	fmt.Fprint(a.out, "┌──────────────────────┬──────────────────────────────────────────────────────────────────────┐\r\n")
	leftHeader := "\x1b[38;5;48m" + padRight("assistant", 20) + resetBG
	rightHeader := padBetween("Project: "+truncate(project.Name, 36), now.Format("Mon 02 Jan · 15:04:05"), 68)
	fmt.Fprintf(a.out, "│ %s │ %s │\r\n", leftHeader, rightHeader)
	fmt.Fprint(a.out, "├──────────────────────┼──────────────────────────────────────────────────────────────────────┤\r\n")
	viewNames := []string{"Today", "Blocked", "Waiting"}
	left := []string{"VIEWS", ""}
	for i, name := range viewNames {
		prefix := "  "
		if i == a.view {
			prefix = "▸ "
		}
		left = append(left, fmt.Sprintf("%s%-12s %3d", prefix, name, a.viewCounts[i]))
	}
	left = append(left, "", "PROJECTS", "")
	for i, p := range a.projects {
		prefix := "  "
		if i == a.project {
			prefix = "▸ "
		}
		left = append(left, truncate(prefix+p.Name, 20))
	}
	for len(left) < 22 {
		left = append(left, "")
	}
	right := a.outlineRows(project, open)
	if a.focusTaskID != 0 {
		right = a.focusRows(now)
	}
	for i := range left {
		leftCell := padRight(left[i], 20)
		focusRow := a.sidebar + 2
		if a.sidebar >= 3 {
			focusRow = a.sidebar + 5
		}
		if a.projectFocus && i == focusRow {
			leftCell = "\x1b[48;5;22m" + leftCell + resetBG
		}
		rightCell := padRight(right[i], 68)
		if a.focusTaskID != 0 {
			switch i {
			case 0, 12:
				rightCell = "\x1b[38;5;48m" + padRight(right[i], 68) + resetBG
			case 3, 7:
				rightCell = "\x1b[1;38;5;255m" + padRight(right[i], 68) + resetBG
			case 10:
				rightCell = right[i]
			}
		}
		if a.focusTaskID == 0 && len(a.tasks) > open && i == open+3 {
			rightCell = "\x1b[38;5;244m" + rightCell + resetBG
		}
		if task, ok := a.taskAtRow(i); a.focusTaskID == 0 && ok && task.Done {
			rightCell = "\x1b[9;38;5;244m" + rightCell + resetBG
		}
		if a.focusTaskID == 0 && i == a.selectedTaskRow() && a.current < len(a.tasks) {
			style := "\x1b[48;5;22m"
			if a.tasks[a.current].Done {
				style += "\x1b[9;38;5;244m"
			}
			rightCell = style + padRight(right[i], 68) + resetBG
		}
		fmt.Fprintf(a.out, "│ %s │ %s │\r\n", leftCell, rightCell)
	}
	fmt.Fprint(a.out, "├──────────────────────┴──────────────────────────────────────────────────────────────────────┤\r\n")
	var footer string
	if a.focusTaskID == 0 {
		footer = "\x1b[33mh/←" + resetBG + " sidebar   \x1b[33ml/→" + resetBG + " tasks   \x1b[33mj/k" + resetBG + " select   \x1b[33mm" + resetBG + " move   \x1b[33mf" + resetBG + " focus   \x1b[33menter" + resetBG + " edit   \x1b[33mq" + resetBG + " quit"
	} else {
		footer = "\x1b[33mp" + resetBG + " set progress   \x1b[33mesc" + resetBG + " outline   \x1b[33mq" + resetBG + " quit"
	}
	fmt.Fprintf(a.out, "│ %s │\r\n", padVisible(footer, 91))
	fmt.Fprint(a.out, "└─────────────────────────────────────────────────────────────────────────────────────────────┘\r\n")
	if a.status != "" {
		fmt.Fprintf(a.out, "\x1b[K\x1b[33m%s%s\r\n", sanitize(a.status), resetBG)
	} else {
		fmt.Fprint(a.out, "\x1b[K\r\n")
	}
	fmt.Fprint(a.out, "\x1b[J")
}

func (a *app) focusRows(now time.Time) []string {
	rows := make([]string, 22)
	task, ok := a.focusTask()
	if !ok {
		rows[2] = "Focused task is unavailable"
		return rows
	}
	elapsed := time.Duration(task.CapturedSeconds) * time.Second
	progress := task.Progress
	rows[0] = padCenter("F O C U S", 68)
	rows[3] = padCenter(truncate(task.Title, 58), 68)
	started := task.CreatedAt.Local().Format("02 Jan 2006 · 15:04")
	rows[7] = padCenter(fmt.Sprintf("STARTED  %s          CAPTURED  %s", started, formatDuration(elapsed)), 68)
	rows[10] = focusBar(elapsed, progress)
	rows[12] = padCenter("TASK PROGRESS", 68)
	rows[16] = padCenter("p set progress  ·  Esc return to outline", 68)
	return rows
}

func focusBar(_ time.Duration, progress int) string {
	const width = 52
	filled := progress * width / 100
	var bar strings.Builder
	bar.WriteString(strings.Repeat(" ", 8))
	for i := 0; i < width; i++ {
		if i == filled && progress > 0 && progress < 100 {
			bar.WriteString("\x1b[97m━")
		} else if i < filled {
			bar.WriteString("\x1b[38;5;48m━")
		} else {
			bar.WriteString("\x1b[38;5;240m─")
		}
	}
	bar.WriteString(resetBG + strings.Repeat(" ", 8))
	return bar.String()
}

func padCenter(value string, width int) string {
	length := len([]rune(value))
	if length >= width {
		return truncate(value, width)
	}
	left := (width - length) / 2
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", width-length-left)
}

func formatDuration(duration time.Duration) string {
	total := int64(duration.Seconds())
	if total < 0 {
		total = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total/60)%60, total%60)
}

func (a *app) outlineRows(project store.Project, open int) []string {
	right := make([]string, 22)
	right[0] = fmt.Sprintf("%-61s%7s", truncate(project.Name, 60), fmt.Sprintf("%d open", open))
	doneStarted := false
	row := 2
	for _, task := range a.tasks {
		if task.Done && !doneStarted {
			doneStarted = true
			doneCount := len(a.tasks) - open
			row++
			right[row] = fmt.Sprintf("· DONE (%d) ·", doneCount)
			row++
		}
		if row >= len(right) {
			break
		}
		mark := progressGlyph(task.Progress)
		right[row] = fmt.Sprintf("▸ %s  %s", mark, truncate(sanitize(task.Title), 56))
		row++
	}
	return right
}

func progressGlyph(progress int) string {
	switch {
	case progress <= 0:
		return "○"
	case progress < 34:
		return "◔"
	case progress < 67:
		return "◑"
	case progress < 100:
		return "◕"
	default:
		return "●"
	}
}

func (a *app) selectedTaskRow() int {
	if a.current < 0 || a.current >= len(a.tasks) {
		return -1
	}
	row := a.current + 2
	if a.tasks[a.current].Done {
		row += 2
	}
	return row
}

func (a *app) taskAtRow(row int) (store.Task, bool) {
	for i, task := range a.tasks {
		position := i + 2
		if task.Done {
			position += 2
		}
		if position == row {
			return task, true
		}
	}
	return store.Task{}, false
}

func padBetween(left, right string, width int) string {
	leftRunes, rightRunes := []rune(left), []rune(right)
	spaces := width - len(leftRunes) - len(rightRunes)
	if spaces < 1 {
		return truncate(left, width-len(rightRunes)-1) + " " + right
	}
	return left + strings.Repeat(" ", spaces) + right
}

func padVisible(value string, width int) string {
	visible := len([]rune(stripANSI(value)))
	if visible >= width {
		return value
	}
	return value + strings.Repeat(" ", width-visible)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}

func (a *app) move(delta int) {
	if len(a.tasks) == 0 {
		return
	}
	a.current = (a.current + delta + len(a.tasks)) % len(a.tasks)
}

func (a *app) selected() (store.Task, bool) {
	if len(a.tasks) == 0 {
		return store.Task{}, false
	}
	return a.tasks[a.current], true
}

func (a *app) selectTask(id int) {
	for i := range a.tasks {
		if a.tasks[i].ID == id {
			a.current = i
			return
		}
	}
}

func (a *app) setResult(err error, success string) {
	if err != nil {
		a.status = err.Error()
	} else {
		a.status = success
	}
}

func toggleMessage(wasDone bool) string {
	if wasDone {
		return "Task reopened"
	}
	return "Task completed"
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\x1b", "")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

func (a *app) prompt(label string) (string, bool, error) {
	return a.promptWithValue(label, "")
}

func (a *app) promptWithValue(label, initial string) (string, bool, error) {
	value := []rune(initial)
	for {
		a.draw()
		fmt.Fprintf(a.out, "\x1b[?25h%s%s\x1b[K", label, string(value))
		result := <-a.keys
		if result.err != nil {
			return "", false, result.err
		}
		key := result.key
		switch key {
		case "enter":
			fmt.Fprint(a.out, "\x1b[?25l")
			return string(value), false, nil
		case "escape", "ctrl-c":
			fmt.Fprint(a.out, "\x1b[?25l")
			return "", true, nil
		case "backspace":
			if len(value) > 0 {
				value = value[:len(value)-1]
			}
		default:
			r := []rune(key)
			if len(r) == 1 && r[0] >= 32 {
				value = append(value, r[0])
			}
		}
	}
}

func (a *app) readKey() (string, error) {
	b, err := a.reader.ReadByte()
	if err != nil {
		return "", err
	}
	switch b {
	case 3:
		return "ctrl-c", nil
	case 13, 10:
		return "enter", nil
	case 127, 8:
		return "backspace", nil
	case 27:
		if a.reader.Buffered() == 0 {
			return "escape", nil
		}
		next, _ := a.reader.ReadByte()
		if next == '[' && a.reader.Buffered() > 0 {
			last, _ := a.reader.ReadByte()
			switch last {
			case 'A':
				return "up", nil
			case 'B':
				return "down", nil
			case 'C':
				return "right", nil
			case 'D':
				return "left", nil
			}
		}
		return "escape", nil
	case 9:
		return "tab", nil
	default:
		return string(rune(b)), nil
	}
}
