---
status: active
created: 2026-08-13
---

# CLI 易用性审查（初学者视角 + 借鉴优秀开源 CLI）

## 方法

1. 沙箱 fresh-home 实跑每个命令（`scripts/dev/with-sandbox.sh --build`），记录初学者实际所见。
2. 全文读 README.md。
3. 对照 gh / rustup / uv / mise / bun / cargo / kubectl / docker / git 的 CLI 最佳实践。

## 核心发现：`--help` 是最大发现性灾难

`--help`/`-h` 是所有 CLI 用户的肌肉记忆。当前**几乎每个命令都坏**：

| 命令 | `--help` 实际行为 | 应有 |
|------|------------------|------|
| `sift ps --help` | ❌ 去连 daemon 报 "daemon unavailable" | 显帮助 |
| `sift logs --help` | ❌ 同上（连 daemon） | 显帮助 |
| `sift timeline --help` | ⚠️ 原始 Go flag.Usage（无中文/无示例） | 中文帮助+示例 |
| `sift metrics --help` | ⚠️ 原始 Go flag.Usage | 中文帮助+示例 |
| `sift init --help` | ⚠️ 原始 Go flag.Usage（"Usage of setup:"） | 中文帮助+示例 |
| `sift service --help` | ❌ "unknown service action --help" | 显子命令帮助 |
| `sift update --help` | ✅ 自定义 usage（OK） | — |
| `sift doctor --help` | ✅ 自定义 usage（OK） | — |

根因：`--help`/`-h` 没有在分发前**全局拦截**，落到各命令自己的 flag 解析或 daemon 调用。这是 Tier-1 必修。

## 其他摩擦点

- **缺 `sift version` 子命令**（只有 `--version`）；且不报"是否有新版"。
- **缺 shell completion**（`sift completion bash|zsh`）。gh/kubectl/rustup 都有——Tab 补全是发现性 + 速度的最大杠杆。
- **缺 `sift status`**：初学者要一个"一切 OK 吗？"的一眼总览（daemon 在跑？config 合法？版本最新？几个项目？）。
- **project/agent 只有 `add`，无 `list`/`remove`**：初学者配完没法用 CLI 看/删，只能手编 YAML。缺 CRUD 动词。
- **各命令帮助无 Examples 段**（gh/cargo 标配：每命令 2-3 个示例）。
- **daemon-down 错误不够上下文化**：config 在时应提示 `sift daemon`，无 config 才提示 `sift init`。
- **无全局 `-v`/`-q`**；长操作（update 下载）无进度指示。
- **README**：好（7 步齐全），缺①「常用命令速查」表 ② completion 安装段 ③ bootstrap 说明（curl\|bash 一次 → 之后 `sift update`）。LICENSE 文件缺失（README 注"待创建"）。

## 借鉴的优秀实践

| 来源 | 借鉴点 |
|------|--------|
| **gh** | 每命令 `--help` 含 Examples；动作后建议下一步；`completion -s bash`；交互默认值 `[括号]` |
| **rustup/uv/mise/bun** | 自更新（已有 `sift update` ✓）；`completions` 子命令；清晰的 "add to PATH" 提示 |
| **cargo** | `--list` 列全部命令；每子命令 Examples；一致动词（build/test/run） |
| **kubectl/docker** | `-o json/wide`（已有 `--json` ✓）；资源动词 get/describe；`completion` |
| **git** | `status` 给建议（"use git add to track…"）；`help -a`/`help <cmd>` |

## 改进规划（按初学者影响分层）

### Tier 1 — 发现性（最大杠杆，先做）
1. **全局一致的 `--help`/`-h`/`help [cmd]`**：分发前拦截，每命令出中文一句话 + flags + 2 示例。`sift help ps` == `sift ps --help`。
2. **`sift completion bash|zsh|fish`**：生成补全脚本；README 加安装段。Tab 补全命令/flags/project-id。
3. **`sift status`**：一眼总览（daemon 运行?+PID、config 合法?、版本 current vs latest、N 项目）。

### Tier 2 — 资源模型 + 动词
4. **`sift project list`/`remove`、`sift agent list`/`remove`**（add 已有）→ 完整 CRUD。
5. **`sift version`** 子命令（= `--version` + 报 latest + 是否最新）。
6. **顶层 help 加「常用流程」示例段**（init→daemon→trigger→observe）。

### Tier 3 — 打磨
7. 上下文化 daemon-down 错误（config 在→`sift daemon`；无→`sift init`）。
8. 全局 `-v`/`-q`；长操作进度（update 下载）。
9. `--dry-run`（init/project add/kill）。
10. LICENSE 文件补上。

## 非目标
- 不改 RPC 协议/schema/daemon/调度。
- 不做 Web/GUI（零界面公理）。
- 不引第三方 CLI 框架（保持 stdlib；completion 脚本手生成或用 Go 内置 `flag` 扩展）。