# M5 three-budget coexistence rereview (#763)

- **Issue**: [#763](https://github.com/miaoxiaoyong/sift/issues/763)
- **PR**: [#762](https://github.com/miaoxiaoyong/sift/pull/762) (merge `9e26f6f`)
- **Agent**: pi-deepseek-v4-pro
- **Round**: 1（首次，无历史关闭包）
- **Verdict**: PASS

## Checklist（WBS §5.6 六项关键验收）

| # | 验收项 | 证据 | YES/NO |
|---|--------|------|--------|
| 1 | Token 预算沿用 Brain `RecordBrainAttempt` 收费口 | `three_budget_coexistence_test.go` L134–140；`brain.go:198` 唯一 post-charge 路径；brain.md §6 确认 key 格式 | YES |
| 2 | API 预算沿用 Forge `ChargeForgeAPICall` 收费口 | L143–147；`forgebudget.go:95` 唯一 charge 路径；forge.md §9 | YES |
| 3 | Attention 沿用 `EmitInterrupt`，无直接 counter 写入 | L150–153；interrupt.md §1 声明 `EmitInterrupt` 为唯一入口 | YES |
| 4 | 无重复收费实现 | PR #762 diff：仅新增测试文件，零生产代码改动 | YES |
| 5 | token/API 降级不突破注意力配额 | `TestTokenDegradeDoesNotBreakAttentionQuota` + `TestForgeAPIDegradeDoesNotBreakAttentionQuota` 各 3/3 PASS，含 `-race` | YES |
| 6 | 相关包全绿 | `./internal/storage/` + `./internal/brain/` PASS；`-race` 三轮 PASS | YES |

## Findings

无。

## Scope summary

| 级别 | 数量 | 本轮是否实施 |
|---|---|---|
| P0 | 0 | — |
| P1 | 0 | — |
| P2 | 0 | — |
| DEFER | 0 | — |

## 后续

WBS §5.6 三个 checkbox 可据此勾选。不读作 M5 已实现（§5.7 指标/CLI、critical 熔断、Channel `ops.ps`/`ops.doctor` 仍开）。
