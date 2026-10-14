// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// relocate_test.go — перенос стенда в своё пространство имён доказывается
// инъекцией: каждый отказ — близнец законного входа, отличающийся одним фактом.
package standns

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const legalStand = `apiVersion: v1
kind: ConfigMap
metadata:
  name: vpc-config
  namespace: t1-x
data:
  config.yaml: |
    peers:
      geo: kacho-geo.kacho.svc:9090
      iam: kaname-internal.kacho.svc.cluster.local:9091
    untouched: notkacho.svc.example
  mode: "on"
  octal: 0644
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: kacho-internal-ca
spec:
  ca:
    secretName: kacho-internal-ca-root
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: kacho-selfsigned
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: kacho-internal-ca-root
  namespace: cm
spec:
  isCA: true
  secretName: kacho-internal-ca-root
  issuerRef:
    name: kacho-selfsigned
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: vpc-server
spec:
  secretName: kacho-vpc-server-tls
  dnsNames: [vpc.t1-x.svc]
  issuerRef:
    name: kacho-internal-ca
    kind: ClusterIssuer
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ui
  annotations:
    cert-manager.io/cluster-issuer: kacho-internal-ca
spec:
  rules: [{host: console.kacho.local}]
`

func relocate(t *testing.T, in string, opts Options) (string, Census, error) {
	t.Helper()
	var out bytes.Buffer
	c, err := Relocate(strings.NewReader(in), &out, opts)
	return out.String(), c, err
}

func docsOf(t *testing.T, s string) []map[string]any {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(s))
	var out []map[string]any
	for {
		var d map[string]any
		if err := dec.Decode(&d); err != nil {
			break
		}
		out = append(out, d)
	}
	return out
}

func TestRelocateMovesAddressesIssuersAndCARootIntoTheStandNamespace(t *testing.T) {
	out, c, err := relocate(t, legalStand, Options{Namespace: "t1-x", CANamespace: "cm"})
	if err != nil {
		t.Fatalf("законный стенд отвергнут: %v", err)
	}
	for _, want := range []string{
		"kacho-geo.t1-x.svc:9090", "kaname-internal.t1-x.svc.cluster.local:9091",
		"notkacho.svc.example", // граница имени: чужое имя с той же подстрокой не тронуто
		"name: kacho-internal-ca-t1-x", "name: kacho-selfsigned-t1-x",
		"secretName: kacho-internal-ca-root-t1-x", "name: kacho-internal-ca-root-t1-x",
		"kacho.io/stand-ns: t1-x",
		`mode: "on"`, "octal: 0644", // нестроковые и «булевоподобные» значения не перетипированы
	} {
		if !strings.Contains(out, want) {
			t.Errorf("в переносе нет %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ".kacho.svc") {
		t.Errorf("адрес рабочего стенда пережил перенос:\n%s", out)
	}
	if strings.Contains(out, "kind: Ingress") {
		t.Errorf("вход с хостом рабочего стенда пережил перенос")
	}
	want := Census{Documents: 6, AddressRewrites: 2, IssuersRenamed: 2, IssuerRefs: 2, CARootsRenamed: 1, IngressesDropped: 1}
	if c != want {
		t.Errorf("перепись %+v, ждали %+v", c, want)
	}
	// Секрет корня рождает cert-manager — метка обязана быть в шаблоне секрета.
	for _, d := range docsOf(t, out) {
		md, _ := d["metadata"].(map[string]any)
		if d["kind"] == "Certificate" && md["namespace"] == "cm" {
			spec := d["spec"].(map[string]any)
			tmpl, _ := spec["secretTemplate"].(map[string]any)
			labels, _ := tmpl["labels"].(map[string]any)
			if labels[StandLabel] != "t1-x" {
				t.Errorf("корень CA без метки стенда в шаблоне секрета: %v", spec)
			}
		}
	}
}

func TestRelocateRefusesWhatItCannotMove(t *testing.T) {
	cases := []struct {
		name, in string
		opts     Options
		want     string
	}{
		{"перенос в пространство рабочего стенда", legalStand, Options{Namespace: WorkingNamespace, CANamespace: "cm"}, "рабочего стенда"},
		{"имя пространства не DNS-1123", legalStand, Options{Namespace: "T1_x", CANamespace: "cm"}, "DNS-1123"},
		{"пространство cert-manager не названо", legalStand, Options{Namespace: "t1-x"}, "cert-manager"},
		{"общекластерный вид, кроме издателя", "apiVersion: networking.k8s.io/v1\nkind: IngressClass\nmetadata:\n  name: nginx\n",
			Options{Namespace: "t1-x", CANamespace: "cm"}, "общекластерный"},
		{"объект в пространстве рабочего стенда", "apiVersion: v1\nkind: Service\nmetadata:\n  name: vpc\n  namespace: kacho\n",
			Options{Namespace: "t1-x", CANamespace: "cm"}, "в пространстве \"kacho\""},
		{"не сертификат в пространстве cert-manager", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: s\n  namespace: cm\n",
			Options{Namespace: "t1-x", CANamespace: "cm"}, "в пространстве \"cm\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := relocate(t, tc.in, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ждали отказ со словами %q, получили %v", tc.want, err)
			}
		})
	}
}
