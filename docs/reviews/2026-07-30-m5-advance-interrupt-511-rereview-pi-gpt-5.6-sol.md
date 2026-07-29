FAIL

# M5 #511 AdvanceInterrupt after #503 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#503 / PR #508，合入提交 `6d1a165`（实现提交 `2f61604`）
> 评审基线：`main` `6d1a165`
> 前次结论：[#499 FAIL](2026-07-30-m5-advance-interrupt-499-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §4、§8–§9](../specs/interrupt.md)、[`storage.md` §6.1–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#499 的 5 个 P1 仍无一完整关闭。** #503 修正了 dispatch member 的提交后 version、拆分 initial/critical admission 唯一索引、补充部分 admission 字段和 termination 的 summary/fuse 接线，并修复了固定 DST gap vector；但额度耗尽仍直接拒绝发射，critical 窗口和 episode 仍不合法，历史 migration 仍覆盖冻结策略且 binding 回填不可安全执行，batch renderer/effect binding 也仍不满足 closed contract。

本轮遵守“禁止自修自审”：评审者未参与 #503 实现，只新增本报告，不修改被评审代码或规格。

## 2. #499 P1 关闭对账

### P1-1 Channel snapshot / batch：**NO（部分改进）**

`AdvanceDispatch` 现在先 CAS 到新 version，再用 `ExpectedVersion+1` 写单条 delivery 或 daily member（`internal/storage/advance_interrupt.go:51-63`）；#498 带入的 `PrepareDueAttentionBatches` 也会按 open/version/nonce 排除 stale member并原子 seal。这关闭了 #499 指出的“成员提交即 stale”。

剩余阻断项：

- critical batch ID 仍以本次 fused admission 单独构造，scope 永远写 `global/global`，due 固定为 `now`（`advance_interrupt.go:261-291`）。它没有 per-Run scope、共享 episode、窗口 expiry 重裁决或 successor，因此同 scope 候选不能进入唯一 episode。
- immediate renderer 仍只有 `headline + brief`（`:164-182`）；sealed batch 的 `command_lines` 固定为空且 `rendered_text` 只含 ID/headline（`channel_batch.go:50-88`）。links、canonical options 与当前 run ID/nonce 的完整命令没有进入确定性文本。
- #503 没有新增任何 AdvanceInterrupt batch/snapshot/sealing/replay 测试；现有 `advance_interrupt_test.go` 仍只覆盖一次升级、一次 dispatch 和 startup_stall hold。

### P1-2 critical fuse / admission：**NO**

0016 将错误的无谓词唯一索引拆成 initial 与 critical 两个 partial unique index，high→critical 不再必然撞 initial admission；新增列也能保存 charge、severity、quota day 与 zone。这些是正确改进。

但生产协议仍有直接反例：

- `chargeAttentionTx` 在 non-critical 日额度耗尽时仍返回 `ErrAttentionQuotaExceeded`（`interrupt.go:996-1032`），调用方整笔回滚；规格要求继续创建 Interrupt、写 `quota_batched`、charge=NULL 并入 daily batch。当前代码也始终把 non-critical admission 写成 `quota_charged`。
- critical 计数仍使用 `created_at_ms >= now-window AND created_at_ms < now`（`advance_interrupt.go:198-203`），而冻结边界是 `created_at_ms > now-window`。它错误计入左边界并排除同毫秒已提交 evidence；相同注入毫秒的串行事务仍可共同越过 limit。
- `admitCriticalTx` 在升级前读取旧 severity（`:186-210`），所以 high→critical admission 持久化的是 `high`，不是最终 `critical`。
- fused 行没有持久化命中的 global/per-Run scope；代码以 `global >= total || local >= perRun` 合并判断后始终建 global batch。没有“global 优先、否则 per-Run”的可审计归属。
- critical batch due=now 后直接 seal，没有窗口恢复重裁决、共享 episode或 successor。故 quota_batched→critical、边界、同毫秒并发和 episode vectors 均未闭合。

### P1-3 migration / config wiring / schema：**NO**

migration 文件重编号本身正确：父基线同时含两个 0014；#503 将 `0014_emit_interrupt_bindings.sql` 原样改为 0015，并新增 0016。当前文件严格为 0001–0016、无重复，migration loader 与 schema version 测试通过。

这不等于 migration/config P1 已关闭：

- 危险的 0013 仍无条件按 reason 重写所有历史 `expires_after_ms/on_max_escalations/base_severity`（`0013_advance_interrupt_closure.sql:9-23`）。0016 不能恢复已被覆盖的调用方冻结值，也没有回填历史 Channel snapshot。
- termination 新接入 daily summary 与 critical fuse，但仍未从启动期配置传入 `reason_defaults.startup_stall` 的 expires/on-expire/on-max/max-escalations；`RecordTerminationObservation` 仍依赖 storage 内建 reason 默认值，而非该 Run 的有效配置。
- 历史 binding 回填位于 `ensureChannelSchema`，它持有查询 rows 时在同一个 `MaxOpenConns(1)` pool 内再次 `Exec`（`channel.go:43-59`），存在等待同一连接而卡住升级的路径。即使执行，所有缺失行都伪造为规格不存在的 `{"arm":"run_transition"}`；同一 Run 的多行还产生相同 digest，`INSERT OR IGNORE` 会因 UNIQUE digest 静默留下后续 Interrupt 无 binding。
- `nonce_issued_at_ms` 仍无 NOT NULL 数据库约束，startup_stall 的 on-max 双保险和 migration 自定义冻结值/Channel/binding 升级测试仍缺失。

### P1-4 多次升级与 low/normal summary：**NO（DST gap 已修复）**

新 `nextSummary` 对固定 `America/New_York/02:30` gap vector 返回 `1772953200000`，fold vector也返回第一次 `01:30`；daily member 使用提交后 version。因此 #499 的 DST gap 与旧 member version 两个具体反例已修复。

完整项仍未关闭：

- 已有 `critical_fused` admission 的后续升级复用同一 critical batch ID；`INSERT OR IGNORE` 因 `(batch_id,interrupt_id)` 冲突保留旧 member version/nonce（`advance_interrupt.go:191-194,261-294`）。Interrupt 已轮换到新 version/nonce，sealer随后排除旧 member，留下 `dispatch_state=batched` 却没有可发送成员。
- critical 升级仍受 P1-2 的错误窗口/scope/episode 配方影响；不能证明多次升级后合法收敛。
- 仍没有 count 0/1/2、downgrade 后 low/normal、summary before/after expiry、gap/fold、restart 或 fused 再升级测试。

### P1-5 canonical auto-reject variants：**NO**

新发射会写带 `arm` 的 binding，Report quota arm 也可让 `closeExpiredInterrupt` 跳过 Run transition；这是正向变化。但 frozen closed binding contract 尚未形成：

- 0015 只增加通用列和 UNIQUE digest，没有按 arm 的 CHECK、required/null 字段或组合 FK。Go 写出的若干 arm 也不等于 storage §6.4 的 closed shape（例如 `agent_blocked` 多出 `report_id`；guardrail/code-review 缜密绑定字段不全）。
- `closeExpiredInterrupt` 只解析 `arm` 与旧 `no_transition` 布尔值，接受未知/额外字段，不校验 reason、schema version、digest 或 arm-specific identity（`advance_interrupt.go:134-161`）。
- 历史回填统一写非法 `run_transition` arm，且存在上节所述卡住/UNIQUE-ignore 后仍缺 binding的问题；历史 open auto-reject 因 inner join 仍可能无法关闭。
- #503 没有 waiting_human transition、Report no-transition、历史 migration、corrupt/unknown arm、stale Run/version 或事务回滚/replay 测试。

## 3. migration 0014→0015 专项结论

**YES。** #503 的重编号解决了父基线中 `0014_channel_authority.sql` 与 `0014_emit_interrupt_bindings.sql` 的重复版本：最终为 0014 channel authority、0015 emit bindings、0016 AdvanceInterrupt closure；文件版本连续且唯一。`loadEmbeddedMigrations` 的 duplicate guard和 `TestMigrationRecordedAndIdempotent` 均通过。

该 YES 仅评价编号，不覆盖上述 migration 内容与历史升级语义缺陷。

## 4. 回归与执行证据

- 已从检测到的 GitHub forge 获取并阅读 #511 全文、Agent 建议、关闭条件、约束及 comments（无评论），并读取 #499 FAIL、#503、PR #508 对应合入 diff。
- `git diff 345938c..6d1a165 --check`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./...`：**通过**。
- 独立执行 `nextSummary` 同构程序：config §3.9 gap/fold 固定 vectors 均通过。
- migration 文件扫描：0001–0016 连续，重复版本为零。

现有绿灯没有覆盖上述 quota exhaustion、critical boundary/episode、多次 fused 升级、历史 migration/binding 或 canonical renderer 反例。

## 5. Issue #511 验收清单

- [x] 获取并阅读 #511 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #511 comments：**YES（无评论）**
- [x] 对照 #499 FAIL / #503 逐项复审：**YES**
- [x] migration 0014→0015 重编号正确且无重复版本：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #499 的 5 个 P1 全部关闭：**NO（0/5 完整关闭）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO（5）**

## 6. 最终裁决

**FAIL。** 0014→0015 重编号可单独核销，但 #503 不可核销 #499 的五项 P1。下一实现应优先关闭 quota_batched 发射、严格 critical 窗口与 scope/episode/re裁决、可证明且不会卡住的历史 migration/binding，再完成 canonical Channel renderer和 closed effect binding，并补齐对应 production vectors 后交由不同代理复审。
