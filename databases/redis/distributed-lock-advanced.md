# Redis 分布式锁 · 高级面试题

**标签**: #redis #distributed-lock #高级 #系统设计 #八股升级
**难度**: ⭐⭐⭐⭐⭐
**前置阅读**: [distributed-lock.md](./distributed-lock.md)

> 本文只讨论 **进阶话题**：基础的 `SET NX EX`、Lua 释放、Owner 标识、看门狗等请回到基础篇。这里的每一道题都是二面/三面/架构面的高频拷打点。

---

## 目录

1. [Redlock 深度剖析（Martin vs antirez）](#一redlock-深度剖析martin-vs-antirez)
2. [Fencing Token —— 真正安全的护身符](#二fencing-token--真正安全的护身符)
3. [GC Pause / STW 对分布式锁的致命影响](#三gc-pause--stw-对分布式锁的致命影响)
4. [时钟漂移与 NTP 跳跃](#四时钟漂移与-ntp-跳跃)
5. [可重入锁的实现](#五可重入锁的实现)
6. [公平锁的实现](#六公平锁的实现)
7. [读写锁 / 联锁 / 信号量](#七读写锁--联锁--信号量)
8. [锁粒度与分段锁 —— 性能优化](#八锁粒度与分段锁--性能优化)
9. [惊群效应与订阅唤醒](#九惊群效应与订阅唤醒)
10. [锁与事务、幂等的关系](#十锁与事务幂等的关系)
11. [Redisson 源码级问题](#十一redisson-源码级问题)
12. [Redis vs ZooKeeper vs etcd 深度对比](#十二redis-vs-zookeeper-vs-etcd-深度对比)
13. [生产事故复盘](#十三生产事故复盘)
14. [可观测性与压测](#十四可观测性与压测)
15. [硬核压轴题](#十五硬核压轴题)

---

## 一、Redlock 深度剖析（Martin vs antirez）

### Q1.1 请完整描述 Redlock 算法

Redlock 由 antirez（Redis 作者）提出，用于在 **多主 Redis** 环境下实现分布式锁。假设有 N 个（推荐 5 个）**完全独立** 的 Redis Master 节点（不是主从，是独立部署）。

获取锁流程：

```text
1. 获取当前时间 T1（毫秒）
2. 依次向 N 个 Redis 节点发送 SET key owner NX PX ttl 请求
   - 每个请求设一个远小于 TTL 的超时（比如 5~50ms），避免长时间阻塞
   - 无论成功失败都立即尝试下一个节点
3. 计算总耗时 = T2 - T1
4. 只有当以下两个条件同时满足才算获取成功：
   - 至少在 (N/2 + 1) 个节点上 SET 成功
   - 总耗时 < TTL（否则剩余可用时间太短，等于没锁）
5. 有效锁时长 = TTL - 总耗时 - 时钟漂移补偿
6. 如果失败，向所有节点发 DEL（包括那些不确定是否成功的节点）
```

### Q1.2 Martin Kleppmann 的批评（2016 年著名博文）

Martin 在《How to do distributed locking》里指出 Redlock 有两个致命缺陷。

**问题 1：依赖强同步的物理时钟**

Redlock 用 TTL 判断锁是否过期，前提是 **各节点的时钟走速一致**。但：

- 管理员手动改时钟
- NTP 服务修正时钟时可能跳跃几百毫秒到几秒
- 硬件时钟本身有漂移

时间线示例（N=5，需要多数派 3 票）：

```text
T=0     A 在节点 1、2、3 上获取锁成功（多数派）→ Redlock 认为 A 获得锁
T=1s    节点 1、2、3 因 NTP 修正时钟跳跃 +30 秒
        → 锁在这些节点上"提前"过期
T=2s    B 在节点 3、4、5 上获取锁成功（多数派）→ Redlock 认为 B 也获得锁
❌ 两个客户端同时持有锁 —— Safety 失败
```

**问题 2：GC Pause / 网络延迟导致的"过期锁执行"**

即使不考虑时钟问题，客户端在拿到锁到实际使用锁之间存在时间差：

```text
Client 1: acquire → 收到成功响应
                        │
                        ├── STW GC pause 15s（此时锁的 5s TTL 早已过期）
                        │
                        └── 醒来后继续写数据

同时：
Client 2: acquire → 因为锁已过期，成功获取
Client 2: 开始写数据

结果：Client 1 和 Client 2 同时向存储写数据 ❌
```

Martin 的结论是：Redlock 不是一个真正的 **线性一致（linearizable）** 锁。要真正安全，必须在 **被保护的资源侧** 引入 fencing token 校验。

### Q1.3 antirez 的反驳

antirez 在《Is Redlock safe?》里回应：

- 时钟跳跃是运维问题，正确配置的 NTP（step 阈值、slew mode）不会有大跳跃
- GC pause 问题所有分布式锁（包括 ZooKeeper）都有 —— 因为 ZK 的 session timeout 一样会被 GC pause 影响
- 现实中大部分业务场景对 "efficiency lock"（提升性能，减少重复工作）就够了，不需要 "correctness lock"（保证正确性，一票也不能错）

### Q1.4 面试标准答法

面试官问 "Redlock 安全吗"，不要只回答"安全"或"不安全"。分层回答：

1. **算法本身**：多数派 + TTL 补偿，理论上比单点 Redis 强
2. **两大缺陷**：时钟漂移、GC pause / 网络延迟
3. **场景区分**：
   - Efficiency lock（缓存击穿、去重、限流）：Redlock 甚至基础版都够
   - Correctness lock（金钱、库存、订单幂等）：必须叠加 fencing token 或用 ZK/etcd
4. **实际选型**：多数公司用单点 Redis + Owner + 看门狗 + 业务侧幂等，不上 Redlock，因为部署 5 个独立 Redis 成本高、运维复杂

---

## 二、Fencing Token —— 真正安全的护身符

### Q2.1 什么是 Fencing Token？

Fencing Token（栅栏令牌）是一个 **单调递增的整数**，每次获取锁都发放一个更大的 token。**被保护的资源** 在写入时校验 token，只接受 token 更大的请求。

流程：

```text
Client 1: acquire → 拿到 token = 33
Client 1: GC pause 15s
Client 2: acquire → 拿到 token = 34
Client 2: write(data, token=34) → 存储接受，记录 max_token=34

Client 1 醒来:
Client 1: write(data, token=33) → 存储检查：33 < max_token(34) → 拒绝 ❌
```

即使两个客户端"同时"以为自己拿到锁，token 校验保证只有最新的能写入。

### Q2.2 Redis 里如何生成 Fencing Token？

Redis 单点场景很简单：`INCR` 一个自增 key：

```lua
-- Lua 脚本：原子获取锁 + 发放递增 token
-- KEYS[1] = lock key, KEYS[2] = fencing counter key
-- ARGV[1] = owner, ARGV[2] = ttl_seconds
if redis.call("SET", KEYS[1], ARGV[1], "NX", "EX", ARGV[2]) then
    local token = redis.call("INCR", KEYS[2])
    return token
else
    return nil
end
```

Redlock 场景则要复杂得多 —— 因为多主环境本身就没有全局单调计数器。Martin 的论文里说这正是 Redlock 无法提供 fencing token 的根本原因，而 ZooKeeper 的 zxid、etcd 的 revision 天然就是全局单调递增的。

### Q2.3 存储层如何校验 Fencing Token？

需要 **资源侧配合**。常见做法：

- **数据库**：`UPDATE ... WHERE token > current_token` 或用版本号
- **对象存储**：某些云对象存储支持 `x-if-token-greater-than` 之类的条件写
- **应用侧**：写之前 SELECT max_token FOR UPDATE 校验

**关键**：Redis 锁本身给不了完整安全性 —— 必须在写数据的地方加防御。这也是 Martin 的核心观点。

---

## 三、GC Pause / STW 对分布式锁的致命影响

### Q3.1 STW 具体怎么破坏锁？

任何 Full GC、Swoole 死循环、CPU 抢占、虚拟机迁移都会造成进程暂停几秒甚至几十秒。时间线：

```text
T=0.0s  Client A: acquire(lock, ttl=10s) → 成功
T=0.1s  Client A: GC Pause 开始 ...
T=10s   锁过期
T=10.1s Client B: acquire(lock, ttl=10s) → 成功（对 Redis 来说 A 已释放）
T=11s   Client B: 开始扣库存 stock=100→99
T=12s   Client A: GC Pause 结束（沉睡 12 秒后醒来）
T=12.1s Client A: 依然认为自己持锁，继续扣库存 stock=99→98
        ❌ 但实际上 A 没锁，导致超卖
```

### Q3.2 缓解手段

按防御深度从浅到深：

1. **看门狗续期**：能缓解一般的慢查询，但缓解不了 STW（Timer 也被 STW 停了）
2. **业务操作前二次校验**：写库前重新 `GET lock` 校验 owner，仍有 TOCTOU 窗口
3. **Fencing Token**：把校验推到存储层，彻底解决（推荐）
4. **业务幂等**：即使重复执行也不会出错，比如订单 ID 唯一索引 + `INSERT IGNORE`
5. **缩短临界区**：把 IO 挪到锁外，锁内只做内存操作

### Q3.3 PHP-FPM 有 STW 问题吗？

PHP 没有 GC STW，但有类似问题：

- `pcntl_signal` 处理器被延迟
- 慢 SQL / 慢 HTTP 调用把整个请求周期拉长
- OPCache 编译大文件时短暂阻塞

所以 **PHP 场景更依赖"TTL 大 + 业务侧超时"** 而不是看门狗。

---

## 四、时钟漂移与 NTP 跳跃

### Q4.1 NTP 是怎么导致锁失效的？

NTP 有两种时间修正模式：

- **slew（缓步调整）**：每秒微调几百微秒，慢慢追平，安全
- **step（一次性跳跃）**：一次性跳过几百 ms 到几秒，**危险**

当本地时钟和 NTP 服务器差距超过 128ms 时（默认阈值），ntpd 会执行 step。这会导致：

- Redis 侧 TTL 判定错乱（本机看到的 EXPIRE 时刻突然改变）
- 客户端本地判断"锁还有多久过期"错乱

真实事故：某电商大促期间机房时钟同步异常，一台 Redis 时钟跳跃 30 秒，多个业务分布式锁瞬间过期，扣减库存出现超卖。

### Q4.2 如何防御？

- 只使用 slew 模式：`ntpd -x` 或 chrony 的 `makestep 0.0`（禁用 step）
- Redis 判断锁过期用 **单调时钟（monotonic clock）** 而不是 wall clock（Redis 6.0 部分改进）
- 应用侧的 TTL 计算也用 `hrtime()` 或 `CLOCK_MONOTONIC`，不用 `time()`
- 加大 TTL 冗余，减少边界踩踏概率
- 用 fencing token 做终极保底

### Q4.3 monotonic clock 和 wall clock 的区别

| 时钟类型 | 特性 | 用途 |
|----------|------|------|
| Wall clock（`time()`、`microtime()`） | 可回调、可被 NTP 修改 | 显示时间、日志时间戳 |
| Monotonic clock（`hrtime()`、`CLOCK_MONOTONIC`） | 只增不减、不受 NTP 影响 | 计算耗时、超时判断 |

**判断锁是否过期永远用 monotonic**，写日志/展示时间才用 wall clock。

---

## 五、可重入锁的实现

### Q5.1 什么是可重入锁？

同一个线程/协程可以多次获取同一把锁而不会自阻塞。Java 的 `ReentrantLock`、Redisson 的 `RLock` 都支持。

场景：`serviceA()` 加锁后调用 `serviceB()`，B 内部又想加同一把锁 —— 如果不可重入就死锁。

### Q5.2 用 Redis Hash 实现

value 存 owner，用 Hash 记录 owner + 重入计数：

```text
HSET lock:xxx owner "host1:pid1:threadId1" count 1
```

加锁 Lua：

```lua
-- KEYS[1] = lock key
-- ARGV[1] = owner, ARGV[2] = ttl_ms
if redis.call("EXISTS", KEYS[1]) == 0 then
    -- 锁不存在，直接获取
    redis.call("HSET", KEYS[1], "owner", ARGV[1], "count", 1)
    redis.call("PEXPIRE", KEYS[1], ARGV[2])
    return 1
elseif redis.call("HGET", KEYS[1], "owner") == ARGV[1] then
    -- 是自己的锁，重入 +1
    redis.call("HINCRBY", KEYS[1], "count", 1)
    redis.call("PEXPIRE", KEYS[1], ARGV[2])
    return 1
else
    -- 别人的锁
    return 0
end
```

释放 Lua：

```lua
-- KEYS[1] = lock key
-- ARGV[1] = owner
if redis.call("HGET", KEYS[1], "owner") ~= ARGV[1] then
    return 0  -- 不是自己的锁
end
local count = redis.call("HINCRBY", KEYS[1], "count", -1)
if count > 0 then
    return 1  -- 还有重入层数，不真正释放
else
    redis.call("DEL", KEYS[1])
    return 1  -- 完全释放
end
```

Redisson 的 RLock 就是这样实现的（还多了 pub/sub 订阅唤醒 + Watchdog 续期）。

### Q5.3 PHP 的可重入锁靠谱吗？

FPM 每个请求是独立进程，天然不需要重入 —— 一个请求内不会跨方法反复加同一把锁到"死锁"程度，因为没有跨协程调度。Swoole/Hyperf 协程环境下需要考虑，用 `Coroutine::getCid()` 作为 owner 一部分。

---

## 六、公平锁的实现

### Q6.1 为什么需要公平锁？

普通锁是"抢占式"：谁先 SET NX 成功谁就获得，可能出现某个客户端连续失败饿死。公平锁按 **请求顺序** 授予锁，先到先得。

### Q6.2 用 List 实现 FIFO 队列

思路：等待队列用 Redis List，锁的当前持有者写在另一个 key。

```text
Key 结构:
  lock:xxx              -- 当前持有者 owner
  lock:xxx:queue        -- 等待队列（List）
  lock:xxx:timeout:$id  -- 每个等待者的超时 ZSet（按时间戳排序）
```

获取锁伪代码：

```lua
-- 简化版
local first = redis.call("LINDEX", KEYS[2], 0)
if not redis.call("EXISTS", KEYS[1]) and (first == false or first == ARGV[1]) then
    -- 锁空闲且我是队首（或队列空）
    if first == ARGV[1] then
        redis.call("LPOP", KEYS[2])
    end
    redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
    return 1
else
    -- 排队（如果还没在队里）
    if redis.call("LPOS", KEYS[2], ARGV[1]) == false then
        redis.call("RPUSH", KEYS[2], ARGV[1])
    end
    return 0
end
```

### Q6.3 公平锁的代价

- 吞吐降低（增加了排队开销）
- 队首客户端崩溃需要超时清理，否则堵后面所有人（用 ZSet 记录每个等待者的最后活跃时间）
- 实现复杂度陡增

**结论**：除非有明确的公平性需求（如按序处理消息），不建议用。大部分场景抢占锁 + 幂等就够了。

---

## 七、读写锁 / 联锁 / 信号量

### Q7.1 读写锁（RWLock）

多个 reader 可以同时持有读锁，writer 必须独占。

Redis 实现思路：

```text
lock:xxx:mode     -- "read" 或 "write"
lock:xxx:readers  -- Hash，记录每个 reader 的重入次数
lock:xxx:writer   -- 当前写锁持有者
```

获取读锁的 Lua：

```lua
local mode = redis.call("HGET", KEYS[1], "mode")
if mode == false or mode == "read" then
    redis.call("HSET", KEYS[1], "mode", "read")
    redis.call("HINCRBY", KEYS[1], "reader:" .. ARGV[1], 1)
    redis.call("PEXPIRE", KEYS[1], ARGV[2])
    return 1
end
if mode == "write" and redis.call("HGET", KEYS[1], "writer") == ARGV[1] then
    -- writer 可以降级为 reader（同一 owner 内）
    ...
end
return 0
```

场景：读多写少的配置刷新、缓存重建。

### Q7.2 联锁（MultiLock）

一次性锁定多个 key（例如同时锁定订单 A 和订单 B 才能转账）。**必须按固定顺序** 获取，否则可能死锁：

```text
线程 1: 想同时锁 A → B
线程 2: 想同时锁 B → A
        → 线程 1 拿到 A，线程 2 拿到 B，互相等对方 → 死锁
```

解决：所有客户端按 key 字典序排序后再依次获取。任何一步失败就回滚已获取的锁。

### Q7.3 信号量（Semaphore）

限制同时执行的客户端数量（比如同时最多 10 个请求访问某下游服务）。Redis 实现：

```lua
-- 尝试获取一个 permit
local current = redis.call("HLEN", KEYS[1])
if current < tonumber(ARGV[3]) then
    redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])  -- ARGV[2] = 过期时间戳
    return 1
end
return 0
```

**注意**：需要清理过期的 permit（存过期时间戳，加锁前先扫一遍 HDEL 过期项）。或用 ZSet 更方便（score 为过期时间戳，`ZREMRANGEBYSCORE 0 now`）。

---

## 八、锁粒度与分段锁 —— 性能优化

### Q8.1 锁粒度带来的性能问题

假设有一个"库存扣减"的锁：

```text
lock:stock:global  → 所有 SKU 共用一把锁
```

所有 SKU 的扣减都串行 → QPS 上不去。

正确粒度：

```text
lock:stock:sku_001
lock:stock:sku_002
...
```

每个 SKU 一把锁，不同 SKU 并行。

### Q8.2 分段锁（Segmented Lock）

如果 SKU 数量太多（比如 100 万），每个 SKU 一把锁 Redis 内存占用高。可以用 hash 分段：

```php
$segment = crc32($sku) % 16;      // 分 16 段
$lockKey = "lock:stock:seg:{$segment}";
```

代价：同一段内的不同 SKU 会有 **假冲突**（本来不冲突的两个 SKU 因为哈希到同一段被迫串行）。段数越多冲突越少，但内存开销越大。这是 ConcurrentHashMap 的经典权衡。

### Q8.3 大 Key 与热 Key 陷阱

- **大 Key**：Hash 存了几万个 reader/waiter，`HGETALL` 一次几百 ms 阻塞 Redis 单线程
- **热 Key**：某个热点商品的锁 QPS 十万级，单个 Redis Master 顶不住

热 Key 解决方案：
- 本地锁 + 分布式锁（先在本机进程内 mutex，减少访问 Redis 次数）
- 客户端限流 / 排队合并请求
- 分段锁把热点打散

---

## 九、惊群效应与订阅唤醒

### Q9.1 什么是惊群？

大量客户端同时轮询同一把锁，锁一释放它们全部同时发 SET NX 请求 → Redis 瞬时压力尖峰、多数请求空跑失败。

轮询代码：

```php
while (!$lock->acquire($key)) {
    usleep(50 * 1000);  // 50ms 一次
}
// 100 个客户端每 50ms 轮询一次 → Redis 每 50ms 收到 100 个 SET NX 请求
```

### Q9.2 Pub/Sub 唤醒方案（Redisson 的方式）

获取锁失败后 **订阅一个 channel**，释放锁的客户端在 DEL 之后 PUBLISH 一条消息。等待中的客户端收到消息才发起下一次抢锁。

伪代码：

```php
public function acquireBlocking(string $key, string $owner, int $ttlMs, int $waitMs): bool
{
    $deadline = hrtime(true) + $waitMs * 1_000_000;
    while (hrtime(true) < $deadline) {
        // 尝试获取
        $ttlLeft = $this->tryAcquire($key, $owner, $ttlMs);
        if ($ttlLeft === null) {
            return true;  // 获取成功
        }
        
        // 订阅锁释放事件
        $sub = $this->redis->subscribe(["lock:release:{$key}"]);
        // 阻塞等待消息，或等到 $ttlLeft 到期（防止消息丢失）
        $sub->wait(min($ttlLeft, $deadline - hrtime(true)));
    }
    return false;
}

public function release(string $key, string $owner): void
{
    // Lua 中 DEL 后 PUBLISH
    $lua = <<<'LUA'
        if redis.call("GET", KEYS[1]) == ARGV[1] then
            redis.call("DEL", KEYS[1])
            redis.call("PUBLISH", KEYS[2], "released")
            return 1
        end
        return 0
    LUA;
    $this->redis->eval($lua, [$key, "lock:release:{$key}", $owner], 2);
}
```

优点：从轮询变为事件驱动，Redis 压力骤降，响应更及时。
缺点：需要 pub/sub 支持，实现复杂；PHP-FPM 天然不支持长订阅，主要在 Swoole/Java/Go 环境用。

---

## 十、锁与事务、幂等的关系

### Q10.1 有锁还需要幂等吗？

**必须要**。锁只能保证 **正常路径的互斥**，无法防御：

- 客户端超时重试（网络分区导致上游以为失败，实际下游已执行）
- STW 之后的重复执行
- 消息队列重投递

分布式系统的黄金法则：**锁做互斥（提升效率），幂等做正确性（保证不出错）**。

### Q10.2 有幂等还需要锁吗？

看场景：

- **纯读场景（缓存击穿）**：只是想让一个查库就够 → 锁避免"缓存击穿" → 幂等无关
- **写场景**：如果幂等成本低（唯一索引、乐观锁 CAS），可以不用分布式锁，靠 DB 唯一约束兜底
- **重资源操作**：调用第三方支付网关，重复调用会双扣款 → 锁 + 幂等 token 双保险

### Q10.3 数据库事务能不能代替分布式锁？

**特定场景可以**：

- `SELECT ... FOR UPDATE`：行级锁，事务提交才释放，能替代但性能差
- 唯一索引：天然的"分布式锁"，插入冲突就是别人先做过了
- 乐观锁 `UPDATE ... WHERE version = ?`：CAS 语义

Redis 锁 vs DB 锁：

| 维度 | Redis 锁 | DB `FOR UPDATE` | DB 唯一索引 |
|------|----------|-----------------|-------------|
| 性能 | 高（QPS 十万级） | 中（QPS 千级） | 极高（一次插入） |
| 死锁风险 | 有 TTL 兜底 | DB 有死锁检测 | 无 |
| 一致性 | 弱（TTL 失效） | 强（事务） | 强 |
| 跨表锁 | 可以 | 只能锁自己表 | 只能保单表唯一 |

---

## 十一、Redisson 源码级问题

### Q11.1 Redisson 的 Watchdog 默认续多久一次？

- 默认 lockWatchdogTimeout = 30 秒
- 续期间隔 = timeout / 3 ≈ 10 秒
- 未指定 leaseTime 时才启用 watchdog；指定了就用固定 leaseTime，不续期

### Q11.2 tryLock 和 lock 的区别？

- `tryLock()` / `tryLock(waitTime, unit)`：非阻塞或有限等待，返回 boolean
- `lock()` / `lock(leaseTime, unit)`：阻塞直到成功
- 建议 **永远用 tryLock 带 waitTime**，避免无限等待

### Q11.3 Redisson 内部锁 key 长什么样？

以 RLock 为例：

```text
lock:name           # 值类型是 Hash: {uuid:threadId → reentryCount}
redisson_lock__channel:{lock:name}  # 释放通知 channel
```

释放锁的 Lua 里会 `PUBLISH` 到 channel，等待方通过 pub/sub 唤醒（就是第九节讲的方案）。

### Q11.4 Redisson 遇到 Redis 主从切换怎么办？

Redisson 自己也承认单主 RLock 不安全，官方推荐用 `RedissonRedLock`（组合多个 RLock）或 `RedissonMultiLock`。但正如前面 Redlock 章节讨论，即使 RedLock 也不能完全解决问题，官方在文档里已经不再力推 RedLock，转而建议在关键路径上用 fencing token 或换用其他协调服务。

---

## 十二、Redis vs ZooKeeper vs etcd 深度对比

| 维度 | Redis | ZooKeeper | etcd |
|------|-------|-----------|------|
| 一致性模型 | 单点 CP，主从最终一致 | CP（ZAB 协议） | CP（Raft） |
| 锁实现 | SET NX + TTL | 临时顺序节点 + Watch | Lease + Compare + Watch |
| 崩溃检测 | TTL 被动过期 | Session 心跳主动检测 | Lease 心跳主动检测 |
| 惊群 | 需要 Pub/Sub 缓解 | 顺序节点 + Watch 前驱天然避免 | Watch 天然避免 |
| Fencing Token | 需要额外 INCR | zxid 天然递增 | revision 天然递增 |
| 性能 | 极高（10w+ QPS） | 中（1w QPS） | 中（1w QPS） |
| 运维复杂度 | 低 | 高 | 中 |
| 生态 | 极广泛 | Java 系为主 | K8s / cloud native |
| 适用场景 | 缓存互斥、去重、限流 | 强一致协调、Leader 选举 | K8s、服务发现、配置 |

### Q12.1 ZooKeeper 的锁为什么"更安全"？

- **Session-based**：客户端断连立即 session 失效，锁立即释放；不像 Redis 要等 TTL
- **顺序节点**：天然公平锁，watch 前一个节点，前驱释放才争锁 → 无惊群
- **ZAB 协议**：写入必须多数派确认，主从切换不丢数据
- **zxid**：天然的 fencing token

代价：性能低（每次 Watch/Create 都要走一致性协议）、运维复杂、Java 客户端最成熟。

### Q12.2 etcd 相比 ZK 的优势？

- 更现代（Go 实现、gRPC 接口、Kubernetes 依赖）
- Lease 更直观（时间到期自动释放，可 KeepAlive 续期）
- MVCC + Watch 更适合动态配置场景
- API 更简单

---

## 十三、生产事故复盘

### 事故 1：库存超卖

**现象**：秒杀活动扣减库存时，某 SKU 卖出 105 件（实际库存 100）。

**排查**：
- 用了 SET NX + Owner，但 TTL = 5s
- 慢查询导致某请求执行了 7s
- 锁 5s 过期，第二个请求拿到锁开始扣减
- 第一个请求 7s 后完成，扣减了旧的库存值

**根因**：TTL 过短 + 无看门狗 + 直接读写库存值（非 CAS）。

**修复**：
1. TTL 改为业务最大耗时 × 3
2. 库存扣减改为 `UPDATE stock SET count = count - 1 WHERE sku = ? AND count > 0`（DB CAS）
3. 引入 Fencing Token，DB 拒绝旧 token 的写入
4. 增加超时告警

### 事故 2：Redis 主从切换导致重复消费

**现象**：MQ 消费者用 Redis 锁做幂等，切换后同一条消息被消费两次。

**根因**：
- Master SET 成功后异步同步到 Slave
- Master 挂了，Slave 提升，锁不在
- 消费者重试，Slave（新 Master）SET 成功再次执行

**修复**：
1. 消费幂等 **不依赖 Redis 锁**，改用 DB 唯一索引 `INSERT INTO consumed(msg_id, ...)` 冲突则跳过
2. Redis 锁只做"减少重复处理"的优化，不做正确性保证

### 事故 3：看门狗导致锁永不释放

**现象**：某后台任务锁一直不释放，堵住整个队列。

**根因**：
- 用了看门狗自动续期
- 业务代码抛异常，异常处理里忘记调 `release`
- 看门狗持续续期，Redis 里锁永不过期

**修复**：
1. 用 try/finally 或 with-lock 语法糖强制释放
2. 看门狗限制最大续期次数（比如最多续 10 次 = 5 分钟）
3. 监控长时间持有的锁（Redis TTL 曲线 + 告警）

### 事故 4：跨机房网络抖动

**现象**：某跨机房服务的分布式锁经常获取失败，业务成功率下跌。

**根因**：
- Redis 部署在 A 机房，服务在 B 机房
- 机房间 RTT 从 2ms 抖到 200ms
- SET NX 请求经常超时，客户端以为失败但实际 Redis 已写入
- 客户端重试 → 死锁（因为已经写入的锁没被感知到）

**修复**：
1. 客户端超时时长 > TTL（避免"以为失败实际成功"）
2. 客户端每次重试前先 GET 检查是不是自己已经拿到
3. 关键业务 Redis 就近部署

---

## 十四、可观测性与压测

### Q14.1 分布式锁需要监控什么？

| 指标 | 说明 | 告警阈值示例 |
|------|------|-------------|
| 锁获取成功率 | acquire 成功 / 总尝试 | < 95% 告警 |
| 锁等待时长 P99 | 从尝试到获取成功耗时 | > 500ms 告警 |
| 锁持有时长 P99 | acquire 到 release 耗时 | > TTL 的 50% 告警（预警） |
| 锁超时未释放数 | Redis 中 TTL > 阈值 的锁数量 | > 10 告警 |
| 释放失败数 | release 返回 0（不是自己的锁） | > 0 告警（说明锁过期了） |
| Redis 侧 SET NX QPS | 特定 key 前缀的 QPS | 判断热点 |
| 看门狗续期次数 | 单锁续期次数 | 超过 N 次说明业务异常长 |

### Q14.2 如何压测分布式锁？

- **正确性**：多客户端并发 acquire，统计临界区被多个持有者访问的次数（应为 0）
- **性能**：单锁最大 QPS、不同 key 的整体 QPS
- **故障注入**：kill Redis 主节点、注入网络延迟、模拟 STW（sleep 一段时间）
- **长尾**：观察 P99 P999 延迟，找抖动源

工具：wrk / hey / vegeta 打压 + chaos-mesh 注入故障。

---

## 十五、硬核压轴题

### Q15.1 如果面试官说"我不允许你用锁，如何保证转账不重复？"

**核心思路**：把互斥推到数据层。

方案：
1. **DB 层唯一约束**：转账流水表 `UNIQUE(idempotency_key)`，重复请求插入失败
2. **乐观锁**：`UPDATE account SET balance = balance - 100, version = version + 1 WHERE id = ? AND version = ?`
3. **状态机**：订单从 `PENDING → PAID`，只允许 `PENDING` 状态更新为 `PAID`
4. **补偿事务（Saga / TCC）**：先扣款 + 冻结，超时或失败自动回滚

### Q15.2 分布式锁能保证严格串行执行吗？

**不能**。分布式系统的 FLP 不可能定理：在异步网络中，只要有一个进程可能失败，就不可能同时保证正确性 + 可用性 + 一致性。

分布式锁最多做到"减少并发冲突到可接受概率"，绝不是"绝对串行"。这也是为什么金融、数据库都还要靠事务、Paxos、Raft 等更严格的协议。

### Q15.3 你在设计一个分布式秒杀系统，锁怎么用？

标准架构：

```text
用户请求
    ↓
限流（本机 + 全局）
    ↓
预扣库存（Redis DECR，原子）  ← 用 Lua 判断 > 0 才扣
    ↓
放入 MQ 排队
    ↓
异步扣真库存（DB CAS + 唯一索引幂等）
    ↓
落单
```

**关键**：不需要传统分布式锁！用 Redis 的 `DECR` 原子性 + Lua 就是天然的锁语义，且性能远高于 SET NX。

### Q15.4 Redis 6.0 多线程对分布式锁有影响吗？

Redis 6.0 的多线程只是 **IO 多线程**（网络读写），命令执行依然是单线程 —— 所以 SET NX、Lua 等命令的原子性没变。分布式锁的实现不需要任何调整。

但吞吐提升了：单实例 QPS 可以从 8w+ 提升到 20w+，热点锁的抗压能力更强。

### Q15.5 一句话总结分布式锁

> 分布式锁只是"提示"，不是"保证"。真正的正确性来自数据层的幂等和约束。

---

## 参考资料

- Martin Kleppmann - [How to do distributed locking](https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html)
- antirez - [Is Redlock safe?](http://antirez.com/news/101)
- Redis 官方文档 - [Distributed Locks with Redis](https://redis.io/docs/latest/develop/use/patterns/distributed-locks/)
- Redisson - [Distributed locks and synchronizers](https://github.com/redisson/redisson/wiki/8.-distributed-locks-and-synchronizers)
- 《Designing Data-Intensive Applications》Chapter 8/9
- Diego Ongaro - Raft 论文（对比一致性协议）
