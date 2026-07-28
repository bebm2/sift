# DESIGN.md 架构评审（第 14 轮，D0.7 复评）

> 日期：2026-07-28  
> 评审人：**Codex（GPT-5）**  
> 评审对象：[`docs/DESIGN.md`](../DESIGN.md) **D0.7**（1107 行，对应 PRD V0.6）  
> 评审依据：[`docs/PRD.md`](../PRD.md) V0.6、ADR 001–010、[`design-review-13`](2026-07-27-design-review-kimi-k3-04.md)  
> 评审重点：（a）review-13 N1/N2 是否真正关闭；（b）两个后端的启动拓扑与终止结局；（c）`spawning → waiting_human` 后旧执行者迟到恢复的交错；（d）人工分支的动作、幂等与授权口径

## 1. 结论

**不通过，暂不建议进入 WBS。**

D0.7 对 review-13 的两项遗留修得正确：后端只承载 wrapper、Agent 恒为 wrapper 直接子进程并留在同一进程组，使 `process` / `tmux` 共用同一观测原语；`kill`、`retry`、恢复三条触发路径的终止结局也已明确区分。把受控终止纪律推广到 `running` 阶段同族路径，是必要且正确的自查。相关正文、ADR-005/010 与 V4 已同步，本轮不再保留 review-13 的 N1/N2。

但复评重放「受控终止失败 → 转人工」后的交错，发现一项新的协议阻断：Run 已从 `queued` 转为 `waiting_human` 后，旧 wrapper 的 session / permit 仍因冻结而有效；若旧执行者随后恢复并提交合法 `claim:started`，现有协议仍只定义 Run `queued → running`，没有定义它与人工决定谁先线性化、Interrupt 如何关闭、活着的 Agent 如何被接管或终止。该窗口可稳定落入“Agent 已活着、attempt 仍 `spawning`、Run 为 `waiting_human`”或跨投影不一致状态，与 Q1 的无幽灵状态及 §10.1 的唯一收敛动作冲突。

本轮共发现 **1 项 P1、3 项 P2**。F1 是进入 WBS 前的阻断项；F2–F4 应与 F1 同轮关闭，避免把人工分支的字段级歧义下放给实现阶段猜测。

## 2. review-13 遗留核销

| 项 | 结论 | 证据与判断 |
|----|------|------------|
| N1：tmux 下缺少等价的进程组观测原语 | **关闭** | DESIGN §8.4 裁定后端只决定 wrapper 跑在哪里，Agent 恒为 wrapper 的直接子进程且在其进程组内；真 PTY 改由 wrapper 分配并中继。ADR-005/010 与 V4 同步锁定该拓扑。该方案比同时维护 PGID / tmux 会话两套恢复证据更小且更强 |
| N2：`kill` 与 `retry` 的终止结局未区分 | **关闭** | DESIGN §8.10 与 §10.1 路径表明确：恢复按策略新开或失败，`retry` 新开 attempt，`kill` 不新开且 Run 转 `failed`；V4 增加相反断言 |
| D0.7 自查：`running` 后端会话在、wrapper 不在时直接 `orphaned` | **关闭** | §10.1 已把受控终止流程推广到所有“执行者可能仍存活却准备判 `orphaned`”的路径，并要求无法确认时转人工 |

上述结论只核销 D0.7 的修订目标，不代表人工分支已经闭环；新阻断见 F1。

## 3. 本轮发现

### F1 · P1：Run 转入 `waiting_human` 后，迟到的合法 `claim:started` 没有线性化规则

**位置**：DESIGN §8.4「attempt 启动协议」步骤 8、§10.1 恢复矩阵与人工分支；ADR-010 决策 4–5；PRD §4.1。

以下交错全部符合当前文本：

1. Run 为 `queued`，attempt 已进入 `spawning`；旧 wrapper 持有仍有效的 session 与唯一 spawn permit。
2. wrapper 在 permit 后暂停，或进程组因系统状态暂时无法被可靠终止。恢复、超时、`kill` 或 `retry` 进入统一受控终止流程。
3. 有界终止后仍无法确认进程组消失。§10.1 要求生成一次 `failure_review` Interrupt，Run `queued → waiting_human`，attempt 与 claim 保持冻结。冻结禁止换 owner，但没有作废旧 session / permit——也不能安全作废，因为 OS `spawn` 不消费 fencing token。
4. 旧 wrapper 随后恢复，用已经签发的 permit 启动 Agent、原子写 Agent 身份，并提交凭据与证据均合法的 `claim:started`。
5. §8.4 步骤 8 与 §10.1 两条“补 started”路径只定义 attempt `spawning → running` 加 Run `queued → running`；此时 Run 实际为 `waiting_human`。

当前文本没有一种实现能同时满足全部承诺：

- 若 attempt 与 Run 在同一事务 CAS，Run 前置状态不符会让整笔 started 失败，Agent 却已经活着，wrapper 只能重试，系统仍停在人工态；
- 若两笔更新可部分提交，会得到 attempt `running`、Run `waiting_human` 的跨投影矛盾；
- 若实现自行允许 `waiting_human → running`，当前 Interrupt、注意力记账以及与人的 `kill` / `retry` 并发决定又没有关闭规则；
- `result.json` 迟到也有同一问题：恢复矩阵要求先补 `queued → running` 再按结果推进，但 `queued` 已不存在。

这不是低概率异常。人工分支成立的前提恰好是“旧执行者可能仍活着但系统证明不了”，因此旧执行者恢复并补齐事实就是该分支必须建模的正常输入。

**关闭条件**：

1. 为“迟到启动事实”和“人工决定”指定明确线性化点。建议采用事实优先但只限于人工决定尚未提交的窗口：合法 `started` / 身份一致的 `result` 先到时，在同一事务内完成 attempt 推进、Run `waiting_human → running`、将当前启动停滞 Interrupt 标为 `superseded_by_fact`，并接管监督；不能只改其中一项。
2. 人工决定先提交时，记录不可逆的 decision marker。随后到达的 started 证据不得直接推进 Run，但必须被吸收为新的可终止身份，继续执行该决定对应的受控终止；不能简单拒绝 RPC 后放任 Agent 存活。
3. 明确 `claim:started`、恢复补 started、`result.json`、Interrupt 指令四者共享同一套 CAS 前置条件与幂等结果。
4. 在 V2/V4 增加至少四组确定性交错：Interrupt 事务前 / 后收到 started；人工决定前 / 后收到 started；对 `result.json` 重放同组交错。断言不得出现活着但无人监督的 Agent、悬空 Interrupt、部分投影提交或第二个 owner。

### F2 · P2：启动停滞 Interrupt 的可选动作与超时结局没有可执行契约

**位置**：DESIGN §10.1 人工分支；PRD §4.1–4.4、§7.1；DESIGN §8.8。

§10.1 说人工分支的选项是 `kill` 与 `retry`，下一段却说人决定“kill 还是放弃”；PRD §4.4 同样写“kill 还是放弃”。更关键的是，这个 Interrupt 正是在受控终止已经无法确认消失后生成的：人再选一次 `kill` 或 `retry` 并没有新增证据或能力，只会重复相同流程并再次落回同一人工分支。

通用指令面又允许 `/sift approve`、`sift:approved`、`reject`、`hold`，但文档没有规定指令必须属于当前 Interrupt 的 `options[]`。PRD 状态图允许 `waiting_human` 经 approve 回到 `running / queued`；对“旧执行者尚未证明消失”的 Interrupt，这种通用转移不能由实现自行套用。另一个未闭合点是 `failure_review` 默认 `on_expire: auto_reject`：它会把 Run 转为 `failed`，但旧执行者与冻结 claim 可能仍在，之后的迟到 started/result、继续监督与人工 retry 均无契约。

**关闭条件**：

- 给该场景稳定的 cause / subtype（例如 `startup_stall`），并按 subtype 约束可接受指令；评论指令与审批标签都必须验证“动作属于当前 Interrupt 的 options”，不能只验证 nonce。
- 定义真正能推进事实的动作。至少需要“外部处置后重新探测（recheck）”与“保持隔离并继续等待（hold/quarantine）”；若保留 `kill` / `retry`，要说明它们在前一次终止失败后如何获得新证据或为何不会循环收费。
- 定义“放弃”的系统含义：是否维持 attempt 隔离、是否继续监督、何时允许 worktree 清理或后续 retry。不得把“Run 进终态”等同于“执行者已消失”。
- 为该 subtype 明确 `on_expire`；在旧执行者未确认消失时，不应沿用低价值 `failure_review` 的通用 `auto_reject` 而丢失处置责任。

### F3 · P2：“只发一次 Interrupt”缺少可跨并发与崩溃成立的幂等身份

**位置**：DESIGN §6.3–6.5、§8.7、§10.1。

单一发射器只保证入口唯一，不等于同一故障只生成一条记录。启动超时扫描、恢复扫描、重复的 `kill` / `retry`、进程观测回调都可能并发发现同一个停滞；§8.7 虽声明发射器负责“去重”，但 §6.5 没有给 Interrupt **生成**稳定键：

- `run_id + nonce` 是已生成 Interrupt 的指令防重放键，不能给生成过程去重；
- `interrupt:{interrupt_id}:publish` 是发布 operation key，前提是已经选定了同一个 `interrupt_id`，也不能阻止两个发现者各建一条 Interrupt；
- “一次 Interrupt + Run `waiting_human` + 注意力扣费 + 发布 outbox”若不在同一事务，崩溃可留下有状态无 Interrupt、重复扣费或有 Interrupt 无发布的窗口。

**关闭条件**：为该故障定义稳定键，例如 `(run_id, attempt_no, generation, cause=startup_stall)`，并明确唯一约束；在一次 §6.3 事务内提交 manual marker、Run 转移、Interrupt、预算记录与发布 outbox。V4 应让超时 / recovery / kill / retry 四个发现者并发，另在事务边界逐点崩溃，断言始终只有一条当前待决 Interrupt、一次配额消耗和一条可重放的发布 operation。

### F4 · P2：Q7 的“任何本地 RPC”与已承认的 operator capability 暴露面直接矛盾

**位置**：DESIGN §2.2 Q7、§8.9、§9.1 TM6、V10；ADR-008。

Q7 声称 agent 因拿不到启动凭据而无法经“任何本地 RPC”改变 Run；但 §8.9 明确承认同 UID agent 在 V0 可读取 `~/.sift/operator.token` 并通过运维 socket 调 `sift kill` / `sift retry`，§9.1 将其列为未闭合 TM6，V10 甚至要求该调用在 V0 **预期成功**。启动凭据只证明 agent 不能冒充 wrapper 调三个 handoff 动词，不能推出它无法通过另一端点与另一凭据改变 Run。

Q7 后半句“若无法保证则暴露面可见”不能消除前半句的绝对断言；同一个验收场景同时要求“不可能”和“预期成功”，WBS 无法据此形成单一验收结果。

**关闭条件**：把 Q7 拆成两个可独立判定的性质：

1. **run token / Report 端点性质**：run token 不能调用状态转移或 wrapper handoff 动词——V0 必须拒绝；
2. **agent 进程整体能力边界**：V0 因同 UID operator capability 尚未闭合——攻击复现必须成功且 `doctor` 必须报告，未来沙箱后端再把预期翻转为拒绝。

V10 分别引用两条，不再让一个编号承载互相相反的断言。

## 4. 复评通过项

除 review-13 的核销项外，本轮重检以下路径未发现新的冲突：

- `process` 与 `tmux` 后端保持同一 wrapper → Agent 父子关系与进程组语义；tmux 会话不成为完成事实源。
- permit 响应重放仍由 wrapper 进程内 one-shot guard 挡住第二次 spawn；同代竞争由 operation lease、instance CAS、session 与 permit 分层约束。
- `kill` 成功确认消失后不新开 attempt；`retry` 成功确认消失后才新开 attempt。
- `running` 阶段后端会话残留但 wrapper 缺失时，先证明进程组消失再 `orphaned`，不再直接释放 worktree 写权。
- ADR-005、ADR-010、§8.4、§10.1 与 V4 对 D0.7 新拓扑的口径一致。

## 5. 复评条件

下次无需全量重审，按以下条件做定向复评即可：

1. 关闭 F1：给出 started/result 与人工决定的线性化表，并把全部交错加入 V2/V4。
2. 关闭 F2：定义 `startup_stall` 人工动作、reason-scoped 指令与不会遗失活执行者的过期语义。
3. 关闭 F3：给出 Interrupt 生成稳定键、唯一约束与原子事务边界。
4. 关闭 F4：拆分 Q7 的端点性质和 agent 整体能力边界，V10 分别验收。
5. 同步 ADR-010、PRD §4.1–4.4 / §7.1、DESIGN §6.5 / §8.7 / §10.1，避免只在恢复矩阵局部打补丁。

F1 关闭前不建议编写 WBS；否则“Runtime 故障注入通过”的验收标准仍需要实现者自行发明关键竞态语义，WBS 只会把未决架构问题伪装成任务拆分。

---

_评审人：Codex（GPT-5）_
