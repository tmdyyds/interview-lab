# Gin 路由原理

**标签**: #go #gin #routing #radix-tree #高频

Gin 路由基于 [httprouter](https://github.com/julienschmidt/httprouter) 的 **Radix Tree（基数树）**，是 Gin 快的核心原因之一。

---

## 一、为什么用 Radix Tree

### 对比其他路由匹配方式

| 方式 | 时间复杂度 | 代表框架 |
|------|-----------|---------|
| 遍历比较 | O(N)，N=路由数 | 原生 `net/http` |
| 正则匹配 | O(N × L) | Beego 部分场景 |
| Trie 树 | O(L)，L=路径长度 | 通用 |
| **Radix Tree** | **O(L)** | **Gin / echo** |

**Radix Tree = 压缩前缀树**：把共享前缀合并成一个节点，减少树的深度和内存占用。

---

## 二、Radix Tree 结构示意

假设注册了这些路由：

```
GET /user
GET /user/:id
GET /user/:id/profile
GET /users
GET /users/list
```

Radix Tree 长这样：

```
                      root
                        │
                     /user  ← 公共前缀
                    /    \
                  ""      s        ← /user 和 /users 分叉
                 /  \      \
               ""   /:id    /list  ← /user 和 /user/:id 分叉
                     \
                    /profile
```

每次请求进来，从根节点沿路径匹配，走一条链就到叶子。**没有多余的比较**。

---

## 三、路由注册和匹配

### 注册路由

```go
r := gin.New()
r.GET("/user", listUser)              // 静态路径
r.GET("/user/:id", getUser)           // 命名参数
r.GET("/user/:id/*action", userAction) // 通配符

// 内部：把路径拆成段，插入 Radix Tree
```

### 三种路径类型

| 类型 | 语法 | 匹配示例 |
|------|------|---------|
| 静态 | `/user/list` | 只匹配 `/user/list` |
| 命名参数 | `/user/:id` | `/user/42`、`/user/abc`（一段） |
| 通配符 | `/file/*path` | `/file/a/b/c.txt`（贪婪到底） |

### 优先级规则

同一层节点，Gin 按此顺序匹配：

```
1. 静态路径优先
2. 命名参数 :xxx
3. 通配符 *xxx
```

例如：

```go
r.GET("/user/list", listAll)     // 静态
r.GET("/user/:id", getById)      // 参数

// GET /user/list → 走 listAll（静态优先）
// GET /user/42   → 走 getById（无静态匹配，走参数）
```

---

## 四、路由冲突（启动时 panic）

Gin 在**注册时**就会检测冲突，冲突直接 panic：

```go
r.GET("/user/:id", handler1)
r.GET("/user/:name", handler2)
// panic: ':name' in new path '/user/:name' conflicts with existing wildcard ':id'

r.GET("/user/:id", h1)
r.GET("/user/list", h2)
// panic: '/list' in new path '/user/list' conflicts with existing wildcard ':id'
// （注意：这个在旧版本会 panic，新版 1.7+ 已支持静态优先）
```

**规则**：同一层，命名参数只能有一个（Gin 不支持同层多个参数名）。

---

## 五、分组路由

### 基本用法

```go
r := gin.New()

v1 := r.Group("/api/v1")
{
    v1.GET("/users", listUsers)         // → /api/v1/users
    v1.POST("/users", createUser)       // → /api/v1/users
}
```

**分组只是路径前缀 + 中间件的批量绑定**，没有性能开销。

### 嵌套分组

```go
api := r.Group("/api")
{
    v1 := api.Group("/v1")
    {
        users := v1.Group("/users")
        {
            users.GET("/", listUsers)      // /api/v1/users/
            users.GET("/:id", getUser)     // /api/v1/users/:id
        }
    }
}
```

### 分组中间件

```go
// 全局中间件
r.Use(Logger())

// 分组中间件（只对这个分组生效）
authGroup := r.Group("/admin", AuthMiddleware())
authGroup.GET("/dashboard", dashboard)

// 或用 Use 添加
public := r.Group("/api")
public.Use(RateLimit(), Logger())
{
    public.GET("/hello", hello)
}
```

---

## 六、路由分离：不同 HTTP 方法独立 tree

Gin **每个 HTTP 方法有自己的 Radix Tree**（9 棵树：GET、POST、PUT、DELETE、PATCH、HEAD、OPTIONS、CONNECT、TRACE）。

```go
// 内部结构
type Engine struct {
    trees methodTrees  // 每个方法一棵树
}

type methodTrees []methodTree
type methodTree struct {
    method string
    root   *node  // 树根
}
```

好处：
- `GET /user/:id` 和 `POST /user/:id` 完全独立
- 每棵树都是"轻树"，减少无关分支的干扰
- 匹配时只在对应方法的树里找

---

## 七、常用高级用法

### 处理 404

```go
r.NoRoute(func(c *gin.Context) {
    c.JSON(404, gin.H{"error": "not found", "path": c.Request.URL.Path})
})

// 处理 405（方法不允许）
r.HandleMethodNotAllowed = true  // 需要开启
r.NoMethod(func(c *gin.Context) {
    c.JSON(405, gin.H{"error": "method not allowed"})
})
```

### 静态文件

```go
r.Static("/static", "./public")             // 挂载目录
r.StaticFile("/favicon.ico", "./favicon.ico") // 单文件
r.StaticFS("/assets", http.Dir("./assets"))  // 自定义 FS
```

### 反向获取路由

```go
// 拿到匹配的路由模板（用于埋点、日志）
r.GET("/user/:id", func(c *gin.Context) {
    fmt.Println(c.FullPath())  // "/user/:id"
    fmt.Println(c.Request.URL.Path)  // "/user/42"
})
```

`c.FullPath()` 对 Prometheus 打指标特别有用（避免高基数 `/user/1`、`/user/2` 各是一个 label）。

---

## 八、源码关键点

### Radix Tree 节点

```go
type node struct {
    path      string       // 当前节点的路径片段
    indices   string       // 子节点首字母索引
    wildChild bool         // 是否包含通配符子节点
    nType     nodeType     // 节点类型：static/root/param/catchAll
    priority  uint32       // 优先级（子树中路由数）
    children  []*node      // 子节点
    handlers  HandlersChain // 命中时的 handler 链
    fullPath  string       // 完整路径
}
```

### 插入和匹配

- **插入**：沿共同前缀走，遇分叉则分裂节点
- **匹配**：递归查找，静态优先，参数次之，通配最后
- **优先级排序**：子节点按 `priority` 排序，热门路由匹配更快

---

## 九、性能对比

来自 [go-web-framework-benchmark](https://github.com/smallnest/go-web-framework-benchmark)：

| 框架 | 路由匹配延迟 | QPS |
|------|-------------|-----|
| Fiber (fasthttp) | 极低 | 最高 |
| Gin | 低 | 高 |
| Echo | 低 | 高 |
| Beego | 中 | 中 |
| net/http（默认 mux）| 高 | 低 |

**Gin 的路由性能已经是 Go 生态第一梯队**，普通业务场景下路由匹配几乎不是瓶颈。

---

## 十、常见陷阱

### 陷阱 1：路由顺序不影响匹配

```go
r.GET("/user/:id", getUser)
r.GET("/user/list", listUser)

// GET /user/list 也能匹配到 listUser（静态优先规则，与注册顺序无关）
```

### 陷阱 2：末尾斜杠不同

```go
r.GET("/user", handler1)
r.GET("/user/", handler2)  // 默认会被视作同一路由，可能 panic

// 用 RedirectTrailingSlash 控制
r.RedirectTrailingSlash = false  // 默认 true（自动跳转）
```

### 陷阱 3：大小写敏感

Gin 路由**大小写敏感**：`/User` 和 `/user` 是两个路由。

```go
r.RedirectFixedPath = true  // 自动修正大小写（跳转到规范路径）
```

---

## 十一、面试高频题

### Q1: Gin 的路由为什么快？

基于 Radix Tree（压缩前缀树），时间复杂度 O(L)（L 为路径长度），与路由数量无关。相比原生 `net/http` 的遍历方式（O(N)），QPS 高一个数量级。

### Q2: 路由冲突什么时候检测？

启动时（注册时）就检测，冲突直接 panic。这样把错误提前到开发阶段，不会到运行时才发现。

### Q3: 静态路径和参数路径同时命中怎么选？

静态优先。`/user/list` 和 `/user/:id` 同时存在时，`GET /user/list` 走静态，`GET /user/42` 走参数。

### Q4: 不同 HTTP 方法共用一棵树吗？

不是。Gin 每个 HTTP 方法有独立的 Radix Tree（共 9 棵）。这样每棵树更精简，匹配更快。

### Q5: c.FullPath() 和 c.Request.URL.Path 的区别？

`FullPath()` 是**匹配的路由模板**（`/user/:id`），`URL.Path` 是**实际请求路径**（`/user/42`）。打 Prometheus 指标必须用 FullPath 避免高基数。
