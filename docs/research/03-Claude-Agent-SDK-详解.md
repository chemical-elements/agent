> **调研档案** · 来源:Claude Code 会话「Agent能力开放平台模式」(第三轮上半调研,2026-09-27) · 内容: Claude Agent SDK 深度解读:Skills 加载、MCP 双通道、权限与 Hooks、商业条款与国内合规现实
> 逐字归档,未改动结论;实施进展以仓库代码与 [feishu-bot.md](../feishu-bot.md) 为准。

官方文档已确认，下面给你一份完整的 Claude Agent SDK 深度介绍，并始终对照你的“中央 Skill 工厂 + 一线运行时”模式来讲。

# Claude Agent SDK 详解

## 一句话定位

**把驱动 Claude Code 的整套 Agent 工程能力(agent loop、内置工具、上下文管理、权限系统)打包成一个库，用 Python 或 TypeScript 编程嵌入你自己的应用。**官方定义就是 "Claude Code as a library"。它不是又一个编排框架，而是把一个经过大规模生产验证的 agent harness(工具实现、渐进式披露、上下文压缩)直接给你——这正是上一轮说的“参考实现”的含义。

## 产品家族中的位置

Anthropic 目前有四种用法，选型时别混淆:

| 你想要 | 用什么 | 本质 |
|---|---|---|
| 在你运营的进程里嵌入 Claude Code 级别的 Agent | **Agent SDK** | 库，你自己部署运维 |
| 终端交互式开发 | Claude Code CLI | 交互界面 |
| 直接调 Claude API,自己写工具循环 | Client SDK | API 封装，tool runner 还在 beta |
| 让 Anthropic 托管整个 Agent | Managed Agents | 托管 harness,云沙箱或你自建的 self-hosted sandbox |

对你的模式：**L2 运行时用 Agent SDK;如果想把运行时也外包，Managed Agents 的 self-hosted sandbox 是中间态选项。**

## 核心能力逐项(对照你的四层架构)

**① Agent loop + 内置工具。**规划→调工具→观察→继续的循环由 harness 驱动，内置 Read/Write/Edit/Bash/Grep/WebSearch 等工具和自动上下文压缩。你不用自己实现循环和长会话管理。

**② Skills 自动加载——这是与你中央平台对接的核心点。**SDK 和 Claude Code 一样，自动从项目的 `.claude/skills/` 和用户级 `~/.claude/skills/` 加载技能，渐进式披露(元数据→全文→捆绑文件)由 harness 处理。**所以你的“中央 Skill 下发”落地方式非常朴素：中央 registry 发布 Skill 包 → 运行环境把 skill 文件夹同步(或热同步)到 `.claude/skills/` → SDK 自动识别。**Skill 格式就是 agentskills.io 开放标准，不锁死在 Anthropic。

**③ MCP 双通道——业务系统接入。**
- **进程内自定义工具**:`@tool` 装饰器(Python)/ `tool()`(TypeScript)+ `create_sdk_mcp_server`,工具跑在你应用进程里，适合封装 ERP/MES/WMS 查询、工单创建这类业务 API;
- **外部 MCP server**:标准 `mcpServers` 配置接入第三方 server。
- **Tool search 默认开启**：工具多了之后 schema 按需加载，不占满每轮上下文——这直接回应了我上次提醒的“每个工具描述每轮都进上下文”的预算问题。

**④ Permissions + Hooks——你的“高危操作确认门”和审计就靠这两件。**`tools` 控制工具可见性，`allowedTools`/`disallowedTools`(支持 `Bash(rm *)` 这类范围规则)控制免审批范围，其余调用走权限流；Hooks 在生命周期关键点(如工具调用前)执行你的代码——高危操作拦截人工确认、全量审计留痕、遥测埋点都从这里挂进去。

**⑤ Sessions。**会话上下文可跨轮维持，支持 resume 和 fork——一线人员和 Agent 的多轮对话、任务中断续做，原生支持。

**⑥ Subagents + Plugins。**子 Agent 处理专项子任务;Plugins 把 skills、agents、hooks、MCP server 打成一个包按路径加载——**这可以充当你的“行业包”载体：一个行业的 Skill + 工具 + 权限规则打成一个 plugin 分发。**

## 最小示例(贴近一线场景)

```python
import asyncio
from claude_agent_sdk import query, ClaudeAgentOptions, ResultMessage

async def main():
    options = ClaudeAgentOptions(
        # 指向中央下发的技能目录(运行环境已同步到 .claude/skills/,自动加载)
        # 挂载业务系统工具的 MCP server
        mcp_servers={"erp": erp_server},          # erp_server 由 @tool 定义
        allowed_tools=["mcp__erp__*"],            # 免审批白名单
        disallowed_tools=["Bash(rm *)"],          # 红线
        model="claude-sonnet-5",
    )
    async for msg in query(prompt="查一下 3 号冷柜最近的工单并给出处理建议",
                           options=options):
        if isinstance(msg, ResultMessage) and msg.subtype == "success":
            print(msg.result)

asyncio.run(main())
```

## 接入你的中央平台的映射

| 你的平台需求 | SDK 对应机制 |
|---|---|
| Skill 集成与下发 | Skill 文件夹同步到 `.claude/skills/`(git/对象存储拉取) |
| 升级部署 | 换文件即生效(渐进式披露按会话加载)；灰度=按环境同步不同版本 |
| 行业能力打包 | Plugin(skills+hooks+MCP server 一个包) |
| 高危确认门 | permission 规则 + PreToolUse hook |
| 遥测回流 | 流式消息流(每次工具调用都是消息)+ hooks → Langfuse |
| 评测门禁 | Promptfoo 等脚本直接驱动 SDK 跑评测集 |
| 模型路由 | `model` 参数 + 官方支持 Anthropic API/Bedrock/Vertex |

## 商业条款、费用与部署现实(重要)

- **SDK 本身免费，按 token 计费**(走 Claude API,有 prompt caching 和 batch 折扣)，受 Anthropic 商业条款约束；给第三方产品用时**必须用 API key 认证**，不允许挂 claude.ai 登录或其速率限制。
- **品牌规范**：你的产品不能叫 "Claude Code",可用 "Powered by Claude" / "Claude Agent"。
- **模型绑定**：官方支持 Claude 系模型(Anthropic API、AWS Bedrock、Google Vertex 三条路)。通过 `ANTHROPIC_BASE_URL` 把 SDK 指向 Anthropic 兼容网关(如 LiteLLM 转 DeepSeek/Qwen/GLM)在技术上是社区常见做法，但**官方不支持，且效果取决于该模型的 agentic 能力**——很多模型能过 API 兼容性测试，却在多步工具循环上明显掉链子，必须用你自己的评测集实测。
- **中国大陆现实**：Anthropic API 不对大陆提供服务。所以诚实的结论是：**Claude Agent SDK 适合你的出海/国际版；国内合规版要么走 Bedrock/Vertex 的可用区域，要么照搬它的架构模式(它文档齐全，agent loop、权限、hooks 的设计都可以借鉴)自研 harness 跑备案模型**——这也是我上次把“自研 harness(参考 Claude Agent SDK 模式)”列为国内合规版首选的原因。

## 对你的模式，它解决了什么、还剩什么

**现成的(~70% 的 L2)**:agent loop、工具实现、Skill 加载与渐进式披露、权限与审计钩子、会话管理、上下文预算、多语言接入。

**仍需你自建(也正是壁垒所在)**：中央 registry 与按组织树的灰度分发(同步 `.claude/skills/` 的调度系统)、一线语音/拍照客户端、多租户计费、评测集体系、以及把 hooks 数据变成“无干预完成率”看板的运营层。SDK 是单个 agent 的运行时，不替你做“一支军队”的调度——那部分是你的产品。

Sources: [Agent SDK 官方总览](https://code.claude.com/docs/en/agent-sdk/overview) · [自定义工具文档](https://code.claude.com/docs/en/agent-sdk/custom-tools) · [TypeScript SDK 仓库](https://github.com/anthropics/claude-agent-sdk-typescript) · [Python SDK 仓库](https://github.com/anthropics/claude-agent-sdk-python) · [示例项目](https://github.com/anthropics/claude-agent-sdk-demos)

如果你想动手验证，我可以现在就在你机器上装 Python 版 SDK、写一个带自定义工具 + 技能目录的最小 Demo 跑通——这能让你直观看到“中央下发 Skill → 本地生效”的完整链路。