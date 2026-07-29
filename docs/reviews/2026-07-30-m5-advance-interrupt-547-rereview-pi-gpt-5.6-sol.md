FAIL

# M5 #547 AdvanceInterrupt after #539 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#539 / PR #544，合入提交 `1703ea9`（实现提交 `580ba80`）
> 评审基线：`main` `1703ea9`
> 前次结论：[#535 FAIL](2026-07-30-m5-advance-interrupt-535-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#535 的 5 个 P1 仍无一完整关闭。** #539 把 0021 的 FK pragma 移到 migration 事务外、在 0024 恢复 `interrupts_close_write_once`，并新增 critical episode identity/successor 与 binding canonical/digest 检查；这些是正向改进。但 repeated-fused 的修复只是停止比较既有 member 的 version/nonce：第二次合法升级提交后，member 仍冻结旧 version/nonce，sealer 会把它排除并取消空 batch。新增 successor 也没有覆盖冻结窗口/limit 的正确来源或任何验收测试。所谓 full effect-binding 与规格的 closed union 直接不一致，且没有组合 FK/options 校验。

本轮遵守“禁止自修自审”：评审者未参与 #539 实现，只新增本报告，不修改被评审代码或规格。

## 2. #535 的 5 个 P1 关闭对账

### P1-1 Channel snapshot / batch：**NO**

- critical batch 查找只比较 Channel ID 与 Forge target，不比较 `channel_snapshot_json` 或 batch `project_id`（`internal/storage/advance_interrupt.go:406`）。随后 `INSERT OR IGNORE` 也不重读既有 batch 的完整冻结 identity。
- member 碰撞检查从 version/nonce/Channel snapshot 缩减为只检查 Channel ID/snapshot（`:464-475`）。因此同一 Interrupt 第二次升级命中 `(batch_id,interrupt_id)` 后会把旧 member 当成功，而不是证明本次 version/nonce 已入批。
- sealing 明确只保留 `i.version=m.interrupt_version AND i.nonce=m.nonce` 的 member（`internal/storage/channel_batch.go:44`）；上述“成功” member 随即被排除。故 batch snapshot 与 repeated-fused 均存在直接反例。
- 没有新增 exact sealing/payload digest、碰撞、restart 或 replay 测试。

### P1-2 critical fuse / admission：**NO（部分实现）**

- 0024 增加 episode identity，due path 也会调用 `openCriticalSuccessorTx`；successor 不再因旧 identity 唯一键而必然碰撞。
- 但 successor 的 window 来自该 scope **全部历史 admitted evidence 中最早一行**的 Interrupt（`internal/storage/channel_batch.go:137-143`），不是被重裁决 episode 的冻结配置或当前仍计数的最早 evidence；limit 又取窗口内各 Interrupt limit 的 `MIN`（`:148-152`）。配置变化后可用错误 window/limit 决定 successor 与 due。
- successor 只有 `INSERT OR IGNORE`，不重读验证碰撞行的 frozen bytes（`:165`），也没有 crash/replay/并发测试。
- 初发 critical 仍在 `chargeAttentionTx` 直接返回空 attention entry（`internal/storage/interrupt.go:1050-1052`）；quota CAS 竞争重读等 #535 继承项未改。

### P1-3 migration / config wiring / schema：**NO（专项条件部分关闭）**

- `applyOne` 现在在 0021 事务外关闭 FK，且 write pool 固定单连接；这修正了前次指出的“事务内 pragma 无效”机制。0024 也恢复了 `interrupts_close_write_once`。
- 但实现没有新增 populated FK-linked 0020→0021 regression test，无法由测试证明真实历史升级、失败回滚与重启后 FK 状态。
- 0021 重建还 DROP 了 0018 的 `interrupts_nonce_issued_required_insert` 与 `interrupts_startup_stall_max_reject_insert`（`0021_advance_interrupt_schema_forward.sql:9-10`）；0024 只恢复 close-write-once，没有恢复这两个 forward invariant。fresh schema 同样经过 0021，因此当前最终 schema也缺失它们。
- production startup_stall reason config 与 0013 历史冻结值问题未改。
- 0024 编号唯一；当前 0001–0024 连续，无重复版本。

### P1-4 多次升级与 low/normal summary：**NO**

第二次 fused 升级的具体序列是：Interrupt CAS 轮换到新 version/nonce；member `INSERT OR IGNORE` 命中旧行；缩减后的检查返回成功；到期 sealing 因旧 version/nonce 不匹配而排除该 member。该路径会丢失 fused summary，而不是合法收敛。#539 也没有增加 count 0/1/2、downgrade 后 low/normal、summary before/after expiry、restart 或 repeated-fused 测试。

### P1-5 canonical auto-reject variants / full effect-binding：**NO**

新增 validator 会检查 schema、canonical bytes、digest、未知/缺失字段；但其 union 不是 storage §6.4 的契约：

- `guardrail_violation` 缺少必填 `head_sha`；
- `code_review` 错加 `run_id`，缺少 `review_policy_snapshot_digest`；
- `agent_blocked` 错加 `report_id`；
- `merge_conflict` 错加 `run_id`（`internal/storage/advance_interrupt.go:182-185`）。

因此当前 writer/validator 接受非规格 binding，并拒绝规格合法的 code-review/merge-conflict binding。validator 还不验证 Change/head、attempt/run/generation、report exhaustion/security event 的组合 FK，不校验 reason 对应 options，整数也没有正值约束。没有新增 corrupt/unknown/extra、错 schema/digest、每 arm required/null、cross-reason/FK/options、回滚/replay测试。所谓 full effect-binding 不能核销。

## 3. migration 0024 专项结论

- migration 从 0024 起：**YES**。
- migration 0024 无重复：**YES**；0001–0024 连续且每个版本唯一。
- 0021 FK pragma 时机：**代码机制已修正**。
- `interrupts_close_write_once`：**YES，0024 已恢复**。
- populated FK-linked 0020→0021 验收：**NO（无回归测试，且最终 schema仍丢失两个 0018 trigger）**。
- migration/schema P1 整体关闭：**NO**。

## 4. 回归与执行证据

- 从检测到的 GitHub forge 获取并阅读 #547 全文、Agent 建议、关闭条件与 comments（无评论），并读取 #535、#539、PR #544 与实现 diff。
- `git diff 3729db6..1703ea9 --check`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./internal/storage`：**通过**。
- `go test ./...`：**通过**。
- 0001 SHA-256：`9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`，发布 bytes 未漂移。
- migration 扫描：0001–0024 连续，重复版本为零。

绿灯仍没有覆盖 populated 0020→0021、critical successor、repeated-fused、配置变化或 full closed effect-binding vectors；不能反证上述直接路径。

## 5. Issue #547 验收清单

- [x] 获取并阅读 #547 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #547 comments：**YES（无评论）**
- [x] 对照 #535 FAIL / #539 核验 5×P1：**YES**
- [x] 核验 populated 0021 upgrade 与 close-write-once：**YES；pragma/close trigger 改进 YES，完整验收 NO**
- [x] 核验 critical successor / repeated-fused：**YES；实现结果 NO**
- [x] 核验 full effect-binding：**YES；实现结果 NO**
- [x] 核验 migration 0024 无重复：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #535 的 5 个 P1 全部关闭：**NO（0/5 完整关闭）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO（5）**

## 6. 最终裁决

**FAIL。** #539 不可核销 #535 的五项 P1。下一实现至少须恢复 0021 丢失的全部 forward trigger 并加入 populated upgrade test；使 repeated-fused 保留当前可执行 member 而不是静默留下旧 nonce；以明确冻结配置完成 successor 重裁决和碰撞重读；按 storage §6.4 原样实现每个 binding arm、组合 FK/options 与反例测试，再交由不同代理复审。
