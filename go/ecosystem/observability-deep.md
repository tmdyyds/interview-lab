# Prometheus + OpenTelemetry 深度剖析

**标签**: #go #ecosystem #observability #prometheus #otel #高频

现代 Go 服务的可观测性三支柱：**Metrics（Prometheus）+ Traces（OpenTelemetry）+ Logs（Zap/slog）**。本文深度讲前两个。

---

## 一、Prometheus 客户端

`prometheus/client_golang` 是 Go 服务暴露 metrics 的事实标准。

### 快速上手

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
    httpRequestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "http_requests_total",
            Help: "Total HTTP requests",
        },
        []string{"method", "path", "status"},
    )
    httpDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "http_request_duration_seconds",
            Help:    "HTTP request latency",
            Buckets: prometheus.DefBuckets,  // 默认 buckets
        },
        []string{"method", "path"},
    )
)

func init() {
    prometheus.MustRegister(httpRequestsTotal, httpDuration)
}

func main() {
    http.Handle("/metrics", promhttp.Handler())
    http.ListenAndServe(":8080", nil)
}

// 使用
func handler(w http.ResponseWriter, r *http.Request) {
    start := time.Now()
    // ... 处理请求
    httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, "200").Inc()
    httpDuration.WithLabelValues(r.Method, r.URL.Path).Observe(time.Since(start).Seconds())
}
```

访问 `http://localhost:8080/metrics` 拿到所有指标（标准 Prometheus 拉取格式）。

---

## 二、四种指标类型

### Counter：只增计数器

```go
requestCount := prometheus.NewCounter(prometheus.CounterOpts{
    Name: "requests_total",
})
requestCount.Inc()
requestCount.Add(5)
// requestCount.Dec()  // ❌ 编译错误，Counter 不能减
```

**用途**：请求数、错误数、任务完成数。**任何"只增不减"的量**。

**为什么不能减**：重启后从 0 开始。Prometheus 通过 `rate()` 计算增长率，如果能减，`rate` 会给出错误的结果。

### Gauge：可增可减的瞬时值

```go
goroutineCount := prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "goroutines",
})
goroutineCount.Set(float64(runtime.NumGoroutine()))
goroutineCount.Inc()
goroutineCount.Dec()
```

**用途**：CPU、内存、连接数、队列长度。**当前的量**。

### Histogram：分布统计

```go
requestDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
    Name:    "request_duration_seconds",
    Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
})
requestDuration.Observe(0.15)  // 记录一次观测值
```

**Prometheus 会自动生成**：
- `request_duration_seconds_bucket{le="0.005"}` = 落在 [0, 0.005] 的次数
- `request_duration_seconds_bucket{le="0.01"}` = 落在 [0, 0.01] 的次数
- `request_duration_seconds_sum` = 所有观测值之和
- `request_duration_seconds_count` = 观测次数

**PromQL 计算分位**：

```promql
histogram_quantile(0.99, rate(request_duration_seconds_bucket[5m]))
# 过去 5 分钟的 P99
```

### Summary：客户端算分位

```go
summary := prometheus.NewSummary(prometheus.SummaryOpts{
    Name: "request_duration",
    Objectives: map[float64]float64{
        0.5:  0.05,   // 中位数 ±5%
        0.9:  0.01,   // P90 ±1%
        0.99: 0.001,  // P99 ±0.1%
    },
})
summary.Observe(0.15)
```

### Histogram vs Summary 怎么选

| 维度 | Histogram | Summary |
|-----|----------|---------|
| 分位数计算 | 服务端（PromQL）| 客户端 |
| 聚合能力 | ✅ 多实例可以合并 | ❌ 分位数无法跨实例合并 |
| 精度 | 依赖 bucket 设计 | 高（客户端精确） |
| 性能 | 高（只加 counter） | 低（客户端要维护窗口） |

**规则**：**除非只有单实例，否则永远用 Histogram**。因为分布式系统需要跨实例聚合 P99，Summary 做不到。

---

## 三、Label 陷阱：Cardinality 爆炸

### 什么是 Cardinality

**一个 metric 的所有 label 组合数** = cardinality。

```go
httpRequestsTotal.WithLabelValues(method, path, status).Inc()
// method: 5 种 (GET/POST/PUT/DELETE/PATCH)
// path:   100 种
// status: 20 种 (200, 201, 400, 401, ...)
// cardinality = 5 × 100 × 20 = 10000
```

**Prometheus 每个 label 组合是一个独立的时间序列**。10000 序列还能承受，但如果 path 里带了用户 ID / 请求 ID：

```go
// ❌ 灾难性用法
httpRequestsTotal.WithLabelValues(method, "/api/users/12345", status).Inc()
httpRequestsTotal.WithLabelValues(method, "/api/users/12346", status).Inc()
// 每个用户一个 path → cardinality 爆炸到亿级
// Prometheus 内存爆炸、查询变慢
```

**规则**：
- Label 值必须是**有限集合**（枚举）
- **绝对不要**放：用户 ID、请求 ID、时间戳、IP、UUID
- 大约的红线：单 metric cardinality < 10000

### 修复：规范化路径

```go
// ❌ 用原始路径
path := r.URL.Path  // "/api/users/12345"

// ✅ 用路由模板
path := "/api/users/:id"  // Gin 提供 c.FullPath()
httpRequestsTotal.WithLabelValues(method, path, status).Inc()
```

---

## 四、Vec 类型的性能优化

### 每次 WithLabelValues 都有开销

```go
// ❌ 热路径每次都查 label
for i := 0; i < 1000000; i++ {
    counter.WithLabelValues("GET", "/api").Inc()  // 每次 map lookup
}
```

`WithLabelValues` 内部要 hash 一次，从 map 找对应的 Counter。

### 预先缓存

```go
// ✅ 常用组合预缓存
getRequestCounter := counter.WithLabelValues("GET", "/api")

for i := 0; i < 1000000; i++ {
    getRequestCounter.Inc()  // 直接原子操作，无 map lookup
}
```

生产建议：**中间件里查询一次并缓存到 context / 局部变量**。

---

## 五、自定义 Collector

有时候要暴露"实时计算"的指标（如从数据库拿状态），用 Collector：

```go
type QueueCollector struct {
    queueLen prometheus.Desc
    queue    Queue
}

func NewQueueCollector(q Queue) *QueueCollector {
    return &QueueCollector{
        queueLen: *prometheus.NewDesc(
            "queue_length",
            "Current queue length",
            nil, nil,
        ),
        queue: q,
    }
}

func (c *QueueCollector) Describe(ch chan<- *prometheus.Desc) {
    ch <- &c.queueLen
}

func (c *QueueCollector) Collect(ch chan<- prometheus.Metric) {
    // 每次 /metrics 请求时都会调用
    ch <- prometheus.MustNewConstMetric(
        &c.queueLen,
        prometheus.GaugeValue,
        float64(c.queue.Len()),
    )
}
```

**用途**：不想主动更新指标，Prometheus 拉取时才计算。

---

## 六、Prometheus 生产陷阱

### 陷阱 1：不注册就用

```go
counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "foo"})
counter.Inc()
// ❌ /metrics 里没有 foo
```

必须 `prometheus.MustRegister(counter)`。

### 陷阱 2：多次注册同名指标 panic

```go
func createCounter() prometheus.Counter {
    c := prometheus.NewCounter(...)
    prometheus.MustRegister(c)  // 第二次调用会 panic
    return c
}
```

**修复**：用 `promauto`（自动注册） 或全局变量在 `init` 里注册。

```go
import "github.com/prometheus/client_golang/prometheus/promauto"

var counter = promauto.NewCounter(prometheus.CounterOpts{Name: "foo"})
// 自动注册到 DefaultRegisterer
```

### 陷阱 3：Bucket 设计不合理

```go
// ❌ 默认 buckets [0.005, 0.01, ..., 10] 对 API 响应时间还好
// 对某些场景（如批任务 1-60 分钟）就不合适
prometheus.HistogramOpts{
    Buckets: prometheus.DefBuckets,
}

// ✅ 按业务实际范围设计
Buckets: []float64{
    0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1800,
}
```

**规则**：bucket 覆盖实际值的 P99；bucket 太少精度低，太多 cardinality 爆炸。

---

## 七、OpenTelemetry (OTel)：分布式追踪

### 概念

**Trace**：一次请求的完整链路
**Span**：链路上的一个节点（一次函数调用 / RPC / DB 查询）

```
Trace: user-request (100ms)
  └─ Span: HTTP handler (100ms)
      ├─ Span: DB query (30ms)
      ├─ Span: Redis get (5ms)
      └─ Span: RPC call to user-service (50ms)
              └─ Span: DB query in user-service (40ms)
```

每个 span 有：
- **TraceID**：整个链路的唯一 ID
- **SpanID**：当前节点的 ID
- **ParentSpanID**：父节点

### 基础用法

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func InitTracer(ctx context.Context) (*sdktrace.TracerProvider, error) {
    // 1. Exporter：把 trace 发到哪里（Jaeger / OTLP / Zipkin）
    exp, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint("otel-collector:4317"),
        otlptracegrpc.WithInsecure(),
    )
    if err != nil {
        return nil, err
    }

    // 2. Resource：服务元信息
    res, _ := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName("my-service"),
            semconv.ServiceVersion("v1.0.0"),
        ),
    )

    // 3. TracerProvider：把 exporter 和 resource 组装起来
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exp),  // 批量发送
        sdktrace.WithResource(res),
        sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.1)),  // 采样 10%
    )
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})  // W3C 标准
    return tp, nil
}

// 使用
func handler(ctx context.Context) {
    tracer := otel.Tracer("my-component")
    ctx, span := tracer.Start(ctx, "process-request")
    defer span.End()

    span.SetAttributes(attribute.String("user.id", "u001"))

    if err := doSomething(ctx); err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    }
}
```

---

## 八、跨服务传递：Propagation

Trace ID 需要跨进程传递。OTel 用 **W3C Trace Context** 标准，通过 HTTP header：

```
traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
             │  │                                │                │
             │  TraceID (32 hex)                SpanID (16 hex)   Flags
             版本
```

### 客户端注入

```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

client := http.Client{
    Transport: otelhttp.NewTransport(http.DefaultTransport),
}

req, _ := http.NewRequestWithContext(ctx, "GET", "http://user-svc/api", nil)
resp, _ := client.Do(req)
// 自动注入 traceparent header
```

### 服务端提取

```go
handler := otelhttp.NewHandler(myHandler, "my-server")
http.Handle("/api", handler)
// 自动从 header 提取 traceparent，创建 span 挂在正确的 parent 下
```

### 手动传递（gRPC / MQ）

```go
// 注入
carrier := propagation.HeaderCarrier{}
otel.GetTextMapPropagator().Inject(ctx, carrier)
// 把 carrier 里的 headers 发送

// 提取
ctx := otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(headers))
```

---

## 九、采样策略

全量追踪成本高（存储、网络），采样是必选：

```go
// 1. 固定比例：10% 采样
sdktrace.TraceIDRatioBased(0.1)

// 2. AlwaysSample：全量（只用于开发）
sdktrace.AlwaysSample()

// 3. NeverSample：不采样
sdktrace.NeverSample()

// 4. 组合：错误全量，其他采样
type customSampler struct{}
func (s customSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
    // 有错误标记 → 全量
    for _, attr := range p.Attributes {
        if attr.Key == "http.status_code" && attr.Value.AsInt64() >= 500 {
            return sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample}
        }
    }
    // 其他 10% 采样
    // ...
}
```

**规则**：
- 生产 10-20% 通用采样
- 错误 / 慢请求全采样（Tail-based sampling，需要 collector 支持）
- 关键接口调高采样率

---

## 十、Span 最佳实践

### Attribute：结构化元数据

```go
span.SetAttributes(
    attribute.String("user.id", userID),
    attribute.Int("order.item_count", len(order.Items)),
    attribute.Float64("order.total", order.Total),
    attribute.Bool("cache.hit", hit),
)
```

### Event：时间点标记

```go
span.AddEvent("cache_miss", trace.WithAttributes(
    attribute.String("key", key),
))
span.AddEvent("db_query_start")
// ... 查询
span.AddEvent("db_query_end", trace.WithAttributes(
    attribute.Int("rows", rowCount),
))
```

### 记录错误

```go
if err != nil {
    span.RecordError(err)  // 记录错误详情
    span.SetStatus(codes.Error, err.Error())  // 标记 span 失败
    return err
}
```

---

## 十一、生产陷阱

### 陷阱 1：Span 未 End 导致内存泄漏

```go
func handler(ctx context.Context) {
    _, span := tracer.Start(ctx, "handler")
    // ❌ 忘了 defer span.End()
    doWork()
}
```

Span 未 End 会一直在内存里，等到超时或 GC。**永远 `defer span.End()`**。

### 陷阱 2：不传 ctx 断链

```go
func handler(ctx context.Context) {
    _, span := tracer.Start(ctx, "handler")
    defer span.End()

    // ❌ 用 context.Background()，子 span 挂不上父 span
    doQuery(context.Background())
}

// ✅
doQuery(ctx)  // 用带 span 的 ctx
```

### 陷阱 3：Attribute Cardinality

和 Prometheus 一样，attribute 别放高基数值：

```go
// ❌
span.SetAttributes(attribute.String("request_id", reqID))
// 每个 request 都是不同值 → 后端存储爆炸

// ✅ request_id 应该作为 log 关联字段，不是 attribute
// 用 span.SpanContext().TraceID() 关联
```

**推荐 attribute**：user_id、tenant_id、http.method、http.status_code、db.system。
**不推荐 attribute**：request_id、response body、大 payload。

### 陷阱 4：批量导出的延迟

```go
sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second))
// 默认 5 秒才发一次批
```

如果服务 crash，最后 5 秒的 trace 丢失。**服务优雅关闭时要 `tp.Shutdown(ctx)`** 强制刷 buffer：

```go
defer func() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    tp.Shutdown(ctx)
}()
```

---

## 十二、Metrics + Trace + Log 联动

现代观测的黄金法则：**从任一维度都能跳到另一个**。

### Log 里带 TraceID

```go
import "go.opentelemetry.io/otel/trace"

func log(ctx context.Context, msg string) {
    span := trace.SpanFromContext(ctx)
    traceID := span.SpanContext().TraceID().String()
    spanID := span.SpanContext().SpanID().String()

    logger.Info(msg,
        zap.String("trace_id", traceID),
        zap.String("span_id", spanID),
    )
}
```

在 Kibana / Grafana Loki 里搜到某条日志，直接跳 Jaeger 看这条 trace。

### Trace 里关联 Metric

Grafana 里 Prometheus 面板可以配 **Exemplars**（示例点），点一下就跳到对应的 trace。

### 三支柱联动示意

```
用户看到接口慢
    ↓
Grafana 看 Prometheus P99 指标：某接口 P99 从 100ms 涨到 2s
    ↓ 点 Exemplar
Jaeger 看某条慢请求的 trace：定位到某个 DB span 花了 1.8s
    ↓ 点 TraceID
Loki 搜 trace_id=xxx 的日志：看到 slow query SQL
    ↓ 修复 SQL
```

---

## 十三、面试高频题

### Q1: Prometheus 四种指标怎么选？

- **Counter**：只增（请求数、错误数）
- **Gauge**：可增可减的瞬时值（内存、连接数）
- **Histogram**：观测值分布，服务端算分位（响应时间）
- **Summary**：客户端算分位（**只推荐单实例场景**）

### Q2: Histogram 和 Summary 的区别？

- Histogram：客户端只加 bucket counter，服务端 `histogram_quantile()` 算分位。**支持跨实例聚合**。
- Summary：客户端维护滑动窗口算分位。**跨实例分位数无法合并**。

分布式服务必须用 Histogram。

### Q3: Cardinality 爆炸是什么？

Label 组合数过多，每个组合都是一条时间序列。放高基数值（用户 ID、UUID）会让序列数无限膨胀，Prometheus 内存爆炸、查询变慢。

红线：单 metric cardinality < 10000。

### Q4: 什么是 W3C Trace Context？

分布式追踪的行业标准 header：`traceparent: version-traceid-spanid-flags`。取代了旧的 Zipkin B3 / Jaeger 私有 header，让不同追踪系统能互通。

### Q5: 采样率怎么定？

- 生产 10-20% 通用采样
- 错误请求 100%
- 关键接口调高（如支付）
- **Tail-based sampling**：Collector 端根据完整 trace 决定采样（比如只保留有错误的）

### Q6: 为什么要 batcher 而不是 exporter 直接发？

批量导出减少网络开销，但引入延迟（默认 5s 批发一次）。服务 crash 时 buffer 里的 trace 丢失，**优雅关闭必须 Shutdown**。

### Q7: OpenTelemetry 和 Jaeger 什么关系？

- **OpenTelemetry (OTel)**：**规范 + SDK**，跨语言统一的观测数据采集标准
- **Jaeger**：**后端**，存储 + UI，可视化 trace
- **架构**：应用（OTel SDK）→ OTel Collector → Jaeger / Zipkin / Datadog...

OTel 是采集侧的通用标准，后端可以是任何支持 OTel 的系统。

### Q8: 三支柱怎么联动？

- **Log 里带 TraceID / SpanID**：任一日志能跳到 trace
- **Metric 用 Exemplars**：面板点一下跳到 trace
- **Trace 里能看 log**：span attribute 挂关键日志字段

在 Grafana 里一站式跳转。

### Q9: Metric 和 Trace 有啥区别？

- **Metric**：聚合数据（QPS、错误率、延迟分布）→ **回答"整体怎么样"**
- **Trace**：单个请求的详细链路 → **回答"这一个请求为什么慢"**

用 Metric 发现问题（P99 涨了），用 Trace 定位问题（哪个下游慢）。

### Q10: 高频操作 Inc/Dec 会不会性能问题？

Prometheus 客户端用 `atomic` 操作，纳秒级。真正的开销在 `WithLabelValues`（map lookup），热路径要**预先缓存**：

```go
c := counter.WithLabelValues("GET", "/api")  // 提前查一次
for {
    c.Inc()  // 直接原子加
}
```
