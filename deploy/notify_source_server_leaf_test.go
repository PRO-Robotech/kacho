// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"testing"
)

// ─── СХОДИМОСТЬ: SAN ЗАПИСИ ПЕРЕЧНЯ ∈ URI СЕРВЕРНОГО ЛИСТА ИСТОЧНИКА (Д112 (а)) ─
//
// ЧАСТИ ИСПРАВНЫ ПОРОЗНЬ И НЕ СХОДЯТСЯ (hard-parts-must-converge). Клиент ленты
// notify (services/notify/internal/source/loop.go, exactSANCreds) принимает
// сервер ленты источника, только если его лист несёт РОВНО ОДИН URI-SAN, равный
// `san` записи перечня (KACHO_NOTIFY_SOURCES). Серверный лист источника выпускает
// чарт. Каждая сторона судилась своей пробой и была зелёной: фикстура клиента
// выпускает серверу лист с URI, а рендер листа пробы и kaname судился без
// клиента. На стенде рукопожатие отвергалось бы каждый раз (находка
// SEC-2915-W2-1).
//
// ЧТО СУДИТ ГЕЙТ. По рендеру зонтика: для каждой записи перечня — серверный лист
// (Certificate с `server auth`), чьё DNS-имя равно узлу `feedAddr` записи (имя
// узла клиент проверяет штатно), и у этого листа `spec.uris` — ровно `[san]`.
// Записи без листа и листы без/с чужим URI — находки с именем записи и листа.
//
// ОТКУДА ЗАПИСИ. Нога дерева — цепочки таблицы как есть: строка `notify-probe`
// таблицы модулей (полоса D2) входит в перечень стендовых цепочек. Нога копии
// дополняет строку `kaname` таблицы записью источника — так перечень несёт оба
// НАСТОЯЩИХ источника, которых notify зовёт по точному SAN (проба и kaname), с
// SAN той формы, которую несёт запись (`spiffe://<домен>/ns/<ns>/
// sa/<учётка>`, литералом — независимый производитель), и судит НАСТОЯЩИЕ листы
// рендера. Пустой обход ноги копии — красный.

// notifySourcesEnv — ключ перечня в карте настроек notify.
const notifySourcesEnv = "KACHO_NOTIFY_SOURCES"

// rosterRecord — поля записи перечня, нужные сходимости.
type rosterRecord struct {
	Module   string `json:"module"`
	FeedAddr string `json:"feedAddr"`
	SAN      string `json:"san"`
}

// sourceLeafFixtureRows — строки таблицы копии: настоящие источники с точным SAN.
var sourceLeafFixtureRows = []rosterRecord{
	{Module: "notify-probe", FeedAddr: "kacho-notify-probe.kacho.svc:9091",
		SAN: "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify-probe"},
	{Module: "kaname", FeedAddr: "kaname-internal.kacho.svc:9091",
		SAN: "spiffe://kacho.cloud/ns/kacho/sa/kaname"},
}

// kanameTableRow — строка `kaname` таблицы модулей дерева: полей источника у
// неё нет (их вносит NTF-2).
const kanameTableRow = `(dict "key" "kaname" "ownFlagKey" "kaname.config.notifications.enabled")`

// sourceLeafKanameRow — строка `kaname` копии: та же строка дерева с записью
// источника из sourceLeafFixtureRows (`authorization: certificate`, Д72).
// Строку `notify-probe` копия берёт у дерева как есть.
func sourceLeafKanameRow() string {
	for _, r := range sourceLeafFixtureRows {
		if r.Module != "kaname" {
			continue
		}
		return fmt.Sprintf(`(dict "key" "kaname" "ownFlagKey" "kaname.config.notifications.enabled" "source" `+
			`(dict "module" %q "feedAddr" %q "san" %q "classes" (list "notice") "recipientForms" (list) "authorization" "certificate"))`,
			r.Module, r.FeedAddr, r.SAN)
	}
	return ""
}

// renderedRoster — записи перечня из карт настроек notify рендера.
func renderedRoster(objs []renderedObj) ([]rosterRecord, error) {
	var out []rosterRecord
	for _, o := range objs {
		if o.kind != "ConfigMap" {
			continue
		}
		data, _ := o.doc["data"].(map[string]any)
		raw, ok := data[notifySourcesEnv]
		if !ok {
			continue
		}
		var recs []rosterRecord
		if err := json.Unmarshal([]byte(nstr(raw)), &recs); err != nil {
			return nil, fmt.Errorf("ConfigMap/%s: %s не разбирается: %w", o.name, notifySourcesEnv, err)
		}
		out = append(out, recs...)
	}
	return out, nil
}

// serverLeaf — серверный лист рендера: имя объекта, DNS-имена, URI.
type serverLeaf struct {
	name string
	dns  []string
	uris []string
}

func serverLeaves(objs []renderedObj) []serverLeaf {
	var out []serverLeaf
	for _, o := range objs {
		if o.kind != "Certificate" {
			continue
		}
		spec, _ := o.doc["spec"].(map[string]any)
		server := false
		for _, u := range stringList(spec["usages"]) {
			if u == "server auth" {
				server = true
			}
		}
		if !server {
			continue
		}
		out = append(out, serverLeaf{name: o.name, dns: stringList(spec["dnsNames"]), uris: stringList(spec["uris"])})
	}
	return out
}

// judgeSourceLeaves — находки сходимости по записям перечня и листам рендера.
// Возвращает и число листов, сопоставленных записям (объём осмотренного).
func judgeSourceLeaves(recs []rosterRecord, leaves []serverLeaf) (findings []string, matched int) {
	for _, r := range recs {
		host, _, err := net.SplitHostPort(r.FeedAddr)
		if err != nil {
			findings = append(findings, fmt.Sprintf("запись %q: feedAddr %q не узел:порт: %v", r.Module, r.FeedAddr, err))
			continue
		}
		var hits []serverLeaf
		for _, l := range leaves {
			for _, d := range l.dns {
				if d == host {
					hits = append(hits, l)
					break
				}
			}
		}
		if len(hits) == 0 {
			findings = append(findings, fmt.Sprintf("запись %q: серверного листа с DNS-именем %q в рендере нет — "+
				"сервер ленты не пройдёт штатную проверку имени узла", r.Module, host))
			continue
		}
		for _, l := range hits {
			matched++
			if len(l.uris) == 1 && l.uris[0] == r.SAN {
				continue
			}
			findings = append(findings, fmt.Sprintf("запись %q: серверный лист Certificate/%s несёт URI %v, а клиент "+
				"notify требует ровно [%s] — рукопожатие отвергнуто, ни Claim, ни Subscribe не дойдут",
				r.Module, l.name, l.uris, r.SAN))
		}
	}
	return findings, matched
}

// TestNotifySourceSANIsCarriedByTheSourceServerLeaf — Д112 (а): san каждой
// записи перечня ∈ URI серверного листа той же службы, ровно одним URI.
// Инъекция — снят `uris` у серверного листа пробы, затем kaname → красный с
// именем записи и листа; близнец — рендер как есть → молчание.
func TestNotifySourceSANIsCarriedByTheSourceServerLeaf(t *testing.T) {
	// Нога дерева: цепочки таблицы как есть.
	tree := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	chains := deployStacksForRender(t, "operator.yaml")
	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("цепочек в таблице 0 — обход пуст, судить нечего")
	}
	treeRecs := 0
	for _, n := range names {
		out, err := renderChainFiles(tree.umbrella, chains[n])
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочка %s: рендер отказал: %v\n%s", n, err, lastLines(out, 5))
		}
		objs := parseRendered(t, out)
		recs, err := renderedRoster(objs)
		if err != nil {
			t.Fatalf("цепочка %s: %v", n, err)
		}
		f, matched := judgeSourceLeaves(recs, serverLeaves(objs))
		for _, x := range f {
			t.Errorf("Д112 (а): цепочка %s: %s", n, x)
		}
		treeRecs += len(recs)
		t.Logf("цепочка %-12s записей перечня %d, сопоставлено листов %d, находок %d", n, len(recs), matched, len(f))
	}

	// Нога копии: таблица несёт настоящие источники; стенд с пробой.
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{notifyEdits: map[string]func(string) string{
		"templates/_sources.tpl": replaceOnce(kanameTableRow, sourceLeafKanameRow()),
		"values.yaml":            withSourceLimits("kaname"),
	}})
	const standChain = "dev-prod"
	files := append(c.chainFiles(t, standChain), c.notifyLayer(t))
	out, err := c.render(files, probeStandSets...)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочка %s с пробой и перечнем копии: рендер отказал: %v\n%s",
			standChain, err, lastLines(out, 5))
	}
	objs := parseRendered(t, out)
	recs, err := renderedRoster(objs)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != len(sourceLeafFixtureRows) {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: записей перечня в рендере копии %d, подставлено %d — копия судила бы не тот перечень",
			len(recs), len(sourceLeafFixtureRows))
	}
	leaves := serverLeaves(objs)
	twin, matched := judgeSourceLeaves(recs, leaves)
	if matched < len(recs) {
		t.Errorf("сопоставлено листов %d на записей %d — обход неполон", matched, len(recs))
	}
	for _, x := range twin {
		t.Errorf("Д112 (а): цепочка %s с пробой: %s", standChain, x)
	}
	t.Logf("нога копии: цепочка %s + слой notify + %v; записей %d, серверных листов в рендере %d, сопоставлено %d, находок %d",
		standChain, probeStandSets, len(recs), len(leaves), matched, len(twin))

	// Инъекции: у листа каждого источника снят `uris` → ровно одна находка с
	// именем записи. Лист выбирается по DNS-имени узла записи.
	for _, r := range recs {
		host, _, _ := net.SplitHostPort(r.FeedAddr)
		injected := make([]serverLeaf, len(leaves))
		copy(injected, leaves)
		hit := ""
		for i := range injected {
			for _, d := range injected[i].dns {
				if d == host {
					injected[i].uris = nil
					hit = injected[i].name
				}
			}
		}
		if hit == "" {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: инъекции «uris снят» для %q нечего снимать — листа узла %q нет", r.Module, host)
		}
		f, _ := judgeSourceLeaves(recs, injected)
		want := fmt.Sprintf("запись %q: серверный лист Certificate/%s несёт URI []", r.Module, hit)
		if len(f) != 1 || !strings.HasPrefix(f[0], want) {
			t.Errorf("инъекция «uris снят у %s»: находки %v, ждали одну, начинающуюся с %q", hit, f, want)
		}
		t.Logf("инъекция «uris снят у Certificate/%s» → %v", hit, f)
	}
	t.Logf("Д112 (а): записей перечня в дереве %d (таблица пуста до D2), в ноге копии %d; близнец → находок %d",
		treeRecs, len(recs), len(twin))
}
