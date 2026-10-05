package serverstatus

import (
	"sort"
	"strings"
	"time"

	"sdsm/app/backend/internal/models"
)

const (
	timelineMaxWindow = 72 * time.Hour
	timelineMinWindow = 6 * time.Hour
	timelineMaxTicks  = 12
)

// TimelineBar is one connected session, positioned as percentages of the window width.
type TimelineBar struct {
	Left    float64
	Width   float64
	Live    bool
	Admin   bool
	Tooltip string
}

// TimelineRow groups all sessions of a single player.
type TimelineRow struct {
	Name    string
	SteamID string
	Online  bool
	Bars    []TimelineBar
}

// TimelineTick is a labelled gridline on the time axis.
type TimelineTick struct {
	Left     float64
	Label    string
	DayStart bool
}

// PlayerTimeline is the render model for the players-over-time chart.
type PlayerTimeline struct {
	Rows    []TimelineRow
	Ticks   []TimelineTick
	Start   time.Time
	End     time.Time
	NowLeft float64
	HasData bool
}

// BuildPlayerTimeline lays out player sessions on a shared time axis ending at now.
func BuildPlayerTimeline(clients []*models.Client, now time.Time) PlayerTimeline {
	earliest := now
	for _, c := range clients {
		if c != nil && !c.ConnectDatetime.IsZero() && c.ConnectDatetime.Before(earliest) {
			earliest = c.ConnectDatetime
		}
	}
	start := earliest
	if now.Sub(start) > timelineMaxWindow {
		start = now.Add(-timelineMaxWindow)
	}
	if now.Sub(start) < timelineMinWindow {
		start = now.Add(-timelineMinWindow)
	}
	start = start.In(now.Location()).Truncate(time.Hour)
	end := now.Truncate(time.Hour).Add(time.Hour)
	total := end.Sub(start)

	pct := func(t time.Time) float64 {
		v := float64(t.Sub(start)) / float64(total) * 100
		if v < 0 {
			return 0
		}
		if v > 100 {
			return 100
		}
		return v
	}

	tl := PlayerTimeline{Start: start, End: end, NowLeft: pct(now)}

	rowsByKey := map[string]*TimelineRow{}
	var order []string
	firstSeen := map[string]time.Time{}
	for _, c := range clients {
		if c == nil || c.ConnectDatetime.IsZero() {
			continue
		}
		sessionEnd := now
		if c.DisconnectDatetime != nil {
			sessionEnd = *c.DisconnectDatetime
		}
		if sessionEnd.Before(start) || c.ConnectDatetime.After(end) {
			continue
		}
		key := strings.TrimSpace(c.SteamID)
		if key == "" {
			key = "name:" + strings.ToLower(strings.TrimSpace(c.Name))
		}
		row, ok := rowsByKey[key]
		if !ok {
			row = &TimelineRow{Name: c.Name, SteamID: c.SteamID}
			if row.Name == "" {
				row.Name = "Unknown Player"
			}
			rowsByKey[key] = row
			order = append(order, key)
		}
		left := pct(c.ConnectDatetime)
		width := pct(sessionEnd) - left
		if width < 0.3 {
			width = 0.3
		}
		live := c.DisconnectDatetime == nil
		if live {
			row.Online = true
		}
		label := c.ConnectDatetime.In(now.Location()).Format("Jan 2 3:04 PM") + " – "
		if live {
			label += "now"
		} else {
			label += sessionEnd.In(now.Location()).Format("Jan 2 3:04 PM")
		}
		row.Bars = append(row.Bars, TimelineBar{Left: left, Width: width, Live: live, Admin: c.IsAdmin, Tooltip: row.Name + ": " + label})
		if t, ok := firstSeen[key]; !ok || c.ConnectDatetime.Before(t) {
			firstSeen[key] = c.ConnectDatetime
		}
	}

	sort.SliceStable(order, func(i, j int) bool {
		return firstSeen[order[i]].Before(firstSeen[order[j]])
	})
	for _, k := range order {
		tl.Rows = append(tl.Rows, *rowsByKey[k])
	}
	tl.HasData = len(tl.Rows) > 0

	hours := int(total / time.Hour)
	step := 1
	for hours/step > timelineMaxTicks {
		step++
	}
	for _, s := range []int{1, 2, 3, 4, 6, 8, 12, 24} {
		if s >= step {
			step = s
			break
		}
	}
	for t := start; t.Before(end); t = t.Add(time.Duration(step) * time.Hour) {
		tick := TimelineTick{Left: pct(t), Label: t.Format("3 PM"), DayStart: t.Hour() == 0}
		if tick.DayStart || t.Equal(start) {
			tick.Label = t.Format("Jan 2 3 PM")
			tick.DayStart = true
		}
		tl.Ticks = append(tl.Ticks, tick)
	}
	return tl
}
