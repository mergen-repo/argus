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
