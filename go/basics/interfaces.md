# Go 接口（Interface）

**标签**: #go #basics #interface #高频

---

## 一、接口基础

```go
// 接口 = 方法签名的集合
// 任何类型只要实现了接口的所有方法，就隐式实现了该接口（鸭子类型）
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Writer interface {
    Write(p []byte) (n int, err error)
}

// 接口组合
type ReadWriter interface {
    Reader
    Writer
}

// 实现接口：不需要 implements 关键字，实现方法即可
type MyFile struct{ data []byte }

func (f *MyFile) Read(p []byte) (int, error) {
    copy(p, f.data)
    return len(f.data), nil
}
// *MyFile 实现了 Reader 接口
```

## 二、底层结构（iface / eface）

```go
// 非空接口（有方法的接口）底层结构
type iface struct {
    tab  *itab          // 类型信息 + 方法表
    data unsafe.Pointer // 指向具体值的指针
}

type itab struct {
    inter *interfacetype  // 接口类型描述
    _type *_type          // 具体类型描述
    fun   [1]uintptr      // 方法地址表（变长）
}

// 空接口（interface{} / any）底层结构
type eface struct {
    _type *_type          // 类型信息
    data  unsafe.Pointer  // 指向具体值的指针
}
```

**关键理解**：接口变量 = 类型指针 + 数据指针。两者都不为 nil 时接口才不为 nil。

## 三、nil 接口陷阱（面试必考）

```go
type MyError struct{ msg string }
func (e *MyError) Error() string { return e.msg }

func getError() error {
    var err *MyError = nil  // *MyError 是 nil
    return err              // ⚠️ 返回的是 interface{tab: *itab, data: nil}
}

func main() {
    err := getError()
    fmt.Println(err == nil)  // false！！！

    // 为什么？
    // err 是 interface，内部：{tab: 指向*MyError的类型信息, data: nil}
    // 接口只有 tab 和 data 都是 nil 时才等于 nil
    // 这里 tab 不是 nil（有类型信息），所以 err != nil
}

// ✅ 正确做法：直接返回 nil（不要返回类型化的 nil 指针）
func getError() error {
    var err *MyError = nil
    if err == nil {
        return nil  // ✅ 直接返回 nil，不附带类型信息
    }
    return err
}
```

**判断接口是否真的"空"**：

```go
import "reflect"

func isNilInterface(i interface{}) bool {
    if i == nil {
        return true
    }
    v := reflect.ValueOf(i)
    return v.Kind() == reflect.Ptr && v.IsNil()
}
```

## 四、类型断言与 Type Switch

```go
var i interface{} = "hello"

// 类型断言（不安全）
s := i.(string)   // ✅ s = "hello"
// n := i.(int)   // ❌ panic

// 类型断言（安全，推荐）
s, ok := i.(string)   // ok=true, s="hello"
n, ok := i.(int)      // ok=false, n=0

// Type Switch
func describe(i interface{}) {
    switch v := i.(type) {
    case string:
        fmt.Printf("string: %s (len=%d)\n", v, len(v))
    case int:
        fmt.Printf("int: %d\n", v)
    case bool:
        fmt.Printf("bool: %t\n", v)
    case nil:
        fmt.Println("nil")
    default:
        fmt.Printf("unknown: %T\n", v)
    }
}
```

## 五、空接口 `interface{}` / `any`

```go
// Go 1.18+ 可以用 any 替代 interface{}
type any = interface{}

// 空接口可以存任何类型
var x interface{} = 42
x = "hello"
x = []int{1, 2, 3}

// 常见用途
func Print(args ...interface{}) { ... }           // fmt.Println
func Marshal(v interface{}) ([]byte, error) { ... } // json.Marshal

// ⚠️ 滥用空接口 = 放弃类型安全
// Go 1.18+ 有泛型，很多场景可以替代空接口
func Max[T constraints.Ordered](a, b T) T {
    if a > b { return a }
    return b
}
```

## 六、接口设计原则

### 小接口优于大接口

```go
// ✅ Go 标准库风格：小而精
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Closer interface {
    Close() error
}

// 组合使用
type ReadCloser interface {
    Reader
    Closer
}

// ❌ Java 风格：大而全（Go 中不推荐）
type FileOperator interface {
    Read(p []byte) (n int, err error)
    Write(p []byte) (n int, err error)
    Close() error
    Seek(offset int64, whence int) (int64, error)
    Stat() (os.FileInfo, error)
    // ... 20 个方法
}
```

### 接口由使用者定义（不是实现者）

```go
// ❌ 实现者定义接口（Java 思维）
// service/user.go
type UserServiceInterface interface { ... }
type UserService struct { ... }

// ✅ 使用者定义接口（Go 思维）
// handler/user.go
type UserGetter interface {
    GetUser(id int) (*User, error)
}

type UserHandler struct {
    svc UserGetter  // 只依赖需要的方法
}
```

### 编译期接口检查

```go
// 确保 MyStruct 实现了 Interface（编译期报错，不用等到运行时）
var _ Interface = (*MyStruct)(nil)
var _ io.Reader = (*MyFile)(nil)
```

## 七、接口与多态

```go
type Shape interface {
    Area() float64
    Perimeter() float64
}

type Circle struct{ Radius float64 }
func (c Circle) Area() float64      { return math.Pi * c.Radius * c.Radius }
func (c Circle) Perimeter() float64 { return 2 * math.Pi * c.Radius }

type Rectangle struct{ W, H float64 }
func (r Rectangle) Area() float64      { return r.W * r.H }
func (r Rectangle) Perimeter() float64 { return 2 * (r.W + r.H) }

// 多态
func PrintShapeInfo(s Shape) {
    fmt.Printf("Area: %.2f, Perimeter: %.2f\n", s.Area(), s.Perimeter())
}

PrintShapeInfo(Circle{5})         // Area: 78.54, Perimeter: 31.42
PrintShapeInfo(Rectangle{3, 4})   // Area: 12.00, Perimeter: 14.00
```

## 八、开发常见问题

### 问题 1：指针接收者与接口

```go
type Stringer interface {
    String() string
}

type Dog struct{ Name string }
func (d *Dog) String() string { return d.Name }

// var s Stringer = Dog{"Buddy"}   // ❌ Dog 没有实现 Stringer（只有 *Dog 实现了）
var s Stringer = &Dog{"Buddy"}     // ✅
```

### 问题 2：接口嵌套导致隐式要求

```go
type ReadWriteCloser interface {
    io.Reader
    io.Writer
    io.Closer
}
// 实现者必须同时实现 Read + Write + Close 三个方法
```

### 问题 3：空接口比较

空接口比较分两步：**先比类型，再比值**。

```go
// 规则一：类型不同 → 不相等
var a interface{} = 42
var b interface{} = "42"
fmt.Println(a == b)  // false（int vs string，类型不同）

// 规则二：类型相同，值相同 → 相等
var c interface{} = 42
var d interface{} = 42
fmt.Println(c == d)  // true

// 规则三：类型相同，但该类型不可比较 → panic ！
var e interface{} = []int{1, 2}
var f interface{} = []int{1, 2}
fmt.Println(e == f)  // ❌ panic: comparing uncomparable type []int
```

**哪些类型不可比较？**

| 不可比较 | 可比较 |
|---------|-------|
| `slice` | 基本类型（int, string, bool...） |
| `map` | 指针 |
| `func` | 数组（元素可比较时） |
| 含有以上字段的 struct | struct（所有字段可比较时） |

```go
// struct 取决于字段是否可比较
type A struct{ Name string; Age int }
type B struct{ Tags []string }

var a1 interface{} = A{"Jake", 18}
var a2 interface{} = A{"Jake", 18}
fmt.Println(a1 == a2)  // ✅ true（string 和 int 都可比较）

var b1 interface{} = B{[]string{"go"}}
var b2 interface{} = B{[]string{"go"}}
fmt.Println(b1 == b2)  // ❌ panic（slice 字段不可比较）
```

**安全比较：用 reflect.DeepEqual**

```go
import "reflect"

var e interface{} = []int{1, 2}
var f interface{} = []int{1, 2}

// ✅ 不会 panic，深度比较值是否相等
fmt.Println(reflect.DeepEqual(e, f))  // true

// ⚠️ 注意：reflect.DeepEqual 有性能开销，不适合热点路径
// 一般用于测试断言，不用于生产业务逻辑
```

**底层原因**

```
interface{} 内部：{_type *_type, data unsafe.Pointer}

比较时运行时检查 _type 是否支持 ==（通过 tflag 标志位）
slice/map/func 的 _type.tflag 标记为"不可比较"
运行时检测到后直接 panic，而不是返回 false
```

### 问题 4：接口性能开销

```go
// 接口调用有轻微开销（虚方法表查找 + 无法内联）
// 热点路径上如果确定类型，可以用类型断言绕过接口

func process(r io.Reader) {
    // 如果确定是 *bytes.Buffer，直接断言出来避免接口开销
    if buf, ok := r.(*bytes.Buffer); ok {
        // 直接操作 buf（可内联）
        _ = buf.Bytes()
    }
}
```

---

## 九、面试高频题

### Q1: iface 和 eface 的区别？

`iface` 用于非空接口（有方法），包含方法表 `itab`。`eface` 用于空接口 `interface{}`，只存类型和数据指针。

### Q2: 为什么 nil 指针赋给接口后不等于 nil？

接口 = {类型指针, 数据指针}。nil 指针赋给接口后，类型指针有值（指向具体类型），只有数据指针是 nil。两个指针不全为 nil，所以接口 != nil。

### Q3: Go 接口和 Java 接口有什么区别？

Go 是隐式实现（鸭子类型），不需要 `implements`。Java 是显式实现。Go 接口一般很小（1-3 个方法），Java 接口往往很大。

### Q4: 什么时候用 interface{} / any？

尽量少用。Go 1.18+ 有泛型后，大部分"通用容器"场景应该用泛型替代。空接口只用于真正无法确定类型的场景（如 JSON 解析、框架反射）。

### Q5: 如何在编译期确认类型实现了接口？

```go
var _ MyInterface = (*MyStruct)(nil)
```

如果 `*MyStruct` 没实现 `MyInterface` 的所有方法，编译器立即报错。
