package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/checkout/internal/domain"
	"gorm.io/gorm"
)

func (r *Repository) QueueGroupResolution(ctx context.Context, source platform.Event, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []domain.Saga
		if err := tx.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			key := sha256.Sum256([]byte(source.ID + ":" + source.Type + ":" + row.ID))
			if err := platform.Enqueue(ctx, tx, platform.Event{
				ID:          fmt.Sprintf("group-work-%x", key),
				Type:        constants.EventCheckoutGroupResolution,
				AggregateID: row.ID,
				UserID:      row.UserID,
				Data:        map[string]any{"formed": source.Type == constants.EventGroupFormed},
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
