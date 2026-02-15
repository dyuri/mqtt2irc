# Quick Start Guide

This guide will help you get the MQTT-to-IRC bridge running in under 5 minutes.

## Prerequisites

- Go 1.21 or later
- Docker and Docker Compose (for local testing)
- Or access to an MQTT broker and IRC server

## Option 1: Local Testing with Docker

### Step 1: Start Test Environment

```bash
# Start local MQTT broker and IRC server
docker-compose up -d

# Wait a few seconds for services to start
sleep 5
```

This starts:
- Mosquitto MQTT broker on `localhost:1883`
- ngIRCd IRC server on `localhost:6667`
- A publisher that sends test messages every 10 seconds

### Step 2: Build and Run the Bridge

```bash
# Build the binary
make build

# Run with test configuration
./mqtt2irc -config configs/config.test.yaml
```

### Step 3: Connect to IRC and Watch Messages

In another terminal:

```bash
# Install IRC client if needed
# Ubuntu/Debian: sudo apt-get install irssi
# macOS: brew install irssi

# Connect to local IRC server
irssi -c localhost -p 6667

# Join channels to see messages
/join #sensors
/join #alerts
/join #monitoring
```

You should start seeing MQTT messages appear in the IRC channels!

### Step 4: Publish Your Own Messages

In another terminal:

```bash
# Publish temperature reading
docker exec mqtt2irc-mosquitto mosquitto_pub -t "sensors/temperature/livingroom" -m "22.5°C"

# Publish humidity reading
docker exec mqtt2irc-mosquitto mosquitto_pub -t "sensors/humidity/kitchen" -m "55%"

# Publish critical alert
docker exec mqtt2irc-mosquitto mosquitto_pub -t "alerts/critical" -m "System overheating!"
```

These messages will appear in the respective IRC channels.

### Step 5: Stop Test Environment

```bash
# Stop the bot
Ctrl+C

# Stop Docker containers
docker-compose down
```

## Option 2: Connect to Real Servers

### Step 1: Create Configuration

```bash
cp configs/config.example.yaml configs/config.yaml
vim configs/config.yaml
```

Edit the configuration to point to your real MQTT broker and IRC server:

```yaml
mqtt:
  broker: "tcp://mqtt.yourserver.com:1883"
  client_id: "mqtt2irc_bot"
  username: "your_mqtt_user"
  password: "your_mqtt_password"
  topics:
    - pattern: "your/topic/#"
      qos: 1

irc:
  server: "irc.yourserver.com:6697"
  use_tls: true
  nickname: "yourbot"
  username: "yourbot"
  nickserv_password: "your_nickserv_password"  # If registered

bridge:
  mappings:
    - mqtt_topic: "your/topic/#"
      irc_channels:
        - "#yourchannel"
      message_format: "[{{.Topic}}] {{.Payload}}"
```

### Step 2: Run the Bridge

```bash
# Build and run
make build
./mqtt2irc -config configs/config.yaml
```

## Verify It's Working

### Check Health Endpoint

```bash
# In another terminal
curl http://localhost:8080/health
```

Expected response when healthy:

```json
{
  "mqtt_connected": true,
  "irc_connected": true,
  "queue_size": 0,
  "queue_capacity": 1000,
  "status": "healthy"
}
```

### Check Logs

The bot outputs structured logs showing:
- MQTT connection status
- IRC connection status
- Subscribed topics
- Messages being forwarded

Example log output (console format):

```
12:00:00 INF starting mqtt2irc bridge version=dev
12:00:00 INF connecting to MQTT broker broker=tcp://localhost:1883 component=mqtt
12:00:01 INF connected to MQTT broker component=mqtt
12:00:01 INF MQTT connection established component=mqtt
12:00:01 INF subscribing to MQTT topic component=mqtt pattern=sensors/# qos=1
12:00:01 INF subscribed to topic component=mqtt pattern=sensors/#
12:00:01 INF connecting to IRC server component=irc server=localhost:6667
12:00:02 INF IRC connection established component=irc
12:00:02 INF connected to IRC server component=irc
12:00:02 INF starting bridge component=bridge
12:00:02 INF bridge running component=bridge
12:00:02 INF starting health check server addr=:8080 component=health
```

## Common Issues

### "failed to read config: Config File "config" Not Found"

Make sure you specify the config file path:

```bash
./mqtt2irc -config configs/config.yaml
```

Or place `config.yaml` in the current directory or `./configs/` directory.

### "failed to connect to MQTT: connection refused"

- Verify MQTT broker address and port
- Check if broker requires authentication
- Try `use_tls: false` if broker doesn't use TLS

### "failed to connect to IRC: connection refused"

- Verify IRC server address and port
- Check `use_tls` setting matches your server
- Some IRC servers require TLS on port 6697, plain on port 6667

### Messages not appearing in IRC

- Make sure topic mappings match your MQTT topics exactly
- Check MQTT wildcards (`+`, `#`) are used correctly
- Enable debug logging: set `logging.level: "debug"` in config
- Verify the bot joined the IRC channels (check logs)

### Bot kicked from IRC for flooding

- Reduce `rate_limit.messages_per_second`
- Increase delays between MQTT publishes
- Check message queue isn't building up

## Next Steps

- Read the full [README.md](README.md) for advanced configuration
- Customize message templates in bridge mappings
- Set up multiple topic-to-channel mappings
- Deploy with Docker: `make docker-build`
- Add health checks for monitoring
- Set up systemd service for production

## Example Mappings

### IoT Sensor Dashboard

```yaml
bridge:
  mappings:
    - mqtt_topic: "home/+/temperature"
      irc_channels: ["#home-sensors"]
      message_format: "🌡️  {{.Payload}}"

    - mqtt_topic: "home/+/motion"
      irc_channels: ["#home-security"]
      message_format: "🚶 Motion detected: {{.Payload}}"
```

### Server Monitoring

```yaml
bridge:
  mappings:
    - mqtt_topic: "servers/+/cpu"
      irc_channels: ["#monitoring"]
      message_format: "💻 CPU {{.Payload}}"

    - mqtt_topic: "servers/+/alerts/#"
      irc_channels: ["#alerts", "#oncall"]
      message_format: "🚨 {{.Payload}}"
```

### Multi-Environment

```yaml
bridge:
  mappings:
    - mqtt_topic: "prod/logs/#"
      irc_channels: ["#prod-logs"]
      message_format: "[PROD] {{.Payload}}"

    - mqtt_topic: "staging/logs/#"
      irc_channels: ["#staging-logs"]
      message_format: "[STAGE] {{.Payload}}"
```

## Useful Commands

```bash
# Build
make build

# Run tests
make test

# Run with coverage
make test-cover

# Format code
make fmt

# Clean build artifacts
make clean

# Build for all platforms
make build-all

# Start test environment
make docker-compose-up

# Stop test environment
make docker-compose-down
```

## Getting Help

- Check [README.md](README.md) for detailed documentation
- Review example configs in `configs/`
- Enable debug logging for troubleshooting
- Check health endpoint: `curl http://localhost:8080/health`
