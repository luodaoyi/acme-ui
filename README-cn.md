# acme-ui

[English](README.md)

`acme-ui` 是一个用于 Linux 服务器的临时 Web 控制台，用来远程辅助操作 `acme.sh`。它可以通过浏览器配置 Cloudflare DNS 验证、申请证书、为 nginx 或 HAProxy 安装证书，并执行常见证书维护操作。

`acme-ui` 不是 `acme.sh` 的替代品。账户数据、DNS API 凭据、续签配置、已签发证书和安装 hook 仍然由 `acme.sh` 管理。

作者：[luodaoyi](https://github.com/luodaoyi)

## 安装和运行

在 Linux 服务器上启动 `acme-ui`：

```bash
curl -fsSL https://github.com/luodaoyi/acme-ui/releases/latest/download/acme-ui.sh | bash
```

如果需要把证书写入 `/etc/nginx`、`/etc/haproxy` 或其他 root 拥有的路径，用 root 权限运行：

```bash
curl -fsSL https://github.com/luodaoyi/acme-ui/releases/latest/download/acme-ui.sh | sudo bash
```

启动脚本会把 Linux 二进制下载到临时目录，前台运行进程，并在进程退出后删除下载的二进制。

启动后，终端会输出监听地址和一次性的 `MasterKey`：

```text
acme-ui is running

Listen:    0.0.0.0:43127
Open:      http://<server-ip>:43127
MasterKey: ...
Mode:      foreground, press Ctrl+C to exit
```

在浏览器打开输出的 URL，并输入 `MasterKey`。

## 功能

- 只以前台进程运行，不安装 daemon 或后台服务。
- 默认绑定 `0.0.0.0`，端口随机。
- 每次启动生成当前进程专用的 `MasterKey`。
- Web session 只保存在内存中。
- 可按需使用官方 `https://get.acme.sh` 脚本安装 `acme.sh`。
- 安装 `acme.sh` 前需要二次确认。
- Cloudflare DNS API 参数只注入当前 `acme.sh` 子进程。
- 让 `acme.sh` 自己持久化 DNS 凭据，以便后续自动续签。
- 支持申请、安装、续签、注销和移除证书记录。
- 支持显式卸载已安装的证书文件，不做递归删除。
- 支持 nginx 证书安装。
- 支持 HAProxy combined PEM 生成。
- 浏览器实时显示任务日志，并对敏感值脱敏。

## 安装 acme.sh

如果没有检测到 `acme.sh`，环境页会显示安装入口。该操作会下载并执行官方安装脚本：

```bash
curl -fsSL https://get.acme.sh -o <tmp>/get.acme.sh
sh <tmp>/get.acme.sh
```

可以在 UI 中填写邮箱。填写后会以以下形式传给安装脚本：

```bash
sh <tmp>/get.acme.sh email=admin@example.com
```

这是一次明确的 `acme.sh` 安装操作。官方安装脚本可能会创建 `~/.acme.sh`、shell alias 和每日 cron 任务。

## 证书流程

使用 Cloudflare DNS 验证时，在 Web UI 中填写 Cloudflare 所需参数。`acme-ui` 会把这些值注入到当前选择的 `acme.sh` 命令进程环境中。

DNS 签发成功后，`acme.sh` 会把后续续签需要复用的 DNS 凭据保存到自身配置中，例如：

```text
~/.acme.sh/account.conf
```

这样后续 `acme.sh --cron` 自动续签不需要 `acme-ui` 继续运行。

## 持久化边界

`acme-ui` 本身是临时工具。启动脚本会在退出后删除下载的二进制，Web session 只存在于内存中。

以下内容会按设计保留，因为它们属于 `acme.sh` 或用户选择的证书安装结果：

- `~/.acme.sh/account.conf`
- `~/.acme.sh/<domain>/`
- 官方 `acme.sh` 安装脚本创建的 shell alias 和每日 cron 任务
- `acme.sh` 保存的域名续签配置和 `reloadcmd`
- nginx 或 HAProxy 证书目标文件

## 安全模型

- Web UI 需要启动时生成的 `MasterKey`。
- 修改状态的 API 需要已认证 session 和 CSRF token。
- `acme.sh` 操作映射为固定命令，不暴露任意 shell 输入。
- nginx 和 HAProxy reload 命令由受控选项生成。
- 证书文件卸载只删除显式填写的绝对文件路径，并拒绝删除目录。
- 任务日志会对敏感值脱敏。

## 从源码构建

```bash
go test ./...
go build ./cmd/acme-ui
```

在 Linux 上运行：

```bash
./acme-ui
```

在非 Linux 开发机上临时调试：

```bash
ACME_UI_ALLOW_NON_LINUX=1 go run ./cmd/acme-ui
```

## 发布

推送 `v*` tag 会触发 GitHub Actions 发布流程：

```bash
git tag v0.1.0
git push origin v0.1.0
```

发布产物：

- `acme-ui-linux-amd64`
- `acme-ui-linux-arm64`
- `acme-ui.sh`
- `checksums.txt`
