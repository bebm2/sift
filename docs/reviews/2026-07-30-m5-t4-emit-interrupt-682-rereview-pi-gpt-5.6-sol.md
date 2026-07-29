FAIL

# M5 #682 T4→EmitInterrupt after #676 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#676 / PR #679，实现提交 `a617cbd`，合入提交 `44adec2`
> 评审基线：`main` / `origin/main` `ac0b10f`（另含后续 #677）
> 前次结论：[#670 FAIL](2026-07-30-m5-t4-emit-interrupt-670-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §3.6/§9.2](../specs/interrupt.md)、[`brain.md` §11](../specs/brain.md)、[`gate.md` §5/§7](../specs/gate.md)、[`report.md` §7](../specs/report.md)、[`storage.md` §6.4/§12.2.1](../specs/storage.md)

## 1. 结论

**FAIL（2×P1）。** #676 是有效的测试增量：attempt production 用例已由 `startup_stall` 改为真实 failed-attempt `failure_review`，并冻结本用例的 trace input/output 与 escaped brief；quota production happy path也改为 literal canonical bytes；Gate 两个测试终于命中 `failure_review_attempt + gate_recheck`，对账 exact binding/digest，并证明 binding INSERT 失败时 Gate provenance、Ledger、Interrupt、admission、delivery 与 Run 转移回滚；Report 增加安全事件 INSERT cut，并在四 writer 用例中核对 quota binding/digest 和 admission→Interrupt FK。PR #679 CI、要求的四包、定向重复及 race 均通过。

但 #670 的关闭矩阵仍未闭合。attempt 新用例不是规格 §3.6 冻结的完整 command/marker golden，且只覆盖合法输出；attempt/quota 两个 production 链均没有让无效 provider 输出经过 `Shell.CallT4` 后对账各自 exact fallback source、headline/brief/options/links bytes。Gate 虽修正 arm，却仍没有 forged change/head/snapshot/evaluation provenance 反例或该 arm 的 invalid-T4 fallback；Report 第一事务仍缺 rate-bucket CAS、唯一 winner/提交边界，四 writer 仍未对账 generation/failure digest、安全事件 payload、charge FK 及 outbox key/payload。因此本轮不能核销 #670。

本轮遵守“禁止自修自审”：只新增本评审报告，不修改被评审实现、测试、migration 或规格。

## 2. 阻断项

### P1-1：attempt/quota production canonical 与 fallback goldens仍不完整

`TestEmitInterruptT4PersistsProductionCanonicalTrace` 现在确实走 production `Shell.CallT4` 和 attempt `failure_review`，并把 expected trace 改为 literal bytes，这是相对 #670 的明确进展。但它没有实现 [`interrupt.md` §3.6](../specs/interrupt.md#36-t4-接纳与命令-golden-vectors) 的完整 attempt vector：

- 冻结 fragments 是 `[/<!-- sift-op:x -->, <b>风险</b>, retry]`，不是规格要求同时覆盖 `/sift reject`、`<!-- sift-op:x -->` 与 `<b>风险</b>` 的 exact input/output；output 也没有 command fragment。
- persisted 只断言 `Brief`，没有从数据库逐字节对账 `headline/brief_markdown/options_json/links_json`，也没有对账正常 source。
- provider 只返回合法输出，没有让 options 重排、未知 fragment/HTML delta或未知 recommended option 经 production shell/trace 回退，更没有断言 fallback trace source和 exact persisted bytes。

quota production 用例同样只覆盖合法 `reject,hold` 输出。它以运行时安全事件 ID 插入 literal 模板是合理的动态身份处理，但测试名中的 `PersistedFallback` 并未发生：没有无效 output，没有 fallback source，也没有数据库原始 `options_json/links_json` bytes。既有 `TestReportQuotaT4InvalidOutputsFallBack` 仍直接安装 storage seam，未经过 production Brain serializer/call/trace；既有 storage canonical helper也不能替代这条生产链。

因此正常 happy-path literal 有进展，但 #670 要求的两 variant production canonical + invalid-output fallback goldens仍为 **PARTIAL**。

### P1-2：Gate provenance反例与Report crash/concurrency对象闭包仍缺

#### Gate failure-review provenance/rollback：PARTIAL

#676 已修正 #670 最直接的错误：两个测试现在使用 `InterruptFailureReview`、`FailureReviewAttempt`、`FailureReviewGateRecheck`，binding literal也包含 attempt/generation/change/head/terminal pair；binding failure cut 对 Gate snapshot/evaluation/calibration/Ledger、Interrupt、admission、budget/event/delivery和 Run状态的回滚断言有效。seed 自带的 launch outbox 被明确保留为 1，不是发射泄漏。

但前次要求的 complete provenance 仍未形成正反闭包：

- happy path只读取 snapshot head/effective-policy hash及 evaluation/calibration run ID；没有伪造 change/head、snapshot identity、evaluation/calibration链或 policy provenance的拒绝反例。
- rollback仅注入 binding INSERT失败，没有证明该 failure-review arm 的 provenance身份不一致会被 schema/trigger拒绝并全回滚。
- 仍没有 Gate failure-review 的 invalid T4 production fallback/persisted bytes。

所以 exact arm和一个事务 cut已关闭，但“provenance/rollback”整体仍是 **PARTIAL**。

#### Report crash/replay/concurrency：PARTIAL

新增 `security.report_quota_exhausted` event INSERT trigger正确证明安全事件与 exhaustion/rate token同事务回滚；并发测试新增的 exact quota binding、binding digest和 admission→Interrupt FK也有效。

仍缺 #670 列出的关键证据：

- exhaustion第一事务没有 rate-bucket CAS cut、唯一 winner冲突 cut及提交前/后边界；现有两个 cut只覆盖 exhaustion INSERT和安全 event INSERT。
- 结构拒发后的 generation-key诊断身份与幂等仍未断言。
- 四 writer只新增 binding/admission部分身份；没有逐对象核对 exhaustion→security event row/payload、canonical failure digest与 generation key、admission kind及 attention charge NULL/非 NULL关系、Report charge FK，以及 forge-comment operation key/payload/subject身份。
- replay恢复仍主要按表计数，未逐对象证明重试复用同一 exhaustion/security-event/generation身份。

因此 crash/replay/concurrency矩阵仍不足以满足 [`report.md` §7](../specs/report.md) 的逐行验收。

## 3. 回归与执行证据

- 完整读取 `gh issue view 682` 与 `gh issue view 682 --comments`：GitHub，0 comments；并回溯 #670、#676及 PR #679。
- `git diff f63bc2e..44adec2 --check`：**PASS**；#676仅修改四个测试/测试 seed文件，共 74 insertions、32 deletions。
- `go test ./internal/storage/ ./internal/gate/ ./internal/brain/ ./cmd/siftd/ -count=1`：**PASS**。
- 新增两条 Brain production测试 `-count=50`：**PASS**；Gate/Report定向测试 `-count=20`：**PASS**。
- 上述 Brain与storage定向测试 `-race`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**FAIL**；并行全仓运行出现已知时序型 `controlplane` doctor 命令被 kill，以及 `launchworker` started-file缺失。两个失败集合单独重跑均 **PASS**，记非阻断 flake风险，不归因于 #676。
- PR #679：vet/test、schema drift及四平台 build checks均成功。
- #676未新增 migration；当前基线序列连续至 `0050`，符合“仅需要时从 0051+”约束。

## 4. Issue #682 验收清单

| 条件 | 结果 | 说明 |
|---|---|---|
| 获取并阅读 #682全文、Agent建议、关闭条件、约束与comments | **YES** | GitHub，0 comments。 |
| 对照 #670 FAIL / #676 | **YES** | 已复审实现/合入提交、完整四文件diff及 PR CI。 |
| attempt production canonical golden闭合 | **NO / PARTIAL** | 已改为 failure-review literal happy path；仍非规格完整 command/marker vector，缺 production invalid fallback及完整 DB bytes。 |
| Report-quota production canonical golden闭合 | **NO / PARTIAL** | literal happy path有效；缺 production invalid fallback/source及原始 options/links bytes。 |
| exact Gate failure-review provenance/rollback闭合 | **NO / PARTIAL** | exact arm、digest和 binding-failure回滚已补；缺 forged provenance反例与 invalid-T4 fallback。 |
| Report crash/replay/concurrency完整对象断言闭合 | **NO / PARTIAL** | 补安全-event cut和部分并发身份；仍缺 rate CAS/唯一 winner/提交边界及 generation、charge、outbox逐对象对账。 |
| 要求的四个包测试 | **YES** | 全绿。 |
| 定向重复、race及 vet | **YES** | 全绿；PR #679 CI全绿。 |
| 全仓测试 | **NO（非阻断 flake）** | doctor/launchworker并行时序失败，失败集合单独重跑通过。 |
| migration约束 | **YES** | #676无migration；当前连续至0050。 |
| 结论写入 `docs/reviews/`、仅当前conventional worktree | **YES** | `feat/issue-682-rereview-t4-emitinterrupt-after-676`。 |
| 禁止自修自审、未push/MR/merge | **YES** | 仅新增评审报告。 |
| #676可按 #670关闭标尺核销 | **NO** | production fallback、Gate provenance反例及Report完整矩阵仍未闭合。 |

## 5. 最终裁决

**FAIL。** #676 修正了 Gate arm误测，增加了 attempt/quota production literal happy paths、Gate binding回滚和Report安全事件/部分并发身份证据；但两套 production invalid-output fallback、Gate complete provenance反例及Report第一事务/逐对象矩阵仍未闭合。须补齐后由不同代理再次复审。
