# control-plane.md 字段级评审

> 日期：2026-07-28
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/control-plane.md`](../specs/control-plane.md) 初稿
> 依据：DESIGN §3.2/§8.4/§8.9–§8.10/§10.1、ADR-005/008/010/012、active config/storage spec

## 1. 结论

**通过。** 当前候选稿可转 `active`。双 socket、版本握手、凭据分权、Report 边界、三段启动 handoff、控制文件、异步 kill/retry 与 offline 边界均已达到可实现的字段级闭合。

评审过程中发现的一项 P1 与三项 P2 已在候选稿中修正，修正后未留阻断项。

## 2. 发现与核销

### C1（P1）：持久 daemon 派生根扩大了 Agent 的启动权限

早期候选稿引入 `daemon.secret`，用它 HMAC 派生 bootstrap/run/session/permit。V0 Agent 与 daemon 同 UID，可读取该文件；这不只复现已知 operator 越权，还让 Agent 能派生 wrapper 启动凭据，直接冲突 ADR-008 的“Agent 侧不存在启动凭据”。

**已关闭：** 删除 daemon root。launch worker 使用 `PrepareLaunchDispatch` CAS 保存随机 run token/bootstrap nonce hash，再写 bootstrap；崩溃由 generation 与文件证据收敛。session/permit 由 wrapper 随机生成 candidate，daemon 只在 CAS 成功时授权并保存 hash。响应丢失可重放同 candidate，SQLite 不存明文。

### C2（P2）：launch operation 到 bootstrap/spawn 的崩溃窗口未展开

只写“worker 创建 bootstrap”无法回答事务提交后、文件 rename 前后、spawn 前后的恢复动作。

**已关闭：** 明确 prepare transaction → atomic bootstrap → spawn 顺序；文件缺失且无 owner 时递增 generation 重发，文件存在时新 lease owner 只能复用同 dispatch/digest。M1 验收增加逐点崩溃注入。

### C3（P2）：ops 读方法没有 closed response schema

初稿把 ps/logs/worktree/doctor 展示字段推给不存在的 CLI spec，与“所有请求/响应 closed”冲突。

**已关闭：** 定义四个方法的 params/result、分页、日志 base64 chunk、doctor exit code/security posture，且禁止任意 details 逃生口。

### C4（P2）：`task_transport=file` 缺通用 argv 绑定

把 task 路径直接追加为最后一个 argv 不适配要求显式 flag 的 Agent CLI。

**已关闭：** config 定义恰一个完整 `{task_file}` token；wrapper 只做单 token 替换，不做子串或 shell 展开。每 attempt 独立目录防止重试覆盖历史输入。

## 3. 通过项

- method 按 socket 注册，auth 为 closed tagged union；run token/bootstrap/session/permit 不能互换。
- protocol 与 binary version 均在参数 decode/领域动作前校验。
- started 只接受 Agent identity 或一致 result 证据，并统一进入 `ResolveAttemptRace`。
- `spawning` 的 kill/retry 只返回 accepted；absence 前不换 owner，kill 不创建新 attempt。
- Report 的 completed 只写 event；not_ready 只用于合法 spawning 窗口。
- bootstrap 读后 unlink；task/control/heartbeat/result 原子写且各有事实强度边界。
- offline 只有 `doctor --offline`，不迁移、不建 token、不清 socket、不写 DB。
- V10b 如实保留同 UID Agent 可读 operator token 的 `unsafe-local` 结论。

## 4. 后续边界

- operation payload 与逐类投递证据归 `specs/outbox.md`；不得重定义本规格的 dispatch/handoff 顺序。
- T1/T2 Task Spec 与 Brain schema 归 `specs/brain.md`；不得改变 task snapshot identity 和 transport 文件绑定。
- Report payload 的业务 schema 后续归 `specs/report.md`；授权、阶段和限流边界已由本规格冻结。
