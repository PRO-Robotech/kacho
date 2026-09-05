// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// migrationVersionAny — числовой префикс имени файла миграции. Форма ОДНА, а
// длина разная, и это несущее различие: 14 цифр — метка времени заведения,
// 4 и 6 — унаследованные номера, выведенные из номера задачи.
//
// Прежняя редакция знала только 14 и 6, и всё, названное ЧЕТЫРЬМЯ цифрами,
// было вне наблюдения: не нарушением, которое гейт разрешил, а предметом,
// которого он не видел. Перепись по дереву: 14 цифр — 17 файлов, 6 — 20,
// 4 — 169 (`git ls-files '*/migrations/*.sql'`).
var migrationVersionAny = regexp.MustCompile(`^(\d+)_`)

// acceptedMigrationForm — ПРИНИМАЮЩАЯ форма имени, объявленная одним местом.
//
// Она отличается от [migrationVersionAny] предметом, а не строгостью: та читает
// числовой префикс ЛЮБОЙ ширины (иначе унаследованные номера остались бы вне
// наблюдения), эта говорит, какая форма принимается. Объявление живёт внутри
// функции присваиванием, потому что соседний гейт
// `TestMigrationFormIsDeclaredInOnePlace` читает канон РАЗБОРОМ этого файла и
// узнаёт принимающую регулярку по имени `timestamped` — вынос в пакетную
// переменную сделал бы его предпосылку ложной молча.
func acceptedMigrationForm() *regexp.Regexp {
	timestamped := regexp.MustCompile(`^(\d{14})_`)
	return timestamped
}

// addedMigrationCensus — объём осмотренного. Печатается ВСЕГДА и ДВУМЯ
// величинами: одно число («добавлено N») скрывает ровно тот случай, ради
// которого гейт заведён, — добавленное есть, а сравнивать его не с чем.
type addedMigrationCensus struct {
	Added    int
	Compared int
	Highest  map[string]string
}

func (c addedMigrationCensus) String() string {
	dirs := make([]string, 0, len(c.Highest))
	for dir := range c.Highest {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	var parts []string
	for _, dir := range dirs {
		parts = append(parts, fmt.Sprintf("%s → старшая применённая %s", dir, c.Highest[dir]))
	}
	if len(parts) == 0 {
		parts = append(parts, "каталогов с добавленным нет")
	}
	return fmt.Sprintf("добавлено миграций %d · сравнено с каталогом %d · %s",
		c.Added, c.Compared, strings.Join(parts, " · "))
}

// migrationVersionOf — числовое значение версии файла миграции и её ширина.
//
// Ширина возвращается вместе со значением: по ней отличают метку времени от
// унаследованного номера, а сравнивать их можно и так — четырнадцать цифр
// больше шести и четырёх при любом содержании.
func migrationVersionOf(name string) (version int64, width int, ok bool) {
	m := migrationVersionAny.FindStringSubmatch(name)
	if m == nil {
		return 0, 0, false
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return v, len(m[1]), true
}

// addedMigrationIsSQL — путь принадлежит каталогу миграций.
func addedMigrationIsSQL(rel string) bool {
	return strings.HasSuffix(rel, ".sql") && strings.Contains(rel, "/migrations/")
}

// auditAddedMigrationVersions — разбор. Возвращает находки списком: разбор,
// роняющий пробу изнутри, инъекции не поддаётся.
//
// `added` — миграции, добавленные относительно ствола. `tracked` — состав
// ИНДЕКСА (не диска): вердикт обязан быть свойством коммита, а не рабочего
// каталога прогоняющего.
func auditAddedMigrationVersions(added, tracked []string) ([]string, addedMigrationCensus) {
	census := addedMigrationCensus{Highest: map[string]string{}}
	accepted := acceptedMigrationForm()

	// Старшая применённая версия КАЖДОГО каталога — по индексу, за вычетом
	// добавленного: сравнивать новое с самим собой нечего.
	isAdded := map[string]bool{}
	for _, rel := range added {
		isAdded[rel] = true
	}
	type peak struct {
		version int64
		name    string
	}
	highest := map[string]peak{}
	for _, rel := range tracked {
		if !addedMigrationIsSQL(rel) || isAdded[rel] {
			continue
		}
		name := filepath.Base(rel)
		v, _, ok := migrationVersionOf(name)
		if !ok {
			continue
		}
		dir := filepath.Dir(rel)
		if cur, seen := highest[dir]; !seen || v > cur.version {
			highest[dir] = peak{version: v, name: name}
		}
	}

	var findings []string
	for _, rel := range added {
		if !addedMigrationIsSQL(rel) {
			continue
		}
		census.Added++
		dir := filepath.Dir(rel)
		name := filepath.Base(rel)
		if top, ok := highest[dir]; ok {
			census.Highest[dir] = top.name
		}

		version, width, ok := migrationVersionOf(name)
		if !ok {
			findings = append(findings, rel+" — номер не разобран: ждали метку времени YYYYMMDDHHMMSS")
			continue
		}
		if !accepted.MatchString(name) {
			findings = append(findings, fmt.Sprintf("%s — номер %0*d выведен из номера задачи, "+
				"а он МЕНЬШЕ уже применённых: мигратор такую версию не применит и уронит старт. "+
				"Новая миграция именуется меткой времени заведения (YYYYMMDDHHMMSS)",
				rel, width, version))
			continue
		}

		// ЗНАЧЕНИЕ, а не только форма. Посылка формы — «часы идут вперёд, поэтому
		// метка больше любого унаследованного номера» — на живом каталоге не
		// выполняется: метка, взятая не в UTC, оказывается ниже уже лежащей, и
		// накат идёт не в том порядке, в каком миграции заводились.
		top, ok := highest[dir]
		if !ok {
			// Каталог без унаследованных версий — законное состояние нового
			// домена: сравнивать не с чем by construction.
			continue
		}
		census.Compared++
		if version > top.version {
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"%s — номер %d НЕ БОЛЬШЕ уже применённого %s (%d) в том же каталоге. "+
				"Мигратор такую версию не применит и уронит старт: «found N missing migrations "+
				"before current version». Метка обязана быть строго больше старшей применённой — "+
				"часы дают её не всегда (метка не в UTC оказывается ниже уже лежащей); "+
				"возьмите номер на секунду выше старшей и назовите отступление в шапке миграции",
			rel, version, top.name, top.version))
	}

	return findings, census
}

// TestNewMigrationOutranksEveryAppliedOne — номер НОВОЙ миграции больше любого
// уже применённого.
//
// # Предмет: номер по задаче монотонности не даёт
//
// Номер выводится из номера задачи (`<задача><порядковый>`), и это решает своё —
// две линии не соревнуются за слот в каталоге. Но задачи берутся не по
// возрастанию: задача с номером НИЖЕ максимального применённого даёт версию
// меньше текущей версии базы.
//
// Мигратор такую миграцию не применяет и роняет старт: «found N missing
// migrations before current version». Свежая база получает одну схему,
// существующая — другую, и расходятся они МОЛЧА: стенд поднимается, боевой
// кластер не мигрирует.
//
// Наблюдалось при работе над задачей #703: клейм строки требовал колонки-аренды,
// номер встал бы ниже применённого, и вместо клейма был взят замок прохода —
// приём слабее по существу, выбранный ограничением нумерации, а не задачей.
//
// # Решение: метка времени заведения
//
// Версия новой миграции — `YYYYMMDDHHMMSS`. Она:
//
//   - по-прежнему ВЫВОДИТСЯ, а не выбирается из каталога, — то есть свойство,
//     ради которого заводился номер по задаче, сохраняется: две линии не смотрят
//     на один и тот же последний файл;
//   - совпадение двух линий до секунды ловит существующая проверка
//     уникальности номера — это её предмет, и здесь он не дублируется.
//
// # Что проверяет ЭТА проверка — ФОРМУ и ЗНАЧЕНИЕ, а не только форму
//
// Прежняя редакция судила только форму имени (четырнадцать цифр) и была зелена
// на ОБОИХ номерах — и на том, что превосходит все применённые, и на том, что
// ниже соседнего. Посылка формы — «часы идут вперёд, поэтому метка больше любого
// унаследованного номера» — на живом каталоге НЕ ВЫПОЛНЯЕТСЯ: в нём лежала метка
// на семь часов впереди UTC, и исполнитель, честно взявший `date -u`, получил
// номер ниже соседнего. Совпадение двух номеров ловится уникальностью ДО
// слияния; немонотонность не ловило ничто (#1895).
//
// Поэтому судится ЗНАЧЕНИЕ: всякая добавленная относительно ствола миграция
// строго больше каждой применённой В СВОЁМ каталоге. Версии нумеруются по
// каталогу, поэтому чужая старшая в сравнение не входит.
//
// Унаследованные шестизначные файлы остаются как есть — переномеровать
// применённое нельзя, и правило действует ВПЕРЁД.
//
// Состав индекса берётся у git, а не с диска: вердикт обязан быть свойством
// коммита, а не рабочего каталога прогоняющего.
//
// Граница названа честно и ОТДЕЛЕНА от отказа предпосылки: ствол, который не
// разрешается в клоне с рабочим деревом git, — это настройка клона (мелкий
// checkout), а не отсутствие предмета, и такой исход обязан быть красным. Разбор
// и цена — [requireTrunkRef].
func TestNewMigrationOutranksEveryAppliedOne(t *testing.T) {
	root := repoRoot(t)
	base := requireTrunkRef(t, root)

	out, err := gitenv.Command(root, "diff", "--name-only",
		"--diff-filter=A", base+"...HEAD").Output()
	if err != nil {
		t.Fatalf("состав добавленного относительно %s не прочитан: %v — это отказ "+
			"предпосылки, а не пустой список", base, err)
	}
	var added []string
	for _, rel := range strings.Split(string(out), "\n") {
		if rel = strings.TrimSpace(rel); rel != "" {
			added = append(added, rel)
		}
	}

	indexed, err := gitenv.Command(root, "ls-files", "--", "*/migrations/*.sql").Output()
	if err != nil {
		t.Fatalf("состав индекса не прочитан: %v — это отказ предпосылки, а не пустой список", err)
	}
	var tracked []string
	for _, rel := range strings.Split(string(indexed), "\n") {
		if rel = strings.TrimSpace(rel); rel != "" {
			tracked = append(tracked, rel)
		}
	}
	if len(tracked) == 0 {
		t.Fatalf("обход пуст: индекс не назвал НИ ОДНОЙ миграции — сравнивать не с чем, "+
			"и «находок 0» здесь неотличимо от «прочитано 0» (корень %s)", root)
	}

	findings, census := auditAddedMigrationVersions(added, tracked)

	t.Logf("относительно %s: %s; миграций в индексе %d", base, census, len(tracked))
	for _, f := range findings {
		t.Error(f)
	}
}
