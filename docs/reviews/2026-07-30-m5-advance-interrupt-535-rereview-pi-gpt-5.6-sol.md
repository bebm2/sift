FAIL

# M5 #535 AdvanceInterrupt after #527 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#527 / PR #532，合入提交 `61adcf4`（实现提交 `1bb18dd`）
> 评审基线：`main` `61adcf4`
> 前次结论：[#523 FAIL](2026-07-30-m5-advance-interrupt-523-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.1–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#523 的 5 个 P1 仍无一完整关闭。** #527 恢复了已发布 `0001` 的原始 bytes，新增 0021 使 fresh schema 的 charge 列 nullable，并修正了 fused admission 重读 scope、batch member 碰撞检查及 renderer 的 brief/分隔符；但实现方自述为 NO 的 critical episode successor 与 full effect-binding 确实仍为 **NO**。更严重的是 0021 的表重建在 migration 事务内执行 `PRAGMA foreign_keys=OFF`：SQLite 在事务内不会切换该设置，已有引用 Interrupt 的生产数据时 `DROP TABLE interrupts` 会因外键失败，故这不是可用的历史升级路径。

本轮遵守“禁止自修自审”：评审者未参与 #527 实现，只新增本报告，不修改被评审代码或规格。

## 2. #523 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / batch：**NO（部分改进）**

- renderer 现在包含 brief，并以规格分隔符 `；` 连接；新增 member 重读能拒绝 version/nonce/Channel snapshot 碰撞。
- critical episode 仍没有运行时实现。0021 只增加未被任何 Go/SQL 路径读写的 `episode_admission_id`；`attention_batches_identity` 仍不含 episode。
- `PrepareDueAttentionBatches` 仍把所有到期 collecting batch直接 seal/cancel（`internal/storage/channel_batch.go:12-109`），没有窗口重裁决、successor、成员转移或恢复重放状态机。
- existing collecting batch 查询仍只按 scope/Channel/target 取任意一批（`internal/storage/advance_interrupt.go:313-317`），没有验证 episode identity；batch insert 也没有持久化新增列。
- 没有新增 exact sealing/payload digest/successor/replay 集成测试。

### P1-2 critical fuse / admission：**NO（部分改进）**

- 已有 `critical_fused` admission 现在能经既有 member 重读 scope，关闭了前次的“空 scope”错误。
- **critical episode successor 仍为 NO。** due 时没有重新统计窗口；`episode_admission_id` 为空且不参与 identity；旧 episode 到期只会 seal，不会在仍饱和时创建 successor。
- repeated fused escalation 仍不能收敛：重读 scope 后会尝试把新 version/nonce 的同一 Interrupt再次加入同一 collecting batch；`INSERT OR IGNORE` 命中旧 member，新增 identity 检查随即返回 collision，使整笔 Advance 回滚。
- 初发 critical attention entry、quota CAS 竞争重读等 #523 遗留未改，也没有 admitted/fused/replay/successor 测试。

### P1-3 migration / config wiring / schema：**NO（0001 已恢复，0021 forward migration 阻断）**

- `0001_initial_schema.sql` SHA-256 已恢复为发布值 `9696d3e1…`，与 #515 前相同；0001 checksum drift **已关闭**。
- migration 编号为唯一且连续的 0001–0021，0021 编号正确，无重复。
- 0021 **不能安全升级有数据的历史库**。`applyOne` 先开启事务，再执行 migration body（`internal/storage/migrate.go:164-187`）；此时 `PRAGMA foreign_keys=OFF` 无效。0021 随后 `DROP TABLE interrupts`（`:30`），存在 `attention_admissions`、bindings、deliveries 等引用行时会触发 FK failure。SQLite 最小复现已得到 `FOREIGN KEY constraint failed`。现有测试只对空 fresh DB 连跑全部 migration，未覆盖 populated 0020→0021。
- 表重建还删除了 0004 的 `interrupts_close_write_once` trigger，却未重建；closed Interrupt 的关闭事实写一次约束发生回归。
- `episode_admission_id` 没有 NOT NULL/conditional CHECK、FK、identity index或数据回填，无法表达规格中的 durable critical episode。
- production startup_stall reason config 与 0013 历史冻结值问题未改。

### P1-4 多次升级与 low/normal summary：**NO**

#527 没有修改升级 severity/summary 状态机，也没有增加 count 0/1/2、downgrade 后 low/normal、restart 或 episode 测试。上述 repeated fused escalation 仍以 member identity collision 回滚，critical due 也不重裁决。因此多次升级合法收敛仍有直接反例。

### P1-5 canonical auto-reject variants / full effect-binding：**NO**

#527 未修改 `closeExpiredInterrupt`、binding schema或相关测试。运行时仍只解析 `arm`、`run_id` 与 report quota 三字段（`internal/storage/advance_interrupt.go:135-171`），不验证 `binding_schema_version`、`binding_digest == SHA-256(canonical_json)`、未知字段、各 arm 完整 required/null shape 或完整 effect identity。0018 的 legacy binding 改写也仍保留。因此实现方自述的 **full effect-binding NO** 核验属实。

## 3. 0001 / 0021 专项结论

- 已发布 0001 原始 bytes 恢复：**YES**。
- 0021+ forward migration 编号：**YES**（0021，0001–0021 连续且无重复）。
- 0021 nullable charge schema 目标：**fresh/空库 YES；真实有引用数据升级 NO**。
- 历史升级验收：**NO**；事务内 FK pragma 导致 parent-table rebuild失败，且 close-write-once trigger 丢失。

因此不能把“0001 恢复 + 新增 0021”解释为 migration P1 已关闭。

## 4. 回归与执行证据

- 从检测到的 GitHub forge 获取并阅读 #535 全文、Agent 建议、关闭条件与 comments（无评论），并读取 #523、#527、PR #532 与实现 diff。
- `git diff 61adcf4^..61adcf4 --check`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./internal/storage`：**通过**。
- `go test ./...`：**通过**。
- migration 扫描：0001–0021 连续，重复版本为零。
- 0001 SHA-256：当前及 #515 前均为 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- SQLite 外键最小复现：事务内 `PRAGMA foreign_keys=OFF` 后删除被引用 parent table返回 `FOREIGN KEY constraint failed`，与 0021/applyOne 的事务结构一致。

绿灯只证明 fresh/空库 migration 与既有窄测试；不覆盖 populated 0020→0021、critical successor、重复 fused escalation或 closed effect-binding 反例。

## 5. Issue #535 验收清单

- [x] 获取并阅读 #535 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #535 comments：**YES（无评论）**
- [x] 对照 #523 FAIL / #527 逐项核验 5×P1：**YES**
- [x] 核验 0001 是否恢复：**YES；实现结果 YES**
- [x] 核验 0021 forward migration：**YES；编号 YES，真实升级正确性 NO**
- [x] 严格核验 critical episode successor：**YES；实现结果 NO**
- [x] 严格核验 full effect-binding：**YES；实现结果 NO**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #523 的 5 个 P1 全部关闭：**NO（0/5 完整关闭）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO（5）**

## 6. 最终裁决

**FAIL。** #527 不可核销 #523 的五项 P1。下一实现至少须用可在 populated 历史库执行且保留全部 index/trigger/FK 约束的 forward migration迁移 nullable charge；实现持久 episode identity、到期事务重裁决与 successor；闭合 repeated fused escalation；按 exact fixtures 完成 batch sealing/replay与 closed effect-binding，并由不同代理复审。
