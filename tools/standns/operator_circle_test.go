// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// operator_circle_test.go — судья личности оператора доказывается инъекцией:
// каждая находка — близнец законного рендера, отличающийся одним фактом
// (kacho#3151).
package standns

import (
	"bytes"
	"strings"
	"testing"
)

const operatorSAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-internal-rest-operator"

func operatorStand(leafURI, circle string) string {
	return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
  namespace: t1-x
spec:
  template:
    spec:
      containers:
        - name: api-gateway
          env:
            - name: KACHO_API_GATEWAY_INTERNAL_REST_ADDR
              value: ":8081"
            - name: ` + OperatorCircleKnob + `
              value: "` + circle + `"
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ` + OperatorCertName + `
  namespace: t1-x
spec:
  secretName: ` + OperatorCertName + `
  uris:
    - "` + leafURI + `"
`
}

func relocateStand(t *testing.T, in string) (string, Census, error) {
	t.Helper()
	var out bytes.Buffer
	c, err := Relocate(strings.NewReader(in), &out, Options{Namespace: "t1-x", CANamespace: "cm"})
	return out.String(), c, err
}

func TestRelocateRefusesAnOperatorLeafOutsideTheEdgeCircle(t *testing.T) {
	// Законный близнец: пространство личности — закреплённое, лист в круге.
	out, c, err := relocateStand(t, operatorStand(operatorSAN, operatorSAN+",spiffe://kacho.cloud/ns/kacho/sa/extra"))
	if err != nil {
		t.Fatalf("законный рендер отвергнут: %v", err)
	}
	if c.OperatorLeaves != 1 {
		t.Fatalf("перепись: ждали один лист оператора, осмотрено %d", c.OperatorLeaves)
	}
	if !strings.Contains(out, operatorSAN) {
		t.Fatalf("имя оператора не дошло до вывода переноса:\n%s", out)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		// Дефект kacho#3151 дословно: лист взял пространство релиза.
		{"лист в пространстве релиза", operatorStand("spiffe://kacho.cloud/ns/t1-x/sa/kacho-internal-rest-operator", operatorSAN), "вне круга края"},
		{"чужое имя учётки", operatorStand("spiffe://kacho.cloud/ns/kacho/sa/other", operatorSAN), "вне круга края"},
		{"чужой домен доверия", operatorStand("spiffe://other.cloud/ns/kacho/sa/kacho-internal-rest-operator", operatorSAN), "вне круга края"},
		{"пустой круг", operatorStand(operatorSAN, ""), "круга " + OperatorCircleKnob + " у края нет"},
		{"лист без имени", operatorStand("", operatorSAN), "без URI-имени"},
	}
	for _, tc := range cases {
		_, _, err := relocateStand(t, tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: ждали отказ с %q, получили %v", tc.name, tc.want, err)
		}
	}
}

func TestOperatorCircleJudgeIsSilentWithoutTheLeaf(t *testing.T) {
	// PKI выключена: листа оператора нет — судить нечего, и это не находка.
	in := strings.SplitN(operatorStand(operatorSAN, ""), "---\n", 2)[0]
	if _, c, err := relocateStand(t, in); err != nil || c.OperatorLeaves != 0 {
		t.Fatalf("рендер без листа оператора: ждали молчание и ноль листов, получили %v, листов %d", err, c.OperatorLeaves)
	}
}
