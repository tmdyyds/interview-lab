# 缓存策略：击穿 / 穿透 / 雪崩 / 一致性

**标签**: #redis #cache #高频 #系统设计
**难度**: ⭐⭐⭐⭐
**适用场景**: 电商商品详情、用户信息、库存、搜索热词等高并发读场景

---

## 一、核心概念总览

| 问题 | 本质 | 后果 | 核心解法 |
|------|------|------|----------|
| 缓存穿透 | 查询**不存在的数据**，缓存和 DB 都 miss | DB 被大量无效请求打爆 | 布隆过滤器 + 空值缓存 |
| 缓存击穿 | **热点 key 过期**瞬间，大量并发直接打 DB | 单个热点 key 引发 DB 压力尖峰 | 互斥锁 + 逻辑过期 |
| 缓存雪崩 | **大量 key 同时过期** 或 Redis 整体不可用 | DB 瞬间承受全部流量 | 过期打散 + 多级缓存 + 熔断 |
| 缓存一致性 | 缓存与 DB 数据**不一致** | 用户看到脏数据 | 延迟双删 + Canal 订阅 binlog |

---

## 二、缓存穿透（Cache Penetration）

### 2.1 问题描述

请求的数据在**缓存和数据库中都不存在**。攻击者可以构造大量不存在的 ID（如负数、超大值），绕过缓存直接打 DB。

### 2.2 解决方案

#### 方案一：布隆过滤器（Bloom Filter）前置拦截

在缓存层之前增加布隆过滤器，将所有合法 ID 预加载。请求先过布隆过滤器，不存在则直接拒绝。

**核心特点**：
- 只有假阳性（说存在但实际不存在），没有假阴性（说不存在就一定不存在）
- **不支持删除**：bit 被设为 1 后不能安全设回 0（多个元素可能共享同一个 bit 位）

**生产级完整方案（实时添加 + 定时全量重建）**：
- 新增数据时 → 实时 `add` 到当前布隆过滤器（保证立即可查）
- 删除/下架时 → 不处理（布隆过滤器天然删不了）
- 定时任务 → **全量重建**：从 DB 重新加载所有有效 ID 生成全新的过滤器，替换旧的脏过滤器，使误判率回归理论值

**部署形态选择**：

| 形态 | QPS | 适用场景 |
|------|-----|----------|
| Redis bitmap（下面的实现） | 8~10 万 | 多服务共享同一个过滤器，分布式场景 |
| RedisBloom 模块（`BF.ADD`/`BF.EXISTS`） | ~15 万 | 同上，但更简单，推荐 |
| 本地进程内存（Swoole/Hyperf 常驻进程） | **百万级** | 单服务独占，极致性能 |

##### Redis bitmap 实现

```php
<?php

declare(strict_types=1);

namespace App\Cache;

use Redis;

/**
 * Redis 实现的布隆过滤器
 * 
 * 生产环境推荐使用 RedisBloom 模块（BF.ADD / BF.EXISTS），
 * 此处为纯 Redis bitmap 实现，便于理解原理。
 */
class BloomFilter
{
    private Redis $redis;
    private string $key;
    private int $bitSize;
    private int $hashCount;

    public function __construct(
        Redis $redis,
        string $key = 'bloom:product_ids',
        int $expectedInsertions = 1000000,
        float $falsePositiveRate = 0.01
    ) {
        $this->redis = $redis;
        $this->key = $key;

        // ====================================================================
        // 布隆过滤器最优参数计算
        //
        // 【本质】布隆过滤器 = bit 数组（全 0 初始）+ k 个哈希函数
        //   - 添加元素：k 个哈希函数各算一个位置，把那些 bit 设为 1
        //   - 查询元素：算同样 k 个位置，全是 1 → "可能存在"；有 0 → "一定不存在"
        //
        // 【核心矛盾】
        //   - bit 数组太小 → 很快被 1 填满 → 误判率飙升
        //   - bit 数组太大 → 浪费内存
        //   - 哈希函数太少 → 区分能力弱 → 误判多
        //   - 哈希函数太多 → 每个元素设太多 bit 为 1 → 数组很快填满 → 误判反而更多
        //
        // 所以存在一个最优平衡点，以下两个公式就是数学最优解。
        // ====================================================================

        // 【公式一】最优 bit 数组大小: m = -(n × ln(p)) / (ln2)²
        //
        //   n = 预计插入的元素数量
        //   p = 可接受的误判率（False Positive Rate）
        //   m = 需要的 bit 数
        //
        //   直觉：
        //     - n 越大（元素越多），需要的 bit 越多 → 正比关系
        //     - p 越小（精度要求越高），需要的 bit 越多
        //       → ln(p) 为负数且绝对值随 p 减小而增大，前面负号变正
        //
        //   代入数字（n=100万, p=0.01 即 1%）：
        //     m = -(1000000 × ln(0.01)) / (ln2)²
        //       = -(1000000 × (-4.605)) / (0.693)²
        //       = 4605000 / 0.480
        //       ≈ 9,585,058 bit ≈ 1.14 MB
        //
        //   结论：100 万元素 + 1% 误判率，只需约 1.14 MB 内存。非常划算。
        $this->bitSize = (int) ceil(
            -($expectedInsertions * log($falsePositiveRate)) / (log(2) ** 2)
        );

        // 【公式二】最优哈希函数个数: k = (m / n) × ln2
        //
        //   m/n = 平均每个元素能分到多少个 bit
        //   乘以 ln2 ≈ 0.693，意思是用约 69% 的"可用 bit 数"作为哈希函数个数
        //
        //   为什么是 ln2？
        //     数学证明：在这个比例下，bit 数组中 0 和 1 的分布接近 50:50，
        //     此时误判率达到理论最低值。
        //
        //   代入数字（m≈9585058, n=1000000）：
        //     k = (9585058 / 1000000) × 0.693
        //       = 9.585 × 0.693
        //       ≈ 6.64 → 向上取整为 7
        //
        //   结论：用 7 个哈希函数最优。
        $this->hashCount = (int) ceil(
            ($this->bitSize / $expectedInsertions) * log(2)
        );
    }

    /**
     * 添加元素到布隆过滤器
     */
    public function add(string $value): void
    {
        $offsets = $this->getOffsets($value);
        // Pipeline 批量设置
        $pipe = $this->redis->pipeline();
        foreach ($offsets as $offset) {
            $pipe->setBit($this->key, $offset, 1);
        }
        $pipe->exec();
    }

    /**
     * 判断元素是否可能存在
     * 
     * @return bool true=可能存在(有误判) false=一定不存在
     */
    public function mightContain(string $value): bool
    {
        $offsets = $this->getOffsets($value);
        // Pipeline 批量查询
        $pipe = $this->redis->pipeline();
        foreach ($offsets as $offset) {
            $pipe->getBit($this->key, $offset);
        }
        $results = $pipe->exec();

        // $results 是每个 getBit 的返回值数组，例如 [1, 1, 1, 0, 1, 1, 1]
        //
        // in_array(0, $results, true)：
        //   → 检查结果中是否有任何一个 bit 为 0（strict 模式，类型也要匹配）
        //   → 如果有 0，说明该元素**一定不存在**
        //
        // 前面加 !（取反）：
        //   → 没有 0（全是 1）→ true → "可能存在"
        //   → 有 0             → false → "一定不存在"
        return !in_array(0, $results, true);
    }

    /**
     * 使用多个 hash seed 计算 bit 偏移量
     */
    private function getOffsets(string $value): array
    {
        $offsets = [];
        for ($i = 0; $i < $this->hashCount; $i++) {
            $hash = crc32($value . ':' . $i);
            $offsets[] = abs($hash) % $this->bitSize;
        }
        return $offsets;
    }
}

// ====================================================================
// 使用示例
// ====================================================================

// 1. 初始化 Redis 连接
$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

// 2. 创建布隆过滤器实例
//    参数：Redis 实例, key 名, 预计元素数(100万), 误判率(1%)
$bloom = new BloomFilter(
    redis: $redis,
    key: 'bloom:product_ids',
    expectedInsertions: 1000000,
    falsePositiveRate: 0.01
);

// 3. 预热：系统启动时把所有合法商品 ID 加入布隆过滤器
//    通常在部署脚本或定时任务中执行
$productIds = [1, 2, 3, 100, 2024, 999999]; // 实际从 DB 批量查询
foreach ($productIds as $id) {
    $bloom->add((string) $id);
}

// 4. 请求进来时先判断
$requestId = 12345;
if (!$bloom->mightContain((string) $requestId)) {
    // 一定不存在 → 直接返回 404，不查 DB
    echo "商品不存在（布隆过滤器拦截）\n";
} else {
    // 可能存在 → 继续查缓存/DB
    echo "可能存在，继续查询...\n";
}

// 5. 攻击场景：大量不存在的 ID
$fakeIds = [-1, 0, 88888888, 99999999];
foreach ($fakeIds as $fakeId) {
    $exists = $bloom->mightContain((string) $fakeId);
    // 绝大部分返回 false，直接被拦截，不会打到 DB
    echo "ID={$fakeId} → " . ($exists ? '可能存在(误判)' : '一定不存在(拦截)') . "\n";
}

// 6. 新增商品时，同步更新布隆过滤器
$newProductId = 1000001;
// ... 插入 DB 成功后 ...
$bloom->add((string) $newProductId);
```

##### 定时全量重建（解决"不支持删除"问题）

布隆过滤器不能删除元素，时间长了已下架/删除的 ID 仍留在里面，误判率逐渐升高。
解决方案：**定时从 DB 全量重建一个干净的布隆过滤器，原子替换旧的**。

```php
<?php

declare(strict_types=1);

/**
 * 布隆过滤器全量重建定时任务
 *
 * 建议频率：
 *   - 数据变动少（如商品库） → 每天凌晨一次
 *   - 数据变动频繁（如优惠券） → 每 4~6 小时一次
 *
 * 流程：
 *   1. 创建全新的空布隆过滤器（新 key）
 *   2. 分批从 DB 加载所有有效 ID
 *   3. RENAME 原子切换，无缝衔接
 */
class BloomFilterRebuildJob
{
    private const BATCH_SIZE = 10000; // 每批从 DB 查 1 万条

    public function __construct(
        private Redis $redis,
        private PDO $db
    ) {
    }

    public function handle(): void
    {
        $newKey = 'bloom:product_ids:rebuilding';
        $activeKey = 'bloom:product_ids';

        // 1. 确保新 key 是干净的
        $this->redis->del($newKey);

        // 2. 创建新的布隆过滤器实例（指向新 key）
        $newBloom = new BloomFilter(
            redis: $this->redis,
            key: $newKey,
            expectedInsertions: 2000000, // 预留 2 倍余量，避免实际数据超预期导致误判升高
            falsePositiveRate: 0.01
        );

        // 3. 分批从 DB 加载所有有效商品 ID（游标分页避免内存爆）
        $lastId = 0;
        $total = 0;

        while (true) {
            $stmt = $this->db->prepare(
                'SELECT id FROM products WHERE id > :lastId AND status = 1 ORDER BY id LIMIT :batch'
            );
            $stmt->bindValue(':lastId', $lastId, PDO::PARAM_INT);
            $stmt->bindValue(':batch', self::BATCH_SIZE, PDO::PARAM_INT);
            $stmt->execute();

            $ids = $stmt->fetchAll(PDO::FETCH_COLUMN);
            if (empty($ids)) {
                break;
            }

            // 批量加入新过滤器
            foreach ($ids as $id) {
                $newBloom->add((string) $id);
            }

            $lastId = (int) end($ids);
            $total += count($ids);
        }

        // 4. 原子切换：RENAME 是 Redis 原子操作，切换瞬间无缝衔接
        //    旧 key 被覆盖，正在使用旧过滤器的请求在下一次查询自动使用新数据
        $this->redis->rename($newKey, $activeKey);

        echo "[BloomRebuild] Done: {$total} products loaded.\n";
    }
}
```

##### 本地进程内存实现（百万 QPS 极致性能）

适合 Swoole / Hyperf 常驻进程。布隆过滤器直接放进程内存，**零网络开销**。

```php
<?php

declare(strict_types=1);

/**
 * 本地内存布隆过滤器
 * 
 * 特点：
 *   - 查询零网络 RTT，纯 CPU 计算，纳秒级响应
 *   - 单进程可承载百万 QPS
 *   - 缺点：每个 Worker 进程各存一份（内存翻倍），需定时同步
 *
 * 使用场景：
 *   - Hyperf 服务启动时在 onWorkerStart 中执行 rebuild
 *   - 定时器（Timer）周期性重建（如每小时）
 *   - 新增商品时通过进程间通信或广播通知各 Worker add
 */
class LocalBloomFilter
{
    private string $bitArray = '';  // 用 PHP 字符串模拟 bit 数组
    private int $bitSize = 0;
    private int $hashCount = 0;

    /**
     * 全量重建（从 DB 加载）
     *
     * 典型调用时机：
     *   - Worker 启动（onWorkerStart）
     *   - 定时器回调（每 1~6 小时）
     */
    public function rebuild(PDO $db): void
    {
        $expectedInsertions = 2000000;
        $falsePositiveRate = 0.01;

        $this->bitSize = (int) ceil(
            -($expectedInsertions * log($falsePositiveRate)) / (log(2) ** 2)
        );
        $this->hashCount = (int) ceil(
            ($this->bitSize / $expectedInsertions) * log(2)
        );

        // 分配全新的 bit 数组（全 0）
        // 100 万元素 + 1% 误判 ≈ 1.14 MB 内存，非常轻量
        $byteSize = (int) ceil($this->bitSize / 8);
        $newBitArray = str_repeat("\0", $byteSize);

        // 分批从 DB 加载
        $lastId = 0;
        while (true) {
            $stmt = $db->prepare(
                'SELECT id FROM products WHERE id > ? AND status = 1 ORDER BY id LIMIT 10000'
            );
            $stmt->execute([$lastId]);
            $ids = $stmt->fetchAll(PDO::FETCH_COLUMN);
            if (empty($ids)) break;

            foreach ($ids as $id) {
                $this->addToBitArray($newBitArray, (string) $id);
            }
            $lastId = (int) end($ids);
        }

        // 原子替换引用（PHP 赋值是 COW，切换瞬间对读无影响）
        $this->bitArray = $newBitArray;
    }

    /**
     * 实时添加（新增商品时调用）
     */
    public function add(string $value): void
    {
        $this->addToBitArray($this->bitArray, $value);
    }

    /**
     * 查询 — 纯内存操作，零网络，纳秒级
     */
    public function mightContain(string $value): bool
    {
        for ($i = 0; $i < $this->hashCount; $i++) {
            $offset = abs(crc32($value . ':' . $i)) % $this->bitSize;
            $byteIndex = intdiv($offset, 8);
            $bitIndex = $offset % 8;

            // 检查指定 bit 是否为 1
            if (!(ord($this->bitArray[$byteIndex]) & (1 << $bitIndex))) {
                return false; // 有一个 bit 为 0 → 一定不存在
            }
        }
        return true; // 全部为 1 → 可能存在
    }

    private function addToBitArray(string &$bitArray, string $value): void
    {
        for ($i = 0; $i < $this->hashCount; $i++) {
            $offset = abs(crc32($value . ':' . $i)) % $this->bitSize;
            $byteIndex = intdiv($offset, 8);
            $bitIndex = $offset % 8;

            // 将指定 bit 设为 1（位或操作）
            $bitArray[$byteIndex] = chr(ord($bitArray[$byteIndex]) | (1 << $bitIndex));
        }
    }
}

// ====================================================================
// Hyperf 中的使用示例
// ====================================================================

// 在 config/autoload/processes.php 或自定义 Process 中：
//
// use Hyperf\Process\Annotation\Process;
// #[Process]
// class BloomRebuildProcess {
//     public function handle(): void {
//         $bloom = make(LocalBloomFilter::class);
//         while (true) {
//             $bloom->rebuild(make(PDO::class));
//             sleep(3600); // 每小时重建
//         }
//     }
// }
//
// 在 Controller / Service 中：
//
// $bloom = $this->container->get(LocalBloomFilter::class);
// if (!$bloom->mightContain((string) $productId)) {
//     return null; // 一定不存在，直接返回
// }
// // 继续查缓存/DB...
```

##### 方案对比总结

| 实现方式 | QPS | 网络开销 | 支持分布式 | 适用场景 |
|----------|-----|----------|-----------|----------|
| Redis bitmap（手写） | 8~10 万 | 1 次 RTT/查询 | ✅ | 多服务共享、中小流量 |
| RedisBloom 模块 | ~15 万 | 1 次 RTT/查询 | ✅ | 同上，更推荐 |
| 本地进程内存 | **百万级** | 零 | ❌（每进程独立） | 单服务极致性能 |
| 本地 + Redis 广播同步 | 百万级 | 仅同步时 | ✅ | 大厂终极方案 |

##### 本地 + Redis Pub/Sub 广播同步（大厂终极方案）

解决的核心矛盾：本地内存查询快（百万 QPS），但多个服务实例/Worker 之间数据不同步。

**架构**：查询走本地内存（快）+ 写入走 Redis 广播（同步）+ 定时全量重建（兜底）

```
                新增商品 ID=2000001
                       │
                       ▼
              ┌─────────────────┐
              │  商品服务（写入 DB） │
              └────────┬────────┘
                       │ publish 广播
                       ▼
              ┌─────────────────┐
              │  Redis Pub/Sub   │  ← 广播频道：bloom:product:sync
              └────────┬────────┘
                       │ 广播到所有订阅者
          ┌────────────┼────────────┐
          ▼            ▼            ▼
    ┌──────────┐ ┌──────────┐ ┌──────────┐
    │ 服务器 A  │ │ 服务器 B  │ │ 服务器 C  │
    │ 4 Worker │ │ 4 Worker │ │ 4 Worker │
    │ 各自 add │ │ 各自 add │ │ 各自 add │
    └──────────┘ └──────────┘ └──────────┘
```

```php
<?php

declare(strict_types=1);

namespace App\Bloom;

use Redis;
use PDO;
use Psr\Log\LoggerInterface;

/**
 * 本地布隆 + Redis Pub/Sub 广播同步
 *
 * 架构：
 *   - 查询：纯本地内存，零网络，百万 QPS
 *   - 新增同步：通过 Redis Pub/Sub 广播，所有节点秒级收到
 *   - 全量重建：定时器周期性从 DB 重建，保证数据干净
 *
 * 适用：Swoole / Hyperf 常驻进程 + 多服务实例（水平扩展）+ 高并发读 + 低频写
 */
class DistributedLocalBloom
{
    private const CHANNEL = 'bloom:product:sync'; // Pub/Sub 频道
    private const REBUILD_INTERVAL = 3600;        // 全量重建间隔（秒）

    private LocalBloomFilter $bloom;

    // ─────────────────────────────────────────────────────────────────────
    // 为什么需要两个 Redis 连接？
    //
    // Redis 的 subscribe 命令一旦调用，该连接进入"订阅模式"：
    //   - 会一直阻塞等待消息（永远不返回）
    //   - 只能执行 SUBSCRIBE / UNSUBSCRIBE / PSUBSCRIBE / PUNSUBSCRIBE
    //   - 不能再执行 GET、SET、DEL、PUBLISH 等普通命令
    //
    // 类比：打电话说"我挂着等你消息"，电话就一直占线，不能用来打别的电话
    //
    // 所以必须两个连接各司其职：
    //   pubRedis（连接1）：正常业务用，可以 publish / get / set
    //   subRedis（连接2）：专门订阅，阻塞等消息，不干别的
    // ─────────────────────────────────────────────────────────────────────
    private Redis $pubRedis;   // 发布用（共享连接，可正常执行其他命令）
    private Redis $subRedis;   // 订阅用（独占连接，进入阻塞模式）
    private PDO $db;
    private LoggerInterface $logger;

    public function __construct(
        LocalBloomFilter $bloom,
        Redis $pubRedis,
        Redis $subRedis,
        PDO $db,
        LoggerInterface $logger
    ) {
        $this->bloom = $bloom;
        $this->pubRedis = $pubRedis;
        $this->subRedis = $subRedis;
        $this->db = $db;
        $this->logger = $logger;
    }

    // ─────────────────────────────────────────────
    //  发布端（业务代码调用）
    // ─────────────────────────────────────────────

    /**
     * 新增商品时调用：本地 add + 广播给其他节点
     */
    public function addAndBroadcast(string $id): void
    {
        // 1. 本进程立即 add（本地即刻生效）
        $this->bloom->add($id);

        // 2. 通过 pubRedis 广播给其他所有节点
        //    注意：用的是 pubRedis（普通连接），不是 subRedis（订阅连接）
        $message = json_encode([
            'action' => 'add',
            'id' => $id,
            'timestamp' => time(),
        ]);
        $this->pubRedis->publish(self::CHANNEL, $message);
    }

    /**
     * 查询：纯本地内存，零网络开销
     */
    public function mightContain(string $id): bool
    {
        return $this->bloom->mightContain($id);
    }

    // ─────────────────────────────────────────────
    //  订阅端（在独立进程中运行）
    // ─────────────────────────────────────────────

    /**
     * 启动订阅监听
     *
     * ⚠️ 必须在独立进程中运行！
     *    因为 subscribe 会永久阻塞，如果放在 HTTP Worker 里，
     *    整个 Worker 会卡死，无法处理请求。
     *
     * Hyperf 中的正确做法：放在自定义 Process 中（见下方集成示例）
     */
    public function startSubscriber(): void
    {
        $this->logger->info('[BloomSync] Subscriber started, channel: ' . self::CHANNEL);

        // ─────────────────────────────────────────────────────────────────
        // subscribe() 详解：
        //
        // $this->subRedis->subscribe(['bloom:product:sync'], function($redis, $channel, $msg) {
        //     //                                                  │       │         │
        //     //                                                  │       │         └── 消息内容(字符串)
        //     //                                                  │       └── 来自哪个频道
        //     //                                                  └── Redis 连接实例
        //     $this->handleMessage($msg);
        // });
        //
        // 执行流程：
        //   1. 调用 subscribe → 连接进入订阅模式
        //   2. 阻塞等待...（代码停在这里，不往下走）
        //   3. 收到一条消息 → 执行回调函数
        //   4. 回调执行完 → 继续阻塞等待下一条消息
        //   5. 无限循环，直到连接断开
        //   6. subscribe 后面的代码永远不会执行
        //
        // 示意：
        //   subscribe(...);        ← 永久阻塞在这里
        //   echo "到不了这里";     ← ❌ 永远不会执行
        // ─────────────────────────────────────────────────────────────────
        $this->subRedis->subscribe([self::CHANNEL], function ($redis, $channel, $message) {
            $this->handleMessage($message);
        });
    }

    /**
     * 处理收到的广播消息
     */
    private function handleMessage(string $raw): void
    {
        $data = json_decode($raw, true);
        if (!$data || !isset($data['action'])) {
            return;
        }

        switch ($data['action']) {
            case 'add':
                // 其他节点新增了 ID，本地 add
                $this->bloom->add($data['id']);
                $this->logger->debug('[BloomSync] Added: ' . $data['id']);
                break;

            case 'rebuild':
                // 收到重建指令，本地全量重建
                $this->bloom->rebuild($this->db);
                $this->logger->info('[BloomSync] Rebuild triggered by broadcast');
                break;
        }
    }

    // ─────────────────────────────────────────────
    //  定时全量重建（兜底机制）
    // ─────────────────────────────────────────────

    /**
     * 定时重建 + 广播通知其他节点也重建
     *
     * 只需要一个节点执行（用分布式锁保证），其他节点通过广播收到 rebuild 指令后自行重建
     */
    public function scheduledRebuild(): void
    {
        // 分布式锁，确保只有一个节点触发广播
        $lockKey = 'bloom:rebuild:lock';
        $locked = $this->pubRedis->set($lockKey, '1', ['NX', 'EX' => 60]);
        if (!$locked) {
            return; // 其他节点已经在执行
        }

        // 本节点先重建
        $this->bloom->rebuild($this->db);

        // 广播通知其他节点
        $message = json_encode([
            'action' => 'rebuild',
            'timestamp' => time(),
        ]);
        $this->pubRedis->publish(self::CHANNEL, $message);

        $this->logger->info('[BloomSync] Scheduled rebuild completed and broadcasted');
    }
}
```

**Hyperf 集成示例**：

```php
<?php

// ====================================================================
// 1. 自定义进程：监听 Pub/Sub（每个服务实例启动一个）
//    独立进程不影响 HTTP Worker 处理请求
// ====================================================================

use Hyperf\Process\AbstractProcess;
use Hyperf\Process\Annotation\Process;

#[Process(name: 'bloom-subscriber')]
class BloomSubscriberProcess extends AbstractProcess
{
    public function handle(): void
    {
        $bloom = $this->container->get(DistributedLocalBloom::class);

        // 冷启动：先做一次全量重建
        $bloom->scheduledRebuild();

        // 然后阻塞监听广播（永不返回）
        $bloom->startSubscriber();
    }
}

// ====================================================================
// 2. 定时任务：每小时全量重建
// ====================================================================

use Hyperf\Crontab\Annotation\Crontab;

#[Crontab(rule: '0 * * * *', memo: 'bloom-rebuild')]
class BloomRebuildCrontab
{
    public function execute(): void
    {
        $bloom = make(DistributedLocalBloom::class);
        $bloom->scheduledRebuild();
    }
}

// ====================================================================
// 3. 业务代码中使用
// ====================================================================

class ProductService
{
    public function __construct(
        private DistributedLocalBloom $bloom,
        private ProductRepository $repo
    ) {
    }

    public function getProduct(int $id): ?array
    {
        // 布隆过滤器拦截 — 纯内存查询，纳秒级
        if (!$this->bloom->mightContain((string) $id)) {
            return null; // 一定不存在，不查缓存也不查 DB
        }
        // 继续走缓存/DB 逻辑...
    }

    public function createProduct(array $data): int
    {
        $id = $this->repo->create($data);
        // 新增后广播给所有节点
        $this->bloom->addAndBroadcast((string) $id);
        return $id;
    }
}
```

**为什么 Pub/Sub 丢消息也没关系？**

Redis Pub/Sub 是"发后即忘"模式——离线节点或网络抖动时会丢消息。但这里不是问题：
- 丢了一个 `add` 消息 → 某节点布隆过滤器暂时不包含该 ID → 请求穿过布隆到 DB → 空值缓存兜底
- 定时全量重建（每小时）→ 无论中间丢了多少消息，重建后数据完全干净
- 所以 Pub/Sub 的"不可靠"恰好被重建兜底覆盖了，不需要上更重的 MQ

#### 方案二：空值缓存（Null Object Cache）

查询 DB 后若不存在，缓存一个空标记，短 TTL（防止占用太多内存）。

```php
<?php

declare(strict_types=1);

namespace App\Cache;

use Redis;
use App\Repository\ProductRepository;

class ProductCache
{
    private const PREFIX = 'product:';
    private const NULL_PLACEHOLDER = '__NULL__';
    private const DEFAULT_TTL = 3600;        // 正常数据 1 小时
    private const NULL_TTL = 300;            // 空值 5 分钟
    private const LOCK_TTL = 5;              // 互斥锁超时

    private Redis $redis;
    private ProductRepository $repository;
    private BloomFilter $bloomFilter;

    public function __construct(
        Redis $redis,
        ProductRepository $repository,
        BloomFilter $bloomFilter
    ) {
        $this->redis = $redis;
        $this->repository = $repository;
        $this->bloomFilter = $bloomFilter;
    }

    /**
     * 获取商品信息 - 防穿透完整方案
     * 
     * 流程：布隆过滤器 → 缓存 → 互斥锁 → DB → 回填缓存
     */
    public function getProduct(int $productId): ?array
    {
        $key = self::PREFIX . $productId;

        // Step 1: 布隆过滤器前置拦截（一定不存在的直接拒绝）
        if (!$this->bloomFilter->mightContain((string) $productId)) {
            return null;
        }

        // Step 2: 查缓存
        $cached = $this->redis->get($key);
        if ($cached !== false) {
            // 命中空值标记
            if ($cached === self::NULL_PLACEHOLDER) {
                return null;
            }
            return json_decode($cached, true);
        }

        // Step 3: 缓存未命中，加互斥锁防止并发击穿
        $lockKey = $key . ':lock';
        $locked = $this->redis->set($lockKey, '1', ['NX', 'EX' => self::LOCK_TTL]);

        if (!$locked) {
            // 未获取到锁，短暂等待后重试（或返回降级数据）
            usleep(50000); // 50ms
            return $this->getProduct($productId);
        }

        try {
            // Double check: 获取锁后再查一次缓存（可能被其他进程回填了）
            $cached = $this->redis->get($key);
            if ($cached !== false) {
                if ($cached === self::NULL_PLACEHOLDER) {
                    return null;
                }
                return json_decode($cached, true);
            }

            // Step 4: 查 DB
            $product = $this->repository->findById($productId);

            // Step 5: 回填缓存
            if ($product !== null) {
                $ttl = self::DEFAULT_TTL + random_int(0, 300); // 加随机偏移防雪崩
                $this->redis->setex($key, $ttl, json_encode($product));
            } else {
                // 空值缓存，短 TTL
                $this->redis->setex($key, self::NULL_TTL, self::NULL_PLACEHOLDER);
            }

            return $product;
        } finally {
            // 释放锁
            $this->redis->del($lockKey);
        }
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$bloomFilter = new BloomFilter($redis);
$repository = new ProductRepository(/* ... */);
$cache = new ProductCache($redis, $repository, $bloomFilter);

// 正常查询 — 存在的商品
$product = $cache->getProduct(12345);
// 流程：布隆过滤器通过 → 查缓存 miss → 加锁 → 查 DB → 回填缓存 → 返回数据

// 攻击请求 — 不存在的商品
$product = $cache->getProduct(99999999);
// 流程：布隆过滤器通过(误判) → 查缓存 miss → 加锁 → 查 DB 为 null → 写入空值标记 → 返回 null
// 后续再查 99999999：缓存命中空值标记 → 直接返回 null（5 分钟内不再查 DB）

// 布隆过滤器直接拦截的请求
$product = $cache->getProduct(-1);
// 流程：布隆过滤器返回 false → 直接 return null（不查缓存也不查 DB）
```

#### 方案三：参数校验前置

最简单但容易被忽略——在入口层直接校验参数合法性：

```php
// Controller 层
public function show(int $id): JsonResponse
{
    // ID 必须为正整数，最大不超过业务自增上限
    if ($id <= 0 || $id > 100000000) {
        return response()->json(['error' => 'Invalid product ID'], 400);
    }
    // ...
}
```

### 2.3 大厂实践

| 层级 | 手段 | 说明 |
|------|------|------|
| 网关层 | 参数校验 + 限流 | 非法参数直接 400，同 IP 限频 |
| 应用层 | 布隆过滤器 | 拦截一定不存在的请求 |
| 缓存层 | 空值缓存 + 短 TTL | 兜底少量漏网请求 |
| DB 层 | SQL 限流 | 最后一道防线 |

---

## 三、缓存击穿（Cache Breakdown / Hotspot Invalid）

### 3.1 问题描述

某个**热点 key** 过期的瞬间，大量并发请求同时穿透到 DB。和穿透的区别：数据存在，只是缓存过期了。

典型场景：秒杀商品详情、热搜话题、明星八卦。

### 3.2 解决方案

#### 方案一：互斥锁（Mutex Lock）

只让一个请求去查 DB 并回填，其余等待。

> 📌 分布式锁的完整深入（Owner 标识、Lua 原子释放、看门狗续期、FPM 方案）见 [distributed-lock.md](./distributed-lock.md)

上面 `getProduct()` 中已包含互斥锁逻辑。单独提炼核心模式：

```php
<?php

declare(strict_types=1);

namespace App\Cache;

use Redis;

/**
 * 基于 Redis SETNX 的互斥锁缓存模式
 */
class MutexCachePattern
{
    private Redis $redis;

    public function __construct(Redis $redis)
    {
        $this->redis = $redis;
    }

    /**
     * 互斥锁模式读缓存
     *
     * @param string $key 缓存 key
     * @param callable $loader DB 查询回调 fn(): mixed
     * @param int $ttl 缓存过期时间(秒)
     * @param int $lockTimeout 锁超时(秒)
     * @param int $waitMs 等待间隔(毫秒)
     * @param int $maxRetries 最大重试次数
     */
    public function getOrLoad(
        string $key,
        callable $loader,
        int $ttl = 3600,
        int $lockTimeout = 5,
        int $waitMs = 50,
        int $maxRetries = 100
    ): mixed {
        // 尝试从缓存获取
        $value = $this->redis->get($key);
        if ($value !== false) {
            return json_decode($value, true);
        }

        // 尝试获取互斥锁
        $lockKey = $key . ':mutex';
        $retries = 0;

        while ($retries < $maxRetries) {
            $locked = $this->redis->set(
                $lockKey,
                posix_getpid(),  // 存进程 ID 便于排查
                ['NX', 'EX' => $lockTimeout]
            );

            if ($locked) {
                try {
                    // Double check
                    $value = $this->redis->get($key);
                    if ($value !== false) {
                        return json_decode($value, true);
                    }

                    // 查 DB
                    $data = $loader();

                    // 回填缓存（加随机偏移）
                    $actualTtl = $ttl + random_int(0, (int) ($ttl * 0.1));
                    $this->redis->setex($key, $actualTtl, json_encode($data));

                    return $data;
                } finally {
                    $this->redis->del($lockKey);
                }
            }

            // 未获取锁，等待后重试
            usleep($waitMs * 1000);
            $retries++;

            // 每次重试先查缓存（可能已被回填）
            $value = $this->redis->get($key);
            if ($value !== false) {
                return json_decode($value, true);
            }
        }

        // 超过最大重试，降级直查 DB（限流保护下）
        return $loader();
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$mutexCache = new MutexCachePattern($redis);

// 场景：热点商品详情（多个请求同时查同一个已过期的 key）
$product = $mutexCache->getOrLoad(
    key: 'product:hot_12345',
    loader: function () {
        // 这个回调只会被一个请求执行，其他请求等待
        // 模拟 DB 查询
        return ['id' => 12345, 'name' => 'iPhone 16', 'price' => 7999];
    },
    ttl: 3600,          // 缓存 1 小时
    lockTimeout: 5,     // 锁最多持有 5 秒
    waitMs: 50,         // 未获取锁的请求每 50ms 重试
    maxRetries: 100     // 最多重试 100 次（即最多等 5 秒）
);

// 场景：秒杀库存（一致性要求高，宁可等也不能脏读）
$stock = $mutexCache->getOrLoad(
    key: 'stock:sku_001',
    loader: fn() => ['sku' => 'sku_001', 'quantity' => 100],
    ttl: 60,            // 库存缓存 1 分钟
    lockTimeout: 3,
    waitMs: 20,         // 库存场景等待间隔更短
    maxRetries: 150
);
```

#### 方案二：逻辑过期（Logical Expiration）

不设置 Redis TTL，在 value 中存入逻辑过期时间。读取时发现过期，异步刷新，当前请求返回旧数据。

**特点**：牺牲短暂一致性，换取零等待。适合能容忍旧数据的场景（如商品详情页）。

```php
<?php

declare(strict_types=1);

namespace App\Cache;

use Redis;

/**
 * 逻辑过期模式
 * 
 * 缓存永不过期（或设极长 TTL），value 中携带逻辑过期时间。
 * 读取时发现逻辑过期 → 异步刷新 → 返回旧数据。
 */
class LogicalExpirationCache
{
    private Redis $redis;

    public function __construct(Redis $redis)
    {
        $this->redis = $redis;
    }

    /**
     * 写入带逻辑过期的缓存
     */
    public function set(string $key, mixed $data, int $logicalTtl): void
    {
        $wrapper = [
            'data' => $data,
            'expire_at' => time() + $logicalTtl,
        ];
        // Redis 层面设更长的 TTL（兜底防止永久占用内存）
        $this->redis->setex($key, $logicalTtl * 3, json_encode($wrapper));
    }

    /**
     * 读取 - 逻辑过期则异步刷新
     *
     * @param string $key 缓存 key
     * @param callable $loader 数据加载回调
     * @param int $logicalTtl 逻辑过期时间
     * @return mixed 数据（可能是旧值）
     */
    public function get(string $key, callable $loader, int $logicalTtl = 3600): mixed
    {
        $raw = $this->redis->get($key);

        if ($raw === false) {
            // 缓存完全不存在（冷启动），同步加载
            $data = $loader();
            $this->set($key, $data, $logicalTtl);
            return $data;
        }

        $wrapper = json_decode($raw, true);

        // 未逻辑过期，直接返回
        if ($wrapper['expire_at'] > time()) {
            return $wrapper['data'];
        }

        // 已逻辑过期 → 尝试获取刷新锁
        $lockKey = $key . ':refresh_lock';
        $locked = $this->redis->set($lockKey, '1', ['NX', 'EX' => 10]);

        if ($locked) {
            // 获取锁成功，异步刷新（生产环境用队列/协程）
            // Swoole/Hyperf 环境下：
            // go(function () use ($key, $loader, $logicalTtl, $lockKey) { ... });
            //
            // FPM 环境下用 fastcgi_finish_request 或队列：
            $this->asyncRefresh($key, $loader, $logicalTtl, $lockKey);
        }

        // 无论是否获取锁，先返回旧数据
        return $wrapper['data'];
    }

    /**
     * 异步刷新（简化版：FPM 下用 register_shutdown_function 实现）
     * 生产建议用消息队列
     */
    private function asyncRefresh(
        string $key,
        callable $loader,
        int $logicalTtl,
        string $lockKey
    ): void {
        register_shutdown_function(function () use ($key, $loader, $logicalTtl, $lockKey) {
            try {
                $data = $loader();
                $this->set($key, $data, $logicalTtl);
            } finally {
                $this->redis->del($lockKey);
            }
        });
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$logicalCache = new LogicalExpirationCache($redis);

// 预热：提前写入热点数据（部署脚本或定时任务执行）
$logicalCache->set('article:hot_001', ['id' => 1, 'title' => '热搜文章', 'views' => 100000], 600);

// 读取：逻辑未过期 → 直接返回
$article = $logicalCache->get(
    key: 'article:hot_001',
    loader: fn() => ['id' => 1, 'title' => '热搜文章', 'views' => 120000], // DB 查询
    logicalTtl: 600   // 逻辑过期 10 分钟
);
// 返回当前缓存数据，零等待

// 读取：逻辑已过期 → 返回旧数据 + 后台异步刷新
// 假设 10 分钟后再次请求：
// 1. 发现 expire_at < time()（逻辑过期了）
// 2. 获取刷新锁成功 → 异步调用 loader 更新缓存
// 3. 当前请求立即返回旧数据（不等待刷新完成）
// 4. 后续请求获取到新数据

// 适用场景：商品详情、文章内容、用户 Profile（能容忍几秒的旧数据）
// 不适用场景：库存、余额（必须实时一致）
```

#### 方案三：热点 key 永不过期 + 后台定时刷新

对确定的热点 key（如首页推荐、秒杀商品），不依赖 TTL，由后台定时任务主动刷新。

```php
<?php

// 定时任务（Cron / Swoole Timer / Laravel Schedule）
// 每 30 秒刷新一次热点商品缓存

class HotProductRefreshJob
{
    public function handle(Redis $redis, ProductRepository $repo): void
    {
        // 热点商品 ID 列表（来自运营配置或实时 TopK 统计）
        $hotIds = $redis->sMembers('hot_product_ids');

        foreach ($hotIds as $id) {
            $product = $repo->findById((int) $id);
            if ($product !== null) {
                $redis->setex(
                    'product:' . $id,
                    7200, // 2 小时兜底 TTL
                    json_encode($product)
                );
            }
        }
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

// 1. 运营后台配置热点商品（或由实时 TopK 算法自动写入）
$redis->sAdd('hot_product_ids', '1001', '1002', '1003', '2001');

// 2. 定时任务注册（以 Laravel 为例）
// app/Console/Kernel.php:
//   $schedule->job(new HotProductRefreshJob)->everyThirtySeconds();
//
// Hyperf Crontab:
//   #[Crontab(rule: "*/30 * * * * *", memo: "hot-product-refresh")]
//
// Swoole Timer:
//   Timer::tick(30000, function() use ($redis, $repo) {
//       (new HotProductRefreshJob)->handle($redis, $repo);
//   });

// 3. 业务读取时正常读缓存即可 — 缓存永远是热的
$cached = $redis->get('product:1001');
$product = json_decode($cached, true);
// 因为后台每 30 秒刷新，key 永远不会过期（或在极端情况下 2 小时兜底 TTL 到期前已被刷新）
// 所以不存在击穿问题
```

### 3.3 方案对比

| 方案 | 一致性 | 可用性 | 实现复杂度 | 适用场景 |
|------|--------|--------|-----------|----------|
| 互斥锁 | 强 | 有等待 | 中 | 数据一致性要求高（库存、余额） |
| 逻辑过期 | 弱（短暂旧数据） | 高（零等待） | 中 | 容忍短暂不一致（商品详情、文章） |
| 后台刷新 | 中 | 高 | 低 | 热点可预知（首页、活动页） |

---

## 四、缓存雪崩（Cache Avalanche）

### 4.1 问题描述

两种触发场景：
1. **大量 key 同时过期**：同一批数据用相同 TTL，到期时 DB 瞬间承压
2. **Redis 集群整体不可用**：宕机、网络分区

### 4.2 解决方案

#### 方案一：TTL 随机化（解决同时过期）

```php
<?php

/**
 * 设置缓存时，TTL 加随机偏移
 * 
 * 基准 TTL 3600 秒，加 0~600 秒随机 → 实际 TTL 分布在 3600~4200
 * 避免大量 key 在同一秒过期
 */
function setWithJitter(Redis $redis, string $key, mixed $data, int $baseTtl): void
{
    // 随机偏移为基准的 10%~20%
    $jitter = random_int(0, (int) ($baseTtl * 0.15));
    $actualTtl = $baseTtl + $jitter;
    $redis->setex($key, $actualTtl, json_encode($data));
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

// 场景：批量导入商品缓存时，不要用固定 TTL
// ❌ 错误：所有商品都用 3600 秒 → 1 小时后同时过期 → 雪崩
foreach ($products as $product) {
    $redis->setex('product:' . $product['id'], 3600, json_encode($product));
}

// ✅ 正确：加随机偏移，TTL 分散在 3600~4140 秒之间
foreach ($products as $product) {
    setWithJitter($redis, 'product:' . $product['id'], $product, 3600);
}
// 结果：过期时间分散在未来 1 小时~1 小时 9 分之间，不会同时失效
```

#### 方案二：多级缓存（L1 进程缓存 + L2 Redis）

即使 Redis 不可用，本地缓存仍可兜底。

```php
<?php

declare(strict_types=1);

namespace App\Cache;

use Redis;
use Psr\Log\LoggerInterface;

/**
 * 两级缓存
 * 
 * L1: 进程内存（APCu / Swoole Table / 静态数组）
 * L2: Redis
 * L3: DB
 * 
 * 生产环境 L1 推荐用 APCu（FPM）或 Swoole Table / 协程上下文（Hyperf）
 */
class MultiLevelCache
{
    /** @var array<string, array{data: mixed, expire_at: int}> 进程级缓存 */
    private static array $localCache = [];
    private const LOCAL_MAX_SIZE = 10000;

    private Redis $redis;
    private LoggerInterface $logger;

    public function __construct(Redis $redis, LoggerInterface $logger)
    {
        $this->redis = $redis;
        $this->logger = $logger;
    }

    /**
     * 多级缓存读取
     * 
     * @param string $key 缓存 key
     * @param callable $loader DB 加载器
     * @param int $l1Ttl L1 本地缓存秒数（短，如 10-60 秒）
     * @param int $l2Ttl L2 Redis 缓存秒数（长，如 3600 秒）
     */
    public function get(
        string $key,
        callable $loader,
        int $l1Ttl = 30,
        int $l2Ttl = 3600
    ): mixed {
        // L1: 进程内存
        if (isset(self::$localCache[$key])) {
            $entry = self::$localCache[$key];
            if ($entry['expire_at'] > time()) {
                return $entry['data'];
            }
            unset(self::$localCache[$key]);
        }

        // L2: Redis
        try {
            $cached = $this->redis->get($key);
            if ($cached !== false) {
                $data = json_decode($cached, true);
                $this->setLocal($key, $data, $l1Ttl);
                return $data;
            }
        } catch (\RedisException $e) {
            // Redis 不可用，降级继续查 DB
            $this->logger->error('Redis unavailable', [
                'key' => $key,
                'error' => $e->getMessage(),
            ]);
        }

        // L3: DB
        $data = $loader();

        // 回填 L2
        try {
            $ttl = $l2Ttl + random_int(0, (int) ($l2Ttl * 0.1));
            $this->redis->setex($key, $ttl, json_encode($data));
        } catch (\RedisException $e) {
            $this->logger->error('Redis write failed', ['key' => $key]);
        }

        // 回填 L1
        $this->setLocal($key, $data, $l1Ttl);

        return $data;
    }

    private function setLocal(string $key, mixed $data, int $ttl): void
    {
        // 简单 LRU 淘汰：超限时清掉一半
        if (count(self::$localCache) >= self::LOCAL_MAX_SIZE) {
            self::$localCache = array_slice(self::$localCache, (int) (self::LOCAL_MAX_SIZE / 2));
        }
        self::$localCache[$key] = [
            'data' => $data,
            'expire_at' => time() + $ttl,
        ];
    }

    /**
     * 删除缓存（所有层级）
     */
    public function invalidate(string $key): void
    {
        unset(self::$localCache[$key]);
        try {
            $this->redis->del($key);
        } catch (\RedisException $e) {
            $this->logger->error('Redis delete failed', ['key' => $key]);
        }
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);
$logger = new YourPsrLogger(); // 任意 PSR-3 Logger

$cache = new MultiLevelCache($redis, $logger);

// 读取用户信息（L1 本地 30 秒 + L2 Redis 1 小时）
$user = $cache->get(
    key: 'user:10001',
    loader: function () {
        // 只有 L1 和 L2 都 miss 时才执行（查 DB）
        return ['id' => 10001, 'name' => 'Jake', 'level' => 'vip'];
    },
    l1Ttl: 30,    // 本地缓存 30 秒（进程内，零延迟）
    l2Ttl: 3600   // Redis 缓存 1 小时
);

// 第二次调用（30 秒内）— 命中 L1 本地内存，不走 Redis
$user = $cache->get('user:10001', fn() => null, 30, 3600);

// 第三次调用（30 秒后，1 小时内）— L1 过期，命中 L2 Redis
$user = $cache->get('user:10001', fn() => null, 30, 3600);

// Redis 宕机场景 — L2 抛异常，降级直查 DB，L1 继续缓存结果
// 后续 30 秒内的请求都走 L1，不会继续打 Redis 和 DB

// 更新时删除所有层级
$cache->invalidate('user:10001');
```

#### 方案三：熔断 + 降级

Redis 不可用时，启动熔断器，短时间内直接返回降级数据或从本地缓存读取：

```php
<?php

declare(strict_types=1);

namespace App\Cache;

/**
 * 简易熔断器（滑动窗口计数）
 * 
 * 生产环境推荐用 Hyperf 自带的 CircuitBreaker 或 Resilience 库
 */
class SimpleCircuitBreaker
{
    private const STATE_CLOSED = 'closed';       // 正常
    private const STATE_OPEN = 'open';           // 熔断
    private const STATE_HALF_OPEN = 'half_open'; // 试探

    private string $state = self::STATE_CLOSED;
    private int $failureCount = 0;
    private int $successCount = 0;
    private int $lastFailureTime = 0;

    public function __construct(
        private int $failureThreshold = 5,   // 连续失败 5 次触发熔断
        private int $recoveryTimeout = 30,   // 熔断 30 秒后进入半开
        private int $halfOpenMaxAttempts = 3  // 半开状态成功 3 次关闭熔断
    ) {
    }

    public function isAvailable(): bool
    {
        return match ($this->state) {
            self::STATE_CLOSED => true,
            self::STATE_OPEN => $this->shouldAttemptRecovery(),
            self::STATE_HALF_OPEN => true,
            default => false,
        };
    }

    public function recordSuccess(): void
    {
        if ($this->state === self::STATE_HALF_OPEN) {
            $this->successCount++;
            if ($this->successCount >= $this->halfOpenMaxAttempts) {
                $this->state = self::STATE_CLOSED;
                $this->reset();
            }
        } else {
            $this->reset();
        }
    }

    public function recordFailure(): void
    {
        $this->failureCount++;
        $this->lastFailureTime = time();

        if ($this->failureCount >= $this->failureThreshold) {
            $this->state = self::STATE_OPEN;
        }
    }

    private function shouldAttemptRecovery(): bool
    {
        if (time() - $this->lastFailureTime >= $this->recoveryTimeout) {
            $this->state = self::STATE_HALF_OPEN;
            $this->successCount = 0;
            return true;
        }
        return false;
    }

    private function reset(): void
    {
        $this->failureCount = 0;
        $this->successCount = 0;
    }
}

// ====================================================================
// 使用示例：熔断器保护 Redis 调用
// ====================================================================

$breaker = new SimpleCircuitBreaker(
    failureThreshold: 5,      // 连续失败 5 次触发熔断
    recoveryTimeout: 30,      // 熔断 30 秒后进入半开状态试探
    halfOpenMaxAttempts: 3    // 半开状态连续成功 3 次才关闭熔断
);

function getFromRedisWithBreaker(
    SimpleCircuitBreaker $breaker,
    Redis $redis,
    string $key
): ?string {
    // 1. 熔断器检查：熔断中则直接返回降级
    if (!$breaker->isAvailable()) {
        // 熔断开启，直接返回 null（降级：跳过 Redis，查 DB 或返回默认值）
        echo "熔断中，跳过 Redis\n";
        return null;
    }

    // 2. 尝试调用 Redis
    try {
        $result = $redis->get($key);
        $breaker->recordSuccess(); // 成功，计数器重置
        return $result ?: null;
    } catch (\RedisException $e) {
        $breaker->recordFailure(); // 失败，累计计数
        echo "Redis 调用失败（{$breaker->failureCount}/{$breaker->failureThreshold}）\n";
        return null;
    }
}

// 模拟 Redis 故障场景：
// 前 4 次失败 → 记录失败，继续尝试
// 第 5 次失败 → 触发熔断
// 之后的请求 → 直接跳过 Redis（30 秒内不再尝试）
// 30 秒后 → 进入半开，试探性发请求
// 连续成功 3 次 → 关闭熔断，恢复正常
```

### 4.3 大厂实践（完整防雪崩架构）

```
                      ┌───────────────┐
                      │   客户端/CDN   │  ← 静态资源 + 页面缓存
                      └──────┬────────┘
                             │
                      ┌──────▼────────┐
                      │    网关层      │  ← 限流（令牌桶）+ 熔断
                      └──────┬────────┘
                             │
                      ┌──────▼────────┐
                      │   L1 本地缓存  │  ← APCu / Swoole Table（秒级 TTL）
                      └──────┬────────┘
                             │ miss
                      ┌──────▼────────┐
                      │   L2 Redis    │  ← 主从 + Sentinel / Cluster
                      └──────┬────────┘
                             │ miss 或 unavailable
                      ┌──────▼────────┐
                      │   熔断器判断   │  ← 熔断则返回降级
                      └──────┬────────┘
                             │ 允许通过
                      ┌──────▼────────┐
                      │     DB        │  ← 连接池限制 + SQL 限流
                      └───────────────┘
```

---

## 五、缓存一致性（Cache Consistency）

### 5.1 经典问题

缓存与 DB 是两个独立的数据源，更新时天然存在不一致窗口。

**错误做法**：
- ❌ 先更新缓存，再更新 DB → DB 失败后缓存是脏数据
- ❌ 先更新 DB，再更新缓存 → 并发下 A、B 两个写请求可能导致缓存存旧值

### 5.2 Cache-Aside 模式（旁路缓存 — 最常用）

**读**：缓存 → 命中返回 → 未命中查 DB → 回填缓存
**写**：先更新 DB → 再删除缓存

```php
<?php

declare(strict_types=1);

namespace App\Service;

use Redis;
use App\Repository\ProductRepository;
use Psr\Log\LoggerInterface;

class ProductService
{
    private const CACHE_PREFIX = 'product:';
    private const CACHE_TTL = 3600;

    public function __construct(
        private Redis $redis,
        private ProductRepository $repository,
        private LoggerInterface $logger
    ) {
    }

    /**
     * 读取商品 - Cache Aside 模式
     */
    public function getProduct(int $id): ?array
    {
        $key = self::CACHE_PREFIX . $id;

        // 1. 查缓存
        $cached = $this->redis->get($key);
        if ($cached !== false) {
            return json_decode($cached, true);
        }

        // 2. 查 DB
        $product = $this->repository->findById($id);
        if ($product === null) {
            return null;
        }

        // 3. 回填缓存
        $ttl = self::CACHE_TTL + random_int(0, 300);
        $this->redis->setex($key, $ttl, json_encode($product));

        return $product;
    }

    /**
     * 更新商品 - 先更新 DB，再删除缓存
     */
    public function updateProduct(int $id, array $data): bool
    {
        // 1. 先更新 DB（事务内）
        $success = $this->repository->update($id, $data);
        if (!$success) {
            return false;
        }

        // 2. 删除缓存
        $key = self::CACHE_PREFIX . $id;
        $this->redis->del($key);

        return true;
    }
}
```

**为什么是删缓存而不是更新缓存**：
- 更新缓存在并发场景下可能出现 A 更新 DB 后还没更新缓存，B 又更新了 DB 和缓存，最后 A 才更新缓存导致缓存是旧值
- 删除缓存则让下次读的人负责回填，天然用最新 DB 数据

### 5.3 延迟双删（解决先删缓存、后更新 DB 的不一致）

极端场景：线程 A 删了缓存，还没更新 DB；线程 B 读 miss 后回填了旧数据到缓存。

**方案**：更新 DB 后，延迟一段时间再删一次缓存。

```php
<?php

declare(strict_types=1);

namespace App\Service;

use Redis;
use App\Repository\ProductRepository;
use App\Queue\DelayedCacheInvalidationJob;

class ProductServiceWithDoubleDelete
{
    private const CACHE_PREFIX = 'product:';
    private const DELAY_MS = 500; // 延迟 500ms 删第二次

    public function __construct(
        private Redis $redis,
        private ProductRepository $repository,
        private DelayQueue $delayQueue
    ) {
    }

    /**
     * 延迟双删
     * 
     * 1. 删缓存
     * 2. 更新 DB
     * 3. 延迟 N ms 再删一次缓存
     * 
     * 延迟时间 > 一次读请求回填缓存的耗时（通常 DB 查询 + 序列化 < 500ms）
     */
    public function updateProduct(int $id, array $data): bool
    {
        $key = self::CACHE_PREFIX . $id;

        // Step 1: 删缓存
        $this->redis->del($key);

        // Step 2: 更新 DB
        $success = $this->repository->update($id, $data);
        if (!$success) {
            return false;
        }

        // Step 3: 延迟二次删除（通过延迟队列实现）
        $this->delayQueue->dispatch(
            new DelayedCacheInvalidationJob($key),
            self::DELAY_MS
        );

        return true;
    }
}

/**
 * 延迟删除缓存 Job
 */
class DelayedCacheInvalidationJob
{
    public function __construct(private string $cacheKey)
    {
    }

    public function handle(Redis $redis): void
    {
        $redis->del($this->cacheKey);
    }
}
```

### 5.4 基于 Binlog 的最终一致性（Canal / Debezium）

大厂主流方案——**不在业务代码里删缓存**，而是监听 MySQL binlog 变更，异步删除/更新缓存。

```
          ┌──────────┐     write      ┌──────────┐
          │  应用层   │──────────────→│  MySQL   │
          └──────────┘                └────┬─────┘
                                           │ binlog
                                      ┌────▼─────┐
                                      │  Canal   │  ← 伪装成 slave 拉 binlog
                                      └────┬─────┘
                                           │ 解析变更
                                      ┌────▼─────┐
                                      │   MQ     │  ← Kafka / RocketMQ
                                      └────┬─────┘
                                           │ 消费
                                      ┌────▼─────┐
                                      │ 缓存同步  │  ← 删除或更新 Redis
                                      │  Worker  │
                                      └──────────┘
```

```php
<?php

declare(strict_types=1);

namespace App\Consumer;

use Redis;
use Psr\Log\LoggerInterface;

/**
 * Canal binlog 变更消费者
 * 
 * 消费 MySQL binlog 变更事件，同步失效 Redis 缓存。
 * 通常部署为独立进程/消费者组。
 */
class BinlogCacheInvalidator
{
    private const KEY_MAP = [
        // 表名 => 缓存 key 前缀
        'products' => 'product:',
        'users' => 'user:',
        'orders' => 'order:',
    ];

    public function __construct(
        private Redis $redis,
        private LoggerInterface $logger
    ) {
    }

    /**
     * 处理 binlog 变更事件
     * 
     * @param array $event Canal 推送的事件结构
     * 格式示例:
     * {
     *   "database": "shop",
     *   "table": "products",
     *   "type": "UPDATE",  // INSERT, UPDATE, DELETE
     *   "data": [{"id": 123, "name": "..."}],
     *   "old": [{"id": 123, "name": "old..."}]
     * }
     */
    public function handle(array $event): void
    {
        $table = $event['table'] ?? '';
        $type = $event['type'] ?? '';
        $rows = $event['data'] ?? [];

        if (!isset(self::KEY_MAP[$table])) {
            return;
        }

        $prefix = self::KEY_MAP[$table];

        foreach ($rows as $row) {
            $id = $row['id'] ?? null;
            if ($id === null) {
                continue;
            }

            $key = $prefix . $id;

            // 删除缓存（幂等操作，重复消费无副作用）
            $deleted = $this->redis->del($key);

            $this->logger->info('Cache invalidated by binlog', [
                'table' => $table,
                'type' => $type,
                'id' => $id,
                'key' => $key,
                'deleted' => $deleted,
            ]);
        }
    }

    /**
     * 批量失效（针对批量 UPDATE 场景）
     */
    public function handleBatch(array $events): void
    {
        $keysToDelete = [];

        foreach ($events as $event) {
            $table = $event['table'] ?? '';
            if (!isset(self::KEY_MAP[$table])) {
                continue;
            }

            $prefix = self::KEY_MAP[$table];
            foreach ($event['data'] ?? [] as $row) {
                if (isset($row['id'])) {
                    $keysToDelete[] = $prefix . $row['id'];
                }
            }
        }

        if (!empty($keysToDelete)) {
            // Pipeline 批量删除
            $pipe = $this->redis->pipeline();
            foreach ($keysToDelete as $key) {
                $pipe->del($key);
            }
            $pipe->exec();

            $this->logger->info('Batch cache invalidation', [
                'count' => count($keysToDelete),
            ]);
        }
    }
}
```

### 5.5 一致性方案对比

| 方案 | 一致性强度 | 复杂度 | 延迟 | 适用场景 |
|------|-----------|--------|------|----------|
| Cache-Aside（先更 DB 后删缓存） | 最终一致（极短窗口） | 低 | 无 | 大部分 CRUD 场景 |
| 延迟双删 | 最终一致 | 中 | 500ms~1s | 写多读多、对一致性有要求 |
| 读写锁 | 强一致 | 高 | 高 | 金融场景 |
| Binlog 订阅 | 最终一致 | 高（架构重） | 秒级 | 大厂标配、异构数据同步 |
| 版本号/ETag | 最终一致 | 中 | 无 | 客户端缓存、API 缓存 |

### 5.6 面试追问：为什么不先删缓存再更新 DB？

**经典反例**：
1. 线程 A 删除缓存
2. 线程 B 读缓存 miss，查 DB 获取**旧值**，回填缓存
3. 线程 A 更新 DB 为**新值**
4. 结果：缓存中是旧值，DB 是新值 → **不一致**

**先更 DB 后删缓存也有问题**（但概率极低）：
1. 线程 A 读缓存 miss，查 DB 获取旧值
2. 线程 B 更新 DB 为新值
3. 线程 B 删缓存
4. 线程 A 回填缓存为旧值
5. 结果：不一致

概率极低的原因：第 4 步（写缓存）通常比第 2 步（写 DB）快得多，实际上几乎不可能发生。加上 TTL 兜底，即使发生也是短暂不一致。

---

## 六、综合实战：电商商品缓存完整方案

```php
<?php

declare(strict_types=1);

namespace App\Service;

use Redis;
use App\Cache\BloomFilter;
use App\Cache\MultiLevelCache;
use App\Cache\SimpleCircuitBreaker;
use App\Repository\ProductRepository;
use Psr\Log\LoggerInterface;

/**
 * 电商商品缓存完整方案
 * 
 * 融合防穿透、防击穿、防雪崩、最终一致性
 */
class ProductCacheService
{
    private const CACHE_PREFIX = 'product:';
    private const L1_TTL = 30;       // 本地缓存 30 秒
    private const L2_TTL = 3600;     // Redis 1 小时
    private const NULL_TTL = 300;    // 空值 5 分钟
    private const NULL_MARKER = '__NULL__';
    private const LOCK_TTL = 5;

    public function __construct(
        private Redis $redis,
        private ProductRepository $repository,
        private BloomFilter $bloomFilter,
        private MultiLevelCache $multiLevelCache,
        private SimpleCircuitBreaker $circuitBreaker,
        private LoggerInterface $logger
    ) {
    }

    /**
     * 读取商品 - 完整缓存策略
     */
    public function getProduct(int $id): ?array
    {
        // 1. 参数校验
        if ($id <= 0) {
            return null;
        }

        // 2. 布隆过滤器防穿透
        if (!$this->bloomFilter->mightContain((string) $id)) {
            return null;
        }

        $key = self::CACHE_PREFIX . $id;

        // 3. 多级缓存读取（内含 L1 + L2 + 熔断降级）
        return $this->multiLevelCache->get(
            $key,
            fn() => $this->loadFromDbWithProtection($id, $key),
            self::L1_TTL,
            self::L2_TTL
        );
    }

    /**
     * 更新商品 - Cache-Aside + 延迟双删
     */
    public function updateProduct(int $id, array $data): bool
    {
        $key = self::CACHE_PREFIX . $id;

        // 1. 更新 DB
        $success = $this->repository->update($id, $data);
        if (!$success) {
            return false;
        }

        // 2. 立即删除缓存（所有层级）
        $this->multiLevelCache->invalidate($key);

        // 3. 延迟二次删除（通过队列，此处用 Redis 延迟队列示意）
        $this->redis->zAdd(
            'delayed_cache_invalidation',
            time() + 1, // 1 秒后
            $key
        );

        // 4. 同步更新布隆过滤器（如果是新增商品）
        $this->bloomFilter->add((string) $id);

        return true;
    }

    /**
     * DB 加载 + 互斥锁保护
     */
    private function loadFromDbWithProtection(int $id, string $key): ?array
    {
        // 熔断检查
        if (!$this->circuitBreaker->isAvailable()) {
            $this->logger->warning('Circuit breaker open, returning null', ['id' => $id]);
            return null; // 或返回降级数据
        }

        // 互斥锁
        $lockKey = $key . ':lock';
        $locked = $this->redis->set($lockKey, '1', ['NX', 'EX' => self::LOCK_TTL]);

        if (!$locked) {
            // 等待 50ms 后重试一次
            usleep(50000);
            $cached = $this->redis->get($key);
            if ($cached !== false && $cached !== self::NULL_MARKER) {
                return json_decode($cached, true);
            }
            // 仍然 miss，降级直查
        }

        try {
            $product = $this->repository->findById($id);
            $this->circuitBreaker->recordSuccess();
            return $product;
        } catch (\Throwable $e) {
            $this->circuitBreaker->recordFailure();
            $this->logger->error('DB query failed', ['id' => $id, 'error' => $e->getMessage()]);
            return null;
        } finally {
            if ($locked) {
                $this->redis->del($lockKey);
            }
        }
    }
}
```

---

## 七、面试高频题

### Q1: 穿透、击穿、雪崩有什么区别？

- **穿透**：数据不存在（缓存和 DB 都没有）
- **击穿**：热点 key 过期（数据存在，只是缓存过期了）
- **雪崩**：大量 key 同时过期，或 Redis 不可用

### Q2: 为什么先更新 DB 再删缓存，而不是先删缓存再更新 DB？

先删缓存的不一致窗口大（读请求回填旧数据的概率高）；先更新 DB 后删缓存的不一致窗口极小（需要读 DB 比写 Redis 慢才会出现，几乎不可能）。

### Q3: 布隆过滤器有误判怎么办？

布隆过滤器只有假阳性（说存在但实际不存在），没有假阴性。误判的请求会走到空值缓存兜底，不会打爆 DB。

### Q4: 互斥锁用 Redis 实现有什么坑？

- 锁超时后自动释放，但业务还没执行完 → 其他线程进入（加看门狗续期）
- 释放了别人的锁 → 加 value 校验（存 requestId / processId）
- Redis 主从切换丢锁 → Redlock（多数派确认）

### Q5: 大厂如何保证缓存一致性？

标准答案：**Canal 监听 binlog + MQ 异步失效 + TTL 兜底**。不在业务代码里硬编码删缓存逻辑，解耦且可靠。

---

## 八、参考资料

- Redis 官方：缓存模式 https://redis.io/docs/manual/patterns/
- 阿里云：缓存一致性最佳实践
- 美团技术博客：缓存穿透/击穿/雪崩解决方案
- Canal GitHub：https://github.com/alibaba/canal
