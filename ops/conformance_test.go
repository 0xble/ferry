package ops

import (
	"testing"
	"time"

	"github.com/0xble/toolkit/toolkittest"

	"github.com/0xble/ferry/share"
)

// seededID is the live share of doc.md every conformance fixture starts with.
const seededID = "seeded-0001"

// TestConformance runs the toolkit conformance kit over every operation,
// against a real daemon on loopback per fixture. The files to publish live in
// one directory shared by every fixture, so the cases can name them.
func TestConformance(t *testing.T) {
	w := newWorld(t)
	doc := w.file("doc.md", "# Doc\n")
	notes := w.file("notes.txt", "notes\n")
	newFixture := func(testing.TB) toolkittest.Fixture {
		f := newWorldIn(t, w.dir)
		f.daemon.Seed(t, seededID, doc, share.ModeLive, false, time.Hour)
		return toolkittest.Fixture{Registry: f.registry(), State: func() any { return f.daemon.Shares(t) }}
	}

	toolkittest.Run(t, toolkittest.Suite{
		New:     newFixture,
		Options: Options(w.b),
		Cases: map[string]toolkittest.Case{
			"shares.list": {Input: map[string]any{}, Args: []string{"list"}},
			"share.get":   {Input: map[string]any{"id": seededID}, Args: []string{"get", seededID}},
			"doctor":      {Input: map[string]any{}, Args: []string{"doctor"}},
			"share.publish": {Input: map[string]any{"path": notes, "expires_in": "24h"},
				Args: []string{"publish", notes, "--expires-in", "24h"}},
			"share.renew":   {Input: map[string]any{"id": seededID, "for": "48h"}, Args: []string{"renew", seededID, "--for", "48h"}},
			"share.unshare": {Input: map[string]any{"target": seededID}, Args: []string{"unshare", seededID}},
		},
	})
}
