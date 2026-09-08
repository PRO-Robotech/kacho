// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_requestbody.go — анализатор «пример запроса в клиентской
// документации исполним»: адрес резолвится в объявленный маршрут, а каждый ключ
// тела существует в сообщении запроса и не отвергается кодом.
//
// # Охват — ВСЕ СЕМЬ доменов, и это стоило отдельной работы (#1643)
//
// Механизм заводился полосой iam и полтора месяца судил ОДИН домен из семи. У
// шести соседей о нём нельзя было сказать ничего: перепись там не проводилась, и
// «находок ноль» по дереву означало «не смотрели». Расширение — параметры, а не
// новый гейт; реестр доменов и проверка его полноты — в пробе рядом.
//
// Расширение обязано МЕНЯТЬ ОСМОТРЕННОЕ, иначе оно холостое. Замер на день
// расширения: страниц 74 → 214, тел 54 → 125, ключей 168 → 417, и в шести
// прежде не наблюдавшихся доменах нашлось 9 находок — четыре пути без маршрута
// у geo и пять снятых с контракта ключей у nlb.
//
// # Гейт об этом предмете ОДИН — сведены два (#1725)
//
// Об исполнимости тел запроса судили ДВА анализатора, и распознавали они тела
// РАЗНЫМИ признаками: этот — по разбираемому JSON, второй — по форме команды
// (`-X POST|PATCH|PUT` плюс путь). Ни один не был надмножеством другого, и это
// худший из возможных исходов: тело, видимое одному и невидимое другому, выглядело
// проверенным одинаково в обоих случаях. Клиенту это стоило не отказа, а
// НЕВИДИМОСТИ.
//
// Сведение потребовало ЗАМЕРА, а не утверждения. Второй видел то, чего не видел
// этот, тремя формами записи команды, и каждая теперь распознаётся здесь:
//
//   - ПОДСТАНОВКА ОБОЛОЧКИ в теле (`"'"$PROJECT"'"`) — строгим JSON не берётся, а
//     после раскрытия оболочкой валидна: 9 тел, из них 5 не судил НИКТО;
//   - АДРЕС БЕЗ СХЕМЫ (`$BASE/vpc/v1/…`) — путь написан и сопоставим: 11 тел;
//   - ГЛАГОЛ СЛИТНО (`-XPOST`) — 1 тело, молча получавшее умолчание GET.
//
// Что дало сведение на дереве: тел сопоставлено 123 → 135, ключей рассужено
// 418 → 467, «адреса в команде нет» 2 → 0, «не разобралось как JSON» 10 → 0.
// Покрытие снятого гейта доказано пообъектно: из 79 пар (страница, ключ верхнего
// уровня), которые он судил, не покрыто НИ ОДНОЙ.
//
// Нашлись при этом ТРИ живых дефекта, невидимых обоим: ключ `volumeId` вместо
// `sourceVolumeId` в быстром старте (край отбрасывает молча, а отказ приходит о
// поле, которое клиент считает присланным), лишняя запятая в теле (тело не
// доезжает до сервиса вовсе) и — ЗА НЕЙ — путь, которого контракт не объявляет.
// Последний показателен: внешний дефект прятал внутренний, и пока тело не
// разбиралось, до проверки маршрута дело не доходило.
//
// АДЪЮДИКАЦИЯ, без которой сведение вернуло бы тот же класс: «не разобралось как
// JSON» было ОДНИМ числом на ДВА разных предмета — слепую зону распознавателя
// (9 тел) и дефект примера (1 тело). Пока они считались вместе, ни один не был
// виден. Теперь первое судится, второе — находка.
//
// # Предмет
//
// Пример `curl` на странице документации — это не иллюстрация, а ИНСТРУКЦИЯ:
// клиент копирует его целиком. Ключ, которого в сообщении запроса нет, не
// вызывает ошибку разбора — край выбрасывает неизвестное поле МОЛЧА
// (`DiscardUnknown`), и запрос доходит до сервиса без него. Дальше исходов два,
// и оба хуже отказа на самом ключе:
//
//   - поле было ОБЯЗАТЕЛЬНЫМ под другим именем → сервис отвечает «<поле> is
//     required» о поле, которое клиент, по его мнению, прислал;
//   - поле СНЯТО с входа и стало выходным → сервис отвергает его отдельной
//     веткой, и отказ читается как «значение не то», хотя верно «поля здесь нет».
//
// Замер на день заведения (kacho#1603 / #1614 / #1615): первая команда быстрого
// старта iam не проходила НИ ПРИ КАКОМ теле, потому что несла `ownerUserId` —
// поле, выведенное из вызывающего и отвергаемое с любым значением, включая
// собственный верный идентификатор. Тело выдачи прав несло `scopeRef` — ключ,
// снятый с контракта тумбстоуном; край отбрасывал его молча, и клиент получал
// три отказа подряд, из которых третий неугадываем.
//
// # Что судит анализатор
//
// Сообщение запроса ВЫВОДИТСЯ из дескрипторов — не из текста `.proto` и не из
// перечня в этом файле. Служба, метод, путь и вид тела читаются из
// зарегистрированных дескрипторов пакета контрактов (`google.api.http`), поэтому
// переименование поля или переезд пути доезжают сюда сами.
//
// В документации распознаются ДВЕ формы команды. `curl`: метод (`-X`, по
// умолчанию GET), адрес и тело (`-d '{…}'`). `grpcurl`: тело и ПОЛНОЕ имя
// службы с методом (`kaname.cloud.iam.v1.AccountService/Create`) — его сопоставлять
// с путём не надо вовсе, и вместе с ним под наблюдение попадают методы
// `Internal*`, у которых HTTP-привязки нет by construction.
//
// Форм ровно столько, сколько нашлось замером; третья, найденная и НЕ покрытая,
// названа в границах ниже. Адрес сопоставляется с шаблоном пути метода — форм
// подстановки ТРИ, и распознаватель знает все три: `{id}` и `{id=*}` берут один
// сегмент, `{name=**}` — все оставшиеся (имя репозитория реестра содержит слэш).
// Тело разбирается как JSON, и каждый
// его ключ обязан быть полем сообщения — в любом из ДВУХ написаний, которые
// принимает край: camelCase (`ownerUserId`) и proto (`owner_user_id`). Судится не
// стиль, а исполнимость.
//
// Разбор РЕКУРСИВНЫЙ: вложенный объект судится по полю-сообщению, в котором
// лежит. Иначе `"target": {"allInScope": {}}` проверялся бы только по имени
// `target`, а ветвь внутри — самое неугадываемое место — осталась бы вне
// наблюдения.
//
// # ВТОРОЙ предикат: поле есть в сообщении, но код его ОТВЕРГАЕТ
//
// Первого предиката НЕДОСТАТОЧНО, и это измерено, а не предположено. Прогон на
// возвращённом настоящем дефекте #1603 остался ЗЕЛЁНЫМ: `owner_user_id` из
// `CreateAccountRequest` никуда не снят — поле объявлено, дескриптор его знает,
// а отвергается оно ВЕТКОЙ ПРОВЕРКИ ВХОДА, которой в дескрипторе нет. Гипотеза
// «раз клиент получает отказ, значит поля в сообщении нет» была ложной, и без
// перепроверки гейт объявил бы закрытым класс, которого не видит.
//
// Поэтому набор отвергаемых имён ВЫВОДИТСЯ разбором прод-кода use-case'ов:
// вызов `shared.InvalidArg("<поле>", "<текст>")`, чей текст помечает поле
// невходным (`derived from caller` / `output-only`). Такое имя не вправе стоять
// ключом ни в одном теле, сопоставленном с методом, чьё сообщение это поле
// несёт. Отказ у него ТЕРМИНАЛЬНЫЙ: значение подобрать нельзя, потому что
// отвергается сам факт присутствия ключа.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо, а не подразумевается
//
//  1. ЗНАЧЕНИЯ не судятся — только имена ключей. Неверный идентификатор зоны или
//     несуществующая роль в примере этим гейтом не ловятся: у них нет предиката
//     в дереве.
//
//  2. ОБЯЗАТЕЛЬНОСТЬ не судится ни одним из двух предикатов. Пример, не
//     назвавший обязательное поле, здесь молчит: `proto3` не отличает «не
//     задано» от «задано нулём», поэтому требование живёт в коде проверки входа,
//     а не в дескрипторе. Это ровно та половина #1615, которую гейт НЕ
//     закрывает, — `target` обязателен по коду, и его отсутствие в примере
//     остаётся за обзором. Симметрию с ВТОРЫМ предикатом («отвергается»
//     выводится из кода, «требуется» — нет) стоит назвать вслух: обязательность
//     выражается десятком разных форм, и распознаватель по одной из них дал бы
//     слепую зону, поданную как покрытие.
//
//     2а. ЗАРЕЗЕРВИРОВАННОЕ ИМЯ — находка, и это НЕ тот класс, где предикат по имени
//     врёт. Соседний замер по дереву nlb показал: имя, объявленное `reserved` в
//     контракте, отвечает «существую» на вопрос «встречается ли в контракте» — 13
//     вхождений, из них дефект один, — а на вопрос «есть ли среди живых полей»
//     отвечает «нет» и даёт 12 ложных находок, потому что то же имя живёт именем
//     СТОЛБЦА применённой миграции. Здесь этой развилки нет by construction:
//     судится не имя, встреченное где-то в дереве, а КЛЮЧ ТЕЛА ЗАПРОСА,
//     сопоставленного с методом, и судится он по ДЕСКРИПТОРУ. Зарезервированного
//     поля в дескрипторе не существует — значит край такой ключ и вправду
//     отбрасывает молча. Имя столбца сюда не попадает: оно не ключ тела.
//     Не «чините» это добавлением reserved-имён в набор известных — вернёте ровно
//     тот дефект, ради которого гейт заведён (снятый с контракта `scopeRef`).
//
//  3. КАРТЫ и известные типы обхода не углубляют: у `map<string,string>`
//     (`labels`) ключи произвольны by construction, а у `google.protobuf.Struct`
//     и `Any` — тем более. Рекурсия в них означала бы находки на законном.
//
//  4. ТЕЛА ВНЕ КОМАНДЫ вне охвата, и здесь две разные причины. Блок JSON,
//     показывающий ОТВЕТ, судиться НЕ ДОЛЖЕН: в ответе законны выходные поля,
//     которых на входе нет (`ownerUserId` в ответе Create — верен). А вот тело,
//     нарисованное узлом ДИАГРАММЫ последовательности
//     (`Cli->>GW: POST /iam/v1/accounts<br/>{…}`), судиться должно бы — и не
//     судится: метка узла есть свободный текст, а не команда. Это объявленная
//     слепая зона, а не покрытие, и с расширением охвата она СЧИТАЕТСЯ отдельным
//     числом переписи: молчать о ней значило бы выдавать «находок ноль» шире,
//     чем оно есть. Замер на день расширения — 7 узлов, все у iam.
//
//  5. ПУТЬ БЕЗ МАРШРУТА — ТЕПЕРЬ НАХОДКА, и здесь стояло обратное (#1647).
//     Прежняя редакция считала его переписью с доводом «примеры ходят и к
//     соседним доменам, объявлять их дефектом значило бы краснеть на законном».
//     Довод был верен ровно потому, что вселенной маршрутов был ОДИН домен: гейт
//     не пропускал чужое сознательно, он его не видел. Со вселенной из семи у
//     законного примера такого исхода не осталось, и десять путей, копившихся
//     под этим числом, оказались тем, чем и выглядели.
//
//     Клиенту это дороже неверного ключа: неверный ключ край отбрасывает молча,
//     а неверный путь даёт `404` без тела — отказ, не называющий верного
//     написания и не восстанавливающий следующий шаг.
//
//     Два соседних исхода находкой НЕ являются и считаются отдельно: адреса в
//     команде нет вовсе (он из переменной) — судить нечего; служба `grpcurl` вне
//     регистра дескрипторов — `grpcurl` ходит и к чужим службам, а предиката,
//     отличающего их от опечатки, в дереве нет.
//
//  6. ОТКАЗ С ПОМЕТКОЙ ВНЕ КОНВЕНЦИОННОЙ ФОРМЫ вторым предикатом не читается и
//     считается отдельным числом. Пометка ищется в скобках (`Illegal argument
//     <поле> (<пометка>)`), потому что скобка привязывает её к полю из ПЕРВОГО
//     АРГУМЕНТА. Без привязки распознаватель ошибается в сторону ЛОЖНОЙ НАХОДКИ:
//     отказ, названный по РОДИТЕЛЮ, а говорящий о его подполях, объявил бы
//     запрещённым ключом сам родитель — то есть ровно то поле, без которого
//     запрос не собирается. Свойство это общее и от наличия конкретного такого
//     отказа в дереве НЕ зависит; сколько их сегодня — печатает перепись
//     (`отказов с пометкой вне конвенционной формы`), и ноль здесь законен.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль доменов, ноль методов из дескрипторов у любого домена, ноль прочитанных
// страниц у любого домена, ноль разобранных тел либо ноль рассуженных ключей —
// «находок ноль» неотличимо от «прочитано ноль». Предпосылка ВТОРОГО предиката
// проверяется СУММОЙ по дереву, а не по каждому домену: невходных полей у шести
// доменов из семи нет вовсе, и требовать их от каждого значило бы требовать
// наличия дефекта.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"

	// Регистрация дескрипторов ВСЕХ доменов: источник путей, методов и сообщений.
	// Пакет контракта и имя каталога сервиса — РАЗНЫЕ словари: балансировщик
	// живёт в `services/nlb`, а его контракт — в `kacho.cloud.loadbalancer.v1`.
	// Совпадение имён у остальных шести — совпадение, а не свойство дерева.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

// ClientTruthRequestBodyDomain — один домен под наблюдением.
type ClientTruthRequestBodyDomain struct {
	// Name — имя домена для переписи. Совпадает с каталогом сервиса, а НЕ с
	// пакетом контракта: у балансировщика это `nlb` против
	// `kacho.cloud.loadbalancer.v1`.
	Name string
	// ProtoPackage — пакет контрактов, чьи дескрипторы задают истину.
	ProtoPackage string
	// DocsDirs — каталоги клиентской документации (от корня дерева).
	DocsDirs []string
	// UseCaseDirs — каталоги прод-кода use-case'ов (от корня дерева), откуда
	// выводится набор полей, отвергаемых на входе ЭТИМ доменом.
	UseCaseDirs []string
}

// ClientTruthRequestBodyOptions — вход анализатора.
type ClientTruthRequestBodyOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree
	// Domains — домены под наблюдением. Маршруты ВСЕХ перечисленных образуют
	// вселенную путей: пример на странице iam, зовущий `/vpc/v1/networks`, —
	// такая же инструкция, и судится он по сообщению vpc, а не пропускается как
	// «чужой домен». Отсюда же берётся предикат #1647: путь, не резолвящийся ни
	// в один маршрут вселенной, документирует вызов, которого нет.
	Domains []ClientTruthRequestBodyDomain
	// DocExts — расширения страниц.
	DocExts []string
}

// ClientTruthRequestBodyCensus — объём осмотренного, ПО ДОМЕНАМ и суммой.
//
// По домену — потому что «ноль находок» у шести соседей неотличимо от «шесть
// соседей не осматривались»: ровно так класс и прожил у них незамеченным, пока
// охват был один домен из семи (#1643).
type ClientTruthRequestBodyCensus struct {
	// Domains — перепись по каждому домену в порядке объявления.
	Domains []ClientTruthRequestBodyDomainCensus
	// Methods — методов с телом выведено из дескрипторов ВСЕХ доменов.
	Methods int
	// DocFiles — страниц прочитано.
	DocFiles int
	// CurlBlocks — команд curl распознано.
	CurlBlocks int
	// BodiesParsed — тел разобрано как JSON.
	BodiesParsed int
	// BodiesMatched — тел, чей адрес сопоставился с методом вселенной.
	BodiesMatched int
	// BodiesUnrouted — тел, чей адрес РАСПОЗНАН, но не резолвится ни в один
	// объявленный маршрут. Это находка (#1647), а не слепая зона: клиент
	// получает `404` без тела — отказ, не называющий верного написания.
	BodiesUnrouted int
	// BodiesNoAddress — тел, у которых адреса в команде не нашлось вовсе (адрес
	// из переменной, плейсхолдер). Не находка: судить нечего.
	BodiesNoAddress int
	// GrpcUnknownService — тел `grpcurl`, чья служба вне регистра дескрипторов.
	// Не находка: `grpcurl` ходит и к чужим службам (провайдер личности), и
	// предиката, отличающего их от опечатки, в дереве нет.
	GrpcUnknownService int
	// KeysJudged — ключей рассужено.
	KeysJudged int
	// BodiesNotJSON — тел, не разобравшихся как JSON ДАЖЕ ПОСЛЕ снятия подстановок
	// оболочки. Теперь это НАХОДКА, а не перепись (#1725): такой пример не
	// исполним ни при каком окружении. Число печатается по-прежнему — оно
	// показывает, что предикат вообще исполнялся.
	BodiesNotJSON int
	// BodiesShellInterpolated — тел, разобравшихся только после снятия подстановок
	// оболочки (`"'"$PROJECT"'"`). Прежде они уходили в BodiesNotJSON и не
	// судились вовсе: девять инструкций, которые клиент копирует чаще прочих,
	// стояли вне наблюдения. Число печатается отдельно, потому что расширение
	// распознавателя обязано МЕНЯТЬ осмотренное, иначе оно холостое.
	BodiesShellInterpolated int
	// DiagramBodies — тел запроса, нарисованных УЗЛОМ ДИАГРАММЫ. Объявленная
	// слепая зона: метка узла есть свободный текст, а не команда, и
	// распознавателя под неё не заводилось. Число печатается, чтобы «находок
	// ноль» не читалось шире, чем есть.
	DiagramBodies int
	// RejectedFields — сколько невходных полей выведено из прод-кода всех доменов.
	RejectedFields int
	// RejectedFreeForm — отказов, несущих пометку невходного поля ВНЕ
	// конвенционной формы `(<пометка>)`. Объявленная граница распознавателя:
	// привязать такой текст к полю нечем. Печатается, чтобы её не приняли за
	// покрытие.
	RejectedFreeForm int
}

// ClientTruthRequestBodyDomainCensus — перепись одного домена.
type ClientTruthRequestBodyDomainCensus struct {
	Name               string
	ProtoPackage       string
	Methods            int
	DocFiles           int
	CurlBlocks         int
	BodiesParsed       int
	BodiesMatched      int
	BodiesUnrouted     int
	BodiesNoAddress    int
	GrpcUnknownService int
	KeysJudged         int
	// BodiesNotJSON — тел, не разобравшихся как JSON и после подстановок (находка).
	BodiesNotJSON int
	// BodiesShellInterpolated — тел, разобранных после снятия подстановок оболочки.
	BodiesShellInterpolated int
	// DiagramBodies — тел запроса, нарисованных узлом диаграммы (не судятся).
	DiagramBodies int
	// RejectedFreeForm — отказов с пометкой вне конвенционной формы.
	RejectedFreeForm int
	// RejectedFields — невходных полей выведено из прод-кода ЭТОГО домена. Ноль
	// законен: у шести доменов из семи веток «поле выведено из вызывающего» нет
	// вовсе (замер на день расширения). Предпосылка распознавателя проверяется
	// СУММОЙ по дереву, а не по каждому домену: узкая популяция предпосылку не
	// подтверждает, она её скрывает.
	RejectedFields int
	// Findings — находок на страницах этого домена.
	Findings int
}

// ClientTruthRequestBodyFinding — одна находка. Видов ЧЕТЫРЕ, и они принадлежат
// РАЗНЫМ предикатам: ключа нет в сообщении · ключ есть, но код его отвергает ·
// путь не резолвится ни в один маршрут · тело не разбирается как JSON. Смешивать
// их в одну строку нельзя — у каждого свой исход для клиента и свой способ починки.
type ClientTruthRequestBodyFinding struct {
	// Domain — домен СТРАНИЦЫ, на которой стоит пример.
	Domain  string
	File    string
	Line    int
	Method  string
	Path    string
	Message string
	KeyPath string
	// Rejected — поле в сообщении ЕСТЬ, но код отвергает его присутствие.
	Rejected bool
	// Unrouted — адрес примера не резолвится ни в один объявленный маршрут.
	// Ключи такого тела не рассуживаются: сообщения, по которому судить, нет.
	Unrouted bool
	// Malformed — тело не разобралось как JSON ДАЖЕ ПОСЛЕ снятия подстановок
	// оболочки. Такой пример не исполним ни при каком окружении: край отвечает на
	// него разбором тела, а не отказом по существу.
	Malformed bool
}

func (f ClientTruthRequestBodyFinding) String() string {
	switch {
	case f.Malformed:
		return fmt.Sprintf("%s:%d: %s %s — тело не разбирается как JSON и после подстановок "+
			"оболочки (%s); скопированное дословно, оно не доедет до сервиса вовсе",
			f.File, f.Line, f.Method, f.Path, f.Message)
	case f.Unrouted:
		return fmt.Sprintf("%s:%d: %s %s — путь не резолвится ни в один объявленный маршрут",
			f.File, f.Line, f.Method, f.Path)
	case f.Rejected:
		return fmt.Sprintf("%s:%d: %s %s — ключ %q есть в %s, но код отвергает его присутствие",
			f.File, f.Line, f.Method, f.Path, f.KeyPath, f.Message)
	}
	return fmt.Sprintf("%s:%d: %s %s — ключа %q нет в %s",
		f.File, f.Line, f.Method, f.Path, f.KeyPath, f.Message)
}

// httpMethodBinding — один метод контракта: глагол, шаблон пути, сообщение входа.
type httpMethodBinding struct {
	verb  string
	tmpl  []string
	input protoreflect.MessageDescriptor
}

// path — шаблон одной строкой. Нужен для УСТОЙЧИВОГО порядка: дескрипторы
// приходят обходом регистра, порядок которого не определён, а вердикт, зависящий
// от порядка прогона, читать нельзя.
func (b httpMethodBinding) path() string { return b.verb + " /" + strings.Join(b.tmpl, "/") }

var (
	curlLineRe = regexp.MustCompile(`\b(?:grpcurl|curl)\b`)
	// Глагол пишут ДВУМЯ законными способами, и пробел между ключом и значением
	// необязателен: `curl -X POST` и `curl -XPOST` — одна и та же команда. Первая
	// редакция знала только форму с пробелом, поэтому слитная молча получала
	// умолчание GET и не сопоставлялась ни с одним маршрутом мутации.
	verbRe = regexp.MustCompile(`-X\s*([A-Z]+)`)
	// Адрес пишут ТРЕМЯ законными способами: в одинарных кавычках, в двойных и
	// без кавычек вовсе. Первая редакция знала только кавычки — и не видела
	// ни одного примера инженерной части, где адрес голый: четырнадцать тел
	// уходили в «не сопоставилось», и среди них жил настоящий дефект
	// (`owner_user_id` в теле Create). Распознаватель, не знающий одной из
	// законных форм, не даёт ни красного, ни зелёного — он молчит.
	urlRe  = regexp.MustCompile(`['"](https?://[^'"\s]+)['"]|(https?://[^'"\s\\]+)`)
	bodyRe = regexp.MustCompile(`(?s)-d\s+'(\{.*?\})'`)
	// Адрес пишут и БЕЗ СХЕМЫ — от переменной оболочки (`$BASE/vpc/v1/…`) либо
	// просто путём. Схема тогда живёт в переменной, а путь в команде есть и
	// сопоставим с шаблоном — то есть судить как раз есть что. Форма законная и в
	// дереве живая: одиннадцать тел из ста тридцати пяти написаны так, и до этой
	// правки все одиннадцать уходили в «адреса в команде нет вовсе».
	//
	// Ищется ПОСЛЕ снятия тела: путь встречается и значением ключа, и тогда
	// распознаватель взял бы адресом строку из тела запроса.
	pathRe = regexp.MustCompile(`(/[a-z][A-Za-z0-9]*/v1/[^\s'"\\]*)`)
	// shellInterpRe — подстановка оболочки внутри одинарно-закавыченного тела:
	// `"'"$PROJECT"'"`. Это НЕ плейсхолдер и НЕ дефект — это единственный способ
	// подставить переменную в `-d '…'`, и после раскрытия оболочкой тело
	// становится валидным JSON. Форма в дереве ОДНА (замер: 20 вхождений, все
	// `'"$VAR"'`); фигурные скобки приняты как вторая законная запись той же
	// формы. Значения гейт не судит (объявленная граница), поэтому подстановка
	// заменяется на плейсхолдер, а не раскрывается.
	shellInterpRe = regexp.MustCompile(`'"\$\{?[A-Za-z_][A-Za-z0-9_]*\}?"'`)
	// grpcurl называет метод ПОЛНЫМ именем прямо в команде, поэтому сопоставлять
	// его с шаблоном пути не нужно вовсе — и заодно становятся судимы методы
	// Internal*, у которых HTTP-привязки нет by construction и которые первым
	// распознавателем не наблюдались никак.
	grpcurlRe    = regexp.MustCompile(`\bgrpcurl\b`)
	grpcMethodRe = regexp.MustCompile(`([a-z][\w.]*\.[A-Z]\w*)/(\w+)`)

	// diagramRequestNode — узел диаграммы последовательности, несущий ТЕЛО
	// запроса: стрелка запроса (`->>`, а не ответа `-->>`), мутирующий глагол и
	// фигурная группа с содержимым. Судить такое тело гейт НЕ умеет — метка узла
	// есть свободный текст, а не команда, — но СЧИТАТЬ обязан: слепая зона,
	// о которой молчат, неотличима от покрытия.
	diagramArrowRe = regexp.MustCompile(`(^|[^-])->>`)
	diagramVerbRe  = regexp.MustCompile(`\b(POST|PUT|PATCH)\b`)
	diagramBodyRe  = regexp.MustCompile(`\{[^}]*[:,][^}]*\}`)
)

// isDiagramRequestNode — строка рисует запрос с телом узлом диаграммы.
func isDiagramRequestNode(line string) bool {
	return diagramArrowRe.MatchString(line) &&
		diagramVerbRe.MatchString(line) &&
		diagramBodyRe.MatchString(line)
}

// AuditClientTruthRequestBody требует, чтобы каждый ключ тела запроса в
// клиентской документации существовал в сообщении запроса этого метода, а адрес
// примера резолвился в объявленный маршрут.
func AuditClientTruthRequestBody(
	opts ClientTruthRequestBodyOptions, log io.Writer,
) ([]ClientTruthRequestBodyFinding, ClientTruthRequestBodyCensus, error) {
	var census ClientTruthRequestBodyCensus

	if len(opts.Domains) == 0 {
		return nil, census, fmt.Errorf(
			"домен не назван ни один — судить нечего, а «находок ноль» получено даром")
	}

	// ВСЕЛЕННАЯ МАРШРУТОВ — объединение по всем доменам. Пример на странице
	// одного домена, зовущий соседа, — такая же инструкция клиенту; судить его
	// «чужим» значило бы вывести из наблюдения ровно те места, где ошибиться
	// легче всего.
	var universe []httpMethodBinding
	rejectedByPkg := map[string]map[string]bool{}
	for i := range opts.Domains {
		d := opts.Domains[i]
		bindings, err := collectHTTPBindings(d.ProtoPackage)
		if err != nil {
			return nil, census, err
		}
		if len(bindings) == 0 {
			return nil, census, fmt.Errorf(
				"домен %s: из дескрипторов пакета %s не выведено ни одного метода с телом — "+
					"судить примеры не по чему", d.Name, d.ProtoPackage)
		}
		universe = append(universe, bindings...)

		rejected, freeForm, rerr := collectRejectedInputFields(opts.Tree, d.UseCaseDirs)
		if rerr != nil {
			return nil, census, rerr
		}
		rejectedByPkg[d.ProtoPackage] = rejected

		census.Domains = append(census.Domains, ClientTruthRequestBodyDomainCensus{
			Name: d.Name, ProtoPackage: d.ProtoPackage,
			Methods: len(bindings), RejectedFields: len(rejected),
			RejectedFreeForm: freeForm,
		})
		census.Methods += len(bindings)
		census.RejectedFields += len(rejected)
		census.RejectedFreeForm += freeForm
	}

	// Предпосылка ВТОРОГО предиката проверяется СУММОЙ по дереву, а не по
	// каждому домену. Ноль у отдельного домена — факт о домене (веток «поле
	// выведено из вызывающего» у него нет), ноль по всему дереву — сломанный
	// распознаватель, и тогда «находок ноль» получено даром.
	if census.RejectedFields == 0 {
		return nil, census, fmt.Errorf(
			"по всем доменам не выведено ни одного невходного поля — второй предикат " +
				"беспредметен, а «находок ноль» получено даром")
	}

	var findings []ClientTruthRequestBodyFinding
	for i := range opts.Domains {
		d := opts.Domains[i]
		dc := &census.Domains[i]
		for _, dir := range d.DocsDirs {
			for _, rel := range clientTruthTreeFiles(opts.Tree, dir, true, opts.DocExts...) {
				raw, rerr := clientTruthReadTreeFile(opts.Tree, rel)
				if rerr != nil {
					return nil, census, fmt.Errorf("чтение %s: %w", rel, rerr)
				}
				dc.DocFiles++
				got := auditOneDoc(d.Name, rel, string(raw), universe, rejectedByPkg, dc)
				dc.Findings += len(got)
				findings = append(findings, got...)
			}
		}
		census.DocFiles += dc.DocFiles
		census.CurlBlocks += dc.CurlBlocks
		census.BodiesParsed += dc.BodiesParsed
		census.BodiesMatched += dc.BodiesMatched
		census.BodiesUnrouted += dc.BodiesUnrouted
		census.BodiesNoAddress += dc.BodiesNoAddress
		census.GrpcUnknownService += dc.GrpcUnknownService
		census.KeysJudged += dc.KeysJudged
		census.DiagramBodies += dc.DiagramBodies
		census.BodiesNotJSON += dc.BodiesNotJSON
		census.BodiesShellInterpolated += dc.BodiesShellInterpolated
	}

	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: доменов %d · методов с телом %d · страниц %d · "+
			"команд curl %d · тел разобрано %d · сопоставлено %d · путь без маршрута %d (НАХОДКА) · "+
			"адреса в команде нет %d · служба gRPC вне регистра %d · ключей рассужено %d · "+
			"невходных полей выведено %d · отказов с пометкой вне конвенционной формы %d "+
			"(НЕ читаются — объявленная граница) · тел, нарисованных узлом диаграммы, %d "+
			"(НЕ судятся — объявленная слепая зона) · тел разобрано после подстановок оболочки %d · "+
			"тел не разобралось как JSON и после подстановок %d (НАХОДКА)\n",
			len(census.Domains), census.Methods, census.DocFiles, census.CurlBlocks,
			census.BodiesParsed, census.BodiesMatched, census.BodiesUnrouted,
			census.BodiesNoAddress, census.GrpcUnknownService, census.KeysJudged,
			census.RejectedFields, census.RejectedFreeForm, census.DiagramBodies,
			census.BodiesShellInterpolated, census.BodiesNotJSON)
		for _, d := range census.Domains {
			_, _ = fmt.Fprintf(log, "  %-9s (%s): методов %d · страниц %d · команд %d · тел %d · "+
				"сопоставлено %d · без маршрута %d · без адреса %d · gRPC вне регистра %d · "+
				"ключей %d · тел узлом диаграммы %d · после подстановок %d · не JSON %d · "+
				"невходных полей %d (вне формы %d) · находок %d\n",
				d.Name, d.ProtoPackage, d.Methods, d.DocFiles, d.CurlBlocks, d.BodiesParsed,
				d.BodiesMatched, d.BodiesUnrouted, d.BodiesNoAddress, d.GrpcUnknownService,
				d.KeysJudged, d.DiagramBodies, d.BodiesShellInterpolated, d.BodiesNotJSON,
				d.RejectedFields, d.RejectedFreeForm, d.Findings)
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].KeyPath < findings[j].KeyPath
	})
	return findings, census, nil
}

// auditOneDoc разбирает одну страницу.
func auditOneDoc(
	domain, rel, text string, universe []httpMethodBinding,
	rejectedByPkg map[string]map[string]bool,
	census *ClientTruthRequestBodyDomainCensus,
) []ClientTruthRequestBodyFinding {
	var out []ClientTruthRequestBodyFinding
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if isDiagramRequestNode(lines[i]) {
			census.DiagramBodies++
			// Не `continue`: строка может нести и команду, и стрелку — считать
			// её слепой зоной и одновременно судить не запрещено.
		}
		if !curlLineRe.MatchString(lines[i]) {
			continue
		}
		// Команда продолжается, пока строка кончается признаком продолжения. В
		// шаблонной строке страницы он удвоен (`\\`), в обычном коде — одинарен.
		block := []string{lines[i]}
		j := i
		for j < len(lines)-1 && strings.HasSuffix(strings.TrimRight(lines[j], " \t"), `\`) {
			j++
			block = append(block, lines[j])
		}
		// Тело может продолжаться и после последней строки продолжения — оно
		// многострочное и закрывается кавычкой. Дочитываем до неё.
		joined := strings.Join(block, "\n")
		if strings.Contains(joined, "-d '") && !bodyRe.MatchString(joined) {
			for j < len(lines)-1 && !strings.Contains(lines[j], "}'") {
				j++
				block = append(block, lines[j])
			}
			joined = strings.Join(block, "\n")
		}
		census.CurlBlocks++
		i = j
		isGRPC := grpcurlRe.MatchString(joined)

		m := bodyRe.FindStringSubmatch(joined)
		if m == nil {
			continue
		}
		var body map[string]any
		raw := m[1]
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			// Строгий разбор не удался — снимаем подстановки оболочки и пробуем
			// снова. Порядок именно такой: 115 тел дерева разбираются строго, и
			// трогать их нормализацией незачем.
			normalized := shellInterpRe.ReplaceAllString(raw, "SHELL_VALUE")
			if normalized == raw || json.Unmarshal([]byte(normalized), &body) != nil {
				// Тело не исполнимо ни при каком окружении. Прежняя редакция
				// считала это переписью («плейсхолдер вместо значения»), и число
				// покрывало ДВА разных предмета: девять тел с законной подстановкой
				// оболочки (слепая зона распознавателя) и одно с лишней запятой
				// (дефект примера). Адъюдикация развела их: первое теперь судится,
				// второе — находка.
				census.BodiesNotJSON++
				verb, path := "?", "?"
				if v := verbRe.FindStringSubmatch(joined); v != nil {
					verb = v[1]
				}
				if u := urlRe.FindStringSubmatch(joined); u != nil {
					path = urlPath(firstNonEmpty(u[1:]))
				}
				out = append(out, ClientTruthRequestBodyFinding{
					Domain: domain, File: rel, Line: i + 1, Method: verb, Path: path,
					Malformed: true, Message: err.Error(),
				})
				continue
			}
			census.BodiesShellInterpolated++
		}
		census.BodiesParsed++

		if isGRPC {
			gm := grpcMethodRe.FindStringSubmatch(joined)
			if gm == nil {
				census.BodiesNoAddress++
				continue
			}
			input, ok := grpcInput(gm[1], gm[2])
			if !ok {
				// Служба вне регистра дескрипторов. Находкой НЕ объявляется, и
				// это решение, а не недосмотр: `grpcurl` ходит и к чужим службам
				// (провайдер личности, средства наблюдения), а предиката,
				// отличающего их от нашей опечатки, в дереве нет.
				census.GrpcUnknownService++
				continue
			}
			census.BodiesMatched++
			out = append(out, judgeObject(domain, rel, i+1, "gRPC", gm[1]+"/"+gm[2],
				input, "", body, rejectedByPkg, census)...)
			continue
		}

		verb := "GET"
		if v := verbRe.FindStringSubmatch(joined); v != nil {
			verb = v[1]
		}
		path, ok := commandPath(joined, m[0])
		if !ok {
			// Пути в команде нет вовсе — ни со схемой, ни без неё. Судить нечего:
			// это не путь, а его отсутствие.
			census.BodiesNoAddress++
			continue
		}
		bind, matched := matchBinding(universe, verb, path)
		if !matched {
			// АДРЕС РАСПОЗНАН И НЕ РЕЗОЛВИТСЯ. Это находка (#1647), а не слепая
			// зона: край отвечает `404` без тела, то есть отказом, который не
			// называет верного написания и не восстанавливает следующий шаг.
			// Прежняя редакция считала это переписью, потому что вселенной был
			// ОДИН домен и путь соседа законно не резолвился; со вселенной из
			// семи такого исхода у законного примера не осталось.
			census.BodiesUnrouted++
			out = append(out, ClientTruthRequestBodyFinding{
				Domain: domain, File: rel, Line: i + 1, Method: verb, Path: path,
				Unrouted: true,
			})
			continue
		}
		census.BodiesMatched++
		out = append(out, judgeObject(domain, rel, i+1, verb, path, bind.input, "", body,
			rejectedByPkg, census)...)
	}
	return out
}

// commandPath — путь запроса, каким бы из ДВУХ законных способов адрес ни был
// записан: полным адресом со схемой либо путём (в том числе от переменной
// оболочки).
//
// Тело из команды снимается ДО поиска: путь встречается и значением ключа, и без
// этого распознаватель взял бы адресом строку из тела запроса — то есть судил бы
// пример против чужого сообщения и молчал бы там, где надо говорить.
func commandPath(joined, body string) (string, bool) {
	if u := urlRe.FindStringSubmatch(joined); u != nil {
		return urlPath(firstNonEmpty(u[1:])), true
	}
	outside := strings.Replace(joined, body, " ", 1)
	if p := pathRe.FindStringSubmatch(outside); p != nil {
		return urlPath(p[1]), true
	}
	return "", false
}

// judgeObject рекурсивно сверяет ключи объекта с полями сообщения.
func judgeObject(
	domain, rel string, line int, verb, path string, msg protoreflect.MessageDescriptor,
	prefix string, obj map[string]any, rejectedByPkg map[string]map[string]bool,
	census *ClientTruthRequestBodyDomainCensus,
) []ClientTruthRequestBodyFinding {
	var out []ClientTruthRequestBodyFinding
	// Набор невходных полей берётся у ВЛАДЕЛЬЦА сообщения, а не у домена
	// страницы: «поле выведено из вызывающего» — свойство кода того сервиса,
	// который сообщение принимает. Общий набор на все домены запретил бы
	// присылать поле там, где его никто не отвергает.
	rejected := rejectedByPkg[string(msg.ParentFile().Package())]
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		census.KeysJudged++
		fd := lookupField(msg, k)
		if fd == nil {
			out = append(out, ClientTruthRequestBodyFinding{
				Domain: domain, File: rel, Line: line, Method: verb, Path: path,
				Message: string(msg.FullName()), KeyPath: prefix + k,
			})
			continue
		}
		// ВТОРОЙ предикат: поле в сообщении есть, но код отвергает его присутствие.
		if rejected[fd.JSONName()] || rejected[string(fd.Name())] {
			out = append(out, ClientTruthRequestBodyFinding{
				Domain: domain, File: rel, Line: line, Method: verb, Path: path,
				Message: string(msg.FullName()), KeyPath: prefix + k, Rejected: true,
			})
			continue
		}
		// Углубляемся только туда, где имена полей закрыты контрактом: карты и
		// известные типы имеют произвольные ключи by construction.
		if fd.Kind() != protoreflect.MessageKind || fd.IsMap() || isOpaqueMessage(fd.Message()) {
			continue
		}
		nested := fd.Message()
		switch v := obj[k].(type) {
		case map[string]any:
			out = append(out, judgeObject(domain, rel, line, verb, path, nested,
				prefix+k+".", v, rejectedByPkg, census)...)
		case []any:
			for _, el := range v {
				if em, ok := el.(map[string]any); ok {
					out = append(out, judgeObject(domain, rel, line, verb, path, nested,
						prefix+k+"[].", em, rejectedByPkg, census)...)
				}
			}
		}
	}
	return out
}

// lookupField принимает ОБА написания, которые принимает край: camelCase и proto.
func lookupField(msg protoreflect.MessageDescriptor, key string) protoreflect.FieldDescriptor {
	if fd := msg.Fields().ByJSONName(key); fd != nil {
		return fd
	}
	return msg.Fields().ByTextName(key)
}

// isOpaqueMessage — сообщение, у которого имена «полей» задаёт не контракт.
func isOpaqueMessage(md protoreflect.MessageDescriptor) bool {
	if md == nil {
		return true
	}
	switch md.FullName() {
	case "google.protobuf.Struct", "google.protobuf.Value", "google.protobuf.Any",
		"google.protobuf.ListValue":
		return true
	}
	return false
}

// collectHTTPBindings выводит методы С ТЕЛОМ из зарегистрированных дескрипторов.
func collectHTTPBindings(pkg string) ([]httpMethodBinding, error) {
	var out []httpMethodBinding
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != pkg {
			return true
		}
		for si := 0; si < fd.Services().Len(); si++ {
			svc := fd.Services().Get(si)
			for mi := 0; mi < svc.Methods().Len(); mi++ {
				m := svc.Methods().Get(mi)
				rule, hasRule := httpRule(m)
				if !hasRule || rule.GetBody() == "" {
					continue
				}
				verb, tmpl := verbAndTemplate(rule)
				if verb == "" {
					continue
				}
				out = append(out, httpMethodBinding{
					verb: verb, tmpl: splitPath(tmpl), input: m.Input(),
				})
			}
		}
		return true
	})
	// Порядок обхода регистра дескрипторов не определён — закрепляем свой, чтобы
	// перепись и вердикт не зависели от прогона.
	sort.Slice(out, func(i, j int) bool { return out[i].path() < out[j].path() })
	return out, nil
}

func httpRule(m protoreflect.MethodDescriptor) (*annotations.HttpRule, bool) {
	opts := m.Options()
	if opts == nil {
		return nil, false
	}
	ext := proto.GetExtension(opts, annotations.E_Http)
	rule, ok := ext.(*annotations.HttpRule)
	if !ok || rule == nil {
		return nil, false
	}
	return rule, true
}

func verbAndTemplate(r *annotations.HttpRule) (string, string) {
	switch p := r.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return "GET", p.Get
	case *annotations.HttpRule_Post:
		return "POST", p.Post
	case *annotations.HttpRule_Put:
		return "PUT", p.Put
	case *annotations.HttpRule_Patch:
		return "PATCH", p.Patch
	case *annotations.HttpRule_Delete:
		return "DELETE", p.Delete
	default:
		return "", ""
	}
}

func splitPath(p string) []string {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func urlPath(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.Index(u, "/"); i >= 0 {
		u = u[i:]
	} else {
		u = "/"
	}
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	return u
}

// matchBinding сопоставляет адрес примера с шаблоном пути.
//
// Форм подстановки в `google.api.http` ТРИ, и распознаватель обязан знать все:
// `{id}` и `{id=*}` берут ОДИН сегмент, `{name=**}` — все оставшиеся (один и
// более). Первая редакция знала только односегментную и требовала равенства длин,
// поэтому маршруты реестра (`{repository=**}` — имя репозитория содержит слэш:
// `backend/api`) не резолвились НИКОГДА: два законных примера объявлялись
// документирующими несуществующий путь. Форма, о которой распознаватель не знает,
// не даёт ни красного, ни зелёного у своего предмета — она даёт ЛОЖНОЕ КРАСНОЕ
// у соседнего, и снимают такой гейт первым.
//
// Суффикс последнего сегмента шаблона (`{id}:verb`) обязан совпасть — иначе
// `POST /x/{id}:rename` матчил бы `POST /x/{id}:archive`.
// Совпадений бывает несколько: многосегментная подстановка перекрывает более
// частные маршруты. Берётся САМОЕ ЧАСТНОЕ — больше дословных сегментов, при
// равенстве шаблон без `**`, при равенстве и этого — первый в лексикографическом
// порядке. Порядок обхода регистра дескрипторов не определён, поэтому «первый
// подошедший» давал бы вердикт, зависящий от прогона.
func matchBinding(bs []httpMethodBinding, verb, path string) (httpMethodBinding, bool) {
	segs := splitPath(path)
	var best httpMethodBinding
	found := false
	for _, b := range bs {
		if b.verb != verb || !matchTemplate(b.tmpl, segs) {
			continue
		}
		if !found || moreSpecific(b, best) {
			best, found = b, true
		}
	}
	return best, found
}

// moreSpecific — частнее ли a, чем b.
func moreSpecific(a, b httpMethodBinding) bool {
	al, bl := literalSegments(a.tmpl), literalSegments(b.tmpl)
	if al != bl {
		return al > bl
	}
	aw, bw := hasMultiWildcard(a.tmpl), hasMultiWildcard(b.tmpl)
	if aw != bw {
		return !aw
	}
	return a.path() < b.path()
}

func literalSegments(tmpl []string) int {
	n := 0
	for _, t := range tmpl {
		if !strings.HasPrefix(t, "{") {
			n++
		}
	}
	return n
}

func hasMultiWildcard(tmpl []string) bool {
	for _, t := range tmpl {
		if strings.Contains(t, "=**") {
			return true
		}
	}
	return false
}

// matchTemplate — сопоставление сегментов с шаблоном.
func matchTemplate(tmpl, segs []string) bool {
	for i, t := range tmpl {
		if !strings.HasPrefix(t, "{") {
			if i >= len(segs) || t != segs[i] {
				return false
			}
			continue
		}
		suffix := ""
		if j := strings.Index(t, "}"); j >= 0 && j+1 < len(t) {
			suffix = t[j+1:]
		}
		if strings.Contains(t, "=**") {
			// Многосегментная подстановка: забирает ВСЕ оставшиеся сегменты, но
			// хотя бы один. Суффикс проверяется на последнем из них.
			if i >= len(segs) {
				return false
			}
			return strings.HasSuffix(segs[len(segs)-1], suffix)
		}
		if i >= len(segs) || !strings.HasSuffix(segs[i], suffix) {
			return false
		}
	}
	return len(tmpl) == len(segs)
}

// nonInputMarkers — пометки, которыми отказ объявляет поле НЕВХОДНЫМ. Судится
// текст самого отказа, а не имя функции: `InvalidArg` зовётся сотнями мест по
// любому поводу (формат, диапазон, взаимоисключение), и без пометки набор
// вобрал бы каждое проверяемое поле разом — то есть запретил бы присылать всё.
//
// Пометка ищется В СКОБКАХ, и это несущее сужение, а не украшение. Конвенционный
// тон отказа — `Illegal argument <поле> (<пометка>)`, и в нём скобка привязывает
// пометку к полю из ПЕРВОГО АРГУМЕНТА. Без привязки распознаватель ошибается в
// сторону ЛОЖНОЙ НАХОДКИ, и это измерено на расширении охвата (#1643): отказ,
// названный по РОДИТЕЛЮ, а описывающий его ПОДПОЛЯ, делает запрещённым ключом сам
// родитель — при том что он обязателен, и без него запрос не собирается вовсе.
//
// # Обоснование намеренно НЕ опирается на живучесть одного отказа
//
// Здесь стоял конкретный отказ compute как ДЕЙСТВУЮЩИЙ пример. Такое обоснование
// переживает свой предмет: отказ правят по своему тикету, а цитата остаётся и
// продолжает утверждать, что дефект в дереве есть. Сужение верно СВОЙСТВОМ формы, а
// не наличием экземпляра, поэтому экземпляр здесь не называется координатой.
// Сколько таких отказов сегодня — говорит перепись, и ноль в ней законен.
var nonInputMarkers = []string{"derived from caller", "output-only", "compiled/output-only"}

// nonInputMarkerInMessage — несёт ли текст отказа пометку в конвенционной форме.
func nonInputMarkerInMessage(msg string) bool {
	for _, marker := range nonInputMarkers {
		if strings.Contains(msg, "("+marker+")") {
			return true
		}
	}
	return false
}

// nonInputMarkerFreeForm — пометка есть, но не в конвенционной форме. Такой
// отказ распознаватель НЕ читает: привязать его к полю нечем, а догадка здесь
// даёт находку на верном примере. Граница объявлена и СЧИТАЕТСЯ — молчание о
// ней снова сделало бы «ноль находок» неотличимым от «ноль прочитанного».
func nonInputMarkerFreeForm(msg string) bool {
	if nonInputMarkerInMessage(msg) {
		return false
	}
	for _, marker := range nonInputMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// collectRejectedInputFields выводит имена полей, чьё ПРИСУТСТВИЕ на входе
// прод-код отвергает, — разбором вызовов `shared.InvalidArg("<поле>", "<текст>")`.
//
// Разбор, а не поиск по образцу: те же имена стоят в комментариях рядом с самими
// ветками (и в этом файле тоже), поэтому гейт по подстроке краснел бы на
// собственном объяснении. Судится узел-вызов и его строковые аргументы.
func collectRejectedInputFields(
	tree *treecorpus.Tree, dirs []string,
) (out map[string]bool, freeForm int, err error) {
	out = map[string]bool{}
	for _, dir := range dirs {
		for _, rel := range clientTruthTreeFiles(tree, dir, true, ".go") {
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			// Исходник подаётся разбору ТЕКСТОМ, а не именем файла: имя открыл бы
			// файл сам разбор, то есть чтение вернулось бы в обход. Здесь оно
			// одно и то же для всех — [clientTruthReadTreeFile].
			src, rerr := clientTruthReadTreeFile(tree, rel)
			if rerr != nil {
				return nil, 0, fmt.Errorf("чтение %s: %w", rel, rerr)
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, rel, src, 0)
			if perr != nil {
				return nil, 0, fmt.Errorf("разбор %s: %w", rel, perr)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "InvalidArg" {
					return true
				}
				field, ok1 := clientTruthStringLit(call.Args[0])
				msg, ok2 := clientTruthStringLit(call.Args[1])
				if !ok1 || !ok2 || field == "" {
					return true
				}
				switch {
				case nonInputMarkerInMessage(msg):
					out[field] = true
				case nonInputMarkerFreeForm(msg):
					freeForm++
				}
				return true
			})
		}
	}
	return out, freeForm, nil
}

// grpcInput резолвит сообщение входа по ПОЛНОМУ имени службы и метода — так,
// как его называет сама команда. Служба вне регистра (соседний домен, опечатка)
// находкой не считается: она уходит в «адрес не сопоставился».
func grpcInput(service, method string) (protoreflect.MessageDescriptor, bool) {
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return nil, false
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, false
	}
	md := sd.Methods().ByName(protoreflect.Name(method))
	if md == nil {
		return nil, false
	}
	return md.Input(), true
}

func clientTruthStringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return s, true
}
