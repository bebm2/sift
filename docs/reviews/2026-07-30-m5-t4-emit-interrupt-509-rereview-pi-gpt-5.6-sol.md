# M5 T4→EmitInterrupt #509 定向复审

## 结论

**FAIL。** #509 已修复 migration 冲突、failed attempt/generation 的生产写口校验和真实 32 位 hex security event 身份，且定向测试恢复全绿；这些增量有效，P1-1 的 attempt/security-event 子项可以核销。但 Report quota 新入口没有接入 `RecordReport`，也不拥有 rate-token CAS，仍只能由测试直接调用；0017 的 triggers 也没有落实 closed union 的 reason/arm、字段全集、digest 和 gate-recheck 组合约束。P1-4 新增的 Gate 用例及既有 T4 用例仍未逐字节覆盖 §3.6 要求的 canonical input/output、完整 persisted fields 与 normal/invalid 身份不变矩阵。因此 #504 的 P1-1/P1-4 尚未关闭。

## 评审基线

- Issue：#516（含 comments，无评论）；被评审 Issue：#509（含 comments）
- 前次结论：[`2026-07-30-m5-t4-emit-interrupt-497-rereview-pi-gpt-5.6-sol.md`](2026-07-30-m5-t4-emit-interrupt-497-rereview-pi-gpt-5.6-sol.md) **FAIL**
- PR：#513，commit `c82c2a5a6183a0b030b7a47cd85397a8afb7bdc8`，merge commit `11bc9c5`
- 当前基线：`a0fa644`（`origin/main`）
- 判定基准：`docs/specs/interrupt.md` §3.6、§5.1；`docs/specs/storage.md` §6.4、§7.5、§12.2.1；`docs/specs/report.md` §6.2、§7
- #509 范围：7 个文件，`+270/-21`

## 已关闭项

### P1-0：migration 冲突已关闭

migration 现为连续且唯一的 `0001`–`0017`：EmitInterrupt bindings 已重编号为 `0015`，#509 只新增 `0017_emit_interrupt_binding_invariants.sql`，没有重复版本。`SchemaVersion`/migration count 测试同步为 17，数据库与定向测试均可启动。

### P1-1a：attempt failed-generation 写口校验已关闭

`EmitInterrupt` 现在按 `(run_id,attempt_no,generation)` 查询 attempt；默认 `new_attempt` 只接受 `phase=finished` 且非零 exit code 或 signal 的失败结果，并在进入 generation/admission/Interrupt/binding/event/operation 写入前拒绝 pending attempt。0017 另为直接插入的 `failure_review_attempt/new_attempt` binding 加同类 guard。正例 fixture 已改为 finished failure，负例断言 pending attempt 拒绝且不写 Interrupt/binding。

### P1-1b：security event 身份兼容已关闭

`SecurityEventID` 已从 `int64` 改为 string，并要求 32 位 lowercase hex；exhaustion、event 查询和 `sift://event/<id>` renderer 均使用同一真实 `newID()` 身份。新增用例验证了 system event、exhaustion、Interrupt brief 的同一 event ID。

## 未关闭的阻断项

### P1-1：Report 生产 owner 与 closed binding 仍不成立

1. **新入口未接入生产 Report 流。** 全仓 `RecordReportQuotaExhaustion` 的唯一调用在 `internal/storage/interrupt_test.go`；仓库仍没有 `RecordReport` 生产写口。该函数的注释明确要求 caller 已消费 rate token，但没有 caller，函数自身也不执行 report rate CAS、request/report 去重或 exhaustion 并发线性化。因此 storage §12.2.1 要求的“第一事务 token + exhaustion/security event，第二事务专用发射”并未接通，不能以一个测试可直调 helper 认定 Report quota owner 存在。
2. **0017 不是 closed union。** `interrupt_command_effect_bindings_closed_insert` 只检查 reason 枚举、schema version、JSON 合法及存在任意 `arm`；它不校验 reason/arm 的合法映射，不限制 arm 枚举/额外字段，不重算 `binding_digest`。`failed_attempt` trigger 只处理 `new_attempt`，对 `gate_recheck` 的必填 change/head、terminal NULL 和 exact checked head 没有约束；quota trigger 也未拒绝混入 attempt/retry/terminal/change/head 字段。其余 arms 的 §6.4 required/null shape 和组合 FK 仍未补。
3. **负向/回滚矩阵不足。** 新测试只覆盖 pending attempt 拒绝和一个真实 event 正例；仍没有错 run/generation、finished-success、terminal pair 错配、gate-recheck change/head、exhaustion end/event/digest/key、non-system event、extra/cross-arm fields、digest 不一致和 binding INSERT 失败的全事务回滚矩阵。

所以 P1-1 为 **PARTIAL / NO**。

### P1-4：§3.6 exact golden 与 Gate matrix 仍为 PARTIAL

- attempt 的 special-fragment golden 仍只直接调用 `acceptInterruptT4`；经 `EmitInterrupt` 的 attempt 用例实际提供普通 `CI` facts，并只验证 option 重排 fallback、brief/headline/options。它没有逐字节冻结 §3.6 canonical input/output、normal persisted headline/brief/options/links，也没有 unknown fragment、安全事件 link、`recommended_action=hold` 和三类失败唯一 fallback persisted bytes 的完整矩阵。
- quota 正例检查 brief 与 generation key，invalid 三例检查 fallback brief/severity/key/options ID；未逐字节断言完整 fallback renderer JSON、canonical T4 input/output、persisted headline/options_json/links_json。
- cross-arm 仍主要直接调用 `validateFailureReviewVariant`，没有证明错误 variant/FK 在全部领域写入前整体回滚。
- 新 Gate 测试覆盖 code-review 的 normal/invalid 两路，但只比较 brief、severity，并要求 generation key/calibration ID 非空；没有逐字节断言 options/links、两路 generation key 相等、calibration/evaluation/Interrupt/operation 的事务身份相等，也未覆盖 Gate `failure_review` attempt 的 §3.6 vector。

所以 P1-4 为 **PARTIAL / NO**。

## 未回退项

- P1-2 统一 DB 级 T4 接缝：静态未回退。
- P1-3 单次 sink escaping：定向测试及代码静态未回退。
- replay 缺 frozen contract fail closed：定向测试通过。

## 验证

- `go test ./internal/brain ./internal/storage ./internal/gate ./internal/replay`：**PASS**。
- `go test ./internal/storage -run 'TestEmitInterrupt|TestRecordReportQuota|Test.*T4|Test.*Report.*Quota|Test.*Gate.*Interrupt' -count=10`：**PASS**。
- `go test ./...`：首次 **FAIL** 于 `TestDoctorBaselineChecksConfiguredDependencies` 的 fixture command 被 killed，以及 `TestLaunchWorkerWrapperCrashSuite` 的时序断言；两个失败用例分别单独重跑均 **PASS**，未见 #509 相关失败。

## 关闭清单

| #516 / #509 条件 | 结果 |
|---|---|
| 对照 #504 FAIL / #509 | **YES** |
| migration 0017 无重复版本 | **YES** |
| P1-1 attempt failed-generation 校验 | **YES** |
| P1-1 security event 真实身份兼容 | **YES** |
| P1-1 Report quota 生产 owner | **NO**：仅孤立 helper + 测试调用，无 `RecordReport`/rate-token 接线 |
| P1-1 closed binding union/组合约束 | **NO** |
| P1-1 完整拒绝与回滚矩阵 | **NO** |
| P1-4 attempt §3.6 exact golden | **NO** |
| P1-4 Report quota §3.6 exact golden | **NO** |
| P1-4 Gate normal/invalid 完整身份矩阵 | **NO** |
| 已关闭 P1-2/P1-3/replay 未回退 | **YES** |
| 定向测试全绿 | **YES** |
| 全量测试稳定全绿 | **NO**：两项单跑即过的非定向时序失败 |
| #509 可核销 | **NO** |

**最终：FAIL。**
