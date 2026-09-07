# Gin 生产架构设计

**标签**: #go #gin #architecture #分层

Gin 本身只是 Web 框架，生产级项目需要一套清晰的架构。这里给出一份**中大型项目的推荐分层**。

---

## 一、推荐目录结构

```
myapp/
├── cmd/
│   └── server/
│       └── main.go              ← 入口：只做组装和启动
│
├── internal/                    ← 项目私有代码
│   ├── config/
│   │   └── config.go            ← 配置结构 + 加载
│   ├── middleware/
│   │   ├── auth.go
│   │   ├── cors.go
│   │   ├── logger.go
│   │   └── recovery.go
│   ├── router/
│   │   └── router.go            ← 路由注册
│   ├── handler/                 ← 控制器层（薄）
│   │   ├── user.go
│   │   └── order.go
│   ├── service/                 ← 业务逻辑层（核心）
│   │   ├── user.go
│   │   └── order.go
│   ├── repository/              ← 数据访问层
│   │   ├── user.go
│   │   └── order.go
│   ├── model/                   ← 数据模型
│   │   ├── user.go
│   │   └── order.go
│   ├── dto/                     ← 请求/响应结构
│   │   └── user.go
│   └── errcode/                 ← 业务错误码
│       └── code.go
│
├── pkg/                         ← 可对外复用的公共包
│   ├── logger/
│   ├── response/
│   └── validator/
│
├── configs/
│   ├── config.dev.yaml
│   └── config.prod.yaml
│
├── scripts/
├── docs/
├── go.mod
└── Makefile
```

---

## 二、分层职责

### Handler 层（薄）

**只做三件事**：接收参数、调用 service、返回响应。

```go
// internal/handler/user.go
type UserHandler struct {
    svc *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
    return &UserHandler{svc: svc}
}

func (h *UserHandler) Create(c *gin.Context) {
    var req dto.CreateUserReq
    if err := c.ShouldBindJSON(&req); err != nil {
        response.BadRequest(c, err)
        return
    }

    user, err := h.svc.Create(c, &req)
    if err != nil {
        response.Fail(c, err)
        return
    }

    response.OK(c, user)
}
```

**不要在 Handler 里写业务逻辑**。

### Service 层（核心）

业务逻辑集中在这里，编排 repository、外部服务、缓存等。

```go
// internal/service/user.go
type UserService struct {
    repo   *repository.UserRepo
    cache  *cache.Cache
    logger *zap.Logger
}

func NewUserService(repo *repository.UserRepo, cache *cache.Cache, logger *zap.Logger) *UserService {
    return &UserService{repo: repo, cache: cache, logger: logger}
}

func (s *UserService) Create(ctx context.Context, req *dto.CreateUserReq) (*model.User, error) {
    // 校验业务规则
    if exists, _ := s.repo.ExistsByEmail(ctx, req.Email); exists {
        return nil, errcode.ErrEmailDuplicate
    }

    // 业务对象组装
    user := &model.User{
        Name:  req.Name,
        Email: req.Email,
    }
    hashed, _ := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
    user.Password = string(hashed)

    // 持久化
    if err := s.repo.Create(ctx, user); err != nil {
        s.logger.Error("create user failed", zap.Error(err))
        return nil, errcode.ErrInternal
    }

    // 副作用（如发消息、清缓存）
    s.cache.Del(ctx, "user:list")

    return user, nil
}
```

### Repository 层

**只负责数据存取**，不含业务规则。

```go
// internal/repository/user.go
type UserRepo struct {
    db *gorm.DB
}

func NewUserRepo(db *gorm.DB) *UserRepo {
    return &UserRepo{db: db}
}

func (r *UserRepo) Create(ctx context.Context, u *model.User) error {
    return r.db.WithContext(ctx).Create(u).Error
}

func (r *UserRepo) FindByID(ctx context.Context, id int64) (*model.User, error) {
    var u model.User
    err := r.db.WithContext(ctx).First(&u, id).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, errcode.ErrUserNotFound
    }
    return &u, err
}

func (r *UserRepo) ExistsByEmail(ctx context.Context, email string) (bool, error) {
    var count int64
    err := r.db.WithContext(ctx).Model(&model.User{}).Where("email = ?", email).Count(&count).Error
    return count > 0, err
}
```

### DTO 层（Data Transfer Object）

请求和响应结构，独立于 model：

```go
// internal/dto/user.go
type CreateUserReq struct {
    Name     string `json:"name"     binding:"required,min=2,max=50"`
    Email    string `json:"email"    binding:"required,email"`
    Password string `json:"password" binding:"required,min=8"`
}

type UserResp struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
    // 注意：不返回 Password
}

func ToUserResp(u *model.User) *UserResp {
    return &UserResp{ID: u.ID, Name: u.Name, Email: u.Email}
}
```

**为什么 DTO 和 Model 分开**：Model 是数据库实体，DTO 是接口契约。数据库字段改了不应该影响 API 响应结构。

---

## 三、统一响应结构

```go
// pkg/response/response.go
type Response struct {
    Code int    `json:"code"`
    Msg  string `json:"msg"`
    Data any    `json:"data,omitempty"`
}

func OK(c *gin.Context, data any) {
    c.JSON(200, &Response{Code: 0, Msg: "success", Data: data})
}

func Fail(c *gin.Context, err error) {
    var be *errcode.BizError
    if errors.As(err, &be) {
        c.JSON(200, &Response{Code: be.Code, Msg: be.Msg})
        return
    }
    c.JSON(500, &Response{Code: 500, Msg: "internal error"})
}

func BadRequest(c *gin.Context, err error) {
    c.JSON(200, &Response{Code: 400, Msg: err.Error()})
}
```

---

## 四、业务错误码

```go
// internal/errcode/code.go
type BizError struct {
    Code int
    Msg  string
}

func (e *BizError) Error() string { return e.Msg }

func New(code int, msg string) *BizError {
    return &BizError{Code: code, Msg: msg}
}

var (
    ErrUserNotFound    = New(10001, "用户不存在")
    ErrEmailDuplicate  = New(10002, "邮箱已注册")
    ErrPasswordInvalid = New(10003, "密码错误")
    ErrInternal        = New(50000, "系统繁忙")
)
```

---

## 五、依赖注入

### 手动 DI（简单项目）

```go
// cmd/server/main.go
func main() {
    cfg := config.Load()
    logger := logger.New(cfg.Log)
    db := database.New(cfg.DB)
    rdb := redis.New(cfg.Redis)
    cache := cache.New(rdb)

    // 从底往上组装
    userRepo := repository.NewUserRepo(db)
    userSvc := service.NewUserService(userRepo, cache, logger)
    userHandler := handler.NewUserHandler(userSvc)

    // 注册路由
    r := router.New(userHandler)

    // 启动
    srv := &http.Server{Addr: cfg.Addr, Handler: r}
    srv.ListenAndServe()
}
```

### 用 Wire（推荐大项目）

```go
// wire.go
//go:build wireinject

func InitializeUserHandler(cfg *config.Config) (*handler.UserHandler, error) {
    wire.Build(
        database.New,
        redis.New,
        cache.New,
        logger.New,
        repository.NewUserRepo,
        service.NewUserService,
        handler.NewUserHandler,
    )
    return nil, nil
}

// 运行：wire ./...
// 会自动生成 wire_gen.go
```

---

## 六、路由组织

```go
// internal/router/router.go
func New(userHandler *handler.UserHandler, orderHandler *handler.OrderHandler) *gin.Engine {
    r := gin.New()
    r.Use(
        middleware.Logger(),
        middleware.Recovery(),
        middleware.TraceID(),
        middleware.CORS(),
    )

    r.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "ok"})
    })

    api := r.Group("/api/v1")
    {
        api.POST("/register", userHandler.Register)
        api.POST("/login", userHandler.Login)

        auth := api.Group("", middleware.JWTAuth())
        {
            auth.GET("/user/profile", userHandler.Profile)
            auth.POST("/orders", orderHandler.Create)
        }
    }

    return r
}
```

---

## 七、优雅关闭

```go
// cmd/server/main.go
func main() {
    // 组装省略...

    srv := &http.Server{
        Addr:         cfg.Addr,
        Handler:      r,
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 10 * time.Second,
    }

    // 启动
    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Fatal("listen failed", zap.Error(err))
        }
    }()
    logger.Info("server started", zap.String("addr", cfg.Addr))

    // 等待信号
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    logger.Info("shutdown signal received")

    // 30 秒内优雅关闭
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        logger.Error("forced shutdown", zap.Error(err))
    }

    // 关闭其他资源
    db.Close()
    rdb.Close()
    logger.Info("server exited")
}
```

---

## 八、配置管理

```go
// internal/config/config.go
type Config struct {
    App   AppConfig
    DB    DBConfig
    Redis RedisConfig
    Log   LogConfig
}

type AppConfig struct {
    Env  string `mapstructure:"env"`
    Addr string `mapstructure:"addr"`
}

func Load(path string) *Config {
    viper.SetConfigFile(path)
    viper.AutomaticEnv()  // 允许环境变量覆盖
    if err := viper.ReadInConfig(); err != nil {
        panic(err)
    }
    var cfg Config
    viper.Unmarshal(&cfg)
    return &cfg
}
```

`configs/config.prod.yaml`：

```yaml
app:
  env: production
  addr: :8080
db:
  dsn: user:pass@tcp(mysql:3306)/mydb
redis:
  addr: redis:6379
log:
  level: info
```

---

## 九、完整调用链示例

一个"创建用户"请求的完整流转：

```
[请求] POST /api/v1/register  { "name":"Jake", "email":"j@x.com", "password":"12345678" }
    ↓
[Middleware] Logger → Recovery → TraceID → CORS
    ↓
[Router] 匹配到 userHandler.Register
    ↓
[Handler] userHandler.Register(c)
    ↓ ShouldBindJSON → dto.CreateUserReq
    ↓ 调用 service
[Service] userSvc.Create(ctx, req)
    ↓ 检查邮箱是否存在 → repo.ExistsByEmail
    ↓ 加密密码
    ↓ 持久化 → repo.Create
    ↓ 清缓存 → cache.Del
    ↓ 返回 *model.User
[Handler] dto.ToUserResp → response.OK(c, resp)
    ↓
[Middleware 后置] Logger 记录访问日志
    ↓
[响应] 200 { "code":0, "msg":"success", "data":{...} }
```

---

## 十、常见架构反模式

### 反模式 1：Fat Handler

```go
// ❌ Handler 里塞满业务逻辑
func Register(c *gin.Context) {
    var req dto.CreateUserReq
    c.ShouldBindJSON(&req)

    // 直接查 DB
    var count int64
    db.Model(&User{}).Where("email = ?", req.Email).Count(&count)
    if count > 0 { ... }

    // 加密、创建、发邮件全在这
    hashed, _ := bcrypt.GenerateFromPassword(...)
    db.Create(&User{Name: req.Name, Password: hashed})
    sendEmail(req.Email)

    c.JSON(200, ...)
}
```

问题：难测试、难复用、SRP 违反。

### 反模式 2：Service 依赖 gin.Context

```go
// ❌ Service 层依赖 Web 框架
func (s *UserService) Create(c *gin.Context, req *dto.Req) error {
    ip := c.ClientIP()  // ⚠️
    // ...
}
```

问题：Service 应该框架无关（可能被 gRPC 或定时任务复用）。传 `context.Context`，需要的信息在 Handler 层从 Context 提取好再传。

### 反模式 3：DTO 和 Model 混用

```go
// ❌ 直接用 GORM Model 作为响应
type User struct {
    ID       int64
    Name     string
    Password string  // ⚠️ API 里返回密码
    gorm.Model       // ⚠️ 内部字段全暴露
}
```

问题：结构耦合、字段泄漏、演进困难。

---

## 十一、面试高频题

### Q1: 为什么要分 Handler / Service / Repository？

- Handler：适配 HTTP 协议，改成 gRPC 只改这层
- Service：核心业务逻辑，可测、可复用
- Repository：数据存取，换 DB 只改这层
- **关注点分离**，每层职责单一，可测试性和可维护性好

### Q2: Service 层能直接返回 gin.H 吗？

不能。Service 层要框架无关，返回业务对象（`*model.User`、`error`），由 Handler 转成响应格式。

### Q3: 依赖注入用 Wire 还是 fx？

- Wire：编译期生成代码，无运行时开销，Google 出品
- fx：运行时反射，灵活，Uber 出品

小项目手动 DI 也够。中大型推荐 Wire。

### Q4: 全局变量存放 db、redis 好吗？

不推荐。全局变量导致：
- 单元测试难 mock
- 隐式依赖，看代码看不出用了什么
- 初始化顺序难控

正确做法：把依赖当参数传入，用 DI 组装。

### Q5: 错误码是用 HTTP 状态码还是业务码？

生产推荐：**HTTP 状态码 + 业务码双层**。
- HTTP 状态码：客户端判断成功/失败的粗粒度（200 / 4xx / 5xx）
- 业务码：response body 里的 `code`，具体业务含义

也有团队选择"HTTP 一律 200，靠业务码区分"，各有利弊。
