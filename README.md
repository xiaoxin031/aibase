<div align="center">

# aibase

**The local context layer for every AI coding agent.**

One tiny binary. Your AI already knows this machine, this project, and what you did last time.

[![ci](https://github.com/xiaoxin031/aibase/actions/workflows/ci.yml/badge.svg)](https://github.com/xiaoxin031/aibase/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/xiaoxin031/aibase?color=36c)](https://github.com/xiaoxin031/aibase/releases)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)
![platforms](https://img.shields.io/badge/macOS%20%7C%20Linux%20%7C%20Windows-amd64%20%7C%20arm64-555)

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

---

Every new AI session starts from zero. Claude Code, Codex, Gemini CLI, Cursor, opencode: each one re-runs `uname`, re-reads your `package.json`, re-discovers that Java isn't installed, and asks you again how you like things done. On a server it's worse.

**aibase fixes the context problem, not the agent.** It is not another agent. It's a ~4 MB, zero-dependency binary that hands any agent a short, accurate, redacted briefing the moment a session starts, plus tools to dig deeper on demand.

```text
[memory] USER [61/1200]
Prefers Go for CLIs; never commit without asking.

PROJECT [24/2500]
Staging DB is read-only.

Recent: 2026-10-10 migrated invoices to v2 API; TODO flaky TestRetry

[cached 2h ago] Machine: linux 24.04/amd64; CPU AMD EPYC 7763 (8 cores); RAM 31.3GB; disk free 212.4GB; shell /bin/bash; ssh; go 1.26.5; node 24.13.0; python3 3.12.3; git 2.43.0; docker 29.2.0; ffmpeg 8.1; claude 2.1.280; codex 0.160.0

[live] Project directory: /home/alice/src/billing
Manifests: go.mod, Makefile
[cached 0h ago] branch main, 3 uncommitted; last commit: add invoice v2 client; make: build test lint

[cached 2h ago] Skills (details: aibase skill show <name>): release-notes: Draft release notes from merged PRs; video-trim: Cut and compress clips with ffmpeg

[cached 0h ago] Gaps: client.gemini: detected; integration missing → aibase install gemini --dry-run
```

That's the whole brief. A few hundred tokens, with a hard cap on size. Everything else is one tool call away.

## Install

**macOS / Linux / any server**

```sh
curl -fsSL https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.sh | sh
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.ps1 | iex
```

**From source** (Go 1.26+)

```sh
git clone https://github.com/xiaoxin031/aibase && cd aibase && ./install.sh
```

The installer verifies a SHA-256 checksum, puts `aibase` on your PATH, and runs `aibase setup`. Setup detects every AI client you have, wires each one in (with timestamped backups), warms the cache and prints a health check. **Then you just open your AI tool as usual.** There's nothing to configure.

Run `aibase setup` again whenever you install a new AI client. It's idempotent.

## Why aibase

| Without | With aibase |
|---|---|
| Every session re-explores the OS, toolchain and repo | A short briefing is injected at session start, under a hard size cap |
| Each AI tool keeps its own separate memory, or none | One memory shared by Claude, Codex, Gemini, Cursor, opencode… |
| Old notes get treated as current facts | Every fact is labeled `[live]`, `[cached Xh ago]` or `[memory]` |
| "Is Java installed?" "Which port is that service on?" | `inspect host`: toolchain, listeners, services, containers, already parsed |
| "Where is that repo again?" | `aibase projects billing`, which searches your whole disk in ~0.1 s |
| Secrets leak into prompts | Redaction runs on every output; credential stores are never read |
| Reusable workflows are lost after the session | Agent Skills (`SKILL.md`) are discovered, checked and shared across clients |

## What it knows

- **Machine**: OS and version, architecture, CPU, RAM, disk, GPU, shell, SSH/container/WSL/VM, root/admin, package managers, language runtimes, docker/kubectl, media tools (ffmpeg, ImageMagick, yt-dlp…) and installed AI CLIs. On request it also reads listening ports with their process, running services, containers and the top CPU users.
- **Project**: git root, branch, upstream, dirty count, recent commits, manifests (go.mod, package.json scripts, Makefile targets, pom.xml, Cargo.toml, pyproject, Dockerfile, compose), language mix, and whether AGENTS.md or CLAUDE.md exists. Declared commands are **shown, never executed**.
- **Memory**: a size-capped profile, global facts and per-project facts, plus searchable notes and session logs. At the end of a session it asks the agent to save what's worth keeping, so the memory grows on its own and stays bounded.
- **Skills**: the open [Agent Skills](https://agentskills.io) standard in `~/.agents/skills`. aibase checks each skill's dependencies against your machine, and syncs skills to clients that don't read that directory natively. Agents can draft new skills; drafts stay inactive until you `promote` them.
- **Gaps**: `aibase doctor` tells you what's missing, such as an unwired client, a project that needs `mvn` when Maven isn't installed, a skill whose tool is absent, memory near its cap, or a secret sitting in your notes. Each item comes with the exact fix command.

## Works with

| Client | How aibase plugs in |
|---|---|
| Claude Code | `SessionStart` + `Stop` hooks, `/save` command |
| Codex CLI | `SessionStart` + `Stop` hooks, `save` skill; trusted automatically through Codex's own app-server (no `/hooks` step) |
| Gemini CLI | `SessionStart` + `AfterAgent` hooks |
| Cursor | `sessionStart` + `stop` hooks, plus an MCP server for `cursor-agent` |
| opencode | local MCP server + global `AGENTS.md` (JSONC edited in place, comments preserved) |
| Anything else | `aibase mcp` (stdio MCP), or paste `aibase install --print-agents-md` into AGENTS.md |

It's model-agnostic. Claude, GPT, DeepSeek, Grok, Gemini, Qwen or a local model all get the same context through whichever client you use.

## Fast, small, boring

| | |
|---|---|
| Binary | ~4 MB, static, `CGO_ENABLED=0`, Go standard library only |
| Session brief (warm) | P95 ≈ 9 ms on macOS, ≈ 7 ms in Alpine/Debian containers |
| Full machine probe | 1.5 s total budget. Slow probes return `timeout` instead of hanging |
| Background processes | none. The CLI exits when done, and MCP lives only as long as the client session |
| Network | none, and no telemetry |
| Platforms | macOS, Linux (incl. Alpine/musl, containers, WSL), Windows, on amd64 and arm64 |

Cache refreshes happen in a detached one-shot process, so a session never waits on a slow `system_profiler` or a sleeping disk.

## Safe by design

- **Read-only.** aibase helps the AI see. Actions stay under your client's own permission system.
- **Redaction everywhere.** API keys (`sk-…`, `AKIA…`, `ghp_…`, `glpat-…`, `xox…`, `AIza…`), JWTs, PEM private keys, passwords in URLs, bearer tokens and `SECRET=…` style assignments are masked before anything leaves the process.
- **Never reads** `~/.ssh`, keychains or credential managers, browser profiles, cloud credential files or `.env` values. It only reports that they *exist*.
- **Auditable.** `aibase audit` shows every injection and MCP call: when it happened, which client, how many bytes, and how many redactions.
- **Reversible.** Every config change gets a timestamped backup, `--dry-run` shows the change before it's made, and `aibase uninstall` reverts.
- **Your data stays yours** in `~/.aibase` (0700). `aibase redact scan` checks it for secrets before you share anything.

> aibase can only limit what goes *through* aibase. Your AI client's own terminal access is controlled by the client and your OS.

## Commands

```text
Getting started   setup · doctor · version
Context           context · inspect host [--level basic|tools|runtime] · inspect project · projects [query] · refresh
Memory            ls · add · rm · save · log · note · show · search · import · migrate amem
Skills            skill ls | show | new [--draft] | check | sync | promote
Integration       install [client] [--dry-run] · uninstall [client] · mcp · hook
Maintenance       audit · redact scan [path]
```

Every command accepts `--json`.

**MCP tools:** `context`, `host_inspect`, `project_inspect`, `projects_find`, `skills_list`, `skill_show`, `doctor`, `memory_load`, `memory_save`, `memory_log`, `memory_search`, `memory_show`, `memory_note`.

## Optional tray app

`aibase-tray` is a menu-bar / system-tray icon showing wired clients, memory usage, recent sessions and doctor results, with switches to pause injection and toggle auto-save. It's a separate binary that only calls `aibase … --json`, so servers never need it.

```sh
curl -fsSL https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.sh | AIBASE_TRAY=1 sh   # macOS / Linux
$env:AIBASE_TRAY='1'; irm https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.ps1 | iex   # Windows
```

Launch `aibase-tray` (Linux), `aibase-tray.exe` (Windows) or `open ~/.local/bin/Aibase.app` (macOS). Linux needs a StatusNotifier/AppIndicator tray host. The macOS build is not notarized yet.

## Installer options

| Variable | Effect |
|---|---|
| `AIBASE_VERSION=v0.1.0` | install a specific release (default: latest) |
| `AIBASE_BIN_DIR=/path` | install location (default `~/.local/bin`, `/usr/local/bin` for root, `%LOCALAPPDATA%\Programs\aibase` on Windows) |
| `AIBASE_TRAY=1` | also install the tray app |
| `AIBASE_FROM_SOURCE=1` | build with your local Go instead of downloading |
| `AIBASE_NO_SETUP=1` | install the binary only, don't wire clients |
| `AIBASE_DRY_RUN=1` | print the plan and change nothing |

## FAQ

**Is this another agent?** No. Claude Code, Codex and the rest are already excellent agents. What they lack is context, and that's the part aibase supplies.

**Will it bloat my prompts?** No. The brief has a hard cap (default 6000 characters, configurable via `brief_limit`) no matter how much history you accumulate. Details are fetched only when the agent asks.

**Coming from amem?** `aibase setup` copies `~/.amem` into `~/.aibase` (the original stays untouched) and replaces the old hooks.

**Uninstall?** Run `aibase uninstall`, then delete the binary and `~/.aibase`.

## License

[MIT](LICENSE)
