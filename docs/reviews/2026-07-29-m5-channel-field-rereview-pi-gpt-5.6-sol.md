FAIL

# channel.md MCH1–MCH3 定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 原评审：[`2026-07-29-m5-channel-field-review-pi-gpt-5.6-sol.md`](2026-07-29-m5-channel-field-review-pi-gpt-5.6-sol.md)
> 核销对象：#420 / PR #421
> 评审基线：`main` `60ec9f2`
> 评审对象：[`docs/specs/channel.md`](../specs/channel.md) draft；交叉核对 active [`config.md`](../specs/config.md)、[`outbox.md`](../specs/outbox.md)、[`storage.md`](../specs/storage.md) 与 [`interrupt.md`](../specs/interrupt.md)

## 1. 结论

**FAIL（3 个 P1）。**

#420 已把 MCH2 的 target 模型统一为非秘密 `secret_ref:<name>` resolver handle；唯一 resolver owner、每 attempt 解析、rotation、解析失败分类、payload/digest/日志边界和 exact batch fixture 互相一致，fixture digest `69e429…105` 也可由正文 bytes 复算。MCH2 可核销。

但 MCH1 与 MCH3 尚未闭合：batch key 没有编码其声称冻结的完整 Forge target，且 active `interrupt.md` 仍规定旧 batch identity；所谓 closed `forge_alert` payload 缺少共用 schema 必填的 `markdown`；`lease_expired` 又被计入 episode，却只有不会执行 reclaim 的 `CompleteOutboxAttempt` 获准更新 episode。三处都让实现无法从当前规格得到唯一结果，不能降为 notes。

按 Issue 约束，本轮只新增本报告，不修改规格、不自修；[`channel.md`](../specs/channel.md) 保持 `status: draft`。

## 2. P1 发现

### MCH1-A（P1）：batch identity 没有唯一绑定完整 Forge target，active interrupt 契约仍是旧键

[`channel.md` §3](../specs/channel.md#3-attention-接缝与-batch-forge-告警目标) 要求 batch 成员的 `forge_kind/forge_host/forge_project_key/target_kind/target_id` 全部逐字节相同；[`storage.md` §6.3](../specs/storage.md#63-attention_admissionsattention_batches-与成员) 也把五个字段冻结到 batch。但 daily key 只包含：

```text
daily:<project_id>:<zone>:<due_at_ms>:<channel_id>:<target_kind>:<base64url(target_id)>
```

它没有编码 `forge_kind`、`forge_host`、`forge_project_key`。因此同一 `project_id/zone/due/channel/target_kind/target_id` 下，两个冻结 Run snapshot 若 Forge host 或 project key 不同，会争用同一 PK；哪一个 target 先占用 batch、另一个走何种 held/单条路径取决于并发顺序。`storage.md` §6.6 只覆盖同 target 并发，并以一句“不同 target 不入本 batch”带过，没有给该碰撞的第二个稳定 identity 或确定性拒绝结果。这与 [`config.md` §3.9](../specs/config.md#日历与摘要-batch)“不同 target 必为不同 batch”也不相容。

此外 active [`interrupt.md` §8.3](../specs/interrupt.md#83-attention-admissioncritical-熔断与-batch) 仍把 daily key 固定为 `daily:<zone>:<due_at_ms>:<channel_id>`、critical key 固定为旧四/五段形式，其验收仍要求同 zone/due/channel 合批并无条件继续重试。它直接冲突于 #420 新增的 project/target identity 与 `max_attempts` terminal 语义。`storage.md` 虽自称唯一 batch authority，active 规格仍在逐字节定义另一套 key，不能要求实现者自行选择忽略哪一份。

**关闭条件：** 令 batch identity 对完整冻结 target 唯一且无碰撞，或冻结另一条可证明确定性的映射/拒绝规则；增加同 project、同 kind/id、不同 forge host/project key 的并发 exact vector；同步消除 active `interrupt.md` 的旧 key、旧合批验收与无条件重试表述。

### MCH1-B（P1）：failure alert 的“closed payload”缺少必填正文

[`storage.md` §6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors) 将以下对象称为唯一 closed alert payload：

```json
{"forge_host":"github.com","forge_kind":"github","forge_project_key":"owner/project-a","purpose":"channel_failure","target_id":"42","target_kind":"issue"}
```

但 active [`outbox.md` §5.1](../specs/outbox.md#51-payload) 规定 `forge_comment`、`command_ack`、`forge_alert` 共用 body，包含 `markdown`；worker 只追加 marker，不会替调用方补造评论语义。同时 [`channel.md` §4](../specs/channel.md#4-重试failure-episode-与-forge-告警) 要求告警评论至少包含 Channel operation key、episode/generation、连续失败数、最近安全错误分类和 retry/terminal 状态。现有 payload 一个字段都未承载，既不符合共用 schema，也无法生成规定正文，因而不是可执行的 closed payload。

**关闭条件：** 给单条和 batch 冻结同一 closed `forge_alert` body 规则，并给至少一个含 canonical `markdown` 的 exact payload/digest；正文必须从持久化 operation/episode 安全字段确定性生成，且仍满足凭据与 endpoint 剥离规则。

### MCH3（P1）：`lease_expired` 的计数写者与 outbox reclaim 事务互斥

[`channel.md` §4](../specs/channel.md#4-重试failure-episode-与-forge-告警)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish) 都要求 reclaim 写入的 `lease_expired` result 增加 `consecutive_failures`；[`storage.md` §6.5.1](../specs/storage.md#651-channel_failure_episodes) 又规定只有 `CompleteOutboxAttempt` 可以创建或更新 episode。

然而 active [`outbox.md` §4](../specs/outbox.md#4-worker-状态机) 明确要求 `lease_expired` result 在**新 worker 的 claim/reclaim CAS 事务**中插入；旧 worker 随即失去 complete 权，reclaim 本身也不调用 `CompleteOutboxAttempt`。所以合法 reclaim 后没有被授权的事务能把该 result 计入 episode。若 claim 直接更新 episode，就违反“只有 Complete”与 storage 权威；若不更新，就违反 Channel/outbox §10 的连续失败算法。重启或反复 lease expiry 时阈值与 alert 时点会因实现选择而漂移。现有 §6.6 vectors 没有覆盖 reclaim，无法仲裁。

**关闭条件：** 冻结唯一写端口：要么授权 reclaim CAS 同事务更新 episode/阈值告警，要么定义不破坏 immutable result 与 lease CAS 的另一原子协议；同步 channel/outbox/storage 的 sole-writer 文字，并增加 threshold 前后 lease expiry、reclaim 后重启及并发 stale completion exact vectors。

## 3. MCH1–MCH3 核销表

| 项 | 判断 | 说明 |
|---|---|---|
| MCH1 batch Forge 告警目标唯一策略 | **NO** | 策略方向已选择，但完整 target 与 batch key 不一一对应，active interrupt 仍是旧键；closed alert payload 也不可执行。 |
| MCH2 secret/target/endpoint 唯一模型 | **YES** | `secret_ref:<name>` handle、resolver owner、rotation、错误分类与安全边界一致；URL fixture 已移除，digest 可复算。 |
| MCH3 failure episode 状态机/持久化/投影 vectors | **NO** | subject/generation、terminal 与多数 vectors 已补，但 reclaim result 没有合法 episode 写者。 |

## 4. 验收检查清单

- [x] 在检测到的 GitHub forge 获取并阅读 #422 全文、Agent 建议、范围、依赖与约束
- [x] 获取并阅读 #422 comments（当前无评论）
- [x] 获取并核对 #420 全文、评论及 PR #421 合入基线
- [x] 对照原评审 MCH1–MCH3 逐项复审
- [x] 交叉核对 config/outbox/storage，并检查其引用的 active interrupt 权威
- [x] 复算唯一 Channel batch exact fixture SHA-256
- [x] batch 告警目标、payload、operation key 与并发重放唯一（**NO**）
- [x] secret_ref/target_ref/endpoint 模型及 exact fixture 唯一（**YES**）
- [x] failure episode 状态机、持久化、投影与 vectors 闭合（**NO**）
- [x] 只新增评审报告，未修改规格或实现（**YES**）
- [x] 报告首行为 `FAIL`（**YES**）
- [x] `channel.md` 保持 `status: draft`（**YES**）
- [x] 遗留 P1：**3**
