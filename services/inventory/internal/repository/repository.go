package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/democatalog"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"os"
	"sort"
)

type Repository struct{ DB *gorm.DB }

func (r *Repository) Migrate(ctx context.Context) error {
	return r.DB.WithContext(ctx).AutoMigrate(&domain.Stock{}, &domain.Reservation{}, &domain.Ledger{})
}
func (r *Repository) Seed(ctx context.Context) error {
	ids := append([]string{"sku-phone", "sku-headphones", "sku-watch", "sku-coffee"}, democatalog.SKUs()...)
	if os.Getenv("DEMO_MODE") == "true" || os.Getenv("DEMO_MODE") == "1" {
		ids = append(ids, democatalog.ExpandedSKUs()...)
	}
	stocks := make([]domain.Stock, 0, len(ids))
	for _, id := range ids {
		stocks = append(stocks, domain.Stock{SKUID: id, OnHand: 50})
	}
	// Only absent SKU rows are initialized; restarts must preserve actual stock,
	// reservations, allocations and movement history from the demo purchase flow.
	return r.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(stocks, 100).Error
}
func (r *Repository) Get(ctx context.Context, id string) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.inventory.Get")
	defer s.End()
	var v domain.Stock
	if e := r.DB.WithContext(ctx).First(&v, "sku_id = ?", id).Error; e != nil {
		return nil, status.Error(codes.NotFound, "stock not found")
	}
	return platform.Result{
		"sku_id":    v.SKUID,
		"on_hand":   v.OnHand,
		"reserved":  v.Reserved,
		"allocated": v.Allocated,
		"available": v.OnHand - v.Reserved - v.Allocated,
	}, nil
}
func (r *Repository) GetMany(ctx context.Context, ids []string) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "repository.inventory.GetMany")
	defer span.End()
	rows := []domain.Stock{}
	if err := r.DB.WithContext(ctx).Where("sku_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	byID := map[string]domain.Stock{}
	for _, row := range rows {
		byID[row.SKUID] = row
	}
	items, missing := []platform.Result{}, []string{}
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			items = append(
				items,
				platform.Result{
					"sku_id":    row.SKUID,
					"on_hand":   row.OnHand,
					"reserved":  row.Reserved,
					"allocated": row.Allocated,
					"available": row.OnHand - row.Reserved - row.Allocated,
				},
			)
		} else {
			missing = append(missing, id)
		}
	}
	return platform.Result{"items": items, "missing_ids": missing}, nil
}
func normalized(lines []domain.Line) ([]domain.Line, error) {
	q := map[string]int64{}
	for _, l := range lines {
		if l.SKUID == "" || l.Quantity < 1 || l.Quantity > 10 {
			return nil, status.Error(codes.InvalidArgument, "quantity must be 1..10")
		}
		q[l.SKUID] += l.Quantity
		if q[l.SKUID] > 10 {
			return nil, status.Error(codes.InvalidArgument, "aggregated SKU quantity exceeds 10")
		}
	}
	if len(q) == 0 {
		return nil, status.Error(codes.InvalidArgument, "lines required")
	}
	out := make([]domain.Line, 0, len(q))
	for k, n := range q {
		out = append(out, domain.Line{SKUID: k, Quantity: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SKUID < out[j].SKUID })
	return out, nil
}

func (r *Repository) Reserve(
	ctx context.Context,
	c platform.Command,
	order string,
	lines []domain.Line,
) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.inventory.Reserve")
	defer s.End()
	if c.ID == "" || order == "" {
		return nil, status.Error(codes.InvalidArgument, "reservation and order IDs required")
	}
	ls, e := normalized(lines)
	if e != nil {
		return nil, e
	}
	b, _ := json.Marshal(ls)
	var out domain.Reservation
	e = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old domain.Reservation
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", c.ID).Error
		if er == nil {
			if old.UserID != c.UserID {
				return status.Error(codes.PermissionDenied, "reservation belongs to another user")
			}
			if old.State == constants.InventoryReservationStateCancelled {
				return status.Error(codes.FailedPrecondition, "reservation cancelled")
			}
			if old.OrderID != order || old.LinesJSON != string(b) {
				return status.Error(codes.AlreadyExists, "reservation payload differs")
			}
			out = old
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		out = domain.Reservation{
			ID:        c.ID,
			UserID:    c.UserID,
			OrderID:   order,
			State:     constants.InventoryReservationStateReserved,
			LinesJSON: string(b),
		}
		if er = tx.WithContext(ctx).Create(&out).Error; er != nil {
			return er
		}
		for _, l := range ls {
			v := tx.WithContext(ctx).
				Model(&domain.Stock{}).
				Where("sku_id = ? AND on_hand - reserved - allocated >= ?", l.SKUID, l.Quantity).
				Update("reserved", gorm.Expr("reserved + ?", l.Quantity))
			if v.Error != nil {
				return v.Error
			}
			if v.RowsAffected != 1 {
				return status.Error(codes.ResourceExhausted, "insufficient SKU stock")
			}
			if er = movement(ctx, tx, c.ID, c.UserID, l, constants.InventoryMovementKindReserve); er != nil {
				return er
			}
		}
		return nil
	})
	return platform.Map(out), e
}
func (r *Repository) Transition(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.inventory."+c.Action)
	defer s.End()
	if c.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "reservation ID required")
	}
	var out domain.Reservation
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", c.ID).Error
		if errors.Is(er, gorm.ErrRecordNotFound) && c.Action == constants.ActionCancel {
			out = domain.Reservation{
				ID:        c.ID,
				UserID:    c.UserID,
				State:     constants.InventoryReservationStateCancelled,
				LinesJSON: "[]",
			}
			return tx.WithContext(ctx).Create(&out).Error
		}
		if er != nil {
			if !errors.Is(er, gorm.ErrRecordNotFound) {
				return er
			}
			return status.Error(codes.NotFound, "reservation not found")
		}
		if out.UserID != c.UserID {
			return status.Error(codes.PermissionDenied, "reservation belongs to another user")
		}
		target := ""
		switch c.Action {
		case constants.ActionConfirm:
			target = constants.InventoryReservationStateAllocated
		case constants.ActionCancel:
			target = constants.InventoryReservationStateCancelled
		case constants.ActionDispatch:
			target = constants.InventoryReservationStateDispatched
		}
		if out.State == target {
			return nil
		}
		if out.State == constants.InventoryReservationStateCancelled ||
			out.State == constants.InventoryReservationStateDispatched {
			return status.Error(codes.FailedPrecondition, "terminal reservation")
		}
		var ls []domain.Line
		if er = json.Unmarshal([]byte(out.LinesJSON), &ls); er != nil {
			return er
		}
		for _, l := range ls {
			u := map[string]any{}
			switch c.Action {
			case constants.ActionConfirm:
				if out.State != constants.InventoryReservationStateReserved {
					return status.Error(codes.FailedPrecondition, "must be reserved")
				}
				u["reserved"] = gorm.Expr("reserved - ?", l.Quantity)
				u["allocated"] = gorm.Expr("allocated + ?", l.Quantity)
			case constants.ActionDispatch:
				if out.State != constants.InventoryReservationStateAllocated {
					return status.Error(codes.FailedPrecondition, "must be allocated before dispatch")
				}
				u["on_hand"] = gorm.Expr("on_hand - ?", l.Quantity)
				u["allocated"] = gorm.Expr("allocated - ?", l.Quantity)
			case constants.ActionCancel:
				if out.State == constants.InventoryReservationStateAllocated {
					if yes, _ := c.Data["warehouse_confirmed"].(bool); !yes {
						return status.Error(codes.FailedPrecondition, "warehouse cancellation confirmation required")
					}
					u["allocated"] = gorm.Expr("allocated - ?", l.Quantity)
				} else {
					u["reserved"] = gorm.Expr("reserved - ?", l.Quantity)
				}
			}
			if er = tx.WithContext(ctx).Model(&domain.Stock{}).Where("sku_id = ?", l.SKUID).Updates(
				u,
			).Error; er != nil {
				return er
			}
			if er = movement(ctx, tx, c.ID, c.UserID, l, target); er != nil {
				return er
			}
		}
		if er = tx.WithContext(ctx).Model(&out).Update("state", target).Error; er != nil {
			return er
		}
		out.State = target
		return nil
	})
	return platform.Map(out), e
}
func (r *Repository) Restock(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.inventory.Restock")
	defer s.End()
	sku := platform.Text(c.Data, "sku_id")
	qty := platform.Int(c.Data, "quantity")
	condition := platform.Text(c.Data, "condition")
	if c.ID == "" || sku == "" || qty < 1 || qty > 100000 ||
		(condition != constants.StockConditionSellable && condition != constants.StockConditionDamaged &&
			condition != constants.StockConditionUnsellable) {
		return nil, status.Error(codes.InvalidArgument, "valid inspection, SKU, quantity, condition required")
	}
	l := domain.Ledger{ID: c.ID, ReferenceID: c.ID, UserID: c.UserID, SKUID: sku, Quantity: qty, Kind: condition}
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old domain.Ledger
		if er := tx.WithContext(ctx).First(&old, "id = ?", c.ID).Error; er == nil {
			if old.SKUID != sku || old.Quantity != qty || old.Kind != condition {
				return status.Error(codes.AlreadyExists, "inspection payload differs")
			}
			return nil
		} else if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		var st domain.Stock
		if er := tx.WithContext(ctx).First(&st, "sku_id = ?", sku).Error; er != nil {
			return status.Error(codes.NotFound, "stock not found")
		}
		if er := tx.WithContext(ctx).Create(&l).Error; er != nil {
			return er
		}
		if condition == constants.StockConditionSellable {
			return tx.WithContext(ctx).
				Model(&domain.Stock{}).
				Where("sku_id = ?", sku).
				Update("on_hand", gorm.Expr("on_hand + ?", qty)).
				Error
		}
		return nil
	})
	return platform.Map(l), e
}
func (r *Repository) Adjust(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.inventory.Adjust")
	defer s.End()
	qty := platform.Int(c.Data, "on_hand")
	if c.ID == "" || qty < 0 {
		return nil, status.Error(codes.InvalidArgument, "nonnegative on_hand required")
	}
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var st domain.Stock
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&st, "sku_id = ?", c.ID).Error
		if errors.Is(er, gorm.ErrRecordNotFound) {
			return tx.WithContext(ctx).Create(&domain.Stock{SKUID: c.ID, OnHand: qty}).Error
		}
		if er != nil {
			return er
		}
		if qty < st.Reserved+st.Allocated {
			return status.Error(codes.FailedPrecondition, "cannot set stock below committed resources")
		}
		return tx.WithContext(ctx).Model(&st).Update("on_hand", qty).Error
	})
	if e != nil {
		return nil, e
	}
	return r.Get(ctx, c.ID)
}
func movement(ctx context.Context, tx *gorm.DB, ref, user string, line domain.Line, kind string) error {
	sum := sha256.Sum256([]byte(ref + "\x00" + line.SKUID + "\x00" + kind))
	v := domain.Ledger{
		ID:          "move-" + hex.EncodeToString(sum[:]),
		ReferenceID: ref,
		UserID:      user,
		SKUID:       line.SKUID,
		Quantity:    line.Quantity,
		Kind:        kind,
	}
	return tx.WithContext(ctx).Create(&v).Error
}
