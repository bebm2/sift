---
status: active
created: 2026-08-04
last_updated: 2026-08-17
summary: Sift 总体计划执行情况。工作包分解见 WBS.md。
---

# Sift — 总体计划执行情况

> **文档分工**：本文跟踪**总体计划的执行**(里程碑状态、门禁裁决、整体进度、遗留与下一步)。**任务/工作包分解见 [`WBS.md`](WBS.md)**。需求语义见 [PRD](PRD.md)，结构理由见 [DESIGN](DESIGN.md)/ADR，字段契约见 `specs/`，执行步骤见 `plans/`。

## 里程碑执行状态

| 里程碑 | 状态 | 门禁裁决 | 产出要点 |
|---|---|---|---|
| M1 骨架 | ✅ 完成 | PASS WITH NOTES | Go/SQLite、状态机、outbox、控制面、配置、Brain 壳/T1/T2、fake 骨架链 |
| M2 Forge/Intake | ✅ 完成 | PASS WITH NOTES | GitHub/GitLab 适配、Intake、actor 闸门、API 预算 |
| M3 Runtime | ✅ 完成 | PASS WITH NOTES | process backend、wrapper handoff、恢复、泛型 Interrupt 发射核心 |
| M4 Gate/Shadow/Ledger/回放 | ✅ 完成 | PASS WITH NOTES | 有效策略、Gate/Shadow、Ledger/认证、回放、Change 创建 |
| M5 Attention/Command/Report/Brain/指标 | ✅ 完成 | PASS WITH NOTES | Interrupt 全功能、Command、Report、Channel、九项指标 |
| M6 tmux + 完整故障矩阵 | ✅ 完成 | PASS WITH NOTES | tmux 第二后端、PTY、V2/V4 双后端全矩阵、阶段门归档 |
| **M7 真实 Agent + PoC 取证** | 🔬 **PoC 已验证** | — | **Pi Brain+Agent 双 forge 端到端跑通** |
| **M8 发布** | 🔄 **自动化核心完成** | — | §8.1–8.4 合入 main(#907–#910)；**Release 已迭代至 v0.6.8**（初跑旅程、doctor 稳定性与持续编排迭代线，`curl\|bash` 一键安装实测通过 #913/#914）。A10 干净机 + live 跨版本升级 = 人工门禁，待 M7 通过后 |

## M7 PoC 验证成果(本轮)

**Pi 作为 Brain(T1/T2 分类)+ Agent(编码执行)在 Sift 下端到端跑通**，双 forge(GitHub + GitLab):

| 步骤 | GitHub | GitLab |
|---|---|---|
| issue 发现(trigger label) | ✅ | ✅ |
| Brain T1(Pi: triage) | ✅ valid | ✅ valid |
| Brain T2(Pi: agent 分配) | ✅ valid → pi-coder | ✅ valid → pi-coder |
| launch dispatch(qualification + wrapper spawn) | ✅ | ✅ |
| wrapper handoff(claim.acquire → permit → started) | ✅ | ✅ |
| Pi Agent 实际编码(task.json → result) | ✅ | ✅ |
| Gate 评估 | ✅ waiting_human | ✅ running |

### 本轮发现并修的 8 个生产 bug

| # | bug | 根因 |
|---|---|---|
| 1 | forge pathPart `%2F` | GitHub `/` 被整体编码 → API 404 |
| 2 | T2 + launch 不 wire | production daemon 缺 T2 调用 + launch enqueue |
| 3 | launch op 不 complete | 无 CompleteOutboxAttempt → 重试风暴 |
| 4 | GitLab labels 格式 | GitLab `["sift"]` vs GitHub `[{"name":"sift"}]` |
| 5 | ErrNoRows 中断 tick | FindPendingIntake 无行 error → 后续 project starve |
| 6 | brain envelope 字段名 | `result_text` 非 `result`(pi-brain-wrapper.py 适配) |
| 7 | qualification 空 env | bun 脚本需 PATH 里找 node(pi-run.sh 适配) |
| 8 | run.sock 路径 | 3x vs 4x filepath.Dir，wrapper 永远找不到 daemon |

### 结构优化(本轮)

| 项 | PR | 内容 |
|---|---|---|
| siftd+sift 合并 | #900 | 3→2 二进制 |
| storage 6 域拆分 | #891-#897 | 全 <500 行 |
| config/forge 拆分 | #885 | <500 行 |
| 类型 dispatch 策略化 | #899 | lookup table |
| 代码质量评审 | #890 | deadcode 4 移除 + findings 报告 |
| 兼容层清理 | #898 | WorktreeManager/ReconcilerScheduler |
| SQL 中心化评估 | #880 | 评估完毕，无安全提取项 |
| 文档规范 | #888 | STATUS.md(执行)+ WBS(纯分解) |

## 初学者旅程优化（本轮，2026-08-14）

围绕「从接触到用起来」的初学者旅程收敛命令面（基于 #947/#955/#957 onboarding 主线后的第二轮 UX 打磨）：

| # | 内容 | 状态 | PR |
|---|---|---|---|
| #960 | init 依赖引导：gh/glab 三态诊断 + 确认式安装 + auth 内嵌 + pi 默认引导 | ✅ 合并 | #969 |
| #961 | init 收尾三合一：doctor 自检 / service 安装 / trigger label 创建 + 轮询预期 | ✅ 合并 | #970 |
| #962 | `sift pi` 会话入口：注入 Sift 操作 skill（go:embed 分发、只读/副作用/红线分级） | ✅ 合并 | #972 |
| #963 | `sift issue` 语义入口（v1 只读） | 📌 P3 待启动 | — |

首跑路径收敛为：`install` → `sift init`（依赖引导+收尾三合一）→ 打标签 → 可选 `sift pi` 会话探索。详见 [getting-started](../docs/guides/getting-started.md)（§7.5 会话式探索）。

## 技术债清理（本轮，2026-08-14）

扫描并处理 [#927](https://github.com/xsift/sift/issues/927) backlog 及流程残留：

| # | 债 | 处理 | PR |
|---|---|---|---|
| #973 | 依赖引导测试 CI hermeticity（ubuntu gh 在 /usr/bin） | 隔离修复（遗留未闭合收尾） | #974 |
| #976 | init `--agent-args` trim + interactive 判定 | 修复 + 测试 | #979 |
| #977 | 写 os.Executable 目录的冗余 flaky 测试 | 删除（runtime 包等价覆盖） | #978 |
| #980 | 测试防覆盖真实 launchd plist（P0 安全） | 隔离修复（遗留未闭合收尾） | #982 |
| #927 五项 | config 数值漂移 / doctor 渲染噪音 / staticcheck CI / 安装器 rc+chmod / tmux flake 超时 | 一次处理 | #981 |

另：补存 #913/#915 审核存档（早期 review 分支未合入 main 的遗漏，PR #975）；清理 7 个残留 worktree 与远端分支。

## 真实首跑验证轮（2026-08-14/15，v0.5.8–v0.5.11）

收尾三合一（#961）合入后，用 v0.5.9 二进制在真实 GitLab（gitlab.hexinfo.cn / platform/hexark）跑全流程，暴露的缺陷逐个修复并当日发版（v0.5.9→v0.5.11）：

| # | 内容 | 状态 | PR/Release |
|---|---|---|---|
| #986 | 收尾幂等重跑 + glab label create 参数式语法（`-n/-c/-R`） | ✅ 合并 | #988（v0.5.9） |
| #967 | launchctl stub 退出码对齐 + unloaded 分类收敛 | ✅ 合并 | #984（v0.5.9） |
| — | 收尾 doctor 自检按严重度分组输出（#961 体验补强） | ✅ 合并 | #989（v0.5.9）；getting-started 同步 #990 |
| #987 | glab label 颜色 `#` 前缀 + 查重 tab 表格匹配（真机复现 409/400） | ✅ 合并 | #991（v0.5.10），真机闭环验证 |
| #992 | Agent 选择拒绝词 `n/no/N` 不再注册假 agent（真机复现 config 污染） | ✅ 合并 | #991（v0.5.10）；P2（管道提示错位）转 #927 |
| — | label 查重前置不扰民（已存在静默跳过、两次确认收敛为一次）+ doctor 指引人话化 + `sift pi` TTY 修复 | ✅ 合并 | #994（v0.5.11） |
| #993 | Agent launch env（HOME/PATH）在 init 资格测试时冻结，不经 shell wrapper | ✅ 合并 | #995（v0.5.11）+ schema 再生成 |

## 近期稳定性与技术债收敛（2026-08-17）

- **doctor 慢/超时与呈现**：#1007/#1008 先后关闭 poller 与 reconciler 的限流重试风暴；#1009 仅为 `ops.doctor` 扩展端到端 RPC 预算；#1011、#1014、#1015 将独立 CLI/项目探活并行、加入 `stage_ms` 诊断，并将 SQLite 读检查保持有序以避免伪 `database is locked`。#1020 将默认输出收敛为当前项目/其他项目/运行环境摘要，`--details`/`--debug` 分层，并在 daemon 结果源头剥离 Forge auth 原文、token scope、endpoint 与多行 probe 输出。真实 macOS 在线 doctor 已恢复为约 2–3 秒；排障入口见 [troubleshooting](runbooks/troubleshooting.md#3-doctor-报告)。
- **`sift issue` 只读边界**：#1013 已合入 `list`/`ls` 和写动词拒绝；#927 中的引号写动词绕过已修复并由测试钉住。混合语义 `list --all <question>` 仍未定义，保持问答路径而非猜测。
- **#927 consolidated backlog**：静态检查已入 CI；已移除 os.Executable 测试污染并缓解 tmux 时序。#1017 关闭 `sift issue` 引号写动词绕过；#1018 让 init 明示当前项目，并把其他已登记项目的 doctor 问题带路径/显式 remove 指引呈现，避免 error 后误称“全部就绪”；本轮让管道输入的 init closeout prompt 自行换行，不再与下一条输出挤行。仅保留低优先级的安装器矩阵/fish 兼容、macOS 进程/tmux flaky 的持续观察；按价值另拆，不与 M7 门禁混用。
- **#883 profile**：仍严格绑定 M7 的 ≥3 并行真实 Run；无该负载不得凭本机 doctor 数据声称完成 profile 或 P50 门禁。

## 遗留 / 延期

- **M7 完整门禁**:≥3 并行 Run + P50<60s 测量、手机端审批证据、凭证存储 spike
- **#883 性能 profile**:M7 真实负载绑定（应随 M7 并行 Run 片一并做，不再单独排期）
- **#963 `sift issue` 语义入口 v1（只读）**:依赖 #960/#961/#962 均已闭合，已解锁待启动
- **#927 backlog**:安装器矩阵/fish 兼容及 macOS tmux/进程 flaky 持续观察；详见上节，不作为 M7 门禁替代
- **wrapper handoff 精调**:`waiting_human` 上的 `kill`/`retry`/`approve` 操作验证
- **M8 A10 干净机验收**:干净 macOS + systemd Linux 从发布归档安装跑通 + 四组合冒烟证据(人工门禁,待 M7)
- **M8 live 跨版本升级**:不丢 DB 状态 + 较新 schema 拒旧 daemon(待 M7 真实数据;契约级回归已随 #903)
- **M8 backlog**:✅ `curl\|bash` 一键安装已上线（Release v0.1.0，#913/#914）；Homebrew tap 实际发布为可选（formula 草稿已在 #905，需时再推 tap repo）。✅ 其余 M8 P2(doctor `checks` id 升序 F4、GoReleaser v2 命令、退出码去重)已随 #911/#912 关闭。安装器 F6(缺 manifest 成员 fail-open, P2)留 backlog。
- ✅ **main CI 曾为红**(8bb8b93 run.sock 4x-Dir 改了生产布局但遗留 5 个夹具在旧布局,致 wrapper/crash-harness 套件 CI 失败)→ #903 `d2f6aef` 修复,main 现已绿

## 下一步

1. **M7 剩余验收**:人工前置(并行 Run 环境、手机设备、凭证策略)——这是版本退出的关键 Human Gate
2. **M8 A10 干净机验收**:M7 通过后,从发布归档在干净 macOS/systemd-Linux 安装跑通(§8.3)
3. M7+A10 满足后进入【7 完成】:核对 PRD/WBS 退出条件,输出发布建议
