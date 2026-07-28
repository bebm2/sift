# DESIGN.md 架构评审（第 13 轮，D0.6 复评）

> 日期：2026-07-27  
> 评审人：**kimi-k3**  
> 评审对象：[`docs/DESIGN.md`](../DESIGN.md) **D0.6**（1080 行，对应 PRD V0.6）  
> 评审依据：[`docs/PRD.md`](../PRD.md) V0.6、ADR 001–010、[`design-review-09`](2026-07-27-design-review-codex-01.md)（阻断源）、[`design-review-10`](2026-07-27-design-review-kimi-k3-03.md)（我上一轮，维持阻断）  
> 评审重点：（a）review-09 五条复评条件与两个 P1 是否真正关闭；（b）review-10 N1 与 §14.9 四笔自留账的处置；（c）对新启动协议（ADR-010）的独立对抗性复查

## 1. 结论

**通过（附两项遗留）。阻断解除，可以进入 WBS。**

review-09 的两个 P1 在 D0.5 被真正关闭——不是补字段式的降级，而是协议重写：三动词拆义（`claim:acquire` / `claim:permit-spawn` / `claim:started`）+ wrapper session + 唯一 spawn permit + 不可换代的 `spawning` handoff + operation CAS/lease 认领。R9-F1 要求的四件事（原子认领、两阶段可区分、线性化方案、确定性交错测试）逐条有着落；R9-F2 要求的四件事（pre-spawn 不进 `running`、`claim:started` 与进程身份、恢复矩阵两侧窗口、授权表述收窄）同样逐条有着落。D0.6 又处置了 D0.5 自己留下的四笔账，其中「受控终止流程」把 `spawning` 冻结的活性出口补成了可见打扰——这正是 ADR-010「用可用性换无双写」成立的前提，补得对。

我独立重走了协议的对抗路径（§4），在默认 `process` 后端下没有找到新的双起或幽灵 `running` 窗口。两处遗留见 §5：**N1（P2）`spawning` 阶段的核心观测原语「进程组」在 tmux 后端下没有定义对应物**，而 tmux 是 V0 第 6 片的功能——该项必须在第 6 片开工前关闭，建议现在就修 DESIGN（只需定义执行句柄 + 补两行矩阵）；**N2（P3）`kill` 与 `retry` 在确认消失后的结局未区分**。两项都不影响第 1–5 片开工。

另记一句：D0.5 采纳了我 review-10 建议的 `spawning` handoff 换代冻结方案；§14.9 主动记录「自己留下的账」而非等评审发现，是好的处置文化，值得保持。

## 2. review-09 复评条件逐条核销

| # | 条件 | 结论 | 证据 |
|---|------|------|------|
| 1 | 唯一、可线性化的 spawn 权 | **满足** | §8.4 452–469：acquire 以 `wrapper_instance_id` 幂等、签发 session；permit-spawn CAS `starting → spawning` 并持久化唯一 permit，同 session 重放返回同一 permit；`spawning` 期间禁止换代，直到旧 wrapper 与进程组经**受控终止流程**确认消失（467）；operation CAS + lease 认领（455）。§10.1 line 758/759 覆盖了 R9-F1 的全部反例路径 |
| 2 | `running` 只认启动证据，恢复矩阵覆盖两侧窗口 | **满足** | §8.4 461/488：`queued → running` 只以「Agent 身份原子落盘 + `claim:started` 验证」为依据；极快退出以身份一致的 `result.json` 为证据（444、461）。§10.1 745–750 覆盖「permit 已发未 spawn」「spawn 已成 started 未落库」「身份落盘进程已退出」等全部组合 |
| 3 | V2/V4 确定性交错测试 | **满足** | V2（§12）覆盖 lease、bootstrap 读取、acquire/permit/started 响应丢失、极快退出等九个注入点；V4（line 814）覆盖同代双 wrapper、permit 响应重放时 spawn 计数仍为 1、`spawning` 中人工 retry/kill、进程组拒绝消失与身份不可确认两例收敛为一次 Interrupt |
| 4 | Brain trace 独立调用身份 | **满足** | §7 line 380/382：调用身份 `(run_id, attempt_no, touchpoint, call_seq)`，`gate_input_snapshot_id` 改为可空关联，「共用同一 ID」收窄到缓存/影子记录/Gate 回放三者 |
| 5 | PRD 版本入口 + README 阈值理由 | **满足** | PRD §13 line 912 已标 D0.6 并改为链接处置节（不再维护评审轮数）；README line 100 补了 300→1200 的语义与理由（提醒线非硬上限、保留完整因果链） |

review-10 补充条件：**N1 循环引用已消除**——PRD §2.1 line 106 现在自己承载分发动机（「V0 起即以对外分发为目标」），ADR-009 只承接技术裁决（§14.8 对账表 line 1031）。

## 3. §14.9 四笔自留账核销（D0.5 → D0.6）

| 账 | 结论 | 证据 |
|----|------|------|
| `spawning` 冻结无活性出口（P2） | **关闭** | §10.1 line 766 定义受控终止流程为恢复、`kill`/`retry`、超时三路径共用的单一实现（身份确认 → 有界升级信号 → 复核消失），三种结局各自确定；确认不了经单一发射器发一次 `failure_review` Interrupt、Run 转 `waiting_human`、attempt 冻结，计入注意力配额（591、768）；PRD §4.1 line 198 与 §4.4 line 262 已同步加「启动阶段停滞需人裁决」与例外条款 |
| 新时限未纳管（P3） | **关闭** | §14.2 line 909 点名五个时限（lease TTL、等 permit 超时、`spawning` 有界等待与升级序列、复核次数、`not_ready` 退避上限），全进 `specs/config.md` 默认值表 |
| Q7 表述（P3） | **关闭** | §2.2 line 59 改为「agent 出示不了启动凭据，因此无法经任何本地 RPC 改变 Run」，并显式承认 `run.sock` 上存在 wrapper 动词；§8.9 line 623 同步收窄为「run token 的 Report 动词不改状态」 |
| `kill`/`retry` 作用于 `spawning` 的语义（P3） | **关闭（但留 N2 接缝）** | §8.10 line 637 降级为「已受理」，CLI 不得回「已终止」 |

## 4. 新协议的独立对抗复查（通过项）

我重走了 review-09 的全部反例与若干新路径，均闭合：

- **同代双 wrapper**：operation CAS + lease 挡同会话双派发（455）；即使两个 wrapper 拿到同一 bootstrap，只有 acquire CAS 记录的 `wrapper_instance_id` 能取得 session，另一实例被拒并记安全事件（457、758）。
- **RPC 重放**：acquire / permit-spawn / started 三个动词各自幂等（457、459、461）；「第一次确认被误当第二次」的歧义因动词拆义 + session 绑定不复存在。
- **check-then-spawn 窗口**：permit 发出后 `spawning` 冻结换代，直到旧 owner 与进程组被证明消失（467、763）——校验与 OS spawn 之间不再存在可插入新 owner 的点。代次校验不再被当作互斥原语，文档明示「OS spawn 不消费 fencing token」（763）。
- **恢复顺序**：attempt 恢复先于 operation lease 回收，防止 worker 在识别旧 wrapper 前重放（737）。
- **幽灵 `running`**：spawn 成功但身份未落盘 → 恢复视为「可能已启动」，先终止确认消失再 `orphaned`，绝不补 `running`（468、749）；身份已落盘但 started 未提交 → 以既有 session/permit 补齐（746）——两方向都不产生虚假 `running`，也不重放同一 operation。
- **PID 复用**：进程身份组合启动时间 + 可执行路径 + control nonce（764）。
- **凭据分离**：bootstrap nonce / session / permit / run token 四者生命周期分离，agent 侧不存在启动凭据（469）；V10 交叉使用全部拒绝（820）。

## 5. 本轮新发现

### N1 · P2：`spawning` 阶段的「进程组」在 tmux 后端下没有定义，且 tmux 会话表被明示排除出恢复依据

**位置**：§8.4 line 433、440；§10.1 恢复矩阵 745–750。

新协议的线性化保证全部挂在同一个观测原语上：「已登记的 wrapper 仍活着**或其进程组仍存在**」（467）。这个原语是 `process` 后端的语义——`SysProcAttr{Setpgid}`，wrapper 是组长，spawn 进「已记录的进程组」（184、440）。但 V0 有两个后端（433：`process` 默认 + `tmux` 可选 durable PTY），tmux 后端下 agent 跑在 tmux server 的会话里，**不在 wrapper 的进程组中**；而 line 433 又明示「tmux 不是事实源……把 durable PTY 当成裁定依据，会让恢复逻辑依赖外部工具的会话表」。

两条规定合并后，tmux 后端在「tmux 会话已建、Agent 身份未落盘」窗口出现了观测真空：wrapper 崩溃后，wrapper 的进程组不存在、Agent 身份缺失——按 §10.1 line 750 的字面，恢复判 `orphaned` 并**新开 attempt**，而 tmux 会话里的 agent 还活着，两个 agent 并行写同一 worktree。这正是本轮协议要消灭的窗口，只是换了个后端复活。对比可见同一文档 `running` 行（755/756）已经区分「后端会话」，`spawning` 行没有——说明作者知道这个问题在 running 阶段存在，只是没收尾到 spawning 阶段。

**建议**（满足其一即可）：

1. 定义后端中性的**执行句柄**写进 `control.json`：`process` 后端为 pgid，`tmux` 后端为「确定性命名的会话名 + pane PID」（会话名从 attempt + dispatch 派生，恢复按名探测存在性）；把 §10.1 `spawning` 行的「进程组」全部改写为「执行句柄」。按名探测是**活性证据**而非裁定依据，与 line 433 不冲突——433 禁止的是拿 tmux 会话当完成/裁定依据，不是禁止看它判断「是否可能已 spawn」。
2. 或者显式声明 tmux 后端 V0 不支持 `spawning` 证据链，第 6 片落地前先补协议——但这等于把已知洞留进 WBS，不推荐。

该项不影响默认 `process` 后端的任何保证，因此**不阻断第 1–5 片**；但 tmux 在第 6 片（§13 line 857），开工前必须关闭。

### N2 · P3：`kill` 与 `retry` 在确认消失后的结局未区分

**位置**：§8.10 line 637、§10.1 line 766。

两处都写「确认旧 wrapper 与进程组消失后**才换代或新开 attempt**」／「按矩阵推进（**多为** `orphaned` + 新开 attempt）」。这对 `retry` 与恢复路径是对的，但对 `kill` 不对：用户要求终止的 Run 不应新开 attempt，其结局按 PRD §4.1 应收敛为 `failed`（人工关闭）。「多为」暗示有例外但没点名，实现时可能照字面给 kill 也新开 attempt。建议在 §8.10 把三路径的结局各自写明：retry → 新开 attempt；kill → attempt 终止、Run 经唯一 `transition()` 转 `failed`；恢复超时 → 按重试策略。

## 6. 遗留与复评条件

- 阻断项（review-09 R9-F1/F2、review-10 全部条件）：**已满足，核销归档**。
- 进入 WBS：可以。WBS 验收须携带 §14.8 末尾的两条维护义务与本轮 N1 的关闭条件（第 6 片开工前）。
- 下次复评触发条件：N1/N2 修订落盘，或第 6 片（tmux 后端）开工——两者先到为准。届时只需评审 §8.4/§10.1 的增量与 ADR-010 是否需补段，无需全量复审。

---

_评审人：kimi-k3_
