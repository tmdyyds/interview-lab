# Redis 持久化（RDB / AOF / 混合）

**标签**: #redis #persistence #高频
**难度**: ⭐⭐⭐

---

## 一、总览

Redis 数据在内存中，断电就没了。持久化 = 把内存数据写到磁盘，重启后恢复。

| 方式 | 原理 | 优点 | 缺点 |
|------|------|------|------|
| RDB | 某个时间点的内存快照（二进制文件） | 恢复快、文件小、性能影响低 | 数据可能丢失（两次快照之间） |
| AOF | 追加记录每条写命令（文本日志） | 数据安全性高（最多丢 1 秒） | 文件大、恢复慢 |
| 混合持久化 | RDB 快照 + 增量 AOF（Redis 4.0+） | 兼顾两者优点 | 文件格式复杂，不可读 |

---

## 二、RDB（Redis DataBase）

### 2.1 原理

在某个时间点把整个内存数据集生成一个压缩二进制文件 `dump.rdb`。

```
         ┌──────────────────────────┐
         │     Redis 主进程          │
         │   （继续处理读写请求）      │
         └──────────┬───────────────┘
                    │ fork()
                    ▼
         ┌──────────────────────────┐
         │     子进程（COW 机制）     │
         │   遍历内存 → 写 dump.rdb  │
         └──────────────────────────┘
```

### 2.2 触发方式

```bash
# 手动触发
SAVE        # 阻塞主进程（生产禁用）
BGSAVE      # 后台 fork 子进程生成

# 自动触发（redis.conf 配置）
save 900 1      # 900 秒内至少 1 次写操作 → 触发 BGSAVE
save 300 10     # 300 秒内至少 10 次写操作
save 60 10000   # 60 秒内至少 10000 次写操作
```

### 2.3 fork + COW（Copy-On-Write）详解

```
fork 前：
  主进程内存 → [数据页A] [数据页B] [数据页C]

fork 后（父子共享物理页）：
  主进程 → [页表] ──→ [数据页A] [数据页B] [数据页C]
  子进程 → [页表] ──→     ↑          ↑          ↑
                       （共享同一份物理内存）

主进程有写操作时（COW）：
  主进程写数据页B → 操作系统拷贝一份新的页B' 给主进程
  子进程仍然读旧的页B → 保证快照一致性

  主进程 → [页表] ──→ [数据页A] [数据页B'] [数据页C]
  子进程 → [页表] ──→ [数据页A] [数据页B]  [数据页C]
```

**关键点**：
- fork 瞬间几乎不复制内存（只复制页表，微秒级）
- 子进程看到的是 fork 瞬间的快照（一致的时间点）
- 主进程继续处理请求，写操作触发 COW（按需拷贝页）
- 极端情况（fork 后大量写）：内存翻倍（每页都被写了一份）

### 2.4 优缺点

| 优点 | 缺点 |
|------|------|
| 文件紧凑，适合备份/灾恢 | 两次快照之间的数据可能丢失 |
| 恢复速度快（直接加载二进制） | fork 大内存实例可能导致毫秒级卡顿 |
| 对主进程性能影响小 | 不适合对数据安全性要求极高的场景 |

### 2.5 使用示例

```php
<?php

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

// 手动触发 RDB 快照
$redis->bgSave();

// 查看上次快照时间
$lastSave = $redis->lastSave(); // Unix 时间戳
echo "Last RDB save: " . date('Y-m-d H:i:s', $lastSave) . "\n";

// 查看 RDB 状态
$info = $redis->info('persistence');
echo "rdb_last_bgsave_status: " . $info['rdb_last_bgsave_status'] . "\n";
echo "rdb_last_bgsave_time_sec: " . $info['rdb_last_bgsave_time_sec'] . "\n";
```

---

## 三、AOF（Append Only File）

### 3.1 原理

把每条写命令追加到 `appendonly.aof` 文件末尾。恢复时重放所有命令。

```
客户端: SET name "Jake"
         │
         ▼
Redis 执行命令 → 写入 AOF 缓冲区 → 根据策略写入磁盘

AOF 文件内容：
*3\r\n$3\r\nSET\r\n$4\r\nname\r\n$4\r\nJake\r\n
*3\r\n$3\r\nSET\r\n$3\r\nage\r\n$2\r\n28\r\n
...
```

### 3.2 写入策略（fsync）

```bash
# redis.conf
appendonly yes
appendfsync always      # 每条命令都 fsync → 最安全，最慢
appendfsync everysec    # 每秒 fsync 一次 → 推荐（最多丢 1 秒数据）
appendfsync no          # 由操作系统决定 → 最快，可能丢几十秒数据
```

| 策略 | 数据安全 | 性能 | 丢失数据量 |
|------|----------|------|-----------|
| always | 最高 | 最低 | 不丢 |
| everysec | 高 | 高 | 最多 1 秒 |
| no | 低 | 最高 | 取决于 OS（通常 30s） |

**生产推荐**：`appendfsync everysec`

### 3.3 AOF 重写（Rewrite）

AOF 文件会越来越大（同一个 key 被改了 100 次，文件里有 100 条命令）。重写 = 用最终状态生成最小等效命令集。

```
重写前 AOF：
  SET counter 1
  INCR counter
  INCR counter
  INCR counter
  ...（100 条）

重写后 AOF：
  SET counter 100     ← 一条命令等效
```

**重写流程**：

```
         ┌──────────────────────────────────┐
         │           Redis 主进程             │
         │  1. fork 子进程                    │
         │  2. 继续处理新请求                  │
         │  3. 新写命令同时写入：              │
         │     - 旧 AOF 文件（保证安全）       │
         │     - AOF 重写缓冲区               │
         └──────────────┬───────────────────┘
                        │ fork
                        ▼
         ┌──────────────────────────────────┐
         │          子进程                    │
         │  遍历当前内存 → 生成新 AOF 文件     │
         │  （基于 fork 瞬间的快照）           │
         └──────────────┬───────────────────┘
                        │ 完成
                        ▼
         ┌──────────────────────────────────┐
         │          主进程                    │
         │  4. 把重写缓冲区内容追加到新 AOF    │
         │  5. 原子替换旧 AOF → 新 AOF        │
         └──────────────────────────────────┘
```

**触发方式**：

```bash
# 手动
BGREWRITEAOF

# 自动（redis.conf）
auto-aof-rewrite-percentage 100    # AOF 文件比上次重写后大 100% 时触发
auto-aof-rewrite-min-size 64mb     # AOF 文件至少 64MB 才触发
```

### 3.4 使用示例

```php
<?php

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

// 手动触发 AOF 重写
$redis->bgRewriteAOF();

// 查看 AOF 状态
$info = $redis->info('persistence');
echo "aof_enabled: " . $info['aof_enabled'] . "\n";
echo "aof_current_size: " . $info['aof_current_size'] . "\n";
echo "aof_base_size: " . $info['aof_base_size'] . "\n";           // 上次重写后的大小
echo "aof_last_rewrite_time_sec: " . $info['aof_last_rewrite_time_sec'] . "\n";
```

---

## 四、混合持久化（Redis 4.0+）

### 4.1 原理

AOF 重写时不再生成纯文本命令，而是：**先写一个 RDB 格式的快照，再追加重写期间的增量 AOF 命令**。

```
混合 AOF 文件结构：
┌──────────────────────────────────┐
│  RDB 格式数据（fork 瞬间快照）    │  ← 二进制，加载快
├──────────────────────────────────┤
│  增量 AOF 命令（重写期间的写入）   │  ← 文本，量小
└──────────────────────────────────┘
```

### 4.2 配置

```bash
# redis.conf（Redis 4.0+ 默认开启）
aof-use-rdb-preamble yes
```

### 4.3 恢复流程

```
Redis 启动
    │
    ├── 有 AOF 文件？
    │       ├── yes → 加载 AOF（混合模式：先加载 RDB 部分 + 重放增量 AOF）
    │       └── no  → 加载 dump.rdb
    │
    └── 恢复完成
```

**注意**：如果同时开启了 RDB 和 AOF，Redis 优先加载 AOF（数据更完整）。

### 4.4 优缺点

| 优点 | 缺点 |
|------|------|
| 加载速度快（RDB 部分） | 文件混合格式，不可读 |
| 数据安全（AOF 增量） | 兼容性：旧版本 Redis 无法识别 |
| 文件比纯 AOF 小得多 | |

---

## 五、三种方式对比

| 对比项 | RDB | AOF | 混合 |
|--------|-----|-----|------|
| 数据安全性 | 低（可能丢分钟级） | 高（最多丢 1 秒） | 高 |
| 文件大小 | 小（压缩二进制） | 大（文本命令） | 中 |
| 恢复速度 | 快 | 慢（逐条重放） | 快 |
| 性能影响 | fork 瞬间卡顿 | everysec 几乎无感 | 同 AOF |
| 适用场景 | 备份、灾恢、缓存 | 数据安全要求高 | **生产推荐** |

---

## 六、生产推荐配置

```bash
# redis.conf 生产配置

# 开启 AOF
appendonly yes
appendfsync everysec

# 开启混合持久化
aof-use-rdb-preamble yes

# AOF 重写触发条件
auto-aof-rewrite-percentage 100
auto-aof-rewrite-min-size 64mb

# RDB 同时开启（用于备份，不影响 AOF）
save 900 1
save 300 10
save 60 10000

# 防止 fork 期间写放大（Linux）
# echo never > /sys/kernel/mm/transparent_hugepage/enabled
```

---

## 七、面试高频题

### Q1: RDB 和 AOF 的区别？

- RDB = 定时快照（二进制），恢复快但可能丢数据
- AOF = 追加写命令（文本），数据安全但文件大恢复慢
- 混合 = RDB 快照 + 增量 AOF，兼顾两者

### Q2: AOF 重写为什么不阻塞主进程？

fork 子进程做重写，主进程继续处理请求。新命令同时写入旧 AOF 和重写缓冲区。子进程完成后追加缓冲区内容，原子替换。

### Q3: fork 的性能问题？

fork 只复制页表（不复制数据），理论上很快。但大内存实例（几十 GB）页表也很大，fork 可能阻塞几十毫秒。Linux 开启 THP（Transparent Huge Pages）会加剧问题，生产建议关闭。

### Q4: 混合持久化为什么是生产推荐？

- RDB 部分 → 恢复快
- AOF 增量 → 数据不丢（最多 1 秒）
- 重写后文件小 → 磁盘和网络传输友好

### Q5: AOF 文件损坏了怎么修复？

```bash
redis-check-aof --fix appendonly.aof
```

会截断到最后一条完整命令。

### Q6: 数据恢复优先级？

同时有 RDB 和 AOF 时，**优先加载 AOF**（数据更新更完整）。只有关闭 AOF 时才加载 RDB。
