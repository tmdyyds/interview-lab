# Channel 详解

**标签**: #go #concurrency #channel #hchan #高频

---

## 一、Channel 是什么

Channel 是 goroutine 之间**通信 + 同步**的管道，Go 官方倡导：

> **"不要通过共享内存来通信，而要通过通信来共享内存"**

```go
ch := make(chan int)      // 无缓冲 channel
ch := make(chan int, 3)   // 缓冲区大小为 3

ch <- 42       // 发送
v := <-ch      // 接收
v, ok := <-ch  // 接收（ok 为 false 表示 channel 已关闭且无剩余数据）
close(ch)      // 关闭
```

---

## 二、底层结构 hchan

```go
// runtime/chan.go
type hchan struct {
    qcount   uint           // 当前缓冲区中元素数
    dataqsiz uint           // 缓冲区容量
    buf      unsafe.Pointer // 指向环形缓冲区
    elemsize uint16         // 元素大小
    closed   uint32         // 是否已关闭
    elemtype *_type         // 元素类型
    sendx    uint           // 发送位置索引
    recvx    uint           // 接收位置索引
    recvq    waitq          // 等待接收的 goroutine 队列
    sendq    waitq          // 等待发送的 goroutine 队列
    lock     mutex          // 互斥锁（channel 内部同步用）
}
```

**结构示意：**

```
hchan {
  buf: [_][_][_][_][_]       ← 环形缓冲区（容量 5）
        ↑     ↑
       recvx sendx           ← 读写位置

  sendq: G1 → G2 → G3        ← 等发送的 goroutine 队列（缓冲满时）
  recvq: G4 → G5             ← 等接收的 goroutine 队列（缓冲空时）
}
```

**关键理解**：channel 内部有锁，所有操作都是**串行**的（不是无锁）。

---

## 三、无缓冲 channel（同步 channel）

**发送和接收必须同时准备好**，否则阻塞。

```go
ch := make(chan int)  // 容量 0

// 场景一：发送方先到 → 阻塞等接收方
go func() {
    ch <- 1   // 阻塞在这里
    fmt.Println("sent")
}()
time.Sleep(time.Second)
v := <-ch     // 接收方到达，双方"握手"，发送方解除阻塞
// 输出：sent

// 场景二：接收方先到 → 阻塞等发送方
go func() {
    time.Sleep(time.Second)
    ch <- 1
}()
v := <-ch  // 阻塞等发送
```

**用途：同步信号**（"我做完了" 通知）

```go
done := make(chan struct{})

go func() {
    doWork()
    close(done)  // 通知完成
}()

<-done  // 等待 goroutine 结束
```

---

## 四、有缓冲 channel

**缓冲区没满就能发送，没空就能接收**。

```go
ch := make(chan int, 3)  // 容量 3

ch <- 1   // ✅ 缓冲区: [1]
ch <- 2   // ✅ 缓冲区: [1, 2]
ch <- 3   // ✅ 缓冲区: [1, 2, 3]
ch <- 4   // ❌ 阻塞（缓冲区满）

v := <-ch // ✅ 缓冲区: [2, 3]，之前阻塞的发送解除
```

**用途：解耦生产者和消费者**

```go
tasks := make(chan Task, 100)

// 生产者
go func() {
    for _, t := range taskList {
        tasks <- t  // 缓冲区未满时不阻塞
    }
    close(tasks)
}()

// 消费者
for t := range tasks {
    process(t)
}
```

---

## 五、Channel 的操作规则（面试重点）

| 操作 | nil channel | 空的 channel | 满的 channel | 已关闭 channel |
|-----|------------|-------------|-------------|---------------|
| 发送 `ch <- x` | **永久阻塞** | 阻塞或成功 | **阻塞** | **panic** |
| 接收 `<-ch` | **永久阻塞** | 阻塞 | 成功 | 返回零值 |
| close | **panic** | 成功 | 成功 | **panic** |

### 详细说明

```go
// 1. nil channel：发送和接收都永久阻塞
var ch chan int   // nil
// ch <- 1        // 永久阻塞
// <-ch           // 永久阻塞
// close(ch)      // ❌ panic: close of nil channel

// 2. 关闭后发送 → panic
ch := make(chan int, 3)
close(ch)
// ch <- 1  // ❌ panic: send on closed channel

// 3. 关闭后接收 → 返回零值，ok=false
ch := make(chan int, 3)
ch <- 1
close(ch)
v, ok := <-ch   // v=1, ok=true（缓冲区还有数据）
v, ok = <-ch    // v=0, ok=false（缓冲区空了，channel 已关闭）

// 4. 重复关闭 → panic
ch := make(chan int)
close(ch)
// close(ch)  // ❌ panic: close of closed channel
```

---

## 六、range 遍历 channel

```go
ch := make(chan int, 3)
ch <- 1
ch <- 2
ch <- 3
close(ch)  // ⚠️ 必须 close，否则 range 永远阻塞

for v := range ch {
    fmt.Println(v)  // 1, 2, 3
}
// 循环自动在 channel 关闭且缓冲区读空时退出
```

**关键**：`range` 不 close 会永久阻塞，等价于泄漏。

---

## 七、close 的正确姿势

### 原则一：只有发送方关闭

```go
// ❌ 接收方关闭 → 后续发送会 panic
func consumer(ch chan int) {
    v := <-ch
    close(ch)  // ❌
}

// ✅ 发送方关闭
func producer(ch chan int) {
    for i := 0; i < 10; i++ {
        ch <- i
    }
    close(ch)  // ✅
}
```

### 原则二：多发送方场景，用协调 channel

```go
// 多个发送方，谁都不能直接 close（可能其他人还在发）
// 用一个专门的 done channel 协调

func producers(ch chan int, done chan struct{}) {
    for i := 0; i < 10; i++ {
        select {
        case <-done:
            return  // 收到停止信号就不发了
        case ch <- i:
        }
    }
}

// 由外部（一般是唯一的协调方）close done
close(done)
```

### 常用检查模式

```go
// 检查 channel 是否已关闭（非阻塞）
select {
case _, ok := <-ch:
    if !ok {
        // channel 已关闭
    }
default:
    // channel 还开着但没数据
}
```

---

## 八、select 语句

`select` 用于同时等待多个 channel 操作。

### 基础用法

```go
select {
case v := <-ch1:
    fmt.Println("recv from ch1:", v)
case ch2 <- 42:
    fmt.Println("sent to ch2")
case <-time.After(time.Second):
    fmt.Println("timeout")   // 超时
default:
    fmt.Println("nothing ready")  // 非阻塞
}
```

**规则：**
- 多个 case 同时就绪 → **随机选一个**（避免饥饿）
- 都没就绪 + 有 default → 立即执行 default
- 都没就绪 + 无 default → 阻塞等待

### 常见模式一：超时控制

```go
select {
case result := <-ch:
    handle(result)
case <-time.After(3 * time.Second):
    return errors.New("timeout")
}
```

### 常见模式二：非阻塞发送/接收

```go
// 非阻塞发送
select {
case ch <- data:
    // 发送成功
default:
    // 缓冲区满，丢弃或降级
}

// 非阻塞接收
select {
case v := <-ch:
    process(v)
default:
    // 没数据，跳过
}
```

### 常见模式三：优雅退出

```go
func worker(ctx context.Context, tasks chan Task) {
    for {
        select {
        case <-ctx.Done():
            return   // 收到取消信号
        case t := <-tasks:
            process(t)
        }
    }
}
```

---

## 九、常见并发模式

### 模式 1：Worker Pool（工作池）

```go
func workerPool(jobs <-chan int, results chan<- int, workerCount int) {
    var wg sync.WaitGroup
    for i := 0; i < workerCount; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for job := range jobs {
                results <- job * 2  // 处理
            }
        }(i)
    }
    wg.Wait()
    close(results)
}

func main() {
    jobs := make(chan int, 100)
    results := make(chan int, 100)

    go workerPool(jobs, results, 5)

    // 提交任务
    for i := 1; i <= 20; i++ {
        jobs <- i
    }
    close(jobs)

    // 收集结果
    for r := range results {
        fmt.Println(r)
    }
}
```

### 模式 2：Pipeline（流水线）

```go
// 阶段 1：产生数字
func gen(nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for _, n := range nums {
            out <- n
        }
    }()
    return out
}

// 阶段 2：平方
func sq(in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for n := range in {
            out <- n * n
        }
    }()
    return out
}

func main() {
    // 组装 pipeline
    for v := range sq(gen(1, 2, 3, 4)) {
        fmt.Println(v)  // 1, 4, 9, 16
    }
}
```

### 模式 3：Fan-out / Fan-in（扇出扇入）

```go
// Fan-out：多个 worker 消费同一个 channel
func fanOut(in <-chan int, workers int) []<-chan int {
    outs := make([]<-chan int, workers)
    for i := 0; i < workers; i++ {
        out := make(chan int)
        outs[i] = out
        go func() {
            defer close(out)
            for v := range in {
                out <- v * v  // 处理
            }
        }()
    }
    return outs
}

// Fan-in：多个 channel 合并到一个
func fanIn(chs ...<-chan int) <-chan int {
    out := make(chan int)
    var wg sync.WaitGroup
    for _, ch := range chs {
        wg.Add(1)
        go func(c <-chan int) {
            defer wg.Done()
            for v := range c {
                out <- v
            }
        }(ch)
    }
    go func() {
        wg.Wait()
        close(out)
    }()
    return out
}
```

### 模式 4：限流器（信号量）

```go
sem := make(chan struct{}, 3)  // 最多 3 个并发

for i := 0; i < 100; i++ {
    sem <- struct{}{}  // 获取（满了阻塞）
    go func(i int) {
        defer func() { <-sem }()  // 释放
        process(i)
    }(i)
}
```

---

## 十、发送方向限制

函数参数可以声明**单向 channel**，增强类型安全：

```go
// chan<- int：只能发送
func producer(ch chan<- int) {
    ch <- 1
    // <-ch  // ❌ 编译错误
}

// <-chan int：只能接收
func consumer(ch <-chan int) {
    v := <-ch
    // ch <- 1  // ❌ 编译错误
}

// 双向 channel 可以转为单向，反之不行
ch := make(chan int)
producer(ch)  // ✅ 隐式转换
```

---

## 十一、面试高频陷阱

### 陷阱 1：向 nil channel 发送/接收永久阻塞

```go
var ch chan int
go func() {
    ch <- 1  // ⚠️ 永久阻塞，goroutine 泄漏
}()
```

**利用场景**：动态禁用 select 分支

```go
// 用 nil 让 select case 失效
var timerCh <-chan time.Time
if enableTimeout {
    timerCh = time.After(3 * time.Second)
}

select {
case v := <-dataCh:
    process(v)
case <-timerCh:  // timerCh 是 nil 时，这个 case 永不触发
    handleTimeout()
}
```

### 陷阱 2：channel 泄漏

```go
// ❌ goroutine 泄漏
func query(ctx context.Context) string {
    ch := make(chan string)  // 无缓冲
    go func() {
        ch <- slowQuery()  // 如果没人读，永久阻塞
    }()

    select {
    case r := <-ch:
        return r
    case <-ctx.Done():
        return ""  // context 取消了，但 goroutine 还在等 ch
    }
}

// ✅ 修复：用缓冲 channel，让 goroutine 能立即写完退出
func query(ctx context.Context) string {
    ch := make(chan string, 1)  // 容量 1
    go func() {
        ch <- slowQuery()  // ✅ 不阻塞，写完就退
    }()

    select {
    case r := <-ch:
        return r
    case <-ctx.Done():
        return ""
    }
}
```

### 陷阱 3：关闭 channel 时机

```go
// ❌ 多发送方场景直接 close
func multiProducers(ch chan int) {
    var wg sync.WaitGroup
    for i := 0; i < 3; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            ch <- 1
            close(ch)  // ❌ 多个 goroutine 竞争 close → panic
        }()
    }
    wg.Wait()
}

// ✅ 等所有发送方结束后再 close
func multiProducers() chan int {
    ch := make(chan int)
    var wg sync.WaitGroup
    for i := 0; i < 3; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            ch <- 1
        }()
    }
    go func() {
        wg.Wait()
        close(ch)  // ✅ 唯一 closer
    }()
    return ch
}
```

---

## 十二、面试高频题

### Q1: Channel 底层结构？

`hchan` 结构，包含环形缓冲区 `buf`、发送/接收位置 `sendx/recvx`、等待队列 `sendq/recvq`、锁 `lock`。所有操作都持锁，是**串行**的。

### Q2: 无缓冲和有缓冲 channel 的区别？

- 无缓冲：发送和接收必须同时就绪（同步握手）
- 有缓冲：缓冲区未满可发送，未空可接收（异步）

### Q3: 向已关闭的 channel 发送/接收会怎样？

- 发送：panic
- 接收：立即返回，返回零值 + `ok=false`（如果缓冲区还有数据，先取完再返回零值）

### Q4: nil channel 有什么特点？

发送和接收都永久阻塞。可以用来动态禁用 `select` 的某个 case。

### Q5: 为什么推荐"发送方 close"？

close 相当于"发送完成"的信号。接收方不知道发送方还会不会发，close 逻辑属于发送方。多发送方场景，用 sync.WaitGroup 等所有发送方结束后由协调方 close。

### Q6: channel 和 mutex 该怎么选？

- 传递**数据流/所有权** → channel（生产消费、pipeline、fan-out）
- 保护**共享状态** → mutex（多个 goroutine 读写同一份数据）

Rob Pike 原话：`sync` for state, `channels` for communication.

### Q7: channel 会不会导致死锁？

会。典型场景：
- 无缓冲 channel 单 goroutine 自发自收
- 循环 channel 依赖（A 等 B，B 等 A）
- 忘记 close 导致 range 永远阻塞

用 `go run -race` 和 pprof 定位。
