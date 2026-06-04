# acme-ui

`acme-ui` 是一个 Linux-only 的临时 Web 控制台，用来远程辅助配置 `acme.sh`、Cloudflare DNS 验证、nginx / HAProxy 证书安装和常见证书操作。

它不是 acme.sh 的替代品。证书账户、DNS 凭据、续签配置和安装记录仍由 acme.sh 管理。

## 使用方式

发布后可以这样启动：

```bash
curl -fsSL https://github.com/<owner>/acme-ui/releases/latest/download/acme-ui.sh | bash
```

开发或未替换仓库名时：

```bash
ACME_UI_REPO=<owner>/acme-ui curl -fsSL https://raw.githubusercontent.com/<owner>/acme-ui/main/scripts/acme-ui.sh | bash
```

启动后终端会显示随机端口和 `MasterKey`：

```text
Listen:    0.0.0.0:43127
Open:      http://<server-ip>:43127
MasterKey: ...
Mode:      foreground, press Ctrl+C to exit
```

## 特性

- 前台运行，不安装后台服务。
- 默认绑定 `0.0.0.0`，端口随机。
- 每次启动生成一次性 `MasterKey`。
- 登录 session 只存在内存中。
- 环境页可用官方 `https://get.acme.sh` 脚本安装 acme.sh，邮箱可选。
- Cloudflare DNS API 参数只注入当前 acme.sh 子进程。
- acme.sh 成功申请后，由 acme.sh 自己把续签所需配置保存到 `~/.acme.sh/account.conf`。
- 支持申请、安装、续签、注销、移除续签记录。
- 支持显式卸载安装文件，非递归删除，不删除目录。
- 支持 nginx 证书安装。
- 支持 HAProxy combined PEM 生成。
- 任务日志实时输出，敏感信息脱敏。

## 持久化边界

工具自身退出后不保留配置。通过 `curl | bash` 启动时，下载的二进制会放到临时目录，进程退出后删除。

以下文件属于 acme.sh 或用户选择的业务结果，会保留：

- `~/.acme.sh/account.conf`
- `~/.acme.sh/<domain>/`
- 官方 acme.sh 安装脚本创建的 alias 和 daily cron job
- acme.sh 保存的 domain 配置和 `reloadcmd`
- nginx / HAProxy 的证书目标路径
- 用户显式安装的 acme.sh cron 任务

## 权限

工具不会在 Web 页面里要求 sudo 密码。它以当前启动用户的权限运行。

如果要写 `/etc/nginx`、`/etc/haproxy` 或执行 `systemctl reload`，通常需要用 root 启动：

```bash
curl -fsSL https://github.com/<owner>/acme-ui/releases/latest/download/acme-ui.sh | sudo bash
```

## 本地构建

```bash
go test ./...
go build ./cmd/acme-ui
```

Linux 运行：

```bash
./acme-ui
```

开发机非 Linux 临时调试：

```bash
ACME_UI_ALLOW_NON_LINUX=1 go run ./cmd/acme-ui
```

## 发布

推送 tag 会触发 GitHub Actions：

```bash
git tag v0.1.0
git push origin v0.1.0
```

发布产物：

- `acme-ui-linux-amd64`
- `acme-ui-linux-arm64`
- `acme-ui.sh`
- `checksums.txt`
