FROM node:26-alpine AS build

WORKDIR /app

ARG KACHO_DASHBOARD_REMOTE=/dashboard-remote/assets/remoteEntry.js
ARG KACHO_VPC_REMOTE=/vpc-remote/assets/remoteEntry.js
ARG KACHO_IAM_REMOTE=/iam-remote/assets/remoteEntry.js
ARG KACHO_NLB_REMOTE=/nlb-remote/assets/remoteEntry.js
ARG KACHO_REGISTRY_REMOTE=/registry-remote/assets/remoteEntry.js
ARG KACHO_SYSTEM_REMOTE=/system-remote/assets/remoteEntry.js
ARG KACHO_COMPUTE_REMOTE=/compute-remote/assets/remoteEntry.js
ARG KACHO_STORAGE_REMOTE=/storage-remote/assets/remoteEntry.js
ENV KACHO_DASHBOARD_REMOTE=$KACHO_DASHBOARD_REMOTE
ENV KACHO_VPC_REMOTE=$KACHO_VPC_REMOTE
ENV KACHO_IAM_REMOTE=$KACHO_IAM_REMOTE
ENV KACHO_NLB_REMOTE=$KACHO_NLB_REMOTE
ENV KACHO_REGISTRY_REMOTE=$KACHO_REGISTRY_REMOTE
ENV KACHO_SYSTEM_REMOTE=$KACHO_SYSTEM_REMOTE
ENV KACHO_COMPUTE_REMOTE=$KACHO_COMPUTE_REMOTE
ENV KACHO_STORAGE_REMOTE=$KACHO_STORAGE_REMOTE

# Этот remote ставит зависимости сам (свой package-lock), но собирает и исходники
# shared/ — они лежат рядом, а не внутри его дерева.
#
# ПОЧЕМУ ЭТО ЗДЕСЬ. Модуль начал импортировать из `@shared` (граница отказа
# раздела, канон имён), и без этой строки образ собирается на дереве, где
# `shared/` нет вовсе: `tsc` не находит модуль, сборка падает. Локальная сборка
# при этом зелёная — каталог рядом, — и расхождение видно ТОЛЬКО в образе.
#
# Установка в КОРЕНЬ стадии даёт обоим деревьям один node_modules: иначе импорты
# shared/src резолвились бы вверх от /app/shared и не находили ничего, а react
# нашёлся бы дважды. Так же устроены образы compute, storage, nlb и registry.
COPY host/package*.json ./
RUN npm ci

# Скрипт сборки: кладёт редактор JSON в public модуля. Без него `prebuild`
# останавливает сборку — вкладка JSON иначе собралась бы «успешно» и не работала
# бы в рантайме (CSP запрещает загрузку редактора со стороннего CDN).
#
# Каталог кладётся в КОРЕНЬ стадии: здесь сборка идёт из `/app/host`, а шаг зовёт
# скрипт как `../scripts/…`. Семь остальных образов консоли копируют его так же —
# и только этот не копировал, отчего его сборка падала на `MODULE_NOT_FOUND`,
# тогда как у прочих проходила.
COPY scripts/ scripts/

COPY shared/ shared/
COPY host/ host/
WORKDIR /app/host
RUN npm run build

# nginx-unprivileged: работает под non-root uid 101, слушает 8080, pid и временные файлы
# уводит в /tmp — поэтому уживается с runAsNonRoot + readOnlyRootFilesystem
# (см. deploy/templates/deployment-*.yaml). Конфиг в кластере приезжает шаблоном из
# ConfigMap и рендерится в /etc/nginx/conf.d, куда деплоймент подкладывает emptyDir;
# конфиг ниже — запечённый, для одиночного `docker run`.
FROM nginxinc/nginx-unprivileged:1.31-alpine

COPY host/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/host/dist /usr/share/nginx/html

# Явный non-root USER (trivy DS-0002: контейнер не должен работать под root).
USER 101

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
