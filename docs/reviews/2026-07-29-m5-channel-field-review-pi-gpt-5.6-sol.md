FAIL

# channel.md 字段级评审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`368c9d6`（#416 / PR #417 合入）
> 评审对象：[`docs/specs/channel.md`](../specs/channel.md) draft
> 依据：[PRD Channel / Attention 条款](../PRD.md)、[DESIGN §6.4 / §8.7](../DESIGN.md)、active [`interrupt.md`](../specs/interrupt.md)、[`outbox.md`](../specs/outbox.md)、[`storage.md`](../specs/storage.md)、[`config.md`](../specs/config.md)

## 1. 结论

**FAIL（3 个 P1）。**

草案的主方向正确：V0 选择 webhook 适配器；Channel 保持在适配层；至少一次与可见 operation 标识写清；单条和 batch identity 对齐既有 outbox/storage；worker 不改写 sealed batch、不自行收费；升级留在原 Channel；V0 明确排除 TTS、第二 Channel 与 T6 收费口。

但当前契约仍不能作为唯一实现基准：失败 batch 没有可确定的 Forge 告警目标；`secret_ref → target_ref → webhook endpoint` 的秘密解析与 payload 边界互相冲突；失败 episode 的稳定 identity、持久化投影与 `forge_alert` generation 未闭合。三处都直接落在 Issue #418 的必验项，故不能以 notes 放行。

按 Issue 约束，本轮只产出本报告，不修改规格、不自修、不将 [`channel.md`](../specs/channel.md) 转为 `active`。

## 2. P1 发现

### MCH1（P1）：attention batch 失败时没有唯一 Forge 告警目标

草案规定每个失败 operation 只创建一个 `forge_alert`，并称告警发到“该 Run”冻结的 Issue、Change 或 manual discussion target（[`channel.md` §4](../specs/channel.md#4-投递重试与错误收敛)）。这对单条 delivery 可解释，但对 batch 不成立：

- batch identity 是 `<zone/due/scope/channel>`，不是 Run identity；
- [`storage.md` §6.3/§6.6](../specs/storage.md#63-attention_admissionsattention_batches-与成员) 明确允许同一 batch 含多个 Run，exact fixture 就是 `run-a` 与 `run-b`；
- global critical batch 也天然可跨 Run；
- 不同成员可能分别只有 Issue、Change 或 manual discussion target，甚至属于不同项目。

因此实现无法确定“对应 Issue / Change”是哪一个，也无法同时满足“一个 batch operation 只建一个 alert”与“每个受影响 Run 的独立 Forge 兜底可见”。任取第一成员会引入未冻结排序/归属策略并让其他 Run 静默；按成员发多条又违反当前唯一 alert 表述和 key 形态。

**可执行关闭条件：** 在 Channel/outbox/storage 契约中冻结 batch failure 的唯一策略。二选一并给出 closed payload、目标绑定、operation key 和并发/崩溃重放 vector：

1. 按 batch member 的冻结 command/discussion target 各建一个告警，key 纳入 batch delivery 与 member/target identity；或
2. 为 batch 在创建/密封时冻结一个经验证的单一 Forge 运维目标，并禁止无法形成该绑定的 batch。

同时明确跨项目 batch 是否允许；若不允许，project identity 必须进入 batch 分组/唯一 identity，而不能由 worker 临时猜测。

### MCH2（P1）：webhook target 的秘密解析路径与 closed payload 边界冲突

草案同时要求：

- adapter 只接收 closed `channel_publish` payload，不读取当前配置（[`channel.md` §1/§3](../specs/channel.md#1-v0-选择与适配层边界)）；
- webhook target 只从 `secret_ref` 取得，凭据不进入 payload 或 operation digest；
- outbox payload 又必须是 immutable、可在重启后原样重放。

active 交叉规格本身也有冲突：[`config.md` §3.7.1](../specs/config.md#371-attentionchannels) 先说解析值不进入 outbox payload，随后又说 outbox 携带“解析后的 target reference”；[`outbox.md` §10](../specs/outbox.md#10-channel-publish) 与 [`storage.md` §6.6](../specs/storage.md#66-batch-exact-vectorsconfig--interrupt--outbox-共用) 则把 `target_ref`（fixture 为 URL）直接放进 immutable payload/digest。webhook URL 常含鉴权 token，不能在未定义语义时假定它是“非秘密 reference”。

当前文字无法回答 adapter 在只拿 closed payload、且不读当前 config/secret 的情况下如何取得 endpoint；也无法判断重启后 secret rotation 应重放冻结 endpoint、解析冻结 secret name 的当前值，还是 fail closed。不同实现会得到不同安全边界和 operation digest。

**可执行关闭条件：** 在 config/channel/outbox/storage 四处统一冻结一种模型，并更新唯一 exact fixture：

- 若 `target_ref` 是不含秘密的 resolver handle：定义 handle schema、唯一 resolver owner（适配层）、解析时点、重启/rotation 语义、解析失败分类，并明确 adapter 可以只用 payload handle 调 secret resolver；
- 若 payload 冻结解析后的 endpoint：明确其敏感分类、加密/持久化/日志/diagnostic/digest 规则，不能继续宣称 resolved value 或凭据不进 payload。

不得保留“URL fixture + 非秘密 target_ref + resolved value 不进 payload”三种互斥解释。

### MCH3（P1）：failure episode 与唯一告警 key 没有可持久化的精确定义

草案新增“同一连续失败 episode 只创建一个告警”、重启恢复 episode、`ps/doctor` 展示连续失败数和告警 operation 状态，但只给出通用 key：

```text
alert:channel_failure:<subject_id>:<generation>
```

`subject_id` 仍是“实现应使用”delivery identity，`generation` 没有取值/递增规则；[`storage.md` §6.2/§6.5](../specs/storage.md#62-interrupt_deliveries) 只有 `attempt_count/last_error/state`，没有 episode identity、连续失败计数或 alert operation 关联。虽然 immutable attempt results 可重算当前 operation 的失败次数，但草案没有冻结哪些 outcome 打断连续性、成功/terminal 后 episode 如何结束、`max_attempts` 后如何投影，也没有证明并发 Complete/重启会命中同一个 alert key。

这使“阈值只告警一次”“重启恢复”和 `ps/doctor` 所需字段无法得到唯一实现。尤其草案无条件写“原 operation 仍继续重试”，而 active [`outbox.md` §4.1](../specs/outbox.md#41-退避) 允许正数 `max_attempts` 达限后转 `failed`。

**可执行关闭条件：** 冻结 failure episode 的状态机和持久化来源：

- 精确规定连续失败计数包含哪些 attempt result、何时清零/终结；
- 对单条与 batch 分别固定 `subject_id` bytes，并固定 `generation`（若每个 immutable operation 仅有一个 episode，可明确为 `1`；否则定义 durable generation）；
- 指明 delivery/doctor 投影如何关联 alert operation，并给出 threshold 前后、并发 Complete、daemon 重启、告警自身失败及 `max_attempts` 达限 vectors；
- 将“继续重试”限定为 outbox retry policy 尚允许时，terminal operation 必须保持“已生成、未送达”可见。

## 3. 已通过项

| 核验项 | 判断 | 证据 |
|---|---|---|
| 适配层边界 | **YES** | domain/application 不直接 HTTP/凭据；adapter 不改领域状态；renderer 确定性。 |
| 至少一次 + 可见重复识别 | **YES** | 明确无远端查询证据，响应丢失重放同一 immutable operation/payload，worker 追加 `[sift <operation_key>]`。 |
| 已生成 / 已送达分离 | **YES** | delivered 只由 `CompleteOutboxAttempt` 成功写回；pending/retry/held 不伪造送达。 |
| operation / delivery key 对齐 | **YES** | 单条 `interrupt:<id>:publish:<escalation_no>`、batch `attention-batch:<batch_id>:publish:1` 及各 delivery identity 与 active outbox/storage 一致。 |
| sealed Attention payload 不改写 | **YES** | `PrepareAttentionBatch` 是唯一 sealing 入口；worker 不增删成员、不重建 batch、不刷新 nonce/config。 |
| Channel 不自扣配额 | **YES** | 禁止写 admissions/预算或创建、关闭、升级 Interrupt；升级/熔断/合批归既有 Attention 写口。 |
| 升级强提醒 | **YES** | 同一冻结 Channel、新 escalation operation、strong 档位；不支持优先级时原通道重推，不引入第二 Channel。 |
| 配置落点唯一 | **YES（存在 P1 语义冲突）** | 阈值指向 `attention.channel_failure_alert_after`，退避/max attempts 指向 outbox；未另造字段。target 的值/秘密语义见 MCH2。 |
| 非目标清晰 | **YES** | 排除 TTS/`sift speak`、第二/备用 Channel、T6 收费口、批量 Command 与第二配置源。 |

## 4. 验收判断

- 获取并核对 Issue #418 全文、Agent 建议、acceptance checklist、comments 与约束：**YES**
- 对照 PRD Channel / Attention 条款：**YES**
- 对照 DESIGN §6.4 / §8.7：**YES**
- 对照 active interrupt/outbox/storage/config：**YES**
- 适配层边界闭合：**YES**
- 至少一次、可见重复标识、已生成/已送达分离：**YES**
- operation/delivery key 与 active 规格一致：**YES**
- 连续失败 → Forge 告警对单条与 batch 均可唯一实现：**NO**
- target secret 解析、payload 与重启语义唯一：**NO**
- failure episode/key/投影与 `ps`/`doctor` 恢复闭合：**NO**
- 升级强提醒、不改 sealed payload、不自扣配额：**YES**
- 配置无第二来源、非目标清晰：**YES**
- 只产出评审报告，不修改规格、不自修：**YES**
- `channel.md` 保持 `status: draft`：**YES**
- 遗留 P1：**3**
