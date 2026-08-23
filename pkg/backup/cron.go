package backup

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func scheduleExpression(s Schedule) (string, error) {
	switch s.Kind {
	case "daily":
		if s.Hour < 0 || s.Hour > 23 || s.Minute < 0 || s.Minute > 59 {
			return "", fmt.Errorf("daily schedule time is invalid")
		}
		return fmt.Sprintf("%d %d * * *", s.Minute, s.Hour), nil
	case "weekly":
		if s.Weekday < 0 || s.Weekday > 6 || s.Hour < 0 || s.Hour > 23 || s.Minute < 0 || s.Minute > 59 {
			return "", fmt.Errorf("weekly schedule is invalid")
		}
		return fmt.Sprintf("%d %d * * %d", s.Minute, s.Hour, s.Weekday), nil
	case "cron":
		if strings.TrimSpace(s.Cron) == "" {
			return "", fmt.Errorf("cron expression is required")
		}
		return strings.TrimSpace(s.Cron), nil
	default:
		return "", fmt.Errorf("schedule kind must be daily, weekly, or cron")
	}
}

type cronSpec struct {
	minute, hour, day, month, weekday map[int]bool
	dayAny, weekdayAny                bool
}

func parseCron(expr string) (cronSpec, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return cronSpec{}, fmt.Errorf("cron must contain five fields")
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	sets := make([]map[int]bool, 5)
	for i := range fields {
		set, err := parseCronField(fields[i], ranges[i][0], ranges[i][1])
		if err != nil {
			return cronSpec{}, fmt.Errorf("cron field %d: %w", i+1, err)
		}
		sets[i] = set
	}
	if sets[4][7] {
		sets[4][0] = true
		delete(sets[4], 7)
	}
	return cronSpec{sets[0], sets[1], sets[2], sets[3], sets[4], fields[2] == "*", fields[4] == "*"}, nil
}

func parseCronField(field string, min, max int) (map[int]bool, error) {
	set := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		step := 1
		base := part
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			base = pieces[0]
			var err error
			step, err = strconv.Atoi(pieces[1])
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("invalid step %q", pieces[1])
			}
		}
		lo, hi := min, max
		switch {
		case base == "*":
		case strings.Contains(base, "-"):
			pieces := strings.Split(base, "-")
			if len(pieces) != 2 {
				return nil, fmt.Errorf("invalid range %q", base)
			}
			var err error
			lo, err = strconv.Atoi(pieces[0])
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", pieces[0])
			}
			hi, err = strconv.Atoi(pieces[1])
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", pieces[1])
			}
		default:
			value, err := strconv.Atoi(base)
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", base)
			}
			lo, hi = value, value
		}
		if lo < min || hi > max || lo > hi {
			return nil, fmt.Errorf("value outside %d-%d", min, max)
		}
		for value := lo; value <= hi; value += step {
			set[value] = true
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("empty field")
	}
	return set, nil
}

func nextSchedule(schedule Schedule, after time.Time) (time.Time, error) {
	expr, err := scheduleExpression(schedule)
	if err != nil {
		return time.Time{}, err
	}
	spec, err := parseCron(expr)
	if err != nil {
		return time.Time{}, err
	}
	candidate := after.Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(2, 0, 0)
	for !candidate.After(limit) {
		dayMatches := spec.day[candidate.Day()]
		weekdayMatches := spec.weekday[int(candidate.Weekday())]
		calendarMatches := dayMatches && weekdayMatches
		if !spec.dayAny && !spec.weekdayAny {
			calendarMatches = dayMatches || weekdayMatches
		}
		if spec.minute[candidate.Minute()] && spec.hour[candidate.Hour()] &&
			spec.month[int(candidate.Month())] && calendarMatches {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("cron has no occurrence in the next two years")
}
