# Gin 快速入门

**标签**: #go #gin #basics

---

## 一、安装

```bash
go mod init myapp
go get -u github.com/gin-gonic/gin
```

---

## 二、Hello World

```go
package main

import "github.com/gin-gonic/gin"

func main() {
    r := gin.Default()  // 带 Logger + Recovery 中间件
    // r := gin.New()   // 空引擎（无中间件）

    r.GET("/ping", func(c *gin.Context) {
        c.JSON(200, gin.H{"message": "pong"})
    })

    r.Run(":8080")  // 默认 :8080
}
```

访问 `http://localhost:8080/ping` 得到 `{"message":"pong"}`。

**gin.H** 是 `map[string]any` 的别名，语法糖。

---

## 三、路由方法

Gin 支持所有 HTTP 方法：

```go
r.GET("/users", listUsers)
r.POST("/users", createUser)
r.PUT("/users/:id", updateUser)
r.PATCH("/users/:id", patchUser)
r.DELETE("/users/:id", deleteUser)
r.HEAD("/users", headUsers)
r.OPTIONS("/users", optionsUsers)

// 任意方法
r.Any("/callback", callback)

// 匹配多种方法
r.Handle("GET", "/foo", handler)
```

---

## 四、路径参数

### 命名参数 `:name`

```go
r.GET("/user/:id", func(c *gin.Context) {
    id := c.Param("id")
    c.String(200, "user id: %s", id)
})

// GET /user/42 → "user id: 42"
```

### 通配符 `*action`

```go
r.GET("/user/:name/*action", func(c *gin.Context) {
    name := c.Param("name")
    action := c.Param("action")  // 包含前导 /
    c.String(200, "%s %s", name, action)
})

// GET /user/jake/edit    → "jake /edit"
// GET /user/jake/profile → "jake /profile"
```

**规则**：`:` 是单段匹配，`*` 匹配到路径末尾（贪婪）。

---

## 五、查询参数

```go
r.GET("/search", func(c *gin.Context) {
    // GET /search?q=golang&page=2
    q := c.Query("q")                 // "golang"
    page := c.DefaultQuery("page", "1") // "2"（有默认值）

    // 判断是否存在
    if val, ok := c.GetQuery("q"); ok {
        c.String(200, "search: %s", val)
    }
})
```

---

## 六、表单参数

```go
r.POST("/login", func(c *gin.Context) {
    // Content-Type: application/x-www-form-urlencoded
    username := c.PostForm("username")
    password := c.DefaultPostForm("password", "")

    // 表单文件上传（multipart/form-data）
    file, _ := c.FormFile("avatar")
    c.SaveUploadedFile(file, "./uploads/"+file.Filename)

    c.JSON(200, gin.H{"user": username})
})
```

---

## 七、JSON 请求体

```go
type LoginReq struct {
    Username string `json:"username" binding:"required"`
    Password string `json:"password" binding:"required,min=6"`
}

r.POST("/login", func(c *gin.Context) {
    var req LoginReq
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    c.JSON(200, gin.H{"user": req.Username})
})
```

---

## 八、响应类型

```go
// JSON
c.JSON(200, gin.H{"code": 0, "msg": "ok"})

// XML
c.XML(200, gin.H{"code": 0})

// YAML
c.YAML(200, gin.H{"code": 0})

// String
c.String(200, "hello %s", name)

// HTML（需要先 LoadHTMLGlob）
r.LoadHTMLGlob("templates/*")
c.HTML(200, "index.html", gin.H{"title": "Home"})

// 文件
c.File("./static/logo.png")
c.FileAttachment("./report.pdf", "report.pdf")  // 强制下载

// 重定向
c.Redirect(302, "/login")

// 只写状态
c.Status(204)

// 自定义 Header
c.Header("X-Trace-ID", "abc123")

// 流式响应（SSE）
c.Stream(func(w io.Writer) bool {
    fmt.Fprint(w, "event: message\ndata: hello\n\n")
    return true  // 返回 false 结束
})
```

---

## 九、分组路由

```go
r := gin.Default()

// v1 分组
v1 := r.Group("/api/v1")
{
    v1.GET("/users", listUsers)
    v1.POST("/users", createUser)
    v1.GET("/users/:id", getUser)
}

// v2 分组，可以有独立中间件
v2 := r.Group("/api/v2", AuthMiddleware())
{
    v2.GET("/users", listUsersV2)
}

// 嵌套分组
admin := r.Group("/admin")
admin.Use(AdminAuth())
{
    users := admin.Group("/users")
    {
        users.GET("/", adminListUsers)
        users.DELETE("/:id", adminDeleteUser)
    }
}
```

---

## 十、常用启动方式

```go
// 默认 8080
r.Run()

// 指定端口
r.Run(":9000")

// 指定 IP + 端口
r.Run("127.0.0.1:9000")

// HTTPS
r.RunTLS(":443", "./cert.pem", "./key.pem")

// Unix Socket
r.RunUnix("/tmp/app.sock")

// 用标准的 http.Server（更多控制）
srv := &http.Server{
    Addr:         ":8080",
    Handler:      r,
    ReadTimeout:  10 * time.Second,
    WriteTimeout: 10 * time.Second,
}
srv.ListenAndServe()
```

---

## 十一、模式设置

```go
gin.SetMode(gin.DebugMode)    // 默认，打印详细日志
gin.SetMode(gin.ReleaseMode)  // 生产，关闭 debug 日志
gin.SetMode(gin.TestMode)     // 测试

// 通过环境变量
// GIN_MODE=release ./myapp
```

---

## 十二、一个完整的 Demo

```go
package main

import (
    "net/http"
    "time"

    "github.com/gin-gonic/gin"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name" binding:"required"`
    Age  int    `json:"age"  binding:"gte=0,lte=150"`
}

var users = []User{
    {ID: 1, Name: "Jake", Age: 28},
    {ID: 2, Name: "Alice", Age: 25},
}

func main() {
    gin.SetMode(gin.ReleaseMode)
    r := gin.Default()

    api := r.Group("/api/v1")
    {
        api.GET("/users", listUsers)
        api.GET("/users/:id", getUser)
        api.POST("/users", createUser)
    }

    srv := &http.Server{
        Addr:         ":8080",
        Handler:      r,
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 10 * time.Second,
    }
    srv.ListenAndServe()
}

func listUsers(c *gin.Context) {
    c.JSON(200, gin.H{"data": users, "total": len(users)})
}

func getUser(c *gin.Context) {
    id := c.Param("id")
    for _, u := range users {
        if fmt.Sprint(u.ID) == id {
            c.JSON(200, u)
            return
        }
    }
    c.JSON(404, gin.H{"error": "not found"})
}

func createUser(c *gin.Context) {
    var u User
    if err := c.ShouldBindJSON(&u); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    u.ID = len(users) + 1
    users = append(users, u)
    c.JSON(201, u)
}
```
