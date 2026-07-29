FAIL

# M5 #487 AdvanceInterrupt after #481 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#481 / PR #484，合入提交 `43da96f`（实现提交 `6f16ab5`）
> 评审基线：`main` `43da96f`
> 前次结论：[#475 FAIL](2026-07-29-m5-advance-interrupt-475-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §4、§8–§9](../specs/interrupt.md)、[`storage.md` §6.1–§6.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#475 的 5 个 P1 均未完整关闭。** #481 增加了 Channel operation/delivery、reason defaults 配置结构、按 count 重算 severity、critical admission 壳和 `transition()` 调用，方向上覆盖了五项标题；但生产闭环仍不符合冻结规格：Channel payload 由 ID 伪造而非复用冻结 snapshot，critical 路径没有 per-Run fuse 或 critical batch 且 fused 分支会因 SQL 参数数错误直接失败，配置只接到 Gate，升级后的 low/normal 仍不重算 summary，auto-reject 仍不支持 Report no-transition variant。既有危险 migration 也未修订。

本轮遵守“禁止自修自审”：评审者未参与 #481 实现，只新增本报告，不修改被评审代码或规格。

## 2. #475 P1 关闭对账

### P1-1 Channel enqueue：**NO**

`AdvanceDispatch` 现在把 Interrupt CAS、operation 与 delivery 放进同一事务，这是正向改进；但提交的对象不是规格要求的冻结投影：

- `internal/storage/advance_interrupt.go:165-179` 只读取 `channel_id/min_modality/headline/brief`，随后硬编码 `type=webhook`、`target_ref=secret_ref:<channel_id>`、`renderer=plain-v1`，并只把 `{id}` 写入 `channel_snapshot_json`。这会丢失创建时的 type、target_ref、capabilities、renderer，甚至把 channel ID 当 secret 名。`interrupts` schema 仍没有 `channel_snapshot_json`，故重启后无权威 snapshot 可复用。
- dispatch CAS 在 `:65` 把 version 加一，delivery 却在 `:70` 冻结旧的 `ExpectedVersion`；operation/delivery 不对应提交后的 Interrupt version。
- `:65` 将单条 delivery 的对象写成 `dispatch_state=batched`，却没有 `attention_batches/attention_batch_members` membership；这违反 storage §6.1 的“batched 必有 batch membership”。
- `internal/storage/advance_interrupt_test.go:51-80` 只断言状态与 due marker 清除，不断言 immutable payload、snapshot、version、delivery/operation identity 或原子回滚。

因此原先“吞 due marker”虽被局部替换，仍未形成可按冻结 Channel snapshot 恢复的合法 delivery。

### P1-2 critical fuse/admission：**NO**

`admitCriticalTx` 创建了唯一 `<interrupt_id>:critical` evidence 并使用半开窗口左边界查询，但 mandatory fuse 仍未实现：

- `CriticalPerRunLimit` 从未被读取；`:205-213` 只计算全局 count，没有 per-Run count，也没有“global 优先、per-Run 次之”的 scope 选择。
- production supervisor 在 `:237-253` 只构造 ID/version/nonce/kind/now；没有传入启动期冻结的 fuse 配置。`:197-203` 因而静默使用硬编码默认值，配置覆盖不会生效，注释所称“production callers pass the limits”与实际接线相反。
- fused 时只写 `held_reason=critical_fuse`；没有创建唯一 critical batch/member、episode、due_at 或后续 sealing 路径。row 也没有提交本次 escalation count/severity/nonce/expiry 配方。
- 更直接地，fused UPDATE `:116` 有 4 个 SQL placeholder，却传入 6 个参数；命中 fuse 会返回参数数量错误并回滚，不能提交 `critical_fused`。
- admission 行没有规格要求的 severity、quota/dayzone 或复用 `attention_charge_entry_id`；`ensureChannelSchema` 仍是仅含 critical 两种 kind 的缩减表，不能承载 storage §6.3 的完整 append-only ledger。
- 没有 critical、per-Run、窗口边界、并发、fused batch 或重放副作用测试。

### P1-3 config freeze / migration / schema：**NO**

config decode/default/normalize 的 closed `reason_defaults` 已增加，Gate caller也会把匹配 reason 的三项值传给 emitter；但关闭条件要求的是每个生产 Emit 路径和安全升级 migration：

- 全仓仅 `internal/gate/interrupt.go:37-40` 消费 `Attention.ReasonDefaults`。termination/recovery、Report 及其他直接 emitter 仍依赖 `interrupt.go:366-373` 的内部模板与统一 `OnMaxEscalations=hold`；例如非 Gate `agent_blocked` 仍不会冻结 canonical `on_max_escalations=auto_reject`。
- migration `internal/storage/migrations/0011_advance_interrupt.sql:2-7` 完全未改：既有行仍被伪造为 `expires_after_ms=1`、`on_max_escalations=hold`、`base_severity=normal`，且 `nonce_issued_at_ms` 仍 nullable。
- schema 仍只有 `startup_stall/on_expire` 双保险，没有 `startup_stall/on_max_escalations` CHECK。Go emitter 的拒绝不能代替数据库约束。
- 没有从 0010 既有数据库升级并验证 open Interrupt 语义的 migration 测试，也没有逐生产 caller 的冻结测试。

### P1-4 多次升级与 low/normal summary：**NO**

`internal/storage/advance_interrupt.go:101-109` 改为从领域 base 按 `escalation_count+1` 重算，修正了“每次固定只提升一级”的核心计算；但其余阻断条件未关闭：

- `:123-131` 对升级后 low/normal 仍一律写 `held/batch_after_expiry`。它没有冻结/读取 day timezone 与 `daily_summary_at`，没有求升级后的首个 summary，也没有在 summary 早于新 expiry 时进入 `ready/batch`。
- critical fused 分支不提交升级后的 severity/count/nonce/expiry，故第二次进入 critical 的完整配方仍断裂。
- 测试仍只有一次 normal→high；没有 count 0/1/2、downgrade 后 low/normal、第二次 critical、summary before/after expiry、DST gap/fold 或 restart vectors。

### P1-5 canonical auto-reject transition：**NO**

`closeExpiredInterrupt` 已调用 private `transition()`，因此 waiting_human 路径可以获得 expected-version CAS 和 `run.transitioned` 事件；但它把所有 reason/effect variant 硬编码成同一前态与终态：

- `internal/storage/advance_interrupt.go:146-156` 强制 Run 必须是 `waiting_human`，并固定转为 `failed(hitl_expired)`；没有读取 `interrupt_command_effect_bindings` 来选择 reason/effect 的合法状态机。
- 规格明确允许 Report no-transition `failure_review` 绑定在 running Run。该 Interrupt 到期时这里返回 `ErrRejectedStale`，既不关闭也不按 canonical binding 收敛。
- 没有 waiting_human transition event、Report no-transition variant、stale Run/version、Interrupt CAS 失败导致整笔 transition 回滚或 replay 测试。

## 3. 回归与执行证据

- 已从 GitHub forge 获取并阅读 #487 全文与 comments（无评论）、#475、#481、PR #484 元数据和完整实现 diff。
- `git diff f05e7a6..43da96f --check`：**通过**。
- `go vet ./internal/config ./internal/gate ./internal/storage ./cmd/siftd`：**通过**。
- `go test ./internal/config ./internal/gate ./internal/storage ./cmd/siftd`：**通过**。
- `go test ./...`：**通过**。

现有绿灯只能证明已覆盖回归不失败；上述关键路径缺少验收测试，且 fused SQL 参数错误、Report variant 与 frozen snapshot 等静态反例仍成立。

## 4. Issue #487 验收清单

- [x] 在检测到的 GitHub forge 获取并阅读 #487 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #487 comments：**YES（无评论）**
- [x] 对照 #475 FAIL 报告、#481、PR #484 与合入 diff 逐项复审：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #475 的 5 个 P1 全部关闭：**NO（0/5 完整关闭；五项均有部分改进）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO（5）**

## 5. 最终裁决

**FAIL。** #481 不可按当前实现核销 #475 的五项 P1。需由实现方补齐冻结 Channel snapshot 与合法 delivery/batch、完整双阈值 critical admission/batch、全生产 caller config freeze 和安全 migration、升级 summary 日历，以及 binding-aware auto-reject；之后再由不同代理复审。
