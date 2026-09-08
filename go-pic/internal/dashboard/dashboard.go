package dashboard

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/earendil-works/task-system/go-pic/internal/project"
	"github.com/earendil-works/task-system/go-pic/internal/store"
)

// Version is injected by the pic CLI entrypoint so the dashboard health
// payload reports the binary version.
var Version string

func Web(args []string) error {
	port, host := "4377", "127.0.0.1"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			i++
			if i >= len(args) {
				return errors.New("--port requires a value")
			}
			port = args[i]
		case "--host":
			i++
			if i >= len(args) {
				return errors.New("--host requires a value")
			}
			host = args[i]
		case "--unsafe-allow-network":
		default:
			return fmt.Errorf("unknown web option: %s", args[i])
		}
	}
	mux := http.NewServeMux()
	health := func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, healthData())
	}
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/healthz", health)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { HandleAPI(w, r) })
	mux.HandleFunc("/", serveDashboard)
	fmt.Fprintf(os.Stderr, "pic web listening on http://%s:%s\n", host, port)
	return http.ListenAndServe(host+":"+port, mux)
}

func healthData() map[string]any {
	return map[string]any{"ok": true, "implementation": "go", "version": Version, "dashboard_assets": dashboardBuildDir() != ""}
}

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	buildDir := dashboardBuildDir()
	if buildDir == "" {
		http.Error(w, "dashboard assets not found", http.StatusNotFound)
		return
	}
	path := filepath.Join(buildDir, strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), string(os.PathSeparator)))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, filepath.Join(buildDir, "index.html"))
}

func dashboardBuildDir() string {
	exe, _ := os.Executable()
	candidates := []string{
		os.Getenv("PIC_DASHBOARD_DIR"),
		filepath.Join(filepath.Dir(exe), "..", "web", "build"),
		filepath.Join("go-pic", "web", "build"),
		filepath.Join("web", "build"),
	}
	for _, dir := range candidates {
		if dir != "" {
			if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
				abs, _ := filepath.Abs(dir)
				return abs
			}
		}
	}
	return ""
}

func writeJSONResponse(w http.ResponseWriter, value any) { writeJSONStatus(w, http.StatusOK, value) }

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	data, _ := jsonMarshal(value)
	_, _ = w.Write(data)
}

func DecodeJSONBody(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	const maxBytes = 65536
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, errors.New("failed to read request body")
	}
	if len(data) == 0 {
		return nil, errors.New("request body is required")
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("request body exceeds %d byte limit", maxBytes)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, errors.New("invalid JSON in request body")
	}
	return body, nil
}

func ValidateString(value any, field string, minLength, maxLength int) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	text = strings.TrimSpace(text)
	if len(text) < minLength {
		return "", fmt.Errorf("%s must be at least %d character(s)", field, minLength)
	}
	if len(text) > maxLength {
		return "", fmt.Errorf("%s must be at most %d characters", field, maxLength)
	}
	return text, nil
}

func ValidateEnum(value any, field string, allowed []string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	if !store.Contains(allowed, text) {
		return "", fmt.Errorf("%s must be one of: %s", field, strings.Join(allowed, ", "))
	}
	return text, nil
}

func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }

func openProjectDB(path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("No database path provided")
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Database not found: %s", path)
		}
		return nil, err
	}
	path, _ = filepath.Abs(path)
	projectSchemaMu.Lock()
	if !initializedProjectSchema[path] {
		db, err := project.OpenSQLite(path)
		if err != nil {
			projectSchemaMu.Unlock()
			return nil, fmt.Errorf("Failed to open database %s: %w", path, err)
		}
		_ = db.Close()
		if err := project.InitDB(path); err != nil {
			projectSchemaMu.Unlock()
			return nil, fmt.Errorf("Failed to update database schema %s: %w", path, err)
		}
		initializedProjectSchema[path] = true
	}
	projectSchemaMu.Unlock()
	db, err := project.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to open database %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func closeProjectDB(db *sql.DB) {
	if db != nil {
		_ = db.Close()
	}
}
