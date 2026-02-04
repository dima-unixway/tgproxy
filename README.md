# tgproxy

tgproxy is a [Go](https://go.dev/) HTTP server providing a REST API and Server-Sent Events (SSE) for interacting with Telegram using [TDLib](https://core.telegram.org/tdlib). It allows sending messages, fetching peers/contacts, retrieving chat history, real-time message subscriptions, and media downloads.

## Motivation

Built to simplify Telegram integration into applications without directly managing TDLib or Telegram MTProto. Expose Telegram as an HTTP service with:
- Simple JSON APIs for common operations
- SSE for real-time incoming messages
- Static binary deployment (Docker-based build)

Useful for simple account automation.

## Quick start

1. **Prerequisites**:
   - Telegram API credentials: [my.telegram.org](https://my.telegram.org)

2. **Setup**:
   ```bash
   cp .env.example .env
   # Edit .env with your API_ID, API_HASH, PHONE
   ```

3. **Run**:
   ```bash
   ./tgproxy
   ```
   - First run: Interactive auth (phone code, 2FA)
   - HTTP API on `http://localhost:8080`
   - Data stored in `./data`

## Running

```bash
./tgproxy
```

- **Ports**: `HTTP_PORT` (default `:8080`)
- **Graceful shutdown**: SIGINT/SIGTERM
- **Logs**: Structured (Slog), configurable level
- **Persistence**: TDLib DB in `TDLIB_DATABASE_DIR` (default `./data`)

## Configuration

Load from `.env` (via godotenv). Required:

| Var                  | Description                          | Default     |
|----------------------|--------------------------------------|-------------|
| `API_ID`            | Telegram API ID (int)                | required   |
| `API_HASH`          | Telegram API hash (string)           | required   |
| `PHONE`             | Phone number (e.g. `+1234567890`)    | required   |
| `HTTP_ADDR`         | HTTP listen addres                   | `127.0.0.1`|
| `HTTP_PORT`         | HTTP listen port                     | `8080`     |
| `TDLIB_DATABASE_DIR`| TDLib data directory                 | `./data`   |
| `TDLIB_LOG_LEVEL`   | TDLib verbosity (0-3)                | `0`        |
| `LOG_LEVEL`         | App logs: `debug`/`info`/`warn`/`error` | `info`  |

Example `.env`:
```
API_ID=123456
API_HASH=abcdef0123456789abcdef0123456789
PHONE=+1234567890
HTTP_PORT=8080
LOG_LEVEL=debug
```

## curl examples

Base URL: `http://localhost:8080`

### Health check
```bash
curl http://localhost:8080/health
# {"status":"healthy"}
```

### List peers/contacts (recent + search)
```bash
# Recent peers (limit 5)
curl "http://localhost:8080/peers?limit=10"

# Search peers
curl "http://localhost:8080/peers/john?limit=5"
```
Response: `{"peers": [{"peer_id":"...","type":"user","name":"John Doe","last_message_timestamp":1700000000}, ...]}`

### Send message
```bash
curl -X POST http://localhost:8080/send \
  -H "Content-Type: application/json" \
  -d '{
    "peer_id": "123",
    "type": "text",
    "content": { "text": "Hello from tgproxy!" }
  }'
```
Response: `{"success":true}`

### Get recent messages by chat_id, user_id or fuzzy search by name
```bash
# Last message from chat
curl "http://localhost:8080/messages/chat1?limit=5"

# Multiple chats
curl "http://localhost:8080/messages/chat1,chat2?limit=3"
```

### Subscribe (SSE real-time messages)
```bash
# All chats
curl "http://localhost:8080/subscribe"

# Multiple chats
curl "http://localhost:8080/subscribe/chat1,chat2"
```

**Message format**:
```json
{
  "id": "123",
  "peer_id": "123",
  "peer_name": "John",
  "type": "text",
  "content": { "text": "Hi!" },
  "timestamp": 1700000000
}
```
Supports text, photo, video, document, voice, sticker, media groups.

### Media download
```bash
curl http://localhost:8080/attachment/{media_url}
```
Proxies Telegram media files.

## Development

### Building
- Static binary: `make build` (Docker-based cross-compile)
- Docker image: `make docker-build`
- Packages: `make rpm` / `make deb`
- Full build: `make all`
- Clean: `make clean`

## License
[MIT](LICENSE)
