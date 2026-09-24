# Go + Docker + Kubernetes 完整 Demo

一个能真跑起来的最小生产级 Go 服务：HTTP + graceful shutdown + Prometheus 指标 +
多阶段 Docker 构建 + 一整套 K8s 部署清单。每个文件都带面试向注释，便于对着讲。

## 目录结构

```
demo/
├── go.mod                  # 依赖清单（prometheus/client_golang）
├── Dockerfile              # 多阶段构建 → distroless nonroot 运行镜像
├── .dockerignore           # 减小构建上下文
├── Makefile                # 常用命令入口
├── README.md
├── cmd/                    # Go 源码目录（遵循 cmd/<binary>/ 惯例）
│   └── server/
│       └── main.go         # HTTP 服务入口：路由、探针、优雅停机、指标、日志
└── k8s/
    ├── namespace.yaml      # 命名空间
    ├── configmap.yaml      # 非敏感配置
    ├── secret.yaml         # 敏感配置（演示用明文，生产用 Sealed/External Secrets）
    ├── deployment.yaml     # 主部署：副本、探针、资源、安全上下文、拓扑分布
    ├── service.yaml        # ClusterIP Service
    ├── ingress.yaml        # nginx Ingress
    ├── hpa.yaml            # 基于 CPU/内存的自动伸缩
    └── pdb.yaml            # PodDisruptionBudget，主动驱逐可用性保障
```

**关于 `cmd/`**：Go 官方惯例，每个可执行程序放在 `cmd/<binary-name>/` 下。好处是
Dockerfile / Makefile 里的构建路径能明确对应到源码位置，多二进制项目（比如同一个 repo
里既有 HTTP server 又有 background worker）也能自然组织成 `cmd/server/` 和
`cmd/worker/`。业务共享代码放 `internal/`（只允许本 module 内部引用）或 `pkg/`（可对外复用）。

## 快速开始

### 1) 本地直接跑

```bash
make tidy      # 拉依赖
make run       # 启动，监听 8080
```

另开一个终端：

```bash
curl http://localhost:8080/api/hello
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/metrics | head -20
```

Ctrl+C 触发优雅停机，能看到日志里依次输出：
`shutdown signal received` → `readiness disabled, draining traffic` → `server stopped cleanly`。

### 2) Docker 跑

```bash
make docker            # 构建镜像
make docker-run        # 前台跑，映射 8080:8080
```

镜像大小参考：distroless base + 静态二进制，通常 **10~20 MB**。

### 3) Kubernetes 跑

需要一个可用的集群（minikube / kind / 云上托管都行）。

```bash
# 用 minikube 时先把镜像加载进去（否则会拉不到 latest）
minikube image load go-k8s-demo:latest

# 部署
make k8s-apply

# 观察
make k8s-status
make k8s-logs

# 转发端口本地测试
make k8s-portforward
# 另开终端：curl http://localhost:8080/api/hello

# 清理
make k8s-delete
```

## 面试知识点索引

按面试常问频率排序，指向具体文件的具体位置。

### 高频

| 知识点 | 位置 |
|---|---|
| liveness vs readiness 区别 & 各自失败后果 | `main.go` /healthz /readyz 定义处；`k8s/deployment.yaml` livenessProbe / readinessProbe |
| graceful shutdown 完整时序 | `main.go` 优雅停机段落（第 6 步注释） |
| SIGTERM → endpoints 摘除 → SIGKILL 时序 | `main.go` 停机注释 + `k8s/deployment.yaml` terminationGracePeriodSeconds |
| requests vs limits，CFS throttle / OOMKilled | `k8s/deployment.yaml` resources 段 |
| HPA 副本数计算公式 & 抖动控制 | `k8s/hpa.yaml` behavior 段 |
| Deployment 滚动升级 maxSurge/maxUnavailable | `k8s/deployment.yaml` strategy 段 |
| 多阶段 Docker 构建 & 静态二进制 | `Dockerfile` Stage 1/2 |

### 中频

| 知识点 | 位置 |
|---|---|
| distroless vs alpine vs scratch | `Dockerfile` Stage 2 注释 |
| PID 1 / ENTRYPOINT exec 形式 与信号传递 | `Dockerfile` 末尾 |
| PDB 覆盖的场景 & 不覆盖的场景 | `k8s/pdb.yaml` |
| topologySpreadConstraints 高可用意义 | `k8s/deployment.yaml` 末尾 |
| Service 四种类型 & kube-proxy 转发 | `k8s/service.yaml` |
| Ingress vs Gateway API | `k8s/ingress.yaml` |
| ConfigMap / Secret 消费方式（env vs volume） | `k8s/configmap.yaml`、`k8s/secret.yaml` |
| Secret 只是 base64 不是加密 & 生产做法 | `k8s/secret.yaml` |
| Pod 只读根文件系统 + emptyDir | `k8s/deployment.yaml` securityContext + volumes |

### 低频但加分

| 知识点 | 位置 |
|---|---|
| readOnlyRootFilesystem / capabilities drop ALL / seccompProfile | `k8s/deployment.yaml` 两处 securityContext |
| Prometheus Counter/Histogram 标签基数控制 | `main.go` 指标定义处 |
| http.Server 各类 timeout 与 Slowloris | `main.go` server 构造处 |
| Docker 构建缓存分层（go.mod 先拷） | `Dockerfile` Stage 1 |
| revisionHistoryLimit / progressDeadlineSeconds | `k8s/deployment.yaml` spec 顶部 |

## 完整线上流水线（demo 只覆盖到 K8s 落地）

真实生产环境还有这些环节：

1. **代码托管**：GitLab / GitHub → Merge Request 触发 CI
2. **CI**：`go test -race`、`golangci-lint`、`gosec`、`trivy` 扫镜像漏洞
3. **镜像仓库**：Harbor / ACR / ECR，tag 用 commit SHA 而不是 latest
4. **GitOps**：Argo CD / Flux 监听 GitOps 仓库自动同步到集群
5. **多环境**：dev / staging / prod 通过 Kustomize overlay 或 Helm values 区分
6. **观测**：日志 Loki / ELK，指标 Prometheus + Grafana，链路 Jaeger / Tempo
7. **告警**：AlertManager 按错误率 / P99 / SLO 触发，接企业微信 / 钉钉 / PagerDuty
8. **回滚**：`kubectl rollout undo` 或 Argo CD 一键回滚上一个已知好版本

## 常见坑（踩过才懂）

- **Dockerfile 用 shell 形式 ENTRYPOINT**：SIGTERM 被 shell 吃掉，graceful shutdown 失效
- **livenessProbe 探外部依赖**：DB 抖动 → 所有 Pod 被判死 → 全体重启 → 雪崩
- **只有 1 副本 + PDB minAvailable=1**：节点维护时 kubectl drain 会永远卡住
- **CPU limits 设太紧**：GC / 编译突发被 throttle，P99 抖得很难查
- **Secret 明文进 Git**：泄露事故 top 1，务必用 Sealed Secrets / External Secrets
- **latest tag + imagePullPolicy: Always**：不同节点拉到不同版本的镜像，行为不一致
- **没设 terminationGracePeriodSeconds**：默认 30s，如果 preStop sleep + shutdown > 30s 会被强杀
