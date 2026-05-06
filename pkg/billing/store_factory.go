package billing

import (
	"fmt"
)

// NewStore creates a Store (and optionally AdminStore) for the given driver.
// driver: "sqlite" or "postgres". dsn: connection string or file path.
// Returns (Store, AdminStore, error) — AdminStore is nil for drivers that don't support it.
func NewStore(driver, dsn string) (Store, AdminStore, error) {
	switch driver {
	case "sqlite", "":
		s, err := NewSQLiteStore(dsn)
		if err != nil {
			return nil, nil, err
		}
		return s, s, nil
	case "postgres":
		s, err := NewPostgresStore(dsn)
		if err != nil {
			return nil, nil, err
		}
		return s, s, nil
	default:
		return nil, nil, fmt.Errorf("billing: unknown store driver %q", driver)
	}
}
