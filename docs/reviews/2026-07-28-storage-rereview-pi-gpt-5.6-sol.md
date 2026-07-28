# storage.md 定向复评

> 日期：2026-07-28
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/storage.md`](../specs/storage.md)（基于 commit `7da4041` 的修订候选稿）
> 评审范围：首次评审 S1–S5 的关闭情况、与 active config/DESIGN/WBS 的交叉一致性、SQLite 字段级可实现性

## 1. 结论

**通过。** 首次评审的两项 P1、两项 P2 与 S5 全部关闭；本轮未发现新的阻断项。`storage.md` 可以从 `draft` 转为 `active`，进入其余 M1 spec 编写阶段。

本轮额外检查出的三项实现风险已在本次候选稿中闭合：

1. SQLite rowid 表的组合主键列显式 `NOT NULL`，不依赖 SQLite 的宽松 PRIMARY KEY 行为；
2. `budget_entries` 不再反向引用 Interrupt，由 Interrupt 单向引用不可变 charge，消除插入时循环外键；
3. `gate_cache` 改为 append-only、insert-or-return existing，并用 verdict digest 拒绝同键异值覆盖。

## 2. 首次评审逐项核销

| 项 | 结果 | 复评依据 |
|----|------|----------|
| S1 Brain trace 身份域 | 关闭 | `intake/run/aggregate + subject_key`；T1/T2/T7 的 CHECK、可空 Run/attempt 与唯一键均可实现；DESIGN/WBS 已移除旧 tuple 复制 |
| S2 受限写端口覆盖 | 关闭 | projects、初始 Task Spec、Forge API charge、Interrupt 自动/指令推进、delivery 均有显式写入族；hooks/rate bucket 等新增投影也有归属 |
| S3 hooks 基线 | 关闭 | 基线覆盖 `.git/config` digest、原始/effective hooksPath 与最终目录 digest；来源 attempt、CAS、事件及可选 Run/HITL 原子效果明确 |
| S4 Report burst | 关闭 | 持久整数令牌桶表达 rate + burst；重复 receipt 不耗 token；critical 使用 append-only entry 的真实滑动窗口 |
| S5 字段细节 | 关闭 | schema version 整数化并定名、隔离释放语义、组合 FK、原因枚举、probe 归属、manual Run、close reason、JSONL v1、索引与 payload trigger 均已明确 |

## 3. 写入族覆盖复核

逐表检查 §4–§10 的可变对象：

- daemon boot：`ActivateConfig` / `FinishDaemonBoot`；
- projects：`ActivateConfig` / `UpdateProjectRuntime`；
- hook baseline：`RecordHookBaseline(expectedDigest)`；
- runs/task spec：`TransitionRun`、`SetInitialTaskSpec` 与各复合领域端口；
- attempts/claims/probes：`AdvanceAttempt`、`StartOrAdvanceProbe`、`ApplyRetryProbeResult`、`ResolveAttemptRace`；
- Interrupt/delivery：`EmitInterrupt`、`AdvanceInterrupt`、`ApplyInterruptCommand`、`CompleteOutboxAttempt`；
- forge cursor/receipt：`PersistIntakeBatch`；
- outbox operation/attempt/result：claim/complete 两段；
- budget/rate bucket：Forge、Report、Brain、Interrupt 的唯一收费口；
- Gate cache/certification/calibration：Gate 与人的决定端口。

未发现必须绕过 §11 才能写入的可变表。

## 4. 事务与数据库约束复核

- Run 状态仍只有 transition 路径可写；复合端口只能在同一存储模块内调用该路径。
- Interrupt 首次发射按 generation key 查重后先生成 charge，再插入引用它的 Interrupt；不存在立即外键死锁。
- `startup_stall` retry 成功仍是 ADR-013 的单笔 CAS 事务，关闭原因为 `responded`。
- append-only 表及一次性补全/不可变列的 trigger 边界明确；Gate cache 不再是可覆盖例外。
- T1/T2/T7、Report 跨分钟边界、critical fuse、JSONL round-trip 均已进入 §16 验收。

## 5. 交叉一致性

- config 的 Report 子配额、critical fuse、Brain raw output 上限、certification version、max attempts/max escalations 冻结值均有存储承接点。
- DESIGN §7 与 WBS §1.7 已改为引用 storage/brain spec，不再复制失效的 Brain 调用 tuple。
- WBS V2 已覆盖项目健康、Task Spec、Forge 收费、Interrupt 推进与 delivery 的崩溃注入。

## 6. 非阻断后续

- `specs/outbox.md` 仍需定义各 operation payload、远端证据与错误分类的逐类契约。
- `specs/brain.md` 仍需冻结 subject key 构造、call sequence 分配、T1/T2 schema、超限与兜底协议。
- `specs/control-plane.md` 仍需冻结 socket envelope、capability 与 wrapper 文件协议。

这些内容已有稳定存储承接点，不阻塞 `storage.md` 转 `active`。
