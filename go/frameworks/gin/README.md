# Gin 框架深度解析

**标签**: #go #gin #framework #高频

Gin 是 Go 最流行的 Web 框架，基于 `net/http`，核心优势是 **Radix Tree 路由 + 中间件洋葱模型 + Context 复用**。

---

## 目录

- [1. 快速入门](./01-quickstart.md) — 基本用法、Hello World、路由参数
- [2. Context 详解](./02-context.md) — 参数获取、绑定、响应、并发陷阱
- [3. 路由原理](./03-routing.md) — Radix Tree 底层、分组路由、路由冲突
- [4. 中间件](./04-middleware.md) — 洋葱模型、Next/Abort、自定义中间件
- [5. 参数校验](./05-validation.md) — binding、validator、自定义规则
- [6. 生产架构](./06-architecture.md) — 分层设计、依赖注入、优雅关闭
- [7. 性能与优化](./07-performance.md) — Gin 为什么快、sync.Pool、火焰图
- [8. 源码剖析](./08-internals.md) — Engine 启动、请求生命周期

---

## 快速导航

**新手起步**：先看 [1. 快速入门](./01-quickstart.md) → [2. Context 详解](./02-context.md)

**面试准备**：重点看 [3. 路由原理](./03-routing.md) + [4. 中间件](./04-middleware.md) + [8. 源码剖析](./08-internals.md)

**生产实践**：看 [6. 生产架构](./06-architecture.md) + [7. 性能与优化](./07-performance.md)

---

## 高频面试题速览

1. Gin 为什么快？→ Radix Tree 路由 + Context 用 `sync.Pool` 复用 + 零反射
2. 中间件的洋葱模型？→ `c.Next()` 递归调用下一个，形成前置/后置逻辑
3. `c.Next()` 和 `c.Abort()` 区别？→ Next 继续执行链，Abort 阻止后续但当前 handler 继续
4. 在 goroutine 里能用 `c` 吗？→ 必须用 `c.Copy()`，因为 Context 会被复用
5. `Bind` 和 `ShouldBind` 区别？→ Bind 失败自动返回 400，ShouldBind 只返错误让你决定
6. 路由冲突怎么处理？→ Gin 启动时 panic，静态路径优先于参数路径
