// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package carrierprobe

// carrierprobe_test.go — предикат, страж и суждение об исходе подъёма доказаны
// на НАСТОЯЩЕМ производителе входа: каждое написание адреса предъявляется
// `net.Listen`, занятость порта берётся у ядра, а не рисуется строкой.
//
// Прежде совпадение предиката со слушателем стояло в комментарии словом
// «замерено», и счёт в нём разошёлся с перечнем (названо восемь написаний при
// семи). Здесь счёт не пишется — он выводится из таблицы, а совпадение
// исполняется.

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/servicecontract"
)

// listens — поднимается ли настоящий слушатель на адресе; слушатель сразу
// гасится. Адрес здесь всегда на петле либо эфемерный — фиксированных портов
// проба не занимает.
func listens(addr string) bool {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func TestKernelAssignedMatchesTheListenerOnNumericSpellings(t *testing.T) {
	type spelling struct {
		addr string
		want bool
		why  string
	}
	// Написания, на которых предикат СОВПАДАЕТ со слушателем: «да» ⇔ слушатель
	// поднимается на порту, который выбрало ядро.
	agree := []spelling{
		{"127.0.0.1:0", true, "каноническое написание"},
		{"127.0.0.1:00", true, "ядро смотрит на ЧИСЛО"},
		{"127.0.0.1:0000", true, "то же числом, другой записью"},
		{"127.0.0.1:+0", true, "слушатель принимает знак плюс"},
		{"127.0.0.1:-0", true, "и знак минус: область приёма шире канонической записи"},
		{"127.0.0.1:0x0", false, "шестнадцатеричную запись слушатель тоже отвергает"},
		{"127.0.0.1", false, "двоеточия нет: слушатель отказывает, «missing port»"},
	}
	for _, s := range agree {
		if got := KernelAssigned(s.addr); got != s.want {
			t.Errorf("KernelAssigned(%q) = %v, ждали %v — %s", s.addr, got, s.want, s.why)
		}
		// Совпадение ИСПОЛНЯЕТСЯ: на «да» слушатель обязан подняться, на
		// нечисловом «нет» — отказать.
		if s.want && !listens(s.addr) {
			t.Errorf("%q: предикат говорит «ядро назначит порт», а настоящий слушатель не поднялся — %s", s.addr, s.why)
		}
		if !s.want && listens(s.addr) {
			t.Errorf("%q: предикат говорит «нет», а настоящий слушатель поднялся — предикат уже слушателя там, "+
				"где обещал совпадение (%s)", s.addr, s.why)
		}
	}

	// Фиксированный порт — предмет запрета. Слушатель на нём поднимется или нет в
	// зависимости от машины; предикат отвечает «нет» всегда.
	for _, fixed := range []string{":9090", ":9091", "0.0.0.0:10", "[::1]:9090"} {
		if KernelAssigned(fixed) {
			t.Errorf("KernelAssigned(%q) = true на фиксированном порту", fixed)
		}
	}

	// РАСХОЖДЕНИЕ, намеренное, и потому утверждены ОБА факта: слушатель на
	// пустом порту поднимается (ядро выдаёт эфемерный), а предикат говорит
	// «нет» — пустое значение есть незаданная ручка.
	for _, empty := range []string{"127.0.0.1:", ""} {
		if !listens(empty) {
			t.Errorf("%q: слушатель на пустом порту не поднялся — расхождение, ради которого написано "+
				"исключение, больше не наблюдается; пересмотри шапку KernelAssigned", empty)
		}
		if KernelAssigned(empty) {
			t.Errorf("KernelAssigned(%q) = true: незаданная ручка принята за разрешение", empty)
		}
	}
	// ЗАВИСИМОЕ ОТ РАЗБОРЩИКА ПОРТА написание. Нечисловой порт `net.Listen`
	// отдаёт разборщику имён служб, и разборщиков два: собственный Go
	// (`CGO_ENABLED=0`, им собран каждый образ службы — `services/*/Dockerfile`,
	// `gateway/Dockerfile`) отказывает «unknown port», а системный (cgo, сборка
	// проб на машине с компилятором C) пропускает ведущий пробел и выдаёт
	// эфемерный порт. Совпадения со слушателем здесь обещать нельзя: на одном
	// и том же коммите оно есть под одним разборщиком и нет под другим.
	//
	// Обещание предиката на нём — только «нет», и это направление безопасно:
	// страж откажет «условие не создано», а не пропустит фиксированный порт.
	// Утверждено ОБА факта, на которых оно держится: предикат отвечает «нет», и
	// разборщик образа службы порт отвергает.
	resolverDependent := []string{"127.0.0.1: 0"}
	goResolver := &net.Resolver{PreferGo: true}
	for _, addr := range resolverDependent {
		if KernelAssigned(addr) {
			t.Errorf("KernelAssigned(%q) = true: нечисловое написание принято за порт ядра", addr)
		}
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("%q: SplitHostPort: %v — написание не дошло до разбора порта, проба не о том", addr, err)
		}
		if p, err := goResolver.LookupPort(context.Background(), "tcp", port); err == nil {
			t.Errorf("%q: разборщик Go принял порт %q как %d — предпосылка «образ службы его отвергает» "+
				"больше не держится; перенеси написание в таблицу совпадений", addr, port, p)
		}
		t.Logf("%q: слушатель на машине прогона поднялся=%v (зависит от разборщика), предикат «нет»",
			addr, listens(addr))
	}
	t.Logf("написаний: совпадающих со слушателем %d, фиксированных 4, намеренно расходящихся 2, "+
		"зависимых от разборщика %d", len(agree), len(resolverDependent))
}

// wideTwin — «широкая» сводка двух прежних копий: принимает и эндпойнт службы
// (`tcp://хост:порт`), и голый порт. Держится здесь как законный близнец пробы
// ниже: проба обязана отличать его от узкой формы, иначе она ничего не судит.
func wideTwin(v string) bool {
	if _, rest, ok := strings.Cut(v, "://"); ok {
		v = rest
	}
	if _, port, err := net.SplitHostPort(v); err == nil {
		v = port
	}
	n, err := strconv.Atoi(v)
	return err == nil && n == 0
}

// narrowFormViolations — входы, законные для ОДНОЙ из сторон и незаконные как
// адрес слушателя, на которых предикат ответил «да».
func narrowFormViolations(pred func(string) bool) []string {
	var out []string
	for _, in := range []struct{ value, side string }{
		// Законно для служб, задающих эндпойнт: это ВЕЛИЧИНА КОНФИГУРАЦИИ, а не
		// адрес — процесс собирает из неё `127.0.0.1:0`.
		{"tcp://127.0.0.1:0", "эндпойнт службы, задающей слушатель эндпойнтом"},
		// Законно для служб, задающих порт: величина конфигурации, из которой
		// процесс собирает `:0`.
		{"0", "голый порт службы, задающей слушатель портом"},
		// Эндпойнт, поданный в ручку порта: процесс склеит `":" + значение`, и
		// слушатель не поднимется вовсе.
		{":127.0.0.1:0", "эндпойнт, склеенный в адрес службой, задающей порт"},
		{":tcp://127.0.0.1:0", "то же со схемой"},
	} {
		if pred(in.value) {
			out = append(out, fmt.Sprintf("%q (%s)", in.value, in.side))
		}
	}
	return out
}

// TestKernelAssignedAnswersByTheNarrowForm — узкая семантика закреплена пробой,
// а не комментарием (kacho#2749, п. 4 предиката снятия): на входе, законном для
// одной стороны и незаконном как адрес слушателя, общий предикат отвечает «нет».
// Близнец широкой формы на тех же входах обязан быть пойман — иначе проба
// зеленела бы и на сводке, которая взяла широкую семантику.
func TestKernelAssignedAnswersByTheNarrowForm(t *testing.T) {
	if v := narrowFormViolations(KernelAssigned); len(v) != 0 {
		t.Errorf("общий предикат принял вход, который не является адресом слушателя: %s", strings.Join(v, "; "))
	}
	if v := narrowFormViolations(wideTwin); len(v) == 0 {
		t.Fatal("проба узкой формы не отличила широкую сводку — она не судит ничего")
	}
	for _, s := range []string{"tcp://127.0.0.1:0", "0", ":127.0.0.1:0"} {
		if listens(s) {
			t.Errorf("%q: настоящий слушатель поднялся на входе, который проба считает незаконным адресом", s)
		}
	}
}

// fataler — подставной исполнитель стража: записывает отказ вместо остановки.
type fataler struct{ msgs []string }

func (f *fataler) Helper() {}
func (f *fataler) Fatalf(format string, args ...any) {
	f.msgs = append(f.msgs, fmt.Sprintf(format, args...))
}

// Способность стража упасть — инъекцией: фиксированный порт краснит его и
// называет ручку, эфемерный — молчит.
func TestRequireKernelAssignedRefusesAFixedPortAndNamesTheKnob(t *testing.T) {
	t.Run("инъекция: внутренний слушатель на умолчании", func(t *testing.T) {
		f := &fataler{}
		RequireKernelAssigned(f, servicecontract.Spec{PublicAddr: "127.0.0.1:0", InternalAddr: ":9091"},
			"KACHO_PROBE_GRPC_PORT", "KACHO_PROBE_INTERNAL_PORT")
		if len(f.msgs) != 1 {
			t.Fatalf("страж не отказал на фиксированном порту: %q", f.msgs)
		}
		for _, want := range []string{"УСЛОВИЕ НЕ СОЗДАНО", "KACHO_PROBE_INTERNAL_PORT", `":9091"`} {
			if !strings.Contains(f.msgs[0], want) {
				t.Errorf("отказ не называет %q:\n%s", want, f.msgs[0])
			}
		}
		if strings.Contains(f.msgs[0], "KACHO_PROBE_GRPC_PORT") {
			t.Errorf("отказ назвал исправную ручку:\n%s", f.msgs[0])
		}
	})
	t.Run("законный близнец: оба эфемерны", func(t *testing.T) {
		f := &fataler{}
		RequireKernelAssigned(f, servicecontract.Spec{PublicAddr: ":0", InternalAddr: "127.0.0.1:0"}, "a", "b")
		if len(f.msgs) != 0 {
			t.Fatalf("страж отказал исправной посадке: %q", f.msgs)
		}
	})
}

// Исход подъёма: занятость слушателя — третья категория, отказ носителя —
// красное. Занятость берётся у ЯДРА: второй слушатель на порту первого, и
// ошибка завёрнута так же, как её заворачивает носитель.
func TestClassifyTellsAnOccupiedListenerFromACarrierRefusal(t *testing.T) {
	occupant, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("предпосылка не создана: эфемерный слушатель не поднялся: %v", err)
	}
	defer occupant.Close()
	_, bindErr := net.Listen("tcp", occupant.Addr().String())
	if bindErr == nil {
		t.Fatal("предпосылка не создана: второй слушатель поднялся на занятом порту")
	}
	occupied := fmt.Errorf("servicehost: публичный слушатель %s: %w", occupant.Addr(), bindErr)

	for _, tc := range []struct {
		name string
		err  error
		want Outcome
		text string
	}{
		{"подъём состоялся", nil, Raised, ""},
		{"порт занят соседом", occupied, NotCreated, "УСЛОВИЕ НЕ СОЗДАНО"},
		{"носитель отказал", fmt.Errorf("servicehost: kacho-probe не поднимается — служимый набор RPC пуст"), Refused, "ОТКАЗАЛ"},
		{"прочая ошибка", fmt.Errorf("servicehost: сервер упал"), Failed, "ошибку подъёма"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != tc.want {
				t.Fatalf("Classify = %v, ждали %v", got, tc.want)
			}
			v := Verdict("kacho-probe", tc.err)
			if tc.text == "" && v != "" || tc.text != "" && !strings.Contains(v, tc.text) {
				t.Fatalf("текст исхода %q не называет %q", v, tc.text)
			}
			f := &fataler{}
			RequireRaised(f, "kacho-probe", tc.err)
			if (tc.want == Raised) != (len(f.msgs) == 0) {
				t.Fatalf("RequireRaised разошёлся с исходом %v: %q", tc.want, f.msgs)
			}
		})
	}
}
