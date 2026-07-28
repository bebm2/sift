# S1 / M1 实现结案第二次定向复审

> 日期：2026-07-29
> 复审人：pi × GPT-5.6-sol
> 复审基线：`82a4a36`（`origin/main`；复审分支 `chore/s1-m1-rereview-2`）
> 前次复审：[`2026-07-29-s1-m1-rereview-pi-gpt-5.6-sol.md`](2026-07-29-s1-m1-rereview-pi-gpt-5.6-sol.md)

## 结论

**M1 PASS WITH NOTES。**

前次 FAIL 的两个剩余阻断均已解除：#34 将 offline 与 online doctor 结果中的 `exit_code` 映射到 `sift` 进程退出状态，#35 在 hosted Linux CI 中显式执行并留存了 `TestV10ZeroNetworkListeners` 的具名 PASS 记录。前次已通过的 doctor 真实检查与 Intake M2 范围归属未发生倒退。

唯一注记不阻断 M1：V0 当前固定报告 `unsafe-local` warning，因此真实 doctor 结果在当前安全姿态下不会产生 clean/0；进程级实跑可达并验证了 warning/1 与 error/2，0 映射由同一 `main -> os.Exit(run(...))` 路径及 int/JSON float 两种结果形态的单元测试覆盖。该现状与 V10b 要求持续暴露未闭合边界一致，不应为制造 exit 0 而隐藏 warning。

## 1. #34：doctor 进程退出状态

`cmd/sift/main.go` 现在由 `main` 唯一执行 `os.Exit(run(...))`。两条 doctor 路径均先输出 JSON，再返回结果内的退出码：

- offline：`OfflineDoctor -> emitDoctor -> doctorExitCode`；
- online：`OperatorRequest -> response.Result -> doctorExitCode`。

`cmd/sift/main_test.go` 覆盖：

- offline error 返回 2；
- offline warning 返回 1；
- online warning 返回 1；
- offline `int` 与 online JSON `float64` 形态的 0/1/2 全部映射；
- daemon 不可用仍返回 1，而不伪装为 doctor clean。

本次另以构建出的真实 `sift`/`siftd` 进程验证：

```text
offline_error_process_exit=2 json_exit_code=2
offline_warning_process_exit=1 json_exit_code=1
online_warning_process_exit=1 json_exit_code=1
mandatory_warning=true
```

因此 shell 可观察状态与 JSON 结果一致，符合 [`config.md` §7](../specs/config.md)。clean/0 当前不可由真实 doctor 产生，是固定 `operator-token-readable-by-agent` warning 的预期结果；映射测试确认 clean 结果会返回 0。

## 2. #35：Linux 零网络 listener 证据

`82a4a36` 的 GitHub Actions run [30391415900](https://github.com/miaoxiaoyong/sift/actions/runs/30391415900) 在 Ubuntu `vet + test` job 中成功完成具名步骤 `V10 zero-listener evidence (linux)`。日志包含：

```text
=== RUN   TestV10ZeroNetworkListeners
--- PASS: TestV10ZeroNetworkListeners (0.04s)
PASS
ok github.com/miaoxiaoyong/sift/internal/controlplane 0.043s
```

该步骤执行：

```text
CGO_ENABLED=0 go test -v -run TestV10ZeroNetworkListeners ./internal/controlplane/
grep -q -- "--- PASS: TestV10ZeroNetworkListeners" v10.log
```

run 的 `headSha` 为 `82a4a3638d3dc0ec49ed908b9d4ea206f7e25d9b`，整体 conclusion、`vet + test` job 与具名证据步骤均为 `success`。这关闭了前次因 Darwin 本机无法编译 linux build-tag 测试而留下的证据缺口。

## 3. 回归与文档一致性

在 `82a4a36` 执行：

```text
CGO_ENABLED=0 go test ./...                                      PASS
CGO_ENABLED=0 go test -v ./cmd/sift -run 'TestDoctorExitCode|TestRunDoctor'  PASS
git diff --check                                                 PASS
```

`AGENTS.md` 已准确指向前次定向复审并描述 #34 状态；WBS M1 §1.5 已将 doctor 基线及 0/1/2 进程退出映射勾选完成。Intake crash marker 与旧 generation 回复仲裁仍明确归属 M2，未发现以 M1 骨架冒充 M2 实现的回退。

## 4. 最终判定

前次列出的最小解除条件均满足：

1. doctor online/offline 的结果已传播为进程退出状态并有 cmd 层覆盖；
2. AGENTS/WBS 已同步当前事实；
3. Linux CI 已留存具名 listener 测试成功记录。

故 S1/M1 退出门禁通过，可以进入 M2；上述 clean/0 可达性注记仅记录 V0 强制安全 warning 的现状，不构成遗留阻断。
