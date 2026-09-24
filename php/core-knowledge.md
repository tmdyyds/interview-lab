# PHP 核心知识全景

**标签**: #php #zend #fpm #opcache #psr #core #高频

系统化梳理 PHP 核心知识——**架构 → 请求生命周期 → zval 与内存 → 数组底层 → 编译执行 → FPM 内核 → Nginx 集成 → 错误体系 → PSR 全景 → Composer → PHP 8+ 新特性 → 协程对比**。

定位是**知识纲要 + 底层原理**，配套 [interview-answers.md](./interview-answers.md) 的 28 道问答使用：本文讲**结构、机制、版本演进**，问答讲**具体考点**，遇到概念不清就跳过去看题目实战。

---

## 目录

1. [PHP 整体架构](#一php-整体架构)
2. [请求生命周期五阶段](#二请求生命周期五阶段)
3. [zval 与变量系统](#三zval-与变量系统)
4. [HashTable：数组的底层](#四hashtable数组的底层)
5. [编译执行流程 与 OPCache](#五编译执行流程-与-opcache)
6. [FPM 内核详解](#六fpm-内核详解)
7. [Nginx + FPM 部署模型](#七nginx--fpm-部署模型)
8. [内存管理与 GC](#八内存管理与-gc)
9. [错误体系全景](#九错误体系全景)
10. [PSR 全景](#十psr-全景)
11. [Composer 与 autoload](#十一composer-与-autoload)
12. [PHP 8 / 8.1 / 8.2 / 8.3 新特性](#十二php-8--81--82--83-新特性)
13. [协程模型对比](#十三协程模型对比)
14. [常用扩展速查](#十四常用扩展速查)
15. [性能优化速查](#十五性能优化速查)

---

## 一、PHP 整体架构

PHP 是**分层架构**——SAPI 抽象运行环境，Zend 引擎负责编译执行，扩展系统提供功能。

```
┌────────────────────────────────────────────────┐
│                 应用代码                        │
│           (Laravel / Hyperf / 业务)             │
└────────────────────────────────────────────────┘
                     │
┌────────────────────▼───────────────────────────┐
│  SAPI (Server API) —— 运行环境抽象层           │
│  ┌────┐ ┌──────┐ ┌────┐ ┌───────┐ ┌────────┐  │
│  │ CLI│ │ FPM  │ │CGI │ │ Apache│ │ Swoole │  │
│  └────┘ └──────┘ └────┘ └───────┘ └────────┘  │
└────────────────────┬───────────────────────────┘
                     │
┌────────────────────▼───────────────────────────┐
│              Zend 引擎                          │
│  ┌────────┐  ┌────────┐  ┌────────┐            │
│  │ 词法/  │  │ Zend   │  │ 内存 / │            │
│  │ 语法   │→ │ VM     │  │ GC     │            │
│  │ 编译   │  │ 执行   │  │        │            │
│  └────────┘  └────────┘  └────────┘            │
└────────────────────┬───────────────────────────┘
                     │
┌────────────────────▼───────────────────────────┐
│              扩展系统                           │
│  Core / OPCache / PDO / mbstring / curl /      │
│  Swoole / Xdebug / apcu / ...                  │
└────────────────────────────────────────────────┘
```

### SAPI 是什么

**Server API 抽象层**——把"从哪来的请求"这件事和 Zend 引擎解耦。同一份 PHP 代码：

- `php artisan xxx` → **CLI SAPI**
- `nginx → 9000/php-fpm.sock` → **FPM SAPI**
- Swoole 直接跑 PHP 脚本 → **Swoole 是"自定义 SAPI"**（不走标准 SAPI，直接 hook Zend）

**面试记忆点**：一个 PHP 安装通常同时有多个 SAPI 二进制：`/usr/bin/php`（CLI）和 `/usr/sbin/php-fpm`（FPM），共享同一个 `.ini` 配置目录但生效的配置不完全相同（`php --ini` 看当前 SAPI 用的是哪个）。

### 三大执行模式对比

| 模式 | 生命周期 | 场景 |
|-----|---------|-----|
| **CLI** | 一次执行完就退 | 命令行脚本、artisan、cron |
| **FPM** | 每个请求一次 Init/Execute/Shutdown | 传统 Web（Laravel、Symfony、WordPress） |
| **常驻**（Swoole / RoadRunner / FrankenPHP） | **进程常驻**，请求间不释放对象 | 高性能微服务（Hyperf） |

**常驻模式的核心差异**：**全局状态在请求间不重置**——所以不能像 FPM 那样"用完就丢，进程死了自然清"。写代码必须像 Java / Go 那样管好状态。

---

## 二、请求生命周期五阶段

**FPM SAPI 视角**——每个请求走完 5 个阶段。

```
┌───────────────────────────────────────────────────┐
│  FPM Master 启动                                   │
│    ↓                                               │
│  1. MINIT (Module Init) —— 每个扩展初始化一次      │
│     - 加载 extension.so                            │
│     - 注册函数、常量、类                            │
│     - opcache 载入共享内存                          │
│    ↓                                               │
│  FPM Worker 启动 → 进入等待                        │
│    ↓ 收到请求                                       │
│  ┌───────────── 每个请求 ─────────────┐            │
│  │  2. RINIT (Request Init)           │            │
│  │     - 初始化 EG / CG                │            │
│  │     - session 启动、超全局变量准备   │            │
│  │  3. EXECUTE                        │            │
│  │     - 编译脚本 → OPArray            │            │
│  │     - 执行 OPCode                   │            │
│  │  4. RSHUTDOWN (Request Shutdown)   │            │
│  │     - 清 register_shutdown_function │            │
│  │     - 关闭 session                  │            │
│  │     - EG 内存池整体释放             │            │
│  └────────────────────────────────────┘            │
│    ↓ 回到等待，下一个请求                            │
│    ↓ Worker 处理够 max_requests 请求后退出          │
│  5. MSHUTDOWN (Module Shutdown) —— 进程退出时      │
└───────────────────────────────────────────────────┘
```

### 关键点

- **MINIT / MSHUTDOWN 只在进程启动/退出时各跑一次** —— 扩展的全局初始化在这里
- **RINIT / RSHUTDOWN 每个请求跑一次** —— 请求级状态在这里管理
- **RSHUTDOWN 的内存池释放是关键**——请求内 `new` 的对象、分配的字符串、数组，**RSHUTDOWN 时整体释放**（不是靠 GC，是整个内存池 free）
- **PHP-FPM 的 "每请求独立" 语义就是靠 RSHUTDOWN 的整体释放实现的**

### 影响

- `static` 变量在**同一 worker 内的不同请求间保留**（RSHUTDOWN 不清 static 存储）
- 单例、连接池在传统 FPM 里意义不大——**下个请求进来时 static 数据还在，但环境不一定还在原来 worker**
- **常驻模式下所有请求共享 worker 内存**——不能用请求级作用域的对象污染全局

---

## 三、zval 与变量系统

**面试深度题的核心**。PHP 7 之后 zval 大改，性能翻倍。

### PHP 7+ zval 结构（简化版）

```c
// Zend/zend_types.h（简化）
struct _zval_struct {
    zend_value        value;     // 值本身，8 字节（union）
    union {
        struct {
            zend_uchar type;     // 类型：IS_NULL / IS_LONG / IS_STRING ...
            zend_uchar type_flags;
            zend_uchar const_flags;
            zend_uchar reserved;
        } v;
        uint32_t type_info;
    } u1;
    union {
        uint32_t next;           // 用于 HashTable 冲突链
        uint32_t cache_slot;
        // ...
    } u2;
};
// 一个 zval = 16 字节（PHP 5 时代 zval 是 40+ 字节）
```

**value 联合体**：

```c
union _zend_value {
    zend_long         lval;      // long
    double            dval;      // double
    zend_refcounted  *counted;   // 指向可引用计数的对象
    zend_string      *str;       // 字符串
    zend_array       *arr;       // 数组
    zend_object      *obj;       // 对象
    zend_resource    *res;       // 资源
    zend_reference   *ref;       // 引用
    // ...
};
```

### 关键改进（PHP 7 vs PHP 5）

| 项 | PHP 5 | PHP 7+ |
|---|-------|--------|
| zval 大小 | ~40 字节 | 16 字节 |
| 简单类型（int/bool） | 也走引用计数，堆分配 | **值直接内嵌**，零分配 |
| 数组 | zval 数组 → zval → 值 | 直接指向 zend_array |
| 内存 | 大量小 alloc | 内存池 + 类型内嵌 |

**为什么 PHP 7 快 2 倍**：**简单类型不再堆分配**是最大功臣（配合 HashTable 优化）。

### 引用计数与 COW

**引用计数**只针对**堆分配的类型**（string、array、object、resource、reference），标记为 `IS_TYPE_REFCOUNTED`。整数、浮点、布尔、null 不计数（值内嵌了）。

```php
$a = "hello";           // 字符串堆分配，refcount = 1
$b = $a;                // COW：不复制，只 refcount++，refcount = 2
$b .= " world";         // 写时复制：refcount--，b 拿到新副本，refcount = 1
```

**引用（`&`）会禁用 COW**：

```php
$a = "hello";
$b = &$a;               // 加 is_ref 标志，不是普通 COW
$b .= " world";         // 直接改 a 的内存，$a 也变了
```

**面试深挖**：为什么"引用计数循环引用"会内存泄漏？——ARC 无法回收循环，需要 **GC 循环收集器**（详见第八节）。

---

## 四、HashTable：数组的底层

**PHP 数组既是索引数组又是字典**，靠的就是 HashTable。

### 结构（简化）

```c
struct _zend_array {
    zend_refcounted_h gc;
    union {
        struct {
            ZEND_ENDIAN_LOHI_4(
                zend_uchar    flags,
                zend_uchar    _unused,
                zend_uchar    nIteratorsCount,
                zend_uchar    _unused2)
        } v;
        uint32_t flags;
    } u;
    uint32_t          nTableMask;
    Bucket           *arData;        // ★ Bucket 数组（真正存 KV 的地方）
    uint32_t          nNumUsed;
    uint32_t          nNumOfElements;
    uint32_t          nTableSize;    // 2 的幂，如 8/16/32
    uint32_t          nInternalPointer;
    zend_long         nNextFreeElement;
    dtor_func_t       pDestructor;
};

typedef struct _Bucket {
    zval              val;           // ★ 值
    zend_ulong        h;             // ★ 哈希（整数键就是键本身）
    zend_string      *key;           // ★ 字符串键（整数键时为 NULL）
} Bucket;
```

### 布局图

```
zend_array {
    arData ──▶ ┌──────────┬──────────┬──────────┬──────────┐
               │ Bucket 0 │ Bucket 1 │ Bucket 2 │ Bucket 3 │  ← 有序，按插入顺序
               │ val, h,  │ val, h,  │ val, h,  │ val, h,  │
               │ key      │ key      │ key      │ key      │
               └──────────┴──────────┴──────────┴──────────┘
                    ↑
    Hash 索引区（arData 的前面）：
    hash(key) & mask → 存 Bucket 序号
    冲突通过 Bucket 里的 next 字段串成链表
}
```

**关键设计**：
1. **有序性**：`arData` 是**紧凑数组**，按插入顺序存 —— 保证 foreach 顺序
2. **O(1) 访问**：哈希索引直接映射到 Bucket 序号
3. **合并索引数组和关联数组**：整数键的 `key = NULL`，只用 `h`；字符串键用 `key + h`

### 关键操作复杂度

| 操作 | 复杂度 | 说明 |
|-----|-------|------|
| `$arr[$k]` 读 | O(1) 均摊 | 哈希直接找 |
| `$arr[] = $v` 追加 | O(1) 均摊 | Bucket 数组尾部 |
| `$arr[$k] = $v` 写 | O(1) 均摊 | 可能触发扩容 |
| `unset($arr[$k])` | O(1) | Bucket 里标记删除（不立即压缩） |
| foreach | O(n) | 遍历 arData |
| `array_shift` | **O(n)** ★ | 要重建键，慢！|
| `array_unshift` | **O(n)** ★ | 同上 |

**面试考点**：`array_shift` 慢！因为要把整个索引数组的 h 都减 1。**要 O(1) 出队用 `SplQueue`**。

### 扩容规则

- 初始 `nTableSize = 8`
- 满了扩容到当前 × 2
- 扩容时**重新计算所有哈希**（rehash）

---

## 五、编译执行流程 与 OPCache

### 完整 5 阶段

```
PHP 源码
   │
   ▼
[1] 词法分析（Lexer）
    src/Zend/zend_language_scanner.re
   │
   ▼
[2] 语法分析（Parser）—— 生成 AST
    src/Zend/zend_language_parser.y
   │
   ▼
[3] AST → OPArray 编译
    src/Zend/zend_compile.c
   │
   ▼
[4] Zend VM 执行 OPCode
    src/Zend/zend_vm_execute.h
   │
   ▼
[5] JIT（PHP 8+，可选）
    OPCode → 机器码
```

### 无 OPCache：每个请求全流程

```
请求 1: 源码 → 词法 → 语法 → AST → OPArray → 执行 → 释放
请求 2: 源码 → 词法 → 语法 → AST → OPArray → 执行 → 释放
                     ↑ 每次都重复！
```

大量 CPU 消耗在"编译"上——生产没开 opcache = **性能砍半**。

### 开 OPCache 后

```
第一次请求：
   源码 → 词法 → 语法 → AST → OPArray ──┐
                                        │
                              ┌─────────▼───────┐
                              │ 共享内存 (SHM)   │
                              │ 缓存 OPArray    │
                              └─────────┬───────┘
                                        │
   执行 ← ─────────────────────────────┘

第 N 次请求：
   源码 ─┐
        ▼
   查找 SHM → 命中 → 直接执行 OPCode
```

**性能收益**：省掉词法+语法+编译，通常提速 **50-100%**。

### OPCache 关键参数

```ini
[opcache]
opcache.enable=1
opcache.memory_consumption=256           ; SHM 大小 MB
opcache.max_accelerated_files=20000      ; 最大缓存文件数
opcache.validate_timestamps=0            ; ★ 生产必设 0（不检查文件时间戳）
opcache.revalidate_freq=0                ; validate_timestamps=1 时的检查间隔
opcache.save_comments=1                  ; 保留 DocBlock 注释（反射需要）
opcache.enable_cli=0                     ; CLI 通常不开
opcache.jit=1255                         ; PHP 8+ JIT 配置
opcache.jit_buffer_size=100M
```

**面试关键**：`validate_timestamps=0` 后代码更新不生效——**必须** `opcache_reset()` 或重启 FPM 或删共享内存。这是很多"发布不生效"事故的根源。

### 部署时 opcache 处理

```bash
# 方式 1：graceful reload FPM（推荐）
sudo systemctl reload php8.2-fpm       # 或 kill -USR2 pid

# 方式 2：通过 FPM 管理接口清 opcache
# 需要暴露 opcache_reset() 接口（内网可访问）

# 方式 3：opcache_reset() 通过一个内网 URL 触发（发布脚本调用）
```

生产大厂常用**方式 3**：一个内网 `/opcache-reset` 端点，Deploy 完调一下。

### JIT

PHP 8.0 引入。**在 OPCode 基础上再编译成机器码**。

**JIT 收益的场景**：
- **CPU 密集计算**（数学、图像处理、加密）：可能 2-5x
- **纯业务代码**（大部分是 IO 等待）：**几乎无收益**（甚至因为额外内存和编译时间轻微变慢）

**结论**：Web 请求不用 JIT（关掉或最保守设置）；数值计算类脚本值得开。

---

## 六、FPM 内核详解

### 进程模型

```
┌─────────────────────────┐
│   FPM Master (root)      │  ← 读配置、管理 worker
└──────────┬──────────────┘
           │
     ┌─────┴─────┬─────────┬──────┐
     ▼           ▼         ▼      ▼
  ┌──────┐  ┌──────┐  ┌──────┐ ┌──────┐
  │Worker│  │Worker│  │Worker│ │ ...  │  ← 处理请求，运行时权限降级
  └──────┘  └──────┘  └──────┘ └──────┘
     │           │
     └─ Nginx 通过 FastCGI 协议 → 分发请求给 worker
```

**Master 职责**：
- 监听端口 / socket（9000 或 unix sock）
- 启动 / 监控 worker
- 处理管理信号（reload、graceful stop）
- **不处理请求**

**Worker 职责**：
- accept 连接
- 按 FastCGI 协议读请求
- Zend 引擎处理 PHP 代码
- 返回响应，释放请求内存池

### pm 三种模式对比

```ini
[www]
listen = /run/php/php8.2-fpm.sock
user = www-data
group = www-data

; ===== 模式 1：static（推荐生产稳定流量）=====
pm = static
pm.max_children = 50                  ; 固定 50 个 worker

; ===== 模式 2：dynamic（默认）=====
pm = dynamic
pm.max_children = 100                 ; 上限
pm.start_servers = 20                 ; 启动时
pm.min_spare_servers = 10             ; 空闲 worker 最少
pm.max_spare_servers = 30             ; 空闲 worker 最多

; ===== 模式 3：ondemand（低流量或省内存）=====
pm = ondemand
pm.max_children = 100
pm.process_idle_timeout = 10s         ; 空闲多久回收

; 共同参数
pm.max_requests = 500                 ; ★ 每个 worker 处理够 500 请求就重启（清内存）
request_terminate_timeout = 30s       ; ★ 单请求最长时间
```

### 计算 max_children 的公式

```
max_children = (可用内存 - 系统预留) / 单 worker 内存
```

**示例**：8GB 内存服务器
- 系统 + 其他服务预留 2GB → PHP 可用 6GB
- 单 worker 平均 60MB → **max_children ≈ 100**

**过大**：内存不够会 swap 或 OOM，反而慢
**过小**：请求排队 → 502 Bad Gateway

### 关键状态页

```ini
pm.status_path = /status
```

访问 `http://your-domain/status`：

```
pool:                 www
process manager:      dynamic
start time:           22/Sep/2026:10:30:15 +0800
accepted conn:        123456
listen queue:         0                    ; ★ 排队请求数（长期 > 0 说明 worker 不够）
max listen queue:     10
idle processes:       15                   ; ★ 空闲 worker
active processes:     5
total processes:      20
max active processes: 45                   ; 历史最大活跃数
```

**监控关键**：
- `listen queue > 0` 持续 → **worker 不够**，加 max_children 或加机器
- `max active processes` 逼近 `max_children` → 快满了，扩容

### 慢日志（Slow Log）

```ini
slowlog = /var/log/php-fpm-slow.log
request_slowlog_timeout = 5s              ; 请求超过 5s 记堆栈
```

输出示例：

```
[22-Sep-2026 10:30:15]  [pool www] pid 12345
script_filename = /var/www/index.php
[0x00007f8c00000000] mysql_query() /var/www/User.php:42
[0x00007f8c00000010] User->find() /var/www/UserController.php:15
```

**排查慢接口的利器**——比在应用层加日志便宜且不用改代码。

---

## 七、Nginx + FPM 部署模型

### 完整数据流

```
浏览器 ─HTTP─▶ Nginx :80 / :443
                  │
                  │ FastCGI 协议
                  │ (unix sock 或 tcp 9000)
                  ▼
             FPM Master
                  │
              ┌───┴───┐
              ▼       ▼
           Worker  Worker  ...
              │       │
              └───────┴──▶ PHP 代码 → MySQL / Redis / ...
```

### 标准 Nginx 配置

```nginx
server {
    listen 80;
    server_name example.com;
    root /var/www/html/public;                # ★ Laravel 的 public 目录
    index index.php;

    # ★ SPA / Laravel 风格路由
    location / {
        try_files $uri $uri/ /index.php?$query_string;
        #        ↑ 静态文件      ↑ 目录       ↑ 交给 PHP 入口处理
    }

    # ★ PHP 请求转 FPM
    location ~ \.php$ {
        # 用 unix sock（生产推荐，比 TCP 快）
        fastcgi_pass unix:/run/php/php8.2-fpm.sock;
        # fastcgi_pass 127.0.0.1:9000;         # ← 也可以走 TCP

        fastcgi_index index.php;
        include fastcgi_params;

        # ★ 关键：告诉 PHP 要执行哪个脚本
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        #                              ↑ root 指令的值   ↑ 请求里的 .php 部分
        fastcgi_param PATH_INFO $fastcgi_path_info;

        # ★ 超时（超过就 504）
        fastcgi_read_timeout 60s;
        fastcgi_send_timeout 30s;
        fastcgi_connect_timeout 5s;

        # ★ 缓冲
        fastcgi_buffers 8 16k;
        fastcgi_buffer_size 32k;
    }

    # ★ 拒绝隐藏文件（.env / .git）
    location ~ /\. {
        deny all;
    }
}
```

### SCRIPT_FILENAME 陷阱（安全漏洞）

**错误写法**：

```nginx
location ~ \.php$ {
    fastcgi_pass unix:/run/php/php8.2-fpm.sock;
    fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
}
```

**攻击**：`http://example.com/upload/evil.jpg/x.php`

如果 `cgi.fix_pathinfo=1`（老版本默认），PHP 会一直往前找**存在的文件**——找到 `evil.jpg` 就把它当 PHP 脚本执行！攻击者上传 jpg 里藏 PHP 代码就能 RCE。

**修复**：

```nginx
location ~ \.php$ {
    # ★ 严格检查文件存在
    try_files $uri =404;
    # ...
}
```

或者 `php.ini` 里：

```ini
cgi.fix_pathinfo=0
```

PHP 7+ 默认已经是 0，但检查配置总是好习惯。

### 502 / 504 排查

**502 Bad Gateway** — FPM 不可达：
- FPM 挂了 / sock 文件权限错 / 端口错
- Worker 全忙没空闲（listen queue 满）

**504 Gateway Timeout** — FPM 处理超时：
- 请求超过 `fastcgi_read_timeout`
- 后端（DB / API）响应慢
- PHP 死循环

看 FPM 慢日志、Nginx error.log、系统内存和 CPU。

---

## 八、内存管理与 GC

### 请求级内存池（Zend Memory Manager）

PHP 每个请求有一个**独立的内存池**（EG(memory_manager)）：

- `new`、字符串拼接、数组分配都从池里取
- **RSHUTDOWN 时整个池 free** —— 不逐个析构

**含义**：
- **短生命周期对象几乎零成本**（不用 free，反正马上一起清）
- 但 `unset` **不一定立即释放物理内存**（内存回到池但没还给 OS）
- `memory_get_usage(true)` 看池的 real size

### 循环引用与 GC

**引用计数解决不了循环引用**：

```php
$a = new stdClass();
$b = new stdClass();
$a->b = $b;    // b refcount = 2
$b->a = $a;    // a refcount = 2
unset($a);     // a refcount = 1（$b->a 还在）
unset($b);     // b refcount = 1（$a->b 还在）
// $a 和 $b 都是垃圾但 refcount != 0 → 泄漏
```

PHP 5.3 引入**同步 GC 循环收集器**：

- 疑似"可能循环"的 zval 放入 gc_root_buffer
- buffer 满（默认 10000）或调 `gc_collect_cycles()` 时启动扫描
- 用**三色标记法**找出真正的循环，回收

**主动触发**：

```php
gc_collect_cycles();     // 手动 GC，返回回收的对象数
gc_disable();            // 关闭自动 GC（长事务/批处理场景）
gc_enable();
```

**Swoole/常驻场景**：每处理一批请求后手动 GC 一次，防止 root buffer 堆积。

---

## 九、错误体系全景

### 错误分类

PHP 7 之前**错误（Error）和异常（Exception）是两套**——错误不能 try-catch。**PHP 7 统一到 Throwable**：

```
Throwable (interface)
    │
    ├── Error (class)
    │   ├── TypeError               类型错误
    │   ├── ValueError              (PHP 8+) 值不合法
    │   ├── ArgumentCountError      参数数量错
    │   ├── ArithmeticError
    │   │   └── DivisionByZeroError
    │   ├── AssertionError
    │   ├── ParseError              语法错
    │   └── CompileError
    │
    └── Exception (class)
        ├── RuntimeException
        │   ├── OutOfBoundsException
        │   ├── UnexpectedValueException
        │   └── ...
        └── LogicException
            ├── InvalidArgumentException
            ├── DomainException
            └── ...
```

**关键**：PHP 7+ 可以 `try { ... } catch (Throwable $t)` 抓一切错误 + 异常。

### 错误级别（老式 error）

不通过异常抛出的运行时问题（PHP 5 遗留）：

```php
E_ERROR              // 致命错误，脚本终止
E_WARNING            // 警告，继续执行
E_NOTICE             // 通知，继续执行
E_DEPRECATED         // 废弃警告
E_USER_ERROR / _WARNING / _NOTICE
E_STRICT             // 5.4-7.4 用，8.0 移除
```

**PHP 8 大改动**：过去很多 E_NOTICE / E_WARNING 升级成了 Error 或 TypeError（比如未定义变量、未定义数组下标）。

### set_error_handler

**把老式 error 转成异常**（推荐用法）：

```php
<?php

set_error_handler(function ($severity, $message, $file, $line) {
    if (!(error_reporting() & $severity)) {
        return false;   // @ 抑制符号或 error_reporting 关闭该级别
    }
    throw new ErrorException($message, 0, $severity, $file, $line);
});

// 现在 include 一个不存在的文件也是 catch-able
try {
    include 'nonexistent.php';
} catch (Throwable $e) {
    // 抓到了
}
```

Laravel、Symfony 内部就是这么干的——统一走 Throwable。

### register_shutdown_function

**RSHUTDOWN 阶段的最后一次机会**——处理致命错误：

```php
<?php

register_shutdown_function(function () {
    $err = error_get_last();
    if ($err && in_array($err['type'], [E_ERROR, E_CORE_ERROR, E_PARSE])) {
        // 致命错误：记日志、发通知
        error_log("Fatal: {$err['message']} in {$err['file']}:{$err['line']}");
        // 给用户返回友好错误页
        http_response_code(500);
        echo json_encode(['error' => 'server error']);
    }
});
```

---

## 十、PSR 全景

PHP-FIG 制定的**互操作性标准**。让 Laravel 组件能被 Symfony 用，反之亦然。

### 核心 PSR（按面试频率）

| PSR | 主题 | 现代常用度 |
|-----|-----|----------|
| **PSR-4** ★ | 自动加载 | 100%（Composer 用它） |
| **PSR-12** | 编码风格（PSR-2 升级版） | 90%（IDE 和 linter 默认） |
| **PSR-7** ★ | HTTP 消息接口（Request/Response） | 80%（框架间共享） |
| **PSR-11** ★ | 容器接口 | 80% |
| **PSR-15** | HTTP 中间件 | 70%（现代框架都用） |
| **PSR-17** | HTTP 工厂 | 配合 PSR-7 用 |
| **PSR-3** | 日志接口 | 100%（Monolog 遵循） |
| **PSR-6** / **PSR-16** | 缓存（对象/简化） | 70% |
| **PSR-14** | 事件分发 | 50% |
| **PSR-18** | HTTP 客户端 | 增长中 |

**淘汰的**：
- PSR-0（旧自动加载）→ PSR-4 替代
- PSR-1 / PSR-2 → PSR-12 替代

### PSR-4 自动加载

**规则**：命名空间前缀 ↔ 文件路径前缀。

```json
{
    "autoload": {
        "psr-4": {
            "App\\": "src/",
            "Vendor\\Package\\": "vendor-src/"
        }
    }
}
```

- `App\User\Service` → `src/User/Service.php`
- `Vendor\Package\Http\Client` → `vendor-src/Http/Client.php`

**PSR-4 vs PSR-0 关键差异**：PSR-0 里 `_` 会被转成 `/`（老式 PEAR 风格），PSR-4 不转。

### PSR-7 Request / Response

**不可变对象**（immutable）——修改返回新实例：

```php
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Message\ResponseInterface;

$response = $response
    ->withStatus(200)
    ->withHeader('Content-Type', 'application/json');
$response->getBody()->write(json_encode(['ok' => true]));
```

**面试考点**：为什么不可变？——**线程安全**（对 Swoole 常驻场景友好）+ 便于函数式链式调用。

### PSR-15 中间件

```php
use Psr\Http\Server\MiddlewareInterface;
use Psr\Http\Server\RequestHandlerInterface;

class AuthMiddleware implements MiddlewareInterface
{
    public function process(
        ServerRequestInterface $request,
        RequestHandlerInterface $handler
    ): ResponseInterface {
        // 前置逻辑
        if (!$this->authenticate($request)) {
            return new Response(401);
        }

        // 交给下一个中间件
        $response = $handler->handle($request);

        // 后置逻辑
        return $response->withHeader('X-Auth', 'ok');
    }
}
```

**洋葱模型**——每层可以在前后各做一次逻辑。

### PSR-11 容器

```php
use Psr\Container\ContainerInterface;

interface ContainerInterface {
    public function get(string $id);            // 拿实例（找不到抛异常）
    public function has(string $id): bool;      // 是否有绑定
}
```

Laravel、Symfony、Hyperf 的容器都实现了这个接口，可以互换使用。

---

## 十一、Composer 与 autoload

### 依赖解析简述

Composer 用 **SAT（Boolean Satisfiability）求解器**处理依赖冲突：

- 每个 `require` 是一个约束（`^1.2`、`~2.0` 等）
- 求解器找一组满足所有约束的**版本组合**
- 找不到 → 报冲突

**`composer.lock`**：锁定"某次求解出来的**具体版本集合**"——保证不同机器 `composer install` 结果一致。

```bash
composer install    # 用 lock（生产必须）
composer update     # 重新求解 + 更新 lock（开发用）
```

### 4 种 autoload 方式

`composer.json`：

```json
{
    "autoload": {
        "psr-4": {
            "App\\": "src/"
        },
        "psr-0": {
            "Legacy_": "legacy/"
        },
        "classmap": [
            "vendor/legacy-lib/src/"
        ],
        "files": [
            "src/helpers.php"
        ]
    }
}
```

| 方式 | 用途 | 加载时机 |
|-----|-----|---------|
| **psr-4** | 现代命名空间 → 目录 | 首次用到时（惰性） |
| **psr-0** | 老代码兼容 | 首次用到时（惰性） |
| **classmap** | 扫描全部 PHP 文件生成映射 | **执行 composer install 时扫描一次** |
| **files** | 全局函数、常量 | **每次请求 autoload 一开始就 require** |

**性能**：`psr-4` 走目录约定，慢；`classmap` 生成后是 O(1)。**生产上跑**：

```bash
composer install --no-dev --optimize-autoloader
# 或
composer dump-autoload -o           # -o = --optimize
composer dump-autoload -a           # -a = --classmap-authoritative
#                                    ↑ 生成后**只信 classmap**，不 fallback
```

**生产强烈推荐 `-o` 或 `-a`**，autoload 速度提升 3-5x。

### 常用命令

```bash
composer install                    # 按 lock 装
composer update                     # 重新求解
composer update vendor/pkg          # 只更某个包
composer require vendor/pkg:^1.0    # 添加依赖
composer remove vendor/pkg
composer show vendor/pkg            # 查包详情
composer show --tree                # 依赖树
composer why vendor/pkg             # 谁在依赖它
composer why-not vendor/pkg:^2      # 为什么不能升级到 v2
composer outdated                   # 有哪些包过期
composer audit                      # 安全审计
composer diagnose                   # 诊断本地环境
```

---

## 十二、PHP 8 / 8.1 / 8.2 / 8.3 新特性

### PHP 8.0（2020）

**里程碑版本**。

- **JIT 编译器**
- **命名参数**：`str_replace(subject: $s, search: 'a', replace: 'b')`
- **构造器属性提升**：

  ```php
  class User {
      public function __construct(
          public string $name,       // ★ 参数 + 属性 + 赋值 一步到位
          public int $age,
      ) {}
  }
  ```
- **match 表达式**：strict 比较 + 返回值
- **联合类型**：`function f(int|string $x)`
- **nullsafe 操作符**：`$user?->address?->city`
- **Attributes（原生注解）**：`#[Route('/users')]`
- **throw 表达式**：`$x = $arg ?? throw new InvalidArgumentException;`
- **`str_contains` / `str_starts_with` / `str_ends_with`**（终于原生了）
- **弱比较严格化**：`0 == "a"` 现在是 **false**（PHP 7 是 true）

### PHP 8.1（2021）

- **Enum**：真正的枚举类型

  ```php
  enum Status: string {
      case Active = 'active';
      case Inactive = 'inactive';

      public function label(): string {
          return match($this) {
              Status::Active => '生效中',
              Status::Inactive => '已停用',
          };
      }
  }
  ```
- **readonly 属性**：`public readonly string $name;`
- **First-class 可调用语法**：`$fn = strlen(...);`
- **Fiber**（协程原语）
- **never 返回类型**：函数必然抛异常或退出
- **Intersection Types**：`Foo&Bar`

### PHP 8.2（2022）

- **readonly class**：整个类所有属性都 readonly
- **Disjunctive Normal Form Types**（DNF）：`(A&B)|C`
- **null / false / true 独立类型**
- **Dynamic Property 废弃**：不能给未声明的属性赋值（用 `#[AllowDynamicProperties]` 绕过）
- **Random 扩展**：新的伪随机 API
- **`SensitiveParameter` attribute**：栈跟踪里不显示敏感参数

### PHP 8.3（2023）

- **`json_validate`** 函数（省内存版 json_decode 校验）
- **Typed class constants**：`const int MAX = 100;`
- **`#[Override]` attribute**：显式标记覆盖父类方法
- **Deep-cloning readonly properties**：readonly 属性可以在 `__clone` 里改
- **`Randomizer::getBytesFromString`**
- **Class constants with dynamic name**：`Foo::{$name}`

### 升级建议

- **PHP 7.4 → 8.0**：主要处理**弱比较严格化**（`0 == "a"` 结果变了）和**必需参数不能跟在可选参数后**
- **8.0 → 8.1**：处理 `readonly` 语义、`never` 类型、被删除的函数（`money_format` 等）
- **8.1 → 8.2**：处理**动态属性废弃**（大量代码需要显式声明属性）
- **8.2 → 8.3**：几乎无痛升级

**推荐节奏**：**LTS 版本跟随 Laravel / 主框架发布节奏**（Laravel 11 要 PHP 8.2+）。

---

## 十三、协程模型对比

三种"并发处理"方式：

### FPM 传统模型

```
一个请求 一个 worker 进程（同步阻塞 IO）
        ↓ 请求完成
   worker 回到空闲池
```

**特点**：
- 简单，无并发问题
- 每 worker 内存独立
- **等 IO 时 worker 空占内存**

### Swoole 协程

```
一个 worker 进程 内跑多个协程
    ↓ 协程 A 遇到 IO（如 MySQL 查询）
    ↓ 挂起协程 A，切换到协程 B
    ↓ 协程 B 也 IO 挂起 → 切协程 C
    ↓ 底层用 epoll 监听所有 fd
    ↓ IO 完成 → 恢复对应协程
```

**特点**：
- 一个 worker 撑几万并发（远超 FPM）
- **写同步代码，享受异步性能**
- IO 必须是 hook 过的（`Co\MySQL`、`Co\Redis`）——原生 mysqli 会阻塞整个 worker
- **进程常驻**，请求间共享内存 → 状态管理要小心

### Fiber（PHP 8.1+ 原生）

```php
<?php

$fiber = new Fiber(function () {
    echo "hello ";
    Fiber::suspend();      // ★ 暂停
    echo "world\n";
});

$fiber->start();           // 输出 "hello "
$fiber->resume();          // 输出 "world\n"
```

**特点**：
- **原生**支持，不需要扩展
- **只是原语**——本身不是"协程调度器"，需要框架/库把 Fiber 变成真正的协程系统
- 目前用 Fiber 的：Amp 3、ReactPHP、Guzzle 的异步实现

### 对比表

| 维度 | FPM | Swoole | Fiber |
|-----|-----|--------|-------|
| 并发单元 | 进程 | 协程 | 纤程（原语） |
| IO | 阻塞 | Hook 过的非阻塞 | 用户手动切换 |
| 内存共享 | 无 | 有（每 worker） | 有 |
| 学习成本 | 低 | 中 | 高 |
| 生态 | 完善 | 需要 Hyperf 等框架 | 早期，需要 Amp/ReactPHP |
| 典型 QPS | 千级 | 万级 | 万级 |

---

## 十四、常用扩展速查

| 扩展 | 用途 | 生产必装 |
|-----|-----|---------|
| **opcache** | OPCode 缓存 | ✅ |
| **pdo_mysql** | 数据库 | ✅ |
| **mbstring** | 多字节字符串 | ✅ |
| **openssl** | TLS / 加密 | ✅ |
| **curl** | HTTP 客户端 | ✅ |
| **json** | (PHP 8+ 已内置) | ✅ |
| **redis** / **igbinary** | Redis 客户端 + 二进制序列化 | 视场景 |
| **apcu** | 用户态本地缓存 | 视场景 |
| **swoole** | 协程 / 常驻服务 | 常驻场景 |
| **xdebug** | 调试 / 覆盖率 | **仅开发环境** |
| **pcov** | 生产友好的覆盖率工具 | 视场景 |
| **imagick** | 图像处理 | 视场景 |
| **bcmath** / **gmp** | 高精度数学 | 视场景 |
| **intl** | 国际化（数字/日期/排序） | i18n 项目 |

**⚠️ Xdebug 别装生产**——即使不用，加载后也会拖慢 3-5 倍。

---

## 十五、性能优化速查

按见效程度和易用度排序：

| # | 优化点 | 见效程度 |
|---|-------|---------|
| 1 | **开 opcache**（`validate_timestamps=0`） | ⭐⭐⭐⭐⭐ |
| 2 | **composer dump-autoload -o** | ⭐⭐⭐⭐ |
| 3 | **Laravel `route:cache` + `config:cache`** | ⭐⭐⭐⭐ |
| 4 | **修复 N+1 查询**（`with()` eager loading） | ⭐⭐⭐⭐ |
| 5 | **前端资源走 CDN**（Nginx 静态直接送） | ⭐⭐⭐⭐ |
| 6 | **数据库索引优化** | ⭐⭐⭐⭐ |
| 7 | **Redis 缓存热数据** | ⭐⭐⭐ |
| 8 | **合理设 pm.max_children** | ⭐⭐⭐ |
| 9 | **JSON 序列化改 Protobuf / MessagePack**（微服务间） | ⭐⭐⭐ |
| 10 | **PDO 用 prepared statement 缓存** | ⭐⭐ |
| 11 | **`preload`（PHP 7.4+）** | ⭐⭐ |
| 12 | **升级到最新稳定 PHP** | ⭐⭐ |

**PHP 版本性能对比**（相同代码相对值，越大越快）：

```
PHP 5.6:  1.0x    (baseline)
PHP 7.0:  1.8x    ★ 里程碑
PHP 7.4:  2.2x
PHP 8.0:  2.5x    (+ JIT 部分场景 3-5x)
PHP 8.3:  2.8x
```

单纯升 PHP 就有可观提升——**能升就升**。

---

## 相关文档

- [interview-answers.md](./interview-answers.md) — 28 道高频问答（本文机制的具体考点）
- [scenarios.md](./scenarios.md) — 10+ 生产场景实战
- [../infrastructure/proxy/core-knowledge.md](../infrastructure/proxy/core-knowledge.md) — Nginx 侧的 FastCGI 配置
- [../databases/mysql/core-knowledge.md](../databases/mysql/core-knowledge.md) — MySQL 深度
