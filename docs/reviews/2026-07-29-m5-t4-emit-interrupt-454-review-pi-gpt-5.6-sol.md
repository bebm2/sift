# M5 T4→EmitInterrupt 接线（#454 / PR #459）定向评审

## 结论

**FAIL。** PR #459 把 T4 调用放在 `EmitInterrupt` 五件事事务之前，并在 Gate HITL 生产路径注入了统一 Brain shell；持久化的 options、severity 与 generation key 也仍由既有确定性代码生成，没有新增 Interrupt 创建入口。这些方向正确。

但 #454 的关闭条件尚未闭合：fallback 的 canonical action 接纳仍缺失，T4 只在 Gate 调用方 opt-in、现有 `startup_stall` 生产路径不会调用，fragment 会在正常 T4 渲染中二次转义，而且 PR 没有交付 §3.6 attempt / Report quota 任一验收测试。Report quota variant 在当前发射器中也仍不存在。故不能核销 W-M5-t4-emit-interrupt。

## 评审基线

- Issue：#460（含 comments）；被评审 Issue：#454（含 comments）
- PR：#459，commit `956f736cb1e489f0fa8d41d46155e89d3247abab`
- 对照：`docs/specs/interrupt.md` §3.6、§7.1、§9.2；`docs/specs/brain.md` §11；wave1 I1
- 变更范围：5 个 Go 文件，`+131/-1`；没有测试文件

## P1

### P1-1：fallback `recommended_action` 未经过 variant-aware canonical 接纳

`internal/storage/interrupt.go:415-453` 只检查必需事实非空并渲染 `recommended_action`，没有验证它逐字节命中当前 reason/source variant 的 canonical option ID。T4 接纳也只验证模型的 `recommended_option_id`（`:374-399`），没有先约束 fallback fact。

因此调用方可传入 `recommended_action=bogus`：T4 失败时仍会发出不可执行的 fallback 建议；T4 成功时又可用合法模型推荐掩盖非法冻结 fact。这违反 interrupt §3.6 要求的、发生在 generation key/admission/operation 之前的确定性接纳。

**关闭条件：** 在 T4 调用和 generation/admission 之前，按 attempt 与 Report quota source variant 选择 canonical options，并拒绝不命中的 fallback action；覆盖合法值、未知值与两个 variant 交叉混用。

### P1-2：T4 不是 `EmitInterrupt` 生产路径的统一接线，只是可选调用方回调

`EmitInterruptCmd.T4` 是可空字段（`internal/storage/interrupt.go:97-116`），发射器仅在调用方显式设置时调用（`:229-239`）。当前只有 Gate reconciler 设置该字段（`internal/gate/reconciler.go:202-209`）；既有生产 `startup_stall` 路径在 `internal/storage/termination.go:51-56` 直接调用 `EmitInterrupt`，没有 T4。

所以同一个唯一创建端口会因调用者不同而有或没有 T4，不能满足 #454「生产路径在 EmitInterrupt 事务外调用 T4」和 brain §11 的统一消费边界。它也使新增调用者很容易静默绕过 T4。

**关闭条件：** 让生产配置在唯一发射路径统一取得 T4 caller，同时保留 provider 调用在事务外；证明 Gate HITL 与现有 `startup_stall` 生产入口均经过该接线，provider disabled/invalid 时仍发 fallback 并留下 trace。

### P1-3：T4 fragment 被预转义后再次执行 `EscapeT4Text`

`interruptBriefFragments` 先对事实调用 fallback `escapeBrief`（`internal/storage/interrupt.go:361-371`），接纳后的 renderer 又对 conclusion/key points 调用 `escapeT4Text`（`:395-411`）。含 `>`, `-`, `*`, `\\` 等字符的冻结事实会被双重转义，无法得到 interrupt §3.6 的 exact bytes；例如预转义的 `\>` 会再次变为 `\\\>`。

这也偏离 brain §11.1–§11.2 的边界：fragment 应是无 Cc/换行的冻结纯文本，sink renderer 只执行一次 `EscapeT4Text`。

**关闭条件：** 从冻结事实生成未作 Markdown sink 转义的安全 fragment，并只在最终 T4 renderer 转义一次；逐字节覆盖 §3.6 的 HTML/marker/动作文本 vector 及 fallback bytes。

### P1-4：#454 明列的 attempt / Report quota 验收矩阵完全缺失

PR #459 没有修改或新增任何测试文件。现有 `interruptTemplates` 也只提供 attempt `failure_review` 的 `retry,reject,hold` 三项（`internal/storage/interrupt.go:151`），没有 Report quota v1 的独立 `reject,hold` variant。因而无法证明：

- 正常 T4 改善 persisted headline/brief，options 四字段与顺序不变；
- schema/provider/接纳失败写 trace 后精确回退；
- options 重排、未知 fragment、错误 recommended option 的 exact fallback；
- Report quota `reject,hold` 接受且添加 `retry`/重排被拒绝；
- Gate HITL 真实接线可达。

**关闭条件：** 至少按 interrupt §3.6 各交付一组 attempt 与 Report quota exact golden，并补 Gate 生产接线、provider failure/fallback、options/severity/key 不变断言。

## P2 注记

`internal/replay/replay.go:116-124` 在调用方没有 T4 contract 时静默跳过 T4 trace，既不计入 `Records`，也不验证 terminal output。T4 contract 虽依赖冻结 input，但 replay 应从记录的 `input_json` 重建 contract，或明确报 unsupported；当前行为会让 replay 成功却漏验新产生的 T4 trace。该改动不属于 #454 必需范围，修复时应避免用静默跳过掩盖契约缺失。

## 正向确认

- T4 provider 调用位于 `BeginTx` 之前：**YES**。
- 正常结果经 closed decode/domain validator：**YES（但 emitter 前置 action 接纳仍缺）**。
- persisted options 始终取 canonical template，T4 不能改 effect/risk/order：**YES**。
- severity 与 generation key 不取 T4 输出：**YES**。
- 未发现第二个 Interrupt insert/收费/发布入口：**YES**。
- Gate `failure_review` 补入 attempt binding，且 reconciler 注入 T4：**YES**。

## 验证

- `go test ./internal/brain ./internal/storage ./internal/gate ./internal/replay`：**通过**。
- `go test ./...`：**未全绿**；`internal/controlplane/TestDoctorBaselineChecksConfiguredDependencies` 出现外部 fixture 命令被 kill，`internal/launchworker/TestLaunchWorkerWrapperCrashSuite` 出现时序失败。二者不在 PR #459 变更范围，且不能弥补上述定向测试缺失。

## 关闭清单

| #454 条件 | 结果 |
|---|---|
| T4 在五件事事务外调用 | **PARTIAL**：Gate 是，其他 `EmitInterrupt` 生产调用者不是 |
| closed 结果经确定性接纳 | **NO**：fallback action/variant 未接纳，fragment bytes 错误 |
| 失败/schema/接纳失败确定性 fallback + trace | **PARTIAL**：Gate shell 可留 trace；统一生产路径与验收证据缺失 |
| T4 不改 options 效果/severity/generation key | **YES** |
| 无第二发射入口 | **YES** |
| §3.6 attempt golden | **NO** |
| Report quota variant golden | **NO** |
| Gate HITL 生产接线 | **YES（无定向测试）** |
| 不越界实现 Channel/T6/Command/Report 全量 | **YES** |

**最终：FAIL。**
