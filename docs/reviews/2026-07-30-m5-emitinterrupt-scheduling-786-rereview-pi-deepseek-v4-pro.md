# M5 #786 EmitInterrupt scheduling conjunct 定向复审 (#787)

> 日期：2026-07-30
> 评审人：pi × DeepSeek V4 Pro（Sol role）
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#786 / PR #787，合入提交 ≥ `37da6ef`
> 判定基准：[`interrupt.md` §8](../specs/interrupt.md)、[`brain.md` §12](../specs/brain.md)、Issue #786 scope §1-4

## 1. 结论

**PASS WITH NOTES。** PR #787 闭合了 Issue #786 WBS §5.2 EmitInterrupt 调度合取。无 P0/P1 finding。一条 P2：`CallT6` 硬编码 `Availability.State="unknown"`（不阻断 `next_window` 调度合取，但 Issue scope §3 显式标记了此缺口）。

## 2. 实现范围审查

### 生产路径证据

| 端口 | 唯一生产调用点 | 判定 |
|---|---|---|
| `EmitInterrupt` | `interrupt.go:437`（唯一 `INSERT INTO interrupts`） | ✅ 唯一创建口 |
| `SupervisorInterruptTick` | `cmd/siftd/main.go:182`（唯一接线） | ✅ 唯一调度驱动 |
| `AdvanceInterrupt` | `advance_interrupt.go:691`（仅被 `SupervisorInterruptTick` 调用） | ✅ 唯一推进口 |
| `PrepareDueAttentionBatches` | `advance_interrupt.go:695`（仅被 `SupervisorInterruptTick` 调用） | ✅ 唯一封口 |

`rg` 验证无第二调度写口：Command 路径（`command.go:334`、`command_probe.go:148`、`command.go:403`）的 `dispatch_state='held'` / `'probe_in_progress'` 是 HITL 转换，不在 Issue scope §非范围 内。

### 测试覆盖

`internal/storage/scheduling_conjunct_test.go`（+547 LOC / -0 生产代码）：

| 测试 | 覆盖的调度合取 | 判定 |
|---|---|---|
| `TestSchedulingConjunctFallbackBatchReadyToSealedChannelPublish` | fallback/T6 → batch ready → due tick → `batched` + member/authority → `PrepareDue` → sealed `channel_publish`（`attention_batch` arm） | ✅ |
| `TestSchedulingConjunctT6AdvisedBatchSealsViaProductionSeal` | 生产 `SetInterruptT6` seam → T6 建议 batch → 提早 tick 不封口 → due tick 封口 | ✅ |
| `TestSchedulingConjunctImmediateIsSingleDeliveryAndEscalationIsStrong` | immediate 初发 `interrupt` arm、不假借 batch；升级到 high → strong single redelivery、charge 复用 | ✅ |
| `TestSchedulingConjunctNextWindowFrozenBeforeExpiryOrHonestFallback` | 三子向量：frozen window < expiry 调度并封口、无 caller window → 诚实 fallback（拒绝模型猜时间）、window ≥ expiry 在调度前拒绝 | ✅ |
| `TestSchedulingConjunctCrossRestartStaleCASAndIdempotentSeal` | DB reopen 后 stale version/nonce CAS → `ErrRejectedStale` 不重推；done batch → re-tick 幂等（payload/digest 不变、无第二 `channel_publish`） | ✅ |

全部测试 **`-race` / `count=3` PASS**（`go test ./internal/storage/ -run TestSchedulingConjunct -count=3 -race` 三连无 flake）。

## 3. 核对矩阵

| #786 要求 | 证据 | 判定 |
|---|---|---|
| 生产路径仅 `EmitInterrupt` + `SupervisorInterruptTick`→`AdvanceInterrupt` + `PrepareDueAttentionBatches` | 上表 4 唯一端口 rg 验证 | ✅ |
| 验收测试绿（含 `-race`/`count≥3`） | 5 测试全绿，三连无 flake | ✅ |
| Availability 硬编码 `unknown` 不阻断 `next_window` 合取 | `next_window` 窗口由 caller 提供、`validT6Advice` 已有严格校验（< expiry、> frozen） | ✅ （调度合取不依赖 Brain Availability State） |
| 诚实 WBS 同步 | WBS §5.2 注记为 "待 #786 Sol 复核 PASS 后勾选"，五合取证齐备 | ✅（勾选留后续 docs sync） |

## 4. Findings

### [P2] `CallT6` 硬编码 `Availability.State="unknown"`

- **描述**：`internal/brain/t4t6t7_result.go:94` 中 `CallT6` 硬编码 `Availability: T6Availability{State: "unknown", NextWindowAtMS: in.NextWindowAtMS}`。Issue scope §3 明确要求 "若发现 CallT6 硬编码 availability=unknown 阻断 next_window 合取，可最小接线确定性 Availability 快照进 T6 输入"。当前 `next_window` 调度合取不依赖此字段（窗口由 caller 提供，T6 仅建议 delivery+channel），故不阻断。但模型无法得知真实 availability 状态，属于 Issue scope §3 显式标记的缺口。
- **关闭标尺**：在 `CallT6` 中接入确定性 Availability 快照（如基于 Run 状态推导 `available`/`unavailable`），或显式记录 ADR 说明延迟原因。`T6Availability.NextWindowAtMS` 已正确传递。
- **证据缺口**：`State` 未从任何生产数据源派生，始终为 `"unknown"`。
- **fixer=same**

## 5. Scope summary

| 级别 | 数量 | 本轮是否实施 |
|---|---|---|
| P0 | 0 | N/A |
| P1 | 0 | N/A |
| P2 | 1 | 否（记录） |
| DEFER | 0 | 否（backlog） |

## 6. Verdict

**PASS WITH NOTES。** P0/P1 全关。五合取调度证据齐备：#787 可核销 WBS §5.2 EmitInterrupt 调度合取；WBS checkbox 同步由后续 docs sync Issue 诚实执行。一条 P2（CallT6 availability）留后续波次。
