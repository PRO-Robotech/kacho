// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// docsgodependencydirectory_injection_test.go — доказательство способности гейта
// docsgodependencydirectory_test.go упасть и смолчать.
//
// Каждая ось проверяется В ОБЕ СТОРОНЫ. Законные близнецы взяты из дерева, а не
// сочинены: сочинённый близнец доказывает молчание на форме, которой в корпусе
// нет, и оставляет гейт вакуумным ровно там, где он должен работать.
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

type injGoDepCorpus map[string]string

func (c injGoDepCorpus) read(rel string) ([]byte, error) {
	body, ok := c[rel]
	if !ok {
		return nil, fmt.Errorf("нет такого документа")
	}
	return []byte(body), nil
}

func (c injGoDepCorpus) docs() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

// injGoDepTree — дерево, повторяющее СУЩЕСТВЕННУЮ раскладку настоящего:
// `proto/` без единого Go-файла, `pkg/api/...` со стабами, `internal/` с кодом.
func injGoDepTree() treeDirectories {
	return directoriesOf(map[string]bool{
		"proto/kacho/cloud/vpc/v1/network.proto":   true,
		"proto/buf.yaml":                           true,
		"pkg/api/kacho/cloud/vpc/v1/network.pb.go": true,
		"pkg/ids/ids.go":                           true,
		"internal/repohygiene/x.go":                true,
	})
}

func injGoDepScan(t *testing.T, c injGoDepCorpus) ([]goDepFinding, goDepCensus) {
	t.Helper()
	f, cs, err := scanGoDependencyClaims(c.docs(), c.read, injGoDepTree())
	if err != nil {
		t.Fatalf("обход синтетического корпуса: %v", err)
	}
	return f, cs
}

// ── сторона (а): дефект краснеет и называет координату ───────────────────────

// Настоящая форма дерева, слово в слово из services/vpc/docs/content/architecture.
func TestGoDepGate_RedsOnAContractDirectoryWithoutGoCode(t *testing.T) {
	corpus := injGoDepCorpus{
		"services/vpc/docs/content/architecture/overview.mdx": "" +
			"      <td>только stdlib + <code>proto/</code></td>\n",
	}
	findings, census := injGoDepScan(t, corpus)
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d (перепись: %s)", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"services/vpc/docs/content/architecture/overview.mdx", ":1", "proto/"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
	if census.withoutGo != 1 || census.withGoCode != 0 {
		t.Errorf("перепись не разделила каталоги с Go-кодом и без: %s", census)
	}
}

// Проза переносится по ширине: предписание и координата оказываются на разных
// строках. Настоящий экземпляр — известные расхождения nlb.
func TestGoDepGate_RedsWhenTheCoordinateIsOnTheNeighbouringLine(t *testing.T) {
	corpus := injGoDepCorpus{
		"a.md": "**Что.** `architecture.md` предписывает `domain/` импортировать ТОЛЬКО stdlib +\n" +
			"`proto/`. Фактически `internal/domain/` импортирует\n",
	}
	findings, census := injGoDepScan(t, corpus)
	if len(findings) != 1 {
		t.Fatalf("перенесённое по ширине предписание не поймано: находок %d (перепись: %s) "+
			"— однострочный предикат теряет ровно этот случай", len(findings), census)
	}
}

// ── сторона (б): законный близнец обязан молчать ─────────────────────────────

// Эталон дерева: services/vpc/docs/engineering/ARCHITECTURE.md — единственное
// место, где перечисление разрешённого названо верно.
func TestGoDepGate_SilentOnGeneratedStubsWhichDoHaveGoCode(t *testing.T) {
	corpus := injGoDepCorpus{
		"services/vpc/docs/engineering/ARCHITECTURE.md": "" +
			"2. Никаких импортов кроме stdlib и сгенерённых стабов `pkg/api/...` (если нужны enum-зеркала).\n",
	}
	findings, census := injGoDepScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("эталон дерева объявлен находкой: %v — гейт, краснеющий на верном тексте, "+
			"отключают первым (перепись: %s)", findings, census)
	}
	if census.withGoCode != 1 {
		t.Fatalf("близнец не дошёл до предиката: %s — молчание тогда означает «не прочитано», "+
			"а не «верно»", census)
	}
}

// Перечисление, называющее И верный источник, И каталог контрактов, — законно:
// читатель приземляется верно, а `.proto` действительно лежат в `proto/`. Это
// настоящая форма дерева после починки, а не сочинённая.
func TestGoDepGate_SilentWhenAGoDirectoryIsNamedBesideTheContractDirectory(t *testing.T) {
	corpus := injGoDepCorpus{
		"services/geo/docs/content/architecture/overview.mdx": "" +
			"зависит от домена, а домен не зависит ни от чего, кроме stdlib и сгенерённых стабов контракта\n" +
			"(`pkg/api/...`; сами `.proto` лежат в `proto/`).\n",
	}
	findings, census := injGoDepScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("перечисление, назвавшее верный источник, объявлено находкой: %v (перепись: %s) "+
			"— гейт, краснеющий на верном тексте, отключают первым", findings, census)
	}
	if census.withGoCode != 1 {
		t.Fatalf("близнец не дошёл до предиката: %s", census)
	}
}

// Зеркало предыдущего: соседство каталога БЕЗ Go-кода не спасает, если верного
// источника не названо ни одного. Без этой стороны правило «хотя бы один с
// Go-кодом» вырождалось бы во всеразрешение.
func TestGoDepGate_RedsWhenEveryNamedDirectoryLacksGoCode(t *testing.T) {
	corpus := injGoDepCorpus{
		"a.md": "домен не зависит ни от чего, кроме stdlib и контракта в `proto/`\n" +
			"(схемы — `proto/kacho/`).\n",
	}
	findings, census := injGoDepScan(t, corpus)
	if len(findings) == 0 {
		t.Fatalf("перечисление, не назвавшее ни одного каталога с Go-кодом, прошло: перепись %s", census)
	}
	if census.withoutGo != 1 {
		t.Fatalf("перепись не отнесла строку к находкам: %s", census)
	}
}

// Перечисление БЕЗ координаты законно и находкой быть не должно; перепись обязана
// отличать его от перечисления с координатой отдельным числом.
func TestGoDepGate_SilentWhenNoCoordinateIsNamedAtAll(t *testing.T) {
	corpus := injGoDepCorpus{
		"a.md": "Domain зависит только от stdlib; бизнес-логика — в use-case'ах.\n",
	}
	findings, census := injGoDepScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("перечисление без координаты объявлено находкой: %v", findings)
	}
	if census.markerLines != 1 || census.withCoord != 0 {
		t.Fatalf("перепись не отличила «нет координаты» от «есть»: %s", census)
	}
}

// Относительная координата внутри сервиса (`internal/domain`, каталога с таким
// путём в корне НЕТ) — законная форма и не предмет этого гейта. Резолв по самому
// длинному существующему префиксу зачёл бы её за `internal/` и молчал бы по
// неверной причине; здесь она пропускается явно.
func TestGoDepGate_SilentOnACoordinateThatDoesNotResolveFromTheRoot(t *testing.T) {
	corpus := injGoDepCorpus{
		"services/vpc/README.md": "- `internal/domain` — сущности и newtypes (только stdlib);\n",
	}
	findings, census := injGoDepScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("нерезолвящаяся координата объявлена находкой: %v", findings)
	}
	if census.withCoord != 0 {
		t.Fatalf("нерезолвящаяся координата зачтена как резолвящаяся: %s — тогда молчание "+
			"означало бы «каталог с Go-кодом», которого не существует", census)
	}
}

// Строка без маркера не судится вовсе: `proto/` сам по себе координатой ошибки
// не является — ошибочно лишь утверждение о зависимости от него.
func TestGoDepGate_SilentOnACoordinateWithoutTheDependencyClaim(t *testing.T) {
	corpus := injGoDepCorpus{
		"a.md": "Контракты домена лежат в `proto/kacho/cloud/vpc/v1/`.\n",
	}
	findings, census := injGoDepScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("координата без утверждения о зависимости объявлена находкой: %v", findings)
	}
	if census.markerLines != 0 {
		t.Fatalf("перепись насчитала маркер там, где его нет: %s", census)
	}
}

// ── предпосылка и перепись ───────────────────────────────────────────────────

func TestGoDepGate_ZeroMarkerLinesIsDistinguishableFromZeroFindings(t *testing.T) {
	corpus := injGoDepCorpus{"a.md": "страница без единого перечисления импортов\n"}
	findings, census := injGoDepScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("находки на чистом корпусе: %v", findings)
	}
	if census.markerLines != 0 {
		t.Fatalf("перепись насчитала маркер там, где его нет: %s", census)
	}
	// Ровно это гейт на дереве обязан читать как ОТКАЗ.
	if census.docs == 0 {
		t.Fatalf("перепись не назвала объём осмотренного: %s", census)
	}
}

func TestGoDepGate_UnreadableDocIsARefusalNotASilentZero(t *testing.T) {
	_, _, err := scanGoDependencyClaims([]string{"нет-такого.md"}, injGoDepCorpus{}.read, injGoDepTree())
	if err == nil {
		t.Fatal("непрочитанный документ прошёл молчаливым нулём — тогда сломанный обход " +
			"неотличим от чистого корпуса")
	}
}

// ── нормализация координаты ──────────────────────────────────────────────────

func TestGoDepGate_NormalizesTrailingWildcards(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"pkg/api/...", "pkg/api"},
		{"proto/", "proto"},
		{"pkg/api/*", "pkg/api"},
		{"pkg/api", "pkg/api"},
	} {
		if got := normalizeCoordinate(c.in); got != c.want {
			t.Errorf("normalizeCoordinate(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

// Текст находки режется по рунам: байтовый срез рвёт кириллицу пополам, и
// диагностика заканчивается заменяющим символом там, где нужен предмет.
func TestGoDepGate_FindingTextIsCutOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("зависимость ", 30)
	got := trimDocLineForFinding(long)
	if strings.ContainsRune(got, '�') {
		t.Fatalf("текст находки порвал руну: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("длинная строка не укорочена: %q", got)
	}
}
