FROM golang:1.23-alpine AS build

WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/brevyn-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/brevyn-worker ./cmd/worker

FROM node:22-alpine AS admin-web-build

WORKDIR /src/web/admin

COPY web/admin/package.json web/admin/package-lock.json ./
RUN npm ci

COPY web/admin/ ./
RUN npm run build

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /out/brevyn-api /usr/local/bin/brevyn-api
COPY --from=build /out/brevyn-worker /usr/local/bin/brevyn-worker
COPY --from=admin-web-build /src/web/admin/dist /app/web/admin/dist

EXPOSE 4000

CMD ["brevyn-api"]
