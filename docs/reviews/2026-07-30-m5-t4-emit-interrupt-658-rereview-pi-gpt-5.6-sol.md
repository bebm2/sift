FAIL

# M5 #658 T4→EmitInterrupt after #652 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#652 / PR #655，实现提交 `6ba4f95`，合入提交 `ff3be3f`
> 评审基线：`main` / `origin/main` `59dd9db`（另含后续 #653）
> 前次结论：[#646 FAIL](2026-07-30-m5-t4-emit-interrupt-646-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §3.6/§9.2](../specs/interrupt.md)、[`brain.md` §11](../specs/brain.md)、[`gate.md` §5/§7](../specs/gate.md)、[`report.md` §7](../specs/report.md)、[`storage.md` §6.4/§12.2.1](../specs/storage.md)

## 1. 结论

**FAIL（1×P1）。** #652 是有效增量：新增测试首次把一条 `startup_stall` 经 production `Shell.CallT4`、`BuildT4Input`/`T4Contract`、Brain trace 与 `EmitInterrupt` persisted brief 串在一起；Gate `mergeability_unknown` 也首次经过 `EvaluateRecordAndEmitInterrupt`；Report 发射事务补了 `budget_entries` cut，并让七个发射 cut 都在解除注入后验证恢复；migration `0048` 修正 `0047` 将 Gate snapshot Change 身份误读为 `$.change.id` 的问题。PR #655 的 CI 全绿，本轮要求的四个包、定向重复与定向 race 也通过。

但 #646 的关闭矩阵仍未闭合。新增 production canonical 用例测试的是 `startup_stall`，不是规格和 #646 指定的 attempt / Report-quota 两套 golden；Gate 只断言若干非空 ID，没有对账 binding、snapshot/evaluation/calibration、T4/fallback bytes 或原子回滚；Report 仍缺 exhaustion 第一事务的完整 crash cuts 和逐对象并发断言；closed binding union 没有新增覆盖。故不能核销 #646。

本轮遵守“禁止自修自审”：只新增本评审报告，不修改被评审实现、测试、migration 或规格。

## 2. 阻断项

### P1-1：#646 的 canonical / Gate / Report / provenance acceptance matrix 仍不完整

#### Production canonical 与 persisted bytes：PARTIAL

`TestEmitInterruptT4PersistsProductionCanonicalTrace` 的方向正确：它安装真实 `shell.CallT4`，再比较导出的 trace input/output 与 `BuildT4Input`、`T4Contract` 结果，并断言最终 brief。它关闭了“测试完全绕开 production Brain serializer”的窄缺口。

但该用例选的是 `startup_stall`，而 [`interrupt.md` §3.6](../specs/interrupt.md#36-t4-接纳与命令-golden-vectors) 冻结并要求逐层重算的是两套互斥 golden：attempt `failure_review` 与 Report quota `failure_review`。新增测试没有证明：

- attempt golden 中 HTML/marker/命令 fragment 的 exact input/output、`EscapeT4Text` persisted bytes；
- quota 的 `attempt_no:null`、安全事件 link、两项 options 和 exact canonical input/output；
- 两个 variant 的 invalid output 经 production serializer/call 后回退到各自 exact persisted headline/brief/options/links；
- persisted `headline/options_json/links_json` 与 trace/source 全链一致。

现有 `emit_interrupt_acceptance_test.go` 仍是 storage 手工 map serializer；Report quota 测试仍只安装 storage seam 并比较 Go 字段/literal，不经过 production Brain serializer。因此 canonical 项只能从 NO 提升为 PARTIAL。

#### Gate failure-review 与 provenance：PARTIAL

`TestGateFailureReviewInterruptPersistsCalibrationAndBinding` 已从 command builder 提升到真实 `EvaluateRecordAndEmitInterrupt` happy path，证明 `mergeability_unknown` 能创建 evaluation、calibration 与 attempt `failure_review`。migration `0048_mergeability_unknown_provenance_identity.sql` 也正确把 `0047` 的 snapshot path 从不存在的 `$.change.id` 改为 canonical Gate input 的 `$.identity.change_id`，且编号符合约束。

但测试名所称的 “Binding” 未被读取或断言：它只检查 `CalibrationID`、Interrupt ID/reason/attempt。没有核对 `binding_json`/digest 中的 run、attempt/generation、`gate_recheck`、change/head，也没有核对 snapshot/evaluation/calibration 的逐层 FK 与 verdict provenance；没有 forged change/head/snapshot/evaluation 反例来证明 `0048` trigger 拒绝；没有 invalid T4 fallback/persisted bytes；也没有任一 provenance/binding 失败时 Gate evaluation、calibration、Interrupt、admission、event、delivery/outbox 与 Run 转移全回滚的 cut。

此外 #646 要求的八 arm closed binding-union合法/非法来源、typed/canonical/options/rollback矩阵在 #652 没有新增测试。migration `0048` 的窄修复有效，但不等于完整 persisted provenance acceptance closure。

#### Report quota crash/replay/concurrency：PARTIAL

#652 把发射事务 cut 从六张表扩到七张，加入 `budget_entries`，并在每个 cut 后移除 trigger、再次提交新 report，验证 generation-key 发射恢复收敛。这是有效关闭项。

剩余缺口仍包括：

- exhaustion 线性化事务仍只有 `report_quota_exhaustions` INSERT trigger；没有分别覆盖 rate bucket CAS、安全 event INSERT、唯一 winner 冲突及提交边界；
- 结构拒发后的 generation-key 诊断身份/幂等没有断言；
- 并发测试仍主要计数，没有逐对象核对 exhaustion/security-event identity、failure digest/generation key、binding JSON/digest、admission result及 charge FK、operation key/payload；
- replay 的每个对象只按总数验证，没有对 persisted identity/digest 做 exact 对账。

因此 Report 的 emission crash-cut 有明显进展，但完整 crash/replay/concurrency matrix 仍为 PARTIAL。

## 3. 回归与执行证据

- 完整读取 `gh issue view 658` 与 `gh issue view 658 --comments`：GitHub，0 comments；并回溯 #646、#652、PR #655。
- `git diff 09ce49c..ff3be3f --check`：**PASS**。
- `go test ./internal/storage/ ./internal/gate/ ./internal/intake/ ./cmd/siftd/ -count=1`：**PASS**。
- 新增 canonical / Gate 测试各 `-count=50`，Report crash/replay 测试 `-count=20`：**PASS**。
- 三组相关测试定向 `-race`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**FAIL**；一次并行运行出现 controlplane doctor 与 launchworker 时序失败，失败包单独重跑均通过，记非阻断 flake 风险。
- `cmd/siftd` 前次确定性失败用例 `-count=50`：**PASS**，#646 的该独立门禁阻断已关闭。
- PR #655：vet/test、schema drift及四平台 build checks均成功。
- #652 使用 migration `0048`；当前基线后续 #653 已连续到 `0049`，编号约束满足。

## 4. Issue #658 验收清单

| 条件 | 结果 | 说明 |
|---|---|---|
| 获取并阅读 #658 全文、Agent 建议、关闭条件、约束与 comments | **YES** | GitHub，0 comments。 |
| 对照 #646 FAIL / #652 | **YES** | 已复审实现提交、合入提交、migration 与 CI。 |
| production canonical serializer exact bytes 闭合 | **NO / PARTIAL** | 新增 startup-stall 生产链有效；指定 attempt/quota golden 仍未过 production serializer 全链。 |
| Gate failure-review acceptance 闭合 | **NO / PARTIAL** | 已过真实 Gate happy path；缺 exact binding/provenance、fallback 与 rollback。 |
| complete persisted provenance 闭合 | **NO / PARTIAL** | `0048` 修正 Change identity path；缺 persisted identity/digest 正反矩阵。 |
| closed binding-union matrix 闭合 | **NO** | #652 无新增八 arm acceptance 覆盖。 |
| Report crash/replay/concurrency 闭合 | **NO / PARTIAL** | 补 budget cut 和逐 cut 恢复；exhaustion 第一事务与逐对象 identity 仍缺。 |
| 要求的四个包测试 | **YES** | 全绿。 |
| 定向重复与 race | **YES** | 全绿。 |
| #646 的 `cmd/siftd` 确定性失败已关闭 | **YES** | 定向 `-count=50` 全绿。 |
| migration `0048` 约束 | **YES** | `0048` 存在且当前序列连续。 |
| 结论写入 `docs/reviews/`、仅当前 conventional worktree | **YES** | `feat/issue-658-rereview-t4-emitinterrupt-after-652`。 |
| 禁止自修自审、未 push/MR/merge | **YES** | 仅新增评审报告。 |
| #652 可按 #646 关闭标尺核销 | **NO** | 三大 acceptance 闭包及 binding union 仍未完成。 |

## 5. 最终裁决

**FAIL。** #652 的 production T4 seam、Gate happy path、Report budget/replay cuts及 migration `0048` 都是有效修复，且已恢复 #646 的 `cmd/siftd` 门禁；但指定 attempt/quota canonical golden、Gate/Binding complete provenance 与 Report 完整 crash/concurrency 对账仍未形成端到端验收闭包。补齐后须由不同代理再次复审。
