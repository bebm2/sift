# brain.md 字段级评审

> 日期：2026-07-28
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/brain.md`](../specs/brain.md)（`draft`）
> 依据：[PRD §5.3、§5.7](../PRD.md)、[DESIGN §8.3](../DESIGN.md)、[`storage.md` §9–§12](../specs/storage.md)、[`config.md` §3.4](../specs/config.md)、[`control-plane.md` §7](../specs/control-plane.md)、[`outbox.md` §2–§5](../specs/outbox.md)、[WBS M1 §1.7](../WBS.md)

## 1. 总结论

**阻断（block）。** `brain.md` 已正确冻结 LLM 只给建议、同 prompt 重试、确定性兜底、Task Spec 来源以及真实 usage 记账方向，但当前不能转为 `active`：

1. storage 的单行 `brain_traces` 无法同时表达 logical call、两个物理 provider attempt 和调用前 fallback；
2. T1 的 `needs_clarification` / `possible_duplicate` 在 Run 创建前没有权威投影、原子回复或恢复协议；
3. T1/T2 目前是示例 JSON 加零散规则，还不足以生成文中承诺的 closed schema。

本结论不否定统一调用壳方向。P1 均可在下游 #2 通过修订 `brain.md` 及明确列出的交叉规格关闭；本 Issue 不应把 `brain.md` 标为 `active`。

## 2. §11 四项独立结论

| §11 项 | 结论 | 核心判断 |
|---|---|---|
| 1. logical call / provider attempt 表形态 | **阻断（block）** | 应拆 `brain_calls` / `brain_attempts`，另保留 mutable counter；继续扩单表会混合逻辑结果与物理尝试，且与 active storage 的唯一键冲突。 |
| 2. 单次 token 越界 | **有条件通过（conditional pass）** | 对只能事后返回 usage 的 CLI，这是 V0 可审计且不伪造用量的语义；但须把“每个物理 attempt 前重查阈值”、越界后的 retry 行为和原子记账例外写死。 |
| 3. T1 pre-Run 投影与恢复 | **阻断（block）** | 必须有独立 intake 投影；不能借用要求 `run_id` 的 Interrupt，也不能只靠 event/trace 推导当前待办。 |
| 4. `claude-json-v1` 外层 envelope | **有条件通过（conditional pass）** | 顶层及 usage 容器可 open-envelope 以容忍 CLI 新增诊断字段；必需字段、类型、重复键和内层触点输出仍必须严格。 |

## 3. P1：转 active 前必须关闭

### B1（P1）：调用身份与 attempt 持久化模型不成立

`brain.md` §3 要求唯一键 `(scope, subject_key, touchpoint, call_seq, provider_attempt)`，而 active `storage.md` §10.1 仍以 `(scope, subject_key, touchpoint, call_seq)` 唯一，并把一次调用的输入、输出、fallback 与 usage 放在同一 `brain_traces` 行。一次 invalid → valid 已需要两行，当前 storage 会拒绝第二行；若覆盖第一行，又违反 append-only 并丢失第一次成本与原始输出。

**裁决：拆表，不继续扩单表。** 推荐字段边界：

- `brain_call_counters`（mutable）：`scope, subject_key, touchpoint, next_call_seq, version, updated_at_ms`；主键前三项；`ReserveBrainCall` 在 `BEGIN IMMEDIATE` 中递增并以旧值插入已冻结输入的 call 行，禁止 `max()+1`。
- `brain_calls`（single-finalize logical call）：`id/logical_call_id, scope, subject_key, project_id, run_id, attempt_no, touchpoint, call_seq, prompt_version, output_schema_version, input_json, input_digest, status, selected_attempt_no, fallback_reason, validated_output_json, started_at_ms, finished_at_ms`；reserve 时插入 `status=running`，仅允许一次 `running → valid | fallback` 补全结果，身份/输入字段永不可改。
- `brain_attempts`（immutable physical/preflight result）：`id, logical_call_id, provider_attempt, outcome, provider_error_code, raw_output_text, raw_output_digest, raw_output_bytes, stderr_summary, exit_code, input_tokens, output_tokens, started_at_ms, finished_at_ms`。

约束至少包括：

- `UNIQUE(scope, subject_key, touchpoint, call_seq)` 在 call 表；`UNIQUE(logical_call_id, provider_attempt)` 在 attempt 表；call 禁止 DELETE，并以 trigger 只开放一次终结更新；
- `provider_attempt=0` 只能是 `outcome=fallback` 且 token/exit/raw output 为空；`1..2` 才表示已启动 provider；
- call 的 `status=running | valid | fallback`，后两者为终态；valid 必须指向本 call 的 valid attempt，fallback 必须有原因且不能伪造 selected attempt；
- prompt/input/schema 只存 call 一次，attempt 通过 FK 继承“同 prompt”事实；若要证明实际发送 bytes，再给 attempt 存 `request_digest` 并 CHECK 等于 call 冻结值；
- attempt 1/2 分别落库后再决定后续动作；最终 call 收敛与最后 attempt/调用前 fallback 必须由显式存储端口完成，只能一次性终结 call，不能更新 immutable attempt；daemon 重启遇到遗留 `running` call 时按已有 attempts 收敛为 fallback，不重放无法证明未执行的 provider attempt；
- replay JSONL 一条 logical record 内携有序 attempts，或定义 call/attempt 两种 record；不能继续把一次 retry 导出成两个彼此无归属的“Brain record”。

`fallback_used` 放在每个 attempt 上会混淆“该 attempt 失败”与“整个 logical call 最终 fallback”，应从 attempt 表删除。`attempt_outcome=valid | invalid_output | provider_error | fallback` 也需配套稳定的 `provider_error_code` 枚举，至少覆盖 `timeout | nonzero_exit | output_too_large | invalid_envelope | usage_missing | usage_invalid | spawn_failed`；否则 §4/§10 fixture 无法按字段断言。

**处置落点：** 下游修订 Issue #2 同步修改 `brain.md` §3/§5/§10、`storage.md` §10.1/§10.8/§11/§13/§15/§16 和 WBS M1 §1.7 的 trace 验收引用。

### B2（P1）：T1 在 Run 创建前会形成不可恢复的悬空状态

`needs_clarification` / `possible_duplicate` 明确不创建 Run；现有 `runs` 只覆盖 queued 之后，`interrupts` 绑定 Run，`forge_event_receipts` 只是去重收据，`brain_traces` 又是审计记录而非当前投影。daemon 在“写 trace、发评论、推进 cursor”任一窗口崩溃后，没有权威字段回答该 Issue 正在等什么、哪组问题已发、哪个回复可恢复处理。

应新增 pre-Run intake 模型，最小闭包如下：

- `intake_items`（mutable）：`id, project_id, forge_kind, normalized_host, forge_project_key, issue_id, issue_url, issue_digest, state, version, latest_assessment_id, linked_run_id, duplicate_candidate_run_id, clarification_generation, created_at_ms, updated_at_ms`；
- 唯一键 `(forge_kind, normalized_host, forge_project_key, issue_id)`；
- `state = pending_evaluation | evaluating | awaiting_clarification | awaiting_duplicate_confirmation | ready | consumed`；`consumed` 必须有 `linked_run_id`，两个 awaiting 状态必须有对应 assessment；遗留 `evaluating` 通过 Brain call 的持久状态恢复，不靠内存超时猜测；
- `intake_assessments`（immutable）：`id, intake_id, logical_call_id, disposition, questions_json, possible_duplicate_run_id, rationale, created_at_ms`；字段约束与 T1 output 同源；
- clarification/confirmation 评论使用 `forge_comment`，但 `outbox.md` 当前 `purpose` 仅允许 `interrupt | summary`，应增加 intake purpose 和稳定 key，例如 `comment:intake-clarification:<intake_id>:<generation>`；payload 带 `intake_id/generation`，不伪造 `run_id`；
- `PersistIntakeBatch` 先在一笔事务中写 receipt、`pending_evaluation` 投影与 event，再推进 forge cursor；T1 worker 另行 reserve call 并 CAS 到 `evaluating`，外部 provider 调用不占数据库事务；
- `PersistIntakeDecision` 必须在一笔事务中写 assessment、CAS intake state、event 与必要 outbox operation；ready/fallback 则同事务幂等创建 Run 并写 `linked_run_id`；
- 回复由可信 actor 的 `issue_comments` receipt 驱动，按 `intake_id + generation` 关联；旧 generation 回复只记审计、不推进当前状态；重启靠 intake state + pending outbox 恢复，不扫描自然语言猜状态；
- duplicate 只能经确定性 Issue identity / Run 事实确认。确认重复后应有显式终态或 `consumed` 结果并链接既有 Run；否决重复后进入 ready，而不是静默丢弃。

T1 的“待澄清问题”不是 PRD §4.3 已定义的 Run Interrupt，除非 PRD/DESIGN 另行扩展 Interrupt 为可空 Run；V0 更小的改动是保持独立 intake 投影与 forge comment 协议。

**处置落点：** #2 同步修改 `brain.md` §7、`storage.md` §7/§11/§12.3/§15/§16、`outbox.md` §2/§5，并在 WBS M1 §1.7 增加 crash/replay 验收。若产品决定把该交互移出 M1，也必须把 T1 的非-ready 行为改成确定性直接入队；不能保留未实现的悬空分支。

### B3（P1）：所谓 closed T1/T2 schema 尚未达到字段级可生成

§7/§8 的 JSON 目前未冻结完整 `required`、`additionalProperties:false`、字符串/数组上限及若干枚举：

- T1 input：`forge.kind` 未列 `github | gitlab`；`labels[]` 元素结构、`known_candidates.status` 枚举及候选字段 required 未定义；Issue 各字符串和候选数量无界。
- T1 output：questions 只有条数/非空规则，缺每项长度、去重规则；`rationale` 无上限；各 disposition 与 duplicate/questions 的互斥矩阵不完整（例如 `possible_duplicate` 是否必须非空 id）。
- T2 input：`capabilities[]`、`task_annotations[]` 元素 schema 未定义；Issue、Context、候选数量均无大小边界。
- T2 output：goals 未冻结 trim 后判空/去重、单项与总字节上限；`risk_notes/rationale` 无上限。

这些缺口会让 prompt schema、runtime schema 与 replay schema各自选择不同约束，直接违反 §1.2。应在正文逐字段表或引用仓库内 canonical schema；领域后校验只承接“候选必须存在、确定性身份确认”等依赖运行时事实的规则，不应替 schema 补类型和长度。

输入也必须有总 canonical JSON byte 上限及超限的确定性结果；否则 stdin 可被超大 Issue/Context 无界放大。该上限是 Brain 输入契约，不应误用 `max_raw_output_bytes`。

**处置落点：** #2 修改 `brain.md` §7/§8/§10；如上限需配置，交叉修改 `config.md` §3.4。prompt 的 `.schema.json` 在实现期必须由同一 schema 定义生成，不得手写第二份。

## 4. P2：方向可接受，但修订时必须写死

### B4（P2）：token 越界语义须按 physical attempt 定义

允许一个已发起调用在 usage 返回后使 counter 越界是合理的：CLI 不提供可信的事前 token 成本，预估收费会破坏“实际 usage 全额审计”。但当前文本同时写“invalid 必重试一次”和“越界后所有后续触点兜底”，未说明 attempt 1 越界后是否仍可发 attempt 2。

条件如下：

1. **每个物理 provider attempt 前**串行检查当日 counter；若 `consumed >= limit`，不再启动下一 attempt。attempt 1 已失败且使 counter 越界时，logical call 直接 fallback，不发 attempt 2。
2. threshold check 只决定能否发起；attempt 返回后，`input_tokens + output_tokens` 与 attempt trace、budget entry、counter 在同一事务写入，即使新值大于 limit。重复 operation key 返回原 charge。
3. storage §9.1 的通用 `consumed + amount <= limit` CAS 必须显式排除 token post-charge；token 使用“发起前 threshold check + 完成后允许一次越界”的专用语句。单 daemon/单 writer 不等于协议，需由事务和唯一 operation key 保证。
4. `usage` 已知且总和为 0 时保存 trace 但不创建要求 `amount>0` 的 budget entry；usage 缺失则不猜测、不收费，按 provider error 处理。
5. 冻结“每日”的 UTC bucket 边界；跨午夜以 attempt **开始时**冻结的 bucket 收费，避免调用结束时换桶绕过阈值。
6. 越界告警采用稳定 generation/key，只发一次；它仍走正常 attention budget，不得因 token 告警突破注意力配额。

**处置落点：** `brain.md` §5/§6、`storage.md` §9/§11/§16；告警 payload/key 若复用 `forge_alert`，同步 `outbox.md`。

### B5（P2）：外层可 open，但“open”不等于宽松解析

建议 `claude-json-v1` 采用：

- CLI 顶层 object 接受未知诊断字段；`result_text` 与 `usage` 仍为 required 且类型精确；
- `usage` object 可接受未知计数项，但 `input_tokens/output_tokens` required、非负整数且不做数值字符串强转；
- JSON parser 拒绝重复键、非 UTF-8、非有限数字、尾随文本及非 object 顶层；未知字段只忽略，不进入 prompt、领域输出或 token 计算；
- `result_text` 内层仍必须是恰一个 object，并按触点 schema `additionalProperties:false`；
- 协议重大语义改变以新的 `protocol` 值适配，不能靠 open-envelope 猜兼容。

这能容忍 CLI 新增 `session_id/cost/diagnostics`，同时不放宽触点输出。`config.md` §3.4 尚无 `protocol` 字段，需按 `brain.md` §4 增加并限定 V0 为 `claude-json-v1`。

**处置落点：** `brain.md` §4 与 `config.md` §3.4。

### B6（P2）：provider 证据字段与超限输出行为未闭合

§4 要记录 bounded stderr 摘要、exit、timeout、oversize 原因，storage 当前没有对应字段；§5 又要求“原始 stdout 完整保存（受上限）”。stdout 一旦超过上限，不可能既完整保存又受限。

建议冻结：达到 `max_raw_output_bytes + 1` 即终止；保存前 `max_raw_output_bytes` bytes、完整已读部分的 digest/byte count、`raw_output_truncated=true`，结果为 `provider_error/output_too_large`。stderr 另设固定 byte 上限与 `stderr_truncated`，先做凭据模式去除；不得拼入重试 prompt。若安全策略不允许保存截断 raw bytes，则 `raw_output_text=null`，但 digest/count/error code 仍必填，二选一必须明确。

**处置落点：** `brain.md` §4/§5 与 B1 的 `storage.md` attempt 字段。

### B7（P2）：T2 的审批消费规则少一段状态事务

T2 valid 且 `hitl_before_start=true` 时，PRD §6 要先走 `design_approval`；正文只详细写了 T2 failure 的人工分派。应明确 valid 结果先原子写 Run kind/agent、初始 Task Spec、`waiting_human`、Interrupt/event/outbox，批准后才 queued/launch；来自非 allowlist Issue 作者的确定性强制审批必须对 LLM 的 `false` 取 OR，不能被 T2 降级。

**处置落点：** `brain.md` §8/§9，引用 storage 的 `SetInitialTaskSpec`/`EmitInterrupt` 或新增能保证单事务的复合端口；不得先把 Run 暴露为可 launch 的 queued 再补 Interrupt。

## 5. P3：编辑与验收改进

- prompt version 写成 `<touchpoint>/v<integer>/<hash>`，但 schema version 又单独为 integer；建议明确 `prompt_asset_version`、`output_schema_version` 和 `protocol_version` 三者的独立 bump 规则，避免把 hash 字符串塞进 storage integer。
- `project/global` Context 的“规定空内容 hash”应引用统一 canonical hash 规则；当前 `config.md` 使用 `$SIFT_HOME/context.md`，PRD 示例使用 `~/.sift/context.md`，正文应只引用 `config.md` 的 `SIFT_HOME` 解析，不复制路径事实。
- M1 fixture 增加：attempt 1 用量使 token 越界后禁止 attempt 2、跨 UTC 日界、零 token usage、outer unknown field 接受、duplicate key 拒绝、intake 评论远端成功本地提交前崩溃、旧 generation 回复拒绝推进。

## 6. 已通过项

以下方向可保留：

- LLM 输出只作建议，未知/非候选 Agent、硬护栏、预算和状态转移均由确定性代码裁定；
- schema failure 不修 JSON、不抽 fence、不加修复提示，允许重试时保持同 prompt/input；
- provider 禁用/预算拒绝仍产生可审计 logical call，而不是静默跳过；
- T1 fallback 直接入队，T2 fallback 不擅自选择第一个 Agent；
- Task Spec 由 Description → Goals → Guardrails → Context 确定性组装，policy/context 从 base/global/task 冻结，attempt 引用不可变 snapshot；
- token、Forge API 与注意力预算方向分离，任何 fallback 不突破注意力配额；
- 真实 CLI 只用 fixture 子进程验收，M1 fake 链不依赖线上模型。

## 7. 建议的关闭顺序

1. 先在 #2 冻结 `brain_calls` / `brain_attempts` 与 token 事务语义；
2. 再冻结 intake 投影、outbox purpose/key 和回复 generation；
3. 补齐 T1/T2 canonical schema、输入上限及 T2 approval 事务；
4. 最后按上述字段重写 replay/fixture 验收并做交叉规格一致性检查。

完成后可进行定向复评；在 B1–B3 关闭前，`brain.md` 应继续保持 `draft`。
