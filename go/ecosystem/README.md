# Go 常用包与库

**标签**: #go #ecosystem #libraries #高频

按用途分类整理 Go 常用的标准库和第三方库，每个都附**用途、代表场景、简单示例**。

## 深度专题（面试高频，必读）

面试常问、生产必用的库，单独做了深度剖析：

- **[Zap 深度剖析](./zap-deep.md)** — 为什么快 20 倍、Field API、缓冲池、生产陷阱
- **[GORM 深度剖析](./gorm-deep.md)** — Session 机制、N+1、事务传播、10+ 陷阱
- **[go-redis 深度剖析](./go-redis-deep.md)** — 连接池、Pipeline、Lua、Cluster 路由
- **[errgroup + singleflight 深度剖析](./errgroup-singleflight-deep.md)** — 源码、模式、坑
- **[Prometheus + OpenTelemetry 深度剖析](./observability-deep.md)** — 4 种指标、Cardinality、W3C Trace Context

- **[Gin 深度专题](../frameworks/gin/README.md)** — 已有独立目录（8 篇）

---

## 目录

1. [Web 框架](#1-web-框架)
2. [数据库 & ORM](#2-数据库--orm)
3. [Redis 客户端](#3-redis-客户端)
4. [消息队列](#4-消息队列)
5. [日志](#5-日志)
6. [配置管理](#6-配置管理)
7. [并发工具](#7-并发工具)
8. [HTTP 客户端 & 工具](#8-http-客户端--工具)
9. [序列化](#9-序列化)
10. [参数校验](#10-参数校验)
11. [依赖注入](#11-依赖注入)
12. [微服务框架](#12-微服务框架)
13. [RPC](#13-rpc)
14. [可观测性](#14-可观测性)
15. [CLI 工具](#15-cli-工具)
16. [测试工具](#16-测试工具)
17. [工具函数](#17-工具函数)
18. [限流 / 熔断](#18-限流--熔断)
19. [Excel / PDF](#19-excel--pdf)
20. [标准库常用](#20-标准库常用)

---

## 1. Web 框架

| 库 | 特点 | 适合场景 |
|----|------|---------|
| **Gin** | 最流行，Radix Tree 路由，性能好 | 中小项目、微服务、API 网关 |
| **Echo** | 轻量，中间件丰富 | 类似 Gin，风格更简洁 |
| **Fiber** | 基于 fasthttp，性能极致 | 高性能 API（但生态相对小） |
| **Chi** | 标准 `net/http` 兼容，官方风格 | 强调标准库、无框架依赖 |
| **Beego** | 全栈式（MVC、ORM、CLI 一体） | 传统企业项目 |
| **Iris** | 功能全，性能好 | 大型项目 |

```go
// Gin 示例
import "github.com/gin-gonic/gin"

r := gin.Default()
r.GET("/user/:id", func(c *gin.Context) {
    id := c.Param("id")
    c.JSON(200, gin.H{"id": id})
})
r.Run(":8080")
```

**面试常问**：Gin 为什么快？→ Radix Tree 路由 + 复用 `sync.Pool` 缓存 Context。

---

## 2. 数据库 & ORM

| 库 | 特点 | 适合场景 |
|----|------|---------|
| **`database/sql`** | 标准库，最原始 | 简单查询，追求可控 |
| **GORM** | 最流行 ORM，链式 API | 常规 CRUD，快速开发 |
| **sqlx** | `database/sql` 的增强 | 手写 SQL + struct 映射 |
| **sqlc** | 从 SQL 生成 Go 代码 | 类型安全，性能好 |
| **ent** | Facebook 出品，图模式 | 复杂关系，代码生成 |
| **Bun** | 类似 GORM，性能更好 | 追求性能的 ORM |
| **Squirrel** | SQL 构建器 | 动态构建 SQL |

```go
// GORM 示例
db, _ := gorm.Open(mysql.Open(dsn))
var user User
db.Where("id = ?", 1).First(&user)
db.Create(&User{Name: "Jake"})

// sqlx 示例
var users []User
db.Select(&users, "SELECT * FROM users WHERE age > ?", 18)
```

**面试常问**：GORM 的 N+1 问题？→ 用 `Preload` 或 `Joins` 预加载。

---

## 3. Redis 客户端

| 库 | 特点 |
|----|------|
| **`go-redis/redis`** | 主流选择，功能全 |
| **`redigo`** | 老牌，需要手动管连接池 |
| **`rueidis`** | 高性能，支持 Redis 7 特性 |

```go
// go-redis 示例
import "github.com/redis/go-redis/v9"

rdb := redis.NewClient(&redis.Options{
    Addr:     "localhost:6379",
    Password: "",
    DB:       0,
})

rdb.Set(ctx, "key", "value", time.Hour)
val, _ := rdb.Get(ctx, "key").Result()

// Pipeline
pipe := rdb.Pipeline()
pipe.Set(ctx, "k1", "v1", 0)
pipe.Set(ctx, "k2", "v2", 0)
pipe.Exec(ctx)

// Lua 脚本
script := redis.NewScript(`return redis.call('GET', KEYS[1])`)
result, _ := script.Run(ctx, rdb, []string{"key"}).Result()
```

---

## 4. 消息队列

| 库 | 用途 |
|----|------|
| **`segmentio/kafka-go`** | Kafka 官方推荐 Go 客户端 |
| **`confluent-kafka-go`** | Confluent 官方，性能高（需 CGo） |
| **`Shopify/sarama`** | 老牌 Kafka 客户端 |
| **`go.uber.org/nats.go`** | NATS 客户端 |
| **`streadway/amqp`** | RabbitMQ 客户端 |
| **`apache/rocketmq-clients`** | RocketMQ 客户端 |
| **`asynq`** | 基于 Redis 的任务队列 |

```go
// kafka-go 消费者示例
reader := kafka.NewReader(kafka.ReaderConfig{
    Brokers: []string{"localhost:9092"},
    Topic:   "orders",
    GroupID: "consumer-1",
})
for {
    msg, err := reader.ReadMessage(ctx)
    if err != nil { break }
    process(msg.Value)
}
```

---

## 5. 日志

| 库 | 特点 |
|----|------|
| **`log/slog`** | Go 1.21+ 标准库，结构化日志 |
| **Zap** | Uber 出品，性能极致 |
| **Zerolog** | 零分配，性能好 |
| **Logrus** | 老牌但性能一般，正在被 Zap 取代 |

```go
// Go 1.21+ slog（推荐新项目）
import "log/slog"

logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
logger.Info("user login",
    slog.String("user_id", "u001"),
    slog.Int("attempts", 3),
)

// Zap 示例（性能敏感场景）
import "go.uber.org/zap"

logger, _ := zap.NewProduction()
logger.Info("user login",
    zap.String("user_id", "u001"),
    zap.Int("attempts", 3),
)
```

**面试常问**：Zap 为什么快？→ 零反射（用 field API 而非 `interface{}`）、避免内存分配、批量写。

---

## 6. 配置管理

| 库 | 特点 |
|----|------|
| **Viper** | 多源配置（YAML/JSON/env/etcd），支持热更新 |
| **`env`（caarlos0/env）** | 只从环境变量读，简单 |
| **`kelseyhightower/envconfig`** | 环境变量绑定 struct |
| **`koanf`** | Viper 的替代，更模块化 |

```go
// Viper 示例
import "github.com/spf13/viper"

viper.SetConfigName("config")
viper.SetConfigType("yaml")
viper.AddConfigPath(".")
viper.ReadInConfig()

port := viper.GetInt("server.port")
viper.WatchConfig()  // 支持热更新

// envconfig 示例
type Config struct {
    Port     int    `envconfig:"PORT" default:"8080"`
    Database string `envconfig:"DATABASE_URL" required:"true"`
}
var cfg Config
envconfig.Process("", &cfg)
```

---

## 7. 并发工具

| 库 | 用途 |
|----|------|
| **`golang.org/x/sync/errgroup`** | 一组 goroutine 协同 + 错误收集 |
| **`golang.org/x/sync/singleflight`** | 同一 key 请求合并（防击穿） |
| **`golang.org/x/sync/semaphore`** | 加权信号量 |
| **`sourcegraph/conc`** | 现代化并发工具集 |
| **`sync/atomic`** | 原子操作（标准库） |

```go
// errgroup 示例
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(10)  // 最多并发 10 个

for _, url := range urls {
    url := url
    g.Go(func() error {
        return fetch(ctx, url)
    })
}
err := g.Wait()

// singleflight 示例：防缓存击穿
var sf singleflight.Group

result, err, _ := sf.Do("user:1", func() (any, error) {
    return db.Query("...")
})
```

---

## 8. HTTP 客户端 & 工具

| 库 | 用途 |
|----|------|
| **`net/http`** | 标准库客户端，够用 |
| **Resty** | 链式 API，好用 |
| **`hashicorp/go-retryablehttp`** | 自动重试 |
| **`valyala/fasthttp`** | 高性能 HTTP（但不完全兼容 net/http） |

```go
// Resty 示例
import "github.com/go-resty/resty/v2"

client := resty.New().
    SetTimeout(5 * time.Second).
    SetRetryCount(3)

resp, _ := client.R().
    SetHeader("Authorization", "Bearer xxx").
    SetBody(map[string]any{"name": "jake"}).
    Post("https://api.example.com/users")
```

---

## 9. 序列化

| 库 | 用途 | 特点 |
|----|------|------|
| **`encoding/json`** | 标准库 | 反射，速度一般 |
| **`bytedance/sonic`** | 字节跳动 JSON | 速度最快（3-6 倍于标准库） |
| **`goccy/go-json`** | 高性能 JSON | 兼容 encoding/json |
| **`json-iterator/go`** | 高性能 JSON | 早期方案 |
| **`google.golang.org/protobuf`** | Protobuf | RPC 首选 |
| **`vmihailenco/msgpack`** | MessagePack | 二进制紧凑 |

```go
// sonic 示例（用法和标准库完全一致，性能翻倍）
import "github.com/bytedance/sonic"

data, _ := sonic.Marshal(user)
sonic.Unmarshal(data, &user)
```

---

## 10. 参数校验

| 库 | 用途 |
|----|------|
| **`go-playground/validator`** | 最流行，tag 驱动 |
| **`ozzo-validation`** | 代码驱动，链式 API |
| **`asaskevich/govalidator`** | 老牌 |

```go
// validator 示例
import "github.com/go-playground/validator/v10"

type User struct {
    Name  string `validate:"required,min=2,max=50"`
    Email string `validate:"required,email"`
    Age   int    `validate:"gte=0,lte=150"`
}

v := validator.New()
err := v.Struct(user)
// 校验失败会返回详细错误列表
```

---

## 11. 依赖注入

| 库 | 特点 |
|----|------|
| **Wire** | Google 出品，编译期生成代码，零运行时开销 |
| **fx** | Uber 出品，运行时反射，灵活 |
| **dig** | fx 的底层 |

```go
// Wire 示例（编译期）
// wire.go
func InitializeService() *UserService {
    wire.Build(
        NewDB,
        NewUserRepo,
        NewUserService,
    )
    return nil
}
// 运行 wire 命令后自动生成初始化代码

// fx 示例（运行时）
fx.New(
    fx.Provide(NewDB, NewUserRepo, NewUserService),
    fx.Invoke(func(s *UserService) { s.Run() }),
).Run()
```

---

## 12. 微服务框架

| 框架 | 特点 |
|------|------|
| **go-zero** | 国内主流，注重工程化，自带代码生成 |
| **Kratos** | B 站出品，DDD 分层清晰 |
| **Kitex** | 字节跳动，RPC 性能强 |
| **go-kit** | 老牌，模块化，需要自己组装 |
| **micro** | 早期主流，现在使用少 |

**选型建议**：
- 追求快速开发 + 工程规范 → **go-zero**
- 追求代码优雅 + 分层清晰 → **Kratos**
- 追求 RPC 性能（尤其字节技术栈）→ **Kitex**

---

## 13. RPC

| 库 | 用途 |
|----|------|
| **`google.golang.org/grpc`** | gRPC 官方 |
| **`grpc-ecosystem/grpc-gateway`** | gRPC → REST 网关 |
| **`cloudwego/kitex`** | 字节 RPC，性能高 |
| **`smallnest/rpcx`** | 简单易用的 RPC |
| **`net/rpc`** | 标准库（简单，生产少用） |

```go
// gRPC 服务端拦截器
server := grpc.NewServer(
    grpc.UnaryInterceptor(loggingInterceptor),
    grpc.StreamInterceptor(authInterceptor),
)
```

---

## 14. 可观测性

| 类别 | 库 |
|-----|-----|
| **Metrics** | `prometheus/client_golang` |
| **Trace** | `go.opentelemetry.io/otel` |
| **Profile** | `net/http/pprof` |
| **Log 聚合** | Zap + ELK / Loki |

```go
// Prometheus 示例
import "github.com/prometheus/client_golang/prometheus"

var requestCount = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "http_requests_total",
        Help: "Total HTTP requests",
    },
    []string{"method", "status"},
)

// OpenTelemetry 示例
tracer := otel.Tracer("my-service")
ctx, span := tracer.Start(ctx, "operation")
defer span.End()
```

---

## 15. CLI 工具

| 库 | 用途 |
|----|------|
| **Cobra** | 最流行，子命令层级（kubectl / hugo / docker 用它） |
| **urfave/cli** | 简单易用 |
| **kingpin** | 声明式风格 |
| **pflag** | POSIX 风格 flag 解析 |

```go
// Cobra 示例
var rootCmd = &cobra.Command{
    Use:   "myapp",
    Short: "My application",
}

var serveCmd = &cobra.Command{
    Use: "serve",
    Run: func(cmd *cobra.Command, args []string) {
        startServer()
    },
}

rootCmd.AddCommand(serveCmd)
rootCmd.Execute()
```

---

## 16. 测试工具

| 库 | 用途 |
|----|------|
| **`testing`** | 标准库 |
| **`stretchr/testify`** | 断言 + Mock，最常用 |
| **`gomock`** | Google 出品的 Mock 生成 |
| **`golang/mock`** | 已被 gomock 取代 |
| **`onsi/ginkgo`** | BDD 风格 |
| **`go.uber.org/goleak`** | 检测 goroutine 泄漏 |
| **`stretchr/testify/mock`** | Mock 框架 |
| **`DATA-DOG/go-sqlmock`** | Mock 数据库 |
| **`httptest`** | 标准库 HTTP 测试 |

```go
// testify 示例
import "github.com/stretchr/testify/assert"

func TestUser(t *testing.T) {
    user := GetUser(1)
    assert.NotNil(t, user)
    assert.Equal(t, "Jake", user.Name)
    assert.Greater(t, user.Age, 0)
}

// gomock 示例
mockRepo := NewMockUserRepo(ctrl)
mockRepo.EXPECT().Find(1).Return(&User{Name: "Jake"}, nil)
```

---

## 17. 工具函数

| 库 | 用途 |
|----|------|
| **`samber/lo`** | 类 Lodash 工具集（Map/Filter/Reduce） |
| **`google/uuid`** | UUID 生成 |
| **`shopspring/decimal`** | 精确小数（金融必备） |
| **`sony/sonyflake`** | 分布式 ID（雪花算法变种） |
| **`bwmarrin/snowflake`** | 雪花 ID |
| **`hashicorp/golang-lru`** | LRU 缓存 |
| **`patrickmn/go-cache`** | 带 TTL 的内存缓存 |
| **`allegro/bigcache`** | 大对象内存缓存 |
| **`avast/retry-go`** | 重试 |
| **`cenkalti/backoff`** | 指数退避 |

```go
// samber/lo 示例
import "github.com/samber/lo"

names := []string{"a", "b", "c"}
upper := lo.Map(names, func(s string, _ int) string {
    return strings.ToUpper(s)
})

// decimal 示例（金融计算）
import "github.com/shopspring/decimal"

price := decimal.NewFromFloat(19.99)
qty := decimal.NewFromInt(3)
total := price.Mul(qty)  // 59.97，绝对精确
```

---

## 18. 限流 / 熔断

| 库 | 用途 |
|----|------|
| **`golang.org/x/time/rate`** | 令牌桶限流（官方） |
| **`uber-go/ratelimit`** | 漏桶限流 |
| **`sony/gobreaker`** | 熔断器 |
| **`afex/hystrix-go`** | Hystrix 移植版 |
| **`sentinel-golang`** | 阿里 Sentinel |

```go
// rate 限流示例
limiter := rate.NewLimiter(100, 200)  // 100/s，突发 200
if !limiter.Allow() {
    return errors.New("too many requests")
}

// gobreaker 熔断示例
cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
    Name: "MyService",
    ReadyToTrip: func(counts gobreaker.Counts) bool {
        return counts.Requests > 10 && float64(counts.TotalFailures)/float64(counts.Requests) > 0.5
    },
})
result, err := cb.Execute(func() (any, error) {
    return callService()
})
```

---

## 19. Excel / PDF

| 库 | 用途 |
|----|------|
| **`xuri/excelize`** | Excel 读写（最流行） |
| **`tealeg/xlsx`** | Excel 读写（老牌） |
| **`gofpdf` / `signintech/gopdf`** | PDF 生成 |
| **`unidoc/unipdf`** | PDF 强大（商用需授权） |

```go
// excelize 示例
import "github.com/xuri/excelize/v2"

f := excelize.NewFile()
f.SetCellValue("Sheet1", "A1", "Name")
f.SetCellValue("Sheet1", "B1", "Age")
f.SetCellValue("Sheet1", "A2", "Jake")
f.SetCellValue("Sheet1", "B2", 28)
f.SaveAs("users.xlsx")
```

---

## 20. 标准库常用

除了应用层的三方库，标准库有很多常用包：

| 包 | 用途 |
|----|------|
| `context` | 超时、取消、值传递 |
| `sync` | Mutex、RWMutex、WaitGroup、Once、Pool、Map |
| `sync/atomic` | 原子操作 |
| `encoding/json` | JSON 序列化 |
| `encoding/base64` | Base64 编码 |
| `crypto/md5` / `sha256` | 哈希 |
| `crypto/rand` | 安全随机数（不要用 math/rand 做加密） |
| `crypto/tls` | TLS |
| `net/http` | HTTP 服务端 + 客户端 |
| `net/http/httptest` | HTTP 测试 |
| `net/url` | URL 解析 |
| `os` / `os/exec` | 系统调用、命令执行 |
| `io` / `bufio` | I/O 接口 |
| `path/filepath` | 跨平台路径 |
| `regexp` | 正则 |
| `strings` / `strconv` | 字符串操作、转换 |
| `time` | 时间处理 |
| `sort` | 排序 |
| `errors` | 错误链（Is/As/Unwrap） |
| `flag` | 命令行参数 |
| `reflect` | 反射 |
| `unsafe` | 底层指针操作 |
| `runtime` | 运行时（NumGoroutine、GC、pprof） |
| `runtime/pprof` | 性能分析 |
| `text/template` / `html/template` | 模板 |

```go
// 标准库组合技示例
import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "time"
)

// 生成安全随机 token
func GenToken() string {
    b := make([]byte, 16)
    rand.Read(b)
    return hex.EncodeToString(b)
}

// 带超时的 context
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
```

---

## 快速选型 Checklist

**建一个新项目，我该用啥？**

| 需求 | 推荐 |
|------|------|
| Web 框架 | Gin（微服务） / go-zero（工程化） |
| ORM | GORM（快速） / sqlc（类型安全） / sqlx（手写 SQL） |
| Redis | go-redis/v9 |
| 日志 | slog（Go 1.21+） / Zap（性能敏感） |
| 配置 | Viper / envconfig |
| 参数校验 | validator/v10 |
| HTTP 客户端 | Resty |
| JSON | sonic（性能）/ 标准库（默认） |
| 依赖注入 | Wire（编译期）/ fx（灵活） |
| 测试 | testify + gomock |
| 限流 | golang.org/x/time/rate |
| 熔断 | sony/gobreaker |
| 分布式 ID | sony/sonyflake |
| 唯一 ID | google/uuid |
| 金融精度 | shopspring/decimal |
| CLI | Cobra |
| Excel | excelize |
| Metrics | prometheus/client_golang |
| Trace | opentelemetry-go |
| 缓存 | hashicorp/golang-lru（进程内） + redis（分布式） |
| 并发协调 | errgroup + singleflight（`x/sync`） |

---

## 面试常问

### Q1: 为什么 Go 有很多"官方扩展"但不在标准库？

`golang.org/x/*` 是 Go 团队维护但不保证 API 稳定性的扩展。比如 `errgroup`、`singleflight`、`rate` 都在这里，因为需要迭代空间。用起来和标准库一样可靠。

### Q2: GORM 和 sqlx 该选哪个？

- 团队快速开发、CRUD 多、可以接受性能一般 → **GORM**
- 复杂 SQL、性能敏感、想手写 SQL → **sqlx** 或 **sqlc**
- 大厂内部实践一般是 sqlc（类型安全 + 手写 SQL 可控）

### Q3: Zap 和 slog 该选哪个？

- Go 1.21+ 新项目 → **slog**（标准库，无外部依赖）
- 老项目已在用 → **Zap**（性能更极致，可以慢慢迁移）
- 生态成熟 → Zap 目前更多中间件适配

### Q4: 为什么很多库都在 `google.golang.org/*` 或 `go.uber.org/*`？

这是自定义 import 路径（vanity import path），通过 HTTP 重定向指向真实的 Git 仓库。好处是可以更换托管平台不影响用户代码。

### Q5: 依赖太多怎么办？

- 定期 `go mod tidy` 清理无用依赖
- 用 `go mod why <package>` 查为什么依赖某个库
- 用 `nancy` / `govulncheck` 检查漏洞
- 大型项目考虑 workspace mode（`go.work`）
