// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanasyncpeerwarmup_test.go — гейт по дереву: асинхронная мутация не уносит
// ЧУЖОЙ свежий идентификатор к его владельцу первым обращением.
//
// # Предмет
//
// Обёртка окна видимости (`retry_until_authorized`) ключуется на КОДЕ ОТВЕТА шага.
// Асинхронная мутация отвечает `200` и конвертом `Operation` — в том числе когда
// владелец отказал: отказ приезжает терминальной ошибкой ВНУТРИ операции и читается
// уже другим шагом. На этой полосе обёртка сработать не может by construction.
//
// Она при этом НЕ инертна целиком, и это надо держать в голове, чтобы не чинить не
// то: отказ ШЛЮЗА по собственной цели проверки прав этого RPC приходит синхронным
// `403`, и обёртка на нём работает. Не закрыта ровно одна полоса — отказ ВЛАДЕЛЬЦА
// чужого ресурса, которого воркер (или синхронная сверка) резолвит под личностью
// вызывающего. Поэтому гейт судит НЕ о всякой свежей ссылке, а только о ЧУЖОЙ.
//
// # Предикат находки
//
// Шаг M — мутирующий глагол, чей RPC возвращает `operation.Operation` (берётся из
// контрактов, не из имени шага). В том же кейсе M ссылается на переменную V, и:
//
//  1. V опубликована РАНЬШЕ в этом же кейсе шагом, который НЕ является чтением
//     («свежая»: то, что уже прочитано успешно, окна не имеет);
//  2. V не упомянута ни одним предшествующим GET того же кейса («не прогрета»);
//  3. V — НЕ цель проверки прав самого M (`scope_extractor.from_request_field` из
//     каталога прав для RPC этого маршрута). Эту полосу закрывает синхронный `403`,
//     и обёртка на ней работает — требовать прогрева значило бы чинить закрытое;
//  4. домен-владелец V (домен адреса шага, её опубликовавшего) ОТЛИЧАЕТСЯ от домена,
//     обслуживающего M. Своя ссылка резолвится в собственной БД сервиса — отдельной
//     проверки прав там нет, и окна нет тоже. Ровно поэтому «создал сеть → создал в
//     ней подсеть» находкой не является, хотя выглядит так же;
//  5. цепочка опросов после M не несёт повтора по ИСХОДУ операции
//     (`_opRedriveStarted` + `setNextRequest` на имя M) — это второй законный исход
//     наряду с прогревом.
//
// # Почему цель проверки прав берётся из КАТАЛОГА, а не из имени поля
//
// Пункт 3 — единственное послабление гейта, и оно обязано истекать вместе со своим
// основанием. Каталог прав объявляет, ЧТО именно шлюз проверит на этом маршруте;
// перестанет объявлять — послабление останется без предмета. Поэтому маршрут
// резолвится по `google.api.http` из контрактов, а цель — по записи каталога, и
// перепись обоих печатается: маршрутов резолвится ноль ⇒ гейт ослеп, а не чист.
//
// # Проверено на СОБСТВЕННОЙ истории дерева
//
// Предикат выведен из issue #351 и проверен против ревизии, предшествовавшей
// частному фиксу этого же класса (PR #350, `d48fe62a`): на `d48fe62a^` гейт даёт
// **50** находок, и **три** из них — ровно тот кейс, из которого issue заведён
// (`instance-nic-attach`, `{{nicId}}` из vpc уезжает в
// `:detachNetworkInterface`/`:attachNetworkInterface` compute первым обращением).
// На `d48fe62a` их **ноль**, остаётся **47**: фикс добавил ровно прогрев чтением, и
// гейт увидел это переписью прогретых (6 → 9). Ни имя кейса, ни имена этих глаголов
// в гейте не зашиты — он их переоткрыл. Оставшиеся 47 закрыты этим же изменением.
//
// # Размер полосы, которую гейт НЕ судит, назван числом
//
// Послабление п.3 не декларативно: перепись печатает, сколько асинхронных мутаций
// несут СВОЮ цель проверки прав свежей и непрогретой (замер на этой ревизии — 679).
// Судить их значило бы чинить закрытое: там отказ приходит синхронным 403, и
// код-ключевая обёртка срабатывает. Число печатается, чтобы это было видно, а не
// принималось на слово.
//
// # Чего гейт НЕ утверждает
//
//   - что обёртка вообще стоит на первом доступе — это предмет
//     `newmanfreshreadwrap_test.go` (провязка предиката) и `selftest_autowrap.py`
//     (его работа);
//   - что шаг удаления пережидает 404 — предмет `newmandeleteretrywindow_test.go`;
//   - что опубликованный идентификатор защищён исходом операции — предмет
//     `newmanphantomid_test.go`.
//
// Он утверждает ровно одно: чужой свежий идентификатор не уезжает в асинхронную
// мутацию, не будучи ни прочитанным, ни прикрытым повтором по исходу.
package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// nmRedriveMark / nmWarmWrapMark — маркеры, которые эмитят генераторы.
const (
	nmRedriveMark  = "_opRedriveStarted"
	nmWarmWrapMark = "_authRetryStarted"
)

var (
	nmVarRe     = regexp.MustCompile(`\{\{(\w+)\}\}`)
	nmEnvSetVar = regexp.MustCompile(`pm\.(?:environment|collectionVariables)\.set\(\s*['"](\w+)['"]`)
	nmSetNextRe = regexp.MustCompile(`pm\.execution\.setNextRequest\(\s*'([^']+)'\s*\)`)
	nmLeadVarRe = regexp.MustCompile(`^\{\{\w+\}\}`)
	// nmWholeVarSegRe / nmWholeTplSegRe — сегмент адреса, целиком состоящий из
	// подстановки, с необязательным суффиксом `:глагол`. Суффикс СОХРАНЯЕТСЯ: он и
	// есть дискриминатор действия, и без него `…/instances/*` съело бы
	// `…/instances/*:detachNetworkInterface`.
	nmWholeVarSegRe = regexp.MustCompile(`^\{\{\w+\}\}(:.*)?$`)
	nmWholeTplSegRe = regexp.MustCompile(`^\{[^}]+\}(:.*)?$`)
	nmTplBraceRe    = regexp.MustCompile(`\{[^}]+\}`)
)

var nmMutVerbs = map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}

func TestAsyncMutationDoesNotCarryAnUnwarmedPeerId(t *testing.T) {
	root := repoRoot(t)

	// Состав — из ИНДЕКСА git: под корнем лежат рабочие копии агентов и распаковки
	// отчётов прогонов, и вердикт по ним был бы свойством чужого каталога.
	tt := newTrackedTree(t, root)
	var cols, protos []string
	for rel := range tt.files {
		switch {
		case strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json"):
			cols = append(cols, rel)
		case strings.HasPrefix(rel, "proto/") && strings.HasSuffix(rel, ".proto"):
			protos = append(protos, rel)
		}
	}
	sort.Strings(cols)
	sort.Strings(protos)

	findings, cen, err := auditAsyncPeerWarmup(root, cols, protos, nmCatalogPath)
	if err != nil {
		t.Fatal(err)
	}

	// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ — «ноль находок» обязано быть отличимо от
	// «ноль прочитанного». Каждая из четырёх переписей ниже — отдельный источник, и
	// нулевая любая из них означает, что гейт ослеп на своей стороне, а не что
	// дерево чисто.
	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать; " +
			"чинить надо обход, а не выходить успехом")
	}
	if cen.steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов 0 — обход не узнал ни одного шага", cen.collections)
	}
	if cen.protoFiles == 0 || cen.httpRoutes == 0 {
		t.Fatalf("контрактов прочитано %d, маршрутов с `google.api.http` найдено %d — "+
			"без карты «адрес+метод → RPC» гейт не может отличить асинхронную мутацию "+
			"от синхронной и молчал бы на всём", cen.protoFiles, cen.httpRoutes)
	}
	if cen.catalogRows == 0 {
		t.Fatalf("каталог прав %s пуст — послабление п.3 (цель проверки прав самого шага) "+
			"осталось бы без основания, а гейт требовал бы прогрева там, где полосу "+
			"закрывает синхронный отказ", nmCatalogPath)
	}
	if cen.asyncMutations == 0 {
		t.Fatal("ни одной асинхронной мутации во всём корпусе — распознаватель читает " +
			"не ту форму (сменилось имя конверта операции?), гейт ослеп")
	}
	if cen.peerRefs == 0 {
		t.Fatal("ни одной ссылки на ЧУЖОЙ свежий ресурс во всём корпусе — распознаватель " +
			"публикации переменных читает не ту форму, гейт ослеп")
	}

	t.Logf("осмотрено: коллекций %d, шагов %d; контрактов %d, маршрутов %d, записей каталога %d; "+
		"асинхронных мутаций %d (из них с целью-переменной %d, обёрнутых %d); "+
		"НЕ судится полоса шлюза — своя цель прав, свежая и непрогретая: %d; "+
		"ссылок на чужой свежий ресурс %d, из них прогрето чтением %d, прикрыто повтором по "+
		"исходу %d (контроль обратной стороны)",
		cen.collections, cen.steps, cen.protoFiles, cen.httpRoutes, cen.catalogRows,
		cen.asyncMutations, cen.asyncWithVarTarget, cen.asyncWrapped,
		cen.ownTargetFreshUnread,
		cen.peerRefs, cen.peerWarmed, cen.peerRedriven)

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "асинхронных мутаций, уносящих чужой НЕПРОГРЕТЫЙ свежий идентификатор: %d\n\n",
			len(findings))
		fmt.Fprintf(&b, "Такой шаг отвечает 200 и Operation ВСЕГДА, поэтому отказ владельца чужого\n")
		fmt.Fprintf(&b, "ресурса в окне материализации прав приезжает терминальной ошибкой ВНУТРИ\n")
		fmt.Fprintf(&b, "операции — обёртка окна видимости на этом шаге сработать не может.\n")
		fmt.Fprintf(&b, "Отказ при этом неотличим от настоящего промаха (владелец прячет\n")
		fmt.Fprintf(&b, "существование, security.md §6), и кейс читает открытое окно как\n")
		fmt.Fprintf(&b, "утверждение о продукте.\n\n")
		fmt.Fprintf(&b, "Законных исходов ДВА: прочитать ресурс до мутации\n")
		fmt.Fprintf(&b, "(`retry_until_authorized(GET <владелец>/<id>)`, форма PR #350) ЛИБО\n")
		fmt.Fprintf(&b, "повторить мутацию по ИСХОДУ операции (`poll_operation_until_done(retry_from=…)`).\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}

// nmCatalogPath — каталог прав шлюза. Вторая его копия (посев iam) байт-в-байт
// равна этой и держится своим гейтом дрейфа; читать достаточно одну.
const nmCatalogPath = "gateway/internal/middleware/embed/permission_catalog.json"

// ─── перепись и находка ──────────────────────────────────────────────────────

type nmPeerWarmCensus struct {
	collections        int
	steps              int
	protoFiles         int
	httpRoutes         int
	catalogRows        int
	resolvedSteps      int
	asyncMutations     int
	asyncWithVarTarget int
	asyncWrapped       int
	// ownTargetFreshUnread — размер полосы, которую гейт НЕ судит: цель проверки
	// прав самого шага, свежая и непрогретая. Она закрыта синхронным 403, и
	// обёртка на ней работает. Число печатается, чтобы послабление п.3 было
	// измеримым, а не декларативным: судить эту полосу значило бы чинить закрытое.
	ownTargetFreshUnread int
	peerRefs             int
	peerWarmed           int
	peerRedriven         int
}

type nmPeerWarmFinding struct {
	collection string
	folder     string
	step       string
	route      string
	fqn        string
	variable   string
	ownerDom   string
	callerDom  string
	// redrive заполняется на втором проходе кейса: повтор по исходу стоит на
	// ОПРОСЕ, то есть позже самой мутации.
	redrive bool
}

func (f nmPeerWarmFinding) String() string {
	return fmt.Sprintf("%s :: %s :: %s — {{%s}} (владелец %s) уезжает в %s (%s) первым обращением; RPC %s",
		f.collection, f.folder, f.step, f.variable, f.ownerDom, f.route, f.callerDom, f.fqn)
}

// ─── карта маршрутов из контрактов ───────────────────────────────────────────

type nmRoute struct {
	fqn        string
	pathTpl    string
	pathVars   []string
	returnsOp  bool
	scopeField string
	scopeType  string
	inCatalog  bool
}

// auditAsyncPeerWarmup — весь разбор одним входом.
//
// Вынесено из тела гейта намеренно: проба, доказывающая способность гейта упасть,
// обязана гонять ТУ ЖЕ функцию — иначе она доказывала бы свойство своей копии.
func auditAsyncPeerWarmup(root string, cols, protos []string, catalogRel string) (
	[]nmPeerWarmFinding, nmPeerWarmCensus, error,
) {
	var cen nmPeerWarmCensus

	routes, n, err := nmBuildRoutes(root, protos)
	if err != nil {
		return nil, cen, err
	}
	cen.protoFiles = n
	cen.httpRoutes = len(routes)

	rows, err := nmLoadCatalog(root, catalogRel)
	if err != nil {
		return nil, cen, err
	}
	cen.catalogRows = len(rows)
	for key, r := range routes {
		if e, ok := rows[r.fqn]; ok {
			r.inCatalog = true
			r.scopeField = e.Scope.FromRequestField
			r.scopeType = e.Scope.ObjectType
			routes[key] = r
		}
	}

	var findings []nmPeerWarmFinding
	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		var col nmCollection
		if err := json.Unmarshal(b, &col); err != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, err)
		}
		cen.collections++
		for _, top := range col.Item {
			steps := nmFlatten(top)
			findings = append(findings, nmAuditCase(rel, top.Name, steps, routes, &cen)...)
		}
	}
	return findings, cen, nil
}

// nmFlatten — шаги кейса в порядке исполнения. Кейс — папка верхнего уровня;
// вложенность глубже одного уровня в дереве встречается, и порядок обхода обязан
// совпадать с порядком, в котором их исполняет newman.
func nmFlatten(top nmItem) []nmItem {
	var out []nmItem
	var rec func(it nmItem)
	rec = func(it nmItem) {
		if it.isFolder() {
			for _, ch := range it.Item {
				rec(ch)
			}
			return
		}
		out = append(out, it)
	}
	rec(top)
	return out
}

func nmAuditCase(rel, folder string, steps []nmItem, routes map[string]nmRoute,
	cen *nmPeerWarmCensus,
) []nmPeerWarmFinding {
	// published: переменная → домен ресурса, которому она принадлежит.
	published := map[string]string{}
	read := map[string]bool{}
	var pending []*nmPeerWarmFinding
	lastResourceDom := ""

	for _, it := range steps {
		cen.steps++
		method := it.method()
		url := it.rawURL()
		body := it.bodyRaw()
		script := it.testScript()
		dom := nmDomainOf(url)
		route := nmRouteKey(url)
		poll := nmIsOperationsPath(route)
		if dom != "" && !poll {
			lastResourceDom = dom
		}

		// Повтор по исходу операции стоит на ОПРОСЕ и называет мутацию, которую
		// перезапускает. Кредитуется именно ей, а не «ближайшей выше».
		if poll && strings.Contains(script, nmRedriveMark) {
			for _, target := range nmSetNextRe.FindAllStringSubmatch(script, -1) {
				for _, p := range pending {
					if p.step == target[1] {
						p.redrive = true
					}
				}
			}
		}

		if r, ok := routes[method+" "+route]; ok {
			cen.resolvedSteps++
			if nmMutVerbs[method] && r.returnsOp && !poll {
				cen.asyncMutations++
				if strings.Contains(script, nmWarmWrapMark) {
					cen.asyncWrapped++
				}
				target := nmScopeTargetVar(r, url, body)
				if target != "" {
					cen.asyncWithVarTarget++
				}
				for _, v := range nmRefsOf(url, body) {
					owner, fresh := published[v]
					switch {
					case !fresh:
						continue
					case v == target:
						// Полосу шлюза закрывает синхронный отказ — обёртка на ней
						// работает. Послабление держится записью каталога (см. шапку).
						if !read[v] {
							cen.ownTargetFreshUnread++
						}
						continue
					case owner == "" || dom == "" || owner == "operations":
						continue
					case owner == dom:
						// Своя ссылка: владелец резолвит её в собственной БД, отдельной
						// проверки прав нет, окна нет.
						continue
					}
					// Отсюда и ниже — ЧУЖОЙ свежий идентификатор. Считаем его в перепись
					// целиком, а исходы (прогрет / прикрыт повтором / находка) —
					// раздельно: «ноль находок» обязано быть отличимо от «ноль
					// рассмотренного», а обе законные формы обязаны быть видимы.
					cen.peerRefs++
					if read[v] {
						cen.peerWarmed++
						continue
					}
					pending = append(pending, &nmPeerWarmFinding{
						collection: rel, folder: folder, step: it.Name, route: route,
						fqn: r.fqn, variable: v, ownerDom: owner, callerDom: dom,
					})
				}
			}
		}

		if method == "GET" {
			for _, v := range nmRefsOf(url, body) {
				read[v] = true
			}
		}
		for _, m := range nmEnvSetVar.FindAllStringSubmatch(script, -1) {
			v := m[1]
			if _, seen := published[v]; !seen {
				owner := dom
				if poll || owner == "" {
					owner = lastResourceDom
				}
				published[v] = owner
			}
			// Идентификатор, ОПУБЛИКОВАННЫЙ чтением, уже прочитан: окна у него нет
			// by construction. Без этой строки каталожные зоны и регионы, которые
			// кейс сам и вычитывает, попадали бы в находки навсегда.
			if method == "GET" {
				read[v] = true
			}
		}
	}

	var out []nmPeerWarmFinding
	for _, p := range pending {
		if p.redrive {
			cen.peerRedriven++
			continue
		}
		out = append(out, *p)
	}
	return out
}

// nmScopeTargetVar — переменная, стоящая в поле, которое шлюз возьмёт целью
// проверки прав. Ссылка приезжает и адресом, и телом: читать надо обе формы.
func nmScopeTargetVar(r nmRoute, url, body string) string {
	if !r.inCatalog || r.scopeField == "" {
		return ""
	}
	for _, pv := range r.pathVars {
		if pv != r.scopeField {
			continue
		}
		tpl := strings.Split(r.pathTpl, "/")
		us := strings.Split(strings.Split(nmLeadVarRe.ReplaceAllString(url, ""), "?")[0], "/")
		for k, seg := range tpl {
			if !strings.Contains(seg, "{"+r.scopeField) || k >= len(us) {
				continue
			}
			if m := nmVarRe.FindStringSubmatch(us[k]); m != nil {
				return m[1]
			}
		}
	}
	if body == "" {
		return ""
	}
	camel := nmSnakeToCamel(r.scopeField)
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(camel) + `"\s*:\s*"\{\{(\w+)\}\}"`)
	if m := re.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return ""
}

func nmSnakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	out := parts[0]
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}

func nmRefsOf(url, body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, src := range []string{url, body} {
		for _, m := range nmVarRe.FindAllStringSubmatch(src, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	sort.Strings(out)
	return out
}

// nmRouteKey — адрес шага, приведённый к форме шаблона контракта: ведущая
// переменная базы снята, значения путевых переменных заменены на `*`, суффикс
// `:глагол` сохранён (он и есть дискриминатор действия).
func nmRouteKey(url string) string {
	p := strings.Split(nmLeadVarRe.ReplaceAllString(url, ""), "?")[0]
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		if !strings.Contains(seg, "{{") {
			continue
		}
		if m := nmWholeVarSegRe.FindStringSubmatch(seg); m != nil {
			segs[i] = "*" + m[1]
			continue
		}
		segs[i] = nmVarRe.ReplaceAllString(seg, "*")
	}
	return strings.Join(segs, "/")
}

func nmDomainOf(url string) string {
	p := strings.TrimPrefix(nmLeadVarRe.ReplaceAllString(url, ""), "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}

// nmIsOperationsPath — общий опрос/отмена операции (`{{opsBase}}/operations/{id}`).
// Ресурсный `…/<resource>/{id}/operations` сюда НЕ попадает и попадать не должен:
// это обычное чтение истории ресурса, а не ручка самой операции.
func nmIsOperationsPath(route string) bool {
	return route == "/operations" || strings.HasPrefix(route, "/operations/")
}

func (it nmItem) bodyRaw() string {
	if it.Request == nil || it.Request.Body == nil {
		return ""
	}
	return it.Request.Body.Raw
}

// ─── каталог прав ────────────────────────────────────────────────────────────

type nmCatalogEntry struct {
	FQN   string `json:"fqn"`
	Scope struct {
		ObjectType       string `json:"object_type"`
		FromRequestField string `json:"from_request_field"`
	} `json:"scope_extractor"`
}

func nmLoadCatalog(root, rel string) (map[string]nmCatalogEntry, error) {
	b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь-константа этого гейта
	if err != nil {
		return nil, fmt.Errorf("каталог прав %s: %w", rel, err)
	}
	var rows []nmCatalogEntry
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("разбор каталога прав %s: %w", rel, err)
	}
	out := make(map[string]nmCatalogEntry, len(rows))
	for _, r := range rows {
		out[r.FQN] = r
	}
	return out, nil
}

// ─── разбор контрактов ───────────────────────────────────────────────────────

var (
	nmProtoPkgRe  = regexp.MustCompile(`(?m)^\s*package\s+([\w.]+)\s*;`)
	nmProtoSvcRe  = regexp.MustCompile(`\bservice\s+(\w+)\s*\{`)
	nmProtoRPCRe  = regexp.MustCompile(`\brpc\s+(\w+)\s*\(\s*(?:stream\s+)?[\w.]+\s*\)\s*returns\s*\(\s*(?:stream\s+)?([\w.]+)\s*\)`)
	nmProtoHTTPRe = regexp.MustCompile(`option\s*\(\s*google\.api\.http\s*\)\s*=\s*\{`)
	nmProtoVerbRe = regexp.MustCompile(`\b(get|post|put|patch|delete)\s*:\s*"([^"]*)"`)
	nmTplVarRe    = regexp.MustCompile(`\{([^}=]+)`)
)

func nmBuildRoutes(root string, protos []string) (map[string]nmRoute, int, error) {
	routes := map[string]nmRoute{}
	files := 0
	for _, rel := range protos {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, 0, fmt.Errorf("чтение контракта %s: %w", rel, err)
		}
		files++
		src := nmStripProtoComments(string(b))
		pkg := ""
		if m := nmProtoPkgRe.FindStringSubmatch(src); m != nil {
			pkg = m[1]
		}
		for _, ms := range nmProtoSvcRe.FindAllStringSubmatchIndex(src, -1) {
			svc := src[ms[2]:ms[3]]
			open := strings.IndexByte(src[ms[0]:], '{') + ms[0]
			body := src[open:nmMatchBrace(src, open)]
			for _, mr := range nmProtoRPCRe.FindAllStringSubmatchIndex(body, -1) {
				name := body[mr[2]:mr[3]]
				resp := body[mr[4]:mr[5]]
				tail := body[mr[1]:]
				semi := strings.IndexByte(tail, ';')
				brace := strings.IndexByte(tail, '{')
				rpcBody := ""
				if brace >= 0 && (semi < 0 || brace < semi) {
					rpcBody = tail[brace:nmMatchBrace(tail, brace)]
				}
				for _, mh := range nmProtoHTTPRe.FindAllStringIndex(rpcBody, -1) {
					s := strings.IndexByte(rpcBody[mh[0]:], '{') + mh[0]
					blk := rpcBody[s:nmMatchBrace(rpcBody, s)]
					for _, mv := range nmProtoVerbRe.FindAllStringSubmatch(blk, -1) {
						verb := strings.ToUpper(mv[1])
						path := mv[2]
						routes[verb+" "+nmNormTemplate(path)] = nmRoute{
							fqn:       pkg + "." + svc + "/" + name,
							pathTpl:   path,
							pathVars:  nmTemplateVars(path),
							returnsOp: strings.HasSuffix(resp, "Operation"),
						}
					}
				}
			}
		}
	}
	return routes, files, nil
}

func nmTemplateVars(path string) []string {
	var out []string
	for _, m := range nmTplVarRe.FindAllStringSubmatch(path, -1) {
		out = append(out, m[1])
	}
	return out
}

func nmNormTemplate(path string) string {
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		if !strings.Contains(seg, "{") {
			continue
		}
		if m := nmWholeTplSegRe.FindStringSubmatch(seg); m != nil {
			segs[i] = "*" + m[1]
			continue
		}
		segs[i] = nmTplBraceRe.ReplaceAllString(seg, "*")
	}
	return strings.Join(segs, "/")
}

// nmStripProtoComments — гасит комментарии, СОХРАНЯЯ смещения: решение о маршруте
// принимается по исполняемой части, а слова `post:` и `rpc` встречаются в
// комментариях, объясняющих ровно эти конструкции.
func nmStripProtoComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); {
		switch {
		case src[i] == '"':
			j := i + 1
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(src) {
				j = len(src) - 1
			}
			b.WriteString(src[i : j+1])
			i = j + 1
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				b.WriteString(strings.Repeat(" ", len(src)-i))
				i = len(src)
				continue
			}
			b.WriteString(strings.Repeat(" ", j))
			i += j
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			j := strings.Index(src[i:], "*/")
			if j < 0 {
				b.WriteString(strings.Repeat(" ", len(src)-i))
				i = len(src)
				continue
			}
			for _, r := range src[i : i+j+2] {
				if r == '\n' {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
			}
			i += j + 2
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	return b.String()
}

// nmMatchBrace — индекс сразу за скобкой, парной той, что стоит на openIdx.
func nmMatchBrace(src string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '"':
			j := i + 1
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			i = j
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}
