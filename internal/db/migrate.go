// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (s *Store) Migrate(ctx context.Context, dir string) error {
	if _, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version integer PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, path := range files {
		name := filepath.Base(path)
		if strings.HasPrefix(name, "._") {
			continue
		}
		prefix := strings.SplitN(name, "_", 2)[0]
		version, err := strconv.Atoi(strings.TrimLeft(prefix, "0"))
		if err != nil {
			return fmt.Errorf("invalid migration filename %q", name)
		}
		var applied bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4242424401)`); err == nil {
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err == nil && !applied {
				_, err = tx.Exec(ctx, string(sql))
			}
		}
		if err == nil && !applied {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version,name) VALUES($1,$2)`, version, name)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
