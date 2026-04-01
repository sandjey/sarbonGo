package goadmin

import (
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SanitizeAdminFormMiddleware prevents common GoAdmin->Postgres errors like:
// - invalid input syntax for type uuid: "" (empty string)
//
// Strategy: for /admin requests with form body, drop empty values for *_id fields (and "id")
// so Postgres can apply NULL/default and updates won't attempt to set uuid="".
func SanitizeAdminFormMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/admin") {
			c.Next()
			return
		}
		if c.Request.Method != http.MethodPost && c.Request.Method != http.MethodPut && c.Request.Method != http.MethodPatch {
			c.Next()
			return
		}

		ct := strings.TrimSpace(c.GetHeader("Content-Type"))
		mt, _, _ := mime.ParseMediaType(ct)
		if mt != "application/x-www-form-urlencoded" && mt != "multipart/form-data" {
			c.Next()
			return
		}

		// Parse form
		_ = c.Request.ParseMultipartForm(32 << 20)
		_ = c.Request.ParseForm()

		if c.Request.PostForm != nil {
			for k, vs := range c.Request.PostForm {
				if len(vs) == 0 {
					continue
				}
				if len(vs) == 1 && strings.TrimSpace(vs[0]) == "" && looksLikeIDField(k) {
					delete(c.Request.PostForm, k)
				}
			}
		}
		if c.Request.MultipartForm != nil && c.Request.MultipartForm.Value != nil {
			for k, vs := range c.Request.MultipartForm.Value {
				if len(vs) == 0 {
					continue
				}
				if len(vs) == 1 && strings.TrimSpace(vs[0]) == "" && looksLikeIDField(k) {
					delete(c.Request.MultipartForm.Value, k)
				}
			}
		}

		c.Next()
	}
}

func looksLikeIDField(k string) bool {
	k = strings.ToLower(strings.TrimSpace(k))
	if k == "id" {
		return true
	}
	return strings.HasSuffix(k, "_id")
}

