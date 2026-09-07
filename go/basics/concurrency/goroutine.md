# Goroutine 详解

**标签**: #go #concurrency #goroutine #GMP #高频

---

## 一、Goroutine 是什么

Goroutine 是 Go 运行时（runtime）调度的**用户态轻量线程**，由 `go` 关键字启动。

```go
func main() {
    go sayHello("world")   // 启动 goroutine，不阻塞主流程
    time.Sleep(time.Second) // 等 goroutine 执行完
}

func sayHello(name string) {
    fmt.Println("hello", name)
}
```

**核心特点：**

| 对比项 | OS 线程 | Goroutine |
|-------|--------|-----------|
| 栈大小 | 固定（Linux 默认 8MB） | 初始 2KB，动态增长（最大 1GB） |
| 创建成本 | 系统调用（微秒级） | 用户态创建（纳秒级） |
| 切换成本 | 系统调用 + 寄存器保存 | 用户态切换，成本低 |
| 数量上限 | 几千 | 百万级（腾讯有案例跑到 200 万+） |
| 调度器 | OS 内核 | Go runtime（GMP） |

---

## 二、GMP 调度模型（面试核心）

Go 用 **GMP 模型**在少量 OS 线程上调度大量 goroutine。

```
G (Goroutine)：用户协程，包含函数栈、程序计数器、状态
M (Machine)：  OS 线程，真正执行代码的载体
P (Processor)：逻辑处理器，持有 G 的运行队列，M 必须绑定 P 才能执行 G
```

### 结构关系图

```
                     ┌──────────────┐  ┌──────────────┐
                     │   全局队列    │  │   网络轮询器  │
                     │  Global Q    │  │  netpoller   │
                     └──────┬───────┘  └──────┬───────┘
                            │                 │
       ┌───────────┬────────┴────────┬────────┴─────────┐
       │           │                 │                  │
     ┌─▼─┐       ┌─▼─┐             ┌─▼─┐              ┌─▼─┐
     │ P │       │ P │             │ P │              │ P │  ← GOMAXPROCS 个 P
     │本地│      │本地│             │本地│              │本地│    每个 P 一个本地队列
     │队列│      │队列│             │队列│              │队列│    容量 256
     └─┬─┘       └─┬─┘             └─┬─┘              └─┬─┘
       │           │                 │                  │
     ┌─▼─┐       ┌─▼─┐             ┌─▼─┐              ┌─▼─┐
     │ M │       │ M │             │ M │              │ M │  ← OS 线程
     └───┘       └───┘             └───┘              └───┘
       │           │                 │                  │
     [G,G,G]     [G,G]             [G,G,G,G]          [G]     ← 正在跑的 G
```

### 调度流程

```
1. main() 启动 → 创建初始 G0（调度器专用 G）
2. 用户 `go func()` → 创建新 G → 加入当前 P 的本地队列
3. P 的本地队列满（256）→ 一半迁移到全局队列
4. M 从 P 的本地队列取 G 执行
5. 本地队列空 → 从全局队列取一批
6. 全局队列也空 → 从其他 P 偷一半（work stealing）
7. 都空 → M 休眠，等待新 G
```

### GOMAXPROCS

P 的数量由 `GOMAXPROCS` 决定，默认等于 CPU 核心数。

```go
runtime.GOMAXPROCS(0)  // 查询当前值
runtime.GOMAXPROCS(4)  // 设置为 4

// 环境变量方式
// GOMAXPROCS=8 ./myapp
```

**注意**：GOMAXPROCS 是**并行度**的上限，不是 goroutine 数量的上限。goroutine 可以有百万个，但同一时刻最多 GOMAXPROCS 个在真正并行执行。

---

## 三、Goroutine 的栈

### 初始栈很小（2KB）

```go
// Go 1.4+ 每个 goroutine 初始栈 2KB
// 相比 OS 线程 8MB，可以启动几十万个 goroutine
```

### 动态增长（连续栈）

```go
// 栈空间不够时：
// 1. 分配一块新的、更大的内存（通常 2 倍）
// 2. 把旧栈上所有数据复制到新栈
// 3. 更新所有指向旧栈的指针
// 4. 释放旧栈

// Go 1.3 之前是"分段栈"（链表连接多段栈）
// Go 1.3+ 改为"连续栈"（复制到更大的连续内存），性能更好
```

### 栈的最大限制

```go
// 默认最大栈：Linux 1GB，Windows 1GB
// 超过会 panic: runtime: goroutine stack exceeds 1000000000-byte limit

// 常见触发场景：无限递归
func infinite() {
    infinite()  // 栈会一直增长，直到超限 panic
}
```

---

## 四、常见陷阱

### 陷阱 1：main 退出，所有 goroutine 立即终止

```go
func main() {
    go func() {
        time.Sleep(time.Second)
        fmt.Println("goroutine done")  // ⚠️ 可能永远不打印
    }()
    // main 立即退出，goroutine 被强制杀掉
}

// ✅ 解决：用 WaitGroup 或 channel 等待
func main() {
    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        defer wg.Done()
        time.Sleep(time.Second)
        fmt.Println("goroutine done")  // ✅ 会打印
    }()
    wg.Wait()
}
```

### 陷阱 2：循环变量捕获（Go 1.22 之前）

```go
// ❌ Go 1.21 及之前
for i := 0; i < 3; i++ {
    go func() {
        fmt.Println(i)  // 可能都打印 3！
    }()
}
// 原因：所有闭包共享同一个 i 变量，goroutine 执行时 i 已经变成 3

// ✅ 修复方法一：作为参数传入（值拷贝）
for i := 0; i < 3; i++ {
    go func(n int) {
        fmt.Println(n)  // 0, 1, 2（顺序不定）
    }(i)
}

// ✅ 修复方法二：循环内新建局部变量
for i := 0; i < 3; i++ {
    i := i  // 遮蔽，创建新变量
    go func() {
        fmt.Println(i)  // ✅
    }()
}

// ✅ Go 1.22+ 默认每次循环都是新变量，无需修复
// 但生产代码建议兼容老版本，仍然显式处理
```

### 陷阱 3：goroutine 泄漏

goroutine 启动后无法终止，一直占用内存和调度资源。

```go
// ❌ 泄漏场景一：channel 永远不被读，发送方永久阻塞
func leak() {
    ch := make(chan int)
    go func() {
        ch <- 1  // 永远阻塞，goroutine 永远不结束
    }()
    // 函数返回后，没人读 ch，goroutine 泄漏
}

// ❌ 泄漏场景二：context 未取消
func leak() {
    go func() {
        for {
            // 无限循环，没有退出机制
            time.Sleep(time.Second)
        }
    }()
}

// ✅ 正确：用 context 控制生命周期
func good(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return  // ✅ 可退出
            case <-ticker.C:
                // do work
            }
        }
    }()
}
```

### 陷阱 4：goroutine 里 panic 会导致整个程序崩溃

```go
// ❌ goroutine 里的 panic 无法被外部 recover
func main() {
    defer func() {
        if r := recover(); r != nil {
            fmt.Println("recovered:", r)  // ⚠️ 收不到 goroutine 的 panic
        }
    }()

    go func() {
        panic("boom")  // 整个程序崩溃
    }()

    time.Sleep(time.Second)
}

// ✅ 每个 goroutine 内部自己 recover
func main() {
    go func() {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("goroutine recovered: %v", r)
            }
        }()
        panic("boom")
    }()
    time.Sleep(time.Second)
    fmt.Println("main survives")
}
```

**生产建议**：封装一个安全启动函数

```go
func SafeGo(fn func()) {
    go func() {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("goroutine panic: %v\n%s", r, debug.Stack())
                // 上报监控系统
            }
        }()
        fn()
    }()
}

// 使用
SafeGo(func() {
    // 业务代码
})
```

---

## 五、如何检测 goroutine 泄漏

### 方法一：查看当前 goroutine 数量

```go
import "runtime"

fmt.Println(runtime.NumGoroutine())  // 打印当前 goroutine 数
```

### 方法二：pprof

```go
import _ "net/http/pprof"

func main() {
    go http.ListenAndServe("localhost:6060", nil)
    // 业务代码...
}
```

访问：
- `http://localhost:6060/debug/pprof/goroutine?debug=1` 查看所有 goroutine 堆栈
- `http://localhost:6060/debug/pprof/goroutine?debug=2` 详细堆栈

### 方法三：单元测试用 goleak

```go
import "go.uber.org/goleak"

func TestNoLeak(t *testing.T) {
    defer goleak.VerifyNone(t)  // 测试结束时检查是否有泄漏的 goroutine
    // 测试代码...
}
```

---

## 六、Goroutine 数量控制

### 用 semaphore 限流

```go
// 最多同时 10 个 goroutine
sem := make(chan struct{}, 10)
var wg sync.WaitGroup

for i := 0; i < 1000; i++ {
    wg.Add(1)
    sem <- struct{}{}  // 获取信号量（满了会阻塞）
    go func(i int) {
        defer wg.Done()
        defer func() { <-sem }()  // 释放信号量
        // 处理任务
        process(i)
    }(i)
}
wg.Wait()
```

### 用 errgroup 限流（推荐）

```go
import "golang.org/x/sync/errgroup"

g, ctx := errgroup.WithContext(context.Background())
g.SetLimit(10)  // 最多 10 个并发

for i := 0; i < 1000; i++ {
    i := i
    g.Go(func() error {
        return process(ctx, i)
    })
}

if err := g.Wait(); err != nil {
    log.Fatal(err)
}
```

---

## 七、面试高频题

### Q1: Goroutine 和 OS 线程的区别？

Goroutine 是用户态协程，栈初始 2KB 可动态增长，由 Go runtime 调度；OS 线程栈固定 8MB，由内核调度，切换成本高。一个 OS 线程上可以运行多个 goroutine。

### Q2: GMP 模型详细说说？

- G：goroutine，包含栈、PC、状态
- M：OS 线程，真正执行代码
- P：逻辑处理器，持有本地 G 队列，M 必须绑定 P 才能跑 G

P 的数量 = GOMAXPROCS。调度时优先本地队列，其次全局队列，最后从其他 P 偷（work stealing）。

### Q3: Goroutine 的栈为什么可以那么小？

栈会动态增长。初始 2KB，不够时分配 2 倍新栈，把旧栈数据全部复制过去（连续栈机制，Go 1.3+）。这样启动成本极低，同时支持深递归。

### Q4: 为什么 goroutine 会泄漏？怎么排查？

原因：channel 阻塞不释放、无限循环没退出机制、context 未取消。
排查：`runtime.NumGoroutine()` 查数量，pprof 看堆栈，测试用 `goleak`。

### Q5: goroutine 里 panic 怎么办？

必须在 goroutine 内部 `defer recover()`。外层 recover 无效。生产上推荐封装 `SafeGo` 函数统一处理。

### Q6: GOMAXPROCS 应该设多少？

默认等于 CPU 核心数，一般不用改。容器环境下要注意：Go 1.5-1.24 无法自动感知 cgroup limits，可能拿到宿主机核数导致过度并发。Go 1.25+ 自动读取 cgroup。老版本推荐用 `uber-go/automaxprocs` 库自动适配。
