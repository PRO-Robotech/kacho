// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// latencyobserved_injection_test.go — ИНЪЕКЦИЯ: обе стороны гоняют ТУ ЖЕ
// функцию, что и гейт по дереву.
//
// Пар здесь две, а не одна, и вторая несущая.
//
// Первая пара — про измеритель: сервис, строящий сервер сам, БЕЗ измерителя
// краснеет, С измерителем молчит. Без неё гейт утверждал бы свойство, которого
// не проверяет.
//
// Вторая пара — про ГРАНИЦУ предмета: сервис, отдавший входящий путь носителю и
// не строящий измерителя сам, молчать ОБЯЗАН. Без неё гейт превратился бы в
// «все семеро заводят измеритель руками» — то есть потребовал бы повторить то,
// что за шестерых уже сделано построением, и первый же ложный срабат его бы
// отключил.

import (
	"strings"
	"testing"
)

// Сервис поднимает слушателя СВОИМ конструктором.
const directRaiserSrc = `package main

import "github.com/PRO-Robotech/kacho/pkg/grpcsrv"

func main() { _ = grpcsrv.NewServer() }
`

// Тот же сервис — плюс сборка измерителя задержки.
const latencyMeterSrc = `package main

import "github.com/PRO-Robotech/kacho/pkg/grpcsrv"

func meter() { _, _ = grpcsrv.NewServerLatency(nil) }
`

// Сервис отдал входящий путь носителю и своего сервера не строит.
const carrierRaiserSrc = `package main

import "github.com/PRO-Robotech/kacho/pkg/servicehost"

func main() { _ = servicehost.Serve(nil, nil) }
`

// Сторона дефекта: свой конструктор, измерителя нет — гейт называет сервис.
func TestLatencyGateRedOnADirectListenerWithoutAMeter(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": directRaiserSrc,
	})
	raisers, files, err := scanLatencyObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	r, ok := raisers["x"]
	if !ok || r.direct == "" {
		t.Fatalf("собственный подъём слушателя не распознан: %+v", raisers)
	}
	if r.measurer != "" {
		t.Fatalf("измеритель найден там, где его нет: %q", r.measurer)
	}
}

// Законный близнец той же формы: тот же собственный подъём — плюс файл,
// собирающий измеритель.
func TestLatencyGateSilentWhenTheDirectListenerBuildsAMeter(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go":    directRaiserSrc,
		"services/x/cmd/x/metrics.go": latencyMeterSrc,
	})
	raisers, files, err := scanLatencyObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if raisers["x"].measurer == "" {
		t.Fatal("гейт нашёл дефект в законном дереве: измеритель собран, а не засчитан")
	}
	if !strings.Contains(raisers["x"].measurer, "metrics.go") {
		t.Fatalf("координата измерителя не названа: %q", raisers["x"].measurer)
	}
}

// ГРАНИЦА ПРЕДМЕТА: сервис у носителя измерителя не строит и находкой НЕ
// является.
//
// Это не послабление: носитель заводит измеритель сам, и дескриптор без реестра
// не проходит конструктор (О13). Требовать здесь второй, ручной сборки значило
// бы объявить находкой исполнение правила.
func TestLatencyGateSilentOnACarrierServiceThatBuildsNoMeterItself(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": carrierRaiserSrc,
	})
	raisers, _, err := scanLatencyObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	r, ok := raisers["x"]
	if !ok || r.viaCarrier == "" {
		t.Fatalf("подъём через носитель не распознан: %+v", raisers)
	}
	if r.direct != "" {
		t.Fatalf("сервису у носителя приписан собственный конструктор: %q", r.direct)
	}
}

// Отрицательный контроль РАСПОЗНАВАНИЯ: обращение к пакету, не строящее ни
// сервера, ни измерителя, ни тем, ни другим не делает.
func TestLatencyGateIgnoresANonConstructorCall(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": directRaiserSrc,
		"services/x/cmd/x/other.go": `package main

import "github.com/PRO-Robotech/kacho/pkg/grpcsrv"

func limits() any { return grpcsrv.DefaultServerLimits() }
`,
	})
	raisers, _, err := scanLatencyObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if raisers["x"].measurer != "" {
		t.Fatalf("измерителем засчитано обращение к пакету без его сборки (%q) — тогда "+
			"измерителем станет всякий, кто возьмёт оттуда пределы", raisers["x"].measurer)
	}
}

// Отрицательный контроль ТЕКСТА: имя в комментарии измерителем не является.
//
// Имя встречается в комментариях, объясняющих ровно эту провязку, и текстовый
// поиск принял бы объяснение за исполнение — тот самый класс, ради которого
// гейт разбирает синтаксическое дерево.
func TestLatencyGateIgnoresTheNameInAComment(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": directRaiserSrc,
		"services/x/cmd/x/why.go": `package main

// Здесь надо бы позвать grpcsrv.NewServerLatency — но никто не позвал.
const why = "grpcsrv.NewServerLatency"
`,
	})
	raisers, _, err := scanLatencyObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if raisers["x"].measurer != "" {
		t.Fatalf("измерителем засчитано УПОМИНАНИЕ имени (%q): гейт читает текст, а не код",
			raisers["x"].measurer)
	}
}

// Отрицательный контроль ПСЕВДОНИМА: импорт под другим именем слепым пятном не
// становится.
func TestLatencyGateFollowsAnImportAlias(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": `package main

import srv "github.com/PRO-Robotech/kacho/pkg/grpcsrv"

func main() { _ = srv.NewServer() }

func meter() { _, _ = srv.NewServerLatency(nil) }
`,
	})
	raisers, _, err := scanLatencyObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if raisers["x"].direct == "" || raisers["x"].measurer == "" {
		t.Fatalf("псевдоним импорта сделал вызовы невидимыми: %+v", raisers["x"])
	}
}

// Поверхность, строящая слушателя БИБЛИОТЕЧНЫМ конструктором.
const libraryRaiserSrc = `package main

import "google.golang.org/grpc"

func main() { _ = grpc.NewServer() }
`

// TestLatencyGateSeesTheLibraryConstructorToo — библиотечный конструктор
// распознаётся наравне со своим.
//
// Проба заведена по факту промаха: первая редакция гейта знала только имя
// платформенного конструктора, читала каталог края обходом — и раисером его не
// считала, потому что край строит оба слушателя библиотечным. Перепись при этом
// печатала «семь из семи» и выглядела исчерпывающей.
func TestLatencyGateSeesTheLibraryConstructorToo(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"gateway/cmd/api-gateway/main.go": libraryRaiserSrc,
	})
	raisers, files, err := scanLatencyObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	r, ok := raisers["gateway"]
	if !ok || r.direct == "" {
		t.Fatalf("подъём слушателя библиотечным конструктором не распознан: %+v — "+
			"гейт объявил бы «все наблюдают» по неполному перечню поверхностей", raisers)
	}
	if r.measurer != "" {
		t.Fatalf("измеритель найден там, где его нет: %q", r.measurer)
	}
}

// TestLatencyGateSilentWhenTheLibraryRaiserBuildsAMeter — законный близнец.
func TestLatencyGateSilentWhenTheLibraryRaiserBuildsAMeter(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"gateway/cmd/api-gateway/main.go":    libraryRaiserSrc,
		"gateway/cmd/api-gateway/metrics.go": latencyMeterSrc,
	})
	raisers, _, err := scanLatencyObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if raisers["gateway"].measurer == "" {
		t.Fatal("гейт нашёл дефект в законном дереве: измеритель собран, а не засчитан")
	}
}

// TestLatencyGateRefusesATreeWithNoRootsAtAll — предпосылка обхода.
//
// Дерево, в котором нет НИ ОДНОГО корня со слушателями, — это не «находок ноль»,
// а «читать было нечего». Молчаливый успех здесь означал бы, что переехавший
// каталог выглядит как исправное дерево.
func TestLatencyGateRefusesATreeWithNoRootsAtAll(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"pkg/somewhere/else.go": "package somewhere\n",
	})
	if _, _, err := scanLatencyObservers(root); err == nil {
		t.Fatal("обход принял дерево без корней со слушателями: «ноль находок» стало " +
			"неотличимо от «ноль прочитанного»")
	}
}
