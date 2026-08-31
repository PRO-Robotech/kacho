// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// schemaversionreader.go — инвентарь: у объявленной точки невозврата обязан
// быть ЧИТАТЕЛЬ на пути старта, и он обязан быть у КАЖДОГО сервиса.
//
// # Предмет
//
// Соседний гейт (`schemarollbackform.go`, задача #1690) делает точку невозврата
// машинночитаемой и сам называет свою границу: «он производитель отказа на
// КОММИТЕ, а не на откате… читателя у объявления на пути старта сегодня нет
// вовсе». Объявление без потребителя — форма без содержания, ровно тот класс,
// который корпус ловит в коде.
//
// Читателем стал `pkg/schemaguard` (задача #1734). Этот гейт держит второе:
// читатель ПРОВЯЗАН у каждого сервиса, чей образ несёт встроенный набор
// миграций. Провязка у шести из семи — не «почти сделано», а слепая зона,
// неотличимая снаружи от сделанного: под седьмого сервиса объявляется готовым
// на схеме, которую обслужить не может, и получает трафик.
//
// # Единица счёта
//
// Субъект — СЕРВИС, у которого есть встроенный набор миграций
// (`services/<svc>/internal/migrations/*.sql`): именно наличие набора означает,
// что мигратор при раскате двигает схему, а значит образ может разойтись с ней.
// Сервис без набора субъектом не является и в знаменатель не идёт.
//
// Признак провязки — упоминание пакета в НЕ-ТЕСТОВОМ коде композиционного корня
// (`services/<svc>/cmd/**`). Именно корень: он единственное место провязки
// (`architecture.md`), и провязка в другом слое была бы отдельной находкой.
//
// # Чего гейт НЕ утверждает — названо, чтобы не читалось шире
//
//   - что чекер попадает в АГРЕГАТОР готовности, а не просто создан рядом. Это
//     свойство исполнения, и его держат пробы самих сервисов; гейт судит дерево;
//   - что вердикт верен: за это отвечают пробы `pkg/schemaguard`;
//   - что мигратор вообще запускается: это свойство профиля развёртывания.
//
// Способность упасть и смолчать доказана инъекцией —
// schemaversionreader_injection_test.go.
package repohygiene

import (
	"fmt"
	"sort"
	"strings"
)

// schemaGuardPackage — путь пакета-читателя. Признаком служит ИМПОРТ, а не имя
// функции: имя функции переживёт переименование молча, а импорт — нет.
const schemaGuardPackage = "github.com/PRO-Robotech/kacho/pkg/schemaguard"

// schemaReaderCensus — объём осмотренного. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного», поэтому печатаются ОБЕ величины: сколько сервисов
// несут набор миграций и сколько из них провязали читателя.
type schemaReaderCensus struct {
	Services       int
	WithMigrations int
	Wired          int
	RootFiles      int
}

func (c schemaReaderCensus) String() string {
	return fmt.Sprintf(
		"сервисов осмотрено %d · несут встроенный набор миграций %d · провязали читателя %d · "+
			"файлов композиционных корней прочитано %d",
		c.Services, c.WithMigrations, c.Wired, c.RootFiles)
}

// schemaReaderSource — исходник корня, поданный разбору. Разбор ОДИН на
// настоящее дерево и на инъекцию, поэтому фикстура не может оказаться
// снисходительнее того, что судит дерево.
type schemaReaderSource struct {
	Service string
	Rel     string
	Body    string
}

// findServicesMissingSchemaReader — ядро, чистое от файловой системы.
//
// withMigrations — сервисы, несущие встроенный набор; roots — не-тестовые
// исходники их композиционных корней.
func findServicesMissingSchemaReader(withMigrations []string, roots []schemaReaderSource,
) (schemaReaderCensus, []string) {
	wired := map[string]bool{}
	census := schemaReaderCensus{RootFiles: len(roots)}
	for _, r := range roots {
		if strings.Contains(r.Body, schemaGuardPackage) {
			wired[r.Service] = true
		}
	}

	subjects := append([]string(nil), withMigrations...)
	sort.Strings(subjects)
	census.WithMigrations = len(subjects)

	var missing []string
	for _, svc := range subjects {
		if wired[svc] {
			census.Wired++
			continue
		}
		missing = append(missing, svc)
	}
	return census, missing
}

// schemaReaderFindingText — текст отказа. Называет предмет, а не симптом: на
// него пойдут чинить, и он обязан сказать, ЧТО делать.
func schemaReaderFindingText(missing []string, census schemaReaderCensus) string {
	return fmt.Sprintf(
		"сервисы несут встроенный набор миграций и НЕ провязали читателя версии схемы: %v.\n"+
			"    Мигратор идёт при каждом раскате, поэтому откат выкатки ставит прежний образ на "+
			"новую схему; без читателя такой под объявляется ГОТОВЫМ и получает трафик, а отказ "+
			"приходит клиенту на первом обращении к колонке.\n"+
			"    Чинится в композиционном корне сервиса: %s.Describe(migrations.FS) → "+
			"Set.Check(%s.PgxVersionReader(pool)) → именованный чекер готовности рядом с `database`.\n"+
			"    %s",
		missing, schemaGuardPackage, schemaGuardPackage, census)
}
