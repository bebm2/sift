FAIL

# M5 #469 Channel webhook worker after #456 定向评审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：#456 / PR #466，合入提交 `b76f75a7dff6c45365e45133b2eac3a3abc0f2dc`
> 评审基线：`main` `3f68221`
> 判定基准：wave1 I3、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.5–§6.6](../specs/storage.md#65-batch-delivery-投影)

## 1. 结论

**FAIL。** #456 增加了 `WebhookAdapter`、一个未接线的 `Worker`、delivery/episode 辅助表和 completion 投影，但尚不能形成可运行的 I3 闭环。实现没有任何生产 producer/consumer 接线；storage §6.6 的唯一 batch fixture 会被 adapter 当作未知字段拒绝；默认阈值 `3` 永远不会创建 alert；单条 payload 没有可供当前实现建 alert 的 target；max-attempt terminal reclaim、配置化重试、durable delivery identity、`ps`/`doctor` 投影和安全错误摘要均未闭合。`internal/channelworker` 也没有测试，storage §6.6 exact digest/replay 没有实现证据。

本轮只新增评审报告，不修改被评审代码或规格。遗留 **6 个 P1**，#456 / wave1 I3 关闭条件未满足。

## 2. P1 findings

### P1-1：worker 与 producer 均未接入生产路径

仓库中 `channelworker.Worker`、`WebhookAdapter` 和 `EnqueueChannelPublish` 只有定义，没有 `cmd/siftd`、daemon、scheduler 或 Interrupt/batch 生产路径引用。`Worker.RunOnce` 因而不会被启动，`channel_publish` 也没有领域写口创建者。当前代码即使单元调用可编译，运行中的 Sift 也不会产生或消费 Channel operation，不能称为 wave1 I3 的 outbox worker。

**所需修复：** 将唯一 producer 接到既有 `EmitInterrupt` / `AdvanceInterrupt` / `PrepareAttentionBatch` 事务边界，并由 production scheduler 启动独立 `channel_publish` consumer；用真实生产接线测试证明 commit wake、claim、adapter、complete 闭环，不能只测 helper。

### P1-2：storage §6.6 唯一 exact batch payload 被 closed decoder 拒绝

[`internal/channelworker/webhook.go:31-54`](../../internal/channelworker/webhook.go) 的 closed struct 没有 `forge_alert_target`，但 outbox §10 与 storage §6.6 要求 batch payload 必含该字段；第 66 行启用 `DisallowUnknownFields`，所以 digest 为 `ae3dba…` 的唯一 fixture 会在外部调用前直接返回 `contract_violation`。同时 `members` 仅解为 `[]json.RawMessage`，没有校验 member closed schema、排序、delivery identity 或 sealed batch binding，也未拒绝尾随第二个 JSON value。

**所需修复：** 实现真正的 `interrupt | attention_batch` closed tagged union，逐 arm 校验 required/null/extra 字段和 batch/member identity；直接以 §6.6 exact bytes 做 adapter/replay 测试并复算 `ae3dba…`。

### P1-3：阈值 alert 对默认值永远不创建，单条 alert target 也没有来源

[`internal/storage/channel.go:83-111`](../../internal/storage/channel.go) 在每次失败后计算 `count=old+1`，却仅在 `old == 0 && count >= threshold` 时插 alert。默认 threshold `3` 下第三次失败为 `old=2,count=3`，条件必为 false；episode 会被标为 `alerted`，但 `alert_operation_key` 仍为空，之后也再无创建机会。

此外，当前建 alert 只读取 payload 内的 `forge_alert_target`。outbox §10 的单 Interrupt payload不含该 batch-only字段，而实现也没有从 delivery 持久化的冻结 discussion target 读取，因此单条 operation 即使 threshold 为 `1` 也不会建 alert。两条路径均违反“达到阈值唯一 `forge_alert(channel_failure)`”；虽然 renderer 文本包含固定 `Diagnostics: sift ps; sift doctor`，正常默认路径无法生成它。

**所需修复：** 以持久化 episode 的跨阈值 CAS（`old < threshold && count >= threshold`）创建一次 alert；单条与 batch 分别从各自 durable frozen target 取目标；覆盖 threshold 前后、重启、并发 stale completion、alert 自身失败及 canonical `ba1805…` digest。

### P1-4：claim/complete 未实现 max-attempt terminal 与有效配置语义

[`internal/storage/outbox.go:246-276`](../../internal/storage/outbox.go) 的 expired reclaim 无条件把 attempt count 加一、创建新 lease/attempt；它没有读取 `outbox.max_attempts`，也没有“最后一次 lease expiry 后 failed、无 attempt 4”的分支。普通 retryable completion 同样不会在达到 max attempts 时终结。reclaim 还把 alert threshold 硬编码为 `3`，而 worker 把 backoff 硬编码为 `1000/60000/2`；`rate_limited` 不携带 `RetryAfterMS`。这与 config 的唯一 retry policy、outbox §4/§10 及 storage §6.6 terminal vectors冲突。

**所需修复：** 冻结/传入唯一有效 outbox policy；completion 和 reclaim 都在同一 lease CAS 内裁决 max attempts，terminal 时同步终结 operation/delivery/episode、按阈值建 alert且不创建新 attempt；覆盖 storage §6.6 的 attempt 3 expiry/stale completion vectors及 Retry-After。

### P1-5：delivery/episode durable identity 与查询投影不符合 storage §6.2/§6.5

`ensureChannelSchema` 只创建 batch delivery 与 episode 表，未为既有 `interrupt_deliveries` 增加规格要求的 `delivery_id/channel_id/channel_snapshot_json/interrupt_version/nonce/escalation_no`。[`EnqueueChannelPublish`](../../internal/storage/channel.go) 接受 `deliveryID`，但单条分支完全丢弃它；episode 的 `subject_id` 只来自未绑定的 payload。completion 更新也不检查 delivery 行是否命中，成功时不写 `remote_ref`，batch 成功不推进 `attention_batches` 或 member Ledger delivery。

全仓库没有 Channel episode/delivery 的 `sift ps`/`sift doctor` join。故“已生成/未送达”、delivery identity、attempt/count/error/alert state 无法按 channel.md §5 在重启后查询，且 storage §6.6 的 subject/replay identity 无数据库约束。

**所需修复：** 通过 forward migration 落全规格列、FK/CHECK/唯一约束和 append-only/sealed 约束；创建 operation 时原子建立正确 delivery/episode，complete/reclaim 检查恰好一行并推进全部规定投影；补 `ps`/`doctor` durable join 与重启测试。

### P1-6：sender 错误原样持久化，可能泄漏 endpoint/query credential

[`internal/channelworker/webhook.go:95-97,124-134`](../../internal/channelworker/webhook.go) 将任意 `WebhookSender.Send` 错误直接返回，再以 `err.Error()` 写入 outbox `last_error_summary`、delivery `last_error` 和 episode相关诊断。sender 错误常包含请求 URL；这里没有 outbox 要求的 token、URL query credential、原始响应/stderr 剥离，破坏 adapter-only secret boundary。

**所需修复：** adapter 返回 closed、安全的错误分类与已清洗摘要；不得让 endpoint、query、credential 或原始 body/stderr越过 adapter；加入恶意 URL/query/token 错误的持久化与日志负向测试。

## 3. 关闭条件对账

| #456 / #469 条件 | 结果 | 证据 |
|---|---|---|
| adapter 只消费 closed `channel_publish`；`secret_ref` resolver；无业务写端口 | **NO** | resolver ownership 方向正确，但唯一合法 batch fixture因缺 `forge_alert_target` 被拒；nested member/尾随 JSON 未 closed 校验。 |
| worker claim/complete；delivery delivered/failed；durable episode | **NO** | helper 存在，但无 production 接线；max-attempt reclaim、完整 delivery identity/投影和 batch/Ledger推进缺失。 |
| 达阈值唯一 `forge_alert(channel_failure)`，markdown 含 Diagnostics | **NO** | markdown literal 存在；默认阈值条件永远不 insert，单条 target 缺来源。 |
| storage §6.6 exact vectors，至少 replay 一组 digest | **NO** | 无 Channel 测试；`ae3dba…` fixture不能通过 adapter；`ba1805…` alert默认路径不可达。 |
| 不实现 Command/Report；batch sealing 可最小或注明 follow-up | **YES** | 未引入 Command/Report；但当前 batch 路径连 §6.6最小 fixture也不能消费，不能据此豁免上述 P1。 |
| adapter 不泄露 resolver result/endpoint/credential | **NO** | payload不保存 endpoint，但 sender error 原样持久化可泄漏 URL/query secret。 |
| `ps`/`doctor` 展示 durable delivery/episode/alert 状态 | **NO** | 没有对应查询接线。 |

## 4. 执行证据

- 已从 GitHub forge 读取 #469 全文与 comments（无评论）、#456 全文与两条 comments、PR #466 元数据/完整 diff/checks；PR #466 当时 required checks 全部 SUCCESS，且无 review/comment。
- `git diff 6560000..b76f75a --check`：**通过**。
- `go vet ./internal/channelworker ./internal/storage`：**通过**。
- `go test ./internal/channelworker`：**通过，但显示 `[no test files]`**。
- `go test ./...`：**失败**；当前基线的 `internal/controlplane` doctor timing 与 `internal/storage` 两个 T4 测试失败。失败不由本报告改动引入，也不能替代缺失的 Channel tests。
- 全仓生产引用搜索：`channelworker` 和 `EnqueueChannelPublish` 均只有定义，无调用点。

## 5. Issue #469 验收清单

- [x] 在检测到的 GitHub forge 获取并阅读 #469 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #469 comments：**YES（无评论）**
- [x] 获取并核对 #456、comments、PR #466、合入提交与完整 diff：**YES**
- [x] 对照 wave1 I3、adapter 边界、claim/complete、episode、阈值 alert/Diagnostics、storage §6.6 vectors：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增报告：**YES**
- [ ] #456 / wave1 I3 可核销：**NO**
- [ ] 遗留 P1 为零：**NO（6）**

## 6. 最终裁决

**FAIL。** 需先修复 P1-1 至 P1-6，再由不同代理复审；在此之前不得把 #456 描述为完成 Channel webhook worker 或 wave1 I3。
