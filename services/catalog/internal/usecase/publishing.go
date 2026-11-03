package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func publishingPage(c platform.Request) (int, error) {
	page := int(platform.Int(c.Data, "page"))
	if page == 0 {
		page = 1
	}
	if page < 1 || page > 100000 {
		return 0, bad("invalid page")
	}
	return page, nil
}
func (s *Service) DraftSave(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "catalog.usecase.DraftSave")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.ID == "" || len(c.ID) > 128 {
		return nil, bad("商品编号必填")
	}
	var input struct {
		Product     map[string]any `json:"product"`
		Editor      map[string]any `json:"editor"`
		Version     int64          `json:"version"`
		BaseVersion int64          `json:"base_version"`
	}
	if e := platform.Decode(c.Data, &input); e != nil {
		return nil, e
	}
	if input.Product == nil || input.Version < 0 || input.BaseVersion < 0 {
		return nil, bad("商品草稿和版本必填")
	}
	data, e := json.Marshal(input.Product)
	if e != nil {
		return nil, e
	}
	editor, e := json.Marshal(input.Editor)
	if e != nil {
		return nil, e
	}
	if len(data)+len(editor) > 512<<10 {
		return nil, bad("草稿超过512KiB，请使用图片地址")
	}
	d, e := s.repo.SaveDraft(
		ctx,
		domain.ProductDraft{
			Name:        platform.Text(input.Product, "name"),
			ProductID:   c.ID,
			BaseVersion: input.BaseVersion,
			DataJSON:    string(data),
			EditorJSON:  string(editor),
			ActorID:     c.UserID,
		},
		input.Version,
	)
	return platform.Result{"draft": d}, e
}
func (s *Service) DraftList(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	page, e := publishingPage(c)
	if e != nil {
		return nil, e
	}
	return s.repo.Drafts(ctx, page)
}
func (s *Service) WorkspaceGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.ID == "" || len(c.ID) > 128 {
		return nil, bad("商品编号必填")
	}
	page, e := publishingPage(c)
	if e != nil {
		return nil, e
	}
	out, e := s.repo.Workspace(ctx, c.ID, page)
	if e != nil {
		return nil, e
	}
	if _, ok := c.Data["revision"]; ok {
		v, e := s.repo.Revision(ctx, c.ID, platform.Int(c.Data, "revision"))
		if e != nil {
			return nil, e
		}
		rows, e := domain.DecodeRecords([]byte(v.RowsJSON))
		if e != nil {
			return nil, e
		}
		for _, r := range rows {
			if r.ID == c.ID {
				out["revision"] = view(r)
			}
		}
	}
	current, e := s.repo.Get(ctx, c.ID)
	if e == nil {
		out["current"] = view(current)
	} else if status.Code(e) != codes.NotFound {
		return nil, e
	}
	return out, nil
}

var publicationImage = regexp.MustCompile(`^(?:assets/)?[a-zA-Z0-9_-]+\.(?:png|jpg|jpeg|webp|svg)$`)

func (s *Service) publicationRows(
	ctx context.Context,
	c platform.Request,
	data map[string]any,
	active bool,
) ([]domain.Record, []string, []string) {
	issues, warnings := []string{}, []string{}
	name := strings.TrimSpace(platform.Text(data, "name"))
	if name == "" || len([]rune(name)) > 100 {
		issues = append(issues, "商品名称须为1至100字")
	}
	if strings.TrimSpace(platform.Text(data, "category")) == "" {
		issues = append(issues, "请选择商品分类")
	}
	if price := platform.Int(data, "price_cents"); price < 1 || price > 1000000000 {
		issues = append(issues, "售价须为0.01至1000万元")
	}
	image := platform.Text(data, "image")
	validImage := publicationImage.MatchString(image)
	if u, e := url.Parse(image); e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
		validImage = true
	}
	if !validImage {
		issues = append(issues, "请使用HTTPS商品图片或现有商城素材")
	}
	var policy commercepolicy.ProductService
	_ = platform.Decode(map[string]any{"policy": data["service_policy"]}, &struct {
		Policy *commercepolicy.ProductService `json:"policy"`
	}{Policy: &policy})
	if !policy.Valid() {
		issues = append(issues, "商品售后与保修规则不完整")
	}
	if !policy.Configured {
		warnings = append(warnings, "未配置商品专属服务承诺，前台将不展示相关承诺")
	}
	state := constants.CatalogStatusDisabled
	if active {
		state = constants.CatalogStatusActive
	}
	rows, e := catalogRows(c.ID, data, state)
	if e != nil {
		issues = append(issues, status.Convert(e).Message())
	}
	if active {
		shipping, e := platform.NewClients(
			s.rpc,
		).Logistics.PolicyGet(
			ctx,
			platform.Request{UserID: c.UserID, Role: constants.RoleAdmin},
		)
		if e != nil {
			issues = append(issues, "配送政策暂时无法读取，请稍后重试")
		} else {
			var in struct {
				Config commercepolicy.Shipping `json:"config"`
			}
			if e = platform.Decode(shipping, &in); e != nil || !in.Config.Enabled {
				issues = append(issues, "请先启用配送政策")
			}
		}
	}
	return rows, issues, warnings
}
func (s *Service) PublishCheck(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "catalog.usecase.PublishCheck")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.ID == "" {
		return nil, bad("商品编号必填")
	}
	var in struct {
		Product map[string]any `json:"product"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if in.Product == nil {
		d, e := s.repo.Draft(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		_ = json.Unmarshal([]byte(d.DataJSON), &in.Product)
	}
	rows, issues, warnings := s.publicationRows(ctx, c, in.Product, true)
	return platform.Result{
		"ok":         len(issues) == 0,
		"issues":     issues,
		"warnings":   warnings,
		"sku_count":  len(rows),
		"checked_at": time.Now().UTC(),
	}, nil
}
func (s *Service) DraftPublish(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "catalog.usecase.DraftPublish")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	d, e := s.repo.Draft(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if d.Version != platform.Int(c.Data, "version") {
		return nil, status.Error(codes.Aborted, "草稿版本已变化，请重新载入")
	}
	var data map[string]any
	_ = json.Unmarshal([]byte(d.DataJSON), &data)
	rows, issues, _ := s.publicationRows(ctx, c, data, true)
	if len(issues) > 0 {
		return nil, status.Error(codes.FailedPrecondition, strings.Join(issues, "；"))
	}
	d.ActorID = c.UserID
	if e = s.repo.PublishDraft(ctx, d, rows); e != nil {
		return nil, e
	}
	return s.Get(ctx, c)
}
func (s *Service) PublishSchedule(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "catalog.usecase.PublishSchedule")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 {
		return nil, bad("计划请求标识必填")
	}
	at, e := time.Parse(time.RFC3339, platform.Text(c.Data, "run_at"))
	if e != nil || !at.After(time.Now()) || at.After(time.Now().Add(366*24*time.Hour)) {
		return nil, bad("计划时间须在未来一年内")
	}
	mode := platform.Text(c.Data, "mode")
	if mode != "on" && mode != "off" {
		return nil, bad("请选择定时上架或下架")
	}
	var data map[string]any
	var base int64
	if mode == "on" {
		d, e := s.repo.Draft(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if d.Version != platform.Int(c.Data, "version") {
			return nil, status.Error(codes.Aborted, "草稿已变化")
		}
		base = d.BaseVersion
		_ = json.Unmarshal([]byte(d.DataJSON), &data)
	} else {
		r, e := s.repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if r.ParentID != "" && r.ParentID != constants.CatalogParentLegacy {
			return nil, bad("请选择商品主编号")
		}
		data = payload(r)
		base = r.ProductVersion
	}
	rows, issues, _ := s.publicationRows(ctx, c, data, mode == "on")
	if mode == "off" {
		rows, e = catalogRows(c.ID, data, constants.CatalogStatusDisabled)
		issues = nil
		if e != nil {
			return nil, e
		}
	}
	if len(issues) > 0 {
		return nil, status.Error(codes.FailedPrecondition, strings.Join(issues, "；"))
	}
	raw, _ := domain.EncodeRecords(rows)
	id := stable("publication", c.UserID, c.IdempotencyKey)
	plans := []domain.Publication{
		{
			ID:          id,
			ProductID:   c.ID,
			ActorID:     c.UserID,
			Mode:        mode,
			RowsJSON:    string(raw),
			BaseVersion: base,
			RunAt:       at.UTC(),
			Status:      constants.PublicationStatusScheduled,
		},
	}
	if end := platform.Text(c.Data, "ends_at"); end != "" {
		ends, e := time.Parse(time.RFC3339, end)
		if e != nil || !ends.After(at) || ends.After(time.Now().Add(366*24*time.Hour)) || mode != "on" {
			return nil, bad("下架时间须晚于上架时间且在一年内")
		}
		for i := range rows {
			rows[i].Status = constants.CatalogStatusDisabled
			p := payload(rows[i])
			p["active"] = false
			b, _ := json.Marshal(p)
			rows[i].Payload = string(b)
		}
		raw, _ = domain.EncodeRecords(rows)
		plans = append(
			plans,
			domain.Publication{
				ID:          id + ":off",
				DependsOnID: id,
				ProductID:   c.ID,
				ActorID:     c.UserID,
				Mode:        "off",
				RowsJSON:    string(raw),
				BaseVersion: base + 1,
				RunAt:       ends.UTC(),
				Status:      constants.PublicationStatusScheduled,
			},
		)
	}
	e = s.repo.CreatePublications(ctx, plans)
	return platform.Result{"plans": plans}, e
}
func (s *Service) PublicationCancel(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	e := s.repo.CancelPublication(ctx, c.ID, c.UserID)
	return platform.Result{"cancelled": e == nil}, e
}
func (s *Service) VersionRestore(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "catalog.usecase.VersionRestore")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if _, ok := c.Data["product_version"]; !ok {
		return nil, bad("当前商品版本必填")
	}
	rev, e := s.repo.Revision(ctx, c.ID, platform.Int(c.Data, "revision"))
	if e != nil {
		return nil, e
	}
	rows, e := domain.DecodeRecords([]byte(rev.RowsJSON))
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, bad("历史商品快照为空")
	}
	var parent domain.Record
	for _, r := range rows {
		if r.ID == c.ID {
			parent = r
		}
	}
	if parent.ID == "" {
		return nil, bad("历史主商品不存在")
	}
	_, issues, _ := s.publicationRows(ctx, c, payload(parent), parent.Status == constants.CatalogStatusActive)
	if len(issues) > 0 {
		return nil, status.Error(codes.FailedPrecondition, strings.Join(issues, "；"))
	}
	version := platform.Int(c.Data, "product_version")
	event := platform.Event{
		ID:          platform.ID("evt"),
		Type:        constants.EventProductChanged,
		AggregateID: c.ID,
		UserID:      c.UserID,
		Data:        map[string]any{"restored_from": rev.Version},
	}
	if e = s.repo.SaveProductChecked(ctx, c.ID, rows, &event, &version); e != nil {
		return nil, e
	}
	return s.Get(ctx, c)
}
func (s *Service) Tick(ctx context.Context) error {
	if e := s.TickImports(ctx); e != nil {
		return e
	}
	return s.tickPublication(ctx)
}
func (s *Service) tickPublication(ctx context.Context) error {
	ctx, sp := platform.Start(ctx, "catalog.usecase.TickPublication")
	defer sp.End()
	p, e := s.repo.DuePublication(ctx)
	if e != nil || p == nil {
		return e
	}
	actor, e := platform.NewClients(
		s.rpc,
	).IAM.AdminGet(
		ctx,
		platform.Request{ID: p.ActorID, UserID: p.ActorID, Role: constants.RoleAdmin},
	)
	if e == nil &&
		(actor["active"] == false || !platform.StaffAllows(platform.Text(actor, "role"), "catalog", "publish_schedule")) {
		e = status.Error(codes.PermissionDenied, "计划创建者已无发布权限")
	}
	if e == nil && p.Mode == "on" {
		var rows []domain.Record
		rows, e = domain.DecodeRecords([]byte(p.RowsJSON))
		if e == nil && len(rows) > 0 {
			_, issues, _ := s.publicationRows(
				ctx,
				platform.Request{ID: p.ProductID, UserID: p.ActorID, Role: constants.RoleAdmin},
				payload(rows[0]),
				true,
			)
			if len(issues) > 0 {
				e = status.Error(codes.FailedPrecondition, strings.Join(issues, "；"))
			}
		}
	}
	return s.repo.ExecutePublication(ctx, *p, e)
}
