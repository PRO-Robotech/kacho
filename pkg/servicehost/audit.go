// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package servicehost

import (
	"fmt"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// servedSet — что процесс РЕАЛЬНО служит.
//
// Набор снимается У САМИХ СЕРВЕРОВ после регистрации (`grpc.Server.GetServiceInfo`),
// а не с того, что сервис о себе сказал. Разница несущая: сказанное можно
// забыть обновить, а служимое — нельзя, потому что служить RPC и не отдать его
// дескриптор невозможно, это одна операция. Поэтому набор не обходится и
// регистрацией мимо носителя: что бы ни зарегистрировали на сервере и как бы ни
// зарегистрировали, оно окажется здесь.
type servedSet struct {
	methods []servicecontract.MethodFQN
}

// catalogRow — одна строка каталога прав в том виде, в каком её производит
// вывод из АННОТАЦИЙ дескрипторов, слинкованных в бинарь.
//
// Источник один и тот же — опции методов proto, — поэтому «каталог, который
// читает оператор» и «правило, которое исполняет сервис» суть одно утверждение,
// и расходиться им нечему.
type catalogRow struct {
	// Method — полное имя метода в форме grpc-go.
	Method servicecontract.MethodFQN
	// Domain — proto-пакет, ВЫВЕДЕННЫЙ из имени службы, а не объявленный.
	Domain string
	// ScopeFiltered — владелец сужает выдачу на уровне данных, поэтому единичной
	// проверки на крае нет и второго рубежа за ней тоже.
	ScopeFiltered bool
	// HideExistence — отказ обязан прийти текстом промаха владельца.
	HideExistence bool
	// ObjectType — тип объекта модели прав, названный аннотацией.
	ObjectType servicecontract.ObjectType
}

// catalogView — строки каталога, доступные этому процессу.
type catalogView struct {
	rows map[servicecontract.MethodFQN]catalogRow
}

// platformService — регистрация, которая приезжает вместе с сервером, а не с
// доменом сервиса.
//
// # Зачем это ЗАКРЫТЫЙ ИМЕНОВАННЫЙ перечень, а не признак «не наш пакет»
//
// Конструктор сервера общего фундамента регистрирует здоровье и рефлексию сам.
// Строк каталога у них нет и быть не может — это не домены Kachō. Без изъятия
// НИ ОДИН процесс не поднялся бы: О2 и О9 находили бы их на каждом старте.
//
// Соблазн — написать «изымаем всё, что не `kacho.*`». Это была бы корзина
// «прочее»: в неё однажды попадёт доменный RPC, переименованный или заведённый
// в чужом пакете, и попадёт МОЛЧА. Поэтому изъятие идёт по ПОЛНОМУ ИМЕНИ
// службы, у каждой записи названа причина, и перечень истекает сам: запись,
// чью службу конструктор сервера больше не регистрирует, — находка
// (`TestPlatformExemptionsAllStillHaveASubject`).
type platformService struct {
	// why — почему у этой службы не может быть строки каталога.
	why string
}

// platformServices — те самые изъятия, поимённо.
var platformServices = map[string]platformService{
	"grpc.health.v1.Health": {
		why: "проба готовности пода; регистрируется конструктором сервера общего фундамента, " +
			"домена Kachō не имеет и в каталоге прав не представлена",
	},
	"grpc.reflection.v1.ServerReflection": {
		why: "рефлексия сервера для grpcurl и отладки; регистрируется конструктором сервера " +
			"общего фундамента, домена Kachō не имеет",
	},
	"grpc.reflection.v1alpha.ServerReflection": {
		why: "прежняя версия той же рефлексии; регистрируется тем же конструктором ради " +
			"клиентов, не умеющих v1",
	},
}

// census — объём осмотренного. Существует затем, чтобы «ноль находок» было
// отличимо от «ноль прочитанного»: отказ, который ничего не осмотрел, выглядит
// в точности как исправный процесс.
type census struct {
	methods   int
	domains   int
	rows      int
	exempted  int
	narrowers int
	hidden    int
}

func (c census) String() string {
	return fmt.Sprintf("осмотрено: методов %d (изъято платформенных %d), доменов %d, "+
		"строк каталога %d, сужаемых методов %d, скрывающих типов %d",
		c.methods, c.exempted, c.domains, c.rows, c.narrowers, c.hidden)
}

// domainOf выводит proto-пакет из полного имени метода (К-2: домен ВЫВОДИТСЯ, а
// не объявляется).
//
// Разбор НЕ ВПРАВЕ вернуть пустую строку молча. Если бы он это делал, «имя не
// разобралось» и «у домена ноль строк каталога» стали бы одним исходом, и О9
// докладывал бы о втором, когда случилось первое, — то есть посылал бы чинить
// не туда.
func domainOf(m servicecontract.MethodFQN) (string, error) {
	s := string(m)
	if !strings.HasPrefix(s, "/") {
		return "", fmt.Errorf("имя метода %q не начинается со слэша — это не та форма, "+
			"в которой grpc-go передаёт полное имя", s)
	}
	svc, _, ok := strings.Cut(strings.TrimPrefix(s, "/"), "/")
	if !ok || svc == "" {
		return "", fmt.Errorf("имя метода %q не содержит имени службы", s)
	}
	pkg := svc[:strings.LastIndex(svc, ".")+1]
	if pkg == "" {
		return "", fmt.Errorf("имя службы %q не содержит proto-пакета: домен вывести не из чего", svc)
	}
	pkg = strings.TrimSuffix(pkg, ".")
	if pkg == "" {
		return "", fmt.Errorf("имя службы %q даёт пустой proto-пакет", svc)
	}
	return pkg, nil
}

// serviceOf возвращает полное имя службы из полного имени метода.
func serviceOf(m servicecontract.MethodFQN) string {
	svc, _, _ := strings.Cut(strings.TrimPrefix(string(m), "/"), "/")
	return svc
}

// audit — отказы старта, которым нужен СЛУЖИМЫЙ НАБОР: О2, О3, О4, О5, О9.
//
// Возвращает перепись осмотренного даже при отказе — вызывающий обязан уметь
// сказать, сколько было прочитано, а не только сколько найдено.
//
// Все находки собираются РАЗОМ: отказ по первой заставил бы автора чинить по
// одной и узнавать о следующей на следующем старте.
func audit(spec servicecontract.Spec, served servedSet, cat catalogView, rpcMap authz.RPCMap) (census, error) {
	var c census
	var f findings

	if len(served.methods) == 0 {
		return c, fmt.Errorf("servicehost: %s не поднимается — служимый набор RPC пуст (ноль методов). "+
			"Ноль целей есть ОТКАЗ, а не успех: процесс, не служащий ничего, неотличим от исправного "+
			"по всякому признаку, кроме этого", nameOf(spec))
	}

	// ── О2 и О9: служимое против каталога и против карты ────────────────────
	domains := map[string]int{} // домен -> сколько записей он дал КАРТЕ
	seenDomain := map[string]bool{}
	for _, m := range served.methods {
		svc := serviceOf(m)
		if p, exempt := platformServices[svc]; exempt {
			c.exempted++
			_ = p
			continue
		}
		c.methods++

		dom, err := domainOf(m)
		if err != nil {
			f.add(string(m), "домен не выводится: "+err.Error())
			continue
		}
		seenDomain[dom] = true

		// О2: служится RPC, у которого нет строки каталога его домена.
		if _, ok := cat.rows[m]; !ok {
			f.add(string(m), "служится, но строки каталога у метода нет: звено решения о доступе "+
				"отвергнет его как незамапленный, и отказ этот не назовёт ни метода, ни упущения")
		}
		// Карта — то, что звено спрашивает НА САМОМ ДЕЛЕ. Считаем вклад домена
		// именно в неё: строка каталога, не доехавшая до карты, оставляет метод
		// без правила ровно так же, как её отсутствие.
		if _, ok := rpcMap[string(m)]; ok {
			domains[dom]++
		}
	}
	for dom := range seenDomain {
		if domains[dom] == 0 {
			f.add(dom, "выведенный домен не дал карте прав НИ ОДНОЙ записи. Ноль целей есть отказ, "+
				"а не успех: домен, о котором карта не знает ничего, неотличим от домена, у которого всё на месте")
		}
	}
	c.domains = len(seenDomain)
	c.rows = len(cat.rows)

	auditNarrowers(&c, &f, spec, served, cat) // О3 + О4
	auditHideExistence(&c, &f, spec, cat)     // О5

	if err := f.err(nameOf(spec)); err != nil {
		return c, err
	}
	return c, nil
}

// auditNarrowers — О3 и О4, ОБЕ половины сразу.
//
// Без второй половины пропажа проводки невидима: сужатель, потерянный при
// переносе слоёв, не краснеет нигде, потому что каталог о нём не спрашивает —
// ровно тот случай, который уже был.
//
// Здесь же истекает заявление «не применимо»: пока у домена нет строк
// `scope_filtered`, оно законно; появилась первая — то же самое заявление стало
// находкой, и вспоминать о нём никому не нужно.
func auditNarrowers(c *census, f *findings, spec servicecontract.Spec, served servedSet, cat catalogView) {
	declared := map[servicecontract.MethodFQN]struct{}{}
	for _, m := range served.methods {
		if row, ok := cat.rows[m]; ok && row.ScopeFiltered {
			declared[m] = struct{}{}
		}
	}
	c.narrowers = len(declared)

	wired, hasValue := spec.Narrowers.Get()

	// О3: объявлено каталогом, не предъявлено дескриптором.
	for m := range declared {
		n, ok := wired[m]
		if !hasValue || !ok || n == nil {
			why := "каталог объявляет метод сужаемым (`scope_filtered`), а дескриптор проводки " +
				"сужателя на него не несёт: за таким методом пообъектной проверки на крае нет вовсе, " +
				"поэтому отсутствующий сужатель означает не «строже», а «без рубежа»"
			if !hasValue {
				if because, na := spec.Narrowers.NotApplicableBecause(); na {
					why += ". Ось объявлена неприменимой (" + because + ") — заявление истекло: " +
						"у домена появилась строка, которую оно исключало"
				}
			}
			f.add("Narrowers "+string(m), why)
		}
	}
	// О4: предъявлено дескриптором, каталогом не объявлено.
	for m, n := range wired {
		if _, ok := declared[m]; ok {
			continue
		}
		if n == nil {
			continue // уже названо выше либо не относится к сужаемым
		}
		f.add("Narrowers "+string(m), "дескриптор несёт проводку сужателя на метод, которого каталог "+
			"сужаемым не объявлял: либо метод перестал быть сужаемым и проводка пережила свой предмет, "+
			"либо строку каталога потеряли — и без этой половины пропажа была бы невидима")
	}
}

// auditHideExistence — О5, тоже в обе стороны.
//
// Скрытие существования работает ровно настолько, насколько текст отказа
// неотличим от настоящего промаха владельца. Поэтому проверяется не только
// НАЛИЧИЕ формы, но и её ФОРМА: ровно одна подстановка идентификатора.
func auditHideExistence(c *census, f *findings, spec servicecontract.Spec, cat catalogView) {
	hidden := map[servicecontract.ObjectType]struct{}{}
	for _, row := range cat.rows {
		if row.HideExistence && row.ObjectType != "" {
			hidden[row.ObjectType] = struct{}{}
		}
	}
	c.hidden = len(hidden)

	forms, hasValue := spec.HideExistence.Get()

	for ot := range hidden {
		form, ok := forms[ot]
		if !hasValue || !ok {
			why := "каталог называет тип среди скрывающих существование, а форма отказа для него " +
				"не объявлена: ресурс ответил бы своей, отличимой формой — то есть оракулом существования"
			if !hasValue {
				if because, na := spec.HideExistence.NotApplicableBecause(); na {
					why += ". Ось объявлена неприменимой (" + because + ") — заявление истекло"
				}
			}
			f.add("HideExistence "+string(ot), why)
			continue
		}
		if n := strings.Count(string(form), "%s"); n != 1 || strings.Count(string(form), "%") != n {
			f.add("HideExistence "+string(ot), fmt.Sprintf(
				"форма отказа %q обязана нести РОВНО одну подстановку `%%s` под идентификатор, "+
					"который назвал вызывающий; любой другой текст отличим от настоящего промаха владельца",
				string(form)))
			continue
		}
		// Форма сверяется с ТЕМ, ЧТО РЕАЛЬНО ОТВЕТИТ звено решения о доступе.
		// Объявление и производитель текста — два места об одном предмете, и без
		// сверки расхождение между ними осталось бы невидимым: отвечает всегда
		// таблица владельцев, поэтому объявленная форма могла бы разойтись с
		// действительностью и не покраснеть нигде.
		owner, known := authz.OwnerNotFoundFormat(string(ot))
		switch {
		case !known:
			f.add("HideExistence "+string(ot), "форма отказа объявлена для типа, которого нет в таблице "+
				"промахов владельцев: звено ответит нейтральным «not found» — текстом, не похожим ни на "+
				"один ответ владельца, то есть той самой приметой, ради устранения которой скрытие и делается")
		case owner != string(form):
			f.add("HideExistence "+string(ot), fmt.Sprintf(
				"объявленная форма %q расходится с той, которой ответит звено решения о доступе (%q). "+
					"Отвечает вторая, поэтому объявление здесь описывает не действительность; сведите их "+
					"дословно — скрытие работает ровно настолько, насколько текст отказа неотличим от промаха владельца",
				string(form), owner))
		}
	}
	// Зеркально: форма, которой больше нечего скрывать, — находка.
	for ot := range forms {
		if _, ok := hidden[ot]; !ok {
			f.add("HideExistence "+string(ot), "объявлена форма отказа для типа, который каталог "+
				"скрывающим не называет: запись пережила свой предмет и утверждает, что платформа "+
				"отвечает голосом владельца там, где владельца уже нет")
		}
	}
}

func nameOf(s servicecontract.Spec) string {
	if strings.TrimSpace(string(s.Service)) == "" {
		return "<безымянный сервис>"
	}
	return string(s.Service)
}

// findings — накопитель отказов старта носителя. Отдельный от накопителя
// дескриптора намеренно: тексты разные, и смешивать «поле не заполнено» с
// «объявленное не сходится со служимым» значило бы терять, что именно чинить.
type findings struct{ items map[string][]string }

func (f *findings) add(subject, why string) {
	if f.items == nil {
		f.items = map[string][]string{}
	}
	f.items[subject] = append(f.items[subject], why)
}

func (f *findings) err(svc string) error {
	if len(f.items) == 0 {
		return nil
	}
	subjects := make([]string, 0, len(f.items))
	for k := range f.items {
		subjects = append(subjects, k)
	}
	sort.Strings(subjects)

	var b strings.Builder
	fmt.Fprintf(&b, "servicehost: %s не поднимается — объявленное не сходится со служимым (%d предмет(ов)):",
		svc, len(subjects))
	for _, k := range subjects {
		for _, why := range f.items[k] {
			fmt.Fprintf(&b, "\n  · %s: %s", k, why)
		}
	}
	return fmt.Errorf("%s", b.String())
}
