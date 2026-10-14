// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package issuercanon_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/issuercanon"
)

// Правила канона Р5 — по одному на строку, с законным близнецом: формы одного
// издателя равны, формы другого — нет.
func TestCanonicalFormsOfOneIssuerAreEqual(t *testing.T) {
	const decl = "https://issuer.example.test/realm/"
	for _, same := range []string{
		"https://issuer.example.test/realm",
		"HTTPS://ISSUER.EXAMPLE.TEST/realm/",
		"https://issuer.example.test:443/realm/",
		"https://issuer.example.test./realm/",
		"https://issuer.example.test/%72ealm/",
		decl,
	} {
		if !issuercanon.Equal(decl, same) {
			a, _ := issuercanon.Canonical(decl)
			b, _ := issuercanon.Canonical(same)
			t.Errorf("%q и %q — один издатель, канон дал %q и %q", decl, same, a, b)
		}
	}
	for _, other := range []string{
		"https://issuer.example.test/Realm/",
		"https://issuer.example.test/realm2/",
		"http://issuer.example.test/realm/",
		"https://issuer.example.test:8443/realm/",
		"https://other.example.test/realm/",
		"https://issuer.example.test/realm%2F",
		"issuer.example.test/realm",
	} {
		if issuercanon.Equal(decl, other) {
			t.Errorf("%q и %q — разные издатели, канон их слил", decl, other)
		}
	}
}

func TestCanonicalFormIsStableAndExplicit(t *testing.T) {
	for in, want := range map[string]string{
		"HTTP://Host.Example:80/":            "http://host.example",
		"https://h.example/a%2fb":            "https://h.example/a%2Fb",
		"https://h.example/%7Euser?Q=%41%2f": "https://h.example/~user?Q=A%2F",
		"https://[::1]:443/x/":               "https://[::1]/x",
		"https://[::1]:8443/x":               "https://[::1]:8443/x",
	} {
		got, err := issuercanon.Canonical(in)
		if err != nil || got != want {
			t.Errorf("Canonical(%q) = %q, %v; ожидалось %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "issuer.example.test/realm", "ftp://h.example/x", "https:///x", "https://h.example/%zz"} {
		if got, err := issuercanon.Canonical(bad); err == nil {
			t.Errorf("Canonical(%q) = %q — не абсолютный http(s)-URL обязан отвергаться", bad, got)
		}
	}
}
