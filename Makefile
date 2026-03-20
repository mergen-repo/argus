# Argus — APN & Subscriber Intelligence Platform

-include .env
export

.PHONY: help up down restart status logs build build-fresh deploy-dev deploy-prod \
        infra-up infra-down db-migrate db-migrate-down db-seed db-backup db-restore db-console db-reset \
        test test-watch test-coverage lint lint-fix \
        clean docker-clean dev start stop backup web-dev web-build

help:
	@echo ""
	@echo "  Argus — APN & Subscriber Intelligence Platform"
	@echo "  ==============================================="
	@echo ""
	@echo "  Servisler:"
	@echo "    make up              Tum servisleri baslat"
	@echo "    make down            Tum servisleri durdur"
	@echo "    make restart         Servisleri yeniden baslat"
	@echo "    make status          Servis durumlarini goster"
	@echo "    make logs            Loglari goster (SERVICE=argus)"
	@echo ""
	@echo "  Infra:"
	@echo "    make infra-up        Sadece altyapi (postgres, redis, nats)"
	@echo "    make infra-down      Sadece altyapiyi durdur"
	@echo ""
	@echo "  Build & Deploy:"
	@echo "    make build           Docker image build"
	@echo "    make build-fresh     Build (cache'siz)"
	@echo "    make deploy-dev      Dev ortami deploy (build + up)"
	@echo "    make deploy-prod     Prod deploy (backup + build + up)"
	@echo ""
	@echo "  Veritabani:"
	@echo "    make db-migrate      Migration'lari uygula"
	@echo "    make db-migrate-down Son migration'i geri al"
	@echo "    make db-seed         Seed verilerini yukle"
	@echo "    make db-backup       Veritabani yedegi al"
	@echo "    make db-restore      Yedekten geri yukle (BACKUP=dosya)"
	@echo "    make db-console      DB konsolu ac"
	@echo "    make db-reset        Veritabanini sifirla (DIKKAT)"
	@echo ""
	@echo "  Frontend:"
	@echo "    make web-dev         React dev server baslat"
	@echo "    make web-build       React production build"
	@echo ""
	@echo "  Kalite:"
	@echo "    make test            Go testlerini calistir"
	@echo "    make test-coverage   Test coverage raporu"
	@echo "    make lint            Go lint kontrolu"
	@echo "    make lint-fix        Go lint otomatik duzeltme"
	@echo ""
	@echo "  Temizlik:"
	@echo "    make clean           Build artifact'larini temizle"
	@echo "    make docker-clean    Docker volume ve image'lari sil"
	@echo ""
	@echo "  Kisayollar:"
	@echo "    make dev = deploy-dev    make start = up"
	@echo "    make stop = down         make backup = db-backup"
	@echo ""

# ── Servis komutlari ──

up:
	@echo "Argus baslatiliyor..."
	@docker compose -f deploy/docker-compose.yml up -d
	@echo "Baslatildi: https://localhost:8084"

down:
	@echo "Argus durduruluyor..."
	@docker compose -f deploy/docker-compose.yml down
	@echo "Durduruldu."

restart:
	@echo "Argus yeniden baslatiliyor..."
	@docker compose -f deploy/docker-compose.yml down
	@docker compose -f deploy/docker-compose.yml up -d
	@echo "Yeniden baslatildi."

status:
	@docker compose -f deploy/docker-compose.yml ps

logs:
	@docker compose -f deploy/docker-compose.yml logs -f $(SERVICE)

# ── Altyapi ──

infra-up:
	@echo "Altyapi baslatiliyor..."
	@docker compose -f deploy/docker-compose.yml up -d postgres redis nats
	@echo "Altyapi hazir."

infra-down:
	@echo "Altyapi durduruluyor..."
	@docker compose -f deploy/docker-compose.yml stop postgres redis nats
	@echo "Altyapi durduruldu."

# ── Build & Deploy ──

build:
	@echo "Docker image'lar build ediliyor..."
	@docker compose -f deploy/docker-compose.yml build
	@echo "Build tamamlandi."

build-fresh:
	@echo "Docker image'lar sifirdan build ediliyor..."
	@docker compose -f deploy/docker-compose.yml build --no-cache
	@echo "Fresh build tamamlandi."

deploy-dev: build up
	@echo "Dev ortami deploy edildi."

deploy-prod:
	@read -p "PROD deploy yapilacak. Emin misiniz? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	@echo "Prod deploy baslatiliyor..."
	@$(MAKE) db-backup
	@$(MAKE) build
	@$(MAKE) up
	@echo "Prod deploy tamamlandi."

# ── Veritabani ──

db-migrate:
	@echo "Migration'lar uygulanıyor..."
	@docker compose -f deploy/docker-compose.yml exec argus /app/argus migrate up
	@echo "Migration'lar tamamlandi."

db-migrate-down:
	@echo "Son migration geri alinıyor..."
	@docker compose -f deploy/docker-compose.yml exec argus /app/argus migrate down 1
	@echo "Migration geri alindi."

db-seed:
	@echo "Seed verileri yukleniyor..."
	@docker compose -f deploy/docker-compose.yml exec argus /app/argus seed
	@echo "Seed tamamlandi."

db-backup:
	@echo "Veritabani yedekleniyor..."
	@mkdir -p backups
	@docker compose -f deploy/docker-compose.yml exec -T postgres pg_dump -U $${POSTGRES_USER:-argus} $${POSTGRES_DB:-argus} > backups/backup-$$(date +%Y%m%d-%H%M%S).sql
	@echo "Yedekleme tamamlandi."

db-restore:
	@test -n "$(BACKUP)" || (echo "Kullanim: make db-restore BACKUP=backups/dosya.sql" && exit 1)
	@read -p "Veritabani $(BACKUP) dosyasindan geri yuklenecek. Emin misiniz? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	@echo "Veritabani geri yukleniyor..."
	@docker compose -f deploy/docker-compose.yml exec -T postgres psql -U $${POSTGRES_USER:-argus} $${POSTGRES_DB:-argus} < $(BACKUP)
	@echo "Geri yukleme tamamlandi."

db-console:
	@docker compose -f deploy/docker-compose.yml exec postgres psql -U $${POSTGRES_USER:-argus} $${POSTGRES_DB:-argus}

db-reset:
	@read -p "DIKKAT: Veritabani tamamen sifirlanacak. Emin misiniz? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	@echo "Veritabani sifirlaniyor..."
	@docker compose -f deploy/docker-compose.yml exec argus /app/argus migrate down -all
	@docker compose -f deploy/docker-compose.yml exec argus /app/argus migrate up
	@docker compose -f deploy/docker-compose.yml exec argus /app/argus seed
	@echo "Veritabani sifirlandi."

# ── Frontend ──

web-dev:
	@echo "React dev server baslatiliyor..."
	@cd web && npm run dev

web-build:
	@echo "React production build..."
	@cd web && npm run build
	@echo "Build tamamlandi: web/dist/"

# ── Kalite ──

test:
	@echo "Testler calistiriliyor..."
	@go test ./... -v -race
	@echo "Testler tamamlandi."

test-coverage:
	@echo "Coverage raporu olusturuluyor..."
	@go test ./... -coverprofile=coverage.out -race
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Rapor: coverage.html"

lint:
	@golangci-lint run ./...

lint-fix:
	@golangci-lint run --fix ./...

# ── Temizlik ──

clean:
	@echo "Build artifact'lari temizleniyor..."
	@rm -rf bin/ web/dist/ coverage.out coverage.html
	@echo "Temizlendi."

docker-clean:
	@read -p "Docker volume ve image'lar silinecek. Emin misiniz? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	@echo "Docker temizleniyor..."
	@docker compose -f deploy/docker-compose.yml down -v --rmi local
	@echo "Docker temizlendi."

# ── Kisayollar ──

dev: deploy-dev
start: up
stop: down
backup: db-backup
