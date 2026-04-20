package mw

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/server/resp"
)

// CtxDispatcherManagerRole holds the resolved freelance_dispatchers.manager_role
// (CARGO_MANAGER | DRIVER_MANAGER) of the authenticated dispatcher. Set by RequireDispatcherManagerRole.
const CtxDispatcherManagerRole = "dispatcher_manager_role"

// RequireDispatcherManagerRole ensures the current authenticated dispatcher (must be set by
// RequireDispatcher / RequireDispatcherWithQueryToken earlier in the chain) has the expected
// manager_role. Returns 403 "forbidden_role" otherwise. Intended for endpoints that are
// semantically split between CARGO_MANAGER and DRIVER_MANAGER URL spaces.
func RequireDispatcherManagerRole(repo *dispatchers.Repo, expected string) gin.HandlerFunc {
	want := strings.ToUpper(strings.TrimSpace(expected))
	return func(c *gin.Context) {
		raw, ok := c.Get(CtxDispatcherID)
		if !ok {
			resp.ErrorLang(c, 401, "missing_user_token")
			c.Abort()
			return
		}
		dispID, ok := raw.(uuid.UUID)
		if !ok || dispID == uuid.Nil {
			resp.ErrorLang(c, 401, "invalid_user_token")
			c.Abort()
			return
		}
		if repo == nil {
			resp.ErrorLang(c, 500, "internal_error")
			c.Abort()
			return
		}
		d, err := repo.FindByID(c.Request.Context(), dispID)
		if err != nil || d == nil {
			resp.ErrorLang(c, 401, "invalid_user_token")
			c.Abort()
			return
		}
		got := ""
		if d.ManagerRole != nil {
			got = strings.ToUpper(strings.TrimSpace(*d.ManagerRole))
		}
		if got == "" || got != want {
			resp.ErrorLang(c, 403, "forbidden_role")
			c.Abort()
			return
		}
		c.Set(CtxDispatcherManagerRole, got)
		c.Next()
	}
}
