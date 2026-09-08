package schema

func RemoveLegacyTIPSchema(db DB) error {
	if !TableExists(db, "tips") && !HasColumn(db, "completion_reports", "tip_id") && !HasColumn(db, "verification_items", "tip_id") && !HasColumn(db, "escalations", "tip_id") {
		return nil
	}
	for _, table := range []string{"tip_dependencies", "tip_requirement_links"} {
		if TableExists(db, table) {
			if _, err := db.Exec(`DROP TABLE "` + table + `"`); err != nil {
				return err
			}
		}
	}
	for _, pair := range [][2]string{{"completion_reports", "tip_id"}, {"verification_items", "tip_id"}, {"escalations", "tip_id"}} {
		if TableExists(db, pair[0]) && HasColumn(db, pair[0], pair[1]) {
			if _, err := db.Exec(`ALTER TABLE "` + pair[0] + `" DROP COLUMN "` + pair[1] + `"`); err != nil {
				return err
			}
		}
	}
	if TableExists(db, "tips") {
		if _, err := db.Exec(`DROP TABLE tips`); err != nil {
			return err
		}
	}
	return nil
}
