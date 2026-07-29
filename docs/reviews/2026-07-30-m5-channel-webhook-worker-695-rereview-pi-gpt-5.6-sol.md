FAIL

# M5 #695 Channel webhook after #689 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#689 / PR #691，实现提交 `3e6eb11a8fc3c94b1a7390502e3c89dedb6094db`，合入提交 `0dba5a8`
> 评审基线：`main` / `origin/main` `d2be519`
> 判定基准：[#683 FAIL](2026-07-30-m5-channel-webhook-worker-683-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §2、§10](../specs/outbox.md)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)、[`WBS.md` M5 §5.2](../WBS.md#52-interrupt-全功能与-channel)

## 1. 结论

**FAIL。** #689 正确关闭了 #683 的直接 production blocker。`AlertWorker` claim 后以 immutable outbox `attempt_id` 安装 `forge-call:<attempt_id>` charge base，再用同一 context 执行 marker evidence lookup 与评论；production adapter为两次调用分别派生 `:1`、`:2`。新增 GitHub production-adapter测试逐项执行 issue/change target，证明 lookup与comment均实际收费；contract、auth/capability、semantic conflict及rate-limit映射也已与 outbox结果对齐。issue response-loss测试证明远端已有 marker时reclaim不会再次评论。因此 production `forge_alert/channel_failure` consumer不再因缺 charge key在第一次 Forge调用前固定断路。

但 #689 的关闭条件不只要求修复该 blocker，还要求继续关闭 #683 中 P1-1..P1-5。此次两文件 diff只修改 `AlertWorker`及其测试，没有补 Report quota → Channel纵向、production exact sealer/canonical alert digest、batch authority完整矩阵、failure episode/restart/concurrency/terminal矩阵，或 `ps`/`doctor`重启投影。新增 charge测试也只覆盖单个 attempt 内的稳定 base，未显式执行一次 retry/reclaim并断言新 attempt使用自己的稳定 base；marker replay只覆盖 Fake issue client，没有 change或production-adapter replay向量。这些测试缺口不推翻窄修复，但不能把 wave-1 Channel范围判为闭合。

故 #683 的直接 charge-key阻断可核销，P1-4 的 production consumer子项从 NO 提升为 YES；#689整体及 WBS M5 §5.2 wave-1 Channel仍不能通过。

本轮只新增评审报告，不修改实现、规格或WBS。

## 2. #695 Must verify

### 2.1 AlertWorker charge key：实现 YES，跨 attempt retry验收 NO

`internal/forgeworker/alert.go:65` 使用 `forge.WithChargeKey(ctx, "forge-call:"+c.AttemptID)`，且安装点位于 target校验后、任何 evidence lookup/comment前。一个 claimed attempt内的所有真实调用共享稳定 base，adapter只追加单调 call sequence，符合 outbox §2 的 `forge-call:<outbox_attempt_id>:<call_seq>`。

`TestAlertWorkerProductionAdapterChargesStableAttemptKey` 对 issue/change各验证一次 lookup与comment，断言同一 attempt base及 `:1/:2`。但测试没有制造 retryable completion或lease reclaim后再次走 production adapter，也没有断言第二个 outbox attempt获得不同且稳定的新 base。因此行为可由实现与storage attempt identity静态确认，但“across retries”的专门回归证据仍为 NO。

### 2.2 Production issue/change alert delivery charging tests：YES（单 Forge）

`internal/forgeworker/alert_test.go:47-74` 使用 `forge.NewProductionAdapter`和真实 budget-enforcing调用链，分别走 issue/change comment lookup与POST；两格均观察到两个charge key。定向测试与race测试通过。

限制：fixture只构造 GitHub adapter；没有 GitLab production adapter格，也未查询最终 operation state。两次charge及无RunOnce错误足以关闭本轮缺charge key的直接阻断，但不是双 Forge完整资格矩阵。

### 2.3 Classification与marker replay：YES（窄覆盖）

`internal/forgeworker/alert.go:88-105`将：

- `auth_or_capability` → terminal `failed/auth_or_capability`；
- `contract_violation` → terminal `failed/contract_violation`；
- `semantic_conflict` → terminal `conflict/semantic_conflict`；
- `rate_limited` → `retryable/rate_limited`并保存非负Retry-After。

`TestAlertWorkerClassifiesForgeFailures`覆盖四格及rate-limit 60秒。`TestAlertWorkerMarkerReplayDoesNotResend`模拟远端评论成功、本地completion失败、lease到期reclaim，第二次lookup命中operation marker且发送计数保持1。限制是分类测试只注入issue lookup错误，marker replay只使用Fake GitHub issue；change、comment-call error、production adapter replay尚无矩阵。

## 3. #683 P1与wave-1 Channel清单

严格按完整wave-1交付记YES/NO，不以局部实现替代纵向验收：

| 检查项 | YES/NO | 复审结论 |
|---|---|---|
| P1-1 Report quota → frozen Channel → batch/seal/wake/webhook/completion production vertical | **NO** | #689不修改Report、Channel producer/sealer或纵向测试。 |
| P1-2 两成员production exact sealing、`ae3dba99…`、canonical `ba180536…`及同key/bytes response-loss replay | **NO** | 新测试手写alert payload；未经过sealer或第三次Channel failure renderer。 |
| P1-3 batch authority、collision、并发、upgrade/restart及terminal完整矩阵 | **NO** | #689无storage/schema/batch改动或测试。 |
| P1-4 nonterminal/terminal wake + 可工作的purpose-filtered production alert consumer | **YES** | #677 wake保留；#689补stable charge base和production issue/change调用证据。 |
| P1-4 failure/reclaim/restart/double-worker/success/alert-failure完整矩阵 | **NO** | #689只补AlertWorker窄测试。 |
| P1-5 DB reopen后episode/delivery/alert投影及`ops.ps`/`ops.doctor` | **NO** | 无diagnostics、controlplane或CLI增量。 |
| P1-6 `secret_ref:` handle-only、安全错误摘要与非递归边界未回退 | **YES** | #689不触及Channel resolver/payload持久化；AlertWorker只消费Forge alert。 |
| webhook adapter只消费closed sealed handle且不改领域事实 | **YES** | 既有边界未回退。 |
| 单条与batch完整at-least-once/replay验收 | **NO** | 仅Forge alert的issue marker replay；不是Channel webhook单条+batch exact replay。 |
| batch单一verified Forge target与不可retarget完整验收 | **NO** | 既有部分guard保留，但完整authority矩阵仍缺。 |
| durable episode阈值唯一alert、terminal停止、success清零与alert失败不递归完整验收 | **NO** | consumer现可运行，完整§6.6矩阵仍未闭合。 |
| `ps`/`doctor`重启后显示delivery/episode/alert/generated-not-delivered | **NO** | 无operator-surface验收。 |
| sealed payload不改写、不重复收费；升级沿原Channel；无第二Channel/TTS/Brain旁路 | **NO** | #689没有交付该完整纵向范围；仅未观察到回退。 |
| WBS M5 §5.2“实现首个Channel；连续失败N次转Forge告警并在ps/doctor显示” | **NO** | Forge告警consumer直接断路已修，但production vertical及ps/doctor仍未闭合。 |

## 4. 执行证据

- `git diff 6b2b41b..3e6eb11 --check`：通过；实现提交仅修改 `internal/forgeworker/alert.go` 与新增 `internal/forgeworker/alert_test.go`，共176行新增、2行删除，无migration。
- `go test ./internal/storage/ ./internal/forgeworker/ ./cmd/siftd/ -count=1`：通过。
- `go vet ./internal/storage/ ./internal/forgeworker/ ./cmd/siftd/`：通过。
- `go test -race ./internal/forgeworker -count=1`：通过。
- `go test ./internal/forgeworker -run 'TestAlertWorker' -count=20`：通过。
- PR #691说明的范围仅为stable forge-call charge key；无review comment，已由PR #691合入。
- 工作区在写报告前无未提交改动；branch为conventional worktree branch，未push/MR/merge。

## 5. #695验收清单

- [x] 获取并阅读 #695全文、Must verify、References、Constraints及comments：**YES**（无comments、无单列Agent建议）
- [x] 获取并阅读 #689、#683全文与comments：**YES**
- [x] 检测Forge并使用 `gh`：**YES**
- [x] AlertWorker每个attempt安装稳定Forge charge base：**YES**
- [ ] 显式retry/reclaim后charge-key回归测试：**NO**
- [x] production issue alert delivery charging测试：**YES**
- [x] production change alert delivery charging测试：**YES**
- [ ] GitHub + GitLab双Forge production charging矩阵：**NO**
- [x] contract/rate-limit/auth/conflict分类：**YES**
- [x] marker response-loss replay不重发：**YES**
- [ ] issue/change × production-adapter marker replay矩阵：**NO**
- [x] #683缺stable charge key的直接阻断关闭：**YES**
- [ ] #689关闭条件P1-1..P1-5整体关闭：**NO**
- [ ] wave-1 Channel scope关闭：**NO**
- [x] 仅写 `docs/reviews/`：**YES**
- [x] 未push/MR/merge：**YES**

## 6. 最终裁决

**FAIL。** #689已有效修复AlertWorker production charge key，并补了issue/change收费、四类错误映射与issue marker replay；#683所报“第一次Forge调用前固定拒绝”可关闭。但#689明确要求继续关闭P1-1..P1-5，而本次实现没有触及P1-1/2/3/5，P1-4也只关闭consumer子项、未完成failure/restart/concurrency/terminal矩阵。补一次真实retry/reclaim charge-key回归、GitLab及change marker replay可增强该窄修复证据；更关键的剩余阻断仍是Report→Channel纵向、exact sealer/canonical alert、完整authority/failure矩阵及重启后`ps`/`doctor`验收。