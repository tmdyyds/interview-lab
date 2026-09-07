# Go 性能

## 计划收录

- **pprof**：CPU、内存、goroutine、block、mutex、trace
- **benchmark**：`go test -bench`、`b.ReportAllocs`、避免编译器优化掉
- **逃逸分析**：`go build -gcflags="-m"`
- **内联优化**：`go build -gcflags="-m=2"`
- **常见性能坑**：
  - string 与 []byte 频繁转换
  - map 预分配
  - slice 预分配
  - defer 在热点路径的开销
  - interface 装箱
  - reflect 慢
- **sync.Pool 复用对象**
- **性能诊断实战**：定位 CPU 高、内存泄漏、goroutine 泄漏
- **火焰图分析**

## 相关高频题

见 [../questions.md](../questions.md)
