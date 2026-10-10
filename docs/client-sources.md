# 客户端接入依据

核对日期：2026-10-11。以下为本段实现前读取的官方资料；配置存在不等于客户端实际启用或信任。

| 客户端 | 官方目录与接入方式 | skills 原生读取 `~/.agents/skills` |
|---|---|---|
| Claude Code | `~/.claude/settings.json` 的 SessionStart / Stop；保留 `/save` | 未列为读取目录；同步到 `~/.claude/skills/<name>` |
| Codex | `~/.codex/hooks.json`；须由用户在 `/hooks` 信任并启用 | 是 |
| Gemini CLI | `~/.gemini/settings.json` 的 SessionStart / AfterAgent | 是，优先于同层 `~/.gemini/skills` |
| Cursor 编辑器 | `~/.cursor/hooks.json`，version 1，sessionStart / stop | 是 |
| Cursor CLI (`cursor-agent`) | `~/.cursor/mcp.json`，与编辑器共享 MCP；文档未明确保证 CLI 的 sessionStart，使用 MCP 补足 | Cursor skills 文档支持；CLI 的实际版本仍需实机确认 |
| opencode | `~/.config/opencode/opencode.json` 的本地 MCP + 全局 `AGENTS.md` | 是 |

Agent Skills 采用 SKILL.md 的 YAML frontmatter；name、description 必填，metadata 为 string-to-string map，依赖使用 `metadata.requires`，不增加顶层非规范字段。技能安装或晋升不代表授权执行其脚本。

来源：

- [Agent Skills specification](https://agentskills.io/specification)
- [Claude Code skills](https://code.claude.com/docs/en/skills)
- [Claude Code hooks](https://code.claude.com/docs/en/hooks)
- [Codex skills（原 developers.openai.com/codex/skills 重定向）](https://learn.chatgpt.com/docs/build-skills)
- [Codex hooks](https://learn.chatgpt.com/docs/hooks)
- [Gemini skills](https://geminicli.com/docs/cli/skills/)
- [Gemini settings](https://geminicli.com/docs/cli/settings/)
- [Gemini hooks reference](https://geminicli.com/docs/hooks/reference/)
- [Cursor skills](https://cursor.com/docs/skills)
- [Cursor hooks](https://cursor.com/docs/hooks)
- [Cursor CLI MCP](https://cursor.com/docs/cli/mcp)
- [Cursor MCP](https://cursor.com/docs/mcp)
- [opencode skills](https://opencode.ai/docs/skills/)
- [opencode config](https://opencode.ai/docs/config/)
- [opencode MCP](https://opencode.ai/docs/mcp-servers/)
- [opencode rules](https://opencode.ai/docs/rules/)

实现要点：Gemini SessionStart 输出 `hookSpecificOutput.additionalContext`；Cursor sessionStart 输出 `additional_context`，stop 的续轮提示使用 `followup_message` 并防循环。Cursor MCP 使用 `mcpServers.aibase = {command: <绝对路径>, args: ["mcp"]}`。opencode 的格式不同，使用 `mcp.aibase = {type: "local", command: [<绝对路径>, "mcp"], enabled: true}`。全局指令只合并本工具管理的片段，不覆盖其他内容。

补充核验：Gemini AfterAgent 输入包含 `prompt_response` 与 `stop_hook_active`；输出 `decision: "deny"` 和 `reason` 请求续轮。已处于 retry sequence 时跳过，空响应也不触发复盘。依据为上表的 Gemini hooks reference / Agent hooks。
