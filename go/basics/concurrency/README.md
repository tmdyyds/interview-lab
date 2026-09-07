# Go 并发

## 计划收录

- **goroutine**：轻量协程、启动开销、栈管理（可增长栈）
- **channel**：hchan 结构、无缓冲 vs 有缓冲、close 后的读写行为
- **select**：随机性、default 分支、超时模式
- **sync 包**：
  - `Mutex` 正常模式 / 饥饿模式
  - `RWMutex`
  - `WaitGroup`
  - `Once`
  - `Cond`
  - `Pool` 对象池
  - `Map` 何时用
- **atomic**：原子操作与 memory order
- **context**：取消传递、超时、值传递
- **常见并发 bug**：
  - 数据竞争（race detector）
  - 死锁
  - goroutine 泄漏
  - 循环变量捕获
- **并发模式**：Worker Pool、Pipeline、Fan-in / Fan-out、Semaphore
- **errgroup / singleflight** 常见用法

## 相关高频题

见 [../questions.md](../questions.md)
