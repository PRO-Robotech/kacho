// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// clientaddressreader_injection_test.go — гейт TestClientAddressHasOneReaderInTheEdge
// краснеет на настоящих формах второго читателя и молчит на законных близнецах.
// Синтетика — одна прод-подобная форма на кейс; ровно один факт против близнеца.

const caReaderFile = "gateway/internal/middleware/context_extractor.go"

func judgeClientAddressOne(t *testing.T, rel, src string) ([]string, clientAddressCensus) {
	t.Helper()
	var c clientAddressCensus
	got, err := judgeClientAddressReaders(rel, []byte(src), &c)
	if err != nil {
		t.Fatal(err)
	}
	return got, c
}

func TestClientAddressReaderInjection(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, rel, src string
		red            bool
		mustSay        string
	}{
		{"второй читатель HTTP вне оператора", "gateway/internal/handler/x.go",
			`package h; import "net/http"; func ip(r *http.Request) string { return r.Header.Get("X-Forwarded-For") }`, true, "x.go:1"},
		{"второй читатель в файле оператора, но не в читателе", caReaderFile,
			`package m; import "net/http"; func other(r *http.Request) string { return r.Header.Get("x-real-ip") }`, true, "other"},
		{"константа имени — чтение по построению", "gateway/internal/handler/x.go",
			`package h; const h = "Forwarded"`, true, "Forwarded"},
		{"метаданные моста в читателе (C3)", caReaderFile,
			`package m; import "google.golang.org/grpc/metadata"; func grpcForwarded(md metadata.MD) []string { return md.Get("grpcgateway-x-forwarded-for") }`, true, "C3"},
		{"метаданные моста X-Real-IP в читателе (C3)", caReaderFile,
			`package m; import "google.golang.org/grpc/metadata"; func grpcForwarded(md metadata.MD) []string { return md.Get("Grpcgateway-X-Real-Ip") }`, true, "C3"},
		{"близнец: чтение в читателе HTTP", caReaderFile,
			`package m; import "net/http"; func httpForwarded(r *http.Request) []string { return r.Header.Values("X-Forwarded-For") }`, false, ""},
		{"близнец: чтение в читателе gRPC", caReaderFile,
			`package m; import "google.golang.org/grpc/metadata"; func grpcForwarded(md metadata.MD) []string { return md.Get("x-forwarded-for") }`, false, ""},
		{"близнец: запись заголовка ретрансляцией", "gateway/internal/handler/x.go",
			`package h; import "net/http"; func w(h http.Header) { h.Set("X-Forwarded-For", "a"); h.Del("X-Real-IP") }`, false, ""},
		{"близнец: имя в комментарии", "gateway/internal/handler/x.go",
			"package h\n// читаем X-Forwarded-For только в операторе\nfunc f() {}", false, ""},
		{"близнец: соседнее имя", "gateway/internal/handler/x.go",
			`package h; import "net/http"; func p(r *http.Request) string { return r.Header.Get("X-Forwarded-Proto") }`, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, census := judgeClientAddressOne(t, c.rel, c.src)
			if census.Files != 1 {
				t.Fatalf("перепись файлов %d, ожидался 1", census.Files)
			}
			if c.red != (len(got) > 0) {
				t.Fatalf("ожидалось red=%v, находки %v (перепись %s)", c.red, got, census.Summary())
			}
			if c.red && !strings.Contains(strings.Join(got, "\n"), c.mustSay) {
				t.Fatalf("находка не называет %q: %v", c.mustSay, got)
			}
		})
	}
}

// Предпосылка: читатель, переименованный в дереве, роняет гейт — перечень
// читателей с именами, которых в файле нет, не находит ни одного чтения.
func TestClientAddressReaderPremise_RenamedReaderReadsNothing(t *testing.T) {
	t.Parallel()
	_, census := judgeClientAddressOne(t, caReaderFile,
		`package m; import "net/http"; func renamed(r *http.Request) []string { return r.Header.Values("X-Forwarded-For") }`)
	if census.ReaderReads != 0 {
		t.Fatalf("переименованный читатель засчитан: %s", census.Summary())
	}
}
