package project

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/earendil-works/task-system/go-pic/internal/schema"
	"github.com/earendil-works/task-system/go-pic/internal/store"
)

func Init(args []string) error {
	cwd, _ := os.Getwd()
	root := cwd
	name := filepath.Base(root)
	dbPath := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--path":
			i++
			if i >= len(args) {
				return errors.New("--path requires a value")
			}
			dbPath = args[i]
		case "--name":
			i++
			if i >= len(args) {
				return errors.New("--name requires a value")
			}
			name = args[i]
		case "--root":
			i++
			if i >= len(args) {
				return errors.New("--root requires a value")
			}
			root = args[i]
		default:
			return fmt.Errorf("unknown init option: %s", args[i])
		}
	}
	root, _ = filepath.Abs(root)
	if dbPath == "" {
		dbPath = filepath.Join(root, ".pi", "tasks.db")
	} else if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(root, dbPath)
	}
	if err := InitDB(dbPath); err != nil {
		return err
	}
	project, err := UpsertProject(name, root, dbPath)
	if err != nil {
		return err
	}
	store.WriteJSON(os.Stdout, map[string]any{"initialized": true, "db_path": dbPath, "project": project})
	return nil
}

func ProjectCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("project subcommand required")
	}
	switch args[0] {
	case "create":
		if len(args) < 2 {
			return errors.New("project create requires name")
		}
		if FindDB(".") == "" {
			return errors.New("No task database found. Run: pic init")
		}
		root := ""
		for i := 2; i < len(args); i++ {
			if args[i] != "--root" || i+1 >= len(args) {
				return fmt.Errorf("unknown project create option: %s", args[i])
			}
			root = args[i+1]
			i++
		}
		if root == "" {
			root, _ = os.Getwd()
		}
		root, _ = filepath.Abs(root)
		project, err := UpsertProject(args[1], root, filepath.Join(root, ".pi", "tasks.db"))
		if err != nil {
			return err
		}
		store.WriteJSON(os.Stdout, project)
		return nil
	case "current":
		project, err := CurrentProject()
		if err != nil {
			return err
		}
		store.WriteJSON(os.Stdout, project)
		return nil
	case "list":
		project, err := CurrentProject()
		if err != nil {
			return err
		}
		store.WriteJSON(os.Stdout, []Project{project})
		return nil
	case "register":
		root := ""
		for i := 1; i < len(args); i++ {
			if args[i] != "--root" || i+1 >= len(args) {
				return fmt.Errorf("unknown project register option: %s", args[i])
			}
			root = args[i+1]
			i++
		}
		if root == "" {
			root, _ = os.Getwd()
		}
		root, _ = filepath.Abs(root)
		dbPath := filepath.Join(root, ".pi", "tasks.db")
		if _, err := os.Stat(dbPath); err != nil {
			return fmt.Errorf("No task database found at %s. Run: pic init inside the project directory", dbPath)
		}
		project, err := UpsertProject(filepath.Base(root), root, dbPath)
		if err != nil {
			return err
		}
		store.WriteJSON(os.Stdout, map[string]any{"registered": true, "project": project})
		return nil
	case "scan":
		root, _ := os.Getwd()
		maxDepth := 5
		register := false
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--root":
				i++
				if i >= len(args) {
					return errors.New("--root requires a value")
				}
				root = args[i]
			case "--max-depth":
				i++
				if i >= len(args) {
					return errors.New("--max-depth requires a value")
				}
				fmt.Sscanf(args[i], "%d", &maxDepth)
			case "--register":
				register = true
			default:
				return fmt.Errorf("unknown project scan option: %s", args[i])
			}
		}
		found, visited, scanErrors := ScanProjects(root, maxDepth)
		result := map[string]any{"found_count": len(found), "found_projects": found, "visited": visited}
		if len(scanErrors) > 0 {
			result["errors"] = scanErrors
		}
		if register {
			registered, registrationErrors := []string{}, []string{}
			for _, path := range found {
				if _, err := UpsertProject(filepath.Base(path), path, filepath.Join(path, ".pi", "tasks.db")); err != nil {
					registrationErrors = append(registrationErrors, path+": "+err.Error())
				} else {
					registered = append(registered, path)
				}
			}
			result["registered_count"], result["registered"] = len(registered), registered
			if len(registrationErrors) > 0 {
				result["registration_errors"] = registrationErrors
			}
		}
		store.WriteJSON(os.Stdout, result)
		return nil
	default:
		return fmt.Errorf("unknown project subcommand: %s", args[0])
	}
}

func CurrentProject() (Project, error) {
	cwd, _ := os.Getwd()
	dbPath := FindDB(cwd)
	root := cwd
	if dbPath != "" {
		root = filepath.Dir(filepath.Dir(dbPath))
	}
	root, _ = filepath.Abs(root)
	name := filepath.Base(root)
	return BuildProject(name, root, dbPath), nil
}

func BuildProject(name, root, dbPath string) Project {
	registry := ReadRegistry()
	resolvedRoot := RealpathOrAbs(root)
	for _, p := range registry.Projects {
		if RealpathOrAbs(p.RootDir()) == resolvedRoot {
			return Project{
				ID:            p.ID,
				Name:          FirstNonEmpty(p.Name, name),
				RootPath:      root,
				DatabasePath:  FirstNonEmpty(p.DBPath(), dbPath, filepath.Join(root, ".pi", "tasks.db")),
				ChangelogPath: FirstNonEmpty(p.ChangelogFile(), filepath.Join(root, "CHANGELOG.md")),
				CreatedAt:     FirstNonEmpty(p.Created(), NowISO()),
				UpdatedAt:     NowISO(),
			}
		}
	}
	return Project{
		ID:            "proj-" + store.ShortID(),
		Name:          name,
		RootPath:      root,
		DatabasePath:  FirstNonEmpty(dbPath, filepath.Join(root, ".pi", "tasks.db")),
		ChangelogPath: filepath.Join(root, "CHANGELOG.md"),
		CreatedAt:     NowISO(),
		UpdatedAt:     NowISO(),
	}
}

func UpsertProject(name, root, dbPath string) (Project, error) {
	project := BuildProject(name, root, dbPath)
	registry := ReadRegistry()
	entry := RegistryProject{
		ID:            project.ID,
		Name:          project.Name,
		RootPath:      project.RootPath,
		DatabasePath:  project.DatabasePath,
		ChangelogPath: project.ChangelogPath,
		CreatedAt:     project.CreatedAt,
		UpdatedAt:     project.UpdatedAt,
	}
	matched := false
	for i, p := range registry.Projects {
		if p.ID == entry.ID || RealpathOrAbs(p.RootDir()) == RealpathOrAbs(entry.RootPath) {
			registry.Projects[i] = entry
			matched = true
			break
		}
	}
	if !matched {
		registry.Projects = append(registry.Projects, entry)
	}
	if registry.CurrentProjectID == "" {
		registry.CurrentProjectID = entry.ID
	}
	if err := WriteRegistry(registry); err != nil {
		return Project{}, err
	}
	return project, nil
}

func OpenSQLite(path string) (*sql.DB, error) {
	dsn := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := dsn.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	dsn.RawQuery = query.Encode()
	return sql.Open("sqlite", dsn.String())
}

func InitDB(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	db, err := OpenSQLite(path)
	if err != nil {
		return err
	}
	defer db.Close()
	// Connection-scoped pragmas stay outside the versioned migrations:
	// journal mode persists in the file, but foreign_keys must be re-enabled
	// on every open.
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	return schema.ApplyMigrations(db)
}

func FindDB(start string) string {
	dir, _ := filepath.Abs(start)
	for current := dir; ; current = filepath.Dir(current) {
		candidate := filepath.Join(current, ".pi", "tasks.db")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--git-common-dir")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	commonDir := strings.TrimSpace(string(output))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(dir, commonDir)
	}
	candidate := filepath.Join(filepath.Dir(filepath.Clean(commonDir)), ".pi", "tasks.db")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
