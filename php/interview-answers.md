# PHP 高频面试题完整解答

**标签**: #php #interview #高频 #深度
**配套索引**: [questions.md](./questions.md)

> 每道题按"标准答案 → 深入追问 → 代码示例"结构组织，覆盖 PHP 7/8 差异。

---

## 一、基础（6 题）

### Q1: PHP 数组的底层实现是什么？为什么既能当数组又能当字典？

PHP 数组底层是 **HashTable（哈希表）+ 有序双向链表**。

**核心结构**（PHP 7+，zend_array）：

```
zend_array
├── nTableSize        -- 哈希表大小（2 的幂）
├── nNumUsed          -- 已使用 Bucket 槽位数
├── nNumOfElements    -- 实际有效元素数
├── nNextFreeElement  -- 下一个自动分配的整数 key
├── arData            -- ★ Bucket 数组（连续内存）
└── arHash            -- 哈希映射表（索引 → arData 位置）
```

**Bucket 结构**：
```
Bucket
├── val   -- zval（值）
├── h     -- 哈希值（整数 key 直接用，字符串 key 算 DJBX33A）
├── key   -- 字符串 key 指针（整数 key 为 NULL）
```

**为什么既能当数组又能当字典**：

- **当数组用**（`[1, 2, 3]`）：key 是自动递增的整数（0, 1, 2），哈希值就是 key 本身，arData 按插入顺序连续存储，遍历就是顺序扫描 arData
- **当字典用**（`['name' => 'jake']`）：key 是字符串，算哈希 → 查 arHash → 定位到 arData 的具体 Bucket

**关键改进（PHP 7 vs PHP 5）**：

PHP 5 的 HashTable 每个 Bucket 是独立堆分配的，用双向链表串起来。PHP 7 改为**连续内存的 Bucket 数组**：
- 内存局部性大幅提升（CPU 缓存友好）
- 减少了大量小内存分配
- 内存占用降低约 40%

**面试追问**：

**Q：PHP 数组有序吗？**
有序。arData 按插入顺序排列，`foreach` 遍历的是 arData，所以顺序和插入顺序一致。这是 PHP 数组和大多数语言 HashMap 的核心区别。

**Q：扩容机制？**
当 `nNumUsed >= nTableSize` 时触发。如果有效元素数（nNumOfElements）远小于 nNumUsed（说明很多被 unset 了），先**紧凑**（compact），把有效元素移到前面。否则 `nTableSize *= 2` 扩容，所有哈希重新计算。

**Q：`unset($arr[5])` 后内存怎么变？**
不会立即缩小。只是把 Bucket 标记为 `IS_UNDEF`（空洞），等 compact 时才回收。所以频繁 unset 会产生空洞。

---

### Q2: `==` 和 `===` 的区别？`0 == "a"` 在 PHP 7 和 8 中的行为？

- **`==`**（松散比较）：会做**类型转换**后再比较
- **`===`**（严格比较）：先比较类型，类型不同直接 false，类型相同再比值

**PHP 7 的 `0 == "a"`**：
```php
var_dump(0 == "a");   // PHP 7: true ❌
// 原因："a" 被转换为 int → intval("a") = 0 → 0 == 0 → true
```

**PHP 8 的 `0 == "a"`**：
```php
var_dump(0 == "a");   // PHP 8: false ✅
// PHP 8.0 改变了规则：当 int 和非数字字符串比较时，把 int 转成 string 再比
// "0" != "a" → false
```

这是 **PHP 8.0 最重大的 Breaking Change 之一**（RFC: Saner string to number comparisons）。

**经典陷阱表**：

| 表达式 | PHP 7 | PHP 8 | 说明 |
|---|---|---|---|
| `0 == "a"` | true | **false** | 改了 |
| `0 == ""` | true | **false** | 改了 |
| `0 == "0"` | true | true | "0" 是数字字符串 |
| `1 == "1"` | true | true | 正常 |
| `"1" == "01"` | true | true | 都能转数字 |
| `null == false` | true | true | 没改 |
| `null == 0` | true | true | 没改 |
| `null == ""` | true | true | 没改 |

**面试标准答**：永远用 `===`，避免一切类型转换意外。PHP 8 修复了最离谱的那些，但松散比较仍然有坑。

---

### Q3: 值传递与引用传递，`&` 引用与 C 指针的差别？

**值传递**：函数内修改不影响外部。PHP 默认是值传递。

```php
function addOne($x) { $x++; }
$a = 1;
addOne($a);
echo $a;  // 1（没变）
```

**引用传递**：函数内修改直接影响外部。

```php
function addOne(&$x) { $x++; }
$a = 1;
addOne($a);
echo $a;  // 2（变了）
```

**`&` 引用 vs C 指针的区别**：

| 维度 | PHP `&` 引用 | C 指针 |
|---|---|---|
| 本质 | **别名**（alias），两个变量名指向同一个 zval | **内存地址**，存储变量的地址 |
| 能做算术吗 | 不能 | 能（`p++` 移动指针） |
| 有 NULL 值吗 | 没有"空引用"概念 | 有空指针 `NULL` |
| 能多级间接吗 | 不能（没有"引用的引用"） | 能（`int **pp`） |
| 取消引用 | `unset($ref)` 只断开别名 | `free(ptr)` 释放内存 |

```php
$a = 1;
$b = &$a;    // $a 和 $b 是同一个 zval 的两个别名
$b = 100;    // $a 也变成 100
unset($b);   // 只是断开 $b 这个名字，$a 依然是 100
```

**COW 与引用的关系**：
```php
$a = 'hello';
$b = $a;       // COW：$a 和 $b 共享同一份内存
$b .= ' world'; // 写时复制：此时 $b 才分配新内存

$a = 'hello';
$b = &$a;      // 引用：$a 和 $b 绑定，COW 被禁用
$b .= ' world'; // $a 也变了，因为是同一个 zval
```

---

### Q4: 引用计数与 COW 机制

**引用计数（Reference Counting）**：PHP 的 zval 有一个 `refcount` 字段，记录有多少变量指向这个值。

```php
$a = 'hello';       // zval{type=string, val="hello", refcount=1}
$b = $a;            // refcount=2（$a 和 $b 共享）
unset($a);          // refcount=1（只剩 $b）
unset($b);          // refcount=0 → 释放内存
```

**COW（Copy-On-Write 写时复制）**：多个变量共享一份数据，直到有人要**修改**时才真正复制。

```php
$a = str_repeat('x', 1000000);  // 1MB 字符串
$b = $a;                         // 没有复制！只是 refcount++
// 此时内存只有一份 1MB
$b .= 'y';                       // 写时触发复制，$b 指向新的 1MB+1 内存
// 此时内存有两份
```

**COW 的意义**：
- 函数传参不会真的复制大数组 → 如果函数内不修改，开销为 0
- `$b = $a` 赋值不复制 → 只在修改时付出代价

**引用计数的局限**：循环引用。

```php
$a = [];
$a[0] = &$a;   // $a 引用自己 → refcount 永远 > 0 → 内存泄漏
```

**解决**：PHP 5.3+ 引入**垃圾回收器（GC）**，用**"标记-清除"**算法处理循环引用。GC 只在引用计数无法释放的对象上触发。

```php
gc_collect_cycles();  // 手动触发 GC
gc_status();          // 查看 GC 状态
```

---

### Q5: `isset` `empty` `is_null` `array_key_exists` 的差异

| 函数 | null | '' | 0 | false | 不存在的变量/key |
|---|---|---|---|---|---|
| `isset()` | false | true | true | true | **false** |
| `empty()` | true | true | true | true | **true** |
| `is_null()` | true | false | false | false | ⚠ Warning |
| `array_key_exists()` | true | true | true | true | **false** |

**关键区别**：

- `isset($arr['key'])`：key 存在**且值不为 null** 返回 true
- `array_key_exists('key', $arr)`：key 存在就返回 true，**不管值是不是 null**
- `empty()`：等价于 `!isset() || falsy`

**面试经典坑**：
```php
$arr = ['name' => null];

isset($arr['name']);          // false（值是 null）
array_key_exists('name', $arr); // true（key 存在）
empty($arr['name']);          // true（null 是 empty）
```

**PHP 7+ 用 `??`**：
```php
$name = $arr['name'] ?? 'default';  // 等价于 isset($arr['name']) ? $arr['name'] : 'default'
```

---

### Q6: `include` `require` `include_once` `require_once` 差异与性能

| | 文件不存在时 | 重复加载 | 性能 |
|---|---|---|---|
| `include` | **Warning**，继续执行 | 会重复执行 | 最快 |
| `require` | **Fatal Error**，停止 | 会重复执行 | 最快 |
| `include_once` | Warning | **跳过** | 略慢 |
| `require_once` | Fatal Error | **跳过** | 略慢 |

**`_once` 为什么慢一点**：需要维护一个"已加载文件表"，每次检查文件路径是否已在表中。涉及**路径解析（realpath）**。

**现代 PHP 几乎不用这四个**：用 `spl_autoload_register` 或 Composer 的 PSR-4 自动加载。

```php
// 古代写法
require_once 'src/User.php';

// 现代写法（Composer 自动加载）
use App\Models\User;
```

**OPCache 加持下两者性能差异可忽略**。面试答这题重点在于"Fatal vs Warning"和"现代用 Composer 不需要手动引入"。

---

## 二、OOP（4 题）

### Q7: Trait 与继承、接口的差异，冲突如何解决？

**三者定位**：
- **继承**：`is-a` 关系，单继承
- **接口**：`has-ability` 契约，只定义方法签名
- **Trait**：**代码复用机制**，解决单继承的限制

**Trait 的本质**：编译时**把 Trait 的代码复制粘贴到使用类**中。不是继承关系，没有多态。

```php
trait Loggable {
    public function log(string $msg): void {
        echo "[LOG] $msg\n";
    }
}

trait Cacheable {
    public function log(string $msg): void {
        echo "[CACHE] $msg\n";
    }
}

class UserService {
    use Loggable, Cacheable {
        Loggable::log as logInfo;      // 别名
        Cacheable::log insteadof Loggable;  // Cacheable 的 log 优先
    }
}
```

**冲突解决**：
- `insteadof`：选择用谁的方法
- `as`：给被覆盖的方法起别名

**优先级**（从高到低）：
```
当前类方法 > Trait 方法 > 父类方法
```

**面试追问**：
- **Q：Trait 里能写属性吗？** 能，但如果使用类已有同名属性，必须兼容（类型、默认值一致），否则 Fatal Error。
- **Q：Trait 能写抽象方法吗？** 能。使用类必须实现。
- **Q：Trait 能写构造函数吗？** 能，但极不推荐（多 Trait 构造函数冲突难处理）。

---

### Q8: 后期静态绑定 `static::` 与 `self::` 的区别

```php
class ParentClass {
    public static function create(): static {  // PHP 8: return type static
        return new static();    // ★ 后期静态绑定
    }
    
    public function who(): string {
        return static::class;   // ★ 运行时确定
    }
    
    public function whoSelf(): string {
        return self::class;     // ★ 编译时确定
    }
}

class ChildClass extends ParentClass {}

$child = ChildClass::create();     // 返回 ChildClass 实例（不是 ParentClass）
echo $child->who();                // "ChildClass"
echo $child->whoSelf();            // "ParentClass" ← 注意这里
```

**核心区别**：
- `self::`：**定义时绑定**，永远指向写这段代码的类
- `static::`：**运行时绑定**，指向实际调用的类
- `parent::`：指向父类

**典型应用场景**：
```php
// ORM 的 find 方法
class Model {
    public static function find(int $id): static {
        $class = static::class;          // 子类调用时拿到子类名
        return (new $class)->query($id); // 返回子类实例
    }
}

class User extends Model {}
$user = User::find(1);  // 返回 User 实例，不是 Model
```

Laravel 的 Eloquent 大量使用 `static::`，这也是为什么 `User::find()` 能返回 `User` 而不是 `Model`。

---

### Q9: 魔术方法执行顺序与性能代价

**常用魔术方法**：

| 方法 | 触发时机 |
|---|---|
| `__construct` | `new` 实例化时 |
| `__destruct` | 对象销毁时 |
| `__get($name)` | 访问不可达属性时 |
| `__set($name, $val)` | 写入不可达属性时 |
| `__isset($name)` | `isset()` 或 `empty()` 不可达属性时 |
| `__unset($name)` | `unset()` 不可达属性时 |
| `__call($name, $args)` | 调用不可达方法时 |
| `__callStatic($name, $args)` | 调用不可达静态方法时 |
| `__toString` | 对象转字符串时 |
| `__invoke` | 对象当函数调用时 |
| `__clone` | `clone` 对象时 |
| `__serialize` / `__unserialize` | PHP 7.4+ 替代 `Serializable` 接口 |
| `__debugInfo` | `var_dump` 时自定义输出 |

**执行顺序示例**：
```php
$obj = new Foo();        // __construct
$obj->bar;               // __get (如果 bar 不存在或 private)
$obj->bar = 1;           // __set
isset($obj->bar);        // __isset
$obj();                  // __invoke
echo $obj;               // __toString
$clone = clone $obj;     // __clone
unset($obj);             // __destruct
```

**性能代价**：

`__get` / `__set` / `__call` 是**显著慢于直接属性/方法访问**的：
- 直接属性访问：编译时解析到固定偏移量
- 魔术方法：运行时查找 → 调用魔术方法 → 方法内再做分发

```php
// 慢：Laravel 的 Model 属性通过 __get 做了大量逻辑
$user->name;
// 实际调用链：__get → getAttribute → getAttributeValue → cast → accessor

// 快：直接属性
class User {
    public string $name;
}
$user->name;  // 直接内存偏移
```

**面试建议**：不要滥用魔术方法做"花式编程"。ORM 用它是合理的（灵活性），但业务代码优先用明确的属性和方法。

---

### Q10: 抽象类 vs 接口，选择时机

| 维度 | 抽象类 | 接口 |
|---|---|---|
| 关键字 | `abstract class` | `interface` |
| 可以有实现吗 | ✅ 可以有具体方法 | PHP 8 前不行，PHP 8 有默认方法 |
| 可以有属性吗 | ✅ | ❌（只能有常量） |
| 继承数量 | 单继承 | 多实现 |
| 构造函数 | ✅ | ❌ |
| 访问修饰符 | 都支持 | 只能 public |
| 语义 | `is-a`（是什么） | `can-do`（能做什么） |

**选择原则**：
- **有共享代码** → 抽象类
- **只定义契约** → 接口
- **需要多个"能力"** → 接口（一个类可以 implements 多个）
- **有状态（属性）** → 抽象类

```php
// 接口：定义能力
interface Cacheable {
    public function getCacheKey(): string;
    public function getCacheTTL(): int;
}

// 抽象类：共享代码
abstract class BaseRepository {
    protected PDO $db;
    
    public function __construct(PDO $db) {
        $this->db = $db;
    }
    
    abstract public function findById(int $id): ?array;
    
    // 具体方法，子类直接用
    protected function query(string $sql, array $params = []): array {
        $stmt = $this->db->prepare($sql);
        $stmt->execute($params);
        return $stmt->fetchAll();
    }
}
```

---

## 三、高级（4 题）

### Q11: 生成器（Generator）与迭代器的关系

**生成器是迭代器的简化实现**。用 `yield` 关键字的函数返回一个 `Generator` 对象，`Generator` 实现了 `Iterator` 接口。

**核心价值**：**不需要把所有数据一次性放入内存**。

```php
// 传统方式：100 万行全加载到内存
function getAllUsers(): array {
    $users = [];
    $result = $db->query("SELECT * FROM users");
    while ($row = $result->fetch()) {
        $users[] = $row;  // 内存持续增长
    }
    return $users;
}

// 生成器方式：每次只有一行在内存
function getAllUsers(): Generator {
    $result = $db->query("SELECT * FROM users");
    while ($row = $result->fetch()) {
        yield $row;       // 暂停，返回一行，等下次 next() 再继续
    }
}

foreach (getAllUsers() as $user) {
    process($user);  // 内存恒定
}
```

**`yield` 的工作机制**：
1. 函数执行到 `yield $value` → **暂停**，把 $value 返回给调用者
2. 调用者处理完 → 再调 `$gen->next()` 或 `foreach` 下一轮
3. 函数从 `yield` **恢复**继续执行

**`yield from` 委托**：
```php
function innerGenerator(): Generator {
    yield 1;
    yield 2;
}
function outerGenerator(): Generator {
    yield from innerGenerator();
    yield 3;
}
// 结果：1, 2, 3
```

**`yield` 双向通信**：
```php
function accumulator(): Generator {
    $total = 0;
    while (true) {
        $value = yield $total;   // 接收外部 send 的值，返回当前累加
        $total += $value;
    }
}

$gen = accumulator();
$gen->current();          // 0
$gen->send(10);           // 10 (total = 10)
$gen->send(20);           // 30 (total = 30)
```

**面试追问**：
- **Q：生成器能 rewind 吗？** 不能。只能遍历一次。
- **Q：生成器能 count 吗？** 不能。数据量未知。
- **Q：什么时候不该用？** 数据量小的时候，或需要随机访问的时候。

---

### Q12: Fiber 与协程的区别

**Fiber**（PHP 8.1+）是 PHP 原生的**用户态纤程**。

```php
$fiber = new Fiber(function (): void {
    $value = Fiber::suspend('hello');  // 暂停，返回 'hello'
    echo "Resumed with: $value\n";
});

$result = $fiber->start();    // 启动，得到 'hello'
echo "Got: $result\n";
$fiber->resume('world');      // 恢复，传入 'world'
```

输出：
```
Got: hello
Resumed with: world
```

**Fiber vs Swoole 协程**：

| 维度 | Fiber | Swoole 协程 |
|---|---|---|
| 层级 | PHP 核心 | C 扩展 |
| 调度 | **手动**（开发者显式 suspend/resume） | **自动**（遇 IO 自动切换） |
| IO 优化 | 无（需要框架封装） | ✅ hook 了所有 IO |
| 并发模型 | 仅提供挂起/恢复原语 | 完整的协程运行时 |
| 使用方式 | 几乎不直接用，给框架做底层 | 直接写业务代码 |

**关键认知**：Fiber 本身不是协程，是比协程更底层的**原语**。Swoole 协程、AMPHP、ReactPHP Fiber 等框架用它来实现异步。

**举例**：
```php
// Swoole：自动调度，开发者不需要手动切换
go(function () {
    $result = file_get_contents('http://api.example.com');  // IO 时自动让出
    echo $result;
});
go(function () {
    sleep(1);  // 也自动让出
    echo "done";
});

// Fiber：需要框架帮你调度
// AMPHP 的 async/await 就是基于 Fiber 实现的
```

---

### Q13: 反射的应用场景与性能开销

**反射**：运行时检查类、方法、属性、参数等元信息。

```php
$ref = new ReflectionClass(UserController::class);

// 获取所有 public 方法
$methods = $ref->getMethods(ReflectionMethod::IS_PUBLIC);

// 检查构造函数参数（依赖注入用）
$constructor = $ref->getConstructor();
foreach ($constructor->getParameters() as $param) {
    $type = $param->getType()->getName();  // 参数类型
    // → 自动从容器解析依赖
}

// 调用私有方法（测试用）
$method = $ref->getMethod('privateMethod');
$method->setAccessible(true);
$method->invoke($instance);
```

**应用场景**：
1. **依赖注入容器**（Laravel Service Container）：分析构造函数参数类型 → 自动解析
2. **路由分发**（Controller 方法参数自动绑定）
3. **ORM**：属性映射到数据库字段
4. **单元测试**：访问/修改私有属性和方法
5. **注解/属性扫描**（PHP 8 Attribute）

**性能开销**：

反射比直接调用**慢 5-10 倍**。核心成本在于运行时元数据查询。

```php
// 慢
$ref = new ReflectionMethod($obj, 'doWork');
$ref->invoke($obj);

// 快 50-100 倍
$obj->doWork();
```

**生产优化**：
- **缓存反射结果**：启动时解析一次，缓存 Reflection 对象
- **编译时生成**：Laravel 的 `php artisan optimize`、Symfony 的编译容器
- Hyperf 启动时扫描注解并生成代理类，运行时不再反射

---

### Q14: PHP 8 的属性（Attribute）与注解的区别

PHP 8 前没有原生注解，社区用**DocBlock 注释**模拟：
```php
/**
 * @Route("/users", methods={"GET"})   ← 只是字符串，需要解析器
 * @Middleware("auth")
 */
public function index() {}
```

PHP 8 引入了**原生 Attribute**：
```php
#[Route('/users', methods: ['GET'])]
#[Middleware('auth')]
public function index() {}
```

**核心区别**：

| 维度 | DocBlock 注解 | PHP 8 Attribute |
|---|---|---|
| 语法 | 注释里写，本质是字符串 | 原生语法 `#[...]` |
| 类型安全 | 无（纯字符串解析） | ✅ 有类型检查 |
| IDE 支持 | 有限（依赖插件） | 原生支持（跳转、补全） |
| 性能 | 需要运行时解析字符串 | 编译时解析 |
| 嵌套 | 不行 | 支持 |
| 验证 | 无 | 有 target 限制 |

**定义 Attribute**：
```php
#[Attribute(Attribute::TARGET_METHOD | Attribute::IS_REPEATABLE)]
class Route {
    public function __construct(
        public string $path,
        public array $methods = ['GET'],
    ) {}
}

// 使用
#[Route('/users', methods: ['GET'])]
#[Route('/users', methods: ['POST'])]
public function users() {}

// 读取（通过反射）
$ref = new ReflectionMethod(Controller::class, 'users');
$attrs = $ref->getAttributes(Route::class);
foreach ($attrs as $attr) {
    $route = $attr->newInstance();  // 得到 Route 对象
    echo $route->path;             // "/users"
}
```

---

## 四、性能（4 题）

### Q15: OPCache 的工作流程

PHP 脚本的常规执行：
```
.php 文件 → 词法分析 → 语法分析 → AST → 编译为 OPCode → 执行 OPCode
```

**没有 OPCache**：每次请求都重复"词法→语法→编译"这些步骤。

**有 OPCache**：编译一次后把 OPCode 缓存在**共享内存**中，下次直接执行。

```
第一次：.php → 词法 → 语法 → AST → OPCode → 缓存到共享内存 → 执行
第二次：.php → 共享内存命中 → 直接执行
```

**关键配置**：
```ini
opcache.enable=1
opcache.memory_consumption=256        ; 缓存大小 MB
opcache.max_accelerated_files=20000   ; 最多缓存文件数
opcache.validate_timestamps=0         ; ★ 生产设 0，不检查文件修改时间
opcache.revalidate_freq=60            ; 开发环境检查间隔
opcache.preload=/path/preload.php     ; PHP 7.4+ 预加载
```

**`validate_timestamps=0`**：生产必须设。设 0 后 PHP 不检查文件是否更新，部署后需要 `opcache_reset()` 或重启 FPM。

**preload（PHP 7.4+）**：启动时把常用文件加载到共享内存，所有请求共享，比普通 OPCache 更快（省去了逐文件检查）。

**面试追问**：
- **Q：OPCache 存在哪？** 共享内存（mmap），所有 FPM worker 进程共享。
- **Q：部署后怎么刷新？** `opcache_reset()` 或 `kill -USR2 fpm_master`。
- **Q：OPCache 和 APC 的区别？** APC 是社区扩展，功能包括 OPCode 缓存 + 用户数据缓存。PHP 5.5+ 内置 OPCache 只做 OPCode 缓存，用户缓存建议用 APCu。

---

### Q16: JIT 适合什么场景，Web 请求为什么收益有限？

**JIT（Just-In-Time Compilation）**：PHP 8.0 引入，在 OPCode 基础上进一步编译为**机器码**。

```
PHP 7:  .php → OPCode → Zend VM 解释执行
PHP 8 JIT:  .php → OPCode → 热点 OPCode → 机器码 → CPU 直接执行
```

**为什么 Web 请求收益有限**：

Web 请求的典型瓶颈是 **IO**（数据库、Redis、HTTP 调用），不是 CPU：
- 一次请求 80% 时间在等 IO
- PHP 代码实际 CPU 计算只占 10-20%
- JIT 只加速 CPU 计算部分，对 IO 等待无能为力

```
请求耗时 100ms = IO 等待 80ms + PHP 计算 20ms
JIT 把计算提速 50% → PHP 计算 10ms
总耗时：80 + 10 = 90ms，提升 10%
```

**JIT 真正强的场景**：
- 图像处理（`imagick` / `GD`）
- 数学计算、加密解密
- 模板编译
- 纯 CPU 密集的批处理脚本
- PHP 写的解释器/编译器

**配置**：
```ini
opcache.jit=1255              ; tracing JIT（推荐）
opcache.jit_buffer_size=64M   ; JIT 代码缓存大小
```

---

### Q17: FPM 静态 vs 动态进程模型

**三种模式**（`pm` 配置）：

**static**：固定进程数
```ini
pm = static
pm.max_children = 100
```
- 启动时创建 100 个 worker，一直保持
- 优点：无 fork 开销，延迟稳定
- 缺点：空闲时浪费内存
- **适合**：流量稳定的高 QPS 服务

**dynamic**（默认）：按需伸缩
```ini
pm = dynamic
pm.max_children = 100
pm.start_servers = 20
pm.min_spare_servers = 10
pm.max_spare_servers = 30
```
- 启动 20 个，空闲低于 10 则 fork，高于 30 则 kill
- 优点：节省内存
- 缺点：fork 有延迟、突发流量可能不够
- **适合**：流量波动大的服务

**ondemand**：请求来了才 fork
```ini
pm = ondemand
pm.max_children = 100
pm.process_idle_timeout = 10s
```
- 空闲 10 秒后 kill worker
- 优点：最省内存
- 缺点：首次请求慢（等 fork）
- **适合**：低流量、开发环境

**`max_children` 怎么算**：
```
可用内存 / 单进程内存 = max_children
例：4GB / 40MB = 100
```

**面试追问**：
- **Q：FPM 和 Swoole 的进程模型区别？** FPM 一个请求一个进程（或进程复用），同步阻塞。Swoole 一个 Worker 可以跑多个协程，IO 时自动切换。
- **Q：FPM worker 什么时候重启？** `pm.max_requests = 1000` → 处理 1000 个请求后自杀重启（防止内存泄漏）。

---

### Q18: PHP 内存泄漏怎么排查？

**症状**：FPM worker 内存持续增长，最终 OOM 被 kill。

**排查工具**：

```php
// 1. memory_get_usage — 最基本
echo memory_get_usage(true);       // 系统分配的
echo memory_get_peak_usage(true);  // 峰值

// 2. 在循环中观察增长
foreach ($bigData as $item) {
    process($item);
    if ($i % 1000 === 0) {
        echo memory_get_usage() . "\n";  // 如果持续增长 → 泄漏
    }
}
```

**常见原因**：

1. **循环引用**：
   ```php
   $a = new stdClass;
   $b = new stdClass;
   $a->ref = $b;
   $b->ref = $a;  // 循环引用，refcount 不会归 0
   ```
   解决：`gc_collect_cycles()` 或主动断开引用。

2. **全局变量/静态变量累积**：
   ```php
   class Logger {
       static array $logs = [];
       static function log($msg) {
           self::$logs[] = $msg;  // 不断累积
       }
   }
   ```
   解决：定期清理或限制大小。

3. **事件监听器未注销**：观察者模式里注册了回调但没取消。

4. **大数组未及时释放**：
   ```php
   $data = file_get_contents('1GB.log');  // 直接 OOM
   ```
   解决：用 Generator 或 `fread` 分块读取。

5. **ORM 对象缓存**：Eloquent 的 `all()` 在长驻进程里会缓存查询过的模型。

**Swoole/Hyperf 下更严重**：因为进程不重启，任何一处泄漏都会持续累积。**FPM 靠 `max_requests` 兜底**（定期重启 worker）。

---

## 五、框架（6 题）

### Q19: Laravel 请求生命周期

```
1. index.php 入口
   └── require autoload.php（Composer 自动加载）
   └── require bootstrap/app.php → 创建 Application 容器

2. HTTP Kernel handle($request)
   └── 引导（bootstrap）：
       ├── 环境检测 (.env)
       ├── 配置加载
       ├── 异常处理注册
       ├── Facade 注册
       ├── Service Provider 注册
       └── Service Provider boot

3. 中间件管道（Pipeline）
   └── 全局中间件 → 路由中间件 → 路由组中间件 → 控制器中间件
   └── 洋葱模型：请求一层层进去，响应一层层出来

4. 路由匹配 → 执行 Controller 方法
   └── 方法注入：从容器解析参数类型（反射）
   └── 执行业务逻辑 → 返回 Response

5. Response 反向穿过中间件管道

6. Kernel::terminate()
   └── 可终止中间件的 terminate 方法
   └── 请求结束
```

**面试追问**：

**Q：Service Provider 的 register 和 boot 区别？**
- `register()`：只做绑定（`$this->app->bind()`），不能调其他服务（可能还没注册）
- `boot()`：所有 Provider 注册完成后统一调用，可以调其他服务

**Q：为什么 Laravel 请求慢？**
- 每次请求都要引导（bootstrap）全部服务
- 解决：`php artisan optimize`（缓存配置/路由/视图）
- 终极方案：Octane（常驻进程，只引导一次）

---

### Q20: Laravel 服务容器的绑定与解析

**服务容器的核心**：管理类的依赖和实例化。

**三种绑定方式**：

```php
// 1. bind — 每次解析都 new 一个新实例
$this->app->bind(UserRepository::class, function ($app) {
    return new EloquentUserRepository($app->make(DB::class));
});

// 2. singleton — 全局唯一实例
$this->app->singleton(CacheManager::class, function ($app) {
    return new CacheManager($app['config']['cache']);
});

// 3. instance — 直接绑定已有实例
$this->app->instance('config', new Repository($items));
```

**接口绑定实现**：
```php
$this->app->bind(UserRepositoryInterface::class, EloquentUserRepository::class);
// 以后注入 UserRepositoryInterface 就会得到 EloquentUserRepository
```

**解析**：
```php
// 手动解析
$repo = app(UserRepository::class);
$repo = app()->make(UserRepository::class);

// 自动解析（构造函数注入，最常用）
class UserController {
    public function __construct(
        private UserRepositoryInterface $repo  // 容器自动解析
    ) {}
}
```

**解析过程**（简化）：
```
1. 从绑定表找 UserRepositoryInterface → EloquentUserRepository
2. 反射 EloquentUserRepository 的构造函数
3. 发现参数需要 DB::class
4. 递归解析 DB::class
5. 实例化并返回
```

**上下文绑定**：
```php
$this->app->when(PhotoController::class)
    ->needs(FileSystem::class)
    ->give(LocalFileSystem::class);

$this->app->when(VideoController::class)
    ->needs(FileSystem::class)
    ->give(S3FileSystem::class);
```

---

### Q21: Facade 是如何工作的？

**Facade 不是设计模式里的 Facade，是一种静态代理**。

```php
// 看起来像静态调用
Cache::get('key');

// 实际执行
app('cache')->get('key');
```

**实现原理**：

```php
class Cache extends Facade {
    protected static function getFacadeAccessor(): string {
        return 'cache';  // 容器里的绑定名
    }
}

// Facade 基类的魔术方法
abstract class Facade {
    public static function __callStatic($method, $args) {
        $instance = static::resolveFacadeInstance(static::getFacadeAccessor());
        return $instance->$method(...$args);
    }
}
```

**调用链**：
```
Cache::get('key')
  → Facade::__callStatic('get', ['key'])
    → app('cache')              // 从容器解析
      → $cacheManager->get('key') // 调用真实方法
```

**实时 Facade**（Laravel 5.4+）：
```php
use Facades\App\Services\PaymentService;
PaymentService::charge(100);
// 等价于 app(PaymentService::class)->charge(100)
```

**面试追问**：
- **Q：Facade 和依赖注入哪个好？** 依赖注入更好（可测试、显式依赖）。Facade 方便但隐藏了依赖关系。
- **Q：Facade 能 mock 吗？** 能。`Cache::shouldReceive('get')->once()->andReturn('value')`。

---

### Q22: Eloquent 的懒加载与 N+1 问题

**N+1 问题**：
```php
$posts = Post::all();                    // 1 条 SQL
foreach ($posts as $post) {
    echo $post->author->name;           // 每次循环 1 条 SQL
}
// 总共 1 + N 条 SQL（N = 文章数）
```

**预加载解决**：
```php
// eager loading — with
$posts = Post::with('author')->get();    // 2 条 SQL
// SELECT * FROM posts
// SELECT * FROM users WHERE id IN (1, 2, 3, ...)

// 嵌套预加载
$posts = Post::with(['author', 'comments.user'])->get();

// 条件预加载
$posts = Post::with(['comments' => function ($q) {
    $q->where('approved', true)->orderBy('created_at', 'desc');
}])->get();
```

**懒加载 vs 预加载**：
- **懒加载（Lazy）**：访问关系时才查。简单但有 N+1 风险
- **预加载（Eager）**：`with()` 提前批量查。推荐
- **懒急加载（Lazy Eager）**：`$posts->load('author')` 已经查了但关系没加载时补查

**检测 N+1**：
```php
// 开发环境强制禁止懒加载
Model::preventLazyLoading(!app()->isProduction());
// 触发懒加载时直接抛异常，逼你写 with()
```

---

### Q23: Hyperf 协程模型与 Swoole 关系

**Hyperf = Swoole/Swow 的协程框架**，类似 Laravel 之于 PHP-FPM。

**进程模型**：
```
Master Process
├── Manager Process
│   ├── Worker 0  ─── 协程池（每个请求一个协程）
│   ├── Worker 1  ─── 协程池
│   └── Worker N  ─── 协程池
└── Custom Process
```

**协程工作方式**：
```php
// 一个 Worker 进程内
协程 1: 处理请求 A → MySQL 查询（IO 等待，让出 CPU）
协程 2: 处理请求 B → Redis 读（IO 等待，让出 CPU）
协程 1: MySQL 返回 → 继续处理请求 A
协程 3: 处理请求 C → HTTP 调用（IO 等待，让出 CPU）
协程 2: Redis 返回 → 继续处理请求 B
```

**和 FPM 的核心区别**：
- FPM：一个进程同时只能处理一个请求（同步阻塞）
- Hyperf：一个 Worker 进程可以同时处理**几千个请求**（IO 时自动切换协程）

**性能对比**：
- FPM 100 worker → 并发 100
- Hyperf 4 worker × 每 worker 1000 协程 → 并发 4000

---

### Q24: Hyperf 协程内为什么不能用全局变量？

**根本原因**：多个协程在**同一进程同一线程**内并发，共享同一份全局内存。

```php
// 传统 FPM 下安全（每个请求独立进程）
$_SESSION['user_id'] = 123;

// Hyperf 下危险（多个协程共享同一个 $_SESSION）
// 协程 A 设 $_SESSION['user_id'] = 1
// 协程 B 设 $_SESSION['user_id'] = 2
// 协程 A 读 $_SESSION['user_id'] → 得到 2 ❌ 数据串了
```

**受影响的全局状态**：
- 全局变量 `$GLOBALS`
- 超全局 `$_GET` `$_POST` `$_SESSION` `$_COOKIE`
- 静态属性 `MyClass::$instance`
- 单例模式

**正确做法**：用 **Context**（协程上下文）隔离：

```php
use Hyperf\Context\Context;

// 每个协程独立的上下文
Context::set('user_id', 123);
$userId = Context::get('user_id');
```

Context 底层用 Swoole 的 `Coroutine::getContext()`，每个协程有自己独立的 Context 对象。

**另一个坑：连接池**

```php
// 错误：全局共享一个 MySQL 连接
$db = new PDO(...);
// 协程 A 正在查询，协程 B 也用这个连接查 → 数据错乱

// 正确：从连接池借，用完还
$connection = $pool->get();
try {
    $connection->query(...);
} finally {
    $pool->release($connection);
}
```

Hyperf 的 DB、Redis 等组件已经内置连接池。

---

## 六、生态（4 题）

### Q25: Composer 是如何解析依赖的？`composer.lock` 的作用？

**依赖解析（SAT 求解）**：

Composer 把版本约束转化为**布尔可满足性问题（SAT）**，用改良的 DPLL 算法求解。

```json
{
    "require": {
        "laravel/framework": "^10.0",
        "guzzlehttp/guzzle": "^7.0"
    }
}
```

解析过程：
1. 从 Packagist 拉取所有候选版本
2. 根据约束构建版本图
3. 解决冲突（A 要 X ^2.0，B 要 X ^3.0 → 无法解析）
4. 输出完整的依赖树 → 写入 `composer.lock`

**`composer.lock` 的作用**：

- 记录**精确版本号**（不是约束，是 `2.3.1` 这样的确切版本）
- 保证所有环境（开发/测试/生产）安装**完全一致**的版本
- `composer install`：按 lock 安装（确定性）
- `composer update`：重新解析约束，更新 lock

**必须提交 `composer.lock` 到 Git**。

**面试追问**：

**Q：`require` 和 `require-dev` 区别？**
- `require`：生产依赖
- `require-dev`：开发依赖（测试、调试工具）
- `composer install --no-dev`：生产部署时跳过开发依赖

**Q：`^` 和 `~` 的区别？**
- `^2.3.1`：`>= 2.3.1 && < 3.0.0`（最左非零数字不变）
- `~2.3.1`：`>= 2.3.1 && < 2.4.0`（最后一位可变）

---

### Q26: PSR-4 与 PSR-0 的差异

两者都是**自动加载规范**。

**PSR-0**（已废弃）：
```
命名空间的 _ 被转换为目录分隔符
Foo_Bar_Baz → Foo/Bar/Baz.php
```

**PSR-4**（当前标准）：
```json
{
    "autoload": {
        "psr-4": {
            "App\\": "src/"
        }
    }
}
```
```
App\Models\User → src/Models/User.php
```

**核心区别**：
- PSR-0：命名空间完整映射到目录，目录层级深
- PSR-4：可以**省略前缀对应的目录**（`App\` 映射到 `src/`，不需要 `src/App/`）
- PSR-4 目录更简洁，是现代 PHP 唯一推荐的方式

---

### Q27: Swoole 协程调度原理

**核心**：**用户态线程 + IO 事件驱动 + 自动让出**。

Swoole 通过 C 层 hook 了 PHP 的所有 IO 函数：

```
原始 PHP：
  mysqli_query() → 阻塞等待 MySQL 返回 → 继续
  
Swoole Hook 后：
  mysqli_query()
    → 检测到 IO → 注册 epoll 事件
    → 保存当前协程上下文（栈帧）→ 切到其他协程
    → MySQL 返回 → epoll 通知 → 恢复该协程继续执行
```

**调度器核心循环**：
```
while (有协程存活) {
    1. 执行就绪协程队列
    2. 检查 epoll 事件（IO 完成的通知）
    3. 把 IO 完成的协程加入就绪队列
    4. 检查定时器
}
```

**关键 API**：
```php
// 启用协程化（hook 所有 IO）
Swoole\Runtime::enableCoroutine(SWOOLE_HOOK_ALL);

// 创建协程
go(function () {
    $result = file_get_contents('http://example.com');  // 自动让出
});
go(function () {
    sleep(1);  // hook 后变成协程 sleep，不阻塞进程
});
```

**被 hook 的函数**：MySQL、Redis、PDO、file_get_contents、sleep、curl 等几乎所有 IO 操作。

**不能被 hook 的**：
- CPU 密集计算（没有 IO 就没有让出点）
- 某些 C 扩展的 IO（如果没被 Swoole hook）

---

### Q28: Swoole Server 的进程模型

```
Master Process（主进程）
├── Reactor Thread × N    ← 处理网络事件（epoll）
│   ├── 接收客户端连接
│   ├── 接收请求数据
│   └── 发送响应数据
│
├── Manager Process（管理进程）
│   ├── Worker Process × M     ← 处理业务逻辑
│   │   ├── onRequest 回调
│   │   ├── 协程执行
│   │   └── DB/Redis/HTTP 调用
│   │
│   └── Task Worker × K        ← 处理异步任务
│       ├── 耗时任务（邮件、日志写入）
│       └── 同步阻塞操作
```

**各角色职责**：

| 角色 | 职责 | 数量建议 |
|---|---|---|
| **Master** | 管理 Reactor 线程 | 1 个 |
| **Reactor** | 网络 IO（epoll），分发请求到 Worker | CPU 核数 |
| **Manager** | 管理 Worker/Task 生命周期，异常重启 | 1 个 |
| **Worker** | 业务逻辑执行，协程运行 | CPU 核数 × 1~2 |
| **Task Worker** | 异步任务（不影响主请求） | 按业务需求 |

**Worker 和 Task Worker 的区别**：

```php
// Worker 处理正常请求
$server->on('request', function ($request, $response) {
    // 快速响应
    $response->end('OK');
    
    // 需要耗时的工作丢给 Task Worker
    $server->task(['type' => 'send_email', 'to' => 'jake@example.com']);
});

// Task Worker 处理异步任务
$server->on('task', function ($server, $taskId, $workerId, $data) {
    sendEmail($data['to']);  // 可以慢慢来，不影响用户响应
});
```

**面试追问**：

**Q：Worker 挂了怎么办？**
Manager 检测到 Worker 退出后自动 fork 新的（`max_request` 到了或异常退出）。

**Q：Reactor 和 Worker 怎么通信？**
通过 Unix Socket（unixSocketBufferSize）。Reactor 把解析好的请求通过 Socket 发给 Worker。

---

## 总结记忆点

### 基础
1. PHP 数组 = HashTable + 有序 Bucket 数组
2. `===` 永远安全，`==` 在 PHP 8 修了 `0 == "a"` 但仍有坑
3. `&` 是别名不是指针，会禁用 COW

### OOP
4. Trait 是代码粘贴，优先级：当前类 > Trait > 父类
5. `static::` 运行时绑定，`self::` 编译时绑定

### 高级
6. Generator 靠 yield 暂停/恢复，解决大数据内存问题
7. Fiber 是底层原语，不是协程本身

### 性能
8. OPCache 缓存 OPCode 到共享内存，生产 `validate_timestamps=0`
9. JIT 对 Web 收益有限（IO 密集，不是 CPU 密集）
10. FPM static 适合稳定流量，dynamic 适合波动流量

### 框架
11. Laravel 请求 = Bootstrap → 中间件管道 → Controller → Response
12. 容器 = bind/singleton + 反射自动解析
13. N+1 用 `with()` 预加载解决
14. Hyperf 协程内不能用全局变量，用 Context 隔离

### 生态
15. Composer SAT 求解，lock 文件保证确定性
16. Swoole 协程 = hook IO → epoll → 自动让出 → 恢复
17. Swoole 进程模型 = Master/Reactor/Manager/Worker/Task

---

**参考资料**
- [PHP Internals Book](https://www.phpinternalsbook.com/)
- [PHP RFC: Saner string to number comparisons](https://wiki.php.net/rfc/string_to_number_comparison)
- [Laravel Lifecycle](https://laravel.com/docs/lifecycle)
- [Swoole Documentation](https://wiki.swoole.com/)
- [Hyperf Documentation](https://hyperf.wiki/)
