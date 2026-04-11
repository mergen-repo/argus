---
title: "ARGUS — APN & Subscriber Intelligence Platform"
subtitle: "10M+ IoT SIM Yönetimini Tek Platformdan"
version: "1.0"
date: "2026-04-06"
company: "NAR Sistem Teknoloji A.Ş."
---

---

# ARGUS

## APN & Subscriber Intelligence Platform

**10M+ IoT/M2M SIM Yönetimini Tek Platformdan**

*Çoklu operatör, tek kontrol merkezi. Protokol derinliği, operasyonel zekâ.*

NAR Sistem Teknoloji A.Ş.

---

# Executive Summary

Argus, kurumsal IoT/M2M müşterilerinin 10 milyondan fazla SIM kartını birden fazla mobil operatör üzerinden tek bir platformdan yönetmesini sağlayan **APN & Subscriber Intelligence Platform**'dur.

## Temel Değer Önerileri

| # | Değer |
|---|-------|
| 1 | **Yerleşik AAA Motoru** — RADIUS, Diameter, 5G SBA tek uygulamada; 10K+ auth/s performans |
| 2 | **Çoklu Operatör Orkestrasyonu** — Turkcell, Vodafone, TT Mobile tek panelden; otomatik failover |
| 3 | **Politika DSL Motoru** — QoS, FUP, charging kuralları; staged rollout (canary %1 → %10 → %100) |
| 4 | **10M+ SIM Yönetimi** — Group-first UX, bulk operations, eSIM (SM-DP+), IPAM dual-stack |
| 5 | **Gerçek Zamanlı BI** — Anomali tespiti, maliyet optimizasyonu, CDR rating, uyumluluk raporları |

## Önemli Metrikler

| Metrik | Değer |
|--------|-------|
| Auth throughput | 10K+ req/s per node |
| Auth latency | p50 <5ms, p99 <50ms |
| Yönetilen SIM | 10M+ |
| Eşanlı oturum | 5M+ |
| Portal sayfa yükleme | <500ms |

---

# Problem & Çözüm

## Kurumsal Problemler

- Her operatörün ayrı portalı, API'si ve iş akışı — **3-4 operatör = 3-4 ayrı giriş, birleşik görünüm yok**
- QoS, FUP ve charging politikaları her operatör için ayrı ayrı, **manuel konfigüre ediliyor**
- Kullanım verileri, maliyet analizi ve anomali tespiti **operatör silolarına dağılmış**
- eSIM profil geçişleri **her operatörün SM-DP+ platformu ile ayrı koordine ediliyor**
- Mevcut araçlar (FreeRADIUS, Mobile ITM SCM) **protokol derinliği + yaşam döngüsü + analitik kombinasyonundan yoksun**

## Argus'un 5-Katmanlı Çözümü

- **Katman 1 — AAA Core**: RADIUS/Diameter/5G SBA, EAP-SIM/AKA, CoA/DM, active-active HA
- **Katman 2 — SIM & APN**: Yaşam döngüsü, eSIM, IPAM, bulk ops, KVKK/GDPR purge
- **Katman 3 — Multi-Operator**: Adapter framework, SoR, failover, circuit breaker, IMSI routing
- **Katman 4 — Policy Engine**: DSL motoru, staged rollout, dry-run, versiyon yönetimi
- **Katman 5 — BI & Analytics**: Dashboard, anomali tespiti, maliyet optimizasyonu, CDR rating

---

# 5-Katmanlı Platform Mimarisi

```
Portal & API ─── Tek yönetim arayüzü (React SPA, 26 ekran)
     │
Layer 5: BI & Analytics ─── Gerçek zamanlı dashboard, anomali, CDR/faturalandırma
     │
Layer 4: Policy Engine ─── QoS, FUP, charging, staged rollout, DSL
     │
Layer 3: Multi-Operator ─── SoR, failover, IMSI routing, operatör adapter
     │
Layer 2: SIM & APN ─── Yaşam döngüsü, eSIM, IPAM, bulk ops, OTA
     │
Layer 1: AAA Core ─── RADIUS (1812/1813), Diameter (3868), 5G SBA (8443)
```

## Sistem Bileşenleri

| Bileşen | Sorumluluk |
|---------|------------|
| **API Gateway (SVC-01)** | HTTP routing, JWT auth, rate limiting, middleware chain |
| **WebSocket Server (SVC-02)** | Gerçek zamanlı event streaming |
| **Core API (SVC-03)** | SIM, APN, operatör, policy CRUD işlemleri |
| **AAA Engine (SVC-04)** | RADIUS, Diameter, 5G SBA, EAP, session management |
| **Policy Engine (SVC-05)** | DSL parser, kural değerlendirme, staged rollout |
| **Operator Router (SVC-06)** | Adapter framework, SoR, circuit breaker |
| **Analytics (SVC-07)** | CDR processing, anomali tespiti, maliyet analizi |
| **Notification (SVC-08)** | Email, Telegram, webhook, in-app bildirimler |
| **Job Runner (SVC-09)** | Async bulk ops, zamanlanmış görevler |
| **Audit (SVC-10)** | Değiştirilemez hash-chain loglama |

---

# AAA Core Engine

## Protokol Desteği

| Protokol | Port | Standart | Kullanım |
|----------|------|----------|----------|
| **RADIUS** | 1812/1813 | RFC 2865/2866 | Authentication, Authorization, Accounting |
| **RadSec** | TLS | RFC 6614 | Şifrelenmiş RADIUS |
| **Diameter** | 3868 | RFC 6733 | 3GPP Gx (policy), Gy (charging) |
| **5G SBA** | 8443 | 3GPP TS 29.509 | HTTP/2 AUSF/UDM proxy |
| **EAP** | — | RFC 4186/4187 | EAP-SIM, EAP-AKA, EAP-AKA' |

## Özellikler

- **CoA/DM**: Gerçek zamanlı oturum değişikliği ve bağlantı kesme
- **Session Management**: SIM başına yapılandırılabilir eşanlı oturum limiti
- **Active-Active HA**: Kesintisiz küme yapısı
- **Circuit Breaker**: Operatör başına retry, devre kesici, dead letter queue
- **Network Slice Auth**: 5G SA ağlar için dilim kimlik doğrulaması
- **Protocol Bridge**: Diameter ↔ RADIUS protokol köprüleme

## Performans (p99 < 50ms)

```
RADIUS Request → UDP listener (goroutine pool)
  → Parse (0.1ms) → Redis cache lookup (0.5ms)
  → Policy evaluate (0.1ms) → IMSI route (0.01ms)
  → Operator forward (5-30ms) → Session update (0.5ms)
  → NATS accounting event (async) → Response
```

---

# SIM & APN Yaşam Döngüsü

## SIM State Machine

```
ORDERED → ACTIVE ↔ SUSPENDED → TERMINATED → PURGED
                ↘ STOLEN/LOST ↗
```

| Geçiş | Tetikleyici | Yan Etki |
|-------|-------------|----------|
| ORDERED → ACTIVE | Bulk import / manuel aktivasyon | IP tahsis, varsayılan politika ata |
| ACTIVE → SUSPENDED | Manuel / politika (kota aşımı) | CoA/DM, oturum sonlandır, IP'yi tut |
| ACTIVE → STOLEN/LOST | Kayıp/çalıntı bildirimi | Anında CoA/DM, analitik flag |
| TERMINATED → PURGED | Otomatik (yapılandırılabilir gün) | Pseudonymize, kişisel veri sil |

## eSIM Yönetimi

- SM-DP+ API entegrasyonu (profil provision, switch, delete)
- Operatörler arası profil geçişi (bulk)
- Bulk eSIM provisioning

## IP Adres Yönetimi (IPAM)

- APN/operatör bazında IP havuzları
- Statik rezervasyon + dinamik tahsis
- IPv4 + IPv6 dual-stack
- Çakışma tespiti, kullanım alarmları (%80/%90/%100)
- Yapılandırılabilir geri alma bekleme süresi

## Bulk Operations

- Async job queue (NATS-backed)
- Progress bar, partial success, retry failed
- Error report CSV, undo/rollback
- 10K+ SIM per batch

---

# Çoklu Operatör Orkestrasyonu

## Operatör Adapter Framework

- Pluggable adapter mimarisi — her operatör için bağımsız adapter
- Mock simulator (geliştirme & test için)
- IMSI-prefix bazlı akıllı yönlendirme

## Steering of Roaming (SoR)

- RAT-type tercihli yönlendirme (NB-IoT, LTE-M, 4G, 5G)
- Operatör sağlık durumuna göre dinamik karar
- Yapılandırılabilir SoR politikaları

## Failover & Circuit Breaker

| Strateji | Davranış |
|----------|----------|
| **reject** | Auth istekleri reddedilir, oturum engellenir |
| **fallback-to-next** | Sonraki operatöre yönlendir |
| **queue-with-timeout** | N saniye bekle, sonra fallback veya reject |

- Operatör sağlık kontrolü heartbeat (yapılandırılabilir aralık)
- SLA ihlal olayları + bildirimler
- Circuit breaker eşik değeri + toparlanma penceresi yapılandırılabilir
- Otomatik trafik yeniden yönlendirme

---

# Politika & Charging Motoru (mini-PCRF)

## Policy DSL

Operatör bazında yapılandırılabilir kural motoru:

```
RULE "iot_bandwidth_limit"
  WHEN apn = "iot.fleet" AND rat_type IN ("NB-IoT", "LTE-M")
  THEN SET qos.max_bandwidth_dl = 256kbps
       SET qos.max_bandwidth_ul = 128kbps
  PRIORITY 100
```

## Özellikler

- **QoS**: APN/abone/RAT-type bazında bant genişliği limiti
- **Dynamic Rules**: Saat, konum, kota, RAT-type koşulları
- **Charging**: Ön ödeme/sonra ödeme, kota yönetimi
- **FUP**: Adil kullanım politikası uygulama
- **Slice-Aware**: 5G dilim bazlı politikalar

## Staged Rollout (Canary Deployment)

```
Policy Editor → Yeni versiyon → Dry-run: "2.3M SIM etkileniyor"
  → Stage 1: %1 (23K SIM) + CoA → Metrik inceleme
  → Stage 2: %10 (230K SIM) + CoA → Metrik inceleme
  → Stage 3: %100 (2.3M SIM) + CoA → Tamamlandı
  → Herhangi bir noktada: Rollback → önceki versiyona dön + CoA
```

---

# Yönetim Portalı

## 26 Ekranlı Premium Portal

| Modül | Ekranlar | Açıklama |
|-------|----------|----------|
| **Dashboard** | Tenant overview | Sistem sağlığı, SIM özeti, alarm akışı, aktif oturumlar |
| **SIM Yönetimi** | List, Detail, Compare | Group-first UX, segmentler, bulk actions, drill-down |
| **APN** | List, Detail | CRUD, ARCHIVED soft-delete, IP pool |
| **Operatörler** | List, Detail | Adapter, sağlık, SLA izleme |
| **Politikalar** | List, Editor | DSL editor, versiyonlama, rollout |
| **eSIM** | List | Profil yönetimi, operatör geçişi |
| **Oturumlar** | List | Aktif oturum takibi |
| **Analitik** | Dashboard, Cost | Kullanım, maliyet, anomali |
| **Görevler** | List | Arka plan görev izleme |
| **Denetim** | Log viewer | Değiştirilemez log arama/export |
| **Ayarlar** | Users, API Keys, IP Pools, Notifications, System | Tam yönetim |

## UX Özellikleri

- Dark mode varsayılan + light mode toggle
- Command palette (Ctrl+K) — hızlı navigasyon
- Connectivity diagnostics — otomatik teşhis + "Fix Now" butonu
- Notification center (bell icon, read/unread)
- Undo capability — durum değişiklikleri, politika atamaları
- Virtual scrolling — 500+ kayıt performansı

---

# BI & Gözlemlenebilirlik

## Gerçek Zamanlı Dashboard

- Per SIM/APN/operatör/RAT-type kullanım metrikleri
- Canlı oturum sayısı, auth/s, latency percentile (p50/p90/p95/p99)
- Hata oranları ve servis sağlığı görünümü
- WebSocket ile <100ms event-to-UI latency

## Anomali Tespiti

- SIM klonlama tespiti
- Veri kullanım spike'ları
- Kötü kullanım paternleri
- Kural tabanlı alarmlar + bildirimler

## Maliyet Optimizasyonu

- En ucuz operatör yönlendirme önerileri
- RAT-type bazlı maliyet farklılaştırması
- CDR processing & rating engine
- Carrier maliyet takibi

## Uyumluluk Raporları

- **BTK**: Yerel operatör entegrasyonu, veri lokalizasyonu
- **KVKK**: Kişisel veri saklama limitleri, otomatik temizleme, pseudonymization
- **GDPR**: Silme hakkı, veri taşıma, onay takibi
- **ISO 27001**: Denetim izi, erişim kontrolü, olay loglama

---

# Kullanım Senaryoları

## Senaryo 1: IoT Fleet Provisioning

```
Tenant Admin → CSV yükleme (10K SIM: ICCID, IMSI, MSISDN, operatör, APN)
  → Validation + uniqueness check
  → Background job → progress bar
  → Per-SIM: kayıt (ORDERED) → aktivasyon (ACTIVE) → APN ata → politika ata → IP tahsis
  → Partial success: başarılı satırlar uygulanır, hata raporu CSV
  → Bildirim: in-app + email + Telegram
```

## Senaryo 2: Operatör Failover

```
Turkcell RADIUS bağlantısı düşer
  → Circuit breaker tetiklenir (N ardışık hata)
  → Operatör: DEGRADED → alarm gönderilir
  → Per-SIM failover politikası:
    "fallback-to-next" → trafik Vodafone'a yönlendirilir
  → Turkcell toparlanınca → circuit breaker reset → trafik geri
  → SLA raporu: kesinti süresi + etkilenen SIM sayısı
```

## Senaryo 3: eSIM Cross-Operator Switch

```
SIM Manager → "Fleet APN - Turkcell" segmenti seçilir
  → Bulk action: "Vodafone'a geç"
  → Kontrol: Vodafone adapter, APN, IP pool kapasitesi
  → Per-SIM: Turkcell SM-DP+ disable → Vodafone SM-DP+ enable
    → SIM kaydı güncelle → politika ata → IP tahsis
  → CoA: aktif oturumlara politika güncellemesi
  → Analitik: operatör geçişi öncesi/sonrası maliyet karşılaştırması
```

---

# Rekabet Avantajları

## Argus vs Mevcut Çözümler

| Kriter | FreeRADIUS / Mobile ITM | emnify / Cisco Jasper | Enea / Alepo / Nokia AAA | ARGUS |
|--------|------------------------|-----------------------|--------------------------|-------|
| AAA Motoru | FreeRADIUS (C, config) | Bulut tabanlı, sınırlı | Özel, lisanslı | Go, custom, yerleşik |
| Diameter/5G | Ek modül gerekir | Yok / sınırlı | Var ama ayrı ürün | Yerleşik, tek binary |
| SIM Yönetimi | Harici gerekir | Var ama bulut-only | Sınırlı | 10M+ ölçek, group-first UX |
| Çoklu Operatör | Manuel | Platform sınırı | Tek operatör odaklı | Pluggable adapter, otomatik failover |
| Policy Engine | Flat config | Temel kurallar | Gelişmiş ama karışık | DSL, staged rollout, dry-run |
| eSIM | Yok | Kısmen | Yok | SM-DP+ entegrasyon, bulk switch |
| Analitik | Yok | Temel | Ayrı ürün | Yerleşik anomali, maliyet opt. |
| Deployment | On-prem only | Bulut only | On-prem, karmaşık | On-prem + Cloud, Docker, tek komut |
| Fiyatlandırma | Açık kaynak + ops cost | Per-SIM subscription | Yüksek lisans | Rekabetçi, esnek model |

## Argus'un Temel Farkı

Hiçbir rakip tek üründe **5 katmanı** birden sunmuyor:
AAA + SIM Lifecycle + Multi-Operator + Policy Engine + BI Analytics

---

# Güvenlik & Uyumluluk

## Kimlik Doğrulama

| Yöntem | Kullanım |
|--------|----------|
| **JWT + Refresh Token** | Portal kullanıcıları (15dk + 7 gün) |
| **2FA (TOTP)** | Ek güvenlik katmanı |
| **API Key** | M2M entegrasyon, servis hesapları |
| **OAuth2 Client Credentials** | Üçüncü parti entegrasyon |

## RBAC (7 Rol)

| Rol | Yetki Alanı |
|-----|-------------|
| **Super Admin** | Platform yönetimi, tenant CRUD, operatör bağlantıları |
| **Tenant Admin** | Kullanıcı, APN, SIM, politika, audit — tenant kapsamında |
| **Operator Manager** | Operatör bağlantıları, APN, IP havuzları |
| **SIM Manager** | SIM CRUD, eSIM, bulk ops, oturum yönetimi |
| **Policy Editor** | Politika CRUD, DSL, rollout, versiyonlama |
| **Analyst** | Analitik, raporlar, dashboard (read-only) |
| **API User** | Programatik erişim, scope-sınırlı |

## Denetim & Uyumluluk

- **Tamper-proof Audit Log**: Hash chain ile değiştirilemez kayıtlar
- **Before/After Diff**: Her işlemde öncesi/sonrası karşılaştırması
- **KVKK Purge**: Pseudonymization ile kişisel veri temizleme
- **Rate Limiting**: Tenant/API key/endpoint bazında yapılandırılabilir
- **TLS Everywhere**: HTTPS, RadSec, Diameter/TLS
- **Input Validation**: XSS/SQLi önleme, CORS per-tenant

---

# Teknik Özellikler & Deployment

## Teknoloji Yığını

| Katman | Teknoloji |
|--------|-----------|
| Backend | Go 1.22+, chi router, single binary |
| Frontend | React 19, Vite 6, Tailwind CSS, shadcn/ui |
| Database | PostgreSQL 16 + TimescaleDB |
| Cache | Redis 7 |
| Message Bus | NATS JetStream |
| Container | Docker, Docker Compose |
| Reverse Proxy | Nginx (TLS termination, SPA serving) |

## Performans

| Metrik | Değer |
|--------|-------|
| Auth throughput | 10K+ req/s per node |
| Auth latency | p50 <5ms, p95 <20ms, p99 <50ms |
| Yönetilen SIM kapasitesi | 10M+ |
| Eşanlı oturum | 5M+ |
| Bulk işlem | 10K+ SIM per batch |
| Portal yükleme | <500ms (data populated) |
| CDR hacmi | 30-150M kayıt/gün |
| Uptime hedefi | %99.9 (AAA core) |

## Deployment

- **On-Premise**: Müşterinin kendi altyapısında
- **Private Cloud**: AWS, Azure, GCP
- **Docker**: `docker compose up` ile tek komut kurulum
- Horizontal scaling: AAA node'ları bağımsız ölçeklenir

## Devreye Alma Süreci

| Aşama | Süre |
|-------|------|
| Kurulum & konfigürasyon | 1-2 gün |
| Operatör adapter entegrasyonu | 1-2 hafta |
| SIM import & politika tanımlama | 1 hafta |
| Eğitim (admin + operatör) | 1-2 gün |
| Pilot çalıştırma | 2-4 hafta |
| Prod'a geçiş | 1 gün |

---

# İletişim

## NAR Sistem Teknoloji A.Ş.

IoT/M2M bağlantı yönetimi ve APN zekâsı ihtiyaçlarınız için bize ulaşın.

---

*ARGUS — APN & Subscriber Intelligence Platform*
*10M+ IoT/M2M SIM Yönetimini Tek Platformdan*
*Version 1.0 | 2026*
