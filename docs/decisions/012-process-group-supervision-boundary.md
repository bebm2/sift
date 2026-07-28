---
status: active
created: 2026-07-28
summary: V0 进程监督只覆盖不主动脱离 wrapper 进程组的 Agent
---

# ADR-012 进程组监督的适用边界

本 ADR 补充 [ADR-005](005-execution-backend-and-wrapper-contract.md) 与 [ADR-010](010-attempt-spawn-handoff.md) 的监督前提。结构展开见 [DESIGN §8.4 / §10.1](../DESIGN.md)。

## 决策

V0 的“确认执行体消失”只对**遵守进程组契约**的 Agent 成立：Agent 及会继续写 worktree 的后代不得通过 `setsid`、二次 fork 等方式主动脱离 wrapper 进程组。

每个受支持的 agent CLI / 版本组合必须跑进程拓扑资格测试；结果进入支持矩阵与 `sift doctor`。未验证或已观察到脱组行为的组合标为 `process-group-unverified`，不得宣称满足自动 retry 的单 writer 保证；在旧执行体身份含糊时只能保持隔离并转人工。

恶意同 UID 进程主动逃逸不在 V0 强制边界内，归 PRD TM6；彻底闭合依赖未来沙箱后端。

## 理由

“Agent 初始时是 wrapper 直接子进程”不等于“整个执行期都留在原进程组”。若写 worktree 的后代脱组，原进程组消失不能证明执行体消失，此时自动新开 attempt 会重新引入双写窗口。

## 放弃的选项

| 选项 | 放弃理由 |
|------|----------|
| 把直接父子关系当永久事实 | Unix 进程可自行脱组 |
| V0 引入跨平台强沙箱 | 与 ADR-007 的范围裁决冲突 |
| 静默接受所有自定义 Agent | 会把条件保证写成无条件保证 |

## 后果

- 单 writer 声明必须带“进程组契约内”的限定。
- Runtime 测试与真实 Agent 验收增加拓扑资格项。
- `doctor` 必须展示当前 agent/version 的监督姿态。
