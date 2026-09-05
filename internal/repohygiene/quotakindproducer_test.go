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

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"

	"github.com/PRO-Robotech/kacho/pkg/platformmodules"
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

// missingPiece — ЧЕГО именно не хватает виду. Закрытый словарь, а не проза.
//
// ПОЧЕМУ НЕ ПРОЗОЙ. Прежняя редакция описывала недостающее свободным текстом, и
// текст пережил свой предмет: он утверждал, что у kaname нет механизма учёта
// вовсе, — при том что таблица, функция счёта и списывающий триггер заведены
// миграцией `484002_account_quota_identity_carrier.sql`. Ошибка не косметическая:
// «механизма нет» ОТГОВАРИВАЕТ от работы, которая на деле сводится к добавлению
// вида и его триггера к уже работающему механизму (задача #733).
//
// Закрытый словарь чинит это by construction: каждая названная часть —
// утверждение О ДЕРЕВЕ, и оно проверяется.
//
// ГРАНИЦА НАЗВАНА, потому что она есть. Частей три, а обходом миграций (ветка
// «е») сверяются ДВЕ: у таблицы учёта и функции счёта есть общий для владельца
// признак, и он в `quotaMechanismPieceProbe`. Третья — списывающий триггер вида
// — такого признака не имеет by construction: он существует не «у владельца», а
// «у вида», и её предмет разбирают ветки (а)/(б) по множеству производителей.
// Завести ей probe здесь значило бы завести второе место об одном предмете,
// которое разойдётся с первым молча.
//
// Следствие, которое обязано быть ВИДНО, а не подразумеваться: пока все записи
// долга называют только триггер вида, ветка (е) не судит ни одной названной
// части. Это законное состояние, а не поломка, — поэтому оно печатается
// переписью, а не роняет прогон, и истекает само на первой же записи,
// назвавшей таблицу или функцию.
type missingPiece string

const (
	// missingAccountingTable — у владельца нет таблицы строк учёта.
	missingAccountingTable missingPiece = "таблица учёта"
	// missingCountingFunction — у владельца нет функции счёта.
	missingCountingFunction missingPiece = "функция счёта"
	// missingKindTrigger — механизм есть, а этого вида он не считает.
	missingKindTrigger missingPiece = "списывающий триггер вида"
)

// quotaDebt — запись объявленного долга: что именно отсутствует и почему это
// состояние, а не упущение.
//
// `Missing` обязателен и непуст: долг, не называющий недостающего, нечем снять —
// он переживёт свой предмет ровно так, как это уже случилось однажды.
type quotaDebt struct {
	Missing []missingPiece
	Why     string
}

// kindsWithoutADebitProducer — ОБЪЯВЛЕННЫЙ ДОЛГ: виды каталога, которые сегодня
// не списывает никто.
//
// Перечень существует не для того, чтобы гейт зеленел, а для того, чтобы
// состояние было ПОИМЕНОВАНО и СЧЁТНО. До этого гейта «вид без производителя»
// не было наблюдаемым состоянием вовсе: строки такого вида просто не заводились,
// и арендатор видел не «потолка нет», а отсутствие вида в ответе.
//
// Запись ИСТЕКАЕТ САМА, и теперь по ДВУМ осям: как только у вида появляется
// производитель, запись становится находкой; и как только названная ею часть
// механизма появляется в дереве, находкой становится сама причина.
//
// Задача: PRO-Robotech/kacho#414.
var kindsWithoutADebitProducer = map[string]quotaDebt{
	// МЕХАНИЗМ УЧЁТА У iam ЕСТЬ, и это измерено, а не предположено: таблица
	// `kaname.project_resource_quotas`, функция `kaname.kacho_quota_count`
	// и списывающий триггер заведены миграцией
	// `484002_account_quota_identity_carrier.sql`, а единственный производитель
	// отказа — миграцией `484001_quota_refusal_single_source.sql`. Вид
	// `iam.account` через них уже списывается.
	//
	// Недостаёт этим шести не механизма, а СЕБЯ в нём: своего вида и своего
	// триггера. Носитель у всех шести — аккаунт (`carrier_type = 'account'`), и
	// эту величину носителя таблица принимает by construction — её ограничение
	// перечисляет `project`, `account` и `identity`, — поэтому работа сводится к
	// добавлению триггера на таблицу ресурса, а не к заведению учёта заново.
	//
	// Здесь три месяца стояло обратное («у kaname нет таблицы учёта вовсе —
	// ни `project_resource_quotas`, ни триггеров, ни клиента величин к самому
	// себе»). Утверждение писалось ДО `484002` и пережило свой предмет. Оно
	// опаснее обычной устаревшей строки, потому что ОТГОВАРИВАЕТ: читатель
	// заключал, что перед ним заведение подсистемы, а не один триггер. Клиента
	// величин к себе у iam нет и НЕ ТРЕБУЕТСЯ отдельно: iam — сам владелец
	// величин, авторитет лежит в этой же базе и читается тем же оператором, что
	// списывает, — догонять нечего. Задача #733.
	"iam.project": {
		Missing: []missingPiece{missingKindTrigger},
		Why:     "#414: механизм учёта у iam есть (484002) — недостаёт вида и его триггера; носитель — аккаунт",
	},
	"iam.user": {
		Missing: []missingPiece{missingKindTrigger},
		Why:     "#414: механизм учёта у iam есть (484002) — недостаёт вида и его триггера; носитель — аккаунт",
	},
	"iam.serviceAccount": {
		Missing: []missingPiece{missingKindTrigger},
		Why:     "#414: механизм учёта у iam есть (484002) — недостаёт вида и его триггера; носитель — аккаунт",
	},
	"iam.group": {
		Missing: []missingPiece{missingKindTrigger},
		Why:     "#414: механизм учёта у iam есть (484002) — недостаёт вида и его триггера; носитель — аккаунт",
	},
	"iam.role": {
		Missing: []missingPiece{missingKindTrigger},
		Why:     "#414: механизм учёта у iam есть (484002) — недостаёт вида и его триггера; носитель — аккаунт",
	},
	"iam.accessBinding": {
		Missing: []missingPiece{missingKindTrigger},
		Why:     "#414: механизм учёта у iam есть (484002) — недостаёт вида и его триггера; носитель — аккаунт",
	},

	// Вложенные виды vpc: механизм носителя (`kacho_quota_carrier_lifecycle` +
	// умолчания вложенных) заведён у nlb и registry, у vpc — нет. До #401 эти
	// четыре ПРИТВОРЯЛИСЬ списываемыми: строка заводилась с носителем `project`,
	// показывала арендатору потребление 0 и не наполнялась никогда. Теперь их
	// просто нет у владельца — и это состояние наблюдаемо, а не замаскировано.
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
	for kind, debt := range kindsWithoutADebitProducer {
		if produced[kind] {
			findings = append(findings, "запись долга «"+kind+"» ("+debt.Why+") устарела: "+
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

	// (д) запись долга обязана НАЗЫВАТЬ недостающее. Долг без перечня нечем
	// снять: он и есть та форма, в которой утверждение переживает свой предмет.
	for kind, debt := range kindsWithoutADebitProducer {
		if len(debt.Missing) == 0 {
			findings = append(findings, "запись долга «"+kind+"» не называет ни одной "+
				"недостающей части: снять её нечем, потому что нечего проверять")
		}
	}

	// (е) причина долга сверяется с ДЕРЕВОМ. Часть, названная недостающей, но
	// заведённая у владельца, — утверждение, пережившее свой предмет: оно
	// отговаривает от работы, которая давно свелась к меньшему (#733).
	mechanisms, mechFiles := readQuotaMechanisms(t, root)
	t.Logf("перепись механизмов учёта: владельцев осмотрено %d; файлов миграций прочитано %d",
		len(mechanisms), mechFiles)
	// Охват сверки СЧЁТЕН: без него «ноль находок» у этой ветки неотличимо от
	// «ноль осмотренного», а сегодня она и впрямь не судит ничего — все записи
	// называют только триггер вида, чей предмет у веток (а)/(б).
	var piecesJudged, piecesDeferred int
	for kind, debt := range kindsWithoutADebitProducer {
		owner := quotaOwnerDirOfKind(kind)
		present, known := mechanisms[owner]
		if !known {
			findings = append(findings, "запись долга «"+kind+"» указывает на владельца «"+owner+
				"», у которого в дереве нет миграций: причина долга не проверяема")
			continue
		}
		// Триггер вида здесь не судится: он уже разобран ветками (а) и (б) —
		// они читают то же множество производителей.
		for _, piece := range debt.Missing {
			if _, judged := quotaMechanismPieceProbe[piece]; judged {
				piecesJudged++
			} else {
				piecesDeferred++
			}
		}
		for _, stale := range staleDebtPieces(debt.Missing, present) {
			findings = append(findings, "причина долга «"+kind+"» устарела: она называет "+
				"недостающей часть «"+string(stale)+"», а у владельца «"+owner+
				"» эта часть ЗАВЕДЕНА — недостаёт только вида и его триггера")
		}
	}
	t.Logf("перепись причин долга: записей %d; частей названо %d, из них сверено обходом %d, "+
		"отдано веткам (а)/(б) %d",
		len(kindsWithoutADebitProducer), piecesJudged+piecesDeferred, piecesJudged, piecesDeferred)
	if piecesJudged == 0 {
		t.Logf("сверкой обходом сегодня не судится НИ ОДНА названная часть: все записи долга "+
			"называют только «%s», чей предмет у веток (а)/(б). Ветка сработает на первой же "+
			"записи, назвавшей «%s» или «%s»",
			missingKindTrigger, missingAccountingTable, missingCountingFunction)
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

// quotaMechanismPieceProbe — как часть механизма учёта опознаётся в миграции.
//
// Комментарии снимаются ДО поиска: эти миграции подробно объясняют механизм и
// называют его части в прозе постоянно. Гейт, читающий сырой текст, нашёл бы
// таблицу в комментарии, объясняющем её отсутствие.
var quotaMechanismPieceProbe = map[missingPiece]*regexp.Regexp{
	missingAccountingTable:  regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?[a-z_]+\.project_resource_quotas`),
	missingCountingFunction: regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+[a-z_]+\.kacho_quota_count`),
}

// quotaOwnerAliasDir — расхождения «первый сегмент вида» ↔ «каталог сервиса».
//
// Совпадение имени с каталогом — правило, а исключения БЕРУТСЯ у словаря имён
// модулей (pkg/platformmodules), а не выписываются: до #1885 то же соответствие
// жило пятью копиями в этом корпусе.
var quotaOwnerAliasDir = platformmodules.AliasesByCatalogModule()

// quotaOwnerDirOfKind — каталог сервиса-владельца по виду.
func quotaOwnerDirOfKind(kind string) string {
	head := kind
	if i := strings.Index(kind, "."); i >= 0 {
		head = kind[:i]
	}
	if alias, ok := quotaOwnerAliasDir[head]; ok {
		return alias
	}
	return head
}

// staleDebtPieces — части, названные долгом недостающими, которые дерево
// показывает СУЩЕСТВУЮЩИМИ.
//
// Триггер вида не судится здесь by construction: у него нет общего для владельца
// признака, и его разбирают ветки (а)/(б) по множеству производителей.
func staleDebtPieces(named []missingPiece, present map[missingPiece]bool) []missingPiece {
	var out []missingPiece
	for _, piece := range named {
		if _, judged := quotaMechanismPieceProbe[piece]; !judged {
			continue
		}
		if present[piece] {
			out = append(out, piece)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// quotaMechanismInSQL — какие части механизма учёта заводит этот текст.
func quotaMechanismInSQL(sql string) map[missingPiece]bool {
	sql = quotaSQLLineCommentRe.ReplaceAllString(sql, "")
	out := map[missingPiece]bool{}
	for piece, re := range quotaMechanismPieceProbe {
		if re.MatchString(sql) {
			out[piece] = true
		}
	}
	return out
}

// readQuotaMechanisms — какие части механизма учёта заведены у каждого владельца.
func readQuotaMechanisms(t *testing.T, root string) (map[string]map[missingPiece]bool, int) {
	t.Helper()
	out := map[string]map[missingPiece]bool{}
	filesRead := 0

	paths, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".sql")
	if err != nil {
		t.Fatalf("состав дерева под services/: %v", err)
	}
	for _, path := range paths {
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		if len(parts) < 5 || parts[2] != "internal" || parts[3] != "migrations" {
			continue
		}
		b, ferr := os.ReadFile(path)
		if ferr != nil {
			t.Fatalf("чтение %s: %v", rel, ferr)
		}
		filesRead++
		svc := parts[1]
		if out[svc] == nil {
			out[svc] = map[missingPiece]bool{}
		}
		for piece := range quotaMechanismInSQL(string(b)) {
			out[svc][piece] = true
		}
	}
	return out, filesRead
}

// TestQuotaDebtReasonProbe_CanFailAndStaysSilentOnTheGenuineGap — доказательство
// того, что сверка причины долга с деревом способна упасть и что законный
// близнец её не тревожит.
//
// Без пары гейт ловил бы форму: перечень частей можно объявить и не проверить
// ни одной, и выглядело бы это строже прежней прозы, оставаясь тем же самым
// необеспеченным утверждением.
func TestQuotaDebtReasonProbe_CanFailAndStaysSilentOnTheGenuineGap(t *testing.T) {
	// Инъекция: текст, ЗАВОДЯЩИЙ обе части. Долг, называющий их недостающими,
	// обязан быть объявлен устаревшим — обе поимённо.
	const real = `
CREATE TABLE IF NOT EXISTS kaname.project_resource_quotas (
    carrier_type text NOT NULL);
CREATE OR REPLACE FUNCTION kaname.kacho_quota_count()
RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END; $$;`

	present := quotaMechanismInSQL(real)
	if !present[missingAccountingTable] || !present[missingCountingFunction] {
		t.Fatalf("разбор не увидел заведённых частей: %v", present)
	}
	stale := staleDebtPieces(
		[]missingPiece{missingAccountingTable, missingCountingFunction, missingKindTrigger}, present)
	if len(stale) != 2 {
		t.Fatalf("устаревшими названы %v, а заведены обе части", stale)
	}

	// Законный близнец 1: владелец, у которого механизма ДЕЙСТВИТЕЛЬНО нет.
	// Долг о нём — верное утверждение, и гейт обязан молчать.
	genuine := quotaMechanismInSQL(`ALTER TABLE kacho_geo.zones ADD COLUMN status text;`)
	if got := staleDebtPieces(
		[]missingPiece{missingAccountingTable, missingCountingFunction}, genuine); len(got) != 0 {
		t.Fatalf("настоящий пробел объявлен устаревшим долгом: %v", got)
	}

	// Законный близнец 2: проза, называющая части. Комментарий, ОБЪЯСНЯЮЩИЙ
	// отсутствие учёта, не заводит его — иначе гейт зеленел бы на собственном
	// разборе (`testing.md` §«Гейт читает исполняемую часть, а не текст»).
	prose := quotaMechanismInSQL(
		`-- у владельца нет ни kaname.project_resource_quotas, ни FUNCTION kaname.kacho_quota_count`)
	if len(prose) != 0 {
		t.Fatalf("часть, названная в комментарии, принята за заведённую: %v", prose)
	}

	// Законный близнец 3: часть, которую эта сверка не судит вовсе. Триггер вида
	// разбирают ветки (а)/(б); называть его здесь устаревшим было бы вторым
	// местом об одном предмете.
	if got := staleDebtPieces([]missingPiece{missingKindTrigger}, present); len(got) != 0 {
		t.Fatalf("триггер вида судится сверкой причины, хотя его предмет у соседа: %v", got)
	}

	// Соответствие каталога сервису — тоже утверждение, и оно проверяется.
	if got := quotaOwnerDirOfKind("loadbalancer.listeners"); got != "nlb" {
		t.Fatalf("расхождение имени домена и каталога не учтено: %q", got)
	}
	if got := quotaOwnerDirOfKind("iam.project"); got != "iam" {
		t.Fatalf("владелец вида определён неверно: %q", got)
	}
}
