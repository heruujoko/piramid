package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Schedule struct {
	minutes map[int]struct{}
	hours   map[int]struct{}
	dom     map[int]struct{}
	months  map[int]struct{}
	dow     map[int]struct{}
}

func Parse(expr string) (Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return Schedule{}, fmt.Errorf("cron: expected 5 fields, got %d", len(fields))
	}
	minutes, err := parseField(fields[0], 0, 59, "minute")
	if err != nil {
		return Schedule{}, err
	}
	hours, err := parseField(fields[1], 0, 23, "hour")
	if err != nil {
		return Schedule{}, err
	}
	dom, err := parseField(fields[2], 1, 31, "day-of-month")
	if err != nil {
		return Schedule{}, err
	}
	months, err := parseField(fields[3], 1, 12, "month")
	if err != nil {
		return Schedule{}, err
	}
	dow, err := parseField(fields[4], 0, 7, "day-of-week")
	if err != nil {
		return Schedule{}, err
	}
	if _, ok := dow[7]; ok {
		dow[0] = struct{}{}
		delete(dow, 7)
	}
	return Schedule{minutes: minutes, hours: hours, dom: dom, months: months, dow: dow}, nil
}

func Validate(expr string) error {
	_, err := Parse(expr)
	return err
}

func NextAfter(expr string, after time.Time) (time.Time, error) {
	schedule, err := Parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	candidate := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(5, 0, 0)
	for !candidate.After(limit) {
		if schedule.matches(candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("cron: no next fire found within 5 years")
}

func (s Schedule) matches(t time.Time) bool {
	_, minOK := s.minutes[t.Minute()]
	_, hourOK := s.hours[t.Hour()]
	_, domOK := s.dom[t.Day()]
	_, monthOK := s.months[int(t.Month())]
	_, dowOK := s.dow[int(t.Weekday())]
	return minOK && hourOK && domOK && monthOK && dowOK
}

func parseField(field string, min, max int, name string) (map[int]struct{}, error) {
	out := make(map[int]struct{})
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("cron %s: empty list item", name)
		}
		start, end, step, err := parsePart(part, min, max, name)
		if err != nil {
			return nil, err
		}
		for value := start; value <= end; value += step {
			out[value] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("cron %s: no values", name)
	}
	return out, nil
}

func parsePart(part string, min, max int, name string) (int, int, int, error) {
	step := 1
	base := part
	if strings.Contains(part, "/") {
		pieces := strings.Split(part, "/")
		if len(pieces) != 2 || pieces[0] == "" || pieces[1] == "" {
			return 0, 0, 0, fmt.Errorf("cron %s: invalid step %q", name, part)
		}
		base = pieces[0]
		parsedStep, err := strconv.Atoi(pieces[1])
		if err != nil || parsedStep <= 0 {
			return 0, 0, 0, fmt.Errorf("cron %s: invalid step %q", name, part)
		}
		step = parsedStep
	}

	if base == "*" {
		return min, max, step, nil
	}
	if strings.Contains(base, "-") {
		pieces := strings.Split(base, "-")
		if len(pieces) != 2 || pieces[0] == "" || pieces[1] == "" {
			return 0, 0, 0, fmt.Errorf("cron %s: invalid range %q", name, part)
		}
		start, err := strconv.Atoi(pieces[0])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("cron %s: invalid range start %q", name, part)
		}
		end, err := strconv.Atoi(pieces[1])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("cron %s: invalid range end %q", name, part)
		}
		if start > end {
			return 0, 0, 0, fmt.Errorf("cron %s: range start greater than end %q", name, part)
		}
		if start < min || end > max {
			return 0, 0, 0, fmt.Errorf("cron %s: range %q outside %d-%d", name, part, min, max)
		}
		return start, end, step, nil
	}
	value, err := strconv.Atoi(base)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("cron %s: invalid value %q", name, part)
	}
	if value < min || value > max {
		return 0, 0, 0, fmt.Errorf("cron %s: value %d outside %d-%d", name, value, min, max)
	}
	return value, value, step, nil
}
