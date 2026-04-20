package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"
	"go.uber.org/zap"

	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

type DispatcherAnalyticsHandler struct {
	logger          *zap.Logger
	pg              *pgxpool.Pool
	dispatchersRepo *dispatchers.Repo
}

func NewDispatcherAnalyticsHandler(logger *zap.Logger, pg *pgxpool.Pool, dispatchersRepo *dispatchers.Repo) *DispatcherAnalyticsHandler {
	return &DispatcherAnalyticsHandler{
		logger:          logger,
		pg:              pg,
		dispatchersRepo: dispatchersRepo,
	}
}

type analyticsFilter struct {
	DateFrom     time.Time
	DateTo       time.Time
	Period       string
	PeriodStep   string
	Timezone     string
	TruckTypes   []string
	Statuses     []string
	FromCityCode string
	ToCityCode   string
	DriverID     *uuid.UUID
	CargoID      *uuid.UUID
}

type analyticsExportOptions struct {
	Sections      map[string]bool
	ContractScope string
}

func parseAnalyticsExportOptions(c *gin.Context) analyticsExportOptions {
	sections := map[string]bool{
		"summary":   true,
		"kpi":       true,
		"funnel":    true,
		"timeline":  true,
		"top":       true,
		"contracts": true,
	}
	if raw := strings.TrimSpace(c.Query("sections")); raw != "" {
		sections = map[string]bool{}
		for _, p := range strings.Split(raw, ",") {
			k := strings.ToLower(strings.TrimSpace(p))
			if k == "" {
				continue
			}
			sections[k] = true
		}
	}
	scope := strings.ToLower(strings.TrimSpace(c.DefaultQuery("contract_scope", "all")))
	switch scope {
	case "all", "successful", "cancelled", "in_execution":
	default:
		scope = "all"
	}
	return analyticsExportOptions{
		Sections:      sections,
		ContractScope: scope,
	}
}

func parseAnalyticsFilter(c *gin.Context) (analyticsFilter, bool) {
	tz := strings.TrimSpace(c.DefaultQuery("timezone", "Asia/Tashkent"))
	if tz == "" {
		tz = "Asia/Tashkent"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "timezone"})
		return analyticsFilter{}, false
	}

	dateFromRaw := strings.TrimSpace(c.Query("date_from"))
	dateToRaw := strings.TrimSpace(c.Query("date_to"))
	if dateFromRaw == "" || dateToRaw == "" {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "date_from/date_to"})
		return analyticsFilter{}, false
	}
	dateFromDay, err := time.ParseInLocation("2006-01-02", dateFromRaw, loc)
	if err != nil {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "date_from"})
		return analyticsFilter{}, false
	}
	dateToDay, err := time.ParseInLocation("2006-01-02", dateToRaw, loc)
	if err != nil {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "date_to"})
		return analyticsFilter{}, false
	}

	from := dateFromDay.UTC()
	toExclusive := dateToDay.Add(24 * time.Hour).UTC()
	if !from.Before(toExclusive) {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "date_range"})
		return analyticsFilter{}, false
	}
	if toExclusive.Sub(from) > (370 * 24 * time.Hour) {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "date_range_too_big"})
		return analyticsFilter{}, false
	}

	period := strings.ToLower(strings.TrimSpace(c.DefaultQuery("group_by", "day")))
	step := "1 day"
	switch period {
	case "day":
		step = "1 day"
	case "week":
		step = "1 week"
	case "month":
		step = "1 month"
	default:
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "group_by"})
		return analyticsFilter{}, false
	}

	parseList := func(v string, upper bool) []string {
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			item := strings.TrimSpace(p)
			if item == "" {
				continue
			}
			if upper {
				item = strings.ToUpper(item)
			}
			out = append(out, item)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}

	var driverID *uuid.UUID
	if raw := strings.TrimSpace(c.Query("driver_id")); raw != "" {
		id, perr := uuid.Parse(raw)
		if perr != nil {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "driver_id"})
			return analyticsFilter{}, false
		}
		driverID = &id
	}

	var cargoID *uuid.UUID
	if raw := strings.TrimSpace(c.Query("cargo_id")); raw != "" {
		id, perr := uuid.Parse(raw)
		if perr != nil {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{"field": "cargo_id"})
			return analyticsFilter{}, false
		}
		cargoID = &id
	}

	return analyticsFilter{
		DateFrom:     from,
		DateTo:       toExclusive,
		Period:       period,
		PeriodStep:   step,
		Timezone:     tz,
		TruckTypes:   parseList(c.Query("truck_type"), true),
		Statuses:     parseList(c.Query("status"), true),
		FromCityCode: strings.ToUpper(strings.TrimSpace(c.Query("from_city_code"))),
		ToCityCode:   strings.ToUpper(strings.TrimSpace(c.Query("to_city_code"))),
		DriverID:     driverID,
		CargoID:      cargoID,
	}, true
}

func (h *DispatcherAnalyticsHandler) ensureRole(c *gin.Context, expected string) (uuid.UUID, bool) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	d, err := h.dispatchersRepo.FindByID(c.Request.Context(), dispatcherID)
	if err != nil || d == nil || d.ManagerRole == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "unauthorized")
		return uuid.Nil, false
	}
	if strings.TrimSpace(strings.ToUpper(*d.ManagerRole)) != expected {
		resp.ErrorLang(c, http.StatusForbidden, "forbidden")
		return uuid.Nil, false
	}
	return dispatcherID, true
}

func cargoFilterClause(alias string, f analyticsFilter, args *[]any) string {
	parts := []string{
		alias + ".deleted_at IS NULL",
		alias + ".created_by_type = 'DISPATCHER'",
	}
	if len(f.TruckTypes) > 0 {
		*args = append(*args, f.TruckTypes)
		parts = append(parts, alias+".truck_type = ANY($"+strconv.Itoa(len(*args))+")")
	}
	if len(f.Statuses) > 0 {
		*args = append(*args, f.Statuses)
		parts = append(parts, alias+".status = ANY($"+strconv.Itoa(len(*args))+")")
	}
	if f.CargoID != nil {
		*args = append(*args, *f.CargoID)
		parts = append(parts, alias+".id = $"+strconv.Itoa(len(*args)))
	}
	if f.FromCityCode != "" {
		*args = append(*args, f.FromCityCode)
		parts = append(parts, "EXISTS (SELECT 1 FROM route_points rp WHERE rp.cargo_id = "+alias+".id AND rp.is_main_load = true AND rp.city_code = $"+strconv.Itoa(len(*args))+")")
	}
	if f.ToCityCode != "" {
		*args = append(*args, f.ToCityCode)
		parts = append(parts, "EXISTS (SELECT 1 FROM route_points rp WHERE rp.cargo_id = "+alias+".id AND rp.is_main_unload = true AND rp.city_code = $"+strconv.Itoa(len(*args))+")")
	}
	return strings.Join(parts, " AND ")
}

func (h *DispatcherAnalyticsHandler) cargoManagerDashboardData(ctx context.Context, dispatcherID uuid.UUID, f analyticsFilter) (gin.H, error) {
	args := []any{dispatcherID}
	cargoClause := cargoFilterClause("c", f, &args)

	argsKPI := append([]any{}, args...)
	argsKPI = append(argsKPI, f.DateFrom, f.DateTo)
	kpiSQL := `
WITH scoped_cargo AS (
  SELECT c.id, c.status
  FROM cargo c
  WHERE c.created_by_id = $1 AND ` + cargoClause + ` AND c.created_at >= $` + strconv.Itoa(len(args)+1) + ` AND c.created_at < $` + strconv.Itoa(len(args)+2) + `
),
scoped_offers AS (
  SELECT o.id, o.status, o.proposed_by
  FROM offers o
  JOIN cargo c ON c.id = o.cargo_id
  WHERE c.created_by_id = $1 AND ` + cargoClause + ` AND o.created_at >= $` + strconv.Itoa(len(args)+1) + ` AND o.created_at < $` + strconv.Itoa(len(args)+2) + `
),
scoped_trips AS (
  SELECT t.id, t.status, t.driver_id, t.agreed_price
  FROM trips t
  JOIN cargo c ON c.id = t.cargo_id
  WHERE c.created_by_id = $1 AND ` + cargoClause + ` AND t.created_at >= $` + strconv.Itoa(len(args)+1) + ` AND t.created_at < $` + strconv.Itoa(len(args)+2) + `
)
SELECT
  (SELECT COUNT(*) FROM scoped_cargo) AS cargos_total,
  (SELECT COUNT(*) FROM scoped_cargo WHERE status IN ('SEARCHING_ALL', 'SEARCHING_COMPANY', 'PROCESSING')) AS cargos_active,
  (SELECT COUNT(*) FROM scoped_cargo WHERE status = 'COMPLETED') AS cargos_completed,
  (SELECT COUNT(*) FROM scoped_cargo WHERE status = 'CANCELLED') AS cargos_cancelled,
  (SELECT COUNT(*) FROM scoped_offers WHERE COALESCE(proposed_by, 'DRIVER') IN ('DRIVER','DRIVER_MANAGER')) AS offers_incoming_total,
  (SELECT COUNT(*) FROM scoped_offers WHERE COALESCE(proposed_by, 'DRIVER') = 'DISPATCHER') AS offers_outgoing_total,
  (SELECT COUNT(*) FROM scoped_offers WHERE status = 'ACCEPTED') AS offers_accepted,
  (SELECT COUNT(*) FROM scoped_offers WHERE status = 'REJECTED') AS offers_rejected,
  (SELECT COUNT(*) FROM scoped_offers WHERE status = 'CANCELED') AS offers_canceled,
  (SELECT COUNT(*) FROM scoped_trips) AS trips_total,
  (SELECT COUNT(*) FROM scoped_trips WHERE status = 'IN_PROGRESS') AS trips_in_progress,
  (SELECT COUNT(*) FROM scoped_trips WHERE status = 'IN_TRANSIT') AS trips_in_transit,
  (SELECT COUNT(*) FROM scoped_trips WHERE status = 'DELIVERED') AS trips_delivered,
  (SELECT COUNT(*) FROM scoped_trips WHERE status = 'COMPLETED') AS trips_completed,
  (SELECT COUNT(*) FROM scoped_trips WHERE status = 'CANCELLED') AS trips_cancelled,
  (SELECT COUNT(*) FROM scoped_trips WHERE status IN ('IN_PROGRESS', 'IN_TRANSIT', 'DELIVERED')) AS trips_in_execution,
  (SELECT COALESCE(SUM(agreed_price),0) FROM scoped_trips WHERE status = 'COMPLETED') AS revenue_completed`

	var cargosTotal, cargosActive, cargosCompleted, cargosCancelled int64
	var offersIncoming, offersOutgoing, offersAccepted, offersRejected, offersCanceled int64
	var tripsTotal, tripsInProgress, tripsInTransit, tripsDelivered, tripsCompleted, tripsCancelled, tripsInExecution int64
	var revenueCompleted float64
	if err := h.pg.QueryRow(ctx, kpiSQL, argsKPI...).Scan(
		&cargosTotal, &cargosActive, &cargosCompleted, &cargosCancelled,
		&offersIncoming, &offersOutgoing, &offersAccepted, &offersRejected, &offersCanceled,
		&tripsTotal, &tripsInProgress, &tripsInTransit, &tripsDelivered, &tripsCompleted, &tripsCancelled, &tripsInExecution,
		&revenueCompleted,
	); err != nil {
		return nil, err
	}

	offerAcceptRate := 0.0
	if offersIncoming > 0 {
		offerAcceptRate = (float64(offersAccepted) / float64(offersIncoming)) * 100
	}
	tripCompletionRate := 0.0
	if tripsTotal > 0 {
		tripCompletionRate = (float64(tripsCompleted) / float64(tripsTotal)) * 100
	}

	trendArgs := append([]any{}, args...)
	trendArgs = append(trendArgs, f.DateFrom, f.DateTo)
	trendSQL := `
WITH buckets AS (
  SELECT generate_series(
    date_trunc('` + f.Period + `', $` + strconv.Itoa(len(args)+1) + `::timestamptz),
    date_trunc('` + f.Period + `', ($` + strconv.Itoa(len(args)+2) + `::timestamptz - interval '1 second')),
    interval '` + f.PeriodStep + `'
  ) AS bucket
),
cargo_agg AS (
  SELECT date_trunc('` + f.Period + `', c.created_at) AS bucket, COUNT(*) AS cargo_created
  FROM cargo c
  WHERE c.created_by_id = $1 AND ` + cargoClause + ` AND c.created_at >= $` + strconv.Itoa(len(args)+1) + ` AND c.created_at < $` + strconv.Itoa(len(args)+2) + `
  GROUP BY 1
),
offers_agg AS (
  SELECT date_trunc('` + f.Period + `', o.created_at) AS bucket,
         COUNT(*) FILTER (WHERE COALESCE(o.proposed_by, 'DRIVER') IN ('DRIVER','DRIVER_MANAGER')) AS offers_incoming,
         COUNT(*) FILTER (WHERE COALESCE(o.proposed_by, 'DRIVER') = 'DISPATCHER') AS offers_outgoing
  FROM offers o
  JOIN cargo c ON c.id = o.cargo_id
  WHERE c.created_by_id = $1 AND ` + cargoClause + ` AND o.created_at >= $` + strconv.Itoa(len(args)+1) + ` AND o.created_at < $` + strconv.Itoa(len(args)+2) + `
  GROUP BY 1
),
trips_agg AS (
  SELECT date_trunc('` + f.Period + `', t.created_at) AS bucket,
         COUNT(*) AS trips_started,
         COUNT(*) FILTER (WHERE t.status = 'COMPLETED') AS trips_completed
  FROM trips t
  JOIN cargo c ON c.id = t.cargo_id
  WHERE c.created_by_id = $1 AND ` + cargoClause + ` AND t.created_at >= $` + strconv.Itoa(len(args)+1) + ` AND t.created_at < $` + strconv.Itoa(len(args)+2) + `
  GROUP BY 1
)
SELECT b.bucket,
       COALESCE(ca.cargo_created,0),
       COALESCE(oa.offers_incoming,0),
       COALESCE(oa.offers_outgoing,0),
       COALESCE(ta.trips_started,0),
       COALESCE(ta.trips_completed,0)
FROM buckets b
LEFT JOIN cargo_agg ca ON ca.bucket = b.bucket
LEFT JOIN offers_agg oa ON oa.bucket = b.bucket
LEFT JOIN trips_agg ta ON ta.bucket = b.bucket
ORDER BY b.bucket ASC`

	trRows, err := h.pg.Query(ctx, trendSQL, trendArgs...)
	if err != nil {
		return nil, err
	}
	defer trRows.Close()
	trends := make([]gin.H, 0)
	for trRows.Next() {
		var bucket time.Time
		var cargoCreated, offersIn, offersOut, tripsStarted, tripsDone int64
		if err := trRows.Scan(&bucket, &cargoCreated, &offersIn, &offersOut, &tripsStarted, &tripsDone); err != nil {
			return nil, err
		}
		trends = append(trends, gin.H{
			"period_start":    bucket.UTC().Format(time.RFC3339),
			"cargo_created":   cargoCreated,
			"offers_incoming": offersIn,
			"offers_outgoing": offersOut,
			"trips_started":   tripsStarted,
			"trips_completed": tripsDone,
		})
	}

	breakdownArgs := append([]any{}, args...)
	breakdownArgs = append(breakdownArgs, f.DateFrom, f.DateTo)
	routeSQL := `
SELECT
  COALESCE(fl.city_code, 'N/A') AS from_city_code,
  COALESCE(tu.city_code, 'N/A') AS to_city_code,
  COUNT(*) AS trips_count,
  COALESCE(SUM(t.agreed_price),0) AS total_price
FROM trips t
JOIN cargo c ON c.id = t.cargo_id
LEFT JOIN route_points fl ON fl.cargo_id = c.id AND fl.is_main_load = true
LEFT JOIN route_points tu ON tu.cargo_id = c.id AND tu.is_main_unload = true
WHERE c.created_by_id = $1 AND ` + cargoClause + ` AND t.created_at >= $` + strconv.Itoa(len(args)+1) + ` AND t.created_at < $` + strconv.Itoa(len(args)+2) + `
GROUP BY 1,2
ORDER BY trips_count DESC, total_price DESC
LIMIT 10`
	routeRows, err := h.pg.Query(ctx, routeSQL, breakdownArgs...)
	if err != nil {
		return nil, err
	}
	defer routeRows.Close()
	topRoutes := make([]gin.H, 0)
	for routeRows.Next() {
		var from, to string
		var tripsCount int64
		var totalPrice float64
		if err := routeRows.Scan(&from, &to, &tripsCount, &totalPrice); err != nil {
			return nil, err
		}
		topRoutes = append(topRoutes, gin.H{
			"from_city_code": from,
			"to_city_code":   to,
			"trips_count":    tripsCount,
			"total_price":    totalPrice,
		})
	}

	topDriversSQL := `
SELECT t.driver_id, COUNT(*) AS trips_count, COUNT(*) FILTER (WHERE t.status='COMPLETED') AS trips_completed,
       COALESCE(AVG(t.agreed_price),0) AS avg_agreed_price
FROM trips t
JOIN cargo c ON c.id = t.cargo_id
WHERE c.created_by_id = $1 AND ` + cargoClause + ` AND t.created_at >= $` + strconv.Itoa(len(args)+1) + ` AND t.created_at < $` + strconv.Itoa(len(args)+2) + ` AND t.driver_id IS NOT NULL
GROUP BY t.driver_id
ORDER BY trips_completed DESC, trips_count DESC
LIMIT 10`
	topDriverRows, err := h.pg.Query(ctx, topDriversSQL, breakdownArgs...)
	if err != nil {
		return nil, err
	}
	defer topDriverRows.Close()
	topDrivers := make([]gin.H, 0)
	for topDriverRows.Next() {
		var driverID uuid.UUID
		var tripsCount, tripsDone int64
		var avgPrice float64
		if err := topDriverRows.Scan(&driverID, &tripsCount, &tripsDone, &avgPrice); err != nil {
			return nil, err
		}
		topDrivers = append(topDrivers, gin.H{
			"driver_id":        driverID.String(),
			"trips_count":      tripsCount,
			"trips_completed":  tripsDone,
			"avg_agreed_price": avgPrice,
		})
	}

	return gin.H{
		"filters": gin.H{
			"date_from":      f.DateFrom.Format("2006-01-02"),
			"date_to":        f.DateTo.Add(-time.Second).Format("2006-01-02"),
			"group_by":       f.Period,
			"timezone":       f.Timezone,
			"truck_type":     f.TruckTypes,
			"status":         f.Statuses,
			"from_city_code": f.FromCityCode,
			"to_city_code":   f.ToCityCode,
			"driver_id":      idPtrToString(f.DriverID),
			"cargo_id":       idPtrToString(f.CargoID),
		},
		"kpi": gin.H{
			"cargo_total":            cargosTotal,
			"cargos_total":           cargosTotal,
			"cargos_active":          cargosActive,
			"cargos_completed":       cargosCompleted,
			"cargos_cancelled":       cargosCancelled,
			"contracts_total":        offersAccepted,
			"contracts_successful":   tripsCompleted,
			"contracts_cancelled":    tripsCancelled,
			"contracts_in_execution": tripsInExecution,
			"offers_incoming_total":  offersIncoming,
			"offers_outgoing_total":  offersOutgoing,
			"offers_accepted":        offersAccepted,
			"offers_rejected":        offersRejected,
			"offers_canceled":        offersCanceled,
			"offer_accept_rate_pct":  offerAcceptRate,
			"trips_total":            tripsTotal,
			"trips_in_progress":      tripsInProgress,
			"trips_in_transit":       tripsInTransit,
			"trips_delivered":        tripsDelivered,
			"trips_completed":        tripsCompleted,
			"trips_cancelled":        tripsCancelled,
			"trips_in_execution":     tripsInExecution,
			"trip_completion_rate":   tripCompletionRate,
			"revenue_completed":      revenueCompleted,
		},
		"funnel": gin.H{
			"offers_total":                      offersIncoming + offersOutgoing,
			"offers_accepted":                   offersAccepted,
			"trips_in_execution":                tripsInExecution,
			"trips_completed":                   tripsCompleted,
			"trips_cancelled":                   tripsCancelled,
			"conversion_offer_to_completed_pct": tripCompletionRate,
		},
		"timeline": gin.H{
			"items": trends,
		},
		"top": gin.H{
			"routes":  topRoutes,
			"drivers": topDrivers,
		},
	}, nil
}

func idPtrToString(v *uuid.UUID) any {
	if v == nil {
		return nil
	}
	return v.String()
}

func (h *DispatcherAnalyticsHandler) driverManagerDashboardData(ctx context.Context, dispatcherID uuid.UUID, f analyticsFilter) (gin.H, error) {
	args := []any{dispatcherID, f.DateFrom, f.DateTo}
	driverFilterClause := ""
	if f.DriverID != nil {
		args = append(args, *f.DriverID)
		driverFilterClause = " AND r.driver_id = $" + strconv.Itoa(len(args))
	}

	// driver_manager_relations не имеет колонки status (см. migration 000074):
	// наличие строки = активная связь, разрыв — DELETE (UnlinkManager).
	kpiSQL := `
WITH rel AS (
  SELECT r.driver_id
  FROM driver_manager_relations r
  WHERE r.manager_id = $1` + driverFilterClause + `
),
driver_stats AS (
  SELECT
    COUNT(*) AS drivers_total,
    COUNT(*) FILTER (WHERE COALESCE(d.work_status,'') = 'AVAILABLE') AS drivers_available,
    COUNT(*) FILTER (WHERE COALESCE(d.work_status,'') = 'BUSY') AS drivers_busy
  FROM rel r
  LEFT JOIN drivers d ON d.id = r.driver_id
),
offer_stats AS (
  SELECT
    COUNT(*) AS offers_total,
    COUNT(*) FILTER (WHERE o.status='ACCEPTED') AS offers_accepted,
    COUNT(*) FILTER (WHERE o.status='REJECTED') AS offers_rejected,
    COUNT(*) FILTER (WHERE o.status='CANCELED') AS offers_canceled,
    COUNT(*) FILTER (WHERE o.status='WAITING_DRIVER_CONFIRM') AS offers_waiting_driver_confirm
  FROM offers o
  WHERE ((o.proposed_by = 'DRIVER_MANAGER' AND o.proposed_by_id = $1) OR o.negotiation_dispatcher_id = $1)
    AND o.created_at >= $2 AND o.created_at < $3
),
trip_stats AS (
  SELECT
    COUNT(*) AS trips_total,
    COUNT(*) FILTER (WHERE t.status='IN_PROGRESS') AS trips_in_progress,
    COUNT(*) FILTER (WHERE t.status='IN_TRANSIT') AS trips_in_transit,
    COUNT(*) FILTER (WHERE t.status='DELIVERED') AS trips_delivered,
    COUNT(*) FILTER (WHERE t.status='COMPLETED') AS trips_completed,
    COUNT(*) FILTER (WHERE t.status='CANCELLED') AS trips_cancelled,
    COUNT(*) FILTER (WHERE t.status IN ('IN_PROGRESS','IN_TRANSIT','DELIVERED')) AS trips_in_execution,
    COUNT(DISTINCT t.cargo_id) AS cargos_total,
    COUNT(DISTINCT t.cargo_id) FILTER (WHERE t.status='COMPLETED') AS cargos_completed_by_my_drivers,
    COALESCE(SUM(t.agreed_price) FILTER (WHERE t.status='COMPLETED'),0) AS completed_value
  FROM trips t
  JOIN offers o ON o.id = t.offer_id
  WHERE ((o.proposed_by = 'DRIVER_MANAGER' AND o.proposed_by_id = $1) OR o.negotiation_dispatcher_id = $1)
    AND t.created_at >= $2 AND t.created_at < $3
)
SELECT
  ds.drivers_total, ds.drivers_available, ds.drivers_busy,
  os.offers_total, os.offers_accepted, os.offers_rejected, os.offers_canceled, os.offers_waiting_driver_confirm,
  ts.trips_total, ts.trips_in_progress, ts.trips_in_transit, ts.trips_delivered, ts.trips_completed, ts.trips_cancelled, ts.trips_in_execution,
  ts.cargos_total, ts.cargos_completed_by_my_drivers, ts.completed_value
FROM driver_stats ds, offer_stats os, trip_stats ts`

	var driversTotal, driversAvailable, driversBusy int64
	var offersTotal, offersAccepted, offersRejected, offersCanceled, offersWaiting int64
	var tripsTotal, tripsInProgress, tripsInTransit, tripsDelivered, tripsCompleted, tripsCancelled, tripsInExecution int64
	var cargosTotal, cargosCompletedByMyDrivers int64
	var completedValue float64
	if err := h.pg.QueryRow(ctx, kpiSQL, args...).Scan(
		&driversTotal, &driversAvailable, &driversBusy,
		&offersTotal, &offersAccepted, &offersRejected, &offersCanceled, &offersWaiting,
		&tripsTotal, &tripsInProgress, &tripsInTransit, &tripsDelivered, &tripsCompleted, &tripsCancelled, &tripsInExecution,
		&cargosTotal, &cargosCompletedByMyDrivers, &completedValue,
	); err != nil {
		return nil, err
	}

	offerAcceptRate := 0.0
	if offersTotal > 0 {
		offerAcceptRate = (float64(offersAccepted) / float64(offersTotal)) * 100
	}
	tripCompletionRate := 0.0
	if tripsTotal > 0 {
		tripCompletionRate = (float64(tripsCompleted) / float64(tripsTotal)) * 100
	}

	utilizationPct := 0.0
	if driversTotal > 0 {
		utilizationPct = (float64(driversBusy) / float64(driversTotal)) * 100
	}

	trendSQL := `
WITH buckets AS (
  SELECT generate_series(
    date_trunc('` + f.Period + `', $2::timestamptz),
    date_trunc('` + f.Period + `', ($3::timestamptz - interval '1 second')),
    interval '` + f.PeriodStep + `'
  ) AS bucket
),
offers_agg AS (
  SELECT date_trunc('` + f.Period + `', o.created_at) AS bucket,
         COUNT(*) AS offers_total,
         COUNT(*) FILTER (WHERE o.status='ACCEPTED') AS offers_accepted
  FROM offers o
  WHERE ((o.proposed_by = 'DRIVER_MANAGER' AND o.proposed_by_id = $1) OR o.negotiation_dispatcher_id = $1)
    AND o.created_at >= $2 AND o.created_at < $3
  GROUP BY 1
),
trips_agg AS (
  SELECT date_trunc('` + f.Period + `', t.created_at) AS bucket,
         COUNT(*) AS trips_total,
         COUNT(*) FILTER (WHERE t.status='COMPLETED') AS trips_completed
  FROM trips t
  JOIN offers o ON o.id = t.offer_id
  WHERE ((o.proposed_by = 'DRIVER_MANAGER' AND o.proposed_by_id = $1) OR o.negotiation_dispatcher_id = $1)
    AND t.created_at >= $2 AND t.created_at < $3
  GROUP BY 1
)
SELECT b.bucket, COALESCE(oa.offers_total,0), COALESCE(oa.offers_accepted,0), COALESCE(ta.trips_total,0), COALESCE(ta.trips_completed,0)
FROM buckets b
LEFT JOIN offers_agg oa ON oa.bucket = b.bucket
LEFT JOIN trips_agg ta ON ta.bucket = b.bucket
ORDER BY b.bucket ASC`

	trRows, err := h.pg.Query(ctx, trendSQL, dispatcherID, f.DateFrom, f.DateTo)
	if err != nil {
		return nil, err
	}
	defer trRows.Close()
	trends := make([]gin.H, 0)
	for trRows.Next() {
		var bucket time.Time
		var offersCnt, offersOk, tripsCnt, tripsOk int64
		if err := trRows.Scan(&bucket, &offersCnt, &offersOk, &tripsCnt, &tripsOk); err != nil {
			return nil, err
		}
		trends = append(trends, gin.H{
			"period_start":    bucket.UTC().Format(time.RFC3339),
			"offers_total":    offersCnt,
			"offers_accepted": offersOk,
			"trips_total":     tripsCnt,
			"trips_completed": tripsOk,
		})
	}

	topDriversSQL := `
SELECT
  r.driver_id,
  COALESCE(d.name, '') AS driver_name,
  COALESCE(d.work_status, '') AS work_status,
  COUNT(t.id) AS trips_total,
  COUNT(t.id) FILTER (WHERE t.status = 'COMPLETED') AS trips_completed
FROM driver_manager_relations r
LEFT JOIN drivers d ON d.id = r.driver_id
LEFT JOIN trips t ON t.driver_id = r.driver_id AND t.created_at >= $2 AND t.created_at < $3
LEFT JOIN offers o ON o.id = t.offer_id
WHERE r.manager_id = $1
  AND (
    t.id IS NULL OR
    (o.proposed_by = 'DRIVER_MANAGER' AND o.proposed_by_id = $1) OR o.negotiation_dispatcher_id = $1
  )
GROUP BY r.driver_id, d.name, d.work_status
ORDER BY trips_completed DESC, trips_total DESC
LIMIT 10`
	topRows, err := h.pg.Query(ctx, topDriversSQL, dispatcherID, f.DateFrom, f.DateTo)
	if err != nil {
		return nil, err
	}
	defer topRows.Close()
	topDrivers := make([]gin.H, 0)
	for topRows.Next() {
		var driverID uuid.UUID
		var name, workStatus string
		var tripsCnt, tripsDone int64
		if err := topRows.Scan(&driverID, &name, &workStatus, &tripsCnt, &tripsDone); err != nil {
			return nil, err
		}
		topDrivers = append(topDrivers, gin.H{
			"driver_id":       driverID.String(),
			"driver_name":     strings.TrimSpace(name),
			"work_status":     strings.TrimSpace(workStatus),
			"trips_total":     tripsCnt,
			"trips_completed": tripsDone,
		})
	}

	return gin.H{
		"filters": gin.H{
			"date_from":  f.DateFrom.Format("2006-01-02"),
			"date_to":    f.DateTo.Add(-time.Second).Format("2006-01-02"),
			"group_by":   f.Period,
			"timezone":   f.Timezone,
			"driver_id":  idPtrToString(f.DriverID),
			"cargo_id":   idPtrToString(f.CargoID),
			"truck_type": f.TruckTypes,
			"status":     f.Statuses,
		},
		"kpi": gin.H{
			"drivers_total":                  driversTotal,
			"drivers_available":              driversAvailable,
			"drivers_busy":                   driversBusy,
			"driver_utilization_pct":         utilizationPct,
			"cargo_total":                    cargosTotal,
			"cargos_total":                   cargosTotal,
			"cargos_completed_by_my_drivers": cargosCompletedByMyDrivers,
			"contracts_total":                offersAccepted,
			"contracts_successful":           tripsCompleted,
			"contracts_cancelled":            tripsCancelled,
			"contracts_in_execution":         tripsInExecution,
			"offers_total":                   offersTotal,
			"offers_accepted":                offersAccepted,
			"offers_rejected":                offersRejected,
			"offers_canceled":                offersCanceled,
			"offers_waiting_driver_confirm":  offersWaiting,
			"offer_accept_rate_pct":          offerAcceptRate,
			"trips_total":                    tripsTotal,
			"trips_in_progress":              tripsInProgress,
			"trips_in_transit":               tripsInTransit,
			"trips_delivered":                tripsDelivered,
			"trips_completed":                tripsCompleted,
			"trips_cancelled":                tripsCancelled,
			"trips_in_execution":             tripsInExecution,
			"trip_completion_rate_pct":       tripCompletionRate,
			"completed_value":                completedValue,
		},
		"funnel": gin.H{
			"offers_total":                      offersTotal,
			"offers_accepted":                   offersAccepted,
			"trips_in_execution":                tripsInExecution,
			"trips_completed":                   tripsCompleted,
			"trips_cancelled":                   tripsCancelled,
			"conversion_offer_to_completed_pct": tripCompletionRate,
		},
		"timeline": gin.H{"items": trends},
		"top":      gin.H{"drivers": topDrivers},
	}, nil
}

func writeAnalyticsExcel(filenamePrefix string, payload gin.H, opts analyticsExportOptions) ([]byte, string, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	summarySheet := "Summary"
	_ = f.SetSheetName("Sheet1", summarySheet)

	writeKV := func(sheet string, startRow int, m gin.H) int {
		row := startRow
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		for _, k := range keys {
			_ = f.SetCellValue(sheet, "A"+strconv.Itoa(row), k)
			_ = f.SetCellValue(sheet, "B"+strconv.Itoa(row), fmt.Sprintf("%v", m[k]))
			row++
		}
		return row
	}

	if opts.Sections["summary"] {
		if filters, ok := payload["filters"].(gin.H); ok {
			_ = f.SetCellValue(summarySheet, "A1", "Filters")
			writeKV(summarySheet, 2, filters)
		}
	}
	if opts.Sections["kpi"] {
		if kpi, ok := payload["kpi"].(gin.H); ok {
			_ = f.SetCellValue(summarySheet, "D1", "KPI")
			row := 2
			for k, v := range kpi {
				_ = f.SetCellValue(summarySheet, "D"+strconv.Itoa(row), k)
				_ = f.SetCellValue(summarySheet, "E"+strconv.Itoa(row), fmt.Sprintf("%v", v))
				row++
			}
		}
	}
	if opts.Sections["funnel"] {
		if funnel, ok := payload["funnel"].(gin.H); ok {
			_ = f.SetCellValue(summarySheet, "G1", "Funnel")
			row := 2
			for k, v := range funnel {
				_ = f.SetCellValue(summarySheet, "G"+strconv.Itoa(row), k)
				_ = f.SetCellValue(summarySheet, "H"+strconv.Itoa(row), fmt.Sprintf("%v", v))
				row++
			}
		}
	}
	if !opts.Sections["summary"] && !opts.Sections["kpi"] && !opts.Sections["funnel"] {
		_ = f.DeleteSheet(summarySheet)
	}

	if opts.Sections["timeline"] {
		if timelineWrap, ok := payload["timeline"].(gin.H); ok {
			if itemsAny, ok := timelineWrap["items"]; ok {
				if items, ok := itemsAny.([]gin.H); ok {
					sheet := "Timeline"
					_, _ = f.NewSheet(sheet)
					if len(items) > 0 {
						colOrder := make([]string, 0, len(items[0]))
						for k := range items[0] {
							colOrder = append(colOrder, k)
						}
						for i, col := range colOrder {
							cell, _ := excelize.CoordinatesToCellName(i+1, 1)
							_ = f.SetCellValue(sheet, cell, col)
						}
						for r, item := range items {
							for i, col := range colOrder {
								cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
								_ = f.SetCellValue(sheet, cell, fmt.Sprintf("%v", item[col]))
							}
						}
					}
				}
			}
		}
	}

	if opts.Sections["top"] {
		if topWrap, ok := payload["top"].(gin.H); ok {
			for key, rowsAny := range topWrap {
				rows, ok := rowsAny.([]gin.H)
				if !ok {
					continue
				}
				sheet := "Top_" + strings.Title(strings.ReplaceAll(key, "_", " "))
				_, _ = f.NewSheet(sheet)
				if len(rows) == 0 {
					continue
				}
				colOrder := make([]string, 0, len(rows[0]))
				for k := range rows[0] {
					colOrder = append(colOrder, k)
				}
				for i, col := range colOrder {
					cell, _ := excelize.CoordinatesToCellName(i+1, 1)
					_ = f.SetCellValue(sheet, cell, col)
				}
				for r, item := range rows {
					for i, col := range colOrder {
						cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
						_ = f.SetCellValue(sheet, cell, fmt.Sprintf("%v", item[col]))
					}
				}
			}
		}
	}

	if opts.Sections["contracts"] {
		if rowsAny, ok := payload["contracts"]; ok {
			if rows, ok := rowsAny.([]gin.H); ok {
				sheet := "Contracts"
				_, _ = f.NewSheet(sheet)
				if len(rows) > 0 {
					colOrder := make([]string, 0, len(rows[0]))
					for k := range rows[0] {
						colOrder = append(colOrder, k)
					}
					for i, col := range colOrder {
						cell, _ := excelize.CoordinatesToCellName(i+1, 1)
						_ = f.SetCellValue(sheet, cell, col)
					}
					for r, item := range rows {
						for i, col := range colOrder {
							cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
							_ = f.SetCellValue(sheet, cell, fmt.Sprintf("%v", item[col]))
						}
					}
				}
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("%s_%s.xlsx", filenamePrefix, time.Now().UTC().Format("20060102_150405"))
	return buf.Bytes(), name, nil
}

func contractScopeClause(scope string) string {
	switch scope {
	case "successful":
		return " AND t.status = 'COMPLETED' "
	case "cancelled":
		return " AND t.status = 'CANCELLED' "
	case "in_execution":
		return " AND t.status IN ('IN_PROGRESS','IN_TRANSIT','DELIVERED') "
	default:
		return ""
	}
}

func (h *DispatcherAnalyticsHandler) cmContractsForExport(ctx context.Context, dispatcherID uuid.UUID, f analyticsFilter, scope string) ([]gin.H, error) {
	args := []any{dispatcherID, f.DateFrom, f.DateTo}
	sql := `
SELECT t.id, t.status, t.agreed_price, t.agreed_currency, t.created_at, t.updated_at,
       c.id AS cargo_id, c.status AS cargo_status, o.id AS offer_id, COALESCE(o.proposed_by,'') AS proposed_by,
       t.driver_id
FROM trips t
JOIN cargo c ON c.id = t.cargo_id
JOIN offers o ON o.id = t.offer_id
WHERE c.created_by_id = $1
  AND c.deleted_at IS NULL
  AND c.created_by_type = 'DISPATCHER'
  AND t.created_at >= $2 AND t.created_at < $3 ` + contractScopeClause(scope) + `
ORDER BY t.created_at DESC
LIMIT 5000`
	rows, err := h.pg.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		var tripID, cargoID, offerID uuid.UUID
		var status, cargoStatus, proposedBy, currency string
		var driverID *uuid.UUID
		var agreed float64
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&tripID, &status, &agreed, &currency, &createdAt, &updatedAt, &cargoID, &cargoStatus, &offerID, &proposedBy, &driverID); err != nil {
			return nil, err
		}
		row := gin.H{
			"trip_id":         tripID.String(),
			"trip_status":     status,
			"agreed_price":    agreed,
			"agreed_currency": currency,
			"created_at":      createdAt.UTC().Format(time.RFC3339),
			"updated_at":      updatedAt.UTC().Format(time.RFC3339),
			"cargo_id":        cargoID.String(),
			"cargo_status":    cargoStatus,
			"offer_id":        offerID.String(),
			"proposed_by":     proposedBy,
			"driver_id":       nil,
		}
		if driverID != nil {
			row["driver_id"] = driverID.String()
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (h *DispatcherAnalyticsHandler) dmContractsForExport(ctx context.Context, dispatcherID uuid.UUID, f analyticsFilter, scope string) ([]gin.H, error) {
	args := []any{dispatcherID, f.DateFrom, f.DateTo}
	sql := `
SELECT t.id, t.status, t.agreed_price, t.agreed_currency, t.created_at, t.updated_at,
       c.id AS cargo_id, c.status AS cargo_status, o.id AS offer_id, COALESCE(o.proposed_by,'') AS proposed_by,
       t.driver_id
FROM trips t
JOIN offers o ON o.id = t.offer_id
JOIN cargo c ON c.id = t.cargo_id
WHERE ((o.proposed_by = 'DRIVER_MANAGER' AND o.proposed_by_id = $1) OR o.negotiation_dispatcher_id = $1)
  AND t.created_at >= $2 AND t.created_at < $3 ` + contractScopeClause(scope) + `
ORDER BY t.created_at DESC
LIMIT 5000`
	rows, err := h.pg.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		var tripID, cargoID, offerID uuid.UUID
		var status, cargoStatus, proposedBy, currency string
		var driverID *uuid.UUID
		var agreed float64
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&tripID, &status, &agreed, &currency, &createdAt, &updatedAt, &cargoID, &cargoStatus, &offerID, &proposedBy, &driverID); err != nil {
			return nil, err
		}
		row := gin.H{
			"trip_id":         tripID.String(),
			"trip_status":     status,
			"agreed_price":    agreed,
			"agreed_currency": currency,
			"created_at":      createdAt.UTC().Format(time.RFC3339),
			"updated_at":      updatedAt.UTC().Format(time.RFC3339),
			"cargo_id":        cargoID.String(),
			"cargo_status":    cargoStatus,
			"offer_id":        offerID.String(),
			"proposed_by":     proposedBy,
			"driver_id":       nil,
		}
		if driverID != nil {
			row["driver_id"] = driverID.String()
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (h *DispatcherAnalyticsHandler) CargoManagerDashboard(c *gin.Context) {
	dispatcherID, ok := h.ensureRole(c, dispatchers.ManagerRoleCargoManager)
	if !ok {
		return
	}
	filter, ok := parseAnalyticsFilter(c)
	if !ok {
		return
	}
	data, err := h.cargoManagerDashboardData(c.Request.Context(), dispatcherID, filter)
	if err != nil {
		h.logger.Error("cargo manager analytics dashboard", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", data)
}

func (h *DispatcherAnalyticsHandler) CargoManagerExportExcel(c *gin.Context) {
	dispatcherID, ok := h.ensureRole(c, dispatchers.ManagerRoleCargoManager)
	if !ok {
		return
	}
	filter, ok := parseAnalyticsFilter(c)
	if !ok {
		return
	}
	data, err := h.cargoManagerDashboardData(c.Request.Context(), dispatcherID, filter)
	if err != nil {
		h.logger.Error("cargo manager analytics export", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	opts := parseAnalyticsExportOptions(c)
	contracts, err := h.cmContractsForExport(c.Request.Context(), dispatcherID, filter, opts.ContractScope)
	if err != nil {
		h.logger.Error("cargo manager analytics contracts export", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	data["contracts"] = contracts
	bytes, fname, err := writeAnalyticsExcel("cargo_manager_analytics", data, opts)
	if err != nil {
		h.logger.Error("cargo manager analytics excel", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="`+fname+`"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", bytes)
}

func (h *DispatcherAnalyticsHandler) DriverManagerDashboard(c *gin.Context) {
	dispatcherID, ok := h.ensureRole(c, dispatchers.ManagerRoleDriverManager)
	if !ok {
		return
	}
	filter, ok := parseAnalyticsFilter(c)
	if !ok {
		return
	}
	data, err := h.driverManagerDashboardData(c.Request.Context(), dispatcherID, filter)
	if err != nil {
		h.logger.Error("driver manager analytics dashboard", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", data)
}

func (h *DispatcherAnalyticsHandler) DriverManagerExportExcel(c *gin.Context) {
	dispatcherID, ok := h.ensureRole(c, dispatchers.ManagerRoleDriverManager)
	if !ok {
		return
	}
	filter, ok := parseAnalyticsFilter(c)
	if !ok {
		return
	}
	data, err := h.driverManagerDashboardData(c.Request.Context(), dispatcherID, filter)
	if err != nil {
		h.logger.Error("driver manager analytics export", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	opts := parseAnalyticsExportOptions(c)
	contracts, err := h.dmContractsForExport(c.Request.Context(), dispatcherID, filter, opts.ContractScope)
	if err != nil {
		h.logger.Error("driver manager analytics contracts export", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	data["contracts"] = contracts
	bytes, fname, err := writeAnalyticsExcel("driver_manager_analytics", data, opts)
	if err != nil {
		h.logger.Error("driver manager analytics excel", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="`+fname+`"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", bytes)
}
