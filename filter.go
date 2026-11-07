package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
)

// Filter words: o qbt avz nyaa completed inactive. Each word belongs to one
// category; words of one category OR, categories AND, so that command is
// (avistaz OR nyaa) AND completed AND inactive. A torrent has values in
// every category (its tracker names, public or private, completed or
// incomplete, active or inactive, its literal state) and a word selects
// some values of its category: a torrent passes a category when it has one
// of the values its words select. not-WORD drops the torrents WORD
// selects. Everything is data: the categories, the words and the tracker
// table; nothing branches on a word.

// TrackerEntry is one row of the tracker table: a name (a filter word), its
// aliases, the hosts it announces to (a torrent's tracker host is the host
// or a subdomain of it) and whether it is a private tracker.
type TrackerEntry struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Hosts   []string `json:"hosts"`
	Private bool     `json:"private"`
}

// DefaultTrackers is the built-in tracker table; config.json's trackers
// list replaces an entry of the same name and adds new ones.
var DefaultTrackers = []TrackerEntry{
	{Name: "avistaz", Aliases: []string{"avz", "av"}, Hosts: []string{"avistaz.to"}, Private: true},
	{Name: "privatehd", Aliases: []string{"phd"}, Hosts: []string{"privatehd.to"}, Private: true},
	{Name: "cinemaz", Hosts: []string{"cinemaz.to"}, Private: true},
	{Name: "exoticaz", Hosts: []string{"exoticaz.to"}, Private: true},
	{Name: "animez", Hosts: []string{"animez.to"}, Private: true},
	{Name: "torrentleech", Aliases: []string{"tl"}, Hosts: []string{"torrentleech.org", "tleechreload.org"}, Private: true},
	{Name: "nyaa", Hosts: []string{"nyaa.si", "nyaa.tracker.wf"}},
	{Name: "myanonamouse", Aliases: []string{"mam"}, Hosts: []string{"myanonamouse.net"}, Private: true},
	{Name: "jptv", Hosts: []string{"jptv.club"}, Private: true},
}

// trackerTable is the defaults under config's trackers: an entry whose
// name (any case) is a default's replaces it, others are appended.
func trackerTable(c Config) []TrackerEntry {
	out := slices.Clone(DefaultTrackers)
	for _, e := range c.Trackers {
		e.Name = strings.ToLower(strings.TrimSpace(e.Name))
		if e.Name == "" {
			continue
		}
		if i := slices.IndexFunc(out, func(d TrackerEntry) bool { return d.Name == e.Name }); i >= 0 {
			out[i] = e
			continue
		}
		out = append(out, e)
	}
	return out
}

// owns reports whether host (a hostname, no scheme or port) is one of e's
// hosts or a subdomain of one.
func (e TrackerEntry) owns(host string) bool {
	return slices.ContainsFunc(e.Hosts, func(h string) bool {
		h = strings.ToLower(h)
		return host == h || strings.HasSuffix(host, "."+h)
	})
}

// hostName is u's hostname, lower case, no scheme, user info, port, path
// or query; "" for a URL without one (and the DHT/PeX/LSD entries).
func hostName(u string) string {
	if pseudoTracker(u) {
		return ""
	}
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return ""
	}
	return strings.ToLower(p.Hostname())
}

// UnmarshalJSON reads a torrents/info element, keeping of its trackers
// (the current tracker and the magnet URI's tr= list) only the hostnames:
// passkeys live in the URLs' paths and queries and never leave here.
func (t *Torrent) UnmarshalJSON(b []byte) error {
	type plain Torrent
	var x struct {
		plain
		Tracker string `json:"tracker"`
		Magnet  string `json:"magnet_uri"`
	}
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	*t = Torrent(x.plain)
	urls := []string{x.Tracker}
	if _, query, ok := strings.Cut(x.Magnet, "?"); ok {
		if v, err := url.ParseQuery(query); err == nil {
			urls = append(urls, v["tr"]...)
		}
	}
	t.Hosts = nil
	for _, u := range urls {
		if h := hostName(u); h != "" && !slices.Contains(t.Hosts, h) {
			t.Hosts = append(t.Hosts, h)
		}
	}
	slices.Sort(t.Hosts)
	return nil
}

// torrentFacts is a torrent as the filter sees it: the table names its
// hosts belong to and whether it is private.
type torrentFacts struct {
	T        Torrent
	Trackers []string // table names; the hostnames when none matches
	Private  bool
}

// factsOf is t's facts against table: private is the torrent's own flag
// (qBittorrent 5), else whether a private table entry owns one of its hosts.
func factsOf(t Torrent, table []TrackerEntry) torrentFacts {
	f := torrentFacts{T: t}
	tablePrivate := false
	for _, e := range table {
		if slices.ContainsFunc(t.Hosts, e.owns) {
			f.Trackers = append(f.Trackers, e.Name)
			tablePrivate = tablePrivate || e.Private
		}
	}
	if len(f.Trackers) == 0 {
		f.Trackers = slices.Clone(t.Hosts)
	}
	f.Private = tablePrivate
	if t.Private != nil {
		f.Private = *t.Private
	}
	return f
}

// ActiveStates are the states qBittorrent gives a torrent only while data
// moves (downloading is an unstalled download, uploading an unstalled
// seed, metaDL fetching metadata, moving its files). A torrent is active
// when it is in one of them or its download or upload speed is above
// zero; every other torrent is inactive. Forced is not activity: a forced
// seed nobody downloads from is inactive.
var ActiveStates = []string{"downloading", "uploading", "metaDL", "forcedMetaDL", "moving"}

// active reports whether t is moving data now (ActiveStates).
func active(t Torrent) bool {
	return t.DLSpeed > 0 || t.UPSpeed > 0 || slices.Contains(ActiveStates, t.State)
}

// filterCategory is one category: its name, what it is about, and a
// torrent's values in it (lower case).
type filterCategory struct {
	Name, About string
	values      func(torrentFacts) []string
	// texts are what a --PATTERN of the category is matched against
	// (values when nil): a tracker pattern also sees the hosts.
	texts func(torrentFacts) []string
}

// either is a when cond, else b.
func either(cond bool, a, b string) []string {
	if cond {
		return []string{a}
	}
	return []string{b}
}

// The categories the --tracker and --name patterns fall in.
const (
	TrackerCategory = "tracker"
	NameCategory    = "name"
)

// filterCategories are the categories, in the order o qbt words lists them.
var filterCategories = []filterCategory{
	{TrackerCategory, "the tracker table's names (o qbt tracker table); a word is a name, an alias, or a unique prefix or substring of a name or host", func(f torrentFacts) []string { return f.Trackers }, func(f torrentFacts) []string {
		return slices.Concat(f.Trackers, f.T.Hosts)
	}},
	{"privacy", "the torrent's private flag (qBittorrent 5), else the tracker table's", func(f torrentFacts) []string { return either(f.Private, "private", "public") }, nil},
	{"completion", "progress: completed is 100%", func(f torrentFacts) []string { return either(f.T.Progress >= 1, "completed", "incomplete") }, nil},
	{"activity", "moving data now: a speed above zero or one of " + strings.Join(ActiveStates, ", "), func(f torrentFacts) []string { return either(active(f.T), "active", "inactive") }, nil},
	{"state", "qBittorrent's state: a group below, or a literal state (any case, a unique prefix)", func(f torrentFacts) []string { return []string{f.T.State} }, nil},
	{NameCategory, "--name PATTERN only", func(f torrentFacts) []string { return []string{f.T.Name} }, nil},
}

// FilterWord is one word: its aliases, its category and the values of that
// category it selects.
type FilterWord struct {
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases,omitempty"`
	Category string   `json:"category"`
	Selects  []string `json:"selects"`
	About    string   `json:"about,omitempty"`
	// Hay is what a word that names no word may be a substring of to
	// pick this one: a tracker's name, aliases and hosts.
	Hay []string `json:"-"`
}

// names is w's name and aliases, lower case.
func (w FilterWord) names() []string {
	out := []string{strings.ToLower(w.Name)}
	for _, a := range w.Aliases {
		out = append(out, strings.ToLower(a))
	}
	return out
}

// The state groups' states.
var (
	downloadStates  = []string{"downloading", "stalledDL", "queuedDL", "metaDL", "checkingDL", "allocating"}
	fDownloadStates = []string{"forcedDL", "forcedMetaDL"}
	seedingStates   = []string{"uploading", "stalledUP", "queuedUP"}
	fSeedingStates  = []string{"forcedUP"}
	startedStates   = slices.Concat(downloadStates, seedingStates, []string{"checkingUP", "moving"})
	forcedStates    = slices.Concat(fSeedingStates, fDownloadStates)
	stoppedStates   = []string{"stoppedUP", "stoppedDL", "pausedUP", "pausedDL"}
)

// LiteralStates are qBittorrent's states (Web API v2, versions 4 and 5).
var LiteralStates = []string{"error", "missingFiles", "uploading", "pausedUP", "stoppedUP", "queuedUP", "stalledUP", "checkingUP", "forcedUP",
	"allocating", "downloading", "metaDL", "forcedMetaDL", "pausedDL", "stoppedDL", "queuedDL", "stalledDL", "checkingDL", "forcedDL", "checkingResumeData", "moving", "unknown"}

// fixedWords are every word but the trackers': earlier words win an
// exact name (the downloading group over the literal state).
var fixedWords = []FilterWord{
	{Name: "public", Category: "privacy", Selects: []string{"public"}, About: "not on a private tracker"},
	{Name: "private", Category: "privacy", Selects: []string{"private"}, About: "on a private tracker"},
	{Name: "completed", Aliases: []string{"done"}, Category: "completion", Selects: []string{"completed"}, About: "progress 100%"},
	{Name: "incomplete", Category: "completion", Selects: []string{"incomplete"}, About: "progress below 100%"},
	{Name: "active", Category: "activity", Selects: []string{"active"}, About: "moving data now"},
	{Name: "inactive", Aliases: []string{"idle"}, Category: "activity", Selects: []string{"inactive"}, About: "not moving data now"},
	{Name: "download", Aliases: []string{"downloading"}, Category: "state", Selects: downloadStates, About: "downloading or waiting to, not forced"},
	{Name: "fdownload", Aliases: []string{"force-download", "forced-download"}, Category: "state", Selects: fDownloadStates, About: "force-started downloads"},
	{Name: "seeding", Aliases: []string{"seed"}, Category: "state", Selects: seedingStates, About: "seeding, not forced (fseeding is the forced ones)"},
	{Name: "fseeding", Aliases: []string{"force-seeding", "forced-seeding"}, Category: "state", Selects: fSeedingStates, About: "force-started seeds"},
	{Name: "started", Category: "state", Selects: startedStates, About: "started (not stopped), not forced"},
	{Name: "fstarted", Aliases: []string{"force-started", "forced"}, Category: "state", Selects: forcedStates, About: "force-started, downloading or seeding"},
	{Name: "stopped", Aliases: []string{"paused"}, Category: "state", Selects: stoppedStates, About: "stopped (v4: paused)"},
	{Name: "queued", Category: "state", Selects: []string{"queuedUP", "queuedDL"}, About: "queued"},
	{Name: "stalled", Category: "state", Selects: []string{"stalledUP", "stalledDL"}, About: "started, no data moving"},
	{Name: "checking", Category: "state", Selects: []string{"checkingUP", "checkingDL", "checkingResumeData"}, About: "being checked"},
	{Name: "errored", Category: "state", Selects: []string{"error", "missingFiles"}, About: "in error or missing files"},
}

// Vocabulary is every filter word against table: the fixed words, the
// literal states no fixed word names, then the trackers.
func Vocabulary(table []TrackerEntry) []FilterWord {
	out := slices.Clone(fixedWords)
	taken := func(name string) bool {
		return slices.ContainsFunc(out, func(w FilterWord) bool { return slices.Contains(w.names(), strings.ToLower(name)) })
	}
	for _, s := range LiteralStates {
		if !taken(s) {
			out = append(out, FilterWord{Name: s, Category: "state", Selects: []string{s}, About: "the literal state"})
		}
	}
	for _, e := range table {
		out = append(out, FilterWord{Name: e.Name, Aliases: e.Aliases, Category: TrackerCategory, Selects: []string{e.Name}, About: strings.Join(e.Hosts, ", "), Hay: append(e.names(), e.Hosts...)})
	}
	return out
}

// ErrUnknownWord is a word that is no filter word, alias or prefix.
var ErrUnknownWord = errors.New("no filter word or command")

// byCategory is ws ordered by category (filterCategories' order), keeping
// each category's own order: how o qbt words lists them.
func byCategory(ws []FilterWord) []FilterWord {
	out := slices.Clone(ws)
	slices.SortStableFunc(out, func(a, b FilterWord) int {
		at := func(name string) int {
			return slices.IndexFunc(filterCategories, func(c filterCategory) bool { return c.Name == name })
		}
		return cmp.Compare(at(a.Category), at(b.Category))
	})
	return out
}

// NegatePrefix before a word drops the torrents the word selects.
const NegatePrefix = "not-"

// resolveWord finds word in vocab: an exact name or alias (any case;
// the first word that has it), else the one word with a name or alias it
// is a prefix of, else the one word whose Hay (a tracker's names and
// hosts) holds it. Several is an error naming them.
func resolveWord(word string, vocab []FilterWord) (FilterWord, error) {
	w := strings.ToLower(word)
	if i := slices.IndexFunc(vocab, func(x FilterWord) bool { return slices.Contains(x.names(), w) }); i >= 0 {
		return vocab[i], nil
	}
	prefixed := keepWords(vocab, func(x FilterWord) bool {
		return slices.ContainsFunc(x.names(), func(n string) bool { return strings.HasPrefix(n, w) })
	})
	if len(prefixed) == 0 {
		prefixed = keepWords(vocab, func(x FilterWord) bool {
			return slices.ContainsFunc(x.Hay, func(n string) bool { return strings.Contains(strings.ToLower(n), w) })
		})
	}
	switch len(prefixed) {
	case 0:
		return FilterWord{}, fmt.Errorf("%w: %q (o qbt words lists the words, o qbt help the commands; --tracker %s matches any tracker host)", ErrUnknownWord, word, word)
	case 1:
		return prefixed[0], nil
	}
	var names []string
	for _, x := range prefixed {
		names = append(names, x.Name+" ("+x.Category+")")
	}
	return FilterWord{}, fmt.Errorf("%q is ambiguous: %s; type more of it", word, strings.Join(names, ", "))
}

// names is e's name and aliases.
func (e TrackerEntry) names() []string { return append([]string{e.Name}, e.Aliases...) }

func keepWords(vocab []FilterWord, keep func(FilterWord) bool) []FilterWord {
	var out []FilterWord
	for _, x := range vocab {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

// MatchModes are --match's modes: ochazuke's (substring, fuzzy, regex),
// substr for substring, and glob (path.Match, any case).
var MatchModes = append(ochazuke.MatchModes(), "substr", "glob")

// patternMatcher compiles pattern in mode (substring when ""): whether a
// text matches it.
func patternMatcher(mode, pattern string) (func(string) bool, error) {
	switch mode {
	case "glob":
		p := strings.ToLower(pattern)
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("glob %q: %w", pattern, err)
		}
		return func(s string) bool { ok, _ := path.Match(p, strings.ToLower(s)); return ok }, nil
	case "substr", "":
		mode = ochazuke.MatchSubstring
	}
	if !slices.Contains(MatchModes, mode) {
		return nil, fmt.Errorf("--match %q: one of %s", mode, strings.Join(MatchModes, ", "))
	}
	m := ochazuke.NewMatcher(mode, ochazuke.SearchCaseSmart, pattern)
	if err := m.Err(); err != nil {
		return nil, err
	}
	return m.Match, nil
}

// selector is one filter term: its category, whether it drops what it
// matches, and the test.
type selector struct {
	category string
	negate   bool
	match    func(torrentFacts) bool
}

// categoryOf is the category called name (a category of no values when
// none is).
func categoryOf(name string) filterCategory {
	if i := slices.IndexFunc(filterCategories, func(c filterCategory) bool { return c.Name == name }); i >= 0 {
		return filterCategories[i]
	}
	return filterCategory{Name: name, values: func(torrentFacts) []string { return nil }}
}

// categoryValues is the values of f in category name.
func categoryValues(name string, f torrentFacts) []string { return categoryOf(name).values(f) }

// wordSelector selects the torrents with one of w's values.
func wordSelector(w FilterWord, negate bool) selector {
	return selector{category: w.Category, negate: negate, match: func(f torrentFacts) bool {
		return slices.ContainsFunc(categoryValues(w.Category, f), func(v string) bool {
			return slices.ContainsFunc(w.Selects, func(s string) bool { return strings.EqualFold(s, v) })
		})
	}}
}

// patternSelector selects the torrents with a text of category (its
// values, or its texts: a tracker's hosts too) that pattern matches in mode.
func patternSelector(category, mode, pattern string) (selector, error) {
	m, err := patternMatcher(mode, pattern)
	if err != nil {
		return selector{}, err
	}
	c := categoryOf(category)
	texts := c.texts
	if texts == nil {
		texts = c.values
	}
	return selector{category: category, match: func(f torrentFacts) bool { return slices.ContainsFunc(texts(f), m) }}, nil
}

// FilterSpec is a filter as typed: the words, --tracker and --name
// patterns, and --match.
type FilterSpec struct {
	Words   []string
	Tracker string
	Name    string
	Match   string
}

// Filter is a compiled FilterSpec.
type Filter struct {
	table     []TrackerEntry
	selectors []selector
}

// CompileFilter resolves spec against table.
func CompileFilter(spec FilterSpec, table []TrackerEntry) (Filter, error) {
	f := Filter{table: table}
	vocab := Vocabulary(table)
	for _, word := range spec.Words {
		negate := strings.HasPrefix(strings.ToLower(word), NegatePrefix) && len(word) > len(NegatePrefix)
		if negate {
			word = word[len(NegatePrefix):]
		}
		w, err := resolveWord(word, vocab)
		if err != nil {
			return f, err
		}
		f.selectors = append(f.selectors, wordSelector(w, negate))
	}
	for _, p := range []struct{ category, pattern string }{{TrackerCategory, spec.Tracker}, {NameCategory, spec.Name}} {
		if p.pattern == "" {
			continue
		}
		s, err := patternSelector(p.category, spec.Match, p.pattern)
		if err != nil {
			return f, err
		}
		f.selectors = append(f.selectors, s)
	}
	return f, nil
}

// Keep reports whether t passes: in every category with words, one of
// them matches; no not- word matches.
func (f Filter) Keep(t Torrent) bool {
	facts := factsOf(t, f.table)
	pass := map[string]bool{}
	for _, s := range f.selectors {
		hit := s.match(facts)
		if s.negate {
			if hit {
				return false
			}
			continue
		}
		pass[s.category] = pass[s.category] || hit
	}
	for _, ok := range pass {
		if !ok {
			return false
		}
	}
	return true
}

// Apply is the torrents of ts that f keeps, in order.
func (f Filter) Apply(ts []Torrent) []Torrent {
	out := []Torrent{}
	for _, t := range ts {
		if f.Keep(t) {
			out = append(out, t)
		}
	}
	return out
}

// TrackerCount is one tracker's torrents: how many, their size, how many
// are completed and active, and whether the table marks it private.
type TrackerCount struct {
	Tracker   string `json:"tracker"`
	Private   bool   `json:"private"`
	Torrents  int    `json:"torrents"`
	Size      int64  `json:"size"`
	Completed int    `json:"completed"`
	Active    int    `json:"active"`
}

// CountByTracker groups ts by tracker table name (a torrent no entry
// owns counts under each of its hosts; one with no host under "(none)"),
// most torrents first.
func CountByTracker(ts []Torrent, table []TrackerEntry) []TrackerCount {
	by := map[string]*TrackerCount{}
	for _, t := range ts {
		f := factsOf(t, table)
		names := f.Trackers
		if len(names) == 0 {
			names = []string{"(none)"}
		}
		for _, name := range names {
			x := by[name]
			if x == nil {
				x = &TrackerCount{Tracker: name}
				if i := slices.IndexFunc(table, func(e TrackerEntry) bool { return e.Name == name }); i >= 0 {
					x.Private = table[i].Private
				}
				by[name] = x
			}
			x.Torrents++
			x.Size += t.Size
			x.Completed += map[bool]int{true: 1}[t.Progress >= 1]
			x.Active += map[bool]int{true: 1}[active(t)]
		}
	}
	out := make([]TrackerCount, 0, len(by))
	for _, x := range by {
		out = append(out, *x)
	}
	slices.SortFunc(out, func(a, b TrackerCount) int {
		return cmp.Or(cmp.Compare(b.Torrents, a.Torrents), cmp.Compare(a.Tracker, b.Tracker))
	})
	return out
}

// tableName is the table name owning u's host, else TrackerHost(u).
func tableName(table []TrackerEntry) func(u string) string {
	return func(u string) string {
		h := hostName(u)
		if i := slices.IndexFunc(table, func(e TrackerEntry) bool { return h != "" && e.owns(h) }); i >= 0 {
			return table[i].Name
		}
		return TrackerHost(u)
	}
}
