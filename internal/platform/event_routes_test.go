package platform

import (
	"github.com/davveo/lumo/internal/constants"
	amqp "github.com/rabbitmq/amqp091-go"
	"testing"
)

type topologyRecorder struct{ bindings map[string]map[string]bool }

func (*topologyRecorder) ExchangeDeclare(string, string, bool, bool, bool, bool, amqp.Table) error {
	return nil
}
func (r *topologyRecorder) QueueDeclare(name string, _ bool, _ bool, _ bool, _ bool, _ amqp.Table) (amqp.Queue, error) {
	if r.bindings[name] == nil {
		r.bindings[name] = map[string]bool{"#": true}
	}
	return amqp.Queue{Name: name}, nil
}
func (r *topologyRecorder) QueueBind(name, key, exchange string, _ bool, _ amqp.Table) error {
	if exchange == constants.MessageExchangeEvents {
		r.bindings[name][key] = true
	}
	return nil
}
func (r *topologyRecorder) QueueUnbind(name, key, _ string, _ amqp.Table) error {
	delete(r.bindings[name], key)
	return nil
}

func TestTopologyDeclaresAllSubscribersAndMigratesLegacyBindings(t *testing.T) {
	r := &topologyRecorder{bindings: map[string]map[string]bool{}}
	if e := declareTopology(r, "catalog"); e != nil {
		t.Fatal(e)
	}
	for _, name := range services {
		bindings := r.bindings[queueName(name)]
		if bindings["#"] {
			t.Fatal("legacy broadcast still bound", name)
		}
		if len(bindings) != len(EventTopics(name)) {
			t.Fatal("unexpected queue bindings", name, bindings)
		}
		for _, topic := range EventTopics(name) {
			if !bindings[topic] {
				t.Fatal("subscriber not bound before startup", name, topic)
			}
		}
	}
	if eventRequiresRoute(constants.EventProductChanged) || !eventRequiresRoute(constants.EventPaymentSucceeded) ||
		!eventRequiresRoute("MisspelledEvent") {
		t.Fatal("mandatory routing policy is wrong")
	}
}
