package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func purchaseView(row domain.PurchaseOrder) platform.Result {
	out := platform.Map(row)
	var lines []domain.PurchaseLine
	_ = json.Unmarshal([]byte(row.Lines), &lines)
	out["lines"] = lines
	var amount int64
	for _, line := range lines {
		amount += line.Quantity * line.UnitCostCents
	}
	out["total_cost_cents"] = amount
	return out
}
func (r *Repository) CreatePurchase(ctx context.Context, row *domain.PurchaseOrder) (platform.Result, error) {
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var old domain.PurchaseOrder
			if e := tx.First(&old, "id = ?", row.ID).Error; e != nil {
				return e
			}
			if old.Fingerprint != row.Fingerprint {
				return status.Error(codes.AlreadyExists, "相同请求编号的采购内容不同")
			}
			*row = old
		}
		return nil
	})
	return purchaseView(*row), err
}
func (r *Repository) PurchaseList(ctx context.Context, c platform.Request) (platform.Result, error) {
	rows, total, f, e := adminquery.Page[domain.PurchaseOrder](
		ctx,
		r.DB,
		c.Data,
		adminquery.Spec{State: "status", Search: []string{"id", "supplier", "note"}},
	)
	items := []platform.Result{}
	for _, row := range rows {
		items = append(items, purchaseView(row))
	}
	return adminquery.Result(items, total, f), e
}
func (r *Repository) PurchaseGet(ctx context.Context, id string) (platform.Result, error) {
	var row domain.PurchaseOrder
	e := r.DB.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "采购单不存在")
	}
	if e != nil {
		return nil, e
	}
	out := purchaseView(row)
	var receipts []domain.PurchaseReceipt
	query := r.DB.WithContext(ctx).Where("purchase_id = ?", id)
	var count int64
	if e = query.Model(&domain.PurchaseReceipt{}).Count(&count).Error; e != nil {
		return nil, e
	}
	if e = query.Order("created_at DESC,id DESC").Limit(100).Find(&receipts).Error; e != nil {
		return nil, e
	}
	items := []platform.Result{}
	for _, receipt := range receipts {
		v := platform.Map(receipt)
		var ls []domain.PurchaseReceiveLine
		_ = json.Unmarshal([]byte(receipt.Lines), &ls)
		v["lines"] = ls
		items = append(items, v)
	}
	out["receipts"] = items
	out["receipts_total"] = count
	return out, nil
}

func (r *Repository) PurchaseTransition(
	ctx context.Context,
	c platform.Request,
	target string,
) (platform.Result, error) {
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row domain.PurchaseOrder
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", c.ID).Error; e != nil {
			return e
		}
		if row.Status == target {
			return nil
		}
		if row.Version != platform.Int(c.Data, "expected_version") {
			return status.Error(codes.Aborted, "采购单已变化，请刷新")
		}
		if target == constants.PurchaseStatusApproved && row.Status != constants.PurchaseStatusDraft ||
			target == constants.PurchaseStatusCancelled &&
				(row.Status == constants.PurchaseStatusReceived || row.Status == constants.PurchaseStatusCancelled) {
			return status.Error(codes.FailedPrecondition, "当前状态不允许此操作")
		}
		fields := map[string]any{"status": target, "version": row.Version + 1}
		if target == constants.PurchaseStatusApproved {
			fields["approved_by"] = c.UserID
			fields["approved_at"] = time.Now().UTC()
		}
		if target == constants.PurchaseStatusCancelled {
			fields["cancelled_by"] = c.UserID
			fields["cancel_reason"] = platform.Text(c.Data, "reason")
		}
		return tx.Model(&row).Updates(fields).Error
	})
	if err != nil {
		return nil, err
	}
	return r.PurchaseGet(ctx, c.ID)
}

func (r *Repository) ReceivePurchase(
	ctx context.Context,
	c platform.Request,
	lines []domain.PurchaseReceiveLine,
	receiptID, fingerprint, note string,
) (platform.Result, error) {
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order domain.PurchaseOrder
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", c.ID).Error; e != nil {
			return e
		}
		var prior domain.PurchaseReceipt
		e := tx.First(&prior, "id = ?", receiptID).Error
		if e == nil {
			if prior.PurchaseID != c.ID || prior.Fingerprint != fingerprint {
				return status.Error(codes.AlreadyExists, "收货请求编号已被其他内容使用")
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if order.Version != platform.Int(c.Data, "expected_version") {
			return status.Error(codes.Aborted, "采购单已变化，请刷新")
		}
		if order.Status != constants.PurchaseStatusApproved &&
			order.Status != constants.PurchaseStatusPartiallyReceived {
			return status.Error(codes.FailedPrecondition, "采购单未审批或已经结束")
		}
		var purchased []domain.PurchaseLine
		if e = json.Unmarshal([]byte(order.Lines), &purchased); e != nil {
			return e
		}
		bySKU := map[string]*domain.PurchaseLine{}
		for i := range purchased {
			bySKU[purchased[i].SKUID] = &purchased[i]
		}
		for _, line := range lines {
			p := bySKU[line.SKUID]
			if p == nil || line.Quantity > p.Quantity-p.Received {
				return status.Error(codes.FailedPrecondition, "收货数量超过未收数量或规格不属于此采购单")
			}
		}
		for _, line := range lines {
			stock := domain.Stock{SKUID: line.SKUID}
			if e = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&stock).Error; e != nil {
				return e
			}
			if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&stock, "sku_id = ?", line.SKUID).Error; e != nil {
				return e
			}
			good := line.Quantity - line.Damaged
			if stock.OnHand+good > 1000000000 {
				return status.Error(codes.FailedPrecondition, "库存数量超过上限")
			}
			if e = tx.Model(&stock).Update("on_hand", gorm.Expr("on_hand + ?", good)).Error; e != nil {
				return e
			}
			for _, move := range []struct {
				kind string
				qty  int64
			}{{constants.PurchaseMovementReceipt, good}, {constants.PurchaseMovementDamaged, line.Damaged}} {
				if move.qty > 0 {
					if e = tx.Create(&domain.Ledger{ID: receiptID + ":" + line.SKUID + ":" + move.kind, ReferenceID: order.ID, UserID: c.UserID, SKUID: line.SKUID, Quantity: move.qty, Kind: move.kind}).Error; e != nil {
						return e
					}
				}
			}
			p := bySKU[line.SKUID]
			p.Received += line.Quantity
			p.Damaged += line.Damaged
		}
		statusValue := constants.PurchaseStatusReceived
		for _, p := range purchased {
			if p.Received < p.Quantity {
				statusValue = constants.PurchaseStatusPartiallyReceived
			}
		}
		data, _ := json.Marshal(purchased)
		if e = tx.Model(&order).Updates(map[string]any{"lines": string(data), "status": statusValue, "version": order.Version + 1, "updated_at": time.Now().UTC()}).Error; e != nil {
			return e
		}
		received, _ := json.Marshal(lines)
		return tx.Create(
			&domain.PurchaseReceipt{
				ID:          receiptID,
				PurchaseID:  order.ID,
				ActorID:     c.UserID,
				Lines:       string(received),
				Fingerprint: fingerprint,
				Note:        note,
			},
		).Error
	})
	if err != nil {
		return nil, err
	}
	return r.PurchaseGet(ctx, c.ID)
}
