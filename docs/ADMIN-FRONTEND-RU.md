# Admin Frontend API Guide

Полный рабочий документ для frontend-разработчика по двум зонам админки:

- `Admin / Tools`
- `Admin / Analytics`

Цель документа: дать фронтенду весь нужный контракт сразу, чтобы не пришлось уточнять у backend-команды структуру ответов, параметры фильтрации, ограничения доступа и expected UX.

## 1. Доступ и роли

Есть два режима доступа:

- `admin` любого типа: доступ к `Admin / Tools`
- только `admin.type=creator`: доступ к `Admin / Analytics`

Это важно для UI:

- экран `Tools` можно показывать любому авторизованному администратору
- экран `Analytics` нужно скрывать или блокировать для non-creator admin

Если non-creator admin вызовет analytics endpoint, backend вернет `403 forbidden`.

## 2. Базовые требования ко всем admin-запросам

Base URL:

```text
/v1/admin/*
```

Обязательные headers:

```http
X-Device-Type: web
X-Language: ru
X-Client-Token: <client-token>
X-User-Token: <admin-access-token>
```

Важно:

- `X-User-Token` должен быть JWT с `role=admin`
- `X-Language` влияет на `description` в envelope, но не меняет имена полей JSON
- `status` в envelope всегда на английском: `success` или `error`

## 3. Авторизация администратора

### 3.1 Login

```http
POST /v1/admin/auth/login/password
```

Request:

```json
{
  "login": "admin",
  "password": "Secret123"
}
```

Success response:

```json
{
  "status": "success",
  "code": 200,
  "description": "ok",
  "data": {
    "tokens": {
      "access_token": "jwt",
      "refresh_token": "jwt",
      "expires_in": 3600,
      "expires_at": 1713940000000,
      "refresh_expires_in": 2592000,
      "refresh_expires_at": 1716532000000
    },
    "admin": {
      "id": "uuid",
      "login": "admin",
      "name": "Main Admin",
      "status": "active",
      "type": "creator"
    }
  }
}
```

Ключевое для frontend:

- `admin.type` нужно сохранить в auth-store
- именно `admin.type === "creator"` открывает аналитику

### 3.2 Refresh

Используется стандартный refresh endpoint:

```http
POST /v1/admin/auth/refresh
```

## 4. Общий envelope ответа

Все admin endpoints возвращают envelope:

```json
{
  "status": "success",
  "code": 200,
  "description": "ok",
  "data": {}
}
```

Поля:

- `status`: `success | error`
- `code`: HTTP status code
- `description`: локализованный текст
- `data`: полезная нагрузка

## 5. Общие правила для analytics endpoints

Эти правила касаются всех endpoints из `Admin / Analytics`.

### 5.1 Общие query-параметры окна

Почти все analytics endpoints поддерживают:

- `from`
- `to`
- `tz`

Примеры:

```text
from=2026-04-01
from=2026-04-01T10:00:00Z
to=2026-04-24
tz=UTC
tz=Asia/Tashkent
```

Поведение backend:

- `tz` по умолчанию: `UTC`
- `from` по умолчанию: текущий момент минус 30 дней
- `to` по умолчанию: текущий момент
- если `tz` невалиден: `400 invalid_payload_detail`
- если `from >= to`: `400 invalid_payload_detail`

### 5.2 Стандартная meta-обертка analytics

Все analytics endpoints возвращают внутри `data` такой каркас:

```json
{
  "time_window": {
    "from": "2026-04-01T00:00:00Z",
    "to": "2026-04-24T00:00:00Z",
    "tz": "UTC"
  },
  "generated_at_utc": "2026-04-24T09:00:00Z",
  "data": {}
}
```

Значит для frontend:

- данные для экрана лежат в `response.data.data`
- `response.data.time_window` использовать для шапки, subtitle, export label
- `generated_at_utc` использовать для "Обновлено в ..."

### 5.3 Пагинация и сортировка

Во многих analytics list endpoints используются:

- `limit`
- `offset`
- `sort_by`
- `sort_dir`

Ограничения:

- стандартный `limit`: `20`
- max `limit`: `100`
- `offset >= 0`

## 6. Admin / Tools

## 6.1 Список грузов на модерации

```http
GET /v1/admin/cargo/moderation?limit=20&offset=0
```

Назначение:

- экран очереди модерации грузов

Response `data`:

```json
{
  "items": [
    {
      "id": "uuid",
      "weight": 20.5,
      "volume": 82,
      "truck_type": "TENT",
      "status": "PENDING_MODERATION",
      "created_at": "2026-04-24T09:00:00Z",
      "created_by_type": "DISPATCHER",
      "created_by_id": "uuid"
    }
  ],
  "total": 124
}
```

UI-заметки:

- backend здесь не возвращает полный cargo card
- для таблицы доступны только ключевые поля очереди
- `created_by_type` помогает показать badge: `DISPATCHER | COMPANY | ADMIN`

## 6.2 Принять груз на модерации

```http
POST /v1/admin/cargo/{id}/moderation/accept
```

Request body:

```json
{
  "search_visibility": "all"
}
```

Допустимые значения:

- `all`
- `company`

Особое правило:

- если груз создан `DISPATCHER`, backend принудительно использует только `all`

Success `data`:

```json
{
  "status": "SEARCHING_ALL"
}
```

Возможные ошибки:

- `400 invalid_id`
- `400 cargo_not_pending_moderation`
- `404 cargo_not_found`

## 6.3 Отклонить груз на модерации

```http
POST /v1/admin/cargo/{id}/moderation/reject
```

Request body:

```json
{
  "reason": "Недостаточно данных по маршруту"
}
```

Success `data`:

```json
{
  "status": "CANCELLED"
}
```

Ошибки:

- `400 invalid_id`
- `400 moderation_rejection_reason_required`
- `400 cargo_not_pending_moderation`

UI-заметки:

- `reason` обязателен
- отклонение лучше делать через modal с textarea

## 6.4 Статус push-инфраструктуры

```http
GET /v1/admin/push/status
```

Назначение:

- сервисная диагностика Firebase/FCM
- экран “Push Health”

Response `data`:

```json
{
  "enabled": true,
  "project_id": "sarbon-firebase-project",
  "config": {
    "push_notifications_enabled": true,
    "firebase_project_id_configured": true,
    "firebase_project_id": "sarbon-firebase-project",
    "firebase_credentials_file_raw": "secrets/firebase.json",
    "firebase_credentials_path_resolved": "C:/.../firebase.json",
    "firebase_credentials_file_found": true,
    "push_service_initialized": true
  }
}
```

Как трактовать:

- `enabled=true`: push реально может отправляться
- `push_service_initialized=false`: сервис не собрался на старте
- `firebase_credentials_file_found=false`: сервер не видит credentials file

## 6.5 Тестовая отправка push по token

```http
POST /v1/admin/push/send
```

Request body:

```json
{
  "push_token": "fcm-token",
  "title": "Sarbon Test",
  "body": "Test push notification from admin",
  "data": {
    "kind": "manual_test"
  },
  "recipient_kind": "driver",
  "recipient_id": "uuid"
}
```

Поля:

- `push_token` обязательно
- `recipient_kind` опционально: `driver | dispatcher`
- `recipient_id` опционально

Если передать `recipient_kind + recipient_id`, backend попробует сохранить token за пользователем.

Success `data`:

```json
{
  "sent": true,
  "token": "fcm-token",
  "fcm_message_id": "projects/.../messages/...",
  "firebase_project": "sarbon-firebase-project",
  "token_saved": true,
  "token_save_error": ""
}
```

Ошибки:

- `400 invalid_payload_detail`
- `502 firebase error`
- `503 push_not_enabled`

## 6.6 Проверка token по пользователю

```http
GET /v1/admin/push/recipient-status?kind=driver&id=<uuid>
```

Request query:

- `kind`: `driver | dispatcher`
- `id`: UUID

Success `data`:

```json
{
  "kind": "driver",
  "id": "uuid",
  "token_found": true,
  "token_length": 163,
  "token_prefix": "dQw4w9WgXcQ12345",
  "source": "drivers.push_token"
}
```

`source` может быть:

- `drivers.push_token`
- `freelance_dispatchers.push_token`
- `drivers.push_token(fallback)`
- `freelance_dispatchers.push_token(fallback)`
- `token_not_found`
- `recipient_id_nil`

## 6.7 Отправка push по `kind + id`

```http
POST /v1/admin/push/send-by-recipient
```

Request body:

```json
{
  "kind": "dispatcher",
  "id": "uuid",
  "title": "Sarbon",
  "body": "System push (admin trigger)",
  "data": {
    "kind": "manual_admin_push"
  }
}
```

Success `data`:

```json
{
  "sent": true,
  "kind": "dispatcher",
  "id": "uuid",
  "token_found": true,
  "token_prefix": "dQw4w9WgXcQ12345",
  "source": "freelance_dispatchers.push_token",
  "fcm_message_id": "projects/.../messages/...",
  "firebase_project": "sarbon-firebase-project"
}
```

Ошибки:

- `400 invalid_payload_detail`
- `400 invalid_id`
- `502 firebase error`
- `503 push_not_enabled`

## 7. Admin / Analytics

## 7.1 Dashboard

```http
GET /v1/admin/dashboard?from=2026-04-01&to=2026-04-24&tz=UTC&role=driver_manager
```

Назначение:

- главный экран аналитики

Response `data.data`:

```json
{
  "kpi": {
    "cargo_count": 320,
    "offer_count": 1180,
    "trip_count": 210,
    "completed_trip_count": 170,
    "cancelled_trip_count": 18,
    "registered_users": 44,
    "login_success_count": 300,
    "login_failed_count": 17,
    "completion_rate_pct": 80.95,
    "cancellation_rate_pct": 8.57,
    "offer_acceptance_pct": 56.1
  },
  "funnel": {
    "stages": [
      {
        "stage": "cargo",
        "count": 320,
        "conversion_from_prev_pct": 100,
        "dropoff_from_prev": 0
      }
    ]
  },
  "role_breakdown": [
    {
      "role": "driver",
      "total_users": 120
    }
  ],
  "geo": [
    {
      "geo_city": "Tashkent",
      "role": "driver",
      "events_count": 120,
      "login_count": 44,
      "unique_users": 21
    }
  ],
  "growth": {
    "cargo_delta_pct": 12.1,
    "offer_delta_pct": -4.3,
    "trip_delta_pct": 8.4
  },
  "alerts": [
    {
      "severity": "medium",
      "code": "trip_completion_low",
      "message": "Trip completion rate is below 40%",
      "value": 31.4
    }
  ]
}
```

Для UI:

- `kpi` -> карточки
- `funnel.stages` -> funnel widget
- `role_breakdown` -> donut/bar
- `geo` -> geo table/map
- `growth` -> deltas vs previous window
- `alerts` -> alert banners

## 7.2 Universal Metrics

```http
GET /v1/admin/metrics?group_by=time&interval=day&metrics=cargo_count,offer_count,trip_count
```

Поддерживаемые `group_by`:

- `time`
- `role`
- `user`

Поддерживаемые `interval`:

- `hour`
- `day`
- `week`
- `month`

Response `data.data`:

```json
{
  "group_by": "time",
  "interval": "day",
  "items": [
    {
      "bucket": "2026-04-20T00:00:00Z",
      "values": {
        "cargo_count": 10,
        "offer_count": 54,
        "trip_count": 7,
        "completed_trip_count": 5,
        "cancelled_trip_count": 1,
        "login_success_count": 22,
        "login_failed_count": 2,
        "offer_accepted_count": 13
      }
    }
  ]
}
```

Если `group_by=role`:

```json
{
  "role": "driver_manager",
  "values": {
    "offer_count": 88,
    "trip_count": 21
  }
}
```

Если `group_by=user`:

```json
{
  "role": "driver",
  "user_id": "uuid",
  "values": {
    "offers_total": 14,
    "trips_total": 4
  }
}
```

## 7.3 Users List

```http
GET /v1/admin/users?role=driver&search=ali&sort_by=last_seen_at&sort_dir=desc&limit=20&offset=0
```

Response `data.data`:

```json
{
  "items": [
    {
      "id": "uuid",
      "role": "driver",
      "display_name": "ali_driver",
      "phone_or_login": "+998901234567",
      "status": "active",
      "registered_at": "2026-04-01T09:00:00Z",
      "last_seen_at": "2026-04-24T08:55:00Z",
      "manager_role": null,
      "admin_type": null,
      "primary_source": "drivers"
    }
  ],
  "page": {
    "limit": 20,
    "offset": 0,
    "sort_by": "last_seen_at",
    "sort_dir": "desc",
    "total": 145
  }
}
```

Важное поведение backend:

- список режется по окну `from/to`
- то есть это не “все пользователи за всю историю”, а пользователи с `registered_at` внутри окна

`primary_source`:

- `drivers`
- `freelance_dispatchers`
- `admins`

## 7.4 User Detail

```http
GET /v1/admin/users/{id}
```

Response `data.data`:

```json
{
  "user": {
    "id": "uuid",
    "role": "driver_manager",
    "display_name": "dm_01",
    "phone_or_login": "+99890...",
    "status": "active",
    "registered_at": "2026-04-01T09:00:00Z",
    "last_seen_at": "2026-04-24T08:55:00Z",
    "manager_role": "DRIVER_MANAGER",
    "admin_type": null,
    "primary_source": "freelance_dispatchers"
  },
  "logins": {
    "total_logins": 14,
    "successful_logins": 13,
    "failed_logins": 1,
    "last_login_at_utc": "2026-04-24T08:20:00Z",
    "average_session_duration_sec": 1432.2,
    "completed_sessions": 9,
    "recent_sessions": [
      {
        "session_id": "jti",
        "started_at_utc": "2026-04-24T08:20:00Z",
        "ended_at_utc": "2026-04-24T08:50:00Z",
        "duration_seconds": 1800,
        "device_type": "web",
        "platform": "",
        "geo_city": "Tashkent",
        "ip_hash": "..."
      }
    ]
  },
  "metrics": {
    "linked_drivers": 8,
    "offer_count": 31,
    "trip_count": 12,
    "completed_trip_count": 10,
    "cancelled_trip_count": 1,
    "completion_rate": 83.33
  }
}
```

`metrics` зависит от роли:

- `driver`
- `cargo_manager`
- `driver_manager`
- `admin`

Для `driver` есть:

- `offers_total`
- `offers_accepted`
- `trips_total`
- `trips_completed`
- `trips_cancelled`
- `completion_rate`
- `chat_count`
- `messages_sent`
- `calls_total`

Для `cargo_manager` есть:

- `cargo_count`
- `offer_count`
- `trip_count`
- `completed_trip_count`
- `cancelled_trip_count`
- `completion_rate`

Для `driver_manager` есть:

- `linked_drivers`
- `offer_count`
- `trip_count`
- `completed_trip_count`
- `cancelled_trip_count`
- `completion_rate`

Для `admin` есть:

- `admin_actions`
- `cargo_moderated_count`
- `companies_created`

## 7.5 User Logins History

```http
GET /v1/admin/users/{id}/logins?limit=20&offset=0
```

Response `data.data`:

```json
{
  "items": [
    {
      "session_id": "jti",
      "role": "driver",
      "started_at_utc": "2026-04-24T08:20:00Z",
      "ended_at_utc": "2026-04-24T08:50:00Z",
      "duration_seconds": 1800,
      "device_type": "web",
      "platform": "",
      "geo_city": "Tashkent",
      "ip_hash": "..."
    }
  ],
  "page": {
    "limit": 20,
    "offset": 0,
    "total": 17
  }
}
```

## 7.6 Funnels

```http
GET /v1/admin/funnels?role=cargo_manager
```

Response `data.data`:

```json
{
  "stages": [
    {
      "stage": "cargo",
      "count": 320,
      "conversion_from_prev_pct": 100,
      "dropoff_from_prev": 0
    },
    {
      "stage": "offer",
      "count": 1180,
      "conversion_from_prev_pct": 368.75,
      "dropoff_from_prev": 0
    },
    {
      "stage": "trip",
      "count": 210,
      "conversion_from_prev_pct": 17.8,
      "dropoff_from_prev": 970
    },
    {
      "stage": "completed",
      "count": 170,
      "conversion_from_prev_pct": 80.95,
      "dropoff_from_prev": 40
    }
  ]
}
```

Важно:

- это не classical strict 1-to-1 funnel
- `offer_count` может быть больше `cargo_count`, поэтому конверсия между этапами иногда > 100%

## 7.7 Dropoff

```http
GET /v1/admin/dropoff
```

Response identical по структуре `funnels`, но backend отдает:

```json
{
  "items": [
    {
      "stage": "cargo",
      "count": 320,
      "conversion_from_prev_pct": 100,
      "dropoff_from_prev": 0
    }
  ]
}
```

Для frontend:

- можно использовать тот же renderer, что и funnel

## 7.8 Retention

```http
GET /v1/admin/retention?role=driver
```

Response `data.data`:

```json
{
  "items": [
    {
      "cohort": "2026-04-01",
      "registered_users": 20,
      "day_1_rate_pct": 55,
      "day_7_rate_pct": 27.5,
      "day_30_rate_pct": 10
    }
  ]
}
```

Retention activity считается по событиям:

- `login_success`
- `offer_created`
- `trip_started`
- `chat_message_sent`

## 7.9 Flow Time

```http
GET /v1/admin/flows/time?role=driver_manager
```

Response `data.data`:

```json
{
  "items": [
    {
      "name": "registration_to_first_login",
      "description": "Time from registration to first successful login",
      "count": 14,
      "average_sec": 1200,
      "median_sec": 600,
      "p95_sec": 5300
    }
  ]
}
```

Набор `name`:

- `registration_to_first_login`
- `login_to_first_offer`
- `offer_to_accepted`
- `accepted_to_trip_started`
- `trip_to_completed`

## 7.10 Flow Conversion

```http
GET /v1/admin/flows/conversion
```

Response `data.data`:

```json
{
  "registration_to_login_pct": null,
  "cargo_to_offer_pct": 55.2,
  "offer_to_trip_pct": 18.6,
  "trip_to_completed_pct": 80.95,
  "overall_cargo_to_done_pct": 17.1
}
```

Важно:

- `registration_to_login_pct` сейчас backend возвращает `null`
- frontend должен уметь показать `N/A`, а не падать

## 7.11 Chats List

```http
GET /v1/admin/chats?user_id=<uuid>&search=ali&limit=20&offset=0
```

Response `data.data`:

```json
{
  "items": [
    {
      "chat_id": "uuid",
      "user_a_id": "uuid",
      "user_a_name": "ali_driver",
      "user_a_role": "driver",
      "user_b_id": "uuid",
      "user_b_name": "dm_01",
      "user_b_role": "driver_manager",
      "created_at_utc": "2026-04-24T08:00:00Z",
      "last_message_id": "uuid",
      "last_message_body": "Добрый день",
      "last_message_at_utc": "2026-04-24T08:15:00Z",
      "message_count": 34
    }
  ],
  "page": {
    "limit": 20,
    "offset": 0,
    "total": 10
  }
}
```

`search` ищет по:

- имени первого участника
- имени второго участника
- телу последнего сообщения

## 7.12 Chat Messages

```http
GET /v1/admin/chats/{chat_id}/messages?limit=20&offset=0
```

Response `data.data`:

```json
{
  "items": [
    {
      "id": "uuid",
      "sender_id": "uuid",
      "type": "text",
      "body": "Добрый день",
      "payload": null,
      "created_at_utc": "2026-04-24T08:15:00Z",
      "updated_at_utc": "2026-04-24T08:15:00Z",
      "deleted_at_utc": null
    }
  ],
  "page": {
    "limit": 20,
    "offset": 0,
    "total": 34
  }
}
```

Для frontend:

- `payload` может быть объектом, если это media/location message
- сортировка backend: newest first

## 7.13 Calls List

```http
GET /v1/admin/calls?status=ENDED&user_id=<uuid>&limit=20&offset=0
```

Статусы звонка:

- `RINGING`
- `ACTIVE`
- `ENDED`
- `DECLINED`
- `MISSED`
- `CANCELLED`

Response `data.data`:

```json
{
  "items": [
    {
      "id": "uuid",
      "conversation_id": "uuid",
      "caller_id": "uuid",
      "callee_id": "uuid",
      "status": "ENDED",
      "created_at_utc": "2026-04-24T08:00:00Z",
      "started_at_utc": "2026-04-24T08:01:00Z",
      "ended_at_utc": "2026-04-24T08:11:00Z",
      "ended_by": "uuid",
      "ended_reason": "peer_hangup",
      "duration_seconds": 600
    }
  ],
  "page": {
    "limit": 20,
    "offset": 0,
    "total": 12
  }
}
```

## 7.14 Call Detail

```http
GET /v1/admin/calls/{id}
```

Response `data.data`:

```json
{
  "id": "uuid",
  "conversation_id": "uuid",
  "caller_id": "uuid",
  "callee_id": "uuid",
  "status": "ENDED",
  "created_at_utc": "2026-04-24T08:00:00Z",
  "started_at_utc": "2026-04-24T08:01:00Z",
  "ended_at_utc": "2026-04-24T08:11:00Z",
  "ended_by": "uuid",
  "ended_reason": "peer_hangup",
  "duration_seconds": 600,
  "recording_url": null,
  "events": [
    {
      "id": "uuid",
      "actor_id": "uuid",
      "event_type": "call.started",
      "payload": {},
      "created_at_utc": "2026-04-24T08:01:00Z"
    }
  ]
}
```

Важно:

- `recording_url` сейчас backend возвращает `null`
- frontend должен поддерживать `null` без special-case ошибок

## 7.15 Geo

```http
GET /v1/admin/geo?role=driver&from=2026-04-01&to=2026-04-24&limit=20
```

Response `data.data`:

```json
{
  "items": [
    {
      "geo_city": "Tashkent",
      "role": "driver",
      "events_count": 120,
      "login_count": 44,
      "unique_users": 21
    }
  ]
}
```

## 7.16 Geo Realtime

```http
GET /v1/admin/geo/realtime
```

Response `data.data`:

```json
{
  "items": [
    {
      "role": "driver_manager",
      "geo_city": "Tashkent",
      "online_users": 5,
      "last_seen_at_utc": "2026-04-24T08:58:10Z"
    }
  ]
}
```

Важно:

- realtime считается по пользователям, активным за последние 15 минут

## 8. Рекомендуемая структура экранов

## 8.1 Admin Tools

Рекомендуемые блоки:

- `Cargo Moderation Queue`
- `Push Health`
- `Push Send Test`
- `Push Recipient Inspector`

## 8.2 Admin Analytics

Рекомендуемые экраны:

- `Dashboard`
- `Users`
- `Funnels & Conversion`
- `Retention`
- `Chats`
- `Calls`
- `Geo`

## 8.3 Drill-down

Рекомендуемый UX:

- клик по user -> `GET /v1/admin/users/{id}`
- отдельная вкладка логинов -> `GET /v1/admin/users/{id}/logins`
- клик по chat -> `GET /v1/admin/chats/{chat_id}/messages`
- клик по call -> `GET /v1/admin/calls/{id}`

## 9. Frontend edge cases, которые нужно учесть

- `registration_to_login_pct` в `flows/conversion` сейчас `null`
- `recording_url` в `calls/{id}` сейчас `null`
- `last_seen_at`, `ended_at_utc`, `deleted_at_utc` могут быть `null`
- analytics list endpoints возвращают полезные данные внутри `data.data`, а не прямо внутри `data`
- tools endpoints возвращают данные напрямую внутри `data`
- `search_visibility=company` может быть проигнорирован backend для cargo, созданного `DISPATCHER`
- `search` в users/chats может вернуть пустой массив при `total=0`, это штатно

## 10. Что лучше не предполагать на фронте

- не предполагать, что все проценты всегда `0..100`
- не предполагать, что все даты уже локализованы под UI timezone
- не предполагать, что у пользователя есть `manager_role` или `admin_type`
- не предполагать, что analytics доступна каждому admin

## 11. Быстрый checklist для frontend

- сохранить `admin.type` после login
- скрывать analytics menu для non-creator admin
- для analytics всегда уметь передавать `from`, `to`, `tz`
- правильно читать payload как `response.data.data`
- отдельно обработать `null` в conversion/calls
- поддержать пагинацию через `limit/offset`
- не строить UI-логику на `description`, только на полях JSON
