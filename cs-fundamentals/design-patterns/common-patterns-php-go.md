# 常用设计模式:PHP + Go 双实现

**标签**: #design-patterns #php #go #interview
**难度**: ⭐⭐⭐

本文精选**日常开发中真正常用的 10 个设计模式**,每个模式提供:
1. 一句话讲清楚是什么
2. 何时用 / 何时不用
3. PHP 完整实现 + 调用示例
4. Go 完整实现 + 调用示例
5. 常见对比或坑

---

## 目录

**创建型**
- [1. 单例(Singleton)](#1-单例singleton)
- [2. 工厂方法(Factory)](#2-工厂方法factory)
- [3. 建造者(Builder)](#3-建造者builder)
- [4. Functional Options(Go 特色)](#4-functional-optionsgo-特色)

**结构型**
- [5. 适配器(Adapter)](#5-适配器adapter)
- [6. 装饰器(Decorator)](#6-装饰器decorator)
- [7. 外观(Facade)](#7-外观facade)

**行为型**
- [8. 策略(Strategy)](#8-策略strategy)
- [9. 观察者(Observer)](#9-观察者observer)
- [10. 责任链(Chain of Responsibility)](#10-责任链chain-of-responsibility)

---

## 1. 单例(Singleton)

**一句话**:整个进程只有一个实例,全局共享。

**何时用**:配置管理、日志、数据库连接池、连接池管理器
**何时不用**:业务对象、有状态的服务(Hyperf 协程环境尤其危险,见前面 Hyperf 章节)

### PHP 实现

```php
<?php
class Config
{
    // 唯一实例
    private static ?self $instance = null;
    
    // 配置数据
    private array $data = [];
    
    // 私有构造器,禁止外部 new
    private function __construct()
    {
        $this->data = require __DIR__ . '/config.php';
    }
    
    // 禁止 clone
    private function __clone() {}
    
    // 禁止反序列化
    public function __wakeup()
    {
        throw new \Exception("Cannot unserialize singleton");
    }
    
    public static function getInstance(): self
    {
        if (self::$instance === null) {
            self::$instance = new self();
        }
        return self::$instance;
    }
    
    public function get(string $key): mixed
    {
        return $this->data[$key] ?? null;
    }
}

// ============ 调用 ============
$db1 = Config::getInstance()->get('database');
$db2 = Config::getInstance()->get('database');
// Config::getInstance() 每次返回同一个对象

// ❌ 无法这样做
// $c = new Config();          // Fatal error: private constructor
// $c = clone Config::getInstance();  // Fatal error: private clone
```

### Go 实现(sync.Once 是最佳实践)

```go
package config

import (
    "sync"
)

type Config struct {
    data map[string]any
}

var (
    instance *Config
    once     sync.Once   // ★ 保证只执行一次,并发安全
)

// GetInstance 全局单例入口
func GetInstance() *Config {
    once.Do(func() {
        instance = &Config{
            data: loadConfig(),
        }
    })
    return instance
}

func (c *Config) Get(key string) any {
    return c.data[key]
}

func loadConfig() map[string]any {
    return map[string]any{
        "database": "mysql://...",
    }
}
```

**调用**:
```go
db1 := config.GetInstance().Get("database")
db2 := config.GetInstance().Get("database")
// GetInstance() 无论并发调多少次,只 new 一次
```

**面试追问**:为什么用 `sync.Once` 而不是 `if instance == nil`?
- 直接判空**不是并发安全**的:两个 goroutine 同时检查 nil 都通过,会创建两个实例
- 加锁 `sync.Mutex` 也行,但性能差(每次都要加锁)
- **`sync.Once` 内部用原子操作 + 锁**,首次调用加锁保证只执行一次,后续调用直接原子读跳过 —— 最快最安全

---

## 2. 工厂方法(Factory)

**一句话**:把"根据参数创建不同类型对象"的逻辑封装在一个函数里,调用方不用关心具体类型。

**何时用**:根据配置/参数创建不同实现(支付渠道、缓存后端、日志驱动)
**何时不用**:只有一种实现时(直接 new 就行)

### PHP 实现

```php
<?php
// 支付接口
interface Payment
{
    public function charge(int $amount): bool;
}

// 具体实现
class AlipayPayment implements Payment
{
    public function charge(int $amount): bool
    {
        echo "支付宝支付 {$amount} 分\n";
        return true;
    }
}

class WechatPayment implements Payment
{
    public function charge(int $amount): bool
    {
        echo "微信支付 {$amount} 分\n";
        return true;
    }
}

class StripePayment implements Payment
{
    public function charge(int $amount): bool
    {
        echo "Stripe 支付 {$amount} 分\n";
        return true;
    }
}

// 工厂
class PaymentFactory
{
    public static function create(string $channel): Payment
    {
        return match ($channel) {
            'alipay' => new AlipayPayment(),
            'wechat' => new WechatPayment(),
            'stripe' => new StripePayment(),
            default  => throw new \InvalidArgumentException("Unknown: $channel"),
        };
    }
}

// ============ 调用 ============
$payment = PaymentFactory::create('alipay');
$payment->charge(10000);

// 从配置里读渠道,调用方不用关心具体类
$channel = config('payment.default');   // 'wechat'
PaymentFactory::create($channel)->charge(5000);
```

### Go 实现

```go
package payment

import "fmt"

// Payment 接口
type Payment interface {
    Charge(amount int) bool
}

// 三种具体实现
type Alipay struct{}
func (Alipay) Charge(amount int) bool {
    fmt.Printf("支付宝支付 %d 分\n", amount)
    return true
}

type Wechat struct{}
func (Wechat) Charge(amount int) bool {
    fmt.Printf("微信支付 %d 分\n", amount)
    return true
}

type Stripe struct{}
func (Stripe) Charge(amount int) bool {
    fmt.Printf("Stripe 支付 %d 分\n", amount)
    return true
}

// 工厂函数
func New(channel string) (Payment, error) {
    switch channel {
    case "alipay":
        return Alipay{}, nil
    case "wechat":
        return Wechat{}, nil
    case "stripe":
        return Stripe{}, nil
    default:
        return nil, fmt.Errorf("unknown channel: %s", channel)
    }
}
```

**调用**:
```go
p, err := payment.New("alipay")
if err != nil {
    log.Fatal(err)
}
p.Charge(10000)
```

**进阶:注册型工厂**(避免 switch 越来越长):

```go
var registry = map[string]func() Payment{}

func Register(name string, factory func() Payment) {
    registry[name] = factory
}

func New(channel string) (Payment, error) {
    fn, ok := registry[channel]
    if !ok {
        return nil, fmt.Errorf("unknown: %s", channel)
    }
    return fn(), nil
}

// 每个实现自己注册
func init() {
    Register("alipay", func() Payment { return Alipay{} })
    Register("wechat", func() Payment { return Wechat{} })
}
```

新增支付渠道时**不用改工厂代码**,新建文件调 `Register` 即可。这就是"开闭原则"。

---

## 3. 建造者(Builder)

**一句话**:把复杂对象的构造过程一步一步链式调用,最后 `Build()` 拿结果。

**何时用**:构造对象需要 5+ 个参数,且很多参数是可选的(SQL 查询构建、HTTP 请求构建)
**何时不用**:简单对象(用构造器或 Options 模式)

### PHP 实现

```php
<?php
// 目标产品:SQL 查询
class Query
{
    public string $table = '';
    public array  $wheres = [];
    public string $orderBy = '';
    public int    $limit = 0;
    public int    $offset = 0;
    
    public function toSQL(): string
    {
        $sql = "SELECT * FROM {$this->table}";
        if ($this->wheres) {
            $sql .= " WHERE " . implode(' AND ', $this->wheres);
        }
        if ($this->orderBy) {
            $sql .= " ORDER BY {$this->orderBy}";
        }
        if ($this->limit) {
            $sql .= " LIMIT {$this->limit}";
        }
        if ($this->offset) {
            $sql .= " OFFSET {$this->offset}";
        }
        return $sql;
    }
}

// Builder
class QueryBuilder
{
    private Query $query;
    
    public function __construct()
    {
        $this->query = new Query();
    }
    
    public function table(string $t): self
    {
        $this->query->table = $t;
        return $this;      // ★ 返回自己,支持链式调用
    }
    
    public function where(string $cond): self
    {
        $this->query->wheres[] = $cond;
        return $this;
    }
    
    public function orderBy(string $col): self
    {
        $this->query->orderBy = $col;
        return $this;
    }
    
    public function limit(int $n): self
    {
        $this->query->limit = $n;
        return $this;
    }
    
    public function offset(int $n): self
    {
        $this->query->offset = $n;
        return $this;
    }
    
    public function build(): Query
    {
        return $this->query;
    }
}

// ============ 调用 ============
$q = (new QueryBuilder())
    ->table('users')
    ->where("age > 18")
    ->where("status = 'active'")
    ->orderBy('created_at DESC')
    ->limit(10)
    ->offset(20)
    ->build();

echo $q->toSQL();
// SELECT * FROM users WHERE age > 18 AND status = 'active' ORDER BY created_at DESC LIMIT 10 OFFSET 20
```

### Go 实现

```go
package query

import (
    "fmt"
    "strings"
)

type Query struct {
    table   string
    wheres  []string
    orderBy string
    limit   int
    offset  int
}

// Builder
type Builder struct {
    q *Query
}

func New() *Builder {
    return &Builder{q: &Query{}}
}

func (b *Builder) Table(t string) *Builder {
    b.q.table = t
    return b
}

func (b *Builder) Where(cond string) *Builder {
    b.q.wheres = append(b.q.wheres, cond)
    return b
}

func (b *Builder) OrderBy(col string) *Builder {
    b.q.orderBy = col
    return b
}

func (b *Builder) Limit(n int) *Builder {
    b.q.limit = n
    return b
}

func (b *Builder) Offset(n int) *Builder {
    b.q.offset = n
    return b
}

func (b *Builder) Build() string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("SELECT * FROM %s", b.q.table))
    if len(b.q.wheres) > 0 {
        sb.WriteString(" WHERE " + strings.Join(b.q.wheres, " AND "))
    }
    if b.q.orderBy != "" {
        sb.WriteString(" ORDER BY " + b.q.orderBy)
    }
    if b.q.limit > 0 {
        sb.WriteString(fmt.Sprintf(" LIMIT %d", b.q.limit))
    }
    if b.q.offset > 0 {
        sb.WriteString(fmt.Sprintf(" OFFSET %d", b.q.offset))
    }
    return sb.String()
}
```

**调用**:
```go
sql := query.New().
    Table("users").
    Where("age > 18").
    Where("status = 'active'").
    OrderBy("created_at DESC").
    Limit(10).
    Offset(20).
    Build()

fmt.Println(sql)
```

**关键**:每个 Setter 都 `return self`,才能链式调用。这就是**流式接口(Fluent Interface)**。

---

## 4. Functional Options(Go 特色)

**一句话**:用函数当参数,构造对象时可以任选想改的字段。**Go 世界里 Builder 的替代方案**,更符合 Go 风格。

详细讲解见前面 [PHP 面试题答案](../../php/interview-answers.md) 或直接看:

### Go 实现

```go
package server

import "time"

type Server struct {
    addr    string
    timeout time.Duration
    logger  Logger
    tls     bool
}

// Option 是"改造 Server 的函数"
type Option func(*Server)

func New(addr string, opts ...Option) *Server {
    s := &Server{
        addr:    addr,
        timeout: 30 * time.Second,   // 默认值
        logger:  defaultLogger,
    }
    for _, opt := range opts {
        opt(s)   // 应用每个选项
    }
    return s
}

func WithTimeout(d time.Duration) Option {
    return func(s *Server) { s.timeout = d }
}

func WithLogger(l Logger) Option {
    return func(s *Server) { s.logger = l }
}

func WithTLS() Option {
    return func(s *Server) { s.tls = true }
}
```

**调用**:
```go
// 全默认
s1 := server.New(":8080")

// 部分覆盖
s2 := server.New(":8080",
    server.WithTimeout(60*time.Second),
)

// 全指定
s3 := server.New(":8080",
    server.WithTimeout(60*time.Second),
    server.WithLogger(myLogger),
    server.WithTLS(),
)
```

**PHP 里对应的写法**(命名参数,PHP 8.0+):
```php
class Server {
    public function __construct(
        public string $addr,
        public int    $timeout = 30,
        public ?Logger $logger = null,
        public bool   $tls = false,
    ) {}
}

// 调用时可跳过任何参数(命名传参)
$s = new Server(
    addr: ':8080',
    tls: true,          // 跳过 timeout 和 logger,直接指定 tls
);
```

PHP 8+ 用命名参数就够了,不需要 Options 模式。Go 因为没命名参数,才发明了 Options。

---

## 5. 适配器(Adapter)

**一句话**:把一个类的接口转换成客户端期望的另一个接口,让不兼容的东西能一起工作。

**何时用**:接入第三方 SDK(接口和你系统不匹配)、替换旧模块、协议转换
**关键**:适配器不改动被适配对象的代码

### 场景

你的系统只认一个统一的 `PaymentGateway` 接口,但第三方 SDK 是各种奇形怪状的方法。

### PHP 实现

```php
<?php
// 你的系统期望的接口
interface PaymentGateway
{
    public function pay(int $amountInCents, string $currency): string;
}

// 第三方 SDK(你没法改)
class StripeSDK
{
    public function makePayment(float $amountInDollars, array $config): array
    {
        return ['id' => 'stripe_' . uniqid(), 'status' => 'success'];
    }
}

class PayPalSDK
{
    public function submitTransaction(string $currency, int $cents): object
    {
        return (object)['transactionId' => 'pp_' . uniqid()];
    }
}

// 适配器 1:把 Stripe 包装成 PaymentGateway
class StripeAdapter implements PaymentGateway
{
    public function __construct(private StripeSDK $sdk) {}
    
    public function pay(int $amountInCents, string $currency): string
    {
        // 把参数从"分"转成"美元"
        $dollars = $amountInCents / 100;
        $result = $this->sdk->makePayment($dollars, ['currency' => $currency]);
        return $result['id'];
    }
}

// 适配器 2:把 PayPal 包装成 PaymentGateway
class PayPalAdapter implements PaymentGateway
{
    public function __construct(private PayPalSDK $sdk) {}
    
    public function pay(int $amountInCents, string $currency): string
    {
        $result = $this->sdk->submitTransaction($currency, $amountInCents);
        return $result->transactionId;
    }
}

// ============ 调用 ============
// 客户端代码永远只依赖 PaymentGateway,不关心底层用啥
function processOrder(PaymentGateway $gw, int $amount): void
{
    $txnId = $gw->pay($amount, 'USD');
    echo "支付成功: $txnId\n";
}

// 换支付渠道只需要换适配器
processOrder(new StripeAdapter(new StripeSDK()), 10000);
processOrder(new PayPalAdapter(new PayPalSDK()), 10000);
```

### Go 实现

```go
package payment

import "fmt"

// 系统期望的统一接口
type Gateway interface {
    Pay(amountCents int, currency string) (string, error)
}

// 第三方 SDK 1(参数是美元、多参数)
type StripeSDK struct{}
func (StripeSDK) MakePayment(dollars float64, cfg map[string]string) (string, error) {
    return "stripe_" + fmt.Sprintf("%d", 12345), nil
}

// 第三方 SDK 2(参数顺序不同)
type PayPalSDK struct{}
func (PayPalSDK) SubmitTransaction(currency string, cents int) (string, error) {
    return "pp_67890", nil
}

// 适配器 1
type StripeAdapter struct {
    sdk StripeSDK
}
func (a StripeAdapter) Pay(cents int, currency string) (string, error) {
    return a.sdk.MakePayment(float64(cents)/100, map[string]string{"currency": currency})
}

// 适配器 2
type PayPalAdapter struct {
    sdk PayPalSDK
}
func (a PayPalAdapter) Pay(cents int, currency string) (string, error) {
    return a.sdk.SubmitTransaction(currency, cents)
}
```

**调用**:
```go
func processOrder(gw payment.Gateway, amount int) {
    txnID, err := gw.Pay(amount, "USD")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("支付成功:", txnID)
}

processOrder(payment.StripeAdapter{}, 10000)
processOrder(payment.PayPalAdapter{}, 10000)
```

**记忆**:适配器 = **转接头**。你的手机是 Type-C,充电器是 Lightning,适配器把两者接起来。

---

## 6. 装饰器(Decorator)

**一句话**:不改动原对象,在外面套一层"壳",给它加新能力。可以套多层。

**何时用**:给已有函数/对象加日志、缓存、重试、监控、鉴权,而**不动原代码**
**记忆**:HTTP 中间件本质就是装饰器

### PHP 实现

```php
<?php
// 基础接口
interface UserService
{
    public function getUser(int $id): array;
}

// 原始实现
class UserServiceImpl implements UserService
{
    public function getUser(int $id): array
    {
        // 假设是查数据库
        return ['id' => $id, 'name' => 'jake'];
    }
}

// 装饰器 1:加缓存
class CacheDecorator implements UserService
{
    private array $cache = [];
    
    public function __construct(private UserService $inner) {}
    
    public function getUser(int $id): array
    {
        if (isset($this->cache[$id])) {
            echo "[cache hit] $id\n";
            return $this->cache[$id];
        }
        $result = $this->inner->getUser($id);   // ★ 委托给内部服务
        $this->cache[$id] = $result;
        return $result;
    }
}

// 装饰器 2:加日志
class LogDecorator implements UserService
{
    public function __construct(private UserService $inner) {}
    
    public function getUser(int $id): array
    {
        echo "[log] getUser($id) start\n";
        $start = microtime(true);
        $result = $this->inner->getUser($id);
        $elapsed = (microtime(true) - $start) * 1000;
        echo "[log] getUser($id) done in {$elapsed}ms\n";
        return $result;
    }
}

// ============ 调用:套两层壳 ============
$svc = new LogDecorator(
    new CacheDecorator(
        new UserServiceImpl()
    )
);

$svc->getUser(1);
// [log] getUser(1) start
// [log] getUser(1) done in 0.5ms

$svc->getUser(1);   // 第二次
// [log] getUser(1) start
// [cache hit] 1
// [log] getUser(1) done in 0.01ms
```

### Go 实现

```go
package service

import (
    "fmt"
    "time"
)

type UserService interface {
    GetUser(id int) (map[string]any, error)
}

// 原始实现
type userServiceImpl struct{}

func (userServiceImpl) GetUser(id int) (map[string]any, error) {
    return map[string]any{"id": id, "name": "jake"}, nil
}

// 装饰器 1:缓存
type cacheDecorator struct {
    inner UserService
    cache map[int]map[string]any
}

func WithCache(s UserService) UserService {
    return &cacheDecorator{inner: s, cache: make(map[int]map[string]any)}
}

func (c *cacheDecorator) GetUser(id int) (map[string]any, error) {
    if v, ok := c.cache[id]; ok {
        fmt.Println("[cache hit]", id)
        return v, nil
    }
    v, err := c.inner.GetUser(id)
    if err == nil {
        c.cache[id] = v
    }
    return v, err
}

// 装饰器 2:日志
type logDecorator struct {
    inner UserService
}

func WithLog(s UserService) UserService {
    return &logDecorator{inner: s}
}

func (l *logDecorator) GetUser(id int) (map[string]any, error) {
    fmt.Printf("[log] GetUser(%d) start\n", id)
    start := time.Now()
    v, err := l.inner.GetUser(id)
    fmt.Printf("[log] GetUser(%d) done in %v\n", id, time.Since(start))
    return v, err
}
```

**调用**:
```go
// 从内到外套壳
svc := WithLog(WithCache(userServiceImpl{}))

svc.GetUser(1)
svc.GetUser(1)   // 第二次命中缓存
```

**HTTP 中间件的本质**:
```go
func LoggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        log.Println("request:", r.URL)
        next.ServeHTTP(w, r)   // ★ 调用被装饰的 handler
    })
}

// 使用:多层装饰
handler := LoggingMiddleware(AuthMiddleware(BusinessHandler))
```

---

## 7. 外观(Facade)

**一句话**:提供一个统一的入口封装复杂子系统,让调用方用起来简单。

**何时用**:调用方不需要知道内部有哪些子系统,只关心"给我做完这件事"
**Laravel 的 Facade 是这个模式的变体**(但更多是"静态代理"用法)

### PHP 实现

```php
<?php
// 复杂的子系统(用户不该关心这些细节)
class OrderValidator
{
    public function validate(array $data): void { echo "校验\n"; }
}

class InventoryService
{
    public function lock(int $skuId, int $qty): void { echo "锁库存 $skuId x$qty\n"; }
    public function release(int $skuId, int $qty): void { echo "释放库存\n"; }
}

class PaymentService
{
    public function charge(int $userId, int $amount): bool
    {
        echo "扣款 $amount\n";
        return true;
    }
}

class NotificationService
{
    public function send(int $userId, string $msg): void
    {
        echo "通知用户 $userId: $msg\n";
    }
}

// Facade:统一入口
class OrderFacade
{
    public function __construct(
        private OrderValidator $validator,
        private InventoryService $inventory,
        private PaymentService $payment,
        private NotificationService $notifier,
    ) {}
    
    public function placeOrder(array $data): string
    {
        $this->validator->validate($data);
        $this->inventory->lock($data['sku'], $data['qty']);
        
        try {
            $this->payment->charge($data['user_id'], $data['amount']);
        } catch (\Exception $e) {
            $this->inventory->release($data['sku'], $data['qty']);
            throw $e;
        }
        
        $orderId = 'ORD_' . uniqid();
        $this->notifier->send($data['user_id'], "订单 $orderId 创建成功");
        return $orderId;
    }
}

// ============ 调用 ============
$facade = new OrderFacade(
    new OrderValidator(),
    new InventoryService(),
    new PaymentService(),
    new NotificationService(),
);

// 调用方只关心这一行,不用知道内部四个服务
$orderId = $facade->placeOrder([
    'user_id' => 123,
    'sku'     => 456,
    'qty'     => 1,
    'amount'  => 10000,
]);
```

### Go 实现

```go
package order

import "fmt"

// 各子系统
type Validator struct{}
func (Validator) Validate(data map[string]any) error {
    fmt.Println("校验")
    return nil
}

type Inventory struct{}
func (Inventory) Lock(sku, qty int) error {
    fmt.Printf("锁库存 %d x%d\n", sku, qty)
    return nil
}
func (Inventory) Release(sku, qty int) {
    fmt.Println("释放库存")
}

type Payment struct{}
func (Payment) Charge(userID, amount int) error {
    fmt.Printf("扣款 %d\n", amount)
    return nil
}

type Notifier struct{}
func (Notifier) Send(userID int, msg string) {
    fmt.Printf("通知 %d: %s\n", userID, msg)
}

// Facade
type Facade struct {
    validator Validator
    inventory Inventory
    payment   Payment
    notifier  Notifier
}

func NewFacade() *Facade {
    return &Facade{}
}

func (f *Facade) PlaceOrder(data map[string]any) (string, error) {
    if err := f.validator.Validate(data); err != nil {
        return "", err
    }
    if err := f.inventory.Lock(data["sku"].(int), data["qty"].(int)); err != nil {
        return "", err
    }
    if err := f.payment.Charge(data["user_id"].(int), data["amount"].(int)); err != nil {
        f.inventory.Release(data["sku"].(int), data["qty"].(int))
        return "", err
    }
    orderID := "ORD_12345"
    f.notifier.Send(data["user_id"].(int), "订单 "+orderID+" 创建成功")
    return orderID, nil
}
```

**调用**:
```go
facade := order.NewFacade()
orderID, _ := facade.PlaceOrder(map[string]any{
    "user_id": 123,
    "sku":     456,
    "qty":     1,
    "amount":  10000,
})
```

**Facade vs Adapter**:
- Facade:**简化**复杂子系统的调用(内部有多个组件协作)
- Adapter:**转换**接口(两个不兼容的接口能对接上)

---

## 8. 策略(Strategy)

**一句话**:把可替换的算法/行为封装成对象,运行时动态选择用哪个。

**何时用**:一件事有多种做法(不同的支付、不同的排序、不同的折扣计算)
**记忆**:if-else 或 switch 越来越长,就该上策略

### PHP 实现

```php
<?php
// 折扣策略接口
interface DiscountStrategy
{
    public function calc(int $amount): int;
}

// 具体策略
class NoDiscount implements DiscountStrategy
{
    public function calc(int $amount): int { return $amount; }
}

class VipDiscount implements DiscountStrategy
{
    public function calc(int $amount): int { return (int)($amount * 0.8); }
}

class FullReduction implements DiscountStrategy
{
    public function calc(int $amount): int
    {
        return $amount >= 10000 ? $amount - 2000 : $amount;
    }
}

// 使用策略的上下文
class OrderCalculator
{
    public function __construct(private DiscountStrategy $strategy) {}
    
    public function total(int $amount): int
    {
        return $this->strategy->calc($amount);
    }
    
    public function setStrategy(DiscountStrategy $s): void
    {
        $this->strategy = $s;   // 运行时切换
    }
}

// ============ 调用 ============
$calc = new OrderCalculator(new VipDiscount());
echo $calc->total(10000) . "\n";   // 8000

$calc->setStrategy(new FullReduction());
echo $calc->total(10000) . "\n";   // 8000
echo $calc->total(5000) . "\n";    // 5000(不满足满减条件)
```

### Go 实现

```go
package discount

// 策略函数(Go 里 func 类型就是天然的策略)
type Strategy func(amount int) int

var (
    NoDiscount Strategy = func(a int) int { return a }
    VIP        Strategy = func(a int) int { return int(float64(a) * 0.8) }
    FullReduce Strategy = func(a int) int {
        if a >= 10000 {
            return a - 2000
        }
        return a
    }
)

type Calculator struct {
    strategy Strategy
}

func NewCalculator(s Strategy) *Calculator {
    return &Calculator{strategy: s}
}

func (c *Calculator) Total(amount int) int {
    return c.strategy(amount)
}

func (c *Calculator) SetStrategy(s Strategy) {
    c.strategy = s
}
```

**调用**:
```go
calc := discount.NewCalculator(discount.VIP)
fmt.Println(calc.Total(10000))   // 8000

calc.SetStrategy(discount.FullReduce)
fmt.Println(calc.Total(10000))   // 8000
fmt.Println(calc.Total(5000))    // 5000
```

**Go 里策略常用 func 类型**,不必用 interface,更简洁。

---

## 9. 观察者(Observer)

**一句话**:一个对象状态变化时,自动通知所有"订阅"了它的对象。

**何时用**:事件系统、消息发布订阅、GUI 响应、Model 变化通知
**记忆**:Laravel 的 Event/Listener 就是标准观察者

### PHP 实现

```php
<?php
// 观察者接口
interface EventListener
{
    public function handle(string $event, array $data): void;
}

// 具体观察者
class EmailNotifier implements EventListener
{
    public function handle(string $event, array $data): void
    {
        if ($event === 'order.paid') {
            echo "[Email] 订单 {$data['order_id']} 支付成功邮件\n";
        }
    }
}

class SMSNotifier implements EventListener
{
    public function handle(string $event, array $data): void
    {
        if ($event === 'order.paid') {
            echo "[SMS] 订单 {$data['order_id']} 支付成功短信\n";
        }
    }
}

class InventoryUpdater implements EventListener
{
    public function handle(string $event, array $data): void
    {
        if ($event === 'order.paid') {
            echo "[Inventory] 扣减 SKU {$data['sku']}\n";
        }
    }
}

// Subject:被观察的对象
class EventBus
{
    /** @var EventListener[][] */
    private array $listeners = [];
    
    public function subscribe(string $event, EventListener $listener): void
    {
        $this->listeners[$event][] = $listener;
    }
    
    public function publish(string $event, array $data): void
    {
        foreach ($this->listeners[$event] ?? [] as $l) {
            $l->handle($event, $data);
        }
    }
}

// ============ 调用 ============
$bus = new EventBus();
$bus->subscribe('order.paid', new EmailNotifier());
$bus->subscribe('order.paid', new SMSNotifier());
$bus->subscribe('order.paid', new InventoryUpdater());

// 触发事件,所有订阅者都收到
$bus->publish('order.paid', [
    'order_id' => 12345,
    'sku'      => 456,
]);
// [Email] 订单 12345 支付成功邮件
// [SMS] 订单 12345 支付成功短信
// [Inventory] 扣减 SKU 456
```

### Go 实现

```go
package events

import "sync"

type Event struct {
    Name string
    Data map[string]any
}

type Handler func(Event)

type Bus struct {
    mu       sync.RWMutex
    handlers map[string][]Handler
}

func NewBus() *Bus {
    return &Bus{handlers: make(map[string][]Handler)}
}

func (b *Bus) Subscribe(name string, h Handler) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.handlers[name] = append(b.handlers[name], h)
}

func (b *Bus) Publish(name string, data map[string]any) {
    b.mu.RLock()
    handlers := b.handlers[name]
    b.mu.RUnlock()
    
    for _, h := range handlers {
        h(Event{Name: name, Data: data})
    }
}
```

**调用**:
```go
bus := events.NewBus()

bus.Subscribe("order.paid", func(e events.Event) {
    fmt.Printf("[Email] 订单 %v 邮件\n", e.Data["order_id"])
})

bus.Subscribe("order.paid", func(e events.Event) {
    fmt.Printf("[SMS] 订单 %v 短信\n", e.Data["order_id"])
})

bus.Publish("order.paid", map[string]any{
    "order_id": 12345,
})
```

**进阶**:异步观察者(handler 放 goroutine 里跑):
```go
func (b *Bus) PublishAsync(name string, data map[string]any) {
    b.mu.RLock()
    handlers := b.handlers[name]
    b.mu.RUnlock()
    
    for _, h := range handlers {
        go h(Event{Name: name, Data: data})   // ★ 异步
    }
}
```

---

## 10. 责任链(Chain of Responsibility)

**一句话**:多个处理器串成一条链,请求依次经过,每个处理器决定"处理并传下去"还是"截断"。

**何时用**:HTTP 中间件、审批流、日志过滤、拦截器
**记忆**:洋葱模型 —— 请求一层层进去,响应一层层出来

### PHP 实现(HTTP 中间件风格)

```php
<?php
// 处理器接口
interface Middleware
{
    public function handle(array $request, callable $next): array;
}

// 具体中间件
class AuthMiddleware implements Middleware
{
    public function handle(array $request, callable $next): array
    {
        if (empty($request['token'])) {
            return ['status' => 401, 'body' => 'Unauthorized'];   // 截断
        }
        echo "[Auth] passed\n";
        return $next($request);   // 传给下一个
    }
}

class LogMiddleware implements Middleware
{
    public function handle(array $request, callable $next): array
    {
        echo "[Log] before\n";
        $response = $next($request);
        echo "[Log] after, status=" . $response['status'] . "\n";
        return $response;
    }
}

class RateLimitMiddleware implements Middleware
{
    public function handle(array $request, callable $next): array
    {
        echo "[RateLimit] check\n";
        return $next($request);
    }
}

// 责任链构建
class Pipeline
{
    /** @var Middleware[] */
    private array $middlewares = [];
    
    public function pipe(Middleware $m): self
    {
        $this->middlewares[] = $m;
        return $this;
    }
    
    public function run(array $request, callable $finalHandler): array
    {
        // 从最后一个中间件往前包装
        $next = $finalHandler;
        foreach (array_reverse($this->middlewares) as $m) {
            $current = $next;
            $next = fn($req) => $m->handle($req, $current);
        }
        return $next($request);
    }
}

// ============ 调用 ============
$pipeline = (new Pipeline())
    ->pipe(new LogMiddleware())
    ->pipe(new AuthMiddleware())
    ->pipe(new RateLimitMiddleware());

$response = $pipeline->run(
    ['token' => 'abc123', 'path' => '/users'],
    fn($req) => ['status' => 200, 'body' => 'ok']   // 最终业务处理器
);
// [Log] before
// [Auth] passed
// [RateLimit] check
// [Log] after, status=200
```

### Go 实现

```go
package middleware

import "fmt"

type Request struct {
    Token string
    Path  string
}

type Response struct {
    Status int
    Body   string
}

// 处理函数类型
type Handler func(Request) Response

// 中间件类型:接收下一个 Handler,返回新的 Handler
type Middleware func(Handler) Handler

// 具体中间件
func Auth(next Handler) Handler {
    return func(req Request) Response {
        if req.Token == "" {
            return Response{Status: 401, Body: "Unauthorized"}
        }
        fmt.Println("[Auth] passed")
        return next(req)
    }
}

func Log(next Handler) Handler {
    return func(req Request) Response {
        fmt.Println("[Log] before")
        resp := next(req)
        fmt.Printf("[Log] after, status=%d\n", resp.Status)
        return resp
    }
}

func RateLimit(next Handler) Handler {
    return func(req Request) Response {
        fmt.Println("[RateLimit] check")
        return next(req)
    }
}

// 组装链
func Chain(final Handler, middlewares ...Middleware) Handler {
    // 从后往前包装:最后加的最先执行
    for i := len(middlewares) - 1; i >= 0; i-- {
        final = middlewares[i](final)
    }
    return final
}
```

**调用**:
```go
final := func(req middleware.Request) middleware.Response {
    return middleware.Response{Status: 200, Body: "ok"}
}

handler := middleware.Chain(final,
    middleware.Log,        // 第一个执行
    middleware.Auth,
    middleware.RateLimit,
)

resp := handler(middleware.Request{Token: "abc", Path: "/users"})
// [Log] before
// [Auth] passed
// [RateLimit] check
// [Log] after, status=200
fmt.Println(resp)
```

**这就是所有 HTTP 框架**(Gin / Echo / Laravel / Slim)中间件的核心机制。

---

## 附:模式对比 & 选型

### 装饰器 vs 代理 vs 适配器

| 模式 | 目的 | 接口关系 |
|---|---|---|
| **装饰器** | 加功能(不改接口) | 装饰后**和原对象接口一致** |
| **代理** | 控制访问 / 延迟加载 / 远程调用 | 和原对象接口一致 |
| **适配器** | 转换接口 | 装饰后是**新接口** |

### 策略 vs 状态

| | 策略 | 状态 |
|---|---|---|
| 触发者 | 客户端主动切换 | 对象自身根据条件切换 |
| 例子 | 用户选择排序方式 | 订单从"待付款"自动变"已取消" |

### Options vs Builder

| | Functional Options(Go) | Builder |
|---|---|---|
| 语言 | Go 首选 | Java / PHP 首选 |
| 语法 | `New(opts...)` | `Builder.a().b().Build()` |
| 顺序 | 无关 | 无关 |
| 灵活性 | 高(可做校验、联动) | 高 |
| 学习成本 | 中(闭包思想) | 低 |

## 十条选型速记

1. **要全局唯一** → 单例(Go 用 sync.Once)
2. **要根据参数造不同对象** → 工厂
3. **构造对象参数很多且可选** → Go 用 Options,PHP 用命名参数或 Builder
4. **要给已有对象加功能** → 装饰器
5. **要转换接口** → 适配器
6. **要简化复杂子系统的调用** → 外观
7. **一件事多种做法** → 策略
8. **状态变化要通知一堆人** → 观察者
9. **一堆处理器要串起来** → 责任链
10. **不确定用哪个** → 大概率你不需要设计模式,直接写代码

## 一句话总结

> **设计模式是解决"重复出现的问题"的套路,不是炫技工具**。日常业务代码 80% 用不到,但用到时能省大量沟通成本 —— 说"这里用了责任链",大家就懂。**先写清楚代码,发现真的重复出现了再重构成模式**。
