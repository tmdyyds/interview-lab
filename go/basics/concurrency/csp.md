# CSP 模型（Communicating Sequential Processes）

**标签**: #go #concurrency #csp #理论基础

---

## 一、CSP 是什么

**CSP（Communicating Sequential Processes）** 是 1978 年 **Tony Hoare** 提出的并发理论模型，主张：

> **"进程之间通过通信来同步，而不是通过共享内存 + 锁"**

Go 是继承 CSP 思想最彻底的现代语言。它把 CSP 中的抽象概念落地为：

| CSP 概念 | Go 实现 |
|---------|--------|
| Process（进程） | goroutine |
| Channel（通信管道） | channel |
| Synchronization（同步） | 无缓冲 channel 的握手语义 |
| Choice（选择） | select 语句 |

---

## 二、共享内存模型 vs CSP 模型

### 传统共享内存（Java / C++ 主流方式）

```
┌──────────┐        ┌──────────┐
│ Thread A │        │ Thread B │
└────┬─────┘        └────┬─────┘
     │                   │
     └──────┬────────────┘
            ↓
     ┌───────────────┐
     │  共享变量      │  ← 需要加锁保护（mutex）
     │  data = ...   │
     └───────────────┘
```

问题：
- 锁忘加 → 数据竞争
- 锁范围过大 → 性能差
- 多把锁交叉 → 死锁
- 心智负担重，难调试

### CSP 模型（Go 的方式）

```
┌──────────┐         ┌──────────┐
│ Routine A│──ch────▶│ Routine B│
└──────────┘         └──────────┘
       通过 channel 传数据
       数据在同一时刻只有一个所有者
```

优点：
- 数据"所有权"通过 channel 转移，天然避免竞争
- 代码结构清晰（生产者/消费者/pipeline）
- 不需要显式加锁

---

## 三、Go 官方名言

> **"Don't communicate by sharing memory; share memory by communicating."**
> —— Rob Pike

**翻译**：不要通过共享内存来通信，要通过通信来共享内存。

**理解**：把"数据"看作一个物品，channel 就是快递。多个 goroutine 之间传递数据时，不要让大家都能碰到那份数据（共享内存 + 锁），而是让快递员把它从一个人手里送到另一个人手里（channel 通信）。

---

## 四、CSP 的核心思想：所有权转移

```go
// ✅ CSP 风格：所有权通过 channel 转移
func producer(ch chan<- *Order) {
    o := &Order{ID: 1, Amount: 100}
    ch <- o
    // 这里之后，producer 不再访问 o —— 所有权已经交出去
}

func consumer(ch <-chan *Order) {
    o := <-ch
    o.Status = "paid"  // 拿到 o 的独占访问权，随便改
}

// ❌ 共享内存风格
var order *Order
var mu sync.Mutex

func producer() {
    mu.Lock()
    order = &Order{ID: 1, Amount: 100}
    mu.Unlock()
}

func consumer() {
    mu.Lock()
    order.Status = "paid"
    mu.Unlock()
}
```

**关键**：CSP 风格里，`o` 在任何时刻都只被一个 goroutine 持有，不需要锁。

---

## 五、Go 并没有完全否定共享内存

CSP 只是"推荐"模式，不是"唯一"模式。Go 也提供了 `sync` 包用于共享内存场景：

```
使用建议（Rob Pike 原话）:
  sync   → for state    （保护状态）
  channels → for communication （传递数据流）
```

### 什么时候用 channel

- 传递**数据**（订单、任务、事件）
- 生产者消费者模型
- 流水线（pipeline）
- 扇入扇出（fan-in / fan-out）
- 取消/超时信号

### 什么时候用 mutex

- 保护共享的**状态变量**（计数器、缓存、连接池）
- 数据被频繁读写，不适合复制传递（大 struct、map、集合）
- 需要读多写少（sync.RWMutex）
- 单机内的高性能计数（sync/atomic）

**举例**：Redis 客户端连接池 —— 用 mutex 保护 pool 更合理，硬用 channel 反而复杂。

---

## 六、CSP 在 Go 中的典型体现

### 体现 1：无缓冲 channel 的握手同步

```go
done := make(chan struct{})

go func() {
    doWork()
    done <- struct{}{}  // 完成信号
}()

<-done  // 等到 goroutine 完成
```

发送方和接收方**必须同时到达**才能通信 —— 这就是 CSP 里的 rendezvous（会合）。

### 体现 2：select —— 从多个通信选择一个

CSP 理论中的 external choice（外部选择）：

```go
select {
case v := <-ch1:
    handle1(v)
case v := <-ch2:
    handle2(v)
case ch3 <- data:
    // sent
}
```

多个 case 就绪时随机选一个，避免"总选择第一个"造成的饥饿。

### 体现 3：pipeline 组合

```go
// 每个阶段是独立的"process"，用 channel 串起来
nums := gen(1, 2, 3, 4, 5)
squared := sq(nums)
doubled := dbl(squared)

for v := range doubled {
    fmt.Println(v)
}
```

CSP 中的"顺序进程组合"思想。

---

## 七、CSP 与 Actor 模型的对比

CSP 和 Actor（Erlang/Akka 使用）都是消息通信模型，但有关键区别：

| 维度 | CSP（Go） | Actor（Erlang） |
|-----|----------|----------------|
| 通信对象 | 通过 channel（第三方管道） | 直接给 actor 发消息 |
| 寻址 | 不用管对方是谁，只关心 channel | 必须知道对方 actor ID |
| 同步性 | 无缓冲 channel 是同步的 | 通常异步（消息进邮箱） |
| 耦合度 | goroutine 不知道对方存在 | actor 直接引用其他 actor |
| 关系 | 生产者 → 管道 → 消费者 | actor ↔ actor |

**比喻**：
- CSP：往邮筒里投信，谁来收取无所谓
- Actor：直接寄给某个人，需要地址

---

## 八、CSP 的实践建议

### 建议 1：让 channel 传所有权，不是共享

```go
// ✅ 好：数据完整传递，接收方独占
func process(data <-chan *LargeStruct) {
    for d := range data {
        d.Field = "modified"  // 独占访问，不加锁
    }
}

// ❌ 差：channel 里传共享指针，两边都改
```

### 建议 2：channel 只在初始化时创建，close 由发送方负责

```go
func setup() (<-chan int, func()) {
    ch := make(chan int)
    ctx, cancel := context.WithCancel(context.Background())

    go func() {
        defer close(ch)  // 发送方 close
        for {
            select {
            case <-ctx.Done():
                return
            case ch <- rand.Int():
            }
        }
    }()

    return ch, cancel
}
```

### 建议 3：不要过度设计

小任务不要为了"用 channel"而用 channel。简单的共享计数器用 `sync/atomic` 就够了。

```go
// ❌ 过度设计
type Counter struct {
    ch chan int
    count int
}
func (c *Counter) Inc() { c.ch <- 1 }  // 每次 +1 都通过 channel

// ✅ 简单直接
type Counter struct {
    n int64
}
func (c *Counter) Inc() { atomic.AddInt64(&c.n, 1) }
```

---

## 九、思想小结

CSP 给 Go 并发编程带来的**核心思维**：

1. **进程独立** — 每个 goroutine 只做一件事，通过 channel 与外界交流
2. **数据流动** — 数据像水流一样在管道中传递，而不是被多方争抢
3. **组合优于共享** — 通过 channel 组合小 goroutine，构建复杂并发流程
4. **少即是多** — 用简单原语（channel + select）表达复杂并发逻辑

理解 CSP，不是记住术语，而是理解**"通信 = 同步 = 所有权转移"**这个统一的模型。

---

## 十、面试高频题

### Q1: 什么是 CSP？Go 和 CSP 什么关系？

CSP 是 Tony Hoare 1978 年提出的并发理论，主张进程通过通信而非共享内存来同步。Go 借鉴了 CSP 思想，用 goroutine 对应"进程"，用 channel 对应"通信管道"，让并发编程更安全、更清晰。

### Q2: Go 为什么用 CSP 模型？

传统共享内存 + 锁的模型心智负担大、易出错（数据竞争、死锁）。CSP 通过 channel 转移数据所有权，天然避免竞争，让开发者更容易写正确的并发代码。

### Q3: "Don't communicate by sharing memory" 怎么理解？

不要让多个 goroutine 共享同一块内存并用锁保护，而应该把数据放进 channel 传递，让数据在同一时刻只被一个 goroutine 持有（所有权转移）。

### Q4: CSP 和 Actor 模型有什么区别？

CSP：通过 channel（第三方管道）通信，通信双方不需要知道对方；
Actor：actor 之间直接发消息，需要知道对方的 ID；
Erlang 是 Actor 代表，Go 是 CSP 代表。

### Q5: Go 里什么时候用 channel，什么时候用 mutex？

传数据用 channel（生产消费、pipeline、事件流），保护状态用 mutex（计数器、缓存、连接池）。Rob Pike：`sync for state, channels for communication`。
