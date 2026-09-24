// Package main 演示一个 Go HTTP 服务如何在 Kubernetes 上稳定运行。
//
// 面试常问的关键点（每一处代码都对应一个考察点）：
//  1. graceful shutdown：收到 SIGTERM 后先关 readiness，让 K8s Service 摘流量，
//     再给存量请求留时间处理，最后关闭 HTTP server。
//  2. liveness / readiness 分离：liveness 判断进程是否活着（失败会重启）；
//     readiness 判断能否接流量（失败只是从 endpoints 摘除）。
//  3. /metrics 暴露 Prometheus 指标，K8s 集群通过 ServiceMonitor 或 annotation 采集。
//  4. 配置全部通过环境变量注入，符合 12-Factor App，镜像跨环境复用。
//  5. 结构化日志（slog + JSON），方便被 Loki/ELK 索引。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ============================================================================
// Prometheus 指标定义
// ============================================================================
//
// 用 promauto 声明的指标会自动注册到默认 registry，
// /metrics 端点会把它们暴露出去。生产上常见的四大类指标：
//   - Counter：只增不减，比如请求总数、错误总数
//   - Gauge：可增可减，比如当前并发连接数
//   - Histogram：分布，用来算 P95/P99 延迟
//   - Summary：类似 Histogram，但分位数在客户端算，一般不推荐
var (
	// httpRequests 按 path / method / status 维度统计请求总数。
	// 面试点：标签基数不能爆炸，不要把 user_id 之类高基数字段做标签。
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "HTTP 请求总数",
	}, []string{"path", "method", "status"})

	// httpDuration 用 Histogram 记录请求耗时，后续在 Prometheus 里用
	// histogram_quantile(0.99, sum(rate(...)) by (le, path)) 算 P99。
	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP 请求耗时（秒）",
		Buckets: prometheus.DefBuckets, // 默认桶：5ms ~ 10s
	}, []string{"path", "method"})
)

// ready 是一个原子布尔，控制 /readyz 的返回值。
//   - 进程刚起来时为 false（如果需要等 DB 连接池等就绪，可以推迟置 true）
//   - 服务准备好后置 true，K8s 才会往这个 Pod 发流量
//   - 收到 SIGTERM 后立刻置 false，让 K8s 从 endpoints 摘掉这个 Pod
var ready atomic.Bool

func main() {
	// ------------------------------------------------------------------
	// 1. 初始化结构化日志
	// ------------------------------------------------------------------
	// 用标准库 log/slog（Go 1.21+），JSON handler 让日志天然可解析。
	// 不用 zap/logrus，减少依赖；真实项目里 zap 性能更好，可以按需替换。
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// ------------------------------------------------------------------
	// 2. 读配置
	// ------------------------------------------------------------------
	// 只从环境变量读，K8s 里通过 ConfigMap / Secret 注入。
	// 不读文件、不查 etcd，让容器无状态、可任意重启。
	port := getenv("PORT", "8080")
	appEnv := getenv("APP_ENV", "dev")
	// 敏感值示例：真实场景来自 Secret，不打印到日志
	_ = getenv("DB_PASSWORD", "")

	// ------------------------------------------------------------------
	// 3. 注册 HTTP 路由
	// ------------------------------------------------------------------
	// Go 1.22 起 http.ServeMux 支持 "METHOD /path" 语法，
	// 简单服务可以完全用标准库，不需要 gin/echo。
	mux := http.NewServeMux()

	// 3.1 业务接口
	mux.HandleFunc("GET /api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"hello from go on k8s","env":"` + appEnv + `"}`))
	})

	// 3.2 liveness：只要 HTTP server 还在响应就算活着。
	// 关键：不要在这里探 DB / Redis / 下游 API！
	// 否则下游抖动会导致所有 Pod 被 K8s 判定"死亡"并重启，故障从局部放大到全局。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// 3.3 readiness：能不能接流量。
	// 启动完成 → 200；停机前 → 503，K8s 会把这个 Pod 从 Service endpoints 摘除。
	// 依赖（DB / Redis）出问题时也可以主动置 false，实现"依赖 breaker"。
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	// 3.4 Prometheus 指标端点
	mux.Handle("GET /metrics", promhttp.Handler())

	// 中间件：埋点 + 访问日志
	handler := withInstrumentation(mux)

	// ------------------------------------------------------------------
	// 4. 构造 HTTP server
	// ------------------------------------------------------------------
	// 显式设置超时，避免慢客户端把 goroutine 打满（Slowloris 攻击）。
	// ReadHeaderTimeout：读完请求头的时间上限，抵御慢速攻击的关键项。
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ------------------------------------------------------------------
	// 5. 启动 server（子 goroutine）
	// ------------------------------------------------------------------
	go func() {
		slog.Info("http server starting", "addr", server.Addr, "env", appEnv)
		// 简化处理：进程一起来就 ready。
		// 真实项目里应等 DB 连接池、Redis、下游 gRPC 通道都就绪之后再置 true。
		ready.Store(true)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server exited unexpectedly", "err", err)
			os.Exit(1)
		}
	}()

	// ------------------------------------------------------------------
	// 6. 优雅停机
	// ------------------------------------------------------------------
	// K8s 停 Pod 的时序（重点，面试常问）：
	//   1) kubectl delete / Deployment 滚动更新 → API server 标记 Pod terminating
	//   2) 同时并行做两件事：
	//        a. 把这个 Pod 从所有 Service 的 endpoints 摘除
	//        b. 给容器发 SIGTERM
	//   3) 等 terminationGracePeriodSeconds（默认 30s），进程没退就发 SIGKILL
	//
	// 问题：步骤 2a 和 2b 是并行的，且 endpoints 传播到每个 kube-proxy 有延迟。
	// 如果我们收到 SIGTERM 立刻关服务，那"endpoints 还没摘除干净"期间发过来的
	// 请求会被 refuse，用户看到 502。
	//
	// 解法：收到 SIGTERM 后
	//   ① 先关 readiness（这会让本地的 Service 摘除更快触发）
	//   ② sleep 一小段，等 kube-proxy 摘除完成
	//   ③ 再调用 server.Shutdown 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutdown signal received", "signal", sig.String())

	// ① 关 readiness
	ready.Store(false)
	slog.Info("readiness disabled, draining traffic")
	// ② 等 kube-proxy 摘除。5s 是经验值，取决于集群规模；
	//    大集群可以设长一点，但要小于 terminationGracePeriodSeconds
	time.Sleep(5 * time.Second)

	// ③ server.Shutdown 会：
	//    - 停止接受新连接
	//    - 等已有连接的请求处理完
	//    - 若 ctx 超时则强制关闭
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "err", err)
	}
	slog.Info("server stopped cleanly")
}

// getenv 读环境变量，取不到就用默认值。避免每次都写 if os.Getenv == ""。
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ============================================================================
// 中间件：埋点 + 访问日志
// ============================================================================

// statusRecorder 包一层 ResponseWriter，把状态码记下来给 metrics/日志用。
// Go 标准库没有直接暴露 status 的方式，只能这么 hack。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// withInstrumentation 给每个请求打日志 + 更新 Prometheus 指标。
// 探针请求（/metrics /healthz /readyz）不打日志，避免刷屏和干扰指标。
func withInstrumentation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics", "/healthz", "/readyz":
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		elapsed := time.Since(start)

		httpRequests.WithLabelValues(r.URL.Path, r.Method, strconv.Itoa(rec.status)).Inc()
		httpDuration.WithLabelValues(r.URL.Path, r.Method).Observe(elapsed.Seconds())

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", elapsed.Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}
