// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iam_chart_foreign_kinds_established_test.go — ЧУЖОЙ ВИД, КОТОРЫЙ ВЕЗЁТ
// ПОСТАВЛЯЕМЫЙ ЧАРТ СЛУЖБЫ ДОСТУПА, ОБЯЗАН ЗАВОДИТЬСЯ ВЛАДЕЛЬЦЕМ ПОДЪЁМА ДО
// УСТАНОВКИ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Чарт везёт `PrometheusRule` — вид ЧУЖОГО оператора. Там, где определения вида
// в кластере нет, схема отвергает объект и отвергает вместе с ним ВСЮ
// установку, а не одну тревогу:
//
//	Error: INSTALLATION FAILED: unable to build kubernetes objects from release
//	manifest: resource mapping not found for name: "kaname" ... no matches for
//	kind "PrometheusRule" in version "monitoring.coreos.com/v1"
//	ensure CRDs are installed first
//
// Риск был НАЗВАН в шапке самого шаблона заранее — и условие под него создано
// не было: владелец подъёма (`.github/scripts/kaname-chart-boots.sh`) заводил
// шесть секретов и базу, а про оператора наблюдения не знал ВОВСЕ. Полоса
// кластера ставит чарт в ПУСТОЙ kind, и потому падала не на посадке, не на
// готовности и не на миграциях, а на первом же шаге установки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТОТ ГЕЙТ ЖИВЁТ В МОДУЛЕ ПЛАТФОРМЫ, А НЕ РЯДОМ С ЧАРТОМ
//
// Он сверяет ДВА перечня из РАЗНЫХ деревьев: что везёт чарт — дерево СЛУЖБЫ; что
// заводит владелец подъёма — дерево ПЛАТФОРМЫ (`.github/`). Значит предмет
// гейта есть свойство ПЛАТФОРМЫ, и жить он обязан здесь: отсюда видны оба
// дерева, потому что дерево службы лежит внутри платформенного.
//
// Первая редакция стояла в модуле службы и доставала скрипт ПОБЕГОМ ЗА КОРЕНЬ
// МОДУЛЯ. Служба выносится отдельным продуктом; в её самостоятельном клоне
// каталога `.github/` нет вовсе, а под чужим деревом такой путь указал бы на
// ЧУЖОЙ файл — и вердикт был бы о нём. Страж
// `TestPlatformCoordinatesTouchingTheTreeAreAnchoredInTheModule` назвал это
// находкой, и он прав: ослаблять его нельзя — он держит несущее свойство
// выноса. Закрыто ПЕРЕЕЗДОМ, как и класс 2 задачи #2532.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ УСЛОВИЕ СОЗДАЁТ ВЛАДЕЛЕЦ ПОДЪЁМА, А НЕ ЧАРТ
//
// Это ровно та роль, которую скрипт себе уже объявил: «он заводит то, что в
// чужом облаке заводит ставящий». Секреты, база и удостоверяющие центры —
// оттуда же. Определение чужого вида принадлежит той же полке: в настоящей
// установке его кладёт оператор Prometheus, а не наш чарт.
//
// Три отвергнутых хода названы, чтобы их не изобрели заново:
//
//	ПРОВЕРКА ВОЗМОЖНОСТЕЙ в шаблоне (`.Capabilities.APIVersions.Has`) — даёт
//	    ТИХИЙ ПРОПУСК, и цена измерена: `helm template` без кластера отвечает
//	    `false` ВСЕГДА (проверено на пробном чарте: без кластера `false`, с
//	    `--api-versions` `true`, с `--validate` против кластера без оператора
//	    `false`). Значит объект перестал бы рендериться у ВСЕХ проб чарта,
//	    которые зовут `helm template` без кластера, — их 10, — и сверка со
//	    страницей покраснела бы, доказывая не то, что хотела. Молчание
//	    неотличимо от исправности — тот самый класс.
//	ОТКАЗ РЕНДЕРА (`fail`) — по той же измеренной причине сработал бы во ВСЕХ
//	    десяти, потому что вне кластера возможностей не знает никто.
//	`crds/` ЧАРТА — заставил бы НАШ чарт владеть определением ЧУЖОГО оператора:
//	    helm такие определения не обновляет и не удаляет, а при настоящем
//	    операторе рядом они конфликтуют.
//
// Выключить ручку правил в самой джобе — тоже не ход: он снимает красноту,
// СУЖАЯ предикат полосы, чьё объявление — «судит ВСЁ».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ
//
//	Р1  чарт рендерится и несёт СВОИ виды — иначе классификатор ниже пуст, и
//	    «ноль чужих» означало бы «ноль прочитанного»;
//	Р2  каждый ЧУЖОЙ вид рендера заведён владельцем подъёма;
//	Р3  у каждой записи владельца ЕСТЬ ПРЕДМЕТ — вид, который чарт правда
//	    везёт. Запись, которой больше нечего заводить, — находка: она унаследует
//	    следующую слепую зону;
//	Р4  перепись печатается числами, и пустой обход роняет прогон.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПЕРЕЧЕНЬ ВЛАДЕЛЬЦА СПРАШИВАЕТСЯ ИСПОЛНЕНИЕМ, А НЕ ЧТЕНИЕМ ТЕКСТА
//
// Проба зовёт `kaname-chart-boots.sh --foreign-kinds` и читает то, что скрипт
// НАПЕЧАТАЛ. Печатает он из той же величины, которую потом и заводит, поэтому
// объявление и исполнение разойтись не могут ПО ПОСТРОЕНИЮ. Чтение исходника
// скрипта текстом дало бы второе место об одном предмете — и совпало бы с
// комментарием, объясняющим этот самый механизм.
//
// Ключ отвечает ДО опроса предпосылок и не требует ни кластера, ни демона
// контейнеров, ни сети: перечень — свойство дерева, а не стенда.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ЗАКРЫВАЕТ — сказано прямо
//
// Она судит СОГЛАСИЕ двух перечней: что чарт везёт и что владелец заводит. Она
// НЕ утверждает, что определение по названному адресу существует, что оно
// правда заводит этот вид и что выражения правил проходят его схему, — это
// решает живой прогон полосы кластера, и только он. Список встроенных групп
// ниже выписан, а не измерен: кластера у пробы нет. Группа вне списка считается
// чужой НАМЕРЕННО — ошибаться безопаснее в сторону «потребовать завести».
package deploy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// deliveredIAMChart — ПОСТАВЛЯЕМЫЙ чарт службы доступа: единственный артефакт,
// который ставящий получает отдельно. Это НЕ подчарт зонта (`iamChartDir`):
// наборы их ключей пересекаются меньше чем наполовину, и зелёный зонт о
// поставке не утверждает ничего.
const deliveredIAMChart = repoRoot + "/services/iam/deploy"

// iamBootOwnerScript — владелец подъёма поставляемого чарта. Живёт в дереве
// ПЛАТФОРМЫ, и потому адресуется от её корня, а не подъёмом из чужого модуля.
const iamBootOwnerScript = repoRoot + "/.github/scripts/kaname-chart-boots.sh"

// foreignKindsFlag — ключ, которым владелец подъёма печатает свой перечень.
const foreignKindsFlag = "--foreign-kinds"

// deliveredIAMChartProfiles — БОЕВАЯ цепочка поставки, та же, которой чарт
// ставит полоса кластера.
var deliveredIAMChartProfiles = []string{"values.yaml", "values.prod.yaml"}

// deliveredIAMOperatorCoordinates — координаты, которые чарт требует НАЗВАТЬ и
// умолчаний которым не даёт намеренно: без них рендер отказывает целиком.
var deliveredIAMOperatorCoordinates = []string{
	"image=registry.example.invalid/pro-robotech/kaname:0.1.0",
	"db.host=postgres.example.invalid",
	"db.passwordSecretName=kaname-db",
	"db.passwordSecretKey=password",
}

// builtinAPIGroups — группы, которые кластер служит САМ, без определения извне.
//
// Список ВЫПИСАН, и это его граница: кластера у пробы нет, измерить его нечем.
// Появится новая встроенная группа — проба назовёт её чужой и потребует завести;
// ошибка в эту сторону видна сразу и чинится строкой, ошибка в обратную —
// невидима и стоит прогона.
var builtinAPIGroups = map[string]bool{
	"":                             true, // core: v1
	"apps":                         true,
	"batch":                        true,
	"autoscaling":                  true,
	"policy":                       true,
	"networking.k8s.io":            true,
	"rbac.authorization.k8s.io":    true,
	"storage.k8s.io":               true,
	"apiextensions.k8s.io":         true,
	"admissionregistration.k8s.io": true,
	"apiregistration.k8s.io":       true,
	"authentication.k8s.io":        true,
	"authorization.k8s.io":         true,
	"certificates.k8s.io":          true,
	"coordination.k8s.io":          true,
	"discovery.k8s.io":             true,
	"events.k8s.io":                true,
	"flowcontrol.apiserver.k8s.io": true,
	"node.k8s.io":                  true,
	"scheduling.k8s.io":            true,
	"resource.k8s.io":              true,
}

// apiGroupOf — группа из `apiVersion`. У встроенного ядра (`v1`) группа пуста.
func apiGroupOf(apiVersion string) string {
	if i := strings.Index(apiVersion, "/"); i >= 0 {
		return apiVersion[:i]
	}
	return ""
}

// renderedAPIVersions — `apiVersion` каждого объекта рендера.
//
// Берётся ключ ВЕРХНЕГО УРОВНЯ документа: `apiVersion` встречается и внутри
// тел (в шаблонах пода, в ссылках владельца), и счёт по подстроке считал бы
// вложенное объектом.
func renderedAPIVersions(rendered string) []string {
	var out []string
	for _, doc := range strings.Split(rendered, "\n---") {
		var api, kind string
		for _, line := range strings.Split(doc, "\n") {
			switch {
			case strings.HasPrefix(line, "apiVersion:"):
				api = strings.TrimSpace(strings.TrimPrefix(line, "apiVersion:"))
			case strings.HasPrefix(line, "kind:"):
				kind = strings.TrimSpace(strings.TrimPrefix(line, "kind:"))
			}
		}
		if api != "" && kind != "" {
			out = append(out, api)
		}
	}
	return out
}

// renderDeliveredIAMChart рендерит ПОСТАВЛЯЕМЫЙ чарт боевой цепочкой.
//
// Отсутствие helm в CI — жёсткий провал, а не пропуск: гейт, молча ставший
// инертным на джобе, гейтящей мёрж, гейтом не является. Та же дисциплина, что у
// соседей по каталогу.
func renderDeliveredIAMChart(t *testing.T, sets ...string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — вердикта НЕТ: третья категория, не зелёное и не красное")
	}
	args := []string{"template", "kaname", deliveredIAMChart, "-n", "kaname"}
	for _, f := range deliveredIAMChartProfiles {
		args = append(args, "-f", filepath.Join(deliveredIAMChart, f))
	}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	require.NoErrorf(t, err,
		"поставляемый чарт не отрендерился цепочкой %v: это НЕ вердикт гейта, а "+
			"«не выполнилось» — третья категория, и в успех она не засчитывается\n%s",
		deliveredIAMChartProfiles, out)
	return string(out)
}

// bootOwnerForeignKinds — перечень, который владелец подъёма ПЕЧАТАЕТ и по
// которому же заводит виды. Возвращает `apiVersion` каждой записи.
//
// Отказ ключа — НЕ пустой перечень: пустоту проба обязана отличать от «спросить
// не удалось», иначе «владелец не заводит ничего» стало бы неотличимо от
// «владелец не умеет отвечать».
func bootOwnerForeignKinds(t *testing.T, scriptPath string, env ...string) []string {
	t.Helper()
	cmd := exec.Command("bash", scriptPath, foreignKindsFlag) // #nosec G204 -- путь из констант этого файла
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err,
		"владелец подъёма не ответил на %s — перечень чужих видов спросить нечем, "+
			"и «ничего не заводит» неотличимо от «отвечать не умеет»\n%s", foreignKindsFlag, out)

	var kinds []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "§")
		require.Lenf(t, fields, 3,
			"запись перечня разобрана в %d поля вместо трёх: %q — владелец пропустил бы её МОЛЧА",
			len(fields), line)
		for i, f := range fields {
			require.NotEmptyf(t, strings.TrimSpace(f), "поле %d записи %q пусто", i+1, line)
		}
		kinds = append(kinds, strings.TrimSpace(fields[0]))
	}
	return kinds
}

func TestIAMChartForeignKindsAreEstablishedByTheBootOwner(t *testing.T) {
	// Владелец подъёма — файл ПЛАТФОРМЫ, и его отсутствие здесь есть находка, а
	// не «третья категория»: этот модуль им владеет.
	_, err := os.Stat(iamBootOwnerScript)
	require.NoErrorf(t, err, "владельца подъёма нет по пути %s — сверять перечни не с чем",
		iamBootOwnerScript)

	// ── Р1: чарт рендерится, и классификатору есть что делить ────────────────
	rendered := renderDeliveredIAMChart(t, deliveredIAMOperatorCoordinates...)
	apiVersions := renderedAPIVersions(rendered)
	require.NotEmptyf(t, apiVersions, "рендер цепочкой %v не дал НИ ОДНОГО объекта — "+
		"обход пуст, и «чужих видов 0» означало бы «прочитано 0»", deliveredIAMChartProfiles)

	shipped := map[string]bool{}
	builtinSeen, foreign := 0, map[string]bool{}
	for _, api := range apiVersions {
		shipped[api] = true
		if builtinAPIGroups[apiGroupOf(api)] {
			builtinSeen++
			continue
		}
		foreign[api] = true
	}
	require.Positivef(t, builtinSeen,
		"среди %d объектов рендера НЕТ НИ ОДНОГО встроенного вида — так не бывает "+
			"у работающего чарта, значит классификатор читает не то", len(apiVersions))

	established := map[string]bool{}
	for _, k := range bootOwnerForeignKinds(t, iamBootOwnerScript) {
		established[k] = true
	}

	// ── Р2: каждый чужой вид поставки заведён владельцем подъёма ─────────────
	var unestablished []string
	for api := range foreign {
		if !established[api] {
			unestablished = append(unestablished, api)
		}
	}
	sort.Strings(unestablished)
	require.Emptyf(t, unestablished,
		"чарт везёт чужие виды, которых владелец подъёма НЕ ЗАВОДИТ: %v.\n"+
			"В пустом кластере схема отвергнет объект и вместе с ним ВСЮ установку — "+
			"не одну тревогу. Условие создаётся в %s, а не выключением ручки: "+
			"выключенная ручка сужает предикат джобы, чьё объявление — «судит ВСЁ»",
		unestablished, iamBootOwnerScript)

	// ── Р3: у каждой записи владельца есть ПРЕДМЕТ ──────────────────────────
	var stale []string
	for api := range established {
		if !foreign[api] {
			stale = append(stale, api)
		}
	}
	sort.Strings(stale)
	require.Emptyf(t, stale,
		"владелец подъёма заводит виды, которых чарт БОЛЬШЕ НЕ ВЕЗЁТ: %v — "+
			"записи нечего заводить, и она унаследует следующую слепую зону",
		stale)

	// ── Р4: перепись ────────────────────────────────────────────────────────
	t.Logf("перепись: объектов рендера %d · видов %d · встроенных вхождений %d · "+
		"чужих видов %d · заведено владельцем подъёма %d · находок 0",
		len(apiVersions), len(shipped), builtinSeen, len(foreign), len(established))
}
