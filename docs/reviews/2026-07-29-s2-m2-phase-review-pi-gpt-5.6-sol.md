# S2/M2 Forge & Intake 阶段审计

**结论：FAIL**

审计基线：`origin/main` / `75c9918`（#40–#46 后）。范围按 [`WBS.md` M2](../WBS.md) 的任务与门禁逐项核验，并以 [`specs/forge.md`](../specs/forge.md) 为字段级判定基准。

M2 已有相当数量的可复用组件和绿色单元/契约测试，但当前真实 Intake 主链绕过了触发 actor 闸门，双平台适配器没有实现规格要求的安全增量游标，反向同步与 daemon 接线也未完成。因此不能宣称 V3、actor 门禁或 V11 首段已通过，M3 前置仍不成立。

## 执行证据

- `CGO_ENABLED=0 go test ./...`：第二次全量执行通过。
- 首次全量执行曾在 `internal/controlplane/TestDoctorBaselineChecksConfiguredDependencies` 因 5 秒 context deadline 失败；该测试单独重跑通过，第二次全量也通过。此现象与 M2 功能无直接关系，但提示全套测试仍有时序波动。
- `CGO_ENABLED=0 go test ./internal/forge ./internal/forgebudget ./internal/forgeworker ./internal/intake ./internal/storage ./internal/skeleton`：通过。

绿色测试只证明被覆盖的局部行为；下述生产路径缺口和契约偏差不会被现有套件捕获。

## M2 门禁核验

| WBS M2 门禁 | 结果 | 证据与缺口 |
|---|---|---|
| V3 通过；V7 Forge/marker/CAS 部分通过 | **FAIL** | marker 全状态查找、stale-head 分类及双平台 CAS 测试存在且通过；但 V3 不能通过：增量游标不符合 spec，13 动词的双平台契约覆盖不完整，Checks/review/labels 等归一仍缺失。 |
| 条件合并能力缺失时 `auto_merge` 被结构性禁用 | **FAIL** | `Adapter` 在一次 merge 返回特定 stderr 后仅于本进程内置位；初始状态反而默认“支持”。没有启动期能力证明、项目有效 capability 接线或持久化，重启后恢复为支持。 |
| actor 缺失事件被忽略；坏项目不影响健康项目 | **FAIL** | 适配器局部测试证明空 actor 行被丢弃，poller 测试证明单项目 `AuthOrCapability` 不终止同 tick；但真实 Intake 根本不查询触发标签事件、不校验 allowlist，而把 Issue 作者写成事件 actor，安全闸门被绕过。 |
| Intake crash marker 与旧 generation 回复仲裁测试通过 | **PASS（组件级）** | `forgeworker` 的远端成功/本地提交前崩溃重放测试和 `storage` generation CAS 测试通过。注意：回复消费器及 worker 尚未接入 daemon，因此这不足以使 M2 整体通过。 |
| V11 外部事实收敛首段通过 | **FAIL** | 测试先从 fake 读 Change，随后在测试体内手工调用 `AppendEvent` 与 `TransitionRun`；仓库没有执行该收敛的 Change reconciler。生产代码只有不完整的 Issue-close helper，不能据此认定 V11 可运行。 |

未同步 WBS checkbox：唯一局部通过项仍缺生产消费/daemon 接线；其余 checkbox 是组合条件，不能用局部单测代替完整证据。

## 阻断发现

### F1 — 触发标签 actor/allowlist 闸门被 Intake 绕过（阻断）

`internal/intake/poller.go:69` 只调用 `ListIssuesByLabel`，没有调用 `ListLabelEvents` 回溯是谁添加触发标签，也没有 allowlist 输入。`internal/intake/poller.go:79` 将 `Issue.Author` 写成 receipt/event actor。

这把“Issue 作者”误当成“驱动事件 actor”，违反 PRD §9.2、WBS H9 与 M2 §2.3。结果是：只要当前标签存在，未知 actor、缺失 actor 或不可信 actor 的 Issue 都可进入 `pending_evaluation`。同时也没有实现“不可信 Issue 作者被可信 actor 触发时强制开工前审批”。适配器丢弃空 actor 的测试无法保护一条从未调用 actor 端口的主链。

**关闭条件**：Intake 对每个候选 Issue 回溯当前 generation 的 trigger-label add event，按平台 allowlist fail closed；receipt 保存标签事件身份而非 Issue 作者；增加 unknown/missing/untrusted actor 的 poller 级负测试及不可信作者强制审批测试。

### F2 — 不透明增量游标实现不成立，存在漏项/重复全扫风险（阻断 V3）

`internal/forge/cli.go:240-270`、`:292-319`、`:671` 用“本次响应条数”作为 Cursor，下一轮又把该值作为 `since` 发送。它既不是时间边界，也没有稳定 tie-breaker；GitLab Issue 查询还使用 `since`，而规格要求 `updated_after`。label event 端口没有使用传入的 cursor。

现有分页测试只断言 101 条返回 cursor `"101"`，恰好固化了错误实现，没有覆盖同时间戳边界、重启续拉或第二轮增量读取。

**关闭条件**：按 forge spec §2/§4 实现平台私有的时间边界 + 稳定 ID 游标、重叠读取和去重，并对 GitHub/GitLab 分别测试同时间戳跨页、持久化后续拉、重放不漏项。

### F3 — 13 动词“已实现”但双平台契约未闭合（阻断 V3）

现有共享套件主要覆盖分页外形、actor 丢弃、Change 基础归一、marker 与 merge CAS；没有逐动词跑完整双平台契约。代码中已有可见偏差：

- `GetChecks` 未按 spec 合并 GitHub Checks + Statuses；GitLab 只读 pipeline，不读 jobs/`allow_failure`；
- `Change.ReviewState` 在 `internal/forge/cli.go:397` 恒为 `unknown`，没有 GitHub Reviews / GitLab Approvals 归一；
- `SetLabels` 没有执行后重读并验证目标子集与保留无关标签；
- rate-limit 分类没有解析结构化 `RetryAt`；
- 多处未知平台状态被默认映射为确定值，而不是 `ContractViolation`/`unknown`。

因此测试名 `TestV3ContractSuite*` 不能作为整个 V3 的通过证明。

### F4 — `auto_merge` capability 默认乐观且仅内存禁用（阻断）

`internal/forge/cli.go:96` 以“不在 unsupported map 中”等价于支持，未先证明远端 expected-head CAS capability；只有一次真实 merge 已失败且 stderr 命中特定字符串后才禁用。状态只在 adapter 内存中，重启丢失，且没有与项目有效策略或 startup probe 接线。

这违反 H13/M2 §2.2 的“不能证明即结构性禁用”，也可能让首次自动 merge 成为能力探测。

**关闭条件**：启动期对已配置项目执行可审计能力探测；未证明即将项目 capability 置为不可用，并让策略组装/merge worker消费该状态；重启后仍 fail closed。

### F5 — Intake/worker/预算均未接入 daemon，API 收费可被静默绕过（阻断）

`cmd/siftd/main.go` 只加载配置并启动 control-plane，没有构造 Forge adapter、charger、Poller、T1Evaluator、comment/reply worker 或 reconciler。生产代码中也没有 `WithCharger` / `WithChargeKey` 的调用点。

此外 `internal/forge/cli.go:71-76` 在 charger 或 charge key 缺失时直接放行 CLI。于是当前可执行 daemon 没有 M2 Intake，且即使未来调用 adapter 时漏接 context key，也会无收费访问 Forge，违背“API 调用只在适配层收费”的硬约束。`CommentWorker` 又调用不按 kind 过滤的通用 `ClaimOutboxOperation`；一旦与其他 worker 同时接线，它可抢占非 `forge_comment` operation 并按错误 payload 处理。

**关闭条件**：完成 siftd 组装与调度接线；生产 adapter 强制 charger/key（仅显式 test/fake 构造可关闭）；Intake tick 和每个 outbox attempt 使用可重放稳定 key；worker 只领取自己支持的 kind；增加 daemon 级接线测试。

### F6 — PRD §4.5 反向同步仅有不完整 Issue helper（阻断 V11）

`internal/storage/reverse_sync.go` 只处理 Issue closed，且无人调用。Change 外部 merged/closed、可信 actor 移除触发标签均未实现。V11 测试在测试体中手工拼接通用 storage 端口，不是 reconciler 行为证据。

同时项目隔离只写 `project.isolated` event；没有看到 WBS 要求的“一次告警”发布 operation。

**关闭条件**：实现并调度四行反向同步；事实观测不做 actor 鉴权，label removal 做 allowlist 鉴权；用 fake/fixture 通过实际 reconciler 驱动 `waiting_human → done + gate_bypassed`，并覆盖 change closed、issue closed、可信/不可信 untrigger。

## 已确认可保留的实现

- `specs/forge.md` 已 active 且端口包含 13 动词。
- argv 数组执行边界、基本五类错误 sentinel、actor 空值过滤、Change marker 搜索、expected-head 参数与无条件 merge 禁止已有实现基础。
- `PersistIntakeBatch` 的“同事务持久化批次后推进 cursor”、`PersistIntakeDecision` 的状态 CAS/outbox 创建、旧 generation 审计以及 comment marker crash recovery 都有针对性测试。
- 项目级 auth/capability 隔离不会阻断健康项目的局部 poller 测试已通过。
- Forge API 固定小时桶、幂等收费与 slow-poll 状态存储已有独立测试；缺口主要在强制调用和 daemon 集成。

## 结论

M2 当前是“组件骨架与局部契约测试已具备”，不是可运行且满足安全不变量的 Forge & Intake 切片。F1、F2、F4、F5、F6 均直接阻断 M2 gate；修复并增加真实主链测试后应重新进行定向复审。M3 前置 checkbox 保持未勾选。
