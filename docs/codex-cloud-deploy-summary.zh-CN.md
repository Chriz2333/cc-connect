# Codex 云服务器部署 cc-connect 总结

本文档面向未来接手的 Codex/Agent，目标是在一台新设备或云服务器上部署带有 DingTalk 群聊文件回传补丁的 cc-connect。

## 当前版本来源

使用用户 fork：

```text
https://github.com/Chriz2333/cc-connect
```

使用分支：

```text
fix/dingtalk-group-file-send
```

关键提交：

```text
d306cbb fix: support DingTalk group file send
```

不要直接使用 npm 官方包 `cc-connect v1.3.2` 部署这个需求。官方 v1.3.2 的 DingTalk 平台在 `cc-connect send --file` 时会返回：

```text
Error: platform dingtalk: operation not supported by this platform
```

本分支修复的是 DingTalk 文件回传，尤其是 `share_session_in_channel = true` 的群聊共享会话场景。

## 修复内容

问题根因：

- `cc-connect send --file` 有通用文件发送管线。
- DingTalk 最新源码已有 `SendFile`，但原实现默认走单聊 API `robot/oToMessages/batchSend`。
- 当配置 `share_session_in_channel = true` 时，群聊 session key 只有群会话 ID，没有 `senderStaffId`。
- 原实现会用空或错误的 `userIds` 发文件，钉钉返回 `staffId.notExisted`。

本分支改动：

- 群聊文件发送走 `POST /v1.0/robot/groupMessages/send`，使用 `openConversationId`。
- 单聊或按用户隔离的会话继续走 `POST /v1.0/robot/oToMessages/batchSend`，使用 `userIds`。
- 增加 DingTalk 单聊/群聊文件发送单元测试。
- 增加 Windows 构建所需的 `daemon.CheckLinger` stub。

## 新云服务器部署流程

以下示例默认 Linux x86_64 云服务器。

### 1. 安装基础工具

```bash
sudo apt-get update
sudo apt-get install -y git curl wget ca-certificates
```

如果服务器不是 Debian/Ubuntu，使用对应包管理器安装 `git`、`curl`、`wget`。

### 2. 安装 Go

项目 `go.mod` 要求 Go 1.25 或更新版本。推荐直接安装 Go 1.26.3：

```bash
wget https://go.dev/dl/go1.26.3.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.26.3.linux-amd64.tar.gz
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc
source ~/.bashrc
go version
```

期望看到类似：

```text
go version go1.26.3 linux/amd64
```

### 3. 拉取 fork 分支

```bash
git clone https://github.com/Chriz2333/cc-connect.git
cd cc-connect
git checkout fix/dingtalk-group-file-send
```

### 4. 验证源码

```bash
go test ./daemon ./platform/dingtalk ./tests/release_local/media_pipeline
```

期望输出全部为 `ok`。

### 5. 编译 CLI 版本

不需要 Web Admin 时使用 `no_web` 构建，避免构建前端资产：

```bash
go build -tags no_web -o cc-connect ./cmd/cc-connect
./cc-connect --version
```

然后安装到系统路径：

```bash
sudo install -m 0755 ./cc-connect /usr/local/bin/cc-connect
```

## 配置 cc-connect

创建工作目录：

```bash
sudo mkdir -p /opt/cc-workspace
sudo chown -R "$USER:$USER" /opt/cc-workspace
mkdir -p ~/.cc-connect
```

创建 `~/.cc-connect/config.toml`：

```toml
language = "zh"
attachment_send = "on"

[log]
level = "info"

[display]
thinking_messages = false
tool_messages = false

[[projects]]
name = "codex-dingtalk"

[projects.agent]
type = "codex"

[projects.agent.options]
work_dir = "/opt/cc-workspace"
mode = "suggest"
reasoning_effort = "medium"

[[projects.platforms]]
type = "dingtalk"

[projects.platforms.options]
client_id = "your-dingtalk-app-key"
client_secret = "your-dingtalk-app-secret"
allow_from = "*"
share_session_in_channel = true
```

注意：

- `client_id` 是钉钉 AppKey。
- `client_secret` 是钉钉 AppSecret。
- 个人自用可以直接写入配置；多人服务器应改用环境变量或受限权限文件。
- `share_session_in_channel = true` 表示同一个钉钉群共享一个 Agent 会话上下文。
- `attachment_send = "on"` 必须开启，否则 `send --file` 会被 cc-connect 全局拦截。

## 安装 Codex CLI

服务器上需要安装并登录/配置 Codex CLI。具体命令依赖当时 OpenAI/Codex 的官方安装方式；部署时应以官方文档为准。

验证方式：

```bash
codex --version
```

如果 Codex CLI 需要认证，先完成认证，再启动 cc-connect。

## 手动启动与冒烟测试

启动：

```bash
cc-connect --config "$HOME/.cc-connect/config.toml"
```

日志中应看到：

```text
dingtalk: stream connected
cc-connect is running
```

在钉钉群里先发一条消息给机器人，让 cc-connect 建立 session。然后在服务器执行：

```bash
cc-connect sessions list
```

找到群聊共享 session，格式类似：

```text
dingtalk:g:<openConversationId>
```

测试文件发送：

```bash
cc-connect send --file /absolute/path/to/test.pdf -p codex-dingtalk -s "dingtalk:g:<openConversationId>"
```

成功时命令输出：

```text
Message sent successfully.
```

日志中应出现：

```text
dingtalk: file message sent
```

## systemd 常驻运行

创建用户级 systemd 服务：

```bash
mkdir -p ~/.config/systemd/user
nano ~/.config/systemd/user/cc-connect.service
```

写入：

```ini
[Unit]
Description=cc-connect DingTalk bridge
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/cc-connect --config %h/.cc-connect/config.toml
Restart=always
RestartSec=5
WorkingDirectory=%h

[Install]
WantedBy=default.target
```

启用：

```bash
systemctl --user daemon-reload
systemctl --user enable --now cc-connect
loginctl enable-linger "$USER"
systemctl --user status cc-connect
```

查看日志：

```bash
journalctl --user -u cc-connect -f
```

## 常见问题

### 1. `cc-connect send --file` 仍然提示 operation not supported

说明运行的不是本 fork 分支构建出的二进制，而是官方 npm 版本。检查：

```bash
which cc-connect
cc-connect --version
```

确保 `/usr/local/bin/cc-connect` 是从 `fix/dingtalk-group-file-send` 分支编译安装的。

### 2. 钉钉返回 `staffId.notExisted`

通常说明群聊共享 session 被错误地走了单聊 API。检查是否使用本分支，并确认 session key 是：

```text
dingtalk:g:<openConversationId>
```

不要把 `#1` 这种 `sessions list` 编号直接传给 `send -s`。`send -s` 需要真实 session key。

### 3. 文件发送被 cc-connect 拦截

检查配置：

```toml
attachment_send = "on"
```

### 4. 群聊上下文没有共享

检查配置：

```toml
share_session_in_channel = true
```

修改配置后需要重启 cc-connect。

### 5. 云服务器上钉钉收不到消息

逐项检查：

```bash
systemctl --user status cc-connect
journalctl --user -u cc-connect -n 100
cc-connect sessions list
```

钉钉开放平台侧确认：

- 应用机器人已启用。
- Stream 模式已启用。
- AppKey/AppSecret 正确。
- 机器人已添加到目标群。
- 相关消息发送权限已开通。

## 回滚

停止服务：

```bash
systemctl --user stop cc-connect
```

恢复官方版本：

```bash
npm install -g cc-connect
cc-connect --version
```

注意：官方 `v1.3.2` 不支持本次 DingTalk 群聊文件回传修复。

## 给未来 Codex 的任务提示

在新云服务器上部署时，可以直接对 Codex 说：

```text
请读取 docs/codex-cloud-deploy-summary.zh-CN.md，按里面的步骤部署 Chriz2333/cc-connect 的 fix/dingtalk-group-file-send 分支，配置钉钉机器人，并用 cc-connect send --file 测试群聊 PDF 回传。
```

执行过程中不要把 AppSecret、Codex token 或其他密钥写入 Git commit、日志或公开 issue。
