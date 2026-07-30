# M5 #782 Channel ops.ps/ops.doctor 端点级跨重启验收定向复审 (#782 / #783)

> 日期：2026-07-30
> 评审人：pi × DeepSeek V4 Pro（Sol role）
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#782 / PR #783，合入提交 `c7aa017`（feature tip `3c305c1`）
> 评审基线：`origin/main` @ `c7aa017` vs prior `a6b8db7`
> 判定基准：WBS §5.2 Channel 行残留缺口；[#715 PASS WITH NOTES](2026-07-30-m5-channel-webhook-worker-715-rereview-pi-kimi-k3-sol.md) §6 注记 4 / P1-5

## 1. 结论

**PASS。** PR #783 (`3c305c1` / merge `c7aa017`) 闭合了 #715 注记 4 指出的 `ops.ps`/`ops.doctor` 端点级（socket + CLI）跨重启验收缺口。无 P0/P1 finding。纯测试 PR（+393），零生产代码改动——wiring 经 `ChannelDiagnostics` 已正确。

## 2. 实现范围审查

| 文件 | 变更 |
|---|---|
| `internal/controlplane/ops_read_test.go` | +249：三 socket 验收测试 + 共享 seeding/assertion helpers |
| `cmd/sift/main_test.go` | +144：一 CLI online 端到端测试 + seeding helper |

### 验收证据

1. **`TestOpsPSExposesDurableChannelDiagnostics`** — 经 production write ports（`EnqueueChannelPublish` → `ClaimOutboxOperationKind` → `CompleteOutboxAttempt`）播种阈值失败 → alert → `ops.ps` 断言 delivery/episode/alert/`generated_not_delivered`。
2. **`TestOpsDoctorExposesDurableChannelDiagnostics`** — 同投影经 `ops.doctor`。
3. **`TestOpsPSDoctorChannelDiagnosticsSurviveDBReopen`** — DB close/reopen + 新 Server 绑定后两端点 `reflect.DeepEqual` 逐字节一致。
4. **`TestRunPSDoctorOnlineExposeChannelDeliveries`** — 真实 unix socket + 子进程 `sift ps`/`sift doctor` JSON round-trip。

## 3. Findings

| ID | 级别 | 摘要 | 处置 |
|---|---|---|---|
| F1 | P2 | CLI `startServerWithChannelFailure` 未 `t.Cleanup` 关闭 DB | backlog |
| F2 | P2 | 仅覆盖 `batch_deliveries`；`interrupt_deliveries` 未触达 | backlog / 可 DEFER |
| F3 | P2 | controlplane/CLI seed helper 近克隆 | backlog |
| F4 | P2 | CLI harness 缺 `run.sock` → doctor 噪声 | backlog |

## 4. Scope summary

| 级别 | 数量 | 本轮是否实施 |
|---|---|---|
| P0 | 0 | — |
| P1 | 0 | — |
| P2 | 4 | 否（记录） |
| DEFER | 0 | 否 |

## 5. WBS 诚实同步指引

- **可勾**：§5.2「实现首个 Channel；连续失败 N 次转 forge 告警评论，并在 ps/doctor 显示」——#715 闭合 production sealer/alert/replay；#782/#783 Sol PASS 闭合端点级跨重启验收。
- **不可勾**：§5.2 EmitInterrupt 接入行（**调度仍未闭合**）；Command `ErrCommandEffectNotWired`；§6.6 完整 failure-episode 矩阵；M5 门禁。
- **不声称** M5 已实现。不启动 #748+ code-opt。
