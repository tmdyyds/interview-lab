# Gin 性能与优化

**标签**: #go #gin #performance #sync-pool

Gin 是 Go 生态里最快的 Web 框架之一。本章讲清楚**它为什么快**，以及生产上还能怎么优化。

---

## 一、Gin 快在哪里

### 1. Radix Tree 路由

- 时间复杂度 O(L)（路径长度），与路由数量无关
- 详见 [03-routing.md](./03-routing.md)

### 2. Context 用 sync.Pool 复用

每个请求都要创建 Context，QPS 高时 GC 压力大。Gin 用 `sync.Pool` 缓存 Context 对象：

```go
// Gin 源码简化版
type Engine struct {
    pool sync.Pool
}

func (e *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    c := e.pool.Get().(*Context)  // 从池里拿
    c.reset()                     // 重置状态
    c.Request = req
    c.writermem.reset(w)

    e.handleHTTPRequest(c)

    e.pool.Put(c)                 // 放回池里
}
```

**效果**：单请求少一次分配，减少 GC 频率。

### 3. Handler 链是 slice，无反射

```go
type HandlersChain []HandlerFunc

// 中间件 + 最终 handler 全部编译期确定，运行时直接遍历调用
for c.index < int8(len(c.handlers)) {
    c.handlers[c.index](c)
    c.index++
}
```

不需要反射、不需要动态分派，性能接近原生 `net/http`。

### 4. 参数绑定按需反射

- 请求参数解析只在 `Bind` 时才用反射
- 不 Bind 就没有反射开销

### 5. 响应写入直接走 http.ResponseWriter

```go
// 直接调标准库，没有中间层
c.Writer.WriteHeader(200)
c.Writer.Write(data)
```

Gin 只是加了个"记录状态、字节数"的薄壳，几乎没有性能损耗。

---

## 二、生产优化清单

### 1. 关闭 debug 模式

```go
gin.SetMode(gin.ReleaseMode)
```

`DebugMode` 有额外的路由检查日志、启动横幅等。生产必开 `ReleaseMode`。

### 2. 换 JSON 库为 sonic

```go
import _ "github.com/bytedance/sonic"

func init() {
    // Gin 1.9+ 支持替换 JSON 实现
    // build tag: -tags sonic
}
```

`bytedance/sonic` 比标准库 `encoding/json` 快 3-6 倍，尤其大 payload。

### 3. 关闭不必要的中间件

```go
// gin.Default() 加了 Logger + Recovery
r := gin.Default()

// 高性能场景可以完全自定义
r := gin.New()
r.Use(customFastLogger())
r.Use(customRecovery())
```

`gin.Logger()` 默认写 stdout，可以改为**异步日志**（Zap）。

### 4. 用 http.Server 精细控制

```go
srv := &http.Server{
    Addr:              ":8080",
    Handler:           r,
    ReadTimeout:       10 * time.Second,   // 读请求头 + body 超时
    ReadHeaderTimeout: 5 * time.Second,    // 只读 header 超时
    WriteTimeout:      15 * time.Second,   // 写响应超时
    IdleTimeout:       120 * time.Second,  // keep-alive 空闲超时
    MaxHeaderBytes:    1 << 20,            // 1MB
}
srv.ListenAndServe()
```

**为什么必须设**：默认没有超时会导致慢连接把资源耗尽（Slowloris 攻击）。

### 5. 启用 Gzip

```go
import "github.com/gin-contrib/gzip"

r.Use(gzip.Gzip(gzip.DefaultCompression))
```

对 JSON 响应压缩效果 60% 以上，显著减少带宽。

**注意**：小于几百字节的响应压缩反而费 CPU，可以设阈值：

```go
r.Use(gzip.Gzip(gzip.DefaultCompression, gzip.WithExcludedPaths([]string{"/health"})))
```

### 6. 用 fasthttp 替代方案（Fiber）

如果**极致性能**是硬需求，Gin 已经不够，可以换 Fiber（基于 fasthttp）。代价是脱离 `net/http` 生态。

---

## 三、Prometheus 指标

给 Gin 加指标监控：

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    httpDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "http_request_duration_seconds",
            Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
        },
        []string{"method", "path", "status"},
    )
)

func Prometheus() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()

        path := c.FullPath()  // ⚠️ 用模板路径，避免高基数
        if path == "" {
            path = "unknown"
        }
        httpDuration.WithLabelValues(
            c.Request.Method,
            path,
            strconv.Itoa(c.Writer.Status()),
        ).Observe(time.Since(start).Seconds())
    }
}
```

**关键**：`c.FullPath()` 返回路由模板（`/user/:id`），而 `c.Request.URL.Path` 是实际路径（`/user/42`）。用后者会导致 metric label 爆炸。

---

## 四、pprof 集成

```go
import _ "net/http/pprof"

// 生产环境绑 localhost，通过 SSH 或 kubectl port-forward 访问
go http.ListenAndServe("localhost:6060", nil)
```

或用 `gin-contrib/pprof` 集成到 Gin 路由：

```go
import "github.com/gin-contrib/pprof"

pprof.Register(r)  // 挂载到 /debug/pprof/*
// 但注意：这会把 pprof 暴露在业务端口上，生产要用中间件保护
```

**推荐**：pprof 单独端口 + `localhost` 绑定，与业务端口分离。

---

## 五、常见性能瓶颈定位

### 场景 1：QPS 上不去

用 wrk 压测：

```bash
wrk -t8 -c100 -d30s http://localhost:8080/api/hello
```

看输出：
- 延迟高 → 用 pprof CPU profile 找热点
- QPS 低但 CPU 未打满 → 有阻塞（DB、外部 API）
- CPU 打满 → 找到最耗 CPU 的函数优化

### 场景 2：内存持续增长

```bash
go tool pprof http://localhost:6060/debug/pprof/heap
(pprof) top
(pprof) list SomeFunc
```

常见问题：
- 全局 map 不清理
- goroutine 泄漏
- 大 slice/string 引用了大底层数组

### 场景 3：goroutine 数暴涨

```bash
curl 'http://localhost:6060/debug/pprof/goroutine?debug=1'
```

找卡在 `chan send` / `chan receive` 的堆栈，一般是没做超时或未 close。

---

## 六、Benchmark 参考

用 `httptest` 做压测（不走网络，看纯路由 + handler 性能）：

```go
func BenchmarkGinRoute(b *testing.B) {
    gin.SetMode(gin.ReleaseMode)
    r := gin.New()
    r.GET("/hello", func(c *gin.Context) {
        c.JSON(200, gin.H{"msg": "hi"})
    })

    req := httptest.NewRequest("GET", "/hello", nil)
    w := httptest.NewRecorder()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        r.ServeHTTP(w, req)
    }
}

// go test -bench=. -benchmem
// BenchmarkGinRoute-8   1000000    1234 ns/op    128 B/op    3 allocs/op
```

**参考数据**（Gin 1.10 + Go 1.22）：
- 简单 JSON 响应：~1μs/op，3-5 次分配
- 带 5 个中间件：~2μs/op
- 路由匹配（10000 条路由）：~200ns

**结论**：Gin 单机 10 万 QPS 是常规水平，20 万 QPS 也可达（简单业务）。

---

## 七、真实案例：优化前后对比

### 优化前（QPS 1.2 万）

```go
r := gin.Default()  // DebugMode
r.Use(cors.Default())
r.POST("/upload", uploadHandler)  // 每次都 json.Unmarshal 大 payload
```

问题：
- DebugMode 有额外开销
- 标准 JSON 库慢
- 无 Gzip
- 没超时

### 优化后（QPS 8 万，6x 提升）

```go
gin.SetMode(gin.ReleaseMode)

r := gin.New()
r.Use(
    middleware.AccessLogAsync(),  // 异步 Zap
    middleware.Recovery(),
    gzip.Gzip(gzip.DefaultCompression),
    cors.Default(),
)

// build with -tags sonic

r.POST("/upload", uploadHandler)

srv := &http.Server{
    Addr:         ":8080",
    Handler:      r,
    ReadTimeout:  10 * time.Second,
    WriteTimeout: 10 * time.Second,
}
srv.ListenAndServe()
```

关键改动：
1. ReleaseMode
2. 异步日志（Zap）
3. sonic 加速 JSON
4. Gzip 压缩
5. 合理超时

---

## 八、面试高频题

### Q1: Gin 为什么快？

三个核心原因：
1. **Radix Tree 路由**：O(L) 匹配，与路由数无关
2. **Context 用 sync.Pool 复用**：减少 GC
3. **无反射的中间件链**：编译期确定的 slice，运行时直接遍历

### Q2: sync.Pool 有什么坑？

- Pool 里的对象**可能随时被 GC 回收**（GC 时清空），不能假设存在
- 归还前**必须重置状态**，否则会污染下一次使用者
- Gin 的 Context.reset() 就是干这个的

### Q3: FullPath() 和 URL.Path 用哪个打指标？

**FullPath**。Prometheus label 基数太高会占用大量内存并变慢。用模板路径把 `/user/1`、`/user/2` 合并成 `/user/:id`。

### Q4: 生产要不要用 Fiber 替代 Gin？

看场景：
- **一般业务**：Gin 够快，生态好，选 Gin
- **极致性能** + 可以脱离 net/http 生态：Fiber
- Fiber 不兼容 `net/http` middleware，中间件生态相对小

### Q5: 一台机器 Gin 能扛多少 QPS？

- 简单 JSON 响应：8 核机器 10-20 万 QPS
- 带 DB 查询：受 DB 限制，通常几千到几万
- 带 Redis 缓存：几万到十几万

QPS 数字不重要，**能否稳定扛住业务峰值 + P99 延迟达标**才是关键。
