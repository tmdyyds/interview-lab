# Go 内存分配器深度剖析

**标签**: #go #runtime #memory #allocator #高频

Go 的内存分配器基于 Google 的 **tcmalloc**（Thread-Caching Malloc）思想，用**多级缓存 + Size Class**实现高性能无锁分配。

---

## 一、为什么需要复杂的分配器

用最原始的 `malloc` 会遇到几个问题：

1. **锁竞争**：多个 goroutine 同时分配 → 争抢全局锁
2. **碎片化**：不同大小对象混着分配 → 内存空洞
3. **系统调用开销**：每次 `mmap` 都陷入内核

tcmalloc 思路：**分级缓存 + 按大小分类**。

---

## 二、三级分配架构

```
┌──────────────────────────────────────────────────────────────┐
│                       OS (mmap/munmap)                        │
└──────────────────────────────┬───────────────────────────────┘
                               │
                    ┌──────────▼──────────┐
                    │       mheap         │  ← 全局堆
                    │  (整个进程唯一)      │     管理所有内存
                    │                     │     以 page (8KB) 为单位
                    │  freelist / arena   │
                    └──────────┬──────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
      ┌───────▼──────┐  ┌──────▼───────┐  ┌────▼─────────┐
      │  mcentral    │  │  mcentral    │  │  mcentral    │
      │  size=8      │  │  size=16     │  │  size=32 ... │
      │              │  │              │  │              │
      │  按大小分类   │  │              │  │              │
      │  (~70 种)    │  │              │  │              │
      └───────┬──────┘  └──────┬───────┘  └────┬─────────┘
              │                │                │
      ┌───────▼─────────────────▼─────────────── ▼──────┐
      │                                                  │
      │  每个 P 一个 mcache（无锁）                       │
      │                                                  │
      │  P0.mcache: [span8, span16, span32, ...]         │
      │  P1.mcache: [span8, span16, span32, ...]         │
      │  P2.mcache: [span8, span16, span32, ...]         │
      │  ...                                              │
      │                                                  │
      └─────────────────┬────────────────────────────────┘
                        │
                  用户 goroutine
                  malloc → 从当前 P.mcache 拿
```

**三级关系**：

| 层级 | 归属 | 锁 | 作用 |
|-----|------|---|------|
| **mcache** | 每个 P | 无锁 | 快速分配，最热的一层 |
| **mcentral** | 全局，按 size class 分 | 有锁（细粒度） | mcache 缺货时补货 |
| **mheap** | 全局唯一 | 有锁 | 从 OS 申请大块内存，切成 span |

---

## 三、核心概念

### 1. Page（页）—— OS 单位

- Linux 默认 4KB，Go 用 **8KB**（`_PageSize = 8192`）
- 是 mheap 管理的最小单位

### 2. Span —— Go 的核心分配单元

**Span = 连续的 N 个页**，专门存**同一 size class 的对象**。

```go
// runtime/mheap.go
type mspan struct {
    next *mspan
    prev *mspan

    startAddr uintptr    // 起始地址
    npages    uintptr    // 包含几个页
    freeindex uintptr    // 下一个可分配对象的位置
    nelems    uintptr    // 总对象数
    allocBits *gcBits    // 哪些位置被占用（bitmap）
    gcmarkBits *gcBits   // GC 标记 bitmap
    allocCount uint16    // 已分配对象数
    spanclass  spanClass // size class + 是否包含指针
    state      mSpanStateBox
    // ...
}
```

**图示**：一个 span 里的对象排布

```
一个 size=32 的 span，占 1 页（8KB）：
可以放 8192/32 = 256 个 32 字节对象

┌────┬────┬────┬────┬────┬────┬────┬────┐
│Obj │Obj │Obj │Obj │Obj │    │Obj │    │
│ 0  │ 1  │ 2  │ 3  │ 4  │ 5  │ 6  │ 7  │ ...
└────┴────┴────┴────┴────┴────┴────┴────┘
allocBits: 11111 011 0 ...
                 ↑
              第 5 个是空的（被释放了）
```

分配对象 = 从 allocBits 找一个 0，标记为 1，返回对应位置。

### 3. Size Class（大小类）

Go 预定义了 **~70 种 size class**，避免为每个大小都做一个 span：

```
size class 1  → 8 字节
size class 2  → 16 字节
size class 3  → 32 字节
size class 4  → 48 字节
size class 5  → 64 字节
size class 6  → 80 字节
size class 7  → 96 字节
size class 8  → 112 字节
size class 9  → 128 字节
size class 10 → 144 字节
...
size class 67 → 32768 字节 (32KB)
```

**分配时**：向上取整到最近的 size class。

```
申请 20 字节 → 用 size class 3 (32 字节)     浪费 12 字节
申请 33 字节 → 用 size class 4 (48 字节)     浪费 15 字节
申请 65 字节 → 用 size class 5 (80 字节)     浪费 15 字节

平均内碎片：约 10-15%（可接受）
```

**每个 size class 有两种 span**：包含指针的 / 不包含指针的（GC 扫描时可以跳过无指针 span）→ 总共约 140 种 span class。

**为什么这样设计**：所有对象的分配都归到有限的 size class → 大幅减少碎片和管理复杂度。

---

## 四、分配流程

### 小对象（<= 32KB）—— 走三级

```go
// runtime/malloc.go 简化
func mallocgc(size uintptr, typ *_type, needzero bool) unsafe.Pointer {
    if size <= maxSmallSize {  // 32KB
        if size <= maxTinySize {  // 16 字节
            return tinyAlloc(...)   // 微小对象，特殊处理（后面讲）
        }

        // 1. 计算 size class
        sizeclass := size_to_class[divRoundUp(size, smallSizeDiv)]
        spc := makeSpanClass(sizeclass, noscan)

        // 2. 从当前 P 的 mcache 拿对应 span
        c := getMCache()
        span := c.alloc[spc]

        // 3. 从 span 里找空位
        v := nextFreeFast(span)
        if v == 0 {
            v, span = c.nextFree(spc)  // 慢路径：换 span 或找 mcentral
        }
        return unsafe.Pointer(v)
    }

    // 大对象走 largeAlloc
    return largeAlloc(size, ...)
}
```

**逐层回退**：

```
Step 1: mcache.alloc[spc] 里的 span 有空位吗？
  是 → 返回，结束（最快，纳秒级）
  否 → 下一步

Step 2: mcentral 里有空 span 吗？
  是 → 从 mcentral 取一个 span 给 mcache，然后回 Step 1
  否 → 下一步

Step 3: mheap 里有空 span 吗？
  是 → 从 mheap 切出 span 给 mcentral
  否 → 下一步

Step 4: 从 OS 申请内存（mmap）
  → mheap 拿到新页，切成 span 给 mcentral
```

**热路径**：99% 的分配都在 Step 1 就完成（mcache 有货），**纳秒级 + 无锁**。

### 大对象（> 32KB）—— 直接从 mheap

```go
func largeAlloc(size uintptr, ...) unsafe.Pointer {
    npages := size >> _PageShift
    if size&_PageMask != 0 {
        npages++
    }

    // 直接向 mheap 申请 npages 个连续页
    s := mheap_.alloc(npages, spanClass, needzero)
    return unsafe.Pointer(s.base())
}
```

**大对象没有 size class**，直接按需申请，独占整个 span。

### 微小对象（<= 16 字节，noscan）—— tinyAlloc

```go
// 特殊优化：多个微小对象合并到一个 16 字节块里
if size <= maxTinySize {
    off := c.tinyoffset
    if off+size <= 16 {
        // 复用当前 tiny 块
        x := unsafe.Pointer(c.tiny + off)
        c.tinyoffset = off + size
        return x
    }
    // 当前块用完，取新的 16 字节块
    // ...
}
```

**目的**：`bool`、`byte` 之类的小对象不至于每个都占一个独立的 16 字节 size class 位置。

---

## 五、mcache 结构

```go
// runtime/mcache.go
type mcache struct {
    tiny       uintptr    // tiny 分配器
    tinyoffset uintptr

    // 每个 size class 一个 span 指针
    // numSpanClasses = ~140（size class × 2 for scan/noscan）
    alloc [numSpanClasses]*mspan

    // 用于 stack 分配（stack 也走 mcache）
    stackcache [_NumStackOrders]stackfreelist

    // 累计分配统计
    scanAlloc uintptr
    // ...
}
```

**关键**：`alloc [numSpanClasses]*mspan`，每个 size class 一个 span 指针。分配时直接索引到 span，无锁。

---

## 六、mcentral 结构

```go
// runtime/mcentral.go
type mcentral struct {
    spanclass spanClass  // 这个 mcentral 服务哪个 size class

    partial [2]spanSet  // 部分空闲的 span
    full    [2]spanSet  // 完全占满的 span

    // [2] 用于 sweep 前后的两组，交替使用
}
```

**为什么有 partial 和 full**：
- `partial`：有空位的 span，mcache 缺货来这里拿
- `full`：满的 span，等 GC 释放对象后重新变 partial

**为什么 [2] 数组**：GC 扫描时和分配同时进行，用两组交替避免冲突。

---

## 七、mheap 结构

```go
// runtime/mheap.go
type mheap struct {
    lock mutex

    pages pageAlloc  // 页分配器（Go 1.14+ 引入）

    allspans []*mspan  // 所有的 span

    // 空闲 span 按大小组织
    central [numSpanClasses]struct {
        mcentral mcentral
        pad      [cpu.CacheLinePadSize - unsafe.Sizeof(mcentral{})%cpu.CacheLinePadSize]byte
    }

    arenas [1 << arenaL1Bits]*[1 << arenaL2Bits]*heapArena  // arena 数组
    // ...
}
```

**注意**：**mcentral 是 mheap 的一部分**（`mheap.central[]`），只是逻辑上分层。

### Arena：向 OS 申请内存的单位

Go 从 OS 申请内存的最小单位是 **arena**：
- Linux 64位：64MB
- Windows / 32位：4MB

```
一次 mmap 64MB → 得到一个 arena
mheap 把 arena 切成 pages (8KB 每页)
pages 组合成 spans
spans 服务 mcentral / mcache
```

---

## 八、无锁的秘密

### mcache 完全无锁

每个 P 一个 mcache，同一时刻只有一个 goroutine 在这个 P 上跑 → **不需要锁**。

```go
// runtime/mcache.go
func getMCache() *mcache {
    // 当前 P 的 mcache
    return getg().m.p.ptr().mcache
}
```

**这就是 P 存在的另一个重要意义**（除了 GMP 调度）：**为无锁内存分配提供 goroutine → P → mcache 的绑定**。

### mcentral 细粒度锁

mcentral 按 size class 分，每个 mcentral 一把锁。竞争范围只在"同 size class 且都需要向 mcentral 补货"的场景。

### mheap 全局锁

只在**新申请 arena** 或**大块 span 调度**时才碰这把锁，频率很低。

---

## 九、GC 与内存分配的交互

### span 的三种状态

```go
// runtime/mheap.go
type mSpanState uint8
const (
    mSpanDead   mSpanState = iota  // 已释放
    mSpanInUse                     // 在用
    mSpanManual                    // 手动管理（栈用）
)
```

### GC 清扫过程

```
1. GC 标记完成，进入并发清扫
2. 遍历所有 span
3. 对每个 span：
   - 看 allocBits & gcmarkBits 差异
   - 如果整个 span 都没有存活对象 → 归还给 mheap
   - 如果部分存活 → 更新 allocBits = gcmarkBits，重新变成 partial
```

### 内存归还给 OS

Go 不会立刻把内存还给 OS（还给 OS 后又要用还要 mmap）：

```
Go 1.13+：MADV_FREE  → 通知 OS "这块内存我不用了，你可以拿走"
                       但如果 OS 没内存压力，物理页还留着
Go 1.16+：MADV_DONTNEED  → 更激进
```

**观察**：`RSS`（实际占用）可能高于 `HeapInuse`（Go 认为在用），因为 OS 还没回收。

---

## 十、结构体字段排布：内存对齐

Go 的内存分配按 size class 走，但**结构体内部字段顺序**会影响大小。

### 例子

```go
// ❌ 字段顺序不好
type BadStruct struct {
    a bool    // 1 字节
    b int64   // 8 字节
    c bool    // 1 字节
}
// 实际大小：24 字节（对齐 padding）
// 1 + 7 padding + 8 + 1 + 7 padding = 24

// ✅ 字段顺序优化
type GoodStruct struct {
    b int64   // 8
    a bool    // 1
    c bool    // 1
}
// 实际大小：16 字节
// 8 + 1 + 1 + 6 padding = 16
```

**规则**：
- Go 内存对齐按字段最大类型的对齐值（一般 8 字节）
- 相邻字段类型对齐要求相同则不 padding
- **建议：大字段在前，小字段在后**

用 `unsafe.Sizeof` 验证：

```go
fmt.Println(unsafe.Sizeof(BadStruct{}))   // 24
fmt.Println(unsafe.Sizeof(GoodStruct{}))  // 16
```

对**百万级实例**的场景，这个差异可以省几十 MB 内存。

---

## 十一、生产观察

### runtime.MemStats

```go
var m runtime.MemStats
runtime.ReadMemStats(&m)

fmt.Printf("HeapAlloc:    %d MB\n", m.HeapAlloc/1024/1024)   // Go 眼中的堆使用
fmt.Printf("HeapSys:      %d MB\n", m.HeapSys/1024/1024)     // 从 OS 拿到的堆内存
fmt.Printf("HeapIdle:     %d MB\n", m.HeapIdle/1024/1024)    // 空闲的
fmt.Printf("HeapReleased: %d MB\n", m.HeapReleased/1024/1024)// 归还给 OS 的

fmt.Printf("Sys:          %d MB\n", m.Sys/1024/1024)         // 从 OS 拿到的总内存
fmt.Printf("NumGC:        %d\n",    m.NumGC)
```

**关系**：`HeapSys = HeapInuse + HeapIdle + HeapReleased`

### pprof 看分配热点

```bash
# allocs：累计分配（找频繁分配的地方）
go tool pprof http://localhost:6060/debug/pprof/allocs
(pprof) top
(pprof) list funcName
```

---

## 十二、优化技巧

### 1. sync.Pool 复用对象

减少分配和 GC 压力（详见 GC 章节）。

### 2. 预分配 slice / map

```go
s := make([]int, 0, 1000)  // 预分配容量 1000
m := make(map[string]int, 1000)
```

避免频繁扩容 + 分配。

### 3. 用值类型 vs 指针类型

```go
// 值类型：栈上分配（除非逃逸）
type User struct { Name string; Age int }
u := User{}  // 栈

// 指针类型：更容易逃逸到堆
u := &User{}  // 可能栈可能堆，看逃逸分析
```

**规则**：小的（<= 128 字节）、生命周期短的 → 值类型；大的、共享的 → 指针类型。

### 4. 字节缓冲复用

```go
// ❌ 每次分配新 buffer
func handle() {
    var buf bytes.Buffer
    buf.WriteString("hello")
}

// ✅ 用 sync.Pool
var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
func handle() {
    buf := bufPool.Get().(*bytes.Buffer)
    defer func() { buf.Reset(); bufPool.Put(buf) }()
    buf.WriteString("hello")
}
```

### 5. 结构体字段重排

见上一节，大字段在前。工具：`fieldalignment`（golang.org/x/tools/go/analysis/passes/fieldalignment）。

---

## 十三、面试高频题

### Q1: Go 内存分配的核心思想？

**tcmalloc（Thread-Caching Malloc）+ Size Class**：
- 多级缓存：mcache（P 私有）→ mcentral（全局按大小分）→ mheap（全局）
- 按大小分类：预定义 ~70 种 size class，向上取整
- 大部分分配走 mcache 无锁纳秒级

### Q2: 三级架构里，每一级的作用？

- **mcache**：每 P 一个，无锁，最快
- **mcentral**：全局按 size class 分，mcache 缺货时补货
- **mheap**：全局唯一，向 OS 申请大块内存切成 span

### Q3: Size Class 是什么？为什么这样设计？

Go 预定义 ~70 种对象大小（8, 16, 32, ...），分配时向上取整。**减少碎片 + 简化管理**：
- 同大小对象放一起，回收后能立刻被同大小的新对象复用
- 只需要 ~70 个空闲链表，而不是无限多种

**代价**：约 10-15% 的内部碎片（申请 20 字节实际给 32）。

### Q4: 小对象、大对象、微小对象的分配路径？

- **微小对象（≤16B 且 noscan）**：tinyAlloc，多个合并到 16B 块
- **小对象（≤32KB）**：mcache → mcentral → mheap 三级
- **大对象（>32KB）**：直接从 mheap 分配连续页

### Q5: mcache 为什么可以无锁？

每个 P 一个 mcache。同一时刻只有一个 goroutine 在这个 P 上跑，不存在并发访问。这就是 P 存在的另一个重要意义（除 GMP 调度外）。

### Q6: Go 内存归还给 OS 吗？

不立刻归还。用 `MADV_FREE`（Go 1.13+）通知 OS 可以拿走，但物理页还在。**RSS 可能比 HeapInuse 大**（OS 没回收）。

内存压力大时 OS 会真正回收。

### Q7: 结构体字段顺序会影响什么？

**大小和对齐**。Go 按最大类型对齐（一般 8 字节），字段乱序可能造成 padding：

```go
struct {a bool; b int64; c bool}  // 24 字节
struct {b int64; a bool; c bool}  // 16 字节
```

**规则**：大字段在前，小字段在后。工具 `fieldalignment` 自动检查。

### Q8: 什么是 Span？

**连续的 N 个 8KB 页**，服务同一个 size class 的对象。span 内部维护 bitmap，分配时找空位标记，回收时清标记。

### Q9: 分配的对象在栈还是堆？

由**逃逸分析**决定（详见 04-escape-analysis.md）。栈上零成本，堆上要走 mcache/mcentral/mheap 分配 + GC 扫描。

### Q10: sync.Pool 是怎么复用的？和 mcache 什么关系？

sync.Pool 是**应用层**的对象池，跟 mcache 不同：
- mcache 分配**任意类型**的内存（Go runtime 管理）
- sync.Pool 缓存**特定类型**的对象（业务代码手动 Put/Get）
- Pool 里的对象**会被 GC 清空**（Go 1.13+ 有 victim cache 缓解一次）

Pool 是"减少 GC 压力"的工具，不是"内存分配器"。

### Q11: Go 有内存泄漏吗？

有。GC 只回收"不可达"对象，如果**引用一直存在**就不会回收：
- 全局 map 只加不删
- goroutine 泄漏（引用大量对象）
- 长生命周期对象持有短生命周期对象的引用

排查用 pprof heap。

### Q12: 内存分配的性能怎么优化？

1. **减少堆分配**（栈分配零成本）
2. **sync.Pool** 复用临时对象
3. **预分配 slice / map**
4. **结构体字段重排**（省内存）
5. **合并小对象**（省对齐 padding）
6. **考虑值类型 vs 指针**（小对象值类型更好）
