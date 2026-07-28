# Sift 架构设计评审报告（D0.1）

> **评审对象**：[`docs/DESIGN.md`](../DESIGN.md)（版本 D0.1，2026-07-27）
> **核心约束**：[`docs/PRD.md`](../PRD.md)（版本 V0.2，2026-07-27）
> **评审日期**：2026-07-27
> **文档位置**：`docs/reviews/2026-07-27-design-review-hex-01.md`

---

## 1. 评审结论 (Executive Summary)

**评审结论：【通过 / 优秀】**

[`docs/DESIGN.md`](../DESIGN.md)（版本 D0.1）高度严谨且系统地承接了 [`docs/PRD.md`](../PRD.md)（版本 V0.2）所定义的所有核心设计公理 (A1–A7)、硬性约束、状态模型、安全信任边界与非功能需求。

架构的核心亮点在于**用结构性保证替代纪律性约束**（“靠没有那个函数保证，而不是靠 review 记得拦”），具体体现在：
1. **Gate 纯函数化**（§8.5）：冻结输入快照，彻底解耦网络 IO，使影子门禁与离线回放集白拿。
2. **Reconciler + Transactional Outbox**（§6.1/§6.4）：消除“写库成功但 Forge 调用失败”的悬挂状态，从结构上解决死锁与幽灵 running。
3. **Sift 自身 Git 操作的防投毒防护**（§8.4）：显式带有 `-c core.hooksPath=/dev/null`，相比 PRD 的事后指纹校验实现了**事前失效**。
4. **Report 通道 CLI 化**（§8.9）：收敛外部集成面，避免 harness 绑定。

---

## 2. PRD 核心约束契合度逐项核查

### 2.1 设计公理 (A1 – A7) 承接对账

| 公理 | PRD 核心要求 | DESIGN 架构承接机制 | 结论 |
| :--- | :--- | :--- | :---: |
| **A1** | LLM 有话语权，无决定权；代码做裁定 | §6.2 强类型隔离 (`Recommendation` $\rightarrow$ `DomainCommand` $\rightarrow$ `Transition`)；§8.5 Gate 与 §8.7 severity 纯函数；§8.8 手写指令解析器 | **完全对齐** |
| **A2** | Forge 是事实源，本地只存运行时 | §3.1/§6.1 Reconciler 架构以 Forge 为事实源；§7 数据库只保留本地 Run 状态与事件 | **完全对齐** |
| **A3** | 通知是核心功能（主动推送） | §8.7 Attention 模块与 Transactional Outbox 确保推送可靠性 | **完全对齐** |
| **A4** | 最快闭环优先于架构完备 | §2.1 C9；§13 交付切片（纵向切片，第 1 片即包含 Fake Forge/Agent 端到端） | **完全对齐** |
| **A5** | 隔离仅限 worktree 内；退出码裁定 | §8.4 Worktree + Wrapper 契约；§9.1 诚实声明 TM6（Worktree 外不受操作系统级保护） | **完全对齐** |
| **A6** | 打扰结构化 (options $\le 4$、可朗读) | §8.7 Attention 发射器硬约束（超过 4 选项拒发）；TTS Renderer 入口拦截 `min_modality: visual` | **完全对齐** |
| **A7** | 积累校准证据，绝不做自动放行模型 | §8.6 Ledger 账本只有 T7 提案生成与指标计算两个消费者，**架构上无反向流向 Gate/Attention 的路径** | **完全对齐** |

---

### 2.2 PRD §13.1 已采纳架构约束对账

| PRD §13.1 约束 | DESIGN.md 落实情况 |
| :--- | :--- |
| **1. Forge 集成走 CLI `api` 子命令** | DESIGN §8.1 封装 `gh api` / `glab api` (plumbing)，统一中性类型。 |
| **2. Change 由 Sift 创建，Agent 只提交分支** | DESIGN §8.4 Reconciler 检测分支新提交后调用 `createChange`，Agent 全程不接触 Forge 创卡 API。 |
| **3. 策略从 Base 分支读取，不读 Worktree** | DESIGN §8.4 规定读取逻辑为 `git show <base>:.sift/policy.yaml`，代码中不存在读取 Worktree 内策略的函数。 |
| **4. Actor 经 events/timeline 回溯，取不到即 fail closed** | DESIGN §8.1 适配器类型中 Actor 为必填，取不到直接丢弃/拒绝。 |
| **5. 敏感配置启动期读入 + 指纹校验，不热加载** | DESIGN §9.1 明确敏感配置只在启动期加载，改动需重启 `siftd`。 |

---

### 2.3 威胁模型 (TM1 – TM6) 与安全承接

- **TM1 – TM5 (Forge 交互与上报防护)**：DESIGN §9.1 矩阵完全承接。特别是针对 TM5（注意力耗尽攻击），DESIGN 建立了“Report 入口限流 + 子配额 + 发射器总配额 + Critical 熔断”四重防护。
- **TM6 (Worktree 外未闭合边界)**：DESIGN §9.1 和 [ADR-007](../decisions/007-tm6-minimal-credential-sandbox-direction.md) 采纳了“最小凭证沙箱”方向，且 V0 如实声明 `unsafe-local`，并提出了凭证形态 Spike 的**证伪条件**，态度极度客观严谨。

---

### 2.4 成功标准 (PRD §10.1) 与验证策略 (DESIGN §12)

DESIGN §12 定义了 V1–V9 九套测试方案，覆盖属性测试、崩溃注入、Forge 契约测试、Runtime 故障注入、Gate 安全/回放测试与预算测试。PRD §10.1 中的所有 PoC 验收项（除真实双平台连线外）均被**转化为 CI 自动化门禁**，避免了人工演示脚本的脆弱性。

---

## 3. 隐性风险与技术细节微调建议

在架构设计高度成熟的前提下，针对后续下沉到 `specs/` 编写及具体实现阶段，提出以下 4 点建设性微调建议：

### 3.1 Reconciler Tick 队列解耦建议 (针对 DESIGN §6.1)
- **现状**：DESIGN §6.1 采用自适应轮询（60s / 15s / 10s）。若触发 Forge API 限流，Intake 轮询速度降低。
- **隐患**：如果 Tick 是单一主循环，Intake 的降速可能会连带拖慢 `waiting_human` 的超时扫描与 Outbox worker 的重试派发。
- **建议**：在 `specs/storage.md` 或 `specs/config.md` 细节定义中，显式将 Tick 拆分为三组独立步频的子调度器：
  1. `Intake Tick`（自适应 Forge 轮询）；
  2. `Heartbeat & Timeout Tick`（高频固定周期：如 2s，专门扫描 HITL 超时与进程存活）；
  3. `Outbox Worker Tick`（事件驱动 + 指数退避）。

### 3.2 `SIFT_RUN_TOKEN` 传递安全性增强 (针对 DESIGN §8.9)
- **现状**：DESIGN §8.9 使用环境变量 `SIFT_RUN_TOKEN` 供 Agent 进程调用 `sift report`。
- **隐患**：同属主下的其他非沙箱进程可以通过 `ps e` 或 `/proc/<pid>/environ` 窥探到环境变量中的 Token。
- **建议**：在 `specs/report.md` 中补充推荐逻辑：优先通过落盘的 `control.json`（权限 `0600`）只读文件传递 Token，或通过 stdin 注入，以最大程度减少命令行环境变量曝光风险。

### 3.3 影子门禁 (Shadow Gate) 预测快照的事务原子性 (针对 DESIGN §8.5)
- **现状**：PRD §5.6 要求“在人做决定**之前**先写下预判”。
- **建议**：在 `specs/gate.md` 中强化规定：当 Reconciler 判定 Run 需要转入 HITL 时，**必须在生成 Interrupt 并写入 Outbox 的同一数据库事务中，调用 Gate 纯函数并将输入快照与预判结果写入 `calibration_log`**。严禁延迟到用户回复评论时才去补算预判（避免 Checks 或 Base 分支发生漂移导致快照不对齐）。

### 3.4 `sift doctor` 退出码约定 (针对 DESIGN §8.10)
- **建议**：在 `specs/config.md` 中明确 `sift doctor` 的 Exit Code 规范（如 0 表示完全健康，1 表示包含策略漂移 Warning，2 表示包含 Schema 校验错误 Error）。确保在 CI 或 Git hook 中，诊断脚本可以作为强门禁使用。

---

## 4. 后续行动方案 (Next Steps)

1. **确定 PRD 修订回写 (DESIGN §14.3)**：确认 PRD 中关于 Report 模块 CLI 化及 Forge 零凭证星号标注的文字修订已全部同步完成。
2. **推进派生 Specs 文档编写 (DESIGN §15)**：
   - 优先推进 `specs/gate.md`（定义判定顺序与默认硬护栏路径）；
   - `specs/forge.md`（双平台 API 契约与 Actor 强制提取）；
   - `specs/storage.md`（数据库 Schema 与 Outbox Key 定义）。
3. **按照交付切片 (DESIGN §13) 编写 WBS**：将凭证形态 Spike（验证 `claude` / `codex` 的凭证落盘机制）作为 Slice 7 的前置卡位任务。
