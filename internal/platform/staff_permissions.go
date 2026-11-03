package platform

import (
	"github.com/davveo/lumo/internal/constants"
	"sort"
	"strings"
)

// Grants are evaluated at both the HTTP and named-RPC boundary. An employee
// becomes a privileged domain actor only for the individually granted operation.
var employeeGrants = map[string]map[string]string{
	"staff_support": {
		"analytics":   "customer_stats",
		"points":      "admin_get",
		"coupon":      "customer_assets",
		"management":  "global_search",
		"order":       "list get admin_list admin_summary",
		"aftersale":   "list get admin_list admin_summary approve reject evidence_download policy_get",
		"helpdesk":    "list get admin_list reply close faq work_queue work_detail assign_work save_view views policy_get",
		"iam":         "admin_get admin_list",
		"member":      "admin_get customer_get customer_update segment_list",
		"interaction": "list get admin_list answer",
		"logistics":   "get list tracking",
	},
	"staff_warehouse": {
		"analytics":   "sku_sales",
		"management":  "global_search",
		"helpdesk":    "work_queue work_detail assign_work save_view views policy_get",
		"fulfillment": "list get admin_list admin_summary dispatch inspect_return",
		"inventory": "get batch_get admin_list adjust adjustments threshold ledger procurement_list " +
			"procurement_get procurement_create procurement_approve procurement_receive " +
			"procurement_cancel supplier_list supplier_upsert replenishment_get",
		"catalog":   "list get batch_get admin_list",
		"logistics": "get list tracking tracking_update exceptions exception_resolve",
	},
	"staff_operations": {
		"interaction":    "list get admin_list moderate answer review_get review_image",
		"iam":            "admin_get admin_list",
		"member":         "customer_get customer_update segment_list segment_preview segment_upsert",
		"points":         "offers_admin offer_upsert redemptions_admin redemption_retry",
		"management":     "dashboard global_search customer_profile",
		"aftersale":      "policy_get policy_upsert",
		"analytics":      "dashboard admin_list list sync_history drilldown customer_stats",
		"catalog":        "list get batch_get admin_list upsert import_preview import_commit import_get draft_save draft_list workspace_get publish_check draft_publish publish_schedule publication_cancel version_restore",
		"inventory":      "get batch_get",
		"coupon":         "templates_admin template_upsert",
		"campaign":       "admin_list list upsert rule_upsert",
		"content":        "admin_list admin_upsert popup_list popup_upsert popup_configure",
		"notification":   "templates_admin template_upsert",
		"engagement":     "policy_get policy_upsert admin_list admin_cancel journey_rules journey_rule_save journey_list journey_run journey_cancel",
		"recommendation": "policy_get policy_upsert",
		"search":         "policy_get policy_upsert",
		"flash":          "admin_list upsert requests_admin",
		"group":          "admin_list rules_admin rule_upsert",
	},
	"staff_finance": {
		"iam":            "admin_get",
		"member":         "customer_get",
		"helpdesk":       "work_queue work_detail assign_work save_view views policy_get export_create export_list export_get export_retry export_download",
		"management":     "dashboard global_search customer_profile",
		"analytics":      "dashboard sync_history drilldown customer_stats",
		"order":          "list get admin_list admin_summary",
		"payment":        "list get admin_list admin_summary confirm_demo",
		"refund":         "list get admin_list admin_summary confirm_demo",
		"invoice":        "list get admin_list admin_summary issue_demo adjust_demo reversals",
		"aftersale":      "list get admin_list admin_summary refund",
		"reconciliation": "list get admin_list note",
	},
	"staff_analyst": {
		"helpdesk":   "export_create export_list export_get export_retry export_download",
		"management": "dashboard global_search",
		"analytics":  "dashboard admin_list list sync_history drilldown",
		"order":      "admin_summary",
		"payment":    "admin_summary",
		"refund":     "admin_summary",
		"catalog":    "list admin_list get batch_get",
	},
}

func IsEmployee(role string) bool { _, ok := employeeGrants[role]; return ok }
func StaffAllows(role, service, action string) bool {
	if role == constants.RoleAdmin {
		return true
	}
	return strings.Contains(" "+employeeGrants[role][service]+" ", " "+action+" ")
}
func StaffCanExport(role string) bool {
	return role == constants.RoleAdmin || role == "staff_finance" || role == "staff_analyst"
}
func StaffGrants(role string) []string {
	out := []string{}
	for service, actions := range employeeGrants[role] {
		for _, action := range strings.Fields(actions) {
			out = append(out, service+"/"+action)
		}
	}
	sort.Strings(out)
	return out
}
func StaffRoles() []string {
	return []string{"staff_support", "staff_warehouse", "staff_operations", "staff_finance", "staff_analyst"}
}
func StaffRoleAtBoundary(role, service, action string) string {
	if IsEmployee(role) && StaffAllows(role, service, action) {
		return constants.RoleAdmin
	}
	return role
}
