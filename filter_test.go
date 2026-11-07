package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// filterTorrents are torrents across trackers, states and private flags.
// Trackers come from the current tracker and the magnet URI's tr= list,
// with passkeys in their paths.
var filterTorrents = []map[string]any{
	ft("a1", "Avz Seed Forced", "forcedUP", 1, 0, 0, ptr(true), "https://tracker.avistaz.to/SECRETPASS/announce", "https://tracker.avistaz.to/SECRETPASS/announce"),
	ft("a2", "Avz Stopped Half", "stoppedDL", 0.3, 0, 0, ptr(true), "", "https://tracker.avistaz.to:443/SECRETPASS/announce"),
	ft("a3", "Avz Uploading", "uploading", 1, 0, 500, ptr(true), "https://tracker.avistaz.to/SECRETPASS/announce", ""),
	ft("n1", "Nyaa Idle Seed", "stalledUP", 1, 0, 0, ptr(false), "http://nyaa.tracker.wf:7777/announce", "http://nyaa.tracker.wf:7777/announce", "udp://open.stealth.si:80/announce"),
	ft("n2", "Nyaa Downloading", "downloading", 0.5, 100, 0, ptr(false), "https://nyaa.si/announce", ""),
	ft("t1", "TL Stalled", "stalledDL", 0.2, 0, 0, nil, "https://tracker.tleechreload.org/a/SECRETPASS/announce", ""),
	ft("p1", "PHD Forced Download", "forcedDL", 0.1, 10, 0, ptr(true), "https://tracker.privatehd.to/SECRETPASS/announce", ""),
	ft("o1", "Open Missing", "missingFiles", 1, 0, 0, ptr(false), "udp://tracker.opentrackr.org:1337/announce", ""),
	ft("m1", "MAM Queued", "queuedUP", 1, 0, 0, ptr(true), "https://t.myanonamouse.net/tracker.php/SECRETPASS/announce", ""),
}

func ptr(b bool) *bool { return &b }

// ft is one torrents/info element; magnet trackers follow the current one.
func ft(hash, name, state string, progress, dl, up float64, private *bool, tracker string, magnet ...string) map[string]any {
	m := map[string]any{"hash": hash, "name": name, "state": state, "progress": progress, "size": 1000.0, "dlspeed": dl, "upspeed": up, "eta": 0.0, "ratio": 1.0,
		"category": "", "tags": "", "added_on": 1.0, "completion_on": -1.0, "save_path": "/d", "tracker": tracker}
	if private != nil {
		m["private"] = *private
	}
	uri := "magnet:?xt=urn:btih:" + hash + "&dn=" + strings.ReplaceAll(name, " ", "+")
	for _, tr := range magnet {
		if tr != "" {
			uri += "&tr=" + strings.NewReplacer(":", "%3A", "/", "%2F").Replace(tr)
		}
	}
	m["magnet_uri"] = uri
	return m
}

// filterFake is the fake serving filterTorrents.
func filterFake(t *testing.T) (*fakeQBT, *Tool) {
	f := newFake(t)
	f.list = fakeList(filterTorrents)
	return f, testTool(t, f.URL, "good", nil)
}

// hashesOf runs o-qbt ARGS --format json and returns the hashes, sorted.
func hashesOf(t *testing.T, tool *Tool, args ...string) []string {
	t.Helper()
	out, errOut, err := cli(t, tool, append(args, "--format", "json")...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, errOut)
	}
	var rows []struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	hs := []string{}
	for _, r := range rows {
		hs = append(hs, r.Hash)
	}
	slices.Sort(hs)
	return hs
}

func TestFilterWords(t *testing.T) {
	_, tool := filterFake(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		// each category alone
		{[]string{"avz"}, "a1 a2 a3"},
		{[]string{"nyaa"}, "n1 n2"},
		{[]string{"private"}, "a1 a2 a3 m1 p1 t1"}, // t1 has no flag: the table marks torrentleech private
		{[]string{"public"}, "n1 n2 o1"},
		{[]string{"completed"}, "a1 a3 m1 n1 o1"},
		{[]string{"incomplete"}, "a2 n2 p1 t1"},
		{[]string{"active"}, "a3 n2 p1"},
		{[]string{"inactive"}, "a1 a2 m1 n1 o1 t1"},
		{[]string{"errored"}, "o1"},
		// the owner's state shorthands: plain and forced are disjoint
		{[]string{"seeding"}, "a3 m1 n1"},
		{[]string{"fseeding"}, "a1"},
		{[]string{"force-seeding"}, "a1"},
		{[]string{"seeding", "fseeding"}, "a1 a3 m1 n1"},
		{[]string{"download"}, "n2 t1"},
		{[]string{"downloading"}, "n2 t1"},
		{[]string{"fdownload"}, "p1"},
		{[]string{"force-download"}, "p1"},
		{[]string{"started"}, "a3 m1 n1 n2 t1"},
		{[]string{"fstarted"}, "a1 p1"},
		{[]string{"force-started"}, "a1 p1"},
		{[]string{"forced"}, "a1 p1"},
		{[]string{"stopped"}, "a2"},
		{[]string{"paused"}, "a2"},
		{[]string{"queued"}, "m1"},
		// literal states: any case, a unique prefix
		{[]string{"stalledUP"}, "n1"},
		{[]string{"STALLEDUP"}, "n1"},
		{[]string{"stalledu"}, "n1"},
		{[]string{"missingf"}, "o1"},
		// OR within a category, AND across
		{[]string{"avz", "nyaa"}, "a1 a2 a3 n1 n2"},
		{[]string{"completed", "inactive"}, "a1 m1 n1 o1"},
		// the owner's examples
		{[]string{"avz", "completed", "inactive"}, "a1"},
		{[]string{"avz", "nyaa", "completed", "inactive"}, "a1 n1"},
		{[]string{"nyaa", "avz", "inactive", "completed"}, "a1 n1"},
		// tracker names: alias, prefix, substring of a name or host
		{[]string{"av"}, "a1 a2 a3"},
		{[]string{"avis"}, "a1 a2 a3"},
		{[]string{"tl"}, "t1"},
		{[]string{"private", "torr"}, "t1"}, // first, torr is oden's prefix of torrent
		{[]string{"leech"}, "t1"},
		{[]string{"tleech"}, "t1"},
		{[]string{"mam"}, "m1"},
		{[]string{"phd"}, "p1"},
		// negation
		{[]string{"avz", "not-completed"}, "a2"},
		{[]string{"not-avz", "not-nyaa"}, "m1 o1 p1 t1"},
		{[]string{"private", "not-fseeding", "not-fdownload"}, "a2 a3 m1 t1"},
		// patterns
		{[]string{"--tracker", "*.to", "--match", "glob"}, "a1 a2 a3 p1"},
		{[]string{"--tracker", "^nyaa", "--match", "regex"}, "n1 n2"},
		{[]string{"--tracker", "trntlch", "--match", "fuzzy"}, "t1"},
		{[]string{"--tracker", "stealth"}, "n1"},
		{[]string{"--tracker", "stealth", "--match", "substr"}, "n1"},
		{[]string{"avz", "--tracker", "stealth"}, "a1 a2 a3 n1"}, // one category: OR
		{[]string{"--name", "seed"}, "a1 n1"},
		{[]string{"--name", "*forced*", "--match", "glob"}, "a1 p1"},
		{[]string{"avz", "--name", "seed"}, "a1"},
		// limit after the filter
		{[]string{"avz", "--limit", "1"}, "a1"}, // by name: Avz Seed Forced first
	} {
		if got := strings.Join(hashesOf(t, tool, tc.args...), " "); got != tc.want {
			t.Errorf("o qbt %s = %q, want %q", strings.Join(tc.args, " "), got, tc.want)
		}
	}
}

func TestFilterWordErrors(t *testing.T) {
	_, tool := filterFake(t)
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"pri"}, []string{`"pri" is ambiguous`, "private (privacy)", "privatehd (tracker)"}},
		{[]string{"avz", "st"}, []string{`"st" is ambiguous`, "started (state)", "stalledUP (state)"}},
		{[]string{"avz", "bogus"}, []string{`"bogus"`, "o qbt words"}},
		{[]string{"--tracker", "[", "--match", "glob"}, []string{"glob"}},
		{[]string{"--tracker", "(", "--match", "regex"}, []string{"regex"}},
	} {
		_, _, err := cli(t, tool, tc.args...)
		for _, want := range tc.want {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("o qbt %s: err %v, want %q", strings.Join(tc.args, " "), err, want)
			}
		}
	}
}

// TestFilterKeepsCommands: a first word that is a command runs it as
// before; the old torrent presets keep their server filter.
func TestFilterKeepsCommands(t *testing.T) {
	f, tool := filterFake(t)
	for _, verb := range []string{"active", "seeding", "downloading", "paused", "errored", "stalled"} {
		f.queries = nil
		if _, _, err := cli(t, tool, "torrent", verb, "--format", "json"); err != nil {
			t.Fatal(err)
		}
		if len(f.queries) != 1 || f.queries[0] != "filter="+verb {
			t.Errorf("torrent %s asked %v", verb, f.queries)
		}
	}
	f.queries = nil
	hashesOf(t, tool, "seeding")
	if len(f.queries) != 1 || f.queries[0] != "" {
		t.Errorf("o qbt seeding asked %v, want every torrent", f.queries)
	}
	// every filter word and alias is free at the first position: no
	// command, builtin or shortcut takes it (a word or prefix oden resolves
	// would run the command instead).
	for _, w := range Vocabulary(DefaultTrackers) {
		for _, name := range append([]string{w.Name}, w.Aliases...) {
			out, errOut, err := cli(t, tool, "which", name)
			if err == nil || out != "" || strings.Contains(errOut, "ambiguous") || shortcuts[name] != nil {
				t.Errorf("filter word %s is taken by a command: %q %q", name, out, errOut)
			}
		}
	}
}

func TestFilterNeverPrintsPasskeys(t *testing.T) {
	_, tool := filterFake(t)
	for _, format := range Formats {
		out, _, err := cli(t, tool, "private", "--format", format, "--columns", "name,trackers,private")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "SECRET") || !strings.Contains(out, "tracker.avistaz.to") {
			t.Errorf("%s: %s", format, out)
		}
	}
}

func TestWordsListing(t *testing.T) {
	_, tool := filterFake(t)
	out, _, err := cli(t, tool, "words", "--format", "tsv")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"tracker\tavistaz\tavz,av\tavistaz\tavistaz.to",
		"tracker\ttorrentleech\ttl\ttorrentleech\ttorrentleech.org, tleechreload.org",
		"privacy\tprivate\t",
		"activity\tinactive\tidle\tinactive",
		"state\tseeding\tseed\tuploading,stalledUP,queuedUP\t",
		"state\tfseeding\tforce-seeding,forced-seeding\tforcedUP\t",
		"state\tfstarted\tforce-started,forced\tforcedUP,forcedDL,forcedMetaDL\t",
		"state\tdownload\tdownloading\tdownloading,stalledDL,queuedDL,metaDL,checkingDL,allocating\t",
		"state\tstalledUP\t\tstalledUP\t",
	} {
		if !strings.Contains(out, want+"") {
			t.Errorf("words lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "state\tdownloading\t") {
		t.Errorf("the literal downloading shadows the download group:\n%s", out)
	}
}

// TestTrackerConfig: config.json's trackers replace a default of the same
// name and add new ones; words, the registry and summary --by name follow.
func TestTrackerConfig(t *testing.T) {
	f, tool := filterFake(t)
	cfg := filepath.Join(tool.getenv("XDG_CONFIG_HOME"), "o-qbt", "config.json")
	os.MkdirAll(filepath.Dir(cfg), 0o700)
	os.WriteFile(cfg, []byte(`{"trackers":[{"name":"nyaa","aliases":["ny"],"hosts":["nyaa.si"]},{"name":"opentrackr","aliases":["otr"],"hosts":["opentrackr.org"]},{"name":"Example","hosts":["example.org"],"private":true}]}`), 0o600)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"ny"}, "n2"}, // nyaa now owns nyaa.si only
		{[]string{"otr"}, "o1"},
		{[]string{"opentr"}, "o1"},
	} {
		if got := strings.Join(hashesOf(t, tool, tc.args...), " "); got != tc.want {
			t.Errorf("o qbt %s = %q, want %q", strings.Join(tc.args, " "), got, tc.want)
		}
	}
	out, _, err := cli(t, tool, "tracker", "registry", "--format", "tsv")
	if err != nil || !strings.Contains(out, "nyaa\tny\tnyaa.si\tfalse\n") || !strings.Contains(out, "example\t\texample.org\ttrue\n") {
		t.Errorf("registry: %v\n%s", err, out)
	}
	f.list = fakeList(fakeTorrentList)
	out, _, err = cli(t, tool, "tracker", "summary", "--by", "name", "--format", "tsv", "--columns", "host,torrents,working")
	if err != nil || !strings.Contains(out, "example\t2\t1\n") || !strings.Contains(out, "http://private.example.com\t1\t0\n") || strings.Contains(out, "SECRET") {
		t.Errorf("summary --by name: %v\n%s", err, out)
	}
}

func TestTrackerCounts(t *testing.T) {
	_, tool := filterFake(t)
	out, _, err := cli(t, tool, "tracker", "counts", "--format", "tsv")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tracker\tprivate\ttorrents\tsize\tcompleted\tactive\n", "avistaz\ttrue\t3\t3000\t2\t1\n", "nyaa\tfalse\t2\t2000\t1\t1\n",
		"torrentleech\ttrue\t1\t1000\t0\t0\n", "tracker.opentrackr.org\tfalse\t1\t1000\t1\t0\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("counts lacks %q:\n%s", want, out)
		}
	}
	out, _, err = cli(t, tool, "tracker", "counts", "completed", "inactive", "--format", "tsv", "--columns", "tracker,torrents")
	if err != nil || out != "tracker\ttorrents\navistaz\t1\nmyanonamouse\t1\nnyaa\t1\ntracker.opentrackr.org\t1\n" {
		t.Errorf("counts completed inactive: %v\n%s", err, out)
	}
}

func TestSpace(t *testing.T) {
	f := newFake(t)
	free := int64(577 * gib)
	tool := testTool(t, f.URL, "good", &free)
	if err := tool.writeBaseline(600 * gib); err != nil {
		t.Fatal(err)
	}
	out, _, err := cli(t, tool, "space")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mode          floor", "free          577.0 GiB", "base          600.0 GiB", "used          23.0 GiB", "headroom      572.0 GiB", "min-free      5.0 GiB", "resume-above  20.0 GiB", "limit         10.0 GiB", "qbt-free      584.9 GiB", "paused        no"} {
		if !strings.Contains(out, want) {
			t.Errorf("space lacks %q:\n%s", want, out)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"space", "free"}, "577.0 GiB\n"},
		{[]string{"space", "free", "--bytes"}, "619549032448\n"},
		{[]string{"space", "base"}, "600.0 GiB\n"},
		{[]string{"space", "base", "--bytes"}, "644245094400\n"},
		{[]string{"space", "used", "--bytes"}, "24696061952\n"},
		{[]string{"space", "min-free", "--min-free", "8GiB"}, "8.0 GiB\n"},
		{[]string{"space", "qbt-free", "--bytes"}, "628000000000\n"},
		{[]string{"guard", "disk", "free"}, "577.0 GiB\n"},
	} {
		got, _, err := cli(t, tool, tc.args...)
		if err != nil || got != tc.want {
			t.Errorf("o qbt %s = %q %v, want %q", strings.Join(tc.args, " "), got, err, tc.want)
		}
	}
	logins := f.logins
	if _, _, err := cli(t, tool, "space", "free"); err != nil || f.logins != logins {
		t.Errorf("space free logged in: %v (%d -> %d)", err, logins, f.logins)
	}
	out, _, err = cli(t, tool, "space", "free", "--json", "{}")
	if err != nil || !strings.Contains(out, `"bytes": 619549032448`) {
		t.Errorf("--json: %v\n%s", err, out)
	}
	os.Remove(filepath.Join(tool.DataDir(), "baseline"))
	if _, _, err := cli(t, tool, "space", "base"); err == nil || !strings.Contains(err.Error(), "no baseline") {
		t.Errorf("space base without one: %v", err)
	}
}

func TestFilterCompletion(t *testing.T) {
	_, tool := filterFake(t)
	complete := func(words ...string) []string {
		out, _, err := cli(t, tool, append([]string{"complete", "--"}, words...)...)
		if err != nil {
			t.Fatalf("complete %v: %v", words, err)
		}
		return strings.Fields(out)
	}
	for _, tc := range []struct {
		words      []string
		want, lack []string
	}{
		{[]string{"av"}, []string{"av", "avistaz", "avz"}, nil},
		{[]string{"to"}, []string{"torrent", "torrentleech", "torrents"}, nil},
		{[]string{"sp"}, []string{"space"}, nil},
		{[]string{"avz", "f"}, []string{"fdownload", "force-started", "forced", "fseeding", "fstarted"}, []string{"torrent"}},
		{[]string{"avz", "not-ny"}, []string{"not-nyaa"}, nil},
		{[]string{"avz", "--mat"}, []string{"--match"}, nil},
		{[]string{"avz", "--match", "g"}, []string{"glob"}, nil},
		{[]string{"avz", "--tracker", "tor"}, []string{"torrentleech"}, nil},
		{[]string{"space", "f"}, []string{"free"}, nil},
	} {
		got := complete(tc.words...)
		for _, w := range tc.want {
			if !slices.Contains(got, w) {
				t.Errorf("complete %v = %v, lacks %s", tc.words, got, w)
			}
		}
		for _, w := range tc.lack {
			if slices.Contains(got, w) {
				t.Errorf("complete %v = %v, has %s", tc.words, got, w)
			}
		}
	}
}
