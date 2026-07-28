---
status: active
created: 2026-07-28
summary: startup_stall 仅显式拒绝才终局；retry 成功以单事务回到 queued
---

# ADR-013 `startup_stall` 的终局边界与 retry 收敛

本 ADR 细化 [ADR-010](010-attempt-spawn-handoff.md) 决策 6。结构展开见 [DESIGN §10.1](../DESIGN.md)。

## 决策

1. 将 ADR-010 的 `attempt_decision` 更名为更准确的 `attempt_resolution`：它既可记录人的决定，也可记录探测结果。对 `startup_stall`，人的显式 `reject` 写不可逆 `attempt_resolution=reject` 并关闭 Interrupt。`retry` 请求、`hold`、`escalate` 以及达到 `max_escalations` 后封顶为 `hold` 都不写 marker，事实优先窗口保持开放。
2. `retry` 是非终局探测请求。探测仍失败时复用同一 Interrupt、轮换 nonce 并升级；达上限后保持 `hold`。
3. 探测确认旧执行体消失时，以一个 CAS 事务完成：记录消失证据摘要、写 `attempt_resolution=retry_after_absence` 与旧 attempt 终结事实、解除隔离、关闭 Interrupt、Run `waiting_human → queued`、创建新 `pending` attempt 与 claim、写启动及指令回执 outbox operation、追加事件。任一前置版本变化则整笔拒绝并重算。这里关闭 Interrupt 的是**探测成功结果**，不是 `retry` 请求本身。

## 理由

自动升级不是人的决定。若升级上限写 marker，定时器会关闭迟到事实窗口并终止一个可能合法启动的 Agent。

retry 成功若分多事务提交，会留下“Interrupt 已关但没有新 attempt”“Run 已 queued 但启动未入 outbox”等崩溃窗口。把它作为一次状态收敛事务，才能复用现有 outbox 与恢复不变量。

## 放弃的选项

| 选项 | 放弃理由 |
|------|----------|
| 升级上限自动终局 | PRD 要求 `startup_stall` 落 `hold`，且执行体可能仍在跑 |
| retry 成功后保持 `waiting_human` 直到 started | Interrupt 已关闭却仍显示等待人，语义失真且可能静默挂起 |
| 为探测新增 Interrupt 状态机 | 不需要；请求在途用幂等操作与 CAS 前置即可表达 |
| 分事务创建 attempt 与关闭 Interrupt | 引入不可恢复的部分提交窗口 |

## 后果

- 状态机必须支持 `waiting_human → queued` 的 retry 回边。
- V2/V4 必须逐边界崩溃注入，并覆盖升级上限后迟到 `started` 仍事实优先。
- `attempt_resolution` 的取值必须枚举；V0 为 `reject | retry_after_absence`。
