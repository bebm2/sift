# DESIGN.md 架构评审（第 9 轮，D0.3 再评审）

> 日期：2026-07-27  
> 评审人：**Codex（GPT-5）**  
> 评审对象：[`docs/DESIGN.md`](../DESIGN.md) **D0.3**（对应 PRD V0.3，工作区未提交版本）  
> 评审依据：[`docs/PRD.md`](../PRD.md) V0.3、active ADR 001–008、[`design-review-05`](2026-07-27-design-review-hex-05.md) 至 [`design-review-08`](2026-07-27-design-review-kimi-k3-02.md)  
> 评审重点：D0.3 新增的 attempt bootstrap / claim / fencing 协议、恢复矩阵、Gate 回放关联，以及上一轮遗留项的独立复验

## 1. 结论

**不通过；修正 attempt 启动协议前，不建议进入 WBS 或实现。**

D0.3 对 Gate 缓存、Change 创建幂等证据、项目级失败隔离、双 socket、人工验收分类等上一轮核心问题的修订仍然成立。但 attempt 启动协议没有给“只启动一个 Agent”提供可线性化的所有权交接点：两次 `claim:confirm` 使用同一 owner nonce 与 fencing 代次，无法识别同代重复 wrapper；而 fencing 校验与操作系统 `spawn` 之间仍有不可原子化的窗口。与此同时，文档在真实 Agent 尚未启动时就把 attempt 与 Run 置为 `running`，直接违反 PRD 对该状态的定义，并产生文档自己声称“不存在”的幽灵窗口。

本轮发现 **2 个 P1、1 个 P2、2 个 P3**。其中 R9-F1 与 R9-F2 是同一启动协议的两个独立失败面：前者破坏 effectively-once，后者破坏 Run 状态真实性。它们应联合修订，但不能只靠在 spec 中补字段来降级处理；DESIGN §6.4、§8.4、§10.1 与 ADR-003/005 的核心保证都依赖这套机制。

## 2. 问题汇总

| 编号 | 级别 | 问题 | 主要影响 |
|------|------|------|----------|
| R9-F1 | **P1** | 两次同凭据 `claim:confirm`、恢复矩阵缺口与 check-then-spawn 没有唯一启动许可 | 同代重复 wrapper 可误确认；claim 换代后旧 wrapper 仍可能 spawn，`effectively-once` 不成立 |
| R9-F2 | **P1** | `running` 在真实 Agent spawn 前落库，且恢复证据只证明 wrapper 存在 | 出现 `running` 但 Agent 从未启动；违反 PRD 状态语义与 Q1 |
| R9-F3 | P2 | 所有 Brain trace 被要求共用 `gate_input_snapshot_id` | 非 Gate 触点无合法外键，提示词回放集无法完整落模 |
| R9-F4 | P3 | PRD §13 仍把 DESIGN 标为 D0.2 | 当前文档版本与追溯入口漂移 |
| R9-F5 | P3 | README 的单文件阈值从约 300 行放宽到约 1200 行仍无理由记录 | 文档拆分规则缺少可回溯依据 |

## 3. 详细发现

### R9-F1 · P1：启动协议没有可线性化的“一次性 spawn 许可”

**位置**：DESIGN §6.4（启动 agent 声明为 effectively-once）、§8.4 步骤 1–5、§10.1“多个 wrapper 竞争”行；ADR-003、ADR-005。

当前协议是：daemon 先创建唯一 claim，把同一份 owner nonce 与 fencing 代次写入 `bootstrap.json`；wrapper 第一次调用 `claim:confirm` 将 attempt 推到 `starting`，写完 `control.json` 后，以同一凭据第二次调用同名 RPC，将 attempt 推到 `running`，随后才执行 `spawn`。

V2 明示要覆盖“bootstrap 读取后”崩溃，但 §10.1 没有对应的恢复行：daemon 已 spawn wrapper A，A 已读取并 unlink bootstrap、尚未第一次 confirm 时 daemon 崩溃；重启后可观测到 `pending`、claim 未确认、wrapper 进程存在、无 `control.json`。矩阵只定义了“claim 未确认、**无进程**、无 control”时递增代次重发，没有规定这个进程仍在的组合如何收敛，也没有规定未完成启动 operation 何时可以重放。若直接重放且沿用原 claim，便会产生同代 wrapper A/B；同一 daemon 会话内若 operation worker 没有原子认领，同样能产生该组合。

而一旦同代 A/B 存在，协议就无法同时正确处理“合法的第二次确认”和“重复 wrapper 的第一次确认”。可达交错如下：

1. 同一启动 operation 在一个 daemon 会话内被重复派发，wrapper A 与 B 都在 unlink 前读到同一份 bootstrap；“读后立即 unlink”不是互斥原语，两个进程先 open/read 再 unlink 是合法竞态。
2. A 的第一次 confirm 把 attempt 从 `pending` 推到 `starting`。
3. B 的第一次 confirm 到达时，服务端只看到“合法 owner nonce + 合法代次 + 当前 `starting`”。这与 A 的合法第二次 confirm 在协议字段上完全相同。
4. 若服务端拒绝，A 的第二次 confirm 也可能被拒，最终零启动；若服务端接受或为 RPC 重试做幂等接受，则竞争 wrapper 也取得继续执行的许可。claim 唯一约束无济于事，因为 claim 是 daemon 预先创建的，两名 wrapper 都没有竞争插入 claim，而是在出示同一个 claim 的凭据。

因此 §10.1“claim 唯一约束使后到者的 `claim:confirm` 失败”的论证不成立。上一轮 review-08 的 W3 已指出可区分性，但把它判为 spec 接缝；本轮给出的交错证明它会直接推翻 §6.4 的 effectively-once 保证，应升级为 P1。

此外，即使把两个 RPC 拆名，单纯的“spawn 前查询 fencing 代次”仍是 check-then-act：旧 wrapper 校验成功、收到回复后，daemon 可能释放/替换 claim 并启动新 wrapper；旧 wrapper 随后恢复并调用不识别 fencing token 的操作系统 `spawn`，仍会双起。fencing 只有在资源执行点也校验 token，或旧 owner 在新 owner 生效前已被证明失去 spawn 能力时才成立；`spawn` 本身不会消费数据库代次。

**修订要求**：

1. 启动 operation 必须有 daemon 会话内的原子认领/lease，禁止同一 operation 并发派发；不能把这一责任推给预建 claim。
2. 两阶段 RPC 必须可区分，并绑定一个在第一次握手后签发、只属于该 wrapper 会话的 ID；pre-spawn 许可必须以 CAS 一次性消费，重复调用的语义要明示。
3. 给出 fencing 校验与 `spawn` 之间的线性化方案。可选方向包括：由 daemon 作为唯一 spawner；或使用能跨 spawn 持有的 OS 级互斥，并规定换代前先终止并确认旧 owner；或把 claim 置为不可换代的 `spawning` handoff，直到获得启动证据或确认旧 wrapper 已死亡。仅“再查一次代次”不够。
4. V2/V4 增加同代双 wrapper、第二次 confirm 响应丢失重试、校验成功后换代、换代后旧 wrapper 恢复四类确定性交错测试。

### R9-F2 · P1：Run 在 Agent 真正启动前已进入 `running`

**位置**：DESIGN §8.4 步骤 4–5、attempt 生命周期图及其后说明、§10.1 `starting` 恢复行；PRD §4.1。

PRD 把 `running` 定义为“Agent 进程已启动”。但 DESIGN 的顺序是：先写 `control.json`，第二次 `claim:confirm` 成功后 daemon 将 attempt 与 Run 置为 `running`，RPC 返回后 wrapper 才调用 `spawn`。因此下面的崩溃窗口必然存在：

```text
第二次 confirm 已提交 running → 响应返回 → wrapper 崩溃 / spawn 失败 → Agent 从未启动
```

DESIGN §8.4 随后声称“不会有 `running` 但从未启动”，理由是转移发生在“spawn 前的最后一道校验之后”。这恰好把“校验已完成”和“副作用已发生”混为一谈；两者之间仍是一个不可回滚的进程边界。恢复矩阵也不能证明 Agent 已启动：`control.json` 在 spawn 前已经写入，其进程身份至多证明 wrapper 存在，不能证明真实 Agent 子进程存在。按现有 §10.1，恢复甚至可能据此补齐 `queued → running`。

这不是短暂显示误差：如果 wrapper 在该窗口死亡，系统记录的是一个**从未启动过 Agent** 的 `running` attempt，直到后续 supervisor 再把它判为 orphaned。它同时违反 PRD 状态定义、Q1“无幽灵 running”和 §6.3“投影与事实一致”的设计意图。

**修订要求**：

1. pre-spawn 许可成功后 attempt 仍保持 `starting`（或增加明确的执行事实子阶段），不得提前改变 Run 为 `running`。
2. wrapper 在 `spawn` 成功后原子落下可验证的 Agent 子进程身份，并通过独立的 `claim:started`/等价确认推进 attempt 与 Run；`control.json` 必须区分 wrapper 与 Agent 的 PID/启动时间/可执行路径。
3. 恢复矩阵显式覆盖“许可已发、尚未 spawn”“spawn 已成功、started 确认尚未落库”两个窗口，并为每个窗口定义唯一收敛动作。
4. 同步修正 §3.2 / §8.9 / ADR-008 对 `run.sock`“无任何改 `runs.status` 的能力”的绝对表述：如果 owner-nonce RPC 会经唯一转移入口推进 `queued → running`，应准确写成“run token 无此能力，wrapper 的启动协议动词只允许这一条受约束转移”。

### R9-F3 · P2：Brain trace 不能全部强制关联 Gate 快照

**位置**：DESIGN §7“回放集含两类可重跑对象”之后的关联规则。

§7 规定 Gate 输入快照与 Brain 触点 trace“共用同一个 `gate_input_snapshot_id`”。但 PRD 的 T1（Intake 体检）、T2（分派）、T6（打扰调度）、T7（校准提案）都可能发生在没有 Gate 快照的时点；T4/T5 也不保证与某一次 Gate 调用一一对应。若照字面建表，非 Gate trace 只能伪造关联、悬空，或被遗漏，都会破坏 PRD §5.6 对“量化提示词改动”的回放要求。

**建议**：Brain trace 使用自身稳定键，例如 `(run_id, attempt_no, touchpoint, call_seq)`；只有确实参与某次 Gate 输入组装的 trace 才携带可空的 `gate_input_snapshot_id`。同时把 §7 的“共用同一个 ID”收窄为“缓存、Gate 影子记录与 Gate 回放共用同一快照 ID；Brain trace 按调用身份独立关联”。这项需要在 DESIGN 改口径，再由 `specs/storage.md` 落字段。

### R9-F4 · P3：PRD 的 DESIGN 版本入口已过期

**位置**：PRD §13“后续文档”表。

PRD V0.3 仍把 `docs/DESIGN.md` 标为 **D0.2**，并写“已过三轮评审”；当前 DESIGN 页脚与本轮评审对象均为 D0.3。该表是文档导航入口，继续保留旧版本会让后续代理误判应加载的设计基线。

**建议**：更新为 D0.3，并避免维护容易持续漂移的评审轮数；改成链接 §14.5–14.6 的处置对账即可。

### R9-F5 · P3：文档拆分阈值变更仍不可追溯

**位置**：`docs/README.md`“与 AI 代理的接口”、DESIGN §15；上一轮 review-08 W1。

单文件拆分提示从约 300 行放宽为约 1200 行，但 README、CHANGELOG 与 DESIGN 的对账节仍没有记录理由。这个数值决定后续文档何时拆分，当前改动又恰好让 972 行的 DESIGN 留在单文件内，容易被理解为为现状反向调整规则。

**建议**：在 CHANGELOG 或 README 留一条简短理由；若实际含义是从“建议拆分”改为“强制考虑拆分”，把语义一并写清楚。

## 4. 已复验通过的关键修订

- Gate 缓存已改为完整冻结输入摘要 `(gate_input_hash, gate_version)`；同 SHA 下 Checks、review、mergeability、riskScore 变化会失效，R5-F1 维持关闭。
- Change 创建使用 `op_key` marker、全状态搜索与远端 ID 收敛，不再把同 base/head 的他人 Change 当成本次 operation 证据，R5-F3 维持关闭。
- 项目级策略错误与进程级启动失败已分层；双 socket、沙箱挂载集、PoC 自动化/人工验收分类已对账，相关旧发现维持关闭。
- attempt、claim、owner nonce 与 fencing 代次的引入方向正确；本轮否定的是最后一段“授权交接到真实 spawn”的闭合性，不是退回 wrapper 直写 SQLite 的旧方案。

## 5. 复评通过条件

1. 启动协议给出唯一、可线性化的 spawn 权；同代重复 wrapper、RPC 重试与 claim 换代均不能双起或零启动。
2. Run 只在有可验证的 Agent 已启动证据后进入 `running`；恢复矩阵覆盖许可与真实 spawn 两侧的崩溃窗口。
3. V2/V4 把上述竞态写成确定性交错测试，不再只测试“第二次校验前”的单线程崩溃点。
4. Brain trace 采用独立调用身份，`gate_input_snapshot_id` 只在存在真实 Gate 关联时使用。
5. PRD 的 DESIGN 版本入口与当前 D0.3 对齐；README 阈值变更留下理由。

在 R9-F1/R9-F2 关闭前，`specs/control-plane.md` 与 `specs/outbox.md` 无法仅靠补字段解决协议歧义；应先修 DESIGN 与 ADR-003/005，再下沉规格和 WBS。

---

_评审人签名：Codex（GPT-5）_
