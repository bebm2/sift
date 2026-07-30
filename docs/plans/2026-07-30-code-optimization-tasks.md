---
status: active
created: 2026-07-30
summary: 代码精简整改任务清单 — 原因、必要性、证据支撑
---

# 代码精简整改任务清单

> **基线**：`b6d89c0`（2026-07-30）
> **当前规模**：Go 生产代码 ~24,235 行，测试 ~20,355 行，Markdown 文档 ~27,211 行
> **目标**：生产代码从 ~24K 行压到 ~11K 行（减 ~55%），同时不丢失已验证的功能和门禁
>
> **使用方式**：每个任务对应一个独立的可勾选项。建议按 P0→P1→P2→P3 顺序推进。

---

## P0 — 流程优化（今天可做，防止问题继续产生）

### T001 改 review prompt：增量模式

| 维度 | 内容 |
|------|------|
| **做什么** | 修改 project skill 中的 review 提示词，要求 AI reviewer 只输出「与上次 review 相比新增的 finding」。上次已通过的 finding 不再重复。每条 finding 固定格式：`[P0/P1/P2/DEFER] 描述` |
| **为什么** | 当前每轮 review 生成完整独立文件，9 轮 rereview = 9 份结构几乎相同的文档。M5 command spec 审了 9 轮，产生 9 份独立 .md，每份都在重新分析同样的问题 |
| **证据** | review 文档 185 份，~15,820 行，占全部 .md 的 58%，是 Go 生产代码的 65%。M5 command spec rereview 序列（9 轮）和 M5 channel webhook worker rereview 序列（13 轮）是典型案例 |
| **必要性** | 不改变 review 流程，§10 分析的「复审驱动膨胀」会持续发生。当前每轮 review 的产出量 ≈ 初始 review，没有边际递减 |
| **预估效果** | review 文档量减少 60-70%，从 ~16K 行降到 ~5K 行 |
| **风险** | 低。只改提示词，不改工具。如果 reviewer 不遵守，可以在后续 review 中纠正 |

### T002 改 review prompt：scope gate

| 维度 | 内容 |
|------|------|
| **做什么** | review 输出中每条 finding 强制标注优先级标签 `[P0/P1/P2/DEFER]`，并在 review 末尾提供「scope summary」表格列出各优先级数量 |
| **为什么** | 当前 reviewer 从不建议 defer，author 从不拒绝。结果：每条 review finding 都被实现，即使它属于 V1 范围。T6（打扰调度）、T7（校准提案）、shadow gate 完整实现、replay 导出——全是 PRD §3.3 明确 backlog 的功能，但代码已实现 |
| **证据** | `internal/brain/t4t6t7.go`（536 行）包含 T6/T7 实现逻辑，`internal/replay/`（260 行）实现回放集，`internal/forgebudget/`（192 行）实现 API 预算跟踪。这些功能的 PRD 状态均为 backlog 或「等有数据再立项」 |
| **必要性** | 没有 scope gate，PRD 的「明确不做」和「Backlog」章节就形同虚设。AI 的「把事情做完整」倾向会持续把 V1 功能拉到 V0 实现 |
| **预估效果** | 防止未来继续产生 ~2,000 行/周的 backlog 代码 |
| **风险** | 低。需要人工复核 reviewer 的优先级判断，但只需要在合并前花 2 分钟看 scope summary 表格 |

### T003 首次提交前的新增/删除比检查

| 维度 | 内容 |
|------|------|
| **做什么** | 在 `pre-commit hook`（或相当于 git commit 前的手动检查步骤）中加入：`git diff --stat --cached | awk -F',' '{print $2}'` 计算新增/删除行比。如果 >5:1，打印警告并要求确认 |
| **为什么** | AI 代码生成只有新增，从不主动删除或重构。结果是代码量单向增长，没有清理阶段 |
| **证据** | 项目总 commit 635，从数据看新增行数远大于删除行数。storage 包 16K 行——如果是手写，开发过程中至少会有一次重构合并，但 AI 生成的代码从来没有「把 41 个文件合并成 20 个」的步骤 |
| **必要性** | 防止代码单向膨胀。这不是要限制新增，而是强制每一次提交都考虑「哪些旧的可以删」。没有这个检查，storage 包只会从 71 个文件继续增长到 100+ |
| **预估效果** | 每次提交的新增/删除比从 ~10:1 降到 ~3:1 |
| **风险** | 低。只是一个警告，不是硬拒绝。可以考虑只在 PR 合并前检查，不在每次 commit 时阻拦 |

---

## P1 — 删除已实现的 Backlog 功能（这周可做）

### T004 删除 brain 中的 T6/T7 实现

| 维度 | 内容 |
|------|------|
| **做什么** | 从 `internal/brain/t4t6t7.go` 中移除 T6（打扰调度）和 T7（校准提案）的执行路径。保留类型定义和接口签名（因为被引用），函数体改为 `return nil, ErrNotImplemented` |
| **为什么** | PRD §5.3 明确 T6 和 T7 的立项信号分别是「首次 ≥3 Run 并发」和「已稳定跑 ≥ 数周」。V0 尚未达到任一条件 |
| **证据** | 文件 `internal/brain/t4t6t7.go` 536 行，加上 `internal/brain/t6_emit_interrupt_acceptance_test.go`、`internal/brain/t7_a7_firewall_test.go`，合计 ~1,000 行。T6 的打扰调度需要在「注意力配额被真实用满」的场景下才有意义——目前单用户单机还未产生足够数据驱动调度决策 |
| **必要性** | T6/T7 的逻辑依赖真实运行数据来校准。没有数据就实现的调度算法是「猜」——既浪费代码又可能在真实场景下做出错误调度。V0 的兜底路径（按 severity 确定性阈值打断）已经完全够用 |
| **预估效果** | 删 ~1,000 行（含测试） |
| **风险** | 低。调用方已通过兜底路径（确定性阈值）处理。回归测试应验证兜底路径跑通 |

### T005 删除 replay 包

| 维度 | 内容 |
|------|------|
| **做什么** | 删除 `internal/replay/` 整个包。从 `internal/gate/reconciler_test.go` 中移除对 replay 的引用 |
| **为什么** | 回放集（replay set）的功能前提是影子门禁已经累积了足够的历史决策数据。项目目前还没有真实 Change 流过 Gate |
| **证据** | `internal/replay/` 共 260 行，只有 `internal/gate/reconciler_test.go` 引用它。影子门禁在 V0 阶段只是被动记录，不会产生需要「回放」的数据量 |
| **必要性** | 影子门禁的基本记录功能（在 Ledger 中）已经满足 V0 需求。独立的回放集导出是「等有数据再立项」的功能（PRD §5.6） |
| **预估效果** | 删 ~260 行 + 一个包 |
| **风险** | 低。唯一引用在测试文件中，可以改用手动构造的测试数据替代 |

### T006 删除 forgebudget 或合并到 intake

| 维度 | 内容 |
|------|------|
| **做什么** | 将 `internal/forgebudget/` 的 API 预算跟踪逻辑简化，合并到 `internal/intake/` 中。或者直接删——V0 中轮询间隔是配置常量，不需要独立的预算跟踪模块 |
| **为什么** | Forge API 预算在 V0 中就是「配置中的每小时上限」+「一个计数器」。为此建一个独立包（192 行 + 测试）属于过度抽象 |
| **证据** | `internal/forgebudget/charger.go` 2458 行，`internal/forgebudget/charger_test.go` 4480 行。引用方只有 `internal/daemon/daemon.go`。PRD §5.2「API 预算」描述为：「配置每小时调用上限；接近上限自动降级」——一句话需求，四行代码能解决 |
| **必要性** | 独立包带来的抽象成本（接口、测试 suite、文件组织）超过了功能本身的复杂度。V0 不需要可插拔的预算策略 |
| **预估效果** | 删 ~192 行 + 一个包，或压缩到 ~50 行内联 |
| **风险** | 低。功能本身不复杂，内联后不影响正确性 |

---

## P2 — 机械合并（需专门安排一天重构）

### T007 storage 包：71 文件合并到 ~20 文件

| 维度 | 内容 |
|------|------|
| **做什么** | 将 `internal/storage/` 中的 71 个 Go 文件按领域合并为约 20 个文件，不改变任何外部 API。具体合并方案见下表 |
| **为什么** | 单机 SQLite 持久层不需要 entity-per-file。每个文件约 30-80 行 SQL CRUD，被分开的唯一理由是「AI 生成时每个概念独立一个文件」——这是工具的组织习惯，不是好的工程实践 |
| **证据** | 71 个文件 / 16,364 行 = **平均每文件 230 行**。但实际上大多数 CRUD 文件只有 ~80 行（如 `channel_batch.go`、`command_probe.go`、`config_activation.go`、`doctor.go`、`security.go`、`assignment.go`、`proposal.go`）。对比：`transition.go` 是整个 storage 最核心的约束逻辑，也只有 200 行——它被分散在大量薄 CRUD 文件中，反而降低了可读性 |
| **必要性** | 71 个文件不是「结构清晰」，是**导航噪音**。找一个表的查询要在 6-8 个文件里翻。合并到 20 个文件后，一个领域的读写逻辑集中在一个文件里，维护成本反而降低 |
| **预估效果** | 从 16K 行 / 71 文件 → ~8K 行 / 20 文件 |
| **风险** | 中。合并时可能出现 git merge 冲突（如果其他人同时改了 storage）。建议选低峰期做，先写一个合并脚本，逐文件确认内容后批量执行 |

**建议的合并分组**：

| 新文件 | 包含旧文件 | 预估行数 |
|--------|-----------|---------|
| `runs.go` | interrupt.go, advance_interrupt.go, termination.go, launch.go, transition.go, assignment.go | ~1,500 |
| `events.go` | appendevent.go, events.go, outbox.go | ~400 |
| `forge_io.go` | intake.go, intake_m2.go, intake_reply.go, change.go, ready_change.go, reverse_sync.go, forgebudget.go, report.go, report_quota.go, gate.go, gate_candidate.go | ~1,500 |
| `brain_io.go` | brain.go, ledger.go, proposal.go | ~700 |
| `channel_io.go` | channel.go, channel_batch.go | ~350 |
| `system.go` | boot.go, migrate.go, storage.go, security.go, doctor.go, testseed.go | ~600 |
| `scheduler.go` | scheduler.go | ~200 |
| `command.go` | command.go, command_probe.go | ~300 |
| `handoff.go` | handoff.go, attempt_race.go | ~400 |
| `recovery.go` | recovery.go | ~300 |
| `config_activation.go` | config_activation.go | ~100 |
| `reverse_sync.go` | reverse_sync.go | ~100 |
| 测试文件 | 合并到对应的 *_test.go（如 `forge_io_test.go`） | ~7,600（不减少） |

### T008 config 包：16 个 production 文件合并到 ~5 个

| 维度 | 内容 |
|------|------|
| **做什么** | 将 `internal/config/` 的生产文件从 16 个合并到约 5 个，按功能分组：类型定义、加载、校验、规范化、激活 |
| **为什么** | 配置解析在 Go 中通常是 1-2 个文件的事。16 个文件暗示每一层处理（raw → partial → normalized → validated → activated）都被拆成了独立文件，但 V0 配置只有 ~20 个有效字段 |
| **证据** | `internal/config/` 共 16 个 production 文件，3,855 行。最大的文件 `normalize.go` 955 行，但其中大量是从各种来源（环境变量、默认值、文件覆盖）合并配置的胶水代码。Go 标准库 `encoding/json` + 一个校验函数就可以处理 90% 的场景 |
| **必要性** | 16 个文件维持了一个远超 V0 需求的配置生命周期（raw → partial → normalized → validated → activated）。如果配置只有 ~20 个字段，这个生命周期的每一层都很薄，合在一起反而更清晰 |
| **预估效果** | 从 3.8K 行 / 16 文件 → **~2K 行 / 5 文件** |
| **风险** | 中。config 是全局引用最多的包之一，合并后需确认所有导入路径都更新 |

### T009 decode + contract 合并为 internal/schema

| 维度 | 内容 |
|------|------|
| **做什么** | 将 `internal/decode/`（942 行）和 `internal/contract/`（367 行 + 子包 410 行）合并为一个 `internal/schema/` 包，保留 JSON schema 校验和 envelope 解码功能 |
| **为什么** | 两个包在做同一件事——JSON schema 校验和类型安全解码。被分开的原因是「decode 负责运行时解码，contract 负责编译期类型定义」——这个区分在 Go 中是不必要的，一个包里有类型定义 + 校验函数即可 |
| **证据** | `internal/decode/decode.go` 引用 `internal/contract` 中的类型。`internal/contract/schemagen/schemagen.go` 从 Go struct 生成 JSON Schema——这个「从类型定义到运行时校验」的闭环完全可以在一个包内完成，不需要三个子包（contract + schemagen + genschema） |
| **必要性** | 三个子包的额外文件组织成本（import 路径、init 顺序、测试套件）超过了它们各自的功能复杂度。Go 的惯例是一个功能一个包 |
| **预估效果** | 从 ~1,700 行 / 3 包 → **~800 行 / 1 包** |
| **风险** | 中。需要更新所有 import 路径（brain、config、command、policy 等 6 个包） |

---

## P3 — 基础设施清理（有余力再做）

### T010 删除 schema 生成器

| 维度 | 内容 |
|------|------|
| **做什么** | 删除 `internal/contract/genschema/main.go`、`internal/contract/schemagen/`、`internal/brain/genschemas/main.go`。将 JSON Schema 文件改为手写维护 |
| **为什么** | schema 生成器运行一次后产出的 .schema.json 文件就固定了。运行时不需要生成器本身。把它留在仓库里相当于保留了「编译器的编译器」——它只在你修改 Go struct 定义时才需要重新运行 |
| **证据** | 当前有 3 个生成器入口（contract/genschema、contract/schemagen、brain/genschemas），每个都是 main.go 或单独的 CLI。产出的 .schema.json 文件（closed_example.schema.json、raw_config.schema.json、brain 各 T 的 schema）一旦生成，在 Go struct 定义不变的情况下不会变化 |
| **必要性** | 不是非要删——如果 struct 定义频繁变化，生成器有用。但 V0 阶段 struct 定义在一周内已趋于稳定，删除生成器可以减少 build 依赖和代码量 |
| **预估效果** | 删 ~460 行 |
| **风险** | 中。下次修改 struct 时需要手动更新对应的 .schema.json。建议保留生成器脚本但不合入主分支，仅在需要时运行 |

### T011 删除或合并单实现接口

| 维度 | 内容 |
|------|------|
| **做什么** | 检查 `internal/` 中所有只有单一实现的接口（如 `internal/attempt`、`internal/launchworker`、`internal/channelworker` 的接口），改为直接使用结构体 |
| **为什么** | 接口的收益在于多实现替换。V0 中只有一个实现的情况下，接口增加了间接调用成本和学习成本，没有收益 |
| **证据** | `internal/attempt`：1 个接口，2 个文件。`internal/launchworker`：1 个接口，1 个文件。`internal/channelworker`：1 个接口，1 个文件。三个接口都没有第二个实现 |
| **必要性** | 这些接口在 V0 中是「以防万一以后需要」的防御性抽象。但 Go 的接口是隐式满足的——你可以在需要时再提取接口，不需要提前声明 |
| **预估效果** | 删 ~200 行 + 简化测试 mock |
| **风险** | 低。纯机械替换，测试覆盖保证行为不变 |

---

## 执行建议

### 顺序选择

| 策略 | 适用场景 | 理由 |
|------|---------|------|
| **P0 → P1 → P2 → P3**（按优先级） | 正常节奏 | 先改流程防止复发，再删无争议的 backlog，再做机械合并 |
| **P2 先行**（重整 storage 再删其他的） | 想「一次性做完」 | Storage 合并占 60% 以上的代码缩减量，先啃硬骨头 |
| **P1 优先**（只删不重构） | 时间紧 | P1 操作都是删除，代码量减少明显但风险最低 |

### 人工复核要求

1. P1 任务（删除 backlog）：合并前 `git diff` 确认只删除了目标代码，没有误删被其他包引用的路径
2. P2 任务（合并）：先在分支上跑完整测试套件，确认 green 后再合入 main
3. P0 任务：不需要大改，今天就可以改 review prompt

### 成功标准

| 指标 | 当前 | 目标 |
|------|------|------|
| Go 生产代码 | 24,235 行 | ~11,000 行 |
| storage 文件数 | 71 个 | ~20 个 |
| 包总数 | ~24 个 | ~15 个 |
| review 文档增量 | 全文 | 增量 |
| 新增/删除比 | ~10:1 | ≤3:1 |
