# Chromium seccomp 配置

[chromium-seccomp.json](chromium-seccomp.json) 基于 [Docker/Moby 默认 seccomp 配置](https://github.com/moby/profiles/blob/main/seccomp/default.json)，追加允许 clone、setns 和 unshare，供非 root Chromium 创建沙箱命名空间。clone3 保留基线行为，其他系统调用继续受默认限制。

基线许可证见 [SECCOMP_LICENSE](SECCOMP_LICENSE)。Docker 配置说明见 [seccomp 文档](https://docs.docker.com/engine/security/seccomp/)。

## 使用

在仓库根目录执行：

```bash
docker run --rm --init --shm-size=256m \
  --security-opt seccomp=deploy/chromium-seccomp.json \
  pageweave:local
```

路径由 Docker CLI 读取，相对于执行命令的目录。异地执行时使用绝对路径。compose.yaml 已引用此配置。

## 主机要求

主机内核需支持非特权用户命名空间。AppArmor/SELinux 也可能限制 Chromium 创建命名空间；应为部署环境配置适当策略。

此文件配合 Chromium 沙箱使用，不代替网络层的出站访问控制。升级 Docker、内核或 Chromium 后，运行 browser-tag 和容器验收。不要以 --no-sandbox、privileged、host IPC/network 或 seccomp=unconfined 替代沙箱配置。
