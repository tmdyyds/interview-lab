# Go 数据类型

**标签**: #go #basics #data-types #高频

---

## 一、类型分类总览

```
Go 类型体系
├── 基本类型（值类型）
│   ├── 布尔型：bool
│   ├── 整型：int, int8, int16, int32, int64, uint, uint8...
│   ├── 浮点型：float32, float64
│   ├── 复数型：complex64, complex128
│   ├── 字符串：string（不可变字节序列）
│   ├── 字节/字符：byte(uint8), rune(int32)
│   └── 指针：*T（虽然是引用语义，但指针本身是值类型）
│
├── 复合类型（值类型）
│   ├── 数组：[N]T（固定长度，值拷贝）
│   └── 结构体：struct
│
├── 引用类型（底层包含指针，赋值/传参时共享底层数据）
│   ├── 切片：[]T
│   ├── 映射：map[K]V
│   ├── 通道：chan T
│   ├── 函数：func
│   └── 接口：interface
│
└── 特殊
    ├── uintptr：存指针值的整数，不被 GC 追踪
    └── unsafe.Pointer：通用指针
```

## 二、值类型 vs 引用类型

| 特征 | 值类型 | 引用类型 |
|------|--------|----------|
| 赋值行为 | 完整拷贝 | 拷贝 header（共享底层数据） |
| 函数传参 | 值拷贝（修改不影响原值） | header 拷贝（修改影响原始数据） |
| 代表 | int, bool, string, array, struct | slice, map, chan, func, interface |
| 零值 | 0, false, "", 各字段零值 | nil |

```go
// 值类型：赋值后独立
a := [3]int{1, 2, 3}
b := a       // 完整拷贝
b[0] = 100   // a 不变

// 引用类型：共享底层
s1 := []int{1, 2, 3}
s2 := s1     // 共享底层数组
s2[0] = 100  // s1[0] 也变成 100！
```

## 三、零值（Zero Value）

Go 所有变量声明后自动初始化为零值（不存在"未初始化"的变量）。

| 类型 | 零值 | 说明 |
|------|------|------|
| bool | `false` | |
| 整型/浮点 | `0` / `0.0` | |
| string | `""` | 空字符串，不是 nil |
| pointer | `nil` | |
| slice | `nil` | len=0, cap=0，但可以 append |
| map | `nil` | 读不 panic，写 panic！ |
| chan | `nil` | 读写永久阻塞 |
| interface | `nil` | |
| struct | 各字段的零值 | |
| func | `nil` | |

```go
// 零值陷阱
var m map[string]int
_ = m["key"]     // ✅ 读 nil map 不 panic，返回零值 0
m["key"] = 1     // ❌ panic: assignment to entry in nil map

var s []int
s = append(s, 1) // ✅ append nil slice 是安全的

var ch chan int
ch <- 1          // ❌ 死锁：向 nil channel 发送永久阻塞
<-ch             // ❌ 死锁：从 nil channel 接收永久阻塞
```

## 四、整型细节

```go
// 平台相关大小（32位系统=32bit，64位系统=64bit）
var a int      // 大小取决于平台
var b uint     // 同上

// 固定大小
var c int32    // 明确 32 bit
var d int64    // 明确 64 bit

// 特殊别名
var e byte     // = uint8，用于字节
var f rune     // = int32，用于 Unicode 码点

// ⚠️ int 和 int64 不能直接运算（Go 无隐式转换）
var x int = 10
var y int64 = 20
// z := x + y  // ❌ 编译错误
z := int64(x) + y  // ✅ 显式转换
```

## 五、string 底层

```go
// string 底层结构（reflect.StringHeader）
type StringHeader struct {
    Data uintptr  // 指向字节数组的指针
    Len  int      // 字节长度（不是字符数！）
}

// string 是不可变的：
s := "hello"
// s[0] = 'H'  // ❌ 编译错误：cannot assign to s[0]

// 中文字符串：
s := "你好Go"
len(s)              // 8（字节数：3+3+1+1）
len([]rune(s))      // 4（字符数）
utf8.RuneCountInString(s)  // 4（推荐）

// string 与 []byte 转换（有拷贝开销）：
b := []byte(s)      // string → []byte（分配新内存 + 拷贝）
s2 := string(b)     // []byte → string（分配新内存 + 拷贝）

// ⚠️ 高频转换在热点路径上有性能问题
// 优化：unsafe 零拷贝转换（Go 1.20+ 有 unsafe.String / unsafe.SliceData）
```

## 六、类型转换 vs 类型断言

```go
// 类型转换（编译期，用于兼容类型间转换）
var i int = 42
var f float64 = float64(i)  // int → float64
var b byte = byte(i)        // int → byte（可能截断）

//变量.(目标类型)

// 类型断言（运行时，用于 interface 提取具体类型）
var x interface{} = "hello"
s := x.(string)             // 成功
// n := x.(int)             // panic: interface conversion

// 安全断言（推荐）
s, ok := x.(string)         // ok=true, s="hello"
n, ok := x.(int)            // ok=false, n=0

// 类型 switch
switch v := x.(type) {
case string:
    fmt.Println("string:", v)
case int:
    fmt.Println("int:", v)
default:
    fmt.Println("unknown")
}
```

## 七、类型别名 vs 类型定义

```go
// 类型定义（新类型，不继承原类型方法）
type UserID int64
type Money float64

var id UserID = 100
var m Money = 99.9
// id = int64(100)  // ❌ 不同类型不能直接赋值
id = UserID(100)    // ✅ 需要显式转换

// 类型别名（完全相同的类型，只是换个名字）
type byte = uint8   // Go 内置
type rune = int32   // Go 内置

// 自定义别名
type MyInt = int
var a MyInt = 10
var b int = a  // ✅ 直接赋值（同一类型）
```

## 八、开发常见问题

### 问题 1：隐式转换不存在

```go
var a int32 = 1
var b int64 = 2
// c := a + b  // ❌ 编译错误

// Go 不做任何隐式类型转换，必须显式
c := int64(a) + b  // ✅
```

### 问题 2：string 遍历的两种方式

```go
s := "Hello你好"

// 方式一：按字节遍历
for i := 0; i < len(s); i++ {
    fmt.Printf("%d: %c\n", i, s[i])  // 中文乱码
}

// 方式二：按字符（rune）遍历（推荐）
for i, ch := range s {
    fmt.Printf("%d: %c\n", i, ch)  // 正确显示中文
}
```

### 问题 3：数组是值类型

```go
// 数组传参是完整拷贝！
func modify(arr [3]int) {
    arr[0] = 100  // 修改的是副本
}

a := [3]int{1, 2, 3}
modify(a)
fmt.Println(a[0])  // 1（原数组不变）

// 解决：传指针或用 slice
func modifySlice(s []int) {
    s[0] = 100  // 修改生效
}
```

### 问题 4：常量无类型

```go
// 无类型常量可以参与不同类型运算
const x = 10
var a int32 = x     // ✅
var b float64 = x   // ✅
var c int64 = x     // ✅

// 有类型常量则不行
const y int32 = 10
// var d int64 = y  // ❌ 类型不匹配
```

---

## 九、面试高频题

### Q1: 值类型和引用类型的区别？

值类型赋值是完整拷贝（int, array, struct），引用类型赋值拷贝的是 header 结构体（共享底层数据），包括 slice、map、chan。

### Q2: nil slice 和空 slice 的区别？

```go
var s1 []int        // nil slice: s1 == nil, len=0, cap=0
s2 := []int{}       // 空 slice: s2 != nil, len=0, cap=0
s3 := make([]int,0) // 空 slice: s3 != nil, len=0, cap=0

// 行为上几乎一样（都能 append），但 JSON 编码不同：
json.Marshal(s1) // "null"
json.Marshal(s2) // "[]"
```

### Q3: string 是值类型还是引用类型？

string 是值类型（赋值拷贝 StringHeader），但底层字节数据不可变（不会拷贝底层字节）。string 赋值只拷贝 16 字节（指针+长度），很廉价。

### Q4: 为什么 map 的 value 不能取地址？

```go
m := map[string]int{"a": 1}
// p := &m["a"]  // ❌ 编译错误

// 原因：map 扩容时 value 可能被搬迁到新内存，地址失效
// 解决：value 用指针类型
m2 := map[string]*int{}
```

### Q5: rune 和 byte 的区别？

- `byte` = `uint8`，表示一个原始字节
- `rune` = `int32`，表示一个 Unicode 码点（字符）
- ASCII 字符用 byte 够了，中文/emoji 需要 rune
