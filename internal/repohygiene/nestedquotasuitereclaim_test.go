// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// Набор сквозных проб, заводящий ребёнка под ВЛОЖЕННЫМ потолком в ЧУЖОМ
// посеянном родителе, обязан объявлять его снятие.
//
// ПРЕДМЕТ. Вложенный потолок считает «сколько детей помещается в ОДНОМ
// родителе» (`vpc.network.subnet` — умолчание 16). Родитель, посеянный до
// прогона и общий для всех кейсов набора, живёт ДОЛЬШЕ любого из них: каждый
// незакрытый ребёнок остаётся в нём навсегда. Набор, объявляющий больше созданий,
// чем снятий, упирается в предел BY CONSTRUCTION — не «иногда под нагрузкой», а
// всегда, начиная с того кейса, который перешагнул величину.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ. Отказ приходит ПРЯМЫМ `QUOTA_EXCEEDED` ровно
// одному кейсу — тому, что оказался семнадцатым, — а красным становится всё, что
// стояло на несозданном ребёнке: незахваченные переменные, уборка по
// несуществующему предмету, ложные 403/404. Виновником в отчёте выглядит
// невиновный, и порядок кейсов решает, кто им окажется. Такой прогон читается как
// «набор нестабилен», а не как «набор течёт»; на боевом прогоне это дало 58 прямых
// отказов и 278 падений-следствий. Единственное место, где утечка видна ДО
// прогона, — объявленные шаги.
//
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ: по каждой сгенерированной коллекции число УСПЕШНЫХ
// созданий ребёнка в ПОСЕЯННОМ родителе не превышает числа объявленных снятий
// того же вида.
//
// ТРИ РАЗЛИЧЕНИЯ, БЕЗ КОТОРЫХ ГЕЙТ МЕРИЛ БЫ НЕ ТО (каждое проверено замером по
// дереву, а не предположено):
//
//	1. РОДИТЕЛЬ ПОСЕЯН, а не заведён набором. Ребёнок, чей родитель создаётся тем
//	   же набором, уходит вместе с ним: счётчик вложенного потолка снимается со
//	   снятием родителя. Признак структурный — переменную родителя набор нигде не
//	   ЗАХВАТЫВАЕТ (`pm.environment.set`), то есть она приходит из посева.
//	   Без этого различения гейт назвал бы находкой `listener.postman_collection`
//	   (32 создания слушателя против 18 снятий) — а там родитель `{{nlbId}}`
//	   заводится и сносится покейсно, и течь нечему.
//	2. СОЗДАНИЕ УСПЕШНОЕ, а не отвергаемое. Кейс, утверждающий отказ (создание в
//	   заведомо негодном родителе), не создаёт НИЧЕГО и потолка не занимает.
//	   Замер: все 11 таких шагов дерева утверждают 4xx и адресуют `garbage*`-родителя.
//	   Симметрично и на стороне снятия: шаг, утверждающий ТОЛЬКО отказ, ничего не
//	   удаляет и находку не закрывает. Уборка `oneOf([200, 400, …])` в счёт идёт —
//	   200 в ней есть, а прочие коды суть терпимость к лагу, не к неудаче.
//	3. СНЯТИЕ АДРЕСУЕТ ЭКЗЕМПЛЯР, а не коллекцию и не ВНУКА. `DELETE` по самой базе
//	   снятием ребёнка не является; `DELETE`, спускающийся в под-коллекцию ребёнка
//	   (`…/repositories/app/tags/v1`), снимает внука. Длиной пути их не различить:
//	   имя репозитория само содержит косые черты — поэтому под-коллекции названы.
//
// ЧЕГО ГЕЙТ НЕ ДЕЛАЕТ. Он читает ОБЪЯВЛЕННЫЕ шаги сгенерированных коллекций — то,
// что реально исполняется, — а не исходники кейсов: между ними стоит генератор, и
// свойство требуется от его выхода. Он не судит о РАНТАЙМЕ: снятие, объявленное и
// отказавшее, — предмет вердикта прогона, а не переписи дерева.

// nestedChildEndpoint — координаты РЕБЁНКА вложенного вида на крае.
type nestedChildEndpoint struct {
	// Base — СЕГМЕНТНЫЙ ОБРАЗЕЦ базы ребёнка. `POST <Base>` заводит его,
	// `DELETE <Base>/<хвост>` снимает. Сегмент `{parent}` означает «здесь стоит
	// родитель»: у части видов он назван путём, а не телом.
	Base string
	// ParentField — поле тела создания, называющее родителя. Читается ТОЛЬКО когда
	// родителя нет в пути. Именно значение родителя решает, посеян он или заведён
	// набором.
	ParentField string
	// SubCollections — собственные под-коллекции ребёнка. Нужны, потому что снятие
	// ребёнка и снятие его ВНУКА отличаются не длиной пути: имя репозитория само
	// содержит косые черты, поэтому `…/repositories/del/svc-1` — ребёнок, а
	// `…/repositories/app/tags/v1` — внук. Считать внука снятием ребёнка значило бы
	// закрывать находку шагом, который ребёнка не трогает.
	SubCollections []string
}

// nestedChildEndpoints — вложенный вид каталога -> координаты его ребёнка.
//
// Таблица ЯВНАЯ, а не выведенная из имени вида. Вывести её нельзя: имена
// нерегулярны в обе стороны — `vpc.network.subnet` даёт `/vpc/v1/subnets`
// (единственное число во виде, множественное в пути), а
// `loadbalancer.networkLoadBalancers.listeners` — `/nlb/v1/listeners` (служба
// названа одним словом во виде и другим в пути, обе части уже во множественном).
// Деривация разбором строки промахнулась бы молча и превратила бы гейт в
// тождественно-истинный: не найдя ни одного шага по выдуманной базе, он объявил
// бы «находок 0».
//
// Полнота таблицы держится ОТДЕЛЬНЫМ утверждением
// (`TestNestedChildEndpointsCoverTheCatalogue`) в обе стороны: новый вложенный вид
// каталога краснит гейт, пока ему не названы координаты, а запись, чей вид из
// каталога ушёл, — находка, а не безобидный остаток.
var nestedChildEndpoints = map[string]nestedChildEndpoint{
	"vpc.network.subnet":        {Base: "/vpc/v1/subnets", ParentField: "networkId"},
	"vpc.network.routeTable":    {Base: "/vpc/v1/routeTables", ParentField: "networkId"},
	"vpc.network.securityGroup": {Base: "/vpc/v1/securityGroups", ParentField: "networkId"},
	// Шестого вида — `vpc.subnet.networkInterface` — в таблице НЕТ намеренно, и это
	// не пропуск: эта же линия сняла его с контракта миграцией
	// `353001_withdraw_nic_per_subnet_kind.sql`. Число интерфейсов в подсети
	// ограничено её адресным пространством, отказ по исчерпанию уже реализован, и
	// второй предел поверх конечного ресурса не защищал бы ничего.
	//
	// Проверка полноты этой таблицы двусторонняя, поэтому запись здесь стала бы
	// находкой САМА — «координаты пережили свой предмет», — а на стволе, где
	// снятия ещё нет, её отсутствие краснеет прямой стороной. Обе стороны верны:
	// таблица обязана сходиться с каталогом ТОГО дерева, на котором гейт стоит.
	"loadbalancer.networkLoadBalancers.listeners": {
		Base: "/nlb/v1/listeners", ParentField: "loadBalancerId",
	},
	// Удостоверения принципала (`PRO-Robotech/kacho#1191`). Родитель назван
	// сегментом пути у обоих: у человека это его строка, у служебной учётки —
	// она сама. Под-коллекций у удостоверения нет — оно лист.
	"iam.user.credential": {
		Base: "/iam/v1/users/{parent}/tokens",
	},
	"iam.serviceAccount.credential": {
		Base: "/iam/v1/serviceAccounts/{parent}/keys",
	},

	// Родитель назван ПУТЁМ, а не телом, и у ребёнка есть своя под-коллекция.
	"registry.registries.repositories": {
		Base:           "/registry/v1/registries/{parent}/repositories",
		SubCollections: []string{"tags"},
	},
}

// TestNestedChildEndpointsCoverTheCatalogue — таблица координат сходится с
// каталогом вложенных видов В ОБЕ СТОРОНЫ.
//
// Прямая сторона: вложенный вид без координат невидим главному гейту, и его
// молчание про этот вид неотличимо от чистоты. Обратная: запись, чей вид каталог
// больше не объявляет, переживает свой предмет — гейт продолжал бы стеречь
// потолок, которого нет.
func TestNestedChildEndpointsCoverTheCatalogue(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)
	declared := nestedKindsOfCatalogue(t, root)
	require.NotEmpty(t, declared,
		"вложенных видов в каталоге не найдено — предпосылка гейта сломана, и его "+
			"молчание не отличимо от согласия")

	declaredSet := map[string]bool{}
	for _, k := range declared {
		declaredSet[k] = true
	}

	for _, kind := range declared {
		ep, ok := nestedChildEndpoints[kind]
		require.Truef(t, ok,
			"вложенный вид %q объявлен каталогом, но координат его ребёнка на крае нет: "+
				"гейт утечки наборов этого вида НЕ ВИДИТ, и «находок 0» по нему означает "+
				"«не смотрели». Назови базу и поле родителя в nestedChildEndpoints", kind)
		require.Truef(t, strings.HasPrefix(ep.Base, "/"),
			"вид %q: база %q обязана быть путём края", kind, ep.Base)
		// Родитель обязан быть НАЗВАН — путём либо телом. Без него посеянного
		// родителя от заведённого набором не отличить, и гейт по этому виду
		// считал бы находкой всякое создание.
		require.Truef(t,
			strings.Contains(ep.Base, nestedParentPlaceholder) || ep.ParentField != "",
			"вид %q: родитель не назван ни сегментом %s в базе, ни полем тела — "+
				"посеянного родителя от собственного не отличить",
			kind, nestedParentPlaceholder)
	}
	for kind := range nestedChildEndpoints {
		require.Truef(t, declaredSet[kind],
			"координаты %q пережили свой предмет: каталог такого вложенного вида не "+
				"объявляет. Запись обязана уйти вместе с видом", kind)
	}

	t.Logf("перепись: вложенных видов каталога %d (%s); координат объявлено %d",
		len(declared), strings.Join(declared, ", "), len(nestedChildEndpoints))
}

// ─────────────────────────────────────────────────────────────────────────────
// Разбор коллекции
// ─────────────────────────────────────────────────────────────────────────────

type newmanStep struct {
	Name    string `json:"name"`
	Request *struct {
		Method string `json:"method"`
		URL    struct {
			Path []string `json:"path"`
		} `json:"url"`
		Body *struct {
			Raw string `json:"raw"`
		} `json:"body"`
	} `json:"request"`
	Event []struct {
		Script struct {
			Exec []string `json:"exec"`
		} `json:"script"`
	} `json:"event"`
	Item []newmanStep `json:"item"`
}

type newmanCollection struct {
	Item []newmanStep `json:"item"`
}

type nestedReclaimFinding struct {
	File    string
	Kind    string
	Parent  string
	Created int
	Deleted int
	Where   []string
}

type nestedReclaimCensus struct {
	Files    int
	Steps    int
	Kinds    int
	Creates  int // успешные создания в посеянном родителе
	Refusals int // создания, утверждающие отказ (потолка не занимают)
	OwnPar   int // создания в родителе, заведённом самим набором
	Deletes  int
	// Unparsed — шаги, утверждающие про код ответа формой, которую разбор не читает.
	// Слепая зона обязана быть НАЗВАНА числом, а не растворена в «находок 0».
	Unparsed int
}

var (
	envSetRe = regexp.MustCompile(`environment\.set\(\s*['"](\w+)['"]`)

	// ГРАНИЦА СТЕЙТМЕНТА — `;`, ТА ЖЕ, ЧТО У ГЕНЕРАТОРА (`PRO-Robotech/kacho#1278`).
	//
	// Один предмет — «какие коды шаг объявляет приемлемым исходом» — разбирают ДВА
	// механизма: `_accepted_http_codes` в `scripts/gen.py` каждого набора (решает,
	// оборачивать ли шаг ограниченным ретраем) и этот разбор (решает, создал ли шаг
	// ребёнка). Пока границы у них разные, фраза «шаг утверждает отказ» означает у
	// них РАЗНОЕ, и расхождение приезжает находкой об утечке там, где её нет.
	//
	// Прежняя редакция читала ПОСТРОЧНО, и это было неверно дважды:
	//   - перенос ВНУТРИ выражения (генератор сам пишет
	//     `pm.expect(pm.response.code, pm.response.text())` с продолжением на
	//     следующей строке) не читался вовсе — шаг, утверждающий 400, считался
	//     успешным созданием;
	//   - привязки к `pm.response.code` не было, поэтому соседний стейтмент о
	//     gRPC-коде тела (`pm.expect(j.code).to.eql(200)`) подмешивался в набор
	//     HTTP-исходов и делал отвергаемое создание «успешным».
	//
	// ОБЕ формы равенства, а не одна: замер по дереву — `to.eql(` 6725 вхождений,
	// `to.equal(` 724. Отрицание (`to.not.eql(`, `to.not.equal(`) принятием НЕ
	// является и сюда не попадает by construction: между `.to.` и написанием
	// равенства допускается только `be.`.
	//
	// Согласие двух сторон держится ОБЩИМ ИСТОЧНИКОМ ожиданий —
	// `testdata/response_code_assertion_forms.json`; им судятся обе
	// (`responsecodeformparity_test.go`), поэтому разойтись молча они больше не
	// могут.
	respCodeEqRe  = regexp.MustCompile(`pm\.response\.code[^;]*?\.to\.(?:be\.)?(?:eql|equal)\((\d{3})\)`)
	respCodeOneOf = regexp.MustCompile(`pm\.response\.code[^;]*?\.to\.be\.oneOf\(\[([0-9,\s]+)]\)`)
	respCodeAnyRe = regexp.MustCompile(`\d{3}`)
)

// flattenSteps разворачивает папки коллекции в плоский список шагов, ДОСТРАИВАЯ
// имя шага именем папки-кейса.
//
// Без этого координата находки нечитаема: генератор зовёт шаги одинаково («post»,
// «cleanup»), и перечень «post, post, post» не приводит читателя к предмету —
// а координата, не приводящая к предмету, ничем не лучше её отсутствия.
func flattenSteps(items []newmanStep, prefix string, out *[]newmanStep) {
	for i := range items {
		if len(items[i].Item) > 0 {
			flattenSteps(items[i].Item, joinCase(prefix, items[i].Name), out)
			continue
		}
		s := items[i]
		s.Name = joinCase(prefix, s.Name)
		*out = append(*out, s)
	}
}

func joinCase(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func stepScriptLines(s newmanStep) []string {
	var out []string
	for _, ev := range s.Event {
		out = append(out, ev.Script.Exec...)
	}
	return out
}

// stepAssertsSuccess — утверждает ли шаг УСПЕХ создания.
//
// Шаг, чьи утверждения перечисляют только отказы, ресурса не создаёт и потолка не
// занимает. Шаг без утверждений о коде считается создающим: молчание — не
// доказательство отказа, и ошибаться здесь надо в сторону находки.
func stepAssertsSuccess(s newmanStep) (success, unparsed bool) {
	body := strings.Join(stepScriptLines(s), "\n")
	codes := acceptedResponseCodes(body)
	sawCodeAssertion := strings.Contains(body, "pm.response.code")
	// Шаг УТВЕРЖДАЕТ про код, но формой, которую разбор не читает. Считать его
	// успешным безопасно (ошибаться надо в сторону находки), но МОЛЧА этого делать
	// нельзя: так заводится слепая зона, и «находок 0» по ней означает «не
	// смотрели». Перепись такие шаги называет числом.
	if sawCodeAssertion && len(codes) == 0 {
		return true, true
	}
	if len(codes) == 0 {
		return true, false
	}
	return codes[200], false
}

// acceptedResponseCodes — HTTP-коды, которые шаг объявляет приемлемым исходом.
//
// ЯДРО, общее для гейта дерева и для пробы согласия с генератором: проба,
// повторяющая логику своей копией, доказывала бы свойство копии.
//
// Разбор идёт по ТЕКСТУ ЦЕЛИКОМ, а не построчно: границей стейтмента служит `;`
// (см. блок регулярок выше). Ожидания по каждой форме объявлены в
// `testdata/response_code_assertion_forms.json` — общем источнике для этой
// стороны и для `_accepted_http_codes` генераторов.
func acceptedResponseCodes(body string) map[int]bool {
	codes := map[int]bool{}
	for _, m := range respCodeEqRe.FindAllStringSubmatch(body, -1) {
		codes[atoiSafe(m[1])] = true
	}
	for _, m := range respCodeOneOf.FindAllStringSubmatch(body, -1) {
		for _, c := range respCodeAnyRe.FindAllString(m[1], -1) {
			codes[atoiSafe(c)] = true
		}
	}
	// Числа вне диапазона HTTP-статусов исходом не являются — та же отсечка, что у
	// генератора. Без неё перечень gRPC-кодов, записанный формой `oneOf`, попал бы
	// в набор HTTP-исходов.
	for c := range codes {
		if c < 100 || c > 599 {
			delete(codes, c)
		}
	}
	return codes
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// auditNestedQuotaReclaim — ЧИСТОЕ ядро гейта: та же функция, что гоняет проба
// инъекции. Проба, повторяющая логику своей копией, доказывала бы свойство копии.
func auditNestedQuotaReclaim(
	collections map[string]string,
	endpoints map[string]nestedChildEndpoint,
) (nestedReclaimCensus, []nestedReclaimFinding, error) {
	census := nestedReclaimCensus{Kinds: len(endpoints)}
	var findings []nestedReclaimFinding

	for _, file := range sortedKeys(collections) {
		var col newmanCollection
		if err := json.Unmarshal([]byte(collections[file]), &col); err != nil {
			return census, nil, fmt.Errorf("%s: %w", file, err)
		}
		census.Files++

		var steps []newmanStep
		flattenSteps(col.Item, "", &steps)
		census.Steps += len(steps)

		// Переменные, которые набор ЗАХВАТЫВАЕТ сам: их родитель заведён здесь же.
		captured := map[string]bool{}
		for _, s := range steps {
			for _, ln := range stepScriptLines(s) {
				for _, m := range envSetRe.FindAllStringSubmatch(ln, -1) {
					captured[m[1]] = true
				}
			}
		}

		for _, kind := range sortedKeys(endpoints) {
			ep := endpoints[kind]
			want := strings.Split(strings.Trim(ep.Base, "/"), "/")
			var parentRe *regexp.Regexp
			if ep.ParentField != "" {
				parentRe = regexp.MustCompile(`"` + regexp.QuoteMeta(ep.ParentField) + `"\s*:\s*"\{\{(\w+)\}\}"`)
			}
			sub := map[string]bool{}
			for _, s := range ep.SubCollections {
				sub[s] = true
			}

			created := map[string]int{}
			var where []string
			deleted := 0

			for _, s := range steps {
				if s.Request == nil {
					continue
				}
				segs := s.Request.URL.Path
				switch {
				case s.Request.Method == "POST" && matchPattern(segs, want) && len(segs) == len(want):
					parent, ok := parentOf(segs, want, s, parentRe)
					if !ok {
						continue // родитель не назван переменной — посеянности не установить
					}
					if captured[parent] {
						census.OwnPar++
						continue
					}
					ok, unparsed := stepAssertsSuccess(s)
					if unparsed {
						census.Unparsed++
					}
					if !ok {
						census.Refusals++
						continue
					}
					census.Creates++
					created[parent]++
					if len(where) < 3 {
						where = append(where, s.Name)
					}
				case s.Request.Method == "DELETE" && len(segs) > len(want) && matchPattern(segs[:len(want)], want):
					// Снятие ВНУКА ребёнком не является (см. SubCollections), а снятие,
					// утверждающее только отказ, ничего не удаляет — симметрично созданию.
					delOK, delUnparsed := stepAssertsSuccess(s)
					if delUnparsed {
						census.Unparsed++
					}
					if hasAnySegment(segs[len(want):], sub) || !delOK {
						continue
					}
					deleted++
					census.Deletes++
				}
			}

			total := 0
			for _, n := range created {
				total += n
			}
			if total > deleted {
				findings = append(findings, nestedReclaimFinding{
					File: file, Kind: kind, Parent: strings.Join(sortedKeys(created), ", "),
					Created: total, Deleted: deleted, Where: where,
				})
			}
		}
	}
	return census, findings, nil
}

// nestedParentPlaceholder — сегмент образца, на месте которого стоит родитель.
const nestedParentPlaceholder = "{parent}"

// matchPattern — совпадение сегментов с образцом; `{parent}` матчит любой сегмент.
func matchPattern(segs, pattern []string) bool {
	if len(segs) != len(pattern) {
		return false
	}
	for i := range pattern {
		if pattern[i] == nestedParentPlaceholder {
			continue
		}
		if segs[i] != pattern[i] {
			return false
		}
	}
	return true
}

// parentOf — имя переменной родителя: из ПУТИ, если образец его там называет,
// иначе из поля тела. Литерал (не `{{…}}`) переменной не является: посеянность по
// нему не устанавливается, и такой шаг в счёт не идёт.
func parentOf(segs, pattern []string, s newmanStep, bodyRe *regexp.Regexp) (string, bool) {
	for i, p := range pattern {
		if p != nestedParentPlaceholder {
			continue
		}
		if m := regexp.MustCompile(`^\{\{(\w+)}}$`).FindStringSubmatch(segs[i]); m != nil {
			return m[1], true
		}
		return "", false
	}
	if bodyRe == nil || s.Request.Body == nil {
		return "", false
	}
	if m := bodyRe.FindStringSubmatch(s.Request.Body.Raw); m != nil {
		return m[1], true
	}
	return "", false
}

func hasAnySegment(segs []string, set map[string]bool) bool {
	for _, s := range segs {
		if set[s] {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Прогон по дереву
// ─────────────────────────────────────────────────────────────────────────────

func newmanCollectionSources(t *testing.T, root string) map[string]string {
	t.Helper()

	out, err := gitenv.Command(root, "ls-files").Output()
	require.NoError(t, err,
		"git ls-files: перепись невозможна, а без переписи «ноль находок» неотличимо "+
			"от «ноль прочитанного»")

	sources := map[string]string{}
	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !strings.HasSuffix(rel, ".postman_collection.json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		require.NoErrorf(t, err, "%s: состав коллекций неизвестен, вердикт был бы "+
			"утверждением ни о чём", rel)
		sources[rel] = string(b)
	}
	return sources
}

// TestSeededParentChildrenAreReclaimedBySuites — гейт класса.
func TestSeededParentChildrenAreReclaimedBySuites(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)
	sources := newmanCollectionSources(t, root)

	// Предпосылка гейта. Коллекции могли переехать, расширение — смениться; в обоих
	// случаях зелёный вердикт ниже был бы получен даром. Беспредметность — находка,
	// а не успех.
	require.NotEmpty(t, sources,
		"сгенерированных коллекций не найдено — гейт беспредметен. «Ноль находок» "+
			"здесь неотличимо от «ноль прочитанного»")
	require.NotEmpty(t, nestedChildEndpoints,
		"координат вложенных детей не объявлено — стеречь нечего")

	census, findings, err := auditNestedQuotaReclaim(sources, nestedChildEndpoints)
	require.NoError(t, err, "разбор коллекции")

	// Предпосылка дискриминатора. Если ни одного создания в посеянном родителе не
	// распознано, делить нечего, и молчание гейта означало бы поломку разбора, а не
	// чистоту наборов.
	require.Positivef(t, census.Creates,
		"на %d коллекциях (%d шагов) не распознано НИ ОДНОГО создания ребёнка в "+
			"посеянном родителе — разбор сломан либо координаты разошлись с краем. "+
			"Всякая утечка была бы объявлена чистотой", census.Files, census.Steps)

	var lines []string
	for _, f := range findings {
		lines = append(lines, fmt.Sprintf(
			"%s\n    вид %s: успешных созданий в ПОСЕЯННОМ родителе {{%s}} — %d, "+
				"объявленных снятий — %d (не снимается %d)\n    напр.: %s",
			f.File, f.Kind, f.Parent, f.Created, f.Deleted, f.Created-f.Deleted,
			strings.Join(f.Where, ", ")))
	}

	// Пустой перечень — ЦЕЛЬ, а не поломка: гейт объявляет перепись и проходит.
	t.Logf("перепись: коллекций прочитано %d, шагов обойдено %d, видов под стражей %d; "+
		"созданий в посеянном родителе %d, в собственном %d (не в счёт), "+
		"утверждающих отказ %d (не в счёт), объявленных снятий %d, "+
		"утверждений о коде неразобранной формы %d; находок %d",
		census.Files, census.Steps, census.Kinds, census.Creates, census.OwnPar,
		census.Refusals, census.Deletes, census.Unparsed, len(findings))

	require.Emptyf(t, findings,
		"набор заводит ребёнка под вложенным потолком в ЧУЖОМ посеянном родителе и не "+
			"объявляет его снятия. Посеянный родитель живёт дольше кейса, поэтому "+
			"незакрытый ребёнок остаётся в нём навсегда: набор упирается в предел "+
			"by construction, а красным становится не виновный кейс, а тот, что стоял "+
			"на несозданном ребёнке.\nОбразец снятия — services/nlb/tests/newman/cases/"+
			"listener.py::_cleanup_lb (условие СТРУКТУРНОЕ: шаг не появляется там, где "+
			"предмета нет; уборка best-effort и НИКОГДА не роняет кейс).\n%s",
		strings.Join(lines, "\n"))
}
