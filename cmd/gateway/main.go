// 飞书机器人网关(M1):长连接接收事件 + 卡片回调,异步驱动 Agent 运行时,
// 全量事件流水落 SQLite。配置见 .env.example,对接步骤见 docs/feishu-bot.md。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	larkcard "github.com/larksuite/oapi-sdk-go/v3/card"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"

	"agent/internal/agent"
	"agent/internal/config"
	"agent/internal/feishuapi"
	"agent/internal/gateway"
	larkws "agent/internal/larkws"
	"agent/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[网关] %v", err)
	}

	st, err := store.Open(cfg.EventDB)
	if err != nil {
		log.Fatalf("[网关] %v", err)
	}
	defer st.Close()

	gw := gateway.New(st, feishuapi.New(cfg.AppID, cfg.AppSecret), &agent.Echo{}, cfg.QueueSize)
	gw.StartWorkers(context.Background(), cfg.Workers)

	eventDispatcher := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(gw.OnP2MessageReceive)
	cardHandler := larkcard.NewCardActionHandler("", "", gw.OnCardAction)

	cli := larkws.NewClient(cfg.AppID, cfg.AppSecret,
		larkws.WithEventHandler(eventDispatcher),
		larkws.WithCardHandler(cardHandler),
		larkws.WithLogLevel(logLevel(cfg.LogLevel)),
		larkws.WithOnReady(func() {
			log.Printf("[网关] 长连接就绪,等待消息 (runtime=echo)")
		}),
		larkws.WithOnError(func(err error) {
			log.Printf("[网关] 连接错误: %v", err)
		}),
	)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Println("[网关] 收到退出信号,关闭长连接")
		cli.Close()
	}()

	log.Printf("[网关] 启动: workers=%d queue=%d db=%s", cfg.Workers, cfg.QueueSize, cfg.EventDB)
	if err := cli.Start(context.Background()); err != nil {
		log.Fatalf("[网关] 长连接退出: %v", err)
	}
	log.Println("[网关] 已退出")
}

func logLevel(s string) larkcore.LogLevel {
	switch s {
	case "debug":
		return larkcore.LogLevelDebug
	case "warn":
		return larkcore.LogLevelWarn
	case "error":
		return larkcore.LogLevelError
	default:
		return larkcore.LogLevelInfo
	}
}
