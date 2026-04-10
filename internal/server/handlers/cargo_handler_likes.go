package handlers

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/security"
)

// cargoAPIViewer identifies who is requesting cargo payloads (for is_liked).
type cargoAPIViewer struct {
	DriverID      *uuid.UUID
	DispatcherID *uuid.UUID
}

func parseCargoAPIViewer(c *gin.Context, jwtm *security.JWTManager) *cargoAPIViewer {
	if jwtm == nil {
		return nil
	}
	raw := strings.TrimSpace(c.GetHeader(mw.HeaderUserToken))
	if raw == "" {
		return nil
	}
	userID, role, _, _, err := jwtm.ParseAccessWithSID(raw)
	if err != nil || userID == uuid.Nil {
		return nil
	}
	switch role {
	case "driver":
		return &cargoAPIViewer{DriverID: &userID}
	case "dispatcher":
		return &cargoAPIViewer{DispatcherID: &userID}
	default:
		return nil
	}
}

func cargoViewerFromGin(c *gin.Context) *cargoAPIViewer {
	if id, ok := c.Get(mw.CtxDriverID); ok {
		did := id.(uuid.UUID)
		return &cargoAPIViewer{DriverID: &did}
	}
	if id, ok := c.Get(mw.CtxDispatcherID); ok {
		did := id.(uuid.UUID)
		return &cargoAPIViewer{DispatcherID: &did}
	}
	return nil
}

func (h *CargoHandler) cargoLikedFlags(ctx context.Context, viewer *cargoAPIViewer, ids []uuid.UUID) map[uuid.UUID]bool {
	if h.fav == nil || viewer == nil {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	uniq := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return map[uuid.UUID]bool{}
	}
	var m map[uuid.UUID]bool
	var err error
	if viewer.DriverID != nil {
		m, err = h.fav.DriverLikedCargoIDs(ctx, *viewer.DriverID, uniq)
	} else if viewer.DispatcherID != nil {
		m, err = h.fav.DispatcherLikedCargoIDs(ctx, *viewer.DispatcherID, uniq)
	} else {
		return nil
	}
	if err != nil {
		h.logger.Warn("cargo is_liked flags", zap.Error(err))
		return map[uuid.UUID]bool{}
	}
	return m
}

func applyIsLikedToDetail(detail gin.H, flags map[uuid.UUID]bool, cargoID uuid.UUID) {
	if flags == nil || detail == nil {
		return
	}
	detail["is_liked"] = flags[cargoID]
}

func (h *CargoHandler) applyIsLikedToOfferListItems(ctx context.Context, items []gin.H, viewer *cargoAPIViewer) {
	if h.fav == nil || viewer == nil || len(items) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		cidStr, _ := it["cargo_id"].(string)
		cid, err := uuid.Parse(cidStr)
		if err != nil || cid == uuid.Nil {
			continue
		}
		ids = append(ids, cid)
	}
	flags := h.cargoLikedFlags(ctx, viewer, ids)
	if flags == nil {
		return
	}
	for i := range items {
		cg, ok := items[i]["cargo"].(gin.H)
		if !ok {
			continue
		}
		cidStr, _ := items[i]["cargo_id"].(string)
		cid, _ := uuid.Parse(cidStr)
		cg["is_liked"] = flags[cid]
		items[i]["cargo"] = cg
	}
}

func (h *CargoHandler) applyIsLikedToCargoMap(ctx context.Context, m gin.H, cargoID uuid.UUID, viewer *cargoAPIViewer) {
	if m == nil {
		return
	}
	flags := h.cargoLikedFlags(ctx, viewer, []uuid.UUID{cargoID})
	if flags == nil {
		return
	}
	m["is_liked"] = flags[cargoID]
}
