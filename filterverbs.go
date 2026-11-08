package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ofam.dev/ochazuke"
)

// The filter words' verbs: torrent find (what o qbt WORDS runs), torrent
// vocabulary (o qbt words), tracker registry and counts, and guard disk
// (o qbt space).

// named is name/value pairs (params' form) with every parameter Named:
// no bare word fills it, so a verb's bare words all go to its rest
// parameter.
func named(kv []any) []any {
	out := make([]any, len(kv))
	for i := 0; i+1 < len(kv); i += 2 {
		p := kv[i+1].(ochazuke.Parameter)
		p.Named = true
		out[i], out[i+1] = kv[i], p
	}
	return out
}

// filterParams are the filter's parameters: the words (bare, terms), then
// --tracker, --name and --match.
func filterParams() []any {
	return append([]any{
		"terms", ochazuke.Parameter{Type: "string", Rest: true, Source: "torrent.vocabulary", Free: true, Description: "filter words (o qbt words): same category OR, categories AND; not-WORD drops what WORD selects"},
	}, named([]any{
		"tracker", ochazuke.Parameter{Type: "string", Source: "tracker.registry", Free: true, Description: "a pattern any tracker name or host must match, in --match mode (quote a glob: '*.to')"},
		"name", ochazuke.Parameter{Type: "string", Description: "a pattern the torrent name must match, in --match mode"},
		"match", ochazuke.Parameter{Type: "string", Enum: MatchModes, Description: "how --tracker and --name match: substring (default; substr), fuzzy, regex or glob"},
	})...)
}

// filterOf compiles c's filter against the configured tracker table.
func (t *Tool) filterOf(c ochazuke.Ctx) (Filter, []TrackerEntry, error) {
	conf, _, _, err := t.config()
	if err != nil {
		return Filter{}, nil, err
	}
	table := trackerTable(conf)
	f, err := CompileFilter(FilterSpec{
		Words:   strings.Fields(ochazuke.StringArg(c.Args, "terms", "")),
		Tracker: ochazuke.StringArg(c.Args, "tracker", ""),
		Name:    ochazuke.StringArg(c.Args, "name", ""),
		Match:   ochazuke.StringArg(c.Args, "match", ""),
	}, table)
	return f, table, err
}

// filtered is the torrents c's query and filter select: the query asks the
// server for everything but --limit, the filter keeps, then --limit cuts.
func (t *Tool) filtered(c ochazuke.Ctx, fetch func(context.Context, TorrentQuery) ([]Torrent, error)) ([]Torrent, []TrackerEntry, error) {
	f, table, err := t.filterOf(c)
	if err != nil {
		return nil, nil, err
	}
	q, err := torrentQuery(c, "")
	if err != nil {
		return nil, nil, err
	}
	limit := q.Limit
	q.Limit = 0
	ts, err := fetch(context.Background(), q)
	if err != nil {
		return nil, nil, err
	}
	ts = f.Apply(ts)
	if limit > 0 && limit < len(ts) {
		ts = ts[:limit]
	}
	return ts, table, nil
}

// defineFilters registers the filter's verbs. fetch is torrents/info;
// setRows makes torrents the torrent list's rows.
func (t *Tool) defineFilters(r *ochazuke.Runtime, api func() (*Client, error), fetch func(context.Context, TorrentQuery) ([]Torrent, error), setRows func([]Torrent) ([]Row, error), loc *time.Location) {
	query := named(append(queryParams(true), outputParams()...))
	r.Define("torrent").
		Verb("find", func(c ochazuke.Ctx) ochazuke.Result {
			ts, _, err := t.filtered(c, fetch)
			if err != nil {
				return ochazuke.Failed(err)
			}
			rows, err := setRows(ts)
			return t.present(c, rows, TorrentTable(ts, loc), err)
		}, "the torrents filter words select (o qbt WORDS runs this): words of one category OR, categories AND, not-WORD drops; --tracker and --name match a pattern in --match mode; takes torrent list's query and output flags, --limit after the filter").Rows("torrent").Fetch().Positional("terms").
		Verb("vocabulary", func(c ochazuke.Ctx) ochazuke.Result {
			conf, _, _, err := t.config()
			if err != nil {
				return ochazuke.Failed(err)
			}
			ws := byCategory(Vocabulary(trackerTable(conf)))
			return t.present(c, ws, WordTable(ws), nil)
		}, "the filter words (o qbt words): category, name, aliases, the values it selects; the categories are tracker, privacy, completion, activity and state").Fetch()
	r.Operations.Set(withParams(r.Operations.At("torrent.find"), params(append(filterParams(), query...)...)))
	r.Operations.Set(withParams(r.Operations.At("torrent.vocabulary"), params(named(outputParams())...)))

	r.Define("tracker").
		Verb("registry", func(c ochazuke.Ctx) ochazuke.Result {
			conf, _, _, err := t.config()
			if err != nil {
				return ochazuke.Failed(err)
			}
			es := trackerTable(conf)
			return t.present(c, es, TrackerTableTable(es), nil)
		}, "the tracker table the filter words use: name, aliases, hosts, private; config.json's trackers list replaces an entry of the same name and adds others").Fetch().
		Verb("counts", func(c ochazuke.Ctx) ochazuke.Result {
			ts, table, err := t.filtered(c, fetch)
			if err != nil {
				return ochazuke.Failed(err)
			}
			cs := CountByTracker(ts, table)
			return t.present(c, cs, TrackerCountTable(cs), nil)
		}, "torrents per tracker table name (a torrent no entry owns counts under its hosts): torrents, size, completed, active; takes filter words and torrent list's query; one request").Fetch().Positional("terms")
	r.Operations.Set(withParams(r.Operations.At("tracker.registry"), params(named(outputParams())...)))
	r.Operations.Set(withParams(r.Operations.At("tracker.counts"), params(append(filterParams(), query...)...)))

	r.Define("guard").Verb("disk", func(c ochazuke.Ctx) ochazuke.Result {
		conf, _, _, err := t.config()
		if err != nil {
			return ochazuke.Failed(err)
		}
		gs, err := guardSettings(conf, c.Args)
		if err != nil {
			return ochazuke.Failed(err)
		}
		raw, _ := c.Args["bytes"].(bool)
		return t.answer(t.spaceAnswer(context.Background(), gs, ochazuke.StringArg(c.Args, "field", ""), raw, api))
	}, fmt.Sprintf("free space and the guard's numbers (o qbt space): %s; FIELD prints one alone (o qbt space free), a size or with --bytes the bytes; df errors are reported", strings.Join(SpaceFieldNames(), ", "))).Fetch().Positional("field")
	r.Operations.Set(withParams(r.Operations.At("guard.disk"), params(append([]any{
		"field", ochazuke.Parameter{Type: "string", Enum: SpaceFieldNames(), Description: "print only this number"},
		"bytes", ochazuke.Parameter{Type: "boolean", Description: "bytes instead of sizes"},
	}, named(guardParamKV())...)...)))
}

// SummaryKeys are tracker summary's --by values: host (the default) or
// name, the tracker table's.
var SummaryKeys = []string{"host", "name"}

// summaryKey is how tracker summary groups for --by by.
func (t *Tool) summaryKey(by string) (func(string) string, error) {
	if by == "" || by == SummaryKeys[0] {
		return TrackerHost, nil
	}
	if by != SummaryKeys[1] {
		return nil, fmt.Errorf("--by %q: host or name", by)
	}
	conf, _, _, err := t.config()
	return tableName(trackerTable(conf)), err
}

// summarizeTrackers is tracker summary's rows for c's --by, unless err.
func (t *Tool) summarizeTrackers(c ochazuke.Ctx, trs [][]Tracker, err error) ([]HostSummary, error) {
	if err != nil {
		return nil, err
	}
	key, err := t.summaryKey(ochazuke.StringArg(c.Args, "by", ""))
	if err != nil {
		return nil, err
	}
	return SummarizeTrackersBy(trs, key), nil
}

// filterWord reports whether word (a first word: a flag, or not-WORD) can
// start a filter: a flag, or a word that resolves or is ambiguous.
func (t *Tool) filterWord(word string) bool {
	if strings.HasPrefix(word, "-") {
		return true
	}
	conf, _, _, _ := t.config()
	table := trackerTable(conf)
	if w, ok := strings.CutPrefix(strings.ToLower(word), NegatePrefix); ok && w != "" {
		word = w
	}
	_, err := resolveWord(word, Vocabulary(table))
	return !errors.Is(err, ErrUnknownWord)
}
