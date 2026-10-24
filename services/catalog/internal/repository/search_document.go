package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

const searchDocumentIndex = "idx_catalog_search_document_v2"

type searchProjectionColumn struct {
	name, definition, expression, kind string
	length                             int64
}

func mysqlSearchText(path string) string {
	return "JSON_UNQUOTE(JSON_EXTRACT(payload, '" + path + "'))"
}

func searchDocumentColumns() []searchProjectionColumn {
	name, brand, category := mysqlSearchText("$.name"), mysqlSearchText("$.brand"), mysqlSearchText("$.category")
	// CHAR returns a binary string by default. Convert the joined document back
	// to UTF-8 before LOWER/CHAR_LENGTH, preserving case folding and rune counts.
	joined := "CONVERT(CONCAT(COALESCE(" + name + ", ''), CHAR(0), COALESCE(" + brand + ", ''), CHAR(0), COALESCE(" +
		category +
		", '')) USING utf8mb4)"
	guard := func(expression, fallback string) string {
		return "CASE WHEN JSON_VALID(payload) THEN " + expression + " ELSE " + fallback + " END"
	}
	visible := "CASE WHEN kind = '" + constants.CatalogKindSku + "' AND parent_id = '' AND status = '" +
		constants.CatalogStatusActive +
		"' AND JSON_VALID(payload) THEN CASE WHEN COALESCE(CAST(" +
		mysqlSearchText(
			"$.catalog_visible",
		) + " AS CHAR), 'true') NOT IN ('false', '0') THEN 1 ELSE 0 END ELSE 0 END"
	long := "CASE WHEN CHAR_LENGTH(" + joined + ") > 300 OR CHAR_LENGTH(COALESCE(" + brand +
		", '')) > 80 OR CHAR_LENGTH(COALESCE(" +
		category +
		", '')) > 80 OR CHAR_LENGTH(COALESCE(" +
		mysqlSearchText(
			"$.price_cents",
		) + ", '0')) > 32 OR CHAR_LENGTH(COALESCE(" + mysqlSearchText(
		"$.sales",
	) + ", '0')) > 32 THEN 1 ELSE 0 END"
	return []searchProjectionColumn{
		{"search_public_v2", "TINYINT", visible, "tinyint", 0},
		{"search_long_v2", "TINYINT", guard(long, "0"), "tinyint", 0},
		{
			"search_category_v2",
			"VARCHAR(80) COLLATE utf8mb4_bin",
			guard("LEFT(COALESCE("+category+", ''), 80)", "''"),
			"varchar",
			80,
		},
		{
			"search_brand_v2",
			"VARCHAR(80) COLLATE utf8mb4_bin",
			guard("LEFT(COALESCE("+brand+", ''), 80)", "''"),
			"varchar",
			80,
		},
		{
			"search_text_v2",
			"VARCHAR(300) COLLATE utf8mb4_bin",
			guard("LEFT(LOWER("+joined+"), 300)", "''"),
			"varchar",
			300,
		},
		// Keep numeric text in the index and cast only during reads. This keeps
		// MySQL's existing SELECT cast semantics even for historical nonnumeric
		// JSON, without introducing strict generated-column conversion failures.
		{
			"search_price_v2",
			"VARCHAR(32) COLLATE utf8mb4_bin",
			guard("LEFT(COALESCE("+mysqlSearchText("$.price_cents")+", '0'), 32)", "'0'"),
			"varchar",
			32,
		},
		{
			"search_sales_v2",
			"VARCHAR(32) COLLATE utf8mb4_bin",
			guard("LEFT(COALESCE("+mysqlSearchText("$.sales")+", '0'), 32)", "'0'"),
			"varchar",
			32,
		},
	}
}

// Search projections are generated from the authoritative row and indexed by
// online DDL. Every writer, including administrators and seeders, updates them
// atomically; no backfill job, trigger or eventually consistent mirror exists.
// Short documents use only the covering index. Long documents retain a separate
// exact JSON branch, so bounded index fields can never truncate search results.
func (r *Repository) migrateSearchDocument(ctx context.Context) error {
	r.searchDocumentIndexed = false
	if r.db.Dialector.Name() != "mysql" {
		return nil
	}
	ctx, span := platform.Start(ctx, "catalog.repository.MigrateSearchDocument")
	defer span.End()
	type column struct {
		ColumnName             string
		DataType               string
		CharacterMaximumLength int64
		CollationName          string
		Extra                  string
		GenerationExpression   string
	}
	read := func() (map[string]column, error) {
		var columns []column
		if err := r.db.WithContext(ctx).Raw(
			"SELECT COLUMN_NAME AS column_name, DATA_TYPE AS data_type, CHARACTER_MAXIMUM_LENGTH AS character_maximum_length, COLLATION_NAME AS collation_name, EXTRA AS extra, GENERATION_EXPRESSION AS generation_expression FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
			"catalog_records",
		).Scan(
			&columns,
		).Error; err != nil {
			return nil, err
		}
		result := map[string]column{}
		for _, value := range columns {
			result[value.ColumnName] = value
		}
		return result, nil
	}
	columns, err := read()
	if err != nil {
		return fmt.Errorf("inspect catalog search document: %w", err)
	}
	definitions := searchDocumentColumns()
	add := []string{}
	for _, definition := range definitions {
		if _, exists := columns[definition.name]; !exists {
			add = append(
				add,
				"ADD COLUMN "+definition.name+" "+definition.definition+" GENERATED ALWAYS AS ("+definition.expression+
					") VIRTUAL",
			)
		}
	}
	if len(add) > 0 {
		if err = r.db.WithContext(ctx).Exec(
			"ALTER TABLE catalog_records " + strings.Join(add, ", ") + ", ALGORITHM=INPLACE, LOCK=NONE",
		).Error; err != nil {
			return fmt.Errorf("create catalog search document online: %w", err)
		}
		if columns, err = read(); err != nil {
			return fmt.Errorf("verify catalog search document: %w", err)
		}
	}
	for _, definition := range definitions {
		value, exists := columns[definition.name]
		if !exists || value.DataType != definition.kind || value.CharacterMaximumLength != definition.length ||
			!strings.Contains(value.Extra, "VIRTUAL GENERATED") ||
			normalizeSearchProjectionExpression(
				value.GenerationExpression,
			) != normalizeSearchProjectionExpression(
				definition.expression,
			) ||
			definition.kind == "varchar" && value.CollationName != "utf8mb4_bin" {
			return fmt.Errorf("catalog %s has an unexpected schema; refusing to trust or replace it", definition.name)
		}
	}
	type indexColumn struct {
		ColumnName string
		SubPart    *int
	}
	readIndex := func() ([]indexColumn, error) {
		var columns []indexColumn
		err := r.db.WithContext(ctx).
			Raw(
				"SELECT COLUMN_NAME AS column_name, SUB_PART AS sub_part FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ? ORDER BY SEQ_IN_INDEX",
				"catalog_records",
				searchDocumentIndex,
			).
			Scan(&columns).
			Error
		return columns, err
	}
	expected := []string{
		"search_public_v2",
		"search_long_v2",
		"search_category_v2",
		"search_brand_v2",
		"search_text_v2",
		"search_price_v2",
		"search_sales_v2",
		"created_at",
		"id",
	}
	indexes, err := readIndex()
	if err != nil {
		return fmt.Errorf("inspect catalog search document index: %w", err)
	}
	if len(indexes) == 0 {
		if err = r.db.WithContext(ctx).Exec(
			"ALTER TABLE catalog_records ADD INDEX " + searchDocumentIndex + " (" + strings.Join(
				expected,
				",",
			) + "), ALGORITHM=INPLACE, LOCK=NONE",
		).Error; err != nil {
			return fmt.Errorf("create catalog search document index online: %w", err)
		}
		if indexes, err = readIndex(); err != nil {
			return fmt.Errorf("verify catalog search document index: %w", err)
		}
	}
	if len(indexes) != len(expected) {
		return fmt.Errorf("catalog search document index has unexpected columns")
	}
	for i, field := range indexes {
		if field.ColumnName != expected[i] || field.SubPart != nil {
			return fmt.Errorf("catalog search document index has unexpected order or prefix")
		}
	}
	r.searchDocumentIndexed = true
	return nil
}

func normalizeSearchProjectionExpression(value string) string {
	value = strings.ReplaceAll(value, "\\'", "'")
	word := func(character byte) bool {
		return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '_' ||
			character == '$'
	}
	tokens := []string{}
	for index := 0; index < len(value); {
		current := value[index]
		if current == ' ' || current == '\n' || current == '\t' || current == '\r' {
			index++
			continue
		}
		last := index + 1
		if current == '\'' {
			for last < len(value) {
				if value[last] == '\'' {
					last++
					if last < len(value) && value[last] == '\'' {
						last++
						continue
					}
					break
				}
				last++
			}
		} else if current == '`' {
			for last < len(value) && value[last] != '`' {
				last++
			}
			if last < len(value) {
				last++
			}
		} else if word(current) {
			for last < len(value) && word(value[last]) {
				last++
			}
		}
		token := value[index:last]
		if current != '\'' {
			token = strings.ToLower(token)
		}
		if current == '`' {
			token = strings.Trim(token, "`")
		}
		if token != "_utf8mb4" && token != "_utf8mb3" {
			tokens = append(tokens, token)
		}
		index = last
	}
	// MySQL makes the connection charset explicit when it prints CAST AS CHAR.
	for index := 0; index+1 < len(tokens); index++ {
		if tokens[index] == "charset" && tokens[index+1] == "utf8mb4" {
			tokens = append(tokens[:index], tokens[index+2:]...)
			index--
		}
	}
	// Keep function parentheses and boolean grouping. Only discard redundant
	// expression/WHEN/CASE wrappers and atomic comparison wrappers added by MySQL.
	for {
		stack, first, last := []int{}, -1, -1
		for index, token := range tokens {
			if token == "(" {
				stack = append(stack, index)
			}
			if token != ")" || len(stack) == 0 {
				continue
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			inner := tokens[open+1 : index]
			if len(inner) == 0 {
				continue
			}
			whole := open == 0 && index == len(tokens)-1
			when := open > 0 && tokens[open-1] == "when" && index+1 < len(tokens) && tokens[index+1] == "then"
			atomicCase, nesting := inner[0] == "case" && inner[len(inner)-1] == "end", 0
			for position, part := range inner {
				if part == "case" {
					nesting++
				}
				if part == "end" {
					nesting--
					if nesting == 0 && position != len(inner)-1 {
						atomicCase = false
					}
				}
			}
			comparison, boolean, depth, cases := false, false, 0, 0
			for _, part := range inner {
				switch part {
				case "(":
					depth++
				case ")":
					depth--
				case "case":
					cases++
				case "end":
					cases--
				default:
					if depth == 0 && cases == 0 {
						comparison = comparison || part == "=" || part == ">" || part == "<" || part == "in"
						boolean = boolean || part == "and" || part == "or"
					}
				}
			}
			previous := ""
			if open > 0 {
				previous = tokens[open-1]
			}
			keyword := previous == "when" || previous == "then" || previous == "else" || previous == "and" ||
				previous == "or" ||
				previous == "not"
			function := previous != "" && !keyword && previous[0] >= 'a' && previous[0] <= 'z'
			next := ""
			if index+1 < len(tokens) {
				next = tokens[index+1]
			}
			booleanContext := (previous == "" || previous == "when" || previous == "and" || previous == "or" ||
				previous == "not" ||
				previous == "(") &&
				(next == "" || next == "then" || next == "and" || next == "or" || next == ")")
			if !function && (whole || when || atomicCase || comparison && !boolean && booleanContext) {
				first, last = open, index
				break
			}
		}
		if first < 0 {
			break
		}
		tokens = append(tokens[:last], tokens[last+1:]...)
		tokens = append(tokens[:first], tokens[first+1:]...)
	}
	return strings.Join(tokens, "\x00")
}
