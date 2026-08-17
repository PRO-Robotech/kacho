// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotakindproducer_test.go — репо-широкий гейт: вид, объявленный в ЗАКРЫТОМ
// каталоге потолков, обязан иметь производителя списания хоть у кого-нибудь.
//
// ЗАЧЕМ ОН СУЩЕСТВУЕТ. Каталог `countableKinds` объявляет себя закрытым именно
// затем, чтобы потолок нельзя было назначить на вид, которого никто не считает.
// Проверка членства на входе это и держит — но она держит ОДНУ сторону: что вид
// назван в каталоге. Вторая сторона — что у названного вида есть кто-то, кто
// списывает место, — не держалась ничем. Потолок на несписываемый вид
// сохраняется, отвечает успехом и не применяется никогда: класс
// «принято-и-проигнорировано» (`api-conventions.md`), ровно тот, ради
// предотвращения которого каталог и закрывали.
//
// ЧЕМ ОН ОТЛИЧАЕТСЯ ОТ СОСЕДА. `quotaadmitkinds_test.go` сверяет ДВЕ ПОЛОСЫ
// одного владельца — о чём спрашивает совещательная и что списывает
// авторитетная. Он верен и остаётся, но по построению не может увидеть вид,
// которого нет НИ В ОДНОЙ из двух: обе его стороны читаются у владельца, а
// каталог живёт у владельца величин. Здесь третья сторона — сам каталог.
//
// ПОЧЕМУ ДОМЕНЫ ВЫВОДЯТСЯ, А НЕ ВЫПИСЫВАЮТСЯ. Прежний гейт пинил один файл
// миграции и один каталог use-case'ов константами, поэтому у шести доменов из
// семи расхождение было НЕНАБЛЮДАЕМО: гейт не читал их вовсе и печатал зелёный.
// Здесь производители ищутся по всем `services/*/internal/migrations/*.sql`, и
// «ноль находок» отличимо от «ноль прочитанного» переписью ниже.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// quotaCatalogueFile — где объявлен закрытый каталог видов.
//
// Координата выписана, и это единственная выписанная координата гейта: вывести
// её из дерева можно было бы только поиском по имени переменной, то есть тем же
// действием, которое гейт и делает, — предикат совпал бы с предметом.
const quotaCatalogueFile = "services/iam/internal/domain/limit.go"

var (
	// начало и конец объявления каталога
	quotaCatalogueStartRe = regexp.MustCompile(`(?m)^var countableKinds = \[\]CountableKind\{`)
	// запись каталога: {"vpc.network", CarrierProject} либо {"vpc.network.subnet", "vpc.network"}
	quotaCatalogueEntryRe = regexp.MustCompile(`\{"([a-zA-Z][a-zA-Z.]*)",`)

	// вызов списывающего триггера ЛЮБОЙ формы. Аргументов бывает один
	// (проектный вид) и три (проектный вид, столбец родителя, вложенный вид),
	// причём трёхаргументная форма пишется В НЕСКОЛЬКО СТРОК — поэтому тело
	// скобок берётся целиком, а виды выбираются из него.
	//
	// Первая редакция читала только ПЕРВЫЙ аргумент однострочной формой и
	// недосчитала четыре вида у двух доменов: `loadbalancer.listeners` и
	// `registry.repositories` вместе с их вложенными близнецами. Ошибка тихая —
	// она уменьшает число производителей, то есть УВЕЛИЧИВАЕТ находки, и
	// выглядит как добросовестная строгость.
	quotaCountCallRe      = regexp.MustCompile(`(?s)kacho_quota_count\s*\(([^)]*)\)`)
	quotaCarrierCallRe    = regexp.MustCompile(`(?s)kacho_quota_carrier_lifecycle\s*\(([^)]*)\)`)
	quotaKindLiteralRe    = regexp.MustCompile(`'([a-zA-Z][a-zA-Z.]*)'`)
	quotaSQLLineCommentRe = regexp.MustCompile(`(?m)^\s*--.*$`)
)

// kindsWithoutADebitProducer — ОБЪЯВЛЕННЫЙ ДОЛГ: виды каталога, которые сегодня
// не списывает никто.
//
// Перечень существует не для того, чтобы гейт зеленел, а для того, чтобы
// состояние было ПОИМЕНОВАНО и СЧЁТНО. До этого гейта «вид без производителя»
// не было наблюдаемым состоянием вовсе: строки такого вида просто не заводились,
// и арендатор видел не «потолка нет», а отсутствие вида в ответе.
//
// Запись ИСТЕКАЕТ САМА: как только у вида появляется производитель, запись
// становится находкой и гейт падает, требуя её снять. Держать её «на всякий
// случай» нельзя — это и есть механизм, которым исключение переживает свой
// предмет.
//
// Задача: PRO-Robotech/kacho#414.
var kindsWithoutADebitProducer = map[string]string{
	// У kacho-iam НЕТ таблицы учёта вовсе — ни `project_resource_quotas`, ни
	// триггеров, ни клиента величин к самому себе. Это не «дописать триггер»,
	// а завести владельцу учёт целиком; носитель у всех шести — аккаунт, а не
	// проект, поэтому и материализация у них другая.
	"iam.project":        "#414: у kacho-iam нет таблицы учёта; носитель — аккаунт",
	"iam.user":           "#414: у kacho-iam нет таблицы учёта; носитель — аккаунт",
	"iam.serviceAccount": "#414: у kacho-iam нет таблицы учёта; носитель — аккаунт",
	"iam.group":          "#414: у kacho-iam нет таблицы учёта; носитель — аккаунт",
	"iam.role":           "#414: у kacho-iam нет таблицы учёта; носитель — аккаунт",
	"iam.accessBinding":  "#414: у kacho-iam нет таблицы учёта; носитель — аккаунт",

	// Вложенные виды vpc: механизм носителя (`kacho_quota_carrier_lifecycle` +
	// умолчания вложенных) заведён у nlb и registry, у vpc — нет. До #401 эти
	// четыре ПРИТВОРЯЛИСЬ списываемыми: строка заводилась с носителем `project`,
	// показывала арендатору потребление 0 и не наполнялась никогда. Теперь их
	// просто нет у владельца — и это состояние наблюдаемо, а не замаскировано.
	"vpc.network.subnet":        "#414: у vpc нет механизма носителя вложенности",
	"vpc.network.routeTable":    "#414: у vpc нет механизма носителя вложенности",
	"vpc.network.securityGroup": "#414: у vpc нет механизма носителя вложенности",
}

// TestEveryCatalogueKindHasADebitProducer — у каждого вида каталога есть тот,
// кто списывает под ним место, либо вид стоит в объявленном долге.
func TestEveryCatalogueKindHasADebitProducer(t *testing.T) {
	root := repoRoot(t)

	catalogue := readQuotaCatalogue(t, root)
	if len(catalogue) == 0 {
		t.Fatalf("предпосылка гейта не выполнена: в %s не найдено ни одной записи каталога — "+
			"форма объявления `countableKinds` изменилась, и гейт судит пустоту",
			quotaCatalogueFile)
	}

	produced, filesRead, servicesSeen := readQuotaDebitProducers(t, root)
	if len(produced) == 0 {
		t.Fatal("предпосылка гейта не выполнена: ни в одной миграции не найдено вызова " +
			"списывающего триггера — форма объявления изменилась, и гейт судит пустоту")
	}

	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного», и
	// отдельно — от «прочитали один домен из семи», что и было прежним
	// состоянием этой проверки.
	t.Logf("перепись: видов в каталоге %d; сервисов осмотрено %d; файлов миграций прочитано %d; "+
		"видов со списанием %d; объявленный долг %d",
		len(catalogue), servicesSeen, filesRead, len(produced), len(kindsWithoutADebitProducer))

	var findings []string

	// (а) вид каталога без производителя и без записи в долге.
	for _, kind := range catalogue {
		if produced[kind] {
			continue
		}
		if _, declared := kindsWithoutADebitProducer[kind]; declared {
			continue
		}
		findings = append(findings, "вид «"+kind+"» объявлен каталогом, но не списывается ничем "+
			"и не стоит в объявленном долге: потолок на него сохраняется, отвечает успехом "+
			"и не применяется никогда")
	}

	// (б) запись долга, которой больше нечего исключать. Без этой ветки
	// послабление переживает свой предмет — и следующий читатель унаследует его
	// как описание действительности.
	for kind, why := range kindsWithoutADebitProducer {
		if produced[kind] {
			findings = append(findings, "запись долга «"+kind+"» ("+why+") устарела: "+
				"у вида ПОЯВИЛСЯ производитель списания — снять запись")
		}
	}

	// (в) запись долга про вид, которого в каталоге нет. Каталог закрыт, поэтому
	// такая запись означает либо опечатку, либо вид, снятый с каталога, — в обоих
	// случаях она молча уменьшает предмет проверки.
	for kind := range kindsWithoutADebitProducer {
		if !listsKind(catalogue, kind) {
			findings = append(findings, "запись долга «"+kind+"» не названа каталогом: "+
				"исключается вид, которого нет, — значит исключение шире своего предмета")
		}
	}

	// (г) списывается вид, которого каталог не называет. Потолка у него нет, а
	// место он занимает: триггер пишет строку учёта, которую никто не заводил.
	for kind := range produced {
		if !listsKind(catalogue, kind) {
			findings = append(findings, "вид «"+kind+"» списывается триггером, но каталог его "+
				"НЕ называет: место занимается под потолок, которого не существует")
		}
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("каталог видов и производители списания разошлись (%d):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// TestQuotaKindProducerGate_CanFailAndNamesTheKind — доказательство того, что
// разбор выше способен упасть, и что законная форма его не тревожит.
//
// Без пары «инъекция + законный близнец» гейт ловил бы форму, а не существо:
// первая редакция разбора читала только первый аргумент триггера и недосчитывала
// четыре вида — то есть находила БОЛЬШЕ, чем есть, и выглядела при этом строже.
func TestQuotaKindProducerGate_CanFailAndNamesTheKind(t *testing.T) {
	// Инъекция: трёхаргументная форма, разбитая на строки ровно так, как её
	// пишут в дереве. Разбор обязан достать ОБА вида, а не только первый.
	const nested = `
DROP TRIGGER IF EXISTS listeners_quota_count ON kacho_nlb.listeners;
CREATE TRIGGER listeners_quota_count
    AFTER INSERT OR DELETE ON kacho_nlb.listeners
    FOR EACH ROW EXECUTE FUNCTION kacho_nlb.kacho_quota_count(
        'loadbalancer.listeners',
        'load_balancer_id',
        'loadbalancer.networkLoadBalancers.listeners');`

	got := quotaKindsInSQL(nested)
	for _, want := range []string{"loadbalancer.listeners", "loadbalancer.networkLoadBalancers.listeners"} {
		if !got[want] {
			t.Fatalf("трёхаргументная форма: вид %q не найден, найдено %v", want, quotaKeysOf(got))
		}
	}
	// `load_balancer_id` — имя СТОЛБЦА, а не вид: у него нет точки, и он не
	// смеет попасть в множество списываемых, иначе гейт объявит производителем
	// то, что им не является.
	if got["load_balancer_id"] {
		t.Fatal("имя столбца принято за вид: множество списываемых загрязнено")
	}

	// Законный близнец: однострочная форма по-прежнему читается.
	single := quotaKindsInSQL(
		`FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.network');`)
	if !single["vpc.network"] {
		t.Fatalf("однострочная форма перестала читаться: %v", quotaKeysOf(single))
	}

	// Проза не является объявлением. Комментарий, называющий вид, встречается в
	// этих миграциях постоянно — и он объясняет механизм, а не заводит его.
	prose := quotaKindsInSQL(
		`-- вид пишется как kacho_quota_count('vpc.cidrGroup'), а не по памяти`)
	if len(prose) != 0 {
		t.Fatalf("вид, названный в комментарии, принят за производителя: %v", quotaKeysOf(prose))
	}
}

// readQuotaCatalogue достаёт виды из объявления `countableKinds`.
func readQuotaCatalogue(t *testing.T, root string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, quotaCatalogueFile))
	if err != nil {
		t.Fatalf("каталог видов не прочитан (%s): %v", quotaCatalogueFile, err)
	}
	src := string(b)
	loc := quotaCatalogueStartRe.FindStringIndex(src)
	if loc == nil {
		return nil
	}
	// Тело объявления — до первой строки, состоящей из закрывающей скобки.
	rest := src[loc[1]:]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		rest = rest[:end]
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range quotaCatalogueEntryRe.FindAllStringSubmatch(rest, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// readQuotaDebitProducers обходит миграции ВСЕХ сервисов и собирает виды, под
// которыми кто-то списывает место.
func readQuotaDebitProducers(t *testing.T, root string) (kinds map[string]bool, filesRead, servicesSeen int) {
	t.Helper()
	kinds = map[string]bool{}

	// Состав берётся у индекса дерева, а не обходом диска: под `services/` на
	// машине, где поднимали стенд, лежат распаковки чартов и отчёты прогонов.
	services, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".sql")
	if err != nil {
		t.Fatalf("состав дерева под services/: %v", err)
	}
	svcSeen := map[string]bool{}
	for _, path := range services {
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		// services/<svc>/internal/migrations/<file>.sql
		if len(parts) < 5 || parts[2] != "internal" || parts[3] != "migrations" {
			continue
		}
		b, ferr := os.ReadFile(path)
		if ferr != nil {
			t.Fatalf("чтение %s: %v", rel, ferr)
		}
		filesRead++
		svcSeen[parts[1]] = true
		for k := range quotaKindsInSQL(string(b)) {
			kinds[k] = true
		}
	}
	return kinds, filesRead, len(svcSeen)
}

// quotaKindsInSQL достаёт виды из объявлений списывающих триггеров.
//
// Комментарии снимаются ПЕРВЫМИ: эти миграции подробно объясняют механизм и
// называют виды в прозе постоянно. Гейт, читающий сырой текст, нашёл бы вид в
// комментарии, ОБЪЯСНЯЮЩЕМ его отсутствие, и остался бы зелёным при снятом
// триггере (`testing.md` §«Гейт читает исполняемую часть, а не текст»).
func quotaKindsInSQL(sql string) map[string]bool {
	sql = quotaSQLLineCommentRe.ReplaceAllString(sql, "")
	out := map[string]bool{}
	for _, re := range []*regexp.Regexp{quotaCountCallRe, quotaCarrierCallRe} {
		for _, call := range re.FindAllStringSubmatch(sql, -1) {
			for _, lit := range quotaKindLiteralRe.FindAllStringSubmatch(call[1], -1) {
				// Вид — всегда `<домен>.<ресурс>`; аргумент со столбцом
				// родителя точки не содержит и видом не является.
				if strings.Contains(lit[1], ".") {
					out[lit[1]] = true
				}
			}
		}
	}
	return out
}

func listsKind(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func quotaKeysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
