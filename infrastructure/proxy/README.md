# Nginx / 反向代理

Nginx 是后端 / SRE 面试的高频考点——**高并发架构 + 反向代理 + 负载均衡**。两份文档形成完整体系。

## 已收录

- [**core-knowledge.md**](./core-knowledge.md) — Nginx 核心知识全景
  - Master-Worker 架构与事件驱动模型（epoll + 惊群解决）
  - 配置文件结构（context 层级 + 指令继承）
  - 请求处理 11 阶段
  - server_name 与 location 匹配规则（含 `=` `^~` `~` `~*` 优先级详解）
  - 反向代理 `proxy_pass`（含结尾 `/` 陷阱）
  - upstream 与 5 种负载均衡算法
  - 静态资源、gzip、sendfile、零拷贝
  - `proxy_cache` 缓存 + 击穿保护
  - `limit_req` / `limit_conn` 限流
  - 变量、rewrite、`map`、`if 陷阱`
  - 日志、监控、性能调优参数
  - 热重载与二进制平滑升级
  - OpenResty / Lua 扩展
  - **每个配置块可直接跑，关键指令逐行注释**

- [**interview-and-scenarios.md**](./interview-and-scenarios.md) — 面试题 + 生产场景实战
  - **Part 1**：40 道高频面试题（架构 10 + 配置 10 + 性能 10 + 安全 5 + 生产 5）
  - **Part 2**：10 个生产场景实战
    - 灰度发布（cookie / 用户 ID / IP 白名单 / OpenResty 四方案）
    - 分级限流（登录严 / API 普通 / 静态宽 + 白名单跳过）
    - 大文件上传（`client_max_body_size` + `proxy_request_buffering off`）
    - WebSocket 反向代理（`Upgrade` + `Connection` + 长 timeout）
    - HTTPS + HTTP/2 + HSTS 完整最佳实践
    - CORS 跨域（含预检 204 + 动态 origin）
    - 反爬虫（UA 黑名单 + 分级限流 + Lua）
    - 动静分离（`try_files` + 命名 location）
    - 缓存击穿保护（`proxy_cache_lock` + `use_stale` + 后台刷新）
    - 优雅升级二进制（USR2 → WINCH → QUIT 完整流程）

## 两份文档的定位

| | core-knowledge | interview-and-scenarios |
|---|---|---|
| 形式 | 系统化知识 + 配置示例 | Q&A + 场景 SQL |
| 用途 | **建立框架、掌握原理** | **刷题、面前突击** |
| 覆盖 | 底层原理 + 语法细节 | 高频考点 + 生产案例 |
| 建议顺序 | ① 先看 core 建立框架 | ② 再刷题看场景 |

## 面试三大主题

绝大部分深度问题都会绕回这三个：

1. **架构与并发**（epoll / master-worker / 惊群 / 事件模型 / 11 阶段）
2. **配置语法陷阱**（location 优先级 / root vs alias / proxy_pass 结尾 /）
3. **生产实战**（限流 / 缓存 / 灰度 / 长连接 / 热重载）

## 常问一梯队

- Nginx 为什么高并发？（→ 事件驱动 + 多进程 + epoll + 零拷贝）
- Master 和 Worker 的分工？为什么不用多线程？
- location 的匹配优先级？`= ^~ ~ ~*` 分别什么意思？
- root 和 alias 的区别？
- proxy_pass 后带 `/` 和不带 `/` 有什么区别？
- 负载均衡有哪几种算法？各自适用什么场景？
- 长连接怎么配（upstream keepalive 三兄弟）
- `nginx -s reload` 的底层流程是怎样的？
- 灰度发布怎么做？限流怎么做？
- 缓存击穿怎么防？（`proxy_cache_lock` 的作用）

## 阅读建议

**首次学习**：
1. 通读 `core-knowledge.md` 建立框架（重点看 location 匹配、proxy_pass、upstream、请求 11 阶段）
2. 抽出 `interview-and-scenarios.md` 的 40 道题过一遍，答不上来的回 core-knowledge 找
3. 场景题**动手照着配一遍**（vagrant / docker 起个 nginx）

**面前突击**：
1. 只看 `interview-and-scenarios.md`
2. 场景速查表 → 找出自己不熟的重点看
