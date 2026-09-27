// Package agent 定义 Agent 运行时接口。
//
// M1 用 Echo 实现打通飞书链路;M1 后续把实现替换为 Claude Agent SDK worker
// (内部跑 agent loop + Skill 目录 + 业务 MCP 工具),网关侧代码不变。
package agent

import (
	"context"

	"agent/internal/model"
)

type Runtime interface {
	// Name 运行时标识,写进回答卡片 footer 便于遥测区分。
	Name() string
	// HandleMessage 处理一条用户消息,同步返回结果。
	// 需要用户确认的动作(P1/L1)通过 TurnResult.PendingAction 表达。
	HandleMessage(ctx context.Context, msg *model.InboundMessage) (*model.TurnResult, error)
	// ExecuteAction 用户在确认卡片点击"确认执行"后的回调(P3 审批链的执行段)。
	ExecuteAction(ctx context.Context, act *model.ActionEvent) (result string, err error)
	// OnTakeover 用户点击"转人工"后的回调(P2 三级接管),实现负责打包上下文交接。
	OnTakeover(ctx context.Context, act *model.ActionEvent) error
}
