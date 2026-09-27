# 飞书机器人能力对接(M1)

网关实现调研定稿架构的第一段闭环:**飞书长连接(事件 + 卡片回调)→ 幂等去重 → 异步 worker → Agent 运行时 → 卡片回复 → 全量事件流水落库**。
对应里程碑 M1(0–6 周)的"飞书机器人 + SDK + 事件流水"部分,Agent 运行时当前为 Echo 回声实现。

## 已对接的飞书能力清单

| 能力 | 实现 | 说明 |
|---|---|---|
| 长连接接收事件 | `internal/larkws`(官方 SDK vendor + 补丁) | 免公网 IP、免回调加密,开发期即用 |
| 接收消息 `im.message.receive_v1` | `internal/gateway` | 单聊全响应;群聊仅 @机器人 时响应 |
| 回复文本 / 卡片消息 | `internal/feishuapi` | `ReplyText` / `ReplyCard` |
| 卡片原地更新 | `internal/feishuapi.PatchCard` | "处理中" → 回答/错误/确认/终态 |
| 卡片交互回调(按钮) | `internal/gateway.OnCardAction` | 👍/👎 反馈、确认执行、取消、转人工 |
| 回调即时反馈(toast) | `internal/cards.Toast` | 按钮点击后的轻量反馈 |
| 幂等去重 | `internal/store.RecordEvent` | event_id 唯一索引,飞书至少一次投递兜底 |
| 全量事件流水 | SQLite `events` 表 | 原始 JSON append-only,审计/遥测同源 |
| 结构化对话记录 | SQLite `agent_turns` 表 | 无干预完成率/负反馈率指标数据源 |
| 卡片动作流水 | SQLite `card_actions` 表 | 反馈归因、执行留痕 |

## 飞书侧开通步骤(自建应用)

1. **建应用**: [飞书开发者后台](https://open.feishu.cn/app) → 创建企业自建应用,拿到 `App ID` / `App Secret`;
2. **开机器人能力**: 应用详情 → 应用能力 → 添加"机器人";
3. **配置权限**(权限管理,按需开通):
   - `im:message:send_as_bot` — 以应用身份发消息(回复/卡片);
   - `im:message` — 获取与发送单聊、群组消息(回复消息依赖);
   - `im:message.p2p.msg:readonly` — 读取用户发给机器人的单聊消息;
   - `im:message.group_at_msg:readonly` — 接收群聊中 @机器人的消息;
   - 更新卡片消息需要的写权限(控制台内一般为 `im:message:update_as_bot` 或随 `im:message` 授权)。
4. **订阅事件**: 事件与回调 → 订阅方式选择 **"使用长连接接收事件"** → 添加事件 `im.message.receive_v1`(接收消息);
5. **卡片回调**: 事件与回调 → 卡片交互回调,同样选择 **长连接** 模式(按钮点击经 CARD 帧推给网关);
6. **发布版本**: 版本管理与发布 → 创建版本 → 申请发布(企业自建应用通常管理员即批)。**未发布版本收不到任何事件**;
7. **可用范围**: 确保试用用户/部门在应用可用范围内。

## 本地运行

```bash
cp .env.example .env      # 填入 FEISHU_APP_ID / FEISHU_APP_SECRET
set -a; source .env; set +a
go run ./cmd/gateway
```

看到 `长连接就绪,等待消息` 后即可测试。

## 验收脚本(在飞书里逐条验证)

| 动作 | 预期 |
|---|---|
| 机器人单聊发送 `你好` | 先回"⏳ 处理中"卡片,随后原地下移为"🤖 Agent 回答"卡片,内容 `收到:你好`,底部带 👍/👎/转人工 按钮 |
| 单聊发送 `#审批 退款 100 元` | 卡片变为"⚠️ 待确认执行";点 ✅ → 卡片变"🔄 已确认 · 执行中" → 再变"✅ 已执行"(Echo 演示动作);点 取消 → 变"已取消" |
| 点 👎 | toast "已记录反馈 👎,运营会在 48 小时内归因" |
| 点 转人工 | 卡片变"🙋 已转人工",会话里多一条 M1 说明文本 |
| 发送图片/文件 | 回复"M1 网关暂只支持文本消息" |
| 把机器人拉进群,@它 发消息 | 正常回答;群里不 @ 它发消息则不响应 |
| 断网 30 秒后恢复 | 长连接自动重连(官方 SDK auto-reconnect) |

验收数据落库检查:

```bash
sqlite3 .data/events.db "SELECT kind, count(*) FROM events GROUP BY kind;"
sqlite3 .data/events.db "SELECT turn_id, question, status, feedback, latency_ms FROM agent_turns ORDER BY id DESC LIMIT 5;"
```

## 架构关键决策(来自调研定稿,勿随意更改)

1. **事件快速 ack**:接收 handler 只做解析/去重/入队(毫秒级),消息处理全异步;LLM 耗时不可控,禁止在回调线程里同步等 Runtime;
2. **先"处理中"再 patch**:长任务先回占位卡片,结果原地更新,避免用户以为掉线;
3. **卡片动作同步、确认执行异步**:反馈/取消是毫秒级写库,直接处理并用 toast 反馈;只有确认执行转 goroutine;
4. **event_id 幂等**:飞书事件是至少一次投递,不去重就会重复回答;
5. **卡片按钮的 value 自带路由信息**(action/turn_id),回调侧不解析卡片结构;
6. **生产红线(架构期定死)**:云端多租户环境禁用 Bash 与文件写工具;Agent 永远不进高危白名单,只做起草人和执行人。

## vendor 补丁说明(internal/larkws)

官方 Go SDK v3.12.0 的长连接会把卡片回调帧(CARD)直接丢弃,但 `cardHandler` 字段与
`WithCardHandler` 选项已预留(被注释)。我们把官方 `ws` 包原样拷入 `internal/larkws`,
打三个有标记的补丁:包名、放开 `WithCardHandler`、CARD 帧分发(`card_dispatch.go`)。
上游正式支持后删除该目录即可,详见 [internal/larkws/README.md](../internal/larkws/README.md)。

另:回调 payload 若为 schema 2.0 格式,SDK 的扁平字段可能取不到 open_id/action,
`gateway.toActionEvent` 已做兜底解析,两种格式都兼容。

## M1 → M2 路线

- **Runtime 替换**: `internal/agent.Runtime` 接口已定型,把 `Echo` 换成 Claude Agent SDK worker
  (agent loop + `skills/` 目录 + 业务 MCP 工具),网关零改动;
- **转人工闭环**: `OnTakeover` 里补会话上下文打包 → SME 值班群(P2 三级接管);
- **遥测看板**: `agent_turns` 已含 status/feedback/latency,直接出无干预完成率、负反馈率;
- **权限引擎**: 消息事件里已带 open_id/chat_id,M2 接通讯录 API 映射角色 → 工具白名单;
- **审批流升级**: 交互卡片确认(M1) → 飞书审批流实例(P3,M2+,复用客户审批习惯)。
