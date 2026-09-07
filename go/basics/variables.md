# Go 变量与常量

**标签**: #go #basics #variables #iota

---

## 一、变量声明

```go
// 方式一：var 声明（可用于包级别和函数内）
var name string              // 零值初始化
var age int = 28             // 显式初始化
var x, y int = 1, 2         // 多变量

// 方式二：短声明 :=（只能在函数内）
name := "Jake"               // 自动推导类型
x, y := 1, 2

// 方式三：var 块
var (
    host   string = "localhost"
    port   int    = 8080
    debug  bool
)
```

## 二、短声明 `:=` 的陷阱

### 陷阱 1：变量遮蔽（Shadowing）

```go
x := 1
fmt.Println(x)  // 1

if true {
    x := 2      // ⚠️ 新变量！不是修改外层的 x
    fmt.Println(x)  // 2
}

fmt.Println(x)  // 1（外层 x 没变）

// 解决：用 = 赋值而不是 :=
if true {
    x = 2       // ✅ 修改外层 x
}
```

### 陷阱 2：多返回值中的部分新变量

```go
x := 1
x, y := 2, 3   // ✅ 合法：只要有一个新变量（y）就行
// x := 2      // ❌ 编译错误：no new variables on left side
```

### 陷阱 3：err 遮蔽

```go
func demo() error {
    var err error

    if true {
        result, err := doSomething()  // ⚠️ 这个 err 是新变量！
        _ = result
        _ = err  // 这个 err 只在 if 块内有效
    }

    return err  // 永远返回 nil！外层 err 没被赋值

    // 解决：
    // var result Type
    // result, err = doSomething()  // 用 = 而不是 :=
}
```

## 三、常量（const）

```go
// 基本常量
const pi = 3.14159
const (
    StatusOK    = 200
    StatusError = 500
)

// 无类型常量（可以参与不同类型运算）
const x = 10
var a int32 = x    // ✅
var b float64 = x  // ✅

// 有类型常量（只能用于同类型）
const y int32 = 10
// var c int64 = y // ❌
```

## 四、iota（常量生成器）

```go
// iota 在 const 块中从 0 开始，每行 +1
const (
    Sunday    = iota  // 0
    Monday            // 1（隐式 iota）
    Tuesday           // 2
    Wednesday         // 3
    Thursday          // 4
    Friday            // 5
    Saturday          // 6
)

// 跳值
const (
    _  = iota  // 0（跳过）
    KB = 1 << (10 * iota)  // 1 << 10 = 1024
    MB                     // 1 << 20
    GB                     // 1 << 30
    TB                     // 1 << 40
)

// 位掩码
const (
    FlagRead    = 1 << iota  // 1
    FlagWrite                // 2
    FlagExecute              // 4
)

// 多个 iota 在同一行
const (
    a, b = iota, iota + 10  // 0, 10
    c, d                    // 1, 11
    e, f                    // 2, 12
)

// 每个 const 块 iota 重新归零
const x = iota  // 0
const y = iota  // 0（新块，重新开始）
```

## 五、变量作用域

```go
// 包级变量（整个包内可见）
var pkgVar = "package level"

func main() {
    // 函数级变量
    funcVar := "function level"

    {
        // 块级变量
        blockVar := "block level"
        fmt.Println(blockVar)  // ✅
    }
    // fmt.Println(blockVar)  // ❌ 编译错误：未定义

    // for 循环变量的作用域
    for i := 0; i < 3; i++ {
        // i 的作用域在 for 块内
    }
    // fmt.Println(i)  // ❌ 编译错误
}
```

## 六、特殊变量 `_`（空白标识符）

```go
// 忽略不需要的返回值
_, err := os.Open("file.txt")

// 强制实现接口（编译期检查）
var _ io.Reader = (*MyStruct)(nil)

// 忽略 import 的副作用（只执行 init）
import _ "github.com/go-sql-driver/mysql"

// 忽略 for range 的索引或值
for _, v := range slice { ... }
for i := range slice { ... }  // 只要索引
```

## 七、全局变量 vs 局部变量的初始化

```go
// 全局变量：在程序启动时初始化，init() 之前
var globalDB *sql.DB

// ⚠️ 全局变量的初始化顺序：
// 1. 按依赖关系排序（被依赖的先初始化）
// 2. 同文件内按声明顺序
// 3. 同包不同文件按文件名字母序（不要依赖这个！）

var (
    a = b + 1  // a 依赖 b → b 先初始化
    b = 1
)
// 结果：b=1, a=2
```

## 八、new 与 make

```go
// new(T)：分配内存，返回 *T（指向零值的指针）
p := new(int)       // *int，值为 0
s := new(MyStruct)  // *MyStruct，各字段为零值

// make(T, args)：只用于 slice/map/chan，返回 T（不是指针）
s := make([]int, 5, 10)     // len=5, cap=10
m := make(map[string]int)   // 初始化的 map（可写入）
ch := make(chan int, 3)     // 容量为 3 的缓冲 channel

// 区别：
// new → 分配零值内存，返回指针
// make → 初始化内部结构（slice header、hash table、channel buffer），返回值本身
```

---

## 九、面试高频题

### Q1: `:=` 和 `var` 的区别？

`:=` 只能在函数内使用，自动推导类型。`var` 可以在包级别声明，支持显式类型。

### Q2: iota 是什么？

const 块内的行计数器，从 0 开始每行 +1。每个新 const 块重新归零。

### Q3: Go 有全局变量吗？

有。包级别的变量就是全局的（对整个包可见，首字母大写则对外部包可见）。但 Go 社区推荐少用全局变量，用依赖注入替代。

### Q4: 为什么短声明容易出 bug？

变量遮蔽（shadowing）：在内层作用域用 `:=` 创建了同名新变量，误以为修改了外层变量。用 `go vet -shadow` 检测。

### Q5: new 和 make 的区别？

- `new(T)` → 分配零值内存，返回 `*T`
- `make(T)` → 只用于 slice/map/chan，初始化内部结构，返回 `T`
- 不能 `make(int)` 也不能 `new` 代替 `make`（new 出来的 map 是 nil，不能写入）
