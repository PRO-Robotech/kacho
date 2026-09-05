// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// terraformdatasourcecoverage_test.go — гейт на класс: КАЖДЫЙ читаемый сервис контрактов
// представлен источником данных провайдера.
//
// # Предмет — вторая слепая зона гейтов покрытия
//
// Соседний гейт (`terraformresourceparity_test.go`) спрашивает «каждый ли СОЗДАВАЕМЫЙ ресурс
// управляем» и делает это добросовестно. Но его перепись выводится из СОЗДАЮЩЕГО глагола, а
// справочник платформы (регион, зона, тип диска, тип машины) создающего глагола не имеет by
// construction: его строки заводит администратор облака, а не арендатор. Значит справочник в
// ту перепись не попадает — и его отсутствие в провайдере не роняет НИЧЕГО. Собственный
// комментарий соседа это прямо говорит («их место в источниках данных»), но сказанное в
// комментарии ничего не держит: оно переживёт и себя, и свой предмет.
//
// Цена пропуска не косметическая. Размещаемый ресурс обязан назвать зону или регион
// (`zone_id`/`region_id`), машина — тип, том — тип диска. Без источника данных пользователь
// ВПИСЫВАЕТ идентификатор справочника литералом: конфигурация перестаёт переноситься между
// установками платформы, а опечатка выясняется отказом края на `apply`, когда половина
// ландшафта уже создана, — вместо `plan`.
//
// # Свойство, которое гейт требует
//
// Публичный сервис контрактов, у которого ЕСТЬ чтение ресурса и НЕТ создающего глагола, обязан
// быть представлен источником данных, ДОСТИЖИМЫМ из реестра провайдера.
//
// Достижимость, а не объявление: файл, объявляющий источник, но не провязанный в
// `DataSources()`, для пользователя не существует. Обход «от файлов к реестру» счёл бы его
// существующим — поэтому обход идёт ОТ реестра.
//
// # Чем этот гейт связан со своим соседом
//
// Состав контрактов, разбор тела сервиса и словарь создающих глаголов берутся ТЕМИ ЖЕ
// предикатами, что у соседа по ресурсам (`trackedFilesUnder`, `serviceBody`, `creatingVerbs`),
// и намеренно не переписываются: два обходчика одного предмета разошлись бы молча — и
// разошлись бы именно там, где расхождение не видно, потому что на полном дереве оба
// возвращают «полно».
//
// # Чего этот гейт НЕ требует, и почему это решение, а не упущение
//
// У соседа есть обратная сверка «ресурс провайдера без создающего сервиса — находка». Здесь
// такой сверки нет НАМЕРЕННО, и причина в самом предмете: у ресурса соответствие с сервисом
// один-к-одному, а чтение законно приходит НЕСКОЛЬКИМИ формами на один сервис — одиночная,
// списочная, поиск по имени поверх управляемого ресурса. Таблица, претендующая перечислить их
// все, описывала бы нашу расторопность, а не дерево: каждая новая законная форма делала бы
// гейт красным до правки таблицы, то есть красный перестал бы означать дефект.
//
// Что от обратной сверки сохранено там, где она осмысленна: у каждого ИСКЛЮЧЕНИЯ названы имена,
// появление которых его опровергает (`refutedBy`). Это точная тревога без домыслов о
// словообразовании — и она честно ограничена: источник над исключённым сервисом, названный
// как-то ещё, мимо неё пройдёт. Морфологию в гейт не зашивают: предикат, угадывающий
// множественное число, ловит форму, а не существо.
//
// Переименование источника при этом ловится ПРЯМОЙ сверкой: таблица называет имена, и ни одно
// не найдётся в реестре — сервис окажется непредставленным.
//
// # Что гейт умеет уронить
//
//  1. читаемый сервис без записи в таблице соответствия — новый справочник приезжает
//     непокрытым и молча;
//  2. запись, ни одно имя которой не найдено в реестре провайдера, — таблица описывает
//     намерение, а не дерево;
//  3. запись, чей сервис из контрактов исчез или перестал быть читаемым, — таблица копит
//     мёртвые строки, и её полнота перестаёт что-либо значить;
//  4. исключение, чей сервис исчез из переписи, — исключению нечего исключать;
//  5. исключение, чьё опровергающее имя ПОЯВИЛОСЬ в реестре, — причина опровергнута громко,
//     а не молча переписана;
//  6. глагол публичного НЕсоздающего сервиса, похожий на чтение (`Get…`/`List…`) и не
//     отнесённый ни к чтению ресурса, ни к объявленным исключениям, — иначе слепое пятно
//     повторится ровно так, как оно повторилось у соседа с глаголом `Invite`: перепись,
//     выведенная из угаданного имени, молча теряет целый вид предмета;
//  7. запись словаря глаголов, которой больше нечего классифицировать;
//  8. запись реестра, форму которой предикат не знает, — гейт называет её по имени и
//     краснеет, вместо того чтобы объявить непокрытым то, что покрыто.
//
// # Красный гейт на пустом реестре — это исход, а не поломка
//
// Гейт пишется как СВОЙСТВО дерева, а не как описание его нынешнего состояния: пока источников
// нет, он красный, и это верно. Отличать надо «реестр пуст» от «реестра нет»: второе означает
// переехавший предикат и роняет гейт отказом (`Fatal`), первое даёт находку на каждый
// непредставленный сервис. Оба случая названы в переписи разными числами, поэтому «ноль
// находок» отличимо от «ноль прочитанного».

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// dsReadingVerbs — глаголы, читающие РЕСУРС.
//
// Перечень явный, потому что имя глагола угадать нельзя, и цена угадывания измерена: у
// соседнего гейта пользователь заводится глаголом `Invite`, и первая редакция того гейта не
// увидела целый вид ресурса. Здесь та же ловушка с другой стороны — читать можно и глаголом
// `ListPermissionCatalog`.
var dsReadingVerbs = map[string]bool{
	"Get": true, "List": true, "ListPermissionCatalog": true,
}

// dsNotAResourceRead — глаголы, ПОХОЖИЕ на чтение и чтением ресурса НЕ являющиеся.
//
// Запись здесь — не послабление, а классификация: гейт обязан отличать «читает коллекцию
// объектов» от «отвечает про вызывающего». Без явного перечня оба вида слились бы в один, и
// выбор остался бы только между ложным требованием источника над решением о правах и
// молчаливой потерей настоящего справочника.
//
// Классификация ограничена глаголами публичных НЕсоздающих сервисов: у создающих перепись
// ведёт сосед, и там эти имена ничего не решают.
//
// Запись САМОИСТЕКАЕТ: глагол, которого больше нет ни у одного такого сервиса, — находка.
var dsNotAResourceRead = map[string]string{
	"ListSubjects": "кто имеет доступ к объекту — ответ о ПРАВАХ, а не о ресурсе: он меняется " +
		"от выдачи привязки, которой в конфигурации нет, и зависит от того, чьим токеном " +
		"настроен провайдер",
}

// tfDataSources — соответствие читаемого сервиса именам источников данных провайдера.
//
// Имён у одного сервиса бывает несколько, потому что чтение законно приходит несколькими
// формами: одиночная (взять запись по идентификатору) и списочная (перечислить справочник).
// Требуется ЛЮБАЯ из них: свойство, которое гейт защищает, — «справочник читается из
// провайдера», а не «читается именно поштучно». Требовать одиночную форму поимённо значило бы
// вписать в гейт вкус вместо свойства.
//
// Ни одно имя не найдено в реестре — сервис не представлен (находка 2).
var tfDataSources = map[string][]string{
	// geo — ось размещения. Её читает КАЖДЫЙ размещаемый ресурс: без источника идентификатор
	// зоны попадает в конфигурацию литералом.
	"RegionService": {"kacho_geo_region", "kacho_geo_regions"},
	"ZoneService":   {"kacho_geo_zone", "kacho_geo_zones"},

	// каталоги форм — их называют при создании машины и тома.
	"MachineTypeService": {"kacho_compute_machine_type", "kacho_compute_machine_types"},
	"DiskTypeService":    {"kacho_storage_disk_type", "kacho_storage_disk_types"},
}

// dsExemption — читаемый сервис, которому источник данных НЕ ПОЛОЖЕН.
//
// refutedBy — имена, появление которых в реестре ОПРОВЕРГАЕТ причину. Без них исключение
// опровергалось бы молча: кто-то завёл бы источник, причина осталась бы в тексте, и гейт
// продолжал бы её предъявлять как действующее решение.
type dsExemption struct {
	refutedBy []string
	why       string
}

// tfDataSourceExempt — исключения и причина ПО СУЩЕСТВУ.
//
// Здесь не тикет и не долг: тикет означал бы «мы это ещё напишем», жил бы вечно и превратил бы
// гейт в перечень намерений. Запись здесь означает другое — источник над этим сервисом НЕВЕРЕН
// по свойствам самого сервиса, и это свойство от нашей занятости не зависит.
//
// Запись САМОИСТЕКАЕТ в обе стороны: сервис ушёл из переписи — исключению нечего исключать
// (находка 4); опровергающее имя появилось в реестре — причина опровергнута (находка 5).
var tfDataSourceExempt = map[string]dsExemption{
	"QuotaService": {
		refutedBy: []string{"kacho_vpc_quota", "kacho_vpc_quotas", "kacho_quota", "kacho_quotas"},
		why: "квота — не объект ландшафта, а ДВЕ величины о нём: разрешено и занято. Занятое " +
			"меняется САМО, каждой чужой мутацией в том же проекте, поэтому источник над ним " +
			"делал бы план несходящимся — следующий `plan` показывал бы изменение, которого " +
			"никто не вносил. И воспользоваться прочитанным в конфигурации нечем: условие " +
			"«создавать, пока есть место» решается на плане, а применяется позже, то есть " +
			"вводит гонку ровно там, где предел и должен сработать. Разрешённую величину " +
			"назначает администратор облака на внутреннем слушателе — она не в поверхности " +
			"провайдера by construction. Опровергается появлением любого из названных имён: " +
			"завели источник — причина неверна, и запись обязана уйти.",
	},

	"IdentityQuotaService": {
		refutedBy: []string{"kacho_identity_quota", "kacho_identity_quotas"},
		why: "то же, что у `QuotaService`, и с одним усилением. Занятое меняется САМО — здесь " +
			"даже не чужой мутацией в проекте, а любым действием того же человека в ЛЮБОМ его " +
			"аккаунте, — поэтому источник делал бы план несходящимся тем вернее. Плюс предмет " +
			"чтения здесь — сам вызывающий: конфигурация ландшафта описывает ресурсы, а не " +
			"того, кто её применяет, и источник над «мной» описывал бы учётную запись " +
			"исполнителя, то есть менял бы план при смене исполнителя. Опровергается " +
			"появлением любого из названных имён.",
	},

	"OperationService": {
		refutedBy: []string{"kacho_operation", "kacho_operations"},
		why: "операция — конверт МУТАЦИИ, а не объект ландшафта. Её идентификатор рождается " +
			"применением и не называется в конфигурации никем, поэтому ссылаться источником не " +
			"на что; дожидается операции провайдер сам, внутри каждого ресурса. И состояние " +
			"операции меняется САМО, без правки конфигурации: источник над ней делал бы план " +
			"несходящимся — следующий `plan` показывал бы изменение, которого никто не вносил.",
	},

	"MembershipService": {
		refutedBy: []string{"kaname_membership", "kaname_memberships"},
		why: "членство — СВЯЗЬ, чьё состояние меняет событие вне всякой конфигурации: первый " +
			"вход человека переводит в «состоит» ВСЕ его приглашённые членства разом, и ни " +
			"один `apply` при этом не исполняется. Источник над ним делал бы план " +
			"несходящимся ровно как у квоты — следующий `plan` показывал бы изменение, " +
			"которого никто не вносил, причём здесь это не «занятое», а собственное поле " +
			"состояния ресурса. Второе, самостоятельное: адресовать источник нечем. " +
			"Идентификатор членства не чеканится, а ВЫЧИСЛЯЕТСЯ из пары «человек × аккаунт» " +
			"и потому ПЕРЕИСПОЛЬЗУЕТСЯ — снятие есть удаление строки, повторное приглашение " +
			"возвращает тот же идентификатор, — то есть он не различает «ту же связь» и " +
			"«заведённую заново», и в конфигурации его никто не пишет: продукт нигде не " +
			"показывает его как адрес. Естественная координата — пара, а пара это не чтение " +
			"по идентификатору, а список с термом. Ссылаться источнику при этом не на что: " +
			"глаголов создания, правки и снятия у ресурса на этой поверхности нет ни одного " +
			"— связь заводит приглашение и снимает исключение из аккаунта, оба принадлежат " +
			"потоку человека. Опровергается появлением любого из названных имён.",
	},

	"PermissionCatalogService": {
		refutedBy: []string{"kaname_permission_catalog", "kaname_permissions"},
		why: "каталог прав — словарь САМОЙ ПЛАТФОРМЫ, а не ландшафт арендатора: он генерируется " +
			"из контрактов, ставится байт-в-байт одинаковым в посев iam и в посредник края и " +
			"меняется вместе с версией платформы. Источник над ним втянул бы версию платформы в " +
			"состояние: обновили край — план показывает изменение, которого никто не вносил. " +
			"Проверять существование права конфигурации незачем — несуществующее право " +
			"отвергается краем синхронно и с именем поля при выдаче привязки.",
	},
}

func TestEveryReadOnlyAPIServiceHasATerraformDataSource(t *testing.T) {
	root := repoRoot(t)

	census := readOnlyServiceCensus(t, root)
	if census.publicServices == 0 {
		t.Fatal("публичных сервисов в контрактах не найдено — предикат переписи устарел или " +
			"каталог proto переехал; ноль здесь означает сломанный обходчик, а не чистое дерево")
	}
	if len(census.readable) == 0 {
		t.Fatal("читаемых сервисов без создающего глагола не найдено — предикат устарел: " +
			"справочники платформы (регион, зона, тип диска, тип машины) существуют, и ноль " +
			"здесь означает, что их перестали видеть, а не что их не стало")
	}

	dataSources, dsEntries, providerFiles := registeredTerraformDataSources(t, root)

	// (6) глагол, похожий на чтение и не классифицированный.
	sort.Strings(census.unclassified)
	for _, v := range census.unclassified {
		t.Errorf("глагол %s похож на чтение, но не отнесён ни к чтению ресурса (dsReadingVerbs), "+
			"ни к объявленным исключениям (dsNotAResourceRead) — классифицируйте его, иначе "+
			"читаемый сервис проедет мимо переписи молча", v)
	}

	// (7) словарь глаголов самоистекает.
	for _, v := range dsSortedKeys(dsNotAResourceRead) {
		if !census.verbsSeen[v] {
			t.Errorf("глагол %s объявлен не-чтением, но его нет ни у одного публичного "+
				"несоздающего сервиса — записи нечего классифицировать, снимите её", v)
		}
	}

	inCensus := map[string]bool{}
	for _, s := range census.readable {
		inCensus[s] = true
	}

	// (1)(2) прямая сверка: читаемый сервис → источник в реестре.
	covered := 0
	for _, svc := range census.readable {
		if _, exempt := tfDataSourceExempt[svc]; exempt {
			continue
		}
		names, ok := tfDataSources[svc]
		if !ok || len(names) == 0 {
			t.Errorf("сервис %s читается, но записи в таблице источников нет: заведите источник "+
				"данных провайдера либо внесите сервис в tfDataSourceExempt с причиной ПО СУЩЕСТВУ "+
				"(почему источник над ним неверен, а не почему его ещё не написали)", svc)
			continue
		}
		found := false
		for _, n := range names {
			if dataSources[n] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("сервис %s читается, но ни одно из имён %s не провязано в реестре провайдера "+
				"(DataSources): пользователю нечем прочитать этот справочник, и идентификатор "+
				"придётся вписывать в конфигурацию литералом", svc, strings.Join(names, ", "))
			continue
		}
		covered++
	}

	// (3) запись таблицы пережила свой сервис.
	for _, svc := range dsSortedKeys(tfDataSources) {
		if !inCensus[svc] {
			t.Errorf("запись таблицы пережила свой сервис: %s больше не является читаемым "+
				"сервисом контрактов (переименован, обзавёлся создающим глаголом или его чтение "+
				"переклассифицировано) — снимите строку, иначе полнота таблицы ничего не значит", svc)
		}
		if ex, dup := tfDataSourceExempt[svc]; dup {
			t.Errorf("сервис %s стоит одновременно в таблице источников и в исключениях — два "+
				"утверждения об одном предмете, из которых верно одно (%s)", svc, ex.why)
		}
	}

	// (4)(5) исключения самоистекают в обе стороны.
	for _, svc := range dsSortedKeys(tfDataSourceExempt) {
		ex := tfDataSourceExempt[svc]
		if !inCensus[svc] {
			t.Errorf("исключению нечего исключать: %s не является читаемым сервисом контрактов — "+
				"запись пережила свой предмет и с этого дня прикрывает пустоту", svc)
		}
		if len(ex.refutedBy) == 0 {
			t.Errorf("исключение %s не называет ни одного имени, появление которого его "+
				"опровергает, — такую запись нечем опровергнуть, и она переживёт свою причину", svc)
		}
		for _, name := range ex.refutedBy {
			if dataSources[name] {
				t.Errorf("источник %s появился в реестре, а сервис %s объявлен исключением — "+
					"причина исключения опровергнута деревом (%s); снимите запись и внесите "+
					"сервис в таблицу, а не молчите", name, svc, ex.why)
			}
		}
	}

	t.Logf("осмотрено: файлов контрактов %d, публичных сервисов %d, из них без создающего "+
		"глагола %d, читаемых %d; файлов провайдера %d, записей в реестре источников %d",
		census.protoFiles, census.publicServices, census.nonCreating, len(census.readable),
		providerFiles, dsEntries)
	t.Logf("перепись покрытия: читаемых сервисов %d, представлено источниками %d, исключено %d; "+
		"имён источников выведено из реестра %d",
		len(census.readable), covered, len(tfDataSourceExempt), len(dataSources))
	if gap := len(census.readable) - covered - len(tfDataSourceExempt); gap > 0 {
		t.Logf("НЕ ПРЕДСТАВЛЕНО ИСТОЧНИКАМИ: %d сервисов — предмет назван находками выше", gap)
	}
}

func dsSortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- перепись контрактов -------------------------------------------------------------------

// dsServiceCensus — перепись контрактов и объём осмотренного.
//
// Числа возвращаются вместе с находками намеренно: «ноль находок» обязано быть отличимо от
// «ноль прочитанного», а обходчик, не нашедший ни одного файла, обязан выглядеть иначе, чем
// обходчик, прочитавший дерево и ничего не нашедший.
type dsServiceCensus struct {
	readable       []string        // публичные сервисы с чтением и без создающего глагола
	unclassified   []string        // глаголы, похожие на чтение и не отнесённые ни к чему
	verbsSeen      map[string]bool // все глаголы публичных несоздающих сервисов
	protoFiles     int
	publicServices int
	nonCreating    int
}

// readOnlyServiceCensus — перепись по ИНДЕКСУ дерева контрактов, а не по диску.
func readOnlyServiceCensus(t *testing.T, root string) dsServiceCensus {
	t.Helper()
	out := dsServiceCensus{verbsSeen: map[string]bool{}}
	for _, rel := range trackedFilesUnder(t, root, "proto/kacho/cloud", ".proto") {
		src, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		out.protoFiles++
		pub, nonCreating, readable, unclassified, verbs := classifyProtoServices(string(src))
		out.publicServices += pub
		out.nonCreating += nonCreating
		out.readable = append(out.readable, readable...)
		out.unclassified = append(out.unclassified, unclassified...)
		for _, v := range verbs {
			out.verbsSeen[v] = true
		}
	}
	sort.Strings(out.readable)
	out.unclassified = dsDedup(out.unclassified)
	return out
}

// classifyProtoServices — разбор ТЕКСТА контрактов, отделённый от чтения дерева.
//
// Функция чистая ради пробы: свойство «созидающий сервис в перепись не попадает» проверяется на
// литерале с законным и незаконным близнецами, а не на живом дереве, где сегодня оба случая
// просто есть. Проба на дереве утверждала бы о дереве, а не о предикате.
func classifyProtoServices(s string) (publicServices, nonCreating int, readable, unclassified, verbs []string) {
	for _, m := range reService.FindAllStringSubmatchIndex(s, -1) {
		name := s[m[2]:m[3]]
		// Internal-сервисы исключены НАМЕРЕННО и той же причиной, что у соседа: их поверхность —
		// админский каталог на cluster-internal слушателе, арендатору она не адресована, и
		// провайдер до неё не дотягивается вовсе.
		if strings.HasPrefix(name, "Internal") {
			continue
		}
		publicServices++

		var own []string
		creating := false
		for _, v := range reAnyRPC.FindAllStringSubmatch(serviceBody(s, m[1]), -1) {
			own = append(own, v[1])
			if creatingVerbs[v[1]] {
				creating = true
			}
		}
		if creating {
			continue
		}
		nonCreating++
		verbs = append(verbs, own...)

		reads := false
		for _, v := range own {
			switch {
			case dsReadingVerbs[v]:
				reads = true
			case dsNotAResourceRead[v] != "":
			case strings.HasPrefix(v, "Get"), strings.HasPrefix(v, "List"):
				// Похоже на чтение и не классифицировано. Молчать здесь значит вернуть ту самую
				// слепую зону, ради которой гейт написан.
				unclassified = append(unclassified, v)
			}
		}
		if reads {
			readable = append(readable, name)
		}
	}
	return publicServices, nonCreating, readable, unclassified, verbs
}

// ---- реестр источников провайдера ------------------------------------------------------------

// registeredTerraformDataSources — имена источников, ДОСТИЖИМЫЕ из реестра провайдера, число
// записей самого реестра и число прочитанных файлов.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: текстовый поиск нашёл бы имя источника
// в комментарии, который его же и объясняет («NewGeoRegionDataSource — kacho_geo_region»), и
// объявил бы покрытым то, чего в реестре нет. Ровно этот класс мы ловим в продуктовом коде, и
// такие комментарии в провайдере уже есть.
//
// Возвращается ТРИ числа, и это не удобство. «Реестр пуст» и «реестра нет» — разные состояния с
// разными исходами: первое законно (источников ещё не написали) и даёт находки по каждому
// непредставленному сервису; второе означает, что предикат устарел, и роняет гейт отказом. Одно
// число их не различает.
func registeredTerraformDataSources(t *testing.T, root string) (names map[string]bool, entries, files int) {
	t.Helper()
	sources := map[string]string{}
	for _, rel := range trackedFilesUnder(t, root, "terraform/internal/provider", ".go") {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		sources[rel] = string(src)
	}
	names, entries, found, unresolved := dataSourceRegistryNames(t, sources)
	if !found {
		t.Fatal("метод DataSources провайдера не найден — предикат переписи устарел или реестр " +
			"переехал; здесь это отказ, а не «источников нет»: пустой реестр и отсутствующий " +
			"реестр обязаны выглядеть по-разному")
	}
	// (8) незнакомая форма записи роняет гейт ПО ИМЕНИ, а не превращается в тихое «не покрыто».
	for _, e := range unresolved {
		t.Errorf("реестр источников содержит запись %s, имя источника из которой предикат не "+
			"вывел. Предикат знает три формы: литерал в самом источнике; описание с полем "+
			"tfName; имя-суффикс полем описания, читаемым методом Metadata своего типа. "+
			"Появилась четвёртая — научите гейт, иначе он объявит непокрытым то, что покрыто", e)
	}
	return names, entries, len(sources)
}

// dsSources — разобранный пакет провайдера: то, что нужно для прохода от реестра к именам.
type dsSources struct {
	funcs   map[string]*ast.FuncDecl // объявления функций верхнего уровня
	vars    map[string]ast.Expr      // значения переменных верхнего уровня (описания)
	methods map[string][]*ast.FuncDecl
}

// dataSourceRegistryNames — имена источников из готовых исходников: чистая функция ради пробы.
func dataSourceRegistryNames(t *testing.T, sources map[string]string) (names map[string]bool, entries int, found bool, unresolved []string) {
	t.Helper()
	fset := token.NewFileSet()
	pkg := dsSources{
		funcs:   map[string]*ast.FuncDecl{},
		vars:    map[string]ast.Expr{},
		methods: map[string][]*ast.FuncDecl{},
	}
	var registry *ast.FuncDecl

	// Детерминизм входа — часть контракта проверки: обход карты дал бы разный порядок разбора
	// и, на неоднозначном дереве, разный вердикт от прогона к прогону.
	for _, name := range dsSortedKeys(sources) {
		file, err := parser.ParseFile(fset, name, sources[name], parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					pkg.funcs[d.Name.Name] = d
					continue
				}
				recv := dsRecvTypeName(d.Recv)
				pkg.methods[recv] = append(pkg.methods[recv], d)
				if recv == "kachoProvider" && d.Name.Name == "DataSources" {
					registry = d
				}
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, id := range vs.Names {
						if i < len(vs.Values) {
							pkg.vars[id.Name] = vs.Values[i]
						}
					}
				}
			}
		}
	}
	if registry == nil {
		return nil, 0, false, nil
	}

	names = map[string]bool{}
	for _, el := range dsRegistryElements(registry) {
		entries++
		got := dsNamesOfEntry(el, pkg)
		if len(got) == 0 {
			unresolved = append(unresolved, dsEntryLabel(el))
			continue
		}
		for _, n := range got {
			names[n] = true
		}
	}
	sort.Strings(unresolved)
	return names, entries, true, unresolved
}

// dsRegistryElements — элементы возвращаемого реестром составного литерала.
//
// `return nil` даёт НОЛЬ элементов, и это законный ответ: реестр есть, источников в нём нет.
func dsRegistryElements(fd *ast.FuncDecl) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range ret.Results {
			if lit, ok := r.(*ast.CompositeLit); ok {
				out = append(out, lit.Elts...)
			}
		}
		return true
	})
	return out
}

// dsNamesOfEntry — имена источников, выводимые из ОДНОЙ записи реестра.
//
// Три формы, все три настоящие:
//
//   - конструктор `NewXxxDataSource`, объявляющий имя литералом в своём Metadata;
//   - сборка из описания прямо в реестре (`newFlatDataSource(xxxSpec)`) — имя лежит полем
//     `tfName` описания;
//   - конструктор, возвращающий общий тип, собранный фабрикой из описания
//     (`newCatalogOne(geoRegionCatalog)()`) — имени в тексте конструктора НЕТ ВООБЩЕ: его
//     читает Metadata общего типа из ПОЛЯ описания. Здесь и лежит ловушка: разбор по файлу или
//     по описанию целиком приписал бы одиночному источнику ещё и списочное имя, потому что оба
//     суффикса живут в одном описании. Поэтому имя поля берётся у METADATA КОНКРЕТНОГО ТИПА, а
//     значение — из описания по этому полю.
func dsNamesOfEntry(el ast.Expr, pkg dsSources) []string {
	switch e := el.(type) {
	case *ast.Ident:
		fd := pkg.funcs[e.Name]
		if fd == nil {
			return nil
		}
		return dsNamesFromConstructor(fd, pkg)
	case *ast.CallExpr:
		var out []string
		for _, spec := range dsSpecValuesIn(e, pkg) {
			out = append(out, dsLiteralNamesIn(spec)...)
		}
		return dsDedupPlain(out)
	}
	return nil
}

func dsNamesFromConstructor(fd *ast.FuncDecl, pkg dsSources) []string {
	// Форма 1: имя литералом где-то в самом конструкторе.
	out := dsLiteralNamesIn(fd.Body)

	specs := dsSpecValuesIn(fd.Body, pkg)

	// Тип источника: либо возвращён конструктором прямо, либо фабрикой, которую он зовёт.
	types := dsReturnedTypeNames(fd.Body)
	for _, callee := range dsCalleeFuncsIn(fd.Body, pkg) {
		types = append(types, dsReturnedTypeNames(callee.Body)...)
	}

	for _, typ := range dsDedupPlain(types) {
		for _, m := range pkg.methods[typ] {
			if m.Name.Name != "Metadata" {
				continue
			}
			out = append(out, dsLiteralNamesIn(m.Body)...)
			for _, field := range dsTypeNameFieldsIn(m.Body) {
				for _, spec := range specs {
					if s := dsQualify(dsFieldString(spec, field)); s != "" {
						out = append(out, s)
					}
				}
			}
		}
	}

	// Запасной путь: описание с полем tfName, если тип имени не назвал.
	if len(out) == 0 {
		for _, spec := range specs {
			out = append(out, dsLiteralNamesIn(spec)...)
		}
	}
	return dsDedupPlain(out)
}

// dsLiteralNamesIn — имена вида `kacho_*`, объявленные ЛИТЕРАЛОМ в поддереве.
func dsLiteralNamesIn(n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(x ast.Node) bool {
		switch v := x.(type) {
		case *ast.BinaryExpr:
			if v.Op != token.ADD {
				return true
			}
			if sel, ok := v.X.(*ast.SelectorExpr); !ok || sel.Sel.Name != "ProviderTypeName" {
				return true
			}
			if s := dsQualify(stringLit(v.Y)); s != "" {
				out = append(out, s)
			}
		case *ast.KeyValueExpr:
			if k, ok := v.Key.(*ast.Ident); !ok || k.Name != "tfName" {
				return true
			}
			if s := stringLit(v.Value); strings.HasPrefix(s, "kacho_") {
				out = append(out, s)
			}
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "TypeName" || i >= len(v.Rhs) {
					continue
				}
				if s := stringLit(v.Rhs[i]); strings.HasPrefix(s, "kacho_") {
					out = append(out, s)
				}
			}
		}
		return true
	})
	return out
}

// dsTypeNameFieldsIn — имена ПОЛЕЙ описания, из которых Metadata собирает имя источника
// (`resp.TypeName = req.ProviderTypeName + d.spec.nameMany` → `nameMany`).
func dsTypeNameFieldsIn(n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(x ast.Node) bool {
		bin, ok := x.(*ast.BinaryExpr)
		if !ok || bin.Op != token.ADD {
			return true
		}
		if sel, ok := bin.X.(*ast.SelectorExpr); !ok || sel.Sel.Name != "ProviderTypeName" {
			return true
		}
		if sel, ok := bin.Y.(*ast.SelectorExpr); ok {
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	return out
}

// dsFieldString — строковое значение поля составного литерала-описания.
func dsFieldString(spec ast.Expr, field string) string {
	lit, ok := spec.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if k, ok := kv.Key.(*ast.Ident); ok && k.Name == field {
			return stringLit(kv.Value)
		}
	}
	return ""
}

// dsQualify — суффикс имени приводится к полному имени источника.
func dsQualify(s string) string {
	switch {
	case strings.HasPrefix(s, "_"):
		return "kacho" + s
	case strings.HasPrefix(s, "kacho_"):
		return s
	default:
		return ""
	}
}

// dsSpecValuesIn — значения переменных-описаний, переданных аргументами вызовов в поддереве.
func dsSpecValuesIn(n ast.Node, pkg dsSources) []ast.Expr {
	var out []ast.Expr
	seen := map[string]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			id, ok := arg.(*ast.Ident)
			if !ok || seen[id.Name] {
				continue
			}
			if v := pkg.vars[id.Name]; v != nil {
				seen[id.Name] = true
				out = append(out, v)
			}
		}
		return true
	})
	return out
}

// dsCalleeFuncsIn — функции пакета, вызываемые в поддереве (фабрики источников).
func dsCalleeFuncsIn(n ast.Node, pkg dsSources) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	seen := map[string]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || seen[id.Name] {
			return true
		}
		if fd := pkg.funcs[id.Name]; fd != nil {
			seen[id.Name] = true
			out = append(out, fd)
		}
		return true
	})
	return out
}

// dsReturnedTypeNames — имена типов, возвращаемых поддеревом (`return &xxx{}`), включая
// возвраты изнутри функциональных литералов: фабрика возвращает функцию, а та — источник.
func dsReturnedTypeNames(n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(x ast.Node) bool {
		ret, ok := x.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range ret.Results {
			expr := r
			if u, ok := expr.(*ast.UnaryExpr); ok && u.Op == token.AND {
				expr = u.X
			}
			lit, ok := expr.(*ast.CompositeLit)
			if !ok {
				continue
			}
			if id, ok := lit.Type.(*ast.Ident); ok {
				out = append(out, id.Name)
			}
		}
		return true
	})
	return out
}

func dsRecvTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	typ := recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// dsEntryLabel — как назвать нераспознанную запись реестра в тексте находки.
//
// Находка без координаты бесполезна: «предикат не вывел имя» не говорит читателю, куда смотреть.
func dsEntryLabel(el ast.Expr) string {
	switch e := el.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok {
			args := make([]string, 0, len(e.Args))
			for _, a := range e.Args {
				switch v := a.(type) {
				case *ast.Ident:
					args = append(args, v.Name)
				default:
					if s := stringLit(a); s != "" {
						args = append(args, strconv.Quote(s))
					} else {
						args = append(args, "…")
					}
				}
			}
			return id.Name + "(" + strings.Join(args, ", ") + ")"
		}
	}
	return "запись неизвестной формы"
}

func dsDedup(in []string) []string {
	return dsDedupPlain(in)
}

func dsDedupPlain(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// ---- пробы предпосылок -------------------------------------------------------------------

// TestDataSourceRegistryReadsTheRegistryNotTheDeclaration — предпосылка предиката, проверенная
// в ОБЕ стороны на одном литерале.
//
// Отрицание («непровязанный источник не считается зарегистрированным») в одиночку зеленеет и на
// разборе, который не находит вообще ничего. Поэтому рядом стоит положительный контроль: все
// три формы записи, будучи провязанными, обязаны найтись. Законный и незаконный близнецы
// отличаются ровно одним признаком — строкой в реестре.
//
// Отдельный близнец — комментарий, называющий имя источника: в живом провайдере такие
// комментарии стоят над КАЖДЫМ конструктором («NewGeoRegionDataSource — kacho_geo_region»), и
// текстовый разбор объявил бы покрытым непровязанное.
func TestDataSourceRegistryReadsTheRegistryNotTheDeclaration(t *testing.T) {
	const src = `package provider

type kachoProvider struct{}

// Форма 1 — имя литералом в своём Metadata. Провязана.
func NewGeoRegionDataSource() datasource.DataSource { return &geoRegionDataSource{} }

type geoRegionDataSource struct{}

func (d *geoRegionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_geo_region"
}

// Форма 2 — сборка из описания прямо в реестре. Провязана.
var vpcNetworkDSSpec = flatSpec{tfName: "kacho_vpc_network_by_name"}

// Форма 3 — общий тип, собранный фабрикой из описания: имени в конструкторе НЕТ.
// Одно описание несёт ОБА суффикса, поэтому проба ловит именно приписку лишнего.
type catalogSpec struct {
	name     string
	nameMany string
}

var geoZoneCatalog = catalogSpec{name: "_geo_zone", nameMany: "_geo_zones"}

type catalogOneDataSource struct{ spec catalogSpec }

func newCatalogOne(spec catalogSpec) func() datasource.DataSource {
	return func() datasource.DataSource { return &catalogOneDataSource{spec: spec} }
}

func (d *catalogOneDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + d.spec.name
}

type catalogManyDataSource struct{ spec catalogSpec }

func newCatalogMany(spec catalogSpec) func() datasource.DataSource {
	return func() datasource.DataSource { return &catalogManyDataSource{spec: spec} }
}

func (d *catalogManyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + d.spec.nameMany
}

// NewGeoZoneDataSource — kacho_geo_zone. Провязана.
func NewGeoZoneDataSource() datasource.DataSource { return newCatalogOne(geoZoneCatalog)() }

// NewGeoZonesDataSource — kacho_geo_zones. НЕ провязана: для пользователя не существует.
// Имя названо этим комментарием — текстовый разбор счёл бы её зарегистрированной.
func NewGeoZonesDataSource() datasource.DataSource { return newCatalogMany(geoZoneCatalog)() }

// НЕ провязана: объявлена файлом, в реестре её нет.
func NewStorageDiskTypeDataSource() datasource.DataSource { return &diskTypeDataSource{} }

type diskTypeDataSource struct{}

func (d *diskTypeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage_disk_type"
}

func (p *kachoProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewGeoRegionDataSource,
		newFlatDataSource(vpcNetworkDSSpec),
		NewGeoZoneDataSource,
	}
}
`

	names, entries, found, unresolved := dataSourceRegistryNames(t, map[string]string{"provider.go": src})
	if !found {
		t.Fatal("реестр источников не найден в литерале, где он есть — предикат не читает свой предмет")
	}
	if len(unresolved) != 0 {
		t.Fatalf("предикат не вывел имя из записи реестра: %v", unresolved)
	}
	if entries != 3 {
		t.Fatalf("записей реестра насчитано %d, а их три", entries)
	}

	got := make([]string, 0, len(names))
	for n := range names {
		got = append(got, n)
	}
	sort.Strings(got)
	want := []string{"kacho_geo_region", "kacho_geo_zone", "kacho_vpc_network_by_name"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("реестр прочитан неверно: получено %v, ожидалось %v.\n"+
			"Положительный контроль: все три ПРОВЯЗАННЫЕ формы обязаны найтись.\n"+
			"Отрицательные: непровязанные kacho_geo_zones и kacho_storage_disk_type попасть не "+
			"должны, как и имя из комментария; и kacho_geo_zones не должен приехать «прицепом» "+
			"к одиночному источнику из общего описания", got, want)
	}
}

// TestDataSourceRegistryAbsenceDiffersFromEmptiness — «реестра нет» и «реестр пуст» обязаны
// выглядеть по-разному.
//
// Без этой пробы оба состояния сошлись бы в «источников ноль», и гейт на переехавшем предикате
// объявлял бы находки по каждому сервису вместо честного отказа — то есть лгал бы уверенно и
// подробно.
func TestDataSourceRegistryAbsenceDiffersFromEmptiness(t *testing.T) {
	const empty = `package provider

type kachoProvider struct{}

func (p *kachoProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
`
	const absent = `package provider

type kachoProvider struct{}

func (p *kachoProvider) Resources(_ context.Context) []func() resource.Resource {
	return nil
}
`

	if names, entries, found, _ := dataSourceRegistryNames(t, map[string]string{"p.go": empty}); !found || entries != 0 || len(names) != 0 {
		t.Fatalf("пустой реестр прочитан неверно: found=%v entries=%d names=%v", found, entries, names)
	}
	if _, _, found, _ := dataSourceRegistryNames(t, map[string]string{"p.go": absent}); found {
		t.Fatal("предикат объявил реестр найденным там, где его нет — отказ и пустота слились")
	}
}

// TestDataSourceRegistryNamesTheFormItDoesNotKnow — четвёртая форма записи роняет гейт по имени,
// а не превращается в тихое «не покрыто».
//
// Это проверка СВОЕЙ предпосылки. Гейт обоснован фактом о дереве («реестр знает три формы»);
// факт изменится — запрет обязан сказать об этом сам, а не выдать ложные находки по каждому
// сервису.
func TestDataSourceRegistryNamesTheFormItDoesNotKnow(t *testing.T) {
	const src = `package provider

type kachoProvider struct{}

func (p *kachoProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newCatalogDataSource("geo", "kind"),
	}
}
`
	_, entries, found, unresolved := dataSourceRegistryNames(t, map[string]string{"p.go": src})
	if !found || entries != 1 {
		t.Fatalf("реестр прочитан неверно: found=%v entries=%d", found, entries)
	}
	if len(unresolved) != 1 || !strings.Contains(unresolved[0], `newCatalogDataSource("geo", "kind")`) {
		t.Fatalf("незнакомая форма записи не названа по имени: %v", unresolved)
	}
}

// TestReadOnlyServiceCensusCutsBothWays — перепись читаемых сервисов, проверенная в обе стороны
// на одном литерале.
//
// Каждый близнец отличается от своей пары ровно одним признаком, иначе проба ловила бы форму, а
// не существо: созидающий сервис несёт те же `Get`/`List`; внутренний — те же глаголы, что
// публичный; сервис с глаголом `ListNamespaces` отличается от справочника только ИМЕНЕМ глагола
// — и обязан не потеряться, а потребовать классификации.
func TestReadOnlyServiceCensusCutsBothWays(t *testing.T) {
	const src = `
service ZoneService {
  rpc Get(GetZoneRequest) returns (Zone);
  rpc List(ListZonesRequest) returns (ListZonesResponse);
}

service NetworkService {
  rpc Get(GetNetworkRequest) returns (Network);
  rpc List(ListNetworksRequest) returns (ListNetworksResponse);
  rpc Create(CreateNetworkRequest) returns (operation.Operation);
}

service InternalZoneService {
  rpc Get(GetInternalZoneRequest) returns (InternalZone);
  rpc List(ListInternalZonesRequest) returns (ListInternalZonesResponse);
}

service AuthorizeService {
  rpc ListSubjects(ListSubjectsRequest) returns (ListSubjectsResponse);
  rpc WhoAmI(WhoAmIRequest) returns (WhoAmIResponse);
}

service NamespaceCatalogService {
  rpc ListNamespaces(ListNamespacesRequest) returns (ListNamespacesResponse);
}
`

	pub, nonCreating, readable, unclassified, verbs := classifyProtoServices(src)

	if pub != 4 {
		t.Fatalf("публичных сервисов насчитано %d, а их четыре (внутренний не считается)", pub)
	}
	if nonCreating != 3 {
		t.Fatalf("несоздающих публичных сервисов насчитано %d, а их три", nonCreating)
	}

	sort.Strings(readable)
	if strings.Join(readable, ",") != "ZoneService" {
		t.Fatalf("перепись читаемых неверна: получено %v; справочник обязан попасть, созидающий "+
			"сервис с теми же Get/List — нет, внутренний — нет, решение о правах — нет", readable)
	}

	if strings.Join(dsDedup(unclassified), ",") != "ListNamespaces" {
		t.Fatalf("глагол, похожий на чтение и не классифицированный, не потребован: %v — именно "+
			"так теряется целый вид предмета", unclassified)
	}

	// Словарь глаголов собирается только с НЕсоздающих сервисов: иначе самоистечение записи
	// проверялось бы на множестве, к которому она не относится.
	for _, v := range []string{"ListSubjects", "WhoAmI", "ListNamespaces"} {
		if !dsContains(verbs, v) {
			t.Fatalf("глагол %s не попал в словарь осмотренного — самоистечение записи перестало "+
				"бы работать", v)
		}
	}
	if dsContains(verbs, "Create") {
		t.Fatal("глагол созидающего сервиса попал в словарь несоздающих — записи исключений " +
			"начали бы жить на чужом множестве")
	}
}

func dsContains(in []string, v string) bool {
	for _, s := range in {
		if s == v {
			return true
		}
	}
	return false
}
