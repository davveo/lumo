package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/invoice/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) ApplyRefund(ctx context.Context, orderID, refundID, userID string, amount, total int64) error {
	ctx, span := platform.Start(ctx, "repository.invoice.ApplyRefund")
	defer span.End()
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row domain.Record
		err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&row, "order_id = ?", orderID).
			Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if userID != "" && row.UserID != userID {
			return status.Error(codes.PermissionDenied, "invoice refund event owner mismatch")
		}
		var old domain.Reversal
		err = tx.WithContext(ctx).First(&old, "refund_id = ?", refundID).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if total > 0 {
			amount = total - row.RefundedCents
		}
		remaining := row.AmountCents - row.RefundedCents
		if amount > remaining {
			amount = remaining
		}
		if amount < 0 {
			amount = 0
		}
		if amount == 0 {
			return nil
		}
		row.RefundedCents += amount
		adjustment := domain.Reversal{
			ID:             row.ID + ":" + refundID,
			InvoiceID:      row.ID,
			RefundID:       refundID,
			OriginalNumber: row.Number,
			AmountCents:    amount,
			Status:         constants.InvoiceReversalStatusCancelledUnissued,
		}
		if row.Number != "" {
			row.Status = constants.InvoiceStatusReversalPending
			adjustment.Status = constants.InvoiceReversalStatusPendingExternalAdjustment
		} else if row.RefundedCents >= row.AmountCents {
			row.Status = constants.InvoiceStatusCancelled
		} else {
			row.Status = constants.InvoiceStatusAdjustedRequest
		}
		row.Version++
		if err = tx.WithContext(ctx).Save(&row).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Create(&adjustment).Error
	})
}

func (r *Repository) IssueDemo(ctx context.Context, id, number string) (*domain.Record, error) {
	ctx, span := platform.Start(ctx, "repository.invoice.IssueDemo")
	defer span.End()
	var row domain.Record
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(
			&row,
			"id = ?",
			id,
		).Error; err != nil {
			return err
		}
		if row.Status == constants.InvoiceStatusIssued {
			return nil
		}
		if row.Status != constants.InvoiceStatusRequested && row.Status != constants.InvoiceStatusAdjustedRequest {
			return status.Error(codes.FailedPrecondition, "invoice no longer eligible for issue")
		}
		net := row.AmountCents - row.RefundedCents
		if net <= 0 {
			return status.Error(codes.FailedPrecondition, "no invoiceable amount")
		}
		row.Status = constants.InvoiceStatusIssued
		row.Number = number
		row.IssuedCents = net
		row.Provider = constants.InvoiceProviderLocalDemoOnly
		row.Version++
		if err := tx.WithContext(ctx).Save(&row).Error; err != nil {
			return err
		}
		event := platform.Event{
			ID:          row.ID + ":issued",
			Type:        constants.EventInvoiceIssued,
			AggregateID: row.ID,
			UserID:      row.UserID,
			Data: map[string]any{
				"order_id":     row.OrderID,
				"invoice_id":   row.ID,
				"number":       number,
				"amount_cents": net,
				"provider":     constants.InvoiceProviderLocalDemoOnly,
			},
		}
		return platform.Enqueue(ctx, tx, event)
	})
	return &row, err
}
func (r *Repository) Reversals(ctx context.Context, id string) ([]domain.Reversal, error) {
	ctx, span := platform.Start(ctx, "repository.invoice.Reversals")
	defer span.End()
	rows := []domain.Reversal{}
	err := r.DB.WithContext(ctx).Where("invoice_id = ?", id).Order("created_at asc").Find(&rows).Error
	return rows, err
}
func (r *Repository) CompleteDemoAdjustments(ctx context.Context, id string) (*domain.Record, error) {
	ctx, span := platform.Start(ctx, "repository.invoice.CompleteDemoAdjustments")
	defer span.End()
	var row domain.Record
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(
			&row,
			"id = ?",
			id,
		).Error; err != nil {
			return err
		}
		if row.Status == constants.InvoiceStatusAdjusted {
			return nil
		}
		if row.Status != constants.InvoiceStatusReversalPending {
			return status.Error(codes.FailedPrecondition, "no pending invoice adjustment")
		}
		if err := tx.WithContext(ctx).Model(&domain.Reversal{}).Where(
			"invoice_id = ? AND status = ?",
			id,
			constants.InvoiceReversalStatusPendingExternalAdjustment,
		).Update(
			"status",
			constants.InvoiceReversalStatusDemoAdjusted,
		).Error; err != nil {
			return err
		}
		row.Status = constants.InvoiceStatusAdjusted
		row.Version++
		return tx.WithContext(ctx).Save(&row).Error
	})
	return &row, err
}
