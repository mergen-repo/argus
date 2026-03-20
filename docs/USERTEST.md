# Argus - Manuel Test Senaryolari

Bu dosya her story tamamlandiktan sonra guncellenir. Docker ortaminda calistirma gerektiren test adimlari icin `make up` komutu ile ortami baslatmaniz gerekmektedir.

---

## STORY-001: Project Scaffold & Docker Infrastructure

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make up` -- 5 container baslamali (nginx, argus, postgres, redis, nats)
2. `make status` -- Tum containerlar "running" ve "healthy" olmali
3. `curl -k https://localhost/api/health` -- `{"status":"success","data":{"db":"ok","redis":"ok","nats":"ok","uptime":"..."}}` donmeli
4. `make down` -- Tum containerlar durduruluyor olmali
5. `make infra-up` -- Sadece postgres, redis, nats baslamali
6. `make infra-down` -- Altyapi containerlar durmali

---

## STORY-002: Core Database Schema & Migrations

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make infra-up` -- PG, Redis, NATS baslamali
2. `migrate -path migrations -database "postgres://argus:argus_secret@localhost:5450/argus?sslmode=disable" up` -- 4 migration uygulanmali (extensions, core_schema, hypertables, continuous_aggregates)
3. `psql -h localhost -p 5450 -U argus -d argus -c "\dt"` -- 25+ tablo gorunmeli
4. `psql` ile `SELECT hypertable_name FROM timescaledb_information.hypertables;` -- sessions, cdrs, operator_health_logs donmeli
5. `psql` ile `SELECT view_name FROM timescaledb_information.continuous_aggregates;` -- cdrs_hourly, cdrs_daily donmeli
6. `psql -f migrations/seed/001_admin_user.sql` -- Demo tenant + admin user olusturulmali
7. `psql -f migrations/seed/002_system_data.sql` -- Mock operator + grant olusturulmali
8. Seed'leri tekrar calistirmak hata vermemeli (idempotent)
9. `migrate ... down 3` -- 3 app migration geri alinabilmeli
10. `migrate ... up` -- Tekrar uygulanabilmeli

---

## STORY-003: Authentication — JWT + Refresh Token + 2FA

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make up` -- Tum servisleri baslat
2. Login testi:
   ```bash
   curl -sk -X POST https://localhost/api/v1/auth/login \
     -H 'Content-Type: application/json' \
     -d '{"email":"admin@argus.io","password":"admin"}' -c cookies.txt
   ```
   JWT token + refresh cookie donmeli
3. Yanlis sifre ile login -- 401 INVALID_CREDENTIALS donmeli
4. 5 kez yanlis sifre -- 403 ACCOUNT_LOCKED donmeli (15 dk)
5. Refresh testi:
   ```bash
   curl -sk -X POST https://localhost/api/v1/auth/refresh -b cookies.txt
   ```
   Yeni JWT donmeli
6. Logout testi:
   ```bash
   curl -sk -X POST https://localhost/api/v1/auth/logout \
     -H 'Authorization: Bearer <token>' -b cookies.txt
   ```
   204 donmeli, sonraki refresh basarisiz olmali
7. 2FA setup (JWT ile):
   ```bash
   curl -sk -X POST https://localhost/api/v1/auth/2fa/setup \
     -H 'Authorization: Bearer <token>'
   ```
   TOTP secret + QR URI donmeli
8. 2FA verify -- Yanlis kod ile 401, dogru kod ile tam JWT donmeli
9. Expired JWT ile protected route -- 401 donmeli

---

## STORY-004: RBAC Middleware & Permission Enforcement

Bu story icin manuel test senaryosu yok (backend/altyapi). RBAC middleware unit testleri ile dogrulanmistir:

1. `go test ./internal/gateway/... -v` -- 12 test fonksiyonu gecmeli
2. super_admin rollu JWT ile tum endpointlere erisim saglanmali
3. api_user rollu JWT ile sinirli erisim olmali (role yetersizse 403 donmeli)
4. JWT olmadan protected route -- 401 donmeli (403 degil)
5. Yanlis/eksik role ile istek -- 403 INSUFFICIENT_ROLE donmeli

---

## STORY-005: Tenant Management & User CRUD

1. `make up` -- Tum servisleri baslat
2. super_admin ile login yap (admin@argus.io)
3. Tenant listele:
   ```bash
   curl -sk https://localhost/api/v1/tenants -H 'Authorization: Bearer <token>'
   ```
   200 + tenant listesi donmeli
4. Yeni tenant olustur:
   ```bash
   curl -sk -X POST https://localhost/api/v1/tenants \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Test Corp","domain":"testcorp.com","contact_email":"admin@testcorp.com","max_sims":1000,"max_apns":10,"max_users":5}'
   ```
   201 donmeli
5. Tenant detay: GET /api/v1/tenants/:id -- 200 + stats donmeli
6. Kullanici olustur (tenant_admin olarak):
   ```bash
   curl -sk -X POST https://localhost/api/v1/users \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"email":"user@testcorp.com","name":"Test User","role":"analyst"}'
   ```
   201 + state="invited" donmeli
7. max_users limitine ulasildiginda user olusturma -- 422 RESOURCE_LIMIT_EXCEEDED
8. Kullanici guncelle (role degistir) -- 200
9. Tenant state degistir (active → suspended) -- 200
10. Farkli tenant'in verisine erisim denemesi -- 403 veya bos sonuc

---

## STORY-006: Structured Logging, Config & NATS Event Bus

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make up` -- Tum servisleri baslat
2. Log formati kontrolu -- Argus container loglarinda JSON formatli cikti olmali:
   ```bash
   docker logs argus 2>&1 | head -5
   ```
   Her satirda `timestamp`, `level`, `service` alanlari bulunmali
3. Correlation ID kontrolu:
   ```bash
   curl -sk https://localhost/api/health -v 2>&1 | grep X-Correlation-ID
   ```
   Response header'da X-Correlation-ID donmeli
4. NATS stream kontrolu:
   ```bash
   docker exec nats nats stream ls 2>/dev/null
   ```
   EVENTS ve JOBS stream'leri gorunmeli
5. Config validation -- JWT_SECRET bos birakilirsa container baslatilamamali
6. Graceful shutdown -- `docker stop argus` 5 saniye icinde temiz kapanmali

---

## STORY-007: Audit Log Service — Tamper-Proof Hash Chain

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make up` -- Tum servisleri baslat
2. Login yap ve JWT al (admin@argus.io)
3. State-changing islem yap (user olustur veya guncelle) -- Audit entry NATS uzerinden olusturulur
4. Audit log listele:
   ```bash
   curl -sk https://localhost/api/v1/audit-logs \
     -H 'Authorization: Bearer <token>'
   ```
   200 + audit log listesi donmeli (action, entity_type, entity_id, diff)
5. Filtreleme testi:
   ```bash
   curl -sk 'https://localhost/api/v1/audit-logs?action=create&entity_type=user' \
     -H 'Authorization: Bearer <token>'
   ```
   Sadece user create kayitlari donmeli
6. Hash chain dogrulama:
   ```bash
   curl -sk 'https://localhost/api/v1/audit-logs/verify?count=100' \
     -H 'Authorization: Bearer <token>'
   ```
   `{"verified": true, "entries_checked": N}` donmeli
7. CSV export:
   ```bash
   curl -sk -X POST https://localhost/api/v1/audit-logs/export \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"from":"2026-03-01","to":"2026-03-31"}'
   ```
   CSV dosyasi donmeli (Content-Type: text/csv)
8. Yetkisiz erisim (JWT olmadan veya analyst rolu ile) -- 401/403 donmeli
9. Unit testler: `go test ./internal/audit/... ./internal/store/... ./internal/api/audit/... -v` -- 30 test gecmeli

---

## STORY-008: API Key Management & Rate Limiting

1. `make up` -- Tum servisleri baslat
2. Login yap (admin@argus.io) ve JWT al
3. API key olustur:
   ```bash
   curl -sk -X POST https://localhost/api/v1/api-keys \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Test Key","scopes":["sims:read","apns:read"]}'
   ```
   201 + `argus_{prefix}_{secret}` formatinda key donmeli (tek seferlik gosterilir)
4. API key listele:
   ```bash
   curl -sk https://localhost/api/v1/api-keys -H 'Authorization: Bearer <token>'
   ```
   200 + key listesi (sadece prefix gorunur, secret gizli)
5. API key ile istek yap:
   ```bash
   curl -sk https://localhost/api/v1/audit-logs \
     -H 'X-API-Key: argus_{prefix}_{secret}'
   ```
   Scope izni varsa 200, yoksa 403 donmeli
6. API key rotate:
   ```bash
   curl -sk -X POST https://localhost/api/v1/api-keys/{id}/rotate \
     -H 'Authorization: Bearer <token>'
   ```
   200 + yeni key donmeli, eski key 24 saat daha gecerli
7. Rate limiting testi -- Cok sayida istek gonderildiginde 429 + Retry-After header donmeli
8. API key sil (revoke):
   ```bash
   curl -sk -X DELETE https://localhost/api/v1/api-keys/{id} \
     -H 'Authorization: Bearer <token>'
   ```
   204 donmeli, silinen key ile istek 401 donmeli
9. Unit testler: `go test ./internal/store/... ./internal/api/apikey/... ./internal/gateway/... -v`

---

## STORY-009: Operator CRUD & Health Check

1. `make up` -- Tum servisleri baslat
2. Login yap (admin@argus.io) ve JWT al
3. Operator olustur (super_admin):
   ```bash
   curl -sk -X POST https://localhost:8084/api/v1/operators \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Turkcell","code":"turkcell","type":"mobile","country":"TR","adapter_type":"mock","adapter_config":{"endpoint":"https://api.turkcell.com.tr"}}'
   ```
   201 donmeli
4. Operator listele:
   ```bash
   curl -sk https://localhost:8084/api/v1/operators -H 'Authorization: Bearer <token>'
   ```
   200 + operator listesi donmeli
5. Operator guncelle (state degistir):
   ```bash
   curl -sk -X PATCH https://localhost:8084/api/v1/operators/{id} \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"state":"suspended"}'
   ```
   200 donmeli
6. Health check:
   ```bash
   curl -sk https://localhost:8084/api/v1/operators/{id}/health \
     -H 'Authorization: Bearer <token>'
   ```
   200 + health status donmeli
7. Grant olustur (tenant'a operator erisimi ver):
   ```bash
   curl -sk -X POST https://localhost:8084/api/v1/operators/{id}/grants \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"tenant_id":"00000000-0000-0000-0000-000000000001"}'
   ```
   201 donmeli
8. Grant listele + sil: GET/DELETE /api/v1/operators/{id}/grants
9. Unit testler: `go test ./internal/store/... ./internal/crypto/... ./internal/api/operator/... ./internal/operator/... -v`

---

## STORY-010: APN CRUD & IP Pool Management

1. `make up` -- Tum servisleri baslat
2. Login yap (admin@argus.io) ve JWT al
3. APN olustur:
   ```bash
   curl -sk -X POST https://localhost:8084/api/v1/apns \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"iot.fleet","operator_id":"<operator-id>","network_identifier":"iot.fleet.turkcell","ip_version":"ipv4"}'
   ```
   201 donmeli
4. APN listele: GET /api/v1/apns -- 200 + APN listesi
5. APN guncelle: PATCH /api/v1/apns/{id} -- 200
6. IP Pool olustur:
   ```bash
   curl -sk -X POST https://localhost:8084/api/v1/ip-pools \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Fleet Pool","apn_id":"<apn-id>","cidr":"10.100.0.0/24","type":"dynamic"}'
   ```
   201 + IP adresleri otomatik olusturulacak
7. IP Pool listele: GET /api/v1/ip-pools -- 200 + pool listesi
8. IP adresleri listele: GET /api/v1/ip-pools/{id}/addresses -- 200 + IP listesi
9. Statik IP rezervasyon: POST /api/v1/ip-pools/{id}/reserve -- 201 + rezerve IP
10. APN arsivleme: DELETE /api/v1/apns/{id} -- Aktif SIM varsa 422, yoksa 200
11. Unit testler: `go test ./internal/store/... ./internal/api/apn/... ./internal/api/ippool/... -v`
