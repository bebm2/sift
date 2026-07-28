---
status: done
created: 2026-07-29
summary: S2：Brain 统一调用壳、T1/T2 契约、Task Spec 组装与 fake provider（issue #20）
---

# S2 — M1 Brain 调用壳 T1/T2 + fake provider fixtures

回链：[WBS M1 §1.7](../WBS.md)、[specs/brain.md](../specs/brain.md)、issue #20。

## 目标

- 统一调用壳：发起前门禁 → provider 子进程 → closed decode → 同 prompt 重试一次 → 触点兜底
- 提示词与 schema 版本化入 git；完整 call/attempt trace；token 收费口
- T1/T2 契约与确定性兜底；Task Spec v1 组装
- fake provider 与 fixture/子进程测试；M1 fake 链合法 T2 输出

## 交付

- `internal/storage/brain.go`：`ReserveBrainCall` / `RecordBrainAttempt`（token post-charge + 越界 forge_alert 稳定 key 去重）/ `FinalizeBrainCall`，trace 读端口与 `ExportBrainCallsJSONL`（单 record 携有序 attempts）
- `internal/decode`：`sift` tag 约束词表（`minbytes/maxbytes/minitems/maxitems/itemminbytes/itemmaxbytes/keyrequired`）、`NullString`、`RejectDuplicateKeys`、`Canonical`
- `internal/contract/schemagen`：schema 生成器抽取为库（既有 golden 零漂移），genschema 瘦身为薄壳
- `internal/brain`：`prompts/T1,T2/v1.md` + 生成式 `v1.schema.json`（drift 测试）、claude-json-v1 open-envelope 适配（usage 缺失/非法计费拒绝）、子进程 provider（stdin、空临时 cwd、env allowlist、stdout 上限杀进程、stderr 去凭据 4096 上限）、统一 `Shell`（全局串行、每 attempt 门禁、崩溃收敛 `RecoverRunning`）、T1/T2 契约与兜底、`AssembleTaskSpec`、`FakeProvider`（`ValidT2ResultText` 供 M1 fake 链）
- 测试：fixture CLI（helper 子进程 + testdata 驱动）覆盖 valid first / invalid→valid / invalid→fallback / timeout / nonzero exit / oversize / usage missing / usage invalid / spawn failed；token 越界只发 attempt 1、全额 post-charge、跨 UTC 日界按 attempt 开始冻桶、零 usage 无 entry、重复 operation key 不重复收费

## 不做（留给后续片）

- `CommitT2Assignment`（hitl=true 的 Interrupt 单事务）——Interrupt 发射核心在 M3
- intake 投影写端口与完整 Intake 轮询——M2
- T3–T7 schema——随对应里程碑增补

## DoD

- [x] schema 失败→同 prompt 重试一次有测试（含两次 attempt 字节级相同 prompt 与 digest 断言）
- [x] 逐触点兜底 + trace + token 收费有测试
- [x] Task Spec 四段来源/hash 可重建
- [x] `CGO_ENABLED=0 go test ./...` 通过；`go generate ./...` 无漂移
- [x] 仅 feature worktree 提交
