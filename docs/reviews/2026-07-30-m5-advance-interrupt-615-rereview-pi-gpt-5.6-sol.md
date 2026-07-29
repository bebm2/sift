FAIL

# M5 #615 AdvanceInterrupt after #610 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#610 / PR #614，合入提交 `c5000df`（实现提交 `18d3585`）
> 评审基线：当前 `main` `c5000df`
> 前次结论：[#604 FAIL](2026-07-30-m5-advance-interrupt-604-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.4、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#604 的 5 个 P1 仍只有 1/5 完整关闭。** #610 使用正确的 migration 0041，修复了 collecting authority 指向 sealed/cancelled batch 的特定旁路、补上 Go 消费端的 guardrail provenance，并修正 intake 与 agent-blocked 测试 fixture；`internal/storage` 与 `internal/intake` 已全绿。但实现方已明确自述为 NO 的 duplicate-digest 工具和 escalation/restart 矩阵确实仍不存在；此外 authority 仍可 retarget 到另一个 collecting member，0041 重建的 direct-SQL provenance trigger 只覆盖 agent-blocked，merge-conflict 仍可持久化无 durable conflict source 的 binding。

本轮遵守“禁止自修自审”：评审者未参与 #610 实现，只新增本报告，不修改被评审代码、规格或 runbook。

## 2. #604 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / repeated-fused authority：**NO（部分关闭）**

0041 的 authority UPDATE guard 同时 JOIN OLD/NEW batch 并要求两者 collecting（`0041_advance_interrupt_provenance_closure.sql:5-19`），所以 #604 指出的 collecting→sealed/cancelled retarget 已封死。

但该 guard明确允许把 `(OLD.batch_id, OLD.interrupt_id)` 改成另一个 **collecting** batch/member，只要 NEW 字段匹配其 open Interrupt（`:9-17`）。这会删除原 member 的唯一 current authority，并把该行变成 NEW member 的 authority；schema 没有保证每个 collecting member 恰有一行 authority。既有全 DELETE trigger不能拦截通过 PK UPDATE 完成的等效删除。#610 也没有新增 authority rejection、cross-batch retarget、close race、restart 或 projection replay 测试；`advance_interrupt_test.go` 只增加两行 report fixture（`:20-21`）。因此 sealed-target 特定缺口已关，但 storage §6.3 的完整 current-authority 边界仍未闭合。

### P1-2 successor provenance / replay：**YES（未回退）**

#610 未修改 `openCriticalSuccessorTx`。旧 batch 冻结 window/limits、collision 后完整 identity 重读和 successor 行为未见回退。本项继续按静态路径核销；collision/crash/restart 专项覆盖风险仍在。

### P1-3 migration、rollback 与 duplicate-digest 升级路径：**NO**

- migration 从 **0041** 起且版本唯一：**YES**；当前 0001–0041 连续，无重复或缺号。
- 既有 duplicate-digest 失败事务回滚测试：**YES，未回退**。
- 仓库内可操作升级/恢复工具：**NO**。

#610 没有修改 runbook，也没有增加工具。runbook 仍要求使用未命名、仓库内不存在的“发布工具”重算 digest（`advance-interrupt-migration.md:17-23`）；相同 canonical binding 重算仍得到相同 SHA-256，不能在保留两行时满足 UNIQUE。文档仍没有工具命令、输入输出、immutable history 无损映射、真实 populated duplicate 库演练或恢复命令，而且升级后期望版本仍写 `38`（`:25-33`），已与 latest schema 41 不符。故该路径不可执行。

### P1-4 multi-escalation / low-normal / restart 矩阵：**NO**

#610 没有增加任何矩阵测试。`advance_interrupt_test.go` 唯一改动是为已有单次 agent-blocked escalation 加 report event/receipt fixture（`:20-21`）。count 0/1/2、downgrade 后 low/normal summary、summary expiry 前后、上限去向、restart 后 tick/seal、repeated-fused 关闭/排除矩阵仍缺失。

### P1-5 full effect-binding provenance：**NO（消费端改善，写边界仍未闭合）**

Go auto-reject validator 现在会把 guardrail arm 绑定到 calibration/gate/snapshot 的 head、rule 与 matched-path digest（`advance_interrupt.go:313-322`）；agent-blocked 的合法 report fixture也恢复了正常 EmitInterrupt/intake 路径。这是实质改善。

但 latest-schema direct-SQL 边界仍不完整：

1. 0041 先 DROP 0038 的综合 `interrupt_binding_provenance_insert`，随后只为 `agent_blocked` 重建该 trigger（`0041:21-29`）。文件头所称“every effect-binding arm”与实际 SQL 不符。
2. 0039 的 identity trigger对 merge-conflict 只绑定当前 Change/head；0041 删除了 0038 对 calibration/snapshot/conflict digest 的附加检查。于是 direct SQL 仍可插入匹配 Change/head但任意 `conflict_digest` 的 canonical binding。Go 消费端会在 auto-reject 时拒绝（`advance_interrupt.go:302-312`），但这不能替代 storage §6.4 要求的写入回滚，也不能隔离既有错误 row。
3. #610 没有新增每 arm 的无 report/无 calibration、错 policy/conflict/report、旧 row、direct-SQL rejection 测试。

因此 effect-binding 不能核销。

## 3. migration 0041 与回归结论

- 新 migration 为 0041：**YES**。
- migration 0001–0041：**连续、无重复、无缺号**。
- 发布的 0001 bytes 未漂移：SHA-256 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- collecting→sealed/cancelled authority retarget：**已拒绝**；collecting→collecting 等效删除：**仍允许**。
- duplicate-digest 工具及 populated 恢复演练：**不存在**。
- multi-escalation/low-normal/restart 矩阵：**不存在**。
- latest-schema full effect-binding direct-SQL provenance：**未闭合**。
- `go test ./internal/storage/ ./internal/intake/`：**通过**。
- `go test ./...`：首次仅 doctor fixture `signal: killed` 与 launchworker crash suite 时序失败；两个失败用例分别单独重跑均通过，属于既有并行时序 flake，未发现本变更功能回归。
- `go vet ./internal/storage ./internal/intake ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。

## 4. Issue #615 验收清单

- [x] 从检测到的 GitHub forge 获取并阅读 #615 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #615 comments：**YES（0 条）**
- [x] 对照 #604 FAIL / #610 核验 5×P1：**YES**
- [x] 严格核验实现方所述 intake、duplicate-digest 工具与 escalation/restart 矩阵：**YES；intake YES，后两项 NO**
- [x] 核验 migration 0041：**YES；版本正确且唯一，但不能关闭其余 P1**
- [x] `go test ./internal/storage/ ./internal/intake/`：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #604 的 5 个 P1 全部关闭：**NO（1/5）**
- [ ] duplicate-digest 升级/恢复工具可操作：**NO**
- [ ] escalation/restart 验收矩阵完整：**NO**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 5. 最终裁决

**FAIL。** #610 不可核销 #604。下一实现至少须：禁止 authority 通过 PK UPDATE retarget 到任意其他 member并补 direct-SQL/restart vectors；提供仓库内真实可执行的 duplicate-digest 检查、备份、无损修复、恢复工具和 populated 演练；补齐 multi-escalation、low/normal summary、expiry 与 restart sealing 矩阵；恢复/补齐每个 effect-binding arm 的 unconditional durable-source 写入约束，特别是 merge-conflict，并覆盖错误旧 row 与错 policy/conflict/report 反例，再交由不同代理复审。
