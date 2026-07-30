# M5 #798 LLM severity-only downgrade + visual rejects voice · Sol 复审

> 日期：2026-07-30
> 评审人：pi × DeepSeek V4 Pro（Sol role）
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#798 / PR #799，实现提交 `22d94a9`，合入提交 `926e5b5`
> 评审基线：`chore/issue-798-review` @ `926e5b5`
> 判定基准：[`interrupt.md` §1.2 / §4.2](../specs/interrupt.md)、[`brain.md` T4/T6](../specs/brain.md)、WBS §5.2 行「LLM 只能建议 severity 降级；`min_modality: visual` renderer 拒绝语音路径」

## 1. 结论

**PASS。** P0/P1 全部关闭。WBS §5.2 该行实现正确，可勾选。

- **唯一 severity 写口**：导出 `Severity(base, suggestedDowngrade)`；`EmitInterruptCmd` / `InterruptT4Output` 无 severity 字段；T6 仅 `SuggestedDowngrade: bool`。至多降一级、永不开级；`low` 钳位。
- **visual 拒语音**：`admitInterruptT6` 在调用 T6 **前**按 channel capabilities 过滤；零兼容通道 → held / `no_compatible_channel`，不调 T6；降级不改 modality。
- **并发/重放**：同 key 并发仅一条 Interrupt、一笔 charge、一级降级；重放不调 T6、不复降级、不重复收费。
- **升级复用冻结降级**：`AdvanceInterrupt` 对 escalate 后 severity 应用一次 `Severity(escalated, persisted_downgrade)`，不再调 T6。

**不读作** M5 完成；不触及 once-charge 全生命周期、Command workers、#748+。

## 2. Findings（Scope gate：仅记 P2）

| 级别 | 数量 | 本轮是否实施 |
|---|---|---|
| P0 | 0 | — |
| P1 | 0 | — |
| P2 | 3 | 否（记录） |
| DEFER | 0 | 否 |

### [P2] `Severity()` 非累加由调用方纪律保证

纯函数连调可两级降；生产路径对冻结 base / 持久化标志各应用一次。注释与测试已覆盖三条调用路径。`fixer=same`

### [P2] escalate→`Severity` 无新增独立集成测试

同义重构；#711 `TestAdvanceInterruptEscalationCountsReuseDowngrade` 已覆盖。`fixer=same`

### [P2] 跨切面注记（ledger certification optional）

复审过程对 tip 树旁路注记；与 #798 语义无直接关联，不阻断勾选。`fixer=switch:backlog`

## 3. 通过依据（简注）

1. `TestSeverityIsAtMostOneDowngradeAndNeverUpgrades` — 四级 base 纯函数覆盖。
2. `TestEmitInterruptVisualModalityHoldsRatherThanRoutingToVoice` — code_review（visual）不在语音通道投递。
3. `TestEmitInterruptT6SuggestedDowngradeNeverChangesModalityToVoice` — 降级不拓宽 modality。
4. `TestConcurrentEmitInterruptWithT6SuggestedDowngradeConvergesOneCharge` / `TestEmitInterruptReplayKeepsSingleDowngradeAndSingleCharge` — race/replay 收敛。
