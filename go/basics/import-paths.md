# Go Import 路径详解

**标签**: #go #basics #import #modules #gopath

---

## 一、import 路径是什么

import 路径是一个**字符串**，告诉编译器去哪里找这个包。

```go
import "fmt"                          // 标准库
import "net/http"                     // 标准库（子目录）
import "github.com/gin-gonic/gin"     // 第三方包
import "myapp/internal/service"       // 项目内部包
```

路径规则：**import 路径 = 模块路径 + 包在模块内的相对路径**

---

## 二、路径的组成

### 标准库路径

标准库没有模块前缀，直接是包名或目录结构：

```
fmt
os
net/http          ← net 目录下的 http 包
encoding/json     ← encoding 目录下的 json 包
database/sql
crypto/tls
```

标准库源码位于 Go 安装目录：
```
$GOROOT/src/
├── fmt/
├── net/
│   └── http/     ← import "net/http"
├── encoding/
│   └── json/     ← import "encoding/json"
└── ...
```

### 第三方包路径

格式通常是：`托管平台/用户名/仓库名[/子目录]`

```
github.com/gin-gonic/gin           ← GitHub 上的 gin 框架
github.com/go-redis/redis/v9       ← v9 是子目录（大版本）
golang.org/x/crypto/bcrypt         ← golang 官方扩展库
go.uber.org/zap                    ← uber 的日志库
gorm.io/gorm                       ← gorm ORM
```

### 项目内部包路径

格式：`模块名（go.mod 中的 module）+ 包的相对路径`

```go
// go.mod
module github.com/jake/myapp

// 项目结构
myapp/
├── go.mod
├── internal/
│   ├── service/
│   │   └── user.go     // package service
│   └── repository/
│       └── user.go     // package repository
└── pkg/
    └── logger/
        └── logger.go   // package logger

// import 写法
import "github.com/jake/myapp/internal/service"
import "github.com/jake/myapp/internal/repository"
import "github.com/jake/myapp/pkg/logger"
```

---

## 三、GOPATH 时代 vs Go Modules 时代

### GOPATH 时代（Go 1.11 之前）

所有代码必须放在 `$GOPATH/src/` 下，路径即目录结构：

```
$GOPATH/
└── src/
    ├── github.com/
    │   └── gin-gonic/
    │       └── gin/         ← import "github.com/gin-gonic/gin"
    └── myapp/
        └── service/         ← import "myapp/service"
```

**问题**：
- 所有项目共用一个 `$GOPATH`，依赖版本无法隔离
- 不同项目依赖同一个库的不同版本 → 冲突

### Go Modules 时代（Go 1.11+，1.16 默认开启）

每个项目有独立的 `go.mod`，代码可以放在任意目录：

```
任意目录/
└── myapp/
    ├── go.mod       ← module github.com/jake/myapp
    ├── go.sum
    └── internal/
        └── service/
```

依赖缓存统一放在：
```
$GOPATH/pkg/mod/
├── github.com/
│   └── gin-gonic/
│       ├── gin@v1.9.0/
│       └── gin@v1.9.1/    ← 不同版本并存，互不干扰
└── go.uber.org/
    └── zap@v1.26.0/
```

**查看当前 GOPATH：**
```bash
go env GOPATH    # 默认 ~/go（Linux/Mac）或 %USERPROFILE%\go（Windows）
go env GOROOT    # Go 安装目录（标准库在这里）
go env GOMODCACHE  # 模块缓存目录（= $GOPATH/pkg/mod）
```

---

## 四、go.mod 与模块路径

`go.mod` 的第一行 `module` 定义了这个项目的**模块路径**，是项目内所有包 import 路径的前缀。

```
module github.com/jake/myapp   ← 模块路径
```

```
项目结构:                          对应 import 路径:
myapp/
├── go.mod
├── main.go                        （package main，不需要 import）
├── config/
│   └── config.go                  "github.com/jake/myapp/config"
├── internal/
│   ├── service/
│   │   └── user.go                "github.com/jake/myapp/internal/service"
│   └── repo/
│       └── user.go                "github.com/jake/myapp/internal/repo"
└── pkg/
    └── util/
        └── helper.go              "github.com/jake/myapp/pkg/util"
```

**模块路径不必是真实的 URL**，只是一个唯一标识符。私有项目可以用：

```
module myapp          // 简单名字（小团队内部项目）
module company.com/backend/myapp  // 公司内部域名
```

---

## 五、包名 vs 路径名

import 路径的最后一段通常就是包名，但不强制一致：

```go
// 路径最后一段是 "gin"，包名也是 "gin" → 直接用 gin.xxx
import "github.com/gin-gonic/gin"
gin.Default()

// 路径最后一段是 "v9"，但包名是 "redis" → 用 redis.xxx
import "github.com/go-redis/redis/v9"
redis.NewClient(...)

// 路径最后一段是 "http"，包名也是 "http"
import "net/http"
http.ListenAndServe(...)
```

当路径名和包名不一致时，用**别名**明确指定：

```go
import redisv9 "github.com/go-redis/redis/v9"
redisv9.NewClient(...)

// 或直接用包名（编译器会推断）
import "github.com/go-redis/redis/v9"
redis.NewClient(...)   // 用的是包声明的名字 "redis"，不是路径末段 "v9"
```

---

## 六、大版本路径（v2+）

Go Modules 规定：**v2 及以上的大版本，import 路径必须加 `/v2` 后缀**。

```go
// v1（路径无后缀）
import "github.com/go-redis/redis"        // v1.x

// v2+（路径加版本号）
import "github.com/go-redis/redis/v8"     // v8.x
import "github.com/go-redis/redis/v9"     // v9.x

// go.mod 里也要对应
require github.com/go-redis/redis/v9 v9.3.0
```

原因：允许同一个项目同时依赖一个库的 v1 和 v2（破坏性变更），两个路径完全独立。

---

## 七、replace 与本地路径

开发时替换依赖为本地目录（常用于调试或 monorepo）：

```
// go.mod
module github.com/jake/myapp

require github.com/jake/mylib v1.0.0

replace github.com/jake/mylib => ../mylib   // ← 指向本地相对路径
```

```
目录结构：
workspace/
├── myapp/          ← 当前项目
│   └── go.mod
└── mylib/          ← 本地依赖（../mylib）
    └── go.mod
```

也可以替换为绝对路径：
```
replace github.com/jake/mylib => /home/jake/projects/mylib
```

---

## 八、import 路径查找顺序

当你写 `import "some/pkg"` 时，编译器按以下顺序查找：

```
1. 标准库（$GOROOT/src/some/pkg）
        ↓ 没找到
2. go.mod 中声明的依赖（$GOPATH/pkg/mod/...）
        ↓ 没找到
3. replace 指令替换后的路径
        ↓ 没找到
4. 编译错误：cannot find package
```

---

## 九、常见路径问题

### 问题一：import 路径写错大小写

```go
// ❌ 路径大小写必须和实际目录一致（Linux 区分大小写）
import "github.com/Jake/myapp/Service"  // 实际目录是 service

// ✅
import "github.com/Jake/myapp/service"
```

### 问题二：包名和目录名不一致导致困惑

```go
// 目录名是 util，但文件里声明 package utils（多了个s）
import "myapp/util"     // import 路径用目录名
utils.Helper()          // 调用时用包名（package utils）

// ✅ 最佳实践：目录名和包名保持一致，避免混淆
```

### 问题三：用了 internal 包但不在允许范围内

```
github.com/jake/myapp/internal/service

允许导入的范围：github.com/jake/myapp/ 及其子目录
禁止导入的范围：其他模块（编译报错）
```

```go
// 外部项目试图导入
import "github.com/jake/myapp/internal/service"
// ❌ 编译错误：use of internal package github.com/jake/myapp/internal/service not allowed
```

### 问题四：go mod tidy 后路径消失

```bash
# 原因：代码里没有实际使用该包，tidy 认为是无用依赖，从 go.mod 删掉
# 解决：确保代码中有 import，或用空白导入保留副作用
import _ "github.com/go-sql-driver/mysql"
```

---

## 十、路径相关命令速查

```bash
# 查看当前模块信息
go env GOMODCACHE    # 缓存目录
go env GOPATH        # GOPATH
go env GOROOT        # 标准库位置

# 查看某个包的完整路径信息
go list -m all               # 列出所有依赖模块
go list ./...                # 列出当前模块所有包路径
go list -f '{{.Dir}}' fmt    # 查看标准库 fmt 的物理路径

# 查找某个包属于哪个模块
go list -m github.com/gin-gonic/gin

# 清理模块缓存
go clean -modcache
```

---

## 十一、面试高频题

### Q1: import 路径和包名的关系？

import 路径决定去哪里找代码（目录路径），包名是代码里 `package xxx` 声明的名字（调用时用）。两者通常一致，但不强制。路径末段是 `v9` 时，包名可能是 `redis`。

### Q2: GOPATH 和 Go Modules 的区别？

GOPATH 是旧机制，所有代码必须放在 `$GOPATH/src` 下，无版本隔离。Go Modules（1.16 默认）用 `go.mod` 管理依赖，代码放任意位置，不同版本并存于模块缓存中。

### Q3: 为什么 v2 的包路径要加 `/v2`？

Go Modules 的兼容性保证：同一路径意味着兼容。v2 有破坏性变更，加路径后缀让 v1 和 v2 成为完全不同的包，同一项目可以同时依赖两个版本。

### Q4: internal 包有什么限制？

`internal` 目录下的包只能被其父目录及子目录中的代码导入，外部模块无法 import，编译期报错。用于隐藏实现细节，防止外部依赖内部接口。

### Q5: replace 指令用来做什么？

在 `go.mod` 中将某个依赖替换为另一个版本或本地路径。常用于：本地调试依赖、使用 fork 版本修复 bug、monorepo 中引用兄弟模块。
