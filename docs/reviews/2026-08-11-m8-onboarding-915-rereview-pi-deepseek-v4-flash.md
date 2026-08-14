# 独立复审 Issue #915 — onboarding：install.sh next-steps + opt-in auto-PATH + README getting-started

review_round: 1（无历史关闭包；独立审核 worktree 只读，未改生产代码）
实施分支：`feat/issue-915-onboarding-installsh-next-steps-output-opt-in-au` @ 3e39df0
审核方式：`bash -n` + 本地假归档/假 checksums + fake curl mock 全路径实测（隔离 `SIFT_INSTALL_ROOT`/`HOME`，覆盖默认、`SIFT_AUTO_PATH=1`、`--add-to-path`、unsupported shell、rc 写入失败、重跑 dedup）+ README 与 `docs/specs/config.md` §2.1/§3.1–3.3/§3.14 逐条比对 + CLI 命令面核对 + `CGO_ENABLED=0 go build ./...`

## 已验证通过（简档）

- 语法：`bash -n scripts/install.sh` OK；`CGO_ENABLED=0 go build ./...` OK（exit 0）。
- 默认路径（zsh/bash 双测）：仅打印 next-steps，rc 文件零改动（`echo "# original" > .bashrc` 前后一致），exit 0。
- Next-steps 块四项齐全：① 按 `$SHELL` 的 zsh→`~/.zshrc`/bash→`~/.bashrc` 确切命令 + `source`；② `gh auth login`/`glab auth login`；③ `~/.sift/config.yaml` 最小配置 + `chmod 600`，指向 config.md §3.1–3.3 / installation.md / README；④ `sift doctor --offline` → `sift daemon`/`sift service install`。
- opt-in auto-PATH：`SIFT_AUTO_PATH=1`（zsh）与 `--add-to-path`（bash）均正确追加 `export PATH="<root>/bin/current:$PATH"`；重跑命中 `grep -Fqx` 去重、仅 1 条 entry 不重复追加；unsupported shell（fish）只警告不写；rc 只读时警告不失败（exit 0 且提示手动加 PATH）。
- `chmod 700 "$INSTALL_ROOT"` 实测生效（`stat` 为 `drwx------`），与 daemon `EnsureHomeLayout` 的 `HomeDirMode 0o700` / config.md §2.1 一致。
- README getting-started 七步（安装/PATH/登录/最小 config/doctor/daemon/trigger+ps/timeline）齐全；config 示例字段与 config.md §3.1–3.3 完全一致（含必填 `version: 1`、`forge.host` 默认 github.com 注释、`enabled`、`agents` 引用）；`sift ps` 的「isolation_state / attention_remaining」描述经 `internal/storage/ops_ps.go` 核对属实；`sift:run` trigger 默认值与 config.md §3.14 一致；所有 CLI（doctor --offline / daemon / service install / ps / timeline）均在 cmd/sift/main.go usage 实存。
- 非范围合规：仅改 `scripts/install.sh` + `README.md`；`.goreleaser.yml`/Release 归档未动；未新增 `sift init`；auto-PATH 严格 opt-in。
- 无回归：#913 P1（`ln -sfn "$version" "$current"`）、P2（symlink/hardlink 成员拒绝、`if ! tar -tzf` 检查）均已在本分支基线上闭合，未受本 commit 影响。

## Findings（本轮无 P0/P1）

### [P2] D1 — dedup 只匹配「展开后」绝对路径，手工按 README 加的 `$HOME` 字面行不参与去重
- 描述：README 手工步骤写入的是 `$HOME` 字面量（单引号不展开），installer 写入的是展开后绝对路径；先手工加、后 `SIFT_AUTO_PATH=1` 会得到两条 PATH entry（功能无害但破坏「重跑不重复」严格语义、rc 变脏）。
- 关闭标尺：`echo 'export PATH="$HOME/.sift/bin/current:$PATH"' >> ~/.zshrc` 后跑 `SIFT_AUTO_PATH=1` 安装，`grep -c 'export PATH' ~/.zshrc` 应为 1；现为 2。期望 NO（即修好后 YES）。
- 证据缺口：已按 mock 实测同构复现（展开/字面两条）；真实用户序列未跑。
- 建议：dedup 再匹配 `$HOME` 字面形式，或改按 `# Added by the Sift installer` marker 去重。
- fixer=same（P2 仅记录，不进当前 MR）。

### [P2] D2 — `chmod 700 "$INSTALL_ROOT"` 无条件执行，会静默收紧「已存在」的安装根权限
- 描述：对全新 curl|bash 安装根是脚本自建目录、无影响；但当 `SIFT_INSTALL_ROOT` 指向预先存在且有意的组共享目录时，不打招呼就收成 0700（与 §2.1 daemon 要求一致、方向正确，但属未 opt-in 的外部目录变更）。
- 关闭标尺：预建 `mkdir -m 755 "$root"` 后安装，安装前后 `stat -f %Sp "$root"` 应不变；现为 755→700。期望 NO。
- 证据缺口：mock 已验证 700 生效路径；预建 755 场景按代码路径推断（未逐测）。
- fixer=same（P2 仅记录）。

### [P2] D3 — install.sh onboarding 路径无仓库内回归测试，验收「mock 实测」仅靠手工
- 描述：`.github/workflows/build.yml` 只跑 hosting-smoke-test.sh，`bash -n`/mock 安装矩阵（默认只打印、auto-PATH 追加、dedup、unsupported shell）无 CI 覆盖，后续改动易回归。
- 关闭标尺：`bash -n scripts/install.sh` + mock 全矩阵入 CI（或 scripts/ 下测试脚本），PR 需含回归步；现为无。期望 NO。
- 证据缺口：无（CI 现状已核实）。
- fixer=same（P2 仅记录）。

### [DEFER] D4 — rc 探测仅覆盖 zsh/bash（按 Issue 范围），fish 等只给通用提示
- 描述：符合 Issue 明确范围（非缺陷）；fish（`~/.config/fish/config.fish`）等可作后续扩展 Issue。
- 关闭标尺：后续 Issue 覆盖 fish 时 `SIFT_AUTO_PATH=1` 应写入 `~/.config/fish/config.fish` 且 dedup。backlog。

## Scope summary

| 级别 | 数量 | 本轮是否实施 |
|---|---|---|
| P0 | 0 | 是 |
| P1 | 0 | 是 |
| P2 | 3 | 否（记录） |
| DEFER | 1 | 否（backlog） |

## Verdict

**PASS** — 验收四项（`bash -n`、mock 默认只打印 / auto-PATH 追加+重跑 dedup、README 可复制且 config 与 §3.1–3.3 一致、`CGO_ENABLED=0 go build ./...` 不回归）全部实测通过，P0/P1 全关（0 项）；P2×3 + DEFER×1 仅记录，不阻塞合并/Closes（由主指挥决定）。
