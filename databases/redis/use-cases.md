# Redis 常用业务场景设计

**标签**: #redis #use-cases #系统设计 #高频
**定位**: 每个场景 = 问题 → Redis 数据结构选型 → 核心代码（PHP）→ 使用示例 → 面试追问

---

## 目录

1. [限流器](#一限流器)
2. [延迟队列](#二延迟队列)
3. [排行榜](#三排行榜)
4. [分布式 Session](#四分布式-session)
5. [点赞/计数器](#五点赞计数器)
6. [用户签到](#六用户签到bitmap)
7. [消息已读未读](#七消息已读未读)
8. [附近的人](#八附近的人geo)
9. [唯一 ID 生成器](#九唯一-id-生成器)
10. [抢红包](#十抢红包)
11. [库存扣减](#十一库存扣减)
12. [消息队列](#十二消息队列)

---

## 一、限流器

### 场景

API 接口防刷：单用户每秒最多 10 次请求，超出则返回 429。

### 方案对比

| 算法 | Redis 实现 | 特点 |
|------|-----------|------|
| 固定窗口 | INCR + EXPIRE | 简单，但窗口边界有突刺 |
| 滑动窗口 | ZSet + 时间戳 | 精确，推荐 |
| 令牌桶 | Lua 脚本 | 允许突发流量 |
| 漏桶 | List + 定速消费 | 平滑流量 |

### 实现一：滑动窗口（ZSet）

```php
<?php

declare(strict_types=1);

namespace App\RateLimit;

use Redis;

/**
 * 滑动窗口限流器（基于 ZSet）
 * 
 * 原理：
 *   ZSet 的 member = 唯一请求ID（或微秒时间戳）
 *   ZSet 的 score  = 请求时间戳（微秒）
 *   每次请求：移除窗口外的旧记录 → 统计窗口内数量 → 判断是否超限 → 添加当前请求
 *   
 * 优点：无窗口边界突刺，精确到毫秒
 * 缺点：每个用户一个 ZSet，高并发时内存占用较高
 */
class SlidingWindowRateLimiter
{
    public function __construct(private Redis $redis)
    {
    }

    /**
     * 判断是否允许请求
     * 
     * @param string $key 限流标识（如 "rate:user:1001" 或 "rate:ip:1.2.3.4"）
     * @param int $maxRequests 窗口内允许的最大请求数
     * @param int $windowSeconds 窗口大小（秒）
     * @return bool true=允许 false=拒绝
     */
    public function isAllowed(string $key, int $maxRequests, int $windowSeconds): bool
    {
        $now = microtime(true);
        $windowStart = $now - $windowSeconds;

        // Lua 保证原子性（ZREMRANGEBYSCORE + ZCARD + ZADD 三步不被打断）
        $lua = <<<'LUA'
            local key = KEYS[1]
            local window_start = tonumber(ARGV[1])
            local now = tonumber(ARGV[2])
            local max_requests = tonumber(ARGV[3])
            local window_seconds = tonumber(ARGV[4])
            local request_id = ARGV[5]

            -- 1. 移除窗口外的过期记录
            redis.call("ZREMRANGEBYSCORE", key, "-inf", window_start)

            -- 2. 统计当前窗口内的请求数
            local current_count = redis.call("ZCARD", key)

            -- 3. 判断是否超限
            if current_count < max_requests then
                -- 未超限：添加当前请求
                redis.call("ZADD", key, now, request_id)
                -- 设置 key 过期（防止无限增长）
                redis.call("EXPIRE", key, window_seconds + 1)
                return 1  -- 允许
            else
                return 0  -- 拒绝
            end
        LUA;

        // request_id 用微秒时间戳 + 随机数保证唯一
        $requestId = $now . ':' . mt_rand();

        $result = $this->redis->eval(
            $lua,
            [$key, (string) $windowStart, (string) $now, (string) $maxRequests, (string) $windowSeconds, $requestId],
            1
        );

        return $result === 1;
    }

    /**
     * 获取剩余配额
     */
    public function remaining(string $key, int $maxRequests, int $windowSeconds): int
    {
        $windowStart = microtime(true) - $windowSeconds;
        $this->redis->zRemRangeByScore($key, '-inf', (string) $windowStart);
        $current = $this->redis->zCard($key);
        return max(0, $maxRequests - $current);
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$limiter = new SlidingWindowRateLimiter($redis);

// 场景：API 限流 — 每用户每秒最多 10 次
$userId = 1001;
$key = "rate:api:user:{$userId}";

if ($limiter->isAllowed($key, maxRequests: 10, windowSeconds: 1)) {
    // 正常处理请求
    echo "允许\n";
} else {
    // 返回 429 Too Many Requests
    http_response_code(429);
    echo "限流中\n";
}

// 场景：短信验证码 — 同一手机号每天最多 5 次
$phone = '13800138000';
$key = "rate:sms:{$phone}";
$allowed = $limiter->isAllowed($key, maxRequests: 5, windowSeconds: 86400);

// 查看剩余配额
$remaining = $limiter->remaining("rate:api:user:1001", 10, 1);
echo "本窗口剩余 {$remaining} 次\n";
```

### 实现二：令牌桶（Lua）

```php
<?php

declare(strict_types=1);

namespace App\RateLimit;

use Redis;

/**
 * 令牌桶限流器
 * 
 * 原理：
 *   桶中有固定容量的令牌，以固定速率补充。
 *   每个请求消耗一个令牌，没有令牌则拒绝。
 *   允许短时间的突发流量（桶满时可一次性消耗多个令牌）。
 *
 * 存储：用 Redis Hash 存 {tokens: 当前令牌数, last_time: 上次填充时间}
 */
class TokenBucketLimiter
{
    public function __construct(private Redis $redis)
    {
    }

    /**
     * @param string $key 限流标识
     * @param int $capacity 桶容量（最大突发量）
     * @param float $rate 令牌填充速率（个/秒）
     * @param int $requested 本次请求消耗的令牌数（默认 1）
     */
    public function isAllowed(string $key, int $capacity, float $rate, int $requested = 1): bool
    {
        $lua = <<<'LUA'
            local key = KEYS[1]
            local capacity = tonumber(ARGV[1])
            local rate = tonumber(ARGV[2])
            local now = tonumber(ARGV[3])
            local requested = tonumber(ARGV[4])

            -- 获取当前状态
            local data = redis.call("HMGET", key, "tokens", "last_time")
            local tokens = tonumber(data[1]) or capacity  -- 首次默认满桶
            local last_time = tonumber(data[2]) or now

            -- 计算自上次以来应该补充的令牌数
            local elapsed = now - last_time
            local new_tokens = math.min(capacity, tokens + elapsed * rate)

            -- 判断令牌是否足够
            if new_tokens >= requested then
                new_tokens = new_tokens - requested
                redis.call("HMSET", key, "tokens", new_tokens, "last_time", now)
                redis.call("EXPIRE", key, math.ceil(capacity / rate) + 1)
                return 1  -- 允许
            else
                -- 令牌不够，更新补充时间（下次计算继续累积）
                redis.call("HMSET", key, "tokens", new_tokens, "last_time", now)
                redis.call("EXPIRE", key, math.ceil(capacity / rate) + 1)
                return 0  -- 拒绝
            end
        LUA;

        $result = $this->redis->eval(
            $lua,
            [$key, (string) $capacity, (string) $rate, (string) microtime(true), (string) $requested],
            1
        );

        return $result === 1;
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$bucket = new TokenBucketLimiter($redis);

// 场景：API 限流 — 容量 100（允许突发），每秒补充 10 个令牌
$allowed = $bucket->isAllowed(
    key: 'bucket:api:user:1001',
    capacity: 100,    // 桶容量：最多积攒 100 个令牌
    rate: 10,         // 每秒补充 10 个
    requested: 1      // 本次消耗 1 个
);

// 场景：大文件上传 — 每次消耗 10 个令牌（限制大请求频率）
$allowed = $bucket->isAllowed('bucket:upload:user:1001', 50, 5, 10);
```

---

## 二、延迟队列

### 场景

- 订单 30 分钟未支付自动取消
- 优惠券到期前 1 小时提醒
- 延迟重试失败的消息

### 方案

用 ZSet：member = 任务 ID，score = 执行时间戳。轮询取到期任务。

```php
<?php

declare(strict_types=1);

namespace App\Queue;

use Redis;

/**
 * Redis ZSet 延迟队列
 * 
 * 原理：
 *   ZADD queue score member → score 存执行时间戳
 *   ZRANGEBYSCORE queue 0 now LIMIT 0 1 → 取到期任务
 *   Lua 原子取 + 删（防止多个消费者重复消费）
 */
class DelayQueue
{
    private const KEY_PREFIX = 'delay_queue:';

    public function __construct(private Redis $redis)
    {
    }

    /**
     * 添加延迟任务
     * 
     * @param string $queue 队列名
     * @param string $taskId 任务 ID（唯一标识，用于幂等）
     * @param array $payload 任务数据
     * @param int $delaySeconds 延迟时间（秒）
     */
    public function addTask(string $queue, string $taskId, array $payload, int $delaySeconds): void
    {
        $executeAt = time() + $delaySeconds;
        $key = self::KEY_PREFIX . $queue;
        $dataKey = $key . ':data';

        // ZSet 存执行时间
        $this->redis->zAdd($key, $executeAt, $taskId);
        // Hash 存任务详情
        $this->redis->hSet($dataKey, $taskId, json_encode($payload));
    }

    /**
     * 消费到期任务（原子取 + 删，防重复消费）
     * 
     * @param string $queue 队列名
     * @param int $batchSize 每次取多少个
     * @return array 到期任务列表 [{id, payload}, ...]
     */
    public function consumeReady(string $queue, int $batchSize = 10): array
    {
        $key = self::KEY_PREFIX . $queue;
        $dataKey = $key . ':data';
        $now = time();

        // Lua 原子操作：取到期任务 + 从 ZSet 删除（避免多消费者重复取）
        $lua = <<<'LUA'
            local key = KEYS[1]
            local now = tonumber(ARGV[1])
            local batch = tonumber(ARGV[2])

            local tasks = redis.call("ZRANGEBYSCORE", key, "-inf", now, "LIMIT", 0, batch)
            if #tasks > 0 then
                redis.call("ZREM", key, unpack(tasks))
            end
            return tasks
        LUA;

        $taskIds = $this->redis->eval($lua, [$key, (string) $now, (string) $batchSize], 1);

        if (empty($taskIds)) {
            return [];
        }

        // 获取任务详情
        $tasks = [];
        foreach ($taskIds as $taskId) {
            $payload = $this->redis->hGet($dataKey, $taskId);
            if ($payload !== false) {
                $tasks[] = [
                    'id' => $taskId,
                    'payload' => json_decode($payload, true),
                ];
                $this->redis->hDel($dataKey, $taskId); // 清理详情
            }
        }

        return $tasks;
    }

    /**
     * 取消任务
     */
    public function cancelTask(string $queue, string $taskId): bool
    {
        $key = self::KEY_PREFIX . $queue;
        $dataKey = $key . ':data';

        $removed = $this->redis->zRem($key, $taskId);
        $this->redis->hDel($dataKey, $taskId);
        return $removed > 0;
    }

    /**
     * 查看队列中待执行任务数
     */
    public function pendingCount(string $queue): int
    {
        return $this->redis->zCard(self::KEY_PREFIX . $queue);
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$queue = new DelayQueue($redis);

// 1. 添加延迟任务：订单 30 分钟后自动取消
$queue->addTask(
    queue: 'order_cancel',
    taskId: 'order:10001',
    payload: ['order_id' => 10001, 'user_id' => 1001, 'action' => 'cancel'],
    delaySeconds: 1800  // 30 分钟
);

// 2. 添加延迟任务：5 秒后重试发送通知
$queue->addTask('notification_retry', 'notify:abc123', ['type' => 'sms', 'to' => '138xxx'], 5);

// 3. 用户支付了 → 取消自动关单任务
$queue->cancelTask('order_cancel', 'order:10001');

// 4. 消费者循环（在独立进程/协程中运行）
while (true) {
    $tasks = $queue->consumeReady('order_cancel', batchSize: 20);
    foreach ($tasks as $task) {
        echo "执行任务: {$task['id']}\n";
        // 关闭订单逻辑...
        cancelOrder($task['payload']['order_id']);
    }
    usleep(500000); // 500ms 轮询间隔
}
```

---

## 三、排行榜

### 场景

游戏积分榜、商品销量榜、文章热度榜。

### 方案

ZSet 天然适合：member = 用户ID，score = 分数。自动排序。

```php
<?php

declare(strict_types=1);

namespace App\Leaderboard;

use Redis;

/**
 * Redis ZSet 排行榜
 */
class Leaderboard
{
    public function __construct(
        private Redis $redis,
        private string $key = 'leaderboard:game'
    ) {
    }

    /** 更新分数（原子增加） */
    public function addScore(string $memberId, float $score): float
    {
        return $this->redis->zIncrBy($this->key, $score, $memberId);
    }

    /** 设置分数（覆盖） */
    public function setScore(string $memberId, float $score): void
    {
        $this->redis->zAdd($this->key, $score, $memberId);
    }

    /** 获取 Top N（从高到低） */
    public function getTopN(int $n): array
    {
        // ZREVRANGEBYSCORE 返回 member => score
        return $this->redis->zRevRange($this->key, 0, $n - 1, true);
    }

    /** 获取某用户排名（从 0 开始，0 = 第一名） */
    public function getRank(string $memberId): ?int
    {
        $rank = $this->redis->zRevRank($this->key, $memberId);
        return $rank === false ? null : $rank;
    }

    /** 获取某用户分数 */
    public function getScore(string $memberId): ?float
    {
        $score = $this->redis->zScore($this->key, $memberId);
        return $score === false ? null : $score;
    }

    /** 获取某用户排名 + 分数 + 前后 N 名 */
    public function getAroundMe(string $memberId, int $range = 5): array
    {
        $rank = $this->getRank($memberId);
        if ($rank === null) return [];

        $start = max(0, $rank - $range);
        $end = $rank + $range;

        return $this->redis->zRevRange($this->key, $start, $end, true);
    }

    /** 获取排行榜总人数 */
    public function totalMembers(): int
    {
        return $this->redis->zCard($this->key);
    }

    /** 移除用户 */
    public function removeMember(string $memberId): void
    {
        $this->redis->zRem($this->key, $memberId);
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$board = new Leaderboard($redis, 'leaderboard:weekly_sales');

// 更新销售额
$board->addScore('shop:1001', 5000);
$board->addScore('shop:1002', 8000);
$board->addScore('shop:1003', 3200);
$board->addScore('shop:1001', 2000); // 累加 → 7000

// 获取 Top 3
$top3 = $board->getTopN(3);
// ['shop:1002' => 8000, 'shop:1001' => 7000, 'shop:1003' => 3200]

// 我的排名
$rank = $board->getRank('shop:1001'); // 1（第二名，从 0 开始）

// 我周围的人
$around = $board->getAroundMe('shop:1001', 2);
```

---

## 四、分布式 Session

### 场景

多台服务器共享用户登录状态，不依赖单机文件 Session。

### 方案

Session 数据存 Redis Hash，Session ID 通过 Cookie 传递。

```php
<?php

declare(strict_types=1);

namespace App\Session;

use Redis;

/**
 * Redis 分布式 Session
 */
class RedisSession
{
    private const PREFIX = 'session:';
    private const DEFAULT_TTL = 7200; // 2 小时

    private Redis $redis;
    private string $sessionId;
    private int $ttl;

    public function __construct(Redis $redis, ?string $sessionId = null, int $ttl = self::DEFAULT_TTL)
    {
        $this->redis = $redis;
        $this->ttl = $ttl;
        $this->sessionId = $sessionId ?? $this->generateId();
    }

    /** 生成安全的 Session ID */
    private function generateId(): string
    {
        return bin2hex(random_bytes(32)); // 64 字符十六进制
    }

    public function getId(): string
    {
        return $this->sessionId;
    }

    /** 设置 Session 值 */
    public function set(string $field, mixed $value): void
    {
        $key = self::PREFIX . $this->sessionId;
        $this->redis->hSet($key, $field, json_encode($value));
        $this->redis->expire($key, $this->ttl); // 续期
    }

    /** 获取 Session 值 */
    public function get(string $field): mixed
    {
        $key = self::PREFIX . $this->sessionId;
        $value = $this->redis->hGet($key, $field);
        return $value !== false ? json_decode($value, true) : null;
    }

    /** 获取所有 Session 数据 */
    public function all(): array
    {
        $key = self::PREFIX . $this->sessionId;
        $data = $this->redis->hGetAll($key);
        return array_map(fn($v) => json_decode($v, true), $data);
    }

    /** 删除某个字段 */
    public function forget(string $field): void
    {
        $this->redis->hDel(self::PREFIX . $this->sessionId, $field);
    }

    /** 销毁整个 Session */
    public function destroy(): void
    {
        $this->redis->del(self::PREFIX . $this->sessionId);
    }

    /** 续期 */
    public function touch(): void
    {
        $this->redis->expire(self::PREFIX . $this->sessionId, $this->ttl);
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

// 登录时创建 Session
$session = new RedisSession($redis);
$session->set('user_id', 1001);
$session->set('username', 'Jake');
$session->set('role', 'admin');

// 返回 Session ID 给客户端（通过 Cookie）
setcookie('SESSION_ID', $session->getId(), time() + 7200, '/', '', true, true);

// 后续请求：从 Cookie 取 Session ID → 恢复 Session
$sessionId = $_COOKIE['SESSION_ID'] ?? null;
if ($sessionId) {
    $session = new RedisSession($redis, $sessionId);
    $userId = $session->get('user_id'); // 1001
    $session->touch(); // 续期
}

// 登出
$session->destroy();
```

---

## 五、点赞/计数器

### 场景

文章点赞数、视频播放量、页面 UV/PV。

### 方案

- 计数：String INCR（原子自增）
- 点赞去重：Set（记录谁点了赞）
- 大规模 UV：HyperLogLog（近似去重计数，误差 0.81%）

```php
<?php

declare(strict_types=1);

namespace App\Counter;

use Redis;

/**
 * 点赞 + 计数系统
 */
class LikeCounter
{
    public function __construct(private Redis $redis)
    {
    }

    // ─── 精确点赞（Set 去重）───

    /** 点赞 */
    public function like(string $targetId, string $userId): bool
    {
        $key = "likes:{$targetId}";
        $added = $this->redis->sAdd($key, $userId);
        return $added > 0; // true=新点赞 false=已点过
    }

    /** 取消赞 */
    public function unlike(string $targetId, string $userId): bool
    {
        return $this->redis->sRem($key = "likes:{$targetId}", $userId) > 0;
    }

    /** 是否已点赞 */
    public function isLiked(string $targetId, string $userId): bool
    {
        return $this->redis->sIsMember("likes:{$targetId}", $userId);
    }

    /** 点赞总数 */
    public function likeCount(string $targetId): int
    {
        return $this->redis->sCard("likes:{$targetId}");
    }

    // ─── 高性能计数器（INCR，不去重）───

    /** 增加浏览量 */
    public function incrementView(string $targetId): int
    {
        return $this->redis->incr("views:{$targetId}");
    }

    /** 获取浏览量 */
    public function getViews(string $targetId): int
    {
        return (int) $this->redis->get("views:{$targetId}");
    }

    // ─── 大规模 UV 统计（HyperLogLog，近似去重）───

    /** 记录 UV */
    public function recordUv(string $page, string $userId): void
    {
        $key = "uv:{$page}:" . date('Y-m-d');
        $this->redis->pfAdd($key, [$userId]);
    }

    /** 获取 UV（近似值，误差 0.81%） */
    public function getUv(string $page): int
    {
        $key = "uv:{$page}:" . date('Y-m-d');
        return $this->redis->pfCount([$key]);
    }

    /** 合并多天 UV */
    public function mergeUv(string $page, array $dates): int
    {
        $keys = array_map(fn($d) => "uv:{$page}:{$d}", $dates);
        $destKey = "uv:{$page}:merged";
        $this->redis->pfMerge($destKey, $keys);
        return $this->redis->pfCount([$destKey]);
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$counter = new LikeCounter($redis);

// 点赞
$counter->like('article:2001', 'user:1001');  // true
$counter->like('article:2001', 'user:1001');  // false（已点过）
$counter->like('article:2001', 'user:1002');  // true

echo $counter->likeCount('article:2001');    // 2
echo $counter->isLiked('article:2001', 'user:1001') ? '已赞' : '未赞';

// 浏览量
$counter->incrementView('article:2001');     // 原子 +1
echo $counter->getViews('article:2001');     // 1

// UV 统计（百万级用户也只占 12KB 内存）
$counter->recordUv('homepage', 'user:1001');
$counter->recordUv('homepage', 'user:1002');
$counter->recordUv('homepage', 'user:1001'); // 重复，不计数
echo $counter->getUv('homepage');           // 2
```

---

## 六、用户签到（Bitmap）

### 场景

记录用户每天是否签到，统计连续签到天数、月签到次数。

### 方案

Bitmap：一个 bit 代表一天。一个用户一年只需 365 bit = 46 字节。

```php
<?php

declare(strict_types=1);

namespace App\Sign;

use Redis;

/**
 * Bitmap 签到系统
 * 
 * key: sign:{userId}:{yyyyMM}
 * offset: 日期 - 1（0~30）
 * value: 0=未签到 1=已签到
 */
class SignIn
{
    public function __construct(private Redis $redis)
    {
    }

    /** 签到 */
    public function sign(string $userId, ?string $date = null): bool
    {
        $date = $date ?? date('Y-m-d');
        [$key, $offset] = $this->getKeyAndOffset($userId, $date);

        // SETBIT 返回旧值：0=之前没签（本次签到成功），1=已签过
        $oldValue = $this->redis->setBit($key, $offset, 1);
        return $oldValue === 0; // true=签到成功 false=重复签到
    }

    /** 查询某天是否签到 */
    public function isSigned(string $userId, string $date): bool
    {
        [$key, $offset] = $this->getKeyAndOffset($userId, $date);
        return $this->redis->getBit($key, $offset) === 1;
    }

    /** 本月签到次数 */
    public function monthlyCount(string $userId, ?string $yearMonth = null): int
    {
        $yearMonth = $yearMonth ?? date('Y-m');
        $key = "sign:{$userId}:{$yearMonth}";
        return $this->redis->bitCount($key);
    }

    /** 本月连续签到天数（从今天往回数） */
    public function consecutiveDays(string $userId): int
    {
        $key = "sign:{$userId}:" . date('Y-m');
        $today = (int) date('j'); // 今天是几号

        // 获取从第 0 位到今天的所有 bit
        // BITFIELD 获取指定范围的整数
        $bits = $this->redis->rawCommand('BITFIELD', $key, 'GET', "u{$today}", '0');

        if (empty($bits) || $bits[0] === 0) {
            return 0;
        }

        $value = $bits[0];
        $count = 0;

        // 从最低位开始，连续 1 的个数
        while (($value & 1) === 1) {
            $count++;
            $value >>= 1;
        }

        return $count;
    }

    private function getKeyAndOffset(string $userId, string $date): array
    {
        $yearMonth = substr($date, 0, 7); // "2025-01"
        $day = (int) substr($date, 8, 2); // 日期
        $key = "sign:{$userId}:{$yearMonth}";
        $offset = $day - 1; // 1号 → offset 0
        return [$key, $offset];
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$sign = new SignIn($redis);

// 签到
$sign->sign('user:1001');                     // true（首次）
$sign->sign('user:1001');                     // false（重复）
$sign->sign('user:1001', '2025-01-15');       // 补签

// 查询
echo $sign->isSigned('user:1001', '2025-01-15') ? '已签' : '未签';
echo "本月签到 {$sign->monthlyCount('user:1001')} 天\n";
echo "连续签到 {$sign->consecutiveDays('user:1001')} 天\n";

// 内存占用：100 万用户 × 12 个月 × 4 字节/月 ≈ 46 MB（极低）
```

---

## 七、消息已读未读

### 场景

IM 消息已读标记、通知中心未读数。

### 方案

- 单条消息已读：Bitmap（offset = 用户ID）
- 未读计数：String INCR / DECR

```php
<?php

declare(strict_types=1);

namespace App\Message;

use Redis;

class MessageReadStatus
{
    public function __construct(private Redis $redis)
    {
    }

    /** 标记已读（某用户读了某消息） */
    public function markRead(string $messageId, int $userId): void
    {
        $this->redis->setBit("msg_read:{$messageId}", $userId, 1);
        // 同时减少未读计数
        $this->redis->decr("unread:{$userId}");
    }

    /** 是否已读 */
    public function isRead(string $messageId, int $userId): bool
    {
        return $this->redis->getBit("msg_read:{$messageId}", $userId) === 1;
    }

    /** 消息的已读人数 */
    public function readCount(string $messageId): int
    {
        return $this->redis->bitCount("msg_read:{$messageId}");
    }

    /** 增加未读数 */
    public function incrementUnread(int $userId, int $count = 1): int
    {
        return $this->redis->incrBy("unread:{$userId}", $count);
    }

    /** 获取未读数 */
    public function getUnreadCount(int $userId): int
    {
        return (int) $this->redis->get("unread:{$userId}");
    }

    /** 清零未读 */
    public function clearUnread(int $userId): void
    {
        $this->redis->set("unread:{$userId}", '0');
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$msg = new MessageReadStatus($redis);

// 发消息时：给接收者增加未读数
$msg->incrementUnread(userId: 1001);
$msg->incrementUnread(userId: 1002);

// 用户打开消息：标记已读 + 未读数 -1
$msg->markRead('msg:5001', userId: 1001);

// 查询
echo $msg->getUnreadCount(1001);              // 未读数
echo $msg->isRead('msg:5001', 1001) ? '已读' : '未读';
echo "已读人数: {$msg->readCount('msg:5001')}\n";
```

---

## 八、附近的人（GEO）

### 场景

查找附近 5km 内的门店、外卖骑手、司机。

### 方案

Redis GEO（底层是 ZSet + GeoHash 编码）。

```php
<?php

declare(strict_types=1);

namespace App\Geo;

use Redis;

class NearbyService
{
    public function __construct(
        private Redis $redis,
        private string $key = 'geo:stores'
    ) {
    }

    /** 添加位置 */
    public function addLocation(string $memberId, float $longitude, float $latitude): void
    {
        $this->redis->geoAdd($this->key, $longitude, $latitude, $memberId);
    }

    /** 查找附近的人/店 */
    public function findNearby(float $lng, float $lat, float $radiusKm, int $limit = 20): array
    {
        // GEORADIUS 返回指定坐标附近的 member + 距离
        return $this->redis->geoRadius(
            $this->key,
            $lng,
            $lat,
            $radiusKm,
            'km',
            ['WITHDIST', 'ASC', 'COUNT' => $limit]
        );
    }

    /** 两点之间距离 */
    public function distance(string $member1, string $member2): ?float
    {
        $dist = $this->redis->geoDist($this->key, $member1, $member2, 'km');
        return $dist !== false ? (float) $dist : null;
    }

    /** 获取某成员坐标 */
    public function getPosition(string $memberId): ?array
    {
        $pos = $this->redis->geoPos($this->key, $memberId);
        return $pos[0] ?? null; // [lng, lat]
    }

    /** 移除位置 */
    public function removeLocation(string $memberId): void
    {
        $this->redis->zRem($this->key, $memberId);
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$geo = new NearbyService($redis, 'geo:riders');

// 骑手上报位置
$geo->addLocation('rider:1001', 116.403963, 39.915119); // 天安门
$geo->addLocation('rider:1002', 116.410000, 39.920000); // 附近
$geo->addLocation('rider:1003', 121.474000, 31.230000); // 上海（很远）

// 查找附近 3km 的骑手
$nearby = $geo->findNearby(116.405000, 39.916000, 3);
// [['rider:1001', '0.15'], ['rider:1002', '0.73']]（带距离 km）

// 两骑手之间距离
$dist = $geo->distance('rider:1001', 'rider:1002'); // 0.xx km
```

---

## 九、唯一 ID 生成器

### 场景

分布式环境下生成全局唯一、递增的 ID（订单号、流水号）。

### 方案

Redis INCR 原子自增 + 时间戳前缀 = 有序唯一 ID。

```php
<?php

declare(strict_types=1);

namespace App\Id;

use Redis;

/**
 * Redis 分布式 ID 生成器
 * 
 * 结构：时间戳(32bit) + 序列号(32bit) = 64bit 整数
 *   - 时间戳：秒级（相对于某个基准时间）
 *   - 序列号：同一秒内的自增序号（每秒最多 2^32 个）
 * 
 * 特点：有序、唯一、高性能（单 Redis 10万+ ID/s）
 */
class RedisIdGenerator
{
    // 基准时间戳（2024-01-01 00:00:00 UTC）
    private const EPOCH = 1704067200;
    private const SEQUENCE_BITS = 32;

    public function __construct(private Redis $redis)
    {
    }

    /**
     * 生成唯一 ID
     * 
     * @param string $bizTag 业务标识（不同业务用不同计数器）
     */
    public function nextId(string $bizTag): int
    {
        // 当前时间戳（相对于 EPOCH）
        $timestamp = time() - self::EPOCH;

        // Redis INCR 获取同一秒内的序列号
        $key = "id:{$bizTag}:" . date('Y-m-d');
        $sequence = $this->redis->incr($key);

        // 设置过期时间（防止 key 无限增长）
        if ($sequence === 1) {
            $this->redis->expire($key, 86400 * 2); // 2 天后过期
        }

        // 拼接：时间戳左移 32 位 + 序列号
        return ($timestamp << self::SEQUENCE_BITS) | $sequence;
    }

    /**
     * 生成可读的业务单号
     * 格式：前缀 + 日期 + 序列号（补零）
     */
    public function nextBizNo(string $prefix, string $bizTag): string
    {
        $key = "id:{$bizTag}:" . date('Ymd');
        $sequence = $this->redis->incr($key);

        if ($sequence === 1) {
            $this->redis->expire($key, 86400 * 2);
        }

        // 如：ORD202501180000001
        return $prefix . date('Ymd') . str_pad((string) $sequence, 7, '0', STR_PAD_LEFT);
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$idGen = new RedisIdGenerator($redis);

// 生成数字 ID（适合数据库主键）
$orderId = $idGen->nextId('order');     // 如：7234567890001
$userId = $idGen->nextId('user');       // 如：7234567890001（不同业务独立计数）

// 生成业务单号（可读）
$orderNo = $idGen->nextBizNo('ORD', 'order');  // "ORD202501180000001"
$payNo = $idGen->nextBizNo('PAY', 'payment');  // "PAY202501180000001"
```

---

## 十、抢红包

### 场景

群里发红包，N 个人抢 M 元。要求：不超发、不少发、金额随机、高并发安全。

### 方案

预分配 + List：发红包时预先算好每份金额存入 List，抢红包 = LPOP（原子操作）。

```php
<?php

declare(strict_types=1);

namespace App\RedPacket;

use Redis;

class RedPacket
{
    public function __construct(private Redis $redis)
    {
    }

    /**
     * 发红包（预分配金额）
     * 
     * @param string $packetId 红包 ID
     * @param int $totalAmount 总金额（分）
     * @param int $totalCount 总个数
     * @param int $expireSeconds 过期时间
     */
    public function create(string $packetId, int $totalAmount, int $totalCount, int $expireSeconds = 86400): void
    {
        // 二倍均值法预分配金额
        $amounts = $this->splitAmount($totalAmount, $totalCount);

        $key = "redpacket:{$packetId}";

        // 用 Pipeline 批量 RPUSH
        $pipe = $this->redis->pipeline();
        foreach ($amounts as $amount) {
            $pipe->rPush($key, (string) $amount);
        }
        $pipe->expire($key, $expireSeconds);
        $pipe->exec();

        // 记录红包信息
        $this->redis->hMSet("redpacket:info:{$packetId}", [
            'total_amount' => $totalAmount,
            'total_count' => $totalCount,
            'remain_count' => $totalCount,
            'created_at' => time(),
        ]);
        $this->redis->expire("redpacket:info:{$packetId}", $expireSeconds);
    }

    /**
     * 抢红包
     * 
     * @return int|null 抢到的金额（分），null 表示没抢到
     */
    public function grab(string $packetId, string $userId): ?int
    {
        // Lua 原子操作：检查是否抢过 + LPOP + 记录
        $lua = <<<'LUA'
            local key = KEYS[1]
            local user_key = KEYS[2]
            local user_id = ARGV[1]

            -- 检查是否已抢过
            if redis.call("SISMEMBER", user_key, user_id) == 1 then
                return -1  -- 已抢过
            end

            -- 取一个红包
            local amount = redis.call("LPOP", key)
            if not amount then
                return -2  -- 红包已抢完
            end

            -- 记录已抢用户
            redis.call("SADD", user_key, user_id)

            return tonumber(amount)
        LUA;

        $key = "redpacket:{$packetId}";
        $userKey = "redpacket:users:{$packetId}";

        $result = $this->redis->eval($lua, [$key, $userKey, $userId], 2);

        if ($result === -1) return null;  // 已抢过
        if ($result === -2) return null;  // 抢完了
        return (int) $result;
    }

    /**
     * 二倍均值法分配金额
     * 保证每份金额在合理范围内且总和精确
     */
    private function splitAmount(int $totalAmount, int $totalCount): array
    {
        $amounts = [];
        $remaining = $totalAmount;

        for ($i = 0; $i < $totalCount - 1; $i++) {
            $remainCount = $totalCount - $i;
            // 最少 1 分，最多 2 倍均值
            $max = (int) floor($remaining / $remainCount * 2);
            $amount = random_int(1, max(1, $max));
            $amounts[] = $amount;
            $remaining -= $amount;
        }
        $amounts[] = $remaining; // 最后一份 = 剩余

        shuffle($amounts); // 打乱顺序
        return $amounts;
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$rp = new RedPacket($redis);

// 发红包：100 元（10000 分），分 10 个
$rp->create('rp_20250118_001', totalAmount: 10000, totalCount: 10);

// 抢红包
$amount = $rp->grab('rp_20250118_001', 'user:1001'); // 如 1523（15.23 元）
$amount = $rp->grab('rp_20250118_001', 'user:1001'); // null（已抢过）
$amount = $rp->grab('rp_20250118_001', 'user:1002'); // 如 832（8.32 元）
```

---

## 十一、库存扣减

### 场景

秒杀/下单时扣库存，防止超卖。

### 方案

Lua 原子操作：判断库存 + 扣减在同一条 Lua 中完成。

```php
<?php

declare(strict_types=1);

namespace App\Stock;

use Redis;

class StockService
{
    public function __construct(private Redis $redis)
    {
    }

    /** 初始化库存 */
    public function setStock(string $skuId, int $quantity): void
    {
        $this->redis->set("stock:{$skuId}", (string) $quantity);
    }

    /**
     * 原子扣减库存
     * 
     * @return int 扣减后的库存（-1 表示库存不足，-2 表示商品不存在）
     */
    public function decrStock(string $skuId, int $quantity = 1): int
    {
        $lua = <<<'LUA'
            local key = KEYS[1]
            local quantity = tonumber(ARGV[1])

            local stock = redis.call("GET", key)
            if not stock then
                return -2  -- key 不存在
            end

            stock = tonumber(stock)
            if stock < quantity then
                return -1  -- 库存不足
            end

            -- 扣减并返回新库存
            local new_stock = stock - quantity
            redis.call("SET", key, new_stock)
            return new_stock
        LUA;

        return (int) $this->redis->eval($lua, ["stock:{$skuId}", (string) $quantity], 1);
    }

    /** 回滚库存（支付超时 / 取消订单） */
    public function incrStock(string $skuId, int $quantity = 1): int
    {
        return $this->redis->incrBy("stock:{$skuId}", $quantity);
    }

    /** 查询当前库存 */
    public function getStock(string $skuId): int
    {
        return (int) $this->redis->get("stock:{$skuId}");
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$stock = new StockService($redis);

// 初始化库存
$stock->setStock('sku_iphone16', 100);

// 下单扣库存（高并发安全）
$result = $stock->decrStock('sku_iphone16', 1);
if ($result >= 0) {
    echo "扣减成功，剩余库存: {$result}\n";
    // 继续创建订单...
} elseif ($result === -1) {
    echo "库存不足\n";
}

// 取消订单回滚库存
$stock->incrStock('sku_iphone16', 1);
```

---

## 十二、消息队列

### 场景

轻量级异步任务、事件通知。不想引入 Kafka/RabbitMQ 的简单场景。

### 方案对比

| 方式 | 数据结构 | 特点 | 适用 |
|------|----------|------|------|
| List (LPUSH/BRPOP) | List | 最简单，先进先出 | 简单任务队列 |
| Pub/Sub | 频道 | 广播，不持久化 | 实时通知 |
| Stream (XADD/XREAD) | Stream | 持久化、消费者组、ACK | **推荐**（Redis 5.0+） |

### 实现：Stream 消息队列

```php
<?php

declare(strict_types=1);

namespace App\MQ;

use Redis;

/**
 * Redis Stream 消息队列
 * 
 * 相比 List：
 *   - 支持消费者组（多消费者竞争消费）
 *   - 支持 ACK 确认（消费失败可重试）
 *   - 持久化（不丢消息）
 *   - 支持回溯历史消息
 */
class RedisStreamQueue
{
    public function __construct(private Redis $redis)
    {
    }

    /** 发布消息 */
    public function publish(string $stream, array $message): string
    {
        // XADD stream * field value → 返回消息 ID（如 "1705555555555-0"）
        return $this->redis->xAdd($stream, '*', $message);
    }

    /** 创建消费者组 */
    public function createGroup(string $stream, string $group, string $startId = '0'): void
    {
        try {
            $this->redis->xGroup('CREATE', $stream, $group, $startId, true); // MKSTREAM
        } catch (\RedisException $e) {
            // 组已存在忽略
            if (!str_contains($e->getMessage(), 'BUSYGROUP')) {
                throw $e;
            }
        }
    }

    /**
     * 消费消息（消费者组模式）
     * 
     * @param string $stream 流名
     * @param string $group 消费者组
     * @param string $consumer 消费者名（同组内唯一）
     * @param int $count 每次读取数量
     * @param int $blockMs 阻塞等待毫秒（0=不阻塞）
     */
    public function consume(string $stream, string $group, string $consumer, int $count = 10, int $blockMs = 2000): array
    {
        $result = $this->redis->xReadGroup($group, $consumer, [$stream => '>'], $count, $blockMs);
        return $result[$stream] ?? [];
    }

    /** 确认消息已处理 */
    public function ack(string $stream, string $group, string $messageId): void
    {
        $this->redis->xAck($stream, $group, [$messageId]);
    }

    /** 获取待处理消息（未 ACK 的） */
    public function pending(string $stream, string $group, int $count = 10): array
    {
        return $this->redis->xPending($stream, $group, '-', '+', $count);
    }

    /** 裁剪流长度（防止无限增长） */
    public function trim(string $stream, int $maxLen): void
    {
        $this->redis->xTrim($stream, $maxLen, true); // MAXLEN ~
    }
}

// ====================================================================
// 使用示例
// ====================================================================

$redis = new Redis();
$redis->connect('127.0.0.1', 6379);

$mq = new RedisStreamQueue($redis);

// 1. 创建消费者组
$mq->createGroup('stream:orders', 'order_processors');

// 2. 生产者发布消息
$msgId = $mq->publish('stream:orders', [
    'order_id' => '10001',
    'action' => 'created',
    'user_id' => '1001',
]);
echo "Published: {$msgId}\n";

// 3. 消费者读取消息（在独立进程中循环运行）
while (true) {
    $messages = $mq->consume(
        stream: 'stream:orders',
        group: 'order_processors',
        consumer: 'worker-1',  // 同组多消费者竞争
        count: 5,
        blockMs: 2000          // 无消息时阻塞 2 秒
    );

    foreach ($messages as $msgId => $data) {
        echo "Processing: {$msgId} → order={$data['order_id']}\n";

        // 处理业务逻辑...
        processOrder($data);

        // 确认消费完成
        $mq->ack('stream:orders', 'order_processors', $msgId);
    }
}

// 4. 定期裁剪（保留最近 10000 条）
$mq->trim('stream:orders', 10000);
```

---

## 面试综合题

### Q1: 为什么用 ZSet 做延迟队列而不是 List？

List 只能先进先出，无法按"执行时间"排序。ZSet 的 score 天然支持按时间排序 + 范围查询。

### Q2: HyperLogLog 为什么只占 12KB 就能统计亿级 UV？

基于概率算法（LogLog Counting 变种），用有限寄存器记录"最大前导零"来估计基数。精度换空间。

### Q3: Bitmap 签到为什么比 Set 好？

100 万用户一年签到：Bitmap = 100万 × 46字节 ≈ 44 MB。Set = 100万 × 365 × 成员 ≈ 几 GB。内存差距 100 倍。

### Q4: GEO 底层为什么用 ZSet？

GEO 把经纬度编码成 GeoHash（52bit 整数），存为 ZSet 的 score。范围查询 = score 范围查询。

### Q5: 库存扣减为什么用 Lua 不用 WATCH + MULTI？

WATCH/MULTI 是乐观锁，高并发下冲突率高（大量重试）。Lua 是原子执行，无冲突，一次搞定。

### Q6: Redis Stream 相比 Kafka 适合什么场景？

轻量级、低延迟、消息量不大（万级/s）。不需要 TB 级存储、跨机房复制、schema 管理时，Stream 够用且运维简单。
