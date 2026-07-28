# WBS D0.1 评审

**评审对象**：[WBS.md](../WBS.md) D0.1  
**基线**：[PRD.md](../PRD.md) V0.8、[DESIGN.md](../DESIGN.md) D0.10、[ADR-010](../decisions/010-attempt-spawn-handoff.md)、[ADR-013](../decisions/013-startup-stall-retry-convergence.md)  
**日期**：2026-07-28  
**结论**：**不通过，需修订后复评。**

WBS 已覆盖八个交付切片的大部分结构性约束，尤其是 Gate 纯函数、双端点授权、expected-head CAS、三类预算、回放集同片交付和跨平台发布。不过，当前版本仍有 7 项阻断问题和 2 项非阻断问题。最关键的缺口是：Brain 没有任何交付任务，`startup_stall` 的安全收敛既未完整分解又出现错误简化，多处里程碑验收依赖尚未实现的后续切片。因此当前 WBS 还不能作为可执行基线。

## 阻断问题

### F1 — Brain 七触点没有工作包、spec 或验收归属

[WBS §M4](../WBS.md#m4门禁对应-design-第-4-片) 只把 T3/T5 当作 Gate 的既有输入，[§4.4](../WBS.md#44-回放集导出) 只消费 Brain trace，[§5.5](../WBS.md#55-三类预算) 只给“Brain 调用壳”收费；全文没有实现统一调用壳、T1–T7、Task Spec、逐触点确定性兜底、提示词/schema 版本、trace 写入的任务，也漏了 DESIGN 派生清单明确要求的 `specs/brain.md`。

这会直接导致：PRD §3.1 的“一次 LLM 分派”无法交付；T2 无法产生 Agent/Goals；M4 没有 T3 风险分和 T5 失败分诊的生产者；M5 没有 T4 决策简报与 T6 调度；回放导出声明了一类永远不会被写入的数据；M7 的真实闭环无分派路径。

**要求**：新增 Brain 工作包与 `specs/brain.md`。至少明确：

- T1/T2 在首次可执行闭环前落地；
- T3/T5 在 M4 Gate 验收前落地；
- T4/T6 在 M5 Attention 验收前落地；
- T7 的提案边界、A7 防火墙及其与 Ledger 的依赖；
- 统一调用壳的 schema 校验、同 prompt 重试一次、逐触点兜底、token 收费、调用身份和 trace 持久化测试。

### F2 — `startup_stall` 的收敛协议未被分解，且“迟到事实”规则写错

[WBS §3.6](../WBS.md#36-受控终止流程) 把第三种结局写成“迟到 `started`（事实优先）→ 吸收身份后继续终止”。这不是 ADR-013 的规则：

- `attempt_resolution` 尚未落定时，合法 `started` 先到应推进 attempt，并使 Run `waiting_human → running`，Interrupt 以 `superseded_by_fact` 关闭；此时不能继续终止一个已被事实确认的正常执行体。
- `attempt_resolution=reject | retry_after_absence` 已先落定时，才由决定吸收迟到事实、登记身份并继续终止旧执行体。

此外，WBS 只在表结构中提到 `attempt_resolution`，没有分解 ADR-013 要求的关键实现：`reject` 后保持隔离且 worktree 不回收/不复用；`retry` 请求与探测结果两阶段；探测失败复用同一 Interrupt；探测在途拒绝新指令；成功结果以单一 CAS 事务提交消失证据、marker、隔离解除、Interrupt 关闭、Run 回 `queued`、新 attempt/claim、启动与回执 operation、事件；四个入口共享同一仲裁函数。M6 中列出测试名不能替代 M3/M5 的实现任务。

这是防止同一 worktree 双写和“终态掩盖活进程”的安全不变量，不能只靠引用 DESIGN 带过。

**要求**：在 M3/M5 之间给出明确工作归属；补 attempt 隔离字段/生命周期、完整仲裁任务和逐事务验收，并改正 §3.6 的迟到事实描述。

### F3 — 多个里程碑依赖后续切片，当前验收不可执行

当前线性前置关系要求每一片验收通过后才能进入下一片，但验收项跨越了尚未实现的能力：

| 当前声明 | 冲突 |
|---|---|
| M1 fake 闭环为“Issue → 分派 → 执行 → Gate → 合并” | Forge 在 M2、Runtime 在 M3、Gate/Change 创建在 M4；未定义 M1 使用哪些 fake/stub 契约 |
| M2 验收“双平台摄入→门禁流程” | Gate 仍要到 M4 才实现 |
| M3 验收“.sift/** 硬护栏拦截” | 拦截者 Gate 在 M4；M3 此时只能验证 base/worktree 读取源 |
| M3 验收“两个后端同一套断言”，括注却说仅 `process` | 第二个后端到 M6 才存在，验收项自身矛盾 |
| M4 验收“预判与 Interrupt 同事务” | Interrupt 发射器和五件事事务到 M5 才实现 |
| V10a 归 M1 完整验收 | acquire/permit/started 直到 M3 才有完整语义 |

**要求**：要么为前置切片明确定义最小 fake/stub 接口及其可执行验收，要么把验收拆成“本片可验部分 / 后续集成复验”。M1–M4 的完成条件必须只依赖截至该片已经实现的能力，且 V5、V10a 等跨片测试要标出首次可运行与最终闭合的不同时间点。

### F4 — 配置与有效策略只有“写 spec”，没有实现任务

WBS 有 decode gateway、`specs/config.md`、`specs/policy.md` 和 `doctor` 检查，但没有负责以下行为的工作包：

- `SIFT_HOME` 与统一路径解析；全局配置启动期加载；敏感配置指纹；运行期不热加载；磁盘漂移告警且拒绝生效；
- 进程级启动失败与项目级隔离的分级：SQLite/全局配置/相关 forge CLI 失败拒启，单项目 policy 无效或运行期 `AuthOrCapability` 只隔离该项目并告警一次；
- 有效策略组装：base 分支 policy ∪ 全局缺省，再按认证证据剔除未达标的提权项；
- Agent 定义校验、`max_concurrent`、未知 Agent 拒启和非 worktree 模式项目互斥；
- V12 零配置启动。V12 只出现在汇总表，没有 M1 任务或 M1 验收项；A8 的“至少两个 Agent 定义均通过校验”也只有最终人工检查，没有生产该能力的任务。

缺少这些实现，H8/H11、PRD §9.1 的敏感配置边界、PRD §10.1 多 Agent 标准和 M4 的 `auto_merge` 证据门槛都没有落点。

**要求**：新增配置/策略生命周期工作包和分级启动测试；将 V12 纳入 M1 明细验收；明确有效策略是在 Gate 外组装并作为冻结输入传入，避免 Gate 直接读取认证或配置。

### F5 — `max_escalations` 达上限后的统一 `hold` 与 PRD 冲突

[WBS §5.2](../WBS.md#52-超时与升级) 写“达上限封顶 severity + 落 `hold`”。PRD §4.2 的规则是**按 reason** 落 `auto_reject` 或 `hold`；只有 `startup_stall` 明确禁止 `auto_reject` 并必须落 `hold`。当前写法会让本应在上限后自动拒绝的 Interrupt 永久保持待决。

**要求**：改为“达上限后 severity 封顶，并按 reason 的确定性映射进入 `auto_reject` 或 `hold`；`startup_stall` 强制 `hold`”，并为两类结局各补状态机/超时测试。

### F6 — Ledger 没有人类结果写入路径，认证投影与 V0 指标无法成立

[WBS §4.2](../WBS.md#42-影子门禁记录器) 只记录 Gate 预判与快照；[§4.3](../WBS.md#43-认证投影) 随即要求从“校准数据”计算双向阈值，但没有任务把人的实际决定、一致性、负样本、触碰路径/文件类型、Issue 作者、打扰特征，以及 `/sift reject`、`/sift ask` 的自然语言原料写入 Ledger。Command 也没有“决定落库后回填校准样本”的职责。结果是认证投影没有合法、完整的数据源。

同时，PRD §10.2 要求 V0 即打点的指标在 WBS 中没有工作包或验收：加权打扰/已合并 Change、误放行率、门禁绕过率、Gate 漏放/误拦、HITL、配额消耗、分派准确率、LLM 成本。PRD §9.3 的“打标→Agent 启动 P50 < 60s”也没有测量与验收项。

**要求**：补 Ledger 写入与人类结果关联任务，明确与认证投影同事务的边界；补事件到指标的确定性投影/查询、`sift ps`/日志时间线落点和性能验收。至少要能证明认证分母、负样本绝对数及 `gate_bypassed` 排除口径正确。

### F7 — 发布冒烟要求了尚未安排实现的运行时/版本契约

[WBS §8.1](../WBS.md#81-构建与分发) 要求测试 manifest/版本握手和 wrapper handoff，却没有任务实现 DESIGN §11 的两条运行时契约：`siftd` 只从自己的安装目录解析同版本 wrapper，不从 `PATH` 猜；CLI/daemon/wrapper 握手携带版本且主版本不一致拒绝执行并由 `doctor` 报错。[WBS §3](../WBS.md#m3runtime对应-design-第-3-片) 也漏了 ADR-007 要求的单一 launcher 接缝，以及 Report 链路只向 Agent 环境注入非机密 `SIFT_RUN_DIR` 的任务。

此外，单机单实例、零网络监听端口是 PRD §9.3 的硬约束，但 WBS 没有第二实例拒启和“仅 Unix socket、无 TCP listener”的实现/验收。

**要求**：把 launcher、安装目录解析、三方版本握手、`SIFT_RUN_DIR`、单实例互斥和零网络监听加入 M1/M3/M8 的对应任务，并让 V15 冒烟有明确的被测实现。

## 非阻断问题

### N1 — M8 的“真实项目稳定运行 ≥3 天”是无来源的新发布门槛

[WBS §M8 前置](../WBS.md#m8发布对应-design-第-8-片) 新增“至少一个真实项目连续稳定运行 ≥3 天”，PRD/DESIGN 没有该要求。它可以作为项目管理 readiness gate，但当前文首声明“硬约束全部来自 PRD/DESIGN，本文不做新增设计决策”，二者不一致。

**建议**：要么删除，要么明确标为“WBS 自定的发布准备条件（非 PRD PoC 成功标准）”，并定义“稳定运行”的可判定口径、证据与失败后的重跑规则。

### N2 — 汇总表没有完全回灌到各里程碑的验收清单

V11、V12 在自动化门禁汇总中分别归 M2、M1，但 M2/M1 的明细验收表未列出；A5 在人工验收汇总归 M3，而实际 Gate 拦截应在 M4 才可闭合。汇总与明细一旦分叉，执行时通常只会照最近的里程碑清单勾选，导致门禁被漏跑。

**建议**：每个 V/A 只定义一次权威验收归属；其他位置使用链接或明确“首次执行 / 持续回归 / 最终发布证据”，避免重复表述漂移。

## 复评条件

1. F1–F7 均在 WBS 的“评审处置对账”中逐项给出处理结论和落点；本 review 原文不回填。
2. M1–M8 每一片都能在只依赖已完成前置片的条件下独立通过其验收。
3. PRD §8 的十个模块均至少有一个明确工作包；DESIGN §15 的每份待写 spec 均有里程碑归属。
4. V1–V15 与 A1–A10 均能从“实现任务 → 测试/证据 → 里程碑门禁”单向追溯，不存在只在汇总表出现的验收项。
5. 不新增未标注来源的产品/发布硬门槛；WBS 自定的项目管理条件必须与 PRD 成功标准明确区分。
