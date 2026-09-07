# GC 与写屏障深度剖析

**标签**: #go #runtime #gc #write-barrier #高频

Go GC 是**并发三色标记 + 混合写屏障**。理解这一句话是入门，但面试要讲透原理、演进和坑，需要更深的知识。

---

## 一、GC 演进史（面试常问版本对比）

| 版本 | 关键变化 | STW |
|-----|---------|-----|
| Go 1.0 | 标记-清扫（Mark & Sweep），全程 STW | 秒级 |
| Go 1.3 | 精确 GC（准确识别指针） | 秒级 |
| Go 1.5 | **三色标记 + 并发 GC**（里程碑） | ~10ms |
| Go 1.7 | 增量优化 | ~1ms |
| Go 1.8 | **混合写屏障** | **~100us** |
| Go 1.14 | 页分配器优化 | 更低 |
| Go 1.19 | 引入 `GOMEMLIMIT` 软内存上限 | - |
| Go 1.20+ | 内存分配优化、Arena（实验） | - |

**为什么 GC 一路优化 STW**：Go 的招牌是"低延迟"，STW 时间过长会打破这个卖点。

---

## 二、三色标记法基础

### 三种颜色

```
白色 (White)：初始状态，未访问 → GC 结束还是白色的就被回收
灰色 (Gray)：已发现但未处理（有指向白色对象的引用）
黑色 (Black)：已处理完（自己和所有引用都已扫描）
```

### 标记流程

```
初始：所有对象都是白色
        ↓
  从 GC roots 出发（全局变量、每个 g 的栈）
        ↓
  把 root 引用的对象染成灰色
        ↓
  从灰色对象里取一个 → 扫描它的引用
        ├── 引用的白色对象 → 染成灰色
        └── 处理完的这个对象 → 变黑
        ↓
  重复，直到没有灰色对象
        ↓
  剩下的白色对象全部回收
```

### 图示

```
Roots
  │
  ▼
[Obj A] ──→ [Obj B] ──→ [Obj C]
                          │
                          ▼
                        [Obj D]

Step 1: A 从 root 变灰
White: B, C, D    Gray: A

Step 2: 扫描 A → B 变灰，A 变黑
White: C, D    Gray: B    Black: A

Step 3: 扫描 B → C 变灰，B 变黑
White: D       Gray: C    Black: A, B

Step 4: 扫描 C → D 变灰，C 变黑
White: -       Gray: D    Black: A, B, C

Step 5: 扫描 D → D 变黑
White: -       Gray: -    Black: A, B, C, D

结束：没有白色，回收 0 个对象
```

如果 C 不在引用链上：

```
Roots ── A ── B
              (原本 B → C，现在断了)

结束：C 是白色 → 回收
```

---

## 三、并发 GC 的挑战：漏标问题

STW 时代 GC 和用户代码不并发，没问题。**并发 GC 时用户代码在改指针，可能造成对象被错误回收**。

### 漏标场景

```
初始状态：
  A(黑) ──→ B(灰) ──→ C(白)

用户代码执行：
  1. A.next = C     ← 让黑色 A 指向白色 C
  2. B.next = nil   ← B 不再指向 C

GC 视角：
  B 已经处理完（灰→黑），不会再重新扫描
  A 已经黑了，也不会重新扫描
  C 从灰色路径断开，永远是白色
  ↓
  C 被错误回收！但用户代码里 A 还引用它 → 悬空指针 → 崩溃
```

**根本原因**：
- **黑色对象不再扫描**（GC 认为处理完了）
- **灰色对象的引用被删除**（丢失了到白色的路径）

### 三色不变性（Tricolor Invariant）

学术上定义两条"不变性"，只要保证一条就没有漏标：

**强三色不变性**：黑色对象不能引用白色对象
**弱三色不变性**：黑色可以引用白色，但**这个白色必须有其他灰色对象能到达它**

写屏障就是"在指针写入时做补偿"来维持不变性。

---

## 四、写屏障：三种经典方案

### 1. Dijkstra 写屏障（插入屏障）

**规则**：黑色对象 → 白色对象写入时，**把白色染成灰色**。

```go
// 伪代码：Dijkstra 写屏障
func writeBarrier(slot *unsafe.Pointer, ptr unsafe.Pointer) {
    shade(ptr)  // 把新写入的指针指向的对象染灰
    *slot = ptr
}

// 场景
A(黑).next = C(白)
     ↓
写屏障介入：先把 C 染成灰
     ↓
A(黑) ──→ C(灰)   ✅ 满足强三色不变性
```

**优点**：直接明了
**缺点**：**栈上的对象也要屏障**，性能开销大。栈操作极频繁，全屏障不现实。

### 2. Yuasa 写屏障（删除屏障）

**规则**：删除某个引用时，**把被删除的目标染灰**（防止它没被 GC 到）。

```go
func writeBarrier(slot *unsafe.Pointer, ptr unsafe.Pointer) {
    shade(*slot)  // 把旧值染灰（防止它成为不可达）
    *slot = ptr
}

// 场景
B(灰).next = nil     ← 删除 B → C 的引用
                      ↓
                    写屏障：先把 C 染灰
                      ↓
B(灰) ─X→ C(灰)     C 已被染灰，会被继续扫描 ✅
```

**优点**：栈也可以避免（GC 开始前把所有栈上的可达对象都染灰）
**缺点**：**GC 开始时需要 STW 扫描所有栈**（Go 1.5-1.7 就这样）

### 3. 混合写屏障（Go 1.8+）

**结合 Dijkstra + Yuasa**：

```go
// runtime/mbarrier.go 简化
func writePointer(slot *unsafe.Pointer, ptr unsafe.Pointer) {
    shade(*slot)   // 删除屏障：染灰旧值
    shade(ptr)     // 插入屏障：染灰新值
    *slot = ptr
}
```

**关键突破**：**GC 开始时，把所有栈上可达的对象直接染黑**（一次性），之后**栈上不需要屏障**（栈上永远是黑）。

**为什么栈上可以不屏障**：
- 栈是 goroutine 私有的
- 一开始就全染黑，之后栈内部指针变化不会影响堆的 GC
- 只在栈到堆的写才需要屏障

这样 STW 从"扫描所有栈"（1.5-1.7）变成"只做一次栈黑化"（1.8+），**STW 时间从 100ms 降到 100us**。

---

## 五、Go GC 完整流程

```
GC 阶段（Go 1.8+）：
┌─────────────────────────────────────────────────────────────┐
│ 1. GC 触发                                                  │
│    条件之一：                                                │
│    - 堆增长到 GOGC 阈值（默认 100%，即上次 GC 后翻倍）        │
│    - 手动 runtime.GC()                                       │
│    - 距上次 GC 超过 2 分钟（sysmon 强制）                    │
│    - Go 1.19+ 接近 GOMEMLIMIT                                │
└──────────────────────┬──────────────────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. STW: Sweep Termination（清扫终止，微秒级）                │
│    - 完成上一轮的清扫（如果还在进行）                        │
│    - 准备本轮 GC                                             │
└──────────────────────┬──────────────────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. STW: Mark Setup（标记准备，微秒级）                       │
│    - 开启写屏障                                              │
│    - 把每个 g 的栈上根对象染黑（栈黑化）                     │
│    - 准备 GC roots（全局变量、寄存器等）                     │
└──────────────────────┬──────────────────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. 并发标记（Concurrent Marking，主要耗时阶段）              │
│    - 与用户代码并发执行                                      │
│    - GC 协程（gcBgMarkWorker）从灰色队列取对象扫描           │
│    - 用户代码写指针触发写屏障 → 维持三色不变性                │
│    - CPU 分配：默认 25% 给 GC                                │
│    - 用户 goroutine 分配对象时可能被强制"助攻"（mark assist）│
└──────────────────────┬──────────────────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────────────────┐
│ 5. STW: Mark Termination（标记终止，微秒级）                 │
│    - 关闭写屏障                                              │
│    - 处理残留的灰色对象                                      │
│    - 统计信息                                                │
└──────────────────────┬──────────────────────────────────────┘
                       ▼
┌─────────────────────────────────────────────────────────────┐
│ 6. 并发清扫（Concurrent Sweep）                              │
│    - 与用户代码并发                                          │
│    - 遍历 heap，把白色对象回收（放回 mcentral 或 mheap）     │
│    - 一般不需要主动扫全部，用户分配时按需清扫（lazy sweep）  │
└─────────────────────────────────────────────────────────────┘
```

**STW 只发生在 2 和 3、5 三个短瞬间**，加起来一般 < 500us。

---

## 六、GC 触发机制

### 触发条件

```go
// runtime/mgcpacer.go
// 简化：判断是否触发 GC
if heap.Live > heap.Trigger {
    startGC()
}
```

`heap.Trigger` 由 `GOGC` 决定：

```
Trigger = (1 + GOGC/100) × 上次 GC 后的存活对象大小

GOGC=100 (默认)：堆翻倍时触发
GOGC=200：堆 3 倍时触发（GC 少但堆大）
GOGC=50：堆 1.5 倍时触发（GC 频繁但堆小）
GOGC=off：禁用 GC（内存会无限涨）
```

### GC Pacer（配速器）

GC 不是"到时间就跑"，而是**预测 + 追赶**：

```
目标：GC 完成时堆增长不超过 GOGC 设定的比例
     ↓
Pacer 计算：以什么速度做 GC 才能追上用户分配速度
     ↓
如果 GC 慢了（要跟不上用户分配）：
    → 用户 goroutine 强制"assist"：每分配 N 字节就要 GC 扫描 M 字节
    → 相当于给分配加"惩罚性"任务
```

**面试常问**：为什么我程序看着 CPU 用得比 GOGC 想象的多？→ 因为 mark assist，用户 goroutine 被强制干了 GC 的活。

### GOMEMLIMIT（Go 1.19+）

老问题：GOGC 只看比例，不看绝对值。如果内存只有 4GB，GOGC=100 可能让堆涨到 4GB+ 直接 OOM。

`GOMEMLIMIT` 是**软上限**：

```go
debug.SetMemoryLimit(3 << 30)  // 3GB
// 或环境变量 GOMEMLIMIT=3GiB
```

内存接近上限时，GC 触发更激进（可以完全无视 GOGC），保护 OOM。

**推荐生产设置**：
```
GOGC=100
GOMEMLIMIT=容器内存的 70-80%
```

---

## 七、Mark Assist：用户 goroutine 也要做 GC

```go
// runtime/mgcmark.go 简化
func gcAssistAlloc(gp *g) {
    // 计算这个 goroutine 分配了多少字节
    debtBytes := gp.gcAssistBytes

    // 分配得多 → 欠 GC 的债多 → 强制帮 GC 做事
    for debtBytes > 0 {
        // 从灰色队列取对象扫描
        workDone := gcDrainN(...)
        debtBytes -= workDone * scanFactor
    }
}
```

**目的**：让分配对象多的 goroutine 承担更多 GC 责任，避免"某个 goroutine 疯狂分配把 GC 拖爆"。

**性能影响**：分配密集的接口延迟可能间歇性升高（因为被拉去做 GC）。

---

## 八、GC 观察和调优

### GODEBUG=gctrace=1

```bash
GODEBUG=gctrace=1 ./myapp
```

输出：

```
gc 12 @0.031s 0%: 0.005+0.19+0.017 ms clock, 0.041+0.058/0.17/0.44+0.13 ms cpu, 4->4->1 MB, 5 MB goal, 0 MB stacks, 0 MB globals, 8 P
```

**解读**（关键字段）：
- `gc 12`：第 12 次 GC
- `@0.031s`：程序启动 31ms
- `0%`：GC 占 CPU 总时间的比例
- `0.005+0.19+0.017 ms clock`：三个阶段的墙钟时间（STW start + concurrent + STW end）
- `0.041+0.058/0.17/0.44+0.13 ms cpu`：CPU 时间（分为 assist / background / idle）
- `4->4->1 MB`：GC 开始时堆 4MB → 结束时 4MB → 存活 1MB
- `5 MB goal`：本次 GC 的目标堆大小
- `8 P`：GC 使用了 8 个 P

**面试提示**：如果 STW 一直 < 1ms 就说明 GC 良好；如果 clock 时间和 cpu 时间差很多说明 GC 效率高（并发得好）。

### pprof heap

```bash
go tool pprof http://localhost:6060/debug/pprof/heap
(pprof) top
(pprof) list funcName
```

区分：
- **inuse_space**：当前存活对象占用（真泄漏看这个）
- **alloc_space**：累计分配（找频繁分配点用这个）

### runtime.ReadMemStats

```go
var m runtime.MemStats
runtime.ReadMemStats(&m)
fmt.Printf("HeapAlloc: %d MB\n", m.HeapAlloc/1024/1024)
fmt.Printf("NumGC: %d\n", m.NumGC)
fmt.Printf("PauseTotalNs: %d\n", m.PauseTotalNs)
fmt.Printf("LastPause: %d ns\n", m.PauseNs[(m.NumGC+255)%256])
```

**注意**：`ReadMemStats` 本身会 STW 一小段时间，别高频调（比如每秒 100 次）。

---

## 九、常见调优

### 1. 减少堆分配（最有效）

看 04-escape-analysis.md，让变量留在栈上。

### 2. 用 sync.Pool 复用对象

```go
var bufPool = sync.Pool{
    New: func() any { return new(bytes.Buffer) },
}

func handleReq(data []byte) {
    buf := bufPool.Get().(*bytes.Buffer)
    defer func() {
        buf.Reset()
        bufPool.Put(buf)
    }()
    // 用 buf...
}
```

**注意**：Pool 里的对象**会被 GC 回收**（每次 GC 清空一次），不是永久池。适合频繁分配释放的临时对象。

### 3. 预分配 slice / map

```go
// ❌ 触发多次扩容 + 分配
var s []int
for i := 0; i < 10000; i++ {
    s = append(s, i)
}

// ✅ 一次分配够
s := make([]int, 0, 10000)
for i := 0; i < 10000; i++ {
    s = append(s, i)
}
```

### 4. 大对象分离

```go
// ❌ 长期持有一个大对象里的小片段 → 大对象无法回收
type Cache struct {
    hugeData [1<<20]byte  // 1MB
    key      string
}

// ✅ 拆分
type Cache struct {
    hugeData *[1<<20]byte
    key      string
}
```

### 5. 调 GOGC

- 内存充裕，追求低 GC 频率 → `GOGC=200-500`（会用更多内存）
- 内存紧张，追求小堆 → `GOGC=50`（GC 更频繁）
- 生产建议：默认 100 + 设置 GOMEMLIMIT

### 6. 生产建议：GOMEMLIMIT

```yaml
# K8s
env:
  - name: GOMEMLIMIT
    value: "3200MiB"  # 容器 4GB，Go 用 3.2GB（80%）
```

配合 GOGC=100，正常情况按 GOGC 走，接近 3.2GB 时 GC 变激进防 OOM。

---

## 十、Go GC 的局限

### 1. 不是分代 GC

Java/V8 都是分代（Young / Old），基于"大部分对象很快死"的假设。Go 不分代，因为：
- Go 提倡栈分配（对应"死得快的对象"）
- 分代需要写屏障区分代，复杂度高
- 简单的三色 + 并发已经够快

### 2. 不移动对象（Non-moving）

Java 的 GC 会移动对象（compact）来减少碎片。Go 不移动：
- 移动对象需要更新所有引用（复杂）
- Go 的 size class 分配已经减少了碎片
- 不移动 → 可以直接操作指针（对 CGo 友好）

**代价**：碎片可能造成堆比实际存活对象大 1.5-2 倍。

### 3. 并发 GC 有 CPU 开销

默认 25% CPU 给 GC，用户 goroutine 可能被拖累。可以用 `GOGC` 调整（大 GOGC → GC 少但堆大）。

---

## 十一、面试高频题

### Q1: Go GC 用的什么算法？

**并发三色标记 + 混合写屏障 + 非分代 + 非移动**。核心是：
- 三色标记维护对象可达性
- 混合写屏障维持三色不变性（并发标记时不漏对象）
- 非分代、非移动（简化实现，代价是有碎片）

### Q2: 三色标记的三色是什么？

- 白：未访问
- 灰：已发现但未扫描完
- 黑：已扫描完（自己和引用都处理了）

GC 从 root 出发把可达对象一路染黑，剩下的白色对象回收。

### Q3: 为什么并发 GC 需要写屏障？

并发时用户在改指针，可能造成：
- 黑色对象新指向白色对象
- 灰色对象删除到白色的引用
- 白色对象丢失所有指向 → 被错误回收

写屏障在指针写入时做补偿（染灰旧值 or 新值），保证不漏标。

### Q4: 混合写屏障是什么？和 Dijkstra / Yuasa 什么关系？

- **Dijkstra（插入屏障）**：写入新指针时染灰目标 → 栈也要屏障，代价大
- **Yuasa（删除屏障）**：删除旧指针时染灰目标 → GC 开始要 STW 扫描栈
- **Go 混合屏障**：Dijkstra + Yuasa + **GC 开始时栈直接全染黑**

关键突破：**栈黑化后不需要屏障**（栈内部指针变化不影响堆 GC），只在**栈到堆的写**才屏障。STW 从 100ms 降到 100us。

### Q5: GC 什么时候触发？

三种情况：
1. **堆增长到 Trigger**：默认 `(1 + GOGC/100) × 上次存活对象大小`（GOGC=100 就是翻倍）
2. **手动**：`runtime.GC()`（同步阻塞）
3. **超时**：sysmon 检测 2 分钟没 GC 就强制
4. **GOMEMLIMIT（1.19+）**：接近内存上限时激进 GC

### Q6: GC 有几次 STW？多长？

**三次**（Go 1.8+）：
1. **Sweep Termination**：完成上一轮清扫，微秒级
2. **Mark Setup**：开启写屏障 + 栈黑化，微秒级
3. **Mark Termination**：关闭写屏障 + 处理残留，微秒级

总 STW 一般 < 500us，好的服务 < 100us。

### Q7: Mark Assist 是什么？

用户 goroutine 分配得多时，**被强制帮 GC 干活**（扫描灰色对象）。目的是让"污染者"承担 GC 责任，防止 GC 追不上分配速度。

**副作用**：分配密集接口可能间歇性延迟升高。

### Q8: GOGC 和 GOMEMLIMIT 的区别？

- **GOGC**：按比例控制。`GOGC=100` 表示堆翻倍才 GC。只看比例，可能 OOM。
- **GOMEMLIMIT**（Go 1.19+）：**软内存上限**。接近上限时 GC 激进（可以完全无视 GOGC）。

生产建议：GOGC=100 + GOMEMLIMIT=容器内存的 70-80%。

### Q9: Go 为什么不用分代 GC？

- Go 提倡栈分配（相当于"代 0"）
- 分代需要更多写屏障区分代，复杂
- 简单的三色并发已经足够快（P99 STW < 1ms）
- 简化 runtime 实现，减少复杂度

**代价**：如果程序创建大量短命对象，Go GC 效率不如 Java G1。

### Q10: 什么是碎片化？Go 怎么处理？

对象释放后留下"洞"，新对象大小不匹配就用不上 → 碎片。

Go 用 **size class 分配**（详见 03-memory-allocator）：同大小对象放一起，减少碎片。但不移动对象，所以还是有碎片，一般 heap 会比存活对象大 1.5-2 倍。

### Q11: sync.Pool 会被 GC 清空吗？

**会**。每次 GC 会把 Pool 里的对象清空（Go 1.13+ 有 victim cache 稍好），所以 Pool 里的对象**不能保证一直存在**。

Get 每次都要处理"池里可能没有"的情况（`New` 兜底）。

### Q12: 怎么排查 GC 性能问题？

1. **GODEBUG=gctrace=1** 看 GC 频率、STW 时间
2. **pprof heap** 看内存分配热点
3. **pprof allocs** 看总分配（找频繁分配点）
4. **go tool trace** 看 GC 时间线
5. **观察 mark assist 比例**：assist 高说明 GC 追不上分配

优化方向：减少分配、sync.Pool、预分配 slice、调 GOGC/GOMEMLIMIT。
