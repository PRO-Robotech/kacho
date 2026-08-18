// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// foundationadoption_injection_test.go — доказательство того, что перепись
// усыновления УМЕЕТ краснеть и УМЕЕТ молчать.
//
// Инъекция в обе стороны по каждому свойству, которое гейт держит. Без второй
// стороны проба ловила бы форму, а не существо, и первый же ложный срабат её
// отключил бы.
//
//	краснеет  · слушатель снял провязку — находка НАЗЫВАЕТ ЕГО ИМЯ
//	молчит    · тот же слушатель через посредника
//	краснеет  · пропуск, чья возможность уже усыновлена
//	краснеет  · запись «предмета нет», опровергнутая усыновлением
//	краснеет  · запись про слушателя, которого в дереве нет
//	краснеет  · пропуск без номера задачи
//	краснеет  · запись «не несёт ни один» при первом усыновившем
//	краснеет  · заявление посредника, пережившее предмет
//	молчит    · заявление посредника, у которого предмет есть
//	краснеет  · возможность, объявленная обязательной и отсутствующая в фундаменте
//	краснеет  · слушателей ноль — перепись беспредметна
//	молчит    · упоминание возможности в КОММЕНТАРИИ усыновлением не считается —
//	            и это проверено на НАСТОЯЩЕМ файле дерева, а не на синтетике
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// injCap — возможность-функция для синтетики. Форма та же, что у настоящих:
// пакет большой, признак — вызванный символ.
func injCap() FoundationCapability {
	return FoundationCapability{
		Name: "звено", Pkg: "pkg/fake",
		Symbols: []string{"fake.Wire"},
	}
}

// injTree раскладывает синтетическое дерево: фундамент, посредник и слушателей.
// Тела файлов подаются как есть — гейт разбирает их синтаксисом.
func injTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// injBase — дерево, в котором ОБА слушателя усыновили возможность: один своей
// проводкой, второй через посредника. Это тот самый законный близнец, на котором
// гейт обязан молчать.
func injBase() map[string]string {
	return map[string]string{
		// Фундамент: возможность объявлена здесь.
		"pkg/fake/wire.go": "package fake\n\nfunc Wire() int { return 1 }\n",
		// Посредник: ставит возможность за вызывающего.
		"pkg/host/serve.go": "package host\n\nimport \"x/pkg/fake\"\n\n" +
			"func Serve() int { return fake.Wire() }\n",
		// Слушатель A — своей проводкой.
		"services/a/main.go": "package main\n\nimport (\n\t\"google.golang.org/grpc\"\n\t\"x/pkg/fake\"\n)\n\n" +
			"func main() { _ = grpc.NewServer(); _ = fake.Wire() }\n",
		// Слушатель B — через посредника.
		"services/b/main.go": "package main\n\nimport \"x/pkg/host\"\n\n" +
			"func main() { _ = host.Serve() }\n",
	}
}

func injRoster() FoundationRoster {
	return FoundationRoster{
		Capabilities: []FoundationCapability{injCap()},
		Providers: []FoundationProvider{
			{Name: "pkg/host", Entry: "host.Serve", Carries: []string{"звено"}},
		},
	}
}

// injRun прогоняет перепись по синтетическому дереву и отдаёт её итог.
func injRun(t *testing.T, root string, r FoundationRoster) FoundationCensus {
	t.Helper()
	listeners, scans, _, err := DiscoverListeners(root, []string{"grpc.NewServer", "host.Serve"})
	if err != nil {
		t.Fatalf("перепись слушателей: %v", err)
	}
	ps := map[string]*FoundationScan{}
	for _, p := range r.Providers {
		s, serr := ScanGoTree(filepath.Join(root, filepath.FromSlash(p.Name)))
		if serr != nil {
			t.Fatalf("посредник %s: %v", p.Name, serr)
		}
		ps[p.Name] = s
	}
	return r.Adjudicate(listeners, scans, ps)
}

// TestFoundationGateStaysSilentOnLegitimateAdoption — ЗАКОННЫЙ БЛИЗНЕЦ.
//
// Оба слушателя усыновили, и второй сделал это ЧЕРЕЗ ПОСРЕДНИКА — то есть имени
// возможности в его каталоге нет вовсе. Именно на нём наивный предикат («упомянул
// ли слушатель») дал бы находку, и она была бы ложной.
func TestFoundationGateStaysSilentOnLegitimateAdoption(t *testing.T) {
	cen := injRun(t, injTree(t, injBase()), injRoster())
	if len(cen.Listeners) != 2 {
		t.Fatalf("вход построен неверно: слушателей %d, а нужно 2 — без обоих проба "+
			"не различает свою и чужую проводку", len(cen.Listeners))
	}
	if cen.Carried != 2 || len(cen.Findings) != 0 || len(cen.Stale) != 0 {
		t.Fatalf("гейт краснеет на законном усыновлении: %s\nнаходки: %v\nистёкшие: %v",
			cen, cen.Findings, cen.Stale)
	}
	t.Logf("законный близнец: %s", cen)
}

// TestFoundationGateNamesTheListenerThatDroppedTheWiring — КРАСНЕЕТ И НАЗЫВАЕТ.
//
// Снимаем провязку у одного из двух. Второй остаётся усыновившим — иначе проба
// не отличала бы «нашёл виновника» от «покраснел на всём».
func TestFoundationGateNamesTheListenerThatDroppedTheWiring(t *testing.T) {
	files := injBase()
	files["services/a/main.go"] = "package main\n\nimport \"google.golang.org/grpc\"\n\n" +
		"func main() { _ = grpc.NewServer() }\n"

	cen := injRun(t, injTree(t, files), injRoster())
	if len(cen.Findings) != 1 {
		t.Fatalf("снятая провязка не дала ровно одной находки: %s\nнаходки: %v", cen, cen.Findings)
	}
	f := cen.Findings[0]
	if f.Listener != "services/a" {
		t.Fatalf("находка называет %q, а провязку снял services/a: координата уводит не туда", f.Listener)
	}
	if !strings.Contains(f.Detail, "services/a") || !strings.Contains(f.Detail, "звено") {
		t.Fatalf("текст находки не называет ни слушателя, ни возможность: %q", f.Detail)
	}
	if cen.Carried != 1 {
		t.Fatalf("второй слушатель перестал считаться усыновившим: %s — проба покраснела на "+
			"всём и виновника не различает", cen)
	}
	t.Logf("находка: %s", f.Detail)
}

// TestFoundationLedgerEntriesExpireOnTheirOwn — САМОИСТЕЧЕНИЕ обеих ведомостей.
//
// Каждая форма записи проверяется отдельно: одна проверка на все пять сразу не
// отличила бы «истекает пропуск» от «истекает что-нибудь».
func TestFoundationLedgerEntriesExpireOnTheirOwn(t *testing.T) {
	root := injTree(t, injBase()) // дерево, где ОБА усыновили

	cases := []struct {
		name   string
		mutate func(*FoundationRoster)
		want   string
	}{
		{
			name: "пропуску нечего исключать",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Listener: "services/a", Issue: 1}}
			},
			want: "больше нечего исключать",
		},
		{
			name: "запись «нет предмета» опровергнута усыновлением",
			mutate: func(r *FoundationRoster) {
				r.NoSubject = []FoundationNoSubject{{Capability: "звено", Listener: "services/b", Why: "нечего сужать"}}
			},
			want: "опровергнута деревом",
		},
		{
			name: "запись про слушателя, которого нет",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Listener: "services/zzz", Issue: 1}}
			},
			want: "такого в дереве нет",
		},
		{
			name: "запись про возможность, которой нет в наборе",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "чего-нибудь", Listener: "services/a", Issue: 1}}
			},
			want: "такой в наборе нет",
		},
		{
			name: "пропуск без номера задачи",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Listener: "services/a"}}
			},
			want: "не называет задачи",
		},
		{
			name: "отсутствие предмета без причины",
			mutate: func(r *FoundationRoster) {
				r.NoSubject = []FoundationNoSubject{{Capability: "звено", Listener: "services/a"}}
			},
			want: "не названо причиной",
		},
		{
			name: "запись «не несёт ни один» при первом усыновившем",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Issue: 1}}
			},
			want: "уже несёт",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := injRoster()
			tc.mutate(&r)
			cen := injRun(t, root, r)
			if len(cen.Stale) == 0 {
				t.Fatalf("запись, которой нечего исключать, не найдена: %s — значит ведомость "+
					"не самоистекает и переживёт свой предмет", cen)
			}
			joined := strings.Join(cen.Stale, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("истечение названо не тем: жду %q, получено:\n%s", tc.want, joined)
			}
		})
	}

	// Обратная сторона: ведомость, у которой предмет ЕСТЬ, молчит. Без этой
	// проверки все семь выше зеленели бы и на гейте, который краснеет на всякой
	// записи вообще.
	t.Run("живая запись молчит", func(t *testing.T) {
		files := injBase()
		files["services/a/main.go"] = "package main\n\nimport \"google.golang.org/grpc\"\n\n" +
			"func main() { _ = grpc.NewServer() }\n"
		r := injRoster()
		r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Listener: "services/a", Issue: 1, Why: "предмет есть"}}
		cen := injRun(t, injTree(t, files), r)
		if len(cen.Stale) != 0 || len(cen.Findings) != 0 {
			t.Fatalf("живой пропуск объявлен истёкшим: %s\nистёкшие: %v", cen, cen.Stale)
		}
		if cen.Excused != 1 {
			t.Fatalf("живой пропуск не засчитан записанным: %s", cen)
		}
	})
}

// TestFoundationProviderClaimIsVerifiedAgainstTheTree — предпосылка гейта.
func TestFoundationProviderClaimIsVerifiedAgainstTheTree(t *testing.T) {
	scan := func(t *testing.T, root, dir string) map[string]*FoundationScan {
		t.Helper()
		s, err := ScanGoTree(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			t.Fatal(err)
		}
		return map[string]*FoundationScan{dir: s}
	}

	t.Run("заявление с предметом молчит", func(t *testing.T) {
		root := injTree(t, injBase())
		r := injRoster()
		if bad := r.VerifyProviderClaims(scan(t, root, "pkg/host")); len(bad) != 0 {
			t.Fatalf("верное заявление объявлено устаревшим: %v", bad)
		}
	})

	t.Run("заявление пережило предмет — краснеет", func(t *testing.T) {
		files := injBase()
		// Посредник перестал ставить возможность, а заявление осталось.
		files["pkg/host/serve.go"] = "package host\n\nfunc Serve() int { return 0 }\n"
		root := injTree(t, files)
		r := injRoster()
		bad := r.VerifyProviderClaims(scan(t, root, "pkg/host"))
		if len(bad) != 1 || !strings.Contains(bad[0], "pkg/host") || !strings.Contains(bad[0], "звено") {
			t.Fatalf("устаревшее заявление не поймано либо названо без координаты: %v", bad)
		}
		t.Logf("%s", bad[0])
	})

	t.Run("носителем объявлено то, чего нет в наборе", func(t *testing.T) {
		root := injTree(t, injBase())
		r := injRoster()
		r.Providers[0].Carries = []string{"чего-нибудь"}
		bad := r.VerifyProviderClaims(scan(t, root, "pkg/host"))
		if len(bad) != 1 || !strings.Contains(bad[0], "такой возможности в наборе нет") {
			t.Fatalf("несуществующая возможность в заявлении не поймана: %v", bad)
		}
	})
}

// TestFoundationRosterRefusesCapabilityWithoutSubject — возможность, объявленная
// обязательной, обязана существовать и быть видимой.
func TestFoundationRosterRefusesCapabilityWithoutSubject(t *testing.T) {
	root := injTree(t, injBase())

	t.Run("настоящая возможность молчит", func(t *testing.T) {
		if bad := injRoster().VerifyCapabilities(root); len(bad) != 0 {
			t.Fatalf("существующая возможность объявлена отсутствующей: %v", bad)
		}
	})

	t.Run("каталога в фундаменте нет — краснеет", func(t *testing.T) {
		r := injRoster()
		r.Capabilities[0].Pkg = "pkg/нет-такого"
		bad := r.VerifyCapabilities(root)
		if len(bad) != 1 || !strings.Contains(bad[0], "в фундаменте нет") {
			t.Fatalf("отсутствующая возможность не поймана: %v", bad)
		}
	})

	t.Run("признака усыновления нет — краснеет", func(t *testing.T) {
		r := injRoster()
		r.Capabilities[0].Symbols = nil
		bad := r.VerifyCapabilities(root)
		if len(bad) != 1 || !strings.Contains(bad[0], "ни одного признака усыновления") {
			t.Fatalf("возможность без признака не поймана: %v", bad)
		}
	})
}

// TestFoundationCensusRefusesWhenThereAreNoListeners — «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
//
// Дерево без единого слушателя даёт нулевую перепись, и без этого отказа она
// была бы неотличима от «все всё усыновили».
func TestFoundationCensusRefusesWhenThereAreNoListeners(t *testing.T) {
	root := injTree(t, map[string]string{
		"pkg/fake/wire.go":  "package fake\n\nfunc Wire() int { return 1 }\n",
		"pkg/host/serve.go": "package host\n\nimport \"x/pkg/fake\"\n\nfunc Serve() int { return fake.Wire() }\n",
		// Каталог есть, исходники есть, слушателя не поднимает.
		"tools/lint/main.go": "package main\n\nfunc main() {}\n",
	})
	listeners, _, cand, err := DiscoverListeners(root, []string{"grpc.NewServer", "host.Serve"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 0 {
		t.Fatalf("каталог без слушателя признан слушателем: %v", listeners)
	}
	if len(cand) == 0 {
		t.Fatalf("кандидатов рассмотрено ноль — перепись не отличит «не нашли» от «не смотрели»")
	}
	t.Logf("кандидатов %d, слушателей 0 — перепись беспредметна и обязана отказать", len(cand))
}

// TestFoundationAdoptionIgnoresMentionsInComments — контроль на НАСТОЯЩЕМ дереве.
//
// В крае есть ровно та пара, ради которой признак читается синтаксисом:
// помощник, называющий звено ПРОЗОЙ в шапке, и композиционный корень, зовущий
// его по-настоящему. Текстовый предикат не отличил бы одно от другого, и гейт
// зеленел бы на собственном объяснении — записанный класс.
func TestFoundationAdoptionIgnoresMentionsInComments(t *testing.T) {
	root := repoRoot(t)
	recovery := FoundationCapability{
		Name: "восстановление после паники", Pkg: "pkg/grpcsrv",
		Symbols: []string{"grpcsrv.UnaryPanicRecovery", "grpcsrv.StreamPanicRecovery"},
	}

	mention := filepath.Join(root, "gateway", "internal", "middleware")
	body, err := os.ReadFile(filepath.Join(mention, "recovery.go"))
	if err != nil {
		t.Skipf("производителя входа в дереве больше нет (%v) — контроль на настоящем "+
			"файле построить не из чего", err)
	}
	if !strings.Contains(string(body), "UnaryPanicRecovery") {
		t.Fatalf("файл-производитель больше не упоминает звено: вход инъекции пережил свой " +
			"предмет, и контроль ничего не доказывает")
	}

	s, err := ScanGoTree(mention)
	if err != nil {
		t.Fatal(err)
	}
	if s.Direct(recovery) {
		t.Fatalf("упоминание в комментарии засчитано усыновлением: признак читает текст, " +
			"а не синтаксис, — гейт зеленел бы на объяснении рядом с провязкой")
	}

	// Положительный контроль: настоящий вызов в композиционном корне того же
	// края читается. Без него «не засчитал» было бы неотличимо от «не читает вовсе».
	real, err := ScanGoTree(filepath.Join(root, "gateway", "cmd", "api-gateway"))
	if err != nil {
		t.Fatal(err)
	}
	if !real.Direct(recovery) {
		t.Fatalf("настоящий вызов звена не прочитан: признак не читает НИЧЕГО, и отрицание " +
			"выше зеленеет на всём")
	}
	t.Logf("комментарий не засчитан, настоящий вызов прочитан: файлов в помощнике %d, "+
		"в корне %d", s.Files, real.Files)
}

// TestFoundationGateRedensOnTheRealTreeWhenAnEntryIsRemoved — инъекция
// НАСТОЯЩИМ входом, а не синтетикой.
//
// Синтетическое дерево доказывает механику. Оно не доказывает, что механика
// приложена к настоящему дереву верно: перечень слушателей, признаки
// возможностей и цепочка посредников — всё это могло разойтись с деревом, и
// синтетика этого не заметит, потому что построена по тем же представлениям.
//
// Поэтому вторая половина инъекции берёт БОЕВОЙ набор и снимает из его ведомости
// ОДНУ запись. Ожидание точное: находка ровно одна, и она называет того самого
// слушателя и ту самую возможность, которые запись прикрывала.
func TestFoundationGateRedensOnTheRealTreeWhenAnEntryIsRemoved(t *testing.T) {
	root := repoRoot(t)
	listeners, scans, _, err := DiscoverListeners(root, foundationServerMarkers)
	if err != nil {
		t.Fatal(err)
	}
	base := foundationRoster()
	ps := map[string]*FoundationScan{}
	for _, p := range base.Providers {
		s, serr := ScanGoTree(filepath.Join(root, filepath.FromSlash(p.Name)))
		if serr != nil {
			t.Fatal(serr)
		}
		ps[p.Name] = s
	}

	// Снимаемая запись выбирается по СОДЕРЖАНИЮ ведомости, а не выписывается
	// координатой: выписанная устарела бы вместе с первой же правкой набора, и
	// проба падала бы на своей фикстуре вместо предмета.
	victim := base.Ledger[0]
	trimmed := base
	trimmed.Ledger = append([]FoundationLedgerEntry(nil), base.Ledger[1:]...)

	full := base.Adjudicate(listeners, scans, ps)
	if len(full.Findings) != 0 || len(full.Stale) != 0 {
		t.Fatalf("боевой набор не зелёный — инъекция мерила бы разницу от красного: %s", full)
	}

	cut := trimmed.Adjudicate(listeners, scans, ps)
	if len(cut.Findings) == 0 {
		t.Fatalf("снятие записи %q не дало ни одной находки: ведомость ничего не прикрывает, "+
			"то есть перепись зелёная независимо от неё", victim.Capability)
	}
	for _, f := range cut.Findings {
		if f.Capability != victim.Capability {
			t.Fatalf("снята запись про %q, а находка про %q — перепись отвечает не про то, "+
				"что у неё спросили", victim.Capability, f.Capability)
		}
		if victim.Listener != "" && f.Listener != victim.Listener {
			t.Fatalf("снята запись про слушателя %q, а находка про %q", victim.Listener, f.Listener)
		}
	}
	t.Logf("снята запись %q/%s (задача #%d) → находок %d, первая: %s",
		victim.Capability, listenerOrAll(victim.Listener), victim.Issue,
		len(cut.Findings), cut.Findings[0].Detail)
}
