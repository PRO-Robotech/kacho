FROM node:22-alpine AS build

WORKDIR /app

ARG KACHO_DASHBOARD_REMOTE=/dashboard/assets/remoteEntry.js
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

COPY host/package*.json ./
RUN npm ci

COPY host/ ./
RUN npm run build

FROM nginx:1.27-alpine

COPY host/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
