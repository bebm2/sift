---
status: done
created: 2026-08-10
summary: M8 发布自动化核心实现计划（归档/安装/握手、托管、文档、doctor 终态）
---

# M8 发布实现计划

> 父级：WBS §8（M8 发布）。本计划只记 M8 **自动化核心**的执行波次与边界；非范围/门禁见正文。

## 背景与诚实边界

- M1–M6 已 ✅ 完成；M7 PoC 已验证（PR #902 双 forge 端到端），但 **M7 门禁未通过**：剩余项（并发 ≥3 真实 Run + P50<60s、手机端审批证据、凭证存储 spike）全部 **Human-Gate 阻塞**；#883 明令"无真实负载不提前做"。
- 本计划启动 **M8 实现**（发布基础设施），因：二进制面已稳定（3→2，#900）、发布脚手架为绿地、且 A10 干净机验收的前置（归档/安装/托管）必须先存在。
- **不伪造、不跳门禁**：本次**不声称 M7 通过**；M8 **门禁** A10（干净机安装）= Human Gate，推迟到 M7 通过后。现在做的是"未被阻塞的下游实现"，不是跳过 M7。

## 范围

纳入（WBS §8.1–§8.4 可自动化部分）：
- §8.1 release 归档流水线（GoReleaser + 版本目录 + 原子 `current` + 握手可见性）
- §8.2 托管单元（launchd/systemd/foreground + 自启 + 原子升级重启 + Homebrew formula）
- §8.3 发布/安装/运维文档（dev/release.md、guides/installation.md、runbooks/troubleshooting.md）
- §8.4 doctor 终态（§8.10 全集 + TM6 逐条暴露面 + 版本不一致报告 + 平台分行）

不做（Human Gate / 需 M7 真实数据）：
- A10 干净 macOS + systemd Linux 安装验收证据（推迟到 M7 通过后）
- live 跨版本 DB 升级不丢数据（需 M7 真实数据；只做契约级回归）
- Homebrew tap 实际发布（需 GitHub Release 产物）

## Issue 分解与波次

| Issue | 标题 | agent::* | 依赖 | 波次 |
|---|---|---|---|---|
| #903 | release archive + install/version-dir + handshake (§8.1) | deepseek-v4-flash | 无（关键路径） | W1 |
| #904 | doctor final posture §8.10 + TM6 + version-mismatch (§8.4) | gpt-5.6-terra | 无 | W1 |
| #905 | hosting units launchd/systemd/foreground + Homebrew (§8.2) | glm-5.2 | #903 | W2 |
| #906 | release/install docs (§8.3) | gpt-5.6-sol | #903 + #905 | W3 |

- W1：#903 // #904 并行（无文件重叠：#903 = 发布工具+安装+cmd 版本；#904 = internal/controlplane/doctor.go）。
- W2：#905（依赖 #903 的安装/版本目录路径）。
- W3：#906（描述 #903+#905 落地的实际过程）。

## 进度表

| IID | 分支 | cli | 实施 | 审核 | MR | 状态 | 阻塞 | review_round | same_p1_streak |
|---|---|---|---|---|---|---|---|---|---|
| 903 | (squashed) | gh | deepseek-v4-flash/high ✓ | gpt-5.6-terra R1→R'PASS | #907 ✓ | ✅ 合并 | — | 2 | 0 |
| 904 | (squashed) | gh | terra✓→luna B1✓→deepseek B2✓→K3 B3✓→terra B4✓→terra B5✓→K3 B6✓ | gpt-5.6-sol R1→R'''''PASS(7轮) | #908 ✓ | ✅ 合并 | — | 7 | 0 |
| 905 | (squashed) | gh | glm-5.2✓→deepseek B1✓→deepseek B2✓→glm-5.2 B3✓ | gpt-5.6-luna R1→R''PASS(3轮) | #909 ✓ | ✅ 合并 | — | 3 | 0 |
| 906 | (squashed) | gh | gpt-5.6-sol✓ | deepseek R1→R'PASS(2轮) | #910 ✓ | ✅ 合并 | — | 2 | 0 |

## 执行注记

- **main CI 当前为红**（8bb8b93 `fix(wrapper): run.sock 4x-Dir` 改了生产布局但遗留 5 个测试夹具在旧 3x-Dir 布局，CI 上 `TestPausedExecutionWrapperRecoveryDoesNotOverlapOwner`/`TestBackendV2OwnerReplacementBarriers`/`TestLaunchWorkerWrapperCrashSuite` 等失败）。#903 的 `d2f6aef` 隔离提交修复该破坏（夹具对齐 4x-Dir，**无断言放宽**，production 未动），优先合 #903 可解红 main。
- #903 实施自报诚实缺口：CI `release-smoke` job 未在本机跑（无 GH runner）；0.x→1.x 握手 major 比较已在 spec 标注。
- #903/#904 均改 `internal/controlplane/doctor.go`（前者加版本报告、后者加 §8.10 姿态）——合并时后者需 rebase 解 doctor.go 冲突。

## 审修闭环（强制）

实施(A) → 审核(R) 出关闭包（`[P0]/[P1]/[P2]/[DEFER]` + Scope summary）→ 实施(B) 按包修 → 复审(R' 只验未关项)。只实施 P0/P1；未全关禁止 Closes/合并。审核 worker 独立 `chore/issue-<iid>-review` worktree，不改业务代码。

## DoD（M8 自动化核心）

- GoReleaser 干跑产出 4 单归档（各含 sift+sift-agent-wrapper + manifest + sha256）。
- 版本目录 + 原子 `current` symlink；`sift --version` 报 release 版本；握手不一致 doctor 报错。
- launchd/systemd/foreground 三路径安装 + 自启 + 升级重启可验证。
- doctor §8.10 全集 + TM6 逐条 + outbox/推送故障 + 平台分行 + exit 0/1/2 契约。
- 三份发布文档与实现对齐（引用不复制）。
