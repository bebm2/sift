FAIL

# command.md 字段级定向复审

评审基线：`b08eae3`，分支 `docs/issue-321-rereview-commandmd-after-p1-closure-311`。本次按 Issue #321 只复核上一份 [`2026-07-29-m5-command-field-review-pi-gpt-5.6-sol.md`](2026-07-29-m5-command-field-review-pi-gpt-5.6-sol.md) 的 C1–C9 是否由 #311 / PR #317（合入提交 `5f10ee8`）关闭，并核对其改动过的 command/config/forge/gate/ledger/outbox 与权威 storage、interrupt、PRD、DESIGN、WBS 是否形成同一可实现契约。

## 1. 结论

**FAIL（6×P1）。** PR #317 已实质推进 canonical command event key、nullable actor 的 Forge 类型、ABNF、配置快照标签名、远端单调 label position、reason/action 矩阵、私有 Ledger 原语及 ack schema；C5 已关闭，C1/C4 的主体也已补齐。但 PR 没有同步 `storage.md`，且新增的 retry 双事件与“同一 event idempotency key”直接冲突。当前 active 文档同时给出互斥的 actor、唯一键、public transaction port 和 Gate effect 契约，仍不能据此唯一实现。

[`command.md`](../specs/command.md) 必须保持 `draft`，本轮不升 `active`。

## 2. 剩余可执行 P1

| 项 | 对应前轮 | 剩余阻断 | 可执行关闭条件 |
|---|---|---|---|
| R1 | C1 | **actor owner 仍有 active 事实冲突。** [`forge.md` §1/§7](../specs/forge.md) 与 command 要求 Command 候选携 `actor=null` 并写 `ignored_missing_actor` receipt；但 [`DESIGN §8.1`](../DESIGN.md#81-forge-适配层) 仍规定 actor 必填、缺失事件在适配器内丢弃。严格遵守 DESIGN 的实现永远到不了 command 的缺 actor分支。 | 将 DESIGN 的结构理由同步为 source-tagged nullable actor：Command 候选保留 null，其他驱动 consumer 在适配器丢弃；核对 PRD 的“必须解析 actor”不被改写成可补全。冻结 GitHub/GitLab comment/label 缺 actor 从 adapter 到 receipt 的一条端到端 fixture。 |
| R2 | C2 | **canonical identity 没有落进 storage。** [`command.md` §2.2](../specs/command.md#22-receipt-and-candidate-boundary) 要求 receipt 存 `event_key` 且唯一键为 `(project_id,event_kind,forge_event_id)`；[`storage.md` §7.3](../specs/storage.md#73-forge_event_receipts不可变) 没有 `event_key` 列，唯一约束仍是 `(project_id,forge_event_id)`。同项目 comment 与 label stream 的相同 remote ID 会先在 receipt 层碰撞，尚未走到已修正的 ack key。 | 在 storage/migration 中加入格式受约束且唯一的 `event_key`，把 receipt 唯一约束升级为包含 source/event kind 的 canonical identity，并明确旧数据迁移、同 key 异 digest/target 的 contract violation。receipt、`events.idempotency_key`、probe requester 与 ack builder 必须有跨项目/跨 source equal-ID vectors。 |
| R3 | C3/C4/C6 | **closed input、时间/精度和 anti-replay 持久字段仍不可实现。** command 新增 `interrupt_command_targets`、`hold_max_duration_ms`、`approval_label_cutoff_position` 和 `SetApprovalLabelCutoff`，但 storage 的 Interrupt schema/端口/五件事事务均不存在这些对象。`hold` 效果使用未定义的 `occurred_at`，envelope/compiled command 又没有该字段；同时 grammar 接受 `1ns` 等亚毫秒 duration，却只输出 `hold_duration_ms`，未规定拒绝、取整或进位。`ask` 正文只点名 LF/CRLF，`utf8-no-nul` grammar 却也接受裸 CR，vector 的预期结局不唯一。空 label-event 历史也没有“before first event”的非空 cutoff sentinel，初次扫描无法写入可用 cutoff，首次标签批准路径因而不可执行。 | 同步 storage/interrupt：定义 target binding 的真实 FK、hold 上限列、cutoff 列及 nonce 轮换时清空/二阶段 CAS 配方；给 cutoff 定义 nonnegative sentinel 或等价 empty-stream 证据。为 envelope 冻结唯一业务时间来源及 required/null/范围。明确亚毫秒 duration 的唯一结局，并让 ABNF/正文/vector 对裸 CR、LF、CRLF 给出同一逐字节答案。补初次 nonce、空/非空 stream、轮换崩溃与 sub-ms vectors。 |
| R4 | C7 | **reason-specific 矩阵仍缺可提交的 effect schema 与后继生命周期。** [`command.md` §4](../specs/command.md#4-closed-compilation-and-deterministic-effects) 只声称 `interrupt_command_effect_bindings` 是 closed reason-tagged schema，却未列任何列或 JSON arm；storage 无该表。新增“persisted deduplicated Gate re-evaluation operation”也不在 [`storage.md` §8.1](../specs/storage.md#81-outbox_operations) 的 kind、任一内部 operation 表或写端口中。更直接地，[`gate.md` §3/§4](../specs/gate.md#3-判定顺序) 链接 `storage.md §6.4` 的 `human_review_approval`/`command_effects`，而 §6.4 实际是 batch delivery projection，文中没有这些 effect。approve/retry 关闭 Interrupt 后 Run 暂留 `waiting_human` 的区间由谁、以何状态和何崩溃协议结束也未冻结。 | 在唯一 owner spec 中写出 effect binding 的完整 reason-tagged 表/schema、FK/唯一约束、append-only trigger 与创建 owner；为 exemption/review approval/Gate re-evaluation 定义真实持久对象、稳定 key、claim/replay/终态及写端口。逐矩阵行冻结关闭 Interrupt 后的 Run 中间态和 re-evaluation 成败后继，禁止留下无 open Interrupt 的无界 `waiting_human`。修正 gate 的真实链接并补每个 canonical option 的事务/crash vectors。 |
| R5 | C8 | **public transaction owner 仍互相矛盾。** command/ledger 规定普通命令只公开 `ApplyCommandEvent`、内部调用 `recordHumanDecisionTx` 与私有 race primitive；[`storage.md` §11](../specs/storage.md#11-受限写端口) 却仍公开 `ApplyInterruptCommand`、`ResolveAttemptRace`、`RecordHumanDecision`，没有 `ApplyCommandEvent`，§12.7 还把 Interrupt 指令直接列为 `ResolveAttemptRace` 的输入。实现方仍必须在两套互斥端口中自行选 owner。 | 同步 storage §11–§12：每类 command 只保留一个 public transaction port；Ledger append/settlement、normal transition、startup race、receipt/event/probe/effect/ack 全部成为该事务内私有原语。给 normal、startup reject、retry request、retry final/fact-wins 四条调用图和 crash vector，证明各只开一次事务、写一次 HumanDecision。 |
| R6 | C9 | **retry 的 closed event/replay schema 自相冲突。** [`command.md` §1](../specs/command.md#1-不变量与身份) 规定 event `idempotency_key` 使用同一 canonical event key；[`storage.md` §7.1](../specs/storage.md#71-events不可变) 又令该列全库唯一；但 command §6.1 要为同一 retry 写一个 `retry_pending` 初始事件和第二个 final event，二者都带同一 event key。两行无法同时插入。receipt 又是不可变且只链接 initial event，规范未冻结 duplicate 在 pending/final 之间如何查询唯一当前结局。映射表还把 normal action、startup reject、successful probe 的 `applied` event type 写成 `command.accepted` **or** `command.resolved`，没有逐分支唯一值。 | 区分 canonical command identity 与 stage-specific event idempotency key，冻结 initial/final key derivation、真实 FK 和唯一约束；让 receipt 或独立 outcome relation 可确定地解析 pending/final，规定各崩溃点重放结果。把每个 normal、CAS、probe/race 分支映射到唯一 event type/outcome/ack/next_nonce，禁止 `or`。同步 storage schema、operation `created_from_event_id` 与 renderer golden fixtures。 |

## 3. C1–C9 对账

| 前轮项 | 本轮判断 | 说明 |
|---|---|---|
| C1 | **未关闭** | Forge 的 ID/target/nullable actor 已补；active DESIGN 仍要求缺 actor 在 adapter 丢弃，见 R1。 |
| C2 | **未关闭** | canonical key 与 outbox key 已补；receipt schema/唯一约束未同步，见 R2。 |
| C3 | **未关闭** | envelope 已覆盖 receipt 输入；immutable target binding 只存在于 command 文本，storage 无表/FK，见 R3。 |
| C4 | **未关闭** | 对齐空格歧义与主要 Go-duration grammar 已补；持久 hold 上限、业务时间、亚毫秒及裸 CR 仍未闭合，见 R3。 |
| C5 | **关闭** | command/config 均从 Run 的 immutable startup config snapshot 读取规范化 `labels.approved`，默认/非默认及漂移 fixture 已要求。 |
| C6 | **未关闭** | wall-clock 比较已替换为远端单调 position；cutoff 持久/CAS 与 empty-stream 边界未落 storage，见 R3。 |
| C7 | **未关闭** | reason/action 表已穷尽 canonical options；effect binding、Gate re-evaluation 和中间状态仍无实现契约，见 R4。 |
| C8 | **未关闭** | command/ledger 已选择一个 owner；storage 仍公开旧的三个重叠 owner，见 R5。 |
| C9 | **未关闭** | ack/event 字段与大部分 disposition 已补；双事件唯一键、final replay 和 `applied` event type 仍冲突，见 R6。 |

## 4. 已确认未回退

- canonical event key 已覆盖 project/source/remote ID，outbox ack key 不再使用裸 remote ID。
- Forge 已提供 comment/label 稳定 ID、精确 target、nullable actor 与可空 remote monotonic position；`ObservedAt` 明确只作诊断。
- approved label 改为读取冻结配置，旧 nonce 禁止回显而新签发 nonce 可渲染为完整命令。
- `startup_stall` retry request/result 分离、隔离与事实优先原则仍与 ADR-013 一致。
- reason/action 矩阵没有开放 Interrupt 未列出的动作；C5 可维持关闭。

## 5. 验收判断

- `command.md` 转 `active`：**NO**
- 剩余 P1：**6**
- 评审报告入库：**YES**
- 允许开始 Command 实现：**NO；parser 可继续做不依赖上述争议字段的 test harness，但 storage/effect/replay 事实标准不得由实现私自补齐**

## 6. 结论

**FAIL。** #311 / PR #317 不是空修订：C5 已关闭，C1–C9 中多项主体已明显收敛；但最关键的 storage 同步没有发生，新增 Gate effect 还引用了错误章节，retry 双事件则违反现有全局唯一 idempotency key。关闭 R1–R6 并再次定向复审前，`docs/specs/command.md` 保持 `draft`。
