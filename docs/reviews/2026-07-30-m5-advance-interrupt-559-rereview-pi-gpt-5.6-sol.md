FAIL

# M5 #559 AdvanceInterrupt after #551 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#551 / PR #556，合入提交 `10aedda0`（实现提交 `006ce61`）
> 评审基线：`main` `10aedda0`
> 前次结论：[#547 FAIL](2026-07-30-m5-advance-interrupt-547-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#547 的 5 个 P1 仍未全部关闭，且 issue 特别要求的 populated 0020→0021 upgrade 测试仍为 NO。** #551 恢复了 0021 丢失的两个 0018 trigger，给 critical batch 增加了冻结 window/limit 列，并把 Go effect-binding shape 对齐规格；但 repeated-fused 的修复会被既有 member immutable trigger 直接拒绝，合法升级事务回滚。successor 仍缺碰撞重读，effect-binding 仍缺规格要求的组合 FK/options 关闭验证。

本轮遵守“禁止自修自审”：评审者未参与 #551 实现，只新增本报告，不修改被评审代码或规格。

## 2. #547 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / repeated-fused：**NO**

`addBatchMemberTx` 在 `INSERT OR IGNORE` 命中既有 member 后执行 `UPDATE attention_batch_members SET interrupt_version=?,nonce=?,...`（`internal/storage/advance_interrupt.go:475-489`）。但 0023 的 `attention_batch_member_identity_immutable` 明确拒绝 `interrupt_version`、`nonce`、headline、severity、links 或 options 的任何 UPDATE（`internal/storage/migrations/0023_channel_target_matrix.sql:21-30`）。

因此第二次合法 fused 升级不是“保留 current member nonce/version”，而是在 Interrupt CAS 后触发 `batch member authority is immutable`，整笔事务回滚。该直接反例也说明新增代码没有被 repeated-fused 集成测试执行。

此外，critical collecting batch 的查找仍只以 scope/scope ID、Channel ID 和 Forge target 查询，不含 project 或 `channel_snapshot_json`；不匹配会由 member trigger 拒绝，而不是形成可用的正确 batch。

### P1-2 successor provenance / replay：**NO（部分改进）**

0027 给 `attention_batches` 增加并回填 `critical_window_ms`、`critical_total_limit`、`critical_per_run_limit`；`openCriticalSuccessorTx` 也改为使用该 batch 冻结值。这关闭了前次“从全部历史 evidence/动态 MIN limit 推导”的主要来源错误。

但 successor 仍使用裸 `INSERT OR IGNORE` 后直接返回（`internal/storage/channel_batch.go:159-163`），不重读并逐字节验证碰撞行的 project、Channel snapshot、Forge target、scope、episode、due 与冻结 limits。也没有新增 successor、配置变化、crash/replay 或并发测试。故 provenance/replay P1 不能完整核销。

### P1-3 migration / populated 0020→0021：**NO（schema 修复 YES，验收测试 NO）**

- 0027 恢复 `interrupts_nonce_issued_required_insert` 与 `interrupts_startup_stall_max_reject_insert`：**YES**。
- 0021 的 FK pragma 仍在 migration transaction 外切换，完成后执行 `foreign_key_check`：**YES**。
- populated、带 FK 关联数据的 0020→0021 upgrade regression test：**NO**。#551 唯一测试修改只是把 fresh migration version/count 从 24 更新到 27（`internal/storage/storage_test.go`）；仓库没有构造 0020 populated DB、执行 0021、验证数据/FK/trigger、失败回滚与重启的测试。

这正是 #559 关闭条件要求严格核验且实现方自述仍为 NO 的项目，因此本项不能通过。

### P1-4 多次升级与 low/normal summary：**NO**

repeated-fused 会被 0023 immutable trigger 回滚，故 count 0/1/2 多次升级矩阵仍未闭合。#551 没有新增 AdvanceInterrupt 测试，也没有 downgrade 后 low/normal、summary before/after expiry、restart 或 sealing payload 覆盖。

### P1-5 full effect-binding：**NO（closed-union shape 改进）**

Go writer/auto-reject validator 的字段集合现已对齐 storage §6.4：guardrail 含 `head_sha`，code review 含 policy digest 且无 run ID，agent blocked 无 report ID，merge conflict 无 run ID。

但 0027 替换 0025 triggers 后只检查 tag/字段数量和两个 failure-review option 序列；没有检查规格要求的字段类型/非空/正整数、attempt/run/generation 组合 FK、failed status、Change/head、report exhaustion/security event 等。Go 的 `validateEffectBinding` 同样只验证 shape/value，不验证这些 durable 组合 FK，也不验证普通 reason 的 options。仓库没有新增每 arm required/null、错 FK/options、corrupt/replay/rollback 测试。故“由写端口及 CHECK 共同保证”的 full effect-binding 契约仍未闭合。

## 3. migration 0027 专项结论

- 新 migration 从 0027 起：**YES**。
- 0027 无重复：**YES**；扫描结果为 0001–0027 连续、每个版本恰好一个文件。
- 发布的 0001 bytes 未漂移：SHA-256 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- 恢复两个 forward triggers：**YES**。
- populated 0020→0021 upgrade 回归：**NO**。

## 4. 回归与执行证据

- 从检测到的 GitHub forge 获取并阅读 #559 全文、Agent 建议、关闭条件与 comments（无评论），并回溯 #547、#551 与 PR #556。
- `git diff f1a4bb0..10aedda0 --check`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./internal/storage`：**通过**。
- `go test ./...`：**通过**。
- migration 扫描：0001–0027 连续，重复版本为零。

绿灯没有覆盖 populated upgrade、repeated-fused、successor collision/replay 或 effect-binding 反例；不能反证上述静态可达失败路径。

## 5. Issue #559 验收清单

- [x] 获取并阅读 #559 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #559 comments：**YES（无评论）**
- [x] 对照 #547 FAIL / #551 核验 5×P1：**YES**
- [x] 严格核验 populated 0020→0021 upgrade test：**YES；结果 NO**
- [x] 核验 migration 0027 无重复：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #547 的 5 个 P1 全部关闭：**NO**
- [ ] populated 0020→0021 upgrade test：**NO**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 6. 最终裁决

**FAIL。** #551 不可核销 #547。下一实现至少须以不违反 member immutable 契约的方式处理 repeated-fused current nonce/version；补 populated FK-linked 0020→0021 upgrade/rollback/restart 测试；对 successor `INSERT OR IGNORE` 做完整碰撞重读；并以组合 FK/options 反例测试闭合 effect-binding，再交由不同代理复审。
