FAIL

# M5 #475 AdvanceInterrupt after #457 定向评审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：#457 / PR #472，合入提交 `f44ff6a`（实现提交 `83e3ab5`）
> 评审基线：`main` `f44ff6a`
> 判定基准：[`interrupt.md` §4、§8–§9](../specs/interrupt.md)、[`storage.md` §6.1–§6.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)、wave1 I4

## 1. 结论

**FAIL。** #457 已把 Supervisor 生产 tick 接到 expiry/dispatch 两个持久谓词，并让候选只经 `AdvanceInterrupt` 的 version/nonce CAS 推进；单次升级会轮换 nonce，重复旧 CAS 不会重复写预算，发射器也在运行时拒绝 `startup_stall` 的两种 `auto_reject` 配置。这些方向正确。

但 I4 尚未形成可送达、可按冻结配置恢复的推进闭环：dispatch CAS 会永久消费 due marker，却不创建任何 delivery 或 `channel_publish` operation；升级进入 critical 时完全绕过 mandatory critical admission/fuse；多次升级的 severity、低/普通级重排和 reason expiry/on-max 配置均不符合冻结配方；auto-reject 还绕过唯一 Run transition 端口。另有 migration/schema 不能安全承载既有 Interrupt 与 `startup_stall` 双重禁拒绝的问题。遗留 **5 个 P1**，不能核销 #457 / wave1 I4。

本轮遵守“禁止自修自审”，只新增评审报告，不修改被评审代码或规格。

## 2. P1 findings

### P1-1：dispatch 成功 CAS 消费 due marker，却不原子创建 Channel delivery/operation

`internal/storage/advance_interrupt.go:56-67` 只把 due Interrupt 从 `ready` 改成 `batched`、清空 `next_dispatch_at_ms`、递增 version 并写事件。事务没有插入 `interrupt_deliveries` 或 immutable `channel_publish` outbox operation，也没有调用任何 sealing/入批端口。注释声称发布归 Channel worker，但 worker 只能消费已存在 operation，不能从已被清空的 marker 重建领域副作用。

这不是可留待后续的展示增强：初发 immediate、到时 batch/next-window，以及升级后的 immediate 都会被该 CAS 永久吞掉。它也延续了 [#469 P1-1](2026-07-29-m5-channel-webhook-worker-469-pi-gpt-5.6-sol.md#p1-1worker-与-producer-均未接入生产路径) 的 producer 缺口，违反 interrupt §8.2 “推进及其必要 Channel operation”必须在同一写端口提交的崩溃恢复边界。

**关闭条件：** `AdvanceInterrupt` 按冻结 delivery/channel/target 创建单条 immutable delivery + operation，或按规范进入唯一 batch/member/sealing 协议；Interrupt CAS、projection、outbox 和事件同事务提交，重放只返回既有 identity。不得先清 marker、再由 worker 猜测补写。

### P1-2：升级首次进入 critical 完全绕过 hard fuse/admission

`internal/storage/advance_interrupt.go:94-114` 直接写 critical severity 和 immediate marker；事务既不查询 sliding global/per-Run window，也不写唯一 `critical_admitted|critical_fused` admission，更不在 fused 时进入唯一 critical batch。仓库中的 `attention_admissions`/critical batch 也尚无该推进路径实现。

`critical_fuse` 在 config §3.9 是 critical 通道的硬安全边界，并明确要求升级首次进入 critical 与初发使用同一 admission。把它当作普通 follow-up 会允许到期升级无限绕过全局/per-Run 阈值，故本轮按阻断 **P1**，不是 note。

**关闭条件：** 在升级 CAS 同事务，以 `<interrupt_id>:critical` 做唯一 admission，执行半开滑窗和 global 优先规则；admitted 创建该 escalation 的 strong delivery，fused 进入唯一 critical batch；重放不新增 admission、charge、member 或 operation。

### P1-3：冻结 reason expiry/on-max 配置没有生产映射，migration 还会改变既有对象语义

有效 `config.Attention` 根本没有 `reason_defaults` 字段；生产 Gate caller只传 `MaxEscalations`（`internal/gate/interrupt.go:12`），其他调用也没有逐 reason 传入冻结值。发射器因此从内部 template 取 `expires/on_expire`，并把所有省略的 `OnMaxEscalations` 硬编码为 `hold`（`internal/storage/interrupt.go:348-357`）。例如规格默认应在封顶 `auto_reject` 的 `agent_blocked` 会被持久化为 `hold`，配置覆盖也无法生效。

迁移 `0011_advance_interrupt.sql:2-7` 又把既有行的 `expires_after_ms` 回填为 `1`、`on_max_escalations` 回填为 `hold`、`base_severity` 回填为 `normal`，并允许 `nonce_issued_at_ms=NULL`。升级已有数据库后，open Interrupt 下一次升级会以 1ms 续期并可能采用错误 severity/on-max；这不是“创建时冻结”的可恢复快照。

同一 migration 也没有新增 `CHECK (reason <> 'startup_stall' OR on_max_escalations <> 'auto_reject')`。初始 schema 只约束 `on_expire`，所以 `startup_stall` 的 on-max 禁拒绝只有 Go emitter 一层，不满足 interrupt §4.1 要求的 schema + emitter 双重拒绝。

**关闭条件：** 实现 closed `reason_defaults` 的 decode/default/normalize/canonical snapshot，并由每个生产 Emit 路径冻结四项 expiry 输入；为历史行提供语义明确、可验证的 forward migration（无法可靠推导时 fail closed，而非伪造 1ms）；将 `nonce_issued_at_ms` 与 startup on-max 约束落到数据库，并覆盖升级既有 DB。

### P1-4：升级配方不会按 escalation count 继续提升，也不会重算低/普通级 batch

`base_severity` 在创建时保存的是已经应用初始 `EscalationCount` 的 severity（`internal/storage/interrupt.go:367-396,484`），而每次 expiry 都固定执行一次 `promoteSeverity(base_severity)`（`advance_interrupt.go:94-97`）。因此 `max_escalations=2` 的第二次升级仍得到第一次的 severity，而不是按 count 继续提升到下一档；critical admission 也可能永远不触发。

当冻结 downgrade 令升级结果为 low/normal 时，`:98-107` 一律置 `held/batch_after_expiry`。interrupt §8.2 要求此时重算升级后首个 daily summary，只有该 summary 不早于新 expiry 才 held；实现既未冻结重算所需 zone/summary 输入，也没有计算路径。

**关闭条件：** 以创建时领域 base + `escalation_count+1` 重算唯一 severity，再复用冻结 downgrade；high/critical 强制 immediate，low/normal 使用冻结日历求下一 summary 并仅在越过新 expiry 时 held。覆盖 count 0/1/2、downgrade、第二次进入 critical 和 DST/expiry 边界。

### P1-5：expiry auto-reject 绕过唯一 Run transition 端口，且零行 Run 更新仍提交关闭

`internal/storage/advance_interrupt.go:117-127` 直接 `UPDATE runs SET status='failed'`，没有调用 private `transition()`，因此缺少规范 `run.transitioned` 事件并破坏 storage §1.4 的唯一状态写者不变量。该 UPDATE 只限定 `status='waiting_human'`，却不检查 `RowsAffected`；若 Run 已处于其他合法 source variant 状态，事务仍会关闭 Interrupt 并记录 `interrupt.expired_auto_reject`，而 Run 不收敛。

**关闭条件：** 由 reason/effect binding 选择唯一合法 auto-reject 状态机，通过 restricted port 复用 `transition()` 的 expected-version CAS 与事件；Run CAS 零行必须使整笔 Interrupt close 回滚。覆盖 waiting_human、Report no-transition variant、stale Run/version 和重放。

## 3. 正向确认与测试缺口

- Supervisor 生产接线：**YES**；`cmd/siftd/main.go:155-163` 在 termination tick 后调用 `SupervisorInterruptTick`。
- expiry 谓词：**YES**；包含 `held/manual`，排除其他 held 与 probe。
- dispatch 谓词：**YES**；只取 open/ready/due。
- 候选只经 `AdvanceInterrupt`：**YES**。
- version + nonce stale CAS：**YES（单连接事务下）**；旧请求返回 `ErrRejectedStale`。
- 升级 nonce 轮换、`nonce_issued_at_ms` 与 version 同 CAS：**YES（新写入行）**。
- 不重复 attention charge：**YES**；推进路径不写 `budget_entries`，现有 stale test 保持一条 charge。
- `startup_stall` 运行时禁两种 auto-reject、封顶 hold：**YES**；数据库 on-max 双保险：**NO**。

新增测试只覆盖直接单次升级、Supervisor dispatch 状态变化和 startup_stall 封顶。缺失 Supervisor expiry/restart、hold/auto-reject、第二次升级、config freeze、critical fuse、delivery/outbox 原子性、dispatch/expiry 同刻、migration upgrade 与 CAS 失败全部副作用计数；当前 dispatch 测试反而把“无 operation 的 `batched`”当作成功。

## 4. 执行证据

- 已从 GitHub forge 读取 #475 全文与 comments（无评论）、#457 全文与两条 comments、PR #472 元数据及完整 diff。
- `git diff 85e2530..f44ff6a --check`：**通过**。
- `go vet ./internal/storage ./cmd/siftd`：**通过**。
- `go test ./internal/storage ./cmd/siftd`：**通过**。
- `go test ./...`：**失败**；仅 `internal/launchworker/TestLaunchWorkerKilledAtHandoffBoundaries/prepare` 出现既有 crash-suite 时序失败（期望 `agent-started` 1 行，实际 0）。该失败不在 #457 变更范围，也不能弥补上述定向缺口。

## 5. Issue #475 验收清单

- [x] 在检测到的 GitHub forge 获取并阅读 #475 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #475 comments：**YES（无评论）**
- [x] 获取并核对 #457、comments、PR #472、合入提交与完整 diff：**YES**
- [x] 对照 AdvanceInterrupt CAS、expiry/dispatch 扫描、nonce/不重复收费、startup_stall 禁 auto_reject：**YES**
- [x] critical fuse / Channel enqueue 按规格阻断性分类：**YES（均为 P1）**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增报告：**YES**
- [ ] #457 / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO（5）**

## 6. 最终裁决

**FAIL。** 需先修复 P1-1 至 P1-5，再由不同代理复审；在此之前不得把 #457 描述为已完成可恢复的 AdvanceInterrupt delivery/expiry 闭环。
