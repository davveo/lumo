package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/couponrules"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TemplateWrite is separate from the persisted model: claimed coupons remain
// immutable snapshots, and edits apply a delta to live stock instead of replacing
// a potentially stale remaining count.
type TemplateWrite struct {
	couponrules.Rule
	Name       string     `json:"name"`
	Amount     int64      `json:"amount_cents"`
	Minimum    int64      `json:"min_cents"`
	ValidDays  int64      `json:"valid_days"`
	Remaining  *int64     `json:"remaining"`
	StockDelta int64      `json:"stock_delta"`
	Active     *bool      `json:"active"`
	StartsAt   *time.Time `json:"starts_at"`
	MinPoints  int64      `json:"min_points"`
	MinGrowth  int64      `json:"min_growth"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Version    *int64     `json:"version"`
}

func (r *Repository) TemplatesAdmin(ctx context.Context, request platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "repository.coupon.TemplatesAdmin")
	defer span.End()
	var query struct {
		Page     int64  `json:"page"`
		PageSize int64  `json:"page_size"`
		Keyword  string `json:"keyword"`
		Active   string `json:"active"`
	}
	if err := platform.Decode(request.Data, &query); err != nil {
		return nil, err
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 10
	}
	if query.Page < 1 || query.Page > 1000000 || query.PageSize < 1 || query.PageSize > 100 ||
		utf8.RuneCountInString(query.Keyword) > 120 {
		return nil, status.Error(codes.InvalidArgument, "invalid coupon template page or keyword")
	}
	if query.Active != "" && query.Active != "true" && query.Active != "false" {
		return nil, status.Error(codes.InvalidArgument, "active filter must be true or false")
	}
	q := r.DB.WithContext(ctx).Model(&domain.Template{})
	if query.Active != "" {
		q = q.Where("active = ?", query.Active == "true")
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		// LIKE uses bound parameters; wildcards intentionally permit an operator
		// to search either a template's identifier or human-readable name.
		q = q.Where("(name LIKE ? OR id LIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []domain.Template
	if err := q.Order("created_at DESC, id").Offset(int((query.Page - 1) * query.PageSize)).Limit(int(query.PageSize)).Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var claims []struct {
		TemplateID   string
		ClaimedCount int64
	}
	if len(ids) > 0 {
		if err := r.DB.WithContext(ctx).Model(&domain.Claim{}).Select("template_id, COUNT(*) AS claimed_count").Where("template_id IN ?", ids).Group("template_id").Scan(&claims).Error; err != nil {
			return nil, err
		}
	}
	counts := make(map[string]int64, len(claims))
	for _, row := range claims {
		counts[row.TemplateID] = row.ClaimedCount
	}
	items := make([]platform.Result, 0, len(rows))
	for _, row := range rows {
		item := platform.Map(row)
		item["claimed_count"] = counts[row.ID]
		items = append(items, item)
	}
	return platform.Result{
		"items":       items,
		"total":       total,
		"page":        query.Page,
		"page_size":   query.PageSize,
		"has_more":    query.Page*query.PageSize < total,
		"server_time": time.Now(),
	}, nil
}

func validTemplateID(id string) bool {
	if id == "" || len(id) > 120 || id != strings.TrimSpace(id) {
		return false
	}
	for _, char := range id {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func (r *Repository) TemplateUpsert(ctx context.Context, request platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "repository.coupon.TemplateUpsert")
	defer span.End()
	var input TemplateWrite
	if err := platform.Decode(request.Data, &input); err != nil {
		return nil, err
	}
	id := request.ID
	if id == "" {
		var err error
		id, err = platform.NextBusinessID(ctx, r.DB, "coupon-template")
		if err != nil {
			return nil, err
		}
	}
	if !validTemplateID(id) {
		return nil, status.Error(codes.InvalidArgument, "coupon template ID must be 1-120 non-space bytes")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Active == nil || input.Name == "" || utf8.RuneCountInString(input.Name) > 120 || input.Amount < 0 ||
		input.Minimum < 0 ||
		input.ValidDays < 1 ||
		input.ValidDays > 3660 ||
		input.MinPoints < 0 ||
		input.MinGrowth < 0 ||
		input.ExpiresAt.IsZero() ||
		(input.StartsAt != nil && !input.StartsAt.Before(input.ExpiresAt)) ||
		(input.Version != nil && *input.Version < 0) {
		return nil, status.Error(
			codes.InvalidArgument,
			"invalid coupon template name, amount, threshold, duration or claim window",
		)
	}
	if err := input.Rule.Normalize(input.Amount, input.Minimum); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// Cross-language clients can represent these configuration quantities exactly.
	const maxExactInteger = int64(9007199254740991)
	for _, value := range []int64{input.Amount, input.Minimum, input.MinPoints, input.MinGrowth, input.StockDelta} {
		if value > maxExactInteger || value < -maxExactInteger {
			return nil, status.Error(codes.InvalidArgument, "coupon template integer exceeds supported range")
		}
	}
	var out domain.Template
	var claimed int64
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current domain.Template
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if input.Version != nil && *input.Version != 0 {
				return status.Error(codes.NotFound, "coupon template no longer exists")
			}
			if input.Remaining == nil || *input.Remaining < 0 || *input.Remaining > maxExactInteger ||
				input.StockDelta != 0 {
				return status.Error(
					codes.InvalidArgument,
					"new coupon template requires initial remaining stock and no stock_delta",
				)
			}
			if !input.ExpiresAt.After(time.Now()) {
				return status.Error(codes.InvalidArgument, "new coupon claim deadline must be in the future")
			}
			out = domain.Template{
				ID:        id,
				Rule:      input.Rule,
				Name:      input.Name,
				Amount:    input.Amount,
				Minimum:   input.Minimum,
				ValidDays: input.ValidDays,
				Remaining: *input.Remaining,
				Active:    *input.Active,
				StartsAt:  input.StartsAt,
				MinPoints: input.MinPoints,
				MinGrowth: input.MinGrowth,
				ExpiresAt: input.ExpiresAt,
				Version:   1,
				CreatedAt: time.Now(),
			}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&out)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected != 1 {
				return status.Error(codes.AlreadyExists, "coupon template ID already exists; reload before editing")
			}
			return tx.First(&out, "id = ?", id).Error
		}
		if err != nil {
			return err
		}
		if input.Version == nil || *input.Version < 1 || *input.Version != current.Version {
			return status.Error(codes.Aborted, "coupon template changed; reload before editing")
		}
		if input.Remaining != nil {
			return status.Error(
				codes.InvalidArgument,
				"use stock_delta to adjust an existing template's remaining stock",
			)
		}
		if err := tx.Model(&domain.Claim{}).Where("template_id = ?", id).Count(&claimed).Error; err != nil {
			return err
		}
		if claimed > 0 &&
			(input.Amount != current.Amount || input.Minimum != current.Minimum || input.ValidDays != current.ValidDays || !couponrules.Same(input.Rule, current.Rule, input.Amount, input.Minimum, current.Amount, current.Minimum)) {
			return status.Error(
				codes.FailedPrecondition,
				"issued coupon amount, usage threshold and valid days are locked; copy into a new template",
			)
		}
		if input.StockDelta > 0 && current.Remaining > maxExactInteger-input.StockDelta ||
			input.StockDelta < 0 && current.Remaining < -input.StockDelta {
			return status.Error(codes.InvalidArgument, "coupon remaining stock adjustment is out of range")
		}
		if current.Version == math.MaxInt64 {
			return status.Error(codes.FailedPrecondition, "coupon template version exhausted")
		}
		out = current
		out.Rule = input.Rule
		out.Name, out.Amount, out.Minimum, out.ValidDays = input.Name, input.Amount, input.Minimum, input.ValidDays
		out.Remaining += input.StockDelta
		out.Active, out.StartsAt, out.MinPoints, out.MinGrowth, out.ExpiresAt = *input.Active, input.StartsAt, input.MinPoints, input.MinGrowth, input.ExpiresAt
		out.Version++
		result := tx.Model(&domain.Template{}).
			Where("id = ? AND version = ?", id, current.Version).
			Select("coupon_type", "discount_bps", "max_discount", "scope_type", "scope_values", "name", "amount", "minimum", "valid_days", "remaining", "active", "starts_at", "min_points", "min_growth", "expires_at", "version").
			Updates(&out)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return status.Error(codes.Aborted, "coupon template changed; reload before editing")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	item := platform.Map(out)
	item["claimed_count"] = claimed
	return item, nil
}
