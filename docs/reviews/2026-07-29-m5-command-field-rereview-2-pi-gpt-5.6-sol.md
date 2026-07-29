FAIL

# command.md 字段级第二次定向复审

评审基线：`3343598`，分支 `docs/issue-337-rereview-commandmd-after-luna-p1-closure-329`。本次按 Issue #337 只核销上一份 [`2026-07-29-m5-command-field-rereview-pi-gpt-5.6-sol.md`](2026-07-29-m5-command-field-rereview-pi-gpt-5.6-sol.md) 的 R1–R6，并对照 #329 / PR #333（合入提交 `2a1b6b4`）交叉核对 command、storage、DESIGN、forge、gate 与 ledger；不修改规格。

## 1. 结论

**FAIL（4×P1）。** R1、R2、R5 已关闭，R3 的时间/语法主体与 R6 的 outcome relation 也有实质推进；但 storage 的 Interrupt schema 仍不能直接建表，label cutoff 没有受限写端口，新增 immutable Command 对象没有数据库级不可变保障，Gate re-evaluation 的失败后继仍未冻结，retry event key 仍有两种合法读法。当前不能把 `command.md` 升为 `active`。

## 2. 剩余可执行 P1

| 项 | 对应前轮 | 剩余阻断 | 可执行关闭条件 |
|---|---|---|---|
| N1 | R3 | **Interrupt/cutoff 的 storage contract 仍不可实现。** [`storage.md` §6.1](../specs/storage.md#61-interrupts) 在同一列清单中两次声明 `hold_max_duration_ms`，无法据此生成唯一 schema。command §3.2 与 storage §6.1 又调用 `SetApprovalLabelCutoff(...)`，但 storage §11 声称只暴露所列表中的写入族，却没有该端口，也没有它对 version/nonce/cutoff 的独占 CAS 配方。实现方只能旁路受限端口或自行决定把它塞进哪个 owner。 | 删除重复列；在 storage §11/§12 明确唯一 cutoff 写端口、允许写列、expected version/nonce、空流 sentinel、失败/重放结果和崩溃边界。创建、轮换、全量扫描、旧扫描迟到四条路径必须形成唯一调用图。 |
| N2 | R4 | **reason-tagged binding 尚未覆盖已声明的 closed input。** storage §6.4 把 `failure_review` arm 固定为 `(run_id,attempt_no,generation,retry_kind)`；但 command §4、interrupt §5.1 与 report 允许 Report quota `failure_review` 的 `attempt_no=null`，并要求 binding 改携 gate recheck operation 或 new-attempt recipe。storage arm 没有 nullable variant、head/operation/recipe 字段，反而要求 attempt generation；该合法 Interrupt 无法写入所称 closed schema。 | 将 `failure_review` 拆成穷尽的 attempt/report-quota tagged variants；逐 variant 冻结 required/null 字段、真实 FK/组合 FK、`gate_recheck|new_attempt` 所需 head/operation/attempt recipe，以及与 canonical options 的 CHECK。 |
| N3 | R4 | **effect/operation 的不可变性与后继生命周期仍只停在文字要求。** storage §6.4 新称 `interrupt_command_targets`、`interrupt_command_effect_bindings`、`command_effects` immutable，但 §13 的数据库 append-only trigger 清单没有三者。`gate_re_evaluation` 的 terminal failure 也只写成“必须产生确定性后续结局”，没有冻结 failed/conflict 各自创建哪种 Gate/Interrupt 事实、由哪个写端口在何事务转移 Run；这正是前轮要求禁止实现自行补齐的分支。 | 把三表纳入数据库级 UPDATE/DELETE 禁止规则；为 gate re-evaluation 冻结 closed payload、claim/replay、success/failed/conflict 的逐分支 Run/Interrupt 后继及 `CompleteOutboxAttempt` 原子配方，证明 closed Interrupt 不会留下无界 `waiting_human`。 |
| N4 | R6 | **retry stage idempotency key 仍未唯一派生。** command §6.1 先规定 final key 为 `command:<event_key>:final`，紧接着又说 probe/race facts “append” `:probe-failed|:fact-wins|:decision-wins`；文本没有说明 suffix 是附在 canonical key、initial key 还是 final key，也没有说明这些 fact key 与 `command_event_outcomes.final_event_id` 所指 final CommandEvent 的关系。实现可合法选择 `...:final` 或 `...:final:fact-wins`，不满足前轮“逐分支唯一 key”的关闭条件。`command_event_outcomes` 的一次性 pending→final CAS 也未进入 storage §13 的列级 trigger 保障。 | 给 mapping 每一行列出唯一完整 `events.idempotency_key`，区分 command initial/final 与独立 probe/race fact；明确每个 key 对应的 event type、`final_for_event_id` 和 outcome FK。为 outcome relation 加数据库级仅一次 pending→final trigger/CHECK，并补各 crash point 的唯一查询结果。 |

## 3. R1–R6 对账

| 前轮项 | 本轮判断 | 说明 |
|---|---|---|
| R1 | **关闭** | DESIGN §8.1、forge §1/§7 与 command §2 已统一 source-tagged nullable actor；Command 缺 actor 保留并写 ignored receipt，其他 consumer 丢弃。双平台/缺 actor fixture 要求也已落 command §7 与 forge 验收。 |
| R2 | **关闭** | storage §3/§7.3 已加入 canonical `event_key`、source-aware 唯一约束、旧约束迁移拒绝条件及跨项目/跨 source vectors。 |
| R3 | **未关闭** | 业务时间、sub-ms、裸 CR 与空流 sentinel 已冻结；duplicate schema column 与 cutoff 端口缺口见 N1。 |
| R4 | **未关闭** | effect 表和 operation kind 已出现，gate 链接也已指向真实 §6.4；Report quota binding 与 operation 失败后继仍未闭合，见 N2–N3。 |
| R5 | **关闭** | command/storage/ledger 均把 `ApplyCommandEvent` 定为 Command 唯一 public transaction owner，把 race 与 Ledger 原语标为 private；旧 `ApplyInterruptCommand`/public `ResolveAttemptRace` 已移除。storage §11 把 private primitive 留在表中虽不利落，但文字明确其不可 public 调用，不再形成第二个 Command owner。 |
| R6 | **未关闭** | initial/final outcome relation、receipt 解析和逐 branch event type 已补；stage-specific 完整 key 与一次性 finalization 保障仍不唯一，见 N4。 |

## 4. 已确认未回退

- canonical command identity 覆盖 project/source/remote ID，receipt 不再因跨 source equal ID 碰撞。
- actor nullable 边界已在 DESIGN、forge、command 对齐，且没有把缺 actor 改写成可补全。
- `occurred_at_ms`、整毫秒 duration、bare CR/LF/CRLF 与 empty label stream sentinel 均已有唯一正文结论。
- Gate 的 human review/exemption 链接已指向 storage §6.4；普通 Command 的 public transaction owner 与 private Ledger/race primitive 已一致。
- retry initial/final 不再共用全库唯一 event idempotency key，receipt 已可经 outcome relation定位 pending/final。

## 5. 验收判断

- 对照 R1–R6 核销 #329 / PR #333：**YES**
- 交叉核对 storage / DESIGN / forge / gate / ledger：**YES**
- 只产出评审报告、不改规格：**YES**
- `command.md` 转 `active`：**NO**
- 剩余 P1 清零：**NO（4 项）**
- 允许开始完整 Command 实现：**NO**

## 6. 结论

**FAIL。** #329 / PR #333 已关闭 actor、canonical receipt identity 和 public Command owner 等关键冲突，并显著收敛 R3/R4/R6；但 N1–N4 仍要求实现方在 schema、transaction owner、reason binding、Gate 后继和 replay key 上自行做架构决定。关闭后再做定向复审；此前 [`command.md`](../specs/command.md) 保持 `draft`。
