# Техническая спецификация проекта WikiLive (MWS Knowledge Fabric)

WikiLive — это локально развертываемая гибридная корпоративная вики-система. Проект сочетает в себе блочный CRDT-редактор текста в реальном времени (на базе Yjs/Loro) и строгие реляционные данные, интегрируясь с облачным MWS Tables.

Ниже представлена полная системная архитектура проекта, собранная для быстрого понимания инфраструктуры и информационных потоков.

---

## 1. Топология Системы (C4 Model - Container Level)

Общая схема взаимодействия контейнеров в пределах локального деплоя `docker-compose`:

```mermaid
graph TD
    User([Пользователь])
    
    subgraph "Локальная Инфраструктура (Docker Compose)"
        Nginx["Nginx (Reverse Proxy)"]
        
        Frontend["Frontend (React + Yjs/Loro)"]
        Gateway["Backend Gateway (Java/Kotlin Ktor)"]
        SyncEngine["Sync Engine (Go)"]
        AINode["AI Node (Python)"]
        
        PostgreSQL[(PostgreSQL)]
        Redis[(Redis Pub/Sub)]
        ChromaDB[(ChromaDB)]
    end
    
    MWS["MWS Tables API (Внешнее Облако)"]
    
    User -- "HTTP REST / WS" --> Nginx
    
    Nginx -- "Статика" --> Frontend
    Nginx -- "HTTP API" --> Gateway
    Nginx -- "ws:// (Бинарные дельты)" --> SyncEngine
    
    SyncEngine -- "Pub/Sub масштабирование" --> Redis
    SyncEngine -- "Автосохранение снепшота (HTTP POST)" --> Gateway
    
    Gateway -- "Read/Write стейт и метаданные" --> PostgreSQL
    Gateway -- "Проксирование с API-ключом" --> MWS
    Gateway -- "Запросы генерации контента" --> AINode
    
    AINode -- "Векторный поиск" --> ChromaDB
```

---

## 2. Модель Данных (ERD PostgreSQL)

Минималистичная схема хранения метаданных и бинарных снепшотов для MVP:

```mermaid
erDiagram
    users {
        uuid id PK
        string name
        string mock_token
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
        bytea document_state "Бинарный снепшот от Go Sync Engine"
        timestamp updated_at
    }
    
    workspaces ||--o{ pages : "содержит"
    users }|--o{ workspaces : "участвует"
```

---

## 3. Флоу обновления ячейки MWS (Sequence Diagram)

Самый сложный гибридный процесс системы: изменение данных во внешнем источнике (MWS Tables) непосредственно из текстового документа, минуя CRDT-движок.

```mermaid
sequenceDiagram
    actor U as Пользователь
    participant F as Frontend (React Widget)
    participant K as Backend Gateway (Kotlin)
    participant M as MWS Tables API (Cloud)

    U->>F: Изменяет значение в виджете таблицы
    F->>F: Блокирует виджет (отображает лоадер)
    F->>K: PUT /api/tables/{table_id}/rows/{row_id}<br/>Authorization: Bearer <mock_token>
    
    activate K
    K->>K: Валидирует токен пользователя
    K->>K: Подменяет токен на боевой API-ключ MWS
    K->>M: Проксирует HTTP PUT запрос (JSON)
    
    activate M
    M-->>K: 200 OK (Данные сохранены в MWS)
    deactivate M
    
    K->>K: Логирование / сохранение метаданных (Postgres)
    K-->>F: 200 OK (Ответ для фронтенда)
    deactivate K
    
    F->>F: Снимает лоадер
    F->>U: Отрисовывает обновленное значение ячейки
```

---

*Спецификация API Backend шлюза выделена в отдельный файл `docs/openapi.yaml`.*
