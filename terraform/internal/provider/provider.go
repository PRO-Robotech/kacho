// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package provider — Terraform-провайдер Kachō.
package provider

import (
	"context"
	"crypto/x509"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// Version подставляется при сборке релиза; «dev» — местная сборка.
var Version = "dev"

type kachoProvider struct{}

// New — точка входа плагина.
func New() provider.Provider { return &kachoProvider{} }

func (p *kachoProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = providerTypeName
	resp.Version = Version
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
	CABundle types.String `tfsdk:"ca_bundle"`
	Timeout  types.String `tfsdk:"timeout"`
}

func (p *kachoProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Провайдер облачной платформы Kachō. " +
			"Работает с публичным краем по HTTP и требует токен, выпущенный IAM.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Базовый адрес края, например `https://api.kacho.example`. " +
					"Можно задать переменной окружения `KACHO_ENDPOINT`. " +
					"Умолчания нет намеренно: адрес, выведенный из чужого значения, всегда " +
					"непуст, и провайдер выглядел бы настроенным, обращаясь в никуда.",
			},
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Bearer-токен, выпущенный IAM. Можно задать переменной " +
					"окружения `KACHO_TOKEN`.",
			},
			"ca_bundle": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Путь к PEM-файлу с корнями доверия для края " +
					"(переменная `KACHO_CA_BUNDLE`). Нужен, когда сертификат края выпущен " +
					"внутренним центром сертификации. Отключения проверки сертификата у " +
					"провайдера НЕТ: непроверяемое соединение — это не настройка, а дыра.",
			},
			"timeout": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Срок ОДНОГО вызова края, например `30s`. Не путать с " +
					"ожиданием асинхронной операции: у него свой бюджет (по умолчанию 5 минут).",
			},
		},
	}
}

func (p *kachoProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := firstNonEmpty(cfg.Endpoint.ValueString(), os.Getenv("KACHO_ENDPOINT"))
	token := firstNonEmpty(cfg.Token.ValueString(), os.Getenv("KACHO_TOKEN"))
	caPath := firstNonEmpty(cfg.CABundle.ValueString(), os.Getenv("KACHO_CA_BUNDLE"))

	var roots *x509.CertPool
	if caPath != "" {
		// Путь называет САМ оператор в своей конфигурации, и провайдер работает от его
		// имени: обхода границы тут нет by construction — читать чужое ему нечем, прав
		// ровно столько же, сколько у запустившего.
		//
		// Тем не менее путь нормализуется и проверяется на обычный файл: так каталог или
		// устройство дают внятный отказ вместо невнятной ошибки чтения, а `..` в записи
		// не меняет смысла молча.
		clean := filepath.Clean(caPath)
		if st, err := os.Stat(clean); err != nil || !st.Mode().IsRegular() {
			resp.Diagnostics.AddError("Файл корней доверия не читается",
				"Атрибут ca_bundle указывает на "+clean+", но это не обычный файл. "+
					"Укажите путь к PEM-файлу с сертификатами.")
			return
		}
		pem, err := os.ReadFile(clean) // #nosec G304 G703 -- путь задан оператором в его же конфигурации; нормализован и проверен на обычный файл строкой выше
		if err != nil {
			resp.Diagnostics.AddError("Не читается файл корней доверия",
				"Атрибут ca_bundle указывает на "+clean+", но файл не прочитан: "+err.Error())
			return
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			resp.Diagnostics.AddError("В файле корней доверия нет сертификатов",
				"Файл "+clean+" прочитан, но ни одного сертификата в формате PEM в нём нет. "+
					"Пустой набор корней означал бы, что доверять нечему, и каждое соединение "+
					"отвергалось бы — это молчаливая поломка, поэтому она названа здесь.")
			return
		}
	}

	var timeout time.Duration
	if s := firstNonEmpty(cfg.Timeout.ValueString(), os.Getenv("KACHO_TIMEOUT")); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			resp.Diagnostics.AddError("Негодный срок вызова",
				"Значение timeout="+s+" не разбирается как длительность Go (например 30s, 1m): "+err.Error())
			return
		}
		timeout = d
	}

	c, err := client.New(client.Config{
		Endpoint: endpoint,
		Token:    token,
		TLSRoots: roots,
		Timeout:  timeout,
	})
	if err != nil {
		resp.Diagnostics.AddError("Провайдер Kachō не настроен", err.Error())
		return
	}
	if token == "" {
		resp.Diagnostics.AddError("Токен не задан",
			"Укажите атрибут token в блоке provider \"kacho\" либо переменную окружения "+
				"KACHO_TOKEN. Край работает в боевой посадке и отвергает запрос без "+
				"учётных данных — без токена не выполнится ни одно чтение.")
		return
	}

	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *kachoProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		// vpc
		NewNetworkResource,
		NewSubnetResource,
		newFlatResource(vpcNetworkInterfaceSpec),
		newFlatResource(vpcGatewaySpec),
		NewVPCAddressResource,
		NewVPCRouteTableResource,
		NewVPCSecurityGroupResource,
		NewVPCCidrGroupResource,
		// iam
		NewIAMProjectResource,
		NewIAMGroupResource,
		NewIAMServiceAccountResource,
		NewIAMSAKeyResource,
		newFlatResource(iamAccountSpec),
		NewIAMRoleResource,
		NewIAMAccessBindingResource,
		NewIAMUserInvitationResource,
		NewIAMUserTokenResource,
		// storage
		newFlatResource(storageVolumeSpec),
		newFlatResource(storageSnapshotSpec),
		newFlatResource(storageImageSpec),
		// registry
		newFlatResource(registryRegistrySpec),
		NewRegistryRepositoryResource,
		// compute
		NewComputeInstanceResource,
		// nlb
		NewNLBTargetGroupResource,
		NewNLBLoadBalancerResource,
		NewNLBListenerResource,
		// compute — ключ входа гостя и группа размещения (заведены стволом, #288).
		// Ресурс самой машины здесь НЕ повторяется: он объявлен выше, полной
		// реализацией; вторая, с тем же именем типа, снята как дубль.
		NewGuestAccessKeyResource,
		NewPlacementGroupResource,
	}
}

// Data-sources появятся в TF-2 вместе с остальными ресурсами vpc: их путь чтения тот же,
// и заводить их до того, как многошаговое чтение доказано на ресурсах, значило бы
// размножить непроверенное.
// DataSources — чтение существующего БЕЗ взятия под управление.
//
// Их отсутствие было второй слепой зоной покрытия: гейты спрашивали «каждый ли создаваемый
// ресурс управляем» и молчали про «каждый ли читаемый читаем». Справочники создающих
// глаголов не имеют вовсе, поэтому ресурсами быть не могут — и до появления источников
// сослаться на зону, регион или тип диска было нечем.
func (p *kachoProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		// geo — справочник оси размещения
		NewGeoRegionDataSource,
		NewGeoRegionsDataSource,
		NewGeoZoneDataSource,
		NewGeoZonesDataSource,
		// storage / compute — справочники типов
		NewStorageDiskTypeDataSource,
		NewStorageDiskTypesDataSource,
		NewComputeMachineTypeDataSource,
		NewComputeMachineTypesDataSource,
		// поиск уже существующего по имени — чтобы сослаться, не импортируя
		NewVPCNetworkByNameDataSource,
		NewVPCSubnetByNameDataSource,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
