# Интеграция для Backend-разработчика (Паша)

Инструкция по взаимодействию Kotlin Gateway с Go `sync-server`.

---

## 1. Webhook: Проксирование обновлений MWS Tables

Когда MWS Tables API уведомляет ваш Kotlin-сервер об изменении данных в таблице, нужно переслать этот сигнал в Go-сервер, чтобы он уведомил фронтенды.

### Эндпоинт Go-сервера

```
POST http://sync-server:8081/webhooks/mws-update
Content-Type: application/json
```

### Формат JSON-тела запроса

```json
{
  "table_id": "tbl_001",
  "action": "row_updated",
  "source": "mws-api"
}
```

| Поле | Тип | Обязательно | Описание |
|------|-----|-------------|----------|
| `table_id` | string | ✅ | ID таблицы в MWS, данные которой изменились |
| `action` | string | нет | Тип изменения (информационное, Go-сервер не ветвит логику) |
| `source` | string | нет | Источник события (для логирования) |

### Ответ

```json
// 200 OK
{ "status": "ok" }

// 400 Bad Request (отсутствует table_id или невалидный JSON)
{ "error": "table_id is required" }
```

### Пример на Kotlin (Ktor)

```kotlin
// В обработчике вебхука от MWS:
suspend fun forwardToSyncServer(tableId: String) {
    val response = httpClient.post("http://sync-server:8081/webhooks/mws-update") {
        contentType(ContentType.Application.Json)
        setBody(mapOf(
            "table_id" to tableId,
            "action" to "row_updated",
            "source" to "mws-api"
        ))
    }
    logger.info("sync-server webhook response: ${response.status}")
}
```

---

## 2. Приём Snapshot от Go-сервера

Go-сервер периодически (каждые 10 сек) сохраняет состояние документа.

### Эндпоинт, который нужно реализовать в Kotlin

```
POST /api/internal/docs/{doc_id}/snapshot
Content-Type: application/octet-stream
Body: <бинарный CRDT-дамп>
```

### Пример обработчика (Ktor)

```kotlin
post("/api/internal/docs/{doc_id}/snapshot") {
    val docId = call.parameters["doc_id"]
        ?: return@post call.respond(HttpStatusCode.BadRequest)

    val bytes = call.receive<ByteArray>()

    // Сохранить в PostgreSQL
    transaction {
        Pages.update({ Pages.id eq UUID.fromString(docId) }) {
            it[documentState] = ExposedBlob(bytes)
            it[updatedAt] = Instant.now()
        }
    }

    call.respond(HttpStatusCode.OK)
}
```

---

## 3. JWT-токены

Go-сервер парсит JWT при подключении клиента к WebSocket. Формат payload:

```json
{
  "user_id": "user-uuid-123",
  "name": "Паша",
  "cursor_color": "#ff6b6b"
}
```

### Генерация токена в Kotlin

```kotlin
fun generateToken(userId: String, name: String): String {
    val header = Base64.getUrlEncoder().encodeToString(
        """{"alg":"HS256","typ":"JWT"}""".toByteArray()
    )
    val payload = Base64.getUrlEncoder().encodeToString(
        """{"user_id":"$userId","name":"$name"}""".toByteArray()
    )
    val signature = Mac.getInstance("HmacSHA256").apply {
        init(SecretKeySpec(jwtSecret.toByteArray(), "HmacSHA256"))
    }.doFinal("$header.$payload".toByteArray())

    return "$header.$payload.${Base64.getUrlEncoder().encodeToString(signature)}"
}
```

Секрет задаётся через `JWT_SECRET` env-переменную (должен совпадать у Kotlin и Go).
В dev-режиме (`JWT_SECRET` пуст) Go-сервер принимает legacy-токены вида `fake-jwt-token-for-{name}`.

---

## 4. Сетевая топология (docker-compose)

```
Kotlin Gateway  ──POST /webhooks/mws-update──▶  sync-server:8081
sync-server     ──POST /api/internal/docs/*/snapshot──▶  backend-gateway:8080
```

Оба сервиса находятся в одной Docker-сети и обращаются друг к другу по именам контейнеров.
