package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/adminanalytics"
	"sarbonNew/internal/server/resp"
)

type AdminAnalyticsHandler struct {
	logger *zap.Logger
	repo   *adminanalytics.Repo
}

func NewAdminAnalyticsHandler(logger *zap.Logger, repo *adminanalytics.Repo) *AdminAnalyticsHandler {
	return &AdminAnalyticsHandler{logger: logger, repo: repo}
}

func (h *AdminAnalyticsHandler) Dashboard(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	role := strings.TrimSpace(c.Query("role"))
	data, err := h.repo.Dashboard(c.Request.Context(), w, role)
	if err != nil {
		h.logger.Error("admin analytics dashboard", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, data))
}

func (h *AdminAnalyticsHandler) Metrics(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	page := parseAdminAnalyticsPage(c)
	metricNames := parseCSV(c.Query("metrics"))
	groupBy := strings.TrimSpace(c.DefaultQuery("group_by", "time"))
	interval := strings.TrimSpace(c.DefaultQuery("interval", "day"))
	role := strings.TrimSpace(c.Query("role"))
	userID := parseOptionalUUID(c, "user_id")
	if c.IsAborted() {
		return
	}
	items, err := h.repo.Metrics(c.Request.Context(), w, metricNames, groupBy, interval, role, userID, page)
	if err != nil {
		h.logger.Error("admin analytics metrics", zap.Error(err))
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{
		"group_by": groupBy,
		"interval": interval,
		"items":    items,
	}))
}

func (h *AdminAnalyticsHandler) ListUsers(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	page := parseAdminAnalyticsPage(c)
	role := strings.TrimSpace(c.Query("role"))
	search := strings.TrimSpace(c.Query("search"))
	items, total, err := h.repo.ListUsers(c.Request.Context(), w, role, search, page)
	if err != nil {
		h.logger.Error("admin analytics users list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{
		"items": items,
		"page": gin.H{
			"limit":    page.Limit,
			"offset":   page.Offset,
			"sort_by":  page.SortBy,
			"sort_dir": page.SortDir,
			"total":    total,
		},
	}))
}

func (h *AdminAnalyticsHandler) GetUser(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	userID, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil || userID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	data, err := h.repo.GetUserDetails(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("admin analytics user detail", zap.Error(err), zap.String("user_id", userID.String()))
		resp.ErrorLang(c, http.StatusNotFound, "user_not_found")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, data))
}

func (h *AdminAnalyticsHandler) ListUserLogins(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	userID, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil || userID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	page := parseAdminAnalyticsPage(c)
	items, total, err := h.repo.ListUserLogins(c.Request.Context(), userID, page)
	if err != nil {
		h.logger.Error("admin analytics user logins", zap.Error(err), zap.String("user_id", userID.String()))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{
		"items": items,
		"page": gin.H{
			"limit":  page.Limit,
			"offset": page.Offset,
			"total":  total,
		},
	}))
}

func (h *AdminAnalyticsHandler) Funnels(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	data, err := h.repo.Funnels(c.Request.Context(), w, strings.TrimSpace(c.Query("role")))
	if err != nil {
		h.logger.Error("admin analytics funnels", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, data))
}

func (h *AdminAnalyticsHandler) Dropoff(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	data, err := h.repo.Dropoff(c.Request.Context(), w, strings.TrimSpace(c.Query("role")))
	if err != nil {
		h.logger.Error("admin analytics dropoff", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, data))
}

func (h *AdminAnalyticsHandler) Retention(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	items, err := h.repo.Retention(c.Request.Context(), w, strings.TrimSpace(c.Query("role")))
	if err != nil {
		h.logger.Error("admin analytics retention", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{"items": items}))
}

func (h *AdminAnalyticsHandler) FlowTime(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	items, err := h.repo.FlowTime(c.Request.Context(), strings.TrimSpace(c.Query("role")))
	if err != nil {
		h.logger.Error("admin analytics flow time", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{"items": items}))
}

func (h *AdminAnalyticsHandler) FlowConversion(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	data, err := h.repo.FlowConversion(c.Request.Context(), w, strings.TrimSpace(c.Query("role")))
	if err != nil {
		h.logger.Error("admin analytics flow conversion", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, data))
}

func (h *AdminAnalyticsHandler) ListChats(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	page := parseAdminAnalyticsPage(c)
	userID := parseOptionalUUID(c, "user_id")
	if c.IsAborted() {
		return
	}
	items, total, err := h.repo.ListChats(c.Request.Context(), userID, strings.TrimSpace(c.Query("search")), page)
	if err != nil {
		h.logger.Error("admin analytics chats list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{
		"items": items,
		"page": gin.H{
			"limit":  page.Limit,
			"offset": page.Offset,
			"total":  total,
		},
	}))
}

func (h *AdminAnalyticsHandler) ListChatMessages(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	chatID, err := uuid.Parse(strings.TrimSpace(c.Param("chat_id")))
	if err != nil || chatID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	page := parseAdminAnalyticsPage(c)
	items, total, err := h.repo.ListChatMessages(c.Request.Context(), chatID, page)
	if err != nil {
		h.logger.Error("admin analytics chat messages", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{
		"items": items,
		"page": gin.H{
			"limit":  page.Limit,
			"offset": page.Offset,
			"total":  total,
		},
	}))
}

func (h *AdminAnalyticsHandler) ListCalls(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	page := parseAdminAnalyticsPage(c)
	userID := parseOptionalUUID(c, "user_id")
	if c.IsAborted() {
		return
	}
	items, total, err := h.repo.ListCalls(c.Request.Context(), c.Query("status"), userID, page)
	if err != nil {
		h.logger.Error("admin analytics calls list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{
		"items": items,
		"page": gin.H{
			"limit":  page.Limit,
			"offset": page.Offset,
			"total":  total,
		},
	}))
}

func (h *AdminAnalyticsHandler) GetCall(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	callID, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil || callID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	data, err := h.repo.GetCall(c.Request.Context(), callID)
	if err != nil {
		h.logger.Error("admin analytics call detail", zap.Error(err), zap.String("call_id", callID.String()))
		resp.ErrorLang(c, http.StatusNotFound, "call_not_found")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, data))
}

func (h *AdminAnalyticsHandler) Geo(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	page := parseAdminAnalyticsPage(c)
	items, err := h.repo.Geo(c.Request.Context(), w, strings.TrimSpace(c.Query("role")), page)
	if err != nil {
		h.logger.Error("admin analytics geo", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{"items": items}))
}

func (h *AdminAnalyticsHandler) GeoRealtime(c *gin.Context) {
	w, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		return
	}
	items, err := h.repo.GeoRealtime(c.Request.Context())
	if err != nil {
		h.logger.Error("admin analytics geo realtime", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", withAdminAnalyticsMeta(w, gin.H{"items": items}))
}

func parseAdminAnalyticsWindow(c *gin.Context) (adminanalytics.TimeWindow, bool) {
	tz := strings.TrimSpace(c.DefaultQuery("tz", "UTC"))
	loc, err := time.LoadLocation(tz)
	if err != nil {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "tz"})
		return adminanalytics.TimeWindow{}, false
	}
	now := time.Now().In(loc)
	from := parseAdminTimeValue(c.Query("from"), loc, now.AddDate(0, 0, -30))
	to := parseAdminTimeValue(c.Query("to"), loc, now)
	if !from.Before(to) {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "from/to"})
		return adminanalytics.TimeWindow{}, false
	}
	return adminanalytics.TimeWindow{From: from.UTC(), To: to.UTC(), TZ: tz}, true
}

func parseAdminTimeValue(raw string, loc *time.Location, fallback time.Time) time.Time {
	v := strings.TrimSpace(raw)
	if v == "" {
		return fallback
	}
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if ts, err := time.ParseInLocation(layout, v, loc); err == nil {
			if layout == "2006-01-02" {
				return ts
			}
			return ts
		}
	}
	return fallback
}

func parseAdminAnalyticsPage(c *gin.Context) adminanalytics.Page {
	limit := getIntQuery(c, "limit", 20)
	offset := getIntQuery(c, "offset", 0)
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return adminanalytics.Page{
		Limit:   limit,
		Offset:  offset,
		SortBy:  strings.TrimSpace(c.DefaultQuery("sort_by", "registered_at")),
		SortDir: strings.TrimSpace(c.DefaultQuery("sort_dir", "desc")),
	}
}

func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func parseOptionalUUID(c *gin.Context, key string) *uuid.UUID {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": key})
		c.Abort()
		return nil
	}
	return &id
}

func withAdminAnalyticsMeta(w adminanalytics.TimeWindow, data any) gin.H {
	return gin.H{
		"time_window": gin.H{
			"from": w.From.UTC().Format(time.RFC3339),
			"to":   w.To.UTC().Format(time.RFC3339),
			"tz":   w.TZ,
		},
		"generated_at_utc": time.Now().UTC().Format(time.RFC3339),
		"data":             data,
	}
}
