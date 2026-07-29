FAIL

# M5 #675 AdvanceInterrupt after #669 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#669 / PR #674，合入提交 `8b2c698`（实现提交 `9edb9ff`）
> 评审基线：当前 `main` / 本 worktree `8b2c698`
> 前次结论：[#663 FAIL](2026-07-30-m5-advance-interrupt-663-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#669 修复了 frozen daily-summary due 在 dispatch 时被二次推进的确定性缺陷，但没有补齐 #663 明列的 summary 三边界与 restart/seal/close/repeated-fuse 排除矩阵；#639 的 5 个 P1 仍为 4/5。**

生产 dispatch 现在读取 Interrupt 已冻结的 `next_dispatch_at_ms`，并通过 `addDailyBatchMemberAtTx` 使用该 occurrence 创建 daily batch，不再在 tick 时调用 `nextSummary(now)`。新增测试也证明一条升级后 normal/batch 路径在 due tick 使用同一 batch ID/due，并写入当前 version/nonce authority。该修复方向和主路径正确。

但是 #669 只有这一条新增测试。它没有覆盖 summary 等于或晚于新 expiry 的 `batch_after_expiry`，没有 reopen 后 seal daily/critical batch，没有关闭或 version-change 后排除成员，也没有 repeated-fused 在 restart/close 后收敛为正确 sealed payload 或 empty cancellation。仓库既有 restart 测试只覆盖 immediate strong delivery；既有 repeated-fuse 测试只在同一 DB 连接中直接 seal open member。故不能以绿灯代替 #663 明确要求的验收矩阵，AdvanceInterrupt / wave1 I4 仍不可核销。

本轮遵守“禁止自修自审”：评审者未参与 #669 实现，只新增本报告，不修改被评审代码、规格或测试。

## 2. #663 遗留 P1 核验

### 2.1 frozen daily-summary due：**YES**

- `AdvanceInterrupt` 的 dispatch 查询现在同时读取持久化 `next_dispatch_at_ms`。
- 非 immediate dispatch 不再以 `cmd.NowMS` 重算下一日 summary，而是调用 `addDailyBatchMemberAtTx(..., nextDispatch.Int64, ...)`。
- 缺失 frozen due 会使事务拒绝并回滚，不会先永久消费 ready marker。
- `TestAdvanceInterruptDispatchUsesFrozenSummaryDue` 在升级后读取 frozen due，于该 due 执行 supervisor tick，并断言 batch ID、batch due、member version/nonce 及 authority 一致。它能捕获 #663 定位的“到点后推进到次日”回归。

### 2.2 escalation/restart 验收矩阵：**NO**

#669 没有实现其关闭条件要求的完整矩阵：

1. **summary expiry 三边界不完整。** 新测试只覆盖 `summary < new expiry` 的合法入批；没有 `summary == new expiry` 与 `summary > new expiry` 两格，也未断言两格均为 `held/batch_after_expiry`、无 member/authority/operation。
2. **restart 后 batch seal：缺失。** `TestAdvanceInterruptRestartRejectsOldTickAndCreatesStrongEscalationDelivery` 只 reopen 后验证 immediate escalation、旧 CAS 与 strong priority；没有 reopen 后 dispatch/prepare daily batch，也没有 critical batch sealing。
3. **close/version-change 排除与 empty cancellation：缺失。** 没有测试 collecting member 在 sealing 前关闭或 nonce/version 改变后不得进入 payload，也没有断言唯一成员被排除时 batch 为 `cancelled` 且无 `channel_publish`。
4. **repeated-fuse restart/close：缺失。** `TestAdvanceInterruptRepeatedCriticalFuseSealsCurrentAuthority` 只在同一连接中让 open member repeated fuse 后 seal；没有 DB reopen，也没有 close 后 seal，不能证明旧 authority 不会在恢复路径泄入 payload。
5. **完整状态矩阵仍缺。** outcome table 仍只检查 status/held/close reason；未逐格核验 escalation count、version、nonce、expires、delivery、next dispatch、admission/charge/outbox 不重复。允许 reason 与 `startup_stall` 禁止 auto-reject 的组合也没有形成闭合矩阵。

因此 P1-4 `multi-escalation / low-normal / restart` 仍为 **NO**；其余四项沿用 #663 的已核销结论，未见 #669 回退。

## 3. migration 与回归

- #669 **未新增 migration**，符合实现方自述；当前 migration 为 0001–0050 连续、唯一，0050 来自并行已合入变更。
- 已发布 0001 SHA-256 仍为 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- `go test ./internal/storage/ ./internal/intake/ ./cmd/siftd/ ./cmd/sift-advance-interrupt-repair/`：**通过**。
- `go test ./internal/storage -run 'TestAdvanceInterrupt|TestNextDailySummary' -count=10`：**通过**。
- `go test ./cmd/sift-advance-interrupt-repair -count=10`：**通过**。
- `go vet ./internal/storage ./internal/intake ./cmd/sift-advance-interrupt-repair ./cmd/siftd`：**通过**。
- `go test ./...`：**通过**。
- `git diff 9edb9ff^ 9edb9ff --check`：**通过**。

## 4. Issue #675 验收清单

- [x] 从检测到的 GitHub forge 获取并阅读 #675 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #675 comments：**YES（0 条）**
- [x] 对照 #663 FAIL / #669 核验 frozen due 与 restart/seal/close/repeated-fuse：**YES**
- [x] frozen daily-summary due 不再于 dispatch 重算：**YES**
- [x] #669 无新 migration，0001 未漂移：**YES**
- [x] 规定四包测试：**YES**
- [x] `go test ./...` 与定向重复测试：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] summary 早于/等于/晚于新 expiry 三边界完整：**NO**
- [ ] restart 后 daily/critical seal 完整：**NO**
- [ ] close/version-change/empty cancellation 排除完整：**NO**
- [ ] repeated-fuse restart/close 排除完整：**NO**
- [ ] #639 的 5 个 P1 全部关闭：**NO（4/5）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO（1）**

## 5. 最终裁决

**FAIL。** #669 可核销 frozen due 的生产缺陷，但不能核销 #663 要求的完整 escalation/restart P1。下一实现至少须补 summary `< / == / >` 新 expiry 三边界，以及 reopen 后 daily/critical seal、关闭与 version-change 排除、空 batch cancellation、repeated-fused restart/close payload 排除的定向测试，并逐格断言状态、authority、admission/charge 与 operation；之后再由不同代理复审。
