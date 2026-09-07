# Go 包管理 / import / init

**标签**: #go #basics #packages #init #高频

---

## 一、包（Package）基础

```go
// 每个 .go 文件第一行必须声明包名
package main      // 可执行程序入口
package utils     // 库包

// 可见性规则（首字母大小写）
func PublicFunc()  {}  // 包外可见（导出）
func privateFunc() {}  // 包内可见（未导出）
type User struct {
    Name string  // 导出字段
    age  int     // 未导出字段
}
```

## 二、import 详解

```go
// 基本导入
import "fmt"
import "net/http"

// 批量导入（推荐）
import (
    "fmt"
    "net/http"
    "os"

    "github.com/gin-gonic/gin"          // 第三方包
    "myproject/internal/service"        // 项目内部包
)

// 别名导入
import (
    f "fmt"                            // 别名：f.Println(...)
    "database/sql"
)

// 点导入（将包内导出标识符导入当前命名空间，不推荐）
import . "fmt"
// 之后可以直接 Println("hello") 而不是 fmt.Println("hello")

// 空白导入（只执行 init 函数，不使用包内任何标识符）
import _ "github.com/go-sql-driver/mysql"  // 注册 MySQL 驱动
import _ "net/http/pprof"                  // 注册 pprof HTTP 路由
```

## 三、init 函数

### 3.1 基本规则

```go
// init() 特殊函数：
// - 没有参数，没有返回值
// - 不能被显式调用
// - 同一个文件可以有多个 init()（按声明顺序执行）
// - 同一个包多个文件的 init() 按文件名字母序执行
// - 在 main() 之前自动执行

func init() {
    fmt.Println("init 1")
}

func init() {
    fmt.Println("init 2")  // ✅ 同一文件可以有多个 init
}

func main() {
    fmt.Println("main")
}
// 输出：init 1 → init 2 → main
```

### 3.2 完整执行顺序

```
程序启动
    │
    ├── 1. 导入包（递归，被依赖的包先初始化）
    │       │
    │       ├── 包级变量初始化（按依赖关系 + 声明顺序）
    │       └── init() 函数执行
    │
    ├── 2. main 包
    │       │
    │       ├── 包级变量初始化
    │       └── init() 函数执行
    │
    └── 3. main() 函数执行
```

**示例**：

```
main.go imports pkg_a
pkg_a imports pkg_b

执行顺序：
  pkg_b 变量初始化 → pkg_b init()
  → pkg_a 变量初始化 → pkg_a init()
  → main 变量初始化 → main init()
  → main()
```

### 3.3 init 的常见用途

```go
// 1. 注册驱动 / 插件
func init() {
    sql.Register("mysql", &MySQLDriver{})
}

// 2. 初始化配置
var config *Config

func init() {
    config = loadConfig("config.yaml")
}

// 3. 检查环境
func init() {
    if os.Getenv("API_KEY") == "" {
        log.Fatal("API_KEY environment variable is required")
    }
}

// 4. 注册 HTTP 路由（如 pprof）
func init() {
    http.HandleFunc("/debug/pprof/", pprof.Index)
}
```

### 3.4 init 的注意事项

```go
// ⚠️ init 不能有参数和返回值
// func init(x int) {}  // ❌ 编译错误

// ⚠️ init 不能被显式调用
// init()  // ❌ 编译错误

// ⚠️ 不要在 init 中做耗时操作（会拖慢启动）
func init() {
    // ❌ 不好：连接数据库、加载大文件
    db, _ = sql.Open("mysql", dsn)  // 阻塞启动
}
// ✅ 推荐：延迟到第一次使用时初始化（sync.Once）

// ⚠️ 不要依赖不同文件 init 的执行顺序
// 文件名变了顺序就变了！
```

## 四、Go Modules

### 4.1 基本命令

```bash
# 初始化模块
go mod init github.com/yourname/project

# 添加依赖
go get github.com/gin-gonic/gin@latest
go get github.com/gin-gonic/gin@v1.9.1    # 指定版本

# 整理依赖（删除未使用的，添加缺失的）
go mod tidy

# 下载依赖到本地缓存
go mod download

# 查看依赖图
go mod graph

# 替换依赖（本地开发/fork 修复时用）
go mod edit -replace github.com/old/pkg=../local/pkg

# 验证依赖完整性
go mod verify
```

### 4.2 go.mod 文件

```go
module github.com/jake/myproject

go 1.21

require (
    github.com/gin-gonic/gin v1.9.1
    gorm.io/gorm v1.25.5
)

require (
    // indirect: 间接依赖（被直接依赖引用的）
    github.com/bytedance/sonic v1.10.2 // indirect
)

replace (
    // 替换规则（本地调试 / 私有仓库）
    github.com/old/pkg => ../local/pkg
)

exclude (
    // 排除有 bug 的版本
    github.com/some/pkg v1.2.3
)
```

### 4.3 go.sum 文件

```
// 每个依赖的哈希校验（保证可重复构建）
// 不要手动编辑，go mod tidy 自动维护
// 必须提交到 Git（团队成员拿到相同依赖）
```

### 4.4 GOPROXY

```bash
# 国内加速（推荐）
go env -w GOPROXY=https://goproxy.cn,direct

# 私有仓库不走代理
go env -w GOPRIVATE=github.com/your-company/*
```

## 五、internal 包

```
myproject/
├── cmd/
│   └── server/main.go
├── internal/           ← 只有 myproject 内的代码能导入
│   ├── service/
│   └── repository/
├── pkg/                ← 对外公开的库代码
│   └── utils/
└── go.mod
```

`internal/` 下的包只能被 `internal` 的父目录及其子目录导入。外部项目 `import "github.com/jake/myproject/internal/service"` 会编译报错。

## 六、开发常见问题

### 问题 1：循环导入

**场景**：用户服务需要查订单数，订单服务需要查用户名 → 互相导入 → 编译报错。

```
❌ 循环依赖：
  package user  →  import order
  package order →  import user   ← 编译错误：import cycle
```

---

#### ❌ 问题代码

```go
// package user
import "myapp/order"

type User struct{ ID int; Name string }

func (u *User) GetOrderCount() int {
    return order.CountByUserID(u.ID)  // user 依赖 order
}

// package order
import "myapp/user"

type Order struct{ ID int; UserID int }

func (o *Order) GetUserName() string {
    return user.FindByID(o.UserID).Name  // order 依赖 user ← 循环！
}
```

---

#### ✅ 解决方案一：提取公共接口到第三方包

把两个包都依赖的类型/接口，抽到独立的 `domain` 包，谁都只导入它。

```
myapp/
├── domain/      ← 新包，只放公共类型，不导入 user/order
│   └── types.go
├── user/
│   └── user.go  → import domain（不再 import order）
└── order/
    └── order.go → import domain（不再 import user）
```

```go
// package domain —— 只有纯类型，零依赖
type User struct {
    ID   int
    Name string
}

type Order struct {
    ID     int
    UserID int
}

// package user —— 只依赖 domain
import "myapp/domain"

func FindByID(id int) *domain.User { ... }

// package order —— 只依赖 domain
import "myapp/domain"

func CountByUserID(userID int) int { ... }

func (o *domain.Order) GetUserName(u *domain.User) string {
    return u.Name  // 不再需要导入 user 包
}
```

---

#### ✅ 解决方案二：依赖注入（通过 interface 解耦）

order 包定义一个接口，不直接引用 user 包的具体类型。

```go
// package order —— 自己定义接口，不 import user
type UserFinder interface {
    FindByID(id int) (string, error)  // 只依赖接口，不依赖具体类型
}

type OrderService struct {
    userFinder UserFinder  // 注入进来
}

func NewOrderService(uf UserFinder) *OrderService {
    return &OrderService{userFinder: uf}
}

func (s *OrderService) GetUserName(userID int) (string, error) {
    return s.userFinder.FindByID(userID)  // 调接口，不 import user
}

// package user —— 实现 order.UserFinder 接口（不需要 import order）
type UserService struct{}

func (us *UserService) FindByID(id int) (string, error) {
    // 查数据库...
    return "Jake", nil
}

// main.go —— 在最顶层组装
import (
    "myapp/user"
    "myapp/order"
)

func main() {
    us := &user.UserService{}
    os := order.NewOrderService(us)  // 把 user 注入进 order
    // user 和 order 互相不知道对方的存在 ✅
}
```

依赖方向变成单向：

```
❌ 之前：user ←→ order（循环）
✅ 之后：user → interface ← order（都依赖抽象，不依赖对方）
```

---

#### ✅ 解决方案三：合并两个包

如果两个包本来就高度耦合、总是一起变动，直接合并成一个包是最简单的。

```
❌ 之前：
  myapp/user/
  myapp/order/

✅ 之后：
  myapp/userorder/   ← 合并，内部随便互相调用
```

适合场景：两个包是同一个业务域（比如 `user` 和 `userProfile`），强行拆开反而增加复杂度。

---

#### 三种方案选择

| 方案 | 适合场景 |
|------|---------|
| 提取公共类型包 | 两个包都用到相同的数据结构 |
| 依赖注入 + interface | 两个包职责清晰，需要保持独立、可测试 |
| 合并包 | 两个包本来就是同一业务域，拆分过度 |

### 问题 2：import 了不使用

```go
import "fmt"  // ❌ 如果没用 fmt 会编译错误

// 解决（开发阶段临时）：
import _ "fmt"
// 或
var _ = fmt.Sprintf  // 不用于生产代码
```

### 问题 3：vendor 目录

```bash
# 将依赖复制到 vendor/ 目录（离线构建、CI 加速）
go mod vendor

# 使用 vendor 构建
go build -mod=vendor ./...
```

---

## 七、面试高频题

### Q1: init 函数的执行顺序？

被依赖的包先执行。同包按文件名字母序，同文件按声明顺序。所有 init 在 main() 之前。

### Q2: 一个包可以有多少个 init 函数？

无限制。同一个文件可以有多个 init，同一个包的不同文件也可以各有 init。

### Q3: import _ 是什么意思？

空白导入。只执行包的 init 函数（注册驱动等副作用），不使用包内任何标识符。

### Q4: Go 有循环导入吗？

不允许。编译期报错。解决方案：提取接口到第三方包，或用依赖倒置。

### Q5: go.sum 要提交到 Git 吗？

要。它保证团队所有人和 CI 拿到一模一样的依赖版本（可重复构建）。
