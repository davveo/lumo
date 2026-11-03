package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workActor struct{ ID, Role, Name string }

func (s *Service) workActor(ctx context.Context, c platform.Request) (workActor, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return workActor{}, e
	}
	p, e := platform.NewClients(
		s.rpc,
	).IAM.AdminGet(
		ctx,
		platform.Request{ID: c.UserID, UserID: c.UserID, Role: constants.RoleAdmin},
	)
	if e != nil {
		return workActor{}, e
	}
	role := platform.Text(p, "role")
	if p["active"] == false || !platform.StaffAllows(role, constants.ServiceHelpdesk, constants.ActionWorkQueue) {
		return workActor{}, status.Error(codes.PermissionDenied, "当前账号没有待办工作台权限")
	}
	return workActor{c.UserID, role, platform.Text(p, "username")}, nil
}

var workServices = []string{
	constants.ServiceFulfillment,
	constants.ServiceAftersale,
	constants.ServicePayment,
	constants.ServiceRefund,
	constants.ServiceHelpdesk,
}

func workService(service string) bool {
	switch service {
	case constants.ServiceFulfillment,
		constants.ServiceAftersale,
		constants.ServicePayment,
		constants.ServiceRefund,
		constants.ServiceHelpdesk:
		return true
	}
	return false
}
func (a workActor) allows(service string) bool {
	return workService(service) && platform.StaffAllows(a.Role, service, constants.ActionAdminList)
}

type workSources struct {
	Values map[string]platform.Result
	Errors map[string]string
}

// Fixed fan-out: request building and result comparison are pure local work.
func (s *Service) workSources(
	ctx context.Context,
	a workActor,
	filter, keyword string,
	window int64,
	ids map[string][]string,
) (workSources, error) {
	clients := platform.NewClients(s.rpc)
	out := workSources{Values: map[string]platform.Result{}, Errors: map[string]string{}}
	allowed := func(service string) bool {
		return a.allows(service) && (filter == "" || filter == service) && (ids == nil || len(ids[service]) > 0)
	}
	request := func(service string, states []string) platform.Request {
		data := map[string]any{
			"page":         window,
			"page_size":    100,
			"oldest_first": true,
			"keyword":      keyword,
			"states":       states,
		}
		if ids != nil {
			data["ids"] = ids[service]
			data["page"] = 1
		}
		return platform.Request{UserID: a.ID, Role: a.Role, Data: data}
	}
	save := func(service string, value platform.Result, e error) {
		if e != nil {
			out.Errors[service] = status.Code(e).String()
		} else {
			out.Values[service] = value
		}
	}
	if allowed(constants.ServiceFulfillment) {
		v, e := clients.Fulfillment.AdminList(
			ctx,
			request(
				constants.ServiceFulfillment,
				[]string{constants.FulfillmentStatusReady, constants.FulfillmentStatusDispatching},
			),
		)
		save(constants.ServiceFulfillment, v, e)
	}
	if allowed(constants.ServiceAftersale) {
		v, e := clients.Aftersale.AdminList(
			ctx,
			request(
				constants.ServiceAftersale,
				[]string{
					constants.AftersaleStatusRequested,
					constants.AftersaleStatusApproved,
					constants.AftersaleStatusInspected,
					constants.AftersaleStatusRefundRequested,
					constants.AftersaleStatusRefunding,
				},
			),
		)
		save(constants.ServiceAftersale, v, e)
	}
	if allowed(constants.ServicePayment) {
		v, e := clients.Payment.AdminList(
			ctx,
			request(constants.ServicePayment, []string{constants.PaymentStatusPending}),
		)
		save(constants.ServicePayment, v, e)
	}
	if allowed(constants.ServiceRefund) {
		v, e := clients.Refund.AdminList(
			ctx,
			request(constants.ServiceRefund, []string{constants.RefundStatusPending, constants.RefundStatusProcessing}),
		)
		save(constants.ServiceRefund, v, e)
	}
	if allowed(constants.ServiceHelpdesk) {
		rows, total, f, e := s.repo.AdminList(
			ctx,
			request(constants.ServiceHelpdesk, []string{constants.HelpdeskStatusOpen}),
		)
		if e != nil {
			save(constants.ServiceHelpdesk, nil, e)
		} else {
			items := []platform.Result{}
			for _, row := range rows {
				items = append(items, view(row))
			}
			save(constants.ServiceHelpdesk, platform.Result{"items": items, "total": total, "has_more": int64(f.Page*f.PageSize) < total}, nil)
		}
	}
	return out, nil
}
