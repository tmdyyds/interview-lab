# go-redis 深度剖析

**标签**: #go #ecosystem #redis #go-redis #高频

`redis/go-redis` 是 Go 生态最主流的 Redis 客户端。本文从连接池、Pipeline、事务、Cluster 到常见陷阱，深度剖析。

---

## 一、快速开始

```go
import "github.com/redis/go-redis/v9"

rdb := redis.NewClient(&redis.Options{
    Addr:     "localhost:6379",
    Password: "",
    DB:       0,

    // 连接池配置
    PoolSize:     10,               // 每个 CPU 核心的连接数
    MinIdleConns: 5,                // 最小空闲连接
    PoolTimeout:  4 * time.Second,  // 等待连接的超时

    // 命令超时
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,

    // 重试
    MaxRetries:      3,
    MinRetryBackoff: 8 * time.Millisecond,
    MaxRetryBackoff: 512 * time.Millisecond,
})

// 基本操作
rdb.Set(ctx, "key", "value", time.Hour)
val, err := rdb.Get(ctx, "key").Result()  // val="value"
```

---

## 二、连接池设计

### 为什么要连接池

Redis TCP 连接建立 = **3 次握手 + TLS（如果启用）**。频繁建连开销大：

```
每次新建连接：TCP 三次握手 ~ 1-3ms（同机房）
连接池复用：从池取一个 ~ 微秒级
```

生产 QPS 上万时，不用池根本抗不住。

### PoolSize 怎么配

```go
PoolSize: 10 * runtime.NumCPU()
// 默认 = 10 * NumCPU
```

**关键**：`PoolSize` 是**每个客户端实例的连接数**，不是全局。

**计算公式**：`PoolSize ≥ QPS × 平均 RT / 客户端实例数`

举例：
- QPS 10000，平均 RT 5ms（0.005s）
- 单实例应用：`10000 × 0.005 = 50` 个连接
- 5 个实例：每实例 10 个够

**别设太大**：Redis 单实例最多 10000 连接（`maxclients`），太多客户端会打爆。

### 连接池源码简析

```go
// go-redis/internal/pool/pool.go 简化
type ConnPool struct {
    connsMu    sync.Mutex
    conns      []*Conn      // 所有连接
    idleConns  []*Conn      // 空闲连接
    queue      chan struct{} // 信号量：控制并发获取

    poolSize   int
    dialErrorsNum uint32
}

func (p *ConnPool) Get(ctx context.Context) (*Conn, error) {
    // 1. 信号量：阻塞直到有连接可用（或超时）
    if err := p.waitTurn(ctx); err != nil {
        return nil, err
    }

    // 2. 从空闲池拿
    for {
        p.connsMu.Lock()
        cn := p.popIdle()
        p.connsMu.Unlock()

        if cn == nil {
            break
        }
        if !p.isHealthyConn(cn) {  // 检查连接是否还活着
            p.CloseConn(cn)
            continue
        }
        return cn, nil
    }

    // 3. 池空且未满 → 新建连接
    newcn, err := p.newConn(ctx, true)
    return newcn, err
}
```

**关键机制**：
1. **信号量控制并发**（`queue`）
2. **空闲连接队列**（LIFO 提高缓存命中）
3. **连接健康检查**（拿出来先判断是不是坏连接）
4. **懒创建**（不预分配，用时才建）

---

## 三、Pipeline：批量命令

### 单条命令的问题

```go
// ❌ 100 条命令 = 100 次网络往返（RTT）
for i := 0; i < 100; i++ {
    rdb.Set(ctx, fmt.Sprintf("k%d", i), i, 0)
}
// 假设 RTT=1ms → 100ms 才跑完
```

### Pipeline：一次网络往返

```go
pipe := rdb.Pipeline()
for i := 0; i < 100; i++ {
    pipe.Set(ctx, fmt.Sprintf("k%d", i), i, 0)  // 命令进 buffer，未发送
}
cmds, err := pipe.Exec(ctx)  // 一次性发送，一次接收
// 100 条命令 = 1 次 RTT → 1ms
```

**性能对比**（本机 Redis）：
- 单条：约 20us/次 × 100 = 2ms
- Pipeline：约 200us（99% 节省）

### 用 Pipelined 简化写法

```go
cmds, err := rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
    for i := 0; i < 100; i++ {
        pipe.Set(ctx, fmt.Sprintf("k%d", i), i, 0)
    }
    return nil
})
```

### 拿命令结果

```go
setCmd := pipe.Set(ctx, "k", "v", 0)
getCmd := pipe.Get(ctx, "k")
pipe.Exec(ctx)

// 每个命令的返回值
err := setCmd.Err()
val, err := getCmd.Result()
```

### Pipeline 不是事务

**Pipeline ≠ 原子性**。中间某条失败，其他还是会执行。要原子性用 `TxPipeline`（MULTI/EXEC）。

---

## 四、事务：MULTI / EXEC

```go
// TxPipeline = MULTI + 命令 + EXEC
_, err := rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
    pipe.Incr(ctx, "counter")
    pipe.Expire(ctx, "counter", time.Hour)
    return nil
})
```

**关键理解**：
- Redis 事务是**乐观锁**，不是传统数据库事务
- 命令进队列 → `EXEC` 时**一次性顺序执行**
- 中间**不能回滚**（Redis 没有回滚机制，某条失败其他照跑）

### WATCH：乐观锁

```go
// 转账场景：读余额 → 判断 → 扣款
err := rdb.Watch(ctx, func(tx *redis.Tx) error {
    balance, err := tx.Get(ctx, "balance:u1").Int()
    if err != nil {
        return err
    }
    if balance < 100 {
        return errors.New("insufficient")
    }

    // Pipelined 内的命令进队列
    _, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
        pipe.DecrBy(ctx, "balance:u1", 100)
        pipe.IncrBy(ctx, "balance:u2", 100)
        return nil
    })
    return err
}, "balance:u1")  // WATCH 这个 key
// 如果在 WATCH 到 EXEC 之间 balance:u1 被别人修改了 → 事务失败，返回 redis.TxFailedErr
```

**用途**：CAS 场景。**注意重试**：

```go
for i := 0; i < 5; i++ {
    err := rdb.Watch(ctx, txFunc, "key")
    if err == nil {
        break  // 成功
    }
    if err == redis.TxFailedErr {
        continue  // key 变了，重试
    }
    return err  // 其他错误
}
```

**为什么不用 Redis 事务**：大多数场景 **Lua 脚本更好**（后面讲）。Watch 事务用得很少。

---

## 五、Lua 脚本：真正的原子操作

### 为什么用 Lua

Redis 单线程 + Lua 一次执行完，中间不会被其他命令插入。**天然原子**，比 MULTI/EXEC 更强。

### 秒杀经典例子

```go
const stockDeductScript = `
local stock = tonumber(redis.call('GET', KEYS[1]))
if stock == nil or stock <= 0 then
    return -1
end
return redis.call('DECR', KEYS[1])
`

// 方式一：每次都发脚本内容
result, _ := rdb.Eval(ctx, stockDeductScript, []string{"stock:p1"}).Int()

// 方式二：EVALSHA（推荐，脚本预加载到 Redis）
script := redis.NewScript(stockDeductScript)
result, err := script.Run(ctx, rdb, []string{"stock:p1"}).Int()
// 内部先 EVALSHA，如果 NOSCRIPT 错误就 fallback 到 EVAL 并 SCRIPT LOAD
```

### Lua 陷阱

1. **禁止无界循环**：Redis 单线程，Lua 阻塞所有客户端
2. **别调随机命令**：`RANDOMKEY` 会导致主从不一致（除非 `redis.replicate_commands()`）
3. **KEYS 参数要传完**：所有涉及的 key 都放 KEYS，方便集群路由

---

## 六、Redis Cluster 客户端

```go
rdb := redis.NewClusterClient(&redis.ClusterOptions{
    Addrs: []string{
        "cluster-node-1:6379",
        "cluster-node-2:6379",
        "cluster-node-3:6379",
    },
})
```

### 客户端如何路由

```
1. 启动时向任意节点发 CLUSTER SLOTS
2. 拿到 slot → node 的映射表（缓存到本地）
3. 每次命令：CRC16(key) % 16384 → slot → 找对应 node
4. 如果 node 返回 MOVED/ASK → 更新本地映射表 + 重发
```

### 多 key 命令的坑

```go
// ❌ 跨 slot 会报错
rdb.MGet(ctx, "user:1", "user:2", "user:3")
// CROSSSLOT Keys in request don't hash to the same slot

// ✅ 用 hash tag 强制同 slot
rdb.MGet(ctx, "user:{group1}:1", "user:{group1}:2", "user:{group1}:3")
// 花括号里的部分参与 hash 计算，保证同 slot
```

### 读写分离

```go
rdb := redis.NewClusterClient(&redis.ClusterOptions{
    Addrs:    addrs,
    ReadOnly: true,  // 允许从副本读
    RouteRandomly: true,  // 随机选副本
})
```

**注意**：从副本读有主从延迟，强一致场景不能用。

---

## 七、Sentinel 客户端

```go
rdb := redis.NewFailoverClient(&redis.FailoverOptions{
    MasterName:    "mymaster",
    SentinelAddrs: []string{"s1:26379", "s2:26379", "s3:26379"},
})
// 自动感知主从切换
```

---

## 八、扫描大 key：SCAN 而不是 KEYS

```go
// ❌ KEYS 会阻塞 Redis（O(n)，key 多时几秒卡死）
keys, _ := rdb.Keys(ctx, "user:*").Result()

// ✅ 用 SCAN 迭代（每次拿一小批）
iter := rdb.Scan(ctx, 0, "user:*", 100).Iterator()
for iter.Next(ctx) {
    key := iter.Val()
    // 处理 key
}
if err := iter.Err(); err != nil {
    return err
}
```

**SCAN 特点**：
- 非阻塞（每次少量）
- 可能漏（迭代期间新增/删除的 key）
- 可能重复（客户端要自己去重）

### 类似的还有 HSCAN / SSCAN / ZSCAN

```go
iter := rdb.HScan(ctx, "user:1:fields", 0, "*", 100).Iterator()
```

---

## 九、Pub/Sub 订阅

```go
pubsub := rdb.Subscribe(ctx, "channel:1", "channel:2")
defer pubsub.Close()

ch := pubsub.Channel()
for msg := range ch {
    fmt.Println(msg.Channel, msg.Payload)
}

// 发布
rdb.Publish(ctx, "channel:1", "hello")
```

**陷阱**：
- Pub/Sub 是**广播**，消息不持久化，订阅者掉线消息丢失
- 需要持久化用 Redis Stream（`XADD` / `XREAD`）或专门 MQ

---

## 十、常见生产陷阱

### 陷阱 1：忘了处理 `redis.Nil`

```go
val, err := rdb.Get(ctx, "key").Result()
if err != nil {
    if err == redis.Nil {  // ⚠️ key 不存在，不是错误
        return "", nil
    }
    return "", err  // 真的出错
}
```

**关键**：go-redis 用 `redis.Nil` 表示 key 不存在，别当错误处理。

### 陷阱 2：Context 超时太短

```go
// ❌ 100ms 超时不够，Redis 慢查询直接失败
ctx, _ := context.WithTimeout(context.Background(), 100*time.Millisecond)
rdb.Get(ctx, "key")

// ✅ 生产建议：
// - 简单命令：500ms
// - 复杂命令（BITCOUNT / SORT）：1s+
// - Pipeline：视命令数量，5s+
```

### 陷阱 3：BigKey / HotKey

**BigKey**：单 key 太大（如 List/Set 存百万元素）
- 症状：`DEL` 阻塞，`HGETALL` 打爆网络
- 解决：拆分（分片存储）、渐进删除（`UNLINK` 异步删）

**HotKey**：单 key QPS 极高
- 症状：Redis CPU 打满，客户端超时
- 解决：本地缓存兜底、多副本打散（写多份 `key:0` ~ `key:9`，读随机）

### 陷阱 4：连接泄漏

```go
// ❌ 忘了 rows.Close 之类（这里是 pubsub）
pubsub := rdb.Subscribe(ctx, "ch")
// 忘了 pubsub.Close()，连接不归还

// ✅
defer pubsub.Close()
```

### 陷阱 5：Cluster 模式下 SCAN

```go
// Cluster 下 SCAN 只扫一个节点
iter := rdb.Scan(ctx, 0, "user:*", 100).Iterator()
// ❌ 只扫了一个 master，其他 master 上的 key 没扫到

// ✅ 遍历所有 master 节点
rdb.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
    iter := client.Scan(ctx, 0, "user:*", 100).Iterator()
    // 处理...
    return nil
})
```

### 陷阱 6：TxPipelined 用于 Cluster

Cluster 模式下事务/pipeline 里所有 key 必须在**同一个 slot**，否则报错。用 hash tag。

### 陷阱 7：SetNX + Expire 不是原子

```go
// ❌ 两步操作，中间可能崩溃 → key 没有过期时间
rdb.SetNX(ctx, "lock", "1", 0)  // 没设 TTL
rdb.Expire(ctx, "lock", time.Minute)  // 如果这里失败...

// ✅ SetNX 就带 TTL
rdb.SetNX(ctx, "lock", "1", time.Minute)

// 或用 SET NX EX（同一个命令）
rdb.SetArgs(ctx, "lock", "1", redis.SetArgs{
    Mode: "NX",
    TTL:  time.Minute,
})
```

---

## 十一、性能优化清单

1. **用连接池**：生产必配，别用 `NewClient` 默认参数
2. **Pipeline 批量**：多条命令合并
3. **Lua 原子操作**：替代 MULTI/EXEC
4. **合理的 Key 设计**：短 key 名（省内存和网络）
5. **合理的过期时间**：别把 Redis 当持久存储
6. **避免大 key**：单 key < 10KB，集合类 < 5000 元素
7. **本地缓存兜底**：热 key 用 bigcache/freecache 挡在前面
8. **监控**：`INFO stats` 看命中率、`SLOWLOG` 看慢查询
9. **序列化**：value 用 protobuf/msgpack 比 JSON 小 2-3 倍

---

## 十二、其他 Go Redis 客户端

| 库 | 特点 |
|---|---|
| **go-redis** | 主流，功能全 |
| **redigo** | 老牌，API 偏底层，需要自己包装 |
| **rueidis** | **性能最强**，天然支持 Redis 7 客户端缓存、RESP3 |

### rueidis 亮点

```go
import "github.com/redis/rueidis"

client, _ := rueidis.NewClient(rueidis.ClientOption{
    InitAddress: []string{"localhost:6379"},
})

// 自动 pipeline（同一 goroutine 内的命令自动打包）
// 客户端缓存（Redis 7 服务端追踪 key 变更，客户端本地缓存）
// 支持 RESP3，性能比 go-redis 高 2-3 倍
```

**选型**：
- 常规业务 → go-redis（生态好，社区大）
- 追求极致性能 → rueidis（新项目 / 性能敏感）

---

## 十三、面试高频题

### Q1: go-redis 连接池怎么配置？

`PoolSize`（默认 `10 × NumCPU`），一般公式：`QPS × 平均 RT / 客户端实例数`。别设太大，Redis `maxclients` 默认 10000。配合 `MinIdleConns`、`PoolTimeout`。

### Q2: Pipeline 和 Transaction 的区别？

- **Pipeline**：批量发命令，减少 RTT，**没有原子性**（中间失败其他照跑）
- **TxPipeline / MULTI-EXEC**：命令进队列一次执行，**顺序原子**，但**不能回滚**
- **Lua 脚本**：真正的原子（单线程执行完），推荐用于需要原子的场景

### Q3: WATCH 是什么？

Redis 的乐观锁。`WATCH key` 后如果 key 被别人改，`EXEC` 会失败返回 nil，客户端需要重试。用于 CAS 场景，实际用得少（Lua 更方便）。

### Q4: Cluster 客户端怎么路由？

启动时拉 slot 映射表（`CLUSTER SLOTS`），每次命令 `CRC16(key) % 16384` 算 slot，找 node 发。收到 `MOVED` 时更新映射表。

### Q5: 多 key 命令在 Cluster 下怎么办？

只有同 slot 的 key 才能一起操作。用 **hash tag**：`user:{group1}:1`，`{}` 内的部分参与 hash 计算，保证同 slot。

### Q6: 为什么 KEYS 不能用？

`KEYS pattern` 是 O(n)，key 多时会阻塞 Redis 单线程主循环（可能几秒）。生产用 `SCAN` 迭代。

### Q7: BigKey 和 HotKey 怎么处理？

- BigKey：拆分（分片存），删除用 `UNLINK` 异步
- HotKey：本地缓存兜底、多副本打散（写 N 份，读随机）、代理层做

### Q8: SetNX + Expire 分开写有什么问题？

两步操作不原子，中间程序崩溃导致 key 没有 TTL → 永久 key。用 `SET key value NX EX seconds`（一条命令）或 Lua 脚本。

### Q9: 主从切换客户端如何感知？

Sentinel 模式：`NewFailoverClient` 客户端订阅 Sentinel 的 `+switch-master` 事件自动更新。
Cluster 模式：命令收到 `MOVED` 错误后重新拉 topology。

### Q10: go-redis 和 rueidis 哪个好？

- 生态和稳定性：go-redis（社区大，文档全）
- 性能：rueidis（自动 pipeline + 客户端缓存 + RESP3，快 2-3 倍）
- 新项目性能敏感 → rueidis；求稳求生态 → go-redis
