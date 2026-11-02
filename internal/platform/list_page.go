package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type listPosition struct {
	Scope   string    `json:"s"`
	ID      string    `json:"id"`
	Created time.Time `json:"t"`
}

// ReadListPage keeps ownership predicates in the owning repository. The cursor
// is scoped to that collection and principal, with a deterministic tie-breaker.
func ReadListPage[T any](
	ctx context.Context,
	q *gorm.DB,
	request Request,
	collection string,
	position func(T) (string, time.Time),
) ([]T, Result, error) {
	limit, offset := Int(request.Data, "limit"), Int(request.Data, "offset")
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 10000 {
		return nil, nil, status.Error(
			codes.InvalidArgument,
			"limit must be 1..100 and offset 0..10000; use cursor for later pages",
		)
	}
	hash := sha256.Sum256([]byte(collection + "\x00" + request.UserID + "\x00" + request.Role))
	scope := hex.EncodeToString(hash[:])
	var after *listPosition
	if cursor := Text(request.Data, "cursor"); cursor != "" {
		var p listPosition
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || len(b) > 2048 || json.Unmarshal(b, &p) != nil || p.ID == "" || len(p.ID) > 160 ||
			p.Scope != scope ||
			offset != 0 {
			return nil, nil, status.Error(codes.InvalidArgument, "invalid list cursor")
		}
		after = &p
	}
	var total int64
	if err := q.WithContext(ctx).Count(&total).Error; err != nil {
		return nil, nil, err
	}
	query := q.WithContext(ctx)
	if after != nil {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", after.Created, after.Created, after.ID)
	}
	rows := []T{}
	if err := query.Order("created_at DESC, id DESC").Limit(int(limit) + 1).Offset(int(offset)).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	next := ""
	if hasMore {
		id, created := position(rows[len(rows)-1])
		b, _ := json.Marshal(listPosition{Scope: scope, ID: id, Created: created})
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	return rows, Result{"total": total, "limit": limit, "offset": offset, "has_more": hasMore, "next_cursor": next}, nil
}
