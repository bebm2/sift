FAIL

# M5 #634 T4→EmitInterrupt after #628 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#628 / PR #631，实现提交 `ca763f4`，合入提交 `f153348`
> 评审基线：`main` / `origin/main` `2613add`（另含后续 #629）
> 前次结论：[#622 FAIL](2026-07-30-m5-t4-emit-interrupt-622-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §3.6/§9.2](../specs/interrupt.md)、[`report.md` §7](../specs/report.md)、[`storage.md` §6.4/§12.2.1](../specs/storage.md)

## 1. 结论

**FAIL（1×P1）。** #628 增加了有效的 binding 注入失败整事务回滚、Gate unknown fragment/option fallback、Report quota 三类无效输出、两个 crash/replay 场景和四 writer 并发场景；要求的三个包、全仓测试、定向重复测试及完整三个包 `-race` 均通过。实现方自述的 `-race` 超时在本基线上**未复现，不再单独阻断**：完整命令用时约 6 分 13 秒，仍是较慢的非阻断风险。

但 #628 只有 186 行测试增量，仍未完成 #622 明示的 closed SQL union、attempt/Report quota canonical serializer、完整 Gate failure-review identity及 Report quota 全 crash-cut 矩阵。新增 quota 测试比较了若干 Go 字段和 persisted literal，却没有按规格要求构造并逐层 canonical serialize 完整 T4 input/output，再与 §3.6 bytes 逐字节比较。因此 #622 的唯一 P1 仍未关闭。

本轮遵守“禁止自修自审”：只新增本评审报告，不修改被评审实现、测试或规格。

## 2. 阻断项

### P1-1：#622 的完整 acceptance matrix 仍为 NO

#### Report quota crash/concurrency：PARTIAL

`TestReportQuotaExhaustionCrashReplayAndConcurrency` 是有效增量：它覆盖 exhaustion 行 INSERT 失败时 rate token 回滚、binding INSERT 失败后保留已提交 exhaustion 并可重放发射，以及四个 writer 收敛到一条 exhaustion/一个 quota Interrupt。`-count=50` 通过，定向及完整 `-race` 也通过。

但该组仍不是 #622 要求的完整 crash matrix：

- exhaustion 事务只注入一个 `report_quota_exhaustions` INSERT 失败点，没有分别覆盖 rate bucket、安全事件、唯一键竞争的提交前后 cut；
- 发射事务只注入 binding INSERT 失败，没有 publish target、attention admission、Interrupt/event/outbox 等拒绝/SQLite cut；
- 并发用例只断言 exhaustion 与 quota Interrupt 数量，没有逐一断言唯一 security event、generation key、rate token 消费、binding、admission和 operation；
- replay 用例以总 Interrupt 数 `1→2` 间接计数（第一个是普通 `agent_blocked`），没有逐对象核对 quota 发射失败时五件事全无、恢复后各唯一。

故 crash/concurrency 从 **NO** 提升为 **PARTIAL**，不能记完整关闭。

#### Binding rollback：窄项 YES；closed SQL union 仍 NO

`TestEmitInterruptBindingFailureRollsBackFiveThings` 在 binding INSERT 注入 ABORT 后检查 Interrupt、admission、budget、event、outbox、delivery、binding 均为零，并验证 Run 仍为 `queued/version=1`。该窄项可以核销。

但 #622 同时要求的 closed binding union矩阵没有新增：malformed JSON、required/null、错误 value type、额外字段、canonical key order/digest、options一致性，以及各 reason/两个 `failure_review` arm 的 source identity/FK 错配仍未形成完整验收表。既有 identity测试仍主要是在合法提交后插第二条 forged binding；不能代替各 arm 经真实 EmitInterrupt 的拒绝和回滚矩阵。

#### Report quota canonical/persisted bytes：PARTIAL

`TestReportQuotaT4AcceptanceAndPersistedBytes` 经 production `RecordReport` 路径验证 quota variant、最终 headline/brief/options/links literals；无效输出也覆盖 reorder、added retry和错误 recommended option。这些断言有效。

然而 [`interrupt.md` §3.6](../specs/interrupt.md#36-t4-接纳与命令-golden-vectors) 明确要求测试“逐层以同一 serializer 重算”完整 canonical T4 input/output，并比较原始 UTF-8 bytes。新增测试只捕获 storage seam 的 `InterruptT4Input`，用字段比较和 `fmt.Sprint` 检查部分内容；它没有调用 canonical serializer，没有形成完整 input/output JSON bytes，也没有对 Run/reason/severity/modality/links/fallback等输入全集作 exact 比较。无效输出 fallback又没有核对 `links_json`。因此 persisted literal 有增量，但 exact canonical矩阵仍不满足规格。

attempt T4 同样仍只直接调用 `acceptInterruptT4`，#628 没有补完整 canonical input/output和经 EmitInterrupt 的 persisted exact bytes。

#### Gate T4：PARTIAL

Gate表增加 unknown fragment与 unknown option，normal/reorder/fragment/option 四例均经 `EvaluateRecordAndEmitInterrupt`，方向正确。但断言仍只有 brief、severity、非空 generation key/calibration ID；没有 exact options/links、evaluation/calibration/outbox identity，也没有 `internal/gate/interrupt.go` default 分支的 `failure_review` attempt T4 正常/无效矩阵。故不能按 #622 的 Gate normal/invalid/failure-review关闭标尺记 YES。

## 3. `-race` 超时核验

- 定向五组新增/相关测试 `go test -race ...`：**PASS**，约 24 秒。
- 完整 `go test -race ./internal/storage/ ./internal/gate/ ./internal/intake/ -count=1`：**PASS**；storage 约 373 秒、Gate约 59 秒、Intake约 55 秒。
- Go 默认单 package timeout 为 10 分钟，本轮 storage 未越界，也未见 data race；因此实现方所述 timeout **当前不阻断**。
- storage race suite耗时超过 6 分钟，仍可能在较慢 runner 上接近门限，记 non-blocking risk，不得替代缺失验收 vectors。

## 4. 回归与执行证据

- 完整读取 `gh issue view 634` 与 `gh issue view 634 --comments`；GitHub，0 comments。回溯 #622、#628、PR #631及其 diff/CI。
- `git diff f153348^..f153348 --check`：**PASS**。
- `go test ./internal/storage/ ./internal/gate/ ./internal/intake/ -count=1`：**PASS**。
- `go test ./internal/storage -run '^TestReportQuotaExhaustionCrashReplayAndConcurrency$' -count=50`：**PASS**。
- 定向相关测试 `-race`：**PASS**。
- 完整三个包 `-race`：**PASS**，未复现 timeout。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**PASS**。
- #628 未新增 migration；当前 `0001`–`0044` 连续，符合“仅需要时从 0044+”的约束。
- PR #631 的 vet/test、schema drift和四平台 build checks均为成功。

## 5. Issue #634 验收清单

| 条件 | 结果 | 说明 |
|---|---|---|
| 获取并阅读 #634全文、Agent建议、关闭条件、约束与comments | **YES** | GitHub，0 comments。 |
| 对照 #622 FAIL / #628 | **YES** | 逐矩阵复审。 |
| Report quota crash/concurrency完整 | **NO / PARTIAL** | 三类有效增量；crash cuts及并发对象断言仍不完整。 |
| binding拒绝整事务回滚 | **YES** | binding ABORT后五件事、delivery/binding和Run状态均未泄漏。 |
| closed binding union SQL矩阵完整 | **NO** | malformed/type/null/extra/canonical/digest/options/各arm仍缺。 |
| Gate T4 normal/invalid/failure-review完整 | **NO / PARTIAL** | 新增两类fallback；缺exact identity及failure-review路径。 |
| Report quota exact canonical/persisted-byte完整 | **NO / PARTIAL** | persisted literals有覆盖；未用canonical serializer重算完整input/output bytes。 |
| attempt exact canonical/persisted-byte完整 | **NO** | #628无对应增量。 |
| 实现方自述 `-race` timeout仍阻断 | **NO** | 完整三个包race通过；耗时较长仅记风险。 |
| 要求三个包普通测试 | **YES** | 全绿。 |
| 全仓 test/vet | **YES** | 全绿。 |
| migration约束 | **YES** | #628无需 migration，当前连续至0044。 |
| 结论写入 `docs/reviews/`、仅当前 conventional worktree | **YES** | `feat/issue-634-rereview-t4-emitinterrupt-after-628`。 |
| 禁止自修自审、未 push/MR/merge | **YES** | 仅新增评审报告。 |
| #628可按 #622关闭标尺核销 | **NO** | 完整矩阵仍未交付。 |

## 6. 最终裁决

**FAIL。** #628 的 binding rollback、Gate无效输出、quota persisted literals及初步 crash/concurrency测试均为有效增量，且 `-race` timeout本轮不再阻断；但 closed SQL union、canonical serializer exact bytes、Gate failure-review identity和完整 Report crash cuts仍缺。补齐上述矩阵并由不同代理再次复审前，#622不能核销。
