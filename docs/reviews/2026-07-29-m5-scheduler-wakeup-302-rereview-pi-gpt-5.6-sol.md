FAIL

# M5 #302 scheduler / outbox wakeup 定向复审

评审基线：`359b52900f162eaafaafbf57b01476813df77db7`，分支 `feat/issue-399-rereview-302-after-luna-p1-tests-397`。本次按 Issue #399 对照前次 FAIL 报告的 P1-1/P1-2，核对 Issue #397、合入 PR #398 的正文、完整 diff、合入提交与 required checks；PR 无 review/comment。只出报告，不修改代码、规格或 WBS。

## 裁决

#397 补上了两条写口经过 production `startSchedulers` 与真实 comment worker 的测试形状，也补上了并发 commit/wake 收敛测试；但 **P1-1 的关键排除条件仍未成立，P1-2 的 production 职责/步频证据仍基本缺失**，因此 #302 仍不能核销。

`cmd/siftd/main_test.go` 在调用 `startSchedulers` 后立即提交 operation。`startSchedulers` 异步启动三个 `runScheduler`，而每个 `runScheduler` 都会执行一次 startup `Wake()`；测试没有 barrier 证明 outbox 的 startup wake 已经完成并观察过空队列。因此 startup wake 完全可能发生在提交之后并推进 operation。把周期设为 10 秒只能排除 periodic tick，不能排除关闭动作明确禁止作为证据来源的 startup wake；测试中的“only post-start edge available”注释并不成立。

新增 `TestNamedSchedulersKeepWakeupsIndependent` 只在 storage 层手工构造三个同构 scheduler 并分别调用 `Wake()`。它不经过 `startSchedulers`，没有时钟，也没有连接 `Daemon.IntakeTick`、`TerminationCoordinator.Timeout` 或 `Daemon.OutboxTick`，所以无法证明 production callback 映射、独立步频、cursor due skip，或 supervisor 不驱动 Intake/Outbox。WBS 将它表述为“职责隔离”证据仍超出实际覆盖。

## 剩余可执行 P1

### P1-1：使 production commit-wake 证据与 startup wake 可判别

在 production seam 中增加有界 ready/drained barrier 或可观测 hook：先启动 `startSchedulers`，等待 outbox startup recovery 完成且确认队列为空，再分别执行 `EnqueueOperation` 与 `EmitInterrupt`；在远大于 deadline 的 periodic interval 下断言真实 `Daemon.OutboxTick` kind worker推进。测试还应能在移除/破坏 `DB.SetOutboxWakeup(outbox.Wake)` 时确定失败，而不是由迟到的 startup wake 偶然通过。

### P1-2：补 production 三组职责与独立时钟证据

通过可注入 clock/scheduler factory 或等价有界 hook，测试 `startSchedulers` 的真实接线：

1. Intake edge 只调用 `Daemon.IntakeTick`，并以持久化 `NextPollAtMS` 证明未到期 cursor 被跳过；
2. Supervisor edge 调用当前 stale-heartbeat/liveness `Timeout`，且不调用 Intake/Outbox；
3. commit outbox wake 只调用 `Daemon.OutboxTick`，不调用 Intake/Supervisor；
4. 三个 interval/edge 可独立推进，而非仅证明三个手工 scheduler 的 channel 互不共享。

现有 `TestOutboxWakeupConvergesConcurrentCommits` 可作为 race 下最终推进证据保留；它不能替代以上 production mapping/clock 断言。

## P1 对账

| 前次 P1 | 结论 | 证据 |
|---|---|---|
| P1-1：production `startSchedulers` 下 Enqueue/EmitInterrupt → 真实 Outbox worker，且非 startup/periodic | **NO** | 两条写口和真实 comment worker已覆盖，10 秒 interval 排除了 periodic；但 `startSchedulers` 后无 startup-drained barrier，`runScheduler` 的 startup `Wake()` 可在提交后执行。 |
| P1-2：三组职责、独立步频及 race 并发 wake | **NO** | 并发 commit/wake 收敛已有 race 测试；storage 手工 wake 测试没有 production callbacks/clocks，未覆盖 Intake cursor、Supervisor scan 或 production 隔离。 |
| WBS/outbox 生产证据校正；不虚假勾选 HITL §5.3 | **PARTIAL** | outbox 已正确承认旧 storage 测试不证明 production wiring，也列出两条写口；但 WBS 的“职责隔离”仍夸大新增测试，且 production 测试仍无法排除 startup wake。M5 §5.3 相关项保持未勾选，无虚假 HITL 完成声明。 |

## 已确认的正向证据

- `cmd/siftd/main_test.go` 确实经过 production `startSchedulers`、`Daemon.OutboxTick` 与 `forgeworker.CommentWorker`，并分别构造 `EnqueueOperation` 和 `EmitInterrupt`。
- `internal/storage/scheduler_test.go` 的并发用例让 16 个 goroutine 提交 operation，并验证最终全部 claim；本地 `-race -count=20` 通过。
- 三个 production ticker 仍集中在 `cmd/siftd.runScheduler`；`rg 'time\.(NewTicker|Tick)' --glob '*.go'` 未发现新增散落 worker ticker。另一个命中是既有 wrapper heartbeat ticker。
- `docs/WBS.md` §5.3 的 HITL expiry/escalation 工作仍未勾选；本次文档没有把 `termination.Timeout` 虚报为完整 HITL 能力。

## 执行证据

- PR #398 required checks：四平台 build、schema drift、vet + test，全部 **SUCCESS**；PR CI 未执行 race。
- `go test ./cmd/siftd ./internal/storage ./internal/daemon ./internal/intake`：**通过**。
- `go test -race ./cmd/siftd ./internal/storage ./internal/daemon ./internal/intake`：**通过**。
- `go test -count=50 ./cmd/siftd -run '^TestProductionSchedulerWakesOutboxAfterEnqueueAndEmitInterrupt$'`：**通过**；重复通过不消除 startup/commit 两种 wake 来源不可判别的问题。
- `go test -race -count=20 ./internal/storage -run '^(TestNamedSchedulersKeepWakeupsIndependent|TestOutboxWakeupConvergesConcurrentCommits)$'`：**通过**。

## Issue #399 验收清单

- [x] 自 GitHub forge 获取并阅读 #399 全文、Agent 建议、范围、产出与约束。
- [x] 获取 #397、#302 全文与评论，并核对 PR #398、合入提交、diff 与 checks。
- [x] 对照前次报告逐项复审 P1-1/P1-2、文档表述与 HITL §5.3 状态。
- [x] 只新增本评审报告，未修改代码、规格或 WBS，未自修。
- [x] 报告首行为 `FAIL`，并列出剩余可执行 P1。
- [ ] 前次 P1-1/P1-2 全部关闭：**NO**。
- [ ] #302 可核销：**NO**。

## 结论

**FAIL。** 新增测试证明了两条写口可以在当前实现中到达真实 outbox kind worker，也证明了 storage seam 下并发 wake 最终收敛；但 production 测试没有排除迟到的 startup wake，职责测试也没有覆盖 production callback 与独立时钟。完成上述两个 P1 后再复审 #302。
