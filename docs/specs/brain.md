---
status: draft
created: 2026-07-28
summary: Brain 调用壳、T1/T2 schema 与确定性兜底契约
---

# Brain 规格

本文冻结 Brain 统一调用壳、调用身份、提示词资产、T1/T2 输入输出、Task Spec 组装、token 记账与确定性兜底。T3–T7 沿用同一壳，随对应里程碑增补 schema。

来源：[PRD §5.3、§5.7](../PRD.md)、[DESIGN §8.3](../DESIGN.md)、[`storage.md` §9–§10](storage.md)、[`config.md` §3.4](config.md)、[WBS M1 §1.7](../WBS.md)。

## 1. 不变量

1. LLM 只输出建议；状态转移、硬护栏、Agent 存在性/并发、去重、预算和权限均由确定性代码裁定。
2. 每个触点只有一个版本化 prompt + output schema 来源；运行时 schema、生成 JSON Schema 与回放 schema 同源。
3. 调用流程固定为：预算门禁 → 原 prompt 调用 → closed decode → 失败时**同一输入/同一 prompt**重试一次 → 再失败走触点兜底。
4. 禁止提取 markdown fence、修 JSON、忽略未知字段、类型强转或让第二个 LLM“修复”输出。
5. 每个物理 provider attempt 与未调用 provider 的预算/禁用兜底都必须持久 trace；外部调用不发生在数据库事务内。
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

prompt version 为 `<touchpoint>/v<integer>/<sha256前12位>`；hash 覆盖 prompt UTF-8 bytes、output schema canonical JSON 与调用协议版本。文件嵌入 binary，运行期不从磁盘热读。改 prompt、schema 或 envelope decoder 任一项必须生成新 version。

Prompt 固定分区：system contract → untrusted input delimiters → input canonical JSON → output schema。Issue/Context 中出现的指令一律标记为 untrusted data；prompt 不声称这能消除 injection。

## 3. 调用身份与序列

logical identity：

```text
(scope, subject_key, touchpoint, call_seq)
```

- T1：`scope=intake`，`subject_key=forge:<kind>:<normalized_host>:<project_key>:issue:<issue_id>`。
- T2：`scope=run`，`subject_key=run:<run_id>`。
- T3–T6：`scope=run`；T7 为 `aggregate`，与 storage 规则一致。

`call_seq` 是同 subject/touchpoint 的逻辑调用序号，从 1 递增；必须在调用前由存储事务 reserve，禁止 `SELECT max()+1`。schema retry 不增加 call_seq，而增加 `provider_attempt=1|2`。

因此 storage 需在实现前补：

- mutable `brain_call_counters(scope,subject_key,touchpoint,next_call_seq,version)`；
- `ReserveBrainCall` 写端口；
- trace 的 `logical_call_id`、`provider_attempt` 与 `attempt_outcome`；
- 唯一键 `(scope,subject_key,touchpoint,call_seq,provider_attempt)`。

`provider_attempt=0` 只表示 provider disabled/budget exhausted 等调用前兜底；1/2 表示真实子进程调用。`attempt_outcome=valid | invalid_output | provider_error | fallback`。最终走兜底的最后一行 `fallback_used=1`；之前失败行不伪称 fallback。

## 4. Provider 子进程协议

V0 protocol 为 `claude-json-v1`。config 增加 closed 字段：

| 字段 | 默认 | 约束 |
|------|------|------|
| `protocol` | `claude-json-v1` | V0 只能为此值 |

调用使用配置 executable + args，prompt/input 从 stdin 传入，不使用 shell，不把输入放 argv。子进程工作目录为空临时目录，环境只保留运行 CLI 所需的最小 allowlist；不得注入 operator/run/wrapper credential。

stdout 上限为 config `max_raw_output_bytes`；超限立即终止进程并记 `provider_error=output_too_large`。stderr 只保留去凭据后的有界摘要，不进入 prompt 重试。timeout 使用 config `call_timeout`。

`claude-json-v1` adapter 将 CLI 外层结果规范化为：

```json
{
  "result_text":"{...}",
  "usage":{"input_tokens":123,"output_tokens":45}
}
```

外层 envelope 按 adapter schema closed decode；`result_text` 必须是仅含一个 JSON object 的 UTF-8 字符串，再按触点 schema closed decode。usage 必须是非负整数；缺失/负值使本 attempt 无法计费，按 provider error 处理并触发重试/兜底，不能把 token 当 0。

## 5. 重试与 trace

每次逻辑调用：

1. reserve identity；
2. provider disabled 或预算门禁拒绝：写 provider_attempt=0 fallback trace，返回兜底；
3. 调 provider attempt 1；无论 exit/output/schema 结果都写 immutable trace并按实际 usage 收费；
4. valid 则返回；否则以**完全相同 prompt bytes/input digest**调用 attempt 2；
5. attempt 2 valid 则返回；否则该行标 final fallback并返回确定性兜底。

重试不得加入“上次哪里错了”的修复提示。两个 attempt 的 `input_digest/prompt_version/output_schema_version` 必须相同。原始 stdout 完整保存到 `raw_output_text`（受大小上限）；子进程没产生合法 stdout 时为 null。

## 6. Token 预算

Brain 调用全局串行通过预算门禁，避免多个并发调用同时看到旧余额。`daily_token_limit` 是**发起新调用的实际消费阈值**：调用前 `consumed >= limit` 即拒绝；一次已发起调用可能因 provider 只在结束时报告 usage 而越过阈值，越界后当日所有后续触点兜底并发一次告警。不得丢弃这次真实 usage。

因此 token counter 是固定预算的唯一例外：实际 usage 必须全额 append entry并允许 counter 单次越界；attention/Forge/Report 仍禁止借支。config 与 storage 必须同步明确该语义。

收费 operation key 为 `brain:<logical_call_id>:provider:<provider_attempt>`。input+output token 均计入；重试是真实第二次成本，单独收费。usage 已知前不得写猜测值。

## 7. T1 Intake 体检

### 7.1 Input v1

```json
{
  "forge":{"kind":"github","host":"github.com","project_key":"owner/repo"},
  "issue":{"id":"123","title":"...","body":"...","author":"...","url":"...","labels":[]},
  "known_candidates":[{"run_id":"...","issue_id":"...","title":"...","status":"running"}]
}
```

候选由确定性检索产生并按 run_id 排序；不得让 LLM 查询数据库/Forge。

### 7.2 Output v1

```json
{
  "disposition":"ready",
  "questions":[],
  "possible_duplicate_run_id":null,
  "rationale":"..."
}
```

- `disposition=ready | needs_clarification | possible_duplicate`；
- questions 最多 5 个非空字符串；ready 时必须为空；
- duplicate id 只能引用 input candidate，否则领域后校验失败；
- T1 只是建议：possible duplicate 必须由确定性 Issue identity/现有 Run 事实确认，LLM 不能单独吞掉任务。

M1 消费规则：ready → 创建 Run；needs_clarification/possible_duplicate → 创建可审计 intake 结果并走确定性澄清/确认路径。该路径所需摄入投影与 outbox comment 将在评审中确认；不能用“直接不创建 Run”造成静默丢失。

失败兜底固定为：`disposition=ready, questions=[], possible_duplicate_run_id=null, rationale="fallback"`。

## 8. T2 分派

### 8.1 Input v1

```json
{
  "run_id":"...",
  "issue":{"title":"...","body":"...","url":"..."},
  "candidate_agents":[{"id":"claude-code","capabilities":[]}],
  "base_context":{"project_context":"...","global_context":"...","task_annotations":[]}
}
```

candidate agents 已经过配置、项目引用和启动探测过滤，按 id 排序。Context 内容是 untrusted data。

### 8.2 Output v1

```json
{
  "kind":"feature",
  "agent":"claude-code",
  "hitl_before_start":false,
  "goals":["..."],
  "risk_notes":"...",
  "rationale":"..."
}
```

- kind=`feature | bug | chore | docs | refactor`；
- agent 必须精确命中 candidate id；
- goals 1..10，每项非空、去重；
- risk_notes/rationale 可空字符串但字段必填；
- LLM 不输出 guardrails、max attempts、并发或 policy。

失败兜底：Run 保持/进入 `waiting_human`，agent/kind 为空，生成 `design_approval` 人工分派 Interrupt；不自动挑“第一个 Agent”。

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
  "brain":{"trace_id":"...","prompt_version":"..."}
}
```

组装顺序固定为 Description → Goals → Guardrails → project/global/task Context。project context 只从 base ref `.sift/context.md` 读取；global 从 `$SIFT_HOME/context.md` 读取；缺失为空且 hash 为规定空内容 hash。Guardrails 只来自有效 policy/硬编码默认，不接受 T2 改写。

整份 canonical JSON 与 digest 写 immutable snapshot；初始版本为 1。`/sift ask` 创建下一版本并保留旧 snapshot，已启动 attempt 继续引用旧版本。

## 10. M1 验收

1. fixture 覆盖 valid first、invalid→valid、invalid→fallback、timeout、nonzero exit、oversize、usage missing。
2. 两次 provider attempt 的 prompt bytes/input digest完全相同，trace identity 只差 provider_attempt。
3. unknown field/type/enum/fenced JSON/尾随文本均拒绝，不尽力解析。
4. provider disabled 与 token threshold 使用 provider_attempt=0 trace并走触点兜底。
5. 实际 usage 包括失败与重试；单次越界全额记账，之后不再调用。
6. T1 fallback 直接入队且不静默丢 Issue；LLM duplicate 建议不能绕过确定性确认。
7. T2 unknown/noncandidate Agent 触发同 prompt retry，最终进入人工分派；硬护栏不从 LLM 输出读取。
8. Task Spec 的四段来源/hash 可重建；worktree 中 context/policy 修改不进入 snapshot。
9. fake provider 合法 T2 输出可跑 M1 skeleton；真实 CLI 用 fixture 子进程测试，不依赖线上模型。
10. replay JSONL 可区分两个 provider attempt并还原最终 fallback。

## 11. 待评审重点

1. storage 的 logical call/provider attempt 扩展是否应拆 `brain_calls`/`brain_attempts` 两表，而非继续扩单表。
2. token 阈值允许单个已发起调用越界是否是唯一可审计且可实现的 V0 语义。
3. T1 needs_clarification/possible_duplicate 在 Run 创建前需要何种持久投影与回复恢复协议。
4. `claude-json-v1` 外层 envelope 是否应 open-envelope 接受 CLI 新增诊断字段，内层触点输出仍保持 closed。

## 12. 自查结果

- [x] T1/T2 均有 closed input/output、领域后校验和确定性兜底。
- [x] schema retry 不改变 prompt，且每个物理调用独立 trace/收费。
- [x] 调用身份覆盖 intake/run/aggregate，不强绑 attempt。
- [x] Task Spec 来源、hash、不可变版本与 control-plane transport 对齐。
- [x] token 与注意力预算方向不混用。
- [x] 相对链接存在、代码围栏闭合、无尾随空白。

**自查结论：** 初稿完整但有三项存储/摄入边界需评审，保持 `draft`。
