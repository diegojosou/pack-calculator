// Package store persists the configured set of pack sizes so operator
// changes survive restarts.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"

	_ "modernc.org/sqlite"
)

type Store interface {
	GetPackSizes(ctx context.Context) ([]int, error)
	SetPackSizes(ctx context.Context, sizes []int) error
	Close() error
}

var ErrInvalidPackSize = errors.New("pack sizes must be positive integers")

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single connection avoids the modernc driver's "database is locked"
	// surprise when multiple conns are opened on the same file.
	db.SetMaxOpenConns(1)

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	const schema = `
		CREATE TABLE IF NOT EXISTS pack_sizes (
			size INTEGER PRIMARY KEY CHECK (size > 0)
		);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetPackSizes(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT size FROM pack_sizes ORDER BY size ASC")
	if err != nil {
		return nil, fmt.Errorf("query pack sizes: %w", err)
	}
	defer rows.Close()

	var sizes []int
	for rows.Next() {
		var size int
		if err := rows.Scan(&size); err != nil {
			return nil, fmt.Errorf("scan pack size: %w", err)
		}
		sizes = append(sizes, size)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pack sizes: %w", err)
	}
	return sizes, nil
}

// SetPackSizes replaces the entire set inside a transaction so readers never
// see a partially-written state.
func (s *SQLiteStore) SetPackSizes(ctx context.Context, sizes []int) error {
	cleaned, err := dedupePositive(sizes)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM pack_sizes"); err != nil {
		return fmt.Errorf("clear pack sizes: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO pack_sizes (size) VALUES (?)")
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()
	for _, size := range cleaned {
		if _, err := stmt.ExecContext(ctx, size); err != nil {
			return fmt.Errorf("insert pack size %d: %w", size, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// SeedIfEmpty inserts defaults only when the store is empty, so operator
// changes aren't clobbered on subsequent boots.
func (s *SQLiteStore) SeedIfEmpty(ctx context.Context, defaults []int) error {
	existing, err := s.GetPackSizes(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	return s.SetPackSizes(ctx, defaults)
}

type MemoryStore struct {
	mu    sync.RWMutex
	sizes []int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (m *MemoryStore) GetPackSizes(_ context.Context) ([]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]int, len(m.sizes))
	copy(out, m.sizes)
	return out, nil
}

func (m *MemoryStore) SetPackSizes(_ context.Context, sizes []int) error {
	cleaned, err := dedupePositive(sizes)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.sizes = cleaned
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) Close() error { return nil }

func dedupePositive(in []int) ([]int, error) {
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, s := range in {
		if s <= 0 {
			return nil, ErrInvalidPackSize
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Ints(out)
	return out, nil
}
