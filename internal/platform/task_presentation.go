package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"google.golang.org/grpc/status"
)

// Persist only bounded error classes. Raw dependency errors may contain PII,
// connection strings or credentials and must never enter the admin response.
func taskErrorCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "DeadlineExceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "Canceled"
	}
	return status.Code(err).String()
}

var taskEvents = map[string][2]string{
	constants.EventPaymentSucceeded:          {"支付成功通知", "支付单"},
	constants.EventOrderPaymentAccepted:      {"订单收款确认", "订单"},
	constants.EventPaymentRejected:           {"支付未被订单接纳", "支付单"},
	constants.EventOrderAwaitingPayment:      {"订单待付款通知", "订单"},
	constants.EventOrderReadyToFulfill:       {"订单备货通知", "订单"},
	constants.EventOrderClosed:               {"订单关闭通知", "订单"},
	constants.EventOrderCompleted:            {"订单完成通知", "订单"},
	constants.EventRefundSucceeded:           {"退款到账通知", "退款单"},
	constants.EventShipmentDispatched:        {"商品出库通知", "订单"},
	constants.EventDeliveryConfirmed:         {"确认收货通知", "订单"},
	constants.EventGroupFormed:               {"拼团成功通知", "拼团"},
	constants.EventGroupFailed:               {"拼团失败通知", "拼团"},
	constants.EventGroupOrderTerminated:      {"拼团订单终止通知", "订单"},
	constants.EventFlashOrderRequested:       {"秒杀下单请求", "秒杀请求"},
	constants.EventInvoiceIssued:             {"发票开具通知", "发票"},
	constants.EventTicketReplied:             {"客服回复通知", "工单"},
	constants.EventProductChanged:            {"商品信息变更", "商品"},
	constants.EventProductBackInStock:        {"商品到货提醒", "订阅"},
	constants.EventProductPriceReached:       {"商品降价提醒", "订阅"},
	constants.EventReturnInspectionCompleted: {"退货验收通知", "售后单"},
	constants.EventReviewPublished:           {"商品评价发布", "评价"},
	constants.EventBehaviorAccepted:          {"用户行为入库", "行为记录"},
}

func taskFailure(code string) (string, string) {
	switch code {
	case "Unavailable":
		return "依赖服务暂时不可用", "检查关联服务是否恢复，再重试"
	case "DeadlineExceeded":
		return "处理超时，未能及时得到结果", "检查服务响应与网络，恢复后重试"
	case "NotFound":
		return "关联业务记录不存在或尚未同步", "核对关联单据是否存在，再重试"
	case "FailedPrecondition":
		return "业务状态或前置条件不满足", "核对关联订单、库存和资源状态后再重试"
	case "InvalidArgument", "OutOfRange":
		return "业务数据不符合处理要求", "请技术人员核对任务来源和数据，修复后再重试"
	case "PermissionDenied", "Unauthenticated":
		return "服务认证或权限校验失败", "检查服务间认证配置，修复后重试"
	case "ResourceExhausted":
		return "服务繁忙或资源配额不足", "检查服务容量和业务配额后再重试"
	case "AlreadyExists", "Aborted":
		return "发生并发操作或重复业务冲突", "核对关联单据最新状态后再重试"
	case "Canceled":
		return "处理被中断", "确认服务运行正常后重试"
	case "inbox unavailable":
		return "原消费记录不存在", "核对所属服务与原事件记录"
	case "event envelope is not replayable":
		return "原消息不完整或当前服务不处理此类消息", "请技术人员检查原消息和服务订阅"
	case "":
		return "", ""
	default:
		return "任务执行异常", "请技术人员根据追踪编号排查，修复后再重试"
	}
}

func describeTask(row Result, kind, service, payload, carrier string, now time.Time) Result {
	var event Event
	_ = json.Unmarshal([]byte(payload), &event)
	eventType := Text(row, "event_type")
	if event.Type != "" {
		eventType = event.Type
	}
	row["event_type"] = eventType
	meta := taskEvents[eventType]
	if meta[0] == "" {
		meta = [2]string{"业务通知", "关联业务"}
	}
	row["task_title"] = meta[0]
	row["service"] = service
	refs := []Result{}
	seen := map[string]bool{}
	add := func(label, id string) {
		if id == "" || len(id) > 160 || seen[id] {
			return
		}
		seen[id] = true
		refs = append(refs, Result{"label": label, "id": id})
	}
	add(meta[1], event.AggregateID)
	for _, entry := range [][2]string{{"order_id", "订单"}, {"payment_id", "支付单"}, {"refund_id", "退款单"}, {"aftersale_id", "售后单"}, {"sku_id", "商品 SKU"}, {"invoice_id", "发票"}, {"ticket_id", "工单"}, {"group_id", "拼团"}} {
		add(entry[1], Text(event.Data, entry[0]))
	}
	row["business_references"] = refs
	row["business_id"] = event.AggregateID
	var traceCarrier map[string]string
	_ = json.Unmarshal([]byte(carrier), &traceCarrier)
	if parts := strings.Split(traceCarrier["traceparent"], "-"); len(parts) == 4 && len(parts[1]) == 32 {
		row["trace_id"] = parts[1]
	}
	state := Text(row, "state")
	code := Text(row, "last_error_code")
	if kind == "recovery" {
		code = Text(row, "error")
	}
	failure, advice := taskFailure(code)
	lease, _ := row["lease_until"].(time.Time)
	leased := lease.After(now)
	statusLabel, description := "待处理", "等待系统自动处理"
	switch kind {
	case "outbox":
		row["stage_label"] = "发送通知"
		if state == constants.OutboxStatePublished {
			statusLabel, description = "已发送", "通知已送达消息队列，后续处理请查看消费记录"
		} else if leased {
			statusLabel, description = "正在发送", "系统正在发送，请等待本次操作完成"
		} else if Int(row, "attempts") > 0 {
			statusLabel, description = "发送失败待重试", "系统将自动重试，也可在修复问题后手动重试"
		}
	case "inbox":
		row["stage_label"] = "处理业务"
		switch state {
		case constants.InboxStateDone:
			statusLabel, description = "处理完成", "当前服务已完成此通知对应的业务处理"
		case constants.InboxStateDead:
			statusLabel, description = "需人工处理", "自动重试已停止，请排查原因后重新处理"
		case constants.InboxStateRetry:
			statusLabel, description = "等待自动重试", "上次处理未成功，系统会继续尝试"
		case constants.InboxStateProcessing:
			if leased {
				statusLabel, description = "处理中", "当前服务正在处理，请等待完成"
			} else {
				statusLabel, description = "处理超时", "上次处理未完成，处理锁已到期，可重新处理"
			}
		}
	case "recovery":
		row["stage_label"] = "人工重新处理"
		if state == constants.InboxStateDone {
			statusLabel, description = "重试成功", "人工发起的重新处理已完成"
		} else if state == constants.InboxStateDead {
			statusLabel, description = "重试失败", "可到消费处理列表核对原任务，修复后再次发起"
		} else if leased {
			statusLabel, description = "重新处理中", "人工重试正在执行"
		} else {
			statusLabel, description = "重试已排队", "等待系统执行人工重试请求"
		}
	case "audit":
		row["stage_label"], statusLabel, description = "操作记录", "已提交", "操作已记录，执行结果请查看对应任务"
	}
	if failure == "" && (state == constants.InboxStateDead || state == constants.InboxStateRetry && kind == "inbox" || kind == "outbox" && Int(row, "attempts") > 0 && state != constants.OutboxStatePublished) {
		failure, advice = "历史记录未保存具体失败原因", "可根据追踪编号排查；后续失败会记录原因分类"
	}
	row["status_label"], row["status_description"] = statusLabel, description
	row["failure_reason"], row["suggested_action"] = failure, advice
	if row["retryable"] == true {
		row["retry_reason"] = "可在核对业务状态后重试"
	} else if leased {
		row["retry_reason"] = "正在处理，暂不可重复发起"
	} else if kind == "audit" {
		row["retry_reason"] = "仅查看操作记录"
	} else if kind == "recovery" {
		row["retry_reason"] = "请在原消费记录中发起重试"
	} else if state == constants.OutboxStatePublished || state == constants.InboxStateDone {
		row["retry_reason"] = "已完成，无需重试"
	} else {
		row["retry_reason"] = "原消息不完整，无法直接重试"
	}
	return row
}
