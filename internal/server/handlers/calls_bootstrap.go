package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

// GetCallTestBootstrap returns a ready-to-use checklist and templates
// for manual voice-call testing from two devices.
// GET /v1/calls/test/bootstrap
func (h *CallsHandler) GetCallTestBootstrap(c *gin.Context) {
	v, ok := c.Get(mw.CtxUserID)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	userID, _ := v.(uuid.UUID)
	if userID == uuid.Nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}

	resp.OKLang(c, "ok", gin.H{
		"tooling": gin.H{
			"swagger_docs_url": "/docs?group=chat",
			"ws_test_url":      "/ws-test",
			"calls_test_url":   "/calls-test",
		},
		"current_user_id": userID,
		"required_headers": []string{
			"X-Device-Type",
			"X-Language",
			"X-Client-Token",
			"X-User-Token",
		},
		"rest_flow": []string{
			"POST /v1/calls (caller)",
			"POST /v1/calls/{id}/accept (callee)",
			"POST /v1/calls/{id}/end (caller or callee)",
			"POST /v1/calls/{id}/decline (callee, optional scenario)",
			"POST /v1/calls/{id}/cancel (caller, optional scenario)",
		},
		"ws_templates": gin.H{
			"offer":  `{"type":"webrtc.offer","data":{"call_id":"CALL_ID","payload":{"sdp":"..."}}}`,
			"answer": `{"type":"webrtc.answer","data":{"call_id":"CALL_ID","payload":{"sdp":"..."}}}`,
			"ice":    `{"type":"webrtc.ice","data":{"call_id":"CALL_ID","payload":{"candidate":"..."}}}`,
			"end":    `{"type":"call.end","data":{"call_id":"CALL_ID","payload":{"reason":"manual"}}}`,
		},
		"notes": []string{
			"Server handles call state + signaling relay only; media path is WebRTC peer-to-peer.",
			"For production quality calls, configure TURN alongside STUN on clients.",
			"If peer is offline, call.invite websocket event is best-effort and may be missed without push notifications.",
		},
	})
}

