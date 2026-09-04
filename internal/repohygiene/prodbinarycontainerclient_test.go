// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// prodbinarycontainerclient_test.go — боевой бинарь не линкует клиент
// контейнерной среды.
//
// # Предмет
//
// Go компилирует в бинарь ВСЁ, что импортируется по графу, и назначение файла на
// это не влияет: суффикс `_test.go` выводит файл из сборки, слово «test» в его
// имени — нет. Помощник для проб, положенный в непроверочный файл пакета, который
// импортирует прод-код, приезжает в боевой процесс вместе со всей своей
// зависимостью.
//
// Цена двойная и обе половины самостоятельны. Размер: образ несёт десятки
// пакетов, которых его работа не требует. ПОВЕРХНОСТЬ: в боевом процессе
// оказывается код, умеющий говорить с демоном контейнеров, и исполняются `init()`
// библиотек, к предмету сервиса отношения не имеющих. Разница возникла не
// решением, а суффиксом имени файла.
//
// # Где наблюдалось
//
// Задача #1484. `services/iam/internal/repo/kacho/pg/testhelpers.go` — непроверочный
// файл пакета, который импортирует прод-код, — тянул `pkg/pgtest`, а тот
// `testcontainers-go` вместе с клиентом Docker. Задеты были два бинаря:
// `services/iam/cmd/kacho-iam` и
// `services/iam/internal/scopesourcecensus/cmd/scope-source-census-sql` — по 54
// пакета в каждом, при нуле у всех остальных.
//
// Отдельно поучительно, что шапка того файла УТВЕРЖДАЛА обратное: «a helper never
// linked into a production binary (nothing under cmd/ imports it)». Комментарий был
// ложен в момент чтения, и обзор диффа этого показать не мог: импортировал помощник
// не `cmd/`, а пакет-репозиторий, который `cmd/` тянет законно.
//
// # Почему предикат — граф сборки, а не имя файла
//
// Текстовый предикат «файл называется testhelpers» ловит форму, а не существо:
// помощник может называться как угодно, а сломать бинарь может любой импорт.
// Предикат этого гейта — то самое свойство, которое требуется: перечень
// транзитивных зависимостей бинаря. Его считает сам инструмент сборки, поэтому
// гейт не может разойтись с тем, что реально попадёт в образ.
//
// # Чего гейт НЕ утверждает
//
// Он судит ТОЛЬКО пакеты `main`. Библиотека проб, законно импортирующая
// `testcontainers`, — `pkg/pgtest` — под него не подпадает и подпадать не
// должна: предмет запрета не «эта зависимость есть в дереве», а «она доезжает до
// боевого процесса».
//
// Он не судит и происхождение помощника: где живёт код проб — предмет решения
// автора, гейт держит только его СЛЕДСТВИЕ.
//
// # Отличие от предиката задачи, названное намеренно
//
// Признак в #1484 — `go list -deps <бинарь> | grep -cE 'containerd|docker|
// testcontainers|moby'`. Гейт берёт те же четыре токена, но пропускает пакеты
// СВОЕГО модуля: собственный пакет с именем вроде `dockerfilegen` — не клиент
// контейнерной среды, и находка на нём была бы ложной. Сегодня разницы нет
// (собственных совпадений в дереве ноль, см. перепись), поэтому гейт и признак
// задачи измеряют одно и то же; оговорка стоит здесь, чтобы завтра расхождение
// не выглядело дефектом.
package repohygiene

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// containerRuntimeTokens — по этим токенам узнаётся клиент контейнерной среды.
// Те же четыре, что в признаке задачи #1484, — чтобы гейт и задача мерили одно.
var containerRuntimeTokens = []string{"containerd", "docker", "testcontainers", "moby"}

// ownModulePrefix — путь собственного модуля. Пакеты под ним из-под токенов
// выведены: см. §«Отличие от предиката задачи» в шапке.
const ownModulePrefix = "github.com/PRO-Robotech/kacho/"

// listedPackage — то, что гейт знает о пакете. Ровно три поля, потому что судья
// больше ничего не спрашивает: имя отличает бинарь от библиотеки, перечень
// зависимостей и есть предмет.
type listedPackage struct {
	ImportPath string
	Name       string
	Deps       []string
}

// prodBinaryFinding — бинарь, дотянувшийся до клиента контейнерной среды.
type prodBinaryFinding struct {
	Binary string
	Deps   []string
}

func (f prodBinaryFinding) String() string {
	shown := f.Deps
	const maxShown = 5
	suffix := ""
	if len(shown) > maxShown {
		shown = shown[:maxShown]
		suffix = fmt.Sprintf(" … и ещё %d", len(f.Deps)-maxShown)
	}
	return fmt.Sprintf("%s линкует клиент контейнерной среды — пакетов %d: %s%s",
		f.Binary, len(f.Deps), strings.Join(shown, ", "), suffix)
}

// prodBinaryCensus — объём осмотренного. Без него «ноль находок» неотличимо от
// «ноль прочитанного», а у гейта, зовущего внешнюю команду, есть и третий исход:
// команда не отработала вовсе.
type prodBinaryCensus struct {
	PackagesListed    int
	BinariesSeen      int
	OwnMatchesSkipped int
}

func (c prodBinaryCensus) String() string {
	return fmt.Sprintf(
		"перепись: пакетов перечислено %d · из них бинарей осмотрено %d · "+
			"совпадений в собственном модуле пропущено %d",
		c.PackagesListed, c.BinariesSeen, c.OwnMatchesSkipped)
}

// containerRuntimeDeps — какие из зависимостей суть клиент контейнерной среды.
// Возвращает их поимённо, а не число: находка без координаты — не действие.
func containerRuntimeDeps(deps []string) (matched []string, ownSkipped int) {
	for _, dep := range deps {
		hit := false
		for _, tok := range containerRuntimeTokens {
			if strings.Contains(dep, tok) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if strings.HasPrefix(dep, ownModulePrefix) {
			ownSkipped++
			continue
		}
		matched = append(matched, dep)
	}
	sort.Strings(matched)
	return matched, ownSkipped
}

// auditProdBinaryContainerClients — судья. Вынесен из тела теста, чтобы ТОТ ЖЕ
// судья судил синтетические записи пробы инъекции: гейт, чья способность упасть
// проверена другим кодом, не проверена.
func auditProdBinaryContainerClients(pkgs []listedPackage) ([]prodBinaryFinding, prodBinaryCensus) {
	var (
		findings []prodBinaryFinding
		census   prodBinaryCensus
	)
	census.PackagesListed = len(pkgs)

	for _, p := range pkgs {
		if p.Name != "main" {
			continue
		}
		census.BinariesSeen++

		matched, ownSkipped := containerRuntimeDeps(p.Deps)
		census.OwnMatchesSkipped += ownSkipped
		if len(matched) > 0 {
			findings = append(findings, prodBinaryFinding{Binary: p.ImportPath, Deps: matched})
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].Binary < findings[j].Binary })
	return findings, census
}

// listPackagesWithDeps — производитель входа: один вызов инструмента сборки на всё
// дерево. Транзитивные зависимости считает он сам, поэтому гейт не может разойтись
// с тем, что попадёт в образ.
//
// Ошибка команды возвращается ОТДЕЛЬНО от находок: «инструмент не отработал» — это
// третий исход, и подавать его красным вердиктом значило бы соврать о предмете.
func listPackagesWithDeps(root string) ([]listedPackage, error) {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}\t{{.Name}}\t{{join .Deps \" \"}}", "./...")
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("go list не отработал: %w\n%s", err, stderr)
	}

	var pkgs []listedPackage
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		p := listedPackage{ImportPath: parts[0], Name: parts[1]}
		if len(parts) == 3 && parts[2] != "" {
			p.Deps = strings.Fields(parts[2])
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

func TestProdBinaryDoesNotLinkAContainerRuntimeClient(t *testing.T) {
	root := repoRoot(t)

	pkgs, err := listPackagesWithDeps(root)
	if err != nil {
		t.Fatalf("вход не получен, вердикта нет: %v", err)
	}

	findings, census := auditProdBinaryContainerClients(pkgs)
	t.Log(census.String())

	// Пустой обход — отказ, а не тишина: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	if census.PackagesListed == 0 {
		t.Fatal("перечислено ноль пакетов — гейту нечего было осматривать")
	}
	if census.BinariesSeen == 0 {
		t.Fatal("бинарей осмотрено ноль — предпосылка гейта не выполнена")
	}

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "боевых бинарей с клиентом контейнерной среды: %d\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f.String())
		}
		b.WriteString("\nПомощник для проб обязан жить там, куда сборка не заглядывает: " +
			"файл `*_test.go` либо отдельный пакет, который импортируют ТОЛЬКО пробы.")
		t.Fatal(b.String())
	}
}
