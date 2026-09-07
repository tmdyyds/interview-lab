# Go 结构体（Struct）

**标签**: #go #basics #struct #method #高频

---

## 一、结构体基础

```go
// 定义
type User struct {
    ID        int64     `json:"id" db:"id"`
    Name      string    `json:"name" db:"name"`
    Email     string    `json:"email"`
    CreatedAt time.Time `json:"created_at"`
    isActive  bool      // 小写 → 未导出（包外不可见）
}

// 初始化
u1 := User{ID: 1, Name: "Jake", Email: "jake@example.com"}  // 命名字段（推荐）
u2 := User{1, "Jake", "jake@example.com", time.Now(), true}  // 顺序字段（不推荐，加字段会破坏）
u3 := new(User)   // 返回 *User，各字段零值
u4 := &User{}     // 同上（更常用）

// 匿名结构体（一次性使用）
config := struct {
    Host string
    Port int
}{Host: "localhost", Port: 8080}
```

## 二、方法（Method）

### 值接收者 vs 指针接收者

```go
type Rectangle struct {
    Width, Height float64
}

// 值接收者：方法内操作的是副本，不修改原对象
func (r Rectangle) Area() float64 {
    return r.Width * r.Height
}

// 指针接收者：方法内操作原对象本身
func (r *Rectangle) Scale(factor float64) {
    r.Width *= factor   // 修改生效
    r.Height *= factor
}

// 调用
rect := Rectangle{10, 5}
fmt.Println(rect.Area())  // 50
rect.Scale(2)             // Go 自动取地址：(&rect).Scale(2)
fmt.Println(rect.Area())  // 200
```

### 如何选择？

| 使用指针接收者的场景 | 使用值接收者的场景 |
|-------------------|-----------------|
| 需要修改接收者的字段 | 不需要修改 |
| 结构体很大（避免拷贝） | 结构体很小（几个基本类型字段） |
| 一致性（一个方法用指针，全部都用） | 确实是只读操作 |
| 实现接口时（常用指针） | map 的 key / 需要作为值传递 |

**经验法则**：有疑问就用指针接收者。保持一致性比什么都重要。

### 方法集规则（决定能否实现接口）

```go
type Mover interface {
    Move()
}

type Dog struct{ Name string }

func (d *Dog) Move() { fmt.Println(d.Name, "is moving") }

// *Dog 实现了 Mover ✅
var m Mover = &Dog{"Buddy"}  // ✅

// Dog 没有实现 Mover ❌（Move 是指针接收者）
// var m2 Mover = Dog{"Buddy"}  // ❌ 编译错误

// 规则：
// T 的方法集 = 值接收者方法
// *T 的方法集 = 值接收者方法 + 指针接收者方法
```

## 三、结构体嵌入（Embedding / 组合）

Go 没有继承，用**嵌入（组合）**代替。

```go
// 基础类型
type BaseModel struct {
    ID        int64
    CreatedAt time.Time
    UpdatedAt time.Time
}

// 嵌入（匿名字段）
type Product struct {
    BaseModel            // 嵌入，不写字段名
    Name      string
    Price     float64
}

// 嵌入的效果：Product "继承"了 BaseModel 的字段和方法
p := Product{
    BaseModel: BaseModel{ID: 1, CreatedAt: time.Now()},
    Name:      "iPhone",
    Price:     7999,
}

fmt.Println(p.ID)         // 直接访问（提升到外层）
fmt.Println(p.CreatedAt)  // 直接访问
fmt.Println(p.BaseModel.ID)  // 也可以显式访问

// 嵌入的方法也被"提升"
func (b BaseModel) TableName() string {
    return "base"
}
fmt.Println(p.TableName())  // Product 直接调用 BaseModel 的方法
```

### 嵌入的冲突解决

```go
type A struct{ Name string }
type B struct{ Name string }
type C struct {
    A
    B
}

c := C{A: A{"a"}, B: B{"b"}}
// fmt.Println(c.Name)    // ❌ 编译错误：ambiguous selector
fmt.Println(c.A.Name)     // ✅ 显式指定
fmt.Println(c.B.Name)     // ✅
```

### 嵌入接口

```go
// 嵌入接口 = 要求实现该接口的所有方法
type ReadWriter interface {
    io.Reader  // 嵌入 Reader 接口
    io.Writer  // 嵌入 Writer 接口
}
// 等价于：
type ReadWriter interface {
    Read(p []byte) (n int, err error)
    Write(p []byte) (n int, err error)
}
```

## 四、Struct Tag

```go
type User struct {
    ID       int    `json:"id" db:"user_id" validate:"required"`
    Name     string `json:"name" db:"name" validate:"min=2,max=50"`
    Email    string `json:"email,omitempty" validate:"email"`    // omitempty: 空值不序列化
    Password string `json:"-"`                                   // -: 永不序列化
    Age      int    `json:"age" db:"age" validate:"gte=0,lte=150"`
}

// 通过反射读取 Tag
t := reflect.TypeOf(User{})
field, _ := t.FieldByName("Email")
jsonTag := field.Tag.Get("json")    // "email,omitempty"
dbTag := field.Tag.Get("db")        // ""（没设置 db tag）
```

常用 Tag：

| Tag | 库 | 作用 |
|-----|-----|------|
| `json:"name"` | encoding/json | JSON 序列化字段名 |
| `db:"column"` | sqlx / gorm | 数据库列名 |
| `validate:"required"` | go-playground/validator | 参数校验规则 |
| `yaml:"key"` | gopkg.in/yaml | YAML 配置映射 |
| `gorm:"primaryKey"` | gorm | ORM 映射 |
| `mapstructure:"key"` | mapstructure | 配置解析 |

## 五、结构体比较

```go
// 所有字段都可比较 → 结构体可比较
type Point struct{ X, Y int }
p1 := Point{1, 2}
p2 := Point{1, 2}
fmt.Println(p1 == p2)  // true

// 含不可比较字段（slice, map, func）→ 结构体不可比较
type Data struct {
    Values []int  // slice 不可比较
}
d1 := Data{[]int{1, 2}}
d2 := Data{[]int{1, 2}}
// fmt.Println(d1 == d2)  // ❌ 编译错误

// 解决：用 reflect.DeepEqual（性能差）或自定义 Equal 方法
fmt.Println(reflect.DeepEqual(d1, d2))  // true
```

## 六、空结构体 `struct{}`

```go
// 大小为 0，不占内存
var s struct{}
fmt.Println(unsafe.Sizeof(s))  // 0

// 用途一：Set（只需要 key，value 不占空间）
set := make(map[string]struct{})
set["apple"] = struct{}{}
if _, ok := set["apple"]; ok { ... }

// 用途二：Channel 只做信号通知（不传数据）
done := make(chan struct{})
go func() {
    // 工作完成
    close(done)  // 或 done <- struct{}{}
}()
<-done

// 用途三：实现接口但不需要存数据
type NullLogger struct{}
func (NullLogger) Log(msg string) {}
```

## 七、开发常见问题

### 问题 1：结构体初始化忘记字段名

```go
// ❌ 不推荐：按顺序初始化（加字段会出 bug）
u := User{1, "Jake", "jake@test.com", time.Now(), true}

// ✅ 推荐：命名字段
u := User{ID: 1, Name: "Jake"}
```

### 问题 2：值接收者实现接口导致意外

```go
type Counter struct{ count int }

func (c Counter) Increment() { c.count++ }  // ⚠️ 值接收者 → 修改无效！
func (c Counter) Get() int { return c.count }

c := Counter{}
c.Increment()
fmt.Println(c.Get())  // 0（没变！Increment 操作的是副本）
```

### 问题 3：嵌入不是继承

```go
type Animal struct{}
func (a Animal) Speak() { fmt.Println("...") }

type Dog struct{ Animal }
func (d Dog) Speak() { fmt.Println("Woof!") }  // "重写"

var d Dog
d.Speak()         // "Woof!"（调用 Dog 的方法）
d.Animal.Speak()  // "..."（显式调用被嵌入的方法）

// 但多态不存在！
// var a Animal = d  // ❌ Dog 不是 Animal 的子类
```

---

## 八、面试高频题

### Q1: 值接收者和指针接收者的区别？

值接收者操作副本（不修改原对象），指针接收者操作原对象。`*T` 的方法集包含两者，`T` 只包含值接收者方法。

### Q2: Go 怎么实现"继承"？

组合（嵌入）。嵌入一个结构体后，外层结构体自动拥有其字段和方法。但这不是真正的继承——没有多态。

### Q3: 空结构体有什么用？

大小为 0，不占内存。用作 map 的 value（实现 Set）、channel 信号通知、无状态接口实现。

### Q4: 什么时候结构体不能比较？

含有 slice、map 或 func 类型字段时不可比较（编译报错）。用 `reflect.DeepEqual` 或自定义 Equal 方法。

### Q5: struct tag 是怎么工作的？

编译期存储在类型元数据中，运行时通过 `reflect.StructField.Tag.Get("json")` 读取。框架（json、gorm、validator）用反射扫描 tag 来决定行为。
