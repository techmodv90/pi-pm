// Package workitem holds the Work Item store: items, labels, relations,
// claims, lean execution transitions, artifact saving, RRI-T grading, and
// aggregate verification. The cmd/pic layer keeps only the command dispatch.
package workitem

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/earendil-works/task-system/go-pic/internal/tip"
)

// Queryer and Store mirror the read/write database surfaces the store needs;
// both *sql.DB and *sql.Tx satisfy them.
type Queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

type Store interface {
	Queryer
	Execer
}

type rowQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func queryMaps(db Queryer, query string, args ...any) ([]map[string]any, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	results := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}
		entry := make(map[string]any, len(columns))
		for i, column := range columns {
			value := values[i]
			if raw, ok := value.([]byte); ok {
				value = string(raw)
			}
			entry[column] = value
		}
		results = append(results, entry)
	}
	return results, rows.Err()
}

func rowExists(db rowQueryer, query string, args ...any) (bool, error) {
	var exists int
	if err := db.QueryRow(query, args...).Scan(&exists); err != nil {
		return false, err
	}
	return exists != 0, nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
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

// hashJSON delegates to the tip package so artifact and pack hashing share one
// canonical implementation.
func hashJSON(value any) string {
	return tip.HashJSON(value)
}

func parseOptions(args []string) (map[string]string, error) {
	opts := map[string]string{}
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "--") {
			return nil, fmt.Errorf("unexpected argument: %s", args[i])
		}
		key := strings.TrimPrefix(args[i], "--")
		if key == "" || i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return nil, fmt.Errorf("option --%s requires a value", key)
		}
		opts[key] = args[i+1]
		i++
	}
	return opts, nil
}
