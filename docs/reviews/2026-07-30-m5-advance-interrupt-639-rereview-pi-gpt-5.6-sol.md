FAIL

# M5 #639 AdvanceInterrupt after #633 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#633 / PR #638，合入提交 `20c45f2`（实现提交 `eef25fa`、测试提交 `d19d0cb`）
> 评审基线：当前 `main` `20c45f2`
> 前次结论：[#627 FAIL](2026-07-30-m5-advance-interrupt-627-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.4、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#627 的 5 个 P1 只有 2/5 完整关闭。** migration 0045 恢复了 collecting authority 的 current-open mirror 并保留 PK 不可 retarget；successor 路径未回退。repair CLI 的 append-only 阻断和逐 group 提交已经修复，但 #627 要求的 populated restore/失败回滚演练仍未提供。新增 escalation 测试只覆盖一条 downgrade count 1/2→hold 路径，远不是要求的 low/normal、summary expiry、各上限去向和 restart 矩阵。

merge-conflict 的 canonical digest、Go 消费端校验与 latest-schema trigger 有实质改善，但生产 `mergeability_unknown` 路径仍被构造成 `merge_conflict`，而 0045 只接受 snapshot/verdict 都为 `conflicting`；该合法 Gate HITL 会在写 binding 时失败。merge-conflict 的无 calibration/错 digest direct-SQL 反例也没有补齐。当前全仓测试因此还存在一个确定性失败。

本轮遵守“禁止自修自审”：评审者未参与 #633 实现，只新增本报告，不修改被评审代码、规格或 runbook。

## 2. #627 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / repeated-fused authority：**YES**

0045 的 UPDATE trigger 同时拒绝 `(batch_id, interrupt_id)` 改写，并要求 collecting authority 的 version、nonce、headline、reason、severity、links、options 逐项 mirror 当前 open Interrupt（`0045_advance_interrupt_p1_closure.sql:5-22`）。既有 sealed/cancelled immutable trigger仍在。新增 direct-SQL nonce 伪造反例也证明 current-state guard 已恢复（`advance_interrupt_test.go:125-130`）。

测试粒度仍只有字段伪造，没有独立的 restart/projection replay vector；但 #627 指出的实际回退边界已由 schema 封闭，本项核销。

### P1-2 successor provenance / replay：**YES（未回退）**

#633 未修改 critical successor 的 collision/replay 主路径。旧 batch 的冻结 window/limits、successor identity 重读与 replay 行为未见回退，本项沿用 #627 的静态核销结论。

### P1-3 migration、rollback 与 duplicate-digest repair：**NO（主阻断已修，恢复验收未闭合）**

- migration 从 **0045** 起且版本唯一：**YES**；当前 0001–0045 连续、无重复、无缺号。
- repair 不再被 append-only trigger 阻断：**YES**。工具在一个事务中 DROP trigger、修复全部 group、重建 trigger并提交（`main.go:101-130`），失败可全局回滚。
- identical canonical binding：**正确 fail closed**，继续要求人工升级（`main.go:68-79`）。
- populated non-identical repair：**YES**；新增测试确认 digest 分离且 append-only trigger 恢复（`main_test.go:13-47`）。
- populated restore/失败回滚演练：**NO**。测试虽然请求 `--backup`，但从未打开、校验或恢复该 backup，也没有注入第二 group/重建 trigger/commit 失败后证明源库零部分改写；这没有完成 #627 最终裁决明确要求的 “populated repair/restore test”。

runbook 已把最新 schema 期望修正为 45，但标题摘要仍写“0036”，示例 backup 名仍为 `pre-0043`（`advance-interrupt-migration.md:4,16`），属于非阻断的文档漂移。

### P1-4 multi-escalation / low-normal / restart 矩阵：**NO**

新增文件只有一个 57 行测试：冻结 downgrade 后验证两次升级 severity 为 normal/high，第三次进入 `hold/max_escalations`（`advance_interrupt_matrix_test.go:8-57`）。它没有验证：

- count=0 的冻结 severity/dispatch/admission 基线；
- low 与 normal 升级后的 daily summary membership、冻结 Channel snapshot 和 operation；
- summary 在新 expiry 前/等于/晚于 expiry 的 batch 与 `batch_after_expiry` 分支；
- `on_expire=hold|auto_reject` 及 `on_max=hold|auto_reject` 的允许/禁止 reason 矩阵；
- max=0、首次/末次升级的完整 nonce/version/expires/next-dispatch 断言；
- DB reopen 后 supervisor tick、batch seal、旧 tick/restart snapshot 排除；
- repeated-fused 在 restart/close 后不泄入 sealed payload。

测试甚至只断言 severity 和最终 held state，没有断言每步的 count、nonce、version、expiry、delivery、next dispatch、admission 或 outbox。因此 #627 指定的 escalation/restart 矩阵仍未闭合。

### P1-5 full effect-binding provenance：**NO（conflicting arm 改善，但合法 unknown 路径回归）**

关闭的部分：

1. `MergeConflictDigest` 已按规格对 canonical `{change_id,head_sha,mergeability:"conflicting"}` 求 SHA-256（`advance_interrupt.go:163-172`）；Gate emitter 使用该函数（`internal/gate/interrupt.go:25-28`）。
2. storage auto-reject 消费端重算并比较 digest，且要求 calibration/snapshot/evaluation 的 conflicting source（`advance_interrupt.go:313-326`）。
3. 0045 移除了 code-review/merge-conflict 的无-calibration旁路，并要求 snapshot 持久化的 conflict digest 与 binding 一致（`0045:40-57`）。

仍有一个生产语义缺口：`interruptCommand` 把 `merge_conflict` **和 `mergeability_unknown`** 都构造成 `InterruptMergeConflict`，且都生成写死 `mergeability:"conflicting"` 的 digest（`internal/gate/interrupt.go:25-28`）。但 Gate 对 unknown 生成的 snapshot 是 `change.mergeability="unknown"`，verdict 也是 `mergeability_unknown`，而 0045 trigger 要求 snapshot 和 verdict 的 `mergeability` 都等于 `conflicting`（`0045:55-57`）。因此合法 `hitl/mergeability_unknown` 无法通过五件事事务；这也与 storage §8.1 将 `mergeability_unknown` 规定为 `failure_review` successor 相冲突。

此外新增测试只有 digest helper 的正例，没有 latest-schema 的 merge-conflict 无 calibration、错误 digest、错误 snapshot/verdict 或 `mergeability_unknown` 集成反例。现有 daemon 生产唤醒测试仍通过 direct `EmitInterrupt` 构造无 calibration、伪 digest 的 merge-conflict（`cmd/siftd/main_test.go:37-54`），现在稳定失败，说明调用方/fixture 未随 closed provenance 边界迁移。

## 3. migration 0045 与回归结论

- 新 migration 为 0045：**YES**。
- migration 0001–0045：**连续、无重复、无缺号**。
- 发布的 0001 bytes 未漂移：SHA-256 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- collecting authority PK retarget/current mirror：**已拒绝**。
- non-identical duplicate repair：**可执行且全局原子**；restore/失败回滚自动化验收仍缺。
- multi-escalation/low-normal/restart 矩阵：**仍不完整**。
- calibrated conflicting provenance：**实现改善**；`mergeability_unknown` 合法生产路径仍失败。
- `go test ./internal/storage/ ./internal/intake/ ./cmd/sift-advance-interrupt-repair ./internal/gate/`：**通过**。
- `go test ./internal/storage/ ./internal/intake/`：**通过**，满足 #633 明示门禁，intake 未回退。
- `go test ./...`：**失败**；`TestProductionSchedulerWakesOutboxAfterEnqueueAndEmitInterrupt` 稳定报 `constraint failed: invalid interrupt binding identity (1811)`。单独 `-count=3` 三次均失败，不是时序 flake。
- `go vet ./internal/storage ./internal/intake ./internal/gate ./cmd/sift-advance-interrupt-repair ./cmd/siftd`：**通过**。

## 4. Issue #639 验收清单

- [x] 从检测到的 GitHub forge 获取并阅读 #639 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #639 comments：**YES（0 条）**
- [x] 对照 #627 FAIL / #633 核验 5×P1：**YES**
- [x] 核验 repair CLI、collecting authority、provenance、escalation/restart 矩阵：**YES**
- [x] 核验 migration 0045：**YES；版本正确且唯一**
- [x] `go test ./internal/storage/ ./internal/intake/`：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #627 的 5 个 P1 全部关闭：**NO（2/5）**
- [ ] populated repair/restore/rollback 演练完整：**NO**
- [ ] escalation/restart 验收矩阵完整：**NO**
- [ ] full effect-binding provenance 的合法生产矩阵闭合：**NO**
- [ ] `go test ./...`：**NO（确定性 siftd 失败）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 5. 最终裁决

**FAIL。** #633 不可核销 #627。下一实现至少须：补 populated backup restore 与事务失败零部分改写演练；按 interrupt/storage 规格完成 count 0/1/2、low/normal summary expiry、各 on-expire/on-max 去向、restart tick/seal/repeated-fused 排除矩阵；将 `mergeability_unknown` 接到规定的 `failure_review` durable successor，而不是伪造 conflicting provenance；补 merge-conflict latest-schema direct-SQL 与生产 Gate 反例，并修复确定性 `cmd/siftd` 回归后，再交由不同代理复审。
