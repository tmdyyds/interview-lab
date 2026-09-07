# Context 详解

**标签**: #go #concurrency #context #取消传播 #超时控制 #高频

---

## 一、Context 是什么

`context.Context` 是 Go 标准库提供的**跨 goroutine 传递取消信号、超时截止、请求级元数据**的统一机制。

```go
type Context interface {
    Deadline() (deadline time.Time, ok bool)  // 返回截止时间（如果有）
    Done() <-chan struct{}                      // 取消信号 channel
    Err() error                                // Done 关闭后，返回取消原因
    Value(key any) any                         // 取出请求级元数据
}
```

**一句话理解**：Context 是 goroutine 的"遥控器" —— 主调方通过 context 告诉下游 goroutine "该停了"，下游通过 `<-ctx.Done()` 监听这个信号。

---

## 二、为什么需要 Context

### 场景一：HTTP 请求超时

客户端已断开连接，服务端还在执行数据库查询 + 外部 RPC —— 白白浪费资源。

```
Client ──[断开]──▶ Server
                     │
                     ├── DB Query    ← 还在跑
                     ├── RPC Call    ← 还在跑
                     └── File I/O   ← 还在跑
```

有了 context：客户端断开 → context 取消 → 所有下游操作都收到信号，立即退出。

### 场景二：goroutine 泄漏

没有取消机制的 goroutine 一旦启动就无法停止 —— 典型的泄漏根源。

```go
// ❌ 无法停止
go func() {
    for {
        doWork()
        time.Sleep(time.Second)
    }
}()

// ✅ 用 context 控制生命周期
go func() {
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return  // 收到取消信号，退出
        case <-ticker.C:
            doWork()
        }
    }
}()
```

### 场景三：级联取消

一个请求衍生出 N 个 goroutine，取消根 context → 所有派生 context 都收到取消信号。

```
              ctx (root)
             / | \
           /   |   \
        ctx1  ctx2  ctx3     ← cancel(ctx) → ctx1/ctx2/ctx3 全部取消
              / \
           ctx4  ctx5        ← ctx2 取消 → ctx4/ctx5 也取消
```

---

## 三、四种创建方式

### 1. context.Background() / context.TODO()

```go
// Background：顶层 context，一切 context 树的根
ctx := context.Background()

// TODO：还没决定用哪个 context 的占位符（代码迁移期间用）
ctx := context.TODO()
```

两者本质一样（都是空 context），区别是**语义**：Background 表示"顶层入口"，TODO 表示"暂时没想好"。

### 2. context.WithCancel —— 手动取消

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()  // ⚠️ 必须调用，否则资源泄漏

go func() {
    select {
    case <-ctx.Done():
        fmt.Println("cancelled:", ctx.Err())  // context canceled
        return
    case <-time.After(10 * time.Second):
        fmt.Println("done")
    }
}()

time.Sleep(time.Second)
cancel()  // 手动取消 → ctx.Done() 被关闭
```

### 3. context.WithTimeout —— 超时自动取消

```go
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()  // ⚠️ 即使超时了也必须调用（释放定时器资源）

select {
case <-ctx.Done():
    fmt.Println(ctx.Err())  // context deadline exceeded
}
```

### 4. context.WithDeadline —— 截止时间自动取消

```go
deadline := time.Now().Add(5 * time.Second)
ctx, cancel := context.WithDeadline(context.Background(), deadline)
defer cancel()

dl, ok := ctx.Deadline()
fmt.Println(dl, ok)  // 2026-09-01 xx:xx:xx +0000 UTC true
```

**WithTimeout 和 WithDeadline 的关系**：

```go
// WithTimeout 内部就是调用 WithDeadline
func WithTimeout(parent Context, timeout time.Duration) (Context, CancelFunc) {
    return WithDeadline(parent, time.Now().Add(timeout))
}
```

- `WithTimeout` —— "再过多久取消"（相对时间）
- `WithDeadline` —— "到哪个时刻取消"（绝对时间）

### 5. context.WithValue —— 传递请求级元数据

```go
type ctxKey string

const requestIDKey ctxKey = "request_id"

ctx := context.WithValue(context.Background(), requestIDKey, "abc-123")

// 取值
if v := ctx.Value(requestIDKey); v != nil {
    fmt.Println("request_id:", v)  // abc-123
}
```

---

## 四、Context 的树形结构与取消传播

Context 是**树形继承关系**：子 context 从父 context 派生，取消父 → 所有子孙都被取消。

```go
root := context.Background()

ctx1, cancel1 := context.WithCancel(root)
ctx2, cancel2 := context.WithTimeout(ctx1, 5*time.Second)
ctx3 := context.WithValue(ctx2, "key", "val")
```

```
         Background (root)
              │
        ┌─────┴─────┐
        │            │
     ctx1          other...
   (cancel)
        │
      ctx2
   (timeout 5s)
        │
      ctx3
    (value)
```

**取消传播规则：**

| 操作 | 效果 |
|-----|------|
| `cancel1()` | ctx1、ctx2、ctx3 全部取消 |
| `cancel2()` | ctx2、ctx3 取消，ctx1 不受影响 |
| ctx2 超时 | ctx2、ctx3 取消，ctx1 不受影响 |
| 子 context 取消 | 父 context **不受影响**（单向传播） |

**关键**：取消只向下传播，不向上传播。

---

## 五、context.WithoutCancel（Go 1.21+）

从父 context 派生一个**不继承取消信号**的子 context，但仍然继承 Value 和 Deadline。

```go
parentCtx, cancel := context.WithCancel(context.Background())
childCtx := context.WithoutCancel(parentCtx)

cancel()  // 取消父 context

fmt.Println(parentCtx.Err()) // context canceled
fmt.Println(childCtx.Err())  // <nil>  ← 不受影响
```

**典型场景**：HTTP 请求取消了，但后台审计日志、异步清理等操作需要继续执行。

```go
func handleRequest(ctx context.Context) {
    // 业务逻辑（跟随请求取消）
    result := doWork(ctx)

    // 审计日志（不跟随请求取消，要保证写入）
    auditCtx := context.WithoutCancel(ctx)
    go writeAuditLog(auditCtx, result)
}
```

---

## 六、context.AfterFunc（Go 1.21+）

注册一个回调函数，在 context 取消时**在新 goroutine 中**执行。

```go
ctx, cancel := context.WithCancel(context.Background())

stop := context.AfterFunc(ctx, func() {
    fmt.Println("context cancelled, cleaning up...")
    // 清理资源
})

// 如果不再需要回调，可以提前取消注册
// stop()  // 返回 true 表示成功取消注册

cancel()  // 触发 AfterFunc 回调
// 输出：context cancelled, cleaning up...
```

**用途**：替代手动 `go func() { <-ctx.Done(); cleanup() }()` 的样板代码。

---

## 七、context.WithCancelCause（Go 1.20+）

取消时附带原因，方便诊断。

```go
ctx, cancel := context.WithCancelCause(context.Background())

cancel(fmt.Errorf("upstream service unavailable"))

fmt.Println(ctx.Err())                    // context canceled
fmt.Println(context.Cause(ctx))           // upstream service unavailable
```

**与普通 WithCancel 的区别**：

```go
// WithCancel → ctx.Err() 只返回固定的 "context canceled"
// WithCancelCause → context.Cause(ctx) 返回你传入的具体原因
```

**实战场景**：微服务调用链路中，下游超时时附带具体信息，上游可以区分"是自己超时还是下游超时"。

---

## 八、Context 使用规范

### 规范 1：context 作为函数第一个参数，命名 ctx

```go
// ✅ 标准写法
func QueryUser(ctx context.Context, userID int) (*User, error) { ... }

// ❌ 不放第一个
func QueryUser(userID int, ctx context.Context) (*User, error) { ... }

// ❌ 放进 struct
type Service struct {
    ctx context.Context  // ❌ 不要这样做
}
```

**为什么不放 struct 里**：Context 是请求级的（每个请求一个 ctx），放 struct 里意味着所有请求共享同一个 ctx，无法区分不同请求的取消/超时。

### 规范 2：defer cancel() 必须调用

```go
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()  // ⚠️ 必须！即使函数正常返回也要调用

// 不调用 cancel 的后果：
// 1. 定时器不会被回收 → 资源泄漏
// 2. goroutine 可能永远等待 → goroutine 泄漏
```

### 规范 3：不要传 nil context

```go
// ❌ 会 panic
func bad() {
    doSomething(nil)
}

// ✅ 不确定用什么就用 TODO
func good() {
    doSomething(context.TODO())
}
```

### 规范 4：WithValue 只传请求级元数据，不传业务参数

```go
// ✅ 适合用 WithValue 的
ctx = context.WithValue(ctx, requestIDKey, "abc-123")     // 请求 ID
ctx = context.WithValue(ctx, traceIDKey, "trace-456")     // 链路追踪 ID
ctx = context.WithValue(ctx, tenantIDKey, 42)             // 租户 ID

// ❌ 不适合的（应该作为函数参数显式传递）
ctx = context.WithValue(ctx, "user", userObject)          // 业务对象
ctx = context.WithValue(ctx, "db", dbConnection)          // 依赖注入
ctx = context.WithValue(ctx, "filter", queryFilter)       // 查询条件
```

**原则**：WithValue 里的值应该是"跨切面（cross-cutting）"的元数据，不是业务逻辑的输入。

### 规范 5：WithValue 的 key 用自定义类型，避免冲突

```go
// ❌ 用 string 当 key → 不同包可能冲突
ctx = context.WithValue(ctx, "id", 123)

// ✅ 用自定义类型 → 编译期类型安全
type ctxKey string
const userIDKey ctxKey = "user_id"
ctx = context.WithValue(ctx, userIDKey, 123)

// ✅ 更简洁：用空 struct 类型
type requestIDKeyType struct{}
var requestIDKey = requestIDKeyType{}
ctx = context.WithValue(ctx, requestIDKey, "abc-123")
```

---

## 九、实战模式

### 模式 1：HTTP Handler 超时控制

```go
func handler(w http.ResponseWriter, r *http.Request) {
    // r.Context() 已经绑定了客户端连接生命周期
    // 客户端断开 → r.Context() 取消
    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    result, err := queryDB(ctx)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            http.Error(w, "request timeout", http.StatusGatewayTimeout)
            return
        }
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    json.NewEncoder(w).Encode(result)
}
```

### 模式 2：多服务并发调用 + 统一超时

```go
func fetchAll(ctx context.Context) (*Result, error) {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    g, ctx := errgroup.WithContext(ctx)

    var user *User
    var orders []*Order

    g.Go(func() error {
        var err error
        user, err = fetchUser(ctx)
        return err
    })

    g.Go(func() error {
        var err error
        orders, err = fetchOrders(ctx)
        return err
    })

    if err := g.Wait(); err != nil {
        return nil, err  // 任一失败 → ctx 自动取消 → 其他也收到信号
    }

    return &Result{User: user, Orders: orders}, nil
}
```

### 模式 3：优雅关闭服务

```go
func main() {
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    srv := &http.Server{Addr: ":8080", Handler: mux}

    // 启动 HTTP 服务
    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("listen: %v", err)
        }
    }()

    <-ctx.Done()  // 等待信号
    log.Println("shutting down...")

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := srv.Shutdown(shutdownCtx); err != nil {
        log.Fatalf("shutdown: %v", err)
    }
    log.Println("server stopped")
}
```

### 模式 4：数据库查询超时

```go
func queryDB(ctx context.Context, db *sql.DB) ([]Row, error) {
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()

    rows, err := db.QueryContext(ctx, "SELECT * FROM users WHERE active = ?", true)
    if err != nil {
        return nil, fmt.Errorf("query users: %w", err)
    }
    defer rows.Close()

    var result []Row
    for rows.Next() {
        var r Row
        if err := rows.Scan(&r.ID, &r.Name); err != nil {
            return nil, fmt.Errorf("scan row: %w", err)
        }
        result = append(result, r)
    }
    return result, rows.Err()
}
```

### 模式 5：长时间任务的取消检查

```go
func processItems(ctx context.Context, items []Item) error {
    for i, item := range items {
        // 每处理一批检查一次取消信号
        if i%100 == 0 {
            select {
            case <-ctx.Done():
                return ctx.Err()
            default:
            }
        }
        if err := process(item); err != nil {
            return err
        }
    }
    return nil
}
```

---

## 十、底层原理

### Done() channel 的懒初始化

`Done()` 返回的 channel 是**第一次调用时才创建**的（懒初始化），避免不需要取消的 context 浪费资源。

```go
// 简化版原理
func (c *cancelCtx) Done() <-chan struct{} {
    c.mu.Lock()
    if c.done == nil {
        c.done = make(chan struct{})
    }
    d := c.done
    c.mu.Unlock()
    return d
}
```

### 取消传播的实现

父 context 取消时，遍历所有子 context 并逐一取消：

```go
// 简化版原理
func (c *cancelCtx) cancel(err error) {
    c.mu.Lock()
    close(c.done)        // 关闭 Done channel
    for child := range c.children {
        child.cancel(err)  // 递归取消所有子 context
    }
    c.children = nil
    c.mu.Unlock()
}
```

### Value 的查找：沿着 parent 链逐级向上

```go
// 简化版原理
func (c *valueCtx) Value(key any) any {
    if c.key == key {
        return c.val
    }
    return c.parent.Value(key)  // 向上查找
}
```

**时间复杂度**：O(n)，n 是 context 链的深度。不要在 Value 链上存太多值。

```
WithValue(key3, val3)
    │
    └── WithValue(key2, val2)
            │
            └── WithValue(key1, val1)
                    │
                    └── Background
    
查找 key1：从最外层开始，逐层向上，找到 key1 返回 val1
查找 key4：遍历到 Background，返回 nil
```

---

## 十一、常见陷阱

### 陷阱 1：忘记调用 cancel → 资源泄漏

```go
// ❌ 内存泄漏
func bad() {
    ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
    // cancel 被丢弃，定时器和 goroutine 无法回收
    doWork(ctx)
}

// ✅ 必须 defer cancel
func good() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    doWork(ctx)
}
```

`go vet` 会检测到 cancel 未使用并报警。

### 陷阱 2：把 context 放进 struct

```go
// ❌ 所有请求共享一个 context
type Server struct {
    ctx context.Context  // 不同请求用同一个 ctx？
}

// ✅ context 通过函数参数逐层传递
func (s *Server) Handle(ctx context.Context, req *Request) error {
    return s.service.Process(ctx, req)
}
```

**例外**：如果 struct 本身代表一次请求（如 `http.Request`），可以嵌入 context。

### 陷阱 3：用 context 做依赖注入

```go
// ❌ 反模式
ctx = context.WithValue(ctx, "db", db)
ctx = context.WithValue(ctx, "logger", logger)

// 取出来还要类型断言，容易出错
db := ctx.Value("db").(*sql.DB)  // key 写错就是 nil

// ✅ 用显式参数或 struct 注入
func NewService(db *sql.DB, logger *slog.Logger) *Service { ... }
```

### 陷阱 4：子 context 超时大于父 context

```go
parentCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()

// ⚠️ 子 context 5 秒，但父只有 3 秒
// 实际生效的是 3 秒（取较短的那个）
childCtx, cancel2 := context.WithTimeout(parentCtx, 5*time.Second)
defer cancel2()
```

子 context 的 Deadline **不能晚于**父 context。如果子请求了更长的超时，实际仍然以父的为准。

### 陷阱 5：在 select 中不检查 ctx.Done()

```go
// ❌ 无法取消
for {
    select {
    case task := <-taskCh:
        process(task)
    }
}

// ✅ 每个 select 都加 ctx.Done()
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case task := <-taskCh:
        process(task)
    }
}
```

---

## 十二、Context 选型速查

| 需求 | 用什么 |
|-----|-------|
| 顶层入口（main、init、test） | `context.Background()` |
| 占位符，迁移期间暂时没想好 | `context.TODO()` |
| 手动控制取消 | `context.WithCancel` |
| 手动取消 + 附带原因 | `context.WithCancelCause`（Go 1.20+） |
| 相对时间超时 | `context.WithTimeout` |
| 绝对时间截止 | `context.WithDeadline` |
| 传递请求级元数据 | `context.WithValue` |
| 子 context 不跟随父取消 | `context.WithoutCancel`（Go 1.21+） |
| context 取消时回调清理 | `context.AfterFunc`（Go 1.21+） |
| 监听系统信号并取消 | `signal.NotifyContext` |

---

## 十三、面试高频题

### Q1: context.Context 是什么？为什么需要它？

Context 是 Go 标准库提供的接口，用于跨 goroutine 传递取消信号、超时截止和请求级元数据。核心目的是避免 goroutine 泄漏 —— 让主调方能通知下游 goroutine "该停了"。

### Q2: WithCancel、WithTimeout、WithDeadline 有什么区别？

- WithCancel：手动调用 cancel() 取消
- WithTimeout：指定一个**时长**，超时后自动取消（内部调 WithDeadline）
- WithDeadline：指定一个**绝对时刻**，到期后自动取消

三者都返回 cancel 函数，**必须 defer cancel()**。

### Q3: context.Value 应该存什么？

只存跨切面的请求级元数据（request ID、trace ID、租户 ID），不存业务参数、数据库连接等。key 要用自定义类型避免包间冲突。Value 查找是 O(n) 沿 parent 链向上，不适合存大量数据。

### Q4: 为什么 cancel 必须调用？

不调用 cancel 会导致：
1. 定时器无法回收（WithTimeout/WithDeadline 内部创建了 timer）
2. 父 context 一直持有对子 context 的引用 → 内存泄漏
3. 关联的 goroutine 可能永远等待

`go vet` 会检测未使用的 cancel。

### Q5: context 为什么不建议放到 struct 里？

Context 是请求级的，每个请求应有独立的 ctx。放 struct 里会导致多个请求共享同一 ctx，无法区分各请求的取消/超时。推荐作为函数第一个参数显式传递。

### Q6: 子 context 超时设得比父 context 长会怎样？

子 context 的 Deadline 不能晚于父 context。如果子设了 5s 但父只有 3s，实际 3s 后子就会被取消（父取消 → 子也取消）。

### Q7: 如何实现优雅关闭？

用 `signal.NotifyContext` 监听 SIGINT/SIGTERM，收到信号后 context 自动取消。然后用一个带超时的新 context 调用 `srv.Shutdown()`，等待现有请求处理完毕。

### Q8: context.WithoutCancel 的使用场景？

当父 context 取消后，某些操作仍需继续执行（审计日志、异步清理、指标上报）。`WithoutCancel` 派生的子 context 不继承取消信号，但仍继承 Value。Go 1.21+ 可用。

### Q9: context.AfterFunc 有什么用？

注册一个回调函数，在 context 被取消时自动在新 goroutine 中执行。替代手动写 `go func() { <-ctx.Done(); cleanup() }()` 的样板代码。返回一个 stop 函数可以提前取消注册。Go 1.21+ 可用。

### Q10: 多个 goroutine 同时调用 cancel() 安全吗？

安全。cancel 是幂等的，多次调用只有第一次生效，后续调用是 no-op。内部通过 `sync.Once` 保证只关闭一次 Done channel。
