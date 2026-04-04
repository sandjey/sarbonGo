// Интеграционные тесты потока груз/рейс: vehicles_left при LOADING, лимит AcceptOffer, архивация COMPLETED.
// Запуск: задать TEST_DATABASE_URL или DATABASE_URL (как в companies/repo_test.go).
package cargo

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

// TestOnTripEnteredLoadingTx_DecrementsVehiclesLeft проверяет: при первом LOADING vehicles_left−1 и статус IN_PROGRESS.
func TestOnTripEnteredLoadingTx_DecrementsVehiclesLeft(t *testing.T) {
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
	if err := repo.OnTripEnteredLoadingTx(ctx, tx, cargoID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("OnTripEnteredLoadingTx: %v", err)
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
	if c1.Status != StatusInProgress {
		t.Errorf("cargo status want IN_PROGRESS, got %s", c1.Status)
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
	o1, err := repo.CreateOffer(ctx, cargoID, d1, 100, "USD", "")
	if err != nil {
		t.Fatalf("CreateOffer1: %v", err)
	}
	o2, err := repo.CreateOffer(ctx, cargoID, d2, 100, "USD", "")
	if err != nil {
		t.Fatalf("CreateOffer2: %v", err)
	}
	o3, err := repo.CreateOffer(ctx, cargoID, d3, 100, "USD", "")
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

// TestArchiveCompletedCargoTx_LastTripDeletesCargo — один рейс: после архива груза нет.
func TestArchiveCompletedCargoTx_LastTripDeletesCargo(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	if !archivedTripsTableExists(ctx, pool) {
		t.Skip("archived_trips missing — run migration 000059_trip_bilateral_confirm_archive")
	}
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 1)
	driverID := itestInsertDriver(t, pool)
	offerID, err := repo.CreateOffer(ctx, cargoID, driverID, 100, "USD", "")
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if _, _, err := repo.AcceptOffer(ctx, offerID); err != nil {
		t.Fatalf("AcceptOffer: %v", err)
	}

	tripID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO trips (id, cargo_id, offer_id, driver_id, status)
		VALUES ($1, $2, $3, $4, 'COMPLETED')`,
		tripID, cargoID, offerID, driverID)
	if err != nil {
		itestDeleteCargo(t, pool, cargoID)
		t.Fatalf("insert trip: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		itestDeleteCargo(t, pool, cargoID)
		t.Fatalf("begin: %v", err)
	}
	if err := repo.ArchiveCompletedCargoTx(ctx, tx, cargoID, tripID, driverID); err != nil {
		_ = tx.Rollback(ctx)
		itestDeleteCargo(t, pool, cargoID)
		t.Fatalf("ArchiveCompletedCargoTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var n int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM cargo WHERE id = $1`, cargoID).Scan(&n)
	if n != 0 {
		t.Errorf("cargo should be deleted, count=%d", n)
	}
}

// TestArchiveCompletedCargoTx_MultiTripKeepsCargo — два рейса: завершение одного не удаляет груз.
func TestArchiveCompletedCargoTx_MultiTripKeepsCargo(t *testing.T) {
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
	o1, err := repo.CreateOffer(ctx, cargoID, d1, 100, "USD", "")
	if err != nil {
		t.Fatalf("CreateOffer1: %v", err)
	}
	o2, err := repo.CreateOffer(ctx, cargoID, d2, 100, "USD", "")
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
		INSERT INTO trips (id, cargo_id, offer_id, driver_id, status) VALUES
		($1, $2, $3, $4, 'COMPLETED'),
		($5, $2, $6, $7, 'LOADING')`,
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

	var nCargo int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM cargo WHERE id = $1`, cargoID).Scan(&nCargo)
	if nCargo != 1 {
		t.Fatalf("cargo should exist, count=%d", nCargo)
	}
	var nTrips int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM trips WHERE cargo_id = $1`, cargoID).Scan(&nTrips)
	if nTrips != 1 {
		t.Errorf("want 1 trip left, got %d", nTrips)
	}
}

// TestOnTripCancelledTx_RestoresVehiclesLeftWhenLoading — отмена после LOADING возвращает vehicles_left.
func TestOnTripCancelledTx_RestoresVehiclesLeftWhenLoading(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 5)
	defer itestDeleteCargo(t, pool, cargoID)
	driverID := itestInsertDriver(t, pool)
	offerID, err := repo.CreateOffer(ctx, cargoID, driverID, 1, "USD", "")
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
	if err := repo.OnTripEnteredLoadingTx(ctx, tx0, cargoID); err != nil {
		_ = tx0.Rollback(ctx)
		t.Fatalf("OnTripEnteredLoadingTx: %v", err)
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
	if err := repo.OnTripCancelledTx(ctx, tx, cargoID, offerID, "LOADING"); err != nil {
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

// TestOnTripCancelledTx_NoRestoreBeforeLoading — отмена до LOADING не трогает vehicles_left.
func TestOnTripCancelledTx_NoRestoreBeforeLoading(t *testing.T) {
	pool := testIntegrationPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewRepo(pool)

	cargoID := itestCreateCargo(t, repo, 3)
	defer itestDeleteCargo(t, pool, cargoID)
	driverID := itestInsertDriver(t, pool)
	offerID, err := repo.CreateOffer(ctx, cargoID, driverID, 1, "USD", "")
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
	if err := repo.OnTripCancelledTx(ctx, tx, cargoID, offerID, "ASSIGNED"); err != nil {
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
