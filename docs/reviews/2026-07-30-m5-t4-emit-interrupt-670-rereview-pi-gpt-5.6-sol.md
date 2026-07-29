FAIL

# M5 #670 T4→EmitInterrupt after #664 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#664 / PR #667，实现提交 `882e757`，合入提交 `f1f98cd`
> 评审基线：`main` / `origin/main` `ba3b759`（另含后续 #665）
> 前次结论：[#658 FAIL](2026-07-30-m5-t4-emit-interrupt-658-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §3.6/§9.2](../specs/interrupt.md)、[`brain.md` §11](../specs/brain.md)、[`gate.md` §5/§7](../specs/gate.md)、[`report.md` §7](../specs/report.md)、[`storage.md` §6.4/§12.2.1](../specs/storage.md)

## 1. 结论

**FAIL（2×P1）。** #664 的两个新增测试是有效增量：Report-quota happy path 首次经过 production `Shell.CallT4` 并对账 Brain trace；Gate `code_review` happy path新增 binding digest/FK provenance 查询，binding INSERT 注入也证明该路径的 Gate 与发射对象整体回滚。PR #667 CI、要求的四包、全仓 test/vet、定向重复及 race 均通过。

但 #658 的关闭条件仍未满足。新增 production canonical 用例只有 quota 正常输出，expected bytes 又由捕获到的 production input动态重算；attempt `failure_review`、两种 variant 的 invalid-output fallback及冻结 literal bytes仍未形成 production golden。两个名为 Gate failure-review 的新增 storage 测试实际都发射 `InterruptCodeReview`，没有核验或回滚 Gate `failure_review_attempt/gate_recheck` provenance。#664 完全未修改 Report crash/concurrency测试，#658 列出的 exhaustion 第一事务 cuts和逐对象并发对账原样存在。

本轮遵守“禁止自修自审”：只新增本评审报告，不修改被评审实现、测试、migration或规格。

## 2. 阻断项

### P1-1：attempt/quota production canonical goldens仍未闭合

`TestEmitInterruptQuotaT4UsesProductionCanonicalTraceAndPersistedFallback` 确实把 quota调用接到 `Shell.CallT4`，但它只覆盖合法 provider输出：

- `wantInput` 从测试运行时捕获的 `gotInput` 再调用 `BuildT4Input`生成，并非规格冻结的逐字节 literal；生产组装字段漂移时expected会同步漂移。
- `wantOutput`同样从 provider文本经当前 `T4Contract`动态生成；没有固定 canonical output bytes。
- 测试名虽含 `PersistedFallback`，provider返回的是合法正常结果，没有触发 fallback，也没有断言 fallback source/trace及 exact persisted fallback bytes。
- persisted断言只比较返回对象的 headline/brief和option ID；没有将数据库中的 `headline/brief_markdown/options_json/links_json`及正常/兜底 source逐字节串到同一 production trace。

attempt侧没有新增 production用例。既有 `TestEmitInterruptT4PersistsProductionCanonicalTrace`发射的是 `startup_stall`，不是 #658 指定的 attempt `failure_review`；`TestEmitInterruptT4CanonicalAttemptAndQuotaVectors`仍在 storage测试中手工组装 map并调用 storage canonical helper。因此 attempt/quota exact input/output、HTML/marker/command escaping、互斥 options及两套 invalid-output fallback仍为 **PARTIAL**，不能称为 canonical goldens闭合。

### P1-2：Gate failure-review provenance与Report crash/concurrency仍阻断

#### Gate provenance rollback：PARTIAL

新增 `TestGateFailureReviewPersistsExactBindingProvenance` 和 `TestGateBindingFailureRollsBackProvenanceAndEmission` 名称写 failure-review，但命令实际为：

```go
Reason: InterruptCodeReview
```

其 binding arm是 `code_review`，不是 #658 要求的 Gate `failure_review_attempt`（`retry_kind=gate_recheck`及 attempt/generation/change/head/terminal pair）。所以新增查询与 rollback只证明 code-review路径；既有真实 Gate failure-review测试仍只断言 calibration/Interrupt ID、reason和attempt非空，未对账 exact binding JSON/digest、snapshot/evaluation/calibration逐层身份，也未在该 arm的 binding/provenance失败下注入并证明全回滚。forged change/head/policy provenance反例及 invalid T4 fallback仍未补齐。

#### Report crash/replay/concurrency：NO CHANGE / PARTIAL

`837952d..f1f98cd`中 Report实现和测试零改动。#658 的剩余项因此原样存在：

- exhaustion第一事务仍只在 `report_quota_exhaustions` INSERT注入；未分别覆盖 rate bucket CAS、安全事件 INSERT、唯一 winner冲突和提交边界。
- 结构拒发后的 generation-key诊断身份与幂等仍无 exact断言。
- 四 writer并发用例仍以表计数为主，没有逐对象核对 exhaustion/security-event identity、failure digest/generation key、quota binding JSON/digest、admission/charge FK及 operation key/payload。

实现方主要依赖 storage侧既有测试不能消除这些缺口；尤其 #664 没有新增任何 Report证据。

## 3. 回归与执行证据

- 完整读取 `gh issue view 670` 与 `gh issue view 670 --comments`：GitHub，0 comments；并回溯 #658、#664及 PR #667。
- `git diff 837952d..f1f98cd --check`：**PASS**。
- `go test ./internal/storage/ ./internal/gate/ ./internal/brain/ ./cmd/siftd/ -count=1`：**PASS**。
- 新增 Brain测试 `-count=50`、相关 storage/Gate/Report测试 `-count=20`：**PASS**。
- 上述定向 Brain与storage测试 `-race`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**PASS**。
- PR #667：vet/test、schema drift及四平台 build checks均成功。
- #664未新增 migration；当前基线序列连续至 `0050`，无编号冲突。

## 4. Issue #670 验收清单

| 条件 | 结果 | 说明 |
|---|---|---|
| 获取并阅读 #670全文、Agent建议、关闭条件、约束与comments | **YES** | GitHub，0 comments。 |
| 对照 #658 FAIL / #664 | **YES** | 已复审实现提交、合入提交、PR CI及完整diff。 |
| attempt production canonical golden闭合 | **NO** | production测试仍是 `startup_stall`；attempt `failure_review`仍仅storage手工vector。 |
| Report-quota production canonical golden闭合 | **NO / PARTIAL** | 新增正常production链，但expected动态自算，缺literal bytes和invalid fallback。 |
| exact Gate failure-review provenance rollback闭合 | **NO / PARTIAL** | 新增测试实际为 `code_review` arm，不是Gate attempt failure-review。 |
| Report crash/replay/concurrency完整对象断言闭合 | **NO / PARTIAL** | #664无Report改动；第一事务cuts和逐对象identity仍缺。 |
| 要求的四个包测试 | **YES** | 全绿。 |
| 定向重复、race及全仓门禁 | **YES** | 全绿；PR #667 CI全绿。 |
| migration约束 | **YES** | #664无migration；当前连续至0050。 |
| 结论写入 `docs/reviews/`、仅当前conventional worktree | **YES** | `feat/issue-670-rereview-t4-emitinterrupt-after-664`。 |
| 禁止自修自审、未push/MR/merge | **YES** | 仅新增评审报告。 |
| #664可按 #658关闭标尺核销 | **NO** | canonical、Gate failure-review及Report矩阵仍未闭合。 |

## 5. 最终裁决

**FAIL。** #664 增加了 quota production happy path和 code-review provenance/rollback证据，但没有提供 attempt/quota两套冻结 canonical goldens，没有命中 Gate failure-review provenance arm，也没有改变 Report crash/concurrency矩阵。须补齐后由不同代理再次复审。
