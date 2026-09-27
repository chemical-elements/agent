// Package store 是全量事件流水与会话记录的落库层(SQLite, append-only)。
//
// 设计约定(对应调研报告 §四 审计铁律):
//   - events 表:所有进出网关的事件原样留痕,入库后不修改;event_id 唯一索引兼做幂等去重;
//   - agent_turns 表:每轮对话一条结构化记录,是"无干预完成率/负反馈率"指标的数据源;
//   - card_actions 表:卡片按钮点击流水。
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	ts         TEXT NOT NULL,
	kind       TEXT NOT NULL,              -- message.receive / card.action / worker.drop ...
	event_id   TEXT NOT NULL DEFAULT '',   -- 飞书事件 ID(空表示无幂等语义)
	chat_id    TEXT NOT NULL DEFAULT '',
	open_id    TEXT NOT NULL DEFAULT '',
	message_id TEXT NOT NULL DEFAULT '',
	payload    TEXT NOT NULL               -- 原始 JSON
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_event_id ON events(event_id) WHERE event_id <> '';

CREATE TABLE IF NOT EXISTS agent_turns (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	turn_id           TEXT NOT NULL UNIQUE,
	ts                TEXT NOT NULL,
	chat_id           TEXT NOT NULL,
	open_id           TEXT NOT NULL,
	question          TEXT NOT NULL,
	answer            TEXT NOT NULL DEFAULT '',
	answer_message_id TEXT NOT NULL DEFAULT '',
	latency_ms        INTEGER NOT NULL DEFAULT 0,
	status            TEXT NOT NULL DEFAULT '', -- ok / pending / executed / error / takeover
	feedback          TEXT NOT NULL DEFAULT '' -- up / down / ''
);

CREATE TABLE IF NOT EXISTS card_actions (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	ts         TEXT NOT NULL,
	turn_id    TEXT NOT NULL DEFAULT '',
	message_id TEXT NOT NULL DEFAULT '',
	open_id    TEXT NOT NULL,
	action     TEXT NOT NULL,
	value      TEXT NOT NULL DEFAULT '',
	result     TEXT NOT NULL DEFAULT ''      -- 处理结果摘要(执行结果/错误)
);
`

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开事件库 %s: %w", path, err)
	}
	// M1 单进程写,单连接规避 SQLITE_BUSY,量级完全够用
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化事件库: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// RecordEvent 追加一条事件流水;返回是否首次出现(按 event_id 幂等,重复投递返回 false)。
func (s *Store) RecordEvent(kind, eventID, chatID, openID, messageID string, payload []byte) (bool, error) {
	res, err := s.db.Exec(
		`INSERT INTO events (ts, kind, event_id, chat_id, open_id, message_id, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(event_id) WHERE event_id <> '' DO NOTHING`,
		now(), kind, eventID, chatID, openID, messageID, string(payload),
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *Store) BeginTurn(turnID, chatID, openID, question string) error {
	_, err := s.db.Exec(
		`INSERT INTO agent_turns (turn_id, ts, chat_id, open_id, question) VALUES (?, ?, ?, ?, ?)`,
		turnID, now(), chatID, openID, question,
	)
	return err
}

func (s *Store) FinishTurn(turnID, answer, answerMessageID string, latencyMs int64, status string) error {
	_, err := s.db.Exec(
		`UPDATE agent_turns SET answer = ?, answer_message_id = ?, latency_ms = ?, status = ? WHERE turn_id = ?`,
		answer, answerMessageID, latencyMs, status, turnID,
	)
	return err
}

func (s *Store) SetTurnFeedback(turnID, feedback string) error {
	_, err := s.db.Exec(`UPDATE agent_turns SET feedback = ? WHERE turn_id = ?`, feedback, turnID)
	return err
}

func (s *Store) SetTurnStatus(turnID, status string) error {
	_, err := s.db.Exec(`UPDATE agent_turns SET status = ? WHERE turn_id = ?`, status, turnID)
	return err
}

func (s *Store) RecordCardAction(turnID, messageID, openID, action, value, result string) error {
	_, err := s.db.Exec(
		`INSERT INTO card_actions (ts, turn_id, message_id, open_id, action, value, result)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		now(), turnID, messageID, openID, action, value, result,
	)
	return err
}

// SetCardActionResult 补记卡片动作的异步执行结果(如确认执行后的执行结果)。
func (s *Store) SetCardActionResult(id int64, result string) error {
	_, err := s.db.Exec(`UPDATE card_actions SET result = ? WHERE id = ?`, result, id)
	return err
}

// TurnQuestion 查询一轮对话的原始问题(重建卡片状态时用)。
func (s *Store) TurnQuestion(turnID string) (string, error) {
	var q string
	err := s.db.QueryRow(`SELECT question FROM agent_turns WHERE turn_id = ?`, turnID).Scan(&q)
	return q, err
}

func (s *Store) TurnFeedback(turnID string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT feedback FROM agent_turns WHERE turn_id = ?`, turnID).Scan(&v)
	return v, err
}

func (s *Store) TurnStatus(turnID string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT status FROM agent_turns WHERE turn_id = ?`, turnID).Scan(&v)
	return v, err
}

func now() string { return time.Now().Format(time.RFC3339Nano) }
