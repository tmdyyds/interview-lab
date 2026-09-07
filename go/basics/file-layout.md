# Go 文件内布局规范

**标签**: #go #basics #style #organization

---

## 一、文件内标准顺序

```go
// 1. package 声明
package user

// 2. import（标准库 / 第三方 / 内部包，空行分隔）
import (
    "errors"
    "time"

    "github.com/gin-gonic/gin"

    "myapp/internal/domain"
)

// 3. 常量
const (
    MaxNameLength = 50
    MinAge        = 0
    DefaultPageSize = 20
)

// 4. 包级变量 / 错误变量
var (
    ErrNotFound       = errors.New("user not found")
    ErrEmailDuplicate = errors.New("email already exists")
    ErrUnauthorized   = errors.New("unauthorized")
)

// 5. 接口定义（定义契约，放在实现前面）
type UserRepository interface {
    FindByID(id int) (*User, error)
    FindByEmail(email string) (*User, error)
    Create(u *User) error
    Update(u *User) error
    Delete(id int) error
}

// 6. 核心 struct（主角，放前面）
type UserService struct {
    repo   UserRepository
    logger *Logger
    now    func() time.Time  // 便于测试中注入时间
}

// 7. 构造函数（紧跟 struct）
func NewUserService(repo UserRepository, logger *Logger) *UserService {
    return &UserService{
        repo:   repo,
        logger: logger,
        now:    time.Now,
    }
}

// 8. 公开方法（按业务重要性 / 使用频率排序）
func (s *UserService) Register(req *CreateUserRequest) (*User, error) { ... }
func (s *UserService) Login(email, password string) (*User, error) { ... }
func (s *UserService) FindByID(id int) (*User, error) { ... }
func (s *UserService) Update(id int, req *UpdateUserRequest) (*User, error) { ... }
func (s *UserService) Delete(id int) error { ... }

// 9. 私有方法（实现细节，放公开方法之后）
func (s *UserService) validate(req *CreateUserRequest) error { ... }
func (s *UserService) hashPassword(pw string) (string, error) { ... }

// 10. 辅助 struct（请求体、响应体、内部用的小结构）
type CreateUserRequest struct {
    Name     string `json:"name"     validate:"required,min=2,max=50"`
    Email    string `json:"email"    validate:"required,email"`
    Password string `json:"password" validate:"required,min=8"`
    Age      int    `json:"age"      validate:"gte=0,lte=150"`
}

type UpdateUserRequest struct {
    Name string `json:"name" validate:"omitempty,min=2,max=50"`
    Age  int    `json:"age"  validate:"omitempty,gte=0,lte=150"`
}

type UserResponse struct {
    ID        int       `json:"id"`
    Name      string    `json:"name"`
    Email     string    `json:"email"`
    CreatedAt time.Time `json:"created_at"`
}

// 11. 独立工具函数（不属于任何 struct）
func toUserResponse(u *User) *UserResponse { ... }
func sanitizeEmail(email string) string { ... }
```

---

## 二、核心原则：struct 和方法放在一起

同一个文件里有多个 struct 时，每个 struct 和它的方法紧挨着放，**不要**把所有 struct 堆顶部、所有方法堆底部。

```go
// ✅ 好：struct 和方法放一起，读代码像读故事
type OrderService struct {
    repo OrderRepository
}

func NewOrderService(repo OrderRepository) *OrderService { ... }
func (s *OrderService) Create(req *CreateOrderRequest) (*Order, error) { ... }
func (s *OrderService) Cancel(id int) error { ... }
func (s *OrderService) calcTotal(items []OrderItem) float64 { ... }

// ---- 下一个主角 ----

type OrderRepo struct {
    db *sql.DB
}

func NewOrderRepo(db *sql.DB) *OrderRepo { ... }
func (r *OrderRepo) Insert(o *Order) error { ... }
func (r *OrderRepo) FindByID(id int) (*Order, error) { ... }
```

```go
// ❌ 差：所有 struct 堆顶，所有方法堆底，读起来要反复跳跃
type OrderService struct { ... }
type OrderRepo struct { ... }
type CreateOrderRequest struct { ... }
type OrderItem struct { ... }

// ... 几十行之后 ...
func NewOrderService(...) { ... }
func (s *OrderService) Create(...) { ... }
func NewOrderRepo(...) { ... }
func (r *OrderRepo) Insert(...) { ... }
```

---

## 三、辅助 struct 的位置

两种流派都可以，保持团队一致即可：

```go
// 流派一：紧跟使用它的方法（局部性强，适合 struct 少的情况）
func (s *UserService) Register(req *CreateUserRequest) (*User, error) { ... }

type CreateUserRequest struct {  // 就放在用它的方法后面
    Name  string
    Email string
}

// 流派二：集中放文件底部，或单独一个 model.go
// 适合：辅助 struct 很多，或多个方法共用同一批 struct
```

---

## 四、import 分组规范

```go
import (
    // 第一组：标准库
    "context"
    "errors"
    "fmt"
    "time"

    // 第二组：第三方库（空行分隔）
    "github.com/gin-gonic/gin"
    "go.uber.org/zap"

    // 第三组：项目内部包（空行分隔）
    "myapp/internal/domain"
    "myapp/pkg/logger"
)
```

`goimports` / `gofmt` 会自动整理，不用手动排序。

---

## 五、读者视角：从上到下的阅读体验

```
package + import     → 这个文件用了什么
        ↓
const + var          → 有哪些配置和错误
        ↓
interface            → 对外承诺什么契约
        ↓
struct + 构造函数     → 主角是谁，怎么创建
        ↓
公开方法              → 能做什么（先知道能做什么）
        ↓
私有方法              → 怎么做的（再看实现细节）
        ↓
辅助 struct           → 用到了哪些数据结构
        ↓
工具函数              → 有哪些辅助逻辑
```

读者从上往下，**先了解契约和能力，再深入实现细节**。

---

## 六、文件长度参考

| 文件行数 | 状态 |
|---------|------|
| < 200 行 | 健康 |
| 200 ~ 400 行 | 可接受，留意是否需要拆 |
| > 400 行 | 考虑按职责拆成多个文件 |
| > 800 行 | 强烈建议拆分 |

行数只是参考，**职责混乱比行数多更需要拆**。一个 500 行但职责单一的文件，比一个 200 行但混了 HTTP + 业务 + DB 的文件健康得多。

---

## 七、面试常见问题

### Q: Go 文件内有没有强制的代码顺序？

没有强制要求，编译器不关心顺序。但社区有约定俗成的顺序：常量 → 变量 → 接口 → struct → 构造函数 → 方法 → 工具函数。

### Q: 一个文件应该有多少个 struct？

没有硬性限制。关键是**一个文件讲清楚一件事**。同一职责层的 struct（比如一个 service 及其请求/响应体）放一起完全合理。

### Q: 私有方法和公开方法怎么排？

公开方法在前，私有方法在后。读者先了解"能做什么"，再看"怎么实现的"。
