// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// foundationprosecoordinate_injection_test.go — доказательство того, что гейт
// СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Корпус синтетический и подаётся чистой функции значениями: инъекция не пишет в
// живое дерево, поэтому не может уронить соседнюю ось и не может быть спутана с
// настоящей находкой. Каждая проба меняет ОДИН факт против своего положительного
// близнеца.

const proseInjPlatform = "example.test/owner/platform"

// proseInjMoving — переезжающие каталоги синтетического корпуса. `pkg/errorsx` рядом с
// `pkg/errors` стоит намеренно: без проверки границы сегмента распознаватель
// прочитал бы одно как другое.
var proseInjMoving = map[string]bool{
	"pkg/errors":  true,
	"pkg/errorsx": true,
	"pkg/outbox":  true,
}

func proseInjJudge(t *testing.T, contents map[string]string) ([]foundationProseFinding, []string, foundationProseCensus) {
	t.Helper()
	return judgeFoundationProseCoordinatesAgainst(proseInjPlatform, proseInjMoving, contents, nil)
}

// TestFoundationProseInjection_ControlIsSilent — положительный контроль: корпус,
// в котором координаты названы только законными формами, находок не даёт.
//
// Без него отрицание зеленело бы на любом сломанном распознавателе.
func TestFoundationProseInjection_ControlIsSilent(t *testing.T) {
	t.Parallel()
	findings, stale, census := proseInjJudge(t, map[string]string{
		// близнец 1: путь В КАВЫЧКАХ — его переписывает производитель
		"pkg/outbox/emit.go": "import (\n\tkerr \"" + proseInjPlatform + "/pkg/errors\"\n)\n",
		// близнец 1 же, но литералом вне блока импорта
		"pkg/outbox/name.go": "const p = \"" + proseInjPlatform + "/pkg/errors\"\n",
		// близнец 2: ГОЛЫЙ путь модуля платформы
		"pkg/outbox/doc.go": "// `go mod tidy` дописывал бы require " + proseInjPlatform + " псевдоверсией\n",
		// близнец 3: пакет, который НЕ переезжает
		"pkg/errors/doc.go": "// см. " + proseInjPlatform + "/pkg/api/kacho/cloud/compute/v1\n",
	})
	if len(findings) != 0 {
		t.Fatalf("законные формы объявлены находками: %v", findings)
	}
	if len(stale) != 0 {
		t.Fatalf("пустая ведомость не может устареть: %v", stale)
	}
	if census.Quoted != 2 || census.Surviving != 2 {
		t.Fatalf("перепись не отличила близнецов: %s", census)
	}
}

// TestFoundationProseInjection_ProseCoordinateIsAFinding — инъекция НОВОГО
// свойства: та же координата, названная не импортом.
//
// Один факт против контроля выше: кавычки сняты.
func TestFoundationProseInjection_ProseCoordinateIsAFinding(t *testing.T) {
	t.Parallel()
	forms := map[string]string{
		"ссылка godoc": "pkg/outbox/a.go|// решает [" + proseInjPlatform + "/pkg/errors.Reason] — там",
		"проза":        "pkg/outbox/b.go|//   - Writer-side: пакет " + proseInjPlatform + "/pkg/errors",
		"код-span":     "pkg/outbox/c.go|// берётся у полосы `" + proseInjPlatform + "/pkg/errors`.Reason",
		"конец строки": "pkg/outbox/d.go|// см. " + proseInjPlatform + "/pkg/outbox",
	}
	for name, spec := range forms {
		t.Run(name, func(t *testing.T) {
			parts := strings.SplitN(spec, "|", 2)
			findings, _, census := proseInjJudge(t, map[string]string{parts[0]: parts[1] + "\n"})
			if len(findings) != 1 {
				t.Fatalf("форма %q не опознана: находок %d, %s", name, len(findings), census)
			}
			if findings[0].File != parts[0] || findings[0].Line != 1 {
				t.Fatalf("находка обязана называть файл и строку; получено %s:%d",
					findings[0].File, findings[0].Line)
			}
			if !strings.Contains(findings[0].String(), proseInjPlatform+"/pkg/") {
				t.Fatalf("находка обязана называть путь: %s", findings[0])
			}
		})
	}
}

// TestFoundationProseInjection_SegmentBoundaryIsRespected — `pkg/errorsx` не
// читается как `pkg/errors`, и наоборот: путь длиннее выигрывает.
func TestFoundationProseInjection_SegmentBoundaryIsRespected(t *testing.T) {
	t.Parallel()
	findings, _, _ := proseInjJudge(t, map[string]string{
		"pkg/outbox/a.go": "// см. " + proseInjPlatform + "/pkg/errorsx\n",
	})
	if len(findings) != 1 || findings[0].Named != proseInjPlatform+"/pkg/errorsx" {
		t.Fatalf("длинный путь обязан выиграть у своей приставки; получено %v", findings)
	}
}

// TestFoundationProseInjection_EmptyWalkIsAFinding — пустой обход есть отказ, а
// не чистота: гейт, не прочитавший ни одного файла, о дереве не высказался.
func TestFoundationProseInjection_EmptyWalkIsAFinding(t *testing.T) {
	t.Parallel()
	for name, call := range map[string]func() []foundationProseFinding{
		"ноль файлов": func() []foundationProseFinding {
			f, _ := judgeFoundationProseCoordinates(proseInjPlatform, proseInjMoving, nil)
			return f
		},
		"ноль переезжающих каталогов": func() []foundationProseFinding {
			f, _ := judgeFoundationProseCoordinates(proseInjPlatform, nil,
				map[string]string{"pkg/a/a.go": "package a\n"})
			return f
		},
		"модуль платформы не выведен": func() []foundationProseFinding {
			f, _ := judgeFoundationProseCoordinates("", proseInjMoving,
				map[string]string{"pkg/a/a.go": "package a\n"})
			return f
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := call(); len(got) == 0 {
				t.Fatal("беспредметный вердикт объявлен чистотой")
			}
		})
	}
}

// TestFoundationProseInjection_LedgerExpiresByItself — запись ведомости, которой
// больше нечего прощать, обязана краснеть сама.
//
// Это и есть предикат снятия для восьми запечённых дескрипторов: в день, когда
// стаб перегенерирован, гейт требует снять запись тем же изменением.
func TestFoundationProseInjection_LedgerExpiresByItself(t *testing.T) {
	t.Parallel()

	clean := map[string]string{"pkg/outbox/x.pb.go": "// нет координат\n"}
	_, stale, _ := judgeFoundationProseCoordinatesAgainst(proseInjPlatform, proseInjMoving, clean,
		[]knownBakedDescriptor{{"pkg/outbox/x.pb.go", 1, "регенерация"}})
	if len(stale) != 1 || !strings.Contains(stale[0], "нечего прощать") {
		t.Fatalf("запись без предмета обязана краснеть; получено %v", stale)
	}

	// Положительный близнец: у той же записи предмет ЕСТЬ — она молчит и гасит
	// находку. Без этой стороны «краснеет всегда» было бы неотличимо от работы.
	dirty := map[string]string{"pkg/outbox/x.pb.go": "// " + proseInjPlatform + "/pkg/errors\n"}
	f2, stale2, census2 := judgeFoundationProseCoordinatesAgainst(proseInjPlatform, proseInjMoving, dirty,
		[]knownBakedDescriptor{{"pkg/outbox/x.pb.go", 1, "регенерация"}})
	if len(f2) != 0 || len(stale2) != 0 || census2.Excused != 1 {
		t.Fatalf("запись с живым предметом обязана прощать ровно своё; %v / %v / %s", f2, stale2, census2)
	}
}

// TestFoundationProseInjection_LedgerIsClosedToProse — ведомость прощает только
// порождённый стаб. Координата в прозе правится текстом за секунду, и запись о
// ней была бы послаблением без предмета.
func TestFoundationProseInjection_LedgerIsClosedToProse(t *testing.T) {
	t.Parallel()
	dirty := map[string]string{"pkg/outbox/doc.go": "// " + proseInjPlatform + "/pkg/errors\n"}

	f, stale, _ := judgeFoundationProseCoordinatesAgainst(proseInjPlatform, proseInjMoving, dirty,
		[]knownBakedDescriptor{{"pkg/outbox/doc.go", 1, "хочу тишины"}})
	if len(f) != 1 {
		t.Fatalf("прозу ведомость прощать не вправе; находок %d", len(f))
	}
	if len(stale) != 1 || !strings.Contains(stale[0], "ведомость закрыта") {
		t.Fatalf("попытка простить прозу обязана быть названа; получено %v", stale)
	}

	// Второй факт того же рода: запись без обоснования.
	_, stale2, _ := judgeFoundationProseCoordinatesAgainst(proseInjPlatform, proseInjMoving, dirty,
		[]knownBakedDescriptor{{"pkg/outbox/x.pb.go", 1, ""}})
	if len(stale2) == 0 || !strings.Contains(strings.Join(stale2, " "), "без предмета") {
		t.Fatalf("запись без обоснования обязана быть названа; получено %v", stale2)
	}
}
