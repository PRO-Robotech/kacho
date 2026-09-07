// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// generationcoverage_injection_test.go — доказательство способности упасть И
// смолчать.
//
// Инъекция здесь одно-фактная по построению: у каждой находки назван законный
// близнец, отличающийся ОДНИМ фактом — перечнем путей во входах либо тем, куда
// корень порождает. Дельта не объявляется, а вычисляется: корпус у пары общий,
// меняется ровно одна величина.
//
// Перепись проверяется отдельно и в обе стороны: объём осмотренного обязан
// РАВНЯТЬСЯ поданному корпусу на каждом случае (иначе «молчит» могло бы
// означать «ничего не прочитал») и обязан РАСТИ вместе с корпусом (иначе он
// константа, а не замер).
package repohygiene

import (
	"strings"
	"testing"
)

// generationCorpus — синтетическое дерево контрактов: два наших корня и один
// вендорный.
func generationCorpus() []generationContractFile {
	return []generationContractFile{
		{Rel: "kacho/cloud/x/v1/a.proto", Ours: true},
		{Rel: "kacho/cloud/y/v1/b.proto", Ours: true},
		{Rel: "kaname/cloud/iam/v1/c.proto", Ours: true},
		{Rel: "google/api/annotations.proto", Ours: false},
	}
}

// generationCorpusForeignSecondRoot — то же дерево, отличающееся ОДНИМ фактом:
// второй корень порождает наружу.
func generationCorpusForeignSecondRoot() []generationContractFile {
	files := generationCorpus()
	for i := range files {
		if generationRootOf(files[i].Rel) == "kaname" {
			files[i].Ours = false
		}
	}
	return files
}

func generationDeclWith(paths string) string {
	return "version: v2\nplugins:\n  - local: [go, run, example.com/protoc-gen-go]\n" +
		"    out: ../pkg/api\ninputs:\n  - directory: .\n" + paths
}

func TestGenerationCoverageDetectorSeesBothSides(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		files   []generationContractFile
		wantHit bool
		names   string // подстрока, которую находка обязана назвать
	}{
		{
			name:    "оба наших корня названы, вендорный — нет: законный близнец, молчит",
			yaml:    generationDeclWith("    paths:\n      - kacho\n      - kaname\n"),
			files:   generationCorpus(),
			wantHit: false,
		},
		{
			name:    "наш корень не назван — находка, называет корень",
			yaml:    generationDeclWith("    paths:\n      - kacho\n"),
			files:   generationCorpus(),
			wantHit: true,
			names:   "kaname",
		},
		{
			// Тот же перечень, что у находки выше. Различие ровно одно: корень
			// порождает НАРУЖУ. Без этой пары находка ловила бы «корень не в
			// перечне», а не «наш корень не порождается».
			name:    "тот же неназванный корень, но порождающий наружу — молчит",
			yaml:    generationDeclWith("    paths:\n      - kacho\n"),
			files:   generationCorpusForeignSecondRoot(),
			wantHit: false,
		},
		{
			name:    "`paths` отсутствует — входом становится весь модуль, молчит",
			yaml:    generationDeclWith(""),
			files:   generationCorpus(),
			wantHit: false,
		},
		{
			name: "путь во входах без предмета — находка (перечень не истёк сам)",
			yaml: generationDeclWith("    paths:\n      - kacho\n      - kaname\n" +
				"      - kanameold\n"),
			files:   generationCorpus(),
			wantHit: true,
			names:   "kanameold",
		},
		{
			name: "корень целиком исключён — находка",
			yaml: generationDeclWith("    paths:\n      - kacho\n      - kaname\n" +
				"    exclude_paths:\n      - kaname\n"),
			files:   generationCorpus(),
			wantHit: true,
			names:   "kaname",
		},
		{
			name: "исключению нечего исключать — находка",
			yaml: generationDeclWith("    paths:\n      - kacho\n      - kaname\n" +
				"    exclude_paths:\n      - kacho/cloud/zzz\n"),
			files:   generationCorpus(),
			wantHit: true,
			names:   "kacho/cloud/zzz",
		},
		{
			// Ради этого случая единица счёта — файл, а не корень: корень
			// НАЗВАН, и проверка по корням молчала бы, пока половина его
			// контрактов не порождает ничего.
			name:    "корень назван подкаталогом — часть его контрактов не покрыта, находка",
			yaml:    generationDeclWith("    paths:\n      - kacho/cloud/x\n      - kaname\n"),
			files:   generationCorpus(),
			wantHit: true,
			names:   "kacho",
		},
		{
			name: "вход вида, которого разбор не знает — находка, а не тишина",
			yaml: "version: v2\nplugins:\n  - local: [go, run, example.com/protoc-gen-go]\n" +
				"    out: ../pkg/api\ninputs:\n  - неведомый_вид: x\n    paths:\n      - kacho\n",
			files:   generationCorpus(),
			wantHit: true,
			names:   "не знает",
		},
		{
			name: "вход-модуль — разбор судит только каталог, находка",
			yaml: "version: v2\nplugins:\n  - local: [go, run, example.com/protoc-gen-go]\n" +
				"    out: ../pkg/api\ninputs:\n  - module: buf.build/acme/x\n",
			files:   generationCorpus(),
			wantHit: true,
			names:   "module",
		},
		{
			name: "каталог входа не есть корень модуля — находка",
			yaml: "version: v2\nplugins:\n  - local: [go, run, example.com/protoc-gen-go]\n" +
				"    out: ../pkg/api\ninputs:\n  - directory: ../другое\n",
			files:   generationCorpus(),
			wantHit: true,
			names:   "другое",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, census := checkGenerationCoverage("синтетика/buf.gen.yaml", tc.yaml, tc.files)
			if got := len(findings) > 0; got != tc.wantHit {
				t.Fatalf("ожидалась находка=%v, получено %v: %v", tc.wantHit, got, findings)
			}
			// Перепись обязана равняться поданному корпусу на КАЖДОМ случае:
			// иначе «молчит» неотличимо от «ничего не прочитал».
			if census.Files != len(tc.files) {
				t.Fatalf("осмотрено %d контрактов из %d поданных — вердикт вынесен "+
					"не обо всём корпусе", census.Files, len(tc.files))
			}
			if tc.names != "" && !strings.Contains(strings.Join(findings, "\n"), tc.names) {
				t.Fatalf("находка не называет %q — читатель пойдёт искать не там: %v",
					tc.names, findings)
			}
		})
	}
}

// TestGenerationCoverageRefusesAnEmptyCorpus — пустой обход ОТКАЗ, а не чистое
// дерево.
func TestGenerationCoverageRefusesAnEmptyCorpus(t *testing.T) {
	findings, census := checkGenerationCoverage("синтетика/buf.gen.yaml",
		generationDeclWith("    paths:\n      - kacho\n"), nil)
	if len(findings) == 0 {
		t.Fatal("на пустом корпусе проверка смолчала — «ноль находок» стало " +
			"неотличимо от «ноль прочитанного»")
	}
	if census.Files != 0 {
		t.Fatalf("перепись на пустом корпусе назвала %d контрактов", census.Files)
	}
}

// TestGenerationCoverageRefusesAnUnparsedDeclaration — неразобранное объявление
// не выдаётся за проверенное.
func TestGenerationCoverageRefusesAnUnparsedDeclaration(t *testing.T) {
	findings, _ := checkGenerationCoverage("синтетика/buf.gen.yaml",
		"inputs: [ это не YAML\n", generationCorpus())
	if len(findings) == 0 {
		t.Fatal("объявление не разобрано, а проверка смолчала — файл выдан за проверенный")
	}
}

// TestGenerationCoverageCensusGrowsWithTheCorpus — перепись есть замер, а не
// константа: проверка, печатающая одно и то же число при любом входе, о объёме
// осмотренного не утверждает ничего.
func TestGenerationCoverageCensusGrowsWithTheCorpus(t *testing.T) {
	decl := generationDeclWith("    paths:\n      - kacho\n      - kaname\n")

	small := generationCorpus()
	_, censusSmall := checkGenerationCoverage("синтетика/buf.gen.yaml", decl, small)

	large := append(generationCorpus(), []generationContractFile{
		{Rel: "kacho/cloud/z/v1/d.proto", Ours: true},
		{Rel: "kaname/cloud/iam/v1/e.proto", Ours: true},
	}...)
	_, censusLarge := checkGenerationCoverage("синтетика/buf.gen.yaml", decl, large)

	if censusLarge.Files <= censusSmall.Files {
		t.Fatalf("корпус вырос с %d до %d контрактов, а перепись осталась %d — "+
			"это не замер", len(small), len(large), censusLarge.Files)
	}
	if censusSmall.RootsOurs != 2 || censusSmall.Vendored != 1 {
		t.Fatalf("перепись по осям неверна: наших корней %d (ждали 2), вендорных "+
			"контрактов %d (ждали 1)", censusSmall.RootsOurs, censusSmall.Vendored)
	}
	if censusSmall.RootsCovered != 2 {
		t.Fatalf("покрытых наших корней %d, ждали 2", censusSmall.RootsCovered)
	}
}
