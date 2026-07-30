# M5 §5.7 指标/CLI/时间线 bootstrap 定向复审（#770）

**复审对象：** PR #769 / commit `01fd130`（merge `0b63a6c`），M5 wave-2 metrics / CLI / timeline bootstrap (§5.7)

**复审代理：** pi × DeepSeek V4 Pro

**复审日期：** 2026-07-30

**方法：** 逐项对照 [WBS §5.7](#ref-wbs)、[metrics.md](#ref-metrics-spec)、[control-plane.md §6.1–§6.2](#ref-cp-spec) 与实现代码/测试证据。

---

## 结论

**PASS WITH NOTES**

全部 7 项关键验收有具体证据（文件/测试/命令输出），无可阻断缺陷。注记仅涉及已知的设计性缺口（false-release 分子、分派改写、真实 P50），均属本 bootstrap 的诚实声明而非实现缺陷。

---

## 逐项验收

### 1. 九项 PRD §10.2 指标确定性派生

**YES**

证据文件：`internal/storage/metrics.go:86-141` `Metrics()` 方法。

| 指标 | 数据源 | 失败闭合 |
|------|--------|----------|
| 加权打扰/已合并 Change | `interrupts` + `interrupt_deliveries`/`batch_deliveries` + `config_snapshots`（冻结权重） | 缺失 reason 时回退 `defaultReasonWeight()`，不返回 0 |
| 误放行率 | `outbox_operations`（`merge_change` succeeded）+ `runs`（done） | `Numerator=0` + coverage 注记 |
| 门禁绕过率 | `runs.gate_bypassed` | 分母为 0 时 rate=0 |
| Gate 漏放率 | `calibration_entries`（predicted=allow, human=block） | 分母为 0 时 rate=0 |
| Gate 误拦率 | `calibration_entries`（predicted=block, human=allow） | 分母为 0 时 rate=0 |
| HITL 率 | `interrupts` DISTINCT run_id / `runs` COUNT | 分母为 0 时 rate=0 |
| 注意力配额消耗率 | `budget_counters` kind='attention' 最新每日桶 | 从未桶化的 severity 不出现 |
| 分派准确率 | `brain_calls` touchpoint='T2' status='valid' | 结构性 100%，coverage 显式声明非经验测量 |
| LLM 成本/已合并 Change | `brain_attempts` outcome='valid' 的 token 总计 | 分母为 0 时 per-change=0 |

空库查询不报错、不发明数字：`TestMetricsEmptyIsHonest` 验证全零分母 + coverage 非空。coverage 注记在所有 `RatioMetric` 和 `WeightedAttentionMetric`/`LLMCostMetric` 上非空：`TestOpsMetricsCoversNineSeries` 断言。

---

### 2. 北极星权重取冻结配置快照；响应间隔不作人类分钟数

**YES**

证据文件：`internal/storage/metrics.go:292-357` `weightedAttention()` + `reasonWeight()` + `snapshotReasonWeight()`。

- `reasonWeight()`（L338）查 `runs.config_snapshot_id` → 读 `config_snapshots.canonical_json`（L348），不从当前配置推算。
- `snapshotReasonWeight()`（L360）解析 canonical JSON 的 `{"metrics":{...}}` 扁平对象。缺失 reason 返回 -1 → 调用方 `defaultReasonWeight()` 按 `config.DefaultConfig().Metrics` 回退，不用 0 静默清空北极星。
- `weightedAttention()` 仅 join `interrupts` + `interrupt_deliveries`（或 `attention_batch_members` + `batch_deliveries`）/ `runs`，无任何响应间隔字段引用。
- Coverage 文本（L303）显式声明："response interval never used as human-minutes"。
- `TestMetricsWeightedAttentionUsesFrozenWeights` 验证：非默认权重 20+4=24，同 interrupt 重复 delivery 不重复计数。

---

### 3. gate_bypassed 排除误放行率分母、进入门禁绕过率

**YES**

证据文件：`internal/storage/metrics.go:411-437` `falseReleaseRate()` + `internal/storage/metrics_test.go:99-143` `TestV11GateBypassExcludedFromFalseRelease`。

- `falseReleaseRate()` 分母 = `SELECT COUNT(DISTINCT o.run_id) FROM outbox_operations o JOIN runs r ON r.id=o.run_id WHERE o.kind='merge_change' AND o.state='succeeded' AND r.status='done'`。手工合并（`gate_bypassed=1`）无 `merge_change` outbox 操作，自然排除。
- `gateBypassRate()`（L440-454）分子 = `SUM(CASE WHEN gate_bypassed=1 THEN 1 ELSE 0 END)`，分母 = 所有 done Runs。
- V11 测试：`runManual`（gate_bypassed=1，无 merge_change outbox）+ `runSift`（gate_bypassed=0，有 merge_change outbox）→ false-release denominator=1（仅 runSift），gate-bypass rate=1/2=0.5。`TestV11GateBypassExcludedFromFalseRelease` PASS。

---

### 4. sift ps / logs / timeline CLI 面完整

**YES**

- **`sift ps`**：`internal/storage/ops_read.go:63-147` `RunPS()` 返回 Run/attempt（`PSAttempt` 含 `IsolationState`/`HeartbeatAtMS`）、`open_interrupt_count`、`pending_outbox_count`、`gate_bypassed`、今日注意力余量（`attentionRemaining`）。`internal/controlplane/ops_read.go:20-51` `handleOpsPs()` 附加 `channel_deliveries`（来自 `db.ChannelDiagnostics()`）。测试：`TestRunPSProjectsRunAttempt`、`TestRunPSExactRunFilter`、`TestOpsPSReturnsRealRuns`、`TestRunPSOnline`。
- **`sift logs`**：`internal/controlplane/ops_read.go:57-118` `handleOpsLogs()` 按 `run_id`/`attempt_no`/`offset`/`limit` 读取 `agent.log`，base64 编码。offset 不可达时返回 `not_found`（L84-86：`offset > info.Size()`；L96-99：`CopyN` 遇 EOF/UnexpectedEOF），不从当前文件偷偷回零。测试：`TestOpsLogsReadsAgentLog`、`TestOpsLogsNotFound`。
- **`sift timeline`**：`internal/storage/ops_read.go:210-268` `RunTimeline()` keyset 分页（`seq > afterSeq`） + type 过滤。`internal/controlplane/ops_read.go:149-168` `handleOpsTimeline()` 接线。测试：`TestRunTimelineIsBoundedAndKeyset`、`TestOpsTimelineReturnsPersistedEvents`、`TestRunTimelineOnline`。

---

### 5. 触发→启动延迟分布可查

**YES**

证据文件：`internal/storage/metrics.go:147-236` `TriggerStartedLatency()`。

- 从 `events` 表读取：第一个 `intake.trigger_observed`（P50 起始锚点）→ 第一个 `run.transitioned` payload `to='running'`（P50 终点锚点）。
- 缺任一锚点的 Run 被排除（L225：`a.observed <= 0 || a.started <= 0 || a.started <= a.observed` continue）。
- 输出 `count`、`min_ms`、`p50_ms`、`p90_ms`、`max_ms` + `samples` 列表。百分位用 nearest-rank（`percentile()` L239-261）。
- Coverage 显式："real P50<60s is the M7 acceptance, not this slice"。
- `TestTriggerStartedLatencyDistribution` 验证：两条 Run（10s、20s 延迟）→ min=10000, p50=10000, p90=20000, max=20000，非 running 的 transition 不被误认为 start 锚点。

---

### 6. 诚实缺口：false-release 分子=0，分派准确率结构性

**YES**

- `falseReleaseRate()`（L417-437）：`Numerator: 0`，coverage 文本："numerator = post-merge revert/fix follow-ups, not yet written — fails closed at 0"。与 issue 要求 "false-release numerator remains 0 until post-merge revert/fix events are written" 一致。
- `dispatchAccuracy()`（L546-560）：分子=分母= T2 valid assignments 数，rate 在全零时也为 0。Coverage 文本："V0 has no human agent-rewrite command/event; rate is structural (100% when a T2 assignment exists), not an empirical measurement"。与 issue 要求 "dispatch accuracy may be structural 100% until rewrite events exist" 一致。
- 两项 coverage 注记均非空、完整描述缺口原因。`TestMetricsEmptyIsHonest` 断言空库场景 coverage 非空。

---

### 7. 相关包全绿

**YES**

```
$ go test ./internal/storage/ ./internal/controlplane/ ./cmd/sift/ -count=1
ok  	github.com/miaoxiaoyong/sift/internal/storage	22.440s
ok  	github.com/miaoxiaoyong/sift/internal/controlplane	5.598s
ok  	github.com/miaoxiaoyong/sift/cmd/sift	2.341s
```

定向测试全部 PASS：
- `TestV11GateBypassExcludedFromFalseRelease` PASS
- `TestMetricsWeightedAttentionUsesFrozenWeights` PASS
- `TestMetricsGateConfusionAndHITL` PASS
- `TestMetricsAttentionQuota` PASS
- `TestMetricsLLMCostAndDispatch` PASS
- `TestTriggerStartedLatencyDistribution` PASS
- `TestMetricsEmptyIsHonest` PASS
- `TestRunPSProjectsRunAttempt` PASS
- `TestRunTimelineIsBoundedAndKeyset` PASS
- `TestOpsPSReturnsRealRuns` PASS
- `TestOpsMetricsCoversNineSeries` PASS（含 closed-param 拒绝：`TestOpsMetricsRejectsExtraParams` PASS）
- `TestOpsTimelineReturnsPersistedEvents` PASS
- `TestOpsLogsReadsAgentLog` / `TestOpsLogsNotFound` PASS
- `TestRunMetricsOnline` / `TestRunTimelineOnline` / `TestRunPSOnline` PASS

未观察到 doctor flake 或与其他包的不稳定交互。

---

## 注记（非阻断）

1. **false-release 分子 = 0**：如 coverage 诚实声明，直到未来代码写入 post-merge revert/fix 事件前分子恒为 0。这是设计性缺口，非本片缺陷。

2. **分派准确率为结构性**：V0 无人类 Agent 改写命令/事件，比率恒为 `assigned/assigned`。不读作经验测量。

3. **真实 P50<60s 留 M7**：`TriggerStartedLatency` 的 coverage 显式标注此约束，bootstrap 只闭合查询/fixture 路径。

4. **Channel `ops.ps`/`ops.doctor` 端点级验收**：`handleOpsPs()` 与 `doctor()` 均调用 `db.ChannelDiagnostics()` 投影，但跨重启的端点级验收（wave-1 Channel scope）如 #715 注记所述仍属未完项。本片不声称已闭合。

5. **WBS §5.7 勾选框**：issue 明确声明 WBS checkboxes 是"separate honest sync issue after this review"，本复审不修改 WBS.md。

---

## 文件变更清单（review-only，无生产改动）

本次复审只产出本文档，修改 0 个生产文件。

被复审的 commit `01fd130` 变更：
- `cmd/sift/main.go` — CLI `metrics`/`timeline` 子命令 + `nullableStringCLI`
- `cmd/sift/main_test.go` — 在线 metrics/timeline/ps 测试
- `cmd/siftd/main.go` — `SetAttentionQuota` 生产接线
- `docs/specs/metrics.md` — 新规格（77 行）
- `docs/specs/control-plane.md` — §6.1/§6.2 增补 ops.metrics/ops.timeline
- `internal/controlplane/ops_read.go` — handleOpsPs/Logs/Metrics/Timeline（208 行）
- `internal/controlplane/ops_read_test.go` — 控制面读方法测试（148 行）
- `internal/controlplane/server.go` — ops.metrics/ops.timeline 路由
- `internal/storage/metrics.go` — 九项指标 + 延迟分布 + 辅助函数（574 行）
- `internal/storage/metrics_test.go` — V11、权重、混淆、配额、token、延迟、空库测试（400 行）
- `internal/storage/ops_read.go` — RunPS/RunTimeline/MaxAttemptNo（301 行）
- `internal/storage/ops_read_test.go` — ps/时间线/序列化测试（160 行）

---

## 不闭合项（依 issue 要求）

- 未声称 M5 完成。
- critical 熔断、完整 Command、Channel `ops.ps`/`ops.doctor` 端点级验收仍开。
- 未启动 code-opt（#748+/T006–T011）。
- 未勾选 WBS §5.7 checkboxes。

---

## 参考

- <a id="ref-wbs"></a>`docs/WBS.md` §5.7
- <a id="ref-metrics-spec"></a>`docs/specs/metrics.md`
- <a id="ref-cp-spec"></a>`docs/specs/control-plane.md` §6.1–§6.2
- PR #769 / commit `01fd130`
