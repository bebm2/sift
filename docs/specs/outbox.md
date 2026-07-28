---
status: active
created: 2026-07-28
summary: 外部副作用、幂等证据与重试收敛契约
---

# Outbox 规格

本文冻结 transactional outbox 的 operation payload、稳定键、claim/lease、逐类远端证据、错误分类与终态收敛。

结构来源：[DESIGN §6.3–§6.5](../DESIGN.md)、[ADR-003](../decisions/003-transactional-outbox.md)、[ADR-010](../decisions/010-attempt-spawn-handoff.md)、[ADR-011](../decisions/011-merge-requires-expected-head-cas.md)。表与事务见 [`storage.md` §8/§11–§13](storage.md)，launch handoff 见 [`control-plane.md` §4–§7](control-plane.md)，默认值见 [`config.md` §3.7–§3.8](config.md)。

## 评审处置

评审原文：[2026-07-28-outbox-review-pi-gpt-5.6-sol.md](../reviews/2026-07-28-outbox-review-pi-gpt-5.6-sol.md)。

| 发现 | 处置 |
|------|------|
| O1 过期 executing 无法 reclaim | 单 CAS 接管并补旧 attempt lease_expired result |
| O2 launch key 缺 generation | key 纳入 generation，旧 operation stale |
| O3 complete 不能承接领域效果 | outcomeCommand 原子写投影/event/Interrupt/后继 operation |
| O4 marker digest 自引用 | payload 不存 marker，worker 确定生成 |
| O5 Forge charge key 缺失 | 固定 attempt id + call sequence |

O1–O5 已关闭，评审通过，本规格转为 `active`。

## 1. 不变量

1. 外部 IO 前必须已有同领域事务提交的 outbox operation；worker 不临时补建 operation。
2. operation payload 创建后不可改；重试只更新 lease、attempt、证据、错误和调度字段。
3. 一个 operation 同时最多一个有效 lease；过期 worker 的结果整笔拒绝。
4. worker 每次真实 Forge API/CLI 调用前经 `ChargeForgeAPICall` 预留一次预算，包括证据查询；charge key 固定为 `forge-call:<outbox_attempt_id>:<call_seq>`，call_seq 从 1 递增。事务内不调用 Forge。
5. outbox 只保证至少一次尝试。effectively-once 必须由本规格逐 kind 的证据取得，不能由 operation key 推导。
6. 未知 payload version/kind、证据歧义、远端契约缺失均 fail closed，不降级为“再试一次看看”。
7. daemon 启动恢复完成前不得 claim `launch_agent`；其他 kind 可在数据库/配置恢复后执行。

## 2. Operation envelope

`outbox_operations.kind` 与 payload tagged union 一一对应。共同 envelope：

```json
{
  "schema_version": 1,
  "kind": "forge_comment",
  "project_id": "...",
  "created_from_event_id": "...",
  "body": {}
}
```

`schema_version` V0 为 1；`kind` 必须等于表列；`project_id` 可仅在 `channel_publish` 为 null。`created_from_event_id` 必须引用产生 operation 的事件。payload canonical JSON 的 SHA-256 必须等于表中 `payload_digest`。

稳定 operation key 只使用已冻结 ID/hash，不含时间、attempt_count、随机 request id 或可漂移文本：

| kind | operation key |
|------|---------------|
| `forge_comment` | `comment:<purpose>:<subject_id>:<generation>` |
| `forge_labels` | `labels:<subject_kind>:<subject_id>:<projection_version>` |
| `create_change` | `run:<run_id>:create-change:<head_sha>` |
| `merge_change` | `run:<run_id>:merge:<expected_head_sha>` |
| `channel_publish` | `interrupt:<interrupt_id>:publish:<escalation_no>` |
| `launch_agent` | `run:<run_id>:attempt:<attempt_no>:generation:<generation>:launch` |
| `command_ack` | `command:<forge_event_id>:ack` |
| `forge_alert` | `alert:<alert_kind>:<subject_id>:<generation>` |

同 key 不同 payload digest 是 `contract_violation`，不得返回既有 operation 冒充成功。

## 3. Marker

Forge comment 与 Change body 使用同一不可见 marker：

```text
<!-- sift-op:v1:<base64url(UTF-8 operation_key)>:<payload_digest> -->
```

base64url 无 padding。渲染前必须从所有自然语言输入中移除形如 `<!-- sift-op:` 的片段；marker 只能由确定性 renderer 在末尾追加一次。查询时要求 operation key 与 digest 同时匹配：

- 同 key + 同 digest：本 operation 证据；
- 同 key + 异 digest：`semantic_conflict`；
- 多个对象命中同 marker：`semantic_conflict`，不任选其一。

Channel 无可靠查询面，使用可见标识 `[sift <operation_key>]`；重复投递对人可辨认但不宣称去重。

## 4. Worker 状态机

```text
pending/retryable --claim CAS--------------------> executing
executing + expired lease --reclaim CAS----------> executing (new owner/attempt)
executing --success------------------------------> succeeded
executing --transient/rate_limited--> retryable
executing --permanent contract--> failed
executing --fact changed--> stale
executing --ambiguous ownership--> conflict
```

claim 事务必须同时：校验 pending/retryable 到期或 executing lease 过期 → reclaim 时为旧 attempt 插入 `retry/transient:lease_expired` result → 写新 lease owner/expiry → attempt_count+1 → 插入新 immutable `outbox_attempts`。不得先把过期 executing 异步改成 retryable；reclaim 是一次 CAS，旧 owner 随即失去 complete 权。外部执行后 `CompleteOutboxAttempt(expectedLease, outcomeCommand)` 同时插入 immutable result、CAS operation，并按 kind 更新必要投影/事件：Create/Merge 更新 Run/Change 事实，Channel 更新 delivery，auth/capability 隔离项目并建唯一告警，conflict/stale 可原子产生 Interrupt/重算事件/后继 operation。不得先终结 operation、再另事务补领域效果。

lease owner 为 `<daemon_boot_id>:<worker_id>`。执行完成提交前必须仍匹配 owner 且 lease 未被新 owner替换；否则返回 `RejectedStaleWorker`，不插 result。lease 到期不证明外部动作未发生，新 owner 必须先走该 kind 的证据协议。

### 4.1 退避

瞬时失败第 n 次后的本地 delay：

```text
min(retry_max_delay, retry_initial_delay * retry_multiplier^(n-1))
```

只使用整数毫秒并向上取整，不加随机 jitter。`rate_limited` 若带可信 Retry-After，取 `max(local_delay, retry_after)`；它可超过 retry_max_delay，因为后者只约束本地退避。`max_attempts>0` 且达到上限后转 failed；0 持续重试。semantic conflict、contract violation、stale 不消耗后续重试。

## 5. Forge comment / command ack / alert

### 5.1 Payload

三类共用 body：

```json
{
  "forge_kind":"github",
  "forge_host":"github.com",
  "forge_project_key":"owner/repo",
  "target_kind":"issue",
  "target_id":"123",
  "purpose":"interrupt",
  "markdown":"..."
}
```

- `forge_comment.purpose = interrupt | summary`；
- `command_ack.purpose = command_ack`；
- `forge_alert.purpose = channel_failure | project_isolated | config_drift`；
- payload 不存 marker，避免 digest 自引用；worker 在执行时由 operation key + 已冻结 payload digest 重算并追加 marker。

### 5.2 执行

1. 按 marker 查询目标评论；唯一命中则 succeeded 并保存 remote comment id。
2. 无命中才创建评论。
3. 创建成功后保存 id；本地提交前崩溃时重试回到第 1 步。
4. 查询不支持完整分页或返回被截断集合是 contract violation，不能据“没看到”创建。

评论被人删除后重试可能无证据；operation 已 succeeded 时不因后续删除自动重发。只有新的领域事件可创建新 operation。

## 6. Forge labels

Payload：

```json
{
  "forge_kind":"github","forge_host":"github.com","forge_project_key":"owner/repo",
  "target_kind":"issue","target_id":"123",
  "add":["sift:running"],"remove":["sift:queued"],
  "expected_projection_version":3
}
```

add/remove 各自排序去重且不相交。执行前本地目标 projection version 不同则 stale，不把旧状态标签写回。版本仍匹配时读取当前 label set，计算 `(current ∪ add) − remove`，使用平台 set/add-remove 幂等语义写入，再重读确认目标标签子集。与本 operation 无关的标签必须保留。

远端对象已不存在时根据已摄入外部事实收敛 stale；权限/能力缺失为 `auth_or_capability` 并隔离项目，不无限瞬时重试。

## 7. Create Change

Payload：

```json
{
  "run_id":"...","forge_kind":"github","forge_host":"github.com","forge_project_key":"owner/repo",
  "base_ref":"main","head_ref":"sift/run-...","head_sha":"...",
  "title":"...","body_markdown":"..."
}
```

执行协议：

1. 跨 open/closed/merged 全状态按 marker 搜索。
2. 唯一命中：保存 Change id/url/state/head；不创建第二个。
3. 无 marker 命中时，查询同 base/head 的全部 Change；存在 marker 不同或无 marker 的对象即 conflict，绝不接管。
4. 无冲突才创建；body 必须含本 operation marker。
5. 创建响应的 marker/id/head 不一致为 contract violation。
6. 远端已 closed/merged 的 marker 命中按外部事实推进 Run，不重新创建。

成功证据：

```json
{"schema_version":1,"change_id":"...","change_url":"...","state":"open","head_sha":"...","marker":"..."}
```

首次保存 Change id 后，后续对账只按 id；按 marker 搜索仅用于“远端成功、本地 id 未提交”的窗口。head 变化形成新 operation key；旧 operation 收敛 stale，不得以同 key 改 payload。

## 8. Merge Change

Payload：

```json
{
  "run_id":"...","change_id":"...","gate_evaluation_id":"...",
  "expected_head_sha":"...","merge_method":"merge"
}
```

`expected_head_sha` 必须等于引用 Gate snapshot 的 head，operation key 也含该 SHA。执行：

1. 预读 Change：已以 expected head 合并则 succeeded；当前 head 不同则 stale，并触发新 head 重新冻结 Gate；closed 未合并按外部事实收敛。
2. 当前 head 相同时发**远端条件 merge**，请求自身携 expected head；预读不可替代条件请求。
3. 远端 CAS mismatch → stale；不重试旧 operation。
4. 适配器无 expected-head CAS capability → `auth_or_capability`/project isolated；禁止无条件 merge。
5. 成功后按 Change id 重读 merged state/head/merge sha 并持久证据。

`merge_method` V0 只能为 `merge`；未来新增方法须版本化并验证平台语义。

## 9. Channel publish

Payload：

```json
{
  "interrupt_id":"...","escalation_no":0,"priority":"normal",
  "rendered_text":"..."
}
```

worker 由 operation key 生成并追加可见标识；payload 不接受调用方 marker。Channel 无查询证据，每个 executing attempt 都可能真实推送；语义明确为 at-least-once。成功响应只证明本次调用返回成功，不证明未重复。

连续失败次数来自同 operation 的 attempt results。达到 config `channel_failure_alert_after` 时，complete 事务以稳定键创建一个 `forge_alert`，并更新 interrupt delivery/doctor 投影；继续重试原 Channel operation。alert 自身失败不递归创建 alert。

escalation 使用新 operation key但复用原 attention charge；同 escalation 重试不新扣费。

## 10. Launch Agent

Payload不含任何 capability 明文：

```json
{
  "run_id":"...","attempt_no":1,"generation":1,"backend":"process",
  "run_dir":"...","worktree_path":"...","task_spec_snapshot_id":"...",
  "agent":{"id":"...","executable":"...","args":[],"task_transport":"stdin"}
}
```

协议：

1. 恢复门禁通过后 claim operation lease。
2. 生成 dispatch id、run token、bootstrap nonce；调用 `PrepareLaunchDispatch` CAS 保存 dispatch 与 hash。
3. 按 [`control-plane.md` §7.1](control-plane.md) 原子写 bootstrap。
4. backend 只 spawn wrapper，bootstrap path 是唯一新增 argv；不把 credential 放 argv/env。
5. operation 的“外部执行成功”不是 wrapper spawn 返回，而是 `claim.acquire` 已绑定 session；handler 必须调用 `AcquireLaunchClaim`，把 pending→starting、session hash、immutable outbox result 与 operation succeeded 原子提交。后续 permit/started 由 control-plane/storage 推进。

崩溃收敛：

- prepare 前：普通 lease 重试；
- prepare 后、bootstrap 前：无 owner/control，旧 operation → stale；同事务递增 generation并创建含新 generation 的 launch operation，绝不修改旧 payload；
- bootstrap 后、spawn 前：新 owner校验并复用文件/dispatch；
- spawn 后、acquire 前：竞争 wrapper只有一个可绑定 session；
- acquire 后：operation succeeded，attempt 恢复接管 starting/spawning，绝不重放 launch。

`spawning` 后 effectively-once 的强度来自 session/permit/进程组消失证据，不来自 outbox lease。

## 11. 结果与错误分类

| class/outcome | operation state | 领域动作 |
|---------------|-----------------|----------|
| success | succeeded | 保存证据并推进必要投影 |
| transient | retryable | 退避 |
| rate_limited | retryable | honor Retry-After |
| auth_or_capability | failed | 项目隔离 + 唯一告警 |
| contract_violation | failed | 安全事件；必要时 failure_review |
| semantic_conflict | conflict | 转唯一 HITL，不接管远端对象 |
| stale | stale | 吸收新事实；必要时重算 Gate/transition |

错误 summary 必须去除 CLI stderr 中的 token、URL query credential 与控制文件内容；原始 stderr 不入事件或 outbox。每个 attempt 均恰有一个 immutable result；worker 崩溃留下的 attempt 由 reclaim 写 `lease_expired` result。

## 12. M1 骨架与后续 kind

M1 必须完整实现通用 claim/complete、退避、immutable attempts/results 与 `launch_agent`；其他 kind 的 payload decoder、operation key builder 和 fake adapter 契约在 M1 建立，随对应里程碑启用。不得用一个 `map[string]any` payload 占位后绕过 schema。

## 13. 验收

1. operation 与领域投影同事务；各写点崩溃只能全有或全无。
2. payload 创建后数据库 trigger 拒绝修改；同 key 异 digest 为 contract violation。
3. 过期 executing 可被单 CAS reclaim；claim/lease/complete 拒绝旧 owner，每次已结束尝试有且仅有一个 immutable result。
4. comment/ack/alert 在“远端成功、本地提交前崩溃”后按 marker 收敛，不重复创建。
5. create Change 全状态 marker 搜索；同 base/head 非本 operation 对象必须 conflict。
6. merge 的预读相等但远端 CAS mismatch 仍 stale；无 CAS capability 不自动 merge。
7. labels 不删除非 Sift 标签；重复执行得到同集合。
8. Channel 注入响应丢失可产生可辨认重复；达到失败阈值只创建一个 forge alert且不递归。
9. launch 在 prepare/bootstrap/spawn/acquire 每个边界崩溃均不签发两个 permit、不双起有效 Agent。
10. 每次 Forge 证据查询和动作调用均唯一收费；预算不足时不发起调用。
11. max_attempts、指数退避与 Retry-After 使用注入时间可确定测试。
12. fake adapter 覆盖 success/transient/rate-limit/auth/contract/conflict/stale 全分类。

## 14. 评审冻结项

1. comment marker 必须在 GitHub/GitLab 评论与 Change body 中无损保存并全分页搜索。
2. create Change 必须同时执行全状态 marker 查询和同 base/head 冲突查询。
3. 平台不能提供 expected-head merge CAS 时必须禁用 auto merge。
4. launch operation 在 acquire 原子绑定 session 时 succeeded；started 属 attempt handoff，不延长 outbox lease。

## 15. 自查结果

- [x] 八类 operation 均有稳定 key、closed payload 与明确投递语义。
- [x] effectively-once 声明均有 marker/set/CAS/handoff 证据；Channel 如实为 at-least-once。
- [x] create Change 不接管同 base/head 的人工对象；merge 不以预读替代远端 CAS。
- [x] launch payload 无 capability 明文，prepare/file/spawn/acquire 窗口有唯一恢复动作。
- [x] Forge 查询与动作均经过唯一收费口。
- [x] 相对链接存在、代码围栏闭合、无尾随空白。

**自查结论：** 字段级契约完整，评审通过，允许转 `active`。
