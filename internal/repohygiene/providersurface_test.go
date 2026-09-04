// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// providerSurfaceLedger — ВЕДОМОСТЬ: места прод-кода, которым сегодня разрешено
// говорить с внешним поставщиком удостоверений, и то, о чём каждое с ним
// говорит.
//
// Ведомость — НЕ послабление и не список прощённых. Послабление накрывает
// нарушение; здесь накрывать нечего: поставщик жив по решению, и разговор с ним
// законен, пока фаза Ф4 (задача #900) не сняла его целиком. Предмет ведомости —
// РОСТ поверхности, а не её наличие.
//
// Ведомость обязана СОКРАЩАТЬСЯ. Запись, которой больше нечего называть, —
// находка (ProviderFindingStale), поэтому снятие кода заставляет снять и запись:
// одно без другого не зеленеет.
//
// У каждой записи стоит `Until` — факт о дереве, при котором её снимают. Это не
// украшение: без него запись бессрочна, и снять её будет некому.
var providerSurfaceLedger = []ProviderLedgerEntry{
	{
		File:     "gateway/cmd/api-gateway/revocation_validation.go",
		Surfaces: []string{"/admin/oauth2/introspect"},
		Why: "страж старта края: путь назван КОНСТАНТОЙ, чтобы адрес, нацеленный на " +
			"публичный API вместо административного, был отвергнут при старте, а не " +
			"каждым запросом потом",
		Until: "край перестал принимать издателя-поставщика: полоса интроспекции " +
			"существует ровно ради ЕГО токенов, отзыв наших читается своей записью на " +
			"предъявлении (#797)",
	},
	{
		File:     "gateway/internal/middleware/auth_revocation.go",
		Surfaces: []string{"/admin/oauth2/introspect"},
		Why: "текст диагностики оператору: «ответило не то» отличается от «не ответило», " +
			"и назвать различие можно только назвав ожидаемый путь",
		Until: "снята полоса интроспекции у края (та же, что у стража старта выше)",
	},
	{
		File:     "gateway/internal/handler/logout_handler.go",
		Surfaces: []string{"/admin/oauth2/auth/sessions/login"},
		Why: "выход пользователя снимает сессию входа НА СТОРОНЕ ПОСТАВЩИКА: пока вход " +
			"человека идёт через него, выход, не снявший её, оставляет вход живым",
		Until: "вход человека перестал заводить сессию у поставщика",
	},
	{
		File:     "services/iam/internal/apps/kacho/config/authn.go",
		Surfaces: []string{"/oauth2/token"},
		Why:      "вывод адреса его токен-эндпоинта из объявленного издателя",
		Until: "ни один контур выдачи не обменивает утверждение у поставщика: сегодня " +
			"это ещё делает контур выдачи докер-токена на посадке БЕЗ своей чеканки " +
			"(`registrytokenwire.providerExchangeFor`). Бутстрап-удостоверение из этого " +
			"перечня выбыло — оно чеканится нами (#1119)",
	},
	{
		File:     "services/iam/internal/clients/hydra_login_sessions.go",
		Surfaces: []string{"/admin/oauth2/auth/sessions/login"},
		Why: "снятие сессий входа службой прав — принудительный выход и блокировка " +
			"учётной записи",
		Until: "тот же факт, что у обработчика выхода на крае",
	},
	{
		File:     "services/iam/internal/clients/hydra_oauth_clients.go",
		Surfaces: []string{"/admin/clients"},
		Why: "жизненный цикл зеркала клиента у поставщика: строка реестра наша, зеркало " +
			"его, и пока чеканит он — зеркало обязано заводиться и сниматься вместе с " +
			"нашей строкой",
		Until: "выдача переехала на свою чеканку по ВСЕМ контурам (`user_tokens`, " +
			"`sa_keys`, `interactive_client`), и зеркалу нечего отражать. " +
			"`bootstrap_token` из перечня выбыл: зеркала он не заводит (#1119)",
	},
}

// providerSurfaceExemptions — послабления гейта.
//
// Послабление — НЕ ведомость. Ведомость называет законный разговор с
// поставщиком; послабление снимает файл с рассмотрения целиком, и потому у него
// обязан быть предикат снятия и проба, что предмет ещё есть.
var providerSurfaceExemptions = []struct {
	// Prefix — путь либо его начало.
	Prefix string
	// Why — почему исключено.
	Why string
	// Until — при каком факте о дереве запись обязана быть снята.
	Until string
}{
	{
		Prefix: "internal/repohygiene/providersurface.go",
		Why: "здесь живёт САМ СЛОВАРЬ путей поставщика: гейт разбирает строковые " +
			"литералы, а словарь и есть перечень строковых литералов. Без послабления " +
			"гейт находит собственное объявление — то есть краснеет на исправном дереве " +
			"и снимается первым же обходом. Прятать словарь склейкой по частям нельзя: " +
			"проверка, спрятавшаяся от себя самой, перестаёт быть читаемой",
		Until: "словарь перестал быть перечнем строковых литералов — например, " +
			"переехал в отдельные данные, которых разбор исполняемой части не читает",
	},
}

func exemptFromProviderSurface(path string) bool {
	for _, e := range providerSurfaceExemptions {
		if strings.HasPrefix(path, e.Prefix) {
			return true
		}
	}
	return false
}

// providerSurfaceSources — непроверочное дерево Go, спрошенное У ИНДЕКСА.
//
// Обход диска не знает правил игнорирования и судит чужой рабочий каталог —
// произведённые файлы, чужие копии, остатки прогонов.
func providerSurfaceSources(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	sources := map[string]string{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || skipPath(rel) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		sources[rel] = string(b)
	}
	return sources
}

// TestProviderSurfaceIsBoundedByTheLedger — поверхность к внешнему поставщику
// удостоверений ограничена ведомостью, и это утверждение О ДЕРЕВЕ.
//
// Разбор класса и граница предиката — в шапке providersurface.go. Здесь только
// обход дерева и вердикт.
func TestProviderSurfaceIsBoundedByTheLedger(t *testing.T) {
	sources := providerSurfaceSources(t)

	findings, census, err := FindProviderSurface(sources, providerSurfaceLedger, exemptFromProviderSurface)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("осмотрено непроверочных файлов Go: %d; строковых литералов: %d; "+
		"мест разговора с поставщиком: %d в %d файлах; записей ведомости: %d "+
		"(объявлений поверхностей: %d); файлов, называющих поставщика ТОЛЬКО в прозе: %d",
		census.Files, census.Literals, census.Reaches, census.Carriers,
		census.LedgerEntries, census.LedgerSurfaces, census.ProseMentions)

	// Предпосылка гейта: он обязан ОТКАЗЫВАТЬ на беспредметности, а не молчать.
	// Ноль прочитанных файлов снаружи неотличим от «нарушений нет».
	if census.Files == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if census.Literals == 0 {
		t.Fatal("осмотрено ноль строковых литералов — разбор не дошёл до исполняемой части")
	}
	// Пустая ведомость при нуле мест — ЦЕЛЬ фазы Ф4, а не поломка: проба,
	// падающая на достижении собственной цели, подталкивает держать запись ради
	// зелёного. Поэтому предпосылкой требуется только прочитанное дерево.
	if census.Reaches == 0 && census.LedgerEntries == 0 {
		t.Log("мест разговора с поставщиком ноль и ведомость пуста — исход, к которому " +
			"ведёт задача #900")
	}

	for _, f := range findings {
		switch f.Kind {
		case ProviderFindingUnledgered:
			t.Errorf("%s:%d: разговор с внешним поставщиком по %q (%s) — %s.\n"+
				"Платформа переезжает на СВОЮ чеканку (эпик #896); поверхность к поставщику "+
				"обязана сокращаться, а не расти. Новое место либо не нужно — тогда его нет, "+
				"либо нужно — тогда оно названо в providerSurfaceLedger вместе с предикатом "+
				"своего снятия",
				f.File, f.Line, f.Surface, f.Detail, f.Kind)
		case ProviderFindingUndeclared:
			t.Errorf("%s:%d: %s — за этим местом объявлены другие поверхности, а оно просит "+
				"%q (%s).\nИменно так поверхность и растёт: «ещё один вызов туда же». "+
				"Объявите поверхность за местом либо не заводите вызов",
				f.File, f.Line, f.Kind, f.Surface, f.Detail)
		case ProviderFindingStale:
			t.Errorf("%s: %s — %s.\nВедомость обязана сокращаться вместе с кодом: запись, "+
				"которой нечего называть, молча разрешит следующий разговор в этом файле",
				f.File, f.Kind, f.Detail)
		default:
			t.Errorf("%s:%d: неизвестный вид находки %q", f.File, f.Line, f.Kind)
		}
	}
}

// TestProviderSurfaceExemptionsStillHaveASubject — послабление живёт, пока у него
// есть предмет.
//
// «Предмет» здесь — НЕ «под префиксом лежит файл». Такой предикат зеленел бы на
// послаблении, которому нечего исключать: файл существует, разговоров в нём нет,
// а запись стоит и молча накроет следующую слепую зону.
//
// Предмет — «без этой записи гейт нашёл бы ЗДЕСЬ находку». Поэтому разбор
// прогоняется БЕЗ послаблений, и от каждой записи требуется хотя бы одна
// находка под её префиксом.
func TestProviderSurfaceExemptionsStillHaveASubject(t *testing.T) {
	sources := providerSurfaceSources(t)

	bare, census, err := FindProviderSurface(sources, providerSurfaceLedger, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	t.Logf("разбор без послаблений: файлов %d, находок %d, послаблений объявлено %d",
		census.Files, len(bare), len(providerSurfaceExemptions))

	if len(providerSurfaceExemptions) == 0 {
		// Пустой перечень — цель, а не поломка.
		t.Log("послаблений ноль — исключать нечего, и это исход, к которому проба ведёт")
		return
	}
	for _, e := range providerSurfaceExemptions {
		covered := 0
		for _, f := range bare {
			if strings.HasPrefix(f.File, e.Prefix) {
				covered++
			}
		}
		if covered == 0 {
			t.Errorf("послабление %q не исключает НИ ОДНОЙ находки — предмета у него нет, "+
				"и оно обязано быть снято. Оставленное, оно молча накроет следующую слепую "+
				"зону.\nПричина записи: %s\nПредикат снятия: %s",
				e.Prefix, e.Why, e.Until)
			continue
		}
		t.Logf("послабление %q: находок под ним %d", e.Prefix, covered)
	}
}
