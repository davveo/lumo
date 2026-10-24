package usecase

import (
	"encoding/json"
	"time"
)

type Actor struct {
	UserID string
	Role   string
}
type OrderRequest struct {
	Actor Actor
	ID    string
}
type CreateOrderRequest struct {
	OrderRequest
	Quote     json.RawMessage
	Address   json.RawMessage
	Note      string
	Source    string
	ExpiresAt time.Time
}
type AcceptPaymentRequest struct {
	OrderRequest
	PaymentID   string
	AmountCents int64
	PaidAt      time.Time
}
type AcceptPaymentResult struct {
	Accepted  bool
	OrderID   string
	Status    string
	PaymentID string
}
type RecordRefundRequest struct {
	OrderRequest
	RefundID    string
	AmountCents int64
}
