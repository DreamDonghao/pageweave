# PageWeave

PageWeave 是一个独立的网页内容提取服务。它使用无头 Chromium 加载网页并执行 JavaScript，将渲染后的页面转换为 Markdown 或纯文本，通过 HTTP API 返回内容、标题和来源网址。

## 功能

- 提取清理后的整页内容，或使用 Readability 提取文章正文。
- 输出 Markdown 或纯文本，支持标题、列表、引用、表格和代码块。
- 配置链接、图片引用、输出长度、目标选择器等待和有限滚动。
- 复用 Chromium，每次请求使用独立浏览器上下文，隔离 Cookie 和 localStorage。
- 提供请求超时、并发限制、浏览器故障恢复、健康检查和结构化日志。
- 支持本地运行和 Docker 部署，无需数据库或外部 LLM。

## 快速开始

### Docker

在仓库根目录构建并启动：

```bash
docker build -t pageweave:local .
docker run -d --name pageweave --init --shm-size=256m \
  --security-opt seccomp=deploy/chromium-seccomp.json \
  -p 127.0.0.1:7779:7779 pageweave:local
```

服务就绪后检查健康状态：

```bash
docker exec pageweave pageweave healthcheck
```

发布工作流的镜像地址：

| 仓库 | 地址 |
| --- | --- |
| GHCR | ghcr.io/dreamdonghao/pageweave |
| Docker Hub | dreamdonghao/pageweave |

镜像发布目标为 `linux/amd64` 和 `linux/arm64`。Docker Hub 发布由仓库 Secrets 启用。Compose、网络、更新和回滚说明见 [部署文档](docs/DEPLOYMENT.md)。

### 本地运行

安装 Go 1.27.1 和 Chromium 或 Google Chrome，设置浏览器路径后启动：

```bash
export PAGEWEAVE_BROWSER_PATH=/usr/bin/chromium
go mod download
go run ./cmd/pageweave
```

macOS 上的 Google Chrome 路径：

```bash
export PAGEWEAVE_BROWSER_PATH='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
```

服务不自动下载浏览器。各平台的环境安装说明见 [开发文档](docs/DEVELOPMENT.md)。

## 调用接口

```bash
curl -sS http://127.0.0.1:7779/extract \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","format":"markdown"}'
```

响应结构示例：

```json
{
  "request_id": "req-example",
  "url": "https://example.com",
  "final_url": "https://example.com/",
  "title": "Example Domain",
  "format": "markdown",
  "content_scope": "full",
  "content": "# Example Domain\n\n页面内容",
  "truncated": false,
  "warnings": [],
  "duration_ms": 1200
}
```

`content_scope` 默认 `full`，返回清理后的整页，适合首页和列表页。文章可指定 `main` 提取正文。`format` 默认 `markdown`，也可选择 `text`。

动态内容有明确加载目标时，可设置 `wait_for_selector`。它只控制等待时机，不改变输出范围。完整参数、错误码和示例见 [API 文档](docs/API.md)。

## 配置与运行边界

配置统一使用 `PAGEWEAVE_` 环境变量，默认端口为 7779、并发为 2、请求预算为 25 秒。完整配置见 [.env.example](.env.example) 和 [部署文档](docs/DEPLOYMENT.md)。

服务只访问公共 HTTP/HTTPS 地址，拒绝私网、回环等目标。部署网络还需限制浏览器出站访问；DNS 校验不能完整防御 DNS rebinding。

动态稳定观察是启发式。Worker、ServiceWorker、WebSocket、iframe/object 网络加载和新窗口被禁用；依赖登录、验证码、点击展开、封闭 Shadow DOM 或 canvas 的内容不在支持范围内。Markdown 输出不是 HTML 安全清洗，渲染方需要采用自己的安全策略。

## 文档

- [HTTP API](docs/API.md)
- [开发与测试](docs/DEVELOPMENT.md)
- [部署与配置](docs/DEPLOYMENT.md)
- [发布流程](docs/RELEASING.md)
- [编码规范](docs/CODING_STYLE.md)
- [贡献指南](CONTRIBUTING.md)
- [变更记录](CHANGELOG.md)

## 许可证

[AGPL-3.0-only](LICENSE)。
