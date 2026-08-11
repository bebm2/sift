---
status: active
created: 2026-08-11
---

# 易用性打磨（UX Ergonomics）规划

## 目标（北极星）

**初学者不看手册也能用起来**：`安装 → sift init 向导 → 绑定项目 → 起 daemon → 触发 Issue`，全程交互式引导 + 中文语义化输出 + 合理默认。

## 现状证据（实测）

- `sift`（无参数）→ 单行 cryptic `usage:`，无引导。
- `sift --help` → `unknown command "--help"`（**没有 help**）。
- `sift ps` / `sift doctor` → 全是 JSON 信封（`protocol_major`/`ok`/`result`），无人话。
- 无 `init` 向导；绑定项目只能手编 `~/.sift/config.yaml`。
- 依赖极简（x/sys、yaml.v3、sqlite），**无 TUI/表格库**。

## 关键设计原则

1. **协议不动，展示层叠加**：JSON 信封是 client↔daemon RPC 协议（`protocol_major`/`ok`/`result`），**不改**。「人话化」是在 RPC `result` 上叠一层渲染；`--json` / `SIFT_JSON=1` 保留原始 dump 供脚本。零协议破坏、零行为破坏。
2. **默认人话（中文），机器用 `--json`**：默认中文语义化输出（✓/⚠/✗、分节、表格）；英文经 `SIFT_LANG=en`（可后续，V1 先中文）；机器消费一律 `--json`。
3. **零新依赖**：渲染与交互全部 stdlib（`bufio` 读入、`os.Stdout.Stat()` 探 tty、手动表格）。不引 TUI/prompt 库。
4. **向导幂等 + 非交互 fallback**：`sift init` 可重跑（编辑/新增），提供 flags 供脚本；写 config 前备份、`chmod 600`。
5. **不弱化既有断言**：CLI 输出测试以 `--json` 路径为准（协议面），人话渲染新增独立测试。

## 分片（每片 = issue → worker → 审修闭环）

| 片 | 内容 | 模型建议 |
|----|------|---------|
| **ux-1** | `internal/cli/render` 渲染包（tty 检测、状态图标、分节、表格，stdlib）+ `sift help`/`--help`/`-h`/`sift help <cmd>` 中文命令参考 + `sift` 无参数→友好概览（版本/配置在否/下一步）+ `doctor`/`doctor --offline` 人话化（`--json` 保留） | gpt-5.6-luna |
| **ux-2** | 查询命令人话化：`ps`/`timeline`/`logs`（用 render 包，`--json` 各留） | deepseek-v4-flash |
| **ux-3** | 查询命令人话化续：`metrics`/`worktree`/`kill`/`retry`/`report`/`attach` | deepseek-v4-flash |
| **ux-4** | **`sift init` 交互向导** + `sift project add` / `sift agent add`：探测 forge CLI+登录态（`gh auth status`）、探测已装 agent（claude/codex/cursor/pi 可执行）、引导绑定仓库（`git remote` 自动探测 owner/repo）、原子写 config（备份+chmod 600）、幂等、flags 非交互。**末尾提供「安装后台服务并现在启动？」** | gpt-5.6-terra（引擎/深推理） |
| **ux-5** | 友好错误（可行动下一步，不甩 JSON/stack）+ `daemon` 启动/状态人话 + README/getting-started 与新 UX 对齐（协调 #915） | gpt-5.6-sol（文档）+ luna（代码） |
| **ux-6** | **服务生命周期**：`sift service start\|stop\|reload`（per-backend：launchd kickstart/bootout、systemd start/stop；reload V1=重启，诚实标注、SIGHUP 真热重载留后续）；`sift service install` **一致自启**（systemd 升级为 `enable --now`，不只 daemon-reload）；`sift service status` 人话化。安装器不自动起（未配置无意义、curl\|bash 非交互）→ 由 `sift init` 引导启动 | gpt-5.6-terra（launchd/systemd 语义） |

依赖：ux-1 先行（渲染包是 ux-2/ux-3 的地基）；ux-4/ux-6 均改 `cmd/sift/main.go`，与 ux-2/ux-3 **串行**避免冲突（ux-1 合后逐片推进）；ux-5 收尾。优先级：ux-6（用户当下所需）紧随 ux-1。

## 非目标

- 不改 control-plane RPC 协议/schema（V14/V15 不变）。
- 不引第三方 TUI/prompt/table 依赖。
- 不做 Web/GUI（保持零界面公理）；不做完整 i18n 框架（V1 中文默认 + `SIFT_LANG` 钩子即可）。
- 不动 daemon 内部逻辑/调度/门禁（纯 CLI 展示 + 向导写配置）。

## 验收

- 每片：`--json` 输出与现状**逐字节一致**（协议面回归）；人话默认输出中文、含状态图标、tty 无色码；`go build ./...` + 相关测试过。
- ux-4：`sift init` 全新机器上交互跑通（探测→绑定→写 config→`sift doctor` 人话确认）；flags 非交互可脚本化；重跑幂等。
- 全程：初学者按 README 不看 specs 即可完成 安装→init→触发 首 Issue。