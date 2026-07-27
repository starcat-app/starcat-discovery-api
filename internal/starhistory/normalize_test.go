package starhistory

import (
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestNormalizeWatchEventsAggregatesAndCalibratesLastPoint(t *testing.T) {
	events := []DailyWatchEvent{
		{Date: day(2026, 7, 2), Count: 2},
		{Date: day(2026, 7, 1), Count: 1},
		{Date: day(2026, 7, 1), Count: 1},
		{Date: day(2026, 7, 3), Count: 1},
	}
	points, err := NormalizeWatchEvents(events, 10)
	if err != nil {
		t.Fatalf("NormalizeWatchEvents() error = %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("point count = %d", len(points))
	}
	want := []struct {
		date  string
		count int
	}{
		{"2026-07-01", 4},
		{"2026-07-02", 8},
		{"2026-07-03", 10},
	}
	for index, expected := range want {
		if points[index].Date != expected.date || points[index].Count != expected.count {
			t.Fatalf("point[%d] = %+v, want %+v", index, points[index], expected)
		}
		if points[index].Source != model.StarHistorySourceGHArchive ||
			points[index].Precision != model.StarHistoryEstimated {
			t.Fatalf("point[%d] lost estimation metadata: %+v", index, points[index])
		}
	}
}

func TestNormalizeWatchEventsReturnsEmptyForZeroEvents(t *testing.T) {
	points, err := NormalizeWatchEvents([]DailyWatchEvent{
		{Date: day(2026, 7, 1), Count: 0},
	}, 100)
	if err != nil {
		t.Fatalf("NormalizeWatchEvents() error = %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("zero events produced fake history: %+v", points)
	}
}

func TestMergeExactSnapshotsPreservesRealDecreaseAndWinsSameDay(t *testing.T) {
	estimated := []model.StarHistoryPoint{
		{
			Date:      "2026-07-01",
			Count:     90,
			Source:    model.StarHistorySourceGHArchive,
			Precision: model.StarHistoryEstimated,
		},
		{
			Date:      "2026-07-02",
			Count:     100,
			Source:    model.StarHistorySourceGHArchive,
			Precision: model.StarHistoryEstimated,
		},
	}
	exact := []model.StarHistoryPoint{
		{
			Date:      "2026-07-02",
			Count:     98,
			Source:    model.StarHistorySourceDiscoverySnapshot,
			Precision: model.StarHistoryExact,
		},
		{
			Date:      "2026-07-03",
			Count:     96,
			Source:    model.StarHistorySourceDiscoverySnapshot,
			Precision: model.StarHistoryExact,
		},
	}

	points, err := MergeExactSnapshots(estimated, exact)
	if err != nil {
		t.Fatalf("MergeExactSnapshots() error = %v", err)
	}
	if len(points) != 3 || points[1].Count != 98 || points[2].Count != 96 {
		t.Fatalf("exact decrease was not preserved: %+v", points)
	}
	if points[1].Precision != model.StarHistoryExact {
		t.Fatalf("same-day exact point did not win: %+v", points[1])
	}
}

func day(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
