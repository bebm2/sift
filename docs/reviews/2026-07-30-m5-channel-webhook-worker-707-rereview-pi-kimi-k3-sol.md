# M5 #707 Channel webhook after #701 定向复审

> 日期：2026-07-30
> 评审人：pi × Kimi-k3（Sol 角色；Codex Sol 配额耗尽，按指挥要求改用 Kimi）
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#701 / PR #703，实现提交 `086a167725375b0a09ccbef7d106b6184bca47bc`，合入提交 `3be4017`
> 评审基线：`main` / `origin/main` `ad3485a`
> 判定基准：[#695 FAIL](2026-07-30-m5-channel-webhook-worker-695-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §2、§10](../specs/outbox.md)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)、[`WBS.md` M5 §5.2](../WBS.md#52-interrupt-全功能与-channel)

## 1. 结论

**FAIL。** #701 交付的三格测试均为真实增量且全部通过：

1. `TestAlertWorkerRetryReclaimUsesDistinctStableChargeKeys`（`internal/forgeworker/alert_test.go:120`）补上了 #695 判定为 NO 的“显式 retry/reclaim charge-key 回归”：第一个 `AlertWorker`（`alert-1`）在远端评论成功后 completion 崩溃，attempt 保持 leased；lease（1s）到期后第二个 worker（`alert-2`，`alertNow+2000`）reclaim 同一 operation 并再次走 GitHub production adapter。断言两次 attempt 各产生 lookup/comment 两个 charge key，且第二个 base 匹配 `forge-call:[0-9a-f]{32}`、与第一个 base 不同、各自带稳定 `:1/:2` 序列。这正是 #695 要求的“across retries”专门证据，可判 **YES**。
2. `TestReportQuotaExhaustionProducesFrozenChannelDelivery`（`internal/storage/channel_closure_test.go:10`）覆盖 Report 打扰额度耗尽 → interrupt → outbox `channel_publish` 段，断言冻结进 payload 的 Channel `{id:"ops", target_ref:"secret_ref:OPS", type:"webhook"}` 及 `delivery_id/interrupt_id/nonce` 完整，且只持久化 `secret_ref:` handle。
3. `TestSealedBatchPayloadDigestAndAuthoritySurviveReseal`（`internal/storage/channel_closure_test.go:254`）证明 sealed batch 的 `payload_json/payload_digest/operation_key` 在第二次 `PrepareDueAttentionBatches` 后逐字节不变，且 `payload_digest == digestJSON(payload)`，关闭了 reseal 不可变性这一 sealer 子项。

但 #701 的 Must close #2 要求继续关闭 #695 所列 wave-1 缺口中的 authority 完整矩阵与 restart 投影，本次两文件 diff（+109 行，纯测试）均未触及：collision/并发/upgrade/restart 矩阵、DB reopen 后 episode/delivery/alert 投影、`ops.ps`/`ops.doctor`、GitLab production 格、issue/change × production-adapter marker replay 矩阵、Channel webhook 单条+batch exact replay、§6.6 完整 failure episode 矩阵均无增量。Report→Channel 纵向也只接到 outbox payload 为止，seal → wake → webhook worker → completion 的 production 纵段仍未连成。

故 #707 Must verify 第 1、2 项为 YES，但 wave-1 Channel scope 清单仍多项 NO，整体不能通过。

本轮只新增评审报告，不修改实现、规格或 WBS。

## 2. #707 Must verify

### 2.1 显式 retry/reclaim charge-key 稳定性测试：YES

测试使用 `forge.NewProductionAdapter`（GitHub）与 budget-enforcing 调用链，第一次 attempt 的 `Complete` 注入错误模拟“远端已送达、本地 completion 崩溃”，第二个 worker 在 lease 到期后 reclaim。断言：首 attempt 恰 2 个 charge key 且 `:1/:2` 同 base；reclaim 后恰新增 2 个，新 base 为 32 位 hex attempt id、与旧 base 不同、序列同样单调。与 `alert.go:65` 的 `forge.WithChargeKey(ctx, "forge-call:"+c.AttemptID)` 安装点及 outbox §2 的 `forge-call:<outbox_attempt_id>:<call_seq>` 一致。

限制：仅 GitHub 格；未断言第二个 attempt 最终 operation state（RunOnce 无错误已隐含 completion 成功）；GitLab 双 Forge 格仍缺。

### 2.2 Report→Channel 纵向 / sealer 覆盖（来自 #701）：YES（有限制）

- Report→Channel：新测试从 `seedReportQuotaRun` 起走真实 `RecordReport`，第二次 blocker 触发 `report_interrupt_quota_exhausted`，并验证 outbox 中 `channel_publish` payload 携带冻结 Channel 与完整 delivery 身份。该纵段为真，但止于 outbox payload——sealer 冻结后的 webhook worker 投递、marker/completion 不在本格内，完整 P1-1 production 纵向仍 NO。
- Sealer：reseal 不可变性已证（payload/digest/operation_key 稳定、digest 自洽）。但 #695 P1-2 要求的两成员 production exact sealing、`ae3dba99…`/`ba180536…` canonical 向量与同 key/bytes response-loss replay 未交付，P1-2 整体仍 NO。

### 2.3 wave-1 Channel scope 清单：见 §3

### 2.4 判定：FAIL（见 §6）

## 3. wave-1 Channel 清单

严格按完整 wave-1 交付记 YES/NO，不以局部实现替代纵向验收：

| 检查项 | YES/NO | 复审结论 |
|---|---|---|
| P1-1 Report quota → frozen Channel → batch/seal/wake/webhook/completion production vertical | **NO** | #701 接通 Report quota → frozen Channel → `channel_publish` outbox payload 段；seal/wake/webhook/completion production 纵段仍未连成。 |
| P1-2 两成员 production exact sealing、`ae3dba99…`、canonical `ba180536…` 及同 key/bytes response-loss replay | **NO** | reseal 不可变性已证；exact canonical 向量与 replay 未交付。 |
| P1-3 batch authority、collision、并发、upgrade/restart 及 terminal 完整矩阵 | **NO** | 既有 retarget/terminal guard 保留；#701 无 collision/并发/upgrade/restart 增量。 |
| P1-4 nonterminal/terminal wake + 可工作的 purpose-filtered production alert consumer | **YES** | #677 wake 与 #689 charge base 保留；本轮 reclaim 路径再获验证。 |
| P1-4 failure/reclaim/restart/double-worker/success/alert-failure 完整矩阵 | **NO** | #701 补 reclaim charge-key 一格；restart/double-worker/alert-failure 等仍缺。 |
| P1-5 DB reopen 后 episode/delivery/alert 投影及 `ops.ps`/`ops.doctor` | **NO** | 无 diagnostics、controlplane 或 CLI 增量。 |
| P1-6 `secret_ref:` handle-only、安全错误摘要与非递归边界未回退 | **YES** | 新测试仅断言 `secret_ref:OPS` handle 冻结进 payload；resolver/payload 边界未回退。 |
| webhook adapter 只消费 closed sealed handle 且不改领域事实 | **YES** | 既有边界未回退。 |
| 单条与 batch 完整 at-least-once/replay 验收 | **NO** | 仅 Forge alert issue marker replay 与既有 webhook fixture；Channel webhook 单条+batch exact replay 未闭合。 |
| batch 单一 verified Forge target 与不可 retarget 完整验收 | **NO** | 既有 guard 保留，完整 authority 矩阵仍缺。 |
| durable episode 阈值唯一 alert、terminal 停止、success 清零与 alert 失败不递归完整验收 | **NO** | consumer 可运行且 reclaim 安全，完整 §6.6 矩阵仍未闭合。 |
| `ps`/`doctor` 重启后显示 delivery/episode/alert/generated-not-delivered | **NO** | 无 operator-surface 验收。 |
| sealed payload 不改写、不重复收费；升级沿原 Channel；无第二 Channel/TTS/Brain 旁路 | **NO** | “sealed payload 不改写”子项本轮已证；不重复收费/升级连续性/无旁路等子项未闭合，整项仍 NO。 |
| WBS M5 §5.2“实现首个 Channel；连续失败 N 次转 Forge 告警并在 ps/doctor 显示” | **NO** | Forge 告警 consumer 跨 reclaim 收费已证，但 production 纵向及 ps/doctor 仍未闭合。 |

## 4. 执行证据

- `git show 086a167 --stat`：仅 `internal/forgeworker/alert_test.go`（+41）与 `internal/storage/channel_closure_test.go`（+68），共 +109 行，纯测试，无 migration。
- `go test ./internal/storage/ ./internal/forgeworker/ ./cmd/siftd/ -count=1`：通过。
- `go vet ./internal/storage/ ./internal/forgeworker/ ./cmd/siftd/`：通过。
- `go test -race ./internal/forgeworker -count=1`：通过。
- `go test ./internal/forgeworker -run TestAlertWorkerRetryReclaimUsesDistinctStableChargeKeys -count=20`：通过。
- `go test ./internal/storage -run 'TestReportQuotaExhaustionProducesFrozenChannelDelivery|TestSealedBatchPayloadDigestAndAuthoritySurviveReseal' -count=20`：通过。
- #701 comments 仅两条 “Merged via PR #703.” 通知；无单列 Agent 建议。
- 工作区在写报告前无未提交改动；branch 为 conventional worktree branch（`feat/issue-707-rereview-channel-webhook-after-701`），未 push/MR/merge。

## 5. #707 验收清单

- [x] 获取并阅读 #707 全文、Must verify、Constraints（无 comments）：**YES**
- [x] 获取并阅读 #701、#695、#689 及 comments：**YES**
- [x] 检测 Forge 并使用 `gh`：**YES**
- [x] 显式 retry/reclaim charge-key 稳定性测试存在且有实效：**YES**
- [x] #701 的 Report→Channel 纵向 / sealer 覆盖核实：**YES**（纵段止于 outbox payload；sealer 为 reseal 不可变，均有限制）
- [x] wave-1 Channel scope 清单逐项 YES/NO：**YES**（见 §3）
- [ ] wave-1 Channel scope 关闭：**NO**
- [x] 仅写 `docs/reviews/`：**YES**
- [x] 未 push/MR/merge：**YES**

## 6. 最终裁决

**FAIL。** #701 有效关闭了 #695 点名的 retry/reclaim charge-key 回归缺口，并为 Report→Channel outbox 纵段与 sealer reseal 不可变性补了真实测试；#707 Must verify 第 1、2 项均可判 YES。但 #701 自身 Must close #2 要求的 authority 完整矩阵与 restart 投影未交付，wave-1 Channel scope 仍有八项 NO（完整 production 纵向、exact canonical/replay、authority/collision/并发/restart 矩阵、P1-5 重启投影、双 Forge 格、单条+batch exact replay、§6.6 完整 episode 矩阵、ps/doctor operator 验收）。剩余最高优先阻断：P1-1 全段 production 纵向、P1-3/P1-5 的 authority 与重启投影矩阵。
