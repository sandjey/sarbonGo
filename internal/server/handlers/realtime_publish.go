package handlers

import (
	"time"

	"github.com/google/uuid"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/trips"
	"sarbonNew/internal/userstream"
)

// PublishTripStatusForCargoParticipants pushes the same trip snapshot to the driver on the trip and to the freelance dispatcher who owns the cargo (when applicable — same rule as trip notifications).
func PublishTripStatusForCargoParticipants(hub *userstream.Hub, trip *trips.Trip, cg *cargo.Cargo) {
	if hub == nil || trip == nil {
		return
	}
	payload := buildTripStatusSSEPayload(trip)
	if trip.DriverID != nil && *trip.DriverID != uuid.Nil {
		hub.PublishTripStatus(tripnotif.RecipientDriver, *trip.DriverID, payload)
	}
	if disp := tripNotifyDispatcherID(cg); disp != nil && *disp != uuid.Nil {
		hub.PublishTripStatus(tripnotif.RecipientDispatcher, *disp, payload)
	}
}

func buildTripStatusSSEPayload(trip *trips.Trip) map[string]any {
	out := map[string]any{
		"kind":            "trip_status",
		"trip_id":         trip.ID.String(),
		"cargo_id":        trip.CargoID.String(),
		"offer_id":        trip.OfferID.String(),
		"status":          trip.Status,
		"agreed_price":    trip.AgreedPrice,
		"agreed_currency": trip.AgreedCurrency,
		"updated_at":      trip.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"created_at":      trip.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if trip.DriverID != nil {
		out["driver_id"] = trip.DriverID.String()
	}
	if trip.PendingConfirmTo != nil {
		out["pending_confirm_to"] = *trip.PendingConfirmTo
	}
	if trips.CompletionPending(trip) {
		out["completion_awaiting_dispatcher_confirm"] = true
	}
	return out
}
