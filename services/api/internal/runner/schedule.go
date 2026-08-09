package runner

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func matchesEventType(types []string, action string) bool {
	if len(types) == 0 {
		return true
	}
	for _, eventType := range types {
		if eventType == action {
			return true
		}
	}
	return false
}

type cronExpression struct {
	minute     map[int]bool
	hour       map[int]bool
	dayOfMonth map[int]bool
	month      map[int]bool
	dayOfWeek  map[int]bool
	dayAny     bool
	weekAny    bool
}

func parseCron(expression string) (cronExpression, error) {
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return cronExpression{}, errors.New("schedule cron must contain exactly five fields")
	}
	minute, err := parseCronField(parts[0], 0, 59)
	if err != nil {
		return cronExpression{}, fmt.Errorf("invalid schedule minute: %w", err)
	}
	hour, err := parseCronField(parts[1], 0, 23)
	if err != nil {
		return cronExpression{}, fmt.Errorf("invalid schedule hour: %w", err)
	}
	dayOfMonth, err := parseCronField(parts[2], 1, 31)
	if err != nil {
		return cronExpression{}, fmt.Errorf("invalid schedule day: %w", err)
	}
	month, err := parseCronField(parts[3], 1, 12)
	if err != nil {
		return cronExpression{}, fmt.Errorf("invalid schedule month: %w", err)
	}
	dayOfWeek, err := parseCronField(parts[4], 0, 6)
	if err != nil {
		return cronExpression{}, fmt.Errorf("invalid schedule weekday: %w", err)
	}
	return cronExpression{
		minute: minute, hour: hour, dayOfMonth: dayOfMonth, month: month, dayOfWeek: dayOfWeek,
		dayAny: parts[2] == "*", weekAny: parts[4] == "*",
	}, nil
}

func parseCronField(value string, minimum int, maximum int) (map[int]bool, error) {
	values := make(map[int]bool)
	for _, item := range strings.Split(value, ",") {
		if item == "" {
			return nil, errors.New("empty cron field item")
		}
		base := item
		step := 1
		if strings.Contains(item, "/") {
			parts := strings.Split(item, "/")
			if len(parts) != 2 || parts[1] == "" {
				return nil, errors.New("cron step is malformed")
			}
			parsedStep, err := strconv.Atoi(parts[1])
			if err != nil || parsedStep < 1 {
				return nil, errors.New("cron step must be positive")
			}
			step = parsedStep
			base = parts[0]
		}
		start, end := minimum, maximum
		switch {
		case base == "*":
		case strings.Contains(base, "-"):
			parts := strings.Split(base, "-")
			if len(parts) != 2 {
				return nil, errors.New("cron range is malformed")
			}
			var err error
			start, err = strconv.Atoi(parts[0])
			if err != nil {
				return nil, errors.New("cron range start is not an integer")
			}
			end, err = strconv.Atoi(parts[1])
			if err != nil {
				return nil, errors.New("cron range end is not an integer")
			}
		default:
			parsed, err := strconv.Atoi(base)
			if err != nil {
				return nil, errors.New("cron value is not an integer")
			}
			start, end = parsed, parsed
		}
		if start < minimum || end > maximum || start > end {
			return nil, errors.New("cron value is outside its field range")
		}
		for current := start; current <= end; current += step {
			values[current] = true
		}
	}
	return values, nil
}

func LastScheduleOccurrence(expression string, now time.Time) (time.Time, bool) {
	cron, err := parseCron(expression)
	if err != nil {
		return time.Time{}, false
	}
	candidate := now.UTC().Truncate(time.Minute)
	for index := 0; index <= 366*24*60; index++ {
		if cronMatches(cron, candidate) {
			return candidate, true
		}
		candidate = candidate.Add(-time.Minute)
	}
	return time.Time{}, false
}

func cronMatches(cron cronExpression, candidate time.Time) bool {
	if !cron.minute[candidate.Minute()] || !cron.hour[candidate.Hour()] ||
		!cron.month[int(candidate.Month())] {
		return false
	}
	dayMatches := cron.dayOfMonth[candidate.Day()]
	weekMatches := cron.dayOfWeek[int(candidate.Weekday())]
	if cron.dayAny && cron.weekAny {
		return true
	}
	if cron.dayAny {
		return weekMatches
	}
	if cron.weekAny {
		return dayMatches
	}
	return dayMatches || weekMatches
}
