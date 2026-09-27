package agent

import (
	"context"
	"fmt"
	"strings"

	"agent/internal/model"
)

// Echo M1 联调用的回声运行时:
//   - 普通文本消息:原样回显;
//   - "#审批 <事项>":返回待确认动作,演示 L1 确认执行 / P3 审批链;
//   - 转人工:仅记录。
//
// 替换为真实 Runtime(Claude Agent SDK worker)后,以上行为自然消失。
type Echo struct{}

func (e *Echo) Name() string { return "echo" }

func (e *Echo) HandleMessage(_ context.Context, msg *model.InboundMessage) (*model.TurnResult, error) {
	if title, ok := strings.CutPrefix(msg.Content, "#审批"); ok {
		title = strings.TrimSpace(title)
		if title == "" {
			title = "未命名操作"
		}
		return &model.TurnResult{
			PendingAction: &model.PendingAction{
				Title:  title,
				Detail: "Echo 演示动作:点击确认后回调 ExecuteAction(M1 未接真实工具)。",
				Value:  map[string]string{"title": title},
			},
		}, nil
	}
	return &model.TurnResult{Answer: "收到:" + msg.Content}, nil
}

func (e *Echo) ExecuteAction(_ context.Context, act *model.ActionEvent) (string, error) {
	return fmt.Sprintf("已执行「%s」(M1 演示,未调用真实工具)", act.Value["title"]), nil
}

func (e *Echo) OnTakeover(_ context.Context, act *model.ActionEvent) error {
	// M2: 打包会话上下文(对话记录/已调工具/参数/结果)转 SME 值班群
	return nil
}
