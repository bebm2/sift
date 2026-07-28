---
status: draft
created: 2026-07-29
summary: Forge 适配层的最小动词集签名、中性类型、平台归一、actor 契约与错误分类
---

# Forge 适配层规格

本文冻结 Forge 适配层的端口契约：PRD §5.2 最小动词集（完整签名与中性类型）、平台归一规则、actor 必填、Change marker 全状态查找、merge expected-head CAS、错误分类、argv 边界与 API 预算收费口。

来源：[PRD §5.2、§5.4、§9.2](../PRD.md)、[DESIGN §8.1](../DESIGN.md)、[WBS M2 §2.1–§2.5](../WBS.md)。现存 M1 fake 骨架（[`internal/forge/forge.go`](../../internal/forge/forge.go)）定义了 `Kind`、`ProjectRef`、`Issue`、`LabelEvent`、`Change`、`Cursor`、错误分类与 `Client` 接口的**三个动词**（`ListIssuesByLabel`、`ListLabelEvents`、`GetChange`）；本文冻结全部 13 个动词、所有中性类型、双平台归一细节与副作用对账端口，是 M2 实现的共同契约。M1 fake 未实现的动词（`GetIssue`、`ListIssueComments`、`CommentIssue`、`SetLabels`、`CreateChange`、`FindChangeForCreateOperation`、`GetChangeDiff`、`ListChangeComments`、`GetChecks`、`MergeChange`）均在此定义，实现阶段不得另立签名。

## 评审处置

初稿，待评审。

## 1. 不变量

1. `gh api` / `glab api` 是唯一 forge 通道；verb 优先使用 `api` 子命令（plumbing）而非 porcelain。任何动词不得绕过 CLI 直调 HTTP。
2. 平台字段（`number`/`iid`、`mergeable_state`/`detailed_merge_status`、Checks vs Pipelines、Draft 前缀）在边界归一为领域中性类型，不得泄漏到上层。
3. 两平台无法都给出确定性答案时，归一结果是显式 `unknown`，由上层转 HITL——**适配器不得猜测**。
4. actor 是类型的一部分：`ListLabelEvents`、`ListIssueComments`、`ListChangeComments` 返回类型中 actor 为必填字段；取不到 actor 的事件在适配器内即**丢弃**（fail closed，DESIGN §8.1 / PRD §9.2）。
5. 进程调用一律 argv 数组启动，**禁止 shell 拼接**。
6. 错误分类只暴露五种语义：`Transient` | `RateLimited` | `AuthOrCapability` | `ContractViolation` | `SemanticConflict`。平台 HTTP 状态码 / 退出码 / stderr 细节锁定在适配器内部。
7. API 预算只在 Forge 适配层收费；上层不感知 `gh`/`glab` 的速率限制细节。
8. 副作用对账是端口能力，不是上层自行查询：`FindChangeForCreateOperation`（跨全状态 marker 搜索与冲突检测）、`MergeChange`（expected-head CAS）由适配器实现，outbox worker 不得用 raw API 旁路。
9. 能力探测只探测**已配置项目实际引用到的** forge；探测失败拒绝启动 daemon，不做运行时降级（PRD §9.3 / DESIGN §11）。
10. 所有动词签名以本规格为准；M1 fake 已实现的三个动词（`ListIssuesByLabel`、`ListLabelEvents`、`GetChange`）签名与本规格一致，实现阶段不得修改。

## 2. 基础类型

以下类型已在 M1 fake 中定义（[`internal/forge/forge.go`](../../internal/forge/forge.go)），本文冻结其语义：

### `Kind`
```
"github" | "gitlab"
```
Forge 平台族。能力探测按此枚举分支。

### `ProjectRef`
```
Kind       Kind
Host       string   // 规范化主机名，如 github.com
ProjectKey string   // 如 org/repo
```
唯一标识一个已配置的 forge 项目。

### `Issue`
```
ID     string
Title  string
Body   string
Author string   // 必填；缺失为 ContractViolation
URL    string
Labels []string // 适配器排序去重
```
领域中性 Issue 投影。`Labels` 由适配器排序去重。

### `LabelAction`
```
"added" | "removed"
```

### `LabelEvent`
```
TargetID   string      // issue 或 change id
Label      string
Action     LabelAction
Actor      string      // 必填；缺失则整条事件在适配器内丢弃
ObservedAt time.Time
```
actor 取不到时整条事件丢弃——调用方永远不会看到空 Actor 的 `LabelEvent`。

### `ChangeState`
```
"open" | "merged" | "closed"
```
三个状态是两平台可合并性语义的保守交集；`mergeable_state` / `detailed_merge_status` 这类平台特有字符串永不透出。

### `Change`
```
ID       string
HeadSHA  string
State    ChangeState
MergedAt time.Time   // 仅 State=merged 时非零
```
领域中性 Change 投影。`HeadSHA` 是 merge 的 expected-head 契约锚点。

### `Cursor`
```
string  // 适配器不透明游标
```
分页游标。上层只保存、透传，不解析内容。

## 3. 错误分类

已由 M1 定义（[`internal/forge/forge.go`](../../internal/forge/forge.go)），本文冻结为唯一暴露面：

| Sentinel | 语义 | 上层动作 |
|----------|------|---------|
| `ErrTransient` | 网络抖动 / 临时不可用 | 指数退避重试 |
| `ErrRateLimited` | 速率限制，含远端 reset 时间 | 尊重 `Retry-After`/`RateLimit-Reset` 并联动 API 预算降速 |
| `ErrAuthOrCapability` | 凭证失效 / 权限不足 | 停止该项目摄入 + 产生一次告警，不循环轰炸 |
| `ErrContractViolation` | 响应结构不合预期（必填字段缺失、类型错误） | 保留响应摘要，fail closed |
| `ErrSemanticConflict` | 语义冲突（Change marker 未命中而同 base/head 有冲突、merge stale head 等） | 重读事实源后重判或转 HITL |

所有错误经 `ClassifiedError` 包装：
```
Class   error   // 上述 sentinel 之一
Summary string  // 适配器诊断摘要（不含凭证）
```

`ClassifiedError` 实现 `Unwrap()`，上层用 `errors.Is` 分类。

## 4. 最小动词集（完整 13 个）

以下为 PRD §5.2 最小动词集的完整签名。每个动词注明对接的 `gh` / `glab` 端点和归一要点。M1 fake 已实现的前三个动词在此重复仅为闭环，签名不变。

### 4.1 `ListIssuesByLabel`
```
ListIssuesByLabel(ctx, project, label, since Cursor) → ([]Issue, Cursor, error)
```
**用途**：增量摄入。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/issues?labels={label}&since={since}&sort=updated&direction=asc&state=open`。只取 `pull_request` 为 null 的条目（排除 PR，Issue 与 PR 在 GitHub API 中共用编号空间）。
- GitLab：`glab api projects/{id}/issues?labels={label}&updated_after={since}&order_by=updated_at&sort=asc&state=opened`。
- 游标：规范化为适配器不透明 string。GitHub 用 `since` 参数 + 最后一条的 `updated_at` 作为下次 `since`；GitLab 同用 `updated_after`。游标的解析仅发生在适配器内。
- Labels：适配器排序去重。
- Author：取自 `user.login`（GitHub）/ `author.username`（GitLab），缺失即 `ContractViolation`。
- 游标只在一批全部持久化后推进（由 Intake 层保证，适配器不负责）。

### 4.2 `GetIssue`
```
GetIssue(ctx, project, issueID string) → (Issue, error)
```
**用途**：读取单个 Issue 详情与正文。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/issues/{number}`。
- GitLab：`glab api projects/{id}/issues/{iid}`。
- 注意：GitLab 使用 `iid`（项目内编号），不是全局 `id`。适配器以 `iid` 作为 `issueID`；调用方传入的是项目内编号。
- Body 保留原始 markdown，不做平台差异清洗。

### 4.3 `ListIssueComments`
```
ListIssueComments(ctx, project, issueID string, since Cursor) → ([]Comment, Cursor, error)
```
**用途**：读取审批指令与人工补充（含 `/sift *` 指令）。**必须返回 `author`**。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/issues/{number}/comments?since={since}&sort=created&direction=asc`。
- GitLab：`glab api projects/{id}/issues/{iid}/notes?sort=asc&order_by=created_at`（按 `created_at` 过滤）。
- Actor：取自 `user.login`（GitHub）/ `author.username`（GitLab）。**缺失则丢弃该条评论**——这是 C8 的 fail closed 实现点之一（不影响同批次其他评论）。
- 增量游标语义同 `ListIssuesByLabel`。

### 4.4 `ListLabelEvents`
```
ListLabelEvents(ctx, project, targetID string, since Cursor) → ([]LabelEvent, error)
```
**用途**：读取标签增删事件及其 actor——§9.2 的闸门依赖它。**已在 M1 实现**。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/issues/{number}/timeline`（按 `labeled`/`unlabeled` 事件过滤），或 `gh api /repos/{org}/{repo}/issues/{number}/events`。
- GitLab：`glab api projects/{id}/issues/{iid}/resource_label_events`。
- Actor：GitHub `actor.login` / GitLab `user.username`。**缺失则丢弃整条事件**。

### 4.5 `CommentIssue`
```
CommentIssue(ctx, project, issueID, body string) → (commentID string, error)
```
**用途**：发送决策简报 / 确认回执 / 告警评论。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/issues/{number}/comments -f body='...'`。
- GitLab：`glab api projects/{id}/issues/{iid}/notes -f body='...'`。
- 返回远端 comment/note ID 供幂等对账（marker 搜索的 fallback）。
- comment body 内嵌不可见 marker（`run_id + nonce + op_key`），由 outbox 层注入、适配器透传——适配器不负责生成 marker。

### 4.6 `SetLabels`
```
SetLabels(ctx, project, issueID string, add, remove []string) → error
```
**用途**：状态投影与触发标签管理。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/issues/{number} -f labels[]=...` 覆盖式设置。先读当前 labels，计算 `(current ∪ add) \ remove`，一次性 PUT。
- GitLab：`glab api projects/{id}/issues/{iid} -f labels=...` 同覆盖式。
- 标签集合的 set 语义天然幂等（DESIGN §6.4）。
- add 和 remove 不允许同时包含同一 label（调用方保证）。

### 4.7 `CreateChange`
```
CreateChange(ctx, project, branch, base, title, body string) → (Change, error)
```
**用途**：**由 Sift 创建 Change**（PRD §5.1——Agent 不创建 Change）。成功后将远端 Change ID 返回并持久化。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/pulls -f head='...' -f base='...' -f title='...' -f body='...'`。返回 `number` 作为 `ID`、`head.sha` 作为 `HeadSHA`、`state` 映射 `"open"`、`draft` 为 false 才创建（Sift 不创建 draft）。
- GitLab：`glab api projects/{id}/merge_requests -f source_branch='...' -f target_branch='...' -f title='...' -f description='...'`。返回 `iid` 作为 `ID`、`sha`（来自 `diff_refs.head_sha`）作为 `HeadSHA`。
- body 内嵌 `op_key` marker，与评论 marker 同机制、同一实现。
- 创建前**不检查**同 base/head 是否已存在 Change——那是 `FindChangeForCreateOperation` 的职责，outbox worker 先查后建。

### 4.8 `FindChangeForCreateOperation`
```
FindChangeForCreateOperation(ctx, project, opKey, branch, base string) → (*Change, FindResult, error)
```
**用途**：为创建操作做崩溃对账——跨开启 / 关闭 / 已合并状态查找 operation marker，并返回同 base/head 的无 marker 冲突。**这是端口能力，outbox worker 不得绕过**（DESIGN §6.4）。

**`FindResult` 枚举**：
```
"marker_hit"       // marker 命中，返回该 Change
"no_match"        // marker 未命中，且同 base/head 无冲突
"semantic_conflict" // marker 未命中，但同 base/head 存在无 marker 的 Change
```

**归一要点**：
- marker 搜索：对开启 / 关闭 / 已合并的 Change 列表按 body 搜索 `op_key`。GitHub 用 `gh api /repos/{org}/{repo}/pulls?state=all&head={org}:{branch}&base={base}` + body 搜索；GitLab 用 `glab api projects/{id}/merge_requests?state=all&source_branch={branch}&target_branch={base}` + description 搜索。
- 同 base/head 冲突：任何状态下的 Change，若 body 不含 `op_key` marker，即判 `semantic_conflict`（PRD §6.4「绝不接管他人对象」）。**适配器只返回结果，不裁决——裁判规则在上层**。
- marker 命中但 Change 已关闭或已合并：仍返回该 Change（不创建新的），上层按 PRD §4.5 收敛为 forge 外部事实。

### 4.9 `GetChange`
```
GetChange(ctx, project, changeID string) → (Change, error)
```
**用途**：读取 Change 状态、可合并性、head sha、审查状态。**已在 M1 实现**。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/pulls/{number}`。`id` → `number`、`state` → `"open"/"closed"`、`merged_at` 非 null → `ChangeMerged`、`head.sha` → `HeadSHA`。
- GitLab：`glab api projects/{id}/merge_requests/{iid}`。`iid` → `ID`、`state` → `"opened"/"closed"/"merged"`、`merged_at` → `MergedAt`、`sha`（来自 `diff_refs.head_sha`）→ `HeadSHA`。
- 可合并性细节不返回：`mergeable` / `mergeable_state` / `merge_status` / `detailed_merge_status` 等字段仅在 `MergeChange` 的远端条件检查中使用（见 §4.12），不进入 `Change` 结构体。

### 4.10 `GetChangeDiff`
```
GetChangeDiff(ctx, project, changeID string) → (string, error)
```
**用途**：供 T3 风险评分（LLM 读 diff）。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/pulls/{number} -H "Accept: application/vnd.github.v3.diff"` 返回 unified diff。
- GitLab：`glab api projects/{id}/merge_requests/{iid}/changes` 中取 `changes[].diff` 拼接，或 `GET .../merge_requests/{iid}.diff`。
- 返回原始 unified diff 字符串。不做平台差异清洗（T3 的 prompt 已声明输入来源于 GitHub/GitLab）。

### 4.11 `ListChangeComments`
```
ListChangeComments(ctx, project, changeID string, since Cursor) → ([]Comment, Cursor, error)
```
**用途**：读取审批指令。**必须返回 `author`**。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/pulls/{number}/comments?since={since}&sort=created&direction=asc` + `/issues/{number}/comments`（PR 评论在 GitHub 上分两套 API，适配器合并并按 `created_at` 排序）。
- GitLab：`glab api projects/{id}/merge_requests/{iid}/notes?sort=asc&order_by=created_at`。
- Actor 缺失则丢弃（同 `ListIssueComments`）。
- 注意 GitHub 的 review comments（行内评论）与 issue-style comments 是不同端点；适配器只拉 issue-style comments（即 PR 对话评论），V0 不做行内评论解析。

### 4.12 `GetChecks`
```
GetChecks(ctx, project, headSHA string) → (CheckSuite, error)
```
**用途**：CI 状态与失败任务清单。

**归一要点**：
- GitHub：合并 Checks API (`/commits/{sha}/check-runs` / `check-suites`) 与 Statuses API (`/commits/{sha}/status`)。结论取最差（`failure` > `pending` > `success`）。
- GitLab：`glab api projects/{id}/pipelines?sha={sha}` + 对最新 pipeline 取 `status` 与 `failed_jobs`。
- CI 结论归一为 `"success" | "failure" | "pending" | "unknown"`（两平台无法确定时用 `"unknown"`，不猜）。
- `failed_jobs` 列表：每项含 `name`、`web_url`（链接到具体 job/run 供人查看）、`allow_failure`（允许失败的 job 不计入整体失败）。

**`CheckSuite` 结构**：
```
Conclusion    string       // "success" | "failure" | "pending" | "unknown"
FailedJobs   []CheckJob   // 仅 Conclusion=failure 时填充
ExternalURL  string        // CI 详情页 URL（GitHub: check suite URL; GitLab: pipeline URL）
```

**`CheckJob` 结构**：
```
Name         string
WebURL       string
AllowFailure bool
```

### 4.13 `MergeChange`
```
MergeChange(ctx, project, changeID, expectedHeadSHA, method string) → (Change, error)
```
**用途**：**条件合并**——远端当前 head 必须仍等于 Gate 裁定的 `expectedHeadSHA`，否则拒绝并返回 `SemanticConflict`（DESIGN §6.4 / ADR-011）。

**归一要点**：
- GitHub：`gh api /repos/{org}/{repo}/pulls/{number}/merge -f sha='{expectedHeadSHA}' -f merge_method='{method}'`。`sha` 参数提供原子 CAS——GitHub 会比较该 SHA 与当前 PR head，不匹配则拒绝。
- GitLab：`glab api projects/{id}/merge_requests/{iid}/merge -f sha='{expectedHeadSHA}' -f merge_method='{method}'`。GitLab 同样支持 `sha` 参数做原子比较（`merge when pipeline succeeds` 不适用：Sift 的 merge 发生在 Checks 已过之后，直接 `merge`）。
- `method` 取值：`"merge" | "squash" | "rebase"`。（两平台均支持这三种；GitHub 的 rebase 需 repo 开启 allow rebase merge，GitLab 需 fast-forward merge 配置。）
- **适配器无法提供 `sha` 参数的条件语义时**（如 Glab 的某些旧版本 `merge` 子命令缺少该参数，或该平台不支持 head CAS），`MergeChange` 返回 `ErrAuthOrCapability` 并标注 `capability_unsupported`。**上层接到该错误后将该项目 `auto_merge` capability 置为不可用，不得降级为无条件 merge**（ADR-011 / DESIGN §8.1）。
- 远端返回非 200 且原因是 head SHA 不匹配 → `ErrSemanticConflict`。
- 合并成功后返回更新后的 `Change`（`State=ChangeMerged`、`MergedAt` 取自远端响应）。

## 5. 辅助类型

### `Comment`
```
ID        string
Author    string   // 必填；缺失时整条 Comment 在适配器内丢弃
Body      string
CreatedAt time.Time
```
用于 `ListIssueComments` / `ListChangeComments` 的返回值。

### `FindResult`
```
"marker_hit" | "no_match" | "semantic_conflict"
```
`FindChangeForCreateOperation` 的返回枚举，见 §4.8。

## 6. 平台差异清单与归一函数

以下差异必须在适配层内显式处理，每项对应一个归一函数。上层不可见 `number`/`iid`、`mergeable_state`、`Draft:` 前缀这类平台原语。

| 概念 | GitHub | GitLab | 归一处理 |
|------|--------|--------|---------|
| 变更编号 | `number` | `iid`（项目内）与全局 `id` 不同 | `changeID` 统一用 `number`/`iid`；全局 `id` 在适配层内仅用于 API 路径（GitLab 多数端点用 `iid`） |
| Issue 编号 | `number` | `iid`（同变更） | `issueID` 统一用 `number`/`iid` |
| 可合并性 | `mergeable` (bool) + `mergeable_state` | `merge_status` + `detailed_merge_status` | 不进入 `Change` 结构体；仅在 `MergeChange` 内消费。无法确定时 `MergeChange` 的 CAS 自然拒绝 |
| CI | Checks API + Statuses API（两套） | Pipelines（单套） | §4.12 合并两套 + 取最差结论 |
| 审查通过 | Reviews (`APPROVED`) | Approvals（依赖版本/套餐，部分 API 需 Premium/Ultimate） | V0 不取审查状态。若后续需要，GitLab 侧 `approvals_left` 为 0 即通过；GitLab Free 无此端点则 `Change` 补 `ReviewState` 字段并标注 `unknown` |
| 草稿 | `draft` 布尔字段 | 标题 `Draft:` / `WIP:` 前缀 | `Change` 增 `IsDraft bool`（GitHub 取 `draft`，GitLab 取 `title` 前缀匹配时不区分大小写）。此字段待 M4 Gate 阶段加入 `Change` 结构体 |
| 合并方式 | merge / squash / rebase | merge / squash / ff（rebase 需项目配置） | 上层传 `method` 枚举，适配器原样映射到对应 CLI 参数 |
| 增量拉取 | `since` + `sort=updated` | `updated_after` | 归一为适配器不透明 `Cursor` |
| 标签事件 actor | issue events / timeline | resource label events | 统一为 `LabelEvent.Actor`，缺即丢弃 |

**判定原则**：遇到语义差异一律取保守交集。凡是两个平台无法都给出确定性答案的问题，归一结果显式 `unknown`，由上层转 HITL——**适配器不猜**。

## 7. actor 契约

这是 C8 与 PRD §9.2 在适配层的结构保证（DESIGN §8.1「actor 是类型的一部分」）：

| 动词 | actor 来源 | 缺失行为 |
|------|-----------|---------|
| `ListLabelEvents` | `LabelEvent.Actor` | 丢弃整条事件 |
| `ListIssueComments` | `Comment.Author` | 丢弃该条评论 |
| `ListChangeComments` | `Comment.Author` | 丢弃该条评论 |

注意：`Issue.Author` 与 `Change`（无 author 字段）不在此列——`Author` 是 Issue 的创建者，属于事实观测而非驱动性事件，不对其施加 actor 闸门（PRD §4.5）。

**实现约束**：丢弃行为发生在适配器内的归一函数，调用方永远看不到空 actor 的对象。这比在每个调用点记得检查更强——是类型系统保证的 fail closed。

## 8. 进程调用边界

所有 `gh` / `glab` 调用必须：

1. **argv 数组**启动子进程（如 `exec.Command("gh", "api", "/repos/...")`），不得使用 `sh -c` 或任何 shell 字符串拼接。
2. **stdin** 传递 body（`-f` / `--raw-field` 参数本身走 argv，不拼接）。
3. **超时**：每次调用带 context deadline，由上层注入。适配器不设自己的全局超时（不同动词的预期延迟不同）。
4. **stderr**：API 调用的 stderr 截断后进入错误 `Summary`（`ContractViolation` / `Transient` 时附带），但**不与 stdout 混淆**——API 响应始终从 stdout 解析。
5. **环境**：继承 `siftd` 的环境（确保 `GH_TOKEN` / `GITLAB_TOKEN` 等已登录 CLI 的鉴权可用）。

## 9. API 预算收费口

API 调用只在 Forge 适配层收费（DESIGN §9.2）。预算的唯一收费口是适配器内部的一个计数器函数，上层不感知 `gh`/`glab` 的速率限制。

| 规则 | 说明 |
|------|------|
| 每次 `gh`/`glab` 子进程调用计一次 | 不考虑 GraphQL 批量查询的 cost 因子（V0 简化）；即使远端返回 304 也计一次（本地无法区分 304 与真实请求的耗时差，简化为统一计数） |
| 预算状态持久化 | 当前窗口计数、窗口起始时间、剩余配额持久化到 `api_quotas` 表（[`storage.md`](storage.md)）。重启后从 DB 恢复，不重新计数 |
| 接近上限 | 消耗 ≥ 80% 配额时，Intake 间隔降级为慢轮询（PRD §5.2 的「接近上限自动降级为慢轮询」）。慢轮询间隔（默认 5 分钟）是配置项 |
| 达到上限 | 拒绝所有 forge 调用（含非 Intake 的 `CommentIssue`、`SetLabels` 等），返回 `ErrRateLimited`；产生一次告警级通知（走 Attention 发射器） |
| 远端 rate limit 联动 | `gh`/`glab` 返回 429/rate-limit 时，适配器从响应头或 JSON body 解析 `retry-after`/`reset` 时间，联动降低本地预算窗口消费速度 |
| 窗口 reset | 按配置的窗口长度（默认一小时）滑动；窗口边界依赖 `siftd` 时钟，重启不重置窗口 |

**收费点位置**：适配器内部统一的 `chargeAPICall(ctx, project)` 函数，每次子进程调用前先收费。这是 DESIGN §9.2 规定的「唯一收费口」。

## 10. 契约测试与 fixture 录制

双平台跑同一套契约测试（DESIGN §8.1 / WBS V3）：

- 用真实 CLI 输出录成 fixture（`testdata/fixtures/github/`、`testdata/fixtures/gitlab/`）。
- 覆盖：分页、actor 缺失、限流、平台差异（`number` vs `iid`、Checks vs Pipelines、Draft 前缀）、Change marker 跨全状态唯一查找与同 base/head 冲突、merge 的远端 expected-head CAS。
- 录制成本近零——开发时本来就在敲这些命令。
- fixture 入 git（go:embed）。
- 每个边界类型的 golden test（DESIGN §5.2）：`closed` 契约断言额外字段 / 必填缺失被拒；Forge `open-envelope` 契约断言无关新增字段接受、必需字段缺失/变型被拒。

## 11. M1 fake 边界

M1 的 `Fake`（[`internal/forge/fake.go`](../../internal/forge/fake.go)）实现了以下子集：

| 已实现 | 未实现（M2） |
|--------|-------------|
| `ListIssuesByLabel` | `GetIssue` |
| `ListLabelEvents` | `ListIssueComments` |
| `GetChange` | `CommentIssue` |
| | `SetLabels` |
| | `CreateChange` |
| | `FindChangeForCreateOperation` |
| | `GetChangeDiff` |
| | `ListChangeComments` |
| | `GetChecks` |
| | `MergeChange` |

M2 的 `Fake` 需扩展以覆盖全部 13 个动词，且每个新动词遵循与真实适配器相同的契约（actor 必填、错误分类、marker 搜索、merge CAS 等）。fake 的 scripted 数据必须携带所有必填字段；缺失即 panic（当前已如此），确保测试不能通过残缺数据伪装边界。

M2 fake 不要求模拟 API 预算（收费口测试另用 mock），不要求模拟远端 rate limit 联动。

## 12. 交叉引用

| 来源 | 关联 |
|------|------|
| PRD §5.2 | 最小动词集、平台差异清单、自适应轮询参数 |
| PRD §9.2 | actor 鉴权：驱动性事件必须解析 actor |
| PRD §5.4 | merge 必须条件合并 |
| DESIGN §8.1 | Forge 适配层职责、归一在边界完成、argv/错误分类/契约测试 |
| DESIGN §6.4 | 逐类副作用幂等协议（marker 搜索、expected-head CAS） |
| DESIGN §9.2 | API 预算唯一收费口 |
| ADR-011 | merge expected-head CAS |
| [WBS M2 §2.1–§2.5](../WBS.md) | M2 任务分解与门禁 |
| [`brain.md`](brain.md) | T1/T3/T5 消费 Forge verb 的输入 |
| [`storage.md`](storage.md) | `api_quotas` 表、outbox operation key |
| [`outbox.md`](outbox.md) | Change 创建 / merge 的 outbox worker 协议 |
| [`control-plane.md`](control-plane.md) | 与 forge 无关（siftd 内部控制面） |

---

_初稿 | 2026-07-29 | draft | 待评审_
