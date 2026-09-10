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

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
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
			Value: "github.com/PRO-Robotech/kacho/pkg/operations", Module: "github.com/PRO-Robotech/kacho"},
	}
	declared := []string{"github.com/PRO-Robotech/kacho"} // модуль службы уехал

	faults, census := judgeModulePathConstants(consts, declared, 10, 40, 1)
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

	faults, census := judgeModulePathConstants(consts, declared, 10, 40, 1)
	if len(faults) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v", faults)
	}
	if census.PerModule["github.com/PRO-Robotech/kaname"] != 1 {
		t.Fatalf("перепись не подтверждает, что смотреть было на что: %s", census)
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
		{"модулей ноль", injFaultsOf(judgeModulePathConstants(c, nil, 1, 1, 1)), "ни одного `go.mod`"},
		{"владельцев ноль", injFaultsOf(judgeModulePathConstants(c, d, 1, 1, 0)), "владельца имён"},
		{"файлов ноль", injFaultsOf(judgeModulePathConstants(c, d, 0, 1, 1)), "обход пуст"},
		{"констант ноль", injFaultsOf(judgeModulePathConstants(c, d, 1, 0, 1)), "строковых констант"},
	} {
		if len(tc.faults) != 1 || !strings.Contains(tc.faults[0], tc.want) {
			t.Fatalf("%s: прошло как чистота либо смешалось с соседним отказом: %v", tc.name, tc.faults)
		}
	}
}

func injFaultsOf(f []string, _ ModuleConstantCensus) []string { return f }

// ── ТОТ ЖЕ ОПЫТ НА ЖИВОМ ДЕРЕВЕ: снятие каталога обязано краснеть ────────────

// Пробы выше доказывают механизм на синтетике. Эта воспроизводит НАСТОЯЩИЙ
// переход на настоящем корпусе: те же константы дерева, судимые против состава
// без вынесенного модуля. Без неё доказано было бы, что ось работает вообще, но
// не что она сработает в тот день, ради которого заведена.
//
// Пара одно-фактная: близнец — тот же корпус при обоих объявленных модулях.
func TestModuleConstantGate_LiveCorpusRedsWhenTheCarvedOutModuleGoes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	const carvedTree = "services/iam/"
	const carvedModule = "github.com/PRO-Robotech/kaname"
	both := []string{"github.com/PRO-Robotech/kacho", carvedModule}
	owners := moduleNameOwners(both)

	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева не прочитан: %v", err)
	}
	var outside []ModulePathConstant
	read, parsed := 0, 0
	for _, abs := range files {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			t.Fatalf("путь %s не приводится к корню: %v", abs, err)
		}
		rel = filepath.ToSlash(rel)
		// Константы ВНУТРИ вынесенного дерева уезжают вместе с ним и находкой
		// не станут. Предмет — те, что остаются в монорепо.
		if strings.HasPrefix(rel, carvedTree) {
			continue
		}
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
		outside = append(outside, found...)
	}
	if parsed == 0 || read == 0 {
		t.Fatalf("корпус вне вынесенного дерева пуст: файлов %d, констант %d — "+
			"вердикт был бы о непрочитанном", parsed, read)
	}

	// БЛИЗНЕЦ: оба модуля объявлены — молчание.
	if faults, census := judgeModulePathConstants(outside, both, parsed, read, len(owners)); len(faults) != 0 {
		t.Fatalf("живой корпус краснеет при обоих объявленных модулях: %v; перепись: %s", faults, census)
	}

	// ВНЕСЁННЫЙ ФАКТ: объявление второго модуля снято, а константа, называющая
	// его, в корпусе ЕСТЬ.
	//
	// Константа подаётся СИНТЕТИЧЕСКОЙ, и это не ослабление, а починка формы.
	// Прежде инъекция брала её из живого дерева — то есть требовала, чтобы дерево
	// НЕСЛО дефект, ради которого гейт заведён. Пока разрез был впереди, такая
	// константа в нём действительно была; после разреза её не осталось ни одной,
	// и проба покраснела на достижении собственной цели: «снятие прошло молча»
	// печаталось на дереве, где снимать было нечего.
	//
	// Живой корпус остаётся ПОЛОЖИТЕЛЬНЫМ близнецом выше (оба модуля объявлены —
	// молчание) и премисой непустоты; отрицательная половина стоит теперь на
	// входе, который проба строит сама и который поэтому не может исчезнуть.
	injected := append(append([]ModulePathConstant(nil), outside...), ModulePathConstant{
		File:   "internal/repohygiene/zz_injected_module_constant.go",
		Line:   1,
		Name:   "injectedCarvedModule",
		Value:  carvedModule,
		Module: carvedModule,
	})
	faults, census := judgeModulePathConstants(injected, both[:1], parsed, read+1, len(owners))
	if len(faults) == 0 {
		t.Fatalf("снятие модуля прошло молча — ровно тот случай, ради которого гейт "+
			"заведён; перепись: %s", census)
	}
	for _, f := range faults {
		if !strings.Contains(f, carvedModule) {
			t.Fatalf("находка не называет уехавший модуль: %q", f)
		}
	}
	t.Logf("одно-фактная пара на живом корпусе: файлов вне %s — %d, констант прочитано %d, "+
		"названных путём модуля %d; при обоих модулях находок 0, без вынесенного — %d",
		carvedTree, parsed, read, census.Matched, len(faults))
	for _, f := range faults {
		t.Logf("  %s", f)
	}
}
