.PHONY: tidy run test docker-up docker-up-local docker-down admin-install admin-dev admin-build admin-lint admin-docker app-install app-dev app-build

tidy:
	go mod tidy

run:
	go run ./cmd/api

test:
	go test ./cmd/... ./internal/... ./ent/schema

docker-up:
	docker compose up -d --build

docker-up-local: admin-build app-build
	docker compose -f docker-compose.yml -f docker-compose.local.yml up -d --build --pull never api worker

docker-down:
	docker compose down

admin-install:
	cd web/admin && npm install

admin-dev:
	cd web/admin && npm run dev -- --host 127.0.0.1

admin-build:
	cd web/admin && npm run build

admin-lint:
	cd web/admin && npm run lint

admin-docker:
	docker compose up -d --build api

app-install:
	cd web/app && npm install

app-dev:
	cd web/app && npm run dev -- --host 127.0.0.1

app-build:
	cd web/app && npm run build
