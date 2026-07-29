FAIL

# command.md 字段级第八次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`14c2cc8`（#384 / PR #385 合入；实现提交 `86c6d89`）
> 评审对象：[`docs/specs/command.md`](../specs/command.md) draft 与 [`docs/specs/storage.md` §6.4、§8.1、§10.2](../specs/storage.md)
> 前轮结论：[`2026-07-29-m5-command-field-rereview-7-pi-gpt-5.6-sol.md`](2026-07-29-m5-command-field-rereview-7-pi-gpt-5.6-sol.md) 的 1×P1

## 1. 结论

**FAIL（1×P1，另有 1×P2）。** #384 已冻结 snapshot insert-or-return、immutable effect binding 复制、result 给出的 replacement Gate version、post-CAS version 与第二次 failed Complete；replacement input/result/policy 三组摘要也都可独立复算。

但 exact vector 声称的完整 `GateInputV1` 仍不合法。它把 T3 source 标为 `fallback/T3/fallback/v1/provider_disabled`，却同时给出 `risk_score=0`、空 `risk_points`、`rationale="low risk"`；[`brain.md` §9.3](../specs/brain.md#93-高风险兜底与-gate-来源) 冻结的 fallback 结果只能是 `risk_score=100`、`risk_points=["T3 unavailable; deterministic high-risk fallback"]`、`rationale="fallback"`。该 snapshot 也不可能与 vector 声称的 terminal fallback T3 call/link 逐字段一致。按新增 conflict 校验规则，第一次 Complete 必须把它转入 failed arm，不能创建所列 replacement operation，因此后面的第二次 Complete 仍不是同一条可连续执行的 exact vector。

此外，vector 把 `gate_version` / `replacement_gate_version` 写为数值 `2` / `3`，而现有 M4 Gate 的实现版本身份是字符串 `gate/v1`，存储字段也是 TEXT。§8.1 只称它为“Gate implementation version”，没有冻结另一套整数版本空间或转换算法；故即使修正 risk tuple，所列 `gate_version=3` 也尚不能证明是现有 Gate 可 claim/evaluate 的合法版本。此项与非法 input 一并归入同一 continuous-Complete P1。

[`command.md`](../specs/command.md) 应继续保持 `status: draft`，不得按现稿开始完整 Command 实现。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮 P1 对账

| 前轮关闭条件 | 本轮判断 | 证据 |
|---|---|---|
| 合法、完整、canonical `GateInputV1` | **未关闭** | JSON shape、排序及多数交叉字段已补齐，但 fallback source 与冻结 T3 fallback result 矛盾；对应 T3 link 无法通过 §10.2 逐字段校验。 |
| `gate_input_snapshot_id` 事务分配/复用唯一 | **关闭** | Complete 在 lease/CAS 事务内按 hash insert-or-return；vector 预置 byte-identical snapshot `000…002`，因此唯一复用该 ID。 |
| `effect_binding_digest` 来源唯一 | **关闭** | Complete 重验 source Interrupt immutable binding 并逐字节复制原 operation digest；worker/replacement facts 不再提供新 digest。 |
| `gate_version` 来源及校验唯一 | **部分关闭** | operation 唯一复制 closed result 的 `replacement_gate_version`，但 exact vector 的整数版本与现有 `gate/v1` 身份不一致，且未定义合法转换/支持性校验。 |
| 完整 successor + 第二次 Complete / Run `8→9` | **未关闭** | successor 字段与第二个 closed failed result 已列全，failed result digest `d5a8c1…` 也沿用合法 vector；但第一次 Complete 因非法 replacement input 不能生成该 successor。 |

## 3. 剩余可执行 P1

### P1 — replacement exact vector 的首个 Complete 仍不能合法创建 successor

1. 将 replacement `risk` 改为与其 fallback source 和 terminal T3 call 严格一致的 §9.3 固定高风险 tuple，或改用一份完整合法且与 terminal valid T3 call 逐字段一致的 brain source/result；随后重算 replacement input hash、conflict result digest 及所有引用。
2. 使用现有 Gate 可识别的 version identity（当前实现为 `gate/v1`），或先明确冻结另一版本空间的字段类型、与实际 Gate implementation 的映射及 Complete 支持性校验；initial/result/successor 三处须一致。
3. 保留现有 snapshot insert-or-return、source binding digest 复制、post-CAS `source_run_version=8`、不同 replacement head、完整 successor 与第二次 legal failed Complete，并证明修订后实际发生 Run `7→8→9`。

## 4. P2 与独立复算

- §8.1 successor 表的 `merge_conflict` 行仍缺少结尾 `|`；#384 的明确修复项未落地。
- 独立复算确认：replacement input SHA-256 为 `1c1700bdbf45588268ce0ae9d8028199666f296d931371417388d71560376fb4`，nested effective-policy SHA-256 为 `70cc93e283eaef9d52958230d0f5f785494c38cd245d9897d6ac51d8f586bb4f`，conflict result SHA-256 为 `ed0f8ccc985d1d01b5b26c70f64bca347eb8d077ab5f089c3a53955dac8087ab`；算术正确，问题是被摘要内容的契约合法性。
- `go test ./...` 的功能包均通过，但全局命令仍命中已知 doctor 时序 flake：`internal/controlplane.TestDoctorBaselineChecksConfiguredDependencies` 的 fixture agent CLI 被 `signal: killed`。这不改变上述文档字段结论。

## 5. 验收判断

- 获取并核对 Issue #386 全文、Agent 建议、范围、comment 与约束：**YES**
- 获取并核对 #384、PR #385、合入提交与完整相关 diff：**YES**
- 核销 rereview-7 剩余 1×P1：**YES**
- conflict replacement 使用合法完整 canonical `GateInputV1`：**NO**
- snapshot ID / Gate version / binding digest 可由 closed result 与事务事实唯一派生：**NO**（snapshot 与 binding 为 YES；Gate version 合法身份仍为 NO）
- 完整 successor 与第二次 Complete 构成连续 exact vector：**NO**
- §8.1 `merge_conflict` 行表格语法已修：**NO**
- 只产出评审报告、不修改规格或自修：**YES**
- `command.md` 保持 draft：**YES**
- 允许开始完整 Command 实现：**NO**
