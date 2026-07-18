.PHONY: run build tidy web-dev web-build init-db fix-db-perms

run:
	go run ./cmd/api -config configs/config.yaml

build:
	GOTMPDIR=.tmp go build -o bin/ordercore ./cmd/api

tidy:
	GOTMPDIR=.tmp go mod tidy

web-dev:
	cd web && npm run dev

web-build:
	cd web && npm run build

init-db:
	@test -n "$(APP_PASSWORD)" || (echo "用法: make init-db APP_PASSWORD=你的密码"; exit 1)
	chmod +x deploy/setup_db.sh
	./deploy/setup_db.sh "$(APP_PASSWORD)"

fix-db-perms:
	chmod +x deploy/fix_db_permissions.sh
	./deploy/fix_db_permissions.sh
