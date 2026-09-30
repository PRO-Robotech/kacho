// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_minting_census_test.go — ГЕЙТ КЛАССА: у КАЖДОГО объявленного стенда своя
// чеканка токенов объявлена ЦЕЛИКОМ либо не объявлена вовсе, и стенд без неё
// назван поимённо с причиной. Половины пары не бывает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Своя чеканка — не одна ручка, а СОГЛАСИЕ ТРЁХ МЕСТ в разных чартах одной
// умбреллы:
//
//	чеканит   kaname.config.authn.tokenSigning      — издатель, подпись, приём
//	принимает api-gateway.tokenAcceptance           — тот же издатель, его набор ключей
//	принимает registry.tokenAcceptance              — тот же издатель, его набор ключей
//
// НИ ОДИН СТРАЖ СТАРТА ЭТО НЕ СВЯЗЫВАЕТ, и связать не может: страж живёт внутри
// процесса, а расходятся здесь ТРИ процесса. Композиционный корень службы прав
// связывает свою половину (чеканка включена ⇒ ключ обёртки обязателен,
// cmd/kaname/signing.go), но о том, принимает ли край нашего издателя, он не
// знает ничем.
//
// ЦЕНА ЭТОГО КЛАССА ИЗМЕРЕНА ДВАЖДЫ, и оба раза — в этом дереве:
//
//	#1014  перечень адресатов выдачи не назвал адресат края. Токен нашей чеканки
//	       отвергался краем ПО АДРЕСАТУ, ни разу не дойдя до проверки подписи:
//	       полоса объявлена, задокументирована, покрыта типами — и не работала ни
//	       при каком входе;
//	#1184  две величины ОДНОГО профиля разошлись именем полосы. Страж старта
//	       отказал, под не дошёл до готовности, стенд — до подъёма.
//
// Оба — «неисполнимая возможность» (`api-conventions.md` §«ДВА ПРАВИЛА ОБ ОДНОМ
// ПОЛЕ»): каждая половина защитима сама по себе, неверна их РАЗНИЦА, и в диффе
// это не видно, потому что половины лежат в разных файлах.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА СЧЁТА — РАЗВЁРТЫВАЕМЫЙ СТЕНД, А НЕ ФАЙЛ ПРОФИЛЯ
//
// Профили накладываются слева направо, и накладка образов посадку НЕ объявляет —
// она наследует её у слоя под собой. Поэтому счёт по файлам и счёт по стендам
// отвечают на РАЗНЫЕ вопросы, и путать их дорого: чеканящий стенд может не
// объявлять чеканку ни в одном СВОЁМ файле — он её наследует, а файл значений
// может не быть посадкой вовсе (умолчания чарта, пример закрепления дайджестов).
//
// Ручка наследуется и глубже цепочки — у УМОЛЧАНИЙ ПОДЧАРТА службы: стенд, не
// назвавший посадку или переключатель, получает значение оттуда, а не пустоту.
// Гейт кладёт умолчания подчарта под цепочку (coalesceSubchartDefaults), иначе
// он был бы слеп к посадке, которую стенд не пишет.
//
// Чисел здесь нет намеренно: они меняются с каждой правкой таблицы состава.
// Действующие — в строке переписи этого гейта («стендов объявлено … · под
// посадкой own … · чеканят своё …»).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО УЖЕ ДЕРЖАТ СОСЕДИ, И ПОЧЕМУ ЭТО НЕ ОДИН ПРЕДМЕТ
//
// Утверждение «держателя не было ни одного» было бы ЛОЖНЫМ, и первая редакция
// этой шапки его несла — предикат автора был слеп: образец `deploy/**/*_test.go`
// требует хотя бы одного уровня каталога и файлы, лежащие прямо в `deploy/`, НЕ
// находит. Соседей два, и оба судят ПРОФИЛЬ:
//
//	client_token_declaration_test.go        профиль, поднявший токен-эндпоинт,
//	                                       обязан объявить чеканку; перепись —
//	                                       «осмотрено профилей 10, из них
//	                                       поднимают эндпоинт 3»;
//	presented_credential_declaration_test.go профиль боевой посадки, поднявший
//	                                       публичный фронт, обязан объявить
//	                                       читателя; перепись — «осмотрено 10,
//	                                       боевая посадка 3».
//
// Их предмет — ОДИН профиль сам по себе, и для него счёт по файлам верен: они
// ловят профиль, который включает возможность и тут же не объявляет того, чем
// она держится. Здешний предмет ДРУГОЙ — действующее состояние СТЕНДА после
// наложения, где половина пары может приехать слоем ниже. Ни один из соседей
// такого стенда не видит, и ни один из них не читает СОГЛАСИЕ между чартами.
//
// Согласие издателя между чеканящим и принимающими не держит НИЧТО: предикат
// `git grep -lE 'platformIssuer' -- 'deploy/*_test.go'` до этого гейта давал 0.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ ГЕЙТ НЕ УТВЕРЖДАЕТ
//
// Он не судит, ПРАВИЛЬНЫЙ ли издатель выбран, и не проверяет, что ключ обёртки
// выдан оператором: первое — решение профиля, второе живёт в секрете кластера, а
// не в дереве. Не проверяет он и полноту величин токен-эндпоинта и читателя —
// это предмет соседей выше, и второе место об одном предмете разошлось бы с
// первым молча. Его предмет — СОГЛАСИЕ объявлений между чартами и полнота
// набора самой чеканки на действующем состоянии стенда.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// mintingKnob — путь ручки под деревом значений умбреллы.
type mintingKnob struct {
	name string
	path []string
}

// mintingRequiredWhenOn — ручки, которые обязаны быть непусты, КАК ТОЛЬКО
// чеканка включена. Перечень тесен намеренно и повторяет то, что требует страж
// старта службы: пустая величина здесь означает НЕ «взять разумное», а «не
// сужаем», и каждая из трёх невидима на положительном пути.
//
// keySetPath и keyLifetime сюда НЕ входят: у обеих встроенное умолчание процесса
// непусто и годно, поэтому требование к профилю было бы требованием без
// предмета. Так же рассуждает и проба боевого профиля отдельного чарта.
var mintingRequiredWhenOn = []mintingKnob{
	{"издатель", []string{"kaname", "config", "authn", "tokenSigning", "issuer"}},
	{"подпись", []string{"kaname", "config", "authn", "tokenSigning", "algorithm"}},
	{"допустимые подписи приёма", []string{"kaname", "config", "authn", "tokenSigning", "allowedAlgorithms"}},
}

// mintingDependents — возможности, которые чеканкой ДЕРЖАТСЯ: подпись
// предъявленного проверяется НАШИМ реестром ключей, а токен-эндпоинт чеканит
// НАШИМ подписантом. Объявить их без чеканки — половина пары.
var mintingDependents = []mintingKnob{
	{"приём предъявленного удостоверения", []string{"kaname", "config", "authn", "presentedCredential", "enabled"}},
	{"токен-эндпоинт платформы", []string{"kaname", "config", "authn", "clientToken", "enabled"}},
}

// mintingPostureOwn — значение ручки посадки, при котором внешнего поставщика у
// стенда НЕТ, и публикатор набора ключей службы держит ровно одну запись — свою.
//
// Сравнивается ДОСЛОВНО, как это делает сама служба (разборщик посадки не
// сворачивает регистр и не срезает пробелы): `Own` и ` own` посадкой `own` не
// являются — процесс отказывает в старте на разборе, раньше публикатора, и это
// предмет гейтов посадки, а не этого.
const mintingPostureOwn = "own"

// mintingPosturePath — ручка посадки службы под деревом значений умбреллы.
var mintingPosturePath = []string{"kaname", "config", "authn", "identityProvider"}

// jwksListenerCallerVerifyingModes — режимы проверки клиента у слушателя набора
// ключей, при которых служба признаёт, что вызывающий авторитета отзыва
// ПРОВЕРЕН. Перечень повторяет предикат службы (JWKSProxyVerifiesCaller), а не
// выбран здесь: с другим набором служба откажет в старте, пока гейт молчит.
var jwksListenerCallerVerifyingModes = map[string]bool{"mutual": true, "optional-mutual": true}

// jwksListenerDefaultMode — то, что подставляет шаблон чарта при незаданном
// режиме (`| default "server-tls-only"`). Односторонний: сертификата он не
// запрашивает.
const jwksListenerDefaultMode = "server-tls-only"

// mintingConsumers — потребители, обязанные принимать НАШЕГО издателя. Ключ
// `platformIssuer` объявлен обоими чартами и означает у обоих одно.
var mintingConsumers = []mintingKnob{
	{"край", []string{"api-gateway", "tokenAcceptance"}},
	{"реестр", []string{"registry", "tokenAcceptance"}},
}

// stacksNotMintingTheirOwnTokens — стенды, своей чеканки НЕ объявляющие, с
// причиной по каждому.
//
// Причина обязана называть, ЧЕЙ это предмет и чем состояние истечёт, а не
// почему до него не дошли руки.
//
// ВЕДОМОСТЬ САМОИСТЕКАЮЩАЯ В ОБЕ СТОРОНЫ: запись про стенд, которого таблица
// состава больше не объявляет, — находка (иначе снятый стенд оставит прощение,
// под которое уедет следующий); запись про стенд, который чеканку ЗАВЁЛ, — тоже
// находка (прощение выдано тому, кого теперь судят).
var stacksNotMintingTheirOwnTokens = map[string]string{
	// Пусто. Под посадкой `own` стенд без чеканки не поднимается вовсе, и запись о
	// нём гейт сам называет находкой: законна запись только о стенде на внешнем
	// поставщике. Сколько стендов на какой посадке — строка переписи, а не этот
	// текст. Запись стояла у первой фазы `dev` с доводом «чеканка включается
	// боевым слоем»; под `own` первая фаза без чеканки не стартовала, и на ней
	// ложился подъём всех стендов конвейера (#2735).
}

// stackMintingVerdict — что стенд объявил о своей чеканке.
type stackMintingVerdict struct {
	on       bool
	own      bool
	issuer   string
	findings []string
}

// chartSwitch — включён ли переключатель ТАК, КАК ЕГО ПРОЧТЁТ ШАБЛОН ЧАРТА
// (`{{- if .enabled }}`), а не так, как хотелось бы автору профиля.
//
// Законных форм записи у переключателя больше одной, и каждая читается здесь
// своим правилом — иначе гейт судил бы не то, что рендерится:
//
//	bool                 — как есть;
//	отсутствие / null    — выключен (null в родительском профиле УДАЛЯЕТ ключ,
//	                       и шаблон видит пустоту);
//	число                — включён, если не ноль;
//	карта / перечень     — включён, если непуст;
//	строка "" — выключен; прочая строка шаблоном ВКЛЮЧАЕТСЯ.
//
// Две строки двусмысленны, и об этом сказано находкой, а не догадкой:
//
//	"false" / "False" / "FALSE" — разборщик профилей отдаёт их строкой ТОЛЬКО
//	    закавыченными (иначе это bool), а закавыченную шаблон включает: человек
//	    читает «выключено», рендер — «включено»;
//	n / no / off (в трёх написаниях регистра, которые YAML 1.1 называет
//	    ложью) — helm разбирает профиль по YAML 1.1, и незакавыченные — это
//	    bool false, а закавыченные — непустая строка, то есть «включено».
//	    Разборщик профилей кавычек не сохраняет, и различить две формы по
//	    разобранному дереву нельзя. Прочие написания (`nO`) ложью YAML 1.1 не
//	    являются и читаются как строка — «включено».
//
// Таблица не выведена, а снята: рендер `{{ if .Values.e }}` helm v4.2.4
// (2026-09-25) на написаниях no, "no", off, "false", yes, n, nO, N, 0, "0",
// null, False дал ровно эти исходы.
func chartSwitch(v any) (on bool, problem string) {
	switch x := v.(type) {
	case nil:
		return false, ""
	case bool:
		return x, ""
	case int:
		return x != 0, ""
	case int64:
		return x != 0, ""
	case uint64:
		return x != 0, ""
	case float64:
		return x != 0, ""
	case map[string]any:
		return len(x) > 0, ""
	case []any:
		return len(x) > 0, ""
	case string:
		switch x {
		case "":
			return false, ""
		case "false", "False", "FALSE":
			return true, fmt.Sprintf("значение %q закавычено, и шаблон его ВКЛЮЧАЕТ — "+
				"читающий профиль видит «выключено», рендер делает «включено»; объяви bool", x)
		}
		switch x {
		case "n", "N", "no", "No", "NO", "off", "Off", "OFF":
			return false, fmt.Sprintf("значение %q двусмысленно: helm читает незакавыченное "+
				"как bool false, а закавыченное — как непустую строку, то есть «включено»; "+
				"объяви bool", x)
		}
		return true, ""
	default:
		return true, ""
	}
}

// coalesceSubchartDefaults — действующие значения подчарта службы на стенде:
// умолчания самого подчарта, поверх которых легли объявления цепочки.
//
// Это ТРЕТЬЯ законная форма объявления посадки и переключателей, и без неё гейт
// слеп: стенд, не назвавший ручку, наследует её у подчарта, а не получает
// пустоту. `null` в цепочке ключ умолчания УДАЛЯЕТ — так же, как у helm.
//
// Исходные карты не меняются: умолчания читаются одни на все стенды.
func coalesceSubchartDefaults(merged map[string]any, subchart string, defaults map[string]any) map[string]any {
	out := make(map[string]any, len(merged)+1)
	for k, v := range merged {
		out[k] = v
	}
	base := mergeValues(map[string]any{}, defaults)
	if declared, ok := merged[subchart].(map[string]any); ok {
		base = mergeValues(base, declared)
	}
	out[subchart] = base
	return out
}

// jwksListenerVerifiesCaller — сверяет ли слушатель набора ключей службы
// клиентский сертификат, КАК ЭТО РЕШИТ ШАБЛОН ЧАРТА И ПРОЦЕСС.
//
// Шаблон включает TLS слушателя при `mtls.enable` И значении `dig "jwksProxy"
// .mtls.httpListeners .mtls` — то есть полистенная ручка, если она ОБЪЯВЛЕНА
// (даже `false`), перекрывает общую, а необъявленная берёт общую. Режим — ручка
// `mtls.jwksProxyClientAuthMode` либо умолчание шаблона.
func jwksListenerVerifiesCaller(tree map[string]any) (verifies bool, why string, problems []string) {
	mtls, _ := lookupAny(tree, "kaname", "mtls").(map[string]any)

	enable, p := chartSwitch(mtls["enable"])
	if p != "" {
		problems = append(problems, "kaname.mtls.enable: "+p)
	}
	listener, declared := mtls["jwksProxy"]
	knob := "kaname.mtls.jwksProxy"
	if !declared {
		listener, knob = mtls["httpListeners"], "kaname.mtls.httpListeners"
	}
	tls, p := chartSwitch(listener)
	if p != "" {
		problems = append(problems, knob+": "+p)
	}
	mode := strings.TrimSpace(asString(mtls["jwksProxyClientAuthMode"]))
	if mode == "" {
		mode = jwksListenerDefaultMode
	}

	switch {
	case !enable:
		return false, "kaname.mtls.enable выключен — у слушателя нет TLS вовсе", problems
	case !tls:
		return false, knob + " выключен — слушатель набора идёт открытым текстом", problems
	case !jwksListenerCallerVerifyingModes[mode]:
		return false, fmt.Sprintf("kaname.mtls.jwksProxyClientAuthMode=%q сертификата "+
			"вызывающего не запрашивает", mode), problems
	}
	return true, "", problems
}

// judgeStackMinting — ТЕЛО гейта, вынесенное отдельно, чтобы инъекция звала то
// же, что исполняется на дереве.
//
// Виды находок РАЗНЫЕ, и общий текст скрыл бы, что именно чинить.
func judgeStackMinting(stack string, merged map[string]any, excused map[string]string) stackMintingVerdict {
	var v stackMintingVerdict

	on, problem := chartSwitch(lookupAny(merged, "kaname", "config", "authn", "tokenSigning", "enabled"))
	if problem != "" {
		v.findings = append(v.findings, fmt.Sprintf(
			"стенд %s: kaname.config.authn.tokenSigning.enabled — %s", stack, problem))
	}
	v.on = on
	v.own = asString(lookupAny(merged, mintingPosturePath...)) == mintingPostureOwn
	v.issuer = strings.TrimSpace(asString(lookupAny(merged,
		"kaname", "config", "authn", "tokenSigning", "issuer")))

	if !on {
		// Возможность, ДЕРЖАЩАЯСЯ чеканкой, включённая без неё, — половина пары:
		// подпись предъявленного проверяется НАШИМ реестром ключей, а токен-эндпоинт
		// чеканит НАШИМ подписантом. Проверка стоит ИМЕННО ЗДЕСЬ, и это не стиль:
		// в ветви «чеканка есть» условие `включено И чеканки нет` ложно by
		// construction — там она была бы мёртвой ветвью, документирующей запрет,
		// который код не производит. Первая редакция этого гейта именно такой её и
		// написала, и обнажила это НЕ чтением, а инъекция: ось «зависимая без
		// чеканки» не дала ни одной находки.
		for _, d := range mintingDependents {
			enabled, problem := chartSwitch(lookupAny(merged, d.path...))
			if problem != "" {
				v.findings = append(v.findings, fmt.Sprintf(
					"стенд %s: %s — %s", stack, d.name, problem))
			}
			if enabled {
				v.findings = append(v.findings, fmt.Sprintf(
					"стенд %s: %s включён без своей чеканки — подпись проверяется НАШИМ "+
						"реестром ключей, а без чеканки реестра нет вовсе", stack, d.name))
			}
		}
		// ПОСАДКА `own` БЕЗ ЧЕКАНКИ ПРОЩЕНИЯ НЕ ИМЕЕТ, и это не строгость гейта, а
		// свойство службы: под `own` зеркала чужого набора нет, своего набора без
		// чеканки нет, у публикатора набора ключей не остаётся ни одной записи — и
		// служба ОТКАЗЫВАЕТ В СТАРТЕ («no key-set records declared»). Ведомость
		// прощает решение, а не стенд, который не поднимается: запись о таком стенде
		// сама — находка. Так был положен подъём всех стендов конвейера (#2735).
		if v.own {
			reason := "и в ведомости его нет"
			if _, named := excused[stack]; named {
				reason = "и запись ведомости этого НЕ прощает — сними её"
			}
			v.findings = append(v.findings, fmt.Sprintf(
				"стенд %s: посадка личности службы — %s, а своя чеканка не включена: зеркала "+
					"чужого набора под этой посадкой нет, своего без чеканки нет, у публикатора "+
					"набора ключей не остаётся ни одной записи, и служба откажет в старте, %s",
				stack, mintingPostureOwn, reason))
		} else if _, named := excused[stack]; !named {
			v.findings = append(v.findings, fmt.Sprintf(
				"стенд %s своей чеканки не объявляет и НЕ НАЗВАН в ведомости: стенд без "+
					"чеканки — это решение, у которого есть автор и предикат снятия, а "+
					"безымянный пропуск неотличим от забытой ручки", stack))
		}
		// Потребитель, объявивший НАШЕГО издателя там, где его никто не чеканит,
		// принимает издателя, которого не существует.
		for _, c := range mintingConsumers {
			if got := consumerPlatformIssuer(merged, c.path); got != "" {
				v.findings = append(v.findings, fmt.Sprintf(
					"стенд %s: %s объявлен принимающим издателя платформы %q, а чеканки "+
						"на этом стенде нет — принимается издатель, которого никто не "+
						"выпускает", stack, c.name, got))
			}
		}
		return v
	}

	if _, named := excused[stack]; named {
		v.findings = append(v.findings, fmt.Sprintf(
			"стенд %s назван в ведомости не-чеканящих и при этом чеканку ОБЪЯВЛЯЕТ: "+
				"прощение выдано тому, кого теперь судят — сними запись", stack))
	}

	// Своя запись набора выставляет на том же слушателе АВТОРИТЕТ ОТЗЫВА, а он
	// требует проверенного вызывающего: слушатель, сертификата не запрашивающий,
	// не даёт ему ни одного проверенного пира, и служба ОТКАЗЫВАЕТ В СТАРТЕ. Это
	// вторая половина той же пары, и без неё включённая чеканка лишь переносит
	// отказ старта на следующую строку.
	verifies, why, problems := jwksListenerVerifiesCaller(merged)
	for _, p := range problems {
		v.findings = append(v.findings, fmt.Sprintf("стенд %s: %s", stack, p))
	}
	if !verifies {
		v.findings = append(v.findings, fmt.Sprintf(
			"стенд %s: чеканка включена, а слушатель набора ключей службы вызывающего не "+
				"сверяет (%s) — авторитет отзыва на нём не получит ни одного проверенного "+
				"пира, и служба откажет в старте", stack, why))
	}

	for _, k := range mintingRequiredWhenOn {
		if strings.TrimSpace(asString(lookupAny(merged, k.path...))) == "" {
			v.findings = append(v.findings, fmt.Sprintf(
				"стенд %s: чеканка включена, а %s не объявлена. Пустая величина здесь "+
					"означает «не сужаем», а не «взять разумное»: незаданный издатель "+
					"означает «любой», пустой перечень подписей — «принимаем любую»",
				stack, k.name))
		}
	}

	if v.issuer == "" {
		return v // об издателе уже сказано выше; согласие потребителей судить не с чем
	}
	for _, c := range mintingConsumers {
		v.findings = append(v.findings, judgeConsumerAgreement(stack, c, merged, v.issuer)...)
	}
	return v
}

// judgeConsumerAgreement — потребитель обязан назвать НАШЕГО издателя тремя
// согласованными способами: как издателя платформы, как члена перечня
// принимаемых и как ключ записи «издатель → набор ключей».
//
// Все три нужны вместе, и это не педантизм: издатель платформы без записи набора
// даёт токен, чью подпись нечем проверить; член перечня без издателя платформы
// принимается, но не опознаётся нашим; запись набора без членства не читается
// никогда.
func judgeConsumerAgreement(stack string, c mintingKnob, merged map[string]any, issuer string) []string {
	var out []string

	node, ok := lookupAny(merged, c.path...).(map[string]any)
	if !ok {
		return []string{fmt.Sprintf(
			"стенд %s: чеканка включена, а %s перечня принимаемых издателей не объявляет "+
				"вовсе — наш токен отвергался бы по издателю, не дойдя до проверки подписи",
			stack, c.name)}
	}

	if got := strings.TrimSpace(asString(node["platformIssuer"])); got != issuer {
		out = append(out, fmt.Sprintf(
			"стенд %s: чеканит %q, а %s принимает как издателя платформы %q. Стороны "+
				"полосы расходятся, и расхождение НЕВИДИМО на положительном пути ни у "+
				"одной из них", stack, issuer, c.name, got))
	}
	if !commaListHas(asString(node["issuers"]), issuer) {
		out = append(out, fmt.Sprintf(
			"стенд %s: издателя %q нет в перечне принимаемых у потребителя «%s» — "+
				"объявленный издатель платформы, не входящий в перечень, не принимается "+
				"ни при каком входе", stack, issuer, c.name))
	}
	if !keySetsName(asString(node["issuerKeySets"]), issuer) {
		out = append(out, fmt.Sprintf(
			"стенд %s: у потребителя «%s» нет записи «издатель → набор ключей» для %q — "+
				"подпись нашего токена проверять нечем", stack, c.name, issuer))
	}
	return out
}

// consumerPlatformIssuer — издатель платформы, объявленный потребителем, либо
// пусто, если потребитель его не называет.
func consumerPlatformIssuer(merged map[string]any, path []string) string {
	node, ok := lookupAny(merged, path...).(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(asString(node["platformIssuer"]))
}

// commaListHas — членство в перечне через запятую. Считаются ЭЛЕМЕНТЫ, а не длина
// строки: одинокая запятая непуста по длине и пуста по существу.
func commaListHas(list, want string) bool {
	for _, e := range strings.Split(list, ",") {
		if strings.TrimSpace(e) == want {
			return true
		}
	}
	return false
}

// keySetsName — назван ли издатель ключом записи `<издатель>=<адрес набора>`,
// перечисляемой через запятую.
func keySetsName(sets, want string) bool {
	for _, e := range strings.Split(sets, ",") {
		k, _, found := strings.Cut(strings.TrimSpace(e), "=")
		if found && strings.TrimSpace(k) == want {
			return true
		}
	}
	return false
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func lookupAny(tree map[string]any, path ...string) any {
	v, ok := lookup(tree, path...)
	if !ok {
		return nil
	}
	return v
}

// judgeRegisterExpiry — ведомость истекает САМА: запись про стенд, которого
// таблица состава больше не объявляет, прощает того, кого никто не судит, и
// следующий стенд с таким именем унаследует прощение молча.
//
// Вынесено отдельно по той же причине, что и тело выше: инъекция обязана звать
// то, что исполняется на дереве.
func judgeRegisterExpiry(excused map[string]string, declared map[string]bool) []string {
	names := make([]string, 0, len(excused))
	for name := range excused {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []string
	for _, name := range names {
		if !declared[name] {
			out = append(out, fmt.Sprintf(
				"ведомость не-чеканящих называет стенд %q, которого таблица состава не "+
					"объявляет: прощению нечего прощать — сними запись", name))
		}
	}
	return out
}

// TestEveryStackEitherMintsItsOwnTokensOrIsNamedWithAReason — перепись в ДВЕ
// колонки по каждому объявленному стенду.
func TestEveryStackEitherMintsItsOwnTokensOrIsNamedWithAReason(t *testing.T) {
	stacks := deployStacks(t)

	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	// Умолчания подчарта службы — третья законная форма объявления: стенд, не
	// назвавший ручку, наследует её отсюда. Читаются ОДИН раз и не меняются.
	defaults := readSubchartDefaults(t, kanameChartValues)

	minting, own, profilesRead, findings := 0, 0, 0, 0
	seen := map[string]bool{}
	for _, name := range names {
		merged, files, _ := mergedValuesOfStack(t, stacks[name])
		profilesRead += files
		seen[name] = true

		effective := coalesceSubchartDefaults(merged, kanameUmbrellaSubtree, defaults)
		v := judgeStackMinting(name, effective, stacksNotMintingTheirOwnTokens)
		if v.on {
			minting++
		}
		if v.own {
			own++
		}
		for _, f := range v.findings {
			findings++
			t.Error(f)
		}
	}

	for _, f := range judgeRegisterExpiry(stacksNotMintingTheirOwnTokens, seen) {
		findings++
		t.Error(f)
	}

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if len(names) == 0 || profilesRead == 0 {
		t.Fatalf("обход пуст: стендов %d, прочитано наложений %d — предпосылка исчезла, "+
			"а не дерево стало чистым", len(names), profilesRead)
	}
	t.Logf("перепись: стендов объявлено %d (%s) · под посадкой %s %d · чеканят своё %d · "+
		"названо в ведомости с причиной %d · потребителей согласия %d · прочитано наложений %d "+
		"(+ умолчания подчарта %s) · находок %d",
		len(names), strings.Join(names, " "), mintingPostureOwn, own, minting,
		len(stacksNotMintingTheirOwnTokens), len(mintingConsumers), profilesRead,
		kanameChartValues, findings)
}

// readSubchartDefaults — умолчания подчарта, разобранные тем же разборщиком,
// что и профили цепочки. Пустой файл — не «умолчаний нет», а исчезнувшая
// предпосылка: без них гейт слеп к унаследованной посадке.
func readSubchartDefaults(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- путь — константа собственного дерева
	if err != nil {
		t.Fatalf("умолчания подчарта %s не читаются: %v — предпосылка исчезла, а не дерево "+
			"стало чистым", path, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("умолчания подчарта %s не разбираются как YAML: %v", path, err)
	}
	if len(tree) == 0 {
		t.Fatalf("умолчания подчарта %s пусты — унаследованную посадку судить не с чем", path)
	}
	return tree
}
