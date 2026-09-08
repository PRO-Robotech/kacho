// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dataplane_token_hop_test.go — хоп `docker login` между подами не может быть
// открытым.
//
// Что по нему едет. `docker login` шлёт HTTP Basic на /iam/token, и пароль в этой
// паре — ПРИВАТНЫЙ КЛЮЧ ключа служебной учётки. Сервер его не хранит вовсе: он
// выводит SPKI из предъявленного ключа и сверяет с сохранённым публичным. То есть
// этот хоп — единственное место во всей системе, где приватный ключ транзитит, и
// срок его жизни ничем не ограничен: снятый с провода, он предъявляется напрямую,
// без всякого окна TTL.
//
// Почему это заметили поздно. Слабейшая соседняя нога — 5-минутный bearer на
// loopback внутри одного пода — получила и ack-ручку, и boot-guard с явной
// ссылкой на CWE-319. Нога с бессрочным приватным ключом, идущая через сеть
// кластера, не получила ничего. Несимметричный предикат при починке класса.
package deploy_test

import (
	"strings"
	"testing"
)

// TestDataplaneSidecar_TokenHopIsEncryptedAndVerified — шим токена вызывается по
// https, а сертификат собеседника ПРОВЕРЯЕТСЯ: https без проверки не отличает
// нужного собеседника от подсунутого.
func TestDataplaneSidecar_TokenHopIsEncryptedAndVerified(t *testing.T) {
	cm := docOf(t, helmTemplate(t, lbSets...), "registry-dataplane-nginx-cm.yaml", "kind: ConfigMap")

	if strings.Contains(cm, "proxy_pass http://kaname") {
		t.Errorf("the docker-login hop to the token shim is cleartext — the Basic password on it "+
			"is the service-account key's PRIVATE KEY, and it never expires:\n%s", cm)
	}
	if !strings.Contains(cm, "proxy_pass https://") {
		t.Errorf("token shim upstream is not https:\n%s", cm)
	}
	if !strings.Contains(cm, "proxy_ssl_verify on") {
		t.Errorf("token shim upstream is https but UNVERIFIED — encryption without peer " +
			"authentication does not stop an interposed listener from collecting the key")
	}
	if !strings.Contains(cm, "proxy_ssl_trusted_certificate") {
		t.Errorf("verification is on but no trust anchor is configured — nginx would refuse every " +
			"request, and the next person would 'fix' it by turning verification off")
	}
	if !strings.Contains(cm, "proxy_ssl_name") {
		t.Errorf("no SNI/verify name for the upstream — verification would check the wrong identity")
	}
}

// TestProfiles_EverySidecarProfileUsesEncryptedTokenHop — декларативный гейт:
// профиль, поднимающий sidecar, не объявляет открытый апстрим шима.
func TestProfiles_EverySidecarProfileUsesEncryptedTokenHop(t *testing.T) {
	examined := 0
	for _, p := range umbrellaProfiles(t) {
		tree := umbrellaValues(t, p)
		on, ok := digOpt(tree, "registry", "service", "dataplaneLB", "tlsSidecar", "enabled")
		if !ok || on != true {
			continue
		}
		examined++
		upstream, _ := digOpt(tree, "registry", "service", "dataplaneLB", "tlsSidecar", "iamTokenShimUpstream")
		if s := toStr(upstream); s != "" && strings.HasPrefix(s, "http://") {
			t.Errorf("%s declares a cleartext token-shim upstream %q", p, s)
		}
	}
	if examined == 0 {
		t.Fatal("no profile enables the data-plane TLS sidecar — this gate has lost its subject")
	}
	t.Logf("examined %d profiles enabling the data-plane TLS sidecar", examined)
}
