# 系统设计

## 目录

- [concepts/](./concepts/README.md) — 核心概念：限流、熔断、缓存、一致性
- [distributed/](./distributed/README.md) — 分布式：CAP、共识、分布式事务
- [cases/](./cases/README.md) — 实战 case：秒杀、短链、Feed、IM

## 常考重点

- 限流算法：计数器、滑动窗口、漏桶、令牌桶
- 熔断降级：Hystrix、Sentinel、go-zero 熔断器
- 缓存三大问题及方案
- CAP 定理、BASE 理论
- 分布式一致性：2PC、3PC、TCC、Saga、本地消息表
- 共识算法：Paxos、Raft、Zab
- 分布式 ID：UUID、雪花、号段
- 高可用架构：主备、主从、集群
