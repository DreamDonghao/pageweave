# HTTP API

接口使用 UTF-8 JSON。每次响应包含 X-Request-ID；提取响应中的 request_id 与该响应头一致。

## POST /extract

请求 Content-Type 为 application/json，可附带 charset。只提供 url 即可调用：

```json
{
  "url": "https://example.com/article"
}
```

完整请求示例：

```json
{
  "url": "https://example.com/article",
  "format": "markdown",
  "content_scope": "full",
  "include_links": true,
  "include_images": false,
  "max_chars": 12000,
  "wait_for_selector": null,
  "scroll": false
}
```

### 参数

| 字段 | 默认值 | 规则 |
| --- | --- | --- |
| url | 必填 | 绝对 HTTP/HTTPS URL，无用户名密码，只允许公共目标 |
| format | markdown | markdown 或 text |
| content_scope | full | full：清理后的整页；main：正文 |
| include_links | true | false 去掉链接目标，保留锚文本 |
| include_images | false | 默认保留非空 alt；开启后输出图片引用/地址，不返回图片字节 |
| max_chars | 12000 | 1～PAGEWEAVE_MAX_OUTPUT_CHARS，默认配置上限 50000 |
| wait_for_selector | null | 非空 CSS 选择器，最多 256 个 Unicode code point |
| scroll | false | 最多执行 PAGEWEAVE_MAX_SCROLL_STEPS 次滚动 |

省略字段使用默认值，显式 false 保留。只有 wait_for_selector 接受 null；其他字段为 null 时返回 422。显式 0 按字段范围校验。

请求必须是单个 JSON 对象，未知字段、第二个 JSON 和尾部数据返回 400。请求体默认最大 16384 字节。

### 内容范围与格式

full 返回清理后的页面主体，适用于首页和列表页。main 使用 Readability 提取正文；失败时尝试清理后的 main/article 或主体，并附 fallback_full_content 警告。

清理去除脚本、样式、隐藏节点、导航、页面级页脚和表单等噪声。合法短文本及正文中的重复词保留。

Markdown 支持标题、列表、引用、表格和代码块。text 输出结构化纯文本，没有 Markdown 链接语法。相对链接和图片地址依据最终页面 URL 与有效的 HTTP(S) base URL 解析。javascript/data 等目标不输出。

图片关闭时保留有用的替代文字；开启时 Markdown 输出引用，text 输出替代文字和地址。

### 动态等待与滚动

服务观察内容变化，默认观察上限为 5 秒，最少观察 500ms，每 100ms 采样，连续三次稳定后捕获。观察是启发式，不能证明所有后台任务都已完成。

wait_for_selector 额外等待目标可见且具有非空文本，仍受同一总预算约束。选择器只控制等待时机，不将输出范围缩小到目标元素。

搜索或列表页调用示例：

```bash
curl -sS http://127.0.0.1:7779/extract \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/search?q=example","content_scope":"full","wait_for_selector":"#results .result"}'
```

选择器应依据目标页面的实际结构设置；目标没有出现或始终没有可见文本时返回 504。

scroll 开启后按服务配置有限滚动，无新增内容或预算不足时停止。页面没有有效内容时返回 no_content。只有加载提示或辅助入口标签的占位页继续等待，等待结束后仍无正文则返回错误。

### 成功响应

HTTP 200，响应结构示例：

```json
{
  "request_id": "req-example",
  "url": "https://example.com/article",
  "final_url": "https://example.com/article",
  "title": "页面标题",
  "format": "markdown",
  "content_scope": "full",
  "content": "# 页面标题\n\n清理后的页面内容",
  "truncated": false,
  "warnings": [],
  "duration_ms": 1320
}
```

| 字段 | 含义 |
| --- | --- |
| request_id | 请求标识，可关联运行日志 |
| url | 校验后的输入 URL |
| final_url | 捕获 DOM 时的页面地址 |
| title | 页面标题，缺失时为空字符串 |
| format / content_scope | 请求使用的格式与范围 |
| content | 提取后的内容 |
| truncated | 是否达到输出字符上限 |
| warnings | 警告数组，无警告时为空数组 |
| duration_ms | 提取耗时，单位毫秒 |

max_chars 只计算 content 的 Unicode code point。截断不会破坏 UTF-8 字节序列，但可能截断 Markdown 结构。

| 警告 | 含义 |
| --- | --- |
| fallback_full_content | 正文模式采用了清理内容兜底 |
| render_wait_limit_reached | 观察到上限，返回已获得的有效内容 |
| scroll_limit_reached | 滚动到配置上限，页面仍在新增内容 |

## 错误响应

```json
{
  "request_id": "req-example",
  "error": {
    "code": "extraction_timeout",
    "message": "网页提取超过服务端时间限制"
  }
}
```

| HTTP | code | 场景 |
| --- | --- | --- |
| 400 | invalid_json | JSON 语法/类型、未知字段或尾部数据错误 |
| 403 | blocked_url | 初始/最终 URL、主框架重定向或 DNS 地址被策略拒绝 |
| 413 | content_too_large | 请求体或 HTML 快照超限 |
| 415 | unsupported_media_type | Content-Type 不是 application/json |
| 422 | invalid_request / no_content | 参数或 CSS 错误、无有效内容 |
| 429 | too_many_requests | 槽位满，附 Retry-After: 1 |
| 502 | upstream_error | 连接错误、非 HTML、最终文档 HTTP 4xx/5xx |
| 503 | browser_unavailable | 浏览器初始化、恢复或退出中 |
| 504 | extraction_timeout | 总预算、导航预算或目标等待耗尽 |
| 500 | internal_error | 未预期内部错误 |

未知路径返回 404/not_found；错误方法返回 405/method_not_allowed 并附 Allow。无关子资源被阻止不会自动导致整个提取失败。错误不包含上游整页 HTML 或内部路径。

## 健康检查

| 接口 | 成功 | 不可用 |
| --- | --- | --- |
| GET /health/live | 200，HTTP 服务存活 | — |
| GET /health/ready | 200，浏览器可接受请求 | 503，初始化/恢复/退出中 |

健康响应为 {"status":"ok"} 或 {"status":"unavailable"}。处理槽位满不改变 readiness。

## 安全与支持范围

请求不能覆盖服务预算、并发、网络策略和浏览器参数，也不能传入 Cookie、JavaScript、任意 headers 或文件路径。

Worker、SharedWorker、ServiceWorker、WebSocket、iframe/object 网络加载和新窗口被禁用。普通公共 HTTP/HTTPS 脚本、CSS、图片和 fetch 请求仍由出站策略校验。登录、验证码、任意点击、封闭 Shadow DOM、canvas、PDF/OCR 和批量爬虫不在支持范围内。

转换得到的 Markdown 不等于 HTML 安全清洗。将其渲染为 HTML 的应用应采用自己的安全策略。
