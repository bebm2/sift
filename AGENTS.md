# AGENTS.md — Sift 代理导航

## 先读

- 文档地图、命名约定、上下文加载规则：[docs/README.md](docs/README.md)

## 项目现状

M1 已有 Go 实现：三个命令、SQLite/状态机/outbox 骨架、配置与控制面、Brain T1/T2 调用壳及 fake 骨架链均已有代码和测试；五份基础规格（`config`、`storage`、`control-plane`、`outbox`、[`brain`](docs/specs/brain.md)）保持 `active`。产品需求：[docs/PRD.md](docs/PRD.md)；架构与工作基线见 [docs/DESIGN.md](docs/DESIGN.md) / [docs/WBS.md](docs/WBS.md)。

[S1/M1 阶段审计](docs/reviews/2026-07-29-s1-m1-phase-review-pi-gpt-5.6-sol.md) 当前结论为 **FAIL**：M1 `doctor` 基线仍待实现（#29）；Linux listener 成功记录仍需留存后再复审。Intake 评论 crash marker 与旧 generation 回复仲裁已正式归属 M2，Brain replay JSONL 保留为 M1 证据。当前下一步是关闭 M1 剩余阻断并重新审计，不得把已有骨架描述为 M2 Intake 已实现。

## 上下文规则（摘要）

- 默认只加载 `status: active | draft` 的文档；`done / abandoned / superseded`、`reviews/`、`CHANGELOG.md` 仅回溯类任务加载。
- 不全量加载 `docs/`；按 docs/README.md 的「按任务类型的默认上下文集」表选读。
- 引用不复制：事实只写一次，其余地方链接。
