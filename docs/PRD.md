# Sift — 产品需求文档

> **Sift**：本地多 Agent 任务编排与门禁中枢  
> **版本：V0.1（PoC / 从 0）** | **日期：2026-07-27**  
> **仓库：** https://github.com/miaoxiaoyong/sift

本文档只定义**需求与边界**。技术设计、任务拆解、实现另文跟进。  
前序概念验证（内部项目 JANUS）中已验证的方向予以吸收；命名、仓库与实现均不兼容、不迁移。

---

## 1. 问题与公理

### 1.1 要解决的问题

**把 GitLab Issue 变成已合并的 MR：能自动筛过的自动过，必须人类判断的绝不偷懒放过；并在需要人时主动找到人。**

| 资源 | 稀缺性 | 说明 |
|------|--------|------|
| **Human 注意力与判断** | 极稀缺 | 单用户；响应以小时计；审查必须严谨，不能「随便看一眼」 |
| Agent 算力 | 不稀缺 | 订阅制 coding agent，跑满更划算 |
| 机器资源 | 不稀缺 | 单机少量并发远未触顶 |

Sift 不是通用任务管理系统，也不是又一个 coding agent IDE。它是：

1. **筛子（Sift）**：用确定性门禁过滤不合格变更；  
2. **注意力调度器**：只在检查点打断人，并主动推送完整上下文。

### 1.2 命名

| 项 | 约定 |
|----|------|
| 产品名 | **Sift** |
| 含义 | 筛查、分辨优劣后再放行 |
| CLI / 守护进程 | `sift` / `siftd`（实现阶段再定） |
| 配置与数据目录 | `~/.sift/`（实现阶段再定） |
| 不使用背书式全称 | 名称即语义，不搞强制缩写展开 |

### 1.3 设计公理

与公理冲突的需求一律砍掉或推迟。

| # | 公理 | 推论 |
|---|------|------|
| **A1** | **判断交给 LLM，裁定交给代码** | 分类、选 Agent、提取验收标准、风险提示 → LLM；进程生死、worktree、门禁通断、状态合法性 → 确定性代码 |
| **A2** | **GitLab 是任务事实源，本地只存运行时状态** | Issue / Label / MR / CI 以 GitLab 为准；本地只存执行档案、进程、worktree、事件、HITL 队列 |
| **A3** | **通知是核心功能** | 进入 HITL 必须主动推送；不能只靠人想起来打开看板 |
| **A4** | **最快闭环优先于架构完备** | 先 1 Agent × 1 项目端到端跑通；用运行数据驱动下一功能 |
| **A5** | **隔离是物理保证，不是约定** | 每 Task 独立 git worktree；**退出码 + 门禁**裁定结局；Agent 自报仅供时间线 |

### 1.4 从前序验证中保留 / 丢弃

| 方向 | 处置 |
|------|------|
| Webhook 事件驱动 + 轮询兜底 | **保留** |
| Push 下发 Task Spec、双层上报（MCP 时间线 + 退出码裁定） | **保留** |
| Worktree 隔离、`protected_paths` 硬/软护栏 | **保留** |
| 5 状态任务机 + HITL reason | **保留**（实现时禁止再叠隐式第二套状态机） |
| 多 Agent 并存（Claude Code / Codex / Cursor 等）via 启动命令适配 | **保留** |
| 重型规则引擎、自建 Diff 审查、多用户多机、GitHub/Jira 适配 | **不做（V0）** |
| 与前序项目的数据/配置/API 兼容 | **不做** |

---

## 2. 用户与场景

### 2.1 用户

唯一角色：**系统管理员兼唯一 Human 决策者**（熟悉 GitLab / CI / CLI）。

V0 范围：单用户、单机。

### 2.2 核心场景

| 场景 | Human 做什么 |
|------|----------------|
| 需要决策时 | 收推送 → 打开 Issue/MR/上下文 → 一键批准或拒绝 |
| 日常巡视 | 看板或 CLI 查看排队 / 运行中 / 待审批 |
| 代码审查 | 在 **GitLab MR** 完成（Sift 不重造审查 UI） |
| 失败处理 | 看保留的 worktree 与日志 → retry / reassign / close |
| 接入项目 | 登记本地仓库路径、GitLab 项目、触发标签、webhook |

### 2.3 交互原则

- **系统找人，人不找系统。**
- **审批 ≤ 一次点击或一条命令**（通知链接 / 看板 / CLI 等价）。
- **人不在线时安全挂起**，不擅自越过检查点。
- **严谨优先于爽快**：低风险可配置自动合并；默认策略偏向需人确认（实现时默认值另定，须可配置）。

---

## 3. 范围

### 3.1 V0 必须具备（PoC 成功标准）

端到端跑通至少一次：

1. GitLab Issue（带触发标签）→ 本地创建 Run（queued）  
2. Brain：一次 LLM 分派 → 选定 Agent + Goals（可 HITL 开工前审批）  
3. Runtime：worktree + 启动 Agent + 进程监督  
4. Agent 产出 MR → Gate：`protected_paths` + CI + review 策略  
5. 需人时推送通知并可一键审批  
6. MR 合并 → Run done；失败可重试 / 人工关闭  

另需：极简本地看板或等价 CLI 可见性；至少 2 种 Agent 启动方式可配置（证明多 Agent 适配不是口号）。

### 3.2 V0 明确不做

- 替代 GitLab CI  
- 自建 MR diff / 行内评论  
- 多用户、多机器、权限体系  
- GitHub / Jira 等非 GitLab 源  
- 需求管理、排期、绩效报表  
- 桌面客户端包装  
- 嵌入式自研 coding agent 框架（不绑定某一 harness；Agent 是外部进程）

### 3.3 Backlog（有数据再立项）

| 候选 | 立项信号 |
|------|----------|
| 任务依赖链自动触发 | 反复出现「等人做完 A 再开 B」 |
| Agent 自主拆子任务 | 单 Task 因过大而高频超时失败 |
| 更强隔离（容器等） | worktree 外写文件成为真实事故 |
| 指标周报与策略校准 | 已稳定跑 ≥ 数周并需要调 review/超时默认值 |

---

## 4. 状态模型

### 4.1 Run 状态（5 个）

```
queued → running → waiting_human ⇄（approve 后回到 running / queued）
              ↘ done
              ↘ failed
failed → queued（仅人工 retry）
```

| 状态 | 含义 |
|------|------|
| `queued` | 等待分派或等待并发槽位 |
| `running` | Agent 进程已启动 |
| `waiting_human` | 卡在 HITL，附 `reason` |
| `done` | MR 已合并且门禁通过 |
| `failed` | 重试耗尽 / 硬护栏 / 人工关闭 |

约束：

- **重试不是独立状态**（`retry_count` 字段）。  
- **Agent 自报 phase 不进状态机**（只进事件时间线）。  
- 所有合法转移必须经唯一校验入口（实现约束，防止旁路改状态）。

### 4.2 HITL `reason`（V0）

| reason | 何时 |
|--------|------|
| `design_approval` | 分派认为高风险，开工前需人确认 |
| `guardrail_violation` | 软护栏命中，等人豁免 |
| `code_review` | 按项目策略需要人审 MR |
| `agent_blocked` | Agent 上报阻塞 / 需求不清 |
| `merge_conflict` | 合并冲突需人处理 |
| `failure_review` | 失败或 CI 异常后的人工裁决 |

### 4.3 什么不必打扰人

Issue 摄入与分派、worktree 生命周期、Agent 启停监督、Goal 自检汇总、CI 等待、硬护栏直接失败、（若项目开启）低风险自动合并。

---

## 5. 核心概念

### 5.1 Run / Task Spec

调度单元与 GitLab Issue 通常 1:1（也允许无 Issue 的手工任务）。  
启动 Agent 时一次性下发：

```
Task Spec = Description + Goals + Guardrails + Context
```

### 5.2 Brain（分派）

对每个 `queued` Run：**一次** LLM 调用，输出结构化结果（示意）：

```json
{
  "kind": "feature | bug | chore | docs | refactor",
  "agent": "claude-code",
  "hitl_before_start": false,
  "goals": ["…"],
  "risk_notes": "…",
  "rationale": "…"
}
```

代码硬护栏（LLM 不可越过）：

- `max_concurrent`（按 Agent）  
- 非 worktree 隔离时的项目互斥  
- 未知 Agent 名拒绝启动  

LLM 失败：降级为 `waiting_human`（人工分派/裁决），系统不静默丢任务。

### 5.3 Gate（门禁）

MR 就绪后按序（逻辑顺序，实现可等价）：

1. `protected_paths`（hard → failed；soft → HITL）  
2. CI（pending 超时 → HITL；failed → HITL 或可配置的有限自动修复轮次）  
3. `review_policy`：`always` / `risky-only` / `never`  
4. `auto_merge`（仅当策略与门禁均允许时）

### 5.4 Context 约定

| 层 | 位置 |
|----|------|
| 全局 | `~/.sift/context.md` |
| 项目 | `{repo}/.sift/context.md` |
| 任务 | 创建时的附注 |

原则：真相在仓库里；全文注入；人写 Agent 读。

### 5.5 双层上报

| 层 | 来源 | 用途 |
|----|------|------|
| Layer 1 | Agent → MCP（进度 / blocker / goal / 声明完成） | 时间线与 HITL 触发 |
| Layer 2 | 进程退出码 + Gate | **最终裁定** |

---

## 6. 端到端流程（逻辑）

```
GitLab Issue（触发标签）
  → Intake（webhook，轮询兜底）
  → Run[queued]
  → Brain 分派
  →（可选）HITL design_approval
  → worktree + 启动 Agent → Run[running]
  → MR opened
  → Gate
  →（可选）HITL / 自动合并
  → MR merged → Run[done] → 清理 worktree
```

失败：非 0 退出 / 超时 → 有限次退避重试 → 耗尽则 `failed` + 保留 worktree 供排查 + 通知。

---

## 7. 功能模块（需求视角）

| 模块 | 职责 |
|------|------|
| **Intake** | GitLab Webhook（Issue / MR / Pipeline）+ 兜底轮询；幂等去重 |
| **Brain** | 一次 LLM 分派 + 硬护栏 |
| **Runtime** | worktree、Agent 启动（tmux / subprocess 等）、监督、超时、重试 |
| **Gate** | 护栏、CI、review 策略、自动合并判定 |
| **Attention** | HITL 队列、推送通知、审批入口（Web / CLI / 通知链接） |
| **MCP** | Agent 上报工具集（进度、goal、blocker、完成声明等） |
| **Control** | 本地 API + 极简看板 + CLI（状态、审批、重派、关闭） |

实现阶段补充：配置 schema、API 草案、数据表、技术选型——**不在本需求文档展开**。

---

## 8. 非功能需求（V0）

| 项 | 要求 |
|----|------|
| 部署 | 单机单实例守护进程 |
| 持久化 | 进程重启不丢 Run 状态；重启后核对 running 进程 / worktree |
| 安全 | GitLab Token 仅守护进程持有；审批动作需防误触（如 token/本地绑定）；配置与 DB 权限收紧 |
| 暴露面 | 默认仅本机；若为接收 webhook 绑定非回环地址，必须有密钥校验 |
| 延迟 | webhook 到达 → Agent 启动目标少于约 1 分钟（含一次分派调用） |
| 可观测 | append-only 事件流；可按 Run 查看时间线 |

---

## 9. 成功标准（PoC）

| 标准 | 定义 |
|------|------|
| 闭环 | 真实 GitLab 项目上，至少 1 条 Issue 经 Sift 产出并合并 MR（允许人工审批，不允许手工替 Agent 改完代码冒充） |
| 门禁 | 硬护栏违规任务不能进入 done |
| 注意力 | 至少一类 HITL 能推送并完成一键审批 |
| 多 Agent | 至少两种 Agent 配置各完成 ≥ 1 次真实或等价假 Agent 监督路径 |
| 恢复 | 杀死 `siftd` 后重启，不出现「幽灵 running」且可继续排队任务 |

定量指标（Human 分钟数/MR、HITL 率、自主闭环率、分派准确率）在 PoC 跑通后开始采集，不阻塞 V0 功能。

---

## 10. 开放问题

| # | 问题 | 决策时机 |
|---|------|----------|
| 1 | Brain 默认走本机 agent CLI 还是 API | 实现 Brain 时 |
| 2 | `review_policy` / `auto_merge` 默认值 | 第一次接真实项目前 |
| 3 | Agent 无事件超时阈值 | 有两周运行数据后校准 |
| 4 | 通知通道（IM webhook 具体厂商） | 接 Attention 时按现有基础设施 |
| 5 | 技术栈（倾向 Bun + TypeScript 单进程，可推翻） | DESIGN 文档 |

---

## 11. 文档地图（后续）

| 文档 | 状态 |
|------|------|
| `docs/PRD.md` | **本稿** |
| `docs/DESIGN.md` | 待写（架构、数据、配置、模块） |
| `docs/WBS.md` | 待写（里程碑与验收） |

---

_文档版本：V0.1 | 2026-07-27_
