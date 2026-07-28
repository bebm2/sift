# outbox.md 字段级评审

> 日期：2026-07-28
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/outbox.md`](../specs/outbox.md) 初稿
> 依据：DESIGN §6.3–§6.5、ADR-003/010/011、active storage/control-plane/config spec

## 1. 结论

**通过。** 当前候选稿可转 `active`。八类 operation 均有 closed payload、稳定 key、证据语义、错误分类和崩溃收敛；未发现遗留阻断项。

## 2. 发现与核销

| 项 | 级别 | 发现 | 处置 |
|----|------|------|------|
| O1 | P1 | `executing` lease 过期后 claim 只接受 pending/retryable，会永久卡住 | claim CAS 可直接 reclaim 过期 executing；原 attempt 原子补 `lease_expired` result，旧 owner complete 失败 |
| O2 | P1 | launch operation key 不含 generation，换代后无法在 payload 不可变前提下重发 | key 加 generation；旧 operation stale，新 generation 创建新 operation |
| O3 | P1 | Complete 端口只能写 outbox/delivery，无法原子承接 Create/Merge 投影、项目隔离或 conflict HITL | 扩为 `CompleteOutboxAttempt(expectedLease,outcomeCommand)`，kind-specific 投影/event/Interrupt/后继 operation 同事务 |
| O4 | P2 | payload 内存含 payload digest marker 会形成 digest 自引用 | payload 不存 marker；worker 从 operation key + 已冻结 digest 确定生成 |
| O5 | P2 | Forge 收费幂等键未定义 | 固定为 `forge-call:<outbox_attempt_id>:<call_seq>`，每次查询/动作调用前收费 |

## 3. 逐类核对

- comment/ack/alert：不可见 marker 同时绑定 operation key 与 payload digest，全分页查询后才允许创建。
- labels：set 语义保留非 Sift 标签，本地 projection version 过期则 stale。
- create Change：跨 open/closed/merged 搜索；同 base/head 非本 marker 对象 conflict，绝不接管。
- merge Change：expected head 同时进入 Gate、payload、operation key 与远端条件请求；预读不替代 CAS。
- Channel：如实为 at-least-once，失败阈值只生成一个 forge alert且不递归。
- launch：prepare/bootstrap/spawn/acquire 各崩溃窗口有唯一动作；acquire 与 launch operation complete 原子提交。

## 4. 通过条件

- payload trigger、immutable attempts/results 与旧 lease owner CAS 可由 storage 直接实现。
- auth/capability、contract、conflict、stale 均为领域终态，不被通用退避误重试。
- fake adapter 可以确定注入全部结果分类；M1 不需真实 Forge 即可验证 worker 核心。

结论：允许 `outbox.md` 转 `active`，下一步进入 `brain.md`。
