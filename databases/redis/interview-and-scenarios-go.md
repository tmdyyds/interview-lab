# Redis 面试题 + Go 场景实例合集

**标签**: #redis #go #interview #scenarios #高频

Redis 是后端面试**必考**方向。本文覆盖两部分：

1. **60 道高频面试题**（按数据结构 / 持久化 / 集群 / 缓存 / 分布式锁 / 性能 6 大类分组）
2. **10 个典型业务场景的 Go 实现**（可直接用于生产）

配套阅读：
- `use-cases.md`（PHP 版 12 个场景，更详尽）
- `distributed-lock.md` / `distributed-lock-advanced.md`（分布式锁专题）
- `cache-patterns.md`（缓存策略）
- `sentinel.md` / `cluster.md`（高可用）

---

## Part 1：60 道高频面试题

### 一、基础与数据结构（12 题）

#### Q1: 为什么 Redis 单线程还这么快？

- **纯内存操作**：所有数据在 RAM 里，读写 100 纳秒级
- **非阻塞 IO + 多路复用**：epoll/kqueue 单线程能同时处理上万连接
- **单线程避免锁竞争**：没有线程切换、没有锁的开销
- **高效数据结构**：SDS、跳表、ziplist / listpack 都是精心设计
- **Redis 6+ 引入多线程 IO**：只用来读写网络包，命令执行还是单线程

#### Q2: Redis 有哪些数据类型？各自底层实现？

| 类型 | 底层结构 |
|-----|--------|
| String | SDS（Simple Dynamic String） |
| List | quicklist（Redis 7+ 用 listpack） |
| Hash | listpack + hashtable（大小超阈值时切换） |
| Set | intset + hashtable + listpack |
| ZSet | skiplist + hashtable（跳表 + 字典） |
| Stream | radix tree + listpack |
| Bitmap | 本质是 String，按 bit 操作 |
| HyperLogLog | 本质是 String，用 12KB 存概率计数 |
| GEO | 本质是 ZSet，用 GeoHash 编码经纬度 |

#### Q3: ZSet 为什么用跳表不用红黑树？

- **实现简单**：跳表代码约 200 行，红黑树上千行且难调
- **区间查询快**：ZRANGEBYSCORE 天然按层顺序遍历，O(log N + M)
- **易于并发**：跳表的插入、删除只影响局部，无 rebalance
- **内存友好**：Redis 用的是"含平均前后节点数"的跳表，指针数量可控

#### Q4: SDS 相比 C 字符串有什么优势？

```
struct sdshdr {
    int len;        // 字符串实际长度
    int free;       // 剩余可用空间
    char buf[];     // 字节数组（末尾有 \0，但不算 len）
}
```

- **O(1) 获取长度**（C 的 strlen 是 O(N)）
- **二进制安全**（存图片、序列化数据都行，不怕 \0）
- **杜绝缓冲区溢出**（写入前检查空间）
- **减少内存重分配**（预分配 + 惰性释放）

#### Q5: List 的 ziplist / listpack / quicklist 什么关系？

- **ziplist**（Redis 6-）：连续内存的压缩列表，节省空间但**连锁更新问题**
- **listpack**（Redis 7+）：ziplist 的改良版，去掉连锁更新，取代 ziplist
- **quicklist**：`双向链表 + 每个节点是 listpack`，兼顾链表的 O(1) 头尾操作和 listpack 的空间效率

小 List 用一个 listpack 存；大 List 用 quicklist（多个 listpack 链起来）。

#### Q6: Hash 什么时候从 listpack 转 hashtable？

两个条件（默认值）：
- 字段数 > `hash-max-listpack-entries`（默认 128）
- 单个字段或值长度 > `hash-max-listpack-value`（默认 64 字节）

**任一超过就转 hashtable**，转了不会转回来（避免抖动）。

#### Q7: Set 的三种编码？

- **intset**：所有元素都是整数且数量少（< 512）
- **listpack**（Redis 7.2+）：非整数但数量少
- **hashtable**：大集合

#### Q8: 跳表的空间复杂度和时间复杂度？

- **时间复杂度**：查找、插入、删除都是 **O(log N)**
- **空间复杂度**：O(N)，但每个节点平均有 1/(1-p) 层指针，p 是每层晋升概率（Redis 用 0.25，平均 1.33 层）

#### Q9: BitMap 有什么应用？占多少内存？

**场景**：用户签到、活跃状态、布隆过滤器、A/B 测试。

**内存**：1 亿用户签到 = 1 亿 bit = **12.5 MB**（极省）。

**操作**：`SETBIT`、`GETBIT`、`BITCOUNT`、`BITOP AND/OR/XOR`。

#### Q10: HyperLogLog 是什么？误差多少？

- **概率去重计数算法**（不存实际值，只存"基数估计")
- **12 KB 内存能估算 2^64 个不同元素的数量**
- **误差率约 0.81%**（标准误差）
- **适合**：UV 统计、大规模去重

**关键**：不能取出具体元素，只能得到数量。

#### Q11: Stream 相比 List 做队列有什么优势？

| | List | Stream |
|---|------|--------|
| 消息持久化 | ✅ | ✅ |
| 多消费者组 | ❌（一条消息一个消费者） | ✅（一条消息多组各消费一次） |
| 消息确认（ACK） | ❌ | ✅ |
| 消息回溯 | ❌（LPOP 后就没了） | ✅（按 ID 定位） |
| 阻塞读 | ✅ | ✅ |

Stream 是 Redis 5+ 的正统消息队列方案。

#### Q12: GEO 底层原理？

- **底层是 ZSet**
- **经纬度用 GeoHash 算法编码成 52 位整数** → 作为 ZSet 的 score
- **member 就是位置的名字**（如 "shop:1001"）

查附近 = ZSet 范围查询 + GeoHash 距离计算。

---

### 二、持久化（8 题）

#### Q13: RDB 和 AOF 的区别？

| | RDB | AOF |
|---|-----|-----|
| 存的什么 | **数据快照**（二进制） | **命令日志**（文本） |
| 恢复速度 | **快**（直接加载） | 慢（需重放命令） |
| 数据安全 | 差（最多丢 5 分钟） | 好（可秒级同步） |
| 文件大小 | 小 | 大（可 AOF 重写压缩） |
| 触发时机 | 手动/定时/主从同步 | 每次写命令 |

#### Q14: AOF 的三种同步策略？

- **always**：每次写命令都 fsync 到磁盘。最安全但最慢（QPS 会打折）
- **everysec**：每秒 fsync 一次（默认）。最多丢 1 秒数据
- **no**：交给 OS 决定何时刷盘（一般 30 秒）。最快但最不安全

生产建议：**everysec**（性能和安全的平衡）。

#### Q15: 什么是 AOF 重写？

AOF 文件会变很大（每次写都追加）。重写 = **根据当前内存快照，生成最精简的命令集合**：

```
原 AOF：
  SET name jake
  DEL name
  SET name mike
  INCR counter
  INCR counter
  ...

重写后：
  SET name mike
  SET counter 2
```

**触发**：`BGREWRITEAOF` 或 `auto-aof-rewrite-percentage`（默认文件翻倍触发）。

#### Q16: AOF 重写用的 fork 有什么问题？

- **fork 会 COW（Copy-On-Write）**：写操作多时会大量复制内存页
- **fork 期间 STW**：内存越大 fork 越慢（100GB 实例可能卡几百毫秒）
- **fork 后子进程写盘慢会拖累主进程**（内存爆涨）

**优化**：控制单实例内存 < 10GB、关闭大页（Transparent HugePages）。

#### Q17: 混合持久化是什么？（Redis 4.0+）

**RDB + AOF 二合一**：AOF 重写时，前半段用 RDB 格式（快速加载），后半段用 AOF 增量。

```
[RDB 二进制快照] + [新命令的 AOF 记录]
```

**优点**：恢复速度快（RDB） + 数据安全（AOF）。生产强烈推荐开启（`aof-use-rdb-preamble yes`）。

#### Q18: Redis 挂了怎么恢复数据？

- **只开 RDB**：加载最近的 `dump.rdb`
- **只开 AOF**：重放 `appendonly.aof`
- **两个都开**：**优先加载 AOF**（数据更全）
- **混合模式**：加载 AOF 文件（前半是 RDB 快照）

#### Q19: 大 key 对持久化的影响？

- **RDB**：大 key 序列化时阻塞（生成 RDB 期间不能有其他命令）
- **AOF**：大 key 重写时占用大量内存和 CPU
- **主从同步**：大 key 传输会阻塞主库

**避免大 key**：单 key < 10KB，集合类元素 < 5000。

#### Q20: 如何做数据热备份？

方案：
1. **主从 + 定时 BGSAVE**：从库定时生成 RDB 上传对象存储
2. **AOF 备份**：定时 tar 打包 `appendonly.aof`
3. **云厂商备份**（阿里云 Redis 内置每日自动备份）
4. **两地三中心**：主中心 + 同城灾备 + 异地灾备

---

### 三、集群与高可用（10 题）

#### Q21: 主从复制的流程？

```
1. 从库执行 REPLICAOF host port（或启动时配置）
2. 从库向主库发 PSYNC replid offset
3. 主库判断：
   - 第一次同步 → 全量同步（BGSAVE 生成 RDB → 传输 → 从库加载 → 增量追赶）
   - 断线重连 → 增量同步（从 backlog 找到断点，传缺失的命令）
4. 之后主库每次写命令 → 异步复制到从库
```

#### Q22: PSYNC 相比 SYNC 有什么改进？

- **SYNC**：断线重连必须全量同步（低效）
- **PSYNC**：主库维护**复制积压缓冲区**（backlog，默认 1MB），断线短暂时可以**增量同步**

关键参数：`replid`（主库标识）+ `offset`（复制偏移量）。

#### Q23: 主从复制是同步的吗？会丢数据吗？

**默认异步**。主库写完就返回客户端，之后再复制给从库。**主库挂了没同步的部分会丢**。

**WAIT 命令**：可以强制等待 N 个从库同步完成（准同步）：
```
WAIT 2 100  # 等 2 个从库同步，最多等 100ms
```

#### Q24: 哨兵（Sentinel）做什么？

**三个职责**：
1. **监控**：定期 ping 主从库，判断是否宕机
2. **故障转移**：主库挂了，选一个从库升为主
3. **通知**：变更后通知客户端（发布订阅）

**Sentinel 自身也是集群**（3 或 5 个节点），Raft 选举协调。

#### Q25: 什么是脑裂？怎么避免？

**脑裂**：主库和 sentinel 之间网络分区，sentinel 以为主库挂了 → 选了新主 → 老主又活过来 → 两个主库同时接受写。

**避免**：
```
min-slaves-to-write 1        # 至少 1 个从库存活才允许写
min-slaves-max-lag 10        # 从库延迟不超过 10s
```

主库发现从库不满足条件时**拒绝写入**，避免脑裂时的数据分裂。

#### Q26: Redis Cluster 有多少个 slot？为什么？

**16384 个**（2^14）。原因：
- Redis 每个节点用 bitmap 记录自己管哪些 slot，16384 bit = 2KB
- 65536 slot 会占 8KB，且集群通信开销大
- **Redis 集群最多 1000 节点**，16384 slot 足够均匀分配

#### Q27: Cluster 如何路由 key 到节点？

```
1. 客户端算 hash：CRC16(key) % 16384 → slot 编号（0-16383）
2. 客户端本地维护 slot → node 的映射表（启动时拉取）
3. 直接发到对应 node
4. 如果 slot 迁移中，收到 MOVED 或 ASK 重定向 → 更新映射
```

#### Q28: hash tag 是什么？

Cluster 里跨 slot 的 key 不能一起操作（如 MSET、事务）。**hash tag 强制多个 key 落到同一 slot**：

```
user:{group1}:1
user:{group1}:2
user:{group1}:3
```

`{}` 内的内容参与 hash 计算，三个 key 会到同一 slot。

#### Q29: Cluster 主从切换过程？

1. **主观下线**：某节点 ping 主库超时（`cluster-node-timeout`）
2. **客观下线**：超半数主节点都认为下线 → 触发故障转移
3. **从库选举**：从库中根据"复制偏移量"和"节点 ID"选一个
4. **提升为主**：新主接管原主的 slot，广播新配置
5. **原主恢复后变从库**

#### Q30: Redis 集群方案对比？

| 方案 | 特点 | 适合 |
|-----|------|-----|
| **主从 + 哨兵** | 简单，支持故障转移，单点写 | 数据量小、QPS 中等 |
| **Cluster** | 分片 + 自动故障转移 | 大数据量、高 QPS |
| **Codis** | 豌豆荚出品的 proxy 方案 | Redis 3.0 之前流行 |
| **Twemproxy** | Twitter 出品的 proxy | 老方案，现在少用 |
| **云托管**（阿里云/AWS） | 全托管，自动扩容 | 生产首选 |

---

### 四、缓存问题（10 题）

#### Q31: 什么是缓存穿透？

**查询不存在的数据**，缓存和 DB 都没有 → 每次都打 DB → 恶意攻击可打爆 DB。

**解决**：
1. **空值缓存**：DB 返回空也缓存（短 TTL 如 1 分钟）
2. **布隆过滤器**：请求前先查 BF，不存在直接返回
3. **参数校验**：非法参数直接拒绝（如 user_id 为负数）

#### Q32: 什么是缓存击穿？

**某个热 key 突然失效**，瞬间大量请求同时查 DB → DB 压力骤增。

**解决**：
1. **互斥锁**（`SETNX`）：只允许一个请求查 DB，其他等结果
2. **singleflight**：应用层去重
3. **永不过期 + 后台刷新**：热 key 不设 TTL，后台定时更新
4. **多级缓存**：本地缓存兜底

#### Q33: 什么是缓存雪崩？

**大量 key 同时失效**，或 **Redis 整个挂掉** → 请求全打到 DB → DB 崩溃。

**解决**：
1. **TTL 加随机**：`ttl := 5*time.Minute + rand.Intn(300)`，避免同时过期
2. **Redis 高可用**：Cluster / Sentinel
3. **降级 + 限流**：Redis 挡不住时限流保护 DB
4. **本地缓存兜底**

#### Q34: 三大缓存问题的区别？

| | 穿透 | 击穿 | 雪崩 |
|---|------|------|------|
| 表现 | 查不存在的 key | 单个热 key 失效 | 大量 key 同时失效 |
| 后果 | DB 被无效请求打爆 | DB 被同一 key 打爆 | DB 被全部请求打爆 |
| 解决 | 布隆过滤 + 空值缓存 | 互斥锁 + 后台刷新 | TTL 随机 + 高可用 |

#### Q35: 缓存一致性 Cache Aside 方案的流程？

```
读：先缓存，未命中 → DB → 回写缓存
写：先更新 DB → 删除缓存
```

**为什么先 DB 后删缓存**：反过来（先删缓存后 DB）在并发时有 bug：
```
T1 删缓存
T2 读，未命中，读 DB 拿到旧值
T3 T2 写回缓存（旧值）
T4 T1 更新 DB（新值）
→ DB 新，缓存旧，不一致
```

#### Q36: 什么是延迟双删？

Cache Aside 极端并发下仍可能不一致。**延迟双删**加固：

```go
1. redis.Del(key)           // 先删
2. db.Update(...)           // 更新 DB
3. time.Sleep(500ms)        // 等所有正在读的请求结束
4. redis.Del(key)            // 再删一次，清掉并发时写回的脏数据
```

**代价**：多一次 Del、多一点延迟。

#### Q37: 强一致场景怎么办？

- **订阅 binlog（Canal / Debezium）**：DB 变更 → MQ → 消费者删缓存。异步但可靠
- **分布式锁**：更新时锁 key，串行化读写。性能差
- **不用缓存**：金融强一致场景直接读 DB + 优化 DB 性能

#### Q38: Redis 的过期策略？

**两种结合**：
1. **惰性删除**：访问 key 时才检查是否过期 → 省 CPU，但过期 key 长期不访问会占内存
2. **定期删除**：Redis 每 100ms 随机检查一批 key → 补偿惰性删除的短板

**注意**：过期 key 不会被主动搜索，靠这两种被动方式清理。

#### Q39: Redis 的内存淘汰策略？

内存满了触发（`maxmemory` 配置）。8 种策略：

| 策略 | 含义 |
|-----|------|
| `noeviction` | **默认**，不淘汰，写命令直接报错 |
| `allkeys-lru` | 全体 key 用 LRU 淘汰 |
| `allkeys-lfu` | 全体 key 用 LFU 淘汰（4.0+） |
| `allkeys-random` | 全体 key 随机淘汰 |
| `volatile-lru` | 只淘汰设了 TTL 的 key（LRU） |
| `volatile-lfu` | 同上（LFU） |
| `volatile-random` | 同上（随机） |
| `volatile-ttl` | 淘汰 TTL 最近到期的 |

**生产推荐**：`allkeys-lru`（缓存场景）或 `volatile-lru`（混用场景）。

#### Q40: LRU 和 LFU 什么区别？

- **LRU**（Least Recently Used）：**最久没访问**的先淘汰。适合"最近访问的以后还会访问"
- **LFU**（Least Frequently Used）：**访问次数最少**的先淘汰。适合"热点数据长期热点"

Redis 的 LRU/LFU 都是**近似算法**（随机采样几个 key 比较），不是严格的。

---

### 五、分布式锁（8 题）

#### Q41: Redis 分布式锁基本实现？

```go
// 加锁：SET NX EX
ok := redis.SetNX(ctx, key, uuid, 30*time.Second)

// 解锁：Lua 保证原子（先比对再删）
const unlockScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0`
```

**关键**：value 存 UUID 唯一标识，防止误删别人的锁。

#### Q42: 为什么解锁要用 Lua？

**直接写代码有并发问题**：

```
1. GET key           → 拿到 "uuid-A"
2. 判断 == "uuid-A"  → 是
3. DEL key           
```

`2` 和 `3` 之间锁 TTL 到期 → B 拿到锁 → 我 DEL 误删了 B 的锁。

**Lua 在 Redis 单线程内原子执行**，中间不会插入其他命令。

#### Q43: 什么是看门狗？为什么需要？

**问题**：业务执行时间可能超过锁 TTL → 锁自动释放 → 其他人拿到锁 → 并发。

**看门狗（Watchdog）**：加锁成功后启动一个 goroutine，**每隔 TTL/3 时间续期**：

```go
go func() {
    ticker := time.NewTicker(ttl / 3)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            redis.Expire(ctx, key, ttl)
        }
    }
}()
```

业务处理完 `cancel()` 停止续期 + 解锁。

#### Q44: Redlock 是什么？有什么问题？

Redis 作者 antirez 提出：**在 N 个独立 Redis 实例上加锁，超半数（N/2+1）成功才算加锁成功**。

**争议**（Martin Kleppmann 批评）：
1. **依赖时钟同步**：如果时钟不准，某些节点会误认为锁过期
2. **GC 停顿问题**：客户端 GC 长时间 stop-the-world，锁可能已过期
3. **Fencing Token 才是正解**：给每个锁一个递增的 token，DB 层校验

**结论**：一般业务场景 Redis 单实例 + Sentinel 足够；金融强一致用 etcd/ZooKeeper。

#### Q45: 分布式锁不用 Redis 用什么？

- **etcd**：Raft 强一致，有 lease 机制，天然适合分布式锁
- **ZooKeeper**：Zab 协议，临时顺序节点做公平锁
- **Consul**：类似 etcd

Redis 是**AP 系统**（可用性优先），有丢锁风险；etcd/ZK 是 **CP 系统**（一致性优先）。金融、支付、扣款等强一致场景用 CP。

#### Q46: 可重入锁怎么实现？

用 Hash 记录持有者和重入次数：

```lua
-- 加锁
if redis.call('HGET', key, 'owner') == owner then
    return redis.call('HINCRBY', key, 'count', 1)  -- 重入
end
if redis.call('EXISTS', key) == 0 then
    redis.call('HSET', key, 'owner', owner, 'count', 1)
    redis.call('EXPIRE', key, ttl)
    return 1
end
return 0  -- 别人持有

-- 解锁
if redis.call('HGET', key, 'owner') == owner then
    local n = redis.call('HINCRBY', key, 'count', -1)
    if n <= 0 then
        return redis.call('DEL', key)
    end
    return 1
end
return 0
```

#### Q47: 公平锁怎么实现？

用 List 排队：加锁失败时把自己加到 List 尾部，锁释放时通知队首。**复杂且性能一般，生产少用**。

用 Redisson 的 `RLock` 有现成实现。

#### Q48: 读写锁怎么实现？

Redis 原生不支持，可以模拟：

- 读锁：Hash 存 `readers` 计数，每个读者 +1
- 写锁：设置 `writer` 字段，独占

复杂度高，直接用 Redisson 或换 etcd。

---

### 六、性能与生产（12 题）

#### Q49: 什么是大 key？有什么危害？

**大 key**：单 key 值太大（如 List 上百万元素、String 上 MB）。

**危害**：
- **删除阻塞**：`DEL bigkey` 是同步操作，可能卡几秒
- **迁移阻塞**：Cluster slot 迁移时会阻塞
- **网络堵塞**：`HGETALL` 一次取几 MB，撑爆网络
- **持久化阻塞**：RDB / AOF 重写时序列化大 key 慢

**处理**：拆分、`UNLINK` 异步删（Redis 4+）、`SCAN` 分批处理。

#### Q50: 什么是热 key？如何处理？

**热 key**：单 key QPS 极高（如爆款商品、明星微博）。

**症状**：Redis CPU 打满，客户端超时。

**处理**：
1. **本地缓存兜底**（bigcache / freecache，短 TTL）
2. **多副本打散**：写 N 份 `key:0` ~ `key:9`，读随机选一个
3. **热 key 迁移到独立实例**（避免影响其他 key）
4. **代理层做**：Twemproxy / Codis 支持热 key 探测

#### Q51: 如何找出大 key 和热 key？

**大 key**：
```bash
redis-cli --bigkeys                    # 内置扫描
redis-cli --memkeys                    # Redis 6.2+，按内存扫
```

**热 key**：
```bash
redis-cli --hotkeys                    # Redis 4+，需 maxmemory-policy 为 LFU
MONITOR                                # 实时看命令（谨慎用，会打满 CPU）
```

生产建议：**客户端埋点上报**（在业务代码里统计）。

#### Q52: Pipeline 是什么？和 Transaction 什么区别？

- **Pipeline**：**批量发命令 + 一次返回**，减少 RTT。**没有原子性**（中间失败其他继续跑）
- **Transaction**（MULTI/EXEC）：**命令进队列一次执行**，**顺序原子**，但**不能回滚**
- **Lua**：**真正的原子**（单线程执行完），比 Transaction 更强

QPS 高时优先用 Pipeline。

#### Q53: KEYS 为什么不能用？

`KEYS pattern` 是 **O(N)**，会**阻塞 Redis 单线程主循环**（key 多时几秒到几十秒）。

**生产用 SCAN**：迭代式扫描，每次返回一小批，不阻塞。

```go
iter := rdb.Scan(ctx, 0, "user:*", 100).Iterator()
for iter.Next(ctx) {
    fmt.Println(iter.Val())
}
```

**注意**：SCAN 可能漏、可能重复，客户端自己去重。

#### Q54: Redis 6 的多线程是什么？

**Redis 6+ 引入多线程 IO**：
- **接收/发送数据**：多线程（默认关闭，`io-threads 4` 开启）
- **命令执行**：**仍然单线程**（避免并发问题）

**为什么这样设计**：Redis 的瓶颈通常是**网络 IO**，不是命令执行。多线程 IO 后 QPS 能提升 30-50%。

#### Q55: Redis 事务能回滚吗？

**不能**。Redis 事务里某条命令**执行失败其他照常执行**（不像 MySQL 会 rollback）。

原因：Redis 设计上认为命令级错误都是"程序 bug"，不该在生产出现。

#### Q56: WATCH 有什么用？

**乐观锁**。`WATCH key` 后如果 key 被别人修改，`EXEC` 时返回 nil（事务失败），客户端需要重试。

```
WATCH stock
GET stock         -- 假设 = 10
MULTI
DECR stock
EXEC              -- 如果 stock 期间被人改了，返回 nil
```

**用得少**，因为 Lua 脚本更简单直接。

#### Q57: Redis 内存碎片率高怎么办？

**碎片率** = `used_memory_rss / used_memory`。

- **接近 1**：健康
- **> 1.5**：碎片严重

**处理**：
```
CONFIG SET activedefrag yes    # Redis 4+ 主动碎片整理
CONFIG SET active-defrag-threshold-lower 10
```

或重启（RDB 恢复后碎片会重置，但期间不可用）。

#### Q58: Redis 内存占用高，怎么排查？

```
INFO memory                    # 看总内存、峰值、碎片率
MEMORY USAGE key               # 看单个 key 占多少
MEMORY STATS                   # 详细统计
redis-cli --bigkeys            # 找大 key
DEBUG OBJECT key               # 查看 key 编码
```

优化：
- 用 hash 存对象比多个 string 省内存
- 短字符串 < 44 字节走 embstr 编码
- 集合类元素少时用 listpack（小对象编码）

#### Q59: 生产 Redis 部署有哪些建议？

- **内存**：单实例 < 10GB（避免 fork 慢）
- **maxmemory**：设 80% 物理内存，留缓冲
- **淘汰策略**：`allkeys-lru`（缓存场景）
- **持久化**：AOF everysec + 混合持久化
- **系统调优**：
  - `vm.overcommit_memory = 1`（允许 fork）
  - 关闭 THP（Transparent HugePages）
  - `net.core.somaxconn` 调大
- **监控**：Prometheus + redis_exporter + 报警

#### Q60: Redis 常见运维命令？

```bash
# 查状态
INFO
CLIENT LIST                    # 所有客户端连接
SLOWLOG GET 10                 # 慢查询

# 配置
CONFIG GET maxmemory
CONFIG SET maxmemory 8gb

# 持久化
BGSAVE                         # 后台生成 RDB
BGREWRITEAOF                   # 后台重写 AOF
LASTSAVE                       # 上次 RDB 时间

# 阻塞检查
CLIENT PAUSE 5000              # 暂停接受命令 5s（谨慎）
DEBUG SLEEP 5                  # 阻塞主线程 5s（测试用）
```

---

## Part 2：10 个业务场景的 Go 实现

以下场景都可以直接用于生产，配合 `redis/go-redis/v9`。

### 场景 1：分布式锁（含看门狗）

```go
package lock

import (
    "context"
    "errors"
    "time"

    "github.com/google/uuid"
    "github.com/redis/go-redis/v9"
)

type RedisLock struct {
    client *redis.Client
    key    string
    value  string
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

// 加锁 + 启动看门狗
func (l *RedisLock) Lock(ctx context.Context) error {
    ok, err := l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
    if err != nil {
        return err
    }
    if !ok {
        return errors.New("lock held by others")
    }
    watchCtx, cancel := context.WithCancel(context.Background())
    l.cancel = cancel
    go l.renewLoop(watchCtx)
    return nil
}

// 解锁（Lua 原子）
const unlockScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0`

func (l *RedisLock) Unlock(ctx context.Context) error {
    if l.cancel != nil {
        l.cancel()
    }
    _, err := l.client.Eval(ctx, unlockScript, []string{l.key}, l.value).Result()
    return err
}

// 看门狗：每 TTL/3 续期一次
func (l *RedisLock) renewLoop(ctx context.Context) {
    ticker := time.NewTicker(l.ttl / 3)
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

**用法**：

```go
lock := NewLock(rdb, "order:pay:123", 30*time.Second)
if err := lock.Lock(ctx); err != nil {
    return err
}
defer lock.Unlock(ctx)

// 业务逻辑
processOrder(orderID)
```

---

### 场景 2：限流器（滑动窗口 + Lua）

```go
package ratelimit

import (
    "context"
    "time"

    "github.com/redis/go-redis/v9"
)

const slidingWindowScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])

redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)
if count >= limit then
    return 0
end
redis.call('ZADD', key, now, now .. ':' .. math.random())
redis.call('PEXPIRE', key, window)
return 1`

type SlidingWindowLimiter struct {
    client *redis.Client
    script *redis.Script
}

func NewLimiter(client *redis.Client) *SlidingWindowLimiter {
    return &SlidingWindowLimiter{
        client: client,
        script: redis.NewScript(slidingWindowScript),
    }
}

// 每 windowMs 毫秒内最多 limit 次
func (r *SlidingWindowLimiter) Allow(ctx context.Context, key string, windowMs, limit int) (bool, error) {
    now := time.Now().UnixMilli()
    result, err := r.script.Run(ctx, r.client, []string{key}, now, windowMs, limit).Int()
    if err != nil {
        return true, nil  // 限流器故障放行
    }
    return result == 1, nil
}
```

**用法**：

```go
limiter := NewLimiter(rdb)
allowed, _ := limiter.Allow(ctx, "api:user:1001", 1000, 10)  // 1 秒 10 次
if !allowed {
    return errors.New("rate limited")
}
```

---

### 场景 3：延迟队列（订单超时关闭）

```go
package delayqueue

import (
    "context"
    "encoding/json"
    "fmt"
    "strconv"
    "time"

    "github.com/redis/go-redis/v9"
)

type DelayQueue struct {
    client *redis.Client
    name   string  // 队列名
}

func New(client *redis.Client, name string) *DelayQueue {
    return &DelayQueue{client: client, name: name}
}

// 添加延迟任务
func (q *DelayQueue) Add(ctx context.Context, taskID string, payload any, delay time.Duration) error {
    executeAt := time.Now().Add(delay).UnixMilli()

    data, err := json.Marshal(payload)
    if err != nil {
        return err
    }

    pipe := q.client.TxPipeline()
    pipe.ZAdd(ctx, q.zsetKey(), redis.Z{Score: float64(executeAt), Member: taskID})
    pipe.HSet(ctx, q.hashKey(), taskID, data)
    _, err = pipe.Exec(ctx)
    return err
}

// 消费到期任务（Lua 原子取 + 删）
const consumeScript = `
local zkey = KEYS[1]
local hkey = KEYS[2]
local now = tonumber(ARGV[1])
local batch = tonumber(ARGV[2])

local tasks = redis.call('ZRANGEBYSCORE', zkey, '-inf', now, 'LIMIT', 0, batch)
if #tasks == 0 then
    return {}
end

local result = {}
for i, id in ipairs(tasks) do
    local payload = redis.call('HGET', hkey, id)
    table.insert(result, id)
    table.insert(result, payload)
    redis.call('ZREM', zkey, id)
    redis.call('HDEL', hkey, id)
end
return result`

type Task struct {
    ID      string
    Payload []byte
}

func (q *DelayQueue) Consume(ctx context.Context, batch int) ([]Task, error) {
    result, err := q.client.Eval(ctx, consumeScript,
        []string{q.zsetKey(), q.hashKey()},
        time.Now().UnixMilli(), batch).Result()
    if err != nil {
        return nil, err
    }

    arr := result.([]any)
    tasks := make([]Task, 0, len(arr)/2)
    for i := 0; i < len(arr); i += 2 {
        tasks = append(tasks, Task{
            ID:      arr[i].(string),
            Payload: []byte(arr[i+1].(string)),
        })
    }
    return tasks, nil
}

// 取消任务
func (q *DelayQueue) Cancel(ctx context.Context, taskID string) error {
    pipe := q.client.TxPipeline()
    pipe.ZRem(ctx, q.zsetKey(), taskID)
    pipe.HDel(ctx, q.hashKey(), taskID)
    _, err := pipe.Exec(ctx)
    return err
}

func (q *DelayQueue) zsetKey() string { return "dq:" + q.name + ":z" }
func (q *DelayQueue) hashKey() string { return "dq:" + q.name + ":h" }

// 消费者循环
func (q *DelayQueue) Run(ctx context.Context, handler func(Task) error) {
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            tasks, err := q.Consume(ctx, 100)
            if err != nil {
                continue
            }
            for _, t := range tasks {
                if err := handler(t); err != nil {
                    // 失败重新入队
                    q.Add(ctx, t.ID, string(t.Payload), 30*time.Second)
                }
            }
        }
    }
}

// 用法
func Example() {
    q := New(rdb, "order_cancel")

    // 下单时添加 30 分钟超时任务
    q.Add(ctx, "order:123", map[string]any{"order_id": 123}, 30*time.Minute)

    // 用户支付了 → 取消超时任务
    q.Cancel(ctx, "order:123")

    // 后台消费
    go q.Run(ctx, func(t Task) error {
        fmt.Println("cancel order:", t.ID)
        return nil
    })

    _ = strconv.Itoa  // silence
}
```

---

### 场景 4：排行榜

```go
package leaderboard

import (
    "context"
    "github.com/redis/go-redis/v9"
)

type Leaderboard struct {
    client *redis.Client
    key    string
}

func New(client *redis.Client, name string) *Leaderboard {
    return &Leaderboard{client: client, key: "lb:" + name}
}

// 增加分数（原子）
func (l *Leaderboard) AddScore(ctx context.Context, memberID string, delta float64) (float64, error) {
    return l.client.ZIncrBy(ctx, l.key, delta, memberID).Result()
}

// 设置分数（覆盖）
func (l *Leaderboard) SetScore(ctx context.Context, memberID string, score float64) error {
    return l.client.ZAdd(ctx, l.key, redis.Z{Score: score, Member: memberID}).Err()
}

type Entry struct {
    MemberID string
    Score    float64
    Rank     int
}

// Top N
func (l *Leaderboard) TopN(ctx context.Context, n int64) ([]Entry, error) {
    result, err := l.client.ZRevRangeWithScores(ctx, l.key, 0, n-1).Result()
    if err != nil {
        return nil, err
    }
    entries := make([]Entry, 0, len(result))
    for i, z := range result {
        entries = append(entries, Entry{
            MemberID: z.Member.(string),
            Score:    z.Score,
            Rank:     i + 1,
        })
    }
    return entries, nil
}

// 查询某人排名和分数
func (l *Leaderboard) GetRank(ctx context.Context, memberID string) (rank int64, score float64, err error) {
    pipe := l.client.Pipeline()
    rankCmd := pipe.ZRevRank(ctx, l.key, memberID)
    scoreCmd := pipe.ZScore(ctx, l.key, memberID)
    _, err = pipe.Exec(ctx)
    if err != nil {
        return 0, 0, err
    }
    return rankCmd.Val() + 1, scoreCmd.Val(), nil  // 排名从 1 开始
}

// 用法
func Example(rdb *redis.Client, ctx context.Context) {
    lb := New(rdb, "game_score")

    lb.AddScore(ctx, "player:1001", 500)
    lb.AddScore(ctx, "player:1002", 800)
    lb.AddScore(ctx, "player:1001", 200)  // 累加 = 700

    top10, _ := lb.TopN(ctx, 10)
    for _, e := range top10 {
        _ = e  // 处理
    }
}
```

---

### 场景 5：布隆过滤器（防穿透）

```go
package bloomfilter

import (
    "context"
    "github.com/redis/go-redis/v9"
    "hash/fnv"
)

// 简易 Redis 布隆过滤器（生产建议用 RedisBloom 模块）
type BloomFilter struct {
    client   *redis.Client
    key      string
    bits     uint    // 位数组大小
    hashNum  int     // hash 函数数量
}

func New(client *redis.Client, key string, bits uint, hashNum int) *BloomFilter {
    return &BloomFilter{client: client, key: key, bits: bits, hashNum: hashNum}
}

// N 个 hash 函数（用一个 hash 加不同 seed 实现）
func (b *BloomFilter) hashes(data string) []uint {
    result := make([]uint, b.hashNum)
    for i := 0; i < b.hashNum; i++ {
        h := fnv.New64()
        h.Write([]byte(data))
        h.Write([]byte{byte(i)})
        result[i] = uint(h.Sum64()) % b.bits
    }
    return result
}

func (b *BloomFilter) Add(ctx context.Context, data string) error {
    pipe := b.client.Pipeline()
    for _, offset := range b.hashes(data) {
        pipe.SetBit(ctx, b.key, int64(offset), 1)
    }
    _, err := pipe.Exec(ctx)
    return err
}

func (b *BloomFilter) MightContain(ctx context.Context, data string) (bool, error) {
    pipe := b.client.Pipeline()
    cmds := make([]*redis.IntCmd, b.hashNum)
    for i, offset := range b.hashes(data) {
        cmds[i] = pipe.GetBit(ctx, b.key, int64(offset))
    }
    if _, err := pipe.Exec(ctx); err != nil {
        return false, err
    }
    for _, c := range cmds {
        if c.Val() == 0 {
            return false, nil  // 一定不存在
        }
    }
    return true, nil  // 可能存在（有误判率）
}

// 用法：防止缓存穿透
func GetUser(ctx context.Context, bf *BloomFilter, id string) (*User, error) {
    exists, _ := bf.MightContain(ctx, id)
    if !exists {
        return nil, ErrNotFound  // 一定不存在，直接返回，不查 DB
    }
    // 可能存在 → 走缓存 → DB
    return queryUser(ctx, id)
}
```

**生产提示**：直接用 [RedisBloom 模块](https://github.com/RedisBloom/RedisBloom)，性能更好。

---

### 场景 6：Session（Hash 存储 + 过期续期）

```go
package session

import (
    "context"
    "encoding/json"
    "time"

    "github.com/google/uuid"
    "github.com/redis/go-redis/v9"
)

type SessionManager struct {
    client *redis.Client
    ttl    time.Duration
}

func New(client *redis.Client, ttl time.Duration) *SessionManager {
    return &SessionManager{client: client, ttl: ttl}
}

// 创建 session
func (s *SessionManager) Create(ctx context.Context, data map[string]any) (string, error) {
    sessionID := uuid.NewString()
    key := "sess:" + sessionID

    fields := make(map[string]any)
    for k, v := range data {
        b, _ := json.Marshal(v)
        fields[k] = string(b)
    }

    pipe := s.client.TxPipeline()
    pipe.HSet(ctx, key, fields)
    pipe.Expire(ctx, key, s.ttl)
    _, err := pipe.Exec(ctx)
    return sessionID, err
}

// 读取字段
func (s *SessionManager) Get(ctx context.Context, sessionID, field string, dst any) error {
    key := "sess:" + sessionID
    val, err := s.client.HGet(ctx, key, field).Result()
    if err != nil {
        return err
    }
    // 访问续期
    s.client.Expire(ctx, key, s.ttl)
    return json.Unmarshal([]byte(val), dst)
}

// 销毁
func (s *SessionManager) Destroy(ctx context.Context, sessionID string) error {
    return s.client.Del(ctx, "sess:"+sessionID).Err()
}
```

---

### 场景 7：秒杀库存扣减

参考 `scenarios-2026.md` 第 2 题秒杀方案。核心 Lua 脚本：

```go
const stockDeductScript = `
local stock = tonumber(redis.call('GET', KEYS[1]))
if stock == nil then return -2 end
if stock <= 0 then return -1 end
if stock < tonumber(ARGV[1]) then return 0 end
return redis.call('DECRBY', KEYS[1], ARGV[1])`

var deductScript = redis.NewScript(stockDeductScript)

func Deduct(ctx context.Context, productID string, qty int) error {
    key := "stock:" + productID
    result, err := deductScript.Run(ctx, rdb, []string{key}, qty).Int()
    if err != nil {
        return err
    }
    switch result {
    case -2:
        return errors.New("product not found")
    case -1:
        return errors.New("sold out")
    case 0:
        return errors.New("insufficient")
    default:
        return nil  // 扣减成功，剩余库存 = result
    }
}
```

---

### 场景 8：签到（Bitmap）

```go
package signin

import (
    "context"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"
)

type SignIn struct {
    client *redis.Client
}

func (s *SignIn) key(userID int64, date time.Time) string {
    return fmt.Sprintf("sign:%d:%s", userID, date.Format("200601"))
}

// 签到
func (s *SignIn) Sign(ctx context.Context, userID int64, date time.Time) (bool, error) {
    key := s.key(userID, date)
    offset := int64(date.Day() - 1)  // 1 号 → offset 0
    old, err := s.client.SetBit(ctx, key, offset, 1).Result()
    if err != nil {
        return false, err
    }
    // 设置过期（保留 12 个月）
    s.client.Expire(ctx, key, 365*24*time.Hour)
    return old == 0, nil  // true = 首次签到
}

// 查询是否签到
func (s *SignIn) IsSigned(ctx context.Context, userID int64, date time.Time) (bool, error) {
    key := s.key(userID, date)
    offset := int64(date.Day() - 1)
    v, err := s.client.GetBit(ctx, key, offset).Result()
    return v == 1, err
}

// 本月签到天数
func (s *SignIn) MonthlyCount(ctx context.Context, userID int64, month time.Time) (int64, error) {
    key := s.key(userID, month)
    return s.client.BitCount(ctx, key, nil).Result()
}
```

**内存对比**：100 万用户 × 12 个月 × 4 字节/月 = **约 46 MB**。用 String 存的话 100 万 × 30 天 × 20 字节 = 570 MB。**省 10 倍**。

---

### 场景 9：附近的人（GEO）

```go
package geo

import (
    "context"
    "github.com/redis/go-redis/v9"
)

type NearbyService struct {
    client *redis.Client
    key    string
}

func New(client *redis.Client, name string) *NearbyService {
    return &NearbyService{client: client, key: "geo:" + name}
}

// 添加位置
func (g *NearbyService) Add(ctx context.Context, memberID string, lng, lat float64) error {
    return g.client.GeoAdd(ctx, g.key, &redis.GeoLocation{
        Name:      memberID,
        Longitude: lng,
        Latitude:  lat,
    }).Err()
}

type NearbyResult struct {
    MemberID string
    Distance float64  // km
}

// 查附近
func (g *NearbyService) Nearby(ctx context.Context, lng, lat, radiusKm float64, limit int) ([]NearbyResult, error) {
    result, err := g.client.GeoSearch(ctx, g.key, &redis.GeoSearchQuery{
        Longitude:  lng,
        Latitude:   lat,
        Radius:     radiusKm,
        RadiusUnit: "km",
        Sort:       "ASC",
        Count:      limit,
    }).Result()
    if err != nil {
        return nil, err
    }

    // 查距离需要用 GeoSearchLocation
    withDist, err := g.client.GeoSearchLocation(ctx, g.key, &redis.GeoSearchLocationQuery{
        GeoSearchQuery: redis.GeoSearchQuery{
            Longitude: lng, Latitude: lat,
            Radius: radiusKm, RadiusUnit: "km",
            Sort: "ASC", Count: limit,
        },
        WithDist: true,
    }).Result()
    if err != nil {
        return nil, err
    }

    results := make([]NearbyResult, 0, len(withDist))
    for _, loc := range withDist {
        results = append(results, NearbyResult{
            MemberID: loc.Name,
            Distance: loc.Dist,
        })
    }
    _ = result
    return results, nil
}
```

---

### 场景 10：热点探测 + 本地缓存兜底

```go
package hotkey

import (
    "context"
    "sync"
    "sync/atomic"
    "time"

    "github.com/redis/go-redis/v9"
    "github.com/allegro/bigcache/v3"
)

type HotKeyCache struct {
    redis    *redis.Client
    local    *bigcache.BigCache
    counter  sync.Map          // key -> *atomic.Int64
    hotSet   sync.Map          // key -> struct{}（标记为热 key）
    threshold int64            // 热 key 阈值（次/分钟）
}

func New(rdb *redis.Client) *HotKeyCache {
    local, _ := bigcache.New(context.Background(), bigcache.DefaultConfig(1*time.Minute))
    h := &HotKeyCache{
        redis:     rdb,
        local:     local,
        threshold: 1000,  // 每分钟 1000 次即视为热 key
    }

    // 定时清零计数
    go func() {
        for range time.Tick(time.Minute) {
            h.counter = sync.Map{}
        }
    }()
    return h
}

func (h *HotKeyCache) Get(ctx context.Context, key string) (string, error) {
    // 1. 记录访问频次
    v, _ := h.counter.LoadOrStore(key, new(atomic.Int64))
    cnt := v.(*atomic.Int64).Add(1)
    if cnt > h.threshold {
        h.hotSet.Store(key, struct{}{})
    }

    // 2. 热 key 走本地缓存
    if _, isHot := h.hotSet.Load(key); isHot {
        if data, err := h.local.Get(key); err == nil {
            return string(data), nil
        }
    }

    // 3. Redis
    val, err := h.redis.Get(ctx, key).Result()
    if err != nil {
        return "", err
    }

    // 4. 热 key 写本地
    if _, isHot := h.hotSet.Load(key); isHot {
        h.local.Set(key, []byte(val))
    }
    return val, nil
}
```

**思路**：应用侧统计访问频次，超过阈值的 key 自动降级到本地缓存，减轻 Redis 压力。

---

## 总结

**Redis 面试的核心考察点**：

1. **数据结构与场景匹配**：知道每个类型的底层实现和适用场景
2. **性能设计**：单线程为什么快、Pipeline vs Transaction vs Lua
3. **缓存三大问题**：穿透、击穿、雪崩的成因和解法
4. **分布式锁**：SetNX + Lua + UUID + 看门狗，以及 Redlock 的争议
5. **持久化**：RDB 和 AOF 的取舍、混合持久化
6. **集群与高可用**：主从、Sentinel、Cluster 的差异
7. **生产坑**：大 key、热 key、内存碎片、慢查询

**Go 生态推荐**：
- 客户端：`redis/go-redis/v9`（主流）或 `rueidis`（性能）
- 分布式锁：直接手写（本文示例）或 `redis/go-redis/v9/redislock`
- 布隆过滤器：`RedisBloom` 模块（比自己写效率高）
- 消息队列：Redis Stream（简单场景）或 Kafka（大规模）
