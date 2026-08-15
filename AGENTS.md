# AGENTS.md — Sift 代理导航

## 先读

- 文档地图、命名约定、上下文加载规则：[docs/README.md](docs/README.md)
- 编码纪律（软规范）：[docs/dev/coding-constraints.md](docs/dev/coding-constraints.md)

## 项目现状

**权威进度与门禁裁决见 [docs/STATUS.md](docs/STATUS.md)**（随执行更新）；本节只给导航摘要，不复制进度叙事。产品需求：[docs/PRD.md](docs/PRD.md)；架构与工作基线：[docs/DESIGN.md](docs/DESIGN.md) / [docs/WBS.md](docs/WBS.md)。

- **M1–M6 全部闭合**（含 M6 tmux 双后端与 V2/V4 全矩阵，门禁存档 `docs/reviews/2026-08-04-m6-phase-gate-pi-minimax-m3.md`）。
- **M7 处于 PoC 已验证、完整门禁未过**：Pi 作为 Brain+Agent 已在 GitHub+GitLab 双 forge 端到端跑通（含 8 个真机 bug 修复）；剩余 ≥3 并行 Run、P50<60s、手机审批证据、凭证存储 spike 属**人工前置**。不得宣称 M7 通过或完整 PoC。
- **M8 自动化核心已合入**（#903–#911），Release 已迭代至 v0.5.x；A10 干净机验收与 live 跨版本升级属人工门禁，待 M7。
- **首跑旅程已两轮打磨并经真机验证**（#913–#995：安装器/init 向导/收尾三合一/`sift pi` 会话入口/真实 GitLab 全流程验证，v0.5.8–v0.5.11）。
- **当前 open issue 池**：#963（`sift issue` 语义入口 v1 只读，依赖已闭合待启动）、#927（技术债 consolidated backlog，按批取用）、#883（性能 profile，绑定 M7 并行 Run 片）。修复已合入但未收口的 issue 应验证后及时关闭，避免池子失真。
- 阶段门禁结论只能由独立复审 issue 产出；`active` 规格不代表对应运行时已实现。

## 上下文规则（摘要）

- 默认只加载 `status: active | draft` 的文档；`done / abandoned / superseded`、`reviews/`、`CHANGELOG.md` 仅回溯类任务加载。
- 不全量加载 `docs/`；按 docs/README.md 的「按任务类型的默认上下文集」表选读。
- 引用不复制：事实只写一次，其余地方链接。
