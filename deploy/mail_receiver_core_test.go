// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// mail_receiver_core_test.go — узел-приёмник писем ЕСТЬ в поставке, он не
// шлюзуемый, его аппетит назван числом, и ни один профиль не объявляет полосу
// до него незашифрованной.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ЛОВИТ И ПОЧЕМУ ЭТОГО НЕ БЫЛО ВИДНО
//
// Три профиля называли почтовым узлом `mailhog.kacho.svc:1025`, и не поднимал
// его НИ ОДИН манифест поставки. Со стороны всё выглядело исправным: рендер
// проходил, поды стартовали, посадка зелёная. Письма уходили в никуда, и
// сигнала не было ни одного — ни отказа, ни строки в журнале, ни красного
// вердикта. Обе задачи доставки (приглашение не доходит; после выхода войти
// нельзя) упирались сюда.
//
// Дефект тихий by construction: «профиль называет узел» и «поставка узел
// поднимает» — РАЗНЫЕ утверждения, и до этого гейта их не сверял никто.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕТЫРЕ УТВЕРЖДЕНИЯ И ПОЧЕМУ ИХ ЧЕТЫРЕ, А НЕ ОДНО
//
//	(1) названный внутрикластерный узел ПОДНИМАЕТСЯ поставкой;
//	(2) приёмник НЕ шлюзуемый: ни один шард сквозных проб его не снимает
//	    (решение Р18 приёмки ID-MAIL-1 — шард, снявший узел и оставивший
//	    настройку, стартует и молча не доставляет);
//	(3) аппетит приёмника назван ЧИСЛОМ, и самый тяжёлый шард вместе с ним
//	    остаётся ниже allocatable раннера — знаменатель назван, а не
//	    подразумевается;
//	(4) ни один профиль не объявляет полосу без шифрования или без проверки
//	    сертификата (решение Р5, ban #16).
//
// Каждое падает отдельно: слить их в одно значило бы получить находку, по
// которой не видно, что именно неверно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ ЧИТАЕТ ОБЪЯВЛЕНИЕ, А НЕ ОТРЕНДЕРЕННЫЙ ЧАРТ
//
// Рендер требует helm и сети; такая проверка пропускается ровно там, где её
// отсутствия никто не заметит. Объявление читается всегда и одинаково. То, что
// проверяемо ТОЛЬКО рендером (отказ стража на внесённом дефекте), доказывается
// отдельно — tests/helm/identity-mail-lane-guard-inject.sh.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// mailReceiverKey — ключ значений, которым приёмник включается. Он же служит
// именем компонента в раскладке шардов: если бы приёмник когда-нибудь стал
// шлюзуемым, он появился бы там ИМЕННО под этим именем.
const mailReceiverKey = "mailpit"

// mailReceiverTemplate — манифест поставки, поднимающий приёмник.
const mailReceiverTemplate = "helm/umbrella/templates/mail-receiver.yaml"

// mailLaneGuard — страж рендера (место С1 решения Р4а).
const mailLaneGuard = "helm/umbrella/templates/identity-mail-lane-guard.yaml"

// umbrellaProfiles — профили умбреллы. Перечень ВЫВОДИТСЯ из каталога: выписанный
// отстанет от дерева на первом же новом профиле, и отставание будет незаметным.
func umbrellaProfiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob("helm/umbrella/values*.yaml")
	if err != nil {
		t.Fatalf("обход профилей: %v", err)
	}
	sub, err := filepath.Glob("helm/umbrella/charts/*/values.yaml")
	if err != nil {
		t.Fatalf("обход подчартов: %v", err)
	}
	matches = append(matches, sub...)
	if len(matches) == 0 {
		t.Fatal("профилей не найдено — гейт, не прочитавший предмет, обязан падать, а не зеленеть")
	}
	sort.Strings(matches)
	return matches
}

// declaredMailURI — величина `global.kacho.identity.smtp.connectionURI` профиля,
// либо пустая строка. Читается ЛИСТ дерева значений, а не текст файла: то же
// слово встречается в прозе и в самом страже, и предикат по подстроке краснел бы
// на собственном объяснении.
func declaredMailURI(doc map[string]any) string {
	node := any(doc)
	for _, k := range []string{"global", "kacho", "identity", "smtp", "connectionURI"} {
		m, ok := node.(map[string]any)
		if !ok {
			return ""
		}
		node, ok = m[k]
		if !ok {
			return ""
		}
	}
	s, _ := node.(string)
	return strings.TrimSpace(s)
}

func readProfile(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- путь из обхода каталога репозитория
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: разбор: %v", path, err)
	}
	return doc
}

// mailHostOf вытаскивает узел из адреса полосы: `smtp://[user:pass@]host[:port][/…]`.
func mailHostOf(uri string) string {
	rest := uri
	for _, p := range []string{"smtps://", "smtp://"} {
		rest = strings.TrimPrefix(rest, p)
	}
	rest = strings.SplitN(rest, "/", 2)[0]
	rest = strings.SplitN(rest, "?", 2)[0]
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		rest = rest[:i]
	}
	return strings.TrimSpace(rest)
}

// inClusterHost — узел, у которого нет собственного доменного имени наружу.
// Именно этот класс и был предметом дефекта: имя внутри кластера, которого никто
// не поднимает. Внешний ретранслятор боевой площадки под утверждение (1) не
// подпадает — его поднимаем не мы.
func inClusterHost(h string) bool {
	if h == "" {
		return false
	}
	if !strings.Contains(h, ".") {
		return true
	}
	return strings.HasSuffix(h, ".svc") || strings.HasSuffix(h, ".svc.cluster.local")
}

// receiverServiceSuffix — суффикс имени, под которым приёмник поднимается.
// ВЫВОДИТСЯ из самого манифеста, а не выписывается здесь: копия литерала
// разошлась бы с шаблоном молча, и гейт стал бы сверять себя с собой.
func receiverServiceSuffix(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(mailReceiverTemplate) // #nosec G304 -- координата из константы
	if err != nil {
		t.Fatalf("манифест приёмника %s не читается: %v — предмет гейта отсутствует, "+
			"и молчать об этом нельзя", mailReceiverTemplate, err)
	}
	re := regexp.MustCompile(`printf\s+"%s-([a-z0-9-]+)"\s+\.Release\.Name`)
	m := re.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s не объявляет имя рабочего объекта формой `printf \"%%s-<имя>\" .Release.Name` — "+
			"вывести суффикс неоткуда, а выписать его здесь значило бы завести второе место об одном предмете",
			mailReceiverTemplate)
	}
	return "-" + string(m[1])
}

// ─────────────────────────────────────────────────────────────────────────────
// СУЖДЕНИЯ — ЧИСТЫЕ ФУНКЦИИ
//
// Вынесены не ради красоты: доказательство инъекцией
// (mail_receiver_core_injection_test.go) обязано подавать им ВХОД, а не
// переписывать дерево. Второй предикат, написанный в доказательстве, доказывал
// бы себя, а не гейт.

// unraisedInClusterHost — узел, названный полосой, который поставка НЕ поднимает.
// Возвращает имя узла (находка) либо пустую строку. Внешний ретранслятор под
// суждение не подпадает: его поднимаем не мы.
func unraisedInClusterHost(uri, receiverSuffix string) string {
	host := mailHostOf(uri)
	if !inClusterHost(host) {
		return ""
	}
	if strings.HasSuffix(strings.SplitN(host, ".", 2)[0], receiverSuffix) {
		return ""
	}
	return host
}

// unprotectedLane — почему полоса не защищена; пустая строка ⇒ защищена.
func unprotectedLane(uri string) string {
	low := strings.ToLower(strings.TrimSpace(uri))
	switch {
	case strings.Contains(low, "disable_starttls=true"), strings.Contains(low, "disable_starttls=1"):
		return "шифрование снято (disable_starttls)"
	case strings.Contains(low, "skip_ssl_verify=true"), strings.Contains(low, "skip_ssl_verify=1"):
		return "проверка сертификата снята (skip_ssl_verify)"
	case !strings.HasPrefix(low, "smtp://") && !strings.HasPrefix(low, "smtps://"):
		return "схема не распознана"
	}
	return ""
}

// receiverIsGateable — находки о том, что приёмник объявлен снимаемым.
func receiverIsGateable(l shardLayout, key string) []string {
	var out []string
	for _, g := range l.Gates {
		if g == key {
			out = append(out, fmt.Sprintf("приёмник писем %q значится ШЛЮЗУЕМЫМ компонентом", key))
		}
	}
	for _, s := range l.Shards {
		for _, c := range s.Components {
			if c == key {
				out = append(out, fmt.Sprintf("шард %q перечисляет приёмник писем %q среди компонентов "+
					"СВЕРХ ядра — значит остальные шарды его снимают", s.ID, key))
			}
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// (1) НАЗВАННЫЙ ВНУТРИКЛАСТЕРНЫЙ УЗЕЛ ПОДНИМАЕТСЯ ПОСТАВКОЙ

func TestNamedMailNodeIsRaisedByTheDelivery(t *testing.T) {
	suffix := receiverServiceSuffix(t)
	profiles := umbrellaProfiles(t)

	filesRead, lanesDeclared := 0, 0
	var stray []string
	for _, p := range profiles {
		doc := readProfile(t, p)
		filesRead++
		uri := declaredMailURI(doc)
		if uri == "" {
			continue
		}
		lanesDeclared++
		if host := unraisedInClusterHost(uri, suffix); host != "" {
			stray = append(stray, fmt.Sprintf("%s → %s", filepath.Base(p), host))
		}
	}

	t.Logf("перепись: профилей прочитано %d · полос объявлено %d · манифест приёмника %s",
		filesRead, lanesDeclared, mailReceiverTemplate)

	if filesRead == 0 {
		t.Fatal("прочитано ноль профилей — «расхождений нет» означало бы «ничего не прочитано»")
	}
	if len(stray) > 0 {
		sort.Strings(stray)
		t.Errorf("профиль называет внутрикластерный почтовый узел, которого поставка НЕ поднимает: %v.\n"+
			"Это ровно тот дефект, ради которого гейт заведён: рендер проходит, поды стартуют, посадка\n"+
			"зелёная — и письма уходят в никуда без единого сигнала. Либо подними узел манифестом\n"+
			"(%s поднимает объект с суффиксом %q), либо назови внешний ретранслятор.",
			stray, mailReceiverTemplate, suffix)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (2) ПРИЁМНИК НЕ ШЛЮЗУЕМЫЙ (MAIL-45, первая половина)

// shardEntry — один шард раскладки сквозных проб. Тип ИМЕНОВАН, а не вложен
// анонимно: доказательство инъекцией собирает такие записи руками, и анонимный
// тип пришлось бы переписывать там дословно — второе место об одном предмете.
type shardEntry struct {
	ID         string   `json:"id"`
	Components []string `json:"components"`
}

type shardLayout struct {
	Gates  []string     `json:"gates"`
	Shards []shardEntry `json:"shards"`
}

func readShardLayout(t *testing.T) shardLayout {
	t.Helper()
	raw, err := os.ReadFile("e2e-shards.json")
	if err != nil {
		t.Fatalf("раскладка шардов не читается: %v", err)
	}
	var l shardLayout
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatalf("раскладка шардов: разбор: %v", err)
	}
	if len(l.Shards) == 0 {
		t.Fatal("шардов ноль — обходить нечего; «приёмник не снят ни одним» на пустом множестве " +
			"верно тривиально, и это не вердикт")
	}
	return l
}

func TestMailReceiverIsNotAGateableComponent(t *testing.T) {
	l := readShardLayout(t)

	// Число шардов ВЫВОДИТСЯ из объявления, а не выписывается: выписанное
	// разошлось бы с деревом молча (круг 1б приёмки написал «семь» при пяти,
	// и заметил это только рецензент).
	t.Logf("перепись: шардов прочитано %d · шлюзуемых компонентов %d", len(l.Shards), len(l.Gates))

	for _, why := range receiverIsGateable(l, mailReceiverKey) {
		t.Errorf("%s\nШард, снявший узел и оставивший настройку, стартует и молча не доставляет — "+
			"ровно то состояние, которое чинится; а снять вместе с узлом настройку он не вправе: "+
			"тогда служба личности не стартует вовсе (решение Р18 приёмки ID-MAIL-1).", why)
	}

	// Положительный контроль: шлюзуемый компонент, который ВПРАВЕ сниматься,
	// гейт не роняет. Без него утверждение «приёмника нет в перечне» зеленело
	// бы и на пустом перечне.
	if len(l.Gates) == 0 {
		t.Error("шлюзуемых компонентов ноль — отрицание «приёмника среди них нет» стало вакуумным")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (3) АППЕТИТ НАЗВАН ЧИСЛОМ И ПОМЕЩАЕТСЯ В ЗАПАС (MAIL-45, вторая половина)

var (
	// Строка таблицы §4 `deploy/E2E-SHARDS.md`: CPU — предпоследний столбец.
	shardRowRe = regexp.MustCompile(`(?m)^\|\s*` + "`" + `([a-z0-9-]+)` + "`" + `\s*\|.*\|\s*(\d+)m\s*\|\s*\d+%\s*\|\s*$`)
	// Знаменатель: allocatable раннера, названный §3 того же документа.
	allocatableRe = regexp.MustCompile(`(\d+)m[\s\S]{0,40}?allocatable`)
	cpuMilliRe    = regexp.MustCompile(`^(\d+)m$`)
)

func TestMailReceiverAppetiteFitsTheHeaviestShard(t *testing.T) {
	// Величина ЧИТАЕТСЯ ИЗ ОБЪЯВЛЕНИЯ, а не выписана в пробе: выписанная
	// перестала бы что-либо утверждать о дереве в первой же правке значений.
	base := readProfile(t, "helm/umbrella/values.yaml")
	mp, ok := base[mailReceiverKey].(map[string]any)
	if !ok {
		t.Fatalf("узел значений %q в helm/umbrella/values.yaml отсутствует — предмет гейта не найден",
			mailReceiverKey)
	}
	res, _ := mp["resources"].(map[string]any)
	req, _ := res["requests"].(map[string]any)
	cpuStr, _ := req["cpu"].(string)
	memStr, _ := req["memory"].(string)
	if cpuStr == "" || memStr == "" {
		t.Fatalf("аппетит приёмника не назван числом (`%s.resources.requests`): cpu=%q memory=%q.\n"+
			"Неназванный запрос означает, что планировщик размещает узел «как получится», и вопрос\n"+
			"«помещается ли самый тяжёлый шард в раннер» ответа не имеет вовсе", mailReceiverKey, cpuStr, memStr)
	}
	m := cpuMilliRe.FindStringSubmatch(cpuStr)
	if m == nil {
		t.Fatalf("запрос CPU приёмника %q объявлен НЕ в тех единицах, что остальная раскладка "+
			"(миллиядра, форма `NNm`) — сложить его с числами шардов нельзя", cpuStr)
	}
	receiverCPU, _ := strconv.Atoi(m[1])

	raw, err := os.ReadFile("E2E-SHARDS.md")
	if err != nil {
		t.Fatalf("раскладка стендов E2E-SHARDS.md не читается: %v — знаменатель взять неоткуда", err)
	}
	rows := shardRowRe.FindAllStringSubmatch(string(raw), -1)
	if len(rows) == 0 {
		t.Fatal("в E2E-SHARDS.md §4 не прочитано ни одной строки шарда — «запас есть» на пустой " +
			"таблице верно тривиально, и это не вердикт")
	}
	heaviest, heaviestName := 0, ""
	for _, r := range rows {
		v, _ := strconv.Atoi(r[2])
		if v > heaviest {
			heaviest, heaviestName = v, r[1]
		}
	}
	am := allocatableRe.FindStringSubmatch(string(raw))
	if am == nil {
		t.Fatal("в E2E-SHARDS.md не назван allocatable раннера — доля от полного стенда на вопрос " +
			"«помещается ли» не отвечает вовсе, и знаменатель обязан стоять рядом с числами")
	}
	allocatable, _ := strconv.Atoi(am[1])

	total := heaviest + receiverCPU
	t.Logf("перепись: строк шардов прочитано %d · тяжелейший %q %dm · приёмник %dm (%s) · "+
		"итого %dm · allocatable раннера %dm · запас %dm",
		len(rows), heaviestName, heaviest, receiverCPU, memStr, total, allocatable, allocatable-total)

	if allocatable <= 0 {
		t.Fatal("allocatable раннера прочитан как ноль — сравнивать не с чем")
	}
	if total > allocatable {
		t.Errorf("самый тяжёлый шард %q (%dm) вместе с приёмником писем (%dm) даёт %dm при "+
			"allocatable раннера %dm. Решение Р18 в этом случае пересматривается ЦЕЛИКОМ, а не "+
			"подпирается поднятым пределом: предикат возврата отвергнутой альтернативы "+
			"(шлюзуемый приёмник плюс гейт) назван именно этим замером.",
			heaviestName, heaviest, receiverCPU, total, allocatable)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (4) НИ ОДИН ПРОФИЛЬ НЕ ОБЪЯВЛЯЕТ ПОЛОСУ БЕЗ ШИФРОВАНИЯ (Р5, ban #16)

func TestNoProfileDeclaresAnUnencryptedMailLane(t *testing.T) {
	profiles := umbrellaProfiles(t)
	filesRead, lanesDeclared := 0, 0
	var bad []string

	for _, p := range profiles {
		doc := readProfile(t, p)
		filesRead++
		uri := declaredMailURI(doc)
		if uri == "" {
			continue
		}
		lanesDeclared++
		if why := unprotectedLane(uri); why != "" {
			bad = append(bad, filepath.Base(p)+": "+why)
		}
	}

	t.Logf("перепись: профилей прочитано %d · полос объявлено %d", filesRead, lanesDeclared)

	if filesRead == 0 {
		t.Fatal("прочитано ноль профилей — «нарушений нет» означало бы «ничего не прочитано»")
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Errorf("полоса до почтового узла объявлена без защиты: %v.\n"+
			"Под ban #16 (production-mode ВЕЗДЕ, включая стенд) такой посадки на поднятом стенде\n"+
			"существовать не должно; решение Р5 приёмки ID-MAIL-1 исключения для стенда не знает.", bad)
	}

	// Страж рендера обязан существовать и называть ОБЕ величины: без него это
	// утверждение защищает только те профили, что уже лежат в дереве, — а
	// защищать надо и тот, которого ещё нет.
	g, err := os.ReadFile(mailLaneGuard)
	if err != nil {
		t.Fatalf("страж почтовой полосы %s не найден: %v — перепись выше защищает только "+
			"сегодняшние профили и ничего не говорит о следующем", mailLaneGuard, err)
	}
	for _, needle := range []string{"disable_starttls", "skip_ssl_verify", "fromAddress", "connectionURI"} {
		if !strings.Contains(string(g), needle) {
			t.Errorf("страж %s не упоминает %q — величина остаётся вне его предмета", mailLaneGuard, needle)
		}
	}
}
