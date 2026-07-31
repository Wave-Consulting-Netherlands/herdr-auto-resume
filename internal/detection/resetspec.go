package detection

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/terminal"
)

type ResetKind string

const (
	ResetKindAbsolute   ResetKind = "absolute"
	ResetKindLocalClock ResetKind = "local-clock"
	ResetKindRelative   ResetKind = "relative"
	ResetKindDateTime   ResetKind = "date-time"
	ResetKindUnknown    ResetKind = "unknown"
)

type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

type ResetSpec struct {
	Kind       ResetKind
	Raw        string
	Timezone   string
	ParsedTime time.Time
	Confidence Confidence
}

var (
	clockPattern    = regexp.MustCompile(`(?i)^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)
	datePattern     = regexp.MustCompile(`(?i)^(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+(\d{1,2})(?:,\s*(\d{4}))?\s*(?:at\s+|,\s*|\s+)(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)
	weekdayPattern  = regexp.MustCompile(`(?i)^(sun(?:day)?|mon(?:day)?|tue(?:sday)?|wed(?:nesday)?|thu(?:rsday)?|fri(?:day)?|sat(?:urday)?)\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)
	durationPattern = regexp.MustCompile(`(?i)(\d+)\s*(days?|hours?|h|minutes?|mins?|m)\b`)
	tzPattern       = regexp.MustCompile(`\s*\(([^()]*)\)\s*$`)
	markerPattern   = regexp.MustCompile(`(?i)\b(?:resets?|try\s+again|wait)\b`)
)

var abbreviationLocations = map[string]string{
	"UTC": "UTC", "GMT": "UTC", "BST": "Europe/London",
	"CET": "Europe/Paris", "CEST": "Europe/Paris",
	"EST": "America/New_York", "EDT": "America/New_York", "ET": "America/New_York",
	"PST": "America/Los_Angeles", "PDT": "America/Los_Angeles", "PT": "America/Los_Angeles",
	"CST": "America/Chicago", "CDT": "America/Chicago", "CT": "America/Chicago",
	"MST": "America/Denver", "MDT": "America/Denver", "MT": "America/Denver",
}

// ParseReset parses a reset expression using only the supplied clock's
// location. It returns a zero ParsedTime for malformed or implausible input.
func ParseReset(text string, now time.Time) ResetSpec {
	raw := resetExpression(text)
	spec := ResetSpec{Kind: ResetKindUnknown, Raw: raw, Confidence: ConfidenceLow}
	if raw == "" {
		return spec
	}

	if duration, ok := parseDuration(raw); ok {
		if duration <= 0 || duration > 370*24*time.Hour {
			return spec
		}
		spec.Kind = ResetKindRelative
		spec.Confidence = ConfidenceHigh
		spec.ParsedTime = now.Add(duration)
		return spec
	}

	zoneText := ""
	if match := tzPattern.FindStringSubmatch(raw); match != nil {
		zoneText = strings.TrimSpace(match[1])
		raw = strings.TrimSpace(raw[:len(raw)-len(match[0])])
		spec.Raw = strings.TrimSpace(spec.Raw)
	}
	loc, zone, confidence := resetLocation(zoneText, now.Location())
	spec.Timezone = zone
	spec.Confidence = confidence
	localNow := now.In(loc)

	if match := datePattern.FindStringSubmatch(raw); match != nil {
		month, ok := parseMonth(match[1])
		if !ok {
			return spec
		}
		day, year, hour, minute, ok := parseDateClock(match[2], match[3], match[4], match[5], match[6])
		if !ok {
			return spec
		}
		if year == 0 {
			year = localNow.Year()
		}
		candidate := dateInLocation(year, month, day, hour, minute, loc)
		if candidate.Year() != year || candidate.Month() != month || candidate.Day() != day {
			return spec
		}
		if candidate.Before(localNow) {
			if match[3] != "" {
				return spec
			}
			candidate = dateInLocation(year+1, month, day, hour, minute, loc)
		}
		if candidate.Sub(now) > 370*24*time.Hour {
			return spec
		}
		spec.Kind = ResetKindDateTime
		if zoneText != "" && confidence == ConfidenceLow {
			spec.Confidence = ConfidenceLow
		} else if zoneText != "" {
			spec.Confidence = confidence
		} else {
			spec.Confidence = ConfidenceHigh
		}
		spec.ParsedTime = candidate
		return spec
	}

	if match := weekdayPattern.FindStringSubmatch(raw); match != nil {
		weekday, ok := parseWeekday(match[1])
		if !ok {
			return spec
		}
		hour, minute, ok := parseClock(match[2], match[3], match[4])
		if !ok {
			return spec
		}
		candidate := dateInLocation(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, loc)
		delta := (int(weekday) - int(localNow.Weekday()) + 7) % 7
		if delta == 0 && !candidate.After(localNow) {
			delta = 7
		}
		candidate = dateInLocation(localNow.Year(), localNow.Month(), localNow.Day()+delta, hour, minute, loc)
		if candidate.Sub(now) > 370*24*time.Hour {
			return spec
		}
		spec.Kind = ResetKindDateTime
		spec.Confidence = ConfidenceMedium
		spec.ParsedTime = candidate
		return spec
	}

	if match := clockPattern.FindStringSubmatch(raw); match != nil {
		hour, minute, ok := parseClock(match[1], match[2], match[3])
		if !ok {
			return spec
		}
		candidate := dateInLocation(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, loc)
		candidate = nextClockOccurrence(candidate, localNow, loc)
		if candidate.Sub(now) > 370*24*time.Hour {
			return spec
		}
		spec.Kind = ResetKindLocalClock
		if zoneText != "" && confidence != ConfidenceLow {
			spec.Kind = ResetKindAbsolute
			spec.Confidence = confidence
		}
		if match[3] == "" && hour <= 12 {
			spec.Confidence = ConfidenceMedium
		} else if match[3] == "" && zoneText == "" {
			spec.Confidence = ConfidenceHigh
		} else if zoneText == "" {
			spec.Confidence = ConfidenceHigh
		}
		spec.ParsedTime = candidate
		return spec
	}
	return spec
}

func resetExpression(text string) string {
	text = strings.TrimSpace(terminal.StripANSI(text))
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if match := markerPattern.FindStringIndex(line); match != nil {
			value := strings.TrimSpace(line[match[1]:])
			value = strings.TrimSpace(strings.TrimLeft(value, ":·.-"))
			if !strings.Contains(value, "(") {
				value = strings.TrimSuffix(value, ")")
			}
			if value != "" {
				return strings.TrimSpace(strings.TrimRight(value, "·."))
			}
		}
	}
	return text
}

func parseDuration(raw string) (time.Duration, bool) {
	matches := durationPattern.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return 0, false
	}
	if !strings.Contains(strings.ToLower(raw), "in") && !strings.Contains(strings.ToLower(raw), "wait") &&
		!strings.Contains(strings.ToLower(raw), "again") && len(matches) == 1 && !regexp.MustCompile(`(?i)^\d+\s*(?:h|m|hours?|minutes?|mins?)$`).MatchString(strings.TrimSpace(raw)) {
		return 0, false
	}
	var total time.Duration
	for _, match := range matches {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			return 0, false
		}
		unit := strings.ToLower(match[2])
		var multiplier time.Duration
		switch unit[0] {
		case 'd':
			multiplier = 24 * time.Hour
		case 'h':
			multiplier = time.Hour
		case 'm':
			multiplier = time.Minute
		}
		total += time.Duration(n) * multiplier
	}
	return total, true
}

func resetLocation(zoneText string, fallback *time.Location) (*time.Location, string, Confidence) {
	if zoneText == "" {
		return fallback, "", ConfidenceHigh
	}
	if loc, err := time.LoadLocation(zoneText); err == nil {
		return loc, zoneText, ConfidenceHigh
	}
	if mapped, ok := abbreviationLocations[strings.ToUpper(zoneText)]; ok {
		loc, err := time.LoadLocation(mapped)
		if err == nil {
			return loc, mapped, ConfidenceMedium
		}
	}
	return fallback, zoneText, ConfidenceLow
}

func parseClock(hourText, minuteText, meridiem string) (int, int, bool) {
	hour, err := strconv.Atoi(hourText)
	if err != nil || hour > 23 {
		return 0, 0, false
	}
	minute := 0
	if minuteText != "" {
		minute, err = strconv.Atoi(minuteText)
		if err != nil || minute > 59 {
			return 0, 0, false
		}
	}
	if meridiem != "" {
		if hour > 12 || hour == 0 {
			return 0, 0, false
		}
		if strings.EqualFold(meridiem, "pm") && hour != 12 {
			hour += 12
		} else if strings.EqualFold(meridiem, "am") && hour == 12 {
			hour = 0
		}
	}
	return hour, minute, true
}

func parseDateClock(dayText, yearText, hourText, minuteText, meridiem string) (int, int, int, int, bool) {
	day, err := strconv.Atoi(dayText)
	if err != nil || day < 1 || day > 31 {
		return 0, 0, 0, 0, false
	}
	year := 0
	if yearText != "" {
		year, err = strconv.Atoi(yearText)
		if err != nil {
			return 0, 0, 0, 0, false
		}
	}
	hour, minute, ok := parseClock(hourText, minuteText, meridiem)
	return day, year, hour, minute, ok
}

func parseMonth(value string) (time.Month, bool) {
	value = strings.ToLower(value)
	for month := time.January; month <= time.December; month++ {
		if strings.HasPrefix(strings.ToLower(month.String()), value) || strings.HasPrefix(value, strings.ToLower(month.String())[:3]) {
			return month, true
		}
	}
	return 0, false
}

func parseWeekday(value string) (time.Weekday, bool) {
	value = strings.ToLower(value)
	for day := time.Sunday; day <= time.Saturday; day++ {
		name := strings.ToLower(day.String())
		if strings.HasPrefix(name, value) || strings.HasPrefix(value, name[:3]) {
			return day, true
		}
	}
	return 0, false
}

func dateInLocation(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	candidate := time.Date(year, month, day, hour, minute, 0, 0, loc)
	// time.Date chooses the earlier side of a repeated fall-back hour. Choose
	// the later instant when the same wall clock renders one hour later.
	later := candidate.Add(time.Hour)
	if sameWallClock(candidate, later) {
		return later
	}
	return candidate
}

func sameWallClock(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day() &&
		a.Hour() == b.Hour() && a.Minute() == b.Minute()
}

func nextClockOccurrence(candidate, now time.Time, loc *time.Location) time.Time {
	if candidate.Before(now) && now.Sub(candidate) > time.Hour {
		return dateInLocation(now.Year(), now.Month(), now.Day()+1, candidate.Hour(), candidate.Minute(), loc)
	}
	return candidate
}
