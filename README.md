# Docker Monitor Bot

A self-hosted Telegram bot that monitors Docker containers and Compose projects, sending real-time notifications about status changes and errors.

## Features

- **Docker Compose Support** — groups containers by Compose project, with project-level actions (start/stop/restart/rebuild all services)
- **Real-time Event Monitoring** — instant notifications when containers start, stop, or crash (OOM)
- **Log Error Detection** — periodically scans container logs for errors using a marker-based approach to avoid duplicate alerts
- **Interactive Management** — browse and manage containers/projects via inline Telegram keyboard
- **Status Overview** — quick status check grouped by Compose project and standalone containers

## Commands

| Command | Description |
|---------|-------------|
| `/start` | Welcome message with available commands |
| `/check` | Quick status overview of all containers grouped by project |
| `/list`  | Interactive container/project browser with management actions |

### `/check` output example

```
📊 Docker Status

🗂 my-app (3/3 running)
  🟢 web — nginx:latest
  🟢 api — myapp:v2
  🟢 db — postgres:16

📦 Standalone
  🟢 portainer — portainer/portainer
  🔴 old-service — myimage:v1
```

### `/list` navigation

1. **Main view** — shows Compose projects (with running/total count) and standalone containers
2. **Project view** — drill into a project to see its services, with project-level actions
3. **Container detail** — inspect a container, start/stop/restart it

## Deployment

### Prerequisites

- **Go 1.23+**
- **Docker** (the bot connects to the Docker daemon via socket)
- **docker compose** (required only for the rebuild action)
- **Telegram Bot Token** from [@BotFather](https://t.me/BotFather)

### Step 1: Clone the Repository

```bash
git clone https://github.com/HarkushaVlad/Docker-Monitor-bot
cd Docker-Monitor-bot
```

### Step 2: Configure Environment Variables

Copy the example and fill in your values:

```bash
cp .env.example .env
```

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TELEGRAM_BOT_TOKEN` | Yes | — | Bot token from [@BotFather](https://t.me/BotFather) |
| `TELEGRAM_CHAT_ID` | Yes | — | Chat ID(s) where notifications are sent. Multiple IDs can be separated by commas (e.g., `123,456`). The bot only responds to commands from these chats. |
| `DOCKER_HOST` | No | `unix:///var/run/docker.sock` | Docker daemon socket. On Windows: `tcp://127.0.0.1:2376` |
| `POLL_INTERVAL_SECONDS` | No | `60` | How often to scan container logs for errors (seconds) |
| `TAIL_COUNT` | No | `100` | Number of recent log lines to fetch per container |

### Step 3: Build and Run

```bash
go build -o docker-monitor-bot ./cmd/bot
./docker-monitor-bot
```

Or run directly:

```bash
go run ./cmd/bot
```

## License

This project is licensed under the MIT License. See the [LICENSE](https://github.com/HarkushaVlad/Docker-Monitor-bot/blob/main/LICENSE) file for details.
