// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// published_image_pin_is_reachable_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// проверка досягаемости пина способна упасть, и что она молчит на законном.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ДОКАЗАТЕЛЬСТВО ВОТ ТАКОЕ
//
// У гейта две половины, и подделывать нельзя ни одну:
//
//	РЕШЕНИЕ     `unreachablePinFindings` — чистая функция. Ей подаётся вход той же
//	            формы, что приходит из дерева, и каждая ось проверяется отдельно
//	            вместе с законным близнецом;
//	КЛАССИФИКАЦИЯ `askRegistry` — три ответа реестра. Им подаётся НАСТОЯЩИЙ HTTP:
//	            свой сервер, отвечающий как реестр. Сети здесь нет, поэтому
//	            доказательство не умеет пропуститься, а разбор ответов исполняется
//	            тот самый, который работает в конвейере.
//
// Проба, зовущая гейт целиком, доказала бы лишь, что он зелен на сегодняшнем
// дереве — то есть ровно то, что и так видно из его переписи.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАЗЛИЧЕНИЕ, КОТОРОЕ ДОКАЗЫВАЕТСЯ ГЛАВНЫМ ОБРАЗОМ
//
// «Манифеста нет» против «спросить не удалось». Обе половины отвечают одним и тем
// же кодом 404 — на снятый тег и на закрытый либо несуществующий репозиторий, —
// и различает их только КОНТРОЛЬ ПЕРЕД ГЛАВНЫМ ВОПРОСОМ. Поэтому у каждой оси
// здесь есть близнец, отличающийся ОДНИМ фактом: читается ли перечень тегов.
package deploy_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// injectedRegistry — свой реестр: отвечает так, как отвечает Docker Hub.
//
// `tagsCode` — ответ на управляющий вопрос (перечень тегов репозитория),
// `manifestCode` — на главный, `tokenCode` — на выдачу маркера. Меняя РОВНО ОДИН
// из трёх, доказательство отделяет находку от несостоявшегося измерения.
type injectedRegistry struct {
	tokenCode    int
	tagsCode     int
	manifestCode int
}

func (r injectedRegistry) start(t *testing.T) (registryEndpoints, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasPrefix(req.URL.Path, "/token"):
			if r.tokenCode != http.StatusOK {
				w.WriteHeader(r.tokenCode)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"injected"}`))
		case strings.Contains(req.URL.Path, "/tags/list"):
			w.WriteHeader(r.tagsCode)
		case strings.Contains(req.URL.Path, "/manifests/"):
			w.WriteHeader(r.manifestCode)
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	return registryEndpoints{Token: srv.URL + "/token", API: srv.URL + "/v2/"}, srv.Close
}

// TestAskRegistryTellsAbsenceFromFailureToAsk — классификация ответов реестра.
func TestAskRegistryTellsAbsenceFromFailureToAsk(t *testing.T) {
	const ref = "docker.io/prorobotech/kaname:main-injected"
	client := &http.Client{Timeout: 5 * time.Second}

	for _, c := range []struct {
		name   string
		reg    injectedRegistry
		want   registryAnswer
		expect string
	}{
		{
			// ВНЕСЁННЫЙ ДЕФЕКТ. Репозиторий читается, манифеста нет — находка.
			name:   "тега нет, репозиторий читается",
			reg:    injectedRegistry{tokenCode: 200, tagsCode: 200, manifestCode: 404},
			want:   answerAbsent,
			expect: "манифест",
		},
		{
			// ЗАКОННЫЙ БЛИЗНЕЦ, отличается ОДНИМ фактом — кодом манифеста.
			name: "тег есть",
			reg:  injectedRegistry{tokenCode: 200, tagsCode: 200, manifestCode: 200},
			want: answerPresent,
		},
		{
			// ТОТ ЖЕ 404 НА МАНИФЕСТЕ, но управляющий вопрос НЕ прошёл. Это
			// «не выполнилось», а не находка: непрочитанный репозиторий
			// отсутствия отдельного тега не доказывает.
			name:   "репозиторий не читается — тот же 404 не находка",
			reg:    injectedRegistry{tokenCode: 200, tagsCode: 401, manifestCode: 404},
			want:   answerUnresolved,
			expect: "не читается",
		},
		{
			name:   "маркер не выдан",
			reg:    injectedRegistry{tokenCode: 503, tagsCode: 200, manifestCode: 200},
			want:   answerUnresolved,
			expect: "токен",
		},
		{
			// Реестр отвечает непонятным кодом — тоже «не спросили».
			name:   "непонятный код манифеста",
			reg:    injectedRegistry{tokenCode: 200, tagsCode: 200, manifestCode: 500},
			want:   answerUnresolved,
			expect: "не «нет»",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			ep, stop := c.reg.start(t)
			defer stop()
			got, why := newRegistryProbe(client, ep).askRegistry(ref)
			if got != c.want {
				t.Fatalf("ответ %d, ожидался %d (причина: %q)", got, c.want, why)
			}
			if c.expect != "" && !strings.Contains(why, c.expect) {
				t.Fatalf("причина %q не называет %q — находка обязана называть предмет, "+
					"а не симптом", why, c.expect)
			}
		})
	}
}

// TestAskRegistryDoesNotJudgeAnUnknownRegistry — реестр, которого проба не
// умеет спрашивать, даёт «не выполнилось», а не находку.
func TestAskRegistryDoesNotJudgeAnUnknownRegistry(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	ep, stop := injectedRegistry{tokenCode: 200, tagsCode: 200, manifestCode: 404}.start(t)
	defer stop()

	got, why := newRegistryProbe(client, ep).askRegistry("ghcr.io/someone/something:v1")
	if got != answerUnresolved {
		t.Fatalf("чужой реестр разобран как %d — непонятый адрес не доказывает отсутствия", got)
	}
	if !strings.Contains(why, "не умеет") {
		t.Fatalf("причина %q не называет границу пробы", why)
	}
}

// TestUnreachablePinFindingsNameTheStandAndTheCoordinate — решение гейта.
func TestUnreachablePinFindingsNameTheStandAndTheCoordinate(t *testing.T) {
	const dead = "docker.io/prorobotech/kaname:main-be5a45a2"
	const live = "docker.io/prorobotech/kacho-vpc:main-d941344b"

	where := map[string][]pinnedImage{
		dead: {
			{Stand: "dev", Component: "kaname.image", Repo: "docker.io/prorobotech/kaname", Tag: "main-be5a45a2"},
			{Stand: "prod", Component: "kaname.image", Repo: "docker.io/prorobotech/kaname", Tag: "main-be5a45a2"},
		},
		live: {
			{Stand: "prod", Component: "vpc.image", Repo: "docker.io/prorobotech/kacho-vpc", Tag: "main-d941344b"},
		},
	}

	// (а) ВНЕСЁННЫЙ ДЕФЕКТ — одна ссылка без манифеста.
	got := unreachablePinFindings([]pinAnswer{
		{Ref: dead, Answer: answerAbsent, Why: "перечень тегов читается, манифест main-be5a45a2 → код 404"},
		{Ref: live, Answer: answerPresent},
	}, where)
	if len(got) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v", len(got), got)
	}
	for _, must := range []string{dead, "dev", "prod", "kaname.image", "ImagePullBackOff"} {
		if !strings.Contains(got[0], must) {
			t.Errorf("находка не называет %q — читатель пойдёт искать не там:\n%s", must, got[0])
		}
	}
	if strings.Contains(got[0], live) {
		t.Errorf("находка называет живую ссылку %q — обвинение шире предмета", live)
	}

	// (б) ЗАКОННЫЙ БЛИЗНЕЦ — обе ссылки тянутся. Молчит.
	if got := unreachablePinFindings([]pinAnswer{
		{Ref: dead, Answer: answerPresent},
		{Ref: live, Answer: answerPresent},
	}, where); len(got) != 0 {
		t.Errorf("на достижимых ссылках гейт краснеет: %v", got)
	}

	// (в) ВТОРОЙ ЗАКОННЫЙ БЛИЗНЕЦ — спросить не удалось. НЕ находка: иначе гейт
	//     краснел бы на выключенной сети, и его отключили бы первым.
	if got := unreachablePinFindings([]pinAnswer{
		{Ref: dead, Answer: answerUnresolved, Why: "репозиторий не читается"},
		{Ref: live, Answer: answerUnresolved, Why: "токен не получен"},
	}, where); len(got) != 0 {
		t.Errorf("несостоявшееся измерение выдано за находку: %v", got)
	}

	// (г) ТРЕТИЙ БЛИЗНЕЦ — ответов нет вовсе (ручка не поднята). Молчит.
	if got := unreachablePinFindings(nil, where); len(got) != 0 {
		t.Errorf("при выключенном измерении гейт краснеет: %v", got)
	}
}

// TestWalkImageDeclarationsKnowsEveryLegalForm — распознаватель знает ВСЕ формы
// объявления образа, которые в этом дереве законны.
//
// Форма, о которой он не знает, — не край и не редкость: всё записанное в ней
// оказывается вне наблюдения, и гейт при этом молчит, а не краснеет.
func TestWalkImageDeclarationsKnowsEveryLegalForm(t *testing.T) {
	tree := map[string]any{
		// плоская строка — братские подчарты
		"vpc": map[string]any{"image": "docker.io/prorobotech/kacho-vpc:main-1"},
		// плоская строка + соседний digest — вендоренный kacho-geo
		"kacho-geo": map[string]any{
			"image":       "docker.io/prorobotech/kacho-geo:main-1",
			"imageDigest": "sha256:aa",
		},
		// карта — kaname, storage, registry
		"kaname": map[string]any{"image": map[string]any{
			"repository": "docker.io/prorobotech/kaname", "tag": "main-2",
		}},
		// карта с digest — digest перебивает тег
		"storage": map[string]any{"image": map[string]any{
			"repository": "docker.io/prorobotech/kacho-storage", "tag": "main-2", "digest": "sha256:bb",
		}},
		// вложенный модуль консоли
		"uif": map[string]any{"dashboard": map[string]any{
			"image": "docker.io/prorobotech/kacho-ui-future-dashboard:main-3",
		}},
		// репозиторий без тега — судить нечего, но УВИДЕТЬ обязан
		"kacho-nlb": map[string]any{"image": map[string]any{
			"repository": "docker.io/prorobotech/kacho-nlb",
		}},
	}

	want := map[string]string{
		"vpc.image":           "docker.io/prorobotech/kacho-vpc:main-1",
		"kacho-geo.image":     "docker.io/prorobotech/kacho-geo@sha256:aa",
		"kaname.image":        "docker.io/prorobotech/kaname:main-2",
		"storage.image":       "docker.io/prorobotech/kacho-storage@sha256:bb",
		"uif.dashboard.image": "docker.io/prorobotech/kacho-ui-future-dashboard:main-3",
		"kacho-nlb.image":     "docker.io/prorobotech/kacho-nlb:",
	}

	got := map[string]string{}
	for _, d := range walkImageDeclarations(tree, nil) {
		got[d.Component] = d.Ref()
	}
	if len(got) != len(want) {
		t.Fatalf("распознано %d объявлений из %d: %v", len(got), len(want), got)
	}
	for comp, ref := range want {
		if got[comp] != ref {
			t.Errorf("%s: разобрано %q, ожидалось %q", comp, got[comp], ref)
		}
	}
}

// TestPinReadingRecognisesTheRealTree — предикат обязан узнавать НАСТОЯЩЕЕ
// дерево, а не только синтетику: иначе доказательства выше зелёные, а обход
// читает ноль.
func TestPinReadingRecognisesTheRealTree(t *testing.T) {
	pins, census := collectUmbrellaImagePins(t)
	if len(pins) == 0 {
		t.Fatalf("в настоящем дереве не найдено ни одного пина продукта (стендов %d, "+
			"объявлений %d) — предикат разошёлся с деревом", census.Stands, census.Declared)
	}

	// Обе формы объявления обязаны встретиться в настоящем дереве — иначе
	// доказательство формы выше проверяет то, чего в дереве нет.
	forms := map[string]bool{}
	for _, p := range pins {
		switch p.Image() {
		case "kaname":
			forms["карта"] = true
		case "kacho-vpc":
			forms["плоская строка"] = true
		}
		if strings.HasPrefix(p.Component, "uif.") {
			forms["вложенный модуль"] = true
		}
	}
	for _, form := range []string{"карта", "плоская строка", "вложенный модуль"} {
		if !forms[form] {
			t.Errorf("форма объявления %q в настоящем дереве не встретилась — либо она из него "+
				"ушла, и доказательство формы стало вакуумным, либо предикат её не узнаёт", form)
		}
	}
	t.Logf("осмотрено настоящего дерева: пинов продукта %d, стендов %d, объявлений образа %d",
		len(pins), census.Stands, census.Declared)
}

// TestImagesTheUmbrellaLeavesToItsSubcharts — решение второй половины, в обе
// стороны.
func TestImagesTheUmbrellaLeavesToItsSubcharts(t *testing.T) {
	produced := map[string]string{
		"kacho-vpc":      "vpc",
		"kacho-registry": "registry",
	}

	// (а) ВНЕСЁННЫЙ ДЕФЕКТ — умбрелла пинит один образ из двух.
	got := imagesTheUmbrellaLeavesToItsSubcharts(produced, map[string]string{"kacho-vpc": "vpc.image"})
	if len(got) != 1 || !strings.Contains(got[0], "kacho-registry") {
		t.Fatalf("непинуемый образ не назван: %v", got)
	}
	if strings.Contains(got[0], "kacho-vpc") {
		t.Errorf("обвинение шире предмета — назван и запинённый образ: %s", got[0])
	}

	// (б) ЗАКОННЫЙ БЛИЗНЕЦ — запинены оба. Молчит.
	if got := imagesTheUmbrellaLeavesToItsSubcharts(produced, map[string]string{
		"kacho-vpc": "vpc.image", "kacho-registry": "registry.image",
	}); len(got) != 0 {
		t.Errorf("на полном покрытии гейт краснеет: %v", got)
	}

	// (в) ВТОРОЙ ЗАКОННЫЙ БЛИЗНЕЦ — умбрелла пинит БОЛЬШЕ, чем дерево собирает
	//     (образ службы доступа собирает чужой конвейер). Это не находка: предмет
	//     проверки — покрытие произведённого, а не равенство множеств.
	if got := imagesTheUmbrellaLeavesToItsSubcharts(produced, map[string]string{
		"kacho-vpc": "vpc.image", "kacho-registry": "registry.image", "kaname": "kaname.image",
	}); len(got) != 0 {
		t.Errorf("лишний пин выдан за пропущенный: %v", got)
	}
}

// TestRegistryProbeRemembersPerRepositoryWithoutWeakeningTheControl — память по
// репозиторию НЕ ослабляет управляющий вопрос.
//
// ПРЕДМЕТ. Маркер и ответ управляющего вопроса помнятся, чтобы гейт не упирался в
// предел реестра (измерено: на сорока семи ссылках анонимный Docker Hub начинал
// отвечать `429`, и сверялось 8 из 47). Память — оптимизация, и оптимизация
// обязана быть проверена с той стороны, с которой она могла бы стать
// послаблением: закрытый репозиторий обязан давать «не выполнилось» КАЖДОЙ своей
// ссылке, а не только первой.
func TestRegistryProbeRemembersPerRepositoryWithoutWeakeningTheControl(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}

	// (а) Репозиторий читается: управляющий вопрос задан ОДИН раз на три ссылки,
	//     а вердикт у каждой свой.
	var tags, manifests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasPrefix(req.URL.Path, "/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"injected"}`))
		case strings.Contains(req.URL.Path, "/tags/list"):
			tags++
			w.WriteHeader(http.StatusOK)
		case strings.Contains(req.URL.Path, "/manifests/"):
			manifests++
			// Первый тег есть, остальные — нет.
			if strings.HasSuffix(req.URL.Path, "/main-1") {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	ep := registryEndpoints{Token: srv.URL + "/token", API: srv.URL + "/v2/"}

	probe := newRegistryProbe(client, ep)
	want := []registryAnswer{answerPresent, answerAbsent, answerAbsent}
	for i, tag := range []string{"main-1", "main-2", "main-3"} {
		got, why := probe.askRegistry("docker.io/prorobotech/kaname:" + tag)
		if got != want[i] {
			t.Fatalf("%s: ответ %d, ожидался %d (%s)", tag, got, want[i], why)
		}
	}
	if tags != 1 {
		t.Errorf("управляющий вопрос задан %d раз на три ссылки одного репозитория — память "+
			"не работает, и гейт упрётся в предел реестра", tags)
	}
	if manifests != 3 {
		t.Errorf("манифест спрошен %d раз вместо трёх — память перестала различать ссылки, а "+
			"это уже послабление: вердикт одной выдаётся за вердикт другой", manifests)
	}

	// (б) ЗАКОННЫЙ БЛИЗНЕЦ ОБРАТНОЙ СТОРОНЫ: репозиторий НЕ читается. Каждая его
	//     ссылка обязана дать «не выполнилось», а не только первая, — иначе
	//     память превратила бы контроль в однократный.
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasPrefix(req.URL.Path, "/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"injected"}`))
		case strings.Contains(req.URL.Path, "/tags/list"):
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer closed.Close()

	probe = newRegistryProbe(client, registryEndpoints{Token: closed.URL + "/token", API: closed.URL + "/v2/"})
	for _, tag := range []string{"main-1", "main-2"} {
		got, why := probe.askRegistry("docker.io/prorobotech/kaname:" + tag)
		if got != answerUnresolved {
			t.Errorf("%s: ответ %d при нечитаемом репозитории — 404 на манифесте выдан за "+
				"отсутствие тега (%s)", tag, got, why)
		}
	}
}
