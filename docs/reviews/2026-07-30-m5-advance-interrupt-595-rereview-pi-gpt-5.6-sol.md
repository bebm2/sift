FAIL

# M5 #595 AdvanceInterrupt after #587 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#587 / PR #592，合入提交 `4664f82`（实现提交 `4ada947`）
> 评审基线：`main` `4664f82`
> 前次结论：[#583 FAIL](2026-07-30-m5-advance-interrupt-583-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#583 的 5 个 P1 仍未全部关闭。** #587 已把 mutable member authority 写入 active contract，补了 repeated critical fuse sealing 测试、0036 失败事务回滚测试，并恢复 binding digest UNIQUE；successor provenance 也未回退。但 authority 的 schema 约束仍不满足新 contract，multi-escalation/low-normal/restart 矩阵仍未补齐，full effect-binding 仍允许未绑定 source fact 的 policy/conflict/report identity。更直接地，0036 会拒绝含重复 binding digest 的真实旧库，而仓库没有任何可执行的升级前检查或修复/恢复说明。

本轮遵守“禁止自修自审”：评审者未参与 #587 实现，只新增本报告，不修改被评审代码或规格。

## 2. #583 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / repeated-fused：**NO（主路径与规格已改善，schema authority 未闭合）**

`storage.md:477-479` 和 `interrupt.md:316` 现已把 immutable member history 与 collecting current authority 分开定义；`TestAdvanceInterruptRepeatedCriticalFuseSealsCurrentAuthority` 也确实执行 normal→high→fused critical→repeated fused，并断言 sealed payload 使用最新 version/nonce（`advance_interrupt_test.go:52-133`）。生产 sealer 从 authority 读取的主路径因此有实质覆盖。

但 0036 新 trigger 仍弱于刚写入的 contract：

- insert/update 查询只要求 batch collecting 和字段匹配，未要求 `i.status='open'`（`0036:92-119`），与 `storage.md:479` 的“当时 open Interrupt”不一致；
- 没有 `DELETE` trigger，collecting 或 sealed/cancelled authority 均可被直接删除；
- 0035 的 sealed authority UPDATE guard 不含 `updated_at_ms`（`0035:18-27`），所以 sealed/cancelled 后仍能改该列，而 contract 要求 authority 不可改；
- 新测试只覆盖一次进程内 happy path，没有 projection replay、关闭竞态、restart 或上述 direct-SQL rejection vectors。

因此 mutable authority 已有规格和主路径测试，但“只能 current/open，seal 后 immutable”的持久化边界仍未关闭。

### P1-2 successor provenance / replay：**YES（保留覆盖风险）**

#587 未修改 `openCriticalSuccessorTx`；旧 batch 的冻结 window/limits 与 collision 后完整 project/Channel/Forge/scope/episode/due/fuse-config 重读仍保留。仍无 successor collision、配置变化、crash/replay、restart 或并发定向测试，本项继续只按静态路径核销。

### P1-3 migration / rollback、trigger 与升级路径：**NO（事务回滚 YES，完整升级闭环 NO）**

`TestPopulated0035To0036FailureRollsBackBindingIndexAndMigrationRecord` 确实让 UNIQUE index 创建失败，并验证 migration record/index/两行数据均回滚（`storage_test.go:205-244`）；`applyOne` 的单 migration 事务原子性得到定向覆盖。这一子项由 NO 变为 **YES**。

但该 fixture 不是 populated 0035 schema：它只手建 `schema_migrations` 与单列 binding 表，且失败发生在 0036 第一条语句，未验证在 drop/recreate triggers 后注入失败时旧 trigger 集可完整回滚。更重要的是，0036 第一条语句直接创建 UNIQUE index（`0036:5-6`），已知含重复 digest 的 0033–0035 旧库会拒绝启动；本变更没有修改 `docs/guides/`、`docs/runbooks/` 或其他用户文档，也没有提供升级前查询、备份、合法重复项判定、无损修复/迁移、重试和回退步骤。0033 曾明确允许不同 Interrupt 具有相同 closed binding，不能把删除 immutable 历史留给操作者猜测。

故“失败时不留下半 migration”成立，但 issue #595 特别要求核验的可接受升级路径不存在，完整 migration P1 不能核销。

### P1-4 多次升级与 low/normal summary：**NO**

新测试只覆盖一条 normal→high→critical→repeated-critical 轨迹，并在远未来立即 seal（`advance_interrupt_test.go:94-131`）。它没有覆盖 #583 要求的 count 0/1/2 表驱动边界、downgrade 后 low/normal batch、summary expiry 前后、上限去向、restart 后 tick/seal，以及 repeated-fused 的关闭/排除矩阵。`interrupt.md:336-338` 的派生验收矩阵仍远大于当前覆盖。

因此 dedicated repeated critical fuse sealing test 为 **YES**，但 dedicated multi-escalation/low-normal/restart matrix 仍为 **NO**。

### P1-5 full effect-binding：**NO**

0036 恢复 reason/type/shape trigger 和 digest UNIQUE，writer/Go validator 也统一把 `report_id` 加入 `agent_blocked` arm；这是实质修复。但 durable identity 仍未闭合：

- 0036 的 `agent_blocked` identity 只查 attempt，不再把 `report_id` FK-match 到同 Run/attempt 的 blocker `report_receipts`（`0036:83-85`）；Go auto-reject validator同样只查 attempt（`advance_interrupt.go:304-317`）。任意非空 report ID 可成为 privilege binding。
- code-review trigger 在 `calibration_id IS NULL` 或 `run.change_id IS NULL` 时把 identity 当作存在，且 Go 只重读当前 Change/head，不验证 `review_policy_snapshot_digest`（`0036:74-76`、`advance_interrupt.go:294-299`）。
- merge-conflict 只绑定当前 Change/head，`conflict_digest` 仅非空；没有 durable conflict source fact（`0036:77-79`、`advance_interrupt.go:294-299`）。
- guardrail insert trigger虽查 Gate facts，但 Go auto-reject 路径仍直接 `exists=1`（`advance_interrupt.go:300-303`）；0033–0035 窗口中已持久化的错误 row 没有 0036 数据校验/隔离，仍可被消费。
- 仓库仍没有每 arm required/null、错 options、错组合 FK、错 policy/conflict/report provenance、旧库 corrupt row、重复 digest 修复后的 auto-reject 定向测试。

这不满足 `storage.md:490` 的“写端口及 CHECK 共同保证”与错配 FK/重复 binding 一律回滚契约。

## 3. migration 0036 专项结论

- 新 migration 从 0036 起：**YES**。
- 0036 版本无重复：**YES**；0001–0036 连续、36 个版本各一个文件。
- 发布的 0001 bytes 未漂移：SHA-256 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- duplicate digest migration failure 原子回滚：**YES**。
- 真实 populated 0035 schema 的 mid-migration trigger rollback：**NO**。
- 含重复 digest 旧库的可接受升级路径说明：**NO**。
- latest-schema authority current/open/immutable 约束：**NO**。
- latest-schema full effect-binding durable references：**NO**。

## 4. 回归与执行证据

- 从检测到的 GitHub forge 获取并阅读 #595 全文、Agent 建议、关闭条件与 comments（0 条），并回溯 #583、#587 与实现提交。
- `git diff 4ada947^..4ada947 --check`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./internal/storage`：**通过**。
- `go test ./...`：**首次未通过**，仅 `internal/controlplane.TestDoctorBaselineChecksConfiguredDependencies` 的 fixture agent 被 `signal: killed`；单独重跑该测试通过，符合既有 doctor 时序 flake 特征，不是本变更阻断项。
- migration 扫描：0001–0036 连续，重复版本为零。

绿灯没有覆盖上述 authority direct-SQL vectors、真实 0035 trigger rollback、multi-escalation/low-normal/restart 矩阵或 effect-binding provenance 反例，不能反证静态缺口。

## 5. Issue #595 验收清单

- [x] 获取并阅读 #595 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #595 comments：**YES（0 条）**
- [x] 对照 #583 FAIL / #587 核验 5×P1：**YES**
- [x] 核验 migration 0036 无重复：**YES**
- [x] 核验含重复 binding digest 旧库的升级路径说明：**YES；结果 NO**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #583 的 5 个 P1 全部关闭：**NO（1/5 完整关闭）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO**

## 6. 最终裁决

**FAIL。** #587 不可核销 #583。下一实现至少须：补齐 authority 的 open/current、DELETE 与 sealed/cancelled 全列 immutable schema guards及 rejection/restart 测试；提供真实 populated 0035 duplicate-digest 库的可执行升级/恢复路径和 mid-migration trigger rollback 测试；补 count 0/1/2、low/normal summary、expiry/restart sealing 矩阵；把 agent report、review policy、conflict 和 guardrail 每个 identity 绑定到 durable source fact，并覆盖旧 row 与错配 FK vectors，再交由不同代理复审。
