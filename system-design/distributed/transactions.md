# 微服务分布式事务 · 深度面试题

**标签**: #distributed #transaction #微服务 #系统设计 #高频
**难度**: ⭐⭐⭐⭐⭐
**相关阅读**: [distributed-lock](../../databases/redis/distributed-lock.md) · [distributed/README](./README.md)

> 本文覆盖分布式事务从理论到落地的全部核心考点。每个方案都会拆到**协议流程 → 生产陷阱 → 选型判据**三个层面，尽量把面试官会追问的地方一次讲透。

---

## 目录

1. [题面与考察点](#一题面与考察点)
2. [理论基础：ACID / CAP / BASE](#二理论基础acid--cap--base)
3. [2PC / 3PC / XA](#三2pc--3pc--xa)
4. [TCC（Try-Confirm-Cancel）](#四tcctry-confirm-cancel)
5. [Saga 模式](#五saga-模式)
6. [本地消息表（可靠消息最终一致）](#六本地消息表可靠消息最终一致)
7. [事务消息（RocketMQ）](#七事务消息rocketmq)
8. [最大努力通知](#八最大努力通知)
9. [Seata 四种模式深度剖析](#九seata-四种模式深度剖析)
10. [三大共性难题：幂等 / 空回滚 / 悬挂](#十三大共性难题幂等--空回滚--悬挂)
11. [方案选型矩阵](#十一方案选型矩阵)
12. [实战案例：电商下单链路](#十二实战案例电商下单链路)
13. [高频追问 Q&A](#十三高频追问-qa)
14. [生产事故复盘](#十四生产事故复盘)

---

## 一、题面与考察点

> **面试题**：在微服务架构体系中，常用的保障多个事务的情况一般如何处理？

这道题看似基础，实际是**系统设计面试的分水岭**。面试官通常会顺着回答向下深挖：

- 追问 1：你说的 TCC 和 Saga 区别是什么？什么场景选哪个？
- 追问 2：本地消息表和事务消息有什么本质区别？
- 追问 3：TCC 的空回滚、悬挂、幂等具体怎么实现？
- 追问 4：Seata AT 模式原理讲一下，为什么它可以做到"无侵入"？
- 追问 5：如果补偿也失败了怎么办？
- 追问 6：跨库、跨服务、跨语言时你会怎么设计？

考察点：**理论深度 + 工程落地经验 + 对一致性/可用性/性能的 tradeoff 判断力**。

---

## 二、理论基础：ACID / CAP / BASE

### 2.1 ACID vs BASE

| 维度 | ACID（单机/传统） | BASE（分布式/微服务） |
|---|---|---|
| Atomicity | 事务原子性，全成功或全失败 | Basically Available，允许部分不可用 |
| Consistency | 强一致，任何时刻数据一致 | Soft state，允许中间不一致状态 |
| Isolation | 事务间隔离 | Eventually consistent，最终一致 |
| Durability | 持久化 | — |

分布式事务的核心哲学转变：**从"强一致"退让到"最终一致"，换取可用性与性能**。这是 BASE 理论的本质。

### 2.2 CAP 三选二

- **C**（Consistency）：所有节点在同一时刻读到相同数据
- **A**（Availability）：每个请求都能得到响应（可能是旧数据）
- **P**（Partition tolerance）：网络分区仍能对外服务

分布式系统 **P 必选**（网络分区无法避免），实际是在 **CP** 和 **AP** 之间选。微服务典型选择 AP + 最终一致性。

### 2.3 分布式事务的本质难点

跨越以下任一边界，就无法直接用单机事务：

1. **跨库**：同一服务内的多个数据源（订单库 + 账户库）
2. **跨服务**：订单服务调用库存服务、支付服务
3. **跨技术栈**：MySQL + Redis + MQ + ES 都要保持一致
4. **跨地域/云**：多活、多云部署

难点核心是**部分成功**：A 执行完，B 执行失败，A 的结果无法自动回滚。

---

## 三、2PC / 3PC / XA

### 3.1 2PC 协议流程

两阶段提交由**事务协调者（TM/Coordinator）**统一控制**资源管理器（RM/Participant）**。

```text
Phase 1  Prepare（投票阶段）
  Coordinator ──"can commit?"──▶ RM1, RM2, RM3
  RM 执行事务但不提交，写 undo/redo log，锁定资源
  RM ──"YES / NO"──▶ Coordinator

Phase 2  Commit / Rollback（决策阶段）
  Coordinator 若收到全部 YES → 发 COMMIT
  Coordinator 若任一 NO 或超时 → 发 ROLLBACK
  RM 执行 commit/rollback 并释放资源
  RM ──"ACK"──▶ Coordinator
```

### 3.2 2PC 的三大致命缺陷

**缺陷 1：同步阻塞**
- Prepare 阶段所有 RM 必须锁定资源直到收到最终决议
- 高并发下锁等待时间线性增长，吞吐量急剧下降

**缺陷 2：协调者单点**
- Coordinator 在 Phase 2 发送 Commit 前宕机 → 参与者永远等待
- 部分参与者收到 Commit、部分没收到 → 数据不一致

**缺陷 3：脑裂 / 数据不一致**
- Phase 2 Coordinator 只成功通知了部分 RM 就宕机
- 恢复后 RM 状态不一致，需人工介入

### 3.3 3PC 的改进与仍然存在的问题

3PC 在 2PC 前多加一个 **CanCommit** 阶段，把 Prepare 拆成 CanCommit + PreCommit：

```text
CanCommit  → 只询问，不锁资源
PreCommit  → 参与者锁资源、记 log
DoCommit   → 最终提交
```

引入超时机制：参与者在 PreCommit 后若长时间收不到 DoCommit，会**自行 Commit**（假设大概率会提交）。

**3PC 的问题**：网络分区下自行 Commit 假设不成立，仍可能不一致。工业界几乎不用 3PC，多数系统直接跳到 TCC/Saga。

### 3.4 XA 协议

XA 是 X/Open DTP 组织定义的分布式事务标准接口，基于 2PC。MySQL、Oracle、PostgreSQL 都实现了 XA 接口：

```sql
XA START 'xid';
-- 业务 SQL
XA END 'xid';
XA PREPARE 'xid';
-- ...其他分支同样操作
XA COMMIT 'xid';   -- 或 XA ROLLBACK 'xid'
```

**微服务中为什么不用 XA**：
- 性能差，锁资源时间长
- MySQL XA 实现有 bug 历史（8.0 之前 XA 恢复问题）
- 不支持跨异构存储（Redis、MQ）
- 无法解决协调者单点

**唯一场景**：银行核心系统、传统 ERP，对强一致要求高于可用性的领域。

---

## 四、TCC（Try-Confirm-Cancel）

### 4.1 TCC 本质

TCC 是**业务层面的 2PC**，把每个业务操作拆成三个方法：

- **Try**：预留业务资源（业务检查 + 冻结资源）
- **Confirm**：真正执行业务（使用被冻结的资源）
- **Cancel**：释放冻结资源（放弃执行）

### 4.2 TCC 流程

```text
业务流程                              账户服务              库存服务
──────────────────────────────────────────────────────────────
1. 全局事务开始（TM 生成 xid）
2. 分支 1：账户扣款
   Try  → 冻结余额 100 元                ✓ frozen=100
3. 分支 2：库存扣减
   Try  → 冻结库存 1 件                                    ✓ frozen=1
4. 全部 Try 成功
5. Confirm 阶段（并行）
   账户：真正扣余额（frozen → deducted）  ✓
   库存：真正扣库存（frozen → deducted）                    ✓
6. 全局事务提交

若步骤 3 失败：
5'. Cancel 阶段
    账户：解冻余额                        ✓ frozen=0
    库存：无 Try 记录 → 空回滚忽略
```

### 4.3 TCC 的业务改造

以扣款为例，传统写法：

```sql
UPDATE account SET balance = balance - 100 WHERE user_id = 1;
```

TCC 改造后需要三张表操作（一般用一张表加冻结字段）：

```sql
-- Try: 冻结
UPDATE account
SET balance = balance - 100,
    frozen_amount = frozen_amount + 100
WHERE user_id = 1 AND balance >= 100;

-- Confirm: 释放冻结（真正扣走）
UPDATE account
SET frozen_amount = frozen_amount - 100
WHERE user_id = 1;

-- Cancel: 归还余额
UPDATE account
SET balance = balance + 100,
    frozen_amount = frozen_amount - 100
WHERE user_id = 1;
```

### 4.4 TCC 优缺点

**优点**：
- 性能好，无长期锁（对比 XA）
- 一致性强（业务层保证）
- 灵活可控，能针对业务优化

**缺点**：
- **侵入性极强**：每个操作都要写三个方法，改造成本高
- **业务承担幂等/空回滚/悬挂**：见 [第十节](#十三大共性难题幂等--空回滚--悬挂)
- 冻结字段增加设计复杂度
- 补偿逻辑需自行编写

### 4.5 TCC 适用场景

- **强一致要求**：支付、扣款、库存等核心链路
- **业务可以自然拆成"预留-确认-释放"**：账务、票务、券包
- **不适合**：只读操作、无法预留资源的操作（发短信、调三方 API）

### 4.6 主流 TCC 框架

| 框架 | 特点 |
|---|---|
| **Seata TCC** | 阿里系主流，与 AT/Saga/XA 统一 |
| **ByteTCC** | 基于 JTA 规范扩展 |
| **Hmily** | Dromara 出品，高性能异步补偿 |
| **DTM (Go)** | Go 语言首选，跨语言支持 |

---

## 五、Saga 模式

### 5.1 Saga 起源与本质

Saga 概念来自 1987 年 Hector Garcia-Molina 论文《Sagas》，本意是解决**长事务**问题。将一个长事务拆成 N 个本地短事务 T1, T2, ..., Tn，每个 Ti 有对应的补偿 Ci。

```text
正向：T1 → T2 → T3 → T4 → T5   ✓ 全部成功即提交
反向：T1 → T2 → T3 → T4 → T5(失败)
      C1 ← C2 ← C3 ← C4          逆向补偿
```

### 5.2 两种实现方式

#### 5.2.1 编排式 Choreography（协同式）

各服务通过**事件驱动**，无中心协调者：

```text
订单服务 ── OrderCreated ──▶ 库存服务 ── StockDeducted ──▶ 支付服务
                                    │
                                    ▼ StockDeductFailed
                              订单服务（自行处理补偿）
```

- **优点**：去中心化、松耦合、无单点
- **缺点**：流程分散在各服务、可观测性差、复杂业务难以追踪

#### 5.2.2 编排式 Orchestration（中央协调）

中央协调器（Saga Coordinator）驱动流程：

```text
       ┌─────── Saga Coordinator ───────┐
       │                                │
       ▼               ▼                ▼
     订单服务       库存服务          支付服务
```

- **优点**：流程集中、可视化、易运维
- **缺点**：协调器可能成为瓶颈、有中心化风险

### 5.3 Saga vs TCC 的核心区别

| 维度 | TCC | Saga |
|---|---|---|
| 阶段 | Try / Confirm / Cancel（三阶段） | 正向 / 补偿（两阶段） |
| 资源锁定 | Try 冻结资源 | 无冻结，直接执行 |
| 隔离性 | 相对好（冻结资源不可见） | **无隔离性**（中间状态可见） |
| 补偿方式 | 释放冻结 | 反向操作（如退款） |
| 适用场景 | 短事务、强一致 | 长事务、可容忍中间态 |
| 侵入性 | 高（三个方法） | 中（两个方法） |

### 5.4 Saga 的隔离性陷阱

Saga **不保证隔离性**——事务中间状态对外可见。示例：

```text
T1 扣款 100  → 用户余额显示已扣
T2 库存扣减失败
C1 补偿：退款
```

如果 T1 完成到 C1 之间用户查询余额，看到的是**中间态**。这在业务上可能引发用户投诉、羊毛党套利等问题。

**解决方案**：
- **Semantic Lock**：业务层加"状态标记"（如订单状态=`PENDING`），中间态对用户屏蔽
- **Commutative Updates**：让操作可交换、可重复
- **Pessimistic View**：查询接口过滤中间态

### 5.5 Saga 的补偿失败问题

如果补偿 Ci 也失败呢？

**核心原则**：补偿必须**永不失败**（best-effort），实现方式：

1. **无限重试**：补偿设计为幂等，失败后无限重试直到成功
2. **人工介入**：补偿失败进死信队列，运维手动处理
3. **业务保底**：设计"最终态"允许补偿不完全成功后由对账修正

---

## 六、本地消息表（可靠消息最终一致）

### 6.1 核心思想

利用**本地事务的原子性**保证业务写入和消息写入的一致性。

```text
业务 DB
├── order 表（业务表）
└── message 表（消息表）  ← 关键：与业务表在同一个数据库

写入时用本地事务同时写两张表，消息写入即代表业务确认。
后台任务扫描消息表 → 投递到 MQ → 下游消费 → ACK 后标记完成
```

### 6.2 完整流程

```text
┌─── 生产端 ───────────────────────────────────────────────┐
│                                                          │
│  BEGIN                                                   │
│    INSERT INTO order VALUES (...)                        │
│    INSERT INTO message VALUES (id, payload, 'PENDING')   │
│  COMMIT                                                  │
│         │                                                │
│         ▼                                                │
│  定时任务 / binlog 监听                                  │
│    扫描 status='PENDING' 的消息                          │
│    发送到 MQ                                             │
│    发送成功 → UPDATE status='SENT'                       │
└──────────────────────────────────────────────────────────┘
                     │
                     ▼
┌─── 消费端 ───────────────────────────────────────────────┐
│  从 MQ 消费                                              │
│  BEGIN                                                   │
│    执行业务逻辑（幂等）                                  │
│    INSERT INTO consumed_message VALUES (msg_id)          │
│  COMMIT                                                  │
│  ACK 给 MQ                                               │
└──────────────────────────────────────────────────────────┘
```

### 6.3 关键细节

**消息表设计**：

```sql
CREATE TABLE local_message (
  id            BIGINT PRIMARY KEY,
  biz_id        VARCHAR(64) NOT NULL,     -- 业务唯一 ID
  topic         VARCHAR(64) NOT NULL,
  payload       TEXT NOT NULL,
  status        TINYINT NOT NULL,          -- 0=PENDING, 1=SENT, 2=DONE
  retry_count   INT DEFAULT 0,
  next_retry_at DATETIME,
  created_at    DATETIME,
  updated_at    DATETIME,
  INDEX idx_status_retry (status, next_retry_at)
);
```

**重试策略**：指数退避（1s → 2s → 4s → ...），达到最大次数进死信。

**消息表清理**：`status=DONE` 的消息定期归档删除，避免表膨胀。

### 6.4 优缺点

**优点**：
- 实现简单，不依赖复杂框架
- 消息一定发出，业务和消息强一致
- 与本地事务完全绑定，可靠性高

**缺点**：
- **业务库压力增加**：多一张表，扫描 IO 开销
- **延迟较高**：扫描间隔 → 秒级延迟
- **需要清理机制**：否则消息表无限膨胀
- 消费端仍需保证幂等

### 6.5 变种：基于 Binlog

用 Canal / Debezium 订阅业务表的 binlog，业务方无需自己写消息表：

```text
业务写库 → binlog → Canal → MQ → 下游消费
```

- **优点**：完全无侵入
- **缺点**：schema 变更需协同、消息内容是行变化不是业务事件

---

## 七、事务消息（RocketMQ）

### 7.1 半消息（Half Message）机制

RocketMQ 独创的事务消息，通过**半消息**实现消息发送与本地事务的原子性。

```text
Step 1  Producer 发送半消息到 Broker
        Broker 存储，但对 Consumer 不可见（"半"消息）
        Broker 返回 SendResult

Step 2  Producer 执行本地事务
        成功 → 向 Broker 发 COMMIT
        失败 → 向 Broker 发 ROLLBACK

Step 3  Broker 收到 COMMIT → 消息对 Consumer 可见
        Broker 收到 ROLLBACK → 删除半消息

Step 4  若 Step 2 后 Producer 宕机
        Broker 定时反查：调用 Producer 的
        checkLocalTransaction() 接口确认状态
```

### 7.2 事务回查（Transaction Check）

Broker 主动询问 Producer 本地事务状态：

```go
// 伪代码
func (p *Producer) CheckLocalTransaction(msg Message) LocalTransactionState {
    // 根据 msg 中的 bizId 查询本地事务表
    tx := db.Query("SELECT status FROM tx_log WHERE biz_id = ?", msg.BizID)
    switch tx.Status {
    case "COMMITTED":  return COMMIT
    case "ROLLBACK":   return ROLLBACK
    default:           return UNKNOWN  // 稍后再问
    }
}
```

回查间隔配置：默认 60s 一次，最多 15 次，超过后标记为 rollback。

### 7.3 与本地消息表对比

| 维度 | 本地消息表 | RocketMQ 事务消息 |
|---|---|---|
| 消息存储 | 业务 DB 自建 | RocketMQ Broker |
| 一致性 | 强（本地事务保证） | 强（半消息 + 回查） |
| 实现复杂度 | 中（自己写扫描 + 重试） | 低（框架支持） |
| 延迟 | 秒级（扫描间隔） | 毫秒级 |
| 依赖 | 无 | 强绑定 RocketMQ |
| 反查接口 | 无需 | 需实现 checkLocalTransaction |

### 7.4 事务消息的坑

- **Kafka 不支持事务消息**（Kafka 的 transaction 是 exactly-once 语义，含义不同）
- 事务消息**顺序不保证**：Commit 顺序不等于消费顺序
- 反查接口必须**幂等**且**快速**（不能查外部依赖导致超时）
- **业务字段唯一 ID**必须写入消息，否则反查无法定位

---

## 八、最大努力通知

### 8.1 使用场景

对一致性要求**较弱**的场景，只保证"通知到"，不保证"处理成功"：

- 支付结果通知给业务方
- 账单同步给下游 BI
- 短信/邮件推送

### 8.2 实现要点

```text
1. 主事务完成后异步通知下游
2. 失败按指数退避重试：1min → 5min → 30min → 2h → 1天
3. 达到最大重试次数后标记为"最终失败"
4. 提供【主动查询接口】给下游对账
5. 定期跑对账任务发现遗漏
```

### 8.3 关键设计

- **回调地址可配置**：下游 URL 由业务方注册
- **签名机制**：防止伪造通知（HMAC + 时间戳）
- **通知 ID**：下游用来去重
- **状态查询 API**：下游可主动拉取最新状态

---

## 九、Seata 四种模式深度剖析

Seata 是分布式事务的主流开源框架，支持四种模式：**AT / TCC / Saga / XA**。前三种是重点。

### 9.1 三大角色

- **TC（Transaction Coordinator）**：事务协调者，独立部署的 Seata Server
- **TM（Transaction Manager）**：事务管理者，通常是发起全局事务的服务
- **RM（Resource Manager）**：资源管理者，操作分支事务的服务

```text
        ┌────── TC (Seata Server) ──────┐
        │                                │
    注册全局事务                    注册分支事务
        ▲                                ▲
        │                                │
       TM ────── 调用 ──────▶ RM (Service A)
                       └────▶ RM (Service B)
                       └────▶ RM (Service C)
```

### 9.2 Seata AT 模式（Automatic Transaction）—— 灵魂所在

AT 模式是 Seata 的招牌，**对业务完全无侵入**，用起来像本地事务：

```java
@GlobalTransactional
public void createOrder() {
    orderService.create();       // RM A
    stockService.deduct();       // RM B
    accountService.debit();      // RM C
}
```

#### 9.2.1 AT 模式的工作原理

**Phase 1（业务执行）**：

```text
1. 拦截业务 SQL（比如 UPDATE product SET stock = stock - 1 WHERE id = 1）
2. 解析 SQL，查询【前镜像】：
   SELECT * FROM product WHERE id = 1  → before_image
3. 执行业务 SQL
4. 查询【后镜像】：
   SELECT * FROM product WHERE id = 1  → after_image
5. 将 before/after image 写入本地 undo_log 表（业务库同一事务内）
6. 提交本地事务 + 释放本地锁
7. 上报分支状态到 TC
```

**Phase 2**：

- **提交**：异步删除 undo_log（快，秒级完成）
- **回滚**：读取 undo_log 中的 before_image，生成反向 SQL 执行

#### 9.2.2 全局锁

AT 模式的关键设计：**Phase 1 结束就释放本地锁，但保留全局锁**（存于 TC）。

```text
时刻 T1: 事务 A 修改 product.id=1，Phase 1 完成，本地锁释放，全局锁保留
时刻 T2: 事务 B 想修改 product.id=1
        → 本地锁能拿到（A 已释放）
        → 但需要向 TC 申请全局锁
        → 若 A 未完成 Phase 2 → B 等待或超时
```

**优点**：本地锁快速释放，提升并发；仍能保证全局隔离。
**代价**：TC 需存储全局锁信息，读多写多的表可能成为热点。

#### 9.2.3 AT 模式的隔离级别

- **写隔离**：全局锁保证同时只有一个全局事务在写同一行
- **读隔离**：默认是"读未提交"（因为 Phase 1 本地已提交，其他事务能读到中间态）
- **提升到读已提交**：加 `@GlobalLock` + `SELECT FOR UPDATE`，强制通过 TC 校验全局锁

#### 9.2.4 AT 模式的适用限制

- **必须有主键**：undo_log 依赖主键生成反向 SQL
- **仅支持关系型数据库**：MySQL、Oracle、PG、TiDB
- **对 DDL 不支持**：只支持 DML
- **中间态可见**：Phase 1 提交后其他事务能读到未 commit 的全局事务数据

### 9.3 Seata TCC 模式

需要业务方实现 Try/Confirm/Cancel 三个方法，Seata 只负责协调。TC 记录每个分支状态，根据全局决议触发 Confirm/Cancel。

### 9.4 Seata Saga 模式

基于**状态机引擎**，用 JSON 定义流程：

```json
{
  "StartState": "ReduceInventory",
  "States": {
    "ReduceInventory": {
      "Type": "ServiceTask",
      "ServiceName": "stockService",
      "ServiceMethod": "deduct",
      "CompensateState": "CompensateReduceInventory",
      "Next": "DebitAccount"
    },
    ...
  }
}
```

适合流程长、有条件分支、需要可视化编排的业务。

### 9.5 Seata XA 模式

基于数据库 XA 接口，仍然是传统 2PC，仅在必须强一致 + 有 XA 支持的 DB 时用。

---

## 十、三大共性难题：幂等 / 空回滚 / 悬挂

无论 TCC 还是 Saga，都会遇到这三个问题。这是**面试必问**的深度知识。

### 10.1 幂等

**问题**：网络抖动、TC 重试导致同一操作被调用多次。

```text
Confirm 被调用两次 → 扣款扣了两次 ❌
```

**解决方案**：

1. **业务唯一 ID**：每个全局事务分配 xid + branchId，作为幂等 Key
2. **事务状态表**：记录每个 xid+branchId 的状态

```sql
CREATE TABLE tx_action_log (
  xid          VARCHAR(64),
  branch_id    BIGINT,
  action       ENUM('TRY','CONFIRM','CANCEL'),
  status       ENUM('DONE','FAILED'),
  created_at   DATETIME,
  PRIMARY KEY (xid, branch_id, action)
);
```

Confirm/Cancel 执行前先查此表，已 DONE 则直接返回成功。

### 10.2 空回滚

**问题**：Try 因网络问题没到达（或 Try 失败但 TC 未收到响应），TC 判定全局失败，向该分支发 Cancel。此时 Cancel **找不到对应的 Try 记录**。

```text
Try 请求丢失          ×
Cancel 请求到达      → 冻结记录不存在，怎么办？
```

**错误处理**：如果 Cancel 直接执行"归还余额"，实际上没冻结过 → 凭空多出 100 元 ❌

**正确处理**：Cancel 前查 `tx_action_log`，若 Try 未执行过 → **记录 Cancel 状态但不执行业务逻辑**（空回滚）。

### 10.3 悬挂

**问题**：Cancel 先于 Try 到达（网络乱序、Try 超时后 TC 先发 Cancel，随后 Try 才姗姗来迟）。

```text
Cancel 到达 → 空回滚记录已写入
Try 到达   → 冻结资源
     ↑ 此时资源被冻结但永远不会有 Confirm/Cancel 释放 → 悬挂！
```

**解决方案**：Try 执行前查 `tx_action_log`，若发现同 xid+branchId 已有 Cancel 记录 → **拒绝执行 Try**。

### 10.4 综合防御模板

```java
// Try 方法
public boolean tryDeduct(BusinessContext ctx, ...) {
    // 1. 悬挂检查：是否已有 Cancel 记录
    if (txLog.exists(ctx.getXid(), ctx.getBranchId(), CANCEL)) {
        return false;  // 拒绝执行
    }
    // 2. 幂等检查：是否已 Try 过
    if (txLog.exists(ctx.getXid(), ctx.getBranchId(), TRY)) {
        return true;   // 已执行，返回成功
    }
    // 3. 业务：冻结资源
    doFreeze(...);
    // 4. 记录 Try 状态
    txLog.insert(ctx.getXid(), ctx.getBranchId(), TRY, DONE);
    return true;
}

// Cancel 方法
public boolean cancelDeduct(BusinessContext ctx, ...) {
    // 1. 幂等检查：是否已 Cancel 过
    if (txLog.exists(ctx.getXid(), ctx.getBranchId(), CANCEL)) {
        return true;   // 已执行
    }
    // 2. 空回滚检查：Try 是否执行过
    if (!txLog.exists(ctx.getXid(), ctx.getBranchId(), TRY)) {
        // 空回滚：只记录 Cancel，不执行业务
        txLog.insert(ctx.getXid(), ctx.getBranchId(), CANCEL, DONE);
        return true;
    }
    // 3. 业务：解冻资源
    doUnfreeze(...);
    // 4. 记录 Cancel 状态
    txLog.insert(ctx.getXid(), ctx.getBranchId(), CANCEL, DONE);
    return true;
}
```

**重点**：所有检查和业务操作必须在**同一本地事务**中完成，否则依旧有并发问题。

---

## 十一、方案选型矩阵

### 11.1 综合对比

| 方案 | 一致性 | 性能 | 复杂度 | 侵入性 | 隔离性 | 适用场景 |
|---|---|---|---|---|---|---|
| 2PC/XA | 强 | 低 | 低 | 低 | 强 | 传统金融、DB 间 |
| TCC | 强 | 高 | 高 | 高 | 中（冻结） | 支付、库存核心 |
| Saga | 最终 | 高 | 中 | 中 | 无 | 长业务流程 |
| 本地消息表 | 最终 | 中 | 低 | 中 | 无 | 中小规模异步 |
| 事务消息 | 最终 | 高 | 中 | 低 | 无 | 电商解耦 |
| Seata AT | 最终 | 高 | 低 | **无** | 弱 | 内部微服务 |
| 最大努力通知 | 弱 | 高 | 低 | 低 | 无 | 非核心通知 |

### 11.2 决策树

```text
是否需要跨异构存储 / 跨语言？
├─ 是 → 消息类方案（本地消息表 / 事务消息）
└─ 否
   └─ 是否需要强一致？
      ├─ 是
      │  └─ 是否愿意改造代码？
      │     ├─ 愿意 → TCC
      │     └─ 不愿意 → Seata AT（数据库支持前提下）
      └─ 否
         └─ 业务是否长流程？
            ├─ 是 → Saga
            └─ 否 → 最大努力通知
```

### 11.3 常见组合

生产环境很少只用一种：

- **核心链路**：TCC（保证一致性）
- **异步扩展**：事务消息 / 本地消息表（通知积分、日志、推荐）
- **非核心**：最大努力通知（通知第三方 BI、短信）
- **长流程编排**：Saga（订单履约、跨境物流）

---

## 十二、实战案例：电商下单链路

以经典的电商下单为例，看不同方案如何落地。

### 12.1 业务场景

```text
用户下单 → 需要同时：
1. 订单服务：创建订单
2. 库存服务：扣减库存
3. 账户服务：扣减余额（或冻结优惠券）
4. 积分服务：预占积分抵扣
5. 通知服务：发短信 / push
```

### 12.2 方案设计

**核心链路（1-4）**：TCC

```text
Try 阶段：
  订单：创建订单（status=PENDING）
  库存：冻结库存
  账户：冻结余额
  积分：冻结积分

Confirm 阶段：
  订单：改为 PAID
  库存：真扣（frozen → deducted）
  账户：真扣
  积分：真扣

Cancel 阶段（任一 Try 失败）：
  订单：改为 CANCELLED
  库存：解冻
  账户：解冻
  积分：解冻
```

**非核心（5）**：事务消息

```text
订单 Confirm 成功后发事务消息 → 通知服务消费 → 发送短信
即使短信失败也不影响主流程
```

**积分累积**：本地消息表

```text
订单完成后写入本地消息 → 异步累积积分
```

### 12.3 常见陷阱

- **冻结字段命名冲突**：账户/积分/库存的冻结字段命名要统一（如 `frozen_amount`）
- **超时时间层级**：TC 全局事务超时 > 分支超时 > 业务 RPC 超时
- **补偿风暴**：某分支持续失败 → 触发大量补偿 → 拖垮系统。要做**熔断**
- **对账兜底**：无论多严密，都要有 T+1 对账任务扫异常事务

---

## 十三、高频追问 Q&A

### Q1：TCC 和 Saga 到底怎么选？

看**业务能不能自然拆出"预留-确认-释放"**：

- 能拆（如账户余额、库存）→ TCC，一致性更强
- 拆不出（如发短信、退款、调三方 API 无法预留）→ Saga，用补偿代替
- 流程超过 5 步、有条件分支 → Saga，可视化编排更易维护

### Q2：Seata AT 为什么能做到"无侵入"？

关键在于 **SQL 拦截 + undo_log 存储 before/after image**：
- 业务代码不需要写补偿
- 拦截器自动记录变更前后镜像
- 回滚时反向生成 SQL 执行
- 前提是必须能拿到主键的 SQL

### Q3：分布式事务里最坑的是什么？

- **补偿失败**：一定要幂等 + 无限重试 + 人工兜底
- **中间态可见**：Saga 和 AT 都要在业务层加状态屏蔽
- **消息重复**：所有消费方必须幂等
- **网络乱序**：产生空回滚 / 悬挂

### Q4：如何监控分布式事务？

- **TraceId 全链路**：SkyWalking / Jaeger 追踪每个全局事务
- **中间态监控**：告警长时间处于 PENDING 的事务
- **对账系统**：T+1 扫描"上下游数据不一致"的记录
- **补偿成功率**：核心指标，低于阈值报警
- **死信队列**：兜底所有失败补偿

### Q5：Kafka 能做事务消息吗？

Kafka 的 "transaction" 是**生产端事务**（跨 partition 的 exactly-once），**不是 RocketMQ 那种业务事务消息**。Kafka 不支持事务回查，也不支持半消息。用 Kafka 做分布式事务需要走本地消息表模式。

### Q6：分布式事务性能瓶颈在哪？

- **TC 单点**：Seata TC 高并发下需要集群化 + 分片
- **全局锁竞争**：AT 模式热点行的全局锁排队
- **网络 RTT**：TCC 每个分支多 2 次 RPC（Try + Confirm）
- **消息堆积**：MQ 侧消费能力不足

### Q7：跨语言微服务（Go + Java + Python）怎么办？

- 选 **DTM** 或**基于消息的方案**（事务消息 / 本地消息表）
- Seata 主要 Java 生态，其他语言支持不完善
- 契约层用**统一 gRPC/REST 接口** + **标准化的 xid 传递**

### Q8：如何测试分布式事务？

- **单元测试**：mock TC，验证 Try/Confirm/Cancel 逻辑
- **集成测试**：真起 Seata Server，模拟分支失败
- **混沌工程**：注入网络延迟、节点宕机、消息重复
- **性能测试**：全局锁热点、TC 吞吐

### Q9：小项目一定要上分布式事务框架吗？

**不一定**。评估维度：

- 业务是否真的跨服务？→ 单体够用就单体
- 一致性容忍度？→ 秒级最终一致 → 本地消息表就够
- 团队维护能力？→ Seata 部署运维成本不低

优先级：**避免分布式事务 > 消息最终一致 > TCC/Saga > XA**。能不用就不用。

### Q10：微服务拆分导致的分布式事务，能否通过拆分调整避免？

**能。核心原则：把强一致业务放同一个服务/同一个库**：

- 账户 + 账务流水 → 同一服务同一库
- 订单 + 订单明细 → 同一服务同一库
- 只在真正需要跨服务的地方引入分布式事务

**架构层面减少分布式事务比技术层面解决更有效**。

---

## 十四、生产事故复盘

### 案例 1：TCC 悬挂导致资金冻结

**现象**：某支付系统凌晨大量用户余额被"莫名冻结"，无法解冻。

**根因**：
- 网络抖动导致 Try 请求 60s 后才到达账户服务
- 此期间 TC 已判超时并发送 Cancel（Cancel 空回滚成功）
- 之后 Try 到达，正常冻结了余额
- 但此全局事务已被 TC 判 Rollback，永远不会再有 Cancel

**修复**：所有 Try 方法开头必须检查是否已有 Cancel 记录（悬挂检查）。

### 案例 2：Saga 中间态被恶意利用

**现象**：某电商发放优惠券后立即扣减库存，用户在扣减库存失败前抢购下单，Saga 补偿了优惠券但订单已生成。

**根因**：Saga 无隔离性，中间态订单可见。

**修复**：订单增加 `PROCESSING` 状态，只有 Saga 全部成功才转 `CONFIRMED`。前端和查询接口过滤 `PROCESSING`。

### 案例 3：本地消息表撑爆磁盘

**现象**：消息表膨胀到千万级，业务库磁盘告警。

**根因**：只写不删，消息表无归档策略。

**修复**：
- 完成状态的消息 7 天后归档到历史库
- 关键索引优化：`(status, next_retry_at)` 组合索引
- 分表按月分（避免单表过大）

### 案例 4：全局锁热点行拖垮 TC

**现象**：Seata AT 模式下某热门商品下单超时率飙升。

**根因**：热门商品的库存行成为全局锁热点，所有并发请求排队等锁。

**修复**：
- 热点商品切换为 TCC（预冻结库存分散到多行）
- 库存"分桶"：单个商品的库存拆成 N 个库存单元
- 加强本地缓存 + 限流

---

## 参考资料

- [Seata 官方文档](https://seata.io/)
- [RocketMQ 事务消息](https://rocketmq.apache.org/docs/featureBehavior/04transactionmessage)
- [DTM 分布式事务框架](https://en.dtm.pub/)
- Martin Kleppmann《Designing Data-Intensive Applications》Ch.9
- Hector Garcia-Molina《Sagas》1987

---

**核心记忆点**：

1. **理论**：BASE / CAP / 最终一致性是根本哲学
2. **方案七种**：XA / TCC / Saga / 本地消息表 / 事务消息 / 最大努力通知 / Seata AT
3. **共性三难**：幂等、空回滚、悬挂
4. **选型口诀**：能不做尽量不做，非做不可先消息，强一致必选 TCC，长流程用 Saga，无侵入选 AT
5. **兜底思维**：对账 + 死信 + 告警是保命三件套
