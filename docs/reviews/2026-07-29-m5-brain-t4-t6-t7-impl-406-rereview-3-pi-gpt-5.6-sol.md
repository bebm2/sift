FAIL

# M5 Brain T4/T6/T7 调用壳（#428）第三次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`27223621e06ae6958b6e70394bc5d8d24b635836`（PR #431）
> 评审对象：Issue #432、Issue #428 / PR #431、[`brain.md` §11–§13](../specs/brain.md)
> 前序报告：[`#424 第二次定向复审`](2026-07-29-m5-brain-t4-t6-t7-impl-406-rereview-2-pi-gpt-5.6-sol.md)
> 范围纪律：只出报告；未修改实现、规格或 WBS，未自修

## 1. 结论

**FAIL（2×P1 仍未完全关闭）。**

PR #431 已把 `ValidateInput` 扩为接收本次完整 `CallParams`，T7 validator 会在 reserve 前校验实际 `Scope/SubjectKey/ProjectID`，input aggregate key 也必须等于实际 subject；`all` contract 现在拒绝 nil、空、非法、重复或未排序的预期类别全集。T4/T6 valid adapter 还新增了领域后校验，测试也首次执行 T4/T6/T7 三个 valid 分支。这些实现方向正确。

但 #428 明列的验收测试主体没有交付。T7 仍只有前序那一个 `negative_samples > total_samples` shell 负例，既没有 table-driven malformed 矩阵，也没有对每例直接断言 `brain_calls=0`、`brain_attempts=0`、provider calls=0。adapter 的六值循环仍丢弃 T6 source，T4/T7 也只断言 reason/version；没有带原始错误详情的 reason 防泄漏用例，T4 fallback skeleton 仍只抽查一条 link 与 brief。新增 valid 用例对 T6/T7 source 也未断言 `OutputSchemaVersion`，且只检查输出指针存在，不能替代关闭条件要求的完整来源与无损证据。

因此 #424 剩余 P1 尚不能由 #428 核销。

## 2. 剩余可执行 P1

### P1-1：T7 真绑定与 `all` guard 已实现，但 malformed 三零矩阵仍缺失

`internal/brain/shell.go:46-50,81-85` 现在把完整 `CallParams` 交给 pre-reserve validator；`internal/brain/t4t6t7.go:413-444` 直接读取本次 `p.Scope/p.SubjectKey/p.ProjectID`，并将 input aggregate key 与实际 subject 绑定。`validTaskKinds` 及 `parts.kind == "all"` guard 也使 `allCategoryKinds` 成为确定性非空、合法、排序去重的全集输入。就实现静态路径而言，前序的闭包副本绕过和可省略 `all` 集合已关闭。

然而 `internal/brain/t4t6t7_issue415_test.go:45-84` 仍只有：

- 一次直接 validator 的合法 project round-trip；
- 一次直接 validator 的 project 拒绝；
- 一个 count overflow shell 负例，以随后合法 call 的 `CallSeq == 1` 间接判断未 reserve，并只直接检查 provider 未调用。

PR #431 对该段只把旧 `ValidateInput([]byte)` 调用适配为 `ValidateInput(CallParams)`，没有新增任一 malformed case。仍无逐例覆盖：required 字段残缺；category/replay 四类 count 的负数及各不等式越界；input 对实际 trace 的 scope/project/window 漂移；concrete/all 类别漂移及 `all` 缺类/多类；category/replay/semantic 内和跨域 evidence ID 重复；category/semantic 未排序。也没有逐例直接查询 storage 证明 call=0、attempt=0。

**关闭条件：** 补 table-driven shell 测试覆盖上述矩阵；每一例在独立 DB 或明确基线计数下，直接断言 `brain_calls` 增量为 0、`brain_attempts` 增量为 0、provider request 增量为 0。trace 的 scope、subject、project 漂移和 `all` nil/空/缺类/多类也必须经过 `Shell.Call` 接缝，而非只直接调用 validator。

### P1-2：valid 分支已出现，但六值 fallback source 与 T4 逐字段无损证据仍缺失

`internal/brain/t4t6t7_issue415_test.go:86-100` 新增了 T4/T6/T7 valid adapter 调用，覆盖了 normal/no-draft 分支和 `kind/logical_call_id/prompt_version` 的基础来源；T4 还断言 schema version。实现端 `internal/brain/t4t6t7_result.go:41-77` 也令 T4/T6 valid adapter 在 decode 前执行各自领域 validator。

但 `internal/brain/t4t6t7_issue415_test.go:102-132` 的 fallback 验收仍有前序报告指出的关键缺口：

1. T6 在四档循环中继续以 `out, _, err := T6ResultFromCall(...)` 丢弃 source，六种 reason 的 `Kind/LogicalCallID/Version/Reason` 完全未断言；
2. T4/T7 每种 reason 只断言 `Reason/Version`，未断言 `Kind == fallback` 与对应 logical call ID；
3. raw reason 仍只是规范短值（token 用 `token_budget_exceeded`），没有使用 `attempts exhausted: ...`、`recovery: ...` 等带自由文本详情的 shell reason，因而没有证明 source 只暴露 canonical 六值而不泄露详情；
4. T4 无损测试只检查 `len(links)==1` 和 `fallback_brief`，没有逐字段证明 run/attempt identity、reason/severity/modality/headline、全部 fragments、links 以及每个 option 的 `{id,label,effect,risk}` 与顺序均保留；
5. valid T6/T7 source 未断言 `OutputSchemaVersion`，输出也只检查非 nil，完整 valid adapter/source round-trip 证据仍不闭合。

**关闭条件：** 六种 fallback reason 对 T4/T6/T7 每个触点逐项断言 `Kind/LogicalCallID/<touchpoint>/fallback/v1/canonical Reason`，并让至少 attempts-exhausted 与 recovery 输入携带不可出现在 source 中的原始详情；保留 T6 四档 delivery 与 T7 `Proposal=nil, NoDraft=true`。T4 以非平凡完整 fixture 对 fallback union/skeleton 做逐字段等值断言；valid 三触点断言 decoded output 及完整 brain source。

## 3. 已通过项

- T7 pre-reserve validator 直接接收并校验本次 `CallParams`；input aggregate key 绑定实际 trace subject。
- T7 强制 aggregate scope，并校验实际 trace project；合法 project builder→validator round-trip 保持通过。
- `all` contract 必须给出非空、合法、排序去重的确定性类别全集。
- T4/T6 valid adapter 先做领域后校验，再 decode canonical output。
- T4/T6/T7 valid adapter 测试分支已存在。
- T6 四档确定性 fallback 和 T7 显式 no-draft 的既有行为保持通过。
- 未发现非目标越界：未接入 EmitInterrupt/Channel/T7 审批流，未勾选 WBS §5.1。

## 4. 测试证据

- `go test ./internal/brain -count=1`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**FAIL**；与前序相同的并行时序 flake：
  - `TestDoctorBaselineChecksConfiguredDependencies` 的 agent/tmux fixture `signal: killed`；
  - `TestLaunchWorkerWrapperCrashSuite` 未观察到 started marker。
- 两个失败分别单独运行均 **PASS**：
  - `go test ./internal/controlplane -run '^TestDoctorBaselineChecksConfiguredDependencies$' -count=1`
  - `go test ./internal/launchworker -run '^TestLaunchWorkerWrapperCrashSuite$' -count=1`
- PR #431 的 build、schema drift、vet + test checks 均为 **SUCCESS**。测试绿不替代 issue 明确要求但未提交的 malformed/source 验收矩阵。

## 5. Issue #432 验收清单

| 检查项 | YES/NO | 判断 |
|---|---|---|
| T7 pre-reserve 绑定实际 `CallParams.SubjectKey/ProjectID` | YES | validator 直接取得完整 params，并绑定 scope/subject/project |
| `all` 提供并校验确定性非空类别全集 | YES | contract guard 拒绝 nil/空/非法/重复/未排序集合 |
| malformed T7 完整矩阵 | NO | 仍仅一个 count overflow shell 负例 |
| 每个 malformed 例 call=0、attempt=0、provider=0 | NO | 仅单例间接判断 call seq，未直接断言 call/attempt，矩阵不存在 |
| T4/T6/T7 valid adapter | YES | 三个 valid 分支均已调用；完整 source 字段仍有缺口 |
| 六值 fallback source 完整测试 | NO | T6 source 被丢弃；T4/T7 未断言 kind/logical ID；无详情防泄漏输入 |
| T4 fallback skeleton 逐字段无损 | NO | 仍只抽查一条 link 与 fallback brief |
| 保留 T6 四档与 T7 no-draft | YES | 既有矩阵/断言保持通过 |
| #424 剩余 P1 可核销 | NO | 两组明确验收测试均未闭合 |
