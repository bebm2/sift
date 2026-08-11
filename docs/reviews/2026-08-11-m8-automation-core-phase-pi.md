# M8 自动化核心 — 阶段完成报告

> 日期：2026-08-11 ｜ 审核方：Pi（总指挥）｜ 范围：M8 §8.1–§8.4 自动化核心
> 前置：[M8 执行计划](../plans/2026-08-10-m8-release-impl.md) ｜ 权威：[WBS §8](../WBS.md)

## 结论

**M8 自动化核心 COMPLETE**：§8.1–§8.4 全部合入 main（#907/#908/#909/#910），CI 四组合构建 + schema-drift + release-snapshot-smoke + vet+test 全绿。

**不声称**：M8 门禁未闭合——A10 干净机验收 + live 跨版本升级属人工门禁（待 M7 通过后）；M7 门禁仍开放（并发/手机/凭证 Human Gate）。本次只完成 M8 **实现**，不是"M8 通过"或"PoC 发布通过"。

## 交付物（issue → PR → squash commit）

| § | Issue | 实施 | 审核(轮次) | PR | main commit |
|---|---|---|---|---|---|
| 8.1 release 归档+安装+握手 | #903 | deepseek-v4-flash | gpt-5.6-terra (R1→R' PASS) | #907 | 4cbef9d |
| 8.4 doctor 终态+版本/协议 fail-closed | #904 | terra/luna/deepseek/K3 多轮 | gpt-5.6-sol (R1→R⁽⁷⁾ PASS) | #908 | 23011de |
| 8.2 hosting 单元+Homebrew | #905 | glm-5.2/deepseek | gpt-5.6-luna (R1→R' PASS) | #909 | 1e8519b |
| 8.3 发布/安装/运维文档 | #906 | gpt-5.6-sol | deepseek-v4-flash (R1→R' PASS) | #910 | 4e99f7e |

## 关键产出

- **§8.1**：`internal/version`（release semver，ldflags 注入，与协议 `config.Version` 分离）+ `.goreleaser.yml`（四组合单归档 + manifest + sha256）+ `internal/install`（版本目录 + 原子 `current` symlink）+ `tools/release`；CI `release snapshot smoke` job。
- **§8.4**：DESIGN §8.10 全量检查 + TM6 逐条暴露面 + 平台分行 + exit 0/1/2；版本/协议不一致的 **fail-closed** 报告（CLI↔daemon binary-major、wrapper `--protocol-major` 实探测、**canonical SemVer 2.0.0 校验器**、负 `protocol_minor` 双端拒绝、response envelope/request-id/SemVer 校验、畸形 `exit_code` fail-closed）。
- **§8.2**：`internal/hosting`（GOOS 后端分发 + launchd/systemd/foreground + 原子 unit 写）+ `sift service install|uninstall|status|restart` + Homebrew formula 草稿 + `hosting-smoke.sh`（launchd 全路径：inode+live doctor RPC 证据；systemd 接入 CI Linux）。
- **§8.3**：`dev/release.md` + `guides/installation.md` + `runbooks/troubleshooting.md`（引用不复制 WBS/DESIGN/PRD/specs）。

## 重要发现（附带价值）

- **main CI 曾为红**：`8bb8b93`（run.sock 4x-Dir 修复）改了生产布局但遗留 5 个 crash-harness 夹具在旧 3x-Dir 布局，致 `TestPausedExecutionWrapperRecovery*`/`TestBackendV2OwnerReplacementBarriers`/`TestLaunchWorkerWrapperCrashSuite`/`TestProductionWrapper*` 在 CI 失败。#903 `d2f6aef` 对齐夹具（**无断言放宽**，production 未动）→ main 现已绿。
- **#904 经 7 轮审修**：版本/握手 fail-closed 报告是深子域，增量点修多轮（F1–F12）；最终 K3 一次性全面审计（canonical SemVer 校验器 + 负 minor + `runAttach` 同类漏洞）收尾。过程中发现并恢复了一处被放宽的断言（F9）——已修，无遗留放宽。

## 审修闭环纪律（强制）

- 实施(A)→审核(R)关闭包→实施(B)按包修→复审(R'只验未关项)；P0/P1 未全关不合并；审核不改生产代码后自审通过。
- 角色 isolation：每轮实施/审核/复审均不同 agent；#904 F1 同类 P1≥2 轮触发 switch；#905 launchd-证据 P1 同理。
- 全部 squash 合并；worktree/分支已 cleanup；进度表含 review_round/same_p1_streak。

## 遗留（非本阶段闭合，已记录）

- **M8 门禁**：A10 干净机验收（人工）；live 跨版本升级不丢 DB + 较新 schema 拒旧 daemon（待 M7 真实数据）。
- **backlog**：doctor `checks` id 升序（P2 F4）；GoReleaser v2 安装命令 / doctor 退出码双述（P2）；Homebrew tap 实际发布（需 GitHub Release）。
- **M7 门禁**：并发 ≥3 真实 Run + P50<60s、手机端审批、凭证 spike（Human Gate）。

## 下一候选

M7 剩余验收（人工前置）→ 满足后 M8 A10 干净机验收 → 进入【7 完成】（核对 PRD/WBS 退出条件，发布建议）。
