# 开发与测试

## 开发环境

- Go 1.27.1，与 go.mod、CI 和 Docker builder 保持一致。
- Chromium 或 Google Chrome。
- race 检查需要 CGO 和平台 C 编译器。
- Docker、Buildx 和 Compose 用于镜像与容器测试。

### Linux

Debian amd64 的安装示例：

```bash
curl -fLO https://go.dev/dl/go1.27.1.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.27.1.linux-amd64.tar.gz
export PATH=/usr/local/go/bin:$PATH
sudo apt-get update
sudo apt-get install -y chromium build-essential
export PAGEWEAVE_BROWSER_PATH=/usr/bin/chromium
```

arm64 使用 go1.27.1.linux-arm64.tar.gz。安装前核对 [官方下载页](https://go.dev/dl/)的校验值；升级已有工具链时遵循 [Go 安装说明](https://go.dev/doc/install)。其他发行版通过对应包管理器安装浏览器，并设置其实际路径。

### macOS

安装官方 Go 1.27.1 pkg。浏览器可使用 Homebrew 安装：

```bash
brew install --cask chromium
export PAGEWEAVE_BROWSER_PATH='/Applications/Chromium.app/Contents/MacOS/Chromium'
```

Google Chrome 路径：

```bash
export PAGEWEAVE_BROWSER_PATH='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
```

race 检查需要 Command Line Tools：

```bash
xcode-select --install
```

### Windows

安装官方 Go 1.27.1 MSI，PowerShell 安装浏览器并设置路径：

```powershell
winget install --id Google.Chrome --exact
$env:PAGEWEAVE_BROWSER_PATH='C:\Program Files\Google\Chrome\Application\chrome.exe'
go version
```

race 检查使用 Go 支持的 MinGW-w64 C 编译器。

## 获取源码与启动

```bash
git clone https://github.com/DreamDonghao/pageweave.git
cd pageweave
go mod download
go run ./cmd/pageweave
```

运行前设置 PAGEWEAVE_BROWSER_PATH。服务从操作系统读取环境变量，不自动读取 .env；Compose 使用 .env 进行变量替换。配置列表见 [部署文档](DEPLOYMENT.md)。

服务默认监听 7779。停止服务使用 Ctrl+C；接口示例见 [HTTP API](API.md)。

## 检查与测试

```bash
gofmt -l cmd internal
go vet ./...
go tool staticcheck ./...
go test -mod=readonly ./...
go test -mod=readonly -race ./...
```

gofmt -l 有输出表示格式需要修正，使用 gofmt -w 格式化。Staticcheck 的固定版本由 go.mod 的 tool 声明管理。

普通测试不启动浏览器，覆盖请求与错误契约、配置、内容转换、网络策略、并发和发布标签决策。

### 真实浏览器测试

```bash
go test -mod=readonly -tags=browser ./...
```

测试使用本地 HTTP 页面与延迟 JavaScript，不依赖外部网站。通过依赖注入只放行测试服务器的精确 origin；生产接口没有内网放行开关。

浏览器测试覆盖动态加载、可见性、选择器、滚动、隔离、超时/取消、并发、故障恢复、事件订阅回收和正常退出。运行前需设置有效的浏览器路径。

容器内运行同一组浏览器测试：

```bash
docker build --target browser-test -t pageweave:browser-test .
docker run --rm --init --shm-size=256m \
  --security-opt seccomp=deploy/chromium-seccomp.json \
  pageweave:browser-test
```

## 构建与容器验收

```bash
CGO_ENABLED=0 go build -mod=readonly -o build/pageweave ./cmd/pageweave
build/pageweave --version
docker build -t pageweave:local .
bash scripts/container-smoke.sh pageweave:local
```

生产二进制使用 CGO_ENABLED=0；race 检查使用 CGO，不能继承生产构建的关闭设置。

容器验收检查启动、健康、非 root、沙箱参数、错误请求及 SIGTERM 退出，结束后清理容器。不同架构的验收结果应分别记录在 PR 或 Release 检查结果中。

## 项目结构

| 位置 | 职责 |
| --- | --- |
| cmd/pageweave | 启动装配、命令入口和信号处理 |
| internal/config | 环境变量与校验 |
| internal/server | HTTP 路由、健康与传输超时 |
| internal/extraction | 请求处理、浏览器生命周期、内容转换 |
| internal/network | URL/DNS 与出站策略 |
| internal/buildinfo | 嵌入版本与 commit |
| cmd/releaseplan / internal/releaseplan | CI 发布标签规划 |

请求流程为 HTTP 校验 → 总预算与并发准入 → 网络检查 → 独立浏览器上下文 → 渲染快照 → 内容清理与转换 → 响应。

应用持有一个 Chromium 根实例及 Rod 传输，每次请求持有独立 Rod 状态缓存和 incognito 上下文。事件订阅随 context 退出，页面和上下文使用独立短预算清理。

## 修改约定

- 参数变更同步请求模型、默认值、校验、null 语义、行为测试和 API 文档。
- 过滤规则使用 DOM 遍历，补充删除与保留边界的测试素材。
- 配置变更集中解析和校验，同步 .env.example、测试及部署文档。
- 依赖变更运行 go mod tidy，并提交 go.mod/go.sum。

Go module 规则决定版本，go.sum 用于完整性校验。检查命令不应改写 module 文件。编码规范见 [CODING_STYLE.md](CODING_STYLE.md)，贡献流程见 [CONTRIBUTING.md](../CONTRIBUTING.md)，发布步骤见 [RELEASING.md](RELEASING.md)。
