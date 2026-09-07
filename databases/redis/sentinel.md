# Redis 主从复制与哨兵（Replication & Sentinel）

**标签**: #redis #sentinel #replication #高可用 #高频
**难度**: ⭐⭐⭐⭐

---

## 一、架构演进路径

```
单机 Redis → 主从复制（读写分离）→ 哨兵（自动故障转移）→ Cluster（分片扩展）
```

| 模式 | 解决的问题 | 不足 |
|------|-----------|------|
| 单机 | 无 | 单点故障、容量有限、QPS 有限 |
| 主从 | 数据备份、读写分离 | 主挂了需要手动切换 |
| 哨兵 | **自动故障转移** | 不能水平扩展容量 |
| Cluster | 水平扩展容量 + 自动故障转移 | 复杂度高 |

---

## 二、主从复制（Replication）

### 2.1 架构

```
          ┌──────────────┐
          │  Master      │  ← 读写
          │  (主节点)     │
          └──────┬───────┘
          全量同步│增量同步
     ┌───────────┼───────────┐
     ▼           ▼           ▼
┌─────────┐ ┌─────────┐ ┌─────────┐
│ Slave 1 │ │ Slave 2 │ │ Slave 3 │  ← 只读
└─────────┘ └─────────┘ └─────────┘
```

### 2.2 同步流程

#### 全量同步（首次连接 / 断线太久）

```
  Slave                         Master
    │                             │
    │── 1. PSYNC ? -1 ──────────→│  (首次连接，不知道 runid 和 offset)
    │                             │
    │←─ 2. +FULLRESYNC runid offset ─│  (Master 返回 runid 和当前 offset)
    │                             │
    │                             │── 3. BGSAVE (fork 生成 RDB)
    │                             │── 4. 同时：新写命令存入 repl_backlog
    │                             │
    │←─ 5. 发送 RDB 文件 ────────│
    │                             │
    │←─ 6. 发送 repl_backlog ────│  (RDB 生成期间的增量命令)
    │                             │
    │── 7. 加载 RDB + 重放增量 ──→│
    │                             │
    │   全量同步完成，进入增量同步  │
```

#### 增量同步（正常运行 / 短暂断线）

```
  Slave                         Master
    │                             │
    │←─ 写命令实时传播 ──────────│  (Master 每执行一条写命令，传给所有 Slave)
    │                             │
    │   断线重连后：               │
    │── PSYNC runid offset ─────→│  (带上已同步到的 offset)
    │                             │
    │   offset 在 repl_backlog 内？│
    │     ├── yes → 增量同步      │  (只发送 offset 之后的命令)
    │     └── no  → 全量同步      │  (backlog 已被覆盖，只能全量)
    │                             │
```

### 2.3 关键概念

| 概念 | 说明 |
|------|------|
| `runid` | Master 的运行 ID（重启后变化），Slave 用它判断是不是同一个 Master |
| `offset` | 复制偏移量，Master 和 Slave 各维护一个，差值 = 延迟量 |
| `repl_backlog` | 复制积压缓冲区（环形缓冲区，默认 1MB），存最近的写命令 |

### 2.4 配置

```bash
# Slave 配置（redis.conf）
replicaof 192.168.1.100 6379    # 指定 Master 地址
replica-read-only yes            # Slave 只读

# Master 配置
repl-backlog-size 64mb           # 积压缓冲区大小（网络不稳时调大）
repl-backlog-ttl 3600            # 所有 Slave 断开后缓冲区保留时间
min-replicas-to-write 1          # 至少 1 个 Slave 在线才接受写入
min-replicas-max-lag 10          # Slave 延迟不超过 10 秒
```

### 2.5 PHP 读写分离

```php
<?php

declare(strict_types=1);

/**
 * 简单的 Redis 读写分离
 * 
 * 生产环境推荐通过 Proxy（如 Twemproxy、Codis）或哨兵客户端自动处理
 */
class RedisReadWriteSplit
{
    private Redis $master;
    /** @var Redis[] */
    private array $slaves;

    public function __construct(Redis $master, array $slaves)
    {
        $this->master = $master;
        $this->slaves = $slaves;
    }

    /**
     * 写操作 → 走 Master
     */
    public function set(string $key, string $value, int $ttl = 0): bool
    {
        if ($ttl > 0) {
            return $this->master->setex($key, $ttl, $value);
        }
        return $this->master->set($key, $value);
    }

    /**
     * 读操作 → 随机挑一个 Slave（简单负载均衡）
     */
    public function get(string $key): string|false
    {
        $slave = $this->slaves[array_rand($this->slaves)];
        return $slave->get($key);
    }

    /**
     * 强一致读 → 走 Master（刚写入就要读的场景）
     */
    public function getFromMaster(string $key): string|false
    {
        return $this->master->get($key);
    }
}

// 使用示例
$master = new Redis();
$master->connect('192.168.1.100', 6379);

$slave1 = new Redis();
$slave1->connect('192.168.1.101', 6379);

$slave2 = new Redis();
$slave2->connect('192.168.1.102', 6379);

$rw = new RedisReadWriteSplit($master, [$slave1, $slave2]);

$rw->set('user:1001', json_encode(['name' => 'Jake']));
$user = $rw->get('user:1001'); // 从 Slave 读
```

### 2.6 主从问题

| 问题 | 说明 | 解决 |
|------|------|------|
| 主从延迟 | Slave 数据落后于 Master | 写后读走 Master；或 WAIT 命令 |
| 主节点宕机 | 需要人工介入切换 | 引入哨兵 |
| 全量同步大量占用带宽 | 大实例 RDB 可能几百 MB | 控制实例大小（单实例 < 10GB）|
| 脑裂 | 网络分区后出现两个 Master | `min-replicas-to-write` 限制 |

---

## 三、哨兵（Sentinel）

### 3.1 架构

```
     ┌──────────────────────────────────────────────┐
     │            Sentinel 集群（至少 3 个）          │
     │  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
     │  │Sentinel 1│  │Sentinel 2│  │Sentinel 3│  │
     │  └─────┬────┘  └─────┬────┘  └─────┬────┘  │
     └────────┼──────────────┼──────────────┼──────┘
              │  监控         │              │
              ▼              ▼              ▼
     ┌──────────────┐
     │    Master    │  ←── 哨兵监控心跳
     └──────┬───────┘
            │ 复制
     ┌──────┼───────┐
     ▼      ▼       ▼
  Slave1  Slave2  Slave3
```

### 3.2 三大功能

| 功能 | 说明 |
|------|------|
| **监控（Monitoring）** | 每秒 PING Master/Slave，检测是否存活 |
| **通知（Notification）** | 故障转移时通过 Pub/Sub 通知客户端新 Master 地址 |
| **自动故障转移（Failover）** | Master 挂了，自动把 Slave 提升为新 Master |

### 3.3 故障发现流程

```
          Sentinel 1         Sentinel 2         Sentinel 3
              │                   │                   │
              │── PING Master ──→ │                   │
              │←─ 超时（1s 无响应）│                   │
              │                   │                   │
              │ 主观下线（SDOWN）  │                   │
              │ "我认为 Master 挂了"│                  │
              │                   │                   │
              │── 询问其他哨兵 ──→ │── PING Master ──→ │
              │                   │←─ 超时           │
              │                   │                   │
              │                   │ 也认为挂了         │
              │←─ 确认 SDOWN ────│                   │
              │                                       │
              │ 客观下线（ODOWN）：多数派（2/3）确认    │
              │ "Master 确实挂了，开始故障转移"         │
```

| 状态 | 含义 | 触发条件 |
|------|------|----------|
| 主观下线（SDOWN） | 单个哨兵认为节点不可达 | `down-after-milliseconds` 内无响应 |
| 客观下线（ODOWN） | 多数派哨兵确认节点不可达 | 达到 `quorum` 数量的哨兵同意 |

### 3.4 故障转移流程

```
1. 选举 Leader Sentinel（Raft 算法）
   → 由 Leader 执行故障转移

2. 选择新 Master（从 Slave 中挑）：
   ├── 排除：断线时间太长的 Slave
   ├── 优先级：replica-priority 最小的优先
   ├── offset：复制偏移量最大的优先（数据最新）
   └── runid：最小的（兜底，保证确定性）

3. 提升新 Master：
   → SLAVEOF NO ONE（让选中的 Slave 变成 Master）

4. 通知其他 Slave：
   → REPLICAOF new_master_ip new_master_port

5. 更新旧 Master 配置：
   → 旧 Master 恢复后自动变成新 Master 的 Slave
```

### 3.5 配置

```bash
# sentinel.conf

# 监控名为 mymaster 的主节点
sentinel monitor mymaster 192.168.1.100 6379 2
#                                              └── quorum: 至少 2 个哨兵同意才客观下线

# 主观下线判断时间
sentinel down-after-milliseconds mymaster 5000   # 5 秒无响应视为 SDOWN

# 故障转移超时
sentinel failover-timeout mymaster 60000         # 60 秒

# 同时参与同步的 Slave 数量（避免全部 Slave 同时 FULLRESYNC）
sentinel parallel-syncs mymaster 1
```

### 3.6 PHP 连接哨兵

```php
<?php

declare(strict_types=1);

/**
 * 通过 Sentinel 连接 Redis Master
 * 
 * 优点：Master 切换后客户端自动感知新地址
 */
class RedisSentinelConnection
{
    /** @var array<array{host: string, port: int}> */
    private array $sentinels;
    private string $masterName;
    private ?Redis $master = null;

    public function __construct(array $sentinels, string $masterName = 'mymaster')
    {
        $this->sentinels = $sentinels;
        $this->masterName = $masterName;
    }

    /**
     * 获取 Master 连接
     * 
     * 流程：询问 Sentinel 当前 Master 地址 → 连接 Master
     */
    public function getMaster(): Redis
    {
        if ($this->master !== null) {
            try {
                $this->master->ping();
                return $this->master;
            } catch (\RedisException $e) {
                // Master 可能已切换，重新获取
                $this->master = null;
            }
        }

        // 遍历 Sentinel 列表，找到一个能响应的
        foreach ($this->sentinels as $sentinel) {
            try {
                $s = new Redis();
                $s->connect($sentinel['host'], $sentinel['port'], 1.0);

                // 向 Sentinel 询问 Master 地址
                $masterInfo = $s->rawCommand('SENTINEL', 'get-master-addr-by-name', $this->masterName);
                // 返回: ['192.168.1.101', '6379']

                if ($masterInfo && count($masterInfo) === 2) {
                    $master = new Redis();
                    $master->connect($masterInfo[0], (int) $masterInfo[1], 2.0);
                    $this->master = $master;
                    return $master;
                }
            } catch (\RedisException $e) {
                continue; // 这个 Sentinel 不可用，试下一个
            }
        }

        throw new \RuntimeException('All sentinels are unreachable');
    }

    /**
     * 获取 Slave 连接列表（用于读）
     */
    public function getSlaves(): array
    {
        foreach ($this->sentinels as $sentinel) {
            try {
                $s = new Redis();
                $s->connect($sentinel['host'], $sentinel['port'], 1.0);

                $slavesInfo = $s->rawCommand('SENTINEL', 'slaves', $this->masterName);
                $slaves = [];

                foreach ($slavesInfo as $info) {
                    // 解析 Sentinel 返回的 Slave 信息
                    $parsed = $this->parseSlaveInfo($info);
                    if ($parsed['flags'] === 'slave' && $parsed['master-link-status'] === 'ok') {
                        $r = new Redis();
                        $r->connect($parsed['ip'], (int) $parsed['port'], 2.0);
                        $slaves[] = $r;
                    }
                }

                return $slaves;
            } catch (\RedisException $e) {
                continue;
            }
        }

        return [];
    }

    private function parseSlaveInfo(array $raw): array
    {
        $result = [];
        for ($i = 0; $i < count($raw); $i += 2) {
            $result[$raw[$i]] = $raw[$i + 1];
        }
        return $result;
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$sentinelConn = new RedisSentinelConnection([
    ['host' => '192.168.1.201', 'port' => 26379],
    ['host' => '192.168.1.202', 'port' => 26379],
    ['host' => '192.168.1.203', 'port' => 26379],
], masterName: 'mymaster');

// 获取 Master 连接（写操作）
$master = $sentinelConn->getMaster();
$master->set('order:1001', json_encode(['status' => 'paid']));

// 获取 Slaves（读操作）
$slaves = $sentinelConn->getSlaves();
if (!empty($slaves)) {
    $slave = $slaves[array_rand($slaves)];
    $order = $slave->get('order:1001');
}

// 故障转移后：
// 1. Sentinel 检测到 Master 挂了
// 2. 自动把 Slave 提升为新 Master
// 3. 客户端下次调用 getMaster() 时 ping 失败 → 重新询问 Sentinel → 得到新 Master 地址
// 4. 自动连接到新 Master，业务无感知
```

### 3.7 脑裂问题

```
网络分区前：
  [Sentinel 1, 2, 3] ←→ [Master] ←→ [Slave 1, 2]

网络分区后：
  区域 A: [Sentinel 1, 2] + [Slave 1, 2]
  区域 B: [Sentinel 3] + [Master]（被隔离）

区域 A 选出新 Master（Slave 1 提升）
区域 B 的旧 Master 还在接受写入（客户端还连着它）

网络恢复后：
  旧 Master 变成 Slave → 从新 Master 全量同步 → 分区期间的写入全部丢失！
```

**防止脑裂**：

```bash
# Master 配置
min-replicas-to-write 1      # 至少 1 个 Slave 确认才接受写入
min-replicas-max-lag 10      # Slave 延迟不超过 10 秒

# 效果：Master 被隔离后发现没有 Slave 响应 → 拒绝写入 → 客户端收到错误 → 避免脏数据
```

---

## 四、面试高频题

### Q1: 主从复制是同步还是异步？

**异步**。Master 执行完写命令后立即返回客户端，然后异步把命令传播给 Slave。所以有延迟，Slave 的数据可能落后 Master。

用 `WAIT numreplicas timeout` 可以实现同步等待（但会降低性能）。

### Q2: 全量同步什么时候触发？

- Slave 首次连接 Master
- Slave 断线时间太久，offset 已经不在 `repl_backlog` 中
- Master 重启（runid 变了）

### Q3: 哨兵为什么至少要 3 个？

- 1 个：自己挂了就没人做故障转移
- 2 个：网络分区后一边一个，都不够多数派（quorum），无法做决策
- 3 个：任何一个挂了或分区，剩下 2 个仍构成多数派

### Q4: Slave 可以有自己的 Slave 吗？

可以。叫**级联复制（Chained Replication）**：

```
Master → Slave 1 → Slave 1-1
                 → Slave 1-2
       → Slave 2
```

好处：减轻 Master 的复制压力。坏处：链路越长延迟越大。

### Q5: 哨兵如何选新 Master？

优先级排序：
1. `replica-priority` 值最小的（配置项，0 表示永不选）
2. 复制偏移量（offset）最大的（数据最新）
3. `runid` 最小的（兜底保证确定性）

### Q6: 主从 + 哨兵的容量上限？

**单机内存上限**。主从只是数据复制，不分片。每个节点都存全量数据。要突破容量限制，需要 Cluster。
