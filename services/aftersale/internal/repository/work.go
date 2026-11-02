package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
)

func (r *Repository) QueueWork(ctx context.Context, events []platform.Event) error {
	return platform.EnqueueBatch(ctx, r.DB, events)
}
