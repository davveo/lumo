package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/fulfillmentplan"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/fulfillment/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PlanState struct {
	Lines   []fulfillmentplan.Progress `json:"lines"`
	Parcels []domain.Record            `json:"parcels"`
	Version int64                      `json:"version"`
}

func state(root domain.Record, children []domain.Record) (PlanState, error) {
	lines, e := fulfillmentplan.Parse(root.LinesJSON)
	if e != nil {
		return PlanState{}, e
	}
	out := PlanState{Version: root.Version, Parcels: []domain.Record{}}
	indexes := map[string]int{}
	for _, l := range lines {
		indexes[l.SKUID] = len(out.Lines)
		p := fulfillmentplan.Progress{SKUID: l.SKUID, Quantity: l.Quantity}
		if !root.Parcelized {
			if root.Status == constants.FulfillmentStatusDispatched {
				p.Shipped = l.Quantity
			}
			if root.Status == constants.FulfillmentStatusDispatching {
				p.Dispatching = l.Quantity
			}
			if root.Status == constants.FulfillmentStatusCancelled {
				p.Cancelled = l.Quantity
			}
		}
		out.Lines = append(out.Lines, p)
	}
	for _, r := range children {
		ls, e := fulfillmentplan.Parse(r.LinesJSON)
		if e != nil {
			return out, e
		}
		if r.Kind == constants.FulfillmentKindParcel {
			out.Parcels = append(out.Parcels, r)
		}
		for _, l := range ls {
			idx, ok := indexes[l.SKUID]
			if !ok {
				return out, status.Error(codes.Internal, "包裹商品与订单不一致")
			}
			p := &out.Lines[idx]
			if r.Kind == constants.FulfillmentKindCancellation {
				switch r.Status {
				case constants.FulfillmentStatusHeld,
					constants.FulfillmentStatusCancelRequested,
					constants.FulfillmentStatusWarehouseCancelConfirmed:
					p.Held += l.Quantity
				case constants.FulfillmentStatusCancelled:
					p.Cancelled += l.Quantity
				}
			}
			if r.Kind == constants.FulfillmentKindParcel {
				switch r.Status {
				case constants.FulfillmentStatusReady:
					p.Planned += l.Quantity
				case constants.FulfillmentStatusDispatching:
					p.Dispatching += l.Quantity
				case constants.FulfillmentStatusDispatched:
					p.Shipped += l.Quantity
				case constants.FulfillmentStatusDelivered:
					p.Shipped += l.Quantity
					p.Delivered += l.Quantity
				}
			}
		}
	}
	for i := range out.Lines {
		p := &out.Lines[i]
		p.Remaining = p.Quantity - p.Cancelled - p.Shipped - p.Dispatching - p.Held
		if p.Remaining < 0 || p.Planned > p.Remaining {
			return out, status.Error(codes.Internal, "订单行履约数量异常")
		}
	}
	return out, nil
}
func children(ctx context.Context, db *gorm.DB, id string) ([]domain.Record, error) {
	var rows []domain.Record
	e := db.WithContext(ctx).Where("parent_id = ?", id).Order("created_at asc,id asc").Find(&rows).Error
	return rows, e
}
func (r *Repository) PlanState(ctx context.Context, id string) (PlanState, error) {
	ctx, sp := platform.Start(ctx, "fulfillment.repository.PlanState")
	defer sp.End()
	var out PlanState
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var root domain.Record
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&root, "id = ?", id).Error; e != nil {
			return e
		}
		rows, e := children(ctx, tx, id)
		if e != nil {
			return e
		}
		out, e = state(root, rows)
		return e
	})
	return out, e
}

// planMutation holds only the owning warehouse rows; remote effects are executed
// after commit and their immutable operation receipts make recovery safe.
func (r *Repository) planMutation(
	ctx context.Context,
	id string,
	fn func(*gorm.DB, *domain.Record, []domain.Record, PlanState) error,
) (*domain.Record, error) {
	ctx, sp := platform.Start(ctx, "fulfillment.repository.PlanMutation")
	defer sp.End()
	var root domain.Record
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&root, "id = ?", id).Error; e != nil {
			return e
		}
		if root.Kind != constants.FulfillmentKindOrder {
			return status.Error(codes.FailedPrecondition, "仅普通订单支持按行履约")
		}
		rows, e := children(ctx, tx, id)
		if e != nil {
			return e
		}
		before, e := state(root, rows)
		if e != nil {
			return e
		}
		if e = fn(tx, &root, rows, before); e != nil {
			return e
		}
		root.Version++
		if e = tx.WithContext(ctx).Save(&root).Error; e != nil {
			return e
		}
		if !root.Parcelized {
			return nil
		}
		rows, e = children(ctx, tx, id)
		if e != nil {
			return e
		}
		after, e := state(root, rows)
		if e != nil {
			return e
		}
		shipped, remaining := int64(0), int64(0)
		for _, l := range after.Lines {
			shipped += l.Shipped
			remaining += l.Quantity - l.Cancelled - l.Shipped
		}
		root.Status = constants.FulfillmentStatusReady
		if shipped > 0 {
			root.Status = constants.FulfillmentStatusPartial
			if remaining == 0 {
				root.Status = constants.FulfillmentStatusDispatched
			}
		} else if remaining == 0 {
			root.Status = constants.FulfillmentStatusCancelled
		}
		if e = tx.WithContext(ctx).Save(&root).Error; e != nil {
			return e
		}
		return platform.Enqueue(
			ctx,
			tx,
			platform.Event{
				ID:          fmt.Sprintf("fp-%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d", root.ID, root.Version)))),
				Type:        constants.EventFulfillmentProgress,
				AggregateID: root.ID,
				UserID:      root.UserID,
				Data:        map[string]any{"order_id": root.OrderID, "version": root.Version, "lines": after.Lines},
			},
		)
	})
	return &root, e
}
func sameOperation(rows []domain.Record, id, kind, encoded string) (*domain.Record, error) {
	for i := range rows {
		if rows[i].ID == id {
			if rows[i].Kind != kind || rows[i].LinesJSON != encoded {
				return nil, status.Error(codes.AlreadyExists, "该编号已用于不同商品数量")
			}
			return &rows[i], nil
		}
	}
	return nil, nil
}

func (r *Repository) HoldLines(
	ctx context.Context,
	order, id, encoded string,
	lines []fulfillmentplan.Line,
) (*domain.Record, error) {
	var hold *domain.Record
	_, e := r.planMutation(
		ctx,
		order,
		func(tx *gorm.DB, root *domain.Record, rows []domain.Record, st PlanState) error {
			old, e := sameOperation(rows, id, constants.FulfillmentKindCancellation, encoded)
			if e != nil {
				return e
			}
			if old != nil {
				hold = old
				if old.Status == constants.FulfillmentStatusReleased {
					return status.Error(codes.FailedPrecondition, "此取消申请已释放")
				}
				return nil
			}
			if !root.Parcelized && root.Status != constants.FulfillmentStatusReady {
				return status.Error(codes.FailedPrecondition, "订单已进入出库，无法拦截所选商品")
			}
			limits := map[string]int64{}
			for _, l := range st.Lines {
				limits[l.SKUID] = l.Remaining
			}
			selected := map[string]bool{}
			for _, l := range lines {
				if limits[l.SKUID] < l.Quantity {
					return status.Error(codes.FailedPrecondition, "所选商品已出库或被其他申请占用")
				}
				selected[l.SKUID] = true
			}
			// A stale packing plan must be reviewed again after an item cancellation.
			for _, p := range rows {
				if p.Kind != constants.FulfillmentKindParcel || p.Status != constants.FulfillmentStatusReady {
					continue
				}
				ls, e := fulfillmentplan.Parse(p.LinesJSON)
				if e != nil {
					return e
				}
				affected := false
				for _, l := range ls {
					affected = affected || selected[l.SKUID]
				}
				if affected {
					if e = tx.WithContext(ctx).Model(&p).Updates(map[string]any{"status": constants.FulfillmentStatusInvalidated, "version": gorm.Expr("version + 1")}).Error; e != nil {
						return e
					}
				}
			}
			root.Parcelized = true
			hold = &domain.Record{
				ID:            id,
				ParentID:      order,
				OrderID:       root.OrderID,
				UserID:        root.UserID,
				ReservationID: root.ReservationID,
				Kind:          constants.FulfillmentKindCancellation,
				Status:        constants.FulfillmentStatusHeld,
				LinesJSON:     encoded,
				Version:       1,
			}
			return tx.WithContext(ctx).Create(hold).Error
		},
	)
	return hold, e
}
func (r *Repository) ChangeHold(ctx context.Context, hold *domain.Record, next string) (*domain.Record, error) {
	var out domain.Record
	_, e := r.planMutation(
		ctx,
		hold.ParentID,
		func(tx *gorm.DB, _ *domain.Record, rows []domain.Record, _ PlanState) error {
			for _, row := range rows {
				if row.ID == hold.ID {
					out = row
					break
				}
			}
			if out.ID == "" {
				return status.Error(codes.NotFound, "取消申请不存在")
			}
			if out.Status == next || out.Status == constants.FulfillmentStatusCancelled {
				return nil
			}
			valid := out.Status == constants.FulfillmentStatusHeld &&
				(next == constants.FulfillmentStatusReleased || next == constants.FulfillmentStatusCancelRequested) ||
				out.Status == constants.FulfillmentStatusCancelRequested &&
					next == constants.FulfillmentStatusWarehouseCancelConfirmed ||
				out.Status == constants.FulfillmentStatusWarehouseCancelConfirmed &&
					next == constants.FulfillmentStatusCancelled
			if !valid {
				return status.Error(codes.FailedPrecondition, "取消申请正在执行，不能重复变更")
			}
			out.Status = next
			out.Version++
			return tx.WithContext(ctx).Save(&out).Error
		},
	)
	return &out, e
}

func (r *Repository) CreateParcel(
	ctx context.Context,
	order, id, encoded string,
	lines []fulfillmentplan.Line,
) (*domain.Record, error) {
	var parcel *domain.Record
	_, e := r.planMutation(
		ctx,
		order,
		func(tx *gorm.DB, root *domain.Record, rows []domain.Record, st PlanState) error {
			old, e := sameOperation(rows, id, constants.FulfillmentKindParcel, encoded)
			if e != nil {
				return e
			}
			if old != nil {
				parcel = old
				return nil
			}
			if !root.Parcelized && root.Status != constants.FulfillmentStatusReady {
				return status.Error(codes.FailedPrecondition, "订单已进入整单出库")
			}
			count := 0
			for _, p := range rows {
				if p.Kind == constants.FulfillmentKindParcel {
					count++
				}
			}
			if count >= 100 {
				return status.Error(codes.ResourceExhausted, "每单最多创建100个包裹计划")
			}
			limits := map[string]int64{}
			for _, l := range st.Lines {
				limits[l.SKUID] = l.Remaining - l.Planned
			}
			for _, l := range lines {
				if limits[l.SKUID] < l.Quantity {
					return status.Error(codes.FailedPrecondition, "商品已装包、拦截或出库，请刷新数量")
				}
			}
			root.Parcelized = true
			parcel = &domain.Record{
				ID:            id,
				ParentID:      order,
				OrderID:       root.OrderID,
				UserID:        root.UserID,
				ReservationID: root.ReservationID,
				Kind:          constants.FulfillmentKindParcel,
				Status:        constants.FulfillmentStatusReady,
				LinesJSON:     encoded,
				Version:       1,
			}
			return tx.WithContext(ctx).Create(parcel).Error
		},
	)
	return parcel, e
}

func (r *Repository) ChangeParcel(
	ctx context.Context,
	parcel *domain.Record,
	next, tracking string,
	event *platform.Event,
) (*domain.Record, error) {
	var out domain.Record
	_, e := r.planMutation(
		ctx,
		parcel.ParentID,
		func(tx *gorm.DB, _ *domain.Record, rows []domain.Record, _ PlanState) error {
			for _, row := range rows {
				if row.ID == parcel.ID {
					out = row
					break
				}
			}
			if out.ID == "" {
				return status.Error(codes.NotFound, "包裹不存在")
			}
			if out.Status == next || out.Status == constants.FulfillmentStatusDelivered {
				return nil
			}
			valid := out.Status == constants.FulfillmentStatusReady &&
				(next == constants.FulfillmentStatusDispatching || next == constants.FulfillmentStatusCancelled) ||
				out.Status == constants.FulfillmentStatusDispatching && next == constants.FulfillmentStatusDispatched ||
				out.Status == constants.FulfillmentStatusDispatched && next == constants.FulfillmentStatusDelivered
			if !valid {
				return status.Error(codes.FailedPrecondition, "包裹状态已变化，请刷新后操作")
			}
			out.Status = next
			out.Version++
			if tracking != "" {
				out.TrackingID = tracking
			}
			if e := tx.WithContext(ctx).Save(&out).Error; e != nil {
				return e
			}
			if event != nil {
				return platform.Enqueue(ctx, tx, *event)
			}
			return nil
		},
	)
	return &out, e
}
func (s PlanState) Result() platform.Result {
	raw, _ := json.Marshal(s)
	var out platform.Result
	_ = json.Unmarshal(raw, &out)
	return out
}
