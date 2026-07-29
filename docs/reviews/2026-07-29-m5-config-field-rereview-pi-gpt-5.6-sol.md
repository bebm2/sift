# config.md M5 增补字段定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/config.md`](../specs/config.md) 当前稿（基线 `b08eae3`）
> 前次评审：[`2026-07-29-m5-config-field-review-pi-gpt-5.6-sol.md`](2026-07-29-m5-config-field-review-pi-gpt-5.6-sol.md)
> 声称关闭：#309 / PR #312（commit `06ffdcf5`，merge `c30c6bb`）

## 1. 结论

**FAIL（6×P1）。**

MC1–MC4 的主要设计方向已经补入：CAS 零行不再冒充额度耗尽；初发与升级首次 critical 均经过 admission；`local` 会规范化进有效快照，下一摘要时刻和 DST gap/fold 有裁决；storage/outbox/ledger 也新增了 batch、member、operation 和逐成员 delivery 身份。

但关闭仍不完整：前次明确要求的并发/回滚与日历 vectors 没有落文；从 `quota_batched` 升级为 critical 时，nullable charge 与 critical admission/Ledger 约束互相冲突；当前 [`interrupt.md`](../specs/interrupt.md) 又在后续 PR #316 中换回另一套 batch 表、键、状态和写端口。除此之外，daily batch 的当前 identity 会在同一摘要时刻生成两批，critical episode 的闭区间计数与 `due_at` 也相差一个毫秒边界。实现仍无法从这组规格生成唯一 schema、operation key 和边界测试。

因此本次**不得把 `config.md` 的 M5 增补视为通过或升 active**。该文件现有 `status: active` 是早于本次增补的 M1–M4 基线，本次不改头部状态，也不回退既有基线。

## 2. 剩余可执行 P1

| ID | 对应前次发现 | 剩余问题 | 为什么阻断 | 可执行关闭条件 |
|---|---|---|---|---|
| RMC1-A | MC1 | **CAS 语义已修正，但承诺的并发与故障 vectors 未落文。** config §3.9、storage §9.1 只有规则；interrupt §9.2 只泛称“配额耗尽的 non-critical 并发 CAS”，没有前次关闭条件指定的 `limit=2`、`limit=1` 和故障回滚结局。 | 不能区分“竞争后仍有额度”“权威超额”“重试耗尽/存储故障”三类结果，也不能据此生成确定性 V8 测试。 | 在派生验收中逐项冻结：`limit=2` 两个并发候选均 `quota_charged` 且 counter=2；`limit=1` 恰一条 charge、一条 `quota_batched`；CAS 重试耗尽、SQLite/事务故障时 Interrupt/admission/counter/entry/batch/outbox 全部回滚且绝不写 `quota_batched`。 |
| RMC2-A | MC2 | **`quota_batched → critical` 没有可表示的 charge/admission/Ledger 组合。** storage §6.1/§6.3 规定 quota-batched Interrupt 的 `charged_budget_entry_id=NULL` 且创建后不可改，同时“任一 `critical_*` 必须引用实际 charge”；config §3.9 又禁止升级新增 charge。ledger §2.4 只允许 `quota_batched` member 的 charge 为空。 | 一个已因日配额入批的 high Interrupt 到期升级为 critical 时，`AdvanceInterrupt` 无法合法写 `critical_admitted|critical_fused`，也无法为成功 critical delivery 生成合法 `AttentionDeliveryV1`；熔断可被该真实路径卡死。 | 明确并统一 config/storage/interrupt/ledger：升级 critical admission 在初发有真实 charge 时复用它；初发为 `quota_batched` 时保持 charge 为 NULL、不得补造 entry，但仍可写唯一 critical admission/fuse evidence。同步冻结 delivery 的 admission/nullable-charge 组合及指标去重身份，并补 quota-batched→critical admitted、fused、重放三组 vectors。 |
| RMC3-A | MC3 | **日历规则已给出，但前次要求的边界 vectors 未落文。** 当前没有固定 zone/epoch 的跨午夜、恰在/晚于摘要时刻、spring-forward gap、fall-back fold 期望值。 | prose 无法防止实现把“严格晚于”、gap 后首个 instant 或 fold 第一次出现写成另一种合法库行为；config hash 与历史回放的边界仍无可执行判据。 | 增加带具体 IANA zone、输入 instant 和期望 `quota_day/due_at_ms` 的 vectors：午夜两侧；入批恰在及晚于 `daily_summary_at`；一次 DST gap；一次 DST fold；并断言 `local` 规范化后的 zone 名进入 canonical JSON/hash，运行期机器时区变化不改历史结果。 |
| RMC4-A | MC4 | **batch 协议在相邻规格中已分叉。** storage/outbox 使用 `attention_batches`、`attention_batch_members`、`PrepareAttentionBatch`、`collecting|sealed|delivered|cancelled` 和 `attention-batch:<batch_id>:publish:1`；interrupt §8.3 使用 `interrupt_batches`、`interrupt_batch_memberships`、`PrepareInterruptBatchDelivery`、`open|published|suppressed` 和 `interrupt-batch:<batch_id>:publish:1`，其 daily/critical key 也不同。 | 同一事实有两套表名、状态机、写端口、batch ID 与 operation key；崩溃重放、成员排除和 Ledger `batch_id` 无法选择唯一权威，直接违反前次复审门槛的交叉术语一致性。 | 选定一套 versioned schema 并同步 config/interrupt/storage/outbox/ledger；全文只保留一个 batch/member 名称、状态机、prepare 端口、stable ID grammar 和 operation key。补全文检索门禁，确保被淘汰术语为零。 |
| RMC4-B | MC4 | **daily identity 不能保证“每日一次摘要”。** storage §6.3 的 ID 为 `daily:<zone>:<quota_day>:<due_at_ms>`。例如前一日 09:00 后入批与次日 09:00 前入批会有不同 `quota_day`，却得到同一 `due_at_ms`，从而在同一时刻 sealed/publish 两批。 | PRD §5.5 的“每日一次摘要推送”和前次 MC4 的唯一汇总对象仍可被合法实现突破；delivery 次数取决于成员来自哪个 quota day。 | 将 daily batch 的唯一 identity 冻结为每个 zone/scheduled occurrence 至多一个对象（或给出等价唯一约束）；允许成员各自保留 `quota_day`，不要用 batch 级单一 quota day 拆分同一 due occurrence。补“前日摘要时刻后 + 次日摘要时刻前均超额，次日只发一个 batch”的 vector。 |
| RMC4-C | MC4 | **critical episode 的窗口恢复边界自相矛盾。** storage §9.3 计数条件是 `created_at_ms >= now-window`，但 §6.3 把 `due_at_ms` 定为最早 evidence 的 `created_at_ms+window`；到该毫秒时 equality 仍计数，尚未“首次少于 limit”。batch 的 `due_at_ms` 又不可改，未定义本次重裁决仍饱和时如何继续。 | episode 可能提前 sealed、永久停在 collecting，或由实现自行加 1ms；窗口左右边界和“每 episode 一次”不再确定。 | 统一为明确的半开窗口，或把 expiry/due 精确定义到毫秒并与查询谓词一致；规定 due 时仍饱和的持久推进结局。补 `window-1ms`、`window`、`window+1ms`、同毫秒并发 admission 及恢复后新 episode vectors。 |

## 3. 已确认关闭的部分

- **MC1 规则主体：** config §3.9 与 storage §9.1 已把 CAS contention、权威 quota exhausted 和不可恢复存储错误分开；只有重读证明超额才可合批。
- **MC2 入口主体：** `EmitInterrupt` 负责初发 critical，`AdvanceInterrupt` 负责升级首次 critical；滑动窗口权威已从 charge 移至 append-only admission evidence，global/per-Run 同时命中固定 global 优先。
- **MC3 规则主体：** `local` 在启动规范化为具体 IANA zone 并进入 snapshot/hash；`due_at` 严格晚于入批时刻；DST gap/fold 已有唯一文字裁决。
- **MC4 存储骨架：** storage/outbox/ledger 一侧已有 versioned batch、member、sealed payload、operation、delivery projection 与逐成员 Ledger 身份；关闭成员在 sealing 前排除，响应丢失重放 immutable payload。

这些关闭项不抵消 §2 的不可表示路径、双协议和边界缺口。

## 4. 复审门槛

- [ ] RMC1-A：并发额度与故障回滚 vectors 完整
- [ ] RMC2-A：quota-batched→critical 的 nullable charge/admission/delivery/metric 闭合
- [ ] RMC3-A：timezone、摘要时刻与 DST 的固定 epoch vectors 完整
- [ ] RMC4-A：batch schema、状态、端口、ID 与 operation key 全文唯一
- [ ] RMC4-B：同一 zone/due occurrence 至多一个 daily batch
- [ ] RMC4-C：critical 滑动窗口与 episode due 边界一致且可恢复
- [ ] `git diff --check`、Markdown 相对链接/围栏/尾随空白检查通过

## 5. 验收判断

- 遗留 P1：**6**
- 前次 4 项 P1：**部分关闭，未全部关闭**
- M5 增补字段定向复审：**FAIL**
- `config.md` M5 增补升 active：**NO**
- `config.md` 既有 M1–M4 active 基线：**不回退**
- 允许按当前跨文档契约实现 M5 配额/critical 熔断/汇总：**NO**
