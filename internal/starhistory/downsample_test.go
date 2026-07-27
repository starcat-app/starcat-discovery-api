package starhistory

import (
	"fmt"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestDownsampleHistoryUsesDailyWeeklyAndMonthlyPolicies(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	points := makeDailyPoints(now.AddDate(-2, 0, 0), now, model.StarHistorySourceGHArchive)

	threeMonths, err := DownsampleHistory(
		points,
		model.StarHistoryRangeThreeMonths,
		now,
		now,
		DefaultMaximumHistoryPoints,
	)
	if err != nil {
		t.Fatalf("DownsampleHistory(3m) error = %v", err)
	}
	oneYear, err := DownsampleHistory(
		points,
		model.StarHistoryRangeOneYear,
		now,
		now,
		DefaultMaximumHistoryPoints,
	)
	if err != nil {
		t.Fatalf("DownsampleHistory(1y) error = %v", err)
	}
	all, err := DownsampleHistory(
		points,
		model.StarHistoryRangeAll,
		now,
		now,
		DefaultMaximumHistoryPoints,
	)
	if err != nil {
		t.Fatalf("DownsampleHistory(all) error = %v", err)
	}

	if len(threeMonths.Points) < 90 || len(threeMonths.Points) > 93 {
		t.Fatalf("3m did not retain daily points: %d", len(threeMonths.Points))
	}
	if len(oneYear.Points) < 52 || len(oneYear.Points) > 55 {
		t.Fatalf("1y did not aggregate weekly: %d", len(oneYear.Points))
	}
	if len(all.Points) < 24 || len(all.Points) > 26 {
		t.Fatalf("all did not aggregate monthly: %d", len(all.Points))
	}
	if threeMonths.CoverageStart != points[0].Date ||
		oneYear.CoverageStart != points[0].Date ||
		all.CoverageStart != points[0].Date {
		t.Fatal("coverage_start must describe full available history")
	}
}

func TestDownsampleHistoryCapsPointsAndPreservesBoundaries(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	points := makeDailyPoints(now.AddDate(0, -3, 0), now, model.StarHistorySourceGHArchive)
	transition := len(points) / 2
	for index := transition; index < len(points); index++ {
		points[index].Source = model.StarHistorySourceDiscoverySnapshot
		points[index].Precision = model.StarHistorySnapshot
	}

	series, err := DownsampleHistory(
		points,
		model.StarHistoryRangeThreeMonths,
		now,
		now,
		10,
	)
	if err != nil {
		t.Fatalf("DownsampleHistory() error = %v", err)
	}
	if len(series.Points) != 10 {
		t.Fatalf("point cap = %d, want 10", len(series.Points))
	}
	if series.Points[0] != points[0] ||
		series.Points[len(series.Points)-1] != points[len(points)-1] {
		t.Fatalf("first or last point was lost: %+v", series.Points)
	}
	hasEstimated := false
	hasSnapshot := false
	for _, point := range series.Points {
		hasEstimated = hasEstimated || point.Source == model.StarHistorySourceGHArchive
		hasSnapshot = hasSnapshot || point.Source == model.StarHistorySourceDiscoverySnapshot
	}
	if !hasEstimated || !hasSnapshot {
		t.Fatalf("source transition was lost: %+v", series.Points)
	}
}

func makeDailyPoints(
	start time.Time,
	end time.Time,
	source model.StarHistorySource,
) []model.StarHistoryPoint {
	points := []model.StarHistoryPoint{}
	count := 0
	for date := start.UTC(); !date.After(end.UTC()); date = date.AddDate(0, 0, 1) {
		count++
		points = append(points, model.StarHistoryPoint{
			Date:      date.Format("2006-01-02"),
			Count:     count,
			Source:    source,
			Precision: model.StarHistoryEstimated,
		})
	}
	if len(points) == 0 {
		panic(fmt.Sprintf("empty test range %s...%s", start, end))
	}
	return points
}
