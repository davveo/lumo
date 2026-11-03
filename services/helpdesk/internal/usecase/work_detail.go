package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) WorkDetail(ctx context.Context, c platform.Request) (platform.Result, error) {
	actor, e := s.workActor(ctx, c)
	if e != nil {
		return nil, e
	}
	service := platform.Text(c.Data, "service")
	if !actor.allows(service) || c.ID == "" || len(c.ID) > 128 {
		return nil, status.Error(codes.PermissionDenied, "work item is outside this role's queue")
	}
	clients := platform.NewClients(s.rpc)
	request := platform.Request{ID: c.ID, UserID: actor.ID, Role: actor.Role}
	var subject platform.Result
	switch service {
	case constants.ServiceFulfillment:
		subject, e = clients.Fulfillment.Get(ctx, request)
	case constants.ServiceAftersale:
		subject, e = clients.Aftersale.Get(ctx, request)
	case constants.ServicePayment:
		subject, e = clients.Payment.Get(ctx, request)
	case constants.ServiceRefund:
		subject, e = clients.Refund.Get(ctx, request)
	case constants.ServiceHelpdesk:
		request.Role = constants.RoleAdmin
		subject, e = s.Get(ctx, request)
	}
	if e != nil {
		return nil, e
	}
	key := service + ":" + c.ID
	assignments, e := s.repo.Assignments(ctx, []string{key})
	if e != nil {
		return nil, e
	}
	history, e := s.repo.WorkChanges(ctx, key)
	if e != nil {
		return nil, e
	}
	out := platform.Result{"subject": subject, "assignment": assignments[key], "assignment_history": history}
	failures := map[string]string{}
	orderID := platform.Text(subject, "order_id")
	if orderID == "" {
		out["errors"] = failures
		return out, nil
	}
	if service == constants.ServiceFulfillment && platform.Text(subject, "kind") == constants.FulfillmentKindGift {
		var address map[string]any
		_ = json.Unmarshal([]byte(platform.Text(subject, "address_json")), &address)
		out["order"] = platform.Result{
			"id":      orderID,
			"kind":    constants.FulfillmentKindGift,
			"address": address,
			"status":  subject["status"],
		}
		out["fulfillment"] = subject
		out["errors"] = failures
		return out, nil
	}
	order, err := clients.Order.Get(
		ctx,
		platform.Request{ID: orderID, UserID: platform.Text(subject, "user_id"), Role: constants.RoleSystem},
	)
	if err != nil {
		failures[constants.ServiceOrder] = status.Code(err).String()
	} else if platform.StaffAllows(actor.Role, constants.ServiceOrder, constants.ActionGet) {
		out["order"] = order
	} else {
		var rows struct {
			Items []struct {
				SKU      string `json:"sku_id"`
				Name     string `json:"name"`
				Quantity int64  `json:"quantity"`
			} `json:"items"`
		}
		_ = platform.Decode(order, &rows)
		out["order"] = platform.Result{
			"id":               orderID,
			"status":           order["status"],
			"address":          order["address"],
			"items":            rows.Items,
			"dispatch_started": order["dispatch_started"],
		}
	}
	related := platform.Request{
		ID:     orderID,
		UserID: actor.ID,
		Role:   actor.Role,
		Data:   map[string]any{"order_id": orderID, "limit": 100},
	}
	if service == constants.ServicePayment {
		out["payment"] = subject
	} else if id := platform.Text(order, "payment_id"); id != "" && platform.StaffAllows(actor.Role, constants.ServicePayment, constants.ActionGet) {
		value, err := clients.Payment.Get(ctx, platform.Request{ID: id, UserID: actor.ID, Role: actor.Role})
		if err != nil {
			failures[constants.ServicePayment] = status.Code(err).String()
		} else {
			out["payment"] = value
		}
	}
	if service == constants.ServiceFulfillment {
		out["fulfillment"] = subject
	} else if platform.StaffAllows(actor.Role, constants.ServiceFulfillment, constants.ActionGet) {
		value, err := clients.Fulfillment.Get(ctx, related)
		if err != nil && status.Code(err) != codes.NotFound {
			failures[constants.ServiceFulfillment] = status.Code(err).String()
		} else {
			out["fulfillment"] = value
		}
	}
	if platform.StaffAllows(actor.Role, constants.ServiceLogistics, constants.ActionTracking) {
		value, err := clients.Logistics.Tracking(ctx, related)
		if err != nil && status.Code(err) != codes.NotFound {
			failures[constants.ServiceLogistics] = status.Code(err).String()
		} else {
			out["logistics"] = value
		}
	}
	if platform.StaffAllows(actor.Role, constants.ServiceAftersale, constants.ActionList) {
		value, err := clients.Aftersale.List(ctx, related)
		if err != nil {
			failures[constants.ServiceAftersale] = status.Code(err).String()
		} else {
			out["aftersales"] = value
		}
	}
	if platform.StaffAllows(actor.Role, constants.ServiceHelpdesk, constants.ActionList) {
		local := related
		local.Role = constants.RoleAdmin
		value, err := s.List(ctx, local)
		if err != nil {
			failures[constants.ServiceHelpdesk] = status.Code(err).String()
		} else {
			out["tickets"] = value
		}
	}
	out["errors"] = failures
	return out, nil
}
