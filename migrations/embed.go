package migrations

import "embed"

// FS содержит все SQL-файлы миграций для golang-migrate (iofs).
// Используется API при старте, чтобы не зависеть от cwd и пути к папке на диске.
//
//go:embed *.sql
var FS embed.FS
