# o-qbt

A family tool for qBittorrent (Web API v2) on the NAS: a credentials test, torrents and categories as rows, transfer limits, and a free-space guard that stops downloads (never seeds) before they fill the volume. `o qbt ARGS` runs it; nabe (`o tui`) shows it after it is registered.

## Install

```sh
go build -o ~/bin/o-qbt .
o qbt config init          # writes ~/.config/o-qbt/config.json (mode 600) with an empty pass
o qbt config set pass      # prompts with echo off; or: printf %s "$PW" | o qbt config set pass --stdin
o qbt app info             # the credentials test
```

`config.json` is `{url, user, pass, volume, ssh, guard}`: `http://nas:8012` (or `http://10.0.0.200:8012` on the LAN), `admin`, the password, `/volume1` and the ssh alias `nas` the guard runs `df` through. It lives in `$XDG_CONFIG_HOME/o-qbt/` when that is set. `O_QBT_URL`, `O_QBT_USER` and `O_QBT_PASS` override it. `config show` prints everything but the password (only whether one is set); `config set KEY VALUE` sets the others, `guard.mode`, `guard.minFree` and `guard.resumeAbove` included. The password is never taken from the command line.

## Ban caution

qBittorrent bans an IP after repeated failed logins. A process logs in at most once: a refused login (`login refused: check user/pass; ...`) is the command's error and every later request in that process returns it without trying again. Do not loop a command that is failing to log in; fix the password with `o qbt config set pass` and run `o qbt app info` once. A 403 on login means the IP may already be banned: wait for the ban to lapse or clear it in the web UI.

## Commands

```
o qbt app info [output] | prefs | rows
o qbt torrent list | active | seeding | downloading | paused | errored | stalled   [query] [output]
o qbt torrent export --out FILE [query] [output] | summary [query] [output] | overview [output]
o qbt torrent show --target HASH
o qbt torrent pause|resume|recheck --target HASH | --all
o qbt torrent delete --target HASH [--files] [--yes]
o qbt torrent delete --hashes H1,H2 | [--status STATUS] [--filter F] [--category C] [--tag T]  [--files] [--yes]
o qbt tracker list --target HASH [--full-url] [output] | summary [query] [output] | hosts [query] [output]
o qbt transfer info [output] | limit --dl N --up N | speed-limits-toggle     (bytes/s, 0 = unlimited)
o qbt guard check|show|rebase|resume [--mode floor|allowance] [--min-free 5GiB] [--resume-above 20GiB] [--limit 10G] | rebase --bytes N | install | uninstall | log [N]
o qbt category list [output]
o qbt torrents | categories | overview | words | space   (shortcuts)
o qbt WORD... [--tracker P] [--name P] [--match substring|fuzzy|regex|glob] [query] [output]   (filter words: torrent find)
o qbt space [free|base|used|headroom|min-free|resume-above|limit|qbt-free] [--bytes]          (guard disk)
o qbt tracker registry | counts [WORD...] [query] [output] | summary --by host|name
o qbt config init | show | set KEY [VALUE] [--stdin]
o qbt catalog
```

Bare noun groups such as `o qbt tracker` (or `o qbt tracker --help`) show their scoped verbs and descriptions without contacting qBittorrent. Shared oden/ochazuke help also supports operation help, such as `o qbt tracker list --help`, with usage and parameter metadata.

`app info` changes nothing: it reads `app/version`, `app/webapiVersion`, `app/buildInfo`, `transfer/info` and `sync/maindata` and exits 0 only when the login worked. `torrent pause`/`resume` use v5's `torrents/stop`/`start` and fall back to v4's `pause`/`resume` on a 404. `torrent delete` asks `Delete torrent LABEL?` unless `--yes`. With a selection instead of `--target` (`--hashes`, or torrents/info's `--filter`/`--category`/`--tag` narrowed by `--status`, the exact qBittorrent state), it posts `torrents/delete` in batches of 200 hashes and prints `deleted N/TOTAL` on stderr when there is more than one batch.

A torrent row is `{id: HASH, label: "status  progress%  name", data: {status, progress, size, dlspeed, eta, category, added_on, completed_on, save_path}}`; a category row is `{id: "category/NAME", label, data: {name, savePath}}`. `app rows` prints both lists as JSON Lines `{noun, id, label}` (the catalog's state). List-like verbs print in `--format` (below). Other verbs a person reads (`guard *`, `config *`, `transfer limit`) print a short table; with `--json '{...}'` (how a host calls) every verb prints JSON.

## Listing and exporting

The query is qBittorrent's own (`torrents/info`), passed through:

| Flag | Values |
|---|---|
| `--filter F` | `all downloading seeding completed paused stopped active inactive resumed running stalled stalled_uploading stalled_downloading errored checking moving` |
| `--category C` / `--tag T` | one category or tag (`""` = none) |
| `--sort FIELD` | `name size progress dlspeed upspeed eta ratio added_on completed_on status` (server-side; without it the list is by name) |
| `--reverse`, `--limit N` | reverse the order, keep the first N |

`seeding`, `downloading`, `paused`, `errored` and `stalled` are saved filter rows of the torrent noun (a status condition each; `torrent` rows in a list or the TUI offer them, `collection.filters` in `settings.json` adds or replaces them); the commands `torrent active|seeding|...` are gone, use `torrent list --filter seeding` (qBittorrent's own filter, `active` included) or the filter words (`o qbt seeding`).

Output, on `torrent list|export|summary|overview`, the presets, `category list`, `transfer info` and `app info`:

| Flag | |
|---|---|
| `--format F` | `table` (the default on a terminal), `json` (the default otherwise), `jsonl`, `tsv`, `csv`, `md`, `toon` |
| `--columns a,b,c` | which columns, in order, for table/tsv/csv/md/toon (json and jsonl carry every field) |
| `--out FILE` | write instead of printing; with no `--format` the extension picks it (`.csv .tsv .md .json .jsonl .toon`, `.txt` = table, else json) |

`table` and `md` humanise sizes, speeds, eta and times (`1.2 GiB`, `3.4 MiB/s`, `2h 10m`, `∞`, `2026-10-25 12:30`); every other format carries the raw numbers. A torrent's columns are `hash name state progress size dlspeed eta` (the default) then `upspeed ratio category tags added_on completed_on save_path`. A host's `--json '{}'` without a `format` still gets the rows (`{id, label, data}`); `--json '{"format":"csv"}'` renders.

```sh
o qbt torrent list --filter seeding --sort ratio --reverse --limit 10
o qbt torrent list --category tv --format md --columns name,size,ratio
o qbt torrent export --filter completed --out done.csv
o qbt torrents --format toon | pbcopy
o qbt torrent summary            # count and size per status, total last
o qbt overview                   # one screen: totals and guard, by status, top 5 down/up, 5 oldest unfinished, categories
```

`torrent overview` (also `o qbt overview`) prints a totals line (torrents, active, seeding, downloading, paused, errored, size, speeds, the free space qBittorrent reports, the guard), the per-state table, the top 5 by download and by upload speed (moving torrents only), the 5 oldest unfinished, and the categories with their counts. `--format json` is the whole structure `{totals, states, top_download, top_upload, oldest_unfinished, categories}`; md, toon, csv, tsv and jsonl print each section in turn (md under `## name`, the others after `# name`, toon under its own header).

### TOON

`--format toon` is Token-Oriented Object Notation for uniform rows:

```
torrent[3]{hash,name,status,progress,size,dlspeed,eta}:
  aaa,alpha,downloading,0.4217,4096,3565158,60
  ccc,"Movie, The \"Cut\"",pausedDL,0.5,1288490188,0,7800
```

The rule, exactly: a header `NAME[N]{col,col,...}:` (N records, the columns in order), then one line per record, indented two spaces, its values joined by `,`. Numbers and booleans are bare (raw: bytes, bytes/s, seconds, 0..1 progress, Unix times); a missing value is empty. A string is bare unless it contains a `,`, a `"` or a newline; then it is wrapped in `"` with `\` `"` and newline written `\\` `\"` `\n`. A string that looks like a number is not quoted, so a reader types values by column.

## Filter words

`o qbt` is mostly for finding torrents, so filter words mix and match at the top level:

```sh
o qbt avz completed inactive          # avistaz AND completed AND inactive
o qbt avz nyaa completed inactive     # (avistaz OR nyaa) AND completed AND inactive
o qbt private fseeding                # force-started seeds on any private tracker
o qbt tl not-completed                # torrentleech, not finished
o qbt seeding fseeding --sort ratio   # seeding, forced or not
o qbt --tracker '*.to' --match glob   # any tracker host ending in .to
o qbt nyaa --name 'S01' --format md --columns name,status,trackers
```

Each word belongs to one category. **Words of one category OR, categories AND**: `avz nyaa completed inactive` is (avistaz OR nyaa) AND completed AND inactive. `not-WORD` drops the torrents WORD selects (`not-avz not-nyaa`: neither). Only bare words and `--flags` are used, so nothing needs quoting in bash or zsh except a glob or regex pattern you pass to `--tracker`/`--name` (`'*.to'`). The result is the torrent list: `torrent list`'s columns and its `--format`, `--columns`, `--out`, `--filter`, `--category`, `--tag`, `--sort` and `--reverse`; `--limit` cuts after the filter. The columns `trackers` (the tracker hostnames, never the announce URLs) and `private` are there for `--columns`. `o qbt words` lists every word (`--format` renders it); `o qbt torrent find WORD...` is the same command spelled out.

| Category | Words | A torrent's value |
|---|---|---|
| tracker | the tracker table's names and aliases: `avistaz` (`avz`, `av`), `privatehd` (`phd`), `cinemaz`, `exoticaz`, `animez`, `torrentleech` (`tl`), `nyaa`, `myanonamouse` (`mam`), `jptv` | the table names owning one of its tracker hosts (a host or a subdomain of one), from the current tracker and the magnet URI's tracker list |
| privacy | `public`, `private` | the torrent's private flag (qBittorrent 5); before that, whether a private table entry owns one of its hosts |
| completion | `completed` (`done`), `incomplete` | progress is 100% or not |
| activity | `active`, `inactive` (`idle`) | moving data now (below) |
| status | the groups below, and every literal qBittorrent state | its state |

**Active** means moving data now: download or upload speed above zero, or a state qBittorrent gives only while data moves (`downloading`, `uploading`, `metaDL`, `forcedMetaDL`, `moving`). Everything else is inactive. Forced is not activity: a forced seed nobody is downloading from is inactive. This differs from `torrent active` (qBittorrent's own `active` filter), which counts every forced torrent as active.

State groups; a plain group and its forced one (`f` prefix) never overlap, so `seeding fseeding` is both:

| Word (aliases) | States |
|---|---|
| `download` (`downloading`) | downloading, stalledDL, queuedDL, metaDL, checkingDL, allocating |
| `fdownload` (`force-download`, `forced-download`) | forcedDL, forcedMetaDL |
| `seeding` (`seed`) | uploading, stalledUP, queuedUP |
| `fseeding` (`force-seeding`, `forced-seeding`) | forcedUP |
| `started` | the download and seeding states, checkingUP, moving (not stopped, not forced) |
| `fstarted` (`force-started`, `forced`) | forcedUP, forcedDL, forcedMetaDL |
| `stopped` (`paused`) | stoppedUP, stoppedDL, pausedUP, pausedDL |
| `queued` | queuedUP, queuedDL |
| `stalled` | stalledUP, stalledDL |
| `checking` | checkingUP, checkingDL, checkingResumeData |
| `errored` | error, missingFiles |
| a literal state | `stalledUP`, `missingFiles`, ... in any case; `downloading` is the group's alias, so the literal state alone is `download not-stalledDL not-queuedDL not-metaDL not-checkingDL not-allocating` |

`torrent list --filter seeding|downloading|active|...` asks qBittorrent's own filter, which counts forced torrents in `seeding` and `downloading`.

**How a word resolves**: an exact word or alias (any case; a group wins over a literal state of the same name), else the one word it is a prefix of (`comp` → completed, `stalledu` → stalledUP, `torr` → torrentleech), else the one tracker whose name or host holds it (`leech`, `tleech` → torrentleech). Several candidates is an error naming them (`"pri" is ambiguous: private (privacy), privatehd (tracker)`); no candidate is an error pointing at `o qbt words`.

**The first word**: a first word that names a command, a unique prefix of one (oden's rule) or a shortcut runs that command, exactly as before (`o qbt torrent list`, `o qbt tr` is ambiguous). Anything else is filter words; a first word that is no filter word either is oden's `command not found` with its suggestion. No filter word or alias is a command or a prefix of one (a test checks), but a prefix you type first can be: `o qbt torr` is `torrent`, so put another word first (`o qbt private torr`) or type the whole name.

**Patterns**: `--tracker P` matches any tracker table name or host, `--name P` the torrent's name, in `--match` mode: `substring` (the default; `substr`; every space-separated word, any order), `fuzzy` (the characters in order), `regex` (Go RE2), `glob` (`*`, `?`, `[...]`; quote it), all case-insensitive unless the pattern has a capital (smart case; glob always ignores case). The first three are ochazuke's match modes. `--tracker` is in the tracker category, so it ORs with tracker words; `--name` is its own category.

**The tracker table** (`o qbt tracker registry`) lives in the code with additions in `config.json`: an entry named like a built-in replaces it, any other is added.

```json
"trackers": [
  {"name": "nyaa", "aliases": ["ny"], "hosts": ["nyaa.si", "nyaa.tracker.wf"], "private": false},
  {"name": "opentrackr", "aliases": ["otr"], "hosts": ["opentrackr.org"]}
]
```

`o qbt tracker counts [WORD...]` counts torrents per table name (a torrent no entry owns counts under each of its hosts) with size, completed and active, in one request; it takes the filter words (`o qbt tracker counts completed inactive`). `o qbt tracker summary --by name` is the per-tracker status summary grouped by table name instead of host.

**Completion**: `o qbt completion zsh` (or `bash`) prints the completion script; the first word completes commands, shortcuts and filter words, later words complete filter words (`not-` forms after `not-`), the flags, `--match` modes and `--tracker` names.

## Free space

`o qbt space` (`guard disk`) prints the numbers the guard weighs:

| Field | |
|---|---|
| `free` | free space on the guard's volume now (`ssh nas df`) |
| `base` | the stored baseline (`guard rebase`) |
| `used` | baseline minus free (allowance mode's measure) |
| `headroom` | free minus `min-free`: room before floor mode stops downloads |
| `min-free`, `resume-above`, `limit` | the guard's thresholds (config, or `--min-free`, `--resume-above`, `--limit`) |
| `qbt-free` | the free space qBittorrent reports for its save path |

`o qbt space FIELD` prints one of them alone, as a size (`280.4 GiB`), or the bytes with `--bytes`; only `qbt-free` (and the whole table) logs in to qBittorrent. A df failure is shown in the table and is the error of a single field that needs it.

```sh
o qbt space
o qbt space free --bytes
o qbt space base
```

## Trackers

`torrents/trackers?hash=H` answers one torrent's trackers; the DHT, PeX and LSD entries (`** [DHT] **` and so on) are skipped everywhere. A tracker's host is its scheme and host (`https://tracker.example.org`, `udp://open.example.net:1337`): no user info, path or query, because private trackers put the passkey there. A URL with no host is `(unparsed)`, never the URL.

- `tracker list --target HASH`: rows `{id: host, label: "host  status", data: {url, status, tier, peers, seeds, leeches, msg}}` (a host listed again is `host#2`). `data.url` is the host; `--full-url` puts the whole announce URL there and in a `url` column, for the owner's own use only. Columns `host status tier peers seeds leeches msg` (the default), `url`.
- `tracker summary [--filter F] [--category C] [--tag T]`: one row per host across the torrents the query selects, most torrents first: `host`, `torrents` (how many list it), `working` (torrents with an entry there in status 2), `not-working` (an entry in status 4 and none working), `msg sample` (the host's most common message, 60 characters).
- `tracker hosts [query]`: the distinct hosts, one per line, for piping; `--format` renders them as a table.

`summary` and `hosts` make one request per torrent, 8 at a time, with a `trackers: N/M torrents` line on stderr. Status codes: 0 disabled, 1 not contacted, 2 working, 3 updating, 4 not working.

```sh
o qbt tracker summary --filter errored --format table   # which trackers the errored torrents (error, missingFiles) use
o qbt tracker hosts --filter errored | wc -l
o qbt tracker list --target HASH
```

## The guard

`guard check` is the monitor step: it reads the volume's free space with `ssh nas df -B1 --output=avail /volume1` and applies one of two rules, `guard.mode` in `config.json` (or `--mode`).

**It stops downloads only, never seeds.** A pause stops the torrents whose state is a downloading one: `downloading`, `stalledDL`, `metaDL`, `forcedMetaDL`, `forcedDL`, `queuedDL`, `checkingDL` and `allocating`. Forced downloads (`forcedDL`, `forcedMetaDL`) are downloads and are stopped too; the guard restores their force start when it starts them again. Seeding torrents (`uploading`, `stalledUP`, `forcedUP`, `queuedUP`, `checkingUP`), stopped ones (`stoppedDL`, `stoppedUP`; v4's `pausedDL`, `pausedUP`), `checkingResumeData`, `moving`, `error` and `missingFiles` are left alone, so seeding goes on and a private tracker sees no hit-and-run. The marker (`~/.local/share/o-qbt/paused`) is JSON listing what the guard stopped, `{"time": "...", "torrents": [{"hash", "name", "state", "forced"}]}`; starting again starts those hashes and nothing else, so a torrent stopped by hand stays stopped. A marker from an older o-qbt (a bare timestamp) still reads, as one that lists no torrents: resuming it starts none and says so.

**floor** (the default): when free space is below `minFree` (default 5 GiB; `guard.minFree`, `--min-free 5GiB`) it stops the downloads and writes the marker. While free space stays below `minFree`, each check also stops any download that started since (an RSS feed or an *arr adding one) and adds it to the marker. Between `minFree` and `resumeAbove` checks log `skipped-already-paused`; once free space is above `resumeAbove` (default 20 GiB, an absolute level; `guard.resumeAbove`, `--resume-above 20GiB`) the guard starts the torrents the marker lists and clears it. Without a marker it starts nothing. `resumeAbove` must be above `minFree`.

**allowance** (opt-in): when the stored baseline minus the free space is more than `--limit` (default 10 GiB) it stops the downloads and writes the marker (and, while still over the limit, stops new ones), and stays paused until `guard resume` (start the marker's torrents, clear the marker) or `guard rebase` (store the current free space, or `--bytes N`, as the baseline and clear the marker without starting anything). Only this mode reads the baseline; `rebase` in floor mode stores it and says so.

When the rule says pause but nothing is downloading, the check logs `no-downloads` and writes no marker: there is nothing to start again, and the next check looks again. `guard resume` starts the marker's torrents in either mode; to start every torrent use `o qbt torrent resume --all`.

```sh
o qbt config set guard.mode floor            # or allowance
o qbt config set guard.resumeAbove 30GiB
o qbt config set guard.minFree 8GiB
```

```json
"guard": {"mode": "floor", "minFree": "5GiB", "resumeAbove": "20GiB"}
```

Without a password a check logs `error:no-credentials` and exits 0. Each check, rebase and resume appends one line (GiB) to `~/.local/share/o-qbt/guard.log`. A pause says how many downloads it stopped and how many torrents (seeds and the rest) it left alone; a resume says how many it started and how many it force-started:

```
2026-10-25T12:30:00-0400 mode=floor min=5.0 resume=20.0 free=577.1 baseline=584.8 used=7.7 limit=10.0 action=ok
2026-10-25T12:35:00-0400 mode=floor min=5.0 resume=20.0 free=4.2 baseline=584.8 used=580.6 limit=10.0 action=paused downloads=3 left-alone=212
2026-10-25T19:05:00-0400 mode=floor min=5.0 resume=20.0 free=24.0 baseline=584.8 used=560.8 limit=10.0 action=resumed downloads=3 forced=1
```

`guard install` writes `~/Library/LaunchAgents/com.unixsuperhero.o-qbt-guard.plist` (runs `~/bin/o-qbt guard check` every 300 s and at load; stdout and stderr in `~/Library/Logs/o-qbt/`), lints it and bootstraps it in `gui/UID`. `guard show` (and the `guard status` prop, as JSON) shows the agent, the last log line, the mode and its thresholds (`min free`, `resume`), free space (against the baseline and limit in allowance mode) and whether the guard paused (since when, and how many downloads). `guard uninstall` removes the agent and keeps the data.

It replaces the earlier shell guard (`qbt-guard.sh` on the NAS, driven by `com.unixsuperhero.qbt-guard`).

## Register in nabe

```sh
MTM_SERVER=default o-nabe extension register o-qbt
```

## Development

`gofmt -l . && go vet ./... && go test -race ./... && go run . catalog | head -5`. Tests run against a fake qBittorrent (`fake_test.go`) with an injected `df` and clock; they never reach the NAS. `go test -run Catalog -update` rewrites `testdata/catalog.golden`. oden and ochazuke are required at their tags (`v0.1.0`); an untracked `go.work` (`go work init . && go work use ../oden ../ochazuke`) builds against the sibling checkouts, and `GOWORK=off` builds from the tags as a fresh clone does.
