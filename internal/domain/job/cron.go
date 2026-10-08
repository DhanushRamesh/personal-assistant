package job

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron : The clock form for the patterns the plain words cannot
// reach.
//
//	cron 0 9 * * 1-5      nine in the morning, Monday to Friday
//	cron */15 9-17 * * *  every quarter of an hour, nine till five
//	cron 0 7 1 * *        seven o'clock on the first of the month
//	cron 30 20 * * sun    half past eight on a Sunday evening
//
// Owner, 8 October 2026: *"I want the time to be in cron style,
// because it can cover every kind of pattern."*
//
// "every 30m" and "at 08:00" cover what somebody says out loud and
// are read back to them in the same words, so they stay. What they
// could never say is *when not to*: "at 08:00" is every day of the
// year, and a weekday rule had to wake every morning and decline on
// the two it was never meant to run on. A condition can stop a rule
// but it cannot stop it waking, and a rule that wakes to say no is
// a rule whose next time is a lie.
//
// Five fields, as everywhere else: minute, hour, day of the month,
// month, day of the week. Each takes a number, a range "9-17", a
// list "1,15", a step "*/15", a range with a step "9-17/2", or "*"
// for all of them. Months may be named jan to dec and days sun to
// sat, since those are what somebody writing it will reach for.
// Sunday is 0, and 7 as well, which is what every other cron does.
const Cron = "cron"

// Crontab : A cron pattern, as five sets of allowed values.
//
// Held as bits rather than parsed each time: a pattern is read when
// the rule is written and asked for its next moment on every pass
// of the clock, and sixty-odd numbers fit in a word.
type Crontab struct {
	minute, hour, dom, month, dow uint64
	// anyDOM, anyDOW : Whether the day fields were left as "*".
	//
	// Kept because the two day fields do not behave like the
	// others. See [Crontab.day].
	anyDOM, anyDOW bool
}

// horizon : How far ahead a pattern is searched before it is called
// unsatisfiable.
//
// Five years. "cron 0 0 30 2 *" -- the thirtieth of February -- is
// a pattern that parses perfectly and can never happen, and
// searching for its next moment would otherwise never return.
const horizon = 5 * 366 * 24 * time.Hour

var months = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var weekdays = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// ReadCron : A cron pattern, or why it could not be read.
//
// Read when the rule is written, so that a pattern nobody could
// satisfy fails in front of whoever wrote it rather than becoming a
// rule that quietly never runs.
func ReadCron(s string) (Crontab, error) {
	f := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	if len(f) > 0 && f[0] == Cron {
		f = f[1:]
	}
	if len(f) != 5 {
		return Crontab{}, fmt.Errorf(
			"%w: a cron pattern is five fields -- minute, hour, day, month, weekday -- and %q is %d",
			ErrUnreadable, s, len(f))
	}

	var c Crontab
	var err error
	if c.minute, _, err = field(f[0], 0, 59, nil); err != nil {
		return Crontab{}, fmt.Errorf("%w (minute)", err)
	}
	if c.hour, _, err = field(f[1], 0, 23, nil); err != nil {
		return Crontab{}, fmt.Errorf("%w (hour)", err)
	}
	if c.dom, c.anyDOM, err = field(f[2], 1, 31, nil); err != nil {
		return Crontab{}, fmt.Errorf("%w (day of the month)", err)
	}
	if c.month, _, err = field(f[3], 1, 12, months); err != nil {
		return Crontab{}, fmt.Errorf("%w (month)", err)
	}
	if c.dow, c.anyDOW, err = field(f[4], 0, 7, weekdays); err != nil {
		return Crontab{}, fmt.Errorf("%w (day of the week)", err)
	}
	// Seven is Sunday too, so a pattern written either way means
	// the same day.
	if c.dow&(1<<7) != 0 {
		c.dow = c.dow&^(1<<7) | 1
	}
	return c, nil
}

// field : One cron field as a set of allowed values, and whether it
// was left as "*".
func field(spec string, lo, hi int, names map[string]int) (uint64, bool, error) {
	var bits uint64
	all := true
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return 0, false, fmt.Errorf("%w: %q has an empty part", ErrUnreadable, spec)
		}
		var step = 1
		if base, after, found := strings.Cut(part, "/"); found {
			n, err := strconv.Atoi(after)
			if err != nil || n <= 0 {
				return 0, false, fmt.Errorf("%w: %q is not a step", ErrUnreadable, after)
			}
			part, step = base, n
		}

		from, to := lo, hi
		switch {
		case part == "*":
		default:
			all = false
			first, last, ranged := strings.Cut(part, "-")
			var err error
			if from, err = number(first, lo, hi, names); err != nil {
				return 0, false, err
			}
			to = from
			if ranged {
				if to, err = number(last, lo, hi, names); err != nil {
					return 0, false, err
				}
			} else if step > 1 {
				// "5/10" is every tenth from five onwards, which
				// is what cron has always meant by it.
				to = hi
			}
			if to < from {
				return 0, false, fmt.Errorf("%w: %q runs backwards", ErrUnreadable, part)
			}
		}
		if step > 1 {
			all = false
		}
		for v := from; v <= to; v += step {
			bits |= 1 << uint(v)
		}
	}
	if bits == 0 {
		return 0, false, fmt.Errorf("%w: %q matches nothing", ErrUnreadable, spec)
	}
	return bits, all, nil
}

// number : One value in a field, by number or by name.
func number(s string, lo, hi int, names map[string]int) (int, error) {
	s = strings.TrimSpace(s)
	if names != nil {
		if n, named := names[s]; named {
			return n, nil
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%w: %q is not between %d and %d", ErrUnreadable, s, lo, hi)
	}
	return n, nil
}

// has : Whether a set allows a value.
func has(bits uint64, v int) bool { return v >= 0 && v < 64 && bits&(1<<uint(v)) != 0 }

// day : Whether a date is a day this pattern runs on.
//
// The one rule in cron that surprises everybody, kept rather than
// fixed because every other cron behaves this way and a pattern
// copied from anywhere else has to mean here what it means there.
//
// When both day fields are given, a date matching *either* runs.
// "cron 0 9 1 * mon" is the first of the month and every Monday,
// not the Mondays that fall on the first. When only one is given it
// alone decides, which is the ordinary case and behaves as anybody
// would expect.
func (c Crontab) day(t time.Time) bool {
	dom, dow := has(c.dom, t.Day()), has(c.dow, int(t.Weekday()))
	switch {
	case c.anyDOM && c.anyDOW:
		return true
	case c.anyDOM:
		return dow
	case c.anyDOW:
		return dom
	}
	return dom || dow
}

// Next : The first moment this pattern names after the one given.
//
// Strictly after, so that a rule which has just run is given its
// following moment and not the one it is standing on.
//
// Searched by skipping rather than by minutes: a wrong month jumps
// to the first of the next one, a wrong day to midnight tomorrow, a
// wrong hour to the top of the next. A pattern naming one minute a
// year is found in a few hundred steps rather than half a million.
//
// Built through time.Date in the person's own zone each time, so an
// hour that does not exist on a clock-change morning is carried to
// the one that does rather than silently skipped.
func (c Crontab) Next(after time.Time, where *time.Location) (time.Time, error) {
	if where == nil {
		where = time.UTC
	}
	t := after.In(where).Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(horizon)

	for t.Before(limit) {
		switch {
		case !has(c.month, int(t.Month())):
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, where).AddDate(0, 1, 0)
		case !c.day(t):
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, where).AddDate(0, 0, 1)
		case !has(c.hour, t.Hour()):
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, where).Add(time.Hour)
		case !has(c.minute, t.Minute()):
			t = t.Add(time.Minute)
		default:
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf(
		"%w: that pattern has no next moment -- it names a date that never comes", ErrUnreadable)
}
