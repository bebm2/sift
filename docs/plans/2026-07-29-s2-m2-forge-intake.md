---
status: active
created: 2026-07-29
summary: S2/M2 Forge 与 Intake 主链——先 forge.md，再适配器、Intake、预算与契约测试
---

# S2 / M2：Forge 与 Intake

## 入口

- WBS：[docs/WBS.md § M2](../WBS.md)
- M1 结案：[第二次定向复审 PASS WITH NOTES](../reviews/2026-07-29-s1-m1-rereview-2-pi-gpt-5.6-sol.md)
- 既有骨架：`internal/forge` fake 端口（不得冒充 M2 完成）

## 工作包顺序

1. **W-M2-forge-spec** — 起草并审定 `specs/forge.md`（M2 实现前置）
2. **W-M2-forge-adapters** — PRD §5.2 最小动词集 + gh/glab argv 适配 + 错误分类
3. **W-M2-change-merge** — Change marker 全状态查找、merge expected-head CAS、能力缺失禁用
4. **W-M2-intake** — 轮询/游标、T1 接线、`PersistIntakeDecision`、crash marker、generation 仲裁、反向同步
5. **W-M2-api-budget** — Forge API 预算唯一收费口与慢轮询
6. **W-M2-contract-tests** — 双平台 fixture、V3/V7、V11 首段、Intake crash/generation

## 退出

M2 门禁全部勾选且阶段审计 PASS / PASS WITH NOTES。
