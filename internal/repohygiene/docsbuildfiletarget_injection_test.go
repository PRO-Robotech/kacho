// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// docsbuildfiletarget_injection_test.go — доказательство способности гейта
// docsbuildfiletarget_test.go упасть и смолчать.
//
// Гейт утверждает ОТСУТСТВИЕ («такой команды в дереве нет»), а такой гейт
// зеленеет легче всех: он молчит и когда предмета нет, и когда сломан разбор.
// Поэтому каждая ось проверяется В ОБЕ СТОРОНЫ — дефект обязан назваться
// координатой, законный близнец той же формы обязан молчать.
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

// injBuildCorpus — синтетический корпус: путь документа → его содержимое.
type injBuildCorpus map[string]string

func (c injBuildCorpus) read(rel string) ([]byte, error) {
	body, ok := c[rel]
	if !ok {
		return nil, fmt.Errorf("нет такого документа")
	}
	return []byte(body), nil
}

func (c injBuildCorpus) docs() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

// injBuildFiles — множество «файлов индекса» для синтетического дерева.
var injBuildFiles = map[string]bool{
	"services/vpc/Dockerfile":    true,
	"services/geo/Dockerfile":    true,
	"ui-future/host/Dockerfile":  true,
	"services/vpc/docs/page.mdx": true,
	"services/vpc/docs/page.md":  true,
	"ui-future/deploy/README.md": true,
}

func injBuildScan(t *testing.T, corpus injBuildCorpus) ([]docBuildFinding, docBuildCensus) {
	t.Helper()
	f, c, err := scanDocBuildFileTargets(corpus.docs(), injBuildFiles, corpus.read)
	if err != nil {
		t.Fatalf("обход синтетического корпуса: %v", err)
	}
	return f, c
}

// mdxRegion оборачивает строки в тот самый MDX-регион, который на живых
// страницах и нёс дефект: <CodeBlock>{dedent`…`}. Регион отступлен ПО
// ПОСТРОЕНИЮ — сопоставитель, привязанный к нулевой колонке, его не увидит.
func mdxRegion(lines ...string) string {
	var b strings.Builder
	b.WriteString("проза страницы\n\n<CodeBlock language=\"bash\">\n  {dedent`\n")
	for _, l := range lines {
		b.WriteString("    " + l + "\n")
	}
	b.WriteString("  `}\n</CodeBlock>\n")
	return b.String()
}

// ── сторона (а): дефект обязан краснеть и называть координату ────────────────

func TestDocsBuildGate_RedsOnASiblingRepositoryPath(t *testing.T) {
	corpus := injBuildCorpus{
		"services/vpc/docs/page.mdx": mdxRegion(
			"docker build -f kacho-vpc/Dockerfile -t kacho-vpc:dev .",
		),
	}
	findings, census := injBuildScan(t, corpus)

	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d (перепись: %s) — гейт, "+
			"не краснеющий на настоящем дефекте из дерева, ничего не стережёт",
			len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"services/vpc/docs/page.mdx", "kacho-vpc/Dockerfile", ":"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s — находка без координаты посылает "+
				"читателя искать не там", want, got)
		}
	}
	if findings[0].line == 0 {
		t.Errorf("находка не называет строку: %s", got)
	}
	if census.withFlag != 1 {
		t.Errorf("перепись не засчитала цитату с флагом файла: %s", census)
	}
}

// ── сторона (б): законный близнец ТОЙ ЖЕ формы обязан молчать ────────────────

func TestDocsBuildGate_SilentOnThePathTheTreeActuallyHas(t *testing.T) {
	corpus := injBuildCorpus{
		"services/vpc/docs/page.mdx": mdxRegion(
			"docker build -f services/vpc/Dockerfile -t kacho-vpc:dev .",
		),
	}
	findings, census := injBuildScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v — гейт, краснеющий на верном "+
			"коде, отключают первым (перепись: %s)", findings, census)
	}
	if census.withFlag != 1 {
		t.Fatalf("близнец не дошёл до предиката: %s — молчание тогда означает "+
			"«не прочитано», а не «верно»", census)
	}
}

// Путь, разрешимый ОТ КАТАЛОГА ДОКУМЕНТА, а не от корня: страница
// ui-future/deploy/README.md пишет `-f host/Dockerfile`, и это верно из
// ui-future/. Предпосылка гейта намеренно слабая.
func TestDocsBuildGate_SilentOnAPathResolvedFromAnAncestorDirectory(t *testing.T) {
	corpus := injBuildCorpus{
		"ui-future/deploy/README.md": "```sh\ndocker build -f host/Dockerfile -t x:dev .\n```\n",
	}
	findings, census := injBuildScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("путь, разрешимый от предка каталога документа, объявлен находкой: %v "+
			"(перепись: %s)", findings, census)
	}
	if census.withFlag != 1 {
		t.Fatalf("цитата не дошла до предиката: %s", census)
	}
}

// ── границы распознавателя: что находкой НЕ является ─────────────────────────

func TestDocsBuildGate_TreatsPlaceholdersAsSamplesNotFindings(t *testing.T) {
	corpus := injBuildCorpus{
		"services/vpc/docs/page.mdx": mdxRegion(
			"docker build -f $DOCKERFILE -t x .",
			"docker build -f <repo>/Dockerfile -t x .",
		),
	}
	findings, census := injBuildScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("запись-образец объявлена находкой: %v — резолвить в ней нечего, "+
			"и обвинение здесь было бы на пустом месте", findings)
	}
	if census.notation != 2 {
		t.Errorf("образцы не попали в перепись (%s) — непосчитанный образец "+
			"неотличим от непрочитанной строки", census)
	}
}

func TestDocsBuildGate_CountsBuildWithoutTheFileFlagButDoesNotAccuseIt(t *testing.T) {
	corpus := injBuildCorpus{
		"services/vpc/docs/page.md": "```sh\ndocker build -t kaname:dev .\n```\n",
	}
	findings, census := injBuildScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("сборка без флага файла объявлена находкой: %v — файл берётся из "+
			"контекста, называть нечего", findings)
	}
	if census.citations != 1 || census.withFlag != 0 {
		t.Fatalf("перепись неверна: %s — ожидалось цитат 1, с флагом 0", census)
	}
}

// `docker buildx bake` — другая команда; читать её как `docker build` значило бы
// обвинять на пустом месте.
func TestDocsBuildGate_DoesNotReadBuildxAsBuild(t *testing.T) {
	corpus := injBuildCorpus{
		"services/vpc/docs/page.md": "```sh\ndocker buildx bake -f kacho-vpc/docker-bake.hcl\n```\n",
	}
	findings, census := injBuildScan(t, corpus)
	if len(findings) != 0 || census.citations != 0 {
		t.Fatalf("buildx прочитан как build: находок %v, перепись %s", findings, census)
	}
}

// ── предпосылка: ноль цитат — это «не прочитано», а не «чисто» ───────────────

func TestDocsBuildGate_ZeroCitationsIsDistinguishableFromZeroFindings(t *testing.T) {
	corpus := injBuildCorpus{
		"services/vpc/docs/page.md": "страница без единой команды сборки\n",
	}
	findings, census := injBuildScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("находки на корпусе без команд: %v", findings)
	}
	// Ровно это условие гейт на дереве обязан читать как ОТКАЗ: иначе сломанный
	// разбор областей документа выглядел бы как чистое дерево.
	if census.citations != 0 {
		t.Fatalf("перепись насчитала цитаты там, где их нет: %s", census)
	}
	if census.docs == 0 {
		t.Fatal("перепись не назвала объём осмотренного — «ноль находок» стало бы " +
			"неотличимо от «ноль прочитанного»")
	}
}
