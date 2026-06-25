package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	storepkg "github.com/heruujoko/piramid/internal/store"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

var _ storepkg.Store = (*Store)(nil)

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	st := &Store{db: db}
	ctx := context.Background()
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", statement, err)
		}
	}
	if err := st.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Health(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
