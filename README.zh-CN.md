<div align="center">

# aibase

**给所有 AI 编程助手用的本机上下文底座。**

一个很小的二进制。AI 一开口就已经认识这台机器、这个项目，也记得你上次做到哪。

[![ci](https://github.com/xiaoxin031/aibase/actions/workflows/ci.yml/badge.svg)](https://github.com/xiaoxin031/aibase/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/xiaoxin031/aibase?color=36c)](https://github.com/xiaoxin031/aibase/releases)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)
![platforms](https://img.shields.io/badge/macOS%20%7C%20Linux%20%7C%20Windows-amd64%20%7C%20arm64-555)

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

---

每开一个新会话，AI 都从零开始。Claude Code、Codex、Gemini CLI、Cursor、opencode 都一样：重新跑一遍 `uname`，重新读 `package.json`，重新发现没装 Java，再问你一遍习惯怎么干活。到了服务器上更麻烦。

**aibase 解决的是上下文问题，不是再造一个智能体。** 它是一个约 4MB、零依赖的二进制。会话一开始，它就递给 AI 一份简短、准确、已脱敏的简报；想知道更多，AI 再按需去查。

```text
[memory] USER [61/1200]
写 CLI 优先 Go；不经允许不提交。

PROJECT [24/2500]
预发库只读。

Recent: 2026-10-10 发票迁到 v2 接口；待办：TestRetry 偶发失败

[cached 2h ago] Machine: linux 24.04/amd64; CPU AMD EPYC 7763 (8 cores); RAM 31.3GB; disk free 212.4GB; shell /bin/bash; ssh; go 1.26.5; node 24.13.0; python3 3.12.3; git 2.43.0; docker 29.2.0; ffmpeg 8.1; claude 2.1.280; codex 0.160.0

[live] Project directory: /home/alice/src/billing
Manifests: go.mod, Makefile
[cached 0h ago] branch main, 3 uncommitted; last commit: add invoice v2 client; make: build test lint

[cached 2h ago] Skills (details: aibase skill show <name>): release-notes: Draft release notes from merged PRs; video-trim: Cut and compress clips with ffmpeg

[cached 0h ago] Gaps: client.gemini: detected; integration missing → aibase install gemini --dry-run
```

整份简报就这么多，几百个 token，大小有硬上限。其余信息一次工具调用就能拿到。

## 安装

**macOS / Linux / 任意服务器**

```sh
curl -fsSL https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.sh | sh
```

**Windows（PowerShell）**

```powershell
irm https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.ps1 | iex
```

**从源码装**（Go 1.26+）

```sh
git clone https://github.com/xiaoxin031/aibase && cd aibase && ./install.sh
```

安装脚本会先校验 SHA-256，再把 `aibase` 放进 PATH，然后执行 `aibase setup`。setup 会：
1. 找出本机所有 AI 客户端，逐个接入，改配置前先带时间戳备份
2. 预热缓存
3. 打印一份体检结果

**装完照常打开你的 AI 工具就行，不需要任何配置。** 以后新装了 AI 客户端，再跑一次 `aibase setup` 即可，重复执行不会出问题。

## 为什么用

| 没有 aibase | 有了 aibase |
|---|---|
| 每次会话都重新摸系统、工具链、仓库 | 会话开始自动注入简报，大小有硬上限 |
| 每个 AI 各记各的，或者干脆不记 | Claude、Codex、Gemini、Cursor、opencode 共用一份记忆 |
| 旧笔记被当成当前事实 | 每条事实都标明是 `[live]` 实时、`[cached Xh ago]` 缓存还是 `[memory]` 记忆 |
| "装没装 Java？""那个服务在哪个端口？" | `inspect host` 直接给出解析好的工具链、端口、服务、容器 |
| "那个仓库放哪了？" | `aibase projects billing`，约 0.1 秒全盘找到 |
| 密钥混进提示词 | 所有输出先脱敏，凭据库一律不读 |
| 好用的流程，会话一结束就丢了 | 按 Agent Skills（`SKILL.md`）标准发现、校验技能，各客户端共用 |

## 它知道什么

- **机器**：
  - 系统与版本、架构、CPU、内存、磁盘、GPU、shell
  - 当前是否 SSH 会话、容器、WSL 或虚拟机，是否 root/管理员
  - 包管理器、各语言运行时、docker/kubectl、媒体工具（ffmpeg、ImageMagick、yt-dlp 等）、已装的 AI 命令行工具
  - 按需才查：监听端口和对应进程、运行中的服务、容器、CPU 占用最高的进程
- **项目**：
  - git 根目录、分支、上游、未提交数、最近提交
  - 清单文件：go.mod、package.json 脚本、Makefile 目标、pom.xml、Cargo.toml、pyproject、Dockerfile、compose
  - 语言构成，有没有 AGENTS.md 或 CLAUDE.md
  - 项目里声明的命令**只展示，绝不执行**
- **记忆**：
  - 有大小上限的三层：用户画像、全局事实、项目事实，另有可搜索的笔记和会话日志
  - 会话结束时提醒 AI 把值得留的存下来，所以记忆会自己长，但不会无限膨胀
- **技能**：
  - 采用开放的 [Agent Skills](https://agentskills.io) 标准，统一放在 `~/.agents/skills`
  - 逐个检查技能依赖的工具本机有没有；不原生读这个目录的客户端，会帮它同步过去
  - AI 可以起草新技能，草稿要你 `promote` 之后才生效
- **缺口**：`aibase doctor` 列出还缺什么，每项都附修复命令，比如：
  - 哪个客户端还没接入
  - 项目需要 `mvn`，本机却没装
  - 某个技能缺工具
  - 记忆快满了
  - 笔记里藏着密钥

## 支持的客户端

| 客户端 | 接入方式 |
|---|---|
| Claude Code | `SessionStart` + `Stop` 钩子，`/save` 命令 |
| Codex CLI | `SessionStart` + `Stop` 钩子，`save` 技能（Codex 要求在 `/hooks` 里信任一次） |
| Gemini CLI | `SessionStart` + `AfterAgent` 钩子 |
| Cursor | `sessionStart` + `stop` 钩子，另给 `cursor-agent` 配 MCP |
| opencode | 本地 MCP 服务 + 全局 `AGENTS.md`（JSONC 原地编辑，保留注释） |
| 其他任意客户端 | `aibase mcp`（stdio MCP），或把 `aibase install --print-agents-md` 的输出贴进 AGENTS.md |

接入跟模型无关。Claude、GPT、DeepSeek、Grok、Gemini、Qwen 或本地模型，通过你用的客户端拿到的是同一份上下文。

## 快、小、稳

| | |
|---|---|
| 体积 | 约 4MB，静态编译，`CGO_ENABLED=0`，只用 Go 标准库 |
| 会话简报（热缓存） | P95：macOS 约 9ms，Alpine/Debian 容器约 7ms |
| 完整机器探测 | 总预算 1.5 秒，慢的探测项返回 `timeout`，不会卡住 |
| 后台进程 | 没有。CLI 用完就退，MCP 跟着客户端会话走 |
| 网络 | 不联网，不收集任何使用数据 |
| 平台 | macOS、Linux（含 Alpine/musl、容器、WSL）、Windows，amd64 和 arm64 都支持 |

缓存放在一个脱离父进程的一次性子进程里刷新，所以会话永远不用等慢吞吞的 `system_profiler`，也不会被休眠的磁盘拖住。

## 安全

- **只读**：aibase 只负责让 AI 看清楚，真要动手，还是归客户端自己的权限系统管。
- **处处脱敏**：任何内容离开进程之前，下面这些都会被遮掉：
  - API 密钥：`sk-…`、`AKIA…`、`ghp_…`、`glpat-…`、`xox…`、`AIza…`
  - JWT、PEM 私钥、URL 里的密码、Bearer 令牌
  - `SECRET=…` 这类赋值
- **绝不读取**：`~/.ssh`、钥匙串和凭据管理器、浏览器配置、云凭据文件、`.env` 的值。只报告它们*存在*。
- **可审计**：`aibase audit` 能看到每次注入和每次 MCP 调用：时间、客户端、字节数、脱敏了几处。
- **可还原**：改任何配置都先带时间戳备份；`--dry-run` 先看会改什么；`aibase uninstall` 一键还原。
- **数据归你**：都在 `~/.aibase`（权限 0700）。要分享之前，先用 `aibase redact scan` 查一遍有没有密钥。

> aibase 只能约束经过它的那部分操作。AI 客户端自己的终端权限，由客户端和操作系统控制。

## 命令

```text
开始使用   setup · doctor · version
上下文     context · inspect host [--level basic|tools|runtime] · inspect project · projects [词] · refresh
记忆       ls · add · rm · save · log · note · show · search · import · migrate amem
技能       skill ls | show | new [--draft] | check | sync | promote
接入       install [客户端] [--dry-run] · uninstall [客户端] · mcp · hook
维护       audit · redact scan [路径]
```

所有命令都支持 `--json`。

**MCP 工具：**`context`、`host_inspect`、`project_inspect`、`projects_find`、`skills_list`、`skill_show`、`doctor`、`memory_load`、`memory_save`、`memory_log`、`memory_search`、`memory_show`、`memory_note`。

## 可选托盘

`aibase-tray` 是放在菜单栏或系统托盘里的小图标。
- 能看到：各客户端接入状态、记忆用量、最近会话、doctor 结果
- 能操作：暂停注入、开关自动保存

它是单独的程序，只调用 `aibase … --json`，服务器上用不着。

```sh
curl -fsSL https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.sh | AIBASE_TRAY=1 sh   # macOS / Linux
$env:AIBASE_TRAY='1'; irm https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.ps1 | iex   # Windows
```

怎么启动：
- Linux：运行 `aibase-tray`，需要桌面有 StatusNotifier/AppIndicator 托盘
- Windows：运行 `aibase-tray.exe`
- macOS：运行 `open ~/.local/bin/Aibase.app`，目前还没做苹果公证

## 安装选项

| 环境变量 | 作用 |
|---|---|
| `AIBASE_VERSION=v0.1.0` | 安装指定版本（默认最新） |
| `AIBASE_BIN_DIR=/path` | 安装位置（默认 `~/.local/bin`；root 是 `/usr/local/bin`；Windows 是 `%LOCALAPPDATA%\Programs\aibase`） |
| `AIBASE_TRAY=1` | 顺带装托盘 |
| `AIBASE_FROM_SOURCE=1` | 用本机 Go 编译，不下载 |
| `AIBASE_NO_SETUP=1` | 只装二进制，不接入客户端 |
| `AIBASE_DRY_RUN=1` | 只打印计划，不做任何改动 |

## 常见问题

**这不就是又一个智能体？** 不是。Claude Code、Codex 本身已经很强了，它们缺的是上下文，aibase 补的就是这个。

**会不会把提示词撑爆？** 不会。不管历史攒了多少，简报都有硬上限：默认 6000 字符，可以用 `brief_limit` 调。细节只在 AI 主动要时才给。

**之前用 amem？** `aibase setup` 会把 `~/.amem` 复制到 `~/.aibase`，原目录不动，同时换掉旧的钩子。

**怎么卸载？** 先跑 `aibase uninstall`，再删掉二进制和 `~/.aibase`。

## 许可

[MIT](LICENSE)
