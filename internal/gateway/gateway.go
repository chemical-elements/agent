// Package gateway 串联飞书事件接入、幂等去重、异步 worker 与 Agent 运行时。
//
// 接入模式对应调研报告定稿架构(docs/research/04-闭环架构设计.md):
//
//	飞书长连接(事件 + 卡片回调) → 去重/入队(毫秒级 ack) → worker 池 → Runtime → 卡片回复
//
// 消息处理全程异步(LLM 耗时不可控,先回"处理中"卡片再原地更新);
// 卡片动作走轻量同步路径(毫秒级写库),确认执行才转异步。
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"agent/internal/agent"
	"agent/internal/cards"
	"agent/internal/model"
	"agent/internal/store"
)

// FeishuAPI 网关对飞书 OpenAPI 的依赖抽象,便于测试替身。
type FeishuAPI interface {
	ReplyCard(ctx context.Context, messageID, cardJSON string) (string, error)
	ReplyText(ctx context.Context, messageID, text string) error
	PatchCard(ctx context.Context, messageID, cardJSON string) error
}

type Gateway struct {
	runtime agent.Runtime
	api     FeishuAPI
	store   *store.Store
	queue   chan *model.InboundMessage

	pendingMu sync.Mutex
	pending   map[string]*pendingConfirm // turn_id -> 待确认动作(执行前保留)
}

type pendingConfirm struct {
	chatID    string
	messageID string // 确认卡片所在消息 ID
	action    *model.PendingAction
}

func New(st *store.Store, api FeishuAPI, rt agent.Runtime, queueSize int) *Gateway {
	return &Gateway{
		runtime: rt,
		api:     api,
		store:   st,
		queue:   make(chan *model.InboundMessage, queueSize),
		pending: map[string]*pendingConfirm{},
	}
}

// StartWorkers 启动 n 个消息处理 goroutine。
func (g *Gateway) StartWorkers(ctx context.Context, n int) {
	for i := 0; i < n; i++ {
		go g.worker(ctx)
	}
}

func (g *Gateway) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-g.queue:
			g.handleMessage(ctx, msg)
		}
	}
}

// OnP2MessageReceive 是飞书事件 im.message.receive_v1 的入口,
// 由长连接 SDK 在独立 goroutine 中调用,这里只做解析、去重、入队,保证 ack 足够快。
func (g *Gateway) OnP2MessageReceive(_ context.Context, event *larkim.P2MessageReceiveV1) error {
	msg := toInbound(event)
	if msg == nil {
		return nil
	}
	// 全量事件流水;飞书为至少一次投递,event_id 去重防止重复回答
	first, err := g.store.RecordEvent("message.receive", msg.EventID, msg.ChatID, msg.SenderOpenID, msg.MessageID, msg.Raw)
	if err != nil {
		log.Printf("[网关] 事件流水入库失败(继续处理): %v", err)
	} else if !first {
		log.Printf("[网关] 重复事件 %s,跳过", msg.EventID)
		return nil
	}
	select {
	case g.queue <- msg:
	default:
		log.Printf("[网关] 队列已满,丢弃消息 %s", msg.MessageID)
		_, _ = g.store.RecordEvent("worker.drop", "", msg.ChatID, msg.SenderOpenID, msg.MessageID, msg.Raw)
	}
	return nil
}

// handleMessage 完整处理一轮对话:先回"处理中"卡片,Runtime 出结果后原地更新。
func (g *Gateway) handleMessage(ctx context.Context, msg *model.InboundMessage) {
	turnID := newTurnID()
	start := time.Now()

	if msg.MsgType != "text" || msg.Content == "" {
		_ = g.store.BeginTurn(turnID, msg.ChatID, msg.SenderOpenID, "["+msg.MsgType+" 消息]")
		_ = g.api.ReplyText(ctx, msg.MessageID, "M1 网关暂只支持文本消息,请直接输入文字。")
		_ = g.store.FinishTurn(turnID, "", "", 0, "unsupported")
		return
	}

	if err := g.store.BeginTurn(turnID, msg.ChatID, msg.SenderOpenID, msg.Content); err != nil {
		log.Printf("[网关] turn 入库失败(继续处理): %v", err)
	}

	proc, err := cards.ProcessingCard(msg.Content)
	if err != nil {
		log.Printf("[网关] 构造处理中卡片失败: %v", err)
		return
	}
	answerMsgID, err := g.api.ReplyCard(ctx, msg.MessageID, proc)
	if err != nil {
		log.Printf("[网关] 回复处理中卡片失败: %v", err)
		_ = g.store.FinishTurn(turnID, "", "", 0, "error")
		return
	}

	result, err := g.runtime.HandleMessage(ctx, msg)
	elapsed := time.Since(start).Milliseconds()
	switch {
	case err != nil:
		log.Printf("[网关] runtime 出错: %v", err)
		if card, cerr := cards.ErrorCard(msg.Content, err.Error()); cerr == nil {
			_ = g.api.PatchCard(ctx, answerMsgID, card)
		}
		_ = g.store.FinishTurn(turnID, "", answerMsgID, elapsed, "error")
	case result.PendingAction != nil:
		g.pendingMu.Lock()
		g.pending[turnID] = &pendingConfirm{chatID: msg.ChatID, messageID: answerMsgID, action: result.PendingAction}
		g.pendingMu.Unlock()
		if card, cerr := cards.ConfirmCard(result.PendingAction, turnID); cerr == nil {
			_ = g.api.PatchCard(ctx, answerMsgID, card)
		}
		_ = g.store.FinishTurn(turnID, "[待确认] "+result.PendingAction.Title, answerMsgID, elapsed, "pending")
	default:
		footer := fmt.Sprintf("runtime %s · %dms · turn %s", g.runtime.Name(), elapsed, turnID)
		if card, cerr := cards.AnswerCard(msg.Content, result.Answer, turnID, footer); cerr == nil {
			_ = g.api.PatchCard(ctx, answerMsgID, card)
		}
		_ = g.store.FinishTurn(turnID, result.Answer, answerMsgID, elapsed, "ok")
	}
}

// toInbound 把飞书 P2 事件转成内部消息;返回 nil 表示跳过(非用户消息/群聊未@)。
func toInbound(event *larkim.P2MessageReceiveV1) *model.InboundMessage {
	if event == nil || event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil {
		return nil
	}
	m := event.Event.Message
	sender := event.Event.Sender

	// 机器人自己(或其他 bot)发的消息不处理,防止自激循环
	if sender.SenderType != nil && *sender.SenderType != "user" {
		return nil
	}
	// 群聊只在被 @ 时响应;单聊全响应
	chatType := deref(m.ChatType)
	if chatType == "group" && len(m.Mentions) == 0 {
		return nil
	}

	msg := &model.InboundMessage{
		MessageID:  deref(m.MessageId),
		ChatID:     deref(m.ChatId),
		ChatType:   chatType,
		MsgType:    deref(m.MessageType),
		SenderType: deref(sender.SenderType),
	}
	if sender.SenderId != nil {
		msg.SenderOpenID = deref(sender.SenderId.OpenId)
	}
	// Header 在 EventV2Base 与 EventReq 上都存在,需显式经 EventV2Base 取事件头
	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		msg.EventID = event.EventV2Base.Header.EventID
	}
	if m.CreateTime != nil {
		if ts, err := strconv.ParseInt(*m.CreateTime, 10, 64); err == nil {
			msg.CreateTimeMs = ts
		}
	}
	if msg.MsgType == "text" && m.Content != nil {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(*m.Content), &body); err == nil {
			msg.Content = strings.TrimSpace(mentionRe.ReplaceAllString(body.Text, ""))
		}
	}
	msg.Raw = rawBody(event)
	return msg
}

var mentionRe = regexp.MustCompile(`@_user_\d+`)

// rawBody 优先取 dispatcher 注入的原始 payload,取不到就重新序列化。
func rawBody(event *larkim.P2MessageReceiveV1) []byte {
	if event.EventReq != nil && len(event.EventReq.Body) > 0 {
		return event.EventReq.Body
	}
	bs, err := json.Marshal(event)
	if err != nil {
		return nil
	}
	return bs
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func newTurnID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return "t" + hex.EncodeToString(b[:])
}
