package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
)

// TableHasColumn reports whether the given SQLite table has a column with the
// given name (via PRAGMA table_info). Shared helper — was copy-pasted verbatim
// into the capreg, decision-audit, and resume stores.
func TableHasColumn(tx *sql.Tx, table, column string) (bool, error) {
	escapedTable := strings.ReplaceAll(table, `"`, `""`)
	rows, err := tx.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, escapedTable))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			pk           int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
