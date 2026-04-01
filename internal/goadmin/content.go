package goadmin

import (
	"html/template"

	"github.com/GoAdminGroup/go-admin/context"
	"github.com/GoAdminGroup/go-admin/modules/config"
	"github.com/GoAdminGroup/go-admin/template/types"
)

// DashboardContent returns the panel for the admin index page (GET /admin).
// Cards for all entities from API/Swagger: Companies, Admins, Drivers, Dispatchers, Cargo, Route Points, Payments, Offers, Company Users, App Roles, User Company Roles, Invitations, Audit Log, Chat.
func DashboardContent(ctx *context.Context) (types.Panel, error) {
	prefix := config.Prefix()
	html := `
		<div class="row">
			<div class="col-md-6 col-lg-4">
				<a href="` + prefix + `/info" class="admin-dash-card">
					<div class="admin-dash-card-icon"><i class="fa fa-database"></i></div>
					<div class="admin-dash-card-title">All tables</div>
					<div class="admin-dash-card-desc">Автоматический каталог всех таблиц (CRUD)</div>
				</a>
			</div>
			<div class="col-md-6 col-lg-4">
				<a href="` + prefix + `/menu" class="admin-dash-card">
					<div class="admin-dash-card-icon"><i class="fa fa-list"></i></div>
					<div class="admin-dash-card-title">Menu</div>
					<div class="admin-dash-card-desc">Полное меню GoAdmin</div>
				</a>
			</div>
			<div class="col-md-6 col-lg-4">
				<a href="/docs" class="admin-dash-card">
					<div class="admin-dash-card-icon"><i class="fa fa-book"></i></div>
					<div class="admin-dash-card-title">Swagger</div>
					<div class="admin-dash-card-desc">OpenAPI документация</div>
				</a>
			</div>
		</div>
	`
	return types.Panel{
		Content:     template.HTML(html),
		Title:       "Главная",
		Description: "Управление данными (Auto CRUD по всем таблицам)",
	}, nil
}


