FAIL

# M5 #663 AdvanceInterrupt after #657 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#657 / PR #662，合入提交 `7f3bd9c`（实现提交 `d94e7d2`）
> 评审基线：当前 `main` / 本 worktree `7f3bd9c`
> 前次结论：[#651 FAIL](2026-07-30-m5-advance-interrupt-651-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#651 的剩余两项中 repair restore/rollback 已关闭，但 escalation/restart 验收矩阵仍未关齐；#639 的 5 个 P1 当前为 4/5。**

#657 增加了 populated backup 的打开、integrity check、实际覆盖恢复和内容复核，也用两个 duplicate groups 及第二组失败注入证明 repair 事务零部分改写、append-only trigger 恢复；该项可核销。runbook 已同步到当前 schema 49。规定四包、定向重复测试、vet 与全仓测试最终均通过。

但是新增 escalation 测试只覆盖四个 outcome、`max=0`、一次 reopen/旧命令 CAS 和 strong priority。它没有实现 #651 明列的 low/normal summary expiry 三边界及 batch membership/authority 断言，也没有覆盖 restart 后 seal/close 与 repeated-fused 排除。更严重的是现有 dispatch 路径仍在到达已冻结 summary 时点后以 `cmd.NowMS` 再算一次 `nextSummary`，会把成员放进下一次 summary，而不是升级时已冻结的本次 summary；新增测试没有观察 batch identity/due，因此无法发现该偏移。AdvanceInterrupt / wave1 I4 仍不可核销。

本轮遵守“禁止自修自审”：评审者未参与 #657 实现，只新增本报告，不修改被评审代码、规格或 runbook。

## 2. #651 剩余项关闭对账

### 2.1 repair restore/rollback：**YES**

- `verifyBackup` 会实际打开 backup 并执行 `PRAGMA integrity_check`，且在源库修改前失败关闭。
- `TestRepairBacksUpAndRestoresPopulatedDatabase` 对 populated backup 逐行核验，实际用 backup 覆盖 source，重新打开执行 integrity check，并确认恢复后的 immutable rows 与修复前相同。
- `TestRepairRollbackLeavesAllDuplicateGroupsUntouched` 构造两个 duplicate groups，在第二组注入 update abort；命令失败后全部四行逐字保持不变，append-only update 继续被 trigger 拒绝，且 backup 存在。
- repair 不再硬编码重建 trigger，而是读取并恢复数据库中的原 trigger SQL。
- runbook 写明 restore、重跑 audit 与失败停止服务，schema 验证值由 45 更新为当前 49。

注记：restore 测试没有直接再次调用 CLI 的 check-only arm，但 integrity、重开及完整行对比已覆盖其关键恢复事实，不阻断本项核销。

### 2.2 escalation/restart matrix：**NO**

已补且通过：

- `on_expire=hold|auto_reject`；
- `on_max=hold|auto_reject`；
- `max_escalations=0`；
- DB reopen 后 supervisor expiry、旧 version/nonce 命令拒绝；
- escalation immediate delivery 使用 `priority=strong`；
- 既有 normal→high→hold、nonce/version/expiry 单次路径与 repeated critical-fuse collecting authority 用例未回退。

仍缺：

1. **low/normal summary expiry 三边界与 membership：NO。** 没有分别断言 summary 早于、等于、晚于新 expiry 时的 `batched` / `batch_after_expiry`，也没有断言升级后 daily batch 的 batch ID、due、member version/nonce 和 collecting authority。
2. **当前实现存在 summary 时点二次推进风险。** escalation 在 `internal/storage/advance_interrupt.go` 中先以 `nextSummary(cmd.NowMS, ...)` 冻结 `next_dispatch_at_ms=at`；dispatch tick 到达 `at` 后又调用 `addDailyBatchMemberTx(..., cmd.NowMS, ...)`，后者再次执行严格晚于 `now` 的 `nextSummary`。当 `now==at` 时成员归到下一次 summary，而非本次已冻结时点；还可能越过新 expiry。现有测试只看 severity/state，未验证这一协议事实。
3. **完整状态快照：NO。** 新 outcome table 仅断言 status/held/close reason；未逐格验证 count、version、nonce、expires、delivery、next dispatch、admission/charge 不重复。
4. **restart/terminal 排除矩阵：NO。** reopen 用例只验证一次旧 CAS 和 immediate priority；没有 restart 后 batch seal、close/version-change 排除、空 batch cancellation，以及 repeated-fused 在 restart/close 后不得泄入 sealed payload。
5. **reason 约束矩阵：不完整。** 有 `startup_stall` 禁止 auto-reject 的既有用例，但没有将允许 reason 与禁止 reason 对 `on_expire/on_max` 的组合做闭合矩阵。

因此 #651 要求的 escalation/restart 验收矩阵不能以新增的两个测试视为完成。

## 3. 其余 P1 与 migration 回归

- Channel snapshot / repeated-fused authority：**YES（未回退，但 restart/close 排除仍属本轮矩阵缺口）**。
- successor provenance / replay：**YES（未回退）**。
- full effect-binding provenance：**YES（未回退）**。
- 新 migration：#657 **未新增 migration，符合“如新增须从 0050+ 起”**；当前 49 个 migration 为 0001–0049 连续、唯一。
- 0001 SHA-256：`9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`，未漂移。

## 4. 验证结果

- `go test ./internal/storage/ ./internal/intake/ ./cmd/siftd/ ./cmd/sift-advance-interrupt-repair/`：**通过**。
- `go test ./internal/storage -run 'TestAdvanceInterrupt|TestNextDailySummary' -count=10`：**通过**。
- `go test ./cmd/sift-advance-interrupt-repair -count=10`：**通过**。
- `go vet ./internal/storage ./internal/intake ./cmd/sift-advance-interrupt-repair ./cmd/siftd`：**通过**。
- `go test ./...`：第一次受已知 doctor 时序 flake 影响失败（fixture 子进程被系统 kill）；单独重跑该测试通过，随后全仓重跑**通过**。该现象与 #657 diff 无关，按既有阶段注记不作为本轮新阻断。
- `git diff d94e7d2^ d94e7d2 --check`：**通过**。

## 5. Issue #663 验收清单

- [x] 从检测到的 GitHub forge 获取并阅读 #663 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #663 comments：**YES（0 条）**
- [x] 对照 #651 FAIL / #657 核验 repair restore/rollback 与 escalation/restart：**YES**
- [x] populated repair backup/restore/rollback 演练：**YES**
- [x] runbook schema 值同步到当前 49：**YES**
- [x] 规定四包测试：**YES**
- [x] 全仓测试最终通过：**YES**
- [x] 只在当前 worktree 操作，未开波次二：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] escalation/restart 验收矩阵完整：**NO**
- [ ] #639 的 5 个 P1 全部关闭：**NO（4/5）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 6. 最终裁决

**FAIL。** #657 已真正关闭 repair restore/rollback，但 escalation/restart 只补了部分 outcome。下一实现至少须修复并测试“冻结 summary due 在 dispatch 时被二次推进”的问题，补 low/normal 的 summary 早于/等于/晚于 expiry、daily member/authority 完整断言，以及 restart 后 seal、close/version-change、empty cancellation 与 repeated-fused payload 排除矩阵；再由不同代理复审。本轨 PASS 前不得开启波次二。
