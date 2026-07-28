# WBS D0.2 独立复评

> 日期：2026-07-28  
> 评审人：pi × GPT-5.6 Sol  
> 评审对象：[`docs/WBS.md`](../WBS.md) D0.2（commit `79a0c12`）  
> 基线：PRD V0.8、DESIGN D0.10、ADR-010～013，以及四份 D0.1 WBS 评审

## 1. 结论

**暂不通过：2 项 P1 阻断，4 项 P2，2 项 P3。**

D0.1 的主要缺口已实质修复：Brain T1–T7、ADR-013 仲裁/隔离/retry 两段式、配置生命周期、Ledger/指标、launcher/版本边界均已有工作包；V/A 验收也改为单一权威归属。前一轮四份评审提出的主体问题可以核销。

但 D0.2 仍有两处后向依赖，直接违反本文自己的执行纪律 1。修复量不大，但修复前不能把 WBS 作为进入 `plans/` 和编码的 active 基线。

## 2. P1 阻断

### F1：M4 的 Gate HITL 依赖 M5 才支持的 Interrupt reason

**证据链：**

- M3 §3.6 交付的是 `startup_stall` 安全最小集；其 spec 任务也写成“先落 startup_stall 最小契约”。
- M4 §4.3 要求 Gate 需要 HITL 时，调用 M3 发射器并把预判与 Interrupt 五件事同事务提交。
- Gate 可能产生 `guardrail_violation`、`code_review`、Checks/failure 等非 `startup_stall` reason。
- M5 §5.2 才写“扩展 M3 唯一发射器到全部 reason”。

因此 M4 无法在 M5 前独立通过；若只用临时 Interrupt 或跳过事务，又违反单一发射入口与影子预判同事务约束。

**修复要求：**M3 的发射核心必须从第一天就是**泛型 Interrupt 领域入口**，支持 PRD 全部 reason 的确定性 fallback 结构与 forge 评论发布；M3 重点验 `startup_stall`，但不能只支持它。M5 只增加 T4/T6、Channel、调度与熔断，不再“扩展到全部 reason”。`specs/interrupt.md` 在 M3 先定义全部 reason 的最小确定性契约。

### F2：V11 的指标分母验收仍从 M5 后向依赖到 M4

**证据链：**

- M4 门禁要求 V11 完整段验证“指标分母排除正确”。
- 自动化门禁权威表也把 V11 最终闭合写为 M4。
- 误放行率、门禁绕过率等指标查询直到 M5 §5.7 才实现。
- M5 前置要求 M4 门禁通过。

M4 可以验证 `done + gate_bypassed` 与 Ledger 分类字段，但不能验证尚不存在的指标查询分母。

**修复要求：**将 V11 拆成三段：M2 外部事实收敛；M4 Gate/审计/Ledger 分类；M5 指标分母最终闭合。M4 门禁只要求前两段，权威表的最终闭合改为 M5。

## 3. P2 发现

### F3：CLI 与 doctor 工作包在 D0.2 重写中不完整

PRD §7.2 要求 `ps/logs/kill/retry/worktree/doctor`。当前 WBS 分散提到 ps/logs、kill/retry 和 doctor 输出，但没有明确交付薄 CLI、`worktree` 命令、daemon 不可用时“标记离线且只读、绝不直改 DB”的行为。

`doctor` 也漏了 DESIGN §8.10 的完整基础检查：SQLite、Agent CLI、相关 forge CLI 登录/版本、可选 tmux、目录/socket 权限等。M8 只列安全与积压类输出。

**要求：**M1 增 CLI 基础与离线只读诊断；M3/M5 接入运行语义；M8 按 DESIGN §8.10 全清单做最终验收。

### F4：Command 未明确覆盖通用指令效果

M5 §5.4 详细展开了 `startup_stall`，但未明确实现其他 reason 下的 `/sift approve`、`reject`、`retry`、`hold`、`ask` 效果。特别是 `/sift ask` 目前只写入 Ledger，没有“注入当前 Task Spec 并继续”的任务。

**要求：**在 M5 增 PRD §7.1 全指令集的 reason-specific DomainCommand 映射；`ask` 同事务写语义原料并更新当前 Run 的任务层 Context，随后按当前 Interrupt 语义继续。

### F5：Gate 的软护栏豁免契约遗漏

M4 只写默认硬护栏，未承接 PRD §5.4 的软护栏“一次性（默认）/记住（显式独立选项并写项目配置例外）”。

**要求：**补 M4 任务与 `specs/gate.md` / `specs/policy.md` 验收；硬护栏永远不能进入该路径。

### F6：手工合并对 Ledger 的校准写入未点名

M4 有通用 `recordHumanDecision`，但没有明确把 forge 手工合并视为人的实际决定：Gate 已有预判时应保留校准样本，同时标 `gate_bypassed`；该样本不进入 Sift 自发合并的误放行率分母。

**要求：**在 M4 V11 段点明这条接线和测试。

## 4. P3 发现

### N1：WBS 状态元数据自相矛盾

文件头是 `status: active`，正文与页尾是 `draft / 待独立复评`。本次结论不通过，因此应统一为 `draft`。

### N2：指标数量写成八项，枚举实际为九项

PRD §10.2 当前有九行指标（新增门禁绕过率后变成九项），WBS 和 DESIGN §9.3 仍写“八项”，但 WBS 实际枚举了九项。这是计数陈旧，不影响机制。

**要求：**WBS 改为“PRD §10.2 全部指标（当前九项）”；DESIGN 后续做事实性修正时避免继续写固定数量。

## 5. D0.1 复评条件核销

| 条件 | 结论 |
|------|------|
| Brain 调用壳、T1–T7、兜底、trace、token 收费 | 已关闭 |
| ADR-013 resolution、隔离、四入口仲裁、retry 原子事务 | 已关闭 |
| 配置不热加载、两级探测、有效策略、V12 | 已关闭 |
| launcher、版本握手、`SIFT_RUN_DIR`、单实例、零网络监听 | 已关闭 |
| V5 分段、V9/V10a 分阶段、A3/A5 归属 | 已关闭 |
| 恢复矩阵全集、ADR-012 资格门控、merge capability 禁用 | 已关闭 |
| Ledger 人类结果、语义原料、认证投影、指标工作包 | 主体关闭；见 F2/F6 |
| M1–M8 无后向依赖 | **未关闭：见 F1/F2** |

## 6. 复评通过条件

1. 关闭 F1、F2，两处里程碑后向依赖消失。
2. F3–F6 进入明确任务、spec 与验收，不静默下沉给实现者。
3. 修正 N1；N2 可同期修正。
4. 修订后重新执行 V/A/spec/module 机械追溯与 `git diff --check`。

满足以上条件后，可将 WBS 标为 `active` 并进入 M1 specs；无需再重做一轮全面架构评审。
