# Техническая спецификация проекта WikiLive (MWS Knowledge Fabric)

**WikiLive** — это локально развертываемая гибридная корпоративная вики-система. Проект сочетает в себе блочный CRDT-редактор текста в реальном времени (на базе Yjs + Tiptap/Lexical с MIT лицензией) и строгие реляционные данные, глубоко интегрируясь с облачным MWS Tables.

## 1. Топология Системы (C4 Model - Container Level)

Система спроектирована для надежного однострочного запуска через `docker-compose`, как требует регламент хакатона.

```mermaid
graph TD
    User([Пользователь])
    
    subgraph "Локальная Инфраструктура (Docker Compose)"
        Nginx["Nginx (Reverse Proxy)"]
        
        Frontend["Frontend (React + Yjs)<br/>Локальный кэш (IndexedDB)"]
        Gateway["Backend Gateway (Java Spring Boot)"]
        SyncEngine["Sync Engine (Go)"]
        AINode["AI & Graph Node (Python FastAPI)"]
        
        PostgreSQL[(PostgreSQL)]
        Redis[(Redis Pub/Sub)]
        ChromaDB[(ChromaDB - Vector DB)]
    end
    
    MWS["MWS Tables API / MWS GPT (Внешнее Облако)"]
    
    User -- "HTTP REST / UI" --> Nginx
    
    Nginx -- "Статика & UI" --> Frontend
    Nginx -- "HTTP API (/api/*)" --> Gateway
    Nginx -- "ws:// (/ws/* - Бинарные дельты Yjs)" --> SyncEngine
    Nginx -- "HTTP API (/ai/*)" --> AINode
    
    SyncEngine -- "Pub/Sub масштабирование" --> Redis
    SyncEngine -- "Извлечение связей (Backlinks)" --> Gateway
    SyncEngine -- "Автосохранение снепшотов" --> Gateway
    
    Gateway -- "CRUD метаданных, версий, ссылок" --> PostgreSQL
    Gateway -- "Проксирование с API-ключом" --> MWS
    
    AINode -- "Векторный поиск" --> ChromaDB
    AINode -- "Промпты для генерации/сайдбара" --> MWS
```

**Разделение ответственности:**
*   **Frontend (React):** Отрисовка UI по Design Kit, slash-menu, горячие клавиши. In-line сохранение в локальный кэш браузера при обрыве сети.
*   **Backend (Java):** Авторизация, управление воркспейсами, REST API таблиц (проксирование в MWS).
*   **Sync Engine (Go + Redis):** Обработка WebSockets, коллаборативное редактирование, парсинг ссылок внутри текста при сохранении (для графа и Backlinks).
*   **AI Node (Python):** Работа с MWS GPT, генерация блоков, анализ связей для графа.

---

## 2. Модель Данных (ERD PostgreSQL)

Схема расширена для поддержки обязательных требований (Backlinks) и дополнительных баллов (Версионирование, Комментарии).

```mermaid
erDiagram
    users {
        uuid id PK
        string name
        string mws_mock_token
    }
    
    workspaces {
        uuid id PK
        string name
        timestamp created_at
    }
    
    pages {
        uuid id PK
        uuid workspace_id FK
        string title
        timestamp updated_at
    }
    
    document_snapshots {
        uuid id PK
        uuid page_id FK
        bytea document_state "Бинарный дамп Yjs"
        timestamp created_at "Для истории правок / версионирования"
    }
    
    page_links {
        uuid source_page_id FK
        uuid target_page_id FK
        timestamp created_at "Для графа и обратных ссылок (Backlinks)"
    }
    
    comments {
        uuid id PK
        uuid page_id FK
        string block_id "ID блока текста"
        string content
        timestamp created_at
    }

    workspaces ||--o{ pages : "содержит"
    users }|--o{ workspaces : "участвует"
    pages ||--o{ document_snapshots : "имеет версии"
    pages ||--o{ page_links : "ссылается на"
    pages ||--o{ page_links : "на нее ссылаются"
    pages ||--o{ comments : "обсуждается"
```

*Примечание по наполнению `page_links`:* Когда Go Sync Engine сохраняет финальный дамп документа в БД, он асинхронно парсит текст/JSON на наличие паттернов ссылок `[[page_id]]` или `href="/page/{id}"` и дергает внутренний эндпоинт Java Gateway для обновления таблицы `page_links`.

---

## 3. Интеграция с MWS Tables: Массовое обновление (Sequence Diagram)

Флоу обновления ячеек таблицы внутри страницы. Использование метода `PATCH` для предотвращения спама запросами (учитываем массовое выделение или D&D).

```mermaid
sequenceDiagram
    actor U as Пользователь
    participant F as Frontend (React Widget)
    participant K as Backend Gateway (Java)
    participant M as MWS Tables API (Cloud)

    U->>F: Переносит 3 карточки (массовое изменение)
    F->>F: Дебаунс (Debounce 300ms) / Сбор изменений
    F->>F: Блокирует UI виджета (Soft loader)
    F->>K: PATCH /api/tables/{table_id}/records<br/>Body: [ {id, fields}, {id, fields} ]
    
    activate K
    K->>K: Валидация + Подмена токена на боевой MWS Key
    K->>M: Проксирует HTTP PATCH (Batch Update)
    
    activate M
    M-->>K: 200 OK (Данные сохранены в MWS)
    deactivate M
    
    K->>K: (Опционально) Логирование действия
    K-->>F: 200 OK
    deactivate K
    
    F->>F: Снимает лоадер, показывает Success
```

---

## 4. Двусторонняя синхронизация: "Живая" таблица

Как фронтенд узнает, что данные в MWS Tables изменил кто-то другой (или другой сервис)? Т.к. вебхуков нет, реализуем Smart Polling (Опрос).

```mermaid
sequenceDiagram
    participant F as Frontend (React Widget)
    participant K as Backend Gateway (Java)
    participant M as MWS Tables API (Cloud)

    loop Каждые 10-15 секунд (если вкладка активна)
        F->>K: GET /api/tables/{table_id}/records?since={last_sync_timestamp}
        activate K
        K->>M: GET /fusion/v1/datasheets/{dstId}/records
        activate M
        M-->>K: Возвращает актуальные строки
        deactivate M
        K-->>F: Отдает Diff (только изменившиеся строки)
        deactivate K
        
        opt Если есть изменения
            F->>F: Реактивно обновляет ячейки (без перезагрузки всей страницы)
        end
    end
```
*Альтернатива:* Polling может делать Java Backend на своей стороне и при наличии изменений пушить их во Frontend через WebSocket (Go Sync Engine). Для хакатона вариант с фронтенд-поллингом (показан выше) надежнее и проще в реализации.
