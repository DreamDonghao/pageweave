# 贡献指南

开始前查找已有 Issue/PR。大功能及公开契约变更先说明目标、兼容性和迁移方式。

Bug 请提供版本、平台、镜像、脱敏复现请求、预期/实际结果和脱敏日志；区分浏览器故障与上游访问限制。不要提交密钥、Cookie、私有网页、日志、二进制、浏览器下载目录或本地配置。

默认分支 main；主题分支 `<类别>/<小写英文连字符描述>`。类别为 feat、fix、refactor、docs、test、build、ci、perf、chore，与主要提交类型一致。

提交与 PR 标题采用 Conventional Commits：`<类型>(<范围>): <简短中文摘要>`，范围例如 api/browser/content/docker，摘要末尾不加句号。正文空一行，每条 `- ` 说明原因、关键实现或兼容影响，Issue 脚注单独放。破坏性变更用 ! 和 BREAKING CHANGE: 并说明迁移。

```text
feat(content): 支持网页正文转 Markdown

- 保留正文标题、列表和表格
- 按最终页面地址解析相对链接
- 补充中文与代码块测试

Closes #4
```

提交前运行 gofmt、vet、Staticcheck、相关 go test；浏览器/依赖/Docker 改动增加真实浏览器及镜像运行检查。race 使用支持平台及 C 编译器。改参数、默认值、输出和部署同步文档；避免无关重构/格式化。

PR 描述最终行为、实现要点、实际检查结果与限制。未运行的架构/环境明确说明，不能用 manifest 或 mock 代替 Chromium 验证。

项目许可为 AGPL-3.0-only；维护者与贡献者分开署名。
