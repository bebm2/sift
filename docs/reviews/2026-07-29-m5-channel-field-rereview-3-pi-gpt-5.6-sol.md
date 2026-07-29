PASS WITH NOTES

# channel.md MCH1–MCH3 第三次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 前次复审：[`2026-07-29-m5-channel-field-rereview-2-pi-gpt-5.6-sol.md`](2026-07-29-m5-channel-field-rereview-2-pi-gpt-5.6-sol.md)
> 核销对象：#434 / PR #437
> 评审基线：`main` `0f9513d`
> 评审对象：[`docs/specs/channel.md`](../specs/channel.md) draft；交叉核对 active [`config.md`](../specs/config.md)、[`interrupt.md`](../specs/interrupt.md)、[`outbox.md`](../specs/outbox.md) 与 [`storage.md`](../specs/storage.md)

## 1. 结论

**PASS WITH NOTES（MCH1-A、MCH1-B、MCH3 均已关闭；MCH2 未回退；无遗留 P1）。**

#434 已把异 Forge host 的并发入批结果冻结为两条具体 batch/member/operation identity，并补齐 critical batch ID grammar 与 exact fixture；`forge_alert(channel_failure)` 的 canonical markdown 现含固定诊断行，两个受影响 payload digest 均可由 canonical UTF-8 bytes 独立复算；reclaim 在最后一次 attempt 过期时现有唯一 terminal CAS，写 `lease_expired`、终结 operation/delivery/episode、按阈值建 alert，且不创建超限 lease/attempt。`channel.md`、`outbox.md` 与 `storage.md` 的规范文字和 exact vectors 对该分支一致。

MCH2 的 `secret_ref:<name>` resolver handle、adapter-only resolution、rotation 与错误分类边界未被 #434 修改或削弱。按 #438 约束，本轮只新增本报告，不修改规格或实现；[`channel.md`](../specs/channel.md) 保持 `status: draft`。

## 2. 核销结果

### MCH1-A：已关闭

[`storage.md` §6.3](../specs/storage.md#63-attention_admissionsattention_batches-与成员) 现冻结完整 critical grammar：

`critical:<scope>:<scope_id>:<episode_admission_id>:<channel_id>:<forge_kind>:<base64url(forge_host)>:<base64url(forge_project_key)>:<target_kind>:<base64url(target_id)>`。

§6.6 的 concrete concurrent vector 给出同 `project-a/github/issue/42`、不同 host `github.com` 与 `git.example.com` 的唯一结果：`i-a/i-b` 属于前一 batch，`i-c` 独占后一 batch；两条完整 batch ID 和 operation key 均已列出，不再保留“distinct 或 held”的二选一。critical vector 同时给出 `i-d/i-e` 的输入、member 归属、完整 batch ID、operation key 与 replay 结果。

独立复算确认相关编码正确：

- `base64url("github.com") = Z2l0aHViLmNvbQ`
- `base64url("git.example.com") = Z2l0LmV4YW1wbGUuY29t`
- `base64url("owner/project-a") = b3duZXIvcHJvamVjdC1h`
- `base64url("42") = NDI`

### MCH1-B：已关闭

[`channel.md` §4](../specs/channel.md#4-重试failure-episode-与-forge-告警) 与 [`outbox.md` §5.1](../specs/outbox.md#51-payload) 均冻结固定行 `Diagnostics: sift ps; sift doctor`。[`storage.md` §6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors) 的唯一 canonical alert markdown 包含该行及 operation key、generation、连续失败数、安全错误分类和 delivery 状态。

按 canonical compact JSON、UTF-8 bytes 独立复算：

- sealed batch body SHA-256：`ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e`
- `forge_alert` body SHA-256：`ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2`

两者均与规格值匹配；payload 仍只含 `secret_ref:SIFT_CHANNEL_OPS_SLACK` handle，不含 resolver result、endpoint 或 credential。

### MCH3：已关闭

[`outbox.md` §4/§4.1/§10](../specs/outbox.md#4-worker-状态机) 与 [`storage.md` §6.5.1/§6.6/§8.4/§11/§12.4](../specs/storage.md) 现一致规定：reclaim 先写旧 attempt 的 immutable `retry/transient:lease_expired` result；若该 result 后达到正数 `max_attempts`，同一 CAS 将 operation、delivery 与 episode 收敛为失败，清 lease，按阈值去重 alert，且不递增 `attempt_count`、不插 attempt 4、不创建新 lease。只有未达限才创建下一 lease/attempt。

§6.6 已新增恰好在 executing attempt 3 过期的 terminal vector，以及随后旧 attempt 3 completion 的 stale vector；后者不得改变既有 result、count、delivery、episode 或 alert，也不得创建新 attempt。该结果关闭了前次“终结同时又无条件接管”的冲突。

## 3. MCH2 回归核对

| 项 | 判断 | 核验结果 |
|---|---|---|
| payload target schema | **YES** | 仍唯一为非秘密 `secret_ref:<name>` handle。 |
| resolver owner / rotation | **YES** | adapter 每次 executing attempt 解析 sealed handle；rotation 下一 attempt 生效，不读取漂移 config。 |
| failure classification | **YES** | handle 缺失/拒绝仍为 `auth_or_capability`；非法 resolved endpoint 仍为 `contract_violation`。 |
| secret boundary | **YES** | endpoint、credential 与 query secret 仍禁止进入 payload、digest、日志及诊断。 |

## 4. 非阻断注记

[`outbox.md` §4](../specs/outbox.md#4-worker-状态机) 顶部的简化 ASCII 状态图仍只画出 `executing + expired lease -> executing (new owner/attempt)`，未画同节紧随其后的“达 `max_attempts` -> failed、无新 owner/attempt”分支。该节规范正文、§4.1、§10、storage 事务配方及两个 exact vectors 已唯一仲裁 terminal 行为，因此不构成实现歧义或 P1；后续若维护该图，宜补上 guard 以减少读者误解。

## 5. 验收检查清单

- [x] 在检测到的 GitHub forge 获取并阅读 #438 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #438 comments（当前无评论）：**YES**
- [x] 获取并核对 #434 全文、评论及 PR #437 合入基线：**YES**
- [x] 对照前次 FAIL 的 MCH1-A、MCH1-B、MCH3 逐项复审：**YES**
- [x] MCH1-A concrete 异 target 并发 vector、critical grammar/fixture closed：**YES**
- [x] MCH1-B canonical markdown 与 payload digest closed：**YES**
- [x] MCH3 terminal reclaim、阈值 alert、无新 attempt 及 stale completion vector closed：**YES**
- [x] MCH2 未回退：**YES**
- [x] 交叉核对 channel/config/interrupt/outbox/storage：**YES**
- [x] 独立复算 base64url 与两个 canonical SHA-256：**YES**
- [x] 只新增评审报告，未修改规格或实现：**YES**
- [x] 报告首行为 `PASS WITH NOTES`：**YES**
- [x] `channel.md` 保持 `status: draft`：**YES**
- [x] 遗留 P1：**NO（0）**
