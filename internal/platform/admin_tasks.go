package platform

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"github.com/davveo/lumo/internal/constants"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type Recovery struct {
	ID         string    `gorm:"primaryKey;size:80" json:"id"`
	EventID    string    `gorm:"size:80;index" json:"event_id"`
	ActorID    string    `gorm:"size:128" json:"actor_id"`
	Reason     string    `gorm:"size:240" json:"reason"`
	State      string    `gorm:"size:20;index" json:"state"`
	Error      string    `gorm:"size:255" json:"error,omitempty"`
	LeaseUntil time.Time `json:"-"`
	Claim      string    `gorm:"size:80" json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
type TaskAudit struct {
	ID        string    `gorm:"primaryKey;size:80" json:"id"`
	EventID   string    `gorm:"size:80;index" json:"event_id"`
	Kind      string    `gorm:"size:20" json:"kind"`
	ActorID   string    `gorm:"size:128" json:"actor_id"`
	Reason    string    `gorm:"size:240" json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

func ServiceNames() []string { return append([]string{}, services...) }
func WithAdministration(m Module, db *gorm.DB) Module {
	if db == nil {
		return m
	}
	m.Administration = map[string]Operation{constants.ActionTasks: func(ctx context.Context, c Request) (Result, error) { return serviceTasks(ctx, db, m.Name, c) }, constants.ActionRetry: func(ctx context.Context, c Request) (Result, error) { return retryServiceTask(ctx, db, m, c) }}
	original := m.Handle
	m.Handle = func(ctx context.Context, c Command) (Result, error) {
		if op := m.Administration[c.Action]; op != nil {
			return op(ctx, c.Request())
		}
		return original(ctx, c)
	}
	tick := m.Tick
	m.Tick = func(ctx context.Context) error {
		if e := processRecovery(ctx, db, m); e != nil {
			return e
		}
		if tick != nil {
			return tick(ctx)
		}
		return nil
	}
	return m
}
func serviceTasks(ctx context.Context, db *gorm.DB, name string, c Request) (Result, error) {
	ctx, sp := Start(ctx, name+".repository.Tasks")
	defer sp.End()
	if e := RequireAdmin(c); e != nil {
		return nil, e
	}
	page, size := Int(c.Data, "page"), Int(c.Data, "page_size")
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	kind := Text(c.Data, "kind")
	if kind == "" {
		kind = "outbox"
	}
	keyword := Text(c.Data, "keyword")
	state := Text(c.Data, "status")
	if page < 1 || page > 1000000 || size < 1 || size > 100 || len([]rune(keyword)) > 120 || len(state) > 40 {
		return nil, status.Error(codes.InvalidArgument, "invalid task page")
	}
	q := db.WithContext(ctx)
	var model any
	var order, search string
	switch kind {
	case "outbox":
		model = &Outbox{}
		order = "created_at DESC,id DESC"
		search = "id"
	case "inbox":
		model = &Inbox{}
		order = "updated_at DESC,event_id DESC"
		search = "event_id"
		q = q.Where("consumer = ?", name)
	case "recovery":
		model = &Recovery{}
		order = "created_at DESC,id DESC"
		search = "event_id"
	case "audit":
		model = &TaskAudit{}
		order = "created_at DESC,id DESC"
		search = "event_id"
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid task kind")
	}
	q = q.Model(model)
	if state != "" && kind != "audit" {
		q = q.Where("state = ?", state)
	}
	if keyword != "" {
		q = q.Where(search+" LIKE ? ESCAPE '!'", "%"+strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(keyword)+"%")
	}
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	q = q.Order(order).Limit(int(size)).Offset(int((page - 1) * size))
	items := []Result{}
	switch kind {
	case "outbox":
		rows := []Outbox{}
		if e := q.Find(&rows).Error; e != nil {
			return nil, e
		}
		for _, v := range rows {
			carrier := map[string]string{}
			_ = json.Unmarshal([]byte(v.CarrierJSON), &carrier)
			trace := ""
			if parts := strings.Split(carrier["traceparent"], "-"); len(parts) == 4 && len(parts[1]) == 32 {
				trace = parts[1]
			}
			items = append(items, Result{"id": v.ID, "event_type": v.EventType, "state": v.State, "attempts": v.Attempts, "next_attempt_at": v.NextAttemptAt, "lease_until": v.LeaseUntil, "created_at": v.CreatedAt, "trace_id": trace, "retryable": v.State != constants.OutboxStatePublished && !v.LeaseUntil.After(time.Now().UTC())})
		}
	case "inbox":
		rows := []Inbox{}
		if e := q.Find(&rows).Error; e != nil {
			return nil, e
		}
		for _, v := range rows {
			items = append(items, Result{"id": v.EventID, "consumer": v.Consumer, "state": v.State, "attempts": v.Attempts, "updated_at": v.UpdatedAt, "retryable": v.PayloadJSON != "" && (v.State == constants.InboxStateDead || v.State == constants.InboxStateRetry || v.State == constants.InboxStateProcessing && !v.LeaseUntil.After(time.Now().UTC()))})
		}
	case "recovery":
		rows := []Recovery{}
		if e := q.Find(&rows).Error; e != nil {
			return nil, e
		}
		for _, v := range rows {
			items = append(items, Map(v))
		}
	case "audit":
		rows := []TaskAudit{}
		if e := q.Find(&rows).Error; e != nil {
			return nil, e
		}
		for _, v := range rows {
			items = append(items, Map(v))
		}
	}
	return Result{"items": items, "total": total, "page": page, "page_size": size, "service": name, "kind": kind}, nil
}
func retryServiceTask(ctx context.Context, db *gorm.DB, m Module, c Request) (Result, error) {
	ctx, sp := Start(ctx, m.Name+".usecase.Retry")
	defer sp.End()
	if e := RequireAdmin(c); e != nil {
		return nil, e
	}
	reason := strings.TrimSpace(Text(c.Data, "reason"))
	kind := Text(c.Data, "kind")
	if c.ID == "" || len([]rune(c.ID)) > 80 || c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 || reason == "" || len([]rune(reason)) > 240 || (kind != "outbox" && kind != "inbox") {
		return nil, status.Error(codes.InvalidArgument, "event id, kind, reason and idempotency key required")
	}
	hash := sha256.Sum256([]byte(c.UserID + ":" + c.IdempotencyKey))
	id := fmt.Sprintf("recovery-%x", hash[:16])
	e := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var audit TaskAudit
		if e := tx.WithContext(ctx).Where("id = ?", id).Find(&audit).Error; e != nil {
			return e
		}
		if audit.ID != "" {
			if audit.Kind != kind || audit.EventID != c.ID || audit.Reason != reason {
				return status.Error(codes.AlreadyExists, "retry key reused with different operation")
			}
			return nil
		}
		now := time.Now().UTC()
		if kind == "outbox" {
			v := tx.WithContext(ctx).Model(&Outbox{}).Where("id = ? AND state IN ? AND lease_until <= ?", c.ID, []string{constants.OutboxStatePending, constants.OutboxStatePublishing}, now).Updates(map[string]any{"state": constants.OutboxStatePending, "next_attempt_at": now, "claim_token": "", "lease_until": now})
			if v.Error != nil {
				return v.Error
			}
			if v.RowsAffected != 1 {
				return status.Error(codes.FailedPrecondition, "published or leased event cannot be retried")
			}
		} else {
			if m.OnEvent == nil {
				return status.Error(codes.FailedPrecondition, "service has no event consumer")
			}
			var inbox Inbox
			if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("consumer = ? AND event_id = ?", m.Name, c.ID).First(&inbox).Error; e != nil {
				return e
			}
			if inbox.PayloadJSON == "" || inbox.State == constants.InboxStateDone || inbox.LeaseUntil.After(now) {
				return status.Error(codes.FailedPrecondition, "completed, leased or historical envelope-free messages cannot be replayed")
			}
			if e := tx.WithContext(ctx).Create(&Recovery{ID: id, EventID: c.ID, ActorID: c.UserID, Reason: reason, State: constants.InboxStateRetry}).Error; e != nil {
				return e
			}
		}
		return tx.WithContext(ctx).Create(&TaskAudit{ID: id, EventID: c.ID, Kind: kind, ActorID: c.UserID, Reason: reason}).Error
	})
	return Result{"id": id, "event_id": c.ID, "queued": e == nil}, e
}
func processRecovery(ctx context.Context, db *gorm.DB, m Module) error {
	if m.OnEvent == nil {
		return nil
	}
	ctx, sp := Start(ctx, m.Name+".usecase.Recovery")
	defer sp.End()
	var job Recovery
	now := time.Now().UTC()
	e := db.WithContext(ctx).Where("state = ? AND lease_until <= ?", constants.InboxStateRetry, now).Order("created_at").First(&job).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	token := ID("recovery-claim")
	v := db.WithContext(ctx).Model(&Recovery{}).Where("id = ? AND state = ? AND lease_until <= ?", job.ID, constants.InboxStateRetry, now).Updates(map[string]any{"claim": token, "lease_until": now.Add(time.Minute)})
	if v.Error != nil {
		return v.Error
	}
	if v.RowsAffected != 1 {
		return nil
	}
	finish := func(state, message string) error {
		return db.WithContext(ctx).Model(&Recovery{}).Where("id = ? AND claim = ?", job.ID, token).Updates(map[string]any{"state": state, "error": message, "claim": ""}).Error
	}
	var in Inbox
	if e = db.WithContext(ctx).Where("consumer = ? AND event_id = ?", m.Name, job.EventID).First(&in).Error; e != nil {
		return finish(constants.InboxStateDead, "inbox unavailable")
	}
	if in.State == constants.InboxStateDone {
		return finish(constants.InboxStateDone, "")
	}
	if in.LeaseUntil.After(now) {
		return nil
	}
	var event Event
	if json.Unmarshal([]byte(in.PayloadJSON), &event) != nil || validateEventMetadata(event) != nil || !moduleWants(m, event.Type) {
		return finish(constants.InboxStateDead, "event envelope is not replayable")
	}
	if in.State == constants.InboxStateDead {
		v := db.WithContext(ctx).Model(&Inbox{}).Where("consumer = ? AND event_id = ? AND state = ? AND claim_token = ?", m.Name, in.EventID, in.State, in.ClaimToken).Update("state", constants.InboxStateRetry)
		if v.Error != nil {
			return v.Error
		}
	}
	claim, owned, e := claimInbox(ctx, db, m.Name, in.EventID)
	if e != nil {
		return e
	}
	if !owned {
		return nil
	}
	carrier := propagation.MapCarrier{}
	_ = json.Unmarshal([]byte(in.CarrierJSON), &carrier)
	parent := otel.GetTextMapPropagator().Extract(ctx, carrier)
	handler, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	e = m.OnEvent(handler, event)
	if e != nil {
		if err := markInbox(ctx, db, claim, constants.InboxStateDead); err != nil {
			return err
		}
		return finish(constants.InboxStateDead, status.Code(e).String())
	}
	if e = markInbox(ctx, db, claim, constants.InboxStateDone); e != nil {
		return e
	}
	return finish(constants.InboxStateDone, "")
}

type platformAdminServer struct {
	mallv1.UnimplementedPlatformAdminServiceServer
	ops map[string]Operation
}

func (s *platformAdminServer) Tasks(ctx context.Context, c *mallv1.ServiceRequest) (*mallv1.ServiceResponse, error) {
	return serveMethod(ctx, s.ops[constants.ActionTasks], c)
}
func (s *platformAdminServer) Retry(ctx context.Context, c *mallv1.ServiceRequest) (*mallv1.ServiceResponse, error) {
	return serveMethod(ctx, s.ops[constants.ActionRetry], c)
}
func ReadServiceTasks(ctx context.Context, caller Caller, service string, c Request) (Result, error) {
	return caller.Call(ctx, service, c.Command(constants.ActionTasks))
}
func RetryServiceTask(ctx context.Context, caller Caller, service string, c Request) (Result, error) {
	return caller.Call(ctx, service, c.Command(constants.ActionRetry))
}
