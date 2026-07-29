FAIL

# M5 #583 AdvanceInterrupt after #575 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#575 / PR #580，合入提交 `8c0cc3e`（实现提交 `142bf87`）
> 评审基线：`main` `8c0cc3e`
> 前次结论：[#571 FAIL](2026-07-30-m5-advance-interrupt-571-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#571 的 5 个 P1 仍未全部关闭。** #575 引入单独的 current-authority projection，使 collecting batch 中的 repeated-fused member 在静态路径上不再因旧 version/nonce 被 sealer 排除；successor collision 重读和 populated 0020→0021 正向升级也保持通过。但实现方自述为 NO 的 dedicated upgrade rollback 与 multi-escalation sealing tests 确实仍不存在，且 0033 重建 effect-binding triggers 时撤销了 0031 已有的 reason、类型、design/report identity 等约束，full effect-binding 反而出现 schema 退化。

本轮遵守“禁止自修自审”：评审者未参与 #575 实现，只新增本报告，不修改被评审代码或规格。

## 2. #571 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / repeated-fused：**NO（静态主路径改善，契约与测试未闭合）**

0033 新增 `attention_batch_member_authority`；`addBatchMemberTx` 对 collecting batch 的 projection 更新 current version/nonce，`PrepareAttentionBatch` 改从该 projection 做 open/version/nonce 关闭检查（`internal/storage/advance_interrupt.go:543-549`、`internal/storage/channel_batch.go:51`）。因此同一 collecting batch 在 seal 前再次 fused 时，旧 member 不再必然 stale，这是实质修复。

但 active storage 规格仍规定发送冻结绑定保存在 immutable `attention_batch_members`，且 sealer 对 member 自身的 version/nonce 做匹配（`storage.md:477`）；新 mutable projection 没有规格定义。实现还会随升级改写 headline/severity/links/options，而不只是 current authority。仓库没有 repeated-fused、projection replay、seal payload 或 restart 定向测试证明该新增事实的边界。故不能把未同步规格且无要求测试的替代模型核销为 P1 完整关闭。

### P1-2 successor provenance / replay：**YES（保留覆盖风险）**

`openCriticalSuccessorTx` 仍从旧 batch 使用冻结 window/limits，并在 `INSERT OR IGNORE` 后完整重读 project、Channel snapshot、Forge target、scope、episode、due 与三项 fuse 配置。#575 未回退该路径。

仍无 successor collision、配置变化、crash/replay 或并发定向测试；本项仅依据静态路径核销。

### P1-3 migration / populated upgrade rollback 与 trigger：**NO**

`TestPopulated0020To0021UpgradePreservesForeignKeysAndRestarts` 仍只覆盖正向升级、`foreign_key_check` 和重启（`internal/storage/storage_test.go:143-203`）。仓库没有制造 migration 失败并核验原 0020 数据、schema_migrations 与 trigger 全部回滚的测试；#575 也只把 fresh schema version/count 从 30 更新到 33。

更严重的是，0033 在重建 `interrupt_binding_identity_insert` 时丢掉了 0031 的多项约束（`0033:26-64`）：

- 不再验证 `NEW.reason` 与 binding arm 一致；
- 不再验证 `design_approval` 的 task-spec identity；
- 不再验证 `report_quota_failure_review` 的 exhaustion/security-event identity；
- 不再在插入边界验证 `agent_blocked` attempt identity；
- 不再保留 failure-review new-attempt 的 failed/terminal-pair 约束；
- 除 `agent_blocked` 外，多数 arm 还失去了 0031 已有的 JSON 字段类型检查。

因此“rollback/trigger 行为”不仅没有 dedicated regression，latest schema 的 trigger closure 还发生了实质退化。

### P1-4 多次升级与 low/normal summary：**NO**

`internal/storage/advance_interrupt_test.go` 仍只有一次 normal→high 的 `TestAdvanceInterruptEscalatesOnceAndRotatesNonce`；没有 count 0/1/2、多次 critical fuse、downgrade 后 low/normal、summary before/after expiry、restart、repeated-fused sealing payload 矩阵。#575 的实现提交没有新增任何测试文件或测试函数。issue 特别要求严格核验的 dedicated multi-escalation tests 结论为 **NO**。

### P1-5 full effect-binding：**NO**

Go validator补了 `code_review`/`merge_conflict` 的当前 Run change/head、agent/startup attempt，以及 failure gate-recheck references；这关闭了 #571 点名的 `agent_blocked` writer/validator shape 直接不一致。

但 full binding 仍未闭合：

- `guardrail_violation` 无任何 durable provenance 查询，直接 `exists=1`（`internal/storage/advance_interrupt.go:300-303`）；
- code review 的 `review_policy_snapshot_digest` 与 merge conflict 的 `conflict_digest` 只检查非空，不绑定 source fact；
- 0033 的 schema trigger 退化如 P1-3 所列；
- 0033 还删除 `binding_digest` UNIQUE index（`0033:2-4`），直接偏离 `storage.md:483` 的 UNIQUE 契约及重复 binding 必须回滚的固定要求；
- 没有每 arm required/null、错组合 FK、错 options、corrupt/replay/rollback 定向测试。

自动拒绝时的 Go 重读不能替代规格要求的“写端口及 CHECK 共同保证”，也不能修复已接受的错误 durable row。

## 3. migration 0033 专项结论

- 新 migration 从 0033 起：**YES**。
- 0033 版本无重复：**YES**；0001–0033 连续、33 个版本各一个文件。
- 发布的 0001 bytes 未漂移：SHA-256 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- populated 0020→0021 正向 FK/restart：**YES，未回退**。
- populated upgrade rollback/trigger dedicated test：**NO**。
- dedicated multi-escalation/repeated-fused sealing test：**NO**。
- latest-schema effect-binding trigger 完整性：**NO，0033 相对 0031 退化**。

## 4. 回归与执行证据

- 从检测到的 GitHub forge 获取并阅读 #583 全文、Agent 建议、关闭条件与 comments（0 条），并回溯 #571、#575 与实现提交。
- `git diff 20195fe..8c0cc3e --check`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./internal/storage`：**通过**。
- `go test ./...`：**通过**。
- migration 扫描：0001–0033 连续，重复版本为零。

绿灯没有执行 issue 点名的 rollback/multi-escalation 矩阵，也没有覆盖 0033 撤销的 binding rejection vectors，不能反证上述 P1。

## 5. Issue #583 验收清单

- [x] 获取并阅读 #583 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #583 comments：**YES（0 条）**
- [x] 对照 #571 FAIL / #575 核验 5×P1：**YES**
- [x] 严格核验 dedicated rollback/multi-escalation tests：**YES；结果均 NO**
- [x] 核验 migration 0033 无重复：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #571 的 5 个 P1 全部关闭：**NO（1/5 完整关闭）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 6. 最终裁决

**FAIL。** #575 不可核销 #571。下一实现至少须补 dedicated populated-upgrade failure rollback/trigger test 和 count 0/1/2 multi-escalation/repeated-fused sealing payload 矩阵；将 authority projection 写入 active storage contract（或改回符合现有 contract 的收敛方案）；恢复并补齐 0033 撤销的 binding reason/type/组合 FK约束，保留 binding digest uniqueness，并为 guardrail/policy/conflict provenance 提供 durable closure，再交由不同代理复审。
