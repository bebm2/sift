---
status: done
created: 2026-07-29
summary: S1：创建 Go module 与 decode gateway/CI 首切片
---

# S1 — M1 bootstrap + decode

回链：[WBS M1](../WBS.md) 前置「创建单 Go module」与 §1.1。

## 目标

- 建立单 Go module 与 `siftd` / `sift` / `sift-agent-wrapper` 可构建骨架
- 落地单一 decode gateway（closed / open-envelope）、schema 入 git、V14 golden、四组合 `CGO_ENABLED=0` CI

## 纳入

- W-M1.0 module/commands 骨架
- W-M1.1 §1.1 decode gateway / schema / CI / V14

## 不做

- SQLite / 状态机 / 控制面 / Brain / fake 链（后续切片）
- 放宽 V14 或伪造 CI 绿

## DoD

- `go.mod` 存在；三命令 `CGO_ENABLED=0` 可构建
- decode 契约有测试；schema 漂移使 CI 失败
- CI 覆盖 darwin/linux × arm64/amd64 构建段（V15 构建段起步）

## 完成

- W-M1.0：Go module 与三命令骨架（#8）。
- W-M1.1：`internal/decode` 单一 gateway（`Closed`/`OpenEnvelope`），
  `internal/contract` 种子边界类型与 V14 golden，`genschema` 生成 JSON Schema
  入 git，CI `schema-drift` + `build-matrix` 四组合 `CGO_ENABLED=0`。
- 依据 DESIGN §5.2。后续切片为各消费者（config/控制面/Brain/Forge）接线
  各自 closed/open-envelope 类型，不在本片范围。
