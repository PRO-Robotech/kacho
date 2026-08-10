// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import "fmt"

// Form names one of the shapes under comparison.
type Form string

const (
	FormA   Form = "A-flat"          // tuple per object × verb × subject           N·M·S
	FormB   Form = "B-group"         // grant to group#member                       N·M + S
	FormC   Form = "C-role-relation" // one role relation per object, verbs derive  N·S
	FormD   Form = "D-container"     // objects point at a container                N + S
	FormBCD Form = "BCD-combined"    // container + group                           N + S + 1
	// FormE — реляционная: вердикт вычисляется запросом к БД поверх того же
	// зеркала объектов, без внешнего движка отношений. Выдача — строка привязки
	// и её правило-селектор: S + 2. Разворота в состав нет by construction.
	FormE Form = "E-relational"
)

// AllForms in report order: today's shape first, then each fold, then the
// composition. The composition is measured BECAUSE the folds compose — their gains
// need not add, and their costs (graph depth on the read path) do accumulate.
//
// Форма E стоит последней и втянута в ОБЕ предпосылки поимённо: попадание в этот
// перечень само по себе втягивает её только в вопросник эквивалентности (тот
// итерируется по перечню), а в доказательстве различающей способности неравенства
// объёмов выписаны по именам — и без своего неравенства форма E измерялась бы вне
// доказательства различимости.
var AllForms = []Form{FormA, FormB, FormC, FormD, FormBCD, FormE}

// Fixture ids. Fixed strings, not random: the comparison is between shapes, and a
// difference in ids would be a difference in the data (requirement 3).
const (
	clusterObj = "cluster:cluster_kacho_root"
	accountObj = "account:acc-bench"
	projectObj = "project:prj-bench"
	groupObj   = "group:grp-bench"
	labelObj   = "label_set:lbl-bench"

	// ЧУЖОЙ арендатор — второй аккаунт того же кластера и его проект.
	//
	// Заведён не для полноты картины, а потому что без него один конъюнкт вердикта
	// формы E — область выдачи — ТОЖДЕСТВЕННО ИСТИНЕН: когда аккаунт и проект в
	// фикстуре по одному, «объект лежит в области привязки» верно про каждый объект,
	// о котором вопросник спрашивает. Инъекция «выкинуть проверку области целиком»
	// оставляла зелёными все шесть проб вердикта (найдено адверсарной проверкой
	// 2026-08-10). Различающая сила была у трёх конъюнктов из четырёх, а решение
	// принимается для МНОГОАРЕНДНОЙ системы — и именно на кросс-арендной границе
	// реляционная переписка правдоподобно расходится с движком: у формы A отказ на
	// чужом объекте СТРУКТУРЕН (кортежа нет — разрешать нечему), у формы E он
	// держится на рукописном условии транзитивной вложенности области.
	otherAccountObj = "account:acc-bench-other"
	otherProjectObj = "project:prj-bench-other"

	// Привязка формы E и её «встраиваемый» близнец — тот, что пишется в одной
	// транзакции с предметом выдачи.
	bindingObj       = "access_binding:ab-bench"
	inlineBindingObj = "access_binding:ab-inline"

	// Каскадный принципал — администратор аккаунта (уровень 3 супер-доступа).
	cascadeAdmin = "user:cascade-account-admin"
)

// Общий словарь намерения — отношения, которыми формы описывают мутацию.
//
// Формы A–BCD пишут его как есть (это их wire-форма), форма E переводит его в
// строки на своей стороне. Словарь общий, а не формо-нейтральное намерение,
// намеренно: так производители намерения ниже остаются нетронутыми, и пять
// прежних форм измеряются ровно тем же кодом, что и до появления шестой.
const (
	bindingRelPrefix       = "binding_role_"         // + роль; User — объект области
	bindingSubjectRel      = "binding_subject"       // субъект привязки
	bindingSelectorRel     = "binding_selector"      // правило-селектор привязки
	labelsRel              = "labels"                // объект входит в набор (общее с формой D)
	cascadeAccountAdminRel = "cascade_account_admin" // каскад, уровень 3
	cascadeClusterAdminRel = "cascade_cluster_admin" // каскад, уровни 1-2
)

// Scenario is the ONE dataset every shape is measured against.
type Scenario struct {
	N        int      // objects inside the selected set
	Spare    int      // objects OUTSIDE it, kept for the relabel operations
	Verbs    []string // M — the verbs the role grants
	Subjects []string // S — full principal strings, e.g. "user:u000"
	Role     string   // "viewer" | "editor" — selects the C/D grant relation
}

// DefaultVerbs is the verb set 22 of the model's 24 verb-bearing types declare.
func DefaultVerbs() []string { return []string{"v_get", "v_list", "v_update", "v_delete"} }

// AllVerbs is every verb the bench type declares — used by the equivalence probe,
// which must also ask about verbs the role does NOT grant.
func AllVerbs() []string { return []string{"v_get", "v_list", "v_update", "v_delete"} }

// NewScenario builds the dataset. Subjects are `user:` principals because that is
// what a tenant binding names; `service_account:` resolves through the same direct
// userset and would not change the shape of the graph.
func NewScenario(n, spare, subjects int, role string, verbs []string) Scenario {
	subs := make([]string, subjects)
	for i := range subs {
		subs[i] = fmt.Sprintf("user:u%04d", i)
	}
	return Scenario{N: n, Spare: spare, Verbs: verbs, Subjects: subs, Role: role}
}

// Object returns the id of the i-th object. Objects [0,N) are inside the selected
// set; [N, N+Spare) are outside it.
func (sc Scenario) Object(i int) string { return fmt.Sprintf("%s:obj-%06d", BenchType, i) }

// Objects returns the in-set objects.
func (sc Scenario) Objects() []string {
	out := make([]string, sc.N)
	for i := range out {
		out[i] = sc.Object(i)
	}
	return out
}

// ForeignObject — объект проекта ЧУЖОГО аккаунта, ПОМЕЧЕННЫЙ меткой набора.
//
// Метка обязательна и она здесь главное. Без неё отказ на этом объекте
// объясняется правилом-селектором, и конъюнкт области снова остаётся
// непроверенным — вопрос стал бы вторым экземпляром уже покрытого «объект вне
// набора». С меткой у формы E выполнены ВСЕ прочие конъюнкты (субъект назван в
// привязке, роль даёт глагол, метка объекта под селектор попадает), и отказать
// обязана ровно область.
//
// Идентификатор намеренно не из нумерации `Object(i)`: объект чужого арендатора
// не должен попадать ни в набор, ни в запасные — иначе он уехал бы в выдачу,
// страницу или переразметку и сдвинул бы базу сравнения пяти форм.
func (sc Scenario) ForeignObject() string { return BenchType + ":obj-foreign" }

// LabelledInMirror — объекты, чья строка зеркала несёт метку набора.
//
// Метку ставит ВЛАДЕЛЕЦ объекта, а не выдача: зеркало существует независимо от
// того, выдано ли кому-нибудь право. Поэтому чужой объект помечен так же, как
// свои, — и это не поблажка форме E, а единственный способ спросить её про
// область, не подсказав ответ другим конъюнктом.
func (sc Scenario) LabelledInMirror() map[string]bool {
	out := make(map[string]bool, sc.N+1)
	for _, o := range sc.Objects() {
		out[o] = true
	}
	out[sc.ForeignObject()] = true
	return out
}

// Structural returns the parent-pointer tuples. They are IDENTICAL in every shape
// and are therefore excluded from the grant accounting: attributing them to a shape
// would flatter whichever shape has fewest grant tuples.
//
// Второй аккаунт, его проект и один объект в нём — тоже структурное: они не входят
// ни в набор, ни в запасные, поэтому ни одна операция записи их не касается и
// величина выдачи каждой формы остаётся прежней. Проверено прогоном, а не
// рассуждением: строки выдачи всех шести форм до и после этой правки совпали, а
// структурная часть выросла ровно на три строки у каждой (см. `ExpectedStructuralRows`).
func (sc Scenario) Structural() []Tuple {
	out := make([]Tuple, 0, sc.N+sc.Spare+5)
	out = append(out,
		Tuple{User: clusterObj, Relation: "cluster", Object: accountObj},
		Tuple{User: accountObj, Relation: "account", Object: projectObj},
	)
	for i := 0; i < sc.N+sc.Spare; i++ {
		out = append(out, Tuple{User: projectObj, Relation: "project", Object: sc.Object(i)})
	}
	out = append(out,
		Tuple{User: clusterObj, Relation: "cluster", Object: otherAccountObj},
		Tuple{User: otherAccountObj, Relation: "account", Object: otherProjectObj},
		Tuple{User: otherProjectObj, Relation: "project", Object: sc.ForeignObject()},
	)
	return out
}

func (sc Scenario) grantRelation() string { return "grant_" + sc.Role }

// Model returns the DSL each shape needs, derived from the canonical text.
//
// A and B share the canonical model unchanged — that is the point of B: it is a
// discipline of granting, not a model change. C and D each carry one transform.
// BCD reuses D's model because `group#member` is already an accepted subject on the
// container's grant relations.
//
// У формы E модели нет вовсе, и это ЗАКОННЫЙ ответ, а не отказ: она возвращает
// ErrModelNotRequired. Ответ отделён от «неизвестной формы» намеренно — иначе
// «модель не требуется» покрывало бы и опечатку в имени формы.
func ModelFor(f Form, canon string) (dsl string, note string, err error) {
	switch f {
	case FormA, FormB:
		return canon, "canonical, unchanged", nil
	case FormE:
		return "", "модель не требуется — вердикт вычисляется запросом к БД", ErrModelNotRequired
	case FormC:
		r, e := ModelC(canon)
		if e != nil {
			return "", "", e
		}
		return r.DSL, fmt.Sprintf("canonical + grant_viewer/grant_editor on %s, %d verbs rewritten", BenchType, len(r.VerbsTouched)), nil
	case FormD, FormBCD:
		r, e := ModelD(canon)
		if e != nil {
			return "", "", e
		}
		return r.DSL, fmt.Sprintf("canonical + type label_set + labels pointer on %s, %d verbs rewritten", BenchType, len(r.VerbsTouched)), nil
	default:
		return "", "", fmt.Errorf("unknown form %q", f)
	}
}

// Grant returns the tuples that materialize the binding over the in-set objects.
func Grant(f Form, sc Scenario) []Tuple {
	rel := sc.grantRelation()
	switch f {
	case FormA:
		out := make([]Tuple, 0, sc.N*len(sc.Verbs)*len(sc.Subjects))
		for i := 0; i < sc.N; i++ {
			obj := sc.Object(i)
			for _, v := range sc.Verbs {
				for _, s := range sc.Subjects {
					out = append(out, Tuple{User: s, Relation: v, Object: obj})
				}
			}
		}
		return out
	case FormB:
		out := make([]Tuple, 0, sc.N*len(sc.Verbs)+len(sc.Subjects))
		for _, s := range sc.Subjects {
			out = append(out, Tuple{User: s, Relation: "member", Object: groupObj})
		}
		for i := 0; i < sc.N; i++ {
			obj := sc.Object(i)
			for _, v := range sc.Verbs {
				out = append(out, Tuple{User: groupObj + "#member", Relation: v, Object: obj})
			}
		}
		return out
	case FormC:
		out := make([]Tuple, 0, sc.N*len(sc.Subjects))
		for i := 0; i < sc.N; i++ {
			obj := sc.Object(i)
			for _, s := range sc.Subjects {
				out = append(out, Tuple{User: s, Relation: rel, Object: obj})
			}
		}
		return out
	case FormD:
		out := make([]Tuple, 0, sc.N+len(sc.Subjects))
		for i := 0; i < sc.N; i++ {
			out = append(out, Tuple{User: labelObj, Relation: "labels", Object: sc.Object(i)})
		}
		for _, s := range sc.Subjects {
			out = append(out, Tuple{User: s, Relation: rel, Object: labelObj})
		}
		return out
	case FormBCD:
		out := make([]Tuple, 0, sc.N+len(sc.Subjects)+1)
		for i := 0; i < sc.N; i++ {
			out = append(out, Tuple{User: labelObj, Relation: "labels", Object: sc.Object(i)})
		}
		for _, s := range sc.Subjects {
			out = append(out, Tuple{User: s, Relation: "member", Object: groupObj})
		}
		out = append(out, Tuple{User: groupObj + "#member", Relation: rel, Object: labelObj})
		return out
	case FormE:
		// Выдача формы E — строка привязки, её субъекты и её правило-селектор.
		// Объекты набора здесь НЕ перечисляются: метка живёт в зеркале, зеркало
		// наполняют владельцы объектов, и выдача узнаёт набор запросом. Отсюда и
		// то, что операция «выдать» у двух форм мерит разные события.
		return grantE(sc, bindingObj, projectObj)
	}
	return nil
}

// grantE собирает выдачу формы E: одна привязка + S субъектов + один селектор.
//
// Область привязки задаётся объектом (`project:` либо `account:`): вложенность
// области транзитивна, поэтому аккаунтная привязка накрывает объекты проектов
// этого аккаунта — и родительский указатель аккаунта в зеркале имеет читателя, а
// не лежит колонкой, которую никто не спрашивает.
func grantE(sc Scenario, binding, scope string) []Tuple {
	out := make([]Tuple, 0, len(sc.Subjects)+2)
	out = append(out, Tuple{User: scope, Relation: bindingRelPrefix + sc.Role, Object: binding})
	for _, s := range sc.Subjects {
		out = append(out, Tuple{User: s, Relation: bindingSubjectRel, Object: binding})
	}
	out = append(out, Tuple{User: labelObj, Relation: bindingSelectorRel, Object: binding})
	return out
}

// GrantScoped — та же выдача, но на названной области.
//
// Существует ради аккаунтной ветви предиката области, и «проверяется» здесь
// означает УТВЕРЖДЕНИЕ, а не исполнение. У ветви есть пара:
// TestAccountScopedGrantReachesObjectsOfItsProjects требует положительного
// (аккаунтная выдача накрывает объект проекта СВОЕГО аккаунта) и отрицательного
// (не накрывает помеченный объект проекта ЧУЖОГО аккаунта). Прежняя редакция
// этого комментария обещала проверку, которой не было: ветвь исполнялась, но ни
// одно утверждение от неё не зависело — снятие корреляции области не роняло ни
// одной пробы.
func GrantScoped(sc Scenario, scope string) []Tuple { return grantE(sc, bindingObj, scope) }

// InlineIntent — предмет выдачи и сама выдача для операций «в одной транзакции».
//
// Предмет — новый объект (строка зеркала), выдача — привязка на него. У движка
// отношений эта пара неприменима by construction, и ячейка сообщает именно это.
func InlineIntent(sc Scenario, obj string) (data, grant []Tuple) {
	return []Tuple{{User: projectObj, Relation: "project", Object: obj}},
		grantE(sc, inlineBindingObj, projectObj)
}

// InlineRevokeIntent — отзыв одного субъекта встраиваемой привязки, в той же
// транзакции, что и правка предмета выдачи.
func InlineRevokeIntent(sc Scenario, obj string) (data, revoke []Tuple) {
	return []Tuple{{User: projectObj, Relation: "project", Object: obj}},
		[]Tuple{{User: sc.Subjects[0], Relation: bindingSubjectRel, Object: inlineBindingObj}}
}

// CascadeSeed — принципал трёх верхних уровней доступа.
//
// У форм A–BCD это прямой кортеж «администратор аккаунта», который канонический
// граф доводит до листа сам (`v_get` ← `super_admin` ← `super_admin from project`
// ← `admin from account`). У формы E — строка администратора аккаунта, которую
// запрос вердикта достаёт соединением ограниченной глубины по родительским
// указателям. Обе формы отвечают на ОДИН вопрос, поэтому его можно задать обеим.
func CascadeSeed(f Form, _ Scenario) []Tuple {
	if f == FormE {
		return []Tuple{{User: cascadeAdmin, Relation: cascadeAccountAdminRel, Object: accountObj}}
	}
	return []Tuple{{User: cascadeAdmin, Relation: "admin", Object: accountObj}}
}

// RelabelOne returns the tuples written when ONE object ENTERS the selected set —
// the "a label changed on one resource" operation.
//
// This is the axis on which the shapes differ most and the one a flat index pays
// worst: A rewrites the whole verb × subject cross-product for that object, D
// writes one pointer.
func RelabelOne(f Form, sc Scenario, obj string) []Tuple {
	rel := sc.grantRelation()
	switch f {
	case FormA:
		out := make([]Tuple, 0, len(sc.Verbs)*len(sc.Subjects))
		for _, v := range sc.Verbs {
			for _, s := range sc.Subjects {
				out = append(out, Tuple{User: s, Relation: v, Object: obj})
			}
		}
		return out
	case FormB:
		out := make([]Tuple, 0, len(sc.Verbs))
		for _, v := range sc.Verbs {
			out = append(out, Tuple{User: groupObj + "#member", Relation: v, Object: obj})
		}
		return out
	case FormC:
		out := make([]Tuple, 0, len(sc.Subjects))
		for _, s := range sc.Subjects {
			out = append(out, Tuple{User: s, Relation: rel, Object: obj})
		}
		return out
	case FormD, FormBCD, FormE:
		// Форма E делит это намерение с формой D дословно, но платит за него
		// иначе: у D это кортеж-указатель в графе, у E — правка метки строки
		// зеркала. Общий словарь на то и общий.
		return []Tuple{{User: labelObj, Relation: labelsRel, Object: obj}}
	}
	return nil
}

// RelabelMany is RelabelOne applied to K objects — the "mass re-tagging" operation.
func RelabelMany(f Form, sc Scenario, objs []string) []Tuple {
	var out []Tuple
	for _, o := range objs {
		out = append(out, RelabelOne(f, sc, o)...)
	}
	return out
}

// RevokeSubject returns the tuples DELETED when ONE subject loses the binding.
//
// Withdrawal is measured separately from grant on purpose: an additive-only
// materialization path is green on every "was it granted" assertion and wrong on
// exactly the operation revocation exists for (see .claude/rules/testing.md
// §"Параллельный newman", create-vs-update discriminator).
func RevokeSubject(f Form, sc Scenario, subject string) []Tuple {
	rel := sc.grantRelation()
	switch f {
	case FormA:
		out := make([]Tuple, 0, sc.N*len(sc.Verbs))
		for i := 0; i < sc.N; i++ {
			obj := sc.Object(i)
			for _, v := range sc.Verbs {
				out = append(out, Tuple{User: subject, Relation: v, Object: obj})
			}
		}
		return out
	case FormB, FormBCD:
		return []Tuple{{User: subject, Relation: "member", Object: groupObj}}
	case FormC:
		out := make([]Tuple, 0, sc.N)
		for i := 0; i < sc.N; i++ {
			out = append(out, Tuple{User: subject, Relation: rel, Object: sc.Object(i)})
		}
		return out
	case FormD:
		return []Tuple{{User: subject, Relation: rel, Object: labelObj}}
	case FormE:
		return []Tuple{{User: subject, Relation: bindingSubjectRel, Object: bindingObj}}
	}
	return nil
}

// ExpectedGrantTuples is the closed-form count each shape predicts, so the measured
// count can be checked against the arithmetic rather than trusted.
func ExpectedGrantTuples(f Form, sc Scenario) int {
	n, m, s := sc.N, len(sc.Verbs), len(sc.Subjects)
	switch f {
	case FormA:
		return n * m * s
	case FormB:
		return n*m + s
	case FormC:
		return n * s
	case FormD:
		return n + s
	case FormBCD:
		return n + s + 1
	case FormE:
		// s + 2: привязка, её субъекты, её правило-селектор. Строки зеркала сюда
		// не входят — они структурные, как указатели родителя у движка.
		return s + 2
	}
	return -1
}

// ExpectedStatements — объявленная ДО прогона арифметика колонки `StmtSQL` формы
// E, по операциям.
//
// Она нужна не для красоты отчёта: без неё измеряемое проверялось бы только по
// объёму, а объём у формы E постоянен — и форма, которая ВДОБАВОК к выдаче
// переразмечает весь набор, прошла бы все утверждения о строках, потому что
// правка метки строк не добавляет. Дефект найден инъекцией собственной пробы, а
// не рассуждением: инъекция «форма E разворачивает набор» пережила проверку
// объёма и упала только на этой величине.
//
// -1 — «не объявлена»: у форм A–BCD стейтменты порождает движок, и их число —
// свойство его реализации, а не наше объявление. Требовать от них чужую
// арифметику значило бы перенести посылку одной формы на другую.
func ExpectedStatements(f Form, op Op) int {
	if f != FormE {
		return -1
	}
	switch op {
	case OpGrant:
		return 5 // begin + привязка + субъекты + правило-селектор + commit
	case OpRevoke, OpRelabel1, OpRelabelK:
		return 3 // begin + один множественный стейтмент + commit
	case OpInlineGrant:
		return 6 // begin + предмет выдачи + три строки выдачи + commit
	case OpInlineRevoke:
		return 4 // begin + предмет выдачи + снятие субъекта + commit
	case OpCheck, OpPage50, OpPageFull, OpCascade:
		return 1 // вердикт — ОДИН запрос, и на одном объекте, и на целой странице
	case OpVolume:
		return -1 // у объёма нет колонки стейтментов: это не операция замера времени
	}
	return -1
}

// ExpectedStructuralRows — объявленная арифметика структурной части.
//
// Названа отдельно и до прогона, потому что у формы E ЭТО и есть величина,
// обязанная расти с N: цена самой выдачи у неё постоянна (s + 2), и постоянство —
// законный результат, а не отказ прибора. Форма, у которой ни одна величина не
// отвечает на удвоение входа, оставляет предпосылку «прибор различает»
// недоказанной, поэтому величина, которая обязана вырасти, названа заранее и
// сверяется с измеренной.
// Слагаемое 5 (а не 2) — чужой арендатор: второй аккаунт, его проект и один
// помеченный объект в нём. Оно названо ЗДЕСЬ, потому что эта арифметика —
// единственное место, где заявленный размер фикстуры сверяется с измеренным:
// пробы объёма упадут, если фикстура вырастет иначе, чем объявлено.
func ExpectedStructuralRows(f Form, sc Scenario) int {
	if f == FormE {
		// Строка зеркала на каждый объект — и внутри набора, и вне его, и чужого
		// арендатора; плюс два проекта, два аккаунта и роль-с-глаголами.
		return sc.N + sc.Spare + 5 + len(sc.Verbs)
	}
	// У форм A–BCD структурное — указатели родителя: по одному на объект плюс
	// звенья двух цепей.
	return sc.N + sc.Spare + 5
}
