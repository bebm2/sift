FAIL

# M5 #683 Channel webhook after #677 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#677 / PR #680，实现提交 `6b2b41b63e1aaf452f5567b67de662d53e59b6bf`，合入提交 `ac0b10f`
> 评审基线：`main` / `origin/main` `ac0b10f`
> 判定基准：[#671 FAIL](2026-07-30-m5-channel-webhook-worker-671-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §2、§10](../specs/outbox.md)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #677 的 nonterminal reclaim wake 修复本身有效：`claimOutboxOperation` 记录原状态是否为 executing，在 Channel reclaim 成功提交、且下一 attempt 已租出后调用 `wakeOutbox`。因此 #671 复现的“count 2 经未达 attempt limit 的 lease expiry 跨 threshold 3，alert 与 attempt 4 同事务产生但不 wake”旁路已从生产代码关闭；terminal reclaim和 completion既有 wake未回退。该改动无 migration，schema仍为 `0050`，未违反“后续 migration 从 0051+ 起”。

但新增 `AlertWorker` 在 production 上无法执行第一次 Forge 证据查询。daemon 注入的是 `NewProductionAdapter` 产生的 budget-enforcing adapter；所有真实 Forge 调用必须携带 `forge.WithChargeKey(ctx, "forge-call:"+attemptID)`。`AlertWorker.RunOnce` claim 后直接把原始 `ctx`传给 `ListIssueComments` / `ListChangeComments`及 `CommentTarget`（`internal/forgeworker/alert.go:37-84`），没有设置 charge key。production adapter因此在发 CLI前固定返回 `ErrContractViolation: forge call requires a stable charge key`（`internal/forge/cli.go:88-100`）。worker又只单独识别 auth/capability，把该 contract violation降为 generic transient并持续 retry，所以 alert不会到达 Forge。#677没有新增任何 AlertWorker、daemon alert vertical或 purpose-filter测试，现有测试也无法捕获此 production-only断路。

故 #671 的“存在 production consumer”只达到结构接线层面的 **PARTIAL / NO**，不能按交付能力核销。#671 已明确仍开放的 P1-1/2/3/5没有实现增量；P1-4虽关闭 reclaim wake旁路，但 consumer不可用，且 broader failure/restart/concurrency矩阵仍开放。P1-6未回退。

本轮只新增评审报告，不修改被评审实现或规格。

## 2. #671 P1逐项复审

### P1-1 — PARTIAL / OPEN：Report production vertical无增量

#677仅修改 outbox claim、daemon wiring并新增 Forge alert worker，不涉及真实 `RecordReport` quota exhaustion到 frozen Channel、batch/member、production sealing、webhook及 completion的纵向验收。

### P1-2 — OPEN：exact sealer、canonical alert与response-loss replay无增量

#677不修改 production sealer、renderer或 Channel adapter测试。batch digest `ae3dba99…`仍只由手写 adapter payload命中；canonical alert digest `ba180536…`仍无 Go 测试命中。两名真实 member exact sealing、同 key/bytes response-loss replay及真实第三次 `rate_limited` canonical alert仍缺。

### P1-3 — PARTIAL / OPEN：authority broader matrix无增量

#677无 schema或 batch authority改动。此前已关闭的 terminal `3 states × 3 columns`保持；`i-a/i-b/i-c`并发双 target、critical identity collision、完整 upgrade/restart authority及 §6.6 production exact sealing仍缺。

### P1-4 — PARTIAL / OPEN：reclaim wake关闭，alert consumer production断路

有效增量：

- nonterminal executing reclaim在 durable commit后统一 wake，覆盖跨 threshold同时续租下一 attempt的路径；
- purpose-filtered claim只消费 `forge_alert/channel_failure`，不会误取其他 alert producer；
- daemon按 `forge_kind|host|project_key`组装 client map并在 `OutboxTick`调用 worker；marker查询先于发送，代码结构上避免 response-loss后盲发。

仍阻断：

- worker没有安装 outbox attempt charge key，production adapter拒绝其第一次 Forge调用；
- contract violation被降为 transient，operation会重试而不是暴露明确 contract终局；
- #677三文件 diff没有任何测试，既无 nonterminal threshold reclaim wake回归，也无 GitHub/GitLab、issue/change、marker replay、success、auth/transient/rate-limit failure及 non-recursion生产验收；
- 并发双 worker、restart count=2、第三次 rate-limited exact alert、success清零和完整 terminal projections仍未形成闭合矩阵。

因此 wake子项为 **YES（缺专门回归）**，可工作的 production consumer为 **NO**，P1-4总项保持 PARTIAL / OPEN。

### P1-5 — PARTIAL / OPEN：restart diagnostics与operator surface无增量

#677不修改 diagnostics、controlplane或 CLI；没有 DB reopen 后的 count/state/error/alert key/alert delivery state验证，也没有 `ops.ps` / `ops.doctor` 对 generated-not-delivered状态的验收。

### P1-6 — CLOSED，未回退

此次改动不触及 Channel resolver、sealed handle、webhook sender或错误摘要；`secret_ref:` handle-only与安全边界保持。

## 3. CI与执行证据

- `git diff 01183b1..ac0b10f --check`：通过；#677实现提交仅修改3文件，127行新增/4行删除，无 migration、无测试。
- `go vet ./...`：通过。
- `go test ./internal/storage ./internal/forgeworker ./internal/daemon -count=1`：通过。
- `go test -race ./internal/forgeworker -count=1`：通过；该包没有 AlertWorker测试。
- `go test ./... -count=1`：仅失败于既有 `internal/controlplane.TestDoctorBaselineChecksConfiguredDependencies` fixture command `signal: killed`及 `internal/launchworker.TestLaunchWorkerWrapperCrashSuite` marker时序；两项分别单独 `-count=3`通过，未发现与 #677三文件 diff的因果关系。
- PR #680四平台 build、schema drift、`vet + test`最终均为绿色。
- 静态 production-path交叉核验：daemon `internal/daemon/daemon.go:85-92,121-123`向 AlertWorker注入 require-budget adapter；AlertWorker `internal/forgeworker/alert.go:37-84`没有 `WithChargeKey`；adapter `internal/forge/cli.go:95-99`必然在外部调用前拒绝。

## 4. #683验收清单

- [x] 获取并阅读 #683全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #671 FAIL / #677复审 reclaim wake与 `forge_alert` consumer：**YES**
- [x] nonterminal reclaim跨 alert threshold后 post-commit wake：**YES（但 #677未加回归测试）**
- [x] terminal reclaim与 completion wake未回退：**YES**
- [x] daemon组装并调度 purpose-filtered `forge_alert/channel_failure` worker：**YES**
- [ ] production `forge_alert` consumer可完成 Forge证据查询/评论：**NO；缺 stable charge key**
- [ ] alert success/failure/response-loss/non-recursion验收：**NO**
- [x] schema保持0050，未新增错误编号 migration：**YES**
- [x] P1-6未回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1关闭：**NO / PARTIAL**
- [ ] P1-2关闭：**NO**
- [ ] P1-3完整矩阵关闭：**NO / PARTIAL**
- [ ] P1-4关闭：**NO / PARTIAL**
- [ ] P1-5关闭：**NO / PARTIAL**
- [ ] #677可按 #671关闭标尺整体核销：**NO**
- [x] PR #680 CI最终绿色：**YES**

## 5. 最终裁决

**FAIL。** #677正确关闭了 nonterminal threshold-crossing reclaim漏 wake，但新增 consumer遗漏 outbox attempt的 stable Forge charge key，production adapter会在任何证据查询或评论前拒绝，且该 contract violation被错误地转成 transient retry。P1-1/2/3/5仍按实现方自述开放，P1-4也因 consumer断路和验收矩阵缺失不能关闭。后续至少须为 AlertWorker每个 attempt安装 `forge-call:<attempt_id>` charge base，正确分类 contract/rate-limit结果，并补 production-adapter vertical及 issue/change、双 Forge、marker replay、success/failure/non-recursion测试；其后再完成其余 broader P1矩阵。
