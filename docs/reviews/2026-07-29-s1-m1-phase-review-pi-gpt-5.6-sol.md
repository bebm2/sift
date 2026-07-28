# S1 / M1 实现结案阶段门禁审计

> 日期：2026-07-29
> 审计人：pi × GPT-5.6-sol
> 审计基线：`762b864`（`origin/main` / `main`）
> 依据：[`AGENTS.md`](../../AGENTS.md)、[`docs/WBS.md`](../WBS.md)、五份 M1 active specs、主线代码与测试

## 结论

**M1 FAIL。**

M1 门禁表中列出的实现切片有真实代码和测试支撑；本次独立执行 `CGO_ENABLED=0 go test ./...`、`CGO_ENABLED=0 go vet ./...`、`go generate ./...` 及 darwin/linux × amd64/arm64 的 `CGO_ENABLED=0 go build ./cmd/...` 均通过。V1/V2 当前切片、V9、V10a、V10b、V12、V14、V15 构建段、第二实例、Brain fixtures 与 fake chain 的证据不是占位测试。

但 M1 仍有两个未获批准的本里程碑缺口：`doctor` 基线没有实现；T1 intake 的 clarification/duplicate comment 崩溃收敛与旧 generation 回复仲裁没有实现。它们仍明确位于 WBS M1 和 active specs 的 M1 验收中，不能仅因 M1 门禁摘要没有列出便自动变成后续范围。另有 `AGENTS.md` 仍宣称“尚无代码、可进入 M1 实现派工”，已与主线事实相反。因此当前可以称“主要 M1 自动化门禁切片通过”，不能称“M1 实现结案”。

## 1. M1 门禁证据核对

| 项目 | 主线证据 | 审计结果 |
|------|----------|----------|
| V1 状态机 | `internal/storage/transition.go` 的唯一状态写入口、完整合法转移函数、CAS；`TestV1RunTransitionGraphAndCAS`、`TestV1IllegalTransitionIsAudited` | **通过** |
| V2 M1 核心 | `TestV2TransitionCrashAtomicity`；`TestV2CurrentWritePortsCrashAtomicity` 对 Forge Run/receipt、Task Spec、Brain attempt/token、outbox claim/completion 在末写入点 abort，并断言无部分提交 | **通过当前已实现写端口切片**；不是 V2 最终闭合 |
| V9 骨架段 | `TestV9FirstSegmentSkeletonChain` 驱动 fake Issue → T1/T2 → queued/running → fake completion → 注入 merged fact → done；断言 `gate_bypassed`、时间锚点及无 create/merge outbox | **通过** |
| V10a 端点段 | `TestV10aEndpointCapabilitiesAndSockets`：双 socket、0600、缺 token 拒绝、`run.sock` 无 ops、run token 无 claim 权限 | **通过 M1 端点段** |
| V10b | `TestV10bUnsafeLocalAttackReproduces` 真实读取 `operator.token` 并成功调用 ops，同时严格断言 `unsafe-local` | **通过**；通过含义是预期攻击可复现，不是隔离已闭合 |
| V12 | `TestV12Scenario1MissingFile`、`TestV12Scenario2MinimalSchedulable`、全默认值断言及 `TestV12ZeroConfigStartsDaemon` | **通过** |
| V14 | 单一 decode gateway、closed/open-envelope 契约测试、goldens；CI 有 schema drift job；`go generate ./...` 后无 tracked diff | **通过** |
| V15 M1 构建段 | `.github/workflows/build.yml` 四组合矩阵且 `CGO_ENABLED=0`；本次四组合手工 cross-build 全绿 | **通过 M1 构建段**；不代表 M8 运行/安装/升级闭合 |
| 第二实例 | flock 实现及 `TestSecondDaemonRefusesLock` | **通过** |
| 零 TCP/UDP listener | daemon 只创建 Unix socket；Linux 专用 `TestV10ZeroNetworkListeners` 枚举本进程 fd inode 与 `/proc/self/net/*` | **有可信测试证据**。本机为 Darwin，Linux 专用测试未在本次本地运行；仓库查询不到该基线的 hosted run 记录，不把 workflow 定义冒充远端执行记录 |
| Brain fixtures | `TestShellWithRealCLIFixture` 走真实 fixture 子进程并验证 invalid→同 prompt retry→valid、逐 attempt 收费；fallback/recovery/zero-usage tests 通过 | **通过** |
| fake chain | fake Forge/Agent/Brain 分别实现端口；骨架明确是 integration harness，成功依赖注入的外部 merged fact | **通过 M1 骨架段** |

## 2. 三个未勾选项

### 2.1 V1/V2 崩溃注入聚合项：诚实的分片延后，非 M1 blocker

该项把“当前写端口”和 M2–M5 才会出现的项目健康、Forge 收费、Interrupt 推进/delivery 写端口放在同一长期复选框中。当前端口已经有实际 abort 注入；WBS 的自动化权威表也明示 V2 在 M1 仅首次运行核心事务、到 M6 最终闭合。未把未来表结构当作崩溃证据，延后范围和落点均可追踪。

结论：**诚实 deferral，不阻断 M1**。建议后续把长期聚合项拆成按里程碑复选框，避免“永远未完成”的 M1 行。

### 2.2 `doctor` 基线：M1 blocker

WBS 1.5 要求基线检查 runtime、SQLite、Agent CLI、相关 forge CLI 登录/版本、按配置启用的 tmux、目录/socket 权限，并明确只有策略、hooks、积压和安全姿态是后续增补。`specs/config.md` §5、§7、§8 同样把启动探测和 doctor 确定退出码列入 M1 验收。

实际 `internal/controlplane/server.go:doctor` 是固定返回一条 `operator-token-readable-by-agent` warning 的硬编码结果；它不读取 runtime、SQLite、配置、CLI、tmux 或权限，也总是 `exit_code: 1`。现有测试只证明 V10b 姿态呈现，不能证明 doctor 基线。

结论：**未实现的 M1 承诺，阻断结案**。要么实现并测试，要么在结案前通过 WBS/spec 修订明确重定里程碑及理由；不能靠门禁摘要省略。

### 2.3 intake crash/replay：部分完成，剩余部分是 M1 blocker

该复选框包含三项：

- replay JSONL 已实现：`ExportBrainCallsJSONL` 与 `TestExportBrainCallsJSONL` 证明单条 `brain_call` 携有序 attempts；
- clarification/duplicate comment 在“远端成功、本地提交前崩溃”后的 marker 收敛未实现；
- 旧 generation 回复只审计、不推进状态未实现。

`internal/storage/intake.go` 明说完整 intake projection/CAS/generation protocol 到 M2；代码也没有对应 comment worker/回复消费写端口。这是透明的实现注释，并非伪装完成。然而 `docs/specs/brain.md` §10.6 仍把后二者列为 **M1 验收**，WBS 1.7 也仍在 M1。代码选择 M2 与当前验收基线互相矛盾，尚无正式 scope change。

结论：**实现层面是诚实暴露，阶段治理上仍是 blocker**。若产品决定它属于 M2，应先同步修改 WBS、brain/outbox/storage 的 M1 验收映射并记录理由；否则应补实现和测试。

## 3. 完成真实性与范围审计

### 未发现伪造的部分

- 没有临时 Gate、没有由 Sift 创建/合并 Change；V9 明确注入外部 merged fact并记录 `gate_bypassed`。
- fake chain 自称 M1 integration harness，而非 daemon 完整链；M2/M3/M4/M5 的真实 intake、launch、Gate、Command 均未被冒充。
- V10b 正确把同 UID token 可读性作为预期成功的攻击复现，没有写成安全边界闭合。
- V2、V9、V10a、V15 均标注阶段切片，没有声称最终门禁已闭合。
- 三个未完成项保持 `[ ]`，没有直接勾成完成。

### 范围/文档问题

1. M1 门禁摘要已全部勾选，但同一 M1 章节和 active specs 仍有两个未完成验收承诺。摘要的“已实现写端口”收窄对 V2 合理，对 doctor/intake 则没有正式改基线。此时宣布 M1 完成会构成 **scope drift 后的假结案**，即使单项复选框仍诚实。
2. `AGENTS.md` 仍写“M1 规格阶段，尚无代码”，而主线已有 Go module、三个命令及完整 M1 实现树。它曾是 S0 的正确事实，现在已成为误导代理的项目状态；这违反其导航职责，必须随阶段状态同步。
3. 本次没有取得 commit `762b864` 的 hosted CI run 记录；本地可复现的 tests/vet/generate/cross-build 全绿，但 Linux-only listener 测试的远端执行结果不应被虚构。

## 4. 解除阻断的最小条件

1. 实现并测试 WBS 1.5 的 doctor 基线，或正式修订 WBS/spec 将其移出 M1并说明依赖。
2. 对 intake 缺口二选一：实现 marker crash convergence 与 stale-generation reply 仲裁；或将这两项从 brain M1 验收正式迁移到 M2 Intake，并同步所有引用。保留已通过的 replay JSONL 为 M1 证据。
3. 更新 `AGENTS.md` 的项目现状，使其准确反映 M1 实现状态和下一阶段入口。
4. 在 Linux CI 留存 `TestV10ZeroNetworkListeners` 的成功记录，再作最终结案证据归档。

完成上述阻断处置后可重新审计；在此之前裁决保持 **M1 FAIL**。
