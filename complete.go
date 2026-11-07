package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
	"ofam.dev/oden"
)

// completeFilters wraps the shell's complete command (o qbt complete --
// WORDS, what the completion script runs) so filter words complete: the
// first word offers the commands, the shortcuts and the filter words; the
// words after a first word that is no command offer the filter words
// (not-WORD after "not-") and the filter's flags, whose values the
// shell's own completion answers as for torrent find. A first word that
// names a command completes as before.
func (t *Tool) completeFilters(app *oden.App, sh *ocli.Shell, next func(context.Context, []string) error) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		words := args
		if len(words) > 0 && words[0] == "--" {
			words = words[1:]
		}
		if len(words) > 1 && shortcuts[words[0]] != nil {
			// A shortcut completes as the noun and verb it stands for.
			return next(ctx, slices.Concat([]string{"--"}, shortcuts[words[0]], words[1:]))
		}
		if len(words) == 0 || len(words) > 1 && t.isCommand(app, words[0]) {
			return next(ctx, args)
		}
		before, current := words[:len(words)-1], words[len(words)-1]
		op := sh.Runtime.Operations[strings.Join(filterCommand, ".")]
		var cands []string
		switch {
		case strings.HasPrefix(current, "--") && !strings.Contains(current, "="):
			for name := range op.Parameters {
				cands = append(cands, "--"+name)
			}
		case strings.HasPrefix(current, "--") || len(before) > 0 && flagTakesValue(op.Parameters, before[len(before)-1]):
			return next(ctx, append(append([]string{"--"}, filterCommand...), words...))
		default:
			cands = t.wordCandidates(current)
			if len(before) == 0 {
				for _, e := range app.Catalog().Entries {
					cands = append(cands, e.Name)
				}
				for name := range shortcuts {
					cands = append(cands, name)
				}
			}
		}
		cands = slices.DeleteFunc(cands, func(c string) bool { return !strings.HasPrefix(strings.ToLower(c), strings.ToLower(current)) })
		slices.Sort(cands)
		for _, c := range slices.Compact(cands) {
			fmt.Fprintln(t.Stdout, c)
		}
		return nil
	}
}

// flagTakesValue reports whether word is a --NAME of a parameter in ps that
// takes a value (not a boolean, not --NAME=VALUE).
func flagTakesValue(ps map[string]ochazuke.Parameter, word string) bool {
	name, ok := strings.CutPrefix(word, "--")
	if !ok || strings.Contains(name, "=") {
		return false
	}
	p, ok := ps[name]
	return ok && p.Type != "boolean"
}

// isCommand reports whether word, first, runs a command (a shortcut, or
// a command oden resolves, ambiguous included) rather than the filter.
func (t *Tool) isCommand(app *oden.App, word string) bool {
	if shortcuts[word] != nil {
		return true
	}
	res := app.Catalog().Resolve(word)
	return res.Resolved() || res.Ambiguous
}

// wordCandidates are the filter words and aliases, with the not- forms
// when current starts with it.
func (t *Tool) wordCandidates(current string) []string {
	conf, _, _, _ := t.config()
	var out []string
	for _, w := range Vocabulary(trackerTable(conf)) {
		out = append(out, w.Name)
		out = append(out, w.Aliases...)
	}
	if strings.HasPrefix(strings.ToLower(current), NegatePrefix) {
		for i := range out {
			out[i] = NegatePrefix + out[i]
		}
	}
	return out
}
