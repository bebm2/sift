FAIL

# M5 #571 AdvanceInterrupt after #563 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#563 / PR #568，合入提交 `d30a1906`（实现提交 `1700725e`）
> 评审基线：`main` `d30a1906`
> 前次结论：[#559 FAIL](2026-07-30-m5-advance-interrupt-559-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#559 的 5 个 P1 仍未全部关闭。** #563 补上了 populated 0020→0021 正向升级测试，并对 critical successor 的碰撞行执行了完整 identity 重读；但 repeated-fused 只是从“immutable trigger 导致事务回滚”退回到 #547 已判失败的“保留旧 member version/nonce”方案，到期 sealer 会排除该成员并取消空 batch。full effect-binding 也仍未闭合：0030 只覆盖部分 arm，且当前 `agent_blocked` writer/DB shape 与 AdvanceInterrupt validator 已直接不一致。

本轮遵守“禁止自修自审”：评审者未参与 #563 实现，只新增本报告，不修改被评审代码或规格。

## 2. #559 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / repeated-fused：**NO（回滚已消除，正确递送未恢复）**

`addBatchMemberTx` 的第二次 `INSERT OR IGNORE` 命中既有 `(batch_id,interrupt_id)` 后，不再 UPDATE immutable member，因此不再触发 0023 的 immutable trigger；这一点关闭了 #559 所述的直接回滚。

但实现明确保留旧 `interrupt_version/nonce`（`internal/storage/advance_interrupt.go:516-532`）。Interrupt CAS 已轮换为新 version/nonce，而 `PrepareAttentionBatch` 只选择 `i.version=m.interrupt_version AND i.nonce=m.nonce` 的成员（`internal/storage/channel_batch.go:44`）。因此 repeated-fused 到期时该成员必被排除；若它是唯一成员，batch 被取消且不产生摘要。该方案正是 #547 已给出并判失败的反例，不是 P1 关闭。

此外，critical collecting batch 查找仍不比较 `project_id` 或 `channel_snapshot_json`；候选命中同 identity 的不兼容 batch 时只会在 member trigger/碰撞检查处拒绝整笔 Advance，而没有规格要求的可用收敛路径。#563 没有新增 repeated-fused 或 sealing payload 测试。

### P1-2 successor provenance / replay：**YES（但缺定向测试）**

`openCriticalSuccessorTx` 继续使用旧 batch 冻结的 window/limits，并在 `INSERT OR IGNORE` 后重读验证 project、Channel ID/snapshot、完整 Forge target、kind/delivery、scope/scope ID、episode、due 与三项冻结 fuse 配置（`internal/storage/channel_batch.go:162-173`）。这关闭了 #559 的 successor identity collision 缺口。

仓库仍没有 successor collision、配置变化、crash/replay 或并发定向测试；本项依据静态路径核销，保留测试覆盖风险。

### P1-3 migration / populated 0020→0021：**NO（特别要求的正向测试 YES，完整 P1 仍 NO）**

`TestPopulated0020To0021UpgradePreservesForeignKeysAndRestarts` 确实从 embedded migrations 建立 0020 schema，写入有 FK 关联的 project/run/budget/interrupt/binding，随后经 production `Open` 升级，执行 `foreign_key_check` 并重启（`internal/storage/storage_test.go:143-203`）。issue 特别点名的 populated upgrade 正向测试由 NO 变为 **YES**。

但 #559 最终要求的是 populated FK-linked upgrade/**rollback**/restart 以及 trigger 关闭验证。新增测试只验证一列 `interrupts.run_id`、无 FK violation 和可重启；没有制造 0021 失败并验证原 0020 数据/schema_migrations 回滚，也没有断言 0021 重建后由后续 migration 恢复的 trigger 行为。因此 migration P1 不能完整核销。

### P1-4 多次升级与 low/normal summary：**NO**

P1-1 的旧 member 反例意味着 count 0/1/2 中 repeated-fused 路径仍会丢失 summary。#563 没有新增任何 `AdvanceInterrupt` 测试；downgrade 后 low/normal、summary before/after expiry、restart、sealing payload 和 repeated-fused 矩阵仍全部缺失。

### P1-5 full effect-binding：**NO**

0030 和新的 Go reference validator只检查 design、startup、failure-review attempt、report-quota 四类引用；guardrail、code-review、agent-blocked 和 merge-conflict 直接走 `default: return nil`（`internal/storage/advance_interrupt.go:291-311`），0030 也明确省略这些 arm（`0030_advance_interrupt_p1_closure.sql:1-32`）。这不满足 storage §6.4 要求的 Change/head、attempt/run/generation 等 full binding。

还有一个直接不一致：storage §6.4 的 `agent_blocked` closed arm 是 `{run_id,attempt_no,generation}`；当前 0028 trigger/writer却要求并写入额外 `report_id`，而 AdvanceInterrupt validator仍只接受四键 JSON（`internal/storage/advance_interrupt.go:188-200`）。因此当前 writer 产生的 agent-blocked binding 在 auto-reject 时必因 `len(binding) != len(expected)` 被拒绝。

普通 reason 的 options、错 Change/head、agent report/attempt、guardrail source facts等反例仍无 durable closure，也没有新增 effect-binding 测试。

## 3. migration 0030 专项结论

- 新 migration 从 0030 起：**YES**。
- 0030 无重复：**YES**；扫描结果为 0001–0030 连续、每个版本恰好一个文件。
- 发布的 0001 bytes 未漂移：SHA-256 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- populated 0020→0021 正向 FK/restart 测试：**YES**。
- populated upgrade rollback/trigger 行为：**NO**。
- full effect-binding durable references：**NO**。

## 4. 回归与执行证据

- 从检测到的 GitHub forge 获取并阅读 #571 全文、Agent 建议、关闭条件与 comments（0 条），并回溯 #559、#563 与 PR #568。
- `git diff 5dba7f0..d30a190 --check`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./internal/storage`：**通过**。
- `go test ./...`：**通过**。
- migration 扫描：0001–0030 连续，重复版本为零。

绿灯没有执行 repeated-fused、successor collision/replay、upgrade rollback/trigger 或 effect-binding 反例，不能反证上述静态可达失败路径。

## 5. Issue #571 验收清单

- [x] 获取并阅读 #571 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #571 comments：**YES（0 条）**
- [x] 对照 #559 FAIL / #563 核验 5×P1：**YES**
- [x] 严格核验 populated 0020→0021 upgrade test：**YES；正向 FK/restart 为 YES，rollback/trigger 为 NO**
- [x] 核验 migration 0030 无重复：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #559 的 5 个 P1 全部关闭：**NO（1/5 完整关闭）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 6. 最终裁决

**FAIL。** #563 不可核销 #559。下一实现至少须让 repeated-fused 在不改写 immutable 历史 member 的前提下保留当前可发送 authority；补齐 populated upgrade rollback/trigger 与多次升级 sealing 矩阵；按 storage §6.4 统一 writer、schema trigger 和 auto-reject validator 的每个 arm、组合 FK 及 options，再交由不同代理复审。
