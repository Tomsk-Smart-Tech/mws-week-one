# Интеграция для Frontend-разработчика (Кирилл)

Инструкция по подключению React-клиента к `sync-server`.

---

## 1. Подключение по WebSocket (с JWT)

```javascript
// Токен приходит из GET /api/login?user=kirill
// или генерируется Kotlin Gateway при реальной авторизации.
const token = "fake-jwt-token-for-kirill"; // dev mode
// const token = "eyJhbGciOiJIUzI1Ni..."; // production JWT

const docId = "doc-uuid-123";

const ws = new WebSocket(
  `ws://localhost:8081/ws/doc/${docId}?token=${token}`
);

ws.binaryType = "arraybuffer";
```

Также поддерживается передача токена через заголовок `Authorization: Bearer <token>` (для библиотек, поддерживающих кастомные заголовки при WS-подключении).

---

## 2. Два типа сообщений

Сервер отправляет **два типа** сообщений через один сокет:

| Тип | `MessageEvent.data` | Описание |
|-----|---------------------|----------|
| **Binary** (`ArrayBuffer`) | `Uint8Array` | CRDT-дельты от других участников — передай в `Y.applyUpdate()` |
| **Text** (`string`) | JSON | Системные события от сервера |

### Роутинг в `onmessage`:

```javascript
ws.onmessage = (event) => {
  if (event.data instanceof ArrayBuffer) {
    // --- Бинарное сообщение: CRDT-дельта ---
    const update = new Uint8Array(event.data);
    Y.applyUpdate(ydoc, update);
  } else {
    // --- Текстовое сообщение: системный JSON ---
    const msg = JSON.parse(event.data);
    handleSystemEvent(msg);
  }
};
```

---

## 3. Системные события (JSON)

### 3.1 `awareness` — Информация о пользователе (приходит сразу после подключения)

```json
{
  "type": "awareness",
  "user_id": "kirill",
  "name": "kirill",
  "cursor_color": "#a4c8e1"
}
```

**Зачем:** Сервер принудительно назначает `user_id`, `name` и `cursor_color`. Клиент НЕ может подделать свое имя — эти данные инжектятся сервером из JWT.

**Использование:** Установи эти данные в `awareness` Yjs-провайдера:

```javascript
function handleSystemEvent(msg) {
  switch (msg.type) {
    case "awareness":
      awareness.setLocalStateField("user", {
        name: msg.name,
        color: msg.cursor_color,
      });
      break;

    case "system":
      handleSystemAction(msg);
      break;
  }
}
```

### 3.2 `system` — Обновление таблицы MWS (приходит при изменениях в MWS)

```json
{
  "type": "system",
  "action": "reload_table",
  "table_id": "tbl_001"
}
```

**Зачем:** Когда кто-то (или внешняя система) обновил данные в таблице MWS Tables, сервер шлёт этот эвент всем пользователям, у которых эта таблица встроена в документ.

**Использование:**

```javascript
function handleSystemAction(msg) {
  if (msg.action === "reload_table") {
    // Найди виджет таблицы с table_id и перезагрузи данные
    const widget = document.querySelector(
      `[data-table-id="${msg.table_id}"]`
    );
    if (widget) {
      widget.dispatchEvent(new CustomEvent("reload"));
      // или: refetchTableData(msg.table_id);
    }
  }
}
```

---

## 4. Отправка CRDT-дельт

Отправляй только бинарные сообщения:

```javascript
// При локальном изменении документа
ydoc.on("update", (update) => {
  if (ws.readyState === WebSocket.OPEN) {
    ws.send(update); // Uint8Array → BinaryMessage
  }
});
```

Текстовые сообщения от клиента **игнорируются** сервером.

---

## 5. Переподключение

При получении кода закрытия `1001 (Going Away)` — сервер перезагружается. Поставь автоматическое переподключение с экспоненциальной задержкой:

```javascript
ws.onclose = (event) => {
  if (event.code === 1001) {
    console.log("Server restarting, reconnecting...");
    setTimeout(() => connect(), 1000 + Math.random() * 2000);
  }
};
```
