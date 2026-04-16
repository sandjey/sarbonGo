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

func (h *TripsHandler) notifInsert(ctx context.Context, tripID uuid.UUID, recipientKind string, recipientID uuid.UUID, eventKind string, fromSt, toSt *string) {
	if h.notif == nil || recipientID == uuid.Nil {
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
		if fromSt != nil {
			msg["from_status"] = *fromSt
		}
		if toSt != nil {
			msg["to_status"] = *toSt
		}
		h.stream.PublishNotification(recipientKind, recipientID, msg)
	}
}

func (h *TripsHandler) notifPair(ctx context.Context, trip *trips.Trip, cg *cargo.Cargo, eventKind string, fromSt, toSt *string) {
	if trip == nil || trip.DriverID == nil {
		return
	}
	h.notifInsert(ctx, trip.ID, tripnotif.RecipientDriver, *trip.DriverID, eventKind, fromSt, toSt)
	if disp := tripNotifyDispatcherID(cg); disp != nil {
		h.notifInsert(ctx, trip.ID, tripnotif.RecipientDispatcher, *disp, eventKind, fromSt, toSt)
	}
}

func (h *TripsHandler) notifyTripTransition(ctx context.Context, before, after *trips.Trip) {
	if before == nil || after == nil {
		return
	}
	cg, _ := h.cargoRepo.GetByID(ctx, after.CargoID, false)
	offer, _ := h.cargoRepo.GetOfferByID(ctx, after.OfferID)

	if before.Status != after.Status {
		switch after.Status {
		case trips.StatusInTransit:
			h.notifPair(ctx, after, cg, tripnotif.EventInTransit, strPtr(before.Status), strPtr(after.Status))
		case trips.StatusDelivered:
			h.notifPair(ctx, after, cg, tripnotif.EventDelivered, strPtr(before.Status), strPtr(after.Status))
		case trips.StatusCompleted:
			h.notifPair(ctx, after, cg, tripnotif.EventCompleted, strPtr(before.Status), strPtr(after.Status))
		}
	}

	bPend := trips.CompletionPending(before)
	aPend := trips.CompletionPending(after)
	if !bPend && aPend {
		if disp := tripNotifyDispatcherID(cg); disp != nil {
			h.notifInsert(ctx, after.ID, tripnotif.RecipientDispatcher, *disp, tripnotif.EventCompletionPending, strPtr(before.Status), strPtr("COMPLETION_PENDING"))
		}
	}
	if before.Status != after.Status || (!bPend && aPend) {
		PublishTripStatusForCargoParticipants(h.stream, after, cg, offer)
	}
}

func (h *TripsHandler) notifyTripCancelled(ctx context.Context, t *trips.Trip) {
	if t == nil {
		return
	}
	cg, _ := h.cargoRepo.GetByID(ctx, t.CargoID, false)
	offer, _ := h.cargoRepo.GetOfferByID(ctx, t.OfferID)
	h.notifPair(ctx, t, cg, tripnotif.EventCancelled, strPtr(t.Status), strPtr(trips.StatusCancelled))
	if h.stream != nil {
		snap := *t
		snap.Status = trips.StatusCancelled
		PublishTripStatusForCargoParticipants(h.stream, &snap, cg, offer)
	}
}
