package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type validationAcknowledgement struct {
	acked, rejected, nacked int
	requeue                 bool
}

func (a *validationAcknowledgement) Ack(uint64, bool) error { a.acked++; return nil }
func (a *validationAcknowledgement) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked++
	a.requeue = requeue
	return nil
}
func (a *validationAcknowledgement) Reject(_ uint64, requeue bool) error {
	a.rejected++
	a.requeue = requeue
	return nil
}

func TestInvalidEventDeliveriesDeadLetterWithoutClaimOrBusinessCall(t *testing.T) {
	valid := Event{ID: "event-1", Type: "ValidationEvent", UserID: "buyer", Data: map[string]any{"quantity": 2}}
	encode := func(event Event) string { b, _ := json.Marshal(event); return string(b) }
	cases := map[string]string{
		"trailing object":  encode(valid) + `{}`,
		"trailing null":    encode(valid) + `null`,
		"trailing garbage": encode(valid) + `not-json`,
		"blank id":         encode(Event{ID: " \t", Type: valid.Type}),
		"blank type":       encode(Event{ID: valid.ID, Type: " \t"}),
		"overlong id":      encode(Event{ID: strings.Repeat("x", 81), Type: valid.Type}),
		"overlong type":    encode(Event{ID: valid.ID, Type: strings.Repeat("x", 101)}),
		"AMQP id bytes":    encode(Event{ID: strings.Repeat("😀", 80), Type: valid.Type}),
		"AMQP type bytes":  encode(Event{ID: valid.ID, Type: strings.Repeat("类", 86)}),
		"malformed":        `{"id":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			db := testDB(t)
			calls := 0
			module := Module{Name: "validator", Topics: []string{"#"}, OnEvent: func(context.Context, Event) error {
				calls++
				return nil
			}}
			ack := &validationAcknowledgement{}
			if err := consumeOne(context.Background(), db, nil, module, amqp.Delivery{Body: []byte(body), Acknowledger: ack}); err != nil {
				t.Fatal(err)
			}
			var claims int64
			if err := db.Model(&Inbox{}).Count(&claims).Error; err != nil {
				t.Fatal(err)
			}
			if calls != 0 || claims != 0 || ack.rejected != 1 || ack.acked != 0 || ack.nacked != 0 || ack.requeue {
				t.Fatalf("invalid delivery reached business/requeue: calls=%d claims=%d ack=%+v", calls, claims, ack)
			}
		})
	}
}

func TestEventMetadataStorageBoundariesAndJSONWhitespaceRemainValid(t *testing.T) {
	db := testDB(t)
	event := Event{ID: strings.Repeat("界", 80), Type: strings.Repeat("V", 100), Data: map[string]any{"quantity": 2}}
	if err := Enqueue(context.Background(), db, event); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []Event{
		{ID: strings.Repeat("界", 81), Type: "Valid"},
		{ID: "valid-id", Type: strings.Repeat("类", 101)},
		{ID: "valid-id", Type: " \t"},
		{ID: strings.Repeat("😀", 80), Type: "Valid"},
		{ID: "valid-id", Type: strings.Repeat("类", 86)},
	} {
		if err := Enqueue(context.Background(), db, invalid); err == nil {
			t.Fatalf(
				"invalid metadata accepted by producer: id chars=%d type chars=%d",
				len([]rune(invalid.ID)),
				len([]rune(invalid.Type)),
			)
		}
	}
	if err := validateEventMetadata(Event{ID: "utf8-boundary", Type: strings.Repeat("类", 85)}); err != nil {
		t.Fatalf("valid 255-byte AMQP field rejected: %v", err)
	}
	body, _ := json.Marshal(event)
	body = append(body, []byte(" \n\t")...)
	ack := &validationAcknowledgement{}
	calls := 0
	m := Module{Name: "validator", Topics: []string{"#"}, OnEvent: func(_ context.Context, got Event) error {
		calls++
		if Int(got.Data, "quantity") != 2 {
			t.Fatal("numeric event payload changed")
		}
		return nil
	}}
	if err := consumeOne(context.Background(), db, nil, m, amqp.Delivery{Body: body, Acknowledger: ack}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || ack.acked != 1 || ack.rejected != 0 {
		t.Fatalf("valid boundary event was rejected: calls=%d ack=%+v", calls, ack)
	}
	var outbox int64
	if err := db.Model(&Outbox{}).Count(&outbox).Error; err != nil || outbox != 1 {
		t.Fatalf("invalid producer metadata persisted: count=%d err=%v", outbox, err)
	}
}
