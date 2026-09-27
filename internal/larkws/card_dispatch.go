package larkws

import (
	"encoding/json"
	"net/http"

	ws "github.com/gorilla/websocket"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
)

// handleCardFrame 把卡片交互回调(card.action.trigger)交给 CardActionHandler,
// 并按长连接协议回写 {code, data} 信封。回调响应里可以携带 toast / 新卡片配置,
// 交给上层 handler 决定。
func (c *Client) handleCardFrame(run *clientRun, event eventMessage) {
	eventResp := c.cardHandler.Handle(run.ctx, &larkevent.EventReq{Body: event.payload})
	if eventResp == nil {
		eventResp = &larkevent.EventResp{StatusCode: http.StatusOK}
	}

	resp := NewResponseByCode(eventResp.StatusCode)
	resp.Data = eventResp.Body

	payload, err := json.Marshal(resp)
	if err != nil {
		c.logger.Error(run.ctx, c.fmtLog("card response encode failed, message_id: %s, err: %v", event.messageID, err)...)
		return
	}
	event.frame.Payload = payload
	message, err := event.frame.Marshal()
	if err != nil {
		c.logger.Error(run.ctx, c.fmtLog("card response frame encode failed, message_id: %s, err: %v", event.messageID, err)...)
		return
	}
	if err := c.writeMessage(run, ws.BinaryMessage, message); err != nil {
		c.logger.Error(run.ctx, c.fmtLog("card response write failed, message_id: %s, err: %v", event.messageID, err)...)
	}
}
