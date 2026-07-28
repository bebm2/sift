---
status: active
created: 2026-07-28
summary: Sift 合并必须由 forge 对预期 head SHA 做条件检查
---

# ADR-011 合并必须使用远端 expected-head CAS

本 ADR 补强 [ADR-003](003-transactional-outbox.md) 的合并投递语义。结构展开见 [DESIGN §6.4](../DESIGN.md)。

## 决策

Gate 对冻结输入中的 head SHA 作出放行结论后，merge outbox operation 必须携带该 SHA；Forge 适配器的合并请求必须让**远端**原子校验当前 head 仍等于 `expected_head_sha`。

执行前重读发现 head 已变化时，旧 operation 收敛为 `stale` / no-op，并为新 head 重新组装 Gate 输入。即使预读仍相等，实际 merge 请求也必须带条件字段，禁止用“先查再合并”替代远端 CAS。适配器无法提供该语义时，Sift 不得自动合并。

## 理由

operation key 中带 SHA 只能区分本地操作，约束不了 forge 最终合并的对象。Gate(A) 与 merge(A) 之间若 head 变为 B，不带条件的 merge API 会合并当前 B，使未过 Gate 的代码进入主干。远端 CAS 是关闭该 TOCTOU 窗口的唯一位置。

## 放弃的选项

| 选项 | 放弃理由 |
|------|----------|
| merge 前重读一次 head | 查询与合并之间仍有竞态 |
| 只把 SHA 放进 operation key | 只能本地判重，远端不知道预期对象 |
| head 变化后沿用旧 Gate 结论 | Gate 输入已变化，直接违反门禁语义 |

## 后果

- Forge 契约中的 merge 动词携带 `expectedHeadSha`。
- 双平台契约测试必须覆盖 stale head 拒绝。
- outbox 重试遇到 head 变化不算瞬时失败，不重放旧 merge，而是触发重新 Gate。
