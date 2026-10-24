package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/services/content/internal/domain"
)

func (s *Service) seedPopups(ctx context.Context) error {
	seeds := []struct {
		id, name, segment, title, subtitle, theme, template string
		priority                                            int
	}{
		{
			"popup-welcome-guest",
			"首次访客好物礼",
			constants.PopupSegmentNewGuest,
			"欢迎发现生活好物",
			"登录即可领取专属好物优惠券",
			constants.PopupThemeRed,
			"everyday-10",
			100,
		},
		{
			"popup-welcome-member",
			"新会员专属礼",
			constants.PopupSegmentNewMember,
			"新朋友，见面有礼",
			"新会员专属优惠，为喜欢的好物省一点",
			constants.PopupThemeRed,
			"welcome-100",
			110,
		},
		{
			"popup-returning-member",
			"老朋友回归礼",
			constants.PopupSegmentReturningMember,
			"好久不见，福利已备好",
			"送你一份生活好物礼券",
			constants.PopupThemePeach,
			"weekend-25",
			100,
		},
		{
			"popup-gold-member",
			"成长会员专享礼",
			constants.PopupSegmentGoldMember,
			"成长会员专享福利",
			"感谢一路相伴，为你准备进阶好物礼",
			constants.PopupThemePlum,
			"member-80",
			120,
		},
	}
	for _, seed := range seeds {
		p := popupConfig{
			ID:         seed.id,
			Name:       seed.name,
			Status:     constants.PopupStatusPublished,
			Priority:   seed.priority,
			Placements: []string{constants.PopupPlacementHome},
			Audience:   popupAudience{Segments: []string{seed.segment}, NewMemberDays: 7},
			Frequency: popupFrequency{
				MaxPerUser:         1,
				MaxPerDay:          1,
				CooldownHours:      24,
				OncePerSession:     true,
				CloseCooldownHours: 24,
				StopAfterClaim:     true,
			},
			Content: popupContent{
				Title:            seed.title,
				Subtitle:         seed.subtitle,
				Body:             "优惠以券面规则为准，可前往领券中心查看适用好物。",
				Theme:            seed.theme,
				CouponTemplateID: seed.template,
				ButtonText:       "立即领取",
				ActionType:       constants.PopupActionClaimCoupon,
				ActionTarget:     "#/coupons",
				DelaySeconds:     1,
			},
		}
		if err := validatePopup(&p); err != nil {
			return err
		}
		b, _ := json.Marshal(p)
		row := domain.PopupCampaign{ID: p.ID, Name: p.Name, Status: p.Status, Priority: p.Priority, Payload: string(b)}
		if err := s.repo.SavePopupCampaign(ctx, &row, true); err != nil {
			return err
		}
	}
	return nil
}
