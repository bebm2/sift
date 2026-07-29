FAIL

# M5 #646 T4→EmitInterrupt after #640 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#640 / PR #643，实现提交 `696e48d`，合入提交 `50326c6`
> 评审基线：`main` / `origin/main` `a2bd374`（另含后续 #641）
> 前次结论：[#634 FAIL](2026-07-30-m5-t4-emit-interrupt-634-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §3.6/§9.2](../specs/interrupt.md)、[`report.md` §7](../specs/report.md)、[`gate.md` §5/§7](../specs/gate.md)

## 1. 结论

**FAIL（2×P1）。** #640 增加了两组手工 canonical map vectors、若干 code-review binding 反例、一个 Gate command 组装测试，以及六个 Report quota 发射事务 INSERT cut 和更强的并发计数；要求的三个包普通测试与定向 `-race` 均通过。这些是有效增量。

但 #634 的四项关闭条件仍未形成端到端 acceptance matrix：canonical 测试没有调用生产 `brain.BuildT4Input`/`T4Contract`，attempt 也未经过 `EmitInterrupt` 核对 persisted bytes；binding 反例仍集中在一条 code-review Interrupt；Gate 测试只调用 command builder；Report crash cuts 仍缺 exhaustion 事务和收费对象等边界。此外，PR #643 自身的 `vet + test` check 为红，当前基线可稳定复现 `cmd/siftd` 的生产调度器测试失败。因此不能核销 #634。

本轮遵守“禁止自修自审”：只新增本评审报告，不修改被评审实现、测试或规格。

## 2. 阻断项

### P1-1：#634 指定的四项 acceptance matrix 仍未闭合

#### Canonical serializer / persisted bytes：NO / PARTIAL

`TestEmitInterruptT4CanonicalAttemptAndQuotaVectors` 的 expected literals 与规格文本一致，但它在 storage 测试中重新手工组装 `map[string]any`，再调用 `internal/storage/ledger.go` 的 `canonicalJSON`。生产 T4 路径实际是 `storage.InterruptT4Input → brain.Shell.CallT4 → brain.BuildT4Input/T4Contract`；新增测试没有调用这条 serializer，也没有验证 Brain trace 中冻结的 input/output bytes。

更直接地，quota 的手工 vector 使用未转义的 `report_interrupt_quota_exhausted`，而同文件的生产 `RecordReport → EmitInterrupt` 测试仍明确期待传给 T4 的 `fallback_brief` 为 `report\_interrupt\_quota\_exhausted`。两组测试同时为绿，恰好证明新增 map vector与生产 input并未逐字节对账。

attempt vector只调用 `acceptInterruptT4`，未走 `EmitInterrupt`，也没有读取 `headline/brief_markdown/options_json/links_json`。quota 的 production persisted 断言是既有覆盖，#640 没有把 input/output canonical bytes、fallback及最终持久化串成同一条链。把共享 Ledger helper改为 `SetEscapeHTML(false)` 不等于验证 T4 的生产 serializer，且该生产改动会改变 Ledger 中含 `<>&` 文本的 canonical bytes，却没有相应回归 vector。

#### Closed binding union：NO / PARTIAL

新增 malformed/null/type/extra/order 反例全部向一条合法 `code_review` Interrupt直接插第二条 forged binding；新增两个 failure-review arm 用例也只是把来源不匹配的 arm插到同一 code-review Interrupt。它们没有形成八个 arm 的 closed matrix，也没有证明：

- 每个合法 arm 经真实 `EmitInterrupt` 生成的 JSON/digest/options可成功提交；
- attempt `new_attempt` terminal pair、`gate_recheck` calibration/change/head及 quota exhaustion/event分别逐字段绑定；
- wrong run/generation/non-failed/pair mismatch、attempt/quota字段混入及两个 arm options交叉错配均拒绝；
- 每个 arm 的拒绝均使五件事、binding、delivery和Run转移整体回滚。

现有 SQL trigger 与零散旧测试可能覆盖其中部分规则，但 #640 的 acceptance matrix并未按 #634 / `report.md` §7.8 逐项闭合。

#### Gate failure-review：NO / PARTIAL

`TestGateFailureReviewInterruptUsesClosedAttemptBinding` 只调用未导出的 `interruptCommand` 并检查内存中的 `EmitInterruptCmd`。测试中的 attempt `3/generation 4`、Change、Gate snapshot/evaluation/calibration都未落库；它不调用 `EvaluateRecordAndEmitInterrupt` 或 `RecordGateEvaluationAndEmitInterrupt`，不验证 binding trigger、Interrupt、attention、event、outbox和calibration identity全有或全无，也没有 invalid T4 fallback/persisted bytes。故它不能替代 #634 要求的 Gate failure-review acceptance路径。

#### Report crash-cut / concurrency：NO / PARTIAL

新增六个发射 INSERT cut能证明对应失败时已提交 exhaustion 保留、发射事务回滚；并发用例也补了 Interrupt/admission/binding/outbox/delivery/security-event/rate-token计数。但完整矩阵仍缺：

- exhaustion 线性化事务中 rate bucket CAS、安全 event、exhaustion unique winner及各提交前/后 cut；
- 发射事务的 `budget_entries` / attention charge cut与断言，以及结构拒绝诊断/generation identity；
- 每个发射 cut恢复后的 replay收敛，而不只是一个 binding cut的恢复；
- 并发结果逐对象核对 generation key、binding内容、admission/charge FK、operation key及唯一安全事实。

所以 #634 所列 Report quota crash/concurrency仍只能记 PARTIAL。

### P1-2：PR #643 的全仓门禁为红，当前稳定复现

PR #643 的 GitHub `vet + test` check结论为 **FAILURE**。失败为：

```text
TestProductionSchedulerWakesOutboxAfterEnqueueAndEmitInterrupt
constraint failed: invalid interrupt binding identity (1811)
```

当前基线执行该测试 `-count=50` 为 **50/50 FAIL**；`go test ./... -count=1` 同样在 `cmd/siftd` 失败。原因表面是生产唤醒测试直接 `EmitInterrupt(merge_conflict)`，而 rebase 后的 calibrated provenance trigger要求该 binding有 Gate snapshot/evaluation/calibration来源；测试没有建立该来源。#640 明确要求基于含 #633 的 main，不能以三个定向包为绿掩盖合入基线的确定性全仓失败。

同一次全仓运行另有 `internal/controlplane` doctor fixture被 kill；它符合项目已知时序 flake特征，单独记非阻断注记，不用于上述 P1判定。

## 3. 回归与执行证据

- 完整读取 `gh issue view 646` 与 `gh issue view 646 --comments`：GitHub，0 comments；并回溯 #634、#640及 PR #643。
- `git diff f153348..50326c6 --check`：**PASS**。
- `go test ./internal/storage/ ./internal/gate/ ./internal/intake/ -count=1`：**PASS**。
- #640新增/相关定向 storage tests `-count=20`：**PASS**。
- Gate新增测试 `-count=100`：**PASS**。
- 定向三个包 `-race`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**FAIL**（`cmd/siftd`确定性 binding identity失败；另见一次 doctor时序失败）。
- `go test ./cmd/siftd -run '^TestProductionSchedulerWakesOutboxAfterEnqueueAndEmitInterrupt$' -count=50`：**FAIL**（50/50）。
- PR #643：四平台 build和schema drift成功；`vet + test`失败。
- #640未新增 migration；其实现基线已有 `0001`–`0045`，符合“仅需要时从 0046+”约束。当前后续 #641新增了 `0046`，编号连续。

## 4. Issue #646 验收清单

| 条件 | 结果 | 说明 |
|---|---|---|
| 获取并阅读 #646全文、Agent建议、关闭条件、约束与comments | **YES** | GitHub，0 comments。 |
| 对照 #634 FAIL / #640 | **YES** | 已逐项复审实现提交、合入提交与CI。 |
| canonical serializer exact bytes闭合 | **NO** | 新测试使用storage手工map，不是生产Brain serializer；production quota input与手工vector可同时不一致。 |
| attempt / Report quota persisted-byte闭合 | **NO / PARTIAL** | quota既有persisted断言有效；attempt未走EmitInterrupt持久化，完整input/output链未对账。 |
| closed binding-union matrix闭合 | **NO / PARTIAL** | code-review集中反例有增量；缺各arm合法/非法来源、options及回滚矩阵。 |
| Gate failure-review闭合 | **NO / PARTIAL** | 仅command builder；未过Gate storage/calibration/EmitInterrupt事务。 |
| Report crash-cut / concurrency闭合 | **NO / PARTIAL** | 六个发射cut与并发计数有效；缺exhaustion/charge/replay/逐对象矩阵。 |
| #640要求的三个包普通测试 | **YES** | 全绿。 |
| 定向 race | **YES** | 全绿。 |
| 全仓 test/vet | **NO** | vet绿；test有确定性`cmd/siftd`失败。 |
| migration约束 | **YES** | #640无migration；当时连续至0045。 |
| 结论写入 `docs/reviews/`、仅当前 conventional worktree | **YES** | `feat/issue-646-rereview-t4-emitinterrupt-after-640`。 |
| 禁止自修自审、未 push/MR/merge | **YES** | 仅新增评审报告。 |
| #640可按 #634关闭标尺核销 | **NO** | 四项矩阵仍未闭合，且PR全仓门禁为红。 |

## 5. 最终裁决

**FAIL。** #640 的局部 vectors、binding反例、Gate command断言和Report发射cut均为有效增量，但它们没有把生产 canonical serializer、各 binding arm、Gate failure-review事务及Report完整crash/replay对象串成验收闭包；同时 PR自身及当前基线均存在稳定的全仓测试失败。补齐矩阵并恢复全仓门禁后，须由不同代理再次复审。
