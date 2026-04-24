# Sarbon (Gin + Postgres + Redis)

API РґР»СЏ Р»РѕРіРёСЃС‚РёРєРё Рё РіСЂСѓР·РѕРїРµСЂРµРІРѕР·РѕРє (РІРѕРґРёС‚РµР»Рё, РґРёСЃРїРµС‚С‡РµСЂС‹, РіСЂСѓР·С‹, РѕС„С„РµСЂС‹, СЂРµР№СЃС‹).

**Документация API на русском:** [docs/DOCUMENTATION-RU.md](docs/DOCUMENTATION-RU.md) — полное руководство с описанием полей и примерами. Swagger UI: `http://localhost:8080/docs` (или ваш хост + `/docs`).
**Frontend guide по admin panel:** [docs/ADMIN-FRONTEND-RU.md](docs/ADMIN-FRONTEND-RU.md) — полный контракт для `Admin / Tools` и `Admin / Analytics`.


go run ./cmd/admin -login admin -password "Secret123" -name "Main Admin"

## Run locally
1) One command start (backend + postgres + redis):

```bash
docker compose up -d --build
```

Windows (PowerShell) logs:

```powershell
docker compose logs -f api
```

2) Open API:

- API: `http://localhost:8080`
- Swagger UI: `http://localhost:8080/docs`

Notes:
- Migrations run automatically on API startup (`cmd/api`).
- In Docker mode, `DATABASE_URL` and `REDIS_ADDR` are overridden to internal service names (`postgres`, `redis`).
- `.env` is required for both local run and Docker run.

## Admin Analytics API

Creator admins now have a dedicated analytics backend under `/v1/admin/*`:

- `GET /v1/admin/dashboard`
- `GET /v1/admin/metrics`
- `GET /v1/admin/users`
- `GET /v1/admin/users/:id`
- `GET /v1/admin/users/:id/logins`
- `GET /v1/admin/funnels`
- `GET /v1/admin/dropoff`
- `GET /v1/admin/retention`
- `GET /v1/admin/flows/time`
- `GET /v1/admin/flows/conversion`
- `GET /v1/admin/chats`
- `GET /v1/admin/chats/:chat_id/messages`
- `GET /v1/admin/calls`
- `GET /v1/admin/calls/:id`
- `GET /v1/admin/geo`
- `GET /v1/admin/geo/realtime`

Rules:

- access is restricted to `admin.type=creator`
- every endpoint accepts `from`, `to`, `tz`
- every response includes `time_window` and `generated_at_utc`
- analytics events are persisted in `analytics_events`, with session/login rollups in `sessions` and `user_login_stats`
For frontend implementation details, use [docs/ADMIN-FRONTEND-RU.md](docs/ADMIN-FRONTEND-RU.md).


## Run without Docker

If you want to run API directly on host:

1) Start only infra:

```bash
docker compose up -d postgres redis
```

2) Configure env:

```bash
cp .env.example .env
```

3) Run API:

```bash
go run ./cmd/api
```

API: `http://localhost:8080`  
Swagger UI: `http://localhost:8080/docs`

**PostgreSQL** (РІ `.env`: `DATABASE_URL=postgres://sarbon:sarbon@localhost:5432/sarbon?sslmode=disable`):

```bash
# РџСЂРѕРІРµСЂРєР°: РїРѕРґРєР»СЋС‡РµРЅРёРµ Рє Р‘Р” (РїР°СЂРѕР»СЊ: sarbon)
psql -h localhost -p 5432 -U sarbon -d sarbon -c "SELECT 1"
```

Р•СЃР»Рё Р‘Р” РёР»Рё РїРѕР»СЊР·РѕРІР°С‚РµР»СЊ РµС‰С‘ РЅРµ СЃРѕР·РґР°РЅС‹:

```bash
psql -h localhost -p 5432 -U postgres -c "CREATE USER sarbon WITH PASSWORD 'sarbon';"
psql -h localhost -p 5432 -U postgres -c "CREATE DATABASE sarbon OWNER sarbon;"
```

**Redis** (РІ `.env`: `REDIS_ADDR=localhost:6379`):

```bash
# РџСЂРѕРІРµСЂРєР°
redis-cli ping
# РћР¶РёРґР°РµС‚СЃСЏ: PONG
```

Р”Р°Р»СЊС€Рµ РєР°Рє СЃ Docker: РЅР°СЃС‚СЂРѕР№ `.env`, РІС‹РїРѕР»РЅРё РјРёРіСЂР°С†РёРё Рё Р·Р°РїСѓСЃС‚Рё API:

```bash
cp .env.example .env   # Рё РїРѕРїСЂР°РІСЊ DATABASE_URL / REDIS_ADDR РїСЂРё РЅРµРѕР±С…РѕРґРёРјРѕСЃС‚Рё
go run ./cmd/api       # РјРёРіСЂР°С†РёРё РїСЂРёРјРµРЅСЏС‚СЃСЏ РїСЂРё СЃС‚Р°СЂС‚Рµ
```

РџСЂРѕРІРµСЂРєР° API: `curl http://localhost:8080/health` в†’ `{"status":"ok",...}`

## Notes

- Only one DB table is used: `drivers` (see `migrations/`).
- OTP is sent via Telegram Gateway API (configure `TELEGRAM_GATEWAY_*`).
- For dev/testing you can set `UNIVERSAL_OTP_CODE` in `.env` (numeric, same length as `OTP_LENGTH`). Then this code works for all roles and OTP flows (registration/login/password reset/phone change), and gateway sending is bypassed.

