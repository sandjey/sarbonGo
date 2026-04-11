package trips

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Trip operational statuses (linear flow). Legacy statuses are migrated in DB.
const (
	StatusInProgress = "IN_PROGRESS"
	StatusInTransit  = "IN_TRANSIT"
	StatusDelivered  = "DELIVERED"
	StatusCompleted  = "COMPLETED"
	StatusCancelled  = "CANCELLED"
)

type Trip struct {
	ID        uuid.UUID
	CargoID   uuid.UUID
	OfferID   uuid.UUID
	DriverID  *uuid.UUID
	Status    string
	AgreedPrice    float64
	AgreedCurrency string
	CreatedAt time.Time
	UpdatedAt time.Time
	// PendingConfirmTo + DriverConfirmedAt: при DELIVERED означают «водитель запросил COMPLETED, ждём cargo manager».
	PendingConfirmTo      *string
	DriverConfirmedAt     *time.Time
	DispatcherConfirmedAt *time.Time
	// RatingFromDriver — звёзды, которые водитель поставил cargo manager по этому рейсу (зеркало trip_ratings).
	RatingFromDriver *float64
	// RatingFromDispatcher — звёзды, которые cargo manager поставил водителю по этому рейсу.
	RatingFromDispatcher *float64
}

// CompletionPending returns true if the trip is DELIVERED and the driver has requested completion but the cargo manager has not confirmed yet.
func CompletionPending(t *Trip) bool {
	if t == nil || t.Status != StatusDelivered || t.PendingConfirmTo == nil || t.DriverConfirmedAt == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(*t.PendingConfirmTo), StatusCompleted)
}
