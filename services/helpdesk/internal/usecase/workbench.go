package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
	"strings"
	"time"
)

type workFilters struct {
	Service     string `json:"service"`
	Assignee    string `json:"assignee"`
	Keyword     string `json:"keyword"`
	OverdueOnly bool   `json:"overdue_only"`
}

func (f workFilters) valid() bool {
	return (f.Service == "" || workService(f.Service)) &&
		(f.Assignee == "" || f.Assignee == "all" || f.Assignee == "mine" || f.Assignee == "unassigned") &&
		len([]rune(f.Keyword)) <= 120
}
func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	if _, e := s.workActor(ctx, c); e != nil {
		return nil, e
	}
	p, e := s.repo.WorkPolicy(ctx)
	return platform.Result{"config": p, "version": p.Version}, e
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	a, e := s.workActor(ctx, c)
	if e != nil {
		return nil, e
	}
	if a.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "only administrator can change team targets")
	}
	var input struct {
		Config commercepolicy.Workbench `json:"config"`
	}
	if e = platform.Decode(c.Data, &input); e != nil {
		return nil, e
	}
	if !input.Config.Valid() {
		return nil, bad("team follow-up targets must be 0..720 hours")
	}
	return s.repo.WorkPolicySave(ctx, c, input.Config)
}
func (s *Service) Views(ctx context.Context, c platform.Request) (platform.Result, error) {
	if _, e := s.workActor(ctx, c); e != nil {
		return nil, e
	}
	rows, e := s.repo.Views(ctx, c.UserID)
	return platform.Result{"items": rows}, e
}
func (s *Service) SaveView(ctx context.Context, c platform.Request) (platform.Result, error) {
	actor, e := s.workActor(ctx, c)
	if e != nil {
		return nil, e
	}
	name := strings.TrimSpace(platform.Text(c.Data, "name"))
	var input struct {
		Filters workFilters `json:"filters"`
	}
	if e := platform.Decode(c.Data, &input); e != nil {
		return nil, e
	}
	if name == "" || len([]rune(name)) > 60 || !input.Filters.valid() ||
		(input.Filters.Service != "" && !actor.allows(input.Filters.Service)) {
		return nil, bad("valid view name and filters required")
	}
	raw, _ := json.Marshal(input.Filters)
	return s.repo.SaveView(ctx, c, name, string(raw))
}
func (s *Service) WorkQueue(ctx context.Context, c platform.Request) (platform.Result, error) {
	actor, e := s.workActor(ctx, c)
	if e != nil {
		return nil, e
	}
	var filters workFilters
	if e = platform.Decode(c.Data, &filters); e != nil {
		return nil, e
	}
	if !filters.valid() {
		return nil, bad("invalid work queue filters")
	}
	if filters.Service != "" && !actor.allows(filters.Service) {
		return nil, status.Error(codes.PermissionDenied, "此角色不可读取所选业务队列")
	}
	window := platform.Int(c.Data, "window")
	if window == 0 {
		window = 1
	}
	if window < 1 || window > 10000 {
		return nil, bad("invalid work queue window")
	}
	sources, e := s.workSources(ctx, actor, filters.Service, filters.Keyword, window, nil)
	if e != nil {
		return nil, e
	}
	rows := []platform.Result{}
	keys, ids := []string{}, []string{}
	seen := map[string]bool{}
	more := false
	for service, value := range sources.Values {
		var p struct {
			Items   []platform.Result `json:"items"`
			HasMore bool              `json:"has_more"`
		}
		if e = platform.Decode(value, &p); e != nil {
			return nil, e
		}
		more = more || p.HasMore
		for _, row := range p.Items {
			if service == constants.ServiceAftersale &&
				platform.Text(row, "status") == constants.AftersaleStatusApproved &&
				platform.Text(row, "type") != constants.AftersaleTypeRefund {
				continue
			}
			row["service"] = service
			row["resource_id"] = row["id"]
			key := service + ":" + platform.Text(row, "id")
			row["work_id"] = key
			keys = append(keys, key)
			id := platform.Text(row, "order_id")
			if id != "" && !seen[id] && platform.Text(row, "kind") != constants.FulfillmentKindGift {
				seen[id] = true
				ids = append(ids, id)
			}
			rows = append(rows, row)
		}
	}
	assignments, e := s.repo.Assignments(ctx, keys)
	if e != nil {
		return nil, e
	}
	orders := map[string]platform.Result{}
	if len(ids) > 0 {
		result, err := platform.NewClients(
			s.rpc,
		).Order.BatchGet(
			ctx,
			platform.Request{UserID: actor.ID, Role: constants.RoleSystem, Data: map[string]any{"ids": ids}},
		)
		if err != nil {
			sources.Errors[constants.ServiceOrder] = status.Code(err).String()
		} else {
			orders, e = platform.IndexResults(result, "id")
			if e != nil {
				return nil, e
			}
		}
	}
	policy, e := s.repo.WorkPolicy(ctx)
	if e != nil {
		return nil, e
	}
	items := []platform.Result{}
	now := time.Now().UTC()
	for _, row := range rows {
		assignment := assignments[platform.Text(row, "work_id")]
		if filters.Assignee == "mine" && assignment.AssigneeID != actor.ID ||
			filters.Assignee == "unassigned" && assignment.AssigneeID != "" {
			continue
		}
		created, _ := time.Parse(time.RFC3339Nano, platform.Text(row, "created_at"))
		hours := policy.SupportHours
		switch platform.Text(row, "service") {
		case constants.ServicePayment:
			hours = policy.PaymentHours
		case constants.ServiceRefund:
			hours = policy.RefundHours
		case constants.ServiceFulfillment:
			hours = policy.ShippingHours
		case constants.ServiceAftersale:
			hours = policy.AftersaleHours
		}
		target := "内部跟进目标"
		var due time.Time
		if hours > 0 && !created.IsZero() {
			due = created.Add(time.Duration(hours) * time.Hour)
		}
		if platform.Text(row, "service") == constants.ServiceAftersale {
			var p commercepolicy.Aftersale
			if json.Unmarshal([]byte(platform.Text(row, "policy_json")), &p) == nil && p.Configured &&
				!created.IsZero() &&
				p.ReviewHours > 0 &&
				platform.Text(row, "status") == constants.AftersaleStatusRequested {
				due = created.Add(time.Duration(p.ReviewHours) * time.Hour)
				target = "申请时的审核目标"
			}
		}
		if platform.Text(row, "service") == constants.ServiceFulfillment {
			var order struct {
				Quote struct {
					Shipping struct {
						Promise struct {
							Available bool   `json:"available"`
							Dispatch  string `json:"dispatch_by"`
						} `json:"promise"`
					} `json:"shipping"`
				} `json:"quote"`
			}
			_ = platform.Decode(orders[platform.Text(row, "order_id")], &order)
			if order.Quote.Shipping.Promise.Available {
				day, err := time.ParseInLocation(
					"2006-01-02",
					order.Quote.Shipping.Promise.Dispatch,
					time.FixedZone("Asia/Shanghai", 8*3600),
				)
				if err == nil {
					due = day.AddDate(0, 0, 1).Add(-time.Second)
					target = "下单时的预计发货日期"
				}
			}
		}
		overdue := !due.IsZero() && now.After(due)
		if filters.OverdueOnly && !overdue {
			continue
		}
		row["assignment"] = assignment
		row["overdue"] = overdue
		row["target_label"] = target
		if !created.IsZero() {
			row["waiting_hours"] = max(float64(0), now.Sub(created).Hours())
		}
		if !due.IsZero() {
			row["due_at"] = due.UTC().Format(time.RFC3339Nano)
		}
		items = append(items, row)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := platform.Text(items[i], "due_at"), platform.Text(items[j], "due_at")
		if a != b {
			if a == "" {
				return false
			}
			if b == "" {
				return true
			}
			at, _ := time.Parse(time.RFC3339Nano, a)
			bt, _ := time.Parse(time.RFC3339Nano, b)
			return at.Before(bt)
		}
		return platform.Text(items[i], "created_at") < platform.Text(items[j], "created_at")
	})
	views, e := s.repo.Views(ctx, actor.ID)
	if e != nil {
		return nil, e
	}
	allowed := []string{}
	for _, service := range workServices {
		if actor.allows(service) {
			allowed = append(allowed, service)
		}
	}
	return platform.Result{
		"items":             items,
		"window":            window,
		"has_more_windows":  more,
		"errors":            sources.Errors,
		"partial":           len(sources.Errors) > 0,
		"views":             views,
		"allowed_services":  allowed,
		"actor_id":          actor.ID,
		"actor_name":        actor.Name,
		"can_assign_others": actor.Role == constants.RoleAdmin,
		"policy":            policy,
		"notice":            "按各业务队列最早100条待处理记录合并排序；内部目标不代表到账承诺。",
	}, nil
}
func (s *Service) AssignWork(ctx context.Context, c platform.Request) (platform.Result, error) {
	actor, e := s.workActor(ctx, c)
	if e != nil {
		return nil, e
	}
	var in struct {
		Items    []repository.AssignmentInput `json:"items"`
		Assignee string                       `json:"assignee_id"`
	}
	if e = platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	if len(in.Items) < 1 || len(in.Items) > 50 || c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 ||
		reason == "" ||
		len([]rune(reason)) > 240 {
		return nil, bad("1..50 items, request key and reason required")
	}
	if actor.Role != constants.RoleAdmin && in.Assignee != "" && in.Assignee != actor.ID {
		return nil, status.Error(codes.PermissionDenied, "员工只能领取为自己负责")
	}
	target := actor
	if in.Assignee == "" {
		target = workActor{}
	} else if in.Assignee != actor.ID {
		p, e := platform.NewClients(s.rpc).IAM.AdminGet(ctx, platform.Request{ID: in.Assignee, UserID: actor.ID, Role: constants.RoleAdmin})
		if e != nil {
			return nil, e
		}
		target = workActor{platform.Text(p, "id"), platform.Text(p, "role"), platform.Text(p, "username")}
		if p["active"] == false || target.Role != constants.RoleAdmin && !platform.IsEmployee(target.Role) {
			return nil, bad("负责人必须为启用的管理员或员工")
		}
	}
	ids := map[string][]string{}
	seen := map[string]bool{}
	for _, item := range in.Items {
		key := item.Service + ":" + item.ID
		if !workService(item.Service) || item.ID == "" || len(item.ID) > 128 || item.ExpectedVersion < 0 || seen[key] {
			return nil, bad("invalid or duplicate work item")
		}
		seen[key] = true
		if actor.allows(item.Service) {
			ids[item.Service] = append(ids[item.Service], item.ID)
		}
	}
	snapshots, e := s.workSources(ctx, actor, "", "", 1, ids)
	if e != nil {
		return nil, e
	}
	found := map[string]platform.Result{}
	for service, value := range snapshots.Values {
		var p struct{ Items []platform.Result }
		if e = platform.Decode(value, &p); e != nil {
			return nil, e
		}
		for _, row := range p.Items {
			found[service+":"+platform.Text(row, "id")] = row
		}
	}
	eligible := []repository.AssignmentInput{}
	results := []platform.Result{}
	for _, item := range in.Items {
		row := found[item.Service+":"+item.ID]
		why := ""
		if !actor.allows(item.Service) {
			why = "当前角色不可分派此业务"
		} else if row == nil {
			why = "任务不存在、已处理或暂不可读取"
		} else if target.ID != "" && !target.allows(item.Service) {
			why = "负责人角色不能处理此业务"
		}
		if why != "" {
			results = append(
				results,
				platform.Result{"id": item.ID, "service": item.Service, "ok": false, "error": why},
			)
			continue
		}
		item.OrderID = platform.Text(row, "order_id")
		eligible = append(eligible, item)
	}
	changed, e := s.repo.Assign(ctx, c, eligible, target.ID, target.Name, actor.Role == constants.RoleAdmin)
	if e != nil {
		return nil, e
	}
	results = append(results, changed...)
	return platform.Result{"items": results}, nil
}
