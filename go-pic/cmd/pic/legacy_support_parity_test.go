package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyProjectDiscoveryParity(t *testing.T) {
	makeProject := func(t *testing.T, root string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".pi", "tasks.db"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("finds project with .pi/tasks.db in root", func(t *testing.T) {
		root := t.TempDir()
		makeProject(t, root)
		found, _, _ := scanProjects(root, 5)
		if len(found) != 1 || found[0] != root {
			t.Fatalf("found=%v", found)
		}
	})
	t.Run("finds project with .pi/tasks.db in subdirectory", func(t *testing.T) {
		root := t.TempDir()
		sub := filepath.Join(root, "sub", "project-a")
		makeProject(t, sub)
		found, _, _ := scanProjects(root, 5)
		if len(found) != 1 || found[0] != sub {
			t.Fatalf("found=%v", found)
		}
	})
	t.Run("finds multiple projects", func(t *testing.T) {
		root := t.TempDir()
		a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
		makeProject(t, a)
		makeProject(t, b)
		found, _, _ := scanProjects(root, 5)
		if len(found) != 2 {
			t.Fatalf("found=%v", found)
		}
	})
	t.Run("skips excluded directories", func(t *testing.T) {
		root := t.TempDir()
		for _, name := range []string{"node_modules", ".git", "dist", "build", "coverage", "venv", "__pycache__"} {
			makeProject(t, filepath.Join(root, name, "project"))
		}
		found, _, _ := scanProjects(root, 5)
		if len(found) != 0 {
			t.Fatalf("found=%v", found)
		}
	})
	t.Run("returns empty for directory without projects", func(t *testing.T) {
		root := t.TempDir()
		_ = os.Mkdir(filepath.Join(root, "empty"), 0o755)
		found, _, _ := scanProjects(root, 5)
		if len(found) != 0 {
			t.Fatalf("found=%v", found)
		}
	})
	t.Run("respects maxDepth", func(t *testing.T) {
		root := t.TempDir()
		deep := filepath.Join(root, "a", "b", "c", "d", "e")
		makeProject(t, deep)
		shallow, _, _ := scanProjects(root, 3)
		full, _, _ := scanProjects(root, 10)
		if len(shallow) != 0 || len(full) != 1 {
			t.Fatalf("shallow=%v full=%v", shallow, full)
		}
	})
}

func TestLegacyProjectRegistryParity(t *testing.T) {
	project := func(id, name, root string) registryProject {
		return registryProject{ID: id, Name: name, RootPath: root, DatabasePath: filepath.Join(root, ".pi", "tasks.db"), CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02"}
	}
	t.Run("globalProjectRegistryPath returns correct path", func(t *testing.T) {
		want := filepath.Join("/home/testuser", ".pi", "task-system", "projects.json")
		if got := globalProjectRegistryPath("/home/testuser"); got != want {
			t.Fatalf("%q != %q", got, want)
		}
	})
	t.Run("readProjectRegistry returns empty for missing file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		r := readRegistry()
		if len(r.Projects) != 0 || r.CurrentProjectID != "" {
			t.Fatalf("%#v", r)
		}
	})
	t.Run("readProjectRegistry parses CLI format (snake_case)", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		path := globalProjectRegistryPath(home)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(`{"projects":[{"id":"proj-test","name":"test","root_path":"/tmp/test","database_path":"/tmp/test/.pi/tasks.db","created_at":"a","updated_at":"b"}],"current_project_id":"proj-test"}`), 0o644)
		r := readRegistry()
		if len(r.Projects) != 1 || r.Projects[0].rootPath() != "/tmp/test" || r.CurrentProjectID != "proj-test" {
			t.Fatalf("%#v", r)
		}
	})
	t.Run("readProjectRegistry parses web registry format (camelCase)", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		path := globalProjectRegistryPath(home)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(`{"projects":[{"id":"proj-test","name":"test","rootPath":"/tmp/test","databasePath":"/tmp/test/.pi/tasks.db","createdAt":"a","updatedAt":"b"}],"currentProjectId":"proj-test"}`), 0o644)
		r := readRegistry()
		if r.Projects[0].rootPath() != "/tmp/test" || r.CurrentProjectID != "proj-test" {
			t.Fatalf("%#v", r)
		}
	})
	t.Run("writeProjectRegistry writes valid JSON", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := writeRegistry(projectRegistry{}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(globalProjectRegistryPath(home)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("upsertRegistryProject adds new project", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		root := t.TempDir()
		p, err := upsertProject("one", root, filepath.Join(root, ".pi", "tasks.db"))
		if err != nil {
			t.Fatal(err)
		}
		r := readRegistry()
		if len(r.Projects) != 1 || r.CurrentProjectID != p.ID {
			t.Fatalf("%#v", r)
		}
	})
	t.Run("upsertRegistryProject updates existing project by id", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		root := t.TempDir()
		p, _ := upsertProject("one", root, "db")
		_, _ = upsertProject("updated", root, "db")
		r := readRegistry()
		if len(r.Projects) != 1 || r.Projects[0].ID != p.ID {
			t.Fatalf("%#v", r)
		}
	})
	t.Run("upsertRegistryProject does not duplicate by rootPath", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		root := t.TempDir()
		_, _ = upsertProject("one", root, "db")
		_, _ = upsertProject("two", root, "db")
		if len(readRegistry().Projects) != 1 {
			t.Fatal("duplicated root")
		}
	})
	t.Run("removeRegistryProject removes project by id", func(t *testing.T) {
		r := projectRegistry{Projects: []registryProject{project("p1", "one", "/tmp/one"), project("p2", "two", "/tmp/two")}, CurrentProjectID: "p1"}
		r = removeRegistryProject(r, "p1")
		if len(r.Projects) != 1 || r.CurrentProjectID != "p2" {
			t.Fatalf("%#v", r)
		}
	})
	t.Run("removeRegistryProject handles last project", func(t *testing.T) {
		r := removeRegistryProject(projectRegistry{Projects: []registryProject{project("p1", "one", "/tmp/one")}, CurrentProjectID: "p1"}, "p1")
		if len(r.Projects) != 0 || r.CurrentProjectID != "" {
			t.Fatalf("%#v", r)
		}
	})
	t.Run("findRegistryProject finds by id", func(t *testing.T) {
		p, ok := findRegistryProject(projectRegistry{Projects: []registryProject{project("p1", "test", "/tmp/test")}}, "p1")
		if !ok || p.Name != "test" {
			t.Fatal("not found")
		}
	})
	t.Run("findRegistryProject finds by rootPath", func(t *testing.T) {
		p, ok := findRegistryProject(projectRegistry{Projects: []registryProject{project("p1", "test", "/tmp/test")}}, "/tmp/test")
		if !ok || p.ID != "p1" {
			t.Fatal("not found")
		}
	})
	t.Run("findRegistryProject returns undefined for missing", func(t *testing.T) {
		if _, ok := findRegistryProject(projectRegistry{}, "none"); ok {
			t.Fatal("found missing")
		}
	})
	t.Run("registerProjectInGlobalRegistry registers and is idempotent", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		root := t.TempDir()
		_, _ = upsertProject("one", root, "db")
		_, _ = upsertProject("one", root, "db")
		if len(readRegistry().Projects) != 1 {
			t.Fatal("not idempotent")
		}
	})
	t.Run("read-write roundtrip preserves registry", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		original := projectRegistry{Projects: []registryProject{project("p1", "test", "/tmp/test")}, CurrentProjectID: "p1"}
		if err := writeRegistry(original); err != nil {
			t.Fatal(err)
		}
		got := readRegistry()
		if len(got.Projects) != 1 || got.CurrentProjectID != "p1" || got.Projects[0].Name != "test" {
			t.Fatalf("%#v", got)
		}
	})
}

func TestLegacyWebRequestParity(t *testing.T) {
	decode := func(body string) (map[string]any, error) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		return decodeJSONBody(r)
	}
	t.Run("readJsonBody parses valid JSON", func(t *testing.T) {
		v, err := decode(`{"key":"value"}`)
		if err != nil || v["key"] != "value" {
			t.Fatalf("%v %#v", err, v)
		}
	})
	t.Run("readJsonBody returns error for empty body", func(t *testing.T) {
		_, err := decode("")
		if err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("readJsonBody returns error for invalid JSON", func(t *testing.T) {
		_, err := decode("not json")
		if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("readJsonBody returns error for oversized body", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", bytes.NewReader(bytes.Repeat([]byte("x"), 80000)))
		_, err := decodeJSONBody(r)
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("validateString accepts valid string", func(t *testing.T) {
		if _, err := validateString("hello", "title", 1, 300); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("validateString rejects non-string", func(t *testing.T) {
		if _, err := validateString(123, "title", 1, 300); err == nil || !strings.Contains(err.Error(), "string") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("validateString rejects empty string", func(t *testing.T) {
		if _, err := validateString("", "title", 1, 300); err == nil || !strings.Contains(err.Error(), "at least") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("validateString rejects too long string", func(t *testing.T) {
		if _, err := validateString(strings.Repeat("x", 400), "title", 1, 300); err == nil || !strings.Contains(err.Error(), "at most") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("validateEnum accepts valid value", func(t *testing.T) {
		if _, err := validateEnum("high", "priority", []string{"low", "medium", "high"}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("validateEnum rejects invalid value", func(t *testing.T) {
		if _, err := validateEnum("urgent", "priority", []string{"low", "medium", "high"}); err == nil || !strings.Contains(err.Error(), "one of") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("validateEnum rejects non-string", func(t *testing.T) {
		if _, err := validateEnum(nil, "status", []string{"open", "done"}); err == nil || !strings.Contains(err.Error(), "string") {
			t.Fatalf("%v", err)
		}
	})
}
