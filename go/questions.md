# Go 高频面试题索引

按主题归类，链接指向具体题目文档。新增题目时同步更新本索引。

## 基础

- [ ] slice 底层结构？扩容规则？为什么 append 后可能出现共享底层数组的问题？
- [ ] map 底层结构？扩容为什么是渐进式？为什么并发不安全？
- [ ] `sync.Map` 与 `map + Mutex` 的区别和适用场景
- [ ] interface 的底层结构 iface / eface 区别？
- [ ] nil interface 和 interface 里存 nil 指针的区别
- [ ] 数组和 slice 的区别？作为函数参数传递时呢？
- [ ] string 与 []byte 转换会发生什么？如何零拷贝转换？
- [ ] defer 的执行顺序？循环中 defer 有什么陷阱？
- [ ] `for range` 循环变量是同一个吗？（Go 1.22 前后差异）
- [ ] error 处理：`errors.Is` vs `errors.As` vs `%w`
- [ ] panic 能否跨 goroutine recover？

## 并发

- [ ] goroutine 和线程的区别？为什么轻量？
- [ ] channel 底层结构？无缓冲 channel 的发送与接收顺序？
- [ ] close 已关闭的 channel 会怎样？向已关闭的 channel 发送呢？
- [ ] select 是随机的还是有序的？为什么？
- [ ] Mutex 的正常模式和饥饿模式
- [ ] RWMutex 写锁饥饿问题
- [ ] `sync.Once` 是如何保证只执行一次的？
- [ ] `sync.Pool` 的对象什么时候被回收？
- [ ] context 是如何做取消传递的？

## Runtime

- [ ] GMP 调度模型详细讲讲
- [ ] G 阻塞在 syscall 时，P 会怎么处理？
- [ ] Go 是如何实现抢占式调度的？
- [ ] `GOMAXPROCS` 的作用？设置为 1 会怎样？
- [ ] GC 三色标记法流程？如何解决漏标问题？
- [ ] 混合写屏障（Hybrid Write Barrier）解决了什么问题？
- [ ] `GOGC` 和 `GOMEMLIMIT` 的区别
- [ ] 逃逸分析规则？哪些情况一定会逃逸？
- [ ] 栈是如何增长的？连续栈 vs 分段栈？

## 性能

- [ ] 如何用 pprof 排查 CPU 高？内存泄漏？goroutine 泄漏？
- [ ] benchmark 编写时有哪些坑？
- [ ] map 与 slice 何时需要预分配？
- [ ] 什么场景适合 `sync.Pool`？
- [ ] interface 装箱的开销体现在哪？

## 框架

- [ ] Gin 的路由为什么快？（Radix Tree）
- [ ] Gin 的 Context 在中间件间如何传递？可以在 goroutine 中使用吗？
- [ ] go-zero 的自适应限流是怎么做的？
- [ ] Kratos 的分层架构与依赖注入

## 生态

- [ ] GORM 的 Session 与常见 N+1 问题
- [ ] gRPC 拦截器执行顺序
- [ ] Zap 为什么比 log 快？
