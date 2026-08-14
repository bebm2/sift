# 独立复审 Issue #913 — curl|bash one-line installer（M8 §8.1）

review_round: 1（无历史关闭包；独立审核 worktree 只读，未改生产代码）
实施分支：`feat/issue-913-curlbash-one-line-installer-scripts-installsh-re` @ 3a1960a
审核方式：`bash -n` + 本地假归档/假 checksums 全路径实测（mock curl/uname、隔离 `SIFT_INSTALL_ROOT`）+ `CGO_ENABLED=0 go build ./...`

## 已验证通过（简档）

- 语法：`bash -n scripts/install.sh` OK；`CGO_ENABLED=0 go build ./...` OK。
- happy path（latest API 解析 `v0.1.0`；darwin/linux × amd64/arm64）：`~/.sift/bin/<version>/{sift,sift-agent-wrapper,manifest.json}` 落位，`current -> 0.1.0` 相对 symlink，`sift --version` 验证通过。
- fail-closed：checksum 不匹配（sha256sum 与 shasum 两分支均 rc=1、无残留）；checksums.txt 缺条目 rc=1；损坏 gzip rc=1；缺 `sift`/`sift-agent-wrapper` rc=1。
- 版本安全：`SIFT_VERSION='../../etc/passwd'`、`0.1.0..evil` 均被 `normalize_version` 拒绝；`--version` 与 `SIFT_VERSION` 均生效（CLI 优先）。
- 平台探测：`Windows`、`ppc64le` 明确报错退出；`x86_64→amd64`、`aarch64→arm64` 映射正确。
- 恶意成员：`../evil` 被成员检查拒绝；trap 清理临时目录，失败后无 `bin/.staging-*`/tmp 残留。
- README：顶部一键行可复制、`SIFT_VERSION=0.1.0` 示例、快速开始改用安装器、无「待实现/尚未开始/早期」残留；状态表述与 AGENTS.md 一致（M1–M6 完成、M7 PoC 已验证、M8 自动化核心持续完善）。

## Findings

### [P1] F1 — `current` symlink 切换在重跑/升级时失效（违反「重跑升级」「原子切换」验收）
- 描述：`mv -f "$link_tmp" "$current"` 在 `current` 已存在（symlink→目录）时把临时链接**移入**目标目录而非替换；实测 0.1.0 安装后再装 0.2.0，`readlink current` 仍为 `0.1.0`，且 `bin/0.1.0/` 内累积 `.current-tmp-0.1.0-*`、`.current-tmp-0.2.0-*` 残留——脚本打印「Installed Sift 0.2.0」但 daemon 仍跑旧版本。
- 关闭标尺：
  ```bash
  export SIFT_INSTALL_ROOT="$(mktemp -d)/.sift"
  bash scripts/install.sh            # latest=0.1.0
  SIFT_VERSION=0.2.0 bash scripts/install.sh
  readlink "$SIFT_INSTALL_ROOT/bin/current"        # 期望 0.2.0（实测 0.1.0）
  find "$SIFT_INSTALL_ROOT/bin" -name '.current-tmp-*' | wc -l   # 期望 0（实测 >0）
  ```
  期望 NO（即修好后为 YES）。
- 证据缺口：无，已实测复现（darwin/arm64，`mv -f` 语义跨 GNU/BSD 一致）。
- 建议修法：`ln -sfn "$version" "$current"`，或先 `rm -f "$current"` 再 `mv`（保留 temp+rename 意图）。
- fixer=same（agent::gpt-5.6-luna；首轮实现缺陷回原实施）。

### [P2] F2 — 成员检查未拒绝 symlink/hardlink 成员（release.md §3 契约要求拒绝）
- 描述：安全检查只验路径模式（绝对/`../`），symlink 成员可携带并通过 `[ -f ]` 检查被安装执行；实测归档中 `sift -> real-sift` 安装成功 rc=0 且目标被执行。属防御纵深缺口（checksum 与脚本同源认证，非提权向量）。
- 关闭标尺：构造 `sift` 为 symlink 的归档安装，应拒绝；现为通过。期望 NO。
- 证据缺口：无，已实测。
- fixer=same（P2 仅记录，不进当前 MR；可并入后续单点 Issue 或在 release.md 注明 curl|bash 与 `sift install` 的保证度差异）。

### [P2] F3 — `|| fail 'could not inspect archive'` 为死代码
- 描述：`tar -tzf` 失败时 while 循环因 `read` EOF 退出码恒 0，`|| fail` 永不触发；损坏 gzip 落到后续 `tar -xzf` 报「could not extract archive」（仍 fail-closed，仅消息不精确）。
- 关闭标尺：损坏 gzip 应输出「could not inspect archive」；现为「could not extract archive」。期望 NO。
- 证据缺口：无，已实测。
- fixer=same（P2 仅记录）。

### [DEFER] F4 — 未认证 api.github.com latest 查询受 60/hr/IP 限流
- 403 时安装失败（`SIFT_VERSION` 钉版本可绕过）；后续可加 gh 认证或镜像/缓存。backlog。

### [DEFER] F5 — curl|bash 路径不执行 manifest closed-schema / 二进制 sha256 / 版本握手（spec §3 仅 `sift install` 保证）
- 建议 release.md 补一节说明两条安装路径的保证度差异，避免读者误以为 curl|bash 满足 spec §3 全契约。backlog。

## Scope summary

| 级别 | 数量 | 本轮是否实施 |
|---|---|---|
| P0 | 0 | 是 |
| P1 | 1 | 是 |
| P2 | 2 | 否（记录） |
| DEFER | 2 | 否（backlog） |

## Verdict

**NEED-FIX** — P1 F1（`current` symlink 切换在升级/重跑时失效）未关；P2/DEFER 不阻塞合并。
