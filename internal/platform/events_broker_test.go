package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRabbitMQOutboxInboxAndPreciseRouting(t *testing.T) {
	if os.Getenv("LUMO_AMQP_TEST") != "1" {
		t.Skip("requires explicitly enabled disposable RabbitMQ vhost")
	}
	u, err := url.Parse(os.Getenv("LUMO_TEST_AMQP_URL"))
	if err != nil || u.User == nil {
		t.Fatal("test AMQP credentials required")
	}
	management := os.Getenv("LUMO_AMQP_MANAGEMENT_URL")
	if management == "" {
		t.Fatal("test RabbitMQ management URL required")
	}
	password, _ := u.User.Password()
	client := &http.Client{Timeout: 5 * time.Second}
	vhost := "lumo-ci-" + ID("broker")
	manage := func(method, path string, body []byte) error {
		r, e := http.NewRequest(method, management+path, bytes.NewReader(body))
		if e != nil {
			return e
		}
		r.SetBasicAuth(u.User.Username(), password)
		r.Header.Set("Content-Type", "application/json")
		response, e := client.Do(r)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("test broker management returned %d", response.StatusCode)
		}
		return nil
	}
	path := "/api/vhosts/" + url.PathEscape(vhost)
	if err = manage("PUT", path, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := manage("DELETE", path, nil); e != nil {
			t.Error(e)
		}
	})
	if err = manage("PUT", "/api/permissions/"+url.PathEscape(vhost)+"/"+url.PathEscape(u.User.Username()), []byte(`{"configure":".*","write":".*","read":".*"}`)); err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + vhost
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "broker.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	defer raw.Close()
	if err = MigratePlatform(ctx, db); err != nil {
		t.Fatal(err)
	}
	var handled atomic.Int64
	m := Module{
		Name:    "broker-probe",
		Topics:  []string{"ProbeEvent"},
		OnEvent: func(context.Context, Event) error { handled.Add(1); return nil },
	}
	done := make(chan error, 1)
	go func() { done <- RunEvents(ctx, db, nil, u.String(), m) }()
	defer func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(5 * time.Second):
			t.Error("broker worker did not stop")
		}
	}()
	event := Event{ID: ID("probe"), Type: "ProbeEvent"}
	if err = Enqueue(ctx, db, event); err != nil {
		t.Fatal(err)
	}
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for !check() {
			if time.Now().After(deadline) {
				t.Fatal("broker condition timed out")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	wait(func() bool {
		var out Outbox
		var in Inbox
		db.First(&out, "id = ?", event.ID)
		db.First(&in, "consumer = ? AND event_id = ?", m.Name, event.ID)
		return out.State == constants.OutboxStatePublished && in.State == constants.InboxStateDone
	})
	conn, err := amqp.Dial(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	if err = ch.QueueBind(queueName("catalog"), "#", constants.MessageExchangeEvents, false, nil); err != nil {
		t.Fatal(err)
	}
	if err = declareTopology(ch, m.Name, m.Topics); err != nil {
		t.Fatal(err)
	}
	publisher, err := newConfirmedPublisher(ch)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(event Event, mandatory bool) error {
		body, _ := json.Marshal(event)
		return publishConfirmed(
			ctx,
			publisher,
			constants.MessageExchangeEvents,
			event.Type,
			body,
			nil,
			event.ID,
			mandatory,
		)
	}
	if err = publish(event, true); err != nil {
		t.Fatal(err)
	}
	barrier := Event{ID: ID("barrier"), Type: "ProbeEvent"}
	if err = publish(barrier, true); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		var in Inbox
		db.First(&in, "consumer = ? AND event_id = ?", m.Name, barrier.ID)
		return in.State == constants.InboxStateDone
	})
	if handled.Load() != 2 {
		t.Fatal("duplicate delivery repeated the business handler", handled.Load())
	}
	if err = publish(Event{ID: ID("routing"), Type: constants.EventPaymentSucceeded}, true); err != nil {
		t.Fatal(err)
	}
	if _, ok, e := ch.Get(queueName("order"), true); e != nil || !ok {
		t.Fatal("declared subscriber missed event", e)
	}
	if _, ok, e := ch.Get(queueName("catalog"), true); e != nil || ok {
		t.Fatal("legacy catch-all still receives unrelated events", e)
	}
	if err = publish(Event{ID: ID("unused"), Type: constants.EventProductChanged}, eventRequiresRoute(constants.EventProductChanged)); err != nil {
		t.Fatal(err)
	}
	if err = publish(Event{ID: ID("typo"), Type: "UnknownUnroutableEvent"}, true); err == nil {
		t.Fatal("unknown unroutable event was silently accepted")
	}
}
