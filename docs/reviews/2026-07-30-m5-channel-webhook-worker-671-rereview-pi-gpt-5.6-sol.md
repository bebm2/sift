FAIL

# M5 #671 Channel webhook after #665 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#665 / PR #668，实现提交 `776051359cea7791f910511c31444e2b90f61e61`，合入提交 `ba3b759`
> 评审基线：`main` / `origin/main` `ba3b759`
> 判定基准：[#659 FAIL](2026-07-30-m5-channel-webhook-worker-659-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #665 / PR #668 的实现方摘要“Daily terminal authority + threshold-alert wakeup; migration 0050; broader P1s still open”对范围的描述基本准确，但 threshold-alert wakeup 本身没有关闭完整规定路径。migration `0050_channel_daily_limits_terminal_closure.sql` 正确把 daily batch 的 UPDATE guard 扩展到所有 `NEW.state`；新增测试也把 `sealed|delivered|cancelled × critical_window_ms|critical_total_limit|critical_per_run_limit` 九个 terminal-transition 向量逐项执行。因此 #659 P1-3 的 terminal per-column 子项可核销，migration 编号满足 **0050**。

但 `CompleteOutboxAttempt` 只覆盖 completion 创建 alert 后的 post-commit wake，`claimOutboxOperation` 只在 terminal reclaim 分支 wake。未达 attempt limit 的 lease-expiry reclaim 可以在 `applyChannelOutcomeTx` 中从 count 2 跨 threshold 3、原子创建 `forge_alert`，随后同事务创建下一 attempt 并从普通 `return &c, nil` 返回，完全不调用 `wakeOutbox`。临时 production-schema probe 以 threshold=3、unbounded retry 执行“两次 completion + 第三次 lease expiry reclaim”，alert 已创建且 attempt 4 已租出，但 wake 计数仍停在 2；`-count=3` 稳定失败。该路径正是 storage §6.6 明列的“reclaim crosses threshold before attempt limit”，故 alert wakeup 仍为 **PARTIAL / NO**。

此外生产 `Daemon.OutboxTick` 虽会运行 comment/change/merge/channel workers，但 `CommentWorker` 固定只 claim `OperationForgeComment`；全仓没有生产 worker claim `OperationForgeAlert`。因此 wake 即使发生，也没有 consumer 可把 Channel failure alert 投递到 Forge，更无法形成 alert success/failure/non-recursion 验收。#665 未修改 Report、sealer/replay、并发 authority、diagnostics control-plane 或 CLI；P1-1/2/4/5及 P1-3其余矩阵继续开放，P1-6未回退。

本轮只新增评审报告，不修改被评审实现或规格；临时 probe 已删除。

## 2. #659 P1逐项复审

### P1-1 — PARTIAL / OPEN：Report production vertical无增量

#665 的四文件 diff 不涉及 Report 或 production vertical。仍无从真实 `RecordReport` quota exhaustion 到 frozen Channel、batch/member、production sealing、outbox wake、webhook与 completion 的单条纵向验收。

### P1-2 — OPEN：exact sealer、canonical alert与response-loss replay无增量

#665 不修改 production sealer、renderer或 webhook worker。batch digest `ae3dba99…`仍只由 `internal/channelworker/webhook_test.go` 的手写 adapter payload命中；canonical alert digest `ba180536…`仍无 Go 测试命中。两名真实 member 的 exact production sealing、同 key/bytes response-loss replay与真实第三次 `rate_limited` canonical alert均未补齐。

### P1-3 — PARTIAL / OPEN：terminal逐状态逐列关闭，其余 authority矩阵仍缺

有效增量：

- migration `0050` 先 drop `0049` trigger，再以 `NEW.kind='daily_summary'`及任一 critical limit 非 NULL 为条件拒绝所有 UPDATE，不再依赖 OLD/NEW state；
- `0046` 的 INSERT guard仍保留，故 daily INSERT与任意后续 UPDATE均不能携带 critical limits；
- 新测试逐项执行三个 terminal state和三列的九个拒绝向量；失败语句保持 collecting，使每个循环项都从合法 authority出发；
- schema version、migration count和 reopen幂等升至 50，定向/race测试通过。

因此 terminal per-column 子项为 **YES**。但 `i-a/i-b/i-c`并发双 target、critical identity collision、完整 upgrade/restart authority与 §6.6 production exact sealing没有新增，P1-3总项仍为 PARTIAL。

### P1-4 — PARTIAL / OPEN：alert wake存在 reclaim旁路，failure矩阵仍不完整

- completion 路径和 terminal reclaim路径现在 post-commit wake；这两个窄路径有效；
- nonterminal reclaim跨 threshold创建 alert 后从 `internal/storage/outbox.go:299` 返回，不经过 `:279`或`:354`的 wake；
- 新测试只断言两次 completion加 terminal reclaim共三次 wake，恰好未覆盖规范要求的“reclaim crosses threshold before attempt limit”；
- 生产没有 `forge_alert` kind consumer，故 alert无法被执行，alert失败不递归也无生产证据。

并发双 worker、restart count=2、第三次 rate-limited exact alert、success清零及完整 terminal projections也未补齐。P1-4保持开放。

### P1-5 — PARTIAL / OPEN：restart diagnostics与 operator surface无增量

#665 只在既有 direct storage diagnostics测试中加入 wake计数；没有 DB reopen 后的 count/state/error/alert key/alert state验证，也没有 `ops.ps` / `ops.doctor` 对 delivery/episode/alert/generated-not-delivered 的 control-plane/CLI验收。

### P1-6 — CLOSED，未回退

本次 migration和 outbox wake逻辑未触及 resolver、sealed handle、sender或错误摘要；`secret_ref:` handle-only与安全边界保持。

## 3. migration、CI与执行证据

- `internal/storage/migrations/0050_channel_daily_limits_terminal_closure.sql`存在且编号唯一；embedded schema version和 migration row count为 50。
- `git diff 59dd9db..ba3b759 --check`：通过；#665实现提交仅修改 4 文件，62 行新增/6 行删除。
- terminal逐列、diagnostics、migration定向测试 `-count=10`：通过。
- 上述定向 storage race测试 `-count=3`：通过。
- `go test ./internal/channelworker -run TestWebhook -count=10`：通过。
- 临时 nonterminal reclaim threshold-wake probe `-count=3`：三次均按预期失败，wake count为 2；probe随后删除。
- `go vet ./...`：通过。
- `go test ./... -count=1`：仅失败于既有 `internal/controlplane.TestDoctorBaselineChecksConfiguredDependencies` fixture command `signal: killed`；该测试单独 `-count=3`通过，未发现与 #665 diff 的因果关系。
- PR #668：四平台 build、schema drift、`vet + test`均为绿色。

## 4. #671验收清单

- [x] 获取并阅读 #671全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #659 FAIL / #665逐项复审 P1-1..P1-5：**YES**
- [x] 严格核验实现方“仅关 terminal authority + alert wakeup”自述：**YES；terminal逐列关闭，alert wakeup仅 PARTIAL**
- [x] 核验新 migration为 `0050_channel_daily_limits_terminal_closure.sql`：**YES**
- [x] terminal `3 states × 3 columns` authority矩阵：**YES**
- [ ] completion/reclaim所有 canonical-alert创建事务均 post-commit wake：**NO；nonterminal threshold reclaim漏 wake**
- [ ] canonical alert存在生产 consumer：**NO**
- [x] P1-6未回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1关闭：**NO / PARTIAL**
- [ ] P1-2关闭：**NO**
- [ ] P1-3完整矩阵关闭：**NO / PARTIAL**
- [ ] P1-4关闭：**NO / PARTIAL**
- [ ] P1-5关闭：**NO / PARTIAL**
- [ ] #665可按 #659关闭标尺整体核销：**NO**
- [x] PR #668 `vet + test`已有最终绿色结论：**YES**

## 5. 最终裁决

**FAIL。** migration `0050`及九个 terminal逐列向量有效关闭 #659 P1-3 的 terminal authority残项；completion和 terminal reclaim的 post-commit wake也是有效窄增量。但规范明确要求的 nonterminal threshold-crossing reclaim仍可创建 alert而不 wake，且生产没有任何 `forge_alert` consumer。其余 P1-1/2/4/5与 P1-3 broader matrix没有实现增量。后续至少须让每个可能创建 alert的 reclaim CAS在 commit后 wake、接通并验收 `forge_alert`生产 consumer，再完成 Report vertical、exact sealing/replay/canonical digest、并发/recovery及 `ops.ps`/`ops.doctor`矩阵。P1-6继续保持关闭。
