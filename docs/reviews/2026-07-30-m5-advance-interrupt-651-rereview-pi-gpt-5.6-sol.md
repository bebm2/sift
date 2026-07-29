FAIL

# M5 #651 AdvanceInterrupt after #645 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#645 / PR #650，合入提交 `fa5ec91`（实现提交 `e25b703`）
> 评审基线：当前 `main` / 本 worktree `fa5ec91`
> 前次结论：[#639 FAIL](2026-07-30-m5-advance-interrupt-639-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.4、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#639 的 5 个 P1 当前为 3/5；`cmd/siftd` 阻断回归已关闭，但 repair restore/rollback 演练与 escalation/restart 验收矩阵仍为 NO。**

#645 将生产 `mergeability_unknown` 从伪造的 `merge_conflict` 改为 `failure_review_attempt/gate_recheck`，携带 attempt/generation/change/head，并以 migration 0047 将 unknown snapshot/verdict 与 binding 的 change/head 绑定；该语义缺口已关闭。原 collecting authority 与 successor provenance 未回退。规定的四包门禁及全仓测试均通过。

但是 #645 没有修改 repair 工具或其测试，也没有修改 `AdvanceInterrupt` escalation matrix。repair 测试仍只证明 repair 后 digest 分离及 append-only trigger 恢复，未打开/验证/恢复 backup，未注入多 group 中途失败证明源库零部分改写。escalation matrix 仍只有一条 downgraded normal→high→hold 路径，缺少 #639 列出的 low/normal summary expiry、各 on-expire/on-max、max=0、restart/旧 tick/seal/repeated-fused 排除矩阵。因此 AdvanceInterrupt / wave1 I4 仍不可核销。

本轮遵守“禁止自修自审”：评审者未参与 #645 实现，只新增本报告，不修改被评审代码、规格或 runbook。

## 2. #639 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / repeated-fused authority：**YES（未回退）**

#645 未修改 0045 的 collecting current-open mirror、PK 不可 retarget 或 sealed/cancelled authority。相关 storage 与全仓测试通过，沿用 #639 的核销结论。

### P1-2 successor provenance / replay：**YES（未回退）**

#645 未修改 critical successor collision/replay 主路径。旧 batch 冻结 authority、successor identity 与 replay 行为未见回退，沿用 #639 的核销结论。

### P1-3 migration、rollback 与 duplicate-digest repair：**NO**

- migration 0047：**YES**；当前 0001–0047 连续、无重复、无缺号。
- 0001 bytes 未漂移：SHA-256 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- repair 实现与测试：**无变化**。`cmd/sift-advance-interrupt-repair/main_test.go` 仍只检查 distinct digest 和 append-only trigger 恢复。
- populated backup restore：**NO**。测试虽传 `--backup`，从未打开或校验 backup，也没有执行 restore 后重开/审计。
- 失败全局回滚演练：**NO**。没有第二 duplicate group，也没有注入第二 group、trigger 重建或 commit 失败来证明事务零部分改写。

另有非阻断 runbook 漂移：示例 backup 名已更新为 `pre-0047`，但升级后验证仍写最新 schema 应为 `45`（`docs/runbooks/advance-interrupt-migration.md`），实际为 47。

### P1-4 multi-escalation / low-normal / restart 矩阵：**NO**

#645 未修改 `internal/storage/advance_interrupt_matrix_test.go`。它仍只覆盖一次创建后两次升级（normal/high）及第三次 `hold/max_escalations`，且只断言每步 severity 与最终 held state。仍缺：

- count=0 基线及每步 count/version/nonce/expiry/delivery/next-dispatch；
- low/normal 的 daily summary membership、冻结 Channel snapshot 与 operation；
- summary 早于/等于/晚于新 expiry 的 batch / `batch_after_expiry` 分支；
- `on_expire=hold|auto_reject`、`on_max=hold|auto_reject` 的允许/禁止 reason 矩阵；
- max=0、首次/末次升级；
- DB reopen 后 supervisor tick、旧 tick/restart snapshot 排除、batch seal；
- repeated-fused 在 restart/close 后不得泄入 sealed payload。

### P1-5 full effect-binding provenance：**YES（测试粒度有注记）**

生产语义已修复：

1. `internal/gate/interrupt.go` 将 `mergeability_unknown` 构造成 `failure_review_attempt/gate_recheck`，不再构造 conflicting digest；attempt/generation/change/head 均进入冻结 command/binding。
2. migration `0047_mergeability_unknown_provenance.sql` 仅在 calibration 对应 snapshot 与 verdict 都为 `unknown` 时启用该 arm 的附加校验，并要求 binding `change_id/head_sha` 与该 snapshot 一致。
3. 0045 既有 provenance trigger 继续要求 binding 的 run/attempt/generation 对应当前 Interrupt 与 durable attempt；closed-union/canonical/digest triggers 未回退。
4. `TestMergeabilityUnknownUsesFailureReviewSuccessor` 覆盖了生产 command 的 reason、variant、retry kind 与 change/head；Gate 与 storage 门禁通过。
5. `cmd/siftd/main_test.go` 不再用无 calibration、伪 digest 的 merge-conflict fixture，确定性 `invalid interrupt binding identity` 回归消失。

注记：#645 没有增加一次完整 `EvaluateRecordAndEmitInterrupt` 的 unknown 集成正例，也没有补 #639 要求的 latest-schema merge-conflict/unknown direct-SQL 错 digest、错 snapshot/verdict 反例。静态 trigger/调用链足以核销已定位的生产语义缺口，但这些验收向量仍应补齐，避免后续 migration 回退。

## 3. migration 与回归结果

- 新 migration：**0047，正确且唯一**；0046 来自已合入的并行 Channel 修复，未发生编号冲突。
- migration 文件：**47 个，0001–0047 连续**。
- `go test ./internal/storage/ ./internal/intake/ ./cmd/siftd/ ./cmd/sift-advance-interrupt-repair/`：**通过**。
- `go test ./internal/gate/ -count=3`：**通过**。
- `go test ./...`：**通过**。
- `go vet ./internal/storage ./internal/intake ./internal/gate ./cmd/sift-advance-interrupt-repair ./cmd/siftd`：**通过**。
- `cmd/siftd` 的 `invalid interrupt binding identity (1811)`：**已关闭**。

## 4. Issue #651 验收清单

- [x] 从检测到的 GitHub forge 获取并阅读 #651 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #651 comments：**YES（0 条）**
- [x] 对照 #639 FAIL / #645 核验 5×P1 与 siftd：**YES**
- [x] migration 从 0047 起且连续唯一：**YES**
- [x] 规定的四包测试：**YES**
- [x] `go test ./...`：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #639 的 5 个 P1 全部关闭：**NO（3/5）**
- [ ] populated repair/restore/rollback 演练完整：**NO**
- [ ] escalation/restart 验收矩阵完整：**NO**
- [x] `mergeability_unknown` full effect-binding 生产语义缺口关闭：**YES**
- [x] `cmd/siftd` 阻断回归关闭：**YES**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 5. 最终裁决

**FAIL。** #645 关闭了 provenance 与 siftd 回归，但不能核销 #639。下一实现至少须：补 populated backup 的打开、内容校验、实际 restore/reopen，以及多 duplicate group 的事务失败注入和零部分改写证明；按 interrupt/storage 规格补齐 low/normal、summary expiry 边界、各 on-expire/on-max、max=0、restart/旧 tick/seal/repeated-fused 排除矩阵。建议同时补 unknown 的生产五件事集成正例及 latest-schema provenance 反例，并把 runbook 的最新 schema 验证值从 45 修正为 47，再交由不同代理复审。
