package main

import (
	"database/sql"
	"strings"
)

func tableExists(db *sql.DB, name string) bool {
	var found string
	return db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found) == nil
}

func tableColumns(db *sql.DB, name string) ([]string, error) {
	rows, err := db.Query(`PRAGMA table_info("` + strings.ReplaceAll(name, `"`, `""`) + `")`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := []string{}
	for rows.Next() {
		var cid, notNull, pk int
		var column, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &column, &kind, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}
