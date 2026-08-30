// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что ПАРА гейтов о разрешимости `Any` СПОСОБНА упасть — и
// что падает она на существе, а не на внешности формы.
//
// Инъекция идёт в обе стороны по каждой оси, потому что одного «краснеет» мало
// (гейт, краснеющий на всём, ничего не измеряет), и одного «молчит» тоже мало
// (молчание бывает от того, что читать не стали).
//
// # Ось ПОЛНОТЫ (auditProtoSurfaces)
//
//	владелец несёт proto-пакет, которого нет у края → находка, называющая пакет;
//	тот же пакет есть и у края                      → молчит;
//	владелец несёт НЕ-proto пакет сверх края        → молчит (предмет — реестр
//	                                                  типов, а не состав вообще).
//
// # Ось РАСПОЗНАВАНИЯ ФОРМ (collectAnyPackSites)
//
// По одной пробе на КАЖДУЮ форму из `anyPackFormNames`, плюс псевдоним импорта,
// плюс законные близнецы, на которых распознаватель обязан молчать. Форма, о
// которой распознаватель не знает, — не редкость и не край: всё записанное в ней
// оказывается вне наблюдения, то есть не даёт ни красного, ни зелёного.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/operationany"
)

// ─────────────────────────────────────────────────────────────────────────────
// Ось ПОЛНОТЫ
// ─────────────────────────────────────────────────────────────────────────────

func surface(cmd string, protoPkgs ...string) binaryProtoSurface {
	s := binaryProtoSurface{Command: cmd, Proto: map[string]bool{}, Links: map[string]bool{}}
	for _, p := range protoPkgs {
		s.Proto[p] = true
	}
	return s
}

// TestCompletenessGateFailsWhenTheEdgeCannotResolveWhatAnOwnerBuilds —
// воспроизведение НАСТОЯЩЕГО дефекта: владелец линкует `emptypb`, край — нет.
// Поверхности синтетические, но решение принимает ТА ЖЕ функция, что и гейт.
func TestCompletenessGateFailsWhenTheEdgeCannotResolveWhatAnOwnerBuilds(t *testing.T) {
	const wkt = "google.golang.org/protobuf/types/known/emptypb"
	const stub = "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc"

	edge := surface("gateway/cmd/api-gateway", stub)
	owner := surface("services/vpc/cmd/vpc", stub, wkt)

	findings := auditProtoSurfaces(edge, []binaryProtoSurface{owner})
	if len(findings) != 1 {
		t.Fatalf("дефект воспроизведён, а находок %d — гейт полноты не измеряет "+
			"того, ради чего заведён", len(findings))
	}
	if findings[0].Owner != owner.Command {
		t.Errorf("находка не называет владельца: %q", findings[0].Owner)
	}
	if len(findings[0].Missing) != 1 || findings[0].Missing[0] != wkt {
		t.Errorf("находка не называет КООРДИНАТУ недостающего пакета: %v", findings[0].Missing)
	}
}

// TestCompletenessGateIsSilentWhenTheEdgeLinksTheSamePackage — законный близнец:
// та же форма, но пакет у края есть. Без этой половины гейт был бы неотличим от
// предиката, отвечающего «нет» на что угодно.
func TestCompletenessGateIsSilentWhenTheEdgeLinksTheSamePackage(t *testing.T) {
	const wkt = "google.golang.org/protobuf/types/known/emptypb"
	edge := surface("gateway/cmd/api-gateway", wkt)
	owner := surface("services/vpc/cmd/vpc", wkt)

	if findings := auditProtoSurfaces(edge, []binaryProtoSurface{owner}); len(findings) != 0 {
		t.Fatalf("гейт краснеет на исправном дереве: %v", findings)
	}
}

// TestCompletenessGateIgnoresPackagesThatRegisterNothing — предмет гейта это
// РЕЕСТР ТИПОВ, а не состав импортов вообще. Владелец законно линкует сотни
// пакетов сверх края (замер по дереву: от 38 у registry до 254 у iam);
// требовать их все значило бы завести проверку, все находки которой ложны, —
// такую отключают первой, и вместе с ней перестают читать настоящую.
func TestCompletenessGateIgnoresPackagesThatRegisterNothing(t *testing.T) {
	pkg := goListPackage{ImportPath: "github.com/PRO-Robotech/kacho/pkg/outbox",
		GoFiles: []string{"outbox.go", "drainer.go"}}
	if pkg.registersProtoMessages() {
		t.Fatal("пакет без *.pb.go признан регистрирующим — гейт требовал бы от края " +
			"пакеты, к реестру типов отношения не имеющие")
	}
	gen := goListPackage{ImportPath: "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc",
		GoFiles: []string{"network.pb.go", "network_grpc.pb.go"}}
	if !gen.registersProtoMessages() {
		t.Fatal("сгенерированный пакет НЕ признан регистрирующим — гейт молчал бы на " +
			"собственном предмете")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось РАСПОЗНАВАНИЯ ФОРМ
// ─────────────────────────────────────────────────────────────────────────────

// writeSynthetic кладёт синтетический исходник во ВРЕМЕННЫЙ каталог и отдаёт
// корень с rel-именем. В живое дерево инъекция не пишет: вердикт гейта обязан
// быть свойством коммита, а не чужого рабочего каталога.
func writeSynthetic(t *testing.T, body string) (root string, rel string) {
	t.Helper()
	root = t.TempDir()
	rel = "services/synthetic/pack.go"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("подготовка синтетики: %v", err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
		t.Fatalf("запись синтетики: %v", err)
	}
	return root, rel
}

// synth собирает исходник вокруг одного места упаковки.
func synth(imports, call string) string {
	return "package synthetic\n\nimport (\n" + imports + "\n)\n\nfunc pack() {\n\t" + call + "\n}\n"
}

const importAnyEmpty = "\t\"google.golang.org/protobuf/types/known/anypb\"\n" +
	"\t\"google.golang.org/protobuf/types/known/emptypb\""

// TestRecognizerJudgesEveryDeclaredForm — по одной инъекции на КАЖДУЮ форму из
// `anyPackFormNames`, плюс псевдоним импорта и второй глагол упаковки.
// Перечень форм печатается переписью гейта; расхождение между этой таблицей и
// перечнем — находка, и она проверяется отдельной пробой ниже.
func TestRecognizerJudgesEveryDeclaredForm(t *testing.T) {
	cases := []struct {
		form    string
		imports string
		call    string
	}{
		{"anypb.New(&pkg.T{})", importAnyEmpty, `_, _ = anypb.New(&emptypb.Empty{})`},
		{"anypb.New(pkg.T{})", importAnyEmpty, `_, _ = anypb.New(emptypb.Empty{})`},
		{"anypb.New(new(pkg.T))", importAnyEmpty, `_, _ = anypb.New(new(emptypb.Empty))`},
		{"anypb.New((*pkg.T)(nil))", importAnyEmpty, `_, _ = anypb.New((*emptypb.Empty)(nil))`},
		{"anypb.MarshalFrom", importAnyEmpty, `_ = anypb.MarshalFrom(&anypb.Any{}, &emptypb.Empty{}, proto.MarshalOptions{})`},
		{"псевдоним импорта", "\t\"google.golang.org/protobuf/types/known/anypb\"\n" +
			"\tep \"google.golang.org/protobuf/types/known/emptypb\"",
			`_, _ = anypb.New(&ep.Empty{})`},
		{"псевдоним самого anypb", "\tapb \"google.golang.org/protobuf/types/known/anypb\"\n" +
			"\t\"google.golang.org/protobuf/types/known/emptypb\"",
			`_, _ = apb.New(&emptypb.Empty{})`},
	}
	for _, c := range cases {
		t.Run(c.form, func(t *testing.T) {
			root, rel := writeSynthetic(t, synth(c.imports, c.call))
			census := collectAnyPackSites(root, []string{rel})
			if census.CallsSeen != 1 {
				t.Fatalf("форма %s не опознана как упаковка вовсе: мест %d", c.form, census.CallsSeen)
			}
			if len(census.Written) != 1 {
				t.Fatalf("форма %s опознана, но тип НЕ прочитан: написанных %d, "+
					"неразрешённых %d — всё записанное так уходит вне наблюдения",
					c.form, len(census.Written), census.Unwritten)
			}
			got := census.Written[0]
			if got.Package != "google.golang.org/protobuf/types/known/emptypb" || got.Name != "Empty" {
				t.Fatalf("форма %s: тип прочитан неверно — %s.%s", c.form, got.Package, got.Name)
			}
			if got.Line == 0 || got.File != rel {
				t.Fatalf("форма %s: находка не называет координату — %s:%d", c.form, got.File, got.Line)
			}
		})
	}
}

// TestRecognizerFormTableMatchesTheCensus — таблица форм выше и перечень,
// который гейт ПЕЧАТАЕТ переписью, обязаны совпадать по числу. Иначе перепись
// объявляет объём, которого доказательство не покрывает: читатель увидит «судимых
// форм 5» и решит, что каждая проверена.
func TestRecognizerFormTableMatchesTheCensus(t *testing.T) {
	const provenForms = 5 // четыре формы записи типа + второй глагол упаковки
	if len(anyPackFormNames) != provenForms {
		t.Fatalf("перепись объявляет %d судимых форм, инъекцией покрыто %d — "+
			"объявленный объём шире доказанного", len(anyPackFormNames), provenForms)
	}
}

// TestRecognizerIsSilentOnLegalTwins — законные близнецы. Без них гейт ловил бы
// форму, а не существо, и первый же ложный срабат его отключил бы.
func TestRecognizerIsSilentOnLegalTwins(t *testing.T) {
	twins := []struct {
		name    string
		imports string
		body    string
		// wantCalls — сколько мест упаковки обязан насчитать распознаватель.
		wantCalls int
		// wantWritten — из них с прочитанным типом.
		wantWritten int
	}{
		{
			name:      "упоминание в КОММЕНТАРИИ",
			imports:   importAnyEmpty,
			body:      "\t// anypb.New(&emptypb.Empty{}) — так это пишется\n\t_ = emptypb.Empty{}\n\t_ = anypb.Any{}",
			wantCalls: 0,
		},
		{
			name:      "упоминание в СТРОКОВОМ ЛИТЕРАЛЕ",
			imports:   importAnyEmpty,
			body:      "\t_ = \"anypb.New(&emptypb.Empty{})\"\n\t_ = emptypb.Empty{}\n\t_ = anypb.Any{}",
			wantCalls: 0,
		},
		{
			name:      "одноимённый метод ЧУЖОГО пакета",
			imports:   "\tanypb \"google.golang.org/protobuf/types/known/anypb\"\n\t\"google.golang.org/protobuf/types/known/emptypb\"",
			body:      "\tvar other struct{ New func(any) }\n\t_ = other\n\t_ = anypb.Any{}\n\t_ = emptypb.Empty{}",
			wantCalls: 0,
		},
		{
			name:        "тип назван ПЕРЕМЕННОЙ — граница, а не находка",
			imports:     importAnyEmpty,
			body:        "\tm := &emptypb.Empty{}\n\t_, _ = anypb.New(m)",
			wantCalls:   1,
			wantWritten: 0,
		},
		{
			name:        "тип назван ВЫЗОВОМ — граница, а не находка",
			imports:     "\t\"google.golang.org/protobuf/types/known/anypb\"\n\t\"google.golang.org/protobuf/proto\"",
			body:        "\t_, _ = anypb.New(proto.Clone(nil))",
			wantCalls:   1,
			wantWritten: 0,
		},
	}
	for _, tw := range twins {
		t.Run(tw.name, func(t *testing.T) {
			root, rel := writeSynthetic(t,
				"package synthetic\n\nimport (\n"+tw.imports+"\n)\n\nfunc pack() {\n"+tw.body+"\n}\n")
			census := collectAnyPackSites(root, []string{rel})
			if census.FilesRead != 1 {
				t.Fatalf("файл не прочитан вовсе (%d) — молчание было бы от нечтения", census.FilesRead)
			}
			if census.CallsSeen != tw.wantCalls {
				t.Fatalf("мест упаковки %d, ожидалось %d", census.CallsSeen, tw.wantCalls)
			}
			if len(census.Written) != tw.wantWritten {
				t.Fatalf("прочитанных типов %d, ожидалось %d", len(census.Written), tw.wantWritten)
			}
		})
	}
}

// TestUnwrittenArgumentIsCountedNotSwallowed — ГРАНИЦА названа числом, а не
// умолчанием. Место, тип которого распознаватель прочесть не может, обязано
// попасть в перепись: «ноль находок» иначе неотличимо от «ноль прочитанного».
func TestUnwrittenArgumentIsCountedNotSwallowed(t *testing.T) {
	root, rel := writeSynthetic(t, synth(importAnyEmpty,
		"m := &emptypb.Empty{}\n\t_, _ = anypb.New(m)\n\t_, _ = anypb.New(&emptypb.Empty{})"))
	census := collectAnyPackSites(root, []string{rel})
	if census.CallsSeen != 2 {
		t.Fatalf("мест упаковки %d, ожидалось 2", census.CallsSeen)
	}
	if len(census.Written) != 1 || census.Unwritten != 1 {
		t.Fatalf("граница не отделена от предмета: написанных %d, неразрешённых %d",
			len(census.Written), census.Unwritten)
	}
}

// TestDeclarationIsReadByValuesNotByText — единый источник: обе координаты якоря
// (Go-имя и proto-адрес) выводятся из ОДНОГО значения, поэтому разойтись не
// могут. Проба утверждает именно это, а не наличие строки в файле.
func TestDeclarationIsReadByValuesNotByText(t *testing.T) {
	coords := operationany.AnchoredGoCoordinates()
	urls := operationany.AnchoredTypeURLs()
	if len(coords) == 0 || len(coords) != len(urls) {
		t.Fatalf("координат %d, адресов %d — словарь и перечень разошлись", len(coords), len(urls))
	}
	seen := map[string]bool{}
	for _, u := range urls {
		seen[u] = true
	}
	for _, c := range coords {
		if !seen[c.TypeURL] {
			t.Errorf("координата %s.%s даёт адрес %q, которого нет среди адресов якорей",
				c.Package, c.Name, c.TypeURL)
		}
		if !strings.Contains(c.Package, "/") {
			t.Errorf("координата %s.%s не называет ПУТЬ Go-пакета — сверять с местом "+
				"упаковки нечем", c.Package, c.Name)
		}
	}
}
