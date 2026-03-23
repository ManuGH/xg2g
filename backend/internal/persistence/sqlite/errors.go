package sqlite

import (
	"errors"

	sqlite3 "modernc.org/sqlite/lib"
)

type sqliteCodeError interface {
	error
	Code() int
}

func errorCode(err error) (int, bool) {
	var sqliteErr sqliteCodeError
	if !errors.As(err, &sqliteErr) {
		return 0, false
	}
	return sqliteErr.Code(), true
}

func IsBusySnapshot(err error) bool {
	code, ok := errorCode(err)
	return ok && code == sqlite3.SQLITE_BUSY_SNAPSHOT
}

func IsBusyRetryable(err error) bool {
	code, ok := errorCode(err)
	return ok && (code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_BUSY_SNAPSHOT)
}
