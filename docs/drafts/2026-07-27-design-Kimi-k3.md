---
status: superseded
created: 2026-07-27
summary: 架构设计骨架稿：章节划分与逐节决策点
---

> 已融合进 [DESIGN.md](../DESIGN.md)；取舍记录见其 §14.4。本文只作过程留档。

# Sift — 架构设计

> 本文档定义系统结构与其**设计理由**。字段级契约（接口、表结构、配置 schema、消息格式）一律下沉到 `specs/`，此处只链接不复制（见 [docs/README.md](README.md) 的边界仲裁规则）。
> PRD 层已拍板的架构约束见 [PRD §13.1](PRD.md)，本文只展开不重议。

**当前为骨架稿**：每节列出要回答的问题与关键决策点，逐节评审后填充为正式设计。

---

## 1. 定位与约束映射

- 本文回答什么、不回答什么；与 PRD / specs / ADR 的分工。
- PRD §13.1 五条已拍板约束在本文的落点索引（每条指向展开它的章节）。
- §3.4 并行取证模型的架构化：**影子门禁是 day-1 常驻记录器**——Gate 的任何实现形态都必须从第一行代码就写出影子记录，不得作为后期追加的旁路。
- 决策点：无（本节是纯索引与纪律声明）。

## 2. 技术栈与运行时

- **已定方向：Bun + TypeScript，单进程**（PRD §12 #15 倾向确认；落地为 ADR）。
- 理由：单文件可执行分发；内置 SQLite；schema 密集型负载（配置 / Task Spec / LLM 输出 / Interrupt 全需校验）TS 生态最顺手；MCP 官方 SDK 以 TS 为一等公民；迭代速度契合 A4。
- **推翻条件（spike 验收项）**，任一失败则退 Node + TS（代码平移，不换语言）：
  1. `bun:sqlite` 在长生命周期进程 + WAL 下稳定运行；
  2. 子进程 / 伪终端对 agent CLI 的启动、监督、输出捕获能力满足 Runtime 需求；
  3. 单文件构建产物可由 launchd 常驻托管。
- 关键依赖选型原则：zod（一切外部输入的 schema 校验）、手写 SQL + 版本化迁移文件（不引 ORM）、不引框架。
- 决策点：spike 的具体验收脚本与通过标准；依赖白名单。

## 3. 进程模型与拓扑

- `siftd`：单进程 daemon，承载全部模块；**唯一写者**（所有状态写入经它，无第二个写路径）。
- `sift` CLI：薄客户端，经 Unix domain socket（`~/.sift/`）与 daemon 通信——Unix socket 不是网络端口，不违反 §9.3「暴露面为零」。
- `sift mcp-serve`：stdio MCP 垫片进程（agent CLI 只认 stdio MCP），把 Agent 上报经同一 socket 转发给 daemon；**限流与去重只在 daemon 内一处执行**（§5.8）。
- Agent 进程：Runtime 以子进程形态启动与监督，每 Run 一个，跑完即死。
- 部署：launchd 常驻（macOS）；崩溃自启 + 启动期恢复核对（§9.3 持久化要求）。
- 决策点：socket 协议形态（行分隔 JSON vs 其他）；只读类命令（`sift ps`）是否允许直读 SQLite 还是一律走 socket；MCP 垫片的鉴权（如何确认上报者就是 daemon 启动的那个 Run）。

## 4. 信任边界与 TM6 姿态（DESIGN 第一议题，PRD §12 #13）

- 本节必须先给出结论再谈其他模块：它反向决定 Runtime 的 Agent 启动契约。
- 要回答的问题：
  1. V0 对 TM6 的最终姿态是「只缓解 + 如实声明」还是落地「最小凭证沙箱」的部分机制？
  2. 若走最小凭证沙箱：共享 `.git` 如何处置（worktree 定义即共享主仓库 git 目录）；候选 agent 的凭证形态逐个核验（文件可挂载 / keychain 绑定不可挂载）；
  3. 三条 V0 缓解措施的机制化：敏感配置启动期读入 + 指纹校验（§9.1）、hooks 指纹覆盖 `core.hooksPath` 取值与最终指向、策略一律从 base 分支读取（TM2）——各自在哪个模块、什么时机执行。
- 决策点：V0 姿态结论（进 ADR）；Runtime 为沙箱预留的接口形状（即使 V0 不实现沙箱，Agent 启动参数也不应假设「与 daemon 同环境」）。

## 5. 模块结构与依赖方向

- PRD §8 的十个模块到代码包的映射；依赖方向规则（存储与 Forge 在最底层；Brain 可调用 LLM 触点但**不持有状态转移权**；Gate/Gate 产物只能经状态机入口落地）。
- 贯穿约束的落点：A1（LLM 只产出 recommendation，一切转移/通断/合并/解析由确定性代码执行）在代码层面如何强制——状态转移唯一校验入口、severity 确定性映射、指令确定性解析器各自的位置。
- 决策点：包划分粒度（按 §8 十模块一一对应还是合并）；模块间通信是直接函数调用还是内部事件总线（倾向前者，A4）。

## 6. 状态机与事件流

- 5 状态 Run 状态机（PRD §4.1）的唯一校验入口设计：所有转移经单一函数，转移表显式声明，非法转移直接拒绝——堵「旁路改状态」。
- append-only 事件流：一切事实（轮询观测、Agent 上报、门禁判定、人指令、配额消耗）先入事件流，状态是事件的投影；事件流同时是时间线 UI、回放集、指标打点（§10.2）的共同数据源。
- 幂等：摄入幂等键 `(forge, project, issue_id)`；指令幂等 `run_id + nonce`（§7.1）；事件游标持久化与重启续拉。
- 决策点：事件流与状态表是「事件溯源派生」还是「双写 + 对账」（倾向前者简化版：事件为主，状态表为物化缓存）；事件 schema 的版本策略。

## 7. 存储

- SQLite（WAL），单文件 `~/.sift/sift.db`；进程重启不丢 Run 状态与游标（§9.3）。
- 表骨架（字段级契约下沉 `specs/storage.md`，此处只列集合与职责）：
  - `runs`（状态、retry_count、关联 Issue/Change、worktree 路径）
  - `events`（append-only 事件流）
  - `intake_cursors`（每项目游标）
  - `interrupts`（含 severity / min_modality / expires_at / on_expire / nonce / 升级次数）
  - `shadow_gate_records`（预判 vs 人决定 + Run 特征，§5.6）
  - `ledger_records`（校准账本，§5.9：含语义原料 `/sift reject` `/sift ask` 的自然语言）
- 决策点：迁移机制（版本化 SQL 文件）；事件流保留策略；worktree 路径与 DB 记录的对账时机（启动期恢复核对）。

## 8. Intake 与 Forge 适配层

- Forge 封装结构：CLI `api` 子命令的执行器（超时 / 重试分类「网络可重试 vs 语义不可重试」/ 速率预算）、最小动词集的平台对称实现、平台差异归一（PRD §5.2 差异清单逐条的处理位置）。
- 启动期能力探测：只探测被已配置项目引用的 forge；失败拒启不降级。
- 自适应轮询循环：间隔表（60/15/10s）的实现形态；API 预算接近上限时的降级路径。
- actor 回溯：标签事件经 events / timeline 取 actor，取不到即忽略（§9.2 fail closed）的管道位置。
- 决策点：两个平台的适配是同构双实现还是「一个核心 + 差异表」；GraphQL 批量查询的使用边界。动词契约下沉 `specs/forge.md`。

## 9. Brain：LLM 触点的实现形态

- 统一调用壳：所有 T1–T7 经同一个「调用 agent CLI → schema 校验输出 → 失败走兜底」的壳，兜底路径逐触点落实 PRD §5.3 表。
- 结构化输出：经 agent CLI 的 JSON 输出能力 + zod 校验；不合 schema 视为触点失败，不重试超过一次。
- token 预算：计量点（调用壳内）、超限降级为纯确定性模式的切换机制；与注意力配额的优先级规则（§5.3：注意力是硬约束，兜底不得突破配额）。
- 决策点：调用 agent CLI 的具体形态（`claude -p` 的参数集、超时、并发）；提示词的存放与版本化（仓库内文件，随 git 追踪）。触点输出 schema 下沉 `specs/brain.md`。

## 10. Runtime：worktree 与 Agent 监督

- worktree 生命周期：创建（基于 base 分支）、Agent 提交检测、保留（failed 时）与清理（done 时）。
- Agent 启动契约：Task Spec 下发方式（§5.1）、环境变量的最小集（**不含 forge 凭证**；TM6 结论会细化本节）、MCP 垫片的注册。
- 监督：无事件超时（§12 #6）、退出码采集、退避重试与耗尽转 failed。
- 硬护栏执行点：退出后对 worktree diff 的 `protected_paths` 检查（hard → failed；soft → HITL）。
- 决策点：子进程 vs tmux 的选择（倾向子进程 + 输出落盘，tmux 仅作为人工接管的可选附着层）；「分支有提交 → Sift 创建 Change」的检测机制。

## 11. Gate、影子门禁与回放集

- Gate 流水线（护栏 → Checks[T5] → T3 → review_policy → auto_merge）的阶段化实现；「逻辑顺序，实现可等价」允许的优化边界。
- 策略读取：一律从 base 分支读（TM2）；schema 校验失败拒接入（§5.7）。
- 影子门禁：**day-1 常驻**——Gate 每次判定先写预判记录再进人审；记录结构与 §5.6 双向不对称阈值所需的负样本统计字段。
- 回放集导出：与 Gate 同期落地（§10.3 需求级约束）；导出格式与离线重跑的入口。
- 决策点：Checks 等待的实现（轮询间隔与 pending 超时）；T3 风险评分的缓存粒度。记录格式下沉 `specs/gate.md`。

## 12. Attention：Interrupt、配额与 Channel

- Interrupt 生命周期：生成（T4 简报 → 结构校验：options ≤4、headline 可朗读）→ 调度（T6）→ 推送 → 等待 → 超时/升级（§4.2，含 max_escalations 封顶与终态）。
- severity 确定性映射的唯一实现位置（§5.5：`(reason, gate 阶段, 护栏等级, 已升级次数) → severity`，LLM 只可建议降级）；critical 熔断。
- 注意力配额：记账口径（一次 Interrupt 计一次，升级重推不重复计）、超额合批降级、`sift ps` 余量展示。
- Channel 抽象：V0 单实现（IM webhook 或系统通知，§12 #7）；语音只是未来 Channel，不进架构主线。
- 决策点：合批摘要的形态与发送时机；T6 在 token 预算耗尽时的确定性兜底表。Interrupt 结构下沉 `specs/interrupt.md`。

## 13. Command：forge 指令的确定性处理

- 指令管道：轮询发现评论/标签事件 → actor 鉴权（§9.2，fail closed）→ 严格语法确定性解析（不用 LLM）→ nonce 匹配当前待决 Interrupt → 执行 → 回执评论。
- 与状态机的接口：指令执行只是「向状态机提交一次转移请求」，不直接改状态。
- 决策点：指令集与语法下沉 `specs/command.md`；allowlist 配置形式（§12 #9）。

## 14. 配置体系与可观测

- 配置分层：`~/.sift/`（全局缺省 + 敏感配置）与 `{repo}/.sift/`（项目策略，版本控制）；合并规则「全局只给缺省，不覆盖仓库显式声明」。
- 敏感配置：启动期读入 + 指纹校验 + 不热加载（改配置需重启，这是特性不是 bug，§9.1 TM6）。
- `sift doctor`：能力探测、策略 schema 校验、漂移对比的输出形态。
- 可观测：事件流即日志；指标打点（§10.2 八项指标各自的采集点，含北极星的加权打扰口径）。
- 决策点：配置格式（TOML vs YAML）；漂移判定口径（§12 #5）。配置 schema 下沉 `specs/config.md`。

## 15. 恢复与一致性

- 启动期恢复：核对 DB 中 `running` 的 Run 与实际进程 / worktree，幽灵 running 的处置（§10.1 恢复标准）；游标不回退不丢事件。
- 崩溃窗口分析：事件流先于状态落盘，保证最坏情况只丢「尚未裁定」的进行中动作，重启后重新裁定。
- 决策点：恢复流程的确定性顺序表。

## 16. 开放问题跟踪

- PRD §12 各开放问题到本文章节的映射表（哪节落地时结案哪条）；本文新增开放问题也登记于此。

---

## 骨架评审要点（评审通过后逐节填充）

1. 章节划分是否漏了 PRD 的硬性要求？（对照 §3.1 成功标准、§9.3 非功能需求、§10 指标逐项核对）
2. 哪些节应该直接拆分独立文件？（docs/README.md 约定单文件 ~300 行上限；候选：§4 TM6、§8 Forge、§12 Attention）
3. specs/ 的首批文件清单：`forge.md` / `storage.md` / `brain.md` / `interrupt.md` / `command.md` / `config.md` / `gate.md`——是否过多或过少？
4. 骨架里没有出现、但实现时一定会撞上的问题？（如：多项目轮询的调度公平性、LLM 提示词里的注入防护措辞、worktree 磁盘占用治理）
