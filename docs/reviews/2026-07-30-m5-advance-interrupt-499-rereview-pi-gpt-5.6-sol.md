FAIL

# M5 #499 AdvanceInterrupt after #491 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#491 / PR #496，合入提交 `6c0fbb2`（实现提交 `b1005cb`）
> 评审基线：`main` `6c0fbb2`
> 前次结论：[#487 FAIL](2026-07-30-m5-advance-interrupt-487-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §4、§8–§9](../specs/interrupt.md)、[`storage.md` §6.1–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#487 的 5 个 P1 均有继续实现，但仍没有一项完整关闭。** immediate Channel 路径已改为读取持久 snapshot 并使用升级后的 version/nonce；升级也开始读取持久 fuse/summary 配置，Report quota 增加了 no-transition 分支。然而 daily/critical batch 仍不是冻结规格的 batch 协议，non-critical→critical 会被错误的唯一索引直接拒绝，migration 会覆盖合法历史 snapshot，DST gap 计算错误，effect binding 也只是一个布尔占位且未回填历史行。

本轮遵守“禁止自修自审”：评审者未参与 #491 实现，只新增本报告，不修改被评审代码或规格。

## 2. #487 P1 关闭对账

### P1-1 Channel snapshot / batch：**NO（部分改进）**

正向变化是 `internal/storage/interrupt.go:127-149,350,600` 会冻结所选 Channel 的完整 snapshot，`advance_interrupt.go:151-170` 的 immediate escalation 也复用该 snapshot，并将升级后的 version/nonce 写入 operation 与 delivery。

但 batch 与 payload 闭环仍不成立：

- `advance_interrupt.go:230` 的 daily ID 只有 `daily:<channel>:<due>`，缺 project、zone 和完整 Forge target；critical ID `:236` 也缺 global/per-Run scope、scope ID、episode 与完整 target。不同项目/host/project key/target 可碰撞，且 critical 候选无法按窗口合入唯一 episode。
- `addBatchMemberTx` 接收 `kind`/`due` 却完全不持久化；migration 0013 的 `attention_batches` 没有规格要求的 kind/scope/due/delivery/payload/digest/seal timestamps。仓库也没有 `PrepareAttentionBatch`，故 collecting batch 不会排除 stale member、seal immutable payload、创建 batch delivery/outbox，或在空集时 cancel。
- dispatch 先以旧 `ExpectedVersion` 写 member（`:50`），再把 Interrupt version 加一（`:53`）；成员提交后立即与 Interrupt version 不一致，若实现规格要求的 prepare 重读便会被排除。
- immediate payload（`:165`）只有 headline/brief 拼接，不含 links、canonical options 和带 run ID/nonce 的完整命令，仍不是 interrupt.md §8.1 的确定性 renderer。
- 新增实现没有增加 Channel snapshot、batch identity、member version、atomic rollback 或 sealed replay 的 AdvanceInterrupt 测试。

### P1-2 critical fuse / admission：**NO**

双阈值字段已经持久化，代码也查询 global/per-Run count；但 production critical 路径当前不能满足最基本的 high→critical vector：

- migration 0013:37 建立的是无谓词 `UNIQUE attention_admissions(interrupt_id)`。每个 non-critical 初发已在 `interrupt.go:612-615` 写 `<id>:initial`，随后 `AdvanceInterrupt` 在 `advance_interrupt.go:197` 插入 `<id>:critical` 必然触发 UNIQUE 失败并整笔回滚。规格要求的是 initial 与 critical 各一个 partial unique index。
- 窗口查询使用 `created_at_ms >= now-window AND created_at_ms < now`（`:186-190`）；它错误计入恰在左边界、又排除同毫秒已提交 evidence。规格要求 `created_at_ms > now-window`，同毫秒并发还必须串行化，当前实现可让同毫秒候选共同越过 limit。
- fused 结果没有持久化 global/per-Run scope，判断 `global >= total || local >= perRun` 后也无法审计“global 优先”。critical batch 以本次 fused admission 单独建 ID、due=now，既不是共享 episode，也没有窗口 expiry/re裁决/successor 流程。
- `attention_admissions` 缺 `attention_charge_entry_id`、severity、quota day/timezone；初发 critical 在 `chargeAttentionTx:926-928` 不写规格要求的 critical attention entry，升级 admission也不能复用既有 charge。
- 没有 critical 初发、high→critical、quota_batched→critical、global/per-Run、边界、同毫秒并发、fused episode 或 replay 测试。

### P1-3 migration / config wiring / schema：**NO**

Gate caller已传 daily summary 与 fuse 配置，但全生产 caller wiring 和安全升级仍未关闭：

- 全仓只有 `internal/gate/interrupt.go` 消费 `Attention.ReasonDefaults`、`DailySummaryAt` 和 `CriticalFuse`。例如 `internal/storage/termination.go:51-56` 的生产 startup_stall 发射仍未接入启动期有效配置，只会落到 storage 内建默认值。
- migration 0013:11-23 无条件按当前默认表重写**所有**既有 Interrupt 的 `expires_after_ms/on_max_escalations/base_severity`。从 0012 升级时，#481 已按调用方配置写入的合法冻结值也会被覆盖；这不是仅修复旧 placeholder 的可判定 migration。
- migration 没有为既有有 Channel 的 open Interrupt 回填可恢复 snapshot，也没有为既有 Interrupt 回填 effect binding；升级后 dispatch/auto-reject 会分别失败或失去 Channel delivery。
- 数据库仍只有 `startup_stall/on_expire` CHECK；没有 `startup_stall/on_max_escalations` 双保险。`nonce_issued_at_ms` 仍是 nullable 列，也未增加 NOT NULL 数据库约束。
- 没有从 0010/0012 且包含自定义冻结策略、Channel 与 open auto-reject Interrupt 的数据库升级测试。

### P1-4 多次升级与 low/normal summary：**NO（部分改进）**

`advance_interrupt.go:74-90` 已从领域 base/count 重算 severity，并为升级后的 low/normal 计算下一 summary；这是本项的实质进展。但仍有阻断反例：

- `nextSummary` 直接依赖 `time.Date`。固定 DST gap vector `America/New_York/02:30, now=1772953140000` 返回 `1773037800000`（次日 02:30），而规格要求 gap 后首个有效 instant `1772953200000`（当日 03:00）。fold/gap 规则没有被实现或测试。
- summary 到点后的 daily member冻结旧 version，随后 Interrupt version+1，见 P1-1；因此“重算后合法入批”仍不能通过未来的发送前 CAS。
- critical 第二次升级受 P1-2 的错误唯一索引阻断；fused 路径也没有合法 episode/due。
- 仍没有 count 0/1/2、downgrade 后 low/normal、summary before/after expiry、DST gap/fold、restart vectors。

### P1-5 canonical auto-reject variants：**NO（部分改进）**

新代码会为本版本新建的 Report quota Interrupt 写 `{"no_transition":true}`，`closeExpiredInterrupt` 因而可跳过 Run transition；普通 waiting_human 路径仍经 canonical `transition()`。但该关闭不是规格中的 immutable effect binding：

- migration 0013:39-43 的表只有 `binding_json/created_at_ms`；缺 reason、schema version、digest、closed tagged union 及 reason-specific FK/字段约束。`interrupt.go:630-632` 对所有 reason 只写一个可伪造的布尔对象，不能证明 Report quota arm 或其他 reason owner 的绑定身份。
- migration 没有为既有 Interrupt 回填 binding；`closeExpiredInterrupt:127` 使用 inner join，所以升级前任何 open auto-reject Interrupt 到期都会得到 `sql.ErrNoRows`，既不关闭 Interrupt，也不收敛 Run。
- `json.Unmarshal` 允许未知/额外字段，且普通分支仍只按当前 Run 状态固定为 `waiting_human → failed(hitl_expired)`；没有按 canonical tagged arm 验证 effect。
- 没有 waiting_human transition、Report no-transition、历史 migration、stale Run/version 或 Interrupt CAS 回滚/replay 测试。

## 3. 回归与执行证据

- 已从检测到的 GitHub forge 获取并阅读 #499 全文与 comments（无评论），以及 #487、#491、PR #496 元数据和完整实现 diff。
- `git diff 6284f23..HEAD --check`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./cmd/siftd`：**通过**。
- `go test ./internal/storage ./internal/gate ./internal/config ./cmd/siftd`：**通过**。
- `go test ./...`：**通过**。
- 以实现同构的 `time.Date` 小程序执行 config §3.9 DST vectors：fold vector通过；gap vector得到 `1773037800000`，期望 `1772953200000`，**失败**。

现有测试绿灯不能覆盖上述 production 反例；#491 除 schema version 数字外没有新增 AdvanceInterrupt 专项测试。

## 4. Issue #499 验收清单

- [x] 在检测到的 GitHub forge 获取并阅读 #499 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #499 comments：**YES（无评论）**
- [x] 对照 #487 FAIL、#491、PR #496 与合入 diff 逐项复审：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #487 的 5 个 P1 全部关闭：**NO（0/5 完整关闭；五项均有部分实现）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO（5）**

## 5. 最终裁决

**FAIL。** #491 不可按当前实现核销 #487 的五项 P1。优先修复会直接阻断生产路径的 admission 唯一索引、历史 migration 覆盖/缺 binding、batch member 旧 version 与 DST gap；随后按 storage §6.3/§6.6 完成唯一 batch/episode/sealing 协议、canonical effect binding 和逐生产 caller config snapshot，并由不同代理再次复审。
