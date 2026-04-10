# Реализованные фичи `sync-server` (MWS Knowledge Fabric)

В ходе разработки и рефакторинга микросервис `sync-server` был существенно модернизирован и подготовлен к интеграции в энтерпрайз-архитектуру проекта WikiLive. Ниже представлен полный список внедренных функций и улучшений.

## 1. Рефакторинг и Обновление Архитектуры
- **Переименование и реструктуризация:** Сервис переименован из `crdt-engine` в `sync-server`. Обновлены все модули импорта и конфигурационные файлы Nginx / Docker Compose.
- **Локализаций инфраструктуры:** Полный перевод на локальное окружение (Docker Compose, Redis, Postgres).

## 2. Безопасность и JWT Авторизация
- **JWT-парсер (HMAC-SHA256):** Написан легковесный парсер токенов (пакет `internal/auth`), встраиваемый на этапе подключения (`/ws/doc/{doc_id}?token=...` или через заголовок `Authorization: Bearer`).
- **Server-Authoritative Awareness:** Сервер извлекает информацию о пользователе (`user_id`, `name`) из JWT и принудительно рассылает ее в виде JSON-эвента `awareness`. Клиент больше не может подделать имя пользователя.
- **Детерминированный цвет курсора:** Цвет курсора пользователя автоматически генерируется на основе SHA256 хеша его имени, чтобы каждый пользователь имел постоянный цвет курсора, если он не передан в claims.
- **Legacy-поддержка (Dev Mode):** Возможность работы с токенами `fake-jwt-token-for-{name}` при отключенной проверке подписи.

## 3. Webhook Router (Интеграция с MWS Tables)
- **POST `/webhooks/mws-update`:** Реализован HTTP обработчик для приема payload от Kotlin Gateway об изменении данных в MWS.
- **Multiplexing (Бинарные/Текстовые данные):** В WebSocket соединении добавлен параллельный канал (`systemSend`) для системных событий.
- **Broadcast System Event:** При получении вебхука, сервер рассылает событие `{"type": "system", "action": "reload_table", "table_id": "tbl_id"}` всем подключенным клиентам, позволяя React-фронтенду перезагрузить конкретную таблицу.

## 4. Observability. Мониторинг: Prometheus + Grafana
- **Метрики (Пакет `internal/metrics`):**
  - Активные соединения и комнаты (`active_ws_connections`, `active_rooms` — Gauge).
  - CRDT-трафик (`crdt_deltas_total` — Counter).
  - Webhook уведомления (`webhook_events_total` — Counter).
  - Сохранения и ошибки Snapshot Worker (`snapshot_saves_total`, `snapshot_errors_total`).
  - Статистика Broadcast (`messages_broadcast_total`, `messages_dropped_total`).
- **Дашборд:** Создан специализированный JSON дашборд (находится в `infrastructure/grafana/dashboards/sync-server.json`).
- **Интеграция Nginx / Docker:** Добавлен прометеус эндпоинт `/metrics`, Prometheus и Grafana добавлены в `docker-compose.yaml`.

## 5. Покрытие тестами (Unit Testing)
- **30 тестов (покрытие всех пакетов)**
  - `auth`: Валидация подписей JWT, Dev Mode токенов, генерация цветов.
  - `webhook`: Проверка JSON, правильных форматов вебхуков.
  - `snapshot`: Тестирование `FlushNow`, Periodic Flush, State Updates и горутин-утечек.
  - `api`: Mock API (`/api/login`, `/api/tables`) тестирование.
- Все тесты запускаются с проверкой data races (`-race`).

## 6. Техническая Документация (Docs)
Созданы подробные инструкции в папке `docs/integration/`:
1. `frontend_guide.md` — Подключение фронтенда, JWT, awareness, роутинг бинарных и текстовых дельт.
2. `backend_guide.md` — Генерация JWT, формат вебхука MWS, snapshot POST.
3. `devops_guide.md` — Nginx config, порты, Graceful Shutdown, PromQL, docker-compose.
4. Значительно расширен `README.md` в самом `sync-server`.

## 7. Поддержка Graceful Shutdown
- При остановке сервиса (SIGINT/SIGTERM), прекращается прием новых соединений.
- Выполняется Flush всех оставшихся Snapshot комнат на внешний Kotlin Server.
- Клиентам отправляется фрейм закрытия `1001 Going Away`, безопасно закрывая WebSocket, предотвращая потерю данных.
