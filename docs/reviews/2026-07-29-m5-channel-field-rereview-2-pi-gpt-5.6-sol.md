FAIL

# channel.md MCH1–MCH3 第二次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 前次复审：[`2026-07-29-m5-channel-field-rereview-pi-gpt-5.6-sol.md`](2026-07-29-m5-channel-field-rereview-pi-gpt-5.6-sol.md)
> 核销对象：#426 / PR #429
> 评审基线：`main` `8ee9117`
> 评审对象：[`docs/specs/channel.md`](../specs/channel.md) draft；交叉核对 active [`config.md`](../specs/config.md)、[`interrupt.md`](../specs/interrupt.md)、[`outbox.md`](../specs/outbox.md) 与 [`storage.md`](../specs/storage.md)

## 1. 结论

**FAIL（3 个 P1）。**

#426 已完成大部分结构性修订：daily batch key 纳入 Forge kind/host/project key，`interrupt.md` 的旧 daily/critical literal key 已删除；`forge_alert(channel_failure)` 现有含 `markdown` 的 canonical payload，两个 SHA-256 均可由正文 bytes 复算；reclaim CAS 也被同步授权为 episode 写端口，并补了 lease expiry、重启和 stale completion vectors。MCH2 的 `secret_ref:<name>` resolver 模型没有回退。

但 MCH1-A 要求的异 target **exact** 并发 vector 没有落地，critical batch ID 仍没有 closed bytes；MCH1-B 的唯一 canonical markdown 又缺少 `channel.md` 自己规定的 `sift ps` / `sift doctor` 指引；MCH3 的 reclaim 文字无条件创建新 attempt，与 `max_attempts` 达限终结冲突。三处仍会使实现或验收得到不同结果，不能降为 notes。

按 Issue #430 约束，本轮只新增本报告，不修改规格、不自修；[`channel.md`](../specs/channel.md) 保持 `status: draft`。

## 2. P1 发现

### MCH1-A（P1）：不同 Forge target 仍无 exact 并发结果，critical identity 也未闭合

[`storage.md` §6.3](../specs/storage.md#63-attention_admissionsattention_batches-与成员) 已给 daily key 加入完整 target，正文声明不同 host/project key 不碰撞；[`interrupt.md` §8.3](../specs/interrupt.md#83-attention-admissioncritical-熔断与-batch) 也不再保留旧 literal key。这两项方向正确。

然而 #426 明确要求“同 project/kind/id、不同 forge host/project key 的并发 exact vector”。[`storage.md` §6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors) 目前只有一组 `github.com / owner/project-a` 的 exact batch bytes；所谓异 target vector 只写成抽象二选一：产生不同 identity，**或**走确定性 held/single-delivery。它没有给第二个具体 host/project key、期望 batch ID、member 归属和 operation key，因此既不是 exact vector，也没有在两个允许分支中冻结唯一结果。

此外同节的表定义只逐字节给出 daily ID；critical 仅写“采用同样纳入完整 target 的稳定 identity”。它未规定原有 `<scope>:<scope_id>:<episode_admission_id>:<channel_id>` 与五个 target 段的准确顺序/编码，也无 critical exact fixture。`interrupt.md` 已把 critical key 权威完全委托给 storage，故实现者仍须自行发明 critical bytes。

**关闭条件：** 给至少一组同 project/kind/target kind/target ID、但 host 或 project key 不同的 concrete 并发输入和唯一 exact 输出（两条 batch ID、member 归属、operation key）；同时冻结 critical batch ID 的完整 grammar，或给确定性拒绝分支的唯一 exact 结果，不能继续保留“distinct 或 held”二选一。

### MCH1-B（P1）：canonical alert markdown 不满足本规格的最小正文

[`storage.md` §6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors) 现在确实给出 closed `forge_alert` body，含必填 `markdown`，并声明只读取持久化 operation/episode 安全字段。复算结果为：

- batch body：`86f73989368627715ae55312b3edb4471bd4af5b0a9a85c97c21eeb51b42ab17`
- alert body：`41f7569fa72bf8a4ca94e45e1e8d82a6035b1cb3420bfe659bea6d6811937e9a`

但 [`channel.md` §4](../specs/channel.md#4-重试failure-episode-与-forge-告警) 规定告警评论“至少含”operation key、episode/generation、连续失败数、安全错误分类、delivery/terminal 状态以及 `sift ps` / `sift doctor` 标记。唯一 canonical markdown 包含前五类信息，却完全没有 `sift ps` 或 `sift doctor`。因此 storage 的 sole exact fixture 与 channel 的 mandatory minimum 直接冲突；实现照 fixture 与照 channel 均有依据，closed payload 仍不可作为唯一验收 bytes。

**关闭条件：** 在 deterministic renderer 的持久化安全输入中冻结所需诊断指引，并更新 canonical markdown、payload digest；或明确修改 Channel 的最小正文契约。两处必须逐字节一致。

### MCH3（P1）：reclaim 到达 `max_attempts` 时仍被要求创建下一 attempt

Channel/outbox/storage 已一致授权 `ClaimOutboxOperation` reclaim 分支与 `CompleteOutboxAttempt` 为仅有 episode 写端口，并要求 reclaim 的 immutable `lease_expired` result、episode/alert 投影和 lease CAS 原子提交；要求的 expiry/restart/stale vectors 也已出现。这关闭了前次“没有合法写者”的原问题。

但 active [`outbox.md` §4](../specs/outbox.md#4-worker-状态机) 仍无条件要求通用 reclaim 在写旧 attempt 的 `lease_expired` result 后写新 lease、`attempt_count+1` 并创建新 attempt；[`channel.md` §4](../specs/channel.md#4-重试failure-episode-与-forge-告警) 也要求 reclaim 的阈值更新与“新 attempt lease”同 CAS。与此同时 [`outbox.md` §4.1](../specs/outbox.md#41-退避) 与 Channel 同节规定 `max_attempts>0` 达限必须 `failed`。若过期 attempt 正是最后一次，reclaim 写入 `lease_expired` 后既应 terminal，又被要求创建超限 attempt。现有 §6.6 只有普通 completion 达到 `max_attempts=3` 的 terminal vector，没有仲裁 lease expiry 达限分支。

**关闭条件：** 冻结 reclaim result 使 attempt 达限时的单一 CAS 分支：写 immutable `lease_expired`、终结 operation/delivery/episode、按阈值创建告警，但不创建新 lease/attempt；并增加 expiry 恰好达限及 stale old completion 的 vector。未达限时才原子创建下一 attempt。

## 3. 核销表

| 项 | 判断 | 说明 |
|---|---|---|
| MCH1-A 完整 Forge target batch identity | **NO** | daily grammar 已补、interrupt 旧键已删；异 target exact vector和 critical exact identity 仍缺。 |
| MCH1-B closed `forge_alert` payload/digest | **NO** | schema 与 digest 可复算，但唯一 canonical markdown 缺 mandatory `sift ps` / `sift doctor` 内容。 |
| MCH2 secret/target/endpoint 模型 | **YES** | handle schema、resolver owner、rotation、失败分类及 payload/digest 安全边界未回退。 |
| MCH3 reclaim sole-writer 与 recovery vectors | **NO** | sole-writer 原问题已关；但 expiry 达 `max_attempts` 的 terminal CAS 与无条件新 attempt 相冲突。 |

## 4. 验收检查清单

- [x] 在检测到的 GitHub forge 获取并阅读 #430 全文、Agent 建议、范围、依赖与约束
- [x] 获取并阅读 #430 comments（当前无评论）
- [x] 获取并核对 #426 全文、评论及 PR #429 合入基线
- [x] 对照前次复审剩余 MCH1-A、MCH1-B、MCH3 逐项复审
- [x] 交叉核对 active config/interrupt/outbox/storage
- [x] 复算 batch 与 `forge_alert` canonical payload SHA-256（均匹配）
- [x] 完整 Forge target 无碰撞且异 target exact vector 唯一：**NO**
- [x] `interrupt.md` 旧 daily/critical literal key 与旧无条件重试表述已消除：**YES**
- [x] `forge_alert` 有必填 markdown 的 closed payload/digest：**NO**（有 payload/digest，但与 mandatory markdown minimum 冲突）
- [x] reclaim 有唯一 episode 写端口及 expiry/restart/stale vectors：**NO**（写端口已统一，达限分支未闭合）
- [x] MCH2 未回退：**YES**
- [x] 只新增评审报告，未修改规格或实现：**YES**
- [x] 报告首行为 `FAIL`：**YES**
- [x] `channel.md` 保持 `status: draft`：**YES**
- [x] 遗留 P1：**3**
