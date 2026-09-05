// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationnotawriterofmodulerole_test.go — держатель Г2: миграция не
// вставляет роль модуля, у которого есть манифест (сценарий MOD-RD-23).
//
// # ГЕЙТ СУДИТ ДОБАВЛЕННЫЕ МИГРАЦИИ, А НЕ ВСЁ ДЕРЕВО
//
// Так требует его собственная APPROVED-приёмка, и требует в ТРЁХ местах сразу:
// сценарий MOD-RD-23 начинается словами «Дано НОВАЯ миграция»; §6 называет
// держателем «гейт дерева ПО ДОБАВЛЕННЫМ ФАЙЛАМ МИГРАЦИЙ (форма — как у
// TestNewMigrationCitesAnApprovedAcceptance, П16)»; §7 задаёт инъекцию
// «ДОБАВЛЕННАЯ миграция со вставкой роли модуля с манифестом».
//
// Первая редакция обходила всё дерево — то есть была шире своего объявления по
// каждому из трёх мест, и шире В СТОРОНУ СТРОГОСТИ, поэтому выглядела исправной.
// Пока манифестов в дереве было ноль, разница не наблюдалась вовсе: популяция
// пуста, гейт выходил рано. Первый же манифест её обнажил — 53 находки, ни одна
// из которых не исполнима.
//
// # ПОЧЕМУ ИСТОРИЧЕСКИЕ МИГРАЦИИ НЕ НАХОДКА — И ЭТО НЕ ПОСЛАБЛЕНИЕ
//
// Роли модулей пишут ПРИМЕНЁННЫЕ миграции, а применённую миграцию не правят
// (ban #5): «починка» здесь означала бы правку того, что уже стоит в боевой
// базе, и мигратор её не заметил бы вовсе. Требовать её значило бы держать
// ствол красным за прошлое, которого правкой не изменить, — дословно тот довод,
// которым П16 обосновывает свою границу.
//
// Переезд уже записанных строк к применителю — предмет ОТДЕЛЬНОЙ задачи
// продукта #1891, названной прозой каждого манифеста дерева. Здесь он не
// молчание, а ЧИСЛО: перепись печатает исторический остаток на каждом прогоне.
//
// # ВЕДОМОСТЬ ИСТОРИЧЕСКИХ СТРОК РАССМОТРЕНА И ОТВЕРГНУТА
//
// Три причины, каждой хватило бы одной: (а) приёмка её не заводит — ведомость
// §6 Г6 про ДРУГОЙ предмет, «модуль без манифеста»; (б) ведомость ТОЧНЫМ числом
// краснела бы на каждом шаге #1891, то есть на движении К СОБСТВЕННОЙ ЦЕЛИ, а
// ПОТОЛКОМ не краснела бы никогда и потому не истекла бы; (в) она стала бы
// вторым местом об одном предмете и разошлась бы с деревом молча — довод,
// которым разбор этого же гейта уже отверг ведомость модулей.
//
// # ПЕРЕПИСЬ ИДЁТ ПО ВСЕМУ ДЕРЕВУ, А НАХОДКИ — ТОЛЬКО ПО ДОБАВЛЕННОМУ
//
// Разделение обязательно. Порог `migrationRoleCensusFloor` стережёт РАЗБОР:
// сменится форма записи имени — и гейт замолчит, не находя предмета. Считай
// перепись по добавленному, и ветка без миграций роняла бы порог, то есть гейт
// падал бы на достижении собственной цели.
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `migrationnotawriterofmodulerole_injection_test.go`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

const (
	// iamMigrationsDir — каталог миграций iam.
	iamMigrationsDir = "services/iam/internal/migrations/"
	// migrationRoleCensusFloor — блоков вставки роли, ниже которого обход
	// беспредметен: их в дереве десять, и обвал переписи означает, что разбор
	// перестал видеть предмет.
	migrationRoleCensusFloor = 5
)

// manifestBearingModules — модули, у которых манифест в дереве ЕСТЬ. Перечень
// выводится обходом, а не выписывается.
func manifestBearingModules(t *testing.T, root string, tt *trackedTree) (map[string]string, int) {
	t.Helper()
	out := map[string]string{}
	var scanned int
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".yaml") && !strings.HasSuffix(rel, ".yml") {
			continue
		}
		if strings.Contains(rel, "/testdata/") || strings.Contains(rel, "_test") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		scanned++
		if site, ok := ScanModuleManifest(rel, src); ok {
			out[site.Module] = site.File
		}
	}
	return out, scanned
}

// sitesInFiles — сайты, чей файл назван перечнем.
//
// Вынесена ЧИСТОЙ функцией намеренно: ось «судятся только добавленные»
// доказывается инъекцией на синтетическом входе, не трогая живое дерево.
func sitesInFiles(sites []MigrationRoleSite, files map[string]bool) []MigrationRoleSite {
	var out []MigrationRoleSite
	for _, s := range sites {
		if files[s.File] {
			out = append(out, s)
		}
	}
	return out
}

// addedMigrationFiles — миграции, ДОБАВЛЕННЫЕ относительно ствола.
//
// Форма — та же, что у П16 (`auditNewMigrations`): один источник состава на
// оба гейта, иначе «добавленное» разошлось бы у них молча.
func addedMigrationFiles(t *testing.T, root, base string) map[string]bool {
	t.Helper()
	out, err := gitenv.Command(root, "diff", "--name-only", "--diff-filter=A",
		base+"...HEAD").Output()
	if err != nil {
		t.Fatalf("состав добавленного относительно %s не прочитан: %v — это отказ "+
			"ПРЕДПОСЫЛКИ, а не пустой список: без него гейт судил бы дерево, а не изменение",
			base, err)
	}
	files := map[string]bool{}
	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || !strings.HasSuffix(rel, ".sql") || !strings.Contains(rel, "/migrations/") {
			continue
		}
		files[rel] = true
	}
	return files
}

// migrationRoleFindings — предикат находки. Тот же зовёт инъекция.
func migrationRoleFindings(sites []MigrationRoleSite, bearing map[string]string) []string {
	var out []string
	for _, s := range sites {
		if s.Owner == "" {
			continue
		}
		if manifestFile, ok := bearing[s.Owner]; ok {
			out = append(out, fmt.Sprintf("%s: роль %q модуля %q, чей манифест лежит в %s",
				s.File, s.Name, s.Owner, manifestFile))
		}
	}
	sort.Strings(out)
	return out
}

// TestMODRD23MigrationDoesNotWriteARoleOfAManifestBearingModule — сам гейт.
func TestMODRD23MigrationDoesNotWriteARoleOfAManifestBearingModule(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	bearing, yamlScanned := manifestBearingModules(t, root, tt)

	var (
		rels                      []string
		blocks, names, unreadable int
		sites                     []MigrationRoleSite
		parsedMigrated            int
	)
	for rel := range tt.files {
		if strings.HasPrefix(rel, iamMigrationsDir) && strings.HasSuffix(rel, ".sql") {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parsedMigrated++
		s, census := ScanMigrationRoleInserts(rel, src)
		blocks += census.Blocks
		names += census.Names
		unreadable += census.Unreadable
		sites = append(sites, s...)
	}

	// Перепись ДЕРЕВА и проверки РАЗБОРА идут ДО разрешения ствола: иначе клон
	// без ствола уходил бы в пропуск, не сказав ни числа, и «ноль находок» снова
	// стало бы неотличимо от «ноль прочитанного».
	t.Logf("перепись: файлов YAML осмотрено %d, манифестов модулей найдено %d %v; "+
		"миграций iam прочитано %d, блоков вставки роли %d, имён ролей извлечено %d, "+
		"блоков с НЕПРОЧИТАННЫМ именем %d",
		yamlScanned, len(bearing), manifestModuleNames(bearing), parsedMigrated,
		blocks, names, unreadable)

	if parsedMigrated == 0 {
		t.Fatalf("миграций iam прочитано ноль — каталог %s переехал, и гейт стережёт "+
			"координату, которой больше нет", iamMigrationsDir)
	}
	if blocks < migrationRoleCensusFloor {
		t.Fatalf("блоков вставки роли прочитано %d при пороге %d — разбор перестал видеть "+
			"предмет, и его молчание сказано ни о чём", blocks, migrationRoleCensusFloor)
	}
	if names == 0 {
		t.Fatalf("имён ролей извлечено ноль при %d блоках — форма записи имени сменилась, "+
			"и разбор МОЛЧИТ вместо того чтобы находить", blocks)
	}
	// Блок, вставляющий роль, у которого имя прочитать не удалось, — НЕВИДИМОСТЬ,
	// а не находка и не молчание: роль вставлена, а гейт о ней не судил.
	//
	// Проверка заведена 2026-09-04 вместе с чтением второй формы записи имени.
	// Прежде её место занимало `names == 0`, и оно ловило только ПОЛНУЮ слепоту:
	// свод миграций iam сменил форму разом у всех сорока восьми блоков, поэтому
	// разбор дал ровно ноль и предпосылка сработала. Смени форму ОДИН блок —
	// сорок семь имён скрыли бы сорок восьмое, и гейт остался бы зелёным.
	if unreadable > 0 {
		t.Fatalf("блоков вставки роли, из которых имя прочитать НЕ УДАЛОСЬ, %d при %d "+
			"прочитанных именах. Роль вставлена, а гейт о ней не судил: это не находка и "+
			"не молчание, а невидимость. Форма записи имени — предмет ScanMigrationRoleInserts; "+
			"неизвестную форму надо ЗАВЕСТИ в разбор, а не пропустить", unreadable, names)
	}

	if len(bearing) == 0 {
		// Популяция пуста — и это НАЗВАНО, а не выдано за проверенное. Молчание
		// гейта здесь означает «сверять не с чем», а не «расхождений нет».
		//
		// Ветвь достижима в дереве БЕЗ манифестов — отдельный клон, обход по
		// иному признаку, снятый раздел, — и снятию не подлежит: она про ИСХОД,
		// а не про сегодняшнее его значение. Здесь стояло «производитель
		// манифестов — задача #1091», то есть ссылка на неё как на незакрытую;
		// утверждение пережило свой предмет (#1907), и ветвь говорит теперь о
		// том, что видит СВОЙ прогон, а не о календаре чужой задачи.
		t.Logf("манифестов модулей в дереве НОЛЬ — популяция гейта пуста: миграция " +
			"остаётся законным писателем КАЖДОЙ роли. Это не «проверено», это «сверять не с " +
			"чем»: разбор прочитал дерево и не опознал в нём ни одного манифеста")
		return
	}

	// Состав ДОБАВЛЕННОГО — предмет находок. Ствол разрешается ЗДЕСЬ, после
	// переписи дерева и проверок разбора: его недостижимость есть отказ
	// предпосылки, и он обязан прийти отдельно от них, а не вместо них.
	base := requireTrunkRef(t, root)
	added := addedMigrationFiles(t, root, base)
	addedSites := sitesInFiles(sites, added)

	// Исторический остаток — ЧИСЛО на каждом прогоне, а не молчание. Его предмет
	// назван задачей продукта #1891 и прозой каждого манифеста; находкой он быть
	// не может, потому что применённую миграцию не правят (ban #5).
	t.Logf("добавлено относительно %s миграций %d, из них вставок роли %d; "+
		"исторический остаток: вставок роли модуля с манифестом в УЖЕ ПРИМЕНЁННЫХ "+
		"миграциях %d — правке не подлежат (ban #5), их переезд к применителю есть "+
		"задача продукта #1891",
		base, len(added), len(addedSites),
		len(migrationRoleFindings(sites, bearing))-len(migrationRoleFindings(addedSites, bearing)))

	if findings := migrationRoleFindings(addedSites, bearing); len(findings) > 0 {
		t.Fatalf("ДОБАВЛЕННАЯ миграция вставляет роль модуля, у которого ЕСТЬ манифест — "+
			"%d место(а):\n  %s\n\n"+
			"Это второй писатель одного предмета: правка манифеста до строки не доедет, "+
			"потому что строку держит миграция, и перепись применителя об этом промолчит.\n"+
			"Снятие: роль объявляется манифестом, а миграция её не вставляет.",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// manifestModuleNames — имена модулей в устойчивом порядке для переписи.
func manifestModuleNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
