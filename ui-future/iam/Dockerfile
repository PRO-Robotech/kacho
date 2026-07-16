FROM node:22-alpine AS build

WORKDIR /app

ARG KACHO_PUBLIC_BASE=/iam-remote/
ENV KACHO_PUBLIC_BASE=$KACHO_PUBLIC_BASE

# Workspace-aware install: the iam remote shares source with the vpc remote via
# the @kacho-ui-future/shared workspace package, so the build needs the root
# manifest + lockfile and every declared workspace manifest before npm ci.
COPY package.json package-lock.json ./
COPY shared/package.json shared/
COPY vpc/package.json vpc/
COPY iam/package.json iam/
RUN npm ci

# Sources needed to build the iam remote (its own tree + the shared package).
COPY shared/ shared/
COPY iam/ iam/
RUN npm run build --workspace kacho-ui-future-iam

FROM nginx:1.27-alpine

COPY iam/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/iam/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
