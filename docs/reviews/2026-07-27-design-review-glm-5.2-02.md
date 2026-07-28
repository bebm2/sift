# DESIGN.md 架构评审（第 12 轮，D0.4 独立评审）

> 日期：2026-07-27
> 评审人：**glm-5.2（MiMo Code Agent）**
> 评审对象：[`docs/DESIGN.md`](../DESIGN.md) **D0.4**（1019 行，对应 PRD V0.4）
> 评审依据：[`docs/PRD.md`](../PRD.md) V0.4、ADR 001–009（001 已 superseded）、[`design-review-09`](2026-07-27-design-review-codex-01.md)（Codex/GPT-5，D0.3 阻断评审）、[`design-review-10`](2026-07-27-design-review-kimi-k3-03.md)（kimi-k3，D0.4 再评审）
> 评审范围：R9-F1 / R9-F2 两个 P1 的独立复核 + Go 切换后的全文一致性 + 新发现

## 1. 结论

**维持 review-09 / review-10 的阻断：在 R9-F1 / R9-F2 关闭前，不建议进入 WBS 或实现。**

本轮独立推演确认两个 P1 均为**协议级结构缺陷**，不是措辞问题，无法靠补字段或加注释降级。D0.4 对它们的处置是零——§8.4 启动协议五步表、§10.1 恢复矩阵、§6.4 outbox 表的 agent 启动行均与 D0.3 逐字相同，且无 §14.8 对账节。技术栈切换（Go）本身合格（review-10 已确认，本轮复核无异议），但它改写了 §5 / §7 / §11 / §14.7 却绕开了同一份文档里已被两轮评审标记为阻断的 §8.4。

**需要如实认账的一点**：我在 review-04（D0.2 评审）中给出的结论是「无阻断级问题，文档可进入 WBS」，而同期 review-05 正确识别了三个 P1（缓存键漏输入、claim 协议不可实现、Change 证据不唯一）。review-08 对此已有准确复盘——我和 review-06/07 核销的是「修订文字是否出现」，没有验证新机制在既有边界下能否运行。本轮我逐交错推演了启动协议的竞态路径，以独立验证而非引用的方式确认 R9-F1 / R9-F2 成立。

本轮新发现 2 项（N1 级 P2、N2 级 P3），均不阻断，但 N1 是 R9-F1 在另一张表里的投影，应随 R9-F1 一并修正。

## 2. R9-F1 / R9-F2 独立复核

### 2.1 R9-F1 · P1：两次同名 `claim:confirm` 无法区分语义——独立验证

**位置**：§8.4 步骤 3 与步骤 5（第 454、456 行）；§10.1 第 739 行；§6.4 第 297 行。

协议规定步骤 3 和步骤 5 都调用 `claim:confirm`，使用**同一 owner nonce + 同一 fencing 代次**。daemon 靠 attempt 当前状态推断这是第一次还是第二次：`pending` → 第一次（转 `starting`），`starting` → 第二次（转 `running`）。

**我独立推演的三条可达路径，每条都推翻 §6.4 表「启动 agent = effectively-once」的主张：**

**路径 A（RPC 重放误判为第二次 confirm）：**

```
1. wrapper A 第一次 confirm 成功（pending → starting），RPC 响应丢失
2. A 重试 confirm，此时 attempt 已是 starting
3. daemon 将重试当作第二次 confirm，若 control.json 已落盘（步骤 4 已执行），即转 running
4. A 尚未做 spawn 前代次校验，更未 spawn → Run 已 running 但 agent 未启动
```

这与 R9-F2 合流。根因：**同一凭据 + 同一动词名 + 靠状态推断语义 = RPC 重放在状态机的不同位置被误判。**

**路径 B（同代双 wrapper 的 confirm 交错）：**

```
1. outbox worker 在同一 daemon 会话内重复派发同一启动 operation（§6.4 只说 worker 至少一次执行，
   未禁止会话内并发认领；bootstrap 读后 unlink 不是互斥原语）
2. wrapper A 与 B 都持有同一 owner nonce + 同一代次
3. A 的第一次 confirm：pending → starting（CAS 成功）
4. B 的第一次 confirm：CAS 失败（expected pending, got starting），B 退出 ← 这一步 CAS 确实挡住了
5. 但若 A 与 B 的第一次 confirm 几乎同时到达，SQLite 单写者只保证一个 CAS 成功；
   成功者继续，失败者退出——这一条是安全的
6. 真正的问题：A 的第二次 confirm 与 B 被拒后的 RPC 重试在 daemon 侧不可区分
   （同 nonce、同代次、attempt 均为 starting）
```

路径 B 的步骤 5 由 CAS 保护，但步骤 6 回到了路径 A 的问题：**daemon 无法区分「合法 wrapper 的第二次 confirm」与「竞争 wrapper 被拒后的重试」或「合法 wrapper 第一次 confirm 的重试」。**

**路径 C（§10.1 第 739 行论证失效）：**

恢复矩阵写「多个 wrapper 竞争同一 attempt | 不可能发生：**claim 唯一约束**使后到者的 `claim:confirm` 失败而自退」。

这个论证不成立。`attempt_claim(run_id, attempt_no)` 唯一约束防止的是**插入第二条 claim 行**。但两个 wrapper 不是在竞争插入 claim——claim 由 daemon 在步骤 1 预建，两个 wrapper 都是在**出示同一个已有 claim 的 nonce**。唯一约束在这个攻击向量上完全无关。真正挡住并发 confirm 的是 attempt 状态 CAS（路径 B 步骤 5），不是 claim 唯一约束。矩阵把保护机制归错了来源，实现者据此推理会得出错误的安全结论。

**结论：R9-F1 确认成立。** 修复方向（与 review-09/10 一致）：两动词拆名（如 `claim:acquire` / `claim:start`），第一次握手签发会话级 ID，第二次必须出示；启动 operation 在 outbox worker 侧需有会话内原子认领语义。

### 2.2 R9-F2 · P1：Run 在 Agent 真正 spawn 前进入 `running`——独立验证

**位置**：§8.4 步骤 5（第 456 行）、第 479 行；PRD §4.1。

步骤 5 的顺序是：wrapper 调 `claim:confirm` → daemon 校验代次通过 → **attempt 转 `running`** → RPC 返回 → wrapper 调 `spawn`。

**PRD §4.1 定义 `running` = 「Agent 进程已启动」。** 但在步骤 5 的顺序里，`running` 落库时 Agent 尚未被 spawn。必然存在的窗口：

```
confirm 响应返回 → wrapper 尚未调用 spawn → wrapper 崩溃 / spawn 失败 → Agent 从未启动，但 Run = running
```

§8.4 第 479 行声称「不会有 `running` 但从未启动（转移发生在 spawn 前的最后一道校验之后）」。**这句话恰好是自我证伪的**：「校验之后」在时间上早于「spawn 之后」——两个事件之间存在一个不可回滚的进程边界。文本用「最后一道校验之后」来描述，实际承认了转移发生在 spawn **之前**。

恢复矩阵也无法证明 Agent 已启动：`control.json` 在步骤 4（spawn 前）已写入，其进程身份字段最多证明 wrapper 存在，不能证明 Agent 子进程存在。按 §10.1 第 730 行（`starting | claim 已确认、control.json 在、进程身份匹配 | 补齐 queued → running 转移并接管监督`），恢复时甚至会**主动补齐**这个虚假的 `running`。

**结论：R9-F2 确认成立。** 修复方向：Run 的 `queued → running` 转移必须以**可验证的 Agent 已启动证据**（wrapper 在 spawn 成功后写 `agent_started` 标记或调独立 `claim:started` RPC，含 Agent PID）为依据，不得以 spawn 前的校验为依据。`control.json` 必须区分 wrapper PID 与 agent PID。恢复矩阵需补「许可已发未 spawn」「spawn 已成功、started 未落库」两行。

### 2.3 R9-F1 与 R9-F2 是同一协议的两个独立失败面

它们应联合修订。R9-F1 破坏 effectively-once（可能双起或零启动），R9-F2 破坏状态真实性（`running` 不代表 Agent 在跑）。修 R9-F1 而不修 R9-F2，只是把「可能双起」变成「确定单起但可能从未起」；修 R9-F2 而不修 R9-F1，CAS 下的竞态仍然存在。

## 3. 其余 open 项复核

| 编号 | 级别 | 来源 | 状态 | 证据 |
|------|------|------|------|------|
| R9-F3 | P2 | review-09 | **未修** | §7 第 381 行仍写「两类共用同一个 `gate_input_snapshot_id` 关联」。T1/T2/T6/T7 可能在无 Gate 快照时运行，强制关联会悬空或伪造。建议改为 Brain trace 以 `(run_id, attempt_no, touchpoint, call_seq)` 为主键，仅 Gate 相关触点携带可空外键 |
| R9-F5 | P3 | review-09 | **未修** | README 约 300 → 1200 行阈值放宽仍无理由记录 |
| R10-N1 | P3 | review-10 | **未修** | PRD §9.3 与 ADR-009 互相把对方当「为什么分发」的出处，循环引用 |
| §14.8 | — | review-10 | **不存在** | DESIGN 追溯节止于 §14.7（技术栈重议），review-09 / review-10 的发现无正式对账节 |

## 4. 本轮新发现

### N1 · P2：§6.4 outbox 表 agent 启动行声明了协议未兑现的保证

**位置**：§6.4 第 297 行。

表中 agent 启动行写「**effectively-once（互斥 + fencing 保证）**」，并在括号内指向 §8.4。但 §8.4 的启动协议存在 R9-F1（同名 confirm 不可区分）与 R9-F2（running 在 spawn 前），「互斥 + fencing」在当前协议下不成立——路径 A/B/C（§2.1）都可达。

这不是独立于 R9-F1 的新缺陷，而是 **R9-F1 在另一张表里的投影**：outbox 表把未兑现的保证写成了已结算的事实。实现者若先读 §6.4（副作用语义总表）再设计 outbox worker，会认为 agent 启动的 effectively-once 已由底层保证，不再追问 §8.4 的协议细节。

**建议**：R9-F1 修正后，同步更新此行的措辞，使其与修正后的协议动词一致。当前保持原样会让两张表互相声称一个两处都未兑现的保证。

### N2 · P3：§10.1 恢复矩阵缺「claim 未确认、wrapper 进程在」组合

**位置**：§10.1 第 727–740 行。

矩阵第 729 行覆盖「`pending` / `starting` | claim 未确认、**无进程**、无 `control.json`」。但 daemon 崩溃重启后还可能观测到：attempt `pending`、claim 存在但未确认、**wrapper 进程仍在运行**（wrapper 已读 bootstrap 但尚未调 confirm，或 confirm 在途时 daemon 崩溃）。

当前矩阵对这一组合无定义。若按第 729 行处置（递增代次重发），旧 wrapper 的 confirm 会因代次不符被拒并退出，新 wrapper 启动——行为正确但矩阵没写。若实现者只看矩阵，可能不知道该杀旧 wrapper 还是等它。

**建议**：补一行「`pending` / `starting` | claim 未确认、wrapper 进程在、无 `control.json` | 递增代次重发（旧 wrapper confirm 必因代次失败自退）」。

## 5. 技术栈切换（Go）复核

review-10 已确认 ADR-009 合格，本轮抽查无异议，仅记录三点：

- **§5.2 decode gateway** 是正确的结构性补偿——Go 默认反序列化与 fail closed 相反，用「单一入口 + DisallowUnknownFields + 指针字段 + golden test」四条把它变回结构保证。V14 测试锁定「未知字段必拒、必填缺失必拒」。
- **§7 写连接池上限钉为 1** 从根上排除了 `modernc.org/sqlite` 驱动内部并发导致的 SQLITE_BUSY，比靠 `busy_timeout` 兜底更强。
- **§14.7 的诚实度**值得肯定：明确承认重议触发是「需求变化」而非「原仲裁判错」，并记录了「ADR-001 把 PRD 不做常驻服务写进 Go 否决理由，而 PRD §9.3 部署行写的就是守护进程——这条矛盾活了三轮评审没被发现」的文档级教训。

## 6. 确认无问题的部分（抽查）

以下关键承接本轮独立核对，确认成立：

- **§8.5 Gate 缓存键**（`gate_input_hash`）完备性论证正确：整份快照摘要替代维度枚举，riskScore 来源与版本入快照，新增输入字段自动改变摘要——D0.2 的缓存键缺陷（我 review-04 的 M1 / review-05 的 R5-F1）已彻底闭合。
- **§6.4 Change 创建的「marker 定位 + ID 收敛」五步协议**逻辑自洽，覆盖了已关闭/已合并状态搜索、`SemanticConflict` 不接管分支等边界。
- **§8.4 context.md 从 base 读**的理由（不给 agent 改自己提示词的间接通路）正确，代价（改 context 须提交 base 才生效）如实写出。
- **§11 启动期探测分两级**（进程级 vs 项目级）解决了 D0.2 「坏仓库停全部项目」的矛盾，forge CLI 留进程级的理由正确引向 PRD §9.3 明文。
- **§3.2 沙箱挂载集**（`run.sock` + 本 attempt 的 run dir）修正了 D0.2 「只挂 run.sock」会把上报面一起关掉的错误。
- **V10–V15 测试新增项**（控制面授权、零配置启动、critical 熔断、边界解码、跨平台矩阵）针对性都正确。

## 7. 给 R9-F1 / R9-F2 的处置建议

按 DESIGN 自己「结构性保证优先于纪律性保证」的标准，这两项应正式修订协议，不降级为 spec 接缝：

1. **两动词拆名 + 会话 ID**：`claim:acquire`（pending → starting）签发 wrapper 会话 ID；`claim:start`（starting → spawning）要求会话 ID + 代次校验。RPC 重试时 daemon 可凭会话 ID 判断是 acquire 的幂等重放还是 start 的首次调用。
2. **Run → `running` 改以 spawn 成功为依据**：wrapper 在 spawn 成功后写 `agent_started`（含 Agent PID/启动时间）或调 `claim:started`，daemon 据此推进。`control.json` 区分 wrapper PID 与 agent PID。
3. **attempt 生命周期补 `spawning` 阶段**：`starting → spawning（spawn 已发、started 未确认）→ running`。恢复矩阵补「许可已发未 spawn」「spawn 已成、started 未落库」两行。
4. **outbox worker 对启动类 operation 增加会话内原子认领语义**（§6.4），禁止同一 operation 在同一 daemon 会话内并发执行。
5. **§10.1 第 739 行论证修正**：保护并发 confirm 的是 attempt 状态 CAS，不是 claim 唯一约束。
6. 处置时增设 §14.8 对账节，记录 review-09 / review-10 / 本轮的处置映射。

## 8. 复评通过条件

沿用 review-09 §5 与 review-10 §6 的条件，核心两条：

1. **R9-F1**：启动协议给出唯一、可线性化的 spawn 许可；同代重复 wrapper、RPC 重试与 claim 换代均不能双起或零启动。§10.1 第 739 行论证修正。§6.4 agent 启动行措辞同步。
2. **R9-F2**：Run 只在有可验证的 Agent 已启动证据后进入 `running`；恢复矩阵覆盖许可与真实 spawn 两侧的崩溃窗口；§8.4 第 479 行的自我证伪表述修正。

次要条件（可在 specs 阶段收敛，不阻断 WBS）：R9-F3（Brain trace 关联键）、R9-F5（README 阈值）、R10-N1（循环引用）、N2（恢复矩阵补行）。

---

_评审人：glm-5.2（MiMo Code Agent）_
