# pprof 排查专题

**标签**: #go #performance #pprof #trace #高频

CPU 打满、内存暴涨、goroutine 泄漏、接口 P99 抖动——**Go 生产事故 80% 靠 pprof 定位**。本文把六种 profile 的采集方式、看图技巧、常见结论一次讲全，附实战案例。

---

## 一、pprof 概览

Go 内置的**采样式性能分析器**，通过定期采样获取程序运行状态：

| Profile 类型 | 采样对象 | 典型问题 |
|-------------|---------|---------|
| **cpu** | 每 10ms 采样一次栈 | CPU 高、热点函数 |
| **heap** | 每分配 512KB 采样一次 | 内存占用高、内存泄漏 |
| **allocs** | 累计分配总量 | 频繁 GC、分配热点 |
| **goroutine** | 所有活跃 goroutine 栈 | goroutine 泄漏、死锁 |
| **block** | 阻塞事件（channel/lock 等待） | 慢接口、锁等待 |
| **mutex** | 互斥锁竞争 | 锁瓶颈 |
| **threadcreate** | OS 线程创建 | 冷门，CGo 场景 |
| **trace** | 全量事件时间线 | 调度/GC/网络的精细化分析 |

**核心思想**：pprof 是**采样**（不是全量），有一定误差但开销极低，可以生产开启。trace 是**全量**，开销大，只能短时开。

---

## 二、四种采集方式

### 方式 1：HTTP 端点（生产推荐）

```go
import _ "net/http/pprof"   // 空导入注册路由

func main() {
    // 单独端口，只绑内网
    go func() {
        http.ListenAndServe("127.0.0.1:6060", nil)
    }()
    // 业务代码...
}
```

⚠️ **千万不要暴露到公网**——这个端口能拉走堆栈、内存、代码路径，是信息泄漏和 DoS 攻击面。绑 `127.0.0.1` 或用 iptables 限内网。

采集：

```bash
# CPU 30 秒
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# 内存快照（在用中的对象）
go tool pprof http://localhost:6060/debug/pprof/heap

# 累计分配（找频繁分配点）
go tool pprof http://localhost:6060/debug/pprof/allocs

# 所有 goroutine 堆栈
go tool pprof http://localhost:6060/debug/pprof/goroutine

# 阻塞事件
go tool pprof http://localhost:6060/debug/pprof/block

# 锁竞争
go tool pprof http://localhost:6060/debug/pprof/mutex

# trace（30 秒全事件）
curl http://localhost:6060/debug/pprof/trace?seconds=30 -o trace.out
go tool trace trace.out
```

### 方式 2：runtime API 主动 dump

程序内主动写文件，适合无法开 HTTP 或需要精确时机采集：

```go
import "runtime/pprof"

// CPU
f, _ := os.Create("cpu.prof")
pprof.StartCPUProfile(f)
defer pprof.StopCPUProfile()

// heap
f, _ := os.Create("heap.prof")
pprof.WriteHeapProfile(f)
f.Close()

// goroutine
f, _ := os.Create("goroutine.prof")
pprof.Lookup("goroutine").WriteTo(f, 0)  // 0 = protobuf, 1 = 人类可读
```

### 方式 3：benchmark 自带

```bash
go test -bench=. -cpuprofile=cpu.prof -memprofile=mem.prof -benchmem
go tool pprof cpu.prof
```

**最方便的开发期分析方式**，改代码后立刻对比。

### 方式 4：block / mutex profile 需要启用

这两个默认关闭（有 runtime 开销）：

```go
runtime.SetBlockProfileRate(1)      // 1 = 每次阻塞都记录；大于 1 = 采样
runtime.SetMutexProfileFraction(1)  // 类似
```

生产上采样值调大（如 100），减少开销。

---

## 三、CPU Profile 实战

### 采集与查看

```bash
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# 进入交互模式后：
(pprof) top              # 前 10 个耗 CPU 函数（默认按 flat）
(pprof) top -cum         # 按累计时间排序（包含被调用函数的时间）
(pprof) list funcName    # 显示函数源码 + 每行耗时
(pprof) web              # 浏览器打开调用图（需要 graphviz）
(pprof) svg > cpu.svg    # 生成 SVG
(pprof) tree             # 树状调用图
```

### flat vs cum 的区别

```
top 输出示例：
      flat  flat%   sum%        cum   cum%
     3.5s  35.0%  35.0%      3.5s  35.0%   json.Marshal
     0.1s   1.0%  36.0%      4.8s  48.0%   handleRequest
```

- **flat**：函数**自身**耗时（不含子调用）
- **cum**：函数**及其子调用**总耗时

看优化目标：
- `flat` 高 → 函数内部有热点循环、序列化等
- `cum` 高但 `flat` 低 → 分派型函数，热点在子调用里

### 火焰图

```bash
# 交互模式内
(pprof) web

# 或用 http 界面（更推荐）
go tool pprof -http=:8080 cpu.prof
```

打开浏览器看火焰图（Flame Graph）：
- **横向**：占 CPU 时间比例，越宽越耗
- **纵向**：调用栈深度，向上是被调用者
- **看图诀窍**：找**宽平台**（横向宽 + 顶部平），那就是热点函数

### 常见 CPU 热点结论

| 火焰图看到什么 | 结论 |
|---------------|------|
| `runtime.mallocgc` 占比高 | 分配太频繁，考虑 `sync.Pool` / 预分配 |
| `runtime.gcBgMarkWorker` 高 | GC 压力大，减分配或调 `GOGC` |
| `runtime.mapaccess` 高 | map 频繁查询，考虑 slice / 数组 |
| `reflect.*` 高 | 反射滥用，改成代码生成或 unsafe |
| `syscall.*` 高 | 系统调用多，批量化 / 减少 IO |
| `runtime.futex` / `sync.Mutex.Lock` 高 | 锁竞争严重 |
| `encoding/json` 高 | 换 sonic / easyjson / gogoproto |

---

## 四、内存 Profile：heap vs allocs

### 两者的区别

- **heap**：当前**在用**的对象快照（`inuse_space` / `inuse_objects`）
- **allocs**：程序启动以来**累计分配**总量（`alloc_space` / `alloc_objects`）

```
关注"当前占了多少内存"     → heap
关注"哪里频繁产生 GC 压力" → allocs
```

### 四种视角

进入 pprof 后可以切换：

```
(pprof) sample_index=inuse_space    # 当前占用字节（默认，看内存泄漏）
(pprof) sample_index=inuse_objects  # 当前对象数（看是否小对象爆炸）
(pprof) sample_index=alloc_space    # 累计分配字节（看谁分配最多）
(pprof) sample_index=alloc_objects  # 累计分配对象数（看谁最频繁）
```

### 排查内存泄漏

**核心方法**：**两次快照 diff**，看增长的部分。

```bash
# 时刻 A
curl http://localhost:6060/debug/pprof/heap > heap_a.prof

# 等 10 分钟让泄漏累积

# 时刻 B
curl http://localhost:6060/debug/pprof/heap > heap_b.prof

# 对比
go tool pprof -base heap_a.prof heap_b.prof
(pprof) top
```

**看增长在哪里**——那就是泄漏点。

### 常见内存泄漏源

- **全局 map 只加不删**：如 sessionMap 装用户会话但从不清理
- **全局 slice 追加**：日志/事件累积
- **goroutine 泄漏带走的栈内存**（heap 看不到，用 goroutine profile）
- **time.Ticker / time.After 未 Stop**：定时器堆积
- **大对象缓存无淘汰策略**

---

## 五、Goroutine Profile 与泄漏排查

### 采集

```bash
# 命令行分析
go tool pprof http://localhost:6060/debug/pprof/goroutine

# 直接看文本（更快）
curl 'http://localhost:6060/debug/pprof/goroutine?debug=1'  # 汇总
curl 'http://localhost:6060/debug/pprof/goroutine?debug=2'  # 详细堆栈
```

`debug=2` 输出示例：

```
goroutine 12345 [chan receive, 15 minutes]:
main.handleRequest(0xc0001234)
    /app/main.go:42 +0x100
```

**关键信息**：
- `[chan receive, 15 minutes]` — 阻塞类型 + 阻塞时长
- 15 分钟没结束 = **大概率泄漏**

### 泄漏典型模式

**模式 1：channel 无人接收**

```go
ch := make(chan int)  // 无缓冲
go func() {
    ch <- 42  // 永远阻塞，goroutine 泄漏
}()
```

**模式 2：context 未取消**

```go
go func(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-time.After(time.Minute):
            doWork()
        }
    }
}(context.Background())  // 忘了传可取消的 ctx
```

**模式 3：HTTP body 未关闭**

```go
resp, _ := http.Get(url)
// 忘了 resp.Body.Close() → 底层连接不释放，可能开新 goroutine 读
```

### 生产判定标准

- `runtime.NumGoroutine()` 持续增长且无回落 → **泄漏**
- 稳定在几百/几千，且和 QPS 正相关 → **正常**（如 gRPC 每连接一 g）

### 测试辅助：goleak

```go
import "go.uber.org/goleak"

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

跑测试时自动检查有没有残留 goroutine，发现即失败。

---

## 六、Block Profile

**记录 goroutine 因等待而阻塞的时间**：channel 发送/接收、sync.Mutex.Lock、sync.WaitGroup.Wait 等。

### 开启

```go
runtime.SetBlockProfileRate(1000000)  // 1ms 阈值，只记录超过 1ms 的阻塞
```

### 用途

- 慢接口 P99 分析：函数没耗 CPU 但慢，通常就是 block
- 找出 channel 满/空、锁等待时间长的地方

### 采集

```bash
go tool pprof http://localhost:6060/debug/pprof/block
(pprof) top
```

结果里 `flat` 是**等待时间**，不是 CPU 时间。

---

## 七、Mutex Profile

**专门看锁竞争**：一个 goroutine 持锁 → 另一个 goroutine 等锁的时间统计。

### 开启

```go
runtime.SetMutexProfileFraction(100)  // 每 100 次锁竞争采一次
```

### 用途

- `sync.Mutex` / `sync.RWMutex` 是否是瓶颈
- 找出高频加锁的粗粒度锁 → 考虑拆分或用 sharding

### 典型热点

```
sync.(*Mutex).Lock 40%
    → 追到具体的锁对象
    → 分析持锁函数是否太长
    → 优化：缩短临界区、读多用 RWMutex、拆分锁
```

---

## 八、trace（时间线分析）

profile 是**统计**，trace 是**时间线**——每一次调度、GC、syscall、网络事件都记录。

### 采集

```bash
curl 'http://localhost:6060/debug/pprof/trace?seconds=5' -o trace.out
go tool trace trace.out
```

浏览器打开的界面有：
- **Goroutine analysis**：每类 g 的运行/等待时间分布
- **Scheduler latency profile**：调度延迟
- **Syscall blocking profile**：syscall 阻塞热点
- **User-defined regions**：自定义打点区间

### 用户打点

```go
import "runtime/trace"

ctx, task := trace.NewTask(ctx, "processOrder")
defer task.End()

trace.WithRegion(ctx, "validate", func() {
    validate(order)
})
trace.WithRegion(ctx, "save", func() {
    saveToDB(order)
})
```

trace UI 里能看到每个 region 精确耗时，比 pprof 更细。

### 何时用 trace

pprof 看不清的场景：
- **调度延迟高**：g 就绪了但没被调度上
- **GC 卡顿**：STW 时长和触发原因
- **网络 IO 时序**：netpoller 唤醒延迟
- **系统调用穿插**：syscall 阻塞了 P 多久

**注意**：trace 开销大（大约 CPU +30%），只能开几秒抓样本。

---

## 九、生产环境接入模板

```go
package main

import (
    "context"
    "log"
    "net/http"
    _ "net/http/pprof"
    "os/signal"
    "runtime"
    "syscall"
)

func main() {
    // 1. 开启 block / mutex profile（生产用大采样值）
    runtime.SetBlockProfileRate(1000000)      // 1ms
    runtime.SetMutexProfileFraction(100)

    // 2. pprof 端口只绑内网
    go func() {
        log.Println("pprof on :6060 (bind 127.0.0.1)")
        srv := &http.Server{Addr: "127.0.0.1:6060"}
        log.Fatal(srv.ListenAndServe())
    }()

    // 3. 业务服务
    go runBusinessService()

    // 4. 优雅退出
    ctx, cancel := signal.NotifyContext(context.Background(),
        syscall.SIGINT, syscall.SIGTERM)
    defer cancel()
    <-ctx.Done()
}
```

**上线前检查**：
- [ ] pprof 端口未暴露公网（防火墙、K8s NetworkPolicy）
- [ ] block/mutex 采样率适当（1000000 / 100 是合理起点）
- [ ] 可通过 sidecar 或 kubectl port-forward 拉 profile
- [ ] 保留 CI/压测环境的 baseline profile 便于回归对比

---

## 十、实战案例：定位 goroutine 泄漏

```go
// 泄漏代码
func handler(w http.ResponseWriter, r *http.Request) {
    ch := make(chan string)
    go queryDB(ch)             // 启动一个 g 查 DB
    result := <-ch             // 等结果
    if r.Context().Err() != nil {
        // 用户超时/取消，接口返回错误
        // 但上面那个 g 还在跑，chan 也没人接收，泄漏！
        return
    }
    w.Write([]byte(result))
}
```

**排查流程**：

```bash
# 1. 发现问题
curl localhost:6060/debug/pprof/goroutine | wc -l   # 数量持续涨

# 2. 拉详细堆栈
curl 'localhost:6060/debug/pprof/goroutine?debug=2' > g.txt

# 3. 找相同栈的 goroutine 数
grep -A 5 "chan receive" g.txt | head -30
```

典型输出：

```
goroutine 45678 [chan send, 30 minutes]:
main.queryDB(0xc0003210)
        /app/main.go:15 +0x80
```

**结论**：`queryDB` 的 `ch <- result` 阻塞了 30 分钟——外面的 handler 早就 return 了，chan 没人接。

**修复**：

```go
func handler(w http.ResponseWriter, r *http.Request) {
    ch := make(chan string, 1)   // ★ 用缓冲 channel，发送不阻塞
    go queryDB(ch)
    select {
    case result := <-ch:
        w.Write([]byte(result))
    case <-r.Context().Done():
        return   // g 里的 send 因为缓冲还能成功，goroutine 会自然退出
    }
}
```

---

## 十一、面试高频题

### Q1: pprof 有几种 profile？分别用于什么场景？

7 种：**cpu / heap / allocs / goroutine / block / mutex / trace**（严格说 trace 是独立工具）。

| profile | 场景 |
|---------|-----|
| cpu | CPU 打满 |
| heap | 内存占用高 / 内存泄漏 |
| allocs | 分配太多 / GC 压力大 |
| goroutine | goroutine 泄漏 / 死锁 |
| block | 慢接口但 CPU 不高 |
| mutex | 锁竞争 |
| trace | 调度/GC/网络的时间线细节 |

### Q2: heap 和 allocs 的区别？

- **heap** 看**当前存活**的对象（inuse_space）
- **allocs** 看**累计分配**的总量（alloc_space）

内存泄漏用 heap 快照 diff；GC 压力/优化分配用 allocs。

### Q3: flat 和 cum 的区别？

- **flat**：函数**自身**耗时
- **cum**：函数**及其调用链**总耗时

看优化点：flat 高说明函数内部有热点；cum 高但 flat 低说明它是"路由型"函数，热点在下游。

### Q4: 怎么排查 goroutine 泄漏？

三种方法：

1. **RPS 监控**：`runtime.NumGoroutine()` 持续涨且不回落
2. **两次 goroutine profile 对比**：用 `-base` 看增量
3. **看 debug=2 的堆栈时长**：`[chan receive, 15 minutes]` 通常就是泄漏点

**根因几乎必是**：channel 无人接收 / context 未取消 / body 未关闭 / 无限循环无退出条件。

### Q5: block profile 默认为什么关闭？怎么开？

开启后每次阻塞事件都要记录，有 runtime 开销。默认 `blockprofilerate = 0` 就是关。

```go
runtime.SetBlockProfileRate(rate)
// rate = 1     每次阻塞都记（开发用）
// rate = 1e6   1ms 以上阻塞才记（生产用）
// rate = 0     关闭
```

mutex 类似：`SetMutexProfileFraction(fraction)`，1 = 全采，100 = 每 100 次采一次。

### Q6: pprof 生产上安全吗？怎么防泄漏和攻击？

**风险**：
- profile 里含代码路径、常量、部分参数 → 信息泄漏
- CPU/trace 长时间采集 → 性能下降甚至 DoS
- 未鉴权的 `/debug/pprof/profile` 是**攻击面**

**防护**：
- 绑内网 IP（`127.0.0.1:6060`）
- K8s 用 NetworkPolicy 或 sidecar 代理
- 加 auth middleware（basic auth / mTLS）
- 限制 `seconds` 参数最大值

### Q7: 火焰图怎么看？

- **宽度**：占 CPU 比例，越宽越热
- **高度**：调用栈深度，向上是被调用方
- **看诀窍**：找**又宽又平的顶部**——那就是自身耗 CPU 的热点函数
- **颜色**通常无实际意义（只是区分）

### Q8: pprof 和 trace 的区别？

| | pprof | trace |
|---|-------|-------|
| 原理 | 采样统计 | 事件全量 |
| 开销 | 极低，生产可开 | 高（~30% CPU），短时开 |
| 视角 | 汇总（"哪个函数最慢"） | 时间线（"这一秒都发生了什么"） |
| 强项 | CPU/内存/锁热点 | 调度/GC/网络时序 |

**互补关系**：先用 pprof 找热点函数，再用 trace 看某个时间窗口的事件细节。

### Q9: CPU profile 采样精度多少？

**每 10ms 采样一次**（`runtime.SetCPUProfileRate` 可调，默认 100Hz）。

含义：**总样本数 × 10ms ≈ 总 CPU 时间**。样本少（< 1000）时数据不可信，采集时间要够长（30s 是常见值）。

### Q10: 采样值一样但服务变慢，怎么定位？

CPU profile 里看不出——因为**采样看的是"在跑的东西"，不是"等的东西"**。要看：

1. **block profile**：等锁/channel 的时间
2. **goroutine profile**：是否堆积
3. **trace**：调度延迟、GC 时长
4. **系统指标**：syscall、网络 RTT、磁盘 IO

CPU 不忙但服务慢 = **等待型瓶颈**，profile 之外的东西。

---

## 十二、延伸阅读

- 官方文档：[net/http/pprof](https://pkg.go.dev/net/http/pprof)、[runtime/pprof](https://pkg.go.dev/runtime/pprof)
- Dave Cheney：《[High Performance Go Workshop](https://dave.cheney.net/high-performance-go-workshop/dotgo-paris.html)》
- Uber Blog：《[How We Optimized Go Services with pprof](https://www.uber.com/blog/go-monorepo-bazel/)》
- [go.uber.org/goleak](https://github.com/uber-go/goleak)：测试时检测 goroutine 泄漏

---

**回到 [performance 目录](./README.md)**
