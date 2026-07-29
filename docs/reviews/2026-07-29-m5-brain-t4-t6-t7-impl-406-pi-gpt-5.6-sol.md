FAIL

# M5 Brain T4/T6/T7 调用壳首切片（#406）定向核销

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`67b07cadfc3e1a96a93de4df69d88d2c5260106d`（PR #408）
> 评审对象：Issue #406、[`brain.md` §11–§13](../specs/brain.md)、[WBS §5.1](../WBS.md)
> 范围纪律：只出报告；未修改实现、规格或 WBS，未自修

## 1. 结论

**FAIL（3×P1）。**

PR #408 已新增并嵌入 T4/T6/T7 `v1.md` 与生成式 `v1.schema.json`，三个 contract 也能复用统一 `Shell.Call` 的 reserve/provider/closed-decode/retry/trace 路径。定向测试覆盖了各触点的 valid output、两次 invalid output 后 fallback、provider disabled；T7 output 为 closed object，`policy_patch` 等额外执行字段会被拒绝。PR 没有接入 `EmitInterrupt`、Channel、调度生产路径或 T7 审批流，也没有提前勾 WBS 实现项。

但首切片尚未按 active 契约闭合：T4/T6 使用了错误的七种 Interrupt reason；T7 完全缺少 §13.1 input contract/domain validator，测试甚至把只有 `aggregate_key` 的残缺输入送入并 reserve；三触点也没有把 shell 的自由文本失败原因收敛成规格要求的 closed fallback source/reason 与各自确定性 fallback 结果。故不能核销 #406。

## 2. 遗留可执行 P1

### P1-1：T4/T6 reason enum 与 active Interrupt 契约漂移

`internal/brain/t4t6t7.go:19-21` 注册的是：

`design_approval | failure_review | human_input | merge_approval | policy_block | rate_limited | run_stalled`

active [`interrupt.md` §3.1](../specs/interrupt.md#31-固定对象) 的七种 reason 则是：

`design_approval | guardrail_violation | code_review | agent_blocked | merge_conflict | failure_review | startup_stall`

当前 `BuildT4Input`/`BuildT6Input` 会拒绝五种合法生产 reason，并接受五种不存在的旧 reason；现有 fixture 只使用共同存在的 `failure_review`，因此没有暴露漂移。

**关闭条件：** 改为 active 七值唯一来源；为七种 reason 逐项覆盖 T4/T6 input 接纳，并逐项拒绝旧值。不得在 Brain 复制另一份会继续漂移的手写枚举。

### P1-2：T7 §13.1 输入 contract/domain 校验不存在

`internal/brain/t4t6t7.go:217-257` 只有 T7 output 与 `aggregateScope` 的局部校验，没有 `T7Input`/`BuildT7Input`。因此 §13.1 要求的 window↔aggregate key 绑定、global/project↔trace project 绑定、category 集合和排序、certification/replay count 不等式、digest/version 格式、semantic material bounds、全输入 evidence ID 唯一性均没有实现。

`internal/brain/t4t6t7_test.go:81,114` 还直接使用 `{"aggregate_key":"aggregate:v1:global:all:1:2"}` 作为 T7 shell input；这不是合法 §13.1 对象，却会被 reserve 并调用/兜底，证明统一壳前没有该触点所需的 schema/domain gate。

**关闭条件：** 从 §13.1 建立唯一 closed Go input contract 和 canonical builder，闭合上述交叉约束；Shell 定向测试只可使用 builder 产出的完整 input，并补残缺字段、计数越界、scope/project/window 漂移、重复/未排序 evidence 的 reserve 前拒绝测试。

### P1-3：closed fallback source/reason 与触点确定性兜底未注册

T4/T6/T7 contract 都只设置 `ValidateOutput`，没有类似既有 `T3ResultFromCall`/`T5TriageFromCall` 的 terminal result adapter。测试在 `internal/brain/t4t6t7_test.go:87,118` 直接断言 shell 内部自由文本：`attempts exhausted: invalid_output`、`provider_disabled`；没有证明规格要求的六值 closed reason：

`provider_disabled | token_threshold | input_too_large | invalid_output | provider_error | recovery`

也没有可调用的触点结果边界来保证 T4 fallback 返回完整冻结 skeleton、T6 fallback 按 v1 `high` 阈值得到 `immediate|batch + default_channel_id + suggested_downgrade=false`、T7 fallback 明确得到“不创建 draft”，并携带对应 `T4|T6|T7/fallback/v1` source。该缺口不要求接入 `EmitInterrupt`/Channel/审批流，但属于 Issue #406 明列的调用壳 fallback 注册范围。

**关闭条件：** 为三个触点提供 terminal result adapter/closed source union，将所有 shell 原因穷尽映射为六值枚举并生成对应 fallback version；补 valid、invalid、provider-disabled、token/input/provider/recovery 的来源一致性测试，以及 T4/T6/T7 各自确定性 fallback 结果测试。不得把原始 shell 错误字符串暴露给后继消费者。

## 3. 范围与 A7 核对

- Prompt/schema asset：**通过。** 三组文件已嵌入；`genschemas`、schema drift test 与 prompt version hash 已覆盖 T4/T6/T7。
- Output closed decode：**通过。** T4 保持冻结 headline/fragments/options；T6 限定 channel/delivery；T7 unknown 字段被拒绝。
- T7 A7 output 字段边界：**通过本切片标尺。** schema 没有 policy patch、Gate verdict、Interrupt、action 或自动生效字段；`requires_human_approval=true` 和 aggregate scope 有领域后校验。
- 非目标越界：**未发现。** 没有新增 EmitInterrupt/Channel/生产调度/T7 proposal persistence 或审批实现。
- WBS 诚实性：**通过。** [`WBS.md` §5.1](../WBS.md) 全部保持 `[ ]`；未把 prompt/call-shell 首切片冒充 T4/T6/T7 生产接线或 A7 回放门禁。

## 4. 测试证据

- `go test ./internal/brain`：**PASS**
- `go vet ./...`：**PASS**
- `go test ./...`：**FAIL**；并行运行时出现既有时序型失败：controlplane doctor fixture 被 kill、launchworker 等待 control 文件超时、wrapper 未及时发布 identity。
- 上述三项失败分别单独 `-run ... -count=1` 重跑：**全部 PASS**。它们与本切片无代码交集，不是本次 FAIL 的决定性依据。
- PR #408 forge checks：四平台 build、schema drift、vet + test 均为 **SUCCESS**。

## 5. Issue #409 验收清单

| 检查项 | YES/NO | 判断 |
|---|---|---|
| T4/T6/T7 prompt/schema 已嵌入并可交给统一调用壳 | YES | asset、contract、trace 路径存在 |
| valid / invalid→fallback / provider_disabled 有定向测试 | YES | 但 P1-2/P1-3 说明输入与消费者 fallback 关闭标尺不足 |
| T7 output 不携带可执行 policy patch / Gate/HITL 写入口字段 | YES | closed schema + unknown-field rejection |
| 未越界实现 EmitInterrupt/Channel/T7 审批流 | YES | 未发现越界接线 |
| WBS §5.1 仅诚实勾选本切片项 | YES | 本 PR 未改 WBS，相关项均未勾 |
| #406 可核销 | NO | P1-1～P1-3 未关闭 |
