# GMP 调度器深度剖析

**标签**: #go #runtime #scheduler #gmp #高频

Go 能在**少量 OS 线程上跑百万级 goroutine**，全靠 runtime 的 GMP 调度器。本文从数据结构到调度流程到抢占机制，一次讲透。

---

## 一、为什么需要 GMP

### OS 线程的痛

传统"每个连接一个线程"模型的问题：

```
线程模型：              成本：
1000 个连接 = 1000 个线程   1000 × 8MB 栈 = 8GB 内存
                            每次切换涉及内核态陷入（微秒级）
                            调度器不知道业务优先级
```

**协程（goroutine）的解法**：用户态调度，栈可增长，一个 OS 线程跑成千上万个协程。

### 三代调度模型的演进

```
Go 1.0：GM 模型
    G ─┐
    G ─┼─→ 全局队列 ─→ M (OS 线程)
    G ─┘
    问题：全局锁竞争严重

Go 1.1+：GMP 模型（当前）
    G ─┐        每个 P 有本地队列
    G ─┼─→ P ─→ M
    G ─┘   local queue   
    P 是"逻辑处理器"，减少全局锁
```

---

## 二、G / M / P 数据结构

### G（goroutine）—— 用户协程

```go
// runtime/runtime2.go
type g struct {
    stack       stack          // 栈内存 [lo, hi)
    stackguard0 uintptr        // 栈溢出检查阈值
    stackguard1 uintptr

    m           *m             // 当前绑定的 M（可能为 nil）
    sched       gobuf          // 调度上下文（PC/SP 等寄存器快照）
    atomicstatus uint32        // 状态：_Grunnable / _Grunning / _Gwaiting ...
    goid         uint64        // ID
    waitreason   waitReason    // 阻塞原因

    // 抢占相关
    preempt      bool
    preemptStop  bool          // Go 1.14+ 抢占停止
    preemptShrink bool

    // ... 100+ 字段
}

type gobuf struct {
    sp   uintptr  // 栈指针
    pc   uintptr  // 程序计数器
    g    guintptr
    ctxt unsafe.Pointer
    ret  uintptr
    lr   uintptr
    bp   uintptr  // for framepointer-enabled architectures
}
```

**G 的状态机**：

```
_Gidle          刚分配未初始化
    ↓
_Grunnable      在运行队列中等待
    ↓
_Grunning       正在 M 上运行
    ↓
_Gsyscall       陷入系统调用
_Gwaiting       阻塞（channel、mutex、IO 等）
_Gdead          已结束，等待复用
```

### M（Machine）—— OS 线程

```go
type m struct {
    g0            *g           // 调度用的 g（M 自己的栈，用于执行 runtime 代码）
    curg          *g           // 当前运行的用户 g
    p             puintptr     // 当前绑定的 P
    nextp         puintptr     // 下次要绑定的 P
    oldp          puintptr     // syscall 前的 P

    id            int64
    procid        uint64       // OS 线程 ID
    createstack   [32]uintptr  // 创建时的栈

    park          note         // 休眠的信号量
    schedlink     muintptr     // 空闲 M 链表
    lockedg       guintptr     // 被 LockOSThread 绑定的 g

    // ...
}
```

**关键**：
- 每个 M 有个 `g0`（系统 g），M 执行调度代码时用 `g0` 的栈，避免用户 goroutine 栈污染
- M 必须绑定 P 才能执行用户 G

### P（Processor）—— 逻辑处理器

```go
type p struct {
    id           int32
    status       uint32        // _Pidle / _Prunning / _Psyscall / _Pgcstop
    m            muintptr      // 当前绑定的 M

    // 本地运行队列（无锁，容量 256）
    runqhead     uint32
    runqtail     uint32
    runq         [256]guintptr

    runnext      guintptr      // 优先执行的下一个 G（时间局部性优化）

    // 内存分配器缓存
    mcache       *mcache       // 每个 P 有自己的 mcache（详见 03-memory-allocator）

    // GC 相关
    gcAssistTime int64

    // ...
}
```

**为什么 runq 是 256 定长数组**：
- 无锁 SPMC（Single Producer Multi Consumer）：本 M 是唯一 producer，其他 M 只能 steal
- 定长省内存 + cache line 友好
- 满了就迁移一半到全局队列

### 全局队列

```go
// runtime/proc.go
type schedt struct {
    goidgen  atomic.Uint64
    lastpoll atomic.Int64

    lock     mutex           // ← 全局锁，性能瓶颈
    runq     gQueue          // 全局 runnable 队列
    runqsize int32

    // ...
}
```

**全局队列有锁**，是"备用队列"，尽量少用。

---

## 三、整体架构图

```
                     ┌──────────────────────┐
                     │    全局队列 (有锁)     │
                     │    G, G, G, G, G     │
                     └──────────┬───────────┘
                                │
                     ┌──────────┴───────────┐
                     │                      │
              ┌──────▼───┐            ┌─────▼────┐
              │    P     │            │    P     │
              │ (id=0)   │            │ (id=1)   │
              │          │            │          │
              │ 本地队列: │            │ 本地队列: │
              │[G,G,G,G] │            │ [G,G]    │
              │ runnext: │            │ runnext: │
              │  G       │            │  G       │
              │ mcache:  │            │ mcache:  │
              │  ...     │            │  ...     │
              └──────┬───┘            └─────┬────┘
                     │                      │
              ┌──────▼───┐            ┌─────▼────┐
              │    M     │            │    M     │
              │ (系统线程)│            │(系统线程) │
              │          │            │          │
              │ g0(调度栈)│            │ g0        │
              │ curg=G   │            │ curg=G   │
              └──────────┘            └──────────┘

              ┌──────────────────────┐
              │  空闲 M 池 / 空闲 P 池 │
              │  netpoller (epoll)   │
              └──────────────────────┘
```

**GOMAXPROCS = P 的数量**，默认等于逻辑核心数。

---

## 四、调度流程

### `go func()` 发生了什么

```go
go func() {
    fmt.Println("hello")
}()
```

编译器把 `go` 语句翻译成 `runtime.newproc`：

```go
// runtime/proc.go 简化
func newproc(fn *funcval) {
    gp := getg()          // 当前 g
    pc := getcallerpc()

    systemstack(func() {  // 切到 g0 栈执行
        newg := newproc1(fn, gp, pc)  // 分配一个 g（可能从空闲池复用）

        pp := getg().m.p.ptr()
        runqput(pp, newg, true)  // 放入当前 P 的本地队列

        if mainStarted {
            wakep()  // 有空闲 P 就唤醒
        }
    })
}
```

**runqput 的逻辑**：

```go
func runqput(pp *p, gp *g, next bool) {
    if next {
        // 优先放 runnext（下次立即执行）
        // 如果 runnext 已有 G，把旧 G 换到 runq
        oldnext := pp.runnext
        pp.runnext = gp
        gp = oldnext
    }

    // 尝试放本地队列
    h := atomic.Load(&pp.runqhead)
    t := pp.runqtail
    if t-h < uint32(len(pp.runq)) {
        pp.runq[t%uint32(len(pp.runq))] = gp
        atomic.Store(&pp.runqtail, t+1)
        return
    }

    // 本地队列满 → 迁移一半到全局队列
    runqputslow(pp, gp, h, t)
}
```

**关键设计**：
- `runnext` 优先：新起的 goroutine 通常和父 goroutine 相关（时间局部性），优先执行
- 本地队列满时迁移一半（不是全部），保持流动性

### 调度循环 schedule

M 拿到 P 后进入调度循环：

```go
// runtime/proc.go 简化
func schedule() {
    mp := getg().m

top:
    pp := mp.p.ptr()

    // 每 61 次调度检查一次全局队列（防止全局队列饥饿）
    if pp.schedtick%61 == 0 && sched.runqsize > 0 {
        gp := globrunqget(pp, 1)
        if gp != nil {
            execute(gp, false)
        }
    }

    // 1. 从本地队列拿
    gp, inheritTime := runqget(pp)
    if gp != nil {
        execute(gp, inheritTime)
    }

    // 2. 本地空 → 找活干（findrunnable，阻塞式）
    gp, inheritTime, tryWakeP := findrunnable()
    execute(gp, inheritTime)
}
```

### findrunnable：找活流程

```go
func findrunnable() (gp *g, inheritTime, tryWakeP bool) {
top:
    pp := getg().m.p.ptr()

    // 1. 本地队列
    if gp, inheritTime := runqget(pp); gp != nil {
        return gp, inheritTime, false
    }

    // 2. 全局队列（取一批到本地）
    if sched.runqsize != 0 {
        lock(&sched.lock)
        gp := globrunqget(pp, 0)  // 0 表示取尽可能多的
        unlock(&sched.lock)
        if gp != nil {
            return gp, false, false
        }
    }

    // 3. netpoller（有 IO 就绪的 G）
    if netpollinited() && atomic.Load64(&sched.lastpoll) != 0 {
        if list := netpoll(0); !list.empty() {
            gp := list.pop()
            injectglist(&list)  // 剩余的塞回队列
            return gp, false, false
        }
    }

    // 4. Work Stealing：从其他 P 偷
    for i := 0; i < 4; i++ {
        for enum := stealOrder.start(fastrand()); !enum.done(); enum.next() {
            p2 := allp[enum.position()]
            if p2 == pp { continue }
            if gp := runqsteal(pp, p2, true); gp != nil {
                return gp, false, false
            }
        }
    }

    // 5. 都没有 → M 休眠
    stopm()
    goto top
}
```

**5 层递进**：本地 → 全局 → netpoller → 偷别人 → 休眠。

### Work Stealing 图解

```
P0 (空闲)              P1 (忙)
runq: []                runq: [G1, G2, G3, G4, G5, G6]
                                                    ↑
                                                    偷这一半
     ↓ steal
runq: [G4, G5, G6]      runq: [G1, G2, G3]

P0 从 P1 队列尾部偷一半，避免和 P1 本身（从头部消费）冲突。
```

---

## 五、抢占式调度（Go 1.14 里程碑）

### Go 1.14 之前：协作式抢占

```go
for {
    // 计算密集型代码
}
// ❌ 这个 goroutine 会独占 M，其他 G 饿死
```

Go 1.14 之前，只有**函数调用时**才检查抢占标志（编译器在函数序言插入检查点）。所以纯 CPU 密集的死循环无法被打断。

**臭名昭著的 bug**：GC 需要 STW，但一个死循环 goroutine 拒绝配合 → 整个程序卡死。

### Go 1.14+：基于信号的抢占

**核心思路**：向 M 发 `SIGURG` 信号，中断当前执行流，进入 signal handler，检查是否需要让出。

```
runtime 想抢占 G7 (在 M2 上跑)
        ↓
    向 M2 发 SIGURG
        ↓
    OS 中断 M2 当前执行
        ↓
    进入 signal handler (runtime.sighandler)
        ↓
    检查 gp.preempt，如果需要抢占：
        - 修改 gp.sched.pc 指向 asyncPreempt
        - 从信号返回时会跳到 asyncPreempt
        ↓
    asyncPreempt 保存现场，调用 mcall(gopreempt_m)
        ↓
    G7 状态变为 _Grunnable，重新入队
    调度下一个 G
```

**关键源码**：

```go
// runtime/signal_unix.go
func preemptM(mp *m) {
    if mp.signalPending.CompareAndSwap(0, 1) {
        signalM(mp, sigPreempt)  // 发送 SIGURG
    }
}

// runtime/signal_unix.go
func doSigPreempt(gp *g, ctxt *sigctxt) {
    if wantAsyncPreempt(gp) {
        if ok, newpc := isAsyncSafePoint(gp, ...); ok {
            ctxt.pushCall(newpc, oldpc)  // 修改 PC 到 asyncPreempt
        }
    }
}
```

**为什么用 SIGURG**：这是个几乎不用的信号，不会和用户程序冲突。

### 抢占触发时机

```go
// runtime/proc.go
func retake(now int64) uint32 {
    for i := 0; i < len(allp); i++ {
        pp := allp[i]
        pd := &pp.sysmontick

        if pp.status == _Prunning {
            t := int64(pp.schedtick)
            // G 连续运行超过 10ms → 抢占
            if int64(pd.schedtick) != t {
                pd.schedtick = uint32(t)
                pd.schedwhen = now
            } else if pd.schedwhen+forcePreemptNS <= now {  // 10ms
                preemptone(pp)
            }
        }
    }
}
```

**sysmon 系统监控线程**每 10-20ms 检查一次，发现某个 G 跑太久就抢占。

---

## 六、Syscall 处理

### 阻塞式 syscall

用户 G 陷入阻塞式 syscall（如读文件）：

```
1. G 进入 syscall（_Gsyscall 状态）
2. M 和 P 解绑（P.status = _Psyscall）
3. sysmon 发现 P 阻塞太久 → 把 P 交给其他空闲 M（handoff）
4. syscall 返回后：
   - 尝试拿回原来的 P（快）
   - 拿不到就找其他空闲 P
   - 都没有就把 G 放全局队列，M 休眠
```

**关键**：**P 不会因为 syscall 空转**，会被 hand off 给其他 M。

```go
// runtime/proc.go
func entersyscall() {
    // ...
    pp := gp.m.p.ptr()
    pp.m = 0                              // P 和 M 解绑
    gp.m.oldp.set(pp)
    gp.m.p = 0                            // M 释放 P
    atomic.Store(&pp.status, _Psyscall)   // P 进入 syscall 状态
}

func exitsyscall() {
    // 快路径：拿回原来的 P
    if exitsyscallfast(oldp) {
        // 恢复
    }
    // 慢路径：找空闲 P，或休眠
    mcall(exitsyscall0)
}
```

### 非阻塞式 IO（netpoller）

网络 IO 用 epoll/kqueue，**不阻塞 M**：

```go
// 用户代码
conn.Read(buf)

// runtime 内部
1. socket 设为 non-blocking
2. read 返回 EAGAIN → G 挂到 netpoller，状态 _Gwaiting
3. M 拿下一个 G 继续跑
4. 数据到达 → netpoller 通知 → G 变 _Grunnable 入队
5. 某个 M 拿到这个 G，继续执行 Read
```

**关键**：Go 的网络 IO 天生"异步"，用户代码写着**同步**（`conn.Read`），底层是**epoll 事件驱动**。

---

## 七、goroutine 的生命周期

```
go func() {                   ← newproc → 分配 g，入 P.runq
    x := 1                    ← _Grunning
    <-ch                      ← _Gwaiting（挂 channel 等待队列）
    fmt.Println(x)            ← 被唤醒 → _Grunnable → 调度 → _Grunning
}()                           ← _Gdead → 回收到 gFree 池（复用）
```

**G 复用**：`_Gdead` 的 g 不销毁，进 P 的 `gFree` 链表，下次 `newproc` 直接复用（栈也复用）。

---

## 八、runqput 和 runnext 的巧思

### 为什么有 runnext

```go
func parent() {
    for i := 0; i < 100; i++ {
        go child(i)  // 每次起一个新 goroutine
    }
}
```

如果每次都塞队尾，parent 起完 100 个才轮到 child 跑。**runnext 让新起的 g 立即执行**（假设它和 parent 数据相关，cache 更热）。

### 陷阱：可能造成饥饿

```go
func worker() {
    for {
        // 高频起短任务
        go doQuick()
    }
}
```

每次都用 runnext 会导致队列里其他 g 一直不被执行。runtime 有防御：**每 61 次 schedule 检查一次全局队列**（`schedule()` 里的 `schedtick%61 == 0`）。

---

## 九、面试高频题

### Q1: GMP 各是什么？

- **G**：goroutine，用户协程，包含栈、寄存器快照、状态
- **M**：Machine，OS 线程，真正执行代码
- **P**：Processor，逻辑处理器，持有本地 runqueue 和 mcache。M 必须绑定 P 才能执行 G

P 的数量 = `GOMAXPROCS`。

### Q2: 为什么要 P 这一层？

Go 1.0 只有 GM 模型，全局队列有锁竞争。引入 P 是为了：
1. **本地队列无锁**：每个 P 有自己的 runq，M 从中取 G 无需加锁
2. **本地 mcache**：每个 P 有自己的 mcache，内存分配无锁
3. **work stealing 单位**：steal 以 P 为单位，减少全局竞争
4. **GC 协调单位**：GC 用 P 数量做工作分片

### Q3: Work Stealing 怎么做的？

M 找不到活时，随机遍历其他 P 的本地队列，**从队尾偷一半**：
- 从队尾偷 → 减少和目标 P 的正常消费（队头）冲突
- 偷一半 → 平衡负载，同时避免频繁 steal
- 走完 4 轮还没偷到 → 找 netpoller → 都没有就 stopm 休眠

### Q4: Go 1.14 抢占式调度的原理？

**基于信号的异步抢占**：
1. sysmon 发现 G 运行超过 10ms
2. 向 M 发 SIGURG 信号
3. signal handler 修改 G 的 PC 指向 `asyncPreempt`
4. 从 signal 返回时执行 `asyncPreempt`，让出 CPU

**Go 1.14 之前**：只在函数序言检查抢占标志，纯 CPU 死循环无法被抢占（GC STW 会卡死）。

### Q5: 阻塞式 syscall 时 P 会怎样？

M 和 P 解绑，P 进入 `_Psyscall` 状态。sysmon 检查到 P 阻塞太久（20us），会把 P 交给其他空闲 M（`handoffp`）。syscall 返回时，M 尝试拿回原 P（快路径），拿不到就找其他空闲 P 或把 G 放全局队列，M 休眠。

### Q6: 网络 IO 会阻塞 M 吗？

不会。Go 用 epoll/kqueue 做非阻塞 IO。用户 `conn.Read` 内部：
1. socket 非阻塞
2. 返回 EAGAIN → G 挂到 netpoller，状态 `_Gwaiting`
3. M 拿下一个 G 继续跑（**M 没被阻塞**）
4. 数据到达 → netpoller 唤醒 G

这就是 Go **"同步代码风格 + 异步 IO 性能"**的秘诀。

### Q7: goroutine 是怎么复用的？

goroutine 结束进 `_Gdead`，不销毁，放入 P 的 `gFree` 链表。`newproc` 分配新 g 时优先从 gFree 复用，栈也一并复用。**减少内存分配和栈初始化开销**。

### Q8: runnext 是什么？

P 结构里的一个特殊槽位，**优先于本地队列执行**。新起的 goroutine 默认放 runnext（假设和当前 g 数据相关，缓存热）。

runnext 里的 g 不会被 steal（除非本地队列也空）。

### Q9: 全局队列有什么用？为什么本地队列满会迁移？

- 全局队列是"备用池"，跨 P 共享，减少本地队列溢出的处理成本
- 本地队列满（256）→ 迁移一半到全局，保持本地队列有空间
- 每 61 次 schedule 检查一次全局，避免全局队列 g 饿死

### Q10: GOMAXPROCS 该设多少？

默认 = 逻辑核心数（`runtime.NumCPU()`）。规则：
- **CPU 密集型**：默认值最优
- **IO 密集型**：默认值也够（netpoller 不阻塞 M）
- **容器环境（Go 1.24-）**：可能拿到宿主机核数，用 `uber-go/automaxprocs` 修正
- **Go 1.25+**：原生感知 cgroup CPU limits

### Q11: sysmon 线程做什么？

runtime 的"监控线程"，独立于 GMP 之外，每 10-20us 检查：
- 抢占运行太久的 G（>10ms）
- syscall 阻塞太久的 P → handoff
- 强制 GC（>2 分钟没触发过）
- 释放长时间空闲的 P

**sysmon 是一个不占 P 的特殊 M**。

### Q12: 一个 M 可以对应多少个 G？

理论无限，实际由内存决定。**一个 M 同一时刻只跑一个 G**，但生命周期内可以切换成千上万个 G。

goroutine 初始栈 2KB，一台 8GB 内存机器理论上能跑 400 万个（实际会因为 stack 扩容、runtime 数据结构占用而少一些）。

---

## 十、生产实践

### 观察调度器状态

```bash
# GODEBUG 输出调度器统计
GODEBUG=schedtrace=1000 ./myapp
# 每 1000ms 输出：
# SCHED 1000ms: gomaxprocs=8 idleprocs=6 threads=15 spinningthreads=0 idlethreads=8 runqueue=0 [0 0 0 0 0 0 0 0]

# 详细模式
GODEBUG=schedtrace=1000,scheddetail=1 ./myapp
```

### pprof 查看 goroutine

```bash
go tool pprof http://localhost:6060/debug/pprof/goroutine
(pprof) top
(pprof) list funcName
```

### trace 工具（最强大）

```go
import "runtime/trace"

f, _ := os.Create("trace.out")
trace.Start(f)
defer trace.Stop()
// ... 业务代码
```

```bash
go tool trace trace.out
# 打开浏览器，看每个 goroutine 的调度、P 的忙碌情况、GC 时间线
```

### 常见调优

1. **Goroutine 数量控制**：用 `errgroup.SetLimit` 或 semaphore
2. **避免长时间占用 P**：CPU 密集任务主动 `runtime.Gosched()`（Go 1.14+ 一般不需要）
3. **合理 GOMAXPROCS**：容器环境用 automaxprocs
4. **锁的粒度**：mutex 持锁太久等价于减少了有效的 P
