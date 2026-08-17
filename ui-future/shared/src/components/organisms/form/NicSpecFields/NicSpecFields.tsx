// NicSpecFields — bespoke-рендер NIC-секции формы создания Instance
// (поле `network_interface_specs[i]`, тип CustomField).
//
// Заменяет прежний набор полей {subnet_id + primary_v4_address_spec.address +
// _nat checkbox + nic_id ref} на:
//   1. Cascader «Сеть → Подсеть → [Адрес | (без адреса) | + Создать адрес…]»
//      (3 уровня: пользователь просил 2, но «без адреса» всё равно требует
//      подсеть, а адреса логически принадлежат подсети — поэтому подсеть как
//      промежуточный уровень; уровень-Сеть с встроенным поиском). Leaf-выбор
//      пишет `subnet_id` и (если выбран адрес) `primary_v4_address_spec.address`
//      = `internal_ipv4_address.address`. «+ Создать адрес…» открывает
//      InlineResourceCreateForm(addresses, kind=internal, subnet pre-filled).
//   2. Segmented «Без адреса | Автоматически | Список» для external (one-to-one
//      NAT). «Автоматически» → one_to_one_nat_spec = {ip_version:"IPV4"}.
//      «Список» → Select внешних Address-ресурсов + «+ Создать адрес…»
//      (kind=external); выбор пишет one_to_one_nat_spec = {address:<IP>}.
//   3. (advanced) toggle «Использовать существующий сетевой интерфейс» → ref на
//      network-interfaces; если задан — sanitize отдаёт только nic_id.
//
// Все «сырые» поля (`subnet_id`, `primary_v4_address_spec.address`,
// `_ext_mode`, `_ext_addr_value`, …) живут в объекте формы; sanitizeInstanceCreate
// собирает из них wire-shape и вычищает `_*`-служебные ключи.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Cascader, Modal, Segmented, Select, Switch, Typography } from "antd";
import { api } from "@shared/api/client";
import { getResource } from "@shared/lib/resource-registry";
import { useProjectStore } from "@shared/lib/context-store";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScope, pickerScopeOfSpec } from "@shared/lib/picker-search";
import { getByPath, setByPath, deleteByPath } from "@shared/lib/path";
import { Label } from "@shared/components/atoms/ui/Input";
import { RefSelect } from "@shared/components/organisms/form/RefSelect";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { InlineResourceCreateForm } from "@shared/components/organisms/InlineResourceCreateForm";
import { displayText } from "@shared/lib/display-text";

interface AnyRec {
  id: string;
  name?: string;
  [k: string]: unknown;
}
interface AddressRec extends AnyRec {
  internal_ipv4_address?: { subnet_id?: string; address?: string };
  external_ipv4_address?: { address?: string };
}

interface Props {
  pathPrefix: string; // "network_interface_specs[0]"
  value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
}

const CASCADER_CREATE_PREFIX = "__create__:"; // value = "__create__:<subnetId>"
const CASCADER_NOADDR_PREFIX = "__noaddr__:"; // value = "__noaddr__:<subnetId>"

// Область поиска ДЕРЕВА «сеть → подсеть → адрес» (#528).
//
// Владельцы всех трёх списков сужать умеют (`serverSearchField` в реестре), но
// ДЕРЕВО так сузить нельзя: совпадение на уровне адреса показывается только
// вместе со своими предками, а ответ, суженный по имени, предков не содержит —
// подсеть и сеть найденного адреса просто не пришли бы, и лист оказался бы
// невидим. Поэтому здесь ввод остаётся в браузере ОСОЗНАННО, и поле говорит об
// этом вместо «нет совпадений»: три списка прочитаны по одной странице каждый,
// и совпадение за страницей это поле не покажет — но и не утверждает, что его
// нет. Незакрытым остаётся именно это: адрес (подсеть, сеть) за первой
// страницей недостижим отсюда; закрыть его можно только другой формой выбора,
// а не подписью.
const TREE_SCOPE = pickerScope(undefined);
const TREE_NOTICE =
  "Ввод сужает только загруженные варианты: дерево собрано из трёх списков (сети, подсети, адреса), " +
  "каждый прочитан одной страницей. Совпадение за страницей это поле не покажет — и не утверждает, что его нет.";

export function NicSpecFields({ pathPrefix, value, onChange }: Props) {
  const project = useProjectStore((s) => s.project);
  const addressesSpec = getResource("addresses");

  const get = (rel: string) => getByPath(value, `${pathPrefix}.${rel}`);
  const set = (rel: string, v: unknown) => onChange(setByPath(value, `${pathPrefix}.${rel}`, v));
  const del = (rel: string) => onChange(deleteByPath(value, `${pathPrefix}.${rel}`));

  const subnetId = get("subnet_id") as string | undefined;
  const primaryAddr = (getByPath(value, `${pathPrefix}.primary_v4_address_spec.address`) as string | undefined) ?? "";
  const cascaderPath = get("_addr_cascader") as string[] | undefined;
  const nicId = get("nic_id") as string | undefined;
  const useExisting = !!get("_use_existing_nic") || !!nicId;
  const extMode = (get("_ext_mode") as string | undefined) ?? "none";
  const extAddrId = get("_ext_addr_id") as string | undefined;

  // ----- data: networks + subnets + internal IPv4 addresses (project-scoped) -----
  const enabled = !!project;
  const networksQ = useQuery({
    queryKey: ["nic-cascader-networks", project?.id],
    queryFn: () => api.list<{ networks: AnyRec[] }>("/vpc/v1/networks", { project_id: project!.id, pageSize: "1000" }),
    enabled,
    staleTime: 30_000,
  });
  const subnetsQ = useQuery({
    queryKey: ["nic-cascader-subnets", project?.id],
    queryFn: () => api.list<{ subnets: AnyRec[] }>("/vpc/v1/subnets", { project_id: project!.id, pageSize: "1000" }),
    enabled,
    staleTime: 30_000,
  });
  const addressesQ = useQuery({
    queryKey: ["nic-cascader-addresses", project?.id],
    queryFn: () =>
      api.list<{ addresses: AddressRec[] }>("/vpc/v1/addresses", { project_id: project!.id, pageSize: "1000" }),
    enabled,
    staleTime: 15_000,
  });

  // Публичные адреса читаются ОТДЕЛЬНЫМ запросом, а не тем же, что кормит
  // дерево. Причина не в экономии: ввод в этом списке уходит на сервер (#528),
  // и общий ответ означал бы, что набор в поле «Список» молча сужает ещё и
  // дерево «сеть → подсеть → адрес» выше — сужение одного поля меняло бы
  // содержимое другого, которого человек не трогал. Запрос идёт только в режиме
  // «Список», то есть ровно тогда, когда это поле показано.
  const extScope = pickerScopeOfSpec(addressesSpec);
  const [extTerm, setExtTerm] = useState("");
  const debouncedExtTerm = useDebouncedValue(extTerm, extScope.asksServer ? 250 : 0);
  const extServerQuery = extScope.asksServer ? extScope.query(debouncedExtTerm) : {};
  // Ключ запроса несёт ввод ТОЛЬКО когда сужает сервер: иначе каждое нажатие
  // клавиши сбрасывало бы кэш и перечитывало один и тот же список.
  const extTermKey = extScope.asksServer ? (extServerQuery.filter ?? "") : "";
  const extAddressesQ = useQuery({
    queryKey: ["nic-external-addresses", project?.id, extTermKey],
    queryFn: () =>
      api.list<{ addresses: AddressRec[] }>("/vpc/v1/addresses", {
        ...extServerQuery,
        project_id: project!.id,
        pageSize: "1000",
      }),
    enabled: enabled && extMode === "list",
    staleTime: 15_000,
  });

  const networks = useMemo(() => networksQ.data?.networks ?? [], [networksQ.data?.networks]);
  const subnets = useMemo(() => subnetsQ.data?.subnets ?? [], [subnetsQ.data?.subnets]);
  const allAddresses = useMemo(() => addressesQ.data?.addresses ?? [], [addressesQ.data?.addresses]);
  const internalAddrs = useMemo(() => allAddresses.filter((a) => !!a.internal_ipv4_address), [allAddresses]);
  const externalAddrs = useMemo(
    () => (extAddressesQ.data?.addresses ?? []).filter((a) => !!a.external_ipv4_address),
    [extAddressesQ.data?.addresses],
  );

  const cascaderOptions = useMemo(() => {
    const addrsBySubnet = new Map<string, AddressRec[]>();
    for (const a of internalAddrs) {
      const sid = a.internal_ipv4_address?.subnet_id;
      if (!sid) continue;
      (addrsBySubnet.get(sid) ?? addrsBySubnet.set(sid, []).get(sid)!).push(a);
    }
    const subnetsByNetwork = new Map<string, AnyRec[]>();
    for (const s of subnets) {
      const nid = s.network_id as string | undefined;
      if (!nid) continue;
      (subnetsByNetwork.get(nid) ?? subnetsByNetwork.set(nid, []).get(nid)!).push(s);
    }
    return networks.map((net) => ({
      value: net.id,
      label: net.name ? `${net.name}` : net.id,
      children: (subnetsByNetwork.get(net.id) ?? []).map((sub) => {
        const sid = sub.id;
        const addrLeaves = (addrsBySubnet.get(sid) ?? []).map((a) => {
          const ip = a.internal_ipv4_address?.address;
          return {
            value: a.id,
            label: ip ? `${a.name ? a.name + " — " : ""}${ip}` : (a.name ?? a.id),
          };
        });
        return {
          value: sid,
          label: sub.name ? `${sub.name}${sub.zone_id ? ` (${displayText(sub.zone_id)})` : ""}` : sid,
          children: [
            { value: `${CASCADER_NOADDR_PREFIX}${sid}`, label: "(без адреса)" },
            ...addrLeaves,
            { value: `${CASCADER_CREATE_PREFIX}${sid}`, label: "+ Создать адрес…" },
          ],
        };
      }),
    }));
  }, [networks, subnets, internalAddrs]);

  // ----- inline create modals -----
  const [createInternalSubnet, setCreateInternalSubnet] = useState<string | null>(null);
  const [createExternal, setCreateExternal] = useState(false);

  const applyAddressSelection = (addr: AddressRec | undefined, sid: string, networkId: string) => {
    let v = setByPath(value, `${pathPrefix}.subnet_id`, sid);
    if (addr) {
      const ip = addr.internal_ipv4_address?.address;
      // Если IP ещё не аллоцирован (редкий случай) — оставляем пусто, backend
      // выделит из CIDR подсети при создании NIC.
      v = setByPath(v, `${pathPrefix}.primary_v4_address_spec.address`, ip ?? "");
      v = setByPath(v, `${pathPrefix}._addr_cascader`, [networkId, sid, addr.id]);
    } else {
      v = setByPath(v, `${pathPrefix}.primary_v4_address_spec.address`, "");
      v = setByPath(v, `${pathPrefix}._addr_cascader`, [networkId, sid, `${CASCADER_NOADDR_PREFIX}${sid}`]);
    }
    onChange(v);
  };

  const onCascaderChange = (vals: unknown, _opts: unknown) => {
    void _opts;
    const arr = (vals as string[] | undefined) ?? [];
    if (arr.length < 3) {
      // частичный/сброшенный выбор — чистим
      del("_addr_cascader");
      del("subnet_id");
      del("primary_v4_address_spec");
      return;
    }
    const networkId = String(arr[0]);
    const subnetLevel = String(arr[1]);
    const leaf = String(arr[2]);
    if (leaf.startsWith(CASCADER_CREATE_PREFIX)) {
      setCreateInternalSubnet(subnetLevel);
      return; // не коммитим sentinel
    }
    if (leaf.startsWith(CASCADER_NOADDR_PREFIX)) {
      applyAddressSelection(undefined, subnetLevel, networkId);
      return;
    }
    const addr = internalAddrs.find((a) => a.id === leaf);
    applyAddressSelection(addr, subnetLevel, networkId);
  };

  const networkIdOfSubnet = (sid: string): string | undefined =>
    subnets.find((s) => s.id === sid)?.network_id as string | undefined;

  // ----- external IP -----
  const onExtModeChange = (m: string) => {
    let v = setByPath(value, `${pathPrefix}._ext_mode`, m);
    if (m !== "list") {
      v = deleteByPath(v, `${pathPrefix}._ext_addr_id`);
      v = deleteByPath(v, `${pathPrefix}._ext_addr_value`);
    }
    onChange(v);
  };
  const onExtAddrSelect = (id: string) => {
    const a = externalAddrs.find((x) => x.id === id);
    // Строка, унесённая сужением из ответа края, не имеет права ОБНУЛИТЬ уже
    // записанное: сохранённый выбор показан в списке отдельным вариантом, и
    // повторный выбор того же адреса стёр бы IP, который уезжает в тело
    // запроса (`one_to_one_nat_spec.address`). Для ДРУГОГО адреса пустое
    // значение остаётся законным — его IP просто ещё не выделен.
    const kept = id === extAddrId ? ((get("_ext_addr_value") as string | undefined) ?? "") : "";
    let v = setByPath(value, `${pathPrefix}._ext_addr_id`, id);
    v = setByPath(v, `${pathPrefix}._ext_addr_value`, a?.external_ipv4_address?.address ?? kept);
    onChange(v);
  };

  // ----- existing NIC toggle -----
  const onUseExistingToggle = (checked: boolean) => {
    let v = setByPath(value, `${pathPrefix}._use_existing_nic`, checked);
    if (!checked) v = deleteByPath(v, `${pathPrefix}.nic_id`);
    onChange(v);
  };

  return (
    <div className="space-y-4">
      {/* --- existing NIC (advanced) --- */}
      <div className="space-y-1.5">
        <div className="flex items-center gap-2">
          <Switch size="small" checked={useExisting} onChange={onUseExistingToggle} />
          <span className="text-sm">Использовать существующий сетевой интерфейс</span>
        </div>
        {useExisting && (
          <RefSelect
            refResource="network-interfaces"
            refProjectScoped
            value={nicId}
            onChange={(uid) => set("nic_id", uid || undefined)}
            placeholder="— выбрать NetworkInterface —"
            createResource="network-interfaces"
            createTitle="Создать сетевой интерфейс"
            createPresetFields={() => ({ project_id: project?.id ?? "" })}
            formValue={value}
          />
        )}
      </div>

      {!useExisting && (
        <>
          {/* --- network → subnet → address cascader --- */}
          <div className="space-y-1.5">
            <Label
              required
              description="Уровень 1 — сеть (с поиском); уровень 2 — подсеть; уровень 3 — внутренний IPv4-адрес из подсети, либо «(без адреса)», либо «+ Создать адрес…»."
            >
              Сеть и адрес
            </Label>
            <Cascader
              style={{ width: "100%" }}
              options={cascaderOptions}
              value={cascaderPath}
              onChange={onCascaderChange}
              showSearch={{
                filter: (input, path) =>
                  path.some((o) =>
                    String(o.label ?? "")
                      .toLowerCase()
                      .includes(input.toLowerCase()),
                  ),
              }}
              placeholder={enabled ? "Выберите сеть → подсеть → адрес" : "Выберите проект в шапке"}
              // Область поиска названа вслух: сужается ЗАГРУЖЕННОЕ дерево, и
              // пустой ответ говорит именно это, а не «такого адреса нет».
              // Пока списки едут, не говорится ничего: «нет среди загруженных»
              // на недогруженном дереве — утверждение о том, что ещё не
              // прочитано.
              title={TREE_NOTICE}
              notFoundContent={
                networksQ.isLoading || subnetsQ.isLoading || addressesQ.isLoading ? undefined : TREE_SCOPE.emptyText
              }
              disabled={!enabled}
              changeOnSelect={false}
              expandTrigger="hover"
            />
            {subnetId && (
              <div className="text-xs text-muted-foreground space-y-0.5">
                <div>
                  Подсеть: <CopyableId id={subnetId} />
                </div>
                {primaryAddr ? (
                  <div className="font-mono">Внутренний IPv4: {primaryAddr}</div>
                ) : (
                  <div className="italic">без внутреннего адреса</div>
                )}
              </div>
            )}
            {!enabled && <p className="text-xs text-amber-600">Выберите проект в шапке для загрузки сетей.</p>}
            {(networksQ.isLoading || subnetsQ.isLoading || addressesQ.isLoading) && (
              <p className="text-xs text-muted-foreground">Загрузка сетей/подсетей/адресов…</p>
            )}
            {networksQ.error && <ErrorResult error={networksQ.error} />}
            {subnetsQ.error && <ErrorResult error={subnetsQ.error} />}
            {addressesQ.error && <ErrorResult error={addressesQ.error} />}
          </div>

          {/* --- external IP --- */}
          <div className="space-y-1.5">
            <Label description="«Автоматически» — backend выделит и привяжет внешний адрес. «Список» — выбрать существующий публичный Address. «Без адреса» — без публичного IP.">
              Публичный IP (NAT один-к-одному)
            </Label>
            <Segmented
              value={extMode}
              onChange={(v) => onExtModeChange(String(v))}
              options={[
                { value: "none", label: "Без адреса" },
                { value: "auto", label: "Автоматически" },
                { value: "list", label: "Список" },
              ]}
            />
            {extMode === "list" && (
              <div className="space-y-1">
                <Select
                  style={{ width: "100%" }}
                  value={extAddrId}
                  placeholder={enabled ? "Выберите публичный адрес" : "Выберите проект в шапке"}
                  disabled={!enabled}
                  showSearch
                  onSearch={setExtTerm}
                  title={extScope.notice}
                  // Сузил сервер — клиент НЕ пересеивает: метка здесь «имя — IP»,
                  // а сервер искал по имени, и повторное сужение по метке вычло
                  // бы из ответа края строки, которые он прислал по этому вводу.
                  {...(extScope.asksServer ? { filterOption: false as const } : { optionFilterProp: "label" as const })}
                  // Пустой ответ обязан называть свою ОБЛАСТЬ, а не утверждать
                  // отсутствие адреса, которого поле не спрашивало.
                  notFoundContent={extAddressesQ.isLoading ? undefined : extScope.emptyText}
                  onSelect={(v: string) => {
                    if (v === "__create__") {
                      setCreateExternal(true);
                      return;
                    }
                    onExtAddrSelect(v);
                  }}
                  options={[
                    // Выбранный адрес обязан пережить сужение: сервер отвечает
                    // по ВВОДУ, и уже сделанный выбор в ответ попадать не
                    // обязан. Метку берём из формы (`_ext_addr_value` — тот
                    // самый IP, который туда записал выбор), поэтому поле
                    // показывает адрес, а не сырой `adr-…`.
                    ...(extAddrId && !externalAddrs.some((a) => a.id === extAddrId)
                      ? [{ value: extAddrId, label: (get("_ext_addr_value") as string) || extAddrId }]
                      : []),
                    ...externalAddrs.map((a) => ({
                      value: a.id,
                      label: `${a.name ? a.name + " — " : ""}${a.external_ipv4_address?.address ?? a.id}`,
                    })),
                    { value: "__create__", label: "+ Создать адрес…" },
                  ]}
                />
                {extAddrId &&
                  (() => {
                    const a = externalAddrs.find((x) => x.id === extAddrId);
                    // Подпись под селектором тоже не имеет права деградировать
                    // до идентификатора, когда сужение унесло выбранный адрес
                    // из ответа: сам IP уже лежит в объекте формы — его туда
                    // записал выбор.
                    const ip = a?.external_ipv4_address?.address ?? ((get("_ext_addr_value") as string) || "");
                    return (
                      <div className="text-xs text-muted-foreground">
                        {ip ? <span className="font-mono">{ip}</span> : null} <CopyableId id={extAddrId} />
                      </div>
                    );
                  })()}
              </div>
            )}
          </div>
        </>
      )}

      {/* --- inline create: internal address for subnet --- */}
      {createInternalSubnet && addressesSpec && (
        <Modal
          open
          footer={null}
          onCancel={() => setCreateInternalSubnet(null)}
          width={640}
          destroyOnClose
          title="Выделить IPv4-адрес из подсети"
        >
          <InlineResourceCreateForm
            spec={addressesSpec}
            ctx={{ projectId: project?.id, accountId: project?.accountId }}
            presetFields={{
              _address_kind: "internal",
              "internal_ipv4_address_spec.subnet_id": createInternalSubnet,
            }}
            projectId={project?.id ?? null}
            onCancel={() => setCreateInternalSubnet(null)}
            onSuccess={() => {
              const sid = createInternalSubnet;
              const before = new Set(internalAddrs.map((a) => a.id));
              void addressesQ.refetch().then((r) => {
                const after = (r.data?.addresses ?? []).filter((a) => !!a.internal_ipv4_address);
                const fresh = after.find((a) => !before.has(a.id) && a.internal_ipv4_address?.subnet_id === sid);
                const nid = networkIdOfSubnet(sid);
                if (fresh && nid) applyAddressSelection(fresh, sid, nid);
                else if (nid) applyAddressSelection(undefined, sid, nid);
              });
              setCreateInternalSubnet(null);
            }}
          />
        </Modal>
      )}

      {/* --- inline create: external address --- */}
      {createExternal && addressesSpec && (
        <Modal
          open
          footer={null}
          onCancel={() => setCreateExternal(false)}
          width={640}
          destroyOnClose
          title="Создать публичный IP-адрес"
        >
          <InlineResourceCreateForm
            spec={addressesSpec}
            ctx={{ projectId: project?.id, accountId: project?.accountId }}
            presetFields={{ _address_kind: "external" }}
            projectId={project?.id ?? null}
            onCancel={() => setCreateExternal(false)}
            onSuccess={() => {
              // Перечитывается ТОТ список, который кормит это поле: у публичных
              // адресов он теперь свой (сужается вводом), и обновление общего
              // не добавило бы созданный адрес в выбор.
              const before = new Set(externalAddrs.map((a) => a.id));
              void extAddressesQ.refetch().then((r) => {
                const after = (r.data?.addresses ?? []).filter((a) => !!a.external_ipv4_address);
                const fresh = after.find((a) => !before.has(a.id));
                if (fresh) onExtAddrSelect(fresh.id);
              });
              setCreateExternal(false);
            }}
          />
        </Modal>
      )}

      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        Подсеть должна быть в той же зоне, что и ВМ.
      </Typography.Text>
    </div>
  );
}
