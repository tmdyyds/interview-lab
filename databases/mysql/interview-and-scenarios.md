# MySQL 面试题 + 场景实例合集

**标签**: #mysql #innodb #interview #scenarios #高频

MySQL 是后端面试的必考大项。本文覆盖两部分：

1. **60 道高频面试题**（按存储引擎/索引 / 事务 / 锁 / 日志 / 主从 / SQL 优化 6 大类分组）
2. **10 个生产场景的解决方案**（真实案例 + SQL / Go 实现）

> 💡 遇到概念模糊时，跳去 [**core-knowledge.md**](./core-knowledge.md) 查底层原理——那份文档系统梳理了 InnoDB 内存/磁盘结构、Page 与行格式、B+ 树物理组织、MVCC 深度、Next-Key Lock 加锁规则、两阶段提交、优化器 Cost Model、调优参数速查等。本文侧重**考点问答**，那边侧重**结构与原理**。

---

## Part 1：60 道高频面试题

### 一、存储引擎与索引（12 题）

#### Q1: InnoDB 和 MyISAM 有什么区别？

| | InnoDB | MyISAM |
|---|---|---|
| 事务 | 支持 | 不支持 |
| 外键 | 支持 | 不支持 |
| 锁粒度 | 行锁 | 表锁 |
| 崩溃恢复 | 有 redo log | 无（易损坏） |
| 索引结构 | 聚簇索引（数据+索引一起） | 非聚簇（数据独立） |
| 全文索引 | 5.6+ 支持 | 支持 |
| count(*) | 需扫描 | 有计数器，O(1) |

**MySQL 5.5+ 默认引擎是 InnoDB**。除了纯只读、极致读性能场景，一律用 InnoDB。

#### Q2: 为什么 InnoDB 用 B+ 树而不是 B 树？

- **叶子节点存所有数据**：非叶子节点只存索引，能容纳更多分支 → 树更矮 → IO 更少
- **叶子节点双向链表**：范围查询天然高效（`BETWEEN`、`ORDER BY`、分页）
- **只在叶子命中**：查询路径长度稳定，性能可预期
- B 树数据分散在所有节点，范围查询需要多次遍历

3 层 B+ 树可以存约 2000 万行（每页 16KB，非叶子约 1170 条索引项）。绝大部分表查询只需要 3-4 次磁盘 IO。

#### Q3: 为什么不用哈希索引？

- **不支持范围查询**（哈希打散了顺序）
- **不支持排序**
- **不支持部分匹配**（联合索引最左前缀无效）
- **哈希冲突**性能退化到链表

哈希索引在 InnoDB 内部有个特殊用法叫 **Adaptive Hash Index（自适应哈希索引，AHI）**：InnoDB 观察到某些页被频繁访问时，自动在内存里为它们建哈希索引加速。**用户不能显式创建**。

#### Q4: 聚簇索引和二级索引的区别？

- **聚簇索引（Clustered Index）**：**索引和数据存在一起**，叶子节点存整行数据
  - 一张表只能有一个聚簇索引
  - InnoDB 用主键作为聚簇索引；没主键则用第一个 NOT NULL 唯一索引；再没有则内部生成 6 字节 ROW_ID
- **二级索引（Secondary Index）**：**叶子节点存主键值**
  - 通过二级索引查非索引字段需要**回表**（拿到主键再去聚簇索引查一次）

**关键推论**：主键选择很重要。用递增 ID 而不是 UUID，因为随机主键会导致聚簇索引大量页分裂。

#### Q5: 什么是覆盖索引？

查询需要的字段**全部**在索引里，**不需要回表**。

```sql
-- 表：user(id PK, name, age, city)
-- 索引：idx_name_age (name, age)

SELECT name, age FROM user WHERE name = 'jake';
-- ✅ 覆盖索引，只查 idx_name_age，无需回表

SELECT name, age, city FROM user WHERE name = 'jake';
-- ❌ city 不在索引里，需要回表
```

EXPLAIN 里 `Extra` 会显示 `Using index` 表示走了覆盖索引。**优化深分页的核心手段**。

#### Q6: 联合索引的最左前缀原则？

联合索引 `(a, b, c)` 相当于建了三个索引：`(a)`、`(a,b)`、`(a,b,c)`。

**能走索引**：
- `WHERE a = ?`
- `WHERE a = ? AND b = ?`
- `WHERE a = ? AND b = ? AND c = ?`
- `WHERE a = ? AND c = ?`（a 走索引，c 不走，但索引下推可用）

**不能走索引**：
- `WHERE b = ?`（缺 a）
- `WHERE b = ? AND c = ?`（缺 a）

**"最左"指的是索引定义的顺序，不是 SQL 里 WHERE 的顺序**。优化器会自动调整 WHERE 条件顺序，只要包含最左字段就行。

#### Q7: 什么是索引下推（ICP）？

**Index Condition Pushdown**，MySQL 5.6+ 引入。

传统方式：二级索引先按最左前缀过滤 → 拿主键回表 → Server 层再过滤剩余条件。

ICP 优化：**把 WHERE 里能用索引字段的条件下推到存储引擎层**，回表前先过滤。

```sql
-- 索引 (name, age)
SELECT * FROM user WHERE name LIKE 'j%' AND age > 20;
```

无 ICP：
1. 引擎按 `name LIKE 'j%'` 找到所有匹配的主键
2. 逐条回表
3. Server 层过滤 `age > 20`

有 ICP：
1. 引擎按 `name LIKE 'j%'` 找到候选，**同时用索引里的 age 过滤**
2. 只对存活的记录回表

减少大量回表 IO。EXPLAIN 里 Extra 显示 `Using index condition`。

#### Q8: 哪些情况会导致索引失效？

1. **函数或表达式**：`WHERE UPPER(name) = 'JAKE'` → 索引失效
2. **隐式类型转换**：`WHERE phone = 13800138000`（phone 是 varchar）→ 索引失效
3. **前导模糊匹配**：`WHERE name LIKE '%jake'` → 索引失效
4. **不等于**：`WHERE status != 1`（大多数情况）
5. **OR 连接非索引列**：`WHERE a = 1 OR b = 2`（b 无索引）
6. **联合索引不满足最左前缀**
7. **数据分布不均导致优化器主动放弃索引**（比如查询结果占比 > 30%，直接全表扫更快）

排查方法：`EXPLAIN` 看 `key` 是否为 NULL、`type` 是否为 ALL。

#### Q9: 主键应该用自增 ID 还是 UUID？

**推荐自增 ID**（雪花、号段等递增 ID 也可以）。原因：

- **顺序写入**：新数据总是插在 B+ 树最右边，不会造成页分裂
- **索引更小**：BIGINT 8 字节 vs UUID 36 字符
- **二级索引更小**：二级索引叶子存主键值，主键越大二级索引越大

**UUID 的问题**：
- 无序 → 大量页分裂 → 写性能差
- 占空间 → 索引膨胀
- 可读性差

**特殊场景可以用 UUID**：
- 分布式环境不方便协调递增 ID
- 不想暴露业务量（自增 ID 会泄露"今天订单量"）
- 客户端预生成 ID

折中方案：**雪花算法**（Snowflake），既递增又分布式无冲突。

#### Q10: 前缀索引和它的问题？

给长字符串字段建索引时只取前 N 个字符：

```sql
ALTER TABLE user ADD INDEX idx_email (email(10));
```

**优点**：索引更小，B+ 树更浅
**问题**：
- **不能用于覆盖索引**（索引里只存了前缀，回表拿完整值）
- **不能用于 ORDER BY / GROUP BY**（前缀相同的顺序不确定）
- 长度选择需权衡：太短 → 区分度低；太长 → 索引大

选择长度看**区分度**：
```sql
SELECT COUNT(DISTINCT LEFT(email, 7)) / COUNT(*) FROM user;
-- 结果接近 1 说明区分度够
```

#### Q11: 什么时候不该建索引？

- **数据量小**（几百行），全表扫更快
- **频繁写入的字段**（每次写要更新索引，代价高）
- **区分度低**的字段（性别、状态，走索引不如全表扫）
- **很少作为查询条件**的字段
- **大字段**（TEXT、BLOB）—— 用前缀索引或全文索引

#### Q12: 一张表建多少索引合适？

**一般 5 个以内**。原因：
- 每个索引都占空间
- 每次写入都要维护所有索引（写放大）
- 优化器需要评估更多索引组合，可能变慢
- 冷索引占用 buffer pool 内存

**优化思路**：
- 优先建**联合索引**覆盖多个场景（一个联合索引顶多个单索引）
- 定期检查未使用的索引（`sys.schema_unused_indexes`）
- **删除冗余索引**：`(a)` 是 `(a, b)` 的前缀，`(a)` 可删

---

### 二、事务与 MVCC（10 题）

#### Q13: ACID 分别是什么？

- **A（Atomicity 原子性）**：事务内操作全成功或全失败。**靠 undo log 实现**（回滚）
- **C（Consistency 一致性）**：事务前后数据满足业务规则。**是最终目的**，靠其他三个特性保证
- **I（Isolation 隔离性）**：并发事务互不干扰。**靠锁 + MVCC 实现**
- **D（Durability 持久性）**：事务提交后数据不丢。**靠 redo log 实现**

#### Q14: MySQL 有哪四种隔离级别？

| 级别 | 脏读 | 不可重复读 | 幻读 |
|---|---|---|---|
| Read Uncommitted (RU) | ✅ | ✅ | ✅ |
| Read Committed (RC) | ❌ | ✅ | ✅ |
| **Repeatable Read (RR)** ← MySQL 默认 | ❌ | ❌ | ⚠️ |
| Serializable | ❌ | ❌ | ❌ |

**MySQL 默认是 RR**（Oracle、PostgreSQL 默认 RC），历史原因是早期主从复制用 statement binlog 需要 RR 才安全。

- **脏读**：读到别人未提交的数据
- **不可重复读**：同一事务两次读同一行结果不同（别人 update 了）
- **幻读**：同一事务两次范围查询结果集不同（别人 insert 了）

#### Q15: RR 和 RC 的核心区别？

**核心区别是 ReadView 的生成时机**：

- **RC**：**每次快照读都新建 ReadView** → 能看到已提交事务的最新变更
- **RR**：**事务开始时的第一次快照读生成 ReadView，后续复用** → 整个事务看到一致的快照

从锁的角度：
- **RC**：只加**记录锁**，无间隙锁 → 高并发下性能更好，但有幻读
- **RR**：加 **Next-Key Lock**（记录锁 + 间隙锁）→ 一定程度上避免幻读

**大厂选择**：京东、字节等很多公司实际用 RC，因为并发更好，用应用层保证幻读安全。

#### Q16: MVCC 是怎么实现的？

**Multi-Version Concurrency Control**（多版本并发控制），InnoDB 的关键设计。

三个核心组件：

**1. 隐藏字段**（每行数据）
- `DB_TRX_ID`：最近修改这行的事务 ID
- `DB_ROLL_PTR`：指向 undo log 中该行的上一个版本
- `DB_ROW_ID`：无主键时自动生成的行 ID

**2. undo log 版本链**
每次 UPDATE 或 DELETE 都会在 undo log 里保留旧版本，通过 `DB_ROLL_PTR` 串起链表：

```
最新版本 → v3 → v2 → v1 (最早版本)
```

**3. ReadView**（一致性视图）
包含四个关键字段：
- `m_ids`：生成 ReadView 时**未提交的事务 ID 列表**
- `min_trx_id`：m_ids 中最小值
- `max_trx_id`：预分配的下一个事务 ID
- `creator_trx_id`：创建该 ReadView 的事务 ID

**可见性判断规则**（对某行数据的 DB_TRX_ID）：
```
if trx_id == creator_trx_id → 可见（自己改的）
if trx_id < min_trx_id      → 可见（早已提交）
if trx_id >= max_trx_id     → 不可见（后开启的事务）
if trx_id in m_ids          → 不可见（未提交）
if trx_id not in m_ids      → 可见（已提交）
```

不可见时沿 `DB_ROLL_PTR` 找旧版本，直到找到可见的。

#### Q17: 快照读 vs 当前读？

- **快照读**（一致性非锁定读）：走 MVCC，读快照版本，**不加锁**
  - 普通 `SELECT`

- **当前读**（锁定读）：读最新已提交数据，**加锁**
  - `SELECT ... LOCK IN SHARE MODE`（S 锁）
  - `SELECT ... FOR UPDATE`（X 锁）
  - `INSERT` / `UPDATE` / `DELETE`（隐式加 X 锁）

**关键**：只有当前读才会看到别人的新插入（这也是幻读的来源）。

#### Q18: 幻读什么时候会出现？RR 能完全解决幻读吗？

**幻读定义**：同一事务内两次相同的查询，第二次看到了新插入的行。

**MySQL RR 部分解决幻读**：
- **快照读**：靠 MVCC 天然一致，看不到新插入的行 → 无幻读
- **当前读**：靠 **Next-Key Lock**（间隙锁 + 记录锁）锁住范围，阻止插入 → 无幻读

**RR 下仍会出现幻读的场景**：
```sql
-- 事务 A
BEGIN;
SELECT * FROM t WHERE id > 10;   -- 快照读，看到 3 条
-- 事务 B: INSERT id = 100; COMMIT;
UPDATE t SET name = 'x' WHERE id > 10;  -- 当前读，此时会锁到并更新 4 条
SELECT * FROM t WHERE id > 10;   -- 又变成快照读，但因为 A 自己 UPDATE 了那 4 条，能看到 4 条 → 幻读！
```

严格避免幻读需要 Serializable 或应用层加锁。

#### Q19: 长事务有什么危害？

- **锁不释放**：其他事务阻塞
- **undo log 膨胀**：MVCC 要保留旧版本给长事务用，无法 purge
- **主从延迟**：主库长事务 → binlog 迟迟不 flush → 从库延迟
- **占用连接**：连接池被占，其他请求拿不到连接
- **回滚代价大**：长事务失败回滚要重放很久

**监控**：
```sql
SELECT * FROM information_schema.INNODB_TRX 
WHERE TIME_TO_SEC(TIMEDIFF(NOW(), trx_started)) > 60;
```

**规范**：
- 事务内不做网络调用、不 sleep、不做耗时业务
- 事务时长控制在秒级
- 长事务用 kill 命令强制回滚

#### Q20: 事务嵌套和保存点？

MySQL 不支持真正的嵌套事务，但支持 **SAVEPOINT**：

```sql
BEGIN;
INSERT INTO t VALUES (1);
SAVEPOINT sp1;
INSERT INTO t VALUES (2);
ROLLBACK TO SAVEPOINT sp1;  -- 只回滚到 sp1，第一条还在
COMMIT;
```

Spring 的 `PROPAGATION_NESTED` 底层就是 SAVEPOINT。

#### Q21: autocommit 是什么？

MySQL 默认 `autocommit = 1`，每条 SQL 自动开一个事务并立即提交。

**手动事务**：
- 显式 `BEGIN` / `START TRANSACTION`
- 或 `SET autocommit = 0`（当前会话所有 SQL 都在事务里）

**注意**：`SET autocommit = 0` 会开启一个隐式事务，忘记 COMMIT 会造成长事务和锁不释放。

#### Q22: 分布式事务的方案有哪些？

- **XA（两阶段提交）**：MySQL 5.7.7+ 支持，性能差，强一致
- **TCC**（Try-Confirm-Cancel）：业务层两阶段
- **Saga**：正向流程 + 补偿
- **本地消息表**：业务和消息在同一事务，异步投递
- **可靠消息**（RocketMQ 事务消息）：半消息机制

详见 `system-design/distributed/transactions.md`。

---

### 三、锁（10 题）

#### Q23: MySQL 有哪些锁？

按粒度：
- **全局锁**：`FLUSH TABLES WITH READ LOCK`，用于全库备份
- **表锁**：`LOCK TABLES ... READ/WRITE`，MyISAM 才用，InnoDB 少用
- **行锁**：InnoDB 主流，粒度最细

按性质：
- **共享锁（S）**：读锁，多个事务可同时持有
- **排他锁（X）**：写锁，独占

按算法（InnoDB）：
- **记录锁（Record Lock）**：锁单条索引记录
- **间隙锁（Gap Lock）**：锁一个开区间
- **临键锁（Next-Key Lock）**：记录锁 + 前面的间隙锁
- **插入意向锁（Insert Intention Lock）**：INSERT 时的特殊间隙锁

#### Q24: 意向锁（Intention Lock）是什么？

**表级锁**，用于协调表锁和行锁的兼容性。

场景：事务 A 对某行加了 X 锁，事务 B 想加表 X 锁 → B 需要遍历所有行看有没有 X 锁 → 太慢。

**解决**：A 加行 X 锁时，先给表加**意向排他锁（IX）**，标记"这张表里有行被排他锁定"。B 想加表锁时只需检查是否有意向锁。

- **IS**（意向共享锁）：某行有 S 锁
- **IX**（意向排他锁）：某行有 X 锁

意向锁之间**互不冲突**（都是"标记"），只和表锁冲突。

#### Q25: 间隙锁和临键锁的区别？

假设索引 `age` 上有值 `10, 20, 30`，间隙就是：
```
(-∞, 10)  (10, 20)  (20, 30)  (30, +∞)
```

- **记录锁**：锁具体的记录，如 `age = 20`
- **间隙锁**：锁一个开区间，如 `(10, 20)`。不锁具体记录，但阻止 INSERT
- **Next-Key Lock**：`(10, 20]` 左开右闭，等于记录锁 + 前面的间隙锁

**为什么要有间隙锁**：为了防止**幻读**。如果只有记录锁，`SELECT ... FOR UPDATE WHERE age > 15` 只锁到 20 和 30，别人 INSERT 一条 age=25 依然成功 → 幻读。

**间隙锁只在 RR 隔离级别下生效**。RC 下没有间隙锁。

#### Q26: 一个 UPDATE 到底会加什么锁？

以 RR 为例，索引 `age` 上有 `10, 20, 30`：

**唯一索引精确查询**：
```sql
UPDATE t SET name = 'x' WHERE id = 20;   -- id 是主键
-- 只加记录锁 (id=20)
```

**唯一索引范围查询**：
```sql
UPDATE t SET name = 'x' WHERE id > 10 AND id < 30;
-- 加 Next-Key Lock: (10, 20], (20, 30)
```

**非唯一索引精确查询**：
```sql
UPDATE t SET name = 'x' WHERE age = 20;
-- 加 Next-Key Lock: (10, 20], 以及间隙锁 (20, 30)
-- 相当于锁住 age=20 及其前后间隙
```

**没有索引**：
```sql
UPDATE t SET name = 'x' WHERE name = 'jake';
-- 锁全表（每一行都加锁）
```

**规则不好记，实战永远用 `SHOW ENGINE INNODB STATUS` 看实际锁情况**。

#### Q27: 死锁是怎么发生的？

**四个必要条件**：互斥、请求与保持、不剥夺、循环等待。

MySQL 死锁经典场景：

```
事务 A: UPDATE t SET name = 'a' WHERE id = 1;   -- 锁 id=1
事务 B: UPDATE t SET name = 'b' WHERE id = 2;   -- 锁 id=2
事务 A: UPDATE t SET name = 'a' WHERE id = 2;   -- 等 B
事务 B: UPDATE t SET name = 'b' WHERE id = 1;   -- 等 A → 死锁
```

**InnoDB 有死锁检测**：发现循环等待时，选一个事务回滚（一般选影响行数少的）。

#### Q28: 如何避免死锁？

- **加锁顺序一致**：多条记录按固定顺序（如按 id 升序）加锁
- **缩短事务**：事务越短锁持有越短，冲突概率越低
- **降低隔离级别**：RC 无间隙锁，死锁概率低
- **索引优化**：没索引会锁全表 → 死锁高发
- **重试机制**：应用层捕获死锁异常（error 1213）并重试
- **拆分大事务**：一个大事务改多张表 → 拆成多个小事务

#### Q29: 死锁怎么排查？

```sql
-- 1. 查看最近的死锁信息
SHOW ENGINE INNODB STATUS\G
-- 找 LATEST DETECTED DEADLOCK 段落

-- 2. 开启死锁日志到错误日志
SET GLOBAL innodb_print_all_deadlocks = ON;

-- 3. 查看当前锁等待
SELECT * FROM sys.innodb_lock_waits;

-- 4. 查看事务列表
SELECT * FROM information_schema.INNODB_TRX;
```

**分析套路**：
1. 从死锁日志找到两个事务的 SQL
2. 画出锁获取顺序图
3. 找出循环点
4. 决策：改代码还是加索引

#### Q30: auto-increment 锁是什么？

`AUTO_INCREMENT` 自增字段的并发控制。

`innodb_autoinc_lock_mode` 三种模式：
- **0（传统）**：语句级表锁，所有 INSERT 串行
- **1（连续，MySQL 5.7 默认）**：普通 INSERT 用轻量互斥，批量 INSERT 加表锁
- **2（交错，MySQL 8.0 默认）**：全部用轻量互斥，性能最好但可能产生**不连续的自增值**

模式 2 下，`INSERT ... SELECT` 生成的 ID 可能不连续，但不影响正确性。**主从复制用 row binlog 时才安全用模式 2**。

#### Q31: FOR UPDATE 和 LOCK IN SHARE MODE 区别？

- **`SELECT ... FOR UPDATE`**：加 **X 锁**，用于"读了之后要写"的场景
- **`SELECT ... LOCK IN SHARE MODE`**：加 **S 锁**，用于"确认存在但不改"的场景

```sql
-- 转账：先读余额确保足够，再扣款
BEGIN;
SELECT balance FROM account WHERE id = 1 FOR UPDATE;  -- X 锁
UPDATE account SET balance = balance - 100 WHERE id = 1;
COMMIT;
```

#### Q32: 快照读和当前读的场景对比？

```sql
-- 事务 A（RR）
BEGIN;
SELECT * FROM t WHERE id = 1;             -- 快照读，MVCC，看到旧值
UPDATE t SET name = 'x' WHERE id = 1;     -- 当前读，锁定 + 更新最新值
SELECT * FROM t WHERE id = 1;             -- 仍是快照读，但看到自己修改后的
SELECT * FROM t WHERE id = 1 FOR UPDATE;  -- 当前读，看到最新值
```

**关键**：UPDATE/DELETE 都是"当前读"，即使在 RR 下也读最新数据。这是 RR 为什么无法完全避免幻读的根本原因。

---

### 四、日志与恢复（8 题）

#### Q33: redo log / undo log / binlog 的区别？

| | redo log | undo log | binlog |
|---|---|---|---|
| 作用 | 崩溃恢复（前滚） | 回滚 + MVCC | 主从复制 + 数据恢复 |
| 层级 | InnoDB 引擎层 | InnoDB 引擎层 | Server 层 |
| 格式 | 物理日志（记录页的物理修改） | 逻辑日志（记录反向操作） | 逻辑日志（Statement/Row/Mixed） |
| 写入 | 循环写（固定大小） | 循环写 | 追加写 |
| 场景 | 保证事务 D | 保证事务 A + MVCC | 复制、审计、备份 |

#### Q34: 两阶段提交（2PC）是什么？

**问题**：redo log 和 binlog 是两个独立日志，如果两者不一致，主从会不同步。

**解决**：MySQL 的 2PC 保证两个日志一致：

```
1. Prepare 阶段：写入 redo log（状态 prepare）
2. Commit 阶段：写入 binlog
3. Commit 阶段：redo log 状态改为 commit
```

**崩溃恢复规则**：
- 如果 redo log 是 commit 状态 → 提交
- 如果 redo log 是 prepare 状态：
  - 检查 binlog 是否完整
  - binlog 完整 → 提交
  - binlog 不完整 → 回滚

这样保证 redo log 和 binlog 语义一致。

#### Q35: binlog 有几种格式？

- **Statement**：记录 SQL 语句
  - 优点：日志小
  - 缺点：某些函数（`NOW()`、`UUID()`）主从执行结果不同 → 数据不一致
- **Row**（推荐）：记录每行的变更（before + after）
  - 优点：绝对安全，任何数据变化都能精确复现
  - 缺点：日志大（一条 UPDATE 影响 100 万行 → 100 万条 row event）
- **Mixed**：普通 SQL 用 Statement，不安全的用 Row

**生产强烈推荐 Row**。搭配 `binlog_row_image = MINIMAL` 节省空间。

#### Q36: redo log 什么时候写入？

InnoDB 采用 **WAL（Write-Ahead Log）**：先写日志，再写数据页。

`innodb_flush_log_at_trx_commit`：
- **0**：每秒 fsync 一次。**性能最好，最不安全**（宕机丢 1 秒数据）
- **1**（默认）：每次事务提交都 fsync。**最安全但最慢**
- **2**：写入 OS 缓存但不 fsync，每秒 fsync。折中

配合 `sync_binlog`：
- **0**：由 OS 决定 fsync 时机
- **1**（默认）：每次 commit fsync
- **N**：每 N 次事务 fsync

**双 1 配置**（`innodb_flush_log_at_trx_commit = 1` + `sync_binlog = 1`）：金融级安全。

#### Q37: undo log 具体存什么？

**逻辑日志**，记录反向操作：
- INSERT → 记录一个 DELETE
- UPDATE → 记录旧值
- DELETE → 记录整行数据

**两个用途**：
1. **事务回滚**：`ROLLBACK` 时依次执行反向操作
2. **MVCC**：为其他事务提供旧版本

**清理时机**：由 purge 线程异步清理"没有任何活跃事务需要的"旧版本。**长事务会阻塞 undo purge**，导致 undo 表空间膨胀。

#### Q38: MySQL 崩溃后如何恢复？

```
1. 重启 MySQL
2. InnoDB 扫描 redo log
3. 找出未提交完成的事务（prepare 状态）
4. 检查 binlog 是否有对应事务
   - 有 → 通过 redo log 前滚（重做）
   - 无 → 通过 undo log 回滚
5. 应用所有已提交事务的 redo → 数据文件与日志一致
```

关键：**redo log 只需存储"未刷盘的脏页对应的日志"**，已刷盘的部分可以被覆盖（循环写）。

#### Q39: change buffer 是什么？

**优化二级索引的写入性能**。

正常流程：更新二级索引需要读取索引页到 buffer pool → 修改 → 刷盘。如果索引页不在 buffer pool，还需要一次磁盘 IO。

change buffer：**如果索引页不在 buffer pool，先把修改记录到 change buffer**，等下次读到这个页时再合并（merge）。

**限制**：
- 只对**非唯一索引**有效（唯一索引必须读页判断是否冲突）
- 写多读少的场景收益大（写入频繁，合并延迟长）
- 读多的场景反而是负担

配置：
```
innodb_change_buffering = all   -- 所有操作都用（默认）
innodb_change_buffer_max_size = 25  -- 占 buffer pool 25%
```

#### Q40: binlog 和主从复制的关系？

主从复制的核心机制：

```
1. 主库执行 SQL 并写 binlog
2. 从库 IO Thread 连接主库，请求 binlog
3. 从库把接收的 binlog 写入本地 relay log
4. 从库 SQL Thread 读 relay log 并重放
```

**从库两个关键线程**：
- IO Thread：拉取 binlog
- SQL Thread：应用 binlog

复制模式：
- **异步复制**（默认）：主库不等从库
- **半同步**：主库等至少一个从库确认收到 binlog
- **组复制**（MGR）：多主，Paxos-like 协议

---

### 五、主从复制与高可用（8 题）

#### Q41: 主从复制的完整流程？

```
主库:                                  从库:
1. 事务提交，写 binlog                 
                                       2. IO Thread 请求 binlog
3. dump 线程发送 binlog                
                                       4. IO Thread 收到，写 relay log
                                       5. SQL Thread 读 relay log
                                       6. SQL Thread 重放 SQL
```

关键点：
- 主库 dump 线程是**推**给从库的（长连接）
- 从库有两个位点：`Master_Log_File`（IO 线程读到哪）、`Relay_Log_File`（SQL 线程执行到哪）

#### Q42: 主从延迟的常见原因？

- **大事务**：一条 UPDATE 影响百万行 → binlog 巨大 → 从库慢
- **主库 QPS 太高**：从库单线程重放跟不上
- **从库配置低**：CPU / IO 差
- **从库锁等待**：从库有查询业务时 DDL / 大事务被阻塞
- **网络延迟**：跨机房复制
- **大表 DDL**：主库 DDL 完成后，从库还在执行

**排查**：
```sql
SHOW SLAVE STATUS\G
-- Seconds_Behind_Master 是延迟秒数（不准，只是参考）
-- 真正准的看 GTID: Retrieved_Gtid_Set vs Executed_Gtid_Set
```

#### Q43: 异步、半同步、组复制的区别？

**异步复制**：
- 主库 commit 后立刻返回
- 主库挂了从库可能没同步 → 丢数据
- 性能好

**半同步复制**：
- 主库等至少一个从库 ACK 后才返回
- 减少数据丢失
- 有超时降级为异步

**组复制（MGR）**：
- 多主，Paxos-like 协议
- 强一致
- 复杂度高、性能一般

金融场景：至少半同步。互联网普通业务：异步够用。

#### Q44: 读写分离如何做？

- **应用层**：ORM 或中间件层根据 SQL 类型路由
- **代理层**：ProxySQL、MyCat、ShardingSphere-Proxy
- **JDBC 层**：ShardingSphere-JDBC

**关键问题**：主从延迟导致"写完立刻读读不到"

**解决方案**：
- **强制走主库**：关键读操作打标记
- **等主库**：写完后 GTID 等待从库同步（`WAIT_FOR_EXECUTED_GTID_SET`）
- **缓存兜底**：写完的数据同时写缓存，读缓存
- **业务容忍**：非关键场景接受秒级延迟

#### Q45: 主从数据不一致怎么办？

发现工具：`pt-table-checksum`（Percona Toolkit）

修复方式：
- **少量差异**：`pt-table-sync` 生成修复 SQL
- **大量差异**：重做从库（备份主库 → 恢复到从库 → 重建复制）

**预防**：
- 从库设 `read_only = ON` + `super_read_only = ON`（防止误写）
- 使用 Row 格式 binlog
- 主键必须有

#### Q46: GTID 是什么？

**Global Transaction ID**：每个事务的全局唯一 ID。

格式：`server_uuid:transaction_id`
```
3E11FA47-71CA-11E1-9E33-C80AA9429562:23
```

**优势**：
- 主从切换更方便（不用记 binlog 文件名 + position）
- 数据一致性更好保证
- 复制拓扑变更简单

启用：`gtid_mode = ON` + `enforce_gtid_consistency = ON`。

**MySQL 5.7+ 推荐启用 GTID**。

#### Q47: 高可用方案有哪些？

- **MHA**（Master High Availability）：Perl 写的经典方案，故障自动切换
- **Orchestrator**：GitHub 出品的现代方案
- **MGR（组复制）+ MySQL Router**：官方方案
- **MySQL InnoDB Cluster**：MGR + Router + Shell 的组合
- **PXC（Percona XtraDB Cluster）**：多主同步复制
- **云托管**：RDS 自动 failover

生产选择：
- 中小公司：主从 + Orchestrator
- 大公司：内部改造的 MHA / MGR

#### Q48: 主库切换（failover）的核心步骤？

1. **确认主库不可达**（不是网络抖动）
2. **选出新主**：从库中数据最新的（GTID 最大）
3. **等待其他从库追平新主的位点**
4. **切换新主为可写**：`SET GLOBAL read_only = OFF`
5. **其他从库指向新主**：`CHANGE MASTER TO`
6. **业务流量切换**：VIP 漂移 / DNS 切换 / 中间件配置更新
7. **老主库恢复后作为新从库**加入

**关键风险**：
- **脑裂**：新老主同时可写 → 需要 fencing
- **数据丢失**：异步复制时新主可能没同步全
- **业务感知**：切换期间部分请求失败

---

### 六、SQL 优化与执行计划（12 题）

#### Q49: EXPLAIN 的关键字段？

```sql
EXPLAIN SELECT * FROM user WHERE name = 'jake';
```

| 字段 | 含义 |
|---|---|
| id | 查询序号，越大越先执行 |
| select_type | SIMPLE / PRIMARY / SUBQUERY 等 |
| table | 表名 |
| **type** ★ | 访问类型：从好到差 `system > const > eq_ref > ref > range > index > ALL` |
| possible_keys | 可能用到的索引 |
| **key** ★ | 实际用的索引 |
| key_len | 用到的索引长度（字节） |
| ref | 索引比较的对象（常量或另一列） |
| **rows** ★ | 估算扫描行数 |
| filtered | 过滤后剩余百分比 |
| **Extra** ★ | 额外信息，Using index/filesort/temporary 都在这 |

**重点看**：type、key、rows、Extra。

#### Q50: type 从好到差的顺序？

```
system > const > eq_ref > ref > range > index > ALL
        (完美) ←──────────────────→ (灾难)
```

- **system**：表只有一行
- **const**：唯一索引/主键等值查询
- **eq_ref**：JOIN 时用唯一索引
- **ref**：非唯一索引等值查询
- **range**：索引范围扫描（BETWEEN、`>`、`<`、IN）
- **index**：全索引扫描（比全表好一点，因为索引小）
- **ALL**：全表扫描 → **需要优化**

生产要求：至少到 `range`，理想 `ref`+。

#### Q51: Using filesort 和 Using temporary 是什么？

- **Using filesort**：无法用索引完成排序，需要在内存或磁盘做额外排序
  - 原因：ORDER BY 字段没有索引 / 排序方向不一致 / 用了 JOIN
  - 解决：给 ORDER BY 字段加索引，让排序走索引顺序

- **Using temporary**：用临时表存储中间结果
  - 原因：GROUP BY 字段没索引 / DISTINCT / UNION（非 UNION ALL）
  - 解决：给 GROUP BY 字段加索引，尽量用 UNION ALL

两者都是**性能杀手**，看到必须优化。

#### Q52: 深分页 LIMIT 100000, 20 为什么慢？

```sql
SELECT * FROM orders ORDER BY id LIMIT 100000, 20;
```

MySQL 会**扫描 100020 行、丢弃前 100000 行**，回表 100020 次。越到后面越慢。

**优化方案**：

**方案 1：延迟关联（覆盖索引 + JOIN）**
```sql
SELECT o.* FROM orders o
JOIN (
    SELECT id FROM orders ORDER BY id LIMIT 100000, 20
) AS t ON o.id = t.id;
```
先只查主键（走覆盖索引，不回表），再回表 20 次。

**方案 2：游标分页（记录上次最后一条 ID）**
```sql
SELECT * FROM orders WHERE id > {last_id} ORDER BY id LIMIT 20;
```
每次利用主键定位，完全避免深分页。**推荐**。

**方案 3：业务限制**
限制用户最多能翻到 50 页。绝大多数用户不会翻超过 10 页。

#### Q53: count(*) 和 count(1) 和 count(字段) 的区别？

- **count(*)**：统计所有行（包括 NULL）。MySQL 优化器特别优化，最快
- **count(1)**：等价于 count(*)，性能一样
- **count(字段)**：统计字段非 NULL 的行数

**性能**：`count(*) ≈ count(1) > count(字段)`

**InnoDB 的 count(*) 优化**：会选择**最小的二级索引**去扫描（不用扫聚簇索引）。

**MyISAM 的 count(*)**：维护一个计数器，O(1) 返回。但 InnoDB 因为 MVCC 每个事务看到的行数不同，无法维护全局计数器。

**大表统计推荐**：
- 精确值：定期任务写入统计表
- 估算值：`SHOW TABLE STATUS`

#### Q54: LIMIT 1 有什么用？

如果确定只需要一条：
```sql
SELECT * FROM t WHERE name = 'jake' LIMIT 1;
```

优化器找到第一条后**立即停止扫描**，无需继续遍历。

对于 UNIQUE 或 PK 查询没影响（本来就只有一条），但对**非唯一字段**查询提升明显。

#### Q55: 慢 SQL 怎么定位？

**开启慢查询日志**：
```sql
SET GLOBAL slow_query_log = ON;
SET GLOBAL long_query_time = 1;  -- 1 秒
```

**日志分析工具**：
- `mysqldumpslow`：MySQL 自带
- `pt-query-digest`：Percona Toolkit，最强
- `Anemometer`：Web UI 可视化

**实时定位**：
```sql
-- 当前正在执行的慢 SQL
SELECT * FROM information_schema.PROCESSLIST 
WHERE COMMAND != 'Sleep' AND TIME > 10;
```

**Performance Schema**：
```sql
SELECT * FROM performance_schema.events_statements_summary_by_digest 
ORDER BY sum_timer_wait DESC LIMIT 10;
```

#### Q56: 什么是索引选择性？

**Selectivity = COUNT(DISTINCT col) / COUNT(*)**

- 接近 1：区分度高，适合建索引
- 接近 0：区分度低，索引价值小（性别 0.5）

**联合索引的字段顺序**：**区分度高的放前面**。

```sql
SELECT COUNT(DISTINCT city) / COUNT(*) FROM user;  -- 0.01
SELECT COUNT(DISTINCT phone) / COUNT(*) FROM user;  -- 1.00

-- 索引应该建 (phone, city) 而不是 (city, phone)
```

#### Q57: IN 和 EXISTS 的选择？

- **`IN`**：适合**子查询结果集小、主查询表大**
  ```sql
  SELECT * FROM order WHERE user_id IN (SELECT id FROM vip_user);
  ```
- **`EXISTS`**：适合**主查询结果集小、子查询表大**
  ```sql
  SELECT * FROM order WHERE EXISTS (
      SELECT 1 FROM order_log WHERE order_log.order_id = order.id
  );
  ```

MySQL 5.6+ 优化器会智能选择，两者性能差异不大。**但 NOT IN 遇到 NULL 会返回空**，用 NOT EXISTS 更安全。

#### Q58: JOIN 的实现算法？

- **Simple Nested Loop Join（SNLJ）**：嵌套循环，O(n×m)。性能差
- **Index Nested Loop Join（INLJ）**：内表用索引，O(n×log m)。**推荐**
- **Block Nested Loop Join（BNLJ）**：一批批加载外表到 join buffer 再匹配，减少内表扫描次数
- **Hash Join**（MySQL 8.0.18+）：无索引情况下的 JOIN 加速，大表 JOIN 神器

**优化 JOIN**：
- 小表驱动大表
- JOIN 字段必须有索引
- 避免多表 JOIN（超过 3 表就要谨慎）
- WHERE 优先于 JOIN 过滤

#### Q59: 大 SQL 拆分策略？

- **UNION ALL** 拆多次查询
- **分批处理**：一次处理 1000 条，减少锁范围
- **JOIN 拆成多个查询**：应用层做关联，减少数据库压力
- **异步化**：非实时结果放 MQ 慢慢跑

**反面教材**：一条 SQL JOIN 8 张表 + 5 层子查询 + ORDER BY GROUP BY，直接死锁。

#### Q60: 优化器为什么会选错索引？

**原因**：
- 统计信息过时：`ANALYZE TABLE` 更新统计信息
- 数据分布不均：某些值特别多，优化器判断失误
- 索引成本估算错误
- SQL 写法误导优化器

**强制使用索引**：
```sql
SELECT * FROM t FORCE INDEX (idx_name) WHERE ...;
SELECT * FROM t USE INDEX (idx_name) WHERE ...;   -- 建议
SELECT * FROM t IGNORE INDEX (idx_name) WHERE ...;
```

**慎用 FORCE INDEX**：数据分布变化后可能反而变慢。生产更推荐通过重写 SQL 引导优化器。

---

## Part 2：10 个生产场景实战

以下场景都是真实生产遇到过的问题，附带诊断思路和落地方案。

---

### 场景 1：订单表分库分表设计

**问题**：单表订单已经 3 亿行，查询变慢，DDL 几乎无法执行。

**思路**：

**第一步：确认瓶颈**
- 磁盘 IO？内存 buffer pool 命中率？CPU？连接数？
- 单表 500 万以下通常不需要分表；超过 5000 万查询开始明显变慢

**第二步：分表策略**

按 `user_id` 哈希分（推荐）：
```
db_order_00.orders_0000
db_order_00.orders_0001
...
db_order_15.orders_0063
```

- **分片键选择**：`user_id` 覆盖 90% 的查询场景（我的订单列表）
- **分片数**：一般 1024（4 库 × 256 表 或 16 库 × 64 表）
- **路由算法**：`user_id % 1024 → 表索引`；`表索引 / 64 → 库索引`

**第三步：跨分片查询的处理**

问题：按订单号查、按商家查怎么办？

方案：
- **订单号里带分片信息**：订单号 = 时间 + user_id 后 4 位 + 序列号，能反解出分片
- **异构索引表**：为其他查询维度建冗余表（`商家维度订单表`），通过 binlog 同步
- **搜索引擎兜底**：把订单同步到 ES，复杂查询走 ES

**第四步：数据迁移**

**双写方案**：
```
1. 上线双写代码（新表 + 老表都写）
2. 全量迁移历史数据（chunked，避免影响线上）
3. 数据校验（对比新老表）
4. 灰度切读到新表
5. 停止老表写入，下线老表
```

工具：Alibaba Canal / DTS / 自研迁移工具。

**关键陷阱**：
- **分片键不能改**：分片后 user_id 不能修改，否则要跨分片迁移数据
- **跨分片事务**：分布式事务解决（Seata / 消息表）
- **分页问题**：跨分片 ORDER BY LIMIT 需要读所有分片再合并，慎用

---

### 场景 2：深分页优化实战

**问题**：`ORDER BY id DESC LIMIT 500000, 20` 慢得像老年机（3s+）。

**诊断**：
```sql
EXPLAIN SELECT * FROM orders ORDER BY id DESC LIMIT 500000, 20;
-- rows: 500020, type: index, Extra: Backward index scan
```

优化器逼不得已扫 50 万行 + 回表 50 万次。

**优化方案演进**：

**版本 1：延迟关联（覆盖索引）**
```sql
SELECT o.* FROM orders o
INNER JOIN (
    SELECT id FROM orders ORDER BY id DESC LIMIT 500000, 20
) t ON o.id = t.id;
```
子查询走覆盖索引不回表 → 50 倍提升。**但 500000 offset 依然扫描 500020 个索引项**。

**版本 2：游标分页（推荐）**
```sql
-- 第一页
SELECT * FROM orders ORDER BY id DESC LIMIT 20;

-- 下一页（前端把上页最后一条的 id 传回来）
SELECT * FROM orders WHERE id < {last_id} ORDER BY id DESC LIMIT 20;
```

每次都是精准的主键定位，性能恒定 O(log n)。

**代价**：
- 不能跳页（无法直接跳到第 500 页）
- 需要前后端约定游标格式
- 排序字段必须有索引且组合唯一

**版本 3：业务折中**

看主流产品——淘宝、拼多多都不让翻超过 100 页。**深分页本身就是伪需求**，用户不会一页页翻到 5000 页。

**Go 代码示例**：

```go
type Cursor struct {
    LastID    int64 `json:"last_id"`
    CreatedAt int64 `json:"created_at"`
}

func ListOrders(ctx context.Context, userID int64, cursor *Cursor, limit int) ([]Order, *Cursor, error) {
    var orders []Order
    query := db.WithContext(ctx).
        Where("user_id = ?", userID).
        Order("id DESC").
        Limit(limit + 1)  // 多查 1 条判断是否还有下一页

    if cursor != nil {
        query = query.Where("id < ?", cursor.LastID)
    }

    if err := query.Find(&orders).Error; err != nil {
        return nil, nil, err
    }

    var nextCursor *Cursor
    if len(orders) > limit {
        orders = orders[:limit]
        nextCursor = &Cursor{LastID: orders[len(orders)-1].ID}
    }
    return orders, nextCursor, nil
}
```

---

### 场景 3：大表加字段（Online DDL）

**问题**：给 5 亿行的 `orders` 表加一个 `remark VARCHAR(200)` 字段。

**MySQL 5.6+ 的 Online DDL**：
```sql
ALTER TABLE orders ADD COLUMN remark VARCHAR(200), ALGORITHM=INPLACE, LOCK=NONE;
```

**但仍然有风险**：
- 表可能被 MDL（元数据锁）阻塞
- 中间需要复制整张表（Copy 算法）时磁盘暴涨
- 主库执行 30 分钟，从库串行执行也要 30 分钟 → 主从延迟严重

**生产推荐工具**：

**方案 1：gh-ost（GitHub 出品，推荐）**
```bash
gh-ost \
  --host=master.db \
  --database=order \
  --table=orders \
  --alter="ADD COLUMN remark VARCHAR(200)" \
  --chunk-size=1000 \
  --execute
```

**原理**：
1. 创建影子表 `_orders_gho`
2. 从从库读 binlog，异步应用变更到影子表
3. 用 chunk 的方式把原表数据复制到影子表
4. 复制完成后，rename 原表和影子表（原子操作）
5. 老表变成 `_orders_del`，稍后删除

**优点**：
- **不用触发器**（避免主库压力）
- 可暂停、可限流
- 主从延迟可控

**方案 2：pt-online-schema-change**
```bash
pt-online-schema-change \
  --alter="ADD COLUMN remark VARCHAR(200)" \
  D=order,t=orders \
  --execute
```

**原理**：用触发器同步增量数据。工作稳定但对源库压力大。

**关键 checklist**：
- 磁盘空间足够（需要 2 倍表大小）
- 避开业务高峰
- 有主从延迟监控
- 先在测试库演练
- 准备回滚方案（gh-ost 有 postpone-cut-over-flag-file 可暂停 cut-over）

---

### 场景 4：死锁排查实战

**问题**：线上 error log 频繁出现 `Deadlock found when trying to get lock`。

**诊断步骤**：

**Step 1：看死锁日志**
```sql
SHOW ENGINE INNODB STATUS\G
```
找 `LATEST DETECTED DEADLOCK` 段落。

**示例日志片段**：
```
*** (1) TRANSACTION:
TRANSACTION 12345, ACTIVE 5 sec
mysql tables in use 1, locked 1
LOCK WAIT 5 lock struct(s), heap size 1136, 3 row lock(s)
MySQL thread id 100, OS thread handle ...
INSERT INTO stock (sku_id, count) VALUES (100, 1) ON DUPLICATE KEY UPDATE count = count + 1;

*** (1) WAITING FOR THIS LOCK TO BE GRANTED:
RECORD LOCKS space id 20 page no 3 n bits 72 index PRIMARY of table `stock`

*** (2) TRANSACTION:
...同上另一个事务
```

**Step 2：还原案发现场**

两个事务并发执行 `INSERT ... ON DUPLICATE KEY UPDATE`，且 SKU 相同或相邻。

死锁流程：
```
A: INSERT sku=100  → 拿到主键的插入意向锁 + gap lock
B: INSERT sku=101  → 拿到 sku=101 的锁
A: 检测到冲突，转 UPDATE → 等 B 释放锁
B: 也需要检测冲突 → 等 A 释放锁
→ 死锁
```

**Step 3：修复**

方案 1：**统一加锁顺序**
```go
// 多个 SKU 时先按 ID 排序
sort.Slice(skus, func(i, j int) bool { return skus[i] < skus[j] })
for _, sku := range skus {
    db.Exec("INSERT INTO stock ... ON DUPLICATE KEY UPDATE ...", sku)
}
```

方案 2：**减少事务范围**
```go
// 不好：一个事务里改 10 个 SKU
db.Transaction(func(tx *gorm.DB) error {
    for _, sku := range 10个sku {
        tx.Exec(...)
    }
})

// 好：每个 SKU 一个短事务
for _, sku := range 10个sku {
    db.Exec(...)
}
```

方案 3：**改成先 SELECT 再 UPDATE/INSERT**
```go
row := db.Where("sku_id = ?", sku).Take(&stock)
if row.Error == gorm.ErrRecordNotFound {
    db.Create(&Stock{SkuID: sku, Count: 1})
} else {
    db.Model(&stock).Update("count", gorm.Expr("count + 1"))
}
```

方案 4：**应用层重试**
```go
for i := 0; i < 3; i++ {
    err := doTransaction()
    if err == nil { return nil }
    if !isDeadlockError(err) { return err }
    time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
}
```

**长期治理**：
- 开启 `innodb_print_all_deadlocks = ON`（记录所有死锁）
- 监控死锁频率，异常告警
- Code Review 关注新增 SQL 的锁行为

---

### 场景 5：慢 SQL 排查实战

**问题**：接口 P99 时不时飙到 5s，观察到某条 SQL 慢查询频繁。

**Step 1：定位慢 SQL**
```sql
SET GLOBAL slow_query_log = ON;
SET GLOBAL long_query_time = 0.5;
SET GLOBAL slow_query_log_file = '/data/mysql/slow.log';
```

一段时间后：
```bash
pt-query-digest /data/mysql/slow.log | less
```

排名第一：
```sql
SELECT * FROM orders 
WHERE status = 1 AND created_at > '2026-01-01' 
ORDER BY updated_at DESC LIMIT 20;
-- 平均 3.2s, 每天执行 5000 次
```

**Step 2：EXPLAIN 分析**
```
type: ALL
key: NULL
rows: 8000000
Extra: Using where; Using filesort
```

全表扫 + 文件排序，灾难现场。

**Step 3：加索引**
```sql
ALTER TABLE orders ADD INDEX idx_status_updated (status, updated_at DESC);
```

再看 EXPLAIN：
```
type: ref
key: idx_status_updated
rows: 200000
Extra: Using where
```

改进：走了索引，但 rows 还是 20 万（status=1 数据量大）+ 需要回表。

**Step 4：进一步优化**

如果 `status=1` 占比很低（比如 5%），加索引已经够用。如果 `status=1` 占大部分：

**方案 A：改造查询**
```sql
-- 加 created_at 过滤后区分度就够了
SELECT * FROM orders 
WHERE created_at > '2026-01-01' AND status = 1
ORDER BY updated_at DESC LIMIT 20;
```
索引改为 `(created_at, status, updated_at)`。

**方案 B：覆盖索引 + 延迟关联**
```sql
SELECT o.* FROM orders o
JOIN (
    SELECT id FROM orders 
    WHERE status = 1 AND created_at > '2026-01-01'
    ORDER BY updated_at DESC LIMIT 20
) t ON o.id = t.id;
```

**Step 5：验证**
- Grafana 慢查询数下降
- P99 恢复到 100ms 以下

**通用套路**：
```
慢日志 → pt-query-digest → EXPLAIN → 加索引 or 改 SQL → 验证
```

---

### 场景 6：唯一索引做订单幂等

**问题**：秒杀活动一人只能买一次，如何防止用户重复下单？

**核心思路**：**用 DB 唯一索引做兜底幂等**。

**表设计**：
```sql
CREATE TABLE orders (
    id             BIGINT PRIMARY KEY,
    order_no       VARCHAR(32) NOT NULL,
    user_id        BIGINT NOT NULL,
    activity_id    BIGINT NOT NULL,
    sku_id         BIGINT NOT NULL,
    idempotent_key VARCHAR(64) NOT NULL,  -- 幂等键
    ...
    UNIQUE KEY uk_order_no (order_no),
    UNIQUE KEY uk_idempotent (idempotent_key),
    UNIQUE KEY uk_user_activity (user_id, activity_id)  -- ★ 一人一单
);
```

**Go 实现**：
```go
func CreateOrder(ctx context.Context, req CreateOrderReq) (*Order, error) {
    order := &Order{
        OrderNo:       generateOrderNo(),
        UserID:        req.UserID,
        ActivityID:    req.ActivityID,
        SkuID:         req.SkuID,
        IdempotentKey: fmt.Sprintf("seckill:%d:%d", req.ActivityID, req.UserID),
    }
    
    err := db.WithContext(ctx).Create(order).Error
    if err != nil {
        if isMySQLDuplicateErr(err) {  // MySQL error 1062
            // 已存在，查出来返回
            var existing Order
            db.Where("user_id = ? AND activity_id = ?", req.UserID, req.ActivityID).
                First(&existing)
            return &existing, nil
        }
        return nil, err
    }
    return order, nil
}

func isMySQLDuplicateErr(err error) bool {
    var mysqlErr *mysql.MySQLError
    return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
```

**关键点**：
1. **冲突不报错，返回已存在的订单**：用户体验好
2. **不用 INSERT IGNORE**：会丢失冲突信息
3. **不用 REPLACE**：会误删已支付订单
4. **UNIQUE KEY 是最后防线**：即使前面所有幂等（Redis、Token）都失守，DB 层依然安全

---

### 场景 7：冷热数据归档

**问题**：订单表 5 年数据 10 亿行，90% 查询只看近 3 个月。

**方案**：**冷热分离**。

**Step 1：设计归档方案**

- **热表**：`orders`，只保留近 3 个月数据
- **冷表**：`orders_archive_2024_Q1`（按季度分），存到低成本存储

**Step 2：归档流程**

```sql
-- 每天凌晨执行
-- 1. 复制到归档表
INSERT INTO orders_archive_2024_Q1
SELECT * FROM orders 
WHERE created_at BETWEEN '2024-01-01' AND '2024-04-01'
  AND status IN ('COMPLETED', 'CANCELLED')  -- 只归档终态订单
LIMIT 1000;  -- 分批

-- 2. 从热表删除
DELETE FROM orders WHERE id IN (刚才复制的 id 列表) LIMIT 1000;
```

**Step 3：应用层适配**

```go
// 优先查热表
func GetOrder(ctx context.Context, orderNo string) (*Order, error) {
    var order Order
    if err := hotDB.Where("order_no = ?", orderNo).First(&order).Error; err == nil {
        return &order, nil
    }
    // 热表没有，查冷库
    quarter := getQuarterFromOrderNo(orderNo)  // 从订单号解析出季度
    return getFromArchive(ctx, quarter, orderNo)
}
```

**Step 4：进阶方案**

- **归档到 TiDB / OceanBase**：便宜且能查
- **归档到对象存储（S3/OSS）+ Presto/Athena**：极致成本，查询稍慢
- **binlog 订阅归档**：Canal 消费 binlog，实时同步到冷库

**关键陷阱**：
- **必须先复制成功再删除**：删完发现复制丢了没法恢复
- **归档表要有相同索引**：否则冷查询也慢
- **分批 + 限流**：一次删百万行会锁表 + 主从延迟
- **业务变更需通知**：归档的订单不能被后续业务改（比如售后）

---

### 场景 8：主从延迟处理

**问题**：主从延迟持续 300 秒，用户下单后看不到订单。

**诊断**：
```sql
-- 从库
SHOW SLAVE STATUS\G
```

关注：
- `Seconds_Behind_Master`：延迟秒数（不准，仅参考）
- `Slave_SQL_Running_State`：SQL 线程状态
- `Retrieved_Gtid_Set` vs `Executed_Gtid_Set`：GTID 差距（最准）

**常见原因和处理**：

**原因 1：主库有大事务**
```sql
-- 主库查大事务
SELECT * FROM information_schema.INNODB_TRX 
WHERE TIME_TO_SEC(TIMEDIFF(NOW(), trx_started)) > 60;
```
处理：kill 或让业务优化，改成分批。

**原因 2：从库单线程慢**

MySQL 5.7+ 支持并行复制：
```sql
SET GLOBAL slave_parallel_type = 'LOGICAL_CLOCK';
SET GLOBAL slave_parallel_workers = 8;
```

**原因 3：从库有业务读锁住**

从库跑了慢查询/长事务，阻塞了 SQL 线程。**分析处理慢查询**。

**原因 4：网络问题**

跨机房复制，网络抖动。切换主库到同机房。

**应用层短期止血**：
- **强制走主库**：关键查询打标记路由到主库
- **写完读主**：写完接口内的查询强制走主库
- **缓存兜底**：写完立即写缓存，客户端读缓存

**Go 中的读写分离处理**：
```go
type DBRouter struct {
    master *gorm.DB
    slaves []*gorm.DB
}

func (r *DBRouter) DB(ctx context.Context) *gorm.DB {
    // 强制走主库的标记
    if forceMaster, _ := ctx.Value("force_master").(bool); forceMaster {
        return r.master
    }
    return r.pickSlave()
}

// 使用
ctx = context.WithValue(ctx, "force_master", true)
orders := repo.List(ctx, userID)  // 走主库
```

---

### 场景 9：分布式 ID 生成（Snowflake）

**问题**：分库分表后不能用自增 ID，需要全局唯一有序 ID。

**Snowflake 结构**（64 位）：
```
| 1 bit | 41 bits         | 10 bits    | 12 bits     |
| 符号  | 毫秒时间戳       | 机器 ID    | 同毫秒序号  |
```

- 41 位时间戳：够用 69 年
- 10 位机器 ID：支持 1024 台机器
- 12 位序号：每毫秒每台机器 4096 个 ID

**Go 实现**：
```go
package snowflake

import (
    "errors"
    "sync"
    "time"
)

const (
    epoch         int64 = 1609459200000  // 2021-01-01 UTC 毫秒
    machineBits   uint8 = 10
    sequenceBits  uint8 = 12
    maxMachineID  int64 = -1 ^ (-1 << machineBits)   // 1023
    maxSequence   int64 = -1 ^ (-1 << sequenceBits)  // 4095
    machineShift  uint8 = sequenceBits                // 12
    timestampShift uint8 = sequenceBits + machineBits // 22
)

type Snowflake struct {
    mu         sync.Mutex
    machineID  int64
    lastTS     int64
    sequence   int64
}

func New(machineID int64) (*Snowflake, error) {
    if machineID < 0 || machineID > maxMachineID {
        return nil, errors.New("machineID out of range")
    }
    return &Snowflake{machineID: machineID}, nil
}

func (s *Snowflake) Next() (int64, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    now := time.Now().UnixMilli()
    
    // 时钟回拨
    if now < s.lastTS {
        return 0, errors.New("clock moved backwards")
    }

    if now == s.lastTS {
        s.sequence = (s.sequence + 1) & maxSequence
        if s.sequence == 0 {
            // 序号用完，等下一毫秒
            for now <= s.lastTS {
                now = time.Now().UnixMilli()
            }
        }
    } else {
        s.sequence = 0
    }
    s.lastTS = now

    id := ((now - epoch) << timestampShift) |
        (s.machineID << machineShift) |
        s.sequence
    return id, nil
}
```

**关键问题**：

**问题 1：时钟回拨**
- **短暂回拨**：等待时钟追上（几毫秒内）
- **长时间回拨**：告警 + 拒绝服务，避免生成重复 ID
- **NTP 用 slew 模式**（不用 step）

**问题 2：机器 ID 分配**
- 手动配置：容易冲突
- Zookeeper / etcd 注册获取：推荐
- 基于 IP 后 10 位：内网 IP 可能冲突

**问题 3：单机瓶颈**
- 每毫秒 4096 个 = 每秒 400 万，单机绝对够
- 如果不够可以水平扩展多台生成器

**替代方案**：
- **UUID**：简单但无序，索引性能差
- **号段模式（美团 Leaf）**：DB 分配号段，无时钟依赖
- **Redis INCR**：性能高但依赖 Redis 可用性
- **数据库自增 + 步长**：4 台机器步长 4，起始 0/1/2/3

---

### 场景 10：乐观锁 vs 悲观锁的选择

**场景**：库存扣减，两种方案怎么选？

**悲观锁方案**：
```sql
BEGIN;
SELECT count FROM stock WHERE sku_id = 100 FOR UPDATE;  -- 加 X 锁
-- 业务判断
UPDATE stock SET count = count - 1 WHERE sku_id = 100;
COMMIT;
```

**特点**：
- 事务级独占，其他并发请求排队
- 简单直接，逻辑清晰
- 高并发下吞吐低

**乐观锁方案（CAS）**：
```sql
-- 版本号方式
UPDATE stock 
SET count = count - 1, version = version + 1
WHERE sku_id = 100 AND version = ? AND count > 0;

-- 或者直接判断值
UPDATE stock 
SET count = count - 1 
WHERE sku_id = 100 AND count > 0;
```

**特点**：
- 无显式锁，MySQL 行锁保护单条 UPDATE 的原子性
- affected rows = 0 说明冲突或库存不足
- 需要应用层重试

**Go 代码**：

```go
// 悲观锁版本
func DeductWithPessimistic(ctx context.Context, skuID int64) error {
    return db.Transaction(func(tx *gorm.DB) error {
        var stock Stock
        if err := tx.Set("gorm:query_option", "FOR UPDATE").
            Where("sku_id = ?", skuID).
            First(&stock).Error; err != nil {
            return err
        }
        if stock.Count <= 0 {
            return errors.New("out of stock")
        }
        return tx.Model(&stock).Update("count", gorm.Expr("count - 1")).Error
    })
}

// 乐观锁版本
func DeductWithOptimistic(ctx context.Context, skuID int64) error {
    result := db.Exec(
        "UPDATE stock SET count = count - 1 WHERE sku_id = ? AND count > 0",
        skuID,
    )
    if result.Error != nil {
        return result.Error
    }
    if result.RowsAffected == 0 {
        return errors.New("out of stock or concurrent conflict")
    }
    return nil
}
```

**选择原则**：

| 场景 | 推荐 |
|---|---|
| 冲突概率高，重试代价大 | 悲观锁 |
| 冲突概率低，读多写少 | 乐观锁 |
| 需要事务内多次操作 | 悲观锁 |
| 单条更新，判断逻辑简单 | 乐观锁 |
| 高并发秒杀 | **乐观锁**（DB CAS） |
| 转账、退款等资金操作 | 悲观锁 + FOR UPDATE |

**秒杀专用组合技**：
```
Redis 预扣（Lua）→ MQ 削峰 → DB 乐观锁真扣（count > 0 兜底）
```

DB 层用乐观锁最终校验，即使前面失守也不会超卖。见 `system-design/cases/` 秒杀章节。

---

## 结语与备忘

### MySQL 面试三大主题

绝大部分深度问题最后都会绕回这三个：
1. **索引怎么用**（B+ 树 + 最左前缀 + 覆盖索引）
2. **锁怎么加**（行锁 + Next-Key Lock + MVCC）
3. **日志怎么工作**（redo/undo/binlog + 两阶段提交）

### 核心记忆点

1. **InnoDB 用 B+ 树**，聚簇索引存整行数据，二级索引存主键
2. **MVCC = 版本链 + ReadView + undo log**，RR 复用 ReadView，RC 每次新建
3. **RR 用 Next-Key Lock 缓解幻读**，快照读靠 MVCC，当前读靠间隙锁
4. **两阶段提交**：redo log(prepare) → binlog → redo log(commit)
5. **深分页用游标**，永远别写 `LIMIT 100000, 20`
6. **索引选择性看 DISTINCT / COUNT**，高的放联合索引前面
7. **主从延迟**：大事务、单线程、网络是三大主因
8. **分库分表按 user_id 哈希**，异构索引 + binlog 同步解决其他维度查询
9. **DB 唯一索引是幂等最后防线**，永远不能省
10. **Row binlog + GTID + 双 1 配置**是生产标配

### 相关文档

- [core-knowledge.md](./core-knowledge.md) — MySQL 核心知识全景（架构 / 内存 / MVCC / 锁 / 日志 / 优化器 / 调优）
- `system-design/distributed/transactions.md` — 分布式事务方案
- `databases/redis/interview-and-scenarios-go.md` — Redis 面试题
- `system-design/cases/` — 秒杀、订单幂等等场景设计

