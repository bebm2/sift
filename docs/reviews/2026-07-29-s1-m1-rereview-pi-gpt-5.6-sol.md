# S1 / M1 实现结案定向复审

> 日期：2026-07-29
> 复审人：pi × GPT-5.6-sol
> 复审基线：`32675b7`（`origin/main`；复审分支 `chore/s1-m1-rereview`）
> 前次审计：[`2026-07-29-s1-m1-phase-review-pi-gpt-5.6-sol.md`](2026-07-29-s1-m1-phase-review-pi-gpt-5.6-sol.md)

## 结论

**M1 FAIL。**

前次两个主要实现/范围缺口已有实质处置：doctor 已不是固定 warning 的 stub，而会读取配置并检查 runtime、SQLite、Agent/Brain/Forge CLI、按配置启用的 tmux 及 home/socket 权限；Intake 评论 crash marker 与旧 generation 回复仲裁也已在 WBS、brain/storage/outbox specs 中一致迁至 M2，M1 只保留已实现的 Brain replay JSONL。

但 doctor 的 CLI 退出语义尚未闭合。`internal/controlplane/doctor.go` 正确计算并返回 `exit_code = 0 | 1 | 2`，`cmd/sift/main.go` 却只打印结果：offline 路径无条件 `return`，online 路径只按 RPC `response.OK` 决定是否退出 1。因此存在 error 的 `sift doctor --offline` 仍以进程状态 0 退出，online doctor 的 warning/error 同样不会映射为 1/2。这违反 [`config.md` §7](../specs/config.md) 对 **`sift doctor` 退出码**的确定性契约，不能仅以 JSON 内含同名字段替代 shell 可观察退出状态。

此外，前次要求的 Linux listener 成功记录仍未取得；本次 `gh run list` 对处置提交及 merge commit 均返回空列表，本机 Darwin 也不能执行 build-tag 为 linux 的 `TestV10ZeroNetworkListeners`。它仍只能算可信测试实现，不能算已留存的 Linux 成功证据。

## 1. 处置核对

### 1.1 Doctor：真实实现，但 CLI 契约未完成

已确认的真实检查：

- `runtimeCheck` 报告 Go runtime/OS/arch；
- `storage.CheckReadOnly` 以只读连接执行 SQLite `PRAGMA quick_check`，offline 路径不创建或迁移数据库；
- 配置中的全部 Agent、可选 Brain、启用项目的 Forge CLI version/login 及按 backend 启用的 tmux 会被实际解析和执行；
- home、配置、数据库、token、lock、工作目录和双 socket 的类型/权限会被检查；
- `unsafe-local` warning 继续保留，未伪称 V0 隔离闭合；
- 新测试覆盖配置依赖成功、SQLite/权限错误及 socket 类型错误。

因此 #29 不是占位实现。阻断点位于 CLI 对检查结果的最终传播，而非检查本身。复现实验在空的 0700 `SIFT_HOME` 执行 `sift doctor --offline`，得到：

```text
process_exit=0
"id": "sqlite"
"level": "error"
"exit_code": 2
"offline": true
```

应让命令进程按 doctor result 退出 0/1/2，并增加 cmd 层测试同时覆盖 online/offline，避免只测试内部 map。

### 1.2 Intake scope：通过

#30 的范围变更在以下位置对齐：

- WBS M1 §1.7 只勾选 replay JSONL，并明确 crash/generation 依赖 M2 的真实 Forge worker、receipt 消费与 `PersistIntakeDecision`；
- WBS M2 §2.3、§2.5 和 M2 门禁均有明确工作项；
- `brain.md` 将验收拆为 M1 replay 与 M2 Intake crash/generation；
- `storage.md` 将 Intake 投影写端口标记为 M2；
- `outbox.md` 将真实 marker 执行与崩溃窗口标记为 M2。

未发现以 schema、operation key 或通用 outbox 框架冒充 M2 Intake 已实现的表述。该项不再阻断 M1。

### 1.3 状态文档：有待同步

`AGENTS.md` 已不再声称仓库“尚无代码”，但仍写 doctor “待实现”；WBS M1 §1.5 的 doctor 基线也仍为未勾选。鉴于本次复审仍为 FAIL，复选框保持未完成是诚实的；但 AGENTS 应改为准确指出当前剩余的 CLI exit propagation，而不是把整个 doctor 描述为未实现。

## 2. 验证记录

在 `32675b7` 执行：

```text
CGO_ENABLED=0 go test ./...    PASS
```

所有 Go package 通过，包括新增 `internal/controlplane` doctor tests。另执行 `git diff --check` 通过。

Linux listener 证据查询：

```text
gh run list --commit 7d1a757 ...   []
gh run list --commit 7ee3700 ...   []
gh run list --commit 0b87ca1 ...   []
gh run list --commit 32675b7 ...   []
```

因此本复审不虚构 hosted Linux 执行结果。

## 3. 解除阻断的最小条件

1. 让 `sift doctor` 与 `sift doctor --offline` 的进程退出状态传播 doctor 计算出的 0/1/2，并以 cmd/进程级测试覆盖 clean、warning、error。
2. 更新 `AGENTS.md` 与 WBS §1.5 的当前事实；只有上述行为闭合后才勾选 doctor 基线。
3. 在 Linux CI 留存 `TestV10ZeroNetworkListeners` 的成功记录，并在下一次复审中引用可核验的 run/commit。

在这些条件完成前，M1 不满足退出门禁。
