# Go 函数 / 闭包 / defer / panic / recover

**标签**: #go #basics #function #defer #高频

---

## 一、函数基础

```go
// 基本函数
func add(a, b int) int {
    return a + b
}

// 多返回值
func divide(a, b float64) (float64, error) {
    if b == 0 {
        return 0, errors.New("division by zero")
    }
    return a / b, nil
}

// 命名返回值（naked return，慎用）
func split(sum int) (x, y int) {
    x = sum * 4 / 9
    y = sum - x
    return  // 隐式 return x, y
}

// 可变参数
func sum(nums ...int) int {
    total := 0
    for _, n := range nums {
        total += n
    }
    return total
}
sum(1, 2, 3)       // 6
nums := []int{1,2,3}
sum(nums...)       // 展开 slice

// 函数作为值（一等公民）
var fn func(int) int
fn = func(x int) int { return x * 2 }
fn(5)  // 10
```

## 二、闭包（Closure）

```go
// 闭包 = 函数 + 引用的外部变量
func counter() func() int {
    count := 0
    return func() int {
        count++  // 捕获外部变量 count
        return count
    }
}

c := counter()
fmt.Println(c())  // 1
fmt.Println(c())  // 2
fmt.Println(c())  // 3（count 被持续引用，不会销毁）

// ⚠️ 闭包陷阱：for 循环中的闭包
funcs := make([]func(), 3)
for i := 0; i < 3; i++ {
    funcs[i] = func() {
        fmt.Println(i)  // ⚠️ 捕获的是变量 i 的引用，不是值
    }
}
funcs[0]()  // Go 1.21-: 全部打印 3（循环结束时 i=3）
            // Go 1.22+: 打印 0（每次迭代一个新变量）

// Go 1.22 之前的修复方式：
for i := 0; i < 3; i++ {
    i := i  // 创建新变量遮蔽外层
    funcs[i] = func() { fmt.Println(i) }
}
```

## 三、defer

### 3.1 基本规则

```go
// defer 延迟到函数返回前执行
// 多个 defer 按 LIFO（后进先出）顺序执行
func demo() {
    defer fmt.Println("1")
    defer fmt.Println("2")
    defer fmt.Println("3")
    fmt.Println("main")
}
// 输出：main → 3 → 2 → 1

// defer 参数在声明时求值（不是执行时）
func demo2() {
    x := 10
    defer fmt.Println(x)  // 参数 x 此时求值 = 10
    x = 20
}
// 输出：10（不是 20）
```

### 3.2 defer 与返回值

```go
// 命名返回值 + defer 可以修改返回值
func f() (result int) {
    defer func() {
        result++  // defer 中修改命名返回值
    }()
    return 0  // result = 0 → defer: result++ → 最终返回 1
}
// 返回 1

// 匿名返回值 + defer 不能修改
func g() int {
    x := 0
    defer func() {
        x++  // 修改的是局部变量 x，不是返回值
    }()
    return x  // 返回值已经确定为 0
}
// 返回 0
```

### 3.3 defer 常见用途

```go
// 1. 资源释放
func readFile(path string) ([]byte, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer f.Close()  // 无论后续怎么 return，都会关闭文件
    return io.ReadAll(f)
}

// 2. 解锁
func (s *SafeMap) Get(key string) interface{} {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.data[key]
}

// 3. recover panic
func safeCall(fn func()) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("recovered: %v", r)
        }
    }()
    fn()
}

// 4. 耗时统计
func trace(name string) func() {
    start := time.Now()
    return func() {
        fmt.Printf("%s took %v\n", name, time.Since(start))
    }
}
func demo() {
    defer trace("demo")()  // 注意末尾的 ()
    time.Sleep(100 * time.Millisecond)
}
```

### 3.4 defer 性能

```go
// Go 1.14+ defer 已经非常便宜（栈上分配，无堆逃逸）
// 大部分场景无需担心性能
// 只有在极端热点循环内部（百万次/秒）才需要考虑去掉 defer
```

## 四、panic / recover

```go
// panic：终止当前 goroutine 的正常执行
// 触发后：
//   1. 停止当前函数剩余代码
//   2. 执行当前函数的 defer（包括 recover）
//   3. 向上传播（调用者的 defer 也会执行）
//   4. 如果没有 recover，程序崩溃

// recover：只能在 defer 中调用，捕获 panic
func safeDivide(a, b int) (result int, err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic recovered: %v", r)
        }
    }()
    return a / b, nil  // b=0 时 panic
}

result, err := safeDivide(10, 0)
// result=0, err="panic recovered: runtime error: integer divide by zero"
```

### 什么时候该 panic？

```go
// ✅ 合理的 panic：
// 1. 程序初始化失败（不可恢复）
func mustLoadConfig() *Config {
    c, err := loadConfig()
    if err != nil {
        panic("failed to load config: " + err.Error())
    }
    return c
}

// 2. 编程错误（bug，不应该出现的情况）
func MustCompile(pattern string) *Regexp {
    re, err := Compile(pattern)
    if err != nil {
        panic("regexp: Compile(" + pattern + "): " + err.Error())
    }
    return re
}

// ❌ 不该 panic：
// 业务错误（用户输入无效、资源不存在、网络超时）→ 返回 error
```

### recover 不能跨 goroutine

```go
func main() {
    defer func() {
        recover()  // ❌ 无法捕获子 goroutine 的 panic
    }()

    go func() {
        panic("子 goroutine panic")  // 程序直接崩溃
    }()

    time.Sleep(time.Second)
}

// 正确：每个 goroutine 自己 recover
go func() {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("goroutine panic: %v", r)
        }
    }()
    // 业务逻辑...
}()
```

## 五、函数式编程模式

```go
// 高阶函数
func filter(nums []int, predicate func(int) bool) []int {
    var result []int
    for _, n := range nums {
        if predicate(n) {
            result = append(result, n)
        }
    }
    return result
}

evens := filter([]int{1,2,3,4,5}, func(n int) bool { return n%2 == 0 })
// [2, 4]

// Option 模式（函数选项）
type ServerOption func(*Server)

func WithPort(port int) ServerOption {
    return func(s *Server) { s.port = port }
}
func WithTimeout(t time.Duration) ServerOption {
    return func(s *Server) { s.timeout = t }
}

func NewServer(opts ...ServerOption) *Server {
    s := &Server{port: 8080, timeout: 30 * time.Second}  // 默认值
    for _, opt := range opts {
        opt(s)
    }
    return s
}

srv := NewServer(WithPort(9090), WithTimeout(60*time.Second))
```

---

## 六、面试高频题

### Q1: defer 执行顺序？

LIFO（后进先出，栈结构）。参数在声明时求值，函数体在返回前执行。

### Q2: defer 能修改返回值吗？

只有命名返回值 + defer 闭包才能修改。匿名返回值不行（return 时已拷贝到返回寄存器）。

### Q3: panic 能被 recover 吗？跨 goroutine 呢？

同一 goroutine 内，defer + recover 可以捕获。不能跨 goroutine recover。

### Q4: for range 闭包陷阱？

Go 1.22 之前：循环变量共享，闭包捕获的是同一个变量的引用。Go 1.22 起：每次迭代创建新变量，问题消除。

### Q5: 函数选项模式（Functional Options）的好处？

参数可选、顺序无关、向后兼容（新增选项不破坏签名）、可读性好。适合构造函数参数多且部分可选的场景。
