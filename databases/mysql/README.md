# MySQL

## 已收录

- [**core-knowledge.md**](./core-knowledge.md) — 核心知识全景（知识纲 + 底层原理）
  - 架构分层、InnoDB 内存/磁盘、Page 与行格式、B+ 树物理组织
  - 事务/MVCC 深度、锁体系全景、Next-Key Lock 加锁规则详演
  - 日志与两阶段提交、崩溃恢复、优化器 Cost Model
  - EXPLAIN 全字段、调优参数速查、5.7 vs 8.0 差异
  - 面试记忆锚点

- [**interview-and-scenarios.md**](./interview-and-scenarios.md) — 60 道高频面试题 + 10 个生产场景
  - 存储引擎/索引/事务/锁/日志/主从/SQL 优化 六大类问答
  - 分库分表 / 深分页 / Online DDL / 死锁 / 慢查询 / 幂等 / 归档 / 主从延迟 / Snowflake / 乐观锁 vs 悲观锁

## 两份文档的定位

| | core-knowledge | interview-and-scenarios |
|---|---|---|
| 形式 | 知识纲要 + 结构图 + 参数表 | Q&A + 场景实战 |
| 用途 | **建立框架、背知识点** | **刷题、面前突击** |
| 覆盖 | 底层原理 + 系统全景 | 高频考点 + 生产案例 |
| 建议顺序 | ① 先看 core 建立框架 | ② 再刷题 & 看场景 |

## 计划收录

以下细分主题在 core-knowledge.md 里已有系统覆盖，未来若单独展开将放在这里：

- **MVCC 源码级剖析**：trx_sys、read_view 分配、purge 线程细节
- **死锁诊断实战手册**：从 `LATEST DETECTED DEADLOCK` 到根因定位
- **Online DDL 内核**：INSTANT / INPLACE / COPY 三种算法源码路径
- **分库分表专题**：路由、跨库事务、扩容、数据迁移
- **主从复制内核**：并行复制（LOGICAL_CLOCK / WRITESET）、组复制（MGR）
- **性能压测方法论**：sysbench、tpcc-mysql、真实业务回放

## 面试三大主题

绝大部分深度问题最后都会绕回这三个：

1. **索引怎么选**（B+ 树 + 覆盖 + 最左前缀 + 索引下推 + 索引失效）
2. **事务怎么保证**（ACID + MVCC + 锁 + 隔离级别）
3. **日志怎么工作**（redo/undo/binlog + 两阶段提交）

## 常问的一梯队

- B+ 树相比 B 树的优势 / 3 层撑多少数据
- 为什么 InnoDB 用 B+ 树而不是哈希
- MVCC 是怎么解决幻读的（快照读 vs 当前读）
- Next-Key Lock 加锁规则（4 种典型场景）
- redo log 和 binlog 的两阶段提交
- 慢 SQL 排查思路（EXPLAIN → 索引 → 回表 → 锁 → 长事务）
