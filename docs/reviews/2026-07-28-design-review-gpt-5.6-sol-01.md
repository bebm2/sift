# DESIGN.md 架构评审（第 16 轮，PRD 核心输入）

> 日期：2026-07-28  
> 评审人：GPT-5.6 SOL  
> 评审对象：[`docs/DESIGN.md`](../DESIGN.md) D0.9（对应 [`docs/PRD.md`](../PRD.md) V0.7）  
> 核心依据：PRD 的公理、状态模型、门禁语义、非功能需求与 PoC 成功标准  
> 辅助核对：ADR-003、ADR-005、ADR-010，以及只读存档 [`design-review-15`](2026-07-28-design-review-kimi-k3-05.md)

## 1. 结论

**不通过，建议暂缓进入 WBS。**

D0.9 的总体架构方向是成立的：确定性裁定、唯一状态转移入口、Gate 纯函数、transactional outbox、actor fail closed、注意力配额单一收费口、base 策略读取、TM6 如实暴露，均正确承接了 PRD 的核心约束。

但当前仍有 **3 项 P1**：

1. 旧的 merge outbox operation 可以在 Change head 已变化后合并未经对应 Gate 裁定的新 head；
2. `startup_stall` 达升级上限时同时被定义为 `hold` 和“终局决定”，会错误关闭事实窗口；
3. `startup_stall` 的 `retry` 探测成功后，没有定义 Run / Interrupt / attempt / outbox 的原子收敛事务。

三项都位于“门禁不能被绕过”或“旧执行体与新 attempt 不能并存”的安全主路径，不宜下沉到 specs 后再决定。

## 2. 阻断项

### F1 · P1：merge operation 没有远端 head CAS，旧门禁结论可合并新代码

**位置**：DESIGN §6.4、§6.5；ADR-003。

当前协议只规定：

- operation key 含 `head_sha`；
- 重试前查询 Change 状态和已合并 head SHA；
- head 变化视为另一次合并。

这不足以保护实际 merge 动作。以下交错可达：

1. Gate 对 head `A` 全绿，写入 `merge:A` outbox operation；
2. operation 尚未执行时，Change head 被更新为 `B`；
3. 旧 worker 看到 Change 仍未合并；
4. 若调用现有 PRD 动词 `mergeChange(id, method)`，forge 会合并当前 head `B`；
5. `B` 从未经过对应的 Gate 快照与审批。

本地 operation key 只能去重，不能约束远端实际合并的对象。这直接违反 PRD A1、§5.4 和 §10.1 的门禁要求。

**必须修订**：

- merge 的语义契约必须携带 `expected_head_sha`，并使用 forge 提供的条件合并能力；
- 执行前重读发现 current head 与 expected head 不同，应把旧 operation 收敛为 stale/no-op，重新组装 Gate 输入，不能执行 merge；
- 即使“查询后相等”，实际 merge 请求本身仍须带远端 CAS，不能依赖 check-then-act；
- V7 增加确定性交错：Gate(A) → 入队 merge(A) → head 变 B → 旧 operation 必须拒绝，且 B 只有重新过 Gate 后才能合并。

PRD 的最小动词当前写作 `mergeChange(id, method)`；字段签名可下沉 spec，但“必须按预期 head 条件合并”属于需求安全语义，应同步回写 PRD 或至少在 DESIGN 明确为该动词的强制语义。

### F2 · P1：`startup_stall` 达升级上限的处置自相矛盾

**位置**：DESIGN §10.1 的人工分支与仲裁表。

D0.9 同时写了：

- 探测继续失败，达 `max_escalations` 后落 `hold`；
- `hold` 是非终局请求，不写 `attempt_decision`，Interrupt 保持待决；
- 但“达 `max_escalations` 后的处置”又被列入终局决定，写不可逆 marker 并关闭 Interrupt。

PRD §4.2 对 `startup_stall` 已明确：禁用 `auto_reject`，达升级上限后落 `hold`，原因正是系统仍不能证明执行体已经消失。因此这里不能存在自动终局处置。

若实现按“终局决定”分支执行，定时器会在无人做决定时关闭事实窗口；随后迟到的合法 `started` 会被当成 `superseded_by_decision` 并终止。这与 PRD “事实与人的决定谁先落库谁生效”冲突，因为此时根本没有人的决定。

**必须修订**：

- 对 `startup_stall`，只有显式 `reject`（以及未来另有 PRD 明示的显式终局动作）写 `attempt_decision`；
- `retry`、`hold`、`escalate`、达到升级上限后封顶为 `hold` 均不写 marker、不关闭 Interrupt；
- ADR-010 决策 6 同步把“人的决定”收窄为明确动作集合；
- V4 增加“达到升级上限后迟到 started 仍按事实优先恢复”的断言。

这也是现存 `design-review-15` 的 N1；D0.9 未关闭它。

### F3 · P1：`retry` 探测成功后的状态与副作用没有原子收敛协议

**位置**：DESIGN §10.1 “retry 两段式”。

当前只写：探测确认旧执行体消失后，写 `attempt_decision`、关闭 Interrupt、按重试策略新开 attempt。仍缺四个关键答案：

1. Run 从 `waiting_human` 收敛到哪个合法状态——应明确是 `queued`，还是保留 `waiting_human` 直到新 agent started；
2. 新 attempt 的创建、启动 outbox operation、Run 转移、Interrupt 关闭、marker 写入是否同事务；
3. 注意力与指令回执事件何时落库；
4. 在这些动作之间崩溃时，恢复依据是什么。

若它们分事务执行，可出现：

- Interrupt 已关闭、Run 仍 `waiting_human`、没有新 attempt；
- Run 已 `queued`，但新 attempt / 启动 operation 未创建；
- 新 attempt 已可启动，但旧 Interrupt 仍接受指令；
- marker 已写导致迟到事实被吸收，但 retry 的新执行没有被可靠排入。

这些窗口会制造静默挂起或错误仲裁，违反 PRD A3、§4.1 和 DESIGN 自己的 Q1/Q5。

**必须修订**：定义单一原子提交，例如：

`受控探测成功证据 + attempt_decision + 旧 attempt 终结/隔离解除 + Interrupt 关闭 + Run waiting_human→queued + 新 attempt/启动 operation + 事件/回执`

应在同一事务完成；若出于模型原因不能同事务，必须给出等价的可恢复中间态与唯一恢复动作。V2/V4 要逐边界做崩溃注入。

## 3. 重要非阻断项

### F4 · P2：Change 创建触发条件在文档内不一致

**位置**：DESIGN §8.4、§10.1。

§8.4 写“检测到分支有新提交后”即创建 Change；§10.1 则写 `result.json` 成功后校验提交与 head SHA，再创建 Change / 进 Gate。

前一种实现会在 Agent 尚未退出、仍可能继续提交时提前创建 Change，甚至让失败 attempt 的中间提交进入 Change。它削弱 PRD §5.8 的 Layer 2 裁定边界。

建议裁死为：**只有 wrapper 给出成功完成证据、最终 head 已冻结且至少有一个有效提交后，才生成 create-change operation**。运行中的提交只作进度事实，不触发 Change。补测“先提交、后继续运行/失败”不得提前创建 Change。

### F5 · P2：进程组消失证明依赖未声明假设，当前不变式有过度声明

**位置**：DESIGN §6.4、§8.4、§10.1；ADR-005、ADR-010。

“任一时刻至多一个存活 Agent 写该 worktree”的证明依赖 Agent 及其写工作副本的子进程不通过 `setsid`、二次 fork 等方式脱离 wrapper 进程组。直接父子关系只在 spawn 时成立，不保证整个执行期都成立。

这不要求 V0 立即做沙箱，但必须如实写成安全假设：

- 已知、通过配置校验的 agent CLI 不主动脱离受监督进程组；
- 每个受支持 agent 都要有进程拓扑 spike / 冒烟证据；
- 一旦发现脱离行为，该 agent/backend 组合不得宣称满足“确认消失后可安全 retry”；
- `sift doctor` 应能报告未经验证的组合。

这也是现存 `design-review-15` 的 N2，D0.9 未处置。

### F6 · P2：创建 Change 的幂等协议没有对应的 Forge 端口能力

**位置**：PRD §5.2 最小动词集；DESIGN §6.4、§8.1。

DESIGN 要求按 `op_key` marker 跨 open / closed / merged 全状态搜索 Change，但其声明的 Forge 适配层只承接 PRD 最小动词集；该集合只有 `createChange` / `getChange`，没有按 marker 或 operation key 查找 Change 的能力。

因此协议目前无法通过既定端口实现，除非 outbox worker 绕过 Forge 适配层直接调用平台 API，这又违反“归一在边界完成”。

建议二选一并明确：

1. 增加内部端口 `findChangeByOperationKey`；或
2. 把 `createChange` 定义为适配层内的幂等复合操作，由适配器负责全状态搜索和冲突返回。

同时用 GitHub / GitLab 真实 fixture 证明该查询可实现，不应只在本地 fake 中验证。

### F7 · P2：两项 PRD 约束在 DESIGN 中尚无可实现解释

#### F7.1 `sift speak` 与“平台差异只允许两处”冲突

PRD §11 把系统 TTS 的 `sift speak` 列入 V0；PRD §9.3 与 DESIGN §11 又规定平台差异只允许出现在托管与沙箱后端两处。macOS 与 Linux 的系统 TTS 能力、命令和依赖并不相同。

DESIGN 只写了 TTS renderer，没有说明如何在四种发布组合上满足该要求，V15 也没有 TTS 冒烟项。需要产品侧裁决：

- 把 TTS 适配器列为允许的平台差异第三处；或
- 采用随归档分发的跨平台 TTS 实现，并放弃“系统 TTS”的措辞；或
- 将 `sift speak` 移出 V0。

在裁决前，DESIGN 不应继续宣称平台差异只有两处。

#### F7.2 `escalate` 要“换更强通道”，但 V0 只有一个 Channel

PRD §4.2 要求 `escalate` 换更强通知通道再推一次，§7.3 又规定 V0 只实现一个 Channel。DESIGN 没有解释“更强”在单 Channel 下是同通道的高优先级模式、forge + Channel 双投递，还是事实上不可实现。

建议 PRD 先收敛语义，再由 DESIGN 固化 renderer/outbox 行为；不能留到实现时自行猜测。

### F8 · P2：评审处置对账与只读存档不一致

**位置**：DESIGN §1、§14.12；PRD §13；`reviews/2026-07-28-design-review-kimi-k3-05.md`。

当前 review-15 存档的两项发现是：

- N1：达升级上限后的终局措辞悬空；
- N2：进程组消失证明依赖 agent 不脱组的假设。

它没有提出 DESIGN §14.12 所称的“唯一新发现：`retry` 两段式”。但 DESIGN 与 PRD 都把 review-15 记成了该修订的来源，并声称其发现已处置；实际 N1/N2 仍在正文中。

根据 docs/README.md，reviews 是只读存档，不能通过改写 review 来对齐正文。应当：

- 修正 DESIGN §1 / §14.12 与 PRD §13 的来源说明；
- 把 `retry` 两段式记为 D0.8→D0.9 自查，或指向真实存在的评审来源；
- 在 DESIGN 的新处置节对本轮 F2/F5 正式对账。

这不直接改变运行时，但会破坏后续代理的可信上下文，且已经掩盖了一个安全语义矛盾，因此定为 P2。

### F9 · P3：PRD“自身不落盘凭证”与本地 capability 文件的术语边界不清

PRD §9.3 写“Sift 自身不落盘凭证（凭证由 CLI 持有）”；DESIGN 则持久化 `operator.token`、run token 与短时 bootstrap 凭据。结合 PRD §5.2，产品意图很可能只是“不落盘 forge 凭证”，但当前安全行的文字比这个范围更宽。

建议把 PRD 改为“Sift 不落盘 forge 凭证”；本地 capability / attempt 凭据另列权限、生命周期和 TM6 暴露面。DESIGN 当前对后者的风险披露较诚实，问题主要是与核心输入的术语未对齐。

## 4. 通过项

以下关键设计与 PRD 对齐，建议保留：

- recommendation / command / transition 类型分离，状态写入只有一个入口；
- Forge 事实观测与驱动性指令分开，只有后者做 actor 鉴权；
- Gate 纯函数、冻结快照、整份快照摘要作为缓存键；
- 影子门禁、认证投影、回放集与 Gate 同片交付；
- 注意力配额与 critical 熔断收在单一 Interrupt 发射器；
- 两个 socket 按端点与凭据授权，并如实承认同 UID agent 的 TM6 暴露；
- attempt 阶段不冒充第二套 Run 状态机；
- `spawning` handoff、迟到事实吸收和 worktree 隔离标记解决了多数双起窗口；
- 项目级失败与 daemon 级拒启分层；
- 发布归档、版本握手、前向迁移和跨平台验证要求完整。

## 5. 复评通过条件

1. **F1**：merge 改为远端 expected-head CAS，并补 stale operation 测试；
2. **F2**：`startup_stall` 达升级上限只落 `hold`，不写 decision marker、不关闭 Interrupt；
3. **F3**：补齐 retry 探测成功后的 Run 状态与原子事务；
4. **F4**：Change 只在成功完成证据后创建；
5. **F5**：声明并验证“不脱离进程组”的监督假设；
6. **F6**：Forge 端口具备 marker 全状态对账能力；
7. **F7**：PRD 对 TTS 平台差异与单 Channel escalation 给出可实现裁决；
8. **F8**：修正 review-15 处置来源，不修改只读评审存档。

F1–F3 关闭前不建议进入 WBS；其余项至少应转成明确的 PRD 回写或 WBS 前置验收，不能静默下沉给实现者。

---

_评审人：GPT-5.6 SOL_
