# Goroutine 栈机制深度剖析

**标签**: #go #runtime #stack #morestack #copystack #高频

Go 能开百万级 goroutine，除了 GMP 调度，还有一大功臣：**每个 goroutine 初始栈只有 2KB，按需扩容，闲时收缩**。本文讲透栈的演进、扩容、收缩、以及"栈上变量地址会不会变"这个必考坑。

---

## 一、为什么 goroutine 栈这么小

### 线程栈的浪费

| 运行时 | 单栈默认大小 | 1 万个"协程/线程"的栈开销 |
|--------|-------------|-------------------------|
| Linux pthread | 8 MB | **80 GB**（不可接受）|
| Java 线程 | 512 KB ~ 1 MB | 5 ~ 10 GB |
| goroutine (Go 1.4+) | **2 KB** | 20 MB |

线程栈大是因为它**一次分配到位**（虚拟内存），必须给最坏情况留够。Go 反其道：**给最小值，用完再扩**。

### 核心思路

```
goroutine 栈 = 一段可增长的连续内存
    ↓ 函数序言检查
不够了 → 分配新栈（原栈 2 倍）→ 拷贝旧内容 → 修正指针 → 释放旧栈
    ↓ GC 时
用得少 → 缩容一半
```

**关键词**：**连续栈（Contiguous Stack）** + **栈拷贝（Stack Copy）**。

---

## 二、演进史：分段栈 → 连续栈

### Go 1.2 之前：分段栈（Segmented Stack）

栈不够就**分配一段新内存作为"下一段"**，链表串起来：

```
栈段 1: [main frame]
             ↓ 栈溢出
栈段 2: [foo frame][bar frame]
             ↓ 又溢出
栈段 3: [baz frame]
```

**致命问题：hot split（热分裂）**

```go
for i := 0; i < 1e9; i++ {
    foo()  // 每次调用触发跨段
}

// foo 内部：
// - 序言：分配新栈段
// - 尾声：释放栈段
// → 每次循环反复 malloc/free，性能崩塌
```

### Go 1.3+：连续栈（Contiguous Stack）

**方案**：栈不够就分配一整块**两倍大**的新栈，把旧栈内容**整体拷贝**过去。

```
初始:   [2KB stack]
        ↑
        用满了
        ↓
扩容:   [4KB new stack]  ← 把旧内容拷进来
                          ← 旧栈整体释放
```

**代价**：拷贝时要修正**指向栈内的所有指针**（否则指针会指向已释放的旧栈）。这需要 GC 精确类型信息配合——Go 1.3 GC 已经具备。

---

## 三、栈的底层数据结构

### stack 与 gobuf

```go
// runtime/runtime2.go
type g struct {
    stack       stack       // 栈的 [lo, hi) 边界
    stackguard0 uintptr     // 栈扩容检查阈值（用户栈用）
    stackguard1 uintptr     // 栈扩容检查阈值（g0 系统栈用）

    sched       gobuf       // 保存 PC / SP 用于恢复
    // ...
}

type stack struct {
    lo uintptr  // 栈底（低地址）
    hi uintptr  // 栈顶（高地址）
}
```

**内存布局**（栈从高地址向低地址增长）：

```
高地址  hi ─────────────────
              栈帧 N        ← SP 从这里开始往下压
              栈帧 N-1
              ...
              栈帧 0
        stackguard0 ─────── ← 溢出预警线（lo + StackGuard）
              保护区（红区）
低地址  lo ─────────────────
```

### stackguard0 是什么

不是栈的真实底部，而是**预留了一段安全余量的"预警线"**：

```
stackguard0 = lo + StackGuard   // StackGuard = 896 字节（amd64）
```

为什么留 896 字节：给 morestack、runtime 内部函数一点余量，避免刚扩容又立刻溢出。

**特殊值**：`stackguard0 = stackPreempt (0xfffffade)` 表示"请求抢占"。这样**一次比较同时做两件事**——栈溢出检查 & 抢占检查。

---

## 四、栈扩容：morestack → newstack → copystack

### 步骤 1：编译器插入的序言检查

每个函数入口，编译器都插入类似这样的汇编：

```asm
TEXT foo(SB), $frame_size-0
    MOVQ (TLS), CX          ; 拿到当前 g
    CMPQ SP, 16(CX)         ; SP 和 g.stackguard0 比
    JLS  morestack           ; SP <= stackguard0 → 跳去扩容
    ; ... 正常函数体
```

**只有大函数才检查**：帧小于 `StackSmall`（128B）的函数会省略这步（反正撑不爆预警线）。

### 步骤 2：morestack 保存现场

```
morestack (汇编)：
    保存 PC / SP / BP 到 g.sched
    切换到 g0 系统栈
    调用 newstack()
```

### 步骤 3：newstack 决策

```go
// runtime/stack.go 伪代码
func newstack() {
    gp := getg().m.curg
    
    // 检查是否是抢占请求（stackguard0 == stackPreempt）
    if gp.stackguard0 == stackPreempt {
        gopreempt_m(gp)  // 主动让出，回调度循环
        return
    }
    
    // 真的是栈不够
    oldsize := gp.stack.hi - gp.stack.lo
    newsize := oldsize * 2               // ★ 两倍扩容
    if newsize > maxstacksize {          // 默认 1GB
        throw("stack overflow")
    }
    
    copystack(gp, newsize)               // 分配新栈 + 拷贝 + 修正指针
    gogo(&gp.sched)                      // 恢复到函数序言，重新执行检查（这次会过）
}
```

### 步骤 4：copystack 关键难点——指针修正

栈上有些变量存的是**指针**，而这些指针可能指向**同一个栈内的其他变量**：

```go
func foo() {
    a := 42
    p := &a       // p 是栈上变量，值是 &a（也在栈上）
    bar(p)
}
```

如果直接 `memcpy(newStack, oldStack)`，`p` 的值还是旧栈里 `a` 的地址——旧栈马上释放，`p` 就成了野指针。

**copystack 的解法**：

```go
func copystack(gp *g, newsize uintptr) {
    old := gp.stack
    new := stackalloc(newsize)
    
    // 1. 拷贝栈内容
    memmove(new.hi - used, old.hi - used, used)
    
    // 2. 遍历栈上所有帧，用类型信息（GC 位图）找出指针字段
    //    对每个指向 [old.lo, old.hi) 的指针 p：
    //    p += (new.lo - old.lo)     // 重定位到新栈
    adjustpointers(...)
    
    // 3. 修正 g.sched 里保存的 SP / BP
    adjustsudogs(...)
    
    // 4. 释放旧栈
    stackfree(old)
    gp.stack = new
    gp.stackguard0 = new.lo + StackGuard
}
```

**这就是为什么 Go 栈变量可以自由取地址**——runtime 保证复制时修正所有指针。

---

## 五、"栈上变量地址会变吗"？

**面试红牌陷阱题**。答案：

- **同一个函数内，短期内看：不会变**（除非中途触发栈扩容）
- **跨函数调用、跨 goroutine 保留地址：可能变**（扩容会改地址）

看这段代码：

```go
func main() {
    x := 42
    fmt.Printf("%p\n", &x)     // 打印一次
    growStack()                 // 疯狂递归触发扩容
    fmt.Printf("%p\n", &x)     // 再打印——地址变了！
}
```

**但对用户是透明的**：`&x` 变量本身也在栈上，扩容时被自动重定位，所有引用都指向新地址，逻辑正确。

**逃逸分析的介入**：如果编译器**看到指针可能逃出当前函数**（比如返回 `&x`），就把 x 分配到堆上——堆地址稳定，不受栈扩容影响。

```go
func escape() *int {
    x := 42
    return &x   // 逃逸到堆，&x 永远稳定
}
```

---

## 六、栈缩容

只增不缩会浪费——一个偶尔用了大栈的 goroutine 会一直占着。

### 触发时机

**GC 标记结束时**，遍历所有 goroutine 检查：

```
if 当前栈使用量 < 栈总大小 / 4:
    缩容到栈总大小 / 2
```

即"实际使用不到 1/4 就砍半"。

### 缩容也是 copystack

调 `copystack(gp, oldsize/2)`，走同一套指针修正流程。缩容和扩容共用一份代码。

### 最小栈

不会缩到比 `StackMin`（2KB，amd64）更小。

---

## 七、特殊栈：g0 与信号栈

### g0：M 的系统栈

每个 M 有一个 `g0`，是**系统栈**（不是用户 goroutine）：

- **固定大小**（Linux amd64 上 8KB，Windows 上 32KB）
- **不会扩容**（超了就直接 crash）
- 用途：执行调度代码、GC 代码、morestack 本身

**为什么调度代码要在 g0 上跑**：调度代码可能修改当前 g 的栈，如果在自己栈上跑就等于"拆自己坐的椅子"。

```
用户 G 栈：跑用户代码
    ↓ morestack
切换到 g0 栈
    ↓
在 g0 栈上执行 newstack、copystack ...
    ↓
切换回用户 G 的新栈
```

### signal 栈

M 处理信号时用独立的 `gsignal` 栈（也是固定大小），避免信号处理器污染用户栈。

---

## 八、栈溢出与最大栈

### 硬上限

```go
// runtime/proc.go
var maxstacksize uintptr = 1 << 30   // 默认 1GB

func SetMaxStack(n int) int {
    ...  // 通过 debug.SetMaxStack 修改
}
```

超过就 `throw("stack overflow")` 直接崩溃（不是 panic，无法 recover）。

### 无限递归会怎样

```go
func recurse() { recurse() }

func main() { recurse() }
// runtime: goroutine stack exceeds 1073741824-byte limit
// runtime: sp=... stack=[...]
// fatal error: stack overflow
```

不断扩容到 1GB 就 crash。**注意这不是 panic，defer 都不会跑**。

---

## 九、版本演进

| 版本 | 变化 |
|-----|-----|
| Go 1.0 | 分段栈，8KB 初始 |
| Go 1.2 | 引入栈缩容 |
| **Go 1.3** | **分段栈 → 连续栈**，解决 hot split |
| Go 1.4 | 初始栈从 8KB 降到 2KB |
| Go 1.5 | 栈拷贝优化，配合三色 GC |
| Go 1.14 | 基于信号的抢占，`stackguard0` 复用为抢占标志 |
| Go 1.19+ | 栈大小提示 `runtime/debug.SetGCPercent` 影响缩容策略 |

---

## 十、面试高频题

### Q1: goroutine 栈初始多大？为什么这么小？

**2KB**（Go 1.4+，amd64）。

小的原因：
1. **省内存**：百万 goroutine × 2KB = 2GB，可承受；如果 8MB × 100 万 = 8TB，不可能
2. **多数 goroutine 用不完**：短平快的处理函数（HTTP handler、消息消费）远远用不到大栈
3. **有动态扩容兜底**：真需要大栈的场景（深度递归、大局部数组）会自动扩

代价是**每次函数调用要做栈溢出检查**，但检查只是一条 CMP + JMP，可忽略。

### Q2: 栈是怎么扩容的？

**三步走**（详见正文第四节）：

1. 函数序言：`SP <= stackguard0` → 跳 `morestack`
2. `morestack` 切到 g0 栈，调 `newstack`
3. `newstack` → `copystack`：分配 2 倍大的新栈，`memmove` 拷贝，**遍历栈帧修正所有指向旧栈的指针**，释放旧栈，`gogo` 回到序言重试

**关键点**：扩容单位是 2 倍；上限 1GB；指针修正靠 GC 位图找出栈上的指针字段。

### Q3: 分段栈和连续栈的区别？为什么切换？

| 维度 | 分段栈 (Go 1.2-) | 连续栈 (Go 1.3+) |
|-----|-----------------|------------------|
| 数据结构 | 链表串多段 | 单块连续内存 |
| 扩容 | 分配新段 | 分配 2 倍新栈 + 拷贝 |
| Cache 友好 | ❌ 跨段访问差 | ✅ 单块内存 |
| Hot split | ❌ 循环内跨段反复分配 | ✅ 无 |
| 代价 | 段管理开销 | 扩容时拷贝 + 指针修正 |

切换核心原因：**hot split 无法接受**——即使发生率不高，一旦发生就是循环级别的性能崩塌。

### Q4: 栈缩容什么时候发生？

**GC 标记结束时**，遍历所有 g：

```
if 栈使用量 < 栈容量 / 4:
    缩到 栈容量 / 2
```

也用 `copystack`（同一套代码，只是新栈更小）。最小不会低于 2KB。

### Q5: 栈上变量的地址会变吗？

**会**，栈扩容/缩容时整个栈被拷贝到新内存，**所有栈上变量地址都改**。

但对用户代码透明：栈上指针会被 runtime 一起重定位，业务逻辑不受影响。

**要注意的场景**：
- 不要把 `uintptr` 存起来当"栈上地址"用——`uintptr` 不是指针，GC 不会更新它
- 不要用 `unsafe.Pointer` 存"当前栈地址"传给 C

### Q6: 栈上取地址（`&x`）安全吗？为什么不像 C 一样出问题？

安全。两层保护：

1. **栈内使用**：地址随扩容变，但 runtime 自动重定位
2. **逃出函数**：逃逸分析会把它挪到堆上，返回后栈帧销毁也没事

```go
func f() *int {
    x := 42
    return &x   // 编译器发现 x 逃逸 → 堆分配
}
```

对比 C：C 里返回栈变量地址就是 UB，因为没有逃逸分析也没有 GC。

### Q7: g0 栈和用户 goroutine 栈的区别？

| 维度 | 用户 g 栈 | g0 栈（系统栈） |
|-----|----------|----------------|
| 初始大小 | 2KB | 8KB / 32KB |
| 能否扩容 | ✅ 动态扩容 | ❌ 固定 |
| 跑什么 | 用户代码 | 调度器、GC、morestack |
| 数量 | 每 goroutine 一个 | 每 M 一个 |

**为什么调度代码要在 g0 上跑**：调度器要操作用户 g 的栈（切换、扩容），必须站在另一个栈上操作，不能站在自己要动的栈上。

### Q8: 什么是 `stackguard0`？为什么要留 896 字节余量？

`stackguard0 = stack.lo + StackGuard`，**栈溢出预警线**（不是真实栈底）。

留 896 字节余量是给：
- `morestack` 汇编自身的调用开销
- runtime 内部函数（比如 `newstack`）的最小栈需求

不留余量的话：刚过预警线就调 `morestack`，而 `morestack` 自己又要压栈——立刻真实溢出。

**双重身份**：Go 1.14 后，`stackguard0` 被置为 `0xfffffade` 表示"请求抢占"。这样一次 `CMP` 同时做栈检查 + 抢占检查——省一条指令。

### Q9: 栈溢出会 panic 吗？能 recover 吗？

**不能**。栈溢出走 `throw` 路径，是**致命错误**（fatal error），进程直接崩溃：

```
runtime: goroutine stack exceeds 1073741824-byte limit
fatal error: stack overflow
```

defer 不执行、recover 无效。因为溢出时连"跑 defer 的栈空间"都没有。

要避免：不要写无限递归；深递归改迭代或加深度限制。

### Q10: 一个 goroutine 结束后，它的栈去哪了？

goroutine 结束进 `_Gdead` 状态，**放入 P 的 `gFree` 链表复用**：

- 栈内存不释放，等下个 `go func()` 拿到复用它的 g
- 复用时只重置栈内容，不重新 malloc
- 大栈的 g 有单独的 `gFreeStack` 队列，小栈的走 `gFreeNoStack`

好处：**避免频繁 malloc/free 栈内存**，减少 GC 压力。

### Q11: 深递归和大数组，哪个更容易触发栈扩容？

都会，但表现不同：

- **深递归**：每层调用压入一个栈帧，扩容次数多、频繁——每次翻倍最终能撑到 1GB
- **大数组（局部变量）**：单次帧就很大，编译器可能一次性判断栈不够，直接触发扩容；或者**逃逸到堆**（局部大对象常见做法）

```go
func f() {
    var big [10 * 1024 * 1024]byte  // 10MB
    _ = big
}
// 逃逸分析可能把它移到堆，避免栈爆炸
```

看逃逸情况用 `go build -gcflags="-m"`。

### Q12: 如何观察 goroutine 栈的当前大小？

```go
import "runtime/debug"

buf := debug.Stack()   // 打印当前 goroutine 栈跟踪
```

或者性能分析：

```
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

生产上要看栈内存汇总：`runtime.MemStats.StackInuse`（所有 goroutine 栈总占用）。

---

## 十一、延伸阅读

- Go 官方设计文档：[Contiguous stacks](https://docs.google.com/document/d/1wAaf1rYoM4S4gtnPh0zOlGzWtrZFQ5suE8qr2sD8uWQ)
- `runtime/stack.go`：源码，`copystack` / `newstack` 是精华
- Dave Cheney：《[Goroutine stack size](https://dave.cheney.net/2013/06/02/why-is-a-goroutines-stack-infinite)》

---

**回到 [Runtime 目录](./README.md)**
