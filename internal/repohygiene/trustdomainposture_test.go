// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trustdomainposture_test.go — выпускающая и принимающая стороны домена доверия
// берут его из ОДНОГО объявления (приёмка KAN-WIRE-1, сценарий KAN-W4-04,
// предмет `ПР-4`).
//
// Разбор, формы записи и границы распознавателя — в шапке
// `trustdomainposture.go`; здесь не пересказываются.
//
// # Почему это ОТДЕЛЬНЫЙ гейт, а не следствие переписи литералов
//
// Перепись литералов требует нуля в коде и на дереве без литералов молчит
// независимо от того, какую величину объявляет посадка. Расхождение двух сторон
// она не видит by construction — у неё другой предмет.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// trustDomainDeployRoots — деревья шаблонов посадки.
//
// Их ДВА уровня, и второй не выводится из первого: зонтичный чарт держит
// вендоренные копии части компонентов (`deploy/helm/umbrella/charts/`), а
// исходные чарты живут рядом со своим кодом (`<компонент>/deploy/`). Обход,
// знающий только первый, слеп к шести чартам из девяти — и слеп МОЛЧА: он не
// даёт ни красного, ни зелёного. Померено при заведении: первая редакция читала
// только `deploy/helm/` и объявляла дерево чистым, пока пять сертификатов
// чеканились под доменом, взятым своей рукой.
var trustDomainDeployRoots = []string{"deploy/helm/", "gateway/deploy/", "ui-future/deploy/"}

// trustDomainDeployRootOf — принадлежит ли файл дереву шаблонов посадки. Чарты
// служб живут по образцу `services/<служба>/deploy/`, и перечислять их поимённо
// значило бы завести перечень, который разойдётся с деревом молча.
func trustDomainDeployRootOf(rel string) bool {
	for _, r := range trustDomainDeployRoots {
		if strings.HasPrefix(rel, r) {
			return true
		}
	}
	return strings.HasPrefix(rel, "services/") && strings.Contains(rel, "/deploy/")
}

// TestAcceptedTrustDomainMatchesIssued — сам гейт.
func TestAcceptedTrustDomainMatchesIssued(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// ── имена ручек выводятся из КОДА, а не выписываются ───────────────────────
	var knobSites []TrustDomainKnobSite
	var goFiles int
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || skipPath(rel) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		found, perr := ScanTrustDomainKnobs(rel, src)
		if perr != nil {
			t.Fatalf("разбор %s: %v", rel, perr)
		}
		goFiles++
		knobSites = append(knobSites, found...)
	}

	var tokens []string
	for _, ks := range knobSites {
		tokens = append(tokens, TrustDomainKnobTokens(ks.Knob)...)
	}
	sort.Strings(tokens)

	// ── шаблоны посадки ───────────────────────────────────────────────────────
	var rels []string
	for rel := range tt.files {
		if !trustDomainDeployRootOf(rel) {
			continue
		}
		if !strings.HasSuffix(rel, ".yaml") && !strings.HasSuffix(rel, ".yml") && !strings.HasSuffix(rel, ".tpl") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		census   TrustDomainPostureCensus
		decls    []TrustDomainDeclSite
		uses     []TrustDomainUseSite
		findings []string
		issuing  int
		produced = map[string]bool{}
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		d, u, c := ScanTrustDomainPosture(rel, trustDomainChartOf(rel), src, tokens)
		census.Files += c.Files
		census.Actions += c.Actions
		decls = append(decls, d...)
		uses = append(uses, u...)
	}
	census.Knobs = len(knobSites)

	for _, u := range uses {
		switch u.Side {
		case "issuing":
			issuing++
		case "accepting":
			// Ключ файла настроек опознаётся ПОСЛЕДНИМ СЕГМЕНТОМ имени, и сегмент
			// у разных служб один и тот же (`authn.trust-domain` и
			// `authz.trust-domain` кончаются одинаково). Поэтому производство
			// ключом засчитывается только В ДЕРЕВЕ ВЛАДЕЛЬЦА: иначе чарт одной
			// службы объявлял бы переведённой другую — и объявлял бы молча.
			produced[u.Knob+"@"+trustDomainComponentRootOf(u.File)] = true
			produced[u.Knob] = true
		}
		if !u.FromHelper {
			findings = append(findings, fmt.Sprintf(
				"%s:%d  сторона=%s  чарт=%s — домен взят НЕ из объявления чарта "+
					"(нет include \"%s.trustDomain\")", u.File, u.Line, u.Side, u.Chart, u.Chart))
		}
	}
	for _, d := range decls {
		if d.Helper == "" {
			findings = append(findings, fmt.Sprintf(
				"%s:%d  чарт=%s — умолчание %q объявлено ВНЕ именованного шаблона: у домена "+
					"стало два адреса, и расходиться они будут молча", d.File, d.Line, d.Chart, d.Default))
		}
	}
	if mismatch := TrustDomainDeclDisagreement(decls); mismatch != "" {
		findings = append(findings, "умолчания домена РАСХОДЯТСЯ: "+mismatch)
	}
	// ПОЛОВИНА ПЕРЕХОДА и есть предмет: ручка, объявленная кодом, обязана иметь
	// производителя в посадке. Иначе процесс её никогда не получит — домена у него
	// не будет, своим он не признает никого, и отказ будет неотличим от вызова без
	// личности. Отказ называет ОБЕ стороны: ручку с координатой её объявления и
	// то, что ни один шаблон её не производит.
	//
	// Связывание идёт по ИМЕНИ РУЧКИ, а не по имени чарта: имена чартов и имена
	// служб в этом дереве не совпадают (`vpc` против `kacho-vpc`), и связывание по
	// ним промахивалось бы молча — то есть давало бы зелёное на непереведённом.
	// Судится РУЧКА, а не имя: у одной ручки форм имени бывает две (переменная
	// окружения и ключ файла настроек), и произведённая любой из них ручка
	// произведена. Счёт по именам объявил бы находкой службу, которая величину
	// получает, — то есть краснел бы на верной посадке.
	for _, ks := range knobSites {
		names := TrustDomainKnobTokens(ks.Knob)
		owner := trustDomainComponentRootOf(ks.File)
		ok := false
		for _, n := range names {
			if strings.Contains(n, ".") {
				// Ключ файла настроек — только в дереве владельца.
				if produced[n+"@"+owner] {
					ok = true
				}
				continue
			}
			// Имя переменной окружения однозначно by construction: оно несёт
			// приставку своей службы, и спутать его не с чем.
			if produced[n] {
				ok = true
			}
		}
		if ok {
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"ручку %q (имена: %v) не производит НИ ОДИН шаблон посадки; объявлена кодом: %s:%d "+
				"(служба %s). Выпускающая сторона переведена, принимающая величины не получит никогда",
			ks.Knob, names, ks.File, ks.Line, ks.Service))
	}

	t.Logf("перепись: файлов Go разобрано %d, ручек выведено из кода %d, имён ручек %d (%v), "+
		"шаблонов посадки прочитано %d, действий шаблона осмотрено %d, объявлений домена %d, "+
		"употреблений %d (чеканка %d, произведённых имён %d), находок %d",
		goFiles, census.Knobs, len(tokens), tokens, census.Files, census.Actions, len(decls),
		len(uses), issuing, len(produced), len(findings))

	if census.Files == 0 || census.Actions == 0 {
		t.Fatalf("прочитано %d шаблонов и %d действий — обход перестал видеть предмет, и его "+
			"молчание сказано ни о чём", census.Files, census.Actions)
	}
	if census.Knobs == 0 {
		t.Fatalf("из кода не выведено ни одной ручки домена (Spec.TrustDomainKnob) — связывать " +
			"стороны нечем, и гейт молчал бы при любом расхождении")
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("домен доверия объявлен ДВАЖДЫ либо только с одной стороны — %d находк(и):\n  %s\n\n"+
			"Сертификат чеканится под доменом из величины профиля, а процесс признаёт своим тот, "+
			"что назван его ручкой. Разойдутся — законный отправитель перестанет опознаваться: "+
			"сертификат настоящий, выпущен настоящим центром, а принимающий его домена не знает, "+
			"и отказ неотличим от вызова без личности.\n"+
			"Снятие: объявить домен ОДИН раз именованным шаблоном чарта (`define \"<чарт>.trustDomain\"`) "+
			"и брать его через include на ОБЕИХ сторонах.",
			len(findings), strings.Join(findings, "\n  "))
	}
	if len(decls) == 0 || issuing == 0 {
		t.Fatalf("объявлений домена %d, мест чеканки %d — гейт беспредметен: он молчит и тогда, "+
			"когда посадка домен назначать перестала", len(decls), issuing)
	}
}

// trustDomainChartOf — чарт, которому принадлежит файл. Umbrella-шаблоны
// относятся к чарту `umbrella`: у них своё объявление и свой процесс.
func trustDomainChartOf(rel string) string {
	const charts = "/charts/"
	if i := strings.Index(rel, charts); i >= 0 {
		rest := rel[i+len(charts):]
		if j := strings.IndexByte(rest, '/'); j > 0 {
			return rest[:j]
		}
	}
	return "umbrella"
}

// trustDomainComponentRootOf — корень компонента, которому принадлежит файл.
//
// Нужен затем, чтобы ключ файла настроек засчитывался производством только в
// дереве СВОЕЙ службы: последний сегмент имени у разных служб совпадает
// (`authn.trust-domain` и `authz.trust-domain`), и общий счёт объявил бы
// переведённой службу, чей чарт величины не отдаёт.
//
// Корень берётся ПО РАСКЛАДКЕ, а не по перечню имён: перечень разошёлся бы с
// деревом молча. Код компонента живёт под `<корень>/cmd/…` либо
// `<корень>/internal/…`, его чарт — под `<корень>/deploy/…`.
func trustDomainComponentRootOf(rel string) string {
	for _, seg := range []string{"/cmd/", "/internal/", "/deploy/"} {
		if i := strings.Index(rel, seg); i > 0 {
			return rel[:i]
		}
	}
	if i := strings.LastIndexByte(rel, '/'); i > 0 {
		return rel[:i]
	}
	return rel
}
