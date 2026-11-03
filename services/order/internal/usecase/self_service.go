package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

func appendServiceEvent(o *domain.Order, kind, region string) {
	var events []domain.ServiceEvent
	_ = json.Unmarshal([]byte(o.ServiceHistory), &events)
	events = append(events, domain.ServiceEvent{Kind: kind, Region: region, OccurredAt: time.Now().UTC()})
	if len(events) > 50 {
		events = events[len(events)-50:]
	}
	raw, _ := json.Marshal(events)
	o.ServiceHistory = string(raw)
}
func (s *Service) ChangeAddress(ctx context.Context, in ChangeAddressRequest) (domain.Order, error) {
	var address map[string]any
	if len(in.Address) > 16000 || json.Unmarshal(in.Address, &address) != nil || commercepolicy.Region(address) == "" ||
		in.ExpectedVersion < 1 ||
		in.ShippingCents < 0 {
		return domain.Order{}, status.Error(codes.InvalidArgument, "valid address, shipping and order version required")
	}
	return s.update(
		ctx,
		constants.ActionChangeAddress,
		in.OrderRequest,
		internalAccess,
		func(_ *repository.Tx, o *domain.Order) (transition, error) {
			if platform.Text(address, "user_id") != o.UserID {
				return transition{}, status.Error(codes.PermissionDenied, "address owner mismatch")
			}
			if o.Address == string(in.Address) {
				return transition{unchanged: true}, nil
			}
			if o.DispatchStarted ||
				(o.Status != constants.OrderStatusPendingPayment && o.Status != constants.OrderStatusReadyToFulfill) {
				return transition{}, status.Error(codes.FailedPrecondition, "订单已进入仓库处理或当前状态不支持修改地址")
			}
			if o.Version != in.ExpectedVersion {
				return transition{}, status.Error(codes.Aborted, "订单已变化，请刷新后重试")
			}
			var quote map[string]any
			if json.Unmarshal([]byte(o.Quote), &quote) != nil {
				return transition{}, status.Error(codes.Internal, "invalid order quote")
			}
			if platform.Int(quote, "shipping_cents") != in.ShippingCents {
				return transition{}, status.Error(codes.FailedPrecondition, "新地址运费与原订单不同，请取消后重新下单或联系客服")
			}
			o.Address = string(in.Address)
			appendServiceEvent(o, constants.OrderServiceKindAddressChanged, commercepolicy.Region(address))
			return transition{}, nil
		},
	)
}
func (s *Service) BeginDispatch(ctx context.Context, in OrderRequest) (domain.Order, error) {
	return s.update(
		ctx,
		constants.ActionBeginDispatch,
		in,
		internalAccess,
		func(_ *repository.Tx, o *domain.Order) (transition, error) {
			if (o.Status != constants.OrderStatusReadyToFulfill && o.Status != constants.OrderStatusPartiallyShipped) ||
				!o.ResourcesCommitted {
				return transition{}, status.Error(codes.FailedPrecondition, "order is not ready for warehouse dispatch")
			}
			if o.DispatchStarted {
				return transition{unchanged: true}, nil
			}
			o.DispatchStarted = true
			appendServiceEvent(o, constants.OrderServiceKindWarehouseProcessing, "")
			return transition{}, nil
		},
	)
}
func (s *Service) Urge(ctx context.Context, in OrderRequest) (domain.Order, error) {
	return s.update(
		ctx,
		constants.ActionUrge,
		in,
		memberAccess,
		func(_ *repository.Tx, o *domain.Order) (transition, error) {
			if o.Status != constants.OrderStatusReadyToFulfill {
				return transition{}, status.Error(codes.FailedPrecondition, "当前订单不支持催发货")
			}
			now := time.Now().UTC()
			if o.LastUrgedAt != nil && now.Sub(*o.LastUrgedAt) < 24*time.Hour {
				return transition{}, status.Error(codes.ResourceExhausted, "已收到催发货请求，每 24 小时可催一次")
			}
			o.LastUrgedAt = &now
			appendServiceEvent(o, constants.OrderServiceKindShipmentUrged, "")
			return transition{
				eventType: constants.EventOrderUrged,
				eventID:   fmt.Sprintf("urge-%s-%d", o.ID, o.Version+1),
				data:      map[string]any{"order_id": o.ID},
			}, nil
		},
	)
}
