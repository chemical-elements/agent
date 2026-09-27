# Agent 能力开放平台

把 Agent 能力开放给各行业一线操作人员:飞书机器人作为统一入口,云端运行 Agent,
全量会话回流统计,中央团队持续调优 Skill 并版本化下发——"飞书入口 + 云端 Agent 运行时 + 中央 Skill 工厂"闭环。

模式论证与完整方案见 [docs/](docs/README.md)(四轮调研逐字归档);当前实施进度:**M1 · 飞书机器人能力对接已完成**。

## 目录结构

```
cmd/gateway/           飞书机器人网关入口(长连接 + 异步 worker)
internal/larkws/       官方飞书 Go SDK ws 包 vendor + 卡片回调补丁(见其 README)
internal/gateway/      事件/卡片回调接入、幂等去重、异步编排、业务流
internal/agent/        Agent 运行时接口 + Echo 联调实现(Claude Agent SDK 从这里接入)
internal/feishuapi/    飞书 IM OpenAPI 封装(回复/卡片更新)
internal/cards/        交互卡片构造(回答/确认执行/转人工/终态)
internal/store/        事件流水 + 对话记录落库(SQLite, append-only)
internal/config/       环境变量配置
skills/                中央 Skill 工厂下发目录(占位,含样例 SKILL.md)
docs/                  调研档案 + 实施文档
```

## 快速开始

```bash
cp .env.example .env      # 填入 FEISHU_APP_ID / FEISHU_APP_SECRET
set -a; source .env; set +a
go run ./cmd/gateway
```

飞书侧开通步骤与验收脚本见 [docs/feishu-bot.md](docs/feishu-bot.md)。

## 当前能力(M1)

- 飞书长连接接收消息(单聊全响应、群聊 @响应),卡片交互回调(👍/👎 反馈、确认执行、转人工);
- "处理中 → 结果原地更新"异步回复模式,event_id 幂等去重;
- 全量事件流水 + 结构化对话记录落 SQLite(遥测/审计/效果计费同源);
- `#审批 <事项>` 演示 L1 确认执行 / P3 审批链(Echo Runtime)。

## 路线图

| 阶段 | 内容 |
|---|---|
| M1(0–6 周) | ✅ 飞书机器人接入 + 事件流水;接下来:Agent SDK worker 接入 Runtime 接口、5 个真实 Skill、卡片反馈遥测看板 |
| M2(6–20 周) | 权限引擎(角色→工具白名单)、审计库、Promptfoo 评测门禁、按部门灰度下发 |
| M3(5 月+) | ISV 多租户、ERP 对接、模型路线定版、对外备案 |

## 测试

```bash
go test ./...
```
