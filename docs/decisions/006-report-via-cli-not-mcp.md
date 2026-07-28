---
status: active
created: 2026-07-27
summary: Layer 1 上报走 sift report CLI 而非 MCP；MCP 保留为未来可选前端
---

# ADR-006 Layer 1 上报通道：`sift report` CLI，不用 MCP

> **控制面部分由 [ADR-008](008-control-plane-endpoints-and-capabilities.md) 修订。** 本 ADR 只裁定「上报走 CLI 而非 MCP」；socket 端点与鉴权模型一律以 ADR-008 为准。**下文「后果」中原有「上报与运维命令共用一条 socket 与一套鉴权」一句已失效**（D0.3 修订，评审 R5-F5）——那是本 ADR 成文时的形态，现已改为两个端点、两类能力。

结构展开见 [DESIGN §8.9](../DESIGN.md)。本决策已回写 [PRD §5.8 与 §8 模块表](../PRD.md)（模块 MCP 改名 Report）。

## 决策

Agent 的 Layer 1 上报（进度 / goal / blocker / 完成声明）通过 **`sift report` 子命令**完成，不通过 MCP。

Sift 只向 agent 环境注入非机密的 `SIFT_RUN_DIR`；attempt 作用域的 run token 存放在该目录下的 `control.json`（0600），由 CLI 读取——**token 不进环境变量**，那会经 `ps e` / `/proc/<pid>/environ` 白送出去。`sift report` 只连 `run.sock`，`siftd` 校验 token 属于该 attempt 且该 attempt 仍活跃，跨 Run 调用拒绝。端点划分与运维面的授权见 [ADR-008](008-control-plane-endpoints-and-capabilities.md)。

`run.sock` 上还有 wrapper 的启动动词 `claim:acquire` / `claim:permit-spawn` / `claim:started`（见 [ADR-010](010-attempt-spawn-handoff.md)）。它们分别要求 bootstrap nonce 或 wrapper session + spawn permit，**run token 出示不了**；这些启动凭据不进入 Agent 环境或 `control.json`。因此「run token 只能上报」仍是端点上的结构性事实，而不是靠动词命名约定。

限流、去重、每 Run 的 Interrupt 子配额（PRD §5.8 / TM5）在 `siftd` 内用库里的令牌桶**确定性**执行，不经 LLM。上报能力集里**不存在**能写 `runs.status` 的操作。

MCP 保留为未来某 harness 明显受益时的第二种前端，接入点是 Report 服务而非新通道。

## 理由

目标用户是 coding agent，**跑 shell 命令是它的本职**（与 `git` / `gh` 同级），`--help` 自带文档，Task Spec 里写一行用法即可。三条更具体的理由：

1. **与既有集成哲学同源。** PRD §5.2 已经把 forge 集成定为「CLI 即一个已鉴权好的传输层」。上报也走 CLI，意味着 Sift 对外的集成面只有一种形态，没有第二种协议要维护。
2. **MCP 适配本身就是一种 harness 绑定。** 这是决定性的一条。PRD §3.2 明确「不绑定某一 harness；Agent 是外部进程」，而各 harness 注册 MCP 服务端的方式互不相同（命令行 `--mcp-config`、配置文件、内置注册表），配置放在哪里还牵扯到「不能写进 agent 自己能改的 worktree」。为每个 harness 写一份 MCP 接线，恰恰是在做我们声明不做的事。
3. **少一层。** MCP 方案需要一个 stdio shim 进程 + 一条 JSON-RPC 转发链才能抵达 `siftd`（因为 PRD §9.3 要求零监听端口，不能开 HTTP MCP）。CLI 方案里 shim 与 JSON-RPC 都不存在。

**安全模型不因通道形态改变**：可信边界一直在守护进程侧，不在传输层。两种方案同样需要 run 作用域凭据、同样在 daemon 内限流。

**一处必须说清的边界**：本 ADR 只能保证「Report 端点上不存在改状态的动词」，**不能**保证「agent 改不了状态」——agent 与用户 CLI 同 UID，V0 下它可以读 operator token 去调运维 RPC。把前者说成后者是过度声明，收窄后的表述与未闭合清单见 [ADR-008](008-control-plane-endpoints-and-capabilities.md) 与 [ADR-007](007-tm6-minimal-credential-sandbox-direction.md)。

## 放弃的选项

| 选项 | 放弃理由 |
|------|---------|
| **MCP（stdio shim + Unix socket）** | 见理由 2、3。它的真实优势是工具 schema 自动注入——但对能读 prompt、会跑 shell 的 agent 是冗余 |
| HTTP MCP | 违反 PRD §9.3 零监听端口 |
| Agent 直写 SQLite | 违反单写者模型，且把 DB 暴露给不可信进程 |
| 文件投递（agent 写文件、siftd 扫描） | 无鉴权、无背压、限流点不明确 |

## 后果

- 正面：无 shim 进程、无第二种协议；上报与运维走**两个端点、两类能力**（ADR-008），能力边界由「连哪个 socket、出示什么凭据」决定而非子命令名；换 harness 零成本。
- 负面：agent 不会「自动发现」上报能力，必须在 Task Spec 里明确告知用法——这条落到 `specs/report.md` 与提示词模板，属于必须记住的事。
- 可逆性：若将来某 harness 因 MCP 获得实质收益，新增 MCP 前端接到同一个 Report 服务即可，限流与鉴权不重写。
