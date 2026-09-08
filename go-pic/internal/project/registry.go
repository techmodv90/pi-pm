package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/earendil-works/task-system/go-pic/internal/store"
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

type RegistryProject struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	RootPath           string `json:"rootPath"`
	RootPathSnake      string `json:"root_path,omitempty"`
	DatabasePath       string `json:"databasePath"`
	DatabasePathSnake  string `json:"database_path,omitempty"`
	ChangelogPath      string `json:"changelogPath,omitempty"`
	ChangelogPathSnake string `json:"changelog_path,omitempty"`
	CreatedAt          string `json:"createdAt"`
	CreatedAtSnake     string `json:"created_at,omitempty"`
	UpdatedAt          string `json:"updatedAt"`
	UpdatedAtSnake     string `json:"updated_at,omitempty"`
}

type Registry struct {
	Projects              []RegistryProject `json:"projects"`
	CurrentProjectID      string            `json:"currentProjectId"`
	CurrentProjectIDSnake string            `json:"current_project_id,omitempty"`
}

func GlobalProjectRegistryPath(home string) string {
	return filepath.Join(home, ".pi", "task-system", "projects.json")
}

func RegistryPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return GlobalProjectRegistryPath(home)
}

func ReadRegistry() Registry {
	var registry Registry
	data, err := os.ReadFile(RegistryPath())
	if err != nil {
		return registry
	}
	_ = json.Unmarshal(data, &registry)
	if registry.CurrentProjectID == "" {
		registry.CurrentProjectID = registry.CurrentProjectIDSnake
	}
	if registry.CurrentProjectID == "" && len(registry.Projects) > 0 {
		registry.CurrentProjectID = registry.Projects[0].ID
	}
	for i := range registry.Projects {
		registry.Projects[i] = registry.Projects[i].normalized()
	}
	return registry
}

func ScanProjects(root string, maxDepth int) ([]string, int, []string) {
	root, _ = filepath.Abs(root)
	found, scanErrors := []string{}, []string{}
	visited := 0
	var walk func(string, int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		visited++
		if _, err := os.Stat(filepath.Join(dir, ".pi", "tasks.db")); err == nil {
			found = append(found, dir)
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			scanErrors = append(scanErrors, dir+": "+err.Error())
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || store.Contains([]string{"node_modules", "dist", "build", "coverage", "venv", "__pycache__"}, name) {
				continue
			}
			walk(filepath.Join(dir, name), depth+1)
		}
	}
	walk(root, 0)
	return found, visited, scanErrors
}

func RemoveRegistryProject(registry Registry, id string) Registry {
	projects := []RegistryProject{}
	for _, project := range registry.Projects {
		if project.ID != id {
			projects = append(projects, project)
		}
	}
	registry.Projects = projects
	if registry.CurrentProjectID == id {
		registry.CurrentProjectID = ""
		if len(projects) > 0 {
			registry.CurrentProjectID = projects[0].ID
		}
	}
	return registry
}

func FindRegistryProject(registry Registry, value string) (RegistryProject, bool) {
	for _, project := range registry.Projects {
		project = project.normalized()
		if project.ID == value || project.Name == value || RealpathOrAbs(project.RootDir()) == RealpathOrAbs(value) {
			return project, true
		}
	}
	return RegistryProject{}, false
}

func WriteRegistry(registry Registry) error {
	path := RegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func RealpathOrAbs(path string) string {
	if path == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		return abs
	}
	return path
}

func (p RegistryProject) RootDir() string {
	return FirstNonEmpty(p.RootPath, p.RootPathSnake)
}

func (p RegistryProject) DBPath() string {
	return FirstNonEmpty(p.DatabasePath, p.DatabasePathSnake)
}

func (p RegistryProject) ChangelogFile() string {
	return FirstNonEmpty(p.ChangelogPath, p.ChangelogPathSnake)
}

func (p RegistryProject) Created() string {
	return FirstNonEmpty(p.CreatedAt, p.CreatedAtSnake)
}

func (p RegistryProject) Updated() string {
	return FirstNonEmpty(p.UpdatedAt, p.UpdatedAtSnake)
}

func (p RegistryProject) normalized() RegistryProject {
	return RegistryProject{
		ID:            p.ID,
		Name:          p.Name,
		RootPath:      p.RootDir(),
		DatabasePath:  p.DBPath(),
		ChangelogPath: p.ChangelogFile(),
		CreatedAt:     p.Created(),
		UpdatedAt:     p.Updated(),
	}
}

func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
