package gateway

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	larkcard "github.com/larksuite/oapi-sdk-go/v3/card"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"agent/internal/agent"
	"agent/internal/model"
	"agent/internal/store"
)

// ---- 测试替身 ----

type fakeAPI struct {
	mu        sync.Mutex
	replies   []apiCall
	patches   []apiCall
	texts     []apiCall
	nextMsgID int
}

type apiCall struct {
	messageID string
	body      string
}

func (f *fakeAPI) ReplyCard(_ context.Context, messageID, cardJSON string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextMsgID++
	f.replies = append(f.replies, apiCall{messageID, cardJSON})
	return "om_card_" + strconv.Itoa(f.nextMsgID), nil
}

func (f *fakeAPI) PatchCard(_ context.Context, messageID, cardJSON string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patches = append(f.patches, apiCall{messageID, cardJSON})
	return nil
}

func (f *fakeAPI) ReplyText(_ context.Context, messageID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, apiCall{messageID, text})
	return nil
}

type fakeRuntime struct {
	mu       sync.Mutex
	handled  []*model.InboundMessage
	executed []*model.ActionEvent
	takeover []*model.ActionEvent
	result   *model.TurnResult
}

func (f *fakeRuntime) Name() string { return "fake" }

func (f *fakeRuntime) HandleMessage(_ context.Context, msg *model.InboundMessage) (*model.TurnResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handled = append(f.handled, msg)
	return f.result, nil
}

func (f *fakeRuntime) ExecuteAction(_ context.Context, act *model.ActionEvent) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executed = append(f.executed, act)
	return "执行完成", nil
}

func (f *fakeRuntime) OnTakeover(_ context.Context, act *model.ActionEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.takeover = append(f.takeover, act)
	return nil
}

// ---- 构造工具 ----

func newTestGateway(t *testing.T, rt agent.Runtime) (*Gateway, *fakeAPI, *fakeRuntime, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/events.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	api := &fakeAPI{}
	frt, _ := rt.(*fakeRuntime)
	return New(st, api, rt, 16), api, frt, st
}

func p2Event(eventID, text, chatType string, mentions int) *larkim.P2MessageReceiveV1 {
	ms := []*larkim.MentionEvent{}
	for i := 0; i < mentions; i++ {
		ms = append(ms, &larkim.MentionEvent{Key: strPtr("@_user_" + strconv.Itoa(i+1))})
	}
	return &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Schema: "2.0",
			Header: &larkevent.EventHeader{EventID: eventID, EventType: "im.message.receive_v1"},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_test")},
				SenderType: strPtr("user"),
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_test"),
				ChatId:      strPtr("oc_test"),
				ChatType:    strPtr(chatType),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"` + text + `"}`),
				CreateTime:  strPtr("1727000000000"),
				Mentions:    ms,
			},
		},
	}
}

func strPtr(s string) *string { return &s }

// ---- 用例 ----

func TestOnMessageDedupeAndEnqueue(t *testing.T) {
	g, _, _, _ := newTestGateway(t, &fakeRuntime{})

	if err := g.OnP2MessageReceive(context.Background(), p2Event("evt-1", "你好", "p2p", 0)); err != nil {
		t.Fatal(err)
	}
	if err := g.OnP2MessageReceive(context.Background(), p2Event("evt-1", "你好", "p2p", 0)); err != nil {
		t.Fatal(err)
	}
	if len(g.queue) != 1 {
		t.Fatalf("重复事件应去重,队列长度=%d", len(g.queue))
	}
}

func TestOnMessageGroupWithoutMentionSkipped(t *testing.T) {
	g, _, _, _ := newTestGateway(t, &fakeRuntime{})

	if err := g.OnP2MessageReceive(context.Background(), p2Event("evt-1", "大家好", "group", 0)); err != nil {
		t.Fatal(err)
	}
	if len(g.queue) != 0 {
		t.Fatalf("群聊未@不应入队,队列长度=%d", len(g.queue))
	}
}

func TestOnMessageGroupWithMentionAccepted(t *testing.T) {
	g, _, _, _ := newTestGateway(t, &fakeRuntime{})

	err := g.OnP2MessageReceive(context.Background(), p2Event("evt-1", "@_user_1 你好", "group", 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.queue) != 1 {
		t.Fatalf("群聊@机器人应入队,队列长度=%d", len(g.queue))
	}
	msg := <-g.queue
	if strings.Contains(msg.Content, "@_user_1") {
		t.Fatalf("@ 占位符应被剔除: %q", msg.Content)
	}
}

func TestHandleMessageHappyPath(t *testing.T) {
	rt := &fakeRuntime{result: &model.TurnResult{Answer: "这是回答"}}
	g, api, _, _ := newTestGateway(t, rt)
	g.handleMessage(context.Background(), toInbound(p2Event("evt-1", "怎么开发票", "p2p", 0)))

	if len(rt.handled) != 1 {
		t.Fatalf("runtime 应被调用 1 次,实际 %d", len(rt.handled))
	}
	if len(api.replies) != 1 {
		t.Fatalf("应先回复 1 张处理中卡片,实际 %d", len(api.replies))
	}
	if len(api.patches) != 1 {
		t.Fatalf("应原地更新 1 次为回答卡片,实际 %d", len(api.patches))
	}
	if !strings.Contains(api.patches[0].body, "这是回答") {
		t.Fatal("回答卡片应包含回答内容")
	}
	if !strings.Contains(api.patches[0].body, `"turn_id"`) {
		t.Fatal("回答卡片必须带反馈按钮")
	}
}

func TestHandleMessagePendingConfirmAndExecute(t *testing.T) {
	rt := &fakeRuntime{result: &model.TurnResult{
		PendingAction: &model.PendingAction{Title: "退款 100 元", Detail: "工单 #1", Value: map[string]string{"title": "退款 100 元"}},
	}}
	g, api, _, _ := newTestGateway(t, rt)
	g.handleMessage(context.Background(), toInbound(p2Event("evt-1", "#审批 退款 100 元", "p2p", 0)))

	if len(api.patches) != 1 || !strings.Contains(api.patches[0].body, "待确认执行") {
		t.Fatalf("应更新为确认卡片,实际 patches=%d", len(api.patches))
	}

	turnID := turnIDOf(t, g)
	act := &model.ActionEvent{
		Action:    "confirm_execute",
		OpenID:    "ou_test",
		MessageID: api.patches[0].messageID,
		TurnID:    turnID,
		Value:     map[string]string{"action": "confirm_execute", "turn_id": turnID},
		Raw:       []byte(`{}`),
	}
	if _, err := g.OnCardAction(context.Background(), cardActionOf(act)); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool { rt.mu.Lock(); defer rt.mu.Unlock(); return len(rt.executed) == 1 })
	waitFor(t, func() bool {
		api.mu.Lock()
		defer api.mu.Unlock()
		for _, p := range api.patches {
			if strings.Contains(p.body, "已执行") {
				return true
			}
		}
		return false
	})

	// 重复确认应失效
	resp, _ := g.OnCardAction(context.Background(), cardActionOf(act))
	content := resp.(map[string]any)["toast"].(map[string]any)["content"].(string)
	if content != "该确认已处理或已失效" {
		t.Fatalf("重复确认应提示失效: %s", content)
	}
}

func TestFeedbackAction(t *testing.T) {
	rt := &fakeRuntime{result: &model.TurnResult{Answer: "ok"}}
	g, api, _, st := newTestGateway(t, rt)
	g.handleMessage(context.Background(), toInbound(p2Event("evt-1", "问题", "p2p", 0)))
	turnID := turnIDFromCard(t, api)

	act := &model.ActionEvent{Action: "feedback", Feedback: "down", OpenID: "ou_test", MessageID: "om_x", TurnID: turnID, Raw: []byte(`{}`)}
	resp, err := g.OnCardAction(context.Background(), cardActionOf(act))
	if err != nil {
		t.Fatal(err)
	}
	content := resp.(map[string]any)["toast"].(map[string]any)["content"].(string)
	if !strings.Contains(content, "48 小时") {
		t.Fatalf("👎 反馈 toast 应提示 48h 归因: %s", content)
	}
	if feedback, err := st.TurnFeedback(turnID); err != nil || feedback != "down" {
		t.Fatalf("反馈应落库: feedback=%q err=%v", feedback, err)
	}
}

func TestTransferHuman(t *testing.T) {
	rt := &fakeRuntime{result: &model.TurnResult{Answer: "ok"}}
	g, api, _, st := newTestGateway(t, rt)
	g.handleMessage(context.Background(), toInbound(p2Event("evt-1", "问题", "p2p", 0)))
	turnID := turnIDFromCard(t, api)

	act := &model.ActionEvent{Action: "transfer_human", OpenID: "ou_test", MessageID: "om_x", TurnID: turnID, Raw: []byte(`{}`)}
	if _, err := g.OnCardAction(context.Background(), cardActionOf(act)); err != nil {
		t.Fatal(err)
	}
	if status, err := st.TurnStatus(turnID); err != nil || status != "takeover" {
		t.Fatalf("转人工应落库: status=%q err=%v", status, err)
	}
	waitFor(t, func() bool { rt.mu.Lock(); defer rt.mu.Unlock(); return len(rt.takeover) == 1 })
	if len(api.texts) != 1 {
		t.Fatal("转人工应回复说明文本")
	}
}

func TestCancelInvalid(t *testing.T) {
	g, _, _, _ := newTestGateway(t, &fakeRuntime{})
	act := &model.ActionEvent{Action: "cancel_execute", OpenID: "ou_test", MessageID: "om_x", TurnID: "t_missing", Raw: []byte(`{}`)}
	resp, _ := g.OnCardAction(context.Background(), cardActionOf(act))
	content := resp.(map[string]any)["toast"].(map[string]any)["content"].(string)
	if content != "该确认已处理或已失效" {
		t.Fatalf("未知 turn 的取消应提示失效: %s", content)
	}
}

// ---- 辅助 ----

func turnIDOf(t *testing.T, g *Gateway) string {
	t.Helper()
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	for turnID := range g.pending {
		return turnID
	}
	t.Fatal("没有待确认动作")
	return ""
}

// turnIDFromCard 从网关发出的卡片 JSON 里提取真实 turn_id(按钮 value 携带)。
func turnIDFromCard(t *testing.T, api *fakeAPI) string {
	t.Helper()
	re := regexp.MustCompile(`"turn_id":"(t[0-9a-f]+)"`)
	for _, p := range api.patches {
		if m := re.FindStringSubmatch(p.body); m != nil {
			return m[1]
		}
	}
	t.Fatal("卡片里没有 turn_id")
	return ""
}

func cardActionOf(act *model.ActionEvent) *larkcard.CardAction {
	value := map[string]any{}
	for k, v := range act.Value {
		value[k] = v
	}
	value["action"] = act.Action
	if act.Feedback != "" {
		value["feedback"] = act.Feedback
	}
	value["turn_id"] = act.TurnID
	return &larkcard.CardAction{
		OpenID:        act.OpenID,
		OpenMessageID: act.MessageID,
		OpenChatId:    "oc_test",
		Action: &struct {
			Value      map[string]interface{} `json:"value"`
			Tag        string                 `json:"tag"`
			Option     string                 `json:"option"`
			Timezone   string                 `json:"timezone"`
			Name       string                 `json:"name"`
			FormValue  map[string]interface{} `json:"form_value"`
			InputValue string                 `json:"input_value"`
			Options    []string               `json:"options"`
			Checked    bool                   `json:"checked"`
		}{Value: value},
		EventReq: &larkevent.EventReq{Body: act.Raw},
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}
