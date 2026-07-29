PASS

# M5 #302 scheduler / outbox wakeup 第二次定向复审

评审基线：`488f6ac0ec3e1d50c8bae4386a77e8bbadb1f355`，分支 `feat/issue-403-rereview-2-302-after-terra-p1-401`。本次按 Issue #403 对照前次 FAIL 报告 `2026-07-29-m5-scheduler-wakeup-302-rereview-pi-gpt-5.6-sol.md` 的 P1-1/P1-2，核对 Issue #401、合入 PR #402 的正文、完整 diff、合入提交与 required checks；PR 无 review/comment。只出报告，不修改代码、规格或 WBS。

## 裁决

#401 已关闭前次两个 P1。Outbox scheduler 现在由 `startScheduler` 启动，并在 `startSchedulers` 返回前通过 `WakeAndWait` 完成首轮真实 `Daemon.OutboxTick`；测试在该 barrier 后才提交 operation，10 秒周期远大于 750 ms 断言窗口。`EnqueueOperation` 与 `EmitInterrupt` 均经 production `startSchedulers`、真实 outbox callback 和真实 comment worker 推进；移除 `SetOutboxWakeup` 后同一正向测试确定超时失败，迟到 startup wake 不再能代偿。

`startSchedulersWithFactory` 在 production callback 的同一调用点分别绑定 `Daemon.IntakeTick`、`TerminationCoordinator.Timeout` 和 `Daemon.OutboxTick`。定向测试以三个独立 factory scheduler edge 逐一推进：Intake edge 保持未来 `NextPollAtMS` cursor 且不访问 Forge，Supervisor edge 不触发 Intake/Outbox，Outbox edge 不触发另外两组。生产路径仍为每组 scheduler 分别创建 `runSchedulerClock`，没有恢复单 ticker 驱动全部 worker。

WBS 已把旧 storage 测试降格为并发 wake 的 storage seam 证据，并把 production mapping、cursor due-skip、startup drain 与两条写口证据归到 `cmd/siftd/main_test.go`；outbox 规格同步写明 barrier。M5 §5.3 的 T7/HITL 抑制、收费、升级、Command 等未实现项仍保持未勾选，没有把本次 supervisor wiring 虚报为完整 HITL 能力。

因此前次 P1-1/P1-2 均已核销，**#302 关闭标尺已满足**。

## P1 对账

| 前次 P1 | 结论 | 证据 |
|---|---|---|
| P1-1：production commit-wake 与 startup wake 可判别 | **YES** | `Scheduler.WakeAndWait` + `startScheduler` 建立 startup-drained barrier；两条写口在 barrier 后经真实 worker 推进；10 秒周期排除 periodic；断开 hook 的负向用例在窗口内保持 pending。对 production 注册点做移除 mutation 后，正向测试确定失败。 |
| P1-2：三组 production 职责、独立时钟、cursor due-skip | **YES** | production factory 分别封装 `IntakeTick` / `Timeout` / `OutboxTick`；三个独立 scheduler edge 可逐一推进且 callback 不串联；未来 `NextPollAtMS` 保持不变且 Forge poll 次数为 0；每组各自持有 `runSchedulerClock`。既有并发 wake race 用例继续保留。 |
| WBS/outbox 表述校正；不虚假勾选 HITL §5.3 | **YES** | WBS 不再把 storage channel 测试称作 production 职责证据；outbox 明示 startup barrier 与负向证据；M5 §5.3 相关未实现项仍为 `[ ]`。 |

## 执行证据

- PR #402 required checks：四平台 build、schema drift、vet + test，全部 **SUCCESS**。
- `go test ./...`：**通过**。
- `go test ./cmd/siftd ./internal/storage ./internal/daemon ./internal/intake`：**通过**。
- `go test -race ./cmd/siftd ./internal/storage ./internal/daemon ./internal/intake`：**通过**。
- `go test -count=30 ./cmd/siftd -run '^(TestProductionSchedulerWakesOutboxAfterEnqueueAndEmitInterrupt|TestProductionSchedulerCommitWakeCannotPassFromStartupRecovery|TestStartSchedulersKeepsProductionEdgesIndependent)$'`：**通过**。
- `go test -race -count=20 ./internal/storage -run '^(TestNamedSchedulersKeepWakeupsIndependent|TestOutboxWakeupConvergesConcurrentCommits)$'`：**通过**。
- Mutation：临时移除 production `db.SetOutboxWakeup(outbox.Wake)` 后，`TestProductionSchedulerWakesOutboxAfterEnqueueAndEmitInterrupt` 在 750 ms 窗口确定失败；随后已恢复工作树。
- `git diff 359b529..488f6ac --check`：**通过**。

## Issue #403 验收清单

- [x] 自 GitHub forge 获取并阅读 #403 全文、Agent 建议、范围、产出与约束。
- [x] 获取 #401、#302 全文与评论，并核对 PR #402、合入提交、完整 diff 与 required checks。
- [x] P1-1：startup drain 后两条 commit-wake 可判别，破坏 `SetOutboxWakeup` 时确定失败：**YES**。
- [x] P1-2：production 三组职责、独立时钟及 cursor due-skip 证据：**YES**。
- [x] WBS/outbox 表述已校正，且没有虚假勾选 HITL §5.3：**YES**。
- [x] 只新增本评审报告，未修改代码、规格或 WBS，未自修。
- [x] 报告首行为 `PASS`。
- [x] #302 关闭标尺已满足：**YES**。

## 结论

**PASS。** #401/PR #402 已关闭前次 P1-1/P1-2，文档证据边界与 HITL 未完成状态准确；#302 可按既定关闭标尺核销。
