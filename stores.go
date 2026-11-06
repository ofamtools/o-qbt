package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ofam.dev/ochazuke"
)

// The torrent, category and tracker nouns are thing nouns (DESIGN-001)
// with read-only (facts) stores over qBittorrent's answers: the standard
// list and get read the same torrents, categories and trackers the named
// verbs do. torrent delete stays o-qbt's own verb (it removes from
// qBittorrent, optionally with data, in batches, and asks first); torrent
// pause, resume and recheck are verbs, not props. config is a singleton
// over the settings file: get reads the effective settings, set writes
// cfg_url, cfg_user, cfg_volume and cfg_ssh through config set (pass is never a prop: it
// is read with echo off); config set KEY VALUE stays the named verb.

func qbtProp(name, kind, desc string) ochazuke.PropSpec {
	return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: kind, Description: desc}}
}

func torrentProps() []ochazuke.PropSpec {
	return []ochazuke.PropSpec{
		{Name: "hash", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The torrent's info hash"}},
		{Name: "name", Role: ochazuke.RoleLabel, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The torrent's name"}},
		qbtProp("state", "string", "qBittorrent's state (downloading, stalledUP, pausedDL, error, missingFiles ...)"),
		qbtProp("progress", ochazuke.SettingNumber, "Fraction done, 0 to 1"),
		qbtProp("size", ochazuke.SettingInteger, "Bytes"),
		qbtProp("dlspeed", ochazuke.SettingInteger, "Download speed, bytes/s"),
		qbtProp("upspeed", ochazuke.SettingInteger, "Upload speed, bytes/s"),
		qbtProp("eta", ochazuke.SettingInteger, "Seconds left"),
		qbtProp("ratio", ochazuke.SettingNumber, "Share ratio"),
		qbtProp("category", "string", "The category"),
		qbtProp("tags", "string", "Comma-separated tags"),
		qbtProp("added_on", ochazuke.SettingInteger, "When it was added (Unix seconds)"),
		qbtProp("completion_on", ochazuke.SettingInteger, "When it completed (Unix seconds)"),
		qbtProp("save_path", "string", "Where its data is saved"),
	}
}

func categoryProps() []ochazuke.PropSpec {
	return []ochazuke.PropSpec{
		{Name: "name", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The category's name (category/NAME names it too)"}},
		qbtProp("savePath", "string", "The category's save path"),
	}
}

func trackerProps() []ochazuke.PropSpec {
	return []ochazuke.PropSpec{
		{Name: "host", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The tracker's scheme and host (#2 ... for another entry of the same host)"}},
		qbtProp("url", "string", "The scheme and host (the whole announce URL, passkey included, only with full-url)"),
		qbtProp("status", "string", "The tracker's status"),
		qbtProp("tier", ochazuke.SettingInteger, "The tier"),
		qbtProp("peers", ochazuke.SettingInteger, "Peers"),
		qbtProp("seeds", ochazuke.SettingInteger, "Seeds"),
		qbtProp("leeches", ochazuke.SettingInteger, "Leeches"),
		qbtProp("msg", "string", "The tracker's message"),
	}
}

func configProps() []ochazuke.PropSpec {
	text := func(name, desc string) ochazuke.PropSpec {
		return ochazuke.PropSpec{Name: name, Parameter: ochazuke.Parameter{Type: "string", Description: desc}}
	}
	return []ochazuke.PropSpec{
		// The settings file's keys, each a prop named cfg_KEY.
		text("cfg_url", "The Web UI address (the settings file's url)"),
		text("cfg_user", "The Web UI user (the settings file's user)"),
		text("cfg_volume", "The volume the guard reads free space from (the settings file's volume)"),
		text("cfg_ssh", "The ssh destination for the guard's disk reads (the settings file's ssh)"),
		qbtProp("pass_set", ochazuke.SettingBoolean, "Whether a password is set (config set pass reads it with echo off)"),
		qbtProp("exists", ochazuke.SettingBoolean, "Whether the settings file exists"),
	}
}

// storeRows are the stores' sources: closures over the shell's session.
type storeRows struct {
	torrents   func(context.Context) ([]Torrent, error)
	categories func(context.Context) ([]Category, error)
	trackers   func(ctx context.Context, hash string) ([]Tracker, error)
}

func (s storeRows) torrentStore() ochazuke.Store {
	return ochazuke.Store{Kind: ochazuke.Collection, Facts: true,
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			ts, err := s.torrents(context.Background())
			if err != nil {
				return nil, err
			}
			out := make([]ochazuke.Thing, len(ts))
			for i, x := range ts {
				if out[i], err = ochazuke.ThingOf(x); err != nil {
					return nil, err
				}
			}
			return out, nil
		},
		Get: func(_ ochazuke.Ctx, hash string) (ochazuke.Thing, error) {
			ts, err := s.torrents(context.Background())
			if err != nil {
				return nil, err
			}
			for _, x := range ts {
				if x.Hash == hash {
					return ochazuke.ThingOf(x)
				}
			}
			return nil, fmt.Errorf("no torrent with hash %s (o qbt torrent list)", hash)
		},
	}
}

func (s storeRows) categoryStore() ochazuke.Store {
	thing := func(c Category) ochazuke.Thing { return ochazuke.Thing{"name": c.Name, "savePath": c.SavePath} }
	return ochazuke.Store{Kind: ochazuke.Collection, Facts: true,
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			cs, err := s.categories(context.Background())
			if err != nil {
				return nil, err
			}
			out := make([]ochazuke.Thing, len(cs))
			for i, c := range cs {
				out[i] = thing(c)
			}
			return out, nil
		},
		Get: func(_ ochazuke.Ctx, name string) (ochazuke.Thing, error) {
			name = strings.TrimPrefix(name, "category/")
			cs, err := s.categories(context.Background())
			if err != nil {
				return nil, err
			}
			for _, c := range cs {
				if c.Name == name {
					return thing(c), nil
				}
			}
			return nil, fmt.Errorf("no category %q (o qbt category list)", name)
		},
	}
}

func (s storeRows) trackerStore() ochazuke.Store {
	return ochazuke.Store{Kind: ochazuke.Collection, Facts: true,
		ListParams: map[string]ochazuke.Parameter{
			"torrent": {Type: "string", Required: true, Noun: "torrent", Description: "The torrent's hash: trackers are one torrent's"},
		},
		List: func(_ ochazuke.Ctx, q ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			hash, _ := q.Args["torrent"].(string)
			if hash == "" {
				return nil, errors.New("tracker list needs a torrent (--torrent HASH, or tracker list --target HASH)")
			}
			trs, err := s.trackers(context.Background(), hash)
			if err != nil {
				return nil, err
			}
			rows := trackerRows(trs, false)
			out := make([]ochazuke.Thing, len(rows))
			for i, row := range rows {
				d := row.Data.(TrackerData)
				out[i] = ochazuke.Thing{"host": row.ID, "url": d.URL, "status": d.Status, "tier": d.Tier, "peers": d.Peers, "seeds": d.Seeds, "leeches": d.Leeches, "msg": d.Msg}
			}
			return out, nil
		},
		Missing: map[string]string{"get": "a tracker is read with its torrent: tracker list --torrent HASH"},
	}
}

// cfgProp prefixes a settings file key to make its prop.
const cfgProp = "cfg_"

func (t *Tool) configStore() ochazuke.Store {
	thing := func() (ochazuke.Thing, error) {
		v, err := t.ConfigShow()
		if err != nil {
			return nil, err
		}
		return ochazuke.Thing{cfgProp + "url": v.URL, cfgProp + "user": v.User, cfgProp + "volume": v.Volume, cfgProp + "ssh": v.SSH, "pass_set": v.PassSet, "exists": v.Exists}, nil
	}
	return ochazuke.Store{Kind: ochazuke.Singleton,
		Get: func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return thing() },
		Patch: func(_ ochazuke.Ctx, _ string, changes ochazuke.Thing) (ochazuke.Thing, error) {
			for prop, v := range changes {
				key, ok := strings.CutPrefix(prop, cfgProp)
				if !ok {
					continue
				}
				if _, err := t.ConfigSet(key, fmt.Sprint(v), false); err != nil {
					return nil, err
				}
			}
			return thing()
		},
	}
}

// defineStores gives the read nouns their props and stores, over the
// shell's session (api) and its torrent and category readers.
func (t *Tool) defineStores(r *ochazuke.Runtime, api func() (*Client, error),
	torrentsOf func(context.Context, TorrentQuery) ([]Torrent, []Row, error),
	categoriesOf func(context.Context) ([]Category, []Row, error)) {
	src := storeRows{
		torrents: func(ctx context.Context) ([]Torrent, error) {
			ts, _, err := torrentsOf(ctx, TorrentQuery{})
			return ts, err
		},
		categories: func(ctx context.Context) ([]Category, error) {
			cs, _, err := categoriesOf(ctx)
			return cs, err
		},
		trackers: func(ctx context.Context, hash string) ([]Tracker, error) {
			cl, err := api()
			if err != nil {
				return nil, err
			}
			return Trackers(ctx, cl, hash)
		},
	}
	r.Define("torrent").Props(torrentProps()...).Store(src.torrentStore())
	r.Define("category").Props(categoryProps()...).Store(src.categoryStore())
	r.Define("tracker").Props(trackerProps()...).Store(src.trackerStore())
	r.DefineOwn("config").Props(configProps()...).Store(t.configStore())
}
