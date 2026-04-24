package adminanalytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

func (r *Repo) TrackEvent(ctx context.Context, in EventInput) error {
	if r == nil || r.pg == nil {
		return nil
	}
	if strings.TrimSpace(in.EventName) == "" {
		return errors.New("analytics event name is required")
	}
	if in.EventTime.IsZero() {
		in.EventTime = time.Now().UTC()
	}
	in.Role = NormalizeRole(in.Role)
	if in.Role == "" {
		in.Role = RoleUnknown
	}
	meta := in.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
INSERT INTO analytics_events (
  event_name,
  event_time_utc,
  session_id,
  user_id,
  role,
  entity_type,
  entity_id,
  actor_id,
  device_type,
  platform,
  ip_hash,
  geo_city,
  metadata
)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
`, strings.TrimSpace(in.EventName), in.EventTime.UTC(), nullableString(in.SessionID), in.UserID, in.Role, nullableString(in.EntityType), in.EntityID, in.ActorID, nullableString(in.DeviceType), nullableString(in.Platform), nullableString(in.IPHash), nullableString(in.GeoCity), metaJSON)
	if err != nil {
		return err
	}

	if err := r.applyRollupsTx(ctx, tx, in); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repo) applyRollupsTx(ctx context.Context, tx pgx.Tx, in EventInput) error {
	day := in.EventTime.UTC().Format("2006-01-02")
	scopeUserID := ScopeUserID(in.UserID)

	if strings.TrimSpace(in.GeoCity) != "" {
		_, err := tx.Exec(ctx, `
INSERT INTO geo_stats (day_utc, geo_city, role, user_id, event_count, login_count, updated_at)
VALUES ($1,$2,$3,$4,1,$5,now())
ON CONFLICT (day_utc, geo_city, role, user_id)
DO UPDATE SET
  event_count = geo_stats.event_count + 1,
  login_count = geo_stats.login_count + EXCLUDED.login_count,
  updated_at = now()
`, day, strings.TrimSpace(in.GeoCity), in.Role, scopeUserID, boolToInt(in.EventName == EventLoginSuccess))
		if err != nil {
			return err
		}
	}

	switch in.EventName {
	case EventLoginSuccess:
		if in.UserID == nil || *in.UserID == uuid.Nil {
			return nil
		}
		_, err := tx.Exec(ctx, `
INSERT INTO user_login_stats (
  user_id, role, total_logins, successful_logins, failed_logins, last_login_at_utc,
  total_session_duration_seconds, completed_sessions_count, avg_session_duration_seconds, updated_at
)
VALUES ($1,$2,1,1,0,$3,0,0,0,now())
ON CONFLICT (user_id, role)
DO UPDATE SET
  total_logins = user_login_stats.total_logins + 1,
  successful_logins = user_login_stats.successful_logins + 1,
  last_login_at_utc = GREATEST(user_login_stats.last_login_at_utc, EXCLUDED.last_login_at_utc),
  updated_at = now()
`, *in.UserID, in.Role, in.EventTime.UTC())
		return err
	case EventLoginFailed:
		if in.UserID == nil || *in.UserID == uuid.Nil {
			return nil
		}
		_, err := tx.Exec(ctx, `
INSERT INTO user_login_stats (
  user_id, role, total_logins, successful_logins, failed_logins, last_login_at_utc,
  total_session_duration_seconds, completed_sessions_count, avg_session_duration_seconds, updated_at
)
VALUES ($1,$2,0,0,1,NULL,0,0,0,now())
ON CONFLICT (user_id, role)
DO UPDATE SET
  failed_logins = user_login_stats.failed_logins + 1,
  updated_at = now()
`, *in.UserID, in.Role)
		return err
	case EventSessionStarted:
		if strings.TrimSpace(in.SessionID) == "" || in.UserID == nil || *in.UserID == uuid.Nil {
			return nil
		}
		_, err := tx.Exec(ctx, `
INSERT INTO sessions (
  session_id, user_id, role, started_at_utc, last_seen_at_utc, device_type, platform, ip_hash, geo_city, metadata
)
VALUES ($1,$2,$3,$4,$4,$5,$6,$7,$8,$9)
ON CONFLICT (session_id)
DO UPDATE SET
  user_id = EXCLUDED.user_id,
  role = EXCLUDED.role,
  last_seen_at_utc = EXCLUDED.last_seen_at_utc,
  device_type = COALESCE(EXCLUDED.device_type, sessions.device_type),
  platform = COALESCE(EXCLUDED.platform, sessions.platform),
  ip_hash = COALESCE(EXCLUDED.ip_hash, sessions.ip_hash),
  geo_city = COALESCE(EXCLUDED.geo_city, sessions.geo_city),
  metadata = COALESCE(EXCLUDED.metadata, sessions.metadata)
`, in.SessionID, *in.UserID, in.Role, in.EventTime.UTC(), nullableString(in.DeviceType), nullableString(in.Platform), nullableString(in.IPHash), nullableString(in.GeoCity), mustJSON(in.Metadata))
		return err
	case EventSessionEnded:
		if strings.TrimSpace(in.SessionID) == "" {
			return nil
		}
		var userID uuid.UUID
		var role string
		var startedAt time.Time
		if err := tx.QueryRow(ctx, `
SELECT user_id, role, started_at_utc
FROM sessions
WHERE session_id = $1
`, in.SessionID).Scan(&userID, &role, &startedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		endedAt := in.EventTime.UTC()
		duration := int64(0)
		if endedAt.After(startedAt) {
			duration = int64(endedAt.Sub(startedAt).Seconds())
		}
		_, err := tx.Exec(ctx, `
UPDATE sessions
SET ended_at_utc = COALESCE(ended_at_utc, $2),
    last_seen_at_utc = GREATEST(COALESCE(last_seen_at_utc, $2), $2),
    duration_seconds = GREATEST(COALESCE(duration_seconds, 0), $3),
    metadata = CASE WHEN $4::jsonb IS NULL THEN metadata ELSE COALESCE(metadata, '{}'::jsonb) || $4::jsonb END
WHERE session_id = $1
`, in.SessionID, endedAt, duration, mustJSONPtr(in.Metadata))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
INSERT INTO user_login_stats (
  user_id, role, total_logins, successful_logins, failed_logins, last_login_at_utc,
  total_session_duration_seconds, completed_sessions_count, avg_session_duration_seconds, updated_at
)
VALUES ($1,$2,0,0,0,NULL,$3,1,$3,now())
ON CONFLICT (user_id, role)
DO UPDATE SET
  total_session_duration_seconds = user_login_stats.total_session_duration_seconds + EXCLUDED.total_session_duration_seconds,
  completed_sessions_count = user_login_stats.completed_sessions_count + 1,
  avg_session_duration_seconds =
    CASE
      WHEN user_login_stats.completed_sessions_count + 1 <= 0 THEN 0
      ELSE (user_login_stats.total_session_duration_seconds + EXCLUDED.total_session_duration_seconds)::double precision /
           (user_login_stats.completed_sessions_count + 1)::double precision
    END,
  updated_at = now()
`, userID, NormalizeRole(role), duration)
		return err
	case EventCargoCreated:
		return r.bumpCargoStatsTx(ctx, tx, day, in.Role, in.UserID, 1, 0, 0)
	case EventOfferCreated:
		return r.bumpOfferStatsTx(ctx, tx, day, in.Role, in.UserID, 1, 0, 0, 0, 0)
	case EventOfferAccepted:
		return r.bumpOfferStatsTx(ctx, tx, day, in.Role, in.UserID, 0, 1, 0, 0, 0)
	case EventTripStarted:
		return r.bumpTripStatsTx(ctx, tx, day, in.Role, in.UserID, 1, 0, 0)
	case EventTripCompleted:
		return r.bumpTripStatsTx(ctx, tx, day, in.Role, in.UserID, 0, 1, 0)
	case EventCallStarted:
		return r.bumpTripStatsTx(ctx, tx, day, RoleAdmin, nil, 0, 0, 0)
	default:
		return nil
	}
}

func (r *Repo) bumpCargoStatsTx(ctx context.Context, tx pgx.Tx, day, role string, userID *uuid.UUID, createdCount, completedCount, cancelledCount int) error {
	_, err := tx.Exec(ctx, `
INSERT INTO cargo_stats (day_utc, role, user_id, created_count, completed_count, cancelled_count, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,now())
ON CONFLICT (day_utc, role, user_id)
DO UPDATE SET
  created_count = cargo_stats.created_count + EXCLUDED.created_count,
  completed_count = cargo_stats.completed_count + EXCLUDED.completed_count,
  cancelled_count = cargo_stats.cancelled_count + EXCLUDED.cancelled_count,
  updated_at = now()
`, day, role, ScopeUserID(userID), createdCount, completedCount, cancelledCount)
	return err
}

func (r *Repo) bumpOfferStatsTx(ctx context.Context, tx pgx.Tx, day, role string, userID *uuid.UUID, createdCount, acceptedCount, rejectedCount, canceledCount, waitingCount int) error {
	_, err := tx.Exec(ctx, `
INSERT INTO offer_stats (day_utc, role, user_id, created_count, accepted_count, rejected_count, canceled_count, waiting_driver_confirm_count, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
ON CONFLICT (day_utc, role, user_id)
DO UPDATE SET
  created_count = offer_stats.created_count + EXCLUDED.created_count,
  accepted_count = offer_stats.accepted_count + EXCLUDED.accepted_count,
  rejected_count = offer_stats.rejected_count + EXCLUDED.rejected_count,
  canceled_count = offer_stats.canceled_count + EXCLUDED.canceled_count,
  waiting_driver_confirm_count = offer_stats.waiting_driver_confirm_count + EXCLUDED.waiting_driver_confirm_count,
  updated_at = now()
`, day, role, ScopeUserID(userID), createdCount, acceptedCount, rejectedCount, canceledCount, waitingCount)
	return err
}

func (r *Repo) bumpTripStatsTx(ctx context.Context, tx pgx.Tx, day, role string, userID *uuid.UUID, startedCount, completedCount, cancelledCount int) error {
	_, err := tx.Exec(ctx, `
INSERT INTO trip_stats (day_utc, role, user_id, started_count, completed_count, cancelled_count, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,now())
ON CONFLICT (day_utc, role, user_id)
DO UPDATE SET
  started_count = trip_stats.started_count + EXCLUDED.started_count,
  completed_count = trip_stats.completed_count + EXCLUDED.completed_count,
  cancelled_count = trip_stats.cancelled_count + EXCLUDED.cancelled_count,
  updated_at = now()
`, day, role, ScopeUserID(userID), startedCount, completedCount, cancelledCount)
	return err
}

func (r *Repo) Dashboard(ctx context.Context, w TimeWindow, role string) (map[string]any, error) {
	args := []any{w.From.UTC(), w.To.UTC()}
	roleFilterUsers := ""
	roleFilterEvents := ""
	if NormalizeRole(role) != "" && NormalizeRole(role) != RoleUnknown {
		args = append(args, NormalizeRole(role))
		roleFilterUsers = " WHERE role = $" + strconv.Itoa(len(args))
		roleFilterEvents = " AND role = $" + strconv.Itoa(len(args))
	}

	var cargoCount, offerCount, tripCount, completedTrips, cancelledTrips, registeredUsers, loginSuccess, loginFailed int64
	err := r.pg.QueryRow(ctx, `
WITH users AS (
  SELECT id::uuid AS id, 'driver'::text AS role, created_at FROM drivers
  UNION ALL
  SELECT id::uuid AS id,
         CASE
           WHEN UPPER(COALESCE(manager_role, '')) = 'CARGO_MANAGER' THEN 'cargo_manager'
           WHEN UPPER(COALESCE(manager_role, '')) = 'DRIVER_MANAGER' THEN 'driver_manager'
           ELSE 'unknown'
         END AS role,
         created_at
  FROM freelance_dispatchers
  WHERE deleted_at IS NULL
  UNION ALL
  SELECT id::uuid AS id, 'admin'::text AS role, now() AS created_at FROM admins
),
scoped_users AS (
  SELECT * FROM users `+roleFilterUsers+`
)
SELECT
  (SELECT COUNT(*) FROM cargo WHERE created_at >= $1 AND created_at < $2),
  (SELECT COUNT(*) FROM offers WHERE created_at >= $1 AND created_at < $2),
  (SELECT COUNT(*) FROM trips WHERE created_at >= $1 AND created_at < $2),
  (SELECT COUNT(*) FROM trips WHERE status = 'COMPLETED' AND updated_at >= $1 AND updated_at < $2),
  (SELECT COUNT(*) FROM trips WHERE status = 'CANCELLED' AND updated_at >= $1 AND updated_at < $2),
  (SELECT COUNT(*) FROM scoped_users WHERE created_at >= $1 AND created_at < $2),
  (SELECT COUNT(*) FROM analytics_events WHERE event_name = 'login_success' AND event_time_utc >= $1 AND event_time_utc < $2`+roleFilterEvents+`),
  (SELECT COUNT(*) FROM analytics_events WHERE event_name = 'login_failed' AND event_time_utc >= $1 AND event_time_utc < $2`+roleFilterEvents+`)
`, args...).Scan(&cargoCount, &offerCount, &tripCount, &completedTrips, &cancelledTrips, &registeredUsers, &loginSuccess, &loginFailed)
	if err != nil {
		return nil, err
	}

	offerAcceptedCount, err := r.countOfferAccepted(ctx, w, role)
	if err != nil {
		return nil, err
	}
	geoRows, err := r.Geo(ctx, w, role, Page{Limit: 10})
	if err != nil {
		return nil, err
	}
	roleRows, err := r.roleBreakdown(ctx)
	if err != nil {
		return nil, err
	}
	funnel, err := r.Funnels(ctx, w, role)
	if err != nil {
		return nil, err
	}
	prevWindow := TimeWindow{From: w.From.Add(-w.To.Sub(w.From)), To: w.From, TZ: w.TZ}
	prevCargo, prevOffers, prevTrips, err := r.compareWindowCounts(ctx, prevWindow)
	if err != nil {
		return nil, err
	}

	completionRate := ratioPct(completedTrips, tripCount)
	cancellationRate := ratioPct(cancelledTrips, tripCount)
	offerAcceptanceRate := ratioPct(offerAcceptedCount, offerCount)

	alerts := make([]map[string]any, 0, 4)
	if cancellationRate >= 20 {
		alerts = append(alerts, map[string]any{
			"severity": "high",
			"code":     "trip_cancellation_rate_high",
			"message":  "Trip cancellation rate is above 20%",
			"value":    cancellationRate,
		})
	}
	if loginFailed > loginSuccess && loginFailed > 0 {
		alerts = append(alerts, map[string]any{
			"severity": "medium",
			"code":     "login_failures_spike",
			"message":  "Failed logins exceed successful logins in the selected window",
			"value":    loginFailed,
		})
	}
	if tripCount > 0 && completionRate < 40 {
		alerts = append(alerts, map[string]any{
			"severity": "medium",
			"code":     "trip_completion_low",
			"message":  "Trip completion rate is below 40%",
			"value":    completionRate,
		})
	}

	return map[string]any{
		"kpi": map[string]any{
			"cargo_count":           cargoCount,
			"offer_count":           offerCount,
			"trip_count":            tripCount,
			"completed_trip_count":  completedTrips,
			"cancelled_trip_count":  cancelledTrips,
			"registered_users":      registeredUsers,
			"login_success_count":   loginSuccess,
			"login_failed_count":    loginFailed,
			"completion_rate_pct":   completionRate,
			"cancellation_rate_pct": cancellationRate,
			"offer_acceptance_pct":  offerAcceptanceRate,
		},
		"funnel":         funnel,
		"role_breakdown": roleRows,
		"geo":            geoRows,
		"growth": map[string]any{
			"cargo_delta_pct": growthPct(prevCargo, cargoCount),
			"offer_delta_pct": growthPct(prevOffers, offerCount),
			"trip_delta_pct":  growthPct(prevTrips, tripCount),
		},
		"alerts": alerts,
	}, nil
}

func (r *Repo) Metrics(ctx context.Context, w TimeWindow, metricNames []string, groupBy, interval, role string, userID *uuid.UUID, page Page) ([]MetricPoint, error) {
	groupBy = strings.ToLower(strings.TrimSpace(groupBy))
	if groupBy == "" {
		groupBy = "time"
	}
	switch groupBy {
	case "time":
		return r.metricsByTime(ctx, w, metricNames, interval, role)
	case "role":
		return r.metricsByRole(ctx, w, metricNames)
	case "user":
		return r.metricsByUser(ctx, w, metricNames, role, userID, page)
	default:
		return nil, fmt.Errorf("unsupported group_by: %s", groupBy)
	}
}

func (r *Repo) metricsByTime(ctx context.Context, w TimeWindow, metricNames []string, interval, role string) ([]MetricPoint, error) {
	period := strings.ToLower(strings.TrimSpace(interval))
	switch period {
	case "hour", "day", "week", "month":
	default:
		period = "day"
	}
	acceptedCountMap, err := r.offerAcceptedBuckets(ctx, w, period, role)
	if err != nil {
		return nil, err
	}

	args := []any{w.From.UTC(), w.To.UTC()}
	roleFilter := ""
	if NormalizeRole(role) != "" && NormalizeRole(role) != RoleUnknown {
		args = append(args, NormalizeRole(role))
		roleFilter = " AND role = $" + strconv.Itoa(len(args))
	}

	rows, err := r.pg.Query(ctx, `
WITH buckets AS (
  SELECT generate_series(
    date_trunc('`+period+`', $1::timestamptz),
    date_trunc('`+period+`', ($2::timestamptz - interval '1 second')),
    interval '1 `+period+`'
  ) AS bucket
),
user_events AS (
  SELECT date_trunc('`+period+`', event_time_utc) AS bucket,
         COUNT(*) FILTER (WHERE event_name = 'login_success') AS login_success_count,
         COUNT(*) FILTER (WHERE event_name = 'login_failed') AS login_failed_count
  FROM analytics_events
  WHERE event_time_utc >= $1 AND event_time_utc < $2`+roleFilter+`
  GROUP BY 1
),
cargos AS (
  SELECT date_trunc('`+period+`', created_at) AS bucket, COUNT(*) AS cargo_count
  FROM cargo
  WHERE created_at >= $1 AND created_at < $2
  GROUP BY 1
),
offers AS (
  SELECT date_trunc('`+period+`', created_at) AS bucket, COUNT(*) AS offer_count
  FROM offers
  WHERE created_at >= $1 AND created_at < $2
  GROUP BY 1
),
trips AS (
  SELECT date_trunc('`+period+`', created_at) AS bucket, COUNT(*) AS trip_count
  FROM trips
  WHERE created_at >= $1 AND created_at < $2
  GROUP BY 1
),
trip_done AS (
  SELECT date_trunc('`+period+`', updated_at) AS bucket,
         COUNT(*) FILTER (WHERE status = 'COMPLETED') AS completed_trip_count,
         COUNT(*) FILTER (WHERE status = 'CANCELLED') AS cancelled_trip_count
  FROM trips
  WHERE updated_at >= $1 AND updated_at < $2
  GROUP BY 1
)
SELECT b.bucket,
       COALESCE(c.cargo_count, 0),
       COALESCE(o.offer_count, 0),
       COALESCE(t.trip_count, 0),
       COALESCE(td.completed_trip_count, 0),
       COALESCE(td.cancelled_trip_count, 0),
       COALESCE(ue.login_success_count, 0),
       COALESCE(ue.login_failed_count, 0)
FROM buckets b
LEFT JOIN cargos c ON c.bucket = b.bucket
LEFT JOIN offers o ON o.bucket = b.bucket
LEFT JOIN trips t ON t.bucket = b.bucket
LEFT JOIN trip_done td ON td.bucket = b.bucket
LEFT JOIN user_events ue ON ue.bucket = b.bucket
ORDER BY b.bucket ASC
`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MetricPoint, 0)
	for rows.Next() {
		var bucket time.Time
		var cargoCount, offerCount, tripCount, completedCount, cancelledCount, loginSuccessCount, loginFailedCount int64
		if err := rows.Scan(&bucket, &cargoCount, &offerCount, &tripCount, &completedCount, &cancelledCount, &loginSuccessCount, &loginFailedCount); err != nil {
			return nil, err
		}
		values := map[string]any{
			"cargo_count":          cargoCount,
			"offer_count":          offerCount,
			"trip_count":           tripCount,
			"completed_trip_count": completedCount,
			"cancelled_trip_count": cancelledCount,
			"login_success_count":  loginSuccessCount,
			"login_failed_count":   loginFailedCount,
			"offer_accepted_count": acceptedCountMap[bucket.UTC().Format(time.RFC3339)],
		}
		filterMetricValues(values, metricNames)
		out = append(out, MetricPoint{
			Bucket: bucket.UTC().Format(time.RFC3339),
			Values: values,
		})
	}
	return out, rows.Err()
}

func (r *Repo) metricsByRole(ctx context.Context, w TimeWindow, metricNames []string) ([]MetricPoint, error) {
	roleRows, err := r.roleBreakdown(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MetricPoint, 0, len(roleRows))
	for _, row := range roleRows {
		roleName, _ := row["role"].(string)
		dash, err := r.Dashboard(ctx, w, roleName)
		if err != nil {
			return nil, err
		}
		values, _ := dash["kpi"].(map[string]any)
		filterMetricValues(values, metricNames)
		out = append(out, MetricPoint{Role: roleName, Values: values})
	}
	return out, nil
}

func (r *Repo) metricsByUser(ctx context.Context, w TimeWindow, metricNames []string, role string, userID *uuid.UUID, page Page) ([]MetricPoint, error) {
	users, _, err := r.ListUsers(ctx, w, role, "", page)
	if err != nil {
		return nil, err
	}
	if userID != nil && *userID != uuid.Nil {
		filtered := users[:0]
		for _, u := range users {
			if u.ID == *userID {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	}
	out := make([]MetricPoint, 0, len(users))
	for _, u := range users {
		detail, err := r.GetUserDetails(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		values, _ := detail["metrics"].(map[string]any)
		filterMetricValues(values, metricNames)
		uid := u.ID
		out = append(out, MetricPoint{Role: u.Role, UserID: &uid, Values: values})
	}
	return out, nil
}

func (r *Repo) ListUsers(ctx context.Context, w TimeWindow, role, search string, page Page) ([]UserLookup, int64, error) {
	limit, offset := normalizePage(page)
	sortBy := strings.TrimSpace(page.SortBy)
	if sortBy == "" {
		sortBy = "registered_at"
	}
	sortDir := normalizeSortDir(page.SortDir)
	orderExpr := map[string]string{
		"registered_at": "registered_at",
		"last_seen_at":  "last_seen_at",
		"display_name":  "display_name",
		"role":          "role",
	}[sortBy]
	if orderExpr == "" {
		orderExpr = "registered_at"
	}

	args := []any{w.From.UTC(), w.To.UTC(), limit, offset}
	filters := []string{"registered_at >= $1", "registered_at < $2"}
	if nr := NormalizeRole(role); nr != "" && nr != RoleUnknown {
		args = append(args, nr)
		filters = append(filters, "role = $"+strconv.Itoa(len(args)))
	}
	if s := strings.TrimSpace(search); s != "" {
		args = append(args, "%"+strings.ToLower(s)+"%")
		filters = append(filters, "(LOWER(display_name) LIKE $"+strconv.Itoa(len(args))+" OR LOWER(phone_or_login) LIKE $"+strconv.Itoa(len(args))+")")
	}

	q := `
WITH users AS (
  SELECT
    id::uuid AS id,
    'driver'::text AS role,
    COALESCE(NULLIF(TRIM(name), ''), phone) AS display_name,
    phone AS phone_or_login,
    COALESCE(account_status, 'active') AS status,
    created_at AS registered_at,
    last_online_at AS last_seen_at,
    NULL::text AS manager_role,
    NULL::text AS admin_type,
    'drivers'::text AS primary_source
  FROM drivers
  UNION ALL
  SELECT
    id::uuid AS id,
    CASE
      WHEN UPPER(COALESCE(manager_role, '')) = 'CARGO_MANAGER' THEN 'cargo_manager'
      WHEN UPPER(COALESCE(manager_role, '')) = 'DRIVER_MANAGER' THEN 'driver_manager'
      ELSE 'unknown'
    END AS role,
    COALESCE(NULLIF(TRIM(name), ''), phone) AS display_name,
    phone AS phone_or_login,
    COALESCE(status, 'active') AS status,
    created_at AS registered_at,
    last_online_at AS last_seen_at,
    manager_role,
    NULL::text AS admin_type,
    'freelance_dispatchers'::text AS primary_source
  FROM freelance_dispatchers
  WHERE deleted_at IS NULL
  UNION ALL
  SELECT
    id::uuid AS id,
    'admin'::text AS role,
    name AS display_name,
    login AS phone_or_login,
    status,
    now() AS registered_at,
    NULL::timestamptz AS last_seen_at,
    NULL::text AS manager_role,
    type AS admin_type,
    'admins'::text AS primary_source
  FROM admins
),
filtered AS (
  SELECT * FROM users
  WHERE ` + strings.Join(filters, " AND ") + `
)
SELECT
  id, role, display_name, phone_or_login, status, registered_at, last_seen_at, manager_role, admin_type, primary_source,
  (SELECT COUNT(*) FROM filtered) AS total_count
FROM filtered
ORDER BY ` + orderExpr + ` ` + sortDir + ` NULLS LAST
LIMIT $3 OFFSET $4`
	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]UserLookup, 0, limit)
	var total int64
	for rows.Next() {
		var item UserLookup
		if err := rows.Scan(&item.ID, &item.Role, &item.DisplayName, &item.PhoneOrLogin, &item.Status, &item.RegisteredAt, &item.LastSeenAt, &item.ManagerRole, &item.AdminType, &item.PrimarySource, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (r *Repo) GetUserDetails(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	user, err := r.lookupUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	loginStats, err := r.userLoginStats(ctx, userID, user.Role)
	if err != nil {
		return nil, err
	}
	roleMetrics, err := r.userBusinessMetrics(ctx, user)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"user":    user,
		"logins":  loginStats,
		"metrics": roleMetrics,
	}, nil
}

func (r *Repo) lookupUser(ctx context.Context, userID uuid.UUID) (*UserLookup, error) {
	q := `
WITH users AS (
  SELECT
    id::uuid AS id,
    'driver'::text AS role,
    COALESCE(NULLIF(TRIM(name), ''), phone) AS display_name,
    phone AS phone_or_login,
    COALESCE(account_status, 'active') AS status,
    created_at AS registered_at,
    last_online_at AS last_seen_at,
    NULL::text AS manager_role,
    NULL::text AS admin_type,
    'drivers'::text AS primary_source
  FROM drivers
  UNION ALL
  SELECT
    id::uuid AS id,
    CASE
      WHEN UPPER(COALESCE(manager_role, '')) = 'CARGO_MANAGER' THEN 'cargo_manager'
      WHEN UPPER(COALESCE(manager_role, '')) = 'DRIVER_MANAGER' THEN 'driver_manager'
      ELSE 'unknown'
    END AS role,
    COALESCE(NULLIF(TRIM(name), ''), phone) AS display_name,
    phone AS phone_or_login,
    COALESCE(status, 'active') AS status,
    created_at AS registered_at,
    last_online_at AS last_seen_at,
    manager_role,
    NULL::text AS admin_type,
    'freelance_dispatchers'::text AS primary_source
  FROM freelance_dispatchers
  WHERE deleted_at IS NULL
  UNION ALL
  SELECT
    id::uuid AS id,
    'admin'::text AS role,
    name AS display_name,
    login AS phone_or_login,
    status,
    now() AS registered_at,
    NULL::timestamptz AS last_seen_at,
    NULL::text AS manager_role,
    type AS admin_type,
    'admins'::text AS primary_source
  FROM admins
)
SELECT id, role, display_name, phone_or_login, status, registered_at, last_seen_at, manager_role, admin_type, primary_source
FROM users
WHERE id = $1
LIMIT 1`
	var out UserLookup
	if err := r.pg.QueryRow(ctx, q, userID).Scan(&out.ID, &out.Role, &out.DisplayName, &out.PhoneOrLogin, &out.Status, &out.RegisteredAt, &out.LastSeenAt, &out.ManagerRole, &out.AdminType, &out.PrimarySource); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}
	return &out, nil
}

func (r *Repo) userLoginStats(ctx context.Context, userID uuid.UUID, role string) (map[string]any, error) {
	var totalLogins, successfulLogins, failedLogins, completedSessions int64
	var avgSession float64
	var lastLogin *time.Time
	err := r.pg.QueryRow(ctx, `
SELECT total_logins, successful_logins, failed_logins, avg_session_duration_seconds, last_login_at_utc, completed_sessions_count
FROM user_login_stats
WHERE user_id = $1 AND role = $2
`, userID, NormalizeRole(role)).Scan(&totalLogins, &successfulLogins, &failedLogins, &avgSession, &lastLogin, &completedSessions)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		totalLogins = 0
		successfulLogins = 0
		failedLogins = 0
		completedSessions = 0
		avgSession = 0
		lastLogin = nil
	}

	rows, err := r.pg.Query(ctx, `
SELECT session_id, started_at_utc, ended_at_utc, duration_seconds, device_type, platform, geo_city, ip_hash
FROM sessions
WHERE user_id = $1 AND role = $2
ORDER BY started_at_utc DESC
LIMIT 20
`, userID, NormalizeRole(role))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	recent := make([]map[string]any, 0, 20)
	for rows.Next() {
		var sessionID string
		var startedAt time.Time
		var endedAt *time.Time
		var durationSeconds int64
		var deviceType, platform, geoCity, ipHash *string
		if err := rows.Scan(&sessionID, &startedAt, &endedAt, &durationSeconds, &deviceType, &platform, &geoCity, &ipHash); err != nil {
			return nil, err
		}
		recent = append(recent, map[string]any{
			"session_id":       sessionID,
			"started_at_utc":   startedAt.UTC().Format(time.RFC3339),
			"ended_at_utc":     formatTimePtr(endedAt),
			"duration_seconds": durationSeconds,
			"device_type":      strValue(deviceType),
			"platform":         strValue(platform),
			"geo_city":         strValue(geoCity),
			"ip_hash":          strValue(ipHash),
		})
	}
	return map[string]any{
		"total_logins":                 totalLogins,
		"successful_logins":            successfulLogins,
		"failed_logins":                failedLogins,
		"last_login_at_utc":            formatTimePtr(lastLogin),
		"average_session_duration_sec": avgSession,
		"completed_sessions":           completedSessions,
		"recent_sessions":              recent,
	}, rows.Err()
}

func (r *Repo) userBusinessMetrics(ctx context.Context, user *UserLookup) (map[string]any, error) {
	switch user.Role {
	case RoleDriver:
		return r.driverMetrics(ctx, user.ID)
	case RoleCargoManager:
		return r.cargoManagerMetrics(ctx, user.ID)
	case RoleDriverManager:
		return r.driverManagerMetrics(ctx, user.ID)
	case RoleAdmin:
		return r.adminMetrics(ctx, user.ID)
	default:
		return map[string]any{}, nil
	}
}

func (r *Repo) driverMetrics(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	var offersTotal, offersAccepted, tripsTotal, tripsCompleted, tripsCancelled, chatsTotal, messagesSent, callsTotal int64
	err := r.pg.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM offers WHERE carrier_id = $1),
  (SELECT COUNT(*) FROM analytics_events WHERE event_name = 'offer_accepted' AND user_id = $1),
  (SELECT COUNT(*) FROM trips WHERE driver_id = $1),
  (SELECT COUNT(*) FROM trips WHERE driver_id = $1 AND status = 'COMPLETED'),
  (SELECT COUNT(*) FROM trips WHERE driver_id = $1 AND status = 'CANCELLED'),
  (SELECT COUNT(*) FROM chat_conversations WHERE user_a_id = $1 OR user_b_id = $1),
  (SELECT COUNT(*) FROM chat_messages WHERE sender_id = $1 AND deleted_at IS NULL),
  (SELECT COUNT(*) FROM calls WHERE caller_id = $1 OR callee_id = $1)
`, userID).Scan(&offersTotal, &offersAccepted, &tripsTotal, &tripsCompleted, &tripsCancelled, &chatsTotal, &messagesSent, &callsTotal)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"offers_total":    offersTotal,
		"offers_accepted": offersAccepted,
		"trips_total":     tripsTotal,
		"trips_completed": tripsCompleted,
		"trips_cancelled": tripsCancelled,
		"completion_rate": ratioPct(tripsCompleted, tripsTotal),
		"chat_count":      chatsTotal,
		"messages_sent":   messagesSent,
		"calls_total":     callsTotal,
	}, nil
}

func (r *Repo) cargoManagerMetrics(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	var cargoCount, offerCount, tripCount, completedTrips, cancelledTrips int64
	err := r.pg.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM cargo WHERE created_by_id = $1 AND UPPER(COALESCE(created_by_type,'')) = 'DISPATCHER'),
  (SELECT COUNT(*) FROM offers WHERE proposed_by = 'DISPATCHER' AND proposed_by_id = $1),
  (SELECT COUNT(*) FROM trips t JOIN cargo c ON c.id = t.cargo_id WHERE c.created_by_id = $1),
  (SELECT COUNT(*) FROM trips t JOIN cargo c ON c.id = t.cargo_id WHERE c.created_by_id = $1 AND t.status = 'COMPLETED'),
  (SELECT COUNT(*) FROM trips t JOIN cargo c ON c.id = t.cargo_id WHERE c.created_by_id = $1 AND t.status = 'CANCELLED')
`, userID).Scan(&cargoCount, &offerCount, &tripCount, &completedTrips, &cancelledTrips)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"cargo_count":          cargoCount,
		"offer_count":          offerCount,
		"trip_count":           tripCount,
		"completed_trip_count": completedTrips,
		"cancelled_trip_count": cancelledTrips,
		"completion_rate":      ratioPct(completedTrips, tripCount),
	}, nil
}

func (r *Repo) driverManagerMetrics(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	var linkedDrivers, offerCount, tripsTotal, tripsCompleted, tripsCancelled int64
	err := r.pg.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM driver_manager_relations WHERE manager_id = $1),
  (SELECT COUNT(*) FROM offers WHERE proposed_by = 'DRIVER_MANAGER' AND proposed_by_id = $1),
  (SELECT COUNT(*) FROM trips t JOIN offers o ON o.id = t.offer_id WHERE o.proposed_by_id = $1 OR o.negotiation_dispatcher_id = $1),
  (SELECT COUNT(*) FROM trips t JOIN offers o ON o.id = t.offer_id WHERE (o.proposed_by_id = $1 OR o.negotiation_dispatcher_id = $1) AND t.status = 'COMPLETED'),
  (SELECT COUNT(*) FROM trips t JOIN offers o ON o.id = t.offer_id WHERE (o.proposed_by_id = $1 OR o.negotiation_dispatcher_id = $1) AND t.status = 'CANCELLED')
`, userID).Scan(&linkedDrivers, &offerCount, &tripsTotal, &tripsCompleted, &tripsCancelled)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"linked_drivers":       linkedDrivers,
		"offer_count":          offerCount,
		"trip_count":           tripsTotal,
		"completed_trip_count": tripsCompleted,
		"cancelled_trip_count": tripsCancelled,
		"completion_rate":      ratioPct(tripsCompleted, tripsTotal),
	}, nil
}

func (r *Repo) adminMetrics(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	var actionsCount, cargoModeratedCount, companiesCreatedCount int64
	err := r.pg.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM analytics_events WHERE event_name = 'admin_action_performed' AND user_id = $1),
  (SELECT COUNT(*) FROM analytics_events WHERE event_name = 'admin_action_performed' AND user_id = $1 AND entity_type = 'cargo'),
  (SELECT COUNT(*) FROM analytics_events WHERE event_name = 'admin_action_performed' AND user_id = $1 AND entity_type = 'company')
`, userID).Scan(&actionsCount, &cargoModeratedCount, &companiesCreatedCount)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"admin_actions":         actionsCount,
		"cargo_moderated_count": cargoModeratedCount,
		"companies_created":     companiesCreatedCount,
	}, nil
}

func (r *Repo) ListUserLogins(ctx context.Context, userID uuid.UUID, page Page) ([]map[string]any, int64, error) {
	limit, offset := normalizePage(page)
	rows, err := r.pg.Query(ctx, `
SELECT session_id, role, started_at_utc, ended_at_utc, duration_seconds, device_type, platform, geo_city, ip_hash,
       COUNT(*) OVER()
FROM sessions
WHERE user_id = $1
ORDER BY started_at_utc DESC
LIMIT $2 OFFSET $3
`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, limit)
	var total int64
	for rows.Next() {
		var sessionID, role string
		var startedAt time.Time
		var endedAt *time.Time
		var durationSeconds int64
		var deviceType, platform, geoCity, ipHash *string
		if err := rows.Scan(&sessionID, &role, &startedAt, &endedAt, &durationSeconds, &deviceType, &platform, &geoCity, &ipHash, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{
			"session_id":       sessionID,
			"role":             role,
			"started_at_utc":   startedAt.UTC().Format(time.RFC3339),
			"ended_at_utc":     formatTimePtr(endedAt),
			"duration_seconds": durationSeconds,
			"device_type":      strValue(deviceType),
			"platform":         strValue(platform),
			"geo_city":         strValue(geoCity),
			"ip_hash":          strValue(ipHash),
		})
	}
	return out, total, rows.Err()
}

func (r *Repo) Funnels(ctx context.Context, w TimeWindow, role string) (map[string]any, error) {
	cargoCount, offerCount, tripCount, completedCount, err := r.funnelCounts(ctx, w, role)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"stages": []map[string]any{
			{"stage": "cargo", "count": cargoCount, "conversion_from_prev_pct": 100.0, "dropoff_from_prev": 0},
			{"stage": "offer", "count": offerCount, "conversion_from_prev_pct": ratioPct(offerCount, cargoCount), "dropoff_from_prev": maxInt64(cargoCount-offerCount, 0)},
			{"stage": "trip", "count": tripCount, "conversion_from_prev_pct": ratioPct(tripCount, offerCount), "dropoff_from_prev": maxInt64(offerCount-tripCount, 0)},
			{"stage": "completed", "count": completedCount, "conversion_from_prev_pct": ratioPct(completedCount, tripCount), "dropoff_from_prev": maxInt64(tripCount-completedCount, 0)},
		},
	}, nil
}

func (r *Repo) Dropoff(ctx context.Context, w TimeWindow, role string) (map[string]any, error) {
	funnel, err := r.Funnels(ctx, w, role)
	if err != nil {
		return nil, err
	}
	stages, _ := funnel["stages"].([]map[string]any)
	return map[string]any{"items": stages}, nil
}

func (r *Repo) Retention(ctx context.Context, w TimeWindow, role string) ([]map[string]any, error) {
	users, _, err := r.ListUsers(ctx, w, role, "", Page{Limit: 5000})
	if err != nil {
		return nil, err
	}
	type cohortStats struct {
		total int64
		d1    int64
		d7    int64
		d30   int64
	}
	byCohort := map[string]*cohortStats{}
	for _, u := range users {
		cohortKey := u.RegisteredAt.UTC().Format("2006-01-02")
		if _, ok := byCohort[cohortKey]; !ok {
			byCohort[cohortKey] = &cohortStats{}
		}
		byCohort[cohortKey].total++

		rows, err := r.pg.Query(ctx, `
SELECT event_time_utc
FROM analytics_events
WHERE user_id = $1
  AND event_name IN ('login_success', 'offer_created', 'trip_started', 'chat_message_sent')
  AND event_time_utc > $2
  AND event_time_utc <= $3
ORDER BY event_time_utc ASC
`, u.ID, u.RegisteredAt.UTC(), u.RegisteredAt.UTC().Add(31*24*time.Hour))
		if err != nil {
			return nil, err
		}
		var d1, d7, d30 bool
		for rows.Next() {
			var eventAt time.Time
			if err := rows.Scan(&eventAt); err != nil {
				rows.Close()
				return nil, err
			}
			diffDays := int(eventAt.Sub(u.RegisteredAt.UTC()).Hours() / 24)
			if diffDays >= 1 {
				d1 = true
			}
			if diffDays >= 7 {
				d7 = true
			}
			if diffDays >= 30 {
				d30 = true
			}
		}
		rows.Close()
		if d1 {
			byCohort[cohortKey].d1++
		}
		if d7 {
			byCohort[cohortKey].d7++
		}
		if d30 {
			byCohort[cohortKey].d30++
		}
	}
	keys := make([]string, 0, len(byCohort))
	for k := range byCohort {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		s := byCohort[k]
		out = append(out, map[string]any{
			"cohort":           k,
			"registered_users": s.total,
			"day_1_rate_pct":   ratioPct(s.d1, s.total),
			"day_7_rate_pct":   ratioPct(s.d7, s.total),
			"day_30_rate_pct":  ratioPct(s.d30, s.total),
		})
	}
	return out, nil
}

func (r *Repo) FlowTime(ctx context.Context, role string) ([]FlowMetric, error) {
	type flowQuery struct {
		name        string
		description string
		sql         string
		args        []any
	}
	queries := []flowQuery{
		{
			name:        "registration_to_first_login",
			description: "Time from registration to first successful login",
			sql: `
WITH users AS (
  SELECT id::uuid AS user_id, 'driver'::text AS role, created_at AS registered_at FROM drivers
  UNION ALL
  SELECT id::uuid AS user_id,
         CASE
           WHEN UPPER(COALESCE(manager_role, '')) = 'CARGO_MANAGER' THEN 'cargo_manager'
           WHEN UPPER(COALESCE(manager_role, '')) = 'DRIVER_MANAGER' THEN 'driver_manager'
           ELSE 'unknown'
         END AS role,
         created_at AS registered_at
  FROM freelance_dispatchers
  WHERE deleted_at IS NULL
),
durations AS (
  SELECT EXTRACT(EPOCH FROM (MIN(s.started_at_utc) - u.registered_at)) AS duration_sec
  FROM users u
  JOIN sessions s ON s.user_id = u.user_id AND s.role = u.role
  WHERE u.role = COALESCE(NULLIF($1, ''), u.role)
  GROUP BY u.user_id, u.role, u.registered_at
)
SELECT COUNT(*), AVG(duration_sec), PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY duration_sec), PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_sec)
FROM durations
WHERE duration_sec IS NOT NULL AND duration_sec >= 0`,
			args: []any{NormalizeRole(role)},
		},
		{
			name:        "login_to_first_offer",
			description: "Time from first successful login to first offer created",
			sql: `
WITH user_base AS (
  SELECT user_id, role, MIN(started_at_utc) AS first_login_at
  FROM sessions
  WHERE role = COALESCE(NULLIF($1, ''), role)
  GROUP BY user_id, role
),
first_offer AS (
  SELECT proposed_by_id AS user_id,
         CASE
           WHEN proposed_by = 'DISPATCHER' THEN 'cargo_manager'
           WHEN proposed_by = 'DRIVER_MANAGER' THEN 'driver_manager'
           WHEN proposed_by = 'DRIVER' THEN 'driver'
           ELSE 'unknown'
         END AS role,
         MIN(created_at) AS first_offer_at
  FROM offers
  WHERE proposed_by_id IS NOT NULL
  GROUP BY proposed_by_id, role
),
durations AS (
  SELECT EXTRACT(EPOCH FROM (f.first_offer_at - u.first_login_at)) AS duration_sec
  FROM user_base u
  JOIN first_offer f ON f.user_id = u.user_id AND f.role = u.role
)
SELECT COUNT(*), AVG(duration_sec), PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY duration_sec), PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_sec)
FROM durations
WHERE duration_sec IS NOT NULL AND duration_sec >= 0`,
			args: []any{NormalizeRole(role)},
		},
		{
			name:        "offer_to_accepted",
			description: "Time from offer creation to offer acceptance",
			sql: `
WITH accepted AS (
  SELECT entity_id AS offer_id, MIN(event_time_utc) AS accepted_at
  FROM analytics_events
  WHERE event_name = 'offer_accepted'
  GROUP BY entity_id
),
durations AS (
  SELECT EXTRACT(EPOCH FROM (a.accepted_at - o.created_at)) AS duration_sec
  FROM offers o
  JOIN accepted a ON a.offer_id = o.id
  WHERE CASE
          WHEN o.proposed_by = 'DISPATCHER' THEN 'cargo_manager'
          WHEN o.proposed_by = 'DRIVER_MANAGER' THEN 'driver_manager'
          WHEN o.proposed_by = 'DRIVER' THEN 'driver'
          ELSE 'unknown'
        END = COALESCE(NULLIF($1, ''), CASE
          WHEN o.proposed_by = 'DISPATCHER' THEN 'cargo_manager'
          WHEN o.proposed_by = 'DRIVER_MANAGER' THEN 'driver_manager'
          WHEN o.proposed_by = 'DRIVER' THEN 'driver'
          ELSE 'unknown'
        END)
)
SELECT COUNT(*), AVG(duration_sec), PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY duration_sec), PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_sec)
FROM durations
WHERE duration_sec IS NOT NULL AND duration_sec >= 0`,
			args: []any{NormalizeRole(role)},
		},
		{
			name:        "accepted_to_trip_started",
			description: "Time from offer accepted to trip start",
			sql: `
WITH accepted AS (
  SELECT entity_id AS offer_id, MIN(event_time_utc) AS accepted_at
  FROM analytics_events
  WHERE event_name = 'offer_accepted'
  GROUP BY entity_id
),
durations AS (
  SELECT EXTRACT(EPOCH FROM (t.created_at - a.accepted_at)) AS duration_sec
  FROM trips t
  JOIN accepted a ON a.offer_id = t.offer_id
)
SELECT COUNT(*), AVG(duration_sec), PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY duration_sec), PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_sec)
FROM durations
WHERE duration_sec IS NOT NULL AND duration_sec >= 0`,
			args: []any{},
		},
		{
			name:        "trip_to_completed",
			description: "Time from trip start to completion",
			sql: `
WITH completed AS (
  SELECT entity_id AS trip_id, MIN(event_time_utc) AS completed_at
  FROM analytics_events
  WHERE event_name = 'trip_completed'
  GROUP BY entity_id
),
durations AS (
  SELECT EXTRACT(EPOCH FROM (COALESCE(c.completed_at, t.updated_at) - t.created_at)) AS duration_sec
  FROM trips t
  LEFT JOIN completed c ON c.trip_id = t.id
  WHERE t.status = 'COMPLETED'
)
SELECT COUNT(*), AVG(duration_sec), PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY duration_sec), PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_sec)
FROM durations
WHERE duration_sec IS NOT NULL AND duration_sec >= 0`,
			args: []any{},
		},
	}

	out := make([]FlowMetric, 0, len(queries))
	for _, q := range queries {
		var item FlowMetric
		item.Name = q.name
		item.Description = q.description
		var avg, median, p95 *float64
		if err := r.pg.QueryRow(ctx, q.sql, q.args...).Scan(&item.Count, &avg, &median, &p95); err != nil {
			return nil, err
		}
		item.AverageSec = avg
		item.MedianSec = median
		item.P95Sec = p95
		out = append(out, item)
	}
	return out, nil
}

func (r *Repo) FlowConversion(ctx context.Context, w TimeWindow, role string) (map[string]any, error) {
	cargoCount, offerCount, tripCount, completedCount, err := r.funnelCounts(ctx, w, role)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"registration_to_login_pct": nil,
		"cargo_to_offer_pct":        ratioPct(offerCount, cargoCount),
		"offer_to_trip_pct":         ratioPct(tripCount, offerCount),
		"trip_to_completed_pct":     ratioPct(completedCount, tripCount),
		"overall_cargo_to_done_pct": ratioPct(completedCount, cargoCount),
	}, nil
}

func (r *Repo) ListChats(ctx context.Context, userID *uuid.UUID, search string, page Page) ([]map[string]any, int64, error) {
	limit, offset := normalizePage(page)
	args := []any{limit, offset}
	filters := []string{"1=1"}
	if userID != nil && *userID != uuid.Nil {
		args = append(args, *userID)
		filters = append(filters, "(c.user_a_id = $"+strconv.Itoa(len(args))+" OR c.user_b_id = $"+strconv.Itoa(len(args))+")")
	}
	if s := strings.TrimSpace(search); s != "" {
		args = append(args, "%"+strings.ToLower(s)+"%")
		filters = append(filters, `(
      LOWER(COALESCE(ua.display_name, '')) LIKE $`+strconv.Itoa(len(args))+`
      OR LOWER(COALESCE(ub.display_name, '')) LIKE $`+strconv.Itoa(len(args))+`
      OR LOWER(COALESCE(lm.body, '')) LIKE $`+strconv.Itoa(len(args))+`
    )`)
	}
	q := `
WITH user_dim AS (
  SELECT id::uuid AS id, COALESCE(NULLIF(TRIM(name), ''), phone) AS display_name, 'driver'::text AS role FROM drivers
  UNION ALL
  SELECT id::uuid AS id, COALESCE(NULLIF(TRIM(name), ''), phone) AS display_name,
         CASE
           WHEN UPPER(COALESCE(manager_role, '')) = 'CARGO_MANAGER' THEN 'cargo_manager'
           WHEN UPPER(COALESCE(manager_role, '')) = 'DRIVER_MANAGER' THEN 'driver_manager'
           ELSE 'unknown'
         END AS role
  FROM freelance_dispatchers
  WHERE deleted_at IS NULL
  UNION ALL
  SELECT id::uuid AS id, name AS display_name, 'admin'::text AS role FROM admins
),
conv AS (
  SELECT c.id, c.user_a_id, c.user_b_id, c.created_at,
         ua.display_name AS user_a_name, ua.role AS user_a_role,
         ub.display_name AS user_b_name, ub.role AS user_b_role,
         lm.id AS last_message_id, lm.body AS last_message_body, lm.created_at AS last_message_at,
         (SELECT COUNT(*) FROM chat_messages m WHERE m.conversation_id = c.id AND m.deleted_at IS NULL) AS message_count
  FROM chat_conversations c
  LEFT JOIN user_dim ua ON ua.id = c.user_a_id
  LEFT JOIN user_dim ub ON ub.id = c.user_b_id
  LEFT JOIN LATERAL (
    SELECT id, body, created_at
    FROM chat_messages
    WHERE conversation_id = c.id AND deleted_at IS NULL
    ORDER BY created_at DESC
    LIMIT 1
  ) lm ON true
  WHERE ` + strings.Join(filters, " AND ") + `
)
SELECT id, user_a_id, user_b_id, created_at, user_a_name, user_a_role, user_b_name, user_b_role,
       last_message_id, last_message_body, last_message_at, message_count, COUNT(*) OVER()
FROM conv
ORDER BY COALESCE(last_message_at, created_at) DESC
LIMIT $1 OFFSET $2`
	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, limit)
	var total int64
	for rows.Next() {
		var id, userAID, userBID uuid.UUID
		var createdAt time.Time
		var userAName, userARole, userBName, userBRole string
		var lastMessageID *uuid.UUID
		var lastMessageBody *string
		var lastMessageAt *time.Time
		var messageCount int64
		if err := rows.Scan(&id, &userAID, &userBID, &createdAt, &userAName, &userARole, &userBName, &userBRole, &lastMessageID, &lastMessageBody, &lastMessageAt, &messageCount, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{
			"chat_id":             id.String(),
			"user_a_id":           userAID.String(),
			"user_a_name":         userAName,
			"user_a_role":         userARole,
			"user_b_id":           userBID.String(),
			"user_b_name":         userBName,
			"user_b_role":         userBRole,
			"created_at_utc":      createdAt.UTC().Format(time.RFC3339),
			"last_message_id":     uuidPtrString(lastMessageID),
			"last_message_body":   strValue(lastMessageBody),
			"last_message_at_utc": formatTimePtr(lastMessageAt),
			"message_count":       messageCount,
		})
	}
	return out, total, rows.Err()
}

func (r *Repo) ListChatMessages(ctx context.Context, chatID uuid.UUID, page Page) ([]map[string]any, int64, error) {
	limit, offset := normalizePage(page)
	rows, err := r.pg.Query(ctx, `
SELECT m.id, m.sender_id, m.type, m.body, m.payload, m.created_at, m.updated_at, m.deleted_at, COUNT(*) OVER()
FROM chat_messages m
WHERE m.conversation_id = $1
ORDER BY m.created_at DESC
LIMIT $2 OFFSET $3
`, chatID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, limit)
	var total int64
	for rows.Next() {
		var messageID, senderID uuid.UUID
		var msgType string
		var body *string
		var payload []byte
		var createdAt, updatedAt time.Time
		var deletedAt *time.Time
		if err := rows.Scan(&messageID, &senderID, &msgType, &body, &payload, &createdAt, &updatedAt, &deletedAt, &total); err != nil {
			return nil, 0, err
		}
		var payloadAny any
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &payloadAny)
		}
		out = append(out, map[string]any{
			"id":             messageID.String(),
			"sender_id":      senderID.String(),
			"type":           msgType,
			"body":           strValue(body),
			"payload":        payloadAny,
			"created_at_utc": createdAt.UTC().Format(time.RFC3339),
			"updated_at_utc": updatedAt.UTC().Format(time.RFC3339),
			"deleted_at_utc": formatTimePtr(deletedAt),
		})
	}
	return out, total, rows.Err()
}

func (r *Repo) ListCalls(ctx context.Context, status string, userID *uuid.UUID, page Page) ([]map[string]any, int64, error) {
	limit, offset := normalizePage(page)
	args := []any{limit, offset}
	filters := []string{"1=1"}
	if userID != nil && *userID != uuid.Nil {
		args = append(args, *userID)
		filters = append(filters, "(caller_id = $"+strconv.Itoa(len(args))+" OR callee_id = $"+strconv.Itoa(len(args))+")")
	}
	if s := strings.ToUpper(strings.TrimSpace(status)); s != "" {
		args = append(args, s)
		filters = append(filters, "status = $"+strconv.Itoa(len(args)))
	}
	rows, err := r.pg.Query(ctx, `
SELECT id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason,
       COUNT(*) OVER()
FROM calls
WHERE `+strings.Join(filters, " AND ")+`
ORDER BY created_at DESC
LIMIT $1 OFFSET $2
`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, limit)
	var total int64
	for rows.Next() {
		var id uuid.UUID
		var conversationID *uuid.UUID
		var callerID, calleeID uuid.UUID
		var callStatus string
		var createdAt time.Time
		var startedAt, endedAt *time.Time
		var endedBy *uuid.UUID
		var endedReason *string
		if err := rows.Scan(&id, &conversationID, &callerID, &calleeID, &callStatus, &createdAt, &startedAt, &endedAt, &endedBy, &endedReason, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{
			"id":               id.String(),
			"conversation_id":  uuidPtrString(conversationID),
			"caller_id":        callerID.String(),
			"callee_id":        calleeID.String(),
			"status":           callStatus,
			"created_at_utc":   createdAt.UTC().Format(time.RFC3339),
			"started_at_utc":   formatTimePtr(startedAt),
			"ended_at_utc":     formatTimePtr(endedAt),
			"ended_by":         uuidPtrString(endedBy),
			"ended_reason":     strValue(endedReason),
			"duration_seconds": durationSeconds(startedAt, endedAt),
		})
	}
	return out, total, rows.Err()
}

func (r *Repo) GetCall(ctx context.Context, callID uuid.UUID) (map[string]any, error) {
	var id uuid.UUID
	var conversationID *uuid.UUID
	var callerID, calleeID uuid.UUID
	var status string
	var createdAt time.Time
	var startedAt, endedAt *time.Time
	var endedBy *uuid.UUID
	var endedReason *string
	err := r.pg.QueryRow(ctx, `
SELECT id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason
FROM calls
WHERE id = $1
`, callID).Scan(&id, &conversationID, &callerID, &calleeID, &status, &createdAt, &startedAt, &endedAt, &endedBy, &endedReason)
	if err != nil {
		return nil, err
	}
	eventsRows, err := r.pg.Query(ctx, `
SELECT id, actor_id, event_type, payload, created_at
FROM call_events
WHERE call_id = $1
ORDER BY created_at ASC
`, callID)
	if err != nil {
		return nil, err
	}
	defer eventsRows.Close()
	events := make([]map[string]any, 0)
	for eventsRows.Next() {
		var eventID uuid.UUID
		var actorID *uuid.UUID
		var eventType string
		var payload []byte
		var created time.Time
		if err := eventsRows.Scan(&eventID, &actorID, &eventType, &payload, &created); err != nil {
			return nil, err
		}
		var payloadAny any
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &payloadAny)
		}
		events = append(events, map[string]any{
			"id":             eventID.String(),
			"actor_id":       uuidPtrString(actorID),
			"event_type":     eventType,
			"payload":        payloadAny,
			"created_at_utc": created.UTC().Format(time.RFC3339),
		})
	}
	return map[string]any{
		"id":               id.String(),
		"conversation_id":  uuidPtrString(conversationID),
		"caller_id":        callerID.String(),
		"callee_id":        calleeID.String(),
		"status":           status,
		"created_at_utc":   createdAt.UTC().Format(time.RFC3339),
		"started_at_utc":   formatTimePtr(startedAt),
		"ended_at_utc":     formatTimePtr(endedAt),
		"ended_by":         uuidPtrString(endedBy),
		"ended_reason":     strValue(endedReason),
		"duration_seconds": durationSeconds(startedAt, endedAt),
		"recording_url":    nil,
		"events":           events,
	}, nil
}

func (r *Repo) Geo(ctx context.Context, w TimeWindow, role string, page Page) ([]map[string]any, error) {
	limit := page.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	args := []any{w.From.UTC(), w.To.UTC(), limit}
	roleFilter := ""
	if nr := NormalizeRole(role); nr != "" && nr != RoleUnknown {
		args = append(args, nr)
		roleFilter = " AND role = $" + strconv.Itoa(len(args))
	}
	rows, err := r.pg.Query(ctx, `
SELECT COALESCE(NULLIF(TRIM(geo_city), ''), 'unknown') AS city,
       role,
       COUNT(*) AS events_count,
       COUNT(*) FILTER (WHERE event_name = 'login_success') AS login_count,
       COUNT(DISTINCT user_id) AS unique_users
FROM analytics_events
WHERE event_time_utc >= $1 AND event_time_utc < $2`+roleFilter+`
GROUP BY 1,2
ORDER BY events_count DESC, login_count DESC
LIMIT $3
`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, limit)
	for rows.Next() {
		var city, roleName string
		var eventsCount, loginCount, uniqueUsers int64
		if err := rows.Scan(&city, &roleName, &eventsCount, &loginCount, &uniqueUsers); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"geo_city":     city,
			"role":         roleName,
			"events_count": eventsCount,
			"login_count":  loginCount,
			"unique_users": uniqueUsers,
		})
	}
	return out, rows.Err()
}

func (r *Repo) GeoRealtime(ctx context.Context) ([]map[string]any, error) {
	rows, err := r.pg.Query(ctx, `
WITH latest_geo AS (
  SELECT DISTINCT ON (user_id, role)
         user_id, role, COALESCE(NULLIF(TRIM(geo_city), ''), 'unknown') AS geo_city, event_time_utc
  FROM analytics_events
  WHERE user_id IS NOT NULL
  ORDER BY user_id, role, event_time_utc DESC
),
current_users AS (
  SELECT id::uuid AS user_id, 'driver'::text AS role, last_online_at AS last_seen
  FROM drivers
  WHERE last_online_at IS NOT NULL AND last_online_at >= now() - interval '15 minutes'
  UNION ALL
  SELECT id::uuid AS user_id,
         CASE
           WHEN UPPER(COALESCE(manager_role, '')) = 'CARGO_MANAGER' THEN 'cargo_manager'
           WHEN UPPER(COALESCE(manager_role, '')) = 'DRIVER_MANAGER' THEN 'driver_manager'
           ELSE 'unknown'
         END AS role,
         last_online_at AS last_seen
  FROM freelance_dispatchers
  WHERE deleted_at IS NULL AND last_online_at IS NOT NULL AND last_online_at >= now() - interval '15 minutes'
)
SELECT cu.role, COALESCE(lg.geo_city, 'unknown') AS geo_city, COUNT(*) AS online_users, MAX(cu.last_seen) AS last_seen
FROM current_users cu
LEFT JOIN latest_geo lg ON lg.user_id = cu.user_id AND lg.role = cu.role
GROUP BY cu.role, COALESCE(lg.geo_city, 'unknown')
ORDER BY online_users DESC, geo_city ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var roleName, geoCity string
		var onlineUsers int64
		var lastSeen time.Time
		if err := rows.Scan(&roleName, &geoCity, &onlineUsers, &lastSeen); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"role":             roleName,
			"geo_city":         geoCity,
			"online_users":     onlineUsers,
			"last_seen_at_utc": lastSeen.UTC().Format(time.RFC3339),
		})
	}
	return out, rows.Err()
}

func (r *Repo) roleBreakdown(ctx context.Context) ([]map[string]any, error) {
	rows, err := r.pg.Query(ctx, `
WITH roles AS (
  SELECT 'driver'::text AS role, COUNT(*) AS total_users FROM drivers
  UNION ALL
  SELECT 'cargo_manager'::text AS role, COUNT(*) AS total_users FROM freelance_dispatchers WHERE deleted_at IS NULL AND UPPER(COALESCE(manager_role, '')) = 'CARGO_MANAGER'
  UNION ALL
  SELECT 'driver_manager'::text AS role, COUNT(*) AS total_users FROM freelance_dispatchers WHERE deleted_at IS NULL AND UPPER(COALESCE(manager_role, '')) = 'DRIVER_MANAGER'
  UNION ALL
  SELECT 'admin'::text AS role, COUNT(*) AS total_users FROM admins
)
SELECT role, total_users FROM roles
ORDER BY role ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, 4)
	for rows.Next() {
		var roleName string
		var totalUsers int64
		if err := rows.Scan(&roleName, &totalUsers); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"role":        roleName,
			"total_users": totalUsers,
		})
	}
	return out, rows.Err()
}

func (r *Repo) funnelCounts(ctx context.Context, w TimeWindow, role string) (int64, int64, int64, int64, error) {
	var cargoCount, offerCount, tripCount, completedCount int64
	err := r.pg.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM cargo WHERE created_at >= $1 AND created_at < $2),
  (SELECT COUNT(*) FROM offers WHERE created_at >= $1 AND created_at < $2),
  (SELECT COUNT(*) FROM trips WHERE created_at >= $1 AND created_at < $2),
  (SELECT COUNT(*) FROM trips WHERE status = 'COMPLETED' AND updated_at >= $1 AND updated_at < $2)
`, w.From.UTC(), w.To.UTC()).Scan(&cargoCount, &offerCount, &tripCount, &completedCount)
	return cargoCount, offerCount, tripCount, completedCount, err
}

func (r *Repo) countOfferAccepted(ctx context.Context, w TimeWindow, role string) (int64, error) {
	args := []any{w.From.UTC(), w.To.UTC()}
	roleFilter := ""
	if nr := NormalizeRole(role); nr != "" && nr != RoleUnknown {
		args = append(args, nr)
		roleFilter = " AND role = $" + strconv.Itoa(len(args))
	}
	var count int64
	err := r.pg.QueryRow(ctx, `
SELECT COUNT(*)
FROM analytics_events
WHERE event_name = 'offer_accepted'
  AND event_time_utc >= $1 AND event_time_utc < $2`+roleFilter+`
`, args...).Scan(&count)
	return count, err
}

func (r *Repo) compareWindowCounts(ctx context.Context, w TimeWindow) (int64, int64, int64, error) {
	var cargoCount, offerCount, tripCount int64
	err := r.pg.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM cargo WHERE created_at >= $1 AND created_at < $2),
  (SELECT COUNT(*) FROM offers WHERE created_at >= $1 AND created_at < $2),
  (SELECT COUNT(*) FROM trips WHERE created_at >= $1 AND created_at < $2)
`, w.From.UTC(), w.To.UTC()).Scan(&cargoCount, &offerCount, &tripCount)
	return cargoCount, offerCount, tripCount, err
}

func (r *Repo) offerAcceptedBuckets(ctx context.Context, w TimeWindow, period, role string) (map[string]int64, error) {
	args := []any{w.From.UTC(), w.To.UTC()}
	roleFilter := ""
	if nr := NormalizeRole(role); nr != "" && nr != RoleUnknown {
		args = append(args, nr)
		roleFilter = " AND role = $" + strconv.Itoa(len(args))
	}
	rows, err := r.pg.Query(ctx, `
SELECT date_trunc('`+period+`', event_time_utc) AS bucket, COUNT(*)
FROM analytics_events
WHERE event_name = 'offer_accepted'
  AND event_time_utc >= $1 AND event_time_utc < $2`+roleFilter+`
GROUP BY 1
`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var bucket time.Time
		var count int64
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, err
		}
		out[bucket.UTC().Format(time.RFC3339)] = count
	}
	return out, rows.Err()
}

func normalizePage(page Page) (limit, offset int) {
	limit = page.Limit
	offset = page.Offset
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func normalizeSortDir(sortDir string) string {
	if strings.EqualFold(strings.TrimSpace(sortDir), "asc") {
		return "ASC"
	}
	return "DESC"
}

func nullableString(v string) any {
	s := strings.TrimSpace(v)
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func mustJSON(v map[string]any) []byte {
	if v == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func mustJSONPtr(v map[string]any) any {
	if v == nil {
		return nil
	}
	return mustJSON(v)
}

func filterMetricValues(values map[string]any, metricNames []string) {
	if len(metricNames) == 0 {
		return
	}
	allowed := make(map[string]struct{}, len(metricNames))
	for _, name := range metricNames {
		allowed[strings.TrimSpace(name)] = struct{}{}
	}
	for k := range values {
		if _, ok := allowed[k]; !ok {
			delete(values, k)
		}
	}
}

func ratioPct(num, den int64) float64 {
	if den <= 0 {
		return 0
	}
	return (float64(num) / float64(den)) * 100
}

func growthPct(prev, cur int64) *float64 {
	if prev == 0 {
		if cur == 0 {
			v := 0.0
			return &v
		}
		return nil
	}
	v := ((float64(cur) - float64(prev)) / float64(prev)) * 100
	return &v
}

func uuidPtrString(v *uuid.UUID) any {
	if v == nil || *v == uuid.Nil {
		return nil
	}
	return v.String()
}

func strValue(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func formatTimePtr(v *time.Time) any {
	if v == nil || v.IsZero() {
		return nil
	}
	return v.UTC().Format(time.RFC3339)
}

func durationSeconds(startedAt, endedAt *time.Time) int64 {
	if startedAt == nil {
		return 0
	}
	end := time.Now().UTC()
	if endedAt != nil && !endedAt.IsZero() {
		end = endedAt.UTC()
	}
	if end.Before(startedAt.UTC()) {
		return 0
	}
	return int64(end.Sub(startedAt.UTC()).Seconds())
}

func maxInt64(v, min int64) int64 {
	if v < min {
		return min
	}
	return v
}
