package starhistory

import (
	"fmt"
	"sort"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

// NormalizeWatchEvents 把日 WatchEvent 累计值按当前 Stars 归一化为估算曲线。
//
// GH Archive 没有 Unstar 事件，原始累计不能冒充真实历史。整数四舍五入后再次做单调
// 钳制，并强制末点等于 currentStars；totalEvents 为零时不生成伪造的零曲线。
func NormalizeWatchEvents(
	events []DailyWatchEvent,
	currentStars int,
) ([]model.StarHistoryPoint, error) {
	if currentStars < 0 {
		return nil, fmt.Errorf("current_stars must not be negative")
	}
	if len(events) == 0 {
		return []model.StarHistoryPoint{}, nil
	}

	aggregated := make(map[string]int64, len(events))
	for _, event := range events {
		if event.Date.IsZero() {
			return nil, fmt.Errorf("watch event date is required")
		}
		if event.Count < 0 {
			return nil, fmt.Errorf("watch event count must not be negative")
		}
		day := event.Date.UTC().Format("2006-01-02")
		aggregated[day] += event.Count
	}
	days := make([]string, 0, len(aggregated))
	var totalEvents int64
	for day, count := range aggregated {
		days = append(days, day)
		totalEvents += count
	}
	if totalEvents == 0 {
		return []model.StarHistoryPoint{}, nil
	}
	sort.Strings(days)

	points := make([]model.StarHistoryPoint, 0, len(days))
	var cumulative int64
	previous := 0
	for _, day := range days {
		cumulative += aggregated[day]
		// (2*n + denominator) / (2*denominator) 等价于非负整数的四舍五入。
		numerator := int64(currentStars) * cumulative
		estimated := int((2*numerator + totalEvents) / (2 * totalEvents))
		if estimated < previous {
			estimated = previous
		}
		if estimated > currentStars {
			estimated = currentStars
		}
		points = append(points, model.StarHistoryPoint{
			Date:      day,
			Count:     estimated,
			Source:    model.StarHistorySourceGHArchive,
			Precision: model.StarHistoryEstimated,
		})
		previous = estimated
	}
	points[len(points)-1].Count = currentStars
	return points, nil
}

// MergeExactSnapshots 合并 Discovery 精确快照；同日精确点优先，且不会对精确段做
// 单调钳制，因此真实 Unstar 导致的下降会原样保留。
func MergeExactSnapshots(
	estimated []model.StarHistoryPoint,
	exact []model.StarHistoryPoint,
) ([]model.StarHistoryPoint, error) {
	byDay := make(map[string]model.StarHistoryPoint, len(estimated)+len(exact))
	for _, point := range estimated {
		if err := validateHistoryPoint(point); err != nil {
			return nil, err
		}
		byDay[point.Date] = point
	}
	for _, point := range exact {
		if err := validateHistoryPoint(point); err != nil {
			return nil, err
		}
		if point.Precision != model.StarHistorySnapshot {
			return nil, fmt.Errorf("exact snapshot precision must be snapshot")
		}
		byDay[point.Date] = point
	}
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Strings(days)
	points := make([]model.StarHistoryPoint, 0, len(days))
	for _, day := range days {
		points = append(points, byDay[day])
	}
	return points, nil
}

func validateHistoryPoint(point model.StarHistoryPoint) error {
	if _, err := time.Parse("2006-01-02", point.Date); err != nil {
		return fmt.Errorf("invalid history date %q: %w", point.Date, err)
	}
	if point.Count < 0 {
		return fmt.Errorf("history count must not be negative")
	}
	if point.Source == "" || point.Precision == "" {
		return fmt.Errorf("history source and precision are required")
	}
	return nil
}
