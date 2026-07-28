# S2/M2 Forge & Intake 定向复审

**结论：FAIL**

复审基线：`origin/main` / `222a8b4`（F1–F6：#56–#61 后），工作分支 `chore/s2-m2-rereview`。本次只复核[上次阶段审计](2026-07-29-s2-m2-phase-review-pi-gpt-5.6-sol.md)的 F1–F6 关闭条件，并以 [`specs/forge.md`](../specs/forge.md)、PRD §4.5/§9.2 与 WBS M2 为判定基准。

F1、F2 已闭合，F6 的独立 reconciler 行为也已落地；但 F3 的双平台动词契约仍未闭合，F4 的能力探测没有进入生产启动路径，F5 的 daemon 组装在真实存储、收费和多项目 worker 边界上不可运行。F6 虽有组件级测试，生产 reconciler 的每个 Forge 调用都会因缺稳定收费键而失败，故不能通过 M2 门禁。

## 执行证据

- 第一次 `CGO_ENABLED=0 go test ./...`：**失败**。`internal/controlplane/TestDoctorBaselineChecksConfiguredDependencies` 再次在 5 秒 deadline 下出现时序失败；Forge/Intake/M2 相关包均通过。
- `CGO_ENABLED=0 go test ./internal/controlplane -run TestDoctorBaselineChecksConfiguredDependencies -count=1`：通过。
- `CGO_ENABLED=0 go test ./internal/forge ./internal/forgebudget ./internal/forgeworker ./internal/intake ./internal/daemon ./internal/storage -count=1`：通过。
- 第二次 `CGO_ENABLED=0 go test ./...`：通过。

全套测试仍有与上次相同的 doctor 时序波动。它不是本次 FAIL 的主因；以下结论来自生产路径和契约缺口，绿色组件测试不会覆盖这些缺口。

## F1–F6 关闭条件复核

| 原发现 | 结果 | 复核证据 |
|---|---|---|
| F1 actor/allowlist 闸门 | **PASS** | `internal/intake/poller.go:124` 对候选 Issue 调用 `ListLabelEvents`，选取当前 trigger-label 事件并按项目 allowlist fail closed；receipt 保存标签事件 actor，Issue 作者不可信时写入 `ForceHITLBeforeStart`。`TestPollerGatesOnTrustedTriggerActor` 覆盖 missing/unknown/untrusted/trusted actor、receipt actor 与强制 HITL。 |
| F2 不透明增量游标 | **PASS** | `internal/forge/cursor.go` 编码时间边界和稳定 ID，查询边界回退一秒并允许重放；GitHub 使用 `since`，GitLab 使用 `updated_after`，label event 也消费 cursor。`TestIssueCursorReplaysTimestampBoundaryWithoutLoss` 双平台覆盖同时间戳跨页及续拉不漏项，`TestLabelEventCursorIsForwarded` 覆盖事件续拉。 |
| F3 13 动词双平台契约 | **FAIL** | Checks/review/labels/RetryAt 的原可见缺口已有实现和回归测试，但完整契约仍不成立：`CreateChange` 先调用 `change()`，而 `change()` 在 head SHA 为空时直接 `ContractViolation`，因此规格 §4.7 要求的 GitLab 创建响应缺 SHA 后按 ID 重读路径不可达。GitHub `GetChecks` 请求 `/commits/{sha}/status` 却把响应解码为数组；该端点的组合 status envelope 不是当前 fixture 中人为提供的数组。现有所谓共享 V3 suite 也未逐动词覆盖真实适配器：`CommentTarget`、`CreateChange`、`ListChangeComments` 没有适配器契约测试。 |
| F4 `auto_merge` fail closed | **FAIL** | Adapter 初值 false、merge 同时检查进程内证明与持久化投影、存储重启保持 false，这些组件已具备；但 `ProbeAndRecordAutoMergeCapability` 在生产代码中没有调用点。`daemon.Assemble` 只执行 `WithAutoMergeCapabilityReader(db)`（`internal/daemon/daemon.go:57`），既不探测也不写 `capabilities_json`/审计事件。因此“已配置项目启动期执行可审计探测”的关闭条件未满足；生产只能永久保持未证明，而非完成启动能力组装。 |
| F5 daemon/预算/worker 接线 | **FAIL** | `cmd/siftd/main.go` 已构造并 tick workers，生产 Adapter 也强制 charger/key，comment worker 已按 operation kind claim；但真实主链仍不可运行。第一，启动只 `config.Load` + `storage.Open`，没有 `ActivateConfig` 或任何生产写口把 config snapshot/projects 投影入库；charger 随即无法用 Forge ref 解析 project，Intake 持久化也没有 FK 项目。第二，`Reconciler` 未设置 `WithChargeKey`，故生产 Adapter 的首个 `GetIssue` 必然返回 `ErrContractViolation`。第三，仓库仍没有读取人工回复并调用 `ApplyIntakeReply` 的 reply consumer。第四，每项目一个 comment worker，却都从全局 `forge_comment` 队列领取；GitHub worker可领取 GitLab/其他项目 operation，再交给错误平台 Adapter。第五，daemon 只用 `SupervisorInterval` ticker，未消费 `forge_cursors.next_poll_at_ms`，写入的 idle/active/slow 自适应轮询时间不参与调度。`TestAssembleWires...` 只检查对象数量/指针和“无 key 会拒绝”，没有执行上述生产 tick。 |
| F6 PRD §4.5 反向同步 | **FAIL（组件通过，生产关闭条件未通过）** | `internal/intake/reconciler.go` 已实现 Issue closed、Change merged、Change closed、可信 trigger removal 四行，并正确区分事实观测与指令鉴权；`TestReconcilerOnce*` 通过实际 reconciler 覆盖 `waiting_human → done + gate_bypassed`、两类关闭、可信/不可信 untrigger 和项目隔离。因此独立组件关闭条件通过。可是 production reconciler 复用强制收费 Adapter 且没有 charge key，第一次远端读取即失败；同时启动没有 projects 投影。故“实现并调度”的生产关闭条件和 V11 可运行首段仍未通过。 |

## 仍阻断 M2 的发现

### R1 — 生产启动未激活配置投影，所有真实 Forge 收费/Intake 写入缺项目身份（F5，阻断）

`config.Load` 明确产出应持久化的 canonical snapshot，但 `cmd/siftd/main.go` 在 `storage.Open` 后直接调用 `daemon.Assemble`。仓库没有生产 `ActivateConfig` 调用或等价写口，只有测试 seed 会创建 `config_snapshots` 与 `projects`。

这不是测试便利性问题：`forgebudget.Charger` 每次调用先以 Forge ref 查询 project ID；空库上该查询失败，Adapter 把它映射为 transient，CLI 永远不会执行。即使绕过收费，`PersistIntakeBatch` 和后续 Run 创建也依赖项目/config snapshot 外键。

**关闭条件**：daemon 启动事务化持久化/激活本次配置快照与 enabled/disabled projects 投影，再构造 Forge workers；增加从空 DB + 最小真实配置启动并执行一次带 fixture Runner 的 daemon tick 测试，证明收费、receipt、intake item/T1 handoff 均可落库。

### R2 — 生产调度与 worker 路由未形成可执行的 M2 主链（F5/F6，阻断）

`Poller` 为 Intake 调用设置收费键，`CommentWorker` 为 outbox attempt 设置收费键；`Reconciler` 没有。生产 Adapter 开启 `RequireBudget` 后，反向同步因此确定性失败。daemon 也没有人工回复 consumer；旧 generation 仲裁仅有 storage 端口测试，没有真实回复消费链。

此外，每项目 comment worker 只按 operation kind claim，不按项目/平台 claim。多平台配置下，先运行的 worker可领取另一项目的 operation。最后，`cmd/siftd` 用 supervisor interval 驱动全部工作，完全不检查持久化的 `next_poll_at_ms`，所以 adaptive/slow poll 只是写状态而不控制请求频率。

**关闭条件**：为 reconciler 每次 tick/project 使用可重放稳定收费键；接入并测试回复消费器；outbox claim/dispatch 保证 operation 路由到其 project/platform Adapter；具名 Intake scheduler 按 `next_poll_at_ms` 调度，slow mode 实际降频；用 GitHub+GitLab 两项目 daemon 级测试执行而非只检查组装字段。

### R3 — V3 仍缺真实双平台逐动词契约（F3，阻断）

当前回归补上了上次点名的 Checks/review/labels/RetryAt，但 `CreateChange` 的 GitLab 缺 SHA 重读契约不可达，GitHub combined status 响应形状与解码类型不符。共享 suite 的名称仍大于实际覆盖面，没有让 13 个动词逐项跑双平台 fixture。

**关闭条件**：用平台真实响应形状修正上述两处；建立 13 动词 × GitHub/GitLab 的显式矩阵，至少让每个动词各有一次正常归一和关键 fail-closed 断言。尤其补 `CreateChange` 缺 head 后重读、`CommentTarget`、`ListChangeComments` 与 combined Checks+Statuses 的录制 fixture。

### R4 — 启动能力探测只有端口，没有生产调用（F4，阻断）

持久化 schema/读写端口和 merge fail-closed 是有效基础，但生产组装未执行 `ProbeAndRecordAutoMergeCapability`。因此 capability checked 时间与审计事件不会由真实 daemon 产生，修复只停留在可调用组件。

**关闭条件**：在任何 merge worker/有效策略组装前，对每个已配置项目调用探测并持久化 true/false 及证据；歧义仍启动但 false，存储失败按启动错误处理；daemon 级测试覆盖成功、歧义和重启重探。

## M2 门禁

| WBS M2 门禁 | 结果 |
|---|---|
| V3 通过；V7 Forge/marker/CAS 部分通过 | **FAIL** — marker/CAS 与 comment crash 段可保留，V3 未闭合。 |
| 条件合并能力缺失时 `auto_merge` 被结构性禁用 | **FAIL** — merge 边界 fail closed，但启动期探测/持久化未接生产。 |
| actor 缺失事件被忽略；坏项目不影响健康项目 | **PASS（组件级）** — poller/reconciler 测试成立；不抵消 daemon 主链阻断。 |
| Intake crash marker 与旧 generation 回复仲裁测试通过 | **PASS（组件级）** — 两组测试通过；reply consumer 仍未接线。 |
| V11 外部事实收敛首段通过 | **FAIL** — reconciler 测试成立，但生产调用因收费键/项目投影缺失不可运行。 |

未同步 WBS checkbox：M2 组合门禁仍失败，现有局部通过项在 WBS 中与尚未接线的生产条件组合，不能整体勾选。

## 结论

#56/#57 与 #61 的组件实现解决了 F1、F2 和反向同步行为本身；#58/#59/#60 也提供了可复用基础。但本轮发现 fixes 仍以组件测试和组装对象检查为主，没有证明 `siftd` 从空库按配置启动后能执行一轮真实 M2 主链。F3、F4、F5 关闭条件未满足，F6 的生产调度亦被 F5 的收费/配置缺口阻断，因此 S2/M2 复审结论保持 **FAIL**，M3 前置不得勾选。
