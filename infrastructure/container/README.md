# 容器化

## Docker

- 与 VM 的区别
- 镜像分层与 COW 文件系统（OverlayFS）
- 命名空间（Namespace）与 cgroups
- Dockerfile 最佳实践（多阶段构建、缓存、体积优化）
- 网络模式：bridge、host、none、container、overlay
- 数据卷（Volume）与 bind mount
- 常用命令与排查

## Kubernetes

- 核心概念：Pod、Deployment、ReplicaSet、StatefulSet、DaemonSet
- Service（ClusterIP / NodePort / LoadBalancer）、Ingress
- ConfigMap、Secret
- 命名空间与 RBAC
- 探针：Liveness / Readiness / Startup
- 资源限制：Requests / Limits、QoS
- 调度：亲和性、污点与容忍
- HPA / VPA 弹性伸缩
- 网络：CNI、Service Mesh（Istio）
- 存储：PV / PVC / StorageClass
- Operator 模式
- 常用排查命令（`kubectl describe` `logs` `exec` `port-forward`）

## 面试常问

- Docker 底层原理（namespace + cgroups + rootfs）
- K8s 中 Pod 与容器的关系
- Service 与 Ingress 的区别
- Deployment 滚动更新流程
