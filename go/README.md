# Go 专题

Go 语言相关的所有内容集中在这里：语法、并发、runtime、框架、生态、面试题。

## 目录

- [basics/](./basics/README.md) — 基础：slice、map、interface、error、反射
- [concurrency/](./concurrency/README.md) — 并发：goroutine、channel、sync、context、select
- [runtime/](./runtime/README.md) — Runtime：GMP、GC、内存模型、逃逸分析
- [performance/](./performance/README.md) — 性能：pprof、benchmark、编译器优化
- [frameworks/](./frameworks/README.md) — 框架：Gin、go-zero、Kratos
- [ecosystem/](./ecosystem/README.md) — 生态：GORM、Zap、Viper、Cobra
- [questions.md](./questions.md) — 高频面试题索引

## 学习路径建议

1. **基础必吃**：`basics/` — slice/map/interface 三件套源码级理解
2. **并发核心**：`concurrency/` — Go 的核心竞争力
3. **深入 runtime**：GMP、GC 是必考大题
4. **框架实战**：挑一个主力框架用熟
5. **性能调优**：pprof、benchmark 实战

## 常考重点

- slice 扩容规则与共享底层数组问题
- map 底层（hmap/bmap）、扩容、并发不安全
- interface 内部结构（iface / eface）、类型断言
- channel 底层（hchan）、close 语义、select 随机性
- GMP 调度模型、work stealing
- GC 三色标记 + 混合写屏障
- 逃逸分析与内联优化
- context 传播与取消传递
- 常见并发 bug：竞态、死锁、goroutine 泄漏
