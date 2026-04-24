package adminanalytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Tracker struct {
	repo   *Repo
	logger *zap.Logger
	salt   string
}

func NewTracker(repo *Repo, logger *zap.Logger, salt string) *Tracker {
	return &Tracker{repo: repo, logger: logger, salt: salt}
}

func (t *Tracker) SafeTrack(c *gin.Context, in EventInput) {
	if t == nil || t.repo == nil {
		return
	}
	if c != nil {
		enrichEventFromRequest(c, t.salt, &in)
	}
	if err := t.repo.TrackEvent(context.Background(), in); err != nil && t.logger != nil {
		t.logger.Warn("analytics track event failed", zap.String("event_name", in.EventName), zap.Error(err))
	}
}

func (t *Tracker) SafeTrackWithContext(ctx context.Context, in EventInput) {
	if t == nil || t.repo == nil {
		return
	}
	if err := t.repo.TrackEvent(ctx, in); err != nil && t.logger != nil {
		t.logger.Warn("analytics track event failed", zap.String("event_name", in.EventName), zap.Error(err))
	}
}

func (t *Tracker) SafeEndSession(ctx context.Context, sessionID string, userID *uuid.UUID, role string, metadata map[string]any) {
	if t == nil || t.repo == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	in := EventInput{
		EventName: EventSessionEnded,
		EventTime: time.Now().UTC(),
		UserID:    userID,
		Role:      NormalizeRole(role),
		SessionID: strings.TrimSpace(sessionID),
		Metadata:  metadata,
	}
	if err := t.repo.TrackEvent(ctx, in); err != nil && t.logger != nil {
		t.logger.Warn("analytics end session failed", zap.String("session_id", sessionID), zap.Error(err))
	}
}

func HashIP(ip, salt string) string {
	raw := strings.TrimSpace(ip)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s", salt, raw)))
	return hex.EncodeToString(sum[:])
}

func enrichEventFromRequest(c *gin.Context, salt string, in *EventInput) {
	if c == nil || in == nil {
		return
	}
	if in.EventTime.IsZero() {
		in.EventTime = time.Now().UTC()
	}
	if strings.TrimSpace(in.DeviceType) == "" {
		in.DeviceType = strings.ToLower(strings.TrimSpace(c.GetHeader("X-Device-Type")))
	}
	if strings.TrimSpace(in.Platform) == "" {
		in.Platform = strings.TrimSpace(c.GetHeader("X-Platform"))
		if in.Platform == "" && c.Request != nil {
			in.Platform = strings.TrimSpace(c.Request.UserAgent())
		}
	}
	if strings.TrimSpace(in.IPHash) == "" {
		in.IPHash = HashIP(c.ClientIP(), salt)
	}
	if strings.TrimSpace(in.GeoCity) == "" {
		in.GeoCity = strings.TrimSpace(c.GetHeader("X-Geo-City"))
	}
	if in.Metadata == nil {
		in.Metadata = map[string]any{}
	}
	if c.Request != nil && c.Request.URL != nil {
		if _, ok := in.Metadata["path"]; !ok {
			in.Metadata["path"] = c.Request.URL.Path
		}
	}
	if _, ok := in.Metadata["method"]; !ok {
		in.Metadata["method"] = c.Request.Method
	}
}
