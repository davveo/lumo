// Package adminquery provides bounded, read-only query mechanics. Each owning
// repository supplies its own model and trusted searchable columns.
package adminquery

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type Spec struct {
	State, Amount string
	Search        []string
}
type Filters struct {
	AsOf     string `json:"as_of"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Keyword  string `json:"keyword"`
	Status   string `json:"status"`
	From     string `json:"from"`
	To       string `json:"to"`
}

func filtered(ctx context.Context, db *gorm.DB, model any, data map[string]any, spec Spec) (*gorm.DB, Filters, error) {
	var f Filters
	if err := platform.Decode(data, &f); err != nil {
		return nil, f, err
	}
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = 20
	}
	if f.Page < 1 || f.Page > 1000000 || f.PageSize < 1 || f.PageSize > 100 ||
		utf8.RuneCountInString(f.Keyword) > 120 ||
		len(f.Status) > 40 {
		return nil, f, status.Error(codes.InvalidArgument, "invalid management page or filter")
	}
	q := db.WithContext(ctx).Model(model)
	if f.AsOf != "" {
		cutoff, e := time.Parse(time.RFC3339Nano, f.AsOf)
		if e != nil {
			return nil, f, status.Error(codes.InvalidArgument, "invalid snapshot time")
		}
		q = q.Where("created_at <= ?", cutoff.UTC())
	}
	if f.Status != "" {
		q = q.Where(spec.State+" = ?", f.Status)
	}
	if value := strings.TrimSpace(f.Keyword); value != "" {
		like := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value) + "%"
		parts := []string{}
		args := []any{}
		for _, column := range spec.Search {
			parts = append(parts, column+" LIKE ? ESCAPE '!'")
			args = append(args, like)
		}
		if len(parts) > 0 {
			q = q.Where("("+strings.Join(parts, " OR ")+")", args...)
		}
	}
	var start, end time.Time
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	for _, input := range []struct {
		value string
		end   bool
	}{{f.From, false}, {f.To, true}} {
		if input.value == "" {
			continue
		}
		parsed, err := time.ParseInLocation("2006-01-02", input.value, zone)
		if err != nil {
			return nil, f, status.Error(codes.InvalidArgument, "management dates must be YYYY-MM-DD")
		}
		if input.end {
			end = parsed.AddDate(0, 0, 1)
			q = q.Where("created_at < ?", end.UTC())
		} else {
			start = parsed
			q = q.Where("created_at >= ?", parsed.UTC())
		}
	}
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return nil, f, status.Error(codes.InvalidArgument, "invalid date range")
	}
	return q, f, nil
}
func Page[T any](ctx context.Context, db *gorm.DB, data map[string]any, spec Spec) ([]T, int64, Filters, error) {
	var model T
	q, f, err := filtered(ctx, db, &model, data, spec)
	if err != nil {
		return nil, 0, f, err
	}
	var total int64
	if err = q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, f, err
	}
	rows := []T{}
	if int64((f.Page-1)*f.PageSize) < total {
		err = q.Order("created_at DESC,id DESC").Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error
	}
	return rows, total, f, err
}
func Result(items any, total int64, f Filters) platform.Result {
	return platform.Result{
		"items":     items,
		"total":     total,
		"page":      f.Page,
		"page_size": f.PageSize,
		"has_more":  int64(f.Page*f.PageSize) < total,
	}
}
func Summary[T any](ctx context.Context, db *gorm.DB, data map[string]any, spec Spec) (platform.Result, error) {
	days := platform.Int(data, "days")
	if days == 0 {
		days = 7
	}
	if days != 7 && days != 30 {
		return nil, status.Error(codes.InvalidArgument, "dashboard days must be 7 or 30")
	}
	var model T
	amount := "0"
	if spec.Amount != "" {
		amount = "COALESCE(SUM(" + spec.Amount + "),0)"
	}
	type bucket struct {
		Status string `json:"status"`
		Count  int64  `json:"count"`
		Amount int64  `json:"amount_cents"`
		Day    string `json:"day,omitempty"`
	}
	states := []bucket{}
	if err := db.WithContext(ctx).Model(&model).Select(spec.State + " AS status,COUNT(*) AS count," + amount + " AS amount").Group(spec.State).Scan(&states).Error; err != nil {
		return nil, err
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone).AddDate(0, 0, -int(days)+1)
	date := "DATE(datetime(created_at,'+8 hours'))"
	if db.Dialector.Name() == "mysql" {
		date = "DATE_FORMAT(CONVERT_TZ(created_at,'+00:00','+08:00'),'%Y-%m-%d')"
	}
	daily := []bucket{}
	if err := db.WithContext(ctx).Model(&model).Select(date+" AS day,"+spec.State+" AS status,COUNT(*) AS count,"+amount+" AS amount").Where("created_at >= ? AND created_at < ?", start.UTC(), now.UTC()).Group(date + "," + spec.State).Order("day").Scan(&daily).Error; err != nil {
		return nil, err
	}
	var total int64
	for _, row := range states {
		total += row.Count
	}
	return platform.Result{
		"total":        total,
		"states":       states,
		"daily":        daily,
		"days":         days,
		"timezone":     "Asia/Shanghai",
		"generated_at": now,
	}, nil
}
