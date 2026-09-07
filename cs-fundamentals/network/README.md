# 计算机网络

## 计划收录

- **OSI / TCP-IP 分层模型**
- **TCP**：
  - 三次握手 / 四次挥手
  - TIME_WAIT / CLOSE_WAIT
  - 滑动窗口、拥塞控制（慢启动、拥塞避免、快重传、快恢复）
  - 粘包与拆包
  - Nagle 算法与 delayed ACK
- **UDP** 与 QUIC
- **HTTP**：
  - HTTP/1.0 vs 1.1 vs 2 vs 3
  - 请求方法、状态码
  - Cookie / Session / Token
  - 缓存：强缓存 / 协商缓存（ETag、Last-Modified）
  - 跨域（CORS）与预检请求
- **HTTPS**：
  - TLS 握手流程
  - 对称/非对称加密、数字证书、CA
  - 中间人攻击与防御
- **DNS**：递归查询、迭代查询、DNS 劫持
- **CDN**：原理与调度
- **WebSocket** vs SSE vs 长轮询

## 面试常问

- 三次握手能改成两次吗？为什么？
- 为什么 TIME_WAIT 是 2MSL？
- HTTP/2 相比 1.1 的改进（多路复用、头部压缩、Server Push）
- HTTPS 完整握手流程
