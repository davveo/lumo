package repository

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RecommendationRecall struct {
	Categories []string
	Brands     []string
	Queries    []string
	Limit      int
}

// RecommendationCandidates reads only matching public products, including older
// ones outside the default catalog page. There is no COUNT, offset walk, or
// startup backfill. Sparse JSON filters can scan many rows; the derived deadline
// bounds this optional recall work so the recommender can fall back safely.
func (r *Repository) RecommendationCandidates(ctx context.Context, input RecommendationRecall) ([]domain.Record, bool, error) {
	rows := []domain.Record{}
	if len(input.Categories)+len(input.Brands)+len(input.Queries) == 0 {
		return rows, false, nil
	}
	if input.Limit < 1 || input.Limit > 200 {
		return nil, false, status.Error(codes.InvalidArgument, "invalid recommendation recall limit")
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	ctx, span := platform.Start(ctx, "catalog.repository.RecommendationCandidates")
	defer span.End()
	// Paths are constants, and interest values are always bound parameters. The
	// guarded document also keeps a malformed historical payload from failing recall.
	text := func(path string) string {
		document := "CASE WHEN JSON_VALID(payload) THEN payload ELSE '{}' END"
		if r.db.Dialector.Name() == "mysql" {
			return "JSON_UNQUOTE(JSON_EXTRACT(" + document + ", '" + path + "'))"
		}
		return "json_extract(" + document + ", '" + path + "')"
	}
	type recallLane struct {
		condition string
		args      []any
	}
	lanes := []recallLane{}
	if len(input.Categories) > 0 {
		lanes = append(lanes, recallLane{text("$.category") + " IN ?", []any{input.Categories}})
	}
	if len(input.Brands) > 0 {
		lanes = append(lanes, recallLane{text("$.brand") + " IN ?", []any{input.Brands}})
	}
	conditions, args := []string{}, []any{}
	for _, value := range input.Queries {
		// Escape wildcards as literals rather than accepting an unbounded '%' recall.
		value = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(value))
		columns := []string{}
		for _, path := range []string{"$.name", "$.brand", "$.category"} {
			columns = append(columns, "LOWER(COALESCE("+text(path)+",'')) LIKE ? ESCAPE '!'")
			args = append(args, "%"+value+"%")
		}
		conditions = append(conditions, "("+strings.Join(columns, " OR ")+")")
	}
	if len(conditions) > 0 {
		lanes = append(lanes, recallLane{"(" + strings.Join(conditions, " OR ") + ")", args})
	}
	// Each signal channel receives a fixed share. A popular category must not
	// consume the entire result limit and hide an older, rare preferred brand.
	type laneResult struct {
		rows   []domain.Record
		err    error
		active bool
	}
	results := make([]laneResult, len(lanes))
	var workers sync.WaitGroup
	for i, lane := range lanes {
		quota := input.Limit / len(lanes)
		if i < input.Limit%len(lanes) {
			quota++
		}
		if quota == 0 {
			continue
		}
		results[i].active = true
		workers.Add(1)
		go func(i, quota int, lane recallLane) {
			defer workers.Done()
			// Each channel has an independent deadline. A sparse brand or query
			// must not discard successful category candidates or add serial waits.
			laneCtx, done := context.WithTimeout(ctx, 400*time.Millisecond)
			defer done()
			query := r.db.WithContext(laneCtx).Model(&domain.Record{}).
				Where("kind = ? AND parent_id = ? AND status = ?", constants.CatalogKindSku, "", constants.CatalogStatusActive).
				Where("JSON_VALID(payload)").
				Where("COALESCE(CAST("+text("$.catalog_visible")+" AS CHAR),'true') NOT IN ('false','0')").
				Where(lane.condition, lane.args...).Order("created_at DESC,id DESC").Limit(quota)
			results[i].err = query.Find(&results[i].rows).Error
			if laneCtx.Err() != nil {
				results[i].rows = nil
				results[i].err = status.FromContextError(laneCtx.Err()).Err()
			}
		}(i, quota, lane)
	}
	workers.Wait()
	if ctx.Err() != nil {
		return nil, true, status.FromContextError(ctx.Err()).Err()
	}
	degraded, successful, seen := false, 0, map[string]bool{}
	var firstError error
	for _, result := range results {
		if !result.active {
			continue
		}
		if result.err != nil {
			degraded = true
			if firstError == nil {
				firstError = result.err
			}
			continue
		}
		successful++
		for _, row := range result.rows {
			if !seen[row.ID] {
				seen[row.ID] = true
				rows = append(rows, row)
			}
		}
	}
	if successful == 0 && firstError != nil {
		return nil, true, firstError
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].ID > rows[j].ID
	})
	if len(rows) > input.Limit {
		rows = rows[:input.Limit]
	}
	return rows, degraded, nil
}
