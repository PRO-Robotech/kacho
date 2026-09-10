// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// chart_name_addresses_exactly_one_chart_test.go — ИМЯ ЧАРТА ОБЯЗАНО АДРЕСОВАТЬ,
// А ГДЕ НЕ АДРЕСУЕТ — РАЗЛИЧИЕ ОБЪЯВЛЕНО ДАННЫМИ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ — ловушка ЗАМЕРА, а не совпадение строк
//
// Двух чартов с одним именем достаточно, чтобы имя перестало быть адресом. Цена
// измерена, а не предположена: два независимых замера подряд вынесли вердикт по
// открытому предмету НЕ ПО ТОМУ чарту — координаты подчарта стенда приписали
// чарту продукта, а обход с областью `deploy` разрешился в каталог верхнего
// уровня и чарт продукта не увидел вовсе. Оба замера выглядели аккуратными: с
// координатами, номерами строк и командой.
//
// Следствие дороже самой ошибки: по такому вердикту снимают метку ожидания, а
// снятая метка ЗАПРЕЩАЕТ брать задачу, которую никто не ведёт, — и запрет этот
// невидим.
//
// Это тот же класс, что «ключ домена и имя каталога — разные словари, и оба
// зовутся одинаково»: имя, совпадающее в двух местах, перестаёт быть адресом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ НЕ ПЕРЕИМЕНОВАНИЕ — отвергнуто ЗАМЕРОМ, и обе цены названы
//
// У ПОДЧАРТА имя чарта есть ключ секции значений: его несут девять профилей
// стенда, плюс сотни ссылок вида `<имя>.<путь>` в рецептах и пробах. Helm
// незнакомый ключ верхнего уровня НЕ отвергает — переименование не сломало бы
// установку, оно молча вернуло бы подчарт к собственным умолчаниям на каждом
// развёрнутом стенде. Отказ был бы тихим, а это худший вид отказа.
//
// У ЧАРТА ПРОДУКТА переименование дёшево: `.Chart.Name` не читает ни один его
// шаблон, имена объектов идут от `.Values.name`, поэтому в кластере не
// поменялось бы ни одно имя. Но имя `kaname` там и есть имя продукта — отнять
// его у продукта ради различения значило бы чинить не ту сторону.
//
// Поэтому различие объявляется ТРЕТЬИМ признаком — назначением поставки, — и
// он единственный, которым предикат «чарт продукта» подчартом удовлетворить
// нельзя. Путь и лицензия, различавшие их до сих пор, для этого не годятся:
// путь не спрашивают, когда ключ поиска — имя, а лицензию не спрашивают никогда.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО ТРЕБУЕТСЯ, И ПОЧЕМУ КАЖДОЕ ИЗ ТРЁХ
//
//	назначение поставки  — оно и есть ключ, которым предикат адресует;
//	путь тёзки           — читатель обязан узнать о втором, не зная о нём;
//	взаимность           — односторонняя запись оставляет половину ловушки: тот,
//	                       кто открыл ВТОРОЙ чарт, о первом по-прежнему не узнает.
//
// Пара (имя · назначение) обязана быть уникальной: два чарта с одним именем и
// одним назначением вернули бы неадресующий предикат под другим ключом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОСЛАБЛЕНИЕ ИСТЕКАЕТ САМО
//
// Объявление требуется ТОЛЬКО от чартов-тёзок. Станут имена уникальны —
// объявление потеряет предмет, и оставшаяся запись сама станет находкой. Так
// проверка не превращается в налог на двадцать чартов, у девятнадцати из
// которых предмета нет by construction.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА СЧЁТА — ОТСЛЕЖИВАЕМЫЙ ФАЙЛ `Chart.yaml`, спрошенный у индекса git
//
// Не обход диска: под теми же каталогами живут распаковки чартов, рабочие копии
// полос и отчёты прогонов, и обход диска считал бы их наравне — вердикт стал бы
// свойством рабочего каталога, а не коммита. Обход, не давший ни одного чарта,
// — ОТКАЗ, а не чистое дерево.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ДОКАЗАНА СПОСОБНОСТЬ УПАСТЬ
//
// Разбор вынесен в чистую функцию auditChartNameAddressing, принимающую
// ОБЪЯВЛЕНИЯ. Инъекция — chart_name_addresses_exactly_one_chart_injection_test.go.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// chartNameDecl — объявление одного чарта: то, чем он себя называет.
type chartNameDecl struct {
	Path         string // путь от корня монорепо
	Name         string // поле name
	Distribution string // annotations.distribution
	NameTwin     string // annotations.nameTwin — путь чарта-тёзки
}

// Назначения поставки. Словарь ЗАКРЫТ: значение вне его не адресует ничего, а
// выглядит объявлением.
const (
	chartDistributionStandalone = "standalone" // уезжает тому, кто ставит продукт отдельно
	chartDistributionPlatform   = "platform"   // часть нашего стенда
)

// chartNameCensus — объём осмотренного. Печатается ВСЕГДА и отдельно от
// найденного: «ноль находок» обязано быть отличимо от «ноль прочитанного».
type chartNameCensus struct {
	ChartsRead     int
	NamesTotal     int
	NamesColliding int
	DeclsFound     int
}

func (c chartNameCensus) String() string {
	return fmt.Sprintf("чартов прочитано %d · имён %d · имён-тёзок %d · объявлений различия %d",
		c.ChartsRead, c.NamesTotal, c.NamesColliding, c.DeclsFound)
}

// auditChartNameAddressing — разбор объявлений.
//
// exists отвечает, резолвится ли путь в дереве. Отдельным доводом, а не чтением
// диска внутри: инъекция обязана управлять этим фактом, иначе случай «тёзка
// назван, но такого файла нет» доказать нечем.
func auditChartNameAddressing(decls []chartNameDecl, exists func(string) bool) ([]string, chartNameCensus, error) {
	census := chartNameCensus{ChartsRead: len(decls)}
	if len(decls) == 0 {
		return nil, census, fmt.Errorf(
			"обход пуст: ни одного объявления чарта не прочитано — вердикт беспредметен")
	}

	byName := map[string][]chartNameDecl{}
	byPath := map[string]chartNameDecl{}
	for _, d := range decls {
		if d.Name == "" {
			return nil, census, fmt.Errorf(
				"%s не называет имени чарта — группировать по имени нечем", d.Path)
		}
		byName[d.Name] = append(byName[d.Name], d)
		byPath[d.Path] = d
		if d.Distribution != "" || d.NameTwin != "" {
			census.DeclsFound++
		}
	}
	census.NamesTotal = len(byName)

	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)

	var findings []string
	for _, name := range names {
		group := byName[name]
		sort.Slice(group, func(i, j int) bool { return group[i].Path < group[j].Path })

		if len(group) == 1 {
			// САМОИСТЕЧЕНИЕ: имя адресует, различать не от чего. Оставшееся
			// объявление прощает вперёд ту находку, ради которой держатель заведён.
			d := group[0]
			if d.Distribution != "" || d.NameTwin != "" {
				findings = append(findings, fmt.Sprintf(
					"  %s: имя %q в дереве ОДНО, а чарт всё ещё объявляет различие "+
						"(distribution=%q nameTwin=%q). Объявлению нечего различать — снимите его: "+
						"запись без предмета читается как действующее ограничение и переживёт свою причину.",
					d.Path, name, d.Distribution, d.NameTwin))
			}
			continue
		}

		census.NamesColliding++
		seenDist := map[string]string{}
		for _, d := range group {
			switch d.Distribution {
			case "":
				findings = append(findings, fmt.Sprintf(
					"  %s: имя %q носят %d чарта, и этот НЕ объявляет назначения поставки "+
						"(annotations.distribution: %s|%s). Имя перестало быть адресом, а признака, "+
						"которым его вернуть, нет: предикат по имени выберет оба, и вердикт будет "+
						"вынесен не по тому чарту.",
					d.Path, name, len(group), chartDistributionStandalone, chartDistributionPlatform))
			case chartDistributionStandalone, chartDistributionPlatform:
				if other, dup := seenDist[d.Distribution]; dup {
					findings = append(findings, fmt.Sprintf(
						"  %s: пара (имя %q · назначение %q) не уникальна — то же объявляет %s. "+
							"Предикат по этой паре снова выбирает два чарта, то есть не адресует.",
						d.Path, name, d.Distribution, other))
				} else {
					seenDist[d.Distribution] = d.Path
				}
			default:
				findings = append(findings, fmt.Sprintf(
					"  %s: назначение поставки %q не из закрытого словаря (%s|%s). Значение вне "+
						"словаря ничего не адресует, а выглядит объявлением.",
					d.Path, d.Distribution, chartDistributionStandalone, chartDistributionPlatform))
			}

			switch {
			case d.NameTwin == "":
				findings = append(findings, fmt.Sprintf(
					"  %s: чарт-тёзка не назван (annotations.nameTwin). Тот, кто открыл этот файл, "+
						"о втором чарте с именем %q не узнает ниоткуда — ровно так вердикт и "+
						"выносят не по тому чарту.", d.Path, name))
			case !exists(d.NameTwin):
				findings = append(findings, fmt.Sprintf(
					"  %s: nameTwin=%q в дереве не резолвится. Мёртвая координата хуже отсутствующей: "+
						"её читают как живое утверждение.", d.Path, d.NameTwin))
			default:
				twin, known := byPath[d.NameTwin]
				switch {
				case d.NameTwin == d.Path:
					// Ссылка на себя проходит взаимность ТОЖДЕСТВЕННО, поэтому
					// проверяется отдельно: без этого случая запись «мой тёзка —
					// я сам» была бы зелёной, а тёзка остался бы неназванным.
					findings = append(findings, fmt.Sprintf(
						"  %s: nameTwin называет ЭТОТ ЖЕ чарт. Тёзка — другой файл; ссылка на "+
							"себя удовлетворяет взаимность тождественно и не называет никого.",
						d.Path))
				case !known:
					findings = append(findings, fmt.Sprintf(
						"  %s: nameTwin=%q не значится чартом в переписи — назван файл, который "+
							"чартом не является.", d.Path, d.NameTwin))
				case twin.Name != name:
					findings = append(findings, fmt.Sprintf(
						"  %s: nameTwin=%q носит имя %q, а не %q — назван не тёзка, и различать "+
							"этой записью нечего.", d.Path, d.NameTwin, twin.Name, name))
				case twin.NameTwin != d.Path:
					findings = append(findings, fmt.Sprintf(
						"  %s: тёзка %s называет своим тёзкой %q, а не этот чарт. Односторонняя "+
							"запись оставляет половину ловушки: открывший ВТОРОЙ файл о первом "+
							"по-прежнему не узнает.", d.Path, d.NameTwin, twin.NameTwin))
				}
			}
		}
	}
	return findings, census, nil
}

// trackedChartDecls — объявления всех отслеживаемых чартов дерева.
func trackedChartDecls(t *testing.T) ([]chartNameDecl, map[string]bool) {
	t.Helper()

	// Помощник, а не прямой вызов: `GIT_DIR` в окружении СИЛЬНЕЕ рабочего
	// каталога, поэтому `-C` сам по себе репозитория не выбирает. Прогон из хука
	// отправки наследует эту переменную, и обход ушёл бы в чужое дерево.
	out, err := gitenv.Command(repoRoot, "ls-files", "-z", "--", "*Chart.yaml").Output()
	if err != nil {
		t.Fatalf("индекс git не прочитан (%v) — обход не состоялся, и это НЕ чистое дерево", err)
	}
	paths := []string{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	tracked := map[string]bool{}
	decls := make([]chartNameDecl, 0, len(paths))
	for _, rel := range paths {
		tracked[rel] = true
		raw, readErr := os.ReadFile(filepath.Join(repoRoot, rel))
		if readErr != nil {
			t.Fatalf("%s отслеживается индексом, но не читается: %v", rel, readErr)
		}
		var doc struct {
			Name        string            `yaml:"name"`
			Annotations map[string]string `yaml:"annotations"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s не разбирается как YAML: %v", rel, err)
		}
		decls = append(decls, chartNameDecl{
			Path:         rel,
			Name:         doc.Name,
			Distribution: doc.Annotations["distribution"],
			NameTwin:     doc.Annotations["nameTwin"],
		})
	}
	return decls, tracked
}

func TestChartNameAddressesExactlyOneChart(t *testing.T) {
	decls, tracked := trackedChartDecls(t)

	findings, census, err := auditChartNameAddressing(decls, func(p string) bool { return tracked[p] })
	if err != nil {
		t.Fatalf("обход не состоялся: %v", err)
	}
	t.Logf("перепись: %s · находок %d", census, len(findings))

	if len(findings) != 0 {
		t.Fatalf("имя чарта не адресует, и различие не объявлено:\n%s", strings.Join(findings, "\n"))
	}
}

// ЗДЕСЬ СТОЯЛ ПРОГОН САМОГО ПРЕДИКАТА «чарт продукта» — СНЯТ ВМЕСТЕ С ПРЕДМЕТОМ.
//
// Он утверждал, что предикат «имя kaname И назначение standalone» выбирает РОВНО
// ОДИН чарт, и что это `services/iam/deploy/Chart.yaml`. Продукт вынесен
// собственным репозиторием, чарт уехал вместе с ним, назначения `standalone` в
// дереве не осталось ни у одного чарта — предикат стал беспредметным, а его
// зелёное относилось бы к другому дереву.
//
// КЛАСС, ради которого файл заведён, ОСТАЁТСЯ выше и от снятия не зависит:
// TestChartNameAddressesExactlyOneChart читает ВСЕ чарты дерева и требует, чтобы
// имя адресовало ровно один, а всякое различие тёзок было объявлено ДАННЫМИ. Он
// же и уронил прогон на объявлении, пережившем своего тёзку.
