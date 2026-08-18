---
status: draft
created: 2026-08-18
summary: bebm2 三条 issue 合入 #1024 后的后续切片
---

# bebm2 issue 后续规划

> #1024 主路径已合入本地 `main`（`fd6626c`）。本文只规划下一步，不改协议范围。

## 现状

| Issue | 状态 | 判断 |
|---|---|---|
| #1024 中转 API | 本地已实现，未推远程 | Claude 不用手写 wrapper；CC Switch 改 settings.json 下次派工生效 |
| #1022 macOS 进程身份 | 未做 | Darwin 仍 fail-closed，`/sift retry` 会在同一点再卡 |
| #1023 Brain 用 Codex | 未做 | Brain 协议仍只能 `claude-json-v1` |

## 建议顺序

先收口同一条产品线（#1024 还没推、Codex 切换还有洞），再单独立项另外两条。不要把 #1022/#1023 塞进同一个 PR。

### 切片 A（立刻，同一 PR 或紧随 #1024）

1. **把 `fd6626c` 推成 PR 并关 #1024 的「不用 wrapper」部分**  
   在 issue 里写清：Claude 中转已通；Codex toml 另开；不要宣称三条 issue 都修了。

2. **Codex 直播配置（CC Switch 对称）——暂缓**  
   等用户反馈再做。现在只读 `.json` 的 `env`；init 冻过 `OPENAI_*` 时 CC Switch 切不动 Codex。

3. **向用户说清楚**  
   `sift init` 收尾或 getting-started 补一句：换中转用 CC Switch 后不用重跑 init（Claude / 补齐后的 Codex）；正在跑的任务不会中途变。

### 切片 B（#1022，独立，先调研再写代码）

根因在 `internal/runtime/termination_nonlinux.go`：非 Linux 没有 PID/启动时刻/可执行文件证明，身份未知必须 fail-closed。这不是 init 环境问题。

候选（只能选一条做 spike，再 ADR）：

- Darwin `proc_pidinfo` 补齐 `ProcessStartedAtMS` + executable，达到与 Linux 同级证明；或
- macOS 默认改走 tmux 后端（若现有身份证明对 tmux 已够用）；或
- 产品层：macOS + process 明确提示改用 tmux / 接受冻结，不再让 `/sift retry` 看起来像能修好。

**不做**：放宽 fail-closed。那是安全契约，不是 bug。

验收：macOS process 后端一次成功 launch 后，supervisor 不再因「身份未知」冻 worktree；retry 不再在同一点空转。没有 Darwin 真机证据不得关 #1022。

### 切片 C（#1023，独立，规格先行）

用户要的是 Brain（T1/T2 分拣）走 Codex，不是把 Codex 登记成干活 Agent（那已经可以）。

`config.md` §3.4 / `brain.md` §4 把 V0 `protocol` 钉死为 `claude-json-v1`。加 Codex 等于新协议值 + 新 envelope 适配，不是换个 executable。

先写规格再改 enum：

- Codex 非交互输出如何对应现有 T1–T7 触点 closed schema；
- 失败/重试/token 统计与 `claude-json-v1` 对齐或显式差异；
- 是否允许同一 daemon 上 Brain=Codex、Agent=Claude（或反过来）。

没有规格就改 `BrainProtocol` 会冲 V0 闭合契约。短期缓解（不关 #1023）：Brain 继续用 Pi/Claude，Agent 用 Codex。

## 明确不做（本轮）

- 守护进程继承用户终端环境，或热加载 `config.yaml`
- 解析 Cursor / OpenCode / Pi 的非 JSON 配置（没有第二家真实需求）
- init 向导里问 `model`/`thinking`（手改 config 可用，不是 #1024 阻塞）
- 把 #1022 或 #1023 标成已修复
