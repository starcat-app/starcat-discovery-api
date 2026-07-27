package starhistory

import (
	"fmt"
	"sort"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

const DefaultMaximumHistoryPoints = 500

type datedHistoryPoint struct {
	point model.StarHistoryPoint
	date  time.Time
}

// DownsampleHistory 按固定产品范围输出日 / 周 / 月序列。
//
// 每个 bucket 按 source + precision 保留末点，避免精度边界被聚合吞掉；随后再执行上限
// 采样，并强制保留范围首末点和来源切换边界。
func DownsampleHistory(
	points []model.StarHistoryPoint,
	historyRange model.StarHistoryRange,
	now time.Time,
	generatedAt time.Time,
	maximumPoints int,
) (model.StarHistorySeries, error) {
	if now.IsZero() || generatedAt.IsZero() {
		return model.StarHistorySeries{}, fmt.Errorf("now and generated_at are required")
	}
	if maximumPoints <= 0 {
		maximumPoints = DefaultMaximumHistoryPoints
	}

	dated, err := parseAndSortHistoryPoints(points)
	if err != nil {
		return model.StarHistorySeries{}, err
	}
	series := model.StarHistorySeries{
		Range:       historyRange,
		GeneratedAt: generatedAt.UTC(),
		Points:      []model.StarHistoryPoint{},
	}
	if len(dated) == 0 {
		return series, nil
	}
	series.CoverageStart = dated[0].point.Date

	filtered, bucket, err := historyRangePolicy(dated, historyRange, now.UTC())
	if err != nil {
		return model.StarHistorySeries{}, err
	}
	if len(filtered) == 0 {
		return series, nil
	}

	grouped := groupHistoryPoints(filtered, bucket)
	grouped = ensureFirstPoint(grouped, filtered[0].point)
	series.Points = capHistoryPoints(grouped, maximumPoints)
	return series, nil
}

func parseAndSortHistoryPoints(points []model.StarHistoryPoint) ([]datedHistoryPoint, error) {
	dated := make([]datedHistoryPoint, 0, len(points))
	for _, point := range points {
		if err := validateHistoryPoint(point); err != nil {
			return nil, err
		}
		date, _ := time.Parse("2006-01-02", point.Date)
		dated = append(dated, datedHistoryPoint{point: point, date: date})
	}
	sort.SliceStable(dated, func(i, j int) bool { return dated[i].date.Before(dated[j].date) })
	return dated, nil
}

func historyRangePolicy(
	points []datedHistoryPoint,
	historyRange model.StarHistoryRange,
	now time.Time,
) ([]datedHistoryPoint, func(time.Time) string, error) {
	var cutoff time.Time
	var bucket func(time.Time) string
	switch historyRange {
	case model.StarHistoryRangeThreeMonths:
		cutoff = now.AddDate(0, -3, 0)
		bucket = func(date time.Time) string { return date.Format("2006-01-02") }
	case model.StarHistoryRangeOneYear:
		cutoff = now.AddDate(-1, 0, 0)
		bucket = func(date time.Time) string {
			year, week := date.ISOWeek()
			return fmt.Sprintf("%04d-W%02d", year, week)
		}
	case model.StarHistoryRangeAll:
		bucket = func(date time.Time) string { return date.Format("2006-01") }
	default:
		return nil, nil, fmt.Errorf("unsupported star history range %q", historyRange)
	}

	if cutoff.IsZero() {
		return points, bucket, nil
	}
	// 历史点是 UTC 日期；范围边界也归一到当天 00:00，避免请求发生在中午时误删首日。
	cutoff = time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.UTC)
	filtered := make([]datedHistoryPoint, 0, len(points))
	for _, point := range points {
		if !point.date.Before(cutoff) {
			filtered = append(filtered, point)
		}
	}
	return filtered, bucket, nil
}

func groupHistoryPoints(
	points []datedHistoryPoint,
	bucket func(time.Time) string,
) []model.StarHistoryPoint {
	lastByGroup := make(map[string]datedHistoryPoint)
	for _, point := range points {
		key := bucket(point.date) + "|" + string(point.point.Source) + "|" + string(point.point.Precision)
		lastByGroup[key] = point
	}
	grouped := make([]datedHistoryPoint, 0, len(lastByGroup))
	for _, point := range lastByGroup {
		grouped = append(grouped, point)
	}
	sort.SliceStable(grouped, func(i, j int) bool { return grouped[i].date.Before(grouped[j].date) })
	result := make([]model.StarHistoryPoint, 0, len(grouped))
	for _, point := range grouped {
		result = append(result, point.point)
	}
	return result
}

func ensureFirstPoint(
	points []model.StarHistoryPoint,
	first model.StarHistoryPoint,
) []model.StarHistoryPoint {
	for _, point := range points {
		if point == first {
			return points
		}
	}
	points = append(points, first)
	sort.SliceStable(points, func(i, j int) bool { return points[i].Date < points[j].Date })
	return points
}

func capHistoryPoints(
	points []model.StarHistoryPoint,
	maximumPoints int,
) []model.StarHistoryPoint {
	if len(points) <= maximumPoints {
		return points
	}

	required := map[int]bool{0: true, len(points) - 1: true}
	for index := 1; index < len(points); index++ {
		if points[index].Source != points[index-1].Source ||
			points[index].Precision != points[index-1].Precision {
			required[index-1] = true
			required[index] = true
		}
	}
	selected := make(map[int]bool, maximumPoints)
	for index := range required {
		selected[index] = true
	}

	if len(selected) > maximumPoints {
		selected = map[int]bool{0: true, len(points) - 1: true}
	}
	for len(selected) < maximumPoints {
		target := float64(len(selected)) * float64(len(points)-1) /
			float64(maximumPoints-1)
		index := int(target + 0.5)
		for index < len(points)-1 && selected[index] {
			index++
		}
		for index > 0 && selected[index] {
			index--
		}
		if selected[index] {
			break
		}
		selected[index] = true
	}

	result := make([]model.StarHistoryPoint, 0, len(selected))
	for index, point := range points {
		if selected[index] {
			result = append(result, point)
		}
	}
	return result
}
