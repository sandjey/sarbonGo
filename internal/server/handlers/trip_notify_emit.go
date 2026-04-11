package handlers

import (
	"context"
	"strings"

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
	if err := h.notif.Insert(ctx, tripID, recipientKind, recipientID, eventKind, fromSt, toSt); err != nil {
		h.logger.Warn("trip notification insert", zap.Error(err), zap.String("event", eventKind))
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
	if h.notif == nil || before == nil || after == nil {
		return
	}
	cg, _ := h.cargoRepo.GetByID(ctx, after.CargoID, false)

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
}

func (h *TripsHandler) notifyTripCancelled(ctx context.Context, t *trips.Trip) {
	if h.notif == nil || t == nil {
		return
	}
	cg, _ := h.cargoRepo.GetByID(ctx, t.CargoID, false)
	h.notifPair(ctx, t, cg, tripnotif.EventCancelled, strPtr(t.Status), strPtr(trips.StatusCancelled))
}
