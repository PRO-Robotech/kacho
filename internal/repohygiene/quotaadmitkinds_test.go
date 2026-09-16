// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotaadmitkinds_test.go — репо-широкий гейт: вид, о котором СПРАШИВАЕТ
// совещательная полоса, обязан быть видом, который СПИСЫВАЕТ триггер, — и это
// судится у КАЖДОГО владельца величин, а не у одного.
//
// ЗАЧЕМ ОН СУЩЕСТВУЕТ. Полос учёта две: совещательная отвечает арендатору
// синхронно (`quota.Guard.Admit`, вид приезжает из use-case'а), а авторитетная
// списывает место триггером (вид приезжает аргументом `TG_ARGV[0]`). Виды
// написаны в РАЗНЫХ файлах и на разных языках, поэтому разъехаться они могут
// молча — и разъедутся не там, где это заметно.
//
// ЧТО ПРОИСХОДИТ ПРИ РАСХОЖДЕНИИ. Use-case, спрашивающий про вид, которого
// триггер не знает, получает «потолок не назван» ВСЕГДА: строки такого вида не
// заводит никто. То есть опечатка в одном имени выключает создание целого
// ресурса — наглухо, для всех арендаторов, и выглядит это как «платформа не
// назвала потолок», а не как опечатка.
//
// Обратное расхождение тише и хуже: вид, который триггер списывает, а полоса про
// него не спрашивает, теряет РАННИЙ отказ. Арендатор перестаёт получать 429 и
// узнаёт об исчерпании из отказа операции — поведение, которое приёмка называет
// негодным, и которое ничем себя не выдаёт, потому что предел при этом
// продолжает соблюдаться.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ ИСПРАВЛЕНО ПРОТИВ ПРЕЖНЕЙ РЕДАКЦИИ (задача продукта #2669)
//
// ОХВАТ. Прежняя редакция несла ДВЕ выписанные координаты, обе на `vpc`: файл
// миграции и каталог use-case'ов. Владельцев величин пять, видов девятнадцать —
// вне взгляда гейта оставались одиннадцать из девятнадцати, а его «ноль находок»
// читалось как свойство платформы. Охват теперь ВЫВОДИТСЯ из дерева: владелец —
// служба, чья миграция несёт списывающий триггер, и миграции её читаются ВСЕ, а
// не одна названная.
//
// РАСПОЗНАВАТЕЛЬ. Прежняя редакция объявляла предпосылкой свойство дерева — «в
// этом дереве виды пишутся литералами, первое же отступление станет находкой для
// читателя». Отступление БЫЛО и находкой не стало ни для кого: `storage` подаёт
// вид ИМЕНОВАННОЙ КОНСТАНТОЙ (`quota.KindVolumes`), и лежало это вне охвата.
// Распознаватель знает теперь обе формы записи и разрешает константу по её
// объявлению в дереве той же службы.
//
// ЧЕГО ГЕЙТ НЕ ЛОВИТ, И ЭТО СКАЗАНО ЧЕСТНО. Вид, вычисленный в рантайме
// (склейка, выбор по условию), ему не виден. Не виден и вид, приехавший
// ПАРАМЕТРОМ: `Guard.Admit(ctx, carrier, id, kind)` внутри самой полосы
// пересылает чужой аргумент, и разрешать там нечего. Такие вызовы гейт не
// молчаливо пропускает, а СЧИТАЕТ и печатает отдельной строкой переписи: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
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

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// quotaAdmitVerbs — глаголы совещательной полосы. Вид у обоих — ПОСЛЕДНИЙ
// аргумент, и это не совпадение формы: у `AdmitCarrier` перед ним стоит носитель,
// у `Admit` — корень аренды, и в обоих случаях спрашиваемое стоит последним.
var quotaAdmitVerbs = map[string]bool{"Admit": true, "AdmitCarrier": true}

// quotaSides — две стороны учёта одного владельца.
type quotaSides struct {
	charged     map[string]bool
	asked       map[string][]string
	unresolved  int
	sqlFiles    int
	goFiles     int
	constsKnown int
	// onProject, nested — разбиение списываемых видов ПО НОСИТЕЛЮ. Печатается
	// раздельно: число видов на носителе-проекте есть та величина, которой
	// приёмка называет каталог посадки, и слив их в одну сумму сделал бы
	// расхождение с ней неразличимым.
	//
	// МНОЖЕСТВА, а не счётчики: одна и та же пара «вид — носитель» объявляется
	// у vpc дважды (вторая миграция переобъявляет триггер с носителем-родителем),
	// и счёт вхождений дал бы 22 там, где различных видов 19.
	onProject map[string]bool
	nested    map[string]bool
}

// quotaAdvisoryGap — ОДИН известный пробел совещательной полосы: вид списывается,
// а полоса про него не спрашивает.
//
// Ведомость существует потому, что гейт нашёл пробел, которого не видел прежний
// охват, а закрытие пробела меняет НАБЛЮДАЕМОЕ поведение пути запроса
// (синхронный отказ вместо отказа операции) — то есть предмет со своей приёмкой,
// а не правка охвата гейта.
//
// Запись САМОИСТЕКАЕТ: как только полоса начнёт спрашивать про этот вид либо
// триггер перестанет его списывать, записи станет нечего прощать, и гейт назовёт
// это находкой — иначе послабление пережило бы свой предмет.
type quotaAdvisoryGap struct {
	Owner string
	Kind  string
	Why   string
}

// quotaAdvisoryGaps — ведомость известных пробелов.
//
// ТРИ ВЛОЖЕННЫХ ВИДА vpc: триггер считает их в СЕТИ (`kacho_quota_count(…,
// 'network_id', 'vpc.network.subnet')`), а совещательная полоса домена глагола
// про носителя-родителя не несёт вовсе — у соседей он есть (`AdmitCarrier` у nlb
// и registry), у vpc нет. Следствие для арендатора: вложенный предел соблюдается,
// но ранний отказ по нему не приходит, и об исчерпании он узнаёт из отказа
// операции.
var quotaAdvisoryGaps = []quotaAdvisoryGap{
	{Owner: "vpc", Kind: "vpc.network.subnet", Why: "kacho#2673"},
	{Owner: "vpc", Kind: "vpc.network.securityGroup", Why: "kacho#2673"},
	{Owner: "vpc", Kind: "vpc.network.routeTable", Why: "kacho#2673"},
}

func TestQuotaAdvisoryBandAsksAboutTheKindsTheTriggerCharges(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	sides := quotaSidesFromTree(t, root)
	if len(sides) == 0 {
		t.Fatalf("предпосылка гейта не выполнена: под %s не найдено ни одного владельца "+
			"величин — форма объявления триггеров учёта изменилась, и гейт судит пустоту",
			quotaServicesDir)
	}

	owners := make([]string, 0, len(sides))
	for name := range sides {
		owners = append(owners, name)
	}
	sort.Strings(owners)

	totalCharged, totalAsked, unresolved, sqlSeen, goSeen, constsSeen := 0, 0, 0, 0, 0, 0
	onProject, nested := 0, 0
	for _, name := range owners {
		s := sides[name]
		totalCharged += len(s.charged)
		totalAsked += len(s.asked)
		unresolved += s.unresolved
		sqlSeen += s.sqlFiles
		goSeen += s.goFiles
		constsSeen += s.constsKnown
		onProject += len(s.onProject)
		nested += len(s.nested)
	}

	// Перепись осмотренного печатается ДО вердикта: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного», и по этой строке видно, вырос ли охват.
	t.Logf("осмотрено: доменов %d (%s); миграций %d; файлов Go %d; "+
		"именованных видов разрешено %d; видов списывается %d "+
		"(на носителе-проекте %d, на носителе-родителе %d); видов спрашивается %d; "+
		"вызовов с неразрешимым видом %d",
		len(sides), strings.Join(owners, ", "), sqlSeen, goSeen, constsSeen,
		totalCharged, onProject, nested, totalAsked, unresolved)

	for _, name := range owners {
		s := sides[name]
		if len(s.charged) == 0 {
			t.Errorf("%s: видов списывается НОЛЬ — форма объявления триггеров изменилась, "+
				"и вердикт по этому владельцу беспредметен", name)
			continue
		}
		if len(s.asked) == 0 {
			t.Errorf("%s: вызовов совещательной полосы НЕ НАЙДЕНО ни одного — форма вызова "+
				"изменилась, и вердикт по этому владельцу беспредметен", name)
			continue
		}
		for _, f := range judgeQuotaSides(name, s.charged, s.asked, quotaForgiven(name)) {
			t.Error(f)
		}
	}

	// САМОИСТЕЧЕНИЕ ведомости: запись, которой нечего прощать, — находка.
	for _, gap := range quotaAdvisoryGaps {
		s, ok := sides[gap.Owner]
		if !ok {
			t.Errorf("ведомость прощает %s у владельца %s, которого в дереве нет — "+
				"послабление пережило свой предмет", gap.Kind, gap.Owner)
			continue
		}
		if !s.charged[gap.Kind] {
			t.Errorf("ведомость прощает %s у %s, а триггер этот вид БОЛЬШЕ НЕ СПИСЫВАЕТ — "+
				"записи нечего прощать, снимите её тем же изменением (%s)",
				gap.Kind, gap.Owner, gap.Why)
			continue
		}
		if _, asked := s.asked[gap.Kind]; asked {
			t.Errorf("ведомость прощает %s у %s, а полоса УЖЕ про него спрашивает — "+
				"пробел закрыт, снимите запись тем же изменением (%s)",
				gap.Kind, gap.Owner, gap.Why)
		}
	}
}

// judgeQuotaSides — ВЕРДИКТ, отделённый от чтения дерева: инъекция подаёт ему
// синтетические стороны и проверяет, что он способен упасть по каждой оси.
// quotaForgiven — известные пробелы ОДНОГО владельца.
func quotaForgiven(owner string) map[string]bool {
	out := map[string]bool{}
	for _, gap := range quotaAdvisoryGaps {
		if gap.Owner == owner {
			out[gap.Kind] = true
		}
	}
	return out
}

func judgeQuotaSides(
	owner string, charged map[string]bool, asked map[string][]string, forgiven map[string]bool,
) []string {
	var out []string

	var stray []string
	for kind, at := range asked {
		if !charged[kind] {
			stray = append(stray, kind+" ← "+strings.Join(at, ", "))
		}
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		out = append(out, owner+": полоса спрашивает про вид, который триггер НЕ списывает ("+
			strconv.Itoa(len(stray))+"). Создание такого ресурса отвергается «потолок не "+
			"назван» ВСЕГДА: строк этого вида не заводит никто:\n  "+strings.Join(stray, "\n  "))
	}

	var uncovered []string
	for kind := range charged {
		if _, ok := asked[kind]; ok || forgiven[kind] {
			continue
		}
		uncovered = append(uncovered, kind)
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		out = append(out, owner+": триггер списывает вид, о котором полоса НЕ спрашивает ("+
			strconv.Itoa(len(uncovered))+"). Арендатор теряет ранний отказ: исчерпание "+
			"приезжает отказом операции вместо синхронного 429, и это ничем себя не "+
			"выдаёт:\n  "+strings.Join(uncovered, "\n  "))
	}
	return out
}

// quotaSidesFromTree ВЫВОДИТ владельцев и обе их стороны из дерева.
func quotaSidesFromTree(t *testing.T, root string) map[string]*quotaSides {
	t.Helper()

	sqlFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, quotaServicesDir), ".sql")
	if err != nil {
		t.Fatalf("состав дерева под %s: %v", quotaServicesDir, err)
	}
	goFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, quotaServicesDir), ".go")
	if err != nil {
		t.Fatalf("состав дерева под %s: %v", quotaServicesDir, err)
	}

	out := map[string]*quotaSides{}
	side := func(service string) *quotaSides {
		s, ok := out[service]
		if !ok {
			s = &quotaSides{
				charged:   map[string]bool{},
				asked:     map[string][]string{},
				onProject: map[string]bool{},
				nested:    map[string]bool{},
			}
			out[service] = s
		}
		return s
	}

	// Сторона СПИСАНИЯ: ВСЕ миграции службы, а не одна названная.
	for _, path := range sqlFiles {
		service, ok := quotaServiceOf(root, path)
		if !ok {
			continue
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", path, rerr)
		}
		// СОЮЗ обоих носителей: совещательная полоса спрашивает и про вид,
		// считаемый в проекте, и про вид, считаемый в РОДИТЕЛЕ
		// (`AdmitCarrier`). Прежняя редакция читала только первый аргумент
		// объявления и теряла второй род молча — то есть объявляла находкой
		// законный вопрос, а расхождение по нему не находила вовсе.
		onProject, nested := quotaChargedKinds(string(b))
		if len(onProject) == 0 && len(nested) == 0 {
			continue
		}
		s := side(service)
		s.sqlFiles++
		for kind := range onProject {
			s.charged[kind] = true
			s.onProject[kind] = true
		}
		for kind := range nested {
			s.charged[kind] = true
			s.nested[kind] = true
		}
	}

	// Сторона ВОПРОСА. Разбор идёт в ДВА прохода: сперва собираются именованные
	// виды службы, потом вопросы — иначе константа, объявленная в файле, который
	// встретится позже, осталась бы неразрешённой, и порядок обхода решал бы
	// вердикт.
	type parsedFile struct {
		file *ast.File
		rel  string
	}
	parsed := map[string][]parsedFile{}
	consts := map[string]map[string]string{}
	for _, path := range goFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		service, ok := quotaServiceOf(root, path)
		if !ok {
			continue
		}
		if _, isOwner := out[service]; !isOwner {
			continue
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", path, rerr)
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, b, 0)
		if perr != nil {
			t.Fatalf("разбор %s: %v", path, perr)
		}
		rel, _ := filepath.Rel(root, path)
		parsed[service] = append(parsed[service], parsedFile{file: file, rel: filepath.ToSlash(rel)})
		side(service).goFiles++
		if consts[service] == nil {
			consts[service] = map[string]string{}
		}
		for name, value := range quotaKindConstants(file) {
			consts[service][name] = value
		}
	}
	for service, files := range parsed {
		s := side(service)
		s.constsKnown = len(consts[service])
		for _, pf := range files {
			asked, unresolved := quotaAskedKinds(pf.file, consts[service])
			s.unresolved += unresolved
			for _, kind := range asked {
				s.asked[kind] = append(s.asked[kind], pf.rel)
			}
		}
	}

	// Владелец — тот, у кого есть списывающий триггер. Служба без него учёта не
	// ведёт, и судить её этим гейтом нечем.
	for name, s := range out {
		if len(s.charged) == 0 {
			delete(out, name)
		}
	}
	return out
}

// quotaKindConstants — ИМЕНОВАННЫЕ виды файла: `KindVolumes = "storage.volumes"`.
//
// Ключ — только имя: вызов называет константу либо голым именем внутри пакета,
// либо через имя пакета извне, и оба написания приводятся к одному.
func quotaKindConstants(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil || !quotaKindShapeRe.MatchString(v) {
					continue
				}
				out[name.Name] = v
			}
		}
	}
	return out
}

// quotaAskedKinds — виды, о которых СПРАШИВАЕТ этот файл, и число вызовов, чей
// вид разрешить нечем.
//
// Судится узел разбора, а не текст: имя вида встречается в комментариях и в
// прозе отказов, и проверка по подстроке краснела бы на собственном объяснении.
func quotaAskedKinds(file *ast.File, consts map[string]string) ([]string, int) {
	var (
		out        []string
		unresolved int
	)
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !quotaAdmitVerbs[sel.Sel.Name] {
			return true
		}
		kind, ok := quotaResolveKindArg(call.Args[len(call.Args)-1], consts)
		if !ok {
			unresolved++
			return true
		}
		out = append(out, kind)
		return true
	})
	return out, unresolved
}

// quotaResolveKindArg — вид из аргумента: строковый литерал ЛИБО именованная
// константа, названная голым именем или через имя пакета.
func quotaResolveKindArg(arg ast.Expr, consts map[string]string) (string, bool) {
	switch e := arg.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(e.Value)
		if err != nil || !quotaKindShapeRe.MatchString(v) {
			return "", false
		}
		return v, true
	case *ast.Ident:
		v, ok := consts[e.Name]
		return v, ok
	case *ast.SelectorExpr:
		v, ok := consts[e.Sel.Name]
		return v, ok
	}
	return "", false
}
