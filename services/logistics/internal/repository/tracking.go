package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/logistics/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func exceptionID(id, kind, epoch string) string {
	return fmt.Sprintf("LXE_%x", sha256.Sum256([]byte(id+"\x00"+kind+"\x00"+epoch)))
}
func openException(ctx context.Context, tx *gorm.DB, row domain.Record, kind, details, epoch string) error {
	issue := domain.DeliveryException{
		ID:          exceptionID(row.ID, kind, epoch),
		LogisticsID: row.ID,
		TrackingID:  row.TrackingID,
		UserID:      row.UserID,
		Kind:        kind,
		Status:      constants.DeliveryExceptionStatusOpen,
		Details:     details,
	}
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&issue)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil
	}
	return platform.Enqueue(
		ctx,
		tx,
		platform.Event{
			ID:          issue.ID,
			Type:        "ShipmentException",
			AggregateID: row.ID,
			UserID:      row.UserID,
			Data: map[string]any{
				"order_id":     row.OrderID,
				"tracking_id":  row.TrackingID,
				"exception_id": issue.ID,
				"kind":         kind,
			},
		},
	)
}
func (r *Repository) Tracking(ctx context.Context, c platform.Request) (platform.Result, error) {
	var records []domain.Record
	query := r.DB.WithContext(ctx).Where("(id = ? OR order_id = ? OR tracking_id = ?)", c.ID, c.ID, c.ID)
	if c.Role != "admin" && c.Role != "system" {
		query = query.Where("user_id = ?", c.UserID)
	}
	if e := query.Order("created_at,id").Limit(100).Find(&records).Error; e != nil {
		return nil, e
	}
	if len(records) == 0 {
		return nil, status.Error(codes.NotFound, "尚无可查看的物流信息")
	}
	offset := int(platform.Int(c.Data, "event_offset"))
	if offset < 0 || offset > 10000 {
		return nil, status.Error(codes.InvalidArgument, "无效的轨迹分页")
	}
	shipments := []platform.Result{}
	for _, row := range records {
		v := platform.Map(row)
		var events []domain.TrackingEvent
		q := r.DB.WithContext(ctx).Where("logistics_id = ?", row.ID)
		var count int64
		if e := q.Model(&domain.TrackingEvent{}).Count(&count).Error; e != nil {
			return nil, e
		}
		if e := q.Order("occurred_at DESC,id DESC").Offset(offset).Limit(100).Find(&events).Error; e != nil {
			return nil, e
		}
		v["events"] = events
		v["events_total"] = count
		v["has_more_events"] = int64(offset+len(events)) < count
		v["next_event_offset"] = offset + len(events)
		shipments = append(shipments, v)
	}
	return platform.Result{"shipments": shipments}, nil
}

func (r *Repository) TrackUpdate(
	ctx context.Context,
	c platform.Request,
	event domain.TrackingEvent,
	provider string,
) (platform.Result, error) {
	var row domain.Record
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var prior domain.TrackingEvent
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "tracking_id = ?", event.TrackingID).Error
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return status.Error(codes.NotFound, "运单尚未登记，请稍后重试")
			}
			return e
		}
		if provider != "" && provider != row.Provider {
			return status.Error(codes.PermissionDenied, "物流渠道不匹配")
		}
		// A locking read sees concurrent committed callbacks under MySQL's
		// REPEATABLE READ isolation, after acquiring the shipment lock.
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&prior, "id = ?", event.ID).Error; e == nil {
			if prior.Fingerprint != event.Fingerprint {
				return status.Error(codes.AlreadyExists, "物流事件编号对应不同内容")
			}
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		event.LogisticsID = row.ID
		if e = tx.Create(&event).Error; e != nil {
			return e
		}
		if row.Status == constants.TrackingStatusDelivered ||
			row.LastEventAt != nil && event.OccurredAt.Before(*row.LastEventAt) ||
			event.Status == constants.TrackingStatusCreated && row.Status != constants.TrackingStatusCreated {
			return nil
		}
		next := constants.TrackingStatusInTransit
		if event.Status == constants.TrackingStatusCreated {
			next = constants.TrackingStatusCreated
		}
		if event.Status == constants.TrackingStatusException {
			next = constants.TrackingStatusException
		}
		if event.Status == constants.TrackingStatusDelivered {
			next = constants.TrackingStatusDelivered
		}
		if e = tx.Model(&row).Updates(map[string]any{"status": next, "last_event_at": event.OccurredAt, "version": gorm.Expr("version + 1")}).Error; e != nil {
			return e
		}
		if next == constants.TrackingStatusException {
			if e = openException(ctx, tx, row, "carrier_exception", event.Description, event.ID); e != nil {
				return e
			}
		}
		if next == constants.TrackingStatusDelivered {
			if e = tx.Model(&domain.DeliveryException{}).Where("logistics_id = ? AND status = ?", row.ID, constants.DeliveryExceptionStatusOpen).Updates(map[string]any{
				"status":      constants.DeliveryExceptionStatusResolved,
				"resolution":  "配送已签收",
				"resolved_by": c.UserID,
				"resolved_at": event.OccurredAt,
				"version":     gorm.Expr("version + 1"),
			}).Error; e != nil {
				return e
			}
			if e = platform.Enqueue(ctx, tx, platform.Event{
				ID:          row.ID + ":delivered",
				Type:        deliveryEventType(row.Kind),
				AggregateID: row.ID,
				UserID:      row.UserID,
				Data: map[string]any{
					"order_id":       row.OrderID,
					"fulfillment_id": row.ID,
					"tracking_id":    row.TrackingID,
					"kind":           row.Kind,
					"provider":       row.Provider,
				},
			}); e != nil {
				return e
			}
		}
		return tx.First(&row, "id = ?", row.ID).Error
	})
	return platform.Map(row), err
}
func (r *Repository) Exceptions(ctx context.Context, c platform.Request) (platform.Result, error) {
	rows, total, f, e := adminquery.Page[domain.DeliveryException](
		ctx,
		r.DB,
		c.Data,
		adminquery.Spec{State: "status", Search: []string{"id", "tracking_id", "logistics_id", "details"}},
	)
	return adminquery.Result(rows, total, f), e
}
func (r *Repository) ResolveException(ctx context.Context, c platform.Request) (platform.Result, error) {
	var out domain.DeliveryException
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", c.ID).Error; e != nil {
			return e
		}
		if out.Status == constants.DeliveryExceptionStatusResolved {
			return nil
		}
		if out.Version != platform.Int(c.Data, "expected_version") {
			return status.Error(codes.Aborted, "异常记录已更新，请刷新")
		}
		if e := tx.Model(&out).Updates(map[string]any{
			"status":      constants.DeliveryExceptionStatusResolved,
			"resolution":  platform.Text(c.Data, "resolution"),
			"resolved_by": c.UserID,
			"resolved_at": time.Now().UTC(),
			"version":     out.Version + 1,
		}).Error; e != nil {
			return e
		}
		return tx.First(&out, "id = ?", c.ID).Error
	})
	return platform.Map(out), err
}
func (r *Repository) ScanDeliveryExceptions(ctx context.Context, pickup, stale time.Duration) error {
	now := time.Now().UTC()
	var rows []domain.Record
	return r.DB.WithContext(ctx).
		Where("(status = ? AND created_at <= ?) OR (status = ? AND COALESCE(last_event_at,created_at) <= ?)", constants.TrackingStatusCreated, now.Add(-pickup), constants.TrackingStatusInTransit, now.Add(-stale)).
		FindInBatches(&rows, 100, func(batch *gorm.DB, _ int) error {
			for _, row := range rows {
				if e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", row.ID).Error; e != nil {
						return e
					}
					last := row.CreatedAt
					if row.LastEventAt != nil {
						last = *row.LastEventAt
					}
					if !(row.Status == constants.TrackingStatusCreated && !row.CreatedAt.After(now.Add(-pickup))) &&
						!(row.Status == constants.TrackingStatusInTransit && !last.After(now.Add(-stale))) {
						return nil
					}
					kind, details, epoch := "late_pickup", "包裹超过揽收时限", row.CreatedAt.UTC().Format(time.RFC3339Nano)
					if row.Status == constants.TrackingStatusInTransit {
						kind = "stalled"
						details = "运输轨迹长时间未更新"
						if row.LastEventAt != nil {
							epoch = row.LastEventAt.UTC().Format(time.RFC3339Nano)
						}
					}
					return openException(ctx, tx, row, kind, details, epoch)
				}); e != nil {
					return e
				}
			}
			return nil
		}).
		Error
}
