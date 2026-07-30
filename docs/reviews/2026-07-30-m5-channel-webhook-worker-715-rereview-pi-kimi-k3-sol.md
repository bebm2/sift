# M5 #715 Channel webhook after #712 定向复审

> 日期：2026-07-30
> 评审人：pi × Kimi-k3（Sol 角色；Codex Sol 配额耗尽，按指挥要求改用 Kimi）
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#712 / PR #714，实现提交 `c8bd2933dc1dc5ba08d63974a393ebcbf7270c50`，合入提交 `c7a56f1`
> 评审基线：worktree `feat/issue-715-rereview-channel-webhook-after-712` @ `c7a56f1`
> 判定基准：[#707 FAIL](2026-07-30-m5-channel-webhook-worker-707-rereview-pi-kimi-k3-sol.md)、[#695 FAIL](2026-07-30-m5-channel-webhook-worker-695-rereview-pi-gpt-5.6-sol.md)、[`storage.md` §6.5–§6.6](../specs/storage.md)、[`channel.md`](../specs/channel.md)、[`WBS.md` M5 §5.2](../WBS.md#52-interrupt-全功能与-channel)

## 1. 结论

**PASS WITH NOTES。** #712 交付一格 656 行纯测试文件（`internal/storage/channel_p1_closure_test.go`），五条测试全部经 production 路径并逐字节命中 §6.6 canonical 向量：

1. `TestProductionSealerTwoMemberBatchHashesToExactFixtureDigest`（`:157`）以真实两名 member（i-a/i-b，authority 行齐备）驱动 production sealer `PrepareDueAttentionBatches`，断言 `operation_key == attention-batch:<batch_id>:publish:1`、`payload_digest == ae3dba99…` 且 `sha256(payload_json)` 逐字节等于该 digest、batch 进入 `sealed` 且两名 member 未排除。这是自 #553 起连续八轮 FAIL 点名的 production sealer exact 向量，本轮首次真实闭合。
2. `TestProductionChannelThresholdAlertHashesToCanonicalDigest`（`:198`）用真实第三次 `rate_limited` completion（前两次 `transient`）经 production `CompleteOutboxAttempt` 触发阈值，断言 alert payload 及持久化 digest 逐字节等于 canonical `ba180536…`，markdown 携带 subject marker/operation key/`Consecutive failures: 3`/`Latest error class: rate_limited`，episode 为 `alerted`、count=3、`alert_operation_key == alert:channel_failure:<subject>:1`。
3. `TestProductionChannelResponseLossReplayReturnsSameKeyAndBytes`（`:366`）模拟远端已送达、本地 completion 丢失：reclaim 后持久化 `operation_key`/`payload_json` 逐字节不变，第二次 claim 拿到同 key 同 bytes，success 后 delivery=`delivered`、attempt_count=2、无 alert。同 key/bytes response-loss replay 闭合。
4. `TestProductionChannelDiagnosticsSurviveRestart`（`:460`）在阈值 3/上限 3 下走完三次失败 completion，close/reopen DB 后 `ChannelDiagnostics` 十项键（含 `generated_not_delivered=true`、`alert_state=pending`）逐值一致，alert 与 episode 行各恰一行、reopen 不新建 alert。P1-5 reopen 投影在 storage 层闭合。
5. `TestProductionBatchCollisionDifferentHostResolvesToSeparateSealedBatches`（`:555`）证明同 project/target 下 `github.com` 与另一 host 解析为两个独立 sealed batch、独立 operation key/delivery，互不吸收。

#715 Must verify 第 1–3 项均可判 YES（限制见 §2），wave-1 Channel 清单中 P1-1、P1-2、batch 单一 target 不可 retarget 三行由 NO 翻为 YES。整体裁决为 PASS WITH NOTES 而非 PASS，原因见 §6 五条非阻断注记（纵向为分段 digest 链接而非单条端到端、collision 第二 host 身份非 canonical、member 为 SQL fixture、ps/doctor 端点级验收缺席、alert 泄漏断言无实际 endpoint）。本轮只新增评审报告，不修改实现、规格或 WBS。

## 2. #715 Must verify

### 2.1 P1-1 完整 Report→Channel production 纵向（seal/wake/webhook/completion）：YES（分段 digest 链接）

逐段核实：

- **Report quota → frozen Channel → payload**：#701 的 `TestReportQuotaExhaustionProducesFrozenChannelDelivery`（`internal/storage/channel_closure_test.go:10`）保留并通过，真实 `RecordReport` 触发 `report_interrupt_quota_exhausted`，冻结 `secret_ref:` handle Channel 进 `channel_publish` payload。
- **seal**：production `PrepareDueAttentionBatches`（`internal/storage/channel_batch.go:13`）本轮首次被 exact 向量测试驱动；sealer 在同一事务内 `insertOperation` channel_publish、写 `batch_deliveries` 投影、置 `sealed` 并 `wakeOutbox()`（`channel_batch.go:103–118`），wake 接线在 production 代码路径上。
- **webhook**：`channelworker.WebhookAdapter.Publish` 对逐字节等于 `ae3dba99…` 的 sealed batch payload 两次投递成功且校验 `[sift <operation_key>]` marker（`internal/channelworker/webhook_test.go:25`）；daemon 以 `EnvironmentSecretResolver`+`HTTPWebhookSender` 装配 `channelworker.Worker`（`internal/daemon/daemon.go:76`）。
- **completion**：production `CompleteOutboxAttempt` 成功原子标记 `batch_deliveries=delivered`、episode 关闭、attempt_count=2（replay 测试）。

限制：仍无单条测试把 sealer → `Worker.RunOnce` → adapter → completion 连成一次执行；四段由「同一 canonical bytes」密码学链接——sealer 输出、adapter 输入、enqueue payload 三者均哈希为 `ae3dba99…`（本评审在仓外用等价 map 重建 `mustSpecChannelPayload` 独立复算确认）。判 YES，注记保留。

### 2.2 P1-2 exact sealing digests + response-loss replay：YES

- sealer digest：payload 字节哈希、持久化 `payload_digest` 列、operation key 三重断言全部命中 `ae3dba99…`/`attention-batch:…:publish:1`。
- canonical alert：真实第三次 `rate_limited` completion 生成，`ba180536…` 同时按字节重算与读持久化列断言；episode/alert key/count 与 §6.6「third `rate_limited` completion」行一致。
- response-loss replay：同一 durable operation 上 lease 过期 → reclaim 同 key 同 bytes → success delivered，与 §6.5「响应丢失的重放沿用同一 operation key 和 frozen payload」一致。

限制：replay/alert 两格的 operation 由 production 端口 `EnqueueChannelPublish` 以 canonical-identical payload 入队，而非直接消费 sealer 自己写出的 outbox 行（两者字节相同，见 §2.1）。

### 2.3 P1-3/P1-5 authority/collision/restart + diagnostics reopen：YES（各带限制）

- **authority**：production sealer 经 `attention_batch_member_authority` JOIN 且要求 `i.version=a.interrupt_version AND i.nonce=a.nonce`（`channel_batch.go:51`）；#701 的 retarget/terminal guard 测试（`channel_closure_test.go:47–253`）保留并通过。
- **collision**：不同 host → 两个独立 sealed batch/key/delivery，无跨 host 吸收。限制：第二 host 身份非 canonical，见 §6 注记 2。
- **restart + diagnostics reopen**：`ChannelDiagnostics` 跨 close/reopen 十项键逐值一致；`generated_not_delivered=true`、`alert_state=pending`；alert/episode 行数不变。`ops.ps`/`ops.doctor` 均直接查询 `ChannelDiagnostics`（`internal/controlplane/server.go:262,289`），但端点级 reopen 验收测试缺席，见 §6 注记 4。

### 2.4 判定：PASS WITH NOTES（见 §6）

## 3. wave-1 Channel 清单

严格按完整 wave-1 交付记 YES/NO，不以局部实现替代纵向验收：

| 检查项 | YES/NO | 复审结论 |
|---|---|---|
| P1-1 Report quota → frozen Channel → batch/seal/wake/webhook/completion production vertical | **YES** | 四段均经 production 路径且由 canonical digest 逐字节链接；单条端到端连通测试仍缺（注记 1）。 |
| P1-2 两成员 production exact sealing、`ae3dba99…`、canonical `ba180536…` 及同 key/bytes response-loss replay | **YES** | 本轮闭合：sealer exact digest、真实第三次 rate_limited canonical alert、同 key/bytes replay。 |
| P1-3 batch authority、collision、并发、upgrade/restart 及 terminal 完整矩阵 | **NO** | authority/collision/restart 子项已证；并发同批插入竞争与 upgrade 连续性无增量；collision 第二身份非 canonical（注记 2）。 |
| P1-4 nonterminal/terminal wake + 可工作的 purpose-filtered production alert consumer | **YES** | 既有 wake 与 alert consumer 保留并通过。 |
| P1-4 failure/reclaim/restart/double-worker/success/alert-failure 完整矩阵 | **NO** | reclaim charge-key、replay success、terminal `ended_failed`、restart 重载已证；double-worker 竞争完成、lease 过期 reclaim 跨阈值、stale completion 拒绝、alert 自身失败不递归等 §6.6 行仍缺。 |
| P1-5 DB reopen 后 episode/delivery/alert 投影及 `ops.ps`/`ops.doctor` | **NO** | reopen 投影在 storage 层闭合且两端点均消费该投影；端点级（socket）验收缺席（注记 4）。 |
| P1-6 `secret_ref:` handle-only、安全错误摘要与非递归边界未回退 | **YES** | 新测试断言 alert payload 不含 resolver 细节；adapter 边界未回退。泄漏断言在 storage 层无实际 endpoint，强度有限（注记 5）。 |
| webhook adapter 只消费 closed sealed handle 且不改领域事实 | **YES** | 既有边界未回退；exact fixture 双重投递测试保留。 |
| 单条与 batch 完整 at-least-once/replay 验收 | **NO** | batch replay 本轮闭合；单条 interrupt channel delivery 的 response-loss replay 仍无增量。 |
| batch 单一 verified Forge target 与不可 retarget 完整验收 | **YES** | sealer 单 target authority + collision 分离 + retarget guard 共同闭合；并发竞争子项见上行。 |
| durable episode 阈值唯一 alert、terminal 停止、success 清零与 alert 失败不递归完整验收 | **NO** | 阈值唯一 alert 与 terminal `ended_failed` 本轮已证；success 清零/`ended_delivered`、alert 失败不递归行仍缺。 |
| `ps`/`doctor` 重启后显示 delivery/episode/alert/generated-not-delivered | **NO** | 投影层跨重启已证；operator 面端到端显示无验收（注记 4）。 |
| sealed payload 不改写、不重复收费；升级沿原 Channel；无第二 Channel/TTS/Brain 旁路 | **NO** | 「不改写」子项已证；不重复收费/升级连续性/无旁路子项未闭合。 |
| WBS M5 §5.2「实现首个 Channel；连续失败 N 次转 Forge 告警并在 ps/doctor 显示」 | **NO** | 首个 Channel 与阈值 Forge 告警均有 production 证据；ps/doctor 显示仅投影层证据，端点验收缺席。 |

## 4. 执行证据

- `gh issue view 715 [--comments]`：无 comments；已读 Goal/Must verify/Constraints。`gh issue view 712/707/701`：comments 仅「Merged via PR #7xx」通知，无单列 Agent 建议。
- `git show c8bd293 --stat`：仅 `internal/storage/channel_p1_closure_test.go`（+656），纯测试，无 migration、无 production 改动。
- `go test ./internal/storage/ -run TestProduction -count=1 -v`：5 条全部 PASS。
- `go test ./internal/storage/ ./internal/channelworker/ ./internal/controlplane/ ./internal/daemon/ ./cmd/... -count=1`：全部通过。
- `go vet ./internal/storage/ ./internal/channelworker/ ./internal/controlplane/ ./cmd/...`：通过。
- `go test -race ./internal/storage -run TestProduction -count=1`：通过。
- `go test ./internal/storage -run 'TestProduction…（五条）' -count=20`：通过，无 flake。
- 仓外独立复算：`mustSpecChannelPayload` 等价 map 经 `json.Marshal` 的 SHA-256 恰为 `ae3dba99…`，确认三格共用 canonical bytes。
- base64 核验：`Z2l0aHViLmV4YW1wbGUuY29t`=「github.example.com」，`Z2l0LmV4YW1wbGUuY29t`=「git.example.com」（注记 2 的事实基础）。
- 工作区写报告前 `git status` 干净；branch 为 conventional worktree branch（`feat/issue-715-rereview-channel-webhook-after-712`），未 push/MR/merge。

## 5. #715 验收清单

- [x] 获取并阅读 #715 全文、Must verify、Constraints（无 comments）：**YES**
- [x] 获取并阅读 #712、#707、#701、#695 及 comments：**YES**
- [x] 检测 Forge 并使用 `gh`：**YES**
- [x] P1-1 完整 Report→Channel production 纵向核实：**YES**（分段 digest 链接，注记 1）
- [x] P1-2 exact sealing digests + response-loss replay 核实：**YES**
- [x] P1-3/P1-5 authority/collision/restart + diagnostics reopen 核实：**YES**（注记 2、4）
- [x] wave-1 Channel scope 清单逐项 YES/NO：**YES**（见 §3）
- [ ] wave-1 Channel scope 全部关闭：**NO**（P1-3/P1-4 矩阵、P1-5 端点、单条 replay、episode 完整矩阵、无旁路等行仍 NO）
- [x] 仅写 `docs/reviews/`：**YES**
- [x] 未 push/MR/merge：**YES**

## 6. 最终裁决

**PASS WITH NOTES。** #712 的 Must close 三项（P1-1/2/3/5）均经 production 路径真实闭合：八轮 FAIL 反复点名的 production sealer exact 向量（`ae3dba99…`）、canonical alert 向量（`ba180536…`）、同 key/bytes response-loss replay、reopen diagnostics 投影与 host collision 分离本轮首次全部命中，#715 Must verify 第 1–3 项判 YES。以下注记非阻断，但须由后续实现/复审跟踪：

1. **纵向分段链接**：Report→Channel 全纵段由四段独立测试经 canonical digest 逐字节链接，仍无单条 sealer → `Worker.RunOnce` → adapter → completion 端到端测试；建议后续补一格连通验收。
2. **collision 第二身份非 canonical**：`channel_p1_closure_test.go:579` 的第二 batch id 嵌入 `Z2l0aHViLmV4YW1wbGUuY29t`（=「github.example.com」），与其 payload 的 `forge_host=git.example.com` 及 §6.6 canonical `Z2l0LmV4YW1wbGUuY29t` 不一致；production sealer 按 `enc([]byte(host))` 派生 id（`advance_interrupt.go:581`），绝不会为该 host 生成此 id。分离结论成立，但 §6.6 concurrent 向量的精确身份仍未被断言，且该测试未创建真实 i-c member（经 enqueue 端口造第二 sealed batch）。
3. **member 为 SQL fixture**：sealer 测试注释称「admitted through the production batch membership path」，实际 member/authority 行由 `insertSpecMember` 裸 SQL 插入，未走 `advance_interrupt.go:634` 的 production 入批路径；被测的是 sealer 的 production 读取/冻结路径，注释表述过强。
4. **ps/doctor 端点级验收缺席**：`ops.ps`/`ops.doctor` 均直接返回 `ChannelDiagnostics`（`server.go:262,289`），但无 socket 层跨重启验收测试；controlplane/CLI 测试中无任何 `channel_deliveries` 断言。
5. **alert 泄漏断言强度有限**：阈值 alert 测试在 storage 层无 resolver/endpoint 参与，`github.com/?token` 泄漏断言对该格近于空转；endpoint 不泄漏由 adapter 层既有测试承担。

剩余最高优先缺口（超 #712 范围）：§6.6 完整 failure-episode 矩阵（lease 过期 reclaim 跨阈值 CAS、terminal 过期 reclaim、stale completion 拒绝、double-worker、success 清零、alert 失败不递归）、单条 interrupt channel replay、并发同批插入竞争、GitLab 双 Forge 格、ps/doctor 端点级验收。
