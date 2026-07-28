---
status: active
created: 2026-07-27
summary: Agent 经 wrapper 启动；wrapper 落盘文件是裁定与恢复依据，tmux 只是可选后端
---

# ADR-005 执行后端抽象与 wrapper 契约

> **attempt 启动协议由 [ADR-010](010-attempt-spawn-handoff.md) 修订。** 本 ADR 的 backend / wrapper / 落盘事实源决策继续有效；两次 `claim:confirm` 与 spawn 前置 `running` 的旧形态已失效。

结构展开见 [DESIGN §8.4](../DESIGN.md)、恢复矩阵见 [DESIGN §10.1](../DESIGN.md)。

## 决策

1. 定义 `ExecutionBackend` 抽象。V0 两个实现：**`process`（默认）** 与 **`tmux`（可选的持久宿主：attach 与人工查看）**。
2. **后端只决定 wrapper 跑在哪里，永不插到 wrapper 与 Agent 之间。** 任何后端都只启动 Sift 自带的 wrapper，由 wrapper 再启动真实 Agent；**Agent 恒为 wrapper 的直接子进程且恒在 wrapper 自己的进程组内**，两个后端同拓扑。真 PTY 由 wrapper 自建并中继到 pane 与 `agent.log`，不靠继承 tmux 的 pane tty。

   wrapper 负责 acquire session、写 wrapper 控制身份、取得唯一 spawn permit、spawn 后补写 Agent 身份并上报 started、追加 `agent.log`、更新 heartbeat、转发终止信号与进程组回收、结束时写 `result.json`。控制文件一律「写临时文件 + fsync + rename」。
3. **attempt 生命周期独立于 Run 状态机建模**：`pending / starting / spawning / running / finished / orphaned`。`spawning` 表示 spawn 权已交给某个 wrapper session 且在 owner / 进程组消失前不可换代；它是被观测的执行事实，不裁定 Run 结局。
4. **claim 由 daemon 建立，wrapper 从不写 DB。** 当前启动协议为 operation lease + `claim:acquire` + `claim:permit-spawn` + `claim:started`（ADR-010）。Run `queued → running` 只以已验证的 Agent 身份为依据，不以 wrapper 或 permit 存在为依据。
5. **tmux 不是事实源，也不参与裁定。** 权威日志是 `agent.log`，结构化时间线在 SQLite，完成依据是 `result.json` 加 Gate。会话/pane 存在只是活性线索，不构成启动证据。
6. 进程身份判定**不能只比 PID**：至少组合启动时间、可执行路径与 control nonce；wrapper 与 Agent 身份分开记录，进程组也需防复用。
7. 启动 Agent 只经单一 **launcher 间接层**（V0 为恒等实现），为将来的沙箱留缝（见 [ADR-007](007-tm6-minimal-credential-sandbox-direction.md)）。

## 理由

PRD §10.1 把「杀死 `siftd` 后重启不出现幽灵 running」列为验收标准，PRD §9.3 要求「重启后核对 running 进程 / worktree」。要满足它，必须先回答一个问题：**重启后的 siftd 凭什么知道上一次的 agent 是死是活、干成没干成？**

三个候选答案里只有一个经得起崩溃：

- 靠父子进程句柄——`siftd` 一重启就没了，直接排除；
- 靠 tmux 会话表——会话在不等于 agent 还活着，更不告诉你退出码；且它把恢复正确性押在一个外部工具的状态上；
- **靠 agent 自己落盘的控制文件**——崩溃后仍在磁盘上，包含退出码与完成证据。

所以 wrapper 不是为了方便，是**恢复矩阵的数据来源**。它同时白拿三样东西：`sift logs` 不依赖 tmux scrollback、信号能按进程组回收（避免 agent 派生的子进程变孤儿）、`result.json` 给出 Layer 2 裁定所需的退出码与 head SHA。

tmux 降为可选后端而非主路径，是因为它提供的 `attach` 与「守护进程重启后仍有可见宿主」都是**便利**，把它提到裁定依据的位置则是**把权威交给外部工具**。

**保留它的理由已经改写。** 原稿写的是「某些 agent 的非交互模式不稳定，需要真 PTY」——这条在拓扑裁决后不再成立：PTY 由 wrapper 自建，与后端选择解耦，`process` 后端同样能给 Agent 一个真 PTY。tmux 剩下的价值只有 attach 与持久宿主两项，仍然值得保留一个后端实现，但它不再是「为了 PTY」。

**为什么必须把拓扑裁死（决策 2）**：ADR-010 的整条 `spawning` 证据链只挂在一个观测原语上——「已登记 wrapper 仍活着，或其进程组仍存在」。若 tmux 后端让 Agent 成为 tmux server 的子进程，该原语在这个后端失效（wrapper 崩溃后进程组不存在、Agent 身份缺失，恢复会判 `orphaned` 并新开 attempt，而会话里的 Agent 还活着——双写窗口换个后端复活），决策 2 的「按进程组回收」与启动协议「spawn 到已记录的进程组」也一并不成立。备选方案是引入后端中性的执行句柄（`process` 用 pgid、`tmux` 用会话名 + pane PID），代价是恢复矩阵要维护两套观测语义，其中一套正是决策 5 拒绝依赖的会话表。裁决拓扑更小且更强。

PID 三元组校验防的是一个真实且后果严重的故障：PID 复用后向不相关进程发信号。因此恢复矩阵里明确写下「进程身份无法确认时**不发信号**」，并走受控终止流程的人工分支：冻结该 attempt 并发一次 Interrupt（DESIGN §10.1）——宁可留一个**可见的**待处理项，不要杀掉用户的别的进程，也不要留一个没人知道的停滞。

**claim 与 attempt 阶段是初稿的补漏。** 原稿只覆盖「wrapper 已落盘之后」的恢复，把启动过程当成瞬时的；但启动是一个 outbox 副作用，崩溃可以恰好落在「命令已发出、claim 未确认」或「claim 已确认、进程未起」之间。不把 attempt 生命周期显式建模，这两个窗口在数据模型里根本无法表达，恢复扫描也看不到它们（只扫 `running` 的 Run 时，「仍是 `queued` 但进程已在跑」永远进不了视野）。

**claim 由 daemon 建立是 D0.3 保留下来的正确边界；D0.5 修的是 owner 交接。** 原稿让 wrapper 直取 DB claim，与单写者模型无解；daemon 预建 claim 解决了“谁写 DB”，却没有解决“哪个 wrapper 获得一次性 spawn 权”。ADR-010 因此再加四层：operation lease、wrapper instance/session、唯一 permit、`spawning` 不可换代。fencing generation 仍用于拒绝旧请求，但 OS spawn 不消费 token，故不能单独承担线性化。

bootstrap nonce / wrapper session / run token 分离：前者只完成 acquire，session / permit 只在 wrapper 内存与 daemon DB，run token 才写入 `control.json` 给 Agent。Agent 侧不存在启动凭据。

本决策**曾**是 [ADR-001](001-tech-stack-bun-typescript.md) 敢选 Bun 的前提（监督权威在文件上，不在 runtime 的子进程 API 上）。技术栈改为 Go 之后（[ADR-009](009-tech-stack-go.md)）这条附带作用消失，但**本决策本身不受影响**：wrapper 落盘契约的第一性理由是 `siftd` 重启后必须能判断上一次 agent 是死是活，那与语言和 runtime 质量都无关。区别只在于它不再需要兼任「对冲 runtime bug」这第二份职责。

Go 侧的实现形态：`os/exec` + `SysProcAttr{Setpgid: true}` 建独立进程组，终止时向 `-pgid` 发信号，存活判定用 `syscall.Kill(pid, 0)` 配合进程身份三元组。这些是标准库能力，不构成新的依赖。

## 放弃的选项

| 选项 | 放弃理由 |
|------|---------|
| 裸子进程、无 wrapper | 崩溃后无完成证据；子进程组无人回收；日志靠父进程转发，父进程一死就断 |
| tmux 为唯一后端且作为事实源 | 恢复正确性依赖外部工具的会话表；会话存在 ≠ agent 存活；拿不到退出码 |
| 后端夹在 wrapper 与 Agent 之间（wrapper 请 tmux 起 Agent） | Agent 脱离 wrapper 进程组，`spawning` 证据链与进程组回收在该后端失效；要么补第二套观测原语，要么留一个已知双写窗口 |
| 让 agent 自己上报「我完成了」作为完成依据 | 违反 PRD §5.8：Layer 1 永不越权，声明完成不等于完成 |
| 容器 / 独立用户作为 V0 的默认后端 | 见 ADR-007：V0 不实施沙箱 |
| wrapper 直连 SQLite 自取 claim | 破坏单写者模型，且要把 DB 句柄交给一个即将 spawn 不可信 agent 的进程 |
| 用继承 fd 下发 bootstrap 凭据 | 更干净，但后端可能夹在 siftd 与 wrapper 之间（`tmux` 在 pane 里重开会话时 fd 不保证传递）。改用 0600 文件 + 读后 unlink + 一次性代次，代价是磁盘上一个短窗口 |

## 后果

- 正面：恢复矩阵有确定的输入；`sift logs` / `sift kill` / 重试全部走同一套控制文件；后端可替换，沙箱后端将来是第三个实现。
- 负面：多一个自研进程要维护（wrapper 自身的健壮性成为关键路径）；控制文件的原子写与 heartbeat 频率需要调。
- 中性：tmux 纳入 `sift doctor` 探测，但只在配置使用时才要求存在。
