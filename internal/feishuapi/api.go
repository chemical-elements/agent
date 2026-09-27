// Package feishuapi 封装 M1 需要的飞书 IM OpenAPI:回复消息、发卡片、更新卡片。
package feishuapi

import (
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type API struct {
	cli *lark.Client
}

func New(appID, appSecret string) *API {
	return &API{cli: lark.NewClient(appID, appSecret)}
}

// ReplyCard 以卡片回复消息,返回新消息 ID。后续用 PatchCard 原地更新该卡片。
func (a *API) ReplyCard(ctx context.Context, messageID, cardJSON string) (string, error) {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().MsgType("interactive").Content(cardJSON).Build()).
		Build()
	resp, err := a.cli.Im.Message.Reply(ctx, req)
	if err != nil {
		return "", fmt.Errorf("回复卡片: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("回复卡片: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", fmt.Errorf("回复卡片: 响应缺少 message_id")
	}
	return *resp.Data.MessageId, nil
}

// ReplyText 以文本回复消息。
func (a *API) ReplyText(ctx context.Context, messageID, text string) error {
	bs, err := json.Marshal(map[string]any{"text": text})
	if err != nil {
		return err
	}
	content := string(bs)
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().MsgType("text").Content(content).Build()).
		Build()
	resp, err := a.cli.Im.Message.Reply(ctx, req)
	if err != nil {
		return fmt.Errorf("回复文本: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("回复文本: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// PatchCard 原地更新一张机器人发出的卡片消息(把"处理中"换成结果、把待确认换成终态)。
func (a *API) PatchCard(ctx context.Context, messageID, cardJSON string) error {
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().Content(cardJSON).Build()).
		Build()
	resp, err := a.cli.Im.Message.Patch(ctx, req)
	if err != nil {
		return fmt.Errorf("更新卡片: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("更新卡片: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}
