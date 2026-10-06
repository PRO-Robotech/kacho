// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package proxycircle_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/proxycircle"
)

// ИМЕНА ЗВЕНЬЕВ В СЕРТИФИКАТЕ (kacho#3028, C4): имя SPIFFE либо имя DNS,
// которое звено фронта несёт в клиентском сертификате. Отказ — всё, что
// совпало бы с сертификатом, которого никто не выдавал звену: подстановочный
// знак, адрес, имя без домена доверия.
func TestParseLinkSANs(t *testing.T) {
	got, err := proxycircle.ParseLinkSANs(" spiffe://kacho.test/ns/kacho/sa/console-front ,api-gateway-front-ingress.kacho.svc,,spiffe://kacho.test/ns/kacho/sa/console-front")
	if err != nil {
		t.Fatalf("законный перечень отвергнут: %v", err)
	}
	if strings.Join(got, ",") != "spiffe://kacho.test/ns/kacho/sa/console-front,api-gateway-front-ingress.kacho.svc" {
		t.Fatalf("перечень %v", got)
	}
	if got, err := proxycircle.ParseLinkSANs(""); err != nil || len(got) != 0 {
		t.Fatalf("пустой ввод: %v, %v", got, err)
	}
	for _, bad := range []string{"*", "*.kacho.svc", "10.244.1.17", "spiffe://", "spiffe:///ns/x", "spiffe://kacho.test", "https://x.test/a", "Front.Kacho"} {
		if _, err := proxycircle.ParseLinkSANs(bad); err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("запись %q принята либо отказ её не называет: %v", bad, err)
		}
	}
}
