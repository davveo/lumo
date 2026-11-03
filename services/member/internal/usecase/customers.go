package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/member/internal/domain"
	"strings"
)

func (s *Service) CustomerGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "member.usecase.CustomerGet")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.ID == "" || len(c.ID) > 128 {
		return nil, bad("customer id required")
	}
	card, e := s.repo.CustomerGet(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	profile, e := s.Get(ctx, c)
	if e != nil {
		return nil, e
	}
	card["profile"] = profile
	return card, nil
}
func (s *Service) CustomerUpdate(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "member.usecase.CustomerUpdate")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Tags    []string `json:"tags"`
		Note    string   `json:"note"`
		Version int64    `json:"version"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	in.Note = strings.TrimSpace(in.Note)
	if c.ID == "" || len(c.ID) > 128 || len(in.Tags) > 12 || len([]rune(in.Note)) > 1000 || in.Version < 0 {
		return nil, bad("客户编号、标签或备注格式无效")
	}
	tags := []string{}
	seen := map[string]bool{}
	for _, tag := range in.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || len([]rune(tag)) > 24 {
			return nil, bad("每个标签需1至24字")
		}
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	account, e := platform.NewClients(
		s.rpc,
	).IAM.AdminGet(
		ctx,
		platform.Request{ID: c.ID, UserID: c.UserID, Role: c.Role},
	)
	if e != nil {
		return nil, e
	}
	if platform.Text(account, "role") != constants.RoleUser {
		return nil, bad("客户档案仅用于普通会员")
	}
	raw, _ := json.Marshal(tags)
	return s.repo.CustomerUpdate(
		ctx,
		domain.CustomerCard{UserID: c.ID, Tags: string(raw), Note: in.Note, UpdatedBy: c.UserID},
		in.Version,
	)
}
func (s *Service) SegmentList(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.repo.SegmentList(ctx, c)
}
func segment(c platform.Request) (domain.CustomerSegment, error) {
	var row domain.CustomerSegment
	if e := platform.Decode(c.Data, &row); e != nil {
		return row, e
	}
	row.Name = strings.TrimSpace(row.Name)
	if row.MinOrders < 1 || row.MinOrders > 1000000 || row.MinNetCents < 0 || row.MinNetCents > 100000000000000 ||
		row.InactiveDays < 0 ||
		row.InactiveDays > 3650 ||
		row.Version < 0 {
		return row, bad("请设置有效的购买次数、净实付和未购买天数")
	}
	return row, nil
}
func (s *Service) SegmentUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "member.usecase.SegmentUpsert")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	row, e := segment(c)
	if e != nil {
		return nil, e
	}
	if row.Name == "" || len([]rune(row.Name)) > 80 {
		return nil, bad("分群名称需1至80字")
	}
	row.ID = c.ID
	if row.ID == "" {
		row.ID = platform.ID("segment")
	}
	if len(row.ID) > 128 {
		return nil, bad("invalid segment id")
	}
	row.UpdatedBy = c.UserID
	return s.repo.SegmentUpsert(ctx, row, row.Version)
}
func (s *Service) SegmentPreview(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "member.usecase.SegmentPreview")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	row, e := segment(c)
	if e != nil {
		return nil, e
	}
	return platform.NewClients(
		s.rpc,
	).Analytics.CustomerStats(
		ctx,
		platform.Request{
			UserID: c.UserID,
			Role:   c.Role,
			Data: map[string]any{
				"min_orders":    row.MinOrders,
				"min_net_cents": row.MinNetCents,
				"inactive_days": row.InactiveDays,
				"page":          c.Data["page"],
				"page_size":     20,
			},
		},
	)
}
