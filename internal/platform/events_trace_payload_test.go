package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type traceDeliveryAcknowledgement struct{ acknowledgements int }

func (a *traceDeliveryAcknowledgement) Ack(uint64, bool) error      { a.acknowledgements++; return nil }
func (*traceDeliveryAcknowledgement) Nack(uint64, bool, bool) error { return nil }
func (*traceDeliveryAcknowledgement) Reject(uint64, bool) error     { return nil }

func TestMQTraceRecordsEventArgumentsAndDeliveryOutcomes(t *testing.T) {
	db := testDB(t)
	exporter, _ := tracing(t)
	ctx := context.Background()
	event := Event{
		ID:          "trace-event",
		Type:        "TracePayloadTest",
		AggregateID: "trace-aggregate",
		UserID:      "buyer",
		Data:        map[string]any{"sku_id": "sku-phone", "password": "mq-trace-private-value"},
	}
	if err := Enqueue(ctx, db, event); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Outbox{}).Where("id = ?", event.ID).Update("state", "PUBLISHED").Error; err != nil {
		t.Fatal(err)
	}
	var row Outbox
	if err := db.First(&row, "id = ?", event.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := publishOne(ctx, db, nil, row); err != nil {
		t.Fatalf("completed outbox entry should not be republished: %v", err)
	}
	calls := 0
	module := Module{
		Name:    "trace-consumer",
		Topics:  []string{event.Type},
		OnEvent: func(context.Context, Event) error { calls++; return nil },
	}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	ack := &traceDeliveryAcknowledgement{}
	delivery := amqp.Delivery{Body: body, MessageId: event.ID, DeliveryTag: 1, Acknowledger: ack}
	for range 2 {
		if err := consumeOne(ctx, db, nil, module, delivery); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || ack.acknowledgements != 2 {
		t.Fatalf("trace recording changed delivery deduplication: handled=%d ack=%d", calls, ack.acknowledgements)
	}
	seen := map[string]bool{}
	for _, span := range exporter.GetSpans() {
		if span.Name != "mq.publish."+event.Type && span.Name != "mq.consume."+event.Type {
			continue
		}
		attributes := rpcSpanAttributes(span)
		if !json.Valid([]byte(attributes["lumo.request"])) || !json.Valid([]byte(attributes["lumo.response"])) ||
			!strings.Contains(attributes["lumo.request"], "sku-phone") {
			t.Fatalf("MQ request/response missing from %s: %v", span.Name, attributes)
		}
		for _, value := range attributes {
			if strings.Contains(value, "mq-trace-private-value") {
				t.Fatalf("MQ payload secret leaked from %s", span.Name)
			}
		}
		response := attributes["lumo.response"]
		for _, outcome := range []string{`"claimed":false`, `"disposition":"ack"`, `"disposition":"duplicate_ack"`} {
			if strings.Contains(response, outcome) {
				seen[outcome] = true
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("MQ delivery outcomes missing: %v", seen)
	}
}
