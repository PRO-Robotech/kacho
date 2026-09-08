// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dbtls_declaration_test.go — стенд, чьи базы ОТДАЮТ TLS, обязан ПРОСИТЬ его у
// каждого клиента, и просить объявлением профиля, а не наследованием.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Шифрование канала к базе держится на ОДНОМ значении у КАЖДОГО клиента —
// `sslmode`. Сервер, у которого TLS включён, продолжает принимать и открытый
// текст: клиент с `sslmode=disable` соединяется успешно, и со стороны клиента
// это ничем не отличается от шифрованного соединения. Поэтому пропуск виден
// только со стороны СУБД (`pg_stat_ssl`) — то есть постфактум, на поднятом
// стенде, и только пока клиент подключён.
//
// Четыре чарта из семи несут в своих значениях умолчание `disable`
// (kacho-geo, kaname, registry, vpc — предикат в dbTLSKnobs ниже
// перечисляет их из дерева, а не отсюда). Значит профиль, который просто НЕ
// НАЗЫВАЕТ `sslmode`, получает открытый текст — молча и с видом настройки по
// умолчанию. Ровно так и было: `values.prod.yaml` объявлял `sslmode` источника
// разовой работы переноса географии, а `values.dev-prod.yaml` — профиль, на
// котором ночной гейт посадки доказывает боевую посадку, — не объявлял, и
// значение приезжало из чарта.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ТРЕБУЕТСЯ ИМЕННО ОБЪЯВЛЕНИЕ, А НЕ «ЛИШЬ БЫ ЗНАЧЕНИЕ БЫЛО ВЕРНЫМ»
//
// Умолчание чарта — свойство ЧАРТА, а не стенда, и оно меняется под профилем
// без единой правки профиля. Это уже происходило в этом же дереве: умолчание
// `authMode` у kacho-geo перевели с `dev` на `production`, и dev-профилю
// пришлось начать объявлять свою посадку самому (см. комментарий у
// `kacho-geo.authMode` в values.dev.yaml). Здесь та же дисциплина, только цена
// ошибки — открытый канал с учётными данными и строками БД (CWE-319).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседних posture_parity_test.go и
// gateway/deploy/token_shape_test.go: контракт — то, что профиль ОБЪЯВЛЯЕТ.
// Проверке не нужны ни `helm`, ни скачанные зависимости чартов, поэтому она не
// умеет пропуститься. Рендер тут и не помог бы: значение, приехавшее из
// умолчания чарта, в манифесте выглядит точно так же, как объявленное.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ЭТО ОТЛИЧАЕТСЯ ОТ ДВУХ СОСЕДНИХ ПРОВЕРОК ТОГО ЖЕ ПРЕДМЕТА
//
// Их три, и они не дублируют друг друга — у каждой свой момент и своя цена:
//
//   - tests/helm/prod-profile-fail-closed-test.sh — РЕНДЕР одного профиля
//     (values.prod.yaml) и три сервиса (iam, vpc, nlb). Читает то, что реально
//     соберётся в манифест, — но требует `helm dep update` и mikefarah yq, то
//     есть умеет не исполниться;
//   - scripts/assert-production-posture.sh §B — ИСХОД на поднятом стенде,
//     свидетельствует сама СУБД. Ловит и то, чего в объявлениях нет вовсе
//     (посторонний клиент, не перекатившийся под), но постфактум и только пока
//     соединение существует: короткая работа между двумя прогонами гейта не
//     наблюдается никогда;
//   - этот файл — ОБЪЯВЛЕНИЯ всех восьми ручек во всех цепочках, до деплоя, без
//     зависимостей чартов. Он один отвечает на вопрос «а объявлено ли вообще».
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Проверяются ТОЛЬКО подчарты НАШЕГО дерева (зависимости `file://` +
//     каталоги charts/). Хранилища Ory держат свою строку соединения сами,
//     их послабление объявлено в шапке values.fe3455-prod.yaml, и решать
//     за них здесь нечего — они не наши.
//   - Проверяются ТОЛЬКО стеки, у которых хотя бы одна база отдаёт TLS. Там,
//     где сервер TLS не отдаёт, требовать `require` от клиента значило бы
//     требовать несуществующего: соединение просто не установится. Правило
//     самонастраивается — профиль, включивший TLS на своих базах, приходит под
//     проверку без правки этого файла.
//   - Проверяется КАНАЛ, а не подлинность сервера: `require` шифрует, но не
//     сверяет сертификат. Выбор между require/verify-ca/verify-full — решение
//     профиля, здесь принимается любой из трёх.
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

// secureSSLModes — значения, при которых канал к базе шифруется. Всё остальное
// (disable/allow/prefer и пустая строка) допускает открытый текст: `prefer`
// молча падает в plaintext, если сервер в этот момент TLS не предложил.
var secureSSLModes = map[string]bool{"require": true, "verify-ca": true, "verify-full": true}

// ─────────────────────────────────────────────────────────────────────────────
// Чтение дерева. Ничего не выписывается руками.

// dbTLSKnob — одна ручка `sslmode`, которую объявляет чарт нашего дерева.
// Путь — внутри поддерева значений подчарта, поэтому в профиле он читается как
// `<subchart>.<path…>`.
type dbTLSKnob struct {
	subchart     string
	path         []string
	chartDefault string
}

func (k dbTLSKnob) coord() string { return k.subchart + "." + strings.Join(k.path, ".") }

// walkStrings обходит дерево значений и отдаёт листья, чьё ИМЯ ключа проходит
// предикат. Регистр имени не нормализуется у источника: чарты пишут и
// `sslMode`, и `sslmode`, и это разные ключи helm — предикат сравнивает в
// нижнем регистре, а координата печатается как в дереве.
func walkStrings(node any, path []string, keyOK func(string) bool, out *[]struct {
	Path  []string
	Value any
}) {
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		next := append(append([]string{}, path...), k)
		if keyOK(k) {
			*out = append(*out, struct {
				Path  []string
				Value any
			}{next, m[k]})
		}
		walkStrings(m[k], next, keyOK, out)
	}
}

// dbTLSKnobs — набор ручек, ВЫВЕДЕННЫЙ из значений подчартов нашего дерева.
// Новый сервис со своей базой попадает под проверку сам, без правки этого
// файла; именно поэтому список не выписан.
func dbTLSKnobs(t *testing.T) []dbTLSKnob {
	t.Helper()
	var out []dbTLSKnob
	dirs := subchartDirs(t)
	keys := make([]string, 0, len(dirs))
	for k := range dirs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		vf := filepath.Join(dirs[key], "values.yaml")
		if _, err := os.Stat(vf); err != nil {
			continue // чарт без своих значений — нечего объявлять
		}
		var found []struct {
			Path  []string
			Value any
		}
		walkStrings(readYAML(t, vf), nil, func(k string) bool {
			return strings.EqualFold(k, "sslmode")
		}, &found)
		for _, f := range found {
			out = append(out, dbTLSKnob{
				subchart:     key,
				path:         f.Path,
				chartDefault: fmt.Sprint(f.Value),
			})
		}
	}
	return out
}

// stacksTable — координата ЕДИНСТВЕННОГО места в дереве, где цепочки `-f`
// объявлены. Цепочки не дублируются здесь и нигде больше: расхождение двух
// списков об одном предмете — тот самый класс, который эта проверка ловит на
// уровне значений. Единственность держит stack_table_test.go
// (TestNoSecondCopyOfAStackChain), а не уговор.
const stacksTable = "stacks.txt"

var stackLine = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*):(values[^,\s]*(?:,values[^,\s]*)*)$`)

// deployStacks — цепочки профилей, которыми стенды раскатываются, прочитанные
// из таблицы выше. Тот же файл читает shell-сторона (tests/helm/stacks.sh).
func deployStacks(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(stacksTable)
	if err != nil {
		t.Fatalf("таблица стеков %s не читается (%v) — предпосылка проверки исчезла, "+
			"а не дерево стало чистым", stacksTable, err)
	}
	out := map[string][]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := stackLine.FindStringSubmatch(line)
		if m == nil {
			// Нераспознанная строка — это НЕ «стеков меньше», это «предикат
			// перестал их узнавать». Молчать здесь значит сузить проверку.
			t.Fatalf("строка таблицы стеков не разобрана: %q (%s)", line, stacksTable)
		}
		out[m[1]] = strings.Split(m[2], ",")
	}
	if len(out) == 0 {
		t.Fatalf("в %s нет ни одной строки стека — проверка не вправе считать, "+
			"что стеков не осталось", stacksTable)
	}
	for name, chain := range out {
		for _, p := range chain {
			if _, err := os.Stat(filepath.Join(umbrellaDir, p)); err != nil {
				t.Fatalf("стек %q называет профиль %s, которого нет: %v", name, p, err)
			}
		}
	}
	return out
}

// mergeValues накладывает src на dst так же, как helm накладывает файлы
// значений: карты сливаются по ключам, всё остальное замещается целиком.
func mergeValues(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if cur, ok := dst[k].(map[string]any); ok {
				dst[k] = mergeValues(cur, sub)
				continue
			}
			dst[k] = mergeValues(map[string]any{}, sub)
			continue
		}
		dst[k] = v
	}
	return dst
}

func lookup(tree map[string]any, path ...string) (any, bool) {
	var cur any = tree
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[k]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// pgAliases — имена инстансов Postgres, выведенные из зависимостей умбреллы
// (alias `pg-<svc>` у чарта postgresql).
func pgAliases(t *testing.T) []string {
	t.Helper()
	chart := readYAML(t, filepath.Join(umbrellaDir, "Chart.yaml"))
	deps, _ := chart["dependencies"].([]any)
	var out []string
	for _, d := range deps {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		alias, _ := m["alias"].(string)
		if strings.HasPrefix(alias, "pg-") {
			out = append(out, alias)
		}
	}
	sort.Strings(out)
	return out
}

// stackFacts — то, что проверке нужно знать про один стек: что объявляют ЕГО
// профили (без значений чартов — иначе «объявлено» и «унаследовано» неразличимы),
// полное действующее дерево и то, отдаёт ли TLS хоть одна база НАШИХ сервисов.
type stackFacts struct {
	declared  map[string]any // слияние ТОЛЬКО профилей стека
	effective map[string]any // значения умбреллы + профили стека
	pgTLSOn   []string       // наши инстансы Postgres, у которых tls.enabled=true
}

// ourPGAliases — инстансы Postgres, которые НАШИ подчарты называют своим
// хостом. Выводится из дерева: хост в значениях записан как `<release>-<alias>`
// (`kacho-umbrella-pg-iam`), поэтому принадлежность устанавливается суффиксом,
// а не списком имён. Хранилища Ory сюда не попадают by construction —
// на них не ссылается ни один наш подчарт.
func ourPGAliases(aliases []string, effective map[string]any, ours map[string]string) []string {
	referenced := map[string]bool{}
	subs := make([]string, 0, len(ours))
	for s := range ours {
		subs = append(subs, s)
	}
	sort.Strings(subs)
	for _, sub := range subs {
		node, ok := effective[sub]
		if !ok {
			continue
		}
		for _, s := range stringLeaves(node) {
			for _, a := range aliases {
				if s == a || strings.HasSuffix(s, "-"+a) {
					referenced[a] = true
				}
			}
		}
	}
	var out []string
	for _, a := range aliases {
		if referenced[a] {
			out = append(out, a)
		}
	}
	return out
}

// stringLeaves — все строковые листья поддерева.
func stringLeaves(node any) []string {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out []string
		for _, k := range keys {
			out = append(out, stringLeaves(v[k])...)
		}
		return out
	case []any:
		var out []string
		for _, e := range v {
			out = append(out, stringLeaves(e)...)
		}
		return out
	case string:
		return []string{strings.TrimSpace(v)}
	}
	return nil
}

// stackFactsFor собирает факты одного стека. Одна функция на все проверки файла:
// два расчёта одного и того же разъезжаются молча.
func stackFactsFor(t *testing.T, chain []string, base map[string]any,
	aliases []string, ours map[string]string) stackFacts {
	t.Helper()
	declared := map[string]any{}
	for _, p := range chain {
		declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
	}
	effective := mergeValues(mergeValues(map[string]any{}, base), declared)
	var on []string
	for _, a := range ourPGAliases(aliases, effective, ours) {
		if v, ok := lookup(effective, a, "tls", "enabled"); ok && v == true {
			on = append(on, a)
		}
	}
	return stackFacts{declared: declared, effective: effective, pgTLSOn: on}
}

// allStackFacts — факты по всем стекам дерева.
func allStackFacts(t *testing.T) (map[string]stackFacts, []dbTLSKnob, []string) {
	t.Helper()
	knobs := dbTLSKnobs(t)
	chains := deployStacks(t)
	aliases := pgAliases(t)
	ours := subchartDirs(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))

	out := map[string]stackFacts{}
	for name, chain := range chains {
		out[name] = stackFactsFor(t, chain, base, aliases, ours)
	}
	return out, knobs, aliases
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы самопроверка ниже могла подать ей
// синтетический вход, а не подделывать дерево.

type dbFinding struct {
	stack string
	coord string
	kind  string // "не объявлено" | "открытый текст"
	value string
}

func scanDBTLS(stacks map[string]stackFacts, knobs []dbTLSKnob) []dbFinding {
	var out []dbFinding
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		f := stacks[name]
		if len(f.pgTLSOn) == 0 {
			continue // базы этого стека TLS не отдают — требовать нечего
		}
		for _, k := range knobs {
			v, ok := lookup(f.declared, append([]string{k.subchart}, k.path...)...)
			if !ok {
				out = append(out, dbFinding{name, k.coord(), "не объявлено", k.chartDefault})
				continue
			}
			if !secureSSLModes[strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))] {
				out = append(out, dbFinding{name, k.coord(), "открытый текст", fmt.Sprint(v)})
			}
		}
	}
	return out
}

// recordedPlaintextConcessions — послабления, принятые ЯВНО и записанные с
// координатой и причиной. Ключ — «<стек>/<координата>».
//
// Запись живёт, пока у неё есть предмет: если послабление снято в профиле,
// TestRecordedConcessions_StillHaveASubject краснеет и требует удалить запись.
// Иначе исключение переживёт свой предмет и начнёт разрешать то, чего уже нет.
var recordedPlaintextConcessions = map[string]string{
	"fe3455/registry.db.sslMode": "объявленная уступка кластера fe3455: сервер pg-registry " +
		"TLS отдаёт (mtls.pgTls.services включает registry), клиент его пока не просит. " +
		"Причина и обратимость записаны в шапке values.fe3455-prod.yaml §KNOWN, DOCUMENTED " +
		"concessions. Снятие — решение владельца: меняет посадку живого кластера",
}

// ─────────────────────────────────────────────────────────────────────────────
// САМА ПРОВЕРКА.

func TestProductionStacks_DeclareEncryptedDatabaseChannel(t *testing.T) {
	stacks, knobs, aliases := allStackFacts(t)

	tlsStacks := 0
	for _, f := range stacks {
		if len(f.pgTLSOn) > 0 {
			tlsStacks++
		}
	}

	// Проверка СВОЕЙ предпосылки. «Ноль находок» обязано быть отличимо от «ноль
	// прочитанного»: если обход перестал узнавать ручки, стеки или инстансы
	// Postgres, он объявит дерево чистым, ничего не осмотрев.
	if len(knobs) == 0 || len(stacks) == 0 || len(aliases) == 0 || tlsStacks == 0 {
		t.Fatalf("обход ничего не прочитал: ручек sslmode=%d, стеков=%d, инстансов Postgres=%d, "+
			"стеков с TLS на базах=%d — предикат перестал узнавать дерево, "+
			"а не дерево стало чистым", len(knobs), len(stacks), len(aliases), tlsStacks)
	}
	coords := make([]string, 0, len(knobs))
	for _, k := range knobs {
		coords = append(coords, k.coord())
	}
	t.Logf("осмотрено: ручек sslmode=%d (%s), стеков=%d, из них с TLS на наших базах=%d, инстансов Postgres=%d",
		len(knobs), strings.Join(coords, " "), len(stacks), tlsStacks, len(aliases))

	for _, f := range scanDBTLS(stacks, knobs) {
		if why, recorded := recordedPlaintextConcessions[f.stack+"/"+f.coord]; recorded && f.kind == "открытый текст" {
			t.Logf("записанное послабление %s/%s = %q: %s", f.stack, f.coord, f.value, why)
			continue
		}
		switch f.kind {
		case "не объявлено":
			t.Errorf("%s: %s НЕ ОБЪЯВЛЕН ни одним профилем стека — значение приедет из умолчания "+
				"чарта (%q). Базы этого стека отдают TLS, а умолчание чарта — свойство чарта: "+
				"оно меняется под профилем без единой правки профиля. Объяви sslmode в профиле "+
				"стека (require|verify-ca|verify-full)", f.stack, f.coord, f.value)
		case "открытый текст":
			t.Errorf("%s: %s = %q — канал к базе НЕ ШИФРУЕТСЯ, при том что базы этого стека "+
				"отдают TLS. По этому каналу едут пароль БД и строки данных (CWE-319). "+
				"Допустимо require|verify-ca|verify-full; осознанное послабление вносится "+
				"в recordedPlaintextConcessions с причиной, а не молчанием", f.stack, f.coord, f.value)
		}
	}
}

// Строка соединения, выписанная целиком (а не собранная чартом из host/user/
// sslmode), обходит проверку выше by construction: у неё нет ключа `sslmode`.
// Ровно такой ручкой один сервис дотягивается до БАЗЫ ДРУГОГО (kacho-nlb
// умеет слушать оповещения прямо из базы iam) — то есть это единственный
// объявленный путь, которым к чужой базе подключается не её владелец.
// Сегодня она пуста во всех профилях; проверка стоит на входе, а не на факте.
func TestProductionStacks_InlineDSNsCarrySSLMode(t *testing.T) {
	stacks, _, _ := allStackFacts(t)
	ours := subchartDirs(t)

	subs := make([]string, 0, len(ours))
	for s := range ours {
		subs = append(subs, s)
	}
	sort.Strings(subs)

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	inspected := 0
	for _, name := range names {
		f := stacks[name]
		if len(f.pgTLSOn) == 0 {
			continue
		}
		for _, sub := range subs {
			node, ok := f.effective[sub].(map[string]any)
			if !ok {
				continue
			}
			inspected++
			for _, hit := range inlineDSNs(node, []string{sub}) {
				if !dsnSSLModeSecure(hit.Value) {
					t.Errorf("%s: %s — строка соединения к Postgres без шифрования (%s). "+
						"Выписанная целиком строка не проходит через ручку sslmode чарта, "+
						"поэтому обязана нести sslmode=require|verify-ca|verify-full сама",
						name, strings.Join(hit.Path, "."), dsnRedact(hit.Value))
				}
			}
		}
	}
	if inspected == 0 {
		t.Fatal("не осмотрено ни одного поддерева подчарта — обход перестал узнавать дерево")
	}
	t.Logf("осмотрено поддеревьев подчартов в стеках с TLS на наших базах: %d", inspected)
}

type dsnHit struct {
	Path  []string
	Value string
}

// inlineDSNs — строковые листья, похожие на строку соединения к Postgres.
func inlineDSNs(node any, path []string) []dsnHit {
	var out []dsnHit
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, inlineDSNs(v[k], append(append([]string{}, path...), k))...)
		}
	case []any:
		for i, e := range v {
			out = append(out, inlineDSNs(e, append(append([]string{}, path...), fmt.Sprintf("[%d]", i)))...)
		}
	case string:
		s := strings.TrimSpace(v)
		if strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://") {
			out = append(out, dsnHit{path, s})
		}
	}
	return out
}

var dsnSSLMode = regexp.MustCompile(`(?i)[?&]sslmode=([a-z-]+)`)

func dsnSSLModeSecure(dsn string) bool {
	m := dsnSSLMode.FindStringSubmatch(dsn)
	return m != nil && secureSSLModes[strings.ToLower(m[1])]
}

// dsnRedact вырезает пароль: сообщение проверки читает тот же, кто читает
// журнал сборки, а строка соединения несёт учётные данные.
func dsnRedact(dsn string) string {
	at := strings.Index(dsn, "@")
	sl := strings.Index(dsn, "//")
	if at < 0 || sl < 0 || at < sl {
		return dsn
	}
	return dsn[:sl+2] + "***@" + dsn[at+1:]
}

// Записанное послабление обязано иметь предмет. Запись, которой больше нечего
// разрешать, — находка: её унаследует следующая слепая зона.
func TestRecordedConcessions_StillHaveASubject(t *testing.T) {
	stacks, knobs, _ := allStackFacts(t)

	live := map[string]bool{}
	for _, f := range scanDBTLS(stacks, knobs) {
		if f.kind == "открытый текст" {
			live[f.stack+"/"+f.coord] = true
		}
	}
	for key := range recordedPlaintextConcessions {
		if !live[key] {
			t.Errorf("записанное послабление %q больше нечего разрешать — канал уже шифруется "+
				"или ручка исчезла. Удали запись из recordedPlaintextConcessions: исключение, "+
				"пережившее свой предмет, разрешает то, чего нет", key)
		}
	}
	t.Logf("записанных послаблений=%d, действующих открытых каналов=%d",
		len(recordedPlaintextConcessions), len(live))
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе той же формы.
//
// Без положительного контроля отрицание зеленеет на всём сломанном: обход,
// который перестал что-либо узнавать, «не находит» ровно так же, как чистое
// дерево.

func TestScanDBTLS_SelfTest(t *testing.T) {
	knobs := []dbTLSKnob{
		{subchart: "kacho-geo", path: []string{"db", "sslmode"}, chartDefault: "disable"},
	}

	// (а) внесённый дефект №1 — стек с TLS на базах НЕ объявляет ручку.
	//     Обязан покраснеть и назвать координату.
	undeclared := map[string]stackFacts{
		"injected": {declared: map[string]any{}, pgTLSOn: []string{"pg-geo"}},
	}
	got := scanDBTLS(undeclared, knobs)
	if len(got) != 1 || got[0].kind != "не объявлено" || got[0].coord != "kacho-geo.db.sslmode" {
		t.Fatalf("пропуск объявления не пойман или пойман без координаты: %+v", got)
	}

	// (б) внесённый дефект №2 — объявлено, но открытым текстом.
	plaintext := map[string]stackFacts{
		"injected": {
			declared: map[string]any{"kacho-geo": map[string]any{"db": map[string]any{"sslmode": "disable"}}},
			pgTLSOn:  []string{"pg-geo"},
		},
	}
	got = scanDBTLS(plaintext, knobs)
	if len(got) != 1 || got[0].kind != "открытый текст" || got[0].value != "disable" {
		t.Fatalf("открытый канал не пойман: %+v", got)
	}

	// (в) законная конструкция ТОЙ ЖЕ формы — объявлено и шифруется. Молчит.
	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		ok := map[string]stackFacts{
			"injected": {
				declared: map[string]any{"kacho-geo": map[string]any{"db": map[string]any{"sslmode": mode}}},
				pgTLSOn:  []string{"pg-geo"},
			},
		}
		if got := scanDBTLS(ok, knobs); len(got) != 0 {
			t.Fatalf("законное объявление sslmode=%s покрашено: %+v", mode, got)
		}
	}

	// (г) вторая законная конструкция той же формы — стек, чьи базы TLS не
	//     отдают, ручку не объявляет. Требовать `require` там значило бы
	//     требовать соединения, которое не установится. Молчит.
	noTLS := map[string]stackFacts{
		"injected": {declared: map[string]any{}, pgTLSOn: nil},
	}
	if got := scanDBTLS(noTLS, knobs); len(got) != 0 {
		t.Fatalf("стек без TLS на базах покрашен: %+v", got)
	}
}

func TestInlineDSN_SelfTest(t *testing.T) {
	cases := []struct {
		dsn  string
		want bool
	}{
		{"postgres://u:p@h:5432/d?sslmode=require", true},
		{"postgres://u:p@h:5432/d?sslmode=verify-full&x=1", true},
		{"postgres://u:p@h:5432/d?sslmode=disable", false},
		{"postgres://u:p@h:5432/d?sslmode=prefer", false},
		{"postgres://u:p@h:5432/d", false},
	}
	for _, c := range cases {
		if got := dsnSSLModeSecure(c.dsn); got != c.want {
			t.Errorf("dsnSSLModeSecure(%q) = %v, ждали %v", c.dsn, got, c.want)
		}
	}
	if got := dsnRedact("postgres://u:secret@h:5432/d?sslmode=require"); strings.Contains(got, "secret") {
		t.Errorf("пароль не вырезан из сообщения: %s", got)
	}
	hits := inlineDSNs(map[string]any{
		"config": map[string]any{"authz": map[string]any{"iamDsn": "postgres://u@h/d?sslmode=disable"}},
		"other":  "not a dsn",
	}, []string{"kacho-nlb"})
	if len(hits) != 1 || strings.Join(hits[0].Path, ".") != "kacho-nlb.config.authz.iamDsn" {
		t.Fatalf("строка соединения не найдена по дереву значений: %+v", hits)
	}
}

// Предикаты обязаны узнавать НАСТОЯЩЕЕ дерево, а не только синтетику: иначе
// самопроверка выше зелёная, а обход читает ноль.
func TestDBTLSPredicates_RecogniseTheRealTree(t *testing.T) {
	var have []string
	for _, k := range dbTLSKnobs(t) {
		have = append(have, k.coord())
	}
	for _, want := range []string{
		"kaname.config.repository.postgres.sslMode",
		"vpc.repository.postgres.sslMode",
		"kacho-geo.dataMigration.source.sslMode",
	} {
		found := false
		for _, h := range have {
			if h == want {
				found = true
			}
		}
		if !found {
			t.Errorf("ручка %s не опознана в дереве — обход перестал её узнавать; найдено: %v", want, have)
		}
	}

	chains := deployStacks(t)
	for _, want := range []string{"dev-prod", "prod", "fe3455"} {
		if len(chains[want]) == 0 {
			t.Errorf("стек %q не выведен из %s — таблица стеков сменила форму; выведено: %v",
				want, stacksTable, chains)
		}
	}
	if got := pgAliases(t); len(got) < 5 {
		t.Errorf("инстансов Postgres выведено %d (%v) — предикат alias `pg-*` перестал узнавать "+
			"зависимости умбреллы", len(got), got)
	}
}
