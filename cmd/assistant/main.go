package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/AndrzejKrzywda00/assistant/internal/store"
	"github.com/AndrzejKrzywda00/assistant/internal/tui"
)

const usage = `assistant - a keyboard-first local task tracker

Usage:
  assistant                         Open the interactive terminal UI
  assistant add [--project NAME] <title>  Add a task to a project
  assistant list [--json] [--all] [--project NAME]
  assistant projects                List projects
  assistant project add <name>      Create a project
  assistant done <id>               Complete a task
  assistant reopen <id>             Reopen a task
  assistant progress <id> <0-100>   Set task completion percentage
  assistant state <id> <state>      Move task to today/blocked/waiting
  assistant delete <id>             Delete a task
  assistant path                    Print the local data file path
  assistant context                 Print a Claude-friendly task summary
  assistant version                 Print version and build information

Set ASSISTANT_DATA_PATH to use a specific data file.`

// Release builds override these using -ldflags. Development builds retain
// readable defaults rather than pretending to be a published version.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "assistant:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	s, err := store.NewDefault()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return tui.Run(s, os.Stdin, os.Stdout)
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Println(usage)
		return nil
	case "path":
		fmt.Println(s.Path())
		return nil
	case "version", "--version", "-v":
		fmt.Printf("assistant %s (commit %s, built %s)\n", version, commit, date)
		return nil
	case "add":
		return addCommand(s, args[1:])
	case "projects":
		projects, err := s.Projects()
		if err != nil {
			return err
		}
		for _, p := range projects {
			fmt.Printf("%d\t%s\n", p.ID, p.Name)
		}
		return nil
	case "project":
		if len(args) < 3 || args[1] != "add" {
			return errors.New("usage: assistant project add <name>")
		}
		p, err := s.AddProject(strings.Join(args[2:], " "))
		if err == nil {
			fmt.Printf("Created project #%d: %s\n", p.ID, p.Name)
		}
		return err
	case "progress":
		if len(args) != 3 {
			return errors.New("progress requires a task id and percentage")
		}
		id, err := store.ParseID(args[1])
		if err != nil {
			return err
		}
		value, err := strconv.Atoi(args[2])
		if err != nil {
			return errors.New("progress must be a number from 0 to 100")
		}
		if err := s.SetProgress(id, value); err != nil {
			return err
		}
		fmt.Printf("Set task #%d progress to %d%%\n", id, value)
		return nil
	case "state":
		if len(args) != 3 {
			return errors.New("state requires a task id and today, blocked, or waiting")
		}
		id, err := store.ParseID(args[1])
		if err != nil {
			return err
		}
		if err := s.SetState(id, args[2]); err != nil {
			return err
		}
		fmt.Printf("Moved task #%d to %s\n", id, strings.ToLower(args[2]))
		return nil
	case "done", "reopen", "delete":
		if len(args) != 2 {
			return fmt.Errorf("%s requires exactly one task id", args[0])
		}
		id, err := store.ParseID(args[1])
		if err != nil {
			return err
		}
		if args[0] == "delete" {
			err = s.Delete(id)
		} else {
			err = s.SetDone(id, args[0] == "done")
		}
		if err == nil {
			fmt.Printf("%s task #%d\n", map[string]string{"done": "Completed", "reopen": "Reopened", "delete": "Deleted"}[args[0]], id)
		}
		return err
	case "list":
		return listCommand(s, args[1:])
	case "context":
		return contextCommand(s)
	default:
		return fmt.Errorf("unknown command %q; run 'assistant help'", args[0])
	}
}

func addCommand(s *store.Store, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	projectName := fs.String("project", "", "project name or ID (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if title == "" {
		return errors.New("add requires a task title")
	}
	if strings.TrimSpace(*projectName) == "" {
		return errors.New("add requires --project NAME")
	}
	project, err := resolveProject(s, *projectName)
	if err != nil {
		return err
	}
	t, err := s.AddToProject(title, project.ID)
	if err == nil {
		fmt.Printf("Added #%d to %s: %s\n", t.ID, project.Name, t.Title)
	}
	return err
}

func resolveProject(s *store.Store, value string) (store.Project, error) {
	projects, err := s.Projects()
	if err != nil {
		return store.Project{}, err
	}
	for _, p := range projects {
		if strings.EqualFold(p.Name, value) || fmt.Sprint(p.ID) == value {
			return p, nil
		}
	}
	return store.Project{}, fmt.Errorf("project %q not found", value)
}

func listCommand(s *store.Store, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "emit machine-readable JSON")
	all := fs.Bool("all", false, "include completed tasks")
	projectName := fs.String("project", "", "only tasks in this project")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tasks, err := s.List(*all)
	if err != nil {
		return err
	}
	if *projectName != "" {
		project, err := resolveProject(s, *projectName)
		if err != nil {
			return err
		}
		filtered := tasks[:0]
		for _, task := range tasks {
			if task.ProjectID == project.ID {
				filtered = append(filtered, task)
			}
		}
		tasks = filtered
	}
	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tasks)
	}
	for _, t := range tasks {
		mark := " "
		if t.Done {
			mark = "x"
		}
		fmt.Printf("%d\t[%s] %s\n", t.ID, mark, t.Title)
	}
	return nil
}

func contextCommand(s *store.Store) error {
	tasks, err := s.List(false)
	if err != nil {
		return err
	}
	fmt.Printf("# Local tasks (%s)\n", time.Now().Format("2006-01-02"))
	if len(tasks) == 0 {
		fmt.Println("No open tasks.")
	}
	for _, t := range tasks {
		fmt.Printf("- [ ] #%d %s\n", t.ID, t.Title)
	}
	return nil
}
