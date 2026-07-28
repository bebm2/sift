---
status: active
created: 2026-07-27
summary: 版本间变更与修订沉淀
---

# Sift — 演进记录

版本间变更摘要与 PRD/DESIGN 正文中剥离的修订历史。只追加。

## DESIGN D0.10 标记通过（2026-07-28，版本不变）

[review-18](reviews/2026-07-28-design-review-kimi-k3-06.md) 独立核销 review-16 全部九项发现，结论为**通过，可以进入 WBS**。`docs/DESIGN.md` 状态由 `draft` 改为 `active`，版本保持 D0.10。

唯一新增 P3 不阻断 WBS：ADR-010 决策 6 仍用旧名 `attempt_decision`。后续不改其历史论证，只增加指向 ADR-013 的术语修订说明；最迟在 `specs/storage.md` 或 `specs/control-plane.md` 首次落笔前完成，并由 WBS 第 1 片的文档前置检查验收。现行 spec / 代码一律使用 `attempt_resolution`。处置账见 [DESIGN §14.14](DESIGN.md)。

## PRD V0.8 + DESIGN D0.10（2026-07-28）

根据 [design-review-16](reviews/2026-07-28-design-review-gpt-5.6-sol-01.md) 修订，处置对账见 [DESIGN §14.13](DESIGN.md)。三项阻断均以协议级约束关闭：merge 由 forge 对 Gate 裁定的 expected head 做远端 CAS，旧 operation 不得合并新 head（[ADR-011](decisions/011-merge-requires-expected-head-cas.md)）；`startup_stall` 只有显式 `reject` 才终局，升级封顶只落 `hold`；retry 探测成功以一笔 CAS 事务完成旧 attempt、隔离、Interrupt、Run `waiting_human→queued`、新 attempt/claim、启动与回执 outbox（[ADR-013](decisions/013-startup-stall-retry-convergence.md)）。

其余接缝同步：Change 只在成功完成证据与最终 head 冻结后创建；Forge 增 Change operation 全状态对账端口；进程组消失证明收窄为受支持 Agent 不主动脱组的契约，按 agent/version 做资格测试并由 `doctor` 报告（[ADR-012](decisions/012-process-group-supervision-boundary.md)）；V0 的 escalation 定义为单 Channel 强提醒档位；`sift speak` 移出 V0，避免引入第三处平台分叉；“不落盘凭证”收窄为 forge 凭证。

文档治理修正：review-15 的存档原文有 N1（升级上限终局措辞）与 N2（进程组假设）两项 P3，并未提出 `retry` 两段式。D0.9 的 retry 修订来自自查，原归因属事实性错误；reviews 保持只读，PRD / DESIGN / 本 CHANGELOG 更正来源。

## DESIGN D0.9（2026-07-28，PRD 不变）

D0.9 自查发现 `startup_stall` Interrupt 在 `retry` 再探测失败时的生命周期歧义：若 `retry` 与 `reject` 一样立即关闭 Interrupt，后续升级无处落。该版把 `retry` 改为非终局探测请求：探测失败复用同一 Interrupt、轮换 nonce 并升级，不新增 Interrupt 状态。

> **事实性更正（D0.10）**：本项曾被错误归因于 [review-15](reviews/2026-07-28-design-review-kimi-k3-05.md) 的“唯一新发现”。review-15 原文实际提出的是升级上限终局措辞与进程组不脱离假设两项 P3；D0.9 未关闭它们，也未定义 retry 探测成功的原子结果段。完整处置见 [DESIGN §14.12–14.13](DESIGN.md)。

## PRD V0.7 + DESIGN D0.8（2026-07-28）

[review-14](reviews/2026-07-28-design-review-codex-03.md) 核销 review-13 的两项遗留与 D0.7 自查项，但在重放「受控终止失败 → 转人工」之后的交错时找到一个 P1；逐项对账见 [DESIGN §14.11](DESIGN.md)。**根因是 D0.6 的人工出口只定义了怎么进人工态，没定义进去之后事实迟到怎么办**——与 §14.10 同一形态，第三次。

**人工态不是静止状态，而是仍在接收事实的状态。** 冻结纪律故意不作废旧 session / permit（作废不安全，OS `spawn` 不消费 fencing token），所以旧执行体醒来提交合法 `claim:started` 是必须建模的正常输入。仲裁点定为单一 `attempt_decision` marker（CAS、不可逆）：人的决定未提交前**事实优先**——同一事务内推进 attempt、Run `waiting_human → running`、把停滞 Interrupt 标 `superseded_by_fact` 关闭、接管监督；决定已提交则**由决定吸收事实**——不推进 Run，但把迟到的 Agent 身份登记为可终止身份并回 wrapper `superseded_by_decision`，继续执行该决定的终止。后者是收益所在：迟到的 started 恰好提供确切身份，把「证明不了」变成「可执行的终止」，所以绝不能简单拒绝。`claim:started`、恢复补 started、迟到 `result.json`、Interrupt 指令四个入口共享同一套 CAS 前置与幂等结果。

**新增 HITL reason `startup_stall`（PRD §4.3）**，不再借用 `failure_review`：后者默认 `auto_reject` 会把 Run 推进终态，而执行体可能仍在跑，终态会把处置责任一起丢掉。该 reason 禁用 `auto_reject`，达升级上限落 `hold`。动作集收窄为 `retry`（人已在系统外处置后重新探测——这是唯一能改变「证明不了」的输入）/ `reject`（放弃并**保持隔离**：Run 转 `failed`，但 worktree 不回收、不被任何新 attempt 复用）/ `hold`；`approve` 不在 options 内。PRD §7.1 相应增加「指令必须属于当前 Interrupt 的 `options[]`」这一道校验，nonce 匹配不再是唯一门槛。

**Interrupt 生成需要稳定键**：单一发射器只保证入口唯一，挡不住超时扫描 / 恢复扫描 / `kill` / `retry` 四个发现者并发各建一条。`startup_stall` 的生成键为 `(run_id, attempt_no, generation, cause)` 并加唯一约束；一次打扰的五件事（Run 转移 / Interrupt / 配额 / 事件 / 发布 operation）同事务提交。

**Q7 拆为 Q7a / Q7b**：D0.6 把它改成「无法经任何本地 RPC 改变 Run」，这个更强的说法与同一份文档已承认的 operator token 暴露面冲突，V10 一个编号同时要求「不可能」与「预期成功」。现在 Q7a 是端点与凭据性质（V0 必须成立），Q7b 是 agent 进程整体能力（V0 明确不成立，攻击复现必须成功且 `doctor` 必须报告），V10 拆 V10a / V10b。V2 增加人工态事务的逐点崩溃，V4 增加四组人工态交错与四发现者并发。

## DESIGN D0.7（2026-07-27，PRD 不变）

[review-13](reviews/2026-07-27-design-review-kimi-k3-04.md) 判 D0.6 **通过、阻断解除、可进 WBS**（review-09 两个 P1 与 review-10 全部条件核销归档）。本版处置其两项遗留与一笔同族自查，对账见 [DESIGN §14.10](DESIGN.md)。

**裁决执行拓扑**：后端只决定 wrapper 跑在哪里，永不插到 wrapper 与 Agent 之间；Agent 恒为 wrapper 的直接子进程且恒在其进程组内，`process` 与 `tmux` 同拓扑。起因是 D0.6 对此有两处互相矛盾的表述（§8.4「任何后端都只启动 wrapper」vs 凭据段「wrapper 与 agent 之间可能夹一层后端」），而 ADR-010 的整条 `spawning` 证据链只挂在「wrapper 存活或其进程组存在」这一个原语上——按后一种读法，tmux 后端会让双写窗口原样复活，wrapper 契约的「按进程组回收」也不成立。**未采纳评审建议的后端中性执行句柄**：它要求恢复矩阵维护两套观测语义，其中一套正是 ADR-005 拒绝依赖的会话表。代价如实记：真 PTY 改由 wrapper 自建并中继到 pane 与 `agent.log`，`tmux` 的保留理由从「某些 agent 需要真 PTY」收窄为 attach 与持久宿主。

**终止结局按触发路径分开**：恢复 → 按重试策略；`sift retry` → 新开 attempt；`sift kill` → 不新开、Run 经唯一 `transition()` 转 `failed`。原文「才换代或新开 attempt」会被读成 kill 也续命。

**受控终止流程的适用面推广到全部阶段**：D0.6 只把它用在 `spawning`，而 `running` 的「后端会话在、wrapper 不在」仍在执行者可能存活时直接判 `orphaned`——未确认的 `orphaned` 加一次人工 `retry` 就是同一 worktree 上的第二个 agent，与 §6.4 的不变式冲突。两笔 P2 是同一形态：新纪律只写在了被反复评审的那个阶段与那个后端上。V4 相应要求两个后端跑同一套断言。

## PRD V0.6 + DESIGN D0.6（2026-07-27）

不来自评审，来自对 D0.5 自身的复核：**D0.5 把 attempt 启动的安全侧写全了，漏了可见性侧。** `running` 改为只认 Agent 启动证据后，卡在 `spawning` 的 attempt 不再产生任何 Run 状态变化，而「运维终止流程」「转人工」在 D0.5 全文只被引用、从未定义——最坏情况是 Run 静默停在 `queued`，不推送、不计配额，正是 [DESIGN §6.4](DESIGN.md) 为 Channel 推送失败写过的同一条错误（不可见的停滞等于把 Run 挂死，A3）。逐项对账见 [DESIGN §14.9](DESIGN.md)。

D0.6 定义**受控终止流程**为恢复、`sift kill` / `retry`、超时三条路径共用的单一实现（身份确认 → 有界升级信号 → 复核消失）：确认消失按恢复矩阵推进；身份不可确认或进程组仍在，则经单一发射器发一次 `failure_review` Interrupt、Run 转 `waiting_human`、attempt 冻结。停滞因此计入注意力配额，不另开运维告警旁路。ADR-010 补记「以可用性换无双写只在挂住可见时才划得算」。

配套：PRD §4.1 状态图补 `queued → waiting_human`（开工前审批与启动停滞都需要它），§4.4 为「Agent 启停监督不打扰人」加例外条款，不新增 HITL reason；DESIGN §14.2 把新协议引入的五个时限纳入「配置项 + 确定性默认值」纪律；§2.2 的 Q7 改为「agent 出示不了启动凭据，因此无法经任何本地 RPC 改变 Run」，与 §8.9 已收窄的三层表述一致；§8.10 明示 `kill` / `retry` 在 `spawning` 期间降级为「已受理」；V4 增加「进程组拒绝消失」与「身份不可确认」两例，断言不得静默停在 `queued`。

## PRD V0.5 + DESIGN D0.5（2026-07-27）

根据 [design review-10](reviews/2026-07-27-design-review-kimi-k3-03.md)、[review-11](reviews/2026-07-27-design-review-codex-02.md)、[review-12](reviews/2026-07-27-design-review-glm-5.2-02.md) 合并修订；逐项对账见 [DESIGN §14.8](DESIGN.md)。D0.4 的阻断结论成立；D0.5 已闭合 attempt 启动的线性化与真实 `running` 语义，保持 `draft`，待复评确认后再进入 WBS。技术栈不重议。

启动协议由“两次同凭据 `claim:confirm`”改为 [ADR-010](decisions/010-attempt-spawn-handoff.md) 的 session 绑定一次性 spawn handoff：启动 operation 先经 CAS + lease 认领；`claim:acquire` 绑定唯一 wrapper instance 并签 session；`claim:permit-spawn` 持久化唯一 permit、进入不可换 owner 的 `spawning`；真实 Agent 身份落盘后 `claim:started` 才推进 attempt / Run 为 `running`。恢复、人工 retry/kill 在换代前必须证明旧 wrapper 与进程组已消失；证据不全宁可 `orphaned` / 转人工，也不双起或补虚假 `running`。Agent 刚 spawn 而 started 尚未提交的报告返回有界可重试 `not_ready`。

其余接缝同步收口：Brain trace 改用独立调用身份，只有实际参与 Gate 时才关联快照；单一 decode gateway 显式区分封闭契约 `closed` 与 Forge 开放信封 `open-envelope`；发布契约裁决为**三个同版本自包含二进制组成一个归档**并按版本目录原子升级，四种 OS/架构组合均跑安装与二进制冒烟、每 OS 跑完整恢复；无 systemd Linux 支持 foreground 但不承诺自动常驻。对外分发的产品动机只留 PRD，ADR-009 只回答技术选型。

文档治理同步修正：PRD / DESIGN 升为 V0.5 / D0.5，使阻断项进入默认上下文；README 将约 1200 行明确为基于因果上下文完整性的提醒线而非硬上限。相关 ADR-003/005/006/008/009 与验证矩阵 V2/V4/V10/V14/V15 已同步。

## PRD V0.4 + DESIGN D0.4（2026-07-27）

**技术栈由 Bun + TypeScript 改为 Go**，[ADR-009](decisions/009-tech-stack-go.md) 取代 [ADR-001](decisions/001-tech-stack-bun-typescript.md)（后者标 `superseded`，保留为决策轨迹）。

需求侧先动：PRD §9.3 新增**兼容性**（macOS / Linux × arm64 / amd64）与**分发**（自包含单文件、Homebrew tap 与 Release 归档、前向迁移）两行非功能需求，§2.1 澄清「单用户单机」约束的是一个安装内的形态而非安装数量——因此 C4「不做分布式」不受分发影响。

推翻 ADR-001 的理由是**前提失效**，不是原论证有误：它拒绝 Go 的依据是「Go 的价值只在多机 / 常驻服务 / 对外分发前提下成立，而 PRD 不做这三件事」，其中「对外分发」这半句刚被否掉（另有「不做常驻服务」一句本就与 PRD §9.3 部署行矛盾）。另有两条独立理由：ADR-001 承担 Bun 长跑风险的退路是「切 Node LTS」，而 Node 做不出等价的自包含单文件产物，**风险对冲与分发需求互斥**；以及此刻尚无代码，现在换是三处文档、有实现后换是全部实现且没有渐进路径（TS → Go 无逃生舱，不同于 Bun → Node 的三个适配模块）。

Go 的代价如实记录：**丢掉 zod「一份 schema 三处使用」的收益**，且 `encoding/json` 的默认行为（缺失给零值、未知字段忽略）与 fail closed 相反。补偿是三条与决策同等效力的约束——结构体为唯一定义并生成 JSON Schema、全部外部输入经单一 decode gateway、每个边界类型一对 golden test（新增 V14 锁定）。Rust 仍被否决：本项目难点是事务、恢复、fencing、外部 CLI 与权限边界，不是内存安全或性能。

DESIGN 侧影响面见 [§14.7](DESIGN.md)：§5 全节重写、§7 路径统一 `~/.sift/` + `SIFT_HOME`（否决 Linux 侧 XDG 三分）、§11 两平台托管单元与升级纪律、§12 新增 V14/V15、§13 新增第 8 片发布链（交叉编译从第 1 片起进 CI）。**除 ADR-001/005/007 外其余 ADR 无变化**——这本身证明了文档的语言无关性是真的。

## DESIGN D0.3（2026-07-27）

四份 D0.2 复评（glm-5.2/hex/kimi-k3/hex 各一份）的处置，逐条落点见 [DESIGN §14.6](DESIGN.md)。**结论上采纳 review-05 的「暂不进 WBS」而非另三份的「通过」**：另三份核销的是修订文字是否出现，review-05 继续验证了新机制在既有边界下能否运行，并因此发现三个阻断项。三处是机制层面的改动，不是措辞：

- **Gate 缓存键（R5-F1，含 R4-M1）**：D0.2 的枚举式键漏掉了一半输入——同一 `head_sha` 下 Checks / review / mergeability / riskScore 都会变，CI 重新失败后可复用此前的放行 verdict（漏放通道）。改为整份冻结快照的摘要 `gate_input_hash`，完备性由构造而非人工维护保证；缓存、影子记录与回放集共用同一快照 ID。
- **attempt 启动协议（R5-F2）**：D0.2 要求 wrapper 自取 DB claim，与单写者模型、`run.sock` 动词集、token 生成顺序三条互锁，**无可实现路径**；且无 fencing，「释放 claim 后新开 attempt」这条恢复动作自身会双起 agent。改为 claim 由 daemon 在 attempt 事务内建立并附 owner nonce + fencing 代次，凭据经 0600 bootstrap 文件下发、读后 unlink，wrapper 在 spawn 前再校验一次代次。
- **创建 Change 的证据（R5-F3）**：「同 base/head 存在开启 Change」既漏已关闭/已合并者，又会接管他人对象。改为 `op_key` marker + 全状态搜索 + 按远端 ID 收敛，marker 不符即 `SemanticConflict` 不接管。

另有三项一致性修订（项目级 vs 进程级启动失败分级、ADR-006 残留的单 socket 后果行、PoC 验收拆自动化门禁与人工验收两组）与十余项接缝完善（沙箱挂载集写全 run dir、拓扑图拆两端点三步频、`context.md` 从 base 读、认证投影同事务、回放集含 Brain trace、配置重载不对称的理由、新增 V12 零配置启动与 V13 critical 熔断）。

文档治理修正一处：§14.5 原写「三轮评审全部采纳」，实际有 review-01 的 5 项 P2/P3 未进表，措辞限定为「本表所列项」，5 项在 §14.6 逐条处置（其中默认值表一项为部分采纳，理由记在 §14.2）。

## DESIGN D0.2（2026-07-27）

三轮 D0.1 评审（`reviews/2026-07-27-design-review-hex-01/02/03.md`）的处置。四个阻断项、两个重要项、三个新发现全部采纳，逐条落点见 [DESIGN §14.5](DESIGN.md)。实质变化五处：

- **控制面授权（F1）**：拆运维 socket 与 `run.sock` 两类能力；「Layer 1 永不越权」收窄为端点性质；agent 越权如实进 TM6 清单。新增 [ADR-008](decisions/008-control-plane-endpoints-and-capabilities.md)。
- **投递语义（F2）**：outbox 只给至少一次，逐类副作用各自声明幂等协议；Channel 推送如实降级；agent 启动改用 attempt claim 互斥。
- **认证投影（F3）**：`auto_merge` 证据门槛获得合法的聚合级数据流与版本快照，认证版本进 Gate 缓存键。
- **attempt 生命周期（F4）**：与 Run 状态机分开建模，恢复扫描覆盖启动阶段的崩溃窗口。
- **调度公平性（F5、N2）**：三组独立步频调度器，outbox 由提交唤醒并有最大推进延迟目标。

另修正三处措辞失真：影子门禁的落地时点（N1）、单 Channel 下不存在的「备用通道」（N3）、run token 的环境变量暴露（review-01 §3.2）。

## PRD V0.3（2026-07-27）

由 DESIGN 阶段的评审发现驱动的需求层修订。三处：

- **§4.1 `done` 语义裁决（评审 F6）**：原文「Change 已合并**且**门禁通过」与 §4.5「手工合并 → `done`」、§10.1「硬护栏违规不得进入 done」三者在「人手工合并了一个门禁未过的 Change」时无法同时成立。裁决取**事实优先**：`done` 只表示 Change 已合并，门禁结论降为 `gate_bypassed` 审计属性；不新增终态（§1.5 禁止叠第二套状态机）。连带修订 §4.5、§10.1 门禁验收行、§10.2（误放行率分母限定 + 新增门禁绕过率）。
- **§5.8 / §8 Layer 1 通道**：MCP 改为 `sift report` CLI（Run 作用域 token），模块 **MCP** 改名 **Report**。取舍见 [decisions/006](decisions/006-report-via-cli-not-mcp.md)。
- **§5.2 「零凭证管理」加星号**：只指 forge 侧，不等于 Agent 取不到凭证。同时 §12 #13（TM6 收口）与 #15（技术栈）标记结案，指向 DESIGN 与 [ADR-001](decisions/001-tech-stack-bun-typescript.md) / [ADR-007](decisions/007-tm6-minimal-credential-sandbox-direction.md)。

## V0.2（2026-07-27）

- 文档目录规范确立（`docs/README.md`）。
- 正文中「对 V0.x 的批评 / 补正」callout 承载设计理由，V1.0 时将纯历史对比部分沉淀至本文档（PRD §13.2）。
