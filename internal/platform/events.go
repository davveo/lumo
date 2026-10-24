package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const eventExchange = "lumo.events"
const deadExchange = "lumo.dead"

// Outbox belongs to the same schema and SQL transaction as its producer's
// business mutation. CarrierJSON is stored with it, not recreated by the worker.
type Outbox struct {
	ID            string `gorm:"primaryKey;size:80"`
	EventType     string `gorm:"size:100;index"`
	PayloadJSON   string `gorm:"type:text"`
	CarrierJSON   string `gorm:"type:text"`
	State         string `gorm:"size:20;index"`
	Attempts      int
	ClaimToken    string    `gorm:"size:80"`
	NextAttemptAt time.Time `gorm:"index"`
	LeaseUntil    time.Time `gorm:"index"`
	PublishedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
type Inbox struct {
	Consumer   string `gorm:"primaryKey;size:80"`
	EventID    string `gorm:"primaryKey;size:80"`
	State      string `gorm:"size:20"`
	Attempts   int
	ClaimToken string `gorm:"size:80"`
	LeaseUntil time.Time
	UpdatedAt  time.Time
}

func MigratePlatform(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).AutoMigrate(&Outbox{}, &Inbox{})
}
func Enqueue(ctx context.Context, tx *gorm.DB, event Event) error {
	if event.Type == "" {
		return fmt.Errorf("event type is required")
	}
	if event.ID == "" {
		event.ID = ID("evt")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("serialize event: %w", err)
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	headers, err := json.Marshal(carrier)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	row := Outbox{ID: event.ID, EventType: event.Type, PayloadJSON: string(payload), CarrierJSON: string(headers), State: "PENDING", NextAttemptAt: now, LeaseUntil: now}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

// All queues are declared before the first publish. Binding all service queues
// once avoids losing startup events before a subscriber starts. Consumers apply
// their module's topic allowlist; this development topology can later be narrowed
// centrally without changing the event contract.
var services = []string{"iam", "member", "address", "catalog", "media", "interaction", "engagement", "search", "recommendation", "content", "analytics", "cart", "pricing", "checkout", "order", "campaign", "coupon", "points", "group", "flash", "inventory", "fulfillment", "logistics", "aftersale", "payment", "refund", "invoice", "reconciliation", "notification", "helpdesk"}

func queueName(service string) string { return "lumo." + service + ".events" }
func declareTopology(ch *amqp.Channel, service string) error {
	if err := ch.ExchangeDeclare(eventExchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.ExchangeDeclare(deadExchange, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	names := append([]string(nil), services...)
	found := false
	for _, n := range names {
		if n == service {
			found = true
		}
	}
	if !found {
		names = append(names, service)
	}
	for _, name := range names {
		q := queueName(name)
		if _, err := ch.QueueDeclare(q, true, false, false, false, amqp.Table{"x-dead-letter-exchange": deadExchange, "x-dead-letter-routing-key": name}); err != nil {
			return err
		}
		if err := ch.QueueBind(q, "#", eventExchange, false, nil); err != nil {
			return err
		}
		if _, err := ch.QueueDeclare("lumo."+name+".dead", true, false, false, false, nil); err != nil {
			return err
		}
		if err := ch.QueueBind("lumo."+name+".dead", name, deadExchange, false, nil); err != nil {
			return err
		}
		if _, err := ch.QueueDeclare("lumo."+name+".retry", true, false, false, false, amqp.Table{"x-message-ttl": int32(2000), "x-dead-letter-exchange": "", "x-dead-letter-routing-key": q}); err != nil {
			return err
		}
	}
	return nil
}

type confirmedPublisher struct {
	*amqp.Channel
	returns <-chan amqp.Return
}

func newConfirmedPublisher(ch *amqp.Channel) (*confirmedPublisher, error) {
	if err := ch.Confirm(false); err != nil {
		return nil, err
	}
	return &confirmedPublisher{Channel: ch, returns: ch.NotifyReturn(make(chan amqp.Return, 8))}, nil
}

func publishConfirmed(ctx context.Context, ch *confirmedPublisher, exchange, key string, body []byte, headers amqp.Table, messageID string) error {
drain:
	for {
		select {
		case _, open := <-ch.returns:
			if !open {
				return fmt.Errorf("publisher channel closed")
			}
		default:
			break drain
		}
	}
	confirm, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, key, true, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: messageID, Timestamp: time.Now().UTC(), Headers: headers, Body: body})
	if err != nil {
		return err
	}
	if confirm == nil {
		return fmt.Errorf("publisher confirms not enabled")
	}
	ok, err := confirm.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("broker rejected publish")
	}
	// RabbitMQ delivers basic.return before its confirm for an unroutable
	// mandatory message. A confirm alone is not proof of successful routing.
	for {
		select {
		case returned, open := <-ch.returns:
			if !open {
				return fmt.Errorf("publisher channel closed")
			}
			if returned.MessageId == messageID {
				return fmt.Errorf("event has no queue route")
			}
		default:
			return nil
		}
	}
}

func publishOne(ctx context.Context, db *gorm.DB, ch *confirmedPublisher, row Outbox) (traceErr error) {
	carrier := propagation.MapCarrier{}
	if err := json.Unmarshal([]byte(row.CarrierJSON), &carrier); err != nil {
		return err
	}
	parent := otel.GetTextMapPropagator().Extract(ctx, carrier)
	ctx, span := otel.Tracer("lumo/mq").Start(parent, "mq.publish."+row.EventType, trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(attribute.String("messaging.system", "rabbitmq"), attribute.String("messaging.destination.name", eventExchange), attribute.String("messaging.message.id", row.ID), attribute.String("code.layer", "mq")))
	defer span.End()
	TraceRequest(ctx, json.RawMessage(row.PayloadJSON))
	outcome := Result{"event_id": row.ID, "claimed": false, "broker_confirmed": false, "outbox_recorded": false}
	defer func() { TraceResponse(ctx, outcome, traceErr) }()
	// A lease prevents competing publisher workers from processing the same row.
	// A lost confirm is intentionally retried with the same event ID.
	claim, owned, err := claimOutbox(ctx, db, row.ID)
	outcome["claimed"] = owned
	if err != nil {
		return err
	}
	if !owned {
		return nil
	}
	outgoing := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, outgoing)
	headers := amqp.Table{}
	for k, v := range outgoing {
		headers[k] = v
	}
	sendCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	err = publishConfirmed(sendCtx, ch, eventExchange, row.EventType, []byte(row.PayloadJSON), headers, row.ID)
	if err != nil {
		span.SetStatus(codes.Error, "event publish failed")
		backoff := time.Second * time.Duration(1<<min(row.Attempts, 5))
		_ = markOutbox(ctx, db, row.ID, claim, map[string]any{"state": "PENDING", "next_attempt_at": time.Now().UTC().Add(backoff)})
		return err
	}
	outcome["broker_confirmed"] = true
	published := time.Now().UTC()
	err = markOutbox(ctx, db, row.ID, claim, map[string]any{"state": "PUBLISHED", "published_at": published})
	outcome["outbox_recorded"] = err == nil
	return err
}

func claimOutbox(ctx context.Context, db *gorm.DB, eventID string) (string, bool, error) {
	now := time.Now().UTC()
	token := ID("publish")
	claimed := db.WithContext(ctx).Model(&Outbox{}).Where("id = ? AND ((state = ? AND next_attempt_at <= ?) OR (state = ? AND lease_until < ?))", eventID, "PENDING", now, "PUBLISHING", now).Updates(map[string]any{"state": "PUBLISHING", "claim_token": token, "lease_until": now.Add(time.Minute), "attempts": gorm.Expr("attempts + 1")})
	return token, claimed.RowsAffected == 1, claimed.Error
}

func markOutbox(ctx context.Context, db *gorm.DB, eventID, token string, values map[string]any) error {
	changed := db.WithContext(ctx).Model(&Outbox{}).Where("id = ? AND state = ? AND claim_token = ?", eventID, "PUBLISHING", token).Updates(values)
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected != 1 {
		return fmt.Errorf("outbox claim expired")
	}
	return nil
}

func outboxLoop(ctx context.Context, db *gorm.DB, ch *confirmedPublisher) error {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		var rows []Outbox
		now := time.Now().UTC()
		if err := db.WithContext(ctx).Where("(state = ? AND next_attempt_at <= ?) OR (state = ? AND lease_until < ?)", "PENDING", now, "PUBLISHING", now).Order("created_at ASC").Limit(20).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if err := publishOne(ctx, db, ch, row); err != nil {
				Log(ctx, slog.LevelWarn, "outbox publish retry scheduled", "event_type", row.EventType)
				if ch.IsClosed() {
					return err
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func moduleWants(m Module, eventType string) bool {
	if m.OnEvent == nil {
		return false
	}
	for _, topic := range m.Topics {
		if topic == "#" || topic == "*" || topic == eventType {
			return true
		}
	}
	return false
}
func claimInbox(ctx context.Context, db *gorm.DB, consumer, eventID string) (Inbox, bool, error) {
	now := time.Now().UTC()
	record := Inbox{Consumer: consumer, EventID: eventID, State: "PROCESSING", Attempts: 1, ClaimToken: ID("claim"), LeaseUntil: now.Add(time.Minute)}
	created := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
	if created.Error != nil {
		return record, false, created.Error
	}
	if created.RowsAffected > 0 {
		return record, true, nil
	}
	var old Inbox
	if err := db.WithContext(ctx).Where("consumer = ? AND event_id = ?", consumer, eventID).First(&old).Error; err != nil {
		return old, false, err
	}
	if old.State == "DONE" || old.State == "DEAD" {
		return old, false, nil
	}
	if old.State == "PROCESSING" && old.LeaseUntil.After(now) {
		return old, false, nil
	}
	changed := db.WithContext(ctx).Model(&Inbox{}).Where("consumer = ? AND event_id = ? AND state = ? AND claim_token = ?", consumer, eventID, old.State, old.ClaimToken).Updates(map[string]any{"state": "PROCESSING", "claim_token": record.ClaimToken, "lease_until": record.LeaseUntil, "attempts": gorm.Expr("attempts + 1")})
	if changed.Error != nil {
		return old, false, changed.Error
	}
	if changed.RowsAffected == 0 {
		return old, false, nil
	}
	record.Attempts = old.Attempts + 1
	return record, true, nil
}
func markInbox(ctx context.Context, db *gorm.DB, row Inbox, state string) error {
	result := db.WithContext(ctx).Model(&Inbox{}).Where("consumer = ? AND event_id = ? AND claim_token = ?", row.Consumer, row.EventID, row.ClaimToken).Updates(map[string]any{"state": state, "lease_until": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("inbox claim expired")
	}
	return nil
}

func consumeOne(ctx context.Context, db *gorm.DB, retry *confirmedPublisher, m Module, d amqp.Delivery) (traceErr error) {
	var event Event
	decoder := json.NewDecoder(bytes.NewReader(d.Body))
	decoder.UseNumber()
	if err := decoder.Decode(&event); err != nil || event.ID == "" || event.Type == "" {
		return d.Reject(false)
	}
	if !moduleWants(m, event.Type) {
		return d.Ack(false)
	}
	carrier := propagation.MapCarrier{}
	for k, v := range d.Headers {
		if s, ok := v.(string); ok && (k == "traceparent" || k == "tracestate") {
			carrier[k] = s
		}
	}
	parent := otel.GetTextMapPropagator().Extract(ctx, carrier)
	ctx, span := otel.Tracer("lumo/mq").Start(parent, "mq.consume."+event.Type, trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attribute.String("messaging.system", "rabbitmq"), attribute.String("messaging.destination.name", queueName(m.Name)), attribute.String("messaging.message.id", event.ID), attribute.String("code.layer", "mq")))
	defer span.End()
	TraceRequest(ctx, event)
	outcome := Result{"event_id": event.ID, "handled": false}
	defer func() { TraceResponse(ctx, outcome, traceErr) }()
	claim, owned, err := claimInbox(ctx, db, m.Name, event.ID)
	span.SetAttributes(attribute.Int("messaging.message.retry_count", max(claim.Attempts-1, 0)), attribute.String("lumo.aggregate_id", event.AggregateID))
	if err != nil {
		return err
	}
	if !owned {
		if claim.State == "DONE" {
			outcome["disposition"] = "duplicate_ack"
			return d.Ack(false)
		}
		if claim.State == "DEAD" {
			outcome["disposition"] = "dead_letter"
			return d.Reject(false)
		}
		outcome["disposition"] = "retry"
		return retryDelivery(ctx, retry, m.Name, d)
	}
	handlerCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = m.OnEvent(handlerCtx, event)
	outcome["handled"] = err == nil
	if err == nil {
		if err = markInbox(ctx, db, claim, "DONE"); err != nil {
			return err
		}
		Log(ctx, slog.LevelInfo, "event consumed", "event_type", event.Type, "event_id", event.ID)
		outcome["disposition"] = "ack"
		return d.Ack(false)
	}
	span.SetStatus(codes.Error, "event handler failed")
	Log(ctx, slog.LevelWarn, "event handler retry", "event_type", event.Type, "attempt", claim.Attempts)
	if claim.Attempts >= 5 {
		if err = markInbox(ctx, db, claim, "DEAD"); err != nil {
			return err
		}
		outcome["disposition"] = "dead_letter"
		return d.Reject(false)
	}
	if err = markInbox(ctx, db, claim, "RETRY"); err != nil {
		return err
	}
	outcome["disposition"] = "retry"
	return retryDelivery(ctx, retry, m.Name, d)
}
func retryDelivery(ctx context.Context, ch *confirmedPublisher, service string, d amqp.Delivery) error {
	retryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := publishConfirmed(retryCtx, ch, "", "lumo."+service+".retry", d.Body, d.Headers, d.MessageId); err != nil {
		return err
	}
	return d.Ack(false)
}

func brokerSession(ctx context.Context, db *gorm.DB, url string, m Module) error {
	conn, err := amqp.DialConfig(url, amqp.Config{Heartbeat: 10 * time.Second, Dial: amqp.DefaultDial(10 * time.Second)})
	if err != nil {
		return err
	}
	defer conn.Close()
	admin, err := conn.Channel()
	if err != nil {
		return err
	}
	if err = declareTopology(admin, m.Name); err != nil {
		admin.Close()
		return err
	}
	admin.Close()
	producerChannel, err := conn.Channel()
	if err != nil {
		return err
	}
	defer producerChannel.Close()
	producer, err := newConfirmedPublisher(producerChannel)
	if err != nil {
		return err
	}
	retryChannel, err := conn.Channel()
	if err != nil {
		return err
	}
	defer retryChannel.Close()
	retry, err := newConfirmedPublisher(retryChannel)
	if err != nil {
		return err
	}
	consumer, err := conn.Channel()
	if err != nil {
		return err
	}
	defer consumer.Close()
	if err = consumer.Qos(1, 0, false); err != nil {
		return err
	}
	deliveries, err := consumer.Consume(queueName(m.Name), "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	publisherErrors := make(chan error, 1)
	go func() { publisherErrors <- outboxLoop(sessionCtx, db, producer) }()
	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-publisherErrors:
			return err
		case err := <-closed:
			if err == nil {
				return fmt.Errorf("broker connection closed")
			}
			return err
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("broker delivery stream closed")
			}
			if err = consumeOne(sessionCtx, db, retry, m, delivery); err != nil {
				_ = delivery.Nack(false, true)
				return err
			}
		}
	}
}

// RunEvents reconnects after transient broker failures. Outbox rows stay durable.
// Inbox completion is committed after OnEvent succeeds, so a crash between a
// business commit and Inbox completion may replay the event: domain handlers
// MUST use the event/business ID as their own idempotency key.
func RunEvents(ctx context.Context, db *gorm.DB, _ *redis.Client, url string, m Module) error {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if m.Tick != nil {
		go func() {
			timer := time.NewTicker(time.Second)
			defer timer.Stop()
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-timer.C:
					tickCtx, done := context.WithTimeout(workerCtx, 30*time.Second)
					if err := runServiceTick(tickCtx, db, m.Name, m.Tick); err != nil {
						Log(tickCtx, slog.LevelWarn, "scheduled task will retry", "service", m.Name)
					}
					done()
				}
			}
		}()
	}
	if strings.TrimSpace(url) == "" {
		<-ctx.Done()
		return nil
	}
	for {
		err := brokerSession(ctx, db, url, m)
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return nil
		}
		Log(ctx, slog.LevelWarn, "broker disconnected; reconnecting", "service", m.Name)
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
