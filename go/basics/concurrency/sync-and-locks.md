# Go 锁与并发问题详解

**标签**: #go #concurrency #sync #mutex #高频

---

## 一、为什么需要锁

多个 goroutine 同时读写同一份数据 → **数据竞争（Data Race）** → 结果不可预测。

```go
type Counter struct {
    count int
}

func (c *Counter) Inc() {
    c.count++  // ❌ 非原子操作：读 → +1 → 写，三步之间可能被其他 goroutine 打断
}

func main() {
    c := &Counter{}
    var wg sync.WaitGroup
    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() { defer wg.Done(); c.Inc() }()
    }
    wg.Wait()
    fmt.Println(c.count)  // 期望 1000，实际可能 967、954... 每次不同
}
```

**检测工具**：`go run -race main.go` 会明确报告 DATA RACE。

---

## 二、sync.Mutex（互斥锁）

同一时刻只允许**一个** goroutine 访问被保护的数据。

### 基础用法

```go
type Counter struct {
    mu    sync.Mutex
    count int
}

func (c *Counter) Inc() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.count++
}

func (c *Counter) Get() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.count  // ⚠️ 读也要加锁，否则可能读到"半写状态"
}

func main() {
    c := &Counter{}
    var wg sync.WaitGroup
    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() { defer wg.Done(); c.Inc() }()
    }
    wg.Wait()
    fmt.Println(c.Get())  // ✅ 稳定输出 1000
}
```

### 关键规则

| 规则 | 说明 |
|-----|------|
| mutex 和数据放一起 | 通常作为 struct 第一个字段，明确保护范围 |
| 用方法封装访问 | 外部拿不到内部字段，只能通过加锁方法访问 |
| 读也要加锁 | 否则可能读到"读写撕裂"，尤其是 64 位数据在 32 位平台 |
| `defer Unlock()` | panic 时也能释放锁，防止死锁 |
| 别复制 mutex | 复制后两个副本互不影响，`go vet` 会报错 |
| 别重复 Lock | 同一 goroutine 重入 Lock 会死锁（Mutex **不是**可重入锁） |

### Mutex 底层：正常模式 vs 饥饿模式

```
正常模式：新来的 goroutine 直接和唤醒的老 goroutine 竞争锁（提升性能）
         但可能让老 goroutine 长时间抢不到 → 饥饿

饥饿模式：等待超过 1ms 触发，锁直接传给队列头部的 goroutine（保证公平）
         切换代价：新来的 goroutine 直接进队尾

Go runtime 自动在两种模式间切换。
```

---

## 三、sync.RWMutex（读写锁）

读操作不互斥，写操作独占。**适合读远多于写的场景**（配置、缓存）。

```go
type Cache struct {
    mu   sync.RWMutex
    data map[string]string
}

func NewCache() *Cache {
    return &Cache{data: make(map[string]string)}
}

// 读操作：RLock，多个 goroutine 可以同时持有
func (c *Cache) Get(key string) (string, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    v, ok := c.data[key]
    return v, ok
}

// 写操作：Lock，独占
func (c *Cache) Set(key, val string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.data[key] = val
}

func (c *Cache) Delete(key string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    delete(c.data, key)
}

func main() {
    cache := NewCache()
    var wg sync.WaitGroup

    // 5 个写 goroutine
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("val%d", i))
        }(i)
    }

    // 100 个读 goroutine（并发读不互斥）
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            cache.Get(fmt.Sprintf("key%d", i%5))
        }(i)
    }
    wg.Wait()
}
```

### RWMutex 规则

| 操作 | 与其他 RLock | 与其他 Lock |
|------|-------------|------------|
| RLock | ✅ 可并发 | ❌ 互斥 |
| Lock | ❌ 互斥 | ❌ 互斥 |

**避免写饥饿**：Go 的 RWMutex 有 goroutine 等 `Lock` 时，新的 `RLock` 会被阻塞（不让读一直插队）。

### 什么时候用 RWMutex

- 读远多于写（读:写 > 10:1）
- 读操作耗时较长
- 写操作不频繁

**读写比例接近时用 Mutex 反而更好** —— RWMutex 内部维护读者计数，开销比 Mutex 大。

---

## 四、sync.WaitGroup（等待一组 goroutine 完成）

```go
var wg sync.WaitGroup

for i := 0; i < 10; i++ {
    wg.Add(1)              // 计数 +1
    go func(i int) {
        defer wg.Done()    // 计数 -1
        process(i)
    }(i)
}
wg.Wait()  // 阻塞直到计数归零
```

### 常见陷阱

```go
// ❌ 陷阱 1：在 goroutine 内 Add
for i := 0; i < 10; i++ {
    go func() {
        wg.Add(1)  // ⚠️ 可能主线程已经 Wait() 了
        defer wg.Done()
    }()
}
wg.Wait()  // 可能立即返回（counter 还是 0）

// ✅ 正确：在 goroutine 外 Add
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
    }()
}

// ❌ 陷阱 2：值传递 WaitGroup（会导致 counter 不共享）
func run(wg sync.WaitGroup) { ... }  // ❌ 复制了 wg
func run(wg *sync.WaitGroup) { ... } // ✅ 传指针
```

---

## 五、sync.Once（只执行一次）

用于单例、懒加载等只需初始化一次的场景。

```go
var (
    once     sync.Once
    instance *Config
)

func GetConfig() *Config {
    once.Do(func() {
        instance = loadConfig()  // 只会执行一次，即使多个 goroutine 同时调用
    })
    return instance
}
```

**特点**：
- 并发安全，多个 goroutine 同时 `Do` 也只执行一次
- 如果函数 panic，`Once` 仍然算"已执行"，下次不再调用
- 比 `if instance == nil { ... }` + mutex 更清晰

---

## 六、sync.Cond（条件变量）

goroutine 等待某个条件成立再继续。**用得不多**，通常用 channel 替代更清晰。

```go
type Queue struct {
    mu   sync.Mutex
    cond *sync.Cond
    data []int
}

func NewQueue() *Queue {
    q := &Queue{}
    q.cond = sync.NewCond(&q.mu)
    return q
}

func (q *Queue) Push(v int) {
    q.mu.Lock()
    defer q.mu.Unlock()
    q.data = append(q.data, v)
    q.cond.Signal()  // 唤醒一个等待者
}

func (q *Queue) Pop() int {
    q.mu.Lock()
    defer q.mu.Unlock()
    for len(q.data) == 0 {
        q.cond.Wait()  // 释放锁 + 阻塞；被唤醒后重新获取锁
    }
    v := q.data[0]
    q.data = q.data[1:]
    return v
}
```

**规则**：
- `Wait()` 必须在锁内调用
- 用 `for` 循环判断条件（防止虚假唤醒）
- `Signal()` 唤醒一个，`Broadcast()` 唤醒全部

---

## 七、sync.Pool（对象池，减少 GC）

复用临时对象，减少内存分配和 GC 压力。

```go
var bufPool = sync.Pool{
    New: func() any {
        return new(bytes.Buffer)  // 池空时创建新对象
    },
}

func handleRequest(data []byte) {
    buf := bufPool.Get().(*bytes.Buffer)
    defer func() {
        buf.Reset()          // 使用前重置状态
        bufPool.Put(buf)     // 归还到池
    }()

    buf.Write(data)
    // 使用 buf...
}
```

**注意**：
- Pool 里的对象可能**随时被 GC 回收**（不适合做连接池等需要保持长生命周期的资源）
- 适合频繁分配、可复用的**临时**对象（bytes.Buffer、[]byte 缓冲区）
- 每个 P 有独立的 pool，减少锁竞争

---

## 八、sync.Map（并发安全的 map）

参考 `basics/slices-maps.md` 中的详细讨论。**读多写少**场景性能好，**写多**场景比 `Mutex + map` 差。

```go
var m sync.Map

m.Store("k1", 1)
v, ok := m.Load("k1")            // 读
m.LoadOrStore("k2", 2)           // 不存在则写入（原子）
m.Delete("k1")
m.Range(func(k, v any) bool {
    return true
})
```

**局限**：
- 无 `Len()` 方法
- 值类型是 `any`，需要类型断言
- 写多时性能不如 `Mutex + map`

---

## 九、sync/atomic（原子操作）

无锁的原子操作，比 Mutex 更轻量。适合**单个变量**的读写。

```go
import "sync/atomic"

var counter int64

// 原子加
atomic.AddInt64(&counter, 1)

// 原子读
v := atomic.LoadInt64(&counter)

// 原子写
atomic.StoreInt64(&counter, 100)

// CAS（Compare and Swap）
swapped := atomic.CompareAndSwapInt64(&counter, 100, 200)
// 如果 counter == 100 就设置成 200，返回 true

// Go 1.19+ 类型化原子（推荐）
var c atomic.Int64
c.Add(1)
v := c.Load()
c.Store(100)
c.CompareAndSwap(100, 200)
```

### atomic vs Mutex

| 场景 | 推荐 |
|-----|-----|
| 单个变量的自增/赋值 | atomic |
| 多个变量的一起更新 | Mutex |
| 复杂业务逻辑保护 | Mutex |
| 高频计数器（QPS 统计） | atomic |

---

## 十、多锁场景：银行转账（避免死锁）

保护多个对象的一致性，需要**按固定顺序加锁**。

```go
type Account struct {
    id      int
    mu      sync.Mutex
    balance int
}

func Transfer(from, to *Account, amount int) error {
    // 按 ID 排序加锁，防止 A→B 和 B→A 同时发生时死锁
    first, second := from, to
    if from.id > to.id {
        first, second = to, from
    }

    first.mu.Lock()
    defer first.mu.Unlock()
    second.mu.Lock()
    defer second.mu.Unlock()

    if from.balance < amount {
        return errors.New("insufficient balance")
    }
    from.balance -= amount
    to.balance += amount
    return nil
}
```

**为什么按顺序加锁**：

```
❌ 交叉加锁 → 死锁
线程1: 转账 A→B   Lock A → 等 Lock B
线程2: 转账 B→A   Lock B → 等 Lock A
两个线程互相等待，永远走不出去

✅ 按 ID 排序 → 都先锁 min(A, B)，再锁 max(A, B)
线程1: Lock A → Lock B ✓
线程2: 也先 Lock A → 排队 → Lock B ✓
```

---

## 十一、常见并发问题

### 问题 1：数据竞争（Data Race）

多个 goroutine 同时读写同一变量，至少一个是写 → 数据竞争。

**检测**：`go run -race main.go` 或 `go test -race`

**根治**：mutex、atomic、channel（三选一）

### 问题 2：死锁（Deadlock）

多个 goroutine 互相等待对方持有的锁 → 永远无法推进。

**避免**：
- 多锁按固定顺序加
- 用 `TryLock`（Go 1.18+）尝试加锁，避免无限等待
- 用 `context` 加超时

### 问题 3：活锁（Livelock）

goroutine 一直在运行，但没有实质进展（互相谦让 → 谁都没干）。

```go
// 简化示例：两个 goroutine 都不断尝试加锁但都失败后立即重试
for {
    if a.TryLock() {
        if b.TryLock() {
            // 做事
            return
        }
        a.Unlock()  // 让出，重试
    }
}
```

**避免**：加随机退避（backoff）。

### 问题 4：goroutine 泄漏

见 `goroutine.md`。goroutine 启动后永久阻塞，无法终止。

**避免**：
- channel 加缓冲或加超时
- 用 context 控制生命周期
- for + select 至少有一个逃生分支

### 问题 5：循环变量捕获（Go 1.22 之前）

```go
// ❌ Go 1.21 及之前
for i := 0; i < 3; i++ {
    go func() {
        fmt.Println(i)  // 可能都打印 3
    }()
}

// ✅ 修复：值拷贝或新变量
for i := 0; i < 3; i++ {
    i := i  // Go 1.22+ 不需要这行
    go func() { fmt.Println(i) }()
}
```

### 问题 6：读写撕裂（Torn Read/Write）

32 位平台上，读写 64 位变量不是原子的，中间可能被打断。

```go
var x int64  // 64 位

// ❌ 32 位平台上直接读写可能读到"半新半旧"值
x = 12345678
v := x

// ✅ 用 atomic
atomic.StoreInt64(&x, 12345678)
v := atomic.LoadInt64(&x)
```

---

## 十二、选型总结

| 场景 | 首选 |
|-----|-----|
| 单变量原子自增 | `sync/atomic` |
| 少量共享状态，读写均衡 | `sync.Mutex` |
| 读多写少的共享状态 | `sync.RWMutex` |
| 只初始化一次 | `sync.Once` |
| 等待一组 goroutine 完成 | `sync.WaitGroup` |
| 等待某个条件成立 | `sync.Cond`（或用 channel） |
| 频繁分配的临时对象 | `sync.Pool` |
| key 固定、读多写极少的 map | `sync.Map` |
| 高并发读写 map | 分片 map（见 `basics/slices-maps.md`） |
| 生产者消费者 / pipeline | `channel` |
| 需要限流的并发 | `errgroup.SetLimit` 或 semaphore channel |

**心法**：
- 数据流用 channel，状态共享用 mutex（Rob Pike 原话）
- 能用 atomic 就不要用 mutex
- 能用 mutex 就不要用 RWMutex（除非确实读多写少）
- 能用 channel 就不要用 Cond

---

## 十三、面试高频题

### Q1: Mutex 和 RWMutex 的区别？

Mutex 完全互斥，同一时刻只有一个 goroutine 能访问。RWMutex 允许多个读并发，写独占。读远多于写时用 RWMutex，否则用 Mutex（RWMutex 内部开销更大）。

### Q2: sync.Mutex 是可重入的吗？

不是。同一 goroutine 二次 Lock 会死锁。Go 有意不提供可重入锁，鼓励清晰的锁边界。

### Q3: sync.Map 为什么不通用？

内部结构（read + dirty + misses）为"读多写极少"设计。写多时 dirty 频繁提升 read，性能反而不如 `Mutex + map`。

### Q4: atomic 和 Mutex 该怎么选？

单变量原子操作用 atomic，多变量或复杂逻辑用 Mutex。atomic 无锁开销更小，但表达能力有限。

### Q5: 死锁怎么排查？

- `go run -race` 检测数据竞争
- pprof 看 goroutine 堆栈，找互相等待的 goroutine
- 生产环境用 `runtime.Stack()` dump 所有 goroutine 状态

### Q6: 为什么不推荐值传递 Mutex？

Mutex 内部有状态字段（state、sema）。值传递会创建独立副本，两个副本状态不同步 → 保护无效。`go vet` 会报错。

### Q7: sync.WaitGroup 内部原理？

内部维护一个计数器（state）+ 信号量（sema）。`Add` 加计数，`Done` 减计数，`Wait` 阻塞直到计数归零（通过信号量唤醒）。

### Q8: sync.Pool 会不会泄漏？

不会。每次 GC 时会清空 Pool。所以 Pool 里的对象**不能假设一定存在**，Get 时永远走 `New` fallback。
