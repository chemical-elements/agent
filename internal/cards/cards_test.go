package cards

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, s string, err error) map[string]any {
	t.Helper()
	if err != nil {
		t.Fatalf("构造卡片失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("卡片不是合法 JSON: %v", err)
	}
	return m
}

// findButton 在卡片元素里找 value.action 等于 action 的按钮。
func findButton(t *testing.T, card map[string]any, action string) map[string]any {
	t.Helper()
	elements := card["elements"].([]any)
	for _, e := range elements {
		elem := e.(map[string]any)
		if elem["tag"] != "action" {
			continue
		}
		for _, b := range elem["actions"].([]any) {
			btn := b.(map[string]any)
			value := btn["value"].(map[string]any)
			if value["action"] == action {
				return btn
			}
		}
	}
	t.Fatalf("未找到 action=%s 的按钮", action)
	return nil
}

func TestAnswerCardButtonsCarryTurn(t *testing.T) {
	s, err := AnswerCard("问题", "回答", "t123", "footer")
	card := decode(t, s, err)

	if up := findButton(t, card, "feedback"); up["value"].(map[string]any)["turn_id"] != "t123" {
		t.Fatal("反馈按钮必须带 turn_id")
	}
	if th := findButton(t, card, "transfer_human"); th == nil {
		t.Fatal("回答卡片必须有转人工按钮")
	}
}

func TestConfirmCardButtons(t *testing.T) {
	s, err := ConfirmCard(&PendingAction{Title: "退款 100 元", Detail: "依据工单 #1"}, "t456")
	card := decode(t, s, err)

	confirm := findButton(t, card, "confirm_execute")
	if confirm["value"].(map[string]any)["turn_id"] != "t456" {
		t.Fatal("确认按钮必须带 turn_id")
	}
	findButton(t, card, "cancel_execute") // 必须有取消按钮
}

func TestToast(t *testing.T) {
	toast := Toast("success", "已记录")
	m, ok := toast["toast"].(map[string]any)
	if !ok || m["content"] != "已记录" {
		t.Fatalf("toast 结构不对: %v", toast)
	}
}

func TestTextContent(t *testing.T) {
	s, err := TextContent("你好")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil || m["text"] != "你好" {
		t.Fatalf("TextContent: %s %v", s, err)
	}
}

func must(t *testing.T, s string, err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var _ = must // 占位:部分用例仍以 decode(t, s, err) 形式断言
