// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package ids

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewHyphenID_Form — going-forward hyphen-form generator (B3 canon, COMP-1
// compute redesign migration point). NewHyphenID emits "<prefix>-<17 crockford>"
// (e.g. "mt-…", "ins-…") — unlike legacy NewID's concat form. Accepts the 2-char
// (`mt`) and 3-char (`ins`) hyphen-canon prefixes. Body reuses the same crockford
// generator as NewID (idBodyLen chars).
func TestNewHyphenID_Form(t *testing.T) {
	for _, p := range []string{PrefixMachineTypeHyphen, PrefixInstanceHyphen} {
		id := NewHyphenID(p)
		require.Truef(t, strings.HasPrefix(id, p+"-"), "id %q must start with %q-", id, p)
		require.Equalf(t, len(p)+1+idBodyLen, len(id), "id %q length", id)
		body := id[len(p)+1:]
		require.Lenf(t, body, idBodyLen, "body %q", body)
		for i := 0; i < len(body); i++ {
			require.Truef(t, isCrockfordChar(body[i]), "id %q body has non-crockford char %q", id, body[i])
		}
	}
}

// TestNewHyphenID_Unique — two calls yield distinct ids (crypto/rand body).
func TestNewHyphenID_Unique(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		id := NewHyphenID(PrefixMachineTypeHyphen)
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate hyphen id %q", id)
		}
		seen[id] = struct{}{}
	}
}

// TestNewHyphenID_PanicsOnBadPrefix — prefix must be 2..3 chars (programmer error
// otherwise; prefix comes from a package-level constant).
func TestNewHyphenID_PanicsOnBadPrefix(t *testing.T) {
	require.Panics(t, func() { NewHyphenID("x") })       // 1 char
	require.Panics(t, func() { NewHyphenID("toolong") }) // >3
	require.Panics(t, func() { NewHyphenID("") })
}

// TestHyphenPrefixConstants_InCanon — the exported hyphen constants must be part
// of the KnownHyphenPrefixes router set (else validate.ResourceID would reject the
// well-formed hyphen id NewHyphenID produces).
func TestHyphenPrefixConstants_InCanon(t *testing.T) {
	canon := KnownHyphenPrefixes()
	for _, p := range []string{PrefixMachineTypeHyphen, PrefixInstanceHyphen} {
		if _, ok := canon[p]; !ok {
			t.Errorf("hyphen prefix %q missing from KnownHyphenPrefixes — validate.ResourceID would reject %q- ids", p, p)
		}
	}
}
