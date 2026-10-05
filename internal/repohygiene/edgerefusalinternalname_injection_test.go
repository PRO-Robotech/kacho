// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

// Инъекции гейта «текст отказа не называет внутреннюю службу» — в обе стороны,
// по каждой из пяти форм записи: дефект краснеет с координатой и формой,
// законный близнец той же формы молчит.

var edgeRefusalTestComponents = append([]string{"vpc", "compute"}, EdgeComponentNames...)

const edgeRefusalPkg = "package x\n\nimport (\n\t\"net/http\"\n\t\"google.golang.org/grpc/codes\"\n\t\"google.golang.org/grpc/status\"\n)\n\n"

type edgeRefusalCase struct {
	form, leak, twin string
}

func edgeRefusalCases() []edgeRefusalCase {
	return []edgeRefusalCase{
		{"status",
			"func f() error { return status.Error(codes.Unavailable, \"authz service unavailable\") }\n",
			"func f() error { return status.Error(codes.Unavailable, \"authorization could not be decided; try again later\") }\n"},
		{"json",
			"const body = `{\"code\":14,\"message\":\"` + why + `\"}`\nconst why = \"kaname-internal:9091 did not answer\"\n",
			"const body = `{\"code\":14,\"message\":\"` + why + `\"}`\nconst why = \"service unavailable; try again later\"\n"},
		{"map",
			"func f(w http.ResponseWriter) { _ = map[string]any{\"code\": 14, \"message\": \"vpc backend unavailable\"} }\n",
			"func f(w http.ResponseWriter) { _ = map[string]any{\"code\": 7, \"message\": \"permission denied: vpc.networks.delete\"} }\n"},
		{"field",
			"type refusal struct{ msg string }\nvar r = refusal{msg: \"InternalSessionRevocationsService did not answer\"}\n",
			"type refusal struct{ msg string }\nvar r = refusal{msg: \"subscription stream is read-only; use GET\"}\n"},
		{"arg",
			"func writeRefusal(w http.ResponseWriter, m string) {}\nfunc f(w http.ResponseWriter) { writeRefusal(w, \"openfga store unreachable\") }\n",
			"func writeRefusal(w http.ResponseWriter, m string) {}\nfunc f(w http.ResponseWriter) { writeRefusal(w, \"end the browser session with POST /iam/v1/auth/logout\") }\n"},
	}
}

func TestEdgeRefusalInjection_EveryFormLeakIsFound(t *testing.T) {
	t.Parallel()
	for _, c := range edgeRefusalCases() {
		f, census := FindEdgeRefusalInternalNames(map[string]string{"gateway/internal/x/x.go": edgeRefusalPkg + c.leak}, edgeRefusalTestComponents)
		if len(f) != 1 || f[0].Form != c.form || f[0].File != "gateway/internal/x/x.go" || f[0].Line == 0 {
			t.Errorf("форма %s: утечка не найдена одной находкой с координатой: %+v (перепись %+v)", c.form, f, census)
		}
	}
}

func TestEdgeRefusalInjection_EveryFormLawfulTwinIsSilent(t *testing.T) {
	t.Parallel()
	for _, c := range edgeRefusalCases() {
		f, census := FindEdgeRefusalInternalNames(map[string]string{"gateway/internal/x/x.go": edgeRefusalPkg + c.twin}, edgeRefusalTestComponents)
		if len(f) != 0 {
			t.Errorf("форма %s: законный близнец краснеет: %+v", c.form, f)
		}
		if census.ByForm[c.form] == 0 {
			t.Errorf("форма %s: близнец не прочитан как текст отказа — молчание значило бы «не видел», а не «чисто» (%+v)", c.form, census)
		}
	}
}

// Вне края и в проверочных файлах суда нет.
func TestEdgeRefusalInjection_ScopeIsTheEdgeNonTestTree(t *testing.T) {
	t.Parallel()
	leak := edgeRefusalCases()[0].leak
	f, census := FindEdgeRefusalInternalNames(map[string]string{
		"services/vpc/internal/x.go":   edgeRefusalPkg + leak,
		"gateway/internal/x/x_test.go": edgeRefusalPkg + leak,
	}, edgeRefusalTestComponents)
	if len(f) != 0 || census.Files != 0 {
		t.Fatalf("область шире края либо судятся пробы: %+v, перепись %+v", f, census)
	}
}
