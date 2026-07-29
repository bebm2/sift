FAIL

# M5 #302 scheduler / outbox wakeup 定向核销

评审基线：`4f4eba5c0c99b5e9929343e8c714f4406a67175d`，分支 `feat/issue-395-verify-302-scheduler-outbox-wakeup-wiring`。本次按 Issue #395 对照 #302 正文、关闭标尺与评论，核对合入 PR #394 的正文、完整 diff、合入提交及 required checks；PR 无 review/comment。只出报告，不修改代码或规格。

## 裁决

#302 **不能按当前证据核销**。生产代码已拆出 Intake / Supervisor / Outbox 三个具名 scheduler，也注册了提交后 outbox wakeup；但关闭标尺明确要求的 production wakeup 自动化证据、`EmitInterrupt` wakeup 证据及三组职责定向测试均缺失。现有唯一新增测试绕过 `cmd/siftd.startSchedulers`，在 storage 测试内自行调用 `SetOutboxWakeup`，只能证明 storage 骨架在手工接线后可由 `EnqueueOperation` 唤醒，不能证明生产接线。

## 剩余可执行 P1

### P1-1：补 production 接线路径的提交唤醒测试

`internal/storage/scheduler_test.go:9-43` 的 `TestOutboxCommitWakeupClaimsWithoutPeriodicTick` 直接构造 `NewOutboxScheduler`、直接注册 `db.SetOutboxWakeup(scheduler.Wake)`，随后直接调用通用 `ClaimOutboxOperation`。测试没有经过 `cmd/siftd.startSchedulers`（`cmd/siftd/main.go:106-116`），也没有经过 production `daemon.OutboxTick` 的 kind/project workers；`cmd/siftd` 当前无测试文件。因此它无法捕获 production 忘记注册 callback、注册到错误 scheduler、或 Outbox worker 未被 wakeup 驱动等回归。

可执行关闭动作：增加有界 production seam/集成测试，以远大于测试 deadline 的 `SupervisorInterval` 启动 `startSchedulers`，分别提交 `EnqueueOperation` 与 `EmitInterrupt`，断言真实 Outbox worker 在 deadline 内 claim/推进，且证据不能来自 startup wake 或 periodic tick。两条写口都必须覆盖，因为 `EmitInterrupt` 是本 issue 所述 M5 发布路径。

### P1-2：补三组职责与独立步频的自动化证据

PR #394 没有测试 `startSchedulers` 创建三组独立 scheduler，也没有测试 Intake 与 Supervisor 的生产 callback 分离。现有新增测试只覆盖一个 storage Outbox scheduler。关闭标尺 1、3、5 因而主要依赖源码阅读，不满足“定向测试充分”。

可执行关闭动作：用注入时钟/有界 hook 断言：

1. Intake clock 只调用 `Daemon.IntakeTick`，`PollOnce` 仍按持久化 `NextPollAtMS` 跳过未到期 cursor；
2. Supervisor clock 调用当前已实现的 stale-heartbeat/liveness scan，且不驱动 Intake/Outbox；
3. Outbox commit wake 不触发 Supervisor 或 Intake，三者互不串联；
4. race 下并发 commit/wake 不丢失最终推进。

M5 的 HITL expiry/escalation 实现仍在 `docs/WBS.md` §5.3 明确未勾选，本次不能把 `termination.Timeout` 描述为该能力已经生产生效；后续实现应接入同一个 Supervisor scheduler，不得再建 ticker。

## 关闭标尺对账

| #302 关闭标尺 | 结论 | 证据 |
|---|---|---|
| 1. 三组独立具名调度器，非单 ticker 驱动全部 worker | **YES（代码）/ NO（定向测试）** | `startSchedulers` 分别构造 Intake/Supervisor/Outbox；`Daemon.IntakeTick` 与 `OutboxTick` 已拆分，三个 `runScheduler` 独立运行。但无 `cmd/siftd` 测试。 |
| 2. production `SetOutboxWakeup`；Enqueue/EmitInterrupt 不待 SupervisorInterval | **NO** | production 确有 `db.SetOutboxWakeup(outbox.Wake)`，且两个写口均在 commit 后调用 `wakeOutbox`；但自动化仅手工接线覆盖 `EnqueueOperation`，未覆盖 production wiring、真实 worker 或 `EmitInterrupt`。 |
| 3. Intake 持久化自适应；Supervisor 保留扫描职责 | **YES（现有能力）/ NO（完整声明）** | `PollOnce` 读取 `NextPollAtMS`，Intake clock 取最小配置间隔；Supervisor 当前只接 `termination.Timeout` 的 stale-heartbeat scan。HITL expiry/escalation 尚属 WBS M5 §5.3 未实现项。 |
| 4. WBS/outbox 区分骨架与生产接线 | **YES（文字）/ NO（证据引用）** | 两处已区分骨架与接线，但都把不足以覆盖 production path 的 storage 测试称为生产证据，需随 P1-1 校正。 |
| 5. race/定向测试充分；无散落匿名 ticker | **NO** | 本地定向 race 通过，但 PR required CI 未运行 race，新增测试只有一个 storage 用例；生产三 scheduler 无测试。PR 新增的 daemon 时钟集中在 `startSchedulers/runScheduler`；仓库另有 wrapper heartbeat ticker，非本 PR 新增且不驱动 siftd worker。 |

## 已确认的实现事实

- `cmd/siftd/main.go:106-116` 构造三个不同 scheduler；单一 supervisor ticker 驱动 `workers.Tick()` 的旧路径已删除。
- `internal/daemon/daemon.go:122-192` 将 Forge fact intake/reconciliation 与 outbox workers 分开，并使用独立互斥锁。
- `internal/intake/poller.go:40-44,117-119` 仍以持久化 cursor 的 `NextPollAtMS` 判定 due，并持久化下一次 idle/active/slow 时点。
- `internal/storage/outbox.go:106-120` 与 `internal/storage/interrupt.go:151-312` 均在事务提交成功后才调用 `wakeOutbox`。
- `rg 'time\.(NewTicker|Tick)' --glob '*.go'` 只发现集中式 `cmd/siftd/main.go:126` 与独立 wrapper heartbeat `internal/wrapper/wrapper.go:451`；PR 未新增散落 worker ticker。

## 执行证据

- PR #394 required checks：四平台 build、schema drift、vet + test，全部 **SUCCESS**；CI 未执行 `-race`。
- `go test ./internal/storage ./internal/daemon ./cmd/siftd`：**通过**；`cmd/siftd` 显示 `[no test files]`。
- `go test -race ./internal/storage ./internal/daemon ./cmd/siftd`：**通过**；同样没有执行 `cmd/siftd` 测试。
- `go test -count=50 ./internal/storage -run '^TestOutboxCommitWakeupClaimsWithoutPeriodicTick$'`：**通过**。

## Issue #395 验收清单

- [x] 自 GitHub forge 获取并阅读 #395 全文、Agent 建议、范围、验收与约束。
- [x] 获取 #302 全文与评论，并核对 PR #394、合入提交、diff 与 checks。
- [x] 逐项核销三 scheduler、production wakeup、cursor/supervisor、文档与 race/ticker。
- [x] 只新增本评审报告，未修改代码、规格或 WBS，未自修。
- [x] 报告首行为 `FAIL`。
- [ ] #302 关闭标尺全部满足：**NO**。
- [ ] Issue #395 可判 PASS：**NO**。

## 结论

**FAIL。** 三组 scheduler 与 callback 的源码接线方向正确，本地定向/race 也未发现运行时竞态；阻断点是关闭标尺要求的 production-path 自动化证据不存在，尤其 `EmitInterrupt → commit wakeup → production Outbox worker` 未被测试。完成 P1-1/P1-2 后再核销；在此之前 WBS/outbox 中“生产证据”表述超出了测试实际覆盖范围。
