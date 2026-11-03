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
		"order":       "list get admin_list admin_summary",
		"aftersale":   "list get admin_list admin_summary approve reject evidence_download policy_get",
		"helpdesk":    "list get admin_list reply close faq work_queue work_detail assign_work save_view views policy_get",
		"iam":         "admin_get",
		"member":      "admin_get",
		"interaction": "list get admin_list answer",
		"logistics":   "get list tracking",
	},
	"staff_warehouse": {
		"helpdesk":    "work_queue work_detail assign_work save_view views policy_get",
		"fulfillment": "list get admin_list admin_summary dispatch inspect_return",
		"inventory": "get batch_get admin_list adjust adjustments threshold ledger procurement_list " +
			"procurement_get procurement_create procurement_approve procurement_receive " +
			"procurement_cancel",
		"catalog":   "get batch_get admin_list",
		"logistics": "get list tracking tracking_update exceptions exception_resolve",
	},
	"staff_operations": {
		"points":         "offers_admin offer_upsert redemptions_admin redemption_retry",
		"management":     "dashboard",
		"aftersale":      "policy_get policy_upsert",
		"analytics":      "dashboard admin_list list sync_history drilldown",
		"catalog":        "get batch_get admin_list upsert import_preview import_commit import_get",
		"inventory":      "get batch_get",
		"coupon":         "templates_admin template_upsert",
		"campaign":       "admin_list list upsert rule_upsert",
		"content":        "admin_list admin_upsert popup_list popup_upsert popup_configure",
		"notification":   "templates_admin template_upsert",
		"engagement":     "policy_get policy_upsert admin_list admin_cancel",
		"recommendation": "policy_get policy_upsert",
		"search":         "policy_get policy_upsert",
		"flash":          "admin_list upsert requests_admin",
		"group":          "admin_list rules_admin rule_upsert",
	},
	"staff_finance": {
		"helpdesk":       "work_queue work_detail assign_work save_view views policy_get",
		"management":     "dashboard",
		"analytics":      "dashboard sync_history drilldown",
		"order":          "list get admin_list admin_summary",
		"payment":        "list get admin_list admin_summary confirm_demo",
		"refund":         "list get admin_list admin_summary confirm_demo",
		"invoice":        "list get admin_list admin_summary issue_demo adjust_demo reversals",
		"aftersale":      "list get admin_list admin_summary refund",
		"reconciliation": "list get admin_list note",
	},
	"staff_analyst": {
		"management": "dashboard",
		"analytics":  "dashboard admin_list list sync_history drilldown",
		"order":      "admin_summary",
		"payment":    "admin_summary",
		"refund":     "admin_summary",
		"catalog":    "admin_list get batch_get",
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
