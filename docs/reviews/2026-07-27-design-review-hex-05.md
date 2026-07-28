# DESIGN.md 架构评审（第 5 轮，D0.2 复评）

> 日期：2026-07-27  
> 评审对象：[`docs/DESIGN.md`](../DESIGN.md) D0.2（对应 PRD V0.3，工作区未提交版本）  
> 评审依据：[`docs/PRD.md`](../PRD.md)、[`docs/README.md`](../README.md)、active ADR、前四轮设计评审  
> 评审重点：上一轮问题核销、修订后机制的可实现性、Gate 缓存、attempt 启动协议、幂等证据与文档对账

## 1. 结论

**D0.2 有明显进步，但仍不建议直接进入 WBS 或实现。**

上一轮的 F1、F3、F5、F6 已关闭；其中 F1 走的是评审允许的“V0 不闭合但如实声明、可诊断”分支，而不是已经获得进程隔离。F2 与 F4 虽补充了逐类副作用语义、attempt 生命周期和恢复矩阵，但新方案本身仍有实现断点，因此只能判为**部分关闭**。

本轮发现 3 个 P1 与 3 个 P2。三个阻断项分别是：Gate 缓存键没有覆盖完整函数输入；wrapper 无法在现有单写者与授权模型下取得 DB claim；创建 Change 的远端证据不足以支撑 effectively-once。它们均会直接破坏 D0.2 新增的核心保证。

因此，本评审不同意 design-review-04 的“通过”结论。review-04 对修订文本是否出现做了充分核销，但没有继续验证新机制能否在既有边界下实际运行。

## 2. 上轮发现核销

| 上轮编号 | 结果 | 说明 |
|----------|------|------|
| F1 · 控制面越权 | **关闭（条件分支）** | 双 socket 与 operator capability 已定形；V0 同 UID 越权仍未闭合，但已进入 TM6、`sift doctor` 与 V10 |
| F2 · Outbox 恰好一次 | **部分关闭** | Q5 已正确收窄，Channel 也如实声明至少一次；但创建 Change 的证据仍不唯一，agent claim 协议不可实现 |
| F3 · 认证投影 | **关闭** | 合法消费者、版本快照、有效策略与缓存失效路径均已补齐 |
| F4 · 启动恢复窗口 | **部分关闭** | attempt 生命周期和矩阵已补，但 claim 获取、token bootstrap 与 fencing 尚未闭合 |
| F5 · Outbox 饥饿 | **关闭** | 独立 worker、提交唤醒、有界配额与推进延迟测试已定义 |
| F6 · 手工合并冲突 | **关闭** | PRD V0.3 已裁决事实优先，`gate_bypassed` 与指标口径完整对账 |

## 3. 问题汇总

| 编号 | 级别 | 问题 | 主要影响 |
|------|------|------|----------|
| R5-F1 | P1 | Gate 缓存键遗漏可变输入 | 复用过期 Checks / review / mergeability verdict |
| R5-F2 | P1 | attempt claim 没有可实现的获取与 fencing 协议 | 违反单写者或产生重复 Agent |
| R5-F3 | P1 | 创建 Change 的证据不是唯一 operation 标识 | 重复创建或错误接管他人 Change |
| R5-F4 | P2 | 项目策略失败的隔离范围矛盾 | 单个坏项目可能拖垮整个 daemon |
| R5-F5 | P2 | ADR-006 仍描述单 socket / 单鉴权模型 | 实现代理可能恢复旧设计 |
| R5-F6 | P2 | PoC 验收被错误声明为全部自动化 | WBS 可能遗漏手机端人工验收 |

## 4. 详细发现

### R5-F1 · P1：Gate 缓存键遗漏可变输入

**位置**：DESIGN §6.5 第 295 行、§8.5 第 420–430 行；ADR-004 第 13–23 行。

Gate 的签名是：

```text
gate(changeFacts, policy, riskScore) → verdict
```

但缓存键只有：

```text
(run_id, head_sha, gate_version, effective_policy_hash, certification_version)
```

`head_sha` 只能代表代码内容，不能代表完整的 `changeFacts`。同一个 SHA 下，以下事实都会变化：

- Checks 从 pending 变成 success 或 failed；
- CI rerun 使已经成功的 Checks 再次失败；
- review / approval 状态变化；
- mergeability 从 unknown / conflicted 变化；
- T3 的 riskScore 或其 prompt/schema 版本变化。

因此两个不同的 Gate 输入可以命中同一个缓存键。轻则 pending verdict 永久阻塞，重则 CI 重新失败后仍复用此前的放行 verdict。

**建议**：

1. 对完整冻结输入做规范化序列化并计算 `gate_input_hash`，以它作为主缓存依据；
2. 或将 Checks 快照、review 状态、mergeability、riskScore 及其生成版本全部纳入键；
3. Gate 回放记录与缓存必须引用同一个输入快照 ID，避免“回放存一份、缓存判另一份”；
4. V6 增加“同 head、Checks/review/mergeability 变化必须 miss cache”的断言。

### R5-F2 · P1：attempt claim 没有可实现的获取与 fencing 协议

**位置**：DESIGN §3.2、§8.4 第 384–405 行、§10.1；ADR-005。

D0.2 同时规定：

- `siftd` 是唯一状态写入者；
- wrapper 不应直连 SQLite；
- `run.sock` 只有 Report 动词；
- wrapper 必须先原子取得 DB 唯一 claim；
- run token 要到 claim 之后才由 wrapper 写入 `control.json`。

这使 claim 没有可实现的获取路径：

- wrapper 直写 DB 会破坏单写者模型；
- wrapper 经 `run.sock` 请求 claim，则该端点没有 claim 动词，并且认证 token 尚未产生；
- daemon 在启动 wrapper 前代为 claim，则“重复 wrapper 取不到即退出”的语义还需要 owner 身份，否则合法 wrapper 也只能看到 claim 已存在。

同时，恢复矩阵允许 daemon 在“claim 已落但进程不存在”时释放 claim。没有 owner nonce / fencing token 时，一个启动较慢、尚未写 `control.json` 的旧 wrapper 可能在释放后继续执行，而新 wrapper 已取得新 claim，最终双起 Agent。

run token 的 bootstrap 也未定义：如果由 daemon 生成，需要说明如何在不进入环境变量或 argv 的前提下交给 wrapper；如果由 wrapper 生成，需要一个已认证通道注册给 daemon。

**建议**：

1. 明确定义 bootstrap capability，由 daemon 在 attempt 事务中生成 owner nonce；
2. 通过继承文件描述符、一次性 bootstrap socket 或等价机制交给 wrapper，不直写数据库；
3. claim 必须携带 owner nonce、代次和 fencing token；旧 owner 在启动真实 Agent 前再次验证 fencing；
4. 恢复释放/替换 claim 时递增 fencing generation，使旧 wrapper 即使苏醒也无法启动；
5. 将“claim 获取前、获取后、control 写入前、真实 Agent spawn 前”分别加入 V2/V4 崩溃与竞态测试。

### R5-F3 · P1：创建 Change 的证据不是唯一 operation 标识

**位置**：DESIGN §6.4 第 275 行；ADR-003 的逐类投递表。

当前协议把 `(base, head branch)` 下已存在的**开启中** Change 当作操作证据。这并不能唯一证明本次 operation 已执行：

- API 创建成功、本地尚未确认时，人可能关闭该 Change；重试只查开启状态会认为未创建并再建一个；
- 同一 base/head 已有用户手工创建的 Change 时，Sift 会错误接管他人的对象；
- 分支被重用或 attempt 变化时，base/head 不能区分两个逻辑 operation。

所以这项仍不能支撑表中声明的 effectively-once，也无法可靠恢复远端对象 ID。

**建议**：

1. 在 Change body 等稳定位置嵌入 `op_key` marker；
2. 查询开启、关闭、已合并的全部状态，而不只查询开启状态；
3. 成功后持久化远端 Change ID，后续一律按 ID 收敛；
4. 找到已关闭或已合并的匹配 marker 时，按 Forge 外部事实收敛，不重新创建；
5. 找到同 base/head 但 marker 不同的 Change 时转 `SemanticConflict`，不可直接接管。

### R5-F4 · P2：项目策略失败的隔离范围矛盾

**位置**：DESIGN §9.4 第 611 行与 §11 第 654 行。

§9.4 规定项目策略 schema 不合法时“拒绝接入该项目”，而 §11 把“项目策略”与 runtime、SQLite 等全局能力一起列入启动探测，并统一规定任一失败即拒绝启动 daemon。

两种语义的影响完全不同。按 §11 实现，一个坏仓库会让其他健康项目全部停止；这也与 §8.2 的每项目独立调度方向不一致。

**建议**：明确分级：SQLite、运行时和 daemon 全局配置失败属于进程级拒启；单项目 Forge 能力或 policy 失败只隔离该项目并告警。同步统一 §8.1、§9.4、§11 与 `sift doctor` 的状态口径。

### R5-F5 · P2：ADR-006 仍描述单 socket / 单鉴权模型

**位置**：ADR-006 第 44 行。

DESIGN 与 ADR-008 已明确采用 `siftd.sock` / `run.sock` 两个端点和两套能力，但仍为 active 的 ADR-006 在“后果”中写着：

> 上报与运维命令共用一条 socket 与一套鉴权

这与同一 ADR 前文及 ADR-008 直接冲突。实现代理若只加载 ADR-006，会恢复上一轮已经否定的单 socket 设计。

**建议**：删除或修正这条后果，并在 ADR-006 顶部增加醒目的“控制面部分由 ADR-008 修订”指针。若要求 ADR 正文只追加，则通过 amendment 段明确旧句失效，不要让两条 active 指令并存。

### R5-F6 · P2：PoC 验收并非全部可自动化

**位置**：DESIGN §12 第 678 行；PRD §10.1。

DESIGN 只排除了真实双平台链，却声称其余 PoC 标准全部实现为自动化门禁。PRD §10.1 明确要求“手机端验证一次”，该步骤依赖真实设备与人工操作；真实 HITL 审批也至少包含人工行为。

如果 WBS 直接继承当前表述，可能只生成自动化测试任务而遗漏人工验收记录。

**建议**：把成功标准拆为两组：

- 自动化门禁：状态机、硬护栏、预算、恢复、授权等；
- 人工验收证据：手机端审批、真实双平台低频链、真实 Agent 闭环等。

二者都应是发布条件，但不能把后者称为自动化门禁。

## 5. 建议修订顺序

1. 先修 R5-F2，定下 attempt bootstrap、claim ownership 与 fencing；它决定 ADR-003/005、恢复矩阵和 control-plane spec 的共同形状；
2. 修 R5-F1，将完整 Gate 输入快照纳入缓存；同步 ADR-004 与 V6；
3. 修 R5-F3，补齐 Change marker 与全状态远端对账；同步 ADR-003；
4. 统一 R5-F4 的项目级/进程级失败语义；
5. 清理 R5-F5 的 active ADR 漂移；
6. 修订 R5-F6 的验收分类，然后再进入 WBS。

## 6. 复评通过条件

- Gate 缓存键能唯一代表全部冻结输入，同 SHA 下外部事实变化必定失效；
- wrapper 无需直写 SQLite 即可取得带 owner 与 fencing 的 claim，且旧 wrapper 在 claim 被替换后无法启动 Agent；
- run token 的生成、下发、注册与失效不存在循环依赖；
- 创建 Change 的远端证据能跨开启/关闭/合并状态唯一定位 operation，不会接管无 marker 的对象；
- 项目级配置错误不会阻止健康项目运行；
- active ADR 对 socket 与鉴权模型只有一个当前结论；
- 自动化门禁与人工 PoC 验收证据分开列示。

