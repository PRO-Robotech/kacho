// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// newman_body_contract_test.go — гейт: ни один REST-запрос регрессионных
// suite'ов не шлёт краю ключ, которого нет в message запроса.
//
// Зачем гейт, а не разовый обход. Ключ, которого сервер не читает, ничем не
// выдаёт себя на прогоне: край отвечает `200`, кейс зеленеет, а поле не
// применено. Так уже случалось — редизайн переименовал поле запроса, устаревшие
// фикстуры продолжали слать старый ключ и продолжали получать `200`, ресурс
// создавался без данных, а разбираться пришлось в соседнем сервисе, куда
// приехал каскад. Единственный способ увидеть этот класс — сверять
// отправляемое с контрактом статически.
//
// Вердикт ставится по СОДЕРЖИМОМУ: тест падает счётчиком находок и печатает их
// все. Гейт, который печатает и выходит с нулём, сам был бы экземпляром того
// класса, который здесь искореняется.

// varPlaceholder — подстановка `{{runId}}`-переменных Postman. Значения на
// контракт не влияют: гейт сверяет ИМЕНА ключей, поэтому любая подстановка,
// оставляющая JSON разбираемым, годится.
var varPlaceholder = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// newmanFinding — находка с координатами в файле коллекции.
type newmanFinding struct {
	bodyFinding
	File string
	Line int
	Case string
	// CaseID — идентификатор кейса (`DISK-METHOD-PUT-NOT-ALLOWED`): им ведётся
	// поимённый список проб, которым тело в никуда оставлено намеренно.
	CaseID string
}

func (f newmanFinding) String() string {
	return fmt.Sprintf("%s:%d [%s] %s", f.File, f.Line, f.Case, f.bodyFinding.String())
}

// newmanRequest — один запрос коллекции, извлечённый для разбора.
type newmanRequest struct {
	name string
	// caseID — идентификатор кейса, которому принадлежит запрос: имя папки
	// верхнего уровня до « — ».
	caseID string
	method string
	path   string
	// bodyRaw — тело как оно лежит в коллекции (с `{{...}}`), пусто если тела нет.
	bodyRaw string
	line    int
}

// absentRouteBodyWaiver — поимённое разрешение нести тело в маршрут, которого
// край не обслуживает.
//
// Правило, которое оно ослабляет: «маршрута нет ⇒ тела быть не может». Запрос в
// пару (метод, путь), не обслуживаемую краем, контракта запроса не имеет —
// сверять его тело не с чем. Раньше из этого следовало, что такой запрос выпадал
// из вердикта ЦЕЛИКОМ, и вместе с ним — право нести какое угодно тело. Фикстура,
// нацеленная в несуществующий маршрут по ошибке, не была видна ни одному гейту:
// «сравнивать не с чем» тихо превращалось в «не проверяем».
//
// Пробы ниже целятся в отсутствующий маршрут НАМЕРЕННО — отсутствие маршрута и
// есть их предмет. Ключ — идентификатор кейса И пара (метод, путь): та же проба,
// переехавшая на другой маршрут, снова попадает в вердикт.
//
// Список печатается КАЖДЫЙ прогон и проверяется на устаревание: разрешение,
// которое некому применить, роняет гейт. Иначе он тихо копил бы право, под
// которым потом въехала бы чужая ошибка.
var absentRouteBodyWaiver = map[string]string{
	// `PUT` на списочный эндпоинт ресурса. Предмет пробы — что коллекция
	// ресурсов не заменяется целиком: у Kachō `PUT` не биндится нигде, замена
	// набора выражается адресными `Create`/`Delete`. Тело тут — ровно то, что
	// послал бы клиент, считающий иначе, и без него проба перестала бы
	// воспроизводить то заблуждение, которое проверяет.
	// Конформанс единого фасада (#59). Восемь адресов ниже НЕ ОБСЛУЖИВАЮТСЯ краем, и
	// это ровно то, что пробы утверждают: чеканка не имеет REST-двери, поверхности
	// провайдера подписи через край не достижимы. Тело у них настоящее — отказ форме
	// не отличался бы от отказа адресу, — а рядом всегда стоит контроль на заведомо
	// бессмысленном пути, с ответом которого отказ и сравнивается.
	"IBT-06-BOOTSTRAP-MINT-HAS-NO-REST-DOOR POST /kaname.cloud.iam.v1.InternalBootstrapTokenService/MintBootstrapToken": "конформанс фасада: предмет пробы — что у чеканки НЕТ REST-двери ни на одном слушателе. Тело настоящее намеренно: без него отказ был бы отказом форме, а проба обязана свидетельствовать, что недостижим сам адрес. Рядом в кейсе стоит контроль на заведомо бессмысленном пути — он и делает утверждение различающим: отказ обязан быть ТЕМ ЖЕ, что у опечатки",
	"IBT-06-BOOTSTRAP-MINT-HAS-NO-REST-DOOR POST /iam/v1/internal/bootstrapToken:mint":                                 "конформанс фасада: предмет пробы — что у чеканки НЕТ REST-двери ни на одном слушателе. Тело настоящее намеренно: без него отказ был бы отказом форме, а проба обязана свидетельствовать, что недостижим сам адрес. Рядом в кейсе стоит контроль на заведомо бессмысленном пути — он и делает утверждение различающим: отказ обязан быть ТЕМ ЖЕ, что у опечатки",
	"IBT-06-BOOTSTRAP-MINT-HAS-NO-REST-DOOR POST /ibt-nonsense-{{runId}}":                                              "конформанс фасада: ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к отрицанию выше — заведомо бессмысленный путь. Он обязан быть непривязанным: именно с его ответом сравнивается отказ на настоящем адресе, и без этой пары отрицание не отличило бы «нет двери» от «есть, но отказала»",
	"IBT-15-PROVIDER-SURFACES-NOT-REACHABLE-THROUGH-THE-EDGE POST /admin/clients":                                      "конформанс фасада: предмет пробы — что поверхности провайдера подписи не обслуживаются краем, то есть фасад нельзя обойти маршрутом. Адрес не привязан by construction, и это утверждаемое свойство, а не промах кейса; тело — ровно то, что послал бы клиент, считающий иначе",
	"IBT-15-PROVIDER-SURFACES-NOT-REACHABLE-THROUGH-THE-EDGE POST /oauth2/token":                                       "конформанс фасада: предмет пробы — что поверхности провайдера подписи не обслуживаются краем, то есть фасад нельзя обойти маршрутом. Адрес не привязан by construction, и это утверждаемое свойство, а не промах кейса; тело — ровно то, что послал бы клиент, считающий иначе",
	"IBT-15-PROVIDER-SURFACES-NOT-REACHABLE-THROUGH-THE-EDGE POST /ibt-nonsense2-{{runId}}":                            "конформанс фасада: ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к отрицанию выше — заведомо бессмысленный путь. Он обязан быть непривязанным: именно с его ответом сравнивается отказ на настоящем адресе, и без этой пары отрицание не отличило бы «нет двери» от «есть, но отказала»",
	"IAM-INT-NEG-EXT-UNBOUND-NEVER-SUCCEEDS POST /kaname.cloud.iam.v1.InternalIAMService/ForceLogout":                   "proba ban #6: путь — полное имя gRPC-метода, у которого REST-привязки НЕТ по построению, и в этом её предмет. Тело — настоящий запрос: без него край отверг бы пустышку и проба перестала бы свидетельствовать, что недостижим именно метод, а не форма",
	"IAM-INT-NEG-EXT-UNBOUND-NEVER-SUCCEEDS POST /kaname.cloud.iam.v1.InternalSessionRevocationsService/IsRevoked":      "proba ban #6: путь — полное имя gRPC-метода, у которого REST-привязки НЕТ по построению, и в этом её предмет. Тело — настоящий запрос: без него край отверг бы пустышку и проба перестала бы свидетельствовать, что недостижим именно метод, а не форма",
	"IAM-INT-NEG-EXT-UNBOUND-NEVER-SUCCEEDS POST /kaname.cloud.iam.v1.InternalSessionRevocationsService/Revoke":         "proba ban #6: путь — полное имя gRPC-метода, у которого REST-привязки НЕТ по построению, и в этом её предмет. Тело — настоящий запрос: без него край отверг бы пустышку и проба перестала бы свидетельствовать, что недостижим именно метод, а не форма",
	"IAM-INT-NEG-EXT-UNBOUND-NEVER-SUCCEEDS POST /kaname.cloud.iam.v1.InternalSessionRevocationsService/ListByUser":     "proba ban #6: путь — полное имя gRPC-метода, у которого REST-привязки НЕТ по построению, и в этом её предмет. Тело — настоящий запрос: без него край отверг бы пустышку и проба перестала бы свидетельствовать, что недостижим именно метод, а не форма. Глагол добавлен #1140: он один из трёх у этой службы НЕСЁТ в ответе сведения о человеке (какие его сессии сняли и когда), и до тех пор единственный из трёх здесь отсутствовал",
	"ADR-METHOD-PUT-NOT-ALLOWED PUT /vpc/v1/addresses":                                                                 "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",
	"GW-METHOD-PUT-NOT-ALLOWED PUT /vpc/v1/gateways":                                                                   "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",
	"NET-METHOD-PUT-NOT-ALLOWED PUT /vpc/v1/networks":                                                                  "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",
	"RT-METHOD-PUT-NOT-ALLOWED PUT /vpc/v1/routeTables":                                                                "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",
	"SG-METHOD-PUT-NOT-ALLOWED PUT /vpc/v1/securityGroups":                                                             "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",
	"SUB-METHOD-PUT-NOT-ALLOWED PUT /vpc/v1/subnets":                                                                   "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",
	"LST-METHOD-PUT-NOT-ALLOWED PUT /nlb/v1/listeners":                                                                 "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",
	"NLB-METHOD-PUT-NOT-ALLOWED PUT /nlb/v1/networkLoadBalancers":                                                      "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",
	"TGR-METHOD-PUT-NOT-ALLOWED PUT /nlb/v1/targetGroups":                                                              "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",
	"TGT-METHOD-PUT-NOT-ALLOWED PUT /nlb/v1/targetGroups":                                                              "PUT-на-список: тело воспроизводит заблуждение «замени коллекцию», которое проба и отвергает",

	// Internal-RPC, постучавшийся в публичный край (ban #6). Предмет пробы —
	// что метода на внешнем эндпоинте НЕТ. Тело — настоящий запрос этого RPC:
	// проба обязана быть неотличима от полноценного вызова, иначе она проверяла
	// бы отказ на пустышке, а не на том, чем воспользовался бы атакующий.
}

func TestNewmanCollectionsSendNoUnknownRequestFields(t *testing.T) {
	root := repoRoot(t)
	files, err := treecorpus.Glob(filepath.Join(root, "services", "*", "tests", "newman", "collections", "*.json"))
	if err != nil {
		t.Fatalf("glob collections: %v", err)
	}
	if len(files) == 0 {
		// Пустой набор входов — это НЕ «зелено»: гейт, которому нечего
		// проверять, ничего и не доказывает.
		t.Fatal("no newman collections found: gate has nothing to check, which is a failure, not a pass")
	}

	var findings []newmanFinding
	requests := 0
	bodies := 0

	for _, file := range files {
		reqs, err := parseNewmanCollection(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		rel := mustRel(root, file)
		for _, r := range reqs {
			requests++
			if r.bodyRaw == "" {
				continue
			}
			bodies++
			obj, err := decodeTemplatedJSONObject(r.bodyRaw)
			if err != nil {
				findings = append(findings, newmanFinding{
					bodyFinding: bodyFinding{
						Kind: "body-unparsed", Method: r.method, Path: r.path,
						Key: err.Error(),
					},
					File: rel, Line: r.line, Case: r.name, CaseID: r.caseID,
				})
				continue
			}
			if obj == nil {
				// Тело — не JSON-объект (массив/скаляр). Именованных ключей нет.
				continue
			}
			for _, f := range analyzeRequestBody(r.method, r.path, obj) {
				findings = append(findings, newmanFinding{bodyFinding: f, File: rel, Line: r.line, Case: r.name, CaseID: r.caseID})
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	// Разделение вердикта по предмету.
	//
	// mis-addressed — тело адресовано RPC, который его не читает. Это ТОТ САМЫЙ
	// класс: край отвечает `200`, поле не применено.
	//
	// unattributed — (метод, путь) не обслуживается краем ВООБЩЕ: grpc-gateway
	// отвечает 404/405, до разбора тела дело не доходит, контракта запроса не
	// существует. Сверять КЛЮЧИ такого тела не с чем — и это по-прежнему так.
	// Но из «нечего сверять» не следует «можно слать что угодно»: маршрута нет,
	// значит и тела быть не должно. Проверяемо статически, ложных срабатываний
	// по построению не даёт, и именно эта щель делала невидимой фикстуру,
	// нацеленную в несуществующий маршрут по ошибке.
	//
	// Намеренные пробы «этого метода на публичном краю нет» перечислены
	// поимённо в absentRouteBodyWaiver и печатаются каждый прогон.
	//
	// Гарантию, что в unattributed не проваливается РЕАЛЬНЫЙ маршрут, даёт
	// отдельная проверка TestHTTPBindingsMatchGeneratedRouteTable.
	var misAddressed, unattributed []newmanFinding
	for _, f := range findings {
		if f.Kind == routeUnresolved {
			unattributed = append(unattributed, f)
			continue
		}
		misAddressed = append(misAddressed, f)
	}

	waived, unwaived, usedWaivers := splitAbsentRouteBodies(unattributed)

	t.Logf("scanned %d collections, %d requests, %d with a JSON body; %d mis-addressed, %d body-on-absent-route (%d waived by name, %d not)",
		len(files), requests, bodies, len(misAddressed), len(unattributed), len(waived), len(unwaived))

	// Разрешения печатаются ВСЕГДА и целиком: молчаливое исключение — это то же
	// «не проверяем», только записанное в коде гейта.
	var w strings.Builder
	fmt.Fprintf(&w, "%d probe(s) carry a body into a route the edge does not serve, waived by name:\n", len(waived))
	for _, f := range waived {
		fmt.Fprintf(&w, "  %s\n      reason: %s\n", f, absentRouteBodyWaiver[absentRouteWaiverKey(f.CaseID, f.Method, f.Path)])
	}
	t.Log(w.String())

	// Устаревшее разрешение — тихо накопленное право. Следующая ошибочная
	// фикстура въехала бы под его именем, и гейт не сказал бы ни слова.
	var stale []string
	for key := range absentRouteBodyWaiver {
		if !usedWaivers[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)

	if len(misAddressed) == 0 && len(unwaived) == 0 && len(stale) == 0 {
		return
	}
	var b strings.Builder
	if len(misAddressed) > 0 {
		fmt.Fprintf(&b, "%d request(s) send the edge a body key it does not read:\n", len(misAddressed))
		for _, f := range misAddressed {
			fmt.Fprintf(&b, "  %s\n", f)
		}
	}
	if len(unwaived) > 0 {
		fmt.Fprintf(&b, "%d request(s) carry a body into a (method, path) the edge does not serve — either the route is wrong, or the body is dead weight; name the probe in absentRouteBodyWaiver if it is deliberate:\n", len(unwaived))
		for _, f := range unwaived {
			fmt.Fprintf(&b, "  %s\n      waiver key: %q\n", f, absentRouteWaiverKey(f.CaseID, f.Method, f.Path))
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(&b, "%d waiver(s) in absentRouteBodyWaiver match nothing — delete them, an unused permission is one the next mistake inherits:\n", len(stale))
		for _, key := range stale {
			fmt.Fprintf(&b, "  %q\n", key)
		}
	}
	t.Fatal(b.String())
}

// absentRouteWaiverKey — ключ поимённого разрешения: идентификатор кейса плюс
// пара (метод, путь). Переезд пробы на другой маршрут ключ меняет, и проба
// возвращается в вердикт.
func absentRouteWaiverKey(caseID, method, path string) string {
	return caseID + " " + method + " " + path
}

// splitAbsentRouteBodies делит тела в никуда на разрешённые поимённо и нет,
// попутно отмечая, какие разрешения были применены.
func splitAbsentRouteBodies(all []newmanFinding) (waived, unwaived []newmanFinding, used map[string]bool) {
	used = make(map[string]bool, len(absentRouteBodyWaiver))
	for _, f := range all {
		key := absentRouteWaiverKey(f.CaseID, f.Method, f.Path)
		if _, ok := absentRouteBodyWaiver[key]; ok {
			used[key] = true
			waived = append(waived, f)
			continue
		}
		unwaived = append(unwaived, f)
	}
	return waived, unwaived, used
}

// decodeTemplatedJSONObject разбирает тело Postman-запроса, подставив вместо
// `{{var}}` нейтральный литерал. Возвращает (nil, nil), если тело — валидный
// JSON, но не объект.
func decodeTemplatedJSONObject(raw string) (map[string]any, error) {
	substituted := varPlaceholder.ReplaceAllString(raw, "X")
	var v any
	if err := json.Unmarshal([]byte(substituted), &v); err != nil {
		return nil, fmt.Errorf("body is not parseable JSON after {{var}} substitution: %w", err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, nil
	}
	return obj, nil
}

// parseNewmanCollection извлекает все запросы коллекции вместе с номером строки
// их тела (для адресуемости находки).
func parseNewmanCollection(path string) ([]newmanRequest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Item []json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("parse collection: %w", err)
	}
	lines := lineIndex(string(content))
	var out []newmanRequest
	var walk func(items []json.RawMessage, folder string) error
	walk = func(items []json.RawMessage, folder string) error {
		for _, raw := range items {
			var node struct {
				Name    string            `json:"name"`
				Item    []json.RawMessage `json:"item"`
				Request *struct {
					Method string          `json:"method"`
					URL    json.RawMessage `json:"url"`
					Body   *struct {
						Mode string `json:"mode"`
						Raw  string `json:"raw"`
					} `json:"body"`
				} `json:"request"`
			}
			if err := json.Unmarshal(raw, &node); err != nil {
				return err
			}
			if len(node.Item) > 0 {
				// Кейс newman — папка верхнего уровня; её имя и есть адрес
				// находки для того, кто пойдёт чинить.
				next := folder
				if next == "" {
					next = node.Name
				}
				if err := walk(node.Item, next); err != nil {
					return err
				}
				continue
			}
			if node.Request == nil {
				continue
			}
			urlRaw, err := requestURL(node.Request.URL)
			if err != nil {
				return fmt.Errorf("request %q: %w", node.Name, err)
			}
			name := node.Name
			if folder != "" {
				name = folder + " / " + node.Name
			}
			req := newmanRequest{
				name:   name,
				caseID: caseIDOf(folder),
				method: strings.ToUpper(node.Request.Method),
				path:   restPath(urlRaw),
			}
			if node.Request.Body != nil && node.Request.Body.Mode == "raw" {
				req.bodyRaw = node.Request.Body.Raw
				req.line = lines.lineOf(node.Request.Body.Raw)
			}
			out = append(out, req)
		}
		return nil
	}
	if err := walk(doc.Item, ""); err != nil {
		return nil, err
	}
	return out, nil
}

// caseIDOf выделяет идентификатор кейса из имени его папки: `newman`-папка
// называется «`<ID>` — `<описание>`», и адресуется кейс именно идентификатором
// (он же в CASES-INDEX). Описание — проза, оно меняется; идентификатор — нет.
func caseIDOf(folder string) string {
	if i := strings.Index(folder, " — "); i >= 0 {
		return strings.TrimSpace(folder[:i])
	}
	return strings.TrimSpace(folder)
}

// requestURL достаёт строку URL: Postman хранит её либо объектом с полем `raw`,
// либо просто строкой.
func requestURL(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("request has no url")
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}
	var asObject struct {
		Raw string `json:"raw"`
	}
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return "", fmt.Errorf("url is neither string nor object: %w", err)
	}
	if asObject.Raw == "" {
		return "", fmt.Errorf("url object has empty raw")
	}
	return asObject.Raw, nil
}

// restPath отрезает базу (`{{baseUrl}}` / `{{internalBaseUrl}}` / `http://host`)
// и query-строку, оставляя REST-путь.
func restPath(url string) string {
	if i := strings.Index(url, "://"); i >= 0 {
		if j := strings.IndexByte(url[i+3:], '/'); j >= 0 {
			url = url[i+3+j:]
		} else {
			url = "/"
		}
	} else if strings.HasPrefix(url, "{{") {
		if j := strings.Index(url, "}}"); j >= 0 {
			url = url[j+2:]
		}
	}
	if i := strings.IndexByte(url, '?'); i >= 0 {
		url = url[:i]
	}
	if i := strings.IndexByte(url, '#'); i >= 0 {
		url = url[:i]
	}
	if !strings.HasPrefix(url, "/") {
		url = "/" + url
	}
	return url
}

// lineIdx отображает JSON-строковый литерал обратно в номер строки файла.
type lineIdx struct {
	content string
	// starts[i] — байтовое смещение начала строки i+1.
	starts []int
}

func lineIndex(content string) lineIdx {
	starts := []int{0}
	for i, r := range content {
		if r == '\n' {
			starts = append(starts, i+1)
		}
	}
	return lineIdx{content: content, starts: starts}
}

// lineOf возвращает номер строки, на которой лежит JSON-энкодинг value, либо 0.
func (l lineIdx) lineOf(value string) int {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	idx := strings.Index(l.content, string(encoded))
	if idx < 0 {
		return 0
	}
	return sort.SearchInts(l.starts, idx+1)
}

// repoRoot поднимается от пакета до корня репозитория (там лежит go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root (go.mod) not found above the package directory")
		}
		dir = parent
	}
}

func mustRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

// TestNewmanCollectionsDoNotDoublePrefixTheRoute — узкий гейт на ОДИН класс:
// путь, который не резолвится, но резолвится после снятия первого сегмента.
//
// # Почему именно так, а не «любой нерезолвящийся путь»
//
// Широкая форма была написана первой и на дереве дала 59 находок при 6044
// безтелесных запросах — и почти все законны: служебные маршруты (`/healthz`,
// `/.well-known/jwks.json`, `/oauth2/token`), прямые gRPC-пути
// (`/kacho.cloud.storage.v1.InternalVolumeService/Attach`) и намеренно мусорный
// ввод (завершающий слеш, пустой сегмент). Отделять их пришлось бы перечнем, а
// перечень стареет молча — ровно тот дефект, который эта волна уже чинила в
// пробах очереди прав.
//
// Узкая форма ложных срабатываний не даёт by construction: если путь начинает
// резолвиться ПОСЛЕ снятия первого сегмента, значит сегмент лишний. Служебный
// маршрут так себя не ведёт, мусорный ввод — тоже.
//
// # Что это стоило
//
// Кейс состояния применения написал `/vpc/v1/operations/{id}`, тогда как
// маршрут операции объявлен контрактом БЕЗ префикса сервиса. Край ответил 404,
// и упали ВСЕ три утверждения шага — включая те, что о маршруте не говорят
// вовсе («операция завершена без ошибки», «ресурс поля не несёт»). По их именам
// предмет не читался, а локальный прогон таких проб не исполняет: диагноз стоил
// полного прогона стенда.
func TestNewmanCollectionsDoNotDoublePrefixTheRoute(t *testing.T) {
	root := repoRoot(t)
	files, err := treecorpus.Glob(filepath.Join(root, "services", "*", "tests", "newman", "collections", "*.json"))
	if err != nil {
		t.Fatalf("glob collections: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no newman collections found: gate has nothing to check, which is a failure, not a pass")
	}

	type finding struct {
		File, Case, Method, Path, Without string
		Line                              int
	}
	var found []finding
	requests, checked := 0, 0

	for _, file := range files {
		reqs, perr := parseNewmanCollection(file)
		if perr != nil {
			t.Fatalf("%s: %v", file, perr)
		}
		rel := mustRel(root, file)
		for _, r := range reqs {
			requests++
			if _, ok := resolveHTTPBinding(r.method, r.path); ok {
				continue
			}
			// Снимаем один и два первых сегмента: префикс сервиса — это ДВА
			// сегмента (`/vpc/v1`), и проверка одного оставила бы гейт зелёным
			// на собственном предмете. Первая редакция так и делала, и это
			// показала инъекция, а не чтение кода.
			var shorter string
			for _, drop := range []int{1, 2} {
				rest := strings.TrimPrefix(r.path, "/")
				cut := rest
				okCut := true
				for i := 0; i < drop; i++ {
					j := strings.Index(cut, "/")
					if j < 0 {
						okCut = false
						break
					}
					cut = cut[j+1:]
				}
				if !okCut {
					continue
				}
				if _, ok := resolveHTTPBinding(r.method, "/"+cut); ok {
					shorter = "/" + cut
					break
				}
			}
			checked++
			if shorter == "" {
				continue
			}
			found = append(found, finding{
				File: rel, Case: r.name, Method: r.method,
				Path: r.path, Without: shorter, Line: r.line,
			})
		}
	}

	// Перепись — отдельное утверждение: «ноль находок» обязано быть отличимо
	// от «ноль прочитанного».
	t.Logf("осмотрено коллекций %d, запросов %d; нерезолвящихся с проверенным укорочением %d; находок %d",
		len(files), requests, checked, len(found))

	if requests == 0 {
		t.Fatal("ни одного запроса не прочитано — гейт смотрит не туда")
	}
	if len(found) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d запрос(ов) несут ЛИШНИЙ первый сегмент пути: без него маршрут резолвится, с ним край отвечает 404 "+
		"и падают все утверждения шага, включая те, что о маршруте не говорят:\n", len(found))
	for _, f := range found {
		fmt.Fprintf(&b, "  %s:%d  %s\n      %s %s\n      резолвится как: %s %s\n",
			f.File, f.Line, f.Case, f.Method, f.Path, f.Method, f.Without)
	}
	t.Fatal(b.String())
}
