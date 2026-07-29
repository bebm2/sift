FAIL

# report.md 字段级第五次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`ca8b4e4`（#365 / PR #367 合入；规格提交 `abdf113`）
> 评审对象：[`docs/specs/report.md`](../specs/report.md) draft
> 前轮结论：[`2026-07-29-m5-report-field-rereview-4-pi-gpt-5.6-sol.md`](2026-07-29-m5-report-field-rereview-4-pi-gpt-5.6-sol.md) 的 2×P1

## 1. 结论

**FAIL（1×P1）。** #365 已关闭共享 attempt arm 的 terminal identity 和两类 binding 的交叉拒绝 P1；也补出了 Report quota v1 的 fallback、合法 T4 input/output 与 mismatch cases。但新增 quota JSON 被明确标为 canonical/exact bytes，却没有按项目 canonical JSON 规则对每层对象 key 词典序排序。因此这些 bytes 会被规格自己的接纳边界拒绝，不能成为 T4 或 persisted golden，前轮 quota exact-vector P1 仍未关闭。

[`report.md`](../specs/report.md) 必须继续保持 `status: draft`，不得按现稿开始 Report 纵向实现。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮 P1 对账

| 前轮项 | 本轮判断 | 证据 |
|---|---|---|
| quota fallback/T4 exact vectors | **部分关闭，仍为 P1** | [`interrupt.md` §3.6](../specs/interrupt.md#36-t4-接纳与命令-golden-vectors) 已给完整对象及合法/拒绝 cases，但三份新增 JSON 都违反其宣称采用的 canonical bytes 规则。 |
| attempt recipe 与双 arm 交叉拒绝 | **关闭** | [`storage.md` §6.4](../specs/storage.md#64-command-targeteffect-与-outcome) 冻结 terminal pair 相等、同 Run failed FK，以及错 Run/generation/non-failed/pair 不等和 attempt/quota 字段、option 交叉污染拒绝；report §7.8 同步派生测试。 |

## 3. 剩余可执行 P1

### P1 — quota exact vectors 不是 canonical JSON

[`config.md` §4](../specs/config.md) 的全局规则要求对象 key 在**每一层**按 UTF-8 字节词典序排列；`interrupt.md` §3.6 也把新增 quota input/output 明确称为 canonical JSON bytes。实际 bytes 有三类直接反例：

1. fallback 顶层从 `reason,headline,brief,...` 开始，而 canonical 顺序应从 `brief,headline,links,min_modality,options,reason` 开始；每个 option 也应为 `effect,id,label,risk`，现稿却是 `id,label,effect,risk`。
2. input 顶层写成 `run_id,attempt_no,interrupt`，应为 `attempt_no,interrupt,run_id`；`interrupt` 和 `candidate_options[]` 内层同样未排序。
3. output 写成 `headline,conclusion,key_points,recommended_option_id,options`，应为 `conclusion,headline,key_points,options,recommended_option_id`。

这不是展示风格差异：接纳器要求 canonical bytes，persisted digest 也依赖唯一 bytes。一个实现若严格执行 config §4 必须拒绝现有 golden；若照现有顺序接受，则违反全局 canonical 单一来源。新增的重排/添加 retry/错误 recommended option cases因此没有合法 quota input 基线可派生。

**关闭条件：** 将 quota fallback、合法 T4 input/output 及 persisted `options_json` 的每层对象按 config §4 排序，给出逐字节 fallback 的 `headline/brief_markdown/min_modality/links_json/options_json`；再以修正后的合法 bytes 冻结重排、添加 retry、错误 recommended option 的 expected code 与完全相同 fallback persisted bytes。可用 canonical serializer 复算并断言原 bytes等于输出。

## 4. 已确认通过的改动

- Report quota v1 仍是独立 `report_quota_failure_review` arm，只允许 `reject|hold`，没有 retry。
- quota fallback 已补齐 headline、brief、controlled event link、voice modality 和四字段 options；T4 case 也覆盖合法两项集合、重排、添加 retry 与错误 recommended option。
- `new_attempt` 的 terminal pair 逐字段等于 binding pair，并命中同 Run `failed` attempt。
- attempt/quota arms 对彼此字段和 option 集的交叉污染已有明确拒绝 vectors；quota arm 继续 attempt-less。

## 5. 验收判断

- 获取并核对 Issue #369 全文、Agent 建议、范围、comment 与约束：**YES**
- 获取并核对 #365、PR #367、合入提交与完整相关 diff：**YES**
- 核销 report rereview-4 两项 P1：**YES**
- quota fallback/T4 exact-vector P1 完全关闭：**NO**
- attempt identity/双 arm 交叉拒绝 P1 完全关闭：**YES**
- 保留 Report quota v1 no-retry `reject|hold`：**YES**
- 只产出评审报告、不修改规格：**YES**
- `report.md` 转 `active`：**NO**
- 允许按现稿开始 Report 纵向实现：**NO**
