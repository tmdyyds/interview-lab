# Gin 参数校验

**标签**: #go #gin #validator #binding

Gin 集成了 [go-playground/validator/v10](https://github.com/go-playground/validator)，通过 struct tag 声明校验规则。

---

## 一、基本用法

```go
type CreateUserReq struct {
    Name     string `json:"name"     binding:"required,min=2,max=50"`
    Email    string `json:"email"    binding:"required,email"`
    Age      int    `json:"age"      binding:"gte=0,lte=150"`
    Password string `json:"password" binding:"required,min=8"`
    Role     string `json:"role"     binding:"oneof=admin user guest"`
}

r.POST("/users", func(c *gin.Context) {
    var req CreateUserReq
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    // 校验通过，处理业务
})
```

请求 `{"name":"a"}`，会返回：

```
Key: 'CreateUserReq.Name'  Error:Field validation for 'Name' failed on the 'min' tag
Key: 'CreateUserReq.Email' Error:Field validation for 'Email' failed on the 'required' tag
Key: 'CreateUserReq.Password' Error:...
```

---

## 二、常用校验规则

### 存在性 & 空值

| Tag | 含义 |
|-----|------|
| `required` | 非零值（`0`、`""`、`nil` 都不通过） |
| `omitempty` | 空值时跳过校验（配合 required 之外的规则） |
| `-` | 跳过校验 |

### 字符串

| Tag | 含义 |
|-----|------|
| `min=2` / `max=50` | 长度范围（string 按字符数） |
| `len=6` | 精确长度 |
| `email` | 邮箱格式 |
| `url` | URL 格式 |
| `uuid` | UUID 格式 |
| `alphanum` | 字母数字 |
| `numeric` | 纯数字（包括字符串形式） |
| `hexadecimal` | 十六进制 |
| `contains=foo` | 包含 `foo` |
| `startswith=abc` | 前缀 |
| `endswith=xyz` | 后缀 |

### 数值

| Tag | 含义 |
|-----|------|
| `gt=0` / `gte=0` | 大于 / 大于等于 |
| `lt=100` / `lte=100` | 小于 / 小于等于 |
| `eq=42` / `ne=0` | 等于 / 不等于 |

### 集合

| Tag | 含义 |
|-----|------|
| `oneof=a b c` | 值必须是这几个之一 |
| `dive` | 深入 slice/map 元素校验 |
| `unique` | slice 元素唯一 |

```go
type Req struct {
    Tags []string `binding:"required,min=1,max=5,dive,min=1,max=20"`
    //                        ↑ 切片本身规则  ↑ dive 后是每个元素的规则
}
```

### 字段间比较

| Tag | 含义 |
|-----|------|
| `eqfield=Field` | 等于另一个字段 |
| `nefield=Field` | 不等于另一个字段 |
| `gtfield=Field` | 大于另一个字段 |
| `required_with=Field` | 当 Field 有值时本字段必填 |
| `required_without=Field` | 当 Field 无值时本字段必填 |

```go
type SignupReq struct {
    Password        string `binding:"required,min=8"`
    ConfirmPassword string `binding:"required,eqfield=Password"`
}
```

---

## 三、支持的 tag 类型

一个字段可以同时支持多种输入来源：

```go
type Req struct {
    ID   int    `json:"id"   form:"id"   uri:"id"   binding:"required"`
    Name string `json:"name" form:"name" query:"name" binding:"required,min=2"`
}
```

| tag | 用于 |
|-----|------|
| `json` | JSON body |
| `form` | form-urlencoded / multipart |
| `uri` | 路径参数 |
| `query` | URL 查询参数 |
| `header` | 请求头 |
| `xml` / `yaml` | XML/YAML body |

对应的绑定方法：

```go
c.ShouldBindJSON(&req)
c.ShouldBind(&req)          // 根据 Content-Type 自动
c.ShouldBindUri(&req)
c.ShouldBindQuery(&req)
c.ShouldBindHeader(&req)
c.ShouldBindXML(&req)
c.ShouldBindYAML(&req)

// 通用
c.ShouldBindWith(&req, binding.JSON)
```

---

## 四、错误处理与友好提示

默认错误信息是英文且不友好。生产上一般封装：

```go
import (
    "errors"
    "github.com/go-playground/validator/v10"
)

func FormatValidationError(err error) map[string]string {
    result := make(map[string]string)
    var ve validator.ValidationErrors
    if errors.As(err, &ve) {
        for _, fe := range ve {
            result[fe.Field()] = translateTag(fe)
        }
    }
    return result
}

func translateTag(fe validator.FieldError) string {
    switch fe.Tag() {
    case "required":
        return fe.Field() + " 不能为空"
    case "email":
        return fe.Field() + " 邮箱格式不正确"
    case "min":
        return fe.Field() + " 最小长度 " + fe.Param()
    case "max":
        return fe.Field() + " 最大长度 " + fe.Param()
    default:
        return fe.Field() + " 校验失败: " + fe.Tag()
    }
}
```

使用：

```go
if err := c.ShouldBindJSON(&req); err != nil {
    c.JSON(400, gin.H{
        "code":   40001,
        "msg":    "参数错误",
        "errors": FormatValidationError(err),
    })
    return
}
```

### 使用官方翻译器（推荐）

```go
import (
    "github.com/go-playground/locales/zh"
    ut "github.com/go-playground/universal-translator"
    "github.com/go-playground/validator/v10"
    zhtrans "github.com/go-playground/validator/v10/translations/zh"
)

var trans ut.Translator

func InitTranslator() {
    zhLocale := zh.New()
    uni := ut.New(zhLocale, zhLocale)
    trans, _ = uni.GetTranslator("zh")

    v := binding.Validator.Engine().(*validator.Validate)
    zhtrans.RegisterDefaultTranslations(v, trans)
}

// 翻译错误
if err := c.ShouldBindJSON(&req); err != nil {
    var ve validator.ValidationErrors
    if errors.As(err, &ve) {
        errs := make(map[string]string)
        for _, fe := range ve {
            errs[fe.Field()] = fe.Translate(trans)
        }
        c.JSON(400, gin.H{"errors": errs})
        return
    }
}
```

---

## 五、自定义校验规则

比如注册"手机号"校验：

```go
import "github.com/go-playground/validator/v10"

// 定义规则
func mobileValidator(fl validator.FieldLevel) bool {
    mobile := fl.Field().String()
    return regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(mobile)
}

// 注册
func init() {
    if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
        v.RegisterValidation("mobile", mobileValidator)
    }
}

// 使用
type Req struct {
    Phone string `binding:"required,mobile"`
}
```

---

## 六、多字段联合校验

单字段 tag 不够时，用 `StructLevel` 校验：

```go
type OrderReq struct {
    PayType string  `json:"pay_type" binding:"required,oneof=alipay wechat cod"`
    Amount  float64 `json:"amount"   binding:"required,gt=0"`
    CODFee  float64 `json:"cod_fee"`
}

// 校验：如果 pay_type=cod，cod_fee 必须 > 0
func orderStructValidator(sl validator.StructLevel) {
    o := sl.Current().Interface().(OrderReq)
    if o.PayType == "cod" && o.CODFee <= 0 {
        sl.ReportError(o.CODFee, "CODFee", "CODFee", "required_when_cod", "")
    }
}

// 注册
v := binding.Validator.Engine().(*validator.Validate)
v.RegisterStructValidation(orderStructValidator, OrderReq{})
```

---

## 七、嵌套 struct 校验

```go
type Address struct {
    City   string `binding:"required"`
    Street string `binding:"required"`
}

type UserReq struct {
    Name    string    `binding:"required"`
    Address Address   `binding:"required"`   // 嵌套 struct 会自动递归校验
    Tags    []string  `binding:"required,min=1,dive,min=1,max=20"`  // 集合 dive
}
```

---

## 八、常见陷阱

### 陷阱 1：required 对 bool 无效

```go
type Req struct {
    Enabled bool `json:"enabled" binding:"required"`  // ⚠️ false 也算"零值"
}

// 请求 {"enabled": false} 会报 required 失败
// 解决方案：用 *bool 指针
type Req struct {
    Enabled *bool `json:"enabled" binding:"required"`
}
```

同理适用于数字 `0`、字符串 `""` 也想区分"传了空值" vs "没传"，都用指针。

### 陷阱 2：多次 Bind 报错

```go
c.ShouldBindJSON(&req1)
c.ShouldBindJSON(&req2)  // ❌ Body 只能读一次
```

如果需要多次绑定，用 `ShouldBindBodyWith`：

```go
c.ShouldBindBodyWith(&req1, binding.JSON)
c.ShouldBindBodyWith(&req2, binding.JSON)  // ✅ 内部会缓存 body
```

### 陷阱 3：omitempty 和 required 一起用

```go
type Req struct {
    Age int `json:"age,omitempty" binding:"required"`
}

// omitempty 只影响 JSON 序列化（Marshal 时零值不输出）
// 不影响 binding 校验
```

### 陷阱 4：dive 位置

```go
// ❌ 错
type Req struct {
    Tags []string `binding:"dive,required"`
}

// ✅ 正确：dive 前的规则给切片，dive 后给元素
type Req struct {
    Tags []string `binding:"required,min=1,dive,required,min=2"`
}
```

---

## 九、面试高频题

### Q1: Bind 和 ShouldBind 有什么区别？

`Bind` 失败自动写 400 + Abort，`ShouldBind` 只返回 error 让调用方处理。生产推荐 `ShouldBind`（可以自定义错误码和响应格式）。

### Q2: 校验 tag 里 `required` 对 false 值有效吗？

对 bool `false`、int `0`、string `""` 都算"零值"，会 required 失败。要区分"传了零值" vs "没传"用**指针**。

### Q3: 怎么自定义中文错误提示？

三种方式：
1. 自己 switch tag 翻译（简单）
2. 用官方 `validator/v10/translations/zh` 翻译器（推荐）
3. 前端根据错误 code 自己翻译（多语言场景）

### Q4: 一个字段可以同时绑定 JSON 和 form 吗？

可以。加多个 tag：`json:"name" form:"name"`。用 `c.ShouldBind` 会根据 Content-Type 自动选择。

### Q5: dive 是什么？

用于校验 slice/map 的**元素**。`dive` 前的规则作用于集合本身，`dive` 后的规则作用于每个元素。
