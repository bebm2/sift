FAIL

# M5 #627 AdvanceInterrupt after #621 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#621 / PR #626，合入提交 `e160a0f`（实现提交 `59593cd`）
> 评审基线：当前 `main` `e160a0f`
> 前次结论：[#615 FAIL](2026-07-30-m5-advance-interrupt-615-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.4、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#615 的 5 个 P1 仍只有 1/5 完整关闭。** #621 使用正确的 migration 0043，并加入 repair CLI、collecting authority identity guard 与更完整的 binding trigger；但 migration 0043 在禁止 authority PK retarget 时覆盖掉了 collecting authority 的 current-open mirror 校验，repair CLI 又会被既有 append-only trigger 拒绝。实现方明确保留为 open 的 escalation/restart 矩阵确实没有实现；merge-conflict 写边界仍允许无 calibration 的 binding，且没有验证规格定义的 `conflict_digest`。

相同 canonical binding 的 identical digest 无法无损改写，工具拒绝并要求人工升级是正确的 fail-closed 行为；本轮失败不是因为该限制，而是非 identical duplicate 的已宣称 repair 路径本身不可执行。

本轮遵守“禁止自修自审”：评审者未参与 #621 实现，只新增本报告，不修改被评审代码、规格或 runbook。

## 2. #615 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / repeated-fused authority：**NO（修一处、回退一处）**

0043 新 trigger 会拒绝 collecting authority 改写 `(batch_id, interrupt_id)`，因此 #615 的 collecting-member retarget 旁路本身已封死（`0043_advance_interrupt_p1_closure.sql:5-10`）。

但 0043 先 DROP 既有 `attention_batch_member_authority_current_update`，再用同名 trigger 替换；新 trigger 只在 PK identity 改变时触发。0038/0041 原本对 collecting UPDATE 的 open Interrupt、version、nonce 和展示字段逐字节 mirror 校验不再存在。于是 direct SQL 保持 PK 不变、只把 collecting authority 的 nonce/version/headline 等改成非当前值即可通过。sealed/cancelled immutable trigger不能保护 collecting 行。#621 也没有新增 collecting authority direct-SQL、retarget、restart 或 projection replay 测试。故 current-authority 边界仍未闭合。

### P1-2 successor provenance / replay：**YES（未回退）**

#621 未改变 successor collision/replay 主路径。当前 batch identity 重读继续核对 target、scope、episode、due 与冻结 critical window/limits；旧 batch sealing 与 successor authority 未见功能回退。collision/crash/restart 专项测试风险仍在，但本项沿用 #615 的静态核销结论。

### P1-3 migration、rollback 与 duplicate-digest 升级路径：**NO**

- migration 从 **0043** 起且版本唯一：**YES**；当前 0001–0043 连续，无重复或缺号。
- duplicate migration 失败回滚：**YES，未回退**。
- identical canonical binding：**正确拒绝并要求人工升级**（`main.go:75-77`）。
- 非 identical duplicate 的仓库内 repair：**NO**。

CLI 对 binding 执行普通 `UPDATE`（`cmd/sift-advance-interrupt-repair/main.go:81-101`），但 0018 至今保留 `interrupt_command_effect_bindings_append_only_update`，任何 UPDATE 都 `RAISE(ABORT,'append-only table')`（`0018_advance_interrupt_final_closure.sql:21-23`）。本轮用含两条非 identical、同 digest row 和该发布 trigger 的 populated SQLite fixture 实跑：check 模式正确报告 duplicate；`--repair --backup` 创建备份后以 `constraint failed: append-only table (1811)` 退出，源 row 完全未变。

此外工具按 duplicate group 各开一个事务，而 runbook 声称全部 repair “inside one transaction”（`advance-interrupt-migration.md:19-24`）；后续 group 失败可留下前序 group 已提交的部分修复。CLI 无测试（`go test ./cmd/sift-advance-interrupt-repair` 报 `[no test files]`），也没有 populated repair/restore 演练。因此该升级路径不可操作。

### P1-4 multi-escalation / low-normal / restart 矩阵：**NO**

PR #626 自述 “escalation matrix still open”，diff 也没有修改任何 AdvanceInterrupt 测试：唯一测试文件改动只是 schema version 42→43。当前 `advance_interrupt_test.go` 仍只有单次 normal→high、repeated critical fuse、startup-stall max=0 与 summary helper 等离散用例；count 0/1/2、downgrade 后 low/normal summary、summary expiry 前后、各上限去向、restart 后 tick/seal、旧快照/repeated-fused 排除的闭合矩阵仍不存在。

### P1-5 full effect-binding provenance：**NO**

0043 恢复了多个 arm 的 trigger 分支，但 merge-conflict 仍未闭合：

1. `code_review` 与 `merge_conflict` 都以 `i.calibration_id IS NULL OR (...)` 放行无 calibration 的新 INSERT（`0043:31-47`）。trigger 只约束新写，不存在为 legacy row 保留旁路的理由；这仍允许没有 durable Gate source 的 binding。
2. calibrated merge-conflict 分支只检查 Change/head 和 snapshot/verdict 的 `mergeability='conflicting'`，完全没有校验 `binding_json.conflict_digest`（`0043:39-47`）。Go auto-reject validator同样不读取该字段（`advance_interrupt.go:302-314`）。这不满足 storage §8.1 的固定定义：`conflict_digest=SHA-256(canonical_json({change_id,head_sha,mergeability:"conflicting"}))`。
3. #621 没有新增 merge-conflict 无 calibration、错 conflict digest、旧 row 或各 arm direct-SQL rejection vectors。

因此消费端与 latest-schema 写边界都不能证明 full provenance。

## 3. migration 0043 与回归结论

- 新 migration 为 0043：**YES**。
- migration 0001–0043：**连续、无重复、无缺号**。
- 发布的 0001 bytes 未漂移：SHA-256 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- collecting→collecting authority PK retarget：**已拒绝**；同 PK 的伪造 current fields：**仍允许**。
- duplicate-digest check：**可执行**；非 identical repair：**被 append-only trigger 拒绝**；identical row：**正确要求人工升级**。
- multi-escalation/low-normal/restart 矩阵：**不存在**。
- latest-schema full effect-binding direct-SQL provenance：**未闭合**。
- `go test ./internal/storage/ ./internal/intake/`：**通过**。
- `go test ./...`：除 `TestDoctorBaselineChecksConfiguredDependencies` 的 agent fixture 两次 `signal: killed` 外均通过；该用例单独重跑仍失败，属于既有 doctor 时序/环境问题，未作为本次功能裁决依据。
- `go vet ./internal/storage ./internal/intake ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd ./cmd/sift-advance-interrupt-repair`：**通过**。

## 4. Issue #627 验收清单

- [x] 从检测到的 GitHub forge 获取并阅读 #627 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #627 comments：**YES（0 条）**
- [x] 对照 #615 FAIL / #621 核验 5×P1：**YES**
- [x] 核验实现方自述的 escalation/restart NO 与 identical digest 人工升级：**YES；前者仍 NO，后者为合理 fail-closed 限制**
- [x] 核验 migration 0043 与 repair CLI：**YES；版本正确，repair 不可执行**
- [x] `go test ./internal/storage/ ./internal/intake/`：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #615 的 5 个 P1 全部关闭：**NO（1/5）**
- [ ] non-identical duplicate-digest 升级/恢复工具可操作：**NO**
- [ ] escalation/restart 验收矩阵完整：**NO**
- [ ] full effect-binding provenance 闭合：**NO**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 5. 最终裁决

**FAIL。** #621 不可核销 #615。下一实现至少须：在禁止 authority PK retarget 的同时保留 collecting row 对当前 open Interrupt 的完整 mirror 校验，并补 direct-SQL/restart vectors；让 repair 工具在明确、受控、全局原子的维护流程中处理 append-only binding，同时提供 populated repair/restore 测试；补齐 escalation/low-normal/expiry/restart 矩阵；移除新写的无-calibration provenance 旁路，并按 storage §8.1 重算和验证 merge-conflict digest，再交由不同代理复审。
