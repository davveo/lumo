package repository

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/davveo/lumo/internal/platform"
)

const searchCategoryExpression = "CASE WHEN JSON_VALID(payload) THEN LEFT(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.category')), ''), 80) ELSE '' END"
const searchCategoryIndex = "idx_catalog_search_category_v1"

// This versioned virtual column derives exclusively from the authoritative
// payload. Historical ordinary category/brand columns are deliberately unused.
// The index backfills through MySQL's online DDL rather than application writes
// or a cursor that could leave old products temporarily missing from search.
func (r *Repository) migrateSearchCategory(ctx context.Context) error {
	r.searchCategoryIndexed = false
	if r.db.Dialector.Name() != "mysql" {
		return nil
	}
	ctx, span := platform.Start(ctx, "catalog.repository.MigrateSearchCategory")
	defer span.End()
	type column struct {
		DataType               string
		CharacterMaximumLength int64
		Extra                  string
		GenerationExpression   string
	}
	readColumn := func() ([]column, error) {
		rows := []column{}
		err := r.db.WithContext(ctx).Raw("SELECT DATA_TYPE AS data_type, CHARACTER_MAXIMUM_LENGTH AS character_maximum_length, EXTRA AS extra, GENERATION_EXPRESSION AS generation_expression FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?", "catalog_records", "search_category_v1").Scan(&rows).Error
		return rows, err
	}
	columns, err := readColumn()
	if err != nil {
		return fmt.Errorf("inspect catalog search category: %w", err)
	}
	if len(columns) == 0 {
		if err = r.db.WithContext(ctx).Exec("ALTER TABLE catalog_records ADD COLUMN search_category_v1 VARCHAR(80) GENERATED ALWAYS AS (" + searchCategoryExpression + ") VIRTUAL, ALGORITHM=INPLACE, LOCK=NONE").Error; err != nil {
			return fmt.Errorf("create catalog search category online: %w", err)
		}
		columns, err = readColumn()
		if err != nil {
			return fmt.Errorf("verify catalog search category: %w", err)
		}
	}
	if len(columns) != 1 || strings.ToLower(columns[0].DataType) != "varchar" || columns[0].CharacterMaximumLength != 80 || !strings.Contains(strings.ToUpper(columns[0].Extra), "VIRTUAL GENERATED") || normalizeGeneratedExpression(columns[0].GenerationExpression) != normalizeGeneratedExpression(searchCategoryExpression) {
		return fmt.Errorf("catalog search_category_v1 has an unexpected schema; refusing to trust or replace it")
	}
	type indexColumn struct {
		ColumnName string
		SubPart    *int
	}
	readIndex := func() ([]indexColumn, error) {
		rows := []indexColumn{}
		err := r.db.WithContext(ctx).Raw("SELECT COLUMN_NAME AS column_name, SUB_PART AS sub_part FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ? ORDER BY SEQ_IN_INDEX", "catalog_records", searchCategoryIndex).Scan(&rows).Error
		return rows, err
	}
	indexes, err := readIndex()
	if err != nil {
		return fmt.Errorf("inspect catalog category index: %w", err)
	}
	if len(indexes) == 0 {
		if err = r.db.WithContext(ctx).Exec("ALTER TABLE catalog_records ADD INDEX " + searchCategoryIndex + " (kind,parent_id,status,search_category_v1,created_at,id), ALGORITHM=INPLACE, LOCK=NONE").Error; err != nil {
			return fmt.Errorf("create catalog category index online: %w", err)
		}
		indexes, err = readIndex()
		if err != nil {
			return fmt.Errorf("verify catalog category index: %w", err)
		}
	}
	expected := []string{"kind", "parent_id", "status", "search_category_v1", "created_at", "id"}
	if len(indexes) != len(expected) {
		return fmt.Errorf("catalog category index has unexpected columns")
	}
	for i, field := range indexes {
		if field.ColumnName != expected[i] || field.SubPart != nil {
			return fmt.Errorf("catalog category index has unexpected order or prefix")
		}
	}
	r.searchCategoryIndexed = true
	return nil
}

func normalizeGeneratedExpression(value string) string {
	value = strings.ToLower(value)
	// MySQL 8.4 information_schema may escape the quotes in its canonical
	// expression text even though the original DDL used ordinary SQL literals.
	value = strings.ReplaceAll(value, "\\'", "'")
	for _, prefix := range []string{"_utf8mb4", "_utf8mb3", "`"} {
		value = strings.ReplaceAll(value, prefix, "")
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
	for strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		value = value[1 : len(value)-1]
	}
	return value
}
