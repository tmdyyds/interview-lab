# PHP 高频面试题索引

按主题归类，链接指向具体题目文档。新增题目时同步更新本索引。

## 基础

- [ ] PHP 数组的底层实现是什么？为什么既能当数组又能当字典？
- [ ] `==` 和 `===` 的区别？`0 == "a"` 在 PHP 7 和 8 中的行为？
- [ ] 值传递与引用传递，`&` 引用与 C 指针的差别？
- [ ] 引用计数与 COW 机制
- [ ] `isset` `empty` `is_null` `array_key_exists` 的差异
- [ ] `include` `require` `include_once` `require_once` 差异与性能

## OOP

- [ ] Trait 与继承、接口的差异，冲突如何解决？
- [ ] 后期静态绑定 `static::` 与 `self::` 的区别
- [ ] 魔术方法执行顺序与性能代价
- [ ] 抽象类 vs 接口，选择时机

## 高级

- [ ] 生成器（Generator）与迭代器的关系
- [ ] Fiber 与协程的区别
- [ ] 反射的应用场景与性能开销
- [ ] PHP 8 的属性（Attribute）与注解的区别

## 性能

- [ ] opcache 的工作流程
- [ ] JIT 适合什么场景，Web 请求为什么收益有限？
- [ ] FPM 静态 vs 动态进程模型
- [ ] PHP 内存泄漏怎么排查？

## 框架

- [ ] Laravel 请求生命周期
- [ ] Laravel 服务容器的绑定与解析
- [ ] Facade 是如何工作的？
- [ ] Eloquent 的懒加载与 N+1 问题
- [ ] Hyperf 协程模型与 Swoole 关系
- [ ] Hyperf 协程内为什么不能用全局变量？

## 生态

- [ ] Composer 是如何解析依赖的？`composer.lock` 的作用？
- [ ] PSR-4 与 PSR-0 的差异
- [ ] Swoole 协程调度原理
- [ ] Swoole Server 的进程模型（Master / Manager / Worker / Task）
