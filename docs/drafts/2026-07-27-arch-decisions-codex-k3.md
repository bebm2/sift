---
status: superseded
created: 2026-07-27
summary: 架构讨论产出：5 条 ADR 草案 + PRD 修订清单 + DESIGN 骨架
---

> 已融合进 [DESIGN.md](../DESIGN.md)；取舍记录见其 §14.4，PRD 修订请求见其 §14.3。本文只作过程留档。

# Sift 架构决策草案（2026-07-27 架构讨论）

> 来源：codex-k3 与项目所有者的架构讨论。范围覆盖 PRD §13.1 留给 DESIGN 的全部自由度（技术栈 #15、存储形态、进程模型、Layer 1 上报通道）及被提前的 #13（TM6 收口）。
> 转正路径：评审通过后，§1–§5 拆分为 `decisions/001–005`，§6 落实为 PRD 修订，§7 落入 `DESIGN.md`。

## ADR-001 技术栈：Bun + TypeScript

**决策**：`siftd`、`sift` CLI、全部模块使用 Bun + TypeScript。

**理由**：代码构成是"编排胶水 + 契约校验"（schema 校验、子进程编排、状态机、SQLite、LLM 输出结构化），非计算密集。zod 生态做 schema 校验最顺手；`bun:sqlite` 内置零依赖；`bun build --compile` 出单文件可执行；AI 代理写 TS 的语料最厚。

**约束**：不依赖 Bun 专有 API（`bun:sqlite` 除外，隔离在存储模块内），保持 Node 兼容作为退路。退出条件：长期运行中撞上 Bun 守护进程敏感区的运行时 bug（子进程 reaping、信号处理）时，降级 Node 运行同一份代码。

**放弃**：Go（单静态二进制最稳，但契约密集代码迭代慢一档）；Node LTS（更无聊，但收益不足以抵消 Bun 的内置 sqlite 与分发便利）；Rust/Python（迭代成本 / 分发与类型强度）。

关闭 PRD §12 #15。

## ADR-002 存储：SQLite（WAL）+ 物化投影 + append-only 事件表

**决策**：单文件 `~/.sift/sift.db`，WAL 模式。`events` 表 append-only（事件即事实）；`runs` / `interrupts` / `cursors` 等物化当前状态投影，避免每次重放；`gate_decisions` / `ledger` 承载影子门禁与校准账本；schema 迁移用 `schema_version` 表 + 顺序迁移脚本。

**理由**：账本与影子门禁的查询模式是关系型的（按 kind / 风险分 / 负样本聚合），文件存储等于自研烂查询引擎；`bun:sqlite` 同步 API 配单进程守护无并发写问题；事务保证状态机"唯一校验入口"的原子性；回放集导出 = `SELECT → JSONL`。

**约束**：所有 SQL 只出现在存储模块，业务代码不直接碰 SQL（也是 ADR-001 的 Node 退路隔离点）。

**放弃**：JSONL 纯事件溯源（多文件无原子性、聚合痛）；嵌入式 KV（过度工程）。

## ADR-003 进程模型：单进程 tick 循环 + Unix socket IPC

**决策**：三个 bin 入口共享一套 TS 代码——

- `siftd`：单进程守护。中心 tick 循环按优先级派发（intake 轮询 → 反向同步 → Checks 跟进 → HITL 超时扫描 → 重试退避）；配合 SQLite 同步 API，进程内无数据竞争。所有 Run 状态转移走唯一函数 `transition(runId, to, cause)`：校验合法转移（§4.1）+ 写事件 + 更新投影，一个事务内完成。LLM 触点（T1–T7）是 tick 派发的异步任务，结果同样经 `transition` 落库，无旁路。
- `sift` CLI 与守护进程经 **Unix domain socket**（`~/.sift/siftd.sock`，0600）通信。文件系统 socket 不是网络端口，不违反 §9.3 零暴露面。

**放弃**：CLI 直读 SQLite + 信号文件（动作确认与 `logs -f` 长输出别扭）；每模块一进程（无收益，增加生命周期管理成本）。

## ADR-004 Layer 1 上报通道：Run 作用域 CLI，取代 MCP

**决策**：Agent 不通过 MCP 上报，改为调用 `sift report` CLI。Sift 启动 Agent 时注入 `SIFT_RUN_ID` + `SIFT_RUN_TOKEN`（每 Run 随机 nonce）；CLI 经 unix socket 到守护进程；守护进程校验 token 属于该 Run，跨 Run 调用拒绝。限流、去重、Interrupt 子配额（§5.8 TM5）逻辑不变。

**理由**：目标用户是 coding agent，跑 shell 命令是本职（与 `gh`/`git` 同），`--help` 自带文档；与 §5.2"CLI 即已鉴权传输层"、§3.2"不绑定 harness"是同一集成哲学——Sift 对外集成面统一为 CLI，无第二种协议；少一个 shim 进程、一个 SDK、一条 JSON-RPC 转发链。

**放弃**：MCP（schema 自动注入对 CLI 原生 agent 是冗余；各 harness 的 MCP 配置形态不一，恰是一种 harness 绑定）。安全模型不变：可信边界一直在守护进程侧，不在传输层。MCP 可作为未来某 harness 受益时的第二种前端重新引入。

**注意**：本决策修订 PRD §5.8 与 §8 模块表（MCP → Report），见 §6。

## ADR-005 TM6 收口：定方向、留接缝、不实施

**决策**：方向采纳**最小凭证沙箱**（只挂 agent CLI 凭证、不挂 forge 凭证，§9.1），否决完全沙箱（与"复用本机订阅"价值主张正面冲突）。V0 不实施沙箱，只做 PRD 已定缓解（敏感配置启动期读入 + 指纹校验、hooks 指纹覆盖 `core.hooksPath`、`~/.sift/` 权限收紧）。

**接缝**：Runtime 启动 Agent 走单一 launcher 间接层（V0 为恒等函数），将来包 `sandbox-exec` / `bwrap` 只改一处。

**共享 `.git` 处置**（沙箱内完整 clone + 两侧同步）明确推迟——它动 A5 的 worktree 设计，只在沙箱立项时需要答案。

**配套 spike（排入 WBS）**：实证检查各目标 agent 的凭证存储形态（文件可挂载 vs 绑 keychain / 设备指纹）。若全部 keychain-only，最小凭证沙箱方向在起点不成立，需回到本分叉重议。这是"方向已定"的证伪条件。

**效果**：PRD §5.2"零凭证管理"措辞提前加星号（指 forge 侧）；§12 #13 状态改为"方向已定，实施在 Backlog"。

## 6. PRD 修订清单（转正时落实）

| 位置 | 修订 |
|------|------|
| §5.8 双层上报 | Layer 1 "Agent → MCP" 改为 "Agent → `sift report` CLI（Run 作用域 token）"；限流去重要求不变 |
| §8 模块表 | **MCP** 模块改为 **Report**（上报通道 + 确定性限流去重） |
| §12 #13 | 状态改为"方向已定（最小凭证沙箱，ADR-005），实施在 Backlog"；凭证形态 spike 为证伪条件 |
| §12 #15 | 关闭：Bun + TypeScript（ADR-001） |
| §5.2 集成方式 | "零凭证管理"加注：指 forge 侧；Agent 经已登录 CLI 继承鉴权的暴露面见 TM6，收口方向见 ADR-005 |

## 7. DESIGN.md 骨架（转正时展开）

```
1. 技术栈与运行时（ADR-001）
2. 进程模型（ADR-003）：siftd tick 循环、transition 唯一入口、unix socket 协议
3. 存储（ADR-002）：表结构、事件模型、迁移、回放集导出
4. 模块设计（对应 PRD §8，每模块：职责 / 接口 / 依赖）
   - Forge / Intake / Brain / Runtime（含 launcher 接缝）/ Gate / Ledger / Attention / Command / Report / CLI
5. 配置 schema（项目策略 + 全局缺省，§5.7 漂移防护的落地）
6. 安全落地（TM1–TM6 各自由哪个模块哪条机制承接）
7. 目录与代码结构（src/ 模块划分）
```

字段级契约不进 DESIGN，随模块实现下沉到 `specs/`（docs/README.md 的边界仲裁规则）。
