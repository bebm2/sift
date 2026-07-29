FAIL

# M5 Brain T4/T6/T7 调用壳（#415）第二次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`442f09bdf744bb5640a7f917912164a9f8f88d56`（PR #423）
> 评审对象：Issue #424、Issue #415 / PR #423、[`brain.md` §11–§13](../specs/brain.md)
> 前序报告：[`#413 定向复审`](2026-07-29-m5-brain-t4-t6-t7-impl-406-rereview-pi-gpt-5.6-sol.md)
> 范围纪律：只出报告；未修改实现、规格或 WBS，未自修

## 1. 结论

**FAIL（2×P1）。**

PR #423 已关闭 reason 唯一源和 T4 fallback 形状：T4/T6 的 active reason 均从 `storage.ActiveInterruptReasons()` 派生并有七值接纳、五个旧值逐项拒绝测试；T4 fallback 现在保留完整 `T4Input`，T7 fallback 也以 `NoDraft=true` 显式表达不创建 draft。project input 可经 builder→validator round-trip，`semantic_material` 的空值 canonical output 已改为 `[]`。

但 #415 的 P1-2/P1-3 验收证据仍未闭合。实现方自述的 “Full malformed T7 negative matrix: NO” 经独立核实属实：新增测试只有一个 count overflow shell 负例，残缺字段、其余计数边界、scope/project/window 漂移、重复/未排序 evidence 均未形成逐例的 pre-reserve 三零断言。更关键的是，T7 validator 仍未真正获得本次 `CallParams` 的 trace identity：它只比较 input 与调用者另传给 `T7Contract` 的闭包参数，`Shell.Call` 调用 validator 时只传 input bytes，因此合法但不同的 `CallParams.SubjectKey` 仍可与 input aggregate key 漂移后 reserve。

adapter 测试也只覆盖 fallback：没有 T4/T6/T7 valid 分支，T6 的六值 fallback source 被直接丢弃而未断言，三者的固定 version、logical call identity 和不泄露原始错误字符串没有完整证明。故 #415 仍不可核销。

## 2. 剩余可执行 P1

### P1-1：T7 trace identity 仍是调用者自报，完整 malformed pre-reserve 矩阵未交付

`T7Contract` 在 `internal/brain/t4t6t7.go:401-419` 接受独立的 `aggregateKey` / `traceProjectID` 参数，并令 input 与这些闭包值比较；但 `Shell.Call` 在 `internal/brain/shell.go:75-99` 只把 `p.Input` 传给 `ValidateInput`，随后才以 `p.SubjectKey` / `p.ProjectID` reserve。contract 参数并不是 storage 将要持久化的 trace identity。

因此调用者可用 input key A 构造 `T7Contract(A, ...)`，同时把另一个语法合法的 aggregate key B 放入 `CallParams.SubjectKey`；validator 接纳 A，storage 随后 reserve B。project 情形同样可让 contract 的 project 上下文与实际 trace 参数分离。§13.1 要求的是 input `aggregate_key` 精确等于**本次 trace** `subject_key`，不是精确等于调用者第二次提供的副本。

`internal/brain/t4t6t7_issue415_test.go:45-84` 也没有覆盖这一接缝：project drift 只直接调用 `ValidateInput` 并替换闭包参数；唯一 shell 负例只是 `negative_samples > total_samples`。它没有逐例覆盖 #415 明列的：

- required 字段残缺；
- category/replay 各 count 的负数与不等式越界；
- input aggregate key 对 trace subject 的 scope/project/window 漂移；
- concrete/all category 漂移，以及 `all` 的缺类/多类；
- category、semantic 和跨 category/replay/semantic evidence ID 的重复；
- category/semantic 未排序；
- 每个负例均未 reserve、未创建 attempt、未调用 provider。

现有 count overflow 用例通过“随后合法 call 的 `CallSeq == 1`”间接证明该一例未 reserve，并只直接断言 provider 未调用；没有逐例查询 call/attempt trace。`allCategoryKinds` 仍允许为 nil（`t4t6t7.go:309` 仅在非 nil 时校验），所以 `all` 的确定性预期类别集合也可被调用者省略。

**关闭条件：** 让 pre-reserve validator 直接取得本次 `CallParams.SubjectKey/ProjectID`（或在 shell 内以同等强度绑定），禁止另传副本绕过；`all` 必须提供并校验确定性非空类别全集。补齐上述 table-driven malformed 矩阵，并对每一例直接断言 call=0、attempt=0、provider calls=0。

### P1-2：ResultFromCall 验收测试仅覆盖部分 fallback，valid/source 闭包仍缺失

`internal/brain/t4t6t7_result.go` 的实现方向已改正：T4 返回 normal/fallback 分支并在 fallback 携带完整 input；T6 保持四档确定性结果；T7 fallback 返回 `{Proposal:nil, NoDraft:true}`。但 #415 要求的 adapter 测试没有完整交付。

`internal/brain/t4t6t7_issue415_test.go:86-116` 的六值循环存在以下缺口：

1. 完全没有构造 `Status=valid` 的 T4/T6/T7 `CallResult`，所以 normal decode、`brain` source、prompt/schema version 和 logical call identity 均未覆盖；
2. T6 调用在第 101 行丢弃 `BrainSource`，六种 reason、`T6/fallback/v1` 与 logical call identity 均未断言；
3. T4/T7 只断言 reason/version，未断言 `Kind` 与 `LogicalCallID`；
4. 六值表只有 token reason 使用旧内部字符串，未用带自由文本的 `attempts exhausted: ...` / recovery 等输入证明 source 不泄露原始 shell 错误；
5. T4 无损测试只抽查一条 link 与 fallback brief，没有逐字段证明 facts/fragments、完整 `{id,label,effect,risk}` options 和 identity 均保留。

**关闭条件：** 为 T4/T6/T7 分别补 valid adapter 用例；对六种 fallback reason 逐触点断言 closed source 的 kind、logical call ID、固定 fallback version、canonical reason，并用含原始错误详情的 shell reason 证明详情不外泄。保留 T6 四档矩阵与 T7 显式 no-draft，扩充 T4 fallback skeleton 的逐字段无损断言。

## 3. 已通过项

- P1-1 reason：**通过。** `InterruptReason.EnumValues` 从 storage 的 `ActiveInterruptReasons()` 派生；T4/T6 对七个 active 值逐项接纳、五个 retired 值逐项拒绝。
- project builder→validator round-trip：**通过。** hidden `TraceProjectID` 在 validator decode 后重新注入，合法 project canonical bytes 可接纳。
- required empty array：**通过。** nil `semantic_material` 在 canonicalize 前归一为非 null `[]`。
- T4 fallback implementation：**通过。** fallback 不再伪装为半份 `T4Output`，而是保留完整冻结 `T4Input`。
- T6 fallback implementation：**通过。** low/normal→batch，high/critical→immediate，default channel 且不建议降级。
- T7 fallback implementation：**通过。** adapter 明确返回 no-draft，不携 proposal。
- 非目标越界：未发现；未接入 EmitInterrupt/Channel/T7 审批流，未勾选 WBS §5.1。

## 4. 测试证据

- `go test ./internal/brain -count=1`：**PASS**
- `go vet ./...`：**PASS**
- `go test ./... -count=1`：**FAIL**；`TestDoctorBaselineChecksConfiguredDependencies` 的 agent fixture `signal: killed`，并发运行中的 `TestLaunchWorkerWrapperCrashSuite` 未观察到 started marker。
- 两个失败分别单独运行均 **PASS**：
  - `go test ./internal/controlplane -run '^TestDoctorBaselineChecksConfiguredDependencies$' -count=1`
  - `go test ./internal/launchworker -run '^TestLaunchWorkerWrapperCrashSuite$' -count=1`
- 上述全量失败与本次 Brain diff 无代码交集，按既有并行时序 flake 记录，不改变本次 FAIL 的 P1 判断。
- PR #423 可见 build/schema checks 为成功；本地 Brain 定向测试通过，但测试“绿”不能替代缺失的验收矩阵。

## 5. Issue #424 验收清单

| 检查项 | YES/NO | 判断 |
|---|---|---|
| reason 逐值接纳/拒绝，storage 为唯一源 | YES | 七 active 值来自 storage API；五 retired 值逐项拒绝 |
| T7 input aggregate key 精确绑定实际 trace subject/project | NO | validator 只绑定独立 contract 参数，未取得本次 `CallParams` identity |
| project 合法 input builder→shell round-trip | YES | builder bytes 可被带 project context 的 validator 接纳 |
| required empty arrays 输出 `[]` 非 `null` | YES | `semantic_material=nil` 被归一为空 slice |
| `all` 预期类别全集不可省略 | NO | `allCategoryKinds=nil` 会跳过全集校验 |
| malformed T7 完整负例矩阵 | NO | 仅一个 count overflow shell 负例 |
| 每个 T7 负例在 reserve 前拒绝且 call/attempt/provider 均为零 | NO | 仅单例间接证明 call 未 reserve，未逐例断言 attempt |
| T4 无损 fallback skeleton / terminal union | YES | fallback 分支携带完整 `T4Input` |
| T4/T6/T7 ResultFromCall valid + 六值 reason 测试 | NO | 无 valid；T6 source 未断言，三者 source identity/version 证据不完整 |
| T6 四档确定性 fallback | YES | 实现与测试均覆盖四档 delivery |
| T7 显式 no-draft | YES | fallback 为 `Proposal=nil, NoDraft=true` |
| #415 可核销 | NO | P1-2/P1-3 验收仍未闭合 |
