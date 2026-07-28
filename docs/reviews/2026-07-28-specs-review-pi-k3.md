# Specs 评审：config.md + storage.md

> 日期：2026-07-28
> 评审人：pi × Kimi K3
> 评审对象：[`docs/specs/config.md`](../specs/config.md)（draft）、[`docs/specs/storage.md`](../specs/storage.md)（draft）
> 评审输入：PRD V0.8、DESIGN D0.10、WBS D0.3、ADR-002/003/004/009/010/013

## 1. 结论

- **storage.md：暂不通过。** 2 项 P1（brain_traces 身份域与触点作用域矛盾；§11 受限写端口清单覆盖不全）均为字段级可实现性缺陷，M1 实现会立即撞上。修订后需定向复评。
- **config.md：条件通过。** 无 P1；4 项 P2 修订后可转 `active`，P3 随修订一并处置。

两份 spec 对 PRD/DESIGN/ADR/WBS 的追溯整体扎实：`attempt_resolution` 新规范名与 V0 枚举已按 DESIGN §14.14 落地；DESIGN §14.2 点名的启动协议五时限、PRD §12 全部数值类开放问题均有确定性默认值；两级启动探测、Interrupt 五件事事务、ADR-013 CAS 结果事务、outbox claim CAS、append-only trigger 清单均与上游一致（逐项核对见 §5）。阻断项集中在 storage 的两处「表与端口对不上触点/端口声称覆盖的现实」。

## 2. storage.md 发现

### S1（P1）：`brain_traces` 的 `run_id`/`attempt_no` NOT NULL 与触点实际作用域矛盾

§10.1 定义 `run_id TEXT NOT NULL`、`attempt_no INTEGER NOT NULL`，唯一约束 `(run_id, attempt_no, touchpoint, call_seq)`，`touchpoint` 枚举含 `T1`..`T7`。但按 PRD §6 与 WBS M1 §1.6/§1.7 的既定流程：

- **T1 体检发生在 Run 创建之前**（Issue → Intake → T1 → Run[queued]），被 T1 判为不可执行/重复的 Issue 永远不会成为 Run，无 `run_id` 可填；
- **T2 分派发生在任何 attempt 创建之前**，无 `attempt_no` 可填（attempt_no 从 1 开始且属于调度产物）；
- **T7 校准提案是聚合层触点**，从 Ledger 生成类别级提案，本就不属于单条 Run/attempt。

WBS M1 §1.7 要求「每次调用持久化输入、原始输出摘要、合法输出、版本、兜底标记、usage/token」，M1 门禁含「trace 持久化与 token 收费」——按现 schema，T1/T2 的 trace 在 M1 就无法落库。DESIGN §7 的「调用身份 `(run_id, attempt_no, touchpoint, call_seq)`」在字段层不成立，spec 必须修正而不是照搬：`run_id`/`attempt_no` 改为可空（并按 touchpoint 声明何时必填），或为聚合/摄入期触点定义独立身份域；唯一约束相应改为部分唯一索引。

### S2（P1）：§11 受限写端口清单覆盖不了 §4–§10 声明的可变表

§11 自称封闭全集（「存储模块只暴露以下写入族」「任何新增写端口必须证明不能由上述端口表达」），但以下可变写入无端口归属：

| # | 写入 | 触发处 | 现状 |
|---|------|--------|------|
| a | `projects` 投影：启动期配置演进（§4.3「项目配置变化更新当前投影」）、项目级探测失败写健康投影（config.md §5.2）、运行期 `AuthOrCapability` 隔离与 capability 刷新（WBS M2 §2.2） | 启动期 + Intake/outbox 运行期 | `SaveConfigSnapshot` 允许写入仅列「config_snapshots、daemon boot」，无 projects |
| b | 初始 `task_spec_snapshots`（T2 组装，§5.1 明示「初始 T2 组装可空」即存在该行） | T2 分派 | `RecordHumanDecision` 只覆盖 `/sift ask` 产生的新 snapshot；`TransitionRun` 允许写入为「runs + events + 可选 outbox/budget/idempotency」，不含 snapshots |
| c | forge API 预算收费（§9.1 `kind=forge_api`） | outbox worker 执行 `forge_comment`/`merge_change` 等动词同样消耗 API；Gate 输入组装的 `getChecks` 等也是 API 调用 | `CompleteOutboxAttempt` 允许写入仅 outbox 三表；`PersistIntakeBatch` 未列预算。收费口（DESIGN §9.2「只在 Forge 适配层」）没有对应的存储端口 |
| d | 通用 Interrupt 的升级与关闭：`escalate`（version+1、nonce 轮换、escalation_count、expires_at）、`/sift hold`、非 Gate 非 `startup_stall`  reason 的指令关闭（`design_approval`/`agent_blocked`/`merge_conflict`/`failure_review`/`guardrail_violation` 的 approve/reject/hold） | Supervisor tick、Command | `EmitInterrupt` 描述的是创建（五件事）；`ResolveAttemptRace` 是 `startup_stall` 专用；`RecordHumanDecision` 面向 Gate 校准且允许写入不含 interrupts |
| e | `interrupt_deliveries` 状态推进（delivered/failed、attempt_count、remote_ref、delivered_at_ms） | outbox 投递完成 | `CompleteOutboxAttempt` 允许写入不含本表 |

实现阶段只能二选一：违反封闭清单随手加端口，或把不相干的写入塞进既有端口——两者都是 §11 想防的漂移。处置方向：为 a–e 补齐端口（或明确扩写既有端口的允许写入列），并同步 §16 验收与 WBS 崩溃注入清单。

### S3（P2）：hooks 指纹基线无存储位置

DESIGN §8.4/§9.1 要求 hooks 指纹覆盖 `.git/config`、`core.hooksPath` 取值及其最终指向目录内容，**每次 Agent 结束后复核**；WBS M3 §3.8 同。复核需要跨重启持久的基线，但全文无任何表存 per-repo（或 per-project）的 hooks 指纹基线——events 只能记录漂移结果，存不下「上次的基线是什么」。需新增投影表或声明挂接在既有投影（如 projects）上。

### S4（P2）：Report 限流是令牌桶，存储只有固定窗口模型

config.md §3.10 定义 `events_per_minute`（12）+ `burst`（4）——burst 是令牌桶语义。§9.1 `budget_counters` 只有固定窗口（bucket_start/end + limit + consumed）：1 分钟窗口限 12 的固定窗口允许窗口边缘瞬时 12 个，表达不了 burst=4。需要二选一：声明 Report 限流用固定窗口近似并回写 config 的 burst 语义，或为令牌桶状态（当前令牌数 + 上次补充时间）增加持久化列/表。

### S5（P3，可随 P1/P2 修订一并处置）

1. §10.1 `brain_traces.schema_version` 为 `TEXT`，与全库其余 schema version 列（events、config_snapshots、task_spec_snapshots、outbox、gate_input_snapshots、ledger 均为 `INTEGER`）不一致；§2.1 基础类型表也未为 brain 单独立例。
2. §5.3 `attempts.released_at_ms` 无任何语义与写入路径说明——释放的是什么（claim？worktree？隔离？）、由哪条路径写入，全文未出现。
3. 外键声明不一致：§10 各表（brain_traces、gate_evaluations、calibration_entries、ledger_entries）的 `run_id NOT NULL` 均未标 FK runs；`attempt_probes`、`report_receipts` 的 `(run_id, attempt_no)` 未声明对 attempts 的组合外键（§5.1/§5.3 自己已确立组合外键保证归属的先例，应一致执行）。
4. §12.5 结果事务第 4 步「Interrupt → closed」未指定写入的 `close_reason`——现有枚举（responded / expired_auto_reject / superseded_by_fact / superseded_by_decision / external_fact）中哪个对应「探测成功关闭」，需点名。
5. `runs.failure_reason`、`projects.isolation_reason`、`attempts.isolation_reason` 未声明是枚举还是自由文本；§2.1 要求 enum 列带 CHECK，自由文本则应明示。PRD §4.5 已给出 `closed_upstream`/`change_closed`/`untriggered` 等既有取值，建议枚举化。
6. 回放集 JSONL 导出格式未定义。DESIGN §15 把「回放集导出格式」明确划归 storage.md；若有意留待 M4 增补，应在 §10 或 §16 写明，否则属缺项。
7. probe 的执行路径未声明：`attempt_probes.state` 由哪个调度器推进、是否伴随 outbox operation（§8.1 kind 枚举无 probe 类；DESIGN §10.1 与 ADR-013 的 CAS 前置提到「probe operation」），需一句话归属。
8. critical 熔断窗口（config §3.9 `critical_fuse.window: 15m`）是滑动语义，`budget_counters` 是固定桶——近似口径未声明。
9. §5.2 约束「`done` 必须有 `change_id`」与 manual source + `project_id NULL`（无 forge 的手工任务）的组合边界未澄清：此类 Run 如何合法到达 `done`。
10. §15 索引清单 `runs(project_id, status)` 行缺右反引号（编辑性）。
11. 建议项：§8.1「payload 一经创建不可改」目前仅靠纪律，可用与 calibration 同法的受限列更新 trigger 加固，与 §13 的「结构上无法绕过」立场一致。

## 3. config.md 发现

### C1（P2）：`agents[].max_concurrent` 与 `runtime.default_agent_max_concurrent` 的解析规则缺失

§3.2 `agents[].max_concurrent` 默认 `1`；§3.6 `runtime.default_agent_max_concurrent` 默认 `1`。前者有默认值之后，后者永远不会被读到——它要么是为「Agent 定义省略该字段」准备的回退值（则 §3.2 的默认应写成「回退到 runtime.default_agent_max_concurrent」），要么就是死配置应删除。两者并存且各自带默认，解析顺序未定，V12「任一可选字段缺少默认值即失败」也无法发现这种「默认互相覆盖」的情形。

### C2（P2）：`certification.*` 是否可被项目 policy 覆盖未钉死，且阈值变更与 `certification_version` 的联动未声明

§3.11 把认证阈值（`total_samples_min`/`negative_samples_min`/`leak_rate_max`/`false_block_rate_max`/`window`）与 policy 缺省（`review_policy`/`auto_merge` 等）并列在 `gate_defaults` 下，表头只说「本节只规定全局缺省及认证数值」。若 policy.md 将来允许项目 policy 声明这些阈值，证据门槛本身就被它要约束的对象参数化了——项目把 `negative_samples_min` 调成 1 即可让 §5.6 的生效条件自我满足，「提权需证据」循环失效（PRD §5.7）。需明文：认证阈值是全局专有配置、不属于 policy 字段全集。同时按 ADR-004/storage §10.6（`certification_version` 含窗口与阈值版本），阈值变更应导致 `certification_version` 变化并使旧 Gate 缓存失效——这条联动在 config 侧值得点名一次。

### C3（P2）：`~/.sift/context.md` 未列入 §2.1 路径契约

PRD §5.7 规定全局 context 在 `~/.sift/context.md`，WBS M1 §1.7 要求 Task Spec 的 Context 由「base/全局/任务附注」组合。§2.1 声称定义两平台统一布局，但表中没有该文件（也没有其权限模式）。路径契约漏了一个 M1 就要读的文件。

### C4（P2）：每 Run Interrupt 子配额的适用范围与触顶处置未定

§3.9 有 `per_run_daily_quota: 4`，但：(a) 它是作用于该 Run 的全部 Interrupt，还是仅 PRD §5.8/TM5 语境下由 Agent 上报驱动的 Interrupt，未声明——两种语义不同（后者是防洪水闸，前者可能误伤正常的多检查点 Run）；(b) 触顶后的确定性处置无默认值——PRD §5.8 给的是「转一次 HITL **或**直接 `failed`」的二选一，§14.2 纪律要求在此拍板一个确定规则（或显式划归 interrupt.md/report.md，但需唯一归属）。另：`per_run_daily_quota` 是否计入 critical（不占日配额但受熔断 per_run_limit 约束）也需一句话。

### C5（P3，随 P2 修订一并处置）

1. §2.1 布局表止于 `runs/<run_id>/` 目录，其内文件（`control.json`、`bootstrap.json`、`heartbeat`、`result.json`、`agent.log`）的模式与生命周期未登记也未显式委派——`bootstrap.json` 的 0600 + 读后即 unlink 是 TM6 收窄措施（DESIGN §8.4），`control.json` 含 run token 须 0600（DESIGN §8.9），应在路径契约登记或写明归属 control-plane/runtime spec。
2. §4 漂移监测的周期与机制未定（每次 supervisor tick 重算？基于 mtime 快筛？）；启动时文件不存在（`source_present=false`）、运行期新出现配置文件是否算漂移，未声明。
3. 既有 `config.yaml` 权限宽于 `0600` 时的处置（拒启/告警/自动修正）未定；§2.1 只规定了目录拒启条件。
4. §5.1.4 探测「每个已定义 Agent executable」与 §5.1.7 forge「未引用的不探测」不对称：仅被禁用项目引用（或暂未被任何项目引用）的 Agent 缺 executable 也会拒启。若有意为之（agent 定义少、closed contract），值得一句注记；否则应对齐为「只探测被启用项目引用的 Agent」。
5. `brain` 无 `version_args` 等价物，§5.1.5 的探测 argv 未定（agents 有，brain 没有）。
6. §5.2.5「运行期 forge `AuthOrCapability` 失效」是运行期规则，列在「启动探测」节下（沿袭 DESIGN §11 项目级表的写法），建议注明它持续生效而非一次性探测。
7. `hold_max_duration`（720h）语义未定：单次 hold 上限还是累计上限。
8. 单实例互斥（§5.1.1）的实现机制未声明（锁文件？DB 级互斥？），至少应有一句归属。
9. §3.2 `task_transport: file` 前向引用「Runtime spec」，但 DESIGN §15 与 WBS 派生文档清单中无此 spec（wrapper/启动契约归 `control-plane.md`），引用目标需更正或新增。
10. canonical JSON 的定义在本文 §4.6 与 storage.md §1.8 各写一份（且后者多「拒绝 NaN/Infinity」），违反 docs/README「引用不复制」，应以一处为准、另一处链接。

## 4. 评审范围与方法

按 docs/README「评审/设计讨论」上下文集加载 PRD、DESIGN 全量及被引 ADR/WBS，逐节核对两份 spec 的以下性质：与上游约束的一致性（PRD §4–§5、§9、§12，DESIGN §6–§11、§14，ADR-002/003/004/009/010/013）；spec 内部自洽（字段默认值、枚举、约束交叉引用）；可实现性（M1 任务与门禁能否按 schema/端口直接落地）；docs/README 的文件头、命名与「引用不复制」纪律。

## 5. 逐项核对通过项（无发现处）

| 核对项 | 结果 |
|--------|------|
| `attempt_resolution` 规范名 + V0 枚举 `reject \| retry_after_absence`（DESIGN §14.14 / ADR-013） | storage §1.10、§5.3、§12.5 一致落地；ADR-010 修订指针已在（WBS M1 前置已勾） |
| DESIGN §14.2 点名的启动协议五时限（lease TTL、等 permit、spawning 等待、终止升级序列、复核次数）+ Report `not_ready` 退避 | config §3.6/§3.10 全覆盖且有默认 |
| PRD §12 数值类开放问题（#3 认证阈值、#4 注意力配额、#6 静默超时、#8 标签名、#9 allowlist 形式、#11 指标权重、#12 熔断、#14 max_escalations） | config §3.1/§3.6/§3.9/§3.11/§3.12/§3.13 均有确定性默认值 |
| PRD §4.2 on_expire 默认表与 `startup_stall` 双重禁止 `auto_reject`、禁用 `auto_approve` | config §3.9 一致 |
| 两级启动探测与 DESIGN §11（含「只探测被引用 forge」）、V12 零配置两场景、doctor 0/1/2 退出码与 offline 只读 | config §5/§6/§7 一致 |
| 敏感配置不热加载 + 指纹 + 漂移只告警（PRD §13.1 / H16） | config §1.3/§4 一致 |
| Interrupt 五件事同事务、生成键幂等、升级不重复扣费 | storage §12.2、§6.1 一致 |
| ADR-013 结果事务八步 CAS、probe 不越权关 Interrupt/建 attempt | storage §12.5、§5.5 一致 |
| outbox claim/complete 的 lease CAS 与旧 owner 结果拒绝、恢复完成前禁 claim `launch_agent` | storage §8/§12.4 一致 |
| append-only trigger 清单与各表可变性 | §13 清单核对无遗漏（mutable 表均未误列） |
| 权限模式表（$SIFT_HOME 0700、db/sock/token 0600）与 storage §2 打开契约 | 一致 |
| 事件不可变、seq 仅本库顺序、外部事实/指令分离（receipt disposition 四值） | §7 与 PRD §4.5/§9.2 一致 |
| WBS M1「先写 spec」清单与 M1 §1.2–§1.5 验收映射 | 两份 spec 的 §8/§16 验收映射可追溯 |

## 6. 建议动作

1. **storage.md**：修订 S1（brain_traces 身份域）与 S2（§11 端口补 a–e）两项 P1 后做定向复评；S3/S4（P2）与 S5（P3）同轮处置。
2. **config.md**：修订 C1–C4（P2）即可转 `active`；C5（P3）同轮处置。可与 storage 复评合并核销。
3. 两份文档的 P3 均不单独设评审轮次。
4. 处置对账写回被评审文档（reviews 原文保持只读）。
