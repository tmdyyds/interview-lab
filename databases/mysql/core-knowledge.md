# MySQL 核心知识全景

**标签**: #mysql #innodb #core #高频 #知识纲

系统化梳理 MySQL 核心知识——**架构分层 → 内存/磁盘结构 → Page/行格式 → 索引/事务/锁/日志 → 优化器 → 参数调优 → 版本差异**。定位是「知识纲要 + 底层原理补充」，配套 [interview-and-scenarios.md](./interview-and-scenarios.md) 的 60 道问答题库使用：本文讲**结构与原理**，问答讲**具体考点**，遇到概念对不上就跳过去看题目实战。

---

## 目录

1. [MySQL 整体架构](#一mysql-整体架构)
2. [InnoDB 内存结构](#二innodb-内存结构)
3. [InnoDB 磁盘结构](#三innodb-磁盘结构)
4. [Page 与行格式](#四page-与行格式)
5. [索引 B+ 树的物理组织](#五索引-b-树的物理组织)
6. [事务与 ACID 实现](#六事务与-acid-实现)
7. [MVCC 深度](#七mvcc-深度)
8. [锁体系全景](#八锁体系全景)
9. [Next-Key Lock 加锁规则详演](#九next-key-lock-加锁规则详演)
10. [日志系统与两阶段提交](#十日志系统与两阶段提交)
11. [崩溃恢复流程](#十一崩溃恢复流程)
12. [优化器与 Cost Model](#十二优化器与-cost-model)
13. [EXPLAIN 全字段速查](#十三explain-全字段速查)
14. [调优参数速查表](#十四调优参数速查表)
15. [MySQL 5.7 vs 8.0](#十五mysql-57-vs-80)
16. [面试记忆锚点](#十六面试记忆锚点)

---

## 一、MySQL 整体架构

MySQL 是**分层架构**——Server 层做 SQL 解析优化，存储引擎层做数据存取。

```
┌──────────────────────────────────────────────┐
│  Client (JDBC / mysql-cli / 应用)             │
└─────────────────┬────────────────────────────┘
                  │ MySQL 协议（TCP 3306）
┌─────────────────▼────────────────────────────┐
│                Server 层                      │
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐│
│  │连接器  │ │查询缓存│ │分析器  │ │优化器  ││ ← 8.0 移除
│  └────────┘ └────────┘ └────────┘ └────────┘│
│  ┌────────────────────────────────────────┐ │
│  │            执行器                       │ │
│  └────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────┐ │
│  │  binlog（Server 层日志，跨引擎）        │ │
│  └────────────────────────────────────────┘ │
└─────────────────┬────────────────────────────┘
                  │ Handler API
┌─────────────────▼────────────────────────────┐
│              存储引擎层                       │
│    ┌────────┐  ┌────────┐  ┌────────┐        │
│    │InnoDB  │  │MyISAM  │  │Memory  │  ...   │
│    └────────┘  └────────┘  └────────┘        │
└──────────────────────────────────────────────┘
```

### Server 层五大模块

| 模块 | 职责 | 记忆点 |
|-----|-----|-------|
| **连接器** | 建连接、认证、权限缓存 | `wait_timeout` 默认 8 小时；空连接也吃内存 |
| **查询缓存** | 命中直接返回结果 | **8.0 已移除**（写多场景命中率极低，反倒是瓶颈） |
| **分析器** | 词法、语法分析 → 生成解析树 | 报错 "You have an error in your SQL syntax" 就在这里 |
| **优化器** | 生成执行计划 | 选索引、决定 join 顺序、改写 SQL |
| **执行器** | 调用存储引擎 API | 权限二次校验（分析器只做词法权限） |

### 一条 SQL 的一生

```
SELECT * FROM t WHERE id = 1;

连接器  ─→  分析器  ─→  优化器  ─→  执行器  ─→  InnoDB
(认证)      (语法树)    (走 PK)     (调 Handler) (Buffer Pool 找页)
```

写操作还会经过 redo log + binlog 的两阶段提交（详见第十节）。

---

## 二、InnoDB 内存结构

**Buffer Pool 是 InnoDB 的核心**——所有数据读写都先经过它。

```
┌────────────────── InnoDB 内存结构 ──────────────────┐
│                                                     │
│  ┌──────────── Buffer Pool ─────────────┐          │
│  │  数据页缓存 (16KB × N)                │          │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐│          │
│  │  │ 数据页  │ │ 索引页  │ │ undo 页 ││          │
│  │  └─────────┘ └─────────┘ └─────────┘│          │
│  │  管理链表：                            │          │
│  │  · LRU（冷/热区双向链表）              │          │
│  │  · Flush List（脏页）                 │          │
│  │  · Free List（空闲页）                │          │
│  └───────────────────────────────────────┘          │
│                                                     │
│  ┌──────────── Change Buffer ───────────┐          │
│  │  二级索引 DML 缓冲（合并写盘）         │          │
│  └───────────────────────────────────────┘          │
│                                                     │
│  ┌──────── Adaptive Hash Index ────────┐            │
│  │  自动建的内存哈希，加速热点点查       │            │
│  └───────────────────────────────────────┘          │
│                                                     │
│  ┌──────────── Log Buffer ─────────────┐            │
│  │  redo log 内存缓冲                   │            │
│  └───────────────────────────────────────┘          │
└─────────────────────────────────────────────────────┘
```

### 1) Buffer Pool

- **默认大小 128MB**（生产至少 4-8GB，常见配到物理内存 60%~75%）
- **管理单位是 16KB 的 Page**
- **LRU 变体**：冷热分区，防止全表扫扫脏整个缓冲池

```
     [ 热区 5/8 ]       [ 冷区 3/8 ]
head → ●─●─●─●─●─●─●─●─●─●─●─●─● ← tail
        ↑                 ↑
    新访问的               新加载页放这里
    热点页                （老化 innodb_old_blocks_time = 1s
                          后才移入热区）
```

**为什么不用纯 LRU**：`SELECT * FROM big_table` 会把冷数据挤掉热点数据，命中率崩溃。冷热分区 + 时间窗口后再提升，让"扫过一遍就走"的页留在冷区自然被替换掉。

**关键参数**：
- `innodb_buffer_pool_size` — 总大小
- `innodb_buffer_pool_instances` — 分成几个实例（大 BP 分片，减少锁竞争）
- `innodb_old_blocks_pct` — 冷区占比，默认 37 (%)
- `innodb_old_blocks_time` — 页在冷区停留多久才升热区，默认 1000ms

### 2) Change Buffer

**优化非唯一二级索引的 DML 性能**——把随机写变顺序写：

- 索引页不在 Buffer Pool → 不立刻加载，把变更缓存到 Change Buffer
- 后续读到该页时**合并（merge）**变更到页里
- 后台线程也会定期 merge

**为什么只对二级索引**：主键（聚簇）索引写要立刻校验唯一性，必须读页。二级索引只要非唯一就没这个约束。

**读多写少场景反而害人**：merge 抖动。用 `innodb_change_buffer_max_size` 控制上限（默认 25%）。

### 3) Adaptive Hash Index (AHI)

InnoDB 观察某些页被点查（`=`）频繁访问 → 自动在内存里为它们建**哈希索引**，命中直接 O(1) 定位。

- 用户**无法显式创建**
- 高并发写场景可能成为瓶颈（AHI 锁竞争）→ 可用 `innodb_adaptive_hash_index = OFF` 关闭
- 8.0 引入 `innodb_adaptive_hash_index_parts` 分片降低竞争

### 4) Log Buffer

redo log 的内存缓冲，减少每次写 redo log 都要 fsync 的开销。参数 `innodb_log_buffer_size`（默认 16MB）。事务提交时根据 `innodb_flush_log_at_trx_commit` 决定是否 fsync。

---

## 三、InnoDB 磁盘结构

```
┌───────────────── 磁盘 ───────────────────┐
│                                          │
│  数据文件                                 │
│  ├─ System Tablespace (ibdata1)          │
│  │   · 数据字典（8.0 前）                 │
│  │   · Change Buffer 持久化               │
│  │   · Undo Log（可分离）                 │
│  │                                       │
│  ├─ File-Per-Table Tablespace (*.ibd)   │
│  │   · 每表一个文件（默认，推荐）           │
│  │                                       │
│  ├─ Undo Tablespace (undo_001/undo_002) │
│  │   · MVCC 版本链、事务回滚              │
│  │                                       │
│  └─ Temporary Tablespace (ibtmp1)       │
│      · 临时表、排序、group by 溢出        │
│                                          │
│  日志文件                                 │
│  ├─ Redo Log (ib_logfile0/1) 或 (8.0.30+ │
│  │   #innodb_redo/)                      │
│  │                                       │
│  ├─ Doublewrite Buffer (8.0.20+ 独立)   │
│  │                                       │
│  └─ Binlog (server 层，不属于 InnoDB)     │
└──────────────────────────────────────────┘
```

### File-Per-Table

MySQL 5.6.6+ 默认。每张表独立 `.ibd` 文件：
- **DROP/TRUNCATE 释放磁盘**（系统表空间只标记不释放）
- **可单表拷贝迁移**（用 transportable tablespace）
- **对齐 online DDL 的临时空间**

关闭它（`innodb_file_per_table = 0`）会把所有数据塞进 `ibdata1`，**除非有特殊理由，别关**。

### Doublewrite Buffer

**解决"部分页写"问题**：MySQL 页 16KB，OS 一次 IO 通常 4KB，写到一半崩溃 → 页损坏。

流程：
```
1. 脏页刷新前，先顺序写到 doublewrite buffer
2. 再写到真正的数据文件位置
3. 崩溃恢复时：
   - 如果数据文件页损坏，用 doublewrite 里的副本恢复
```

8.0.20 前 doublewrite 在系统表空间里；之后独立成 `#ib_16384_0.dblwr` / `#ib_16384_1.dblwr`，写入并发更高。

**关闭它**（`innodb_doublewrite = 0`）能提升写性能约 5-10%，但**数据文件损坏时无法恢复**——除非底层存储保证原子写（如 ZFS、NVMe 有 atomic write 支持）。

### 表空间的物理层次

```
Tablespace（表空间，.ibd 文件）
    └─ Segment（段）：Leaf Segment / Non-Leaf Segment
        └─ Extent（区，1MB = 64 页）
            └─ Page（页，16KB）← ★ IO 最小单位
                └─ Row（行）
```

**Extent 分配策略**：
- 表数据 < 32 页：一次分配 1 个页，逐次翻倍（Fragment Pages）
- 超过 → 一次分配一个 Extent（1MB），减少碎片

---

## 四、Page 与行格式

### 16KB Page 的内部布局

```
┌──────────────────────────────────┐
│ File Header (38B)                │ ← 页号、前后页指针（B+ 树叶子双向链表）
├──────────────────────────────────┤
│ Page Header (56B)                │ ← 记录数、free space 偏移等
├──────────────────────────────────┤
│ Infimum + Supremum (26B)         │ ← 虚拟"最小/最大记录"，B+ 树内边界
├──────────────────────────────────┤
│                                  │
│ User Records (从上往下增长)       │ ← 实际行数据
│         ...                      │
│                                  │
│ Free Space (中间空隙)             │
│                                  │
│         ...                      │
│ Page Directory (从下往上)         │ ← 稀疏索引：每 4-8 行放一个槽（slot）
│                                  │    页内定位靠二分槽 → 链表微扫描
├──────────────────────────────────┤
│ File Trailer (8B, checksum)      │ ← 页完整性校验
└──────────────────────────────────┘
```

**页内查找**：不是遍历——而是**二分 Page Directory 槽位**，再在小范围（≤8 行）链表扫描。

### 行格式 Compact / Dynamic / Compressed

| 格式 | MySQL 版本 | 变长字段处理 | 大对象溢出 | 备注 |
|-----|-----------|-------------|-----------|------|
| Redundant | 5.0- | 存偏移 | 前 768B 存页内，余存溢出页 | 老古董，不用 |
| **Compact** | 5.1+ | 变长字段长度列表 | 前 768B 页内 + 溢出 | 5.7 之前默认 |
| **Dynamic** | 5.7+ 默认 | 完全外部溢出 | 页内只留 20B 指针 | **生产推荐** |
| Compressed | 5.7+ | 同 Dynamic | 加 zlib 压缩 | CPU 换空间 |

**Dynamic 优势**：BLOB/TEXT 完全外部存，主页更紧凑，B+ 树扇出更大。

```sql
ALTER TABLE t ROW_FORMAT=DYNAMIC;
```

### 一行数据的物理格式（Compact/Dynamic）

```
┌─────────────────────────────────────────────────────┐
│  变长字段长度列表（逆序）                             │
│  NULL 值列表（bit 位图）                             │
│  记录头信息 (5B)                                     │
│  ─────────────────────────────                       │
│  DB_ROW_ID    (6B, 无主键才有)                       │
│  DB_TRX_ID    (6B, 最后修改事务 ID) ← MVCC 靠它      │
│  DB_ROLL_PTR  (7B, 指向 undo log)  ← 版本链靠它      │
│  ─────────────────────────────                       │
│  列 1 数据                                            │
│  列 2 数据                                            │
│  ...                                                 │
└─────────────────────────────────────────────────────┘
```

**必背**：`DB_TRX_ID` 和 `DB_ROLL_PTR` 是 MVCC 的两根柱子。

---

## 五、索引 B+ 树的物理组织

### 高度与容量估算

假设：主键 8B、指针 6B → 每个索引项 14B → 每个非叶子页可存 `16KB / 14B ≈ 1170` 个索引项。

假设一行数据 1KB → 每个叶子页存 16 行。

```
高度 2：1170 × 16       = 18720 行             ≈ 2 万
高度 3：1170 × 1170 × 16 = 21902400 行         ≈ 2200 万 ★
高度 4：≈ 256 亿行
```

**几乎所有线上表 3 层 B+ 树够用**，最多 4 层。所以点查通常 **3-4 次磁盘 IO**。

### 页分裂 vs 页合并

**插入触发页分裂**：
- 页快满时插入新数据 → 分裂成两个页
- 递增主键：只在尾页分裂，代价小
- **随机主键（如 UUID）**：任意页都可能分裂 → 大量碎片和写放大

**删除触发页合并**：
- 页利用率低于阈值（`MERGE_THRESHOLD`，默认 50%）→ 与相邻页合并
- 也会造成短暂的页移动开销

**Fill Factor**：InnoDB 索引默认预留约 1/16 空间给未来插入，减少分裂概率。

### 索引类型速查

| 类型 | 说明 | 典型场景 |
|-----|------|---------|
| Primary Key | 聚簇索引，叶子存整行 | 表的物理排序 |
| Secondary | 叶子存主键值 | 常用的 idx_xxx |
| Unique | 唯一约束 + 索引 | 手机号、邮箱等 |
| Composite | 联合索引，最左前缀 | 高频组合查询 |
| Covering | 查询字段全在索引里 | 深分页、避免回表 |
| Prefix | 长字符串前 N 字节 | URL、长文本 |
| Full-Text | 分词倒排索引 | 简单文本搜索（生产建议用 ES） |
| Spatial | 空间索引 (R-Tree) | 地理坐标 |
| Functional | 函数索引 (8.0+) | `IDX((JSON_EXTRACT(...)))` |
| Invisible | 隐藏索引 (8.0+) | 灰度删索引，先隐藏观察 |

### 索引失效速查

| 场景 | 原因 |
|-----|------|
| 函数/表达式包裹字段 | 索引存的是原值，不是函数结果 |
| 隐式类型转换 | `WHERE phone = 1380000` 而 phone 是 VARCHAR |
| LIKE '%xx'（前导通配符）| B+ 树无法定位起点 |
| `!=` / `<>` / `NOT IN` | 需要扫大量行，优化器可能放弃索引 |
| `OR` 两侧字段不同索引 | 除非各自都有索引且用 `index_merge` |
| 联合索引不满足最左前缀 | `(a,b,c)` 只用 `b` |
| 优化器判断走索引更慢 | 表数据分布不均、直方图过期 |

排查：`EXPLAIN` 看 `key` 是否为 NULL、`type` 是否为 ALL / index。

---

## 六、事务与 ACID 实现

### ACID 与底层机制

| 特性 | 靠什么实现 |
|-----|----------|
| **A** 原子性 | Undo Log（回滚） |
| **C** 一致性 | AID 三者协同 + 约束（PK/UK/FK/CHECK） |
| **I** 隔离性 | MVCC（读）+ 锁（写） |
| **D** 持久性 | Redo Log（WAL + fsync） |

### 隔离级别与并发副作用

| 级别 | 脏读 | 不可重复读 | 幻读 | 加锁 |
|-----|------|-----------|------|------|
| Read Uncommitted (RU) | ✅ | ✅ | ✅ | 极少 |
| Read Committed (RC) | ❌ | ✅ | ✅ | 记录锁（无间隙锁） |
| **Repeatable Read (RR)** ★ | ❌ | ❌ | ⚠️（快照读无、当前读有） | 记录锁 + 间隙锁 |
| Serializable | ❌ | ❌ | ❌ | 读加共享锁 |

**MySQL 默认 RR**（Oracle/PG 默认 RC）。

**RR 下幻读的两面性**：
- 快照读（普通 SELECT）：ReadView 固定 → **无幻读**
- 当前读（`FOR UPDATE`、`INSERT`、`UPDATE`）：读的是最新版本，加**间隙锁**才能防幻读

---

## 七、MVCC 深度

### 三大要素

```
1. DB_TRX_ID    每行的"最后修改事务 ID"
2. DB_ROLL_PTR  指向 undo log 里的旧版本
3. Read View    可见性判断的快照
```

### 版本链

```
data row (最新)              undo log 版本链
┌─────────────┐              ┌─────────┐
│ trx_id: 100 │─roll_ptr──▶ │ trx: 80 │─roll_ptr──▶ ...
│ name: Bob   │              │ Alice   │
└─────────────┘              └─────────┘
```

### ReadView 结构

```go
type ReadView struct {
    m_ids       []trx_id  // 生成快照时活跃的事务列表
    min_trx_id  trx_id    // m_ids 中最小值
    max_trx_id  trx_id    // 系统下一个将分配的 trx_id
    creator_trx trx_id    // 生成 view 的事务自己
}
```

### 可见性判断（4 步）

对于当前遍历到的某个版本的 `DB_TRX_ID`：

1. `DB_TRX_ID == creator_trx` → **可见**（自己改的）
2. `DB_TRX_ID < min_trx_id`   → **可见**（生成 view 前就提交了）
3. `DB_TRX_ID >= max_trx_id`  → **不可见**（生成 view 后才启动）
4. `min_trx_id <= DB_TRX_ID < max_trx_id`：
   - 在 `m_ids` 里 → **不可见**（当时还未提交）
   - 不在 → **可见**（当时已提交）

不可见就沿 `DB_ROLL_PTR` 找旧版本，直到找到可见的。

### RC vs RR 的关键差异

| | RC | RR |
|---|---|---|
| ReadView 生成时机 | **每次 SELECT 都新建** | **第一次 SELECT 时建，事务内复用** |
| 结果 | 能看到别人的最新提交 | 快照固定 |

### Undo Log 的清理（Purge）

- purge 线程异步删除"不再有活跃事务需要的"旧版本
- **长事务的克星**：只要一个事务不结束，`min_trx_id` 就卡住，purge 无法推进，undo tablespace 无限增长
- 监控 `INFORMATION_SCHEMA.INNODB_TRX` 找长事务

---

## 八、锁体系全景

### 锁的分类维度

```
按粒度  ─── 全局锁 / 表锁 / 行锁
按模式  ─── 共享锁 S / 排他锁 X
按算法  ─── Record Lock / Gap Lock / Next-Key Lock / Insert Intention Lock
按用途  ─── 意向锁 IS/IX / 自增锁 AUTO-INC / MDL 元数据锁
```

### 全表锁

| 锁 | 命令 | 场景 |
|---|-----|-----|
| Global Read Lock | `FLUSH TABLES WITH READ LOCK` | 全库备份（mysqldump --lock-tables 时用） |
| Table Lock | `LOCK TABLES ... READ/WRITE` | 老引擎；InnoDB 极少用 |
| MDL 读锁 | 自动，DML 时加 | 保护表结构不被并发 DDL 改 |
| MDL 写锁 | 自动，DDL 时加 | Online DDL 前的短暂阻塞点 |

**MDL 陷阱**：一个未提交的长事务持 MDL 读锁 → 一条 DDL 请求 MDL 写锁被阻塞 → 后续所有 DML 也被阻塞（DDL 排在队列前）→ **表被"锁死"**。

### 行锁（InnoDB）

| 锁 | 别名 | 锁范围 |
|---|-----|-------|
| **Record Lock** | 记录锁 | 单条索引记录 |
| **Gap Lock** | 间隙锁 | 索引区间 `(a, b)` 之间的间隙 |
| **Next-Key Lock** | 临键锁 | Record + Gap，左开右闭 `(a, b]` |
| **Insert Intention Lock** | 插入意向锁 | 特殊 Gap Lock，多个 insert 到同一间隙不互斥 |

### 意向锁 (IS/IX)

**表级"标记锁"**，告诉系统「这张表里已经有行锁了」：

- 事务加行 S 锁前，先给表加 IS
- 事务加行 X 锁前,先给表加 IX
- 表锁请求时快速判断：见到 IX 就知道有人在写某行，不用去扫每行

**兼容矩阵**：

|      | IS | IX | S  | X  |
|------|----|----|----|----|
| IS   | ✅ | ✅ | ✅ | ❌ |
| IX   | ✅ | ✅ | ❌ | ❌ |
| S    | ✅ | ❌ | ✅ | ❌ |
| X    | ❌ | ❌ | ❌ | ❌ |

**记忆点**：IS/IX 之间**永远兼容**；意向锁只和表级 S/X 冲突。

### 自增锁 (AUTO-INC)

`AUTO_INCREMENT` 字段的并发控制。参数 `innodb_autoinc_lock_mode`：

| 模式 | 值 | 行为 |
|-----|---|------|
| Traditional | 0 | 每次 insert 都加表锁至语句结束 |
| Consecutive | 1 | 简单 insert 立即释放；批量 insert 语句级 |
| **Interleaved** | 2 (8.0 默认) | 不加锁，批量分配 ID 可能不连续 |

**注意**：模式 2 + Statement binlog 会导致主从不一致。生产用 **Row binlog** 才能安全用 2。

---

## 九、Next-Key Lock 加锁规则详演

**只在 RR 隔离级别下**加 Gap/Next-Key。

### 两大原则 + 两个优化

**原则**：
1. 加锁的基本单位是 **Next-Key Lock**（左开右闭）
2. 查找过程中访问到的对象才加锁

**优化**：
- **索引等值查询 + 唯一索引 + 命中** → 退化为 **Record Lock**
- **索引等值查询 + 未命中** → 退化为 **Gap Lock**

### 场景演绎

假设表 `t(id PK, age KEY)`，索引 `age` 上有值 `10, 20, 30`，间隙就是：

```
(-∞, 10],  (10, 20],  (20, 30],  (30, +∞)
```

#### 情况 1：唯一索引等值命中

```sql
SELECT * FROM t WHERE id = 5 FOR UPDATE;   -- id=5 存在
```
→ **只加 Record Lock on id=5**。因为唯一索引不会有幻读，无需 Gap。

#### 情况 2：唯一索引等值未命中

```sql
SELECT * FROM t WHERE id = 7 FOR UPDATE;   -- id=7 不存在
```
→ **加 Gap Lock on (5, 10)**。防止别人插入 id=7 造成幻读。

#### 情况 3：普通索引等值命中

```sql
SELECT * FROM t WHERE age = 20 FOR UPDATE;
```
→ **加 Next-Key Lock (10, 20]** + **Gap Lock (20, 30)**。

普通索引可能有重复值，右边的 gap 也要锁住防新插入。

#### 情况 4：范围查询

```sql
SELECT * FROM t WHERE age > 15 AND age <= 25 FOR UPDATE;
```
→ 覆盖 `(10, 20]` + `(20, 30]` 两个 Next-Key。

即使实际范围到 25 结束，Next-Key 的粒度是索引间隙，只能整段锁。

### 常见死锁模式

```
T1: INSERT age=15  → 请求 (10, 20) 的插入意向
T2: INSERT age=17  → 请求 (10, 20) 的插入意向
两者本身兼容 ✅

但如果两者之前都执行了：
T1: SELECT ... WHERE age=15 FOR UPDATE
T2: SELECT ... WHERE age=17 FOR UPDATE
    → 各自持有 (10, 20) 上的 Gap Lock
    → 双方都想升级为插入意向 → 相互等待 → 死锁
```

InnoDB 的死锁检测会 kill 掉其一。

---

## 十、日志系统与两阶段提交

### 三大日志一图定型

| 日志 | 归属 | 内容 | 作用 | 写入方式 |
|-----|-----|-----|-----|---------|
| **redo log** | InnoDB | 物理日志（页 P offset O 变成什么） | 崩溃恢复 (D) | 循环写，覆盖旧的 |
| **undo log** | InnoDB | 逻辑反向操作（insert→delete） | 回滚 (A) + MVCC | 环形，靠 purge 清理 |
| **binlog** | Server 层 | 逻辑操作（SQL 或行变更） | 主从复制 + 时间点恢复 | 顺序追加 |

### WAL：Write-Ahead Log

先写日志，再改数据页——**写日志是顺序 IO，改数据页可能是随机 IO**。事务提交只要日志落盘就算成功，数据页可以慢慢刷。

```
提交事务
   ↓
写 redo log（顺序 IO，快）
   ↓
数据页在 Buffer Pool 里改（内存）
   ↓
后台线程慢慢刷脏页到磁盘
```

### 两阶段提交（2PC）

事务提交时，redo log 和 binlog 必须一致。InnoDB 用 2PC 协调：

```
1. Prepare 阶段：
   redo log 写入 + 标记为 PREPARE 状态

2. Commit 阶段：
   写 binlog
   redo log 标记为 COMMIT 状态
```

**崩溃时的一致性推导**：
- 崩溃在 Prepare 后、binlog 未写 → 事务回滚（redo 里的 PREPARE 无 binlog 匹配）
- 崩溃在 binlog 写完、redo 未 COMMIT → 事务**提交**（有 binlog 就一定要提交，因为从库可能已经复制）
- 崩溃在 COMMIT 之后 → 事务正常提交

**判定桥梁**：redo log 里的 `XID` 与 binlog 里的 `XID`。

### 双 1 配置

|参数 | 含义 | 双 1 |
|-----|-----|-----|
| `innodb_flush_log_at_trx_commit` | 事务提交时 redo 刷盘策略 | **1**（每次都 fsync） |
| `sync_binlog` | binlog 组提交后 fsync 次数 | **1**（每次都 fsync） |

- 双 1：最强一致，性能低。金融必备。
- `0`：redo 不 fsync，MySQL 崩就丢
- `2`：redo 写到 OS 缓存，MySQL 崩不丢、机器崩才丢

### 组提交（Group Commit）

多个事务的 fsync 合并成一次。三阶段：**flush → sync → commit**。参数：
- `binlog_group_commit_sync_delay` — 等待多少微秒收集事务
- `binlog_group_commit_sync_no_delay_count` — 收满多少个立即 fsync

高 QPS 下**开一点点 delay 反而更快**（用一次 fsync 撑起更多事务）。

---

## 十一、崩溃恢复流程

```
1. 启动时扫 redo log
2. 找出所有未刷盘的脏页（redo LSN > 数据页 LSN）
3. 重放 redo，让数据页恢复到崩溃前状态
4. 扫描 undo log 里 PREPARE 状态的事务
   ├─ 如果 binlog 有对应 XID → 提交
   └─ 如果 binlog 没有 → 回滚
5. 打开对外服务
```

**关键点**：redo 是**幂等**的（同一条 redo 重放多次结果相同），所以恢复安全。

**LSN（Log Sequence Number）**：redo log 的全局递增指针。每个数据页有 `PAGE_LSN`，每条 redo 有起始 LSN，比较两者就知道页是否需要恢复。

---

## 十二、优化器与 Cost Model

### 优化器的三大工作

1. **索引选择**：基于统计信息估算 rows，选 cost 最低的索引
2. **JOIN 顺序**：小表驱动大表，动态规划找最优顺序
3. **子查询改写**：`IN` → 半连接，`EXISTS` → 反连接等

### Cost 计算简化公式

```
Cost = IO cost + CPU cost

IO cost  ≈ 扫描 pages 数 × page_read_cost（默认 1.0）
CPU cost ≈ 处理行数 × row_evaluate_cost（默认 0.2）
```

8.0 引入 `mysql.server_cost` / `mysql.engine_cost` 表，可以调整这些常量。

### 统计信息

- **行数估算**：从 InnoDB 页采样计算（`innodb_stats_persistent_sample_pages`，默认 20 页）
- **索引选择性**：`SHOW INDEX FROM t` 里的 `Cardinality`
- **过期时机**：数据变化 10%+ 或手动 `ANALYZE TABLE`
- **直方图（8.0+）**：对非索引列也能采集分布，帮助优化器更精确估算 `WHERE age > 30` 之类

### 优化器 Hint（8.0+ 强化）

```sql
-- 强制走索引
SELECT /*+ INDEX(t idx_name) */ * FROM t WHERE name = 'jake';

-- 强制 JOIN 顺序
SELECT /*+ JOIN_ORDER(t1, t2, t3) */ ...

-- 忽略索引
SELECT /*+ NO_INDEX(t idx_bad) */ ...

-- 强制走全表
SELECT /*+ NO_INDEX_MERGE(t) */ ...
```

比老式 `FORCE INDEX` 更精细，可作用于子查询。

---

## 十三、EXPLAIN 全字段速查

```sql
EXPLAIN SELECT * FROM t1 JOIN t2 ON t1.id = t2.tid WHERE t1.name = 'jake';
```

| 字段 | 含义 | 关注 |
|-----|-----|------|
| `id` | 查询序号 | 相同 id 从上到下执行；子查询 id 递增 |
| `select_type` | 查询类型 | SIMPLE / PRIMARY / SUBQUERY / DERIVED |
| `table` | 表名 | 派生表显示 `<derived N>` |
| `partitions` | 命中的分区 | 分区表才有 |
| **`type`** ★ | 访问类型 | system > const > eq_ref > ref > range > index > **ALL**（差） |
| `possible_keys` | 可能用的索引 | 空表示没索引可用 |
| **`key`** ★ | 实际用的索引 | NULL 表示走全表 |
| `key_len` | 索引使用长度 | 判断联合索引用了几列 |
| `ref` | 与索引匹配的列 | `const`（常量）/ 表.列 |
| **`rows`** ★ | 预估扫描行数 | 越少越好 |
| `filtered` | 预估过滤后剩余百分比 | 越高越准 |
| **`Extra`** ★ | 额外信息 | 见下 |

### Extra 关键值

| 值 | 含义 | 好坏 |
|---|-----|-----|
| `Using index` | 覆盖索引，不回表 | ✅ 优秀 |
| `Using where` | 有 WHERE 过滤（可能索引外过滤） | 中性 |
| `Using index condition` | 索引下推（ICP） | ✅ 好 |
| `Using temporary` | 用临时表（GROUP BY / UNION） | ⚠️ 差 |
| `Using filesort` | 排序无法用索引 | ⚠️ 差 |
| `Using join buffer (BNL/BKA)` | JOIN 无索引，用连接缓冲 | ⚠️ 关注 |

### EXPLAIN ANALYZE（8.0.18+）

**实际执行**并返回真实耗时和行数，比 EXPLAIN 的估算更真实：

```sql
EXPLAIN ANALYZE SELECT * FROM t WHERE id > 100;
-- -> Filter: (t.id > 100)  (actual time=0.03..15.2 rows=8000 loops=1)
--     -> Table scan on t  (actual time=0.02..12.1 rows=10000 loops=1)
```

**注意会真的跑一次查询**（写操作也会执行），生产上慎用。

---

## 十四、调优参数速查表

按重要度排序，生产建议值供参考（**具体值必须结合硬件和 workload 压测**）。

### 内存

| 参数 | 默认 | 生产建议 | 说明 |
|-----|-----|---------|------|
| `innodb_buffer_pool_size` | 128M | **60-75% 物理内存** | 最核心 |
| `innodb_buffer_pool_instances` | 8 | ≥1G 时每 GB 一个实例 | 大 BP 减少锁 |
| `innodb_log_buffer_size` | 16M | 32-64M | 大事务多可提高 |
| `tmp_table_size` / `max_heap_table_size` | 16M | 64-128M | 临时表内存上限 |
| `sort_buffer_size` | 256K | 保持默认 | **每连接分配**，别过大 |

### 日志与刷盘

| 参数 | 默认 | 生产建议 | 说明 |
|-----|-----|---------|------|
| `innodb_log_file_size` | 48M (5.7) / 100M (8.0) | 1-4G | 大日志减少 checkpoint |
| `innodb_log_files_in_group` | 2 | 2 | (8.0.30 后废弃，改用 `innodb_redo_log_capacity`) |
| `innodb_flush_log_at_trx_commit` | 1 | **1** (金融) / 2 (可容忍机器崩) | 事务日志刷盘策略 |
| `sync_binlog` | 1 | **1** | binlog fsync 策略 |
| `innodb_flush_method` | fsync | **O_DIRECT** | 绕过 OS 缓存，避免双缓冲 |
| `innodb_io_capacity` | 200 | 2000 (SSD) / 20000 (NVMe) | 后台 IO 速率 |
| `innodb_io_capacity_max` | 2000 | 2 × capacity | 突发上限 |

### 并发

| 参数 | 默认 | 生产建议 | 说明 |
|-----|-----|---------|------|
| `max_connections` | 151 | 500-2000 | 太大会消耗内存和线程栈 |
| `innodb_thread_concurrency` | 0 | 0 (不限) | 除非明确需要限制 |
| `innodb_read_io_threads` / `write_io_threads` | 4 | 8-16 | IO 密集时提高 |

### 事务与锁

| 参数 | 默认 | 生产建议 | 说明 |
|-----|-----|---------|------|
| `transaction_isolation` | REPEATABLE-READ | RR 或 RC | 大厂常 RC |
| `innodb_lock_wait_timeout` | 50s | 5-10s | 快速失败，避免堆积 |
| `innodb_deadlock_detect` | ON | ON | 关闭有些场景更好，但要能承受长等待 |
| `innodb_rollback_on_timeout` | OFF | 视业务 | 超时是否回滚整个事务 |

---

## 十五、MySQL 5.7 vs 8.0

### 核心变化

| 主题 | 5.7 | 8.0 |
|-----|-----|-----|
| **数据字典** | 存在 .frm 文件 + ibdata | 完全事务性，存 InnoDB 表 |
| **查询缓存** | 有（默认关） | **移除** |
| **默认字符集** | utf8mb4 (5.7.7+) | utf8mb4（`utf8mb4_0900_ai_ci`） |
| **窗口函数** | ❌ | ✅ (`ROW_NUMBER`, `LAG`, `LEAD`, ...) |
| **CTE (WITH)** | ❌ | ✅（含递归） |
| **JSON** | 支持 | 增强（多值索引、部分更新） |
| **降序索引** | 定义了但存 asc | 真实降序存储 |
| **不可见索引** | ❌ | ✅ (`INVISIBLE`) |
| **函数索引** | ❌ | ✅ |
| **DDL 原子性** | ALTER 崩溃后可能残留 | 原子（数据字典事务化） |
| **默认加密** | 表空间加密可用 | 增强，支持 redo/undo 加密 |
| **角色 (Role)** | ❌ | ✅ |
| **优化器** | 基础直方图 | **完整直方图统计**（非索引列也能采集） |
| **哈希连接** | ❌ | ✅ 8.0.18+（大幅加速无索引 JOIN） |
| **doublewrite** | 在系统表空间 | 独立文件，并发更高 |
| **参数持久化** | 修改要写配置文件 | `SET PERSIST` 直接写 |

### 迁移注意

- **utf8mb3 → utf8mb4** 排序规则变化：`utf8mb4_general_ci` → `utf8mb4_0900_ai_ci`，可能影响索引和外键。
- **权限系统重构**：`mysql.user` 表结构变了。
- **caching_sha2_password** 成默认认证插件，老客户端连不上要改用 `mysql_native_password`。
- **保留字扩充**：如 `RANK`、`ROW` 都成关键字，老 SQL 里做字段名需要加反引号。

---

## 十六、面试记忆锚点

### 一句话金句

- **InnoDB 三大件**：Buffer Pool + Undo Log + Redo Log
- **MVCC 三大件**：DB_TRX_ID + DB_ROLL_PTR + Read View
- **崩溃恢复靠 redo + XID 判定，回滚靠 undo**
- **两阶段提交是为了 redo 和 binlog 一致，不是分布式事务**
- **RR ≠ 无幻读**：快照读无、当前读有；间隙锁才能防
- **索引失效第一杀手是隐式类型转换**
- **深分页优化就是覆盖索引 + 主键回查**
- **B+ 树 3 层撑 2000 万行，几乎所有点查 3 次 IO**

### 万能追问链

面试官问 SQL 慢，往这个链条走：

```
SQL 慢
  → EXPLAIN 看 type / key / rows / Extra
  → 索引是否命中（失效场景速查）
  → 索引选对了吗（Cost Model + 直方图）
  → 有无回表（覆盖索引？ICP？）
  → 有无 filesort / temporary（排序/分组优化）
  → 有无锁等待（SHOW ENGINE INNODB STATUS）
  → 有无长事务/慢主从（长事务链条）
  → 数据分布问题（分片/归档）
```

### 学习路径推荐

1. **本文（core-knowledge.md）**：先建立框架
2. **[interview-and-scenarios.md](./interview-and-scenarios.md)**：60 题练手 + 10 个场景实战
3. 官方文档：InnoDB Locking and Transaction Model 章节读一遍
4. 《MySQL 是怎样运行的》（掘金小册）：源码级别深挖
5. 《高性能 MySQL》第 4 版：架构与优化的经典参考

---

## 相关文档

- [interview-and-scenarios.md](./interview-and-scenarios.md) — 60 道问答题库 + 10 个生产场景
- [../redis/README.md](../redis/README.md) — Redis 缓存与 MySQL 一致性
- [../../go/ecosystem/gorm-deep.md](../../go/ecosystem/gorm-deep.md) — ORM 层与 N+1 问题
