package handlers

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/drivers"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/userstream"
)

func publishDriverUpdateToManager(stream *userstream.Hub, logger *zap.Logger, d *drivers.Driver, actorRole, actorID, action string, changed []string) {
	if stream == nil || d == nil || d.FreelancerID == nil {
		return
	}
	raw := strings.TrimSpace(*d.FreelancerID)
	if raw == "" {
		return
	}
	dispatcherID, err := uuid.Parse(raw)
	if err != nil || dispatcherID == uuid.Nil {
		if logger != nil {
			logger.Warn("skip driver update notify: invalid freelancer_id", zap.String("freelancer_id", raw), zap.Error(err))
		}
		return
	}
	stream.PublishDriverUpdate(tripnotif.RecipientDispatcher, dispatcherID, map[string]any{
		"kind":          "driver_update",
		"event":         "driver_update",
		"action":        action,
		"actor_role":    actorRole,
		"actor_id":      actorID,
		"driver_id":     d.ID,
		"changed_fields": changed,
		"driver":        groupedDriverProfile(d),
		"updated_at":    time.Now().UTC(),
	})
}
