---
status: done
created: 2026-07-30
summary: S2：配置生命周期、SIFT_HOME、Agent 定义校验与启动探测
---

# S2 — M1 §1.4 配置与启动生命周期

回链：[WBS M1 §1.4](../WBS.md)，[config.md](../specs/config.md)（active）。

## 目标

- 统一 `SIFT_HOME` 路径解析与目录/文件权限校验（config.md §2）
- 全局配置 closed-contract schema + 全量零配置默认值（§3、§6）
- Agent 定义 schema 与多 Agent 校验（id 唯一、引用一致、max_concurrent 继承）
- 敏感配置启动期一次读取、canonical 指纹；运行期漂移只告警不生效（§4）
- 调度硬护栏：未知 Agent 拒绝、按 Agent `max_concurrent`、`max_concurrent_total`、项目互斥钩子
- 启动探测分级框架：进程级失败拒启；项目级失败仅隔离并告警一次（§5）
- V12：文件缺失健康 idle；最小可调度配置所有可选值均有默认

## 纳入

- `internal/config`：closed 契约类型、YAML→JSON 严格桥、normalize、指纹、load、drift、probes、guard
- `internal/contract/genschema` 增补 `RawConfig` schema 生成（CI schema-drift 覆盖配置）
- V14 风格 golden + V12 验收测试

## 不做

- SQLite 接线、状态机、控制面、Brain 调用、Forge 适配、fake 链（后续切片）
- 配置热加载（V0 明确不做）
- 自动修正过宽权限（spec 要求拒启而非修复）

## 依据

- DESIGN §5.2（单一 decode gateway）、§14.2（开放数值默认）
- config.md §1–§8

## DoD

- `CGO_ENABLED=0` 可构建；`go vet`/`go test ./...` 通过
- 文件缺失与最小配置两种 V12 场景均通过；默认值表缺项即失败
- closed 契约拒绝未知/缺失/类型/枚举变型
- 漂移重算 hash 不同只产生一次告警，不改变有效配置
- 进程级探测任一失败拒启；项目级探测只隔离失败项目

## 完成

- `internal/config`：closed 契约 `RawConfig` + 全量 `DefaultConfig`、严格
  YAML→JSON 桥（拒重复键/非字符串键/alias 环/多文档）、`Normalize`（全字段
  范围与交叉校验、多 Agent 唯一性、`max_concurrent` 继承）、canonical JSON +
  SHA-256 指纹、`Load` 单一入口、warn-only `DriftChecker`、两级 `Probe`
  框架、`Guard` 调度硬护栏。
- `internal/contract/genschema` 增补 `RawConfig` schema 生成
  (`raw_config.schema.json`)，CI `schema-drift` 覆盖配置契约。
- 新增依赖 `gopkg.in/yaml.v3`（纯 Go，`CGO_ENABLED=0`）。
- V14 风格 closed golden + V12 两种场景 + 全默认断言；darwin/linux ×
  arm64/amd64 四组合构建通过；`-race` 通过。
- 后续切片接线 SQLite config_snapshots 写入、socket/单实例锁、Forge CLI 探测。
