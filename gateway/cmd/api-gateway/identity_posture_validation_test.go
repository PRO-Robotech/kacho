// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_posture_validation_test.go — НЕЗАДАННАЯ ПОСАДКА ЛИЧНОСТИ ОТВЕРГАЕТ
// СТАРТ КРАЯ.
//
// # Что здесь было и почему вернулось
//
// Отказ старта при незаданной посадке судился внутри стража полосы отзыва
// (`validateProductionRevocationConfig`) и был снят вместе с осью чужого
// поставщика — снят вместе с пробой, его запиравшей. Обоснование «случаи под
// одну посадку сняты вместе с предметом» к нему не относится: предмет того
// случая — не полосное требование, а САМА ОБЪЯВЛЕННОСТЬ ручки, и ручка жива.
// Она разводит три места провязки корня: полосу личности, ответ «кто я» и
// ретрансляцию глаголов формы.
//
// # Что наблюдалось без него
//
// Оператор ставит боевой стенд, не объявив посадку. Край СТАРТУЕТ молча:
// разрешение возвращает «не задано» без ошибки, а незаданное — не `own`, и
// потому браузерной полосы личности не заводится НИ ОДНОЙ. Ответ «кто я» всегда
// пуст, глаголы формы не смонтированы и уходят в отказ по отсутствию, запрос без
// предъявителя отвергается — войти нельзя ни при каком вводе. При этом чужой
// поставщик на боевом профиле развёрнут и выдаёт сессии: человек входит У НЕГО,
// край его не признаёт, а выход из края ту сессию не оканчивает.
//
// # Почему отказ БЕЗУСЛОВЕН
//
// Соседний страж полосы отзыва терпит послабление под явными метками
// разработки: на местном стенде может не быть достижимого авторитета. Здесь
// послабления нет и быть не может — «на стенде разработки посадку не выбираем»
// означает ровно то состояние, ради снятия которого страж и возвращается: три
// места провязки разводятся значением, которого никто не называл. Образец —
// безусловный отказ при необъявленном перечне издателей
// (`gateway/internal/config/f1d_issuer_address_is_never_derived_test.go`).
//
// # Почему это ПАРИТЕТ, а не ужесточение
//
// Вторая половина того же стенда — служба прав — незаданную посадку отвергает
// с самого заведения поля (`TestF4d01_UnsetIdentityProviderRefusesTheStart` в
// её дереве). Край эту половину потерял, и стенд стал таким, где одна половина
// отказывается стартовать без ответа, а вторая стартует молча.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// Незаданная посадка отвергает старт, и отказ называет РУЧКУ КРАЯ.
func TestUnsetIdentityPostureRefusesTheStart(t *testing.T) {
	err := validateIdentityPosture(identityposture.Unset)
	if err == nil {
		t.Fatal("незаданная посадка ПРИНЯТА: край поднимется, браузерной полосы личности " +
			"не заведёт ни одной, ответит пустым на «кто я» и отвергнет каждый запрос без " +
			"предъявителя — при этом БЕЗ отказа старта, то есть для оператора выкат зелёный")
	}
	if !strings.Contains(err.Error(), config.IdentityProviderKnob) {
		t.Fatalf("отказ обязан называть ручку, которую оператору править: %q", err.Error())
	}
}

// Отказ не зависит от метки окружения: послабление здесь означало бы
// «на стенде разработки посадку не выбираем».
func TestUnsetIdentityPostureRefusesUnderEveryEnvLabel(t *testing.T) {
	for _, env := range []string{"", "production", "production-strict", "dev", "local", "test"} {
		cfg := config.Config{AppEnv: env}
		lane, err := cfg.ResolvedIdentityProvider()
		if err != nil {
			t.Fatalf("KACHO_APP_ENV=%q: разбор незаданной посадки отказал сам (%v) — "+
				"случай проверяет не то", env, err)
		}
		if vErr := validateIdentityPosture(lane); vErr == nil {
			t.Errorf("KACHO_APP_ENV=%q: незаданная посадка принята — послабление по метке "+
				"означает «на стенде разработки посадку не выбираем»", env)
		}
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ: посадка, под которую у корня ЕСТЬ провязка, старт проходит.
//
// # Почему случай ПЕРЕУТВЕРЖДЁН, а не снят
//
// Он утверждал: «принимается КАЖДОЕ значение словаря фундамента». Это свойство
// исчезло вместе с законностью `external` у края, и исходов у такого случая
// три — снять вместе с предметом · перевести на производимый деревом признак ·
// переутвердить новое свойство того же предмета. Снять нельзя: без
// положительной половины «край требует объявления» неотличимо от «край не
// поднимается никогда», и отрицательный случай выше остался бы без близнеца.
//
// Поэтому взято ТРЕТЬЕ, усиленное ВТОРЫМ: предмет тот же — «объявленная
// посадка старт проходит», — но множество объявленных берётся не из словаря
// фундамента, а из того, что ИСПОЛНЯЕТ этот процесс
// (`wiredIdentityPostures`), и совпадение этого множества с провязками корня
// производится ДЕРЕВОМ — `TestGuardAcceptsExactlyWhatTheCompositionRootWires`.
// Против отрицательного кейса по-прежнему меняется ровно один факт.
func TestADeclaredIdentityPostureStillBoots(t *testing.T) {
	wired := wiredIdentityPostures()
	if len(wired) == 0 {
		t.Fatal("процесс не исполняет НИ ОДНОЙ посадки — близнеца не на чем построить, " +
			"и отказ стража был бы отказом всему подряд")
	}
	for _, lane := range wired {
		if err := validateIdentityPosture(lane); err != nil {
			t.Errorf("посадка %s, под которую у корня есть провязка, отвергнута: %v", lane, err)
		}
	}
	t.Logf("перепись: значений словаря фундамента %d · исполняется этим процессом %d — принято все",
		len(identityposture.Values()), len(wired))
}

// ОБЪЯВЛЕННАЯ, НО НЕИСПОЛНИМАЯ ПОСАДКА ОТВЕРГАЕТ СТАРТ.
//
// # Что наблюдалось, пока страж её принимал
//
// `external` не встречался в прод-коде края НИ РАЗУ: корень ветвился по посадке
// только сравнением с `own`, то есть под `external` не провязывались ни
// читатель носителя, ни ответ «кто я», ни ретрансляция глаголов формы. Страж
// при этом принимал значение как законное, и стенд на нём поднимался ГОТОВЫМ,
// не заводя браузерного входа ни одного, — тот же наблюдаемый исход, ради
// снятия которого страж и заведён, только достигнутый ОБЪЯВЛЕННЫМ значением
// вместо унаследованного. В дереве на этом не краснело ничто.
//
// # Отказов ДВА, а не один
//
// «Не объявлено» и «объявлено то, чего этот процесс не исполняет» — разные
// состояния с разным ремонтом: первое чинится объявлением, второе — выбором
// другого значения либо провязкой. Слитые в один отказ, они предлагали бы
// оператору чинить не то.
//
// # Словарь ФУНДАМЕНТА не тронут
//
// Сужается то, что принимает КРАЙ. Значение остаётся законным для тех, кто его
// исполняет: посадку читают два процесса, и служба прав `external` исполняет.
// Правка фундамента здесь не нужна и не делается.
func TestUnwiredIdentityPostureRefusesTheStart(t *testing.T) {
	wired := map[identityposture.Provider]bool{}
	for _, w := range wiredIdentityPostures() {
		wired[w] = true
	}

	var judged int
	for _, lane := range identityposture.Values() {
		if wired[lane] {
			continue
		}
		judged++
		err := validateIdentityPosture(lane)
		if err == nil {
			t.Errorf("посадка %s ПРИНЯТА стражем, а провязки под неё у корня нет ни одной: "+
				"край поднимется готовым, браузерного входа не заведёт ни одного, ответит "+
				"пустым на «кто я» и отвергнет каждый запрос без предъявителя — и ни одного "+
				"отказа старта при этом не произнесёт", lane)
			continue
		}
		// Отказ обязан назвать ПРИЧИНУ и РЕМОНТ, как у соседних стражей посадки.
		for _, want := range []string{
			config.IdentityProviderKnob, lane.String(), "refuse to start",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("отказ по посадке %s не называет %q — оператор не поднимет стенд "+
					"по отказу, который не говорит что чинить: %v", lane, want, err)
			}
		}
		for _, w := range wiredIdentityPostures() {
			if !strings.Contains(err.Error(), w.String()) {
				t.Errorf("отказ по посадке %s не называет исполнимого значения %q — "+
					"причина названа, ремонт нет: %v", lane, w, err)
			}
		}
	}

	// ПРЕДПОСЫЛКА: словарь фундамента шире исполняемого этим процессом. Сойдись
	// они — случай судил бы пустоту и молчал бы о вернувшемся дефекте.
	if judged == 0 {
		t.Fatalf("в словаре фундамента (%d значений) нет ни одного, которого край не "+
			"исполняет, — предмет случая исчез, и его молчание сказано ни о чём",
			len(identityposture.Values()))
	}
	t.Logf("перепись: значений словаря фундамента %d · неисполнимых краем %d — отвергнуто все",
		len(identityposture.Values()), judged)
}

// ПРОВЯЗКА: страж обязан быть ПОЗВАН композиционным корнем.
//
// Страж, объявленный и не позванный, — контроль без механизма исполниться:
// пробы выше были бы зелёными, а край поднимался бы ровно так же, как до их
// написания. main() из пробы не исполним (он занимает порты и дозванивается до
// бэкендов), поэтому провязка утверждается ТАМ, ГДЕ ОНА ЖИВЁТ — в исходнике
// корня, тем же приёмом, что у соседних хопов (`hop_client_wiring_test.go`).
func TestCompositionRoot_RefusesToStartOnAnUndeclaredIdentityPosture(t *testing.T) {
	src := compositionRoot(t)
	if !regexp.MustCompile(`if \w+ := validateIdentityPosture\(identityLane\); \w+ != nil \{` +
		`\s*\n\s*log\.Fatalf`).MatchString(src) {
		t.Fatal("композиционный корень не зовёт стража посадки либо не падает на его отказе — " +
			"страж, которого никто не зовёт, зеленит свои пробы и ничего не меняет в старте")
	}
}

// ПОРЯДОК: страж посадки стоит ДО провязки, которую посадка разводит.
//
// Отказ, произнесённый после провязки, — это отказ, произнесённый после того,
// как решения уже приняты по неназванному значению.
func TestCompositionRoot_JudgesThePostureBeforeItWiresByIt(t *testing.T) {
	src := compositionRoot(t)
	guard := strings.Index(src, "validateIdentityPosture(identityLane)")
	if guard < 0 {
		t.Fatal("страж посадки в корне не найден — порядок проверять не на чем")
	}
	wiring := strings.Index(src, "if identityLane == identityposture.Own")
	if wiring < 0 {
		t.Fatal("провязка по посадке в корне не найдена — предпосылка случая исчезла, " +
			"а это НЕ то же самое, что «порядок верен»")
	}
	if guard > wiring {
		t.Errorf("страж посадки (смещение %d) стоит ПОСЛЕ первой провязки по ней (%d)",
			guard, wiring)
	}
}

// ─── МНОЖЕСТВО ПРИНИМАЕМОГО ПРОИЗВОДИТСЯ ДЕРЕВОМ ───────────────────────────
//
// Страж, чьё множество выписано рядом с ним, расходится с корнем молча: корень
// начинает ветвиться по новому значению — страж его не принимает и стенд не
// поднимается; корень перестаёт ветвиться по старому — страж принимает
// неисполнимое, и возвращается ровно тот дефект, ради которого он сужен.
//
// Поэтому множество СВЕРЯЕТСЯ с деревом: разбором достижимого от `main()` кода
// берутся все посадки, ПО КОТОРЫМ КОРЕНЬ ВЕТВИТСЯ, и они обязаны совпасть.

// postureSelectorsComparedInRoot — посадки, с которыми достижимый код КОРНЯ
// сравнивает значение.
//
// Законных форм записи такого сравнения ДВЕ, и обе в наблюдении: двоичное
// сравнение (`lane == identityposture.Own`, `provider != identityposture.Own`)
// и ветвь выбора (`case identityposture.Own:`). Форма вне наблюдения дала бы
// не красное и не зелёное, а молчание.
func postureSelectorsComparedInRoot(t *testing.T, src string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, compositionRootLabel, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("достижимый код корня не разбирается: %v", err)
	}
	out := map[string]bool{}
	note := func(e ast.Expr) {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			return
		}
		pkg, isIdent := sel.X.(*ast.Ident)
		if !isIdent || pkg.Name != "identityposture" || sel.Sel == nil {
			return
		}
		out[sel.Sel.Name] = true
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BinaryExpr:
			if v.Op == token.EQL || v.Op == token.NEQ {
				note(v.X)
				note(v.Y)
			}
		case *ast.CaseClause:
			for _, e := range v.List {
				note(e)
			}
		}
		return true
	})
	// `Unset` — не провязка, а признак «ответа нет»: по нему ветвится сам
	// страж, и считать его исполняемой посадкой значило бы требовать провязки
	// под отсутствие ответа.
	delete(out, "Unset")
	return out
}

// TestGuardAcceptsExactlyWhatTheCompositionRootWires — множество стража равно
// множеству провязок корня.
func TestGuardAcceptsExactlyWhatTheCompositionRootWires(t *testing.T) {
	inRoot := postureSelectorsComparedInRoot(t, compositionRoot(t))
	accepted := map[string]bool{}
	for _, p := range wiredIdentityPostures() {
		// Имя КОНСТАНТЫ фундамента, а не её печать: корень пишет
		// `identityposture.Own`, страж держит значение, и сверяются они по
		// одному словарю — тому, что объявлен рядом.
		accepted[postureConstName(t, p)] = true
	}

	if len(inRoot) == 0 {
		t.Fatal("в достижимом коде корня нет НИ ОДНОГО сравнения с посадкой — предпосылка " +
			"исчезла: сверять множество стража не с чем, и его молчание сказано ни о чём")
	}
	t.Logf("перепись: посадок, по которым ветвится достижимый корень — %v · принимает страж — %v",
		sortedKeys(inRoot), sortedKeys(accepted))

	for name := range inRoot {
		if !accepted[name] {
			t.Errorf("корень ветвится по посадке %s, а страж её НЕ принимает: стенд, "+
				"объявивший это значение, не поднимется, хотя провязка под него построена",
				name)
		}
	}
	for name := range accepted {
		if !inRoot[name] {
			t.Errorf("страж принимает посадку %s, а корень по ней не ветвится НИ РАЗУ: "+
				"край поднимется готовым и не заведёт под неё ничего — ровно тот дефект, "+
				"ради снятия которого множество сужено", name)
		}
	}
}

// postureConstName — имя константы фундамента по её значению, выведенное из
// самого словаря: выписанное соответствие разошлось бы с ним молча.
func postureConstName(t *testing.T, p identityposture.Provider) string {
	t.Helper()
	switch p {
	case identityposture.Own:
		return "Own"
	case identityposture.External:
		return "External"
	case identityposture.Unset:
		return "Unset"
	}
	t.Fatalf("значение посадки %v словарю этого случая неизвестно — словарь фундамента "+
		"пополнился, а сверка о нём не знает", p)
	return ""
}

// ─── ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ, НА СИНТЕТИКЕ ──────────────────────────────────

// TestPostureSelectorRecognizerKnowsBothLawfulForms — распознаватель знает обе
// законные формы и молчит там, где сравнения нет.
func TestPostureSelectorRecognizerKnowsBothLawfulForms(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"двоичное сравнение", `package main
func main() {
	if lane == identityposture.Own {
		wire()
	}
}`, []string{"Own"}},
		{"отрицание", `package main
func main() {
	if lane != identityposture.Own {
		return
	}
}`, []string{"Own"}},
		{"ветвь выбора", `package main
func main() {
	switch lane {
	case identityposture.Own:
		wire()
	case identityposture.External:
		wireForeign()
	}
}`, []string{"External", "Own"}},
		{"признак «ответа нет» провязкой не считается", `package main
func main() {
	if lane == identityposture.Unset {
		refuse()
	}
}`, nil},
		{"упоминание без сравнения", `package main
func main() {
	log(identityposture.External)
}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sortedKeys(postureSelectorsComparedInRoot(t, c.src))
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("распознано %v, ожидалось %v", got, c.want)
			}
		})
	}
}
