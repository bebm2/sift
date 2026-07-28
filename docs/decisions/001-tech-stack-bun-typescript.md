---
status: superseded
created: 2026-07-27
summary: （已被 ADR-009 取代）技术栈曾定为 Bun + TypeScript 单进程，含逃生舱与退出条件
---

# ADR-001 技术栈：Bun + TypeScript

> **已被 [ADR-009](009-tech-stack-go.md) 取代（2026-07-27，同日）。** 触发因素是需求变化而非本文论证有误：PRD V0.4 把对外分发与多平台列为非功能需求，而本文拒绝 Go 的前提正是「PRD 不做对外分发」。另有两处本文自身的缺陷记录在 ADR-009：第 28 行「PRD 明确不做常驻服务」与 PRD §9.3 矛盾；「切 Node LTS」这条退出条件在需要单文件分发时无法执行。**本文保留为决策轨迹，不再是当前结论。**

关闭 [PRD §12 #15](../PRD.md)。结构展开见 [DESIGN §5](../DESIGN.md)。

## 决策

`siftd`、`sift` CLI、wrapper 全部使用 **TypeScript(strict) + Bun**，单进程，持久化用 SQLite（WAL）单库；校验统一 zod；不引 ORM、不引 Web 框架。

## 背景

工作负载画像里**没有任何一处 CPU 密集**：拉起子进程、解析异构 JSON、写 SQLite、按时钟轮询，并发规模个位数 Run（PRD §9.3 单机单用户）。因此性能与并发模型不构成选型依据。真正有区分度的只有三点：

1. **边界校验成本**。PRD 强制三处 schema 校验：策略文件启动期校验（§5.7）、LLM 触点输出不合 schema 即兜底（§5.3）、forge 双平台 payload 归一（§5.2）。TS + zod 让三者共用一个 idiom——一份 schema 同时产出运行时校验、静态类型、以及喂给 LLM 的 JSON Schema。这是差距最大的一项。
2. **对外动作只有三类**：`gh/glab api`、`claude -p`、启动 agent 进程。`Bun.spawn` 与内置 `bun:sqlite`（同步 API，单写者守护进程正合适）消掉大部分胶水。
3. **分发**。`bun build --compile` 出单文件，目标机无需装 runtime；配 launchd 即可常驻。

## 放弃的选项

| 选项 | 放弃理由 |
|------|---------|
| **Node.js LTS** | 唯一有实质分量的竞争者。进程、信号、流的成熟度确实高于 Bun，这是 codex-sol 提案的核心主张，见下「对该主张的处理」 |
| Go | 静态二进制与 `os/exec` 成熟度值得那点冗长，但只在「多机 / 常驻服务 / 对外分发」前提下成立，而 PRD 明确不做这三件事；契约密集型代码的迭代速度慢一档 |
| Rust | 直接违反 A4（最快闭环优先于架构完备） |
| Python | 长跑守护进程与严格状态机弱于 TS；其唯一优势 pydantic 被 zod 抹平 |

## 对「Node 更稳」这一主张的处理

不反驳，而是把它结构性地移出关键路径：**进程监督的权威不放在 runtime 的子进程 API 上**。存活、退出码、完成证据一律取自 wrapper 落盘的 `control.json` / `heartbeat` / `result.json`（见 [ADR-005](005-execution-backend-and-wrapper-contract.md)），恢复判定靠文件与 `kill(pid, 0)`。即使 Bun 在子进程 reaping 或信号处理上有 bug，PRD §10.1 的「无幽灵 running」仍然成立。

换句话说：**这个选型之所以敢承担 Bun 的风险，是因为另一条决策已经把风险的落点搬走了。** 没有 wrapper 契约，本 ADR 应当选 Node。

## 约束（与决策同等效力）

1. **逃生舱**：Bun 专有 API 只允许出现在三个薄适配模块（`sqlite` / `spawn` / `fs`）。领域层不含任何 runtime 专有调用。切 Node 是改三个文件，不是重写。
2. 所有 SQL 只出现在存储模块（同时是逃生舱的隔离点）。
3. **禁止把定时器当调度器**：唯一的时间驱动来源是 [ADR-002](002-reconciler-and-single-transition-entry.md) 的 tick，不得散落 `setInterval`。

## 退出条件（触发任一即追加 ADR 切 Node LTS，代码平移）

- `bun:sqlite` 在长生命周期进程 + WAL 下出现数据一致性问题；
- wrapper 契约无法弥补的子进程 / 信号缺陷；
- 单文件产物无法由 launchd 稳定托管。

## 后果

- 正面：三处 schema 校验共用一套工具；分发简单；AI 代理写 TS 的语料最厚（本项目由代理实现，这一项不是玩笑）。
- 负面：承担 Bun 的长跑不确定性，以三条约束与退出条件对冲。
- 中性：Node 退路的成本被限制在三个文件，代价是不能使用任何 Bun 独有的便利 API。
