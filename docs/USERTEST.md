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

---

## STORY-066: Reliability, Backup, DR & Runtime Hardening

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `curl http://localhost:8080/health/live` -- `{"status":"ok","data":{"state":"alive"}}` donmeli (her zaman 200)
2. `curl http://localhost:8080/health/ready` -- `{"status":"ok","data":{"state":"healthy","checks":{...},"disks":[...]}}` donmeli; `disks` dizisi yapilandirilmis mount noktalarini icermeli
3. `curl http://localhost:8080/health/startup` -- Baslangictan 60 saniye sonra hazirlik probe'una delege eder; erken cagrida 503 donmeli
4. `curl -H "Authorization: Bearer $SUPERADMIN_TOKEN" http://localhost:8084/api/v1/system/backup-status` -- `{"last_daily":null,"last_weekly":null,"last_monthly":null,"last_verify":null,"history":[]}` (ilk calistirmada, henuz backup yok); Backup processor calishiktan sonra dolu donmeli
5. `curl -H "Authorization: Bearer $SUPERADMIN_TOKEN" http://localhost:8084/api/v1/system/jwt-rotation-history` -- `{"current_fingerprint":"...","previous_fingerprint":"","history":[]}` donmeli; JWT_SECRET_PREVIOUS degistiginde history kaydi gorulmeli
6. `curl http://localhost:8080/metrics | grep argus_disk_usage_percent` -- konfigureli mount noktalari icin gauge serisi donmeli
7. `docker compose logs argus | grep "disk probe configured"` -- Baslangicta mount noktalarini iceren yapilandirilmis log kaydi gorulmeli
8. `docker compose logs argus | grep "backup processor started"` -- Backup islemcisinin baslatildigina dair log kaydi gorulmeli
9. SIGTERM gonderme: `docker compose stop argus` -- Graceful shutdown log sirasini gozlemle: HTTP drain → RADIUS drain → Diameter drain → 5G SBA drain → WS drain → jobs → NATS → Redis → PG (30 saniye icinde tamamlanmali)
10. pprof erisilebilirlik: `PPROF_ENABLED=false` varsayilaniyla `curl http://localhost:6060/debug/pprof/` -- 404 veya baglanti reddedilmeli; `PPROF_ENABLED=true` ve `PPROF_TOKEN` ile `curl "http://localhost:6060/debug/pprof/?token=$PPROF_TOKEN"` -- profil ciktisi donmeli
11. `psql ... -c "\d backup_runs"` -- TBL-32 tablosu mevcut olmali (kind, state, s3_bucket, s3_key, sha256 kolonlari)
12. `psql ... -c "\d backup_verifications"` -- TBL-33 tablosu mevcut olmali (backup_run_id FK, tenants_count, sims_count kolonlari)
13. `make test` -- 2135 test gecmeli, hicbir skiplenmis test olmamali

---

## STORY-067: CI/CD Pipeline, Deployment Strategy & Ops Tooling

Bu story icin manuel test senaryosu yok (backend/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `make build-ctl` -- `dist/argusctl` binary'si derlenmeli; `./dist/argusctl --help` calistirildiginda alt komutlar listesi (tenant, apikey, user, compliance, sim, health, backup) gorulmeli
2. `./dist/argusctl tenant list --token $SUPERADMIN_TOKEN` -- JSON veya tablo formatinda tenant listesi donmeli
3. `./dist/argusctl health --token $SUPERADMIN_TOKEN` -- `{"status":"healthy","checks":{...}}` donmeli
4. `bash -n deploy/scripts/bluegreen-flip.sh` -- syntax hatasi olmamali
5. `bash -n deploy/scripts/rollback.sh` -- syntax hatasi olmamali
6. `bash -n deploy/scripts/smoke-test.sh` -- syntax hatasi olmamali
7. `curl http://localhost:8084/api/v1/status` -- auth gerekmeden `{"status":"ok","data":{"version":"...","uptime_sec":...,"components":{...}}}` donmeli
8. `curl -H "Authorization: Bearer $SUPERADMIN_TOKEN" http://localhost:8084/api/v1/status/details` -- per-dependency latency, disk ve queue depth bilgisi icermeli
9. `curl -s -o /dev/null -w "%{http_code}" http://localhost:8084/api/v1/status` -- 200 donmeli
10. `curl -X POST http://localhost:8084/api/v1/audit/system-events -d '{"action":"test"}' -H "Content-Type: application/json"` -- 401 donmeli (auth gerekli)
11. `curl -X POST -H "Authorization: Bearer $SUPERADMIN_TOKEN" http://localhost:8084/api/v1/audit/system-events -H "Content-Type: application/json" -d '{"action":"deploy.blue-green","entity_type":"deployment","entity_id":"test-001"}' ` -- 201 donmeli; `{"status":"recorded","action":"deploy.blue-green","entity_type":"deployment","entity_id":"test-001"}`
12. `curl -X POST -H "Authorization: Bearer $TENANT_ADMIN_TOKEN" http://localhost:8084/api/v1/audit/system-events -H "Content-Type: application/json" -d '{"action":"test","entity_type":"test","entity_id":"test"}' ` -- 403 donmeli (super_admin gerekli)
13. `psql ... -c "SELECT state FROM users WHERE id = '$USER_ID'"` -- GDPR silme islemi sonrasi `purged` donmeli
14. `curl http://localhost:8080/metrics | grep argus_build_info` -- `argus_build_info{version="...",git_sha="...",build_time="..."}` gauge serisi donmeli
15. `make test` -- 2182 test gecmeli, hicbir skiplenmis test olmamali

---

## STORY-068: Enterprise Auth & Access Control Hardening

Bu story'nin UI bilesenleri vardir. Docker ortaminda calistirmak icin `make up` komutu kullanin.

**On kosul:** `make up` ile ortami baslat. Admin hesabiyla giris yap (`admin@argus.io` / `admin`).

### AC-1: Sifre Politikasi

1. Settings → Users → "Invite User" veya kullanici olustur -- zayif sifre ile dene:
   - `short1A!` (12 karakden az) -- 422 `PASSWORD_TOO_SHORT` donmeli
   - `alllowercase1!` (buyuk harf yok) -- 422 `PASSWORD_MISSING_CLASS` donmeli
   - `ALLUPPERCASE1!` (kucuk harf yok) -- 422 `PASSWORD_MISSING_CLASS` donmeli
   - `NoDigitHere!!` (rakam yok) -- 422 `PASSWORD_MISSING_CLASS` donmeli
   - `NoSymbol12345` (ozel karakter yok) -- 422 `PASSWORD_MISSING_CLASS` donmeli
   - `ValidLong1!ValidLong1!` ancak `aaaa` iceriyorsa (>3 tekrar) -- 422 `PASSWORD_REPEATING_CHARS` donmeli
   - `ValidLongPass1!` -- 201 kabul edilmeli
2. API ile kontrol:
   ```bash
   curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/users \
     -d '{"email":"test@example.com","name":"Test","role":"analyst","password":"weak"}' \
     -H "Content-Type: application/json"
   # 422 PASSWORD_TOO_SHORT donmeli
   ```

### AC-2: Sifre Gecmisi

1. Kullanici olustur ve giris yap. Simdi sifreni degistir (`POST /api/v1/auth/password/change`):
   - Ayni sifre ile tekrar dene -- 422 `PASSWORD_REUSED` donmeli
   - 5 farkli sifre ile degistir, ardindan ilk sifreyi tekrar dene -- `PASSWORD_REUSED` donmeli (son 5 sifreye girmemeli)
   - 6. sifreyi gir, ardindan ilk sifre tekrar gecerli olmali (gecmis penceresi doldu)
2. DB dogrulamasi:
   ```bash
   psql ... -c "SELECT user_id, created_at FROM password_history WHERE user_id = '$USER_ID' ORDER BY created_at DESC LIMIT 5;"
   # Son 5 kayit gorulmeli
   ```

### AC-3: Zorunlu Sifre Degisikligi (Force-Change Flow)

1. Admin kullanicisinin `password_change_required` bayragi set et:
   ```bash
   psql ... -c "UPDATE users SET password_change_required = true WHERE email = 'testuser@example.com';"
   ```
2. Bu kullanici ile giris yap:
   ```bash
   curl -X POST http://localhost:8084/api/v1/auth/login \
     -d '{"email":"testuser@example.com","password":"ValidLongPass1!"}' \
     -H "Content-Type: application/json"
   # {"status":"ok","data":{"partial":true,"reason":"password_change_required"},...} donmeli
   ```
3. Tarayicida giris yap -- `http://localhost:8084/auth/change-password` sayfasina yonlendirilmeli
4. Sifre degistirme formunu doldur (mevcut + yeni sifre) -- tam JWT ile basarili giris
5. Ayni kullanici ile tekrar giris yap -- artik yonlendirme olmamali (bayrak temizlenmeli)
6. DB dogrulamasi: `psql ... -c "SELECT password_change_required, password_changed_at FROM users WHERE email = 'testuser@example.com';"` -- `false` + timestamp gorulmeli

### AC-4: 2FA Backup/Recovery Kodlari

1. Kullanici olustur ve 2FA'yi aktive et (Settings → Security → Enable 2FA)
2. 2FA kurulum ekraninda 10 adet tek kullanımlik kod goruntulenmeli; "I have saved these codes" onay kutusu tiklanmali
3. 2FA aktif bir hesapla giris yap, TOTP kodu yerine backup kod kullan:
   ```bash
   curl -X POST http://localhost:8084/api/v1/auth/login \
     -d '{"email":"2fauser@example.com","password":"ValidLongPass1!","backup_code":"<KOD>"}' \
     -H "Content-Type: application/json"
   # Basarili giris; meta.backup_codes_remaining gorunmeli
   ```
4. Ayni kodu tekrar kullan -- 401 donmeli (kullanilmis kod)
5. 2 kod kalindiginda uyari gormeli (`meta.backup_codes_remaining < 3`)
6. Settings → Security → "Regenerate Backup Codes" -- eski kodlar gecersiz, 10 yeni kod uretilmeli
7. DB dogrulamasi:
   ```bash
   psql ... -c "SELECT id, used_at FROM user_backup_codes WHERE user_id = '$USER_ID' ORDER BY id;"
   # Kullanilan kodun used_at dolu, kalanlar NULL olmali
   ```

### AC-5: API Key IP Whitelist

1. Settings → API Keys → Yeni key olustur, "Allowed IPs" alanina `192.168.1.0/24` gir
2. Bu anahtarla farkli IP'den istek yap (VPN ya da X-Forwarded-For ile):
   ```bash
   curl -H "X-API-Key: <KEY>" -H "X-Forwarded-For: 10.0.0.1" http://localhost:8084/api/v1/sims
   # 403 API_KEY_IP_NOT_ALLOWED donmeli
   ```
3. Izin verilen IP araliginden istek yap:
   ```bash
   curl -H "X-API-Key: <KEY>" -H "X-Forwarded-For: 192.168.1.55" http://localhost:8084/api/v1/sims
   # 200 donmeli
   ```
4. Bos IP listesi ile olusturulan key -- herhangi bir IP'den calismali (geri uyumluluk)
5. Gecersiz CIDR girisi -- 422 `VALIDATION_ERROR` donmeli

### AC-6: Oturum Iptal (Session Revoke)

1. Bir kullanici ile birden fazla cihazdan giris yap (birden fazla refresh token olustur)
2. Admin olarak tum oturumlarini iptal et:
   ```bash
   curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
     http://localhost:8084/api/v1/users/$USER_ID/revoke-sessions
   # 200 donmeli
   ```
3. Iptal edilen kullanicinin refresh tokeni ile yenileme dene:
   ```bash
   curl -X POST http://localhost:8084/api/v1/auth/refresh -b "refresh_token=<TOKEN>"
   # 401 INVALID_REFRESH_TOKEN donmeli
   ```
4. Active Sessions sayfasinda (`/auth/sessions` veya settings) oturumlar temizlenmeli
5. `?include_api_keys=true` ile oturumla birlikte API key'leri de iptal et:
   ```bash
   curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
     "http://localhost:8084/api/v1/users/$USER_ID/revoke-sessions?include_api_keys=true"
   ```
6. Denetim logu olusturuldugunu kontrol et: `GET /api/v1/audit` filtreleyerek `session.revoke` aksiyonu gorunmeli

### AC-7: Toplu Oturum Sonlandirma (Super Admin)

1. Super admin tokenini kullan:
   ```bash
   curl -X POST -H "Authorization: Bearer $SUPERADMIN_TOKEN" \
     "http://localhost:8084/api/v1/system/revoke-all-sessions?tenant=$TENANT_ID"
   # 200 donmeli; etkilenen kullanici sayisi response'da olmali
   ```
2. Tenant_admin ile ayni endpoint'i farkli tenant icin dene -- 403 donmeli
3. Denetim logu: `system.revoke_all_sessions` aksiyonu gorulmeli
4. Bildirim konfigurasyonu varsa etkilenen kullanicilara email gitmeli

### AC-8: Kaynak Limiti Zorunlulugu

1. Tenant limit ayarini kucult:
   ```bash
   psql ... -c "UPDATE tenants SET max_users = 2 WHERE id = '$TENANT_ID';"
   ```
2. 2 kullanici zaten varken yeni kullanici olusturmaya calis:
   ```bash
   curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/users \
     -d '{"email":"extra@example.com","name":"Extra","role":"analyst","password":"ValidLongPass1!"}' \
     -H "Content-Type: application/json"
   # 422 TENANT_LIMIT_EXCEEDED donmeli; details icinde resource=users, current=2, max=2
   ```
3. max_api_keys siniri icin ayni testi API key ile tekrarla
4. Limit 0 ise -- sinirsiz kabul edilmeli (geri uyumluluk)

### AC-9: Denetim Loglama (13 Endpoint)

1. Su islemleri gerceklestir ve ardinda audit log kontrol et (`GET /api/v1/audit`):
   - `POST /api/v1/cdrs/export` -- `cdr.export` kaydi gorunmeli
   - `POST /api/v1/compliance/erasure/:sim_id` -- `compliance.erasure` kaydi gorunmeli
   - `POST /api/v1/msisdn-pool/import` -- `msisdn.import` kaydi gorunmeli
   - `POST /api/v1/msisdn-pool/:id/assign` -- `msisdn.assign` kaydi gorunmeli
   - `PATCH /api/v1/analytics/anomalies/:id` -- `anomaly.update` kaydi gorunmeli
   - `PUT /api/v1/compliance/retention` -- `compliance.retention_update` kaydi gorunmeli
   - `POST /api/v1/jobs/:id/cancel` -- `job.cancel` kaydi gorunmeli
   - `POST /api/v1/users/:id/revoke-sessions` -- `session.revoke` kaydi gorunmeli
   - `POST /api/v1/system/revoke-all-sessions` -- `system.revoke_all_sessions` kaydi gorunmeli

### AC-10: Hesap Kilitleme / Acma

1. Yanlis sifre ile 5 kez giris yap:
   ```bash
   for i in {1..6}; do
     curl -X POST http://localhost:8084/api/v1/auth/login \
       -d '{"email":"testuser@example.com","password":"wrongpassword"}' \
       -H "Content-Type: application/json"
   done
   # 6. istekte 403 ACCOUNT_LOCKED donmeli; details.retry_after_seconds > 0
   ```
2. Doğru sifre ile deneme -- hala kilitli oldugu icin 403 donmeli
3. Admin ile manuel kilit ac:
   ```bash
   curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
     http://localhost:8084/api/v1/users/$USER_ID/unlock
   # 200 donmeli
   ```
4. Simdi dogru sifre ile giris -- basarili olmali
5. DB dogrulamasi: `psql ... -c "SELECT failed_login_count, locked_until FROM users WHERE id = '$USER_ID';"` -- `0` ve `NULL` olmali
6. Denetim: `GET /api/v1/audit` -- `user.lock` ve `user.unlock` kayitlari gorunmeli

### UI Sayfasi Kontrolleri

1. **Change Password sayfasi** (`/auth/change-password`):
   - Mevcut + yeni + yeni (tekrar) alanlar var olmali
   - Sifre guc gostergesi calismali
   - Basarili degisimde dashboard'a yonlendirilmeli
2. **Active Sessions sayfasi** (`/settings/security` > Sessions sekmesi):
   - Aktif oturumlarin listesi gorunmeli (cihaz, IP, tarih)
   - "Revoke All" butonu oturumlarini iptal etmeli
3. **2FA Backup Codes sayfasi** (`/settings/security` > 2FA sekmesi):
   - Kalan backup kod sayisi gorunmeli
   - "Regenerate" butonu onay dialog'u ile calismali
4. **API Keys sayfasi** (`/settings/api-keys`):
   - Key olusturma formunda "Allowed IPs" CIDR alani mevcut olmali
   - Gecersiz CIDR girisinde client-side hata mesaji gorunmeli

### Temel Testler

```bash
make test  # 2329 test gecmeli
go build ./...  # Derleme hatasi olmamali
```

## STORY-069: Onboarding, Reporting & Notification Completeness

### Backend / Altyapi (12 senaryo)

1. **AC-1 Onboarding session start**: `POST /api/v1/onboarding/start` ile tenant context gonderildiginde 201 Created donmeli; response `{session_id, current_step:1, steps_total:5}` icermeli.
2. **AC-1 Onboarding step gonderimi**: `POST /api/v1/onboarding/:id/step/1` step1 payload (company_name, locale) ile cagrildiginda 200 OK donmeli ve `current_step` 2'ye yukselmeli.
3. **AC-1 Onboarding session resume**: `GET /api/v1/onboarding/:id` mevcut session'in `current_step` ve `data_by_step` haritasini hidrate etmeli.
4. **AC-2 Reports on-demand**: `POST /api/v1/reports/generate {report_type:"compliance_kvkk", format:"pdf"}` 202 ile `{job_id, status:"queued"}` donmeli; jobs sayfasinda gorulmeli.
5. **AC-3 Scheduled report CRUD**: `POST /api/v1/reports/scheduled` ile yeni schedule olusturma; `GET /api/v1/reports/scheduled` listede gormeli; `PATCH .../scheduled/:id {state:"paused"}` durumu degistirmeli.
6. **AC-3 Scheduled report sweeper**: 1dk icinde `next_run_at <= now()` olan satir icin yeni `scheduled_report_run` job'i olusturulmali; `last_run_at` ilerlemeli.
7. **AC-5 Webhook config + delivery**: `POST /api/v1/webhooks {url, secret, event_types}` (https zorunlu); ilgili event tetiklendiginde `webhook_deliveries` satiri olusmali; `X-Argus-Signature` HMAC dogrulamali.
8. **AC-6 Webhook retry + dead letter**: 5xx alan webhook 4 retry sonrasi `dead_letter` state'e gecmeli; `webhook.dead_letter` notification yayinlanmali; `argus_webhook_retries_total{result="dead_letter"}` artmali.
9. **AC-7/AC-8 Preferences + templates**: Notifications sayfasinin Preferences sekmesinde `anomaly.detected` icin webhook checkbox'i kaldirildiginda dispatcher webhook gondermemeli; Templates sekmesinde `tr` template'i kaydedildiginde Turkce subject mailde gozukmeli.
10. **AC-9 Data portability**: `POST /api/v1/compliance/data-portability/:user_id` 202 donmeli; `data_portability_export` job calistiginda S3'e zip yuklenmeli (yapilandirilmissa) ve `data_portability_ready` notification gitmeli.
11. **AC-10 KVKK auto-purge dry run**: `kvkk_purge_daily` job'a `payload.dry_run=true` ile el ile job baslattiginda satirlar mutate edilmeden `{would_purge}` raporlanmali.
12. **AC-12 SMS rate limit**: 1 dakikada 60 SMS basariyla gondermeli; 61. SMS 429 + `RATE_LIMITED` error code donmeli.

### Frontend (8 senaryo)

13. **Onboarding wizard resume**: Wizard'in 3. adimindayken sayfayi yenile → wizard ayni adimda acilmali, daha onceki adim verileri server tarafinda saklanmis olmali (localStorage `argus_onboarding_session` kullanilir).
14. **Reports — generate**: Reports sayfasinda bir karta tikla → Generate Report panelinde format sec → Generate → toast "Report queued (job xxx)" gostermeli; Jobs sayfasinda yeni job gorulmeli.
15. **Reports — scheduled**: Sayfanin altinda scheduled tablo gorunmeli; bir satirin Pause/Play butonu state degistirmeli; Trash butonu satiri silmeli.
16. **Webhooks page**: `/webhooks` sayfasi acilmali; New Webhook dialog ile https URL + secret + event_types ile webhook olusturulmali; secret bir kez gosterilmeli; satirin "Deliveries" butonu son 20 delivery'yi acmali; her delivery'nin "Retry" butonu 200 donmeli.
17. **Notification preferences matrix**: `/notifications` Preferences sekmesi event_types x channels checkbox matrix gostermeli; toggle yapildiginda "Save" butonu aktiflesmeli; Save sonrasi sayfa yenilenince state korunmali.
18. **Notification templates**: Templates sekmesinde event_type+locale secince mevcut template hidrate olmali; Subject + Body Text + Body HTML duzenlenip Save edilebilmeli; Turkce karakterler bozulmamali (`G`, `S`, `c`, `o`, `u` korunmali).
19. **SMS gateway**: `/sms` sayfasinda SIM ID + 480 karakter altinda mesaj + priority sec → Send SMS → toast "SMS queued"; SMS History tablosunda satir gozukmeli; status badge'i `queued` olmali; sonra `sent` olarak guncellenmeli.
20. **Data portability page**: `/compliance/data-portability` sayfasinda User ID gir → Request Export → Job ID gosteren success card cikmali; tenant_admin olmayan kullanici farklinin ID'sini istediginde 403 alirmali.

### Operations
21. **Cron schedules**: `make up` sonrasi log'larda 4 yeni cron entry mesaji olmali: `kvkk_purge_daily @daily`, `ip_grace_release @hourly`, `webhook_retry_sweep */1 * * * *`, `scheduled_report_sweeper */1 * * * *`.

### Test command
```bash
make test  # tum testler gecmeli
go build ./...  # Derleme hatasi olmamali
cd web && npm run build  # Frontend build basarili olmali
```

## STORY-070: Frontend Real-Data Wiring

### Backend / Altyapi (8 senaryo)

1. **AC-9 Violation acknowledgment**: `POST /api/v1/policy-violations/:id/acknowledge {note:"resolved"}` 200 OK donmeli; `policy_violations` satiri `acknowledged_at`, `acknowledged_by`, `acknowledged_note` dolu olmali. Audit log `action=violation.acknowledge` kaydi olmali. Ayni ID ile ikinci istek 409 Conflict + `ALREADY_ACKNOWLEDGED` donmeli. Yanlış ID 404 + `VIOLATION_NOT_FOUND` donmeli.
2. **AC-3 APN traffic**: `GET /api/v1/apns/:id/traffic?period=24h` APN icin hourly traffic bucket'lari (`bytes_in`, `bytes_out`) dolu donmeli. Bos donemde `[]` degil `data:[]` response envelope donmeli.
3. **AC-5 Operator metrics**: `GET /api/v1/operators/:id/metrics` metrikleri (`auth_rate`, `latency_p95`, `bytes`) hourly bucket'larla donmeli. `GET /api/v1/operators/:id/health-history` son N sonucu cursor-paginated donmeli.
4. **AC-4 APN list enrichment**: `GET /api/v1/apns` response'inda her APN objesinde `sim_count`, `traffic_24h_bytes`, `pool_used`, `pool_total` alanlari dolu olmali (sifir dahi olsa).
5. **AC-6 Capacity endpoint**: `GET /api/v1/system/capacity` (super_admin) `{sim_capacity, session_capacity, auth_per_sec, monthly_growth, current_sims, current_sessions}` donmeli. `ARGUS_CAPACITY_SIM` env yokken default `15000000` kullanilmali.
6. **AC-8 Report definitions**: `GET /api/v1/reports/definitions` 8 tanim donmeli; her tanim `{id, label, description, formats[]}` alanlarina sahip olmali.
7. **AC-1 Dashboard heatmap**: `GET /api/v1/dashboard/summary` response `traffic_heatmap` alanini icermeli (168 eleman array, hour×weekday). WS `dashboard.realtime` event envelope'unun `id` alani UUID donmeli.
8. **AC-7 SLA metrics**: `GET /api/v1/sla-reports` satirlari `uptime_pct`, `avg_latency_ms`, `incident_count` alanlari ile donmeli; `uptime_pct < target` olan satir SLA violation sayisi olarak sayilmali.

### Frontend (9 senaryo)

9. **AC-3 APN detail traffic**: `/apns/:id` sayfasini ac → Traffic sekmesinde grafik yuklemeli (spinner sonra chart); grafik degerlerinde `NaN` veya `0.00` olmamali (gercek CDR varsa). Network sekmesinde `/apns/:id/traffic` cagrisi olmali.
10. **AC-4 APN list stats**: `/apns` listesi: SIM Count, Traffic 24h, Pool Used/Total sutunlari gercek veri gostermeli; mock `---` placeholder'lar olmamali.
11. **AC-5 Operator detail**: `/operators/:id` sayfasinda Health History tablosunda gercek satir gorulmeli; Metrics sekmesinde gercek latency/auth-rate grafigi yuklemeli.
12. **AC-6 Capacity**: `/capacity` sayfasinda Progress bar'larin percentage degerleri `Math.random` varyasyonu gostermemeli; sayfayi yenileyince degerler degismemeli.
13. **AC-9 Violations DropdownMenu**: `/violations` sayfasinda her satirda uc nokta menu acilmali; "Dismiss" secilince `POST .../acknowledge` cagrisi olmali; basariliysa satir `acknowledged` filter altina tasinmali.
14. **AC-11 URL filter persistence**: `/sims?state=active` URL'ine git → state filter secili gelmeli; geri/ileri navigasyon filter'i korumali. `/apns?search=iot`, `/sessions?state=active`, `/jobs?type=bulk_sim_import`, `/audit?action=violation.acknowledge`, `/violations?acknowledged=false`, `/esim?operator_id=xxx` hepsinde ayni davranis olmali.
15. **AC-12 SIM reserve IPs error**: SIM listesinde birden fazla SIM sec → "Reserve IPs" butonu → hata durumunda bulk toast `"N succeeded, M failed"` gostermeli.
16. **AC-13 WS indicator**: Topbar'da WS durum rozeti gorulmeli; sunucu WebSocket portuna erisim kesilince rozet `disconnected` gostermeli; yeniden baglaninca `connected` donmeli.
17. **AC-14 Dead code**: `web/src/pages/placeholder.tsx` dosyasi mevcut olmamali; `grep -r "Math.random" web/src` sifir sonuc vermeli.

### Operations

18. **Yeni envler**: `ARGUS_CAPACITY_SIM` env set edilmeden calistirildiginda `/system/capacity` default `15000000` dondurulmeli; env set edildiginde (`ARGUS_CAPACITY_SIM=20000000`) yeni deger yansiyor olmali.
19. **Migration reversibility**: `migrate -path migrations down 1` komutu `20260413000003_violation_acknowledgment.down.sql` calismali; `acknowledged_at/by/note` sutunlari ve partial index kalkmali.

### Test command
```bash
make test   # 2576 test gecmeli
go build ./...  # Derleme hatasi olmamali
cd web && npm run build  # Frontend build basarili olmali (Vite ~4s)
npx tsc --noEmit  # TypeScript hata olmamali
```

---

## STORY-071: Roaming Agreement Management

### Backend / Altyapi (10 senaryo)

1. **Migration**: `psql` ile `\d roaming_agreements` → tum alanlar (id, tenant_id, operator_id, partner_operator_name, agreement_type, sla_terms, cost_terms, start_date, end_date, auto_renew, state, notes, terminated_at, created_by, created_at, updated_at) ve CHECK constraint'leri gorulmeli. `\di roaming_agreements*` ile `idx_roaming_agreements_active_unique` partial index ve `idx_roaming_agreements_expiry` index gorulmeli.
2. **Anlaşma oluşturma**: `POST /api/v1/roaming-agreements` (`operator_manager` token ile) gecerli body → 201 Created + `{status:"success", data:{id,...}}` donmeli. `api_user` token ile ayni istek → 403 Forbidden donmeli.
3. **Tekil aktif zorunluluğu**: Ayni `tenant_id + operator_id` icin ikinci `active` anlaşma olusturma denemesi → 409 Conflict + `roaming_agreement_overlap` hata kodu donmeli.
4. **Tarih dogrulamasi**: `start_date >= end_date` olan body → 422 Unprocessable + `roaming_agreement_invalid_dates` donmeli.
5. **Operator grant kontrolü**: Grant edilmemiş `operator_id` ile liste cekilmesi → 403 + `roaming_agreement_operator_not_granted` donmeli.
6. **Fesih (terminate)**: `DELETE /api/v1/roaming-agreements/:id` → state `terminated` olmali, `terminated_at` set olmali. Tekrar DELETE → 409 (terminated anlaşma tekrar feshedilemez). Terminated anlaşmaya PATCH denemesi → 409 state guard.
7. **SoR entegrasyonu**: Aktif anlaşması olan bir operator icin `SoR.Evaluate()` cagrisinda `decision.CostPerMB` anlaşmanin `cost_terms.cost_per_mb` ile override edilmeli, `decision.AgreementID` set olmali. Provider wired degilken (nil) SoR normal seyrinde devam etmeli.
8. **Renewal cron**: `ROAMING_RENEWAL_ALERT_DAYS=30` env ayarliyken, `end_date` 30 gun icerisinde olan aktif anlaşma icin cron calişinca `bus.SubjectAlertTriggered` konusuna `AlertPayload` publish edilmeli. Redis'te `argus:dedup:roaming_renewal:{agreement_id}:{YYYY-MM}` anahtari olusturulmali (TTL ~35 gun). Ayni anlaşma icin ayni ay icinde ikinci cron cagrisi duplicate alert gondermemeli.
9. **Audit log**: Create/Update/Terminate islemlerinde `audit_logs` tablosunda `action` = `roaming_agreement.create` / `.update` / `.terminate` satirlari olmali.
10. **Migration reversibility**: `migrate down 1` → `20260414000001_roaming_agreements.down.sql` calismali; tablo, indexler ve RLS policy kalkmali.

### Frontend (7 senaryo)

11. **Liste sayfasi (SCR-150)**: `/roaming-agreements` sayfasini ac → anlaşma yoksa empty state (Handshake ikonu + aciklama) gorulmeli. Anlaşma varsa tablo satirlari `partner_operator_name`, `agreement_type` badge, `state` badge, `start_date`, `end_date` sutunlariyla gorulmeli. Satira tiklayinca `/roaming-agreements/:id` sayfasina yonlendirmeli.
12. **Yeni anlaşma**: `operator_manager` rolundeyken "New Agreement" butonu → slide panel acilmali; form doldurulup submit edilince liste yenilenmeli. `api_user` rolundeyken buton gorulmemeli veya disabled olmali.
13. **Detay sayfasi (SCR-151)**: `/roaming-agreements/:id` → SLA Terms (uptime, latency p95, max incidents), Cost Terms (rate, currency), gecerlilik suresi progress bar, auto_renew checkbox, notes textarea gorulmeli. Gecerlilik bar `start_date` ile `end_date` arasindaki yuzdeyi gostermeli.
14. **Guncelleme**: Detay sayfasinda `operator_manager` rolundeyken notes veya auto_renew degistirip kaydetmek → `PATCH` istegi atilmali; toast success mesaji gorulmeli.
15. **Fesih**: Detay sayfasinda "Terminate" butonu → onay dialogi acilmali; onay verilince `DELETE` istegi atilmali; state badge `terminated` guncellemeli.
16. **Operator detay tab**: `/operators/:id` sayfasinda `Agreements` sekmesi → o operatora ait anlaşmalar mini-listesi gorulmeli. "New Agreement" butonu bu sayfadan da slide panel acmali.
17. **Sidebar**: Sol kenar cubugunda OPERATIONS altinda "Roaming" menu ogesinin (Handshake ikonu) gorulmesi ve `/roaming-agreements` rotasina yonlendirmesi dogrulanmali.

### Operations

18. **Env vars**: `ROAMING_RENEWAL_ALERT_DAYS=7` set edilip cron el ile tetiklendiginde, `end_date` 7 gun icerisinde olan anlaşmalar icin alert publish edilmeli (30 gun uzerindekiler skip edilmeli).
19. **Cron kapsamı**: `ROAMING_RENEWAL_CRON="*/5 * * * *"` (5 dakikada bir) set edilip argus yeniden baslatildiginda cron tablosunda `roaming_renewal_sweep` caydirici sikligi gozlemlenmeli.

### Test command
```bash
make test   # 2651 test gecmeli
go build ./...  # Derleme hatasi olmamali
cd web && npm run build  # Frontend build basarili olmali
npx tsc --noEmit  # TypeScript hata olmamali
```

---

## STORY-072: Enterprise Observability Screens

### Backend / Altyapi (8 senaryo)

1. **Ops Snapshot (API-236)**: `super_admin` JWT ile `GET /api/v1/ops/metrics/snapshot` → `{status:"success", data:{http_p50, http_p95, http_p99, aaa_auth_rate, active_sessions, error_rate, memory_bytes, goroutines}}` dönmeli. `tenant_admin` JWT ile → 403 Forbidden dönmeli.
2. **Snapshot cache**: 5 saniye içinde iki kez `GET /api/v1/ops/metrics/snapshot` → ikinci yanıt birinciyle identik `data` dönmeli (aynı timestamp; cache hit). 6 saniye bekleyip tekrar → farklı değerler (cache miss).
3. **Infra Health (API-237)**: `GET /api/v1/ops/infra-health` → `{db:{open_conns, idle_conns}, nats:{stream_bytes, consumers, pending, consumer_lag:[...]}, redis:{memory_used, hit_ratio}}` dönmeli. Redis bölümü `redisCachedAt.IsZero()` durumunda bile boş struct döndürmemeli (ilk çağrı cache miss → gerçek Redis sorgusu).
4. **Infra Health — NATS consumer lag**: `nats.consumer_lag` listesinin en az 1 entry içermesi için NATS'te aktif bir consumer'ın bulunması gerekir; `go test ./internal/api/ops/...` → `TestInfraHealth_NATSConsumerLag` geçmeli.
5. **Incidents (API-238)**: `GET /api/v1/ops/incidents` → anomalies + audit_logs merged liste dönmeli; `source` alanı `"anomaly"` veya `"audit"`, `severity` alanı mevcut; satırlar severity DESC + created_at DESC sırasında olmali. 200 satır limiti aşılırsa LIMIT kesilmeli.
6. **Anomaly Comments (API-239/240)**: `POST /api/v1/analytics/anomalies/{id}/comments` body `{"body":"test comment"}` → 201 Created + `{status:"success", data:{id, body, author_email, created_at}}` dönmeli. `GET .../comments` → listedeki ilk satır en yeni yorum olmali (created_at DESC). 2001 karakter body → 422 dönmeli.
7. **Anomaly Escalate (API-241)**: `POST /api/v1/analytics/anomalies/{id}/escalate` body `{"note":"urgent"}` → 200 + anomaly `state:"escalated"` dönmeli; `GET .../comments` listesinde escalation note'u içeren yorum görülmeli. `note` boş gönderilirse yorum satırı oluşturulmamalı.
8. **Migration reversibility**: `migrate -path migrations down 1` → `20260415000001_anomaly_comments.down.sql` çalışmalı; `anomaly_comments` tablosu ve RLS policy kalkmalı.

### Frontend (6 senaryo)

9. **Sidebar OPERATIONS grubu**: Giriş yapıldığında sol sidebar'da `OPERATIONS — SRE` başlığı altında 8 menü ögesi görülmeli: Performance, Errors, AAA Traffic, Infrastructure, Job Queue, Backup, Deploys, Incidents. `tenant_admin` rolündeyken bu grup görünmemeli (minRole: super_admin).
10. **SCR-160 Performance (SCR-130 alias)**: `/ops/performance` → HTTP p50/p95/p99 sparkline'ları ve AAA auth rate görülmeli; 15 saniyede bir otomatik yenilenmeli. WebSocket `metrics.realtime` eventi geldiğinde sparkline'lar aralarındaki interval beklemeksizin güncellenmeli (AAA Traffic sayfasında da aynı davranış).
11. **SCR-163/164/165 Infra sekmeleri**: `/ops/infra` → NATS / DB / Redis sekmeleri; her sekme ilgili `infra-health` bölümünü göstermeli. Redis sekmesindeki `hit_ratio` değeri `%` ile formatlanmali.
12. **SCR-169 Incidents timeline**: `/ops/incidents` → olaylar severity badgeleri (critical/high/medium/low) ve `source` ikonu (anomaly vs audit) ile listelenmeli; severity DESC sıralı görünmeli. Sayfa boşsa "No incidents" empty state görülmeli.
13. **Alert ack/resolve/escalate UX (AC-11)**: `/alerts` → bir uyarı satırına tıkla → Acknowledge, Resolve, Escalate butonları görülmeli. Acknowledge dialog'u → not gir → submit → uyarı listesi güncellenmeli; not girildiğinde anomaly comment olarak kaydedilmeli (API-239/240 ile doğrulanabilir). Escalate → state "escalated" olmalı.
14. **WS indicator (AC-12)**: `/ops/performance` ekranında topbar WS rozeti yeşil/sarı/kırmızı durumda görülmeli; rozete tıklanınca yeniden bağlantı denemesi başlatılmalı (click-to-reconnect).

### Test command
```bash
make test   # 2682 test gecmeli
go build ./...  # Derleme hatasi olmamali
cd web && npm run build  # Frontend build basarili olmali (~3.8s)
npx tsc --noEmit  # TypeScript hata olmamali
```

---

## STORY-073: Multi-Tenant Admin & Compliance Screens

### Backend / Altyapi (7 senaryo)

1. **Kill switch LIST (API-248)**: `super_admin` JWT ile `GET /api/v1/admin/kill-switches` → 5 switch gelmeli; her birinde `key`, `enabled`, `reason`, `toggled_by`, `toggled_at` alanlari olmali. `tenant_admin` JWT ile → 403 Forbidden.
2. **Kill switch TOGGLE (API-249)**: `PATCH /api/v1/admin/kill-switches/bulk_ops` body `{"enabled": true, "reason": "test disable"}` → 200; `enabled: true` dönmeli. Ardından bulk SIM suspend endpoint'i çağırılınca → 503 `SERVICE_DEGRADED` dönmeli. Tekrar `{"enabled": false}` ile toggle → bulk operasyon normal çalışmalı.
3. **Maintenance window CREATE/DELETE (API-251/252)**: `POST /admin/maintenance-windows` → 201 Created; `GET /admin/maintenance-windows` → yeni kayıt listede görülmeli. `DELETE /admin/maintenance-windows/:id` → 204; kayıt listeden düşmeli. Her iki işlem için `audit_logs` tablosunda `action = maintenance.scheduled / maintenance.cancelled` satırları olmali.
4. **Global sessions (API-245)**: `GET /admin/sessions/active` → aktif portal session listesi; `user_email`, `ip`, `browser`, `os`, `last_seen_at` alanları mevcut. `POST /admin/sessions/:id/revoke` → 200; revoke edilen session'a ait token ile herhangi bir endpoint çağrısı → 401.
5. **DSAR queue (API-255)**: `GET /admin/dsar/queue` (tenant_admin) → kendi tenant'ına ait data-portability ve kvkk-purge tipli job'lar filtrelenmiş gelecek; `sla_hours`, `sla_remaining_hours`, `subject_id` alanları mevcut.
6. **Delivery status (API-253)**: `GET /admin/delivery/status` → 5 kanal için `{channel, success_rate, failure_rate, retry_depth, p50_ms, p95_ms, p99_ms, last_delivery_at}` dönmeli. Son 30 dakikada başarılı webhook bildirimi gönderilmişse webhook kanalının `success_rate > 0` olması beklenir.
7. **Migration reversibility**: `migrate -path migrations down 1` → `20260416000001_admin_compliance.down.sql` çalışmalı; `kill_switches` ve `maintenance_windows` tabloları ve RLS policy kalkmalı.

### Frontend (11 senaryo)

8. **Sidebar ADMIN grubu (AC-13)**: `super_admin` olarak giriş → sol sidebar'da ADMIN başlığı altında tüm 12 admin ekranı için link görülmeli. `tenant_admin` olarak giriş → yalnızca izin verilen ekranlar (Quotas, Security Events, Global Sessions, DSAR Queue, Compliance Overview) görülmeli.
9. **SCR-140 Tenant Resources**: `/admin/resources` → her tenant için SIM count, API RPS, active sessions, CDR volume, storage kart grubu görülmeli. Herhangi bir sütun başlığına tıklayınca sıralama değişmeli.
10. **SCR-141 Quota Breakdown**: `/admin/quotas` → her tenant için max_sims / current_sims progress bar; 95% üzerinde kırmızı (danger), 80-95% arası sarı (warning), altı yeşil (ok) renk görülmeli. Limit yaklaşan tenant için banner uyarısı görülmeli.
11. **SCR-143 Security Events**: `/admin/security-events` → audit log'dan auth_failure, role_change, account_locked gibi olaylar listelenm  eli; severity badge'leri görülmeli; tenant/event type filtreleri çalışmalı.
12. **SCR-144 Global Sessions**: `/admin/sessions` → aktif portal oturumları listelenmeli; "Force Logout" butonuna tıklanınca onay dialogi çıkmalı; onay sonrasında session revoke edilmeli.
13. **SCR-145 API Key Usage**: `/admin/api-usage` → her API key için rate limit bar, error rate, anomaly flag görülmeli; anomaly_flag=true olan key kırmızı highlight almalı.
14. **SCR-146 DSAR Queue**: `/admin/dsar` → SLA timer (sla_remaining_hours) geri sayım göstermeli; SLA süresi dolmuş request kırmızı badge almalı; "Generate Response" butonu ilgili job'ı tetiklemeli.
15. **SCR-149 Kill Switches**: `/admin/kill-switches` → 5 switch toggle ile görülmeli; enable etmek için slide panel açılmalı, reason zorunlu alan olmalı; reason girilmeden submit → validasyon hatası görülmeli.
16. **SCR-152 Maintenance Windows**: `/admin/maintenance` → pencere listesi ve "Schedule Window" butonu görülmeli; form doldurulup submit edilince liste yenilenmeli; Cancel butonu pencereyi listeden kaldırmalı.
17. **SCR-153 Delivery Status**: `/admin/delivery` → 5 kanal için health card (webhook/email/sms/in-app/telegram); p50/p95/p99 değerleri görülmeli; kanal sağlığı yeşil/sarı/kırmızı göstergesiyle belirtilmeli.
18. **SCR-147 Compliance Posture**: `/admin/compliance` → 6 posture card görülmeli (read-only mode, external notifications, quota utilization, audit trail, retention, KVKK/GDPR controls); her kart ok/warning/critical badge taşımalı.

### Test command
```bash
make test   # 2693 test gecmeli
go build ./...  # Derleme hatasi olmamali
cd web && npm run build  # Frontend build basarili olmali
npx tsc --noEmit  # TypeScript hata olmamali
```

---

## STORY-075: Cross-Entity Context & Detail Page Completeness

### Backend / Altyapi (5 senaryo)

1. **Session detail (API-256)**: `sim_manager` JWT ile `GET /api/v1/sessions/{id}` → 200; `sim_id`, `operator_id`, `apn_id` ile birlikte enriched DTO dönmeli (`sim.iccid`, `operator.name`, `apn.name` alanları mevcut). Farklı tenant'a ait session id ile istek → 404 (existence leak önlemi).
2. **User detail + activity (API-257/258)**: `tenant_admin` JWT ile `GET /api/v1/users/{id}` → 200; `email`, `role`, `state`, `totp_enabled`, `last_login_at`, `locked_until` alanları mevcut. `GET /api/v1/users/{id}/activity` → cursor-paginated audit log listesi; her satırda `action`, `entity_type`, `entity_id`, `created_at` alanları mevcut. Farklı tenant user id'si → 404.
3. **Violation detail (API-259)**: `GET /api/v1/policy-violations/{id}` → 200; violation satırı + enriched SIM/policy context. Farklı tenant → 404.
4. **Violation remediate (API-260)**: `POST /api/v1/policy-violations/{id}/remediate` body `{"action":"dismiss"}` → 200; `audit_logs` tablosunda `action = violation.dismissed` satırı oluşmali. `{"action":"suspend_sim"}` ile aktif olmayan SIM'e remediate → 409 (geçersiz state transition). `{"action":"escalate"}` → 200; violation state `escalated` olmalı. Geçersiz action değeri → 400.
5. **Tenant RLS**: Tüm 5 yeni endpoint'te farklı tenant'a ait entity_id kullanılınca → 404 (403 değil, existence leak önlemi). `super_admin` JWT ile `GET /api/v1/system/tenants/{id}` → 200; `sim_count`, `session_count`, `user_count` stats alanları mevcut.

### Frontend (11 senaryo)

6. **EntityLink bileşeni**: Audit Log sayfasında (`/audit`) `entity_id` sütunundaki değere tıklanınca ilgili entity'nin detail sayfasına yönlendirilmeli (ör. SIM entity_type → `/sims/{id}`). Actor sütunundaki user ID de EntityLink ile render edilmeli.
7. **CopyableId bileşeni**: Herhangi bir detail sayfasında ID alanı üzerine gelinince kopyalama ikonu görülmeli; tıklanınca panoya kopyalanmalı ve 2 saniye boyunca checkmark gösterilmeli. ID maskeli (ilk 8 karakter) gösterilmeli; hover ile tam değer açılmalı.
8. **SCR-170 Session Detail**: `/sessions/{id}` → SoR, Policy, Quota, Audit, Alerts tabları görülmeli. Force-Disconnect butonuna tıklanınca onay dialogu açılmalı; onay sonrası endpoint çağrılmalı.
9. **SCR-171 User Detail**: `/settings/users/{id}` → Overview, Activity, Sessions, Permissions, Notifications tabları görülmeli. Activity tabında audit satırları EntityLink ile gösterilmeli. "Unlock Account" butonu kilitli kullanıcı için aktif olmalı; tıklanınca unlock endpoint çağrılmalı.
10. **SCR-172 Alert Detail**: `/alerts/{id}` → Overview, Similar, Audit tabları görülmeli. "Acknowledge" butonuna tıklanınca dialog açılmalı; onay sonrası alert state güncellenmeli. Similar tabında aynı entity_type'tan benzer alert'ler listelenmeli.
11. **SCR-173 Violation Detail**: `/violations/{id}` → Overview, Audit tabları görülmeli. "Suspend SIM" aksiyonu seçilip onaylanınca `remediate` endpoint'i çağrılmalı; action başarısız olursa (409 geçersiz state) hata toast gösterilmeli. "Dismiss" ve "Escalate" de aynı şekilde çalışmalı.
12. **SCR-174 Tenant Detail**: `/system/tenants/{id}` → Yalnızca `super_admin` rolü erişebilmeli; `tenant_admin` ile erişim → 403/redirect. Stats kartlarında AnimatedCounter ile canlı sayım animasyonu görülmeli. Overview, Audit, Alerts tabları mevcut.
13. **SIM detail zenginleştirme**: `/sims/{id}` → Policy History, IP History, Cost Attribution ve Related Data tabları görülmeli. RelatedAuditTab, RelatedNotificationsPanel, RelatedAlertsPanel bileşenleri yüklenmeli; boş listede empty state göstermeli; skeleton loader yükleme sırasında görünmeli.
14. **APN/Operator/Policy zenginleştirme**: `/apns/{id}` → Audit, Notifications, Alerts tabları görülmeli. `/operators/{id}` → SIMs tab'ında paginated SIM listesi + EntityLink ile SIM'lere link verilmeli. `/policies/{id}` → Violations tabı + Assigned SIMs tabı + Clone butonu + Export butonu görülmeli.
15. **RelatedXxx bileşenleri yükleme durumları**: Related data yüklenirken skeleton gösterilmeli; boş listedeki empty state mesajı görülmeli; API hatası durumunda error fallback banner görülmeli.
16. **Audit tabı JSON diff**: RelatedAuditTab'da değişiklik içeren bir audit satırı expand edilince `before` ve `after` JSON diff görünmeli; altında "View in Audit Log" footer linki ile `/audit?entity_id={id}` sayfasına yönlendirilmeli.

### Test command
```bash
make test   # 2675 test gecmeli
go build ./...  # Derleme hatasi olmamali
cd web && npm run build  # Frontend build basarili olmali (~3.8s)
npx tsc --noEmit  # TypeScript hata olmamali
```

---

## STORY-076: Universal Search, Navigation & Clipboard

### Backend / Altyapi (3 senaryo)

1. **Universal Search endpoint (API-261)**: `api_user` JWT ile `GET /api/v1/search?q=89012&types=sim,apn,operator,policy,user&limit=5` → 200; gruplu sonuç `[{type, id, label, sub}, ...]` dönmeli. Her sonuç `tenant_id` ile scope edilmiş olmalı. `q` boş olunca → 400 `VALIDATION_ERROR`. `limit=100` ile istek → `limit=20` ile cevap dönmeli (cap zorlama). Farklı tenant JWT ile aynı `q` → sadece o tenant'a ait sonuçlar gelmeli.
2. **Paralel sorgu + timeout**: 5 entity tipi için errgroup.Group ile paralel DB sorgusu çalışmalı; 500ms context timeout içinde cevap gelmeli. Çok yavaş DB simülasyonunda (test ortamında değil, gözlem yolu ile) timeout aşılınca handler 500/504 dönmeli.
3. **Rate limiting**: Gateway middleware rate limit yapılandırması geçerli olmalı; ardışık çok sayıda istek → 429 `TOO_MANY_REQUESTS` dönmeli.

### Frontend (13 senaryo)

4. **Command Palette entity modu**: `Cmd+K` ile palette açılmalı. Input boş iken Recent Searches ve Favorites grupları görülmeli. En az 2 karakter girince entity modu aktif olmalı; API sonuçları gruplu (SIM, APN, Operator, Policy, User) gösterilmeli. Sonuç satırı formatı: `[SIM] 89...1234 — Active — Vodafone` benzeri label + sub. Enter ile ilgili detail sayfasına yönlendirilmeli.
5. **Arama sonucu boş durumu**: Hiç sonuç gelmeyen bir sorgu girince "No results for X." mesajı görülmeli.
6. **Recent Searches**: Palette'e bir sorgu yazıp Enter basılınca, o sorgu Recent Searches listesine eklenmeli. Palette tekrar açılınca listede görünmeli. 10'dan fazla arama yapılınca en eski silinmeli.
7. **`/` kısayolu**: Herhangi bir sayfada `/` tuşuna basılınca Command Palette açılıp input odaklanmalı.
8. **`?` kısayolu**: `?` tuşuna basılınca Keyboard Shortcuts Help Modal açılmalı; tüm kısayollar tablo halinde görülmeli. `Esc` ile kapanmalı.
9. **`g+X` navigasyon kısayolları**: `g` ardından `s` → `/sims` sayfasına gitmelidir. `g+a` → `/apns`, `g+o` → `/operators`, `g+p` → `/policies`, `g+d` → `/`, `g+j` → `/jobs`, `g+u` → `/audit`. Kısayol yanlış sırada ya da tek tuş olarak basılınca tetiklenmemeli.
10. **Favoriler**: Bir SIM detail sayfasında (`/sims/{id}`) yıldız ikonuna tıklanınca yıldız dolu olmalı; sidebar "Favorites" bölümünde SIM görünmeli. Sayfa yenilendikten sonra (localStorage) favori korunmalı. 20 favori sınırı: 20'den sonra yeni ekleme yapılınca eski silinmeli.
11. **Recent Items**: SIM detail sayfasını ziyaret edince sidebar "Recent" bölümünde o SIM görünmeli; max 20 kayıt tutulmalı; deduplication çalışmalı (aynı SIM'i iki kez ziyaret edince listede sadece bir kez olmalı).
12. **Row Actions Menu**: SIM listesinde bir satırın üzerine gelinince `⋮` butonu görünmeli; tıklanınca "View Detail, Copy ICCID, Copy IMSI, Suspend, Activate, Assign Policy, Run Diagnostics, View Audit" seçenekleri açılmalı. "Copy ICCID" tıklanınca ICCID panoya kopyalanmalı. APN, Operator, Policy, Audit, Session, Job, Alert listelerinde de kendi aksiyonları ile çalışmalı.
13. **Row Quick-Peek**: SIM listesinde bir satırın üzerinde 500ms+ beklince özet popover görünmeli (3–4 alan: ICCID, state, operator, apn). Fare çekilince kapanmalı. Popover içindeki "Open" / kart alanına tıklanınca detail sayfasına gidilmeli.
14. **Detail page `e` / `Backspace` kısayolları**: `data-detail-page="true"` attribute'una sahip bir detail sayfasında `e` tuşuna basılınca `argus:edit` custom event dispatch edilmeli (modal açılması sayfaya bağlı). `Backspace` → önceki listeye dönmeli.
15. **Klavye kısayolları help modal içeriği**: Açılan modal tabloda en az şu satırlar bulunmalı: `?` → Shortcuts Modal, `/` → Open Search, `Cmd+K` → Open Palette, `G+S/A/O/P/D/J/U` → Go To, `Esc` → Close. APNs ve Audit satırları doğru yönlendirme ile kayıtlı olmalı.
16. **tsc + build doğrulaması**: Tüm yeni bileşenler (`row-actions-menu.tsx`, `row-quick-peek.tsx`, `favorite-toggle.tsx`, `keyboard-shortcuts.tsx`, `use-search.ts`, `use-keyboard-nav.ts`) TypeScript hatasız derlenmeli; `npm run build` ✓ olmalı.

### Test command
```bash
make test   # 2712 test gecmeli
go build ./...  # Derleme hatasi olmamali
cd web && npm run build  # Frontend build basarili olmali (~4.2s)
npx tsc --noEmit  # TypeScript hata olmamali
```

---

## STORY-077: Enterprise UX Polish & Ergonomics

### Backend / Altyapi (6 senaryo)

1. **Saved views CRUD** _(DEFERRED per FIX-218: FE "Save View" button removed; backend endpoints retained for AC-3 future reintroduction — backend-only smoke still valid)_: `tenant_admin` JWT ile `POST /api/v1/user/views` body `{page:"sims", name:"Active VF", filters_json:{...}, is_default:true}` → 201 döner; `GET /api/v1/user/views?page=sims` → oluşturulan view listede olmalı. `DELETE /api/v1/user/views/:id` → 204. Başka tenant'ın JWT'si ile aynı view_id → 404 dönmeli.
2. **Undo endpoint**: Bir bulk-suspend işlemi sonrası oluşturulan `action_id` ile `POST /api/v1/undo/:action_id` → 200 ve inverse işlem uygulanmış olmalı. 15 saniye TTL geçince aynı action_id ile istek → 404 `NOT_FOUND`. Farklı tenant JWT ile geçerli action_id → 404 döner (tenant isolation).
3. **CSV export — SIM**: `GET /api/v1/sims/export?format=csv&status=active&operator_id=X` → `Content-Type: text/csv` streaming response; `Content-Disposition: attachment; filename=sims_active_...csv`. Her 500 satırda bir flush yapılmalı; 10K satırda OOM çıkmamalı.
4. **Announcements CRUD**: `super_admin` JWT ile `POST /api/v1/admin/announcements` → 201; `GET /api/v1/announcements/active` → başlangıç/bitiş tarihinde aktif olan duyurular listesi dönmeli. Başlangıç tarihi ileride olan duyuru aktif listede görünmemeli. `POST /api/v1/announcements/:id/dismiss` → 204; tekrar `/active` çağrısında o duyuru `dismissed:true` ile işaretlenmeli.
5. **Impersonation flow**: `super_admin` JWT ile `POST /api/v1/admin/impersonate/:user_id` → 200 + impersonation JWT dönmeli (1h exp, `impersonated=true` claim). Impersonation JWT ile `POST /api/v1/sims` → 405 veya 403 dönmeli (read-only middleware). `GET /api/v1/sims` → 200 (read-only izin). Audit log'da `impersonated_by` alanı dolu olmalı.
6. **Chart annotations**: `GET /api/v1/analytics/annotations?chart_id=usage&from=...&to=...` → tenant'a ait anotasyonlar liste olarak dönmeli. `POST /api/v1/analytics/annotations` body `{chart_id, label, annotated_at}` → 201. `DELETE /api/v1/analytics/annotations/:id` → 204.

### Frontend (10 senaryo)

7. **Saved views round-trip** _(DEFERRED per FIX-218: FE "Save View" button removed from list pages; backend + `useSavedViews` hook + `SavedViewsMenu` component retained for AC-3 future reintroduction — skip this step until the Views affordance is re-wired by a future story)_: SIM list sayfasında filtre uygula → "Save View" butonuna tıkla → isim ver → kaydet. Sidebar "My Views" bölümünde görünmeli. Tıklanınca filtreler restore edilmeli. "Set as Default" ile default yapılınca sayfayı yenile → filtreler otomatik uygulanmış olmalı.
8. **Undo toast**: Bir SIM'i suspend et → "1 SIM suspended. [Undo]" toast 10 saniye görünmeli → "Undo" tıklanınca SIM active state'e dönmeli ve "Action undone" toast görünmeli. 10 saniye geçince toast kapanmalı; Undo artık mevcut değilse 404 mesajı toast'ta gösterilmeli.
9. **Inline edit**: SIM list'te bir satırdaki label alanının üzerine gelinince kalem ikonu görünmeli. Tıklanınca contentEditable aktif olmalı. Enter veya blur → PATCH API çağrısı → optimistic olarak UI güncellenmeli. Esc → değişiklik iptal edilmeli, orijinal değer restore edilmeli.
10. **Empty state CTA**: Boş tenant (SIM yok) ile SIM list sayfasına gidince "Import your first SIMs" butonlu empty state görünmeli. Dashboard'da first-run checklist (`Connect an operator → Create an APN → Import SIMs → Create a policy`) görünmeli; her adım ilgili sayfaya link vermeli.
11. **Data freshness indicator**: Her list sayfasının altında "Last updated Xs ago" göstergesi bulunmalı. WS destekli sayfada (sessions, dashboard) "Live" yeşil badge görünmeli. WS bağlantısı kesilince badge "Offline" sarıya dönmeli. Auto-refresh selector (15s/30s/1m/off) çalışmalı.
12. **Impersonation banner**: super_admin olarak `/admin/impersonate` sayfasında bir kullanıcıya "Impersonate" tıkla → tüm sayfada üstte mor banner: "Viewing as [user@email.com] — [Tenant] | Exit". Exit butonuna basılınca banner kaybolmalı.
13. **Announcements banner**: Admin bir "Maintenance" duyurusu oluşturunca diğer kullanıcılar topbar altında renkli banner görmalı. Dismiss ikonuna tıklanınca banner kaybolmalı. Sayfa yenilendikten sonra banner tekrar görünmemeli (dismissed state korunmalı).
14. **Language toggle TR/EN**: Topbar'daki dil seçicisinden TR seçilince sayfa etiketleri Türkçe olmalı; tarih formatı `GG.AA.YYYY` görünmeli; sayılar `1.234.567` formatında olmalı. EN'e geri geçilince İngilizce formatlar restore olmalı.
15. **Table density toggle**: Toolbar'daki density butonuyla "Comfortable" ↔ "Compact" geçişi yapılınca CSS değişkeni `--table-row-height` uygulanmalı. Compact'ta satır yüksekliği daha küçük olmalı. Tercih sayfa yenilemeden sonra korunmalı.
16. **Column customization**: SIM list tabloya dişli ikonu tıklanınca panel açılmalı; sütunlar checkbox ile toggle edilebilmeli; drag-to-reorder çalışmalı. Reset to default tüm sütunları geri yüklemeli. Preferences yenileme sonrası korunmalı.

### Test command
```bash
make test   # 2724 test gecmeli
go build ./...  # Derleme hatasi olmamali
cd web && npm run build  # Frontend build basarili olmali (~4.3s)
npx tsc --noEmit  # TypeScript hata olmamali
```

---

## STORY-062: Performance & Doc Drift Cleanup (final sweep)

### Backend / Perf (5 senaryo)

1. **Dashboard cache 30s TTL**: `GET /api/v1/dashboard/summary` icin tenant JWT ile iki ardisik istek gonder; ikinci istekte Redis `HIT` logu gorulmeli. 30 saniye bekleyip tekrar istekte bulun → `MISS` logu gorulmeli. Ardından `sim.updated` NATS eventi yayinla (ornegin bir SIM durum degistir) → aninda cache invalidation olmali (`dashboard:<tenant_id>` anahtari Redis'ten silinmeli).
2. **MSISDN toplu import**: `POST /api/v1/msisdn-pool/bulk` ile 10.000+ satirlik CSV upload et → arka planda `INSERT ... VALUES ...ON CONFLICT DO NOTHING` calistirilmali; tek tek INSERT dongusu yoktur. DB logunda tek bir cok degerli INSERT ifadesi (500'luk bloklar) gorulmeli. Tekrar ayni CSV yuklersek `duplicates_skipped` sayisi artar, hata olmaz.
3. **Aktif session Redis sayaci**: Yeni bir RADIUS session baslat (`session.started` eventi tetikle) → `sessions:active:count:<tenant_id>` Redis anahtari 1 artar. Session bitirince (`session.ended` eventi) 1 azalir. `GET /api/v1/dashboard/summary` yaniti `active_sessions` degerini Redis'ten okumali; DB sorgusu logu yoktur (cache hit).
4. **Audit tarih aralik sinirlama**: `GET /api/v1/audit-logs?from=2020-01-01` (to parametresi yok) → 400 `INVALID_DATE_RANGE` donmeli. `?from=2020-01-01&to=2020-06-01` (91 gunluk aralik) → 400 `INVALID_DATE_RANGE` donmeli. `?from=2024-01-01&to=2024-03-01` (89 gunluk aralik) → 200 donmeli.
5. **Session CSV export**: `GET /api/v1/sessions/export.csv` ile `sim_manager` rolundeki JWT ile istek gonder → `Content-Type: text/csv`, `Content-Disposition: attachment; filename=sessions_....csv` donmeli. Buyuk dataset icin OOM olmamali (cursor streaming).

### Test command
```bash
make test   # 2738 test gecmeli
go build ./...  # Derleme hatasi olmamali
cd web && npm run build  # Frontend build basarili olmali (~4.3s)
npx tsc --noEmit  # TypeScript hata olmamali
```

---

## STORY-078: SIM Compare & System Config Backfill

### SIM Compare (sim_manager)

1. **Happy path**: Login as a `sim_manager` user. Navigate to `/sims/compare`. Select 2 different SIMs from the same tenant. Verify the side-by-side diff renders with rows where `equal=false` visually highlighted (e.g. different background or indicator). Confirm all diff rows have the field name, SIM A value, SIM B value, and `equal` flag in the response payload.
2. **Audit log entry**: After a successful compare, call `GET /api/v1/audit-logs?action=sim.compare` — the entry should appear with metadata containing `sim_id_b` (the second SIM's ID).
3. **Negative — same SIM twice**: Submit a compare request with the same SIM ID for both `sim_id_a` and `sim_id_b` → expect `422 VALIDATION_ERROR`.
4. **Negative — cross-tenant SIM**: Attempt to pass a SIM ID that belongs to a different tenant → expect `404 SIM_NOT_FOUND` (ID enumeration prevention; do NOT expect `403 FORBIDDEN_CROSS_TENANT` here).

### System Config (super_admin)

5. **Happy path**: Login as `admin@argus.io` (or any `super_admin`). Run:
   ```bash
   curl -H "Authorization: Bearer <jwt>" http://localhost:8080/api/v1/system/config
   ```
   Verify the response body includes all of: `version`, `git_sha`, `build_time`, `started_at`, `feature_flags`, `protocols`, `limits`, `retention`.
6. **Secret scrubbing**: Grep the response body for any of the following strings — none should appear: `JWT_SECRET`, `ENCRYPTION_KEY`, `DB_PASSWORD`, `SMTP_PASSWORD`, `TELEGRAM_BOT_TOKEN`, `S3_SECRET_KEY`.
7. **Negative — tenant_admin**: Make the same request with a `tenant_admin` JWT → expect `403 FORBIDDEN`.
8. **Negative — unauthenticated**: Make the same request without an `Authorization` header → expect `401 UNAUTHORIZED`.

### Test command
```bash
make test   # existing suite must pass
go build ./...  # no compilation errors
```

---

## STORY-079: Phase 10 Post-Gate Follow-up Sweep

### argus CLI subcomutları (operator / super_admin)

1. **migrate subcommand**: Docker dışında doğrudan binary çalıştırın:
   ```bash
   ./argus migrate up
   ```
   Daha önce uygulanmış migration'lar varsa `no change` mesajı görmeli; temiz volumede migration'lar sırasıyla uygulanmalı.
2. **migrate — yön yoksa hata**: `./argus migrate` (direction vermeden) → `"migrate: direction required (up|down)"` hata mesajı görmeli ve sıfırdan olmayan çıkış kodu dönmeli.
3. **seed subcommand**: `./argus seed /path/to/seed.sql` → seed çıktısını logda görmeli, hatasız tamamlanmalı.
4. **version subcommand**: `./argus version` → `version`, `git_sha`, `build_time` alanlarını içeren JSON veya düz metin çıktısı görmeli.

### Seed — temiz volume (super_admin)

5. **Temiz volume seed**: Docker volume'u tamamen sil (`docker compose down -v`), yeniden başlat (`docker compose up -d`). `make db-seed` çalıştır → hatasız tamamlanmalı. `GET /api/v1/sims?limit=5` isteği en az 1 SIM dönmeli.
6. **Seed tekrar çalıştırma**: Seed ikinci kez çalıştırıldığında `ON CONFLICT DO NOTHING` / `DO UPDATE` sayesinde hatasız tamamlanmalı (idempotent).

### /sims/compare — URL parametresi ön-doldurma (sim_manager)

7. **URL'den ön-doldurma**: `/sims/compare?sim_id_a=<uuid-A>&sim_id_b=<uuid-B>` adresine doğrudan gidin. Her iki SIM input alanının ilgili UUID değerleriyle otomatik dolu geldiğini doğrulayın.
8. **Compare butonu — /sims listesinden**: `/sims` listesinde herhangi bir SIM satırının yanındaki "Compare" butonuna tıklayın. `/sims/compare?sim_id_a=<seçilen-uuid>` adresine yönlendirmeli ve input A ön-dolu gelmelidir.
9. **Geçersiz UUID — girişte**: `sim_id_a` parametresi olarak `not-a-uuid` değerini verin → input alanı boş/temiz kalmalı (geçersiz değer sessizce düşürülmeli) veya bir validasyon uyarısı görünmeli.

### /dashboard alias (tüm JWT kullanıcıları)

10. **Alias yönlendirme**: Giriş yapın, ardından adres çubuğuna `/dashboard` yazın. Sayfa 404 yerine ana Dashboard sayfasını render etmeli.
11. **Bookmark deep-link**: Tarayıcıyı kapatın, doğrudan `http://localhost:8084/dashboard` adresini açın (geçerli oturum cookiesi mevcut). Dashboard sayfası yüklenmelidir — 404 görmemeli.

### Oturum toast sessizleştirme (sim_manager)

12. **İlk yükleme — toast yok**: Giriş yapın. Dashboard ilk yüklenirken `"Invalid session ID format"` içerikli kırmızı/turuncu bir toast bildirimi **görünmemeli**. (Eski davranış: boş oturum ID'si ile çağrılan `DELETE /auth/sessions/` endpoint'i hata toast'u tetikliyordu.)
13. **Geçerli oturum silme**: Ayarlar → Oturumlar. Başka bir aktif oturum seçin, "Sil" butonuna tıklayın → oturum listeden kalkmalı, başarı toast'u görünmeli. Kendi mevcut oturumunuzu silmeye çalışırsanız uygun hata mesajı görünmeli.

### /api/v1/status/details — recent_error_5m canlı (super_admin)

14. **Sıfır hata durumunda**: `curl http://localhost:8080/api/v1/status/details | jq '.data.recent_error_5m'` çalıştırın → `0` dönmeli (son 5 dakikada 5xx yok).
15. **5xx üret — sayacı gör**: 5xx tetikleyecek bir istek yapın (örn. payload olmadan POST), ardından `recent_error_5m` sorgulayın → değer `0`'dan büyük olmalı.
16. **5 dakika sonra sıfırlanma**: Son 5xx'den 5 dakika (300 saniye) sonra `recent_error_5m` yeniden `0`'a dönmeli (pencere dışına çıkmış kayıtlar atılır).

### i18n posture kararı (bilgilendirme)

17. **DEV-234 kararı doğrulama**: `docs/brainstorming/decisions.md` içinde DEV-234 kaydını bulun. "DEFER to dedicated localization story post-GA" kararını içermeli. UI'da TR/EN toggle varsa toggle çalışmalı fakat tam TR çevirisi eksik olabilir — bu beklenen davranış.

### /policies Compare posture kararı (bilgilendirme)

18. **DEV-235 kararı doğrulama**: `docs/brainstorming/decisions.md` içinde DEV-235 kaydını bulun. "NO — close the Phase 10 gate note recommendation" kararını içermeli. `/policies` sayfasında Compare butonu **olmamalı** — bu bilinçli bir tasarım kararıdır.

### Test command
```bash
make test   # 2870 test geçmeli
go build ./...  # Derleme hatası olmamalı
cd web && npm run build  # Frontend build başarılı olmalı
npx tsc --noEmit  # TypeScript hatası olmamalı
```

---

## STORY-086: [AUDIT-GAP] sms_outbound tablosunu geri yükle + önyükleme zamanı şema bütünlüğü kontrolü

Bu story backend/altyapi odaklıdır (UI değişikliği yok). Testler Docker stack çalışır durumdayken yapılmalıdır (`make up && make db-migrate`).

### 1. Onarım öncesi / sonrası canlı DB kontrolü

```bash
# ÖNCE (migration uygulanmadan önce sms_outbound'u simüle etmek için):
docker compose exec postgres psql -U argus -d argus \
  -c "SELECT to_regclass('public.sms_outbound');"
# Beklenen: NULL değil (migration 20260417000004 zaten uygulandı)

# Sibling tablolar hâlâ mevcut:
docker compose exec postgres psql -U argus -d argus \
  -c "SELECT to_regclass('public.onboarding_sessions'), to_regclass('public.notification_templates');"
# Beklenen: her ikisi de non-NULL

# Schema migrations versiyonunu doğrula:
docker compose exec postgres psql -U argus -d argus \
  -c "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 3;"
# Beklenen: 20260417000004, dirty=false en üstte
```

### 2. API duman testi (smoke test)

```bash
# JWT token al:
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')

# SMS geçmişini sorgula (tablonun varlığını kanıtlar):
curl -s -o /dev/null -w "%{http_code}\n" \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8084/api/v1/sms/history
# Beklenen: 200

# Tam yanıt zarfını kontrol et:
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8084/api/v1/sms/history | jq '.status'
# Beklenen: "success"
```

### 3. Tetikleyici reddi gösterimi (check_sim_exists)

```bash
# Geçersiz bir sim_id ile doğrudan DB'ye INSERT dene:
docker compose exec postgres psql -U argus -d argus -c "
  SET app.current_tenant = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';
  INSERT INTO sms_outbound (tenant_id, sim_id, msisdn, text_hash, status)
  VALUES ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
          '00000000-0000-0000-0000-000000000001',
          '+905550000001', 'deadbeef', 'queued');
"
# Beklenen: HATA mesajı içermelidir:
#   ERROR:  FK violation: sim_id 00000000-0000-0000-0000-000000000001 does not exist in sims

# Doğrulama: tetikleyici pg_trigger'da kayıtlı:
docker compose exec postgres psql -U argus -d argus \
  -c "SELECT tgname FROM pg_trigger WHERE tgrelid = 'sms_outbound'::regclass AND NOT tgisinternal;"
# Beklenen: trg_sms_outbound_check_sim
```

### 4. Önyükleme zamanı FATAL kontrolü (boot-check demo)

```bash
# sms_outbound tablosunu simüle amacıyla düşür:
docker compose exec postgres psql -U argus -d argus \
  -c "DROP TABLE sms_outbound CASCADE; UPDATE schema_migrations SET version=20260417000003, dirty=false;"

# Argus'u yeniden başlat:
docker compose restart argus

# Logları izle — FATAL mesajı bekle:
docker compose logs argus --since=30s 2>&1 | grep -E "FATAL|schemacheck|missing"
# Beklenen satır (örnek):
#   {"level":"fatal","error":"schemacheck: critical tables missing from database: [sms_outbound]",
#    "expected_tables":["announcement_dismissals",...,"webhook_deliveries"],
#    "message":"boot: schema integrity check failed — run 'argus migrate up' or inspect schema drift"}

# Konteyner exit code 1 ile döngüye girmeli (restart policy):
docker compose ps argus | grep -E "Restarting|Exit"

# Geri yükle — migration uygula, ardından tekrar başlat:
make db-migrate
docker compose restart argus
docker compose logs argus --since=30s 2>&1 | grep -E "schema integrity|postgres connected"
# Beklenen: "schema integrity check passed" — container temiz boot'a geçmeli
```

### Test komutu

```bash
go test ./internal/store/schemacheck/... -v
# Beklenen: 2/2 birim testi PASS (DATABASE_URL ayarlı değilse 3. test atlanır)

DATABASE_URL=postgres://argus:argus_secret@localhost:5450/argus?sslmode=disable \
  go test ./internal/store/schemacheck/... -v
# Beklenen: 3/3 PASS (TestVerify_MissingTableReportsError dahil)

DATABASE_URL=postgres://argus:argus_secret@localhost:5450/argus?sslmode=disable \
  go test ./internal/store -run TestSmsOutbound_RelationPresentAfterMigrations -v
# Beklenen: PASS — tablo mevcut + RLS'li insert başarılı
```

---

## STORY-083: Diameter Simulator Client (Gx/Gy)

Bu story backend/altyapi odaklıdır (simulator dev tool, UI değişikliği yok). Testler Docker stack ve simulator çalışır durumdayken yapılmalıdır.

### Birim ve entegrasyon testleri

```bash
go test ./internal/simulator/... -v
# Beklenen: 41 test PASS (config, peer, ccr, client, engine, metrics paketleri)

go test -race ./internal/simulator/...
# Beklenen: 41 test PASS, race raporu yok

go test -tags=integration -race -run TestSimulator_AgainstArgusDiameter ./internal/simulator/diameter/...
# Beklenen: PASS — in-process argusdiameter.Server karşısında tam Gx+Gy CCR döngüsü
```

### 1. Diameter peer başlatma senaryosu (AC-1)

```bash
# Simulator'ı Diameter etkinleştirilmiş bir operatör ile başlat:
make up                              # argus-app + pg + redis + nats
make sim-up                          # turkcell operatörü için diameter.enabled=true ile simulator

# Peer Open durumunu doğrula (30 saniye içinde):
curl -s http://localhost:9099/metrics | grep simulator_diameter_peer_state
# Beklenen: simulator_diameter_peer_state{operator="turkcell"} 3
#   (3 = Open; CER/CEA el sıkışması başarılı)
```

### 2. Gx/Gy CCR metrikleri senaryosu (AC-2/3/7)

```bash
# 2 dakika simülasyon çalıştır, ardından metrikleri kontrol et:
sleep 120
curl -s http://localhost:9099/metrics | grep simulator_diameter_requests_total
# Beklenen (en az):
#   simulator_diameter_requests_total{operator="turkcell",app="gx",type="ccr_i"} > 0
#   simulator_diameter_requests_total{operator="turkcell",app="gx",type="ccr_t"} > 0
#   simulator_diameter_requests_total{operator="turkcell",app="gy",type="ccr_i"} > 0
#   simulator_diameter_requests_total{operator="turkcell",app="gy",type="ccr_u"} > 0
#   simulator_diameter_requests_total{operator="turkcell",app="gy",type="ccr_t"} > 0

curl -s http://localhost:9099/metrics | grep simulator_diameter_responses_total
# Beklenen: result="success" sayacı sıfırdan büyük, result="error_*" veya "timeout" yok

curl -s http://localhost:9099/metrics | grep simulator_diameter_latency_seconds
# Beklenen: histogram bucket'ları dolu (count > 0)

curl -s http://localhost:9099/metrics | grep simulator_diameter_session_aborted_total
# Beklenen: normal çalışmada bu sayacın artmaması (0 veya yok)
```

### 3. Argus HTTP CDR doğrulama (plan AC-4 — manuel smoke)

```bash
# Geçerli token ve tenant ID ile:
curl -sSf \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8084/api/v1/cdrs?protocol=diameter&limit=10" | jq '.data | length'
# Beklenen: sıfırdan büyük bir tam sayı (Diameter oturumları CDR kaydı oluşturmalı)
```

Not: Bu test otomasyonu DEFERRED edildi (F-A1 — future test-infra story). Birincil kanıt entegrasyon testindeki `TestSimulator_AgainstArgusDiameter`'dır.

### 4. RADIUS-only fallback senaryosu (AC-5/8)

```bash
# Diameter devre dışı bir operatör için Diameter metriklerinin sıfır kalmasını doğrula:
curl -s http://localhost:9099/metrics | grep 'simulator_diameter_requests_total{operator="vodafone"'
# Beklenen: çıktı yok (vodafone operatörü RADIUS-only, Diameter etkinleştirilmemiş)

# STORY-082 RADIUS metrikleri etkilenmemiş olmalı:
curl -s http://localhost:9099/metrics | grep simulator_radius_requests_total
# Beklenen: tüm operatörler için RADIUS sayaçları artmış durumda
```

---

## STORY-084: 5G SBA Simulator Client (AUSF/UDM)

Bu story backend/altyapi odaklıdır (simulator dev tool, UI değişikliği yok). Testler Docker stack ve simulator çalışır durumdayken yapılmalıdır.

### Birim ve entegrasyon testleri

```bash
go test ./internal/simulator/... -v
# Beklenen: 81 test PASS (config, diameter, engine, metrics, radius, sba, scenario paketleri)

go test -race ./internal/simulator/...
# Beklenen: 81 test PASS, race raporu yok

go test -tags=integration -run TestSimulator_AgainstArgusSBA ./internal/simulator/sba/...
# Beklenen: 26 test PASS — in-process aaasba.Server karşısında tam AUSF+UDM döngüsü

go test -tags=integration -run TestSimulator_MandatoryIE_Negative ./internal/simulator/sba/...
# Beklenen: PASS — boş servingNetworkName ile 400 + MANDATORY_IE_INCORRECT hatası
```

### 1. SBA etkinleştirilmiş operatör senaryosu (AC-1/2)

```bash
# Simulator'ı SBA etkinleştirilmiş bir operatör ile başlat:
make up                              # argus-app + pg + redis + nats
make sim-up                          # turkcell operatörü için sba.enabled=true, rate=0.2 ile simulator

# 2 dakika bekle, sonra SBA metriklerini kontrol et:
sleep 120
curl -s http://localhost:9099/metrics | grep simulator_sba_requests_total
# Beklenen (en az):
#   simulator_sba_requests_total{operator="turkcell",service="ausf",endpoint="authenticate"} > 0
#   simulator_sba_requests_total{operator="turkcell",service="ausf",endpoint="confirm"} > 0
#   simulator_sba_requests_total{operator="turkcell",service="udm",endpoint="register"} > 0

curl -s http://localhost:9099/metrics | grep simulator_sba_responses_total
# Beklenen: result="success" sayacı > 0, result="error_*" veya "timeout" yok

curl -s http://localhost:9099/metrics | grep simulator_sba_latency_seconds
# Beklenen: histogram bucket'ları dolu (count > 0)

curl -s http://localhost:9099/metrics | grep simulator_sba_session_aborted_total
# Beklenen: normal çalışmada bu sayacın artmaması (0 veya yok)
```

### 2. Argus SBA proxy log doğrulama (AC-3)

```bash
# 5G SBA oturumları için Argus'un :8443 portunda üç beklenen istek yolunu kontrol et:
docker logs argus-app 2>&1 | grep -E "/nausf-auth/v1/ue-authentications|5g-aka-confirmation|/nudm-uecm/v1/.*/registrations"
# Beklenen: Her SBA oturumu için üç satır:
#   POST /nausf-auth/v1/ue-authentications
#   PUT  /nausf-auth/v1/ue-authentications/<uuid>/5g-aka-confirmation
#   PUT  /nudm-uecm/v1/<supi>/registrations/amf-3gpp-access
```

### 3. prod_guard env enjeksiyon testi (AC-6)

```bash
# prod_guard=true + ARGUS_SIM_ENV=prod + tls_skip_verify=true kombinasyonunun reddini doğrula:
ARGUS_SIM_ENV=prod SIMULATOR_ENABLED=1 \
  ARGUS_SIM_CONFIG=deploy/simulator/config.example.yaml \
  go run ./cmd/simulator 2>&1 | head -5
# Beklenen: config validation error içeren FATAL mesajı
# ("prod_guard: TLSSkipVerify not allowed when ARGUS_SIM_ENV=prod" veya benzeri)
# NOT: config.example.yaml'da tls_skip_verify: false varsayılan; test için geçici olarak true yapın

# Sadece config validation unit testleri ile doğrulama (daha hızlı):
go test ./internal/simulator/config/... -run TestSBA_ProdGuard -v
# Beklenen: TestSBA_ProdGuardTriggers PASS, TestSBA_ProdGuardDefaultIsOn PASS, TestSBA_ProdGuardDisabled PASS
```

### 4. Failover yeniden başlatma senaryosu (AC-7)

```bash
# argus-app SBA sunucusunu durdur ve yeniden başlat; yeni oturumların devam ettiğini doğrula:
docker stop argus-app
sleep 35  # 30+ saniye bekle

# Yeniden başlat:
docker start argus-app
sleep 5   # argus-app'in hazır olmasını bekle

# Metriklerin artmaya devam ettiğini doğrula:
curl -s http://localhost:9099/metrics | grep 'simulator_sba_requests_total'
# Beklenen: sayaçların önceki değerden daha yüksek olması (yeniden bağlantı sonrası yeni oturumlar)
# NOT: HTTP stateless — Diameter'dan farklı olarak peer reconnect bekleme gerekmez

make down
```

## STORY-085: Simulator Reaktif Davranışı (Approach B)

Bu story bir geliştirici/test aracını güçlendirir — Argus production binary'sini etkilemez. Test senaryoları simülatörün reaktif modda doğru çalıştığını doğrular.

### 1. Reaktif modu etkinleştirme ve temel metrik doğrulama (AC-1, AC-5, AC-6)

```bash
# deploy/simulator/config.example.yaml dosyasını düzenle:
#   reactive.enabled: true
#   reactive.coa_listener.enabled: true
# Ardından simülatörü yeniden başlat:
make sim-up

# Reactive subsystem'in başladığını doğrula:
docker compose logs argus-simulator | grep "reactive subsystem ready"
# Beklenen: "reactive subsystem ready" içeren bir log satırı

# Reactive metrik sayaçlarını doğrula (başlangıçta boş olabilir):
curl -s http://localhost:9099/metrics | grep simulator_reactive_
# Beklenen: simulator_reactive_terminations_total, simulator_reactive_reject_backoffs_total,
#           simulator_reactive_incoming_total kayıtlı (değerleri 0 veya daha fazla)

# Birkaç dakika bekleyip termination sayaçlarını tekrar kontrol et:
sleep 120
curl -s http://localhost:9099/metrics | grep 'simulator_reactive_terminations_total'
# Beklenen: cause ∈ {session_timeout, disconnect, coa_deadline, reject_suspend, scenario_end, shutdown}
#           etiketleriyle sayaçlar (herhangi biri > 0 olabilir)
```

### 2. Session-Timeout saygısı testi (AC-1)

```bash
# Session-Timeout değerini düşük tut — Argus'ta bir SIM'in politikasını değiştir
# (örn. hard_timeout=60s) ve simülatörün o SIM'i 60s içinde sonlandırdığını gözlemle:
curl -s http://localhost:9099/metrics | grep 'simulator_reactive_terminations_total{.*session_timeout'
# Beklenen: session_timeout cause'una sahip oturumlar görünür

# Unit test ile doğrulama (daha hızlı):
go test ./internal/simulator/engine/... -run TestSessionTimeout_SubIntervalDeadlineFires -v
# Beklenen: PASS — 500ms deadline, 10s ticker altında deadline timer kazanır
```

### 3. Reject backoff testi (AC-2, AC-5)

```bash
# Bir SIM'i Argus'ta "suspended" state'e al — Access-Reject alır:
# (Argus UI'dan veya API ile SIM state değiştir)
# Simülatör exponential backoff başlatır (30s → 60s → 120s ... → 600s cap):
curl -s http://localhost:9099/metrics | grep 'simulator_reactive_reject_backoffs_total'
# Beklenen: outcome=backoff_set sayacı artıyor;
#           5 reject/saat sonra outcome=suspended görünür

# Unit test ile doğrulama:
go test ./internal/simulator/reactive/... -run TestRejectTracker_AllowedAfterSuspension -v
# Beklenen: PASS
```

### 4. CoA/DM listener testi — Disconnect-Message round-trip (AC-3, AC-7)

```bash
# Aktif bir oturumu API üzerinden zorla sonlandır:
# (Argus UI'dan Sessions sayfası veya API)
SESSION_ID="<aktif-oturum-id>"
TOKEN="<admin-jwt>"
curl -sX POST "http://localhost:8084/api/v1/sessions/${SESSION_ID}/disconnect" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "X-Tenant-ID: <tenant-id>" | jq .

# 3 saniye içinde Accounting-Stop gönderildiğini doğrula:
docker compose logs argus-simulator | grep "AcctStop" | tail -5
# Beklenen: Disconnect-Request alındıktan sonra AcctStop logu (≤3s fark)

# Incoming paket sayacını kontrol et:
curl -s http://localhost:9099/metrics | grep 'simulator_reactive_incoming_total'
# Beklenen: kind=dm, result=ack sayacı artmış
```

### 5. CoA-Request Session-Timeout güncellemesi (AC-4)

```bash
# Argus politika motoru CoA gönderdiğinde (örn. SIM politikası değiştiğinde)
# simülatörün yeni Session-Timeout'u kabul ettiğini doğrula:
curl -s http://localhost:9099/metrics | grep 'simulator_reactive_incoming_total{.*kind="coa"'
# Beklenen: kind=coa, result=ack sayacı artıyor

# Integration test ile doğrulama:
go test -tags=integration ./internal/simulator/reactive/... -run TestReactive_CoAUpdatesDeadline_EndToEnd -v
# Beklenen: PASS
```

### 6. CoA listener yalnızca etkinleştirildiğinde bind ettiğini doğrulama (AC-7)

```bash
# reactive.enabled: false veya coa_listener.enabled: false ile:
# UDP :3799 portu AÇIK OLMAMALI:
nc -zu localhost 3799 2>&1
# Beklenen: bağlantı reddedilmeli (port kapalı)

# Unit test ile doğrulama:
go test ./internal/simulator/reactive/... -run TestReactive_ListenerUnbound_WhenDisabled -v
# Beklenen: PASS
```

### 7. Tam simülatör kapatma

```bash
make sim-down
# Beklenen: tüm oturumlar temiz kapanır; shutdown cause'u olan termination logu görünür
curl -s http://localhost:9099/metrics 2>&1 | head -3
# Beklenen: bağlantı reddedilmeli (simülatör down)
```

---

## STORY-087: [TECH-DEBT] D-032 Pre-069 sms_outbound Shim (Temiz Volume Bootstrap)

Bu story backend/altyapi odaklıdır (UI değişikliği yok). Test senaryoları temiz volume (fresh volume) ortamında ve mevcut canlı DB üzerinde doğrulama yapılmasını kapsar.

**Önemli not**: Testler için `DATABASE_URL` ortam değişkeni ayarlanmış çalışan bir PostgreSQL gereklidir. Go testleri bu değişken yoksa otomatik olarak atlanır (`t.Skip`). Ayrıca TimescaleDB 2.26.2 kullanan ortamlarda migration 20260412000006 sırasında `operation not supported on hypertables that have columnstore enabled` hatası alınabilir — bu STORY-087 ile ilgili değil, D-037 olarak kayıt altına alınmıştır.

### 1. Temiz volume fresh bootstrap (AC-1)

```bash
# Tüm container ve volume'ları kaldır:
make down
docker volume rm argus_postgres-data

# Stack'i yeniden başlat:
make up

# Migration zincirini baştan çalıştır:
make db-migrate
# Beklenen: exit 0, hata yok

# Migration durumunu doğrula:
docker compose exec postgres psql -U argus -d argus \
  -c "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1;"
# Beklenen: en yüksek versiyon, dirty=false

# sms_outbound tablosunun oluştuğunu doğrula:
docker compose exec postgres psql -U argus -d argus \
  -c "SELECT to_regclass('public.sms_outbound');"
# Beklenen: non-NULL (public.sms_outbound)

# Argus boot logunda şema bütünlüğü kontrolü:
docker compose logs argus | grep "schema integrity check passed"
# Beklenen: "schema integrity check passed tables=12"
```

### 2. FK kontrolü — sim_id üzerinde FK olmadığını doğrula (AC-4)

```bash
docker compose exec postgres psql -U argus -d argus -c "
SELECT COUNT(*) FROM pg_constraint
WHERE contype='f' AND conrelid='sms_outbound'::regclass;"
# Beklenen: 1 (yalnızca tenant_id → tenants(id) FK'si; sim_id FK yok)
```

### 3. Trigger ve index/RLS kontrolü (AC-5, AC-6, AC-7)

```bash
# check_sim_exists trigger varlığı:
docker compose exec postgres psql -U argus -d argus -c "
SELECT tgname, tgenabled FROM pg_trigger
WHERE tgrelid='sms_outbound'::regclass AND tgname='trg_sms_outbound_check_sim';"
# Beklenen: 1 satır, tgenabled='O'

# Named index'ler:
docker compose exec postgres psql -U argus -d argus -c "
SELECT indexname FROM pg_indexes WHERE tablename='sms_outbound' ORDER BY indexname;"
# Beklenen: idx_sms_outbound_provider_id, idx_sms_outbound_status, idx_sms_outbound_tenant_sim_time dahil

# RLS policy:
docker compose exec postgres psql -U argus -d argus -c "
SELECT policyname FROM pg_policies WHERE tablename='sms_outbound';"
# Beklenen: sms_outbound_tenant_isolation
```

### 4. Canlı DB üzerinde no-op doğrulama (AC-2)

```bash
# Canlı DB zaten head versiyonda — migrate up tekrar çalıştır:
docker compose exec argus /app/argus migrate up
# Beklenen: exit 0, log "migrate: no change — already at latest version"

# Sentinel test: shim'in tabloyu yeniden oluşturmadığını doğrula:
docker compose exec postgres psql -U argus -d argus -c "
ALTER TABLE sms_outbound ALTER COLUMN text_preview SET DEFAULT 'sentinel';"
docker compose exec argus /app/argus migrate up
docker compose exec postgres psql -U argus -d argus -c "
SELECT column_default FROM information_schema.columns
WHERE table_name='sms_outbound' AND column_name='text_preview';"
# Beklenen: 'sentinel' (shim tabloyu yeniden oluşturmadı)
# Sentinel'i geri al:
docker compose exec postgres psql -U argus -d argus -c "
ALTER TABLE sms_outbound ALTER COLUMN text_preview DROP DEFAULT;"
```

### 5. Down zinciri doğrulama (AC-8)

```bash
docker compose exec argus /app/argus migrate down -all
# Beklenen: exit 0

docker compose exec postgres psql -U argus -d argus -c "
SELECT to_regclass('public.sms_outbound');"
# Beklenen: NULL (tablo kaldırıldı)
```

## STORY-088: [TECH-DEBT] D-033 — `go vet` non-pointer `json.Unmarshal` fix

**Backend/test-tooling only. No UI. No production behaviour change.**

### 1. Vet temizliği doğrulama (AC-1)

```bash
cd /path/to/argus
go vet ./...
# Beklenen: çıkış 0, sıfır uyarı
# (Önceki durum: internal/policy/dryrun/service_test.go:333:30: call of Unmarshal passes non-pointer as second argument)
```

## STORY-092: Dynamic IP Allocation pipeline + SEED FIX

Bu story backend/altyapi odaklıdır — RADIUS / Diameter Gx / 5G SBA Nsmf hot-path'larında IP tahsis zincirini devreye alır. UI değişikliği yok; mevcut `/sessions` + `/settings/ip-pools` + `/sims/:id` ekranları otomatik olarak populate olur.

**Önemli not**: D-038 integration testi için `DATABASE_URL` ortam değişkeni gerekli; aksi halde test otomatik olarak atlanır (`t.Skip`).

### 1. Seed 006 idempotency + reservation doğrulama (AC-7)

```bash
# Seed'i iki kez çalıştır ve idempotent olduğunu gözle:
docker compose exec postgres psql -U argus -d argus -f /docker-entrypoint-initdb.d/006_reserve_sim_ips.sql
docker compose exec postgres psql -U argus -d argus -f /docker-entrypoint-initdb.d/006_reserve_sim_ips.sql
# Beklenen: her iki çalıştırma da "INSERT 0 N" + "UPDATE 0" satırları (ikinci koşu no-op)

# Materialised ip_addresses satır sayısını doğrula (seed 003 + seed 005):
docker compose exec postgres psql -U argus -d argus -c "
SELECT COUNT(*) FROM ip_addresses;"
# Beklenen: 700 (seed 003'ün 13 pool + m2m.water'dan materialise edilen tüm rezerve edilebilir adresler)

# Reservation count — active + APN-assigned SIMs için 1:1:
docker compose exec postgres psql -U argus -d argus -c "
SELECT COUNT(*) FROM sims WHERE state='active' AND apn_id IS NOT NULL AND ip_address_id IS NOT NULL;"
# Beklenen: 129 (fail-fast assert seed 006 sonunda zaten bu sayıyı doğrular)
```

### 2. `/settings/ip-pools` kapasite smoke (AC-1 görsel)

```bash
# Stack ayakta:
make up

# Login:
# URL: http://localhost:8084/login
# admin@argus.io / admin

# Navigate: /settings/ip-pools
# Beklenen: 4+ aktif pool, her biri USED > 0 (kapasite 3-23 arası, seed 003'ten)
# Referans: docs/stories/test-infra/STORY-092-evidence/ippools-list.png
```

### 3. `/sessions` IP column doğrulama (AC-1 görsel)

```bash
# Simulator'u başlat:
make sim-up

# Navigate: /sessions
# Beklenen: 30+ aktif session, her satırda IP column doldu (10.20.x veya 10.21.x)
# Referans: docs/stories/test-infra/STORY-092-evidence/sessions-list.png
```

### 4. SIM detay IP address field (AC-1 görsel)

```bash
# Navigate: /sims
# Bir active SIM'e tıkla (örn. IMSI 89900100000000002002)
# Beklenen: IP Address alanı "10.20.0.2/32" gibi dolu, state=active
# Referans: docs/stories/test-infra/STORY-092-evidence/sim-detail.png
```

### 5. D-038 nil-cache integration regression (AC-9)

```bash
# DATABASE_URL ayarlı olmalı (test kendi tenant + operator + APN + pool + policy + SIM fixture'ını seed'ler):
export DATABASE_URL="postgres://argus:argus@localhost:5432/argus_test?sslmode=disable"

cd /path/to/argus
go test -run TestEnforcerNilCacheIntegration_STORY092 ./internal/aaa/radius/... -v
# Beklenen:
# - go test exit 0
# - PASS TestEnforcerNilCacheIntegration_STORY092
# - Test, enforcer.New(nil, policyStore, violationStore, nil, nil, ...) literal-nil patterniyle boot
# - RADIUS Access-Request → Access-Accept + Framed-IP attribute assert
# - NPE olmaz (D-038 hole integration seviyesinde kapanıyor)
```

### 6. Full sentinel sweep (12 test)

```bash
go test ./internal/aaa/radius/... ./internal/aaa/diameter/... ./internal/aaa/sba/... ./internal/store/... -run "STORY092|DynamicAllocation|FramedIPAddress|ReleasesDynamic|PreservesStatic|FallbackFramedIP|AllocatesIP|AllocateReleaseCycle|RecountUsedAddresses" -v
# Beklenen: 12/12 sentinel PASS (RADIUS×4 + Gx×3 + SBA×1 + store×4)
```

### 7. Baseline regression guard

```bash
go test ./... 2>&1 | tail -n 5
# Beklenen: 3024 PASS no-DB / 3057 PASS with-DB
# 15 pre-existing DB FAIL unchanged (BackupStore×2, BackupCodeStore×8, FreshVolumeBootstrap_STORY087, DownChain_STORY087, PasswordHistory×3)
```

## STORY-090: Multi-protocol operator adapter refactor

Operator Protocols sekmesi UI test senaryoları. `make up` + `make db-migrate` + `make db-seed` ile tam çalışan ortam gerekmektedir. Testler freshly-built argus-app'e karşı çalıştırılmalı (stale binary riski: `docker images argus-argus` tarihini kontrol et).

### 1. Protocols sekmesi — ilk render doğrulama (AC-4)

```bash
# Navigate: http://localhost:8084/operators/<id>
# Protocols sekmesine tıkla
# Beklenen: 5 kart (RADIUS / Diameter / SBA / HTTP / Mock), her biri "ENABLED" veya "DISABLED" badge'i
# ile ilk renderda doğru durumu gösterir (F-A2 sonrası useOperator detail endpoint'den gelir)
# NOT: Eski davranış — "all disabled on first render" görünürse useOperator list-filter yerine
# GET /api/v1/operators/:id detail endpoint'i kullanmıyor demektir.
```

### 2. Header chip derivation — enabled protocols chip listesi (AC-6)

```bash
# Navigate: http://localhost:8084/operators
# Bir operatör satırına tıkla → Operatör detay sayfası
# Beklenen: header bölümünde adapter_type string yerine
# enabled_protocols dizisinden türetilen chip listesi görünür (örn. "RADIUS · MOCK")
# Doğrulama: adapter_type alanı görünmemeli; chip'ler adapter_config.*.enabled=true ile örtüşmeli
```

### 3. Per-protocol Test Connection (AC-3)

```bash
# Navigate: http://localhost:8084/operators/<id>/protocols
# Mock kartında "Test Connection" butonuna tıkla
# Beklenen: success toast → "Mock test passed (latency: ~Nms)"
#
# Curl ile doğrulama:
export TOKEN="$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.access_token')"
curl -s -X POST "http://localhost:8084/api/v1/operators/<id>/test/mock" \
  -H "Authorization: Bearer $TOKEN" | jq .
# Beklenen: {"status":"success","data":{"protocol":"mock","success":true,"latency_ms":N}}
```

### 4. PROTOCOL_NOT_CONFIGURED 422 doğrulama (AC-3 / F-A3)

```bash
# Tüm protokoller disabled olan bir operatör için test-connection çağrısı:
curl -s -X POST "http://localhost:8084/api/v1/operators/<id>/test/radius" \
  -H "Authorization: Bearer $TOKEN" | jq .
# Beklenen: HTTP 422, {"status":"error","error":{"code":"PROTOCOL_NOT_CONFIGURED","message":"..."}}
```

### 5. adapter_type kolonu yokluğu — DB doğrulama (AC-12)

```bash
docker compose exec postgres psql -U argus -d argus -c "\d operators"
# Beklenen: adapter_type kolonu tabloda YOK
# migration 20260418120000_drop_operators_adapter_type uygulanmış olmalı
```

### 6. Secret masking — adapter_config API response doğrulama (AC-4 / F-A2)

```bash
curl -s "http://localhost:8084/api/v1/operators/<id>" \
  -H "Authorization: Bearer $TOKEN" | jq '.data.adapter_config'
# Beklenen: adapter_config objesi mevcut; secret alanları (shared_secret, auth_token vs.)
# "****" sentinel ile maskeli; non-secret alanlar (listen_addr, host, port) düz metin
```

### 7. STORY-090 sentinel sweep — full test suite

```bash
go test ./internal/operator/... ./internal/api/operator/... ./internal/aaa/radius/... -run \
  "TestDetectShape|TestUpConvert|TestValidate|TestHealthChecker|TestRegistry_|TestTestConnection" -v 2>&1 | grep -E "PASS|FAIL"
# Beklenen: tüm sentinel PASS
# Önemli testler: TestHealthChecker_FansOutPerProtocol, TestRegistry_DeleteOperatorHealth,
# TestTestConnectionForProtocol_422_PROTOCOL_NOT_CONFIGURED, TestOperatorResponse_AdapterConfigSerialization
```

## STORY-089: Operator SoR Simulator

1. `make up` — verify all containers including `argus-operator-sim` transition to healthy (`docker compose ps` shows `(healthy)` next to operator-sim).
2. `curl -s http://localhost:9596/-/health | jq` — expect `{"status":"ok"}`.
3. Login to UI (http://localhost:8084) as admin → Operators → Turkcell → Protocols tab → HTTP card → click "Test Connection" → expect green success (latency_ms < 500).
4. Repeat step 3 for Vodafone_TR and Turk_Telekom.
5. In each operator's Protocols tab, verify HTTP card shows "Enabled" and health status = green.
6. `curl -s http://localhost:9596/-/metrics | grep operator_sim_requests_total` — expect non-zero counters for all 3 operators.
7. `curl -s http://localhost:8080/metrics | grep argus_operator_adapter_health_status | grep protocol=\"http\"` — expect gauge value = 1 for each operator.

---

## FIX-201: Bulk Actions Contract Fix

### Manuel test senaryosu 1 — Bulk SIM state change (sim_ids)

1. Login olup http://localhost:8084/sims sayfasina git.
2. 3 SIM sec (checkbox).
3. Sticky bulk bar gorünmeli (altta, sidebar'la cakismadan).
4. "Suspend" butonuna tikla, reason gir, onayla.
5. Secili rows'lar spinner gostermeli.
6. ~2 saniye sonra rows state'i "suspended" olmali; toast "3 processed, 0 failed".

### Manuel test senaryosu 2 — Filter-aware selection indicator

1. 5 SIM sec.
2. State filter'i degistir → bar label "5 selected (N visible, M hidden by filter)" gosterir.
3. Farkli bir filter degistir → indicator canli güncellenir.

### Manuel test senaryosu 3 — Rate limit (ayni tenant, hizli ardisik bulk)

```bash
export TOKEN="$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.access_token')"

# First request — expect 202
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8084/api/v1/sims/bulk/state-change \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"sim_ids":["00000000-0000-0000-0000-000000000001"],"target_state":"suspended"}'

# Second request ~300ms later — expect 429 RATE_LIMITED with Retry-After header
sleep 0.3
curl -s -X POST http://localhost:8084/api/v1/sims/bulk/state-change \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"sim_ids":["00000000-0000-0000-0000-000000000001"],"target_state":"active"}'
# Beklenen: {"status":"error","error":{"code":"RATE_LIMITED",...}} + Retry-After header
```

### Manuel test senaryosu 4 — Cross-tenant rejection

```bash
# Tenant A olarak login yap; Tenant B'nin bir SIM UUID'sini body'ye ekle
curl -s -X POST http://localhost:8084/api/v1/sims/bulk/state-change \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"sim_ids":["ffffffff-ffff-ffff-ffff-ffffffffffff"],"target_state":"suspended"}'
# Beklenen: 403 FORBIDDEN_CROSS_TENANT
# {"status":"error","error":{"code":"FORBIDDEN_CROSS_TENANT","details":{"violations":["ffffffff-..."]}}}
# Hic job row olusturmamali (jobs tablosunda son satiri kontrol et)
```

---

## FIX-202: SIM List & Dashboard DTO — Operator Name Resolution

### Manuel test scenaryosu 1 — SIM list operator names
1. /sims sayfasına git
2. Operator kolonu: "Turkcell (turkcell)" gibi chip görünmeli, UUID değil
3. Chip'e tıkla → /operators/:id sayfasına gitmeli
4. Orphan SIM varsa "(Unknown)" + warning icon görünmeli

### Manuel test scenaryosu 2 — Dashboard operator health
1. /dashboard git
2. Her operator kartında: kod (turkcell/vodafone_tr/turk_telekom), chip, aktif session sayısı, SLA target görünmeli
3. latency_ms ve auth_rate null → hiç gösterilmemeli (scope: FIX-203'e devredildi)

### Manuel test scenaryosu 3 — Violations list enriched
1. /violations git
2. ICCID, Operator (chip), APN, Policy (vN) kolonları doldu mu?
3. Orphan violation varsa (Unknown) fallback çalışıyor mu?

### Manuel test scenaryosu 4 — Session list
1. /sessions git
2. Her session'da operator_name, policy_name, policy_version_number görünmeli
3. Round-trip sayısı: bir sayfa = 1 session fetch + 1 GetManyByIDsEnriched (log ile kontrol)

### Manuel test scenaryosu 5 — eSIM profiles
1. eSIM profile list sayfasına git
2. OperatorChip rendering doğru mu?

## FIX-203: Dashboard Operator Health — Uptime/Latency/Activity + WS Push

### Scenario 1 — Operator simulator kill → status badge flip via WS
1. Run `docker stop argus-operator-sim` to kill the operator simulator.
2. Navigate to `/dashboard`.
3. Within 5 seconds (next health worker tick + NATS → WS relay), the affected operator row's status badge should flip from "healthy" to "down" without a page refresh.
4. Verify: no manual reload needed; WS `operator.health_changed` event patches the row in-place.

### Scenario 2 — Latency spike → sparkline update + SLA breach chip
1. Cause an operator latency jump of ≥20% (e.g. inject network delay via `tc netem` or adjust simulator response delay).
2. Watch the "Latency 1h" sparkline on the affected operator row — within two 30s ticks the rightmost bucket should reflect the elevated value.
3. If `latency_ms > 500` (default SLA threshold), a red "SLA breach" badge should appear under the operator name.
4. Verify: `auth_rate` column updates with current value; Turkcell ≥99% shows green, Vodafone 94% shows warning color, Turk Telekom 90% shows danger color.

### Scenario 3 — WS disconnect fallback polling
1. Temporarily stop the argus container or block port 8081 (`sudo pfctl` or Docker network partition) to simulate WS disconnect.
2. Dashboard should continue refreshing operator health data via 30s HTTP polling (`refetchInterval: 30_000`).
3. Restore the connection — within one poll cycle the data should be in sync; WS patch resumes once reconnected.

### Scenario 4 — Auth rate threshold colors
1. Navigate to `/dashboard` with seeded operators: Turkcell auth_rate=99.5%, Vodafone auth_rate=94%, Turk Telekom auth_rate=90%.
2. Turkcell "Auth" column: green text (≥99).
3. Vodafone "Auth" column: warning/amber text (≥95).
4. Turk Telekom "Auth" column: danger/red text (<95).
5. Verify no hardcoded hex colors — all threshold classes use design tokens.

### Scenario 5 — Sub-threshold latency suppression (no spurious event)
1. Ensure an operator has stable latency at ~200ms.
2. Cause a <10% latency change (e.g. 200ms → 210ms, 5% delta).
3. No `operator.health_changed` WS event should fire for this tick (verify via browser DevTools WS frame inspector or NATS subject monitor).
4. Dashboard row retains the stale latency value until the next 30s poll or a threshold-crossing event fires.

---

## FIX-204: Analytics group_by NULL Scan Bug + APN Orphan Sessions

### Scenario 1 — Group by APN returns 200, shows "Unassigned APN" bucket
1. `make up` — ensure Docker stack is running.
2. Navigate to `/analytics`.
3. Open the "Group by" dropdown and select "APN".
4. Verify: page does NOT crash with a 500 error. The area chart loads normally.
5. If any CDR/session rows have NULL apn_id (orphan sessions from seed data), a legend entry or chart series labeled "Unassigned APN" appears in the group breakdown.
6. Direct API check:
   ```bash
   curl -sk -H "Authorization: Bearer $TOKEN" \
     'http://localhost:8084/api/v1/analytics/usage?group_by=apn&period=24h' \
     | jq '.data.time_series[0]'
   ```
   Response must be HTTP 200; if an unassigned bucket exists, `group_key` is "Unassigned APN" (not raw `__unassigned__` or null).

### Scenario 2 — Group by Operator shows "Unknown Operator" for orphan sessions
1. On the Analytics page, select "Group by: Operator".
2. Verify page loads without error.
3. If any sessions have NULL operator_id, a series labeled "Unknown Operator" appears in chart + legend.
4. All other series show the resolved operator name (not a UUID).

### Scenario 3 — Group by RAT Type shows "Unknown RAT" for null rows
1. Select "Group by: RAT Type".
2. Verify page loads; if null rat_type rows exist a series labeled "Unknown RAT" appears.
3. Real RAT types (nb_iot, lte_m, lte, nr_5g) display their raw identifiers (handler does not translate rat_type — FE resolveGroupLabel is the sole translation point).

### Scenario 4 — Orphan session detector logs at boot + interval
1. Run `docker logs argus 2>&1 | grep orphan` immediately after `make up`.
2. Look for a startup log line: `orphan session detector started` with `interval=30m0s`.
3. Wait for one detector tick (or set `ORPHAN_SESSION_CHECK_INTERVAL=1m` in `.env` and restart):
   - If orphan sessions exist: log line `orphan sessions detected — active sessions with NULL apn_id` with `tenant_id` and `count`.
   - If no orphans: no warning log; detector runs silently.
4. Verify graceful shutdown: run `docker stop argus` and check logs for `orphan session detector stopped`.

---

## FIX-205: Token Refresh Auto-retry on 401

Prerequisite: `make up` running; log in as admin@argus.io; Chrome/Firefox DevTools open on Network tab.

### Scenario 1 — Single-flight (AC-3): Two concurrent 401s trigger exactly 1 refresh

1. In DevTools Console, paste:
   ```js
   const s = JSON.parse(localStorage.getItem('argus-auth'))
   s.state.token = 'expired.token.value'
   localStorage.setItem('argus-auth', JSON.stringify(s))
   ```
2. Reload the page (Zustand rehydrates the expired token).
3. Navigate to Dashboard (`/`) which fires multiple API calls on mount.
4. In Network tab filter by `auth/refresh`.

Expected: Exactly **one** POST to `/api/v1/auth/refresh`. All original failing requests complete successfully (status 200 after retry).

### Scenario 2 — Redirect on refresh failure (AC-4)

1. Navigate to `/sims?filter=active`.
2. Block `/api/v1/auth/refresh` in DevTools (right-click → Block request URL) OR stop the backend (`make down`).
3. Force-expire the token (Scenario 1, step 1), reload.
4. Wait for the page to 401 → attempt refresh → refresh also fails.

Expected: Browser navigates to `/login?reason=session_expired&return_to=%2Fsims%3Ffilter%3Dactive`. URL contains both `reason=session_expired` and the URL-encoded original path+query.

### Scenario 3 — Loop guard (Risk 1): Refresh 401 does not cause infinite retry

1. With refresh endpoint blocked (same as Scenario 2), trigger a 401.

Expected: Network tab shows exactly **one** POST to `/api/v1/auth/refresh`. No second attempt fires. Browser redirects to `/login?reason=session_expired&...` once. Reason: refresh call uses bare `axios` (not the `api` instance) so the response interceptor cannot re-enter; the `_retry` flag provides a second guard.

### Scenario 4 — Pre-emptive scheduler (AC-5): Refresh fires 5 minutes before expiry

1. In DevTools Console:
   ```js
   const s = JSON.parse(localStorage.getItem('argus-auth'))
   const expiresAt = s.state.tokenExpiresAt
   const fiveMinBefore = new Date(expiresAt - 5 * 60 * 1000)
   console.log('Preemptive refresh fires at:', fiveMinBefore.toISOString())
   console.log('That is in:', Math.round((fiveMinBefore - Date.now()) / 1000 / 60), 'minutes')
   ```
2. To verify the timer fires: simulate a 6-minute expiry:
   ```js
   import('/src/stores/auth.js').then(m => {
     m.useAuthStore.getState().setTokenExpiresAt(Date.now() + 6 * 60 * 1000)
   })
   ```
3. Wait ~1 minute.

Expected: A POST to `/api/v1/auth/refresh` fires automatically after ~1 minute (5 min before the 6-min expiry). No user interaction required. No spinner/UI flash.

### Scenario 5 — BroadcastChannel cross-tab sync (Risk 2)

1. Open http://localhost:8084 in **two separate tabs** (Tab A and Tab B), both logged in.
2. In Tab A's Console:
   ```js
   const ch = new BroadcastChannel('argus-auth-broadcast')
   ch.postMessage({
     type: 'token_refreshed',
     token: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0IiwiZXhwIjo5OTk5OTk5OTk5fQ.test',
     expiresAt: Date.now() + 60 * 60 * 1000
   })
   ```
3. Switch to Tab B's Console:
   ```js
   JSON.parse(localStorage.getItem('argus-auth')).state.token
   ```

Expected: Tab B's localStorage shows the new token value. Tab B did not call the refresh endpoint — it received the update via BroadcastChannel.

### Scenario 6 — Rate limit (AC-8): 60 req/min per session returns 429 on 61st

```bash
TOKEN=$(curl -s -c /tmp/argus_cookies.txt -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')

for i in $(seq 1 61); do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" -b /tmp/argus_cookies.txt \
    -X POST http://localhost:8084/api/v1/auth/refresh)
  echo "Request $i: $STATUS"
done
```

Expected: Requests 1–60 return 200. Request 61 returns 429 with `{"error":{"code":"RATE_LIMITED",...}}`.

---

## FIX-206: Orphan Operator IDs + FK Constraints + Seed Fix

Backend + migration story. No UI surface changes; manual verification is DB-level.

### Scenario 1 — AC-4: Fresh-volume `make db-seed` produces zero orphan references

```bash
# Fresh volume: drop + recreate argus DB, then migrate+seed.
docker exec argus-postgres psql -U argus -d postgres -c "
  SELECT pg_terminate_backend(pid) FROM pg_stat_activity
   WHERE datname='argus' AND pid <> pg_backend_pid();
  DROP DATABASE IF EXISTS argus;
  CREATE DATABASE argus OWNER argus;"
docker compose -f deploy/docker-compose.yml exec argus /app/argus migrate up
docker compose -f deploy/docker-compose.yml exec argus /app/argus seed

# Verify zero orphans across operator / apn / ip_address.
docker exec argus-postgres psql -U argus -d argus -c "
  SELECT
    (SELECT COUNT(*) FROM sims s WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id)) AS orphan_operator,
    (SELECT COUNT(*) FROM sims s WHERE s.apn_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM apns a WHERE a.id = s.apn_id)) AS orphan_apn,
    (SELECT COUNT(*) FROM sims s WHERE s.ip_address_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM ip_addresses i WHERE i.id = s.ip_address_id)) AS orphan_ip;"
```

Expected: all three counts = 0. Total SIM count > 0 (seeds did run).

### Scenario 2 — AC-2/AC-3: Three FK constraints installed on `sims`

```bash
docker exec argus-postgres psql -U argus -d argus -c "
  SELECT conname, pg_get_constraintdef(oid)
  FROM pg_constraint
  WHERE conrelid='sims'::regclass AND contype='f'
    AND conname LIKE 'fk_sims_%'
  ORDER BY conname;"
```

Expected (3 rows):
- `fk_sims_apn | FOREIGN KEY (apn_id) REFERENCES apns(id) ON DELETE SET NULL`
- `fk_sims_ip_address | FOREIGN KEY (ip_address_id) REFERENCES ip_addresses(id) ON DELETE SET NULL`
- `fk_sims_operator | FOREIGN KEY (operator_id) REFERENCES operators(id) ON DELETE RESTRICT`

### Scenario 3 — AC-7: FK violation surfaces as HTTP 400 `INVALID_REFERENCE`

```bash
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')

# The handler-layer 404 is the primary path. To hit the FK-violation 400,
# we need a request whose operator_id is UUID-parseable and passes the
# operator GetByID check but then vanishes. In normal operation this is
# a race. For manual repro, use an unused-but-plausible UUID — response
# is 404 NOT_FOUND (primary), confirming the path works end-to-end.
curl -s -X POST http://localhost:8084/api/v1/sims \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"operator_id":"99999999-9999-9999-9999-999999999999",
       "apn_id":"00000000-0000-0000-0000-000000000301",
       "iccid":"8990286010FIX206TEST001","imsi":"28601FIX206T1",
       "sim_type":"physical"}' | jq .
```

Expected: 404 response body `{"status":"error","error":{"code":"NOT_FOUND","message":"Operator not found"}}` — the handler-layer check catches the bogus operator_id before the store layer.

Defensive FK path (race-only — validated by `TestFIX206_SIMCreate_FKViolations` integration test with 2 sub-tests) returns HTTP 400 with `code: "INVALID_REFERENCE"` and a field hint pointing at the offending column.

### Scenario 4 — AC-2: `ON DELETE RESTRICT` blocks operator delete while SIMs exist

```bash
# Should fail — Turkcell has ~133 SIMs referencing it after seed.
docker exec argus-postgres psql -U argus -d argus -c "
  DELETE FROM operators WHERE id = '20000000-0000-0000-0000-000000000001';"
```

Expected: `ERROR: update or delete on table "operators" violates foreign key constraint "fk_sims_operator" on table "sims_turkcell"` (or sibling partition). Cleanup path for operator removal is "reassign SIMs via bulk operator-switch (FIX-201), then delete operator".

---

## FIX-207: Session/CDR Data Integrity

Backend + migration story. No UI surface changes; manual verification is DB-level and metric-level.

### Scenario 1 — AC-4: IMSI format reject (API + RADIUS)

**REST path:**

```bash
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')

# Malformed IMSI (13 digits — too short)
curl -s -X POST http://localhost:8084/api/v1/sims \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"operator_id":"20000000-0000-0000-0000-000000000001",
       "iccid":"8990286010FIX207TEST001","imsi":"1234567890123",
       "sim_type":"physical"}' | jq .
```

Expected: HTTP 400 with `{"status":"error","error":{"code":"INVALID_IMSI_FORMAT","message":"IMSI must be 14–15 digits (MCC+MNC+MSIN)"}}`.

**RADIUS path** (when `IMSI_STRICT_VALIDATION=true`):

```bash
# Send Access-Request with a malformed IMSI (16 digits) via radclient.
# Requires radclient installed and RADIUS listener on :1812.
echo "User-Name = '1234567890123456', User-Password = 'test'" | \
  radclient -x localhost:1812 auth testing123
```

Expected: `Access-Reject` with Reply-Message attribute indicating IMSI format violation.

### Scenario 2 — AC-1/AC-2: CHECK constraint probe via psql

```bash
# Attempt to insert a session with ended_at before started_at.
docker exec argus-postgres psql -U argus -d argus -c "
  INSERT INTO sessions (id, tenant_id, sim_id, operator_id, started_at, ended_at)
  VALUES (gen_random_uuid(),
          '10000000-0000-0000-0000-000000000001',
          (SELECT id FROM sims LIMIT 1),
          '20000000-0000-0000-0000-000000000001',
          NOW(), NOW() - INTERVAL '1 hour');"

# Attempt to insert a CDR with negative duration_sec.
docker exec argus-postgres psql -U argus -d argus -c "
  INSERT INTO cdrs (id, session_id, tenant_id, timestamp, duration_sec, bytes_in, bytes_out)
  VALUES (gen_random_uuid(),
          (SELECT id FROM sessions LIMIT 1),
          '10000000-0000-0000-0000-000000000001',
          NOW(), -5, 0, 0);"
```

Expected: Both INSERTs fail with `ERROR: new row for relation ... violates check constraint` — SQLSTATE 23514. Constraint names: `chk_sessions_ended_after_started` and `chk_cdrs_duration_nonneg`.

### Scenario 3 — AC-5: Daily data-integrity scan job + metric assertion

```bash
# Check metric baseline before triggering.
curl -s http://localhost:9596/metrics | grep argus_data_integrity_violations_total

# Trigger job manually via admin API (or wait for cron at 03:17 UTC).
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')

curl -s -X POST http://localhost:8084/api/v1/admin/jobs/data_integrity/trigger \
  -H "Authorization: Bearer $TOKEN" | jq .
```

Expected: After trigger, `argus_data_integrity_violations_total{kind="neg_duration_session"}`, `{kind="neg_duration_cdr"}`, `{kind="framed_ip_outside_pool"}`, and `{kind="imsi_malformed"}` counters are visible in `/metrics`. If dev DB has no violations, all counters remain at 0 (no new increments) — job still completes and returns a result with `counts` map populated (all zeros).

### Scenario 4 — AC-7: NAS-IP missing counter probe

```bash
# Send an Accounting-Start packet WITHOUT NAS-IP-Address AVP.
# This requires radclient or a test script that omits attribute 4.
echo "User-Name = '234100000000001', Acct-Status-Type = Start, Acct-Session-Id = 'test-001'" | \
  radclient -x localhost:1813 acct testing123

# Check the counter incremented.
curl -s http://localhost:9596/metrics | grep argus_radius_nas_ip_missing_total
```

Expected: `argus_radius_nas_ip_missing_total` counter increments by 1. WARN log line visible: `"nas_ip_missing": true` with `acct_session_id` context. When NAS-IP-Address AVP IS present, counter does not increment.

---

## FIX-208: Cross-Tab Data Aggregation Unify

Backend aggregation story. No UI surface changes; manual verification is API-level and metric-level.

### Scenario 1 — AC-4: Aggregator cross-tab consistency (F-125)

Verify that Dashboard, Operator detail, APN detail, and Policy list all show the same SIM count for the same policy.

```bash
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')

# Step 1 — get a policy ID from the policy list
POLICY_ID=$(curl -s http://localhost:8084/api/v1/policies \
  -H "Authorization: Bearer $TOKEN" | jq -r '.data[0].id')

# Step 2 — SIM count from Policy list endpoint (SimCount field)
curl -s "http://localhost:8084/api/v1/policies?page_size=10" \
  -H "Authorization: Bearer $TOKEN" | jq ".data[] | select(.id==\"$POLICY_ID\") | .sim_count"

# Step 3 — SIM count from Dashboard (sim_count_by_state total — all non-purged)
curl -s http://localhost:8084/api/v1/dashboard \
  -H "Authorization: Bearer $TOKEN" | jq '.data.sim_counts | to_entries | map(.value) | add'

# Step 4 — SIM count from Operator detail (sim_count in operator list)
OPERATOR_ID=$(curl -s http://localhost:8084/api/v1/operators \
  -H "Authorization: Bearer $TOKEN" | jq -r '.data[0].id')
curl -s "http://localhost:8084/api/v1/operators/$OPERATOR_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '.data.sim_count'
```

Expected: All surfaces that report SIMs for the same policy use `sims.policy_version_id IN (SELECT id FROM policy_versions WHERE policy_id = $2) AND state != 'purged'` as the canonical source (via `internal/analytics/aggregates` facade). The policy-level `sim_count` returned by Step 2 must be non-zero if the policy has active SIMs in seed — not 0 (which was the F-125 regression: direct `policy_version_id = policy.id` comparison returned 0 rows because the UUID spaces differ). After FIX-208, all four surfaces go through `aggSvc.SIMCountByPolicy` / `aggSvc.SIMCountByOperator` / `aggSvc.SIMCountByTenant` — divergent numbers across tabs must not occur.

### Scenario 2 — AC-3: NATS cache invalidation on sim.updated

Verify that the aggregates Redis cache is evicted when a SIM is updated.

```bash
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')

# Step 1 — warm the cache: hit dashboard (forces SIMCountByTenant into Redis)
curl -s http://localhost:8084/api/v1/dashboard \
  -H "Authorization: Bearer $TOKEN" | jq '.data.total_sims'

# Step 2 — verify cache key exists in Redis (key pattern: argus:aggregates:v1:<tenant>:*)
docker exec argus-redis redis-cli KEYS 'argus:aggregates:v1:*' | head -5

# Step 3 — trigger a SIM state change (suspend → resume any active SIM)
SIM_ID=$(curl -s "http://localhost:8084/api/v1/sims?state=active&page_size=1" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.data[0].id')

curl -s -X POST "http://localhost:8084/api/v1/sims/$SIM_ID/suspend" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"reason":"cache-invalidation-test"}' | jq .status

# Step 4 — within ~1s the NATS invalidator (queue=aggregates-invalidator, SubjectSIMUpdated)
# should have evicted all tenant-scoped aggregate keys.
sleep 1
docker exec argus-redis redis-cli KEYS 'argus:aggregates:v1:*' | wc -l

# Step 5 — restore SIM state
curl -s -X POST "http://localhost:8084/api/v1/sims/$SIM_ID/activate" \
  -H "Authorization: Bearer $TOKEN" | jq .status
```

Expected: After Step 2, one or more `argus:aggregates:v1:<tenant_id>:*` keys are present (cache is warm). After Step 4, those keys are absent (evicted by NATS invalidator). On the next dashboard load the cache re-warms. If Step 4 still shows keys, wait 60s for TTL expiry — TTL is the safety net when NATS delivery is delayed.

### Scenario 3 — AC-6: Prometheus aggregates cache hit/miss metrics

Verify that the three aggregates Prometheus counters are visible and increment.

```bash
# Step 1 — baseline: check current hit/miss counts
curl -s http://localhost:9596/metrics | grep 'argus_aggregates_cache'

# Step 2 — warm the cache (first call = miss)
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')
curl -s http://localhost:8084/api/v1/dashboard -H "Authorization: Bearer $TOKEN" > /dev/null

# Step 3 — cache miss counter should have incremented
curl -s http://localhost:9596/metrics | grep 'argus_aggregates_cache_misses_total'

# Step 4 — call dashboard again within 60s (cache hit)
curl -s http://localhost:8084/api/v1/dashboard -H "Authorization: Bearer $TOKEN" > /dev/null

# Step 5 — cache hit counter should have incremented; duration histogram present
curl -s http://localhost:9596/metrics | grep 'argus_aggregates_cache_hits_total'
curl -s http://localhost:9596/metrics | grep 'argus_aggregates_call_duration_seconds'
```

Expected: Three metric families visible after Step 1 warm-up (may be absent before any aggregator call — Prometheus counters are registered but start at 0):
- `argus_aggregates_cache_misses_total{method="SIMCountByTenant"}` increments on first cold call (Step 2).
- `argus_aggregates_cache_hits_total{method="SIMCountByTenant"}` increments on the second call within TTL window (Step 4).
- `argus_aggregates_call_duration_seconds{method="SIMCountByTenant",cache="miss"}` and `{cache="hit"}` histogram buckets present.
p95 latency on cache hit should be in the µs range (gate measured 72µs), well under the 50ms AC-6 target.

---

## FIX-209: Unified `alerts` Table + Operator/Infra Alert Persistence

Bu story hem backend (unified `alerts` table, AlertStore, 3 API endpoints, retention job, notification subscriber refactor) hem de frontend (alerts list/detail pages, dashboard Recent Alerts panel) kapsamaktadir.

### Senaryo 1 — Alerts list: Source filtresi + geçersiz değer validasyonu (AC-3/AC-6)

`GET /api/v1/alerts?source=` filtresi yalnizca eslesme kaynaktaki satirlari dondurmeli; geçersiz source degeri 400 VALIDATION_ERROR dondurmeli.

```bash
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')

# Step 1 — operator source filtresi
curl -s "http://localhost:8084/api/v1/alerts?source=operator" \
  -H "Authorization: Bearer $TOKEN" | jq '{count: (.data | length), sources: [.data[].source] | unique}'
```

Beklenti: `sources` dizisi yalnizca `["operator"]` içermeli (bos dizi de kabul edilir — seed verisinde henüz operator alert olmayabilir).

```bash
# Step 2 — sim source filtresi
curl -s "http://localhost:8084/api/v1/alerts?source=sim" \
  -H "Authorization: Bearer $TOKEN" | jq '{count: (.data | length), sources: [.data[].source] | unique}'
```

Beklenti: `sources` dizisi yalnizca `["sim"]` içermeli.

```bash
# Step 3 — geçersiz source degeri 400 VALIDATION_ERROR dondurmeli
curl -s "http://localhost:8084/api/v1/alerts?source=unknown_source" \
  -H "Authorization: Bearer $TOKEN" | jq '{status: .status, code: .error.code}'
```

Beklenti: `{"status": "error", "code": "VALIDATION_ERROR"}` — HTTP 400.

```bash
# Step 4 — infra source filtresi
curl -s "http://localhost:8084/api/v1/alerts?source=infra" \
  -H "Authorization: Bearer $TOKEN" | jq '{count: (.data | length), sources: [.data[].source] | unique}'
```

Beklenti: `sources` dizisi yalnizca `["infra"]` içermeli (bos dizi de kabul edilir).

### Senaryo 2 — Alert detail: ack/resolve geçişleri + Escalate görünürlüğü (AC-7/AC-8)

PATCH endpoint ile durum geçişleri çalismali; Escalate butonu yalnizca `source=sim` alertlerde görünmeli.

```bash
# Step 1 — open durumdaki bir alert al
ALERT_ID=$(curl -s "http://localhost:8084/api/v1/alerts?state=open&limit=1" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.data[0].id')

echo "Alert ID: $ALERT_ID"

# Step 2 — open → acknowledged geçişi
curl -s -X PATCH "http://localhost:8084/api/v1/alerts/$ALERT_ID" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"state":"acknowledged"}' | jq '{status: .status, state: .data.state}'
```

Beklenti: `{"status": "success", "state": "acknowledged"}`.

```bash
# Step 3 — acknowledged → resolved geçişi
curl -s -X PATCH "http://localhost:8084/api/v1/alerts/$ALERT_ID" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"state":"resolved"}' | jq '{status: .status, state: .data.state}'
```

Beklenti: `{"status": "success", "state": "resolved"}`.

```bash
# Step 4 — resolved → open geçişi geçersiz (409 INVALID_STATE_TRANSITION donmeli)
curl -s -X PATCH "http://localhost:8084/api/v1/alerts/$ALERT_ID" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"state":"open"}' | jq '{status: .status, code: .error.code}'
```

Beklenti: `{"status": "error", "code": "INVALID_STATE_TRANSITION"}` — HTTP 409.

**UI Dogrulamasi** (tarayici):
1. http://localhost:8084/alerts sayfasinda bir alert satiri tikla → detail sayfasi açilmali (SCR-172).
2. `source=sim` ve `meta.anomaly_id` olan alert için Escalate butonu görünmeli; `source=operator` alert için Escalate butonu GIZLI olmali.
3. Source değeri `operator` olan alertlerde Related Anomaly baglantisi görünmemeli.

### Senaryo 3 — Dashboard Recent Alerts paneli + Source chip (AC-10)

Dashboard ana sayfasinda Recent Alerts paneli görünmeli; her satir severity badge + Source chip içermeli; tiklandiginda alert detail sayfasina yönlendirmeli.

```bash
# Step 1 — dashboard endpoint'i çagir
curl -s "http://localhost:8084/api/v1/dashboard" \
  -H "Authorization: Bearer $TOKEN" | jq '{recent_alerts_count: (.data.recent_alerts | length), first_source: .data.recent_alerts[0].source}'
```

Beklenti: `recent_alerts` dizisi 0-10 eleman içermeli; her eleman `source` alani içermeli (sim/operator/infra/policy/system).

**UI Dogrulamasi** (tarayici):
1. http://localhost:8084 dashboard açildiginda Recent Alerts paneli görünmeli (AlertFeed bileşeni).
2. Her alert satirinda severity badge'inin yaninda Source chip (örn. "operator", "sim") görünmeli.
3. Bir alert satirina tiklayinca `/alerts/{id}` detay sayfasina yönlendirmeli.
4. Recent Alerts paneli bos oldugunda "No recent alerts" bos durum mesaji görünmeli.

### Senaryo 4 — Retention job gözlemi (Config: ALERTS_RETENTION_DAYS)

Bu senaryo observation (çalişma zamaninda dogrulanamaz; ayar ve cron kaydini dogrular).

```bash
# Step 1 — env var dokümantasyonu
grep 'ALERTS_RETENTION_DAYS' /path/to/argus/.env.example
```

Beklenti: `.env.example` dosyasinda `ALERTS_RETENTION_DAYS=180` satiri mevcut olmali.

```bash
# Step 2 — cron job kaydi
grep -n 'alerts_retention\|03:15\|AlertsRetention' internal/job/ -r
```

Beklenti: `internal/job/alerts_retention.go` dosyasinda `03:15 UTC` cron pattern ve `DeleteOlderThan` çagrisi mevcut olmali.

```bash
# Step 3 — CONFIG.md dokümantasyonu
grep 'ALERTS_RETENTION_DAYS' docs/architecture/CONFIG.md
```

Beklenti: `ALERTS_RETENTION_DAYS` satiri mevcut, default `180`, min `30` olarak belirtilmeli.

---

## FIX-210: Alert Deduplication + State Machine (Edge-triggered, Cooldown)

Bu story birincil olarak backend + veritabani degisikligidir (dedup, cooldown, edge-trigger). Ana UI degisiklikleri: Alerts listesinde "N× in last Xm" badge + Alerts detail panelinde First/Last seen + cooldown banner.

### Senaryo 1 — Dedup badge (AC-6): ayni alert 3+ kez tetiklenince tek satir goster

```bash
# Step 1 — simülatör ile 5 adet ayni operator-health eventi tetikle
# (ayni operator, ayni tip — örn. degraded probe)
# Ortam: make up

# Operator health_worker'i otomatik tetikler. Alternatif: simulator ile
# birden fazla kez operator'u unavailable yap (SoR endpoint kapatilabilir)
curl -s "http://localhost:8084/api/v1/alerts" \
  -H "Authorization: Bearer $TOKEN" | \
  jq '[.data[] | {id, type, occurrence_count, dedup_key}] | first'
```

Beklenti: Ayni kaynak/tip/entity icin tek alert satiri, `occurrence_count` 2 veya daha buyuk deger.

```bash
# Step 2 — Alerts listesi badge'i kontrol et (UI)
# http://localhost:8084/alerts adresini tarayicide ac
# occurrence_count > 1 olan satirda "N× in last Xm" badge gorulmeli (Repeat ikonu ile)
# occurrence_count == 1 olan satirlarda badge gorulmemeli
```

Beklenti: Dedup badge yalnizca `occurrence_count > 1` satirlarda gozukur; uppercase/tracking-wide stili yoktur; Repeat ikonu badge'in solunda gorunur.

### Senaryo 2 — Alert detail: Occurrence bilgisi + cooldown banner

```bash
# Step 1 — Occurrence bilgisi olan bir alert'in detayini al
ALERT_ID=$(curl -s "http://localhost:8084/api/v1/alerts" \
  -H "Authorization: Bearer $TOKEN" | \
  jq -r '[.data[] | select(.occurrence_count > 1)] | first | .id')

curl -s "http://localhost:8084/api/v1/alerts/$ALERT_ID" \
  -H "Authorization: Bearer $TOKEN" | \
  jq '{id, occurrence_count, first_seen_at, last_seen_at, cooldown_until, state}'
```

Beklenti: `first_seen_at`, `last_seen_at`, `occurrence_count`, `cooldown_until` alanlari JSON'da mevcut. `first_seen_at <= last_seen_at`.

```bash
# Step 2 — Alert'i resolve et (cooldown_until set edilmeli)
curl -s -X PATCH "http://localhost:8084/api/v1/alerts/$ALERT_ID" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"state":"resolved"}' | jq '{state: .data.state, cooldown_until: .data.cooldown_until}'
```

Beklenti: `state: "resolved"`, `cooldown_until` simdi + yaklasik 5 dakika ilerisini gostermeli (bos olmamali).

```bash
# Step 3 — Detail sayfasinda cooldown banner gorunumunu kontrol et (UI)
# http://localhost:8084/alerts/<ALERT_ID> adresini tarayicide ac
# state=resolved + cooldown aktifken: BellOff ikonu + sol accent stripe + "Cooldown active until HH:MM" metni gorunmeli
```

Beklenti: Cooldown banner'i `border-l-2 border-l-accent/60` stilinde, `BellOff` ikonuyla sol tarafta gorulur.

### Senaryo 3 — Publisher edge-trigger: ayni durum iki kez → tek alert publish

```bash
# Step 1 — Operator health worker'in edge-trigger davranisini dogrula
# Ayni operator'a iki kez ayni status dondurmek alinip yeni alert olusturmamali
# Prometheus metrigi: argus_alerts_rate_limited_publishes_total{publisher="enforcer"}
curl -s "http://localhost:8084/metrics" | grep 'argus_alerts_rate_limited_publishes_total'
```

Beklenti: Metrik mevcut (sifir veya daha buyuk deger). Policy enforcer icin 60s aralikta tekrar tetikleme yapilmamali.

```bash
# Step 2 — Operator health worker edge-trigger dogrulama
# Ayni operator saglikli durumdayken iki kez probe — yalnizca bir alert olusturmali
# Prometheus metrigi kontrol et
curl -s "http://localhost:8084/metrics" | grep 'argus_alerts_deduplicated_total'
```

Beklenti: `argus_alerts_deduplicated_total` metrigi mevcut; tekrarlayan problar yalnizca bir satira donusmeli.

### Senaryo 4 — Cooldown via REST: PATCH resolve → cooldown_until set + yeni event drop

```bash
# Step 1 — Resolve edilmis alert ile ayni dedup_key ile yeni event gelmesi halinde drop edilmeli
ALERT_ID=$(curl -s "http://localhost:8084/api/v1/alerts" \
  -H "Authorization: Bearer $TOKEN" | \
  jq -r '[.data[] | select(.state == "resolved" and .cooldown_until != null)] | first | .id')

# Step 2 — cooldown drop metrigini kontrol et
curl -s "http://localhost:8084/metrics" | grep 'argus_alerts_cooldown_dropped_total'
```

Beklenti: `argus_alerts_cooldown_dropped_total` metrigi mevcut. Cooldown suresi icinde ayni `dedup_key` ile gelen event yeni alert satiri OLUSTURMAZ, metrik artar.

```bash
# Step 3 — ALERT_COOLDOWN_MINUTES konfigurasyonu dogrula
grep 'ALERT_COOLDOWN_MINUTES' .env.example docs/architecture/CONFIG.md
```

Beklenti: `.env.example` ve `CONFIG.md` icinde `ALERT_COOLDOWN_MINUTES=5` mevcut, default 5 olarak belirtilmis.

### Senaryo 5 — Suppressed state: SuppressAlert yolu (PATCH /alerts/{id} ile DEGIL)

```bash
# Step 1 — PATCH ile suppressed set etme 400/409 hatasi vermeli (API contract preserved)
ALERT_ID=$(curl -s "http://localhost:8084/api/v1/alerts" \
  -H "Authorization: Bearer $TOKEN" | \
  jq -r '[.data[] | select(.state == "open")] | first | .id')

curl -s -X PATCH "http://localhost:8084/api/v1/alerts/$ALERT_ID" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"state":"suppressed"}' | jq '{status, code: .error.code}'
```

Beklenti: HTTP 409, `code: "INVALID_STATE_TRANSITION"` — suppressed durumu PATCH endpoint'i ile ayarlanamaz; yalnizca store-level `SuppressAlert` methodu ile kullanilir (admin/dedup yolu).

```bash
# Step 2 — Suppressed alert detail sayfasi (UI)
# Eger dedup ile suppressed bir alert varsa http://localhost:8084/alerts/<ID>
# Durum pill'i muted/neutral renkte gozukmeli (alarming kirmizi degil)
# meta.suppress_reason mevcutsa detayda gorulecek
```

Beklenti: Suppressed state, turuncu/kirmizi degil, muted/neutral tonunda gorunur. `meta.suppress_reason` varsa goruntulenir.

---

## FIX-211: Severity Taxonomy Unification

Bu story birincil olarak backend + frontend altyapi degisikligidir (5-degerli kanonik taxonomy: info/low/medium/high/critical). Ana UI degisiklikleri: Alerts/Violations/Notifications sayfalarinda 5 severity secenegi + eski "warning"/"error" degerlerini reddeden 400 dogrulama.

### Senaryo 1 — Alerts sayfasi severity filtresi (AC-4 + AC-5)

Alerts sayfasinda severity filter dropdown'inin 5 deger gosterdigi ve "medium" filtresinin dogru satirlari dondurdugu dogrulanir.

```bash
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq -r '.data.token')

# Step 1 — medium severity filtresiyle anomali listesi (alerts page kaynagi)
curl -s "http://localhost:8084/api/v1/anomalies?severity=medium" \
  -H "Authorization: Bearer $TOKEN" | jq '{count: .meta.total, first_severity: .data[0].severity}'
```

Beklenti: `severity: "medium"` olan satirlar donmeli. `.meta.total` sifirdan buyuk olmali (seed verisi medium-severity anomaliler iceriyor). Hicbir satirda `severity: "warning"` gozukmemeli.

```bash
# Step 2 — eski "warning" degeri 400 INVALID_SEVERITY dondurmeli
curl -s "http://localhost:8084/api/v1/anomalies?severity=warning" \
  -H "Authorization: Bearer $TOKEN" | jq '{status: .status, code: .error.code}'
```

Beklenti: `{"status": "error", "code": "INVALID_SEVERITY"}` — HTTP 400.

```bash
# Step 3 — violations endpoint ayni dogrulamayi yapmali
curl -s "http://localhost:8084/api/v1/policy-violations?severity=error" \
  -H "Authorization: Bearer $TOKEN" | jq '{status: .status, code: .error.code}'
```

Beklenti: `{"status": "error", "code": "INVALID_SEVERITY"}` — HTTP 400.

### Senaryo 2 — Notification Preferences severity threshold (AC-8)

Notification preferences panelinde severity threshold dropdown'inin 5 deger gosterdigi ve "medium" ayarinin info+low eventleri bastirdigini dogrulanir.

```bash
# Step 1 — mevcut preferences'i al
curl -s "http://localhost:8084/api/v1/notifications/preferences" \
  -H "Authorization: Bearer $TOKEN" | jq '.data.severity_threshold'
```

Beklenti: Kayitli deger canonical (info/low/medium/high/critical) olmali.

```bash
# Step 2 — severity_threshold'u "medium" olarak ayarla
curl -s -X PUT "http://localhost:8084/api/v1/notifications/preferences" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"severity_threshold":"medium","email_enabled":true}' | jq '{status: .status, threshold: .data.severity_threshold}'
```

Beklenti: `{"status": "ok", "threshold": "medium"}`.

```bash
# Step 3 — eski "warning" degeri 400 dondurmeli
curl -s -X PUT "http://localhost:8084/api/v1/notifications/preferences" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"severity_threshold":"warning"}' | jq '{status: .status, code: .error.code}'
```

Beklenti: `{"status": "error", "code": "INVALID_SEVERITY"}` — HTTP 400 (onceden 422 VALIDATION_ERROR idi, FIX-211 ile 400'e degistirildi).

**UI Dogrulamasi** (tarayici):
1. http://localhost:8084/alerts sayfasini ac — severity filter dropdown'inda 5 deger (Critical, High, Medium, Low, Info) gorulmeli.
2. "Medium" seciminde sayfa medium satirlari filtrelemeli; badge renkleri token-tabanli olmali (sari/warning-dim).
3. http://localhost:8084/notifications/preferences — severity threshold select'inde 5 deger gorulmeli, default "Info".
4. http://localhost:8084/violations — severity sutunu "medium"/"high" badge'leri gostermeli; "warning"/"error" badge'i gozukmemeli.

## FIX-212: Unified Event Envelope + Name Resolution + Missing Publishers

Bu story icin manuel test senaryosu yok (backend/altyapi — NATS event envelope migration + publisher wiring, FE tarafindan WS hub uzerinden passthrough). Otomasyonla dogrulanabilir:

```bash
# 1. Event catalog endpoint — envelope contract verification
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/events/catalog | jq '.[0] | keys'
# Beklenti: ["default_severity","entity_type","meta_schema","source","subject","type"]

# 2. Event envelope shape via WS — after triggering a SIM state change, verify envelope fields:
#    entity.display_name should be ICCID (not UUID), event_version=1, tenant_id non-null
# Use wscat or browser DevTools WS panel: ws://localhost:8081/ws/v1/events?token=<JWT>

# 3. Legacy shape metric — should be zero for all 14 in-scope subjects:
curl -s http://localhost:8080/metrics | grep argus_events_legacy_shape_total
# Beklenti: metric absent or 0 for session.started, sim.state_changed, operator.health_changed, etc.

# 4. Infra-global publisher tenant_id (SystemTenantID) — trigger NATS consumer lag alert:
#    Observe alert in /alerts with tenant_id = 00000000-0000-0000-0000-000000000001 (SystemTenantID)
```

## FIX-213: Live Event Stream UX — Filter Chips, Usage Display, Alert Body

### 1. Filter Chips — Type

1. Paneli ac (dashboard topbar "Activity" butonu).
2. Filtre barinda `Tür` chipine tikla — popover acilar.
3. 2 event tipi sec (orn. `session.started`, `alert.triggered`).
4. Chip label `Tür (2)` olur; event listesi yalnizca bu tipleri gosterir.
5. Popover `Temizle` → chip label `Tür` olur, liste dolar.

### 2. Filter Chips — Severity Pill (inline)

1. Severity rowunda `HIGH` ve `CRITICAL` pillere tikla (toggle on).
2. Liste yalnizca high/critical severity eventleri gosterir.
3. Pillleri tekrar tikla → deselect, liste genisler.
4. Mobil viewport (<768px): her pill 1 harf (`C/H/M/L/I`); md+ viewportta 3 harf (`CRI/HIG/MED/LOW/INF`).

### 3. Filter Chips — Entity Type ve Source

1. `Varlık` chipine tikla — `sim`, `operator`, `apn`, `policy` gibi entity type setenekleri gorunur.
2. `Kaynak` chipine tikla — `aaa`, `operator`, `policy`, `system` gibi source setenekleri gorunur.
3. Her filter secimi sonrasinda event listesinin dogru sekilde filtrelendigini dogrula.

### 4. Filter Chips — Date Range

1. `Zaman` chipine tikla — `Bu oturum`, `Son 1 saat`, `Son 24 saat` secenekleri gorunur.
2. `Son 1 saat` sec — yalnizca son 60 dakikaya ait eventler gozukur.
3. Filtreyi `Bu oturum` olarak resetle.

### 5. Filter Kaliciligi (localStorage)

1. Filtre sec (orn. Tür = `alert.triggered`).
2. Paneli kapat, tekrar ac.
3. Secilen filtre hala aktif olmali (localStorage `argus.events.filters.v1` kaliciligi).
4. `Temizle` butonuna bas — eventler + filtreler + localStorage silinir; chip labellari reset olur.

### 6. Pause / Resume + Queue Badge

1. `Duraklat` butonuna tikla.
2. Simulator araciligi ile 3-5 event tetikle (orn. SIM durum degisikligi).
3. Event listesi donuk kalir; header/button ust kisimda `"N yeni olay"` badge gozukur.
4. `Devam Et` butonuna tikla — queued eventler listeye flush edilir (ters kronolojik siraya gore), badge kaybolur.

### 7. Event Card — Title / Message (F-09 fix)

1. Bir alert eventi tetikle (orn. operator SLA ihlali).
2. Event satirinda:
   - `"alert new"` yerine envelope `title` gorunur (orn. `"SLA violation for operator Turkcell"`).
   - Ikinci satir envelope `message` gorunur (farkli ise).
   - Severity badge (`HIGH` / kirmizi) gozukur.
   - Source chip (`source=operator`) gozukur.

### 8. Event Card — Clickable Entity (F-19 fix)

1. Herhangi bir event satirinda entity adina tikla (orn. `Turkcell`).
2. Pane kapanir; `/operators/:id` sayfasina navigate edilir.
3. SIM entityli event icin `→ /sims/:id` navigasyonu calisir.
4. Bilinmeyen entity tipi (`entity.type` route tablosunda yok) → span olarak gozukur, tiklanabilir degil, 404 yok.

### 9. Event Card — Bytes Chip (F-12 fix)

1. `session.updated` eventi tetikle (simulator araciligi ile bytes_in/bytes_out verisi ile).
2. Event satirinda `↓2.1 MB ↑48 KB` formatinda bytes chip gozukur.
3. Session tipi olmayan bir event (orn. `alert.triggered`) bytes chip gostermez.

### 10. Event Card — Details Link (Alert Row, AC-4)

1. `meta.alert_id` bulunan bir alert eventi tetikle.
2. Event satirinda `Details →` (ChevronRight ikonu) linki gozukur.
3. Linke tikla — `/alerts/:alert_id` sayfasina navigate edilir.
4. `meta.alert_id` olmayan bir event (orn. `session.updated`) — Details linki gozukmez.

### 11. Virtual Scrolling (AC-9)

1. Simulator ile 200+ event tetikle.
2. Pane acik iken browser DevTools Elements panelinden event listesi DOM'unu kontrol et — yalnizca ~15 satir render edilmis olmali (virtualization).
3. Scroll yap — satirlar dinamik olarak render/unrender edilir.

### 12. 500 Event Buffer Cap (AC-8)

1. Simulator ile 600 event tetikle.
2. `stores/events.ts` event sayisi 500'de sinirlenir; eski eventler dusar.
3. Pane header `"Son 500 olay"` gosterir.

## FIX-214: CDR Explorer Page

### 1. Temel Liste ve Filtre

1. `/cdrs` sayfasina git.
2. `Tarih Araligi` olarak `Son 7 gun` sec — CDR listesi yuklenir.
3. `KAYIT SAYISI`, `BENZERSIZ SIM`, `BENZERSIZ OTURUM`, `TOPLAM BAYT` stat kartlarinin doldugunu dogrula.
4. `SIM Ara` alanina gecerli bir ICCID yaz — liste filtrelenir; satir ICCID/IMSI/MSISDN sutunlarini gosterir.
5. Operator `Select` acilir — tenant'a atanmis operatorler listelenir; birini sec, liste yenilenir.
6. APN `Select` acilir — APN listesi gelir; birini sec, liste yenilenir.
7. `Record Type` chip grubundan `stop` sec — yalnizca stop kayitlari gozukur.
8. `Filtreleri Temizle` (bos durumda EmptyState icerisindeki CTA) tiklayinca tum filtreler sifirlanir.

### 2. Record Type Badge Renkleri

1. Listede `start` kaydinin Badge'i mavi/accent oldugunu dogrula.
2. `interim` kaydinin Badge'i info rengi (mavi-mor).
3. `stop` kaydinin Badge'i yesil/success.
4. `auth_fail` / `reject` kaydinin Badge'i kirmizi/danger.

### 3. Session Timeline Drawer

1. Herhangi bir CDR satirina tikla — sag tarafta SlidePanel/drawer acilar.
2. Drawer basliginda `Oturum Zaman Cizelgesi` gorunur.
3. Metadata header: SIM EntityLink (/sims/:id), Operatör EntityLink (/operators/:id), APN EntityLink veya `—`, Sure, Baslangic, Son alanlari var.
4. Recharts LineChart kumulatif bytes egrisini gosterir.
5. Tablo 7 sutun gosterir: ZAMAN / TİP / ↓ BYTES / Δ↓ / ↑ BYTES / Δ↑ / KÜMÜLATİF.
6. Drawer kapatilir — `X` veya backdrop tikladiginda kapanir.

### 4. EntityLink Navigasyonu

1. Tablodaki Operatör sutununda operatör adina tikla — `/operators/:id` sayfasina navigate edilir.
2. APN sutununda APN adina tikla — `/apns/:id` sayfasina navigate edilir.
3. ICCID sutununa tikla — `/sims/:id` sayfasina navigate edilir.
4. APN null olan bir satirin APN hucresinde `—` gozukur (tiklanabilir degil).

### 5. Export

1. Sayfa toolbar'inda `Disa Aktar` butonuna tikla.
2. Toast `"Rapor kuyruğa alındı. İlerleme için /reports."` mesajini gosterir.
3. `/jobs` sayfasinda yeni `cdr_export` job'i gozukur (pending → running → completed).

### 6. Deep-Link (Session Detail'den)

1. `/sessions/:id` sayfasina git — `CDR Kayıtları` butonu gozukur.
2. Butona tikla — `/cdrs?session_id=X&from=Y&to=Z` adresine navigate edilir.
3. CDR Explorer sayfasi yuklendiginde `session_id` filtresi otomatik uygulanir; yalnizca o oturumun kayitlari listelenir.

### 7. 30 Gun Sinirlama (Non-Admin)

1. Non-admin (orn. `analyst`) rolunde oturum ac.
2. `Son 30 gun` timeframe presetine hover/tikla — devre disi oldugunu ve aciklama mesajini goster.
3. `tenant_admin` veya `super_admin` ile `Son 30 gun` secebilmeli.

### 8. Bos Durum

1. Hicbir CDR donmeyecek sartlarda filtre uygula (orn. gelecek tarih aralik).
2. `Bu filtre için CDR bulunamadı.` baslikli EmptyState gozukur.
3. `Filtreleri Temizle` CTA butonu tiklayinca filtreler sifirlanir; liste yenilenir.

## FIX-215: SLA Historical Reports + PDF Export + Drill-down

### 1. Gecmis SLA Listesi (Rolling Window)

1. `/sla` sayfasina git. 12 adet ay karti (rolling 12-month default) yuklenir.
2. Ust kismda `6M / 12M / 24M` segmented toggle gorunur; `6M`a tikla — liste 6 ay gosterir.
3. `24M`a tikla — liste 24 ay gosterir; kartlar altinda uptime / breach / incident ozeti olur.
4. Kart uzerindeki uptime renk kodu: yesil = target ustu, sari = 99.5–target arasi, kirmizi = < 99.5 (BR-3 classifyUptime).
5. `/sla/history` HTTP call network panelde 200 dondurmeli, body `{status, data:{months:[...]}, meta:{months_requested, months_returned, breach_source}}` olmalidir.

### 2. Ay Detay Drawer

1. `/sla` sayfasinda herhangi bir ay kartina tikla — sag taraftan SlidePanel (aria-modal=true, focus-trap) acilir.
2. Drawer icinde: ay genel uptime ozeti, operator bazinda satirlar (operator adi + uptime + breach sayisi + toplam dakika).
3. Operator satirina tikla — nested SlidePanel acilir (Operator Breach drawer).
4. `Esc` tusuna bas — panel kapanir ve focus geri acilis butonuna doner.
5. Backend rollup olmayan bir ay (orn. gelecek) icin 404 `sla_month_not_available` donmelidir; UI `Bu ay icin rapor olusmadi` EmptyState gosterir.

### 3. Operator Breach Drawer

1. Ay Detay Drawer'dan bir operatore tikla — Operator Breach drawer acilir.
2. Ust kisimda totals: `N breach · Xm Ys · ~Z affected sessions`.
3. Breach satirlari: baslangic/bitis zamani, sure, tetikleyici (down/latency), `~<N> session etkilendi`.
4. Breach `operator_health_logs` 90-gun retention disi ise breach_source=`sla_reports.details` (meta alaninda gorunur).
5. Veri yoksa `Bu ayda breach tespit edilmedi` EmptyState gozukur.

### 4. PDF Indirme (Month + Operator-Month)

1. `/sla` bir ay kartinda `Download PDF` butonuna bas (useSLAPDFDownload Bearer token ile blob fetch).
2. Buton loading state gosterir; bitince dosya browser indirir: `sla-YYYY-MM-all.pdf` (ay-wide).
3. Ay Detay drawer icinde operator satirindaki PDF butonuna bas — `sla-YYYY-MM-<operator_code>.pdf` iner.
4. Expired/invalid token ile cagri yap (orn. logout sonrasi) — sonner toast `PDF indirilemedi` hatasi gosterir.
5. Cross-tenant operator id ile cagri (devtools) 403 `forbidden` donmelidir (BR-6 tenant scope).

### 5. SLA Hedef Duzenleme (Operator Detail)

1. `/operators/:id` sayfasinda `Protocols` sekmesine git — `SLA Targets` bolumu gorunur.
2. `Uptime target %` (default 99.90) ve `Latency breach threshold (ms)` (default 500) alanlarini duzenle.
3. Gecersiz deger gir: uptime=45 → validation hatasi `50.00 ile 100.00 arasinda olmali`; latency=10 → `50 ile 60000 arasinda olmali`.
4. Valid degerler girip `Kaydet`e bas — sonner toast `SLA hedefleri guncellendi` gosterir.
5. Audit sekmesinden `operator.updated` action kaydini dogrula (before/after degerleri JSON icinde).
6. Sayfayi yenile — yeni degerler persist olmalidir.

## FIX-216: Modal Pattern Standardization — Dialog vs SlidePanel

### 1. SIM Bulk State-Change Dialog (Suspend/Resume/Terminate)

1. `/sims` sayfasina git; tabloda 2+ SIM sec (checkbox).
2. Bulk action bar goruntur; `Suspend` (veya `Resume`) tikla — merkezde `Dialog` acilir.
3. Dialog icinde aksiyon baslik + optional reason input + primary/cancel butonlar gorunur.
4. `Terminate` tikla — Dialog primary button `destructive` variant (kirmizi ton) olur.
5. `Esc` tusu Dialog'u kapatir; secim korunur.

### 2. SIM Assign Policy SlidePanel

1. `/sims` sayfasinda 1+ SIM sec; bulk action bar `Assign Policy` tikla.
2. Sag taraftan `SlidePanel` acilir (width=md); title + description header'da gorunur.
3. Icerik: policy picker (search), version dropdown, optional comment textarea.
4. Footer `SlidePanelFooter` icinde primary + cancel butonlari.
5. Focus-trap calisir — Tab sadece panel icinde dolasir; Esc kapatir.

### 3. IP Pool Reserve SlidePanel

1. `/settings/ip-pools/:id` detay sayfasina git; `Reserve IP` butonuna bas.
2. SlidePanel acilir (title + description props kullanildigi icin ayri bir header component yok).
3. Form icerik (CIDR/range input + SIM picker) + SlidePanelFooter tutarli.
4. Save → toast gosterir; panel kapanir; pool table refresh olur.

### 4. Violations Row Detail SlidePanel

1. `/violations` sayfasina git; herhangi bir violation satirina TIKLA (veya Enter/Space ile focus verip ac).
2. Sag taraftan SlidePanel acilir; title=`policy_name ?? violation_type`, description=`created_at`.
3. Body: metadata grid (SIM, tenant, severity) + details grid (DSL, kosul, evidence).
4. Footer: Close butonu (variant=outline).
5. Eski inline row-expand kalkmis; artik row kendisi button olarak davranir.

### 5. Keyboard A11y (Violations Row)

1. `/violations` sayfasinda Tab ile satir'a focus ver — dashed outline gorunur.
2. Enter'a bas — SlidePanel acilir.
3. Panel acikken Esc'e bas — kapanir; focus tetikleyici satira doner.
4. Satir focus'tayken Space'e bas — panel acilir; sayfa scroll OLMAZ (preventDefault calistiginin dogrulamasi).

### 6. Dark Mode Tum Modallar

1. Sidebar'dan theme toggle ile dark mode'a gec.
2. Yukaridaki 4 senaryoyu tekrarla — her modal/drawer dark theme tokenlariyla render olur.
3. Beyaz arka plan (`bg-white`), gri Tailwind palette veya hardcoded hex/rgba gorunmemeli.
4. Shadow tokenlari (`--shadow-card`, `--shadow-card-success/warning/danger`) dogru uygulanir.

---

## FIX-217: Timeframe Selector Pill Toggle Unification

### 1. Canonical Pill Rendering — 5 Sayfa

1. Sirasiyla ac: `/admin/api-usage`, `/admin/delivery`, `/operators/:id` (Traffic + Health-Timeline sekmeleri), `/apns/:id` (Traffic sekmesi), `/cdrs`.
2. Her sayfada ust filtre alaninda segmented-control pill grubu gorunur (rounded-[3px], ghost active=`bg-accent text-bg-primary shadow-sm`, inactive=`text-text-secondary hover:bg-bg-hover`).
3. Preset seti: canonical `1h / 24h / 7d / 30d` (admin/delivery sayfasi allowCustom=false ek Custom yok; cdrs + operators + apns icin Custom var).
4. Default secim varsayilan olarak `24h` (admin sayfalari; cdrs `24h`; operators TrafficTab `24h`; apns TrafficTab `24h`).
5. Hicbir sayfada eski `<Select>` dropdown veya elle-yazilmis `<button>` pill grubu kalmis olmamali.

### 2. Custom Popover — Date Range Akisi (cdrs)

1. `/cdrs` sayfasina git; pill grubunda `Custom` tikla → popover acilir (role=dialog, From/To iki `datetime-local` input, Apply/Cancel butonlari).
2. Baslangic = `2026-04-22T10:00`, Bitis = `2026-04-22T18:00` gir; Apply bas.
3. URL guncellenir: `?tf=custom&tf_start=...&tf_end=...` (ISO UTC formati).
4. Pill etiketi `Custom · Apr 22 → Apr 22` benzeri bir ozet gosterir (secim aktiftir, `bg-accent`).
5. Popover'i yeniden ac — `From`/`To` girilmis DEGERLERI local olarak gosterir (UTC kayma YOK — F-A3/A4 fix).
6. Cancel tikla — popover kapanir, deger degismez.
7. Browser'i URL ile refresh et (`?tf=custom&tf_start=...&tf_end=...`) — Custom pill aktif gelir, popover re-open ayni lokal saat.

### 3. Role Gating — Analyst 30d Kilidi (cdrs)

1. Analyst rolunde oturum ac (admin OLMAYAN kullanici); `/cdrs` ac.
2. 30d pill'i `opacity-40 cursor-not-allowed`, `aria-disabled="true"`, `title="Not available for your role"` gosterir.
3. 30d uzerine TIKLA → hic bir sey olmaz (onChange fire etmez, URL degismez).
4. Klavye ile ArrowLeft/Right cycle et → 30d pill UZERINE LANDING YAPMAZ (selectableIndices atlar).
5. Admin kullanici olarak login ol — 30d artik aktif ve tiklanabilir.

### 4. URL Sync Deep-Link — cdrs

1. `/cdrs?tf=7d` adresine direkt git → 7d pill aktif, tablo son 7 gun CDRs'leri getirir.
2. Pill'den 1h sec → URL `?tf=1h` olur.
3. Record-type chip veya session_id filtresi degistir → URL'de `tf` korunur (F-A1 fix — filter-sync effect `tf/tf_start/tf_end` temizlemez).
4. Back/Forward navigation ile history gezin — her adimda pill state URL ile senkron.
5. `/cdrs?tf=invalidvalue` ac → hook `24h` fallback'e duser (gecersiz preset sessizce reddedilir).

### 5. Back-Compat Legacy Callers

1. `/dashboard` (analytics), `/dashboard/cost` (analytics-cost), `/sims/:id` (detail) sayfalarini ac — her biri hala `TimeframeSelector`'u eski `value: string` overload imzasi ile kullaniyor.
2. Pill grubu ayni canonical stilde render olur; eski davranis bozulmaz (3 sayfada tsc=PASS + runtime normal).
3. Timeframe degistir → iligli grafik/tablolar refresh olur.

### 6. Keyboard Nav + A11y

1. Herhangi bir adopted sayfada pill grubunu Tab ile focus'a al — aktif pill focus halkasi (`focus-visible:outline-accent`).
2. `ArrowRight` → bir sonraki secilebilir preset'e gec (disabled pill atla); `ArrowLeft` → ters yon.
3. `Home` → ilk enabled preset; `End` → son enabled preset.
4. `Enter` veya `Space` → odaktaki pill'i sec (onChange fire eder, URL guncellenir).
5. Disabled pill'de `Enter` → onChange fire ETMEZ (native `disabled` bloklar).
6. Screen reader: `role=group aria-label="Timeframe"` anons eder; her pill `aria-pressed={isActive}` + aktif pill icin "selected" anonsu.

## FIX-218: Views Button Global Removal + Operators Checkbox Cleanup

### 1. Views Button Absent — 4 List Pages

1. `/operators` sayfasini ac — toolbar'da (sayfa basliginin yaninda filtre/arama alani) "Views" veya "Save View" butonu OLMAMALI. `grep 'SavedViewsMenu' web/src/pages/operators/index.tsx` → 0 sonuc.
2. `/apns` sayfasini ac — ayni sekilde toolbar'da Views butonu OLMAMALI.
3. `/policies` sayfasini ac — Views butonu OLMAMALI.
4. `/sims` sayfasini ac — Views butonu OLMAMALI.
5. Her 4 sayfada diger toolbar elemanlari (arama, filtreler, Export, New/Create butonlari) NORMAL calisir.

### 2. Operators Page — Checkbox + Compare Removed

1. `/operators` sayfasini ac; operator kartlari listelenir.
2. Her kartın sag-ust kösesinde CHECKBOX OLMAMALI — hover durumunda da checkbox gorunmez.
3. Sayfanin ustteki toolbar alaninda "Compare (N)" butonu OLMAMALI (secili kart yokken de, hicbir durumda).
4. Kart uzerine hover'da yalnizca `RowActionsMenu` (uc nokta) gorunur — calisir, detail/edit/delete aksiyonlari aktif.
5. `grep 'selectedIds\|Compare\|Checkbox' web/src/pages/operators/index.tsx` → 0 sonuc (tsc-clean).

### 3. Policies + SIMs — Checkbox Scaffolding KORUNDU

1. `/policies` sayfasini ac; satirlarda checkbox var; birden fazla secilince Compare veya bulk aksiyon butonu cikiyor → calisir.
2. `/sims` sayfasini ac; bulk-action bar (Suspend / Resume / Terminate) SIM secilince gorunur → calisir.
3. Policies ve SIMs sayfalarinda `selectedIds` state + Compare/bulk mekanizmasi BOZULMAMIS.

### 4. Backend Retention Smoke

1. `tenant_admin` JWT ile `GET /api/v1/user/views?page=sims` → 200 (endpoint hala aktif; frontend widget kaldirilmis olsa da backend endpoint saglikli).
2. `POST /api/v1/user/views` → 201 (backend endpoint yazma islemleri de calisiyor).
3. `web/src/components/shared/saved-views-menu.tsx` dosyasi MEVCUT (tree-shake tarafindan bundle'dan cikarilir ama kaynak kodda korunur — ROUTEMAP D-096).
4. `web/src/hooks/use-saved-views.ts` dosyasi MEVCUT.

### 5. Build Clean

1. `cd web && npx tsc --noEmit` → 0 hata.
2. `make web-build` → PASS, build suresi ~3s.

---

## FIX-219: Name Resolution + Clickable Cells Everywhere

**Story:** FIX-219 — Global EntityLink extension, EntityHoverCard, backend DTO enrichment, 23-page audit.

### 1. EntityLink Appearance per Page

1. `/sims` listesini ac; APN ve Operator sutunlari "Cloud apnAdi" / "Radio operatorAdi" seklinde ikon + etiket render eder. Ham UUID prefix (`abc12345...`) HIC GORUNMEMELI.
2. `/operators` listesini ac; Tenant sutunu `EntityLink` olarak render edilir — Building2 ikonu + tenant adi.
3. `/audit` listesini ac; Actor ve Entity sutunlari `EntityLink` gosterir (user email label + User ikonu; entity label + entity tipi ikonu). Ham UUID dilimi (`abc12345`) OLMAMALI.
4. `/admin/purge-history` sayfasini ac; Actor sutunu `EntityLink` (user ikonu + email) veya `—` em-dash (sistem aksiyonu). `actor_id` dolu satir varsa tiklanabilir.
5. `/jobs` listesini ac; Created By sutunu kullanici adi veya `[System]` etiketi gosterir — ham UUID OLMAMALI.

### 2. EntityHoverCard Delay + Offline

1. `/dashboard` sayfasinda Op Health veya Top APNs widget'ini bul; EntityLink uzerinde 200ms hover bekle — kucuk popover acilar, entity ozet bilgisi (operator: code + MCC/MNC + health chip; APN: code + operator + subscriber count) gosterilir.
2. Popover acildiktan sonra imleci uzaklastir (mouse-leave) — popover kapanir.
3. Browser'da DevTools → Network → Offline moduna gec; EntityLink uzerine hover yap — popover acilmaz (navigator.onLine guard).
4. Tekrar Online moduna don; hover çalışır.

### 3. Orphan Em-Dash Rendering

1. `/audit` listesini ac; deleted/orphan entity referanslari icin `entityId` dolup `label` bos olan satirlarda truncated UUID tooltip (mevcut fallback), her ikisi de bos ise `—` karakter render edilir.
2. `grep -rn '\.slice(0,8)\|\.slice(0, 8)' web/src/pages/ --include='*.tsx'` → 0 sonuc (UUID dilimi yoklugu dogrulamasi).

### 4. Right-Click Copy UUID

1. Herhangi bir sayfada `EntityLink` uzerine sag-tik yap → native browser context menu yerine "UUID copied" toast gorunur.
2. Clipboard'da kopyalanan deger orijinal `entityId` (tam UUID) dir.
3. `copyOnRightClick={false}` ile render edilmis bir EntityLink uzerinde sag-tik → native browser menu acilir (test icin `audit/index.tsx` export UUID link ornegi).

### 5. Keyboard Navigation + A11y

1. Tab ile EntityLink'e odaklan — `focus-visible` halkas gorunur.
2. Screen reader ile `aria-label="View operator Turkcell"` gibi anlamlı etiket okunur; label yoksa aria-label `entityType` + truncated ID icerir.
3. Enter tusuna bas — detail sayfasina gider.

### 6. UUID Slice Absence (Grep Check)

1. `grep -rn '\.slice(0,8)\|\.slice(0, 8)\|\.substring(0,8)' web/src/pages/ --include='*.tsx'` → 0 match (birincil UI UUID dilimi yok).
2. `grep -rn '\.slice(0,8)' web/src/components/shared/entity-link.tsx` → 0 match (bileşen içinde de yok).

### 7. Backend DTO Enrichment

1. `GET /api/v1/sessions/stats` → response body'de `top_operator.name` alani dolu (UUID degil insan-okunakli isim).
2. `GET /api/v1/jobs` → `created_by_name` + `created_by_email` + `is_system` alanlari mevcut.
3. `GET /api/v1/audit` → `user_email` + `user_name` alanlari dolu.
4. `GET /api/v1/admin/purge-history` → `actor_email` + `actor_name` alanlari dolu (insan aksiyonu icin); sistem purgelarda `actor_id: null` ve `actor_email: ""` beklenir.

---

## FIX-220: Analytics Polish — MSISDN, IN/OUT Split, Tooltip, Delta Cap

### 1. Top Consumers Table — New Columns

1. `/analytics` sayfasina git; Top Consumers tablosu gorunur.
2. Yeni sutunlar: `ICCID | IMSI | MSISDN | Operator | APN | IN/OUT | Total | Sessions | Avg Duration`.
3. IMSI + MSISDN sutunlari `hidden md:table-cell` ile mobile'da gizlenir; md+ breakpoint'te gorunur.
4. Her row: Operator/APN hucreleri `<EntityLink>` ile tiklanabilir (FIX-219 uyumlu); click detail sayfasina gider.
5. `IN/OUT` sutunu `<TwoWayTraffic>` ile render edilir: `↓` (success renk) inbound + `↑` (info renk) outbound + hover'da tooltip "In: 1.2 MB · Out: 0.8 MB · Total: 2 MB".
6. Her iki yon=0 ise `—` em-dash gosterir.

### 2. IN/OUT Zero Edge (cdrs_daily 30d)

1. Timeframe'i 30 gune ayarla (`?tf=30d`).
2. cdrs_daily aggregate tablosu bytes_in/bytes_out split tutmaz (total_bytes var) → IN/OUT sutunu `—` olarak goruntur.
3. Total sutunu hala dolu kalir.

### 3. Delta Cap + Polarity

1. KPI kartlarinda (Total Bytes, Sessions, Auths, Unique SIMs) delta badge gorunur.
2. Eger `delta > 999%` → `">999% ↑"` olarak gosterir (cap).
3. Eger `delta < -100%` → `—` em-dash (anlamsiz azalma) ve `tone='null'`.
4. `prev === 0 && curr > 0` → `"↑"` (yeni veri gostergesi) + `tone='neutral'`.
5. Polarity: bytes/sessions up-good (yeşil when pozitif), down-good metrikler ters ton.
6. `delta === 0` → `"0%"` + neutral ton.

### 4. Rich Usage Chart Tooltip

1. Chart bar'ina hover et → custom `<UsageChartTooltip>` acilir.
2. Tooltip icerik (non-grouped mode):
   - Timestamp (formatli, period'e gore 24h → "14:00", 30d → "Apr 22 14:00")
   - `<TwoWayTraffic>` IN/OUT split
   - Total bytes
   - Δ prev bucket (delta badge)
   - Sessions + Auths
   - `unique_sims` (value > 0 ise; cdrs_hourly 24h/7d iken 0 → row gizlenir — aggregate view limitation).
3. Grouped mode (group_by=operator/apn/rat_type): series renk nokta + series name + formatBytes + "Top: {name} — {value}".
4. Tooltip dark tokenlari kullanir: `bg-bg-elevated`, `border`, `text-text-primary`/`secondary`.

### 5. Empty State + Filter Hints

1. Timeframe'i cok dar bir aralik yap (ornek: gelecekte bir zaman) → tablolar bos doner.
2. Empty state gorunur: "Try expanding the date range or clearing the active filter." (filter aktif ise) veya "Try expanding the date range." (filter yoksa).
3. Date range EmptyState'te formatli gosterilir.
4. group_by secildi ve sifir grup varsa → chart card icinde inline "No groups found for this filter" mesaji.

### 6. Capitalization (humanization)

1. `group_by=operator/apn/rat_type` seciliyken CardTitle dogru etikete donusur (humanizeGroupDim: "Operator" / "APN" / "RAT Type").
2. Breakdown row etiketleri `rat_type` icin `humanizeRatType` uygulanir: `4g` → `4G`, `5g_sa` → `5G SA`, vs.
3. Chart legend (grouped mode) rat_type gruplarini humanize eder.

### 7. Backend DTO (API spot-check)

1. `GET /api/v1/analytics/usage?period=1h` → response `top_consumers[]` her entry: `imsi`, `msisdn`, `bytes_in`, `bytes_out`, `avg_duration_sec` alanlarinin dolu oldugunu dogrula.
2. `time_series[]` her bucket: `bytes_in`, `bytes_out` alanlari mevcut ve toplam `total_bytes`'e esit.
3. `period=7d` → `time_series` bucket'larinda `unique_sims=0` bekle (cdrs_hourly aggregate dimension yok).
4. `period=30d` → `bytes_in=0, bytes_out=0` bekle (cdrs_daily split kolonu yok); `total_bytes` + `unique_sims` (SUM(active_sims)) hala dolu.

### 8. cdrs_daily APN/RAT Filter Fix (F-A12)

1. `?period=30d&apn_id=<id>` query — onceki buggy halde 30d window aggregate view apn filtresini sessizce dusurup TUM verileri donerdi. Gate fix sonrasi artik filtre gecerli ve dogru alt-kumeyi doner.
2. Ayni `rat_type` filtresi icin de gecerli.

## FIX-221: Dashboard Polish — Heatmap Tooltip, IP Pool KPI Clarity

### 1. Traffic Heatmap Tooltip

1. Dashboard (`/`) sayfasina git; Traffic Heatmap kartini bul (7 gun × 24 saat grid).
2. Herhangi bir hucrenin uzerine gel (hover) → tooltip acilmali.
3. Tooltip formati: `<formatBytes(rawBytes)> @ <Weekday> HH:00` (ornek: `"1.4 GB @ Mon 14:00"`).
4. rawBytes = 0 olan (bos) hucreler icin tooltip `"0 B @ <Day> HH:00"` gostermeli.
5. Tooltip metni `text-[10px] font-mono` stilinde, koyu token (`bg-bg-elevated border text-text-primary`) kullanmali.

### 2. IP Pool KPI Karti

1. Dashboard KPI satirinda "Pool Utilization" kartini bul.
2. KPI baslik her zaman `"Pool Utilization (avg across all pools)"` olarak gozukmeli — parantezli aciklama her zaman gorunur.
3. Aktif IP pool'u olan tenant'ta: kartın altinda subtitle `"Top pool: <pool-adi> <pct>%"` (ornek: `"Top pool: iot-pool-1 73%"`) gozukmeli.
4. Aktif IP pool'u olmayan/sifir olan tenant'ta: subtitle gozukmemeli (null/omitempty).
5. Pool adi uzunsa `truncate` ile kesilmeli; tasmamali.

### 3. Backend DTO Spot-Check

1. `GET /api/v1/dashboard` response → `traffic_heatmap[]` her eleman `value` (float, normalize [0,1]) + `raw_bytes` (int64, ham byte toplami) icermeli.
2. Aktif pool'u olan tenant'ta response `top_ip_pool: { id, name, usage_pct }` icermeli.
3. Aktif pool'u olmayan tenant'ta `top_ip_pool` alani response'da yer almamali (omitempty).
4. 168 eleman beklenmez — sadece veri olan bucket'lar doner; bos saatler response'a dahil edilmez.

## FIX-222: Operator/APN Detail Polish — KPI Row, Tab Consolidation, Tooltips, eSIM Tab

### 1. Operator Detail KPI Row

1. `/operators/:id` sayfasina git.
2. Tab satirinin uzerinde 4 KPI karti gorunmeli: **SIMs** (toplam SIM sayisi), **Active Sessions** (anlık), **Auth/s** (1h ortalama), **Uptime %** (24h).
3. Verinin olmadigi durumlarda kart `—` (em-dash) gostermeli; bos/null hucre olmamali.
4. Her kart animated counter ile render olmali.

### 2. Operator Detail Tab Consolidation (11→10)

1. Operatör detay sayfasinda 10 tab olmali: Overview / Protocols / Health / Traffic / Sessions / SIMs / eSIM / Alerts / Audit / Agreements (Agreements FIX-238 sonrasi kaldirilacak).
2. **Circuit** tab artık yok — CircuitBreaker widget, Health tab icine tasindi.
3. **Notifications** tab artık yok — RelatedNotificationsPanel, Alerts tab icine eslesti.
4. Eski URL `?tab=circuit` → otomatik `?tab=health` yonlendirmesi yapmali (replace:true, tarayici gecmisi kirletilmemeli).
5. Eski URL `?tab=notifications` → otomatik `?tab=alerts` yonlendirmesi yapmali.

### 3. Operator Detail eSIM Tab

1. eSIM tab'ina tikla → EID (ⓘ), ICCID (ⓘ), Profile State (Badge), SIM (EntityLink), Created At sutunlari gorunmeli.
2. Verisi olmayan operator icin EmptyState gorunmeli.
3. Yukleme sirasinda skeleton gorunmeli.
4. Hata durumunda AlertCircle + "Retry" butonu gorunmeli.
5. Birden fazla eSIM profili varsa Load More butonu gorunmeli.

### 4. Operator Detail Header InfoTooltip

1. Baslik altinda MCC/MNC gosterilir — yanlarindaki ⓘ simgesine hover et (500ms delay sonrasi) veya tikla → tooltip acilmali.
2. Tooltip icerigi: MCC icin "Mobile Country Code (3 digits identifying country, e.g. 286 = Turkey)"; MNC icin "Mobile Network Code (2-3 digits identifying operator within country)".
3. ESC tusu tooltip'i kapatmali.
4. `aria-expanded` attribute tooltip acik/kapali durumu yansitmali.

### 5. APN Detail KPI Row

1. `/apns/:id` sayfasina git.
2. 4 KPI karti gorunmeli: **SIMs**, **Traffic 24h** (formatBytes), **Top Operator** (en fazla SIM'in bagli oldugu operator — ilk 50 SIM'den turetilir), **APN State** (ACTIVE/SUSPENDED badge).
3. SIM listesi paginated (>50) ise Top Operator subtitle `"Based on first 50 SIMs"` uyarisi gostermeli.
4. SIM verisi yoksa Top Operator karti `—` gostermeli.

### 6. APN Detail Tab Consolidation + Overview First

1. APN detay sayfasinda 8 tab olmali: **Overview** / Config / IP Pools / SIMs / Traffic / Policies / Audit / Alerts.
2. Overview tab varsayilan (ilk) tab olmali — APN konfigurasyonu okuma agirlikli gosterimi saglar.
3. Eski `?tab=notifications` → `?tab=alerts` yonlendirmesi (replace:true).
4. Eski default `config` tab'ina gelen URL'ler (`?tab=config`) normal sekilde Config tab'ini acmali.

### 7. Tab URL Deep-Link

1. Herhangi bir tab'a tikla → URL `?tab=<name>` ile guncellenmeli.
2. URL'yi kopyala / baska sekmede ac → ayni tab aktif olmali.
3. Tarayici geri tusuna basildiginda sayfa URL degistirmemeli (replace:true semantigi).
4. Gecersiz `?tab=xyz` → defaultTab'a (overview) silent fallback; 404 olmamali.

### 8. InfoTooltip — SIMs Tablosu Headers

1. Hem Operator hem APN SIMs tablosunda ICCID (ⓘ), IMSI (ⓘ), MSISDN (ⓘ) basliklarinda InfoTooltip simgesi olmali.
2. APN sayfasinda ayrica APN (ⓘ) sutun basligi olmali.
3. Toplam 11 InfoTooltip cagrisi her iki sayfa arasinda dagitilmis olmali (tsc PASS ile dogrulandi).

## FIX-223: IP Pool Detail Polish — Server-side Search, Last Seen, Reserve Modal ICCID

### 1. Sunucu Tarafli Arama (AC-1)

1. `/settings/ip-pools/:id` sayfasina git; herhangi bir IP pool detay sayfasini ac.
2. Adres tablosunun ustundeki arama kutusuna en az 3 karakter yaz.
3. 300ms bekle — tablo sunucu tarafinda filtrelenmis sonuclari gostermeli (network istegi atilmali: `?q=<term>`).
4. Arama kutusunu temizle → tablo tam listeye donmeli.
5. 64 karakterden uzun sorgu yazildiginda API `400 Bad Request` donmeli (q param validation).

### 2. ICCID ile Arama (AC-1)

1. Bilinen bir SIM ICCID'sinin ilk 8 hanesiyle arama yap.
2. O SIM'e atanmis IP adresi satiri tabloda gorunmeli.
3. IMSISDN veya IMSI ile de ayni sekilde filtreleme calistirilabilir.

### 3. Last Seen Sutunu (AC-3)

1. IP adresleri tablosunda "Last Seen" sutunu gorunmeli (6. sutun).
2. `last_seen_at` dolu olan adreslerde formatDistanceToNow ile goreceli zaman gosterilmeli (ornek: `"3 minutes ago"`).
3. `last_seen_at` null olan (AAA writer henuz implement edilmemis D-121) adreslerde `—` (em-dash) gosterilmeli.

### 4. Reserve SlidePanel — ICCID Gorunumlugu (AC-4)

1. Tabloda herhangi bir IP adresinin ustundeki "Reserve" butonuna tikla.
2. SlidePanel acilmali; "Currently reserved" mini-listesi mevcut rezervasyonlari gostermeli.
3. Her rezervasyon satiri: adres + sim_iccid degerini gostermeli (ornek: `10.0.0.5 — 8901...`).
4. Ana tabloda aktif bir arama filtresi varken "Reserve" acildiginda, unfiltered kaynak kullanildigi icin "Currently reserved" listesi arama filtresiyle kisaltilmamali — tum rezervasyonlar gorunmeli.

### 5. Static IP Tooltip — APN Detay (AC-5)

1. `/apns/:id` sayfasini ac; "IP Pools" bolum basliginin yanindaki ⓘ simgesine hover et veya tikla.
2. Tooltip acilmali; icerik: "Static IP — an IP address permanently assigned to a specific SIM via pool reservation..."
3. ESC tusu tooltip'i kapatmali.

---

## FIX-224: SIM List/Detail Polish — State Filter, Created Datetime, Compare Cap, Import Preview

### 1. Multi-State Filter (AC-1)

1. `/sims` sayfasina git; filtre cubugundaki **States** dropdown'ina tikla.
2. Dropdown acilmali ve 5 checkbox item gosterilmeli: **Ordered**, **Active**, **Suspended**, **Terminated**, **Lost/Stolen**.
3. "Active" secimi: checkbox isaretle — menu acik kalmali (kapanmamali). Tablo "active" state'li SIM'leri gostermeli. URL: `?state=active`.
4. Ek olarak "Suspended" sec — menu acik kalmaya devam etmeli. Tablo her iki state'i gostermeli. URL: `?state=active,suspended`. Filtre cubugunda her state icin ayri chip gosterilmeli ("Active", "Suspended").
5. "Active" chip'indeki X'e tikla → sadece o token kaldirilmali, "Suspended" chip'i kalmali. URL: `?state=suspended`.
6. Dropdown'dan tum secimleri kaldir → States filter yok, URL'de `state` param yok.
7. URL'e manuel `?state=active` yaz + Enter → tablo tek-state filtreli yuklemeli; FIX-219 oncesi URL backward compatible.

### 2. Created Datetime + Tooltip (AC-2)

1. SIM listesinde "Created" kolonuna bak — tarih + saat formatinda gosterilmeli (ornek: `4/19/2026, 15:59:00`).
2. Herhangi bir SIM'in Created hucresinin uzerine hover et — shadcn Tooltip gorunmeli; icerik: goreceli zaman (ornek: `"4 days ago"`).
3. Tooltip 300-500ms sonra acilmali; mouse uzaklasmca kapanmali.
4. Quick-peek panelindeki Created alani eski format'i (toLocaleDateString) korumali — tooltip yok (kompakt goruntu amacli).

### 3. Compare Cap 4 + Warn+Disable (AC-4)

1. `/sims/compare` sayfasina git.
2. 4 SIM slot'u mevcutsa grid `lg:grid-cols-4` seklinde 4 kolona dusmeli.
3. 4 SIM ekle — "Add SIM" butonu devre disi (disabled) olmali ve `aria-disabled="true"` attribute'u olmali.
4. 4 SIM seciliyken "Compare limit reached (4/4) — remove a slot to add another SIM." uyari mesaji gorunmeli (`text-warning` renk, AlertCircle ikonu).
5. 5. SIM aramasi: slot arama kutusuna yeni ICCID yaz → sec tusu/secim aktif olsa da 5. slot eklenmemeli.
6. Bir slot'u kaldirinca "Add SIM" butonu yeniden aktif olmali ve uyari mesaji kaybolmali.
7. EmptyState mesaji "up to 4 SIM cards" ibaresi icermeli (eski "3" degil).

### 4. Import SlidePanel — 3-Stage Flow (AC-5)

1. SIM listesinde "Import" butonuna tikla — SlidePanel acilmali (3 stage: input → preview → result).
2. **Stage: input** — CSV icerigi yapistir veya dosya sec.
3. Gecerli 20 satirlik CSV yapistir (gerekli kolonlar: `iccid,imsi,msisdn,operator_code,apn_name`):
   - "Preview" a tikla → Stage 2'ye gec.
   - 10 preview satiri tablo olarak gorunmeli (fazlasi gosterilmez); "20 rows detected • estimated ~1s processing time" ozeti gosterilmeli.
   - Hicbir satir kirmizi border ile isaretlenmemeli (gecerli veriler).
4. `msisdn` kolonunu cikarin CSV'den → "Preview" tiklandiktan sonra kirmizi banner gorunmeli: "Missing required columns: msisdn". Commit butonu devre disi olmali.
5. ICCID kolonu formatini boz (cok kisa) → format uyarisi satir bazinda kirmizi sol border ile isaretlenmeli; commit butonu aktif (uyari; kolon-seviyesi engel degil).
6. "Back" ile Stage 1'e don; duzelt; "Preview" → Stage 2; "Import N SIMs" tiklayinca Stage 3'e gec.

### 5. Import Post-Process Report (AC-6)

1. Gecerli CSV ile import baslatinca Stage 3'e gec — polling baslamali; spinner + "Importing…" gorunmeli.
2. Job tamamlaninca: "X succeeded • Y failed" header gorunmeli.
3. Hata varsa: ilk 20 satir `Row #{row} — ICCID {iccid}: {reason}` formatinda listelenmeli.
4. Hata sayisi > 0 ise: "View failed rows" butonu gorunmeli → `/jobs/:id` sayfasina navigate etmeli.
5. Hicbir hata yoksa: "View failed rows" butonu gorunmemeli.
6. Import sonrasi SIM listesi yenilenmeli (refetch tetiklenmeli).
7. `useImportSIMs` hook response: `{ job_id, tenant_id, status }` shape (eski `rows_parsed`/`errors[]` yok — `tsc --noEmit` temiz olmali).

---

## FIX-225: Docker Restart Policy + Infra Stability

Bu story icin manuel kullanici arayuzu senaryosu yoktur (ops/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

1. `docker compose -f deploy/docker-compose.yml config --quiet` → exit 0 (YAML gecerli).
2. `make down && make up` → Tum 7 container (nginx, argus, postgres, redis, nats, operator-sim, pgbouncer) `healthy` durumuna gelmeli (~90s bekle).
3. `docker inspect argus-nats --format='{{.State.Health.Status}}'` → `healthy` (NATS /healthz probu aktif).
4. `grep "service_started" deploy/docker-compose.yml` → 0 eslesme (tum argus hard-dep'leri `service_healthy`).
5. `curl -s http://localhost:8084/health` → HTTP 200 OK (nginx → argus zinciri saglikli).
6. Crash recovery: `docker kill -s KILL argus-app` → 10s icinde Docker otomatik yeniden baslatmali; 90s icinde `healthy` olmali.
7. Recovery doc: `docs/architecture/DEPLOYMENT.md` mevcutsa ve 13 bolum iceriyorsa PASS (`grep "^##" docs/architecture/DEPLOYMENT.md | wc -l` ≥ 7).

---

## FIX-226: Simulator Coverage + Volume Realism

Bu story icin manuel kullanici arayuzu senaryosu yoktur (simulator/altyapi). Asagidaki komutlar ile dogrulama yapilabilir:

### 1. SIM Buyume Gercekci (AC-6 — seed stagger)

1. `make db-seed` calistir → hata olmadan tamamlanmali.
2. `psql -c "SELECT DATE_TRUNC('day', activated_at)::date AS day, COUNT(*) FROM sims GROUP BY 1 ORDER BY 1"` → en az 40 farkli gun gostermeli (60 gunluk stagger; ~3.3 SIM/gun).
3. Dashboard → Capacity sayfasina git → "SIM Growth" widget → haftalik buyume orani **< %10** olmali (eski `+73.3%/gun` yerine gercekci deger).

### 2. Bandwidth Ihlali — Gercek Enforcer Yolu (AC-4)

1. Simulatoru baslat: `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.simulator.yml up simulator`.
2. 10 dakika bekle.
3. `psql -c "SELECT COUNT(*) FROM policy_violations WHERE violation_type='bandwidth_exceeded' AND created_at > NOW() - INTERVAL '10 min'"` → `> 0` olmali (aggressive_m2m senaryosu ihlalleri kaydediyor).
4. Dashboard → Alerts sayfasina git → "bandwidth_exceeded" uyarisi gorunmeli.

### 3. NAS-IP AVP Doldurulmus (AC-3)

1. Simulatoru baslat (yukardaki compose komutuyla).
2. `curl -s http://localhost:9099/metrics | grep simulator_nas_ip_missing_total` → deger `0` olmali.
3. Argus tarafinda: `curl -s http://localhost:8080/metrics | grep argus_radius_nas_ip_missing_total` → deger `0` veya cok dusuk olmali (NAS-IP AVP artik her Access-Request'te mevcut).

### 4. CoA Latency Metrik (AC-8)

1. Simulatoru baslat.
2. `curl -s http://localhost:9099/metrics | grep simulator_coa_ack_latency_seconds` → histogram satirlari gorunmeli.
3. CoA exchangi sonrasinda: `histogram_quantile(0.99, ...)` < 200ms olmali.

### 5. Env Knob Dogrulama (AC-9)

1. `ARGUS_SIM_SESSION_RATE_PER_SEC=5 docker compose ... up simulator` → rate.max_radius_requests_per_second=5 ile baslamali.
2. `ARGUS_SIM_VIOLATION_RATE_PCT=10 docker compose ... up simulator` → aggressive_m2m weight ~%10 olmali.
3. `ARGUS_SIM_DIAMETER_ENABLED=false docker compose ... up simulator` → Diameter CCR paketleri gonderilmemeli.
4. Gecersiz deger: `ARGUS_SIM_SESSION_RATE_PER_SEC=0` → simulatoru baslatmamali; hata mesaji `rate must be > 0` icermeli.
5. `docs/architecture/CONFIG.md` → "Simulator Environment (dev/demo only)" bolumu `ARGUS_SIM_SESSION_RATE_PER_SEC`, `ARGUS_SIM_VIOLATION_RATE_PCT`, `ARGUS_SIM_DIAMETER_ENABLED`, `ARGUS_SIM_SBA_ENABLED`, `ARGUS_SIM_INTERIM_INTERVAL_SEC` satirlarini icermeli.

---

## FIX-227: APN Connected SIMs SlidePanel — CDR + Usage graph + quick stats

### 1. Kimlik Karti + SIM Satirina Tiklanma (AC-1)

1. `make up` → `http://localhost:8084/login` → admin@argus.io / admin ile giris yap.
2. Sol menuden **APNs** → herhangi bir APN → **Connected SIMs** sekme.
3. Tablodaki herhangi bir SIM satirina tikla → sag taraftan `SlidePanel` acilmali.
4. Panel icinde **Identity** karti gorunmeli: ICCID (mono), IMSI (mono), MSISDN, State (`<Badge>`), Policy (policy_name veya "None"), Last Session (relatif sure veya "—").
5. Klavye erisilebilirligi: SIM satirina **Tab** ile odaklan → **Enter** tusa bas → panel acilmali. **ESC** → panel kapanmali.

### 2. Kullanim Grafigi + CDR Ozeti (AC-2)

1. Panel acikken **Usage (last 7 days)** karti gorunmeli.
2. Aktif CDR verisi olan bir SIM icin: iki sparkline (Data In — accent renk, Data Out — mor renk) + Total In / Total Out metin degerlerini icermeli.
3. 7 gun icinde verisi olmayan bir SIM icin: "No usage data in last 7 days" mesaji gorunmeli.
4. **CDR Summary (7d)** karti: Sessions, Total Bytes, Avg Duration satirlari gorunmeli.
5. "Top Destinations" satiri dim (soluk) + "(coming soon)" etiketi gorunmeli — herhangi bir API cagrisi yapilmamali (DevTools Network: top-destinations endpoint yok).

### 3. Hizli Aksiyonlar (AC-3)

1. **View Full Details** butonuna tikla → `/sims/<id>` sayfasina gitmeli.
2. **View CDRs** butonuna tikla → `/cdrs?sim_id=<id>` sayfasina gitmeli (FIX-214 CDR Explorer).
3. **Suspend** butonu: SIM `active` durumundayken etkin olmali → tiklaninca mutation ateislenmeli, toast + undo gorunmeli, panel kapanmali.
4. **Suspend** butonu: SIM `active` degilse (suspended/inactive) devre disi (disabled) gorunmeli.

### 4. Tembel Veri Cekme (AC-4)

1. DevTools Network sekmesini ac.
2. Connected SIMs tabine git — panel acilmadan `/sims/{id}/usage`, `/cdrs/stats`, `/sims/{id}/sessions` endpoint cagrilari OLMAMALI.
3. Bir SIM satirina tikla (panel ac) → bu uc endpoint cagrisi tetiklenmeli.
4. Paneli kapat (`onOpenChange` false) → devam eden istekler iptal edilmeli (pending requests → cancelled/aborted).

### 5. Hata Durumu

1. Tarayici DevTools'ta Network → simule edilmis network hatasi icin bir isteği sag tikla → "Block request URL" ile `/sims/{id}/usage` engelle.
2. Panel ac → Usage karti "Failed to load usage" mesaji + `AlertCircle` ikonu gostermeli.
3. Ekranin ust kosesinde `sonner` toast hatasi gorunmeli (tek toast, tekrar acilip kapansa bile yeni toast uretmemeli — stable id ile deduplication).

---

## FIX-228: Login Forgot Password Flow + Version Footer

### 1. Forgot Link + Klavye Erisilebilirligi (AC-1)

1. `make up` → `http://localhost:8084/login` → admin@argus.io / admin ile giris yap, sonra cikis yap.
2. Login sayfasinda "Forgot password?" (veya "Parolamı unuttum?") baglantisini gor — submit butonunun altinda olmali.
3. Baglantiyi **Tab** tusu ile odakla → **Enter** tusa bas → `/auth/forgot` sayfasina gitmeli.
4. Tarayici URL'si `/auth/forgot` gostermeli; sayfa "Reset your password" basligiyla acilmali.

### 2. Forgot Form — Var Olan E-posta (AC-2, AC-3)

1. `/auth/forgot` sayfasinda gecerli bir e-posta gir (ornegin: `admin@argus.io`).
2. **Send reset link** butonuna tikla.
3. Form kaldirili, yerine basarili banner gorunmeli: "If that email exists, a reset link has been sent." (tam metin).
4. `http://localhost:8025` (Mailhog Web UI) → gelen kutusunda `admin@argus.io` adresine gelen sifre yenileme e-postasi gorunmeli.
5. E-posta icindeki `/auth/reset?token=<token>` baglantisini kopyala.

### 3. Forgot Form — Var Olmayan E-posta (AC-2 — Enumeration Defense)

1. `/auth/forgot` → `nonexistent@example.com` gir → **Send reset link** tikla.
2. AYNI basarili banner gorunmeli: "If that email exists, a reset link has been sent."
3. Gercek e-posta ile yanit byte-identic olmali — DevTools Network sekmesinde response body ayni olmali.
4. Mailhog'da bu adrese hic e-posta gitmemeli.

### 4. Rate Limit (AC-7)

1. `/auth/forgot` → ayni e-posta adresiyle ard arda 6 kez gonder (5 + 1).
2. 6. istekte "Too many requests. Please try again later." hata mesaji gorunmeli (form sayfasinda inline).
3. Hata mesaji "email" veya "password reset" ifadesi icermemeli (generic rate-limit, kaynak tespiti engelleme).

### 5. Reset URL — Gecerli Token (AC-4, AC-5)

1. Mailhog'dan alinan reset linkini tarayicida ac: `/auth/reset?token=<token>`.
2. "Set a new password" formu gorunmeli (2 parola alani).
3. Yeni guclu bir parola gir (ornegin: `NewPassword@2026!`), onay alanini ayni degerle doldur.
4. **Set new password** tikla → basarili toast "Password reset successful" gorunmeli → `/auth/login` sayfasina yonlendirilmeli.
5. Yeni parola ile giris yap → giris basarili olmali.
6. Ayni token ile tekrar `/auth/reset?token=<token>` ziyaret et → "This reset link is invalid or has expired" hata paneli gorunmeli (tekrar kullanim engeli — AC-5).

### 6. Reset URL — Gecersiz / Eksik Token (AC-4)

1. `/auth/reset` (token parametresi yok) ziyaret et → inline hata paneli gorunmeli: "This reset link is invalid or has expired. Request a new one." — form gorunmemeli, toast veya yonlendirme olmamali.
2. `/auth/reset?token=INVALID_GARBAGE` ziyaret et → ayni inline hata paneli gorunmeli.
3. "Request a new one" baglantisina tikla → `/auth/forgot` sayfasina gitmeli.

### 7. Parola Politikasi Hatasi (AC-4)

1. Gecerli bir reset token ile `/auth/reset?token=<token>` ac.
2. Cok kisa bir parola gir (ornegin: `abc`) → sunucu hatasi inline gorunmeli (`PASSWORD_TOO_SHORT` veya benzeri policy kodu).
3. Parola alani `autocomplete="new-password"` ozelligi tasimali (DevTools Elements incelemesi).

### 8. Versiyon Altbilgisi (AC-8)

1. `/auth/login`, `/auth/forgot`, `/auth/reset` sayfalarinin alt kisminda `Argus v0.1.0` (veya guncel paket versiyonu) metni gorunmeli.
2. Metin `text-text-secondary text-xs` stilinde olmali — ana icerigin dikkatini dagitrnamali.
3. Dashboard veya diger sehifeler (uygulama icinde) altbilgi gostermemeli — sadece AuthLayout sarmali altinda olmali.

### 9. Mailhog Dev Fixture (AC-3 dev check)

1. `http://localhost:8025` → Mailhog Web UI erisim saglamali.
2. Yeni bir reset istegi gonder → email `To:` alani dogru adrese gitmeli.
3. E-posta icerigi: reset linki (`/auth/reset?token=...`), gecerlilik suresi (1 saat), "ignore if not you" notu.
4. Identity karti her durumda gorunur kalmali (hata olsa bile).

## FIX-229: Alert Feature Enhancements (Mute UX, Export, Similar Clustering, Retention)

### 1. Uyarı Susturma — Ad-hoc Mute (AC-1)

1. `make up` → `http://localhost:8084/login` → admin@argus.io / admin ile giris yap.
2. `/alerts` sayfasina git → herhangi bir uyarı satırında "Mute" butonunu tikla.
3. MutePanel SlidePanel acilmali: Scope radio (this/type/operator/dedup_key), Duration radio (1h/24h/7d/Custom), Reason textarea, "Save as rule" toggle.
4. Scope = "this", Duration = "1h" sec → Reason gir → **Mute** butonuna tikla.
5. 201 yaniti gelmeli; uyarı satirinin state'i "suppressed" olarak guncellenmeli veya liste yenilenmeli.
6. Ayni uyarı satirinda "Unmute" seçeneği gorunmeli → tikla → UnmuteDialog confirm paneli acilmali.
7. Confirm → uyarı listede tekrar "open" olarak gorunmeli.

### 2. Uyarı Dışa Aktarma — Tri-Format Export (AC-2)

1. `/alerts` sayfasina git → Export dropdown'i tikla (3 seçenek: CSV, JSON, PDF).
2. **CSV**: "Export as CSV" tikla → `alerts.csv` indirilmeli; dosya içinde tablo basliklari ve uyari satırlari olmali.
3. **JSON**: "Export as JSON" tikla → `alerts.json` indirilmeli; JSON dizi yapisinda olmali.
4. **PDF**: "Export as PDF" tikla → `alerts.pdf` indirilmeli; PDF dosyası acilabilmeli.
5. Filtre uygula (örn. severity=critical) → export yeniden yapilinca sadece kritik uyarilari icermeli.
6. Sifir sonuc veren filtre ile export yap → 404 `ALERT_NO_DATA` hatasi inline gorunmeli (indirme olmamali).

### 3. Benzer Uyarılar — Similar Clustering (AC-3)

1. `/alerts` sayfasinda herhangi bir uyarı satirina tikla → row-expand acilmali.
2. "Details" ve "Similar(N)" olmak uzere iki sekme gorunmeli.
3. "Similar(N)" sekmesini tikla → dedup_key veya type+source eslesmesine gore benzer uyarilarin listesi gorunmeli.
4. "View all similar" baglantisi varsa tikla → `/alerts?dedup_key=<k>` veya `?type=<t>&source=<s>` URL'ine yonlendirmeli; sayfa dogru filtrelenmiş listeyi gostermeli.
5. Benzer uyari yok ise bos durum metni gorunmeli (nil yerine bos liste — hata degil).

### 4. Uyarı Saklama Suresi — Retention Setting (AC-4)

1. `/settings/alert-rules` sayfasina git → "Retention" bolumunu bul.
2. Alan bos birakilirsa "Required" hata mesaji gorunmeli.
3. 30'dan kucuk veya 365'ten buyuk deger girilirse "Must be between 30 and 365" mesaji gorunmeli.
4. Gecerli deger (örn. 90) girilip kaydedilirse 200 yaniti gelmeli; sayfa yenilenmesinde deger korunmali.
5. Tenant UPDATE endpoint'i (`PATCH /api/v1/tenants/{id}`) `alert_retention_days` key'ini `settings` JSONB icinde guncelledigini dogrulamak icin DevTools → Network sekmesinde istek body'sini incele.

### 5. Kaydedilmiş Kural Yönetimi — Saved Alert Rules (AC-5)

1. `/settings/alert-rules` sayfasina git → mevcut kural listesi gorunmeli (bos olabilir).
2. MutePanel'de "Save as rule" toggle'i ac → `rule_name` input alani acilmali → benzersiz bir isim gir.
3. Kaydedilince kural `/settings/alert-rules` listesinde gorunmeli; scope_type, expires_at, reason sutunlari olmali.
4. Ayni `rule_name` ile tekrar kaydetmeye calis → 409 `DUPLICATE` hatasi inline gorunmeli.
5. Kural satirinda "Delete" / Unmute Dialog → onayla → kural listeden kaldirilmali.

---

## FIX-230: Rollout DSL Match Integration

> **Backend-only story — no UI scenario.** All acceptance criteria are backend/store/DSL layer changes. Doğrulama için `make test` (3662 test PASS) yeterlidir.

### API-Level Test Scenario (AC-1..9)

1. `make up` → sistem ayakta olmalı.
2. `POST /api/v1/policies/{id}/versions` isteği gönder; body'de `MATCH { apn = "data.demo" }` içeren DSL kullan.
   - Beklenen: `data.affected_sim_count` alanı yanıtta dolu olmalı (örn. `7`); `meta.warnings` alanı YOK olmalı.
3. Aynı isteği DSL'siz (boş MATCH `{}`) yap → `affected_sim_count` tüm tenant SIM sayısını yansıtmalı (örn. `153`).
4. `POST /api/v1/policies/{id}/versions/{vid}/rollout` ile rollout başlat (`stages: [1, 50, 100]`).
   - Beklenen: `data.total_sims = 7` (NOT 153) — DSL eşleşen kohort.
5. Stage 0 çalıştır → `ceil(7 * 1 / 100) = 1` SIM migrate edilmeli; migrate edilen SIM'in `apn = "data.demo"` olmalı.
6. Bilinmeyen alan testi: `MATCH { iccid = "x" }` içeren DSL ile version oluşturmaya çalış → HTTP 422 `INVALID_DSL` hatası gelmeli.
7. SQL enjeksiyon testi: `MATCH { apn = "x' OR 1=1 --" }` içeren DSL ile version oluştur → istek başarılı olmalı (değer parametre olarak işlenir, SQL'e eklenmez); `affected_sim_count = 0` gelmeli (eşleşme yok).

---

## FIX-231: Policy Version State Machine + Dual-Source Fix

> **AC-1..9 ve AC-11 (backend/infra):** Bu acceptance criteria'lar veritabani kısıtları, trigger mekanizması, store katmanı ve arka plan job'larını kapsar. Doğrulama için `make test` (3581 test PASS) ve `make db-seed` PASS yeterlidir. Özel DB doğrulama: `docker exec argus-postgres psql -U argus -c "\di policy_active*"` — `policy_active_version` ve `policy_active_rollout` partial unique index'leri görünmeli. Trigger: `\df sims_policy_version_sync` sonucu dolu olmalı.

### 1. Policy Versiyonu Durum Çizelgesi — Versions Tab Timeline (AC-10)

1. `make up` → `http://localhost:8084/login` → admin@argus.io / admin ile giriş yap.
2. Sol menüden **Policies** sayfasına git → herhangi bir policy satırına tıkla → policy detay görünümü açılmalı.
3. **Versions** sekmesine tıkla → sekme içinde "Version Lifecycle" bölümü görünmeli.
4. Her versiyon için bir düğüm (node) olmalı: sol baştan sağa `created_at` ASC sıralamasıyla dizilmeli.
5. Durum renk kodları doğru olmalı:
   - `draft` → ikincil metin rengi + yükseltilmiş arkaplan (gri ton) — hiçbir `text-gray-NNN` sınıfı kullanılmamalı.
   - `rolling_out` → uyarı rengi + nabız animasyonu (`animate-pulse` veya benzeri) — sarı/amber hardcoded değil.
   - `active` → başarı rengi + kenar halkası (ring) — yeşil hardcoded değil.
   - `superseded` → üçüncül metin rengi + üstü çizili — gri hardcoded değil.
   - `rolled_back` → tehlike rengi — kırmızı hardcoded değil.
6. Aktif versiyon düğümünün üzerine fareyi getir → tooltip açılmalı; içinde `activated_at` tarihi görünmeli.
7. `rolled_back_at` dolu olan bir versiyonun üzerine fareyle gel → tooltip'te `rolled_back_at` tarihi de görünmeli.
8. Hiç versiyon yoksa "Version Lifecycle" bölümü tamamen gizlenmeli (boş durum render edilmemeli).
9. **Klavye erişilebilirliği:** Tab tuşuyla her versiyona odaklanılabilmeli; Enter/Space ile tooltip açılabilmeli (ya da tooltip focus ile de tetiklenmeli). Ekran okuyucu: her düğümde `aria-label="v2 — active, activated 22 April 2026"` formatında anlamlı etiket olmalı.
10. **Tasarım token doğrulaması:** DevTools → Elements → herhangi bir versiyon düğümü chip'ini seç → `class` listesinde `text-[#...]` veya `text-green-NNN`, `text-yellow-NNN` vb. hardcoded Tailwind palet sınıfı bulunmamalı; yalnızca `text-success`, `text-warning`, `text-danger`, `text-text-secondary`, `bg-bg-elevated` gibi CSS değişken tabanlı token sınıfları olmalı (PAT-018).

---

## FIX-232: Rollout UI Active State

> Setup: `make up` → `http://localhost:8084/login` → admin@argus.io / admin → Sol menüden **Policies** → herhangi bir policy satırına tıkla → policy detay görünümü → **Rollout** sekmesi.
>
> Full test requires a seeded rollout (state=in_progress). `make db-seed` provides seed data. Alternatively create a rollout manually: `POST /api/v1/policy-versions/{versionId}/rollout`.

### 1. State-Aware Render — Active vs Idle (AC-1)

1. Policy'nin aktif bir rolloutu yokken Rollout sekmesini aç → Selection cards görünmeli (Direct Assign + Staged Canary); `RolloutActivePanel` **görünmemeli**.
2. State=`in_progress` rolloutu olan bir policy'ye git → Rollout sekmesinde selection cards **görünmemeli**; bunun yerine `RolloutActivePanel` görünmeli.
3. State=`pending` rolloutu olan bir policy'ye git → `RolloutActivePanel` görünmeli (Advance/Rollback/Abort butonları enabled; Advance only enabled when current stage is complete).
4. Terminal state'teki rollout (completed/rolled_back/aborted) → Selection cards görünmeli + üstte terminal summary banner. Banner'da rollout terminal timestamp locale-formatted `<time>` içinde görünmeli.

### 2. Active Panel İçeriği (AC-2)

1. In-progress rollout olan bir policy'de Rollout sekmesine git.
2. Panel header'da: state badge (`IN_PROGRESS`), rollout ID (ilk 8 + son 4 karakter), `started_at` göreli süre görünmeli.
3. Strategy satırı: tek stage + %100 ise "Direct" göstermeli; birden fazla stage ise "Staged Canary" göstermeli.
4. Per-stage cards: her stage için status icon + % + migrated count. Active stage `accent` border/bg; completed stage `success` border/bg; pending stage `border-subtle`.
5. Progress bar: `migrated_sims / total_sims` yüzdesi; fill rengi state'e göre token class kullanmalı (in_progress → `bg-gradient-to-r from-accent to-accent/70`).
6. ETA alanı: yeterli veri varsa `~Xm for current stage` formatında; yeterli veri yoksa "—" göstermeli.
7. WS üzerinden veri gelince CoA counter satırı güncellenmeli: `N acked · M failed` format (font-mono).
8. Panel `role="region" aria-label="Active rollout panel"` attribute'larına sahip olmalı.
9. Progress bar `role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}` attribute'larına sahip olmalı.

### 3. Action Buttons + Confirm Dialogs (AC-3, AC-7)

#### 3a. Advance (AC-3)

1. Current stage status = `completed` ise **Advance Stage** butonu görünür ve enabled olmalı; son stage ise veya stage `in_progress` ise hidden/disabled olmalı.
2. Advance butonuna tıkla → Dialog açılmalı: "Advance to next stage. Current stage is complete. Continue?" + [Cancel] + [Advance] butonları.
3. [Cancel] → Dialog kapanmalı; mutasyon yapılmamalı.
4. [Advance] → `POST /api/v1/policy-rollouts/{id}/advance` çağrılmalı → 200 response → panel güncellenmeli.

#### 3b. Rollback (AC-7 — Destructive)

1. **Rollback** butonu görünür ve enabled olmalı (state=pending/in_progress); terminal state'de hidden olmalı.
2. Rollback butonuna tıkla → Dialog açılmalı; "Rollback rollout? This will revert all migrated SIMs to the previous policy version and fire CoA. **Destructive.**" metni + `border-danger` styling + [Cancel] + [Confirm Rollback] butonları.
3. [Confirm Rollback] → `POST /api/v1/policy-rollouts/{id}/rollback` çağrılmalı → 200 → Rollout tab selection cards + terminal banner ("Rolled back at X") göstermeli.

#### 3c. Abort (AC-3, AC-6 — Warning, Non-Reverting)

1. **Abort** butonu görünür ve enabled olmalı (state=pending/in_progress); terminal state'de hidden olmalı.
2. Abort butonuna tıkla → Dialog açılmalı; "Abort rollout? Already-migrated SIMs WILL stay on the new policy. CoA will NOT fire." metni + `border-warning` styling (danger değil!) + [Cancel] + [Confirm Abort] butonları.
3. [Confirm Abort] → `POST /api/v1/policy-rollouts/{id}/abort` çağrılmalı → 200 response body'de `data.state = "aborted"` ve `data.aborted_at` dolu olmalı.
4. Abort sonrası Rollout tab selection cards + terminal banner ("Aborted at X") göstermeli.
5. **Rollback ile görsel fark:** Rollback butonu/dialog kırmızı/danger ton; Abort sarı/warning ton — aynı görünmemeli.
6. Zaten aborted rollout için abort isteği → 422 `ROLLOUT_ABORTED` hatası; UI toast/hata mesajı göstermeli.

#### 3d. View Migrated SIMs

1. "View Migrated SIMs" bağlantısına tıkla → `/sims?rollout_id={rolloutId}` URL'ine yönlenmeli (FIX-233 öncesi liste filtre çalışmayabilir; URL doğru olmalı).

### 4. Abort Endpoint Doğrulaması — Backend (AC-6)

1. DevTools → Network → Abort confirm → `POST /api/v1/policy-rollouts/{id}/abort` isteği gözlemlenmeli; response 200, body `{status:"success", data:{state:"aborted", aborted_at:"..."}}.
2. Audit log: `/audit?entity_id={rolloutId}&action_prefix=policy_rollout` → `policy_rollout.abort` action'ı listelenmiş olmalı.
3. Daha önce abort edilmiş bir rollout'a abort isteği gönder → 422 `ROLLOUT_ABORTED` hatası gelmeli (idempotent guard).
4. Completed rollout'a abort isteği → 422 `ROLLOUT_COMPLETED` hatası gelmeli.
5. Rolled-back rollout'a abort isteği → 422 `ROLLOUT_ROLLED_BACK` hatası gelmeli.

### 5. WebSocket Live Update (AC-5)

1. In-progress rollout paneli açıkken backend'den stage advance yap (başka tarayıcı veya `curl POST /advance`).
2. Sayfayı yenilemeden birkaç saniye içinde panel'in progress bar ve stage kartları güncellenmiş olmalı (WS `policy.rollout_progress` envelope tetikler GET refetch).
3. DevTools → Network → WS tab → `ws://localhost:8081` connection → `policy.rollout_progress` mesajlarını gözlemle.

### 6. Polling Fallback — WS Disconnected (AC-8)

1. In-progress rollout paneli açıkken DevTools → Network → WS bağlantısını blokla (`ws://localhost:8081`).
2. Panel footer'da ~5s içinde "WS disconnected · polling every 5s" metni görünmeli (warning rengi).
3. Backend'de manuel stage advance yap → 5s içinde panel GET isteği atarak güncellenmeli (Network sekmesinde `GET /policy-rollouts/{id}` isteği görünmeli).
4. WS bloğunu kaldır → bağlantı yeniden kurulunca "WS connected" footer metni geri gelmeli; polling interval durmalı.

### 7. Expanded SlidePanel — Drill-downs (AC-11)

1. Active panel header'da "Open expanded view ↗" butonuna tıkla → SlidePanel (sağ drawer) açılmalı.
2. SlidePanel içinde: header (state + strategy), expanded stage listesi (timestamps ile), 4 drill-down linki görünmeli.
3. Drill-down linkleri:
   - "View Migrated SIMs" → `/sims?rollout_id={id}`
   - "CDR Explorer" → `/cdr?rollout_id={id}`
   - "Sessions filtered to rollout cohort" → `/sessions?rollout_id={id}`
   - "Audit log entries for this rollout" → `/audit?entity_id={id}&action_prefix=policy_rollout`
4. Her link tıklanabilir olmalı; `<a>` değil `<Link>` (react-router) kullanılmalı (hard refresh olmadan yönlenmeli).
5. SlidePanel kapatma (× veya dışarı tıklama) → panel kapanmalı; rollout-tab hâlâ aktif panel ile görünmeli.

### 8. Terminal Summary Banner (AC-1)

1. Completed rollout olan policy'de Rollout sekmesini aç → Banner görünmeli: "Last rollout (id Xyyy…) completed at 2026-04-XX" — tarih locale-formatted `<time>` tag ile.
2. Aborted rollout → "Aborted at X" timestamp.
3. Rolled-back rollout → "Rolled back at X" timestamp.
4. Banner'da "View summary" linki varsa tıkla → Expanded SlidePanel açılmalı.

### 9. Design Token / A11y Doğrulaması (PAT-018)

1. DevTools → Elements → `RolloutActivePanel` root element → herhangi bir child class listesinde `text-red-NNN`, `bg-blue-NNN`, `text-green-NNN` vb. hardcoded Tailwind palet utility bulunmamalı; yalnızca `text-danger`, `text-success`, `text-warning`, `bg-bg-surface`, `bg-accent-dim` vb. CSS değişken tabanlı token sınıfları olmalı.
2. Progress bar element'ini incele → `role="progressbar"`, `aria-valuenow`, `aria-valuemin="0"`, `aria-valuemax="100"` attribute'ları mevcut olmalı.
3. Abort ve Rollback butonlarında `aria-label` mevcut olmalı ve boş olmamalı.

---

## FIX-233: SIM List Policy column + Rollout Cohort filter

> **UI Smoke Durumu:** F-U1 (Global React #185 crash — `useFilteredEventsSelector` re-render loop) nedeniyle SIM listesi sayfası açılırken React kilitlenebilir. Bu, FIX-249 kapsamında düzeltilecek PRE-EXISTING hatadır; FIX-233 regresyonu DEĞİLDİR.
> **BLOCKED by FIX-249:** UI tabanlı senaryolar FIX-249 tamamlanana kadar çalışmayabilir. Network katmanı (curl) senaryoları bu süre zarfında primer doğrulama yöntemidir.

### 1. Policy Sütunu Görünümü (AC-6)

> BLOCKED by FIX-249 — curl ile network katmanı doğrulaması:

1. `curl -s -H "Authorization: Bearer $TOKEN" "http://localhost:8080/api/v1/sims?limit=20" | jq '.data[].policy_name'` → politika atanmış SIM'ler için `"Demo Premium v3"` vb. değer, atanmamış SIM'ler için `null` gelmeli.
2. `curl -s ... | jq '.data[].policy_id'` → policy_id UUID değeri gelmeli (atanmış SIM'lerde).
3. UI mevcut olduğunda: SIM listesinde 13. kolon "Policy" başlıklı olmalı; "Demo Premium v3" linki tıklanabilir → `/policies/{policy_id}` sayfasına yönlendirmeli. Politikası olmayan SIM satırında "—" gösterilmeli.

### 2. Policy Filter Chip (AC-7 — PARTIAL)

> AC-7 kısmi uygulama: Politika adı chip'i çalışır, ancak versiyon alt menüsü D-141 kapsamında ertelenmiştir.

1. UI smoke (FIX-249 sonrası): Filter bar'da "Policy" chip'ini tıkla → mevcut politikalar dropdown olarak listelensin.
2. Bir politika seç → URL `?policy_id={uuid}` parametresi eklenmeli.
3. curl doğrulaması: `curl -s ... "http://localhost:8080/api/v1/sims?policy_id={geçerli-uuid}"` → yalnızca o politikaya atanmış SIM'ler gelmeli.
4. `curl -s ... "http://localhost:8080/api/v1/sims?policy_id=gecersiz-uuid"` → `400 INVALID_PARAM` hatası gelmeli.
5. Versiyon alt menüsü: D-141 kapsamında ertelenmiştir; `policy_version_id` URL parametresi doğrudan girilebilir ve çalışır.

### 3. Cohort Filter Chip (AC-7 — Cohort kısmı PASS)

1. `curl -s -H "Authorization: Bearer $TOKEN" "http://localhost:8080/api/v1/policy-rollouts?state=pending,in_progress&limit=10" | jq '.'` → aktif rollout listesi gelmeli (boş liste de kabul edilir: `[]`).
2. Rollout varsa: `curl -s ... "http://localhost:8080/api/v1/sims?rollout_id={rollout_uuid}"` → o rollout'a atanmış SIM'ler gelmeli.
3. `curl -s ... "http://localhost:8080/api/v1/sims?rollout_id={rollout_uuid}&rollout_stage_pct=10"` → stage 10'a atanmış SIM'ler gelmeli.
4. `curl -s ... "http://localhost:8080/api/v1/sims?rollout_stage_pct=150"` → `400 INVALID_PARAM` (1-100 dışı) gelmeli.
5. UI smoke (FIX-249 sonrası): "Cohort" chip'i tıkla → rollout isimleri listesi gösterilmeli; rollout seçilince stage alt menüsü `[1, 10, 100]` görünmeli.

### 4. URL Deep-Linking (AC-8)

1. Tarayıcıda direkt URL gir: `/sims?policy_id={uuid}` → sayfa açıldığında Policy chip'i zaten seçili gelsin.
2. `/sims?rollout_id={uuid}&rollout_stage_pct=10` → Cohort chip seçili + stage=10 seçili olmalı.
3. `/sims?policy_version_id={uuid}` → policy_version_id parametresi query'ye eklenmeli ve filtreleme çalışmalı.
4. Filtreler temizlenince URL parametreleri de temizlenmeli.

### 5. View Cohort Linki — RolloutActivePanel (AC-9)

1. `/policies/{id}` sayfasında aktif rollout varsa `RolloutActivePanel` render olmalı.
2. "View cohort" linkine tıkla → `/sims?rollout_id={rolloutId}&rollout_stage_pct={currentStage}` URL'ine yönlenmeli.
3. SIM listesi açıldığında Cohort chip'i o rollout + stage için seçili olmalı.
4. curl ile: Link URL'ini kontrol et → hem `rollout_id` hem `rollout_stage_pct` parametreleri içermeli.

### 6. WS Refetch (AC-9 — WS kısmı)

1. Açık SIM listesinde (FIX-249 sonrası) terminal'den `curl -X POST .../policy-rollouts/{id}/advance` yap.
2. Birkaç saniye içinde SIM listesi sayfasının yeniden çekildiğini gözlemle (DevTools → Network → `GET /sims` yeni isteği).
3. DevTools → Network → WS tab → `ws://localhost:8081` → `policy.rollout_progress` mesajı tetiklendiğinde refetch.

### 7. Backend Parametre Validasyonu (AC-4)

1. `curl -s ... "http://localhost:8080/api/v1/sims?policy_id=not-a-uuid"` → `400 INVALID_PARAM`.
2. `curl -s ... "http://localhost:8080/api/v1/sims?rollout_id=not-a-uuid"` → `400 INVALID_PARAM`.
3. `curl -s ... "http://localhost:8080/api/v1/sims?rollout_stage_pct=0"` → `400 INVALID_PARAM` (0 kabul edilmez).
4. `curl -s ... "http://localhost:8080/api/v1/sims?rollout_stage_pct=101"` → `400 INVALID_PARAM`.
5. Geçerli kombinasyon: `?rollout_id={uuid}&rollout_stage_pct=10` → 200 ve filtreli sonuç.

### 8. SIM DTO Alanları (AC-5)

1. `curl -s ... "http://localhost:8080/api/v1/sims/{sim_id}" | jq '{policy_name, policy_version, policy_version_id, policy_id, rollout_id, rollout_stage_pct, coa_status}'` → politika atanmış SIM'de bu alanların varlığını kontrol et.
2. Politika atanmamış SIM için: `policy_name`, `policy_id`, `policy_version_id` → `null` veya absent (omitempty); `rollout_id`, `rollout_stage_pct`, `coa_status` → absent.
3. `policy_id` UUID, clickable link için kullanılır; `policy_version_id` filtre hedefi için kullanılır.

### 9. Policy JOIN performansı (AC-10)

1. `EXPLAIN ANALYZE` ile `SELECT ... FROM sims LEFT JOIN policy_assignments pa ON pa.sim_id = sims.id WHERE sims.tenant_id = $1` çalıştır.
2. `idx_policy_assignments_sim` index scan kullanıldığını doğrula (Seq Scan olmamalı).
3. Cohort filtreli sorgu: WHERE koşuluna `pa.rollout_id = $X AND pa.stage_pct = $Y` eklendiğinde `idx_policy_assignments_rollout_stage` composite index kullanıldığını doğrula.
4. Referans: `docs/stories/fix-ui-review/FIX-233-perf.md` — dev-scale p95 = 9.82ms, AC-10 PASS. Staging-scale (10K+ SIM) validasyonu D-143 kapsamında ertelenmiştir.

### 10. Migration ve Index Kontrolü

1. `psql $DATABASE_URL -c "\d policy_assignments"` → `stage_pct integer` sütunu görünmeli.
2. `psql $DATABASE_URL -c "\di policy_assignments*"` → `idx_policy_assignments_rollout_stage` index'i listelenmiş olmalı.
3. `psql $DATABASE_URL -c "SELECT migration_name FROM schema_migrations ORDER BY migration_name DESC LIMIT 5"` → `20260429000001_policy_assignments_stage_pct` en son migration'lardan biri olmalı.

---

## FIX-249: Global React #185 crash — useFilteredEventsSelector yeniden render döngüsü

> **Kapsam:** `web/src/components/event-stream/event-stream-drawer.tsx` — `useShallow` wrap ile Zustand v5 selector referans kararlılığı sağlandı.
> **Bağımlılık:** Bu story FE-only hotfix'tir; backend değişikliği yok.

### Senaryo 1 — Konsol temiz kontrolü (AC-2 keystone)

1. Tarayıcıyı aç, `http://localhost:8084/login` → `admin@argus.io` / `admin` ile giriş yap.
2. DevTools → Console sekmesini aç; "Errors" ve "Warnings" filtrelerini etkin bırak.
3. `/dashboard` rotasına geç. Console'da `React Minified Error #185` veya `Maximum update depth exceeded` hatası OLMAMALI.
4. Aynı kontrolü şu rotalarda tekrarla: `/sims`, `/policies`, `/sessions`, `/policies/<herhangi-bir-policy-id>`.
5. **Beklenen:** Tüm 5 rotada console tamamen temiz — sıfır React #185 hatası. (Pre-FIX-249 baseline'da her koruyuculu rotada çökme vardı.)

### Senaryo 2 — Olay akışı drawer UX kontrolü (AC-3)

1. Sağ üstteki Activity / Bell ikonuna tıkla → "Canlı Olay Akışı" drawer sağdan açılmalı.
2. Severity chip'leri (CRI / HIG / MED / LOW / INF) görünmeli ve tıklanabilir.
3. Bir chip'e tıkla → aktif/pasif toggle çalışmalı; filtreli olay sayısı güncellenmeli.
4. "Duraklat" düğmesine tıkla → yeni olay akışı durur, kuyruk sayacı artar. "Devam Et" → akış yeniden başlar.
5. Drawer'ı kapat (X veya dışarı tıkla) → kapanmalı. Tekrar aç → filtre durumu korunmayabilir (state sıfırlanır) — bu beklenen davranış.
6. **Beklenen:** Tüm işlemler süresince console temiz kalır.

### Senaryo 3 — Canlı olay testi (AC-3 live smoke)

1. Drawer açıkken `/sims` sayfasına git (drawer overlay olarak açık kalır).
2. Herhangi bir aktif SIM seç → "Suspend" aksiyon butonuna tıkla ve onayla.
3. **Beklenen:** Drawer'da 1-2 saniye içinde yeni "SIM active → suspended" olay satırı görünmeli; olay sayacı güncellenmeli (örn: "2/2 olay · 1 filtre aktif").
4. Console'da hata yok.

### Senaryo 4 — Idle gözlem (AC-1 referans kararlılığı)

1. `/sims` rotasında drawer KAPALI iken 5 saniye boyunca herhangi bir etkileşim yapma.
2. DevTools → Console'u gözlemle.
3. **Beklenen:** Sürekli warning/error spam'i OLMAMALI. Pre-FIX-249 baseline'da her saniye onlarca React re-render uyarısı üretiliyordu; şimdi sessiz olmalı.

### Senaryo 5 — Navigasyon stabilitesi (AC-2 multi-route)

1. `/sims` → `/policies` → `/sessions` → `/sims` rotaları arasında sırayla gez (her geçişte ~1 saniye bekle).
2. Her rota geçişinde console temiz kalmalı.
3. Son olarak drawer'ı aç → filter chips çalışmalı, console temiz.
4. **Beklenen:** 0 React #185 hatası; navigasyon boyunca uygulama kararlı kalır.

## FIX-250: Vite-native env access in info-tooltip

> `web/src/components/ui/info-tooltip.tsx` satır 47-48 — `process.env.NODE_ENV !== 'production'` yerine `import.meta.env.DEV` kullanılıyor. Build-time boolean; Vite prod bundle'da tree-shake edilir.

### Senaryo 1 — Container build temizliği (AC-4 keystone)

1. Proje kökünde `make build` komutunu çalıştır.
2. **Beklenen:** Komut hatasız tamamlanmalı. Önceden `Cannot find name 'process'` hatası ile düşüyordu (FIX-222 kalıntısı); FIX-250 sonrası bu hata ortadan kalkar.
3. Docker image `argus-argus:latest` başarıyla oluşturulmalı.

### Senaryo 2 — Dev mode davranışı korundu (AC-5)

1. `make web-dev` ile dev server'ı başlat.
2. InfoTooltip kullanan bir sayfaya git (örn. policy editor side panel veya IP pool detail sayfasındaki tooltip'li başlıklar).
3. Sözlük terimi tanımlı olmayan bir InfoTooltip görüntülenirse DevTools → Console'da `[InfoTooltip] unknown term:` içeren `console.warn` mesajı gözüklü olmalı.
4. **Beklenen:** Dev modda uyarı mesajı baskılanmıyor; önceki davranış korunmuş.

### Senaryo 3 — Prod bundle temiz (AC-5 prod tarafı)

1. `cd web && pnpm build` komutunu çalıştır.
2. **Beklenen:** Build hatasız tamamlanmalı.
3. Doğrulama: `grep -r "console\.warn" web/dist/assets/*.js | grep -i "infoTooltip\|unknown term"` → çıktı yok bekleniyor. Vite prod modunda `import.meta.env.DEV === false` olduğu için dev-only `console.warn` bloğu tree-shake ile bundle'dan çıkar.

## FIX-234: CoA Status Enum Extension + Idle SIM Handling + UI Counters

> Kapsam: 9 AC — DB enum genişletme, 6-state lifecycle, idle SIM re-fire, alerter, metrics, rollout panel breakdown, SIM detail InfoRow, PROTOCOLS.md.

### Senaryo 1 — Migration etkinliği (AC-1)

1. `make db-migrate` çalıştır → version `20260430000001_coa_status_enum_extension` uygulanmış olmalı.
2. Geçersiz değer eklemeyi dene:
   ```sql
   psql -c "BEGIN; UPDATE policy_assignments SET coa_status='invalid' WHERE id = (SELECT id FROM policy_assignments LIMIT 1); ROLLBACK;"
   ```
3. **Beklenen:** `ERROR: new row for relation "policy_assignments" violates check constraint "chk_coa_status"` (SQLSTATE 23514).
4. ROLLBACK nedeniyle veri değişmemeli.

### Senaryo 2 — Geçerli 6 state değeri (AC-2)

1. Aşağıdaki değerlerin her birini tek tek `coa_status`'a set et (test ortamında):
   `pending`, `queued`, `acked`, `failed`, `no_session`, `skipped`
2. **Beklenen:** Hiçbirinde CHECK constraint hatası yok. Constraint sadece listede olmayan değerlerde devreye girmeli.

### Senaryo 3 — Idle SIM → no_session (AC-3)

1. Aktif olmayan (session'ı olmayan) bir SIM'e yeni policy ata ve rollout başlat.
2. `SELECT coa_status FROM policy_assignments WHERE sim_id = '<SIM_ID>'` sorgusunu çalıştır.
3. **Beklenen:** `coa_status = 'no_session'` (önceki davranış: SIM sonsuz `'pending'`'de takılırdı).

### Senaryo 4 — Session başlatınca re-fire (AC-4)

1. `coa_status = 'no_session'` olan bir SIM'in RADIUS Access-Request isteği göndermesini simüle et (veya seed SIM'i kullan).
2. 60 saniye bekle (dedup window dışı).
3. `SELECT coa_status FROM policy_assignments WHERE sim_id = '<SIM_ID>'` sorgula.
4. **Beklenen:** `coa_status` → `queued` → sonra `acked` veya `failed` olarak güncellenmeli (`coaSessionResender` NATS queue group `rollout-coa-resend` üzerinden `ResendCoA` çağırır).

### Senaryo 5 — Re-fire dedup penceresi (AC-4 devamı)

1. `coa_status = 'no_session'` olan bir SIM için 60 saniye içinde iki kez session started eventi tetikle.
2. **Beklenen:** İkinci event yeni bir CoA dispatch'i tetiklemez — `coa_sent_at IS NOT NULL AND NOW() - coa_sent_at <= 60s` koşulu dedup window'u uygular.

### Senaryo 6 — Rollout panel 6-state breakdown (AC-5)

1. Aktif bir rollout içeren bir policy'ye git: `/policies/<id>` → Rollout sekmesi.
2. `RolloutActivePanel` içindeki CoA breakdown bölümünü incele.
3. **Beklenen:**
   - `acked` ve `failed` her zaman gösterilir (high-signal states).
   - `pending`, `queued`, `no_session`, `skipped` yalnızca 0'dan büyükse görünür.
   - Renkler: acked→`text-success`, failed→`text-danger`, queued/no_session→`text-accent`/`text-text-tertiary`.
   - Hexadecimal renk kodu veya Tailwind default palette kullanılmamış olmalı (PAT-018).

### Senaryo 7 — SIM Detail CoA Status satırı (AC-6)

1. `/sims/<id>` sayfasına git.
2. "Policy & Session" kartında "Policy" satırının hemen altında "CoA Status" InfoRow gözükür.
3. **Durum eşlemeleri:**
   - `pending` → sarı `text-warning` label
   - `queued` → mavi `text-info` chip "In Progress"
   - `acked` → yeşil `text-success`
   - `failed` → kırmızı `text-danger`; üstüne hover → tooltip "Last attempt failed. See policy event log for failure reason."
   - `no_session` / `skipped` → `text-text-tertiary` (muted)
   - Policy atanmamış SIM → `—` em-dash (`text-text-tertiary`)

### Senaryo 8 — Alerter tetiklenmesi (AC-7)

1. Test için bir SIM'in `policy_assignments` kaydını doğrudan güncelle:
   ```sql
   UPDATE policy_assignments SET coa_status='failed', coa_sent_at = NOW() - INTERVAL '6 minutes' WHERE sim_id='<SIM_ID>';
   ```
2. 60 saniye bekle (alerter `coa_failure_alerter` her dakika çalışır, cron `* * * * *`).
3. `SELECT * FROM alerts WHERE type='coa_delivery_failed' AND dedup_key='coa_failed:<SIM_ID>'` sorgula.
4. **Beklenen:** Alert oluşturulmuş; `severity` = `high`, `type` = `coa_delivery_failed`, `dedup_key` = `coa_failed:<SIM_ID>`.
5. Aynı koşul altında 2. sweep sonrasında yeni alert oluşmamalı (dedup `UpsertWithDedup`).

### Senaryo 9 — Prometheus metriği (AC-8)

1. Argus çalışırken bir dakika bekle (alerter ilk sweep tamamlanır).
2. `curl http://localhost:8080/metrics | grep argus_coa_status_by_state` çalıştır.
3. **Beklenen:** 6 satır (state="pending", "queued", "acked", "failed", "no_session", "skipped") döner. Değerler `policy_assignments` tablosundaki her state için gerçek satır sayılarıyla eşleşmeli.
   ```
   argus_coa_status_by_state{state="acked"} 112
   argus_coa_status_by_state{state="failed"} 0
   ...
   ```

## FIX-252: SIM Activate 500 — Schema Drift Recovery (Doc-Only Closure)

> **NOT:** Bu story kod değişikliği içermiyor. Symptom (`POST /sims/{id}/activate` 500 — IP-pool allocation failure on reactivate) `make db-reset` ile çözüldü. Discovery, root cause'un IP-pool semantics değil **schema drift** olduğunu ortaya çıkardı (`schema_migrations.version=20260430000001 dirty=false` iken `ip_addresses.last_seen_at` kolonu live DB'de YOKTU — SQLSTATE 42703). Defansif kod (empty-pool guard, audit-on-failure, regression test, suspend-IP-release) FIX-253'e devredildi. PAT-023 schema_migrations drift'i için bug-pattern olarak dosyalandı. DEV-386/387/388 decisions.md'de.

### Senaryo 1 — Round-trip suspend → activate doğrulaması (AC-1, AC-5)

1. Login: admin@argus.io / admin
2. Aktif bir SIM seç (admin tenant `00000000-0000-0000-0000-000000000001`):
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT id FROM sims WHERE tenant_id='00000000-0000-0000-0000-000000000001' AND state='active' AND apn_id IS NOT NULL LIMIT 1;"
   ```
3. SIM list ekranından (`/sims`) seçili SIM'in detayına git, "Suspend" butonuna bas.
4. **Beklenen:** HTTP 200, SIM `suspended` durumuna geçer; sayfa otomatik refresh.
5. Aynı SIM detayında "Activate" / "Resume" butonuna bas.
6. **Beklenen:** HTTP 200, SIM tekrar `active` durumuna geçer; yeni IP allocate edilir; `ip_address_id` dolu döner. **Bare 500 ASLA dönmemeli.**

### Senaryo 2 — Boot-time schema integrity check (PAT-023 ilk savunma hattı)

1. Argus container'ını restart et: `docker restart argus-app`.
2. Container loglarını izle: `docker logs argus-app --since 10s -f` (5 saniye bekle, sonra Ctrl+C).
3. **Beklenen:** Argus normal `starting argus` + `postgres connected` + `pprof server starting` log'ları gösterir; FATAL `schemacheck: critical tables missing` HİÇBİR koşulda görünmemeli.
4. Eğer FATAL görünürse: drift var. `make db-reset` ile schema'yı sıfırla, sonra container'ı tekrar başlat.

### Senaryo 3 — `schema_migrations` doğrulaması (PAT-023 manuel kontrol)

1. Versiyon kontrolü:
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT version, dirty FROM schema_migrations;"
   ```
2. **Beklenen:** Tek satır, `version` = `migrations/` dizinindeki en yüksek dosya versiyonu (ör. `20260430000001`), `dirty` = `f`.
3. Spot-check (FIX-252 sonrası garanti olması gereken objeler):
   ```
   docker exec argus-postgres psql -U argus -d argus -t -c "
   SELECT '20260424000003 ip_addresses.last_seen_at', EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='ip_addresses' AND column_name='last_seen_at')
   UNION ALL SELECT '20260425000001 password_reset_tokens', EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name='password_reset_tokens');"
   ```
4. **Beklenen:** Her iki satır da `t` döner. `f` dönerse drift var → `make db-reset` çalıştır + PAT-023 prosedürünü uygula.

### Senaryo 4 — FIX-253 ön-shadow (defansif kod kontrolü, FIX-253 sonrasına bırakıldı)

1. FIX-253 implement edildikten sonra: APN'i hiç IP pool'u olmayan bir SIM için `/activate` çağır:
   ```
   curl -i -X POST http://localhost:8084/api/v1/sims/<no-pool-sim-id>/activate -H "Authorization: Bearer $TOKEN"
   ```
2. **Beklenen (FIX-253 sonrası):** HTTP 422 + envelope `{"status":"error","error":{"code":"POOL_EXHAUSTED","message":"No IP pool configured for this APN"}}`. **Bare 500 ASLA dönmemeli.**
3. **NOT:** FIX-252 kapsamında bu davranış GARANTILI DEĞİL — sadece sembolik olarak FIX-253 hedefi belirleniyor. Ön-shadow scenario; FIX-253 USERTEST'inde detaylanacak.

## FIX-251: Stale "An unexpected error occurred" toast on /sims — Backend Root-Cause Fix

> **NOT:** Plan pivot — Discovery (FIX-251 W1 T1) revealed the toast originated from a **real backend 500** (`GET /api/v1/operators` returning 500 due to `OperatorStore.List` inline scan missing `sla_latency_threshold_ms` field added by FIX-215). silentPaths cosmetic patch REJECTED; backend bug fixed instead. PAT-006 RECURRENCE #3 dosyalandı. DEV-389 decisions.md'de.

### Senaryo 1 — /operators endpoint canlı doğrulama (AC-1, AC-2)

1. Login: admin@argus.io / admin
2. Curl ile doğrudan endpoint testi:
   ```
   TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login -H 'Content-Type: application/json' -d '{"email":"admin@argus.io","password":"admin"}' | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["token"])')
   curl -i -X GET "http://localhost:8084/api/v1/operators?limit=100" -H "Authorization: Bearer $TOKEN"
   ```
3. **Beklenen:** HTTP 200; `data` array minimum 4 operator döner (Turkcell, Vodafone, Türk Telekom, BiP). Her operator nesnesinde `sla_latency_threshold_ms` field POPULATED (default 500).
4. **Anti-regression:** HTTP 500 ASLA dönmemeli. `argus log | grep 'scan operator'` ZERO `got 20 and 19` mismatch error içermeli.

### Senaryo 2 — /sims sayfası cold load — toast yokluğu (AC-1, AC-3)

1. Browser'da DevTools Console + Network panellerini aç.
2. `/login` üzerinden auth ol → `/sims` sayfasına navigate et (cold load — hard reload Ctrl+Shift+R).
3. **Beklenen:**
   - SIM list sayfası yüklenir (boş veya dolu, herhangi bir state).
   - Toast NOTIFICATION area'da "An unexpected error occurred" mesajı SHOWN OLMAMALI.
   - Network panel: `/api/v1/operators?limit=100` HTTP 200 (yeşil); 500 (kırmızı) yok.
   - Console: error log yok (warn olabilir, error yok).

### Senaryo 3 — Health checker startup (boot-time AC-3)

1. `docker logs argus-app --since 1m | grep -iE 'health checker'`
2. **Beklenen:** `health checker started component=health_checker operator_count=4` (veya seed'deki operator sayısı). 
3. **Anti-regression:** `failed to start health checker — continuing without health checks` mesajı YOK.

### Senaryo 4 — Regression test suite (AC-4)

1. ```
   DATABASE_URL='postgresql://argus:argus_secret@localhost:5450/argus?sslmode=disable' go test -v -count=1 -run 'OperatorColumnsAndScan|OperatorStore_List' ./internal/store/
   ```
2. **Beklenen:** 3 test PASS (TestOperatorColumnsAndScanCountConsistency, TestOperatorStore_List_..., TestOperatorStore_ListActive_...). DATABASE_URL set DEĞİLSE son ikisi SKIP, ilki PASS.
3. **Geliştirici uyarısı:** Bu testler PAT-006 RECURRENCE'a karşı koruma sağlar. `operatorColumns` constant'ına yeni bir kolon eklenirse `TestOperatorColumnsAndScanCountConsistency` FAIL eder ve geliştiriciyi `List` + `ListActive` inline scan'lerini güncellemesi için zorlar.

## FIX-253: Suspend IP Release + Activate Empty-Pool Guard + Audit-on-Failure (FIX-252 spinoff)

> **Backend hardening.** `SIMStore.Suspend` artık dynamic IP'leri atomik olarak serbest bırakır (static IP'ler kullanıcı kararı per değişmez). `Handler.Activate` empty-pool durumda 422 POOL_EXHAUSTED döner (önceden bare 500). Her başarısız Activate/Resume branch'inde `sim.{activate,resume}.failed` audit log entry yazılır (after=`{reason, attempted_state}`). DEV-390/391/392/393 logged. Plan: `docs/stories/fix-ui-review/FIX-253-plan.md`.

### Senaryo 1 — Suspend → IP release atomik (AC-1)

1. Login: admin@argus.io / admin
2. Aktif SIM seç (admin tenant, dynamic IP allocated):
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT s.id, s.ip_address_id, i.allocation_type FROM sims s LEFT JOIN ip_addresses i ON i.id=s.ip_address_id WHERE s.tenant_id='00000000-0000-0000-0000-000000000001' AND s.state='active' AND s.ip_address_id IS NOT NULL AND i.allocation_type='dynamic' LIMIT 1;"
   ```
3. SIM detay sayfasından "Suspend" → curl ile de OK: `POST /sims/{id}/suspend`
4. **Beklenen DB doğrulaması:**
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT s.id, s.ip_address_id, i.state, i.sim_id FROM sims s LEFT JOIN ip_addresses i ON i.sim_id=s.id WHERE s.id='<suspended-sim-id>';"
   ```
   - `sims.ip_address_id` = NULL ✓
   - LEFT JOIN sıfır row döner (yani ip_addresses tablosunda `sim_id=<sim-id>` olan row YOK) ✓
   - Pool counter: `SELECT used_addresses FROM ip_pools WHERE id='<pool-id>'` — 1 azalmış ✓

### Senaryo 2 — Suspend → STATIC IP korunur (AC-1, kullanıcı kararı 2026-04-26)

1. Static IP'li bir SIM seç (`allocation_type='static'`).
2. Suspend.
3. **Beklenen:** `sims.ip_address_id` UNCHANGED (NULL OLMAMALI), ip_addresses row UNCHANGED (`state='allocated'`, `sim_id=<sim-id>` korunur), pool counter UNCHANGED. Test referansı: `TestSIMStore_Suspend_PreservesStaticIP`.

### Senaryo 3 — Activate empty-pool guard 422 (AC-2)

1. APN'i hiç IP pool'u olmayan bir SIM oluştur (test fixture):
   ```sql
   INSERT INTO apns (id, tenant_id, name, mcc, mnc) VALUES (gen_random_uuid(), '00000000-0000-0000-0000-000000000001', 'test-no-pool', '286', '01') RETURNING id;
   -- assign a SIM to this APN (or modify an existing test SIM's apn_id)
   ```
2. POST `/sims/{id}/activate` → **Beklenen:** HTTP **422** (NOT 500), envelope `{"status":"error","error":{"code":"POOL_EXHAUSTED","message":"No IP pool configured for this APN"}}`.
3. Audit log doğrulama: `SELECT action, after FROM audit_logs WHERE entity_id='<sim-id>' AND action='sim.activate.failed' ORDER BY created_at DESC LIMIT 1;` → `after = {"reason":"no_pool_for_apn","attempted_state":"active"}`.

### Senaryo 4 — Resume static IP allocation skip (AC-5, DEV-392)

1. Static IP'li SIM'i suspend et (Senaryo 2'deki gibi).
2. POST `/sims/{id}/resume` → **Beklenen:** HTTP 200, sim state→`active`, `ip_address_id` UNCHANGED (yeni allocate YAPILMAZ).
3. Audit log: `sim.resume` (success) yazılı; `sim.resume.failed` YOK.

### Senaryo 5 — Resume dynamic IP re-allocation (AC-5, DEV-392)

1. Dynamic IP'li SIM'i suspend et (Senaryo 1'deki gibi). Suspend sonrası `sims.ip_address_id IS NULL`.
2. POST `/sims/{id}/resume` → **Beklenen:** HTTP 200, sim state→`active`, `ip_address_id` YENİ bir IP ile DOLDURULUR (re-allocate via handler-side flow per DEV-392).
3. Pool counter: `+1` (yeniden allocate).

### Senaryo 6 — Round-trip suspend → activate dynamic (regression)

1. Dynamic IP'li SIM → Suspend → Activate (Resume yerine `/activate`).
2. **Beklenen:** Suspend sonrası ip_address_id NULL, Activate sonrası YENİ IP allocated. Test: `TestActivate_PoolEmpty_Returns422` benzer mock yoluyla regression coverage.

### Senaryo 7 — Audit log her başarısız branch'de yazılı (AC-3, DEV-393)

1. Her Activate failure branch'i için audit log doğrulama (test referansı: `TestActivate_AuditOnFailure_AllBranches` 4 sub-test):
   - `validate_apn_missing` (SIM'in apn_id'si NULL ise)
   - `no_pool_for_apn` (APN'in pool'u yok)
   - `pool_exhausted` (mevcut pool dolu)
   - `state_transition_failed` (örn. terminated SIM activate edilmek istenirse)
2. Her durumda audit log: `action='sim.activate.failed'`, `after.reason=<branch>`, `after.attempted_state='active'`.

### Senaryo 8 — Regression test suite (AC-4)

1. ```
   DATABASE_URL='postgresql://argus:argus_secret@localhost:5450/argus?sslmode=disable' go test -v -count=1 -run 'SIMStore_Suspend|SIMStore_Activate|SIMStore_Resume|Activate_PoolEmpty|Activate_AuditOn|Resume_StaticIP' ./internal/store/... ./internal/api/sim/...
   ```
2. **Beklenen:** 11 test PASS (8 store + 3 handler). DATABASE_URL set DEĞİLSE testler SKIP eder (existing pattern).

## FIX-241: Global API Nil-Slice Fix — `WriteList` normalize nil → `[]`

> **Backend hardening (P0 cross-cutting).** `apierr.WriteList` artık nil slice'ları reflect ile boş array'e dönüştürüyor (`normalizeListData` helper). Sonuç: 60+ list endpoint'i için empty result `{"data":[]}` döner — ASLA `{"data":null}`. Birçok FE crash (F-243 User Detail, F-277 Ops Performance, F-328 Reports) tek bir backend fix ile kapanıyor. `WriteSuccess` etkilenmedi (single-object response'ları null dönmeye devam eder). DEV-394/395/396/397 logged. Plan: `docs/stories/fix-ui-review/FIX-241-plan.md`.

### Senaryo 1 — Empty-activity user → `[]` (AC-1, AC-4 keystone)

1. Login: admin@argus.io / admin
2. Audit log entry'si olmayan bir kullanıcı bul:
   ```
   docker exec argus-postgres psql -U argus -d argus -t -A -c "SELECT u.id FROM users u WHERE NOT EXISTS (SELECT 1 FROM audit_logs a WHERE a.user_id=u.id) LIMIT 1;"
   ```
3. Activity endpoint'ini çağır:
   ```
   curl -s "http://localhost:8084/api/v1/users/<empty-user-id>/activity?limit=1" -H "Authorization: Bearer $TOKEN" | jq
   ```
4. **Beklenen:**
   ```json
   {"status":"success","data":[],"meta":{"has_more":false,"limit":1}}
   ```
   - `data` array (`[]`), NULL DEĞİL ✓
   - Pre-FIX-241: `{"data":null,...}` dönerdi → FE `data.length` → TypeError → ErrorBoundary

### Senaryo 2 — User Detail FE sayfası boş activity tab (AC-5)

1. Browser: `/settings/users/{id}` (Senaryo 1'deki empty-activity user için)
2. Activity tab'ını aç
3. **Beklenen:**
   - Sayfa yüklenir, "No activity recorded" empty state görünür
   - Browser DevTools Console: `TypeError: Cannot read properties of null (reading 'length')` HATASI YOK
   - Pre-FIX-241: ErrorBoundary tetiklenirdi (F-243)

### Senaryo 3 — Ops Performance + Reports cold-load (AC-6 + F-277/F-328)

1. Browser: `/ops/performance` → sayfa yüklenir, hatasız render
2. Browser: `/reports` → "No scheduled reports yet" empty state görünür
3. **Beklenen:** Console temiz, TypeError YOK

### Senaryo 4 — Populated list davranışı korundu (AC-7 regression)

1. Activity'si olan bir user (örn. admin@argus.io kendisi) için aynı endpoint:
   ```
   curl -s "http://localhost:8084/api/v1/users/00000000-0000-0000-0000-000000000010/activity?limit=5" -H "Authorization: Bearer $TOKEN" | jq '.data | length'
   ```
2. **Beklenen:** > 0 entry, `data` array içinde audit_log row'ları, `meta.has_more` doğru.

### Senaryo 5 — Perf budget (AC-8)

1. ```
   go test -bench=BenchmarkWriteList_NilSlice -benchtime=2s ./internal/apierr/...
   ```
2. **Beklenen:** ~475 ns/op (≈ 0.48 µs) — 10 µs threshold'unun altında (~21x margin). DEV-397 referansı.

### Senaryo 6 — `WriteSuccess` davranışı değişmedi (AC-2 regression)

1. Single-object endpoint çağır: `GET /api/v1/users/{id}` (mevcut user)
2. **Beklenen:** `data` field bir object (map), array değil. Optional field'lar (`smdp_plus_url` gibi) `null` olabilir — bu beklenen davranış (DEV-394 scope discipline).

### Senaryo 7 — Regression test suite (AC-3)

1. ```
   go test -count=1 -run 'TestWriteList' ./internal/apierr/...
   ```
2. **Beklenen:** 5 yeni sub-test PASS (nil_typed_struct_slice, nil_map_slice, empty_initialized_slice, populated_slice_unchanged, non_slice_map_passthrough) + benchmark çalışır.

### Senaryo 8 — Integration test (AC-4)

1. ```
   go test -count=1 -run 'TestActivity_EmptyUserReturnsEmptyArray_ShapeContract' ./internal/api/user/...
   ```
2. **Beklenen:** PASS — `data!=nil`, `data` is `[]interface{}` of length 0.

## FIX-242: Session Detail Extended DTO Populate (SoR / Policy / Quota / CoA history / Audit)

> **Wave 8 P0.** `Handler.Get` artık `sessionDetailDTO`'yu 4 extended pointer field ile dolduruyor: `sor_decision` (JSONB unmarshal), `policy_applied` (D-145 CoA fields fold-in), `quota_usage` (policy compiled rules + live session bytes), `coa_history` (audit_logs lookup). Session lifecycle artık `audit_logs`'a yazıyor (Manager.Create/Terminate). FE Session Detail tüm tab'lar gerçek veri render ediyor + AC-12 layout grid-cols-2. SoR engine wiring D-148 (FIX-24x) deferred. Plan: `docs/stories/fix-ui-review/FIX-242-plan.md`.

### Senaryo 1 — Session Detail tüm tabs gerçek veri (AC-1+AC-3+AC-4+AC-7)

1. Login: admin@argus.io / admin
2. Aktif session bul:
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT id, sim_id FROM sessions WHERE state='active' AND tenant_id='00000000-0000-0000-0000-000000000001' LIMIT 1;"
   ```
3. Curl: `curl -s "http://localhost:8084/api/v1/sessions/<id>" -H "Authorization: Bearer $TOKEN" | jq '{policy_applied, quota_usage, coa_history, sor_decision}'`
4. **Beklenen:** `policy_applied` non-null (policy_name + version_number + coa_status doldur), `quota_usage` non-null (limit_bytes + used_bytes + pct_used), `coa_history` array (boş veya entries — `[]` ASLA `null` per FIX-241), `sor_decision` null veya doldur (engine wiring D-148 sonrası dolu).

### Senaryo 2 — `coa_failure_reason` tooltip (D-145 fold-in, AC-9, DEV-406)

1. Test fixture: bir SIM için `coa_status='failed'` ve `coa_failure_reason='diameter timeout'` set et:
   ```
   docker exec argus-postgres psql -U argus -d argus -c "UPDATE policy_assignments SET coa_status='failed', coa_failure_reason='diameter timeout', coa_sent_at=NOW() WHERE sim_id=(SELECT sim_id FROM sessions WHERE state='active' LIMIT 1);"
   ```
2. Browser: o SIM'in aktif session'ının detayına git → Policy tab → CoA status badge üzerine hover et.
3. **Beklenen:** Tooltip görünür, içeriği `"diameter timeout"`. Pre-FIX-242: tooltip yoktu (D-145 OPEN).

### Senaryo 3 — Session lifecycle audit log (AC-5, F-161, DEV-402)

1. Bir session start olduktan sonra (RADIUS Access-Request veya test fixture):
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT action, after_data FROM audit_logs WHERE entity_type='session' AND entity_id='<session-id>' ORDER BY created_at DESC LIMIT 5;"
   ```
2. **Beklenen:** En az 1 row, `action='session.started'`, `after_data` içinde `sim_id`, `operator_id`, `apn_id`, `ip_address`, `rat_type` keys. Session terminate olunca: `action='session.ended'`, `after_data` içinde `bytes_in/bytes_out/duration_sec/termination_reason`.

### Senaryo 4 — Quota progress bar warning thresholds (AC-8)

1. FE Browser: Session detail → Quota tab.
2. Aktif session'ın quota'sı %0-79 ise: progress bar normal renkte. %80-94 ise: yellow/warning. %95+ ise: red/danger.
3. **Beklenen:** Threshold renkleri PAT-018 disiplin ile semantic CSS-var class'ları kullanıyor (`text-warning`, `text-danger`).

### Senaryo 5 — AC-12 layout fix (F-162)

1. Browser: Session detail page açık.
2. **Beklenen:** Top section iki eşit kart (Connection Details + Data Transfer) `grid-cols-2`. Alt section: Session Timeline kart + Policy Context kart eşit genişlik. Pre-FIX-242: alt yarı boştu.

### Senaryo 6 — Empty-state UX (AC-11)

1. SoR scoring olmayan bir session için (eski session, henüz engine wired değil): SoR tab "SoR scoring not yet persisted for this session — Engine wiring planned in FIX-24x" mesajını göstermeli (NOT "unavailable" veya "broken").
2. Quota policy yoksa: "No quota rule defined in applied policy" empty-state.
3. matched_rules boş ise: "No matched rules logged yet (engine instrumentation pending)" (D-147 evaluator instrumentation deferred).

### Senaryo 7 — Defensive enricher (handler asla 500)

1. Test fixture: bir session'a corrupt `sor_decision` JSONB enjekte et (örn. `'{"invalid":'`):
   ```
   docker exec argus-postgres psql -U argus -d argus -c "UPDATE sessions SET sor_decision='\"corrupt-json'::jsonb WHERE id='<id>';"
   ```
   (Postgres invalid jsonb'yi reddeder — bu zor; alternatif: integration test ile mock store'da zorla.)
2. Curl session detail.
3. **Beklenen:** HTTP 200 (ASLA 500), `sor_decision` field null/omitted, base `dto` döner.

### Senaryo 8 — Regression test suite (AC verification)

1. ```
   DATABASE_URL='postgresql://argus:argus_secret@localhost:5450/argus?sslmode=disable' go test -count=1 -run 'PolicyStore_GetAssignment|UpdateAssignmentCoAStatusWithReason|SessionGet_Enrich|Manager_Create_Publishes' ./internal/store/... ./internal/api/session/... ./internal/aaa/session/...
   ```
2. **Beklenen:** 9 test PASS + 1 SKIP (defensive corrupt-JSONB test infeasible without DB-backed integration harness — documented).

## FIX-237: M2M-Centric Event Taxonomy + Notification Redesign

> **Wave 8 P0.** Event kataloğu 3 tier'a ayrıldı (Tier 1 = internal/metric, Tier 2 = digest/aggregate, Tier 3 = operational). `notification.Service.Notify` Tier 1 event'leri sessizce suppress eder; Tier 2 event'ler yalnızca `Source="digest"` ile publish edilir. Fleet digest worker 15 dakikada bir aggregate event yayınlar. Notification şablonları admin/NOC voice'a dönüştürüldü (34 şablon, 17 event × 2 locale). NATS EVENTS stream retention 72h → 168h. DSAR consumer-facing event'ler (`data_portability.ready` vb.) kaldırıldı. Plan: `docs/stories/fix-ui-review/FIX-237-plan.md`.

### Senaryo 1 — Tier 1 event'ler notification oluşturmuyor (AC-1, AC-2)

1. Event kataloğunu kontrol et — her entry'de `tier` field bulunmalı:
   ```
   curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/events/catalog | jq '.data.events[] | {name, tier}' | head -40
   ```
2. `session.started` event'ini tetikle (RADIUS Access-Request veya test fixture ile).
3. Notifications tablosunda son 5 dakikada oluşan row sayısını kontrol et:
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT count(*) FROM notifications WHERE event_type IN ('session.started','session_started') AND created_at > NOW() - INTERVAL '5 minutes';"
   ```
4. **Beklenen:** Sonuç `0` — Tier 1 event'ler için `notifications` tablosunda yeni row OLUŞMAMALI.

### Senaryo 2 — Tier 1 event'ler WS Live Stream'de görünüyor (AC-2, Conflict 4)

1. Browser: http://localhost:8084/dashboard (Live Event Stream bölümü açık).
2. RADIUS test client ile `session.started` tetikle.
3. **Beklenen:** Live Event Stream'de `session.started` event'i anlık görünür. AMA Senaryo 1'den `notifications` tablosunda row YOKTUR — Tier 1 suppress yalnızca kalıcı notification içindir; WS stream etkilenmez.

### Senaryo 3 — Tier 3 operational event notification oluşturuyor (AC-2, AC-6)

1. `operator_down` event'ini tetikle (operator simulator'ı durdur veya test endpoint üzerinden direkt `notification.Service.Notify` çağır).
2. Notifications tablosunu sorgula:
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT event_type, severity, created_at FROM notifications WHERE event_type='operator_down' ORDER BY created_at DESC LIMIT 3;"
   ```
3. **Beklenen:** 1 yeni row, `event_type='operator_down'`, `severity` config'e göre set edilmiş (örn. `high`).

### Senaryo 4 — Tier 2 digest event source guard (AC-3)

1. `fleet.mass_offline` event'ini `Source=""` (boş) ile yayınla (yanlış kullanım simülasyonu — test endpoint veya unit test).
2. Uygulama loglarını kontrol et:
   ```
   docker logs argus-app 2>&1 | grep "digest_no_source" | tail -5
   ```
3. Prometheus counter'ı doğrula:
   ```
   curl -s http://localhost:8080/metrics | grep 'events_tier_filtered_total'
   ```
4. **Beklenen:** Log'da `"tier=digest but source!=digest"` uyarısı + `argus_events_tier_filtered_total{reason="digest_no_source"}` counter artmış. `notifications` tablosunda yeni row YOK.
5. Aynı event'i `Source="digest"` ile yeniden yayınla.
6. **Beklenen:** Bu sefer `notifications` tablosunda 1 yeni row oluşur.

### Senaryo 5 — Fleet digest worker tick (AC-3)

1. Digest worker'ı manuel tetikle (test endpoint veya job runner):
   ```
   docker exec argus-app /app/argus job run fleet_digest
   ```
2. Uygulama loglarını kontrol et:
   ```
   docker logs argus-app 2>&1 | grep "fleet digest" | tail -10
   ```
3. **Beklenen:** Loglarda `"fleet digest tick: starting"` ve `"fleet digest tick: complete"` mesajları görünür. Eğer threshold aşıldıysa NATS'ta `fleet.*` event yayınlanır ve `notifications` tablosuna 1 row eklenir.

### Senaryo 6 — Notification Preferences UI'da Tier 1 event'ler gizli (AC-4)

1. Browser: http://localhost:8084/notifications?tab=preferences
2. Listede gösterilen event'leri incele.
3. **Beklenen:** Listede SADECE Tier 2 + Tier 3 event'ler görünür. `session.started`, `sim.state_changed`, `heartbeat.ok` gibi Tier 1 event'ler listede BULUNMAMALI.
4. "Add preference" picker'ını aç — burada da Tier 1 event'ler YOK.
5. Sayfa footer veya helper text'te şu mesaj bulunmalı: `"Internal/metric events are not shown — they cannot be configured for notifications."`

### Senaryo 7 — Şablon sayısı ve içerik doğrulaması (AC-5, AC-7)

1. Şablon toplam sayısını kontrol et:
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT count(*) FROM notification_templates;"
   ```
   **Beklenen:** `34` (17 event tipi × 2 locale: `tr` + `en`).
2. Event tiplerini listele:
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT DISTINCT event_type FROM notification_templates ORDER BY event_type;"
   ```
   **Beklenen:** Listede `fleet_mass_offline`, `fleet_traffic_spike`, `fleet_quota_breach_count`, `fleet_violation_surge`, `bulk_job_completed`, `bulk_job_failed`, `backup_verify_failed` VAR; `welcome`, `sim_state_change`, `session_login`, `onboarding_completed`, `data_portability_ready` YOK.
3. Bir şablonun body içeriğini kontrol et:
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT body FROM notification_templates WHERE event_type='operator_down' AND locale='en' LIMIT 1;"
   ```
   **Beklenen:** Admin/NOC voice içeriği (örn. `"Operator Turkcell health degraded — circuit breaker engaged at ..."`). Consumer-facing dil (`"Hello {{UserName}}, your SIM was suspended..."` tarzı) bulunmamalı.

### Senaryo 8 — DSAR event'lerin taxonomy'den kaldırılması (AC-9)

1. Kaynak kodda `data_portability` referansı ara:
   ```
   grep -ri data_portability /Users/btopcu/workspace/argus/internal/api/events/catalog.go
   ```
   **Beklenen:** Sonuç boş — catalog'da bu event YOK.
2. Seed/migration dosyasında cleanup bloğunu doğrula:
   ```
   grep -i data_portability_ready /Users/btopcu/workspace/argus/migrations/seed/004_notification_templates.sql
   ```
   **Beklenen:** Yalnızca `DELETE` / cleanup bloğunda görünür (eski veriyi temizleme amacıyla), yeni ekleme olarak YOK.

### Senaryo 9 — Migration env-gate davranışı (AC-10, AC-11)

1. Env-gate KAPALI (default) ile migration uygula:
   ```
   make db-migrate
   ```
   Uygulama loglarını veya migration çıktısını incele.
   **Beklenen:** NOTICE: `"FIX-237: pre-purge Tier 1 notification row count: N"` + `"argus.drop_tier1_notifications NOT set — skipping purge"` → mevcut Tier 1 notification row'ları KALIR.
2. Env-gate AÇIK ile manuel uygula:
   ```
   PGPASSWORD=argus_secret psql -h localhost -p 5450 -U argus -d argus -v argus.drop_tier1_notifications=true -f /Users/btopcu/workspace/argus/migrations/20260501000002_notifications_taxonomy_migration.up.sql
   ```
   **Beklenen:** NOTICE: `"FIX-237: deleted N Tier 1 notification rows (env gate ON)"` — Tier 1 event'lere ait eski notification row'ları silindi.
3. AC-10 kontrolü: Tier 1 event'lere ait `notification_preferences` row'u varsa:
   **Beklenen:** NOTICE: `"X notification_preferences row(s) reference Tier 1 / removed event types... ineffective..."` uyarısı.

### Senaryo 10 — NATS EVENTS stream retention 168h (AC-8)

1. NATS JetStream yapılandırmasını sorgula:
   ```
   curl -s http://localhost:8222/jsz?streams=true | jq '.streams[] | select(.config.name=="EVENTS") | .config.max_age'
   ```
2. **Beklenen:** `604800000000000` (168 saat = 7 gün, nanosaniye cinsinden). Pre-FIX-237 değer `259200000000000` (72h) idi.

### Senaryo 11 — Bulk job event publish (AC-6)

1. Bulk state-change job'ı çalıştır (örn. 50 SIM ile `/api/v1/sims/bulk/state-change` POST).
2. Job tamamlandıktan sonra notifications tablosunu sorgula:
   ```
   docker exec argus-postgres psql -U argus -d argus -c "SELECT event_type, severity, meta FROM notifications WHERE event_type IN ('bulk_job.completed','bulk_job.failed') ORDER BY created_at DESC LIMIT 5;"
   ```
3. **Beklenen:** 1 yeni row:
   - Tüm SIM'ler başarılı: `bulk_job.completed`, severity=`info`
   - Kısmi başarısız: `bulk_job.completed`, severity=`medium`
   - Tümü başarısız: `bulk_job.failed`, severity=`high`
   - `meta` JSON keys: `bulk_job_id`, `total_count`, `success_count`, `fail_count`, `job_type`

### Senaryo 12 — Regression test suite (AC doğrulaması)

1. Tüm FIX-237 testlerini çalıştır:
   ```
   go test -count=1 -run 'TestNotify_Tier|TestTierFor|TestCatalog_Tier|TestRelayNATS_Tier1|TestWorker' ./...
   ```
2. **Beklenen:** Tüm FIX-237 adlı testler PASS:
   - `TestTierFor_*` (4 adet) — events package tier sınıflandırması
   - `TestCatalog_TierMatchesTierFor` (1 adet) — catalog tier annotation tutarlılığı
   - `TestNotify_Tier1*`, `TestNotify_Tier2*`, `TestNotify_Tier3*` (6 adet) — service tier guard logic
   - `TestWorker_*` (19 adet) — digest aggregation + emit
   - `TestRelayNATS_Tier1Event_DoesNotCreateNotificationRow` (1 adet) — WS regression guard


## FIX-243: Policy DSL Realtime Validate Endpoint + FE Linter

**Hedef:** Policy Editor — DSL yazarken anlık validation, autocomplete, auto-format, did-you-mean önerileri ve seed validation CLI'nin uçtan uca doğrulanması.

**Ön koşullar:**
- `make up` ile tüm servisler ayakta.
- Tarayıcı: `admin@argus.io` / `admin` ile login.
- Bir policy aç (örn. `/policies` → herhangi bir policy → DRAFT bir version seç).

### Senaryo 1 — Validate endpoint smoke test (curl, AC-1/AC-3)

1. Geçerli DSL ile validate endpoint'ini çağır:
   ```
   TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login -H 'content-type: application/json' -d '{"email":"admin@argus.io","password":"admin"}' | jq -r .data.access_token)
   curl -s -X POST http://localhost:8084/api/v1/policies/validate \
     -H "authorization: Bearer $TOKEN" -H 'content-type: application/json' \
     -d '{"dsl_source":"POLICY \"p1\" { MATCH { apn = \"internet\" } RULES { bandwidth_down = 10mbps } }"}' | jq .
   ```
   **Beklenen:** HTTP 200, `data.valid=true`, `data.compiled_rules` doldu, `data.warnings=[]`, hiç state mutation yok.
2. Geçersiz DSL gönder (kapatılmamış brace):
   ```
   curl -s -X POST http://localhost:8084/api/v1/policies/validate \
     -H "authorization: Bearer $TOKEN" -H 'content-type: application/json' \
     -d '{"dsl_source":"POLICY \"broken\" {"}' | jq .
   ```
   **Beklenen:** HTTP 422, `error.code="DSL_VALIDATION_FAILED"`, `error.details.errors[]` en az bir kayıt içerir (line/column/severity/message).

### Senaryo 2 — Rate limit (AC-2)

1. Aynı IP'den 11 adet istek hızlıca gönder:
   ```
   for i in {1..11}; do
     curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8084/api/v1/policies/validate \
       -H "authorization: Bearer $TOKEN" -H 'content-type: application/json' \
       -d '{"dsl_source":"POLICY \"p\" { MATCH { apn = \"x\" } RULES { bandwidth_down = 1mbps } }"}'
   done
   ```
2. **Beklenen:** İlk 10 istek `200`, 11. istek `429 Too Many Requests` (httprate.LimitByIP guard'ı tetiklenir).

### Senaryo 3 — FE linter, anlık squiggly (AC-4)

1. Policy editor sayfasına git, DRAFT bir version seç.
2. Editor içine geçersiz bir alan adı yaz: `MATCH { apnz = "internet" }` (doğrusu `apn`).
3. **Beklenen:** ~600ms içinde altı çizili kırmızı squiggly görünür; hover edince `did you mean "apn"` mesajı gelir; sol kenarda lint gutter'da kırmızı işaret çıkar; alttaki "DSL Errors" özetinde `1 error` görünür.
4. Hatayı düzelt (`apnz` → `apn`). **Beklenen:** ~600ms içinde squiggly kaybolur, özet `0 errors`.

### Senaryo 4 — Autocomplete: Ctrl+Space (AC-5)

1. Editor'da `MATCH {` bloğunun içine yeni satır aç, `Ctrl+Space` (Mac: `Ctrl+Space`) bas.
2. **Beklenen:** Açılır listede `apn`, `imsi`, `tenant`, `msisdn`, `rat_type`, `sim_type`, `roaming`, `operator`, `group` görünür (vocab endpoint'inden gelir).
3. `RULES {` bloğuna geç, `Ctrl+Space` bas.
4. **Beklenen:** `bandwidth_down`, `bandwidth_up`, `rate_limit`, `session_timeout`, `idle_timeout`, `max_sessions`, `qos_class`, `priority` listelenir.

### Senaryo 5 — Ctrl+Enter validate-now & Ctrl+Shift+F format (AC-7/AC-8)

1. Editor'a deliberately düzensiz DSL yapıştır:
   ```
   POLICY    "p1"   {
   MATCH{apn="internet"}
   RULES{bandwidth_down=10mbps}
   }
   ```
2. `Ctrl+Enter` (Mac: `Cmd+Enter`) bas. **Beklenen:** Linter debounce'u beklemeden hemen tetiklenir, hata yoksa squiggly kalmaz; "DSL Errors" özeti güncellenir.
3. `Ctrl+Shift+F` (Mac: `Cmd+Shift+F`) bas. **Beklenen:** Editor içeriği canonical biçime dönüşür:
   ```
   POLICY "p1" {
     MATCH {
       apn = "internet"
     }
     RULES {
       bandwidth_down = 10mbps
     }
   }
   ```
   Satırlar 2-space ile indent'lenmiş, `=` etrafında boşluk bırakılmış, `10mbps` birleşik kalmış.
4. `Ctrl+Shift+Enter` (Mac: `Cmd+Shift+Enter`) bas. **Beklenen:** Dry-run preview tab'ına geçer, etkilenen SIM sayısı görünür (eski `Ctrl+Enter` davranışı bu kombinasyona taşındı).

### Senaryo 6 — Vocab endpoint smoke test (AC-5 backing API)

1. Vocab endpoint'ini doğrudan sorgula:
   ```
   curl -s http://localhost:8084/api/v1/policies/vocab -H "authorization: Bearer $TOKEN" | jq .
   ```
2. **Beklenen:** HTTP 200, `data` içinde 7 key — `match_fields`, `charging_models`, `overage_actions`, `billing_cycles`, `units`, `rule_keywords`, `actions` — her biri non-empty alfabetik sıralı array. FE artık fallback yerine bu listeyi cache'liyor.

### Senaryo 7 — Seed validate CLI (AC-9/AC-10)

1. CLI ile mevcut seed dosyalarını doğrula:
   ```
   ./bin/argusctl validate-seed-dsl
   echo "exit=$?"
   ```
   **Beklenen:** `exit=0`, "All N policy DSL fragments valid" benzeri mesaj.
2. Bilerek bozulmuş bir seed üret (kopyala + parse hatası ekle):
   ```
   cp migrations/003_comprehensive_seed.sql /tmp/seed_broken.sql
   sed -i.bak 's/POLICY "iot/POLICY iot/' /tmp/seed_broken.sql
   ./bin/argusctl validate-seed-dsl --file /tmp/seed_broken.sql
   echo "exit=$?"
   ```
   **Beklenen:** `exit=1`, dosya yolu + line number + DSLError mesajı; `make db-seed-validate` aynı çıktıyı verir.


## FIX-244: Violations Lifecycle UI — Acknowledge + Remediate Wired

### AC-1: Row Acknowledge action
1. `/violations` sayfasında bir Open violation satırının üç-noktalı menüsünden **Acknowledge** seç.
2. Açılan dialog'da opsiyonel not yaz, **Acknowledge** bas.
3. **Beklenen:** Toast "Violation acknowledged"; satırın StatusBadge'i sarı **Acknowledged**'a döner; `/audit-logs` sayfasında `violation.acknowledge` entry görünür.

### AC-1 + AC-5: Row Remediate (Suspend SIM)
1. Bir Open violation satırından **Suspend SIM** seç.
2. Dialog kırmızı destructive uyarı + ICCID + zorunlu reason (≥3 char). 2 char yaz → Confirm pasif. 5 char yaz → aktif. Confirm.
3. **Beklenen:** Toast "SIM suspended"; satır status **Remediated** (yeşil); `/sims/{id}` sayfasında SIM state = `suspended`. `audit_logs`: `violation.remediated` + `sim.suspended`.

### AC-1: Dismiss (false positive)
1. Bir satırdan **Dismiss (false positive)**.
2. Reason "test" (3 char) → Confirm aktif → Confirm.
3. **Beklenen:** Toast "Violation dismissed"; status **Dismissed** (gri).

### AC-3: Filter mismatch fix (F-165)
1. Toolbar'daki **Type** dropdown'unu aç. Değerler: bandwidth_exceeded / session_limit / quota_exceeded / time_restriction / geo_blocked.
2. Bir tip seç → URL `?violation_type=bandwidth_exceeded`; backend doğru filtreyi uygular (sıfır sonuç dönmüyor).
3. Yeni **Action** dropdown ile (block / disconnect / suspend / throttle / log / notify / tag) ayrı filtre dene.

### AC-4: Export path fix (F-166)
1. Sağ üstteki **Export** butonuna bas.
2. **Beklenen:** DevTools Network'te request `/api/v1/policy-violations/export.csv`. CSV dosyası iner.
3. Eski URL'yi tarayıcıdan elle vur: `/api/v1/violations/export.csv` → 301 redirect ile yeni path'e döner.

### AC-6: Status / Severity / Date range filtreleri
1. **Status** = Open + **Severity** = critical + **Date range** = last 24h seç.
2. **Beklenen:** URL'de `?status=open&severity=critical&date_from=...&date_to=...`; sadece eşleşen satırlar.
3. **Status** = Acknowledged → daha önce ack edilen satırlar görünür (önceden tamamen gizleniyordu).

### AC-7: Empty state
1. Tüm filtreleri temizle, Date range = last 1h (boş tenant).
2. **Beklenen:** "No policy violations in the last 1 hours." mesajı (eski "Well done!" yok).

### AC-8: Row polish
1. Satırda ICCID artık literal "SIM" değil, gerçek ICCID + EntityLink.
2. Policy adı + version chip görünür.
3. `details.current_bytes` + `details.threshold_bytes` varsa satırda "180 MB / 1 GB" gibi inline metrik.

### AC-9: Bulk Acknowledge
1. 3 satırın checkbox'ını işaretle. Sticky bottom bar "3 selected" görünür.
2. **Acknowledge** bas → AcknowledgeDialog (mode=bulk, count=3) → opsiyonel note → Confirm.
3. **Beklenen:** Toast "3 violations acknowledged"; üçü de Acknowledged. Tek bir satır zaten ack ise toast "2 acknowledged · 1 failed" gösterir.

### AC-9: Bulk Dismiss + 100-cap
1. 5 satır seç → **Dismiss** → reason ≥3 char → Confirm.
2. 101+ id ile direct API çağrısı: `POST /policy-violations/bulk/dismiss` → 400 "ids must be ≤ 100".
3. Bulk select-all "select all on page" tooltip: "Selection scoped to visible page — bulk-by-filter coming with FIX-236".

### AC-10: Audit trail
1. Yukarıdaki her aksiyondan sonra `/audit-logs` filter `entity_type=policy_violation` → her aksiyon için bir satır:
   - `violation.acknowledge` (single + bulk)
   - `violation.remediated` (suspend_sim path)
   - `violation.escalated`
   - `violation.dismissed` (single + bulk)
2. `bulk: true` flag bulk path için after JSONB'da görünür.
