FAIL

# M5 #604 AdvanceInterrupt after #598 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#598 / PR #602，合入提交 `caa94e8`（实现提交 `bc4edf8`）
> 评审基线：当前 `main` `1836e09`；#598 结论按 `56b5481..caa94e8` 隔离核验
> 前次结论：[#595 FAIL](2026-07-30-m5-advance-interrupt-595-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.4、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#595 的 5 个 P1 仍只有 1/5 完整关闭。** #598 新增 migration 0038，补了 authority 的 open、DELETE、timestamp immutable guard，也加强了 report/review/conflict 的部分 provenance；但 collecting authority 仍可跨 batch UPDATE 到 sealed batch，升级 runbook 依赖不存在的“发布工具”且给出的 duplicate-digest 保留步骤与 digest 定义/UNIQUE 约束互相冲突，要求的 multi-escalation/low-normal/restart 矩阵完全没有新增，effect-binding 的 direct-SQL provenance guard 仍有条件性绕过。当前全量测试还稳定暴露 `invalid interrupt binding provenance` 回归。

本轮遵守“禁止自修自审”：评审者未参与 #598 实现，只新增本报告，不修改被评审代码、规格或 runbook。

## 2. #595 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / repeated-fused authority：**NO（部分关闭）**

0038 的 INSERT guard 现要求 `i.status='open'`，sealed/cancelled authority UPDATE 比较包含 `updated_at_ms`，authority DELETE 全拒绝，sealed/cancelled member DELETE 也拒绝；这些关闭了 #595 指出的直接缺口。既有 repeated-fused happy path 与后续 Channel direct-SQL 测试也仍通过。

但 UPDATE guard 以 **OLD batch** 是否 collecting 决定 current 分支，校验 NEW 行时只 JOIN `attention_batch_members` 与 `interrupts`，没有 JOIN/约束 **NEW batch 必须 collecting**（`0038_advance_interrupt_p1_closure.sql:26-36`）。immutable guard同样只看 OLD batch 是否已离开 collecting（`:38-46`）。因此 direct SQL 可把 collecting batch 的 authority 主键及字段改成另一个已有 member 对应的 sealed/cancelled batch；current guard不会检查新 batch state，immutable guard也不会触发。这仍可改写 storage §6.3 定义的 sealed authority。

#598 没有新增 authority rejection、cross-batch retarget、close race、restart 或 projection replay 测试；`storage_test.go` 的改动只有 latest migration 计数 `38`。故本项不能核销。

### P1-2 successor provenance / replay：**YES（未回退）**

#598 未修改 `openCriticalSuccessorTx`。旧 batch 的冻结 window/limits、collision 后完整 project/Channel/Forge/scope/episode/due/fuse-config 重读路径仍保留。专项 collision/crash/restart 测试仍不足，但未发现相对 #595 的新回退，本项继续按静态路径核销。

### P1-3 migration、rollback 与 duplicate-digest 升级路径：**NO**

- migration 从 **0038** 起：**YES**；0038 版本唯一，当前 0001–0039 连续且无重复。
- 0036 duplicate digest 失败事务回滚测试仍通过：**YES**。
- 可操作升级路径：**NO**。

runbook 的预检 SQL可找出重复 digest，也明确要求备份和失败停机；但修复核心步骤不可执行：

1. 文档要求“由发布工具生成新的、重新计算的 digest”，仓库没有给出工具名称、命令、输入/输出格式或恢复命令；全仓也找不到该工具。
2. contract 定义 `binding_digest=SHA-256(canonical_json(binding_json))`。相同 canonical binding 重算仍得到相同 digest，不能在保留两行的同时通过 UNIQUE；不同 binding 却同 digest 时按同一算法重算也不能消除碰撞。
3. 文档一面要求保留一条被 `interrupt_id` 引用的事实，一面要求 immutable binding 不删除；binding 本身以 `interrupt_id` 为 PK，每条重复行都属于一个不同 Interrupt，未说明如何无损保留其余 Interrupt 的 command authority/audit history。
4. 没有真实 populated 0035/0037 duplicate fixture 演练“备份→修复→0036/0038→foreign_key_check→restart”，也没有回退/恢复命令。服务启动会先在 0036 UNIQUE 处失败，根本到不了 0038。

因此该 runbook 只是处置原则，不是 issue 要求的可操作升级路径。

### P1-4 multi-escalation / low-normal / restart 矩阵：**NO**

#598 对 `advance_interrupt_test.go` **零改动**。#595 要求的 count 0/1/2、downgrade 后 low/normal summary、summary expiry 前后、上限去向、restart 后 tick/seal、repeated-fused 关闭/排除矩阵仍未补齐。已有 normal→high→critical→repeated-critical 单进程路径不能替代 `interrupt.md` §9.2 的派生矩阵。

### P1-5 full effect-binding provenance：**NO（部分加强但仍可绕过）**

Go auto-reject validator现能校验 blocker `report_receipts`、code-review policy snapshot 和 conflict snapshot；0038 也新增了 INSERT provenance trigger。但 schema guard 的 WHEN 条件本身留下 direct-SQL bypass：

- `agent_blocked` 只有在同 run/attempt **已经存在任一 report** 时才检查 `report_id`；若该 attempt 没有 report，外层 `EXISTS` 为 false，伪造 report ID 可插入（0038 `:66-70`）。
- `code_review` 只有 digest 非空且 Interrupt 已有 calibration 时才执行新检查；calibration 缺失时仍落回 0036 identity trigger，而该旧 trigger显式把 `i.calibration_id IS NULL` 当作可接受（0038 `:71-78` 对照 0036 identity trigger）。
- `merge_conflict` 同样只在 calibration 非空时执行新 provenance 检查；无 calibration 时仍只绑定当前 Change/head，`conflict_digest` 没有 durable conflict source fact（0038 `:79-85`）。
- `guardrail_violation` 的 Go validator仍直接 `exists=1`，#598 没有补旧 row 数据校验/隔离，也没有每 arm 错 policy/conflict/report provenance 的定向反例。

此外 #598 没有新增任何 effect-binding 测试。当前 `go test ./...` 的五个 `internal/intake` case 均在正常 `RecordGateEvaluationAndEmitInterrupt` fixture 上被 0038 的 `invalid interrupt binding provenance` 拒绝，说明新增约束与现有生产调用/fixture 尚未闭合；这不是绿灯可忽略的时序 flake。

## 3. migration 0038 专项结论

- 新 migration 为 0038：**YES**。
- 0038 版本无重复：**YES**；当前 migration 0001–0039 连续、39 个版本各一份。
- 发布的 0001 bytes 未漂移：SHA-256 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- authority open、DELETE、timestamp immutable 基础 guard：**YES**。
- authority cross-batch retarget 不可绕过：**NO**。
- 含 duplicate digest 旧库的可执行升级/恢复路径：**NO**。
- latest-schema full effect-binding durable references：**NO**。
- 全仓回归：**NO**。

## 4. 执行证据

- 从检测到的 GitHub forge 获取并阅读 #604 全文、Agent 建议、关闭条件及 comments（0 条），并回溯 #595、#598 和对应提交。
- `git diff caa94e8^..caa94e8 --check`：**通过**。
- `go test ./internal/storage/`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./...`：**失败**；`internal/intake` 5 个 case 报 `constraint failed: invalid interrupt binding provenance (1811)`，其余包通过。
- migration 扫描：0001–0039 连续，无重复/缺号；0038 唯一。

## 5. Issue #604 验收清单

- [x] 获取并阅读 #604 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #604 comments：**YES（0 条）**
- [x] 对照 #595 FAIL / #598 核验 5×P1：**YES**
- [x] 核验 migration 0038：**YES；版本唯一，但不能关闭相关 P1**
- [x] 核验 runbook 升级路径是否可操作：**YES；结果 NO**
- [x] `go test ./internal/storage/`：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #595 的 5 个 P1 全部关闭：**NO（1/5）**
- [ ] 全仓回归通过：**NO**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 6. 最终裁决

**FAIL。** #598 不可核销 #595。下一实现至少须：封死 collecting→sealed/cancelled 的 authority retarget 并补 direct-SQL/restart vectors；提供仓库内真实存在、命令完整且能在 populated duplicate 库上演练通过的修复/恢复工具与 runbook；补齐 multi-escalation、low/normal summary 和 restart sealing 矩阵；将每个 binding arm 无条件绑定 durable source fact并覆盖无 report/无 calibration/错 policy/conflict/report/旧 row vectors；最后修复并跑绿当前 `internal/intake` provenance 回归，再由不同代理复审。
