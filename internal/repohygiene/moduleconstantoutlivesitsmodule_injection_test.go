// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// moduleconstantoutlivesitsmodule_injection_test.go — доказательство
// способности гейта упасть и смолчать.
//
// Гейт ловит МОЛЧАНИЕ: константу, чьё условие перестало совпадать вместе с
// уехавшим модулем. Вакуумным он становится проще всего — достаточно, чтобы
// распознаватель перестал узнавать форму записи, и всякая такая константа
// пройдёт мимо, не дав ни красного, ни зелёного. Поэтому по каждой оси стоит
// пара: внесённый факт и ЗАКОННЫЙ БЛИЗНЕЦ, отличающийся ровно одним фактом.
//
// # Почему фикстуры — ИСХОДНЫЙ ТЕКСТ, а не настоящие константы
//
// Объявить здесь `const dead = "github.com/PRO-Robotech/nosuchmodule/x"` было бы
// нельзя: гейт судит ВСЁ дерево, и такая константа стала бы его собственной
// находкой. Фикстура поэтому есть исходник, подаваемый разбору строкой, — и это
// же положительный контроль на распознаватель: литерал, лишь СОДЕРЖАЩИЙ путь
// модуля, координатой не является и судиться не должен.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// injOwners — владелец имён этого дерева, выведенный так же, как на дереве.
var injOwners = moduleNameOwners([]string{"github.com/PRO-Robotech/kacho"})

// injCollect — разбор одного синтетического исходника.
func injCollect(t *testing.T, src string) ([]ModulePathConstant, int) {
	t.Helper()
	got, read, err := collectModulePathConstants("inj/x.go", src, injOwners)
	if err != nil {
		t.Fatalf("фикстура не разобрана: %v", err)
	}
	return got, read
}

// ── ось РАСПОЗНАВАТЕЛЯ: координата против литерала, её содержащего ───────────

func TestModuleConstantGate_JudgesTheWholeLiteralNotOneContainingIt(t *testing.T) {
	t.Parallel()

	// ВНЕСЁННЫЙ ФАКТ: значение константы ЦЕЛИКОМ есть путь пакета под модулем.
	coord, _ := injCollect(t, `package p

const applierImportPath = "github.com/PRO-Robotech/kaname/internal/apps/kaname/moduleroles"
`)
	if len(coord) != 1 || coord[0].Module != "github.com/PRO-Robotech/kaname" {
		t.Fatalf("координата не распознана: %+v", coord)
	}
	if coord[0].Name != "applierImportPath" || coord[0].Line != 3 {
		t.Fatalf("находка не называет ни имени, ни строки: %+v", coord[0])
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ, отличающийся ровно одним фактом — тот же путь стоит
	// ВНУТРИ более длинного литерала (фикстура инъекции соседнего гейта). Она
	// о дереве не утверждает ничего и после снятия модуля продолжает работать.
	fixture, read := injCollect(t, `package p

const injSrc = "package q\n\nimport \"github.com/PRO-Robotech/kaname/internal/clients\"\n"
`)
	if len(fixture) != 0 {
		t.Fatalf("фикстура инъекции принята за координату: %+v", fixture)
	}
	if read != 1 {
		t.Fatalf("перепись не прочитала константу вовсе: %d — тогда молчание выше "+
			"означает сломанный разбор, а не законный близнец", read)
	}
}

// Второй законный близнец: комментарий, называющий модуль. Без этой пробы
// достаточно было бы поиска по подстроке — и гейт краснел бы на СВОЁМ
// объяснении: шапка moduleconstantoutlivesitsmodule.go называет модули дважды.
func TestModuleConstantGate_DoesNotRedOnItsOwnProse(t *testing.T) {
	t.Parallel()
	got, read := injCollect(t, `package p

// Здесь стояла константа github.com/PRO-Robotech/kaname/internal/authzguard,
// и она пережила свой модуль. Это проза, а не координата.
const unrelated = "просто строка"
`)
	if len(got) != 0 {
		t.Fatalf("проза принята за координату: %+v", got)
	}
	if read != 1 {
		t.Fatalf("разбор не дошёл до константы: прочитано %d", read)
	}
}

// Третий: чужая зависимость. Владелец имён выводится из дерева, поэтому пути
// чужих модулей выпадают из оси by construction, а не по списку исключений.
func TestModuleConstantGate_ForeignModulesAreOutOfTheAxis(t *testing.T) {
	t.Parallel()
	got, read := injCollect(t, `package p

const grpcPkg = "google.golang.org/grpc/codes"
const stdish = "github.com/SomeoneElse/kaname/internal/x"
`)
	if len(got) != 0 {
		t.Fatalf("чужой модуль втянут в ось: %+v", got)
	}
	if read != 2 {
		t.Fatalf("разбор прочитал %d констант из 2", read)
	}
}

// ── ось СУЖДЕНИЯ: объявлен модуль или нет ───────────────────────────────────

func TestModuleConstantGate_RedsWhenTheNamedModuleIsNotDeclared(t *testing.T) {
	t.Parallel()
	consts := []ModulePathConstant{
		{File: "internal/repohygiene/a.go", Line: 95, Name: "applierImportPath",
			Value: "github.com/PRO-Robotech/kaname/internal/x", Module: "github.com/PRO-Robotech/kaname"},
		{File: "internal/repohygiene/b.go", Line: 26, Name: "operationsPkgPath",
			Value: "github.com/PRO-Robotech/corelib/operations", Module: "github.com/PRO-Robotech/kacho"},
	}
	declared := []string{"github.com/PRO-Robotech/kacho"} // модуль службы уехал

	faults, census := judgeModulePathConstants(consts, declared, nil, 10, 40, 1)
	if len(faults) != 1 {
		t.Fatalf("константа, пережившая свой модуль, не найдена: %v", faults)
	}
	for _, want := range []string{"a.go:95", "applierImportPath", "github.com/PRO-Robotech/kaname"} {
		if !strings.Contains(faults[0], want) {
			t.Fatalf("находка не называет %q: %q", want, faults[0])
		}
	}
	if census.PerModule["github.com/PRO-Robotech/kacho"] != 1 || census.Matched != 2 {
		t.Fatalf("перепись не разводит модули: %s", census)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ: тот же вход, отличающийся ровно одним фактом — модуль
// объявлен. Без него отрицание выше зеленело бы на суждении, краснеющем на всём.
func TestModuleConstantGate_SilentWhileTheModuleIsDeclared(t *testing.T) {
	t.Parallel()
	consts := []ModulePathConstant{
		{File: "internal/repohygiene/a.go", Line: 95, Name: "applierImportPath",
			Value: "github.com/PRO-Robotech/kaname/internal/x", Module: "github.com/PRO-Robotech/kaname"},
	}
	declared := []string{"github.com/PRO-Robotech/kacho", "github.com/PRO-Robotech/kaname"}

	faults, census := judgeModulePathConstants(consts, declared, nil, 10, 40, 1)
	if len(faults) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v", faults)
	}
	if census.PerModule["github.com/PRO-Robotech/kaname"] != 1 {
		t.Fatalf("перепись не подтверждает, что смотреть было на что: %s", census)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ ВТОРОГО СПОСОБА ЗНАТЬ: модуль не объявлен, а ЗАТРЕБОВАН.
//
// Ось заведена своей парой, потому что это НОВОЕ свойство суждения, а не
// частный случай прежнего: ровно на нём гейт дал 24 ложные находки в день, когда
// фундамент вынесли отдельным опубликованным модулем. Отрицание к ней —
// TestModuleConstantGate_RedsWhenTheNamedModuleIsNotDeclared выше: тот же вход,
// отличающийся ровно одним фактом — модуль не назван НИ ОДНИМ из двух способов.
func TestModuleConstantGate_SilentWhileTheModuleIsRequired(t *testing.T) {
	t.Parallel()
	consts := []ModulePathConstant{
		{File: "internal/repohygiene/a.go", Line: 26, Name: "operationsPkgPath",
			Value: "github.com/PRO-Robotech/corelib/operations", Module: "github.com/PRO-Robotech/corelib"},
	}
	declared := []string{"github.com/PRO-Robotech/kacho"}
	required := []string{"github.com/PRO-Robotech/corelib"}

	faults, census := judgeModulePathConstants(consts, declared, required, 10, 40, 1)
	if len(faults) != 0 {
		t.Fatalf("затребованный модуль объявлен незнаемым: %v", faults)
	}
	if census.PerModule["github.com/PRO-Robotech/corelib"] != 1 {
		t.Fatalf("перепись не подтверждает, что смотреть было на что: %s", census)
	}
	if census.Required != 1 || census.Modules != 1 {
		t.Fatalf("перепись не разводит объявленное и затребованное: %s", census)
	}
}

// РАЗБОРЩИК затребованных знает ОБЕ законные формы записи. Форма, о которой он
// не знает, даёт не красное и не зелёное — молчание: модуль читается незнаемым,
// и находка приходит на верном дереве.
func TestModuleConstantGate_RequireParserKnowsBothForms(t *testing.T) {
	t.Parallel()
	got := requiredModulePaths(`module github.com/PRO-Robotech/kacho

go 1.24

require github.com/PRO-Robotech/corelib v1.4.0

require (
	google.golang.org/grpc v1.83.2 // indirect
	github.com/PRO-Robotech/other v0.1.0
)
`)
	want := []string{
		"github.com/PRO-Robotech/corelib",
		"github.com/PRO-Robotech/other",
		"google.golang.org/grpc",
	}
	if len(got) != len(want) {
		t.Fatalf("формы записи `require` прочитаны не все: %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("разбор разошёлся: %v, ожидалось %v", got, want)
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: `go.mod` без `require` даёт пустой ответ, а не выдумку.
	if bare := requiredModulePaths("module github.com/PRO-Robotech/kacho\n\ngo 1.24\n"); len(bare) != 0 {
		t.Fatalf("объявление без `require` дало %v", bare)
	}
	// И второй: слово `require` в комментарии директивой не является.
	if prose := requiredModulePaths("module m\n\n// require github.com/PRO-Robotech/ghost v1\n"); len(prose) != 0 {
		t.Fatalf("проза принята за директиву: %v", prose)
	}
}

// ── предпосылки: три РАЗНЫХ нуля — три отказа, а не один пустой успех ────────

func TestModuleConstantGate_EachEmptyInputIsARefusal(t *testing.T) {
	t.Parallel()
	c := []ModulePathConstant{{File: "a.go", Module: "github.com/PRO-Robotech/kacho"}}
	d := []string{"github.com/PRO-Robotech/kacho"}

	for _, tc := range []struct {
		name   string
		faults []string
		want   string
	}{
		{"модулей ноль", injFaultsOf(judgeModulePathConstants(c, nil, nil, 1, 1, 1)), "ни одного `go.mod`"},
		{"владельцев ноль", injFaultsOf(judgeModulePathConstants(c, d, nil, 1, 1, 0)), "владельца имён"},
		{"файлов ноль", injFaultsOf(judgeModulePathConstants(c, d, nil, 0, 1, 1)), "обход пуст"},
		{"констант ноль", injFaultsOf(judgeModulePathConstants(c, d, nil, 1, 0, 1)), "строковых констант"},
	} {
		if len(tc.faults) != 1 || !strings.Contains(tc.faults[0], tc.want) {
			t.Fatalf("%s: прошло как чистота либо смешалось с соседним отказом: %v", tc.name, tc.faults)
		}
	}
}

func injFaultsOf(f []string, _ ModuleConstantCensus) []string { return f }

// ── ТОТ ЖЕ ОПЫТ НА ЖИВОМ ДЕРЕВЕ: уход модуля из знаемых обязан краснеть ──────

// Пробы выше доказывают механизм на синтетике. Эта воспроизводит НАСТОЯЩИЙ
// переход на настоящем корпусе: те же константы дерева, судимые против набора
// знаемых модулей БЕЗ одного из них. Без неё доказано было бы, что ось работает
// вообще, но не что она сработает в тот день, ради которого заведена.
//
// # Пара одно-фактная, и оба её входа берутся ИЗ ДЕРЕВА
//
// Близнец — полный набор знаемых (объявленные плюс затребованные): молчание.
// Внесённый факт — тот же набор БЕЗ модуля фундамента: константы, называющие
// пути внутрь него, обязаны покраснеть, и покраснеть с координатой.
//
// Различие ровно одно и оно названо: одна запись набора. Корпус, распознаватель,
// перепись, суждение — те же.
//
// # Почему константы НАСТОЯЩИЕ, а не синтетические
//
// Прежняя редакция подавала синтетическую константу: на том дереве настоящих не
// оставалось ни одной, и проба краснела на достижении собственной цели. Сегодня
// их две дюжины — фундамент вынесен отдельным опубликованным модулем, и пути
// внутрь него названы константами по всему дереву. Отрицательная половина стоит
// поэтому на РЕАЛЬНОМ входе, и это сильнее синтетики: она доказывает, что
// красное придёт на тех самых координатах, которыми судят дерево.
//
// Премиса непустоты обязательна: корпус без таких констант обратил бы инъекцию
// в форму без содержания, и проба говорит об этом отказом, а не молчанием.
func TestModuleConstantGate_LiveCorpusRedsWhenTheCarvedOutModuleGoes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tree := newTrackedTree(t, root)

	declared, required := knownModulesOfTree(t, root, tree)
	owners := moduleNameOwners(declared)

	// Модуль, чей уход воспроизводится, ВЫВОДИТСЯ: берётся затребованный модуль
	// нашего владельца имён. Выписанное имя было бы ровно тем, что этот гейт
	// ловит, — координатой, переживающей свой предмет.
	ourRequired := ownNamed(required, owners)
	if len(ourRequired) == 0 {
		t.Skip("своих затребованных модулей у дерева нет: воспроизводить уход нечего. " +
			"Это не чистота и не находка — предмета инъекции в этом дереве не существует")
	}
	gone := ourRequired[0]

	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева не прочитан: %v", err)
	}
	var consts []ModulePathConstant
	read, parsed := 0, 0
	for _, abs := range files {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			t.Fatalf("путь %s не приводится к корню: %v", abs, err)
		}
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(abs) // #nosec G304 — путь из состава дерева
		if err != nil {
			t.Fatalf("%s не прочитан: %v", rel, err)
		}
		found, n, err := collectModulePathConstants(rel, string(b), owners)
		if err != nil {
			t.Fatalf("%s не разобран: %v", rel, err)
		}
		parsed++
		read += n
		consts = append(consts, found...)
	}
	if parsed == 0 || read == 0 {
		t.Fatalf("корпус пуст: файлов %d, констант %d — вердикт был бы о непрочитанном",
			parsed, read)
	}

	// ПРЕМИСА ОТРИЦАНИЯ: константы, называющие уходящий модуль, в корпусе ЕСТЬ.
	// Без неё красное ниже было бы невозможно, и молчание читалось бы как
	// «снятие прошло тихо» на дереве, где снимать нечего.
	naming := 0
	for _, c := range consts {
		if c.Module == gone {
			naming++
		}
	}
	if naming == 0 {
		t.Fatalf("ни одна константа не называет %s: инъекция беспредметна — уход этого "+
			"модуля нечем сделать заметным", gone)
	}

	// БЛИЗНЕЦ: полный набор знаемых — молчание.
	if faults, census := judgeModulePathConstants(
		consts, declared, required, parsed, read, len(owners)); len(faults) != 0 {
		t.Fatalf("живой корпус краснеет при полном наборе знаемых модулей: %v; перепись: %s",
			faults, census)
	}

	// ВНЕСЁННЫЙ ФАКТ — ровно один: одна запись ушла из знаемых.
	shrunk := make([]string, 0, len(required))
	for _, m := range required {
		if m != gone {
			shrunk = append(shrunk, m)
		}
	}
	faults, census := judgeModulePathConstants(consts, declared, shrunk, parsed, read, len(owners))
	if len(faults) != naming {
		t.Fatalf("уход модуля %s дал находок %d, а называющих его констант %d — "+
			"суждение потеряло часть предмета; перепись: %s", gone, len(faults), naming, census)
	}
	for _, f := range faults {
		if !strings.Contains(f, gone) {
			t.Fatalf("находка не называет уехавший модуль: %q", f)
		}
	}
	t.Logf("одно-фактная пара на живом корпусе: файлов %d, констант прочитано %d, "+
		"названных путём модуля %d; при полном наборе находок 0, без %s — %d",
		parsed, read, census.Matched, gone, len(faults))
}
