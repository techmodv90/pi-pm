package main

import (
	"errors"
	"fmt"
	"github.com/earendil-works/task-system/go-pic/internal/project"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"os"
	"runtime"

	_ "modernc.org/sqlite"
)

var (
	picVersion = "0.1.0-go"
	picCommit  = "dev"
)

type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RootPath      string `json:"root_path"`
	DatabasePath  string `json:"database_path"`
	ChangelogPath string `json:"changelog_path"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		store.WriteJSON(os.Stderr, map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("command required")
	}
	switch args[0] {
	case "--version", "-v", "version":
		store.WriteJSON(os.Stdout, map[string]any{"name": "pic", "version": picVersion, "implementation": "go", "go": runtime.Version(), "commit": picCommit, "sqlite": "modernc.org/sqlite"})
		return nil
	case "init":
		return project.Init(args[1:])
	case "project":
		return project.ProjectCommand(args[1:])
	case "work-item":
		return cmdWorkItem(args[1:])
	case "workflow":
		return cmdWorkflow(args[1:])
	case "activity":
		return cmdActivity(args[1:])
	case "search":
		return cmdSearch(args[1:])
	case "markdown":
		return cmdMarkdown(args[1:])
	case "web":
		return cmdWeb(args[1:])
	case "list":
		return cmdList(args[1:])
	case "show":
		return cmdShow(args[1:])
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}
