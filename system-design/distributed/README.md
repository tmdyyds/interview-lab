# 分布式系统

## 已收录

- [transactions.md](./transactions.md) — 微服务分布式事务深度面试题（2PC / TCC / Saga / 本地消息表 / 事务消息 / Seata / 三大共性难题）

## 计划收录

- **CAP 定理与 BASE 理论**
- **分布式事务**
  - 2PC / 3PC ✅
  - TCC（Try-Confirm-Cancel）✅
  - Saga ✅
  - 本地消息表 / 事务消息（RocketMQ）✅
  - 最终一致性方案对比 ✅
- **共识算法**
  - Paxos（Basic / Multi）
  - Raft
  - Zab（ZooKeeper）
- **分布式锁**
  - 基于 Redis：SETNX、Redlock、看门狗
  - 基于 ZooKeeper：临时顺序节点
  - 基于 etcd：Lease + Compare
- **分布式 ID**
  - UUID
  - 雪花算法（Snowflake）及时钟回拨问题
  - 号段模式（Leaf）
  - Redis INCR
- **服务注册与发现**：Zookeeper、etcd、Consul、Nacos
- **配置中心**：Apollo、Nacos
- **链路追踪**：Jaeger、SkyWalking、OpenTelemetry

## 面试常问

- CAP 三选二为什么？
- Raft 选主流程
- 雪花算法时钟回拨怎么处理？
- 分布式锁的关键要素（互斥、防死锁、可重入、性能）
