# syntax=docker/dockerfile:1.7

# ============================================================================
# 多阶段构建 —— Go 部署镜像的行业标准做法
#
# 好处：
#   1. 编译阶段用 golang 官方镜像（几百 MB），运行阶段用 distroless（几 MB）
#   2. 编译工具链不进最终镜像，攻击面小、拉取快
#   3. 利用 layer cache：先拷 go.mod 再拷源码，依赖不变时能命中缓存
# ============================================================================


# ----------------------------------------------------------------------------
# Stage 1: 构建
# ----------------------------------------------------------------------------
# 用具体版本 tag，不要 latest。构建结果要可复现，
# 六个月后重跑同一个 commit，产出的二进制应该一致。
FROM golang:1.23-alpine AS builder

# CGO_ENABLED=0 → 静态编译，产物不依赖 glibc，才能塞进 scratch/distroless
# GOOS/GOARCH  → 显式指定目标平台，跨平台构建（比如在 M 芯片 Mac 上构建 amd64）时是必需的
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /src

# 先拷依赖清单，单独一层。只要 go.mod/go.sum 不变，
# `go mod download` 这一层就能被 Docker 缓存复用，构建快很多。
COPY go.mod go.sum ./
RUN go mod download

# 显式拷贝源码目录（而不是 `COPY . .`），Dockerfile 里一眼能看出哪个是源码。
# 项目遵循 Go 惯例：
#   cmd/<binary>/  main 包，每个可执行文件一个子目录
#   internal/      项目内部包（如果有）
#   pkg/           可对外复用的包（如果有）
COPY cmd/ ./cmd/
# 如果有 internal / pkg，也在这里加：
# COPY internal/ ./internal/
# COPY pkg/ ./pkg/

# 构建参数说明：
#   -trimpath          从二进制里去掉构建机的绝对路径，让产物可复现
#   -ldflags="-s -w"   去掉符号表和调试信息，二进制体积能小 30% 左右
#                      副作用：panic 时看不到函数名，生产可以配 debug symbol 单独存
#   -o /out/server     产物路径
#   ./cmd/server       构建目标包路径，对应 cmd/server/main.go 的 main 包
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server


# ----------------------------------------------------------------------------
# Stage 2: 运行
# ----------------------------------------------------------------------------
# distroless/static 只含运行静态二进制所需的最小文件：
#   - CA 证书（HTTPS 调用需要）
#   - /etc/passwd 里预置的 nonroot 用户
#   - tzdata（时区信息）
# 没有 shell、没有包管理器、没有 curl，攻击者进来也没什么能做的。
#
# 备选：
#   - gcr.io/distroless/base-debian12  含 glibc，适合 CGO 打开的场景
#   - alpine:3.20                       含 sh，如果 preStop 用 exec sleep 需要用它
#   - scratch                           完全空，连 CA 证书都没有，慎用
FROM gcr.io/distroless/static-debian12:nonroot

# 以 nonroot 用户运行（UID/GID 65532），
# 配合 K8s Pod securityContext.runAsNonRoot: true 做双重保险。
USER nonroot:nonroot

# 从构建阶段拷贝二进制。用 --chown 保证文件属主是 nonroot（distroless 默认已经是了，写清楚更保险）
COPY --from=builder --chown=nonroot:nonroot /out/server /server

EXPOSE 8080

# 用 exec 形式（JSON 数组），让 PID 1 就是我们的 Go 进程。
# 如果用 shell 形式 `ENTRYPOINT /server`，SIGTERM 会被 shell 吃掉，
# graceful shutdown 就失效了。
ENTRYPOINT ["/server"]
