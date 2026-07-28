---
status: active
created: 2026-07-27
summary: 技术栈改为 Go；取代 ADR-001，触发因素是分发与多平台成为需求
---

# ADR-009 技术栈：Go（取代 ADR-001）

取代 [ADR-001](001-tech-stack-bun-typescript.md)（Bun + TypeScript）。关闭 [PRD §12 #15](../PRD.md) 的第二次裁决。**对外分发为什么是产品需求只以 PRD §2.1 / §9.3 为准**；本 ADR 只回答该需求为何导向 Go。结构展开见 [DESIGN §5](../DESIGN.md)、部署见 [DESIGN §11](../DESIGN.md)。

## 决策

`siftd`、`sift` CLI、`sift-agent-wrapper` 全部使用 **Go**，单模块三个二进制，持久化仍是 SQLite（WAL）单库。

| 层 | 选择 |
|----|------|
| 语言 | Go（当前稳定版），`CGO_ENABLED=0` |
| 持久化 | SQLite via **`modernc.org/sqlite`（纯 Go，无 CGO）**，WAL；手写 SQL + 版本化迁移；不引 ORM |
| 边界校验 | 结构体为唯一定义 → 反射生成 JSON Schema（喂 LLM 触点）+ 同一份 schema 做运行时校验；全部外部输入经单一 decode gateway，按边界显式选 `closed` / `open-envelope` |
| YAML | YAML → JSON 后进入同一个 decode gateway，并显式选择 `closed`；与 Forge 共用入口，不共用 unknown-field 策略 |
| 进程 | `os/exec` + `SysProcAttr{Setpgid}`，按进程组回收；`signal.NotifyContext` |
| 控制面 | Unix socket 上手写 JSON-lines RPC；**不引 gRPC**（零网络作用域下 protobuf 工具链是纯成本） |
| 测试 | 标准 `testing` + 属性测试库 + 子进程崩溃注入 |
| 构建分发 | 每平台交叉编译三个同版本自包含二进制；GoReleaser 出单归档、manifest、校验和与 Homebrew tap；版本目录 + `current` 链接原子升级 |
| 常驻托管 | launchd user agent（macOS）/ systemd user unit（Linux） |

## 为什么推翻 ADR-001

**触发因素是需求变化，不是 ADR-001 论证有误。** ADR-001 拒绝 Go 的理由是「Go 的价值只在多机 / 常驻服务 / 对外分发前提下成立，而 PRD 明确不做这三件事」。其中：

- 「不做常驻服务」这半句当时就是错的——PRD §9.3 部署行写的就是「单机单实例守护进程」，DESIGN §11 要 launchd 常驻。这是 ADR-001 转写 DESIGN §5.1 时多塞的一项（DESIGN 原文只写「多机 / 对外分发」）。
- 承重的那半句是**「对外分发」**。PRD V0.4 已把分发与多平台（macOS / Linux，arm64 / amd64）列为非功能需求，因此这个前提失效。

前提失效之后，还有三条独立理由：

**1. ADR-001 的风险对冲在分发前提下不闭合。** 它承担 Bun 长跑不确定性的方式是留退出条件「触发即切 Node LTS，代码平移」。但 Node 不能稳定产出等价的自包含原生可执行套件，所以退出条件一旦触发，只能二选一：放弃无 runtime 分发，或放弃退路。**风险对冲与分发需求互斥**——这不是措辞问题，是 ADR-001 的风险管理在新前提下失去出口。

**2. 分发把 runtime 不确定性从「我的问题」变成「别人的问题」。** 自用时 Bun 长跑异常的代价是重启一次；分发之后，代价是别人在我没测过的 OS / 架构 / libc 组合上遇到守护进程异常，而我拿不到现场。这类风险的成本不随代码质量下降，只随安装量上升。

**3. 迁移代价严重不对称，而现在没有代码。** 现在换的成本是重写 DESIGN §5、§11 与本 ADR 三处文档；有了实现之后换的成本是全部实现，且**没有渐进路径**——ADR-001 的逃生舱只保到 Node（三个适配模块），TS → Go 没有对应物。更贵的部分不是敲代码（由代理写，两种语言差距不大），而是那些只有跑过才知道的东西：调过的 tick 间隔、真实 `glab` 输出的字段怪癖、崩溃注入里发现的时序、fencing 竞态的复现步骤。文档承接得了一部分，接不住全部。

反过来说，**这套文档的绝大部分不随语言改变**：PRD、DESIGN 除 §5 与 §11 之外、除 ADR-001 之外的全部 ADR、SQLite schema、wrapper 的落盘文件契约（本来就是文件协议）、socket 协议、operation key、Gate 输入快照与回放集格式、V3 录制的 `gh`/`glab` fixture（纯 JSON）。这不是巧合，是「领域层无 IO、契约下沉 specs」的直接结果。

## Go 与本项目风险面的匹配关系

DESIGN §2.2 的质量属性场景决定了真正的风险在哪：崩溃恢复（Q1）、逐类投递语义（Q5）、进程 fencing（§8.4）、外部 CLI 归一（Q3）、权限边界（Q6/Q7）。**没有一项是内存安全或 CPU 性能问题**，这也是 Rust 仍被否决的原因（见下）。Go 在其中三项上是直接收益：

- **进程组与信号是一等公民。** `SysProcAttr{Setpgid: true}` + 向 `-pgid` 发信号，正是 wrapper 契约第 6 条要的东西；`signal.NotifyContext` 让 tick 循环与 RPC 的取消沿 `context.Context` 一路贯通。
- **长生命周期守护进程的行为可预测**，且这条不需要用「把监督权威搬到落盘文件上」来对冲。**注意 [ADR-005](005-execution-backend-and-wrapper-contract.md) 不因此失效**：wrapper 落盘契约的真正理由是 `siftd` 重启后仍需知道上一次 agent 是死是活，那与 runtime 质量无关。Go 只是让它不必**同时**承担「对冲 runtime bug」这第二份职责。
- **自包含原生二进制 + 交叉编译是工具链默认行为**，前提是不引 CGO（见下一节的驱动选型）；三个入口仍须按同一 manifest 组成原子发布套件。

## 代价：zod 的三重收益会丢，必须靠结构补回

TS + zod「一份 schema 同时产出运行时校验、静态类型、喂 LLM 的 JSON Schema」是 ADR-001 最强的一条依据，Go 没有等价物。更糟的是 Go 的默认行为对 fail closed 是**敌对的**：`encoding/json` 遇到缺失字段给零值、遇到未知字段静默忽略——正好是 PRD §5.2「取不到就是 `unknown`，转 HITL」与 §5.3「schema 校验即兜底触发器」的反面。

因此以下三条与决策同等效力，不是建议：

1. **结构体是唯一定义，JSON Schema 由它反射生成**（`go generate` 产物入 git，便于 review 与 diff）。同一份 schema 既用于运行时校验，也用于 LLM 触点的结构化输出约束——zod 那份「一处定义、三处使用」的性质靠代码生成保留，代价是多一个构建步骤。
2. **全部外部输入经单一 decode gateway，但策略不强行相同**：配置、LLM 输出与 socket 请求是封闭契约，使用 `closed`（schema + `DisallowUnknownFields`）；Forge raw payload 是开放世界输入，使用 `open-envelope`，允许上游增加不消费的字段，但适配器所需字段仍做必填、类型、枚举与 actor 校验。两者都用指针字段区分「字段缺失」与「值为零」。领域层不存在第二个反序列化入口。
3. **每个边界类型都有一对 golden test**：closed 断言额外字段 / 必填缺失均拒绝；Forge 断言无关新增字段接受、必需字段缺失或变型拒绝。fail closed 不再以牺牲上游前向兼容为代价。

诚实的结论：这条轴上 Go 是**退步**，从「白拿三样」退成「一个必须守住的关口 + 一个构建步骤」。它可被机械化、可被测试锁定，所以是可接受的代价——但不该被描述成平手。

## 驱动选型：必须是纯 Go 的 SQLite

**`modernc.org/sqlite`，不用 `mattn/go-sqlite3`。** 后者走 CGO，会当场毁掉“无平台 C 工具链即可交叉编译三二进制套件”这条选 Go 的主要收益（需要各平台工具链或 zig 之类的替代方案）。Sift 的写入量是每秒个位数（DESIGN §5.1 的负载画像），纯 Go 驱动的性能差距在本项目不可观测。

配套约束：**写连接池上限设为 1**。单写者是设计前提（DESIGN §7），显式钉住它可以从根上排除驱动内部并发导致的 `SQLITE_BUSY`，而不是靠 `busy_timeout` 兜。

## 放弃的选项

| 选项 | 放弃理由 |
|------|---------|
| **Bun + TypeScript（ADR-001）** | 见「为什么推翻」三条。它在「V0 只服务作者本机」的前提下是正确决策，前提没了 |
| Node.js LTS | 无法稳定提供等价的自包含原生可执行套件，这正是 ADR-001 退出条件的死角 |
| **Rust** | 风险面不匹配：Sift 的难点是事务、崩溃恢复、fencing、外部 CLI 与权限边界，不是内存安全或性能。Rust 会引入生命周期、异步生态与跨平台构建的额外成本，却不解决上述任何一项。「AI 会写 Rust」不等于 Rust 的集成成本消失 |
| Python | 长跑守护进程与严格状态机弱于两个候选；分发依赖解释器 |
| CGO SQLite 驱动 | 见上节：与交叉编译目标直接冲突 |
| gRPC / protobuf 做控制面 | 零网络作用域、两个端点、动词个数很少；引入 IDL 与代码生成没有对价 |
| XDG 目录规范（Linux 侧分裂 config/state/runtime） | 会把沙箱挂载集与 TM6 暴露清单从「一个目录」拆成三处，两份文档的推理都要跟着分叉，而 V0 换不到任何用户可见收益。统一 `~/.sift/` + `SIFT_HOME` 覆盖（DESIGN §7） |

## 退出条件（触发即追加 ADR，但都不换语言）

与 ADR-001 的关键区别：**逃生舱现在都在 Go 内部，且各自被隔离在一个模块里。**

- `modernc.org/sqlite` 在长生命周期 + WAL 下出现正确性问题 → 切 CGO 驱动并接受交叉编译成本（影响面：存储模块 + 发布流水线）；
- 结构体反射生成的 JSON Schema 无法表达某个边界契约 → 改为手写 schema 文件作为唯一定义、结构体由其生成（影响面：decode gateway + `go generate`，校验语义不变）；
- 某平台的常驻托管无法稳定运行 → 缩减支持矩阵并在 PRD §9.3 如实降级，不改语言。

## 后果

- 正面：三个自包含原生二进制、交叉编译与单归档发布是默认路径；进程组 / 信号 / 取消传播是标准库能力；不再有悬在 runtime 上的退出条件；CLI / wrapper / daemon 共用模块与版本 manifest。
- 负面：**丢掉 zod 的三重收益**，以「结构体 → 生成 schema + 单一 decode gateway + golden test」三条补偿，多一个构建步骤。
- 负面：首个闭环大概率比 TS 略慢（错误处理与解码样板更多），与 C9 有轻微张力。判断是：这点差距远小于「有代码之后再换语言」的代价，而且由代理写代码把样板成本压得很低。
- 中性：AI 语料方面 TS 总量更厚，但在守护进程、进程组、信号、状态机这一块 Go 的正典示例质量更高，差距在本项目的实际代码构成里基本抵消。
- 中性：多平台使 [ADR-007](007-tm6-minimal-credential-sandbox-direction.md) 的方向**更可行**——Linux 侧有 user namespaces / bubblewrap 这类正经手段，而 macOS 的 `sandbox-exec` 已 deprecated；凭证形态 spike 的证伪条件因此按平台分开判定。
