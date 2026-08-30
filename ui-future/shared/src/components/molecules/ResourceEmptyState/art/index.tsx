import type { FC } from "react";
import { AddressArt } from "./AddressArt";
import { CidrGroupArt } from "./CidrGroupArt";
import { GatewayArt } from "./GatewayArt";
import { ImageArt } from "./ImageArt";
import { InstanceArt } from "./InstanceArt";
import { LoadBalancerArt } from "./LoadBalancerArt";
import { NetworkArt } from "./NetworkArt";
import { NetworkInterfaceArt } from "./NetworkInterfaceArt";
import { RegistryArt } from "./RegistryArt";
import { RouteTableArt } from "./RouteTableArt";
import { SecurityGroupArt } from "./SecurityGroupArt";
import { SubnetArt } from "./SubnetArt";
import { TargetGroupArt } from "./TargetGroupArt";
import { VolumeArt } from "./VolumeArt";

/**
 * Карта «ресурс → рисунок пустого состояния».
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ У КАЖДОГО РЕСУРСА СВОЙ РИСУНОК
 *
 * Общий рисунок на всё говорит одно: «строк нет». Это и так видно — строк нет.
 * Тематический говорит, ЧЕГО именно нет и на что оно похоже: развилка маршрутов
 * у таблицы маршрутов, щит с правилами у группы безопасности, разъём у
 * интерфейса. Экран, на который человек попал впервые, объясняет предмет ещё
 * до того, как он прочтёт текст.
 *
 * КЛЮЧ У ВСЕХ ОДИН и держится им, а не совпадением: холст 208×136, только
 * линии штрихом 1.25, цвета исключительно токенами, опорная линия снизу, ровно
 * одно акцентное пятно — то, чего ещё нет. Отличается СХЕМА, а не оформление;
 * иначе пятнадцать рисунков читались бы как пятнадцать разных продуктов.
 *
 * ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО
 *
 * Ресурса, которому рисунок не назначен, в карте не значится — и он получает
 * общий (`ResourceEmptyArt`). Это не пробел: собственный рисунок оправдан там,
 * где у предмета есть узнаваемая схема. У списка проектов или пользователей её
 * нет, и выдуманная схема сообщала бы неправду.
 *
 * Идентификаторы ключей — ЖИВЫЕ `spec.id` реестра (сверено с ним в момент
 * заведения карты). Ключ, которого в реестре нет, молча не сработает никогда:
 * промах по карте неотличим от «рисунок не назначен».
 */
export const ART_BY_SPEC: Record<string, FC> = {
  // VPC
  networks: NetworkArt,
  subnets: SubnetArt,
  addresses: AddressArt,
  "address-pools": AddressArt,
  "route-tables": RouteTableArt,
  "security-groups": SecurityGroupArt,
  "network-interfaces": NetworkInterfaceArt,
  gateways: GatewayArt,
  "cidr-groups": CidrGroupArt,

  // Compute и Storage
  "compute-instances": InstanceArt,
  volumes: VolumeArt,
  images: ImageArt,

  // NLB
  "load-balancers": LoadBalancerArt,
  "target-groups": TargetGroupArt,
  listeners: LoadBalancerArt,

  // Registry
  repositories: RegistryArt,
  registries: RegistryArt,

  // Операций в карте НЕТ: `operations` — не ресурс реестра (проверено
  // перечислением всех `spec.id` дерева, единственный промах был именно этот), у
  // журнала операций своя страница со своим пустым состоянием. Рисунок
  // `OperationArt` заведён и ждёт своего вызывающего там, а не здесь: ключ,
  // которого в реестре нет, не сработал бы НИКОГДА, и промах по карте
  // неотличим от «рисунок не назначен».
};
