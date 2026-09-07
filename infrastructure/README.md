# 基础设施

中间件、容器化、运维相关。

## 目录

- [message-queue/](./message-queue/README.md) — 消息队列：Kafka、RabbitMQ、RocketMQ
- [search/](./search/README.md) — 搜索引擎：Elasticsearch
- [proxy/](./proxy/README.md) — 反向代理：Nginx
- [container/](./container/README.md) — 容器化：Docker、Kubernetes
- [linux/](./linux/README.md) — Linux 常用命令、排查工具

## 常考重点

- Kafka 高吞吐原理（顺序写、零拷贝、批量、分区）
- MQ 消息可靠性（At Most Once / At Least Once / Exactly Once）
- Elasticsearch 倒排索引与分片
- Nginx 事件驱动模型
- Docker 与 VM 的区别、镜像分层
- K8s 核心资源：Pod、Deployment、Service、Ingress
- Linux 性能排查：top、iostat、vmstat、pidstat、strace
