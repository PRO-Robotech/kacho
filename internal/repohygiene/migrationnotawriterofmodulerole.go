// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationnotawriterofmodulerole.go — разбор миграций и манифестов для Г2
// (приёмка `services/iam/docs/engineering/acceptance/roles-come-as-data-not-migrations.md`
// §3.4, §6; сценарий MOD-RD-23).
//
// # Предмет
//
// Роль модуля, у которого ЕСТЬ манифест, пишет применитель. Миграция, вставившая
// её же, заводит ВТОРОГО писателя одного предмета: два места разойдутся молча —
// правка манифеста не доедет до строки, потому что строку держит миграция, а
// перепись применителя об этом ничего не скажет.
//
// # Перечень модулей с манифестом ВЫВОДИТСЯ, а ведомость НЕ заводится
//
// Приёмка §3.4 предлагала ведомость «модули без манифеста» с самоистечением
// (её Г6), а §10 п. 3 требовала назвать её носитель до кодирования. Носитель
// назван и он ДРУГОЙ: перечень выводится из дерева.
//
// Довод измерен, а не предпочтён. Манифестов в дереве продукта сегодня НОЛЬ
// (их производитель — задача #1091), значит выписанная ведомость перечисляла бы
// ВЕСЬ закрытый набор модулей и не несла бы ни одного бита сведений сверх
// предиката «манифеста нет». А главное — она стала бы вторым местом об одном
// предмете и разошлась бы с деревом молча, ровно в тот день, когда первый
// манифест появится. Производный перечень истекает BY CONSTRUCTION: запись
// исчезает вместе с появлением манифеста, и истекать вручную нечему.
//
// # Как узнаётся манифест — по СОДЕРЖИМОМУ, а не по пути
//
// Где именно лягут манифесты модулей, решает #1091. Гейт, привязанный к
// угаданному пути, молчал бы вечно, если путь окажется другим, — и его молчание
// было бы неотличимо от чистоты. Поэтому манифест опознаётся парой ключей
// верхнего уровня: `apiVersion: iam/v1` и `module: <член закрытого набора>`.
//
// # Чего разбор НЕ видит — названо
//
//  1. **фикстуры проб** (`testdata/`, `*_test*`) — они объявляют `module: vpc` и
//     сделали бы vpc «модулем с манифестом», превратив каждую его миграцию в
//     находку. Исключены по каталогу, и это единственное исключение;
//  2. **манифест, приезжающий не файлом дерева** (карта настроек, том) — его в
//     дереве нет by construction, и гейт о нём ничего не утверждает;
//  3. **имя роли, записанное литералом идентификатора** (`'rol000000000sysadmin'`)
//     — таких в дереве два, и обе роли вне закрытого набора модулей, то есть
//     находкой быть не могут ни при каком манифесте.
package repohygiene

import (
	"regexp"
	"strings"
)

// ModuleManifestSite — манифест, найденный в дереве.
type ModuleManifestSite struct {
	File   string
	Module string
}

// MigrationRoleSite — роль, вставляемая миграцией.
type MigrationRoleSite struct {
	File string
	Name string
	// Owner — первый сегмент имени: модуль-владелец либо пусто.
	Owner string
}

// MigrationRoleCensus — объём осмотренного.
type MigrationRoleCensus struct {
	// Blocks — блоков `INSERT INTO kacho_iam.roles` прочитано.
	Blocks int
	// Names — имён ролей извлечено.
	Names int
}

var (
	// manifestAPIVersionRe — ключ оболочки манифеста верхнего уровня.
	manifestAPIVersionRe = regexp.MustCompile(`(?m)^apiVersion:\s*["']?iam/v1["']?\s*$`)
	// manifestModuleRe — имя модуля в оболочке манифеста верхнего уровня.
	manifestModuleRe = regexp.MustCompile(`(?m)^module:\s*["']?([a-z][a-z0-9-]*)["']?\s*$`)
	// roleInsertRe — начало блока вставки роли.
	roleInsertRe = regexp.MustCompile(`INSERT\s+INTO\s+kacho_iam\.roles\b`)
	// roleSeedNameRe — имя роли как аргумент деривации идентификатора.
	roleSeedNameRe = regexp.MustCompile(`md5\('([^']+)'\)`)
)

// ScanModuleManifest опознаёт манифест модуля по содержимому.
func ScanModuleManifest(path string, src []byte) (ModuleManifestSite, bool) {
	s := string(src)
	if !manifestAPIVersionRe.MatchString(s) {
		return ModuleManifestSite{}, false
	}
	m := manifestModuleRe.FindStringSubmatch(s)
	if m == nil {
		return ModuleManifestSite{}, false
	}
	return ModuleManifestSite{File: path, Module: m[1]}, true
}

// ScanMigrationRoleInserts извлекает имена ролей, вставляемых миграцией.
//
// Блок — от `INSERT INTO kacho_iam.roles` до первой строки, оканчивающейся `;`.
// Привязка к блоку обязательна: тот же образец идентификатора встречается во
// вставках в ЧУЖИЕ таблицы (селекторы, выдачи служебным учёткам), и предикат
// без привязки мерил бы упоминания идентификатора, а не строки роли.
func ScanMigrationRoleInserts(path string, src []byte) (sites []MigrationRoleSite, census MigrationRoleCensus) {
	s := string(src)
	for _, loc := range roleInsertRe.FindAllStringIndex(s, -1) {
		census.Blocks++
		block := s[loc[0]:]
		if e := regexp.MustCompile(`(?m);\s*$`).FindStringIndex(block); e != nil {
			block = block[:e[1]]
		}
		for _, m := range roleSeedNameRe.FindAllStringSubmatch(block, -1) {
			name := m[1]
			census.Names++
			owner, _, hasDot := strings.Cut(name, ".")
			if !hasDot {
				owner = ""
			}
			sites = append(sites, MigrationRoleSite{File: path, Name: name, Owner: owner})
		}
	}
	return sites, census
}
