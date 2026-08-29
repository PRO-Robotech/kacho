// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// docsformerreponames_injection_test.go — доказательство способности гейта
// docsformerreponames_test.go упасть и смолчать.
//
// Гейт утверждает свойство прозы, а такой гейт легче всего сделать вакуумным:
// достаточно, чтобы распознаватель перестал видеть предмет. Поэтому каждая ось
// проверяется В ОБЕ СТОРОНЫ, и отдельно проверено, что документ не закрывает
// предикат СОБСТВЕННЫМ дефектом.
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

type injFormerCorpus map[string]string

func (c injFormerCorpus) read(rel string) ([]byte, error) {
	body, ok := c[rel]
	if !ok {
		return nil, fmt.Errorf("нет такого документа")
	}
	return []byte(body), nil
}

func (c injFormerCorpus) docs() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

func injFormerScan(t *testing.T, c injFormerCorpus) ([]formerRepoFinding, formerRepoCensus) {
	t.Helper()
	f, cs, err := scanFormerRepoNames(c.docs(), c.read)
	if err != nil {
		t.Fatalf("обход синтетического корпуса: %v", err)
	}
	return f, cs
}

// ── сторона (а): дефект краснеет и называет координату ───────────────────────

func TestFormerRepoGate_RedsOnAPresentTenseCodeHome(t *testing.T) {
	corpus := injFormerCorpus{
		"services/geo/docs/content/intro.mdx": "Источник истины — Protocol Buffers в `kacho-proto`.\n",
	}
	findings, census := injFormerScan(t, corpus)
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d (перепись: %s)", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"services/geo/docs/content/intro.mdx", "kacho-proto", ":1"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

func TestFormerRepoGate_RedsOnCorelibToo(t *testing.T) {
	corpus := injFormerCorpus{
		"a.md": "Переиспользуемое приходит из `kacho-corelib` — не дублируется.\n",
	}
	findings, _ := injFormerScan(t, corpus)
	if len(findings) != 1 || findings[0].name != "kacho-corelib" {
		t.Fatalf("второе имя не распознано: %v", findings)
	}
}

// ── сторона (б): законный близнец обязан молчать ─────────────────────────────

// Эталон, оставленный намеренно при закрытии #1448: прежнее имя рядом с
// нынешней координатой. Гейт не судит время глагола — он судит соседство.
func TestFormerRepoGate_SilentWhenTheCurrentCoordinateStandsBeside(t *testing.T) {
	corpus := injFormerCorpus{
		"services/vpc/docs/engineering/architecture/README.md": "" +
			"- `pkg/` монорепо — `ids`, `operations`, … (прежде отдельный репозиторий `kacho-corelib`).\n" +
			"- `proto/` + сгенерённые стабы `pkg/api/...` (прежде `kacho-proto`).\n",
	}
	findings, census := injFormerScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v — гейт, краснеющий на верном "+
			"тексте, отключают первым (перепись: %s)", findings, census)
	}
	if census.sameLine["kacho-corelib"] != 1 || census.sameLine["kacho-proto"] != 1 {
		t.Fatalf("близнец не дошёл до предиката: %s — молчание тогда означает "+
			"«не прочитано», а не «верно»", census)
	}
}

// Проза переносится по ширине, поэтому координата бывает на соседней строке.
func TestFormerRepoGate_SilentWhenTheCoordinateIsOnTheNeighbouringLine(t *testing.T) {
	corpus := injFormerCorpus{
		"a.md": "Общие пакеты лежат в `pkg/` монорепо\n(прежде отдельный репозиторий `kacho-corelib`).\n",
	}
	findings, census := injFormerScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("координата на соседней строке не зачтена: %v", findings)
	}
	if census.neighbour["kacho-corelib"] != 1 {
		t.Fatalf("перепись не отличила соседнюю строку от своей: %s", census)
	}
}

// ── документ не закрывает предикат СОБСТВЕННЫМ дефектом ──────────────────────

// `proto/` внутри `kacho-proto/...` — часть самого прежнего имени. Засчитать её
// за нынешнюю координату значило бы дать дефекту оправдать себя.
func TestFormerRepoGate_DoesNotAcceptTheDefectAsItsOwnJustification(t *testing.T) {
	corpus := injFormerCorpus{
		"a.md": "Тексты сверены с proto (kacho-proto/.../compute/v1) и кодом.\n",
	}
	findings, census := injFormerScan(t, corpus)
	if len(findings) != 1 {
		t.Fatalf("вхождение закрыло предикат собственным дефектом: находок %d, перепись %s "+
			"— `proto/` внутри `kacho-proto/` координатой не является", len(findings), census)
	}
}

// Зеркало предыдущего: настоящая координата в той же форме обязана зачитываться.
func TestFormerRepoGate_AcceptsARealPathNextToTheFormerName(t *testing.T) {
	corpus := injFormerCorpus{
		"a.md": "Контракты лежат в proto/kacho/cloud/compute/v1 (прежде `kacho-proto`).\n",
	}
	findings, census := injFormerScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("настоящая координата не зачтена: %v (перепись: %s)", findings, census)
	}
}

// ── предпосылка: ноль вхождений — «не прочитано», а не «чисто» ───────────────

func TestFormerRepoGate_ZeroOccurrencesIsDistinguishableFromZeroFindings(t *testing.T) {
	corpus := injFormerCorpus{"a.md": "страница без единого прежнего имени\n"}
	findings, census := injFormerScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("находки на чистом корпусе: %v", findings)
	}
	total := census.hits["kacho-proto"] + census.hits["kacho-corelib"]
	if total != 0 {
		t.Fatalf("перепись насчитала вхождения там, где их нет: %s", census)
	}
	// Ровно это гейт на дереве обязан читать как ОТКАЗ.
	if census.docs == 0 || census.lines == 0 {
		t.Fatalf("перепись не назвала объём осмотренного: %s", census)
	}
}

// Имя сервиса прежним именем репозитория не является и находкой быть не должно.
func TestFormerRepoGate_DoesNotAccuseServiceNames(t *testing.T) {
	corpus := injFormerCorpus{
		"a.md": "Ребро `kacho-vpc` → `kacho-geo`; SPIFFE ns/kacho-api-gateway.\n",
	}
	findings, _ := injFormerScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("имя сервиса объявлено находкой: %v — слепая правка таких имён сломала бы "+
			"документированный круг доверенных отправителей", findings)
	}
}
