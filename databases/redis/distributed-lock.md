# Redis 分布式锁

**标签**: #redis #distributed-lock #高频 #系统设计
**难度**: ⭐⭐⭐⭐
**适用场景**: 缓存击穿互斥、库存扣减、订单防重、定时任务抢占

---

## 一、为什么需要分布式锁

单机环境用 `flock()` 或 `synchronized` 就行。但分布式环境下多台服务器的多个进程需要互斥访问同一资源，需要一个所有节点都能访问的外部锁服务。

常见实现方式：
- **Redis**（最主流）：性能高、实现简单
- **ZooKeeper**：强一致性、临时顺序节点实现
- **etcd**：Lease + Compare 机制
- **MySQL**：`SELECT ... FOR UPDATE`（性能差，不推荐高并发）

---

## 二、从基础到生产级，逐步演进

### 2.1 最简版（有严重 Bug）

```php
<?php

// ❌ 错误示范 — 用于理解问题

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

// 获取锁
$redis->setNx('lock:order:123', '1');  // 问题1：没有过期时间 → 进程挂了就死锁
// 释放锁
$redis->del('lock:order:123');          // 问题2：任何人都能删 → 误删别人的锁

// 加了过期时间？
$redis->setNx('lock:order:123', '1');
$redis->expire('lock:order:123', 5);    // 问题3：SETNX + EXPIRE 不是原子的
                                         // 如果 SETNX 后进程挂了，EXPIRE 没执行 → 还是死锁
```

### 2.2 正确的基础版（SET NX EX 原子操作）

```php
<?php

// ✅ 原子操作：SET key value NX EX seconds（Redis 2.6.12+）
$locked = $redis->set('lock:order:123', '1', ['NX', 'EX' => 5]);

// NX + EX 在一条命令内完成，要么全成功，要么全失败
// 即使获取锁后进程立刻崩溃，5 秒后锁也会自动过期
```

但仍有问题：
- value 是 `'1'` → 任何人都能删（误删别人的锁）
- TTL 固定 → 业务没执行完就过期

### 2.3 生产级：Owner 标识 + Lua 原子释放

```php
<?php

declare(strict_types=1);

namespace App\Lock;

use Redis;

/**
 * 带 Owner 标识的分布式锁
 * 
 * 解决两个问题：
 *   1. 误删别人的锁 → value 存唯一 owner，释放时校验
 *   2. 判断 + 删除的原子性 → 用 Lua 脚本
 */
class RedisDistributedLock
{
    private Redis $redis;

    public function __construct(Redis $redis)
    {
        $this->redis = $redis;
    }

    /**
     * 获取锁
     * 
     * @param string $lockKey 锁的 key
     * @param int $ttl 锁超时时间（秒）— 防死锁兜底
     * @return string|false 成功返回 owner token，失败返回 false
     */
    public function acquire(string $lockKey, int $ttl = 10): string|false
    {
        // 生成全局唯一的 owner 标识
        // 用 进程ID + 随机字节，即使同一台机器的不同进程也不会冲突
        $owner = gethostname() . ':' . getmypid() . ':' . bin2hex(random_bytes(8));

        // SET NX EX 原子获取锁，value 存 owner
        $locked = $this->redis->set($lockKey, $owner, ['NX', 'EX' => $ttl]);

        return $locked ? $owner : false;
    }

    /**
     * 释放锁 — 只能释放自己的锁
     * 
     * 为什么必须用 Lua？
     * 
     * 如果用两条 PHP 命令：
     *   $val = $redis->get($lockKey);      ← T1：读到自己的 owner
     *   if ($val === $owner) {
     *       $redis->del($lockKey);         ← T2：删除
     *   }
     * 
     * T1 和 T2 之间有时间窗口！可能发生：
     *   T1：A 读到 owner 是自己的 ✅
     *   T1.5：锁到期自动过期，B 获取了新锁
     *   T2：A 执行 DEL → 删掉了 B 的锁 ❌
     * 
     * Lua 脚本在 Redis 内部原子执行，GET + DEL 之间不会被打断。
     */
    public function release(string $lockKey, string $owner): bool
    {
        // KEYS[1] = lockKey, ARGV[1] = owner
        $lua = <<<'LUA'
            if redis.call("GET", KEYS[1]) == ARGV[1] then
                return redis.call("DEL", KEYS[1])
            else
                return 0
            end
        LUA;

        // eval 的第三个参数是 KEYS 的数量
        $result = $this->redis->eval($lua, [$lockKey, $owner], 1);
        return $result === 1;
    }

    /**
     * 尝试获取锁（带重试）
     */
    public function acquireWithRetry(
        string $lockKey,
        int $ttl = 10,
        int $retryIntervalMs = 50,
        int $maxRetries = 60
    ): string|false {
        for ($i = 0; $i < $maxRetries; $i++) {
            $owner = $this->acquire($lockKey, $ttl);
            if ($owner !== false) {
                return $owner;
            }
            usleep($retryIntervalMs * 1000);
        }
        return false;
    }

    /**
     * 便捷方法：加锁 → 执行 → 释放
     */
    public function withLock(string $lockKey, callable $callback, int $ttl = 10): mixed
    {
        $owner = $this->acquire($lockKey, $ttl);
        if ($owner === false) {
            throw new \RuntimeException("Failed to acquire lock: {$lockKey}");
        }

        try {
            return $callback();
        } finally {
            $this->release($lockKey, $owner);
        }
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$lock = new RedisDistributedLock($redis);

// 用法一：手动获取和释放
$owner = $lock->acquire('lock:order:create:user_123', 10);
if ($owner !== false) {
    try {
        // 创建订单（防重复提交）
        createOrder($userId, $productId);
    } finally {
        $lock->release('lock:order:create:user_123', $owner);
    }
} else {
    // 获取锁失败 — 说明有另一个请求在处理中
    echo "请勿重复提交\n";
}

// 用法二：自动管理（推荐）
$result = $lock->withLock('lock:stock:sku_001', function () {
    // 扣减库存（同一时刻只有一个请求能执行）
    $stock = getStockFromDb('sku_001');
    if ($stock > 0) {
        decrementStock('sku_001');
        return true;
    }
    return false;
}, ttl: 5);

// 用法三：带重试
$owner = $lock->acquireWithRetry(
    lockKey: 'lock:payment:order_456',
    ttl: 15,
    retryIntervalMs: 100,
    maxRetries: 30  // 最多等 3 秒
);
```

### 2.4 看门狗自动续期（Watchdog）— 解决锁提前过期

**问题**：锁 TTL 设 10 秒，但 DB 查询因为某种原因花了 12 秒 → 锁到期了业务还没完成 → 别人拿到锁 → 并发冲突。

**解决**：获取锁后启动后台定时器，定期续期。业务完成时停止续期。

```php
<?php

declare(strict_types=1);

namespace App\Lock;

use Redis;
use Swoole\Timer;

/**
 * 带看门狗续期的分布式锁（Swoole / Hyperf 环境）
 * 
 * 原理（类似 Java Redisson 的 watchdog）：
 *
 *   获取锁（TTL=30s）
 *       │
 *       ├── 启动看门狗 Timer（每 10s 执行一次）
 *       │       │
 *       │       ├── T=10s: EXPIRE lock 30  ← 续期，TTL 重置为 30s
 *       │       ├── T=20s: EXPIRE lock 30  ← 续期
 *       │       ├── T=30s: EXPIRE lock 30  ← 续期
 *       │       └── ...持续续期
 *       │
 *       ├── 业务执行中...（无论多久都不会过期）
 *       │
 *       └── 业务完成 → release() → 停止 Timer + DEL lock
 *
 * 崩溃场景：
 *   进程崩溃 → Timer 也死了 → 无人续期 → 30s 后锁自然过期 → 不死锁 ✅
 */
class WatchdogLock
{
    private Redis $redis;

    /** @var array<string, int> lockKey => Swoole Timer ID */
    private array $watchdogs = [];

    private const DEFAULT_TTL = 30;              // 初始锁过期时间
    private const RENEWAL_RATIO = 3;             // 每 TTL/3 续期一次

    public function __construct(Redis $redis)
    {
        $this->redis = $redis;
    }

    /**
     * 获取锁 + 启动看门狗
     */
    public function acquire(string $lockKey, ?int $ttl = null): string|false
    {
        $ttl = $ttl ?? self::DEFAULT_TTL;
        $owner = gethostname() . ':' . getmypid() . ':' . bin2hex(random_bytes(8));

        $locked = $this->redis->set($lockKey, $owner, ['NX', 'EX' => $ttl]);
        if (!$locked) {
            return false;
        }

        // 启动看门狗
        $this->startWatchdog($lockKey, $owner, $ttl);

        return $owner;
    }

    /**
     * 释放锁 + 停止看门狗
     */
    public function release(string $lockKey, string $owner): bool
    {
        // 先停看门狗
        $this->stopWatchdog($lockKey);

        // Lua 原子释放
        $lua = <<<'LUA'
            if redis.call("GET", KEYS[1]) == ARGV[1] then
                return redis.call("DEL", KEYS[1])
            else
                return 0
            end
        LUA;

        return $this->redis->eval($lua, [$lockKey, $owner], 1) === 1;
    }

    /**
     * 启动看门狗定时器
     * 
     * 续期逻辑也用 Lua 保证原子性：只有 owner 匹配才续期
     * （防止续了别人的锁）
     */
    private function startWatchdog(string $lockKey, string $owner, int $ttl): void
    {
        $intervalMs = (int) ($ttl / self::RENEWAL_RATIO) * 1000; // 毫秒

        $timerId = Timer::tick($intervalMs, function () use ($lockKey, $owner, $ttl) {
            // Lua：只有 owner 匹配才续期
            $lua = <<<'LUA'
                if redis.call("GET", KEYS[1]) == ARGV[1] then
                    return redis.call("EXPIRE", KEYS[1], ARGV[2])
                else
                    return 0
                end
            LUA;

            $renewed = $this->redis->eval($lua, [$lockKey, $owner, (string) $ttl], 1);

            if (!$renewed) {
                // 锁已不属于自己（过期被抢了），停止续期
                $this->stopWatchdog($lockKey);
            }
        });

        $this->watchdogs[$lockKey] = $timerId;
    }

    private function stopWatchdog(string $lockKey): void
    {
        if (isset($this->watchdogs[$lockKey])) {
            Timer::clear($this->watchdogs[$lockKey]);
            unset($this->watchdogs[$lockKey]);
        }
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$lock = new WatchdogLock($redis);

// 场景：耗时不确定的操作（如大数据量导入）
$owner = $lock->acquire('lock:import:batch_001');
// 获取锁成功，TTL=30s，看门狗每 10s 续期

if ($owner !== false) {
    try {
        // 即使花了 5 分钟也不会过期
        importLargeDataSet();  // 耗时未知

        // 看门狗时间线：
        // T=0s   获取锁 TTL=30s
        // T=10s  续期 → TTL=30s
        // T=20s  续期 → TTL=30s
        // T=30s  续期 → TTL=30s（没有续期的话这里就过期了）
        // T=40s  续期 → TTL=30s
        // ...
        // T=300s 业务完成
    } finally {
        // 停止看门狗 + 释放锁
        $lock->release('lock:import:batch_001', $owner);
    }
}

// 场景：进程崩溃
// T=0s   获取锁 TTL=30s，看门狗启动
// T=10s  看门狗续期 → TTL=30s
// T=15s  进程被 kill -9 ！
//        → Timer 也被杀死 → 没人续期了
// T=45s  锁到期自动过期
// T=45s  其他进程可以正常获取锁 ✅ 不死锁
```

### 2.5 FPM 环境的完整方案（无看门狗）

FPM 没有常驻 Timer，不能做看门狗。策略：**TTL 设为业务最大耗时的 3 倍 + 业务侧超时控制**。

```php
<?php

declare(strict_types=1);

namespace App\Lock;

use Redis;

/**
 * FPM 环境分布式锁（完整生产版）
 *
 * 没有看门狗，靠以下手段弥补：
 *   1. TTL 设为业务最大耗时 × 3（宁愿别人多等，不愿冲突）
 *   2. DB 查询设超时（PDO timeout），避免慢查询拖死锁
 *   3. Owner 标识 + Lua 原子释放（防误删）
 *   4. 最大等待时间限制（不死等，超时降级）
 */
class FpmLock
{
    public function __construct(private Redis $redis)
    {
    }

    /**
     * 加锁执行业务的完整封装
     * 
     * @param string $lockKey 锁 key
     * @param callable $callback 业务逻辑
     * @param int $lockTtl 锁过期时间（建议：正常耗时 × 3）
     * @param int $waitTimeoutMs 最大等待时间（毫秒），超时后降级
     * @param int $retryIntervalMs 重试间隔（毫秒）
     * @param callable|null $fallback 获取锁失败时的降级逻辑
     */
    public function execute(
        string $lockKey,
        callable $callback,
        int $lockTtl = 10,
        int $waitTimeoutMs = 3000,
        int $retryIntervalMs = 50,
        ?callable $fallback = null
    ): mixed {
        $owner = $this->acquireWithTimeout($lockKey, $lockTtl, $waitTimeoutMs, $retryIntervalMs);

        if ($owner === false) {
            // 获取锁失败：执行降级逻辑
            if ($fallback !== null) {
                return $fallback();
            }
            throw new \RuntimeException("Lock timeout: {$lockKey}");
        }

        try {
            return $callback();
        } finally {
            $this->release($lockKey, $owner);
        }
    }

    private function acquireWithTimeout(
        string $lockKey,
        int $ttl,
        int $waitTimeoutMs,
        int $retryIntervalMs
    ): string|false {
        $owner = gethostname() . ':' . getmypid() . ':' . uniqid('', true);
        $waited = 0;

        while ($waited < $waitTimeoutMs) {
            $locked = $this->redis->set($lockKey, $owner, ['NX', 'EX' => $ttl]);
            if ($locked) {
                return $owner;
            }

            usleep($retryIntervalMs * 1000);
            $waited += $retryIntervalMs;
        }

        return false;
    }

    private function release(string $lockKey, string $owner): bool
    {
        $lua = <<<'LUA'
            if redis.call("GET", KEYS[1]) == ARGV[1] then
                return redis.call("DEL", KEYS[1])
            else
                return 0
            end
        LUA;

        return $this->redis->eval($lua, [$lockKey, $owner], 1) === 1;
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$lock = new FpmLock($redis);

// 场景一：缓存击穿互斥（只让一个请求查 DB）
$product = $lock->execute(
    lockKey: 'lock:cache:product:123',
    callback: function () use ($redis) {
        // Double check
        $cached = $redis->get('product:123');
        if ($cached !== false) {
            return json_decode($cached, true);
        }
        $product = queryDb(123);
        $redis->setex('product:123', 3600, json_encode($product));
        return $product;
    },
    lockTtl: 5,              // DB 查询正常 50ms，设 5s（100 倍余量）
    waitTimeoutMs: 2000,     // 最多等 2 秒
    retryIntervalMs: 50,     // 每 50ms 重试
    fallback: function () {  // 超时降级：直接查 DB（有连接池保护）
        return queryDb(123);
    }
);

// 场景二：防止订单重复创建
try {
    $orderId = $lock->execute(
        lockKey: 'lock:order:user_456:create',
        callback: function () {
            // 幂等检查 + 创建订单
            if (orderExists($userId, $productId)) {
                return getExistingOrder($userId, $productId);
            }
            return createOrder($userId, $productId);
        },
        lockTtl: 15,            // 创建订单可能涉及多次 DB 操作
        waitTimeoutMs: 0,       // 不等待！获取不到直接失败（防重复提交）
        retryIntervalMs: 0
    );
} catch (\RuntimeException $e) {
    // 获取锁失败 = 有另一个请求正在创建 = 重复提交
    return response('请勿重复提交', 429);
}

// 场景三：定时任务抢占（只有一台机器执行）
$lock->execute(
    lockKey: 'lock:cron:daily_report',
    callback: function () {
        generateDailyReport();  // 只有抢到锁的机器执行
    },
    lockTtl: 3600,             // 报告生成最多 1 小时
    waitTimeoutMs: 0,          // 不等待，抢不到就放弃（别的机器在做了）
    fallback: function () {
        // 什么都不做，静默退出
        return null;
    }
);
```

---

## 三、锁过期经典问题与完整时间线

### 3.1 问题：A 没执行完，锁过期了，B 拿到锁

```
没有 Owner 标识时：

  T=0s    A: SET lock "1" NX EX 5  → 成功 ✅
  T=3s    A: 还在查 DB...
  T=5s    锁自动过期！
  T=5.1s  B: SET lock "1" NX EX 5  → 成功 ✅（A 和 B 同时在执行！）
  T=6s    A: 查 DB 完成 → DEL lock → ⚠️ 删掉了 B 的锁！
  T=6.1s  C: SET lock "1" NX EX 5  → 成功 ✅（三个人同时执行！）

有 Owner 标识时：

  T=0s    A: SET lock "host1:pid1:abc" NX EX 5  → 成功 ✅
  T=5s    锁过期
  T=5.1s  B: SET lock "host2:pid2:def" NX EX 5  → 成功 ✅
  T=6s    A: 尝试释放 → GET lock → "host2:pid2:def" ≠ "host1:pid1:abc" → 不删 ✅
  B 的锁安全 ✅

有看门狗时：

  T=0s    A: SET lock "host1:pid1:abc" NX EX 30  → 成功 ✅，看门狗启动
  T=10s   看门狗: EXPIRE lock 30  ← TTL 重置为 30s
  T=20s   看门狗: EXPIRE lock 30  ← TTL 重置为 30s
  T=25s   A: 查 DB 完成 → release → 停止看门狗 + DEL lock
  全程锁没有过期 ✅
```

### 3.2 各方案应对总结

| 问题 | 基础版 | Owner 版 | 看门狗版 |
|------|--------|----------|----------|
| 误删别人的锁 | ❌ 会误删 | ✅ 不会 | ✅ 不会 |
| 业务超时锁过期 | ❌ 无解 | ❌ 仍会过期（但不误删） | ✅ 自动续期 |
| 进程崩溃死锁 | ✅ TTL 兜底 | ✅ TTL 兜底 | ✅ 看门狗死了 → TTL 兜底 |
| FPM 可用 | ✅ | ✅ | ❌（需要 Timer） |
| Swoole 可用 | ✅ | ✅ | ✅ |

---

## 四、面试高频题

### Q1: 如何实现一个分布式锁？

**标准答案**：
1. `SET key owner NX EX ttl` 原子获取
2. value 存全局唯一 owner 标识
3. 释放时用 Lua 脚本原子判断 + 删除
4. 看门狗定时续期防过期
5. 进程崩溃 → 看门狗死 → TTL 自然过期 → 不死锁

### Q2: 为什么释放锁要用 Lua？

因为 `GET + DEL` 两条命令不是原子的。中间可能锁过期被别人拿走，导致删掉别人的锁。Lua 在 Redis 内部单线程原子执行。

### Q3: 锁过期了但业务没执行完怎么办？

看门狗续期。每 TTL/3 检查一次，如果锁还是自己的就续期。进程崩了看门狗也没了，TTL 到期自然释放。

### Q4: Redis 主从切换会丢锁吗？

会。Master 写入锁后还没同步到 Slave 就挂了，Slave 升为 Master 后锁不存在。解决方案：
- **Redlock**：在 N 个独立 Redis 实例上加锁，多数派成功才算获取锁
- **实际生产中**：大部分场景接受极低概率的并发冲突，用 Owner + TTL 就够了

### Q5: Redlock 有什么争议？

Martin Kleppmann 的批评：
- 依赖时钟同步（如果某个节点时钟跳跃，锁可能提前过期）
- GC pause / 网络延迟可能导致锁在 check 时有效但执行时过期
- 建议用 fencing token（单调递增的令牌）保证安全

Redis 作者 antirez 的反驳：
- 实际系统时钟偏移极小
- 锁的 TTL 远大于网络延迟和 GC 时间

**面试结论**：了解争议，但实际生产中 Redlock 够用了。真正需要强一致锁的场景用 ZooKeeper/etcd。

### Q6: PHP-FPM 下没有 Timer 怎么做看门狗？

三种折中方案：
1. TTL 设大（业务最大耗时 × 3），接受锁时间长
2. 业务侧设超时（PDO timeout、cURL timeout），确保不会超过 TTL
3. 用消息队列把耗时操作异步化，缩短持锁时间

---

## 五、参考资料

- Redis 官方分布式锁文档：https://redis.io/docs/manual/patterns/distributed-locks/
- Redisson 看门狗源码（Java 参考）
- Martin Kleppmann - How to do distributed locking
- antirez - Is Redlock safe?
