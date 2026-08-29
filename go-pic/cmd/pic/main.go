package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

type registryProject struct {
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

type projectRegistry struct {
	Projects              []registryProject `json:"projects"`
	CurrentProjectID      string            `json:"currentProjectId"`
	CurrentProjectIDSnake string            `json:"current_project_id,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		writeJSON(os.Stderr, map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("command required")
	}
	switch args[0] {
	case "--version", "-v", "version":
		writeJSON(os.Stdout, map[string]any{"name": "pic", "version": picVersion, "implementation": "go", "go": runtime.Version(), "commit": picCommit, "sqlite": "modernc.org/sqlite"})
		return nil
	case "init":
		return cmdInit(args[1:])
	case "project":
		return cmdProject(args[1:])
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

func cmdInit(args []string) error {
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
	if err := initDB(dbPath); err != nil {
		return err
	}
	project, err := upsertProject(name, root, dbPath)
	if err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"initialized": true, "db_path": dbPath, "project": project})
	return nil
}

func cmdProject(args []string) error {
	if len(args) == 0 {
		return errors.New("project subcommand required")
	}
	switch args[0] {
	case "create":
		if len(args) < 2 {
			return errors.New("project create requires name")
		}
		if findDB(".") == "" {
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
		project, err := upsertProject(args[1], root, filepath.Join(root, ".pi", "tasks.db"))
		if err != nil {
			return err
		}
		writeJSON(os.Stdout, project)
		return nil
	case "current":
		project, err := currentProject()
		if err != nil {
			return err
		}
		writeJSON(os.Stdout, project)
		return nil
	case "list":
		project, err := currentProject()
		if err != nil {
			return err
		}
		writeJSON(os.Stdout, []Project{project})
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
		project, err := upsertProject(filepath.Base(root), root, dbPath)
		if err != nil {
			return err
		}
		writeJSON(os.Stdout, map[string]any{"registered": true, "project": project})
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
		found, visited, scanErrors := scanProjects(root, maxDepth)
		result := map[string]any{"found_count": len(found), "found_projects": found, "visited": visited}
		if len(scanErrors) > 0 {
			result["errors"] = scanErrors
		}
		if register {
			registered, registrationErrors := []string{}, []string{}
			for _, path := range found {
				if _, err := upsertProject(filepath.Base(path), path, filepath.Join(path, ".pi", "tasks.db")); err != nil {
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
		writeJSON(os.Stdout, result)
		return nil
	default:
		return fmt.Errorf("unknown project subcommand: %s", args[0])
	}
}

func currentProject() (Project, error) {
	cwd, _ := os.Getwd()
	dbPath := findDB(cwd)
	root := cwd
	if dbPath != "" {
		root = filepath.Dir(filepath.Dir(dbPath))
	}
	root, _ = filepath.Abs(root)
	name := filepath.Base(root)
	return buildProject(name, root, dbPath), nil
}

func buildProject(name, root, dbPath string) Project {
	registry := readRegistry()
	resolvedRoot := realpathOrAbs(root)
	for _, p := range registry.Projects {
		if realpathOrAbs(p.rootPath()) == resolvedRoot {
			return Project{
				ID:            p.ID,
				Name:          firstNonEmpty(p.Name, name),
				RootPath:      root,
				DatabasePath:  firstNonEmpty(p.databasePath(), dbPath, filepath.Join(root, ".pi", "tasks.db")),
				ChangelogPath: firstNonEmpty(p.changelogPath(), filepath.Join(root, "CHANGELOG.md")),
				CreatedAt:     firstNonEmpty(p.createdAt(), nowISO()),
				UpdatedAt:     nowISO(),
			}
		}
	}
	return Project{
		ID:            "proj-" + shortID(),
		Name:          name,
		RootPath:      root,
		DatabasePath:  firstNonEmpty(dbPath, filepath.Join(root, ".pi", "tasks.db")),
		ChangelogPath: filepath.Join(root, "CHANGELOG.md"),
		CreatedAt:     nowISO(),
		UpdatedAt:     nowISO(),
	}
}

func upsertProject(name, root, dbPath string) (Project, error) {
	project := buildProject(name, root, dbPath)
	registry := readRegistry()
	entry := registryProject{
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
		if p.ID == entry.ID || realpathOrAbs(p.rootPath()) == realpathOrAbs(entry.RootPath) {
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
	if err := writeRegistry(registry); err != nil {
		return Project{}, err
	}
	return project, nil
}

func openSQLite(path string) (*sql.DB, error) {
	dsn := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := dsn.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	dsn.RawQuery = query.Encode()
	return sql.Open("sqlite", dsn.String())
}

func removeLegacyTIPSchema(db *sql.DB) error {
	if !tableExists(db, "tips") && !hasColumn(db, "completion_reports", "tip_id") && !hasColumn(db, "verification_items", "tip_id") && !hasColumn(db, "escalations", "tip_id") {
		return nil
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA foreign_keys=ON`)
	for _, table := range []string{"tip_dependencies", "tip_requirement_links"} {
		if tableExists(db, table) {
			if _, err := db.Exec(`DROP TABLE "` + table + `"`); err != nil {
				return err
			}
		}
	}
	for _, pair := range [][2]string{{"completion_reports", "tip_id"}, {"verification_items", "tip_id"}, {"escalations", "tip_id"}} {
		if tableExists(db, pair[0]) && hasColumn(db, pair[0], pair[1]) {
			if _, err := db.Exec(`ALTER TABLE "` + pair[0] + `" DROP COLUMN "` + pair[1] + `"`); err != nil {
				return err
			}
		}
	}
	if tableExists(db, "tips") {
		if _, err := db.Exec(`DROP TABLE tips`); err != nil {
			return err
		}
	}
	return nil
}

func initDB(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	db, err := openSQLite(path)
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
	return applySchemaMigrations(db)
}

func migrateLegacyWorkItemInstructionPacks(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT OR IGNORE INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash,created_at)
		SELECT 'wia-migrated-tip-'||p.task_id,p.task_id,'task_graph',COALESCE((SELECT MAX(a.revision)+1 FROM work_item_artifacts a WHERE a.work_item_id=p.task_id AND a.stage='task_graph'),1),'{}',p.content_hash,p.created_at
		FROM task_instruction_packs p JOIN work_items wi ON wi.id=p.task_id
		WHERE p.version=(SELECT MAX(latest.version) FROM task_instruction_packs latest WHERE latest.task_id=p.task_id)
		AND NOT EXISTS(SELECT 1 FROM work_item_instruction_packs canonical WHERE canonical.work_item_id=p.task_id)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type,created_at)
		SELECT 'wic-migrated-tip-'||a.work_item_id,a.work_item_id,'task_graph',a.id,a.revision,a.content_hash,'migrated_legacy_tip',a.created_at
		FROM work_item_artifacts a WHERE a.id='wia-migrated-tip-'||a.work_item_id`); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash,activated_at,stale_at,created_at)
		SELECT p.id,p.task_id,'wic-migrated-tip-'||p.task_id,p.version,
			CASE p.status WHEN 'active' THEN 'active' WHEN 'draft' THEN 'inactive' ELSE 'stale' END,
			json_object('goal',p.goal,'module',p.module,'estimated_effort_minutes',p.estimated_effort_minutes,'files',json(p.files_json),'patterns',json(COALESCE(p.patterns_json,'[]')),'business_rules',json(p.business_rules_json),'validation_rules',json(p.validation_rules_json),'error_handling',json(p.error_handling_json),'state_transitions',json(p.state_transitions_json),'contract_obligations',json(p.contract_obligations_json),'constraints',json(p.constraints_json),'verification',json(p.verification_json),'requirement_snapshots',json(p.requirement_snapshots_json),'content_schema_version',p.content_schema_version,'skill_families',json(p.skill_families_json)),
			p.content_hash,p.activated_at,COALESCE(NULLIF(p.stale_at,''),p.superseded_at),p.created_at
		FROM task_instruction_packs p JOIN work_items wi ON wi.id=p.task_id
		WHERE EXISTS(SELECT 1 FROM workflow_checkpoints checkpoint WHERE checkpoint.id='wic-migrated-tip-'||p.task_id)
		AND p.version=(SELECT MAX(latest.version) FROM task_instruction_packs latest WHERE latest.task_id=p.task_id)
		AND NOT EXISTS(SELECT 1 FROM work_item_instruction_packs canonical WHERE canonical.work_item_id=p.task_id)`); err != nil {
		return err
	}
	return tx.Commit()
}

func findDB(start string) string {
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

func globalProjectRegistryPath(home string) string {
	return filepath.Join(home, ".pi", "task-system", "projects.json")
}
func registryPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return globalProjectRegistryPath(home)
}

func readRegistry() projectRegistry {
	var registry projectRegistry
	data, err := os.ReadFile(registryPath())
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

func scanProjects(root string, maxDepth int) ([]string, int, []string) {
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
			if !entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || contains([]string{"node_modules", "dist", "build", "coverage", "venv", "__pycache__"}, name) {
				continue
			}
			walk(filepath.Join(dir, name), depth+1)
		}
	}
	walk(root, 0)
	return found, visited, scanErrors
}

func removeRegistryProject(registry projectRegistry, id string) projectRegistry {
	projects := []registryProject{}
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

func findRegistryProject(registry projectRegistry, value string) (registryProject, bool) {
	for _, project := range registry.Projects {
		project = project.normalized()
		if project.ID == value || project.Name == value || realpathOrAbs(project.rootPath()) == realpathOrAbs(value) {
			return project, true
		}
	}
	return registryProject{}, false
}

func writeRegistry(registry projectRegistry) error {
	path := registryPath()
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

func writeJSON(file *os.File, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(file, `{"error":%q}`+"\n", err.Error())
		return
	}
	file.Write(append(data, '\n'))
}

func shortID() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strings.ToLower(hex.EncodeToString([]byte(fmt.Sprint(time.Now().UnixNano()))))[:8]
	}
	return hex.EncodeToString(bytes[:])
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func realpathOrAbs(path string) string {
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

func (p registryProject) rootPath() string {
	return firstNonEmpty(p.RootPath, p.RootPathSnake)
}

func (p registryProject) databasePath() string {
	return firstNonEmpty(p.DatabasePath, p.DatabasePathSnake)
}

func (p registryProject) changelogPath() string {
	return firstNonEmpty(p.ChangelogPath, p.ChangelogPathSnake)
}

func (p registryProject) createdAt() string {
	return firstNonEmpty(p.CreatedAt, p.CreatedAtSnake)
}

func (p registryProject) updatedAt() string {
	return firstNonEmpty(p.UpdatedAt, p.UpdatedAtSnake)
}

func (p registryProject) normalized() registryProject {
	return registryProject{
		ID:            p.ID,
		Name:          p.Name,
		RootPath:      p.rootPath(),
		DatabasePath:  p.databasePath(),
		ChangelogPath: p.changelogPath(),
		CreatedAt:     p.createdAt(),
		UpdatedAt:     p.updatedAt(),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
