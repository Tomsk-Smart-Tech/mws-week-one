# Инструкция для DevOps (Никита)

Настройка инфраструктуры для `sync-server`: Nginx, Prometheus, Grafana.

---

## 1. Обзор портов

| Сервис | Внутренний порт | Публичный порт | Назначение |
|--------|-----------------|----------------|------------|
| `sync-server` | 8081 | 8081 | WebSocket + REST API + Метрики |
| `prometheus` | 9090 | 9090 | Сбор метрик |
| `grafana` | 3000 | 3001 | Визуализация дашбордов |
| `redis` | 6379 | — | Pub/Sub (только внутри compose) |

---

## 2. Роутинг Nginx

Добавь в конфигурацию Nginx (`infrastructure/nginx/nginx.conf`) следующие блоки:

### WebSocket (CRDT дельты)

```nginx
location /ws/ {
    proxy_pass http://sync-server:8081;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

### Webhook эндпоинт (от Kotlin Gateway)

```nginx
location /webhooks/ {
    proxy_pass http://sync-server:8081;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
}
```

### Prometheus метрики (только внутренний доступ)

```nginx
# НЕ выставляй наружу! Метрики доступны только из Docker-сети.
# Prometheus сам стучится на sync-server:8081/metrics.
# Если нужен доступ снаружи для дебага:
location /metrics {
    proxy_pass http://sync-server:8081;
    # allow 172.16.0.0/12;  # Docker subnet
    # deny all;
}
```

### Health Check

```nginx
location /healthz {
    proxy_pass http://sync-server:8081;
}
```

---

## 3. Docker Compose

Prometheus и Grafana уже добавлены в корневой `docker-compose.yaml`:

```bash
# Запуск всего стека
docker-compose up -d

# Только мониторинг
docker-compose up -d prometheus grafana
```

### Доступы

- **Grafana:** http://localhost:3001  
  - Логин: `admin` / Пароль: `wikilive`
  - Анонимный доступ включён (read-only)
  - Дашборд "WikiLive — Sync Server" провижнится автоматически

- **Prometheus:** http://localhost:9090  
  - Target: `sync-server:8081` (scrape каждые 5 сек)

---

## 4. Переменные окружения sync-server

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `PORT` | `8081` | Порт HTTP/WS сервера |
| `REDIS_URL` | `redis://localhost:6379` | URL подключения к Redis |
| `GATEWAY_URL` | `http://backend-gateway:8080` | URL Kotlin бэкенда |
| `SNAPSHOT_INTERVAL_SEC` | `10` | Интервал snapshot flush (секунды) |
| `JWT_SECRET` | *(пусто)* | HMAC секрет JWT. Пусто = dev-режим |

---

## 5. Мониторинг: Ключевые метрики

Prometheus эндпоинт: `GET http://sync-server:8081/metrics`

| Метрика | Тип | Описание |
|---------|-----|----------|
| `sync_server_active_ws_connections` | Gauge | Текущее кол-во WS соединений |
| `sync_server_active_rooms` | Gauge | Кол-во активных комнат |
| `sync_server_crdt_deltas_total` | Counter | Всего принятых CRDT-дельт |
| `sync_server_messages_broadcast_total` | Counter | Всего отправленных broadcast сообщений |
| `sync_server_messages_dropped_total` | Counter | Дропы из-за медленных клиентов |
| `sync_server_snapshot_saves_total` | Counter | Всего snapshot-сохранений |
| `sync_server_snapshot_errors_total` | Counter | Ошибки сохранения |
| `sync_server_webhook_events_total` | Counter | Входящих webhook-уведомлений от MWS |

### Полезные PromQL запросы

```promql
# Дельты в секунду (throughput)
rate(sync_server_crdt_deltas_total[1m])

# Процент дропов 
rate(sync_server_messages_dropped_total[1m]) / rate(sync_server_messages_broadcast_total[1m]) * 100

# Ошибки snapshot за последний час
increase(sync_server_snapshot_errors_total[1h])
```

---

## 6. Graceful Shutdown

При `docker-compose stop` или `docker stop sync-server`:
1. Сервер перехватывает `SIGTERM`
2. Прекращает принимать новые соединения
3. Сохраняет snapshot всех комнат в Kotlin Gateway
4. Шлет `1001 Going Away` всем клиентам
5. Закрывает Redis
6. Завершается (таймаут: 10 сек)

Для роллинг-апдейтов рекомендую `stop_grace_period: 15s` в compose.
