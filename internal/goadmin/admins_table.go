package goadmin

import (
	adminctx "github.com/GoAdminGroup/go-admin/context"
	"github.com/GoAdminGroup/go-admin/modules/db"
	"github.com/GoAdminGroup/go-admin/plugins/admin/modules/table"
	"github.com/GoAdminGroup/go-admin/template/types"
	"github.com/GoAdminGroup/go-admin/template/types/form"
)

// adminsTableGenerator returns a GoAdmin table generator for the `admins` table
// with proper dropdowns for status and type so PostgreSQL check constraints are satisfied.
// Allowed DB values (lowercase): status ∈ {active, inactive, blocked}, type ∈ {creator, operator}.
func adminsTableGenerator() table.Generator {
	return func(ac *adminctx.Context) (t table.Table) {
		t = table.NewDefaultTable(ac, table.DefaultConfigWithDriver(db.DriverPostgresql).
			SetPrimaryKey("id", db.Varchar))

		info := t.GetInfo()
		info.AddField("ID", "id", db.Varchar).FieldSortable()
		info.AddField("Login", "login", db.Varchar).FieldFilterable()
		info.AddField("Name", "name", db.Varchar).FieldFilterable()
		info.AddField("Status", "status", db.Varchar).FieldFilterable()
		info.AddField("Type", "type", db.Varchar).FieldFilterable()
		info.AddField("Password", "password", db.Varchar).
			FieldDisplay(func(_ types.FieldModel) interface{} { return "••••••" })
		info.SetTable("admins").SetTitle("Admins").SetDescription("Manage system administrators")

		formList := t.GetForm()

		formList.AddField("ID", "id", db.Varchar, form.Text).
			FieldDisplayButCanNotEditWhenUpdate().
			FieldDisableWhenCreate()

		formList.AddField("Login", "login", db.Varchar, form.Text)
		formList.AddField("Name", "name", db.Varchar, form.Text)

		formList.AddField("Password", "password", db.Varchar, form.Password).
			FieldHelpMsg("При создании — обязательно. При редактировании — оставьте пустым, чтобы не менять.")

		formList.AddField("Status", "status", db.Varchar, form.SelectSingle).
			FieldOptions(types.FieldOptions{
				{Value: "active", Text: "Active"},
				{Value: "inactive", Text: "Inactive"},
				{Value: "blocked", Text: "Blocked"},
			}).FieldDefault("active")

		formList.AddField("Type", "type", db.Varchar, form.SelectSingle).
			FieldOptions(types.FieldOptions{
				{Value: "creator", Text: "Creator"},
				{Value: "operator", Text: "Operator"},
			}).FieldDefault("operator")

		formList.SetTable("admins").SetTitle("Admin").SetDescription("Create / edit administrator")
		return
	}
}
