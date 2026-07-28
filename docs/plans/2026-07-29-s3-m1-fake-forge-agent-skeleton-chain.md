---
status: active
created: 2026-07-29
summary: S3：fake Forge/Agent 端口 + Brain fake provider 骨架链与 V9 首段 CI（issue #21）
---

# S3 — M1 fake Forge/Agent 骨架链 + V9 首段 CI

回链：[WBS M1 §1.6](../WBS.md)、[specs/brain.md](../specs/brain.md)、[specs/storage.md](../specs/storage.md)、[specs/outbox.md](../specs/outbox.md)、issue #21。

## 目标

- fake Forge、fake Agent 与 Brain fake provider（#20）实现同一端口契约；真实适配器（Forge CLI = M2、Runtime = M3）后续实现同一接口。
- 骨架链：fake Issue → T1/T2 → queued → fake attempt 完成证据 → 注入 fake forge「Change 已合并」→ done。
- 作为 V9 首段 CI 测试，非手工验证。
- 事件时间戳覆盖「可信触发标签观测 → Agent started」，为 P50（PRD §10.2）留 day-1 数据。

## 约束（issue #21）

- **不实现临时 Gate、不创建 Change、不保留旁路裁定。** M4 接入 Gate/Create Change 后替换测试夹具。
- `done` 收敛来自注入的 forge「Change 已合并」事实；按 PRD §4.1/§10.2 与 DESIGN §8.2，未跑门禁的 forge 合并诚实记 `gate_bypassed=true`（审计属性，非裁定）。
- `CGO_ENABLED=0 go test ./...` 通过。

## 交付

- `internal/forge`：最小动词集端口（issue / 标签事件 / change 读取，匹配 PRD §5.2、DESIGN §8.1 的中性类型与错误分类）+ `Fake` 内存实现。完整动词集与 CLI 适配器落在 M2 `specs/forge.md`。
- `internal/attempt`：Agent 启动与完成证据端口（DESIGN §8.4 wrapper 契约的 result.json 对应物）+ `FakeAgent`。
- `internal/storage`（M1 骨架桩写端口，§11 受限写端口的 M1 最小子集）：
  - `CreateForgeRun`：T1 ready → 创建 forge Run + intake 事件 + forge receipt（完整 `PersistIntakeDecision` 在 M2）。
  - `SetInitialTaskSpec`：T2 valid hitl=false → 初始 Task Spec snapshot + Run kind/agent + 事件（完整 `CommitT2Assignment` 含 Interrupt 在 M3）。
- `internal/skeleton`：`Chain` 驱动，串联 forge.Fake + brain.Shell(FakeProvider) + attempt.FakeAgent + storage，按序推进并打点 trigger→started 时间戳。
- `internal/skeleton` 测试：V9 首段 CI。

## 骨架链状态机

```
fake Issue(+可信触发标签)
  → forge receipt + intake.trigger_observed(T0)        [P50 起点]
  → T1(brain.Shell, FakeProvider) → ready
  → CreateForgeRun → Run(queued)
  → T2(brain.Shell, FakeProvider) → kind/agent/goals, hitl=false
  → SetInitialTaskSpec → Run(queued, kind/agent/spec)
  → FakeAgent.Launch → TransitionRun queued→running(T1) [P50 终点]
  → FakeAgent 完成证据(exit 0, head SHA) → attempt.completed 事件
  → forge.Fake 注入「Change 已合并」(change id + head SHA)
  → TransitionRun running→done(T2, gate_bypassed=true, ChangeID)
```

P50(day-1) = T1 − T0，两个事件时间戳可查。

## 不做（留给后续片）

- `specs/forge.md`、完整 Forge CLI 适配器、双平台 fixture —— M2。
- `PersistIntakeDecision`/`PersistIntakeBatch` 完整 intake 投影与回复 generation —— M2。
- `CommitT2Assignment` 的 `design_approval` Interrupt 单事务、attempt 启动协议（claim:acquire/permit/started）—— M3。
- Gate 评估、create_change/merge_change outbox、校准/Ledger —— M4/M5。

## DoD

- [ ] V9 骨架段 CI 绿（fake 链跑通 queued→running→done）
- [ ] 无临时 Gate / Create Change / 旁路裁定
- [ ] trigger→started 事件时间戳存在且 P50 可算
- [ ] `CGO_ENABLED=0 go build ./...` 与 `CGO_ENABLED=0 go test ./...` 通过
- [ ] 仅 feature worktree 提交
