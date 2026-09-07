# Go 指针 / 值传递 / 逃逸分析

**标签**: #go #basics #pointer #escape

---

## 一、指针基础

```go
// Go 有指针但没有指针运算（不能 p++）
x := 42
p := &x     // p 是 *int 类型，存储 x 的地址
*p = 100    // 解引用，修改 x 的值
fmt.Println(x)  // 100

// new 返回指针
p := new(int)   // *int，指向零值 0
*p = 42

// 结构体指针可以直接访问字段（自动解引用）
type User struct{ Name string }
u := &User{Name: "Jake"}
fmt.Println(u.Name)    // 等价于 (*u).Name
```

## 二、值传递（Go 只有值传递）

```go
// Go 函数参数永远是值拷贝。没有引用传递。
// "引用类型"（slice、map、chan）传的也是 header 的副本，但 header 里有指针 → 能修改底层数据

// 值类型传参（修改无效）
func modify(x int) { x = 100 }
a := 1
modify(a)
fmt.Println(a)  // 1（没变）

// 传指针（修改有效）
func modifyPtr(x *int) { *x = 100 }
modifyPtr(&a)
fmt.Println(a)  // 100

// slice 传参（header 拷贝，底层数组共享）
func appendDemo(s []int) {
    s[0] = 100       // ✅ 修改生效（操作共享的底层数组）
    s = append(s, 4) // ❌ 扩容后 s 指向新底层数组，外层看不到
}

// map 传参（hmap 指针拷贝，修改生效）
func mapDemo(m map[string]int) {
    m["new"] = 1  // ✅ 修改生效
}
```

## 三、逃逸分析（Escape Analysis）

### 什么是逃逸

变量的生命周期超出了所在函数的栈帧 → 必须分配到堆上 → 称为"逃逸"。

```go
// 不逃逸（栈分配，快）
func noEscape() int {
    x := 42  // x 在函数结束后不再使用 → 栈上
    return x
}

// 逃逸（堆分配，慢）
func escape() *int {
    x := 42   // x 的指针被返回 → 函数结束后仍需存活 → 堆上
    return &x
}
```

### 常见逃逸场景

```go
// 1. 返回局部变量的指针
func newUser() *User {
    u := User{Name: "Jake"}  // u 逃逸到堆
    return &u
}

// 2. 发送到 channel
func sendToChannel(ch chan *User) {
    u := &User{Name: "Jake"}  // u 逃逸（生命周期不确定）
    ch <- u
}

// 3. 闭包引用
func closure() func() int {
    x := 0          // x 逃逸（被闭包引用，生命周期延长）
    return func() int {
        x++
        return x
    }
}

// 4. interface{} 参数（编译器无法确定大小）
func printAny(x interface{}) { fmt.Println(x) }
func demo() {
    n := 42
    printAny(n)  // n 逃逸（装箱到 interface）
}

// 5. slice/map 容量不确定
func dynamicSlice(n int) []int {
    return make([]int, n)  // n 编译期未知 → 逃逸到堆
}
```

### 查看逃逸分析结果

```bash
go build -gcflags="-m" ./...
# 输出示例：
# ./main.go:10:2: moved to heap: x
# ./main.go:15:10: &User{} escapes to heap

# 更详细：
go build -gcflags="-m -m" ./...
```

### 减少逃逸的技巧

```go
// 1. 避免返回指针（如果可以）
// ❌
func newPoint() *Point { return &Point{1, 2} }
// ✅ 返回值（小结构体栈上拷贝很便宜）
func newPoint() Point { return Point{1, 2} }

// 2. 预分配 slice 容量
// ❌
func bad() []int { return make([]int, 0) }  // append 时逃逸
// ✅
func good(n int) []int {
    s := make([]int, 0, n)  // 如果 n 是常量，可能栈分配
    return s
}

// 3. sync.Pool 复用对象（避免频繁堆分配）
var bufPool = sync.Pool{
    New: func() interface{} { return new(bytes.Buffer) },
}
buf := bufPool.Get().(*bytes.Buffer)
defer bufPool.Put(buf)

// 4. 避免 interface{} 装箱
// ❌ fmt.Sprintf 参数是 interface{} → 会装箱
s := fmt.Sprintf("%d", n)
// ✅ strconv 无装箱
s := strconv.Itoa(n)
```

## 四、栈 vs 堆

| 对比 | 栈 | 堆 |
|------|-----|-----|
| 分配速度 | 极快（移动 SP 指针） | 慢（需要 GC 管理） |
| 释放 | 函数返回自动释放 | GC 扫描回收 |
| 大小 | 初始 2~8KB，可动态增长 | 理论上无限 |
| GC 压力 | 无 | 有 |
| 适用 | 局部变量、小对象 | 生命周期不确定的对象 |

---

## 五、面试高频题

### Q1: Go 是值传递还是引用传递？

**只有值传递**。slice/map/chan 看起来像引用传递，是因为它们的"值"本身就包含指针（拷贝 header ≈ 共享底层数据）。

### Q2: 什么情况下变量会逃逸到堆？

- 返回局部变量的指针
- 被 interface{} 装箱
- 闭包引用外部变量
- 发送到 channel
- 大小编译期不确定的分配

### Q3: 为什么减少逃逸对性能重要？

堆分配需要 GC 参与回收，增加 GC 压力和暂停时间。栈分配无 GC 开销，函数返回自动释放。高频调用的函数减少逃逸可以显著降低 GC 压力。

### Q4: 返回局部变量的指针安全吗？

安全。Go 编译器通过逃逸分析将变量分配到堆上，保证指针有效。这和 C 不同（C 中返回栈变量指针是未定义行为）。

### Q5: 如何查看一个变量是否逃逸？

`go build -gcflags="-m"` 查看编译器的逃逸分析报告。
