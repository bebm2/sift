FAIL

# M5 #523 AdvanceInterrupt after #515 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#515 / PR #520，合入提交 `96210a7`（实现提交 `91031b6`）
> 评审基线：`main` `96210a7`
> 前次结论：[#511 FAIL](2026-07-30-m5-advance-interrupt-511-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §4、§8–§9](../specs/interrupt.md)、[`storage.md` §6.1–§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#511 的 5 个 P1 仍无一完整关闭。** #515 增加了 non-critical quota-batched 新库路径、修正 critical admission 的边界/最终 severity/scope，补充单条与 batch 的命令渲染，并避免在持有历史 binding 查询 rows 时复用单连接；但 critical episode 到期没有重裁决或 successor，且 fused 重推会直接失败。更严重的是实现修改了已发布的 `0001_initial_schema.sql`：任何已有数据库都会因 migration checksum drift 拒绝启动，而真正从旧 0001 升级的表仍保留 `charged_budget_entry_id NOT NULL`，无法写 quota-batched 的 NULL charge。

本轮遵守“禁止自修自审”：评审者未参与 #515 实现，只新增本报告，不修改被评审代码或规格。

## 2. #511 P1 关闭对账

### P1-1 Channel snapshot / batch：**NO（部分改进）**

单条与 batch 现在共用 `renderChannelInterrupt`，会输出 links、canonical option 文案和携带当前 Run/nonce 的命令；member 仍以提交后 version/nonce 入批。这是正向改进。

剩余阻断项：

- critical episode contract 未实现。`PrepareDueAttentionBatches` 对所有到期 batch 直接 seal/cancel（`internal/storage/channel_batch.go:10-107`），没有在事务内按窗口重裁决、保持旧 batch、创建 successor episode或转移候选。
- schema 没有规格要求的 `episode_admission_id`；`attention_batches_identity` 也不含 episode（`internal/storage/channel.go:40`）。旧 episode seal 后，同 scope/Channel/target 的 successor 会撞唯一索引，不能建立新 episode。
- 共享 batch 查询只找任意同 scope 的 collecting batch（`internal/storage/advance_interrupt.go:307`），没有证明它属于当前有效 episode；到期并发与 successor 均无闭环。
- batch renderer 改成以空行连接成员（`channel_batch.go:112-121`），与 storage §6.6 固定 fixture 的 `；`、canonical payload digest 不一致；新增测试只直调 renderer，没有执行 exact batch sealing/replay fixture。
- 入批没有断言 member Run 的 project/冻结 target 与 batch 相等；`INSERT OR IGNORE` 也没有重读并验证既有 batch/member bytes，碰撞可被当作成功（`advance_interrupt.go:336-363`）。

### P1-2 critical fuse / admission：**NO（部分改进）**

窗口查询已改为左开边界并计入同毫秒已提交 evidence，high→critical admission 保存最终 `critical`，同时命中时 global 优先；这些关闭了 #511 的三个具体反例。

仍未关闭：

- **critical episode successor 仍为 NO。** 代码只以当前最早 admitted evidence 计算一次 due，然后由通用 sealer直接 seal；没有到期重裁决、仍饱和 successor 或“低于 limit 后新 candidate 才开新 episode”的状态机。
- 已有 `critical_fused` admission 的重推从 `admitCriticalTx` 返回空 scope（`advance_interrupt.go:202-207`）；调用方拼成 `<admission>:`，随后 `addCriticalBatchMemberTx` 因 scope 非 `global|run` 拒绝（`:294-299`）。因此多次 fused 升级整笔回滚。
- 初发 critical 的 `chargeAttentionTx` 仍直接返回空 entry（`internal/storage/interrupt.go:1050-1052`），没有写 storage §9.3 要求的 critical attention entry；fused admission 的 attention FK 语义也没有完整证明。
- quota exhaustion 只把一次 CAS 零行直接解释为耗尽（`interrupt.go:1076-1084`），未按 §9.3 在稳定 admission key 下重读 counter并区分竞争/存储失败。
- quota-batched 仅在修改后的 fresh 0001 schema可写；真实历史 schema仍为 `charged_budget_entry_id TEXT NOT NULL`，0018 没有 rebuild/迁移该列，因此升级库插入 NULL 会失败。

### P1-3 migration / config wiring / schema：**NO（新增 P1 回归）**

0018 文件内容与编号唯一，但 migration 安全未关闭：

- #515 直接修改了已发布 migration `0001_initial_schema.sql`，其 SHA-256 从 `9696d3e1…` 变为 `b433093d…`。`applyMigrations` 明确校验已应用 prefix 的 checksum（`internal/storage/migrate.go:136-154`），所以任何已应用 0001 的数据库在 Open 时返回 `ErrMigrationMismatch`；这是升级阻断。
- 即使绕过 checksum，历史表的 NOT NULL charge 列也不会因修改 embedded 0001 而变化；0018 只补 nonce、改 binding 和加 trigger，没有 rebuild interrupts。新增 quota-batched 测试只使用当前 fresh schema，未覆盖历史升级。
- 0013 对历史冻结策略的无条件覆盖仍无法恢复；0018 所称“preserve historical frozen values”只是不再重写，不能复原已被 0013 损坏的 `expires_after_ms/on_max_escalations/base_severity`。
- production `TerminationCoordinator` 虽传 summary/fuse 配置，但没有 `reason_defaults.startup_stall` 字段；`RecordTerminationObservationCmd` 也没有 expires/on-expire/on-max/max-escalations（`internal/storage/termination.go:20-36`）。startup_stall 仍回落到 storage 默认值，未冻结该 Run 的有效 reason config。
- 0018 为改历史 binding 主动 DROP append-only triggers 并 UPDATE 事实行（`0018_advance_interrupt_final_closure.sql:10-25`），与 storage §6.4/§13 的不可变 binding contract冲突；没有从真实 0017 数据库升级、checksum、定制冻结值、Channel/binding 的测试。

### P1-4 多次升级与 low/normal summary：**NO**

#515 没有补 count 0/1/2、downgrade 后 low/normal、summary before/after expiry、restart 与 episode 测试。除上述历史库 quota-batched 无法写外，已有 fused admission 的下一次 Advance 会因空 scope 失败，critical due 到期也不会重裁决/successor。因此“多次升级后合法收敛”仍有直接反例，不能由现有单次 quota-batched 与 renderer 单元测试核销。

### P1-5 canonical auto-reject variants：**NO（部分改进）**

`closeExpiredInterrupt` 现在要求 arm/run 匹配，并对 Report quota 检查三个字段；历史回填也先关闭 rows，避免单连接自等待，且 digest按 Interrupt 区分。这些是改进。

closed binding contract 仍未形成：

- runtime 只解析 `arm`、`run_id` 和 Report 三字段，不验证 `binding_schema_version`、`binding_digest`、canonical JSON、未知/额外字段或每个 arm 的 required/null identity；普通 arm只检查 `arm == reason`（`internal/storage/advance_interrupt.go:133-161`）。
- 0017 trigger只验证 known reason、schema、digest非空和 arm存在；除两个 failure-review特例外，没有按 arm 的 required/null 字段、reason/options一致性和组合 FK（`0017_emit_interrupt_binding_invariants.sql`）。
- `ensureChannelSchema` 为缺失历史行构造 `{arm:<reason>,legacy_interrupt_id,run_id}`（`internal/storage/channel.go:75-84`），这不是 storage §6.4 的任一 closed arm；运行时却会接受它并执行 auto-reject。
- 0018 原地改写 append-only binding并把 digest设为 `legacy:<interrupt_id>`，不是 `SHA-256(canonical_json(binding_json))`。没有 corrupt/unknown/extra field、错 schema/digest、历史升级、stale Run/version 与回滚/replay测试。

## 3. migration 0018 专项结论

- migration 文件编号：**YES**。当前严格为 0001–0018，`0018_advance_interrupt_final_closure.sql` 只有一份，重复版本为零。
- migration 可安全升级：**NO**。修改 0001 造成 checksum drift；历史 NOT NULL charge schema未迁移；0018 还改写 append-only binding。故“0018 无重复”不能解释为 migration P1 已关闭。

## 4. 回归与执行证据

- 已从检测到的 GitHub forge 获取并阅读 #523 全文、Agent 建议、关闭条件与约束，以及 comments（无评论）；并读取 #511 FAIL、#515 和合入 diff。
- `git diff 91031b6^..91031b6 --check`：**通过**。
- `go vet ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./internal/storage ./internal/gate ./internal/config ./internal/daemon ./cmd/siftd`：**通过**。
- `go test ./...`：**通过**。
- migration 扫描：0001–0018 连续，重复版本为零。
- 旧/新 0001 文件 SHA-256 对比：**不相等**；按生产 migration loader 逻辑会触发 checksum drift。

现有绿灯全部从修改后的 fresh 0001 建库，没有覆盖已应用旧 checksum/schema 的生产升级，也没有覆盖 critical successor、fused 再升级或 closed binding 反例。

## 5. Issue #523 验收清单

- [x] 获取并阅读 #523 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #523 comments：**YES（无评论）**
- [x] 对照 #511 FAIL / #515 逐项复审：**YES**
- [x] 严格核验 critical episode successor：**YES；实现结果 NO**
- [x] migration 0018 无重复版本：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] #511 的 5 个 P1 全部关闭：**NO（0/5 完整关闭）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] 遗留 P1 为零：**NO（5，且 migration checksum drift 是新增升级阻断）**

## 6. 最终裁决

**FAIL。** #515 不可核销 #511 的五项 P1。下一实现必须先恢复已发布 0001 的原始 bytes，并用新的 forward migration把历史 `charged_budget_entry_id` 安全迁为 nullable；随后实现 durable critical episode admission identity、到期事务重裁决与 successor，修复 fused 重推，再按 exact fixture闭合 batch renderer/effect binding及历史升级测试，交由不同代理复审。
