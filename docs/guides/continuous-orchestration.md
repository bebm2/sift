---
status: active
created: 2026-08-17
summary: 外部指挥 + Sift 执行模式：单人半自主持续推进项目
---

# 持续推进项目：指挥者模式 v0

本指南是指挥官模式（commander mode）的 v0 使用模式。它回答的是「我已经用 Sift 跑通了首个 Issue，现在想让项目**持续**推进到我每天只看 30–60 分钟、不盯盘也前进」这个需求。**v0 不增加任何新机制**：Sift 仍是执行层，「下一步做什么」由你（或你的 agent 会话）拍板，Sift 只负责把拍板后的工作推进到 Gate 与必要的人工审批。

- **如果你只想把 Sift 装好、跑通首个 Run**：回到 [Getting Started](getting-started.md)。本文假设你已经走完那条路径。
- **本指南与 [分析文档](../analysis/2026-08-17-continuous-project-orchestration.md) 配套使用**：分析文档回答「为什么是外部指挥而不是 Sift 内置编排器」，本文回答「外部指挥模式下每天怎么操作」。本文不复述分析文档的方案对比；遇到事实冲突以分析文档为准。

## 1. 三层拼装

持续推进是把三件事拼在一起：指挥层决定下一步、执行层把决定落实、汇合层把当天的结果合并成一次打扰。

| 层 | 角色 | 产出 | 工具 |
|---|---|---|---|
| **指挥层** | 人 + agent 会话（pi-agent skill / pi / Claude Code 等） | 拆解任务、建 Issue、打 `agent::*` 与 `plan:<id>` 标签 | `gh issue create` / `glab issue create` + `sift issue new`（v1 只读起草登记见 [Getting Started §7.7](getting-started.md#77-讨论式起草sift-issue-new)） |
| **执行层** | Sift | 摄入 trigger label → Run → Agent → Gate → Change（PR/MR）→ 人工审批 | Sift daemon |
| **汇合层** | Sift 的每日 digest 合批窗口 | 一次性把当天所有 interrupt 合批为一条；空闲项目发一条状态心跳 | Sift Channel digest（`daily_summary`） |

**关键约束（PRD §1.1 红线）**：指挥智能住在你或你的 agent 会话里，**不**住在 Sift 内核。Sift 内核永远是筛子与注意力调度，不是规划者。任何把「下一步做什么」塞进 Sift daemon 的尝试（写 daemon、塞 `sift plan` 子命令、用 LLM 介入 Gate）一律违反边界。本指南的所有建议都遵守这条线。

## 2. 每日操作环（30–60 分钟）

持续推进的真实承诺是「每天 30–60 分钟项目不停」，不是「撒手」。下面是一个实际可执行的循环，按这个顺序跑一遍就足够。

### 2.1 开窗（5–10 分钟）

打开 Sift 状态 + 每日 digest，**先看汇合层**再看 Run：

```bash
sift ps                # 当前活跃 Run 列表（queued/running/waiting_human）
sift timeline --limit 50   # 最近事件流
sift logs <run-id>     # 如有 Run 卡住，先看 log 再决定
```

然后读 Forge 上 Sift 当天的评论。Sift 在 `daily_summary` 合批时会把所有 `waiting_human` interrupt 合成一个帖子，并附带 Run ID + 一次性 nonce + 完整命令。把**原样提供的完整命令**复制到对应 Issue 评论里回复；不要手写 nonce，也不要只回复 `/sift approve`。

**如何发现「我错过了什么」**：

- 当天 digest 已经发送 → 评论里读命令，照办即可；
- 当天 digest 没发送 → 项目空闲（见 §5 空闲心跳），不是漏了，是 Sift 在告诉你「今天没你的事」；
- 当天 digest 发送了但你 24 小时没读 → 下一个工作日补读、一次性批量处理，但仍按 nonce 各自回复（nonce 过期会 fail closed，必须从最新评论复制新 nonce）。

### 2.2 喂料（15–30 分钟）

指挥层的工作：扫 PRD / DESIGN / WBS / STATUS，挑出**下一波可派工的 Issue**。

判定顺序：

| 决策问题 | 答案 | 下一步 |
|---|---|---|
| 上一个 Run 完成且 Change 已 merged？ | 是 | 下一个 PRD/DESIGN/WBS 候选 Issue 是什么？ |
| 有 Run 卡在 `waiting_human` 且超过 N 小时？ | 是 | 优先处理这一条（digest 里那条命令） |
| 有 `failure_review` 类型 interrupt？ | 是 | 决定 retry / reject；rejection 后该 Issue 是否要重开或关掉 |
| WBS 中下一里程碑的前置条件已具备？ | 是 | 建 Issue，进入下一波 |

为每个要派工的 Issue 做三件事：

1. **建 Issue**（已存在的跳过）：标题清楚、acceptance 列点、引用 PRD/DESIGN 章节；
2. **打 `plan:<id>` 标签**（如属于某个计划波次）：把同一波次的 Issue 串起来，便于一周后回看计划返工率；
3. **打触发标签**（如 `sift:run`，或仓库自定义）：Sift 摄入后建 Run。

```bash
ISSUE=42
gh issue edit "$ISSUE" --add-label "sift:run" --add-label "plan:v0-usage-guide"
# 或在 forge 网页/App 中操作；同一 Issue 不要同时多机 Coordinator 抢（见 Getting Started §6 失败恢复）
```

**为什么用 `plan:<id>` 而不是父 Issue**：forge 原生可筛、人也能直接看；不会因为「父 Issue 已 merged」而让子任务的引用失效（分析文档 §6.1）。

**打 label 之前请确认**：

- Issue 描述含 acceptance checklist；
- 主工作区无你不想混入的临时修改；
- 该 Issue 不会与别人手动操作的 worktree 冲突。

### 2.3 撒手（10–15 分钟）

确认 Sift 在跑（`sift service status`，或前台 `sift daemon`），然后**关窗**。

```bash
sift service status          # service 应为 running
sift doctor                  # 在线 doctor 确认 daemon 可达、无新告警
```

不要让 daemon 前台跑在你 SSH 上去的临时终端上。Sift 不承诺崩溃自启；如果你需要真正的不中断，用平台对应的 supervisor（launchd / systemd user unit）。

## 3. 合批纪律

「合批」是把事件驱动的人审改成窗口驱动。理由很简单：**auto_merge 毕业之前，每个 Change 都要人看**。一旦 daemon 在你睡前生成了一个 Run 的 Gate verdict，你被叫醒去审，那「持续推进」就退化成了「24/7 on-call」。

合批的两条纪律：

1. **每日一次 digest 时窗**（默认 Sift 的 `daily_summary`）：在那之前累积的 `waiting_human` interrupt 全部合批；时窗由 `day_timezone` + `daily_summary_at` 在 Sift 配置里固定。详见 [安装指南 → Channel 配置](installation.md)。
2. **不在 digest 时窗外响应 interrupt**（除 `critical_fused` 与 `merge_conflict` 这两类自动升级的）：daemon 会按 escalation 升级到最大封顶，落 `hold` 而不是再生成 digest。**接受 hold 的代价**——它就是消化批量 human attention 的工具。

合批之外还有一类**空闲心跳**（§5），它不是 interrupt，所以不会打扰你；它只是告诉你「Sift 在跑但今天确实没你事」。

## 4. `plan:<id>` 约定

`plan:` 前缀是给「同一波次」的 Issue 打的串联标签。约定：

| 字段 | 规则 |
|---|---|
| **前缀** | `plan:`，**永不被 Sift 摄入触发**（仅作 Sift 已有的 trigger label 之外的串联用） |
| **值** | 人类可读的短标识，如 `plan:v0-usage-guide`、`plan:m8-release` |
| **作用** | 把同时段、相关、可一起回看返工率的 Issue 串起来 |
| **不写** | 不要写成 forge 父 Issue 编号（详见分析文档 §6.1） |
| **何时移除** | 该波次所有 Issue 关闭、下一波开始时移除旧 `plan:` 标签 |

**用例**：在 §2.2 的「判定顺序」里，对当前 wave 的所有候选 Issue 打同一个 `plan:v0-usage-guide`。一周后看这批 Issue 的 merged / failed / closed 比例、每条 round-trip 次数、有没有「等 A 完开 B」出现——这就是分析文档 §5 重启条件里要的证据。

## 5. 空闲心跳（自动）

从 v0 开始（issue #1010），Sift 在每日 digest 合批时会区分三种状态：

| 项目状态 | 当日 digest 行为 |
|---|---|
| 有 1 个以上 interrupt | 正常 digest：`waiting_human` interrupt 合批成一条评论 |
| **无 interrupt + 有活跃 Run**（`queued` / `running` / `waiting_human`） | 静默不发——你已经有更新鲜的信号 |
| **无 interrupt + 无活跃 Run + 最近 7 天有 Run 活动** | 发一条**单行状态心跳**：见下 |
| 无 interrupt + 无活跃 Run + 最近 7 天无活动（dormant） | 静默不发——项目真休眠了，避免永久噪音 |

状态心跳是一条**确定性的单行文本**，到点按 `daily_summary` 时窗发出：

> 昨日无待办事件；当前无活跃 Run —— 舰队空闲。若尚有未派发工作请开窗喂料

它的语义是「Sift 在跑、项目在 Sift 里没死、今天没你事」。**它不是 interrupt**，所以：

- 不会被任何 `/sift approve` 命令消费；
- 不会消耗 `interrupts_per_run_daily_quota` 等注意力预算；
- 不会被 `failure_review` 等分类升级。

它只是让你（与你的 commander session）能区分「今天没邮件是因为 Sift 死了」与「今天没邮件是因为项目真的没事」——后者不需要你动，前者需要你翻 `sift doctor`。

「7 天」是命名常量（`IdleRunActivityWindowMS`），目前不暴露为配置。后续若反馈需要可调，再以配置项形式外露——参见分析文档 §4 第 5 步的「信号齐备后评估」节奏。

## 6. 档位表（自主度）

自主度**唯一**由「门禁毕业度」决定，与本指南无关——影子门禁未毕业时，自称高自主度的任何方案都是范围蠕变。

| 档位 | 条件 | 你每天的投入 | 何时升级 |
|---|---|---|---|
| **0 半自主** | 影子门禁未毕业（当前 PRD §5.6） | 30–60 分钟/天，digest + 喂料 | 影子门禁毕业后 |
| **1 低风险直通** | 低风险 Change 走 `auto_merge`，其余仍人审 | 15–30 分钟/天，主要审非低风险 Change | 自动合并覆盖到 medium severity 后 |
| **2 计划边界参与** | 整个 plan 的边界由人批，中间 Change 自动跑 | 不定期，主要审 plan 边界 | M7 后另立 issue |

**当前档位是 0**。本指南描述的就是档位 0 的操作模式。

## 7. 信号记录要求

为分析文档 §5 的「重启条件」攒证据，使用过程中请记录：

| 信号 | 记录位置建议 | 何时填 |
|---|---|---|
| 「等 A 完开 B」出现频次 | 该 Issue 评论，或 Wave 总结 Issue | 每次发生 |
| 每波次 digest 打扰次数 | Wave 总结 Issue | Wave 结束 |
| 计划返工率（rework） | Wave 总结 Issue | Wave 结束 |
| 空闲心跳被误判的天数 | `sift doctor` 输出 + Issue | 发现即记 |

**为什么现在就要攒**：方案 B（fan-out）、方案 C（wave 调度）的立项信号完全依赖这些数据；没有数据 Sift 永远不会替你做这些——这本身就是边界的一部分。

记录位置不强求「必须存哪」；一个 Wave 总结 Issue（或本仓库已存在的 review 类 issue）足以。下次回过头来评估方案 B 时，这些数据是论据基础。

## 8. 边界声明（再说一次）

- Sift **不是**编排器；编排智能必须住在你的 agent 会话/prompt 里（PRD §1.1）。
- Sift 不会替你决定「下一步做什么」，只会忠实执行你打的标签。
- `auto_merge` 默认关闭；Gate 通过 ≠ 自动合并。
- 一台机器一个 Coordinator；多机抢同一 Issue 池不是负载均衡，会产生重复 Run 和远端副作用。
- 所有停止可打扰、所有继续静默——这是断链纪律的单边，所有「撒手」承诺的反面都是这条。

## 9. 下一步

1. **立即可用**：在 [Getting Started §7.5](getting-started.md#75-会话式探索sift-pi) 的 `sift pi` 会话里跑一次「看看本仓库当前 open Issues 和最近一周 digest」，验证指挥会话接入 Sift 状态视图；
2. **可选**：为下一个 Wave 建一个总结 Issue，作为本指南 §7 的信号记录容器；
3. **可选**：把 §6 档位表抄进 `.sift/policy.yaml` 注释或团队 wiki，作为档位升级的对外宣示。

不需要做的事：

- 不要把 `sift` 包装成「自动决定下一步」的脚本；
- 不要建 daemon 内的定时任务来扫描 Issue 自动建 Run；
- 不要让 agent 直接调 `gh issue create` 未经你确认——`sift issue new`（Getting Started §7.7）是写路径的边界守住点。