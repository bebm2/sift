---
status: active
created: 2026-08-18
summary: agent family 声明式契约：如何非交互驱动一个 coding agent CLI
---

# Agent Family 规格

本文是 `internal/agentfamily` 包与 `agents[].family/model/thinking`（[`config.md` §3.2](config.md)）的字段级契约，解决 issue #1024：coding agent 的非交互启动参数、model/thinking 覆盖、登录凭据与非机密行为变量因 CLI 而异，此前只能靠用户手写启动脚本绕过 Sift 的凭据隔离（[ADR-007](../decisions/007-tm6-minimal-credential-sandbox-direction.md)）。

## 1. 分层

- **Family**（本文档，`internal/agentfamily`）：一种 coding agent *软件* 的知识——它怎么被非交互调用、哪些环境变量可能携带登录态、model/thinking 怎么映射成 argv。与具体某台机器、某个用户无关，可以内置也可以用户自定义。
- **Instance**（`config.yaml` 的 `agents[]`，[`config.md` §3.2](config.md)）：一个具体 Agent 实例，引用 family id（`family` 字段）并携带该实例的覆盖值（`model`/`thinking`）与既有的 `executable`/`args`/`launch_env`。

`config.yaml` 不知道、也不校验 family 的存在性或其 flags 是否支持某个覆盖值——那是 family 集合加载后、启动时的校验（见 §4）。这样 `internal/config` 不需要依赖 `internal/agentfamily`，避免包间循环。

## 2. Family 文档结构

一个 family 是一份 YAML 文档，字段：

| 字段 | 类型 | 约束 |
|------|------|------|
| `id` | string | 必填；`^[a-z][a-z0-9-]{0,62}$`，同一有效集合内唯一 |
| `match` | `string[]` | 必填非空；可执行文件 basename 列表，用于按 `agents[].executable` 自动识别 family |
| `run.args` | `string[]` | 必填非空；新建 Agent 实例时 `agents[].args` 的默认值种子 |
| `run.version_args` | `string[]` | 可选；启动探测 argv，默认沿用 `config.md` 的 `["--version"]` |
| `run.flags` | `map[string][]string` | 可选；覆盖名（如 `model`、`thinking`）→ argv 片段，片段中必须恰有一个 `{value}` token |
| `auth.env` | `string[]` | 可选；可能携带登录凭据的环境变量名（不是值） |
| `config.env` | `string[]` | 可选；影响行为但非凭据的环境变量名（如 base URL） |
| `config.dirs` / `config.files` | `string[]` | 可选；该 agent 的配置目录/文件路径，供未来"用户自行改了配置"检测使用，当前仅作声明，未接检测逻辑 |

解码走 `config.YAMLToJSON`（单文档、无重复 key、无别名环）后用 `json.Decoder.DisallowUnknownFields` 拒绝未知字段——复用 config 包已有的严格 YAML 桥接，不另起一套解析器。

## 3. 加载与合并

- `agentfamily.Builtin()`：内置 family，来自 `internal/agentfamily/builtin/*.yaml`（`go:embed`），当前覆盖 claude / codex / cursor / opencode / pi。
- `agentfamily.LoadUserDir(dir)`：读取用户目录下的 `*.yaml`；目录不存在不算错误（视为空）。
- `agentfamily.Load(userDir)`：内置集合按 id 被同 id 的用户文件**整体覆盖**（不是逐字段合并），未出现的 id 直接追加。
- `agentfamily.Match(families, executable)`：按 `filepath.Base(executable)` 在 `match` 列表中查找；多个 family 声明同一名字时取 id 字典序最小者，保证确定性。

## 4. 启动时解析

1. `sift init` / `sift agent add`（`cmd/sift/agentfamily.go`）用 `agentfamily.Load(SIFT_HOME/agent-families)` 得到有效 family 集合，按 `agentfamily.Match` 用 `agents[].executable` 的 basename 找到匹配 family 并写入 `agents[].family`；同时用 `agentfamily.SnapshotEnv` 从当前 shell 捕获该 family 声明的 `auth.env`/`config.env`，合并写入 `SIFT_HOME/agent-secrets/<agent-id>.env`（0600，`agentfamily.WriteSecretsFile`）。两个环境变量桶（机密/非机密）目前存进同一份文件——都必须在启动时注入 `cmd.Env`，拆两份文件当前没有额外收益。这份文件永不进入 `config.yaml`。
2. Daemon 启动时（`cmd/sift/daemon.go`）用同一个 `agentfamily.Load` 得到有效集合，一次性传给 `launchworker.Worker.Families`；`SecretsDir` 指向 `SIFT_HOME/agent-secrets`。`config.md §1.3` 的"不 hot-reload"约束在这里同样适用：family 集合和 daemon 生命周期一样长。
3. 每次派工（`internal/launchworker/launch.go` `Worker.RunOnce`，在 `runtime.BuildQualification` 之前）调用 `agentfamily.ResolveArgs(Families, {FamilyID, Model, Thinking}, agent.Args)`：`family` 为空是空操作；`family` 非空但查不到、或 `model`/`thinking` 非空但该 family 的 `run.flags` 未声明 → 启动期失败（fail-closed，不静默忽略）。渲染出的 argv 片段按固定顺序（`model` 后 `thinking`）追加在 `agents[].args` 之后，不影响 `{task_file}` 占位符位置。
4. 同一次派工调用 `agentfamily.ResolveLaunchEnv(family, SecretsDir, agent.ID, agent.LaunchEnv)`，按这个优先级合并进冻结的 `launch_env`（HOME/PATH）：init 快照的 secrets 文件作兜底；family 声明的 `config.files` 里 JSON `{"env":{...}}`（如 `~/.claude/settings.json`）覆盖同名变量。只注入该 family `auth.env`/`config.env` 列出的名字，不把配置文件里的任意键灌进进程。文件不存在不是错误；文件存在但不是合法 JSON 则 fail-closed。`~` 按冻结的 `HOME` 展开。这样 CC Switch 一类只改 Agent 自己配置文件、不改 shell 的工具，下次派工就会用到新值，不必重跑 `sift init`。解析发生在拓扑资格哈希（`topology_qualification_key`）计算之前，所以该哈希反映的是最终会被 exec 的完整 argv/env。
5. 解析结果写入 `bootstrap.json`（`runtime.BootstrapAgent.Args`/`LaunchEnv`）后即被冻结：一次已准备的派工恢复（resume）时会重新跑同一套解析，若 family 集合或 secrets 文件在两次之间发生变化，`matchesDispatch` 的逐字段比较会拒绝复用旧 bootstrap，而不是静默地用不一致的环境继续启动。

**当前不做的事**：`sift init`/`agent add` 尚不提供交互式 prompt 来设置 `model`/`thinking`（用户目前需要手改 `config.yaml`）；`config.dirs` 尚未接入漂移检测；非 JSON 的配置文件（如 Codex 的 `config.toml`）尚未解析——第二个需要它的 family 出现后再加，不预埋第二种解码器。守护进程仍然不继承用户终端的环境变量，也不热加载 `config.yaml`。

## 5. 内置 Family 清单

| id | 默认 args | model flag | thinking flag | auth.env |
|---|---|---|---|---|
| claude | `-p` | `--model` | 未声明（Anthropic 文档未确认专门的 CLI thinking flag） | `ANTHROPIC_API_KEY`、`ANTHROPIC_AUTH_TOKEN` |
| codex | `exec -` | `--model` | `--config model_reasoning_effort=` | `OPENAI_API_KEY` |
| pi | `-p` | `--model` | `--thinking` | `ANTHROPIC_API_KEY`、`OPENAI_API_KEY`、`OPENROUTER_API_KEY`、`GEMINI_API_KEY`（多 provider） |
| cursor | `--print` | `--model` | 未声明 | `CURSOR_API_KEY` |
| opencode | `run` | `--model` | `--variant` | `ANTHROPIC_API_KEY`、`OPENAI_API_KEY`、`GOOGLE_GENERATIVE_AI_API_KEY`、`OPENROUTER_API_KEY`（多 provider） |

来源为各 CLI 官方文档（2026-08 核实）；`match` 列表见对应 `builtin/*.yaml`（cursor 额外匹配 `cursor-agent`/`agent` 两个别名）。未声明的 flag 不是遗漏，是该 CLI 当前没有对应的确认过的启动参数——不臆造。
