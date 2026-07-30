# M5 #810 startup_stall probe process-check worker · Sol 复审

> 日期：2026-07-30
> 评审人：pi × DeepSeek V4 Pro（Sol role）
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#810 / PR #811，实现提交 `b7f531c`，合入提交 `fa70334`
> 评审基线：`chore/issue-810-review` @ `b7f531c`
> 判定基准：[`storage.md` §5.5](../specs/storage.md) `attempt_probes`、[`command.md` §5](../specs/command.md) startup_stall two-phase、WBS §5.4 probe 进程检查 worker 叙事

## 1. 结论

**PASS。** P0/P1 全部关闭。probe process-check worker 可核销为本薄切片。

- **Supervisor tick（非 outbox）**：`ProbeProcessCheckCoordinator.Tick` 扫描 `pending|running` → `ClaimRetryProbe` pending→running CAS（崩溃可续跑）→ 事务外 `ProcessInspector` 观测 + 有界 absence recheck（**不发信号**）→ 唯一 `ApplyRetryProbeResult` finalizer。
- **身份**：复用导出的 `runtime.SameIdentity`；身份缺失/不匹配 fail-closed → `Succeeded=false`。
- **daemon 接线**：siftd scheduler 在 `SupervisorInterruptTick` 之前跑 probe tick（failed→batched 可同 tick 升级）。
- **证据**：success / present-failure / identity-mismatch / missing-identity / crash-replay / stale-swallow 测试；`-race` 相关包绿。
- **诚实 WBS**：once-charge 框保持 `[ ]`；不宣称 M5 门禁闭合。

**不读作** once-charge 全生命周期、gate_re_eval HITL/rerun/ready-merge 后继、T7 生产调用壳、`ProcessGroupVerified` 全矩阵 / M6 恢复矩阵、或 M5 门禁闭合；不启动 #748+。

## 2. Findings（Scope gate：仅记 P2，不实施）

| 级别 | 数量 | 本轮是否实施 |
|---|---|---|
| P0 | 0 | — |
| P1 | 0 | — |
| P2 | 3 | 否（记录） |
| DEFER | 0 | — |

### [P2] Stuck running probe on externally closed interrupt

`ErrRejectedStale` 吞错后 probe 可停在 `running`，每 tick 重复 observe。`fixer=same`

### [P2] `ExpectedGeneration` fetched but unused

candidate 读取 generation 但未传入 `RetryProbeResultCmd`。`fixer=same`

### [P2] Comment mentions `superseded` without writer

`PendingRetryProbes` 注释提及 superseded，代码无写口。`fixer=same`

## 3. Scope summary

P0=0 / P1=0 / P2=3（不实施）/ DEFER=0。Verdict：**PASS**。
