package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/earendil-works/task-system/go-pic/internal/tip"
)

type Queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func QueryOne(db Queryer, query string, args ...any) (map[string]any, error) {
	rows, err := QueryMaps(db, query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("not found")
	}
	return rows[0], nil
}

func QueryMaps(db Queryer, query string, args ...any) ([]map[string]any, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, col := range cols {
			row[col] = NormalizeDBValue(values[i])
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func NormalizeDBValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	case nil:
		return nil
	default:
		return v
	}
}

func RowExists(db RowQueryer, query string, args ...any) (bool, error) {
	var one int
	err := db.QueryRow(query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func NormalizeChoice(value string, allowed []string, fallback string) string {
	if Contains(allowed, value) {
		return value
	}
	return fallback
}

func Contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func BoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func ToInt(value any) int {
	switch v := value.(type) {
	case int64:
		return int(v)
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

type Execer interface {
	Exec(string, ...any) (sql.Result, error)
}

type RowQueryer interface {
	QueryRow(string, ...any) *sql.Row
}

type Store interface {
	Queryer
	Execer
}

func ParseOptions(args []string) (map[string]string, error) {
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

func OutputOne(db Queryer, query string, args ...any) error {
	row, err := QueryOne(db, query, args...)
	if err != nil {
		return err
	}
	WriteJSON(os.Stdout, row)
	return nil
}

func PersistedText(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func NullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func AddEvent(db Execer, workItemID, eventType, role, summary string, payload any) error {
	return AddEventWithModel(db, workItemID, eventType, role, "", summary, payload)
}

func AddEventWithModel(db Execer, workItemID, eventType, role, model, summary string, payload any) error {
	data, _ := json.Marshal(payload)
	_, err := db.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,actor_model,summary,payload_json) VALUES(?,?,?,?,?,?,?)`, "wie-"+ShortID(), workItemID, eventType, role, model, summary, string(data))
	return err
}

func VerificationText(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func NormalizeJSONText(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	var parsed any
	if json.Unmarshal([]byte(value), &parsed) != nil {
		return value
	}
	data, _ := json.Marshal(parsed)
	return string(data)
}

func WriteJSON(file *os.File, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(file, `{"error":%q}`+"\n", err.Error())
		return
	}
	file.Write(append(data, '\n'))
}

func ShortID() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strings.ToLower(hex.EncodeToString([]byte(fmt.Sprint(time.Now().UnixNano()))))[:8]
	}
	return hex.EncodeToString(bytes[:])
}

// hashJSON delegates to the tip package so artifact and pack hashing share one
// canonical implementation.
func HashJSON(value any) string {
	return tip.HashJSON(value)
}
