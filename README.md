# Sarbon (Gin + Postgres + Redis)

API для логистики и грузоперевозок (водители, диспетчеры, грузы, офферы, рейсы).

**Документация API на русском:** [docs/DOCUMENTATION-RU.md](docs/DOCUMENTATION-RU.md) — полное руководство с описанием полей и примерами. Swagger UI: `http://localhost:8080/docs` (или ваш хост + `/docs`).

go run ./cmd/admin -login admin -password "Secret123" -name "Main Admin"

## Run locally
1) One command start (backend + postgres + redis):

```bash
docker compose up -d --build
```

2) Open API:

- API: `http://localhost:8080`
- Swagger UI: `http://localhost:8080/docs`

Notes:
- Migrations run automatically on API startup (`cmd/api`).
- In Docker mode, `DATABASE_URL` and `REDIS_ADDR` are overridden to internal service names (`postgres`, `redis`).

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

**PostgreSQL** (в `.env`: `DATABASE_URL=postgres://sarbon:sarbon@localhost:5432/sarbon?sslmode=disable`):

```bash
# Проверка: подключение к БД (пароль: sarbon)
psql -h localhost -p 5432 -U sarbon -d sarbon -c "SELECT 1"
```

Если БД или пользователь ещё не созданы:

```bash
psql -h localhost -p 5432 -U postgres -c "CREATE USER sarbon WITH PASSWORD 'sarbon';"
psql -h localhost -p 5432 -U postgres -c "CREATE DATABASE sarbon OWNER sarbon;"
```

**Redis** (в `.env`: `REDIS_ADDR=localhost:6379`):

```bash
# Проверка
redis-cli ping
# Ожидается: PONG
```

Дальше как с Docker: настрой `.env`, выполни миграции и запусти API:

```bash
cp .env.example .env   # и поправь DATABASE_URL / REDIS_ADDR при необходимости
go run ./cmd/api       # миграции применятся при старте
```

Проверка API: `curl http://localhost:8080/health` → `{"status":"ok",...}`

## Notes

- Only one DB table is used: `drivers` (see `migrations/`).
- OTP is sent via Telegram Gateway API (configure `TELEGRAM_GATEWAY_*`).

