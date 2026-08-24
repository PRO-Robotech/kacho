// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_inherited_schemas_injection_test.go — гейт покрытия унаследованных
// схем СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Гейт, доказанный только зелёным деревом, доказан не был: он остаётся зелёным и
// когда перестаёт читать. Здесь дефект ВОЗВРАЩАЕТСЯ настоящим входом — теми же
// профилями, той же цепочкой, тем же рендером, — и рядом ставится ЗАКОННЫЙ
// БЛИЗНЕЦ той же формы, на котором гейт обязан молчать.
//
// Зовётся ТОТ ЖЕ адъюдикатор, что исполняет гейт (`adjudicateInheritedSchemas`),
// а не его копия: копия разошлась бы с оригиналом молча и доказывала бы саму
// себя.
//
// Осей пять, у каждой обе стороны:
//
//	(а) покрытие      — ссылка снята ⇒ находка с именем схемы · ссылка на месте ⇒ молчание
//	(б) самоистечение — предмета у объявления нет ⇒ находка   · предмет есть ⇒ молчание
//	(в) согласие тела — копия литерала разошлась ⇒ находка    · тот же узел ⇒ молчание
//	(г) коллизия имён — унаследованное имя = наше ⇒ находка   · разные имена ⇒ молчание
//	(д) исполнение    — рендер БЕЗ объявления теряет схему    · С объявлением несёт
//
// Ось (д) отвечает на вопрос, которого объявленческие оси не задают: читает ли
// шаблон то, что профиль объявил. Объявление, которого шаблон не читает, — форма
// без содержания, и первые четыре оси на нём зелены.
package deploy_test

import (
	"strings"
	"testing"
)

// realDeclsOf — НАСТОЯЩИЕ объявления стека: обе стороны, как их видит гейт.
func realDeclsOf(t *testing.T, stack string) (provider, ours []schemaDecl) {
	t.Helper()
	merged, files, bytes := mergedValuesOfStack(t, chainOf(t, stack))
	if files == 0 || bytes == 0 {
		t.Fatalf("стек %q: профилей прочитано %d, байт %d — инъекция потеряла ВХОД, "+
			"а не предмет", stack, files, bytes)
	}
	pn, _ := lookup(merged, "kratos", "kratos", "config", "identity", "schemas")
	on, _ := lookup(merged, "global", "kacho", "identity", "inheritedSchemas")
	return schemaDeclsOf(pn), schemaDeclsOf(on)
}

func TestIdentityInheritedSchemasGate_ProvenByInjection(t *testing.T) {
	own := ownIdentitySchemaID(t)
	var found, silent int

	// Вход берётся из дерева, а не выдумывается: инъекция обязана идти по тому
	// же входу, что и гейт. Стек назван тот, ради которого задача заведена.
	provider, ours := realDeclsOf(t, "a8f60d")
	if len(provider) == 0 {
		t.Fatalf("стек a8f60d не объявляет НИ ОДНОЙ схемы у слоя под нами — "+
			"дефект возвращать не на чем; предмет инъекции исчез (собственная схема %q)", own)
	}
	if len(ours) == 0 {
		t.Fatal("стек a8f60d не объявляет унаследованных схем — законного близнеца нет, " +
			"и «молчание» было бы свойством пустого входа")
	}

	// ── (а) ДЕФЕКТ ВОЗВРАЩЁН: ссылка снята ─────────────────────────────────
	t.Run("дефект возвращён: унаследованная схема не объявлена", func(t *testing.T) {
		got := adjudicateInheritedSchemas("a8f60d", provider, nil, own)
		if len(got) == 0 {
			t.Fatal("адъюдикатор смолчал на снятом объявлении — гейт не различает " +
				"состояния, ради которых заведён")
		}
		hit := false
		for _, f := range got {
			if f.Reason == inheritedNotCovered && f.ID == provider[0].ID {
				hit = true
			}
		}
		if !hit {
			t.Fatalf("находка есть, но не та: ждали %q по схеме %q, получили %v",
				inheritedNotCovered, provider[0].ID, got)
		}
		found++
		t.Logf("дефект воспроизведён: %d находок, схема %q не покрыта", len(got), provider[0].ID)
	})

	// ── (а) ЗАКОННЫЙ БЛИЗНЕЦ: обе стороны объявлены ────────────────────────
	t.Run("законный близнец: обе стороны объявлены", func(t *testing.T) {
		if got := adjudicateInheritedSchemas("a8f60d", provider, ours, own); len(got) != 0 {
			t.Fatalf("адъюдикатор нашёл %v на исправном входе — гейт ловит форму, "+
				"а не существо, и первый же ложный срабат его отключит", got)
		}
		silent++
		t.Logf("законный близнец молчит: поставщик %v · наше %v", provider, ours)
	})

	// ── (б) САМОИСТЕЧЕНИЕ: объявление без предмета ─────────────────────────
	t.Run("дефект возвращён: объявление пережило свой предмет", func(t *testing.T) {
		got := adjudicateInheritedSchemas("a8f60d", nil, ours, own)
		hit := false
		for _, f := range got {
			if f.Reason == inheritedNoSubject && f.ID == ours[0].ID {
				hit = true
			}
		}
		if !hit {
			t.Fatalf("объявление, которому нечего наследовать, не названо находкой: %v", got)
		}
		found++
		t.Logf("самоистечение работает: %q без предмета — находка", ours[0].ID)
	})

	// ── (в) СОГЛАСИЕ ТЕЛА: копия литерала разошлась ────────────────────────
	t.Run("дефект возвращён: тела разошлись", func(t *testing.T) {
		drifted := make([]schemaDecl, len(ours))
		copy(drifted, ours)
		drifted[0].URL = drifted[0].URL + "Zg==" // копия, отредактированная с одной стороны
		got := adjudicateInheritedSchemas("a8f60d", provider, drifted, own)
		hit := false
		for _, f := range got {
			if f.Reason == inheritedBodyDrift && f.ID == drifted[0].ID {
				hit = true
			}
		}
		if !hit {
			t.Fatalf("разошедшиеся тела одного имени не названы находкой: %v", got)
		}
		found++
		t.Logf("расхождение тела поймано: %q", drifted[0].ID)
	})

	// ── (в) ЗАКОННЫЙ БЛИЗНЕЦ: тела совпадают дословно ──────────────────────
	t.Run("законный близнец: тела совпадают", func(t *testing.T) {
		same := make([]schemaDecl, len(ours))
		copy(same, ours)
		if got := adjudicateInheritedSchemas("a8f60d", provider, same, own); len(got) != 0 {
			t.Fatalf("совпадающие тела названы расхождением: %v", got)
		}
		silent++
	})

	// ── (г) КОЛЛИЗИЯ ИМЁН ──────────────────────────────────────────────────
	t.Run("дефект возвращён: унаследованное имя совпало с нашим", func(t *testing.T) {
		clash := []schemaDecl{{ID: own, URL: "file:///dev/null"}}
		got := adjudicateInheritedSchemas("a8f60d", provider, clash, own)
		hit := false
		for _, f := range got {
			if f.Reason == inheritedCollides && f.ID == own {
				hit = true
			}
		}
		if !hit {
			t.Fatalf("коллизия имени %q с собственной схемой не названа находкой: %v", own, got)
		}
		found++
	})

	// ── ПРЕДПОСЫЛКА: пустой вход не выдаётся за чистый ─────────────────────
	// Обе стороны пусты ⇒ находок нет ПО ПРАВУ (сверять нечего). Именно поэтому
	// сам гейт печатает перепись и падает на пустом обходе дерева: без этого
	// «ноль находок» было бы неотличимо от «ноль прочитанного».
	t.Run("пустой вход даёт ноль находок — и потому вердикт даёт перепись, а не он", func(t *testing.T) {
		if got := adjudicateInheritedSchemas("пусто", nil, nil, own); len(got) != 0 {
			t.Fatalf("на пустом входе адъюдикатор что-то нашёл: %v", got)
		}
		silent++
	})

	t.Logf("перепись инъекции (оси а–г): находок %d · законных близнецов %d", found, silent)
	if found == 0 || silent == 0 {
		t.Fatal("инъекция односторонняя — доказательства нет: гейт, у которого не " +
			"проверена одна из сторон, отличает не то, что заявляет")
	}
}

// TestIdentityInheritedSchemasGate_RenderCarriesTheDeclaration — ось (д).
//
// Объявления мало: шаблон обязан его ЧИТАТЬ. Дефект возвращается настоящим
// входом — тем же чартом и той же цепочкой, у которой значение снято на месте.
func TestIdentityInheritedSchemasGate_RenderCarriesTheDeclaration(t *testing.T) {
	chain := chainOf(t, "a8f60d")
	own := ownIdentitySchemaID(t)
	provider, _ := realDeclsOf(t, "a8f60d")
	if len(provider) == 0 {
		t.Fatal("предмет инъекции исчез: слой под нами схем не объявляет")
	}
	legacy := provider[0].ID

	// ── ДЕФЕКТ ВОЗВРАЩЁН ───────────────────────────────────────────────────
	out, err := renderIdentitySubchart(t, chain, "global.kacho.identity.inheritedSchemas=null")
	if err != nil {
		t.Fatalf("рендер отказал: %v\n%s", err, out)
	}
	cfg, _ := identityConfigOf(t, out)
	ids := renderedSchemaIDs(t, cfg)
	if _, ok := ids[legacy]; ok {
		t.Fatalf("при снятом объявлении схема %q ВСЁ РАВНО в рендере — дефект "+
			"воспроизвести не удалось, значит инъекция меряет не то, что гейт", legacy)
	}
	if _, ok := ids[own]; !ok {
		t.Fatalf("при снятом объявлении пропала и СОБСТВЕННАЯ схема %q — воспроизведён "+
			"не тот дефект", own)
	}
	t.Logf("дефект воспроизведён: рендер несёт %d схем, %q среди них НЕТ", len(ids), legacy)

	// ── ЗАКОННЫЙ БЛИЗНЕЦ ───────────────────────────────────────────────────
	out, err = renderIdentitySubchart(t, chain)
	if err != nil {
		t.Fatalf("рендер отказал: %v\n%s", err, out)
	}
	cfg, body := identityConfigOf(t, out)
	ids = renderedSchemaIDs(t, cfg)
	for _, want := range []string{own, legacy} {
		if _, ok := ids[want]; !ok {
			t.Fatalf("законный близнец не несёт схемы %q (несёт %d: %v)", want, len(ids), ids)
		}
	}
	if got := ids[legacy]; got != provider[0].URL {
		t.Fatalf("тело схемы %q в рендере не то, что объявлено (%s против %s)",
			legacy, shortURL(got), shortURL(provider[0].URL))
	}
	// Умолчание для НОВЫХ регистраций остаётся нашим — иначе покрытие старых
	// строк молча вернуло бы старую форму каждому новому арендатору.
	if def, _ := cfg["identity"].(map[string]any); def != nil {
		if v, _ := def["default_schema_id"].(string); v != own {
			t.Fatalf("умолчание регистрации стало %q вместо %q — покрытие старых строк "+
				"не вправе менять форму НОВЫХ", v, own)
		}
	}
	if !strings.Contains(body, legacy) {
		t.Fatalf("в теле настроек нет имени %q — разбор слеп", legacy)
	}
	t.Logf("законный близнец: рендер несёт %d схем (%v), умолчание регистрации %q",
		len(ids), keysOf(ids), own)

	// ── КОЛЛИЗИЯ: шаблон обязан ОТКАЗАТЬ, а не выбирать по порядку ─────────
	out, err = renderIdentitySubchart(t, chain,
		"global.kacho.identity.inheritedSchemas[0].id="+own,
		"global.kacho.identity.inheritedSchemas[0].url=file:///dev/null")
	if err == nil {
		t.Fatalf("рендер ПРОШЁЛ при двух схемах под именем %q — процесс получил бы "+
			"конфигурацию, где какое тело применится к живым строкам решает порядок:\n%s",
			own, out)
	}
	if !strings.Contains(out, own) {
		t.Fatalf("отказ рендера не называет имя-виновника %q: %s", own, out)
	}
	t.Logf("коллизия имён отвергнута рендером и названа по имени")
}

// renderedSchemaIDs — имена и тела схем из ОТРЕНДЕРЕННЫХ настроек.
func renderedSchemaIDs(t *testing.T, cfg map[string]any) map[string]string {
	t.Helper()
	node, _ := cfg["identity"].(map[string]any)
	if node == nil {
		t.Fatal("в отрендеренных настройках нет секции identity — вердикта нет: " +
			"«схемы не найдено» неотличимо от «секции не найдено»")
	}
	out := map[string]string{}
	for _, d := range schemaDeclsOf(node["schemas"]) {
		out[d.ID] = d.URL
	}
	if len(out) == 0 {
		t.Fatal("секция identity не объявляет НИ ОДНОЙ схемы — разбор слеп")
	}
	return out
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
