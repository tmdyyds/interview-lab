# errgroup + singleflight 深度剖析

**标签**: #go #ecosystem #concurrency #x-sync #高频

`golang.org/x/sync` 是 Go 官方扩展的并发工具集。其中 **errgroup** 和 **singleflight** 是几乎每个中大型 Go 项目都会用到的两个包。

---

## 一、errgroup：一组 goroutine 的错误协调

### 解决什么问题

原生 `sync.WaitGroup` 只等待 goroutine 完成，不能：
1. 收集任一 goroutine 的错误
2. 一个失败其他自动取消
3. 限制并发数

`errgroup` 一次性解决三个。

### 基本用法

```go
import "golang.org/x/sync/errgroup"

func fetchAll(ctx context.Context, urls []string) error {
    g, ctx := errgroup.WithContext(ctx)

    for _, url := range urls {
        url := url  // Go 1.22 前必须这一行
        g.Go(func() error {
            resp, err := http.Get(url)
            if err != nil {
                return err  // 任一 goroutine 返回错误 → ctx 立即取消
            }
            defer resp.Body.Close()
            // 处理响应...
            return nil
        })
    }

    return g.Wait()  // 等所有完成，返回第一个错误
}
```

### 源码剖析（错误传播机制）

```go
// x/sync/errgroup/errgroup.go 简化
type Group struct {
    cancel  func(error)  // 取消函数（WithContext 提供）
    wg      sync.WaitGroup
    errOnce sync.Once   // 保证只记录第一个错误
    err     error
    sem     chan token  // 限流信号量（可选）
}

func WithContext(ctx context.Context) (*Group, context.Context) {
    ctx, cancel := context.WithCancelCause(ctx)
    return &Group{cancel: cancel}, ctx
}

func (g *Group) Go(f func() error) {
    if g.sem != nil {
        g.sem <- token{}  // 限流：满了阻塞
    }
    g.wg.Add(1)

    go func() {
        defer g.done()

        if err := f(); err != nil {
            g.errOnce.Do(func() {  // 只记录第一个错误
                g.err = err
                if g.cancel != nil {
                    g.cancel(err)  // ← 关键：取消 ctx，通知其他 goroutine 退出
                }
            })
        }
    }()
}

func (g *Group) Wait() error {
    g.wg.Wait()
    if g.cancel != nil {
        g.cancel(g.err)
    }
    return g.err
}
```

**关键设计**：
- **只记录第一个错误**：用 `sync.Once` 保证幂等
- **失败即取消**：第一个错误发生时立即 `cancel(ctx)`，其他 goroutine 通过 `ctx.Done()` 感知
- **不改变 goroutine 泄漏风险**：如果你的 f 里没检查 ctx，它还是会跑完（errgroup 不能强杀 goroutine）

### 陷阱 1：函数里不检查 ctx，取消无效

```go
// ❌ 取消 ctx 后这个 goroutine 还在跑
g.Go(func() error {
    time.Sleep(10 * time.Second)  // 不检查 ctx，硬等
    return nil
})

// ✅ 显式检查 ctx
g.Go(func() error {
    select {
    case <-time.After(10 * time.Second):
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
})
```

**理解**：errgroup 的取消是"通知"，不是"强杀"。goroutine 必须自己响应 `ctx.Done()`。

### 陷阱 2：`WithContext` vs `Group{}`

```go
// 版本一：不需要取消传播
var g errgroup.Group
g.Go(func() error { ... })
g.Wait()

// 版本二：需要一个失败取消其他
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error { ... })
g.Wait()
```

**规则**：如果 goroutine 之间是**独立的**（比如并发上传多个不相关的文件，一个失败其他继续）用版本一；如果是**协作的**（比如聚合多个数据源，一个失败整个失败）用版本二。

### 限流：SetLimit（Go 1.20+）

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(10)  // 最多 10 个并发

for i := 0; i < 1000; i++ {
    g.Go(func() error {
        return doWork()
    })
}
g.Wait()
```

**内部实现**：`sem` channel，容量 10，`Go` 时写入信号量，goroutine 结束时读走。

**注意**：`SetLimit` 会阻塞 `Go` 直到有空位。如果 `Go` 里嵌套 `Go`，可能死锁：

```go
// ❌ 死锁风险
g.SetLimit(1)
g.Go(func() error {
    g.Go(func() error { ... })  // 等信号量，但被自己占着 → 死锁
    return nil
})

// ✅ 用 TryGo（Go 1.20+）
g.Go(func() error {
    if !g.TryGo(func() error { ... }) {
        // 池满了，同步执行或跳过
    }
    return nil
})
```

### 陷阱 3：goroutine 里的 panic

```go
// ❌ panic 不会被 errgroup 捕获，整个程序崩
g.Go(func() error {
    panic("boom")
})

// ✅ 每个 goroutine 自己 recover
g.Go(func() (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic: %v", r)
        }
    }()
    doWork()
    return nil
})
```

---

## 二、errgroup 常用模式

### 模式 1：并行查多源聚合

```go
func aggregateData(ctx context.Context, userID string) (*UserFullInfo, error) {
    g, ctx := errgroup.WithContext(ctx)

    var (
        profile *Profile
        orders  []*Order
        friends []*Friend
    )

    g.Go(func() (err error) {
        profile, err = profileSvc.Get(ctx, userID)
        return
    })
    g.Go(func() (err error) {
        orders, err = orderSvc.List(ctx, userID)
        return
    })
    g.Go(func() (err error) {
        friends, err = friendSvc.List(ctx, userID)
        return
    })

    if err := g.Wait(); err != nil {
        return nil, err
    }
    return &UserFullInfo{profile, orders, friends}, nil
}
```

### 模式 2：批量处理任务（带限流）

```go
func batchProcess(ctx context.Context, tasks []Task) error {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(50)  // 最多 50 个并发

    for _, task := range tasks {
        task := task
        g.Go(func() error {
            return process(ctx, task)
        })
    }
    return g.Wait()
}
```

### 模式 3：容错聚合（一个失败不影响其他）

如果不想失败即取消，用普通 `Group` + 收集错误：

```go
func aggregateAllowFail(ctx context.Context) (Result, error) {
    var (
        g   errgroup.Group  // ← 不用 WithContext
        mu  sync.Mutex
        r   Result
        errs []error
    )

    g.Go(func() error {
        v, err := source1(ctx)
        mu.Lock()
        defer mu.Unlock()
        if err != nil {
            errs = append(errs, err)
        } else {
            r.Data1 = v
        }
        return nil  // 返回 nil，不触发全局取消
    })
    // ... source2, source3

    g.Wait()
    if len(errs) == len(sources) {  // 全部失败才认为整体失败
        return r, errors.Join(errs...)
    }
    return r, nil
}
```

---

## 三、singleflight：请求合并

### 解决什么问题

**缓存击穿**场景：某个热 key 缓存失效瞬间，1000 个请求同时打到 DB。

```go
// ❌ 每个请求都查 DB
func GetUser(ctx context.Context, id int) (*User, error) {
    if v, ok := cache.Get(id); ok {
        return v, nil
    }
    user, err := db.Query(id)  // 1000 个请求同时到这里！
    if err != nil {
        return nil, err
    }
    cache.Set(id, user)
    return user, nil
}
```

`singleflight` 让**同一 key 的并发请求只执行一次**，其他等结果：

```go
import "golang.org/x/sync/singleflight"

var sf singleflight.Group

func GetUser(ctx context.Context, id int) (*User, error) {
    if v, ok := cache.Get(id); ok {
        return v.(*User), nil
    }

    // 同一 id 的并发请求合并为一次
    v, err, _ := sf.Do(fmt.Sprintf("user:%d", id), func() (any, error) {
        user, err := db.Query(id)
        if err != nil {
            return nil, err
        }
        cache.Set(id, user)
        return user, nil
    })
    if err != nil {
        return nil, err
    }
    return v.(*User), nil
}
```

**效果**：1000 个请求 → **1 次 DB 查询**，其他 999 个共享结果。

### 源码剖析

```go
// x/sync/singleflight/singleflight.go 简化
type Group struct {
    mu sync.Mutex
    m  map[string]*call  // 正在执行的 key -> call
}

type call struct {
    wg sync.WaitGroup  // 等结果
    val interface{}
    err error
    dups int  // 复用次数
}

func (g *Group) Do(key string, fn func() (any, error)) (any, error, bool) {
    g.mu.Lock()
    if c, ok := g.m[key]; ok {
        c.dups++
        g.mu.Unlock()
        c.wg.Wait()  // ← 等第一个请求的结果
        return c.val, c.err, true  // shared=true
    }

    c := new(call)
    c.wg.Add(1)
    g.m[key] = c  // ← 标记正在执行
    g.mu.Unlock()

    c.val, c.err = fn()  // 真正执行
    c.wg.Done()          // 通知所有等待者

    g.mu.Lock()
    delete(g.m, key)  // 完成后清理
    g.mu.Unlock()

    return c.val, c.err, false
}
```

**核心机制**：
1. 用 `map[string]*call` 记录正在执行的 key
2. 后来的请求发现 key 已在 map 里，就等 `WaitGroup`
3. 第一个请求执行完，`wg.Done()` 唤醒所有等待者
4. 从 map 里删除，允许下一批请求

### 三个返回值

```go
v, err, shared := sf.Do(key, fn)
//              ↑ shared 表示是否复用了别人的结果
```

`shared=true` 表示这次调用是"搭便车"，`false` 表示是真正执行 fn 的那次。

---

## 四、singleflight 常见陷阱

### 陷阱 1：所有调用共享同一个错误

```go
// 用户 A 的请求触发查询，因为 A 的权限问题失败
// 用户 B 同时来查，会拿到"A 的权限错误"（明显不对）
```

**修复**：确保 fn 内部的错误是**通用错误**，不掺入调用方特定的信息。

### 陷阱 2：调用方 context 取消不会取消 fn

```go
ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
defer cancel()

sf.Do("key", func() (any, error) {
    return db.Query()  // ← 内部用什么 ctx？
})
```

`Do` 的调用方超时了，但 `fn` 内部仍在跑（如果没传 ctx 进去）。**需要自己控制 fn 的超时**。

**推荐用 `DoChan` + `select`**：

```go
ch := sf.DoChan(key, fn)
select {
case r := <-ch:
    return r.Val, r.Err
case <-ctx.Done():
    return nil, ctx.Err()  // 但 fn 还是会跑完，等下次请求受益
}
```

### 陷阱 3：Forget —— 强制忘记

如果第一个调用异常（panic / 死锁），后续所有等待者会永远阻塞。用 `Forget` 强制清理：

```go
sf.Do("key", func() (any, error) {
    result, err := slowOp()
    if err != nil {
        sf.Forget("key")  // 让后续请求重试，不共享这个错误
    }
    return result, err
})
```

### 陷阱 4：过热的 key 反而变冷

```go
// singleflight 完成后立即从 map 删除
// 如果 key 极热（1 秒 10 万请求），刚删除又来 1 万请求 → 又执行一次
// 高频热 key 用本地缓存 + singleflight 双保险
```

**最佳实践**：**本地缓存 + singleflight + Redis** 三层防击穿。

---

## 五、singleflight 常用模式

### 模式 1：缓存回源防击穿

```go
var sf singleflight.Group

func GetProduct(ctx context.Context, id int) (*Product, error) {
    // L1: 本地缓存
    if v, ok := localCache.Get(id); ok {
        return v.(*Product), nil
    }

    key := fmt.Sprintf("product:%d", id)
    v, err, _ := sf.Do(key, func() (any, error) {
        // L2: Redis
        if p, err := getFromRedis(ctx, id); err == nil {
            localCache.Set(id, p)
            return p, nil
        }
        // L3: DB 回源
        p, err := getFromDB(ctx, id)
        if err != nil {
            return nil, err
        }
        localCache.Set(id, p)
        setRedis(ctx, id, p)
        return p, nil
    })
    if err != nil {
        return nil, err
    }
    return v.(*Product), nil
}
```

### 模式 2：DoChan + 超时

```go
func GetWithTimeout(ctx context.Context, key string) (any, error) {
    ch := sf.DoChan(key, func() (any, error) {
        return slowQuery()
    })

    select {
    case r := <-ch:
        if r.Shared {
            log.Println("shared result from another call")
        }
        return r.Val, r.Err
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}
```

---

## 六、semaphore：加权信号量

`x/sync/semaphore` 允许每次获取**多个"权重"**，适合处理 CPU / 内存有限的场景。

```go
import "golang.org/x/sync/semaphore"

sem := semaphore.NewWeighted(100)  // 总容量 100

// 场景：处理不同大小的文件，大文件占用更多"权重"
for _, file := range files {
    weight := file.Size / (10 * 1024 * 1024)  // 每 10MB 占 1 权重
    if weight < 1 { weight = 1 }
    if weight > 100 { weight = 100 }

    if err := sem.Acquire(ctx, weight); err != nil {
        return err
    }
    go func(f File) {
        defer sem.Release(weight)
        process(f)
    }(file)
}
```

**vs errgroup.SetLimit**：`SetLimit` 只能限并发数；`semaphore` 可以按资源占用限流。

---

## 七、面试高频题

### Q1: errgroup 和 sync.WaitGroup 的区别？

- `WaitGroup`：只等待，不收集错误，不传播取消
- `errgroup`：等待 + 收集第一个错误 + 失败自动取消 + 可以限流
- errgroup 底层用 WaitGroup + Once + Context

### Q2: errgroup 的 goroutine panic 会被捕获吗？

**不会**。panic 直接崩掉整个进程。必须每个 g.Go 内部 defer recover。这是 Go 语言层面的限制，跨 goroutine 无法 recover。

### Q3: errgroup 的取消是如何传播的？

`WithContext(ctx)` 创建一个带 cancel 的子 ctx。任一 goroutine 返回错误时，第一个错误触发 `cancel(err)`，其他 goroutine 通过 `ctx.Done()` 或 `ctx.Err()` 感知取消。**但必须自己检查 ctx**，errgroup 不能强杀 goroutine。

### Q4: singleflight 的原理？

用 `map[string]*call` 记录正在执行的 key。第一个请求进入时把 call 放 map，其他相同 key 的请求发现 map 里已有，`wg.Wait()` 等结果。第一个执行完 `wg.Done()` 唤醒所有等待者，从 map 删除。

### Q5: singleflight 有什么坑？

1. **共享错误**：调用方相关的错误会被共享给其他调用方
2. **不响应调用方 ctx 取消**：fn 一旦启动就跑完
3. **异常时死锁**：第一个请求 panic 会让所有等待者永远等，用 Forget 兜底
4. **热 key 依然可能击穿**：完成后立即清理 map，需要配合本地缓存

### Q6: singleflight 适合什么场景？

- 缓存回源防击穿
- 相同请求的批量合并（如同一 SKU 的库存查询）
- 幂等的重复调用（DoChan 场景）

### Q7: errgroup 的 SetLimit 会不会死锁？

会。如果嵌套 `g.Go` 且信号量已满，内层 `g.Go` 会阻塞等外层释放，外层等内层完成 → 死锁。Go 1.20+ 有 `TryGo` 可以避免。

### Q8: errgroup 和 conc（sourcegraph/conc）有什么区别？

`conc` 是新一代并发库：
- 自带 panic recovery
- API 更现代（`pool.NewWithResults`）
- 提供 stream / iter 等高级抽象

errgroup 是标准官方扩展；conc 是社区更"人体工学"的选择。**errgroup 依然是生产主流**（稳定 + 官方 + 生态）。

### Q9: singleflight 和分布式锁的区别？

- singleflight：**进程内**去重，本机的并发请求合并
- 分布式锁：**跨进程**互斥，多实例服务同时只有一个执行

两个是配合关系：本机用 singleflight 挡住 99% 的重复请求，跨机器再用分布式锁挡住剩下的少量并发。
