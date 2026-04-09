# crdt-engine — WikiLive Signaling Server

Высокопроизводительный Go-сервер для реального времени. Принимает бинарные CRDT-дельты по WebSocket и мгновенно рассылает их всем участникам документа. Не разбирает содержимое — работает чистым роутером.

## Архитектура

```
┌──────────────┐     WebSocket (binary)     ┌───────────────┐
│  Frontend    │ ◄──────────────────────────► │  crdt-engine  │
│  (Yjs/Loro)  │                             │  (Go)         │
└──────────────┘                             └──────┬────────┘
                                                    │
                                          ┌─────────┴─────────┐
                                          │                   │
                                     Redis Pub/Sub     HTTP POST snapshot
                                          │                   │
                                          ▼                   ▼
                                   ┌───────────┐    ┌──────────────────┐
                                   │   Redis   │    │ Backend Gateway  │
                                   └───────────┘    │ (Kotlin/Java)    │
                                                    └──────────────────┘
```

- **Redis Pub/Sub** — горизонтальное масштабирование: несколько инстансов синхронизируют комнаты.
- **Snapshot Worker** — каждые N секунд (настраивается) автоматически сохраняет состояние документа в Backend Gateway через HTTP POST.

## Структура проекта

```
crdt-engine/
├── cmd/
│   ├── server/main.go              # Точка входа, HTTP-мультиплексор, graceful shutdown
│   └── loadtester/main.go          # Утилита стресс-тестирования (100+ ботов)
├── internal/
│   ├── websocket/
│   │   ├── hub.go                  # Hub + Room management, broadcast, snapshot integration
│   │   ├── client.go               # Client: read/write pump, ping/pong keepalive
│   │   └── handler.go              # HTTP → WebSocket upgrade, парсинг doc_id и token
│   ├── redis/
│   │   └── pubsub.go               # Redis Pub/Sub broker
│   ├── snapshot/
│   │   └── worker.go               # Periodic snapshot worker (HTTP POST → Backend Gateway)
│   └── api/
│       └── mock.go                 # Mock REST: /api/login, /api/tables
├── Dockerfile                      # Multi-stage build (golang:1.22-alpine → alpine:3.19)
├── go.mod
├── go.sum
└── README.md
```

## Запуск локально

### Предварительные требования
- Go 1.22+
- Redis (запущен на `localhost:6379`)

### 1. Запустить Redis (если не запущен)

```bash
docker run -d --name redis -p 6379:6379 redis:latest
```

### 2. Запустить сервер

```bash
cd crdt-engine
go run ./cmd/server
```

Сервер стартует на порту `8081`.

### 3. Проверить, что работает

```bash
# Health check
curl http://localhost:8081/healthz

# Получить фейковый токен
curl "http://localhost:8081/api/login?user=denis"
# → {"token":"fake-jwt-token-for-denis"}

# Получить список таблиц
curl http://localhost:8081/api/tables
```

## Переменные окружения

| Переменная              | По умолчанию                     | Описание                                        |
|-------------------------|----------------------------------|-------------------------------------------------|
| `PORT`                  | `8081`                           | Порт HTTP/WS сервера                            |
| `REDIS_URL`             | `redis://localhost:6379`         | URL подключения к Redis                         |
| `GATEWAY_URL`           | `http://backend-gateway:8080`    | URL Java/Kotlin бэкенда для snapshot POST       |
| `SNAPSHOT_INTERVAL_SEC` | `10`                             | Интервал автосохранения snapshot (секунды)       |

## Подключение фронтенда

### WebSocket

```javascript
const docId = "my-document-123";
const token = "fake-jwt-token-for-denis";

const ws = new WebSocket(`ws://localhost:8081/ws/doc/${docId}?token=${token}`);

ws.binaryType = "arraybuffer";

// Отправка CRDT-дельты (Uint8Array)
ws.send(crdtUpdate);

// Получение CRDT-дельт от других участников
ws.onmessage = (event) => {
  const delta = new Uint8Array(event.data);
  applyRemoteUpdate(delta);
};
```

### Протокол
- Все сообщения — **бинарные** (`Uint8Array` / `ArrayBuffer`).
- Текстовые сообщения игнорируются сервером.
- Сервер не модифицирует данные — пересылает as-is.
- Ping/Pong keepalive: сервер шлёт ping каждые 54 секунды, таймаут — 60 секунд.

## Snapshot Worker

Каждая комната имеет фоновую горутину (Snapshot Worker), которая:
1. Кэширует последнее полученное состояние документа в памяти.
2. Раз в `SNAPSHOT_INTERVAL_SEC` секунд — если были изменения (dirty flag) — делает `POST /api/internal/docs/{doc_id}/snapshot` на Backend Gateway с телом `application/octet-stream`.
3. При закрытии комнаты (последний клиент ушёл) — немедленно делает финальный flush.
4. При `SIGTERM` — все активные комнаты принудительно сохраняются до завершения процесса.

## Graceful Shutdown

При получении `SIGINT`/`SIGTERM` сервер выполняет 4-фазное выключение (таймаут 10 секунд):

```
1. Stop HTTP listener    → новые соединения отклоняются
2. Stop Hub event loop   → register/unregister больше не обрабатываются
3. Flush all snapshots   → параллельный POST на Backend Gateway для каждой комнаты
   + Close WebSockets    → отправка close frame 1001 (Going Away) всем клиентам
4. Close Redis           → соединение с Redis закрывается
```

## Load Tester

Утилита для стресс-тестирования мьютексов, обнаружения race conditions и утечек памяти.

### Запуск

```bash
# Дефолт: 100 ботов, интервал 100мс, 1KB пакеты
go run ./cmd/loadtester

# Кастомные параметры
go run ./cmd/loadtester -bots=200 -interval=50ms -room=my-doc -addr=localhost:8081 -size=2048
```

### Флаги

| Флаг        | По умолчанию   | Описание                               |
|-------------|----------------|-----------------------------------------|
| `-bots`     | `100`          | Количество параллельных ботов           |
| `-interval` | `100ms`        | Интервал отправки сообщений на бота     |
| `-room`     | `stress-test`  | ID документа (комнаты)                  |
| `-addr`     | `localhost:8081`| Адрес crdt-engine                       |
| `-size`     | `1024`         | Размер payload в байтах                 |

### Вывод

```
[LOAD] starting 100 bots → ws://localhost:8081/ws/doc/stress-test
[STATS] sent=4200  recv=415800  errors=0
[STATS] sent=8400  recv=831600  errors=0
```

### Race Detector

Рекомендуется запускать сервер с race detector для поиска data races:

```bash
# Терминал 1: сервер с race detector
go run -race ./cmd/server

# Терминал 2: load tester
go run ./cmd/loadtester -bots=50 -interval=200ms
```

## Docker

### Сборка образа

```bash
docker build -t crdt-engine .
```

### Запуск в docker-compose

```yaml
services:
  crdt-engine:
    build: ./crdt-engine
    ports:
      - "8081:8081"
    environment:
      - PORT=8081
      - REDIS_URL=redis://redis:6379
      - GATEWAY_URL=http://backend-gateway:8080
      - SNAPSHOT_INTERVAL_SEC=10
    depends_on:
      - redis

  redis:
    image: redis:latest
    ports:
      - "6379:6379"
```

## Защита от утечек памяти

- Каждый `Client` владеет буферизированным каналом `send chan []byte` (размер 256).
- При отключении клиента канал **закрывается** → `writePump` завершается → горутина освобождается.
- Пустые комнаты автоматически уничтожаются: Snapshot Worker останавливается, Redis-подписка отменяется, `broadcastLoop` завершается.
- Snapshot Worker корректно завершается через `stopCh` + `done` channel pattern — без утечек горутин.
- Медленные клиенты не блокируют broadcast — сообщения дропаются с warning-логом.
- Graceful shutdown: `SIGTERM` → 4 фазы → процесс завершается только после сохранения всех данных.
