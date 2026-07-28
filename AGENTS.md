# AGENTS.md — Sift 代理导航

## 先读

- 文档地图、命名约定、上下文加载规则：[docs/README.md](docs/README.md)

## 项目现状

M1 规格阶段，尚无代码。产品需求：[docs/PRD.md](docs/PRD.md)；架构与工作基线见 [docs/DESIGN.md](docs/DESIGN.md) / [docs/WBS.md](docs/WBS.md)。

M1 五份基础规格（`config`、`storage`、`control-plane`、`outbox`、[`brain`](docs/specs/brain.md)）全部 `active`，尚无代码，可进入 M1 实现派工。

## 上下文规则（摘要）

- 默认只加载 `status: active | draft` 的文档；`done / abandoned / superseded`、`reviews/`、`CHANGELOG.md` 仅回溯类任务加载。
- 不全量加载 `docs/`；按 docs/README.md 的「按任务类型的默认上下文集」表选读。
- 引用不复制：事实只写一次，其余地方链接。
