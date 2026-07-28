# forge.md 字段级评审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/forge.md`](../specs/forge.md) draft
> 依据：PRD §4.5/§5.2/§9.2、DESIGN §6.4/§8.1/§9.2、ADR-011、WBS M2、active storage/outbox spec

## 1. 结论

**PASS。** 下列阻断与重要问题均已在候选稿及必要交叉文档中核销；`forge.md` 可转 `active`，作为 M2 端口与双平台适配器的实现契约。M1 fake 仍只是骨架，本结论不表示 M2 Intake 或真实适配器已经实现。

## 2. 发现与核销

| 项 | 级别 | 发现 | 处置 |
|----|------|------|------|
| F1 | P1 | `ListLabelEvents(targetID)` 无法区分 GitLab Issue/MR；两类 `iid` 可重号，审批标签也无法路由 | 新增 `TargetRef{Kind,ID}`；标签、评论、SetLabels 统一显式目标类型；PRD 动词表同步 |
| F2 | P1 | `Issue` 无 state，`GetIssue` 无法兑现 PRD §4.5 的 Issue closed 事实收敛 | 增 `IssueState` 与 `Issue.State`，冻结 open/closed 映射 |
| F3 | P1 | `Change` 缺 URL、mergeability、review、draft；与 PRD `getChange` 用途、平台差异表及 outbox 成功证据冲突 | 一次性冻结完整 `Change` 投影；不确定语义显式 `unknown`，不推迟到 M4 改接口 |
| F4 | P1 | `ListLabelEvents` 接收不透明 cursor 却不返回下一 cursor，上层既不能解析也不能安全推进 | 所有增量列表返回 cursor；规定时间戳 tie-break、边界重读、按远端 ID 去重 |
| F5 | P1 | GitLab merge 被错误描述为接受 GitHub 风格 `merge_method=rebase`；与 PRD 平台差异及 active outbox 的 V0 `merge`-only 冲突 | V0 只接受 `merge`；GitHub 传 `merge_method=merge`，GitLab 只传 expected `sha`，后续方法必须版本化验证 |
| F6 | P1 | labels 读整集后覆盖会在竞态窗口删除人类并发加入的无关标签，不能兑现 outbox“保留非 Sift 标签” | 改用平台 add/remove 语义，禁止整集覆盖，执行后重读确认 |
| F7 | P1 | 一次 `--paginate` 子进程可能产生多个 HTTP 请求，但旧预算按子进程只扣一次；marker `no_match` 也未强制穷尽分页 | 列表逐页、每进程单请求、每请求收费；穷尽前禁止 no-match/推进 cursor；多 marker 命中为 conflict |
| F8 | P2 | 预算引用不存在的 `api_quotas` 与滑动窗口，和 active storage 的 `budget_counters` 固定桶冲突 | 统一为 `budget_counters(kind=forge_api)` UTC 固定小时桶，并引用 outbox 稳定 charge key |
| F9 | P2 | `RateLimited` 声称携 reset 时间，但错误结构只有供人读的 `Summary` | `ClassifiedError` 增结构化 `RetryAt`；上层不解析诊断文本 |
| F10 | P2 | 企业实例 hostname、GitLab project path 编码和分页主机路由未冻结，可能请求错误主机/路径 | 每次 CLI 调用显式传 `ProjectRef.Host`；project key 做 path-segment 编码 |
| F11 | P2 | 文档称 M1 三个动词签名已冻结且不得改，与上述缺口冲突 | 明确 M1 是缩小骨架；M2 必须同步迁移 fake，不能把骨架冒充完整契约 |

## 3. 字段级核对

- **13 个动词**：均有用途、完整参数、返回值、目标类型与平台路由；Issue/Change 评论创建统一为 `CommentTarget`，总数不变。
- **事实投影**：Issue closure、Change URL/state/head/mergeability/review/draft、Checks 失败 jobs 均有中性字段；未知值不伪装通过。
- **actor**：评论与标签驱动事件在边界丢弃缺 actor 项；事实观测不误套 actor 闸门。
- **幂等**：评论/Change marker 必须全分页查询；Change marker 唯一命中才可接管；merge 请求远端校验 expected head。
- **边界安全**：argv 启动、正文走 stdin、hostname 显式、stdout/stderr 分流、摘要脱敏。
- **预算**：每个真实 HTTP 请求在唯一收费口扣费；组合端点和分页不漏扣；本地上限在启动 CLI 前拒绝。

## 4. 交叉补丁

- [`PRD.md`](../PRD.md)：动词表补 `TargetRef`、返回游标、Issue closed 事实与泛化目标评论/标签。
- [`WBS.md`](../WBS.md)：M2「先写 spec」项勾选并链接本报告。
- DESIGN 无需复制字段；其 §8.1 已按文档纪律链接 forge spec，结构性结论未改变。

## 5. 验收判断

- forge.md `active`：**YES**
- 评审报告入库：**YES**
- PR 合并：**NO（由 conductor 执行；本 worktree 不 push/MR/merge）**

剩余风险均属于后续实现验证：真实 GitHub/GitLab fixture 需确认企业实例 hostname 参数、Approvals 套餐差异、rate-limit header 与 expected-head CAS；这些已进入 V3/V7 门禁，不阻断规格转 active。
