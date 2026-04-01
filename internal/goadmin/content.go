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
		icon, desc := tableVisuals(t)
		b.WriteString(fmt.Sprintf(
			`<div class="col-md-6 col-lg-3"><a href="%s/info/%s" class="admin-dash-card"><div class="admin-dash-card-icon"><i class="fa %s"></i></div><div class="admin-dash-card-title">%s</div><div class="admin-dash-card-desc">%s</div></a></div>`,
			prefix, t, icon, title, desc,
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

func tableVisuals(tableName string) (icon string, desc string) {
	n := strings.ToLower(strings.TrimSpace(tableName))
	switch {
	case strings.Contains(n, "driver"):
		return "fa-truck", "Drivers / transport"
	case strings.Contains(n, "dispatcher"):
		return "fa-users", "Dispatchers"
	case strings.Contains(n, "cargo"), strings.Contains(n, "route"):
		return "fa-cube", "Cargo / routes"
	case strings.Contains(n, "trip"):
		return "fa-road", "Trips"
	case strings.Contains(n, "payment"):
		return "fa-money", "Payments"
	case strings.Contains(n, "offer"):
		return "fa-handshake-o", "Offers"
	case strings.Contains(n, "company"):
		return "fa-building", "Company data"
	case strings.Contains(n, "role"):
		return "fa-user-circle", "Roles / permissions"
	case strings.Contains(n, "chat"), strings.Contains(n, "message"), strings.Contains(n, "conversation"):
		return "fa-comments", "Chat"
	case strings.Contains(n, "call"):
		return "fa-phone", "Calls"
	case strings.Contains(n, "audit"), strings.Contains(n, "log"):
		return "fa-history", "Audit / logs"
	case strings.Contains(n, "invitation"), strings.Contains(n, "invite"):
		return "fa-envelope", "Invitations"
	case strings.Contains(n, "admin"):
		return "fa-user-secret", "Administration"
	case strings.Contains(n, "city"), strings.Contains(n, "reference"), strings.Contains(n, "type"):
		return "fa-map-marker", "Reference data"
	default:
		return "fa-table", "CRUD"
	}
}


