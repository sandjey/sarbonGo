package goadmin

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/GoAdminGroup/go-admin/context"
	"github.com/GoAdminGroup/go-admin/modules/config"
	"github.com/GoAdminGroup/go-admin/template/types"
)

// DashboardContent returns the panel for the admin index page (GET /admin).
// Cards for all entities from API/Swagger: Companies, Admins, Drivers, Dispatchers, Cargo, Route Points, Payments, Offers, Company Users, App Roles, User Company Roles, Invitations, Audit Log, Chat.
func DashboardContent(ctx *context.Context) (types.Panel, error) {
	prefix := config.Prefix()
	var b strings.Builder
	b.WriteString(`<div class="row">`)
	b.WriteString(`<div class="col-md-6 col-lg-3"><a href="` + prefix + `/menu" class="admin-dash-card"><div class="admin-dash-card-icon"><i class="fa fa-list"></i></div><div class="admin-dash-card-title">Menu</div><div class="admin-dash-card-desc">Полное меню GoAdmin</div></a></div>`)
	b.WriteString(`<div class="col-md-6 col-lg-3"><a href="/docs" class="admin-dash-card"><div class="admin-dash-card-icon"><i class="fa fa-book"></i></div><div class="admin-dash-card-title">Swagger</div><div class="admin-dash-card-desc">OpenAPI документация</div></a></div>`)
	b.WriteString(`</div>`)

	// Render all table cards with direct links to /admin/info/{table}
	names := getAutoTableNames()
	b.WriteString(`<div class="row">`)
	for i, t := range names {
		title := strings.ReplaceAll(t, "_", " ")
		b.WriteString(fmt.Sprintf(
			`<div class="col-md-6 col-lg-3"><a href="%s/info/%s" class="admin-dash-card"><div class="admin-dash-card-icon"><i class="fa fa-table"></i></div><div class="admin-dash-card-title">%s</div><div class="admin-dash-card-desc">CRUD</div></a></div>`,
			prefix, t, title,
		))
		// wrap rows for cleaner layout
		if (i+1)%4 == 0 && i+1 < len(names) {
			b.WriteString(`</div><div class="row">`)
		}
	}
	b.WriteString(`</div>`)

	html := b.String()
	return types.Panel{
		Content:     template.HTML(html),
		Title:       "Главная",
		Description: "Управление данными (Auto CRUD по всем таблицам)",
	}, nil
}


