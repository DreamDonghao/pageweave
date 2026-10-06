# 部署与配置

## 环境要求

Docker 部署需要 Docker Engine 或 Docker Desktop、Buildx 和 Compose 插件。Linux 使用 [Docker Engine 官方安装步骤](https://docs.docker.com/engine/install/)；macOS 和 Windows 使用 [Docker Desktop](https://docs.docker.com/desktop/setup/install/)。Windows 选择 Linux 容器。

安装后检查：

```bash
docker info
docker compose version
docker run --rm hello-world
```

默认并发建议预留至少 2 GiB 内存及 256 MiB 共享内存，实际容量取决于网页复杂度。通过监控内存峰值调整资源和并发。

## 使用发布镜像

拉取镜像后直接创建容器，不需要 Go 或源码编译：

```bash
curl -fsSL https://github.com/DreamDonghao/pageweave/releases/download/v0.1.0/chromium-seccomp.json -o chromium-seccomp.json
docker pull ghcr.io/dreamdonghao/pageweave:0.1.0
docker run -d --name pageweave --init --shm-size=256m \
  --security-opt seccomp=./chromium-seccomp.json \
  -p 127.0.0.1:7779:7779 \
  ghcr.io/dreamdonghao/pageweave:0.1.0
```

GHCR 镜像为 ghcr.io/dreamdonghao/pageweave。正式部署可固定精确版本或 digest；latest 为最新正式版本。

只下载 Compose 部署文件：

```bash
mkdir -p deploy
curl -fsSL https://github.com/DreamDonghao/pageweave/releases/download/v0.1.0/compose.yaml -o compose.yaml
curl -fsSL https://github.com/DreamDonghao/pageweave/releases/download/v0.1.0/chromium-seccomp.json -o deploy/chromium-seccomp.json
docker compose up -d
```

## 从源码构建

在仓库根目录执行：

```bash
docker build -t pageweave:local .
```

镜像包含服务二进制、Chromium、CA 证书和系统依赖，以 UID/GID 10001 运行。Go 与基础镜像在 Dockerfile 中固定；APT 安全更新可能改变 Chromium 和系统包版本。

构建支持 `COMMIT` 和 `APT_MIRROR` 参数：

```bash
docker build --build-arg COMMIT="$(git rev-parse HEAD)" -t pageweave:local .
docker build --build-arg APT_MIRROR=http://mirrors.ustc.edu.cn -t pageweave:local .
```

APT_MIRROR 默认使用 Debian 官方源，替代镜像源须提供 /debian 和 /debian-security。APT 签名与包校验保持启用。

## Docker Compose

在仓库根目录执行：

```bash
PAGEWEAVE_IMAGE=pageweave:local docker compose up -d
docker compose exec pageweave pageweave healthcheck
docker compose logs -f pageweave
```

使用 GHCR 镜像时：

```bash
docker compose pull
docker compose up -d
```

`compose.yaml` 使用 Docker init、256 MiB 共享内存、健康检查、20 秒停止宽限和重启策略。默认网络为 pageweave-tools，不映射宿主端口。

### 接入其他应用

任何支持 HTTP 的应用都可以调用 PageWeave。容器加入相同网络后，使用 `http://pageweave:7779/extract`：

```bash
docker network connect pageweave-tools <应用容器名>
```

接入已有网络时，创建 compose.override.yaml：

```yaml
networks:
  tools:
    external: true
    name: app-network
```

容器中的 localhost 指向容器自身。PageWeave 不需要持久化卷，也不需要挂载 Docker socket。

### 本地端口访问

在 compose.override.yaml 中添加：

```yaml
services:
  pageweave:
    ports:
      - "127.0.0.1:7779:7779"
```

## docker run

以下命令使用本地镜像并开放回环端口：

```bash
docker run -d --name pageweave --init \
  --restart unless-stopped --stop-timeout 20 --shm-size=256m \
  --security-opt seccomp=deploy/chromium-seccomp.json \
  -p 127.0.0.1:7779:7779 \
  pageweave:local
```

seccomp 文件路径相对于执行命令的目录，也可使用绝对路径。容器健康检查调用 `pageweave healthcheck`，无需额外安装 curl。

## 环境变量

| 环境变量 | 默认值 | 用途 |
| --- | --- | --- |
| PAGEWEAVE_HOST | 0.0.0.0 | HTTP 绑定地址 |
| PAGEWEAVE_PORT | 7779 | HTTP 端口 |
| PAGEWEAVE_BROWSER_PATH | /usr/bin/chromium | 浏览器可执行文件 |
| PAGEWEAVE_MAX_CONCURRENCY | 2 | 同时处理的请求数 |
| PAGEWEAVE_REQUEST_TIMEOUT_SECONDS | 25 | 单请求总预算 |
| PAGEWEAVE_NAVIGATION_TIMEOUT_SECONDS | 10 | 导航阶段上限 |
| PAGEWEAVE_RENDER_WAIT_SECONDS | 5 | 动态观察阶段上限 |
| PAGEWEAVE_MAX_HTML_BYTES | 2097152 | HTML 快照字节上限 |
| PAGEWEAVE_MAX_REQUEST_BYTES | 16384 | API 请求体字节上限 |
| PAGEWEAVE_MAX_OUTPUT_CHARS | 50000 | 正文 Unicode code point 上限 |
| PAGEWEAVE_MAX_SCROLL_STEPS | 3 | 最大滚动次数 |
| PAGEWEAVE_LOG_LEVEL | INFO | DEBUG、INFO、WARN 或 ERROR |

配置在启动时校验；空值和非法值会导致启动失败。服务不自动读取 .env；Compose 用 .env 做变量替换。

阶段上限受总预算约束，不相加。页面和浏览器上下文清理使用独立短预算；HTML 解析的取消是协作式的，请求预算不是进程硬实时上限。调用方的 HTTP 超时应给响应和清理留出余量。

若 MAX_OUTPUT_CHARS 小于默认 max_chars（12000），请求需显式指定不超过配置上限的 max_chars。

## 健康、日志与退出

- GET /health/live：HTTP 服务存活返回 200。
- GET /health/ready：浏览器可接受请求返回 200；恢复或退出中返回 503。
- pageweave --version：显示服务版本和 commit。
- pageweave healthcheck：以短超时检查 readiness。

日志为结构化 JSON，输出到 stdout/stderr，提取事件包含请求 ID、耗时、结果长度和警告。默认不记录网页正文、Cookie、授权头或 URL 查询参数。

浏览器故障后停止接收提取请求，最多重建三次；恢复耗尽后退出，由容器重启策略接管。SIGTERM/SIGINT 触发有限等待、在途取消和资源清理。

## 沙箱与出站访问

Chromium 沙箱保持启用，CDP 绑定容器回环地址，不映射管理端口。随仓库提供的 seccomp 配置允许 Chromium 创建沙箱命名空间；内核需支持非特权用户命名空间。

应用校验初始 URL、DNS 全部地址、重定向和子请求。网络层应进一步阻止容器访问私网、回环和本地服务。通过内网或带认证的网关提供访问。

出现 No usable sandbox 或 Operation not permitted 时，检查用户命名空间、seccomp、AppArmor/SELinux。部署配置说明见 [Chromium seccomp](../deploy/README.md)。

## 更新与回滚

以精确版本更新 Compose 服务：

```bash
PAGEWEAVE_IMAGE=ghcr.io/dreamdonghao/pageweave:<版本> docker compose pull
PAGEWEAVE_IMAGE=ghcr.io/dreamdonghao/pageweave:<版本> docker compose up -d
```

回滚时指定旧精确版本并执行相同命令。正式环境可固定镜像 digest。

本地重建镜像后，Compose 会在 up 时重新创建服务。docker run 创建的容器需要停止并移除，再按启动命令重新创建；docker restart 不切换镜像。

## 排错

| 现象 | 检查方向 |
| --- | --- |
| readiness 503 | 浏览器路径、启动日志、恢复状态和沙箱配置 |
| 429 | 调用并发与资源限制，遵循 Retry-After |
| 504 | 页面加载时间、目标选择器和服务预算 |
| 502 | 最终文档状态、HTML 类型、证书和上游连接 |
| 413 | 请求体及 HTML 快照上限；max_chars 不改变快照上限 |
| 容器 OOM | 宿主内存、共享内存和并发数 |

停止 Compose 服务使用 docker compose down；停止单个容器使用 docker stop pageweave。
