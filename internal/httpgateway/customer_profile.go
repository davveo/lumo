package httpgateway

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
	"time"
)

func (g *gateway) customerProfile(ctx context.Context, c platform.Command) (platform.Result, error) {
	if c.ID == "" || len(c.ID) > 128 {
		return nil, status.Error(codes.InvalidArgument, "请选择会员")
	}
	clients := platform.NewClients(g.rpc)
	account, e := clients.IAM.AdminGet(ctx, c.Request())
	if e != nil {
		return nil, e
	}
	if platform.Text(account, "role") != "user" {
		return nil, status.Error(codes.InvalidArgument, "客户档案仅用于普通会员")
	}
	out := platform.Result{"account": account, "generated_at": time.Now().UTC()}
	sections, errors := map[string]platform.Result{}, map[string]string{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	read := func(name, service, action string, op platform.Operation) {
		defer wg.Done()
		if !platform.StaffAllows(c.Role, service, action) {
			return
		}
		value, err := op(
			ctx,
			platform.Request{
				ID:     c.ID,
				UserID: c.UserID,
				Role:   c.Role,
				Data:   map[string]any{"customer_id": c.ID, "page": 1, "page_size": 10},
			},
		)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errors[name] = "此项暂时不可用，请刷新重试"
		} else {
			sections[name] = value
		}
	}
	wg.Add(8)
	go read("member", "member", "customer_get", clients.Member.CustomerGet)
	go read("stats", "analytics", "customer_stats", clients.Analytics.CustomerStats)
	go read("orders", "order", "admin_list", clients.Order.AdminList)
	go read("aftersales", "aftersale", "admin_list", clients.Aftersale.AdminList)
	go read("refunds", "refund", "admin_list", clients.Refund.AdminList)
	go read("tickets", "helpdesk", "admin_list", clients.Helpdesk.AdminList)
	go read("points", "points", "admin_get", clients.Points.AdminGet)
	go read("coupons", "coupon", "customer_assets", clients.Coupon.CustomerAssets)
	wg.Wait()
	out["sections"], out["errors"] = sections, errors
	out["can_edit"] = platform.StaffAllows(c.Role, "member", "customer_update")
	out["can_segment"] = platform.StaffAllows(c.Role, "member", "segment_preview")
	return out, nil
}
func init() {
	publicActions["interaction/review_image"] = true
	reads["review_image"] = true
	reads["review_get"] = true
	for _, a := range []string{"review_get", "review_revise", "followup_save"} {
		userActions["interaction/"+a] = true
	}
	for _, a := range []string{"journey_rules", "journey_rule_save", "journey_list", "journey_run", "journey_cancel"} {
		adminActions["engagement/"+a] = true
	}
	userActions["engagement/journey_open"] = true
	reads["journey_rules"] = true
	reads["journey_list"] = true
	for _, a := range []string{"draft_save", "draft_list", "workspace_get", "publish_check", "draft_publish", "publish_schedule", "publication_cancel", "version_restore"} {
		adminActions["catalog/"+a] = true
	}
	reads["draft_list"] = true
	reads["workspace_get"] = true
	reads["publish_check"] = true
	for _, a := range []string{"export_create", "export_list", "export_get", "export_retry", "export_download"} {
		adminActions["helpdesk/"+a] = true
	}
	reads["export_list"] = true
	reads["export_get"] = true
	reads["export_download"] = true
	for _, action := range []string{"supplier_list", "supplier_upsert", "replenishment_get"} {
		adminActions["inventory/"+action] = true
	}
	reads["supplier_list"], reads["replenishment_get"] = true, true
	adminActions["management/customer_profile"] = true
	reads["customer_profile"] = true
	for _, action := range []string{"customer_get", "customer_update", "segment_list", "segment_upsert", "segment_preview"} {
		adminActions["member/"+action] = true
	}
	for _, action := range []string{"customer_get", "segment_list", "segment_preview"} {
		reads[action] = true
	}
}
