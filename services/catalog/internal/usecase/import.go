package usecase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strconv"
	"strings"
	"time"
)

func importView(job domain.ImportJob) platform.Result {
	out := platform.Map(job)
	var rows []domain.ImportRow
	_ = json.Unmarshal([]byte(job.Rows), &rows)
	preview := []platform.Result{}
	for _, row := range rows {
		v := platform.Result{"line": row.Line, "id": row.ID, "status": row.Status, "error": row.Error}
		if len(row.Records) > 0 {
			var values map[string]any
			_ = json.Unmarshal([]byte(row.Records[0].Payload), &values)
			v["name"] = values["name"]
			v["price_cents"] = values["price_cents"]
		}
		preview = append(preview, v)
	}
	out["rows"] = preview
	return out
}
func (s *Service) ImportPreview(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	mode := platform.Text(c.Data, "mode")
	if mode == "" {
		mode = "create"
	}
	if mode != "create" && mode != "update" && mode != "upsert" {
		return nil, bad("导入模式需为create、update或upsert")
	}
	raw, e := base64.StdEncoding.DecodeString(platform.Text(c.Data, "file_base64"))
	if e != nil {
		return nil, bad("文件内容无效")
	}
	cells, e := importFile(platform.Text(c.Data, "filename"), raw)
	if e != nil {
		return nil, bad(e.Error())
	}
	if len(cells) < 2 {
		return nil, bad("文件需包含表头和至少一行商品")
	}
	columns := map[string]int{}
	allowed := map[string]bool{
		"id":          true,
		"name":        true,
		"price_cents": true,
		"brand":       true,
		"category":    true,
		"active":      true,
		"sub":         true,
		"image":       true,
	}
	for i, name := range cells[0] {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !allowed[name] {
			return nil, bad("不支持的列：" + name)
		}
		if _, ok := columns[name]; ok {
			return nil, bad("表头重复：" + name)
		}
		columns[name] = i
	}
	if _, ok := columns["id"]; !ok {
		return nil, bad("表头必须包含id")
	}
	job := domain.ImportJob{
		ID:        platform.ID("IMP"),
		ActorID:   c.UserID,
		Status:    constants.CatalogImportStatusPreview,
		Mode:      mode,
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
	}
	rows := []domain.ImportRow{}
	seen := map[string]bool{}
	for i, cells := range cells[1:] {
		get := func(name string) string {
			n, ok := columns[name]
			if !ok || n >= len(cells) {
				return ""
			}
			return strings.TrimSpace(cells[n])
		}
		id := get("id")
		if id == "" && strings.TrimSpace(strings.Join(cells, "")) == "" {
			continue
		}
		row := domain.ImportRow{Line: i + 2, ID: id, Status: constants.CatalogImportRowStatusValid}
		data := map[string]any{}
		current, err := s.repo.Get(ctx, id)
		if err == nil {
			row.Exists = true
			row.Version = current.UpdatedAt
			data = payload(current)
		} else if status.Code(err) != codes.NotFound {
			return nil, err
		}
		if seen[id] {
			row.Error = "文件内商品编号重复"
		}
		seen[id] = true
		if mode == "create" && row.Exists {
			row.Error = "该商品已存在，请选择更新或新增及更新"
		}
		if mode == "update" && !row.Exists {
			row.Error = "该商品不存在"
		}
		for key := range columns {
			if value := get(key); value != "" && key != "id" {
				switch key {
				case "price_cents":
					n, e := strconv.ParseInt(value, 10, 64)
					if e != nil {
						row.Error = "价格必须为整数分"
					} else {
						data[key] = n
					}
				case "active":
					if value != "true" && value != "false" && value != "1" && value != "0" {
						row.Error = "active需为true或false"
					} else {
						data[key] = value == "true" || value == "1"
					}
				default:
					if len(value) > 1000 {
						row.Error = "字段内容过长"
					}
					data[key] = value
				}
			}
		}
		if _, ok := data["active"]; !ok {
			data["active"] = false
		}
		state := constants.CatalogStatusActive
		if data["active"] == false {
			state = constants.CatalogStatusDisabled
		}
		if id == "" || len(id) > 128 || platform.Text(data, "name") == "" || platform.Int(data, "price_cents") < 1 ||
			platform.Int(data, "price_cents") > 1000000000 {
			row.Error = "需提供有效id、名称和正整数价格（分）"
		}
		if row.Exists && current.ParentID != "" {
			row.Error = "请使用父商品编号，避免将独立规格覆盖成商品"
		}
		if row.Error == "" {
			row.Records, e = catalogRows(id, data, state)
			if e != nil {
				row.Error = e.Error()
			}
		}
		if row.Error != "" {
			row.Status = constants.CatalogImportRowStatusInvalid
			job.Failed++
		}
		rows = append(rows, row)
	}
	job.Total = len(rows)
	if job.Total == 0 {
		return nil, bad("没有可导入商品")
	}
	if job.Failed > 0 {
		job.Status = constants.CatalogImportStatusValidationFailed
	}
	encoded, e := json.Marshal(rows)
	if e != nil {
		return nil, e
	}
	job.Rows = string(encoded)
	if e = s.repo.CreateImport(ctx, &job); e != nil {
		return nil, e
	}
	return importView(job), nil
}
func (s *Service) ImportCommit(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	job, e := s.repo.StartImport(ctx, c.ID, c.UserID)
	if e != nil {
		return nil, e
	}
	return importView(job), nil
}
func (s *Service) ImportGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	job, e := s.repo.GetImport(ctx, c.ID, c.UserID)
	if e != nil {
		return nil, e
	}
	return importView(job), nil
}
func (s *Service) TickImports(ctx context.Context) error { return s.repo.ProcessImports(ctx) }
