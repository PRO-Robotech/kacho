FROM node:22-alpine AS build

WORKDIR /app

ARG KACHO_PUBLIC_BASE=/vpc-remote/
ENV KACHO_PUBLIC_BASE=$KACHO_PUBLIC_BASE

# Workspace-aware install: the vpc remote shares source with the iam remote via
# the @kacho-ui-future/shared workspace package, so the build needs the root
# manifest + lockfile and every declared workspace manifest before npm ci.
COPY package.json package-lock.json ./
COPY shared/package.json shared/
COPY vpc/package.json vpc/
COPY iam/package.json iam/
RUN npm ci

# Sources needed to build the vpc remote (its own tree + the shared package).
COPY shared/ shared/
COPY vpc/ vpc/
RUN npm run build --workspace kacho-ui-future-vpc

# nginx-unprivileged: работает под non-root uid 101, слушает 8080, pid и временные файлы
# уводит в /tmp — поэтому уживается с runAsNonRoot + readOnlyRootFilesystem
# (см. deploy/templates/deployment-*.yaml). Конфиг в кластере приезжает шаблоном из
# ConfigMap и рендерится в /etc/nginx/conf.d, куда деплоймент подкладывает emptyDir;
# конфиг ниже — запечённый, для одиночного `docker run`.
FROM nginxinc/nginx-unprivileged:1.27-alpine

COPY vpc/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/vpc/dist /usr/share/nginx/html

# Явный non-root USER (trivy DS-0002: контейнер не должен работать под root).
USER 101

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
