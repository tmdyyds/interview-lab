# Nginx 核心知识全景

**标签**: #nginx #reverse-proxy #load-balance #core #高频

系统化梳理 Nginx 核心知识——**架构与事件模型 → 配置结构 → 请求生命周期 → location 匹配 → 反向代理 → 负载均衡 → 限流缓存 → 日志调优**。配套 [interview-and-scenarios.md](./interview-and-scenarios.md) 使用：本文讲**原理与语法**，问答讲**面试题与场景**。

**所有配置示例都可直接跑**，关键指令逐行注释。

---

## 目录

1. [Nginx 整体架构](#一nginx-整体架构)
2. [Master-Worker 与事件模型](#二master-worker-与事件模型)
3. [配置文件结构](#三配置文件结构)
4. [请求处理 11 阶段](#四请求处理-11-阶段)
5. [server 与 server_name 匹配](#五server-与-server_name-匹配)
6. [location 匹配规则](#六location-匹配规则)
7. [反向代理与 proxy_pass](#七反向代理与-proxy_pass)
8. [upstream 与负载均衡](#八upstream-与负载均衡)
9. [静态资源与 gzip](#九静态资源与-gzip)
10. [缓存 proxy_cache](#十缓存-proxy_cache)
11. [限流 limit_req / limit_conn](#十一限流-limit_req--limit_conn)
12. [变量与 rewrite](#十二变量与-rewrite)
13. [日志与监控](#十三日志与监控)
14. [性能调优参数](#十四性能调优参数)
15. [优雅停止与热重载](#十五优雅停止与热重载)
16. [OpenResty 与 Lua](#十六openresty-与-lua)

---

## 一、Nginx 整体架构

**一句话**：Nginx 是**多进程 + 事件驱动 + 非阻塞 IO** 的 HTTP/反向代理服务器，靠 epoll 单线程处理万级并发。

### 三大定位

| 定位 | 用途 |
|-----|------|
| **HTTP Server** | 静态资源、SSL 卸载、gzip |
| **反向代理** | 转发到后端（HTTP / gRPC / TCP / UDP） |
| **负载均衡** | upstream 多种调度算法 |

### 竞品对比（面试常问）

| 维度 | Nginx | Apache | HAProxy |
|-----|-------|--------|---------|
| 模型 | 事件驱动多进程 | 多进程/多线程 prefork/worker | 事件驱动单进程 |
| 静态资源 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ❌ |
| 反向代理 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| 负载均衡 | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| 连接数（10K+） | 优秀 | 差 | 优秀 |
| 配置复杂度 | 中 | 高 | 低 |

**记忆锚点**：**Nginx = 静态资源 + 反向代理**；**HAProxy = 更专业的 L4/L7 均衡**；**Apache = 老牌全能**。

---

## 二、Master-Worker 与事件模型

### 进程结构

```
              ┌────────────┐
              │   Master   │  ← root 启动，管理 worker
              └─────┬──────┘
                    │ 信号 (SIGHUP / SIGUSR2 ...)
        ┌───────────┼───────────┐
        │           │           │
    ┌───▼───┐  ┌───▼───┐  ┌───▼───┐
    │Worker │  │Worker │  │Worker │  ← 通常 = CPU 核数
    └───┬───┘  └───┬───┘  └───┬───┘
        │          │          │
     epoll       epoll      epoll   ← 每个 worker 独立事件循环
```

### 分工

**Master 进程**：
- 读取和解析配置文件
- 启动 worker、监控 worker 存活
- 处理管理信号（重载、优雅退出、日志切割）
- **不处理任何用户请求**

**Worker 进程**：
- 接受客户端连接（accept）
- 处理请求、转发上游、返回响应
- 每个 worker 一个 event loop，用 epoll 监听事件

### 为什么用多进程而不是多线程

面试高频题：

- **稳定性**：一个 worker 崩不影响其他（多线程一崩全崩）
- **无锁**：worker 之间**几乎无共享数据**，不需要锁（多线程共享内存要加锁）
- **利用多核**：CPU 亲和绑定（`worker_cpu_affinity`）
- **热重载**：新老 worker 可以共存过渡（详见第十五节）

### 事件模型：epoll 是核心

**传统 select/poll 的问题**：
- 每次调用都要**把 fd 集合从用户态拷贝到内核态**
- 每次返回都要**遍历所有 fd 找就绪的**
- 支持的 fd 数有限（1024）

**epoll 的三个特点**：
1. **红黑树管理 fd**：注册一次，无需每次传参
2. **就绪链表**：只返回真正有事件的 fd
3. **无 fd 数量上限**（受系统资源限制）

Nginx 用 epoll 实现单个 worker 处理**万级连接**。

### 惊群问题与解决

**惊群（Thundering Herd）**：多个 worker 都 accept 同一个 listen socket，一个连接到来时**所有 worker 都被唤醒**，但只有一个能成功接受。

Nginx 的解决方案：

```nginx
events {
    accept_mutex on;    # ★ 默认开启，一次只让一个 worker accept
    #             ↑ 早期版本必需，现代内核（Linux 4.5+）+ SO_REUSEPORT 可以关掉
}
```

**Linux 3.9+ 引入 `SO_REUSEPORT`**：多个 socket 可以绑同一端口，内核自动做负载均衡：

```nginx
http {
    server {
        listen 80 reuseport;    # ★ 内核层面直接分发到不同 worker
        #        ↑ 现代推荐用法
    }
}
```

开 reuseport 后可关掉 accept_mutex，性能更好。

---

## 三、配置文件结构

Nginx 配置是**层级嵌套**的上下文（context）：

```nginx
# ===== main 上下文（顶层） =====
user  nginx;                        # worker 运行用户（推荐非 root）
worker_processes  auto;             # ★ worker 数量，auto = CPU 核数
worker_rlimit_nofile 65535;         # 每个 worker 能打开的 fd 上限

# ===== events 上下文 =====
events {
    worker_connections  10240;      # ★ 每个 worker 能处理的最大连接数
    #                    ↑ 总上限 = worker_processes × worker_connections
    use epoll;                      # Linux 上默认，其他平台 kqueue/eventport
    multi_accept on;                # 一次事件循环尽可能多 accept 新连接
}

# ===== http 上下文 =====
http {
    include       mime.types;       # 引入 MIME 类型映射
    default_type  application/octet-stream;

    sendfile      on;               # ★ 零拷贝发送文件（内核态直接发，不过用户态）
    tcp_nopush    on;               # 配合 sendfile，合并小包一起发（响应头 + body）
    tcp_nodelay   on;               # 长连接下禁用 Nagle，低延迟
    keepalive_timeout  65;          # 长连接空闲超时

    gzip on;
    gzip_types text/plain application/json;

    # ===== upstream 上下文（可选） =====
    upstream backend {
        server 10.0.0.1:8080;
        server 10.0.0.2:8080;
    }

    # ===== server 上下文 =====
    server {
        listen       80;
        server_name  example.com;

        # ===== location 上下文 =====
        location / {
            proxy_pass http://backend;
        }

        location /static/ {
            root /var/www;          # 文件根路径
        }
    }

    server {
        listen 443 ssl;
        server_name api.example.com;
        # ...
    }
}
```

### 指令继承规则

**内层继承外层**，可以在内层覆盖：

```nginx
http {
    gzip on;                # http 级别开
    server {
        gzip off;           # 这个 server 关掉
        location /api {
            gzip on;        # 但 /api 又开
        }
    }
}
```

**部分指令不继承**（比如 `add_header`）——加了新的会**替换**外层，不是追加。

---

## 四、请求处理 11 阶段

Nginx 把请求处理分成 **11 个阶段**（phase），模块可挂在某阶段处理请求。**面试常问**。

```
1. NGX_HTTP_POST_READ_PHASE       读取请求头后立刻执行（realip 模块）
2. NGX_HTTP_SERVER_REWRITE_PHASE  server 级别的 rewrite
3. NGX_HTTP_FIND_CONFIG_PHASE     找到对应 location
4. NGX_HTTP_REWRITE_PHASE         location 级别的 rewrite
5. NGX_HTTP_POST_REWRITE_PHASE    rewrite 后处理（防死循环）
6. NGX_HTTP_PREACCESS_PHASE       访问前（limit_req、limit_conn）
7. NGX_HTTP_ACCESS_PHASE          访问控制（allow、deny、auth_basic、auth_request）
8. NGX_HTTP_POST_ACCESS_PHASE     访问后处理
9. NGX_HTTP_PRECONTENT_PHASE      内容前（try_files、mirror）
10. NGX_HTTP_CONTENT_PHASE        ★ 生成响应（proxy_pass、root、return、fastcgi_pass）
11. NGX_HTTP_LOG_PHASE            写日志
```

**用途**：
- 排查请求处理顺序时按阶段推理
- 开发 Nginx 模块要知道挂在哪个阶段
- 理解为什么 `limit_req` 在 `access_log` 之前生效（阶段 6 vs 11）

---

## 五、server 与 server_name 匹配

一个 `listen` 端口下可以有**多个 server 块**，Nginx 根据 `Host` 头选中哪个 server（**虚拟主机**）。

### 匹配优先级

```
1. 精确匹配         server_name example.com;
2. 通配符前缀       server_name *.example.com;
3. 通配符后缀       server_name example.*;
4. 正则匹配         server_name ~^www\d+\.example\.com$;
5. default_server   listen 80 default_server;   ← 都不匹配走这个
```

**示例**：

```nginx
# API 服务
server {
    listen 80;
    server_name api.example.com;   # 精确匹配
    location / { proxy_pass http://backend_api; }
}

# 所有二级域名
server {
    listen 80;
    server_name *.example.com;     # 通配符
    location / { proxy_pass http://backend_general; }
}

# 兜底（所有未匹配请求）
server {
    listen 80 default_server;
    server_name _;                 # 惯例写法，表示 "不匹配任何名字"
    return 444;                    # ★ 特殊返回码，直接关闭连接（防扫描）
}
```

**面试考点**：`default_server` 处理 IP 直连（`http://1.2.3.4/`）和 Host 头未匹配的请求。

---

## 六、location 匹配规则

**最高频面试点**。语法：

```nginx
location [修饰符] pattern {
    ...
}
```

### 五种修饰符

| 修饰符 | 类型 | 示例 |
|-------|------|------|
| `=` | **精确匹配** | `location = /login { }` |
| `^~` | **前缀匹配（找到就停）** | `location ^~ /static/ { }` |
| `~` | **正则（大小写敏感）** | `location ~ \.php$ { }` |
| `~*` | **正则（大小写不敏感）** | `location ~* \.(jpg\|png)$ { }` |
| 无 | **前缀匹配（继续找正则）** | `location /api/ { }` |

### 匹配优先级（关键流程）

```
Step 1. 找精确匹配 (=)         → 命中直接用
   ↓ 未命中
Step 2. 找 ^~ 前缀             → 命中最长的直接用
   ↓ 未命中
Step 3. 找普通前缀（无修饰符）  → 记住最长的一个（暂不用）
   ↓
Step 4. 按配置文件顺序找正则    → 第一个命中就用
   ↓ 都没命中
Step 5. 用 Step 3 记住的普通前缀
```

**记忆口诀**：`= > ^~ > 正则（按顺序） > 前缀`。

### 完整示例带注释

```nginx
server {
    listen 80;
    server_name example.com;

    # 优先级 1：精确匹配 /
    location = / {
        return 200 "home page";      # 只匹配 http://example.com/
    }

    # 优先级 2：^~ 前缀（不再走正则）
    location ^~ /static/ {
        alias /var/www/static/;       # ★ alias 替换路径；root 拼接路径
        # 例：/static/logo.png → /var/www/static/logo.png
        expires 30d;                  # 缓存 30 天
    }

    # 优先级 3：正则（按顺序匹配）
    location ~* \.(jpg|jpeg|png|gif|webp)$ {
        # 所有图片请求走这里，前提是没被 ^~ 拦截
        expires 7d;
        access_log off;               # 图片访问不记日志（省磁盘）
    }

    location ~ \.php$ {
        fastcgi_pass 127.0.0.1:9000;
        include fastcgi_params;
    }

    # 优先级 4：普通前缀（如果没正则命中）
    location /api/ {
        proxy_pass http://backend/;   # ★ 注意末尾 /
    }

    # 兜底
    location / {
        try_files $uri $uri/ /index.html;
        # ★ SPA 常用：先找文件，找不到返回 index.html 让前端路由处理
    }
}
```

### root vs alias（易错点）

```nginx
# 请求 /static/logo.png

# root：路径拼接
location /static/ {
    root /var/www;
    # 最终查找：/var/www + /static/logo.png = /var/www/static/logo.png
}

# alias：路径替换
location /static/ {
    alias /var/www/assets/;
    # 最终查找：/var/www/assets/ + logo.png = /var/www/assets/logo.png
}
```

**规则**：
- `root` 拼接（保留 URI 部分）
- `alias` 替换（把 location 部分**换成** alias 的值）
- **alias 结尾必须有 `/`**（配对 location 结尾的 `/`）

---

## 七、反向代理与 proxy_pass

```nginx
server {
    listen 80;
    server_name example.com;

    location /api/ {
        # ★ 转发请求到后端服务
        proxy_pass http://10.0.0.5:8080;
        #           ↑ 后端地址

        # ★ 传递客户端真实信息给后端（必配）
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        # $host             — 客户端请求的 Host
        # $remote_addr      — 客户端真实 IP
        # $proxy_add_...    — 追加当前 IP 到 X-Forwarded-For 链
        # $scheme           — http 或 https

        # ★ 超时（生产必配，默认 60s 通常太长）
        proxy_connect_timeout 5s;     # 建立连接超时
        proxy_send_timeout    30s;    # 发送请求给后端超时
        proxy_read_timeout    60s;    # 等后端响应超时

        # ★ 缓冲：小响应内存缓冲，大响应写临时文件
        proxy_buffering       on;
        proxy_buffer_size     4k;     # 响应头缓冲
        proxy_buffers         8 16k;  # 响应 body 缓冲：8 个 16KB
        proxy_busy_buffers_size 32k;
    }
}
```

### proxy_pass 的 URI 陷阱（面试高频）

**规则**：`proxy_pass` 后带不带 `/` 决定 URI 怎么拼。

```nginx
# 请求 http://example.com/api/user/1

# 情况 1：proxy_pass 没有 URI（不带路径）
location /api/ {
    proxy_pass http://backend;
    # 转发到：http://backend/api/user/1     ← 完整保留
}

# 情况 2：proxy_pass 有 URI（带 /）
location /api/ {
    proxy_pass http://backend/;
    # 转发到：http://backend/user/1        ← /api/ 被去掉
}

# 情况 3：proxy_pass 有 URI（带具体路径）
location /api/ {
    proxy_pass http://backend/v2/;
    # 转发到：http://backend/v2/user/1
}
```

**记忆**：**proxy_pass 有 URI（哪怕只是 `/`）→ 替换掉 location 部分**。

### 反向代理示例：Go 服务

```nginx
upstream go_backend {
    server 127.0.0.1:8080;
    keepalive 32;                     # ★ 复用连接（默认没有 keepalive）
}

server {
    listen 80;
    server_name api.example.com;

    location / {
        proxy_pass http://go_backend;

        # HTTP/1.1 长连接必须显式开
        proxy_http_version 1.1;
        proxy_set_header   Connection "";    # ★ 清掉 close，才能 keepalive

        # 头部
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;

        # 超时
        proxy_connect_timeout 3s;
        proxy_read_timeout    60s;
    }
}
```

---

## 八、upstream 与负载均衡

```nginx
upstream backend {
    # ★ 默认算法：轮询（round-robin）
    server 10.0.0.1:8080;
    server 10.0.0.2:8080;
    server 10.0.0.3:8080;
}
```

### 五种负载均衡算法

#### 1) 轮询（默认）

依次分发。**适合无状态、后端等价**的服务。

```nginx
upstream backend {
    server 10.0.0.1:8080;
    server 10.0.0.2:8080;
}
```

#### 2) 加权轮询（weight）

按权重分配。适合**后端配置不均**：

```nginx
upstream backend {
    server 10.0.0.1:8080 weight=3;    # 3/6 的流量
    server 10.0.0.2:8080 weight=2;    # 2/6
    server 10.0.0.3:8080 weight=1;    # 1/6
}
```

#### 3) ip_hash（基于客户端 IP）

同一 IP 总是打到同一后端。**session 保持**（但不推荐——把状态做无状态才是正道）。

```nginx
upstream backend {
    ip_hash;
    server 10.0.0.1:8080;
    server 10.0.0.2:8080;
}
```

**缺点**：不平衡（用户 IP 分布不均、NAT 后 IP 相同）。

#### 4) least_conn（最少连接）

新请求发给**当前连接数最少**的后端。适合**请求处理时长差异大**（如混合快请求和慢查询）。

```nginx
upstream backend {
    least_conn;
    server 10.0.0.1:8080;
    server 10.0.0.2:8080;
}
```

#### 5) hash（自定义 key）

按变量做一致性哈希：

```nginx
upstream backend {
    hash $request_uri consistent;     # ★ consistent 一致性哈希（增减节点影响小）
    #    ↑ 也可以是 $arg_userid 等自定义
    server 10.0.0.1:8080;
    server 10.0.0.2:8080;
}
```

**用途**：让相同 URI 落到同一后端，**提高缓存命中率**（缓存服务器场景）。

### server 参数

```nginx
upstream backend {
    server 10.0.0.1:8080 weight=3 max_fails=2 fail_timeout=30s;
    #                    ↑ 权重  ↑ 失败次数阈值  ↑ 熔断时长

    server 10.0.0.2:8080 backup;          # ★ 备用，主全挂时才启用
    server 10.0.0.3:8080 down;            # ★ 手动标记下线（灰度/维护用）

    # 长连接池
    keepalive 32;                          # 保留 32 个空闲连接给下次复用
    keepalive_timeout 60s;
    keepalive_requests 100;                # 单连接最多复用 100 个请求
}
```

**熔断逻辑**：`max_fails=2 fail_timeout=30s` = "**30 秒内失败 2 次就标记为不可用 30 秒**"，之后自动重试。

### 健康检查

**开源版 Nginx 只有被动健康检查**（`max_fails`）。主动健康检查要用：
- Nginx Plus（商业版）
- OpenResty + `lua-resty-upstream-healthcheck`
- Tengine（阿里）的 `check` 指令

---

## 九、静态资源与 gzip

```nginx
server {
    listen 80;
    server_name static.example.com;
    root /var/www/static;

    # 静态资源缓存
    location ~* \.(js|css)$ {
        expires 7d;                                          # 7 天缓存
        add_header Cache-Control "public, immutable";        # 强缓存
    }

    location ~* \.(jpg|jpeg|png|gif|webp|svg|ico)$ {
        expires 30d;
        add_header Cache-Control "public";
        access_log off;                                      # 图片不记日志
    }

    location ~* \.(woff|woff2|ttf|eot)$ {
        expires 30d;
        add_header Access-Control-Allow-Origin *;            # 字体跨域
    }

    # HTML 不缓存（每次都拿最新）
    location ~* \.html$ {
        expires -1;
        add_header Cache-Control "no-cache, must-revalidate";
    }
}
```

### gzip 压缩

```nginx
http {
    gzip on;

    gzip_vary on;                       # ★ 加 Vary: Accept-Encoding 头（CDN 需要）
    gzip_min_length 1024;               # 小于 1KB 不压缩（收益低）
    gzip_comp_level 6;                  # ★ 压缩级别 1-9，6 是速度/比率平衡点
    gzip_buffers 16 8k;
    gzip_http_version 1.1;

    # ★ 哪些 MIME 类型压缩（图片/视频本身已压缩，别加）
    gzip_types
        text/plain
        text/css
        text/xml
        application/javascript
        application/json
        application/xml
        application/xml+rss
        image/svg+xml;

    # 代理请求也压缩（默认不压缩带 Via 头的）
    gzip_proxied any;

    # 不压缩老 IE（有 bug）
    gzip_disable "MSIE [1-6]\.";
}
```

**追问：为什么图片/视频不压缩？** 已经是压缩格式（JPEG/H.264），再 gzip 反而变大 + 浪费 CPU。

### brotli（更好的压缩）

Google 出的更先进算法，压缩率比 gzip 高 15-25%。需要 `ngx_brotli` 模块：

```nginx
brotli on;
brotli_comp_level 6;
brotli_types text/plain text/css application/json application/javascript;
```

**兼容性**：主流浏览器都支持，客户端不支持时自动降级 gzip。

---

## 十、缓存 proxy_cache

Nginx 作为**反向代理缓存**：把后端响应缓存到本地，减轻后端压力。

```nginx
http {
    # 定义缓存区
    proxy_cache_path /var/cache/nginx
                     levels=1:2                     # ★ 两级目录（避免单目录文件过多）
                     keys_zone=my_cache:100m        # 元数据内存区 100MB（约存 80 万 key）
                     max_size=10g                   # ★ 磁盘总量上限 10GB
                     inactive=60m                   # 60 分钟未访问的清理
                     use_temp_path=off;             # 临时文件直接写缓存目录（避免跨设备拷贝）

    server {
        location / {
            proxy_pass http://backend;

            proxy_cache my_cache;                       # ★ 用哪个缓存区
            proxy_cache_key "$scheme$host$request_uri"; # ★ 缓存 key
            proxy_cache_valid 200 302 10m;              # 200/302 缓存 10 分钟
            proxy_cache_valid 404 1m;                   # 404 缓存 1 分钟（防击穿）
            proxy_cache_valid any 1m;

            # ★ 什么情况绕过缓存
            proxy_cache_bypass $arg_nocache $http_pragma;
            # 例：http://x.com/?nocache=1 会绕过缓存

            # ★ 只有 GET/HEAD 可缓存（默认）
            proxy_cache_methods GET HEAD;

            # ★ 高并发下同一 key 只有一个请求打到后端（防雪崩）
            proxy_cache_lock on;
            proxy_cache_lock_timeout 5s;

            # ★ 后端出错时使用旧缓存
            proxy_cache_use_stale error timeout http_500 http_502 http_503;

            # 加个响应头方便调试
            add_header X-Cache-Status $upstream_cache_status;
            # 值：HIT / MISS / EXPIRED / STALE / BYPASS
        }
    }
}
```

### 缓存清除

**主动清缓存**（开源版不支持，商业版或第三方模块 `ngx_cache_purge`）：

```nginx
location ~ /purge(/.*) {
    allow 127.0.0.1;
    deny all;
    proxy_cache_purge my_cache "$scheme$host$1";
}
```

**开源替代方案**：改 `proxy_cache_key`（比如加个版本号），旧 key 自然过期。

---

## 十一、限流 limit_req / limit_conn

### 请求速率限流：limit_req（漏桶）

```nginx
http {
    # ★ 定义限流区
    limit_req_zone $binary_remote_addr zone=api_limit:10m rate=10r/s;
    #              ↑ 按客户端 IP 限流（binary 版更省内存）
    #                                    ↑ 区大小       ↑ 10 请求/秒

    server {
        location /api/ {
            limit_req zone=api_limit burst=20 nodelay;
            #              ↑ 突发容量 20   ↑ 突发部分不排队等，直接过（但会消耗令牌）

            # 超限时返回 429
            limit_req_status 429;
            #                ↑ 默认是 503，改成 429 更符合 HTTP 语义

            proxy_pass http://backend;
        }
    }
}
```

**参数详解**：

- `rate=10r/s` — 平均速率：**每秒 10 个请求**
- `burst=20` — 突发容量：允许积累 20 个请求排队
- `nodelay` — 突发部分**立即处理**（不排队），但仍消耗令牌

**行为示例**（rate=10r/s，burst=20，nodelay）：

```
第 0.0 秒：来 30 个请求
  → 10 个正常放行
  → 20 个用 burst 令牌立即放行
  → 剩余 0 个（实际这里假设正好 30）

第 0.0 - 3.0 秒：期间不再有请求，令牌以 10/s 恢复

第 3.0 秒：又来 5 个请求
  → 5 个都正常放行（此时又有 30 令牌）
```

如果**不加 nodelay**：突发的 20 会被"延迟"到符合 rate 的节奏（10/s），第 30 个请求会等 2 秒。

### 连接数限流：limit_conn

```nginx
http {
    limit_conn_zone $binary_remote_addr zone=conn_limit:10m;
    #               ↑ 按 IP 统计并发连接数

    server {
        location / {
            limit_conn conn_limit 10;
            #                     ↑ 每个 IP 最多 10 个并发连接
            limit_conn_status 429;
            proxy_pass http://backend;
        }
    }
}
```

**用途**：防止单 IP 建立过多并发（下载盗链、慢速攻击）。

### 常见限流组合

```nginx
# 组合限流：既限速率又限并发
limit_req_zone  $binary_remote_addr zone=api_rate:10m rate=100r/s;
limit_conn_zone $binary_remote_addr zone=api_conn:10m;

server {
    location /api/ {
        limit_req  zone=api_rate  burst=200 nodelay;
        limit_conn conn_limit 50;

        proxy_pass http://backend;
    }
}
```

---

## 十二、变量与 rewrite

### 常用内置变量

| 变量 | 含义 |
|------|-----|
| `$host` | 请求 Host 头 |
| `$remote_addr` | 客户端 IP |
| `$remote_port` | 客户端端口 |
| `$request` | 完整请求行（GET /a HTTP/1.1） |
| `$request_uri` | 原始 URI（含 query） |
| `$uri` | 解码后的 URI（不含 query） |
| `$args` / `$query_string` | 查询字符串 |
| `$arg_xxx` | 某个查询参数值（`$arg_userid`） |
| `$http_xxx` | 某个请求头（`$http_user_agent`） |
| `$scheme` | http 或 https |
| `$server_name` | 匹配的 server_name |
| `$body_bytes_sent` | 响应 body 字节数 |
| `$request_time` | 请求处理总耗时 |
| `$upstream_response_time` | 后端响应耗时 |
| `$upstream_addr` | 转发到的后端地址 |
| `$upstream_status` | 后端返回码 |
| `$upstream_cache_status` | 缓存命中状态 |

### rewrite / return / if

```nginx
server {
    listen 80;
    server_name example.com;

    # ★ return：直接返回（推荐，比 rewrite 高效）
    location = /old {
        return 301 /new;
    }

    # ★ 强制 HTTPS
    if ($scheme = http) {
        return 301 https://$host$request_uri;
    }

    # ★ rewrite：改写 URI 后继续处理
    location /article/ {
        rewrite ^/article/(\d+)/?$ /post.php?id=$1 last;
        #        ↑ 正则                        ↑ 目标   ↑ 标志
    }
}
```

**rewrite 标志**：

| 标志 | 含义 |
|-----|------|
| `last` | 改写后**重新匹配 location** |
| `break` | 改写后**在当前 location 继续**（不重匹配） |
| `redirect` | 302 临时重定向 |
| `permanent` | 301 永久重定向 |

### `if` 的陷阱

Nginx 官方 Wiki 有一篇 [If is Evil](https://www.nginx.com/resources/wiki/start/topics/depth/ifisevil/)——`if` 在 `location` 上下文里**几乎必然踩坑**。

```nginx
# ❌ 错误：在 location 里的 if 逻辑经常有意外行为
location /api/ {
    if ($request_method = POST) {
        proxy_pass http://post_backend;
    }
    proxy_pass http://get_backend;   # 可能永远走不到，也可能被跳过
}

# ✅ 推荐：用 map 或 limit_except
map $request_method $backend {
    POST post_backend;
    default get_backend;
}
location /api/ {
    proxy_pass http://$backend;
}
```

**记忆**：`if` 只在 `server` 上下文里放心用，`location` 里能避免就避免。

---

## 十三、日志与监控

### access_log 自定义格式

```nginx
http {
    log_format main '$remote_addr - $remote_user [$time_local] '
                    '"$request" $status $body_bytes_sent '
                    '"$http_referer" "$http_user_agent" '
                    'rt=$request_time ut=$upstream_response_time '
                    'uc=$upstream_cache_status ua=$upstream_addr';
    #                     ↑ 请求总时间     ↑ 后端时间          ↑ 缓存状态

    access_log /var/log/nginx/access.log main;

    # JSON 格式（更易被 ELK 消费）
    log_format json escape=json '{'
        '"time":"$time_iso8601",'
        '"remote_addr":"$remote_addr",'
        '"method":"$request_method",'
        '"uri":"$request_uri",'
        '"status":$status,'
        '"body_bytes":$body_bytes_sent,'
        '"referer":"$http_referer",'
        '"ua":"$http_user_agent",'
        '"rt":$request_time,'
        '"ut":"$upstream_response_time",'
        '"cache":"$upstream_cache_status"'
        '}';
    access_log /var/log/nginx/access.json json;
}
```

### 日志切割（logrotate）

```
# /etc/logrotate.d/nginx
/var/log/nginx/*.log {
    daily                          # 每天切
    rotate 30                      # 保留 30 天
    compress                       # 老日志 gzip 压缩
    delaycompress                  # 保留一天不压缩（便于查看）
    missingok
    notifempty
    create 0640 nginx adm
    sharedscripts
    postrotate
        # ★ 重开日志文件（不重启 nginx）
        [ -f /var/run/nginx.pid ] && kill -USR1 `cat /var/run/nginx.pid`
    endscript
}
```

**关键**：`kill -USR1` 让 nginx 重开日志文件，是最优雅的切割方式。

### error_log 级别

```nginx
error_log /var/log/nginx/error.log warn;
#                                   ↑ debug|info|notice|warn|error|crit|alert|emerg
```

生产用 `warn` 或 `error`。排查具体请求用 `debug`（要求 nginx 编译时 `--with-debug`）。

### stub_status 监控

```nginx
server {
    listen 80;

    location /nginx_status {
        stub_status on;
        access_log off;

        # 只允许内网访问
        allow 10.0.0.0/8;
        allow 127.0.0.1;
        deny all;
    }
}
```

访问 `http://your-nginx/nginx_status`：

```
Active connections: 291
server accepts handled requests
 16630948 16630948 31070465
Reading: 6 Writing: 179 Waiting: 106
```

**字段含义**：
- `Active` — 当前活跃连接（含 waiting）
- `accepts` — 累计已接收连接数
- `handled` — 累计已处理（一般 = accepts；不等说明有连接被 worker 拒绝）
- `requests` — 累计请求数（keep-alive 一次连接多个请求）
- `Reading` — 正在读请求头的连接
- `Writing` — 正在返回响应的连接
- `Waiting` — keep-alive 空闲连接

配合 Prometheus 用 `nginx-prometheus-exporter` 采集。

---

## 十四、性能调优参数

按重要度排序：

```nginx
# ===== 主进程 =====
worker_processes auto;                # ★ auto = CPU 核数
worker_cpu_affinity auto;             # ★ CPU 亲和（每个 worker 绑一核）
worker_rlimit_nofile 65535;           # 打开文件描述符上限

# ===== 事件模型 =====
events {
    worker_connections 65535;         # ★ 单 worker 最大连接数
    use epoll;
    multi_accept on;                  # 一次 accept 尽量多的新连接
    accept_mutex off;                 # 配合 reuseport 关掉
}

http {
    # ===== 传输 =====
    sendfile on;                      # ★ 零拷贝
    sendfile_max_chunk 512k;          # 单次 sendfile 上限（避免单个大文件占满）
    tcp_nopush on;                    # ★ 合并小包发送
    tcp_nodelay on;                   # ★ 长连接下禁 Nagle

    # ===== 长连接 =====
    keepalive_timeout 65s;            # 客户端长连接超时
    keepalive_requests 1000;          # 单连接最多处理请求数（老版本默认 100）

    # ===== 客户端限制 =====
    client_max_body_size 100m;        # ★ 上传文件大小上限（重要！默认 1m）
    client_body_buffer_size 128k;
    client_body_timeout 30s;
    client_header_timeout 30s;
    client_header_buffer_size 1k;
    large_client_header_buffers 4 8k;

    # ===== 后端连接 =====
    proxy_connect_timeout 5s;
    proxy_send_timeout 30s;
    proxy_read_timeout 60s;

    # ===== 后端 keepalive =====
    # 在 upstream 块里配 keepalive 32；

    # ===== 缓冲区 =====
    proxy_buffer_size 4k;
    proxy_buffers 8 16k;
    proxy_busy_buffers_size 32k;

    # ===== 隐藏版本号（安全）=====
    server_tokens off;                # ★ 响应头不显示 nginx 版本
}
```

### 系统层（kernel）配合

```bash
# /etc/sysctl.conf
net.core.somaxconn = 65535               # listen backlog（默认 128 太小）
net.ipv4.tcp_max_syn_backlog = 65535
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1                # 复用 TIME-WAIT
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216

# 文件描述符
* soft nofile 65535
* hard nofile 65535
```

`sysctl -p` 让配置生效。

---

## 十五、优雅停止与热重载

Nginx 的**热重载**是它一大特色——**不断开现有连接的情况下切换配置和二进制**。

### 信号语义

| 信号 | 命令 | 作用 |
|-----|------|-----|
| `SIGTERM` / `SIGINT` | `nginx -s stop` | **立即停止**（不等请求处理完）|
| `SIGQUIT` | `nginx -s quit` | **优雅停止**（处理完当前请求再退）|
| `SIGHUP` | `nginx -s reload` | ★ **热重载配置** |
| `SIGUSR1` | `nginx -s reopen` | 重开日志（配合 logrotate）|
| `SIGUSR2` | — | 热升级二进制（换新版本）|
| `SIGWINCH` | — | 老 worker 优雅退出（配合热升级）|

### 热重载原理

```bash
nginx -s reload
```

内部流程：

```
1. Master 收到 SIGHUP
2. Master 重新读配置、验证语法
3. Master 启动新 Worker（用新配置）
4. Master 通知老 Worker "SIGQUIT"（优雅退出）
5. 老 Worker 处理完当前连接后自然退出
6. 新 Worker 接手新连接
```

**关键点**：新老 Worker 并存的短暂时期里，**没有断连**。这就是为什么 Nginx 号称"零停机"。

### 热重载失败

如果新配置语法错误，master 保留老 worker 继续跑，reload 失败但服务不受影响。

**上线前必做**：

```bash
nginx -t         # ★ 语法检查
nginx -T         # 完整打印当前有效配置（含所有 include）
```

### 二进制热升级

```bash
# 1. 保留老 master 的 pid
mv /usr/local/nginx/sbin/nginx /usr/local/nginx/sbin/nginx.old
cp new-nginx /usr/local/nginx/sbin/nginx

# 2. 发 SIGUSR2 给老 master，启动新 master + 新 worker
kill -USR2 `cat /var/run/nginx.pid`

# 3. 通知老 worker 优雅退出
kill -WINCH `cat /var/run/nginx.pid.oldbin`

# 4. 观察新 master 稳定后，杀掉老 master
kill -QUIT `cat /var/run/nginx.pid.oldbin`

# 如果新版本有问题，回滚：
kill -HUP `cat /var/run/nginx.pid.oldbin`     # 老 master 重启 worker
kill -QUIT `cat /var/run/nginx.pid`           # 干掉新 master
```

---

## 十六、OpenResty 与 Lua

**OpenResty** = Nginx + LuaJIT + 一堆 lua-resty-* 模块。让 Nginx **可编程**。

### 典型用途

- 复杂路由（比 nginx map 更灵活）
- 动态限流（基于 Redis 计数）
- API 网关（鉴权、灰度、AB 测试）
- 复杂缓存（多级、条件缓存）
- WAF（Web Application Firewall）

### 简单示例：Lua 鉴权

```nginx
location /api/ {
    access_by_lua_block {
        -- 从 header 拿 token
        local token = ngx.req.get_headers()["Authorization"]
        if not token then
            ngx.exit(401)
        end

        -- 调 Redis 校验
        local redis = require "resty.redis"
        local red = redis:new()
        red:set_timeout(1000)
        red:connect("127.0.0.1", 6379)

        local user_id = red:get("token:" .. token)
        if not user_id or user_id == ngx.null then
            ngx.exit(403)
        end

        -- 把用户 ID 传给上游
        ngx.req.set_header("X-User-Id", user_id)
    }

    proxy_pass http://backend;
}
```

### Lua 阶段挂载点

| 指令 | 阶段 | 用途 |
|-----|------|-----|
| `init_by_lua*` | Master 启动 | 全局初始化 |
| `init_worker_by_lua*` | Worker 启动 | 每 worker 初始化 |
| `set_by_lua*` | rewrite | 动态设置变量 |
| `rewrite_by_lua*` | rewrite | URI 改写 |
| **`access_by_lua*`** | access | ★ 鉴权 |
| **`content_by_lua*`** | content | ★ 完全接管响应 |
| `header_filter_by_lua*` | 响应头 | 改响应头 |
| `body_filter_by_lua*` | 响应体 | 改响应体 |
| `log_by_lua*` | log | 自定义日志 |

---

## 收尾：常问要点速查

| 主题 | 核心结论 |
|-----|---------|
| 为什么高并发 | epoll + 事件驱动 + 多进程 + 非阻塞 IO |
| Master / Worker | Master 管理，Worker 处理请求；不共享数据（几乎无锁） |
| 惊群 | 老版 `accept_mutex`；现代 Linux 用 `SO_REUSEPORT` |
| location 优先级 | `=` > `^~` > 正则（按顺序） > 前缀 |
| root vs alias | root 拼接、alias 替换 |
| proxy_pass 结尾 / 的坑 | 有 URI 就替换 location |
| 负载均衡默认 | round-robin，加权用 `weight` |
| 长连接 | proxy_http_version 1.1 + `Connection ""` + upstream keepalive |
| gzip 图片 | 别开，本身已压缩 |
| limit_req | 漏桶算法，`rate + burst + nodelay` 组合 |
| proxy_cache | `proxy_cache_lock` 防雪崩 + `use_stale` 保底 |
| 热重载 | SIGHUP，新老 worker 共存过渡 |
| if 陷阱 | `location` 里少用，改 `map` |

---

## 相关文档

- [interview-and-scenarios.md](./interview-and-scenarios.md) — 面试题问答 + 生产场景实战
- [../../databases/redis/README.md](../../databases/redis/README.md) — Redis（配合限流/缓存）
- [../linux/troubleshooting.md](../linux/troubleshooting.md) — 系统排查（网络/连接堆积）
