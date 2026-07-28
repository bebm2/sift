---
status: active
created: 2026-07-27
summary: 外部副作用走 transactional outbox，至少一次执行 + 幂等收敛
---

# ADR-003 外部副作用用 transactional outbox

结构展开见 [DESIGN §6.4](../DESIGN.md)。

## 决策

所有不能与 SQLite 共事务的副作用——forge 评论、打标、创建 Change、合并、通知推送、启动 agent——**先在状态转移的同一事务里写入 outbox 记录，再由 worker 执行**。每项副作用有稳定 operation key；worker 按至少一次语义执行。

**投递语义逐类声明，不由 outbox 通用推导。** outbox 本身只保证「提交后不被静默遗忘」；「恰好一次」必须由每一类动作各自的远端证据或本地互斥挣来，没有证据的类别如实降级：

| 类别 | 收敛手段 | 语义 |
|------|---------|------|
| 打标、发评论、合并 | 远端证据查询（标签 set 语义 / 评论内嵌 marker / 按 head sha 查合并态） | effectively-once |
| **创建 Change** | body 内嵌 `op_key` marker + **跨开启/关闭/已合并全状态搜索** + 成功后按远端 ID 收敛（见下） | effectively-once |
| Channel 推送 | 无可靠远端证据 | **至少一次**，消息携带可见去重标识 |
| 启动 agent | operation CAS + lease、wrapper session、唯一 spawn permit、不可换代的 `spawning` handoff（[ADR-010](010-attempt-spawn-handoff.md)） | effectively-once：每 attempt 一个 permit，任一时刻一个存活 writer |

逐类协议的字段级展开在 `specs/outbox.md`。

**创建 Change 的证据必须唯一定位 operation**（D0.3 修订，评审 R5-F3）。初稿用的证据是「同 `(base, head branch)` 下存在**开启的** Change」，它不成立：远端创建成功而本地记账前崩溃、期间人把 Change 关掉，重试就会再建一个；同 base/head 上人已手工建过 Change 时，Sift 会把别人的对象当成自己的 operation 结果接管；分支复用时 base/head 也区分不了两次逻辑 operation。协议因此改为五步：内嵌 marker → 全状态搜索 → 成功后持久化远端 ID 并此后按 ID 收敛 → marker 命中已关闭/已合并者按 forge 外部事实收敛而不重建 → 同 base/head 但 marker 缺失或不符判 `SemanticConflict` 转 HITL，**绝不接管**。

不采用纯事件溯源（同 [ADR-002](002-reconciler-and-single-transition-entry.md)）：当前投影与事件流同事务写入，outbox 记录也在这同一个事务里。

## 理由

**这解决的是一个必然发生的场景，不是一个边缘场景。** 系统在两个不可共享事务的世界之间操作：本地 SQLite 可回滚，forge 与操作系统不可回滚。任何「先调 API 再写库」或「先写库再调 API」的裸写法都有一个崩溃窗口，落在窗口里的结果是二者之一：

- 库说没发过评论，forge 上却有一条 → 人收到无法回复的孤儿 Interrupt，或重复推送耗配额（PRD §5.5 记账口径被破坏）；
- 库说 Change 已创建，forge 上没有 → Run 卡在一个不存在的对象上，等到超时。

merge 尤其致命：重复合并、或「以为没合并而重试」都会污染主干，而 PRD §10.2 把误放行列为质量红线。

Outbox 把问题收敛成一个可测的性质：**只要事务提交了，副作用就不会被静默遗忘**（DESIGN 场景 Q5）。稳定 operation key（如 `run:{run_id}:create-change`、`interrupt:{interrupt_id}:publish`、`command:{forge_event_id}:ack`、`run:{run_id}:merge:{head_sha}`）让 worker 重启后能识别自己上次做到哪。key 里带 `head_sha` 是有意的：head 变了就是另一次合并，不该被旧 key 判为重复。

**但「不被遗忘」不等于「不重复」**，这一点在初稿里被含混过去了：本地 operation key 加「先查询再动作」仍留两个窗口——查询确认未执行后与重试并发执行；远端已成功而本地记账前崩溃。因此上表把每一类的语义写死，而不是让读者从「有 outbox」推出「恰好一次」。启动 agent 是其中唯一一个「重复执行会产生两个并行写同一 worktree 的进程」的动作，所以它不用远端幂等收敛，而用 operation lease 挡会话内双派发、session + permit 挡 RPC 重放、`spawning` handoff 挡先后 owner 重叠（ADR-010）。

**「有远端证据」和「远端证据能唯一定位本次 operation」是两件事。** 创建 Change 的初稿协议满足前者、不满足后者，于是 effectively-once 的声明落空。这条教训适用于将来新增的每一类副作用：登记投递语义时要问的不是「能不能查到东西」，而是「查到的东西能不能证明**正是这次** operation 已执行」。

配套一条不可省的规则：**推送失败不改变「Run 已进入 `waiting_human`」这个事实**，但必须持续重试并告警。否则一个静默失败的 Interrupt 等于把 Run 挂死——PRD A3「通知是核心功能」在实现层就是这一条。告警不能寄托在「备用通道」上（V0 只有一个 Channel，PRD §7.3），走 forge 告警评论这条与 Channel 独立的通路，见 DESIGN §6.4。

## 放弃的选项

| 选项 | 放弃理由 |
|------|---------|
| 裸调用 + 出错重试 | 崩溃窗口无法消除；重复与丢失都不可观测 |
| 两阶段提交 / 分布式事务 | forge 与 CLI 不提供参与者语义，不可能实现 |
| 依赖 forge 侧幂等参数 | 两平台覆盖不全、语义不一；只能作为适配器内的收敛手段之一，不能作为机制 |
| 完全同步执行（事务内调 API） | 长事务阻塞单写者，破坏 tick 循环的时序保证 |

## 后果

- 正面：Q5 成为可注入崩溃验证的性质（DESIGN §12 V2、V7）；重启后的副作用续跑不需要专门逻辑。
- 负面：每个副作用都要设计其「远端证据」查询方式或互斥协议，适配器工作量增加；副作用有延迟（worker 推进），因此确认回执不是即时的。
- 负面：Channel 推送只能做到至少一次，人可能收到重复通知——**这是如实接受的代价，不是待修的缺陷**；代价被限制为「重复对人可辨认」。
- 中性：outbox 表成为运维可见对象——积压即信号，纳入 `sift doctor`；worker 由事务提交唤醒且有最大推进延迟目标（DESIGN §6.1），否则「最终生效」会被固定优先级饿死。
