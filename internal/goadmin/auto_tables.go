package goadmin

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	adminctx "github.com/GoAdminGroup/go-admin/context"
	"github.com/GoAdminGroup/go-admin/modules/db"
	"github.com/GoAdminGroup/go-admin/plugins/admin/modules/table"
	"github.com/GoAdminGroup/go-admin/template/types"
	"github.com/GoAdminGroup/go-admin/template/types/form"
	"github.com/jackc/pgx/v5/pgxpool"
)

type columnMeta struct {
	Name       string
	DataType   string // information_schema.data_type
	UDTName    string // pg_catalog.udt_name (useful for arrays)
	IsNullable bool
	Default    *string
}

var (
	reNonAlphaNum   = regexp.MustCompile(`[^a-zA-Z0-9]+`)
	autoTableNames  []string
	extraTableNames []string // custom (non-auto) registered tables
)

// RegisterCustomTableNames appends extra table names (custom generators) so they
// appear on the dashboard alongside auto-generated ones.
func RegisterCustomTableNames(names ...string) {
	extraTableNames = append(extraTableNames, names...)
}

// AutoTableGenerators scans Postgres public schema and creates GoAdmin generators
// for all tables that are NOT already present in existing map.
func AutoTableGenerators(ctx context.Context, databaseURL string, existing map[string]table.Generator) (map[string]table.Generator, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	tables, err := listPublicTables(ctx, pool)
	if err != nil {
		return nil, err
	}

	out := make(map[string]table.Generator)
	for _, tbl := range tables {
		if _, ok := existing[tbl]; ok {
			continue
		}
		if shouldSkipAdminTable(tbl) {
			continue
		}

		cols, err := listTableColumns(ctx, pool, tbl)
		if err != nil || len(cols) == 0 {
			continue
		}
		pk := findPrimaryKeyColumn(ctx, pool, tbl)
		if pk == "" {
			pk = cols[0].Name
		}
		primaryType := goAdminDBTypeForColumn(pk, cols)

		// Capture values for closure
		tableName := tbl
		columns := cols
		primaryKey := pk
		pkType := primaryType

		out[tableName] = func(ac *adminctx.Context) (t table.Table) {
			t = table.NewDefaultTable(ac, table.DefaultConfigWithDriver(db.DriverPostgresql).
				SetPrimaryKey(primaryKey, pkType))
			info := t.GetInfo()

			for _, c := range columns {
				label := humanizeColumn(c.Name)
				field := info.AddField(label, c.Name, goAdminDBTypeForColumn(c.Name, columns))

				// Filters are useful on most scalar fields.
				if isGoodFilterColumn(c) {
					field.FieldFilterable()
				}
				// Sort by timestamps/ids.
				if isGoodSortColumn(c) {
					field.FieldSortable()
				}
				// Mask secrets
				if isSecretColumn(c.Name) {
					field.FieldDisplay(func(model types.FieldModel) interface{} {
						if strings.TrimSpace(model.Value) == "" {
							return "—"
						}
						return "••••••"
					})
				}
				// bytea cannot be shown; show only presence.
				if strings.EqualFold(c.DataType, "bytea") {
					field.FieldDisplay(func(_ types.FieldModel) interface{} { return "[binary]" })
				}
			}

			info.SetTable(tableName).SetTitle(humanizeTable(tableName)).SetDescription("Auto-generated CRUD for table " + tableName)

			formList := t.GetForm()
			for _, c := range columns {
				// Skip binary columns from forms
				if strings.EqualFold(c.DataType, "bytea") {
					continue
				}

				ft := guessFormType(c)
				f := formList.AddField(humanizeColumn(c.Name), c.Name, goAdminDBTypeForColumn(c.Name, columns), ft)

				if c.Name == primaryKey {
					f.FieldDisplayButCanNotEditWhenUpdate()
					// PK is usually generated (uuid/serial). Disable on create to avoid invalid insert.
					f.FieldDisableWhenCreate()
				}
			}

			formList.SetTable(tableName).SetTitle(humanizeTable(tableName)).SetDescription("Auto-generated CRUD")
			return
		}
	}

	// stable order is handled by goadmin, but keep deterministic map build
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	autoTableNames = keys

	return out, nil
}

func getAutoTableNames() []string {
	seen := make(map[string]bool, len(autoTableNames)+len(extraTableNames))
	var out []string
	for _, n := range extraTableNames {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, n := range autoTableNames {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func listPublicTables(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	const q = `
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
ORDER BY table_name`
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func listTableColumns(ctx context.Context, pool *pgxpool.Pool, tableName string) ([]columnMeta, error) {
	const q = `
SELECT
  c.column_name,
  c.data_type,
  c.udt_name,
  (c.is_nullable = 'YES') AS is_nullable,
  c.column_default
FROM information_schema.columns c
WHERE c.table_schema = 'public' AND c.table_name = $1
ORDER BY c.ordinal_position`
	rows, err := pool.Query(ctx, q, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]columnMeta, 0, 64)
	for rows.Next() {
		var m columnMeta
		if err := rows.Scan(&m.Name, &m.DataType, &m.UDTName, &m.IsNullable, &m.Default); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func findPrimaryKeyColumn(ctx context.Context, pool *pgxpool.Pool, tableName string) string {
	const q = `
SELECT a.attname
FROM pg_index i
JOIN pg_class c ON c.oid = i.indrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY(i.indkey)
WHERE n.nspname = 'public' AND c.relname = $1 AND i.indisprimary
ORDER BY a.attnum
LIMIT 1`
	var name string
	_ = pool.QueryRow(ctx, q, tableName).Scan(&name)
	return strings.TrimSpace(name)
}

func goAdminDBTypeForColumn(colName string, cols []columnMeta) db.DatabaseType {
	for _, c := range cols {
		if c.Name != colName {
			continue
		}
		dt := strings.ToLower(strings.TrimSpace(c.DataType))
		udt := strings.ToLower(strings.TrimSpace(c.UDTName))
		switch {
		case dt == "boolean":
			return db.Boolean
		case dt == "integer" || dt == "smallint" || dt == "bigint":
			return db.Int
		case dt == "numeric" || dt == "decimal" || dt == "real" || dt == "double precision":
			return db.Decimal
		case strings.Contains(dt, "timestamp") || dt == "date" || dt == "time without time zone" || dt == "time with time zone":
			return db.Timestamp
		case dt == "uuid":
			return db.Varchar
		case dt == "json" || dt == "jsonb":
			return db.Varchar
		case dt == "array" || strings.HasPrefix(udt, "_"):
			return db.Varchar
		default:
			return db.Varchar
		}
	}
	return db.Varchar
}

func guessFormType(c columnMeta) form.Type {
	dt := strings.ToLower(strings.TrimSpace(c.DataType))
	udt := strings.ToLower(strings.TrimSpace(c.UDTName))
	switch {
	case isSecretColumn(c.Name):
		return form.Password
	case dt == "boolean":
		return form.Switch
	case dt == "integer" || dt == "smallint" || dt == "bigint" || dt == "numeric" || dt == "decimal" || dt == "real" || dt == "double precision":
		return form.Number
	case strings.Contains(dt, "timestamp") || dt == "date":
		return form.Datetime
	case dt == "json" || dt == "jsonb" || dt == "text" || dt == "array" || strings.HasPrefix(udt, "_"):
		return form.TextArea
	default:
		return form.Text
	}
}

func isUUIDColumn(c columnMeta) bool {
	return strings.EqualFold(strings.TrimSpace(c.DataType), "uuid")
}

func isSecretColumn(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "password", "password_hash", "refresh_token", "access_token", "token", "secret", "api_key":
		return true
	default:
		return strings.Contains(n, "password") || strings.Contains(n, "secret") || strings.Contains(n, "token")
	}
}

func isGoodFilterColumn(c columnMeta) bool {
	dt := strings.ToLower(strings.TrimSpace(c.DataType))
	switch dt {
	case "boolean", "integer", "smallint", "bigint", "numeric", "decimal", "real", "double precision", "uuid":
		return true
	}
	if strings.Contains(dt, "timestamp") || dt == "date" {
		return true
	}
	// short-ish string columns
	return dt == "character varying" || dt == "character" || dt == "text"
}

func isGoodSortColumn(c columnMeta) bool {
	n := strings.ToLower(strings.TrimSpace(c.Name))
	return n == "id" || strings.HasSuffix(n, "_at") || strings.HasSuffix(n, "_id")
}

func shouldSkipAdminTable(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "schema_migrations" {
		return true
	}
	// GoAdmin internal tables
	if strings.HasPrefix(n, "goadmin_") {
		return true
	}
	return false
}

func humanizeTable(name string) string {
	return humanizeColumn(strings.ReplaceAll(name, "_", " "))
}

func humanizeColumn(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Field"
	}
	parts := reNonAlphaNum.Split(name, -1)
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
	}
	return strings.Join(parts, " ")
}

