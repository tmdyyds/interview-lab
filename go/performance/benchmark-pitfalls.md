# Benchmark 编写坑合集

**标签**: #go #performance #benchmark #testing #高频

写 benchmark 只要一处踩坑，数据就完全不可信——**编译器优化掉、setup 混进测量、GC 干扰、并发场景写错**都是常见来源。本文列 10 类典型坑，每条给出错误示例、正确写法和验证方法。

---

## 一、Benchmark 基本骨架

```go
// bench_test.go
package foo

import "testing"

func BenchmarkFoo(b *testing.B) {
    for i := 0; i < b.N; i++ {
        _ = expensiveFunc()
    }
}
```

运行：

```bash
go test -bench=. -benchmem -run=^$ -count=5
```

- `-bench=.` — 跑当前包所有 benchmark
- `-benchmem` — **必开**，报告每次操作的分配次数和字节
- `-run=^$` — 只跑 bench，不跑单元测试
- `-count=5` — 跑 5 轮，配合 benchstat 做统计显著性

输出：

```
BenchmarkFoo-8    5000000    285 ns/op    64 B/op    2 allocs/op
                  ↑          ↑            ↑          ↑
                  b.N       每次操作耗时  每次分配字节 每次分配次数
```

---

## 二、10 类常见坑

### 坑 1：结果被编译器优化掉

```go
// ❌ 结果没被使用，编译器可能直接删掉整个循环
func BenchmarkParse(b *testing.B) {
    for i := 0; i < b.N; i++ {
        parse("input string")
    }
}
```

编译器发现返回值没用 → 判定为 dead code → benchmark 变成空循环，得到"1ns/op"的假数据。

**正确写法**：**用 sink 变量吸收结果**：

```go
var result Result   // package 级变量，编译器无法证明它不被读

func BenchmarkParse(b *testing.B) {
    var r Result
    for i := 0; i < b.N; i++ {
        r = parse("input string")
    }
    result = r     // 让 r 逃逸出函数
}
```

或者 Go 1.24+ 的官方推荐：

```go
func BenchmarkParse(b *testing.B) {
    for range b.N {                    // Go 1.22+ range int 语法
        _ = parse("input string")      // ⚠️ 仍可能被优化
    }
}
```

**最保险**：`testing.B.Loop`（Go 1.24+）——runtime 强制不优化：

```go
func BenchmarkParse(b *testing.B) {
    for b.Loop() {
        parse("input string")   // Loop 保证被执行
    }
}
```

**验证被优化掉了没**：
- 加 `go test -bench=. -gcflags="-m"` 看逃逸日志
- 结果 <1 ns/op 就有 99% 是被优化了

### 坑 2：Setup 混进测量

```go
// ❌ 每次循环都重建大 map
func BenchmarkLookup(b *testing.B) {
    for i := 0; i < b.N; i++ {
        m := buildLargeMap()   // 耗时的准备工作
        _ = m["key"]
    }
}
```

测量结果里 99% 是 `buildLargeMap` 的时间，不是 lookup。

**正确写法**：把 setup 移出循环，用 `b.ResetTimer()` 清零计时：

```go
func BenchmarkLookup(b *testing.B) {
    m := buildLargeMap()

    b.ResetTimer()   // ★ 从这一刻起才开始计时
    for i := 0; i < b.N; i++ {
        _ = m["key"]
    }
}
```

**如果循环内确实需要短 setup**，用 StopTimer/StartTimer 段落式暂停：

```go
func BenchmarkPop(b *testing.B) {
    for i := 0; i < b.N; i++ {
        b.StopTimer()
        stack := makeStack(1000)   // 每次都要新栈
        b.StartTimer()

        stack.PopAll()             // 只测这一段
    }
}
```

**注意**：`StopTimer/StartTimer` 本身有微秒级开销，纳秒级操作别频繁调用。

### 坑 3：忘了 `-benchmem`

```
BenchmarkFoo-8    5000000    285 ns/op       ← 缺内存信息
```

看不到分配情况，性能优化盲飞。解决方案二选一：

```bash
go test -bench=. -benchmem   # 命令行开
```

或代码里强制开：

```go
func BenchmarkFoo(b *testing.B) {
    b.ReportAllocs()   // 该 bench 强制报告分配
    for i := 0; i < b.N; i++ {
        // ...
    }
}
```

### 坑 4：并行 benchmark 用错

想测锁竞争、goroutine 池等，要用 `RunParallel`：

```go
// ✅ 正确：多个 goroutine 并发跑
func BenchmarkMutex(b *testing.B) {
    var mu sync.Mutex
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            mu.Lock()
            mu.Unlock()
        }
    })
}
```

**常见错误**：

```go
// ❌ 手动开 goroutine，b.N 语义错乱
func BenchmarkMutex(b *testing.B) {
    var wg sync.WaitGroup
    for i := 0; i < b.N; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            mu.Lock()
            mu.Unlock()
        }()
    }
    wg.Wait()
}
```

`RunParallel` 内部会：
- 起 `GOMAXPROCS` 个 goroutine
- 用 `-cpu` 参数可调（如 `-cpu=1,4,16`）
- `pb.Next()` 内部原子分配 b.N 的一份工作给当前 goroutine

**并发场景 baseline**：`b.SetParallelism(n)` 调整并发倍数（默认 1 × GOMAXPROCS）。

### 坑 5：多组输入不用 sub-benchmark

```go
// ❌ 写多个 benchmark 函数，维护困难
func BenchmarkParse10()   {...}
func BenchmarkParse100()  {...}
func BenchmarkParse1000() {...}
```

**正确写法**：`b.Run` 生成 sub-benchmark：

```go
func BenchmarkParse(b *testing.B) {
    sizes := []int{10, 100, 1000, 10000}
    for _, size := range sizes {
        input := makeInput(size)
        b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
            b.ResetTimer()
            for i := 0; i < b.N; i++ {
                _ = parse(input)
            }
        })
    }
}
```

输出：

```
BenchmarkParse/size=10-8      50000000    30 ns/op
BenchmarkParse/size=100-8     10000000   150 ns/op
BenchmarkParse/size=1000-8     1000000  1500 ns/op
BenchmarkParse/size=10000-8     100000 15000 ns/op
```

一眼看出复杂度是 O(n)。

### 坑 6：只跑一次就下结论

```
BenchmarkFoo-8   1000000   285 ns/op
```

285 ns/op 是可信的吗？跑第二次可能是 250、第三次是 320。**单次结果不能作为对比依据**。

**正确做法**：

```bash
# 跑 10 次
go test -bench=BenchmarkFoo -count=10 -benchmem > old.txt

# 改代码后再跑 10 次
go test -bench=BenchmarkFoo -count=10 -benchmem > new.txt

# 用 benchstat 做统计分析
go install golang.org/x/perf/cmd/benchstat@latest
benchstat old.txt new.txt
```

输出：

```
name       old time/op    new time/op    delta
Foo-8      285ns ± 3%     220ns ± 2%    -22.81%  (p=0.000 n=10+10)
                          ↑                       ↑
                          方差             统计显著性 p 值
```

**p < 0.05** 才算差异显著。方差大（如 ±20%）说明测量不稳定，需要更多样本。

### 坑 7：忽略 GC 干扰

Go GC 不定时触发，可能刚好命中你的 benchmark 循环，制造尖峰：

```
BenchmarkFoo-8   iter=100   avg=250 ns/op
                            但某几次 iter=800 ns/op ← GC 干扰
```

**减少干扰的方法**：

```go
func BenchmarkFoo(b *testing.B) {
    runtime.GC()   // 循环前先手动 GC 一次，清干净
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // ...
    }
}
```

或者关闭 GC 观察纯性能（不代表生产真实值）：

```bash
GOGC=off go test -bench=.
```

**根本方案**：找出 GC 触发的分配源，用 `-benchmem` 看 allocs/op，用 pprof `-memprofile` 定位热点。

### 坑 8：环境噪音

CPU 频率变化、其他进程竞争核心、Windows 后台任务——都会污染 benchmark 数据。

**建议**：

- 关闭桌面动画、通知
- Linux 上把 CPU governor 设为 performance：
  ```bash
  sudo cpupower frequency-set -g performance
  ```
- 用 `-cpu` 固定核数：`go test -bench=. -cpu=8`
- 用 `taskset` 绑 CPU 核（Linux）：
  ```bash
  taskset -c 0-7 go test -bench=.
  ```
- 关闭 Turbo Boost / HyperThreading（追求极致稳定时）

**CI 里跑 benchmark**：几乎总不稳定，只做趋势观察，不能作为通过/失败标准。

### 坑 9：与逃逸分析冲突

benchmark 里的**局部变量**可能因为你的写法逃逸到堆，让结果不代表真实调用：

```go
// ❌ result 逃逸到堆
var result *bytes.Buffer
func BenchmarkBuffer(b *testing.B) {
    for i := 0; i < b.N; i++ {
        buf := &bytes.Buffer{}
        buf.WriteString("hello")
        result = buf   // 让 buf 逃逸
    }
}
```

生产代码里 `buf` 可能是栈分配的（没这句 `result = buf`），benchmark 里被迫堆分配，测得的分配数比真实多。

**验证**：`go test -bench=. -gcflags="-m"` 看逃逸日志。

**建议**：sink 变量只用最简单的赋值，避免过度改变数据流。

### 坑 10：忘了对齐 workload

benchmark 用的输入数据**必须能代表生产**：

- 生产 payload 均值 5KB → 别用 100B 测
- 生产热点 key 分布是 Zipf → 别用均匀随机
- 生产并发 100 → 别只跑单线程 benchmark 就下结论

**典型翻车**：加缓存优化了 hot path，但 benchmark 用的全是 hot key，看着提升 10x，上线发现真实场景 hot/cold 混合，提升只有 1.2x。

---

## 三、组合技：benchmark + pprof

benchmark 阶段就出 profile，改代码后快速迭代：

```bash
go test -bench=BenchmarkParse -cpuprofile=cpu.prof -memprofile=mem.prof
go tool pprof cpu.prof
```

**技巧**：benchmark 里加 `b.ReportMetric` 报告自定义指标：

```go
func BenchmarkParse(b *testing.B) {
    var total int64
    for i := 0; i < b.N; i++ {
        n := parse(input)
        total += int64(n)
    }
    b.ReportMetric(float64(total)/float64(b.N), "bytes/op")
}
```

输出会多一列 `X bytes/op`，可以用来看吞吐、命中率等业务指标。

---

## 四、一份"通关模板"

```go
var sink Result   // 防优化的 sink 变量

func BenchmarkXxx(b *testing.B) {
    // 1. Setup（不计时）
    input := makeInput(...)
    r := &Runner{Config: cfg}
    runtime.GC()

    // 2. 显式开分配统计
    b.ReportAllocs()

    // 3. 清零计时
    b.ResetTimer()

    // 4. 主循环
    for i := 0; i < b.N; i++ {
        sink = r.Run(input)
    }
}

// 多输入用 sub-benchmark
func BenchmarkXxxSizes(b *testing.B) {
    for _, size := range []int{10, 100, 1000} {
        input := makeInput(size)
        b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
            b.ReportAllocs()
            b.ResetTimer()
            for i := 0; i < b.N; i++ {
                sink = process(input)
            }
        })
    }
}

// 并发场景
func BenchmarkXxxParallel(b *testing.B) {
    r := &Runner{}
    b.ReportAllocs()
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _ = r.Run(nil)
        }
    })
}
```

跑法：

```bash
go test -bench=BenchmarkXxx -benchmem -count=10 -run=^$ > new.txt
benchstat old.txt new.txt
```

---

## 五、面试高频题

### Q1: 写 benchmark 最容易踩的坑是什么？

**编译器优化掉**。返回值没被使用，dead code elimination 会把整个循环删掉，得到 1ns/op 的假数据。

**避坑**：sink 变量吸收结果，或 Go 1.24+ 用 `b.Loop()`。

### Q2: `b.N` 是怎么确定的？

runtime 自适应：**从 1 开始，每轮翻倍**（1、10、100、1000...），直到该 benchmark 累计跑够 1 秒（`-benchtime` 可调）。

含义：**耗时短的 benchmark 会跑很多轮，耗时长的少跑几轮**，都能得到相对稳定的平均值。

### Q3: `b.ResetTimer` / `StopTimer` / `StartTimer` 区别？

- `ResetTimer`：**清零累计计时**（setup 完调一次）
- `StopTimer`：**暂停计时**（循环内做不参与测量的短 setup）
- `StartTimer`：**恢复计时**

规则：`Reset` 用在循环外，`Stop/Start` 成对用在循环内且不能太频繁（有自身开销）。

### Q4: 怎么判断 benchmark 结果是否可信？

三个信号：

1. **方差**：多次跑 `-count=10`，用 benchstat 看 `± X%`，超过 5% 就要警惕
2. **统计显著性**：benchstat 报告 `p` 值，`p > 0.05` 说明差异可能是噪音
3. **合理性检查**：结果 <1 ns/op 或和理论值差太远 → 大概率被优化掉了

### Q5: 并行 benchmark 和串行 benchmark 的区别？

- 串行：`for i := 0; i < b.N; i++` 单 goroutine 跑，测**单次操作**性能
- 并行：`b.RunParallel(func(pb *testing.PB) { for pb.Next() { ... } })` 起 GOMAXPROCS 个 goroutine，测**吞吐** + **锁竞争**

用途：无共享状态用串行；测锁、连接池、共享 buffer 等用并行。

### Q6: `-benchmem` 报告的 `allocs/op` 是什么？

**每次操作触发的堆分配次数**。零分配是最理想的（栈上完成或复用 `sync.Pool`）。

优化思路：
- 用 `sync.Pool` 复用大对象
- `make([]T, 0, capacity)` 预分配
- 避免 interface{} 装箱
- 用 `strings.Builder` 代替 `+` 字符串拼接

### Q7: benchstat 是什么？为什么必须用？

单次 benchmark 结果**不可信**——CPU 频率、GC、调度都可能让波动 ±10%。benchstat 做**统计分析**：

- 多次采样后计算均值和方差
- 用 t-test 判断新旧差异是否显著（p 值）
- 输出人类可读的 delta

**规则**：优化前后各 `-count=10`，用 benchstat 对比，`p < 0.05` 才算真的优化。

### Q8: benchmark 结果和生产不一致，为什么？

常见原因：

1. **输入不代表真实**：数据大小/分布不匹配
2. **单机 vs 分布式**：网络、序列化、跨机 GC 未考虑
3. **热身效应**：生产有 warm-up 期（JIT、cache 填充）
4. **依赖组件的差异**：DB、Redis 的连接数、网络延迟
5. **GC pressure 不同**：微 benchmark 内存增长有限，生产是持续增长

benchmark 只测**微观**性能，宏观还得看**压测**（wrk / vegeta / ghz）+ 生产观测。

### Q9: `-cpu` 参数干什么？

限制 GOMAXPROCS，同一 benchmark 在不同并发下跑：

```bash
go test -bench=. -cpu=1,2,4,8
```

看**扩展性**（scalability）——理想情况下核数翻倍性能翻倍，如果 8 核只快 3 倍说明有锁竞争或伪共享。

### Q10: 为什么 CI 上跑 benchmark 常不稳定？

CI 环境的共性问题：
- **共享虚拟机**：CPU 时间片被其他任务抢
- **CPU 频率不固定**：节能模式跑一半升频
- **磁盘/网络突发**：影响 IO 类 benchmark

**处理**：
- 只做趋势观察，不作为 CI gate
- 有条件用**独占物理机**跑，定期采集数据存档
- 对比时用**同一台机器同一时段**的数据

---

## 六、延伸阅读

- 官方：[Package testing / Benchmark](https://pkg.go.dev/testing#hdr-Benchmarks)
- Dave Cheney：《[High Performance Go Workshop - Benchmarking](https://dave.cheney.net/high-performance-go-workshop/dotgo-paris.html#benchmarking)》
- Russ Cox：《[Benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)》
- Go 1.24 提案：`testing.B.Loop` — [proposal/testing-b-loop](https://github.com/golang/go/issues/61515)

---

**回到 [performance 目录](./README.md)**
