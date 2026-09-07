# 操作系统

## 计划收录

- **进程 / 线程 / 协程**
  - 定义与区别
  - 上下文切换开销
  - 用户态 vs 内核态
- **进程间通信（IPC）**：管道、消息队列、共享内存、信号量、socket
- **线程同步**：互斥锁、读写锁、自旋锁、条件变量、信号量
- **调度算法**：FCFS、SJF、RR、多级反馈队列
- **内存管理**：
  - 虚拟内存 / 物理内存
  - 分页 / 分段
  - 页面置换算法：FIFO、LRU、LFU、Clock
  - Copy-On-Write
- **IO 模型**：
  - 阻塞 / 非阻塞 IO
  - IO 多路复用：select、poll、epoll、kqueue
  - 信号驱动 IO
  - 异步 IO（AIO / io_uring）
  - Reactor / Proactor 模式
- **零拷贝**：`sendfile`、`mmap`、`splice`
- **死锁**：四条件、检测、避免、解除

## 面试常问

- epoll 相比 select/poll 的优势？ET vs LT
- 什么是零拷贝？为什么快？
- 死锁的四个必要条件
- 用户态和内核态切换的开销体现在哪？
