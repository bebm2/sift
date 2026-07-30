PASS

# M5 #727 AdvanceInterrupt Supervisor tick rereview after #723

> 日期：2026-07-30
> 评审人：pi × DeepSeek V4 Pro（Sol 角色）
> 评审对象：#723 / PR #726，合入提交 `debdf01`
> 评审基线：当前 worktree `09a4459`
> 前次结论：[#711 PASS WITH NOTES](2026-07-30-m5-advance-interrupt-711-rereview-pi-deepseek-v4-pro.md)
> 判定基准：[`interrupt.md` §8](../specs/interrupt.md)、[wave1 plan I4](../plans/2026-07-29-m5-attention-impl-wave1.md)

## 1. 结论

**PASS。#723 完成了 I4 SupervisorInterruptTick 的剩余测试覆盖（expiry hold/auto_reject、escalate→redeliver 双 tick、组合扫描），生产 seam（SupervisorInterruptTick 实现 + siftd 接线）在 #723 之前已就位。四个新增测试均以 3/3 无 flake 通过；stale CAS 由 `ErrRejectedStale` 吞噬与矩阵测试中的旧 tick 重放双重覆盖。**

本轮遵守禁止自修自审：只新增本报告，不修改实现、规格、测试或 WBS。

## 2. Must verify

### 2.1 Production scheduler/siftd calls only existing AdvanceInterrupt via SupervisorInterruptTick：**YES**

**siftd 接线**（`cmd/siftd/main.go:175`）：
```go
return db.SupervisorInterruptTick(ctx, time.Now().UnixMilli())
```
生产调度器的 supervisor 协程以 `cfg.SupervisorInterval` 周期调用，与 #302 生产调度器/提交唤醒的既有架构一致。无独立的 expiry/dispatch worker、无第二条 advance 路径。

**SupervisorInterruptTick 实现**（`internal/storage/advance_interrupt.go:671–697`）：
- 两条 SQL 谓词以 UNION ALL 合并扫描候选行（非两遍分别查/分别推进）
- 每条候选行调用 `d.AdvanceInterrupt(ctx, c)`，kind 分别标为 `"expiry"`/`"dispatch"`
- 吞掉 `ErrRejectedStale`，其他错误立即返回
- 尾调用 `d.PrepareDueAttentionBatches(ctx, now)` 密封到期 batch（非第二 advance 口；batch 生命周期函数，参见 §8.2 批次 sealing）

`AdvanceInterrupt` 本身（行 30–138）是 #705 已闭环的单一口径，处理 `AdvanceExpiry`（hold/auto_reject/escalate）和 `AdvanceDispatch`（ready→batched + channel op/batch member），无第三 kind。

**结论**：唯一入口→唯一口径，生产 seam 与 I4 规格一致。

### 2.2 expires_at + next_dispatch predicates covered; stale CAS skipped (no second advance path)：**YES**

**expiry 谓词**（行 672）：
```sql
WHERE status='open'
  AND dispatch_state!='probe_in_progress'
  AND (dispatch_state!='held' OR held_reason='manual')
  AND expires_at_ms<=?
```
与 [`interrupt.md` §8（行 304）](../specs/interrupt.md) 精确一致：包含 `held_reason=manual` 的显式 Command hold 对象，排除其余 held 和 `probe_in_progress` 对象。

**dispatch 谓词**（行 672）：
```sql
WHERE status='open'
  AND dispatch_state='ready'
  AND next_dispatch_at_ms<=?
```
与 spec §8（行 304）一致：只扫描 `ready` 态且有非空到期时点的对象。

**stale CAS 跳过多层覆盖：**
- `SupervisorInterruptTick` 循环吞掉 `ErrRejectedStale`（行 692）：已关闭/版本变更/旧 nonce 的对象不中断其余候选处理
- `AdvanceInterrupt` 入口双重校验（行 46–47）：`status != "open" || version != cmd.ExpectedVersion || nonce != cmd.ExpectedNonce` → `ErrRejectedStale`
- `AdvanceDispatch` 入口状态校验（行 52–53）：`state != "ready" || expiresAt <= cmd.NowMS` → `ErrRejectedStale`
- `AdvanceExpiry` 入口状态校验（行 77–78）：`probe_in_progress` / 非 manual held / 未到期 → `ErrRejectedStale`
- 矩阵测试 `advance_interrupt_matrix_test.go:643–652` 显式覆盖**重启后旧 tick 重放**：`db.Close()` → `db, err = Open(...)` → `SupervisorInterruptTick(testNow)` → 旧 version/nonce 的 `AdvanceInterrupt` 返回 `ErrRejectedStale`

**无第二 advance 路径**：tick 内只调 `AdvanceInterrupt` + `PrepareDueAttentionBatches`；无直写 `interrupts`、直插 batch member、直发 channel op 或绕过 CAS 的旁路。

### 2.3 Expiry hold/auto-reject and next_dispatch redelivery tests green：**YES**

| 测试 | 覆盖路径 | 关键断言 | 3/3 |
|------|---------|---------|-----|
| `TestSupervisorInterruptTickExpiryHoldRoutesHold` | expiry→hold | open/held/expiry, version=2, nonce 不变, interrupt.expired 事件, 1 admission/charge/channelOp | ✓ |
| `TestSupervisorInterruptTickExpiryAutoRejectClosesRunAndInterrupt` | expiry→auto_reject | closed/expired_auto_reject, version=2, nonce 不变, Run→failed/hitl_expired | ✓ |
| `TestSupervisorInterruptTickEscalatesThenRedelivers` | escalate→redeliver 双 tick | tick1: version=2, nonce 轮换, next_dispatch 重冻结; tick2: version=3/escalation=1, batched, channelOps=0, member=1/authority=1, nonce 保持 escalation 值 | ✓ |
| `TestSupervisorInterruptTickProcessesExpiryAndDispatchInOneCall` | 组合扫描 | 两个独立 Interrupt（一 expiry hold、一 batch dispatch）在同一 tick 被正确分流：hold→held/expiry/version=2, dispatch→batched/version=2, 事件 expired:1/dispatched:1, outbox_operations=4 | ✓ |
| `TestSupervisorInterruptTickDispatches`（既有） | 基础 dispatch | ready→batched, next_dispatch=NULL | ✓ |

**组合扫描测试的正确性注记**：
- dispatch Interrupt 使用独立 Run（`run2`），因为两个 `code_review` emit 在同一 Run 上会因 `ExpectedRunVersion` 冲突（首次 emit 将 Run 转至 `waiting_human` 后 version 递增，第二次 emit 的 `ExpectedRunVersion=1` 已过时）。此设计符合 `EmitInterrupt` 的 version CAS 语义，是测试 fixture 的正确做法，非生产代码 bug。
- outbox_operations=4 的分解：2 条 `forge_comment`（每个 Interrupt 一条）+ 1 条 immediate channel publish（hold Interrupt 的首次 delivery）+ 1 条 sealed daily batch publish（dispatch Interrupt 入批后的 `PrepareDueAttentionBatches` 密封）。

**escalate→redeliver 测试的关键覆盖**：
- tick1 升级后 delivery=`batch`（原始 severity=normal，T6 downgrade 后仍为 normal→batch），next_dispatch 重冻结为升级时刻后的首个 summary
- tick2 的 `AdvanceDispatch` 将 interrupt 加入 daily batch member（`addDailyBatchMemberAtTx`），不创建 channel operation（`channelOps=0`）；member 的 nonce/version 匹配升级后的 authority
- 升级前 initialNonce 与升级后 newNonce 不同，且 dispatch 后保持 newNonce（dispatch 不轮换 nonce）

## 3. 回归验证

- `go test ./internal/storage/ -run 'TestSupervisorInterruptTick' -count=3`：**通过（5 测试 × 3 轮 = 15/15，无 flake）**
- `go test ./internal/storage/`：**通过（20.301s）**
- `go test ./cmd/siftd/`：**通过（1.668s）**
- `go test ./...`：**全仓通过（全部 20 个包）**
- `git diff 1215b3d..debdf01 --check`：通过（无冲突空白）
- `git diff 1215b3d..debdf01 --stat`：仅修改 `internal/storage/advance_interrupt_test.go`（+236 行），无 migration、无 spec 变更

## 4. Issue #727 checklist

- [x] 获取并阅读 #727 全文、Must verify、constraints：**YES**
- [x] 获取并阅读 #723 及其 comments：**YES（#723 已 CLOSED；#726 PR 合入 `debdf01`）**
- [x] 验证 production scheduler/siftd 只调 SupervisorInterruptTick→AdvanceInterrupt：**YES**
- [x] 验证 expires_at + next_dispatch 谓词 + stale CAS 跳过 + 无第二 advance 路径：**YES**
- [x] 验证 expiry hold/auto-reject 与 next_dispatch redelivery 测试全绿：**YES**
- [x] 只写 `docs/reviews/`，未 push/MR/merge：**YES**
- [x] 目标包测试与全仓测试通过：**YES**
- [x] Verdict PASS | PASS WITH NOTES | FAIL；checklist YES/NO for I4：**YES**

## 5. 最终裁决

**PASS。** #723 补全了 I4 SupervisorInterruptTick 的四条核心测试（expiry hold、expiry auto_reject、escalate→redeliver 双 tick、expiry+dispatch 组合扫描），生产 seam（SupervisorInterruptTick + siftd 接线）在 #723 前已就位。expiry/dispatch 双谓词与 spec §8 精确一致，stale CAS 在 tick 循环层与 AdvanceInterrupt 入口层双重覆盖，无第二 advance 路径。全部测试 3/3 无 flake，全仓回归绿色。I4 checklist：YES。
