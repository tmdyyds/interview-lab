# Go 性能

## 已收录

- [**pprof-guide.md**](./pprof-guide.md) — pprof 排查专题
  - 7 种 profile 采集方式（HTTP / runtime API / benchmark / block+mutex 开关）
  - CPU / heap / goroutine / block / mutex / trace 六大实战
  - 生产接入模板、goroutine 泄漏实战案例
  - 10 道面试题带答案

- [**benchmark-pitfalls.md**](./benchmark-pitfalls.md) — Benchmark 编写坑合集
  - 10 类典型坑：编译器优化、Setup 混入、并行错用、多输入、GC 干扰、环境噪音等
  - benchmark + pprof + benchstat 组合技
  - 通关模板 + 命令行示例
  - 10 道面试题带答案

## 计划收录

- **逃逸分析实战**：`go build -gcflags="-m"` 解读、常见逃逸模式
- **内联优化**：`-gcflags="-m=2"`、`//go:noinline`、内联预算
- **常见性能坑**（散见于 basics）：
  - string 与 []byte 频繁转换（零拷贝技巧）
  - map / slice 预分配
  - defer 在热点路径的开销
  - interface 装箱
  - reflect 慢在哪
- **sync.Pool 深度使用**：victim cache、大对象注意事项
- **火焰图与差分火焰图**（differential flame graph）
- **compiler-explorer 视角**：看汇编优化决策

## 相关高频题

见 [../questions.md](../questions.md)
