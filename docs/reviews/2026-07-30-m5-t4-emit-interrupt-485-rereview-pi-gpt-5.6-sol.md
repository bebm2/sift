# M5 T4→EmitInterrupt #485 定向复审

## 结论

**FAIL。** #485 新增了显式 `FailureReviewVariant`，并在 `EmitInterrupt` 前把 attempt 与 Report quota 的 attempt/bucket/generation/facts 组合做了闭集校验；Report quota 的独立模板、generation golden 和 no-transition 行为也已有正向测试，Gate provider-disabled fallback trace 已补。这些是有效增量。

但 #479 的 P1-1/P1-4 仍未关闭：variant 只停留在调用命令的枚举和内存校验，规格要求的不可变 command-effect binding 既无表也无事务写入，Report quota 身份没有以 FK 绑定真实 exhaustion 行；新增测试也没有逐字节兑现 §3.6 的完整 attempt/quota input、normal/fallback persisted 对象和 Gate normal/invalid fallback 矩阵。此外，#485 把 fallback `Escape` 的下划线转义删除，直接回退 active interrupt §3.2。

因此不能核销 #485 或 #479；P1-2、T4 单次 sink escaping 机制与 P2 replay 未见回退。

## 评审基线

- Issue：#492（含 comments，无评论）；被评审 Issue：#485（含 comments）
- 前次结论：[`2026-07-29-m5-t4-emit-interrupt-473-rereview-2-pi-gpt-5.6-sol.md`](2026-07-29-m5-t4-emit-interrupt-473-rereview-2-pi-gpt-5.6-sol.md) **FAIL**
- PR：#489，commit `76d2a0ccd517074ec3e05560c9cbdc3ea376f81c`，merge commit `959288b54c63846caa8ddaf3e5d7813ececf4537`
- 当前基线：`dc69cd5a8676bc14cc7232982ade4503d817571b`；T4 定向文件与 #489 merge commit 无后续差异
- 判定基准：`docs/specs/interrupt.md` §3.2、§3.6、§5.1、§7.1；`docs/specs/storage.md` §6.4；`docs/specs/report.md` §5
- PR #489 范围：4 个文件，`+207/-13`

## 阻断项

### P1-1：source discriminator 已闭集校验，但不可变 effect binding 仍不存在

`internal/storage/interrupt.go:228-257` 的 `validateFailureReviewVariant` 已明显优于此前的字段猜测：

- attempt arm 要求 non-nil 且一致的 attempt/generation，并拒绝 quota bucket/security event；
- quota arm 要求 attempt/generation 为空，校验固定 facts、bucket 范围、security-event URI 和 canonical failure digest；
- 缺失/未知 variant 与跨 reason variant 均在模板、generation、T4、admission 和 operation 前拒绝；
- `interruptTemplateFor` 与 `interruptGenerationKeyFor` 均消费该 discriminator。

因此 #479 列出的四个内存命令交叉组合已有 fail-closed 增量。

然而 #479 的关闭条件要求同一 closed source/binding arm 同时决定模板、generation recipe 与后续 effect binding。当前仓库中不存在 `interrupt_command_effect_bindings` 表，也没有任何对应 INSERT；`emitInterrupt` 在创建 `interrupts`、event 和 operation 时没有持久化 variant。quota arm 也只比较调用方提供的 bucket/end/security ID，没有以 `(run_id,daily_bucket_start_ms)` FK/事务查询证明其命中 `report_quota_exhaustions` 的同一行。Interrupt 落库后，`FailureReviewVariant` 随命令消失，后续 Command 无法从不可变事实证明该对象应执行 `failure_review_attempt` 还是 `report_quota_failure_review` 的效果矩阵。

此外，非测试生产调用点只有 `internal/gate/interrupt.go:28` 设置 attempt variant；没有 Report quota 生产调用者设置 quota arm。本次 storage fixture 能直接构造该命令，不等于 Report owner 已接通并绑定真实 exhaustion。

故 P1-1 只能判定 **PARTIAL / NO**。

**关闭条件：** 实现 storage §6.4 的一对一不可变 binding，在 `EmitInterrupt` 五件事事务中写入并以 DB 约束/查询证明 attempt 或 exhaustion 来源；模板、generation、options 与后续 Command effect 必须消费同一 arm。测试须覆盖错误/缺失/额外字段、错 FK、跨 arm、事务回滚与落库后可审计身份。

### P1-4：§3.6 与 Gate 矩阵仍只有部分断言

新增测试覆盖了：

- attempt `acceptInterruptT4` 的目标 brief、options 重排和未知推荐项；
- quota normal brief、固定 generation key、running Run no-transition；
- quota options 重排、添加 retry、错误推荐后的 fallback；
- 四个命令字段交叉组合的 helper 拒绝；
- Gate 的真实 T4 shell provider-disabled fallback trace。

但与 §3.6 和 #479 的明确关闭条件相比仍缺：

1. `TestAcceptInterruptT4AttemptGolden` 只直接调用接纳函数；其 input 没有冻结 `attempt_no`、`fallback_brief`、links，也没有经 Brain canonical serializer 断言规范列出的完整 input/output bytes。它没有把合法 attempt normal output 经 `EmitInterrupt` 落库并逐字节断言 headline/brief/options/links；当前发射测试反而故意返回重排 options，只证明 fallback。
2. attempt 负例缺规范点名的 unknown fragment 及其唯一 fallback persisted bytes，也没有安全事件 links bytes、`recommended_action=hold` vector。
3. quota 正向只断言 brief/key/status；没有逐字节断言规范的 fallback renderer JSON、完整 canonical T4 input/output、persisted headline/options_json/links_json。跨 arm 用例只调用 `validateFailureReviewVariant`，没有证明 `EmitInterrupt` 在任何写入前整体拒绝。
4. Gate 测试只有 provider-disabled fallback；没有 normal provider 与 invalid-output fallback 路径，也没有同时断言 T4 normal/fallback 下 canonical options、severity、generation key 与 Gate calibration/事务身份不变。

故 P1-4 为 **PARTIAL / NO**。

### 新 P1 回退：fallback renderer 不再转义 `_`

#485 将 `internal/storage/interrupt.go:725` 的 `escapeBrief` replacer 中 `"_", "\\_"` 删除。active `interrupt.md` §3.2 明确要求 fallback `Escape` 按序转义下划线；这与此前 T4 sink 是否转义是两个函数、两个边界。现在任一 fallback fact 中的 `_` 会原样进入 Markdown，§3.2 bytes 不再 canonical；仓库也没有下划线回归 vector 捕获它。

**关闭条件：** 恢复 fallback `escapeBrief` 的下划线转义并增加精确测试；不要借此重新引入 T4 fragment 的预转义/双重转义。

## 未回退项

### P1-2：统一生产 T4 接缝 — YES

`cmd/siftd` 仍安装 DB 级唯一 T4 caller；`emitInterrupt` 仍在事务外调用它。#485 没有恢复 caller opt-in。

### P1-3：T4 fragments 单次 sink escaping 机制 — YES

`interruptBriefFragments` 仍向 T4 提供原始冻结值，接纳后只由 `escapeT4Text` 渲染一次；#485 没有恢复 `key=value` shape 或预转义。上面的 `_` 回退发生在 fallback `escapeBrief`，不把它误记为 T4 双重转义回退。

### P2：缺 frozen contract 的 replay fail closed — YES

`internal/replay` 的缺 T4 contract 拒绝逻辑与对应测试仍在，#485 未修改。

## 验证

- `go test ./internal/brain ./internal/storage ./internal/gate ./internal/replay`：**通过**。
- `go test ./internal/storage -run 'TestEmitInterrupt|Test.*T4|Test.*Report.*Quota|Test.*Gate.*Interrupt' -count=10`：**通过**。
- `go test ./...`：**失败**；唯一失败是 `internal/controlplane/TestDoctorBaselineChecksConfiguredDependencies` 的 fixture agent 子进程 `signal: killed`。
- 该 doctor 用例独立 `-count=1` 重跑仍因同一 `signal: killed` 失败。它不是本次 T4/EmitInterrupt 的结论依据；本次 FAIL 由上述可复现的契约和验收缺口独立成立。

## 关闭清单

| #492 / #485 条件 | 结果 |
|---|---|
| P1-1 closed discriminator 隔离模板与 generation | **YES**：命令级校验已拒绝已知交叉组合 |
| P1-1 不可变 source/effect binding | **NO**：无表、无事务写入、无 exhaustion FK 证明 |
| P1-1 Report quota 生产 owner 接线 | **NO**：仅测试直接构造 quota 命令 |
| P1-4 attempt §3.6 完整 canonical/persisted exact golden | **NO** |
| P1-4 Report quota 完整 canonical/persisted exact golden | **NO** |
| P1-4 quota 负向矩阵 | **PARTIAL**：T4 三项与命令四项已测，未覆盖完整写口/FK/回滚矩阵 |
| P1-4 Gate provider-disabled fallback trace | **YES** |
| P1-4 Gate normal + invalid fallback + options/severity/key 不变 | **NO** |
| P1-2 统一生产接缝未回退 | **YES** |
| P1-3 T4 单次 sink escaping 机制未回退 | **YES** |
| P2 replay fail closed 未回退 | **YES** |
| fallback §3.2 `_` canonical escaping 未回退 | **NO** |
| 定向测试全绿 | **YES** |
| 全量测试全绿 | **NO**：doctor fixture `signal: killed` |
| #485 可核销 | **NO** |

**最终：FAIL。**
