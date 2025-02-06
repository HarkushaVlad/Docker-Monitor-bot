# Docker Monitor Bot

This is a self-hosted Telegram bot that monitors running Docker containers and sends notifications about their status and errors to a Telegram chat.

## Features

- **Real-time Monitoring**: Checks running Docker containers for errors and status changes.
- **Log Analysis**: Scans container logs for errors and reports them.
- **Instant Telegram Alerts**: Sends notifications to a Telegram chat when issues are detected.
- **Multi-Language Support**: Supports English (`en`) and Ukrainian (`uk`) for Telegram messages.
- **/check Command**: Responds to the `/check` command with a formatted summary of the current status of all containers.

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
- **`TELEGRAM_CHAT_ID`** – The ID of the Telegram chat where notifications will be sent. This should be the ID of the user chat initiated with the bot; notifications will be sent to that chat. The bot will also only respond to commands from this chat.
- **`DOCKER_HOST`** – The Docker daemon socket (`unix:///var/run/docker.sock` for Linux).
- **`POLL_INTERVAL_SECONDS`** – The interval (in seconds) for checking container logs.
- **`LANGUAGE`** – Language for Telegram notifications (`en` for English, `uk` for Ukrainian).

#### Example `.env` File

```ini
# Telegram Configuration
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_CHAT_ID=your_chat_id_here

# Docker Configuration
DOCKER_HOST=unix:///var/run/docker.sock

# Monitoring Settings
POLL_INTERVAL_SECONDS=15

# Notification Language
LANGUAGE=en
```

### Step 3: Build and Run the Bot

```bash
go build -o docker-monitor-bot
./docker-monitor-bot
```

## Command: /check

When you send the **/check** command, the bot returns a summary of the status of all Docker containers. For example:

```
📊 Container Status:
┌ ID: abc123def456
├ Name: my_container
├ Status: 🟢 Running
└ Image: my_image:latest
```

## License

This project is licensed under the MIT License. See the [LICENSE](https://github.com/HarkushaVlad/Docker-Monitor-bot/blob/main/LICENSE) file for details.
