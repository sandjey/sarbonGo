package mw

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"sarbonNew/internal/admins"
	"sarbonNew/internal/server/resp"
)

const CtxAdminType = "admin_type"

func RequireAdminCreator(repo *admins.Repo) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawAdminID, ok := c.Get(CtxAdminID)
		if !ok {
			resp.ErrorLang(c, http.StatusUnauthorized, "unauthorized")
			c.Abort()
			return
		}
		adminID, ok := rawAdminID.(uuid.UUID)
		if !ok || adminID == uuid.Nil {
			resp.ErrorLang(c, http.StatusUnauthorized, "unauthorized")
			c.Abort()
			return
		}
		if repo == nil {
			resp.ErrorLang(c, http.StatusForbidden, "forbidden")
			c.Abort()
			return
		}
		admin, err := repo.FindByID(c.Request.Context(), adminID)
		if err != nil || admin == nil {
			resp.ErrorLang(c, http.StatusForbidden, "forbidden")
			c.Abort()
			return
		}
		if !strings.EqualFold(strings.TrimSpace(admin.Type), "creator") {
			resp.ErrorLang(c, http.StatusForbidden, "forbidden")
			c.Abort()
			return
		}
		c.Set(CtxAdminType, admin.Type)
		c.Next()
	}
}
