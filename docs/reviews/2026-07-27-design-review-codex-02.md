# DESIGN.md 架构评审（第 11 轮，D0.4 再评审）

> 日期：2026-07-27  
> 评审人：**Codex（GPT-5）**  
> 评审对象：[`docs/DESIGN.md`](../DESIGN.md) **D0.4**（对应 PRD V0.4，工作区未提交版本）  
> 评审依据：[`docs/PRD.md`](../PRD.md) V0.4、active ADR 002–009（ADR-001 已 superseded）、[`design-review-09`](2026-07-27-design-review-codex-01.md) 与 [`design-review-10`](2026-07-27-design-review-kimi-k3-03.md)  
> 评审重点：attempt 启动协议的阻断项复验；D0.4 Go / 分发修订的独立机制检查；回放数据模型与文档对账

## 1. 结论

**不通过；仍不建议进入 WBS 或实现。**

D0.4 的 Go 选型有完整的触发条件、代价与退出路径，技术栈重议本身成立；Gate 缓存与 Change 幂等协议也继续成立。但第 9 轮发现的两个 P1 在当前文本中原样存在：启动协议没有唯一、可线性化的 spawn 权，且 Run 在真实 Agent spawn 之前就进入 `running`。这两项分别推翻 §6.4 的 effectively-once 声明和 PRD §4.1 的状态真实性，不能下沉到 spec 后再决定。

本轮共记录 **2 个 P1、3 个 P2、3 个 P3**。除延续并独立复验 R9-F1～F3 外，D0.4 新增的统一 decode gateway 与分发契约各引入一个 P2。结论与 review-10 的阻断判断一致，但本评审不沿用其核销结果；下面给出可达交错与独立证据。

## 2. 问题汇总

| 编号 | 级别 | 问题 | 主要影响 |
|------|------|------|----------|
| R11-F1 | **P1** | 同凭据双确认、未认领的启动 operation 与 check-then-spawn 没有一次性 spawn 权 | 同代重复 wrapper 可能零启动或双启动，agent 启动并非 effectively-once |
| R11-F2 | **P1** | attempt / Run 在真实 Agent spawn 前进入 `running` | 可产生“Agent 从未启动”的幽灵 running，恢复证据也无法证明子进程已存在 |
| R11-F3 | P2 | 所有 Brain trace 被强制共用 `gate_input_snapshot_id` | 非 Gate 触点无法合法落模，提示词回放集不完整 |
| R11-F4 | P2 | 单一严格 decode 策略拒绝 Forge payload 的所有新增字段 | 把兼容性扩展误判为契约破坏，双平台适配会被上游无关字段变化击停 |
| R11-F5 | P2 | “单文件产物”、三个二进制与四组合验证口径不一致 | 发布包、升级原子性及兼容性验收没有唯一契约 |
| R11-F6 | P3 | 当前阻断评审尚未进入 DESIGN 对账，PRD 文档地图仍只宣称旧轮次已处置 | 默认上下文会隐藏当前 P1 |
| R11-F7 | P3 | README 的约 1200 行阈值仍无变更理由 | 拆分规则不可追溯 |
| R11-F8 | P3 | PRD 与 ADR-009 对“为何新增分发需求”互相引用 | 产品动机没有唯一事实源 |

## 3. 详细发现

### R11-F1 · P1：启动协议仍没有一次性 spawn 权

**位置**：DESIGN §6.4、§8.4 步骤 1–5、§10.1 第 1/11/12 行；ADR-003、ADR-005。

当前 claim 是 daemon 在 wrapper 启动前建立的；两个 `claim:confirm` 使用同一个 owner nonce 与 fencing 代次。claim 的唯一约束因此不区分 wrapper 所有者——wrapper 不竞争插入 claim，只出示同一份预建 claim 的凭据。

可达交错仍然成立：

1. wrapper A 读取并 unlink bootstrap、尚未第一次 confirm 时 daemon 崩溃。这正是 V2 明列的“bootstrap 读取后”崩溃点。
2. 重启时观测为 `pending`、claim 未确认、A 进程存在、无 `control.json`。§10.1 只定义“claim 未确认、**无进程**、无 control”的动作，没有覆盖这个组合，也没有规定未完成启动 operation 何时可重放。
3. 若 operation 沿用原 claim 重放，wrapper B 获得同一 owner nonce / 代次；同一 daemon 会话内若 worker 无原子认领也能产生相同结果。
4. A 第一次 confirm 将 attempt 推到 `starting`。B 的第一次 confirm 与 A 的合法第二次 confirm，在服务端可见字段上完全相同。拒绝会误拒合法推进；接受或幂等重放则向竞争 wrapper 放行。

所以 §10.1“claim 唯一约束使后到者 confirm 失败”的结论不成立。即使拆分两种 RPC，单纯的“spawn 前再查一次代次”仍是 check-then-act：校验回复与操作系统 `spawn` 不在同一原子边界，`spawn` 也不会消费 fencing token。协议必须说明旧 owner 在新 owner 生效前如何被证明已失去 spawn 能力。

**修订要求**：

1. 启动 operation 在 daemon 会话内以 CAS + lease 原子认领；同一 operation 不得并发派发。
2. 第一次握手签发 wrapper-session ID；pre-spawn 许可绑定该 session，并以 CAS 一次性消费，明确请求与响应丢失时的重放语义。
3. 给出真正的线性化点：daemon 唯一 spawn、可跨 spawn 持有的 OS 互斥，或不可换代的 `spawning` handoff 均可；仅查询 fencing 代次不可。
4. 恢复矩阵补“bootstrap 已读、wrapper 在、尚未 confirm”组合；V2/V4 增加同代双 wrapper、响应丢失和校验后换代的确定性交错。

### R11-F2 · P1：`running` 仍早于真实 Agent 启动

**位置**：DESIGN §8.4 步骤 4–5、attempt 生命周期说明、§10.1 `starting` 恢复行；PRD §4.1。

现有顺序仍是：wrapper 先写 `control.json`，第二次 `claim:confirm` 令 daemon 把 attempt 与 Run 推为 `running`，RPC 返回后 wrapper 才执行真实 Agent 的 `spawn`。因此必然存在：

```text
running 已提交 → wrapper 崩溃或 spawn 返回错误 → Agent 从未启动
```

“转移发生在 spawn 前最后一道校验之后”不能消除这个窗口；它只证明校验发生过，不证明副作用已经发生。`control.json` 同样在 spawn 前落盘，当前字段没有区分 wrapper PID 与 Agent 子进程 PID，恢复矩阵据此补齐 `queued → running` 最多能证明 wrapper 存在。

**修订要求**：pre-spawn 许可后 attempt 保持 `starting`；真实 spawn 成功后原子记录 Agent 子进程身份，再以独立 `claim:started`（或等价证据）推进 attempt / Run。恢复矩阵必须分别覆盖“许可已发、未 spawn”和“spawn 已成功、started 尚未落库”。同时修正 §3.2 / §8.9 / ADR-008 对 `run.sock`“无任何改 `runs.status` 的能力”的绝对表述，因为当前 owner-nonce RPC 确实会触发 `queued → running`。

### R11-F3 · P2：Brain trace 与 Gate 快照仍被错误地强制一一关联

**位置**：DESIGN §7 回放集关联规则。

§7 仍要求 Gate 输入快照与 Brain trace 共用同一个 `gate_input_snapshot_id`。T1、T2、T6、T7 可发生在没有 Gate 调用的时点，T4/T5 也不保证与某次 Gate 一一对应；这些 trace 没有合法的 Gate 外键。

**建议**：Brain trace 以 `(run_id, attempt_no, touchpoint, call_seq)` 等调用身份独立主键；仅真正参与某次 Gate 输入的 trace 使用可空 `gate_input_snapshot_id`。缓存、影子 Gate 记录与 Gate 回放继续共用同一快照 ID，不需要退回旧缓存设计。

### R11-F4 · P2：统一 `DisallowUnknownFields` 混淆了两种“未知”

**位置**：DESIGN §5.2 约束 1/3、§8.1、V14；ADR-009“代价”部分。

D0.4 要求 forge payload、LLM 输出、配置和 socket 请求走同一 gateway，并统一拒绝未知字段。这里混淆了：

- **缺少领域判定所需字段**：例如 actor / mergeability 不可得，应 fail closed；
- **输入多出本领域不消费的字段**：对 LLM 输出、配置、RPC 可视为封闭契约而拒绝，但 Forge API payload 是开放世界输入，上游增加一个无关字段不应使现有中性映射失效。

按当前 V14，“每个边界类型未知字段必须拒绝”，GitHub/GitLab 的兼容性字段扩展会被归为 `ContractViolation` 并 fail closed；若字段持续存在，相关摄入/对账会持续失败。这既没有提升 C8 的 actor 保证，也削弱 Q3 的双平台稳定性。单一 gateway 可以保留，但必须支持按边界选择 closed-world / open-world 策略。

**建议**：配置、LLM 输出、socket 请求保持 strict；Forge raw payload 允许额外字段，但对适配器实际读取的字段执行必填、类型和枚举校验，无法归一的必需语义仍输出 `unknown` / fail closed。V14 分成“封闭契约拒绝额外字段”和“Forge 契约接受无关扩展、拒绝必需字段缺失/变型”两组。

### R11-F5 · P2：分发产物与兼容性验证没有唯一口径

**位置**：PRD §9.3 分发行；DESIGN C10、§5.1、§11、V15；ADR-009 决策表。

PRD/C10 要求“自包含单文件产物”，§11 又使用单数“替换二进制”；但 D1 明确定义 `siftd`、`sift`、`sift-agent-wrapper` 三个二进制，GoReleaser 输出归档。两种解释都可以成立，但发布与升级行为完全不同：

- 若“单文件”指整个产品，需要 multicall binary、子命令自举或等价设计；
- 若只指“每个可执行文件均自包含”，PRD 应改为“自包含原生二进制集合/单归档”，并定义三文件升级的原子性与版本一致性检查。

V15 也只要求四组合构建成功、macOS/Linux 各运行一次；这并不等于 darwin/linux × arm64/amd64 四个受支持组合都经过运行验证。另有“Linux”未限定 systemd 基线，而 §11 只给 systemd user unit，支持范围仍不确定。

**建议**：先裁决产物形态，再让发布清单、Homebrew formula、升级/回滚与 `doctor` 的版本一致性检查对齐；四个支持组合至少各跑安装 + SQLite + socket + wrapper/恢复冒烟，或者在 PRD 明确哪些组合只承诺可构建。Linux 同时写清支持的发行版/systemd 基线或提供不依赖 systemd 的前台运行路径。

### R11-F6～F8 · P3：文档治理遗留

1. **阻断结论不可见**：DESIGN 只有 §14.5–14.7，没有 review-09/10 的处置节；PRD §13 仍写“已过七轮评审并逐条处置”。按 README 的默认上下文规则，后续 WBS 任务不会加载 `reviews/`，因此当前两个 P1 会从默认上下文中消失。作者处置本轮时应新增 §14.8，并在关闭前显式保留阻断状态。
2. **1200 行阈值无来源**：README 从约 300 行放宽到约 1200 行的理由仍未进入 README/CHANGELOG；当前 DESIGN 1019 行，规则变化直接决定它是否拆分，更应可追溯。
3. **分发动机循环引用**：PRD §2.1 把“为什么从一开始按对外分发”指向 ADR-009；ADR-009 又以“PRD V0.4 新增分发需求”为触发因素。需求动机应在 PRD 成为唯一事实源，ADR 只引用该动机并裁决技术后果。

## 4. 已复验通过的部分

- Go 取代 Bun 的触发条件成立：分发/多平台成为需求，且当前无代码，迁移时点合理；ADR-001 的 superseded 处置正确。
- `modernc.org/sqlite` + `CGO_ENABLED=0` 与交叉编译目标方向一致；写连接上限 1 与单写者模型一致。
- Gate 使用完整冻结输入摘要作为缓存键，Change 使用 marker + 全状态搜索 + 远端 ID 收敛，相关旧 P1 继续关闭。
- 项目级失败隔离、双 socket、人工/自动验收分组、跨平台发布切片的结构方向成立；本轮指出的是其契约边界尚未唯一化。

## 5. 复评通过条件

1. R11-F1/F2：启动协议给出可线性化的一次性 spawn 权，Run 只在有真实 Agent 启动证据后进入 `running`，恢复矩阵与 V2/V4 覆盖全部新增交错。
2. R11-F3：Brain trace 使用独立调用身份，Gate 快照关联改为可选。
3. R11-F4：decode gateway 按边界区分封闭与开放输入；Forge 的无关新增字段不触发停摆，必需语义缺失仍 fail closed。
4. R11-F5：单文件/三二进制产物形态完成 PRD 裁决，升级一致性与四组合运行验收对齐。
5. 新增评审处置节，把未关闭项带回 DESIGN 默认上下文；补齐阈值与分发动机的单一事实源。

R11-F1/R11-F2 关闭后才适合进入 WBS；其余 P2 应在同一轮设计修订中完成，避免把相互冲突的契约直接下沉到 specs。

---

_评审人签名：Codex（GPT-5）_
