// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// Ключ служебной учётки — то, чем она пользуется. Без ключа учётка заведена, но войти ей
// нечем.
//
// Ресурс несёт СЕКРЕТ, и это меняет в нём три вещи по сравнению с обычным ресурсом:
//
//  1. закрытый ключ выдаётся ОДИН раз, в ответе на выпуск, и повторно не читается ничем;
//  2. поэтому обратное чтение подтверждает только СУЩЕСТВОВАНИЕ ключа и никогда не
//     трогает материал: перечитав, оно затёрло бы его пустотой;
//  3. любая правка входа ПЕРЕСОЗДАЁТ ключ — сменить срок или описание у выпущенного
//     ключа нечем, а тихая подмена материала под тем же адресом ресурса сломала бы всех,
//     кто им пользуется.
type saKeyModel struct {
	ID               types.String `tfsdk:"id"`
	ServiceAccountID types.String `tfsdk:"service_account_id"`
	CreatedByUserID  types.String `tfsdk:"created_by_user_id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	Labels           types.Map    `tfsdk:"labels"`
	TTLSeconds       types.Int64  `tfsdk:"ttl_seconds"`
	Audience         types.List   `tfsdk:"audience"`

	ClientID      types.String `tfsdk:"client_id"`
	KeyID         types.String `tfsdk:"key_id"`
	Algorithm     types.String `tfsdk:"algorithm"`
	PublicKeyPEM  types.String `tfsdk:"public_key_pem"`
	PrivateKeyPEM types.String `tfsdk:"private_key_pem"`
	ExpiresAt     types.String `tfsdk:"expires_at"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

type saKeyResource struct{ c *client.Client }

// NewIAMSAKeyResource — конструктор для реестра провайдера.
func NewIAMSAKeyResource() resource.Resource { return &saKeyResource{} }

// MoveState — переезд состояния с прежнего имени типа.
//
// Объявление обязательно: одного блока `moved` в настройке оператора НЕДОСТАТОЧНО —
// исполнитель шлёт запрос переезда целевому типу, и тип, не объявивший поддержки, роняет
// план. Что именно принимается и почему — iam_type_names.go.
func (r *saKeyResource) MoveState(ctx context.Context) []resource.StateMover {
	return movedFromRetiredTypeName(ctx, r)
}

func (r *saKeyResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameIAMServiceAccountKey
}

func (r *saKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *saKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// ВСЁ, что уходит в запрос, пересоздаёт ключ: изменяющей операции у выпущенного ключа
	// не существует. Помечено явно на каждом поле, а не «по умолчанию»: умолчание здесь
	// означало бы тихую подмену материала под тем же адресом ресурса.
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Ключ служебной учётки — то, чем она входит.\n\n" +
			":::danger Закрытый ключ попадает в состояние Terraform\n" +
			"Край отдаёт закрытый ключ ОДИН раз — в ответе на выпуск — и повторно его не " +
			"выдаёт никому. Значит он лежит в файле состояния в открытом виде. Состояние " +
			"такого проекта обязано храниться там, где хранят секреты; в репозиторий оно не " +
			"коммитится никогда.\n:::",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Идентификатор ключа у края (префикс `soc`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"service_account_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Учётка, которой выдаётся ключ."},
			// Optional, а не Required, и это не послабление: назвать ответственного
			// вправе ТОЛЬКО человек, и только самого себя, — а конвейер, ходящий
			// служебной учёткой, не вправе назвать никого. Край подставляет его сам:
			// человеку — вызывающего, машине — владельца аккаунта учётки, потому что
			// служебная учётка строкой пользователей не является и ответственным быть
			// не может. Требование поля делало ресурс неисполнимым у машины ПРИ ЛЮБОМ
			// входе: непустое значение край отвергает синхронно, а пустого требование
			// не допускает.
			"created_by_user_id": schema.StringAttribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.String{
					// Порядок обязателен: без UseStateForUnknown вычисляемое поле
					// приходило бы в план неизвестным, а «изменение требует замены»
					// считало бы неизвестное другим значением — ключ отзывался бы и
					// выпускался заново от правки чего угодно.
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{notEmptyIfSet(
					"Уберите поле вовсе: край подставит ответственного сам. Пустая строка " +
						"не означает ни «не назвал», ни «назвал» — на провод она уезжает " +
						"неотличимо от отсутствия, и состояние осталось бы утверждать " +
						"пустоту про ключ, у которого ответственный есть.")},
				MarkdownDescription: "Кто выпустил ключ (аудит). Не задавайте это поле: край " +
					"подставит ответственного сам — человеку вызывающего, служебной учётке " +
					"владельца аккаунта. Чужой идентификатор он отвергает, а от служебной " +
					"учётки не принимает НИКАКОЙ: выпустить ключ «от чужого имени» нельзя."},
			"name": schema.StringAttribute{Optional: true, Computed: true,
				Default: stringdefault.StaticString(""), PlanModifiers: replace},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				Default: stringdefault.StaticString(""), PlanModifiers: replace},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				PlanModifiers: []planmodifier.Map{mapRequiresReplace{}}},
			"ttl_seconds": schema.Int64Attribute{Optional: true, Computed: true,
				Default:       int64default.StaticInt64(0),
				PlanModifiers: []planmodifier.Int64{int64RequiresReplace{}},
				MarkdownDescription: "Срок жизни ключа в секундах, до `63072000` (два года). " +
					"`0` — без срока.\n\n" +
					":::tip Срок — не косметика\n" +
					"Ключ без срока живёт, пока его не отзовут вручную, а отзывать забывают. " +
					"Задавайте срок и пересоздавайте ключ заранее.\n:::"},
			"audience": schema.ListAttribute{Optional: true, Computed: true,
				ElementType:   types.StringType,
				PlanModifiers: []planmodifier.List{listRequiresReplace{}},
				MarkdownDescription: "Кому предназначен токен, выпущенный по этому ключу. " +
					"Пусто — умолчание края."},

			"client_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Идентификатор клиента OAuth2 — им ключ представляется при обмене."},
			"key_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Идентификатор ключа подписи (`kid`)."},
			"algorithm": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Алгоритм подписи, например `RS256`."},
			"public_key_pem": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Открытый ключ в PEM."},
			"private_key_pem": schema.StringAttribute{Computed: true, Sensitive: true,
				MarkdownDescription: "Закрытый ключ в PEM. Выдаётся один раз и повторно не читается."},
			"expires_at": schema.StringAttribute{Computed: true},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

func saKeysPath(saID string) string {
	return "/iam/v1/serviceAccounts/" + saID + "/keys"
}

func (r *saKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan saKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Пустой ответственный — законный и ЕДИНСТВЕННЫЙ вход служебной учётки: край
	// подставит его сам. ValueString() отдаёт пустую строку и для null, и для
	// неизвестного, и оба случая здесь означают одно — «оператор ответственного не
	// называл»; пустое поле сборка тела на провод не выносит.
	body := &iamv1.IssueSAKeyRequest{
		ServiceAccountId: plan.ServiceAccountID.ValueString(),
		CreatedByUserId:  plan.CreatedByUserID.ValueString(),
		Name:             plan.Name.ValueString(),
		Description:      plan.Description.ValueString(),
		Labels:           mapFromTF(ctx, plan.Labels),
		TtlSeconds:       plan.TTLSeconds.ValueInt64(),
		Audience:         stringsFromTF(ctx, plan.Audience),
	}

	path := saKeysPath(plan.ServiceAccountID.ValueString())
	raw, err := client.MarshalBody(body)
	if err != nil {
		resp.Diagnostics.AddError("Тело запроса не собрано", err.Error())
		return
	}
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(
		typeNameIAMServiceAccountKey, plan.ServiceAccountID.ValueString()+"/"+plan.Name.ValueString(), raw)}

	httpResp, err := r.c.Do(ctx, http.MethodPost, path, body, hdr)
	if err != nil {
		resp.Diagnostics.AddError("Выпуск ключа не завершился", err.Error())
		return
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		detail := out.Message
		if out.Kind == client.OutcomeDenied {
			// Причин ровно ДВЕ, и они чинятся в разных местах: одна — выдачей права,
			// другая — повторным входом человека. Назвать одну значит отправить
			// половину читателей чинить не то.
			//
			// Здесь стояло «машина второй фактор не проходит, заводите ключи под
			// человеческой личностью». Это неверно: машинный принципал освобождён от
			// порога уровня доверия, и текст отговаривал от полосы, ради которой
			// машинная личность и заводится.
			detail += "\n\nОтказ приходит по одной из двух причин:\n" +
				"  • вызывающему не выдано право на эту служебную учётку — проверьте выдачу;\n" +
				"  • вызывающий-ЧЕЛОВЕК не подтвердил личность повышенным уровнем (step-up): " +
				"выпуск ключа требует его от людей.\n" +
				"Служебная учётка от этого порога освобождена — конвейер, ходящий машинной " +
				"личностью, получает такой отказ из-за прав, а не из-за второго фактора."
		}
		resp.Diagnostics.AddError("Выпуск ключа отвергнут", detail)
		return
	}

	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	done, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{})
	if err != nil {
		resp.Diagnostics.AddError("Выпуск ключа не завершился", err.Error())
		return
	}

	// Материал берётся ИЗ ОТВЕТА операции, и другого случая его получить не будет.
	// Поэтому отсутствие закрытого ключа — терминальная ошибка, а не пустое значение:
	// ресурс без материала бесполезен, а выглядел бы созданным.
	priv, ok := done.ResponseString("privateKeyPem")
	if !ok || priv == "" {
		resp.Diagnostics.AddError("Ключ выпущен, но материал не получен",
			"Операция завершилась успехом, а закрытого ключа в её ответе нет. Повторно край "+
				"его не выдаст: отзовите ключ у учётки "+plan.ServiceAccountID.ValueString()+
				" и повторите apply.")
		return
	}

	id, _ := done.ResponseString("keyId")
	if id == "" {
		// Идентификатор ресурса ключа лежит во вложенном объекте key, а `keyId` — это
		// идентификатор ПОДПИСИ. Разные вещи, и путать их нельзя: по второму ключ не
		// отзывается.
		id = nestedString(done.Response, "key", "id")
	}
	if id == "" {
		resp.Diagnostics.AddError("Ключ выпущен, но не адресуем",
			"В ответе операции нет идентификатора ключа — отозвать его через Terraform будет нечем.")
		return
	}

	plan.ID = types.StringValue(id)
	plan.PrivateKeyPEM = types.StringValue(priv)
	applySAKeyResponse(ctx, &plan, done.Response)
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// nestedString — строка из вложенного объекта ответа.
func nestedString(m map[string]any, path ...string) string {
	cur := any(m)
	for _, k := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[k]
	}
	s, _ := cur.(string)
	return s
}

func applySAKeyResponse(ctx context.Context, m *saKeyModel, r map[string]any) {
	get := func(k string) types.String {
		v, _ := r[k].(string)
		return types.StringValue(v)
	}
	// Ответственный приезжает ИЗ ОТВЕТА: на машинной полосе настройка его не называла,
	// и другого источника у состояния нет. Пустой ответ значения НЕ затирает — поле
	// пересоздающее, и стёртое пустотой следующий план прочитал бы как другое значение,
	// то есть отозвал бы живой ключ и выпустил новый материал.
	if by := nestedString(r, "key", "createdByUserId"); by != "" {
		m.CreatedByUserID = types.StringValue(by)
	}
	m.ClientID = get("clientId")
	m.KeyID = get("keyId")
	m.Algorithm = get("algorithm")
	m.PublicKeyPEM = get("publicKeyPem")
	m.ExpiresAt = types.StringValue(nestedString(r, "key", "expiresAt"))
	m.CreatedAt = types.StringValue(nestedString(r, "key", "createdAt"))
	if m.Audience.IsUnknown() || m.Audience.IsNull() {
		var auds []string
		if list, ok := r["audiences"].([]any); ok {
			for _, a := range list {
				if s, ok := a.(string); ok {
					auds = append(auds, s)
				}
			}
		}
		m.Audience = listFromStrings(ctx, auds)
	}
}

// Read подтверждает, что ключ ЖИВ, и не трогает его материал.
//
// Перечитывать материал нечем: край отдаёт его один раз. Обычное «прочитать ресурс и
// разложить в состояние» затёрло бы закрытый ключ пустотой — то есть уничтожило бы
// единственный экземпляр секрета руками того, кто просто сделал refresh.
func (r *saKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state saKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	list, err := r.c.Do(ctx, http.MethodGet, saKeysPath(state.ServiceAccountID.ValueString()), nil, nil)
	if err != nil {
		resp.Diagnostics.AddError("Список ключей не прочитан", err.Error())
		return
	}
	out := client.Classify(list)
	switch out.Kind {
	case client.OutcomeOK:
	case client.OutcomeNotFound:
		// Учётки нет — значит нет и её ключей. Это единственный случай, когда «не найдено»
		// на СПИСКЕ означает отсутствие: спрашивали не ключ, а владельца.
		resp.State.RemoveResource(ctx)
		return
	default:
		resp.Diagnostics.AddError("Список ключей не прочитан", out.Message)
		return
	}

	var page struct {
		Keys []struct {
			ID        string `json:"id"`
			ExpiresAt string `json:"expiresAt"`
			CreatedAt string `json:"createdAt"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(list.Body, &page); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	for _, k := range page.Keys {
		if k.ID == state.ID.ValueString() {
			state.ExpiresAt = types.StringValue(k.ExpiresAt)
			state.CreatedAt = types.StringValue(k.CreatedAt)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	// Ключа в списке владельца нет — он СНЯТ. Поводов у этого два, и различить их
	// отсюда нечем: отзыв арендатором и снятие ПЛАТФОРМОЙ по истечении срока
	// (задача #1264, край снимает истёкшую строку через отсрочку). Для состояния
	// разницы нет — ресурса больше не существует ни в одном из двух случаев, —
	// а различает их журнал аудита края: у снятия платформой свой тип события.
	//
	// Здесь подтверждение полное: список прочитан целиком и принадлежит ровно тому
	// владельцу, у которого ключ и висел.
	resp.State.RemoveResource(ctx)
}

// Update недостижим: каждое поле входа помечено как пересоздающее. Метод существует,
// потому что его требует интерфейс, и говорит правду, а не молчит.
func (r *saKeyResource) Update(ctx context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Ключ не изменяется",
		"У выпущенного ключа нет изменяющей операции: сменить срок, имя или назначение можно "+
			"только выпуском нового. Если сюда всё же попало исполнение — это ошибка провайдера, "+
			"сообщите о ней.")
	_ = ctx
}

func (r *saKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state saKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := awaitMutation(ctx, r.c, http.MethodDelete,
		saKeysPath(state.ServiceAccountID.ValueString())+"/"+state.ID.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Отзыв ключа не завершился", err.Error())
		return
	}
}

// ImportState отсутствует НАМЕРЕННО, и это не пропуск.
//
// Импортировать можно то, что читается. Закрытый ключ край не отдаёт повторно, поэтому
// импорт дал бы ресурс с пустым материалом — то есть состояние, которое выглядит рабочим и
// не работает. Существующий ключ отзывается и выпускается заново.
var _ resource.Resource = (*saKeyResource)(nil)
