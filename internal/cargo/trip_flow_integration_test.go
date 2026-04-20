// Интеграционные тесты потока груз/рейс: vehicles_left при IN_TRANSIT, лимит AcceptOffer, завершение груза.
// Запуск: задать TEST_DATABASE_URL или DATABASE_URL (как в companies/repo_test.go).
package cargo

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"sarbonNew/internal/infra"
)

func testIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		connStr = os.Getenv("DATABASE_URL")
	}
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL required for cargo/trip integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("pool.Ping: %v", err)
	}
	if err := infra.EnsureCargoTables(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("EnsureCargoTables: %v", err)
	}
	if err := infra.EnsureTripAgreedPriceColumns(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("EnsureTripAgreedPriceColumns: %v", err)
	}
	return pool
}

func itestStrPtr(s string) *string { return &s }

func itestCreateCargo(t *testing.T, repo *Repo, vehicles int) uuid.UUID {
	t.Helper()
	name := "itest cargo " + uuid.New().String()
	id, err := repo.Create(context.Background(), CreateParams{
		Name:             &name,
		Weight:           1,
		Volume:           1,
		VehiclesAmount:   vehicles,
		TruckType:        "TENT",
		PowerPlateType:   "TRUCK",
		TrailerPlateType: "TENTED",
		ContactName:      itestStrPtr("Contact"),
		ContactPhone:     itestStrPtr("+998901234567"),
		Status:           StatusSearchingAll,
		RoutePoints: []RoutePointInput{
			{Type: "LOAD", CountryCode: "UZ", CityCode: "TAS", Address: "load", Lat: 41.31, Lng: 69.24, PointOrder: 1, IsMainLoad: true},
			{Type: "UNLOAD", CountryCode: "UZ", CityCode: "TAS", Address: "unload", Lat: 41.32, Lng: 69.25, PointOrder: 2, IsMainUnload: true},
		},
	})
	if err != nil {
		t.Fatalf("Create cargo: %v", err)
	}
	return id
}

func itestInsertDriver(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	phone := "+99890" + id.String()[:8]
	_, err := pool.Exec(ctx, `INSERT INTO drivers (id, phone) VALUES ($1, $2)`, id, phone)
	if err != nil {
		t.Fatalf("insert driver: %v", err)
	}
	return id
}

func itestDeleteCargo(t *testing.T, pool *pgxpool.Pool, cargoID uuid.UUID) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), `DELETE FROM cargo WHERE id = $1`, cargoID)
}

// archivedTripsTableExists — миграция 000059 (archived_trips / archived_cargo).
func archivedTripsTableExists(ctx context.Context, pool *pgxpool.Pool) bool {
	var n int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'archived_trips'`).Scan(&n)
	return err == nil && n > 0
}

// TestOnTripEnteredInTransitTx_DecrementsVehiclesLeft — при первом IN_TRANSIT vehicles_left−1; статус груза остаётся SEARCHING_*.
func TestOnTripEnteredInTransitTx_DecrementsVehiclesLeft(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 10)
	defer itestDeleteCargo(t, pool, cargoID)

	c0, err := repo.GetByID(ctx, cargoID, false)
	if err != nil || c0 == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if c0.VehiclesLeft != 10 {
		t.Fatalf("vehicles_left want 10, got %d", c0.VehiclesLeft)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := repo.OnTripEnteredInTransitTx(ctx, tx, cargoID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("OnTripEnteredInTransitTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	c1, err := repo.GetByID(ctx, cargoID, false)
	if err != nil || c1 == nil {
		t.Fatalf("GetByID after: %v", err)
	}
	if c1.VehiclesLeft != 9 {
		t.Errorf("vehicles_left want 9, got %d", c1.VehiclesLeft)
	}
	if c1.Status != StatusSearchingAll {
		t.Errorf("cargo status want SEARCHING_ALL, got %s", c1.Status)
	}
}

// TestAcceptOffer_AcceptedSlotsLimit — не больше vehicles_amount принятых офферов.
func TestAcceptOffer_AcceptedSlotsLimit(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 2)
	defer itestDeleteCargo(t, pool, cargoID)

	d1, d2, d3 := itestInsertDriver(t, pool), itestInsertDriver(t, pool), itestInsertDriver(t, pool)
	o1, err := repo.CreateOffer(ctx, cargoID, d1, 100, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer1: %v", err)
	}
	o2, err := repo.CreateOffer(ctx, cargoID, d2, 100, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer2: %v", err)
	}
	o3, err := repo.CreateOffer(ctx, cargoID, d3, 100, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer3: %v", err)
	}

	if _, _, err := repo.AcceptOffer(ctx, o1); err != nil {
		t.Fatalf("AcceptOffer1: %v", err)
	}
	if _, _, err := repo.AcceptOffer(ctx, o2); err != nil {
		t.Fatalf("AcceptOffer2: %v", err)
	}
	_, _, err = repo.AcceptOffer(ctx, o3)
	if err != ErrCargoSlotsFull {
		t.Fatalf("AcceptOffer3 want ErrCargoSlotsFull, got %v", err)
	}
}

// TestArchiveCompletedCargoTx_LastTripSetsCargoCompleted — один рейс: груз остаётся в таблице со статусом COMPLETED.
func TestArchiveCompletedCargoTx_LastTripSetsCargoCompleted(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	if !archivedTripsTableExists(ctx, pool) {
		t.Skip("archived_trips missing — run migration 000059_trip_bilateral_confirm_archive")
	}
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 1)
	defer itestDeleteCargo(t, pool, cargoID)
	driverID := itestInsertDriver(t, pool)
	offerID, err := repo.CreateOffer(ctx, cargoID, driverID, 100, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if _, _, err := repo.AcceptOffer(ctx, offerID); err != nil {
		t.Fatalf("AcceptOffer: %v", err)
	}

	tripID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO trips (id, cargo_id, offer_id, driver_id, status, agreed_price, agreed_currency)
		VALUES ($1, $2, $3, $4, 'IN_PROGRESS', 100, 'USD')`,
		tripID, cargoID, offerID, driverID)
	if err != nil {
		t.Fatalf("insert trip: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := repo.ArchiveCompletedCargoTx(ctx, tx, cargoID, tripID, driverID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ArchiveCompletedCargoTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var n int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM cargo WHERE id = $1 AND status = 'COMPLETED'`, cargoID).Scan(&n)
	if n != 1 {
		t.Errorf("cargo should be COMPLETED, count=%d", n)
	}
	var tripSt string
	_ = pool.QueryRow(ctx, `SELECT status FROM trips WHERE id = $1`, tripID).Scan(&tripSt)
	if tripSt != "COMPLETED" {
		t.Errorf("trip status want COMPLETED, got %s", tripSt)
	}
}

// TestArchiveCompletedCargoTx_MultiTripKeepsCargoSearching — два рейса: завершение одного не завершает груз.
func TestArchiveCompletedCargoTx_MultiTripKeepsCargoSearching(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	if !archivedTripsTableExists(ctx, pool) {
		t.Skip("archived_trips missing — run migration 000059_trip_bilateral_confirm_archive")
	}
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 2)
	defer itestDeleteCargo(t, pool, cargoID)

	d1, d2 := itestInsertDriver(t, pool), itestInsertDriver(t, pool)
	o1, err := repo.CreateOffer(ctx, cargoID, d1, 100, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer1: %v", err)
	}
	o2, err := repo.CreateOffer(ctx, cargoID, d2, 100, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer2: %v", err)
	}
	if _, _, err := repo.AcceptOffer(ctx, o1); err != nil {
		t.Fatalf("AcceptOffer1: %v", err)
	}
	if _, _, err := repo.AcceptOffer(ctx, o2); err != nil {
		t.Fatalf("AcceptOffer2: %v", err)
	}

	trip1 := uuid.New()
	trip2 := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO trips (id, cargo_id, offer_id, driver_id, status, agreed_price, agreed_currency) VALUES
		($1, $2, $3, $4, 'IN_PROGRESS', 100, 'USD'),
		($5, $2, $6, $7, 'IN_PROGRESS', 100, 'USD')`,
		trip1, cargoID, o1, d1, trip2, o2, d2)
	if err != nil {
		t.Fatalf("insert trips: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := repo.ArchiveCompletedCargoTx(ctx, tx, cargoID, trip1, d1); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ArchiveCompletedCargoTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	cargoRow, _ := repo.GetByID(ctx, cargoID, false)
	if cargoRow == nil {
		t.Fatal("cargo missing")
	}
	// Both slots are filled with ACCEPTED offers → cargo was moved to PROCESSING by
	// refreshCargoProcessingStatusTx; completion of only one trip must not promote it to COMPLETED.
	if cargoRow.Status != StatusProcessing {
		t.Errorf("cargo should be PROCESSING after all offers accepted and mid-flight, got %s", cargoRow.Status)
	}
	var nTrips int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM trips WHERE cargo_id = $1`, cargoID).Scan(&nTrips)
	if nTrips != 2 {
		t.Errorf("want 2 trip rows, got %d", nTrips)
	}
}

// TestOnTripCancelledTx_RestoresVehiclesLeftWhenInTransit — отмена после IN_TRANSIT возвращает vehicles_left.
func TestOnTripCancelledTx_RestoresVehiclesLeftWhenInTransit(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 5)
	defer itestDeleteCargo(t, pool, cargoID)
	driverID := itestInsertDriver(t, pool)
	offerID, err := repo.CreateOffer(ctx, cargoID, driverID, 1, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if _, _, err := repo.AcceptOffer(ctx, offerID); err != nil {
		t.Fatalf("AcceptOffer: %v", err)
	}

	tx0, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := repo.OnTripEnteredInTransitTx(ctx, tx0, cargoID); err != nil {
		_ = tx0.Rollback(ctx)
		t.Fatalf("OnTripEnteredInTransitTx: %v", err)
	}
	if err := tx0.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	cBefore, _ := repo.GetByID(ctx, cargoID, false)
	if cBefore == nil || cBefore.VehiclesLeft != 4 {
		t.Fatalf("want vehicles_left 4 before cancel, got %+v", cBefore)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin2: %v", err)
	}
	if err := repo.OnTripCancelledTx(ctx, tx, cargoID, offerID, "IN_TRANSIT"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("OnTripCancelledTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit2: %v", err)
	}

	cAfter, _ := repo.GetByID(ctx, cargoID, false)
	if cAfter == nil {
		t.Fatal("cargo missing")
	}
	if cAfter.VehiclesLeft != 5 {
		t.Errorf("after cancel want vehicles_left 5, got %d", cAfter.VehiclesLeft)
	}
}

// TestOnTripCancelledTx_NoRestoreBeforeInTransit — отмена до IN_TRANSIT не трогает vehicles_left.
func TestOnTripCancelledTx_NoRestoreBeforeInTransit(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 3)
	defer itestDeleteCargo(t, pool, cargoID)
	driverID := itestInsertDriver(t, pool)
	offerID, err := repo.CreateOffer(ctx, cargoID, driverID, 1, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if _, _, err := repo.AcceptOffer(ctx, offerID); err != nil {
		t.Fatalf("AcceptOffer: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := repo.OnTripCancelledTx(ctx, tx, cargoID, offerID, "IN_PROGRESS"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("OnTripCancelledTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	cAfter, _ := repo.GetByID(ctx, cargoID, false)
	if cAfter == nil {
		t.Fatal("cargo missing")
	}
	if cAfter.VehiclesLeft != 3 {
		t.Errorf("want vehicles_left still 3, got %d", cAfter.VehiclesLeft)
	}
}

// TestAcceptOffer_PromotesCargoToProcessingAndRollsBack — при заполнении всех
// слотов cargo уходит в PROCESSING; при отмене одного рейса после IN_TRANSIT
// оффер возвращается в PENDING и cargo откатывается обратно в SEARCHING_ALL.
func TestAcceptOffer_PromotesCargoToProcessingAndRollsBack(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 2)
	defer itestDeleteCargo(t, pool, cargoID)

	d1, d2 := itestInsertDriver(t, pool), itestInsertDriver(t, pool)
	o1, err := repo.CreateOffer(ctx, cargoID, d1, 100, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer1: %v", err)
	}
	o2, err := repo.CreateOffer(ctx, cargoID, d2, 100, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer2: %v", err)
	}

	// First accept: 1/2 slots → cargo must stay SEARCHING_ALL.
	if _, _, err := repo.AcceptOffer(ctx, o1); err != nil {
		t.Fatalf("AcceptOffer1: %v", err)
	}
	cMid, _ := repo.GetByID(ctx, cargoID, false)
	if cMid == nil || cMid.Status != StatusSearchingAll {
		t.Fatalf("after 1/2 accepted want SEARCHING_ALL, got %+v", cMid)
	}

	// Second accept: 2/2 slots → cargo must move to PROCESSING.
	if _, _, err := repo.AcceptOffer(ctx, o2); err != nil {
		t.Fatalf("AcceptOffer2: %v", err)
	}
	cFull, _ := repo.GetByID(ctx, cargoID, false)
	if cFull == nil || cFull.Status != StatusProcessing {
		t.Fatalf("after 2/2 accepted want PROCESSING, got %+v", cFull)
	}

	// Cancel one trip from IN_TRANSIT → offer reverts to PENDING, accepted_count=1 → cargo rolls back.
	trip1 := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO trips (id, cargo_id, offer_id, driver_id, status, agreed_price, agreed_currency)
		 VALUES ($1, $2, $3, $4, 'IN_TRANSIT', 100, 'USD')`,
		trip1, cargoID, o1, d1); err != nil {
		t.Fatalf("insert trip: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM trips WHERE id = $1`, trip1); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("delete trip: %v", err)
	}
	if err := repo.OnTripCancelledTx(ctx, tx, cargoID, o1, "IN_TRANSIT"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("OnTripCancelledTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	cBack, _ := repo.GetByID(ctx, cargoID, false)
	if cBack == nil || cBack.Status != StatusSearchingAll {
		t.Fatalf("after slot freed want SEARCHING_ALL (rollback from PROCESSING), got %+v", cBack)
	}
}

// TestCreateOffer_DriverCannotDuplicatePendingOrRejectedAllowsRetry — второй DRIVER-оффер по тому же грузу запрещён, пока PENDING/ACCEPTED; после REJECTED снова можно.
func TestCreateOffer_DriverCannotDuplicatePendingOrRejectedAllowsRetry(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 3)
	defer itestDeleteCargo(t, pool, cargoID)
	driverID := itestInsertDriver(t, pool)

	o1, err := repo.CreateOffer(ctx, cargoID, driverID, 10, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer1: %v", err)
	}
	_, err = repo.CreateOffer(ctx, cargoID, driverID, 11, "USD", "", OfferProposedByDriver, nil)
	if err != ErrDriverOfferAlreadyExists {
		t.Fatalf("CreateOffer2 want ErrDriverOfferAlreadyExists, got %v", err)
	}
	if err := repo.RejectOffer(ctx, o1, "test"); err != nil {
		t.Fatalf("RejectOffer: %v", err)
	}
	_, err = repo.CreateOffer(ctx, cargoID, driverID, 12, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer after reject: %v", err)
	}
}

// TestCreateOffer_DriverBlockedWhenAccepted — при ACCEPTED второй запрос водителя по тому же грузу запрещён.
func TestCreateOffer_DriverBlockedWhenAccepted(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 2)
	defer itestDeleteCargo(t, pool, cargoID)
	driverID := itestInsertDriver(t, pool)

	o1, err := repo.CreateOffer(ctx, cargoID, driverID, 10, "USD", "", OfferProposedByDriver, nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if _, _, err := repo.AcceptOffer(ctx, o1); err != nil {
		t.Fatalf("AcceptOffer: %v", err)
	}
	_, err = repo.CreateOffer(ctx, cargoID, driverID, 11, "USD", "", OfferProposedByDriver, nil)
	if err != ErrDriverOfferAlreadyExists {
		t.Fatalf("second CreateOffer want ErrDriverOfferAlreadyExists, got %v", err)
	}
}

// TestCreateOffer_DispatcherPendingUniqueRepeatAfterReject — два PENDING DISPATCHER одному водителю нельзя; после REJECTED — снова можно.
func TestCreateOffer_DispatcherPendingUniqueRepeatAfterReject(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 2)
	defer itestDeleteCargo(t, pool, cargoID)
	driverID := itestInsertDriver(t, pool)

	o1, err := repo.CreateOffer(ctx, cargoID, driverID, 10, "USD", "", OfferProposedByDispatcher, nil)
	if err != nil {
		t.Fatalf("CreateOffer dispatcher1: %v", err)
	}
	_, err = repo.CreateOffer(ctx, cargoID, driverID, 11, "USD", "", OfferProposedByDispatcher, nil)
	if err != ErrDispatcherOfferAlreadyExists {
		t.Fatalf("CreateOffer dispatcher2 want ErrDispatcherOfferAlreadyExists, got %v", err)
	}
	if err := repo.RejectOffer(ctx, o1, "test"); err != nil {
		t.Fatalf("RejectOffer: %v", err)
	}
	_, err = repo.CreateOffer(ctx, cargoID, driverID, 12, "USD", "", OfferProposedByDispatcher, nil)
	if err != nil {
		t.Fatalf("CreateOffer dispatcher after reject: %v", err)
	}
}
