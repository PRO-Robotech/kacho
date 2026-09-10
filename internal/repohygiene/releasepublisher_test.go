// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// releasepublisher_test.go — монорепо обязано нести ПРОИЗВОДИТЕЛЯ версии.
//
// # Предмет
//
// Решение владельца по выносу iam: вынесенный сервис ссылается на фундамент
// `pkg/` и контракты `proto/` как на ВНЕШНЮЮ зависимость — versioned Go-модуль
// `github.com/PRO-Robotech/kacho`. У этого решения сегодня нет исполнителя:
// семантических тегов на origin ноль, релизов ноль, и ни один из процессов
// конвейера тега не создаёт (замер задачи #1766, перемерен 2026-09-04 —
// `git ls-remote --tags origin | grep -cE 'refs/tags/v[0-9]'` → 0).
//
// Пиннуть при этом ЕСТЬ что: прокси уже отдаёт псевдоверсию на вершину ствола
// (`@latest` → v0.0.0-<время>-<sha>). То есть препятствие не в упаковке модуля
// и не в его сборке у чужого потребителя — оба доказаны отдельным гейтом
// `internal/release`. Препятствие в том, что ВЫРАЗИТЬ СОВМЕСТИМОСТЬ нечем:
// «бампнуть фундамент до совместимой версии» не является операцией, пока
// версий не существует, а `go get -u` не имеет смысла.
//
// # Что здесь утверждается
//
// Гейт судит ТРИ структурных факта и ни одного смыслового:
//
//  1. каждый механизм линии выпуска существует и исполняем;
//  2. у каждого есть доказательство способности упасть (`*-inject.sh`);
//  3. это доказательство ЗОВЁТСЯ прогонщиком `scripts/ci-local.sh`;
//  4. САМ МЕХАНИЗМ ЗОВЁТСЯ — целью сборки либо шагом конвейера.
//
// Третий пункт — не оформление. Доказательство, которого никто не запускает,
// не отличается от отсутствующего: оно перестаёт исполняться в тот же день,
// когда ломается, и узнать об этом неоткуда. Этот класс в дереве уже
// наблюдался и стоил отдельной починки.
//
// Четвёртый — тот же класс, поднятый на уровень выше, и он заведён по замеру.
// Из механизмов перечня вызывающего не имел РОВНО ОДИН — производитель
// поставки дерева службы: ни строки в рецепте, ни шага в объявлении конвейера.
// То есть выкладку доводил человек, вспоминая службу, адрес артефакта,
// подтверждение и три ключа, — а правило корпуса требует, чтобы у поставки был
// ПРОИЗВОДИТЕЛЬ, а не человек. Прочие восемь вызывающего несли, поэтому ось не
// тождественно-истинна и не тождественно-ложна: она разделяла перечень 8 к 1 в
// день заведения.
//
// ВЫЗОВОМ СЧИТАЕТСЯ ИСПОЛНЯЕМАЯ СТРОКА, А НЕ УПОМИНАНИЕ. Путь механизма стоит и
// в справке этого же Makefile, и в объяснительных комментариях; гейт, считающий
// их вызовом, зеленел бы на СОБСТВЕННОМ объяснении — класс, который корпус ловит
// в продукте (`testing.md` §«Гейт на класс», п. 4).
//
// ПЕРЕЧЕНЬ МЕХАНИЗМОВ ВЫВЕДЕН ИЗ РЕШЕНИЯ, А НЕ ИЗ КАТАЛОГА, и потому растёт
// вместе с ним: механизмов стало шесть, когда у операции «поставить версию»
// появился производитель. Их имена — в `releaseArtifacts` ниже, здесь они не
// перечисляются вторично: два места об одном предмете расходятся молча, и
// расходится обычно то, которое читают чаще.
//
// # Чего гейт НЕ утверждает — названо, чтобы его не читали шире
//
// Он не судит, ВЕРНЫ ли предпосылки механизма и правильно ли он отказывает:
// это поведение, и его доказывает `publish-version-inject.sh` прогоном
// механизма на синтетических репозиториях. Он не судит, опубликована ли
// какая-нибудь версия: публикация — необратимое внешнее действие владельца,
// и гейт дерева, требующий её, был бы красным до тех пор, пока владелец не
// нажмёт, то есть блокировал бы всё остальное по причине, к дереву не
// относящейся.
//
// # Способность упасть
//
// Доказана `releasepublisher_injection_test.go`: снятие любого файла перечня и
// снятие вызова инъекции из прогонщика дают красное с координатой, а законный
// близнец — посторонний скрипт той же формы рядом — молчит. Инъекция
// параметризована тем же перечнем, поэтому новый механизм приходит в неё вместе
// со своей записью, а не отдельной правкой, которую забудут.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releaseArtifact — один предмет переписи: механизм и доказательство его падучести.
type releaseArtifact struct {
	mechanism string // путь механизма относительно корня
	injection string // путь доказательства относительно корня
	why       string // зачем он нужен — попадает в текст находки
}

// releaseArtifacts — перечень ВЫВОДИТСЯ не из дерева, а из решения: каждый
// механизм линии стоит здесь потому, что решение его назвало, и обязан
// существовать. Перечень, выведенный обходом каталога, был бы тождественно-
// истинным — он утверждал бы «что лежит, то и должно лежать» и промолчал бы на
// пустом каталоге.
//
// ЧИСЛО МЕХАНИЗМОВ ЗДЕСЬ НЕ ВЫПИСАНО, и это правка по замеру: прежняя редакция
// говорила «ровно два» при шести записях — комментарий пережил свой предмет и
// разошёлся с перечнем, который объясняет. Счёт выводится длиной перечня.
var releaseArtifacts = []releaseArtifact{
	{
		mechanism: "scripts/release/publish-version.sh",
		injection: "scripts/release/publish-version-inject.sh",
		why:       "производитель версии: проверяет предпосылки и называет единственную команду владельцу",
	},
	{
		mechanism: "scripts/release/probe-published.sh",
		injection: "scripts/release/probe-published-inject.sh",
		why:       "проба годности: собирается ли объявленная версия у внешнего потребителя",
	},
	{
		mechanism: "scripts/release/assert-trunk-green.sh",
		injection: "scripts/release/assert-trunk-green-inject.sh",
		why:       "предпосылка «тег обещает зелёное»: обязательные проверки ствола сверяются ПО ИМЕНАМ на этой ревизии",
	},
	{
		mechanism: "scripts/release/breaking-since-release.sh",
		injection: "scripts/release/breaking-since-release-inject.sh",
		why:       "точка отсчёта совместимости: дельта контрактов с последней ОПУБЛИКОВАННОЙ версии, а не со стволом",
	},
	{
		mechanism: "scripts/release/publish-tag.sh",
		injection: "scripts/release/publish-tag-inject.sh",
		why:       "производитель операции: спрашивает гейты и создаёт ссылку — единственный, кто делает необратимый шаг",
	},
	{
		mechanism: "scripts/release/summarize-run.sh",
		injection: "scripts/release/summarize-run-inject.sh",
		why:       "сводка оператору: читает факт на удалённом, а не пересказывает вход прогона",
	},
	{
		mechanism: "scripts/release/publish-service-artifact.sh",
		injection: "scripts/release/publish-service-artifact-inject.sh",
		why:       "производитель ПОСТАВКИ: дерево службы уезжает в репозиторий артефакта одним коммитом, и его выкладка перестаёт быть ручным снимком",
	},
	{
		mechanism: "scripts/release/assert-no-module-reciprocity.sh",
		injection: "scripts/release/assert-no-module-reciprocity-inject.sh",
		why:       "предпосылка «порядок выпуска существует»: граф ОБЪЯВЛЕННЫХ модулей ацикличен — мерится require/replace, а не подстрокой в пути импорта",
	},
	{
		mechanism: "scripts/release/assert-pin-reachable.sh",
		injection: "scripts/release/assert-pin-reachable-inject.sh",
		why:       "предпосылка шага 4 порядка выпуска: ревизия пина ЗАКРЕПЛЕНА долговечной ссылкой — стволом либо тегом, а не временной головой полосы, снятие которой уничтожает воспроизводимость из исходников (#2243)",
	},
}

// releaseAudit — исход осмотра. Возвращается СТРУКТУРОЙ, а не печатается на
// месте: инъекция обязана гонять тот же код, а не его пересказ, и для этого ей
// нужен исход в виде значения.
type releaseAudit struct {
	filesRead  int
	assertions int
	// callerLines — исполняемых строк, прочитанных в местах вызова. Величина
	// отдельная: без неё «вызывающего нет» неотличимо от «мест вызова не
	// прочитано ни одного», а это разные вердикты — второй есть поломка гейта.
	callerLines int
	findings    []string
}

// releaseCallerLines — ИСПОЛНЯЕМЫЕ строки тех мест, откуда механизм может
// зваться: рецептов сборки и объявлений конвейера.
//
// Комментарии отсеиваются, и это несущее: путь механизма стоит в справке того же
// рецепта и в объяснительных абзацах рядом. Считая их вызовом, гейт объявил бы
// вызванным всё, о чём хоть где-то написано, — и остался бы зелёным ровно в том
// случае, ради которого заведён.
func releaseCallerLines(sources map[string]string) []string {
	var out []string
	for _, body := range sources {
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			out = append(out, line)
		}
	}
	return out
}

// releaseIsCalled — зовётся ли путь хоть одной исполняемой строкой. Сверка идёт
// по пути ЦЕЛИКОМ, а не по базовому имени: одноимённый файл в другом каталоге
// вызовом этого механизма не является.
func releaseIsCalled(callers []string, mechanism string) bool {
	for _, line := range callers {
		if strings.Contains(line, mechanism) {
			return true
		}
	}
	return false
}

// auditReleasePublisher — единственная реализация осмотра. Ею пользуются и
// гейт, и его инъекция; расхождение между ними невозможно by construction.
func auditReleasePublisher(root, runner string, callers []string) releaseAudit {
	var a releaseAudit
	a.callerLines = len(callers)
	for _, art := range releaseArtifacts {
		for _, rel := range []string{art.mechanism, art.injection} {
			a.assertions++
			info, err := os.Stat(filepath.Join(root, rel))
			if err != nil {
				a.findings = append(a.findings, rel+": нет в дереве — "+art.why)
				continue
			}
			a.filesRead++
			a.assertions++
			if info.Mode().Perm()&0o111 == 0 {
				a.findings = append(a.findings, rel+": не исполняем (режим "+info.Mode().Perm().String()+")")
			}
		}

		// Доказательство обязано ЗВАТЬСЯ. Сверка идёт по пути инъекции целиком,
		// а не по её базовому имени: одноимённый файл в другом каталоге вызовом
		// этого доказательства не является.
		a.assertions++
		if !strings.Contains(runner, art.injection) {
			a.findings = append(a.findings,
				art.injection+": прогонщик scripts/ci-local.sh его не зовёт — доказательство падучести не исполняется")
		}

		// Механизм обязан ЗВАТЬСЯ. Без вызывающего он не отличается от
		// отсутствующего: его никто не прогоняет, поэтому он ломается молча, а
		// работа, ради которой он заведён, снова достаётся человеку.
		a.assertions++
		if !releaseIsCalled(callers, art.mechanism) {
			a.findings = append(a.findings,
				art.mechanism+": звать его нечем — ни цели сборки, ни шага конвейера не исполняет этот путь. "+
					art.why+". Заведи вызывающего: механизм без него не отличается от отсутствующего")
		}
	}
	return a
}

func TestMonorepoCarriesAVersionPublisher(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	runnerPath := filepath.Join(root, "scripts", "ci-local.sh")
	runnerRaw, err := os.ReadFile(runnerPath)
	if err != nil {
		t.Fatalf("прогонщик не прочитан (%s): %v — вердикта нет ни по одному предмету", runnerPath, err)
	}

	// МЕСТА ВЫЗОВА собираются из дерева, а не выписываются: рукописный перечень
	// разошёлся бы с деревом молча — класс, который в этом репозитории уже
	// наблюдался тремя копиями одного списка.
	sources := map[string]string{}
	makefileRaw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("Makefile не прочитан: %v — о вызывающих вердикта нет", err)
	}
	sources["Makefile"] = string(makefileRaw)

	wfDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("объявления конвейера не прочитаны (%s): %v — о вызывающих вердикта нет", wfDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(wfDir, e.Name()))
		if err != nil {
			t.Fatalf("объявление %s не прочитано: %v", e.Name(), err)
		}
		sources[e.Name()] = string(raw)
	}

	callers := releaseCallerLines(sources)
	a := auditReleasePublisher(root, string(runnerRaw), callers)

	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного». Пустой обход — красное, а не тихий успех.
	t.Logf("перепись: предметов выпуска %d, файлов прочитано %d, мест вызова прочитано %d "+
		"(источников %d), утверждений %d, находок %d",
		len(releaseArtifacts), a.filesRead, a.callerLines, len(sources), a.assertions, len(a.findings))

	if len(releaseArtifacts) == 0 || a.assertions == 0 {
		t.Fatalf("обход пуст: предметов %d, утверждений %d — вердикт беспредметен",
			len(releaseArtifacts), a.assertions)
	}
	// Ноль исполняемых строк означал бы, что ось «механизм зовётся» судит
	// пустоту: она объявила бы невызванными ВСЕ механизмы, и красное пришло бы
	// от поломки обхода, а не от дерева.
	if a.callerLines == 0 {
		t.Fatalf("мест вызова прочитано ноль при %d источниках — ось «механизм зовётся» беспредметна",
			len(sources))
	}
	for _, f := range a.findings {
		t.Errorf("производитель версии: %s", f)
	}
}
