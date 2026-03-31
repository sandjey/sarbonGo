package handlers

import (
	"strconv"
	"strings"
)

func validateLatLng(lat, lng float64) string {
	if lat < -90 || lat > 90 {
		return "invalid_latitude"
	}
	if lng < -180 || lng > 180 {
		return "invalid_longitude"
	}
	return ""
}

func validatePersonName(name string) string {
	name = strings.TrimSpace(name)
	n := 0
	for _, r := range name {
		if r < 0x20 {
			return "invalid_name"
		}
		n++
		if n > 64 {
			return "name_too_long"
		}
	}
	if n < 2 {
		return "name_too_short"
	}
	return ""
}

func validatePassportSeries(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "driver_passport_series_required"
	}
	if len(s) < 2 || len(s) > 10 {
		return "invalid_passport_series"
	}
	return ""
}

func validatePassportNumber(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "driver_passport_number_required"
	}
	if len(s) < 5 || len(s) > 20 {
		return "invalid_passport_number"
	}
	return ""
}

func validatePINFL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "driver_pinfl_required"
	}
	if len(s) != 14 {
		return "invalid_pinfl"
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "invalid_pinfl"
		}
	}
	return ""
}

// validatePowerTrailerTypes validates power_plate_type + trailer_plate_type pair.
// Returns resp i18n key on error, empty string on success.
func validatePowerTrailerTypes(powerPlateType, trailerPlateType string) string {
	powerPlateType = strings.ToUpper(strings.TrimSpace(powerPlateType))
	trailerPlateType = strings.ToUpper(strings.TrimSpace(trailerPlateType))
	if powerPlateType != "TRUCK" && powerPlateType != "TRACTOR" {
		return "invalid_power_plate_type"
	}
	allowed := map[string]map[string]bool{
		"TRUCK": {
			"FLATBED": true, "TENTED": true, "BOX": true, "REEFER": true, "TANKER": true, "TIPPER": true, "CAR_CARRIER": true,
		},
		"TRACTOR": {
			"FLATBED": true, "TENTED": true, "BOX": true, "REEFER": true, "TANKER": true, "LOWBED": true, "CONTAINER": true,
		},
	}
	if !allowed[powerPlateType][trailerPlateType] {
		return "invalid_trailer_plate_type_for_power"
	}
	return ""
}

func weakETagBytes(b []byte) string {
	const base = "W/\""
	var h uint64 = 1469598103934665603
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return base + strconv.FormatUint(h, 16) + "\""
}
