# 系统设计核心概念

## 计划收录

- **限流**
  - 固定窗口 / 滑动窗口
  - 漏桶 / 令牌桶
  - 分布式限流（Redis + Lua）
  - 自适应限流（go-zero）
- **熔断降级**
  - 断路器状态机（Closed / Open / Half-Open）
  - Hystrix / Sentinel / Resilience4j / go-zero breaker
- **重试与超时**
  - 指数退避
  - 抖动
  - 超时传递
- **缓存**
  - Cache-Aside / Read-Through / Write-Through / Write-Behind
  - 缓存击穿 / 穿透 / 雪崩
  - 缓存与数据库一致性方案（延迟双删、Canal、Debezium）
  - 多级缓存（本地 + Redis）
- **消息队列削峰**
- **幂等性**：Token、去重表、乐观锁、悲观锁

## 面试常问

- 令牌桶和漏桶的区别？各自适合什么场景？
- 缓存一致性方案对比
- 如何设计一个分布式限流？
