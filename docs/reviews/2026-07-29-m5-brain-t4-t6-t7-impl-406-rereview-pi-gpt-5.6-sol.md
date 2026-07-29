FAIL

# M5 Brain T4/T6/T7 调用壳（#411）定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`9a2a5f6be7ce0639b078c60f0ce688082ba07930`（PR #412）
> 评审对象：Issue #413、Issue #411 / PR #412、[`brain.md` §11–§13](../specs/brain.md)
> 前序报告：[`#406 实现定向评审`](2026-07-29-m5-brain-t4-t6-t7-impl-406-pi-gpt-5.6-sol.md)
> 范围纪律：只出报告；未修改实现、规格或 WBS，未自修

## 1. 结论

**FAIL（3×P1）。**

PR #412 已取得实质进展：T4/T6 reason 的运行时值改为引用 storage 的七个 active 常量；新增 `T7Input`/`BuildT7Input`，并把 T7 input validator 放到 reserve 之前；shell 持久化的 fallback reason 也已收敛为六值，T6 v1 fallback 结果符合 high 阈值，T7 fallback adapter 不返回 proposal。PR checks 全绿，本地 `go test ./internal/brain` 与 `go vet ./...` 通过。

但 #409 的三个 P1 尚未达到各自关闭条件：reason 的逐值接纳/拒绝测试完全未补；T7 validator 无法把 input identity 绑定到 trace，且 project 输入在 builder 后会因隐藏上下文丢失而被 validator 拒绝；fallback adapters 没有任何定向测试，T4 所谓 fallback output 也不是规格要求的完整冻结 skeleton。故 #411 仍不可核销。

## 2. 剩余可执行 P1

### P1-1：七值已改正，但验收要求的逐值回归测试缺失

`internal/brain/t4t6t7.go:18-29` 的 `InterruptReason.EnumValues` 已引用 `storage.Interrupt*` 常量，运行时枚举不再保存旧字面量；这一实现方向通过。

但 `internal/brain/t4t6t7_test.go` 的 T4/T6 fixture 仍只使用共同值 `failure_review`（第 13、32 行），没有逐项证明：

- T4 与 T6 均接纳 active 七值；
- T4 与 T6 均拒绝五个已删除旧值 `human_input | merge_approval | policy_block | rate_limited | run_stalled`。

这正是 #409 P1-1 和 #411 明列的关闭条件；当前测试无法防止枚举接缝再次漂移。

**关闭条件：** 从 storage 的 canonical reason 集派生表驱动测试，对 T4/T6 逐值接纳全部七值，并逐值拒绝全部已删除旧值；不得在测试或 Brain 再定义一份独立 active 枚举作为期望源。

### P1-2：T7 trace identity 未闭合，project 合法输入不可调用，reserve 前拒绝矩阵缺失

当前实现有两个决定性 contract 缺口：

1. **input aggregate key 没有绑定 trace subject。** `T7Contract` 的 `ValidateInput`（`internal/brain/t4t6t7.go:381-395`）只对 input 自带的 key 调用 `BuildT7Input`；构造参数 `aggregateKey` 仅在 output validator 第 400 行使用。`ValidateInput` 也拿不到 `CallParams.SubjectKey/ProjectID`。因此 input 可使用另一个语法合法的 global aggregate key，并以不同的 trace `subject_key` reserve，违反 §13.1 “必须精确等于 trace subject_key”。
2. **project input 在 builder 与 shell validator 之间必然丢失 trace project。** `BuildT7Input` 在第 284-291 行要求 `TraceProjectID`，但该字段是 `json:"-"`。builder 输出后，`ValidateInput` 第 383 行重新 decode，`TraceProjectID` 变回空值，再调用 builder 就会在第 285-286 行拒绝。因此合法 project T7 input 无法进入 shell；现有测试只覆盖 global。

此外，当前“合法”fixture 本身让 `semantic_material` 保持 nil（`internal/brain/t4t6t7_test.go:20-27`），builder 会产生 `null`，而 §13.1 要求该 required 字段为 0..64 项 array。`all` aggregate 也只验证 1..5 个排序类别，接口没有传入确定性聚合器的预期非空类别集，无法验证“全部非空类别”。

`internal/brain/t4t6t7_test.go` 没有 #411 要求的任何 malformed input → reserve 前拒绝用例：残缺字段、计数越界、scope/project/window 漂移、重复/未排序 evidence 均未测试，也没有断言拒绝后不存在 Brain call/attempt trace。

**关闭条件：** 让 T7 pre-reserve validator 同时获得并校验 trace `subject_key` 与 `project_id`，保证 global/project 均能接受 builder 的合法 canonical bytes；required empty arrays 输出 `[]` 而非 `null`；把 `all` 的预期类别集合纳入确定性 builder/validator 边界。补上述全部负例，并逐例断言未 reserve、未创建 attempt、未调用 provider。

### P1-3：T4 fallback 不是完整冻结 skeleton，adapter/六值结果测试未交付

shell 在 `internal/brain/shell.go:253` 先执行 `fallbackReason`，所以持久化和 `CallResult` 不再暴露 `attempts exhausted: ...` 等自由文本；`T6FallbackOutput` 也确定性地产生 `high|critical → immediate`、`low|normal → batch`、default channel、`suggested_downgrade=false`。这些部分通过。

但 T4 仍未满足关闭条件：

- `T4FallbackOutput`（`internal/brain/t4t6t7_result.go:9-25`）把 fallback 强行投影成模型 `T4Output`：只保留 headline、最多三个 fragments 和 option IDs，并自行选择首 fragment/首 option；它丢失 §11.3 要求直接交给 `EmitInterrupt` 的 `fallback_brief`、原始 facts、verified links 以及完整 `{id,label,effect,risk}` 候选。第 9 行“complete frozen skeleton”的注释与返回类型/内容不符。
- `T4ResultFromCall` 因而返回的是一份伪装成 T4 正常形状的摘要，不是可供消费者无损执行确定性 fallback 的 closed terminal union。
- `internal/brain/t4t6t7_test.go` 没有调用 `T4ResultFromCall`、`T6ResultFromCall` 或 `T7ResultFromCall`；没有 valid/fallback source shape、六种 reason 映射与 call reason 一致性测试，没有四档 T6 确定性矩阵，也没有证明 T7 fallback 的消费者不会创建 draft。

**关闭条件：** 为 T4 fallback 返回无损冻结 skeleton（或让 adapter 明确返回 normal/fallback tagged union，fallback 分支直接携带原始 T4 input skeleton），不得构造半份模型形状；补 T4/T6/T7 adapter 的 valid 与六种 fallback reason 表驱动测试，断言固定 version、logical call identity、无原始错误字符串、T6 四档结果，以及 T7 fallback 的显式 no-proposal/no-draft 结果。

## 3. 已通过项与范围核对

- T4/T6 active reason **运行时值**：YES；七个值来自 storage constants，旧字面量不在实现枚举中。
- T7 基础 domain checks：YES；key grammar、window、具体 category、排序去重、count inequalities、digest、semantic bounds 与 evidence ID 全局唯一性已有实现。
- T7 malformed input gate 时机：YES；`Shell.Call` 在 reserve 前调用 `ValidateInput`，但 P1-2 的 identity/context 与测试闭合仍失败。
- fallback reason 六值收敛：YES；shell terminal result 不再携自由文本原因。
- T6 v1 确定性 fallback：YES（实现）；缺验收矩阵测试。
- T7 fallback adapter 不返回 proposal 字段：YES（实现）；缺显式 consumer/no-draft 测试。
- 非目标越界：未发现；未接入 `EmitInterrupt`、Channel 或 T7 审批流，未改 WBS。

## 4. 测试证据

- `go test ./internal/brain`：**PASS**
- `go vet ./...`：**PASS**
- `go test ./...`：**FAIL**；`TestDoctorBaselineChecksConfiguredDependencies` 的 agent fixture 被 `signal: killed`。
- 上述失败单独执行 `go test ./internal/controlplane -run '^TestDoctorBaselineChecksConfiguredDependencies$' -count=1`：**PASS**；与本次 Brain diff 无代码交集，视为既有并行时序 flake，不改变评审结论。
- PR #412 checks：四平台 build、schema drift、vet + test 均 **PASS**。

## 5. Issue #413 验收清单

| 检查项 | YES/NO | 判断 |
|---|---|---|
| T4/T6 reason 唯一来源为 active interrupt §3.1 七值 | YES | 实现引用 storage 七常量 |
| 七种 reason 接纳 + 旧值逐项拒绝测试 | NO | 两个 fixture 都只覆盖 `failure_review` |
| closed `T7Input` / `BuildT7Input` 与 §13.1 交叉约束 | NO | trace subject 未绑定、project round-trip 失败、required empty array 可为 null、`all` 集合不可核对 |
| 残缺/越界/identity/evidence 负例在 reserve 前拒绝 | NO | pre-reserve hook 存在，但要求的负例与 trace-zero 断言均缺失 |
| T4/T6/T7 closed fallback adapters 与六值 reason | NO | reason 已收敛；T4 fallback 非无损 skeleton，adapter 测试缺失 |
| T6 fallback 结果确定 | YES | 实现符合 v1 high 阈值 |
| T7 fallback 明确不创建 draft | NO | adapter 返回零值，但没有显式 optional/tagged result 与 no-draft consumer 测试 |
| 不暴露原始 shell 错误字符串 | YES | shell finalization 前统一映射为六值 |
| #411 可核销 | NO | P1-1～P1-3 仍未全部关闭 |
