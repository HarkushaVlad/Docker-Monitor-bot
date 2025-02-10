# Docker Monitor Bot

This is a self-hosted Telegram bot that monitors running Docker containers and sends notifications about their status and errors to a Telegram chat.

## Features

- **Real-time Monitoring**: Checks running Docker containers for errors and status changes.
- **Intelligent Log Analysis**: Scans container logs for errors using a universal, marker-based approach. The bot fetches a configurable number of recent log lines (using the `TAIL_COUNT` environment variable, default is 100) and compares a non-cryptographic hash marker of the last processed log line with the newly fetched logs. If the marker is not found (for example, due to log rotation or an insufficient tail window), all fetched log entries are treated as new.
- **Instant Telegram Alerts**: Sends notifications to a Telegram chat when issues are detected.
- **/check Command**: Responds to the `/check` command with a formatted summary of the current status of all containers.
- **/list Command**: Displays the list of containers in an interactive grid layout.

## Deployment

This bot is designed for self-hosting.

### Prerequisites

- **Go 1.18+**
- **Telegram Bot Token** from [@BotFather](https://t.me/BotFather)

### Step 1: Clone the Repository

```bash
git clone https://github.com/HarkushaVlad/Docker-Monitor-bot
cd Docker-Monitor-bot
```

### Step 2: Configure Environment Variables

Create a `.env` file in the root directory and fill in the required values.

#### Explanation of Environment Variables

- **`TELEGRAM_BOT_TOKEN`** – Token for accessing the Telegram bot (get it from [@BotFather](https://t.me/BotFather)).
- **`TELEGRAM_CHAT_ID`** – The ID of the Telegram chat where notifications will be sent. This should be the ID of the user chat initiated with the bot; notifications will be sent to that chat, and the bot will only respond to commands from this chat.
- **`DOCKER_HOST`** – The Docker daemon socket (`unix:///var/run/docker.sock` for Linux). If using Docker on Windows, this might be something like `tcp://127.0.0.1:2376`.
- **`POLL_INTERVAL_SECONDS`** – The interval (in seconds) for checking container logs.
- **`TAIL_COUNT`** – The number of log lines to fetch (tail) from each container. This value is used to limit the number of recent log entries retrieved for analysis. The bot compares a hash marker of the last processed log line with the fetched logs. If the marker is not found (for example, due to a large number of new entries or log rotation), all fetched log lines are considered new. It should be a positive integer; if not set or invalid, the default value of 100 is used.

#### Example `.env` File

```ini
# Telegram Configuration
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_CHAT_ID=your_chat_id_here

# Docker Configuration
DOCKER_HOST=unix:///var/run/docker.sock

# Monitoring Settings
POLL_INTERVAL_SECONDS=15
TAIL_COUNT=100
```

### Step 3: Build and Run the Bot

Build the bot (the main file is located in `cmd/bot`):

```bash
go build -o docker-monitor-bot ./cmd/bot
```

Then run it:

```bash
./docker-monitor-bot
```

Alternatively, to run without building an executable:

```bash
go run ./cmd/bot
```

## Commands

- **/check** - Returns a formatted summary of the status of all Docker containers. For example:

```
📊 Container Status:
┌ ID: abc123def456
├ Name: my_container
├ Status: 🟢 Running
└ Image: my_image:latest
```

- **/list** - Displays the list of containers in a grid layout (2 columns per row, up to 4 rows per page) with inline pagination. If the number of containers on the current page is odd, the last row will contain a single button.

## License

This project is licensed under the MIT License. See the [LICENSE](https://github.com/HarkushaVlad/Docker-Monitor-bot/blob/main/LICENSE) file for details.
