package handlers

import (
	"testing"
	"time"

	"sarbonNew/internal/drivers"
)

func TestNormalizeAndValidateOfferMoney(t *testing.T) {
	t.Parallel()

	currency, errKey := normalizeAndValidateOfferMoney(1250.5, " uzs ")
	if errKey != "" {
		t.Fatalf("expected valid money input, got error key %q", errKey)
	}
	if currency != "UZS" {
		t.Fatalf("expected normalized currency UZS, got %q", currency)
	}

	if _, errKey := normalizeAndValidateOfferMoney(0, "USD"); errKey != "invalid_price" {
		t.Fatalf("expected invalid_price for zero price, got %q", errKey)
	}
	if _, errKey := normalizeAndValidateOfferMoney(-10, "USD"); errKey != "invalid_price" {
		t.Fatalf("expected invalid_price for negative price, got %q", errKey)
	}
	if _, errKey := normalizeAndValidateOfferMoney(10, "X"); errKey != "invalid_currency" {
		t.Fatalf("expected invalid_currency for short currency, got %q", errKey)
	}
}

func TestParseBoundedIntQueryStrict(t *testing.T) {
	t.Parallel()

	if got, ok := parseBoundedIntQueryStrict("", 20, 1, 100); !ok || got != 20 {
		t.Fatalf("expected default value 20, got value=%d ok=%v", got, ok)
	}
	if _, ok := parseBoundedIntQueryStrict("abc", 20, 1, 100); ok {
		t.Fatal("expected parse failure for non-numeric value")
	}
	if _, ok := parseBoundedIntQueryStrict("101", 20, 1, 100); ok {
		t.Fatal("expected validation failure for out-of-range value")
	}
	if got, ok := parseBoundedIntQueryStrict("50", 20, 1, 100); !ok || got != 50 {
		t.Fatalf("expected value 50, got value=%d ok=%v", got, ok)
	}
}

func TestMergeDriverListsForManagerSortsDedupesAndPaginates(t *testing.T) {
	t.Parallel()

	alpha := "Alpha"
	bravo := "Bravo"
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	older := now.Add(-time.Hour)

	listRel := []*drivers.Driver{
		{ID: "driver-1", Name: &bravo, UpdatedAt: older},
		{ID: "driver-2", Name: &alpha, UpdatedAt: now},
	}
	listLegacy := []*drivers.Driver{
		{ID: "driver-2", Name: &alpha, UpdatedAt: older},
		{ID: "driver-3", UpdatedAt: now.Add(-2 * time.Hour)},
	}

	page1, total := mergeDriverListsForManager(listRel, listLegacy, drivers.ListDriversFilter{
		Page:  1,
		Limit: 2,
		Sort:  "name:asc",
	})
	if total != 3 {
		t.Fatalf("expected total=3 after dedupe, got %d", total)
	}
	if len(page1) != 2 || page1[0].ID != "driver-2" || page1[1].ID != "driver-1" {
		t.Fatalf("unexpected first page order: %+v", driverIDs(page1))
	}

	page2, _ := mergeDriverListsForManager(listRel, listLegacy, drivers.ListDriversFilter{
		Page:  2,
		Limit: 2,
		Sort:  "name:asc",
	})
	if len(page2) != 1 || page2[0].ID != "driver-3" {
		t.Fatalf("unexpected second page: %+v", driverIDs(page2))
	}
}

func driverIDs(list []*drivers.Driver) []string {
	out := make([]string, 0, len(list))
	for _, d := range list {
		if d == nil {
			out = append(out, "<nil>")
			continue
		}
		out = append(out, d.ID)
	}
	return out
}
