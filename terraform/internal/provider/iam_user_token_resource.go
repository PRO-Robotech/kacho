// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// iamUsersPath — коллекция пользователей. Нужна дважды: из неё строится путь токена
// (собственной коллекции верхнего уровня у токена нет) и по ней задаётся вопрос о самом
// владельце в Read.
//
// Второй вопрос НЕ независим, и полагаться на него как на подтверждение отсутствия нельзя:
// отказ в доступе к пользователю край отдаёт побайтово тем же «не найдено», что и настоящее
// удаление, — тем же механизмом сокрытия существования, что и на списке токенов. Ответ
// сужает набор объяснений, но ни одного из них не исключает (см. Read).
const iamUsersPath = "/iam/v1/users"

// userTokenDeniedCauses — почему край отказал в доступе на выпуске или отзыве личного
// токена.
//
// Текст ОДИН на все места, где вызывающий этот отказ увидит: описание ресурса,
// диагностика выпуска, диагностика отзыва. Три копии одного утверждения разошлись бы на
// первом же уточнении, и разошлись бы молча.
//
// Причин ровно ДВЕ, и чинятся они в разных местах: одна — личностью, под которой
// применяется конфигурация, другая — повторным входом человека. Назвать одну значит
// отправить половину читателей чинить не то.
//
// ЗДЕСЬ СТОЯЛО, ЧТО СЛУЖЕБНАЯ УЧЁТКА ПОРОГА НЕ ПРОХОДИТ, — и стояло сразу в трёх местах.
// Это неверно: машинный принципал освобождён от порога уровня доверия ПЕРВЫМ ЖЕ условием
// общего правила (pkg/grpcsrv/acr.go, EvaluateStepUp), и оба места энфорсмента — край и
// iam — приходят к вердикту через эту одну функцию. Текст объявлял препятствием то, чего
// не существует, отговаривая конвейер от полосы, ради которой машинная личность и
// заводится, — а НАСТОЯЩАЯ причина отказа не называлась вовсе. Комментарий,
// противоречащий коду, тем и опасен, что следующий «чинит» код под неверный текст: по
// этому объяснению уже искали причину отказа, и верная нашлась только потому, что
// неверную проверили.
const userTokenDeniedCauses = "Отказ в доступе приходит по одной из ДВУХ причин, и " +
	"чинятся они в разных местах.\n\n" +
	"1) ПРАВО. Выпускать и отзывать личные токены человека вправе ОН САМ. Это свойство " +
	"его строки личности, а не выдаваемое право: ни ролью, ни привязкой доступа его " +
	"получить нельзя — личный токен делает предъявителя самим человеком всюду, где " +
	"действует тот, поэтому право на выпуск никому не передаётся. Помимо владельца край " +
	"отвечает «да» только администратору облака. Значит конвейер, ходящий служебной " +
	"учёткой, выпустит личный токен лишь тогда, когда этой учётке выдано " +
	"администрирование облака; иначе применяйте конфигурацию под личностью самого " +
	"владельца — а машине заводите не личный токен, а ключ служебной учётки " +
	"(`kaname_service_account_key`), который для неинтерактивного входа и " +
	"предназначен.\n\n" +
	"2) ВТОРОЙ ФАКТОР. Вызывающий-ЧЕЛОВЕК не подтвердил личность повышенным уровнем " +
	"(step-up): выпуск и отзыв край относит к смене уровня доступа. Пройдите " +
	"подтверждение и повторите — токен, полученный без второго фактора, этих двух " +
	"действий не открывает.\n\n" +
	"Служебная учётка от этого порога ОСВОБОЖДЕНА правилом платформы: интерактивного " +
	"подтверждения у машины не бывает вовсе, и требовать его значило бы закрыть ей эти " +
	"вызовы навсегда. Поэтому машинному вызывающему тот же отказ означает нехватку ПРАВ, " +
	"а не второй фактор."

// Личный токен пользователя — то, чем ЧЕЛОВЕК ходит в платформу неинтерактивно.
//
// Ресурс несёт СЕКРЕТ, и это меняет в нём четыре вещи против обычного ресурса:
//
//  1. закрытый ключ выдаётся ОДИН раз — в ответе на выпуск — и повторно не читается
//     ничем. Более того, край ЗАТИРАЕТ его в ответе операции по истечении окна
//     (у iam оно настраиваемое, по умолчанию около двух минут), поэтому «дочитать
//     потом» нельзя даже по номеру той же операции;
//  2. обратное чтение подтверждает только СУЩЕСТВОВАНИЕ токена и никогда не трогает
//     материал: перечитав, оно затёрло бы единственный экземпляр секрета пустотой
//     руками того, кто просто сделал refresh;
//  3. любая правка входа ПЕРЕСОЗДАЁТ токен — изменяющей операции у выпущенного токена
//     не существует вовсе (край несёт только Issue/List/Revoke), а тихая подмена
//     материала под тем же адресом ресурса сломала бы всех, кто им пользуется;
//  4. импорта нет намеренно (см. конец файла).
//
// Вложенных объектов у ресурса нет: край возвращает выпущенный токен одним плоским
// объектом «строки-маппинга», и раскладывать его в types.Object было бы вложенностью
// ради вложенности — все его поля вычисляемые и один к одному ложатся в скаляры.
type userTokenModel struct {
	ID              types.String `tfsdk:"id"`
	UserID          types.String `tfsdk:"user_id"`
	CreatedByUserID types.String `tfsdk:"created_by_user_id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Labels          types.Map    `tfsdk:"labels"`
	TTLSeconds      types.Int64  `tfsdk:"ttl_seconds"`

	ClientID      types.String `tfsdk:"client_id"`
	KeyID         types.String `tfsdk:"key_id"`
	Algorithm     types.String `tfsdk:"algorithm"`
	PublicKeyPEM  types.String `tfsdk:"public_key_pem"`
	PrivateKeyPEM types.String `tfsdk:"private_key_pem"`
	ExpiresAt     types.String `tfsdk:"expires_at"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

type userTokenResource struct{ c *client.Client }

// NewIAMUserTokenResource — конструктор для реестра провайдера.
func NewIAMUserTokenResource() resource.Resource { return &userTokenResource{} }

// MoveState — переезд состояния с прежнего имени типа.
//
// Объявление обязательно: одного блока `moved` в настройке оператора НЕДОСТАТОЧНО —
// исполнитель шлёт запрос переезда целевому типу, и тип, не объявивший поддержки, роняет
// план. Что именно принимается и почему — iam_type_names.go.
func (r *userTokenResource) MoveState(ctx context.Context) []resource.StateMover {
	return movedFromRetiredTypeName(ctx, r)
}

func (r *userTokenResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameIAMUserToken
}

func (r *userTokenResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Внутренняя ошибка провайдера",
			fmt.Sprintf("ожидался *client.Client, получено %T", req.ProviderData))
		return
	}
	r.c = c
}

// userTokensPath — коллекция токенов ОДНОГО владельца. Токен адресуется только внутри
// пути своего пользователя: собственной коллекции верхнего уровня у него нет, и это же
// определяет форму обратного чтения — перечисление, а не чтение по идентификатору.
func userTokensPath(userID string) string {
	return iamUsersPath + "/" + userID + "/tokens"
}

func (r *userTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// ВСЁ, что уходит в запрос, пересоздаёт токен: изменяющей операции у выпущенного
	// токена не существует. Помечено явно на каждом поле, а не «по умолчанию»: умолчание
	// здесь означало бы правку, которая молча не применилась.
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Личный токен пользователя — то, чем человек ходит в платформу " +
			"неинтерактивно (CLI, конвейер сборки). Выпуск отдаёт пару ключей: открытый " +
			"остаётся у края, закрытым вызывающий подписывает `client_assertion` при обмене " +
			"на access-токен.\n\n" +
			":::danger Закрытый ключ попадает в состояние Terraform\n" +
			"Край отдаёт закрытый ключ ОДИН раз — в ответе на выпуск — и повторно его не " +
			"выдаёт никому: через короткое окно он затирает его даже в ответе той самой " +
			"операции. Значит единственный экземпляр материала лежит в файле состояния " +
			"В ОТКРЫТОМ ВИДЕ. Состояние такого проекта обязано храниться там, где хранят " +
			"секреты (зашифрованный удалённый backend с ограниченным доступом); в " +
			"репозиторий оно не коммитится никогда. То же касается вывода `terraform show` " +
			"и любых артефактов конвейера, куда попадает состояние.\n:::\n\n" +
			":::warning Истёкший токен край СНИМАЕТ САМ — и план это увидит\n" +
			"У токена есть срок (`ttl_seconds` → `expires_at`). По истечении срока край " +
			"перестаёт принимать подпись, а через отсрочку СНИМАЕТ и саму строку: обратное " +
			"чтение её больше не находит, ресурс уходит из состояния, и `plan` предлагает " +
			"создать его заново.\n\n" +
			"Для конвейера с автоматическим применением это означает САМОСТОЯТЕЛЬНУЮ ротацию " +
			"через окно отсрочки после истечения — по существу лучше прежнего поведения " +
			"(«молча мёртвые учётные данные, план зелёный»), но знать об этом надо: " +
			"пересоздание выдаёт НОВЫЙ закрытый ключ, он попадает в файл состояния, и всё, " +
			"что подписывалось прежним, обязано быть перенастроено. Конвейер с воротами по " +
			"расхождению получит на этом новый класс срабатываний.\n\n" +
			"Замену по-прежнему можно планировать ЗАРАНЕЕ, не дожидаясь снятия: " +
			"`terraform apply -replace=<адрес ресурса>` либо срок жизни, выраженный " +
			"отдельным ресурсом-таймером, от которого зависит этот.\n:::\n\n" +
			"Выпуск и отзыв край относит к смене уровня доступа: он требует и ПРАВА, " +
			"которого не выдают, и повышенного подтверждения личности.\n\n" +
			userTokenDeniedCauses,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Идентификатор токена у края (префикс `uoc`). Он же — " +
					"идентификатор ключа подписи (`kid`) в подписанных утверждениях.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},

			"user_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Пользователь, которому выпускается токен. Токен живёт " +
					"внутри этой личности: удаление пользователя снимает его токены вместе с " +
					"ним. Пользователь обязан быть действующим — приглашённому, но ещё не " +
					"вошедшему, и заблокированному край токен не выпустит."},
			// Optional, а не Required, и это не послабление: край САМ подставляет сюда
			// аутентифицированного вызывающего, когда поле пусто, и отвергает как подлог
			// любое непустое значение, не совпадающее с ним. Требовать его от оператора
			// значило бы требовать написать в конфигурации собственный идентификатор —
			// единственное значение, которое здесь вообще принимается.
			"created_by_user_id": schema.StringAttribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.String{
					// Порядок обязателен: без UseStateForUnknown вычисляемое поле приходило бы
					// в план неизвестным, а «изменение требует замены» считало бы неизвестное
					// другим значением — токен пересоздавался бы от правки чего угодно.
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Кто выпустил токен (аудит). Не задавайте это поле: край " +
					"подставит вызывающего сам. Чужой идентификатор он отвергает — выпустить " +
					"токен «от чужого имени» нельзя."},
			"name": schema.StringAttribute{Optional: true, Computed: true,
				Default: stringdefault.StaticString(""), PlanModifiers: replace,
				MarkdownDescription: "Человекочитаемая метка токена (`ноутбук`, `конвейер сборки`). " +
					"Не идентификатор: адресуется токен только по `id`."},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				Default: stringdefault.StaticString(""), PlanModifiers: replace,
				MarkdownDescription: "Свободное описание, до 256 знаков."},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				PlanModifiers: []planmodifier.Map{mapRequiresReplace{}},
				MarkdownDescription: "Метки токена. Задаются при выпуске и неизменяемы — " +
					"правка пересоздаёт токен."},
			"ttl_seconds": schema.Int64Attribute{Optional: true, Computed: true,
				Default:       int64default.StaticInt64(0),
				PlanModifiers: []planmodifier.Int64{int64RequiresReplace{}},
				MarkdownDescription: "Срок жизни токена в секундах, до `63072000` (два года). " +
					"`0` — без срока.\n\n" +
					":::tip Срок — не косметика\n" +
					"Токен без срока живёт, пока его не отзовут вручную, а отзывать забывают. " +
					"Задавайте срок и заменяйте токен заранее: сам собой он не заменится " +
					"(см. предупреждение об истечении выше).\n:::"},

			"client_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Идентификатор клиента OAuth2 — им токен представляется " +
					"при обмене подписанного утверждения на access-токен. Совпадает с `id` " +
					"токена: у удостоверения одно имя, а не два."},
			"key_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Идентификатор ключа подписи (`kid`). Совпадает с `id` " +
					"токена — так подписанное утверждение само называет свой ключ."},
			"algorithm": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Алгоритм подписи; у личных токенов всегда `ES256`."},
			"public_key_pem": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Открытый ключ в PEM (SPKI). Секретом не является."},
			"private_key_pem": schema.StringAttribute{Computed: true, Sensitive: true,
				MarkdownDescription: "Закрытый ключ в PEM (PKCS#8, ECDSA P-256). Выдаётся ОДИН " +
					"раз и повторно не читается ничем — потеряв его, токен остаётся только " +
					"отозвать и выпустить новый. Лежит в состоянии открытым."},
			"expires_at": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Момент истечения. Пусто — токен бессрочный."},
			"created_at": schema.StringAttribute{Computed: true},
			// Времени последнего использования здесь НЕТ намеренно: оно меняется само по
			// себе, от каждого входа, и его присутствие давало бы вечный дрейф плана на
			// ресурсе, которого никто не трогал.
		},
	}
}

// userTokenWire — форма токена в ответе края (общая у выпуска и у перечисления).
//
// Разбирается своим типом, а не сгенерённым: сгенерённый потребовал бы разрешения
// google.protobuf.Any через глобальный реестр типов, которого у провайдера нет. Тело
// ЗАПРОСА при этом остаётся сгенерённым — именно там опечатка в имени поля прошла бы
// молча.
type userTokenWire struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	// HydraClientID — зеркало клиента у ВНЕШНЕГО поставщика. У токенов нового
	// выпуска пусто; в состояние не берётся ни одним полем (#1121). Оставлено в
	// разборе тела, чтобы форма ответа края читалась целиком, а не выборочно.
	HydraClientID   string            `json:"hydraClientId"`
	Description     string            `json:"description"`
	ExpiresAt       string            `json:"expiresAt"`
	CreatedByUserID string            `json:"createdByUserId"`
	CreatedAt       string            `json:"createdAt"`
	PublicKeyPEM    string            `json:"publicKeyPem"`
	KeyAlgorithm    string            `json:"keyAlgorithm"`
	Name            string            `json:"name"`
	Labels          map[string]string `json:"labels"`
}

// issuedUserTokenWire — ответ операции выпуска.
//
// Три поля верхнего уровня (открытый ключ, алгоритм, идентификатор клиента) дублируют
// то, что уже лежит во вложенном объекте токена. В состояние они берутся ИЗ ВЛОЖЕННОГО —
// тем же местом, каким их берёт обратное чтение, где верхнего уровня не существует.
// Иначе одно и то же значение приезжало бы из двух источников и разошлось бы на первом
// же расхождении края.
type issuedUserTokenWire struct {
	Token         userTokenWire `json:"token"`
	ClientID      string        `json:"clientId"`
	PrivateKeyPEM string        `json:"privateKeyPem"`
	PublicKeyPEM  string        `json:"publicKeyPem"`
	Algorithm     string        `json:"algorithm"`
	KeyID         string        `json:"keyId"`
}

// applyUserToken раскладывает ответ края по НЕсекретным полям модели.
//
// Закрытого ключа функция не касается ни на одном пути — это её главное свойство, а не
// упущение: край его не отдаёт повторно, и присвоение сюда пустоты уничтожило бы
// единственный экземпляр секрета.
//
// Идентификатор ресурса тоже НЕ трогается: он пишется ровно в одном месте — на пути
// выпуска, из метаданных операции. Иначе расхождение метаданных и тела ответа молча
// сменило бы адрес, по которому токен отзывается.
func applyUserToken(ctx context.Context, m *userTokenModel, w *userTokenWire) {
	m.UserID = types.StringValue(w.UserID)
	m.CreatedByUserID = types.StringValue(w.CreatedByUserID)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	// Идентификатор клиента — это `id` СТРОКИ токена, а не зеркало у внешнего
	// поставщика (#1121). Им подписывается утверждение, и по нему издатель
	// платформы разрешает клиента; зеркало на этом пути не участвует и у токенов
	// нового выпуска пусто вовсе. Прежняя редакция брала зеркало — и после
	// перевода выдачи отдала бы сюда пустоту, то есть атрибут, которым, по
	// собственному описанию, «токен представляется при обмене», нечем было бы
	// представиться.
	m.ClientID = types.StringValue(w.ID)
	m.KeyID = types.StringValue(w.ID)
	m.Algorithm = types.StringValue(w.KeyAlgorithm)
	m.PublicKeyPEM = types.StringValue(w.PublicKeyPEM)
	m.ExpiresAt = types.StringValue(w.ExpiresAt)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	// ttl_seconds не трогается СОЗНАТЕЛЬНО: край возвращает момент истечения, а не
	// срок в секундах, и вычислять секунды обратно значило бы получать каждый раз новое
	// число (оно зависит от «сейчас») и вечное расхождение плана.
}

func (r *userTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Пустой автор — законный вход: край подставит вызывающего. ValueString() отдаёт
	// пустую строку и для null, и для неизвестного, и оба случая здесь означают одно —
	// «оператор автора не называл».
	body := &iamv1.IssueUserTokenRequest{
		UserId:          plan.UserID.ValueString(),
		CreatedByUserId: plan.CreatedByUserID.ValueString(),
		Name:            plan.Name.ValueString(),
		Description:     plan.Description.ValueString(),
		Labels:          mapFromTF(ctx, plan.Labels),
		TtlSeconds:      plan.TTLSeconds.ValueInt64(),
	}

	raw, err := client.MarshalBody(body)
	if err != nil {
		resp.Diagnostics.AddError("Тело запроса не собрано", err.Error())
		return
	}
	// Ключ идемпотентности считается ПО ТЕЛУ: повтор того же запроса (потерян ответ,
	// оборвалась сеть) не выпустит второй токен, а изменённый запрос — это другой запрос
	// и уходит заново. Для секрета цена дубля выше обычной: лишний живой токен, о котором
	// никто не знает.
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(
		typeNameIAMUserToken, plan.UserID.ValueString()+"/"+plan.Name.ValueString(), raw)}

	httpResp, err := r.c.Do(ctx, http.MethodPost, userTokensPath(plan.UserID.ValueString()), body, hdr)
	if err != nil {
		resp.Diagnostics.AddError("Выпуск токена не завершился", err.Error())
		return
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		detail := out.Message
		if out.Kind == client.OutcomeDenied {
			detail += "\n\n" + userTokenDeniedCauses
		}
		resp.Diagnostics.AddError("Выпуск токена отвергнут", detail)
		return
	}

	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	// Метаданные операции читаются ТОЛЬКО после проверки отказа — её делает
	// AwaitOperation: при отказе она не возвращает операцию вовсе, потому что
	// идентификатор в метаданных присутствует и у операции, завершившейся отказом.
	done, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{})
	if err != nil {
		resp.Diagnostics.AddError("Выпуск токена не завершился", err.Error())
		return
	}

	issued, err := decodeIssuedUserToken(done.Response)
	if err != nil {
		resp.Diagnostics.AddError("Ответ операции не разобран", err.Error())
		return
	}

	// Идентификатор — из метаданных (край чеканит его при приёме запроса); тело ответа
	// служит запасным источником, чтобы отсутствие одного поля не хоронило выпущенный
	// токен как неадресуемый.
	id, _ := done.MetadataString("keyId")
	if id == "" {
		id = issued.KeyID
	}
	if id == "" {
		id = issued.Token.ID
	}
	if id == "" {
		resp.Diagnostics.AddError("Токен выпущен, но не адресуем",
			"В ответе операции нет идентификатора токена — отозвать его через Terraform "+
				"будет нечем. Найдите его в списке токенов пользователя "+
				plan.UserID.ValueString()+" и отзовите вручную.")
		return
	}

	// Материал проверяется ПОСЛЕ того, как установлен идентификатор: без него отказ не
	// смог бы назвать, что именно осталось висеть у края.
	//
	// Пустой и отсутствующий закрытый ключ — один и тот же исход: ресурс без материала
	// бесполезен, а в состоянии выглядел бы созданным.
	if issued.PrivateKeyPEM == "" {
		resp.Diagnostics.AddError("Токен выпущен, но материал не получен",
			"Операция "+op.ID+" завершилась успехом, а закрытого ключа в её ответе нет. "+
				"Повторно край его не выдаст — он затирает материал в ответе операции по "+
				"истечении короткого окна после её завершения. Отзовите токен "+id+
				" у пользователя "+plan.UserID.ValueString()+" и повторите apply.")
		return
	}

	plan.ID = types.StringValue(id)
	plan.PrivateKeyPEM = types.StringValue(issued.PrivateKeyPEM)
	applyUserToken(ctx, &plan, &issued.Token)
	// Неизвестные значения гасятся до записи: Terraform не принимает после apply НИ
	// ОДНОГО неизвестного, и без этого единственная настоящая причина утонула бы среди
	// сообщений об «invalid result object» по каждому полю.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// decodeIssuedUserToken переводит ответ операции в типизированную форму.
//
// Ответ приезжает разобранным в общую карту (метаданные и ответ операции лежат в
// google.protobuf.Any, а его разрешение зависело бы от глобального реестра типов —
// см. client/operation.go). Обратная сборка в JSON и один разбор по типу дешевле
// разбора карты по полям и держит имена полей в одном месте.
func decodeIssuedUserToken(m map[string]any) (*issuedUserTokenWire, error) {
	if m == nil {
		return nil, fmt.Errorf("операция завершилась успехом, но её ответ пуст — " +
			"ни токена, ни материала в нём нет")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("сборка ответа операции: %w", err)
	}
	var out issuedUserTokenWire
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("разбор ответа операции: %w", err)
	}
	return &out, nil
}

// Read подтверждает, что токен ЖИВ, и не трогает его материал.
//
// Читать нечем, кроме перечисления: чтения по идентификатору у токена нет вовсе. Это
// меняет и способ подтверждения отсутствия — общий absenceDiagnostics здесь неприменим
// by construction: он сужает список фильтром по имени и параметром области, а у этой
// коллекции нет ни того, ни другого (владелец задан сегментом пути, фильтра по имени
// контракт не несёт).
//
// Зато у неё есть свойство, которого нет у ресурсов проекта: список токенов НЕ
// фильтруется пообъектно — доступ решается один раз, на владельце. Поэтому УСПЕШНЫЙ
// ответ списка означает полную картину, и отсутствие нашего идентификатора в нём —
// настоящий отзыв, а не окно прав. Это единственный путь, на котором ресурс снимается
// из состояния.
//
// А вот ответ «не найдено» на самом списке не устанавливает НИЧЕГО, и вопрос о владельце
// этого не меняет: край скрывает отказ в доступе к пользователю тем же байт-в-байт «не
// найдено», что и его удаление, поэтому «пользователя нет» и «прав на него больше нет»
// приходят одинаково — тем же механизмом, что уже дал двусмысленность на списке. Снять
// токен из состояния по такой паре ответов значило бы забыть ЖИВОЙ секрет: отозвать его
// через Terraform станет нечем, а знать о нём — некому. Это ровно то, что Delete ниже
// отказывается делать молча, и Read не вправе делать за него.
func (r *userTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	userID, tokenID := state.UserID.ValueString(), state.ID.ValueString()

	tok, out, err := r.findUserToken(ctx, userID, tokenID)
	if err != nil {
		resp.Diagnostics.AddError("Список токенов не прочитан", err.Error())
		return
	}
	switch out.Kind {
	case client.OutcomeOK:
	case client.OutcomeNotFound:
		// «Не найдено» на списке НЕ означает, что токен отозван: тем же ответом край
		// скрывает существование от того, кому доступ не выдан, — побайтово тем же.
		// Спрашиваем владельца, но ни один его ответ не даёт права снять ресурс из
		// состояния: см. разбор в шапке Read.
		_, uerr := readByID(ctx, r.c, iamUsersPath, userID, false)
		if uerr == nil {
			resp.Diagnostics.AddError("Доступ к токенам пользователя утрачен",
				"Пользователь "+userID+" читается, а список его токенов отвечает «не найдено». "+
					"Это событие ПРАВ, а не отзыв токена: продолжать нельзя, иначе Terraform "+
					"выпустит новый секрет, а прежний останется жить у края без владельца в "+
					"состоянии.")
			return
		}
		var missing *notFoundError
		if asNotFound(uerr, &missing) {
			// Здесь стояло снятие ресурса из состояния с обоснованием «спрашивали не
			// токен, а владельца, значит отсутствие установлено». Обоснование неверно:
			// вопрос о владельце отвечает ТЕМ ЖЕ механизмом сокрытия существования, что
			// и список, — отказ в доступе к пользователю приходит побайтово тем же «не
			// найдено», что и его удаление. Два одинаково двусмысленных ответа не дают
			// однозначного, сколько бы их ни было.
			resp.Diagnostics.AddError("Отсутствие токена не подтверждено",
				"Список токенов пользователя "+userID+" отвечает «не найдено», и сам "+
					"пользователь отвечает тем же. Оба ответа объясняются двояко, и различить "+
					"объяснения нечем: либо пользователь удалён — тогда его токены сняты вместе "+
					"с ним внешним ключом, — либо доступ к нему отозван, а край намеренно "+
					"отдаёт отказ в доступе дословно так же, как настоящее отсутствие.\n\n"+
					"Снять токен из состояния по такому ответу нельзя: если он жив, Terraform "+
					"забудет работающие учётные данные — отозвать их через него будет уже "+
					"нечем, а знать о них некому.\n\nУбедившись, что пользователь "+userID+
					" действительно удалён (значит удалён и токен), уберите ресурс из "+
					"состояния вручную:\n  terraform state rm <адрес ресурса>")
			return
		}
		resp.Diagnostics.AddError("Судьба токена не установлена",
			"Список токенов пользователя "+userID+" отвечает «не найдено», и сам "+
				"пользователь тоже не читается: "+uerr.Error()+"\n\nПродолжать нельзя: "+
				"«токен отозван» и «доступ утрачен» край отдаёт одинаково, а разница "+
				"между ними — выпускать ли новый секрет.")
		return
	default:
		resp.Diagnostics.AddError("Список токенов не прочитан", out.Message)
		return
	}

	if tok == nil {
		// Список прочитан целиком и принадлежит ровно тому владельцу, у которого токен и
		// висел, — отсутствие настоящее.
		resp.State.RemoveResource(ctx)
		return
	}
	applyUserToken(ctx, &state, tok)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// maxUserTokenPages — потолок обхода курсора.
//
// Существует не «на всякий случай»: край, вернувший неподвижный курсор, крутил бы
// провайдера бесконечно, и refresh висел бы без объяснения. Тысяча страниц по тысяче
// строк — заведомо больше, чем токенов бывает у человека, поэтому упереться в потолок
// значит наткнуться на неисправность, а не на большой список.
const maxUserTokenPages = 1000

// findUserToken ищет токен в списке владельца, проходя курсор до конца.
//
// Курсор проходится ЦЕЛИКОМ, а не первой страницей: «нет на первой странице» и «нет
// вовсе» — разные утверждения, и первое, принятое за второе, объявило бы живой токен
// отозванным и заставило бы выпустить новый.
func (r *userTokenResource) findUserToken(ctx context.Context, userID, tokenID string) (*userTokenWire, client.Outcome, error) {
	page := ""
	for i := 0; i < maxUserTokenPages; i++ {
		q := url.Values{}
		// Потолок страницы — контрактный максимум края: чем меньше страниц, тем меньше
		// окон, в которые состав может измениться посреди обхода.
		q.Set("pageSize", "1000")
		if page != "" {
			q.Set("pageToken", page)
		}
		httpResp, err := r.c.Do(ctx, http.MethodGet,
			userTokensPath(userID)+"?"+q.Encode(), nil, nil)
		if err != nil {
			return nil, client.Outcome{}, err
		}
		if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
			return nil, out, nil
		}

		var body struct {
			Tokens        []userTokenWire `json:"tokens"`
			NextPageToken string          `json:"nextPageToken"`
		}
		if err := json.Unmarshal(httpResp.Body, &body); err != nil {
			return nil, client.Outcome{}, fmt.Errorf("разбор списка токенов: %w", err)
		}
		for k := range body.Tokens {
			if body.Tokens[k].ID == tokenID {
				found := body.Tokens[k]
				return &found, client.Outcome{Kind: client.OutcomeOK}, nil
			}
		}
		// Неподвижный курсор приравнивается к концу списка: продолжать по нему значило бы
		// читать ту же страницу до потолка и объявить неисправность «отсутствием».
		if body.NextPageToken == "" || body.NextPageToken == page {
			return nil, client.Outcome{Kind: client.OutcomeOK}, nil
		}
		page = body.NextPageToken
	}
	return nil, client.Outcome{}, fmt.Errorf(
		"список токенов пользователя %s не кончился за %d страниц — курсор края не "+
			"сходится, и отсутствие токена таким обходом не устанавливается",
		userID, maxUserTokenPages)
}

// Update недостижим: каждое поле входа помечено как пересоздающее. Метод существует,
// потому что его требует интерфейс, и говорит правду, а не молчит.
func (r *userTokenResource) Update(ctx context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Токен не изменяется",
		"У выпущенного токена нет изменяющей операции: край несёт только выпуск, "+
			"перечисление и отзыв. Сменить срок, имя, описание или метки можно только "+
			"выпуском нового токена. Если сюда всё же попало исполнение — это ошибка "+
			"провайдера, сообщите о ней.")
	_ = ctx
}

// Delete отзывает токен.
//
// Отказ «не найдено» НЕ проглатывается как успех, хотя соблазн есть: тем же ответом край
// скрывает отказ в доступе, и молчаливый успех снял бы из состояния ЖИВОЙ секрет — то
// есть оставил бы работающие учётные данные, о которых больше никто не знает. Пусть
// оператор решает, увидев причину.
//
// Прежде здесь стояло, что «не найдено» означает ЛИБО уже отозванный токен, ЛИБО
// утраченный доступ. С #1216 первая причина отпала: отзыв идемпотентен успехом, и
// повторный отзыв этой ошибки не даёт. Осталась одна причина, и потому она названа
// в тексте прямо — перечень причин, переживший свой предмет, отправляет оператора
// искать не там.
func (r *userTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := awaitMutation(ctx, r.c, http.MethodDelete,
		userTokensPath(state.UserID.ValueString())+"/"+state.ID.ValueString(), nil)
	if err == nil {
		return
	}
	resp.Diagnostics.AddError("Отзыв токена не завершился",
		err.Error()+"\n\n"+userTokenDeniedCauses+
			"\n\nЕсли край ответил «не найдено», это НЕ значит, что токен уже отозван: "+
			"повторный отзыв идемпотентен успехом и такой ошибки не даёт. Значит утрачен "+
			"доступ к пользователю "+state.UserID.ValueString()+", а отказ в доступе край "+
			"отдаёт тем же ответом, что и отсутствие. Убедившись, что токен действительно "+
			"отозван, уберите его из состояния вручную:\n  terraform state rm <адрес ресурса>")
}

// ImportState отсутствует НАМЕРЕННО, и это не пропуск.
//
// Импортировать можно то, что читается. Закрытый ключ край не отдаёт повторно, поэтому
// импорт дал бы ресурс с пустым материалом — состояние, которое выглядит рабочим и не
// работает, а первое же обращение к нему сорвалось бы у того, кто про эту пустоту не
// знает. Существующий токен отзывается и выпускается заново.
var _ resource.Resource = (*userTokenResource)(nil)
