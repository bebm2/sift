---
status: active
created: 2026-07-27
summary: TM6 方向定为最小凭证沙箱；V0 不实施，只留接缝并如实声明
---

# ADR-007 TM6 收口：最小凭证沙箱（定方向、留接缝、V0 不实施）

关闭 [PRD §12 #13](../PRD.md)。威胁背景见 PRD §9.1 TM6；结构展开见 [DESIGN §9.1](../DESIGN.md)。

## 决策

1. **方向采纳「最小凭证沙箱」**：沙箱内只挂 agent CLI 自身凭证，**不挂 forge 凭证**，切断 agent 到 `~/.sift/`、forge CLI 鉴权、共享 `.git` 的通路。否决「完全沙箱」（与「复用你已订阅的本机 agent」正面冲突）。
2. **V0 不实施沙箱。** 只做 PRD 已定的缓解 + 一条新增的强化，并如实声明未闭合。
3. 新增强化：**Sift 自己的 git 调用一律显式带 `-c core.hooksPath=/dev/null`**。命令行 `-c` 覆盖 `.git/config`，因此 agent 改配置或重指向 hooksPath 都伤不到 Sift 的 git 操作。指纹校验降级为第二道审计信号，不是唯一防线。
4. **留接缝**：启动 agent 只经单一 launcher 间接层（[ADR-005](005-execution-backend-and-wrapper-contract.md)），V0 为恒等实现；将来包 `sandbox-exec` / 容器只改这一处。Agent 启动参数不得假设「与 daemon 同环境」。
5. **执行后端命名与 `sift doctor` 输出必须如实显示 `unsafe-local`**，不得把 worktree 描述为系统沙箱。
6. 共享 `.git` 的处置（沙箱内完整 clone + 两侧同步）**明确推迟**到沙箱立项时回答。

## 为什么方向是「最小凭证」而非二选一

采纳第三方评审（`../reviews/2026-07-27-prd-review-third-party.md` §三）否定二元框架的意见：「零凭证的便宜」与「真安全的贵」不是一道单选题。**两类凭证可以分开处理**——沙箱要切断的是 agent 到 `~/.sift/`、forge CLI、共享 `.git` 的通路；而「复用订阅」只需要把 agent CLI 自己的凭证挂进去。真正退化的只是「agent 与 Sift 同环境」的便利性，不是价值主张。

## 为什么 V0 不实施

三条理由，缺一不可：

1. **macOS 上唯一低成本手段 `sandbox-exec` 已 deprecated。** 把 V0 押在它上面，是把一个「未闭合的边界」换成一个「会在某次系统升级时消失的边界」——后者更坏，因为它看起来是闭合的。
2. **它不闭合共享 `.git`。** 而彻底切 `.git` 等于放弃 worktree、改为完整 clone + 双向同步，会动到 A5 的核心设计。PoC 阶段不该付这个代价。
3. **A4。** TM6 是结构性缺口而非在途事故；PRD §9.1 已把它列入 Backlog 并给出立项信号。先把闭环跑通，再用真实运行形态决定沙箱形态。

## V0 的实际姿态（如实声明）

| 暴露路径 | V0 处置 | 性质 |
|---------|---------|------|
| `~/.sift/`（allowlist、配额、agent 定义、DB） | 权限收紧到仅属主 + 启动期读入 + 指纹校验，运行期不热加载 | **未闭合**（防别的用户，防不了同属主的 agent 进程） |
| 已登录的 `gh` / `glab` | 无对策；Sift 不主动传递凭证 ≠ agent 取不到 | **未闭合** |
| **运维 socket + `operator.token`** | 端点分离（agent 只需 `run.sock`）+ operator capability + 运维 RPC 记安全事件（[ADR-008](008-control-plane-endpoints-and-capabilities.md)） | **未闭合**：同属主 agent 可读 token 并调 `sift kill`。**沙箱一挂即闭合** |
| 共享 `.git`（hooks 投毒） | `core.hooksPath=/dev/null` 事前失效 + 指纹审计 | 对 **Sift 自身**的 git 操作闭合 |
| 共享 `.git`（其余写入）、其他 Run 的 worktree | 不切 | 未闭合 |
| run token 泄露面 | 已从环境变量移入 `control.json`（0600） | 收窄但未闭合（同属主可读该文件） |

指纹的对象是**解析后的有效 hooks 配置**——`core.hooksPath` 的取值、其最终指向目录的内容、以及 `.git/config` 本身；只对 `.git/hooks/` 做指纹会被重指向绕过（PRD §9.1 已列出该手法）。

配套结论：**PRD §5.2「零凭证管理」加星号，星号只指 forge 侧**。Sift 不落盘 forge 凭证成立；「agent 取不到 forge 凭证」只在沙箱生效后成立。

## 证伪条件（本方向的前置 spike，排入 WBS）

逐个实证各目标 agent 的凭证存储形态：**文件形式可挂载，绑 OS keychain 或设备指纹挂不进去**。首批验证 `claude` 与 `codex`。

**若首批全为 keychain-only，最小凭证沙箱方向在起点即不成立**，必须回到本分叉重议（候选退路：完全沙箱并接受订阅凭证不可用，或独立用户 + 单独登录）。这是「方向已定」的可证伪部分，不是修辞。

### 多平台带来的修正（[ADR-009](009-tech-stack-go.md) 之后）

PRD V0.4 把 Linux 纳入支持矩阵，这一节的判定因此**必须按 OS 分别出结论，不是一行**：

- **keychain 是 macOS 特有形态**，Linux 侧 agent CLI 凭证多为 `$HOME` 下的文件，可挂载。所以「macOS 上全为 keychain-only」不再等于「方向不成立」，只等于「macOS 上收不了口」。
- **手段的成熟度也是反向的**：macOS 只有 deprecated 的 `sandbox-exec`，Linux 有 user namespaces / bubblewrap / seccomp。**因此收口的现实次序大概率是 Linux 先、macOS 后**，两平台会长期处于不同的安全姿态。
- 后果是一条诚实性要求：`sift doctor` 的安全姿态必须按平台报告，**不允许出现「已支持沙箱」这种掩盖平台差异的表述**（Q6）。执行后端命名同理——macOS 上仍是 `unsafe-local` 时，Linux 上的 `sandbox-*` 不能让人误以为整个产品已收口。

## 放弃的选项

| 选项 | 放弃理由 |
|------|---------|
| V0 落地 `sandbox-exec` profile | 见「为什么 V0 不实施」1、2 |
| 完全沙箱（不挂任何凭证） | 与 PRD §1.2「复用你已订阅的 coding agent」正面冲突 |
| 共享 `.git` 的半沙箱 | 增加复杂度却保留最关键的越界通路 |
| 不表态、留在 Backlog | PRD §13.1 要求 DESIGN 正面回答；越晚答代价越大 |

## 后果

- 正面：方向明确且可证伪；launcher 接缝让将来落地不需重构 Runtime；`core.hooksPath` 覆盖是本次唯一进入 V0 的强化，成本近零而效果强于指纹。
- 负面：V0 交付一个**已知不安全**的执行环境，这必须在诊断输出与文档里持续可见（PRD §10.1 的安全验收也已限定为 repo 级）。
- 中性：PRD §3.3「更强隔离」的立项信号已由声明本身给出，不必等真实事故。
