// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dataplane_token_forwarder_test.go — сайдкар плоскости данных — доверенный
// пересыльщик точки анонимной выдачи токена (kacho#3330, kaname#930).
//
// Служба доступа берёт адрес клиента из X-Forwarded-For только у пира с
// проверенным клиентским листом объявленного пересыльщика; адрес — ключ темпа
// анонимной выдачи на источник. Пересыльщик годен, только если сходятся три
// части, и проба держит каждую в паре с законным близнецом:
//   - K1: хоп `/iam/token` предъявляет клиентский лист реестра — пара путей в
//     настройках nginx, том с секретом клиентского листа в контейнере сайдкара
//     по тем же путям, лист выписан на URI SAN учётки реестра с clientAuth;
//   - K2: заголовок ПЕРЕЗАПИСЫВАЕТСЯ адресом пира (`$remote_addr`), а не
//     дописывается к присланному клиентом;
//   - K3: служба плоскости данных несёт externalTrafficPolicy Local — иначе
//     адрес пира есть адрес узла;
//   - отказ установки сайдкара без mtls, без секрета клиентского листа и с
//     политикой, отличной от Local.
package deploy_test

import (
	"regexp"
	"strings"
	"testing"
)

// tokenLocation — тело `location = /iam/token { … }` настроек сайдкара.
func tokenLocation(t *testing.T, cm string) string {
	t.Helper()
	i := strings.Index(cm, "location = /iam/token {")
	if i < 0 {
		t.Fatalf("в настройках сайдкара нет location = /iam/token — предмет пробы исчез:\n%s", cm)
	}
	j := strings.Index(cm[i:], "\n      }")
	if j < 0 {
		t.Fatalf("location = /iam/token не закрыт:\n%s", cm[i:])
	}
	return cm[i : i+j]
}

// directive — значение директивы nginx в теле, без комментариев; пусто — нет.
func directive(body, name string) []string {
	var out []string
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) + `\s+([^;]+);`)
	for _, line := range strings.Split(body, "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

func TestDataplaneSidecar_TokenHopPresentsTheRegistryClientLeaf(t *testing.T) {
	out := helmTemplate(t, lbSets...)
	loc := tokenLocation(t, docOf(t, out, "registry-dataplane-nginx-cm.yaml", "kind: ConfigMap"))

	crt, key := directive(loc, "proxy_ssl_certificate"), directive(loc, "proxy_ssl_certificate_key")
	if len(crt) != 1 || len(key) != 1 {
		t.Fatalf("хоп /iam/token не предъявляет клиентскую пару (proxy_ssl_certificate %v, "+
			"proxy_ssl_certificate_key %v) — служба доступа не узнает пересыльщика, и все клиенты за "+
			"сайдкаром делят один темп:\n%s", crt, key, loc)
	}
	dir := strings.TrimSuffix(crt[0], "/tls.crt")
	if dir == crt[0] || key[0] != dir+"/tls.key" {
		t.Fatalf("пара путей не из одного каталога секрета: %q, %q", crt[0], key[0])
	}

	dep := docOf(t, out, "deployment.yaml", "kind: Deployment")
	side := dep[strings.Index(dep, "- name: dataplane-tls"):]
	if end := strings.Index(side, "\n      volumes:"); end > 0 {
		side = side[:end]
	}
	mount := regexp.MustCompile(`- name: mtls-client\n\s+mountPath: "` + regexp.QuoteMeta(dir) + `"`)
	if !mount.MatchString(side) {
		t.Fatalf("контейнер сайдкара не монтирует секрет клиентского листа в %s — nginx не стартует "+
			"либо предъявит пустоту:\n%s", dir, side)
	}
	if !strings.Contains(dep, "- name: mtls-client\n          secret:\n            secretName: \"registry-client-tls\"") {
		t.Fatalf("том mtls-client не ссылается на секрет клиентского листа реестра:\n%s", dep)
	}
	leaf := docOf(t, out, "# Source: registry/templates/certificate.yaml", "kind: Certificate\nmetadata:\n  name: \"registry-client-tls\"")
	for _, want := range []string{`- "spiffe://kacho.cloud/ns/kacho/sa/kacho-registry"`, "- client auth", `name: "kacho-internal-ca"`} {
		if !strings.Contains(leaf, want) {
			t.Errorf("клиентский лист реестра без %q:\n%s", want, leaf)
		}
	}
	if !strings.Contains(dep, "checksum/dataplane-nginx: ") {
		t.Errorf("шаблон пода без отпечатка настроек сайдкара — правка карты не перекатит под")
	}
}

func TestDataplaneSidecar_TokenHopOverwritesForwardedFor(t *testing.T) {
	loc := tokenLocation(t, docOf(t, helmTemplate(t, lbSets...), "registry-dataplane-nginx-cm.yaml", "kind: ConfigMap"))
	var xff []string
	for _, v := range directive(loc, "proxy_set_header") {
		if f := strings.Fields(v); len(f) == 2 && strings.EqualFold(f[0], "X-Forwarded-For") {
			xff = append(xff, f[1])
		}
	}
	if len(xff) != 1 || xff[0] != "$remote_addr" {
		t.Fatalf("хоп /iam/token ставит X-Forwarded-For %v, ожидалось ровно [$remote_addr] — дописанный "+
			"к присланному клиентом заголовок отдаёт клиенту выбор ключа темпа:\n%s", xff, loc)
	}
}

func TestDataplaneLB_PreservesClientAddress(t *testing.T) {
	svc := docOf(t, helmTemplate(t, lbSets...), "service-dataplane-lb.yaml", "kind: Service")
	if !strings.Contains(svc, `externalTrafficPolicy: "Local"`) {
		t.Fatalf("служба плоскости данных без externalTrafficPolicy Local — адрес клиента подменяется "+
			"адресом узла:\n%s", svc)
	}
}

func TestDataplaneSidecar_RefusesWithoutForwarderIdentity(t *testing.T) {
	base := []string{
		"zot.auth.password=" + renderPassword,
		"service.dataplaneLB.enabled=true",
		"service.dataplaneLB.tlsSidecar.enabled=true",
	}
	helmTemplateMustFail(t, "mtls.enable=true", base...)
	helmTemplateMustFail(t, "mtls.clientSecretName", append(append([]string{}, lbSets...), "mtls.clientSecretName=")...)
	helmTemplateMustFail(t, "externalTrafficPolicy=Local",
		append(append([]string{}, lbSets...), "service.dataplaneLB.externalTrafficPolicy=Cluster")...)
	helmTemplateMustFail(t, "externalTrafficPolicy=Local",
		append(append([]string{}, lbSets...), "service.dataplaneLB.externalTrafficPolicy=")...)
	// Близнец: без сайдкара те же величины установку не роняют.
	helmTemplate(t, "zot.auth.password="+renderPassword, "service.dataplaneLB.enabled=true",
		"service.dataplaneLB.externalTrafficPolicy=Cluster")
}
