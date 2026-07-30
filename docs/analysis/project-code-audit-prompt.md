---
status: active
created: 2026-07-30
summary: 一份可复制到其他项目的提示词，用于审计代码量是否过度、膨胀根因分析
---

# 项目代码量过度工程审计 — 提示词

> **用途**：将此提示词复制到你要审计的另一个项目（仓库），交给 AI 编码代理执行。AI 会分析项目的代码规模、文档规模、各层占比，并输出根因分析和改进建议。
>
> **前置条件**：AI 需要有仓库的读写权限（读 git log、代码行数统计、文档目录结构）。
>
> **预期产出**：一份结构化的 <output-path> 文件，包含代码规模数据、过度区域定位、根因分析和改进建议。

---

## 提示词开始

你是一个项目审计专家。请对当前仓库执行一次**代码量/过度工程审计**。

### 目标

评估当前项目的代码规模是否与其阶段（V0/PoC/生产）匹配，识别过度工程区域，分析根因，并给出可操作的改进建议。

### 约束

- 数据必须绑定当前仓库的实际状态，不允许虚构或假设
- 如果无法获取某个指标，明确标注「无法获取」
- 产出写入 <output-path>，采用 Markdown 格式
- 输出必须是**独立的、自包含**的文件，不依赖仓库内其他文档

### 第一阶段：数据采集

执行以下命令并收集结果。

```bash
# 1. 项目基本信息
echo "=== 项目年龄 ==="
git log --reverse --format="%ci %s" | head -1
echo "现在: $(date +%Y-%m-%d)"
echo ""
echo "=== 总 commit 数 ==="
git log --oneline | wc -l
echo ""
echo "=== 代码行数统计（不含 worktree/vendor 副本）==="
echo ""

# 2. 按语言统计
echo "=== 语言占比 ==="
# Go 项目示例，请根据实际语言调整
find . -name '*.go' -not -path './.git/*' -not -path './vendor/*' -not -path '*/node_modules/*' | xargs cat 2>/dev/null | wc -l
find . -name '*.rs' -not -path './.git/*' -not -path './vendor/*' | xargs cat 2>/dev/null | wc -l
find . -name '*.ts' -o -name '*.tsx' -not -path './.git/*' -not -path '*/node_modules/*' | xargs cat 2>/dev/null | wc -l
find . -name '*.js' -not -path './.git/*' -not -path '*/node_modules/*' | xargs cat 2>/dev/null | wc -l
find . -name '*.py' -not -path './.git/*' -not -path '*/node_modules/*' | xargs cat 2>/dev/null | wc -l
echo ""

# 3. 包级代码分布
echo "=== 包级代码行数 ==="
# 按顶层包/目录聚合（排除 test 文件）
find . -name '*.go' -not -path './.git/*' -not -path './vendor/*' -not -name '*_test.go' | sed 's|/[^/]*$||' | sort -u | while read d; do
  lines=$(find "$d" -maxdepth 1 -name '*.go' -not -name '*_test.go' | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}')
  testlines=$(find "$d" -maxdepth 1 -name '*_test.go' | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}')
  if [ -n "$lines" ] && [ "$lines" != "0" ]; then
    echo "  Prod: ${lines}L  Test: ${testlines:-0}L  $d"
  fi
done | sort -rn
echo ""

# 4. 文档规模
echo "=== 文档统计 ==="
find . -name '*.md' -not -path './.git/*' -not -path '*/node_modules/*' -not -path './vendor/*' | xargs wc -l 2>/dev/null | tail -1
echo ""
echo "=== review 类文档 ==="
find . -path '*/review*' -name '*.md' -not -path './.git/*' 2>/dev/null | wc -l
echo ""

# 5. 测试函数数
echo "=== 测试函数数 ==="
# 按语言调整
rg '^func Test' --type go 2>/dev/null | wc -l || echo "(rg not available)"
echo ""
echo "=== 最大包的目录结构 ==="
# 找出行数最多的目录
maxdir=$(find . -name '*.go' -not -path './.git/*' -not -path './vendor/*' -not -name '*_test.go' | sed 's|/[^/]*$||' | sort -u | while read d; do
  lines=$(find "$d" -maxdepth 1 -name '*.go' -not -name '*_test.go' | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}')
  echo "$lines $d"
done | sort -rn | head -3)
echo "$maxdir"
echo ""
echo "=== 文件分布 ==="
find . -name '*.go' -not -path './.git/*' -not -path './vendor/*' -not -path '*/node_modules/*' | wc -l
echo ""
echo "=== 测试/生产比例 ==="
prod=$(find . -name '*.go' -not -path './.git/*' -not -path './vendor/*' -not -path '*/node_modules/*' -not -name '*_test.go' | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}')
test=$(find . -name '*_test.go' -not -path './.git/*' -not -path './vendor/*' -not -path '*/node_modules/*' | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}')
echo "Production: $prod lines"
echo "Test: $test lines"
echo "Ratio: $(echo "scale=2; $test / $prod" | bc 2>/dev/null):1"
```

### 第二阶段：过度区域识别

基于采集数据，逐一检查以下「过度信号」。对每个命中的信号，给出行数估计和具体文件/包名：

| # | 过度信号 | 检查方法 |
|---|---------|----------|
| 1 | **持久层过大** | 检查数据库/存储层行数。单机 SQLite/PoS 项目 >5K 行通常是过度抽象 |
| 2 | **配置解析过大** | config 包 >1K 行需要审查。V0 产品不需要复杂的配置分层 |
| 3 | **基础设施提前实现** | 检查 schema 生成器、编解码框架、插件系统等「工具自身的工具」是否存在且无外部调用方 |
| 4 | **Backlog 功能已实现** | 对照项目需求文档（如果有 PRD 或 README 中的范围说明），检查「明确不做 / 未来版本」的功能是否已有代码 |
| 5 | **review 文档 > 代码的 30%** | 计算 review 类 .md 的行数 / Go 生产代码行数 |
| 6 | **测试/生产比 > 0.8:1** | 超过 80% 的测试覆盖率在 V0 中通常是过度投资 |
| 7 | **双平台/双后端 day-1 但只用一个** | 是否存在抽象的接口 + 两个实现，但生产只使用其中一个 |
| 8 | **包拆分过于细粒度** | 检查是否有 3+ 个包各 <300 行，它们本可以合并 |
| 9 | **边缘情况有完整处理路径** | 是否有「在 V0 几乎不可能触发」的边缘情况（崩溃恢复竞态、跨进程 fencing、分布式一致性协议等）被完整实现 |
| 10 | **文档 > 生产代码** | .md 行数 > Go 生产代码行数 |

### 第三阶段：根因分析

从以下已知模式中识别与本项目匹配的根因。对每条匹配的根因提供**量化证据**（多少行代码由这个原因产生）：

| # | 根因 | 典型表现 | 证据收集方法 |
|---|------|---------|-------------|
| R1 | **复审驱动开发（RDD）** | review 文档多、每个 review 都产生代码变更、边缘情况层层叠加 | review 文档数 / 代码行数比；review 轮次多的模块的代码行数 |
| R2 | **没有「不修」选项** | AI reviewer 从不建议 defer、PRD/README 中的范围声明被忽略 | 检查 backlog 或「V0 不做」的功能是否已实现 |
| R3 | **结构性保证过度提前** | schema 生成器、编译期校验框架、自定义解码器在没有调用方时已实现 | 检查 `internal/` 中无外部引用的「工具」包 |
| R4 | **文档化一切** | 每项决策都有独立 ADR、每个模块都有独立 spec、每次变更都有独立 review 文档 | .md 总行数 / 生产代码行数 > 0.5 |
| R5 | **双/多平台抽象 day-1** | 抽象接口 + 多实现，但 V0 只用其中一个 | 检查 interface 的实现数量 vs 实际使用数量 |
| R6 | **「AI 喜欢把事情做完整」** | 没有配置或配置极少的模块也有完整的分层（接口、多个 impl、factory、test suite） | 检查只被一处引用的接口、只有一个实现的抽象 |

### 第四阶段：改进建议

针对每个识别出的过度区域和根因，给出具体建议：

1. **可立即做的**（不破坏当前代码）：停止继续扩展、添加 TODO 注释标记 V0 边界
2. **可推迟的**：明确标注「V1 之前不实现」
3. **可删除/合并的**：合并细粒度包、删除无调用方的框架代码
4. **流程改进**：修改 review 流程，避免同一模式继续产生膨胀

### 输出模板

将分析结果写入 <output-path>，格式如下：

```markdown
---
title: <项目名> 代码量审计报告
date: <当前日期>
baseline_commit: <最近一次 commit SHA>
---

## 1. 规模总览

| 指标 | 数值 |
|------|------|
| 项目年龄 | ... |
| 总 commit | ... |
| 生产代码 | ... 行 |
| 测试代码 | ... 行（测试/生产比 ...） |
| 文档 | ... 行 |
| review 文档 | ... 份，... 行 |
| 包/模块数 | ... |

## 2. 过度区域识别

| 信号 | 命中? | 涉及包/文件 | 估计行数 |
|------|-------|-----------|---------|
| 持久层过大 | ... | ... | ... |
| 配置解析过大 | ... | ... | ... |
| 基础设施提前 | ... | ... | ... |
| Backlog 已实现 | ... | ... | ... |
| 文档 > 生产代码 | ... | ... | ... |
| ... | ... | ... | ... |

## 3. 过度系数估算

**实际生产代码**: X 行
**合理 V0 代码**: X 行（说明估算依据）
**过度系数**: X.X 倍

## 4. 根因分析

| 根因 | 证据 | 贡献行数估计 |
|------|------|-------------|
| R1 复审驱动开发 | ... | ... |
| R2 没有不修选项 | ... | ... |
| ... | ... | ... |

## 5. 改进建议

| 优先级 | 建议 | 预估效果 |
|--------|------|---------|
| P0 | ... | ... |
| P1 | ... | ... |
| P2 | ... | ... |

## 6. 总结

一句话结论。
```

---

## 提示词结束

### 使用说明

1. 将上面的完整提示词（从「### 提示词开始」到「## 提示词结束」）复制到目标项目的仓库根目录
2. 替换 `<output-path>` 为实际输出文件路径，如 `docs/analysis/code-audit-report.md`
3. 如果项目不是 Go 语言，调整第一阶段命令中的文件匹配模式（`.py` / `.rs` / `.ts` 等）
4. 如果目标项目没有 `rg`（ripgrep），替换为 `grep -rn '^func Test' --include='*.go'`
5. 交给 AI 代理执行

### 已知局限

- 行数统计≠工作量/复杂度，仅供参考
- 过度系数估算需要判断力，不同审查者可能有不同判断
- 建议报告生成后由人工复核「合理 V0 代码」的估算值
