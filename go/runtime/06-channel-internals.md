# Channel 与 Select 源码级实现

**标签**: #go #runtime #channel #hchan #select #sudog #高频

Channel 是 Go 的招牌。基础用法在 [basics/channel.md](../basics/concurrency/channel.md)，本文从 runtime 源码切入，讲透**hchan 数据结构、send/recv 的直接传递优化、close 的原子协议、以及 select 为什么真的是随机的**。

---

## 一、和基础版的边界

| 内容 | basics/channel.md | 本文（runtime 层） |
|-----|-------------------|-------------------|
| API 用法 | ✅ 详细 | ⛔ 跳过 |
| hchan 字段 | ✅ 简介 | ✅ 完整 + sudog |
| send/recv 流程 | ⛔ 表层 | ✅ 源码级、含直接传递 |
| close 语义 | ✅ 使用规则 | ✅ 底层原子协议 |
| select | ⛔ 未涉及 | ✅ selectgo、pollorder、lockorder |
| 版本演进 | ⛔ | ✅ |

---

## 二、hchan 完整数据结构

```go
// runtime/chan.go
type hchan struct {
    qcount   uint           // 缓冲区中当前元素数
    dataqsiz uint           // 缓冲区容量（make 时给的 size）
    buf      unsafe.Pointer // 指向缓冲区（环形数组）
    elemsize uint16         // 元素大小（byte）
    closed   uint32         // 是否已关闭（0 / 1）
    elemtype *_type         // 元素类型
    sendx    uint           // 下一个发送位置索引
    recvx    uint           // 下一个接收位置索引
    recvq    waitq          // 等接收的 goroutine 队列
    sendq    waitq          // 等发送的 goroutine 队列
    lock     mutex          // 互斥锁（内部同步）
}

type waitq struct {
    first *sudog
    last  *sudog
}
```

### sudog 是什么

**sudo goroutine**——goroutine 在等待某个同步事件（channel、select、mutex）时的**代理节点**。

```go
type sudog struct {
    g       *g              // 关联的 goroutine
    next    *sudog
    prev    *sudog
    elem    unsafe.Pointer  // 数据指针（发送/接收用）

    c       *hchan          // 关联的 channel
    isSelect bool           // 是否属于 select（涉及公平锁定）
    success  bool           // 唤醒时是否成功（channel close 会置 false）

    parent   *sudog
    waitlink *sudog
    // ...
}
```

**为什么不直接用 `g` 排队而要中间套一层 sudog**：

1. 一个 g 可能同时在多个 channel 上等（select 场景），需要独立节点
2. sudog 有 pool，可复用，减少 GC 压力
3. `elem` 直接指向 g 栈上要发送/接收的变量地址，方便**直接传递**

---

## 三、内存布局图

```
hchan {
  buf ─→ [_][_][_][_][_]         ← 环形缓冲区
          ↑        ↑
          recvx    sendx

  sendq: sudog1 → sudog2 → ...   ← 等发送的 goroutine
  recvq: sudog3 → sudog4 → ...   ← 等接收的 goroutine

  lock (mutex)
}

每个 sudog 的 elem 指向对应 goroutine 栈上的变量：

  sudog1.elem ─→ [g1 的栈上变量 "要发送的值"]
  sudog3.elem ─→ [g3 的栈上变量 "接收槽位"]
```

**重点**：`sudog.elem` 指向的是**另一个 goroutine 栈上的地址**。因为 goroutine 栈会移动（连续栈扩容），指针需要在栈拷贝时被修正——runtime 已经把 sudog.elem 纳入了指针修正范围。

---

## 四、发送流程 `chansend`

调用 `ch <- v` 编译器会翻译成 `runtime.chansend(c, &v, block, callerpc)`。

```go
// runtime/chan.go 伪代码
func chansend(c *hchan, elem unsafe.Pointer, block bool, callerpc uintptr) bool {
    // Step 0: nil channel → 阻塞（select 非阻塞下直接返回 false）
    if c == nil {
        if !block { return false }
        gopark(...)  // 永远休眠
    }

    // Step 1: 快速失败检查（无锁）
    if !block && c.closed == 0 && full(c) {
        return false
    }

    lock(&c.lock)

    // Step 2: 已关闭 → panic
    if c.closed != 0 {
        unlock(&c.lock)
        panic("send on closed channel")
    }

    // Step 3: recvq 有等接收者 → 直接传递给它，跳过缓冲区
    if sg := c.recvq.dequeue(); sg != nil {
        send(c, sg, elem, ...)
        return true
    }

    // Step 4: 缓冲区还有空 → 拷贝到 buf[sendx]，sendx++
    if c.qcount < c.dataqsiz {
        qp := chanbuf(c, c.sendx)
        typedmemmove(c.elemtype, qp, elem)
        c.sendx++
        if c.sendx == c.dataqsiz { c.sendx = 0 }
        c.qcount++
        unlock(&c.lock)
        return true
    }

    // Step 5: 缓冲区满 + 无接收者 → 阻塞
    // 把当前 g 包成 sudog 挂到 sendq，gopark 让出 CPU
    gp := getg()
    mysg := acquireSudog()
    mysg.elem = elem
    mysg.g = gp
    mysg.c = c
    c.sendq.enqueue(mysg)
    gopark(chanparkcommit, ...)
    // 醒来时 → 数据已被接收方带走
}
```

### 直接传递（Step 3）—— channel 的高性能秘诀

```go
func send(c *hchan, sg *sudog, elem unsafe.Pointer, ...) {
    // sg.elem 指向接收方 g 栈上的接收槽位
    // 直接把 elem 拷贝到 sg.elem —— 跳过缓冲区！
    if sg.elem != nil {
        sendDirect(c.elemtype, sg, elem)
    }
    // 唤醒接收方
    goready(sg.g, ...)
}

func sendDirect(t *_type, sg *sudog, src unsafe.Pointer) {
    // 从发送方栈 → 直接写到接收方栈
    dst := sg.elem
    typeBitsBulkBarrier(t, uintptr(dst), uintptr(src), t.size)
    memmove(dst, src, t.size)
}
```

**为什么这是优化**：

```
普通思路（错的）：
  发送方 → 缓冲区 → 接收方   （两次拷贝）

Go 的做法：
  发送方 → 接收方栈         （一次拷贝，跳过缓冲区）
```

对无缓冲 channel 和"接收方比发送方快"的场景效果显著。

---

## 五、接收流程 `chanrecv`

`v := <-ch` 或 `v, ok := <-ch` 翻译成 `runtime.chanrecv(c, &v, block)`。

```go
func chanrecv(c *hchan, elem unsafe.Pointer, block bool) (selected, received bool) {
    if c == nil {
        if !block { return }
        gopark(...)  // 永远休眠
    }

    lock(&c.lock)

    // Step 1: 已关闭且缓冲区为空 → 返回零值，ok=false
    if c.closed != 0 && c.qcount == 0 {
        unlock(&c.lock)
        if elem != nil { typedmemclr(c.elemtype, elem) }
        return true, false
    }

    // Step 2: sendq 有等发送者
    if sg := c.sendq.dequeue(); sg != nil {
        recv(c, sg, elem, ...)
        return true, true
    }

    // Step 3: 缓冲区有数据 → 从 buf[recvx] 取
    if c.qcount > 0 {
        qp := chanbuf(c, c.recvx)
        if elem != nil { typedmemmove(c.elemtype, elem, qp) }
        typedmemclr(c.elemtype, qp)  // 清零，防内存泄漏
        c.recvx++
        if c.recvx == c.dataqsiz { c.recvx = 0 }
        c.qcount--
        unlock(&c.lock)
        return true, true
    }

    // Step 4: 都不行 → 阻塞挂入 recvq
    gp := getg()
    mysg := acquireSudog()
    mysg.elem = elem   // 记住我的接收槽位，方便发送方直接写
    c.recvq.enqueue(mysg)
    gopark(...)
}
```

### 缓冲区满时的"直接传递"

Step 2 里，发送方在 sendq 排队意味着"缓冲区满 + 有人等发送"。此时接收方拿到数据的路径其实是：

```
从 buf[recvx] 取一个 → 交给接收方
buf[recvx] 空出来 → 把 sendq 队首的元素塞进 buf[sendx]
唤醒该发送方
```

这是**为了保持 FIFO 语义**——发送方先来的先入 buf。不能直接把发送方的数据交给接收方（会破坏顺序）。

---

## 六、close 流程 `closechan`

```go
func closechan(c *hchan) {
    if c == nil { panic("close of nil channel") }
    lock(&c.lock)
    if c.closed != 0 {
        unlock(&c.lock)
        panic("close of closed channel")
    }
    c.closed = 1

    // 收集所有等待的 g
    var glist gList
    for {
        sg := c.recvq.dequeue()
        if sg == nil { break }
        if sg.elem != nil {
            typedmemclr(c.elemtype, sg.elem)  // 清零（接收方拿到零值）
        }
        sg.success = false
        glist.push(sg.g)
    }
    for {
        sg := c.sendq.dequeue()
        if sg == nil { break }
        sg.success = false
        glist.push(sg.g)
    }
    unlock(&c.lock)

    // 批量唤醒（此时 lock 已释放，避免持锁唤醒）
    for !glist.empty() {
        gp := glist.pop()
        goready(gp, ...)
    }
}
```

### close 的三大规则（源码对应）

| 规则 | 源码依据 |
|-----|---------|
| close nil → panic | `if c == nil { panic }` |
| close 已关闭 → panic | `if c.closed != 0 { panic }` |
| 向已关闭 send → panic | `chansend` Step 2 |
| 从已关闭 recv → 零值，ok=false | `chanrecv` Step 1 + `typedmemclr` |
| close 后所有 sender/receiver 被唤醒 | close 收集 recvq/sendq 后 `goready` |

**为什么"多个 sender 一个 receiver"要让 receiver 关**：因为"向已关闭 send 会 panic"——sender 一关，其他 sender 立刻炸；receiver 关则所有 sender 都能安全 drain。

---

## 七、select 底层：为什么真的随机

### 编译期展开

```go
select {
case v := <-ch1:  handleA(v)
case ch2 <- x:    handleB()
case <-time.After(1e9): handleC()
default:          handleD()
}
```

编译器会展开为一个 `runtime.selectgo` 调用，参数是一个 `scase` 数组：

```go
type scase struct {
    c    *hchan         // 涉及的 channel
    elem unsafe.Pointer // 发送/接收的数据地址
}

// 编译器生成的调用等价于：
cases := [3]scase{
    {c: ch1, elem: &v},          // case 0: recv
    {c: ch2, elem: &x},          // case 1: send
    {c: timerCh, elem: ...},     // case 2: recv
}
// default 不进 scase 数组，用 block=false 表示
selected, _ := selectgo(&cases[0], &orders[0], ..., 3, ..., block)
```

### selectgo 的三阶段

```go
// runtime/select.go 简化
func selectgo(cas0 *scase, order0 *uint16, ncases int, block bool) (int, bool) {
    scases := (*[...]scase)(unsafe.Pointer(cas0))[:ncases]
    pollorder := (*[...]uint16)(unsafe.Pointer(order0))[:ncases]
    lockorder := pollorder[ncases:]

    // ★★★ Phase 1: 生成随机的 pollorder（Fisher-Yates 洗牌）
    for i := 1; i < ncases; i++ {
        j := fastrandn(uint32(i + 1))
        pollorder[i] = pollorder[j]
        pollorder[j] = uint16(i)
    }

    // Phase 2: 生成 lockorder（按 hchan 地址升序）
    // 目的：多个 case 用不同 channel 时，避免 A→B 和 B→A 加锁产生死锁

    // 加锁所有涉及的 channel（按 lockorder）
    sellock(scases, lockorder)

    // Phase 3.1: 按 pollorder 遍历，找一个立刻能执行的 case
    for _, casei := range pollorder {
        cas := &scases[casei]
        c := cas.c
        // 判断能否立即通信（有 buf、有等待方、已关闭...）
        if canSendOrRecv(cas, c) {
            selunlock(scases, lockorder)
            return int(casei), ...  // 直接返回
        }
    }

    // Phase 3.2: 都不能立即执行
    if !block {
        selunlock(...)
        return -1, false   // 走 default
    }

    // Phase 3.3: 挂到所有 channel 的 sendq/recvq，gopark
    // 任一 channel 唤醒 → 从其他 channel 的队列里摘掉自己 → 返回
    ...
}
```

### 三个"顺序"的作用

| 顺序 | 排序规则 | 目的 |
|-----|---------|------|
| **pollorder** | Fisher-Yates 洗牌（每次 selectgo 都重新洗） | 打乱扫描顺序 → **真随机** |
| **lockorder** | 按 hchan 内存地址升序 | 全局固定顺序加锁 → **避免死锁** |
| case 书写顺序 | 源码里的字面顺序 | 与调度无关，只影响 `selected` 返回值的映射 |

### 关于随机性的常见误解

- ❌ "select 是按 case 顺序从上到下扫，先就绪的先执行" — 错，是 **pollorder 随机**
- ❌ "多个 case 同时就绪时，编译器选第一个" — 错，是 **等概率随机选一个**
- ✅ "多个 case 同时就绪，每次运行选哪个不可预测；单个就绪则必选它"

### 为什么要随机

**避免饥饿**。假设两个 channel 都长期有数据：

```go
for {
    select {
    case v := <-hot:    // 高频 channel
    case v := <-cold:   // 低频 channel
    }
}
```

如果按顺序扫，`hot` 每次都能满足，`cold` 永远轮不上。随机后两者被公平采样。

---

## 八、几个高频陷阱的源码解释

### 陷阱 1：为什么 `close(nil)` panic 而不是 no-op

源码第一行就 `if c == nil { panic }`。设计者认为**关闭一个未初始化的 channel 是程序 bug**，应尽早暴露。

### 陷阱 2：为什么 range 已关闭 channel 会立即退出（drain 完剩余数据）

`for range ch` 编译为：

```go
for {
    v, ok := <-ch
    if !ok { break }
    // ... 用 v
}
```

关闭时 `ok=false`（chanrecv Step 1），循环退出。**但缓冲区剩余数据会先被消费完**——因为 Step 1 的条件是 `closed && qcount == 0`。

### 陷阱 3：为什么 `send` 到已关闭 channel 一定 panic 而不能"静默失败"

如果静默失败，sender 无法感知——**关闭的 channel 变成黑洞会掩盖 bug**。Go 选择"快速失败"哲学。

### 陷阱 4：select + nil channel 的用法

```go
var ch chan int  // nil
select {
case v := <-ch:  // 永远不会命中（nil channel 永远阻塞）
case <-done:
    return
}
```

利用 `chansend/chanrecv` 遇到 nil 会永远 gopark，可以**动态"禁用"某个 case**：

```go
if !shouldReceive {
    ch = nil   // 该 case 从此永远沉默，直到重新赋值
}
```

面试考频不高但很优雅。

---

## 九、版本演进

| 版本 | 变化 |
|-----|-----|
| Go 1.0 | 基础 hchan 实现 |
| Go 1.1 | select 随机化改用 Fisher-Yates |
| Go 1.5 | sudog 引入对象池，减少分配 |
| Go 1.8 | close 优化：批量唤醒改为持锁收集、释锁后唤醒 |
| Go 1.14 | 抢占改造，channel 阻塞点也可被抢占（`gopark` reason 明确） |
| Go 1.17 | 寄存器调用约定，chansend/recv 参数传递更快 |
| Go 1.19+ | 内部微优化：无 tie 场景快路径、`chanbuf` 边界检查省略 |

---

## 十、面试高频题

### Q1: hchan 里为什么要有 lock？

channel 是**并发安全**的（多个 goroutine 同时 send/recv 不会崩），但内部**不是无锁**的——lock 保护 buf、sendx/recvx、recvq/sendq 的一致性。

高竞争场景下 lock 会成为瓶颈，用 sharding channel 或 lock-free 队列可以规避。

### Q2: 什么是"直接传递"？为什么能优化性能？

无缓冲 channel（或缓冲区空且有 receiver 等待）时，send 不走缓冲区，直接把数据从**发送方栈**拷贝到**接收方栈**——通过 sudog.elem 指针实现。

省的是"发送方 → buf → 接收方"这两次拷贝里的一次。对小对象、高频通信收益明显。

### Q3: 无缓冲 channel 的 send 和 recv 谁先谁后？

**必须同时到位才能通信**（rendezvous 语义）：

- send 先到 → sendq 排队阻塞，等 recv 来配对
- recv 先到 → recvq 排队阻塞，等 send 来配对
- 一到就配对成功、数据传递、双方唤醒

底层用 sudog + gopark/goready 实现。

### Q4: close 已关闭 channel 为什么 panic 而不是幂等？

设计哲学：**关闭一次是 owner 的职责，重复关闭说明所有权混乱**——静默会掩盖架构问题。

工程上如果要"安全关闭"，用 `sync.Once` 包装：

```go
var once sync.Once
func closeOnce(ch chan struct{}) {
    once.Do(func() { close(ch) })
}
```

### Q5: 向已关闭的 channel 发送数据会怎样？

**panic**：`chansend` 拿锁后立即检查 `c.closed != 0` 就抛。

**如何避免**：确保"只有 sender 关闭"或"最后一个 sender 关闭"，或者用 select + done 模式：

```go
select {
case ch <- v:
case <-done:  // 上层已发信号退出
    return
}
```

### Q6: 从已关闭的 channel 接收数据会怎样？

**不会 panic**。规则：

- 缓冲区还有数据 → 正常拿，`ok=true`
- 缓冲区空 → 返回零值，`ok=false`

这就是 `for range ch` 优雅退出的基础。

### Q7: select 是随机选还是按顺序选？为什么？

**多 case 同时就绪时等概率随机**。

实现：`selectgo` 每次都用 Fisher-Yates 洗一个 pollorder 数组，按打乱后的顺序扫描 case。

**为什么随机**：避免饥饿——固定顺序会让排在前面的 channel 永远优先，后面的永远轮不上。

**追问：怎么证明**：写个小测试跑一万次，统计每个 case 命中次数，应近似均等。

### Q8: select 加锁顺序怎么防死锁？

**lockorder 按 channel 的内存地址升序加锁**。

想象两个 goroutine：

```
G1: select { case ch1<-1: ; case ch2<-2: }
G2: select { case ch1<-3: ; case ch2<-4: }
```

如果按 case 书写顺序加锁，G1 拿 ch1、G2 拿 ch1 后想拿 ch2——正常。但如果两个 select 里 channel 顺序不同：

```
G1: select { case ch1<-1: ; case ch2<-2: }
G2: select { case ch2<-3: ; case ch1<-4: }
```

按书写序：G1 拿 ch1 等 ch2，G2 拿 ch2 等 ch1 → 死锁。

统一按地址排序就没这个问题——所有 goroutine 都按同一顺序拿锁。

### Q9: select 里有 default，性能有区别吗？

**有**。带 default 时 `block=false`，如果 Phase 3.1 没找到就绪 case 就直接返回 -1（走 default 分支），**不进 Phase 3.3 挂队列/gopark**。

所以 `select + default` 是**非阻塞探测**，开销约等于加/解锁 + 一次 Fisher-Yates + 一遍扫描。适合"能取就取，不能就干别的"。

### Q10: nil channel 在 select 里有什么用？

nil channel 的 send/recv 永远阻塞（`chansend/chanrecv` 遇 nil + block=true 直接 gopark）。

在 select 里，nil case **永远不会被选中**，等价于把这个 case "关掉"。用法：

```go
var out chan<- Result
for {
    if hasData {
        out = realOut  // 打开
    } else {
        out = nil      // 关闭这个 case
    }
    select {
    case out <- r:
    case v := <-in:
        // ...
    }
}
```

用一个变量控制两个 case 的开启，比 `if/else` 组合 select 简洁。

### Q11: 一个 goroutine 能同时在多个 channel 上等吗？

能，`select` 场景就是。此时同一个 g 会**分别包成不同的 sudog 挂到每个 channel 的队列**：

```
g1
├─ sudog_a → ch1.recvq
├─ sudog_b → ch2.recvq
└─ sudog_c → ch3.sendq
```

任一 channel 有事件唤醒 g1，其他 channel 的 sudog 会被 selectgo 兜底清理（防止悬挂节点）。

### Q12: channel 关闭后再 select 会怎么样？

关闭后 recv 会立即返回零值 + `ok=false`（不阻塞）。select 里的表现是——**该 case 永远处于"就绪"状态**：

```go
for {
    select {
    case v, ok := <-closedCh:
        if !ok { return }  // 记得判断，否则死循环
    case <-otherCh:
        // ...
    }
}
```

忘判 `ok` 会造成 CPU 打满的死循环——这是**channel 面试最经典的一个陷阱**。

---

## 十一、延伸阅读

- 源码：`runtime/chan.go`、`runtime/select.go`
- 提案：[Go: Buffered Channels](https://go.dev/blog/pipelines)
- 深度：Dmitry Vyukov [Go scheduler](https://rakyll.org/scheduler/) 里有 sudog 讲解

---

**回到 [Runtime 目录](./README.md)**
