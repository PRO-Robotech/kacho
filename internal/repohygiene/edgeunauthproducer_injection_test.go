// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

// Инъекции переписи производителей 401 края — в обе стороны, настоящими формами
// записи из дерева.

const edgeUnauthPkg = "package x\n\nimport (\n\t\"net/http\"\n\t\"google.golang.org/grpc/codes\"\n\t\"google.golang.org/grpc/status\"\n)\n\n"

func TestEdgeUnauthInjection_SecondRESTWriterIsFound(t *testing.T) {
	src := edgeUnauthPkg + "func refuse(w http.ResponseWriter) {\n\tw.WriteHeader(http.StatusUnauthorized)\n}\n"
	f, c := findEdgeUnauthProducers(map[string]string{"gateway/internal/x/x.go": src}, nil)
	if len(f) != 1 || f[0].File != "gateway/internal/x/x.go" || f[0].Line != 10 || f[0].Func != "refuse" {
		t.Fatalf("второй писатель 401 не найден с координатой: %+v (перепись %+v)", f, c)
	}
}

func TestEdgeUnauthInjection_SecondNativeBuilderIsFound(t *testing.T) {
	src := edgeUnauthPkg + "func refuse() error {\n\treturn status.Error(codes.Unauthenticated, \"why\")\n}\n"
	f, _ := findEdgeUnauthProducers(map[string]string{"gateway/internal/x/x.go": src}, nil)
	if len(f) != 1 || f[0].Func != "refuse" {
		t.Fatalf("второй построитель UNAUTHENTICATED не найден: %+v", f)
	}
}

// Законные близнецы той же формы: сравнение чужого ответа, классификация чужого
// кода и вызов единственного производителя — молчат.
func TestEdgeUnauthInjection_ReadersAndTheProducerAreSilent(t *testing.T) {
	src := edgeUnauthPkg + `func read(code int, err error) bool {
	if code == http.StatusUnauthorized {
		return true
	}
	switch status.Code(err) {
	case codes.Unauthenticated:
		return true
	}
	return status.Code(err) != codes.Unauthenticated
}
`
	home := "package authnrefusal\n\nimport \"net/http\"\n\nfunc WriteHTTP(w http.ResponseWriter) { w.WriteHeader(http.StatusUnauthorized) }\n"
	f, c := findEdgeUnauthProducers(map[string]string{
		"gateway/internal/x/x.go":                       src,
		"gateway/internal/authnrefusal/authnrefusal.go": home,
		"services/iam/internal/handler/whatever.go":     edgeUnauthPkg + "func f(w http.ResponseWriter) { w.WriteHeader(http.StatusUnauthorized) }\n",
	}, nil)
	if len(f) != 0 || c.Files != 1 {
		t.Fatalf("законные формы краснеют либо область не та: %+v, перепись %+v", f, c)
	}
}

// Исключение Р3 без предмета — находка; с предметом — молчит.
func TestEdgeUnauthInjection_StepUpExemptionExpiresWithItsSubject(t *testing.T) {
	allowed := map[string]int{"gateway/internal/x/x.go#challenge": 1}
	with := edgeUnauthPkg + "func challenge(w http.ResponseWriter) { w.WriteHeader(http.StatusUnauthorized) }\n"
	if f, c := findEdgeUnauthProducers(map[string]string{"gateway/internal/x/x.go": with}, allowed); len(f) != 0 || c.StepUp != 1 {
		t.Fatalf("исключение с предметом краснеет: %+v %+v", f, c)
	}
	without := edgeUnauthPkg + "func challenge(w http.ResponseWriter) {}\n"
	if f, _ := findEdgeUnauthProducers(map[string]string{"gateway/internal/x/x.go": without}, allowed); len(f) != 1 || f[0].Func != "challenge" {
		t.Fatalf("исключение без предмета молчит: %+v", f)
	}
}

// Пустой обход — не вердикт: перепись называет ноль прочитанного.
func TestEdgeUnauthInjection_EmptyWalkReadsZero(t *testing.T) {
	if _, c := findEdgeUnauthProducers(map[string]string{}, nil); c.Files != 0 {
		t.Fatalf("пустой вход дал перепись %+v", c)
	}
}
