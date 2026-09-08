// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttokensurface_injection_test.go — доказательство, что разбор поверхностей
// СПОСОБЕН упасть и способен смолчать.
//
// Инъекция ставится на синтетическом корне, а не на дереве: дерево движется, и
// проба, портящая его, портила бы работу соседних сессий. Синтетика при этом
// воспроизводит ровно ту форму, которую разбор читает в настоящем корне —
// запись поверхности с полями `Handler`/`Reach`, присваивание мукса в
// переменную обработчика и регистрацию пути парой «пакет.константа».
package repohygiene_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// syntheticRoot собирает корень с двумя поверхностями — внешней и внутренней —
// и регистрирует названный путь на названном муксе.
func syntheticRoot(t *testing.T, mountOn string) string {
	t.Helper()
	dir := t.TempDir()
	src := `package main

import (
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kaname/internal/handler/clienttokenhttp"
	"github.com/PRO-Robotech/kaname/internal/handler/tokenintrospecthttp"
)

func run() {
	var externalHandler, internalHandler any
	mux := newMux()
	jwksMux := newMux()

	mux.Handle(clienttokenhttp.TokenPath, nil)
	jwksMux.Handle(tokenintrospecthttp.IntrospectPath, nil)

	externalHandler = mux
	internalHandler = jwksMux

	_ = servicecontract.Surface{
		Name:    "выдача токенов",
		Handler: externalHandler,
		Reach:   servicecontract.ReachExternal,
	}
	_ = servicecontract.Surface{
		Name:    "зеркало ключей",
		Handler: internalHandler,
		Reach:   servicecontract.ReachClusterInternal,
	}
}
`
	if mountOn != "mux" {
		src = replaceFirst(src, "\tmux.Handle(clienttokenhttp.TokenPath, nil)\n",
			"\t"+mountOn+".Handle(clienttokenhttp.TokenPath, nil)\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "serve.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("запись синтетического корня: %v", err)
	}
	return dir
}

func replaceFirst(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// reachOfClientToken — досягаемость, которую разбор приписал пути эндпоинта.
func reachOfClientToken(t *testing.T, dir string) (string, repohygiene.SurfaceCensus) {
	t.Helper()
	regs, census, findings, err := repohygiene.ScanSurfaceRegistrations(dir)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("на синтетике находок быть не должно: %v", findings)
	}
	got := repohygiene.RegistrationsOf(regs, "clienttokenhttp.TokenPath")
	if len(got) != 1 {
		t.Fatalf("регистраций пути %d, ожидалась одна", len(got))
	}
	return got[0].Reach, census
}

// TestSurfaceScannerSeesAnInternalMountAndIsSilentOnTheExternalOne — инъекция в
// ОБЕ стороны.
//
// Без второй половины разбор ловил бы форму: сканер, приписывающий всякой
// регистрации внутреннюю досягаемость, прошёл бы первую половину и был бы
// бесполезен.
func TestSurfaceScannerSeesAnInternalMountAndIsSilentOnTheExternalOne(t *testing.T) {
	// (а) законная посадка — путь на внешне досягаемой поверхности.
	reach, census := reachOfClientToken(t, syntheticRoot(t, "mux"))
	if reach != repohygiene.ExternalReach {
		t.Fatalf("на законной посадке разбор обязан прочитать %q, прочитал %q",
			repohygiene.ExternalReach, reach)
	}
	if census.Surfaces != 2 || census.Registrations != 2 {
		t.Fatalf("перепись синтетики: поверхностей %d, регистраций %d — ожидалось 2 и 2",
			census.Surfaces, census.Registrations)
	}

	// (б) возвращённый дефект — тот же путь на ВНУТРЕННЕЙ поверхности.
	reach, _ = reachOfClientToken(t, syntheticRoot(t, "jwksMux"))
	if reach == repohygiene.ExternalReach {
		t.Fatal("разбор не заметил, что путь эндпоинта уехал на внутреннюю поверхность")
	}
	if reach != "ReachClusterInternal" {
		t.Fatalf("разбор обязан НАЗВАТЬ досягаемость, которую нашёл; получено %q", reach)
	}
}

// TestSurfaceScannerReportsWhatItCouldNotLink — граница инструмента печатается
// числом, а не молчит.
//
// Регистрация пути литералом связать с объявленной константой нечем. Разбор
// обязан посчитать её НЕРАЗОБРАННОЙ, а не пропустить: пропущенная регистрация
// неотличима от отсутствующей, и гейт объявил бы «маршрутов нет» там, где они
// есть.
func TestSurfaceScannerReportsWhatItCouldNotLink(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func run() {
	mux := newMux()
	mux.Handle("/iam/v1/token", nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "serve.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("запись: %v", err)
	}
	regs, census, _, err := repohygiene.ScanSurfaceRegistrations(dir)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(regs) != 0 {
		t.Fatalf("литеральный путь не может быть связан с константой, получено %d записей", len(regs))
	}
	if census.Registrations != 1 || census.Unlinked != 1 {
		t.Fatalf("перепись обязана НАЗВАТЬ неразобранное: регистраций %d, неразобранных %d",
			census.Registrations, census.Unlinked)
	}
}
