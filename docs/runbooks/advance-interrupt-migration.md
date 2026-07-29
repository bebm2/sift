---
status: active
created: 2026-07-30
summary: AdvanceInterrupt 0036 重复摘要升级处置
---

# AdvanceInterrupt schema upgrade

版本 0038 依赖 0036 的 binding digest 唯一索引。升级前在数据库副本上执行：

```sql
SELECT binding_digest, COUNT(*) AS n
FROM interrupt_command_effect_bindings
GROUP BY binding_digest HAVING n > 1;
```

结果非空时不要删除 immutable binding。相同 digest 必须逐字节比较
`binding_json`、`binding_schema_version` 和 `reason`，并保留一条实际被
`interrupt_id` 引用的事实；对仍需保留的历史事实，先导出数据库备份并由
发布工具生成新的、重新计算的 digest，再按 binding 的原始审计顺序恢复。
不得手工改写 `binding_digest`，也不得把不相同的 binding 合并。恢复后重复
执行上面的查询，确认无重复，再重试服务启动。迁移每版本在单独事务中执行；
失败不会留下 migration 记录或部分 trigger/index。

升级后验证：

```sql
PRAGMA foreign_key_check;
SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;
```

应分别返回无行和 `38`。如备份修复无法证明 binding provenance，保持服务
停止并重新生成受影响 Interrupt，而不是绕过 0038 的约束。
