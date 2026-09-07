# Redis

## 计划收录

- **数据类型与底层结构**
  - String — SDS
  - List — quicklist / listpack
  - Hash — listpack / hashtable
  - Set — intset / hashtable / listpack
  - ZSet — 跳表 + 字典
  - Stream / Bitmap / HyperLogLog / GEO
- **持久化**：RDB / AOF / 混合持久化
- **主从复制**：全量同步、增量同步、psync
- **哨兵（Sentinel）**：故障发现、故障转移、脑裂问题
- **Cluster**：一致性哈希 vs 哈希槽（16384）
- **过期策略**：定期删除 + 惰性删除
- **内存淘汰策略**：allkeys-lru、volatile-lru 等
- **缓存问题**：
  - 击穿（热点 key 过期）
  - 穿透（查询不存在数据）
  - 雪崩（大量 key 同时过期）
  - 一致性：Cache-Aside、Read-Through、Write-Through、Write-Behind
- **分布式锁**：SETNX、Redlock、看门狗（Redisson）
- **管道（Pipeline）与事务（MULTI）**
- **Lua 脚本**
- **单线程 + 多路复用模型**、Redis 6 的多线程

## 已收录

- [cache-patterns.md](./cache-patterns.md) — 缓存策略：击穿/穿透/雪崩/一致性（含完整 PHP 实例）
- [distributed-lock.md](./distributed-lock.md) — 分布式锁：Owner 标识 + Lua 原子释放 + 看门狗续期
- [distributed-lock-advanced.md](./distributed-lock-advanced.md) — 分布式锁高级面试题：Redlock 争论 / Fencing Token / GC 停顿 / 可重入·公平·读写锁 / 生产事故复盘
- [persistence.md](./persistence.md) — 持久化：RDB / AOF / 混合（含 fork+COW 原理）
- [sentinel.md](./sentinel.md) — 主从复制 + 哨兵：同步流程 / 故障转移 / 脑裂防护
- [cluster.md](./cluster.md) — Cluster 集群：哈希槽 / 路由 / 迁移 / 故障转移
- [use-cases.md](./use-cases.md) — 常用业务场景：限流 / 延迟队列 / 排行榜 / 抢红包 / 库存等 12 个

## 面试常问

- 为什么 Redis 单线程还这么快？
- ZSet 为什么用跳表而不是红黑树？
- Redlock 有什么问题？
- 缓存与数据库一致性方案
- Cluster 为什么是 16384 个槽？
