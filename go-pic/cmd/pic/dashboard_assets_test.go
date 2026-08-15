package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDashboardBuildDirUsesConfiguredAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("dashboard"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIC_DASHBOARD_DIR", dir)
	if got := dashboardBuildDir(); got != dir {
		t.Fatalf("dashboardBuildDir() = %q, want %q", got, dir)
	}
	if healthData()["dashboard_assets"] != true {
		t.Fatalf("healthData() = %#v", healthData())
	}
}
