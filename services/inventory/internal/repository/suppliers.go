package repository

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) SupplierList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "inventory.repository.SupplierList")
	defer sp.End()
	rows, total, f, e := adminquery.Page[domain.Supplier](
		ctx,
		r.DB,
		c.Data,
		adminquery.Spec{Search: []string{"id", "name", "contact"}},
	)
	return adminquery.Result(rows, total, f), e
}
func (r *Repository) Supplier(ctx context.Context, id string) (domain.Supplier, error) {
	ctx, sp := platform.Start(ctx, "inventory.repository.Supplier")
	defer sp.End()
	var row domain.Supplier
	e := r.DB.WithContext(ctx).First(&row, "id = ?", id).Error
	return row, e
}
func (r *Repository) SupplierUpsert(ctx context.Context, row domain.Supplier, version int64) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "inventory.repository.SupplierUpsert")
	defer sp.End()
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := domain.Supplier{ID: row.ID}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; e != nil {
			return e
		}
		var old domain.Supplier
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", row.ID).Error; e != nil {
			return e
		}
		if old.Version != version {
			return status.Error(codes.Aborted, "供应商档案已更新，请刷新")
		}
		row.Version = version + 1
		row.CreatedAt = old.CreatedAt
		return tx.Save(&row).Error
	})
	return platform.Map(row), e
}

func (r *Repository) ReplenishmentInputs(
	ctx context.Context,
	ids []string,
) (map[string]domain.Stock, map[string]int64, error) {
	ctx, sp := platform.Start(ctx, "inventory.repository.ReplenishmentInputs")
	defer sp.End()
	stocks := []domain.Stock{}
	if e := r.DB.WithContext(ctx).Where("sku_id IN ?", ids).Find(&stocks).Error; e != nil {
		return nil, nil, e
	}
	bySKU := map[string]domain.Stock{}
	inbound := map[string]int64{}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
		bySKU[id] = domain.Stock{SKUID: id, Threshold: 10}
	}
	for _, row := range stocks {
		bySKU[row.SKUID] = row
	}
	var orders []domain.PurchaseOrder
	e := r.DB.WithContext(ctx).
		Where("status IN ?", []string{constants.PurchaseStatusApproved, constants.PurchaseStatusPartiallyReceived}).
		FindInBatches(&orders, 100, func(tx *gorm.DB, batch int) error {
			for _, order := range orders {
				var lines []domain.PurchaseLine
				if e := json.Unmarshal([]byte(order.Lines), &lines); e != nil {
					return e
				}
				for _, line := range lines {
					if wanted[line.SKUID] {
						inbound[line.SKUID] += max(int64(0), line.Quantity-line.Received)
					}
				}
			}
			return nil
		}).
		Error
	return bySKU, inbound, e
}
