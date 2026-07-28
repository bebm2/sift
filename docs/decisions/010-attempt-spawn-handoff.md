---
status: active
created: 2026-07-27
summary: Agent 启动用会话绑定的一次性 spawn handoff，running 只认启动证据
---

# ADR-010 attempt 启动采用 session 绑定的一次性 spawn handoff

本 ADR 修订 [ADR-003](003-transactional-outbox.md) 的“启动 agent”投递语义、[ADR-005](005-execution-backend-and-wrapper-contract.md) 的 attempt 启动部分，以及 [ADR-008](008-control-plane-endpoints-and-capabilities.md) 的 wrapper 动词集。结构展开与恢复矩阵见 [DESIGN §8.4 / §10.1](../DESIGN.md)。

## 决策

保留“daemon 是唯一 DB 写者、所有后端只启动 wrapper、wrapper 再启动 Agent”三条边界；把 D0.4 的两次同凭据 `claim:confirm` 改为以下协议：

1. **启动 operation 先认领**：outbox worker 以 CAS + lease 原子认领 operation，只有 lease owner 可生成 dispatch、写 bootstrap、spawn wrapper。daemon 重启后先恢复 attempt，再回收/重放 operation lease。
2. **`claim:acquire` 绑定 wrapper**：wrapper 生成 `wrapper_instance_id`，携 bootstrap nonce / generation / dispatch 调 acquire。daemon 以 CAS 记录唯一 instance 并签发 wrapper session；同一 instance 的请求幂等返回，其他 instance 拒绝。
3. **`claim:permit-spawn` 交接所有权**：session owner 写好 wrapper 身份与进程组后请求 permit。daemon CAS `starting → spawning`，持久化并返回该 attempt 唯一的 spawn permit；重放返回同一 permit。
4. **`spawning` 期间禁止换 owner**：只要旧 wrapper 或其进程组仍存在，恢复与人工 kill/retry 都不得释放/替换 claim。要换代必须先经 DESIGN §10.1 的**受控终止流程**终止身份确认过的 wrapper / 进程组并确认消失。fencing generation 继续递增，但不能替代“旧 owner 已失去 spawn 能力”的证明。**证明不了就必须打扰人**：身份不可确认或进程组在有界升级后仍存在时，attempt 冻结、发一次 `startup_stall` Interrupt、Run 转 `waiting_human`（PRD §4.1–4.4），不得让 Run 静默停在 `queued`。
5. **`claim:started` 才进入 `running`**：wrapper 以进程内 one-shot guard 确保同一 permit 至多进入一次 spawn 路径；spawn 成功后原子写入 Agent PID / 启动时间 / 可执行路径，再携 session + permit + Agent 身份调用 started。daemon 验证后 CAS `spawning → running`，并经唯一 `transition()` 推进 Run `queued → running`。响应丢失时同一请求幂等返回，permit 响应重放不得再次 spawn。Agent 若在 started 落库前极快退出，身份一致的原子 `result.json` 可证明“已启动并结束”，恢复先补 started 再按结果推进，不重发 operation。

> **名称修订**：[ADR-013](013-startup-stall-retry-convergence.md) 已将下述 `attempt_decision` 更名为规范名称 `attempt_resolution`；本条保留旧名以维持历史论证，后续 spec 与代码只使用新名。

6. **人工态与迟到启动事实的仲裁**：受控终止无法证明旧执行体消失时，Run 进 `waiting_human`（`startup_stall`），claim 与 session / permit **继续冻结有效**——作废它们并不安全，因为 OS `spawn` 不消费 fencing token。因此旧执行体醒来提交合法 `claim:started` 是**必须建模的正常输入**，仲裁点是单一的 `attempt_decision` marker（CAS，不可逆）：人的决定未提交前**事实优先**（同一事务推进 attempt、Run `waiting_human → running`、把该 Interrupt 标 `superseded_by_fact` 关闭、接管监督）；决定已提交则**由决定吸收事实**（不推进 Run，但把迟到的 Agent 身份登记为可终止身份，回 wrapper `superseded_by_decision`，继续执行该决定的终止）。`claim:started`、恢复补 started、迟到 `result.json`、Interrupt 指令四个入口共享同一套 CAS 前置与幂等结果。

attempt 生命周期因此是：

```text
pending → starting → spawning → running → finished
                    ↘ orphaned（启动证据不全且 owner 已失）
```

`control.json` 分开记录 wrapper 与 Agent 身份。spawn 成功但 Agent 身份尚未落盘时，如果 wrapper 崩溃且进程组仍在，恢复先终止该组并确认消失，再把 attempt 判为 `orphaned`、新开 attempt；不补 `running`，也不重放本 attempt 的 spawn operation。

## 理由

D0.4 的协议没有线性化点：

- acquire 与 pre-spawn 都叫 `claim:confirm`，使用同一 nonce / generation；第一次调用的 RPC 重放、竞争 wrapper 的重试与合法第二次调用不可区分。
- claim 由 daemon 预建，唯一约束只防第二行 claim，不区分两个出示同一 claim 凭据的 wrapper。
- “spawn 前再查 generation”是 check-then-act；数据库校验与 OS `spawn` 不原子，旧 wrapper 校验成功后若允许新 owner 生效，二者仍可双起。
- daemon 在 spawn 前就把 Run 置为 `running`，与 PRD“Agent 进程已启动”的定义相反。

session 解决“谁在调用”，唯一 permit 解决“是否已授权一次”，`spawning` 冻结解决“授权后是否还能换 owner”，started 证据解决“Agent 是否真的启动”。四者承担不同职责，不能互相替代。

**本协议的观测原语是“wrapper 存活或其进程组存在”，它的后端中性来自 [ADR-005](005-execution-backend-and-wrapper-contract.md) 决策 2 的拓扑裁决**（Agent 恒为 wrapper 的直接子进程且在其进程组内，后端只决定 wrapper 跑在哪里）。若某个后端让 Agent 脱离 wrapper 进程组，本协议在该后端即失效——不是退化，是 `spawning` 窗口直接重新打开。新增后端必须先满足这条拓扑，或先补一个等价强度的存活证据。

## 放弃的选项

| 选项 | 放弃理由 |
|------|----------|
| 继续两次 `claim:confirm`，只补阶段前置条件 | RPC 重放仍会落在不同阶段，语义不可区分；也不解决 check-then-spawn |
| 只增加 fencing generation | OS `spawn` 不校验 token；校验返回后仍有 TOCTOU 窗口 |
| 跨 spawn 持有 OS 文件锁 | 可提供硬互斥，但 macOS/Linux 的锁继承、wrapper 崩溃后 Agent 持锁、tmux fd 传递都成为新协议面；V0 不需要同时维护两套所有权机制 |
| daemon 直接 spawn Agent | 会推翻 ExecutionBackend / wrapper 的父子关系，tmux 与日志/信号监督均需重做；改动远大于 handoff |
| spawn 前置 `running`，靠 supervisor 事后修 | 交付的是明知虚假的当前投影，直接违反 PRD 状态定义与 Q1 |

## 后果

- 正面：同代双 wrapper、三个 RPC 的响应丢失、permit 后 daemon 崩溃、人工 retry 与旧 wrapper 苏醒都有唯一收敛动作；Run `running` 有可验证事实依据。
- 负面：attempt 多一个 `spawning` 阶段；claim 需存 wrapper instance / session / permit，测试矩阵显著增加。
- 负面：`spawning` owner 卡住时系统宁可等待、终止或转人工，也不抢跑新 owner；这是以可用性换无双写，符合 fail closed。**这笔交换成立的前提是挂住可见**——停滞必须以 Interrupt 落进注意力系统并计配额，否则换来的是一个静默挂死的 Run（A3），比双写更难发现。
- 负面：人工态不是一个静止状态，而是一个仍在接收事实的状态（决策 6）。代价是「Run 已进终态但执行体未证明消失」这种组合必须被显式建模为**隔离**（worktree 不回收、不复用），不能用终态掩盖。收益是迟到的 `started` 从「一条无处安放的非法请求」变成「唯一一次拿到确切 Agent 身份的机会」，终止从此可执行。
- 中性：fencing generation 仍保留，用于拒绝旧 acquire / permit / started；它从“唯一安全机制”降为 handoff 的一层校验。
- 维护义务：任何新增的 retry / kill / recovery 路径都必须复用“证明旧 wrapper 与进程组消失后才换代”的单一函数。
