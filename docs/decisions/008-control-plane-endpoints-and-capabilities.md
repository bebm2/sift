---
status: active
created: 2026-07-27
summary: 控制面拆两个 socket 两类能力；agent 越权在 V0 未闭合，如实声明并纳入诊断
---

# ADR-008 本地控制面：端点分离与能力边界

结构展开见 [DESIGN §3.2、§8.9–8.10、§9.1](../DESIGN.md)。本 ADR 修正 [ADR-006](006-report-via-cli-not-mcp.md) 中一处过度声明；wrapper 启动动词由 [ADR-010](010-attempt-spawn-handoff.md) 修订。

## 决策

1. **两个 Unix socket，两类能力**，不是一个 socket 上放两种权限：

   | socket | 谁连 | 能力 | 凭据 |
   |--------|------|------|------|
   | `~/.sift/siftd.sock` | 人（`sift` CLI） | 运维 RPC：`ps` / `logs` / `kill` / `retry` / `doctor` … | operator capability（`~/.sift/operator.token`，0600，启动期读入） |
   | `~/.sift/run.sock` | agent（`sift report`）与 wrapper | 上报动词 + wrapper 的 acquire / permit-spawn / started；run token 无改状态能力，wrapper 凭据只能推进本 attempt 的启动阶段 | 上报凭 attempt 作用域 run token；启动握手凭 bootstrap nonce，后续凭 wrapper session + spawn permit（ADR-010） |

2. **服务端按「端点 + 出示的凭据」授权，不按子命令名**。客户端可以伪造任何请求，能力不能由客户端自称的身份决定。同一端点上的凭据各自对应动词子集：run token 出示不了启动动词；bootstrap nonce 只能 acquire 一次；wrapper session 只作用于绑定 attempt；permit 只用于该 session 的 started。
3. 所有运维 RPC 记安全事件；失效 generation、错误 wrapper instance / session / permit 的启动请求一律拒绝并记事件。
4. **如实声明：V0 下 agent 越权未闭合。** agent 同 UID 且会跑 shell，可读 `operator.token` 并连运维 socket。该暴露面进 TM6 清单并由 `sift doctor` 逐条列出。
5. **「Layer 1 永不越权」收窄为凭据性质**：run token 对应的上报动词不改状态（结构性成立）；wrapper 启动凭据可推进的只有本 attempt 的 `pending → starting → spawning → running`，且 `running` 需要 Agent 启动证据；「agent 不越权」在 V0 不成立（未闭合）。
6. **沙箱挂载集**（未来收口时的确切形状）：**挂 `run.sock` + 本 attempt 自己的 run dir，deny 其余 `~/.sift/`**（DB、`config.yaml`、`operator.token`、其他 Run 的 run dir）。

## 理由

初稿的推理是「Report 子命令集里没有写 `runs.status` 的操作，所以 Layer 1 不越权」。这个推理只覆盖了一条路径，而 agent 的能力不止那条路径：它能跑 shell，就能跑 `sift kill`；socket 的 `0600` 隔离的是**别的用户**，不是**同属主的 agent**——这句话本来就写在 TM6 里，只是没有被应用到控制面自己身上。后果是 prompt injection 可以从 agent 直接进入本地控制面，而文档声称它不能。

端点分离的价值不在 V0 就闭合什么，而在**让边界成为可以一次关掉的形状**：沙箱按决策 6 的挂载集挂载，运维面即自然不可达。如果两类 RPC 混在一个 socket 上，将来落地沙箱时要么整个控制面不可用（`sift report` 也连不上），要么得回来重构协议。**这是典型的「早做便宜、事后昂贵」**，与 PRD §3.4 的取证顺序同源，所以放在第 1 片。

**挂载集必须写全，否则这条论证本身有裂缝。** 本 ADR 初稿把它写成「只挂 `run.sock`、deny read `~/.sift/`」，而 run token 就在 `~/.sift/runs/<run_id>/control.json`——按那句话字面落地，会「闭合」出一个 agent 无法上报的系统（评审 R6-M1）。端点分离的全部收益都押在这一句上，因此它必须精确到 run dir 一级。

operator token 的作用不是加密强度，而是把「顺手调用」变成「显式读取一个凭据文件」：它抬高一次代价，并让越权行为在事件流里留痕。**不假装它是隔离手段。**

配套把 run token 从环境变量移入 `control.json`：环境变量对同属主进程是公开的（`ps e`、`/proc/<pid>/environ`），而文件至少需要知道路径且受 0600 约束。这是纯收益，没有代价。

## 放弃的选项

| 选项 | 放弃理由 |
|------|---------|
| 单 socket + 请求级权限字段 | 沙箱无法只挂上报面；权限校验散在动词里，容易漏 |
| 不做 operator token（靠 `0600` 就够） | `0600` 对同属主 agent 无效，而 agent 正是主要威胁来源 |
| 声称「Layer 1 永不越权」不改 | 事实不成立。文档里的假保证比没有保证更危险 |
| V0 就上沙箱以真正闭合 | 见 [ADR-007](007-tm6-minimal-credential-sandbox-direction.md)：V0 不实施沙箱 |

## 后果

- 正面：授权模型有唯一实现位置；边界形状可被沙箱一次关闭；越权可审计。
- 负面：V0 交付一个已知可被 agent 越权的控制面。**必须持续可见**——`sift doctor` 报告 + 测试用例锁定（DESIGN §12 V10b：现在断言「暴露面被如实报告」，沙箱落地后翻转为「访问必须失败」）。
- 中性：`sift` CLI 需要读 operator token，因此 daemon 不可用时的离线诊断路径也要能在无 token 情况下工作（只读、明确标记离线）。
