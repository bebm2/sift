# M5 #721 T6→EmitInterrupt after #719 定向复审

> 日期：2026-07-30
> 评审人：pi × DeepSeek V4 Pro（Sol role）
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#719 / PR #720，实现提交 `d3520eb`，合入提交 `5fee632`
> 评审基线：`feat/issue-721-rereview-t6-emitinterrupt-after-719` @ `5fee632`
> 判定基准：[`interrupt.md` §7.2](../specs/interrupt.md)、[`brain.md` §12](../specs/brain.md)、[`interrupt.md` §4.2](../specs/interrupt.md)、[wave1 plan](../plans/2026-07-29-m5-attention-impl-wave1.md)

## 1. 结论

**PASS。** #719 完整实现了 M5 波次一 I2：T6 severity/delivery 通过统一 Brain 壳接入 `EmitInterrupt`，无第二发射入口、无第二收费路径、无 severity 或 option 效果泄漏。

- **生产接缝**：`cmd/siftd/main.go` 创建单一 `shell`，通过 `db.SetInterruptT6(shell.CallT6)` 安装，镜像 T4 模式。`emitInterruptHooks` 在事务外解析 `d.interruptT6Caller()` 并注入 `cmd.T6`，随后 `admitInterruptT6` 调用，全程在五件事事务之前。
- **唯一降级**：`downgradeInterruptSeverity` 严格一档递减（critical→high→normal→low），且降级后若 severity 仍为 high/critical 则强制 immediate。`validT6Advice` 独立二次校验 high/critical→immediate 约束，T6 无法绕过。
- **确定性兜底**：无效建议、provider 错误、schema 校验失败均回退到 `{high|critical→immediate; low|normal→batch}`，使用 `defaultChannelID`；`T6FallbackOutput` 产出与接纳器默认值一致的 deterministic canonical bytes。
- **Brain trace**：`Shell.Call` 写入完整 logical call 记录；`T6ResultFromCall` 区分 `valid`/`fallback` 状态并产出对应的 `BrainSource`。两款 acceptance test 逐字段验证 trace envelope（`RecordID`、`Touchpoint="T6"`、`PromptVersion` 前缀、`OutputSchemaVersion=1`、`Status`、`FallbackReason`）与 input/output exact JSON bytes。
- **Per-call override**：`cmd.T6 != nil` 时跳过生产 seam 解析，测试可独立注入；`TestEmitInterruptPerCallT6OverridesProductionSeam` 验证生产 seam 不被调用。
- **零兼容 Channel**：`admitInterruptT6` 在构造 input 之前检查兼容 Channel 集合，为空时直接返回 `held`，不 reserve T6 call。既有 `TestEmitInterruptHoldsWithoutCompatibleChannelWithoutCallingT6` 不受影响。

无 migration 变更（T6 纯运行时接缝）；全仓 25 包测试全绿；`git diff --check` 无空白错误；定向 `-count=2 -race` 全绿。

## 2. 四项必验逐条分析

### 2.1 Production out-of-tx Shell.Call(T6) seam into existing EmitInterrupt

**YES。**

`cmd/siftd/main.go` 复用同一个 `shell` 实例（已为 T4 构造），追加一行 `db.SetInterruptT6(shell.CallT6)`。`internal/storage/storage.go` 的 `SetInterruptT6`/`interruptT6Caller` 完全镜像 T4 的 `SetInterruptT4`/`interruptT4Caller`，共用 `wakeupMu` 读写锁。

`emitInterruptHooks` 中的接线顺序（第 518–530 行）：

```go
// T4: d.interruptT4Caller() → acceptInterruptT4 → headline/brief
// T6: if cmd.T6 == nil { cmd.T6 = d.interruptT6Caller() }
//      admitInterruptT6(ctx, cmd, t.modality, severity, ...)
// tx := d.db.BeginTx(ctx, nil)  ← 事务在此之后
```

T6 call 严格执行于事务外。`EmitInterrupt` 仍是唯一创建端口；注意力记账、budget charge、forge comment operation 均在统一五件事事务内，未新增第二条 charge 路径。`TestEmitInterruptUsesProductionT6SeamWhenCmdT6Nil` 断言 `assertCount(t, db, "interrupts", 1)` 与 `assertCount(t, db, "outbox_operations", 1)`——无第二发射。

### 2.2 T6 may only suggest one-level severity downgrade + delivery timing

**YES。**

`downgradeInterruptSeverity` 仅实现四级→三级的单步映射：critical→high→normal→low，`low` 保持 `low`。不存在连降两级或升级路径。

`admitInterruptT6` 的裁决顺序严格遵循 interrupt.md §7.2：

1. schema/Channel 兼容性校验（`validT6Advice`：channel 必在 `ChannelCandidates` 中，delivery 必为三 enum 之一）
2. `suggested_downgrade` 一档降级（`downgradeInterruptSeverity`）
3. 降级后若 high/critical → 强制 immediate（第二层安全，覆盖 T6 输出与降级后结果）
4. delivery 分发：immediate → `nextDispatchAtMS=NowMS`；batch → `BatchAtMS`（若早于 expires）或 held；next_window → `NextWindowAtMS`（需 `< expiresAtMS` 且 `> NowMS`）

`validT6Advice` 额外执行：降级后 severity 为 high/critical 时 delivery 必须为 `immediate`（第一层安全）。T6 不能输出 severity、quota、reason、options、expires、on-expire、绝对调度时间或"不发出"指令。`T6FallbackOutput` 产出与接纳器默认值严格一致：`high|critical → immediate + defaultChannelID + downgrade=false`，`low|normal → batch + defaultChannelID + downgrade=false`。

### 2.3 Invalid/error fallback deterministic; Brain trace written

**YES。**

三层兜底路径均确定：

| 路径 | 触发 | 行为 |
|------|------|------|
| `validT6Advice` 拒绝 | 输出 channel 不在 candidates、delivery 非三 enum、high/critical 非 immediate、next_window 缺乏有效窗口 | 使用默认 `{batch\|immediate}+defaultChannel` |
| Provider error | `cmd.T6(ctx, input)` 返回 error | 同默认 |
| Brain shell fallback | Provider disabled、token 阈值、input 超限、schema failure、invalid output | `T6ResultFromCall` 调用 `T6FallbackOutput(in)` 产出确定性 fallback，`CallT6` 正常返回 |

Brain trace 写入路径：
- `Shell.Call` → `recordBrainCall` 写入 `brain_calls` 表（含 `CallID`、`Touchpoint`、`Status`、`FallbackReason`、`Input`、`Output` 等全字段）
- `TestEmitInterruptT6ProductionSeamPersistsCanonicalTrace`：valid trace，断言 `Status="valid"`、`FallbackReason=nil`、`PromptVersion` 前缀 `"T6/v1/"`、`OutputSchemaVersion=1`，decoded input candidate `Severity="high"`、`MinModality="voice"`、`DefaultChannelID="voice"`，decoded output `Delivery="batch"`、`ChannelID="voice"`、`SuggestedDowngrade=true`
- `TestEmitInterruptT6ProductionSeamInvalidFallsBack`：fallback trace，断言 `Status="fallback"`、`FallbackReason` 非 nil、`Touchpoint="T6"`，dispatch 为 `high→immediate`

Storage 侧 `TestEmitInterruptProductionT6InvalidFallsBackDeterministically` 与 `TestEmitInterruptProductionT6ErrorFallsBackDeterministically` 验证 invalid/error fallback 的 dispatch 结果（不重复 brain trace 封套校验，后者已在 brain 包中覆盖）。

### 2.4 Checklist YES/NO for I2

| I2 验收项 | YES/NO | 说明 |
|-----------|--------|------|
| Production Shell.Call(T6) seam | **YES** | `db.SetInterruptT6(shell.CallT6)`，镜像 T4；`emitInterruptHooks` 在事务外注入 |
| 无第二发射入口 | **YES** | `EmitInterrupt` 仍为唯一 creation port；count 断言 1 interrupt + 1 operation |
| 无第二收费路径 | **YES** | 注意力记账仍在统一五件事事务内，T6 不触发独立 charge |
| `suggested_downgrade` 最多降一级 | **YES** | `downgradeInterruptSeverity` 四→三→二→一单步递减 |
| high/critical 强制 immediate | **YES** | 两层校验：`validT6Advice` + 降级后 `if severity == high/critical → immediate` |
| delivery enum 限定 `immediate\|batch\|next_window` | **YES** | `validT6Advice` 拒绝所有其他值；`admitInterruptT6` default 分支拒发 |
| channel_id 只能选 frozen ChannelCandidates | **YES** | `validT6Advice` 要求 `containsString(in.ChannelCandidates, out.ChannelID)` |
| 无效/错误/不可用 fallback 确定 | **YES** | 三条 fallback 路径均 deterministic（见 §2.3 表） |
| Brain trace 写入（valid + fallback） | **YES** | acceptance test 逐字段验证 trace envelope + decoded I/O |
| Per-call cmd.T6 override 优先于生产 seam | **YES** | `TestEmitInterruptPerCallT6OverridesProductionSeam` 断言生产 seam 不被调用 |
| 零兼容 Channel 不调用 T6 | **YES** | `admitInterruptT6` 在 `len(compatible)==0` 时提前返回 held |
| 持久化 dispatch snapshot | **YES** | `interruptDispatch` 写入 severity/channel_id/delivery/next_dispatch_at_ms/held_reason |
| T6 不能改变 reason/options/expires/forge comment | **YES** | T6 output 仅含 `{delivery, channel_id, suggested_downgrade}`；admitInterruptT6 不接触 reason/options/expires |

## 3. 测试覆盖矩阵

| 测试 | 包 | 覆盖场景 |
|------|-----|---------|
| `TestEmitInterruptAdmitsT6AndPersistsDispatch` | storage | T6 正常建议（downgrade + batch）→ dispatch 持久化 |
| `TestEmitInterruptT6InvalidFallsBackAndHighIsImmediate` | storage | 无效 channel（不在 candidates）→ high 回退 immediate |
| `TestEmitInterruptHoldsWithoutCompatibleChannelWithoutCallingT6` | storage | 零兼容 Channel → held，T6 不调用 |
| `TestEmitInterruptUsesProductionT6SeamWhenCmdT6Nil` | storage | 生产 seam 被调用，input 验证，count 断言 |
| `TestEmitInterruptProductionT6InvalidFallsBackDeterministically` | storage | invalid（channel 不在 candidates/mid-high）→ high→immediate |
| `TestEmitInterruptProductionT6ErrorFallsBackDeterministically` | storage | provider error → normal→batch |
| `TestEmitInterruptPerCallT6OverridesProductionSeam` | storage | per-call override 优先，生产 seam 不运行 |
| `TestEmitInterruptT6ProductionSeamPersistsCanonicalTrace` | brain | valid trace envelope + decoded I/O exact |
| `TestEmitInterruptT6ProductionSeamInvalidFallsBack` | brain | fallback trace + fallback dispatch |
| `TestT4T6T7InvalidOutputFallsBack/T6` | brain | schema 校验失败 → fallback |
| `TestT4T6T7ProviderDisabledFallback/T6` | brain | provider disabled → fallback |

## 4. 执行证据

- `gh issue view 721` / `gh issue view 721 --comments`：无 comments。
- `gh issue view 719 --comments`：无 comments（已 merge via #720）。
- `git diff 42f1294..5fee632 --check`：**PASS**（无空白错误）。
- `go test ./... -count=1`：25 包全 **PASS**。
- `go vet ./cmd/siftd/ ./internal/storage/ ./internal/brain/`：**PASS**。
- `go test ./internal/storage/ -run "T6" -count=2 -race`：**PASS**（7 tests）。
- `go test ./internal/brain/ -run "T6" -count=2 -race`：**PASS**（5 tests）。
- 无 migration 变更（T6 为纯运行时接缝，无 schema 修改）。
- 仅在 `feat/issue-721-rereview-t6-emitinterrupt-after-719` worktree 工作。
- 仅新增本评审报告 `docs/reviews/`，未 push/MR/merge。

## 5. 剩余风险

| 风险 | 评估 |
|------|------|
| T6 prompt asset 未交付 | 非回归；prompt/schema 资产属波次一后续 task，不影响本接缝正确性（`FakeProvider` 已覆盖 normal/fallback 路径） |
| availability state 仅 `unknown` | 非回归；`CallT6` 硬编码 `T6Availability{State: "unknown", NextWindowAtMS: ...}`，属波次二 Supervisor tick 集成范畴；当前 `next_window` 路径的 `validT6Advice` 校验与 fallback 已覆盖 |
| `attention.remaining` 硬编码 quota snapshot | 非回归；当前 `CallT6` 构造 `[]T6Quota{{Severity: "low"}, {Severity: "normal"}, {Severity: "high"}}` 但不填 `Remaining` 字段；v1 `remaining` 仅为排序特征，不影响 fallback 确定性 |
| 全仓 long-race（`count=10`）未执行 | 已知 sqlite race-detector 资源饱和 flake，非 T6 引入；`count=2 -race` 定向全绿 |
| Channel `ops.ps`/`ops.doctor` 端点验收 | 非 I2 scope；属 Channel webhook（I3）+ AdvanceInterrupt（I4）范畴 |
| Supervisor tick 尚未接线 | 非 I2 scope；`AdvanceInterrupt` 最小扫描（I4）待独立交付 |

## 6. 与 T4 接缝的对称性核对

| 维度 | T4（#706 PASS WITH NOTES） | T6（本次） |
|------|---------------------------|-----------|
| Shell 构造 | `db.SetInterruptT4(shell.CallT4)` | `db.SetInterruptT6(shell.CallT6)` ✓ |
| 调用时机 | 事务外，在 `EmitInterrupt` 五件事事务之前 | 事务外，同 ✓ |
| 确定性接纳 | `acceptInterruptT4` | `validT6Advice` + 降级后 severity 校验 ✓ |
| 兜底 | fallback headline/brief/fragments 模板 | batch/immediate + defaultChannelID ✓ |
| Trace | `ExportBrainCallsJSONL` envelope + I/O bytes | 同 ✓ |
| Per-call override | cmd 无对应字段（T4 无 per-call 覆盖需求） | `cmd.T6` ✓ |
| 零兼容 → 不调用 | T4 不涉及（无 Channel 概念） | `len(compatible)==0` 提前返回 ✓ |

对称面完整，未引入结构性分歧。
