# DingTalk File Send Deployment

This document records the deployment path for the DingTalk group file send patch.

## What This Patch Fixes

`cc-connect send --file` can upload a file to DingTalk, but shared group sessions
(`share_session_in_channel = true`) do not have a sender staff ID. The upstream
DingTalk file sender used the one-to-one robot API unconditionally, which fails
for shared group sessions because `userIds` is empty or invalid.

This patch routes file messages by reply context:

- Group sessions use `POST /v1.0/robot/groupMessages/send` with
  `openConversationId`.
- Direct or per-user sessions use `POST /v1.0/robot/oToMessages/batchSend` with
  `userIds`.

For DingTalk direct chats, proactive sends must preserve the sender staff ID.
The fixed direct session key format is:

```text
dingtalk:d:<conversationId>:<senderStaffId>
```

Older short keys (`dingtalk:d:<conversationId>`) are migrated into the sender
key so existing direct chats keep their history after upgrade.

## Build On A Linux Cloud Server

Install Go 1.25 or newer:

```bash
wget https://go.dev/dl/go1.26.3.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.26.3.linux-amd64.tar.gz
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc
source ~/.bashrc
go version
```

Clone your fork and build a CLI-only binary:

```bash
git clone https://github.com/Chriz2333/cc-connect.git
cd cc-connect
git checkout fix/dingtalk-group-file-send
go test ./daemon ./platform/dingtalk ./tests/release_local/media_pipeline
go build -tags no_web -o cc-connect ./cmd/cc-connect
./cc-connect --version
```

## Minimal Config

Create `~/.cc-connect/config.toml`:

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

## Start Manually

```bash
mkdir -p /opt/cc-workspace
./cc-connect --config "$HOME/.cc-connect/config.toml"
```

Send a test file after the bot has received at least one DingTalk group message:

```bash
./cc-connect sessions list
./cc-connect send --file /absolute/path/to/test.pdf -p codex-dingtalk -s "dingtalk:g:<openConversationId>"
```

## Run With systemd

Copy the binary:

```bash
sudo install -m 0755 ./cc-connect /usr/local/bin/cc-connect
```

Create `~/.config/systemd/user/cc-connect.service`:

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

Enable it:

```bash
systemctl --user daemon-reload
systemctl --user enable --now cc-connect
loginctl enable-linger "$USER"
systemctl --user status cc-connect
journalctl --user -u cc-connect -f
```

## Rollback

Stop the service:

```bash
systemctl --user stop cc-connect
```

Install the official npm release again:

```bash
npm install -g cc-connect
cc-connect --version
```

Then restart with the official binary if file send-back is no longer needed.
