// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trustdomainposture.go — разбор двух сторон домена доверия В ПОСАДКЕ
// (приёмка KAN-WIRE-1, сценарий KAN-W4-04, предмет `ПР-4`).
//
// # Предмет — РАСХОЖДЕНИЕ, а не литерал
//
// Соседний гейт (`trustdomainliteral_test.go`) требует, чтобы домена не было в
// коде. На дереве без литералов он молчит независимо от того, какую величину
// объявляет посадка, — и это правильно: у него другой предмет. Здесь предмет
// второй половины: выпускающая сторона и принимающая обязаны брать домен из
// ОДНОГО объявления.
//
// Стороны две, и обе живут в шаблонах развёртывания:
//
//	uris: spiffe://<домен>/ns/…      ← ВЫПУСКАЮЩАЯ: под этим доменом чеканится сертификат
//	KACHO_..._TRUST_DOMAIN: <домен>  ← ПРИНИМАЮЩАЯ: этот домен процесс признаёт своим
//
// Разошлись — и законный отправитель перестаёт опознаваться: сертификат
// настоящий, выпущен настоящим центром, а принимающий его домена не знает.
// Отказ при этом неотличим от вызова без личности, потому что личность просто не
// извлеклась.
//
// # Что здесь считается ОБЪЯВЛЕНИЕМ, а что употреблением
//
//	{{- define "kaname.trustDomain" -}}…{{ $sp.trustDomain | default "…" }}  ← ОБЪЯВЛЕНИЕ
//	{{ include "kaname.trustDomain" . }}                                     ← употребление
//	# домен по умолчанию — kacho.cloud                                       ← ПРОЗА
//
// Разбор судит ДЕЙСТВИЕ шаблона (`{{ … }}`), а не строку файла: правило по
// подстроке краснело бы на комментарии, объясняющем это же правило.
//
// # Имена ручек НЕ выписаны — они выведены из кода
//
// Принимающую сторону называет сам процесс: `servicecontract.Spec.TrustDomainKnob`
// в его композиционном корне. Второй перечень имён здесь разошёлся бы с первым
// молча — и разошёлся бы ровно там, где расхождение не видно: у службы, чью
// ручку переименовали, а чарт остался со старой.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **величину, приехавшую из чужого чарта** через глобальные значения: она
//     не пишется в шаблоне и опознаётся только рендером;
//  2. **имя ручки, собранное из частей** (`printf "%s_TRUST_DOMAIN" $prefix`) —
//     разбор судит действие по тексту, а не по его исходу. Такой формы в дереве
//     ноль;
//  3. **верность самого домена**: гейт судит СОГЛАСИЕ двух сторон, а не то,
//     какой домен установка выбрала. Это вопрос к установке.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// TrustDomainDeclSite — координата ОБЪЯВЛЕНИЯ домена в шаблоне посадки.
type TrustDomainDeclSite struct {
	File string
	Line int
	// Chart — чарт, которому принадлежит объявление.
	Chart string
	// Helper — имя именованного шаблона, если объявление внутри него; пусто,
	// когда домен объявлен прямо в месте употребления (это и есть находка).
	Helper string
	// Default — величина умолчания, объявленная этим местом.
	Default string
}

// TrustDomainUseSite — координата УПОТРЕБЛЕНИЯ домена: выпускающая либо
// принимающая сторона.
type TrustDomainUseSite struct {
	File string
	Line int
	// Chart — чарт, которому принадлежит место.
	Chart string
	// Side — `issuing` (чеканка сертификата) либо `accepting` (ручка процесса).
	Side string
	// FromHelper — величина взята из именованного шаблона чарта.
	FromHelper bool
	// Knob — имя ручки для стороны `accepting`.
	Knob string
}

// TrustDomainPostureCensus — объём осмотренного.
type TrustDomainPostureCensus struct {
	Files   int
	Actions int
	// Knobs — сколько имён ручек выведено из кода. Ноль означает, что связывать
	// стороны нечем, и молчание гейта сказано ни о чём.
	Knobs int
}

var (
	// trustDomainDeclRe — объявление умолчания: `trustDomain | default "X"`.
	trustDomainDeclRe = regexp.MustCompile(`trustDomain\s*\|\s*default\s+"([^"]*)"`)
	// trustDomainHelperRe — определение именованного шаблона домена.
	trustDomainHelperRe = regexp.MustCompile(`define\s+"([A-Za-z0-9_.\-]*\.trustDomain)"`)
	// trustDomainIncludeRe — употребление именованного шаблона домена.
	trustDomainIncludeRe = regexp.MustCompile(`include\s+"([A-Za-z0-9_.\-]*\.trustDomain)"`)
	// trustDomainIssueRe — чеканка идентификатора: схема в действии шаблона.
	trustDomainIssueRe = regexp.MustCompile(`spiffe://`)
	// helmActionRe — действие шаблона. Всё вне действий — проза и данные.
	helmActionRe = regexp.MustCompile(`\{\{-?(.*?)-?\}\}`)
	// envNameRe — начало записи перечня переменных окружения контейнера.
	envNameRe = regexp.MustCompile(`^-?\s*name:\s*`)
)

// TrustDomainKnobSite — ручка принимающей стороны, объявленная КОДОМ, вместе с
// её координатой. Координата нужна отказу: находка «ручку никто не производит»
// обязана называть обе стороны, а вторая сторона — вот эта строка.
type TrustDomainKnobSite struct {
	File    string
	Line    int
	Service string
	Knob    string
}

// ScanTrustDomainKnobs выводит ручки принимающей стороны ИЗ КОДА: значения
// `TrustDomainKnob` в композитных литералах дескриптора посадки.
//
// Служба берётся из поля `Service` того же литерала: без неё отказ не назовёт,
// чей это процесс, а связывание по порядку полей — догадка.
func ScanTrustDomainKnobs(path string, src []byte) ([]TrustDomainKnobSite, error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return nil, perr
	}
	var out []TrustDomainKnobSite
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		service, knob := "", ""
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			val, ok := kv.Value.(*ast.BasicLit)
			if !ok || val.Kind != token.STRING {
				continue
			}
			s, uerr := strconv.Unquote(val.Value)
			if uerr != nil {
				continue
			}
			switch key.Name {
			case "Service":
				service = s
			case "TrustDomainKnob":
				knob = s
			}
		}
		if service != "" && knob != "" {
			out = append(out, TrustDomainKnobSite{
				File: path, Line: fset.Position(lit.Pos()).Line, Service: service, Knob: knob,
			})
		}
		return true
	})
	return out, nil
}

// ScanTrustDomainPosture разбирает один шаблон посадки.
//
// `chart` — имя чарта, которому файл принадлежит; `knobTokens` — имена ручек,
// выведенные из кода (по ним опознаётся принимающая сторона).
func ScanTrustDomainPosture(path, chart string, src []byte, knobTokens []string) (
	decls []TrustDomainDeclSite, uses []TrustDomainUseSite, census TrustDomainPostureCensus,
) {
	census.Files = 1
	lines := strings.Split(string(src), "\n")
	// Взятие домена из объявления — свойство ФАЙЛА, а не позиции строки: идиома
	// шаблона присваивает его один раз наверху и употребляет ниже, а перечень
	// переменных окружения называет имя строкой РАНЬШЕ, чем действие подставляет
	// значение. Разбор, судящий по порядку строк, краснел бы на обеих верных
	// записях.
	fileTakesFromHelper := trustDomainIncludeRe.MatchString(string(src))
	helper := ""
	for i, line := range lines {
		lineNo := i + 1
		for _, m := range helmActionRe.FindAllStringSubmatch(line, -1) {
			action := m[1]
			census.Actions++

			if hm := trustDomainHelperRe.FindStringSubmatch(action); hm != nil {
				helper = hm[1]
			}
			if dm := trustDomainDeclRe.FindStringSubmatch(action); dm != nil {
				decls = append(decls, TrustDomainDeclSite{
					File: path, Line: lineNo, Chart: chart, Helper: helper, Default: dm[1],
				})
			}
			fromHelper := fileTakesFromHelper
			if trustDomainIssueRe.MatchString(action) {
				uses = append(uses, TrustDomainUseSite{
					File: path, Line: lineNo, Chart: chart, Side: "issuing", FromHelper: fromHelper,
				})
			}
		}
		// Принимающая сторона опознаётся ИМЕНЕМ РУЧКИ, а имя стоит вне действия:
		// это ключ значения, которое действие заполняет. Поэтому строка судится
		// целиком, но только на присутствие имени, выведенного из кода.
		for _, tok := range knobTokens {
			if !trustDomainKnobOnLine(tok, line) {
				continue
			}
			uses = append(uses, TrustDomainUseSite{
				File: path, Line: lineNo, Chart: chart, Side: "accepting",
				FromHelper: fileTakesFromHelper, Knob: tok,
			})
		}
	}
	return decls, uses, census
}

// TrustDomainKnobTokens — имена, по которым принимающая сторона опознаётся в
// шаблоне: из текста ручки выбираются те слова, которые в шаблон и попадают
// (имя переменной окружения и ключ файла настроек).
//
// Текст ручки пишет автор корня для ОПЕРАТОРА («authn.trust-domain (env
// KANAME_AUTHN__TRUST_DOMAIN)»), поэтому слова из него выбираются, а не берётся
// он целиком.
func TrustDomainKnobTokens(knob string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(knob, func(r rune) bool {
		return r == ' ' || r == '(' || r == ')' || r == ','
	}) {
		if strings.Contains(f, "TRUST_DOMAIN") || strings.Contains(f, "trust-domain") {
			out = append(out, f)
		}
	}
	return out
}

// TrustDomainDeclDisagreement — расхождение умолчаний между объявлениями.
// Возвращает пустую строку, когда все объявления согласны.
func TrustDomainDeclDisagreement(decls []TrustDomainDeclSite) string {
	if len(decls) < 2 {
		return ""
	}
	first := decls[0]
	for _, d := range decls[1:] {
		if d.Default != first.Default {
			return fmt.Sprintf("%s:%d объявляет %q, а %s:%d — %q",
				first.File, first.Line, first.Default, d.File, d.Line, d.Default)
		}
	}
	return ""
}

// trustDomainKnobOnLine — названа ли строкой принимающая ручка, и названа ли
// она ТАМ, ГДЕ ОНА ДЕЙСТВУЕТ.
//
// # Форм ТРИ, и ни одна не выводится из другой
//
//   - name: KACHO_GEO_AUTHZ_TRUST_DOMAIN   ← ИМЯ переменной в перечне окружения
//     KANAME_AUTHN__TRUST_DOMAIN: "…"        ← ключ карты переменных профиля
//     trust-domain: "…"                    ← ключ файла настроек (сегмент имени)
//
// Третья формы не имеет общего с первыми: ключ `authn.trust-domain` живёт в
// документе как `trust-domain:` внутри раздела `authn:`. Распознаватель, знающий
// только переменную окружения, к службе с файлом настроек слеп — и слеп МОЛЧА:
// он не даёт ни красного, ни зелёного.
//
// # Почему судится ПОЗИЦИЯ, а не подстрока
//
// Имя ручки встречается и в прозе: в комментарии профиля, в шапке самого
// объявления, в тексте отказа. Разбор по подстроке краснел бы на собственном
// объяснении — и покраснел на нём при первой же попытке (шапка `kaname.trustDomain`
// называет ручку, которую объявляет). Поэтому имя обязано стоять КЛЮЧОМ или
// ЗНАЧЕНИЕМ ключа `name`, а не где угодно в строке.
func trustDomainKnobOnLine(knob, line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		// Комментарий документа — проза, а не действующее объявление.
		return false
	}
	// (1) `- name: <ИМЯ>` — перечень переменных окружения контейнера.
	if envNameRe.MatchString(trimmed) {
		value := strings.Trim(strings.TrimSpace(envNameRe.ReplaceAllString(trimmed, "")), `"'`)
		if value == knob {
			return true
		}
	}
	// (2) `<ИМЯ>:` — ключ карты переменных профиля.
	if strings.HasPrefix(strings.TrimLeft(trimmed, "- "), knob+":") {
		return true
	}
	// (3) `<последний сегмент>:` — ключ файла настроек.
	if i := strings.LastIndexByte(knob, '.'); i >= 0 {
		if strings.HasPrefix(strings.TrimLeft(trimmed, "- "), knob[i+1:]+":") {
			return true
		}
	}
	return false
}
