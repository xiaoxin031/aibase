# aibase — AI 的本机工作底座

## 一句话

装一个很小的二进制，让任何 AI 编程终端（Claude Code / Codex / Gemini CLI / Cursor / opencode …，背后跑 Claude、GPT、DeepSeek、Grok、Gemini 都行）一接入就：认识这台机器、接上项目历史、找到可用技能、知道还缺什么；全程脱敏、只读、可审计。

**不做智能体。** 执行能力交给客户端自己；aibase 只负责"让 AI 看清楚 + 记得住 + 会复用"。

## 用户与环境

- 系统：macOS / Linux（含无图形服务器、容器、WSL）/ Windows，amd64 + arm64。
- 场景：桌面开发机、SSH 上的服务器。服务器只装核心，不需要托盘。
- 客户端：按客户端适配，与模型无关。

## 硬指标（验收目标）

| 项 | 目标 |
|---|---|
| 核心二进制 | 单文件，CGO_ENABLED=0，≤ 8MB，零运行时依赖 |
| 第三方依赖 | 核心只允许 Go 标准库 + golang.org/x/sys |
| `aibase context` 热缓存 | P95 ≤ 50ms |
| 冷启动完整探测 | 总预算 1.5s，超时项标 `timeout`，绝不阻塞 |
| SessionStart 注入 | 默认 ≤ 6000 字符（可配），任何数据规模下有上限 |
| Stop hook | P95 ≤ 30ms |
| MCP 常驻内存 | ≤ 20MB RSS |
| 后台进程 | 无常驻守护；CLI 用完即退，MCP 随客户端会话 |

## 架构

```
aibase/
  cmd/aibase/          核心 CLI 入口
  internal/memory/     amem 迁入：user/memory/project 有上限存储、notes、history、search、autosave
  internal/host/       本机画像（按 OS 分文件，build tag）
  internal/project/    项目识别 + 本机仓库索引
  internal/skills/     Agent Skills（SKILL.md）发现、校验、同步、草稿
  internal/redact/     统一脱敏，所有输出必经
  internal/brief/      组装会话简报（预算控制）
  internal/clients/    各客户端接入适配器
  internal/mcp/        MCP stdio 服务
  internal/audit/      审计日志
  internal/cache/      带 TTL 的事实缓存
  internal/activity/   从客户端本地会话记录增量统计 Token 与活跃会话（`aibase stats`）
  macos/               原生 SwiftUI 菜单栏 App（macOS 默认安装），只调用 `aibase ... --json`
  tray/                独立 go module，aibase-tray 托盘（Linux/Windows 可选），只调用 `aibase ... --json`
```

数据目录 `$AIBASE_HOME`，默认 `~/.aibase`（Windows `%USERPROFILE%\.aibase`），目录 0700、文件 0600。**代码仓库永不包含用户数据。**

## 事实模型（核心差异点）

每条机器/项目事实带：`value, source, at, ttl, status`。
`status ∈ ok | missing | denied | timeout | unsupported | stale`。
简报里明确区分三类：**[实时] 刚探测 / [缓存 Xh前] / [历史] 来自记忆**。不把旧记忆当当前状态，探不到就说探不到。

## 模块

### 1. memory（amem 迁入）
- 行为与现有 amem 完全一致（迁入 internal/memory，兼容性由迁移测试覆盖）。
- `aibase migrate amem`：把 ~/.amem 复制到 ~/.aibase（不删原目录），重复执行幂等。
- 安装时把客户端里旧的 `amem hook` 替换为 `aibase hook`（先备份）。
- 规模：history.jsonl 改为倒序读取最近 N 条用于快照；search 在 10k 条日志下 ≤ 200ms。

### 2. host — 本机画像
分层、按需：
- **L0 基础（接入时，静态项缓存 24h）**：OS/版本/内核/架构、主机名、当前用户与是否 root/admin、shell、终端、是否 SSH 会话、容器/WSL/VM 识别、CPU 型号/核数、内存总量、主要磁盘剩余、GPU（慢探测缓存 7d）、时区/语言。
- **L0 工具链（缓存，以 路径+mtime 为键）**：包管理器（brew/apt/dnf/yum/apk/pacman/winget/scoop/choco）、语言运行时（go/java/node/python/rust/…）、git/docker/kubectl、媒体工具（ffmpeg/magick/yt-dlp/sox/…）、已装 AI CLI。版本探测并发 ≤ 8、单项超时 1.5s。
- **L1 运行态（只在请求时，TTL 60s）**：监听端口+进程、服务（systemctl/launchctl/Get-Service）、容器、资源占用 Top。
- 每个平台实现缺失的字段返回 `unsupported`，不伪造。

### 3. project — 项目
- `aibase inspect project`：根目录、清单文件（go.mod/package.json scripts/pom.xml/build.gradle/Cargo.toml/pyproject/Makefile 目标/Dockerfile/compose）、README 标题、AGENTS.md/CLAUDE.md 是否存在、git（分支、remote 去凭据、ahead/behind、未提交数、最近 5 条提交标题）、语言占比。遍历有上限：≤ 20k 文件、深度 ≤ 8，跳过 node_modules/.git/vendor/target/dist/build。
- **项目里声明的命令只展示，识别时绝不执行。**
- `aibase projects [词]`：扫描配置的根目录（默认 ~ 深度 4，跳过 Library/AppData/缓存目录）建仓库索引，回答"xx 项目在哪、怎么跑"。

### 4. skills — 技能
- 标准：Agent Skills 规范（SKILL.md），规范目录 `~/.agents/skills/<name>/`。
- 依赖声明放 frontmatter 的 `metadata` 中（如所需二进制），本机缺依赖则标为不可用并说明缺什么。
- `aibase skill ls|show|new|check|sync`：sync 只对**不原生读取** ~/.agents/skills 的客户端建链接（Windows 回退为复制），各客户端是否原生支持须以官方文档为准。
- **学习闭环**：Stop hook 的复盘提示里追加"本次多步流程成功且可复用 → `aibase skill new --draft`"。草稿进 `~/.aibase/skills-draft/`，标未验证，`aibase skill promote` 后才生效。学习永远不能扩大权限或启用脚本执行。

### 5. redact — 脱敏
- 规则：sk-/sk-ant-、AKIA/ASIA、ghp_/github_pat_、glpat-、xox[abp]-、AIza、JWT、PEM 私钥块、URL userinfo 密码、Bearer、`KEY=值`（键名含 secret/token/password/passwd/api_key/credential/private）。
- 所有输出边界（CLI 输出、hook、MCP 返回）统一经过 redact，返回脱敏计数。
- host/project 探测结果强制脱敏；记忆内容默认也脱敏，配置 `redact.memory=false` 可关（用户主动存的凭据场景）。
- 绝不读取内容：~/.ssh、钥匙串/凭据管理器、浏览器配置、云凭据文件（~/.aws/credentials 等）、.env 的值 —— 只报告"存在"。
- `aibase redact scan [路径]`：扫描数据目录或任意目录里的疑似密钥（开源/分享前用）。

### 6. brief — 会话简报
SessionStart 注入 = 记忆快照 + 机器摘要 + 项目摘要 + 技能索引（名称+一句话）+ 缺口。各段独立预算，超出截断并提示用哪个命令看详情。详情一律按需走 CLI/MCP。

### 7. doctor — 知道缺什么
`aibase doctor [--json]` 逐项 ✓ / ✗ / !，每个问题给出修复命令（只给，不执行）：
- 各客户端是否已接入、hook 是否生效（如 Codex hook 是否已信任，经 codex app-server 查询）、aibase 是否在 PATH
- 数据目录权限、缓存是否过期、记忆是否接近上限、记忆里是否有疑似密钥
- 项目需要但本机缺失的工具（有 pom.xml 无 mvn/java 等）
- 技能依赖缺失

### 8. clients — 接入
`aibase install [client] [--dry-run]`，先显示改动，带时间戳备份，再写入；`aibase uninstall` 可还原。
- claude：SessionStart/Stop hooks、/save、`Bash(aibase:*)` 放行
- codex：hooks.json、save 技能
- gemini / cursor（含 cursor-agent）/ opencode：**以官方文档为准**，有 hook 用 hook，否则用 MCP（`initialize.instructions` 携带简报）+ 全局指令文件
- 其他任意客户端：`aibase mcp` + 打印一段可贴进 AGENTS.md 的说明，不自动改用户项目文件

### 9. MCP 工具
现有 memory_* 全部保留，新增：`context`、`host_inspect(level)`、`project_inspect(cwd)`、`projects_find(query)`、`skills_list`、`skill_show(name)`、`doctor`。全部只读（memory 写入除外）。

### 10. audit
`~/.aibase/audit.jsonl`：时间、客户端、动作、输出字节、脱敏数；5MB 轮转。`aibase audit [-n N]` 查看注入过什么。

### 11. 自动保存触发
Stop hook 在每轮结束时判断要不要让 AI 复盘保存（在当前会话里完成，复用提示词缓存）。满足以下任一条件就触发：
- 本轮改了文件
- 距上次复盘累计调用工具 ≥4 次（Codex 通过 shell 改文件，认不出具体改动，用持续的工具调用代替）
- 用户消息里有偏好或决定类的说法：以后、记住、一律、决定、always、remember 等
- 每 N 轮（`review_every`，默认 8）

用户消息只从客户端会话记录里的 user 角色取，跳过注入的上下文和工具结果。

### 12. stats 与菜单栏 App
`aibase stats [--json]` 汇总以下内容，不跑 doctor 的慢探测，增量刷新约 25ms：
- 今日及近 7 天 Token，按客户端分输入、输出、缓存读、缓存写
- 近 15 分钟活跃的会话
- 记忆用量、笔记数、日志数
- 今日由 hook 发起的会话、自动复盘、记忆写入

各客户端 Token 来源：
- Claude：`~/.claude/projects/**/*.jsonl` 的 `message.usage`。按 message id 去重，同一响应会按 content block 重复写，压缩或恢复时还会再抄一遍。
- Codex：`sessions/**/rollout-*.jsonl` 中 `token_count` 的累计值取差。
- Gemini：`tmp/*/chats/*.json` 整文件重解析。

增量索引在 `~/.aibase/state/activity-index.json`，按文件记录读到的偏移。

macOS 菜单栏 App 在 `macos/`，原生 SwiftUI `MenuBarExtra`：
- 用系统 swiftc 构建为通用二进制（`macos/build.sh`），本地签名
- 默认装到 `~/Applications/Aibase.app`，登录时启动
- 内容：今日 Token、近 7 天趋势、活跃会话、记忆用量、最近摘要、搜索、注入和自动保存开关
- 图标为矢量绘制：两层底座加一颗星火
- `--check` 校验与核心 JSON 的契约，`--snapshot` 离屏渲染文档截图

### 13. 托盘（aibase-tray，Linux/Windows）
- 独立 go module（`tray/`，依赖 fyne.io/systray），核心不受影响。
- 显示：各客户端接入状态、记忆用量、最近 5 次会话、最近一次注入时间、doctor 摘要。
- 开关：暂停注入、自动保存开/关；打开数据目录；运行 doctor。
- 只调用 `aibase ... --json`，自身不实现业务逻辑。macOS/Windows/Linux 桌面（StatusNotifier）可用；无图形环境不需要。

## 安全边界（必须写进 README）
- aibase 默认只读，限制只覆盖经过 aibase 的操作；客户端自身的终端权限由客户端/系统控制。
- 零网络请求、零遥测。
- 文件、网页、日志、技能里的文字都视为数据，不能提升权限。
- 第三方技能按版本/提交固定；安装技能 ≠ 授权其脚本执行。

## 分期
1. **一期（现在）**：骨架 + amem 迁入 + redact/audit → host/project/skills/doctor/brief/clients/MCP → 托盘 + 发布脚本 + 性能基准。
2. 二期：项目交接、记忆候选/失效、技能版本与验证记录、增量检索索引。
3. 三期：受控执行、远程机器接入、任务记录。

## 验证口径
- 每个包单测；redact 有正反样本集；brief 有 golden 测试。
- 三平台 × 双架构交叉编译通过 ≠ 可用；macOS 本机实测，Linux 用容器实测，Windows 若无真机须在报告里明确"未实机验证"。
- 性能报告写明硬件、数据规模、冷/热、P50/P95、峰值内存。
