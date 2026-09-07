# GORM 深度剖析

**标签**: #go #ecosystem #orm #gorm #高频

GORM 是 Go 生态最流行的 ORM。用起来简单，但**性能坑、事务坑、Session 坑非常多**，几乎是 Go 面试必问。

---

## 一、核心概念：Session 机制

GORM 从 v2 开始引入 Session 机制。**每次链式调用会返回新的 `*gorm.DB` 实例**（默认），保证并发安全和链式调用的独立性。

### 为什么要 Session

看一段容易踩坑的代码：

```go
// ❌ v1 时代（或误用 v2）：条件累积
db := gorm.Open(...)
db.Where("age > ?", 18)      // 修改了 db 本身
db.Where("status = ?", "active")  // 又累加了条件
db.Find(&users)               // WHERE age > 18 AND status = 'active'
db.Find(&admins)              // 还是这些条件！污染了
```

GORM v2 通过 Session 解决：**每次操作都是独立 Statement**。

```go
// ✅ GORM v2
db := gorm.Open(...)
db.Where("age > ?", 18).Find(&users)      // WHERE age > 18
db.Where("status = ?", "active").Find(&admins)  // WHERE status = 'active'
// 互不影响
```

### 源码原理

```go
// gorm.go 简化
type DB struct {
    Statement *Statement  // 每次链式调用的独立状态
    // ...
}

func (db *DB) Where(query string, args ...any) *DB {
    tx := db.getInstance()  // ← 拿到新的 DB 实例（浅拷贝 + 新 Statement）
    tx.Statement.AddClause(clause.Where{Exprs: ...})
    return tx
}

// getInstance 返回新实例，不污染父 DB
func (db *DB) getInstance() *DB {
    if db.clone > 0 {
        tx := &DB{Config: db.Config, Error: db.Error, RowsAffected: db.RowsAffected}
        tx.Statement = &Statement{...}  // 新的 Statement
        return tx
    }
    return db
}
```

**关键**：`db.clone > 0` 决定是不是共享，默认为 1，每次链式调用生成新实例。

### 显式 Session：`db.Session(&gorm.Session{})`

有时候需要**跨方法传递已配置好的 DB**（比如加了 debug、preload），用 Session：

```go
// 创建一个带 Debug 的独立会话
tx := db.Session(&gorm.Session{Logger: newLogger})
tx.Where("id = ?", 1).Find(&user)
tx.Where("age > 18").Find(&users)  // 都带 Debug
```

---

## 二、GORM 最经典的性能坑：N+1

### 什么是 N+1

```go
var users []User
db.Find(&users)  // 1 次 SQL：SELECT * FROM users

for _, u := range users {
    var orders []Order
    db.Where("user_id = ?", u.ID).Find(&orders)  // ⚠️ N 次 SQL
}
// 总共 1 + N 次 SQL，性能爆炸
```

**100 个用户 → 101 次 SQL**。

### 修复方式一：Preload（推荐）

```go
type User struct {
    ID     int
    Name   string
    Orders []Order  // has many
}

var users []User
db.Preload("Orders").Find(&users)
// 只有 2 次 SQL：
// 1. SELECT * FROM users
// 2. SELECT * FROM orders WHERE user_id IN (1, 2, 3, ...)
```

### 修复方式二：Joins

```go
db.Joins("Orders").Find(&users)
// 1 次 JOIN 查询
// SELECT users.*, Orders.* FROM users LEFT JOIN orders ...
```

**Preload vs Joins**：

| | Preload | Joins |
|---|---|---|
| SQL 数 | 2（父表 + 子表 IN） | 1（JOIN） |
| 返回大小 | 紧凑 | 会有笛卡尔积（1:N 时子表数据被复制） |
| 复杂条件 | 支持 `Preload("Orders", "status = ?", "paid")` | 需要 JOIN 子查询 |
| 常用场景 | 一对多、多对多 | 一对一、内连接过滤 |

**规则**：一对多、多对多用 Preload，一对一或需要 WHERE 过滤用 Joins。

### 嵌套 Preload

```go
db.Preload("Orders.Items.Product").Find(&users)
// 3 层嵌套：user → orders → items → product
```

### 条件 Preload

```go
db.Preload("Orders", "status = ?", "paid").
   Preload("Orders.Items", func(db *gorm.DB) *gorm.DB {
       return db.Order("created_at DESC").Limit(10)
   }).Find(&users)
```

---

## 三、事务：常见错误和最佳实践

### 陷阱：把 `db` 传下去，事务无效

```go
// ❌ 事务没生效
func Transfer(db *gorm.DB, from, to int, amount int) error {
    return db.Transaction(func(tx *gorm.DB) error {
        // 这里必须用 tx，不是外层 db！
        return svc.updateBalance(db, from, -amount)  // ❌ 用了 db，脱离事务
    })
}

// ✅ 事务对象一路传下去
func Transfer(db *gorm.DB, from, to int, amount int) error {
    return db.Transaction(func(tx *gorm.DB) error {
        return svc.updateBalance(tx, from, -amount)  // ✅
    })
}
```

**理解**：`tx` 是事务专属的 `*gorm.DB`，回滚/提交都基于它。传原 `db` 会走**新连接**，事务隔离。

### 事务嵌套：SavePoint

```go
db.Transaction(func(tx *gorm.DB) error {
    tx.Create(&user)  // 主事务

    err := tx.Transaction(func(tx2 *gorm.DB) error {
        return tx2.Create(&order).Error  // 嵌套：内部用 SAVEPOINT
    })
    if err != nil {
        // 内部回滚到 SAVEPOINT，外层可继续
        return nil  // 或者 return err 一起回滚
    }
    return nil
})
```

### 手动事务

```go
tx := db.Begin()
defer func() {
    if r := recover(); r != nil {
        tx.Rollback()
    }
}()

if err := tx.Create(&user).Error; err != nil {
    tx.Rollback()
    return err
}

if err := tx.Create(&order).Error; err != nil {
    tx.Rollback()
    return err
}

return tx.Commit().Error
```

**用 `Transaction()` 更好**，自动 rollback + panic 恢复。

---

## 四、Hook 陷阱

GORM 支持 `BeforeCreate` / `AfterFind` / `BeforeUpdate` 等生命周期 Hook：

```go
func (u *User) BeforeCreate(tx *gorm.DB) error {
    u.Password = hashPassword(u.Password)
    return nil
}

func (u *User) AfterFind(tx *gorm.DB) error {
    u.FullName = u.FirstName + " " + u.LastName
    return nil
}
```

### 陷阱 1：AfterFind 里查 DB 触发 N+1

```go
// ❌ 每个 User 都会触发一次查询
func (u *User) AfterFind(tx *gorm.DB) error {
    tx.Where("user_id = ?", u.ID).Find(&u.Orders)  // N+1！
    return nil
}
```

### 陷阱 2：Hook 里操作事务用错 `tx`

```go
func (u *User) BeforeCreate(tx *gorm.DB) error {
    tx.Create(&Log{UserID: u.ID})  // ✅ tx 是当前事务，用它就对
    return nil
}
```

### 陷阱 3：批量插入 Hook 触发多次

`db.Create(&[]User{...})` 会给每个元素都调 `BeforeCreate`，性能爆炸。用 `db.CreateInBatches` 时 Hook 依然会调，如果只想跳过：

```go
db.Session(&gorm.Session{SkipHooks: true}).Create(&users)
```

---

## 五、查询性能：Select 字段 vs 全部字段

```go
// ❌ SELECT *，即使只要 ID 和 Name
var users []User
db.Find(&users)

// ✅ 只查需要的字段
db.Select("id, name").Find(&users)
```

**性能影响**：
- 网络传输量：全字段可能 1KB/行，只要 2 字段可能 20 字节/行
- ORM 反射赋值成本：字段越多越慢
- 内存占用：结构体全部零值分配

### 更优：用专门的 DTO

```go
type UserBrief struct {
    ID   int
    Name string
}

var briefs []UserBrief
db.Model(&User{}).Select("id, name").Find(&briefs)
// 只反射 2 个字段，性能最好
```

---

## 六、批量插入优化

### 单条插入 vs 批量插入

```go
// ❌ 每次一条 SQL
for _, u := range users {
    db.Create(&u)  // 10000 次 SQL
}

// ✅ 一次批量插入
db.CreateInBatches(users, 100)
// 每 100 条一个 INSERT，10000 条只有 100 个 SQL
```

**为什么不一次全塞进去**：
- MySQL `max_allowed_packet` 限制
- 事务 undo log 太大
- 阻塞时间过长

**Batch size 建议**：50-500，视记录大小调整。

### `Clauses(clause.OnConflict{...})`：Upsert

```go
db.Clauses(clause.OnConflict{
    Columns:   []clause.Column{{Name: "email"}},
    DoUpdates: clause.AssignmentColumns([]string{"name", "updated_at"}),
}).Create(&user)

// 生成 SQL：
// INSERT INTO users (...) VALUES (...)
// ON DUPLICATE KEY UPDATE name = VALUES(name), updated_at = VALUES(updated_at)
```

---

## 七、连接池配置

GORM 底层用 `database/sql`，连接池是必配的：

```go
db, _ := gorm.Open(mysql.Open(dsn), &gorm.Config{})
sqlDB, _ := db.DB()

sqlDB.SetMaxOpenConns(100)         // 最大连接数
sqlDB.SetMaxIdleConns(20)          // 空闲连接数
sqlDB.SetConnMaxLifetime(time.Hour) // 连接最长生命周期
sqlDB.SetConnMaxIdleTime(10*time.Minute) // 空闲连接最长存活
```

**参数选择**：

| 参数 | 太小的问题 | 太大的问题 |
|-----|----------|----------|
| `MaxOpenConns` | 高峰期请求排队 | DB 连接爆掉 |
| `MaxIdleConns` | 频繁建连开销大 | 空闲连接占资源 |
| `ConnMaxLifetime` | 太长可能连到已关闭的连接 | 太短频繁重连 |

**经验值**：`MaxOpenConns` = DB 允许的连接数 / 应用实例数 × 80%。

---

## 八、日志和慢查询

```go
import "gorm.io/gorm/logger"

newLogger := logger.New(
    log.New(os.Stdout, "\r\n", log.LstdFlags),
    logger.Config{
        SlowThreshold:             200 * time.Millisecond,  // 慢查询阈值
        LogLevel:                  logger.Warn,
        IgnoreRecordNotFoundError: true,
        Colorful:                  false,
    },
)

db, _ := gorm.Open(mysql.Open(dsn), &gorm.Config{
    Logger: newLogger,
})
```

生产建议：
- **`SlowThreshold` 设 200ms**，慢 SQL 会自动 Warn 级别记录
- **`IgnoreRecordNotFoundError: true`**：`gorm.ErrRecordNotFound` 不作为错误日志（否则日志刷屏）
- 生产 `LogLevel` 用 `Warn` 或 `Error`

### 接入 Zap

```go
type GormLogger struct {
    zap *zap.Logger
}

func (l *GormLogger) LogMode(level logger.LogLevel) logger.Interface { return l }
func (l *GormLogger) Info(ctx context.Context, msg string, data ...any) {
    l.zap.Sugar().Infof(msg, data...)
}
// ... Warn / Error / Trace 类似
```

---

## 九、常见生产陷阱

### 陷阱 1：Model 里嵌 `gorm.Model` 引发的 `deleted_at` 问题

```go
type User struct {
    gorm.Model  // 自带 ID, CreatedAt, UpdatedAt, DeletedAt
    Name string
}

// ❌ 你以为的：DELETE FROM users WHERE id = 1
db.Delete(&user)
// ✅ 实际执行：UPDATE users SET deleted_at = NOW() WHERE id = 1
// GORM 自动软删除
```

**注意**：
- 后续查询自动带 `WHERE deleted_at IS NULL`
- 想真正删掉：`db.Unscoped().Delete(&user)`
- 想查已删除的：`db.Unscoped().Where(...)`

### 陷阱 2：`.First / .Take / .Last` 返回 `ErrRecordNotFound`

```go
var user User
err := db.First(&user, 1).Error
if err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, nil  // 空结果，不当错误
    }
    return nil, err
}
```

**注意**：`.Find(&user)` 不返回 `ErrRecordNotFound`，只 `.First/.Take/.Last` 返回。

### 陷阱 3：`Updates` 只更新非零值字段

```go
// ❌ 想把 status 改成 0（默认值），但被忽略
db.Model(&user).Updates(User{Status: 0, Name: "Jake"})
// 只更新 name，status 被忽略（因为 int 零值）

// ✅ 用 map 显式指定
db.Model(&user).Updates(map[string]any{"status": 0, "name": "Jake"})

// ✅ 或用 Select 强制
db.Model(&user).Select("Status", "Name").Updates(User{Status: 0, Name: "Jake"})
```

### 陷阱 4：`Where` 用 struct 也只匹配非零字段

```go
// ❌ 想查 status=0 的用户，结果查了全表
db.Where(&User{Status: 0}).Find(&users)

// ✅ 用 map
db.Where(map[string]any{"status": 0}).Find(&users)
// 或直接写 SQL
db.Where("status = ?", 0).Find(&users)
```

### 陷阱 5：`Preload` + `Limit` 会限制父表数量，不是子表

```go
// 想拿 10 个 user，每个 user 只显示 3 个订单
db.Preload("Orders").Limit(10).Find(&users)
// ❌ Limit 只限了 user 数量，Orders 全部拿回来

// ✅ Preload 里加限制
db.Preload("Orders", func(db *gorm.DB) *gorm.DB {
    return db.Order("created_at DESC").Limit(3)  // ⚠️ 但这是"总共 3 个"，不是"每个 user 3 个"
}).Limit(10).Find(&users)

// 每个 user 3 个订单 —— 需要用窗口函数或 raw SQL
```

### 陷阱 6：连接池耗尽 —— 忘了关 `*sql.Rows`

```go
// ❌ raw SQL 忘了 Close
rows, _ := db.Raw("SELECT ...").Rows()
for rows.Next() {
    // ...
}
// 忘了 rows.Close()，连接不归还

// ✅
rows, _ := db.Raw("SELECT ...").Rows()
defer rows.Close()  // 保证归还
```

### 陷阱 7：`AutoMigrate` 生产环境别用

```go
// ❌ 生产环境自动改表结构：可能删列、卡表锁
db.AutoMigrate(&User{})

// ✅ 用专门的迁移工具：golang-migrate、goose、atlasgo
```

---

## 十、面试高频题

### Q1: GORM 的 Session 机制是什么？

每次链式调用返回一个新的 `*gorm.DB` 实例，独立的 `Statement`，保证：
1. **并发安全**：不同 goroutine 用同一个 `db` 不会互相污染条件
2. **链式独立**：`db.Where(...).Find(...)` 后再 `db.Find(...)` 是全新查询
3. **可复用**：`db.Session(&gorm.Session{})` 可以显式创建带配置的会话

### Q2: 什么是 N+1？怎么解决？

主查询返回 N 条记录，每条都触发一次子查询，共 1+N 次 SQL。

解决：
- **Preload**：一对多、多对多，2 次 SQL（父表 + 子表 IN）
- **Joins**：一对一或过滤条件时用，1 次 JOIN

### Q3: Preload 和 Joins 的区别？

Preload 走两次 SQL 用 IN 关联，返回结构紧凑；Joins 用 JOIN 一次搞定，但一对多时会有笛卡尔积膨胀。**多值关系用 Preload，单值/过滤用 Joins**。

### Q4: GORM 事务传播怎么处理？

必须把 `tx` 一路传下去，不能用外层的 `db`。GORM 不像 Spring 有 `@Transactional` 自动传播，必须手动传。

### Q5: `Updates` 为什么零值字段不更新？

GORM 默认认为零值可能是"未设置"，避免误伤。想强制更新用 `map` 或 `.Select("field")`。

### Q6: 软删除的坑？

`gorm.Model` 自带 `DeletedAt`，`Delete` 会变成 `UPDATE deleted_at = NOW()`。所有查询自动加 `WHERE deleted_at IS NULL`。
- 硬删除：`db.Unscoped().Delete()`
- 查所有：`db.Unscoped().Find()`
- 唯一索引失效：MySQL 唯一索引会认为 `deleted_at IS NULL` 的多条记录冲突，需要用组合唯一索引 `(email, deleted_at)`

### Q7: 连接池怎么配？

```go
sqlDB.SetMaxOpenConns(100)           // 视 DB 承载力
sqlDB.SetMaxIdleConns(20)            // 一般 = MaxOpen/5
sqlDB.SetConnMaxLifetime(time.Hour)  // 短于 MySQL wait_timeout
```

**公式**：`MaxOpenConns` = 目标 QPS × 平均 RT / 单实例数。

### Q8: 大表查询怎么优化？

- `Select` 指定字段，避免 `SELECT *`
- 用 DTO 而不是 Model（少反射字段）
- 走索引，避免大 offset：分页用 `WHERE id > last_id LIMIT n` 而不是 `LIMIT offset, n`
- 大量数据用 `.Rows()` 流式读取，别一次 `.Find(&all)` 到内存

### Q9: GORM vs sqlx vs sqlc 怎么选？

| | GORM | sqlx | sqlc |
|---|---|---|---|
| 学习成本 | 中 | 低 | 低 |
| 灵活性 | 高（链式） | 高（手写 SQL） | 中（模板生成） |
| 性能 | 中（反射多） | 高 | 最高 |
| 类型安全 | 中 | 弱 | **强**（编译期） |
| 适合 | 快速开发 | 手写 SQL 项目 | 大型 / 追求性能 |

**大厂内部实践**：越来越多转向 sqlc（类型安全 + 无反射 + SQL 完全可控）。
