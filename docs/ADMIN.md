# 管理后台

管理入口为 `/admin/`，默认地址 `http://127.0.0.1:7779/admin/`。页面及资源内置于服务二进制，不需要额外安装前端依赖。

## Token 登录

管理后台没有账号或密码。服务每次启动生成 256 位随机 Token，在启动输出中显示一次：

```text
PageWeave admin token: <当前启动 Token>
```

查看容器启动输出：

```bash
docker logs pageweave
```

将当前 Token 粘贴到登录页面。Token 不通过 URL 传递，也不会由管理接口返回。登录后使用 HttpOnly、SameSite=Strict 的签名会话 Cookie，有效期为 12 小时。

容器重启时生成新 Token，旧 Token 和旧会话失效；网页触发的配置应用只重新加载服务，保留 Token 和当前会话。退出登录会使该会话失效。

Token 等同于后台访问凭据。仅通过本地连接或 HTTPS 使用管理页面，不分享启动日志中的 Token。HTTPS 反向代理部署设置 `PAGEWEAVE_ADMIN_COOKIE_SECURE=true`。

## 查看与修改

后台展示服务版本、监听地址、就绪状态和并发上限。可编辑：

- 并发数与请求总预算。
- 导航和动态观察上限。
- HTML 快照、API 请求体和正文输出上限。
- 滚动次数及日志级别。

监听地址、浏览器路径、管理数据目录、Cookie 安全标记和网络策略仍由部署环境管理，网页不能覆盖。

**保存配置**只保存参数；**保存并应用**会暂时停止新提取、有限等待在途请求，然后在同一容器内重新加载浏览器和 HTTP 服务。页面会自动检查恢复状态。

## 配置持久化

网页配置保存在 `PAGEWEAVE_DATA_DIR/settings.json`，使用受限文件权限和原子替换。已保存的网页配置优先于对应运行参数的环境变量默认值。

容器的数据目录为 `/var/lib/pageweave`，挂载 Docker 管理的数据卷即可保留配置：

```bash
-v pageweave-settings:/var/lib/pageweave
```

不挂载卷时，重建容器会丢失配置。Compose 已定义 settings 卷；`docker compose down` 保留卷，`docker compose down -v` 会删除配置。

本地运行的数据目录默认为操作系统用户配置目录下的 pageweave 子目录，也可通过 PAGEWEAVE_DATA_DIR 覆盖。

## 接口边界

管理登录只保护 `/admin/api/`。现有 `/extract` 与健康接口的访问策略保持独立，部署时通过内网或网关控制提取 API 的访问。

配置写入与应用要求有效会话、CSRF Token 和同源请求。登录接口按来源地址限流。管理页面不可提交 JavaScript、Cookie、任意浏览器参数或私网放行规则。
