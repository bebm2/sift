# M5 T4→EmitInterrupt #473 第二次定向复审

## 结论

**FAIL。** #473 修复了 T4 adapter 丢失 `options`、§3.6 fragment shape 与 underscore 过度转义，并新增了 Report quota 的两项展示模板；前次两项稳定失败的 storage 测试也已恢复全绿。但 P1-1 仍没有 closed source variant：模板按 `AttemptNo` 与自由 `failure_class` 分派，generation arm 又按另一组 `Generation` 字段独立分派，交叉污染可以进入错误模板或生成键。P1-4 明列的 attempt / Report quota exact golden、负向矩阵、Gate HITL 定向生产接线与 provider fallback trace 测试仍未交付。

因此 #468 的 P1-1/P1-4 未关闭；P1-2、P1-3 与 P2 replay 未见回退，不能据此核销 #473 或 W-M5-t4-emit-p1-close。

## 评审基线

- Issue：#479（含 comments，无评论）；被评审 Issue：#473（含 comments）
- PR：#477，commit `9045e4b85639c90a42338f23630ca53a4a69b089`，merge commit `d4b6d9ae3651bbe21cbe816086d641ce079cea0b`
- 当前 worktree：`6612bc273d394f482c94a576720ea00ba663ac6c`；其在 #473 后仅有 Channel #474 变更，T4 定向文件与 #473 merge commit 无差异
- 前次报告：[`2026-07-29-m5-t4-emit-interrupt-465-rereview-pi-gpt-5.6-sol.md`](2026-07-29-m5-t4-emit-interrupt-465-rereview-pi-gpt-5.6-sol.md)
- 判定基准：`docs/specs/interrupt.md` §3.2、§3.6、§5.1、§7.1；`docs/specs/report.md` §5；`docs/specs/storage.md` §6.4
- PR #477 范围：3 个文件，`+43/-8`

## 阻断项

### P1-1：Report quota 模板存在，但 source variant 仍非 closed、且分派身份可互相矛盾

`internal/storage/interrupt.go:193-206` 新增了 quota 的 `reject,hold` 模板，`emitInterrupt` 在 `:356` 先取该模板，并在 `:382-384` 以模板 options 校验 `recommended_action`；`acceptInterruptT4` 在 `:565-571` 也逐项校验 output options。这是正确增量。

但 variant 选择仍由调用字段组合隐式猜测，而不是 closed binding/discriminator：

1. `interruptTemplateFor` 在 `cmd.AttemptNo != nil` 时无条件返回 attempt 模板（`:195-196`）。所以一个携带 `failure_class=report_interrupt_quota_exhausted` 的命令只要同时带 attempt，就会获得错误的 `retry,reject,hold` 集合，quota 的 `retry` 污染不会 fail closed。
2. `AttemptNo=nil` 但 `failure_class` 不是 quota 字面量时，`:198-199` 仍返回 attempt 模板，而不是拒绝未知 attempt-less `failure_review`。这允许没有 attempt identity 的对象呈现 attempt actions。
3. generation variant 另由 `Generation.AttemptNo==0 && ReportDailyBucketStartMS>0` 选择（`:699-703,724-732`），没有与上述模板 variant、`cmd.AttemptNo`、bucket end、安全事件或 immutable command-effect binding 做同一 closed 校验。调用者可组合 quota 展示与 attempt generation，或 attempt 展示与 quota generation。
4. 当前 migration/写口中没有 `interrupt_command_effect_bindings` 的实现，新增 `ReportDailyBucketEndMS` 与 `SecurityEventID` 也没有被 `EmitInterrupt` 消费；故无法在 generation key/admission/operation 前证明 `report_quota_failure_review(...)` arm，亦无法兑现 §3.6 所要求的 arm 交叉污染拒绝。

这不是单纯测试证据不足：生产接纳边界本身仍能构造互相矛盾的 source fields。P1-1 因而只能判定 **PARTIAL**。

**关闭条件：** 使用一个 closed source/binding arm 同时决定模板、generation recipe 与后续 effect binding；attempt 与 quota arm 对缺字段、额外字段和交叉字段全部在 T4、generation、admission、operation 前拒绝。至少覆盖 quota class + non-nil attempt、attempt-less non-quota、quota template + attempt generation、attempt template + quota generation。

### P1-4：§3.6 exact vectors、quota matrix 与 Gate HITL 验收仍未交付

PR #477 对测试的全部改动只有 `internal/storage/interrupt_test.go:154-166` 三处 fixture 更新：generic `code_review` 用例从 `key=value` 改为原始 `<b>risk</b>` fragment，补上 output options，并同步期望 brief。它不是 §3.6 的 attempt `failure_review` vector，也没有覆盖 Report quota。

仓库中仍不存在以下定向证据：

- attempt §3.6 canonical input/output、HTML/marker/`/sift` fragments、persisted headline/brief/options/links exact bytes；
- Report quota fallback、合法 `reject,hold` input/output/persisted bytes；重排、加 `retry`、错误推荐、两个 arm 交叉污染；
- options/severity/generation key 在 T4 normal/fallback 下不变；
- Gate HITL 经真实生产 T4 seam 的 normal、provider disabled/invalid fallback 与 trace；`internal/gate/gate_test.go:104-135` 只验证 Gate/Shadow/Interrupt 原子提交，没有安装或断言 T4；
- quota generation key 的 §5.1 fixed vector。

前次两项稳定失败已修好：`TestEmitInterruptT4UsesConfiguredSeamAndEscapesFragmentsOnce` 的 fragment/escaping 期望现在通过，`TestEmitInterruptRejectsBeforeAnyWrite` 也因先执行 `renderInterrupt` 而恢复规范 LF 拒绝码。但“修复已有失败”不能替代 #473 明列的 exact golden、quota matrix 与 Gate 定向测试，故 P1-4 仍为 **NO**。

## 未回退项

### P1-2：统一生产接缝 — YES

`cmd/siftd/main.go:47-50` 仍在生产 DB 安装唯一 T4 caller；`internal/storage/interrupt.go:399-415` 仍在 `BeginTx` 前从 DB seam 调用 T4。#473 没有恢复 caller opt-in，startup recovery 与 Gate 均继续经过同一 `EmitInterrupt` 接缝。

### P1-3：单次 sink escaping — YES

`interruptBriefFragments` 在 `internal/storage/interrupt.go:537-548` 现在向 T4 提供原始 value fragments；§3.6 attempt shape 不再是 `key=value`。接纳后只在 `acceptInterruptT4` 的 `:590-594` 调用 `escapeT4Text`；`:605-606` 也移除了规范不要求的 underscore 转义。未发现预转义回退。

### P2 replay fail closed — YES

`internal/replay/replay.go:116-120` 对缺 frozen contract 的任意 Brain trace继续返回确定性错误；`TestReplayBrainJSONLFailsClosedForT4WithoutContract` 仍在。#473 未修改 replay。

## 验证

- `go test ./internal/brain ./internal/storage ./internal/gate ./internal/replay`：**通过**。
- 同一定向包 `-count=3`：**通过**。
- `go test ./internal/storage -run 'TestEmitInterrupt|Test.*T4|Test.*Report.*Quota|Test.*Gate.*Interrupt' -count=10`：**通过**；该结果也暴露当前并没有命中 Report quota / Gate T4 的具名测试。
- `go test ./...`：**失败**；唯一观察到的失败为既知 `internal/controlplane/TestDoctorBaselineChecksConfiguredDependencies` fixture 子进程被 kill。该用例独立 `-count=1` 重跑通过，不改变本次由明确验收缺口和 source variant 缺陷导致的结论。

## 关闭清单

| #479 / #473 条件 | 结果 |
|---|---|
| P1-1 variant-aware canonical `recommended_action` | **NO**：两套模板已存在，但 discriminator/generation/binding 非同一 closed arm，可交叉污染 |
| P1-4 §3.6 attempt exact golden | **NO** |
| P1-4 Report quota exact golden + 负向矩阵 | **NO** |
| P1-4 Gate HITL 定向生产接线 | **NO** |
| P1-4 provider fallback trace 与 options/severity/key 不变 | **NO** |
| P1-4 前次 deterministic storage 失败修复 | **YES** |
| P1-2 统一生产 T4 接缝未回退 | **YES** |
| P1-3 fragments 单次 escaping 未回退 | **YES** |
| P2 replay fail closed 未回退 | **YES** |
| 定向现有测试全绿 | **YES**：但缺少关闭条件要求的测试 |
| 聚焦 T4/EmitInterrupt、兼容 T6/Channel/AdvanceInterrupt | **YES** |
| #473 可核销 | **NO** |

**最终：FAIL。**
