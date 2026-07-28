# WBS D0.3 定向复核

> 日期：2026-07-28  
> 复核人：pi × GPT-5.6 Sol  
> 复核对象：[`docs/WBS.md`](../WBS.md) D0.3  
> 前序评审：[D0.2 独立复评](2026-07-28-wbs-review-pi-gpt-5.6-sol-01.md)

## 1. 结论

**通过。D0.2 独立复评的 2 项 P1、4 项 P2、2 项 P3 全部关闭；未发现新阻断，可将 WBS 标为 `active` 并进入 M1 specs。**

本轮是定向复核，不重做已通过的 PRD/DESIGN 架构评审。复核范围为前序 F1–F6、N1–N2，以及修订是否引入新的里程碑后向依赖。

## 2. 逐项核销

| 前序项 | 结论 | D0.3 落点 |
|--------|------|-----------|
| F1：M4 Gate HITL 依赖 M5 全 reason Interrupt | 关闭 | M3 §3.6 改为支持全部 reason 的泛型确定性发射核心；M5 只接 T4/T6、Channel、调度与熔断；DESIGN §13 同步切片 |
| F2：V11 指标分母从 M5 后向依赖 M4 | 关闭 | M2 事实收敛 → M4 Gate/审计/Ledger 分类 → M5 指标分母；权威表最终闭合改 M5 |
| F3：CLI/doctor 不完整 | 关闭 | M1 §1.5 增全部运维命令壳、offline 只读规则与 doctor 基线；M8 按 DESIGN §8.10 全清单验收 |
| F4：通用 Command 效果缺失 | 关闭 | M5 §5.4 增 PRD §7.1 全指令集、reason-specific options、`ask` 当前 Run 澄清与 Ledger 同事务 |
| F5：软护栏一次性/记住遗漏 | 关闭 | M4 §4.3 增两类软豁免及硬护栏永不豁免测试 |
| F6：手工合并未接 Ledger 校准 | 关闭 | M4 §4.4 明确 `recordHumanDecision`、Gate 预判、`gate_bypassed` 与校准样本接线 |
| N1：active/draft 冲突 | 关闭 | 本轮通过后统一为 `active` |
| N2：指标数量陈旧 | 关闭 | WBS 与 DESIGN §9.3 统一为“全部指标（当前九项）” |

## 3. 依赖复核

- M3 已能在没有 T4/T6/Channel 时，为全部 reason 生成合法 fallback Interrupt；M4 Gate 不再依赖 M5。
- M4 门禁只验 V11 的状态、审计和 Ledger 分类；指标查询在 M5 闭合，不再构成 M4 → M5 → M4 环。
- M1–M8 前置链仍为单向顺序；V5、V9、V10a、V11 的跨片部分均在权威表标明首跑与最终闭合。
- DESIGN §13 已同步 Brain 分片与泛型发射核心前移，不存在 WBS/DESIGN 两套排期。

## 4. 机械检查

- `git diff --check`：通过。
- PRD 十个模块、DESIGN §15 派生 spec、V1–V15、A1–A10：机械追溯通过。
- D0.2 旧错误模式检索：无残留。
- WBS 内部链接：通过。

## 5. 结论动作

1. `docs/WBS.md` 改为 `status: active`。
2. 下一步进入 M1 specs，先写 `specs/config.md` 与 `specs/storage.md`，再写 control-plane/outbox/brain。
3. specs 完成前不提前编写实现代码。
