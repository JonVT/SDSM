package serverstatus

import (
	"testing"
	"time"

	"sdsm/app/backend/internal/models"
)

func TestBuildPlayerTimeline(t *testing.T) {
	now := time.Date(2026, 10, 4, 18, 30, 0, 0, time.UTC)
	d1 := now.Add(-3 * time.Hour)
	d2 := now.Add(-1 * time.Hour)
	d3 := now.Add(-190 * time.Hour)
	clients := []*models.Client{
		{SteamID: "1", Name: "a", ConnectDatetime: now.Add(-5 * time.Hour), DisconnectDatetime: &d1},
		{SteamID: "1", Name: "a", ConnectDatetime: now.Add(-2 * time.Hour)},
		{SteamID: "2", Name: "b", ConnectDatetime: now.Add(-4 * time.Hour), DisconnectDatetime: &d2},
		{SteamID: "3", Name: "old", ConnectDatetime: now.Add(-200 * time.Hour), DisconnectDatetime: &d3},
	}
	tl := BuildPlayerTimeline(clients, now)
	if len(tl.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(tl.Rows))
	}
	if len(tl.Rows[0].Bars) != 2 || !tl.Rows[0].Online {
		t.Fatalf("player a should have two bars and be online: %+v", tl.Rows[0])
	}
	for _, r := range tl.Rows {
		for _, b := range r.Bars {
			if b.Left < 0 || b.Left+b.Width > 100.0001 {
				t.Fatalf("bar out of range: %+v", b)
			}
		}
	}
	if empty := BuildPlayerTimeline(nil, now); empty.HasData {
		t.Fatal("expected no data")
	}
}

func TestBuildPlayerTimelineRange(t *testing.T) {
	now := time.Date(2026, 10, 4, 18, 30, 0, 0, time.UTC)
	d := now.Add(-9 * time.Hour)
	clients := []*models.Client{
		{SteamID: "1", Name: "a", ConnectDatetime: now.Add(-10 * time.Hour), DisconnectDatetime: &d},
		{SteamID: "2", Name: "b", ConnectDatetime: now.Add(-1 * time.Hour)},
	}
	rng := TimelineRange{Start: now.Add(-12 * time.Hour), End: now.Add(-6 * time.Hour)}
	tl := BuildPlayerTimelineRange(clients, now, rng)
	if !tl.Custom || len(tl.Rows) != 1 || tl.Rows[0].Name != "a" {
		t.Fatalf("unexpected custom timeline: %+v", tl)
	}
	if tl.NowLeft != -1 {
		t.Fatalf("now marker should be hidden outside the window, got %v", tl.NowLeft)
	}
	if tl.StartUnix != rng.Start.Unix() || tl.EndUnix != rng.End.Unix() {
		t.Fatalf("window not echoed back")
	}
}
