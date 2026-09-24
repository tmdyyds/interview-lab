# Nginx 面试题 + 生产场景实战

**标签**: #nginx #interview #scenarios #proxy #高频

两部分内容：

1. **Part 1**：40 道高频面试题（架构 / 配置 / 性能 / 安全 / 生产）
2. **Part 2**：10 个生产场景实战（灰度 / 限流 / 大文件 / WebSocket / TLS / 跨域 / 反爬 / 动静分离 / 缓存击穿 / 优雅升级）

配套 [core-knowledge.md](./core-knowledge.md) 使用，本文的答案对应到那里的具体章节。

---

## Part 1：面试题

### 一、架构与原理（10 题）

#### Q1: Nginx 为什么能扛高并发？

四个关键点：

1. **事件驱动 + 非阻塞 IO**：一个 worker 用 epoll 监听所有连接的事件，来一个处理一个，不用为每个连接开线程
2. **多进程**：worker 数 = CPU 核数，充分利用多核；进程间几乎无共享数据，无锁
3. **零拷贝**：`sendfile` 让文件直接从内核 pagecache 送到 socket，不经用户态
4. **内存池**：请求级别的内存池管理，避免频繁 malloc/free

**对比 Apache prefork**：一个连接一个进程，1 万并发 = 1 万进程 = 数十 GB 内存 + 频繁上下文切换，扛不住。

#### Q2: Master 和 Worker 的分工？

**Master**（root 权限）：
- 读配置、启动 worker
- 监控 worker 存活（挂了拉起）
- 处理管理信号：`reload` / `stop` / `reopen`
- **不处理任何用户请求**

**Worker**（普通用户）：
- accept 新连接
- 用 epoll 处理请求
- 转发上游、返回响应
- 每个 worker 一个 event loop

**面试要点**：worker 之间**不共享用户请求数据**——这是无锁的基础。共享的是配置和监听 socket。

#### Q3: 为什么 Nginx 用多进程不用多线程？

- **稳定性**：一个 worker 崩不影响其他（多线程一崩全崩）
- **无锁**：进程间几乎无共享，多线程共享内存要加锁
- **热重载**：新老 worker 可以并存过渡，多线程做不到
- **权限隔离**：worker 可以降权跑，master 保留 root（多线程无法降权）

#### Q4: 什么是惊群？Nginx 怎么解决？

**惊群**：多个 worker 都 accept 同一个 listen socket，一个新连接到来时**所有 worker 都被唤醒**，但只有一个能成功接受——其他 worker 白醒了，浪费 CPU。

**Nginx 的两种解法**：

1. **accept_mutex**（老方案）：用互斥锁，一次只让一个 worker 去 accept

    ```nginx
    events {
        accept_mutex on;
    }
    ```

2. **SO_REUSEPORT**（Linux 3.9+，推荐）：**内核层面**做分发，每个 worker 有独立的 accept 队列

    ```nginx
    server {
        listen 80 reuseport;
    }
    ```

现代内核 + reuseport 性能明显好过 mutex。

#### Q5: 请求处理的 11 个阶段是什么？

按顺序：

```
1. POST_READ         → realip 模块拿真实 IP
2. SERVER_REWRITE    → server 级 rewrite
3. FIND_CONFIG       → 找 location
4. REWRITE           → location 级 rewrite
5. POST_REWRITE      → 防死循环检查
6. PREACCESS         → limit_req、limit_conn
7. ACCESS            → allow/deny、auth_basic、auth_request
8. POST_ACCESS
9. PRECONTENT        → try_files、mirror
10. CONTENT          ★ 生成响应（proxy_pass、root、return）
11. LOG              → 写 access_log
```

**面试用法**：讲清楚 `limit_req` 在 access_log 之前生效——所以限流失败的请求也会被记 log，可用于分析被限流的量。

#### Q6: epoll、select、poll 的区别？

| 特性 | select | poll | epoll |
|-----|--------|------|-------|
| fd 上限 | 1024 | 无 | 无（受系统限制） |
| 数据结构 | 数组 | 数组 | 红黑树 |
| 用户态拷贝 | 每次全拷 | 每次全拷 | **只注册一次** |
| 返回方式 | 遍历找就绪 | 遍历找就绪 | **只返回就绪的** |
| 时间复杂度 | O(n) | O(n) | O(1) |
| 触发模式 | LT | LT | LT + ET |

**epoll 关键**：
- 红黑树管理 fd（添加删除 O(log n)）
- 就绪链表存放已触发事件的 fd
- 内核有事件时直接把 fd 塞进就绪链表，用户态**不用遍历**

#### Q7: LT 和 ET 触发模式的区别？

**LT（Level Triggered，水平触发）** — 默认：
- 只要 fd 上有数据没读完，就一直通知
- **编程简单**，读多少都行，下次事件循环还会通知
- Nginx 默认用 LT

**ET（Edge Triggered，边沿触发）**：
- 只在**状态变化时**通知一次（不可读→可读）
- 必须**一次读完**，否则下次事件循环不会再通知
- 效率高但编程复杂
- 需要 fd 设成 non-blocking

**Nginx 用哪个**：主要用 LT，配合非阻塞 IO 已经够快。

#### Q8: worker_connections 该设多少？

```nginx
events {
    worker_connections 65535;
}
```

**总上限 = worker_processes × worker_connections**。

考虑因素：
- **系统 fd 上限**：`ulimit -n` 和 `worker_rlimit_nofile`
- **内存**：每连接占用几十 KB 内存
- **反向代理场景**：一个客户端连接对应 2 个 fd（一个下游、一个上游）——**实际能扛的并发要除以 2**

**推荐**：单 worker 1-6 万连接，配合足够的 ulimit。

#### Q9: `worker_processes auto` 是几个？

`auto` = 当前机器的 CPU 核数（`grep -c ^processor /proc/cpuinfo`）。

**为什么等于核数最优**：
- 每个 worker 独占一个核（配合 `worker_cpu_affinity auto`）
- 避免 CPU 抢占和 cache miss
- 多于核数没意义（只会增加切换）

**特例**：磁盘 IO 密集（大量静态文件、日志写盘）可以适当**多开一些**，让部分 worker 阻塞时其他能顶上——但这种场景现在很少（都用 SSD 和异步 IO）。

#### Q10: Nginx 和 Apache 的核心区别？

| 维度 | Nginx | Apache |
|-----|-------|--------|
| 模型 | 事件驱动 + 多进程 | prefork（多进程）/ worker（多进程+多线程） |
| 并发 | 万级+ | 千级 |
| 静态资源 | 极快 | 中等 |
| 内存 | 小 | 大 |
| 动态语言 | 转发到 FPM/upstream | 直接 mod_php 内嵌 |
| 配置 | 简洁清晰 | 强大但复杂 |
| .htaccess | 不支持 | 支持 |

**用法结论**：Nginx 前 + Apache 后（Apache 处理 PHP），或 Nginx + PHP-FPM 全套。

---

### 二、配置与语法（10 题）

#### Q11: location 的匹配优先级？

**优先级从高到低**：

```
1. =         精确匹配
2. ^~        前缀匹配（找到就停）
3. ~ / ~*    正则（按配置文件顺序，第一个命中就用）
4. 无修饰    前缀匹配（记住最长的，等正则都没命中时用）
```

**记忆**：`= > ^~ > 正则 > 前缀`。

举例：请求 `/static/logo.png`：

```nginx
location ^~ /static/ { ... }          # ★ 命中，用这个
location ~* \.png$ { ... }            # 被上面拦截了，走不到
location /static/ { ... }             # 优先级低于 ^~，走不到
```

#### Q12: root 和 alias 的区别？

**root 拼接，alias 替换**。

```nginx
# 请求 /static/logo.png

location /static/ {
    root /var/www;
    # 结果：/var/www + /static/logo.png = /var/www/static/logo.png
}

location /static/ {
    alias /var/www/assets/;
    # 结果：/var/www/assets/ + logo.png = /var/www/assets/logo.png
    #      （location 部分 /static/ 被 alias 值替换）
}
```

**alias 必须以 `/` 结尾**（配对 location 结尾的 `/`）。

#### Q13: `proxy_pass` 结尾的 `/` 有什么影响？

关键规则：**proxy_pass 带 URI（哪怕只是 `/`）就替换 location 部分**。

```nginx
# 请求 /api/user/1

location /api/ {
    proxy_pass http://backend;      # 不带 URI
    # 转发到：http://backend/api/user/1
}

location /api/ {
    proxy_pass http://backend/;     # 带 URI（就一个 /）
    # 转发到：http://backend/user/1     ← /api/ 被替换成 /
}

location /api/ {
    proxy_pass http://backend/v2/;  # 带 URI 是 /v2/
    # 转发到：http://backend/v2/user/1
}
```

**面试常问**：给个场景让你写配置，主要看有没有这个坑意识。

#### Q14: server_name 匹配优先级？

```
1. 精确匹配：      server_name api.example.com;
2. 通配符前缀：    server_name *.example.com;
3. 通配符后缀：    server_name example.*;
4. 正则匹配：      server_name ~^www\d+\.example\.com$;
5. default_server：兜底
```

#### Q15: rewrite 的 4 个标志？

```nginx
rewrite ^/old/(.*)$ /new/$1 [标志];
```

| 标志 | 行为 |
|-----|------|
| `last` | 改写后**重新匹配 location** |
| `break` | 改写后**在当前 location 继续处理**（不重匹配） |
| `redirect` | 302 临时重定向（返回 HTTP 302） |
| `permanent` | 301 永久重定向 |

**last vs break** 差异例：

```nginx
location /a/ {
    rewrite ^/a/(.*)$ /b/$1 last;    # 会去找 /b/ 的 location
    # 后面的指令跑不到
}

location /a/ {
    rewrite ^/a/(.*)$ /b/$1 break;   # 内部改成 /b/x 但仍在 /a/ location 里
    proxy_pass http://backend;       # 这行会执行
}
```

#### Q16: 为什么说 `if is evil`？

Nginx 的 `if` 在 `location` 里有大量非预期行为，特别是**多个 if / if 里嵌套 proxy_pass** 时。

**推荐替代方案**：

- 用 `map` 做条件映射
- 用 `try_files` 做文件降级
- 用 `limit_except` 限制 method
- 用不同 location 处理不同情况

**安全使用 if 的场景**：只在 `server` 上下文里、条件简单、不涉及 proxy_pass。

#### Q17: 什么是 try_files？

**按顺序尝试文件是否存在，找到就用，都没有就返回最后一个（一般是 fallback）**。

```nginx
# SPA 单页应用的经典写法
location / {
    try_files $uri $uri/ /index.html;
    #        ↑ 先按 URI 找文件
    #             ↑ 再按 URI + / 找目录
    #                    ↑ 都没有就返回 index.html（让前端路由处理）
}

# 用于 PHP 场景
location / {
    try_files $uri $uri/ /index.php?$args;
    # 找不到就转到 index.php
}
```

#### Q18: 如何让 Nginx 不返回版本号？

```nginx
http {
    server_tokens off;
}
```

**为什么关**：
- 版本号是攻击面（能查到已知漏洞）
- 隐藏后 `Server: nginx`（不含版本）

**更极致**（改源码或用 `more_set_headers`）：完全去掉 Server 头。

#### Q19: `internal` 指令是干什么的？

标记 location **只允许内部访问**（外部请求返回 404）。

```nginx
location /internal-api/ {
    internal;                     # ★ 只允许内部（rewrite/error_page/X-Accel-Redirect）
    proxy_pass http://backend;
}

location / {
    error_page 404 = /internal-api/handle-404;   # 由内部调用，允许
}
```

典型场景：`X-Accel-Redirect` 做安全下载——文件权限在 App 层校验，通过后让 Nginx 内部转发到 internal location 直接送文件。

#### Q20: `map` 指令怎么用？

**基于变量值做映射**（比 if 优雅）：

```nginx
http {
    # 定义映射
    map $http_user_agent $is_mobile {
        default 0;                 # 默认 0
        ~*android 1;
        ~*iphone  1;
        ~*ipad    1;
    }

    server {
        location / {
            if ($is_mobile) {
                rewrite ^ /mobile$uri last;
            }
        }
    }
}
```

**map 的优势**：
- 编译期展开，运行时 O(1)
- 支持正则和通配
- 可嵌套 map

---

### 三、性能（10 题）

#### Q21: sendfile 是什么？为什么快？

**零拷贝**：文件内容直接从**内核 pagecache** 送到 **socket buffer**，不用来回拷贝到用户态。

普通读文件发 socket：

```
磁盘 → 内核 pagecache → 用户 buffer → 内核 socket buffer → 网卡
                        ↑             ↑ 两次上下文切换 + 拷贝
```

sendfile：

```
磁盘 → 内核 pagecache → 内核 socket buffer → 网卡
                        ↑ 内核态直接传，无用户态介入
```

**注意**：sendfile 对**静态文件**才生效。反向代理场景（proxy_pass）本来就是从内存 buffer 发，无关。

#### Q22: tcp_nopush 和 tcp_nodelay 是干什么的？

**tcp_nopush（Linux 上等价于 TCP_CORK）**：
- 攒够一个包或 200ms 后再发
- 配合 sendfile：把响应头和文件内容**合并成一个 TCP 包**发出（不然响应头一个包、body 一个包）

**tcp_nodelay（禁用 Nagle）**：
- 有小包立即发，不等
- 长连接场景禁 Nagle 减延迟

**两者矛盾吗？不矛盾**：
- 传输阶段用 nopush 攒大包
- 传输末尾用 nodelay 尽快把最后一个小包送出

Nginx 会智能切换。

#### Q23: 长连接（keepalive）怎么配？

**客户端到 Nginx**：

```nginx
http {
    keepalive_timeout 65s;        # 空闲多久断开
    keepalive_requests 1000;      # 单个连接最多处理 1000 个请求
}
```

**Nginx 到后端（关键，容易漏！）**：

```nginx
upstream backend {
    server 10.0.0.1:8080;
    keepalive 32;                 # ★ 保留 32 个空闲连接给下次复用
    keepalive_timeout 60s;
    keepalive_requests 100;
}

location / {
    proxy_pass http://backend;
    proxy_http_version 1.1;                # ★ 必须 1.1（1.0 不支持）
    proxy_set_header Connection "";        # ★ 清掉 close，才能 keepalive
}
```

**面试要点**：不配 upstream keepalive 的话，Nginx 到后端每个请求都建 TCP 连接——高 QPS 下会产生大量 TIME-WAIT。

#### Q24: 长连接的三个参数各控制什么？

| 参数 | 作用域 | 说明 |
|-----|-------|------|
| `keepalive 32` | upstream | 空闲连接池大小 |
| `keepalive_requests 100` | upstream / http | 单连接最多处理请求数 |
| `keepalive_timeout 60s` | upstream / http | 空闲多久断开 |

超过 `keepalive_requests` 或 `keepalive_timeout` 后连接关闭。**为什么要限**：防止一个连接被"永远"占用、防止老 fd 累积。

#### Q25: worker 之间怎么共享数据？

Nginx worker 之间**基本不共享**用户请求数据，需要共享的用**共享内存**：

```nginx
http {
    # 限流用的计数（跨 worker 生效）
    limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;
    #                                       ↑ 10MB 共享内存

    # OpenResty：Lua 用的字典
    lua_shared_dict my_cache 100m;
}
```

底层是 mmap 匿名共享内存 + 自旋锁 / 红黑树。

#### Q26: proxy_buffer_size 和 proxy_buffers 区别？

```nginx
proxy_buffer_size 4k;      # 单个 buffer 大小（响应头用）
proxy_buffers 8 16k;       # ★ 响应 body 用：8 个 16KB buffer = 128KB
proxy_busy_buffers_size 32k;   # 已向客户端发送但还在用的 buffer 上限
```

**响应流程**：
1. 收到后端响应头 → 塞进 `proxy_buffer_size`（4KB）的 buffer
2. 收到 body → 塞进 `proxy_buffers`（8 × 16KB）
3. buffer 满 → 写临时文件（`proxy_temp_path`）

**关闭缓冲**（流式转发，适合 SSE / 大下载）：

```nginx
proxy_buffering off;
```

#### Q27: client_max_body_size 是干什么的？

```nginx
client_max_body_size 100m;   # ★ 允许的请求 body 上限
```

**默认 1MB**——上传接口如果没改这个会返回 `413 Request Entity Too Large`。

**注意**：这个值超过后**直接拒绝**，不会先接收后判断（省流）。

#### Q28: gzip 什么类型不该压缩？

- **图片**（jpg/png/gif/webp）——已压缩，再压更大
- **视频/音频**（mp4/mp3/webm）——同上
- **压缩包**（zip/gz/br）——已压缩
- **加密文件**（PDF 有些）——熵接近随机，压不动

面试常问："为什么不给全部内容开 gzip"——CPU 消耗 + 图片压缩负收益。

#### Q29: gzip_comp_level 该怎么选？

`1-9`，越大压缩率越高、CPU 消耗越大。

**推荐 6**——速度和压缩率的平衡点。生产别用 9（CPU 涨很多，压缩率提升有限）。

#### Q30: sendfile 关掉的场景？

**开启 gzip 的响应**——gzip 需要读文件到用户态压缩再发，sendfile 走不了。

Nginx 会自动禁用 sendfile：`Content-Encoding: gzip` 的响应不走 sendfile。**不用手动关**。

---

### 四、安全（5 题）

#### Q31: HTTPS 强制的配置？

```nginx
server {
    listen 80;
    server_name example.com;

    # ★ 所有 HTTP 请求 301 到 HTTPS
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name example.com;

    ssl_certificate     /etc/nginx/cert/fullchain.pem;
    ssl_certificate_key /etc/nginx/cert/privkey.pem;

    # ★ 强制浏览器记住 HTTPS 一年（HSTS）
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    location / {
        proxy_pass http://backend;
    }
}
```

**HSTS 陷阱**：一旦浏览器记住，就算证书过期也拒绝访问——**上线前先小 max-age 试**（`max-age=300` 5 分钟）。

#### Q32: SSL 配置的最佳实践？

```nginx
# ★ 只用 TLS 1.2/1.3（TLS 1.0/1.1 有已知漏洞，已被淘汰）
ssl_protocols TLSv1.2 TLSv1.3;

# ★ Cipher（按 Mozilla intermediate 配置）
ssl_ciphers 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384';
ssl_prefer_server_ciphers off;                    # TLS 1.3 都是好 cipher，让客户端选

# ★ Session 复用（减少握手开销）
ssl_session_cache shared:SSL:10m;                 # 10MB ≈ 4 万会话
ssl_session_timeout 1d;
ssl_session_tickets off;                          # ticket 不安全，用 session cache

# ★ OCSP Stapling（证书吊销检查加速）
ssl_stapling on;
ssl_stapling_verify on;
resolver 8.8.8.8 1.1.1.1 valid=60s;
resolver_timeout 5s;
```

**测试工具**：[SSL Labs](https://www.ssllabs.com/ssltest/) 打分——目标 **A 或 A+**。

#### Q33: 如何防止服务器信息泄漏？

```nginx
server_tokens off;                                # 不显示 Nginx 版本

# 隐藏后端错误页（Tomcat / Django 报错栈别露出去）
proxy_hide_header X-Powered-By;
proxy_hide_header X-AspNet-Version;
proxy_hide_header Server;

# 自定义错误页
error_page 500 502 503 504 /50x.html;
error_page 404 /404.html;
```

#### Q34: 如何限制某些 IP 或 UA？

```nginx
# IP 黑白名单
location /admin/ {
    allow 10.0.0.0/8;              # 内网可访问
    allow 192.168.0.0/16;
    deny all;                       # 其他拒绝
    proxy_pass http://backend;
}

# UA 拦截
if ($http_user_agent ~* (bytespider|semrushbot|ahrefsbot)) {
    return 403;                    # 拦爬虫
}

# ★ 更好：用 map + 变量
map $http_user_agent $blocked_ua {
    default 0;
    ~*bytespider 1;
    ~*semrushbot 1;
    ~*ahrefsbot  1;
}

server {
    if ($blocked_ua) {
        return 403;
    }
}
```

#### Q35: 如何防 CC 攻击？

**分层防御**：

1. **CDN 层**：Cloudflare / 阿里云 SLB 高防 IP
2. **Nginx 层**：limit_req + limit_conn + IP 黑名单
3. **应用层**：验证码、行为分析、账号维度限流

Nginx 层配置（详见场景 2）：

```nginx
# 全局限速
limit_req_zone $binary_remote_addr zone=global:10m rate=30r/s;
limit_conn_zone $binary_remote_addr zone=global_conn:10m;

server {
    location / {
        limit_req  zone=global burst=100 nodelay;
        limit_conn global_conn 20;
        proxy_pass http://backend;
    }
}
```

---

### 五、生产实践（5 题）

#### Q36: 热重载和优雅重启的原理？

`nginx -s reload` 底层：

1. Master 收到 SIGHUP
2. 重新读配置文件（校验语法）
3. 启动**新 worker**（用新配置）
4. 给**老 worker** 发 SIGQUIT（优雅退出）
5. 老 worker 处理完当前请求后自然退出
6. 新 worker 接手新连接

**新老共存的短暂时间里，没有断连**——这就是零停机的核心。

#### Q37: `nginx -t` 和 `nginx -T` 的区别？

- `nginx -t` — 只做**语法校验**（不启动）
- `nginx -T` — 语法校验 + **打印当前完整配置**（含所有 include 展开）

**用途**：
- `nginx -t` 上线前必跑
- `nginx -T` 排查"到底跑的什么配置"（多 include 时特别有用）

#### Q38: 日志切割怎么做？

**方案 1：logrotate（推荐）**

```
/var/log/nginx/*.log {
    daily
    rotate 30
    compress
    missingok
    notifempty
    postrotate
        kill -USR1 `cat /var/run/nginx.pid`         # ★ 通知 nginx 重开日志
    endscript
}
```

**方案 2：自己写脚本**

```bash
#!/bin/bash
# nginx-logrotate.sh
DATE=$(date -d "yesterday" +%Y%m%d)
mv /var/log/nginx/access.log /var/log/nginx/access.$DATE.log
kill -USR1 `cat /var/run/nginx.pid`
find /var/log/nginx -name "access.*.log" -mtime +30 -delete
```

crontab 每天 0 点跑：

```
0 0 * * * /path/to/nginx-logrotate.sh
```

**关键**：`kill -USR1` 让 nginx 重新打开日志文件——**不这样做的话**，Nginx 会继续写到已被 mv 的旧文件（内核靠 inode 找文件，不看名字）。

#### Q39: 平滑升级二进制怎么做？

```bash
# 1. 替换二进制
mv /usr/local/nginx/sbin/nginx /usr/local/nginx/sbin/nginx.old
cp new-nginx /usr/local/nginx/sbin/nginx

# 2. 老 master 生成新 master（保留老的）
kill -USR2 `cat /var/run/nginx.pid`
# 此时：老 master + 老 worker 还在跑；新 master + 新 worker 也在跑

# 3. 老 worker 优雅退出
kill -WINCH `cat /var/run/nginx.pid.oldbin`
# 老 worker 处理完剩下的请求后退出，只剩老 master

# 4. 观察新版本稳定后，关掉老 master
kill -QUIT `cat /var/run/nginx.pid.oldbin`

# 或者回滚（如果新版本有问题）：
kill -HUP  `cat /var/run/nginx.pid.oldbin`   # 老 master 重启 worker
kill -QUIT `cat /var/run/nginx.pid`          # 干掉新 master
```

#### Q40: 生产上怎么监控 Nginx？

**基础指标（stub_status）**：

```nginx
location /nginx_status {
    stub_status on;
    access_log off;
    allow 10.0.0.0/8;
    deny all;
}
```

**Prometheus + Grafana**：
- 用 [nginx-prometheus-exporter](https://github.com/nginxinc/nginx-prometheus-exporter) 或 OpenResty 版
- 采集：连接数、请求速率、状态码分布、响应时间

**日志层**：
- JSON 格式 access.log → Filebeat → ES
- Grafana Loki 也行

**关键监控指标**：

| 指标 | 报警阈值 |
|-----|---------|
| 5xx 比例 | > 1% |
| P99 延迟 | > 业务基线 × 2 |
| 后端连接失败 (upstream error) | > 10 / min |
| Active 连接 | > 单 worker 上限 80% |
| 每秒新增连接数 | 突然翻倍（可能是攻击） |

---

## Part 2：生产场景实战

### 场景 1：灰度发布（按用户 ID / cookie / header）

**目标**：新版本先给 10% 用户，稳定后再全量。

**方案 A：按 cookie 灰度**

```nginx
# 定义 upstream
upstream v1_backend {
    server 10.0.0.1:8080;
    server 10.0.0.2:8080;
}
upstream v2_backend {
    server 10.0.0.11:8080;
    server 10.0.0.12:8080;
}

# ★ 用 map 决定走哪个版本
map $cookie_version $target_backend {
    default v1_backend;             # 默认老版本
    "v2"    v2_backend;             # cookie version=v2 → 新版本
}

server {
    listen 80;
    location / {
        proxy_pass http://$target_backend;
        proxy_set_header Host $host;
    }
}
```

用户想切到新版本：客户端设置 `Cookie: version=v2`。灰度期结束：改 map 默认值为 v2。

**方案 B：按用户 ID 尾号百分比灰度**

```nginx
# ★ 提取用户 ID（假设从 header 传入）
map $http_x_user_id $gray_bucket {
    default 0;
    ~[0-9]$ $http_x_user_id;        # 取最后一位
}

# 尾号 0 → 新版本（10%）
map $gray_bucket $target_backend {
    default v1_backend;
    "0"     v2_backend;
}
```

**方案 C：按内部员工 IP 灰度**

```nginx
geo $is_internal {
    default 0;
    10.0.0.0/8 1;
    192.168.0.0/16 1;
}

map $is_internal $target_backend {
    default v1_backend;
    1       v2_backend;              # 内网员工先用新版本
}
```

**方案 D：Nginx + Lua 灵活灰度（OpenResty）**

```nginx
location /api/ {
    access_by_lua_block {
        local user_id = ngx.req.get_headers()["X-User-Id"]
        if not user_id then
            ngx.var.target = "v1_backend"
            return
        end

        -- 从 Redis 查用户是否在白名单
        local redis = require "resty.redis"
        local red = redis:new()
        red:connect("127.0.0.1", 6379)
        local in_whitelist = red:sismember("gray:v2:users", user_id)

        if in_whitelist == 1 then
            ngx.var.target = "v2_backend"
        else
            ngx.var.target = "v1_backend"
        end
    }
    proxy_pass http://$target;
}
```

**灰度关键点**：
- 灰度期要**打点观察**（v2 的错误率、延迟对比 v1）
- 出问题**立即切回**（改 map 或 upstream 权重）
- **不能中途改 URI 语义**（灰度切换不能破坏用户会话）

---

### 场景 2：分级限流（防刷 + 防爬 + 防 CC）

**目标**：不同接口不同限流阈值，同时限速率和并发。

```nginx
http {
    # ★ 定义三个限流区
    limit_req_zone $binary_remote_addr zone=login_limit:10m  rate=5r/s;
    #                                                        ↑ 登录限严，防暴破
    limit_req_zone $binary_remote_addr zone=api_limit:10m    rate=100r/s;
    limit_req_zone $binary_remote_addr zone=static_limit:10m rate=1000r/s;

    limit_conn_zone $binary_remote_addr zone=conn_limit:10m;

    # ★ 白名单跳过限流
    geo $limit_bypass {
        default 0;
        10.0.0.0/8 1;                # 内网跳过限流
        192.168.0.0/16 1;
    }
    map $limit_bypass $limit_key {
        0 $binary_remote_addr;       # 走限流
        1 "";                         # 空 key = 不限流
    }

    server {
        listen 80;

        # 登录：最严
        location = /api/login {
            limit_req zone=login_limit burst=10 nodelay;
            limit_conn conn_limit 3;
            limit_req_status 429;
            proxy_pass http://backend;
        }

        # 普通 API
        location /api/ {
            limit_req zone=api_limit burst=200 nodelay;
            limit_conn conn_limit 30;
            limit_req_status 429;
            proxy_pass http://backend;
        }

        # 静态资源：宽松
        location /static/ {
            limit_req zone=static_limit burst=2000 nodelay;
            root /var/www;
        }
    }
}
```

**返回 429 时可以带 Retry-After**（提示客户端多久后重试）：

```nginx
location = /50x.html {
    add_header Retry-After 60 always;
}
error_page 429 /50x.html;
```

---

### 场景 3：大文件上传（100MB 视频）

**目标**：允许上传 100MB 视频文件，避免占满 worker 内存。

```nginx
http {
    # ★ 全局上限
    client_max_body_size 200m;         # 允许 200MB
    client_body_buffer_size 128k;      # 小 body 内存缓冲，大 body 直接写磁盘
    client_body_timeout 60s;
    client_body_temp_path /var/nginx/upload_tmp;  # 临时文件目录（挂到 SSD 或 tmpfs）

    server {
        listen 80;
        server_name upload.example.com;

        location /upload {
            # ★ 上传请求不缓冲，直接流给后端（不然占 worker 内存）
            proxy_request_buffering off;

            # ★ 允许长时间上传
            proxy_read_timeout 600s;
            proxy_send_timeout 600s;

            proxy_pass http://upload_backend;
            proxy_http_version 1.1;
            proxy_set_header Connection "";
        }
    }
}
```

**面试要点**：
- `client_max_body_size` 不改会 413
- `proxy_request_buffering off` 是关键——不然大文件全缓存到 worker 才开始转发

**更好方案**：直传 OSS/S3
- 后端生成临时上传凭证
- 客户端**直接上传到对象存储**，绕过 Nginx
- 上传完成后回调后端记录元数据

---

### 场景 4：WebSocket 反向代理

**目标**：Nginx 转发 WebSocket 到后端。

```nginx
# ★ WebSocket 需要升级协议头的 map
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

upstream ws_backend {
    server 10.0.0.5:8080;
    # ★ 用 ip_hash 让同一客户端总落到同一后端
    ip_hash;
}

server {
    listen 80;
    server_name ws.example.com;

    location /ws {
        proxy_pass http://ws_backend;

        # ★ 升级协议
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        # ★ WebSocket 长连接，超时要设大
        proxy_read_timeout  3600s;    # 1 小时无数据才断
        proxy_send_timeout  3600s;

        # 转发真实 IP
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    }
}
```

**关键点**：
- 必须 `HTTP/1.1`（0.9/1.0 不支持 Upgrade）
- 必须传 `Upgrade` 和 `Connection` 头
- 读超时要大（默认 60s，WebSocket 常常没数据也不能断）

---

### 场景 5：HTTPS + HTTP/2 + HSTS

**目标**：全站强制 HTTPS，启用 HTTP/2。

```nginx
# HTTP → HTTPS 重定向
server {
    listen 80;
    server_name example.com www.example.com;

    return 301 https://$host$request_uri;
}

# HTTPS 主站
server {
    listen 443 ssl http2;              # ★ http2 关键字
    server_name example.com www.example.com;

    # 证书
    ssl_certificate     /etc/nginx/cert/fullchain.pem;
    ssl_certificate_key /etc/nginx/cert/privkey.pem;

    # 协议与 cipher
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:...';
    ssl_prefer_server_ciphers off;

    # Session 复用
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;

    # OCSP Stapling
    ssl_stapling on;
    ssl_stapling_verify on;
    resolver 8.8.8.8 valid=60s;

    # ★ HSTS（浏览器记住只走 HTTPS 一年）
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    # ★ 安全响应头
    add_header X-Content-Type-Options    "nosniff" always;
    add_header X-Frame-Options           "SAMEORIGIN" always;
    add_header Referrer-Policy           "strict-origin-when-cross-origin" always;
    add_header Content-Security-Policy   "default-src 'self'; script-src 'self' 'unsafe-inline'" always;

    location / {
        proxy_pass http://backend;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

**上线前小步验证 HSTS**：先 `max-age=300`（5 分钟），观察一天没问题再改一年。**HSTS 一旦生效，就算证书过期也访问不了**——很多人踩过这坑。

---

### 场景 6：跨域（CORS）

**目标**：让 `api.example.com` 允许 `www.example.com` 前端访问。

```nginx
server {
    listen 80;
    server_name api.example.com;

    location / {
        # ★ 预检请求（OPTIONS）单独处理
        if ($request_method = 'OPTIONS') {
            add_header 'Access-Control-Allow-Origin'      'https://www.example.com' always;
            add_header 'Access-Control-Allow-Methods'     'GET, POST, PUT, DELETE, OPTIONS' always;
            add_header 'Access-Control-Allow-Headers'     'Authorization, Content-Type, X-Requested-With' always;
            add_header 'Access-Control-Allow-Credentials' 'true' always;
            add_header 'Access-Control-Max-Age'           1728000;               # 20 天缓存
            add_header 'Content-Length' 0;
            add_header 'Content-Type' 'text/plain charset=UTF-8';
            return 204;                                                          # ★ 直接返回，不转后端
        }

        # 实际请求带上 CORS 头
        add_header 'Access-Control-Allow-Origin'      'https://www.example.com' always;
        add_header 'Access-Control-Allow-Credentials' 'true' always;
        add_header 'Access-Control-Expose-Headers'    'X-Request-Id' always;

        proxy_pass http://backend;
    }
}
```

**要点**：
- OPTIONS 预检要**在 Nginx 层就返回**，别转后端（浪费）
- `always` 后缀关键——**非 2xx 响应也要带 CORS 头**（不然错误码前端拿不到）
- `Access-Control-Allow-Credentials: true` 时 `Allow-Origin` **不能是 `*`**，必须精确匹配

**动态支持多个 Origin**：

```nginx
map $http_origin $cors_origin {
    default "";
    "https://www.example.com" $http_origin;
    "https://m.example.com"   $http_origin;
}

server {
    add_header Access-Control-Allow-Origin $cors_origin always;
}
```

---

### 场景 7：反爬虫

**目标**：识别并拦截爬虫。

**分层策略**：

```nginx
http {
    # ★ 已知爬虫 UA 黑名单
    map $http_user_agent $blocked_ua {
        default 0;
        ~*(bytespider|semrushbot|ahrefsbot|mj12bot|dotbot) 1;
        ~*(python-requests|curl|wget|go-http-client) 1;   # 简单脚本
        "" 1;                                              # 空 UA 拦
    }

    # ★ 爬虫更严的限流
    map $http_user_agent $is_bot {
        default 0;
        ~*(bot|spider|crawler) 1;
    }

    limit_req_zone $binary_remote_addr zone=normal:10m rate=100r/s;
    limit_req_zone $binary_remote_addr zone=bot:10m    rate=10r/s;

    server {
        listen 80;

        # UA 黑名单直接拦
        if ($blocked_ua) {
            return 403;
        }

        location / {
            # 爬虫走严限
            if ($is_bot) {
                set $limit_zone bot;
            }
            # 这里 if + limit_req 用法有陷阱，推荐用 map + 多 location

            proxy_pass http://backend;
        }
    }
}
```

**更好的方案（OpenResty + Redis）**：

```lua
access_by_lua_block {
    local ip = ngx.var.remote_addr
    local ua = ngx.req.get_headers()["User-Agent"] or ""

    -- 1. 校验 UA
    if ua == "" or ngx.re.match(ua, "bot|spider|crawler", "i") then
        -- 频率限制
        local redis = require "resty.redis"
        local red = redis:new()
        red:connect("127.0.0.1", 6379)

        local key = "rate:bot:" .. ip
        local count = red:incr(key)
        if count == 1 then
            red:expire(key, 60)
        end

        if count > 20 then          -- 爬虫 60 秒最多 20 次
            ngx.exit(429)
        end
    end
}
```

**其他手段**：
- **验证码**：limit_req 触发后返回验证码页面
- **JS 挑战**：Cloudflare 那种"5 秒盾"
- **蜜罐 URL**：robots.txt 里假 URL，访问就拉黑
- **行为分析**：应用层判断（鼠标轨迹、TCP 指纹）

---

### 场景 8：动静分离

**目标**：静态资源直接 Nginx 送，动态请求转后端。

```nginx
server {
    listen 80;
    server_name example.com;

    # ★ 静态文件根目录
    root /var/www/example.com;

    # 静态资源直接返回（Nginx 送，不过后端）
    location ~* \.(js|css)$ {
        expires 7d;
        add_header Cache-Control "public, immutable";
        access_log off;                              # 静态资源不记 log
    }

    location ~* \.(jpg|jpeg|png|gif|webp|svg|ico|woff2)$ {
        expires 30d;
        add_header Cache-Control "public";
        access_log off;
    }

    # ★ 未找到静态文件时降级到后端（try_files 神器）
    location / {
        try_files $uri $uri/ @backend;
    }

    # ★ 命名 location：处理动态请求
    location @backend {
        proxy_pass http://api_backend;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

**扩展方案**：
- **CDN 加速静态资源**：Nginx 层配 `Cache-Control` + `expires`，CDN 就会缓存
- **域名分离**：`static.example.com`（CDN）+ `api.example.com`（Nginx 转后端）

---

### 场景 9：缓存击穿保护（多级 + 锁）

**目标**：热点 key 过期瞬间几千请求同时打到后端 → 直接崩。

**Nginx 层的保护（proxy_cache_lock）**：

```nginx
http {
    proxy_cache_path /var/cache/nginx
                     levels=1:2 keys_zone=hot_cache:100m
                     max_size=10g inactive=1h use_temp_path=off;

    upstream backend {
        server 10.0.0.5:8080;
        keepalive 32;
    }

    server {
        location / {
            proxy_pass http://backend;

            # ★ 缓存
            proxy_cache hot_cache;
            proxy_cache_key "$scheme$host$request_uri";
            proxy_cache_valid 200 302 5m;
            proxy_cache_valid 404 30s;

            # ★★ 击穿保护：同一 key 只放一个请求到后端
            proxy_cache_lock on;
            proxy_cache_lock_timeout 5s;            # 拿锁超时后其他请求也放行
            proxy_cache_lock_age 5s;                 # 拿到锁的 5s 内没生成缓存，其他人也放行

            # ★★ 后端挂了或超时用旧缓存兜底
            proxy_cache_use_stale error timeout updating http_500 http_502 http_503 http_504;
            proxy_cache_background_update on;        # 后台刷新，用户看的还是旧的
            proxy_cache_revalidate on;               # 用 If-Modified-Since 验证

            add_header X-Cache-Status $upstream_cache_status always;
        }
    }
}
```

**关键指令解读**：

- `proxy_cache_lock on` — 高并发下**同一 key 只有一个请求打到后端**，其他等着复用响应
- `proxy_cache_use_stale` — 后端出错时**用过期缓存兜底**，避免"缓存 miss + 后端挂"双杀
- `proxy_cache_background_update` — **异步刷新**：老缓存立刻返回，同时后台去后端拿新的

---

### 场景 10：优雅升级 Nginx 二进制

**目标**：不断连接、切换到新版本 Nginx。

```bash
# Step 1: 备份老 pid
old_pid=$(cat /var/run/nginx.pid)

# Step 2: 替换二进制
mv /usr/local/nginx/sbin/nginx /usr/local/nginx/sbin/nginx.old
cp /tmp/nginx-1.25 /usr/local/nginx/sbin/nginx

# Step 3: 让老 master 拉起新 master + 新 worker
kill -USR2 $old_pid

# 此时：
#   - 老 master (pid = $old_pid)      → 老 worker 继续处理连接
#   - 新 master (读 /var/run/nginx.pid) → 新 worker 处理新连接
# 老 pid 移到 /var/run/nginx.pid.oldbin

sleep 3   # 等新版本稳定

# Step 4: 让老 worker 优雅退出（老 master 保留，方便回滚）
kill -WINCH $old_pid

# 观察新版本 5-10 分钟，确认没问题

# Step 5: 干掉老 master
kill -QUIT $old_pid

# 如果需要回滚：
# kill -HUP  $old_pid                                    # 老 master 重启 worker
# kill -QUIT $(cat /var/run/nginx.pid)                   # 干掉新 master
```

**面试考点**：
- 为什么可以零停机？—— 老 worker + 新 worker 短时间并存，各自 accept
- 端口冲突怎么办？—— 用 `SO_REUSEPORT` 或**继承老 master 的 listen fd**（USR2 走的这条路）
- 回滚窗口有多长？—— 只要老 master 还在，随时能回滚

---

## 收尾

### 高频面试题速查

| 题目 | 章节 |
|-----|------|
| Nginx 为什么高并发 | Part 1 Q1 |
| Master / Worker 分工 | Q2 |
| 惊群解决方案 | Q4 |
| 请求 11 阶段 | Q5 |
| epoll vs select/poll | Q6 |
| location 匹配优先级 | Q11 |
| root vs alias | Q12 |
| proxy_pass 结尾 / | Q13 |
| 长连接怎么配 | Q23-Q24 |
| 热重载原理 | Q36 |
| 平滑升级 | Q39 |

### 场景速查

| 场景 | 关键指令 |
|-----|---------|
| 灰度发布 | `map` + upstream 切换 |
| 分级限流 | `limit_req_zone` 多个 zone |
| 大文件上传 | `client_max_body_size` + `proxy_request_buffering off` |
| WebSocket | `Upgrade` + `Connection` header + 大 timeout |
| HTTPS 强制 | `return 301` + HSTS |
| CORS | 预检 204 + `always` 后缀 |
| 反爬 | UA map + limit_req + Lua |
| 动静分离 | `try_files` + 命名 location |
| 缓存击穿 | `proxy_cache_lock` + `use_stale` |
| 优雅升级 | USR2 + WINCH + QUIT |

---

## 相关文档

- [core-knowledge.md](./core-knowledge.md) — 底层原理与配置详解
- [../linux/troubleshooting.md](../linux/troubleshooting.md) — 网络连接问题排查
- [../../databases/redis/README.md](../../databases/redis/README.md) — 缓存架构
