# Nginx

## 计划收录

- 事件驱动模型（epoll + master-worker 多进程）
- 反向代理、负载均衡策略（轮询、加权、ip_hash、least_conn）
- 静态资源服务、gzip 压缩
- location 匹配规则
- 限流：`limit_req` `limit_conn`
- 缓存：`proxy_cache`
- 灰度发布、AB 测试
- OpenResty 与 Lua 扩展
- 常用配置模板与优化参数

## 面试常问

- Nginx 为什么高并发？
- master 和 worker 的分工
- 为什么 Nginx 用多进程不用多线程？
