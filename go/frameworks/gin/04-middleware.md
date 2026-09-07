# Gin 中间件

**标签**: #go #gin #middleware #高频

中间件是 Gin 最强大的机制之一，用于**在请求前后统一处理逻辑**（日志、鉴权、跨域、限流、监控等）。

---

## 一、中间件本质

中间件就是一个 `gin.HandlerFunc`，签名就是普通 handler：

```go
type HandlerFunc func(*gin.Context)
```

**普通 handler 也是中间件**——它们本质是同一种东西，都是 handler 链中的一环。

---

## 二、洋葱模型

Gin 中间件执行是**递归调用**的：中间件 A 里调用 `c.Next()` 才会进入 B，B 执行完（或再 Next 后）才回到 A 的剩余代码。

```
Request 进入
    ↓
┌─── Middleware A（前置） ───┐
│        ↓                    │
│   ┌─── Middleware B ───┐    │
│   │        ↓             │    │
│   │   ┌── Handler ──┐   │    │
│   │   │  业务逻辑    │   │    │
│   │   └─────────────┘   │    │
│   │        ↑             │    │
│   └─── Middleware B（后置）  │
│        ↑                    │
└─── Middleware A（后置） ───┘
    ↓
Response 返回
```

---

## 三、自定义中间件

### 最简单的中间件

```go
func Logger() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()

        c.Next()  // ⚠️ 执行后续中间件和 handler

        latency := time.Since(start)
        status := c.Writer.Status()
        log.Printf("[%s] %s %d %v", c.Request.Method, c.Request.URL.Path, status, latency)
    }
}

// 使用
r := gin.New()
r.Use(Logger())
```

### 只有前置逻辑

```go
func RequestID() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Set("request_id", uuid.NewString())
        // 不调 c.Next()，Gin 自动往下走（实际内部会 Next）
    }
}
```

**注意**：如果没写 `c.Next()`，Gin 会在中间件返回后自动调用下一个。但**前后置分离**的场景必须显式 `c.Next()`。

### 前置 + 后置

```go
func Metrics() gin.HandlerFunc {
    return func(c *gin.Context) {
        // ↓ 前置
        start := time.Now()
        method := c.Request.Method
        path := c.FullPath()

        c.Next()  // ⚠️ 分界点

        // ↓ 后置
        latency := time.Since(start).Seconds()
        status := strconv.Itoa(c.Writer.Status())

        requestsTotal.WithLabelValues(method, path, status).Inc()
        requestDuration.WithLabelValues(method, path).Observe(latency)
    }
}
```

---

## 四、`c.Next()` vs `c.Abort()`

### c.Next()

**执行后续 handler 链，等它们全跑完再回来**。

```go
func A() gin.HandlerFunc {
    return func(c *gin.Context) {
        fmt.Println("A before")
        c.Next()
        fmt.Println("A after")
    }
}

func B() gin.HandlerFunc {
    return func(c *gin.Context) {
        fmt.Println("B before")
        c.Next()
        fmt.Println("B after")
    }
}

r.Use(A(), B()).GET("/hello", func(c *gin.Context) {
    fmt.Println("handler")
})

// 输出：
// A before
// B before
// handler
// B after
// A after
```

### c.Abort()

**阻止后续 handler 执行**（当前 handler 还会执行完）。

```go
func Auth() gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.GetHeader("Authorization") == "" {
            c.JSON(401, gin.H{"error": "unauthorized"})
            c.Abort()  // 阻止后续
            return     // ✅ 建议加，避免继续执行下面的代码
        }
        c.Next()
    }
}
```

### 便捷组合

```go
c.AbortWithStatus(401)           // Abort + Status
c.AbortWithStatusJSON(401, gin.H{...})  // Abort + JSON
c.AbortWithError(500, err)       // Abort + 记录错误
```

### 检查是否已中止

```go
if c.IsAborted() {
    return
}
```

---

## 五、注册中间件的三种方式

### 全局中间件

```go
r := gin.New()
r.Use(Logger(), Recovery())
// 之后注册的所有路由都会经过这两个中间件
```

### 分组中间件

```go
authGroup := r.Group("/admin", AuthMiddleware())
{
    authGroup.GET("/users", listUsers)     // 会经过 AuthMiddleware
    authGroup.POST("/users", createUser)
}
```

### 单路由中间件

```go
r.GET("/hello", Logger(), RateLimit(), helloHandler)
// 只有 /hello 会经过 Logger 和 RateLimit
```

---

## 六、常用中间件示例

### 1. 认证（JWT）

```go
func JWTAuth(secret string) gin.HandlerFunc {
    return func(c *gin.Context) {
        auth := c.GetHeader("Authorization")
        if !strings.HasPrefix(auth, "Bearer ") {
            c.AbortWithStatusJSON(401, gin.H{"error": "missing token"})
            return
        }
        tokenStr := strings.TrimPrefix(auth, "Bearer ")

        token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
            return []byte(secret), nil
        })
        if err != nil || !token.Valid {
            c.AbortWithStatusJSON(401, gin.H{"error": "invalid token"})
            return
        }

        claims := token.Claims.(jwt.MapClaims)
        c.Set("user_id", claims["sub"])
        c.Next()
    }
}
```

### 2. CORS 跨域

```go
func CORS() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Header("Access-Control-Allow-Origin", "*")
        c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
        c.Header("Access-Control-Allow-Headers", "Origin,Content-Type,Authorization")
        c.Header("Access-Control-Allow-Credentials", "true")

        if c.Request.Method == "OPTIONS" {
            c.AbortWithStatus(204)
            return
        }
        c.Next()
    }
}

// 或用官方库
// go get github.com/gin-contrib/cors
// r.Use(cors.Default())
```

### 3. 限流

```go
import "golang.org/x/time/rate"

func RateLimit(rps int) gin.HandlerFunc {
    limiter := rate.NewLimiter(rate.Limit(rps), rps*2)
    return func(c *gin.Context) {
        if !limiter.Allow() {
            c.AbortWithStatusJSON(429, gin.H{"error": "too many requests"})
            return
        }
        c.Next()
    }
}
```

### 4. Recovery（捕获 panic）

Gin 自带 `gin.Recovery()`，但生产上一般自定义：

```go
func Recovery() gin.HandlerFunc {
    return func(c *gin.Context) {
        defer func() {
            if r := recover(); r != nil {
                stack := debug.Stack()
                log.Printf("panic: %v\n%s", r, stack)
                c.AbortWithStatusJSON(500, gin.H{"error": "internal server error"})
            }
        }()
        c.Next()
    }
}
```

### 5. TraceID

```go
func TraceID() gin.HandlerFunc {
    return func(c *gin.Context) {
        traceID := c.GetHeader("X-Trace-ID")
        if traceID == "" {
            traceID = uuid.NewString()
        }
        c.Set("trace_id", traceID)
        c.Header("X-Trace-ID", traceID)  // 响应也带上
        c.Next()
    }
}
```

### 6. 请求日志（结构化）

```go
func AccessLog(logger *zap.Logger) gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()

        logger.Info("access",
            zap.String("method", c.Request.Method),
            zap.String("path", c.FullPath()),
            zap.Int("status", c.Writer.Status()),
            zap.Duration("latency", time.Since(start)),
            zap.String("ip", c.ClientIP()),
            zap.String("trace_id", c.GetString("trace_id")),
        )
    }
}
```

### 7. 修改响应体（Gzip 类）

需要"拦截"响应，比较复杂：

```go
type gzipWriter struct {
    gin.ResponseWriter
    writer *gzip.Writer
}

func (g *gzipWriter) Write(data []byte) (int, error) {
    return g.writer.Write(data)
}

func Gzip() gin.HandlerFunc {
    return func(c *gin.Context) {
        if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
            c.Next()
            return
        }

        c.Header("Content-Encoding", "gzip")
        gz := gzip.NewWriter(c.Writer)
        defer gz.Close()

        c.Writer = &gzipWriter{c.Writer, gz}
        c.Next()
    }
}
```

生产环境推荐用 `github.com/gin-contrib/gzip`。

---

## 七、中间件执行顺序

```go
r := gin.New()
r.Use(A, B)         // 全局中间件

api := r.Group("/api", C)  // 分组中间件
api.GET("/hello", D, handler)  // 单路由中间件 + handler
```

**执行顺序**：`A → B → C → D → handler → D → C → B → A`

（前置从外到内，后置从内到外。因为是洋葱模型）

---

## 八、中间件常见陷阱

### 陷阱 1：忘记 c.Next() 导致后置逻辑失效

```go
func Timing() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        // ❌ 没写 c.Next()，但 Gin 会自动往下走
        // 但下面的代码在 handler 前就执行了！
        latency := time.Since(start)  // 永远接近 0
        log.Println(latency)
    }
}

// ✅ 分离前后置必须显式 Next
func Timing() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()  // 关键：等 handler 跑完
        log.Println(time.Since(start))
    }
}
```

### 陷阱 2：Abort 后没 return

```go
func Auth() gin.HandlerFunc {
    return func(c *gin.Context) {
        if !authOK(c) {
            c.AbortWithStatusJSON(401, gin.H{"error": "no auth"})
            // ❌ 没 return，下面的代码还会执行
        }
        // 这里还会跑，可能覆盖响应
        c.Set("user_id", userID)
    }
}
```

`Abort()` 只是阻止**后续 handler**执行，**当前 handler 的剩余代码还会跑**。要加 `return`。

### 陷阱 3：中间件里用 goroutine 忘 Copy

```go
func LogAsync() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()
        go func() {
            userID := c.GetInt("user_id")  // ❌ Context 可能已被复用
            report(userID)
        }()
    }
}

// ✅
func LogAsync() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()
        cp := c.Copy()  // 复制
        go func() {
            report(cp.GetInt("user_id"))
        }()
    }
}
```

### 陷阱 4：中间件顺序错误

```go
r.Use(Recovery(), Logger())  // ❌ Recovery 在前，Logger 拦不到 panic 的日志

r.Use(Logger(), Recovery())  // ✅ Logger 在最外层
```

**原则**：Recovery 尽量靠内（handler 侧），Logger 尽量靠外（记录所有情况）。

---

## 九、面试高频题

### Q1: 中间件执行顺序是什么？

**洋葱模型**：全局 → 分组 → 单路由，前置从外到内，后置从内到外。递归调用，`c.Next()` 是分界点。

### Q2: c.Next() 和 c.Abort() 有什么区别？

- `Next`：主动执行后续 handler 链，返回后继续本 handler 剩余代码
- `Abort`：标记流程中止，**后续 handler 不再执行**，但当前 handler 的剩余代码继续执行（所以要加 `return`）

### Q3: 中间件里能修改响应吗？

可以，但要在 `c.Next()` 之前，或者用自定义 `ResponseWriter` 拦截。一旦 `c.Writer` 开始写响应，Header 就锁定了。

### Q4: 如何写一个"只对某些路由生效"的中间件？

方式一：只注册到特定分组或路由。
方式二：中间件内部判断路径：

```go
func SelectiveAuth() gin.HandlerFunc {
    return func(c *gin.Context) {
        if strings.HasPrefix(c.Request.URL.Path, "/public") {
            c.Next()
            return
        }
        // 鉴权逻辑
    }
}
```

### Q5: 中间件里 panic 会怎样？

如果没有 `Recovery`，整个进程崩溃。所以 `gin.Default()` 默认加了 `Recovery()`。生产上建议自定义 `Recovery` 记录堆栈 + 上报监控。
