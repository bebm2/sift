---
status: active
created: 2026-07-28
summary: Brain 调用壳、T1/T2 schema 与确定性兜底契约
---

# Brain 规格

本文冻结 Brain 统一调用壳、调用身份、提示词资产、T1/T2 输入输出、Task Spec 组装、token 记账与确定性兜底。T3–T7 沿用同一壳，随对应里程碑增补 schema。

来源：[PRD §5.3、§5.7](../PRD.md)、[DESIGN §8.3](../DESIGN.md)、[`storage.md` §9–§10](storage.md)、[`config.md` §3.4](config.md)、[`outbox.md` §2、§5](outbox.md)、[WBS M1 §1.7](../WBS.md)。

## 评审处置

评审原文：[2026-07-28-brain-review-pi-gpt-5.6-sol.md](../reviews/2026-07-28-brain-review-pi-gpt-5.6-sol.md)。

| 发现 | 处置 |
|------|------|
| B1（P1）单行 trace 无法表达 logical call / 双 attempt | 拆 `brain_call_counters`/`brain_calls`/`brain_attempts`，见 §3/§5；表、约束与 trigger 落 [`storage.md` §10.1/§13](storage.md) |
| B2（P1）T1 pre-Run 悬空状态 | 新增 intake 投影、状态机与回复 generation 协议，见 §7.3；表落 storage §7.5–§7.6，outbox 目的/key 落 [`outbox.md` §5.1](outbox.md) |
| B3（P1）T1/T2 schema 未到字段级可生成 | §7.1/§7.2/§8.1/§8.2 逐字段冻结；输入总上限 `max_input_bytes` 落 [`config.md` §3.4](config.md) |
| B4（P2）token 越界语义 | §6 按物理 attempt 定义发起前阈值 + 事后越界 post-charge；storage §9.1 显式排除 token |
| B5（P2）外层 envelope 边界 | §4 顶层/usage open-envelope、内层触点输出 closed；`protocol` 字段落 config §3.4 |
| B6（P2）超限输出与 provider 证据 | §4/§5 冻结截断语义、digest/bytes、stderr 上限与 `provider_error_code` 枚举 |
| B7（P2）T2 审批消费缺状态事务 | §8.3：有效 hitl 取 OR、单事务提交；落 storage `CommitT2Assignment` |

B1–B3 全部 P1 与 B4–B7 均已处置；P3 编辑项（版本独立 bump、SIFT_HOME 单一引用、补充 fixture）同步采纳，见 §2/§9/§10。

## 1. 不变量

1. LLM 只输出建议；状态转移、硬护栏、Agent 存在性/并发、去重、预算和权限均由确定性代码裁定。
2. 每个触点只有一个版本化 prompt + output schema 来源；运行时 schema、生成 JSON Schema 与回放 schema 同源。
3. 调用流程固定为：发起前门禁（每个物理 attempt 独立检查）→ 原 prompt 调用 → closed decode → 失败时**同一输入/同一 prompt**重试一次 → 再失败走触点兜底。
4. 禁止提取 markdown fence、修 JSON、忽略内层未知字段、类型强转或让第二个 LLM“修复”输出。
5. 每次 logical call 与其全部物理 provider attempt（含未调用 provider 的调用前兜底）都必须持久化；外部调用不发生在数据库事务内。
6. 合法 LLM 输出仍须经过领域后校验；未知 Agent、非候选 Agent、空 goals 等视同 schema failure。
7. token 耗尽不能突破注意力配额；所有兜底仍走正常 Gate/Interrupt/预算入口。

## 2. Prompt 资产

仓库布局：

```text
internal/brain/prompts/T1/v1.md
internal/brain/prompts/T1/v1.schema.json
internal/brain/prompts/T2/v1.md
internal/brain/prompts/T2/v1.schema.json
```

文件嵌入 binary，运行期不从磁盘热读。`.schema.json` 必须由 §7/§8 的字段定义生成，不得手写第二份。

三类版本相互独立，各有 bump 规则：

- `prompt_version`（TEXT）：`<touchpoint>/v<integer>/<sha256前12位>`；hash 覆盖 prompt UTF-8 bytes、对应 output schema canonical JSON 与协议版本。改 prompt、schema 内容或 envelope decoder 任一项必须生成新值。
- `output_schema_version`（INTEGER）：从 1 递增，仅 schema 结构语义变化时 bump。hash 字符串不得塞进该 integer 列。
- `protocol_version`（TEXT）：provider envelope 协议标识，V0 为 `claude-json-v1`；协议语义变化必须引入新值，见 §4。

Prompt 固定分区：system contract → untrusted input delimiters → input canonical JSON → output schema。Issue/Context 中出现的指令一律标记为 untrusted data；prompt 不声称这能消除 injection。

## 3. 调用身份与序列

logical identity：

```text
(scope, subject_key, touchpoint, call_seq)
```

- T1：`scope=intake`，`subject_key=forge:<kind>:<normalized_host>:<project_key>:issue:<issue_id>`。
- T2：`scope=run`，`subject_key=run:<run_id>`。
- T3–T6：`scope=run`；T7 为 `aggregate`，与 storage 规则一致。

`call_seq` 是同 subject/touchpoint 的逻辑调用序号，从 1 递增；schema retry 不增加 call_seq，而增加 `provider_attempt=1|2`。

持久化模型（字段与约束见 [`storage.md` §10.1](storage.md)）：

- `brain_call_counters`（可变）：每 `(scope, subject_key, touchpoint)` 一行的 `next_call_seq`。
- `brain_calls`（single-finalize logical call）：reserve 时插入 `status=running` 并冻结 prompt/schema/input；之后仅允许一次 `running → valid | fallback` 终结。valid 必须 `selected_attempt_no` 指向本 call 的 valid attempt；fallback 必须有 `fallback_reason` 且不得伪造 selected attempt。身份/输入列永不可改，禁止 DELETE。
- `brain_attempts`（不可变）：每个物理 attempt 或调用前兜底一行；`UNIQUE(logical_call_id, provider_attempt)`。`provider_attempt=0` 只表示 provider disabled/预算门禁等调用前兜底（`outcome=fallback`，token/exit/raw 全空）；`1|2` 表示真实子进程调用。attempt 上**没有** `fallback_used`：单个 attempt 失败不等于整个 call 走兜底。

`ReserveBrainCall` 在 `BEGIN IMMEDIATE` 中递增 counter 并以旧值插入 running call，禁止 `SELECT max()+1`。

`attempt_outcome = valid | invalid_output | provider_error | fallback`；`provider_error` 必须带稳定 `provider_error_code`：`timeout | nonzero_exit | output_too_large | invalid_envelope | usage_missing | usage_invalid | spawn_failed`。

prompt/input/schema 只存 call 一次，attempt 通过 FK 继承“同 prompt”事实；attempt 另存 `request_digest`，写端口断言其等于 call 冻结的 `input_digest`，以证明实际发送 bytes 未漂移。

## 4. Provider 子进程协议

V0 protocol 为 `claude-json-v1`，config 字段见 [`config.md` §3.4](config.md)（`protocol` V0 只能为该值）。

调用使用配置 executable + args，prompt/input 从 stdin 传入，不使用 shell，不把输入放 argv。子进程工作目录为空临时目录，环境只保留运行 CLI 所需的最小 allowlist；不得注入 operator/run/wrapper credential。timeout 使用 config `call_timeout`。

### 4.1 `claude-json-v1` 外层 envelope：open-envelope

adapter 将 CLI 外层结果规范化为 `result_text` + `usage`：

- 顶层 object **接受未知诊断字段**（如 CLI 新增的 `session_id/cost/diagnostics`），但 `result_text` 与 `usage` 仍 required 且类型精确。
- `usage` object 接受未知计数项，但 `input_tokens/output_tokens` required、非负整数，不做数值字符串强转。
- JSON parser 拒绝重复键、非 UTF-8、非有限数字、尾随文本及非 object 顶层。未知字段只忽略，不进入 prompt、领域输出或 token 计算。
- `result_text` 必须是仅含一个 JSON object 的 UTF-8 字符串，内层按触点 schema `additionalProperties:false` closed decode——open 只到外层为止。
- 协议重大语义改变以新的 `protocol` 值适配，不靠 open-envelope 猜兼容。

usage 缺失/非法使本 attempt 无法计费：记 `provider_error`（`usage_missing | usage_invalid`），不猜测、不收费、不当 0，触发重试/兜底。

### 4.2 输出上限与 stderr

- stdout 读到 `max_raw_output_bytes + 1` 即终止进程；该 attempt 记 `provider_error/output_too_large`，保存前 `max_raw_output_bytes` bytes、已读部分完整 digest 与 byte count、`raw_output_truncated=true`。“完整保存原始 stdout”只对未截断输出成立。
- stderr 另设固定上限（V0 为 4096 bytes）：先做凭据模式去除，保存 `stderr_summary` 与 `stderr_truncated`；不拼入重试 prompt，不入事件或 outbox。

## 5. 重试与 trace

每次 logical call：

1. `ReserveBrainCall`：counter 递增 + 插入 `status=running` call，同一事务。
2. 每个物理 attempt 发起前串行检查 provider 可用性与当日 token counter；disabled 或 `consumed >= limit`：`RecordBrainAttempt` 写 `provider_attempt=0, outcome=fallback` 行，`FinalizeBrainCall` 终结为 fallback，返回触点兜底。
3. 调 provider attempt 1；子进程结束后 `RecordBrainAttempt` 落 immutable attempt 行，同事务按实际 usage post-charge（见 §6）。
4. outcome=valid：`FinalizeBrainCall` → valid（`selected_attempt_no=1`），返回。
5. 否则在 attempt 2 发起前**重新**执行第 2 步门禁；通过则以**完全相同 prompt bytes/input digest** 调 attempt 2 并落库。
6. attempt 2 valid → finalize valid（`selected_attempt_no=2`）；否则 finalize fallback，返回确定性兜底。

重试不得加入“上次哪里错了”的修复提示。attempt 1/2 分别落库后再决定后续动作；最终 call 收敛只能由 `FinalizeBrainCall` 一次性完成，不得更新 immutable attempt。

崩溃恢复：daemon 重启遇到遗留 `status=running` call，只按已持久 attempts 收敛——已有 valid attempt 则终结 valid，否则终结 fallback（`fallback_reason` 含 recovery）；不得重放无法证明未执行的 provider attempt。

## 6. Token 预算

`daily_token_limit` 是**发起新物理 attempt 的实际消费阈值**，语义如下：

1. Brain 调用壳全局串行。每个物理 attempt 发起前检查当日 counter：`consumed >= limit` 即不再发起，logical call 走兜底。attempt 1 已失败且使 counter 越界时直接 fallback，**不发 attempt 2**。
2. 阈值检查只决定能否发起；attempt 返回后按实际 `input_tokens + output_tokens` post-charge：attempt trace、`budget_entries` 与 counter 在同一事务写入，即使新值大于 limit。收费 operation key 为 `brain:<logical_call_id>:provider:<provider_attempt>`；重复 key 返回原 charge，不重复计费。重试是真实第二次成本，单独收费。
3. [`storage.md` §9.1](storage.md) 的通用 `consumed + amount <= limit` CAS **不适用于** token post-charge；token 使用专用“发起前阈值检查 + 完成后允许一次越界”语句。单 daemon/单 writer 不等于协议，由事务与唯一 operation key 保证。
4. usage 已知且总和为 0：保存 trace，但不创建要求 `amount>0` 的 budget entry；usage 缺失/非法不猜测、不收费，按 §4.1 的 provider error 处理。
5. 日桶为 UTC 自然日；跨午夜以 attempt **开始时**冻结的 bucket 收费，不以结束时间换桶。
6. 越界告警走 `forge_alert`，稳定 key（见 [`outbox.md` §5.1](outbox.md)），每 UTC 日桶只发一次；它仍消耗正常 attention budget，不得因 token 告警突破注意力配额。

token counter 是固定预算允许单次越界的唯一例外：实际 usage 必须全额 append entry，不得丢弃；attention/Forge/Report 仍禁止借支。

## 7. T1 Intake 体检

canonical schema 的唯一来源是与 prompt 同目录的 `.schema.json`（由本节字段表生成）；领域后校验只承接依赖运行时事实的规则（候选必须存在、确定性身份确认），不替 schema 补类型和长度。

### 7.1 Input v1

顶层 closed object（`additionalProperties:false`），三个 required 字段：

`forge`（closed object，required）：

| 字段 | 类型 | 约束 |
|------|------|------|
| `kind` | string | 必填；`github \| gitlab` |
| `host` | string | 必填；规范化 host，1..253 bytes |
| `project_key` | string | 必填；1..255 bytes |

`issue`（closed object，required）：

| 字段 | 类型 | 约束 |
|------|------|------|
| `id` | string | 必填；1..64 bytes |
| `title` | string | 必填；≤512 bytes |
| `body` | string | 必填；≤65536 bytes，可空串 |
| `author` | string | 必填；≤128 bytes |
| `url` | string | 必填；≤1024 bytes |
| `labels` | string[] | 必填；≤32 项，每项 ≤128 bytes，由 intake 排序去重 |

`known_candidates`（array，required，≤20 项，按 `run_id` 排序），每项为 closed object：

| 字段 | 类型 | 约束 |
|------|------|------|
| `run_id` | string | 必填 |
| `issue_id` | string | 必填；1..64 bytes |
| `title` | string | 必填；≤512 bytes |
| `status` | string | 必填；`queued \| running \| waiting_human \| done \| failed` |

整份 input canonical JSON ≤ config `brain.max_input_bytes`；超限不调用 provider，直接按 T1 兜底 ready 入队（确定性结果，不伪造 LLM 输出）。该上限是输入契约，不复用 `max_raw_output_bytes`。

候选由确定性检索产生；不得让 LLM 查询数据库/Forge。

### 7.2 Output v1

closed object（`additionalProperties:false`）：

| 字段 | 类型 | 约束 |
|------|------|------|
| `disposition` | string | 必填；`ready \| needs_clarification \| possible_duplicate` |
| `questions` | string[] | 必填；0..5 项，每项 trim 后 1..1000 bytes，trim 后去重 |
| `possible_duplicate_run_id` | string/null | 必填；非空时长度 ≤64 bytes |
| `rationale` | string | 必填；≤2000 bytes，可空串 |

disposition 互斥矩阵（schema 层表达，领域后校验复核）：

- `ready`：questions 为空且 duplicate 为 null；
- `needs_clarification`：questions 1..5 且 duplicate 为 null；
- `possible_duplicate`：duplicate 非空且 questions 为空。

领域后校验：duplicate id 必须精确命中 input candidate 的 `run_id`，否则视同 schema failure。T1 只是建议：duplicate 必须由确定性 Issue identity/既有 Run 事实确认，LLM 不能单独吞掉任务。

失败兜底固定为：`disposition=ready, questions=[], possible_duplicate_run_id=null, rationale="fallback"`。

### 7.3 pre-Run intake 投影与消费协议

本节契约在 M1 冻结；投影/CAS、真实 Forge comment worker、回复 receipt 消费以及 crash/generation 验收在 [WBS M2 §2.3/§2.5](../WBS.md) 实现。它们必须随真实 Forge 适配层交付，不能用 M1 的 schema 或通用 outbox 框架代替实现证据。

`needs_clarification`/`possible_duplicate` 不创建 Run，也不能只靠 event/trace 推导当前待办。权威投影为 [`storage.md` §7.5–§7.6](storage.md) 的 `intake_items`（可变）与 `intake_assessments`（不可变），唯一键 `(forge_kind, normalized_host, forge_project_key, issue_id)`。

状态机：

```text
pending_evaluation → evaluating → ready
                                → awaiting_clarification
                                → awaiting_duplicate_confirmation
awaiting_* --(可信回复)--> pending_evaluation | ready | consumed
ready --(Run 创建)--> consumed
```

协议：

1. `PersistIntakeBatch` 一笔事务写 receipt、`pending_evaluation` intake 投影与事件，最后推进 forge cursor；崩溃不推进 cursor，重放靠 receipt 唯一键去重。
2. T1 worker 先 `ReserveBrainCall` 并把 intake CAS 到 `evaluating`；外部 provider 调用不占数据库事务。遗留 `evaluating` 按 §5 的 running call 收敛规则恢复，不靠内存超时猜测。
3. `PersistIntakeDecision` 一笔事务：写 assessment、CAS intake state、追加事件、创建必要 outbox operation；ready（含兜底）同事务幂等创建 Run、写 `linked_run_id` 并转 `consumed`。
4. 澄清/确认评论使用 `forge_comment`，purpose 与稳定 key 见 [`outbox.md` §5.1](outbox.md)（如 `comment:intake-clarification:<intake_id>:<generation>`）；payload 带 `intake_id/generation`，outbox 行 `run_id` 保持 NULL，不伪造 run 关联。每一轮新澄清 `clarification_generation` +1。
5. 回复由可信 actor 的 `issue_comments` receipt 驱动，按 `intake_id + 当前 generation` 关联：接受则 CAS 回 `pending_evaluation` 重新评估；**旧 generation 回复只记审计事件，不推进当前状态**。重启靠 intake state + pending outbox 恢复，不扫描自然语言猜状态。
6. duplicate 只能经确定性事实确认：candidate Run 的 Issue identity 与本 Issue 相同 → 直接 `consumed` 并链接既有 Run；否则发确认评论，可信回复确认 → `consumed` + `linked_run_id`=candidate；否决 → `ready`，按正常路径创建 Run。任何路径不得静默丢弃 Issue。

T1 的待澄清问题不是 PRD §4.3 的 Run Interrupt：V0 保持独立 intake 投影与 forge comment 协议，不把 Interrupt 扩展为可空 Run。

## 8. T2 分派

### 8.1 Input v1

顶层 closed object，required 字段：

| 字段 | 类型 | 约束 |
|------|------|------|
| `run_id` | string | 必填 |
| `issue` | closed object | 必填；`title` ≤512 bytes、`body` ≤65536 bytes、`url` ≤1024 bytes，三者均必填 |
| `candidate_agents` | array | 必填；1..32 项，按 `id` 排序 |
| `base_context` | closed object | 必填 |

`candidate_agents[]`（closed object）：

| 字段 | 类型 | 约束 |
|------|------|------|
| `id` | string | 必填；≤64 bytes |
| `capabilities` | string[] | 必填；≤16 项，每项 ≤64 bytes |

`base_context`（closed object）：

| 字段 | 类型 | 约束 |
|------|------|------|
| `project_context` | string | 必填；≤65536 bytes，可空串 |
| `global_context` | string | 必填；≤65536 bytes，可空串 |
| `task_annotations` | array | 必填；≤50 项，每项 closed object：`event_id` string 必填、`text` string ≤2000 bytes 必填 |

整份 input canonical JSON ≤ config `brain.max_input_bytes`；超限不调用 provider，直接走 T2 确定性兜底（人工分派）。

candidate agents 已经过配置、项目引用和启动探测过滤。Context 内容是 untrusted data。

### 8.2 Output v1

closed object（`additionalProperties:false`）：

| 字段 | 类型 | 约束 |
|------|------|------|
| `kind` | string | 必填；`feature \| bug \| chore \| docs \| refactor` |
| `agent` | string | 必填；≤64 bytes；领域后校验必须精确命中 candidate id |
| `hitl_before_start` | boolean | 必填 |
| `goals` | string[] | 必填；1..10 项，每项 trim 后 1..1000 bytes，trim 后去重，合计 ≤8000 bytes |
| `risk_notes` | string | 必填；≤2000 bytes，可空串 |
| `rationale` | string | 必填；≤2000 bytes，可空串 |

LLM 不输出 guardrails、max attempts、并发或 policy；出现额外字段即被 closed decode 拒绝。

失败兜底：Run 保持/进入 `waiting_human`，agent/kind 为空，生成 `design_approval` 人工分派 Interrupt；不自动挑“第一个 Agent”。

### 8.3 审批消费事务

有效 `hitl_before_start = LLM 建议 OR 确定性强制`：来自非 allowlist Issue 作者的 Run 由确定性规则强制 true，LLM 输出的 `false` 不得降级该强制。

- 有效值为 false：`SetInitialTaskSpec` 一笔事务写 Run kind/agent、初始 Task Spec snapshot、Run → `queued` 与事件。
- 有效值为 true：一笔事务（storage `CommitT2Assignment`）写 Run kind/agent、初始 Task Spec snapshot、Run → `waiting_human`、`design_approval` Interrupt、事件与 outbox；批准指令到达后才 queued/launch。**不得先把 Run 暴露为可 launch 的 queued 再补 Interrupt。**

## 9. Task Spec v1

T2 valid 后由确定性 assembler 生成：

```json
{
  "schema_version":1,
  "description":{"title":"...","body":"...","source_url":"..."},
  "goals":["..."],
  "guardrails":{"policy_hash":"...","rules":[]},
  "context":{
    "project":{"blob_hash":"...","text":"..."},
    "global":{"content_hash":"...","text":"..."},
    "task_annotations":[{"event_id":"...","text":"..."}]
  },
  "assignment":{"kind":"feature","agent":"claude-code","hitl_before_start":false},
  "brain":{"logical_call_id":"...","prompt_version":"..."}
}
```

组装顺序固定为 Description → Goals → Guardrails → project/global/task Context。project/global context 的来源路径、权限与缺省语义以 [`config.md`](config.md) 为唯一事实来源，本文不复制路径事实；缺失时为空内容，hash 为对规定空内容的 SHA-256（canonical 与 hash 规则见 [`config.md` §4](config.md)）。Guardrails 只来自有效 policy/硬编码默认，不接受 T2 改写。

整份 canonical JSON 与 digest 写 immutable snapshot；初始版本为 1。`/sift ask` 创建下一版本并保留旧 snapshot，已启动 attempt 继续引用旧版本。

## 10. 分阶段验收

### 10.1 M1：调用壳、T1/T2 与 Brain replay

1. fixture 覆盖 valid first、invalid→valid、invalid→fallback、timeout、nonzero exit、oversize、usage missing、usage invalid、spawn failed。
2. 两次 provider attempt 的 prompt bytes/request digest 完全相同；attempt identity 只差 provider_attempt；call 只经一次终结。
3. 内层触点输出的 unknown field/type/enum/fenced JSON/尾随文本均拒绝，不尽力解析；外层 envelope 未知诊断字段接受、重复键拒绝。
4. provider disabled 与 token threshold 写 `provider_attempt=0` attempt 行并走触点兜底。
5. token：attempt 1 用量越界后禁止 attempt 2；越界 post-charge 全额入账；跨 UTC 日界按 attempt 开始冻结的桶收费；零 token usage 不写 budget entry；重复 operation key 不重复收费。
6. T1：fallback 直接入队且不静默丢 Issue；LLM duplicate 建议不能绕过确定性确认。
7. T2 unknown/noncandidate Agent 触发同 prompt retry，最终进入人工分派；硬护栏不从 LLM 输出读取；hitl 强制规则不被 LLM `false` 降级；批准前 Run 不可 launch。
8. Task Spec 的四段来源/hash 可重建；worktree 中 context/policy 修改不进入 snapshot。
9. fake provider 合法 T2 输出可跑 M1 skeleton；真实 CLI 用 fixture 子进程测试，不依赖线上模型。
10. replay JSONL 一条 `brain_call` record 内携有序 attempts，可区分两个 provider attempt 并还原最终 fallback。
11. input 超过 `max_input_bytes` 时不调用 provider，走触点确定性兜底。

### 10.2 M2：T1 Intake crash/generation

以下验收依赖 M2 的真实 Forge comment worker、回复 receipt 消费与 `PersistIntakeDecision` 写端口，因此不属于 M1 退出条件：

1. 澄清/确认评论在“远端成功、本地提交前崩溃”后按 outbox marker 查询收敛，不重复发送。
2. 回复按当前 `clarification_generation` 仲裁；旧 generation 回复只追加审计事件，不推进 intake 状态。

## 11. 自查结果

- [x] B1：call/attempt 拆表、一次性终结与 `provider_error_code` 枚举落 storage §10.1/§13；replay 单 record 携有序 attempts。
- [x] B2：intake 投影、状态机、回复 generation 协议与 outbox 目的/key 的规格完整；实现与 crash/generation 验收明确归属 M2。
- [x] B3：T1/T2 输入输出逐字段冻结，含枚举、长度、互斥矩阵与总输入上限；`.schema.json` 单一来源。
- [x] B4–B7：token per-attempt 阈值与越界 post-charge、open-envelope 边界、截断/stderr 语义、T2 审批单事务均已写死。
- [x] Task Spec 来源、hash、不可变版本与 control-plane transport 对齐；路径事实只引用 config.md。
- [x] 相对链接存在、代码围栏闭合、无尾随空白。

**自查结论：** 评审全部 P1（B1–B3）与约定采纳的 P2（B4–B7）已按「评审处置」表关闭，交叉补丁（storage/outbox/config/WBS）与本文不矛盾，转 `active`。
