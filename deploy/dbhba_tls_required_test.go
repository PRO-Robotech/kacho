// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dbhba_tls_required_test.go — база, которая ОТДАЁТ TLS, обязана его ТРЕБОВАТЬ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ, И ЧЕМ ОН ОТЛИЧАЕТСЯ ОТ СОСЕДНЕГО dbtls_declaration_test.go
//
// Сосед проверяет КЛИЕНТА: что каждый профиль объявляет `sslmode` и не наследует
// его из умолчания чарта. Этот файл проверяет СЕРВЕР: что он не принимает
// незашифрованное соединение по сети вообще — ни от объявленного клиента, ни от
// какого угодно другого.
//
// Разница не косметическая. `tls.enabled: true` у чарта bitnami заставляет
// сервер УМЕТЬ шифровать, но правила доступа остаются вида `host`, а `host`
// матчит соединение и с TLS, и без него. То есть при включённом TLS сервер
// продолжает принимать открытый текст, и шифрование канала держится ровно на
// том, что КАЖДЫЙ клиент помнит про `sslmode`. Клиент, который его не назвал,
// соединяется открытым текстом, и со стороны клиента это неотличимо от
// шифрованного соединения.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОТКУДА ВЫВЕДЕНО (#121)
//
// Ночной гейт посадки увидел у одной из наших баз нешифрованные соединения по
// сети, тогда как сам сервис в том же прогоне заявлял `db_sslmode=require`, а
// проверка сохранённых настроек была чистой. Установить, кто держал те
// соединения, не удалось: они закрылись раньше, чем к стенду пришли с вопросом.
//
// Это свойство самого наблюдения, а не невезение. Со стороны СУБД пропуск виден
// только пока соединение существует (`pg_stat_ssl`), поэтому короткая работа
// между двумя прогонами гейта не наблюдается никогда. Пока сервер принимает
// открытый текст, вопрос «кто это был» будет возникать снова — и каждый раз
// постфактум.
//
// Поэтому требование перенесено на сервер: пропущенный `sslmode` становится
// отказом в соединении при старте потребителя (громко, сразу, в логе этого
// потребителя), а не тихим открытым каналом, который надо ловить выборкой.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Проверяются ВСЕ инстансы Postgres умбреллы (включая хранилища Ory и
//     OpenFGA) — но ТОЛЬКО те, у кого `tls.enabled: true` в этом стеке.
//     Принадлежность подчартам здесь не спрашивается намеренно: требование
//     обращено к серверу, а не к клиенту, поэтому «чей это клиент» его не
//     меняет. Сегодня у хранилищ Ory TLS выключен, и они проходят через
//     зеркальную ветку, ничего не требуя.
//   - Требовать `hostssl` от базы, которая TLS не отдаёт, значило бы запретить
//     единственный возможный способ соединения — стенд просто не поднялся бы.
//     Правило самонастраивается: профиль, включивший TLS, приходит под проверку
//     без правки этого файла, а выключивший — уходит из-под неё.
//   - Петля (`127.0.0.1/32`, `::1/128`) и unix-сокет НЕ требуют шифрования и это
//     намеренно: сети там нет, а через 127.0.0.1 ходят пробы готовности чарта
//     (`pg_isready -h 127.0.0.1`). Запрет на них сломал бы готовность пода,
//     ничего не защитив. Ровно ту же границу проводит гейт посадки, считая
//     законными соединения с `client_addr IS NULL`.
//   - Проверяется ОБЪЯВЛЕНИЕ профиля, а не рендер: значение, приехавшее из
//     умолчания чарта, в манифесте выглядит точно так же, как объявленное.
//     Проверке не нужны ни `helm`, ни скачанные зависимости — она не умеет
//     пропуститься.
package deploy_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// hbaPath — координата ручки внутри поддерева значений инстанса Postgres.
var hbaPath = []string{"primary", "pgHbaConfiguration"}

// loopbackAddrs — адреса, для которых открытый текст законен: соединение не
// покидает под, поэтому сетевого наблюдателя у него нет by construction.
var loopbackAddrs = map[string]bool{
	"127.0.0.1": true, "127.0.0.1/32": true,
	"::1": true, "::1/128": true,
	"localhost": true, "samehost": true,
}

// hbaRule — одна разобранная строка pg_hba.conf.
type hbaRule struct {
	typ     string // local | host | hostssl | hostnossl
	address string // пусто для local
	method  string
}

// parseHBA разбирает тело pg_hba.conf. Комментарии и пустые строки отбрасываются.
//
// Строка `local` не несёт адреса, поэтому метод у неё — четвёртое поле, а у
// `host*` — пятое. Строка, которую разобрать не удалось, возвращается с пустым
// типом: вызывающий обязан считать её находкой, а не пропустить. «Не понял» —
// это не «претензий нет».
func parseHBA(body string) (rules []hbaRule, unparsed []string) {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch {
		case len(f) >= 4 && f[0] == "local":
			rules = append(rules, hbaRule{typ: "local", method: f[3]})
		case len(f) >= 5 && (f[0] == "host" || f[0] == "hostssl" || f[0] == "hostnossl"):
			rules = append(rules, hbaRule{typ: f[0], address: f[3], method: f[4]})
		default:
			unparsed = append(unparsed, line)
		}
	}
	return rules, unparsed
}

// acceptsNetworkPlaintext — правило, по которому сервер примет незашифрованное
// соединение ИЗ СЕТИ.
//
// `host` учитывается наравне с `hostnossl`: он матчит соединение независимо от
// шифрования, то есть разрешает и открытый текст. Именно эта форма и стоит в
// умолчании чарта, и именно поэтому «TLS включён» не означает «TLS обязателен».
func (r hbaRule) acceptsNetworkPlaintext() bool {
	if r.typ != "host" && r.typ != "hostnossl" {
		return false
	}
	if loopbackAddrs[r.address] {
		return false
	}
	return strings.ToLower(r.method) != "reject"
}

// requiresNetworkTLS — правило, которым сервер принимает шифрованное соединение
// из сети. Без хотя бы одного такого сужение не «строгое», а глухое: не
// подключится вообще никто.
func (r hbaRule) requiresNetworkTLS() bool {
	return r.typ == "hostssl" && !loopbackAddrs[r.address] && strings.ToLower(r.method) != "reject"
}

type hbaFinding struct {
	stack  string
	pg     string
	kind   string // см. константы ниже
	detail string
}

const (
	hbaNotDeclared  = "не объявлено"
	hbaAcceptsPlain = "принимает открытый текст по сети"
	hbaNoTLSRule    = "нет ни одного правила hostssl"
	hbaUnparsed     = "строка не разобрана"
	hbaNarrowNoTLS  = "сужение без TLS"
)

// scanHBA — ядро проверки: чистая функция над фактами стеков, чтобы самопроверка
// ниже подавала ей синтетический вход, а не подделывала дерево.
//
// Две стороны, и вторая обязательна. Прямая: база отдаёт TLS — обязана требовать.
// Зеркальная: база TLS НЕ отдаёт, а сужение объявлено — это не «строже, чем
// надо», это стенд, который не поднимется, и упадёт он не здесь, а в рантайме
// на каждом клиенте сразу.
func scanHBA(stacks map[string]hbaStack, aliases []string) []hbaFinding {
	var out []hbaFinding
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		f := stacks[name]
		tlsOn := map[string]bool{}
		for _, a := range f.tlsOn(aliases) {
			tlsOn[a] = true
		}

		for _, pg := range aliases {
			v, declared := lookup(f.declared, append([]string{pg}, hbaPath...)...)
			body := ""
			if declared {
				body = fmt.Sprint(v)
			}
			if !tlsOn[pg] {
				if declared && strings.TrimSpace(body) != "" {
					out = append(out, hbaFinding{name, pg, hbaNarrowNoTLS,
						"инстанс не отдаёт TLS, а правила требуют его — соединиться не сможет никто"})
				}
				continue
			}
			if !declared || strings.TrimSpace(body) == "" {
				out = append(out, hbaFinding{name, pg, hbaNotDeclared,
					"умолчание чарта — правила вида `host`, принимающие и открытый текст"})
				continue
			}
			rules, unparsed := parseHBA(body)
			for _, u := range unparsed {
				out = append(out, hbaFinding{name, pg, hbaUnparsed, u})
			}
			tlsRule := false
			for _, r := range rules {
				if r.acceptsNetworkPlaintext() {
					out = append(out, hbaFinding{name, pg, hbaAcceptsPlain,
						fmt.Sprintf("%s … %s %s", r.typ, r.address, r.method)})
				}
				if r.requiresNetworkTLS() {
					tlsRule = true
				}
			}
			if !tlsRule {
				out = append(out, hbaFinding{name, pg, hbaNoTLSRule,
					"ни одного `hostssl` на сетевой адрес — по сети не подключится никто"})
			}
		}
	}
	return out
}

// hbaStacks — факты, которые нужны ИМЕННО этой проверке, по каждому стеку.
//
// ПОЧЕМУ НЕ ПЕРЕИСПОЛЬЗУЕТСЯ `pgTLSOn` СОСЕДА. Сосед сужает набор до баз, на
// которые ссылаются НАШИ подчарты (`ourPGAliases`), и правильно делает: его
// предмет — `sslmode` у НАШЕГО клиента, а строку соединения Ory и OpenFGA держат
// сами, решать за них нечего.
//
// Здесь предмет другой, и то же сужение было бы дырой. Требование «сервер не
// принимает открытый текст по сети» относится к СЕРВЕРУ и не зависит от того,
// чей клиент к нему ходит. Измерено на этом дереве: сужение соседа распознаёт
// 3 инстанса из 10 в стеке dev-prod — то есть проверка молчала бы о четырёх
// базах, которые TLS отдают. Поэтому берутся ВСЕ инстансы Postgres умбреллы, а
// принадлежность подчартам не спрашивается вовсе.
type hbaStack struct {
	declared  map[string]any // слияние ТОЛЬКО профилей стека
	effective map[string]any // значения умбреллы + профили стека
}

func hbaStacks(t *testing.T) (map[string]hbaStack, []string) {
	t.Helper()
	chains := deployStacks(t)
	aliases := pgAliases(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))

	out := map[string]hbaStack{}
	for name, chain := range chains {
		declared := map[string]any{}
		for _, p := range chain {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		out[name] = hbaStack{
			declared:  declared,
			effective: mergeValues(mergeValues(map[string]any{}, base), declared),
		}
	}
	return out, aliases
}

// tlsOn — инстансы, у которых сервер РЕАЛЬНО отдаёт TLS. Считается по
// ДЕЙСТВУЮЩЕМУ дереву, а не по объявлению профиля: включить TLS может и
// умолчание умбреллы, и тогда требование всё равно наступает.
func (s hbaStack) tlsOn(aliases []string) []string {
	var out []string
	for _, a := range aliases {
		if v, ok := lookup(s.effective, a, "tls", "enabled"); ok && v == true {
			out = append(out, a)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// САМА ПРОВЕРКА.

func TestProductionStacks_DatabasesRequireTLS_NotMerelyOfferIt(t *testing.T) {
	stacks, aliases := hbaStacks(t)

	tlsStacks, tlsInstances := 0, 0
	for _, f := range stacks {
		if n := len(f.tlsOn(aliases)); n > 0 {
			tlsStacks++
			tlsInstances += n
		}
	}

	// Проверка СВОЕЙ предпосылки. Запрет обоснован фактом о дереве (есть стеки,
	// у которых наши базы отдают TLS). Факт исчезнет — запрет станет ложью, а
	// обход объявит дерево чистым, ничего не осмотрев.
	if len(stacks) == 0 || len(aliases) == 0 || tlsStacks == 0 || tlsInstances == 0 {
		t.Fatalf("обход ничего не прочитал: стеков=%d, инстансов Postgres=%d, стеков с TLS=%d, "+
			"инстансов с TLS=%d — предикат перестал узнавать дерево, а не дерево стало чистым",
			len(stacks), len(aliases), tlsStacks, tlsInstances)
	}
	// Перепись ПОИМЁННАЯ. Суммарное число не даёт отличить «стек осмотрен целиком»
	// от «в стеке распознан один инстанс из семи»: у стека свой набор включённых
	// подчартов, поэтому под проверку попадают только те базы, на которые
	// подчарты этого стека реально ссылаются.
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		on := stacks[n].tlsOn(aliases)
		t.Logf("  стек %-12s инстансов Postgres=%d, из них отдают TLS=%d %v",
			n, len(aliases), len(on), on)
	}
	t.Logf("осмотрено: стеков=%d, инстансов Postgres в умбрелле=%d, стеков с TLS=%d, "+
		"инстанс-объявлений с TLS=%d", len(stacks), len(aliases), tlsStacks, tlsInstances)

	for _, f := range scanHBA(stacks, aliases) {
		switch f.kind {
		case hbaNotDeclared:
			t.Errorf("%s/%s: база отдаёт TLS, но НЕ ТРЕБУЕТ его — %s.\n"+
				"    `tls.enabled` делает шифрование ВОЗМОЖНЫМ, а не обязательным: правило `host`\n"+
				"    матчит соединение и без TLS. Значит канал шифруется ровно постольку, поскольку\n"+
				"    каждый клиент помнит про sslmode, а забывший подключится открытым текстом молча.\n"+
				"    Объяви %s.%s в профиле стека (см. values.dev-prod.yaml).",
				f.stack, f.pg, f.detail, f.pg, strings.Join(hbaPath, "."))
		case hbaAcceptsPlain:
			t.Errorf("%s/%s: правило принимает незашифрованное соединение по сети: %q.\n"+
				"    `host` и `hostnossl` без `reject` разрешают открытый текст. Сетевой адрес\n"+
				"    обязан обслуживаться только `hostssl`; открытыми остаются лишь петля и сокет.",
				f.stack, f.pg, f.detail)
		case hbaNoTLSRule:
			t.Errorf("%s/%s: %s — сужение глухое, а не строгое.", f.stack, f.pg, f.detail)
		case hbaUnparsed:
			t.Errorf("%s/%s: строка pg_hba не разобрана: %q. «Не понял» — это не «претензий нет»: "+
				"проверка не вправе считать нераспознанную строку безопасной.", f.stack, f.pg, f.detail)
		case hbaNarrowNoTLS:
			t.Errorf("%s/%s: %s. Сужение объявлено там, где сервер TLS не отдаёт — "+
				"это не «строже», это неподнимающийся стенд.", f.stack, f.pg, f.detail)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — доказательство в ОБЕ стороны на синтетическом входе.
//
// Без положительного близнеца отрицание зеленеет сильнее всего тогда, когда
// сломано всё: проверка, не находящая ничего, неотличима от проверки, которая
// ничего не читает.

func TestScanHBA_SelfTest(t *testing.T) {
	const good = "local all all md5\n" +
		"host all all 127.0.0.1/32 md5\n" +
		"hostssl all all 0.0.0.0/0 md5\n" +
		"hostnossl all all 0.0.0.0/0 reject\n"

	facts := func(tlsOn []string, hba map[string]string) hbaStack {
		declared := map[string]any{}
		effective := map[string]any{}
		for pg, body := range hba {
			declared[pg] = map[string]any{"primary": map[string]any{"pgHbaConfiguration": body}}
		}
		for _, pg := range tlsOn {
			effective[pg] = map[string]any{"tls": map[string]any{"enabled": true}}
		}
		return hbaStack{declared: declared, effective: effective}
	}

	cases := []struct {
		name  string
		facts hbaStack
		ourPG []string
		want  []string // ожидаемые виды находок
	}{
		{"законный близнец: TLS + сужение", facts([]string{"pg-iam"},
			map[string]string{"pg-iam": good}), []string{"pg-iam"}, nil},
		{"законный близнец: без TLS и без сужения", facts(nil, nil),
			[]string{"pg-iam"}, nil},
		{"дефект: TLS без сужения (умолчание чарта)", facts([]string{"pg-iam"}, nil),
			[]string{"pg-iam"}, []string{hbaNotDeclared}},
		{"дефект: остался `host` на сетевой адрес", facts([]string{"pg-iam"},
			map[string]string{"pg-iam": good + "host all all 0.0.0.0/0 md5\n"}),
			[]string{"pg-iam"}, []string{hbaAcceptsPlain}},
		{"дефект: hostnossl без reject", facts([]string{"pg-iam"},
			map[string]string{"pg-iam": good + "hostnossl all all 10.0.0.0/8 md5\n"}),
			[]string{"pg-iam"}, []string{hbaAcceptsPlain}},
		{"дефект: сужение без единого hostssl", facts([]string{"pg-iam"},
			map[string]string{"pg-iam": "local all all md5\nhostnossl all all 0.0.0.0/0 reject\n"}),
			[]string{"pg-iam"}, []string{hbaNoTLSRule}},
		{"дефект: сужение там, где TLS не отдаётся", facts(nil,
			map[string]string{"pg-iam": good}), []string{"pg-iam"}, []string{hbaNarrowNoTLS}},
		{"дефект: нераспознанная строка не считается безопасной", facts([]string{"pg-iam"},
			map[string]string{"pg-iam": good + "hostssl all\n"}),
			[]string{"pg-iam"}, []string{hbaUnparsed}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scanHBA(map[string]hbaStack{"проба": tc.facts}, tc.ourPG)
			var kinds []string
			for _, f := range got {
				kinds = append(kinds, f.kind)
			}
			if len(tc.want) == 0 {
				if len(kinds) != 0 {
					t.Fatalf("законная конструкция помечена находкой: %v", kinds)
				}
				return
			}
			for _, w := range tc.want {
				found := false
				for _, k := range kinds {
					if k == w {
						found = true
					}
				}
				if !found {
					t.Fatalf("дефект не найден: ждали %q, получили %v", w, kinds)
				}
			}
		})
	}
}

// TestHBAPredicates_RecogniseTheRealTree — предикаты обязаны узнавать НАСТОЯЩЕЕ
// дерево, а не только синтетику самопроверки.
//
// Самопроверка выше подаёт ядру входы, которые пишет сама, поэтому она доказывает
// логику и НИЧЕГО не говорит о том, что разбор справляется с телом, реально
// лежащим в профиле. Разъедутся они молча.
func TestHBAPredicates_RecogniseTheRealTree(t *testing.T) {
	stacks, aliases := hbaStacks(t)

	seen, withTLSRule := 0, 0
	for name, f := range stacks {
		for _, pg := range aliases {
			v, ok := lookup(f.declared, append([]string{pg}, hbaPath...)...)
			if !ok {
				continue
			}
			seen++
			rules, unparsed := parseHBA(fmt.Sprint(v))
			if len(unparsed) != 0 {
				t.Errorf("%s/%s: разбор не понял строки настоящего профиля: %v", name, pg, unparsed)
			}
			if len(rules) == 0 {
				t.Errorf("%s/%s: разбор не извлёк НИ ОДНОГО правила из непустого тела", name, pg)
			}
			for _, r := range rules {
				if r.requiresNetworkTLS() {
					withTLSRule++
					break
				}
			}
		}
	}
	if seen == 0 || withTLSRule == 0 {
		t.Fatalf("в дереве не нашлось ни одного объявленного pg_hba (%d) или ни одного с hostssl (%d) — "+
			"предикат перестал узнавать дерево", seen, withTLSRule)
	}
	t.Logf("настоящих объявлений pg_hba прочитано=%d, из них с сетевым hostssl=%d", seen, withTLSRule)
}
