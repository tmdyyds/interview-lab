# Redis Cluster（集群）

**标签**: #redis #cluster #高频 #系统设计
**难度**: ⭐⭐⭐⭐

---

## 一、为什么需要 Cluster

| 方案 | 容量 | 写 QPS | 高可用 | 问题 |
|------|------|--------|--------|------|
| 单机 | 单机内存 | 单核极限（~10万） | ❌ | 单点故障 |
| 主从 + 哨兵 | 单机内存 | 单核（读可扩展） | ✅ | 容量无法扩展 |
| **Cluster** | **N × 单机内存** | **N × 单核** | ✅ | 复杂度最高 |

Cluster = **数据分片（Sharding）+ 去中心化 + 自动故障转移**。

---

## 二、核心架构

```
                    ┌─────────────────────────────────────────────────┐
                    │               Redis Cluster                      │
                    │                                                 │
                    │   ┌─────────┐   ┌─────────┐   ┌─────────┐     │
                    │   │ Node A  │   │ Node B  │   │ Node C  │     │
                    │   │ Master  │   │ Master  │   │ Master  │     │
                    │   │ 槽0~5460│   │槽5461~10922│ │槽10923~16383│ │
                    │   └────┬────┘   └────┬────┘   └────┬────┘     │
                    │        │             │             │           │
                    │   ┌────▼────┐   ┌────▼────┐   ┌────▼────┐     │
                    │   │ Node A' │   │ Node B' │   │ Node C' │     │
                    │   │ Slave   │   │ Slave   │   │ Slave   │     │
                    │   └─────────┘   └─────────┘   └─────────┘     │
                    └─────────────────────────────────────────────────┘
```

### 2.1 哈希槽（Hash Slot）

Redis Cluster 把数据空间分成 **16384 个哈希槽（slot）**，每个 Master 负责一部分槽。

```
key → CRC16(key) % 16384 → 槽编号 → 对应的 Master 节点
```

```php
<?php

// 计算 key 对应的 slot
function getSlot(string $key): int
{
    // 如果 key 包含 {tag}，只对 tag 部分做 hash（Hash Tag 机制）
    if (preg_match('/\{(.+?)\}/', $key, $matches)) {
        $key = $matches[1];
    }
    return crc16($key) % 16384;
}

// 示例
echo getSlot('user:1001');          // 某个 slot，如 5474 → Node B
echo getSlot('user:1002');          // 另一个 slot，如 13901 → Node C
echo getSlot('{user}:1001');        // } 对 "user" 做 hash
echo getSlot('{user}:1002');        // } 同一个 slot！（Hash Tag）

function crc16(string $data): int
{
    // CRC16-CCITT 算法（Redis 使用的变种）
    $crc = 0;
    for ($i = 0; $i < strlen($data); $i++) {
        $crc ^= ord($data[$i]) << 8;
        for ($j = 0; $j < 8; $j++) {
            if ($crc & 0x8000) {
                $crc = (($crc << 1) ^ 0x1021) & 0xFFFF;
            } else {
                $crc = ($crc << 1) & 0xFFFF;
            }
        }
    }
    return $crc;
}
```

### 2.2 为什么是 16384 个槽？

面试高频！Redis 作者 antirez 的解释：

1. **心跳包大小**：节点间 gossip 通信，每次发送自己负责哪些 slot 的位图（bitmap）。16384 bit = 2KB，能接受。如果用 65536 slot = 8KB，心跳包太大。
2. **集群规模**：Redis 官方建议最多 1000 个 Master 节点。16384 / 1000 ≈ 16 个 slot/节点，足够均匀分配。
3. **压缩效率**：16384 是 2^14，位图操作和传输都很高效。

### 2.3 Hash Tag（让相关 key 落在同一 slot）

**问题**：Cluster 下不同 key 的 CRC16 结果不同，落在不同节点。很多操作只能在同一节点执行（MGET、Lua、MULTI），跨节点会报 `CROSSSLOT` 错误。

**解决**：Hash Tag — 用 `{}` 括起来的部分做 hash。只要 `{}` 内容相同，一定落在同一个 slot。

```php
<?php

declare(strict_types=1);

namespace App\Redis;

/**
 * Redis Cluster Hash Tag 工具类
 * 
 * 提供 key 生成、slot 计算、批量操作等功能，
 * 确保相关 key 落在同一 slot，支持跨 key 原子操作。
 */
class ClusterKeyBuilder
{
    /**
     * 生成带 Hash Tag 的 key
     * 
     * 原理：Redis 计算 slot 时，如果 key 包含 {}，只对 {} 内的部分做 CRC16。
     *       所以只要 {} 里内容一样，key 就一定在同一个 slot。
     *
     * @param string $tag Hash Tag（决定 slot 的部分）
     * @param string $suffix key 后缀（区分不同数据）
     * @return string 完整的 key，如 "{user:1001}:profile"
     */
    public static function buildKey(string $tag, string $suffix): string
    {
        return '{' . $tag . '}:' . $suffix;
    }

    /**
     * 计算 key 对应的 slot（模拟 Redis 底层逻辑）
     * 
     * Redis 内部计算 slot 的逻辑：
     *   1. 找到 key 中第一个 `{`
     *   2. 找到 `{` 之后的第一个 `}`
     *   3. 如果 {} 内有内容 → 只对 {} 内的部分做 CRC16
     *   4. 如果没有 {} 或 {} 为空 → 对整个 key 做 CRC16
     *   5. CRC16 结果 % 16384 = slot 编号
     */
    public static function calculateSlot(string $key): int
    {
        // 提取 Hash Tag（和 Redis 源码逻辑一致）
        $hashInput = $key;

        $start = strpos($key, '{');
        if ($start !== false) {
            $end = strpos($key, '}', $start + 1);
            if ($end !== false && $end > $start + 1) {
                // {} 内有内容 → 只用这部分计算 hash
                $hashInput = substr($key, $start + 1, $end - $start - 1);
            }
        }

        return self::crc16($hashInput) % 16384;
    }

    /**
     * 验证多个 key 是否在同一 slot（调试用）
     * 
     * @param string[] $keys 要检查的 key 列表
     * @return array{same_slot: bool, slot: int, details: array}
     */
    public static function verifySameSlot(array $keys): array
    {
        $details = [];
        $slots = [];

        foreach ($keys as $key) {
            $slot = self::calculateSlot($key);
            $slots[] = $slot;
            $details[$key] = $slot;
        }

        $uniqueSlots = array_unique($slots);

        return [
            'same_slot' => count($uniqueSlots) === 1,
            'slot' => $uniqueSlots[0] ?? -1,
            'details' => $details,
        ];
    }

    /**
     * CRC16-CCITT（Redis 使用的变种）
     */
    private static function crc16(string $data): int
    {
        $crc = 0;
        for ($i = 0; $i < strlen($data); $i++) {
            $crc ^= ord($data[$i]) << 8;
            for ($j = 0; $j < 8; $j++) {
                if ($crc & 0x8000) {
                    $crc = (($crc << 1) ^ 0x1021) & 0xFFFF;
                } else {
                    $crc = ($crc << 1) & 0xFFFF;
                }
            }
        }
        return $crc;
    }
}

// ====================================================================
// 使用示例
// ====================================================================

use App\Redis\ClusterKeyBuilder;

// ─── 1. 生成相关 key（同一用户的数据放在同一 slot）───

$userId = 1001;
$tag = 'user:' . $userId;  // Hash Tag

$profileKey = ClusterKeyBuilder::buildKey($tag, 'profile');  // "{user:1001}:profile"
$ordersKey  = ClusterKeyBuilder::buildKey($tag, 'orders');   // "{user:1001}:orders"
$cartKey    = ClusterKeyBuilder::buildKey($tag, 'cart');      // "{user:1001}:cart"

// 这三个 key 的 slot 一定相同：
// CRC16("user:1001") % 16384 = 某个固定值（如 12950）


// ─── 2. 验证是否在同一 slot ───

$result = ClusterKeyBuilder::verifySameSlot([$profileKey, $ordersKey, $cartKey]);
var_dump($result);
// [
//   'same_slot' => true,
//   'slot' => 12950,
//   'details' => [
//     '{user:1001}:profile' => 12950,
//     '{user:1001}:orders'  => 12950,
//     '{user:1001}:cart'    => 12950,
//   ]
// ]

// 对比：没有 Hash Tag 的 key → 分散在不同 slot
$result2 = ClusterKeyBuilder::verifySameSlot([
    'user:1001:profile',   // slot A
    'user:1001:orders',    // slot B（不同！）
    'user:1001:cart',      // slot C（不同！）
]);
var_dump($result2['same_slot']); // false


// ─── 3. 实际业务操作（同一 slot 内的多 key 操作）───

$cluster = new \RedisCluster(null, ['192.168.1.101:6379']);

// ✅ MGET（同一 slot，不会报 CROSSSLOT）
$data = $cluster->mGet([$profileKey, $ordersKey, $cartKey]);

// ✅ Pipeline（同一 slot 内批量操作）
$cluster->multi(\Redis::PIPELINE);
$cluster->set($profileKey, json_encode(['name' => 'Jake', 'level' => 'vip']));
$cluster->set($ordersKey, json_encode([['id' => 1, 'amount' => 99]]));
$cluster->set($cartKey, json_encode([['sku' => 'A001', 'qty' => 2]]));
$cluster->exec();

// ✅ Lua 脚本（操作多个 key，必须同 slot）
$lua = <<<'LUA'
    local profile = redis.call("GET", KEYS[1])
    local orders = redis.call("GET", KEYS[2])
    return {profile, orders}
LUA;
$result = $cluster->eval($lua, [$profileKey, $ordersKey, 2]);

// ❌ 跨 slot 操作 → CROSSSLOT 错误
// $cluster->mGet(['user:1001:profile', 'user:1002:profile']); // 报错！

// ✅ 不同用户也想批量查？用 Hash Tag 让同类数据聚合
// 但要注意数据倾斜（见下方陷阱）


// ─── 4. 陷阱：避免数据倾斜 ───

// ⚠️ 错误：所有用户共享一个 tag → 全部挤在一个 slot
// '{users}:1001', '{users}:1002', ... '{users}:100000'
// → 10 万 key 在同一 slot → 单节点压力爆炸

// ✅ 正确：tag 按用户 ID 分散
// '{user:1001}:profile', '{user:1002}:profile', '{user:1003}:profile'
// → 不同用户在不同 slot → 均匀分布

// 原则：tag 应该是 "聚合单元"（一个用户、一个订单、一个会话），
//       而不是 "所有同类数据"。
```

---

## 三、请求路由

### 3.1 客户端请求流程

```
客户端: GET user:1001
    │
    ▼ 计算 slot = CRC16("user:1001") % 16384 = 5474
    │
    ▼ 发送到 Node A（客户端缓存的 slot 映射表）
    │
    ├── 命中：Node A 确实负责 slot 5474 → 返回数据
    │
    └── 未命中（slot 已经迁移）：
        Node A 返回 MOVED 5474 192.168.1.102:6379
            │
            ▼ 客户端更新 slot 映射表
            │
            ▼ 重定向到 Node B → 返回数据
```

### 3.2 MOVED vs ASK

| 响应 | 含义 | 客户端行为 |
|------|------|-----------|
| `MOVED slot ip:port` | slot 已**永久**迁移到目标节点 | 更新本地 slot 映射，后续直接找目标 |
| `ASK slot ip:port` | slot **正在迁移**中，这次临时找目标 | 本次重定向（先发 ASKING），不更新映射 |

### 3.3 PHP 客户端连接 Cluster

```php
<?php

declare(strict_types=1);

// ====================================================================
// 方式一：phpredis 扩展原生支持（推荐）
// ====================================================================

$cluster = new RedisCluster(
    null,  // name（集群名，null 即可）
    [      // 种子节点列表（不需要列出所有节点，客户端会自动发现）
        '192.168.1.101:6379',
        '192.168.1.102:6379',
        '192.168.1.103:6379',
    ],
    2.0,   // 连接超时
    2.0,   // 读超时
    true,  // persistent connections
    null   // auth password
);

// 使用方式和普通 Redis 完全一样
$cluster->set('user:1001', json_encode(['name' => 'Jake']));
$user = $cluster->get('user:1001');

// 客户端自动处理：
// 1. 计算 slot → 找到对应节点 → 发送命令
// 2. 收到 MOVED → 自动重定向 + 更新映射
// 3. 收到 ASK → 自动 ASKING + 重定向

// Pipeline（同一 slot 内的 key 才有效）
$cluster->multi(Redis::PIPELINE);
$cluster->set('{order:100}:status', 'paid');
$cluster->set('{order:100}:amount', '99.9');
$cluster->exec();

// MGET（只能操作同一 slot 的 key）
// ❌ 不行：$cluster->mGet(['user:1', 'user:2']); // 可能跨 slot
// ✅ 可以：$cluster->mGet(['{user}:1', '{user}:2']); // Hash Tag 保证同 slot


// ====================================================================
// 方式二：Hyperf Redis Cluster 配置
// ====================================================================

// config/autoload/redis.php
return [
    'default' => [
        'host' => '',
        'port' => 0,
        'cluster' => [
            'enable' => true,
            'name' => null,
            'seeds' => [
                '192.168.1.101:6379',
                '192.168.1.102:6379',
                '192.168.1.103:6379',
            ],
        ],
        'options' => [
            Redis::OPT_READ_TIMEOUT => 2.0,
        ],
    ],
];

// 业务代码无感知（和单机写法一样）
// $this->redis->set('key', 'value');
```

---

## 四、槽迁移（Resharding）

扩容/缩容时需要把一部分 slot 从一个节点迁移到另一个节点。

### 4.1 迁移流程

```
源节点 A（slot 5000）                     目标节点 B
    │                                        │
    │←── CLUSTER SETSLOT 5000 MIGRATING B ──│  A 标记：slot 5000 正在迁出
    │                                        │
    │── CLUSTER SETSLOT 5000 IMPORTING A ──→│  B 标记：slot 5000 正在迁入
    │                                        │
    │ 逐个 key 迁移：                         │
    │── CLUSTER GETKEYSINSLOT 5000 100 ────→│  获取 slot 5000 中的 key
    │── MIGRATE B port key 0 timeout ──────→│  原子迁移 key 到 B
    │   ...（重复直到 slot 为空）              │
    │                                        │
    │── CLUSTER SETSLOT 5000 NODE B ───────→│  确认迁移完成
    │                                        │
    │   广播给所有节点更新 slot 映射            │
```

### 4.2 迁移期间请求处理

```
客户端 GET key（key 在 slot 5000）→ 发到 A
    │
    A 检查：slot 5000 是 MIGRATING 状态
    ├── key 还在 A → 正常返回
    └── key 已迁到 B → 返回 ASK 5000 B的地址
                          │
                          ▼
                 客户端对 B 发 ASKING + GET key → B 返回数据
```

---

## 五、故障转移

Cluster 内置故障转移，**不需要哨兵**。

### 5.1 故障检测（Gossip 协议）

```
每个节点每秒随机 PING 几个其他节点：
  ├── 响应了 → 标记为 PFAIL（可能故障）的取消
  └── 超时了 → 标记为 PFAIL（Possible Failure，主观下线）

当节点 A 发现节点 X 是 PFAIL 时：
  → Gossip 传播给其他节点
  → 如果超过半数 Master 都标记 X 为 PFAIL
  → X 被标记为 FAIL（客观下线）
  → X 的 Slave 开始故障转移
```

### 5.2 Slave 选举 + 提升

```
X 被标记为 FAIL：
    │
    ▼ X 的 Slave 们发起选举
    │
    ├── Slave 1（offset 大 → 数据最新 → 优先级高）
    ├── Slave 2
    └── Slave 3
    │
    ▼ 其他 Master 投票（每个 Master 只能投一票）
    │
    ▼ 获得多数票的 Slave 晋升为新 Master
    │
    ▼ 接管 X 的 slot → 广播 CLUSTER SETSLOT
```

---

## 六、Cluster 限制

| 限制 | 原因 | 解决方案 |
|------|------|----------|
| 多 key 操作必须在同一 slot | 不同 slot 在不同节点 | Hash Tag `{tag}` |
| 不支持 SELECT 多数据库 | Cluster 只用 db0 | 忽略，用前缀区分 |
| Lua 脚本的 key 必须同 slot | 脚本在单节点执行 | Hash Tag |
| 事务（MULTI）必须同 slot | 同上 | Hash Tag |
| 大 key 迁移慢 | MIGRATE 是阻塞的 | 控制 key 大小 |
| Pub/Sub 广播所有节点 | 设计如此 | 消息量大时考虑 MQ |

---

## 七、生产部署建议

### 7.1 集群规模

```bash
# 最小生产配置：3 Master + 3 Slave = 6 节点
# 每个 Master 有一个 Slave 做故障转移备份

redis-cli --cluster create \
    192.168.1.101:6379 \
    192.168.1.102:6379 \
    192.168.1.103:6379 \
    192.168.1.104:6379 \
    192.168.1.105:6379 \
    192.168.1.106:6379 \
    --cluster-replicas 1   # 每个 Master 分配 1 个 Slave
```

### 7.2 扩容

```bash
# 添加新节点
redis-cli --cluster add-node 192.168.1.107:6379 192.168.1.101:6379

# 重新分配 slot（从现有节点迁一部分 slot 到新节点）
redis-cli --cluster reshard 192.168.1.101:6379
```

### 7.3 监控

```php
<?php

$cluster = new RedisCluster(null, ['192.168.1.101:6379']);

// 查看集群状态
$info = $cluster->info('cluster'); // cluster_state:ok / cluster_state:fail
echo $info;

// 查看 slot 分布
// CLUSTER SLOTS
$slots = $cluster->rawCommand('192.168.1.101:6379', 'CLUSTER', 'SLOTS');
print_r($slots);

// 查看节点列表
// CLUSTER NODES
$nodes = $cluster->rawCommand('192.168.1.101:6379', 'CLUSTER', 'NODES');
echo $nodes;

// 常用运维命令
// CLUSTER INFO          — 集群总体状态
// CLUSTER NODES         — 所有节点和 slot 分布
// CLUSTER SLOTS         — slot 分布详情
// CLUSTER KEYSLOT key   — 查询 key 在哪个 slot
// CLUSTER COUNTKEYSINSLOT slot — 某个 slot 有多少 key
```

---

## 八、Cluster vs Sentinel 选型

| 维度 | Sentinel | Cluster |
|------|----------|---------|
| 数据容量 | 单机内存 | N × 单机内存（水平扩展） |
| 写 QPS | 单 Master 上限 | N × 单 Master（线性扩展） |
| 架构复杂度 | 低 | 高 |
| 客户端复杂度 | 需要 Sentinel 协议 | 需要 Cluster 协议 |
| 多 key 操作 | 无限制 | 必须同 slot |
| 适用数据量 | < 10~20 GB | 10 GB ~ TB 级 |
| 适用 QPS | < 10 万 | 10 万 ~ 百万 |

**选型建议**：
- 数据量 < 10GB、QPS < 10万 → 主从 + 哨兵（简单够用）
- 数据量 > 10GB 或 QPS > 10万 → Cluster
- 临时缓存（丢了能重建）→ Cluster（扩展方便）
- 持久化数据（不能丢）→ 根据容量选型 + 定期备份

---

## 九、面试高频题

### Q1: Cluster 为什么用 16384 个槽？

1. 心跳包大小：16384 bit = 2KB，64K slot 则 8KB，太大
2. 集群规模：1000 节点上限，16384 足够均匀分配
3. 位图操作效率：2^14 对齐方便

### Q2: Cluster 如何保证数据路由正确？

客户端缓存 slot → node 映射表。每次请求先本地计算 slot 再发到对应节点。如果节点返回 MOVED/ASK 则更新/重定向。

### Q3: 多 key 操作（MGET、Pipeline、Lua）怎么办？

使用 Hash Tag。把相关 key 的公共部分用 `{}` 括起来，保证落在同一 slot。

### Q4: 集群扩容会影响线上服务吗？

迁移是渐进式的（逐 key 迁移），对客户端透明。迁移中的 key 通过 ASK 重定向处理。大 key 迁移时可能有毫秒级阻塞。

### Q5: Cluster 怎么做故障转移？和哨兵有什么区别？

Cluster 内置故障检测（Gossip）和选举（Slave 选举得到多数 Master 投票后晋升）。不需要额外部署哨兵。本质原理类似，但集成在 Cluster 协议内部。

### Q6: 客户端连接哪个节点？

连**任意一个**种子节点即可。客户端初始化时会通过 `CLUSTER SLOTS` 获取完整的 slot 分布表，之后直连每个 Master。

### Q7: 数据迁移中节点挂了怎么办？

迁移是原子的（MIGRATE 命令保证单 key 原子性）。如果源节点挂了，slot 仍标记在源节点，等 Slave 提升后继续迁移。如果目标节点挂了，slot 还在源节点，不影响。
