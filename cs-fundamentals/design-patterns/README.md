# 设计模式

每个模式建议 PHP + Go 双实现，加深印象。

## 计划收录

### 创建型
- 单例（Singleton）— 并发安全实现
- 工厂方法（Factory Method）
- 抽象工厂（Abstract Factory）
- 建造者（Builder）
- 原型（Prototype）

### 结构型
- 适配器（Adapter）
- 装饰器（Decorator）
- 代理（Proxy）
- 外观（Facade）
- 组合（Composite）
- 桥接（Bridge）
- 享元（Flyweight）

### 行为型
- 观察者（Observer）— 事件驱动
- 策略（Strategy）
- 模板方法（Template Method）
- 责任链（Chain of Responsibility）— HTTP 中间件
- 命令（Command）
- 迭代器（Iterator）
- 状态（State）

## 常考模式

- 单例的并发安全实现（Go 用 `sync.Once`，PHP 注意进程模型）
- 装饰器与代理的区别
- 策略与状态的区别
- 责任链在 HTTP 中间件中的应用
