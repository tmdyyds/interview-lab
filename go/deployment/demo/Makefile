# ============================================================================
# 常用命令集中在 Makefile，方便新人上手也方便 CI 引用同一份逻辑。
# 变量可以在命令行覆盖，比如：make docker IMAGE=harbor.company.com/demo TAG=v1.0.0
# ============================================================================

IMAGE     ?= go-k8s-demo
TAG       ?= latest
NAMESPACE ?= demo

.PHONY: help
help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## 整理 go.mod 依赖
	go mod tidy

.PHONY: build
build: ## 本地编译二进制到 bin/server
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

.PHONY: run
run: ## 本地运行（读默认端口 8080）
	go run ./cmd/server

.PHONY: test
test: ## 跑单元测试，带 race detector 和 coverage
	go test -race -cover ./...

.PHONY: lint
lint: ## 静态检查（需要提前装 golangci-lint）
	golangci-lint run ./...

# ----------------------------------------------------------------------------
# Docker
# ----------------------------------------------------------------------------

.PHONY: docker
docker: ## 构建 Docker 镜像
	docker build -t $(IMAGE):$(TAG) .

.PHONY: docker-run
docker-run: docker ## 构建并本地运行镜像
	docker run --rm -p 8080:8080 --name $(IMAGE) $(IMAGE):$(TAG)

.PHONY: docker-push
docker-push: ## 推送到镜像仓库（IMAGE 需包含 registry 前缀）
	docker push $(IMAGE):$(TAG)

# ----------------------------------------------------------------------------
# Kubernetes
# ----------------------------------------------------------------------------

.PHONY: k8s-apply
k8s-apply: ## 部署到集群（按依赖顺序 apply）
	kubectl apply -f k8s/namespace.yaml
	kubectl apply -f k8s/configmap.yaml
	kubectl apply -f k8s/secret.yaml
	kubectl apply -f k8s/deployment.yaml
	kubectl apply -f k8s/service.yaml
	kubectl apply -f k8s/hpa.yaml
	kubectl apply -f k8s/pdb.yaml
	kubectl apply -f k8s/ingress.yaml

.PHONY: k8s-status
k8s-status: ## 查看部署状态
	kubectl -n $(NAMESPACE) get deploy,pod,svc,ingress,hpa,pdb

.PHONY: k8s-logs
k8s-logs: ## 跟 Pod 日志（-f）
	kubectl -n $(NAMESPACE) logs -f -l app=go-k8s-demo --tail=100

.PHONY: k8s-portforward
k8s-portforward: ## 端口转发到本地 8080
	kubectl -n $(NAMESPACE) port-forward svc/go-k8s-demo 8080:80

.PHONY: k8s-delete
k8s-delete: ## 清理集群里的资源
	kubectl delete -f k8s/ --ignore-not-found=true
