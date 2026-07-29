---
status: active
created: 2026-07-30
summary: AdvanceInterrupt 重复摘要升级处置
---

# AdvanceInterrupt schema upgrade

The repository tool below is the only supported duplicate-digest recovery
path. Run it against a stopped service and a copy of the database first:

```sh
go run ./cmd/sift-advance-interrupt-repair --db /path/sift.db
# If the report contains only distinct immutable bindings:
go run ./cmd/sift-advance-interrupt-repair --db /path/sift.db \
  --repair --backup /path/sift.db.pre-0049
```

`--repair` canonicalizes each JSON binding and recomputes its SHA-256 digest
inside one transaction. It refuses byte-identical immutable rows (there is no
lossless digest-preserving repair for those); retain the backup and escalate
that case for a new Interrupt rather than editing history. The command verifies
that the backup opens and passes SQLite integrity_check before it changes the
source database. It prints each interrupt-to-digest mapping and is safe to
rerun after restoring the backup. To roll back a repair,
stop the service, replace the database with the backup, and rerun the audit
without `--repair` before restarting.

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

应分别返回无行和 `49`。如备份修复无法证明 binding provenance，保持服务
停止并重新生成受影响 Interrupt，而不是绕过 0038 的约束。
