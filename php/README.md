# PHP 专题

PHP 相关的所有内容集中在这里：语言特性、框架源码、生态类库、面试题。

## 目录

- [basics/](./basics/README.md) — 基础：语法、类型、引用、闭包、SPL、数组内部实现
- [oop/](./oop/README.md) — OOP：类、trait、接口、抽象类、魔术方法、命名空间
- [advanced/](./advanced/README.md) — 高级：反射、生成器、Fiber、协程、FFI
- [performance/](./performance/README.md) — 性能：opcache、JIT、内存管理、GC
- [frameworks/](./frameworks/README.md) — 框架：Laravel、Hyperf、Symfony
- [ecosystem/](./ecosystem/README.md) — 生态：Swoole、Guzzle、Composer、PSR
- [questions.md](./questions.md) — 高频面试题索引

## 学习路径建议

1. **基础巩固**：`basics/` → `oop/`
2. **深入原理**：`advanced/` → `performance/`
3. **框架源码**：挑一个主力框架吃透，Laravel 或 Hyperf
4. **生态扩展**：Swoole / 协程 / PSR 标准
5. **面试冲刺**：翻 `questions.md`

## 常考重点

- 数组底层实现（HashTable）
- 引用计数与写时复制（COW）
- Zend 引擎与 opcache 原理
- Laravel 服务容器 / 请求生命周期
- Swoole 协程模型与 Hyperf
- PSR-4 / PSR-7 / PSR-11 / PSR-15
