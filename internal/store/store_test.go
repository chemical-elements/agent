package store

import (
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatalf("打开事件库: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRecordEventDedupe(t *testing.T) {
	s := openTest(t)

	first, err := s.RecordEvent("message.receive", "evt-1", "oc1", "ou1", "om1", []byte(`{}`))
	if err != nil || !first {
		t.Fatalf("首次插入应成功: first=%v err=%v", first, err)
	}
	second, err := s.RecordEvent("message.receive", "evt-1", "oc1", "ou1", "om1", []byte(`{}`))
	if err != nil || second {
		t.Fatalf("重复 event_id 应被去重: second=%v err=%v", second, err)
	}

	// 空 event_id 不参与幂等,每次都算新事件
	for i := 0; i < 2; i++ {
		first, err := s.RecordEvent("card.action", "", "oc1", "ou1", "om1", []byte(`{}`))
		if err != nil || !first {
			t.Fatalf("空 event_id 不应去重: first=%v err=%v", first, err)
		}
	}
}

func TestTurnLifecycle(t *testing.T) {
	s := openTest(t)

	if err := s.BeginTurn("t1", "oc1", "ou1", "怎么开发票"); err != nil {
		t.Fatalf("BeginTurn: %v", err)
	}
	if err := s.FinishTurn("t1", "已回答", "om9", 123, "ok"); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	if err := s.SetTurnFeedback("t1", "down"); err != nil {
		t.Fatalf("SetTurnFeedback: %v", err)
	}

	q, err := s.TurnQuestion("t1")
	if err != nil || q != "怎么开发票" {
		t.Fatalf("TurnQuestion: q=%q err=%v", q, err)
	}
}

func TestCardActions(t *testing.T) {
	s := openTest(t)

	if err := s.RecordCardAction("t1", "om1", "ou1", "feedback", `{"feedback":"up"}`, ""); err != nil {
		t.Fatalf("RecordCardAction: %v", err)
	}
	if err := s.SetTurnStatus("t1", "takeover"); err != nil {
		t.Fatalf("SetTurnStatus: %v", err)
	}
}
