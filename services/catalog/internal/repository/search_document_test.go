package repository

import "testing"

func TestSearchProjectionNormalizationPreservesBooleanPrecedence(t *testing.T) {
	first := "CASE WHEN (a = 'a' OR b = 'b') AND c = 'c' THEN 1 ELSE 0 END"
	second := "CASE WHEN a = 'a' OR (b = 'b' AND c = 'c') THEN 1 ELSE 0 END"
	if normalizeSearchProjectionExpression(first) == normalizeSearchProjectionExpression(second) {
		t.Fatal("different boolean grouping was treated as the same expression")
	}
	original := "CASE WHEN kind = 'sku' AND parent_id = '' THEN CASE WHEN COALESCE(CAST(payload AS CHAR), 'true') NOT IN ('false', '0') THEN 1 ELSE 0 END ELSE 0 END"
	canonical := "(case when ((`kind` = _utf8mb4\\'sku\\') and (`parent_id` = _utf8mb4\\'\\')) then (case when (coalesce(cast(`payload` as char charset utf8mb4),_utf8mb4\\'true\\') not in (_utf8mb4\\'false\\',_utf8mb4\\'0\\')) then 1 else 0 end) else 0 end)"
	if normalizeSearchProjectionExpression(original) != normalizeSearchProjectionExpression(canonical) {
		t.Fatal("MySQL comparison/CASE wrappers were not normalized")
	}
}
