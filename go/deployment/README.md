# Go 服务部署与运行

Go 服务在生产环境的运行形态，以及"怎么把一个 Go 服务安全、可观测、可伸缩地跑起来"这条主线上的知识点。

## 运行形态一览

| 形态 | 代表 | 使用场景 |
|---|---|---|
| 裸机 / VM + systemd | 内网工具、IDC 老项目 | 简单，无云环境 |
| Docker 单机 / compose | 小项目、边缘节点、开发环境 | 快，但无自愈 |
| **Kubernetes** | ACK / TKE / CCE / 自建 | **中大型线上服务默认选项** |
| Serverless / FaaS | AWS Lambda、Cloud Run、阿里函数计算 | 事件驱动、突发流量、按次计费 |
| 边缘 | Cloudflare Workers、KubeEdge | 就近计算、离线设备 |

## 子目录

- **`demo/`** — 完整可跑的示例：Go HTTP 服务 + Dockerfile + K8s 全套清单，每个文件带面试向注释。这是这个目录的核心。

## 核心知识点

按考察频率分层，每一项都能在 `demo/` 里找到对应的代码/YAML 位置：

**Docker 层**
- 多阶段构建：编译镜像 vs 运行镜像
- base 镜像选择：`scratch` / `distroless` / `alpine` 的权衡
- 静态二进制：`CGO_ENABLED=0` 的意义
- ENTRYPOINT exec 形式与 PID 1 信号传递
- layer 缓存：先拷 `go.mod` 再拷源码

**K8s 层**
- Deployment 滚动升级：maxSurge / maxUnavailable
- 三种探针：startup / liveness / readiness 的区别和陷阱
- 优雅停机的完整时序：SIGTERM → endpoints 摘除 → drain → shutdown → SIGKILL
- 资源规格：requests 用于调度、limits 触发 throttle/OOMKilled
- HPA：副本数公式、指标类型、抖动控制
- PDB：主动驱逐的可用性保障
- 拓扑分布：topologySpreadConstraints、多可用区、反亲和
- 安全基线：runAsNonRoot、readOnlyRootFilesystem、drop ALL capabilities
- 配置与秘密：ConfigMap / Secret 消费方式、Sealed Secrets

**Go 应用层**
- 通过环境变量读配置（12-Factor App）
- 结构化日志（slog / zap）
- Prometheus 指标：Counter / Gauge / Histogram，标签基数控制
- HTTP server 超时配置：ReadHeaderTimeout / ReadTimeout / WriteTimeout / IdleTimeout
- graceful shutdown 与 readiness 联动

**生产流水线（demo 未覆盖但要能讲）**
- CI：test / lint / 镜像漏洞扫描（trivy / grype）
- 镜像仓库：Harbor / ACR / ECR，用 commit SHA 而非 latest
- GitOps：Argo CD / Flux
- 多环境管理：Kustomize overlay 或 Helm values
- 观测三支柱：日志（Loki / ELK）、指标（Prometheus）、链路（OpenTelemetry / Jaeger）

## 从 demo 开始

```bash
cd demo
make tidy && make run          # 本地跑
# 或
make docker && make docker-run # Docker 跑
# 或
make k8s-apply                 # 部署到 K8s
```

具体到"每个知识点在哪里体现、面试怎么讲"，进 `demo/README.md` 看知识点索引表。
