# Gin Context 详解

**标签**: #go #gin #context #高频

**`gin.Context` 是 Gin 的核心**——它封装了请求、响应、参数、错误、中间件流程控制等所有能力。每个请求进来都会创建（或从池中拿）一个 Context。

---

## 一、Context 的结构

```go
type Context struct {
    Request  *http.Request           // 标准库请求
    Writer   ResponseWriter          // 增强的 ResponseWriter
    Params   Params                  // 路径参数
    handlers HandlersChain           // 当前路由的中间件链
    index    int8                    // 当前执行到哪个 handler
    engine   *Engine                 // 引用主引擎
    Keys     map[string]any          // 跨中间件传值（惰性初始化）
    Errors   errorMsgs               // 错误列表
    mu       sync.RWMutex            // 保护 Keys
    // ...
}
```

**核心记住**：Context = 请求 + 响应 + 中间件流程控制 + 上下文数据。

---

## 二、获取请求数据

### 路径参数

```go
r.GET("/user/:id", func(c *gin.Context) {
    id := c.Param("id")       // 字符串
    idInt, _ := strconv.Atoi(id)
})
```

### 查询参数（URL Query）

```go
// GET /search?q=go&page=2

q := c.Query("q")                    // "go"
page := c.DefaultQuery("page", "1")  // "2"

val, ok := c.GetQuery("q")           // val="go", ok=true

// 数组：?tags=go&tags=web
tags := c.QueryArray("tags")         // ["go", "web"]

// map：?filter[name]=jake&filter[age]=28
filters := c.QueryMap("filter")      // {"name":"jake","age":"28"}
```

### 表单参数

```go
// application/x-www-form-urlencoded 或 multipart/form-data

username := c.PostForm("username")
pwd := c.DefaultPostForm("password", "")

val, ok := c.GetPostForm("email")
arr := c.PostFormArray("tags")
mp := c.PostFormMap("meta")
```

### 请求 Header / Cookie

```go
auth := c.GetHeader("Authorization")

// Cookie
sid, err := c.Cookie("session_id")
c.SetCookie("session_id", "abc", 3600, "/", "example.com", true, true)
//          name         value  maxAge path domain              secure httpOnly
```

### 请求体

```go
// 读取原始 body
body, _ := c.GetRawData()  // []byte

// 注意：body 只能读一次，读完后再 Bind 会失败
// 如果需要多次读，用 c.Request.Body 手动处理
```

---

## 三、参数绑定（Bind 系列）

绑定就是**把请求数据自动填充到 struct**。

```go
type LoginReq struct {
    Username string `json:"username" form:"username" binding:"required"`
    Password string `json:"password" form:"password" binding:"required,min=6"`
}

// JSON 请求体
var req LoginReq
c.ShouldBindJSON(&req)

// 表单
c.ShouldBind(&req)  // 根据 Content-Type 自动选择

// URI（路径参数）
type UriReq struct { ID int `uri:"id" binding:"required"` }
var u UriReq
c.ShouldBindUri(&u)

// Query
c.ShouldBindQuery(&req)

// Header
c.ShouldBindHeader(&req)
```

### Bind vs ShouldBind 系列

| 方法 | 失败行为 |
|------|---------|
| `Bind*(&req)` | 自动写 400 + 中止 |
| `ShouldBind*(&req)` | 只返回 error，让你自己决定怎么处理 |

**推荐用 `ShouldBind*`**，因为你可能需要自定义错误响应格式（比如返回业务错误码而不是 HTTP 400）。

```go
if err := c.ShouldBindJSON(&req); err != nil {
    c.JSON(400, gin.H{
        "code": 40001,
        "msg":  "参数错误: " + err.Error(),
    })
    return
}
```

---

## 四、响应

```go
// JSON
c.JSON(200, gin.H{"data": user})

// 优化后的 JSON（更快，无 HTML 转义）
c.PureJSON(200, gin.H{"html": "<b>hi</b>"})

// 缩进 JSON（调试用）
c.IndentedJSON(200, user)

// AsciiJSON（转义 unicode）
c.AsciiJSON(200, gin.H{"name": "中文"})  // → {"name":"\u4e2d\u6587"}

// 其他
c.String(200, "hello %s", name)
c.XML(200, data)
c.YAML(200, data)
c.HTML(200, "index.html", data)
c.Redirect(302, "/login")

// 文件下载
c.File("./file.pdf")
c.FileAttachment("./file.pdf", "download.pdf")

// 空响应
c.Status(204)

// 自定义
c.Data(200, "application/octet-stream", []byte{...})
```

---

## 五、跨中间件传值

**通过 `c.Set` / `c.Get`**：

```go
// 中间件里设置
func AuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        userID := parseTokenAndGetUserID(c)
        c.Set("userID", userID)  // 存进 Context
        c.Next()
    }
}

// Handler 里读取
func getProfile(c *gin.Context) {
    userID, exists := c.Get("userID")
    if !exists {
        c.JSON(401, gin.H{"error": "not logged in"})
        return
    }
    id := userID.(int64)  // 类型断言
    // ...
}

// 便捷方法（类型安全）
c.GetString("name")   // string
c.GetInt("id")        // int
c.GetInt64("id")      // int64
c.GetBool("admin")    // bool
c.GetTime("time")     // time.Time
c.GetDuration("ttl")  // time.Duration
c.MustGet("userID")   // 不存在时 panic
```

---

## 六、错误处理

```go
// 收集错误（不中止流程，用于日志）
c.Error(errors.New("something went wrong"))

// 中止 + 错误响应
c.AbortWithError(500, err)  // 类型是 *Error
c.AbortWithStatusJSON(400, gin.H{"error": "bad request"})

// 在最后的中间件里统一处理错误
func ErrorHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()

        if len(c.Errors) > 0 {
            for _, e := range c.Errors {
                log.Printf("error: %v", e.Err)
            }
        }
    }
}
```

---

## 七、⚠️ 并发陷阱：goroutine 里必须用 `c.Copy()`

**为什么**：Gin 的 Context 是从 `sync.Pool` 里复用的。请求返回后，Context 会被清理并放回池子。**如果你在 goroutine 里继续引用它，可能读到别的请求的数据**。

```go
// ❌ 错误
r.GET("/async", func(c *gin.Context) {
    go func() {
        time.Sleep(5 * time.Second)
        userID := c.GetInt("userID")  // ⚠️ 可能是别的请求的 userID
        doSomething(userID)
    }()
    c.JSON(200, gin.H{"status": "ok"})
})

// ✅ 正确：用 c.Copy()
r.GET("/async", func(c *gin.Context) {
    cCp := c.Copy()  // 复制一份只读的 Context
    go func() {
        time.Sleep(5 * time.Second)
        userID := cCp.GetInt("userID")  // 安全
        doSomething(userID)
    }()
    c.JSON(200, gin.H{"status": "ok"})
})
```

### c.Copy() 会复制什么

| 字段 | 是否复制 |
|------|---------|
| Request | ✅ 引用同一个 |
| Writer | ❌ 设为 nil（不能在 goroutine 里响应） |
| Params | ✅ 引用 |
| handlers | ❌ 清空（不能再执行中间件链） |
| Keys | ✅ 深复制 |

所以 `c.Copy()` 拿到的 Context **只能读**，不能再往响应写数据。如果 goroutine 里要响应，用别的机制（如 SSE、WebSocket）。

---

## 八、Context 与标准库的桥接

Gin 的 `*gin.Context` **实现了 `context.Context` 接口**。可以直接传给需要 `context.Context` 的地方：

```go
r.GET("/query", func(c *gin.Context) {
    // c 可以直接作为 context.Context 用
    result, err := db.QueryContext(c, "SELECT ...")  // ✅

    // 客户端断开时 c.Done() 会收到信号
    select {
    case <-c.Done():
        log.Println("client disconnected")
    default:
    }
})

// 手动派生带超时的 context
func handler(c *gin.Context) {
    ctx, cancel := context.WithTimeout(c, 3*time.Second)
    defer cancel()

    result, err := slowQuery(ctx)
    // ...
}
```

**注意**：`c.Value(key)` 用的是 Gin 自己的 Keys，不是 `context.WithValue` 的机制。混用要小心。

---

## 九、常用方法速查

### 请求信息

```go
c.ClientIP()          // 客户端 IP（考虑 X-Forwarded-For）
c.RemoteIP()          // 直连 IP
c.ContentType()       // Content-Type
c.FullPath()          // 匹配到的路由模板：/user/:id
c.Request.URL.Path    // 实际路径：/user/42
c.Request.Method      // "GET"
c.Request.Host        // "example.com:8080"
```

### 响应控制

```go
c.IsAborted()         // 是否已中止
c.Writer.Status()     // 已设置的状态码
c.Writer.Size()       // 已写入的字节数
c.Writer.Written()    // 是否已开始写
```

### 中间件流程

```go
c.Next()      // 执行下一个 handler（中间件）
c.Abort()     // 阻止后续 handler
c.AbortWithStatus(401)
```

---

## 十、面试高频题

### Q1: Context 为什么用 sync.Pool？

减少每个请求都创建 Context 的 GC 压力。QPS 越高，节省越明显。

### Q2: 为什么在 goroutine 里必须用 c.Copy()？

Context 会被 `sync.Pool` 复用。请求结束后放回池，下一个请求可能拿到同一个对象。原始 Context 被"重置"后，goroutine 里再读就是别的数据。

### Q3: Bind 和 ShouldBind 的区别？

`Bind` 失败时自动写 400 并 Abort。`ShouldBind` 只返回 error，让你决定怎么响应（生产推荐）。

### Q4: c.Set / c.Get 是并发安全的吗？

是的。内部有 `sync.RWMutex` 保护 Keys map。

### Q5: 如何在中间件里修改响应？

用 `bytes.Buffer` 替换 `c.Writer`，等 handler 执行完再改写。参考 Gzip 中间件的实现。
