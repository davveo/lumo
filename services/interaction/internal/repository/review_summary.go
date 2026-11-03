package repository

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/interaction/internal/domain"
)

func (r *Repository) ReviewSummary(ctx context.Context, skus []string) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "interaction.repository.ReviewSummary")
	defer sp.End()
	rating := "CAST(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.rating')) AS SIGNED)"
	if r.db.Dialector.Name() == "sqlite" {
		rating = "CAST(JSON_EXTRACT(payload,'$.rating') AS INTEGER)"
	}
	rating = "CASE WHEN JSON_VALID(payload) THEN " + rating + " ELSE 0 END"
	q := r.db.WithContext(ctx).
		Model(&domain.Record{}).
		Where("kind = ? AND status = ?", constants.InteractionKindReview, constants.InteractionStatusPublished)
	if len(skus) > 0 {
		q = q.Where("parent_id IN ?", skus)
	}
	rows := []struct {
		Rating int64
		Total  int64
	}{}
	if e := q.Where("(" + rating + ") BETWEEN 1 AND 5").Select(rating + " AS rating, COUNT(*) AS total").Group(rating).Scan(&rows).Error; e != nil {
		return nil, e
	}
	var total, sum, good int64
	counts := map[int64]int64{}
	for _, v := range rows {
		counts[v.Rating] = v.Total
		total += v.Total
		sum += v.Rating * v.Total
		if v.Rating >= 4 {
			good += v.Total
		}
	}
	var average float64
	if total > 0 {
		average = float64(sum) / float64(total)
	}
	return platform.Result{"total": total, "average": average, "favorable": good, "rating_counts": counts}, nil
}
