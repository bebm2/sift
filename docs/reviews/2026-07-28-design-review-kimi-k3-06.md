# DESIGN.md 架构评审（第 18 轮，D0.10 复评）

> 日期：2026-07-28  
> 评审人：**kimi-k3**  
> 评审对象：[`docs/DESIGN.md`](../DESIGN.md) **D0.10**（1202 行，对应 PRD V0.8）  
> 评审依据：[`docs/PRD.md`](../PRD.md) V0.8、ADR 001–013、[`design-review-15`](2026-07-28-design-review-kimi-k3-05.md)（我，D0.8 通过 + 两项 P3）、[`design-review-16`](2026-07-28-design-review-gpt-5.6-sol-01.md)（GPT-5.6 SOL，D0.9 阻断：3 P1 / 5 P2 / 1 P3）、[`design-review-17`](2026-07-28-design-review-gpt-5.6-sol-02.md)（作者自查）  
> 评审重点：（a）review-16 三项 P1 是否真正关闭（不采信 review-17 自查结论，独立核销）；（b）我 review-15 的 N1/N2  closure；（c）对 D0.10 三个新机制（merge expected-head CAS、retry 两段式收敛、进程组资格）的对抗性复查

## 1. 结论

**通过。review-16 的阻断解除，维持「可以进入 WBS」的判断。**

我独立核销了 review-16 的全部九项（不依赖 review-17 自查），三项 P1 的关闭都是机制级的：merge 改远端 expected-head CAS（ADR-011）、`startup_stall` 仅显式 `reject` 写终局 marker（ADR-013）、retry 探测成功以一笔 CAS 事务原子收敛（ADR-013）。我 review-15 的两项 P3 也随 F2/F5 一并关闭，且关闭质量高于我原建议的处置（N2 从「补一句假设声明」升级为按 agent/版本管理的拓扑资格制度，ADR-012）。对抗性复查（§4）未在三个新机制上找到新的 P1/P2。

本轮新发现仅一项 P3（§5）：ADR-010 决策 6 仍用旧名 `attempt_decision`，未加指向 ADR-013 的更名指引。不阻断 WBS。

两句记录：其一，§14.12 对「D0.9 误把自查归因为我的 review-15」的更正处理得当——不改只读存档，在 §14.12 / PRD §13 / CHANGELOG 三处统一更正，事实治理合格。其二，review-17 是作者自查，其结论本轮经我独立复核确认成立；但自查不能替代独立评审，这一分工建议保持。

## 2. review-16 核销（独立验证）

| 编号 | 级别 | 结论 | 证据 |
|------|------|------|------|
| F1：旧 merge operation 可把 Gate(A) 用于新 head B | P1 | **关闭** | §6.4（300、328）：operation 固化 `expected_head_sha=A`，适配器在**同一个远端 merge 请求**中比较 head；预读仅用于尽早发现 stale，不替代远端条件请求；head 已变或 CAS 失败 → 旧 operation 收敛 stale/no-op，新 head 重新冻结输入过 Gate；平台无法兑现则自动合并能力探测失败、**不得退化为无条件 merge**（§8.1 406、ADR-011）。V3 双平台契约断言（885）、V7 幂等断言「Gate(A)→merge(A) 后 head 变 B 必须 stale/no-op」（889） |
| F2：升级上限同时是 `hold` 与终局 | P1 | **关闭** | §10.1 marker 表（813–821）：`retry` 请求 / `hold` / `escalate` / 封顶 `hold` 全部**不写** marker、保持待决与事实窗口；仅显式 `reject` 与探测结果 `retry_after_absence` 写 `attempt_resolution`。我 review-15 N1 的悬空指称（「过期升级的处置」）随之消除 |
| F3：retry 探测成功缺原子收敛 | P1 | **关闭** | §10.1（819、823）：请求段不关闭 Interrupt；结果段是一笔 CAS 事务，以 Run 版本 / attempt generation / 待决 Interrupt / probe operation 为前置，一次提交消失证据、旧 attempt 终结、隔离解除、Interrupt 关闭、Run→`queued`、新 attempt + claim、启动与回执 operation、事件——任一前置变化整笔拒绝重算；新 attempt 只能在该事务提交后派发。V2 逐点崩溃断言全有或全无（884）、V4 断言「创建且仅创建一个新 attempt」（886） |
| F4：Change 创建时机不一致 | P2 | **关闭** | §8.4（516）与 PRD §5.1（300）：只有成功 `result.json` 已校验、最终 head 冻结且分支有提交才写 `createChange`；中间提交与失败 attempt 不触发 |
| F5：进程组不脱离是假设非物理保证 | P2 | **关闭** | §8.4（446、452）：明示契约边界「直接父子关系只保证 spawn 时刻」；按 agent CLI + 版本跑拓扑资格测试、`doctor` 报告 `process-group-verified/unverified`；未验证组合身份含糊时不得自动 retry，保持隔离转人工（§10.1 788、ADR-012）。V4 覆盖脱组构造（886）。我 review-15 N2 关闭 |
| F6：marker 全状态搜索无 Forge 端口 | P2 | **关闭** | PRD §5.2 增 `findChangeForCreateOperation(opKey, branch, base)`（341），跨全状态返回唯一命中/未命中/同 base-head 冲突；outbox 不得 raw API 旁路（§6.4 324、§8.1 406、V3） |
| F7：V0 TTS 与跨平台、单 Channel escalation 冲突 | P2 | **关闭** | `sift speak` 移出 V0（§8.7 607，PRD §11 871 降为后续阶段且以能力 spike 为前提；§8.10 CLI 清单已无 speak）；escalation 定义为当前 Channel 强提醒档位、不支持则原通道重推、不要求第二 Channel（PRD 229、§8.7 升级段） |
| F8：review-15 对账失真 | P2 | **关闭** | §14.12 统一更正，PRD §13（920）与 CHANGELOG（17、23 行）同步，reviews 存档未改 |
| F9：「不落盘凭证」范围含糊 | P3 | **关闭** | PRD §5.2（317）与 §9.3（806）收窄为「不落盘 forge 凭证」，本地 capability 边界归 TM6 如实披露 |

## 3. 我 review-15 遗留的最终状态

- **N1（P3，「过期升级的处置」指称悬空）：关闭。** D0.10 的 marker 表把非终局动作（retry 请求 / hold / escalate / 封顶 hold）与终局落定（reject / retry_after_absence）分成两列，正是我要求的「仅 retry/reject 写 marker」的精确化——比我的原文更细：retry 的探测成功是**结果**而非决定，marker 因此更名 `attempt_resolution`（ADR-013 决策 1），更名理由成立。
- **N2（P3，脱组假设未声明）：关闭。** 不止声明，还建成了资格制度：拓扑资格测试按 agent/版本管理、未验证组合降级为人工处置、V4 构造脱组断言。我把「已知 agent CLI 无脱组行为」从一句假设升级为一项可持续验证的验收，这是正确的处置方向。

## 4. 新机制的独立对抗复查（通过项）

- **merge CAS 竞态**：head A→B→A（force-push 回滚）不构成绕过——head 变 B 时旧 operation 已终止 stale，head 回到 A 需新 Gate；若期间 reviews/Checks 变化则 `gate_input_hash` 变化、缓存必失效，不会复用旧 verdict。
- **merge 重试与远端证据**：operation key 判重 + 远端已合并状态收敛 + CAS 失败映射，三段都在 §6.4 / ADR-011 内，无 raw API 旁路（406）。
- **retry 探测在途收到合法 started**：探测证明的是旧执行体消失，而 started 到达意味着 wrapper 刚 spawn——进程组存在，探测不可能同时确认消失；探测前置（attempt 冻结）随事实优先事务失效，作废回执收敛（886 已列断言）。两条 CAS 前置链自洽。
- **unverified 组合的降级一致性**：进程组证据对 unverified 不成立后，受控终止永远走「确认不了」分支 → `startup_stall` → 人工；与「不得自动 retry」一致，不存在某条路径仍能自动新开 attempt 的漏口（452、788 互查一致）。
- **结果事务的派发顺序**：新 attempt 只能在结果事务提交后被 worker 派发（823），排除了「Run 已 queued 但启动未入 outbox」的中间态。

## 5. 本轮新发现

### N1 · P3：ADR-010 决策 6 仍用旧名 `attempt_decision`，无更名指引

**位置**：`docs/decisions/010-attempt-spawn-handoff.md` line 21。

ADR-013 决策 1 把 marker 更名为 `attempt_resolution`，DESIGN 正文与 §6.5 仲裁表已全部用新名，但 ADR-010 决策 6 原文未动，单独阅读该 ADR 的代理会拿到旧术语。本项目已有在 ADR 文首加变更指引的先例（ADR-001 的 superseded 横幅），建议在 ADR-010 决策 6 处加一行「名称已由 ADR-013 修订为 `attempt_resolution`」。ADR 不改历史论证是对的，但术语指针不属于改写历史。

## 6. 维护提醒（沿用 review-17，本轮复核同意）

1. DESIGN 1202 行已到提醒线，§15 已自记「后续新增主题前必须先拆分」——WBS 或任何新主题落笔前应先执行拆分（候选 §8.1 / §8.7 / §9.1）。
2. `process-group-verified` 是受支持 Agent 的**资格结论**，不是 OS 沙箱；WBS 不得把拓扑资格测试写成 TM6 已闭合的证据。

## 7. 复评通过条件

本轮无阻断项。下次复评触发条件（先到为准）：

1. DESIGN 拆分落地——届时评审拆分后的索引与默认上下文集是否符合 docs/README 的加载规则；
2. `WBS.md` 落成——重点评审验收是否携带：§14.8 两条维护义务、§14.10 拓扑断言、ADR-011 的能力探测失败即禁用自动合并、ADR-012 的资格制度、以及「回放集 + 认证投影与 Gate 同片」等 §13 四条硬约束；
3. N1 修订可随任一实质变更顺带进行，不单独触发复评。

---

_评审人：kimi-k3_
