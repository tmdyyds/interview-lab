# MySQL

## 计划收录

- **存储引擎**：InnoDB vs MyISAM
- **索引**：
  - B+ 树结构
  - 聚簇索引 vs 二级索引 vs 覆盖索引 vs 联合索引
  - 最左前缀原则
  - 索引失效场景
  - 索引下推（ICP）
  - 前缀索引、全文索引、哈希索引
- **事务**：
  - ACID
  - 隔离级别：RU / RC / RR / Serializable
  - MVCC 实现（版本链 + Read View + undo log）
  - 幻读与间隙锁
- **锁**：
  - 全局锁 / 表锁 / 行锁
  - 共享锁 / 排他锁
  - 记录锁 / 间隙锁 / Next-Key Lock
  - 意向锁
  - 死锁检测
- **日志**：redo log / undo log / binlog / relay log
  - 两阶段提交
- **主从复制**：异步、半同步、组复制
- **执行计划**：`EXPLAIN` 各字段含义
- **SQL 优化**：慢查询、索引优化、分区表
- **分库分表**：垂直/水平拆分、分布式 ID、跨库事务

## 面试常问

- B+ 树相比 B 树的优势
- 为什么 InnoDB 用 B+ 树而不是哈希？
- MVCC 是如何解决幻读的？
- Next-Key Lock 加锁规则
- redo log 和 binlog 的两阶段提交
