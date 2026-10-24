package repository

import "testing"

func TestSearchExpressionNormalizationKeepsQuotedValues(t *testing.T) {
	for _, test := range []struct{ name, first, second string }{
		{
			"case-sensitive JSON path",
			"JSON_UNQUOTE(JSON_EXTRACT(payload, '$.name'))",
			"JSON_UNQUOTE(JSON_EXTRACT(payload, '$.Name'))",
		},
		{
			"charset-looking JSON path",
			"JSON_UNQUOTE(JSON_EXTRACT(payload, '$.name'))",
			"JSON_UNQUOTE(JSON_EXTRACT(payload, '$.na_utf8mb4me'))",
		},
		{
			"backtick inside JSON path",
			"JSON_UNQUOTE(JSON_EXTRACT(payload, '$.name'))",
			"JSON_UNQUOTE(JSON_EXTRACT(payload, '$.na`me'))",
		},
		{
			"literal case",
			"CASE WHEN status = 'ACTIVE' THEN 1 ELSE 0 END",
			"CASE WHEN status = 'active' THEN 1 ELSE 0 END",
		},
		{"literal whitespace", "CONCAT('a b', payload)", "CONCAT('ab', payload)"},
		{"literal quote", "CONCAT('O''Reilly', payload)", "CONCAT('OReilly', payload)"},
		{"literal charset text", "CONCAT('_utf8mb4', payload)", "CONCAT('', payload)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if normalizeSearchProjectionExpression(test.first) == normalizeSearchProjectionExpression(test.second) {
				t.Fatalf("different quoted values were accepted as equivalent: %s / %s", test.first, test.second)
			}
		})
	}
}

func TestSearchExpressionNormalizationKeepsFunctionAndOperatorBoundaries(t *testing.T) {
	for _, test := range []struct{ name, first, second string }{
		{
			"CASE sum before multiplication",
			"(CASE WHEN a = 1 THEN 1 ELSE 0 END + CASE WHEN b = 1 THEN 1 ELSE 0 END) * 2",
			"CASE WHEN a = 1 THEN 1 ELSE 0 END + CASE WHEN b = 1 THEN 1 ELSE 0 END * 2",
		},
		{
			"comparison before arithmetic",
			"CASE WHEN (a > 0) * 2 = 2 THEN 1 ELSE 0 END",
			"CASE WHEN a > 0 * 2 = 2 THEN 1 ELSE 0 END",
		},
		{
			"comparison as arithmetic operand",
			"CASE WHEN (a = 1) + 2 > 2 THEN 1 ELSE 0 END",
			"CASE WHEN a = 1 + 2 > 2 THEN 1 ELSE 0 END",
		},
		{"function scope", "LOWER(CONCAT(name, brand))", "CONCAT(LOWER(name), brand)"},
		{
			"COALESCE argument scope",
			"COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.name')), '')",
			"JSON_UNQUOTE(COALESCE(JSON_EXTRACT(payload, '$.name'), ''))",
		},
		{
			"boolean CASE operand",
			"CASE WHEN (a = 1 OR b = 1) AND c = 1 THEN 1 ELSE 0 END",
			"CASE WHEN a = 1 OR (b = 1 AND c = 1) THEN 1 ELSE 0 END",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if normalizeSearchProjectionExpression(test.first) == normalizeSearchProjectionExpression(test.second) {
				t.Fatalf(
					"different operator/function scopes were accepted as equivalent: %s / %s",
					test.first,
					test.second,
				)
			}
		})
	}
}

func TestSearchExpressionNormalizationAcceptsOnlyPresentationWrappers(t *testing.T) {
	for _, test := range []struct{ name, first, second string }{
		{"whole expression", "CASE WHEN a = 1 THEN 1 ELSE 0 END", "((case when (`a` = 1) then 1 else 0 end))"},
		{
			"nested CASE atom",
			"CASE WHEN a = 1 THEN CASE WHEN b = 2 THEN 1 ELSE 0 END ELSE 0 END",
			"(case when (`a` = 1) then (case when (`b` = 2) then 1 else 0 end) else 0 end)",
		},
		{
			"IN list retains argument boundary",
			"CASE WHEN status IN ('ACTIVE', 'READY') THEN 1 ELSE 0 END",
			"case when (`status` in (_utf8mb4'ACTIVE', _utf8mb4'READY')) then 1 else 0 end",
		},
		{"charset outside literal", "CAST(payload AS CHAR)", "cast(`payload` as char charset utf8mb4)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if normalizeSearchProjectionExpression(test.first) != normalizeSearchProjectionExpression(test.second) {
				t.Fatalf("safe MySQL presentation wrappers were rejected: %s / %s", test.first, test.second)
			}
		})
	}
}
