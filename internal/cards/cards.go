// Package cards 构造飞书交互卡片 JSON(经典卡片格式)。
//
// 所有按钮的 value 里都带上 action 与 turn_id,卡片回调据此路由;
// 回调侧无需解析卡片结构,只依赖这里写入的约定。
package cards

import (
	"encoding/json"

	"agent/internal/model"
)

// PendingAction 是 model.PendingAction 的别名,避免构造方在各包间来回 import。
type PendingAction = model.PendingAction

// ProcessingCard "处理中"占位卡片,先回复再由结果卡片 patch 更新。
func ProcessingCard(question string) (string, error) {
	return marshal(map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": header("grey", "⏳ 处理中"),
		"elements": []any{
			divMD("**问题**\n" + question),
			noteEl("Agent 正在生成回答…"),
		},
	})
}

// AnswerCard 回答卡片:问题 + 回答 + 👍/👎 + 转人工。
// 卡片的负反馈按钮是遥测体系里最廉价、最真实的质量信号(调研报告 §04)。
func AnswerCard(question, answer, turnID, footer string) (string, error) {
	return marshal(map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": header("blue", "🤖 Agent 回答"),
		"elements": []any{
			divMD("**问题**\n" + question),
			hrEl(),
			divMD(answer),
			noteEl(footer),
			actionsEl(
				button("👍 有帮助", "primary", map[string]string{"action": "feedback", "feedback": "up", "turn_id": turnID}),
				button("👎 没帮助", "default", map[string]string{"action": "feedback", "feedback": "down", "turn_id": turnID}),
				button("转人工", "danger", map[string]string{"action": "transfer_human", "turn_id": turnID}),
			),
		},
	})
}

// ErrorCard 处理异常时替换"处理中"卡片。
func ErrorCard(question, errMsg string) (string, error) {
	return marshal(map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": header("red", "⚠️ 处理失败"),
		"elements": []any{
			divMD("**问题**\n" + question),
			divMD("**错误**\n" + errMsg),
			noteEl("请重试或点击下方按钮转人工"),
			actionsEl(
				button("转人工", "danger", map[string]string{"action": "transfer_human"}),
			),
		},
	})
}

// ConfirmCard L1 确认执行卡片(P3 审批链的 M1 形态):Agent 起草,人点确认后执行。
func ConfirmCard(pa *PendingAction, turnID string) (string, error) {
	return marshal(map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": header("orange", "⚠️ 待确认执行"),
		"elements": []any{
			divMD("**操作**\n" + pa.Title),
			divMD("**依据**\n" + pa.Detail),
			noteEl("turn " + turnID + " · Agent 只做起草人,确认后才执行"),
			actionsEl(
				button("✅ 确认执行", "primary", map[string]string{"action": "confirm_execute", "turn_id": turnID}),
				button("取消", "danger", map[string]string{"action": "cancel_execute", "turn_id": turnID}),
			),
		},
	})
}

// ExecutingCard 已确认、执行中的中间态卡片。
func ExecutingCard(pa *PendingAction, turnID string) (string, error) {
	return marshal(map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": header("blue", "🔄 已确认 · 执行中"),
		"elements": []any{
			divMD("**操作**\n" + pa.Title),
			noteEl("turn " + turnID + " · 执行完成后更新本卡片"),
		},
	})
}

// ExecutedCard 确认后执行完成的终态卡片。
func ExecutedCard(pa *PendingAction, result string) (string, error) {
	return marshal(map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": header("green", "✅ 已执行"),
		"elements": []any{
			divMD("**操作**\n" + pa.Title),
			divMD("**执行结果**\n" + result),
			noteEl("本次执行已留痕审计"),
		},
	})
}

// CancelledCard 取消后的终态卡片。
func CancelledCard(pa *PendingAction) (string, error) {
	return marshal(map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": header("grey", "已取消"),
		"elements": []any{
			divMD("**操作**\n" + pa.Title),
			noteEl("未执行任何动作"),
		},
	})
}

// TakeoverCard 转人工后的卡片状态(P2 三级接管 L2 的入口标记)。
func TakeoverCard(question, turnID string) (string, error) {
	return marshal(map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": header("violet", "🙋 已转人工"),
		"elements": []any{
			divMD("**问题**\n" + question),
			noteEl("turn " + turnID + " · 会话已标记接管,M2 起由客户 SME 值班表跟进"),
		},
	})
}

// Toast 卡片回调响应:以 toast 形式给点击人即时反馈,不重绘卡片。
func Toast(toastType, content string) map[string]any {
	return map[string]any{
		"toast": map[string]any{"type": toastType, "content": content},
	}
}

// TextContent 文本消息 content JSON。
func TextContent(text string) (string, error) {
	return marshal(map[string]any{"text": text})
}

func header(template, title string) map[string]any {
	return map[string]any{
		"template": template,
		"title":    map[string]any{"tag": "plain_text", "content": title},
	}
}

func divMD(content string) map[string]any {
	return map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": content}}
}

func noteEl(text string) map[string]any {
	return map[string]any{"tag": "note", "elements": []any{
		map[string]any{"tag": "plain_text", "content": text},
	}}
}

func hrEl() map[string]any { return map[string]any{"tag": "hr"} }

func actionsEl(buttons ...map[string]any) map[string]any {
	return map[string]any{"tag": "action", "actions": buttons}
}

func button(text, btnType string, value map[string]string) map[string]any {
	return map[string]any{
		"tag":   "button",
		"text":  map[string]any{"tag": "plain_text", "content": text},
		"type":  btnType,
		"value": value,
	}
}

func marshal(v any) (string, error) {
	bs, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(bs), nil
}
