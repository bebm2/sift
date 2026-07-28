# DESIGN.md 自查（第 17 轮，D0.10）

> 日期：2026-07-28  
> 自查人：GPT-5.6 SOL  
> 对象：[`PRD.md`](../PRD.md) V0.8、[`DESIGN.md`](../DESIGN.md) D0.10、ADR-011～013  
> 基线：[`design-review-16`](2026-07-28-design-review-gpt-5.6-sol-01.md) 的 3 项 P1、5 项 P2、1 项 P3

## 1. 结论

**自查通过。review-16 的阻断项已关闭，可以进入 WBS。**

本轮没有发现新的 P1/P2。保留两项非阻断维护提醒：

1. DESIGN 已到约 1200 行提醒线；正文已明确下一次新增主题前先拆分。
2. `process-group-verified` 是受支持 Agent 的资格结论，不是 OS 沙箱；WBS 不得把资格测试写成 TM6 已闭合。

## 2. review-16 逐项核销

| 项 | 结论 | 核销证据 |
|----|------|----------|
| F1 merge 缺 expected-head CAS | **关闭** | PRD §5.2 将动词改为 `mergeChange(id, expectedHeadSha, method)`；DESIGN §6.4 要求同一个远端 merge 请求原子比较 head，旧 operation stale/no-op；ADR-011；V3/V7 覆盖 Gate(A)→head B 交错 |
| F2 `startup_stall` 封顶同时是 hold 与终局 | **关闭** | PRD §4.2 将“终态”改为“封顶后的去向”；DESIGN §10.1 规定只有显式 `reject` 立即落定，`escalate` / 封顶 `hold` 不写 resolution、不关闭 Interrupt；ADR-013；V4 覆盖封顶后迟到 started 仍事实优先 |
| F3 retry 成功缺原子收敛 | **关闭** | DESIGN §10.1 定义单一 CAS 事务：消失证据、旧 attempt、resolution、隔离、Interrupt、Run→`queued`、新 attempt/claim、启动与回执 outbox、事件全有或全无；ADR-013；V2/V4 逐边界崩溃注入 |
| F4 Change 创建时机不一致 | **关闭** | PRD §5.1 与 DESIGN §8.4 都限定为成功 `result.json` 已校验、最终 head 冻结且存在提交；中间提交与失败 attempt 不触发 |
| F5 进程组假设未声明 | **关闭** | DESIGN §8.4/§9.1/§10.1 明示不脱组契约与 TM6 边界；ADR-012；`doctor` 和 V4 按 agent CLI + 版本报告/验证 |
| F6 Change marker 查询无端口 | **关闭** | PRD 增 `findChangeForCreateOperation(opKey, branch, base)`，同时覆盖全状态 marker 与同 base/head 无 marker 冲突；DESIGN 禁止 outbox raw API 旁路；V3 覆盖双平台契约 |
| F7 TTS / 单 Channel 冲突 | **关闭** | `sift speak` 移出 V0；V0 只保留 modality 与可朗读结构约束。`escalate` 定义为当前 Channel 的强提醒档位，不要求第二 Channel |
| F8 review-15 对账失真 | **关闭** | review-15 保持只读；DESIGN §14.12、PRD §13、CHANGELOG 均更正为“两项原始遗留 + D0.9 自查” |
| F9 “不落盘凭证”范围含糊 | **关闭** | PRD §9.3 收窄为“不落盘 forge 凭证”，本地 operator/run/bootstrap capability 明确归 DESIGN 与 TM6 |

## 3. 对抗性交错复查

### 3.1 Gate(A) 与 merge(B)

1. Gate 冻结 head A 并写 merge(A)；
2. Change 更新到 B；
3. worker 预读发现 B，可直接把旧 operation 标 stale；
4. 即使预读仍见 A、随后才变 B，远端 merge 请求的 expected-head CAS 仍拒绝；
5. B 重新组装 Gate 输入后才可能生成 merge(B)。

**结果：旧 Gate 结论没有作用于新 head 的路径。**

### 3.2 retry 探测失败直到封顶

- retry 请求不写 `attempt_resolution`；
- 每次失败复用同一 Interrupt、升级并轮换 nonce；
- 达 `max_escalations` 后只转 `hold`，Interrupt 仍待决；
- 此后合法迟到 `started` 仍可赢得 CAS，Run 回 `running`。

**结果：定时器不会冒充人的终局决定。**

### 3.3 retry 探测成功与迟到事实竞争

- started 先提交：事实优先，关闭 Interrupt 为 `superseded_by_fact`，retry 结果事务因版本前置失败而整笔拒绝；
- retry 结果事务先提交：原子写 `attempt_resolution=retry_after_absence`、Run→`queued` 与新 attempt/outbox，旧 attempt 的迟到事实只被吸收，不推进旧路径；
- 任一事务边界崩溃：SQLite 原子性保证结果全有或全无，外部启动/回执由 outbox 续跑。

**结果：没有“Interrupt 已关但无新 attempt”或两个合法 owner 的部分提交窗口。** 该结论以 ADR-012 的进程组契约为前提；未验证 Agent 不走自动 retry。

### 3.4 Change 创建崩溃对账

- 运行中有中间 commit：没有成功 Layer 2 证据，不创建 Change；
- 失败 attempt 留有 commit：不创建；
- 创建远端成功、本地记账前崩溃：`findChangeForCreateOperation` 跨全状态按 marker 找回；
- 同 base/head 有人工 Change、无 marker：返回冲突，不接管。

**结果：确定性创建与 effectively-once 对账都有端口承接。**

## 4. 文档一致性检查

已执行：

- PRD / DESIGN / ADR 相对链接存在性检查：通过；
- Markdown 表格列数与重复标题检查：通过；
- `git diff --check`：通过；
- 版本与追溯检查：PRD V0.8 ↔ DESIGN D0.10，§14.13 对账完整；
- stale 术语检查：V0 CLI 不再含 `sift speak`；旧 merge 签名不再存在；现行人工态不再把封顶 `hold` 写成终局；
- review-15 存档未修改。

## 5. WBS 入口约束

WBS 至少显式携带以下新增验收：

1. Forge 双平台 expected-head CAS 与 Change create-operation 对账契约测试；
2. retry 成功事务逐边界崩溃注入；
3. 封顶 `hold` 后迟到 started 的事实优先测试；
4. Change 只在成功 Layer 2 证据后创建；
5. 每个真实 agent CLI / 版本的进程组拓扑资格证据与 `doctor` 姿态；
6. `process-group-verified` 不得被表述为 TM6 或沙箱闭合。

---

_自查人：GPT-5.6 SOL_
