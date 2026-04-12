# Argus - Manuel Test Senaryolari

Bu dosya her story tamamlandiktan sonra guncellenir. Docker ortaminda calistirma gerektiren test adimlari icin `make up` komutu ile ortami baslatmaniz gerekmektedir.

---

## STORY-001: Project Scaffold & Docker Infrastructure

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make up` -- 5 container baslamali (nginx, argus, postgres, redis, nats)
2. `make status` -- Tum containerlar "running" ve "healthy" olmali
3. `curl -k http://localhost:8084/api/health` -- `{"status":"success","data":{"db":"ok","redis":"ok","nats":"ok","uptime":"..."}}` donmeli
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
   curl -sk -X POST http://localhost:8084/api/v1/auth/login \
     -H 'Content-Type: application/json' \
     -d '{"email":"admin@argus.io","password":"admin"}' -c cookies.txt
   ```
   JWT token + refresh cookie donmeli
3. Yanlis sifre ile login -- 401 INVALID_CREDENTIALS donmeli
4. 5 kez yanlis sifre -- 403 ACCOUNT_LOCKED donmeli (15 dk)
5. Refresh testi:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/auth/refresh -b cookies.txt
   ```
   Yeni JWT donmeli
6. Logout testi:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/auth/logout \
     -H 'Authorization: Bearer <token>' -b cookies.txt
   ```
   204 donmeli, sonraki refresh basarisiz olmali
7. 2FA setup (JWT ile):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/auth/2fa/setup \
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
   curl -sk http://localhost:8084/api/v1/tenants -H 'Authorization: Bearer <token>'
   ```
   200 + tenant listesi donmeli
4. Yeni tenant olustur:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/tenants \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Test Corp","domain":"testcorp.com","contact_email":"admin@testcorp.com","max_sims":1000,"max_apns":10,"max_users":5}'
   ```
   201 donmeli
5. Tenant detay: GET /api/v1/tenants/:id -- 200 + stats donmeli
6. Kullanici olustur (tenant_admin olarak):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/users \
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
   curl -sk http://localhost:8084/api/health -v 2>&1 | grep X-Correlation-ID
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
   curl -sk http://localhost:8084/api/v1/audit-logs \
     -H 'Authorization: Bearer <token>'
   ```
   200 + audit log listesi donmeli (action, entity_type, entity_id, diff)
5. Filtreleme testi:
   ```bash
   curl -sk 'http://localhost:8084/api/v1/audit-logs?action=create&entity_type=user' \
     -H 'Authorization: Bearer <token>'
   ```
   Sadece user create kayitlari donmeli
6. Hash chain dogrulama:
   ```bash
   curl -sk 'http://localhost:8084/api/v1/audit-logs/verify?count=100' \
     -H 'Authorization: Bearer <token>'
   ```
   `{"verified": true, "entries_checked": N}` donmeli
7. CSV export:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/audit-logs/export \
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
   curl -sk -X POST http://localhost:8084/api/v1/api-keys \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Test Key","scopes":["sims:read","apns:read"]}'
   ```
   201 + `argus_{prefix}_{secret}` formatinda key donmeli (tek seferlik gosterilir)
4. API key listele:
   ```bash
   curl -sk http://localhost:8084/api/v1/api-keys -H 'Authorization: Bearer <token>'
   ```
   200 + key listesi (sadece prefix gorunur, secret gizli)
5. API key ile istek yap:
   ```bash
   curl -sk http://localhost:8084/api/v1/audit-logs \
     -H 'X-API-Key: argus_{prefix}_{secret}'
   ```
   Scope izni varsa 200, yoksa 403 donmeli
6. API key rotate:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/api-keys/{id}/rotate \
     -H 'Authorization: Bearer <token>'
   ```
   200 + yeni key donmeli, eski key 24 saat daha gecerli
7. Rate limiting testi -- Cok sayida istek gonderildiginde 429 + Retry-After header donmeli
8. API key sil (revoke):
   ```bash
   curl -sk -X DELETE http://localhost:8084/api/v1/api-keys/{id} \
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
   curl -sk -X POST http://localhost:8084/api/v1/operators \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Turkcell","code":"turkcell","type":"mobile","country":"TR","adapter_type":"mock","adapter_config":{"endpoint":"https://api.turkcell.com.tr"}}'
   ```
   201 donmeli
4. Operator listele:
   ```bash
   curl -sk http://localhost:8084/api/v1/operators -H 'Authorization: Bearer <token>'
   ```
   200 + operator listesi donmeli
5. Operator guncelle (state degistir):
   ```bash
   curl -sk -X PATCH http://localhost:8084/api/v1/operators/{id} \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"state":"suspended"}'
   ```
   200 donmeli
6. Health check:
   ```bash
   curl -sk http://localhost:8084/api/v1/operators/{id}/health \
     -H 'Authorization: Bearer <token>'
   ```
   200 + health status donmeli
7. Grant olustur (tenant'a operator erisimi ver):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/operators/{id}/grants \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"tenant_id":"00000000-0000-0000-0000-000000000001"}'
   ```
   201 donmeli
8. Grant listele + sil: GET/DELETE /api/v1/operators/{id}/grants
9. Unit testler: `go test ./internal/store/... ./internal/crypto/... ./internal/api/operator/... ./internal/operator/... -v`

---

## STORY-021: Operator Failover & Circuit Breaker

1. `make up` -- Servisleri baslat
2. Login yap ve JWT al
3. Mock operator olustur (success_rate dusuk, orn: 20):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/operators \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Failing Op","code":"fail-op","type":"mobile","country":"TR","adapter_type":"mock","adapter_config":{"success_rate":20,"latency_ms":100}}'
   ```
4. Health check durumunu izle:
   ```bash
   curl -sk http://localhost:8084/api/v1/operators/{id}/health \
     -H 'Authorization: Bearer <token>'
   ```
   Dusuk success_rate ile circuit breaker acilmali (status: "down")
5. NATS monitoring (localhost:8222) ile operator.health_changed eventlerini izle
6. WebSocket baglantisi ile real-time health degisikliklerini gozlemle
7. Unit testler: `go test ./internal/operator/... ./internal/notification/... ./internal/ws/... -v`

---

## STORY-020: 5G SBA HTTP/2 Proxy (AUSF/UDM)

Bu story icin manuel test senaryosu yok (backend/altyapi — 5G SBA HTTP/2 protokolu). Asagidaki komutlar ile dogrulama:

1. `make up` -- Servisleri baslat (SBA :8443 — SBA_ENABLED=true gerekli)
2. Health check:
   ```bash
   curl -sk http://localhost:8084/api/health
   ```
   `{"aaa":{"sba":"ok",...}}` icermeli
3. AUSF 5G-AKA baslat:
   ```bash
   curl -sk -X POST https://localhost:8443/nausf-auth/v1/ue-authentications \
     -H 'Content-Type: application/json' \
     -d '{"supiOrSuci":"imsi-310260000000001","servingNetworkName":"5G:mnc026.mcc310.3gppnetwork.org"}'
   ```
   201 + auth context + challenge donmeli
4. Unit testler: `go test ./internal/aaa/sba/... -v`

---

## STORY-019: Diameter Protocol Server (Gx/Gy)

Bu story icin manuel test senaryosu yok (backend/altyapi — Diameter TCP protokolu). Asagidaki komutlar ile dogrulama:

1. `make up` -- Servisleri baslat (Diameter :3868 otomatik dinler)
2. Health check ile Diameter durumu:
   ```bash
   curl -sk http://localhost:8084/api/health
   ```
   `{"aaa":{"radius":"ok","diameter":"ok",...}}` icermeli
3. Unit testler: `go test -race ./internal/aaa/diameter/... -v`
4. Diameter client ile CER/CEA handshake testi (harici Diameter test araci gerekli)

---

## STORY-017: Session Management & Force Disconnect

1. `make up` -- Tum servisleri baslat
2. Login yap (admin@argus.io) ve JWT al
3. Aktif session listele:
   ```bash
   curl -sk "http://localhost:8084/api/v1/sessions?limit=10" \
     -H 'Authorization: Bearer <token>'
   ```
   200 + aktif session listesi (cursor pagination)
4. Session istatistikleri:
   ```bash
   curl -sk http://localhost:8084/api/v1/sessions/stats \
     -H 'Authorization: Bearer <token>'
   ```
   200 + total_active, by_operator, by_apn, avg_duration, avg_bytes
5. Force disconnect (aktif session varsa):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/sessions/{id}/disconnect \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"reason":"test disconnect"}'
   ```
   200 + session terminated, audit log olusturulur
6. Bulk disconnect (tenant_admin):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/sessions/bulk/disconnect \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"segment_id":"<segment-id>","reason":"maintenance"}'
   ```
   >100 session icin 202 + job_id, <=100 icin 200 + disconnected_count
7. Unit testler: `go test ./internal/aaa/session/... ./internal/api/session/... ./internal/job/... -v`

---

## STORY-016: EAP-SIM/AKA/AKA' Authentication

Bu story icin manuel test senaryosu yok (backend/altyapi — EAP protokol seviyesi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make up` -- Servisleri baslat
2. EAP akisi RADIUS uzerinden calisir (radclient ile EAP-Message attribute gondermek gerekir)
3. Mock operator'de EAP vector uretimi otomatik (success_rate config)
4. Unit testler: `go test ./internal/aaa/eap/... -v -count=1`
5. Race detection: `go test -race ./internal/aaa/eap/... -v`

---

## STORY-015: RADIUS Authentication & Accounting Server

Bu story icin manuel test senaryosu yok (backend/altyapi — RADIUS UDP protokolu). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make up` -- Tum servisleri baslat
2. RADIUS_SECRET env var set edilmis olmali (Docker Compose'da default var)
3. Health check ile AAA durumu kontrol:
   ```bash
   curl -sk http://localhost:8084/api/health
   ```
   Cevap: `{"aaa":{"radius":"ok","sessions_active":0}}` icermeli
4. RADIUS test (radtest veya radclient gerekli):
   ```bash
   echo "User-Name=310260000000001" | radclient -x localhost:1812 auth testing123
   ```
   Active SIM icin Access-Accept, bilinmeyen IMSI icin Access-Reject donmeli
5. Unit testler: `go test ./internal/aaa/radius/... ./internal/store/... ./internal/aaa/session/... -v`

---

## STORY-018: Pluggable Operator Adapter + Mock Simulator

Bu story icin manuel test senaryosu yok (backend/altyapi). Adapter framework backend-only. Asagidaki komutlar ile dogrulama yapilabilir:

1. `make up` -- Servisleri baslat
2. Login yap ve JWT al
3. Mock operator olustur (adapter_type: "mock"):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/operators \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Mock Operator","code":"mock-op","type":"mobile","country":"TR","adapter_type":"mock","adapter_config":{"success_rate":80,"latency_ms":50}}'
   ```
4. Test connection:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/operators/{id}/test \
     -H 'Authorization: Bearer <token>'
   ```
   200 + health status donmeli
5. Unit testler: `go test -race ./internal/operator/... -v`

---

## STORY-014: MSISDN Number Pool Management

1. `make up` -- Tum servisleri baslat
2. Login yap (admin@argus.io) ve JWT al
3. MSISDN CSV hazirla (msisdn.csv):
   ```
   msisdn,operator_code
   +905551000001,turkcell
   +905551000002,turkcell
   +905551000003,turkcell
   ```
4. MSISDN import:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/msisdn-pool/import \
     -H 'Authorization: Bearer <token>' \
     -F 'file=@msisdn.csv'
   ```
   201 + import sonucu donmeli
5. MSISDN listele:
   ```bash
   curl -sk "http://localhost:8084/api/v1/msisdn-pool?state=available&limit=10" \
     -H 'Authorization: Bearer <token>'
   ```
   200 + MSISDN listesi (state: available)
6. MSISDN ata:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/msisdn-pool/{id}/assign \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"sim_id":"<sim-id>"}'
   ```
   200 + state:"assigned"
7. Duplicate MSISDN import → 409 donmeli (global uniqueness)
8. SIM terminate → MSISDN state:"reserved" + reserved_until (grace period)
9. Unit testler: `go test ./internal/store/... ./internal/api/msisdn/... -v`

---

## STORY-013: Bulk SIM Import (CSV)

1. `make up` -- Tum servisleri baslat
2. Login yap (admin@argus.io) ve JWT al
3. CSV dosyasi hazirla (test.csv):
   ```
   ICCID,IMSI,MSISDN,operator_code,apn_name
   8990100000000000010,310260000000010,+905551234510,turkcell,iot.fleet
   8990100000000000011,310260000000011,+905551234511,turkcell,iot.fleet
   8990100000000000012,310260000000012,,turkcell,iot.fleet
   ```
4. Bulk import:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/sims/bulk/import \
     -H 'Authorization: Bearer <token>' \
     -F 'file=@test.csv'
   ```
   202 + job_id donmeli
5. Job durumu kontrol:
   ```bash
   curl -sk http://localhost:8084/api/v1/jobs/{job_id} \
     -H 'Authorization: Bearer <token>'
   ```
   200 + status (pending/running/completed), progress yuzde, processed/total_rows
6. Job listele: GET /api/v1/jobs -- 200 + job listesi
7. Duplicate ICCID ile CSV yukle → partial success, error_report'ta duplicate satirlar
8. Hata raporu indir:
   ```bash
   curl -sk http://localhost:8084/api/v1/jobs/{job_id}/errors \
     -H 'Authorization: Bearer <token>'
   ```
   200 + CSV formatinda hata raporu (row, iccid, error)
9. Job iptal (uzun sureli import icin):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/jobs/{job_id}/cancel \
     -H 'Authorization: Bearer <token>'
   ```
   200 + status:"cancelled"
10. Unit testler: `go test ./internal/job/... ./internal/api/job/... ./internal/api/sim/... -v`

---

## STORY-012: SIM Segments & Group-First UX

1. `make up` -- Tum servisleri baslat
2. Login yap (admin@argus.io) ve JWT al
3. Segment olustur:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/sim-segments \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Active IoT SIMs","filter_definition":{"state":"active","rat_type":"nb_iot"}}'
   ```
   201 + segment donmeli
4. Segment listele: GET /api/v1/sim-segments -- 200 + segment listesi
5. Segment detay: GET /api/v1/sim-segments/{id} -- 200 + segment detayi
6. Segment count:
   ```bash
   curl -sk http://localhost:8084/api/v1/sim-segments/{id}/count \
     -H 'Authorization: Bearer <token>'
   ```
   200 + `{"count": N}` donmeli
7. State summary:
   ```bash
   curl -sk http://localhost:8084/api/v1/sim-segments/{id}/summary \
     -H 'Authorization: Bearer <token>'
   ```
   200 + state bazinda sayilar donmeli (active, suspended, vb.)
8. Segment sil: DELETE /api/v1/sim-segments/{id} -- 204
9. Unit testler: `go test ./internal/store/... ./internal/api/segment/... -v`

---

## STORY-011: SIM CRUD & State Machine

1. `make up` -- Tum servisleri baslat
2. Login yap (admin@argus.io) ve JWT al
3. SIM olustur:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/sims \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"iccid":"8990100000000000001","imsi":"310260000000001","msisdn":"+905551234567","operator_id":"<operator-id>","apn_id":"<apn-id>","sim_type":"triple_cut"}'
   ```
   201 + state:"ordered" donmeli
4. Duplicate ICCID ile olustur → 409 ICCID_EXISTS donmeli
5. SIM aktive et:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/sims/{id}/activate \
     -H 'Authorization: Bearer <token>'
   ```
   200 + state:"active", ip_address atanmis olmali
6. SIM askiya al:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/sims/{id}/suspend \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"reason":"non-payment"}'
   ```
   200 + state:"suspended", IP korunmus olmali
7. SIM devam ettir:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/sims/{id}/resume \
     -H 'Authorization: Bearer <token>'
   ```
   200 + state:"active"
8. SIM sonlandir (tenant_admin gerekli):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/sims/{id}/terminate \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"reason":"contract-end"}'
   ```
   200 + state:"terminated", purge_at tarih hesaplanmis olmali
9. Gecersiz gecis testi: ORDERED→SUSPENDED → 422 INVALID_STATE_TRANSITION donmeli
10. SIM listele (filtreli):
    ```bash
    curl -sk "http://localhost:8084/api/v1/sims?state=active&limit=10" \
      -H 'Authorization: Bearer <token>'
    ```
    200 + cursor-based pagination (meta.next_cursor) donmeli
11. SIM detay: GET /api/v1/sims/{id} -- 200 + tum bilgiler
12. State gecmisi: GET /api/v1/sims/{id}/history -- 200 + gecis kayitlari
13. Unit testler: `go test ./internal/store/... ./internal/api/sim/... -v`

---

## STORY-010: APN CRUD & IP Pool Management

1. `make up` -- Tum servisleri baslat
2. Login yap (admin@argus.io) ve JWT al
3. APN olustur:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/apns \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"iot.fleet","operator_id":"<operator-id>","network_identifier":"iot.fleet.turkcell","ip_version":"ipv4"}'
   ```
   201 donmeli
4. APN listele: GET /api/v1/apns -- 200 + APN listesi
5. APN guncelle: PATCH /api/v1/apns/{id} -- 200
6. IP Pool olustur:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/ip-pools \
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

---

## STORY-022: Policy DSL Parser & Evaluator

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Unit testler: `go test ./internal/policy/dsl/... -v` -- 47+ test gecmeli
2. Full suite: `go test ./... -count=1` -- Tum testler gecmeli, regresyon yok
3. Build: `go build ./...` -- Hatasiz derlenmeli

---

## STORY-023: Policy CRUD & Versioning

Onkosul: `make up` ile Docker ortami calisir durumda olmali.

1. Policy olustur:
   ```bash
   curl -sk http://localhost:8084/api/v1/policies \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"name":"Test Policy","description":"Test","scope":"global","dsl_source":"POLICY \"test\" { MATCH { apn = \"iot\" } RULES { bandwidth_down = 1mbps } }"}'
   ```
   201 + policy id + v1 draft donmeli
2. Policy listele: GET /api/v1/policies -- 200 + policy listesi
3. Policy detay: GET /api/v1/policies/{id} -- 200 + tum versionlar
4. Yeni versiyon olustur: POST /api/v1/policies/{id}/versions -- 201 + v2 draft
5. Versiyon aktive et: PUT /api/v1/policies/{id}/versions/{vid}/activate -- 200 + state=active
6. Syntax hatali DSL ile aktivasyon: 422 INVALID_DSL donmeli
7. Policy sil (SIM atanmamis): DELETE /api/v1/policies/{id} -- 200
8. Unit testler: `go test ./internal/store/... ./internal/api/policy/... -v`

---

## STORY-024: Policy Dry-Run Simulation

Onkosul: `make up` + en az 1 policy ve birkac SIM olmali.

1. Dry-run calistir (sync):
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/policy-versions/{vid}/dry-run \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{}'
   ```
   200 + total_affected, by_operator, by_apn, by_rat, behavioral_changes, sample_sims donmeli
2. Segment filtresi ile dry-run: `{"segment_id":"<id>"}` -- sadece segment SIM'leri
3. Gecersiz DSL ile dry-run: 422 + derleme hatalari
4. Unit testler: `go test ./internal/policy/dryrun/... ./internal/api/policy/... -v`

---

## STORY-025: Policy Staged Rollout (Canary)

Onkosul: `make up` + en az 1 aktif policy version + birkac SIM olmali.

1. Staged rollout baslat:
   ```bash
   curl -sk -X POST http://localhost:8084/api/v1/policy-versions/{vid}/rollout \
     -H 'Authorization: Bearer <token>' \
     -H 'Content-Type: application/json' \
     -d '{"stages":[1,10,100]}'
   ```
   201 + rollout_id, stages, state="in_progress" donmeli
2. Rollout ilerleme: GET /api/v1/policy-rollouts/{rollout_id} -- 200 + current_stage, migrated_count
3. Advance (sonraki stage): POST /api/v1/policy-rollouts/{rollout_id}/advance -- 200 + next stage
4. Rollback: POST /api/v1/policy-rollouts/{rollout_id}/rollback -- 200 + reverted_count
5. Aktif rollout varken yeni rollout: 422 ROLLOUT_IN_PROGRESS donmeli
6. Unit testler: `go test ./internal/policy/rollout/... ./internal/job/... ./internal/api/policy/... -v`

---

## STORY-026: Steering of Roaming (SoR) Engine

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Unit testler: `go test ./internal/operator/sor/... -v` -- 16 test gecmeli
2. Migrasyon: `make db-migrate` -- sor_fields migrasyonu uygulanmali
3. Full suite: `go test ./... -count=1` -- Tum testler gecmeli, regresyon yok
4. Build: `go build ./...` -- Hatasiz derlenmeli

---

## STORY-027: RAT-Type Awareness (All Layers)

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Unit testler: `go test ./internal/aaa/rattype/... -v` -- 9 test gecmeli
2. Full suite: `go test ./... -count=1` -- 672+ test gecmeli, regresyon yok
3. Build: `go build ./...` -- Hatasiz derlenmeli

---

## STORY-031: Background Job System

Onkosul: `make up` ile Docker ortami calisir durumda olmali.

1. Job listele: GET /api/v1/jobs -- 200 + bos veya mevcut job listesi
2. Job detay: GET /api/v1/jobs/{id} -- 200 + progress, duration, locked_by
3. Job iptal: POST /api/v1/jobs/{id}/cancel -- 200 + state=cancelled (veya 422 zaten tamamlanmis)
4. Job tekrar: POST /api/v1/jobs/{id}/retry -- 201 + new_job_id (veya 422 hala calisiyor)
5. Unit testler: `go test ./internal/job/... ./internal/api/job/... -v` -- 40+ test gecmeli
6. Full suite: `go test ./... -count=1` -- 696+ test gecmeli

---

## STORY-028: eSIM Profile Management

Onkosul: `make up` + en az 1 eSIM tipi SIM olmali.

1. Profil listele: GET /api/v1/esim-profiles?sim_id={id} -- 200 + profil listesi
2. Profil detay: GET /api/v1/esim-profiles/{id} -- 200 + iccid, operator, state
3. Profil etkinlestir: POST /api/v1/esim-profiles/{id}/enable -- 200 + state=enabled
4. Zaten aktif profil varken enable: 422 PROFILE_ALREADY_ENABLED
5. Profil devre disi: POST /api/v1/esim-profiles/{id}/disable -- 200 + state=disabled
6. Profil degistir: POST /api/v1/esim-profiles/{id}/switch -- 200 + yeni operator bilgisi
7. Fiziksel SIM'de enable: 422 NOT_ESIM
8. Unit testler: `go test ./internal/store/... ./internal/api/esim/... -v`

---

## STORY-029: OTA SIM Management via APDU Commands

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. OTA komutu gonder:
   ```bash
   curl -k -X POST http://localhost:8084/api/v1/sims/<SIM_UUID>/ota \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"command_type":"UPDATE_FILE","channel":"sms_pp","security_mode":"none","payload":{"file_id":"6F07","offset":0,"content":"AQID"}}'
   ```
   201 + command_id + status=queued
2. OTA gecmisi listele:
   ```bash
   curl -k http://localhost:8084/api/v1/sims/<SIM_UUID>/ota -H "Authorization: Bearer $TOKEN"
   ```
   200 + paginated list (cursor-based)
3. OTA komut detayi:
   ```bash
   curl -k http://localhost:8084/api/v1/ota-commands/<CMD_UUID> -H "Authorization: Bearer $TOKEN"
   ```
   200 + command details with delivery status timestamps
4. Bulk OTA (tenant_admin rolu gerekli):
   ```bash
   curl -k -X POST http://localhost:8084/api/v1/sims/bulk/ota \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"sim_ids":["<SIM_UUID_1>","<SIM_UUID_2>"],"command_type":"UPDATE_FILE","channel":"sms_pp","security_mode":"none","payload":{"file_id":"6F07","offset":0,"content":"AQID"}}'
   ```
   202 + job_id + state=queued
5. Rate limit testi: Ayni SIM'e arka arkaya 11+ OTA komutu gonder -- 429 OTA_RATE_LIMIT (limit: 10/saat)
6. Unit testler: `go test ./internal/ota/... ./internal/store/ ./internal/job/ ./internal/api/ota/... -v` -- 78+ OTA test gecmeli

---

## STORY-030: Bulk Operations (State Change, Policy Assign, Operator Switch)

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Bulk state change: `curl -k -X POST http://localhost:8084/api/v1/sims/bulk/state-change -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"segment_id":"<SEG_UUID>","target_state":"suspended","reason":"maintenance"}'` -- 202 + job_id + estimated_count
2. Bulk policy assign: `curl -k -X POST http://localhost:8084/api/v1/sims/bulk/policy-assign -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"segment_id":"<SEG_UUID>","policy_version_id":"<VER_UUID>"}'` -- 202 + job_id
3. Bulk operator switch: `curl -k -X POST http://localhost:8084/api/v1/sims/bulk/operator-switch -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"segment_id":"<SEG_UUID>","target_operator_id":"<OP_UUID>","target_apn_id":"<APN_UUID>"}'` -- 202 + job_id
4. Job progress: WebSocket'ten job progress event'leri gelmeli
5. Error report CSV: `curl -k http://localhost:8084/api/v1/jobs/<JOB_UUID>/errors -H "Authorization: Bearer $TOKEN"` -- CSV dosyasi
6. Unit testler: `go test ./internal/job/... ./internal/api/sim/... -v`

---

## STORY-032: CDR Processing & Rating Engine

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. CDR listele: `curl -k http://localhost:8084/api/v1/cdrs?from=2026-03-01T00:00:00Z&to=2026-03-31T23:59:59Z -H "Authorization: Bearer $TOKEN"` -- 200 + paginated CDR list
2. SIM bazli CDR: `curl -k "http://localhost:8084/api/v1/cdrs?sim_id=<SIM_UUID>" -H "Authorization: Bearer $TOKEN"` -- 200 + filtered list
3. CDR export: `curl -k -X POST http://localhost:8084/api/v1/cdrs/export -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"from":"2026-03-01T00:00:00Z","to":"2026-03-31T23:59:59Z","format":"csv"}'` -- 202 + job_id
4. NATS event test: RADIUS accounting event gonderdikten sonra CDR tablosunda yeni kayit olusturulmali
5. Unit testler: `go test ./internal/analytics/cdr/... ./internal/store/ ./internal/api/cdr/... ./internal/job/ -v`

---

## STORY-033: Built-In Observability & Real-Time Metrics

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Sistem metrikleri: `curl -k http://localhost:8084/api/v1/system/metrics -H "Authorization: Bearer $TOKEN"` -- 200 + auth_per_sec, error_rate, latency, active_sessions, by_operator, system_status
2. Prometheus: `curl -k http://localhost:8084/metrics` -- OpenMetrics format text
3. WebSocket: ws://localhost:8081 baglantisi ile metrics.realtime event'leri 1 saniyede bir gelmeli
4. Unit testler: `go test ./internal/analytics/metrics/... ./internal/api/metrics/... -v`

---

## STORY-034: Usage Analytics Dashboards

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Son 24 saat kullanim: `curl -k "http://localhost:8084/api/v1/analytics/usage?period=24h" -H "Authorization: Bearer $TOKEN"` -- 200 + time_series (15min buckets), totals, breakdowns
2. Operator bazli gruplama: `curl -k "http://localhost:8084/api/v1/analytics/usage?period=7d&group_by=operator" -H "Authorization: Bearer $TOKEN"` -- 200 + operator bazli breakdowns
3. RAT tipi gruplama: `curl -k "http://localhost:8084/api/v1/analytics/usage?period=30d&group_by=rat_type" -H "Authorization: Bearer $TOKEN"` -- 200
4. Karsilastirma modu: `curl -k "http://localhost:8084/api/v1/analytics/usage?period=24h&compare=true" -H "Authorization: Bearer $TOKEN"` -- 200 + comparison delta
5. Unit testler: `go test ./internal/store/ ./internal/api/analytics/... -v`

---

## STORY-035: Cost Analytics & Optimization

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Maliyet analizi: `curl -k "http://localhost:8084/api/v1/analytics/cost?period=30d" -H "Authorization: Bearer $TOKEN"` -- 200 + total_cost, by_operator, cost_per_mb, top_expensive_sims, trend, suggestions
2. Karsilastirma: `curl -k "http://localhost:8084/api/v1/analytics/cost?period=30d&compare=true" -H "Authorization: Bearer $TOKEN"` -- 200 + comparison delta
3. Operator filtre: `curl -k "http://localhost:8084/api/v1/analytics/cost?period=30d&operator_id=<OP_UUID>" -H "Authorization: Bearer $TOKEN"` -- 200
4. Unit testler: `go test ./internal/analytics/cost/... ./internal/api/analytics/... -v`

---

## STORY-036: Anomaly Detection Engine

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Anomali listele: `curl -k "http://localhost:8084/api/v1/analytics/anomalies" -H "Authorization: Bearer $TOKEN"` -- 200 + paginated anomaly list
2. Severity filtre: `curl -k "http://localhost:8084/api/v1/analytics/anomalies?severity=critical" -H "Authorization: Bearer $TOKEN"` -- 200 + sadece critical
3. Anomali detayi: `curl -k "http://localhost:8084/api/v1/analytics/anomalies/<ID>" -H "Authorization: Bearer $TOKEN"` -- 200 + details JSONB
4. Durumu guncelle: `curl -k -X PATCH "http://localhost:8084/api/v1/analytics/anomalies/<ID>" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"state":"acknowledged"}'` -- 200
5. False positive: `curl -k -X PATCH "http://localhost:8084/api/v1/analytics/anomalies/<ID>" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"state":"false_positive"}'` -- 200
6. Unit testler: `go test ./internal/analytics/anomaly/... ./internal/store/ ./internal/api/anomaly/... -v`

---

## STORY-037: SIM Connectivity Diagnostics

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. SIM teshis: `curl -k -X POST "http://localhost:8084/api/v1/sims/<SIM_UUID>/diagnose" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{}'` -- 200 + steps[], overall_status
2. Test auth ile: `curl -k -X POST "http://localhost:8084/api/v1/sims/<SIM_UUID>/diagnose" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"include_test_auth":true}'` -- 200 + 7 adim
3. Cache testi: Ayni istek 1 dakika icinde tekrar -- cached sonuc donmeli
4. Gecersiz SIM: `curl -k -X POST "http://localhost:8084/api/v1/sims/00000000-0000-0000-0000-000000000000/diagnose" -H "Authorization: Bearer $TOKEN"` -- 404
5. Unit testler: `go test ./internal/diagnostics/... ./internal/api/diagnostics/... -v`

---

## STORY-038: Notification Engine (Multi-Channel)

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Bildirim listele: `curl -k "http://localhost:8084/api/v1/notifications" -H "Authorization: Bearer $TOKEN"` -- 200 + paginated list (unread first)
2. Okundu isaretle: `curl -k -X PATCH "http://localhost:8084/api/v1/notifications/<ID>/read" -H "Authorization: Bearer $TOKEN"` -- 200
3. Tumunu okundu: `curl -k -X POST "http://localhost:8084/api/v1/notifications/read-all" -H "Authorization: Bearer $TOKEN"` -- 200 + updated_count
4. Tercihler: `curl -k "http://localhost:8084/api/v1/notification-configs" -H "Authorization: Bearer $TOKEN"` -- 200 + channels, events, thresholds
5. Tercih guncelle: `curl -k -X PUT "http://localhost:8084/api/v1/notification-configs" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"channels":{"email":true,"telegram":false},"events":{"operator.down":true}}'` -- 200
6. Unit testler: `go test ./internal/notification/... ./internal/store/ ./internal/api/notification/... -v`

---

## STORY-039: Compliance Reporting & Auto-Purge

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. Dashboard: `curl -k "http://localhost:8084/api/v1/compliance/dashboard" -H "Authorization: Bearer $TOKEN"` -- 200 + state counts, pending purges, compliance %
2. BTK rapor: `curl -k "http://localhost:8084/api/v1/compliance/btk-report" -H "Authorization: Bearer $TOKEN"` -- 200 + operator breakdown
3. BTK CSV: `curl -k "http://localhost:8084/api/v1/compliance/btk-report?format=csv" -H "Authorization: Bearer $TOKEN"` -- CSV dosyasi
4. Retention guncelle: `curl -k -X PUT "http://localhost:8084/api/v1/compliance/retention" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"retention_days":180}'` -- 200
5. DSAR: `curl -k "http://localhost:8084/api/v1/compliance/dsar/<SIM_UUID>" -H "Authorization: Bearer $TOKEN"` -- 200 + SIM data JSON
6. Erasure: `curl -k -X POST "http://localhost:8084/api/v1/compliance/erasure/<SIM_UUID>" -H "Authorization: Bearer $TOKEN"` -- 200
7. Unit testler: `go test ./internal/compliance/... ./internal/store/ ./internal/job/ ./internal/api/compliance/... -v`

---

## STORY-040: WebSocket Event Server

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. WS baglantisi: `wscat -c "ws://localhost:8081/ws/v1/events?token=$TOKEN"` -- auth.ok mesaji gelmeli
2. Event dinle: Baglanti sonrasi session/alert/job event'leri gelmeli
3. Subscribe: `{"type":"subscribe","events":["session.started","alert.new"]}` gonderin -- sadece subscribe edilen event'ler gelmeli
4. Metrics: Her 1 saniyede metrics.realtime event'i gelmeli
5. Max connection: Ayni tenant ile 101. baglanti denemesi -- 4002 close code
6. Unit testler: `go test ./internal/ws/... -v`

---

## STORY-041: React Scaffold & Routing

1. Dev server: `cd web && npm run dev` -- tarayicida http://localhost:5173 acilmali
2. Login sayfasi: /login rotasinda AuthLayout gorunmeli (ortalanmis kart, Argus logosu)
3. Dashboard layout: / rotasinda sidebar + topbar gorunmeli
4. Sidebar collapse: sidebar kucultme butonu calismali
5. Dark mode: varsayilan dark, sag ust menuden light'a gecis yapilabilmeli
6. Command palette: Ctrl+K ile arama paleti acilmali
7. Tum rotalar: /sims, /analytics, /policies vb. tum rotalarda placeholder sayfa gorunmeli
8. Build: `cd web && npm run build` -- hatasiz build, dist/ klasoru olusturulmali

---

## STORY-042: Frontend Auth (Login + 2FA)

1. Login sayfasi: http://localhost:8084/login adresine gidin -- email/password formu gorunmeli
2. Gecersiz giris: yanlis sifre ile giris deneyin -- "Invalid credentials" hatasi gorunmeli
3. Basarili giris: admin@argus.io / admin ile giris -- dashboard'a yonlendirilmeli
4. 2FA akisi: 2FA aktif kullanici ile giris -- /login/2fa sayfasina yonlendirilmeli
5. 2FA kodu: 6 haneli kodu girin -- auto-focus bir sonraki input'a gecmeli, tamamlaninca submit olmali
6. Protected route: /sims adresine auth olmadan gidin -- /login'e yonlendirilmeli
7. Logout: sidebar'daki logout butonuna tiklayin -- auth temizlenmeli, /login'e donulmeli
8. Remember me: "Beni hatirla" secenegini tiklayin -- uzun sureli oturum

---

## STORY-043: Frontend Main Dashboard

1. Dashboard: http://localhost:8084/ adresine gidin -- 4 metrik karti gorunmeli (Total SIMs, Active Sessions, Auth/s, Monthly Cost)
2. Auth/s canli: Auth/s kartinda LIVE etiketi, deger her saniye guncellenmeli
3. SIM dagitimi: Pasta grafik SIM durumlarini gostermeli (active, suspended, vb.)
4. Operator sagligi: Her operator icin renkli saglik cubugu gorunmeli (yesil/sari/kirmizi)
5. APN trafigi: Top 5 APN cubuk grafigi gorunmeli
6. Alert feed: Son 10 alert listesi, severity ikonu, zaman damgasi
7. Canli alert: Yeni alert geldiginde listenin basina eklenmeli
8. Skeleton: Sayfa yuklenirken iskelet animasyonu gorunmeli
9. Hata durumu: API hatasi olursa retry butonu gorunmeli

---

## STORY-044: Frontend SIM List + Detail

1. SIM listesi: /sims adresine gidin -- data table gorunmeli (ICCID, IMSI, State, vb.)
2. Segment filtre: segment dropdown'dan bir segment secin -- liste filtrelenmeli
3. Arama: ICCID ile arama yapin -- eslesen SIM'ler gorunmeli
4. Filtre: State=active filtresi secin -- sadece aktif SIM'ler
5. Scroll: Asagi scroll edin -- sonraki sayfa otomatik yuklenmeli
6. Bulk islem: 3 SIM secin -- bulk action toolbar cikmalı (suspend/resume/terminate)
7. SIM detay: Bir SIM satirina tiklayin -- /sims/:id detay sayfasi acilmali
8. Tabs: Overview, Sessions, Usage, Diagnostics, History tab'lari gorunmeli
9. Diagnostics: "Run Diagnostics" butonuna tiklayin -- adim adim sonuclar gorunmeli
10. History: State gecis timeline'i gorunmeli (renkli dot'lar, tarih, neden)

---

## STORY-045: Frontend APN + Operator Pages

1. APN listesi: /apns -- kart grid, her kartta SIM sayisi, trafik, IP kullanim cubugu
2. APN detay: Bir APN kartina tiklayin -- config, IP pool istatistikleri, bagli SIM'ler, trafik grafigi
3. Operator listesi: /operators -- saglik noktali kart grid (yesil/sari/kirmizi)
4. Operator detay: Bir operator kartina tiklayin -- saglik timeline, circuit breaker durumu
5. Test baglanti: "Test Connection" butonu -- basarili/basarisiz mesaji
6. Canli guncelleme: Operator saglik degisikligi WS ile otomatik guncellenmeli

---

## STORY-046: Frontend Policy DSL Editor

1. Policy listesi: /policies -- tablo gorunmeli (isim, versiyon, SIM sayisi, durum)
2. Yeni policy: "Create Policy" butonu -- dialog ile yeni policy olusturma
3. Policy editor: Bir policy'ye tiklayin -- split-pane editor acilmali
4. Syntax highlighting: POLICY, MATCH, WHEN gibi anahtar kelimeler renkli gorunmeli
5. Dry-run preview: Sag panelde "Preview" tab'inda etkilenen SIM sayisi gorunmeli
6. Versiyon yonetimi: "Versions" tab'inda surumleri gorun, diff karsilastirma yapin
7. Rollout: "Rollout" tab'inda staged rollout baslatma (1%->10%->100%)
8. Kaydet: Ctrl+S ile draft kaydedin
9. Activate: "Activate" butonuyla versiyon aktif edin (onay dialog'u)

---

## STORY-047: Frontend Sessions + Jobs + eSIM + Audit

1. Live sessions: /sessions -- canli oturum tablosu, WS ile yeni oturum animasyonlu eklenmeli
2. Sessions stats: Toplam aktif, operator bazli sayilar gorunmeli
3. Force disconnect: Bir oturumda "Disconnect" butonu -- onay dialog'u ile sonlandirma
4. Jobs: /jobs -- progress bar'li is tablosu, filtre (type/state)
5. Job detay: Bir is'e tiklayin -- detay paneli, hata raporu, retry/cancel butonlari
6. eSIM: /esim -- profil tablosu, enable/disable/switch butonlari
7. Audit: /audit -- aranabilir log tablosu, filtreleme, satirlari genisletme (JSON diff)
8. Hash chain: "Verify Integrity" butonu -- dogrulama sonucu

---

## STORY-048: Frontend Analytics Pages

1. Usage: /analytics -- zaman serisi grafik, period seçici (1h/24h/7d/30d), group-by toggle
2. Cost: /analytics/cost -- maliyet kartı, operator karşılaştırma bar chart, optimizasyon önerileri
3. Anomalies: /analytics/anomalies -- severity badge'li tablo, satır genişletme, acknowledge/resolve

---

## STORY-056: Critical Runtime Fixes

**Ekran:** IP Pools (SCR-112)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 1 | /settings/ip-pools sayfasina git | Sayfa hatasiz yuklenir, utilization barlari gorunur |
| 2 | Bir IP pool detayina tikla | Detay sayfasi yuklenir, CIDR bilgisi (v4 veya v6) gorunur |

**Ekran:** Tenants (SCR-121)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 3 | /system/tenants sayfasina git | Tenant listesi hatasiz yuklenir |
| 4 | Tenant olustur/duzenle dialogunu ac | Tum alanlar dogru gorunur, nullable alanlar bos olabilir |

**Ekran:** Sessions (SCR-050)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 5 | /sessions sayfasina git | Session listesi 200 ile yuklenir (500 yok) |
| 6 | SIM detay > Sessions tab'ina tikla | Session verileri gorunur |

**Ekran:** Audit Log (SCR-090)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 7 | /audit sayfasina git | Audit log listesi 200 ile yuklenir (404 yok) |

**Ekran:** APN List (SCR-030)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 8 | /apns sayfasinda "Create APN" butonuna tikla | Dialog hemen acilir |
| 9 | Formu doldurup kaydet | Dialog kapanir, liste yenilenir |

**Ekran:** Dashboard (SCR-010)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 10 | Header'daki bildirim ikonuna bak | Okunmamis bildirim sayisi badge olarak gorunur |

**Ekran:** Tum Sayfalar

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 11 | Browser DevTools WS panelini ac | ws://localhost:8084/ws/v1/events baglantisi kurulur |
| 12 | Bir sayfada hata olustur, baska sayfaya git | Yeni sayfa duzgun yuklenir, hata ekrani temizlenir |
| 13 | Browser Network panelinde favicon.ico kontrol et | 200 doner, 404 yok |

**Altyapi:**

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 14 | curl -I http://localhost:8084 | 200 doner, HTTPS redirect yok |
| 15 | docker compose ps | NATS container calisiyor |
| 16 | make build && make up | Basarili (Dockerfile yeni konumda) |

---

## STORY-057: Data Accuracy & Missing Endpoints

**Ekran:** Dashboard (SCR-001)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 1 | Dashboard'a git, "Top 5 APNs" widget'ina bak | APN isimleri gorunur (UUID degil) |
| 2 | "Operator Health" bolumune bak | Operator listesi gorunur (seed varsa 3 operator) |
| 3 | "Monthly Cost" kartina bak | CDR varsa 0'dan buyuk deger, yoksa 0 |
| 4 | KPI sparkline grafiklere bak | Gercek 7 gunluk trend (rastgele degil) |

**Ekran:** SIM Detail �� Sessions Tab (SCR-041)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 5 | Bir SIM detayina git, Sessions tab'ina tikla | Session listesi /sims/:id/sessions endpoint'inden yuklenir |

**Ekran:** SIM Detail — Usage Tab (SCR-042)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 6 | Usage tab'ina tikla, period sec (24h/7d/30d) | Gercek CDR verileriyle grafik cikar, Math.random yok |

**Ekran:** APN Detail — Connected SIMs (SCR-060)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 7 | APN detayinda "Connected SIMs" tab'ina tikla | SIM listesi /apns/:id/sims endpoint'inden yuklenir |

**Ekran:** SIM Edit

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 8 | SIM detayinda label/notes degistir | PATCH /sims/:id basarili, audit log olusur |
| 9 | Terminated SIM'de edit dene | 422 hatasi, guncelleme engellenir |

**Ekran:** Login (SCR-011)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 10 | "Beni hatirla" tikli login yap | JWT suresi 7 gun (normal: 15dk) |

---

## STORY-058: Frontend Consolidation & UX Completeness

**Ekran:** SIM Listesi (SCR-045)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 1 | 5 SIM sec, "Assign Policy" butonuna tikla | Inline dialog acilir, policy picker gorunur |
| 2 | Policy sec, "Confirm" tikla | Bulk job olusur, secim temizlenir |
| 3 | Segment filtrele, "Select all N SIMs" tikla | Tum segment secilir (sadece gorunen satirlar degil) |

**Ekran:** SIM Detail (SCR-075)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 4 | Sessions tab'inda hata olustur | Sadece o tab hata gosterir, diger tablar calisir |
| 5 | RATBadge gorunuyor mu kontrol et | Kompakt mono badge (LTE, 5G NR vb.) |
| 6 | InfoRow gorunuyor mu kontrol et | Label sol, value sag, tutarli yapi |

**Ekran:** Live Sessions (SCR-070)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 7 | Filtre uygula (operator), WS event gelsin | Filtreye uymayan event tabloda gorunmez |

**Ekran:** eSIM (SCR-072)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 8 | Operator dropdown'dan operator sec | Liste API uzerinden filtrelenir |

**Ekran:** Audit Log (SCR-080)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 9 | User dropdown'dan kullanici sec | Audit listesi o kullaniciya filtrelenir |

**Ekran:** Jobs (SCR-071)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 10 | Jobs tablosunda "Created By" kolonunu kontrol et | Kullanici ismi/email gorunur |

**Altyapi:**

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 11 | npm run build | Chunk size uyarisi yok |
| 12 | Lazy-loaded sayfaya git (Dashboard) | Skeleton fallback gorunur, sonra sayfa yukler |

---

## STORY-059: Security & Compliance Hardening

**Ekran:** 2FA Setup (SCR-015)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 1 | Profil > "2FA Etkinlestir" butonuna tikla | QR kod ve gizli kod (plaintext) kullaniciya gosterilir |
| 2 | Authenticator app ile QR'i okut, kodu gir, dogrula | 2FA aktiflesir; DB'de `users.totp_secret` ciphertext (base64) olarak saklanir |
| 3 | Tekrar login ol, 2FA kodunu gir | Dogrulama basarili — decrypt akisi sessiz calisir |

**Ekran:** Compliance Reports (SCR-125)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 4 | BTK Monthly Report sec, format=JSON secili "Generate" | Rapor onizlemesi gelir |
| 5 | Format=CSV sec ve indir | Tarayici CSV dosyasini `btk_report_YYYYMM.csv` olarak indirir |
| 6 | Format=PDF sec ve indir | Tarayici PDF dosyasini `btk_report_YYYYMM.pdf` olarak indirir; icerikte operator tablosu + toplam var |

**Ekran:** Notification Channels (SCR-110)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 7 | Webhook channel'i etkinlestir, bos URL ile kaydet | Inline hata: "HTTPS URL gerekli" — submit engellenir |
| 8 | URL `http://example.com` yaz | Inline hata: "URL https:// ile baslamali" |
| 9 | URL `https://hook.example.com/x` + bos secret | Inline hata: "Secret gerekli" — submit engellenir |
| 10 | URL + secret dolu, kaydet | Basarili; webhook kanali aktif |

**Ekran:** SIM Detail — State (SCR-030)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 11 | State=`stolen_lost` olan SIM'e git, Durum degistir > Terminate | Dogrulama dialog sonrasi state=`terminated`, history row olusur, IP grace period baslar |
| 12 | State=`stolen_lost` badge'i goruntule | Tehlike (danger) renk tokeni ile gosterilir |

**Altyapi:**

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 13 | Tenant A WS baglantisi, tenant B bir policy event publish et | Tenant A event'i ALMAZ (tenant isolation) |
| 14 | System event (tenant_id=nil) publish et | Tum tenant baglantilari event'i ALIR |
| 15 | `make vuln-check` calistir | `govulncheck ./...` 0 high/critical bildirir |
| 16 | `make web-audit` calistir | `npm audit --audit-level=high` 0 vulnerability bildirir |

---

## STORY-060: AAA Protocol Correctness

**Not:** Bu story backend/protokol seviyesi duzeltmelerden olusuyor — UI tarafinda sadece CoA dispatch sayilari mevcut ekranlarda gorunur (Live Sessions, Policy Editor). Ana testler backend ve protocol seviyesinde yapilir.

**Ekran:** Live Sessions (SCR-070)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 1 | Aktif bir SIM icin bulk policy assign tetikle | Jobs tablosunda `coa_sent_count`, `coa_acked_count`, `coa_failed_count` sayaclari gorunur |
| 2 | Policy editor > staged rollout > progress takip et | CoA dispatch sayisi UI'da arttigi gorulur |

**Ekran:** eSIM (SCR-072)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 3 | Aktif oturumu olan bir SIM icin profil degisimi dene (force=false) | Once DM gider, sonuc `disconnected_sessions` field'inda doner; DM NAK ise 409 SESSION_DISCONNECT_FAILED |
| 4 | Ayni durumda `force=true` ile dene | DM atlanir, profil degisimi direkt yapilir |

**WebSocket Davranisi (dev-browser/backend):**

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 5 | WS client ping'e 95 saniye yanit verme | Sunucu baglantiyi `pongWait` sonrasi kapatir (default 90s) |
| 6 | WS client hizli yavas — 300+ mesaj buffer'a yigil | Eski mesajlar dusurulur (drop-oldest), yeni mesajlar alinir; `DroppedMessageCount` artar |
| 7 | Ayni kullanici 6. WS baglantisi acsin | 1. baglanti close code 4029 ile kapatilir, 6. baglanti aktif kalir |
| 8 | Sunucu shutdown baslat | Tum baglantilar `{"type":"reconnect","data":{...,"after_ms":2000}}` alir, sonra baglantilar kapanir |

**Protokol/Altyapi:**

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 9 | EAP-SIM authentication spec-uyumlu MAC ile gonder | Access-Accept + MSK (ConsumeSessionMSK in-memory hit) |
| 10 | EAP-SIM eski test-compat simple-SRES path ile gonder | Access-Reject — RFC 4186 strict |
| 11 | Diameter peer `openssl s_client` ile TLS bagla | TLS 1.2+ handshake OK, CER/CEA akar |
| 12 | Diameter peer gecersiz sertifika ile TLS bagla (mTLS on) | Handshake reddedilir |
| 13 | DSL policy: `WHEN rat_type == "NB_IOT"` ve `"nb_iot"` | Her ikisi ayni canonical RAT'e cozumlenir |
| 14 | Canonical olmayan rat_type degerleri icin migration calistir | `sessions`, `sims`, `cdrs` tablolarinda normalize edilir |

---

## STORY-061: eSIM Model Evolution

**Ekran:** SIM Detail — eSIM Tab (SCR-021 eSIM sekmesi)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 1 | eSIM tipli bir SIM detay sayfasini ac, eSIM sekmesine tikla | Profil kartlari listelenir; her kartta profil durumu badge'i (available=mavi, enabled=yesil, disabled=turuncu, deleted=gri) gorunur |
| 2 | "Load Profile" butonuna tikla | Dialog acilir; EID, Operator, ICCID, Profile ID alanlari doldurulur ve kaydedilir |
| 3 | Yeni yuklenen profil kartinda durumu kontrol et | `available` durumunda gorunur |
| 4 | `available` durumdaki profil icin "Enable" butonuna tikla | Profil `enabled` olur; onceki `enabled` profil `available` durumuna gecer (DEV-164) |
| 5 | `enabled` durumdaki profil icin "Switch" acilir menusunden hedef profil sec | Eski profil `available`, yeni profil `enabled` olur; IP serbest birakilir; policy temizlenir |
| 6 | `available` ya da `disabled` durumundaki profil icin "Delete" butonuna tikla | Onay dialog'u cikip soft-delete yapilir |
| 7 | `enabled` durumundaki profil icin silmeyi dene | 409 CANNOT_DELETE_ENABLED_PROFILE hatasi gorunur |
| 8 | Ayni SIM'e 9 profil yuklemeyi dene | 422 PROFILE_LIMIT_EXCEEDED (max 8) hatasi gorunur |
| 9 | Profil kartinda `profile_id` alani gorulur | profile_id varsa kartta gosterilir (bos ise gosterilmez) |
| 10 | eSIM olmayan (physical) SIM detayinda eSIM sekmesi | Sekme gorunmez ya da profil listesi bos + CTA gorunur |

**Ekran:** eSIM Profiles (SCR-070, /esim)

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 11 | /esim sayfasini ac | Tum tenant eSIM profilleri listelenir; durum filtreleme calisir |
| 12 | `available` ya da `disabled` durumdaki profil satirinda "Delete" butonuna tikla | Onay sonrasi soft-delete basarili |

**Altyapi:**

| # | Senaryo | Beklenen Sonuc |
|---|---------|----------------|
| 13 | `psql ... -c "SELECT profile_state, COUNT(*) FROM esim_profiles GROUP BY profile_state;"` | `available`, `enabled`, `disabled`, `deleted` satirlari gorunebilir |
| 14 | `psql ... -c "INSERT INTO esim_profiles (sim_id,...,profile_state) VALUES (uuid,'enabled',...); INSERT ..." -- ayni sim_id ile ikinci 'enabled' deneme` | Partial unique constraint hatasi: "duplicate key value violates unique constraint idx_esim_profiles_sim_enabled" |

---

## STORY-063: Backend Implementation Completeness

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `curl http://localhost:8084/api/health` -- DB, Redis, NATS, AAA probe sonuclari ile 200 ya da 503 donmeli (hicbir probe calismadiysа 503)
2. `psql ... -c "SELECT id, tenant_id, operator_id, score FROM sla_reports LIMIT 5;"` -- TBL-27 tablosu mevcut ve kayit icermeli (periyodik job calistiysa)
3. `curl -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/sla-reports` -- API-183: SLA rapor listesi donmeli
4. `curl -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/sla-reports/$REPORT_ID` -- API-184: Tek SLA raporu donmeli
5. `ESIM_SMDP_PROVIDER=generic` env set edildiginde eSIM profil download isteği gercek HTTP SM-DP+ adapter'ina yonlendirilen log'u kontrol et
6. `SBA_NRF_URL=http://nrf.5g.local` env set edildiginde uygulama baslarken NRF NFRegister log girdisi gorulmeli
7. `psql ... -c "SELECT id, user_id FROM sessions WHERE id='...';"` -- Oturum DB'ye yazilmali (sadece Redis degil)
8. `make test` -- 1859 test gecmeli, hicbir skiplenmis test olmamali

---

## STORY-064: Database Hardening & Partition Automation

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make db-migrate` -- 6 yeni migration temiz uygulanmali (20260412000003..008). Sonra `make db-rollback` ile geri alinabilmeli ve tekrar `make db-migrate` ile ileri gidebilmeli (round-trip).
2. `psql ... -c "\d sims" | grep chk_sims_state` -- enum CHECK kisitlari aktif olmali (9 kisit toplam: tenants, users, sims, apns, policies, policy_versions, operators)
3. `psql ... -c "INSERT INTO sims (tenant_id, operator_id, imsi, iccid, msisdn, state) VALUES (..., 'invalid_state');"` -- CHECK violation hatasi donmeli (`chk_sims_state`)
4. `psql ... -c "\d+ audit_logs" | grep 2027_03` -- bootstrap migration'in 2027_03 partition'ini olusturdugu gorulmeli (toplam 2026_07..2027_03 = 9 ay, hem audit_logs hem sim_state_history)
5. `psql ... -c "SELECT count(*) FROM pg_policies WHERE policyname LIKE '%_tenant_isolation';"` -- 28 RLS policy gorulmeli
6. `psql ... -c "SELECT relname, relforcerowsecurity FROM pg_class WHERE relname = 'sims';"` -- `relforcerowsecurity = t` gorulmeli (FORCE RLS)
7. `psql ... -c "INSERT INTO esim_profiles (sim_id, eid, profile_id, operator_id, profile_state) VALUES ('00000000-0000-0000-0000-000000000000', ..., 'available');"` -- FK trigger `check_sim_exists` exception donmeli (sim_id yok)
8. `psql ... -c "EXPLAIN SELECT * FROM sessions WHERE sim_id = '...' ORDER BY started_at DESC LIMIT 10;"` -- `Index Scan using idx_sessions_sim_started` gorulmeli (Seq Scan degil)
9. `curl -H "Authorization: Bearer $TOKEN" "http://localhost:8084/api/v1/auth/sessions?limit=20"` -- API-186: Oturum listesi donmeli, `meta.cursor` alanini icermeli (50+ oturum varsa)
10. `curl -H "Authorization: Bearer $TOKEN" "http://localhost:8084/api/v1/notifications/configs?limit=20"` -- notification_configs cursor pagination calismasi
11. `psql ... -c "SELECT supported_rat_types FROM operator_grants LIMIT 5;"` -- `supported_rat_types` kolonu mevcut (TEXT[], default '{}')
12. `psql ... -c "SELECT indexname FROM pg_indexes WHERE indexname = 'idx_operator_grants_rat_types_gin';"` -- GIN index mevcut
13. Cron log: `docker compose logs argus | grep partition_creator` -- gunluk 02:00 UTC tick'inde `partition ensured` log girdisi
14. `make lint-sql` -- `OK: no SELECT * in store layer` ciktisi
15. `make test` -- tum testler gecmeli (1945+ test)

---

## STORY-065: Observability & Tracing Standardization

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml up -d` -- prometheus, grafana, otel-collector, node-exporter, redis-exporter calismali
2. `curl http://localhost:8080/metrics` -- Prometheus text formatinda `# HELP argus_http_requests_total`, `go_goroutines`, `process_resident_memory_bytes` satirlari gorulmeli
3. `curl -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/sims` -- istek sonrasi `/metrics` ciktisinda `argus_http_requests_total{method="GET",route="/api/v1/sims",status="200",tenant_id="..."}` counter artmali
4. `docker compose logs argus | grep tenant_id` -- kimligi dogrulanmis her log satirinda `tenant_id=<uuid>` alani olmali
5. `curl http://localhost:9090/api/v1/targets` -- Prometheus UI'da argus target `UP` gorulmeli
6. `curl http://localhost:9090/api/v1/rules` -- 9 alert kurali yuklenmis olmali (ArgusHighErrorRate, ArgusAuthLatencyHigh, ArgusOperatorDown, ArgusCircuitBreakerOpen, ArgusDBPoolExhausted, ArgusNATSConsumerLag, ArgusJobFailureRate, ArgusRedisEvictionStorm, ArgusDiskSpaceLow)
7. Grafana: `http://localhost:3000` (admin/admin) -> Argus klasorunde 6 dashboard (overview, aaa, database, messaging, tenant, jobs)
8. Tenant dashboard: `$tenant_id` variable dropdown'dan bir tenant sec -- filtreli panel'ler cizilmeli
9. OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 env ile argus restart -> otel-collector `docker logs` ciktisinda span debug log'lari gorulmeli
10. RADIUS auth isteği yap -> `argus_aaa_auth_requests_total{protocol="radius",operator_id="...",result="success",tenant_id="..."}` counter artmali
11. Operator saglik state degistir (adapter mock) -> `argus_operator_health{operator_id="..."}` gauge 2->1->0 gecis gormeli
12. DB pool metric: `curl /metrics | grep argus_db_pool_connections` -- `state="idle"`, `state="in_use"`, `state="total"`, `state="max"` labels
13. Slow query test: `psql -c "SELECT pg_sleep(0.15)"` -> span attribute `argus.db.slow=true` (Tempo/Jaeger uzerinde gorulebilir, debug exporter stdout'unda da)
14. Circuit breaker open simulasyonu (operator failover test) -> `argus_circuit_breaker_state{operator_id="...",state="open"} == 1`
15. `go test -tags integration ./internal/observability/... -race` -- 1 integration test gecmeli
16. `go test ./...` -- 2009+ test gecmeli
