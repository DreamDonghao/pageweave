# 发布流程

## 版本来源

服务版本的唯一来源是 internal/buildinfo/VERSION，通过 go:embed 进入二进制。commit 信息通过构建参数注入。版本采用不含 +build 元数据的 SemVer，数字 prerelease 段不使用前导零。

| VERSION | Git 标签 | Docker 精确标签 |
| --- | --- | --- |
| 0.1.0 | v0.1.0 | 0.1.0 |
| 0.2.0-beta.1 | v0.2.0-beta.1 | 0.2.0-beta.1 |

## 发布前检查

同步版本、变更记录及涉及的 API、配置和部署文档，完成 [开发文档](DEVELOPMENT.md)中的 Go、浏览器和容器检查。

CI 在 main push 和 pull request 时检查格式、静态分析、普通测试、race、原生双架构浏览器及容器运行。PR 不登录镜像仓库，也不依赖发布 Secrets。

## 镜像仓库配置

GHCR 使用 GITHUB_TOKEN，构建与 manifest 作业具有 packages:write 权限。首次推送后，可在 GitHub package 设置中调整可见性。

启用 Docker Hub 发布需要配置仓库 Actions Secrets：

| Secret | 用途 |
| --- | --- |
| DOCKERHUB_USERNAME | Docker Hub 账号 |
| DOCKERHUB_TOKEN | 有目标仓库推送权限的访问令牌 |

缺少 Docker Hub 凭据时，工作流只发布 GHCR。镜像路径由 GitHub 仓库及 Secrets 派生，应用源码不绑定发布账号。

## 创建 Release

1. 更新 VERSION 和 CHANGELOG.md，提交到 main。
2. 在对应提交上创建 v<版本> 标签并推送。
3. 创建对应 GitHub Release；测试版本设置 prerelease。

`.github/workflows/docker-release.yml` 由 release published 触发，包括 prerelease。工作流 checkout 对应标签，并验证标签与 VERSION 一致。

手动重跑时，在 Actions 中选择 Docker release，输入已发布 Release 的标签。构建来源仍为该标签，不使用 main 替代。

## 构建与标签规则

amd64 和 arm64 在原生 runner 分别构建并运行测试，验证成功后按 digest 推送。两个平台均成功后才合并 manifest 并设置最终标签。

- 每次发布生成精确版本和 sha-<12位commit> 标签。
- 正式版本可更新 major.minor；latest 指向最新正式版本。
- 版本串或 Release 标记任一为 prerelease 时，不更新稳定别名。
- 重跑旧版本不会回退 latest 或较新的同系列别名。

发布工作流串行化执行，并在设置别名前重新读取 Release 列表。OCI labels 包含源码仓库、版本、commit 和 AGPL-3.0-only。镜像发布后，Release 附带 compose.yaml、chromium-seccomp.json 和 .env.example。

构建摘要记录平台、Go 和 Chromium 版本。Go 与基础镜像固定，APT 安全更新仍可能改变浏览器及系统包版本。
