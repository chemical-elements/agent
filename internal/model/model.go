// Package model 定义网关内部流转的数据结构。
package model

// InboundMessage 一条用户发给机器人的消息(已从飞书事件中提取)。
type InboundMessage struct {
	EventID      string // 飞书事件 ID,用于幂等去重
	MessageID    string
	ChatID       string
	ChatType     string // p2p / group
	SenderOpenID string
	SenderType   string // user / bot
	MsgType      string // text / post / image ...
	Content      string // text 消息的纯文本内容(已去掉 @ 占位),其余类型为空
	CreateTimeMs int64  // 消息发送时间(毫秒)
	Raw          []byte // 原始事件 JSON,全量流水入库用
}

// PendingAction L1 确认执行动作(P3 高危操作审批在 M1 的形态:交互卡片确认)。
type PendingAction struct {
	Title  string            // 操作标题
	Detail string            // Agent 给出的依据摘要,供审批人判断
	Value  map[string]string // 确认后回传给 Runtime 的业务参数
}

// TurnResult Runtime 处理一轮消息的结果。
type TurnResult struct {
	Answer        string         // 回答内容;PendingAction 非空时可为空
	PendingAction *PendingAction // 非空时走确认卡片流程而不是直接回答
}

// ActionEvent 一次卡片按钮点击(已归一化,兼容新旧回调格式)。
type ActionEvent struct {
	OpenID    string // 点击人
	MessageID string // 卡片所在消息 ID
	ChatID    string
	Action    string // 约定的 value.action: feedback / confirm_execute / cancel_execute / transfer_human
	Feedback  string // feedback 动作的值: up / down
	TurnID    string
	Value     map[string]string // 按钮携带的其余业务参数
	Raw       []byte            // 回调原始 JSON,全量流水入库用
}
