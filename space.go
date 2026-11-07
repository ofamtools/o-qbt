package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Space is guard space's answer (o qbt space): free space on the guard's
// volume and every number the guard weighs it against.
type Space struct {
	Mode        string `json:"mode"`
	Target      string `json:"target"`
	Free        int64  `json:"free"`
	DFError     string `json:"df_error,omitempty"`
	Baseline    int64  `json:"baseline"` // 0: none stored
	Used        int64  `json:"used"`     // baseline minus free
	Headroom    int64  `json:"headroom"` // free minus min free
	MinFree     int64  `json:"min_free"`
	ResumeAbove int64  `json:"resume_above"`
	Limit       int64  `json:"limit"`
	QBTFree     int64  `json:"qbt_free"`
	QBTError    string `json:"qbt_error,omitempty"`
	Paused      string `json:"paused"`
	raw         bool   // Text prints bytes, not sizes
}

// spaceField is one number guard space FIELD prints alone.
type spaceField struct {
	Name, About string
	QBT         bool // asks qBittorrent (one login)
	get         func(Space) (int64, error)
}

// spaceFields are guard space's FIELD values, in its table's order.
var spaceFields = []spaceField{
	{"free", "free bytes on the guard's volume (df over ssh): the current free space", false, func(s Space) (int64, error) { return s.Free, errIf(s.DFError) }},
	{"base", "the stored baseline (guard rebase)", false, func(s Space) (int64, error) {
		if s.Baseline == 0 {
			return 0, errors.New("no baseline stored (o qbt guard rebase)")
		}
		return s.Baseline, nil
	}},
	{"used", "baseline minus free: what allowance mode weighs against the limit", false, func(s Space) (int64, error) {
		if s.Baseline == 0 {
			return 0, errors.New("no baseline stored (o qbt guard rebase)")
		}
		return s.Used, errIf(s.DFError)
	}},
	{"headroom", "free minus min free: room before floor mode stops downloads", false, func(s Space) (int64, error) { return s.Headroom, errIf(s.DFError) }},
	{"min-free", "floor: stop downloads below this", false, func(s Space) (int64, error) { return s.MinFree, nil }},
	{"resume-above", "floor: start them again above this", false, func(s Space) (int64, error) { return s.ResumeAbove, nil }},
	{"limit", "allowance: how far free may drop below the baseline", false, func(s Space) (int64, error) { return s.Limit, nil }},
	{"qbt-free", "the free space qBittorrent reports (its save path's disk)", true, func(s Space) (int64, error) { return s.QBTFree, errIf(s.QBTError) }},
}

// SpaceFieldNames are spaceFields' names (guard space's FIELD enum).
func SpaceFieldNames() []string {
	out := make([]string, len(spaceFields))
	for i, f := range spaceFields {
		out[i] = f.Name
	}
	return out
}

func errIf(msg string) error {
	if msg == "" {
		return nil
	}
	return errors.New(msg)
}

func (s Space) size(n int64) string {
	if s.raw {
		return strconv.FormatInt(n, 10)
	}
	return bytesH(n)
}

func (s Space) Text() string {
	rows := [][2]string{{"mode", s.Mode}, {"target", s.Target}}
	for _, f := range spaceFields {
		v, err := f.get(s)
		text := s.size(v)
		if err != nil {
			text = "unknown (" + err.Error() + ")"
		}
		rows = append(rows, [2]string{f.Name, text})
	}
	return table(append(rows, [2]string{"paused", s.Paused}))
}

// SpaceValue is one field of guard space alone: its bytes, and a person's
// rendering (a size, or the bytes with raw).
type SpaceValue struct {
	Field string `json:"field"`
	Bytes int64  `json:"bytes"`
	raw   bool
}

func (v SpaceValue) Text() string {
	if v.raw {
		return strconv.FormatInt(v.Bytes, 10) + "\n"
	}
	return bytesH(v.Bytes) + "\n"
}

// GuardSpace is the guard's numbers: df of the volume, the baseline, the
// thresholds, and, when withQBT, qBittorrent's own free space (api is
// asked only then). A df or qBittorrent failure is reported in the answer.
func (t *Tool) GuardSpace(ctx context.Context, gs GuardSettings, withQBT bool, api func() (*Client, error)) (Space, error) {
	c, _, _, err := t.config()
	if err != nil {
		return Space{}, err
	}
	s := Space{Mode: gs.Mode, Target: c.SSH + ":" + c.Volume, MinFree: gs.MinFree, ResumeAbove: gs.ResumeAbove, Limit: gs.Limit, Paused: "no"}
	if m, ok := t.readMarker(); ok {
		s.Paused = fmt.Sprintf("by the guard since %s (%d downloads)", m.Time, len(m.Torrents))
	}
	if s.Baseline, _, err = t.readBaseline(); err != nil {
		return s, err
	}
	if s.Free, err = t.df(ctx, c); err != nil {
		s.DFError = err.Error()
	}
	s.Used, s.Headroom = s.Baseline-s.Free, s.Free-s.MinFree
	if !withQBT {
		s.QBTError = "not asked"
		return s, nil
	}
	cl, err := api()
	if err == nil {
		var st ServerState
		st, err = MainState(ctx, cl)
		s.QBTFree = st.Free
	}
	if err != nil {
		s.QBTError = err.Error()
	}
	return s, nil
}

// spaceAnswer is guard space's answer for field ("" for all of it).
func (t *Tool) spaceAnswer(ctx context.Context, gs GuardSettings, field string, raw bool, api func() (*Client, error)) (any, error) {
	if field == "" {
		s, err := t.GuardSpace(ctx, gs, true, api)
		s.raw = raw
		return s, err
	}
	i := slices.IndexFunc(spaceFields, func(f spaceField) bool { return f.Name == strings.ToLower(field) })
	if i < 0 {
		return nil, fmt.Errorf("space %q: one of %s", field, strings.Join(SpaceFieldNames(), ", "))
	}
	s, err := t.GuardSpace(ctx, gs, spaceFields[i].QBT, api)
	if err != nil {
		return nil, err
	}
	n, err := spaceFields[i].get(s)
	return SpaceValue{Field: spaceFields[i].Name, Bytes: n, raw: raw}, err
}
