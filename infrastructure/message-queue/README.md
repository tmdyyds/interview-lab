# 消息队列

## 已收录

- [rabbitmq.md](./rabbitmq.md) — RabbitMQ 基础与重点(AMQP / 四种 Exchange / 可靠性三件套 / DLX / 延迟队列 / Prefetch / 集群 / 顺序 / 幂等 / 堆积 / PHP&Go 代码示例 / 15 道面试题)

## 计划收录

- **Kafka**
  - Broker / Topic / Partition / Replica
  - 生产者：ack、幂等、事务
  - 消费者：Consumer Group、offset 管理、rebalance
  - 存储：顺序写、页缓存、零拷贝
  - ISR、HW、LEO
  - 与 Zookeeper 的关系、KRaft
- **RabbitMQ**
  - AMQP 协议
  - Exchange 类型：direct / topic / fanout / headers
  - 死信队列、延迟队列
  - 集群模式
- **RocketMQ**
  - 顺序消息、事务消息、延迟消息
  - NameServer / Broker / Producer / Consumer
  - 与 Kafka 对比

## 通用面试题

- 如何保证消息不丢失？
- 如何保证消息不重复消费？（幂等性）
- 如何保证消息顺序？
- 如何处理消息堆积？
- MQ 选型对比
