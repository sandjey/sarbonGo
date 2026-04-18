package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/userstream"
)

// ensureSSEID adds a stable dedup id for clients (SSE `cargo_offer` streams / EventSource dedup).
func ensureSSEID(payload gin.H) {
	if payload == nil {
		return
	}
	if _, ok := payload["sse_id"]; !ok {
		payload["sse_id"] = uuid.New().String()
	}
}

// cargoOfferDispatcherRecipients returns freelance dispatcher IDs that should receive cargo_offer SSE events.
// Includes: cargo author (DISPATCHER-created), negotiation driver manager, and author of DISPATCHER-side offers
// (covers company-owned cargo where tripNotifyDispatcherID is empty).
func cargoOfferDispatcherRecipients(cargoObj *cargo.Cargo, offer *cargo.Offer) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{})
	var out []uuid.UUID
	add := func(id uuid.UUID) {
		if id == uuid.Nil {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if cargoObj != nil {
		if d := tripNotifyDispatcherID(cargoObj); d != nil {
			add(*d)
		}
	}
	if offer != nil {
		if dm := offerDriverManagerDispatcherID(offer); dm != nil {
			add(*dm)
		}
		if offer.ProposedByID != nil && strings.EqualFold(strings.TrimSpace(offer.ProposedBy), cargo.OfferProposedByDispatcher) {
			add(*offer.ProposedByID)
		}
	}
	return out
}

func publishCargoOfferToDispatchers(stream *userstream.Hub, cargoObj *cargo.Cargo, offer *cargo.Offer, payload gin.H) {
	if stream == nil || payload == nil {
		return
	}
	ids := cargoOfferDispatcherRecipients(cargoObj, offer)
	if len(ids) == 0 {
		return
	}
	p := make(gin.H, len(payload)+1)
	for k, v := range payload {
		p[k] = v
	}
	ensureSSEID(p)
	for _, id := range ids {
		stream.PublishNotification(tripnotif.RecipientDispatcher, id, p)
	}
}
