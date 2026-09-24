# Go 高频面试题索引

按主题归类，每道题都链到具体答案文档。`[x]` = 已有答案文档，`[ ]` = 待补。

> 💡 部分题目答案分散在多个文件，主链接指向最详细那一个；`+` 后是补充位置。

---

## 基础

- [x] [slice 底层结构？扩容规则？为什么 append 后可能出现共享底层数组的问题？](./basics/slices-maps.md)
- [x] [map 底层结构？扩容为什么是渐进式？为什么并发不安全？](./basics/slices-maps.md)
- [x] [`sync.Map` 与 `map + Mutex` 的区别和适用场景](./basics/concurrency/sync-and-locks.md)
- [x] [interface 的底层结构 iface / eface 区别？](./basics/interfaces.md)
- [x] [nil interface 和 interface 里存 nil 指针的区别](./basics/interfaces.md)
- [x] [数组和 slice 的区别？作为函数参数传递时呢？](./basics/slices-maps.md) `+` [pointers.md](./basics/pointers.md)
- [x] [string 与 []byte 转换会发生什么？如何零拷贝转换？](./basics/data-types.md)
- [x] [defer 的执行顺序？循环中 defer 有什么陷阱？](./basics/functions.md)
- [x] [`for range` 循环变量是同一个吗？（Go 1.22 前后差异）](./basics/control-flow.md)
- [x] [error 处理：`errors.Is` vs `errors.As` vs `%w`](./basics/error-handling.md)
- [x] [panic 能否跨 goroutine recover？](./basics/functions.md) `+` [goroutine.md Q5](./basics/concurrency/goroutine.md)

## 并发

- [x] [goroutine 和线程的区别？为什么轻量？](./basics/concurrency/goroutine.md)
- [x] [channel 底层结构？无缓冲 channel 的发送与接收顺序？](./basics/concurrency/channel.md)
- [x] [close 已关闭的 channel 会怎样？向已关闭的 channel 发送呢？](./basics/concurrency/channel.md)
- [x] [select 是随机的还是有序的？为什么？](./runtime/06-channel-internals.md) — Q7
- [x] [Mutex 的正常模式和饥饿模式](./basics/concurrency/sync-and-locks.md)
- [x] [RWMutex 写锁饥饿问题](./basics/concurrency/sync-and-locks.md)
- [x] [`sync.Once` 是如何保证只执行一次的？](./basics/concurrency/sync-and-locks.md)
- [x] [`sync.Pool` 的对象什么时候被回收？](./basics/concurrency/sync-and-locks.md)
- [x] [context 是如何做取消传递的？](./basics/concurrency/context.md)

## Runtime

- [x] [GMP 调度模型详细讲讲](./runtime/01-gmp-scheduler.md)
- [x] [G 阻塞在 syscall 时，P 会怎么处理？](./runtime/01-gmp-scheduler.md) — Q5
- [x] [Go 是如何实现抢占式调度的？](./runtime/01-gmp-scheduler.md) — Q4
- [x] [`GOMAXPROCS` 的作用？设置为 1 会怎样？](./runtime/01-gmp-scheduler.md) — Q10
- [x] [GC 三色标记法流程？如何解决漏标问题？](./runtime/02-gc-and-write-barrier.md)
- [x] [混合写屏障（Hybrid Write Barrier）解决了什么问题？](./runtime/02-gc-and-write-barrier.md)
- [x] [`GOGC` 和 `GOMEMLIMIT` 的区别](./runtime/02-gc-and-write-barrier.md)
- [x] [逃逸分析规则？哪些情况一定会逃逸？](./runtime/04-escape-analysis.md)
- [x] [栈是如何增长的？连续栈 vs 分段栈？](./runtime/05-goroutine-stack.md)

## 性能

- [x] [如何用 pprof 排查 CPU 高？内存泄漏？goroutine 泄漏？](./performance/pprof-guide.md)
- [x] [benchmark 编写时有哪些坑？](./performance/benchmark-pitfalls.md)
- [x] [map 与 slice 何时需要预分配？](./basics/slices-maps.md)
- [x] [什么场景适合 `sync.Pool`？](./basics/concurrency/sync-and-locks.md)
- [x] [interface 装箱的开销体现在哪？](./basics/interfaces.md)

## 框架

- [x] [Gin 的路由为什么快？（Radix Tree）](./frameworks/gin/03-routing.md)
- [x] [Gin 的 Context 在中间件间如何传递？可以在 goroutine 中使用吗？](./frameworks/gin/02-context.md) `+` [middleware](./frameworks/gin/04-middleware.md)
- [ ] go-zero 的自适应限流是怎么做的？ — *大纲位于 [go-zero/README](./frameworks/go-zero/README.md)，待补深度文档*
- [ ] Kratos 的分层架构与依赖注入 — *大纲位于 [kratos/README](./frameworks/kratos/README.md)，待补深度文档*

## 生态

- [x] [GORM 的 Session 与常见 N+1 问题](./ecosystem/gorm-deep.md)
- [ ] gRPC 拦截器执行顺序 — *待补，可在 [ecosystem](./ecosystem/README.md) 下新增*
- [x] [Zap 为什么比 log 快？](./ecosystem/zap-deep.md)

---

## 待补清单速查

| 类别 | 题目 | 建议归位 |
|-----|------|---------|
| 框架 | go-zero 自适应限流 | `frameworks/go-zero/adaptive-limiter.md` |
| 框架 | Kratos 分层 + Wire | `frameworks/kratos/architecture-and-wire.md` |
| 生态 | gRPC 拦截器执行顺序 | `ecosystem/grpc-interceptor.md` |

---

## 进度总览

- **已完成**：30 / 32
- **待补**：3

新增题目请同步更新本索引；有新答案文档时把 `[ ]` 改为 `[x]` 并加链接。
