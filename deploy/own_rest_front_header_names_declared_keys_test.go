// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_rest_front_header_names_declared_keys_test.go — координата значений,
// названная шапкой скрипта, обязана существовать в ОБЪЯВЛЕНИИ посадки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Шапка скрипта, разрешающего адрес собственного REST-фронта службы, объясняет
// «адрес читается, а не выписывается» и называет ключи чарта, которыми посадка
// объявляет номер порта и транспорт слушателя. Названный ключ — это указание
// «иди сюда», и оно проверяемо ровно так же, как координата файла.
//
// Расхождение здесь МОЛЧИТ вдвойне. Во-первых, скрипт этих ключей не читает
// вовсе: он спрашивает у кластера отрендеренные объекты (порт Service — ПО
// ИМЕНИ, транспорт — по переменной процесса в Deployment), поэтому неверная
// координата не ломает ни одного его прогона. Во-вторых, ключ пишется точкой, а
// не косой чертой, поэтому ни один обходчик путей его не судит.
//
// Наблюдалось (#2208): шапка называла `mtls.httpListeners.publicRest` и
// `mtls.httpListeners.internalRest`. Такого вложения в дереве нет —
// одноимённый ключ `mtls.httpListeners` СКАЛЯР (общая ручка транспорта HTTP),
// а полистенные ручки лежат уровнем выше, соседями этого скаляра:
// `mtls.publicRest`, `mtls.internalRest`. То есть комментарий описывал
// механизм, которого у его собственного файла нет, и посылал читателя во
// вложение, которого не существует.
//
// Класс известен и в этом же дереве уже чинился: у `values.dev-prod.yaml`
// комментарий утверждал `mtls.httpListeners=true` прямо над строкой
// `httpListeners: false` — два места об одном предмете, из которых верно одно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ КООРДИНАТОЙ ЗНАЧЕНИЙ
//
// Токен в обратных кавычках из двух и более сегментов через точку, где каждый
// сегмент — слово в нижневерблюжьей записи. Форма выбрана по дереву, а не по
// памяти, и она отсекает соседей, которые в шапке тоже стоят в кавычках:
//
//	values.dev-prod.yaml   дефис и расширение файла — не ключ
//	service-{public,…}     фигурные скобки и косая черта — путь шаблона
//	KANAME_…_MTLS_ENABLE   переменная процесса: верхний регистр и подчёркивание
//	http-rest              имя порта: дефис, точки нет вовсе
//
// Ноль распознанных координат — ОТКАЗ, а не успех: шапка могла перестать их
// называть, и тогда проверка снимается ВМЕСТЕ с предметом, а не зеленеет молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГДЕ КООРДИНАТА РЕЗОЛВИТСЯ
//
// В объявлениях, а не в рендере, — та же дисциплина, что у соседних
// posture_parity_test.go и dbtls_declaration_test.go. Источников два вида:
// значения самого чарта (корень) и профили зонта (поддерево `kaname`).
// Достаточно ОДНОГО: полистенная ручка закомментирована в чарте намеренно
// («пусто ⇒ берётся общая ручка выше») и объявлена боевыми профилями.
//
// Отказ различает два случая, и различие несущее: «ключа нет нигде» — это
// опечатка, а «родитель оказался скаляром» — это ровно тот дефект, ради
// которого проверка заведена: вложение невыразимо BY CONSTRUCTION, и никакой
// профиль его не объявит.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ownRestFrontScript — файл, чью шапку судит эта проверка.
const ownRestFrontScript = "scripts/own-rest-front-address.py"

// kanameChartValues — значения чарта службы: корень объявления.
const kanameChartValues = umbrellaDir + "/charts/kaname/values.yaml"

// kanameUmbrellaSubtree — ключ, под которым профили зонта объявляют службу.
const kanameUmbrellaSubtree = "kaname"

// valuesKeyRe — координата значений: два и более сегмента через точку, каждый в
// нижневерблюжьей записи. Якорена с обоих концов: содержимое кавычек обязано
// БЫТЬ координатой целиком, а не содержать её.
var valuesKeyRe = regexp.MustCompile(`^[a-z][A-Za-z0-9]*(?:\.[a-z][A-Za-z0-9]*)+$`)

// fileNameSuffixes — последние сегменты, по которым токен есть ИМЯ ФАЙЛА, а не
// координата ключа. Перечень нужен потому, что имя файла профиля устроено ровно
// как ключ: точки, нижний регистр, никаких иных знаков.
//
// Полоса найдена СВОЕЙ ЖЕ инъекцией, а не чтением: `values.dev-prod.yaml` в
// настоящей шапке отсекался дефисом, и распознаватель выглядел исправным. Тот
// же файл без дефиса (`values.prod.yaml`) он объявлял координатой и требовал от
// неё резолва — то есть находка родилась бы из формы записи чужого имени.
var fileNameSuffixes = [...]string{"yaml", "yml", "json", "py", "go", "sh", "md", "txt", "tf"}

func namesAFile(tok string) bool {
	last := tok[strings.LastIndex(tok, ".")+1:]
	for _, ext := range fileNameSuffixes {
		if last == ext {
			return true
		}
	}
	return false
}

// backtickRe — содержимое обратных кавычек.
var backtickRe = regexp.MustCompile("`([^`]+)`")

type valuesKeyFinding struct {
	key    string
	reason string
}

func (f valuesKeyFinding) String() string { return fmt.Sprintf("`%s` — %s", f.key, f.reason) }

// resolveValuesKey идёт по карте объявления сегмент за сегментом.
//
// Три исхода, и второй не равен третьему: ключ найден · путь оборвался на
// НЕ-карте (вложение невыразимо) · ключа такого нет.
func resolveValuesKey(decl map[string]any, key string) (found bool, scalarParent string) {
	var node any = decl
	segs := strings.Split(key, ".")
	for i, seg := range segs {
		m, ok := node.(map[string]any)
		if !ok {
			return false, strings.Join(segs[:i], ".")
		}
		node, ok = m[seg]
		if !ok {
			return false, ""
		}
	}
	return true, ""
}

// valuesKeyCensus — объём осмотренного. Печатается всегда: «находок ноль»
// обязано быть отличимо от «прочитано ноль».
type valuesKeyCensus struct {
	quoted   int // токенов в обратных кавычках
	keys     int // из них признано координатой значений
	distinct int // различных координат
	decls    int // объявлений посадки прочитано
}

func (c valuesKeyCensus) String() string {
	return fmt.Sprintf("токенов в кавычках %d · координат значений распознано %d "+
		"(различных %d) · объявлений прочитано %d", c.quoted, c.keys, c.distinct, c.decls)
}

// scanHeaderValuesKeys разбирает ПРОИЗВОЛЬНЫЙ текст против ПРОИЗВОЛЬНОГО набора
// объявлений: настоящая шапка и синтетический ввод инъекции проходят одну и ту
// же функцию, поэтому доказанное на втором верно для первого.
func scanHeaderValuesKeys(header string, decls map[string]map[string]any) (valuesKeyCensus, []valuesKeyFinding) {
	census := valuesKeyCensus{decls: len(decls)}
	seen := map[string]bool{}
	var order []string
	for _, m := range backtickRe.FindAllStringSubmatch(header, -1) {
		census.quoted++
		tok := m[1]
		if !valuesKeyRe.MatchString(tok) || namesAFile(tok) {
			continue
		}
		census.keys++
		if !seen[tok] {
			seen[tok] = true
			order = append(order, tok)
		}
	}
	census.distinct = len(order)

	var findings []valuesKeyFinding
	for _, key := range order {
		resolved := false
		scalarAt := ""
		for _, src := range sortedDeclSources(decls) {
			ok, sp := resolveValuesKey(decls[src], key)
			if ok {
				resolved = true
				break
			}
			if sp != "" && scalarAt == "" {
				scalarAt = fmt.Sprintf("`%s` в %s — не карта, вложение невыразимо", sp, src)
			}
		}
		if resolved {
			continue
		}
		if scalarAt != "" {
			findings = append(findings, valuesKeyFinding{key, scalarAt})
			continue
		}
		findings = append(findings, valuesKeyFinding{key, "ключа нет ни в одном объявлении посадки"})
	}
	return census, findings
}

// ownRestFrontDecls — объявления посадки, в которых координата вправе
// резолвиться: значения самого чарта (корень) и профили зонта (поддерево
// службы). Достаточно ОДНОГО источника: полистенная ручка закомментирована в
// чарте намеренно и объявлена боевыми профилями.
func ownRestFrontDecls(t *testing.T) map[string]map[string]any {
	t.Helper()
	decls := map[string]map[string]any{
		kanameChartValues: readYAML(t, filepath.FromSlash(kanameChartValues)),
	}
	profiles, err := filepath.Glob(filepath.FromSlash(umbrellaDir + "/values*.yaml"))
	if err != nil {
		t.Fatalf("профили зонта не перечислены: %v", err)
	}
	sort.Strings(profiles)
	for _, p := range profiles {
		sub, ok := readYAML(t, p)[kanameUmbrellaSubtree].(map[string]any)
		if !ok {
			continue
		}
		decls[filepath.ToSlash(p)] = sub
	}
	if len(decls) < 2 {
		t.Fatalf("объявлений собрано %d — профили зонта не прочитаны, и «ключа нет нигде» "+
			"было бы неотличимо от «не искали»", len(decls))
	}
	return decls
}

func TestOwnRestFrontHeaderNamesDeclaredValuesKeys(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash(ownRestFrontScript))
	if err != nil {
		t.Fatalf("скрипт %s не прочитан — вердикт беспредметен: %v", ownRestFrontScript, err)
	}

	census, findings := scanHeaderValuesKeys(string(raw), ownRestFrontDecls(t))
	t.Logf("перепись: %s — %s", ownRestFrontScript, census)

	if census.distinct == 0 {
		t.Fatalf("предпосылка не выполняется: шапка %s не называет ни одной координаты "+
			"значений (токенов в кавычках %d). Либо она перестала их называть — тогда "+
			"проверка снимается вместе с предметом, — либо распознаватель ослеп на форме "+
			"записи, и тогда «находок ноль» означает «не искали»", ownRestFrontScript, census.quoted)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("шапка %s называет %d координат значений, которых посадка не объявляет:%s"+
			"\n\nНазванный ключ — указание «иди сюда», и оно проверяемо. Скрипт этих ключей "+
			"НЕ читает (он спрашивает у кластера порт Service по имени и переменную процесса "+
			"в Deployment), поэтому расхождение не ломает ни одного прогона и молчит: чинится "+
			"оно только чтением объявления.",
			ownRestFrontScript, len(findings), b.String())
	}
}

func sortedDeclSources(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
