# Go 错误处理

**标签**: #go #basics #error #高频

---

## 一、error 接口

```go
// error 是内置接口
type error interface {
    Error() string
}

// 最简单的实现
err := errors.New("something went wrong")
err := fmt.Errorf("user %d not found", id)
```

## 二、自定义错误

```go
// 方式一：结构体实现 error 接口
type NotFoundError struct {
    Resource string
    ID       int64
}

func (e *NotFoundError) Error() string {
    return fmt.Sprintf("%s with id %d not found", e.Resource, e.ID)
}

// 使用
func GetUser(id int64) (*User, error) {
    user := db.Find(id)
    if user == nil {
        return nil, &NotFoundError{Resource: "user", ID: id}
    }
    return user, nil
}

// 方式二：哨兵错误（Sentinel Error）
var (
    ErrNotFound     = errors.New("not found")
    ErrUnauthorized = errors.New("unauthorized")
    ErrInternal     = errors.New("internal error")
)
```

## 三、errors 包（Go 1.13+）

### 错误包装（%w）

```go
// 包装错误：保留原始错误 + 添加上下文
func GetUser(id int64) (*User, error) {
    user, err := db.QueryUser(id)
    if err != nil {
        return nil, fmt.Errorf("GetUser(%d): %w", id, err)
        // 包装链：GetUser(123): QueryUser: connection refused
    }
    return user, nil
}
```

### errors.Is（判断错误链中是否包含某个错误）

```go
err := fmt.Errorf("service error: %w", ErrNotFound)

// 递归检查整个错误链
errors.Is(err, ErrNotFound)  // true（即使被包装了多层）

// 等价于手动 Unwrap 循环比较
// 用于哨兵错误的判断
if errors.Is(err, sql.ErrNoRows) {
    // 数据不存在
}
```

### errors.As（从错误链中提取特定类型）

```go
var notFound *NotFoundError
if errors.As(err, &notFound) {
    // 提取成功，可以读取字段
    fmt.Println(notFound.Resource, notFound.ID)
}

// 用于需要获取错误详情的场景
var validErr *ValidationError
if errors.As(err, &validErr) {
    return BadRequest(validErr.Fields)
}
```

### errors.Unwrap

```go
// 获取被包装的内层错误
inner := errors.Unwrap(err)

// 自定义错误实现 Unwrap 方法
type AppError struct {
    Code    int
    Message string
    Err     error  // 内层错误
}

func (e *AppError) Error() string { return e.Message }
func (e *AppError) Unwrap() error { return e.Err }  // 支持 Is/As 递归
```

## 四、错误处理最佳实践

### 原则 1：错误只处理一次

```go
// ❌ 错误：log + return（处理了两次）
func GetUser(id int64) (*User, error) {
    user, err := db.Query(id)
    if err != nil {
        log.Printf("query failed: %v", err)  // 处理一次
        return nil, err                       // 又交给调用者处理 → 重复日志
    }
    return user, nil
}

// ✅ 正确：包装上下文 + 向上传递（让最上层统一处理）
func GetUser(id int64) (*User, error) {
    user, err := db.Query(id)
    if err != nil {
        return nil, fmt.Errorf("GetUser(%d): %w", id, err)
    }
    return user, nil
}
```

### 原则 2：在调用链顶层统一记录日志

```go
// HTTP Handler / gRPC Interceptor 统一处理
func handleRequest(w http.ResponseWriter, r *http.Request) {
    user, err := userService.GetUser(123)
    if err != nil {
        // 顶层：记日志 + 返回给客户端
        log.Printf("request failed: %v", err)

        var notFound *NotFoundError
        if errors.As(err, &notFound) {
            http.Error(w, "not found", 404)
        } else {
            http.Error(w, "internal error", 500)
        }
        return
    }
    json.NewEncoder(w).Encode(user)
}
```

### 原则 3：不要忽略错误

```go
// ❌ 极差
result, _ := doSomething()

// ❌ 差（吞掉了错误信息）
if err != nil {
    return err  // 没有任何上下文，调试困难
}

// ✅ 好（添加上下文）
if err != nil {
    return fmt.Errorf("doSomething for user %d: %w", userID, err)
}
```

### 原则 4：区分可恢复 vs 不可恢复错误

```go
// 可恢复：返回 error
func Connect(addr string) (*Conn, error) {
    // 网络超时、地址无效 → 调用者可以重试或降级
}

// 不可恢复：panic（仅限初始化阶段或编程 bug）
func MustParseURL(raw string) *url.URL {
    u, err := url.Parse(raw)
    if err != nil {
        panic(err)  // 硬编码的 URL 解析失败 = 代码有 bug
    }
    return u
}
```

## 五、错误码设计（业务系统）

```go
// 大型项目推荐错误码体系
type BizError struct {
    Code    int    `json:"code"`     // 机器可读
    Message string `json:"message"`  // 用户可读
    Err     error  `json:"-"`        // 内部错误（不暴露给用户）
}

func (e *BizError) Error() string { return e.Message }
func (e *BizError) Unwrap() error { return e.Err }

// 预定义错误码
var (
    ErrUserNotFound   = &BizError{Code: 10001, Message: "用户不存在"}
    ErrInvalidParam   = &BizError{Code: 10002, Message: "参数无效"}
    ErrStockInsufficient = &BizError{Code: 20001, Message: "库存不足"}
)

// 使用
func CreateOrder(req *OrderRequest) error {
    stock, err := checkStock(req.SKU)
    if err != nil {
        return fmt.Errorf("check stock: %w", err)
    }
    if stock < req.Quantity {
        return ErrStockInsufficient
    }
    // ...
}
```

---

## 六、面试高频题

### Q1: errors.Is 和 == 的区别？

`==` 只比较当前层。`errors.Is` 递归检查整个包装链（Unwrap 链）。被 `%w` 包装多层后仍能匹配。

### Q2: errors.As 的用途？

从错误链中提取特定类型的错误，获取其字段（如错误码、详情）。类似 type assertion 但在错误链上递归。

### Q3: Go 为什么不用 try/catch？

Go 的设计哲学：错误是值，应该被显式检查和处理。`if err != nil` 让错误处理的代码路径清晰可见，不会被 try/catch 隐藏。

### Q4: panic 什么时候用？

两种场景：1. 初始化失败（数据库连不上、配置错误） 2. 不应该发生的编程 bug（Must* 函数）。业务逻辑错误一律用 error。

### Q5: 如何设计大型项目的错误体系？

分层：底层返回标准 error → 业务层包装为 BizError（含错误码）→ 传输层（HTTP/gRPC）映射为状态码 + 用户消息。全链路通过 `%w` 保留上下文。
