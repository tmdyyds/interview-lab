# Go 场景题合集 2026

**标签**: #go #interview #scenarios #高频

面试中常考的 Go 场景设计题，覆盖高并发、分布式、性能、故障排查、数据一致性等核心方向。每题包含：**场景 → 考察点 → 完整答案 → 深度剖析**。

---

## 目录

1. [首页聚合：多数据源并行查询](#1-首页聚合多数据源并行查询)
2. [秒杀系统：库存扣减](#2-秒杀系统库存扣减)
3. [分布式锁：Redis 实现](#3-分布式锁redis-实现)
4. [限流器：本地 + 分布式](#4-限流器本地--分布式)
5. [消息幂等：防重消费](#5-消息幂等防重消费)
6. [大文件上传：分片 + 断点续传](#6-大文件上传分片--断点续传)
7. [定时任务集群：分布式调度](#7-定时任务集群分布式调度)
8. [内存泄漏排查](#8-内存泄漏排查)
9. [goroutine 泄漏排查](#9-goroutine-泄漏排查)
10. [缓存一致性：先删缓存还是先更新库](#10-缓存一致性)
11. [服务熔断：Hystrix 模式](#11-服务熔断)
12. [长连接推送：百万连接](#12-长连接推送百万连接)
13. [短链服务：ID 生成](#13-短链服务)
14. [context 超时传递陷阱](#14-context-超时传递陷阱)
15. [高并发计数器：分段锁 vs atomic](#15-高并发计数器)
16. [分布式 ID 生成器](#16-分布式-id-生成器)
17. [订单超时关闭](#17-订单超时关闭)
18. [热点 key 探测与打散](#18-热点-key-探测与打散)
19. [平滑重启：零停机部署](#19-平滑重启零停机部署)
20. [链路追踪：TraceID 传递](#20-链路追踪traceid-传递)
21. [MySQL 分库分表方案](#21-mysql-分库分表方案)
22. [异步发送短信服务](#22-异步发送短信服务)

---

## 1. 首页聚合：多数据源并行查询

### 场景
百万 DAU 首页，需要聚合推荐、热门、关注、广告 4 个 ES 索引的数据，要求 P99 < 200ms。

### 考察点
`errgroup`、`context` 超时、降级容错、缓存策略、`singleflight`

### 答案

```go
type FeedService struct {
    recommend, hot, follow, ad *ESClient
    localCache *bigcache.BigCache
    redis      *redis.Client
    sf         singleflight.Group
}

func (s *FeedService) GetFeed(ctx context.Context, userID string, size int) ([]*FeedItem, error) {
    key := fmt.Sprintf("feed:%s:%d", userID, size)

    // L1 本地缓存
    if v, err := s.localCache.Get(key); err == nil {
        return decode(v), nil
    }

    // L2 Redis + singleflight 防击穿
    v, err, _ := s.sf.Do(key, func() (any, error) {
        if data, err := s.redis.Get(ctx, key).Bytes(); err == nil {
            return decode(data), nil
        }
        return s.fetchFromES(ctx, userID, size)
    })
    if err != nil {
        return nil, err
    }

    items := v.([]*FeedItem)
    s.localCache.Set(key, encode(items))
    return items, nil
}

func (s *FeedService) fetchFromES(ctx context.Context, userID string, size int) ([]*FeedItem, error) {
    // 200ms 硬超时
    ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
    defer cancel()

    var mu sync.Mutex
    all := make([]*FeedItem, 0, size*4)

    g, ctx := errgroup.WithContext(ctx)
    sources := map[string]*ESClient{
        "recommend": s.recommend,
        "hot":       s.hot,
        "follow":    s.follow,
        "ad":        s.ad,
    }

    for name, client := range sources {
        name, client := name, client
        g.Go(func() error {
            items, err := client.Search(ctx, userID, size)
            if err != nil {
                log.Printf("source %s failed: %v", name, err)
                return nil  // ⚠️ 吞错误：单源失败不影响整体
            }
            mu.Lock()
            all = append(all, items...)
            mu.Unlock()
            return nil
        })
    }
    _ = g.Wait()

    sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
    if len(all) > size {
        all = all[:size]
    }

    // 兜底：全部源失败返回静态数据
    if len(all) == 0 {
        return s.staticFallback(), nil
    }
    return all, nil
}
```

### 深度剖析

- **为什么用 errgroup 而不是 sync.WaitGroup**：errgroup 有 context 取消传播，任一源超时其他源立即感知
- **为什么单源失败要吞掉**：首页可用性 > 数据完整性。返回 3 个源的数据 > 因 1 个源挂了返回 500
- **本地缓存 + Redis 两级**：本地缓存无网络开销，Redis 兜底跨实例共享
- **singleflight 防击穿**：同 key 并发请求只查一次 ES，缓存失效瞬间尤其重要

---

## 2. 秒杀系统：库存扣减

### 场景
10 万人抢 1000 件商品，要求：不超卖、不少卖、性能高。

### 考察点
Redis 原子扣减、Lua 脚本、异步下单、限流

### 答案

```go
// Lua 脚本：原子检查+扣减，避免超卖
const stockDeductScript = `
local stock = tonumber(redis.call('GET', KEYS[1]))
if stock == nil or stock <= 0 then
    return -1
end
if stock < tonumber(ARGV[1]) then
    return 0
end
return redis.call('DECRBY', KEYS[1], ARGV[1])
`

type SeckillService struct {
    redis    *redis.Client
    mq       MQProducer
    limiter  *rate.Limiter
    script   *redis.Script
}

func (s *SeckillService) Seckill(ctx context.Context, userID, productID string) error {
    // 1. 前置限流：单机 QPS 上限
    if !s.limiter.Allow() {
        return ErrTooManyRequests
    }

    // 2. 用户维度去重：一人只能一次
    ok, _ := s.redis.SetNX(ctx, "sk:user:"+userID+":"+productID, 1, time.Hour).Result()
    if !ok {
        return ErrDuplicateRequest
    }

    // 3. Lua 原子扣减
    stockKey := "sk:stock:" + productID
    result, err := s.script.Run(ctx, s.redis, []string{stockKey}, 1).Int()
    if err != nil {
        s.redis.Del(ctx, "sk:user:"+userID+":"+productID)  // 回滚去重标记
        return err
    }
    if result == -1 {
        return ErrSoldOut
    }
    if result == 0 {
        return ErrInsufficientStock
    }

    // 4. 异步下单（消息队列削峰）
    return s.mq.Publish(ctx, "order.create", &OrderMsg{
        UserID:    userID,
        ProductID: productID,
        Qty:       1,
    })
}
```

### 深度剖析

- **为什么用 Lua**：Redis 单线程，Lua 保证 GET + 判断 + DECR 三步原子，不会超卖
- **为什么先扣 Redis 再下单**：DB 写入慢（毫秒级），Redis 快（纳秒级）。Redis 扣成功进 MQ，异步慢慢写 DB
- **超卖 vs 少卖**：Lua 原子操作解决超卖。少卖可能因为 MQ 消息丢失，需要对账补偿
- **热点商品优化**：把库存拆成 N 份（stock:001~stock:010），请求随机打到一份上，分散热点
- **前置限流**：不要让所有请求都打到 Redis，网关层就拦截 90%

---

## 3. 分布式锁：Redis 实现

### 场景
多实例部署的服务，需要保证某个操作全局只有一个实例在执行。

### 考察点
SETNX、过期时间、锁续期、释放安全

### 答案

```go
type RedisLock struct {
    client *redis.Client
    key    string
    value  string  // 唯一标识，防止误删别人的锁
    ttl    time.Duration
    cancel context.CancelFunc
}

func NewLock(client *redis.Client, key string, ttl time.Duration) *RedisLock {
    return &RedisLock{
        client: client,
        key:    key,
        value:  uuid.NewString(),
        ttl:    ttl,
    }
}

// 加锁（带自动续期）
func (l *RedisLock) Lock(ctx context.Context) error {
    ok, err := l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
    if err != nil {
        return err
    }
    if !ok {
        return ErrLockHeldByOther
    }

    // 启动续期 goroutine（看门狗）
    ctx, cancel := context.WithCancel(context.Background())
    l.cancel = cancel
    go l.renewLoop(ctx)
    return nil
}

// 释放（Lua 保证原子）
const unlockScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
`

func (l *RedisLock) Unlock(ctx context.Context) error {
    if l.cancel != nil {
        l.cancel()
    }
    _, err := l.client.Eval(ctx, unlockScript, []string{l.key}, l.value).Result()
    return err
}

// 续期 goroutine
func (l *RedisLock) renewLoop(ctx context.Context) {
    ticker := time.NewTicker(l.ttl / 3)  // 每 1/3 TTL 续一次
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            l.client.Expire(context.Background(), l.key, l.ttl)
        }
    }
}
```

### 深度剖析

- **为什么要 value 唯一标识**：防止 A 的锁 TTL 到期自动释放，B 拿到锁，A 再来 DEL 就误删了 B 的锁
- **为什么释放要用 Lua**：GET + 比较 + DEL 三步不是原子，中间可能被 B 抢到
- **为什么要看门狗续期**：业务处理超时时，锁 TTL 过期会导致并发。续期保证只要业务在跑，锁就不会掉
- **红锁（RedLock）**：单 Redis 挂了怎么办？RedLock 用多个独立 Redis 节点，超半数成功才算加锁成功。但复杂且有争议，一般用 Redis 集群 + 主从就够
- **etcd/ZooKeeper**：强一致性场景（金融）优先，Redis 是 AP 系统有丢锁风险

---

## 4. 限流器：本地 + 分布式

### 场景
接口 QPS 上限 10000，超过要拒绝。既要单机限流，又要集群统一限流。

### 考察点
令牌桶、漏桶、滑动窗口、`rate.Limiter`、Redis 计数

### 答案

**方案 A：单机令牌桶**（golang.org/x/time/rate）

```go
limiter := rate.NewLimiter(10000, 20000)  // 10000/s，突发 20000

func Handler(w http.ResponseWriter, r *http.Request) {
    if !limiter.Allow() {
        http.Error(w, "too many requests", 429)
        return
    }
    // 处理请求
}
```

**方案 B：分布式滑动窗口**（Redis + Lua）

```go
const slidingWindowScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])   -- 窗口大小(ms)
local limit = tonumber(ARGV[3])

-- 清理窗口外的旧数据
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

-- 统计当前窗口的请求数
local count = redis.call('ZCARD', key)
if count >= limit then
    return 0
end

-- 记录本次请求
redis.call('ZADD', key, now, now .. ':' .. math.random())
redis.call('PEXPIRE', key, window)
return 1
`

func (r *Limiter) Allow(ctx context.Context, key string) (bool, error) {
    now := time.Now().UnixMilli()
    result, err := r.script.Run(ctx, r.client,
        []string{"limit:" + key},
        now, 1000, 10000,  // 1s 窗口，10000 请求
    ).Int()
    if err != nil {
        return true, nil  // 限流器挂了不能挡业务，放行
    }
    return result == 1, nil
}
```

### 深度剖析

| 算法 | 特点 | 适合 |
|------|------|-----|
| 计数器 | 简单，有临界问题 | 粗粒度限流 |
| 滑动窗口 | 平滑，精确 | API 限流 |
| 令牌桶 | 允许突发 | 大部分场景（Go 官方推荐） |
| 漏桶 | 严格平滑输出 | 网络流量整形 |

**多级限流架构**：
```
CDN → 网关限流（IP、User）→ 服务限流（接口）→ 应用限流（业务规则）
```

---

## 5. 消息幂等：防重消费

### 场景
Kafka 消费者，一条消息可能被消费多次（rebalance、重试），要保证业务只执行一次。

### 考察点
幂等设计、去重表、状态机

### 答案

```go
type OrderConsumer struct {
    db    *sql.DB
    redis *redis.Client
}

func (c *OrderConsumer) Consume(ctx context.Context, msg *Message) error {
    // 方案一：Redis SETNX 幂等（简单但依赖 Redis 可用性）
    ok, err := c.redis.SetNX(ctx,
        "idem:"+msg.ID,
        "processing",
        24*time.Hour,
    ).Result()
    if err != nil {
        return err
    }
    if !ok {
        return nil  // 已处理过，直接 ack
    }

    // 方案二：DB 唯一索引兜底（最可靠）
    tx, err := c.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // 幂等表：msg_id 是唯一索引
    _, err = tx.ExecContext(ctx,
        "INSERT INTO idempotent_records(msg_id, status) VALUES(?, 'processing')",
        msg.ID)
    if err != nil {
        if isDuplicateKey(err) {
            return nil  // 已处理，跳过
        }
        return err
    }

    // 业务逻辑
    if err := c.createOrder(ctx, tx, msg); err != nil {
        return err
    }

    // 更新状态
    _, err = tx.ExecContext(ctx,
        "UPDATE idempotent_records SET status='done' WHERE msg_id=?",
        msg.ID)
    if err != nil {
        return err
    }

    return tx.Commit()
}
```

### 深度剖析

- **为什么需要幂等**：MQ 至少投递一次（at-least-once），网络抖动、消费者宕机重启都会导致重复
- **Redis 幂等的坑**：Redis 挂了或 key 过期，还是可能重复。生产建议 DB 唯一索引兜底
- **状态机幂等**：订单从 `created` → `paid` → `shipped`，同一状态只能从特定前置状态转换，天然幂等
- **接口幂等**：客户端生成 `idempotency-key`，服务端记录 key → response 的映射

---

## 6. 大文件上传：分片 + 断点续传

### 场景
上传 10GB 文件到对象存储，支持断点续传、失败重试、并发分片。

### 考察点
分片策略、并发控制、进度追踪、Merkle 树校验

### 答案

```go
type Uploader struct {
    chunkSize int64  // 5MB
    parallel  int    // 并发数 5
    oss       OSSClient
}

func (u *Uploader) Upload(ctx context.Context, filePath, objectKey string) error {
    file, err := os.Open(filePath)
    if err != nil {
        return err
    }
    defer file.Close()

    stat, _ := file.Stat()
    size := stat.Size()
    chunks := (size + u.chunkSize - 1) / u.chunkSize

    // 1. 初始化上传任务，拿到 uploadID
    uploadID, err := u.oss.InitMultipartUpload(ctx, objectKey)
    if err != nil {
        return err
    }

    // 2. 查询已上传的分片（断点续传）
    uploaded, _ := u.oss.ListUploadedParts(ctx, objectKey, uploadID)
    uploadedSet := make(map[int]string)
    for _, p := range uploaded {
        uploadedSet[p.Number] = p.ETag
    }

    // 3. 并发上传剩余分片
    parts := make([]Part, chunks)
    sem := make(chan struct{}, u.parallel)
    g, ctx := errgroup.WithContext(ctx)

    for i := int64(0); i < chunks; i++ {
        i := i
        partNum := int(i + 1)

        // 已上传的跳过
        if etag, ok := uploadedSet[partNum]; ok {
            parts[i] = Part{Number: partNum, ETag: etag}
            continue
        }

        sem <- struct{}{}
        g.Go(func() error {
            defer func() { <-sem }()

            offset := i * u.chunkSize
            length := u.chunkSize
            if offset+length > size {
                length = size - offset
            }

            // 读取分片
            buf := make([]byte, length)
            if _, err := file.ReadAt(buf, offset); err != nil && err != io.EOF {
                return err
            }

            // 带重试的上传
            var etag string
            err := retry.Do(func() error {
                var err error
                etag, err = u.oss.UploadPart(ctx, objectKey, uploadID, partNum, buf)
                return err
            }, retry.Attempts(3), retry.Delay(time.Second))
            if err != nil {
                return err
            }

            parts[i] = Part{Number: partNum, ETag: etag}
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        return err
    }

    // 4. 合并分片
    return u.oss.CompleteMultipartUpload(ctx, objectKey, uploadID, parts)
}
```

### 深度剖析

- **分片大小**：太小 → 请求数多，太大 → 重传成本高。业界推荐 5MB-10MB
- **并发数**：受网络带宽和服务端限制，一般 5-10
- **断点续传**：客户端记录 uploadID，重试时先 `ListUploadedParts` 拿到已完成的分片
- **完整性校验**：每个分片计算 MD5/SHA256，服务端合并时校验
- **秒传**：先算整个文件的 hash，服务端已存在直接返回，无需上传

---

## 7. 定时任务集群：分布式调度

### 场景
多实例部署的服务，有个定时任务需要每分钟执行一次，但**只能有一个实例**执行。

### 考察点
分布式锁、租约、Leader 选举

### 答案

**方案一：Redis 分布式锁 + 定时**

```go
func (s *Scheduler) Run(ctx context.Context) {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // 尝试拿锁，只有一个实例能拿到
            lock := NewLock(s.redis, "cron:daily-report", 50*time.Second)
            if err := lock.Lock(ctx); err != nil {
                continue  // 拿不到锁，其他实例在执行
            }

            func() {
                defer lock.Unlock(ctx)
                s.doTask(ctx)
            }()
        }
    }
}
```

**方案二：etcd Leader 选举**（更可靠）

```go
func (s *Scheduler) RunWithEtcd(ctx context.Context) error {
    session, err := concurrency.NewSession(s.etcdClient, concurrency.WithTTL(15))
    if err != nil {
        return err
    }
    defer session.Close()

    election := concurrency.NewElection(session, "/cron/leader")

    // Campaign 会阻塞直到成为 leader
    if err := election.Campaign(ctx, hostname); err != nil {
        return err
    }

    // 成为 leader 后开始执行任务
    ticker := time.NewTicker(time.Minute)
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-session.Done():
            return errors.New("session expired")
        case <-ticker.C:
            s.doTask(ctx)
        }
    }
}
```

**方案三：专业调度器**：`XXL-Job`、`ElasticJob`、`Temporal`

### 深度剖析

- **Redis 锁的坑**：锁 TTL 需要 > 任务执行时间，否则任务还没跑完锁就被别人抢走
- **etcd Leader**：TTL 到期前需要续期，session 断了自动下线，可靠性高
- **任务重复执行的容忍度**：如果业务能容忍重复，Redis 锁简单够用；不能容忍（金融）用 etcd

---

## 8. 内存泄漏排查

### 场景
线上服务运行 3 天后内存从 200MB 涨到 3GB，怎么定位？

### 考察点
pprof、heap profile、逃逸分析、常见泄漏点

### 完整案例：从写出泄漏 → 定位 → 修复

#### Step 1：写一个有泄漏的服务

```go
// leak_server.go —— 一个"用户会话缓存"服务，故意留下内存泄漏
package main

import (
    "fmt"
    "net/http"
    _ "net/http/pprof"
    "sync"
    "time"
)

type Session struct {
    UserID   string
    LoginAt  time.Time
    Payload  []byte  // 每个 session 1MB 数据
}

// ❌ 泄漏点：全局 map 只加不删，也没有 TTL
var (
    sessions = make(map[string]*Session)
    mu       sync.Mutex
)

func loginHandler(w http.ResponseWriter, r *http.Request) {
    userID := fmt.Sprintf("user_%d", time.Now().UnixNano())
    s := &Session{
        UserID:  userID,
        LoginAt: time.Now(),
        Payload: make([]byte, 1<<20),  // 每次加 1MB
    }
    mu.Lock()
    sessions[userID] = s
    mu.Unlock()
    w.Write([]byte("ok"))
}

func main() {
    // pprof 只绑 localhost
    go http.ListenAndServe("localhost:6060", nil)

    http.HandleFunc("/login", loginHandler)
    http.ListenAndServe(":8080", nil)
}
```

#### Step 2：模拟流量，触发泄漏

```bash
# 用 wrk 或 ab 压测 1 万次
ab -n 10000 -c 100 http://localhost:8080/login
```

观察内存：从启动时的 30MB 涨到 10GB+。

#### Step 3：用 pprof 排查

```bash
# 抓当前堆快照
go tool pprof http://localhost:6060/debug/pprof/heap
```

进入 pprof 交互界面：

```
(pprof) top
Showing nodes accounting for 10.24GB, 100% of 10.24GB total
      flat  flat%   sum%        cum   cum%
   10.24GB   100%   100%    10.24GB   100%  main.loginHandler
         0     0%   100%    10.24GB   100%  net/http.(*ServeMux).ServeHTTP
         0     0%   100%    10.24GB   100%  net/http.serverHandler.ServeHTTP

# 定位到具体行
(pprof) list loginHandler
     .          .     20:        Payload: make([]byte, 1<<20),  ← ⚠️ 每次分配 1MB
     .    10.24GB     21:    }
     .          .     22:    mu.Lock()
     .    10.24GB     23:    sessions[userID] = s              ← ⚠️ 塞进 map 后不释放
     .          .     24:    mu.Unlock()
```

**结论**：`sessions` map 无限增长。10000 次请求 × 1MB = ~10GB。

#### Step 4：对比两个时间点

```bash
# 间隔 5 分钟抓两次
curl http://localhost:6060/debug/pprof/heap > heap1.pb.gz
sleep 300
curl http://localhost:6060/debug/pprof/heap > heap2.pb.gz

# 对比差异（只看增长部分）
go tool pprof -base=heap1.pb.gz heap2.pb.gz
```

如果某个函数在 diff 里持续增长，就是泄漏源。

#### Step 5：修复

```go
// ✅ 修复方案：用带 TTL 的 LRU 缓存
import "github.com/hashicorp/golang-lru/v2/expirable"

var sessions = expirable.NewLRU[string, *Session](
    10000,             // 最多 10000 个 session
    nil,               // 淘汰回调
    30*time.Minute,    // TTL 30 分钟
)

func loginHandler(w http.ResponseWriter, r *http.Request) {
    userID := fmt.Sprintf("user_%d", time.Now().UnixNano())
    s := &Session{
        UserID:  userID,
        LoginAt: time.Now(),
        Payload: make([]byte, 1<<20),
    }
    sessions.Add(userID, s)  // ✅ 自动淘汰过期或超容量的
    w.Write([]byte("ok"))
}
```

---

### 其他常见泄漏模式

#### 模式 1：slice 引用大数组

```go
// ❌ 只想要 10 字节，结果引用了整个数组
func GetHeader(data []byte) []byte {
    return data[:10]  // 底层数组（可能 10MB）整个被引用
}

// ✅ copy 到独立 slice
func GetHeader(data []byte) []byte {
    header := make([]byte, 10)
    copy(header, data[:10])
    return header
}
```

**pprof 现象**：`heap` 中大量 `[]byte` 占用，但业务代码里"没保留大数组"。

#### 模式 2：time.Ticker 未 Stop

```go
// ❌ 每次调用都创建 Ticker 但不 Stop
func StartPolling() {
    ticker := time.NewTicker(time.Second)
    go func() {
        for range ticker.C {
            doWork()
        }
    }()
    // 函数返回后 goroutine 还在跑，ticker 也没停
}

// ✅ 用 context 控制生命周期
func StartPolling(ctx context.Context) {
    ticker := time.NewTicker(time.Second)
    go func() {
        defer ticker.Stop()  // ✅ 保证 Stop
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                doWork()
            }
        }
    }()
}
```

#### 模式 3：string 拼接产生大量字符串

```go
// ❌ 每次 += 都创建新字符串
var result string
for _, s := range strs {
    result += s  // 循环 N 次，产生 N 个中间字符串
}

// ✅ 用 strings.Builder
var b strings.Builder
for _, s := range strs {
    b.WriteString(s)
}
result := b.String()
```

---

### 深度剖析

**pprof 关键指标：**

| 指标 | 含义 | 用途 |
|------|------|-----|
| `inuse_space` | 当前使用的内存 | 排查泄漏 |
| `inuse_objects` | 当前存活的对象数 | 排查对象碎片化 |
| `alloc_space` | 累计分配（含已回收） | 排查频繁分配 |
| `alloc_objects` | 累计对象数 | 排查 GC 压力 |

```bash
# 默认查 inuse_space
go tool pprof http://localhost:6060/debug/pprof/heap

# 查 alloc_space（累计分配）
go tool pprof --alloc_space http://localhost:6060/debug/pprof/heap
```

**其他辅助工具：**

```bash
# GC 日志（观察 GC 频率、堆大小趋势）
GODEBUG=gctrace=1 ./myapp

# 硬性内存上限（Go 1.19+）
GOMEMLIMIT=4GiB ./myapp   # 超过 4GB 触发激进 GC

# runtime 内存指标
runtime.ReadMemStats(&m)
fmt.Println(m.HeapInuse, m.HeapObjects)
```

**inuse vs alloc 的区别**：

- `alloc_space` 高但 `inuse_space` 正常 → **不是泄漏**，是分配频繁（比如反序列化大对象）→ 用 `sync.Pool` 优化
- `inuse_space` 持续增长 → **真泄漏**，要找出未释放的引用

---

## 9. goroutine 泄漏排查

### 场景
线上服务 goroutine 数量从 1000 涨到 100000，接口响应也变慢。怎么定位？

### 考察点
pprof goroutine、channel 阻塞、context 取消、`goleak`

### 完整案例：从写出泄漏 → 定位 → 修复

#### Step 1：写一个泄漏的服务

模拟一个"调用下游 API"的接口，用 `context` 控制超时。

```go
// leak_goroutine.go
package main

import (
    "context"
    "fmt"
    "net/http"
    _ "net/http/pprof"
    "runtime"
    "time"
)

// ❌ 泄漏点：无缓冲 channel + context 提前取消 → 子 goroutine 永久阻塞
func slowQuery(ctx context.Context) (string, error) {
    ch := make(chan string)  // ❌ 无缓冲

    go func() {
        time.Sleep(3 * time.Second)  // 模拟慢查询 3 秒
        ch <- "result"               // 如果 ctx 已取消，没人读 → 永久阻塞
    }()

    select {
    case r := <-ch:
        return r, nil
    case <-ctx.Done():
        // ⚠️ 这里直接返回，上面的 goroutine 还在等着写 ch
        return "", ctx.Err()
    }
}

func queryHandler(w http.ResponseWriter, r *http.Request) {
    // 用户请求超时设置为 100ms（远小于 slowQuery 的 3s）
    ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
    defer cancel()

    result, err := slowQuery(ctx)
    if err != nil {
        http.Error(w, err.Error(), 504)
        return
    }
    w.Write([]byte(result))
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(w, "goroutines: %d\n", runtime.NumGoroutine())
}

func main() {
    go http.ListenAndServe("localhost:6060", nil)

    http.HandleFunc("/query", queryHandler)
    http.HandleFunc("/metrics", metricsHandler)
    http.ListenAndServe(":8080", nil)
}
```

#### Step 2：压测触发泄漏

```bash
# 每次请求 100ms 超时，都会泄漏 1 个 goroutine
ab -n 10000 -c 100 http://localhost:8080/query

# 查看 goroutine 数量
curl http://localhost:8080/metrics
# goroutines: 10024   ← 每次 /query 泄漏一个
```

#### Step 3：抓 goroutine 堆栈

```bash
# 方式 1：debug=1 汇总视图（每种阻塞点合并计数）
curl 'http://localhost:6060/debug/pprof/goroutine?debug=1'
```

输出示例（关键部分）：

```
goroutine profile: total 10024
10000 @ 0x43e0c6 0x44e0f0 0x4a7268 0x475cb1
#	0x4a7267	main.slowQuery.func1+0x87	/app/leak_goroutine.go:19  ← ⚠️
```

**看到 10000 个 goroutine 都卡在 `leak_goroutine.go:19`**，就是 `ch <- "result"` 那一行。

```bash
# 方式 2：debug=2 每个 goroutine 完整堆栈（含阻塞时间）
curl 'http://localhost:6060/debug/pprof/goroutine?debug=2' > gs.txt
head -20 gs.txt
```

```
goroutine 12345 [chan send, 5 minutes]:
main.slowQuery.func1()
        /app/leak_goroutine.go:19 +0x87   ← 卡在这行 5 分钟
created by main.slowQuery in goroutine 999
        /app/leak_goroutine.go:16 +0x9a
```

关键信息：
- `chan send, 5 minutes` = 卡在 channel 发送 5 分钟
- 定位到 `leak_goroutine.go:19` 这一行

#### Step 4：用 pprof web UI 可视化

```bash
go tool pprof http://localhost:6060/debug/pprof/goroutine
(pprof) top
Showing nodes accounting for 10001, 100% of 10001 total
      flat  flat%   sum%        cum   cum%
     10000   100%   100%      10000   100%  main.slowQuery.func1
         1  0.01%   100%          1  0.01%  runtime.gopark

(pprof) list slowQuery.func1
    .          .     17:    go func() {
    .          .     18:        time.Sleep(3 * time.Second)
    .      10000     19:        ch <- "result"           ← ⚠️ 10000 个 goroutine 在这
    .          .     20:    }()
```

#### Step 5：修复

**方案 A：用缓冲 channel（简单直接）**

```go
func slowQuery(ctx context.Context) (string, error) {
    ch := make(chan string, 1)  // ✅ 容量 1，子 goroutine 写完就退出

    go func() {
        time.Sleep(3 * time.Second)
        ch <- "result"  // ✅ 即使没人读，也能立即写入退出
    }()

    select {
    case r := <-ch:
        return r, nil
    case <-ctx.Done():
        return "", ctx.Err()
    }
}
```

**方案 B：把 ctx 传给子 goroutine（更规范）**

```go
func slowQuery(ctx context.Context) (string, error) {
    ch := make(chan string, 1)

    go func() {
        // 用 ctx 控制慢查询本身，不做无用功
        result, err := doQueryWithContext(ctx)
        if err != nil {
            return
        }
        select {
        case ch <- result:
        case <-ctx.Done():
            return  // ctx 取消也退出
        }
    }()

    select {
    case r := <-ch:
        return r, nil
    case <-ctx.Done():
        return "", ctx.Err()
    }
}
```

修复后再次压测：

```bash
ab -n 10000 -c 100 http://localhost:8080/query
curl http://localhost:8080/metrics
# goroutines: 24  ← ✅ 恢复正常
```

---

### 其他常见泄漏模式

#### 模式 1：无限循环没退出机制

```go
// ❌ 每来一个请求启动一个后台协程，永不退出
func StartWorker() {
    go func() {
        for {
            doWork()      // 永远跑
            time.Sleep(time.Second)
        }
    }()
}

// ✅ 用 context 控制生命周期
func StartWorker(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return  // ✅ 支持退出
            case <-ticker.C:
                doWork()
            }
        }
    }()
}
```

#### 模式 2：WaitGroup 计数不匹配

```go
// ❌ 有些分支忘了 Done
wg.Add(1)
go func() {
    if err := do(); err != nil {
        return  // ❌ 没调 wg.Done()，wg.Wait() 永远返回不了
    }
    wg.Done()
}()

// ✅ 用 defer 保证一定执行
wg.Add(1)
go func() {
    defer wg.Done()  // ✅ 无论怎么退出都调用
    if err := do(); err != nil {
        return
    }
}()
```

#### 模式 3：range channel 但不 close

```go
// ❌ 消费者永远等
func consumer(ch <-chan int) {
    for v := range ch {  // 生产者忘了 close，永远等不到 EOF
        process(v)
    }
}

// ✅ 生产者负责 close
func producer(ch chan<- int) {
    defer close(ch)  // ✅
    for i := 0; i < 10; i++ {
        ch <- i
    }
}
```

---

### 测试防御：goleak

CI 里用 `goleak` 检测测试是否泄漏 goroutine：

```go
import "go.uber.org/goleak"

func TestSlowQuery(t *testing.T) {
    defer goleak.VerifyNone(t)  // 测试结束时验证没有泄漏

    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    _, _ = slowQuery(ctx)
    // 如果 slowQuery 内部有泄漏，测试会失败并打印泄漏的 goroutine 堆栈
}
```

**输出示例**（有泄漏时）：

```
goleak: Errors on successful test run: found unexpected goroutines:
[Goroutine 7 in state chan send, with main.slowQuery.func1 on top of the stack:
main.slowQuery.func1()
	/app/leak_goroutine.go:19 +0x87
]
```

---

### 排查 Checklist

线上 goroutine 数暴涨时，按顺序排查：

1. **抓当前堆栈**：`curl /debug/pprof/goroutine?debug=1`
2. **看数量最多的阻塞点**：如 `chan send, chan receive, semacquire`
3. **看阻塞时长**：`debug=2` 里 `X minutes` 越大越可疑
4. **对比启动时的堆栈**：初始 goroutine 是必要的，新增的才是泄漏
5. **查代码模式**：
   - 无缓冲 channel + select ctx.Done？
   - 后台协程有没有退出机制？
   - defer wg.Done 加了吗？
   - range channel 但没 close？

### 深度剖析

- **goroutine 泄漏 → 内存泄漏**：每个 goroutine 至少占 2KB 栈 + 引用的对象，1 万个泄漏 = 20MB 起跳，还会 hold 住引用的对象
- **调度器压力**：goroutine 越多，runtime 调度、GC 扫描栈的开销越大，接口变慢
- **发现时机**：接入 Prometheus 采集 `go_goroutines` 指标，配置告警（如超过 10000 报警）

---

## 10. 缓存一致性

### 场景
DB 更新后，如何让缓存跟上？先删缓存还是先更新库？

### 考察点
Cache Aside、双写一致性、延迟双删、Canal

### 答案

**Cache Aside（业界主流）**：

```go
// 读：缓存不存在则回源，回写缓存
func Get(id int64) (*Product, error) {
    key := fmt.Sprintf("product:%d", id)
    if v, err := redis.Get(key); err == nil {
        return v, nil
    }
    p, err := db.Query(id)
    if err != nil {
        return nil, err
    }
    redis.Set(key, p, 5*time.Minute)
    return p, nil
}

// 写：先更新 DB，再删缓存
func Update(p *Product) error {
    if err := db.Update(p); err != nil {
        return err
    }
    redis.Del(fmt.Sprintf("product:%d", p.ID))
    return nil
}
```

**为什么先更新 DB 再删缓存**：

```
如果反过来（先删缓存 → 更新 DB）：
时刻 T1: A 删缓存
时刻 T2: B 读，缓存 miss，读 DB 拿到旧值
时刻 T3: B 写回缓存（旧值）
时刻 T4: A 更新 DB（新值）
结果：缓存是旧值，DB 是新值 → 长期不一致 ❌

先更新 DB 再删缓存：
时刻 T1: A 更新 DB (新值)
时刻 T2: A 删缓存
时刻 T3: B 读，缓存 miss，读 DB (新值) → 一致 ✅
```

**延迟双删**（应对读写并发）：

```go
func Update(p *Product) error {
    redis.Del(key)                    // 先删一次
    db.Update(p)                      // 更新 DB
    time.AfterFunc(500*time.Millisecond, func() {
        redis.Del(key)                // 延迟再删（清掉并发时被回写的脏数据）
    })
    return nil
}
```

**强一致方案**：Canal 订阅 binlog → 消费者删缓存

### 深度剖析

- **Cache Aside 不保证强一致**：极端情况仍有短暂不一致，但概率极低
- **删除失败怎么办**：MQ 重试删除，或用 Canal 兜底
- **热点数据**：更新时用分布式锁串行化，或用 read-through 模式

---

## 11. 服务熔断

### 场景
下游服务响应慢，导致上游 goroutine 越积越多，最终雪崩。如何保护？

### 考察点
熔断器状态机、`sony/gobreaker`、`hystrix-go`

### 答案

```go
import "github.com/sony/gobreaker"

var cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
    Name:        "UserService",
    MaxRequests: 3,               // 半开状态允许 3 个探测请求
    Interval:    10 * time.Second, // 统计窗口
    Timeout:     60 * time.Second, // 熔断后多久尝试半开
    ReadyToTrip: func(counts gobreaker.Counts) bool {
        // 请求数 >= 10 且失败率 > 60% 则熔断
        return counts.Requests >= 10 &&
            float64(counts.TotalFailures)/float64(counts.Requests) > 0.6
    },
})

func GetUser(ctx context.Context, id int64) (*User, error) {
    result, err := cb.Execute(func() (any, error) {
        return userClient.Get(ctx, id)
    })
    if err != nil {
        if errors.Is(err, gobreaker.ErrOpenState) {
            return getUserFromCache(id)  // 熔断了，走降级
        }
        return nil, err
    }
    return result.(*User), nil
}
```

**状态机**：

```
Closed（正常）
    ↓ 失败率超阈值
Open（熔断）—— 所有请求直接失败，走降级
    ↓ Timeout 后
HalfOpen（半开）—— 允许少量探测请求
    ↓ 成功    ↓ 失败
  Closed     Open
```

### 深度剖析

- **熔断 vs 限流**：限流是控制入口流量，熔断是保护下游依赖
- **熔断粒度**：按依赖服务分别熔断（不能一个挂了全都熔断）
- **降级策略**：返回缓存、返回默认值、返回空、报错
- **Kubernetes 场景**：Istio Sidecar 提供服务网格级别的熔断

---

## 12. 长连接推送：百万连接

### 场景
IM 服务需要维护百万级 WebSocket 长连接，如何设计？

### 考察点
epoll、连接管理、消息路由、水平扩展

### 答案

**架构**：

```
客户端 ──WebSocket──> LB ──> 接入层（多实例）
                              │
                              ├── 连接管理（本地 map: userID → conn）
                              └── 订阅路由消息（Redis Pub/Sub）
                                      ↑
                              业务服务发消息 ──> Redis Pub/Sub
```

**关键代码**：

```go
type Gateway struct {
    conns    sync.Map  // userID -> *Client
    redis    *redis.Client
    hostname string
}

type Client struct {
    userID string
    conn   *websocket.Conn
    send   chan []byte
}

// 建立连接
func (g *Gateway) OnConnect(userID string, conn *websocket.Conn) {
    client := &Client{
        userID: userID,
        conn:   conn,
        send:   make(chan []byte, 100),
    }
    g.conns.Store(userID, client)

    // 注册用户在哪个实例
    g.redis.Set(context.Background(), "user:node:"+userID, g.hostname, time.Hour)

    // 收发协程
    go client.readLoop(g)
    go client.writeLoop()
}

func (c *Client) writeLoop() {
    ticker := time.NewTicker(30 * time.Second)  // 心跳
    defer ticker.Stop()
    for {
        select {
        case msg := <-c.send:
            c.conn.WriteMessage(websocket.TextMessage, msg)
        case <-ticker.C:
            c.conn.WriteMessage(websocket.PingMessage, nil)
        }
    }
}

// 业务服务推送消息
func SendMessage(ctx context.Context, userID string, msg []byte) error {
    // 查用户在哪个实例
    node, err := redis.Get(ctx, "user:node:"+userID).Result()
    if err != nil {
        return err  // 用户离线
    }
    // 通过 Redis Pub/Sub 转发到对应实例
    return redis.Publish(ctx, "push:"+node, encodeMsg(userID, msg)).Err()
}

// 每个实例订阅自己的 channel
func (g *Gateway) subscribeLoop() {
    pubsub := g.redis.Subscribe(context.Background(), "push:"+g.hostname)
    for msg := range pubsub.Channel() {
        userID, payload := decodeMsg(msg.Payload)
        if v, ok := g.conns.Load(userID); ok {
            client := v.(*Client)
            select {
            case client.send <- payload:
            default:
                // send 满了，丢弃或断连
            }
        }
    }
}
```

### 深度剖析

- **单机连接数**：Linux 需要调整 `ulimit -n`，Go 单机 100 万连接可行（内存和 CPU 是瓶颈）
- **为什么用 Redis Pub/Sub**：业务服务不需要知道用户在哪个 gateway 实例，通过 Redis 路由
- **心跳**：TCP KeepAlive 不够（默认 2 小时），应用层每 30s 发心跳
- **消息可靠性**：send channel 缓冲满了怎么办？丢弃或落到 MQ 离线消息
- **性能优化**：连接池、协议压缩（Protobuf）、连接绑定 CPU 核心

---

## 13. 短链服务

### 场景
把 `https://example.com/very/long/url` 缩短为 `https://s.co/aB3xY`。设计短链服务。

### 考察点
ID 生成、Base62 编码、缓存、防碰撞

### 答案

```go
// Base62 字符集
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func toBase62(n int64) string {
    if n == 0 {
        return "0"
    }
    var result []byte
    for n > 0 {
        result = append([]byte{alphabet[n%62]}, result...)
        n /= 62
    }
    return string(result)
}

type ShortURLService struct {
    db      *sql.DB
    redis   *redis.Client
    idGen   IDGenerator  // 雪花算法
}

// 创建短链
func (s *ShortURLService) Shorten(ctx context.Context, longURL string) (string, error) {
    // 1. 已存在则直接返回（幂等）
    if code, _ := s.redis.Get(ctx, "long:"+md5Hex(longURL)).Result(); code != "" {
        return code, nil
    }

    // 2. 生成全局 ID
    id, err := s.idGen.Next()
    if err != nil {
        return "", err
    }

    // 3. Base62 编码为短码
    code := toBase62(id)

    // 4. 持久化
    _, err = s.db.ExecContext(ctx,
        "INSERT INTO short_urls(code, long_url, created_at) VALUES(?, ?, NOW())",
        code, longURL)
    if err != nil {
        return "", err
    }

    // 5. 缓存
    s.redis.Set(ctx, "short:"+code, longURL, 7*24*time.Hour)
    s.redis.Set(ctx, "long:"+md5Hex(longURL), code, 7*24*time.Hour)

    return code, nil
}

// 跳转
func (s *ShortURLService) Resolve(ctx context.Context, code string) (string, error) {
    // 缓存
    if url, err := s.redis.Get(ctx, "short:"+code).Result(); err == nil {
        return url, nil
    }
    // 回源 DB
    var url string
    err := s.db.QueryRowContext(ctx,
        "SELECT long_url FROM short_urls WHERE code=?", code).Scan(&url)
    if err != nil {
        return "", err
    }
    s.redis.Set(ctx, "short:"+code, url, 7*24*time.Hour)
    return url, nil
}
```

### 深度剖析

- **为什么用 Base62 而不是 hash**：hash 有碰撞，Base62 从递增 ID 转换保证唯一
- **为什么用雪花算法**：分布式 ID 唯一 + 单调递增，天然防冲突
- **短码长度**：Base62 6 位 = 62^6 ≈ 568 亿，够用
- **跳转性能**：Redis 缓存命中率 99%+，QPS 可上百万
- **恶意扫描**：加频率限制，或短码里加校验位

---

## 14. context 超时传递陷阱

### 场景
用户配置了 5s 超时，但 API 调用发现某个下游 30s 才超时。为什么？

### 考察点
context 传递、超时叠加、`context.WithoutCancel`

### 答案

**问题代码**：

```go
func Handler(w http.ResponseWriter, r *http.Request) {
    // 用户请求带 5s 超时
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()

    result, err := query(ctx)  // 应该 5s 超时
    // ...
}

func query(ctx context.Context) (*Result, error) {
    // ❌ 用了 context.Background()，丢了上层的超时
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    return db.QueryContext(ctx, "...")
}
```

**修复**：

```go
func query(ctx context.Context) (*Result, error) {
    // ✅ 用传入的 ctx，超时会累积
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    // 实际超时 = min(上层 5s, 本层 30s) = 5s
    return db.QueryContext(ctx, "...")
}
```

**特殊场景：异步任务**（Go 1.21+）：

```go
func Handler(w http.ResponseWriter, r *http.Request) {
    // 用户请求 ctx 会在响应返回后被 cancel
    userCtx := r.Context()

    // ❌ 如果直接用 userCtx，请求返回后异步任务被取消
    go doAsync(userCtx)

    // ✅ 用 WithoutCancel 保留 ctx 值但不受取消影响
    asyncCtx := context.WithoutCancel(userCtx)
    go doAsync(asyncCtx)

    w.Write([]byte("ok"))
}
```

### 深度剖析

- **原则**：context 只传递超时/取消/traceID，不作为参数容器
- **超时是"最短"**：多层 WithTimeout 取最短的那个
- **千万别 context.Background()**：会丢失上层的所有信号
- **`context.WithoutCancel`（Go 1.21+）**：保留 value，切断 cancel

---

## 15. 高并发计数器

### 场景
统计文章 PV/UV，QPS 10 万。用什么方案？

### 考察点
`atomic`、分段锁、Redis、批量落库

### 答案

**方案 A：单机 atomic**

```go
var pv atomic.Int64

func IncPV() {
    pv.Add(1)
}
```

单机极限 QPS 千万级，但多实例聚合有问题。

**方案 B：分段计数器**（假共享优化）

```go
type Counter struct {
    shards [64]atomic.Int64  // 64 个 shard，减少 cache line 竞争
}

func (c *Counter) Inc() {
    n := runtime.NumGoroutine() % 64  // 或用 goroutine ID hash
    c.shards[n].Add(1)
}

func (c *Counter) Get() int64 {
    var total int64
    for i := range c.shards {
        total += c.shards[i].Load()
    }
    return total
}
```

**方案 C：Redis + 批量落库**（生产推荐）

```go
type PVCounter struct {
    redis *redis.Client
}

// 上报（Redis 原子累加）
func (c *PVCounter) Inc(articleID int64) {
    c.redis.HIncrBy(context.Background(), "pv:today", strconv.FormatInt(articleID, 10), 1)
}

// 定时批量落库
func (c *PVCounter) FlushLoop(ctx context.Context) {
    ticker := time.NewTicker(time.Minute)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            data, _ := c.redis.HGetAll(ctx, "pv:today").Result()
            for id, count := range data {
                db.Exec("UPDATE articles SET pv=pv+? WHERE id=?", count, id)
            }
            c.redis.Del(ctx, "pv:today")
        }
    }
}
```

### 深度剖析

- **atomic 的坑**：多个 CPU 修改同一 cache line 时性能反而差（伪共享）
- **UV 用什么**：Redis HyperLogLog（`PFADD`），空间只要 12KB 存亿级去重
- **异地多活**：本地累加 → 中心 Redis 聚合 → 落库

---

## 16. 分布式 ID 生成器

### 场景
分库分表环境下，如何生成全局唯一 ID？

### 考察点
雪花算法、UUID、号段模式、时钟回拨

### 答案

**方案 A：Snowflake（推荐）**

```go
// 64 位：1(符号) + 41(毫秒时间戳) + 10(机器ID) + 12(序列号)
type Snowflake struct {
    mu           sync.Mutex
    lastTS       int64
    machineID    int64  // 0-1023
    sequence     int64  // 0-4095
    epoch        int64  // 起始时间戳（如 2020-01-01）
}

const (
    machineBits  = 10
    sequenceBits = 12
    machineMax   = -1 ^ (-1 << machineBits)  // 1023
    sequenceMax  = -1 ^ (-1 << sequenceBits) // 4095
    timeShift    = machineBits + sequenceBits // 22
    machineShift = sequenceBits               // 12
)

func (s *Snowflake) Next() (int64, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    now := time.Now().UnixMilli()
    if now < s.lastTS {
        return 0, errors.New("clock moved backwards")  // 时钟回拨
    }

    if now == s.lastTS {
        s.sequence = (s.sequence + 1) & sequenceMax
        if s.sequence == 0 {
            // 序列号用完，等下一毫秒
            for now <= s.lastTS {
                now = time.Now().UnixMilli()
            }
        }
    } else {
        s.sequence = 0
    }
    s.lastTS = now

    id := ((now - s.epoch) << timeShift) | (s.machineID << machineShift) | s.sequence
    return id, nil
}
```

**方案 B：号段模式**（美团 Leaf）

```
1. 每个业务在 DB 里有一个 seq 表：biz_tag, max_id, step
2. 服务启动时批量拿一段（如 1000 个）到内存
3. 内存用完再取下一段（异步预取）
4. UPDATE seq SET max_id = max_id + step WHERE biz_tag = ?
```

**方案 C：Redis INCR**

```go
id, _ := redis.Incr(ctx, "id:order").Result()
```

### 深度剖析

| 方案 | 优点 | 缺点 |
|------|------|------|
| UUID | 简单，无中心 | 无序，索引差 |
| Snowflake | 有序，高性能 | 时钟回拨、机器 ID 分配 |
| 号段模式 | 高性能，可读 | 依赖 DB |
| Redis INCR | 简单 | 依赖 Redis，单点 |

**时钟回拨怎么办**：
- 直接报错拒绝（简单）
- 等待时钟追上（回拨小）
- 换机器 ID（回拨大）

---

## 17. 订单超时关闭

### 场景
用户下单后 30 分钟未支付，需要自动关闭订单并释放库存。100 万订单量。

### 考察点
延迟队列、Redis ZSet、Kafka 延迟消息

### 答案

**方案 A：Redis ZSet 延迟队列**

```go
type OrderCloser struct {
    redis *redis.Client
}

// 下单时加入延迟队列
func (c *OrderCloser) AddOrder(ctx context.Context, orderID string) {
    expireAt := time.Now().Add(30 * time.Minute).Unix()
    c.redis.ZAdd(ctx, "order:timeout", redis.Z{
        Score:  float64(expireAt),
        Member: orderID,
    })
}

// 后台扫描
func (c *OrderCloser) ScanLoop(ctx context.Context) {
    ticker := time.NewTicker(time.Second)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            now := time.Now().Unix()
            // 拿到过期的订单
            orders, _ := c.redis.ZRangeByScore(ctx, "order:timeout", &redis.ZRangeBy{
                Min:   "0",
                Max:   strconv.FormatInt(now, 10),
                Count: 100,
            }).Result()

            for _, orderID := range orders {
                // Lua 保证：拿到 + 删除是原子的（避免多实例重复处理）
                if c.redis.ZRem(ctx, "order:timeout", orderID).Val() == 1 {
                    go c.closeOrder(ctx, orderID)
                }
            }
        }
    }
}
```

**方案 B：RocketMQ 延迟消息**

```go
producer.SendSync(ctx, &primitive.Message{
    Topic: "OrderTimeout",
    Body:  []byte(orderID),
}, primitive.WithDelayTimeLevel(16))  // 30 分钟对应的级别
```

**方案 C：时间轮**（进程内，适合小规模）

### 深度剖析

- **ZSet 方案要点**：多实例扫描要 ZREM 保证只有一个成功
- **精度**：ZSet 秒级，MQ 分钟级（RocketMQ 有固定的 delay level）
- **规模**：ZSet 单 key 元素太多会慢，可以按小时分片
- **业务幂等**：关闭订单要幂等，防止重复处理

---

## 18. 热点 key 探测与打散

### 场景
某个明星发微博，评论区的 Redis key 被打爆了。怎么办？

### 考察点
热点探测、本地缓存、key 打散、多副本

### 答案

**方案 A：本地缓存兜底**

```go
type HotKeyCache struct {
    local *bigcache.BigCache
    redis *redis.Client
}

func (c *HotKeyCache) Get(ctx context.Context, key string) ([]byte, error) {
    // L1: 本地缓存（1s 短 TTL，容忍短暂不一致）
    if v, err := c.local.Get(key); err == nil {
        return v, nil
    }

    // L2: Redis + singleflight
    v, err, _ := c.sf.Do(key, func() (any, error) {
        return c.redis.Get(ctx, key).Bytes()
    })
    if err != nil {
        return nil, err
    }
    c.local.Set(key, v.([]byte))
    return v.([]byte), nil
}
```

**方案 B：热 key 打散**

```go
// 写入时，把一个 key 拆成多份
func WriteHotKey(ctx context.Context, key, val string) {
    for i := 0; i < 10; i++ {
        redis.Set(ctx, fmt.Sprintf("%s:%d", key, i), val, 5*time.Minute)
    }
}

// 读取时随机选一份
func ReadHotKey(ctx context.Context, key string) (string, error) {
    n := rand.Intn(10)
    return redis.Get(ctx, fmt.Sprintf("%s:%d", key, n)).Result()
}
```

**方案 C：热点探测**（阿里 JVM-Sandbox 思路）

```go
type HotDetector struct {
    counter sync.Map  // key -> *atomic.Int64
}

func (h *HotDetector) Record(key string) {
    v, _ := h.counter.LoadOrStore(key, new(atomic.Int64))
    n := v.(*atomic.Int64).Add(1)

    if n > 10000 {  // 1 分钟内访问 > 10000 次视为热 key
        h.promoteToLocalCache(key)
    }
}
```

### 深度剖析

- **本地缓存 TTL 短**：容忍短暂不一致，换取压力下沉
- **打散的代价**：写放大 N 倍，读要考虑一致性（可能读到不同副本的旧数据）
- **热 key 定位**：Redis 4.0+ 用 `redis-cli --hotkeys`，或客户端埋点上报
- **架构层面**：CDN、多级缓存、读写分离

---

## 19. 平滑重启：零停机部署

### 场景
线上服务发版时不能中断请求。如何做到？

### 考察点
优雅关闭、SIGTERM、连接排空、`http.Server.Shutdown`

### 答案

```go
func main() {
    srv := &http.Server{
        Addr:    ":8080",
        Handler: router,
    }

    // 启动 HTTP 服务
    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("listen: %v", err)
        }
    }()
    log.Println("server started")

    // 监听退出信号
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("shutdown signal received")

    // 优雅关闭：最多等 30 秒让已有请求完成
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := srv.Shutdown(ctx); err != nil {
        log.Fatalf("forced shutdown: %v", err)
    }

    // 清理其他资源
    db.Close()
    redis.Close()
    log.Println("server exited")
}
```

**K8s 场景**：

```yaml
spec:
  terminationGracePeriodSeconds: 60  # 给 pod 60 秒关闭
  containers:
    - name: app
      lifecycle:
        preStop:
          exec:
            command: ["sleep", "5"]  # 先睡 5s，等 endpoint 摘除
```

### 深度剖析

- **为什么 preStop 要 sleep**：K8s 发 SIGTERM 和从 endpoint 摘除是并行的，先 sleep 让 LB 停止转发新流量
- **30s 是怎么定的**：略小于 K8s 的 `terminationGracePeriodSeconds`，留时间给 K8s 强杀
- **长连接的坑**：WebSocket 之类不会主动关闭，需要业务层通知客户端重连
- **数据库连接**：`db.Close()` 会等所有正在使用的连接归还

---

## 20. 链路追踪：TraceID 传递

### 场景
微服务架构，一个请求经过 5 个服务，如何串联日志？

### 考察点
OpenTelemetry、W3C Trace Context、context 传递

### 答案

**入口生成 TraceID**：

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
)

func TraceMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        tracer := otel.Tracer("api-gateway")
        ctx, span := tracer.Start(r.Context(), "http.request")
        defer span.End()

        // 把 TraceID 塞进日志上下文
        traceID := span.SpanContext().TraceID().String()
        ctx = context.WithValue(ctx, "traceID", traceID)

        // 响应头带出，方便前端排查
        w.Header().Set("X-Trace-ID", traceID)

        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

**跨服务传递**（HTTP 客户端注入 header）：

```go
import (
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// otelhttp 自动把 traceparent header 注入请求
client := &http.Client{
    Transport: otelhttp.NewTransport(http.DefaultTransport),
}

req, _ := http.NewRequestWithContext(ctx, "GET", "http://user-svc/api/user", nil)
resp, _ := client.Do(req)
// 请求头会自动带上：
// traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
```

**日志带 TraceID**：

```go
func Log(ctx context.Context, msg string) {
    traceID, _ := ctx.Value("traceID").(string)
    log.Printf("[trace=%s] %s", traceID, msg)
}
```

### 深度剖析

- **W3C Trace Context**：`traceparent` header 是新标准，取代旧的 Zipkin/Jaeger 私有格式
- **采样**：全部追踪成本高，通常按比例采样（如 1%）
- **可视化**：Jaeger、SkyWalking、Elastic APM、Datadog
- **和日志/指标结合**：Log → Trace → Metric 一体化观测（OpenTelemetry 目标）

---

## 附：面试答题框架

### 通用回答结构

```
1. 澄清需求（1 分钟）
   - QPS、数据量、一致性要求、可用性要求
   - 一个模糊场景问清楚再答，别急着写代码

2. 提炼核心考察点（30 秒）
   - 说出关键词：这题在考"缓存穿透 + 击穿 + 分布式锁"

3. 给出方案框架（1 分钟）
   - 画架构图（口述），说主要组件

4. 展开细节（2-3 分钟）
   - 关键代码片段
   - 边界条件（超时、失败、并发）
   - 性能瓶颈

5. 权衡取舍（30 秒）
   - 方案 A vs B 的对比
   - 生产环境下的额外考虑
```

### 加分项

- 主动提"如果流量再涨 10 倍怎么办"
- 引用真实开源项目：`etcd 的 raft`、`redis 的 cluster`
- 提到监控和降级：`加个熔断保护下游`
- 谈成本：`这方案好，但需要额外部署 Kafka，成本高`

### 减分项

- 上来就写代码，不问需求
- 只说"用 Redis 就好了"，不说细节
- 忽略错误处理、超时、并发安全
- 面试官问"为什么"答不上来

---

## 21. MySQL 分库分表方案

### 场景
订单表数据量从 1000 万涨到 10 亿，单表查询 3 秒起，磁盘 2TB 快满了。如何设计分库分表？

### 考察点
分片键选择、分片算法、全局 ID、跨库查询、扩容、中间件选型

---

### 何时分库分表

**先问自己 4 个问题**，能优化就别分：

| 症状 | 优先手段 | 什么时候不得不分 |
|-----|---------|----------------|
| 单表 > 1 亿行、查询慢 | 索引优化、SQL 改写 | 优化完还是慢，才分表 |
| 磁盘空间快满 | 归档冷数据、分区表 | 归档也不够用 |
| 单库连接数打满 | 连接池优化、读写分离 | 读写分离仍不够 |
| CPU/IO 达到瓶颈 | 加大规格、读写分离 | 无法再扩容 |

**分库分表是最后手段**，因为复杂度呈指数上升（跨库事务、聚合、扩容都是坑）。

---

### 一、垂直拆分 vs 水平拆分

#### 垂直拆分：按字段/表拆

**按字段拆**：把大表按字段冷热拆成两张表（一对一关联）。

```sql
-- 拆分前：一张 100+ 字段的大表
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    name VARCHAR(64),
    age INT,
    ...
    bio TEXT,          -- 长文本，很少用
    extra_json JSON,   -- 大字段
    ...
);

-- 拆分后
CREATE TABLE users_core (       -- 热字段：高频查询
    id BIGINT PRIMARY KEY,
    name VARCHAR(64),
    age INT
);

CREATE TABLE users_extra (      -- 冷字段：偶尔查询
    user_id BIGINT PRIMARY KEY,
    bio TEXT,
    extra_json JSON
);
```

**按业务拆库**：把 `users` 库、`orders` 库、`products` 库分开部署。

**适合**：单表字段多 + 冷热差异大；不同业务模块耦合低。

#### 水平拆分：按行拆

**按行拆**：把 1 张 10 亿行的表拆成 128 张 780 万行的表。

```
orders  →  orders_0000
           orders_0001
           orders_0002
           ...
           orders_0127
```

**适合**：单表数据量太大；本文重点讲这个。

---

### 二、分片键（Sharding Key）选择

分片键是**决定一行数据落在哪张表**的字段。选错了会翻车。

#### 好的分片键特征

1. **数据分布均匀**（避免热点）
2. **查询高频命中**（避免全分片扫描）
3. **不需要跨分片事务**（尽量单分片操作）

#### 常见业务的分片键

| 业务 | 分片键 | 原因 |
|-----|-------|------|
| 订单 | `user_id` | 用户查自己订单 = 单分片；订单号需要能反查 user_id |
| IM 消息 | `session_id` | 会话内查询单分片 |
| 商品 | `merchant_id` 或 `product_id` | 按商家或商品维度均匀 |
| 日志 | 时间（`date`） | 冷热分离 + 按天归档 |

#### 反面教材

```
❌ 按 status（订单状态）分：只有几种状态 → 数据严重倾斜
❌ 按 create_time 分（用户查询场景）：查询要跨多个分片
❌ 用自增 ID 分片：新数据全落到最后一个分片，写入热点
```

#### 订单表的经典难题：user_id vs order_id

订单业务两个高频查询：
- **用户查自己的订单**：按 `user_id` 查最方便
- **客服/系统按订单号查**：按 `order_id` 查

**解法**：**订单号里嵌入 user_id 的分片信息**

```go
// 订单号生成规则
orderID = timestamp(8位) + shardKey(4位) + sequence(6位)
//                          ↑ user_id % 128 编码在这

// 通过订单号能直接算出分片
shardIndex := parseShardFromOrderID(orderID)
```

这样两种查询都能定位单分片。

---

### 三、分片算法

#### 算法 1：取模（Hash 分片）

```go
shardIndex := userID % 128  // 128 张分表
// user_id=42 → orders_0042
// user_id=170 → orders_0042（170 % 128 = 42）
```

**优点**：分布均匀
**缺点**：**扩容极难**（从 128 张扩到 256 张，几乎所有数据都要重分布）

```go
func GetShard(userID int64) string {
    return fmt.Sprintf("orders_%04d", userID%128)
}
```

#### 算法 2：范围分片

```
user_id 0    ~ 10M    → orders_0
user_id 10M ~ 20M     → orders_1
user_id 20M ~ 30M     → orders_2
...
```

**优点**：扩容简单（加新范围就行）
**缺点**：**新数据热点**（新注册用户都落最后一个分片）

#### 算法 3：一致性 Hash

```
Hash 环上放 128 个虚拟节点，每个物理分片对应多个虚拟节点：
       0
       │
       ┌──── orders_0
       │
       └──── orders_1
       │
       ┌──── orders_2
       ...
       360 度

user_id 算 hash → 顺时针找第一个节点 → 那就是它的分片
```

**优点**：**扩容影响的数据少**（只影响相邻节点，不需要全量重分布）
**缺点**：实现复杂，取模够用时不必上一致性 Hash

#### 算法 4：**基因法（推荐用于订单场景）**

分片键 hash 值的低 N 位嵌入到订单号里。

```
user_id = 12345
shard = user_id % 128 = 57

order_id 生成：
  时间戳(10位)  |  57（低7位嵌入这里）  |  自增序列(N位)

订单号 = "1735689600" + "0111001" + "000001"
                       ↑
                    直接读出分片 57

之后：
- 按 user_id 查：hash(user_id) % 128
- 按 order_id 查：读订单号里的分片位
```

**这就是美团、饿了么订单表的常见做法**。

---

### 四、库表数量怎么定

```
容量估算：
  预期 3 年数据量：50 亿
  单表建议：< 5000 万（保证 B+ 树 3 层，索引查询快）
  → 需要分表数 = 50 亿 / 5000 万 = 100
  → 取 2 的幂：128（方便扩容和取模）

库数量：
  单库连接数上限：MySQL 默认 151，生产调到 1000-3000
  预期总 QPS：10 万
  单库承载：5000 QPS
  → 需要 20 个库
  → 取 2 的幂：16 或 32
```

**经典配置**：`16 库 × 8 表 = 128 张分表`（分片数 128）

```
db_0.orders_0    db_0.orders_1  ... db_0.orders_7
db_1.orders_0    ...
db_2.orders_0    ...
...
db_15.orders_0   ...            db_15.orders_7

分片路由：
  shard = user_id % 128
  db_index = shard / 8
  table_index = shard % 8
```

**规则**：库表总数 = 2 的幂，扩容时可以直接**加倍**。

---

### 五、Go 层实现

#### 简单分片路由

```go
type ShardRouter struct {
    dbs        []*gorm.DB  // 16 个库
    tableCount int         // 每库 8 表
}

func (r *ShardRouter) Route(userID int64) (*gorm.DB, string) {
    total := len(r.dbs) * r.tableCount    // 128
    shard := int(userID % int64(total))   // 0-127
    dbIndex := shard / r.tableCount       // 0-15
    tableIndex := shard % r.tableCount    // 0-7

    return r.dbs[dbIndex], fmt.Sprintf("orders_%d", tableIndex)
}

// 查询
func (r *ShardRouter) FindByUserID(userID int64) ([]*Order, error) {
    db, table := r.Route(userID)

    var orders []*Order
    err := db.Table(table).
        Where("user_id = ?", userID).
        Find(&orders).Error
    return orders, err
}

// 写入
func (r *ShardRouter) Insert(order *Order) error {
    db, table := r.Route(order.UserID)
    return db.Table(table).Create(order).Error
}
```

#### 用中间件更优

生产不建议自己写路由（复杂 SQL、事务、join 都要重写），用现成中间件：

| 中间件 | 定位 | 特点 |
|-------|------|------|
| **ShardingSphere-Proxy** | 独立代理 | 支持所有语言，SQL 透明 |
| **ShardingSphere-JDBC** | Java 客户端 | 只支持 Java |
| **Vitess** | Google 出品 | YouTube 生产验证，K8s 友好 |
| **MyCat** | 老牌中间件 | 国内早期主流，社区活跃度下降 |
| **TiDB** | NewSQL | 兼容 MySQL 协议，自带分片，云原生 |

Go 项目常用组合：
- 中小规模：**Go 应用层自己路由**（简单可控）
- 大规模：**ShardingSphere-Proxy** 或直接上 **TiDB**

---

### 六、跨分片难题

分库分表后，很多操作变复杂。

#### 难题 1：跨分片聚合

```sql
-- 查某商品在所有用户下的销量
SELECT product_id, SUM(qty) FROM orders WHERE product_id = 101 GROUP BY product_id;
```

**分片键是 user_id**，这个查询要**扫所有 128 张表**再聚合。

**解决方案**：
1. **异构索引表**：额外维护一张 `product_stats` 表，按 product_id 分片
2. **数仓/OLAP**：明细走分片 MySQL，聚合查询走 ClickHouse / Doris
3. **ES**：同步订单到 ES 做搜索和聚合

#### 难题 2：跨分片 join

**根本原则**：**能避免就避免**。业务上尽量把关联查询拆成两次查询。

```go
// ❌ 期望 join：SELECT * FROM orders o JOIN users u ON o.user_id = u.id WHERE ...
//    分库后 join 不了

// ✅ 拆成两步
orders := orderService.FindByShard(userID)
userIDs := extractUserIDs(orders)
users := userService.MGet(userIDs)  // 批量查
result := merge(orders, users)
```

#### 难题 3：跨分片事务

**分布式事务**是分库最难的问题：

- **XA 事务**：MySQL 支持但性能差、故障恢复复杂
- **TCC**：Try-Confirm-Cancel，业务侵入大
- **Saga**：长事务，最终一致
- **本地消息表 + MQ**：常见方案，最终一致

**实践建议**：**尽量把事务限制在单个分片内**（这就是选对分片键的关键）。跨分片操作用**最终一致性 + 补偿**。

#### 难题 4：分页

```sql
-- 全库分页 LIMIT 1000000, 10
```

分片后不能直接做。方案：
1. **禁止深度分页**：产品限制翻页深度（如最多翻 100 页）
2. **游标分页**：`WHERE id > last_id ORDER BY id LIMIT 10`
3. **业务上不需要真分页**：按时间倒序，用户只关心最近的

---

### 七、扩容方案

假设从 128 张分表扩到 256 张，怎么做？

#### 方案 1：停机扩容（简单粗暴）

1. 停服务
2. 全量导出数据
3. 按新规则重新分片写入
4. 切换配置
5. 启动服务

**只适合小规模**，业务不能停几小时。

#### 方案 2：双写切换（生产主流）

```
Step 1: 新老集群并存，应用双写（同时写老 128 和新 256）
Step 2: 后台任务把老集群历史数据按新规则迁移到新集群
Step 3: 数据一致性校验
Step 4: 切读流量到新集群
Step 5: 观察一段时间，OK 后停止双写，废弃老集群
```

**过程可能持续几周**，需要精细的进度管理和回滚方案。

#### 方案 3：翻倍扩容（最简单）

分表数从 128 → 256（严格翻倍），**每个老分片的数据只需要迁移一半**：

```
老分片 42 → shard = user_id % 128 = 42
新分片规则：shard = user_id % 256

user_id % 128 = 42 的用户，在 mod 256 下：
  - user_id % 256 = 42  → 留在 orders_42
  - user_id % 256 = 170 → 迁到 orders_170（170 - 128 = 42）
```

**优势**：每个老分片只有 50% 数据要迁走，迁移量最少。

**这就是分表数选 2 的幂的核心原因**——为了翻倍扩容。

---

### 八、全局 ID 生成

分库分表后不能用 MySQL 自增 ID（各分片会冲突）。参考[第 16 题](#16-分布式-id-生成器)：

- **Snowflake**：本地生成，无中心依赖
- **号段模式（美团 Leaf）**：DB 分配号段，本地消耗
- **Redis INCR**：简单，但依赖 Redis

订单场景推荐 **Snowflake + 分片信息编码**（前面讲的基因法）。

---

### 九、深度剖析

#### 分库分表 vs 其他方案

| 方案 | 适合 | 优点 | 缺点 |
|-----|-----|------|------|
| **单库 + 索引优化** | < 1 亿 | 简单 | 有上限 |
| **单库 + 分区表** | 1-3 亿 | 无侵入 | 只解决存储，不解决 QPS |
| **读写分离** | 读多写少 | 简单 | 主从延迟，主库写压力不减 |
| **分库分表** | 数据量大 + 高 QPS | 水平扩展 | 复杂度高 |
| **TiDB（NewSQL）** | 大表 + 分布式事务 | 自动分片，兼容 MySQL | 运维复杂，性能不如原生 MySQL |
| **PolarDB / Aurora** | 云上 | 存算分离，秒级弹性 | 云厂商锁定 |

**选型思路**：能不分就不分 → 能云原生（TiDB/PolarDB）就云原生 → 最后才自己分库分表。

#### 生产常见坑

1. **分片键设计错，后期极难改**：一定要在设计初期想清楚 3 年后的业务
2. **忽略跨分片的报表需求**：BI 团队天天问总销量，你分片键是 user_id → 抓瞎
3. **单元测试不覆盖分片路由**：生产遇到路由 bug 才发现
4. **DDL 变更**：加字段要 128 张表一起改，用 online DDL 工具（gh-ost / pt-osc）
5. **备份和恢复**：128 张表的备份策略 vs 单表完全不同

---

### 十、常用工具

| 工具 | 用途 |
|-----|------|
| **ShardingSphere** | 分库分表中间件（Java/Proxy） |
| **Vitess** | Google 分库方案（Go 生态） |
| **DBLE** | 国产 MyCat 分支，性能好 |
| **gh-ost** | 在线 DDL 变更 |
| **pt-osc** | Percona 的在线 DDL |
| **Canal** | 订阅 binlog，做数据同步 |
| **DataX / Flink CDC** | 数据迁移 |

---

### 十一、面试高频追问

**Q1: 为什么分表数要选 2 的幂？**

翻倍扩容时数据迁移量最少。128 → 256 只需迁一半数据；128 → 200 要迁 90% 数据。

**Q2: 分片键选 user_id，怎么按订单号查？**

订单号里嵌入 user_id 的分片信息（基因法）。生成 order_id 时把 `user_id % 128` 编码进去，查询时能直接算出分片。

**Q3: 128 张分表后，报表需要总销量怎么办？**

不用 MySQL 做聚合，用异构索引表 / 数仓 / ES / OLAP（ClickHouse / Doris）。分片 MySQL 只做在线业务查询，分析查询用专门的分析引擎。

**Q4: 分库后事务怎么办？**

三个策略：
1. **单分片内事务**（最好，分片键选对基本能做到）
2. **最终一致性 + 补偿**：本地消息表 / MQ / Saga
3. **强一致场景**（金融）：直接上 TiDB 之类的 NewSQL

**Q5: 扩容会不会影响业务？**

**双写切换方案不需要停服**，但会持续几周，需要：
- 全链路灰度切流
- 数据双写一致性校验
- 完善的回滚方案
- SRE 长时间盯盘

**Q6: 分库分表 vs TiDB 怎么选？**

- **传统分库分表**：性能上限高，运维简单（就是 MySQL），但业务侵入大
- **TiDB**：业务无感（SQL 完全兼容），自带分片和分布式事务，但 P99 延迟比原生 MySQL 高
- **现状**：大厂新业务开始选 TiDB / PolarDB / Aurora；老业务持续用分库分表

**Q7: 为什么单表控制在 5000 万？**

MySQL InnoDB B+ 树 3 层能存约 2000 万行（16KB 页 + 主键 8 字节假设），查询走 3 次磁盘 IO。超过 5000 万 B+ 树可能 4 层，性能下降。

也是经验值，实际根据行大小可以差别很大。

---

## 22. 异步发送短信服务

### 场景

业务需要发短信（注册验证码、支付通知、营销推送）。要求：

- **接口毫秒返回**：不能让用户在下单页等短信通道商响应（可能 500ms ~ 3s）。
- **抗峰值**：秒杀开始瞬间 10 万人请求验证码。
- **QPS 上限**：短信通道商合同 200 QPS，超了要么被限流要么被拉黑。
- **不能重复**：短信按条计费，一条重发一次就是钱；同一手机号 60s 内不能骚扰。
- **失败重试 + 死信**：网络抖动要重试，反复失败要能人工兜底。
- **进程重启不丢消息**：K8s 滚动发布时正在处理的验证码不能丢。

### 考察点

生产者/消费者、MQ 削峰、Worker Pool、Redis 原子频控、`rate.Limiter`、`gobreaker` 熔断、幂等、优雅关闭、DLQ。

### 答案

#### 架构分层

```
业务方 ──HTTP──> SMSService.Send()          （同步：校验/频控/幂等/投 MQ，< 20ms 返回）
                       │
                       ▼
                     Kafka Topic（按优先级分：sms.verify / sms.notify / sms.marketing）
                       │
                       ▼
                  SMSWorker Pool           （异步：限流/熔断/重试）
                       │
                       ├──> 阿里云短信 SDK （主通道）
                       ├──> 腾讯云短信 SDK （备通道，主通道熔断时切）
                       └──> DLQ Topic     （最终失败进死信）
```

#### Producer：同步入口

```go
type SendReq struct {
    Phone    string            // 手机号（E.164 格式）
    Template string            // 模板 ID
    Params   map[string]string // 模板参数
    BizID    string            // 业务方生成的幂等 ID（必填）
    Priority int               // 0=验证码, 1=通知, 2=营销
}

type SMSService struct {
    redis    *redis.Client
    producer sarama.SyncProducer
    logger   *slog.Logger
}

// 频控 Lua：60s 内 1 条，24h 内 5 条
const freqLimitScript = `
local minKey, dayKey = KEYS[1], KEYS[2]
local minLimit, dayLimit = tonumber(ARGV[1]), tonumber(ARGV[2])

if tonumber(redis.call('GET', minKey) or '0') >= minLimit then
    return -1
end
if tonumber(redis.call('GET', dayKey) or '0') >= dayLimit then
    return -2
end
redis.call('INCR', minKey)
redis.call('EXPIRE', minKey, 60)
redis.call('INCR', dayKey)
redis.call('EXPIRE', dayKey, 86400)
return 1
`

func (s *SMSService) Send(ctx context.Context, req *SendReq) error {
    // 1. 参数校验（快速失败，防脏数据进 MQ）
    if !isValidPhone(req.Phone) {
        return ErrInvalidPhone
    }
    if req.BizID == "" {
        return ErrMissingBizID
    }

    // 2. 幂等：同 BizID 24h 内只处理一次
    ok, err := s.redis.SetNX(ctx, "sms:idem:"+req.BizID, 1, 24*time.Hour).Result()
    if err != nil {
        return fmt.Errorf("idem check: %w", err)
    }
    if !ok {
        s.logger.Info("duplicate submit", "biz_id", req.BizID)
        return nil // 幂等：静默成功
    }

    // 3. 频控（验证码通常不做用户频控，由发码策略控制；通知/营销必做）
    if req.Priority > 0 {
        result, err := s.redis.Eval(ctx, freqLimitScript,
            []string{"sms:freq:min:" + req.Phone, "sms:freq:day:" + req.Phone},
            1, 5,
        ).Int()
        if err != nil {
            return fmt.Errorf("freq limit: %w", err)
        }
        switch result {
        case -1:
            return ErrTooFrequent
        case -2:
            return ErrDailyLimitReached
        }
    }

    // 4. 投 MQ（不同优先级不同 topic，验证码不被营销挤压）
    payload, _ := json.Marshal(req)
    topic := topicByPriority(req.Priority)
    _, _, err = s.producer.SendMessage(&sarama.ProducerMessage{
        Topic: topic,
        Key:   sarama.StringEncoder(req.BizID), // 同 BizID 落同一分区，保证顺序
        Value: sarama.ByteEncoder(payload),
    })
    return err
}
```

#### Consumer：Worker Pool + 限流 + 熔断

```go
type SMSChannel interface {
    Send(ctx context.Context, phone, template string, params map[string]string) error
}

type SMSWorker struct {
    consumer sarama.ConsumerGroup
    primary  SMSChannel // 主通道
    backup   SMSChannel // 备通道
    limiter  *rate.Limiter
    breaker  *gobreaker.CircuitBreaker
    slots    chan struct{}     // worker 并发槽
    inflight sync.WaitGroup    // 追踪在飞消息，用于优雅关闭
    dlq      sarama.SyncProducer
    logger   *slog.Logger
}

func NewSMSWorker(cfg WorkerConfig) *SMSWorker {
    return &SMSWorker{
        consumer: cfg.Consumer,
        primary:  cfg.Primary,
        backup:   cfg.Backup,
        // 全局 QPS 限流，保护下游通道商（合同 200 QPS，留 buffer 到 180）
        limiter: rate.NewLimiter(180, 200),
        breaker: gobreaker.NewCircuitBreaker(gobreaker.Settings{
            Name:        "sms-primary",
            MaxRequests: 3,
            Interval:    10 * time.Second,
            Timeout:     30 * time.Second,
            ReadyToTrip: func(c gobreaker.Counts) bool {
                return c.Requests >= 20 &&
                    float64(c.TotalFailures)/float64(c.Requests) > 0.5
            },
        }),
        slots: make(chan struct{}, cfg.Concurrency), // 例如 50
        dlq:   cfg.DLQ,
    }
}

// 实现 sarama.ConsumerGroupHandler
func (w *SMSWorker) ConsumeClaim(sess sarama.ConsumerGroupSession,
    claim sarama.ConsumerGroupClaim) error {

    for msg := range claim.Messages() {
        // 抢 worker slot（阻塞 = 天然反压：MQ 拉太快，这里会阻塞不 poll 下一条）
        select {
        case w.slots <- struct{}{}:
        case <-sess.Context().Done():
            return nil
        }

        w.inflight.Add(1)
        go func(m *sarama.ConsumerMessage) {
            defer w.inflight.Done()
            defer func() { <-w.slots }()

            if w.process(sess.Context(), m) {
                sess.MarkMessage(m, "") // 只有成功才 commit offset
            }
        }(msg)
    }
    return nil
}

// process 返回 true 表示 commit offset（成功或已进 DLQ），false 表示保留 offset 等下次重投
func (w *SMSWorker) process(ctx context.Context, msg *sarama.ConsumerMessage) bool {
    var req SendReq
    if err := json.Unmarshal(msg.Value, &req); err != nil {
        w.logger.Error("bad payload", "err", err, "offset", msg.Offset)
        return true // 脏数据直接跳过，不然会永远卡住
    }

    log := w.logger.With("biz_id", req.BizID, "phone", maskPhone(req.Phone))

    // 全局限流：Wait 会阻塞直到拿到令牌
    if err := w.limiter.Wait(ctx); err != nil {
        return false // ctx 取消，保留 offset
    }

    // 熔断保护主通道
    start := time.Now()
    _, err := w.breaker.Execute(func() (any, error) {
        return nil, w.primary.Send(ctx, req.Phone, req.Template, req.Params)
    })
    smsLatency.WithLabelValues(req.Template).Observe(time.Since(start).Seconds())

    if err == nil {
        smsSuccess.WithLabelValues(req.Template).Inc()
        return true
    }

    // 主通道熔断中：走备通道（不再进熔断器统计）
    if errors.Is(err, gobreaker.ErrOpenState) {
        log.Warn("primary breaker open, fallback to backup")
        if berr := w.backup.Send(ctx, req.Phone, req.Template, req.Params); berr == nil {
            smsSuccess.WithLabelValues(req.Template).Inc()
            return true
        } else {
            err = berr
        }
    }

    smsFailure.WithLabelValues(req.Template, classify(err)).Inc()

    // 重试次数：Kafka Header 里记（Kafka 本身没有 delivery-count）
    retry := getRetryCount(msg) + 1
    if retry >= 3 {
        log.Error("max retry, send to DLQ", "err", err)
        w.sendToDLQ(msg, err)
        return true
    }

    // 重投带上 retry 计数（生产用带延迟的 topic，如 sms.retry.30s / 5m / 30m）
    w.reQueue(msg, retry)
    return true
}
```

#### 优雅关闭

```go
func main() {
    logger := slog.Default()
    worker := NewSMSWorker(loadConfig())

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // 启动消费
    done := make(chan error, 1)
    go func() {
        for {
            if err := worker.consumer.Consume(ctx, []string{
                "sms.verify", "sms.notify", "sms.marketing",
            }, worker); err != nil {
                if errors.Is(err, context.Canceled) {
                    done <- nil
                    return
                }
                logger.Error("consume", "err", err)
                time.Sleep(time.Second) // 简单退避重连
            }
        }
    }()

    // 监听退出信号
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig
    logger.Info("shutdown signal received")

    // 1. cancel → ConsumeClaim 的 for range 会退出（rebalance 也会退出）
    cancel()

    // 2. 等在飞消息处理完（最多 30s，K8s terminationGracePeriodSeconds 给 60s）
    doneCh := make(chan struct{})
    go func() {
        worker.inflight.Wait()
        close(doneCh)
    }()
    select {
    case <-doneCh:
        logger.Info("all in-flight messages processed")
    case <-time.After(30 * time.Second):
        logger.Warn("shutdown timeout, some messages may be reprocessed")
    }

    _ = worker.consumer.Close()
    <-done
}
```

### 深度剖析

#### 为什么必须走 MQ

- **接口响应时间**：短信通道商 P99 可能 1s+，同步等会把连接池打爆。走 MQ 后接口 < 20ms 返回。
- **削峰**：秒杀瞬间 10 万请求，直接打通道商必被限流。MQ 里排队，Worker 按 180 QPS 均匀消费。
- **解耦**：通道商挂了不影响业务下单，消息在 MQ 里等恢复。

**替代方案对比**：

| 方案 | 优点 | 缺点 | 适合 |
|-----|------|------|------|
| 进程内 channel + Worker Pool | 简单，无外部依赖 | 进程挂消息丢；单机容量上限 | 小规模、对丢消息不敏感 |
| Redis Stream / List | 轻量，运维简单 | 消费者 offset 管理麻烦；无严格顺序保证 | 中等规模 |
| Kafka / RocketMQ | 可靠、高吞吐、可回放 | 运维成本高 | 生产首选 |

#### 幂等和频控的区别

**别混淆**：

- **幂等（BizID）**：解决"消息重复投递"问题。MQ 至少投一次，同一 BizID 可能被 Producer 或 Consumer 处理多次，`SetNX` 保证只发一条。粒度 = 一次业务请求。
- **频控（Phone）**：解决"用户被骚扰"问题。同一手机号 60s 只能收 1 条、24h 5 条，是业务规则。粒度 = 一个手机号。

两者都用 Redis，但 key 和过期时间完全不同。

#### Worker Pool 的关键细节

```go
select {
case w.slots <- struct{}{}:  // 抢槽阻塞 = 反压
case <-sess.Context().Done():
    return nil
}
```

`slots` channel 满了会**阻塞 for range，从而不 poll 下一条消息** —— 这是最简单也最有效的**反压机制**。不需要复杂的信号量或队列长度检测。

**为什么不为每条消息 `go func`**：会瞬间起几万 goroutine，把内存和下游都打爆。Worker 数 = 通道商 QPS × 平均耗时（Little's Law），200 QPS × 0.2s = 40 个够了，设 50 留 buffer。

#### 限流和熔断的分工

- **`rate.Limiter`（180 QPS）**：**保护下游**。合同 200 QPS，留 10% buffer 应对时钟抖动。这是"我不发太快"。
- **`gobreaker`（失败率 50%）**：**保护自己**。下游挂了后立刻停手，切备用通道；30s 后半开探测，避免打无谓的调用。这是"你挂了我不硬撑"。

两者互补：限流是**主动**流量整形，熔断是**被动**故障响应。

#### 优先级分 topic 而不是分 priority 字段

如果所有短信混一个 topic，营销短信积压 100 万条时，验证码要排在后面 → **用户点了发码等半小时**。

分 3 个 topic：
- `sms.verify`：单独消费组，配 30 个 worker，最高优先级。
- `sms.notify`：中等资源。
- `sms.marketing`：可以慢慢发，晚上跑，甚至限速。

不同 topic 独立消费、独立限流、独立配置。

#### 死信和补偿

- **重试策略**：MQ 立刻重投会连续失败。业界做法是**延迟重试**：`sms.retry.30s` → `sms.retry.5m` → `sms.retry.30m`，用 RocketMQ 延迟级别或 Kafka 加消费端 delay。
- **DLQ 处理**：进死信的消息必须有值班盯着（告警接钉钉/PagerDuty），人工判断：
  - 是号码问题（黑名单）→ 丢弃 + 加黑名单表。
  - 是通道问题 → 换通道重发。
  - 是模板问题 → 修模板重发。

#### 优雅关闭要处理的三件事

1. **不再拉新消息**：`cancel()` 后 `Consume` 返回。
2. **在飞消息处理完**：`inflight.Wait()`，超时 30s（略小于 K8s `terminationGracePeriodSeconds`）。
3. **超时兜底**：处理不完的消息不 commit offset，rebalance 后另一个 Pod 会重投（此时幂等 key 生效，不会重复发送）。

#### 生产还需要的东西

- **多通道路由**：主备通道 + 按运营商路由（移动走 A、联通走 B），成本和到达率都能优化。
- **发送流水表**：MySQL 记 `biz_id/phone/template/status/channel_msg_id/created_at`，通道回调后更新 `status`（DELIVERED/FAILED）。
- **对账**：每天和通道商对账单，防止计费和实际发送数量对不上。
- **监控指标**：
  - `sms_send_total{template, status}` — 总量和成功率
  - `sms_channel_latency` — 通道耗时分布
  - `sms_queue_lag` — MQ 积压量
  - `sms_breaker_state` — 熔断器状态
- **敏感数据**：日志里手机号必须脱敏（`138****1234`），验证码内容不能落日志，符合合规要求。

#### 高频追问

**Q1：为什么不用 Redis Stream 代替 Kafka？**

小规模可以（Redis Stream 有 consumer group 和 ACK）。规模大了后：吞吐、持久化、多副本、跨机房容灾，Kafka/RocketMQ 更成熟。选型看数据量和运维能力。

**Q2：Kafka 消息顺序问题会不会影响短信？**

短信业务**不需要严格顺序**（每条消息独立）。如果非要顺序（比如"先发通知再发确认"），把两条消息的 key 设成一样（用 phone 或 biz_id），Kafka 保证同 key 落同分区、同分区内有序。

**Q3：如果 Redis 幂等 key 挂了/清了怎么办？**

幂等的最后一道防线是**通道商侧**：阿里云、腾讯云的 SDK 都支持传 `OutId`（外部业务 ID），通道商侧做去重，24 小时内同 OutId 只发一次。所以 `BizID` 要透传到通道商。

**Q4：验证码发不出去业务方怎么感知？**

同步接口只保证"进 MQ 成功"，不保证"发送成功"。业务方需要：
- **推送状态**：Worker 发送后回调业务方接口（可靠但耦合）。
- **查询接口**：业务方主动查 `GET /sms/status/{bizID}`（松耦合，推荐）。
- **超时兜底**：60s 内没收到验证码，前端允许"重新发送"（此时新 BizID，前一条丢弃即可）。

**Q5：一个 Worker Pod 挂了会丢消息吗？**

不会。Kafka 消费是"处理成功后 commit offset"，Pod 崩溃时未 commit 的消息会被 rebalance 到其他 Pod 重新处理。前提是：**幂等做对了**，同一消息被处理两次不能真的发两条短信 —— 这就是为什么 `BizID` 幂等是必须的。

