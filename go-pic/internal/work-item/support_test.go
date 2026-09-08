package workitem

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/earendil-works/task-system/go-pic/internal/tip"
)

func TestContains(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []string
		value  string
		want   bool
	}{
		{name: "present", values: []string{"a", "b"}, value: "b", want: true},
		{name: "absent", values: []string{"a", "b"}, value: "c", want: false},
		{name: "empty slice", values: nil, value: "a", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(tt.values, tt.value); got != tt.want {
				t.Errorf("contains(%v, %q) = %v, want %v", tt.values, tt.value, got, tt.want)
			}
		})
	}
}

func TestParseOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		want    map[string]string
		wantErr string
	}{
		{name: "pairs", args: []string{"--a", "1", "--b", "2"}, want: map[string]string{"a": "1", "b": "2"}},
		{name: "empty", args: nil, want: map[string]string{}},
		{name: "missing value", args: []string{"--a"}, wantErr: "requires a value"},
		{name: "value looks like option", args: []string{"--a", "--b"}, wantErr: "requires a value"},
		{name: "unexpected positional", args: []string{"positional"}, wantErr: "unexpected argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOptions(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseOptions(%v) error = %v, want contains %q", tt.args, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOptions(%v) unexpected error: %v", tt.args, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseOptions(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestShortID(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		id := shortID()
		if len(id) != 8 {
			t.Fatalf("shortID() = %q, want 8 hex chars", id)
		}
		seen[id] = true
	}
	if len(seen) < 2 {
		t.Fatal("shortID() repeated across 32 calls")
	}
}

func TestHashJSONMatchesTipHashJSON(t *testing.T) {
	t.Parallel()
	value := map[string]any{"stage": "rri_t_scenarios", "revision": 1}
	if got, want := hashJSON(value), tip.HashJSON(value); got != want {
		t.Errorf("hashJSON = %q, want tip.HashJSON %q", got, want)
	}
}

func TestQueryMaps(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO work_item_labels VALUES('wi-1','a'),('wi-1','b')`); err != nil {
		t.Fatal(err)
	}
	rows, err := queryMaps(db, `SELECT work_item_id, label FROM work_item_labels WHERE work_item_id=? ORDER BY label`, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["label"] != "a" || rows[0]["work_item_id"] != "wi-1" {
		t.Errorf("queryMaps rows = %v, want two label rows with string columns", rows)
	}
	empty, err := queryMaps(db, `SELECT label FROM work_item_labels WHERE work_item_id='missing'`)
	if err != nil || len(empty) != 0 {
		t.Errorf("queryMaps no-match = %v, %v; want empty slice, nil", empty, err)
	}
}

func TestRowExists(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO work_item_labels VALUES('wi-1','a')`); err != nil {
		t.Fatal(err)
	}
	if got, err := rowExists(db, `SELECT EXISTS(SELECT 1 FROM work_item_labels WHERE work_item_id=?)`, "wi-1"); err != nil || !got {
		t.Errorf("rowExists present = %v, %v; want true, nil", got, err)
	}
	if got, err := rowExists(db, `SELECT EXISTS(SELECT 1 FROM work_item_labels WHERE work_item_id=?)`, "nope"); err != nil || got {
		t.Errorf("rowExists absent = %v, %v; want false, nil", got, err)
	}
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "out.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(file, map[string]any{"ok": true})
	file.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), `{"ok":true}`) {
		t.Errorf("writeJSON output = %q, want canonical JSON plus newline", data)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("writeJSON must terminate output with a newline")
	}
}

func TestQueryOneErrNoRows(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO work_item_labels VALUES('wi-1','a')`); err != nil {
		t.Fatal(err)
	}
	row, err := queryOne(db, `SELECT label FROM work_item_labels WHERE work_item_id=?`, "wi-1")
	if err != nil || row["label"] != "a" {
		t.Fatalf("queryOne hit = %v, %v", row, err)
	}
	if _, err := queryOne(db, `SELECT label FROM work_item_labels WHERE work_item_id='missing'`); err != sql.ErrNoRows {
		t.Errorf("queryOne miss error = %v, want sql.ErrNoRows", err)
	}
}
