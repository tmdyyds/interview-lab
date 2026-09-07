# 逃逸分析深度剖析

**标签**: #go #runtime #escape #performance #高频

**变量在栈还是堆**，是 Go 性能优化的第一课。栈分配几乎零成本，堆分配要走内存分配器 + 增加 GC 压力。Go 用**逃逸分析（Escape Analysis）**在**编译期**决定每个变量的归属。

---

## 一、栈 vs 堆：为什么区别这么大

### 栈分配

```
栈：goroutine 私有，函数调用时分配，返回时自动释放

优点：
- 分配 = 移动 SP 指针一次（1 条指令）
- 释放 = 函数返回自动清理
- 不产生 GC 压力
- CPU cache 友好（栈是连续内存）

缺点：
- 生命周期不能超过函数
- 大小需要编译期可估算
```

### 堆分配

```
堆：全局共享，通过 mcache/mcentral/mheap 分配

优点：
- 生命周期任意长
- 大小灵活

缺点：
- 分配走三级缓存（快也要几十纳秒）
- 需要 GC 扫描回收
- 可能有锁竞争（mcache 缺货去 mcentral）
```

**性能对比**（大致数据）：

| 操作 | 时间 |
|-----|------|
| 栈分配 | ~1 ns |
| 堆分配（mcache 命中） | ~30 ns |
| 堆分配 + GC 扫描 | ~100 ns 起 |

---

## 二、什么是逃逸

**逃逸**：变量本可以在栈上分配，但因为某些原因**必须放到堆上**。

编译器在编译期做静态分析：**这个变量的引用会不会跑出当前函数？**
- **会**：逃逸到堆
- **不会**：留在栈

---

## 三、五种典型逃逸场景

### 场景 1：返回局部变量的指针

```go
// ❌ 逃逸：user 的地址被返回，函数返回后还在用
func NewUser() *User {
    user := User{Name: "Jake"}  // 逃逸到堆
    return &user
}

// 对比：不逃逸
func GetUser() User {
    user := User{Name: "Jake"}  // 栈上分配
    return user  // 值拷贝返回
}
```

**规则**：如果函数返回后还能通过某种方式访问到局部变量 → 逃逸。

### 场景 2：赋值给全局变量或长生命周期对象

```go
var globalUser *User

func setUser() {
    u := User{Name: "Jake"}
    globalUser = &u  // ❌ 逃逸：全局变量持有引用
}
```

**规则**：把栈变量的地址存到"活得更久"的地方 → 逃逸。

### 场景 3：interface 装箱

```go
func log(v interface{}) {  // interface{} = any
    fmt.Println(v)
}

func main() {
    x := 42
    log(x)  // ❌ x 逃逸：被装箱到 interface{}
}
```

**为什么**：`interface{}` 内部是 `(type, data)` 两个指针，`data` 需要指向堆上的实际值。

**特例**：Go 有小整数优化，`log(1)`、`log(2)` 之类的**常量小整数**可能不逃逸（编译器预分配）。

### 场景 4：闭包捕获变量

```go
func makeCounter() func() int {
    count := 0  // ❌ 逃逸：闭包捕获
    return func() int {
        count++
        return count
    }
}
```

**为什么**：外层函数返回后 `count` 还在被闭包使用。

### 场景 5：slice / map 太大或长度未知

```go
// ❌ 长度未知 → 逃逸
func makeSlice(n int) []int {
    return make([]int, n)  // n 不是常量，编译器不确定大小
}

// ✅ 长度已知且小 → 栈上分配
func makeSlice() []int {
    return make([]int, 100)  // 明确大小，且不太大
}

// ❌ 太大 → 逃逸（栈空间有限）
func makeBigSlice() [10000]int {
    var arr [10000]int
    return arr  // 太大，栈放不下（编译器判断）
}
```

**具体阈值**：Go 编译器有 `stackAllocLimit`（约 10MB / goroutine），实际单个对象超过几十 KB 就可能逃逸。

---

## 四、其他常见逃逸情况

### 6. 变长参数（当传 interface 时）

```go
func Println(a ...any) {  // fmt.Println
    // a 是 []any
}

Println(x, y, z)  // x, y, z 都可能逃逸
```

### 7. Channel 发送指针

```go
ch := make(chan *User)
u := User{}
ch <- &u  // ❌ u 逃逸：不知道接收方在哪个 goroutine
```

### 8. Goroutine 里访问外部变量

```go
func f() {
    x := 42
    go func() {
        fmt.Println(x)  // x 逃逸：goroutine 生命周期可能超过 f
    }()
}
```

### 9. 反射操作

```go
func f(v any) {
    reflect.ValueOf(v)  // 通常触发逃逸
}
```

### 10. 切片扩容后的底层数组

```go
s := make([]int, 0, 10)  // 栈上
for i := 0; i < 100; i++ {
    s = append(s, i)  // 扩容后 s 的底层数组可能变到堆
}
```

---

## 五、如何查看逃逸分析

用编译器的 `-m` 参数：

```bash
go build -gcflags="-m" main.go
# -m 是 -m=1，输出关键决策
# -m=2 更详细
```

### 输出示例

```go
// main.go
package main

type User struct{ Name string }

func NewUser() *User {
    u := User{Name: "Jake"}
    return &u
}

func GetUser() User {
    u := User{Name: "Jake"}
    return u
}

func main() {
    _ = NewUser()
    _ = GetUser()
}
```

```bash
$ go build -gcflags="-m" main.go
./main.go:5:6: can inline NewUser
./main.go:6:2: moved to heap: u                    ← ⚠️ 逃逸
./main.go:10:6: can inline GetUser
./main.go:15:6: can inline main
```

**关键信息**：
- `moved to heap: u`：u 被移到堆上（逃逸）
- `can inline`：函数可以内联（性能好）
- `escapes to heap`：某个值逃逸到堆

### 更详细：-m=2

```bash
go build -gcflags="-m -m" main.go
```

会输出**为什么**逃逸：

```
./main.go:6:2: u escapes to heap:
./main.go:6:2:   flow: ~r0 = &u:
./main.go:6:2:     from &u (address-of) at ./main.go:7:9
./main.go:6:2:     from return &u (return) at ./main.go:7:2
```

---

## 六、常见误解

### 误解 1：`new()` 一定在堆上

**错**。`new()` 返回指针，但如果编译器分析出指针没逃逸，一样在栈上：

```go
func f() {
    p := new(int)  // 编译器可能优化到栈
    *p = 42
    fmt.Println(*p)
}
```

用 `-m` 看：`./main.go:2:11: new(int) does not escape`。

### 误解 2：值传参一定不逃逸

**错**。如果函数内部把它的地址传出去了，还是逃逸：

```go
var g *User

func f(u User) {  // u 值传参
    g = &u  // ❌ 逃逸：地址被存到全局变量
}
```

### 误解 3：指针传参一定逃逸

**错**。指针传参本身不必然逃逸，看指针指向的对象怎么用：

```go
func modify(u *User) {  // u 是指针
    u.Age++  // 只是修改，不逃逸
}

func f() {
    u := User{}  // 栈上
    modify(&u)   // u 不逃逸
}
```

**规则**：指针本身不重要，重要的是**指针指向的对象**会不会活到函数外。

---

## 七、逃逸分析的实用优化

### 优化 1：避免不必要的指针返回

```go
// ❌ 触发逃逸
func Small() *SmallStruct {
    return &SmallStruct{}
}

// ✅ 小对象直接值返回
func Small() SmallStruct {
    return SmallStruct{}
}
```

**判断标准**：
- 结构体 < 64-128 字节 → 值返回可能更好
- 结构体 > 几百字节 → 指针返回避免拷贝
- 需要修改原对象 → 必须指针

### 优化 2：预分配 slice/map 避免逃逸

```go
// ❌ 大小未知 → 逃逸
func makeIDs(n int) []int {
    return make([]int, n)
}

// ✅ 固定小容量 → 栈上
func fixedIDs() []int {
    ids := make([]int, 10)  // 明确大小
    // ...
    return ids
}
```

**注意**：`return` 一个 slice 通常会逃逸（因为 caller 要用），但 slice header（24 字节）是拷贝的，底层数组的逃逸取决于是否能栈分配。

### 优化 3：避免 interface 装箱

```go
// ❌ 每次调用 log 都装箱
func log(msg string, v any) {
    fmt.Printf("%s: %v\n", msg, v)
}

log("count", 42)  // 42 装箱

// ✅ 明确类型
func logInt(msg string, v int) {
    fmt.Printf("%s: %d\n", msg, v)
}
```

这就是 Zap 的 `zap.Int("k", v)` 快的原因——**避免装箱**。

### 优化 4：sync.Pool 复用

即使逃逸了，用 sync.Pool 复用对象，减少分配次数（详见 GC 章节）。

### 优化 5：hot path 避免闭包捕获

```go
// ❌ 循环里创建闭包 → 变量逃逸
func process(items []Item) {
    for _, item := range items {
        go func() {
            handle(item)  // item 逃逸
        }()
    }
}

// ✅ 值传参
for _, item := range items {
    go func(it Item) {
        handle(it)
    }(item)
}
```

---

## 八、benchmark 验证

```go
type User struct {
    Name string
    Age  int
}

// 逃逸版
func NewUserHeap() *User {
    return &User{Name: "Jake", Age: 28}
}

// 不逃逸版
func NewUserStack() User {
    return User{Name: "Jake", Age: 28}
}

func BenchmarkHeap(b *testing.B) {
    for i := 0; i < b.N; i++ {
        _ = NewUserHeap()
    }
}

func BenchmarkStack(b *testing.B) {
    for i := 0; i < b.N; i++ {
        _ = NewUserStack()
    }
}
```

```bash
$ go test -bench=. -benchmem
BenchmarkHeap-8    30000000    40 ns/op    32 B/op    1 allocs/op
BenchmarkStack-8   1000000000   0.5 ns/op   0 B/op    0 allocs/op
```

**差距 80 倍**，因为：
1. 栈版本编译器直接内联
2. 堆版本要走 mcache，然后 GC 还得扫描

---

## 九、逃逸分析的限制

编译器的逃逸分析**很保守**，为了安全宁可放堆：

```go
// 编译器无法证明某种情况不会逃逸时 → 保守选择堆

// 示例：函数指针 / 反射 / interface 断言
func f(fn func(*User)) {
    u := User{}
    fn(&u)  // 编译器不知道 fn 内部会不会保存 &u → 逃逸
}
```

如果分析准确度不够 → 变量被错误地放堆上，牺牲性能换取安全。

**这是"编译期 vs 运行时"权衡的必然结果**，Go 选择"编译期分析 + 保守策略"，追求编译速度和实现简洁。

---

## 十、进阶：编译器内联

逃逸分析和**内联（inlining）**紧密相关：

```go
func small(x int) int {
    return x * 2
}

func main() {
    y := small(3)  // 编译器直接内联为 y := 3 * 2
}
```

**内联的好处**：
- 消除函数调用开销
- 让逃逸分析看到更完整的调用链，可能减少逃逸

用 `-m` 观察：`can inline small`。

**限制**：
- Go 编译器内联预算有限（~80 节点）
- 递归函数、包含 defer/panic 的函数不能内联
- 太大的函数不能内联

---

## 十一、面试高频题

### Q1: 什么是逃逸分析？

**编译期决定变量在栈还是堆的分析**。如果一个变量的引用会跑出当前函数，就必须放堆（逃逸）；否则可以放栈（不逃逸）。目的是让编译器自动优化内存分配。

### Q2: 什么情况一定会逃逸？

1. **返回局部变量的指针**
2. **赋值给全局变量或长生命周期对象**
3. **interface 装箱**（`var x any = 42`）
4. **闭包捕获变量**
5. **slice / map 大小未知或太大**
6. **channel 发送指针**
7. **goroutine 访问外部变量**
8. **反射操作**
9. **函数指针 / interface 方法调用**（编译器保守）

### Q3: `new(T)` 和 `&T{}` 一定在堆上吗？

**不一定**。如果编译器分析出返回的指针没逃逸出当前函数，就会放栈。用 `-gcflags="-m"` 观察：

```go
func f() {
    p := new(int)  // does not escape → 栈上
    *p = 42
}
```

### Q4: 值传参和指针传参哪个好？

**看场景**：
- 小对象（<= 128 字节，比如 struct 2-3 字段）：值传参更好（不逃逸 + 缓存友好）
- 大对象（几百字节以上）：指针传参避免拷贝
- 需要修改原对象：必须指针
- 结构体作为 map/slice 值：值语义更清晰

**误解**：指针传参≠一定逃逸，看指针指向的对象是否活到函数外。

### Q5: 怎么查看逃逸分析结果？

```bash
go build -gcflags="-m" main.go       # 简要
go build -gcflags="-m -m" main.go    # 详细，说明原因
```

关注 `moved to heap` 和 `escapes to heap`。

### Q6: interface 装箱为什么会逃逸？

`interface{}` 内部是 `(type_ptr, data_ptr)` 两个指针。`data_ptr` 需要指向具体值：
- 值是指针大小的 → 直接存 data_ptr
- 值是大对象 → data_ptr 指向堆上的实际数据

所以传递大对象给 interface 参数时，对象**必须在堆上**（因为 interface 变量本身可能生命周期长）。

**这就是 Zap 用 Field API 而不是 `interface{}` 的原因**——避免装箱逃逸。

### Q7: sync.Pool 里的对象会逃逸吗？

**会**。Pool.Get 返回的对象在堆上（Pool 本身跨 goroutine 共享）。sync.Pool 的目的不是"避免逃逸"，是"减少重复分配 + 减少 GC 压力"。

### Q8: 逃逸对性能影响多大？

大约差 **一到两个数量级**：
- 栈分配：~1 ns
- 堆分配：~30 ns 起（还要加上 GC 扫描的额外开销）

在 hot path（如每次请求都执行的代码）上，逃逸的累积开销可以让 QPS 差好几倍。

### Q9: 编译器逃逸分析为什么保守？

因为**编译期静态分析有限**：
- 函数指针 / interface 方法调用：不知道运行时会调用哪个
- 反射：完全动态
- 复杂控制流：分析成本高

保守 = 安全（宁可堆分配也不出错），代价 = 部分变量本可以在栈上但被放堆上。

### Q10: 逃逸分析和内联什么关系？

**内联能减少逃逸**。函数调用被内联后，编译器能看到更完整的调用链，能证明更多变量不逃逸。

举例：`func small()` 内联后，调用它的地方的临时变量可能因为看清了 small 内部而变成栈分配。

### Q11: 怎么优化逃逸？

1. **返回值类型而不是指针**（小对象）
2. **避免 interface 装箱**（用具体类型 API）
3. **闭包外面创建变量**，让闭包捕获栈上变量
4. **预分配已知大小的 slice / map**
5. **hot path 用 `zap.Int` 而不是 `fmt.Printf("%d", v)`**
6. **sync.Pool 复用不可避免的堆对象**

### Q12: 什么是"寄存器着色"？和逃逸分析什么关系？

**寄存器分配**是编译器把变量放到 CPU 寄存器（比栈还快）。**逃逸分析先决定栈 vs 堆，然后栈上的变量再看能不能进寄存器**。

Go 1.17+ 有 register-based calling convention（函数调用参数放寄存器），进一步降低栈操作开销。

---

## 十二、真实优化案例

### 案例 1：热点接口去装箱

```go
// 优化前：QPS 5 万，每次分配 2 个 interface{}
func Log(fields ...any) {
    fmt.Printf("%v\n", fields)
}

// 优化后：QPS 15 万，零装箱
type Field struct {
    Key   string
    Value string
}
func LogFields(fields ...Field) {
    // ...
}
```

### 案例 2：sync.Pool + 预分配 buffer

```go
var bufPool = sync.Pool{
    New: func() any {
        b := make([]byte, 0, 1024)
        return &b
    },
}

func handle() {
    bp := bufPool.Get().(*[]byte)
    buf := (*bp)[:0]
    defer func() {
        *bp = buf
        bufPool.Put(bp)
    }()
    // 用 buf
}
```

### 案例 3：结构体重排 + 值语义

```go
// 优化前：24 字节 + 每次调用堆分配
type Result struct {
    OK      bool
    Value   int64
    Padding bool
}
func query() *Result { return &Result{} }

// 优化后：16 字节 + 栈分配
type Result struct {
    Value   int64
    OK      bool
    Padding bool
}
func query() Result { return Result{} }
```
