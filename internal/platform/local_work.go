package platform

import (
	"context"
	"crypto/sha256"
	"fmt"
	"gorm.io/gorm"
	"time"
)

// Recovery work is a local durable outbox write. Consumers execute one item;
// neither the scan nor its transaction can wait on a remote domain.
func RecoveryEvent(kind, id, user, state string, version int64) Event {
	key := fmt.Sprintf("%s:%s:%s:%d:%d", kind, id, state, version, time.Now().UTC().Unix()/30)
	return Event{
		ID:          fmt.Sprintf("work-%x", sha256.Sum256([]byte(key))),
		Type:        kind,
		AggregateID: id,
		UserID:      user,
		Data:        map[string]any{"expected_status": state, "expected_version": version},
	}
}
func EnqueueBatch(ctx context.Context, db *gorm.DB, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, event := range events {
			if err := Enqueue(ctx, tx, event); err != nil {
				return err
			}
		}
		return nil
	})
}
