PASS

# command.md 字段级第九次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`7b6db76`（#388 / PR #389 合入；实现提交 `20ae00d`）
> 评审对象：[`docs/specs/command.md`](../specs/command.md) draft 与 [`docs/specs/storage.md` §8.1](../specs/storage.md#81-outbox_operations)
> 前轮结论：[`2026-07-29-m5-command-field-rereview-8-pi-gpt-5.6-sol.md`](2026-07-29-m5-command-field-rereview-8-pi-gpt-5.6-sol.md) 的 1×P1（另有 1×P2）

## 1. 结论

**PASS。** #388 已关闭 rereview-8 的剩余 P1：replacement snapshot 的 fallback risk 与 [`brain.md` §9.3](../specs/brain.md#93-高风险兜底与-gate-来源) 固定 tuple、terminal T3 fallback call/source 和 §10.2 link 一致；initial operation、conflict result 与 replacement successor 均使用现有 Gate 可识别的 `gate/v1`；两次合法 Complete 明确形成 Run `7→8→9`。前轮 P2 的 `merge_conflict` 表格行也已修复。

[`command.md`](../specs/command.md) 继续保持 `status: draft`。本结论只核销本轮字段级 P1/P2，不表示 M5 已实现，也不扩大 M4 门禁结论。

## 2. 前轮问题对账

| 前轮关闭条件 | 本轮判断 | 证据 |
|---|---|---|
| replacement risk 与 T3 fallback 一致 | **关闭** | replacement input 现为 `risk_score=100`、`risk_points=["T3 unavailable; deterministic high-risk fallback"]`、`rationale="fallback"`，source 为 `fallback/T3/fallback/v1/provider_disabled`；vector 同时冻结 terminal fallback call `t3-call-01` 及匹配的 §10.2 link，符合 brain §9.3。 |
| Gate version identity 合法且三处一致 | **关闭** | initial `gate_version`、result `replacement_gate_version`、successor `gate_version` 均为字符串 `gate/v1`；与 `internal/gate.Version` 及 storage TEXT identity 一致。 |
| 连续 exact vector 证明 Run `7→8→9` | **关闭** | 第一次 conflict Complete 从 version 7 CAS 到 8，并创建 `source_run_version=8` 的不同-head successor；第二次 Complete claim 该 successor 后从 8 CAS 到 9，使用合法 closed failed result。 |
| §8.1 `merge_conflict` 行结尾 `|` | **关闭** | 表格行现有完整结尾分隔符。 |

## 3. 独立复算与一致性检查

- replacement `GateInputV1` 为 canonical JSON；SHA-256 独立复算为 `ac43ab23e60345f43df0305a58e37d58ae6644cbe2cb92619580be353057f104`，与 input、result 和 successor 的全部引用一致。
- nested effective policy 的 canonical SHA-256 独立复算为 `70cc93e283eaef9d52958230d0f5f785494c38cd245d9897d6ac51d8f586bb4f`，与 `effective_policy_hash` 一致。
- conflict result 为 canonical JSON；SHA-256 独立复算为 `86ee95a7748f9a38cbae9851f5092f5a70a2254469bfbb7ef2793fe2b8c76b08`。其 `replacement_input_json` 与列出的 input bytes 完全相同。
- 第二次 failed result 为 canonical JSON；SHA-256 独立复算仍为 `d5a8c1706563ff4ce16fa5419591fdbc56b7d1fd2a942ac644ea78a1f0fac978`。
- replacement head 在 input、result、successor 中均为 `bbbb…bbbb`，与 initial `aaaa…aaaa` 不同；snapshot insert-or-return、immutable source binding digest 复制和 post-CAS version 语义未被改坏。

## 4. 验收判断

- 获取并核对 Issue #390 全文、Agent 建议、范围、comment 与约束：**YES**
- 获取并核对 #388、PR #389、合入提交与完整相关 diff：**YES**
- 核销 rereview-8 剩余 1×P1：**YES**
- replacement risk 与 §9.3 fallback tuple / terminal T3 call 一致，且摘要已重算：**YES**
- initial / result / successor 使用可识别的 `gate/v1`：**YES**
- 两次 legal Complete 连续证明 Run `7→8→9`：**YES**
- §8.1 `merge_conflict` 行表格语法已修：**YES**
- 只产出评审报告、不修改规格或自修：**YES**
- `command.md` 保持 draft：**YES**
