package usecase

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
	"strings"
	"time"
	"unicode"
)

type exportSource struct {
	service, action string
	read            platform.Operation
}

func (s *Service) exportSource(name string, data map[string]any) (exportSource, error) {
	c := platform.NewClients(s.rpc)
	sources := map[string]exportSource{
		"order/admin_list": {
			"order",
			"admin_list",
			c.Order.AdminList,
		}, "payment/admin_list": {"payment", "admin_list", c.Payment.AdminList}, "refund/admin_list": {"refund", "admin_list", c.Refund.AdminList},
		"aftersale/admin_list": {
			"aftersale",
			"admin_list",
			c.Aftersale.AdminList,
		}, "fulfillment/admin_list": {"fulfillment", "admin_list", c.Fulfillment.AdminList}, "invoice/admin_list": {"invoice", "admin_list", c.Invoice.AdminList},
		"catalog/admin_list": {
			"catalog",
			"admin_list",
			c.Catalog.AdminList,
		}, "iam/admin_list": {"iam", "admin_list", c.IAM.AdminList}, "iam/audit_list": {"iam", "audit_list", c.IAM.AuditList},
		"inventory/admin_list": {
			"inventory",
			"admin_list",
			c.Inventory.AdminList,
		}, "inventory/ledger": {"inventory", "ledger", c.Inventory.Ledger}, "inventory/adjustments": {"inventory", "adjustments", c.Inventory.Adjustments},
		"flash/admin_list": {
			"flash",
			"admin_list",
			c.Flash.AdminList,
		}, "flash/requests_admin": {"flash", "requests_admin", c.Flash.RequestsAdmin}, "group/rules_admin": {"group", "rules_admin", c.Group.RulesAdmin}, "group/admin_list": {"group", "admin_list", c.Group.AdminList}, "campaign/admin_list": {"campaign", "admin_list", c.Campaign.AdminList},
		"notification/templates_admin": {
			"notification",
			"templates_admin",
			c.Notification.TemplatesAdmin,
		}, "content/admin_list": {"content", "admin_list", c.Content.AdminList}, "points/ledger": {"points", "ledger", c.Points.Ledger},
		"reconciliation/admin_list": {
			"reconciliation",
			"admin_list",
			c.Reconciliation.AdminList,
		}, "reconciliation/get": {"reconciliation", "get", c.Reconciliation.Get},
	}
	if name == "management/tasks" {
		target := platform.Text(data, "service")
		known := false
		for _, v := range platform.ServiceNames() {
			if target == v {
				known = true
			}
		}
		if !known {
			return exportSource{}, bad("请选择有效服务")
		}
		return exportSource{
			"management",
			"tasks",
			func(ctx context.Context, r platform.Request) (platform.Result, error) {
				return platform.ReadServiceTasks(ctx, s.rpc, target, r)
			},
		}, nil
	}
	v, ok := sources[name]
	if !ok {
		return v, bad("此报表暂不支持导出")
	}
	return v, nil
}
func (s *Service) exportActor(ctx context.Context, c platform.Request) (string, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return "", e
	}
	account, e := platform.NewClients(
		s.rpc,
	).IAM.AdminGet(
		ctx,
		platform.Request{ID: c.UserID, UserID: c.UserID, Role: constants.RoleAdmin},
	)
	if e != nil {
		return "", e
	}
	role := platform.Text(account, "role")
	if account["active"] == false || !platform.StaffCanExport(role) {
		return "", status.Error(codes.PermissionDenied, "当前岗位没有导出权限")
	}
	return role, nil
}
func (s *Service) exportOwned(ctx context.Context, c platform.Request) (domain.ExportJob, string, error) {
	role, e := s.exportActor(ctx, c)
	if e != nil {
		return domain.ExportJob{}, "", e
	}
	j, e := s.repo.Export(ctx, c.ID)
	if e != nil {
		return j, role, e
	}
	if j.OwnerID != c.UserID {
		return j, role, status.Error(codes.PermissionDenied, "仅任务创建者可以访问导出结果")
	}
	var data map[string]any
	_ = json.Unmarshal([]byte(j.FiltersJSON), &data)
	source, e := s.exportSource(j.Source, data)
	if e != nil {
		return j, role, e
	}
	if !platform.StaffAllows(role, source.service, source.action) {
		return j, role, status.Error(codes.PermissionDenied, "当前岗位已无此报表权限")
	}
	return j, role, nil
}
func (s *Service) ExportCreate(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "helpdesk.usecase.ExportCreate")
	defer sp.End()
	role, e := s.exportActor(ctx, c)
	if e != nil {
		return nil, e
	}
	if c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 {
		return nil, bad("导出请求标识必填")
	}
	var input struct {
		Source   string         `json:"source"`
		RecordID string         `json:"record_id"`
		Filters  map[string]any `json:"filters"`
	}
	if e = platform.Decode(c.Data, &input); e != nil {
		return nil, e
	}
	if len(input.RecordID) > 128 {
		return nil, bad("invalid record id")
	}
	if input.Filters == nil {
		input.Filters = map[string]any{}
	}
	source, e := s.exportSource(input.Source, input.Filters)
	if e != nil {
		return nil, e
	}
	if !platform.StaffAllows(role, source.service, source.action) {
		return nil, status.Error(codes.PermissionDenied, "没有该报表的导出权限")
	}
	delete(input.Filters, "page")
	delete(input.Filters, "page_size")
	delete(input.Filters, "export")
	delete(input.Filters, "as_of")
	data, e := json.Marshal(input.Filters)
	if e != nil || len(data) > 16000 {
		return nil, bad("筛选条件过长")
	}
	now := time.Now().UTC()
	j, e := s.repo.CreateExport(
		ctx,
		domain.ExportJob{
			ID:          stable("export", c.UserID, c.IdempotencyKey),
			OwnerID:     c.UserID,
			Source:      input.Source,
			RecordID:    input.RecordID,
			FiltersJSON: string(data),
			Status:      constants.ExportStatusQueued,
			Total:       -1,
			CreatedAt:   now,
			ExpiresAt:   now.Add(7 * 24 * time.Hour),
		},
	)
	return platform.Result{"job": j}, e
}
func (s *Service) ExportList(ctx context.Context, c platform.Request) (platform.Result, error) {
	if _, e := s.exportActor(ctx, c); e != nil {
		return nil, e
	}
	page := int(platform.Int(c.Data, "page"))
	if page == 0 {
		page = 1
	}
	if page < 1 || page > 100000 {
		return nil, bad("invalid page")
	}
	return s.repo.Exports(ctx, c.UserID, page)
}
func (s *Service) ExportGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	j, _, e := s.exportOwned(ctx, c)
	return platform.Result{"job": j}, e
}
func (s *Service) ExportRetry(ctx context.Context, c platform.Request) (platform.Result, error) {
	j, _, e := s.exportOwned(ctx, c)
	if e != nil {
		return nil, e
	}
	if j.Status != constants.ExportStatusFailed {
		return platform.Result{"job": j}, nil
	}
	if e = s.repo.RetryExport(ctx, j); e != nil {
		return nil, e
	}
	return s.ExportGet(ctx, c)
}
func (s *Service) ExportDownload(ctx context.Context, c platform.Request) (platform.Result, error) {
	j, _, e := s.exportOwned(ctx, c)
	if e != nil {
		return nil, e
	}
	if j.Status != constants.ExportStatusReady || !time.Now().Before(j.ExpiresAt) {
		return nil, status.Error(codes.FailedPrecondition, "导出未完成或已到期")
	}
	result, e := platform.NewClients(
		s.rpc,
	).Media.Download(
		ctx,
		platform.Request{ID: j.FileID, UserID: j.OwnerID, Role: constants.RoleSystem},
	)
	if e != nil {
		return nil, e
	}
	return platform.Result{
		"download_url": result["download_url"],
		"file_name":    j.Source + ".csv",
		"expires_in":   result["expires_in"],
	}, nil
}

// One bounded remote page per tick. SQL transactions are only local checkpoints.
func (s *Service) TickExports(ctx context.Context) error {
	ctx, sp := platform.Start(ctx, "helpdesk.usecase.TickExports")
	defer sp.End()
	j, e := s.repo.ClaimExport(ctx)
	if e != nil || j == nil {
		return e
	}
	request := platform.Request{ID: j.ID, UserID: j.OwnerID, Role: constants.RoleAdmin}
	_, role, e := s.exportOwned(ctx, request)
	if e != nil {
		return s.repo.FailExport(ctx, *j, status.Error(codes.PermissionDenied, "创建者已无报表权限"))
	}
	if j.Status == constants.ExportStatusUploading {
		return s.uploadExport(ctx, *j)
	}
	data := map[string]any{}
	if e = json.Unmarshal([]byte(j.FiltersJSON), &data); e != nil {
		return s.repo.FailExport(ctx, *j, e)
	}
	source, e := s.exportSource(j.Source, data)
	if e != nil {
		return s.repo.FailExport(ctx, *j, e)
	}
	data["page"] = j.Processed/100 + 1
	data["page_size"] = 100
	data["as_of"] = j.CreatedAt.Format(time.RFC3339Nano)
	data["export"] = true
	result, e := source.read(ctx, platform.Request{ID: j.RecordID, UserID: j.OwnerID, Role: role, Data: data})
	if e != nil {
		return s.repo.FailExport(ctx, *j, status.Error(status.Code(e), "读取报表失败（"+status.Code(e).String()+"），可重试"))
	}
	if j.Source == "reconciliation/get" {
		var nested map[string]any
		bytes, _ := json.Marshal(result["differences"])
		_ = json.Unmarshal(bytes, &nested)
		result = nested
	}
	var batch struct {
		Items []map[string]any `json:"items"`
		Total *int64           `json:"total"`
	}
	if e = platform.Decode(result, &batch); e != nil {
		return s.repo.FailExport(ctx, *j, e)
	}
	total := int64(len(batch.Items))
	if batch.Total != nil {
		total = *batch.Total
	}
	if total < 0 || len(batch.Items) > 100 || j.Total >= 0 && j.Total != total ||
		len(batch.Items) == 0 && j.Processed < total ||
		j.Processed+int64(len(batch.Items)) > total {
		return s.repo.FailExport(ctx, *j, bad("导出范围发生变化，请重试"))
	}
	if e = s.repo.ExportProgress(ctx, *j, batch.Items, total, nil); e != nil {
		return s.repo.FailExport(ctx, *j, e)
	}
	return nil
}
func exportCell(value any) string {
	if value == nil {
		return ""
	}
	s := fmt.Sprint(value)
	trim := strings.TrimLeftFunc(s, unicode.IsSpace)
	if _, textValue := value.(string); textValue && strings.ContainsAny(trim[:min(1, len(trim))], "=+-@") {
		return "'" + s
	}
	return s
}
func (s *Service) uploadExport(ctx context.Context, j domain.ExportJob) error {
	rows, e := s.repo.ExportRows(ctx, j.ID)
	if e != nil {
		return s.repo.FailExport(ctx, j, e)
	}
	keys := map[string]bool{}
	for _, row := range rows {
		for k, v := range row {
			switch v.(type) {
			case map[string]any, []any:
			default:
				keys[k] = true
			}
		}
	}
	columns := []string{}
	for k := range keys {
		columns = append(columns, k)
	}
	sort.Strings(columns)
	var body bytes.Buffer
	body.WriteString("\ufeff")
	writer := csv.NewWriter(&body)
	header := append([]string{"report", "generated_at", "created_before", "filters"}, columns...)
	if e = writer.Write(header); e != nil {
		return e
	}
	for _, row := range rows {
		line := []string{
			j.Source,
			time.Now().UTC().Format(time.RFC3339),
			j.CreatedAt.Format(time.RFC3339),
			j.FiltersJSON,
		}
		for _, key := range columns {
			line = append(line, exportCell(row[key]))
		}
		if e = writer.Write(line); e != nil {
			return e
		}
		writer.Flush()
		if body.Len() > 2<<20 {
			return s.repo.FailExport(ctx, j, bad("CSV超过2MiB，请缩小筛选范围"))
		}
	}
	writer.Flush()
	if e = writer.Error(); e != nil {
		return e
	}
	if body.Len() > 2<<20 {
		return s.repo.FailExport(ctx, j, bad("CSV超过2MiB，请缩小筛选范围"))
	}
	result, e := platform.NewClients(
		s.rpc,
	).Media.StoreExport(
		ctx,
		platform.Request{
			ID:     j.ID,
			UserID: j.OwnerID,
			Role:   constants.RoleSystem,
			Data: map[string]any{
				"file_content": base64.StdEncoding.EncodeToString(body.Bytes()),
				"expires_at":   j.ExpiresAt.Format(time.RFC3339),
			},
		},
	)
	if e != nil {
		return s.repo.FailExport(ctx, j, status.Error(status.Code(e), "保存私有文件失败，请检查对象存储后重试"))
	}
	return s.repo.FinishExport(ctx, j, platform.Text(result, "file_id"))
}
