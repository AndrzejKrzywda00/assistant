package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/AndrzejKrzywda00/assistant/internal/auth"
	"github.com/AndrzejKrzywda00/assistant/internal/store"
)

var errQuit = fmt.Errorf("quit")

const (
	background = "\x1b[48;5;234m\x1b[38;5;252m"
	resetBG    = "\x1b[0m" + background
)

type app struct {
	store         *store.Store
	in            *os.File
	out           io.Writer
	reader        *bufio.Reader
	tasks         []store.Task
	projects      []store.Project
	project       int
	current       int
	status        string
	keys          chan keyResult
	projectFocus  bool
	focusTaskID   int
	sidebar       int
	space         int
	spaceCounts   [3]int
	taskScroll    int
	sidebarScroll int
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
	// Clear exactly once between the centered authentication card and the
	// dashboard. Normal dashboard frames only move home and overwrite content,
	// which keeps interaction flicker-free.
	fmt.Fprint(out, background+"\x1b[2J\x1b[H")
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
		a.centerAt(top+6, cols, "\x1b[1;38;5;255m"+truncate("What should I call you?", width-4)+resetBG)
		shown := string(value)
		if shown == "" {
			shown = "Your name"
		}
		a.centerAt(top+9, cols, "\x1b[4m  "+shown+"  "+resetBG)
		instruction := "Type your name · Enter continue · Esc quit"
		a.centerAt(top+12, cols, "\x1b[2m"+truncate(instruction, width-4)+resetBG)
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
		a.centerAt(top+6, cols, "\x1b[1;38;5;255m"+truncate(subtitle, width-4)+resetBG)
		a.centerAt(top+8, cols, "\x1b[2m"+truncate(hint, width-4)+resetBG)
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
		case "down":
			if a.focusTaskID != 0 {
				break
			}
			if a.projectFocus {
				a.moveSidebar(1)
			} else {
				a.move(1)
			}
		case "up":
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
				if err := a.promptTaskContent(); err != nil {
					return err
				}
				break
			}
			if a.projectFocus {
				if err := a.promptNewProject(); err != nil {
					return err
				}
				break
			}
			project := a.activeProject()
			if len(a.projects) == 0 {
				var cancelled bool
				var err error
				project, cancelled, err = a.projectForNewTask()
				if err != nil {
					return err
				}
				if cancelled {
					break
				}
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
			if err := a.promptNewProject(); err != nil {
				return err
			}
		case "P":
			if a.focusTaskID != 0 {
				if err := a.promptPriority(); err != nil {
					return err
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
				a.activateSidebarSelection()
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
				if err := a.promptDeleteTaskContent(); err != nil {
					return err
				}
				break
			}
			if a.projectFocus {
				if a.sidebar < 3 || a.sidebar-3 >= len(a.projects) {
					a.status = "Select a project to delete"
					break
				}
				if err := a.confirmDeleteProject(a.sidebar - 3); err != nil {
					return err
				}
				break
			}
			if t, ok := a.selected(); ok {
				yes, _, err := a.prompt(deletePrompt(fmt.Sprintf("Delete task #%d? Type y: ", t.ID)))
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
		case "b":
			if a.focusTaskID == 0 && !a.projectFocus {
				a.setTaskState("blocked")
			}
		case "?":
			a.status = "h/← projects · l/→ tasks · ↑/↓ select · b blocked · d delete · q quit"
		}
		if key == "f" && a.focusTaskID == 0 && !a.projectFocus && a.focusAvailable() {
			if task, ok := a.selected(); ok {
				a.focusTaskID = task.ID
			}
		}
	}
}

func (a *app) promptNewProject() error {
	name, cancelled, err := a.prompt("New project: ")
	if err != nil || cancelled || strings.TrimSpace(name) == "" {
		return err
	}
	project, err := a.store.AddProject(name)
	a.setResult(err, "Project created")
	if err != nil {
		return nil
	}
	if err := a.refresh(); err != nil {
		return err
	}
	for i := range a.projects {
		if a.projects[i].ID == project.ID {
			a.project = i
			a.sidebar = i + 3
			break
		}
	}
	return nil
}

func (a *app) confirmDeleteProject(index int) error {
	project := a.projects[index]
	yes, cancelled, err := a.prompt(deleteProjectPrompt(project.Name))
	if err != nil || cancelled || !strings.EqualFold(strings.TrimSpace(yes), "y") {
		return err
	}
	if err := a.store.DeleteProject(project.ID); err != nil {
		a.setResult(err, "")
		return nil
	}
	if err := a.refresh(); err != nil {
		return err
	}
	if len(a.projects) == 0 {
		a.project = 0
		a.sidebar = 0
	} else {
		a.project = minInt(index, len(a.projects)-1)
		a.sidebar = a.project + 3
	}
	a.status = "Project deleted"
	return nil
}

func deletePrompt(message string) string {
	return "\x1b[31m" + message + resetBG
}

func deleteProjectPrompt(name string) string {
	return "\x1b[31mDelete project \x1b[97m\"" + sanitize(name) + "\"\x1b[31m and all its tasks? Type y: " + resetBG
}

func (a *app) stopFocus() {
	if a.focusTaskID == 0 {
		return
	}
	focusedID := a.focusTaskID
	a.focusTaskID = 0
	a.selectTask(focusedID)
}

func (a *app) focusAvailable() bool {
	return !a.projectFocus && a.sidebar >= 0 && a.sidebar < 3
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

func (a *app) promptPriority() error {
	task, ok := a.focusTask()
	if !ok {
		return nil
	}
	priorities := []string{"P0", "P1", "P2"}
	selected := 1
	for i, priority := range priorities {
		if taskPriority(task) == priority {
			selected = i
			break
		}
	}
	for {
		a.draw()
		var slider strings.Builder
		slider.WriteString("Priority  ")
		for i, priority := range priorities {
			if i > 0 {
				slider.WriteString(" ─── ")
			}
			if i == selected {
				slider.WriteString("\x1b[38;5;48m● " + priority + resetBG)
			} else {
				slider.WriteString("○ " + priority)
			}
		}
		slider.WriteString("  ·  ←/→ choose · Enter save · Esc cancel")
		fmt.Fprintf(a.out, "%s\x1b[K", slider.String())
		result := <-a.keys
		if result.err != nil {
			return result.err
		}
		switch result.key {
		case "left", "up":
			selected = maxInt(0, selected-1)
		case "right", "down":
			selected = minInt(len(priorities)-1, selected+1)
		case "0", "1", "2":
			selected = int(result.key[0] - '0')
		case "enter":
			a.setResult(a.store.SetPriority(task.ID, priorities[selected]), "Priority set to "+priorities[selected])
			return nil
		case "escape", "ctrl-c":
			return nil
		}
	}
}

func taskPriority(task store.Task) string {
	if task.Priority == "P0" || task.Priority == "P2" {
		return task.Priority
	}
	return "P1"
}

func (a *app) promptTaskContent() error {
	task, ok := a.focusTask()
	if !ok {
		return nil
	}
	key, cancelled, err := a.prompt("Content key: ")
	if err != nil || cancelled {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		a.status = "Content key cannot be empty"
		return nil
	}
	value, cancelled, err := a.promptWithValue("Content value: ", task.Content[key])
	if err != nil || cancelled {
		return err
	}
	a.setResult(a.store.SetTaskContent(task.ID, key, value), "Task content saved")
	return nil
}

func (a *app) promptDeleteTaskContent() error {
	task, ok := a.focusTask()
	if !ok {
		return nil
	}
	if len(task.Content) == 0 {
		a.status = "Task has no content keys"
		return nil
	}
	key, cancelled, err := a.prompt("Delete content key: ")
	if err != nil || cancelled {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return nil
	}
	a.setResult(a.store.DeleteTaskContent(task.ID, key), "Task content deleted")
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
	for i := range a.spaceCounts {
		a.spaceCounts[i] = 0
	}
	for _, task := range tasks {
		if task.Done || task.ProjectID != projectID {
			continue
		}
		for i, state := range states {
			if task.State == state {
				a.spaceCounts[i]++
			}
		}
	}
	a.tasks = a.tasks[:0]
	for _, task := range tasks {
		if task.ProjectID == projectID && task.State == states[a.space] {
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
	// Spaces change immediately, but browsing project names only moves the
	// cursor. A project becomes active explicitly with Enter.
	if a.sidebar < 3 {
		a.applySidebarSelection()
	}
}

func (a *app) applySidebarSelection() {
	if a.sidebar < 3 {
		a.space = a.sidebar
	} else if len(a.projects) > 0 {
		a.project = a.sidebar - 3
	}
	a.current = 0
}

func (a *app) activateSidebarSelection() {
	a.applySidebarSelection()
	// Selecting a project keeps the cursor in the Projects list so another
	// project can immediately be chosen with the arrow keys. Newly selected
	// projects always open on Today; spaces open tasks.
	if a.sidebar < 3 {
		a.projectFocus = false
	} else {
		a.space = 0
	}
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

	rows, cols := terminalSize(a.in.Fd())
	fmt.Fprint(a.out, background+"\x1b[H")
	if rows < 14 || cols < 45 {
		message := fmt.Sprintf("Terminal too small (%dx%d) · minimum 45x14", cols, rows)
		a.centerAt(maxInt(1, rows/2), cols, "\x1b[33m"+message+resetBG)
		fmt.Fprint(a.out, "\x1b[J")
		return
	}

	now := time.Now()
	project := a.activeProject()
	bodyHeight := rows - 9
	showTwoPanes := cols >= 65
	sidebarOnly := cols < 65 && a.projectFocus
	// Overwrite the complete row. The top bar can become shorter when Focus is
	// hidden; padding prevents cells from the previous frame remaining visible
	// without clearing the screen and causing a flash.
	fmt.Fprint(a.out, padVisible(a.topBar(), cols)+"\r\n")

	if showTwoPanes {
		sideWidth := clampInt(cols/4, 16, 28)
		mainWidth := cols - sideWidth - 7
		fmt.Fprintf(a.out, "┌%s┬%s┐\r\n", strings.Repeat("─", sideWidth+2), strings.Repeat("─", mainWidth+2))
		leftHeader := "\x1b[38;5;48m" + fitCell("assistant", sideWidth) + resetBG
		rightHeader := padBetween("Project: "+project.Name, now.Format("02 Jan · 15:04:05"), mainWidth)
		leftHeader = boldPaneHeader(leftHeader, a.projectFocus)
		rightHeader = boldPaneHeader(rightHeader, !a.projectFocus)
		fmt.Fprintf(a.out, "│ %s │ %s │\r\n", leftHeader, rightHeader)
		fmt.Fprintf(a.out, "├%s┼%s┤\r\n", strings.Repeat("─", sideWidth+2), strings.Repeat("─", mainWidth+2))
		sideRows := a.sidebarPaneRows(bodyHeight, sideWidth)
		mainRows := a.mainPaneRows(bodyHeight, mainWidth, now)
		for i := 0; i < bodyHeight; i++ {
			fmt.Fprintf(a.out, "│ %s │ %s │\r\n", sideRows[i], mainRows[i])
		}
	} else {
		innerWidth := cols - 4
		fmt.Fprintf(a.out, "┌%s┐\r\n", strings.Repeat("─", cols-2))
		headerLeft := "assistant · Project: " + project.Name
		if sidebarOnly {
			headerLeft = "assistant · navigation"
		}
		header := padBetween(headerLeft, now.Format("15:04:05"), innerWidth)
		fmt.Fprintf(a.out, "│ %s │\r\n", boldPaneHeader(header, true))
		fmt.Fprintf(a.out, "├%s┤\r\n", strings.Repeat("─", cols-2))
		var pane []string
		if sidebarOnly {
			pane = a.sidebarPaneRows(bodyHeight, innerWidth)
		} else {
			pane = a.mainPaneRows(bodyHeight, innerWidth, now)
		}
		for i := 0; i < bodyHeight; i++ {
			fmt.Fprintf(a.out, "│ %s │\r\n", pane[i])
		}
	}

	fmt.Fprintf(a.out, "├%s┤\r\n", strings.Repeat("─", cols-2))
	footer := a.footerForWidth(cols - 4)
	fmt.Fprintf(a.out, "│ %s │\r\n", padVisible(footer, cols-4))
	fmt.Fprintf(a.out, "└%s┘\r\n", strings.Repeat("─", cols-2))
	if a.status != "" {
		fmt.Fprintf(a.out, "\x1b[K\x1b[33m%s%s\r\n", sanitize(a.status), resetBG)
	} else {
		fmt.Fprint(a.out, "\x1b[K\r\n")
	}
	fmt.Fprint(a.out, "\x1b[J")
}

func boldPaneHeader(value string, active bool) string {
	if !active {
		return value
	}
	return "\x1b[1m" + value + "\x1b[22m"
}

type paneRow struct {
	text          string
	taskIndex     int
	done          bool
	doneHeader    bool
	pendingHeader bool
	emptyPrompt   bool
	selected      bool
}

func (a *app) mainPaneRows(height, width int, now time.Time) []string {
	if a.focusTaskID != 0 {
		return a.responsiveFocusRows(height, width, now)
	}
	open := 0
	for _, task := range a.tasks {
		if !task.Done {
			open++
		}
	}
	result := make([]string, height)
	result[0] = padBetween(a.activeProject().Name, fmt.Sprintf("%d open", open), width)
	contentWidth := maxInt(1, width-1)
	doneCount := len(a.tasks) - open
	// Keep the project heading visually separate from the task status sections.
	items := []paneRow{{taskIndex: -1}}
	selectedItem := 0
	appendTask := func(task store.Task, taskIndex int, done bool) {
		prefix := fmt.Sprintf("▸ %s  ", progressGlyph(task.Progress))
		lines := wrapText(task.Title, maxInt(1, contentWidth-len([]rune(prefix))))
		for lineIndex, line := range lines {
			text := strings.Repeat(" ", len([]rune(prefix))) + line
			if lineIndex == 0 {
				text = prefix + line
			}
			item := paneRow{text: text, taskIndex: taskIndex, done: done, selected: taskIndex == a.current}
			if item.selected && lineIndex == 0 {
				selectedItem = len(items)
			}
			items = append(items, item)
		}
	}
	if a.space != 0 {
		spaceName := []string{"TODAY", "BLOCKED", "WAITING"}[a.space]
		items = append(items, paneRow{text: fmt.Sprintf("· %s (%d) ·", spaceName, len(a.tasks)), taskIndex: -1, pendingHeader: true})
		for i, task := range a.tasks {
			appendTask(task, i, task.Done)
		}
		if len(a.tasks) == 0 {
			items = append(items, paneRow{text: "No " + strings.ToLower(spaceName) + " tasks", taskIndex: -1, emptyPrompt: true})
		}
	} else {
		items = append(items, paneRow{text: fmt.Sprintf("· PENDING (%d) ·", open), taskIndex: -1, pendingHeader: true})
		for i, task := range a.tasks {
			if task.Done {
				continue
			}
			appendTask(task, i, task.Done)
		}
		if open == 0 {
			items = append(items, paneRow{text: "No pending tasks · press a to add one", taskIndex: -1, emptyPrompt: true})
		}
		items = append(items, paneRow{taskIndex: -1}, paneRow{text: fmt.Sprintf("· DONE (%d) ·", doneCount), taskIndex: -1, doneHeader: true})
		for i, task := range a.tasks {
			if !task.Done {
				continue
			}
			appendTask(task, i, true)
		}
		if doneCount == 0 {
			items = append(items, paneRow{text: "No completed tasks yet", taskIndex: -1, emptyPrompt: true})
		}
	}
	visible := maxInt(0, height-1)
	if selectedItem < a.taskScroll {
		a.taskScroll = selectedItem
	}
	if selectedItem >= a.taskScroll+visible {
		a.taskScroll = selectedItem - visible + 1
	}
	maxScroll := maxInt(0, len(items)-visible)
	if a.taskScroll > maxScroll {
		a.taskScroll = maxScroll
	}
	overflow := len(items) > visible
	thumbStart, thumbSize := 0, 0
	if overflow && visible > 0 {
		thumbSize = maxInt(1, visible*visible/len(items))
		thumbSize = minInt(visible, thumbSize)
		if maxScroll > 0 {
			thumbStart = a.taskScroll * (visible - thumbSize) / maxScroll
		}
	}
	for row := 1; row < height; row++ {
		index := a.taskScroll + row - 1
		if index >= len(items) {
			result[row] = strings.Repeat(" ", contentWidth)
			if overflow {
				result[row] += "\x1b[2;38;5;240m│" + resetBG
			} else {
				result[row] += " "
			}
			continue
		}
		item := items[index]
		plainCell := fitCell(item.text, contentWidth)
		if item.done && needsStrikeFallback() {
			plainCell = unicodeStrike(plainCell)
		}
		cell := plainCell
		if item.pendingHeader {
			cell = "\x1b[38;5;48m" + cell + resetBG
		}
		if item.doneHeader {
			cell = "\x1b[38;5;244m" + cell + resetBG
		}
		if item.emptyPrompt {
			cell = "\x1b[2;38;5;244m" + cell + resetBG
		}
		if item.done {
			cell = "\x1b[9;38;5;244m" + cell + resetBG
		}
		if item.selected {
			style := "\x1b[48;5;22m"
			if item.done {
				style += "\x1b[9;38;5;244m"
			}
			cell = style + plainCell + resetBG
		}
		if overflow {
			viewportRow := row - 1
			if viewportRow >= thumbStart && viewportRow < thumbStart+thumbSize {
				cell += "\x1b[38;5;48m┃" + resetBG
			} else {
				cell += "\x1b[2;38;5;240m│" + resetBG
			}
		} else {
			cell += " "
		}
		result[row] = cell
	}
	return result
}

func needsStrikeFallback() bool {
	return os.Getenv("TERM_PROGRAM") == "Apple_Terminal"
}

// unicodeStrike uses the combining long-stroke overlay for terminals that do
// not render SGR 9. Internal spaces receive the overlay so the line remains
// continuous between words, while trailing cell padding stays untouched.
func unicodeStrike(value string) string {
	content := strings.TrimRightFunc(value, unicode.IsSpace)
	padding := value[len(content):]
	var result strings.Builder
	for _, r := range content {
		result.WriteRune(r)
		result.WriteRune('\u0336')
	}
	result.WriteString(padding)
	return result.String()
}

func (a *app) sidebarPaneRows(height, width int) []string {
	type sideItem struct {
		text   string
		cursor int
	}
	items := []sideItem{{"SPACES", -1}, {"", -1}}
	spaces := []string{"Today", "Blocked", "Waiting"}
	for i, name := range spaces {
		prefix := "  "
		if a.sidebar == i {
			prefix = "▸ "
		}
		items = append(items, sideItem{fmt.Sprintf("%s%s %d", prefix, name, a.spaceCounts[i]), i})
	}
	items = append(items, sideItem{"", -1}, sideItem{"PROJECTS", -1}, sideItem{"", -1})
	for i, project := range a.projects {
		if i > 0 {
			items = append(items, sideItem{"", -1})
		}
		cursor := i + 3
		nameWidth := maxInt(1, width-4)
		lines := wrapText(project.Name, nameWidth)
		for lineIndex, line := range lines {
			prefix := "  "
			if a.projectFocus && a.sidebar == cursor && lineIndex == 0 {
				prefix = "▸ "
			}
			text := prefix + line
			items = append(items, sideItem{text, cursor})
		}
	}
	selectedRow := 0
	for i, item := range items {
		if item.cursor == a.sidebar {
			selectedRow = i
		}
	}
	if selectedRow < a.sidebarScroll {
		a.sidebarScroll = selectedRow
	}
	if selectedRow >= a.sidebarScroll+height {
		a.sidebarScroll = selectedRow - height + 1
	}
	maxScroll := maxInt(0, len(items)-height)
	if a.sidebarScroll > maxScroll {
		a.sidebarScroll = maxScroll
	}
	result := make([]string, height)
	for row := 0; row < height; row++ {
		index := a.sidebarScroll + row
		if index >= len(items) {
			result[row] = strings.Repeat(" ", width)
			continue
		}
		item := items[index]
		cell := fitCell(item.text, width)
		if item.cursor >= 3 && item.cursor == a.project+3 {
			cell = underlineProjectCell(item.text, width)
		}
		if a.projectFocus && item.cursor == a.sidebar {
			cell = "\x1b[48;5;22m" + cell + resetBG
		}
		result[row] = cell
	}
	return result
}

func underlineProjectCell(value string, width int) string {
	runes := []rune(sanitize(value))
	if len(runes) > width {
		runes = append(runes[:maxInt(0, width-1)], '…')
	}
	prefixWidth := minInt(2, len(runes))
	prefix := string(runes[:prefixWidth])
	name := string(runes[prefixWidth:])
	return prefix + "\x1b[4m" + name + "\x1b[24m" + strings.Repeat(" ", maxInt(0, width-len(runes)))
}

func wrapText(value string, width int) []string {
	value = strings.TrimSpace(sanitize(value))
	if value == "" {
		return []string{""}
	}
	width = maxInt(1, width)
	words := strings.Fields(value)
	lines := make([]string, 0, len(words))
	current := ""
	flush := func() {
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
	}
	for _, word := range words {
		for len([]rune(word)) > width {
			flush()
			runes := []rune(word)
			lines = append(lines, string(runes[:width]))
			word = string(runes[width:])
		}
		if current == "" {
			current = word
			continue
		}
		if len([]rune(current))+1+len([]rune(word)) <= width {
			current += " " + word
			continue
		}
		flush()
		current = word
	}
	flush()
	return lines
}

func (a *app) responsiveFocusRows(height, width int, now time.Time) []string {
	result := make([]string, height)
	task, ok := a.focusTask()
	if !ok {
		result[0] = fitCell("Focused task unavailable", width)
		return result
	}
	set := func(row int, value string) {
		if row >= 0 && row < height {
			result[row] = fitCell(value, width)
		}
	}
	set(0, padCenter("F O C U S", width))
	set(2, padCenter(task.Title, width))
	set(4, padCenter("STARTED  "+task.CreatedAt.Local().Format("02 Jan · 15:04")+"    PRIORITY  "+taskPriority(task), width))
	barRow := maxInt(0, height-4)
	set(6, "CONTENT")
	keys := make([]string, 0, len(task.Content))
	for key := range task.Content {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	contentRows := maxInt(0, barRow-8)
	if len(keys) == 0 && contentRows > 0 {
		set(7, "  No content · press a to add a key")
	} else if contentRows > 0 {
		for i, key := range keys {
			if i >= contentRows {
				set(7+contentRows-1, fmt.Sprintf("  … %d more", len(keys)-contentRows+1))
				break
			}
			set(7+i, fmt.Sprintf("  %s: %s", sanitize(key), sanitize(task.Content[key])))
		}
	}
	if barRow < height {
		result[barRow] = responsiveFocusBar(width, task.Progress)
	}
	set(barRow+1, padCenter("TASK PROGRESS", width))
	set(height-2, padCenter("a content · d delete · P priority · p progress · Esc outline", width))
	_ = now
	for i := range result {
		if result[i] == "" {
			result[i] = strings.Repeat(" ", width)
		}
	}
	return result
}

func responsiveFocusBar(width, progress int) string {
	barWidth := clampInt(width-8, 8, 52)
	left := (width - barWidth) / 2
	filled := progress * barWidth / 100
	var bar strings.Builder
	bar.WriteString(strings.Repeat(" ", left))
	for i := 0; i < barWidth; i++ {
		if i == filled && progress > 0 && progress < 100 {
			bar.WriteString("\x1b[97m━")
		} else if i < filled {
			bar.WriteString("\x1b[38;5;48m━")
		} else {
			bar.WriteString("\x1b[38;5;240m─")
		}
	}
	bar.WriteString(resetBG + strings.Repeat(" ", width-left-barWidth))
	return bar.String()
}

func (a *app) footerForWidth(width int) string {
	if !a.focusAvailable() {
		return "\x1b[33mh/←" + resetBG + " sidebar  \x1b[33ml/→" + resetBG + " tasks  \x1b[33m↑/↓" + resetBG + " select  \x1b[31md" + resetBG + " delete  \x1b[33mq" + resetBG + " quit"
	}
	if a.focusTaskID != 0 {
		if width < 80 {
			return "\x1b[33ma" + resetBG + " +  \x1b[31md" + resetBG + " −  \x1b[33mP" + resetBG + " pri  \x1b[33mp" + resetBG + " %  \x1b[33mesc" + resetBG + "  \x1b[33mq" + resetBG
		}
		return "\x1b[33ma" + resetBG + " content  \x1b[31md" + resetBG + " delete key  \x1b[33mP" + resetBG + " priority  \x1b[33mp" + resetBG + " progress  \x1b[33mesc" + resetBG + " outline  \x1b[33mq" + resetBG + " quit"
	}
	if width < 80 {
		return "\x1b[33mh/l" + resetBG + " panes  \x1b[33m↑/↓" + resetBG + " select  \x1b[33m?" + resetBG + " help  \x1b[33mq" + resetBG + " quit"
	}
	return "\x1b[33mh/←" + resetBG + " sidebar  \x1b[33ml/→" + resetBG + " tasks  \x1b[33m↑/↓" + resetBG + " select  \x1b[33mb" + resetBG + " blocked  \x1b[31md" + resetBG + " delete  \x1b[33mf" + resetBG + " focus  \x1b[33mq" + resetBG + " quit"
}

func (a *app) topBar() string {
	if !a.focusAvailable() {
		return "\x1b[48;5;252m\x1b[30m A · OUTLINE " + resetBG
	}
	if a.focusTaskID == 0 {
		return "\x1b[48;5;252m\x1b[30m A · OUTLINE " + resetBG + "  \x1b[48;5;238m F · FOCUS " + resetBG
	}
	return "\x1b[48;5;238m A · OUTLINE " + resetBG + "  \x1b[48;5;252m\x1b[30m F · FOCUS " + resetBG
}

func fitCell(value string, width int) string {
	value = sanitize(value)
	runes := []rune(value)
	if len(runes) > width {
		value = string(runes[:maxInt(0, width-1)]) + "…"
	}
	return padRight(value, width)
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
