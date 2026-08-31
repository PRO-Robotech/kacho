// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/proto"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// userMembershipPath — коллекция пользователей края. Названа СТРОКОЙ ЧЛЕНСТВА, потому что
// именно это лежит по адресу: одна личность держит по строке на каждый аккаунт, и все
// действия этого ресурса — над одной такой строкой, а не над человеком.
const userMembershipPath = "/iam/v1/users"

// userInvitationHuman — как ресурс называется в текстах отказов.
const userInvitationHuman = "Приглашение пользователя"

// Имена состояний берутся у СГЕНЕРЁННОГО перечисления, а не переписываются литералами:
// своя копия разошлась бы с контрактом молча, и сравнение с «BLOCKED» после переименования
// варианта просто перестало бы совпадать — без единой ошибки сборки.
var (
	inviteStatusPending = iamv1.User_PENDING.String()
	inviteStatusBlocked = iamv1.User_BLOCKED.String()
)

// Приглашение пользователя — ресурс, чей предмет живёт СВОЕЙ жизнью, и это определяет в
// нём почти всё.
//
// Жизненный цикл у края такой: приглашение заводит строку ЧЛЕНСТВА в аккаунте
// (`PENDING`) → человек входит через провайдера личности → та же строка становится
// `ACTIVE` и обзаводится внешним идентификатором. Строку заводит `Invite`, а не `Create`:
// вызова создания пользователя в контракте края НЕТ вовсе — ни рабочего, ни устаревшего.
// Заголовок службы всё ещё поминает какой-то `Create`, отвечающий «используйте Invite», но
// метода с таким именем служба не объявляет: `grep -n 'rpc ' proto/kacho/cloud/iam/v1/
// user_service.proto` даёт восемь вызовов, и `Create` среди них нет. Приводить этот отказ
// как поведение края значило бы обещать ответ, которого некому произвести — потому здесь
// названо то, что верно: создать пользователя нельзя ничем.
// Отсюда и имя ресурса: он управляет ПРИГЛАШЕНИЕМ и его строкой
// членства, а не человеком — личность принадлежит провайдеру входа, и облако ею не
// распоряжается.
//
// Что из этого следует для Terraform, по пунктам, потому что каждый пункт — чужое решение,
// а не наш выбор:
//
//  1. «Изменить» можно РОВНО ДВЕ вещи: метки (изменяющий запрос принимает только их) и
//     состояние блокировки (отдельные действия края, не поле). Всё остальное — почта,
//     аккаунт, затравка отображаемого имени, стартовая привязка к проекту — задаётся
//     приглашением и после него у края не меняется ничем. В схеме это сказано
//     пересозданием, а не молчанием: провайдер, принявший правку и не применивший её,
//     врёт об исходе (`api-conventions.md` §«Принято-и-проигнорировано»).
//
//  2. «Удалить» — значит снять СТРОКУ ЧЛЕНСТВА в этом аккаунте. Для непринятого
//     приглашения это отзыв приглашения; для вошедшего человека — исключение его из
//     аккаунта. Личность при этом не трогается: одна личность держит по строке на каждый
//     аккаунт, и в остальных она продолжает работать.
//
//  3. Приглашение ИДЕМПОТЕНТНО по паре (аккаунт, почта). Повторное приглашение той же
//     почты возвращает УЖЕ СУЩЕСТВУЮЩУЮ строку, а не заводит вторую. Это свойство здесь не
//     украшение: оно делает дешёвой всякую ошибку в сторону «создать заново» — дубля
//     человека получиться не может.
//
//  4. Персональные данные попадают в состояние Terraform: почта и отображаемое имя лежат в
//     нём в открытом виде. Состояние такого проекта хранится там, где хранят персональные
//     данные, и в репозиторий не коммитится. Тексты отказов провайдера почту НЕ называют —
//     ни в одном сообщении этого файла её нет; для указания на ресурс используется
//     идентификатор строки или аккаунта.
type userInvitationModel struct {
	ID        types.String `tfsdk:"id"`
	AccountID types.String `tfsdk:"account_id"`
	Email     types.String `tfsdk:"email"`

	// DisplayName / ProjectID / RoleID — ЗАТРАВКИ приглашения. Край принимает их один раз,
	// обратно не отдаёт (первые два) либо перезаписывает при первом входе (имя), поэтому
	// они НИКОГДА не приводятся из ответа края — см. applyUserInvitation.
	DisplayName types.String `tfsdk:"display_name"`
	ProjectID   types.String `tfsdk:"project_id"`
	RoleID      types.String `tfsdk:"role_id"`

	Labels  types.Map  `tfsdk:"labels"`
	Blocked types.Bool `tfsdk:"blocked"`

	InviteStatus         types.String `tfsdk:"invite_status"`
	ExternalID           types.String `tfsdk:"external_id"`
	EffectiveDisplayName types.String `tfsdk:"effective_display_name"`
	InvitedBy            types.String `tfsdk:"invited_by"`
	CreatedAt            types.String `tfsdk:"created_at"`
}

type userInvitationResource struct{ c *client.Client }

// NewIAMUserInvitationResource — конструктор для реестра провайдера.
func NewIAMUserInvitationResource() resource.Resource { return &userInvitationResource{} }

func (r *userInvitationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_user_invitation"
}

func (r *userInvitationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// userStepUpHint — почему отказ в доступе мог прийти именно ЧЕЛОВЕКУ.
//
// Приглашение и блокировка классифицированы краем как смена уровня доступа и требуют
// повышенного подтверждения личности. Порог ИНТЕРАКТИВНЫЙ: служебная учётка от него
// освобождена правилом платформы, поэтому машинному вызывающему тот же отказ означает
// нехватку ПРАВ, а не второй фактор. Две причины под одним ответом — значит называем обе,
// а не выбираем за оператора.
//
// Подсказка приклеивается к ДВУМ разным вызовам, и право у них разное — поэтому названо
// не одно: приглашение проверяется на АККАУНТЕ (право редактора в нём), блокировка и
// снятие блокировки — на самой СТРОКЕ ЧЛЕНСТВА (право её изменять). Назвать здесь одно
// право значило бы отправить половину читателей выдавать себе не то, чего им не хватает.
const userStepUpHint = "\n\nЭто действие край относит к смене уровня доступа и требует " +
	"ПОВЫШЕННОГО подтверждения личности. Если провайдер работает под токеном человека, " +
	"полученным без второго фактора, — пройдите подтверждение и повторите. Служебная " +
	"учётка от этого порога освобождена правилом платформы: для неё тот же отказ означает " +
	"нехватку прав, а не второй фактор. Права проверяются РАЗНЫЕ и на разных объектах: " +
	"приглашение — право редактора на АККАУНТЕ, блокировка и снятие блокировки — право " +
	"изменять САМУ СТРОКУ ЧЛЕНСТВА."

func (r *userInvitationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// Затравки приглашения. Изменяющей операции у края для них НЕТ, поэтому правка
	// пересоздаёт ресурс — и это сказано вслух, а не спрятано за молчаливым игнорированием.
	seed := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Приглашение пользователя в аккаунт.\n\n" +
			"Пользователь у Kachō заводится ПРИГЛАШЕНИЕМ, а не созданием: ресурс заводит " +
			"строку членства в состоянии `PENDING`, а `ACTIVE` она становится сама, когда " +
			"приглашённый войдёт через провайдера личности. Ждать этого Terraform не будет — " +
			"`apply` завершается на отправленном приглашении.\n\n" +
			":::warning В состоянии Terraform лежат персональные данные\n" +
			"Почта и отображаемое имя приглашённого попадают в файл состояния в открытом " +
			"виде. Храните такое состояние там, где хранят персональные данные, и не " +
			"коммитьте его в репозиторий.\n:::\n\n" +
			":::danger Уничтожение ресурса снимает ЧЕЛОВЕКА с аккаунта\n" +
			"`destroy` удаляет строку членства: непринятое приглашение отзывается, а " +
			"вошедший человек теряет участие в аккаунте вместе с выпущенными на эту строку " +
			"токенами. Личность в других аккаунтах не затрагивается. Пересоздание " +
			"(любая правка неизменяемого поля) означает то же самое, только с последующим " +
			"новым приглашением.\n\n" +
			"В ожидание пересоздание НЕ возвращает: состояние членства выводится из " +
			"состояния личности, а вошедший человек перестал быть приглашённым навсегда — " +
			"новая строка членства становится `ACTIVE` сразу, и приглашения он не увидит. " +
			"Отозвать доступ пересозданием нельзя: его снимает `destroy` (вместе с " +
			"выпущенными на строку токенами) либо `blocked`, прекращающий выдачу новых " +
			"токенов.\n:::",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор строки членства (префикс `usr`). " +
					"По нему выполняется импорт.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},

			"account_id": schema.StringAttribute{Required: true, PlanModifiers: seed,
				MarkdownDescription: "Аккаунт, в который приглашают. Строка членства " +
					"принадлежит ровно одному аккаунту, перенести её нельзя — изменение " +
					"пересоздаёт ресурс."},
			"email": schema.StringAttribute{Required: true, PlanModifiers: seed,
				MarkdownDescription: "Почта приглашаемого. Край ищет по ней без учёта " +
					"регистра и по паре (аккаунт, почта) приглашает ИДЕМПОТЕНТНО: повторное " +
					"приглашение подберёт существующую строку, а не заведёт вторую. " +
					"Изменение почты — это приглашение другого человека, поэтому оно " +
					"пересоздаёт ресурс."},
			"display_name": schema.StringAttribute{Optional: true, PlanModifiers: seed,
				MarkdownDescription: "ЗАТРАВКА отображаемого имени. Край подставляет её в " +
					"строку членства при заведении и перезаписывает при первом входе " +
					"данными провайдера личности, а `UserService/Update` принимает " +
					"единственное изменяемое поле — `labels`. Поэтому значение здесь " +
					"остаётся тем, что задали вы, а фактическое " +
					"имя видно в `effective_display_name`. Если не задано — край выводит " +
					"имя из почты."},
			"project_id": schema.StringAttribute{Optional: true, PlanModifiers: seed,
				MarkdownDescription: "Проект стартовой выдачи. Вместе с `role_id` создаёт " +
					"привязку прав ОДНОЙ транзакцией с приглашением — приглашённый входит " +
					"уже с доступом.\n\n" +
					":::warning Созданная привязка этим ресурсом НЕ управляется\n" +
					"У приглашения нет способа прочитать её обратно или снять: край не " +
					"возвращает её идентификатор, а удаление строки членства привязку не " +
					"убирает. Если выдаче нужен жизненный цикл — заводите её отдельным " +
					"ресурсом привязки прав, а эти два поля не задавайте.\n:::"},
			"role_id": schema.StringAttribute{Optional: true, PlanModifiers: seed,
				MarkdownDescription: "Роль стартовой выдачи. Обязательна тогда и только " +
					"тогда, когда задан `project_id`."},

			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Метки строки ЛИЧНОСТИ — ЕДИНСТВЕННОЕ поле, которое " +
					"изменяющий запрос края принимает.\n\n" +
					"ТРЕБУЕТ ПРАВ АДМИНИСТРАТОРА ОБЛАКА (#1102). Человек — глобальная " +
					"личность: одна строка на все его аккаунты, поэтому метка, поставленная " +
					"из одного аккаунта, меняет состав селекторных выдач в остальных. Край " +
					"гейтит правку отношением `record_writer`, у которого нет источников " +
					"уровня аккаунта, — учётные данные арендатора получат здесь отказ.\n\n" +
					"Приглашение меток не принимает вовсе, поэтому ресурс с метками " +
					"заводится в два шага: пригласить, затем проставить. Между шагами " +
					"существует краткое окно, в котором строка меток не несёт."},
			"blocked": schema.BoolAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Запрещено ли этому человеку аутентифицироваться " +
					"на платформе.\n\n" +
					"ОБЛАСТЬ — ЛИЧНОСТЬ ЦЕЛИКОМ, а не один аккаунт: у человека одна строка на " +
					"все его аккаунты, поэтому запрет достаёт до каждого из них разом. " +
					"ТРЕБУЕТ ПРАВ АДМИНИСТРАТОРА ОБЛАКА (#1102) — край гейтит оба действия " +
					"отношением `identity_suspender`, у которого нет источников уровня " +
					"аккаунта. Распорядителю аккаунта, которому нужно вывести человека у СЕБЯ, " +
					"служит ИСКЛЮЧЕНИЕ ИЗ АККАУНТА (#1127) — оно и происходит при уничтожении " +
					"этого ресурса.\n\n" +
					"У края это не поле, а два ДЕЙСТВИЯ (`:block` / `:unblock`) — намеренно: " +
					"у действия нет маски изменения, поэтому запретить человеку вход, просто " +
					"забыв заполнить поле, нельзя. Провайдер отправляет действие только при " +
					"расхождении желаемого с прочитанным: край принимает повтор молча, но " +
					"пишет запись в журнал аудита на КАЖДЫЙ принятый вызов, и слать его на " +
					"каждый `apply` значило бы засорять след.\n\n" +
					"Непринятое приглашение (`PENDING`) заблокировать нельзя: внешней " +
					"личности у него ещё нет, запрещать нечего. Уже выданные токены " +
					"блокировка не отзывает — она прекращает выдачу новых."},

			"invite_status": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Состояние строки членства: `PENDING` — приглашение " +
					"отправлено, вход не состоялся; `ACTIVE` — человек вошёл; `BLOCKED` — " +
					"участие запрещено. Меняется входом человека и действиями блокировки, " +
					"а не правкой конфигурации."},
			"external_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Идентификатор личности у провайдера входа. Пуст, пока " +
					"приглашение не принято, — это и есть машинно-проверяемый признак того, " +
					"что человек ещё не входил."},
			"effective_display_name": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Отображаемое имя, которое край показывает СЕЙЧАС. " +
					"После первого входа оно приходит от провайдера личности и с затравкой " +
					"`display_name` совпадать не обязано."},
			"invited_by": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Пользователь, пригласивший этого. Пуст, если " +
					"приглашение отправила служебная учётка: пригласившего ЧЕЛОВЕКА в этом " +
					"случае нет, а выдумывать его край не станет."},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

// ValidateConfig ловит парность стартовой выдачи до отправки.
//
// Край требует роль тогда и только тогда, когда задан проект. Половина пары — отказ на
// стороне края, и он придёт после того, как оператор уже дождался операции; здесь он
// приходит на плане и с именем поля.
func (r *userInvitationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg userInvitationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// isSet считает НЕИЗВЕСТНОЕ заданным: ссылка на ещё не созданный проект или роль
	// приходит на этом этапе именно неизвестной, и счесть её пустой значило бы отвергнуть
	// совершенно законную конфигурацию.
	switch {
	case isSet(cfg.ProjectID) && !isSet(cfg.RoleID):
		resp.Diagnostics.AddError("Стартовая выдача задана наполовину",
			"Задан project_id, но не задан role_id. Приглашение создаёт привязку прав одной "+
				"транзакцией с собой, и без роли выдавать нечего — край отвергнет запрос.")
	case isSet(cfg.RoleID) && !isSet(cfg.ProjectID):
		resp.Diagnostics.AddError("Стартовая выдача задана наполовину",
			"Задан role_id, но не задан project_id. Роль выдаётся НА проект; без проекта у "+
				"выдачи нет области — край отвергнет запрос.")
	}
}

// ---- разбор ответа края ----------------------------------------------------------------

// userMembershipWire — форма ответа края. Разбор терпим к неизвестным полям: край добавляет их
// вперёд провайдера, и строгий разбор ломал бы чтение на первом же новом поле.
type userMembershipWire struct {
	ID string `json:"id"`
	// Аккаунта в ответе края БОЛЬШЕ НЕТ (kacho#471): у человека их столько,
	// сколько у него членств, и одного значения, которое можно было бы отдать,
	// не существует. Поле снято отсюда, а не оставлено «на всякий случай»:
	// разбор терпим к неизвестным полям, поэтому объявленное и никогда не
	// приходящее поле молча давало бы пустую строку — и она уехала бы в
	// состояние как факт.
	Email        string            `json:"email"`
	DisplayName  string            `json:"displayName"`
	ExternalID   string            `json:"externalId"`
	InviteStatus string            `json:"inviteStatus"`
	InvitedBy    string            `json:"invitedBy"`
	CreatedAt    string            `json:"createdAt"`
	Labels       map[string]string `json:"labels"`
}

// applyUserInvitation раскладывает ответ края по модели.
//
// Четыре поля НЕ трогаются намеренно — account_id, display_name, project_id, role_id.
// Три последних — затравки приглашения: первых двух в ответе нет вовсе, а имя край
// перезаписывает при первом входе человека. Аккаунт же край перестал называть вовсе
// (kacho#471): у человека их столько, сколько членств. Привести имя из ответа значило бы получить расхождение плана в тот день, когда
// приглашённый войдёт, — а поле пересоздающее, то есть Terraform предложил бы СНЯТЬ
// человека с аккаунта в ответ на то, что он принял приглашение.
func applyUserInvitation(ctx context.Context, m *userInvitationModel, raw []byte) error {
	var w userMembershipWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}
	m.ID = types.StringValue(w.ID)

	// Аккаунт СОХРАНЯЕТСЯ ЗА ВЫЗЫВАЮЩИМ: край его больше не называет (kacho#471),
	// и взять значение неоткуда. Присвоить пустую строку нельзя — поле
	// пересоздающее, и пустота в состоянии означала бы «снять человека с
	// аккаунта» в ответ на успешное чтение. При импорте своего значения ещё нет,
	// и поле остаётся незаполненным — это названная граница импорта, см. godoc
	// ImportState.

	// Написание почты сохраняется ЗА ВЫЗЫВАЮЩИМ, пока речь об одном и том же адресе.
	//
	// Край хранит почту так, как её записали при заведении строки, а ищет без учёта
	// регистра — значит при усыновлении существующей строки её написание может отличаться
	// от того, что стоит в конфигурации. Поле пересоздающее, и разница в одной заглавной
	// букве означала бы снятие человека с аккаунта. Тот же приём, что у состава целей
	// балансировщика (keepTargetsSpelling): совпало по существу — оставили написание
	// вызывающего. Отличается по существу (или своего значения ещё нет, как при импорте) —
	// берём значение края.
	if m.Email.IsNull() || m.Email.IsUnknown() || !strings.EqualFold(m.Email.ValueString(), w.Email) {
		m.Email = types.StringValue(w.Email)
	}

	m.Labels = mapToTF(ctx, w.Labels)
	m.InviteStatus = types.StringValue(w.InviteStatus)
	m.ExternalID = types.StringValue(w.ExternalID)
	m.EffectiveDisplayName = types.StringValue(w.DisplayName)
	m.InvitedBy = types.StringValue(w.InvitedBy)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.Blocked = types.BoolValue(w.InviteStatus == inviteStatusBlocked)
	return nil
}

// ---- CRUD --------------------------------------------------------------------------------

func (r *userInvitationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userInvitationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Желаемое запоминается ДО всего остального: sealUnknowns гасит неизвестные, а
	// обратное чтение перезаписывает эти же поля значениями края. Прочитать их позже
	// означало бы сравнить край сам с собой и никогда ничего не досылать.
	wantLabels := mapFromTF(ctx, plan.Labels)
	wantBlocked := plan.Blocked

	body := &iamv1.InviteUserRequest{
		AccountId:   plan.AccountID.ValueString(),
		Email:       plan.Email.ValueString(),
		DisplayName: plan.DisplayName.ValueString(),
		ProjectId:   plan.ProjectID.ValueString(),
		RoleId:      plan.RoleID.ValueString(),
	}

	raw, err := client.MarshalBody(body)
	if err != nil {
		resp.Diagnostics.AddError("Тело запроса не собрано", err.Error())
		return
	}
	// Адрес ключа повторной подачи — аккаунт: почта и без того входит в ключ через тело,
	// и собирать её отдельной строкой незачем.
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(
		"kacho_iam_user_invitation", plan.AccountID.ValueString(), raw)}

	httpResp, err := r.c.Do(ctx, http.MethodPost, userMembershipPath+":invite", body, hdr)
	if err != nil {
		resp.Diagnostics.AddError("Приглашение не отправлено", err.Error())
		return
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		detail := out.Message
		if out.Kind == client.OutcomeDenied {
			detail += userStepUpHint
		}
		resp.Diagnostics.AddError("Приглашение отвергнуто", detail)
		return
	}

	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	done, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{})
	if err != nil {
		resp.Diagnostics.AddError("Приглашение не завершилось", err.Error())
		return
	}

	// Идентификатор берётся из ОТВЕТА операции, а НЕ из её метаданных, и это не стилевой
	// выбор.
	//
	// Общее правило платформы — читать метаданные только после проверки отказа, потому что
	// идентификатор чеканится при приёме запроса. Здесь мало и этого: приглашение
	// идемпотентно, и когда край подбирает УЖЕ СУЩЕСТВУЮЩУЮ строку членства, заранее
	// выделенный идентификатор из метаданных не соответствует НИЧЕМУ (сама реализация края
	// это оговаривает — «при idempotent возврате existing-row id игнорируется»). Записав
	// его, провайдер получил бы фантом: успешный apply, состояние с идентификатором, за
	// которым нет строки, и «не найдено» при первом же чтении.
	//
	// В ответе операции лежит сам ресурс — та строка, которую край действительно завёл или
	// подобрал.
	//
	// Магической ссылки для входа приглашённого здесь нет намеренно. Контракт объявляет
	// поле `magic_link_url` в метаданных, но производителя у него в дереве НОЛЬ (предикат:
	// `git grep -n MagicLinkUrl -- services/`; замер 2026-08-12) — шаг выпуска ссылки снят
	// вместе с клиентом провайдера входа. Показывать поле, которого край не заполняет
	// никогда, значило бы обещать возможность, которой нет. Вернётся производитель —
	// вернётся и атрибут.
	id, _ := done.ResponseString("id")
	if id == "" {
		resp.Diagnostics.AddError("Приглашение отправлено, но строка членства не адресуема",
			"Операция завершилась успехом, а идентификатора строки в её ответе нет — "+
				"записывать в состояние нечего.\n\nПовторите apply: приглашение идемпотентно "+
				"по паре (аккаунт, почта) и подберёт ту же строку, дубля не возникнет. "+
				"Аккаунт: "+plan.AccountID.ValueString()+".")
		return
	}

	// Идентификатор в состояние ДО обратного чтения: это единственная точка, где потеря
	// необратима — сорвись чтение сейчас, строка существует, а Terraform о ней не знает.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply и отвечает на каждое отдельным сообщением, среди которых
	// настоящая причина — одна строка про неудавшееся чтение — тонет.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw2, err := readByID(ctx, r.c, userMembershipPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError(userInvitationHuman+" отправлено, но не прочитано обратно", err.Error())
		return
	}
	if err := applyUserInvitation(ctx, &plan, raw2); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	// Прочитанное записывается ДО донастройки: сорвись она, состояние уже описывает
	// заведённую строку, а не один голый идентификатор.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Второй шаг заведения: метки приглашение не принимает вовсе, запрет входа задаётся
	// действием. Пропустить их молча нельзя — вызывающий их задал и вправе получить
	// применёнными.
	sent, err := r.reconcileAfterInvite(ctx, id, wantLabels, wantBlocked, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Приглашение отправлено, но донастройка не завершилась", err.Error())
		return
	}
	if !sent {
		return
	}
	raw3, err := readByID(ctx, r.c, userMembershipPath, id, false)
	if err != nil {
		resp.Diagnostics.AddError(userInvitationHuman+" донастроено, но не прочитано обратно", err.Error())
		return
	}
	if err := applyUserInvitation(ctx, &plan, raw3); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// reconcileAfterInvite досылает то, чего приглашение не принимает.
//
// Возвращает признак «что-то отправлялось»: обратное чтение стоит запроса, и делать его
// там, где ничего не менялось, незачем.
func (r *userInvitationResource) reconcileAfterInvite(ctx context.Context, id string,
	wantLabels map[string]string, wantBlocked types.Bool, have *userInvitationModel) (bool, error) {
	sent := false
	if len(wantLabels) > 0 {
		if err := r.patchLabels(ctx, id, wantLabels); err != nil {
			return sent, fmt.Errorf("проставление меток: %w", err)
		}
		sent = true
	}
	if wantBlocked.IsNull() || wantBlocked.IsUnknown() {
		return sent, nil
	}
	changed, err := r.setBlocked(ctx, id, have.InviteStatus.ValueString(),
		have.Blocked.ValueBool(), wantBlocked.ValueBool())
	if err != nil {
		return sent, err
	}
	return sent || changed, nil
}

// patchLabels — единственное изменение, которое край принимает маской.
func (r *userInvitationResource) patchLabels(ctx context.Context, id string, labels map[string]string) error {
	return awaitMutation(ctx, r.c, http.MethodPatch, userMembershipPath+"/"+id, &iamv1.UpdateUserRequest{
		UserId:     id,
		UpdateMask: fieldMask([]string{"labels"}),
		Labels:     labels,
	})
}

// setBlocked приводит запрет входа к желаемому ОДНИМ действием края.
//
// Возвращает признак «действие отправлялось». Отправляется оно только при расхождении:
// край принимает повтор молча (аргумент — состояние, а не переход), но пишет запись в
// журнал аудита на каждый принятый вызов, и слать его на каждый apply значило бы засорять
// след ровно тем, что в нём ищут.
func (r *userInvitationResource) setBlocked(ctx context.Context, id, inviteStatus string, have, want bool) (bool, error) {
	if have == want {
		return false, nil
	}
	// Непринятое приглашение отвергают ОБА направления: внешней личности у него нет, а
	// значит нечего ни запрещать, ни разрешать. Отказ края верен, но говорит про строку;
	// здесь он говорит про то, что делать дальше.
	if inviteStatus == inviteStatusPending {
		if !want {
			// Просят «не заблокировано» — оно и не заблокировано. Отправлять снятие
			// запрета было бы обращением к краю за тем, что уже верно, и получить в ответ
			// отказ.
			return false, nil
		}
		return false, fmt.Errorf(
			"запретить вход строке членства %s нельзя: приглашение ещё не принято (состояние %s), "+
				"внешней личности у него нет и запрещать нечего. Отзывают такое приглашение "+
				"уничтожением ресурса, а не блокировкой", id, inviteStatus)
	}
	// Вызов идёт не через общий awaitMutation, а своим кодом ровно ради одного: у отказа
	// в доступе здесь ДВЕ разные причины (нехватка прав и непройденное подтверждение
	// личности), и различить их край не даёт. Общий путь вернул бы сообщение края без
	// этого пояснения, а оно тут и есть самое полезное.
	verb := ":unblock"
	var body proto.Message = &iamv1.UnblockUserRequest{UserId: id}
	if want {
		verb = ":block"
		body = &iamv1.BlockUserRequest{UserId: id}
	}
	httpResp, err := r.c.Do(ctx, http.MethodPost, userMembershipPath+"/"+id+verb, body, nil)
	if err != nil {
		return false, fmt.Errorf("действие %s над строкой членства %s: %w", verb, id, err)
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		detail := out.Message
		if out.Kind == client.OutcomeDenied {
			detail += userStepUpHint
		}
		return false, fmt.Errorf("действие %s над строкой членства %s: %s", verb, id, detail)
	}
	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
		if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
			return false, fmt.Errorf("действие %s над строкой членства %s: %w", verb, id, err)
		}
	}
	return true, nil
}

func (r *userInvitationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userInvitationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, userMembershipPath, state.ID.ValueString(), false)
	if err == nil {
		if err := applyUserInvitation(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение не удалось: "+userInvitationHuman, err.Error())
		return
	}

	remove, title, detail := r.confirmAbsence(ctx, state.AccountID.ValueString(), state.ID.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

// confirmAbsence устанавливает, что означает «строки нет по идентификатору».
//
// Общий подтверждатель отсутствия здесь не применим: он сужает контрольный список
// фильтром по ИМЕНИ, а у строки членства имени нет — край фильтрует список по почте,
// внешнему идентификатору и состоянию. Слать почту в строке запроса нельзя отдельно:
// адрес вызова попадает в текст ошибки транспорта, и персональные данные утекли бы в
// диагностику. Поэтому перепись идёт по НЕсуженному списку аккаунта, а искомое — наш
// идентификатор.
//
// Исходы те же три, и порядок тот же:
//
//  1. идентификатор найден в списке — строка жива, первый ответ был окном прав;
//  2. список прочитан целиком, наш идентификатор в нём не встретился, но строки в аккаунте
//     есть ⇒ пообъектная выдача работает ⇒ наша действительно удалена;
//  3. список отвечает отказом, пуст целиком или не дочитан до конца ⇒ различить нечем ⇒
//     останавливаемся, а не решаем за оператора.
//
// Слепая зона названа честно: строка, существующая, но невидимая нам пообъектно (точечно
// отозванный доступ), будет принята за удалённую. Цена этой ошибки здесь НИЖЕ, чем у
// прочих ресурсов: приглашение идемпотентно по паре (аккаунт, почта), поэтому повторное
// заведение подберёт ту же самую строку, а не заведёт вторую.
func (r *userInvitationResource) confirmAbsence(ctx context.Context, accountID, id string) (remove bool, title, detail string) {
	const (
		pageSize = 100
		maxPages = 20
	)
	stop := func(reason string) (bool, string, string) {
		return false, "Отсутствие не подтверждено",
			userInvitationHuman + " " + id + " не читается, и подтвердить отсутствие нечем: " +
				reason + ". Различить «удалено» и «доступ отозван» край не позволяет — ответы " +
				"совпадают дословно.\n\nЕсли строка членства действительно снята вне Terraform, " +
				"уберите ресурс из состояния вручную:\n  terraform state rm <адрес ресурса>"
	}

	token := ""
	seen := 0
	for page := 0; page < maxPages; page++ {
		q := url.Values{}
		q.Set("accountId", accountID)
		q.Set("pageSize", fmt.Sprint(pageSize))
		if token != "" {
			q.Set("pageToken", token)
		}
		httpResp, err := r.c.Do(ctx, http.MethodGet, userMembershipPath+"?"+q.Encode(), nil, nil)
		if err != nil {
			return stop("список аккаунта не прочитан: " + err.Error())
		}
		switch out := client.Classify(httpResp); out.Kind {
		case client.OutcomeOK:
		case client.OutcomeDenied, client.OutcomeUnauthenticated:
			return false, "Доступ к аккаунту утрачен",
				userInvitationHuman + " " + id + " не читается, и список аккаунта " +
					accountID + " тоже отвечает отказом. Это событие ПРАВ, а не удаление " +
					"строки: продолжать нельзя, иначе план предложит заново пригласить людей, " +
					"чьё участие цело."
		default:
			return stop("список аккаунта: " + out.Message)
		}

		var body struct {
			Users []struct {
				ID string `json:"id"`
			} `json:"users"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(httpResp.Body, &body); err != nil {
			return stop("список аккаунта не разобран: " + err.Error())
		}
		for _, u := range body.Users {
			seen++
			if u.ID == id {
				// Строка жива — расхождения нет, снимать из состояния нечего.
				return false, "", ""
			}
		}
		token = body.NextPageToken
		if token == "" {
			if seen == 0 {
				return stop("в аккаунте не видно ни одной строки членства, включая ту, " +
					"под которой работает сам вызывающий")
			}
			return true, "", ""
		}
	}
	// Перепись не завершена — значит она ничего не устанавливает. Ложная остановка стоит
	// одной команды оператора, ложное «удалено» — повторного приглашения живых людей.
	return stop(fmt.Sprintf("перепись прервана на пределе в %d страниц (просмотрено строк: %d), "+
		"а продолжение списка не исчерпано", maxPages, seen))
}

func (r *userInvitationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userInvitationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	// Изменяемого у края ровно два предмета, и они меняются РАЗНЫМИ способами: метки —
	// запросом с маской, запрет входа — действием. Всё прочее сюда не доходит: пересоздающие
	// модификаторы схемы разбирают такие правки раньше, на плане.
	if !plan.Labels.Equal(state.Labels) {
		if err := r.patchLabels(ctx, id, mapFromTF(ctx, plan.Labels)); err != nil {
			resp.Diagnostics.AddError("Изменение меток не завершилось", err.Error())
			return
		}
	}
	if !plan.Blocked.IsNull() && !plan.Blocked.IsUnknown() {
		if _, err := r.setBlocked(ctx, id, state.InviteStatus.ValueString(),
			state.Blocked.ValueBool(), plan.Blocked.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Изменение запрета входа не завершилось", err.Error())
			return
		}
	}

	// Обратное чтение выполняется ВСЕГДА, даже когда ничего не отправлялось: в плане
	// вычисляемые атрибуты неизвестны, а состояние неизвестного не хранит.
	plan.ID = state.ID
	raw, err := readByID(ctx, r.c, userMembershipPath, id, false)
	if err != nil {
		resp.Diagnostics.AddError(userInvitationHuman+" изменено, но не прочитано обратно", err.Error())
		return
	}
	if err := applyUserInvitation(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// deleteUserInvitationHint — что мешает снять строку членства. Говорится ЗАРАНЕЕ: иначе
// причина выясняется перебором.
//
// ПЕРЕЧЕНЬ СУЗИЛСЯ ВМЕСТЕ С ПРЕДМЕТОМ (#1127). Уничтожение этого ресурса больше не стирает
// строку ЛИЧНОСТИ — оно ИСКЛЮЧАЕТ человека из названного аккаунта, — поэтому ссылки, которые
// держали личность (владение аккаунтом, чужие ключи, отметки об отзыве сессий, членство в
// группе другого аккаунта), уничтожению этого ресурса больше не мешают. Остаётся одна
// помеха, и она по существу: действующая привязка прав этого человека В ЭТОМ аккаунте.
// Держит её отложенный страж базы, а не наш порядок вызовов, — значит «сперва отозвать
// права, потом исключить» есть конструкция, а не памятка.
//
// Перечень НАМЕРЕННО не объявлен закрытым. Прежняя редакция говорила «все три помехи» и
// называла только ограничения внешнего ключа (владение аккаунтом, чужие ключи, отметки об
// отзыве сессий) — а край отвергает удаление ещё по двум причинам, которые проверяются
// отдельным условием в самом операторе удаления, а не ключом: действующая привязка прав, где
// пользователь субъект, и членство в группе. Именно эти две и встречаются первыми, а
// закрытый перечень, в котором их нет, читается как «других причин не бывает» и уводит от
// настоящей.
//
// ПЕРВУЮ из них производит ЭТОТ ЖЕ ресурс: стартовая выдача (`project_id` + `role_id`)
// создаёт привязку прав, которую снятие строки членства не убирает (это сказано и в схеме).
// То есть приглашение с заданной выдачей по умолчанию НЕ уничтожается, пока привязку не
// снять отдельно, — и без этой строки оператор ищет причину там, где её нет.
const deleteUserInvitationHint = "Исключению мешает действующая привязка прав этого " +
	"человека В ЭТОМ аккаунте — в том числе привязка СТАРТОВОЙ ВЫДАЧИ, созданная этим же " +
	"приглашением через project_id + role_id: снятие членства её не убирает (это сказано и " +
	"в схеме). Снимите привязку и повторите. Личность человека и его участие в ДРУГИХ " +
	"аккаунтах этим действием не затрагиваются вовсе."

func (r *userInvitationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userInvitationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// УНИЧТОЖЕНИЕ ЭТОГО РЕСУРСА — ИСКЛЮЧЕНИЕ ИЗ АККАУНТА, А НЕ СНЯТИЕ ЛИЧНОСТИ (#1127).
	//
	// Здесь стоял `DELETE /iam/v1/users/{id}`, и это перестало быть верным дважды. По
	// СУЩЕСТВУ: ресурс моделирует ЧЛЕНСТВО («приглашение в аккаунт»), а тот вызов стирал
	// глобальную строку личности — то есть уничтожение одного ресурса выносило человека из
	// ВСЕХ его аккаунтов сразу, включая чужие. По ПРАВАМ: край перевёл снятие строки на
	// отношение без источников уровня аккаунта (#1131), поэтому учётные данные арендатора
	// получили бы здесь отказ, и `terraform destroy` перестал бы работать у всех, кроме
	// администратора облака.
	//
	// Исключение называет ПАРУ, потому что членство и есть пара: у человека аккаунтов
	// бывает несколько, и вывести второй половиной из первой нельзя.
	err := awaitMutation(ctx, r.c, http.MethodPost,
		userMembershipPath+"/"+state.ID.ValueString()+":removeFromAccount",
		&iamv1.RemoveUserFromAccountRequest{
			UserId:    state.ID.ValueString(),
			AccountId: state.AccountID.ValueString(),
		})
	if err == nil {
		return
	}
	// Отказ края передаётся дословно: он называет предмет — человека и аккаунт. Пересказ
	// своими словами потерял бы ровно то, что нужно оператору.
	resp.Diagnostics.AddError("Исключение из аккаунта не завершилось: "+userInvitationHuman,
		err.Error()+"\n\n"+deleteUserInvitationHint)
}

// ImportState принимает идентификатор строки членства.
//
// Импорт здесь ЧАСТИЧЕН, и это надо знать до того, как за ним пойдёт apply: затравки
// приглашения (`display_name`, `project_id`, `role_id`) край обратно не отдаёт, поэтому
// после импорта они пусты. Если в конфигурации они заданы, первый же план увидит
// расхождение по пересоздающему полю и предложит СНЯТЬ человека с аккаунта и пригласить
// заново. Импортируя существующее членство, эти три поля в конфигурации не задавайте —
// управляйте метками и запретом входа.
//
// С kacho#471 к ним прибавился ЧЕТВЁРТЫЙ — `account_id`. Человек перестал принадлежать
// одному аккаунту, поэтому чтение его строки больше не отвечает, в каком аккаунте
// состоит ИМЕННО ЭТО членство, и вывести значение неоткуда. Пока публичного ресурса
// членства нет, импорт этого ресурса аккаунта не восстанавливает: импортируйте только
// туда, где `account_id` в конфигурации совпадает с фактическим, иначе первый план
// предложит пересоздание.
func (r *userInvitationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Префикс — литерал, и это не второй словарь: общий каталог платформы держит префиксы
	// iam точно так же (их владелец — сервис iam, а его внутренние константы в общую
	// библиотеку не импортируются). Принадлежность каталогу и форму проверяет сам importByID.
	importByID(ctx, userInvitationHuman, "usr", req, resp)
}

var _ resource.Resource = (*userInvitationResource)(nil)
var _ resource.ResourceWithConfigure = (*userInvitationResource)(nil)
var _ resource.ResourceWithImportState = (*userInvitationResource)(nil)
var _ resource.ResourceWithValidateConfig = (*userInvitationResource)(nil)
