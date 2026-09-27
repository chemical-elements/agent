package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	larkcard "github.com/larksuite/oapi-sdk-go/v3/card"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"

	"agent/internal/cards"
	"agent/internal/model"
)

// OnCardAction 是卡片交互回调(card.action.trigger)的处理入口,由长连接 SDK 调用。
//
// 反馈/转人工/取消是毫秒级动作,同步处理并用 toast 给点击人即时反馈;
// "确认执行"在同步路径改卡片状态后,转 goroutine 调 Runtime 执行(耗时不可控)。
// 返回值会作为回调响应体回给飞书(支持 toast / 卡片配置)。
func (g *Gateway) OnCardAction(ctx context.Context, ca *larkcard.CardAction) (any, error) {
	act := toActionEvent(ca)
	if act == nil || act.Action == "" {
		log.Printf("[网关] 卡片回调解析失败,忽略")
		return cards.Toast("error", "回调数据异常"), nil
	}

	if _, err := g.store.RecordEvent("card.action", "", act.ChatID, act.OpenID, act.MessageID, act.Raw); err != nil {
		log.Printf("[网关] 卡片动作流水入库失败(继续处理): %v", err)
	}
	if err := g.store.RecordCardAction(act.TurnID, act.MessageID, act.OpenID, act.Action, marshalValue(act.Value), ""); err != nil {
		log.Printf("[网关] 卡片动作入库失败(继续处理): %v", err)
	}

	switch act.Action {
	case "feedback":
		return g.onFeedback(act), nil
	case "transfer_human":
		return g.onTakeover(ctx, act), nil
	case "confirm_execute":
		return g.onConfirm(ctx, act), nil
	case "cancel_execute":
		return g.onCancel(ctx, act), nil
	default:
		return cards.Toast("info", "未知操作:"+act.Action), nil
	}
}

func (g *Gateway) onFeedback(act *model.ActionEvent) any {
	feedback := act.Feedback
	if feedback == "" {
		feedback = "unknown"
	}
	if err := g.store.SetTurnFeedback(act.TurnID, feedback); err != nil {
		log.Printf("[网关] 反馈入库失败: %v", err)
	}
	if feedback == "down" {
		// P4 反馈流程:👎 单次 48h 内归因;同 Skill 一周 👎≥3 触发紧急修订(M2 由运营看板承接)
		return cards.Toast("success", "已记录反馈 👎,运营会在 48 小时内归因")
	}
	return cards.Toast("success", "已记录反馈 👍")
}

func (g *Gateway) onTakeover(ctx context.Context, act *model.ActionEvent) any {
	_ = g.store.SetTurnStatus(act.TurnID, "takeover")
	question, _ := g.store.TurnQuestion(act.TurnID)
	if card, err := cards.TakeoverCard(question, act.TurnID); err == nil {
		_ = g.api.PatchCard(ctx, act.MessageID, card)
	}
	_ = g.api.ReplyText(ctx, act.MessageID, "已标记转人工。M1 仅记录接管事件,会话上下文打包转 SME 值班群在 M2 交付。")
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := g.runtime.OnTakeover(cctx, act); err != nil {
			log.Printf("[网关] runtime 转人工回调失败: %v", err)
		}
	}()
	return cards.Toast("success", "已转人工")
}

func (g *Gateway) onConfirm(ctx context.Context, act *model.ActionEvent) any {
	item := g.takePending(act.TurnID)
	if item == nil {
		return cards.Toast("error", "该确认已处理或已失效")
	}
	if card, err := cards.ExecutingCard(item.action, act.TurnID); err == nil {
		_ = g.api.PatchCard(ctx, item.messageID, card)
	}
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		merged := mergeValue(item.action.Value, act.Value)
		merged["action"] = "confirm_execute"
		merged["turn_id"] = act.TurnID
		execAct := *act
		execAct.Value = merged

		result, err := g.runtime.ExecuteAction(cctx, &execAct)
		status, resultText := "executed", result
		var card string
		var cerr error
		if err != nil {
			status, resultText = "error", err.Error()
			card, cerr = cards.ErrorCard(item.action.Title, err.Error())
		} else {
			card, cerr = cards.ExecutedCard(item.action, result)
		}
		if cerr == nil {
			_ = g.api.PatchCard(cctx, item.messageID, card)
		}
		_ = g.store.SetTurnStatus(act.TurnID, status)
		_ = g.store.RecordCardAction(act.TurnID, item.messageID, "runtime:"+g.runtime.Name(), "execute", "", resultText)
	}()
	return cards.Toast("success", "已确认,开始执行")
}

func (g *Gateway) onCancel(ctx context.Context, act *model.ActionEvent) any {
	item := g.takePending(act.TurnID)
	if item == nil {
		return cards.Toast("error", "该确认已处理或已失效")
	}
	if card, err := cards.CancelledCard(item.action); err == nil {
		_ = g.api.PatchCard(ctx, item.messageID, card)
	}
	_ = g.store.SetTurnStatus(act.TurnID, "cancelled")
	return cards.Toast("success", "已取消,未执行任何动作")
}

func (g *Gateway) takePending(turnID string) *pendingConfirm {
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	item := g.pending[turnID]
	delete(g.pending, turnID)
	return item
}

// toActionEvent 把 SDK 解析的卡片回调归一化为 ActionEvent。
// SDK 的 CardAction 按 v1 扁平格式取字段;若长连接推送的是 schema 2.0 格式,
// 扁平字段为空,则从原始 payload 兜底解析。
func toActionEvent(ca *larkcard.CardAction) *model.ActionEvent {
	if ca == nil {
		return nil
	}
	act := &model.ActionEvent{
		OpenID:    ca.OpenID,
		MessageID: ca.OpenMessageID,
		ChatID:    ca.OpenChatId,
	}
	if ca.Action != nil {
		act.Action = asString(ca.Action.Value["action"])
		act.Feedback = asString(ca.Action.Value["feedback"])
		act.TurnID = asString(ca.Action.Value["turn_id"])
		act.Value = toStringMap(ca.Action.Value)
	}
	if act.OpenID == "" || act.Action == "" {
		fillFromSchemaV2(act, ca.EventReq)
	}
	if ca.EventReq != nil {
		act.Raw = ca.EventReq.Body
	}
	return act
}

type schemaV2CardAction struct {
	Header struct {
		EventID string `json:"event_id"`
	} `json:"header"`
	Event struct {
		Operator struct {
			OpenID string `json:"open_id"`
		} `json:"operator"`
		Action struct {
			Tag   string         `json:"tag"`
			Value map[string]any `json:"value"`
		} `json:"action"`
		Context struct {
			OpenChatID    string `json:"open_chat_id"`
			OpenMessageID string `json:"open_message_id"`
		} `json:"context"`
	} `json:"event"`
}

func fillFromSchemaV2(act *model.ActionEvent, req *larkevent.EventReq) {
	if req == nil || len(req.Body) == 0 {
		return
	}
	var v2 schemaV2CardAction
	if err := json.Unmarshal(req.Body, &v2); err != nil {
		return
	}
	if act.OpenID == "" {
		act.OpenID = v2.Event.Operator.OpenID
	}
	if act.ChatID == "" {
		act.ChatID = v2.Event.Context.OpenChatID
	}
	if act.MessageID == "" {
		act.MessageID = v2.Event.Context.OpenMessageID
	}
	if act.Action == "" {
		act.Action = asString(v2.Event.Action.Value["action"])
		act.Feedback = asString(v2.Event.Action.Value["feedback"])
		act.TurnID = asString(v2.Event.Action.Value["turn_id"])
		act.Value = toStringMap(v2.Event.Action.Value)
	}
}

func toStringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
			continue
		}
		if v != nil {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}

func mergeValue(base, overlay map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func marshalValue(m map[string]string) string {
	bs, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(bs)
}
