---
status: active
created: 2026-08-14
summary: 从安全试跑或已有仓库完成首个 Sift Run
---

# Getting Started

本指南把首次使用分成两条真实路径：

- **路径 A：Bluff Template**——已可从 Template 建立独立的安全试炼场；
- **路径 B：已有仓库**——现在可用，建议先选一个可丢弃的小 Issue。

安装和升级细节以 [安装指南](installation.md) 为准；这里聚焦从零到首个 Run。完成首跑后如需把项目推进到「每天只看 30–60 分钟也前进」，进入进阶路径[单人半自主](continuous-orchestration.md)（指挥官模式 v0：指挥者会话 + Sift 执行层）。

## 0. 前置检查

### 依赖自动引导

`sift init` 会自动探测并引导缺失的外部依赖：gh/glab 未安装时确认式引导安装、已装未登录时引导运行官方登录；PATH 中没有任何已知 Coding Agent 时优先推荐安装 pi。**手工检查只用于排障**——安装与登录的命令矩阵见[安装指南「依赖缺失的引导」](installation.md#依赖缺失的引导)。强校验请直接信任 `sift doctor` 的结论。

仍要手动核对时：

### 系统与 Git

Sift 发布包支持 macOS/Linux 的 amd64/arm64。先确认当前目录是有 `origin` 的 Git 仓库：

```bash
git --version
git remote get-url origin
```

**成功预期**：两条命令均输出版本或远端 URL。

**失败恢复**：先安装 Git；若没有 `origin`，进入正确的 clone，或按团队约定添加 GitHub/GitLab 远端。`sift init` 依靠远端识别项目，无法识别时不会猜测目标仓库。

### Forge CLI 与认证

GitHub 项目用 `gh`，GitLab 项目用 `glab`。只需检查对应平台：

```bash
gh --version && gh auth status
# 或
glab --version && glab auth status
```

**成功预期**：CLI 输出版本，`auth status` 显示当前 host 和登录用户。

**失败恢复**：未安装或未登录时，直接重跑 `sift init` 即可获得安装/登录引导；也可手动执行 `gh auth login` / `glab auth login`（Sift 复用官方 CLI 的登录，不保存或刷新 Forge token）。公司自建实例应登录项目 remote 对应的 host。

### Coding Agent 与额度

Sift 必须能从终端启动 Agent，而不只是打开 IDE。向导会自动探测 Claude Code、Codex CLI、Cursor CLI、pi、Gemini CLI、Aider、Qwen Code 和 Cody；未收录的 CLI 也可用可执行文件名登记。

```bash
claude --version       # 示例；换成 codex、cursor、pi 等实际命令
```

**成功预期**：命令无需图形交互即可输出版本。只有 Cursor GUI 而没有 PATH 中的 `cursor` CLI，不满足后台启动条件。

**失败恢复**：PATH 中没有任何 Agent 时，`sift init` 会优先引导安装 pi（开源、多模型、无厂商账号门槛）；其他商业 Agent 不在 PATH 时请自行安装并确认它在 daemon 可见的 PATH 中，也可以在 `sift init` 中输入绝对可执行路径。Agent 的账号、API Key、订阅和模型费用由你负责。Sift 配置中的 Brain token/API/attention 预算用于自身调度和 fail-closed，但不应当作供应商账单上限；首次测试请选小任务并同时检查供应商侧限额。

## 1. 安装 Sift

默认安装 latest release：

```bash
curl -fsSL https://raw.githubusercontent.com/xsift/sift/main/scripts/install.sh | bash
export PATH="$HOME/.sift/bin/current:$PATH"
sift --version
sift-agent-wrapper --version
```

**成功预期**：两个命令输出相同 release 版本，安装目录为 `~/.sift/bin/<version>`，稳定入口是 `~/.sift/bin/current`。

**失败恢复**：

- `sift: command not found`：重新执行 PATH export，并把它写入 `~/.zshrc` 或 `~/.bashrc`；
- 不接受 `curl | bash`：按 [release 归档安装](installation.md) 下载并校验 SHA-256；
- wrapper 版本不同：不要单独复制二进制，重新安装同一份完整 release。

## 路径 A：Bluff Template

[`xsift/bluff`](https://github.com/xsift/bluff) 是独立、安全试跑的入门项目。请先在上游点击 **Use this template** → **Create a new repository**，创建到自己的账号或组织；不要使用普通 Fork，也不要在 `xsift/bluff` 上游操作。

### A1. 创建、bootstrap 与试玩

clone 你刚创建的新仓库，并按 Bluff main README 的命令执行：

```bash
git clone https://github.com/YOUR-OWNER/YOUR-REPO.git
cd YOUR-REPO
gh auth status             # 未认证：gh auth login
./scripts/bootstrap.sh

pnpm install
pnpm dev
# 打开 http://localhost:3000，点击“开始一局”
```

bootstrap 会先验证 `origin`、GitHub 登录和仓库写权限，并拒绝修改模板上游；随后创建 `sift:run`、`sift:seed`、四个 priority labels，以及 `.github/sift-tasks/` 对应的 6 个 seed Issues。中途失败可直接重跑，已有 labels 和同标题 Issues 会被复用。

**成功预期**：新仓库不属于 `xsift/bluff`；bootstrap 输出 `bootstrap complete for OWNER/REPO`；仓库中有 6 个 seed Issues，且都带 `sift:run`、`sift:seed` 和对应 priority label；本地可打开游戏。

Template 创建、首次 bootstrap 生成 6 个 Issues，以及再次运行后仍保持 6 个 Issues，已经过 live 实测。该证据只覆盖 Template/bootstrap 与重跑幂等性，不覆盖真实 Agent→PR→人工审批链。

**失败恢复**：按报错修复认证、`origin` 或权限后重跑 `./scripts/bootstrap.sh`。脚本不会删除已创建对象。若 `origin` 指向 `xsift/bluff`，停止操作并 clone 你通过 Template 创建的仓库。

### A2. 初始化并启动 Sift

```bash
sift init
sift doctor --offline
sift service install
sift service status
sift doctor
```

无可用 launchd/systemd user service 时，按提示在一个保持打开的终端运行 `sift daemon`。成功预期与恢复方式同路径 B 的[初始化](#3-初始化)、[离线检查](#4-离线检查手动复查入口)和[启动 Coordinator](#5-启动-coordinator手动补救入口)。

### A3. 触发并观察 seed Issues

bootstrap 创建的 6 个 seed Issues 已带 `sift:run`，Coordinator 启动后会按轮询摄入。若要给某个已移除触发标签的 Issue 重新添加标签，先填入它的编号：

```bash
ISSUE_NUMBER=1
gh issue edit "$ISSUE_NUMBER" --add-label "sift:run"
sift ps
sift timeline --limit 20
sift logs <run-id>
sift worktree <run-id>
```

也可以在仓库网页中添加 `sift:run`。`<run-id>` 使用 `sift ps` 显示的实际 Run ID。

**成功预期**：Issue 出现在 `sift ps`，随后可从 timeline、日志和隔离 worktree 观察进展。需要人工决定时，只复制 Sift 评论中带 Run ID 与一次性 nonce 的完整命令；默认 `auto_merge=false`，Gate 通过不等于自动合并。

**证据边界**：Bluff 的真实 Agent→PR→人工审批流程尚未验收通过。请把第一次运行视为小范围人工检查，不要据 Template/bootstrap 成功推断 Agent 产出、PR、Gate 或审批已经验证。失败恢复、审批和清理命令见路径 B 的[观察进度](#7-观察进度)、[审批或拒绝](#8-审批或拒绝)与[安全、成本与清理](#安全成本与清理)。

## 路径 B：接入已有仓库

### 2. 选择安全的首个 Issue

选择一个范围小、验收清楚、允许关闭 PR/MR 并丢弃分支的 Issue。避免把首次试跑用于发布、迁移、密钥、支付、生产配置或大规模重构。

默认触发 label 是 `sift:run`。如果仓库尚无此 label，先在 Forge 的 Labels 页面创建；自定义 label 以配置为准。

**成功预期**：Issue 描述包含目标、范围、验收方式，仓库主工作区无你不想混入的临时修改。

### 3. 初始化

```bash
cd /path/to/your/repo
sift init
```

向导会：

1. 从 `origin` 探测 GitHub/GitLab 和项目；
2. 探测 PATH 中的 Agent 并让你选择；
3. 从已登录的 Forge CLI 记录 operator；
4. 写入 `~/.sift/config.yaml`。

配置写入后，向导会询问式引导「收尾三合一」（每步直接回车默认执行，或输入 `n` 跳过）：

1. **离线自检**：内嵌执行 `sift doctor --offline` 的检查逻辑，有未通过项时按严重度分组给出指引（error 带针对性修复建议；`tm6:*`、`operator-token` 等同 UID 安全边界标注为「已知 V0 边界、非故障」），失败不阻塞；
2. **用户级服务**：复用 `sift service install` + `sift service status` 完成安装与启动，已运行的服务会跳过不重复安装；没有可用 supervisor 时提示前台运行 `sift daemon`；
3. **触发 label**：以配置 `labels.trigger` 为准（默认 `sift:run`），先 `gh/glab label list` 查重，不存在时展示将执行的命令并确认后创建（gh 用位置参数，glab 用 `--name` 与 `#RRGGBB` 颜色）；已存在或失败都只降级不阻塞；重复运行 init 不会重复安装或重复创建。

`--offline` 或 flags 全给时这三步全部跳过，非交互输出不变。

**成功预期**：末尾显示已写入配置并完成收尾引导（或按 `n` 跳过）；`sift agent list` 和 `sift project list` 能看到所选 Agent/项目。

**失败恢复**：

- 无法解析项目：检查 `git remote get-url origin`，并确认 host 是 GitHub/GitLab；
- 未发现 Agent：输入可执行文件名或绝对路径，或先修复 service 可见的 PATH；
- operator 缺失：先完成 `gh auth login` / `glab auth login`，再运行 `sift init`；
- 写错配置：重新运行向导会备份原文件为 `config.yaml.bak`；字段级修复见 [配置规格](../specs/config.md)。

### 4. 离线检查（手动复查入口）

`sift init` 收尾时已引导运行离线自检；以下为手动复查入口：

```bash
sift doctor --offline
sift status
```

**成功预期**：doctor 没有 error；`status` 能读取本地配置状态。doctor 的退出码是 0=无问题、1=至少一个 warning、2=至少一个 error。

**失败恢复**：不要忽略 warning。按每条提示修复 PATH、文件权限、Agent 或项目配置后重跑；配置文件存在时应保持：

```bash
chmod 700 "${SIFT_HOME:-$HOME/.sift}"
chmod 600 "${SIFT_HOME:-$HOME/.sift}/config.yaml"
```

### 5. 启动 Coordinator（手动补救入口）

`init` 收尾三合一已引导安装并启动用户级服务；本节为手动补救入口（当时跳过、之后重装或需要重查时使用）：

```bash
sift service install
sift service status
sift doctor
```

macOS 使用 launchd，Linux 优先 systemd user unit。没有可用 supervisor 时，`service install` 会给出 foreground 提示，此时在一个保持打开的终端运行：

```bash
sift daemon
```

**成功预期**：service 为运行状态，在线 `sift doctor` 可连接 daemon。Sift 只创建本地 Unix sockets，不监听网络端口。

**失败恢复**：

```bash
sift service restart
sift service status
sift doctor
```

若仍失败，查看 [故障排查](../runbooks/troubleshooting.md)。前台模式终端退出后 daemon 也会停止，不承诺崩溃自启。

### 6. 触发首个 Run

给测试 Issue 加 label：

```bash
gh issue edit 42 --add-label "sift:run"       # GitHub
# 或
glab issue update 42 --label "sift:run"      # GitLab
```

也可以在 Forge 网页/App 中添加同名 label。

**成功预期**：轮询后 Issue 出现在 `sift ps`，Forge 上可看到 Sift 推进状态所产生的标签或评论。它不是即时 webhook；短暂等待轮询属于正常情况。

**失败恢复**：确认 label 拼写、daemon、项目配置、Forge 认证和 API 权限，然后运行：

```bash
sift doctor
sift ps -a
sift timeline --limit 20
```

同一 Issue 不要同时由两台机器上监听相同 label 的 daemon 消费；这不是负载均衡，会产生重复 Run 和远端副作用。

### 7. 观察进度

```bash
sift ps
sift timeline --limit 20
sift logs <run-id>
sift worktree <run-id>
```

**成功预期**：`ps` 显示 queued/running/waiting_human/done/failed 中的状态；`timeline` 显示 append-only 事件；有 attempt 后 `logs` 和 `worktree` 返回对应信息。

**失败恢复**：

- daemon 未连接：先恢复 service/foreground daemon；
- Run failed：用 `sift logs <run-id>` 和 `sift timeline --run <run-id>` 找原因，修复环境后执行 `sift retry <run-id>`；
- Agent 卡住或方向错误：执行 `sift kill <run-id>`，再检查隔离 worktree，不要直接合并其分支。

### 7.5 会话式探索：`sift pi`

不确定下一步做什么时，可以用会话式入口：

```bash
sift pi
```

`pi` 交互会话会注入一份「Sift 操作 skill」（随 release 内置，自动写到 `~/.pi/agent/skills/sift/`）和当前项目状态快照。你可以在会话里连续提问（例如「最近三个 run 哪个值得重试」「这个 timeline 里 Gate 为什么拒了」），agent 会用只读命令（`ps`/`timeline`/`logs`/`metrics`/`doctor`）自己取证后回答；有副作用的命令（`kill`/`retry`/`rm`/`service restart`）skill 要求先与你确认。

> 安全边界：skill 只是易用层，不是安全层。真正的闸门（approve 的一次性 nonce、policy fail-closed、`auto_merge` 默认关）都在 Sift CLI 内部，agent 无法绕过——把 CLI 交给 agent 用，最坏情况等价于一个手快的人类用户。pi 未安装时 `sift pi` 会打印与 init 相同的安装指引（不阻塞）。

### 7.6 单发语义问答：`sift issue`

不想进会话、只想问一句时，用单发语义入口（v1 只读）：

```bash
sift issue                                # 不烧 token：直接列各绑定项目的 open issue
sift issue list                           # 同上；list/ls 是保留子命令，--all 扩到全部项目
sift issue "这 20 个 issue 里哪些是相关的？"
sift issue "#42 的讨论核心分歧是什么？"
```

带自然语言问题时，Sift 先自己从 forge 只读接口取证（open issue 列表 + 问题中提到的 `#N` 的正文与评论），再调用一次 headless pi 回答。**只读保证是架构性的**：模型侧工具白名单钉死为 `read`，没有任何写路径；快照里没取到的事实模型只能回答「取不到」。写动词（`close`/`reopen`/`edit`/`comment`/`label`/`assign`）作首词会被直接拒绝并提示改用 gh/glab，不会烧 pi 调用。pi 未安装时降级为确定性列表 + 安装指引，不报错。

### 7.7 讨论式起草：`sift issue new`

想把一个想法变成高质量 issue，但不想自己写长文？进入起草会话，人只做判断，agent 负责取证与行文：

```bash
sift issue new                  # 建议在项目仓库目录下运行（agent 可读代码取证）
```

会话内：直接输入讨论内容；`#N` 取某 issue 全文与评论（不烧 token）；「草稿」查看当前草稿；「登记」进入确认；`q` 退出。多轮讨论后说「登记」，Sift（不是 agent）展示完整草稿并要求确认（y / e 进 `$EDITOR` 编辑 / 放弃），标题查重后才写入 forge。

安全红线：agent 全程只有 `read` 工具，登记由宿主代码在人工确认后执行；**默认不打触发标签**，创建后会间「打上触发标签立即开跑？」，默认否，并附手动触发命令。pi 未安装时明确指引退出。

### 8. 审批或拒绝

需要人工决定时，Sift 会在 Forge 评论中给出带 Run ID 和一次性 nonce 的完整命令，例如命令形态为：

```text
/sift approve <run-id> <nonce>
```

把评论里**原样提供的完整命令**回复到对应 Issue/PR/MR。不要手写 nonce，也不要只回复 `/sift approve`。其他可用动作同样以评论提供的命令和选项为准。

**成功预期**：Sift 回写 command acknowledgement，并继续 Gate/Change 流程。默认 `auto_merge=false`，Gate 通过不等于已自动合并；最终以 Forge 上的 PR/MR 状态为准。

**失败恢复**：命令过期、目标不符、operator 不可信或 nonce 错误时会 fail closed。回到最新 Sift 评论复制当前命令；不要复用旧评论中的命令。

## 安全、成本与清理

### 停止和丢弃试跑

```bash
sift kill <run-id>       # 停止活跃 Run
sift worktree <run-id>   # 找到并检查隔离 worktree
sift rm <run-id>         # 终态后从默认列表归档，历史仍保留
```

若 Run 仍在执行而你确定要终止并归档，可用 `sift rm -f <run-id>`。归档不是删除 Forge 上的分支或 PR/MR；远端清理由你按仓库正常流程执行。删除本地 worktree 前先确认 Agent 已停止、需要的 diff 已保存，并使用 Git 的标准 worktree 流程。

### 不会替你做的事

- Sift 不保证 Agent 产出正确，必须看 diff、Checks 和 Gate 证据；
- Sift 不提供模型额度，也不承诺配置预算等于供应商账单硬上限；
- Sift 不接管 `gh` / `glab`、Agent 或云模型的凭证；
- Sift 不让多个独立 Coordinator 安全抢同一 Issue 池；一个仓库/触发 label 只保留一个主动 Coordinator；
- Sift 不把 kill/归档当成已推送远端内容的自动回滚。

### 默认信任保证

- Agent 在隔离 worktree/分支执行；
- `auto_merge` 默认关闭；
- 未知 Checks、review、mergeability 或输入会 fail closed；
- AI 的建议不能覆盖 policy、Gate 或可信 operator 决定；
- Forge 是 Issue、Change、审批和合并状态的最终事实源。

完成首个小 Run 后，再逐步扩大任务范围、调整 `.sift/policy.yaml` 和预算；不要在未观察真实 Agent/Forge 行为前直接开启自动合并。

## 进阶路径：单人半自主

完成首个 Run 之后，最常见的下一步是「想让项目持续推进到我不用每天盯盘」。本指南不覆盖那条路：它是另一个独立的使用模式，与「装好 Sift、跑通首跑」有相当不同的默认上下文与纪律（每日 digest 合批、`plan:<id>` 串联、空闲心跳、信号记录等）。请进入[持续推进项目：指挥者模式 v0](continuous-orchestration.md)，与配套分析文档[持续推进编排需求分析](../analysis/2026-08-17-continuous-project-orchestration.md)一起阅读。
