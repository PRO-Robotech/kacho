// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bothidentityformsproducer_injection_test.go — доказательство того, что
// разбор входа «обе формы личности разом» СПОСОБЕН найти держателя, и находит
// его по ВХОДУ, а не по имени пробы.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanBothIdentityFormsProducers), что и
// гейт: иначе проверялась бы копия правил, а не они сами.
//
// Пар здесь три, по одной на каждую названную форму записи входа, и у каждой
// пары есть законный близнец, отличающийся ровно ОДНИМ фактом:
//
//	форма 1 «одно выражение»      — близнец: тот же вызов с ПУСТЫМ удостоверением;
//	форма 2 «накапливающий носитель» — близнец: две формы на ДВУХ РАЗНЫХ носителях;
//	третий держатель под ЧУЖИМ именем — близнец: перечень без предъявления.
//
// Сверх пар здесь стоит СРАВНЕНИЕ ДВУХ ВЕЛИЧИН, ради которого написана вся эта
// работа: на одном и том же корпусе предикат ПО ИМЕНИ пробы находит два файла,
// а разбор ПО ВХОДУ — трёх держателей. Разница — не край и не редкость: третий
// держатель есть кейс внутри пробы соседнего семейства, и имени снятого
// сценария он не содержит вовсе.
package repohygiene

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ── фикстуры: пакет проб читателя предъявленного ────────────────────────────

// injectedPresentedHarness — помощники пакета. Ни один из них держателем не
// является: их предмет — положить РОВНО ОДНУ форму. Сходятся формы у
// вызывающего, и именно его ищет разбор.
const injectedPresentedHarness = `package presentedcred_test

func (s *stand) present(t *testing.T, raw string, ctxOpts ...func(context.Context) context.Context) (operations.Principal, bool, error) {
	ctx := context.Background()
	if raw != "" {
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(presentedcred.MetadataKey, "Bearer "+raw))
	}
	for _, o := range ctxOpts {
		ctx = o(ctx)
	}
	return s.run(ctx)
}

func (s *stand) good(t *testing.T) string { return goodMint(s.key, s.now).sign(t) }

func withTrustedForwarded(p operations.Principal) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context {
		return metadata.NewIncomingContext(ctx, metadata.Pairs(
			grpcsrv.MDKeyPrincipalType, p.Type,
			grpcsrv.MDKeyPrincipalID, p.ID,
		))
	}
}
`

// injectedHolderNamedAfterTheBranch — держатель ПЕРВЫЙ: проба, названная по
// снимаемому сценарию. Её находит и предикат по имени.
const injectedHolderNamedAfterTheBranch = `package presentedcred_test

func TestKAN_DUP_01_BothIdentityFormsAtOnceAreRefused(t *testing.T) {
	s := newStand(t)
	raw := s.good(t)

	if _, _, err := s.present(t, raw); err != nil {
		t.Fatalf("положительный близнец: %v", err)
	}

	forwarded := operations.Principal{Type: "user", ID: "usr-forwarded"}
	if _, _, err := s.present(t, "", withTrustedForwarded(forwarded)); err != nil {
		t.Fatalf("положительный близнец: %v", err)
	}

	_, _, err := s.present(t, raw, withTrustedForwarded(forwarded))
	if err == nil {
		t.Fatal("обе формы личности в одном запросе приняты")
	}
}
`

// injectedHolderInsideAForeignFamily — держатель ТРЕТИЙ, ради которого всё
// написано: кейс внутри пробы СОСЕДНЕГО семейства. Перечень причин отказа
// закрыт счётчиком, имени снятого сценария в пробе нет ни в каком виде, и
// предикат по имени не видит её by construction.
// Клауза пакета здесь намеренно отсутствует: снимок дописывается к соседнему
// файлу того же пакета — держатель третий живёт в ОДНОМ файле со вторым, и
// предикат по имени считает файлы.
const injectedHolderInsideAForeignFamily = `
func TestKAN_DENY_01_EveryAuthenticationRefusalIsByteIdentical(t *testing.T) {
	s := newStand(t)
	refusals := map[string]error{}

	refusals["срок вышел"] = mustRefuse(t, s, s.expired(t))
	refusals["подпись не сходится"] = mustRefuse(t, s, s.forged(t))

	s = newStand(t)
	_, _, err := s.present(t, s.good(t),
		withTrustedForwarded(operations.Principal{Type: "user", ID: "usr-forwarded"}))
	if err == nil {
		t.Fatal("обе формы разом приняты")
	}
	refusals["обе формы разом"] = err

	if len(refusals) != 13 {
		t.Fatalf("перечень причин закрыт и содержит тринадцать строк, собрано %d", len(refusals))
	}
}
`

// injectedHolderOnTheRealChain — держатель ВТОРОЙ: тот же вход, собранный
// СЛИЯНИЕМ МЕТАДАННЫХ на боевой цепочке. Носитель переданной личности приезжает
// локальным именем — без подстановки на один уровень разбор промолчал бы.
const injectedHolderOnTheRealChain = `package kaname

func TestKAN_DUP_01_BothFormsOnTheRealChain(t *testing.T) {
	reader, raw := chainReader(t)
	chain := publicChainWithReader(reader)

	fwdOnly := forwardedIdentity(verifiedCertPeer(t, fwdGatewaySAN), "usr-alice")
	if _, _, err := runChain(t, chain, fwdOnly); err != nil {
		t.Fatalf("прежний путь сломан: %v", err)
	}

	both := metadata.NewIncomingContext(fwdOnly, mergeIncoming(fwdOnly,
		metadata.Pairs(presentedcred.MetadataKey, "Bearer "+raw)))
	if _, _, err := runChain(t, chain, both); err == nil {
		t.Fatal("обе формы личности в одном запросе приняты")
	}
}

func forwardedIdentity(ctx context.Context, userID string) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs(
		grpcsrv.MDKeyPrincipalType, "user",
		grpcsrv.MDKeyPrincipalID, userID,
	))
}
`

// injectedLawfulAfterWithdrawal — то же дерево ПОСЛЕ снятия ветки: остались
// только положительные близнецы. Обе формы в корпусе по-прежнему производятся —
// поэтому ноль держателей здесь есть вердикт, а не молчание слепого разборщика.
const injectedLawfulAfterWithdrawal = `package presentedcred_test

func TestKAN_FWD_01_ForwardedIdentityStillPassesTheReader(t *testing.T) {
	s := newStand(t)
	raw := s.good(t)

	if _, _, err := s.present(t, raw); err != nil {
		t.Fatalf("годный токен отвергнут: %v", err)
	}

	forwarded := operations.Principal{Type: "user", ID: "usr-forwarded"}
	p, present, err := s.present(t, "", withTrustedForwarded(forwarded))
	if err != nil {
		t.Fatalf("прежний путь отозван: %v", err)
	}
	if !present || p.ID != forwarded.ID {
		t.Fatalf("читатель подменил переданную личность: %+v (носитель=%v)", p, present)
	}
}
`

// injectedAccumulatingCarrier — форма 2: обе формы кладутся носителю
// ПОСЛЕДОВАТЕЛЬНЫМИ вызовами. Одного выражения здесь нет вовсе, и разборщик
// первой формы промолчал бы.
const injectedAccumulatingCarrier = `package principalmeta_test

func TestForwardedRequestCarriesBothForms(t *testing.T) {
	in := metadata.MD{}
	in.Set("authorization", "Bearer tenant-token")
	in.Set(principalmeta.MetaPrincipalType, "user")
	in.Set(principalmeta.MetaPrincipalID, "usr-1")

	out, _ := metadata.FromOutgoingContext(
		principalmeta.OutgoingFromIncoming(metadata.NewIncomingContext(context.Background(), in)))
	_ = out
}
`

// injectedTwoSeparateCarriers — законный близнец формы 2: те же две формы, но
// на ДВУХ РАЗНЫХ носителях. Отличие ровно одно — носитель; вход «обе формы в
// ОДНОМ запросе» не собран, и разбор обязан молчать.
const injectedTwoSeparateCarriers = `package principalmeta_test

func TestEachFormTravelsInItsOwnRequest(t *testing.T) {
	credentialOnly := metadata.MD{}
	credentialOnly.Set("authorization", "Bearer tenant-token")

	forwardedOnly := metadata.MD{}
	forwardedOnly.Set(principalmeta.MetaPrincipalType, "user")
	forwardedOnly.Set(principalmeta.MetaPrincipalID, "usr-1")

	_ = credentialOnly
	_ = forwardedOnly
}
`

// ── помощники инъекции ──────────────────────────────────────────────────────

// injectedByNamePredicate — ТОТ САМЫЙ предикат приёмки: поиск по имени пробы.
// Он стоит здесь не для украшения: без него «три» не с чем сравнивать, а
// именно сравнение двух величин и есть предмет этой работы.
var injectedByNamePredicate = regexp.MustCompile(`func TestKAN_DUP_01`)

func injectedHolders(t *testing.T, sources map[string][]byte) ([]string, BothFormsCensus) {
	t.Helper()
	sites, census, err := ScanBothIdentityFormsProducers(sources)
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range sites {
		key := s.File + "#" + s.Func
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	return out, census
}

func injectedSources(pairs ...string) map[string][]byte {
	out := map[string][]byte{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i]] = []byte(pairs[i+1])
	}
	return out
}

// ── доказательства ──────────────────────────────────────────────────────────

// TestInjectedBothFormsRecogniserFindsTheHolderTheNamePredicateMisses — ядро.
//
// На ОДНОМ корпусе считаются две величины: предикат по имени пробы и разбор по
// входу. Первый даёт два файла, второй — трёх держателей, и третий из них лежит
// в пробе соседнего семейства.
func TestInjectedBothFormsRecogniserFindsTheHolderTheNamePredicateMisses(t *testing.T) {
	sources := injectedSources(
		"services/iam/internal/presentedcred/harness_test.go", injectedPresentedHarness,
		"services/iam/internal/presentedcred/reader_test.go",
		injectedHolderNamedAfterTheBranch+injectedHolderInsideAForeignFamily,
		"services/iam/cmd/kaname/chain_test.go", injectedHolderOnTheRealChain,
	)

	// (а) предикат приёмки — по ИМЕНИ пробы.
	byName := 0
	for _, b := range sources {
		if injectedByNamePredicate.Match(b) {
			byName++
		}
	}

	// (б) разбор — по ВХОДУ.
	holders, census := injectedHolders(t, sources)

	t.Logf("перепись инъекции: файлов %d · функций %d · вызовов %d · мест с удостоверением %d · "+
		"мест с переданной личностью %d", len(sources), census.Funcs, census.Calls,
		census.CredentialSites, census.ForwardedSites)
	t.Logf("предикат ПО ИМЕНИ: файлов %d · разбор ПО ВХОДУ: держателей %d (%s)",
		byName, len(holders), strings.Join(holders, ", "))

	if byName != 2 {
		t.Fatalf("предикат по имени нашёл %d файл(ов), ожидалось 2 — фикстура перестала "+
			"воспроизводить исходную форму, и сравнение величин беспредметно", byName)
	}
	if len(holders) != 3 {
		t.Fatalf("разбор по входу нашёл %d держателей, ожидалось 3: %v.\n"+
			"Три против двух — вся суть: держатели считаются по ВХОДУ, и два из трёх "+
			"лежат в одном файле, поэтому счёт файлов даёт два.", len(holders), holders)
	}
	var foreign bool
	for _, h := range holders {
		if strings.Contains(h, "TestKAN_DENY_01") {
			foreign = true
		}
	}
	if !foreign {
		t.Fatalf("держатель под ЧУЖИМ именем НЕ найден: %v.\n\n"+
			"Ровно его и недосчитал предикат по имени пробы: утверждение о снимаемом входе "+
			"живёт кейсом внутри пробы соседнего семейства и имени снятого сценария не "+
			"содержит вовсе. Разбор, потерявший эту находку, вернулся к измерению имени.",
			holders)
	}
}

// TestInjectedBothFormsSingleExpressionFormIsFoundAndItsTwinIsSilent — форма 1
// в обе стороны, на ПОЛНОМ корпусе трёх файлов.
func TestInjectedBothFormsSingleExpressionFormIsFoundAndItsTwinIsSilent(t *testing.T) {
	withBranch := injectedSources(
		"services/iam/internal/presentedcred/harness_test.go", injectedPresentedHarness,
		"services/iam/internal/presentedcred/reader_test.go",
		injectedHolderNamedAfterTheBranch+injectedHolderInsideAForeignFamily,
		"services/iam/cmd/kaname/chain_test.go", injectedHolderOnTheRealChain,
	)
	holders, census := injectedHolders(t, withBranch)
	t.Logf("до снятия: держателей %d (%s); мест с удостоверением %d, с переданной личностью %d",
		len(holders), strings.Join(holders, ", "), census.CredentialSites, census.ForwardedSites)
	var onTheChain bool
	for _, h := range holders {
		if strings.Contains(h, "BothFormsOnTheRealChain") {
			onTheChain = true
		}
	}
	if !onTheChain {
		t.Fatalf("держатель на БОЕВОЙ ЦЕПОЧКЕ не найден: %v.\n"+
			"Там вход собран слиянием метаданных, а носитель переданной личности приезжает "+
			"локальным именем: без подстановки имени на один уровень разбор промолчал бы.",
			holders)
	}

	// Законный близнец: ветка снята, остались положительные утверждения.
	lawful := injectedSources(
		"services/iam/internal/presentedcred/harness_test.go", injectedPresentedHarness,
		"services/iam/internal/presentedcred/reader_test.go", injectedLawfulAfterWithdrawal,
	)
	twinHolders, twinCensus := injectedHolders(t, lawful)
	t.Logf("после снятия: держателей %d; мест с удостоверением %d, с переданной личностью %d",
		len(twinHolders), twinCensus.CredentialSites, twinCensus.ForwardedSites)
	if len(twinHolders) != 0 {
		t.Fatalf("законный близнец объявлен держателем: %v.\n"+
			"Отличие близнеца ровно одно — предъявленное удостоверение пусто, и производитель "+
			"на пустом входе метаданных удостоверения не ставит вовсе.", twinHolders)
	}
	// Положительный контроль близнеца: обе формы в корпусе ПРОИЗВОДЯТСЯ, значит
	// ноль держателей есть вердикт, а не молчание слепого разборщика.
	if twinCensus.CredentialSites == 0 || twinCensus.ForwardedSites == 0 {
		t.Fatalf("на законном близнеце разбор не видит ни одной формы (удостоверение %d, "+
			"переданная личность %d) — его ноль держателей сказан ни о чём",
			twinCensus.CredentialSites, twinCensus.ForwardedSites)
	}
}

// TestInjectedBothFormsAccumulatingCarrierFormIsFoundAndItsTwinIsSilent —
// форма 2 в обе стороны.
//
// Эта пара обязательна отдельно: разборщик, знающий только форму «одно
// выражение», на накапливающем носителе не даёт ни красного, ни зелёного — он
// МОЛЧИТ, и записанное этой формой оказывается вне наблюдения.
func TestInjectedBothFormsAccumulatingCarrierFormIsFoundAndItsTwinIsSilent(t *testing.T) {
	defect := injectedSources("gateway/internal/principalmeta/strip_test.go", injectedAccumulatingCarrier)
	holders, census := injectedHolders(t, defect)
	t.Logf("накапливающий носитель: держателей %d (%s); носителей запроса %d",
		len(holders), strings.Join(holders, ", "), census.Carriers)
	if len(holders) != 1 {
		t.Fatalf("форма «накапливающий носитель» НЕ опознана: держателей %d, ожидался 1: %v.\n"+
			"Одного выражения в этой форме нет вовсе — обе формы кладутся носителю "+
			"последовательными вызовами, и разборщик, знающий только выражение, промолчал бы.",
			len(holders), holders)
	}

	twin := injectedSources("gateway/internal/principalmeta/strip_test.go", injectedTwoSeparateCarriers)
	twinHolders, twinCensus := injectedHolders(t, twin)
	t.Logf("два носителя: держателей %d; носителей запроса %d", len(twinHolders), twinCensus.Carriers)
	if len(twinHolders) != 0 {
		t.Fatalf("законный близнец объявлен держателем: %v.\n"+
			"Отличие ровно одно — носителя два, и вход «обе формы в ОДНОМ запросе» не собран. "+
			"Разбор, считающий формы по всему телу функции, объявил бы держателем всякую "+
			"пробу, шлющую два разных запроса.", twinHolders)
	}
	if twinCensus.Carriers == 0 {
		t.Fatalf("на близнеце не опознано ни одного носителя запроса — молчание сказано ни о чём")
	}
}

// TestInjectedBothFormsBlindRecogniserIsNotGreen — пустой обход и слепой
// разборщик не выглядят чистым деревом.
func TestInjectedBothFormsBlindRecogniserIsNotGreen(t *testing.T) {
	// (а) пустой корпус: ноль функций и ноль мест обеих форм. Именно на этих
	// величинах гейт роняет прогон, а не на числе держателей.
	empty := injectedSources()
	holders, census := injectedHolders(t, empty)
	if len(holders) != 0 {
		t.Fatalf("на пустом корпусе найдены держатели: %v", holders)
	}
	if census.Funcs != 0 || census.CredentialSites != 0 || census.ForwardedSites != 0 {
		t.Fatalf("пустой корпус объявил непустую перепись: функций %d, мест %d/%d",
			census.Funcs, census.CredentialSites, census.ForwardedSites)
	}
	t.Logf("пустой корпус: функций %d, мест с удостоверением %d, с переданной личностью %d — "+
		"ровно эти нули гейт читает как сломанный обход", census.Funcs,
		census.CredentialSites, census.ForwardedSites)

	// (б) корпус, где производится ТОЛЬКО переданная личность: держателей нет и
	// быть не может, а перепись это показывает — ноль мест удостоверения.
	fwdOnly := injectedSources("services/iam/cmd/kaname/chain_test.go", `package kaname

func TestForwardedOnly(t *testing.T) {
	ctx := forwardedIdentity(context.Background(), "usr-alice")
	_ = ctx
}

func forwardedIdentity(ctx context.Context, userID string) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs(grpcsrv.MDKeyPrincipalType, "user"))
}
`)
	fwdHolders, fwdCensus := injectedHolders(t, fwdOnly)
	if len(fwdHolders) != 0 {
		t.Fatalf("корпус без удостоверения объявил держателей: %v", fwdHolders)
	}
	if fwdCensus.CredentialSites != 0 {
		t.Fatalf("в корпусе без удостоверения опознано %d мест с удостоверением — "+
			"разборщик считает удостоверением что-то другое", fwdCensus.CredentialSites)
	}
	if fwdCensus.ForwardedSites == 0 {
		t.Fatal("в корпусе переданной личности не опознано ни одного её места — разборщик слеп")
	}
	t.Logf("только переданная личность: держателей %d, мест с удостоверением %d, "+
		"с переданной личностью %d", len(fwdHolders), fwdCensus.CredentialSites, fwdCensus.ForwardedSites)
}

// TestInjectedBothFormsAdjudicationCallsTheFindingAndTheOrphanApart — вторая
// половина гейта: РЕШЕНИЕ о найденном.
//
// Разбор отвечает «где вход собран»; решение — «законно ли это здесь». Их
// проверяют порознь: разбор, нашедший всё, при сломанном решении даёт зелёный
// вердикт, и наоборот. Инъекция гоняет ту же `AdjudicateBothFormsProducers`,
// которую зовёт гейт.
func TestInjectedBothFormsAdjudicationCallsTheFindingAndTheOrphanApart(t *testing.T) {
	allowed := map[string]string{"gateway/internal/principalmeta": "пробы снятия на крае"}

	atTheEdge := BothFormsSite{
		File: "gateway/internal/principalmeta/credential_strip_test.go", Line: 35,
		Func: "TestStripLeavesOnlyOneForm", Form: "накапливающий носитель",
	}
	behindTheEdge := BothFormsSite{
		File: "services/iam/internal/presentedcred/reader_test.go", Line: 515,
		Func: "TestKAN_DENY_01_EveryAuthenticationRefusalIsByteIdentical", Form: "одно выражение",
	}

	// (а) только законный производитель — молчание, и запись не осиротела.
	lawful := AdjudicateBothFormsProducers([]BothFormsSite{atTheEdge}, allowed)
	t.Logf("законный: находок %d, осиротевших записей %d, держателей %d",
		len(lawful.Offenders), len(lawful.OrphanEntries), lawful.Holders)
	if len(lawful.Offenders) != 0 || len(lawful.OrphanEntries) != 0 {
		t.Fatalf("законный производитель на крае объявлен находкой: находок %d, "+
			"осиротевших %v", len(lawful.Offenders), lawful.OrphanEntries)
	}

	// (б) держатель ЗА краем — находка, и она называет координату.
	defect := AdjudicateBothFormsProducers([]BothFormsSite{atTheEdge, behindTheEdge}, allowed)
	if len(defect.Offenders) != 1 {
		t.Fatalf("держатель за краем не стал находкой: находок %d", len(defect.Offenders))
	}
	if defect.Offenders[0].File != behindTheEdge.File || defect.Offenders[0].Line != behindTheEdge.Line {
		t.Fatalf("находка называет не ту координату: %s:%d",
			defect.Offenders[0].File, defect.Offenders[0].Line)
	}
	if defect.Holders != 2 {
		t.Fatalf("держателей насчитано %d, ожидалось 2", defect.Holders)
	}
	t.Logf("дефект: находка %s:%d (%s)", defect.Offenders[0].File, defect.Offenders[0].Line,
		defect.Offenders[0].Func)

	// (в) запись разрешения, которой больше нечего разрешать. Послабление,
	// пережившее свой предмет, есть слепая зона, выданная вперёд.
	orphan := AdjudicateBothFormsProducers(nil, allowed)
	if len(orphan.OrphanEntries) != 1 || orphan.OrphanEntries[0] != "gateway/internal/principalmeta" {
		t.Fatalf("запись, потерявшая предмет, не названа: %v", orphan.OrphanEntries)
	}
	t.Logf("самоистечение: запись без предмета названа — %v", orphan.OrphanEntries)

	// (г) находка и осиротевшая запись НЕ маскируют друг друга: разрешение,
	// потерявшее предмет, не делает находку невидимой, и наоборот.
	both := AdjudicateBothFormsProducers([]BothFormsSite{behindTheEdge}, allowed)
	if len(both.Offenders) != 1 || len(both.OrphanEntries) != 1 {
		t.Fatalf("находка и осиротевшая запись маскируют друг друга: находок %d, осиротевших %d",
			len(both.Offenders), len(both.OrphanEntries))
	}
	t.Logf("обе стороны разом: находок %d, осиротевших записей %d",
		len(both.Offenders), len(both.OrphanEntries))
}
