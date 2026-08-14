---
name: sift
description: 操作 Sift 本地自动化工具：查询 run 状态、诊断问题、管理本地服务。当用户询问 sift 的 ps/timeline/logs/metrics/worktree/doctor/service 等操作，或需要排查 Sift 运行问题、解释 Gate/审批流程时使用。
---

# Sift 操作指南

Sift 把「Issue + 触发标签」推进为经过 Gate、必要时由人审批的 PR/MR：本机隔离 worktree → Coding Agent → Checks/Gate → PR/MR → 人工决定。Forge（GitHub/GitLab）是 Issue、Change、审批和合并的事实源；Sift 不托管代码，不监听网络端口。

## 核心概念（一行版）

- **Run**：一次 Issue 触发的完整执行（含若干 attempt）。用 `sift ps` 看到的 `<run-id>` 定位。
- **attempt**：一次 Agent 尝试；`max_attempts`（默认 3）内可重试。
- **Gate**：确定性策略检查；未知输入 fail closed。Gate 通过 ≠ 已自动合并（`auto_merge` 默认关）。
- **Change**：Agent 产出的 PR/MR。
- **approve**：需 Run ID + 一次性 nonce 的完整命令（见下方红线）。

## 只读命令（可自由使用，无副作用）

```bash
sift ps                 # 运行状态：queued/running/waiting_human/done/failed
sift ps -a              # 含已归档（rm 过的）run
sift timeline --limit 20          # append-only 事件流（可 --run <id> 过滤）
sift logs <run-id>      # Agent 尝试日志
sift metrics            # 派生指标（触发→started 延迟等）
sift report             # run.sock 进度报告
sift worktree <run-id>  # 该 run 的隔离 worktree 路径
sift doctor             # 环境诊断（0 无问题 / 1 warning / 2 error）
sift doctor --offline   # 不连 daemon 的只读诊断
sift status             # 本地配置/服务概览
sift agent list         # 已登记 Agent
sift project list       # 已登记项目
```

注意：`sift ps` / `logs` / `metrics` 等在线命令需要 daemon 运行；daemon 未启动时先 `sift service status`，失败则按排障节处理。

## 副作用命令（执行前必须向用户确认并说明后果）

```bash
sift kill <run-id>      # 停止活跃 Run；不自动回滚已推送的远端内容
sift retry <run-id>     # 终态 Run 重新尝试（先看 logs/timeline 找失败原因）
sift rm <run-id>        # 终态后从默认列表归档（历史保留）；rm -f 跳过终态检查
sift service restart    # 重启用户级 daemon 服务
```

## 红线（永不建议、永不帮用户绕过）

- **approve 命令**：必须原样复制 Sift 发布在 Issue/PR/MR 评论里的**完整命令**（含 Run ID + 一次性 nonce）。绝不手写 nonce、绝不从旧评论复用、绝不简写为 `/sift approve`。nonce 错误/过期会 fail closed。
- **auto_merge**：默认关闭是安全设计。不要建议用户开启，除非用户明确要求且已观察过真实 Agent/Forge 行为。
- **「修复」类危险动作**：绝不删除 `siftd.lock` / `siftd.sock` / `schema_migrations` / `sift.db` / token 来重试；迁移只前向执行，降级 binary 可能因 schema 较新拒启。
- **单 Coordinator**：同一仓库、同一触发标签只保留一个主动 daemon；不要起第二个实例抢同一 Issue 池（不是负载均衡，会重复 Run）。
- **凭证**：Sift 复用 `gh`/`glab`/Agent 的登录，不接管凭证；`/login` 与 API Key 由用户自己管理。

## 排障速查（症状 → 处置）

| 症状 | 处置 |
|---|---|
| Run 长时间 queued | daemon 是否在跑（`sift service status`）；label 拼写；是否已有第二个 coordinator |
| Agent 无输出 | `agent_silence_timeout`（默认 30m）未到属正常等待；超过再查 logs |
| attempt 失败 | `sift logs <run-id>` + `sift timeline --run <id>` 找原因，修环境后 `sift retry` |
| `daemon unavailable` | service status、`siftd.sock`、daemon 日志；先恢复 daemon 再跑在线命令 |
| doctor `version:wrapper` error | 两个 binary 来源/版本不一致；从同一归档恢复整套，勿单独复制 |
| 升级后仍旧版本 | `command -v sift`、`bin/current`、service restart |
| schema 较新拒启 | 不要删库；恢复能读该 schema 的新 binary |
| 未知/失败状态 | 按 fail closed 对待，查 timeline 事件回放而非猜测 |

## 首跑路径（简短版）

1. `sift init` 引导配置（依赖缺失会自动引导安装/登录；收尾含 doctor 自检 / service 安装 / 触发 label 创建）
2. 给一个边界清楚、可丢弃的 Issue 打触发标签（默认 `sift:run`）：
   ```bash
   gh issue edit <N> --add-label "sift:run"      # GitHub
   glab issue update <N> --label "sift:run"      # GitLab
   ```
3. 轮询预期：idle 60s / active 15s（`scheduler.intake_idle_interval`，权威默认值见 docs/specs/config.md §3.5）
4. 观察：`sift ps` → `sift timeline` → `sift logs <run-id>` → `sift worktree <run-id>`
5. 需要人决定时，复制评论中带 nonce 的完整命令

完整指南见 `docs/guides/getting-started.md`；排障细节以 `docs/runbooks/troubleshooting.md` 为准（本 skill 是浓缩层，发现矛盾以 docs 为准并反馈）。
