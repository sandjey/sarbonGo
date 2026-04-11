# Схемы потоков (Mermaid)

**Белая доска** на **`/docs`** (три колонки: **водитель**, **CARGO_MANAGER**, **DRIVER_MANAGER** — профиль, уведомления, возможности, логика рейса): **[`/docs/flow`](http://localhost:8080/docs/flow)** — кнопка **«Белая доска»**.

Краткие **пошаговые** диаграммы Mermaid для ролей **водитель** и **cargo manager** (фриланс-диспетчер с `role = CARGO_MANAGER`).

| Файл | Содержание |
|------|------------|
| [driver-flow.md](./driver-flow.md) | От OTP и регистрации до завершения рейса и оценки |
| [cargo-manager-flow.md](./cargo-manager-flow.md) | От регистрации до модерации груза, офферов, рейса и оценки |

**Как смотреть:** откройте `.md` в GitHub / Cursor / VS Code с превью Markdown, либо скопируйте блоки ` ```mermaid ` в [mermaid.live](https://mermaid.live).

### Готовые файлы `.mmd` (один файл = одна диаграмма)

Папка **[mermaid/](./mermaid/)** — чистый код без Markdown: **скопируйте весь файл** в [mermaid.live](https://mermaid.live).

| Файл | Содержание |
|------|------------|
| [mermaid/01-driver-auth-registration.mmd](./mermaid/01-driver-auth-registration.mmd) | Водитель: OTP и регистрация |
| [mermaid/02-driver-trip-lifecycle.mmd](./mermaid/02-driver-trip-lifecycle.mmd) | Водитель: оффер → рейс → COMPLETED |
| [mermaid/03-driver-trip-state.mmd](./mermaid/03-driver-trip-state.mmd) | Машина состояний `trip.status` |
| [mermaid/04-cargo-manager-flow.mmd](./mermaid/04-cargo-manager-flow.mmd) | Cargo manager: груз, модерация, оффер, ограничение роли |
| [mermaid/05-trip-completion-sequence.mmd](./mermaid/05-trip-completion-sequence.mmd) | Sequence: запрос завершения + закрытие CM |

**Заголовки клиента:** для большинства ручек нужны `X-Device-Type`, `X-Language`, `X-Client-Token`; для авторизованных — `X-User-Token` (JWT после входа).

**Актуальность:** пути и статусы сверены с `internal/server/router.go`, `internal/trips`, `internal/domain/enums.go`. Шаг «модерация груза» выполняет **админ** (`POST /v1/admin/cargo/...`), не cargo manager.
