// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// standaloneserviceimports_injection_test.go — доказательство способности гейта
// standaloneserviceimports_test.go упасть и смолчать.
//
// Каждая ось проверяется В ОБЕ СТОРОНЫ. Законные близнецы взяты ИЗ ДЕРЕВА, а не
// сочинены: сочинённый близнец доказывает молчание на форме, которой в корпусе
// нет, и оставляет гейт вакуумным ровно там, где он должен работать. Формы
// импортов ниже — дословно те, что стоят в `services/iam` после разреза.
//
// Инъекция идёт по СИНТЕТИЧЕСКОМУ корпусу в памяти: дерево не трогается вовсе,
// поэтому «краснеет только новый гейт» держится by construction — соседние
// проверки этого файла не видят.
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

const injStandaloneModule = "github.com/PRO-Robotech/kacho"

// injStandaloneCorpus — файл → его исходник.
type injStandaloneCorpus map[string]string

func (c injStandaloneCorpus) read(rel string) ([]byte, error) {
	body, ok := c[rel]
	if !ok {
		return nil, fmt.Errorf("нет такого файла")
	}
	return []byte(body), nil
}

func (c injStandaloneCorpus) files() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

// injGoFile собирает минимальный разбираемый исходник с этими импортами.
func injGoFile(pkg string, imports ...string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\nimport (\n")
	for _, im := range imports {
		b.WriteString("\t\"" + im + "\"\n")
	}
	b.WriteString(")\n")
	return b.String()
}

func injStandaloneScan(t *testing.T, c injStandaloneCorpus) ([]standaloneImportFinding, standaloneImportCensus) {
	t.Helper()
	f, cs, err := scanStandaloneServiceImports("services/iam", c.files(), c.read, injStandaloneModule)
	if err != nil {
		t.Fatalf("обход синтетического корпуса: %v", err)
	}
	return f, cs
}

// ── сторона (а): дефект краснеет и называет координату ───────────────────────

// Возвращённый дефект в его САМОЙ ЧАСТОЙ форме: тестовая поддержка Postgres.
// До разреза так импортировали 145 файлов iam — это и был вес отвязки.
func TestStandaloneGate_RedsOnTheRootInternalImportItWasBuiltFor(t *testing.T) {
	corpus := injStandaloneCorpus{
		"services/iam/cmd/kaname/testmain_pgtest_test.go": injGoFile("main_test",
			"testing",
			injStandaloneModule+"/internal/pgtest",
		),
	}
	findings, census := injStandaloneScan(t, corpus)
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d (перепись: %s)", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{
		"services/iam/cmd/kaname/testmain_pgtest_test.go",
		":5", // строка импорта, а не файла: `package`, пустая, `import (`, "testing", затем он
		injStandaloneModule + "/internal/pgtest",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
	if findings[0].Ground != standaloneGroundLanguage {
		t.Errorf("основание не то: %q — корневой `internal/` закрывает КОМПИЛЯТОР, и читатель "+
			"обязан это видеть, иначе он примет обязательную правку за вкусовую", findings[0].Ground)
	}
	if census.LanguageBound != 1 {
		t.Errorf("перепись не отделила то, на чём откажет сборка: %s", census)
	}
}

// Вторая ось того же дефекта: корневой `tools/`. Основание здесь ДРУГОЕ, и
// перепись обязана их различать — иначе гейт объявляет «сборка откажет» там, где
// она не откажет, и первый же проверивший снимет его как неверный.
func TestStandaloneGate_RedsOnRootToolsWithTheWeakerGround(t *testing.T) {
	corpus := injStandaloneCorpus{
		"services/iam/tools/auditlistfilter/profile.go": injGoFile("auditlistfilter",
			injStandaloneModule+"/tools/listfiltergate",
		),
	}
	findings, census := injStandaloneScan(t, corpus)
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d (перепись: %s)", len(findings), census)
	}
	if findings[0].Ground != standaloneGroundForeign {
		t.Errorf("корневой `tools/` объявлен закрытым языком — это неверно и проверено опытом: "+
			"внешний модуль импортирует такой путь без жалобы (rc=0). Основание: %q", findings[0].Ground)
	}
	if census.LanguageBound != 0 {
		t.Errorf("перепись зачла `tools/` в «сборка откажет»: %s", census)
	}
}

// Ось, которой перечень запрещённого не имел бы вовсе: чужой сервис. Ради неё
// предикат и сделан положительным.
func TestStandaloneGate_RedsOnAForeignServiceImportNoBlacklistWouldHaveNamed(t *testing.T) {
	corpus := injStandaloneCorpus{
		"services/iam/internal/apps/kacho/api/x.go": injGoFile("api",
			injStandaloneModule+"/services/vpc/internal/domain",
		),
	}
	findings, census := injStandaloneScan(t, corpus)
	if len(findings) != 1 {
		t.Fatalf("импорт чужого сервиса не пойман: находок %d (перепись: %s) — перечень "+
			"запрещённого закрыл бы только то, что уже случилось", len(findings), census)
	}
	if !strings.Contains(findings[0].Import, "/services/vpc/") {
		t.Errorf("находка называет не тот путь: %s", findings[0].String())
	}
}

// Антимаска: находка обязана быть названа И ТОГДА, когда рядом в том же файле
// стоят законные импорты. Иначе один разрешённый сосед глушил бы дефект.
func TestStandaloneGate_RedsEvenWhenLawfulImportsStandBeside(t *testing.T) {
	corpus := injStandaloneCorpus{
		"services/iam/internal/authzmodel/admit.go": injGoFile("authzmodel",
			"fmt",
			injStandaloneModule+"/pkg/ids",
			injStandaloneModule+"/internal/authzplan",
			injStandaloneModule+"/services/iam/internal/domain",
		),
	}
	findings, census := injStandaloneScan(t, corpus)
	if len(findings) != 1 {
		t.Fatalf("находка среди законных соседей потеряна: находок %d (перепись: %s)", len(findings), census)
	}
	if census.Allowed != 2 {
		t.Errorf("законные соседи не сосчитаны: %s — «ноль находок» тогда неотличимо от "+
			"«ноль прочитанного»", census)
	}
}

// ── сторона (б): законный близнец обязан молчать ─────────────────────────────

// Тот же файл дерева ПОСЛЕ отвязки — дословная форма из
// services/iam/cmd/kaname/testmain_pgtest_test.go.
func TestStandaloneGate_SilentOnTheSharedFoundation(t *testing.T) {
	corpus := injStandaloneCorpus{
		"services/iam/cmd/kaname/testmain_pgtest_test.go": injGoFile("main_test",
			"testing",
			injStandaloneModule+"/pkg/pgtest",
		),
	}
	findings, census := injStandaloneScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("общий фундамент объявлен находкой: %v — гейт, краснеющий на верном дереве, "+
			"отключают первым (перепись: %s)", findings, census)
	}
	if census.Allowed != 1 {
		t.Fatalf("близнец не дошёл до предиката: %s — молчание тогда означает «не прочитано», "+
			"а не «верно»", census)
	}
}

// Собственное поддерево — дословная форма из services/iam/internal/authzmodel/admit.go
// после переезда `authzplan` под iam. Элемент `internal` в пути ЕСТЬ, и гейт
// обязан не спутать его с корневым: правило языка про свой модуль не действует.
func TestStandaloneGate_SilentOnItsOwnInternalSubtree(t *testing.T) {
	corpus := injStandaloneCorpus{
		"services/iam/internal/authzmodel/admit.go": injGoFile("authzmodel",
			injStandaloneModule+"/services/iam/internal/authzplan",
		),
	}
	findings, census := injStandaloneScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("собственное поддерево сервиса объявлено находкой: %v (перепись: %s) — "+
			"предикат спутал свой `internal` с корневым", findings, census)
	}
	if census.Allowed != 1 {
		t.Fatalf("близнец не дошёл до предиката: %s", census)
	}
}

// Стабы контракта — подкаталог `pkg/`, отдельной записи не требуют. Форма
// дословная: их импортируют 92 прод-файла iam.
func TestStandaloneGate_SilentOnGeneratedContractStubs(t *testing.T) {
	corpus := injStandaloneCorpus{
		"services/iam/internal/apps/kacho/api/user/handler.go": injGoFile("user",
			injStandaloneModule+"/pkg/api/kacho/cloud/iam/v1",
			injStandaloneModule+"/pkg/api/kacho/cloud/operation",
		),
	}
	findings, census := injStandaloneScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("стабы контракта объявлены находкой: %v (перепись: %s)", findings, census)
	}
	if census.Allowed != 2 {
		t.Fatalf("стабы не дошли до предиката: %s", census)
	}
}

// Чужой модуль и stdlib предметом не являются вовсе — иначе гейт краснел бы на
// каждом файле дерева.
func TestStandaloneGate_SilentOnStdlibAndThirdParty(t *testing.T) {
	corpus := injStandaloneCorpus{
		"services/iam/internal/repo/kacho/pg/x.go": injGoFile("pg",
			"context",
			"github.com/jackc/pgx/v5",
			"google.golang.org/grpc",
		),
	}
	findings, census := injStandaloneScan(t, corpus)
	if len(findings) != 0 {
		t.Fatalf("чужой модуль объявлен находкой: %v (перепись: %s)", findings, census)
	}
	if census.Imports != 3 || census.OwnModule != 0 {
		t.Errorf("перепись не отделила свой модуль от чужого: %s", census)
	}
}

// ── перепись обязана отличать «ноль находок» от «ноль прочитанного» ──────────

// Корпус без единого импорта своего модуля даёт OwnModule = 0 — величину, на
// которой гейт дерева ОБЯЗАН упасть с диагнозом «сломан разбор», а не отчитаться
// зелёным. Здесь проверяется, что величина действительно приходит нулём.
func TestStandaloneGate_CensusSeparatesNothingFoundFromNothingRead(t *testing.T) {
	empty := injStandaloneCorpus{"services/iam/x.go": injGoFile("x")}
	_, census := injStandaloneScan(t, empty)
	if census.Files != 1 {
		t.Fatalf("перепись не сосчитала прочитанное: %s", census)
	}
	if census.Imports != 0 || census.OwnModule != 0 {
		t.Fatalf("пустой файл дал импорты: %s", census)
	}
}

// Непрочитанный файл — ОТКАЗ, а не молчаливый ноль.
func TestStandaloneGate_UnreadableFileIsARefusalNotAZero(t *testing.T) {
	_, _, err := scanStandaloneServiceImports(
		"services/iam",
		[]string{"services/iam/нет-такого.go"},
		injStandaloneCorpus{}.read,
		injStandaloneModule,
	)
	if err == nil {
		t.Fatalf("непрочитанный файл принят за пустой — «ноль находок» на «ноль прочитанного»")
	}
}

// ── дискриминатор оснований — обе ветви ──────────────────────────────────────

func TestStandaloneGate_GroundDiscriminatorNamesBothBranches(t *testing.T) {
	for _, c := range []struct {
		rel  string
		want string
	}{
		{"internal/pgtest", standaloneGroundLanguage},
		{"pkg/internal/tlsutil", standaloneGroundLanguage},
		{"services/vpc/internal/domain", standaloneGroundLanguage},
		{"tools/listfiltergate", standaloneGroundForeign},
		{"gateway/internal2/x", standaloneGroundForeign},
		{"terraform/provider", standaloneGroundForeign},
	} {
		if got := standaloneImportGround(c.rel); got != c.want {
			t.Errorf("%s: основание %q, ожидалось %q", c.rel, got, c.want)
		}
	}
}
