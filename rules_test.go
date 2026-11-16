package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke/rulecheck"
)

// TestRules runs ochazuke's hiddenkeys and deadkeys checks over o-qbt's
// runtime: a hidden default row may not take a key that did something at the
// same place without it. testdata/rules/places.json holds the places of every
// noun as a running mtm recorded them (noun, ancestors, modal; the "qbt."
// namespace is dropped from each noun, since mtm mounts the nouns with it and
// this runtime names them without it), so the pages, trees and forms the
// fixture does not build are walked too. The other checks have their own tests
// and baselines.
func TestRules(t *testing.T) {
	home := t.TempDir()
	tool := &Tool{Stdout: io.Discard, Stderr: io.Discard, Stdin: strings.NewReader(""), Home: home, UID: 501, Prog: "o-qbt", Env: []string{"HOME=" + home}, now: time.Now}
	places, err := rulecheck.LoadPlaces("testdata/rules/places.json")
	if err != nil {
		t.Fatal(err)
	}
	rulecheck.Check(t, rulecheck.Config{
		Runtime: tool.shell().Runtime,
		Places:  places,
		Off: map[string]string{
			"names":    "TestNoNewNameBranches holds it",
			"refusals": "no refusecheck baseline here; not part of this check",
			"lexicon":  "TestLexicon holds it",
			"nouns":    "TestNouns holds it",
		},
	})
}
