module github.com/example/go-k8s-demo

go 1.23

// 只依赖 prometheus 客户端；日志用标准库 log/slog（Go 1.21+）。
// 首次 build 前请执行 `go mod tidy`，Go 会自动补齐 require 与间接依赖。
require github.com/prometheus/client_golang v1.20.5
