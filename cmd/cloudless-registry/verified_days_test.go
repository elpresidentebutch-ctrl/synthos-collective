package main

import (
	"testing"
	"time"
)

func TestVerifiedDaysRoundsDownToOneDecimal(t *testing.T) {
	day := 24 * time.Hour
	for _, tc := range []struct {
		uptime time.Duration
		want   float64
	}{
		{0, 0},
		{-time.Hour, 0},
		{36*time.Hour + 39*time.Minute, 1.5}, // the dashboard showed 1.5276041782407408
		{day, 1},
		{time.Duration(29.96 * float64(day)), 29.9}, // never shows 30.0 early
		{30 * day, 30},
	} {
		if got := verifiedDays(tc.uptime); got != tc.want {
			t.Errorf("verifiedDays(%v) = %v, want %v", tc.uptime, got, tc.want)
		}
	}
}
