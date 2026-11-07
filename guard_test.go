package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestGuardThresholdMarkerAndResume(t *testing.T) {
	f := newFake(t)
	free := int64(100 * gib)
	tool := testTool(t, f.URL, "good", &free)
	if _, _, err := cli(t, tool, "config", "set", "guard.mode", "allowance"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cli(t, tool, "guard", "check"); err == nil || !strings.Contains(err.Error(), "no baseline") {
		t.Fatalf("check without baseline: %v", err)
	}
	if _, _, err := cli(t, tool, "guard", "rebase"); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		free   int64
		want   string
		pauses int
	}{
		{95 * gib, "free=95.0 baseline=100.0 used=5.0 limit=10.0 action=ok", 0},
		{90 * gib, "action=ok", 0}, // exactly the limit is still ok
		{89 * gib, "free=89.0 baseline=100.0 used=11.0 limit=10.0 action=paused downloads=1 left-alone=2", 1},
		{80 * gib, "action=skipped-already-paused downloads=0 left-alone=3", 1}, // over the limit: looks for new downloads, finds none
	}
	for _, s := range steps {
		free = s.free
		out, _, err := cli(t, tool, "guard", "check")
		if err != nil || out != "2026-10-25T12:30:00-0400 "+strings.TrimPrefix(out, "2026-10-25T12:30:00-0400 ") || !strings.Contains(out, s.want) {
			t.Fatalf("free %d: %v %q", s.free, err, out)
		}
		if n := strings.Count(strings.Join(f.posted(), "|"), "torrents/stop aaa"); n != s.pauses {
			t.Fatalf("free %d: %d pauses, want %d", s.free, n, s.pauses)
		}
	}
	if m := tool.marker(); m != "2026-10-25T12:30:00-0400" {
		t.Fatalf("marker %q", m)
	}
	out, _, err := cli(t, tool, "guard", "status")
	if err != nil || !strings.Contains(out, "paused    by the guard since 2026-10-25T12:30:00-0400") || !strings.Contains(out, "used      20.0 GiB of 10.0 GiB limit") {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if _, _, err := cli(t, tool, "guard", "resume"); err != nil {
		t.Fatal(err)
	}
	if tool.marker() != "" || !strings.HasSuffix(strings.Join(f.posted(), "|"), "torrents/start aaa") {
		t.Fatalf("resume left marker %q, posted %v", tool.marker(), f.posted())
	}
	out, _, _ = cli(t, tool, "guard", "log", "3")
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 3 || !strings.HasSuffix(lines[2], "action=resumed downloads=1 forced=0") {
		t.Fatalf("log: %q", out)
	}
	free = 50 * gib
	if out, _, _ := cli(t, tool, "guard", "check", "--limit", "60G"); !strings.Contains(out, "limit=60.0 action=ok") {
		t.Fatalf("--limit: %q", out)
	}
}

func TestGuardFloor(t *testing.T) {
	f := newFake(t)
	free := int64(30 * gib)
	tool := testTool(t, f.URL, "good", &free)
	steps := []struct {
		free   int64
		action string
		marker bool
		posted string
	}{
		{30 * gib, "ok", false, ""},
		{4 * gib, "paused downloads=1 left-alone=2", true, "torrents/stop aaa"}, // below min: the download only
		{5 * gib, "skipped-already-paused", true, ""},                           // exactly min, still paused
		{20 * gib, "skipped-already-paused", true, ""},                          // between: no change
		{21 * gib, "resumed downloads=1 forced=0", false, "torrents/start aaa"}, // above resume, the guard paused
		{15 * gib, "ok", false, ""},                                             // between, not paused: no change
		{5 * gib, "ok", false, ""},                                              // exactly min is ok
	}
	for _, s := range steps {
		free = s.free
		before := len(f.posted())
		out, _, err := cli(t, tool, "guard", "check")
		want := fmt.Sprintf("2026-10-25T12:30:00-0400 mode=floor min=5.0 resume=20.0 free=%.1f baseline=0.0 used=0.0 limit=10.0 action=%s\n", float64(s.free)/gib, s.action)
		if err != nil || out != want {
			t.Fatalf("free %d: %v\n%q\nwant %q", s.free, err, out, want)
		}
		if (tool.marker() != "") != s.marker || strings.Join(f.posted()[before:], "|") != s.posted {
			t.Fatalf("free %d: marker %q posted %v", s.free, tool.marker(), f.posted()[before:])
		}
	}
	// a pause the guard did not make is never resumed by it
	free = 100 * gib
	before := len(f.posted())
	if out, _, _ := cli(t, tool, "guard", "check"); !strings.HasSuffix(out, "action=ok\n") || len(f.posted()) != before {
		t.Fatalf("resumed what it did not pause: %q", out)
	}
	if out, _, _ := cli(t, tool, "guard", "check", "--min-free", "120GiB", "--resume-above", "200G"); !strings.Contains(out, "min=120.0 resume=200.0 free=100.0") || !strings.HasSuffix(out, "action=paused downloads=1 left-alone=2\n") {
		t.Fatalf("flags: %q", out)
	}
	if _, _, err := cli(t, tool, "config", "set", "guard.minFree", "25G"); err == nil || !strings.Contains(err.Error(), "must be above minFree") {
		t.Fatalf("minFree above resumeAbove: %v", err)
	}
	if _, _, err := cli(t, tool, "config", "set", "guard.mode", "panic"); err == nil {
		t.Fatal("bad mode accepted")
	}
	for _, kv := range [][2]string{{"guard.resumeAbove", "150G"}, {"guard.minFree", "50G"}} {
		if _, _, err := cli(t, tool, "config", "set", kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	out, _, err := cli(t, tool, "guard", "status")
	for _, want := range []string{"mode      floor", "min free  50.0 GiB (pause below)", "resume    above 150.0 GiB when the guard paused", "free      100.0 GiB on nas:/volume1", "paused    by the guard since 2026-10-25T12:30:00-0400"} {
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("status lacks %q: %v\n%s", want, err, out)
		}
	}
	if strings.Contains(out, "\nbaseline ") {
		t.Errorf("floor status shows the baseline:\n%s", out)
	}
	out, _, err = cli(t, tool, "guard", "rebase")
	if err != nil || !strings.Contains(out, "action=rebase\nnote: the guard is in floor mode (pause below 50.0 GiB); the baseline only matters in allowance mode") {
		t.Fatalf("rebase in floor mode: %v %q", err, out)
	}
	out, _, _ = cli(t, tool, "config", "show")
	if !strings.Contains(out, "guard     mode floor, minFree 50.0 GiB, resumeAbove 150.0 GiB") {
		t.Errorf("config show:\n%s", out)
	}
}

func TestGuardWithoutPasswordLogsNoCredentials(t *testing.T) {
	f := newFake(t)
	free := int64(50 * gib)
	tool := testTool(t, f.URL, "", &free)
	if _, _, err := cli(t, tool, "guard", "rebase", "--bytes", "107374182400"); err != nil {
		t.Fatal(err)
	}
	out, _, err := cli(t, tool, "guard", "check")
	if err != nil || !strings.HasSuffix(strings.TrimSpace(out), "used=50.0 limit=10.0 action=error:no-credentials") {
		t.Fatalf("%v %q", err, out)
	}
	if f.requests() != 0 || tool.marker() != "" {
		t.Fatalf("requests %d marker %q", f.requests(), tool.marker())
	}
	data, _ := os.ReadFile(tool.guardLog())
	if !strings.Contains(string(data), "action=rebase\n") || !strings.Contains(string(data), "action=error:no-credentials\n") {
		t.Fatalf("log:\n%s", data)
	}
}

func TestParseSizeAndDF(t *testing.T) {
	for in, want := range map[string]int64{"10G": 10 << 30, "10GiB": 10 << 30, "512m": 512 << 20, "1024": 1024, "1.5T": 3 << 39} {
		if got, err := parseSize(in); err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v", in, got, err)
		}
	}
	if _, err := parseSize("ten"); err == nil {
		t.Error("parseSize(ten) ok")
	}
	if n, err := parseDF("       Avail\n619700166656\n"); err != nil || n != 619700166656 {
		t.Errorf("parseDF = %d, %v", n, err)
	}
}

func TestGuardPlist(t *testing.T) {
	tool := testTool(t, "http://127.0.0.1:1", "", nil)
	p := tool.GuardPlist()
	for _, want := range []string{"<string>" + GuardLabel + "</string>", "<string>" + tool.Home + "/bin/o-qbt</string>\n    <string>guard</string>\n    <string>check</string>", "<integer>300</integer>", "<key>RunAtLoad</key>\n  <true/>"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
}

// guardTorrents is a library in every state the guard tells apart: five
// downloads (one forced), four seeds, two stopped by hand, one checking
// its resume data and one errored. Names sort as the hashes do.
var guardTorrents = []map[string]any{
	{"hash": "d1", "name": "d1", "state": "downloading", "progress": 0.5},
	{"hash": "d2", "name": "d2", "state": "stalledDL", "progress": 0.2},
	{"hash": "d3", "name": "d3", "state": "forcedDL", "progress": 0.7},
	{"hash": "d4", "name": "d4", "state": "queuedDL", "progress": 0.0},
	{"hash": "d5", "name": "d5", "state": "metaDL", "progress": 0.0},
	{"hash": "e1", "name": "e1", "state": "error", "progress": 0.3},
	{"hash": "k1", "name": "k1", "state": "checkingResumeData", "progress": 1.0},
	{"hash": "s1", "name": "s1", "state": "stalledUP", "progress": 1.0},
	{"hash": "s2", "name": "s2", "state": "uploading", "progress": 1.0},
	{"hash": "s3", "name": "s3", "state": "forcedUP", "progress": 1.0},
	{"hash": "s4", "name": "s4", "state": "queuedUP", "progress": 1.0},
	{"hash": "x1", "name": "x1", "state": "stoppedDL", "progress": 0.4},
	{"hash": "x2", "name": "x2", "state": "stoppedUP", "progress": 1.0},
}

// guardFake is a fake holding guardTorrents.
func guardFake(t *testing.T) *fakeQBT {
	f := newFake(t)
	f.list = fakeList(guardTorrents)
	return f
}

// wantStates fails unless each hash has its state in f.
func wantStates(t *testing.T, f *fakeQBT, want map[string]string) {
	t.Helper()
	for h, s := range want {
		if got := f.state(h); got != s {
			t.Errorf("%s is %s, want %s", h, got, s)
		}
	}
}

// seedsRunning are the states of every torrent the guard must never touch.
var seedsRunning = map[string]string{"s1": "stalledUP", "s2": "uploading", "s3": "forcedUP", "s4": "queuedUP", "k1": "checkingResumeData", "e1": "error", "x1": "stoppedDL", "x2": "stoppedUP"}

func TestGuardFloorPausesOnlyDownloads(t *testing.T) {
	f := guardFake(t)
	free := int64(4 * gib)
	tool := testTool(t, f.URL, "good", &free)
	out, _, err := cli(t, tool, "guard", "check")
	if err != nil || !strings.HasSuffix(out, "free=4.0 baseline=0.0 used=0.0 limit=10.0 action=paused downloads=5 left-alone=8\n") {
		t.Fatalf("pause: %v %q", err, out)
	}
	if got := strings.Join(f.posted(), "|"); got != "torrents/stop d1|d2|d3|d4|d5" {
		t.Fatalf("posted %s", got)
	}
	wantStates(t, f, seedsRunning)
	m, ok := tool.readMarker()
	if !ok || m.Legacy || m.Time != "2026-10-25T12:30:00-0400" || len(m.Torrents) != 5 {
		t.Fatalf("marker %+v", m)
	}
	for _, p := range m.Torrents {
		if p.Forced != (p.Hash == "d3") || p.State != guardTorrents[slices.IndexFunc(guardTorrents, func(x map[string]any) bool { return x["hash"] == p.Hash })]["state"] {
			t.Errorf("marker entry %+v", p)
		}
	}
	if tool.marker() != "2026-10-25T12:30:00-0400" {
		t.Errorf("marker time %q", tool.marker())
	}
	// The owner stops a seed by hand, and an *arr adds a download.
	if _, _, err := cli(t, tool, "torrent", "pause", "--target", "s2"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.list = append(f.list, map[string]any{"hash": "n1", "name": "n1", "state": "downloading", "progress": 0.0})
	f.mu.Unlock()
	before := len(f.posted())
	out, _, _ = cli(t, tool, "guard", "check")
	if !strings.HasSuffix(out, "action=paused downloads=1 left-alone=13\n") || strings.Join(f.posted()[before:], "|") != "torrents/stop n1" {
		t.Fatalf("still below the floor, new download: %q %v", out, f.posted()[before:])
	}
	if m, _ := tool.readMarker(); len(m.Torrents) != 6 || m.Time != "2026-10-25T12:30:00-0400" {
		t.Fatalf("marker after the second pause %+v", m)
	}
	out, _, _ = cli(t, tool, "guard", "status")
	if !strings.Contains(out, "paused    by the guard since 2026-10-25T12:30:00-0400: 6 downloads; seeds left alone (o qbt guard resume)") {
		t.Errorf("status:\n%s", out)
	}
	free = 10 * gib
	before = len(f.posted())
	if out, _, _ = cli(t, tool, "guard", "check"); !strings.HasSuffix(out, "action=skipped-already-paused\n") || len(f.posted()) != before {
		t.Fatalf("between: %q", out)
	}
	free = 21 * gib
	out, _, _ = cli(t, tool, "guard", "check")
	if !strings.HasSuffix(out, "action=resumed downloads=6 forced=1\n") {
		t.Fatalf("resume: %q", out)
	}
	if got := strings.Join(f.posted()[before:], "|"); got != "torrents/start d1|d2|d3|d4|d5|n1|torrents/setForceStart d3" {
		t.Fatalf("resume posted %s", got)
	}
	want := map[string]string{"d1": "downloading", "d2": "stalledDL", "d3": "forcedDL", "d4": "queuedDL", "d5": "metaDL", "n1": "downloading"}
	maps.Copy(want, seedsRunning)
	want["s2"] = "stoppedUP" // stopped by the owner: stays stopped
	wantStates(t, f, want)
	if tool.marker() != "" {
		t.Fatalf("marker left: %q", tool.marker())
	}
}

func TestGuardAllowancePausesOnlyDownloads(t *testing.T) {
	f := guardFake(t)
	free := int64(100 * gib)
	tool := testTool(t, f.URL, "good", &free)
	for _, args := range [][]string{{"config", "set", "guard.mode", "allowance"}, {"guard", "rebase"}} {
		if _, _, err := cli(t, tool, args...); err != nil {
			t.Fatal(err)
		}
	}
	free = 89 * gib
	if out, _, err := cli(t, tool, "guard", "check"); err != nil || !strings.HasSuffix(out, "used=11.0 limit=10.0 action=paused downloads=5 left-alone=8\n") {
		t.Fatalf("pause: %v %q", err, out)
	}
	wantStates(t, f, seedsRunning)
	free = 95 * gib
	before := len(f.posted())
	if out, _, _ := cli(t, tool, "guard", "check"); !strings.HasSuffix(out, "action=skipped-already-paused\n") || len(f.posted()) != before {
		t.Fatalf("within the limit, paused: %q", out)
	}
	out, _, err := cli(t, tool, "guard", "resume")
	if err != nil || !strings.HasSuffix(out, "action=resumed downloads=5 forced=1\n") {
		t.Fatalf("resume: %v %q", err, out)
	}
	if got := strings.Join(f.posted()[before:], "|"); got != "torrents/start d1|d2|d3|d4|d5|torrents/setForceStart d3" {
		t.Fatalf("resume posted %s", got)
	}
	want := map[string]string{"d1": "downloading", "d3": "forcedDL"}
	maps.Copy(want, seedsRunning)
	wantStates(t, f, want)
}

func TestGuardNothingDownloadingWritesNoMarker(t *testing.T) {
	f := newFake(t)
	f.list = fakeList(slices.DeleteFunc(slices.Clone(guardTorrents), func(x map[string]any) bool { return strings.HasPrefix(x["hash"].(string), "d") }))
	free := int64(4 * gib)
	tool := testTool(t, f.URL, "good", &free)
	for range 2 {
		out, _, err := cli(t, tool, "guard", "check")
		if err != nil || !strings.HasSuffix(out, "action=no-downloads downloads=0 left-alone=8\n") || tool.marker() != "" || len(f.posted()) != 0 {
			t.Fatalf("%v %q marker %q posted %v", err, out, tool.marker(), f.posted())
		}
	}
	// a download that starts later is stopped by the next check
	f.mu.Lock()
	f.list = append(f.list, map[string]any{"hash": "n1", "name": "n1", "state": "stalledDL", "progress": 0.1})
	f.mu.Unlock()
	if out, _, _ := cli(t, tool, "guard", "check"); !strings.HasSuffix(out, "action=paused downloads=1 left-alone=8\n") || tool.marker() == "" {
		t.Fatalf("late download: %q", out)
	}
	wantStates(t, f, seedsRunning)
}

func TestGuardLegacyMarker(t *testing.T) {
	f := guardFake(t)
	free := int64(30 * gib)
	tool := testTool(t, f.URL, "good", &free)
	if err := os.MkdirAll(tool.DataDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := func() {
		if err := os.WriteFile(tool.markerPath(), []byte("2026-10-20T01:00:00-0400\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	legacy()
	if m, ok := tool.readMarker(); !ok || !m.Legacy || m.Time != "2026-10-20T01:00:00-0400" || tool.marker() != m.Time {
		t.Fatalf("legacy marker %+v %v", m, ok)
	}
	out, _, _ := cli(t, tool, "guard", "status")
	if !strings.Contains(out, "by the guard since 2026-10-20T01:00:00-0400: an older marker that lists no torrents") {
		t.Errorf("status:\n%s", out)
	}
	// above resumeAbove: clears it and starts nothing
	out, _, err := cli(t, tool, "guard", "check")
	if err != nil || !strings.Contains(out, "action=resumed downloads=0 forced=0\nnote: the marker is from an older o-qbt and lists no torrents") || len(f.posted()) != 0 || tool.marker() != "" {
		t.Fatalf("legacy resume: %v %q posted %v", err, out, f.posted())
	}
	legacy()
	if out, _, _ = cli(t, tool, "guard", "resume"); !strings.Contains(out, "action=resumed downloads=0 forced=0\nnote:") || len(f.posted()) != 0 {
		t.Fatalf("legacy guard resume: %q %v", out, f.posted())
	}
	// below the floor: the downloads are stopped and the marker becomes a list, keeping its time
	legacy()
	free = 4 * gib
	if out, _, _ = cli(t, tool, "guard", "check"); !strings.HasSuffix(out, "action=paused downloads=5 left-alone=8\n") {
		t.Fatalf("legacy pause: %q", out)
	}
	if m, _ := tool.readMarker(); m.Legacy || m.Time != "2026-10-20T01:00:00-0400" || len(m.Torrents) != 5 {
		t.Fatalf("marker %+v", m)
	}
	// guard resume without a marker starts nothing
	free = 30 * gib
	cli(t, tool, "guard", "resume")
	before := len(f.posted())
	if out, _, _ = cli(t, tool, "guard", "resume"); !strings.Contains(out, "downloads=0 forced=0\nnote: no marker") || len(f.posted()) != before {
		t.Fatalf("resume without a marker: %q", out)
	}
}
