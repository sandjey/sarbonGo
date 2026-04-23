package handlers

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/drivers"
)

func normalizedDispatcherManagerRole(role *string) string {
	if role == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(*role))
}

func isCargoManagerRole(role *string) bool {
	return normalizedDispatcherManagerRole(role) == dispatchers.ManagerRoleCargoManager
}

func isDriverManagerRole(role *string) bool {
	return normalizedDispatcherManagerRole(role) == dispatchers.ManagerRoleDriverManager
}

func normalizeAndValidateOfferMoney(price float64, currency string) (string, string) {
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return "", "invalid_price"
	}
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if len(cur) < 3 || len(cur) > 5 {
		return "", "invalid_currency"
	}
	return cur, ""
}

func currentDispatcherManagerRole(ctx context.Context, repo *dispatchers.Repo, dispatcherID uuid.UUID) (string, error) {
	if repo == nil || dispatcherID == uuid.Nil {
		return "", nil
	}
	disp, err := repo.FindByID(ctx, dispatcherID)
	if err != nil || disp == nil {
		return "", err
	}
	return normalizedDispatcherManagerRole(disp.ManagerRole), nil
}

func dispatcherLinkedToDriver(ctx context.Context, repo *drivers.Repo, dispatcherID, driverID uuid.UUID, drv *drivers.Driver) bool {
	if repo == nil || dispatcherID == uuid.Nil || driverID == uuid.Nil {
		return false
	}
	linked, _ := repo.IsLinked(ctx, driverID, dispatcherID)
	if linked {
		return true
	}
	return drv != nil && drv.FreelancerID != nil && strings.TrimSpace(*drv.FreelancerID) == dispatcherID.String()
}

func parseBoundedIntQueryStrict(raw string, def, min, max int) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if v < min || v > max {
		return 0, false
	}
	return v, true
}

func mergeDriverListsForManager(listRel, listLegacy []*drivers.Driver, f drivers.ListDriversFilter) ([]*drivers.Driver, int) {
	byID := make(map[string]*drivers.Driver, len(listRel)+len(listLegacy))
	for _, d := range listRel {
		mergeDriverByID(byID, d)
	}
	for _, d := range listLegacy {
		mergeDriverByID(byID, d)
	}

	list := make([]*drivers.Driver, 0, len(byID))
	for _, d := range byID {
		list = append(list, d)
	}

	sortDrivers(list, f.Sort)
	total := len(list)
	return paginateDrivers(list, f.Page, f.Limit), total
}

func mergeDriverByID(dst map[string]*drivers.Driver, d *drivers.Driver) {
	if d == nil {
		return
	}
	if existing, ok := dst[d.ID]; !ok || existing == nil || existing.UpdatedAt.Before(d.UpdatedAt) {
		dst[d.ID] = d
	}
}

func paginateDrivers(list []*drivers.Driver, page, limit int) []*drivers.Driver {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	if offset >= len(list) {
		return []*drivers.Driver{}
	}
	end := offset + limit
	if end > len(list) {
		end = len(list)
	}
	return list[offset:end]
}

func sortDrivers(list []*drivers.Driver, sortSpec string) {
	col := "updated_at"
	dir := "DESC"
	if sortSpec != "" {
		parts := strings.SplitN(sortSpec, ":", 2)
		if len(parts) == 2 {
			switch strings.TrimSpace(strings.ToLower(parts[0])) {
			case "name", "last_online_at", "work_status", "updated_at":
				col = strings.TrimSpace(strings.ToLower(parts[0]))
			}
			switch strings.TrimSpace(strings.ToUpper(parts[1])) {
			case "ASC", "DESC":
				dir = strings.TrimSpace(strings.ToUpper(parts[1]))
			}
		}
	}

	sort.Slice(list, func(i, j int) bool {
		a := list[i]
		b := list[j]
		switch col {
		case "name":
			if cmp, ok := compareNullableStrings(a.Name, b.Name, dir); ok {
				return cmp
			}
		case "last_online_at":
			if cmp, ok := compareNullableTimes(a.LastOnlineAt, b.LastOnlineAt, dir); ok {
				return cmp
			}
		case "work_status":
			if cmp, ok := compareNullableStrings(a.WorkStatus, b.WorkStatus, dir); ok {
				return cmp
			}
		default:
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				if dir == "ASC" {
					return a.UpdatedAt.Before(b.UpdatedAt)
				}
				return a.UpdatedAt.After(b.UpdatedAt)
			}
		}

		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return strings.Compare(a.ID, b.ID) < 0
	})
}

func compareNullableStrings(a, b *string, dir string) (bool, bool) {
	av, aok := normalizedNullableString(a)
	bv, bok := normalizedNullableString(b)
	if !aok && !bok {
		return false, false
	}
	if !aok {
		return false, true
	}
	if !bok {
		return true, true
	}
	if av == bv {
		return false, false
	}
	if dir == "ASC" {
		return av < bv, true
	}
	return av > bv, true
}

func compareNullableTimes(a, b *time.Time, dir string) (bool, bool) {
	if a == nil && b == nil {
		return false, false
	}
	if a == nil {
		return false, true
	}
	if b == nil {
		return true, true
	}
	if a.Equal(*b) {
		return false, false
	}
	if dir == "ASC" {
		return a.Before(*b), true
	}
	return a.After(*b), true
}

func normalizedNullableString(v *string) (string, bool) {
	if v == nil {
		return "", false
	}
	s := strings.ToLower(strings.TrimSpace(*v))
	if s == "" {
		return "", false
	}
	return s, true
}
