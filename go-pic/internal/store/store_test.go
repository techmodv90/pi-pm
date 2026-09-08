package store

import (
	"os"
	"strings"
	"testing"

	"github.com/earendil-works/task-system/go-pic/internal/tip"
)

func TestParseOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		want    map[string]string
		wantErr string
	}{
		{name: "parses pairs", args: []string{"--actor-role", "contractor", "--summary", "ok"}, want: map[string]string{"actor-role": "contractor", "summary": "ok"}},
		{name: "empty args", args: nil, want: map[string]string{}},
		{name: "unexpected argument", args: []string{"stray"}, wantErr: "unexpected argument"},
		{name: "missing value", args: []string{"--actor-role"}, wantErr: "requires a value"},
		{name: "flag before flag", args: []string{"--a", "--b", "x"}, wantErr: "requires a value"},
		{name: "empty key", args: []string{"--", "x"}, wantErr: "requires a value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseOptions(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseOptions error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("opts = %v, want %v", got, tt.want)
			}
			for key, value := range tt.want {
				if got[key] != value {
					t.Errorf("opts[%q] = %q, want %q", key, got[key], value)
				}
			}
		})
	}
}

func TestNormalizeChoice(t *testing.T) {
	t.Parallel()
	if got := NormalizeChoice("high", []string{"low", "high"}, "low"); got != "high" {
		t.Errorf("NormalizeChoice valid = %q, want high", got)
	}
	if got := NormalizeChoice("urgent", []string{"low", "high"}, "low"); got != "low" {
		t.Errorf("NormalizeChoice fallback = %q, want low", got)
	}
}

func TestContainsBoolIntToInt(t *testing.T) {
	t.Parallel()
	if !Contains([]string{"a", "b"}, "b") || Contains([]string{"a"}, "b") {
		t.Error("Contains misbehaves")
	}
	if BoolInt(true) != 1 || BoolInt(false) != 0 {
		t.Error("BoolInt misbehaves")
	}
	if ToInt(int64(7)) != 7 || ToInt(3.5) != 3 || ToInt(nil) != 0 {
		t.Error("ToInt misbehaves")
	}
}

func TestHashJSONMatchesTipHashJSON(t *testing.T) {
	t.Parallel()
	value := map[string]any{"goal": "ship", "files": []string{"a.go", "b.go"}}
	if HashJSON(value) != tip.HashJSON(value) {
		t.Error("HashJSON must equal tip.HashJSON")
	}
	if HashJSON(value) == HashJSON(map[string]any{"goal": "different"}) {
		t.Error("HashJSON must distinguish content")
	}
}

func TestWriteJSON(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(dir + "/out.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	WriteJSON(f, map[string]any{"error": "boom"})
	data, err := os.ReadFile(dir + "/out.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"error\":\"boom\"}\n" {
		t.Errorf("output = %q", string(data))
	}
}
