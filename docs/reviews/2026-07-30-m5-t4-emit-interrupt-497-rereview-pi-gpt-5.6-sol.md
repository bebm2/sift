# M5 T4→EmitInterrupt #497 定向复审

## 结论

**FAIL。** #497 恢复了 fallback `_` 转义，并尝试在 `EmitInterrupt` 事务中写 effect binding、按 Report exhaustion 行核对 quota arm；这些是有效方向。但当前 `main` 已因两个 `0014` migration 无法打开任何数据库，所有 storage/Gate/daemon 路径均在测试启动前失败。即使单独看 #497 merge 时的代码，P1-1 仍未关闭：attempt binding 不验证 failed attempt/generation，Report quota 仍无生产 owner，且 security event 被收窄成与真实事件 ID 不兼容的整数。P1-4 的 §3.6 exact golden 与 Gate normal/invalid fallback 矩阵也基本未补。

P1-2、P1-3 与 replay fail-closed 的代码未见直接回退；但当前 migration 冲突使这些运行时回归也无法执行验证。故不能核销 #497 或 #492。

## 评审基线

- Issue：#504（含 comments，无评论）；被评审 Issue：#497（含 comments）
- 前次结论：[`2026-07-30-m5-t4-emit-interrupt-485-rereview-pi-gpt-5.6-sol.md`](2026-07-30-m5-t4-emit-interrupt-485-rereview-pi-gpt-5.6-sol.md) **FAIL**
- PR：#501，commit `1689a277dc90d47b489c4dcfd81ec3817b44f88e`，merge commit `6798ebb`
- 当前基线：`b871b562981a7c7d6e2578d3709f207685a094a6`（`origin/main`）
- 判定基准：`docs/specs/interrupt.md` §3.2、§3.6、§5.1、§7.1；`docs/specs/storage.md` §6.4、§7.5、§12.2.1；`docs/specs/brain.md` §11
- #497 范围：6 个文件，`+110/-13`

## 阻断项

### P1-0：当前基线 migration 版本冲突，数据库不可启动

`internal/storage/migrations/0014_emit_interrupt_bindings.sql` 与后续合入的 `0014_channel_authority.sql` 同为 version 14。`loadEmbeddedMigrations` 明确拒绝重复版本，因此 `storage.Open` 稳定返回：

```text
storage: duplicate embedded migration version 0014
```

这不是 flake：定向测试、Brain/storage/Gate/replay 集合和 `go test ./...` 中所有需要数据库的用例均在 Open 阶段失败。#497 必须在含 #498 的当前 main 上重排 migration 版本并同步 schema version 测试；在此之前实现不可运行。

### P1-1：有 binding 表和 quota 查询，但不可变 source/effect binding 仍不成立

有效增量：

- `emitInterrupt` 现在与 Interrupt、event、operation 同事务插入 `interrupt_command_effect_bindings`；binding 有 schema version、canonical Go JSON digest 和 append-only trigger。
- quota arm 在写 Interrupt 前查询 `(run_id,daily_bucket_start_ms)`，并比较 end、event、digest、generation key。
- fallback `_` 转义已恢复。

但以下关闭条件仍缺失：

1. **attempt FK/status 未验证。** `interruptEffectBinding` 直接把命令字段序列化为 `failure_review_attempt`；写口没有查询 `(run_id,attempt_no,generation)`，更没有要求 attempt `status=failed`。现有正例 fixture 的 attempt 反而是 `phase='pending'`，仍被接受并持久化为 `retry_kind=new_attempt`。这违反 storage §6.4 的组合 FK、failed terminal pair 和不可选择另一 generation 的要求。
2. **Report owner 仍不存在。** 仓库中 `report_quota_exhaustions` 的 INSERT 只出现在测试；没有 `RecordReport` 生产写口执行 storage §12.2.1 的 rate-token/exhaustion 第一事务和专用发射第二事务。测试手工造 exhaustion 不能证明 owner 接通。
3. **security-event 身份类型不可生产。** schema 的 `security_event_id` 是 TEXT FK，真实 `newID()` 是 32 位随机 hex；`InterruptGeneration.SecurityEventID`、查询 Scan 和 link renderer却使用 `int64`/`%032x`。测试只能用人工事件 ID `'1'`。真实非数字 event ID 无法 Scan/绑定，也无法形成规范 `sift://event/<32 lowercase hex>`。
4. **closed union/组合约束不完整。** migration 只给旧表追加 nullable digest 和默认 `reason='unknown'`，没有 CHECK/组合 FK。其他 arms 也与 active §6.4 shape 漂移（例如 code review 缺 `review_policy_snapshot_digest`，guardrail 缺 `head_sha`，agent_blocked 多 `report_id`）；后续 consumer 目前只按 `arm` 判断 quota no-transition，没有按 immutable binding 执行 Command effect。
5. **拒绝/回滚矩阵未测。** 没有错 exhaustion FK/end/event/digest/key、非 system event、attempt wrong run/generation/status、binding INSERT 失败全事务回滚及落库后 digest/closed shape 的断言。

因此 P1-1 为 **PARTIAL / NO**。

### P1-4：§3.6 exact golden 与 Gate 矩阵仍为 PARTIAL

#497 对 T4 测试的唯一实质变化是给 quota fixture 手工插入 exhaustion，并把 fallback brief 的下划线期望改为转义后 bytes。前次缺口仍在：

- attempt normal vector仍只直接调用 `acceptInterruptT4`，没有冻结并逐字节断言 §3.6 的完整 canonical input/output，也没有经 `EmitInterrupt` 逐字节断言 normal persisted headline/brief/options/links；EmitInterrupt attempt 用例仍只覆盖 option 重排后的 fallback。
- attempt 缺 unknown fragment、安全事件 link、`recommended_action=hold` 和三类失败唯一 fallback persisted bytes 的完整矩阵。
- quota normal 只断言 brief/generation/status；没有逐字节断言完整 fallback renderer JSON、canonical T4 input/output、persisted headline/options_json/links_json。
- cross-arm 仍只调 helper；没有证明错误 variant/FK 在 generation、admission、Interrupt、binding、event、operation 任一写入前整体回滚。
- Gate 仍只有 provider-disabled fallback trace；无正常 provider、invalid-output fallback，以及两路 options/severity/generation key/calibration/事务身份不变断言。

因此 P1-4 为 **PARTIAL / NO**，与 Issue 中实现方自述的 PARTIAL 一致，不能按“已有若干正例”核销。

## 未回退项

- **P1-2 统一生产 T4 接缝：代码静态检查 YES。** `cmd/siftd` 仍只安装 DB 级 T4 caller，未恢复 caller opt-in。
- **P1-3 单次 sink escaping：代码静态检查 YES。** T4 fragments 仍为原始冻结值，由 `escapeT4Text` 单次渲染；fallback `_` 转义已恢复。
- **P2 replay fail closed：代码静态检查 YES。** #497 未修改 replay 缺 frozen contract 的拒绝逻辑。
- **运行时回归验证：NO。** 重复 migration version 使上述用例均无法进入领域代码。

## 验证

- `go test ./internal/brain ./internal/storage ./internal/gate ./internal/replay`：**失败**；Brain/storage/Gate 均因 duplicate migration 0014 失败，replay 通过。
- `go test ./internal/storage -run 'TestEmitInterrupt|Test.*T4|Test.*Report.*Quota|Test.*Gate.*Interrupt' -count=10`：**失败**；所有命中用例均在 `storage.Open` 阶段稳定失败。
- `go test ./...`：**失败**；广泛失败的共同根因是 duplicate migration 0014，不是此前 doctor 时序 flake。

## 关闭清单

| #504 / #497 条件 | 结果 |
|---|---|
| 当前 main 可迁移、定向测试可执行 | **NO**：两个 `0014`，DB 无法 Open |
| P1-1 五件事事务内 immutable binding | **PARTIAL**：有同事务 INSERT/append-only，但 closed shape/FK/effect consumer 不完整 |
| P1-1 attempt failed generation FK | **NO**：pending attempt 正例仍被接受 |
| P1-1 exhaustion FK 与真实 security event 身份 | **NO**：仅整数 fixture，真实 hex event ID 不兼容 |
| P1-1 Report quota 生产 owner | **NO**：无生产 INSERT/RecordReport 两事务配方 |
| P1-4 attempt §3.6 完整 exact golden | **NO** |
| P1-4 Report quota 完整 exact golden | **NO** |
| P1-4 写口负向/FK/回滚矩阵 | **NO** |
| P1-4 Gate normal + invalid fallback + 不变身份 | **NO** |
| fallback §3.2 `_` escaping 恢复 | **YES** |
| P1-2/P1-3/P2 静态未回退 | **YES** |
| 定向测试全绿 | **NO** |
| 全量测试全绿 | **NO** |
| #497 可核销 | **NO** |

**最终：FAIL。**
