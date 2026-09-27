// Package config 从环境变量加载网关配置。
package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	AppID     string // 飞书自建应用 App ID
	AppSecret string // 飞书自建应用 App Secret
	EventDB   string // 事件流水 SQLite 文件路径
	Workers   int    // 消息处理并发数
	QueueSize int    // 消息队列长度,打满即丢(记入流水)
	LogLevel  string // debug / info / warn / error
}

func Load() (*Config, error) {
	c := &Config{
		AppID:     os.Getenv("FEISHU_APP_ID"),
		AppSecret: os.Getenv("FEISHU_APP_SECRET"),
		EventDB:   getenv("EVENT_DB", ".data/events.db"),
		Workers:   getInt("WORKERS", 4),
		QueueSize: getInt("QUEUE_SIZE", 256),
		LogLevel:  getenv("LOG_LEVEL", "info"),
	}
	if c.AppID == "" || c.AppSecret == "" {
		return nil, errors.New("缺少 FEISHU_APP_ID / FEISHU_APP_SECRET 环境变量(参考 .env.example)")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
