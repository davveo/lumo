package repository

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/davveo/lumo/services/catalog/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type searchPosition struct {
	Query   string    `json:"q"`
	ID      string    `json:"id"`
	Created time.Time `json:"t"`
	Value   int64     `json:"n"`
}

func searchIdentity(input ProductSearch) string {
	b, _ := json.Marshal([]string{input.Query, input.Category, input.Brand, input.Sort})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func searchCursor(input ProductSearch) (*searchPosition, error) {
	if input.Cursor == "" {
		return nil, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(input.Cursor)
	var p searchPosition
	if e != nil || len(b) > 2048 || json.Unmarshal(b, &p) != nil || p.Query != searchIdentity(input) || p.ID == "" ||
		len(p.ID) > 128 {
		return nil, status.Error(codes.InvalidArgument, "invalid product search cursor")
	}
	return &p, nil
}

func ProductSearchCursor(input ProductSearch, row domain.Record) string {
	b, _ := json.Marshal(
		searchPosition{Query: searchIdentity(input), ID: row.ID, Created: row.CreatedAt, Value: row.SearchSortValue},
	)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Every ordering includes a unique ID tie-breaker. The rank is read from SQL,
// preserving database conversion semantics for historical JSON documents.
func searchAfter(rank string, ascending bool, p *searchPosition) (string, []any) {
	if p == nil {
		return "", nil
	}
	tie := "(created_at < ? OR (created_at = ? AND id < ?))"
	args := []any{p.Created, p.Created, p.ID}
	if rank == "0" {
		return tie, args
	}
	operator := "<"
	if ascending {
		operator = ">"
	}
	return "(" + rank + " " + operator + " ? OR (" + rank + " = ? AND " + tie + "))", append(
		[]any{p.Value, p.Value},
		args...)
}
