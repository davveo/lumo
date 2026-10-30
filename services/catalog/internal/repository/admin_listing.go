package repository

import (
	"context"
	"fmt"

	"github.com/davveo/lumo/internal/platform"
)

const adminListingIndex = "idx_catalog_admin_page"

// Public pages fix a status; admin pages include every status. Their ordering
// needs a separate index, with no status column between the filter and sort.
// Keep this out of AutoMigrate so existing MySQL tables use online DDL.
func (r *Repository) migrateAdminListing(ctx context.Context) (spanErr0 error) {
	ctx, span := platform.Start(ctx, "catalog.repository.MigrateAdminListing")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	expected := []string{"kind", "parent_id", "created_at", "id"}
	if r.db.Dialector.Name() == "sqlite" {
		if err := r.db.WithContext(ctx).Exec(
			"CREATE INDEX IF NOT EXISTS " + adminListingIndex + " ON catalog_records (kind,parent_id,created_at,id)",
		).Error; err != nil {
			return fmt.Errorf("create catalog admin listing index: %w", err)
		}
		var columns []struct{ Name string }
		if err := r.db.WithContext(ctx).Raw("PRAGMA index_info(" + adminListingIndex + ")").Scan(&columns).Error; err != nil {
			return fmt.Errorf("inspect catalog admin listing index: %w", err)
		}
		if len(columns) != len(expected) {
			return fmt.Errorf("catalog admin listing index has unexpected columns")
		}
		for i, column := range columns {
			if column.Name != expected[i] {
				return fmt.Errorf("catalog admin listing index has unexpected column order")
			}
		}
		return nil
	}
	if r.db.Dialector.Name() != "mysql" {
		return nil
	}

	type indexColumn struct {
		ColumnName string
		SubPart    *int
		Collation  string
		NonUnique  int
	}
	readIndex := func() ([]indexColumn, error) {
		var columns []indexColumn
		err := r.db.WithContext(ctx).Raw(
			"SELECT COLUMN_NAME AS column_name, SUB_PART AS sub_part, COLLATION AS collation, NON_UNIQUE AS non_unique FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ? ORDER BY SEQ_IN_INDEX",
			"catalog_records", adminListingIndex,
		).
			Scan(&columns).Error
		return columns, err
	}
	columns, err := readIndex()
	if err != nil {
		return fmt.Errorf("inspect catalog admin listing index: %w", err)
	}
	if len(columns) == 0 {
		ddlErr := r.db.WithContext(ctx).Exec(
			"ALTER TABLE catalog_records ADD INDEX " + adminListingIndex +
				" (kind,parent_id,created_at,id), ALGORITHM=INPLACE, LOCK=NONE",
		).Error
		// A second instance may have created the same index while we waited.
		// Accept that race only after verifying its full definition below.
		columns, err = readIndex()
		if err != nil {
			return fmt.Errorf("verify catalog admin listing index: %w", err)
		}
		if len(columns) == 0 && ddlErr != nil {
			return fmt.Errorf("create catalog admin listing index online: %w", ddlErr)
		}
	}
	if len(columns) != len(expected) {
		return fmt.Errorf("catalog admin listing index has unexpected columns")
	}
	for i, column := range columns {
		if column.ColumnName != expected[i] || column.SubPart != nil || column.Collation != "A" ||
			column.NonUnique != 1 {
			return fmt.Errorf("catalog admin listing index has unexpected order, prefix or uniqueness")
		}
	}
	return nil
}
