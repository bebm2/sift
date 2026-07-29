FAIL

# M5 #600 Channel webhook after #594 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#594 / PR #597，实现提交 `600fed6573a05f16c04856cf30a14a18832e59ed`，合入提交 `0c71b30e734ca3525a3e26ffb6fa1f9eafd131f8`
> 评审基线：`main` / `origin/main` `0c71b30`
> 判定基准：[#589 FAIL](2026-07-30-m5-channel-webhook-worker-589-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #594 / PR #597 的实现方自述准确：本次只关闭了 #589 P1-3 指出的 sealed authority DELETE 与 `updated_at_ms` 直接 SQL 路径，P1-1、P1-2、P1-4、P1-5 仍未处理。合入 diff 仅修改三个 storage 文件：新增 migration `0037_channel_authority_delete_closure.sql`、为既有单成员 sealed 测试增加 timestamp UPDATE 和两条 DELETE 负测，并将 schema version/count 从 36 更新到 37。

`0037` 的窄修复有效：非 collecting authority row 的 DELETE 与 `updated_at_ms` UPDATE 被 trigger 拒绝；sealed/delivered/cancelled member row 的 DELETE 被拒绝，因而不能再先删 authority child 后删 parent member。新增测试通过 production sealing 命中 sealed 状态并证明三条路径失败。migration 编号唯一，位于 `0036_advance_interrupt_p1_closure` 之后，version/count/reopen 幂等均正确。

但这不等于 #589 的 P1-3 authority/target **完整矩阵**关闭。测试仍只有单成员 sealed 状态和三个新增语句，没有 `i-a/i-b/i-c` 并发形成两个稳定 batch、collision reread、逐列/各终态负矩阵、关闭重开后 authority/critical identity及最终 exact sealing。其余四个 P1 在 #594 diff 中没有实现或测试增量；P1-6 的 secret/error-summary 边界未回退。

本轮只新增评审报告，不修改被评审实现或规格。

## 2. #589 P1 逐项复审

### P1-1 — PARTIAL / OPEN：Report production vertical 无增量

#594 不修改 Report、production sealer、worker或其测试。`TestReportQuotaCommandRetainsFrozenChannels` 仍直接构造内存 command；既有 Report 测试仍没有从真实 `RecordReport` quota exhaustion继续到 frozen Channel、batch/member、production sealing、outbox wake、webhook及 completion。要求的纵向证据仍不存在。

### P1-2 — OPEN：exact sealer、canonical alert与 response-loss replay 无增量

#594 不修改 `channel_batch.go`、failure renderer或 Channel worker。全仓仍只有 `internal/channelworker/webhook_test.go` 的手写 adapter fixture 命中 batch digest `ae3dba99…`；canonical alert digest `ba180536…` 仍无测试命中。没有两名真实 admission/member 生成 §6.6 exact bytes/digest，没有远端成功、本地 response-loss 后以同 operation key和同 payload重放，也没有从真实第三次 `rate_limited` completion 生成并核验 canonical `forge_alert`。

### P1-3 — PARTIAL / OPEN：DELETE/timestamp 路径已关，完整 authority matrix 仍缺

`0037_channel_authority_delete_closure.sql` 补充三条有效 trigger：

- sealed/delivered/cancelled batch 的 `attention_batch_members` 不可 DELETE；
- 非 collecting batch 的 `attention_batch_member_authority` 不可 DELETE；
- 非 collecting authority snapshot 的 `updated_at_ms` 不可变化。

新增 sealed production 测试覆盖上述三条直接 SQL，且与 `0035` 的 snapshot字段 UPDATE guard共同封住 #589 实测的先删 child/再删 parent及 timestamp 突变路径。因此 #594 声称的 **P1-3 delete/`updated_at_ms` 子路径已关闭**。

原 P1-3 标尺仍未完整满足：没有三 interrupt 并发/collision 双 batch、不同稳定 target 分流、逐列且覆盖 delivered/cancelled 的负矩阵、reopen 后 authority/critical identity，以及与 §6.6 exact payload联结的 sealing 证据。新增测试也没有关闭重开 DB 后重验 trigger/authority内容。故 P1-3 总项保持 PARTIAL，不能记 CLOSED。

### P1-4 — PARTIAL / OPEN：failure/reclaim/concurrency 矩阵无增量

#594 不修改 failure episode、claim/complete/reclaim或 alert 代码。既有 `TestChannelDiagnosticsIncludesBatchFailureProjection` 仍是手写不完整 payload和单 DB handle，只覆盖两次 completion后 attempt 3 terminal reclaim、旧 completion stale及无 attempt 4。

threshold reclaim 后继续 lease、并发双 worker、第三次 rate-limited canonical alert、restart count=2 后唯一 alert、success 清零、alert自身失败不递归，以及完整 terminal delivery/episode/alert assertions仍缺。

### P1-5 — PARTIAL / OPEN：restart及 operator surface 无增量

#594 不修改 diagnostics、controlplane或 CLI。仍没有重启后 count/state/error/alert key/alert state的断言，也没有 `ops.ps` / `ops.doctor` 对 Channel delivery/episode/alert/generated-not-delivered 投影的 RPC/CLI 验收。direct storage projection测试不能替代 operator surface。

### P1-6 — CLOSED，未回退：secret/error-summary 边界保持

#594 只增加 schema trigger和负测，不触及 payload、resolver、sender、completion或错误摘要。payload仍保存 `secret_ref:` handle，未引入 endpoint、credential、response body或原始 sender error。

## 3. migration与合入核验

- `internal/storage/migrations/0037_channel_authority_delete_closure.sql` 存在且编号唯一；顺序为 `0035_channel_authority_closure`、`0036_advance_interrupt_p1_closure`、`0037_channel_authority_delete_closure`。
- embedded schema version和 migration row count均为 37；关闭重开幂等测试通过。
- #594 未回退 `0035`、`emitReportInterruptHooks` 或 `BatchAtMS`。
- PR #597 的四平台 build、schema drift、vet + test checks均通过。
- `git diff 0c71b30^1..0c71b30 --check`通过。
- migration 0037 正确且 delete路径关闭，不代表 P1-1..P1-5整体关闭。

## 4. 关闭条件对账

| #600 条件 | 结果 | 说明 |
|---|---|---|
| 对照 #589 FAIL / #594复审 P1-1..P1-5 | **YES** | 已逐项复审。 |
| P1-1 Report quota Channel production vertical | **NO / PARTIAL** | #594无增量，仍无 `RecordReport`→sealer→worker纵向证据。 |
| P1-2 exact sealer/alert fixtures + replay | **NO** | canonical alert digest及 response-loss replay仍缺。 |
| P1-3 authority/target matrix | **NO / PARTIAL** | delete/`updated_at_ms`路径已关；并发、collision、restart、逐状态及 exact矩阵仍缺。 |
| P1-4 reclaim/alert-failure matrices | **NO / PARTIAL** | #594无增量；硬性 recovery vectors仍缺。 |
| P1-5 restart diagnostics、ops.ps/doctor | **NO / PARTIAL** | 仍无 reopen及 operator surface验收。 |
| P1-6不得回退 | **YES** | handle-only payload与安全错误边界保持。 |
| migration为 `0037` | **YES** | 文件、顺序、version/count及reopen幂等正确。 |
| 严格核验实现方“仅关 P1-3 delete路径” | **YES** | 自述与 diff相符，该子路径确已关闭。 |
| 禁止自修自审 | **YES** | #594由Cursor实现；本轮Sol只写复审报告。 |
| P1-1..P1-5全部关闭 | **NO** | 四项无增量，P1-3总项仍PARTIAL。 |
| #594可按 #589 原关闭标尺核销全部P1 | **NO** | 只能核销 P1-3 的 delete/timestamp 子路径。 |

## 5. 执行证据

- 使用检测到的 GitHub forge获取并阅读 `gh issue view 600`、`gh issue view 600 --comments`；回溯 #589/#594全文、comments、PR #597正文/checks、commit/file列表及完整实现diff。
- `git diff 0c71b30^1..0c71b30 --check`：**通过**。
- migration、sealed authority、diagnostics与 Report定向 storage测试 `-count=10`：**通过**。
- `go test ./internal/channelworker -run TestWebhook -count=10`：**通过**。
- sealed authority、diagnostics及 webhook定向 race测试：**通过**。
- `go vet ./...`：**通过**。
- `go test ./internal/storage ./internal/channelworker ./internal/config ./internal/controlplane -count=1`：**通过**。
- `go test ./... -count=1`：**通过**。
- 全仓证据搜索：exact batch digest仍只在 adapter手写测试；canonical alert digest无测试命中；controlplane/CLI测试无 Channel projection断言。

## 6. Issue #600 验收清单

- [x] 获取并阅读 #600全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #589 FAIL / #594严格复审 P1-1..P1-6：**YES**
- [x] 核验 migration为 `0037_channel_authority_delete_closure`：**YES**
- [x] 严格核验实现方“仅关 P1-3 delete路径”自述：**YES；该子路径已关闭**
- [x] 确认 `0035` / `emitReportInterruptHooks` / `BatchAtMS` 与 P1-6不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5全部关闭：**NO**
- [ ] #594可按 #589 原关闭标尺核销全部P1：**NO**

## 7. 最终裁决

**FAIL。** migration `0037` 编号正确，且 sealed authority DELETE 与 `updated_at_ms` 直接 SQL路径已被有效封住；这足以核销 #594 明示的窄目标，但不足以关闭 #589 的完整 P1-3，更不影响未处理的 P1-1/2/4/5。后续仍需补 `RecordReport` production vertical、两成员 exact sealing与 response-loss replay、canonical alert、authority并发/collision/restart矩阵、完整 failure/reclaim/concurrency/success/alert-failure vectors，以及 reopen后的 `ops.ps`/`ops.doctor`验收。P1-6继续保持关闭。
