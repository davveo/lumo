package repository

import (
	"context"
	"github.com/davveo/lumo/services/analytics/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

func (r *Repository) TrackJourney(ctx context.Context, row *domain.JourneyEvent) (bool, error) {
	inserted := false
	now := time.Now().UTC()
	row.OccurredAt = now
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing domain.JourneyEvent
		if e := tx.Where("id = ?", row.ID).Limit(1).Find(&existing).Error; e != nil {
			return e
		}
		if existing.ID != "" {
			return nil
		}
		limit := domain.JourneyLimit{ID: row.SessionKey + ":" + strconv.FormatInt(now.Unix()/60, 10)}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&limit).Error; e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&limit, "id = ?", limit.ID).Error; e != nil {
			return e
		}
		if limit.Count >= 60 {
			return status.Error(codes.ResourceExhausted, "tracking rate exceeded")
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if result.Error != nil {
			return result.Error
		}
		inserted = result.RowsAffected == 1
		if inserted {
			return tx.Model(&limit).Update("count", gorm.Expr("count + 1")).Error
		}
		return nil
	})
	return inserted, e
}
func (r *Repository) SetOrderAttribution(ctx context.Context, row domain.TransactionFact) error {
	return r.db.WithContext(ctx).
		Model(&domain.TransactionFact{}).
		Where("order_id = ? AND kind = ?", row.OrderID, "paid").
		Updates(map[string]any{
			"session_key":   row.SessionKey,
			"source":        row.Source,
			"search_query":  row.SearchQuery,
			"campaign_cost": row.CampaignCost,
			"coupon_cost":   row.CouponCost,
			"points_cost":   row.PointsCost,
			"cost_known":    true,
		}).
		Error
}

func (r *Repository) PruneJourneyLimits(ctx context.Context) error {
	return r.db.WithContext(ctx).
		Where("updated_at < ?", time.Now().UTC().Add(-48*time.Hour)).
		Delete(&domain.JourneyLimit{}).
		Error
}
