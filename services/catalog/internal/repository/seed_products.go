package repository

import (
	"context"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InsertSeedProducts only initializes a missing product aggregate. Existing
// parents own their current presentation and variant set, including disabled
// variants; a restart must not add obsolete options back to an edited product.
// Bounded transactions avoid one commit per seeded SKU without holding the
// whole demonstration catalog in a single database transaction.
func (r *Repository) InsertSeedProducts(ctx context.Context, products [][]domain.Record) error {
	ctx, span := platform.Start(ctx, "catalog.repository.InsertSeedProducts")
	defer span.End()
	const batchSize = 100
	for first := 0; first < len(products); first += batchSize {
		last := min(first+batchSize, len(products))
		if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			ids := make([]string, 0, last-first)
			for _, rows := range products[first:last] {
				if len(rows) > 0 {
					ids = append(ids, rows[0].ID)
				}
			}
			if len(ids) == 0 {
				return nil
			}
			var existing []string
			if err := tx.Model(&domain.Record{}).Where("id IN ?", ids).Pluck("id", &existing).Error; err != nil {
				return err
			}
			known := make(map[string]bool, len(existing))
			for _, id := range existing {
				known[id] = true
			}
			children := []domain.Record{}
			for _, rows := range products[first:last] {
				if len(rows) == 0 || known[rows[0].ID] {
					continue
				}
				created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows[0])
				if created.Error != nil {
					return created.Error
				}
				// Another writer may have created this product since the read.
				if created.RowsAffected == 0 || len(rows) == 1 {
					continue
				}
				children = append(children, rows[1:]...)
			}
			if len(children) > 0 {
				return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(children, 100).Error
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
