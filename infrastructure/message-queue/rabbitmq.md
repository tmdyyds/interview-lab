# RabbitMQ 基础与重点

**标签**: #rabbitmq #mq #amqp #interview #高频
**难度**: ⭐⭐⭐⭐

RabbitMQ 是基于 AMQP 协议的开源消息中间件，用 Erlang 编写。因为路由灵活、可靠性高、协议标准化，是**企业级应用**（金融、电商订单、异步解耦）的首选。

---

## 目录

1. [RabbitMQ 是什么，AMQP 协议](#一rabbitmq-是什么amqp-协议)
2. [核心概念（七件套）](#二核心概念七件套)
3. [四种 Exchange 类型](#三四种-exchange-类型)
4. [消息完整生命周期](#四消息完整生命周期)
5. [消息可靠性三大机制](#五消息可靠性三大机制)
6. [死信队列（DLX）](#六死信队列dlx)
7. [延迟队列](#七延迟队列)
8. [Prefetch / QoS](#八prefetch--qos)
9. [集群与高可用](#九集群与高可用)
10. [消息顺序性](#十消息顺序性)
11. [消息重复消费与幂等](#十一消息重复消费与幂等)
12. [消息堆积怎么处理](#十二消息堆积怎么处理)
13. [RabbitMQ vs Kafka vs RocketMQ](#十三rabbitmq-vs-kafka-vs-rocketmq)
14. [PHP / Go 生产代码示例](#十四php--go-生产代码示例)
15. [常见面试题速览](#十五常见面试题速览)

---

## 一、RabbitMQ 是什么，AMQP 协议

### 1.1 RabbitMQ 简介

- **语言**：Erlang（天然支持高并发和分布式）
- **协议**：AMQP 0-9-1（默认）、STOMP、MQTT、HTTP
- **核心特性**：灵活路由、消息可靠、集群高可用、多语言客户端

### 1.2 AMQP（Advanced Message Queuing Protocol）

AMQP 是一个**标准化的应用层协议**，定义了消息中间件的通用行为——生产、路由、传输、消费。

**关键**：AMQP 是**协议标准**（像 HTTP、SMTP），不是产品。RabbitMQ 是 AMQP 的一个实现。理论上你换成其他 AMQP 兼容的 Broker（如 Qpid），业务代码可以不改。

**AMQP 三层模型**：
```
Producer → [Exchange → Binding → Queue] → Consumer
              ↑ Broker（RabbitMQ Server）↑
```

---

## 二、核心概念（七件套）

理解 RabbitMQ 就是理解这 7 个核心概念:

```
┌──────────────────────────────────────────────────────────┐
│                     RabbitMQ Broker                      │
│  ┌────────────────────────────────────────────────────┐  │
│  │   Virtual Host (vhost)                             │  │
│  │  ┌──────────┐     ┌──────────┐     ┌──────────┐    │  │
│  │  │ Exchange │ ──▶ │  Binding │ ──▶ │  Queue   │    │  │
│  │  └──────────┘     └──────────┘     └──────────┘    │  │
│  └────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
      ↑                                          ↓
   Producer                                    Consumer
    (发消息)                                    (消费消息)
      ↑         ↑                                ↓
      └── TCP Connection → Channel（多路复用）───┘
```

| 概念 | 说明 |
|---|---|
| **Producer** | 消息生产者，发消息到 Exchange |
| **Consumer** | 消息消费者，从 Queue 取消息处理 |
| **Broker** | RabbitMQ Server 本身 |
| **Virtual Host** | 逻辑隔离单位（类似 MySQL 的 database），不同 vhost 权限、Exchange、Queue 都互相独立 |
| **Exchange** | 交换机，接收 Producer 的消息，按规则**路由**到 Queue（Exchange 不存消息） |
| **Queue** | 队列，真正存储消息的地方，Consumer 从这里取 |
| **Binding** | Exchange 到 Queue 的绑定规则（含 routing key） |
| **Connection** | TCP 连接（Producer/Consumer ↔ Broker） |
| **Channel** | 建立在 Connection 上的**虚拟通道**，AMQP 大多数操作都在 Channel 上 |

### 2.1 为什么要 Channel？

- 每个 Connection 创建代价大（TCP 握手 + 认证）
- **Channel 是轻量的虚拟连接**，一个 Connection 可以有多个 Channel
- 多线程/多协程时，**每个线程/协程用自己的 Channel**（Channel 非线程安全）

```
Application Process
    ├── Connection (TCP)
    │     ├── Channel 1  ← 协程 A 用
    │     ├── Channel 2  ← 协程 B 用
    │     └── Channel 3  ← 协程 C 用
```

### 2.2 Exchange 不存消息

**关键认知**：Exchange 只是"路由器"。消息进 Exchange 后：
- 如果找到匹配的 Binding → 转发到对应 Queue
- 找不到 Binding → **消息被丢弃**（除非开启 `mandatory` 让 Broker 回退给 Producer）

所以：**Producer 发消息前必须先声明 Exchange 和 Binding**。

---

## 三、四种 Exchange 类型

Exchange 的类型决定了消息如何路由到 Queue。

### 3.1 Direct（精确匹配）

Binding key 和消息的 routing key **完全相等**才路由。

```
Producer 发消息 routing_key="error"
    ↓
Direct Exchange
    ├── Binding "error" ──▶ Queue_ErrorLog     ✅ 匹配
    ├── Binding "info"  ──▶ Queue_InfoLog      ❌ 不匹配
    └── Binding "warn"  ──▶ Queue_WarnLog      ❌ 不匹配
```

**典型场景**：日志分级、事件类型分发。

### 3.2 Fanout（广播）

**忽略 routing key**，消息广播到所有绑定的 Queue。

```
Producer 发消息
    ↓
Fanout Exchange
    ├── Queue_A  ✅ 都收到
    ├── Queue_B  ✅ 都收到
    └── Queue_C  ✅ 都收到
```

**典型场景**：广播通知、多消费者独立处理同一份消息（用户下单 → 通知库存、积分、推送、日志等）。

### 3.3 Topic（模式匹配）

Binding key 支持**通配符**：
- `*` 匹配**一个**单词
- `#` 匹配**零个或多个**单词

routing key 是用 `.` 分隔的单词。

```
Producer 发消息 routing_key="order.created.vip"
    ↓
Topic Exchange
    ├── Binding "order.*"      ❌（只匹配 order.xxx，不匹配三段）
    ├── Binding "order.#"      ✅（匹配 order 开头任何）
    ├── Binding "*.created.*"  ✅（三段，中间是 created）
    ├── Binding "#.vip"        ✅（结尾是 vip）
    └── Binding "user.#"       ❌
```

**典型场景**：需要多维度路由的场景（订单事件、地区+等级分发）。

### 3.4 Headers（头匹配，很少用）

按消息**头（headers）**里的键值对匹配，忽略 routing key。

```
消息 headers: {type: "order", region: "asia"}
    ↓
Headers Exchange
    ├── Binding x-match=all: {type:"order", region:"asia"}  ✅ 全部匹配
    ├── Binding x-match=any: {type:"user", region:"asia"}   ✅ 任一匹配
    └── Binding x-match=all: {type:"order", region:"eu"}    ❌
```

**性能比 Topic 差**，实战几乎不用。

### 3.5 对比表

| 类型 | 路由依据 | 典型场景 |
|---|---|---|
| Direct | routing key 完全相等 | 精确分发（日志级别） |
| Fanout | 无（广播） | 全量通知 |
| Topic | routing key 通配符 | 多维度分类分发 |
| Headers | headers 键值对 | 复杂条件匹配（少用） |

**记忆**：**"精确用 Direct，广播用 Fanout，模式用 Topic"**。

---

## 四、消息完整生命周期

```
1. Producer 声明 Exchange + Queue + Binding（幂等，可重复声明）
   
2. Producer 通过 Channel 发送消息到 Exchange
   ├── mandatory=true：如果无 Binding 匹配，Broker 回退消息
   └── immediate=true：如果 Queue 上没消费者，Broker 拒收（已废弃）
   
3. Exchange 根据类型 + Binding 路由消息到 Queue
   ├── 找到匹配的 Queue → 投递
   └── 找不到 → 丢弃 / 回退（取决于 mandatory）
   
4. Queue 存储消息
   ├── 内存（快，重启丢失）
   └── 磁盘（慢，持久化）
   
5. Broker 推送消息到 Consumer（push 模式）或 Consumer 拉取（basic.get，少用）
   
6. Consumer 处理消息
   ├── ACK（basic.ack）→ Broker 删除消息
   ├── NACK（basic.nack）→ 根据 requeue 参数决定重投或丢弃
   └── 消费超时/断连 → 消息重新入队（unacked → ready）
```

---

## 五、消息可靠性三大机制

消息中间件的**核心问题**：如何保证消息不丢？RabbitMQ 分三个环节保证。

### 5.1 生产者可靠：Publisher Confirm

Producer 发消息给 Broker 后，怎么知道 Broker 真的收到了？

**方案 A：事务模式**（transaction，性能极差，不推荐）
```
channel.txSelect()      -- 开启事务
channel.basicPublish(...)
channel.txCommit()      -- 提交（同步阻塞等待）
```
性能下降 100 倍以上。

**方案 B：Confirm 模式**（推荐）

```
channel.confirmSelect()  -- 开启 confirm

Producer 发消息 → Broker 收到后**异步**回 ACK/NACK
    ├── basic.ack：Broker 已收到（可能已入队，也可能持久化完成）
    └── basic.nack：Broker 处理失败
```

三种使用方式：
- **单条同步**：发一条等一次 ACK → 慢
- **批量同步**：发 100 条一起等 ACK → 中等
- **异步回调**：注册 ACK/NACK 回调，Producer 不阻塞 → **推荐，性能最好**

### 5.2 Broker 可靠：持久化

即使 Broker 收到消息，重启后可能丢失。三处都要持久化才安全：

```
1. Exchange 持久化：durable = true
   → Broker 重启后 Exchange 还在

2. Queue 持久化：durable = true
   → Broker 重启后 Queue 还在（但 Queue 里的消息不一定）

3. Message 持久化：delivery_mode = 2
   → 消息落盘
```

**三个 durable 必须都开**，缺一不可。只开 Queue 持久化没用，重启后消息还是没了。

**注意**：持久化不是 100% 安全，因为 RabbitMQ 是**先写内存再刷盘**（fsync 每 100~200ms 一次）。断电瞬间可能丢一小部分未刷盘消息。**极致可靠要用 Publisher Confirm + 集群/镜像队列**。

### 5.3 消费者可靠：手动 ACK

Consumer 拿到消息后，如果处理失败或没处理完就崩了怎么办？

**关键**：**关闭自动 ACK**（`auto_ack=false`），处理完成再手动 ACK。

```
Consumer 收到消息
    ├── 处理成功 → basic.ack → Broker 删除消息
    ├── 处理失败但可重试 → basic.nack(requeue=true) → 重回队列
    ├── 处理失败且不重试 → basic.nack(requeue=false) → 丢弃（或进死信）
    └── Consumer 断连 → 消息自动 requeue（保护机制）
```

**auto_ack 是坑**：消息一投递就 ACK，Consumer 崩了消息就永久丢失。**生产强制手动 ACK**。

### 5.4 完整可靠性组合拳

```
Producer:  confirm 模式 + mandatory=true + 落地本地消息表（终极兜底）
Broker:    durable exchange + durable queue + persistent message + 镜像/仲裁队列
Consumer:  手动 ACK + 幂等处理 + 死信队列
```

---

## 六、死信队列（DLX）

### 6.1 什么是死信

消息在 Queue 里满足下列**任一条件**会变成"死信"：

1. **被拒绝**：`basic.nack(requeue=false)` 或 `basic.reject(requeue=false)`
2. **消息 TTL 到期**：`x-message-ttl` 或消息本身的 `expiration` 属性
3. **队列长度超限**：`x-max-length` 达上限，头部消息被挤出

### 6.2 死信交换机（Dead Letter Exchange, DLX）

给正常 Queue 配置一个**死信 Exchange**，消息变死信时自动路由到死信 Queue：

```
正常 Queue: normal_queue
    x-dead-letter-exchange: dlx_exchange
    x-dead-letter-routing-key: dlx.route.key
                    ↓ 消息变死信时
                    ↓
              DLX Exchange
                    ↓ 按 routing key 路由
                    ↓
                Dead Letter Queue
                    ↓
              死信消费者（重试 / 告警 / 归档）
```

### 6.3 生产用法

**用途 1：失败重试兜底**
```
Consumer 处理失败 → nack(requeue=false) → 进入 DLQ
DLQ 消费者：记录日志、告警、人工介入
```

**用途 2：延迟队列**（下节详细讲）

**用途 3：消息归档**
过期消息不删，进 DLQ 存到冷库归档。

### 6.4 声明示例(PHP + Go)

#### 6.4.1 完整流程图

```
业务消息生产
    ↓
┌─────────────────────────────────────────┐
│   业务 Exchange: order.exchange         │
└─────────────────────────────────────────┘
    ↓ 按 routing_key 路由
┌─────────────────────────────────────────┐
│   业务 Queue: order.queue               │
│   ├── x-dead-letter-exchange = dlx.exch │
│   ├── x-dead-letter-routing-key = failed│
│   └── x-message-ttl = 60000ms          │
└─────────────────────────────────────────┘
    ↓ 消息变死信时(拒绝/TTL/超长)
┌─────────────────────────────────────────┐
│   死信 Exchange: dlx.exchange           │
└─────────────────────────────────────────┘
    ↓
┌─────────────────────────────────────────┐
│   死信 Queue: dlx.queue                 │
└─────────────────────────────────────────┘
    ↓
死信消费者(重试/告警/归档)
```

#### 6.4.2 PHP 完整声明代码

```php
<?php
use PhpAmqpLib\Connection\AMQPStreamConnection;
use PhpAmqpLib\Wire\AMQPTable;

// ============ Step 1: 建立连接和 Channel ============

$connection = new AMQPStreamConnection(
    'localhost',   // Broker 主机
    5672,          // AMQP 端口(默认 5672,SSL 用 5671)
    'guest',       // 用户名
    'guest'        // 密码
);
$channel = $connection->channel();

// ============ Step 2: 先声明死信 Exchange 和 Queue ============
// 【顺序很重要】必须先建好死信基础设施,再声明业务 Queue
// 否则业务 Queue 的死信配置引用不到实际的死信 Exchange

// 声明死信 Exchange
$channel->exchange_declare(
    'dlx.exchange',    // Exchange 名称
    'direct',          // 类型: direct/fanout/topic/headers
    false,             // passive: false = 不存在则创建;true = 只检查不创建
    true,              // durable: true = 持久化,Broker 重启后仍存在
    false              // auto_delete: false = 不自动删除(生产必须 false)
);

// 声明死信 Queue
$channel->queue_declare(
    'dlx.queue',       // Queue 名称
    false,             // passive
    true,              // durable: 持久化(重启后 Queue 定义仍在)
    false,             // exclusive: false = 允许多个 Consumer 消费
    false              // auto_delete: 无 Consumer 时不删除
);

// 绑定死信 Queue 到死信 Exchange
$channel->queue_bind(
    'dlx.queue',       // 要绑定的 Queue
    'dlx.exchange',    // 绑定到的 Exchange
    'failed'           // routing key(direct 类型必须精确匹配)
);

// ============ Step 3: 声明业务 Exchange ============

$channel->exchange_declare(
    'order.exchange',
    'direct',
    false,
    true,              // durable
    false
);

// ============ Step 4: 声明业务 Queue,附带死信配置 ============

// AMQPTable 是键值对的容器,用来传特殊参数(x- 开头的)
$queueArgs = new AMQPTable([
    // 【核心 1】消息变死信时,转发到哪个 Exchange
    'x-dead-letter-exchange' => 'dlx.exchange',

    // 【核心 2】死信被转发时使用的 routing key
    // 必须和 dlx.queue 的 binding key 一致才能路由到 dlx.queue
    // 如果省略,则沿用消息原有的 routing key
    'x-dead-letter-routing-key' => 'failed',

    // 【可选 1】队列级 TTL,所有消息 60 秒未消费自动变死信
    'x-message-ttl' => 60000,

    // 【可选 2】队列最大消息数,超过后头部消息挤出变死信
    // 'x-max-length' => 10000,

    // 【可选 3】队列最大字节数
    // 'x-max-length-bytes' => 100 * 1024 * 1024,  // 100MB

    // 【可选 4】溢出策略: drop-head(默认) / reject-publish
    // 'x-overflow' => 'reject-publish',
]);

$channel->queue_declare(
    'order.queue',     // 业务 Queue 名称
    false,             // passive
    true,              // durable: 持久化
    false,             // exclusive
    false,             // auto_delete
    false,             // nowait: 是否等待 Broker 响应
    $queueArgs         // ★ 关键:附带死信配置参数
);

// 绑定业务 Queue 到业务 Exchange
$channel->queue_bind(
    'order.queue',
    'order.exchange',
    'order.created'    // 业务消息用这个 routing key 发送
);

// ============ Step 5: 声明完成后关闭连接 ============
// 声明代码通常放在启动时执行一次,不需要每次发消息都跑

$channel->close();
$connection->close();

echo "所有 Exchange/Queue/Binding 声明完成\n";
```

#### 6.4.3 Go 完整声明代码

```go
package main

import (
    "log"

    amqp "github.com/rabbitmq/amqp091-go"
)

func setupQueues() error {
    // ============ Step 1: 建立连接和 Channel ============
    conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
    if err != nil {
        return err
    }
    defer conn.Close()

    ch, err := conn.Channel()
    if err != nil {
        return err
    }
    defer ch.Close()

    // ============ Step 2: 先声明死信 Exchange + Queue ============

    // 声明死信 Exchange
    err = ch.ExchangeDeclare(
        "dlx.exchange", // name: Exchange 名称
        "direct",       // kind: 类型(direct/fanout/topic/headers)
        true,           // durable: 持久化,Broker 重启后仍存在
        false,          // autoDelete: 没 Queue 绑定时是否自动删除(false)
        false,          // internal: 是否是内部 Exchange(用户不能直接发消息给它)
        false,          // noWait: 是否等待 Broker 响应
        nil,            // args: 额外参数(通常 nil)
    )
    if err != nil {
        return err
    }

    // 声明死信 Queue
    _, err = ch.QueueDeclare(
        "dlx.queue", // name: Queue 名称
        true,        // durable: 持久化
        false,       // autoDelete: 无消费者时是否自动删除(false)
        false,       // exclusive: 独占(仅当前 Connection 可用,断连即删)
        false,       // noWait
        nil,         // args
    )
    if err != nil {
        return err
    }

    // 绑定死信 Queue 到死信 Exchange
    err = ch.QueueBind(
        "dlx.queue",    // name: 要绑定的 Queue
        "failed",       // key: routing key(direct 类型必须精确匹配)
        "dlx.exchange", // exchange: 绑定到的 Exchange
        false,          // noWait
        nil,            // args
    )
    if err != nil {
        return err
    }

    // ============ Step 3: 声明业务 Exchange ============

    err = ch.ExchangeDeclare(
        "order.exchange",
        "direct",
        true,  // durable
        false, // autoDelete
        false, // internal
        false, // noWait
        nil,
    )
    if err != nil {
        return err
    }

    // ============ Step 4: 声明业务 Queue,附带死信配置 ============

    // amqp.Table 是 map[string]interface{} 的别名,用来传特殊参数
    args := amqp.Table{
        // 【核心 1】消息变死信时,转发到哪个 Exchange
        "x-dead-letter-exchange": "dlx.exchange",

        // 【核心 2】死信转发时使用的 routing key
        // 必须和 dlx.queue 的 binding key 一致
        // 省略则沿用消息原 routing key
        "x-dead-letter-routing-key": "failed",

        // 【可选 1】队列级 TTL(毫秒),消息在队列 60s 未消费变死信
        "x-message-ttl": int32(60000),

        // 【可选 2】队列最大消息数,溢出时头部消息挤出变死信
        // "x-max-length": int32(10000),

        // 【可选 3】队列最大字节数
        // "x-max-length-bytes": int32(100 * 1024 * 1024),

        // 【可选 4】溢出策略
        // "x-overflow": "reject-publish",

        // 【可选 5】队列类型: classic / quorum / stream
        // "x-queue-type": "quorum",  // 使用仲裁队列(强一致)
    }

    _, err = ch.QueueDeclare(
        "order.queue", // name
        true,          // durable
        false,         // autoDelete
        false,         // exclusive
        false,         // noWait
        args,          // ★ 关键:附带死信配置
    )
    if err != nil {
        return err
    }

    // 绑定业务 Queue 到业务 Exchange
    err = ch.QueueBind(
        "order.queue",
        "order.created", // 业务消息用这个 routing key 发送
        "order.exchange",
        false,
        nil,
    )
    if err != nil {
        return err
    }

    log.Println("所有 Exchange/Queue/Binding 声明完成")
    return nil
}
```

#### 6.4.4 死信 Consumer 示例(处理死信)

死信不能不处理,否则死信队列会持续堆积。**必须有 Consumer 消费死信**:

```php
<?php
// 死信消费者:告警 + 归档
$callback = function ($msg) {
    // 从死信 header 里能拿到"为什么变死信"的完整信息
    $props = $msg->get_properties();
    $headers = $props['application_headers']->getNativeData();

    // x-death 是数组,记录消息在各 Queue 的死信历程
    // 结构类似:
    // [
    //   [
    //     'count' => 1,                    // 变死信次数
    //     'reason' => 'expired',           // 原因: rejected/expired/maxlen
    //     'queue' => 'order.queue',        // 从哪个 Queue 变死信的
    //     'time' => 1699999999,
    //     'exchange' => 'order.exchange',
    //     'routing-keys' => ['order.created'],
    //   ]
    // ]
    $deathInfo = $headers['x-death'][0] ?? null;

    error_log(sprintf(
        "死信告警: 原因=%s, 来源 Queue=%s, 内容=%s",
        $deathInfo['reason'] ?? 'unknown',
        $deathInfo['queue'] ?? 'unknown',
        $msg->body
    ));

    // 落库归档,或者触发告警系统
    archiveDeadLetter($msg->body, $deathInfo);

    // 死信 ACK,从死信队列中清除
    $msg->ack();
};

$channel->basic_consume(
    'dlx.queue',
    '',
    false,
    false,   // 手动 ACK
    false,
    false,
    $callback
);

while ($channel->is_consuming()) {
    $channel->wait();
}
```

#### 6.4.5 参数详解速查表

| 参数 | 类型 | 作用 | 示例值 |
|---|---|---|---|
| `x-dead-letter-exchange` | string | 消息变死信时的 Exchange | `"dlx.exchange"` |
| `x-dead-letter-routing-key` | string | 变死信时用的 routing key(省略=沿用原 key) | `"failed"` |
| `x-message-ttl` | int(ms) | 队列级消息 TTL | `60000` |
| `x-expires` | int(ms) | 队列本身空闲多久后删除 | `3600000` |
| `x-max-length` | int | 队列最大消息数 | `10000` |
| `x-max-length-bytes` | int | 队列最大字节数 | `104857600` |
| `x-overflow` | string | 溢出策略:`drop-head`/`reject-publish` | `"drop-head"` |
| `x-max-priority` | int | 优先级队列上限(0-255) | `10` |
| `x-queue-mode` | string | `default`/`lazy`(lazy 优先落盘) | `"lazy"` |
| `x-queue-type` | string | `classic`/`quorum`/`stream` | `"quorum"` |

#### 6.4.6 常见坑

**坑 1:声明顺序错误**
必须**先声明死信 Exchange 和 Queue**,再声明带死信配置的业务 Queue。虽然 RabbitMQ 不强制,但顺序反了万一死信没准备好,死信消息会**丢失**(找不到 Exchange 就丢)。

**坑 2:queue_declare 参数冲突**
如果 Queue 已存在,再次声明时**参数必须完全一致**,否则报 `PRECONDITION_FAILED`。改配置的做法:
- 删除旧 Queue 重建(消息会丢)
- 用 Policy 动态修改(RabbitMQ 支持)

**坑 3:死信队列自己也要设 DLX?**
一般**不要**给死信队列设 DLX,否则死信处理失败会形成**循环**。死信队列的 Consumer 应该只做归档/告警,不做业务处理。

**坑 4:TTL 和 x-dead-letter 是队列属性,不是消息属性**
队列级 TTL(`x-message-ttl`)一旦声明就固定了。**消息级 TTL** 靠发送时的 `expiration` 属性:
```php
$msg = new AMQPMessage($body, [
    'delivery_mode' => 2,
    'expiration' => '30000',  // 这条消息 30s 过期(注意是字符串)
]);
```
消息级 TTL 允许每条消息不同,但**仍受队头阻塞影响**(见第七节延迟队列)。

**坑 5:x-death header 是数组**
消息经过多次死信(比如 A → 死信 → B → 死信 → C),`x-death` 会累加多个元素。取当前次死信信息用 `x-death[0]`。

---

## 七、延迟队列

### 7.1 场景

- **订单 30 分钟未支付自动关闭**
- 定时通知
- 重试策略（失败后 30s / 5min / 30min 重试）

RabbitMQ 原生**没有延迟队列**，两种实现方式:

### 7.2 方案 A:TTL + DLX(经典)

#### 7.2.1 基本原理

```
延迟消息进入 delay_queue(不消费)
    ↓ 设置 x-message-ttl = 30 分钟
    ↓ 消息过期变死信
    ↓ 通过 DLX 路由到 actual_queue
    ↓
Consumer 消费 actual_queue
```

**声明**:
```
delay_queue:
    durable: true
    x-message-ttl: 1800000  (30 分钟)
    x-dead-letter-exchange: "dlx.order"
    x-dead-letter-routing-key: "order.expire"

actual_queue(订单过期处理):
    durable: true
    binding: dlx.order + "order.expire"
```

**缺点**:**队列 TTL 是"队头阻塞"**——消息按 FIFO 出队,如果队头消息 TTL 是 1 小时,队尾消息 TTL 是 10 分钟,队尾也要等队头到期后才检查。

**解决**:**给每条消息单独设 TTL**(`message.expiration`)——但依然有队头阻塞问题。**要精确延迟,得用方案 B**。

#### 7.2.2 重试策略实战:多级延迟队列(TTL + DLX)

需求:消费失败后 30s / 5min / 30min 三级重试,超过 3 次进死信告警。

**核心思路**:为**每个延迟档位建一个独立的 delay queue**,消息在 delay queue 里等 TTL 到期后自动进入业务队列被重新消费。

```
                           首次消费失败(重试 1)
业务消息 ──▶ order.queue ──────────────────▶ retry.30s.queue (TTL=30s)
                    ▲                              │ TTL 到期
                    │                              ▼
                    └── DLX 路由回来 ◀──── retry Exchange
                    
                          第二次失败(重试 2)
                       ──▶ retry.5m.queue (TTL=300s) ──▶ 同上
                       
                          第三次失败(重试 3)
                       ──▶ retry.30m.queue (TTL=1800s) ──▶ 同上
                       
                          第四次失败
                       ──▶ dlx.queue(彻底失败,告警+归档)
```

**关键设计**:
- 每个 retry queue 的 DLX 都指向业务 Exchange,routing key = 业务 key
- 消息 header 里带 `x-retry-count`,消费者读取后决定下一次进哪个 retry queue
- retry queue 只做"延迟等待",没有 Consumer

**PHP 完整实现**:

```php
<?php
use PhpAmqpLib\Connection\AMQPStreamConnection;
use PhpAmqpLib\Message\AMQPMessage;
use PhpAmqpLib\Wire\AMQPTable;

// ============ Step 1: 声明基础设施 ============

$conn = new AMQPStreamConnection('localhost', 5672, 'guest', 'guest');
$ch = $conn->channel();

// 业务 Exchange 和 Queue(消息最终会来这里被消费)
$ch->exchange_declare('order.exchange', 'direct', false, true, false);
$ch->queue_declare('order.queue', false, true, false, false);
$ch->queue_bind('order.queue', 'order.exchange', 'order.process');

// 死信 Exchange 和 Queue(超过最大重试次数进这里)
$ch->exchange_declare('dlx.exchange', 'direct', false, true, false);
$ch->queue_declare('dlx.queue', false, true, false, false);
$ch->queue_bind('dlx.queue', 'dlx.exchange', 'order.dead');

// 声明三个延迟等级的 retry queue
$retryLevels = [
    'retry.30s'  => 30 * 1000,        // 30 秒
    'retry.5m'   => 5 * 60 * 1000,    // 5 分钟
    'retry.30m'  => 30 * 60 * 1000,   // 30 分钟
];

foreach ($retryLevels as $queueName => $ttlMs) {
    $args = new AMQPTable([
        // TTL 到期后,消息经 DLX 回到业务 Exchange
        'x-message-ttl'             => $ttlMs,
        'x-dead-letter-exchange'    => 'order.exchange',   // ★ 回到业务 Exchange
        'x-dead-letter-routing-key' => 'order.process',    // ★ 用业务 routing key
    ]);
    $ch->queue_declare($queueName, false, true, false, false, false, $args);
}

// ============ Step 2: 业务 Consumer(带重试逻辑) ============

const MAX_RETRY = 3;

$callback = function (AMQPMessage $msg) use ($ch) {
    // 从 header 拿当前重试次数(header 不存在时为 0)
    $headers = [];
    if ($msg->has('application_headers')) {
        $headers = $msg->get('application_headers')->getNativeData();
    }
    $retryCount = $headers['x-retry-count'] ?? 0;

    try {
        // ── 业务处理 ──
        $data = json_decode($msg->body, true);
        processOrder($data);   // 可能抛异常

        $msg->ack();
        echo "[SUCCESS] order handled after {$retryCount} retries\n";

    } catch (Throwable $e) {
        echo "[FAIL] retry={$retryCount}, err={$e->getMessage()}\n";

        if ($retryCount >= MAX_RETRY) {
            // 达到最大次数,进死信队列(告警 + 人工介入)
            $deadMsg = new AMQPMessage($msg->body, [
                'delivery_mode' => AMQPMessage::DELIVERY_MODE_PERSISTENT,
                'application_headers' => new AMQPTable([
                    'x-retry-count' => $retryCount,
                    'x-failure-reason' => $e->getMessage(),
                ]),
            ]);
            $ch->basic_publish($deadMsg, 'dlx.exchange', 'order.dead');
        } else {
            // 选择下一级 retry queue
            $nextRetryQueue = match ($retryCount) {
                0 => 'retry.30s',    // 第 1 次失败 → 30 秒后重试
                1 => 'retry.5m',     // 第 2 次失败 → 5 分钟后重试
                2 => 'retry.30m',    // 第 3 次失败 → 30 分钟后重试
            };

            // 发到 retry queue,retry count +1
            // 注意:这里直接把 default exchange 加上 queue 名做 routing key
            // 也可以专门建一个 retry.exchange 做 direct 路由
            $retryMsg = new AMQPMessage($msg->body, [
                'delivery_mode' => AMQPMessage::DELIVERY_MODE_PERSISTENT,
                'application_headers' => new AMQPTable([
                    'x-retry-count' => $retryCount + 1,
                    'x-original-exchange' => 'order.exchange',
                    'x-first-death-time' => $headers['x-first-death-time'] ?? time(),
                ]),
            ]);
            // default exchange("")+ routing key = queue name 是特殊路由
            $ch->basic_publish($retryMsg, '', $nextRetryQueue);
        }

        // ★ 原消息必须 ACK(否则会重回业务 queue 立即被再消费,不是延迟)
        $msg->ack();
    }
};

$ch->basic_qos(null, 10, null);   // prefetch = 10
$ch->basic_consume('order.queue', '', false, false, false, false, $callback);

while ($ch->is_consuming()) {
    $ch->wait();
}
```

**Go 版本关键片段**:

```go
const maxRetry = 3

// retry queue 的 TTL 映射
var retryLevels = []struct {
    QueueName string
    TTL       int32
}{
    {"retry.30s", 30 * 1000},
    {"retry.5m", 5 * 60 * 1000},
    {"retry.30m", 30 * 60 * 1000},
}

// 声明 retry queue
for _, level := range retryLevels {
    args := amqp.Table{
        "x-message-ttl":             level.TTL,
        "x-dead-letter-exchange":    "order.exchange",
        "x-dead-letter-routing-key": "order.process",
    }
    ch.QueueDeclare(level.QueueName, true, false, false, false, args)
}

// Consumer 处理逻辑
for msg := range msgs {
    retryCount, _ := msg.Headers["x-retry-count"].(int32)

    if err := processOrder(msg.Body); err != nil {
        if int(retryCount) >= maxRetry {
            // 进死信
            ch.PublishWithContext(ctx, "dlx.exchange", "order.dead", false, false,
                amqp.Publishing{
                    DeliveryMode: amqp.Persistent,
                    Body:         msg.Body,
                    Headers: amqp.Table{
                        "x-retry-count":    retryCount,
                        "x-failure-reason": err.Error(),
                    },
                })
        } else {
            // 送入对应 retry queue
            nextQueue := retryLevels[retryCount].QueueName
            ch.PublishWithContext(ctx, "", nextQueue, false, false,
                amqp.Publishing{
                    DeliveryMode: amqp.Persistent,
                    Body:         msg.Body,
                    Headers: amqp.Table{
                        "x-retry-count": retryCount + 1,
                    },
                })
        }
        msg.Ack(false)  // ★ 原消息 ACK,防止立刻重试
        continue
    }
    msg.Ack(false)
}
```

**关键点**:

1. **消息必须 ACK**:失败后不能 nack requeue(会立即回到队头无限循环),要把消息重新 publish 到 retry queue 后 ACK 掉原消息
2. **多级 queue 而非单一 queue**:因为**队列 TTL 是队头阻塞**的,不能靠"消息级 TTL"实现分级(队头 30 分钟消息挡住 30 秒的)
3. **retry count 靠 header 传递**:不能存在 broker 里,必须跟着消息走
4. **回到业务 Exchange 而非业务 Queue**:通过 DLX 的 routing key 让消息经业务 Exchange 重新路由,更符合 AMQP 抽象

### 7.3 方案 B:rabbitmq_delayed_message_exchange 插件(推荐)

#### 7.3.1 基本原理

官方插件,专门解决延迟问题。

```
声明特殊类型的 Exchange:x-delayed-message
消息发送时带 header "x-delay" = 30000(毫秒)
Exchange 内部延迟到期后才路由到 Queue
```

**优点**:
- 无队头阻塞
- 每条消息独立延迟
- 支持任意延迟时间

**缺点**:
- 需要安装插件(`rabbitmq-plugins enable rabbitmq_delayed_message_exchange`)
- 大量延迟消息会占用 Broker 内存
- 延迟时间上限约 2^32 毫秒(约 49 天)

#### 7.3.2 重试策略实战:动态延迟(延迟插件)

用延迟插件实现同样的分级重试,**代码简化很多**——不需要建多个 retry queue,每条消息独立指定延迟。

```
业务消息 ──▶ delay.exchange (type: x-delayed-message)
              │
              ├── delay=0     ──▶ order.queue(首次立即处理)
              │
              ├── delay=30s   ──▶ order.queue(第 1 次重试)
              │
              ├── delay=5min  ──▶ order.queue(第 2 次重试)
              │
              └── delay=30min ──▶ order.queue(第 3 次重试)
```

**PHP 实现**:

```php
<?php
use PhpAmqpLib\Wire\AMQPTable;

// ============ Step 1: 声明延迟 Exchange ============

// 类型是 x-delayed-message,通过 arguments 指定实际路由类型(direct/topic 等)
$exchangeArgs = new AMQPTable([
    'x-delayed-type' => 'direct',   // ★ 内部实际用 direct 路由
]);

$ch->exchange_declare(
    'delay.exchange',
    'x-delayed-message',   // ★ 特殊类型(必须安装插件)
    false,
    true,                  // durable
    false,
    false,
    false,
    $exchangeArgs
);

// 业务 Queue
$ch->queue_declare('order.queue', false, true, false, false);
$ch->queue_bind('order.queue', 'delay.exchange', 'order.process');

// 死信 Queue(超过最大重试)
$ch->exchange_declare('dlx.exchange', 'direct', false, true, false);
$ch->queue_declare('dlx.queue', false, true, false, false);
$ch->queue_bind('dlx.queue', 'dlx.exchange', 'order.dead');

// ============ Step 2: 首次发送(delay=0 立即消费) ============

function publishOrder(array $data, int $delayMs = 0, int $retryCount = 0): void {
    global $ch;
    
    $msg = new AMQPMessage(json_encode($data), [
        'delivery_mode' => AMQPMessage::DELIVERY_MODE_PERSISTENT,
        'application_headers' => new AMQPTable([
            'x-delay'         => $delayMs,       // ★ 关键:延迟毫秒数
            'x-retry-count'   => $retryCount,
        ]),
    ]);
    
    $ch->basic_publish($msg, 'delay.exchange', 'order.process');
}

// 首次发送(无延迟)
publishOrder(['order_id' => 12345]);

// ============ Step 3: Consumer(重试用同一个 Exchange) ============

// 重试延迟档位(毫秒)
const RETRY_DELAYS = [
    0 => 30 * 1000,          // 首次失败:30 秒后
    1 => 5 * 60 * 1000,      // 第 1 次重试失败:5 分钟后
    2 => 30 * 60 * 1000,     // 第 2 次重试失败:30 分钟后
];
const MAX_RETRY = 3;

$callback = function (AMQPMessage $msg) use ($ch) {
    $headers = $msg->has('application_headers')
        ? $msg->get('application_headers')->getNativeData()
        : [];
    $retryCount = $headers['x-retry-count'] ?? 0;

    try {
        processOrder(json_decode($msg->body, true));
        $msg->ack();

    } catch (Throwable $e) {
        if ($retryCount >= MAX_RETRY) {
            // 达到上限,进死信
            $deadMsg = new AMQPMessage($msg->body, [
                'delivery_mode' => AMQPMessage::DELIVERY_MODE_PERSISTENT,
                'application_headers' => new AMQPTable([
                    'x-retry-count'    => $retryCount,
                    'x-failure-reason' => $e->getMessage(),
                ]),
            ]);
            $ch->basic_publish($deadMsg, 'dlx.exchange', 'order.dead');
        } else {
            // 重新发到延迟 Exchange,delay 动态指定
            $delayMs = RETRY_DELAYS[$retryCount];
            $retryMsg = new AMQPMessage($msg->body, [
                'delivery_mode' => AMQPMessage::DELIVERY_MODE_PERSISTENT,
                'application_headers' => new AMQPTable([
                    'x-delay'       => $delayMs,        // ★ 动态延迟
                    'x-retry-count' => $retryCount + 1,
                ]),
            ]);
            $ch->basic_publish($retryMsg, 'delay.exchange', 'order.process');
        }
        $msg->ack();   // 原消息 ACK
    }
};

$ch->basic_qos(null, 10, null);
$ch->basic_consume('order.queue', '', false, false, false, false, $callback);

while ($ch->is_consuming()) {
    $ch->wait();
}
```

**Go 版本关键片段**:

```go
// 声明延迟 Exchange
ch.ExchangeDeclare(
    "delay.exchange",
    "x-delayed-message",
    true, false, false, false,
    amqp.Table{
        "x-delayed-type": "direct",   // ★ 内部路由类型
    },
)

// 重试延迟表
var retryDelays = []int32{
    30 * 1000,        // 首次失败后 30s
    5 * 60 * 1000,    // 第 2 次失败后 5m
    30 * 60 * 1000,   // 第 3 次失败后 30m
}

// 消费失败时重新发送
for msg := range msgs {
    retryCount, _ := msg.Headers["x-retry-count"].(int32)
    
    if err := processOrder(msg.Body); err != nil {
        if int(retryCount) >= maxRetry {
            // 死信
            ch.PublishWithContext(ctx, "dlx.exchange", "order.dead", false, false,
                amqp.Publishing{
                    DeliveryMode: amqp.Persistent,
                    Body:         msg.Body,
                    Headers: amqp.Table{
                        "x-retry-count":    retryCount,
                        "x-failure-reason": err.Error(),
                    },
                })
        } else {
            // 动态延迟重发
            ch.PublishWithContext(ctx, "delay.exchange", "order.process", false, false,
                amqp.Publishing{
                    DeliveryMode: amqp.Persistent,
                    Body:         msg.Body,
                    Headers: amqp.Table{
                        "x-delay":       retryDelays[retryCount],
                        "x-retry-count": retryCount + 1,
                    },
                })
        }
        msg.Ack(false)
        continue
    }
    msg.Ack(false)
}
```

**方案 B 相比方案 A 的优势**:
- **单一 Exchange**:不需要为每个延迟档位建 queue,新增延迟档位只需改 `RETRY_DELAYS` 常量
- **无队头阻塞**:延迟 30 分钟的消息不会挡住延迟 30 秒的
- **代码简洁**:重试次数变多时优势明显
- **灵活**:可以支持"指数退避"(1s → 2s → 4s → 8s → ...)等自定义策略

**指数退避示例**:

```php
// 指数退避:每次失败延迟翻倍,上限 30 分钟
function nextDelay(int $retryCount): int {
    $baseMs = 1000;                       // 1 秒基数
    $maxMs = 30 * 60 * 1000;              // 上限 30 分钟
    $delayMs = $baseMs * (2 ** $retryCount);
    // 加随机抖动,避免大量任务同时重试打死下游
    $jitter = random_int(0, (int)($delayMs * 0.2));
    return min($delayMs + $jitter, $maxMs);
}

// 重试次数 0 1 2 3 4 5 ... 对应延迟
// 1s 2s 4s 8s 16s 32s ... 上限 30 分钟
```

### 7.4 生产选型

- **消息量小、延迟精度要求不高** → TTL + DLX(方案 A)
- **精确延迟、消息量大、重试策略灵活** → 延迟插件(方案 B)
- **超大规模延迟消息、需要千万级** → **改用 RocketMQ 的原生延迟消息**(Kafka 也没有,得自己实现)
- **重试链路一定要**:
  - 记录 `x-retry-count`
  - 设置 `MAX_RETRY` 上限
  - 超限进死信
  - 加**抖动**避免同步重试打死下游
  - 监控重试率(某消息类型重试率飙升说明下游有问题)

---

## 八、Prefetch / QoS

### 8.1 问题：Consumer 被撑爆

默认情况下，Broker 会**一次性把所有消息推给 Consumer**：
```
Queue 有 100 万消息
    ↓ 一次性 push 全部
Consumer 内存爆炸
```

### 8.2 basic.qos（Prefetch Count）

限制 Consumer **未 ACK 的消息数**上限：

```
channel.basicQos(10)  -- 最多同时持有 10 条未 ACK 消息
```

工作方式：
```
Broker 推送 10 条 → Consumer 接收
Consumer ACK 1 条 → Broker 补推 1 条
Consumer ACK 1 条 → Broker 补推 1 条
```

**Prefetch 起到削峰限流的作用**。

### 8.3 如何设置合理值

- **prefetch = 1**：最严格，一次只处理一条。慢消费/顺序敏感
- **prefetch = 10~100**：默认推荐值，平衡吞吐和内存
- **prefetch = 1000+**：高吞吐场景，Consumer 内存要够

**经验公式**：`prefetch ≈ 平均消费耗时 / 网络 RTT × 并发数`

### 8.4 公平调度

默认策略是**轮询**，不考虑 Consumer 处理能力。快的 Consumer 空闲，慢的 Consumer 堆积。

**basic.qos + prefetch=1** 相当于开启公平调度：谁处理完谁拿新的。

---

## 九、集群与高可用

### 9.1 普通集群（默认）

多个节点组成集群，元数据（Exchange、Queue、Binding 定义）在所有节点同步，但**消息数据只存在 Queue 所在的节点**。

```
Node A: Queue Q1 (数据本体)
Node B: Q1 元数据引用 → 转发到 Node A
Node C: Q1 元数据引用 → 转发到 Node A
```

**问题**：Q1 所在 Node A 挂了，Q1 的消息**全部丢失**。

普通集群解决的是**吞吐扩展**，不是**高可用**。

### 9.2 镜像队列（Mirrored Queue）

Queue 的消息在多个节点复制，主节点挂了从节点顶上。

```
Node A: Q1 Master (读写)
Node B: Q1 Mirror (同步复制)
Node C: Q1 Mirror
```

**优点**：高可用
**缺点**：
- 性能下降（每次写要同步到所有 Mirror）
- 3.8+ 已被官方标记为**遗留**
- 数据一致性弱（异步复制，可能丢消息）

### 9.3 Quorum Queue（仲裁队列，推荐）

**RabbitMQ 3.8+ 引入**，基于 **Raft 协议**的强一致队列。取代镜像队列。

```
5 节点集群
Quorum Queue Q1: 3 副本（Leader + 2 Follower）
写入要多数派（3/2+1 = 2）确认
```

**优点**：
- **强一致**（Raft 协议）
- **数据不丢**（写入多数派才 ACK）
- 自动故障转移
- 官方推荐取代镜像队列

**缺点**：
- 内存消耗高（每条消息都要在多副本）
- 性能低于普通队列
- **不支持某些特性**（优先级、TTL 有限制）

### 9.4 Streams（3.9+，追加型队列）

类似 Kafka 的分区流。**只追加，不删除**，适合高吞吐场景。

- 单个 Stream 可存**TB 级**消息
- 消费者可**回放历史消息**
- 支持多消费者独立消费

### 9.5 生产选型

| 场景 | 选择 |
|---|---|
| 追求高吞吐，容忍少量丢失 | 普通队列 + 镜像 |
| 高可靠，不能丢消息 | **Quorum Queue** |
| 大数据量、需要回放 | Streams |
| 特殊需求（优先级、精确 TTL） | Classic + Mirror |

---

## 十、消息顺序性

### 10.1 顺序被破坏的场景

RabbitMQ **同一 Queue 内消息 FIFO 有序**，但以下情况会打破顺序：

- **多个 Consumer 并发消费同一 Queue** → 谁快谁先处理完
- **单 Consumer 但并发处理**（多线程）
- **Consumer nack requeue** → 消息重回队列位置改变

### 10.2 保证顺序的方案

**方案 A：单 Queue + 单 Consumer + prefetch=1**
```
最简单，但吞吐极低（串行）
```

**方案 B：按 key 分 Queue**
```
用户订单顺序保证：
    hash(user_id) % N → 分到 N 个 Queue
    每个 Queue 一个 Consumer
    同一用户的消息永远在同一 Queue → 保证顺序
```

这是 Kafka 的 partition 思路的模拟。RabbitMQ 需要自己实现。

**方案 C：业务层顺序号**
消息带 seq，Consumer 收到后按 seq 排序处理。乱序到达也能重排。

### 10.3 关键认知

**RabbitMQ 不擅长严格顺序场景**。需要严格顺序：
- 优先考虑 **RocketMQ 的顺序消息**（原生支持）
- 或 **Kafka 的分区**（同一 key 同一 partition）

---

## 十一、消息重复消费与幂等

### 11.1 消息为什么会重复？

- Consumer 处理完但 ACK 前崩了 → 消息重投
- 网络抖动导致 ACK 丢失 → Broker 认为没消费成功
- Publisher Confirm 超时重发

**at-least-once 语义是主流**：消息**至少投递一次**，可能重复。

**exactly-once 极难做到**，一般靠**消费端幂等**兜底。

### 11.2 幂等消费的实现

**方案 A：消息唯一 ID + Redis 去重**
```
Producer 给每条消息生成 message_id（UUID / 雪花）

Consumer 处理前：
    SETNX processed:msg_id 1 EX 86400
    if 已存在 → 直接 ACK 跳过
    if 首次 → 处理业务 → ACK
```

**方案 B：DB 唯一索引兜底**
```
消费时 INSERT 一条记录带 message_id UNIQUE
    冲突 → 已处理过，跳过
    成功 → 处理业务
```

**方案 C：业务状态机**
```
订单只能从 PENDING → PAID，重复消费"支付成功"消息时:
    UPDATE orders SET status='PAID' WHERE id=? AND status='PENDING'
    影响行数=0 → 已经处理过
```

**详见** `system-design/distributed/transactions.md` 幂等章节。

---

## 十二、消息堆积怎么处理

### 12.1 为什么会堆积

- **Producer 突发流量**（大促、秒杀）
- **Consumer 处理速度跟不上**（下游慢、bug）
- **Consumer 宕机**

### 12.2 处理方案

**方案 A：临时扩容 Consumer**
- 加机器
- 增加 Consumer 进程/线程
- **前提**：Queue 允许多 Consumer 并发（顺序敏感场景不行）

**方案 B：跳过某些消息**
- 非核心消息可以选择性丢弃
- 用死信队列归档，先恢复业务

**方案 C：先转存**
- 消费者不处理业务，只转存到 DB / Kafka
- 后续慢慢消费

**方案 D：预防措施**
- **Prefetch 合理设置**（防止 Consumer 内存爆）
- **Consumer 侧限流**（防止打死下游）
- **告警**：Queue 消息数超过阈值告警
- **削峰**：Producer 侧限流 / 分级消息

### 12.3 监控指标

- `messages_ready`：待消费的消息数
- `messages_unacknowledged`：已投递未 ACK 的
- `messages_persistent`：持久化消息数
- `consume_rate` / `publish_rate`：进出速率
- **消费速率 < 生产速率 → 会持续堆积**

---

## 十三、RabbitMQ vs Kafka vs RocketMQ

| 维度 | RabbitMQ | Kafka | RocketMQ |
|---|---|---|---|
| 语言 | Erlang | Scala/Java | Java |
| 协议 | AMQP、MQTT、STOMP | 自定义 | 自定义 + RemotingCommand |
| 吞吐 | 万级/秒 | **百万级/秒** | 十万级/秒 |
| 延迟 | 微秒级 | 毫秒级 | 毫秒级 |
| 可靠性 | 高 | 高 | 高 |
| 路由灵活性 | ⭐⭐⭐⭐⭐ | ⭐ | ⭐⭐ |
| 顺序消息 | 弱 | Partition 级 | ⭐ 原生支持 |
| 事务消息 | 弱 | 有 | ⭐ 原生支持 |
| 延迟消息 | 需插件 | 无 | ⭐ 原生支持 |
| 消息回溯 | 无 | ⭐ 长期存储 | 有 |
| 集群易用性 | 中 | 复杂 | 中 |
| 典型场景 | 企业应用、金融、复杂路由 | 大数据、日志、流处理 | 电商、订单、金融 |

### 选型建议

- **企业应用/复杂路由**（金融、订单、CRM）→ **RabbitMQ**
- **大数据/日志/流处理**（用户行为、监控指标）→ **Kafka**
- **电商/事务/延迟消息**（订单履约、支付）→ **RocketMQ**
- **多语言、需要 AMQP 兼容** → RabbitMQ
- **超高吞吐（>10w QPS）** → Kafka

---

## 十四、PHP / Go 生产代码示例

### 14.1 PHP（php-amqplib）

**Producer**：
```php
use PhpAmqpLib\Connection\AMQPStreamConnection;
use PhpAmqpLib\Message\AMQPMessage;

$conn = new AMQPStreamConnection('localhost', 5672, 'guest', 'guest');
$channel = $conn->channel();

// 声明持久化 Exchange 和 Queue
$channel->exchange_declare('order.exchange', 'direct', false, true, false);
$channel->queue_declare('order.queue', false, true, false, false);
$channel->queue_bind('order.queue', 'order.exchange', 'order.created');

// 开启 Publisher Confirm
$channel->confirm_select();
$channel->set_ack_handler(function ($msg) {
    echo "ACK: {$msg->getDeliveryTag()}\n";
});
$channel->set_nack_handler(function ($msg) {
    echo "NACK: {$msg->getDeliveryTag()}\n";
    // 落地本地消息表，定时重发
});

// 发送持久化消息
$body = json_encode(['order_id' => 12345]);
$msg = new AMQPMessage($body, [
    'delivery_mode' => AMQPMessage::DELIVERY_MODE_PERSISTENT,
    'message_id'    => uniqid('msg_', true),  // 幂等 ID
    'content_type'  => 'application/json',
]);
$channel->basic_publish($msg, 'order.exchange', 'order.created');
$channel->wait_for_pending_acks();

$channel->close();
$conn->close();
```

**Consumer**：
```php
$conn = new AMQPStreamConnection('localhost', 5672, 'guest', 'guest');
$channel = $conn->channel();

$channel->queue_declare('order.queue', false, true, false, false);
$channel->basic_qos(null, 10, null);  // prefetch=10

$callback = function (AMQPMessage $msg) {
    $data = json_decode($msg->body, true);
    $msgId = $msg->get('message_id');
    
    try {
        // 幂等检查
        if ($redis->setnx("processed:{$msgId}", 1) === false) {
            $msg->ack();  // 已处理过，直接 ACK
            return;
        }
        $redis->expire("processed:{$msgId}", 86400);
        
        // 业务处理
        processOrder($data);
        
        $msg->ack();
    } catch (Throwable $e) {
        // 失败：requeue 让别的 Consumer 试试，或进 DLX
        $msg->nack(false, false);  // requeue=false，走 DLX
    }
};

$channel->basic_consume('order.queue', '', false, false, false, false, $callback);

while ($channel->is_consuming()) {
    $channel->wait();
}
```

### 14.2 Go（amqp091-go）

```go
package main

import (
    "context"
    "encoding/json"
    "log"

    amqp "github.com/rabbitmq/amqp091-go"
)

// Producer
func Publish(order Order) error {
    conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
    if err != nil {
        return err
    }
    defer conn.Close()

    ch, err := conn.Channel()
    if err != nil {
        return err
    }
    defer ch.Close()

    // 声明
    ch.ExchangeDeclare("order.exchange", "direct", true, false, false, false, nil)
    ch.QueueDeclare("order.queue", true, false, false, false, nil)
    ch.QueueBind("order.queue", "order.created", "order.exchange", false, nil)

    // 开启 Confirm
    ch.Confirm(false)
    confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))

    body, _ := json.Marshal(order)
    err = ch.PublishWithContext(
        context.Background(),
        "order.exchange",
        "order.created",
        true,   // mandatory
        false,
        amqp.Publishing{
            ContentType:  "application/json",
            DeliveryMode: amqp.Persistent,
            MessageId:    order.ID,
            Body:         body,
        },
    )
    if err != nil {
        return err
    }

    // 等待 confirm
    confirm := <-confirms
    if !confirm.Ack {
        return errors.New("nack from broker")
    }
    return nil
}

// Consumer
func Consume(redis *redis.Client) {
    conn, _ := amqp.Dial("amqp://guest:guest@localhost:5672/")
    ch, _ := conn.Channel()
    ch.Qos(10, 0, false)

    msgs, _ := ch.Consume("order.queue", "", false, false, false, false, nil)

    for msg := range msgs {
        // 幂等检查
        ok, _ := redis.SetNX(ctx, "processed:"+msg.MessageId, 1, 24*time.Hour).Result()
        if !ok {
            msg.Ack(false)
            continue
        }

        var order Order
        if err := json.Unmarshal(msg.Body, &order); err != nil {
            msg.Nack(false, false)   // 格式错误，进 DLX
            continue
        }

        if err := processOrder(order); err != nil {
            log.Printf("process failed: %v", err)
            msg.Nack(false, false)   // 失败进 DLX
            continue
        }

        msg.Ack(false)
    }
}
```

---

## 十五、常见面试题速览

### Q1: RabbitMQ 如何保证消息不丢？
**三重保障**：Publisher Confirm、Exchange/Queue/Message 全持久化、Consumer 手动 ACK。补充：镜像队列或 Quorum Queue 做集群冗余。

### Q2: RabbitMQ 如何保证消息不重复消费？
Broker 无法保证 exactly-once（网络无法 100% 可靠）。靠 **Consumer 端幂等**：唯一 ID + Redis 去重 / DB 唯一索引 / 业务状态机。

### Q3: RabbitMQ 如何保证消息顺序？
- 单 Queue + 单 Consumer + prefetch=1（最保守）
- 按业务 key 分 Queue（用户维度）
- 严格顺序场景**建议改用 RocketMQ**。

### Q4: RabbitMQ 有哪几种 Exchange？各自场景？
Direct（精确路由）、Fanout（广播）、Topic（通配符）、Headers（键值匹配，少用）。

### Q5: 什么是死信队列？用途？
消息被拒、超时、超长时变死信，通过 DLX 路由到死信队列。用途：失败兜底、延迟队列（TTL+DLX）、消息归档。

### Q6: 如何实现延迟队列？
两种：**TTL + DLX**（有队头阻塞）、**rabbitmq_delayed_message_exchange 插件**（精确延迟，推荐）。

### Q7: Channel 和 Connection 的区别？
Connection 是 TCP 连接，成本高；Channel 是建立在 Connection 上的虚拟通道，轻量。**同一 Channel 不能跨线程/协程共享**。

### Q8: Prefetch 是什么？为什么要设置？
限制 Consumer **未 ACK 消息数**上限，防止内存打爆、实现公平调度。推荐 10~100。

### Q9: 镜像队列和仲裁队列的区别？
- **镜像队列**：主从异步复制，可能丢消息，官方已标记遗留
- **Quorum Queue**：Raft 协议强一致，官方推荐取代镜像队列

### Q10: 消息堆积怎么办？
- 短期：扩容 Consumer、跳过非核心消息、转存到 DB 慢慢处理
- 长期：Prefetch 调优、下游限流、分级队列、告警

### Q11: RabbitMQ 为什么比 Kafka 慢？
- Kafka 顺序写磁盘 + 零拷贝 + PageCache，架构就是为高吞吐设计
- RabbitMQ 是通用消息中间件，路由灵活性和可靠性优先，吞吐量不是首要目标

### Q12: RabbitMQ 集群节点挂了会丢消息吗？
- 普通队列：Queue 所在节点挂了消息丢失
- 镜像队列：主挂了从顶上，异步复制可能丢一小部分
- Quorum Queue：只要多数派存活，不丢消息

### Q13: Publisher Confirm 和事务模式的区别？
- **事务**：同步阻塞，性能极差（下降百倍）
- **Confirm**：异步回调，性能高，是生产推荐方案

### Q14: 什么是 vhost，为什么要用？
虚拟主机，逻辑隔离单位。不同业务用不同 vhost，Exchange/Queue/权限完全独立。类似 MySQL 的 database。

### Q15: RabbitMQ 单节点性能瓶颈在哪？
- Erlang 进程调度
- 磁盘 IO（持久化 fsync）
- 内存（Queue 消息缓存）
- 网络带宽（大消息）
- 生产峰值 QPS 约 1~5w，超过要考虑集群/分片。

---

## 关键记忆点

1. **AMQP 三层模型**：Exchange → Binding → Queue
2. **四种 Exchange**：Direct（精确）/ Fanout（广播）/ Topic（通配符）/ Headers（少用）
3. **可靠性三件套**：Publisher Confirm + 三处持久化 + 手动 ACK
4. **DLX 三个触发条件**：拒绝、TTL、超长
5. **延迟队列两种实现**：TTL+DLX（有队头阻塞）/ 延迟插件（推荐）
6. **Channel 非线程安全**：每个线程/协程独立 Channel
7. **Prefetch 是流控核心**：防内存爆 + 公平调度
8. **Quorum Queue > 镜像队列**：Raft 强一致
9. **RabbitMQ 顺序性弱**：需要严格顺序用 RocketMQ / Kafka
10. **消息重复必然存在**：靠消费者幂等兜底

**一句话**：**RabbitMQ 胜在路由灵活和企业级可靠性，输在吞吐量。选型看业务本质**。

---

## 参考资料

- [RabbitMQ 官方文档](https://www.rabbitmq.com/documentation.html)
- [AMQP 0-9-1 协议规范](https://www.rabbitmq.com/amqp-0-9-1-reference.html)
- [Quorum Queues 官方指南](https://www.rabbitmq.com/quorum-queues.html)
- [RabbitMQ 实战指南](https://www.oreilly.com/library/view/rabbitmq-in-action/9781935182979/)
