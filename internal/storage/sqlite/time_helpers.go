package sqlite

import (
	"fmt"
	"time"
)

// sqliteTimestamp is the canonical datetime format used by SQLite's datetime() function.
// This constant is the single source of truth for all SQLite time parsing in this package.
const sqliteTimestamp = "2006-01-02 15:04:05"

// parseSQLiteTime parses a SQLite datetime string into a time.Time value.
// It returns a wrapped error with the provided context if parsing fails.
func parseSQLiteTime(value, context string) (time.Time, error) {
	t, err := time.Parse(sqliteTimestamp, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: parse time %q: %w", context, value, err)
	}
	return t, nil
}
