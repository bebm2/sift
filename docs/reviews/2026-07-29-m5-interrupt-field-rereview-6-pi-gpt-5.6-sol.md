PASS

# M5 interrupt.md 字段级第六次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/interrupt.md`](../specs/interrupt.md)
> 基线：[第五次定向复审](2026-07-29-m5-interrupt-field-rereview-5-pi-gpt-5.6-sol.md) 的 P1-R1c
> 声称关闭：#372 / PR #374（merge commit `184d42e`，实现 commit `31ee8d6`）
> 当前基线：`main` `082102d`
> 规则来源：[`config.md` §4](../specs/config.md)

## 1. 结论

**PASS（P1-R1c 已关闭）。** PR #374 只改写 `interrupt.md` §3.6 的 Report quota v1 vectors：fallback renderer、合法 T4 input 与合法 output 的每一层对象 key 均已按 UTF-8 字节词典序排列；`brief_fragments=["请人工处理","额度已耗尽"]` 保持正确升序，领域字段和值未变化。

以 UTF-8、无多余空白、递归对象 key 排序的 serializer 独立重算三份 JSON，原始 bytes 与重算结果逐字节相等。quota `options_json` 直接复用 fallback 对象中的 canonical options 数组，也不再留下另一套非 canonical bytes。因此第五次复审的唯一剩余 P1 可以核销。

本结论仅核销 interrupt 字段规格的 P1-R1c；`interrupt.md` 仍诚实保持 `status: draft`，不表示 M5 已实现或完整阶段门禁已通过。

## 2. P1-R1c 对账

| 关闭条件 | 结论 | 核验结果 |
|---|---|---|
| input 顶层 `attempt_no,interrupt,run_id` | **YES** | 顶层 key 顺序正确。 |
| `interrupt` 与 `candidate_options[]` 逐层 canonical | **YES** | `interrupt`、两项 option 及 link 对象均按 key 的 UTF-8 bytes 升序。 |
| output `conclusion,headline,key_points,options,recommended_option_id` | **YES** | 原始 output bytes 与独立 canonical serialization 完全相等。 |
| fallback 与 persisted `options_json` canonical | **YES** | fallback 顶层及嵌套对象均 canonical；persisted options 明确逐字节复用其中两项数组。 |
| fragments 与领域值不变 | **YES** | fragments 仍为正确 UTF-8 bytes 升序；对 #372 前后三个 JSON 作解析后深比较，领域对象完全相等。 |

## 3. 关闭检查清单

- [x] 在检测到的 GitHub forge 获取并阅读 #376 全文、Agent 建议、范围与约束
- [x] 获取并核对 #372、评论、PR #374 合入状态与实际 diff
- [x] 逐项复核第五次报告的 P1-R1c
- [x] 用 canonical serializer 重算三份 quota JSON 并断言原始 UTF-8 bytes 完全相等
- [x] 硬查每层对象 key 与 `brief_fragments` 的 UTF-8 bytes 顺序
- [x] 确认领域字段和值未变化
- [x] 只新增评审报告，未修改规格或实现
- [x] 报告首行为 `PASS`
- [x] P1-R1c 关闭（**YES**）
- [x] interrupt 第六次定向复审通过（**YES**）
- [x] `interrupt.md` 保持 `draft`
