FAIL

# M5 #589 Channel webhook after #582 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#582 / PR #586，实现提交 `bf2effc7e3c120c4fffdf3e37bdbbb5b1457bc23`，合入提交 `4adc4d86f12302edd4bdbbf4efcacbea9e51a895`
> 评审基线：`main` / `origin/main` `4adc4d8`
> 判定基准：[#577 FAIL](2026-07-30-m5-channel-webhook-worker-577-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #582 / PR #586 的实现方自述“P1-1/2/4/5 still open”属实。合入 diff 只有三个文件：新增 `0035_channel_authority_closure` 的两条 sealed-member UPDATE trigger、增加一个单成员 sealed UPDATE 负测，并把 schema version/count 从 32 更新为 35。它没有修改 Report、production sealer、Channel worker、failure episode/reclaim、diagnostics、controlplane 或 CLI。

该增量对 P1-3 有效但仍不足：`attention_batch_member_authority` 在 batch 离开 `collecting` 后不能改写九个展示/nonce字段，新增测试也证明 member `channel_id` 与 authority `nonce` 的 UPDATE 被拒绝。但 migration 没有 DELETE guard，且 authority trigger 未冻结 `updated_at_ms`；直接 SQL 仍可删除 sealed authority row，再删除其 parent member，或单独改写 authority timestamp。因此其“sealed batch and its member snapshots are immutable authority”注释并未由 schema 完整落实。更重要的是，#577 要求的 `i-a/i-b/i-c` 双 batch并发、collision reread、reopen authority及最终 exact sealing矩阵仍不存在。

P1-1、P1-2、P1-4、P1-5 在 #582 中完全没有实现增量。全仓仅 webhook adapter手写 fixture命中 `ae3dba99…`；没有测试命中 canonical alert digest `ba180536…`。新增测试虽首次在该 closure文件调用 `PrepareDueAttentionBatches`，但只是单成员后做两次 UPDATE负测，不核验 production exact bytes/digest、response-loss replay、worker completion或 canonical alert。P1-6 的 secret/error-summary边界未回退。

本轮只新增评审报告，不修改被评审实现或规格。

## 2. #577 P1 逐项复审

### P1-1 — PARTIAL / OPEN：Report production vertical仍缺

#582 不修改 Report代码或测试。`TestReportQuotaCommandRetainsFrozenChannels` 仍只是直接构造内存 command；`TestRecordBlockerReportKeepsRunningRun` 的 config没有 Channel registry；`TestRecordReportQuotaExhaustionUsesSystemEventIdentity` 仍绕过 `RecordReport` production入口。没有测试从真实 `RecordReport` quota exhaustion继续到 frozen Channel delivery、batch/member、production sealing、outbox wake、webhook及 completion。

### P1-2 — OPEN：exact sealer、canonical alert与response-loss replay仍缺

#582 不修改 sealer、worker或 failure renderer。新增 `TestSealedBatchMemberAuthorityCannotBeRetargeted` 只生成一个成员并调用 `PrepareDueAttentionBatches`，随后检查两个 UPDATE失败；它不读取或逐字节核验 sealed payload/digest，也不执行 worker。

全仓证据仍为：

- `ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e` 只在 `internal/channelworker/webhook_test.go` 的手写 adapter payload中出现；
- `ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2` 没有测试命中；
- 没有两名真实 admission/member形成 §6.6 exact batch；
- 没有远端成功、本地 response-loss 后以同 operation key和同 payload重放；
- 没有由第三次真实 `rate_limited` completion生成并核验 canonical `forge_alert`。

### P1-3 — PARTIAL / OPEN：sealed UPDATE guard有效，完整 authority matrix及DELETE封口仍缺

`0035_channel_authority_closure.sql` 对 sealed/delivered/cancelled member authority字段增加 UPDATE guard，并对 `attention_batch_member_authority` 在非 collecting状态增加 snapshot UPDATE guard。新增测试通过 production sealing后验证 member `channel_id` 与 authority `nonce` 各一个改写被拒绝；这是有效窄修复。migration也正确采用 **0035**，位于 `0033_advance_interrupt_p1_closure`、`0034_emit_interrupt_binding_identity` 之后，version/count/reopen幂等相符。

但该 schema closure仍不完整：

- 两张 member表均无 DELETE trigger；authority child可先删，随后 parent member也可删，sealed member evidence并非不可删除；
- authority snapshot UPDATE trigger没有比较 `updated_at_ms`，sealed authority row仍可被改写；
- 新测试只覆盖单成员和两个字段，没有逐列 DELETE/UPDATE负矩阵；
- 没有 `i-a/i-b/i-c` 并发形成两个稳定 batch、collision reread、关闭重开后 authority、critical identity及最终 exact sealing证据。

因此 P1-3仍只能判 PARTIAL，不能以 migration名称或单个测试判定关闭。

### P1-4 — PARTIAL / OPEN：failure/reclaim/concurrency矩阵无增量

#582 不修改 failure episode、claim/complete/reclaim代码或测试。既有 `TestChannelDiagnosticsIncludesBatchFailureProjection` 仍使用手写不完整 payload和同一个打开的 DB，只覆盖两次 completion后 attempt 3 terminal reclaim、旧 completion stale及无 attempt 4。

threshold reclaim后继续 lease、并发双 worker、第三次 rate-limited canonical alert、restart count=2后唯一 alert、success清零、alert自身失败不递归，以及完整 terminal delivery/episode/alert assertions仍缺。

### P1-5 — PARTIAL / OPEN：无 restart及operator surface验收

#582 不修改 diagnostics、controlplane或 CLI。测试搜索仍没有 `internal/controlplane/*_test.go` 或 `cmd/sift/*_test.go` 对 Channel delivery/episode/alert/generated-not-delivered投影的断言；唯一具名 Channel diagnostics测试仍直接调用 storage且不 reopen数据库。重启后的 count/state/error/alert key/alert state及 `ops.ps`/`ops.doctor` surface验收仍为 NO。

### P1-6 — CLOSED，未回退：secret/error-summary边界保持

#582 不修改 payload、resolver、sender、completion或错误摘要。payload仍只保存 `secret_ref:` handle；既有 query-secret和 sender-error负向测试通过。migration与测试没有引入 endpoint、credential、response body或原始 sender error。

## 3. migration与合入核验

- `internal/storage/migrations/0035_channel_authority_closure.sql` 存在且编号唯一；`0033`、`0034`、`0035`顺序无冲突。
- embedded schema version和 migration row count均为 35，关闭重开幂等测试通过。
- PR #586 CI 的四平台 build、schema drift、vet + test均通过。
- `git diff 4adc4d8^1..4adc4d8 --check`通过。
- migration编号正确不等于 P1-3或 P1-1..P1-5整体关闭。

## 4. 关闭条件对账

| #589 条件 | 结果 | 说明 |
|---|---|---|
| 对照 #577 FAIL / #582复审 P1-1..P1-5 | **YES** | 已逐项复审。 |
| P1-1 Report quota Channel production vertical | **NO / PARTIAL** | #582无增量，仍无 `RecordReport`→sealer→worker纵向证据。 |
| P1-2 exact sealer/alert fixtures + replay | **NO** | 单成员封口测试不是 exact/replay测试；canonical alert digest仍无命中。 |
| P1-3 authority/target matrix | **NO / PARTIAL** | sealed UPDATE guard有效；DELETE/`updated_at_ms`封口及并发/restart/exact矩阵仍缺。 |
| P1-4 reclaim/alert-failure matrices | **NO / PARTIAL** | #582无增量；硬性 recovery vectors仍缺。 |
| P1-5 restart diagnostics、ops.ps/doctor | **NO / PARTIAL** | 仍只有 direct storage projection，无 reopen/operator surface测试。 |
| P1-6不得回退 | **YES** | handle-only payload与安全错误边界保持。 |
| migration应为 `0035` | **YES** | 文件、顺序、version/count及reopen幂等正确。 |
| 严格核验实现方“仅关 P1-3”自述 | **YES** | P1-1/2/4/5确无增量；P1-3也仅获窄修复，未完整关闭。 |
| 禁止自修自审 | **YES** | #582由Cursor实现；本轮Sol只写复审报告。 |
| P1-1..P1-5全部关闭 | **NO** | P1-2是明确最小阻断，其余项亦未闭合。 |
| #582可按原关闭标尺核销 | **NO** | PR自述与实际diff均未满足 issue关闭条件。 |

## 5. 执行证据

- 使用检测到的 GitHub forge获取并阅读 `gh issue view 589`、`gh issue view 589 --comments`；回溯 #577/#582全文、comments、PR #586正文/checks、commit/file列表及完整实现diff。
- `git diff 4adc4d8^1..4adc4d8 --check`：**通过**。
- 五个 Channel/Report/migration定向 storage测试 `-count=10`：**通过**。
- `go test ./internal/channelworker -run 'TestWebhook' -count=10`：**通过**。
- 定向 race测试（sealed authority、diagnostics、webhook）：**通过**。
- 临时定向 schema probe：sealed后改写 authority `updated_at_ms`、依次 DELETE authority/member均成功，确认 migration未封住上述直接SQL路径；probe文件已删除、未纳入改动。
- `go vet ./...`：**通过**。
- `go test ./internal/storage ./internal/channelworker ./internal/config ./internal/controlplane -count=1`：**通过**。
- `go test ./... -count=1`：除已知 `TestDoctorBaselineChecksConfiguredDependencies` fixture命令 `signal: killed` 外其余包通过；全量不能记为绿。
- 全仓证据搜索：exact batch digest仍只在 adapter手写测试；canonical alert digest无测试命中；operator/CLI测试无 Channel projection断言。

## 6. Issue #589 验收清单

- [x] 获取并阅读 #589全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #577 FAIL / #582严格复审 P1-1..P1-6：**YES**
- [x] 核验 migration为 `0035_channel_authority_closure`：**YES**
- [x] 严格核验实现方“仅关 P1-3”自述：**YES；且 P1-3仍仅PARTIAL**
- [x] 确认 P1-6不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5全部关闭：**NO**
- [ ] #582可按原关闭标尺核销：**NO**

## 7. 最终裁决

**FAIL。** `0035_channel_authority_closure` 的编号和 sealed UPDATE窄修复有效，但实现方明确未处理 P1-1/2/4/5，P1-3本身也缺 DELETE/完整列封口及并发、reopen、exact矩阵。下一实现至少应先补真实两成员 production sealing的 `ae3dba99…` exact fixture与同 key/payload response-loss replay，并由真实第三次 failure路径命中 `ba180536…` canonical alert；同时补齐 `RecordReport` quota vertical、authority DELETE/逐列/并发/restart、完整 reclaim/concurrency/success/alert-failure，以及 reopen后的 `ops.ps`/`ops.doctor`投影测试。P1-6继续保持关闭。
