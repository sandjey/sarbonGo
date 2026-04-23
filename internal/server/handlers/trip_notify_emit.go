package handlers

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/trips"
)

func tripNotifyDispatcherID(c *cargo.Cargo) *uuid.UUID {
	if c == nil || c.CreatedByType == nil || !strings.EqualFold(strings.TrimSpace(*c.CreatedByType), "DISPATCHER") || c.CreatedByID == nil {
		return nil
	}
	return c.CreatedByID
}

func (h *TripsHandler) notifInsert(ctx context.Context, tripID uuid.UUID, recipientKind string, recipientID uuid.UUID, eventKind string, fromSt, toSt *string, actorID uuid.UUID) {
	h.notifInsertExtra(ctx, tripID, recipientKind, recipientID, eventKind, fromSt, toSt, nil, actorID)
}

func (h *TripsHandler) notifInsertExtra(ctx context.Context, tripID uuid.UUID, recipientKind string, recipientID uuid.UUID, eventKind string, fromSt, toSt *string, extra map[string]any, actorID uuid.UUID) {
	if h.notif == nil || recipientID == uuid.Nil {
		return
	}
	if actorID != uuid.Nil && recipientID == actorID {
		return
	}
	nid, err := h.notif.Insert(ctx, tripID, recipientKind, recipientID, eventKind, fromSt, toSt)
	if err != nil {
		h.logger.Warn("trip notification insert", zap.Error(err), zap.String("event", eventKind))
		return
	}
	if h.stream != nil {
		msg := map[string]any{
			"kind":            "trip_notification",
			"notification_id": nid.String(),
			"trip_id":         tripID.String(),
			"event_kind":      eventKind,
			"created_at":      time.Now().UTC().Format(time.RFC3339Nano),
		}
		for k, v := range extra {
			msg[k] = v
		}
		if actorID != uuid.Nil {
			msg["actor_id"] = actorID.String()
		}
		if fromSt != nil {
			msg["from_status"] = *fromSt
		}
		if toSt != nil {
			msg["to_status"] = *toSt
		}
		h.stream.PublishNotification(recipientKind, recipientID, msg)
	}
}

func (h *TripsHandler) notifPair(ctx context.Context, trip *trips.Trip, cg *cargo.Cargo, eventKind string, fromSt, toSt *string, actorID uuid.UUID) {
	if trip == nil || trip.DriverID == nil {
		return
	}
	h.notifInsert(ctx, trip.ID, tripnotif.RecipientDriver, *trip.DriverID, eventKind, fromSt, toSt, actorID)
	if disp := tripNotifyDispatcherID(cg); disp != nil {
		h.notifInsert(ctx, trip.ID, tripnotif.RecipientDispatcher, *disp, eventKind, fromSt, toSt, actorID)
	}
}

func (h *TripsHandler) notifyTripTransition(ctx context.Context, before, after *trips.Trip, actorID uuid.UUID) {
	if before == nil || after == nil {
		return
	}
	cg, _ := h.cargoRepo.GetByID(ctx, after.CargoID, false)
	offer, _ := h.cargoRepo.GetOfferByID(ctx, after.OfferID)

	if before.Status != after.Status {
		switch after.Status {
		case trips.StatusInTransit:
			h.notifPair(ctx, after, cg, tripnotif.EventInTransit, strPtr(before.Status), strPtr(after.Status), actorID)
		case trips.StatusDelivered:
			h.notifPair(ctx, after, cg, tripnotif.EventDelivered, strPtr(before.Status), strPtr(after.Status), actorID)
		case trips.StatusCompleted:
			h.notifPair(ctx, after, cg, tripnotif.EventCompleted, strPtr(before.Status), strPtr(after.Status), actorID)
			// If this trip was negotiated via a Driver Manager, notify that manager too (DM sees only completion, then can rate driver + cargo manager).
			if offer != nil && offer.NegotiationDispatcherID != nil && *offer.NegotiationDispatcherID != uuid.Nil {
				dispToSkip := uuid.Nil
				if disp := tripNotifyDispatcherID(cg); disp != nil {
					dispToSkip = *disp
				}
				if *offer.NegotiationDispatcherID != dispToSkip {
					extra := map[string]any{}
					if after.DriverID != nil {
						extra["rate_driver_id"] = after.DriverID.String()
					}
					if disp := tripNotifyDispatcherID(cg); disp != nil {
						extra["rate_cargo_manager_id"] = disp.String()
					}
					h.notifInsertExtra(ctx, after.ID, tripnotif.RecipientDispatcher, *offer.NegotiationDispatcherID, tripnotif.EventCompleted, strPtr(before.Status), strPtr(after.Status), extra, actorID)
				}
			}
		}
	}

	bPend := trips.CompletionPending(before)
	aPend := trips.CompletionPending(after)
	if !bPend && aPend {
		if disp := tripNotifyDispatcherID(cg); disp != nil {
			h.notifInsert(ctx, after.ID, tripnotif.RecipientDispatcher, *disp, tripnotif.EventCompletionPending, strPtr(before.Status), strPtr("COMPLETION_PENDING"), actorID)
		}
	}
	if before.Status != after.Status || (!bPend && aPend) {
		PublishTripStatusForCargoParticipants(h.stream, after, cg, offer, actorID)
	}
}

func (h *TripsHandler) notifyTripCancelled(ctx context.Context, t *trips.Trip, actorID uuid.UUID) {
	if t == nil {
		return
	}
	cg, _ := h.cargoRepo.GetByID(ctx, t.CargoID, false)
	offer, _ := h.cargoRepo.GetOfferByID(ctx, t.OfferID)
	h.notifPair(ctx, t, cg, tripnotif.EventCancelled, strPtr(t.Status), strPtr(trips.StatusCancelled), actorID)
	if h.stream != nil {
		snap := *t
		snap.Status = trips.StatusCancelled
		PublishTripStatusForCargoParticipants(h.stream, &snap, cg, offer, actorID)
	}
}
