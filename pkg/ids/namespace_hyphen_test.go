// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package ids

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// REG-1 F1 deliverable: hyphen-form id generation for the redesigned Namespace
// resource (id-prefix `reg`→`ns-`). Phase-0 B3 taught the router (validate.ResourceID)
// to ACCEPT the hyphen form; REG-1 adds the GENERATOR (NewHyphenID) — NewID stays
// 3-char-concat by contract. `ns` is a 2-char prefix (∉ 3-char NewID invariant),
// so it needs its own generator that emits "<prefix>-<17 crockford-base32>".

func TestNewHyphenID_NamespaceShape_REG_1_01(t *testing.T) {
	id := NewHyphenID(PrefixNamespace)
	// "ns" (2) + "-" (1) + 17-char body = 20.
	require.Equal(t, "ns", PrefixNamespace, "Namespace hyphen-prefix is `ns` (unified §2, B3)")
	require.True(t, strings.HasPrefix(id, "ns-"), "id %q must start with hyphen-form `ns-`", id)
	require.Len(t, id, len(PrefixNamespace)+1+idBodyLen, "id=%q", id)

	body := id[len(PrefixNamespace)+1:]
	require.Len(t, body, idBodyLen)
	for i := 0; i < len(body); i++ {
		require.Truef(t, isCrockfordChar(body[i]),
			"body[%d]=%q not crockford-base32 (id=%q)", i, body[i], id)
	}
}

func TestNewHyphenID_Unique(t *testing.T) {
	seen := make(map[string]bool, 5000)
	for i := 0; i < 5000; i++ {
		id := NewHyphenID(PrefixNamespace)
		require.Falsef(t, seen[id], "duplicate hyphen id %q at iter %d", id, i)
		seen[id] = true
	}
}

// NewHyphenID guards against a prefix that is not part of the B3 hyphen canon —
// programmer error (prefix comes from a package-level Prefix* constant), so panic.
func TestNewHyphenID_PanicsOnUnknownHyphenPrefix(t *testing.T) {
	require.Panics(t, func() { NewHyphenID("zz") }, "unknown hyphen prefix must panic")
	require.Panics(t, func() { NewHyphenID("") }, "empty prefix must panic")
}

// PrefixNamespace must be registered in the B3 hyphen canon so the router
// (validate.ResourceID) accepts generator output — regression against the reg/rop
// drift class (constant declared but forgotten in the accept-set).
func TestPrefixNamespace_InHyphenCanon(t *testing.T) {
	hy := KnownHyphenPrefixes()
	_, ok := hy[PrefixNamespace]
	require.Truef(t, ok, "PrefixNamespace %q must be in KnownHyphenPrefixes() (B3 canon)", PrefixNamespace)

	// `ns` is a hyphen-form prefix, NOT a legacy 3-char concat prefix: it must NOT
	// be in the concat known-set (else HasKnownPrefix / NewID would mis-treat it).
	require.NotContains(t, KnownPrefixes(), PrefixNamespace,
		"ns is hyphen-form only; must not leak into the 3-char concat known-set")
}
