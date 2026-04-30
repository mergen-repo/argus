# APN & M2M SIM Yönetim Platformu

## Müşteri Soru-Cevap Matrisi

**Tarih:** 27 Nisan 2026
**Doküman Türü:** Teknik Değerlendirme — Soru-Cevap Yanıtları

---

## Özet

Platformumuz; çoklu mobil operatör altyapısı üzerinde 10 milyondan fazla M2M / IoT SIM kartını yönetebilen, AAA (kimlik doğrulama, yetkilendirme, ücretlendirme) protokollerini ve politika motorunu yerel olarak içeren, tek noktadan operasyon, izleme, denetim ve raporlama sağlayan bir abone yönetim çözümüdür. Aşağıda göndermiş olduğunuz **41 sorunun** her biri için sistemimizin durumu ve nasıl çalıştığı açıklanmıştır.

**Statü kodları:**

- ✅ **Var** — Özellik üretim ortamında mevcut
- 🟡 **Kısmi** — Temel yetkinlik var, bazı alt-özellikler isteğe göre genişletilebilir
- ❌ **Mevcut Değil** — Şu an kapsam dışı, ek geliştirme talebiyle eklenebilir
- 📋 **Sözleşmeyle Belirlenir** — Teknik altyapı hazır; kapsam ticari sözleşmede tanımlanır

---

## Soru-Cevap Matrisi

| # | Soru | Durum | Yanıt |
|---|------|:----:|-------|
| 1 | Sisteminiz farklı operatörler için APN tanımı yapmayı destekliyor mu? | ✅ | Birden fazla mobil operatör tanımlanabilir; her operatör altında bağımsız APN'ler oluşturulur. Her APN'nin kendi IP havuzu, politika kümesi ve kotası vardır. Operatör veya APN bazlı SIM dağılımı tek ekrandan yönetilir. |
| 2 | Radius Server nereye kurulacak? ISP'lerde mi kalacak? | ✅ | RADIUS sunucusu **müşterinin kendi veri merkezine** sistemin parçası olarak kurulur — ISP/operatör tarafına ek bileşen kurulmasına gerek yoktur. Operatörler standart RADIUS protokolüyle (1812/1813 portları) sistemimize bağlanır. Şifreleme için RADIUS-over-TLS desteklenir. |
| 3 | Fatura kontörlü yapabiliyor mu? | 🟡 | **Kontör (kota) yönetimi tam destekli**: SIM/grup başına aylık/günlük bayt, oturum sayısı, süre kotaları tanımlanır; kota aşımında otomatik askıya alma, hız kısıtlama veya bağlantı kesme tetiklenir. Operatör başına maliyet hesabı raporlanır. **Müşteriye PDF fatura üretimi ve ödeme entegrasyonu** çekirdek modülün dışındadır; mevcut ERP/faturalandırma yazılımına webhook veya CSV ile veri aktarımı yapılır. |
| 4 | APN ağlarının güvenliğini nasıl sağlıyorsunuz? | ✅ | Çok katmanlı güvenlik: (1) Operatör ile şifreli iletişim (TLS), (2) APN bazlı IP havuzu izolasyonu — bir APN'nin trafiği diğerine sızamaz, (3) IMSI/MSISDN bazlı erişim kuralları, (4) Şüpheli oturumların anlık olarak uzaktan kapatılması, (5) Davranışsal anomali tespiti (klonlama, beklenmeyen tüketim), (6) Tüm gizli anahtarların şifreli depolanması ve düzenli yenilenmesi. |
| 5 | Sistem kota kullanımı, oturum süresi ve bağlantı geçmişi gibi verileri nasıl raporluyor? | ✅ | Üç katmanlı raporlama: **(1) Anlık panel** — aktif oturum sayısı, kullanım hızı, top APN/operatör; **(2) Detay sayfa** — her SIM için oturum geçmişi, bayt grafiği, kota durumu, son hata sebebi; **(3) Hazır raporlar** — günlük/haftalık/aylık zamanlanmış kullanım, maliyet, anomali, SLA, kapasite raporları (CSV / PDF). |
| 6 | Statik ve dinamik IP atama süreçleri nasıl çalışıyor? | ✅ | Her APN için CIDR formatında IP havuzu tanımlanır. **Statik:** SIM'e sabit IP rezerve edilir; her bağlantıda aynı adres atanır. **Dinamik:** Bağlantı anında havuzdan boş bir IP otomatik tahsis edilir, oturum kapanınca havuza iade edilir. IP havuzu doluluğu için %80/%90/%100 eşik alarmları otomatik üretilir. |
| 7 | Boşta olan IP'lerin otomatik atanması özelliğiniz var mı? | ✅ | Dinamik havuzda en az kullanılan boş IP otomatik tahsis edilir. Oturum sonlandığında ya da boşta kalma süresi aşıldığında, yapılandırılabilir bir bekleme süresi (varsayılan 5 dk) sonrasında IP havuza geri kazandırılır. Sistem arka planda periyodik olarak unutulmuş kira kayıtlarını tarayıp temizler. |
| 8 | Toplu SIM kart tanımlama işlemlerini nasıl gerçekleştiriyorsunuz? | ✅ | Toplu yükleme ekranından **CSV dosyası** ile (ICCID, IMSI, MSISDN, APN, plan kolonları) yüzbinlerce SIM aynı anda tanımlanabilir. İşlem arka planda kuyruğa alınır; her satır doğrulanır, başarısız satırlar için indirilebilir hata raporu üretilir. İşlem ilerlemesi ekranda canlı izlenir. |
| 9 | Sistem üzerinde kullanıcı bazlı yetkilendirme yapılabiliyor mu? | ✅ | Rol bazlı yetkilendirme (RBAC) ile **7 hazır rol** sunulur: Süper Yönetici, Tenant Yöneticisi, Operatör Yöneticisi, SIM Yöneticisi, Politika Editörü, Analist, API Kullanıcısı. Her ekran ve aksiyon rol bazlı korunur. İki faktörlü doğrulama (2FA), API anahtar yönetimi ve çoklu kiracı (multi-tenant) izolasyonu standart olarak gelir. |
| 10 | Ek lisans gereksinimi olmadan tüm fonksiyonlar aktif şekilde çalışıyor mu? | 📋 | Sistem tek paket olarak teslim edilir; modül başına lisans kilidi yoktur — RADIUS, Diameter, 5G, politika, raporlama, denetim modüllerinin tamamı aynı kurulumun parçasıdır. Lisans ve garanti modeli ticari sözleşmede tanımlanır. |
| 11 | IMEI bazlı bir güvenlik yapısı kurabiliyor musunuz? IMEI havuzu oluşturma nasıl çalışıyor? | ✅ | IMEI yakalama her üç AAA protokolünde — **RADIUS** (3GPP-IMEISV VSA, vendor 10415 attr 20), **Diameter S6a** (Terminal-Information AVP 350) ve **5G SBA** (PEI / Permanent Equipment Identifier) — yerel olarak yapılır. Organizasyon seviyesinde **White / Grey / Black IMEI havuzları** tanımlanabilir; her havuz girişi tam IMEI veya 8-haneli TAC aralığı (üretici/model bazlı) olabilir. Toplu CSV import, **IMEI Lookup** arama aracı (hangi IMEI hangi SIM'e bağlı, hangi havuzda, son ne zaman görüldü), havuz-SIM çapraz referans desteklenir. Black list = hard-deny, Grey list = alert-only, White list = filo bazlı izin kuralları. |
| 12 | SIM kartların belirli cihazlarla sınırlandırılması veya farklı cihazlarda çalışabilmesi nasıl yönetiliyor? | ✅ | Altı **SIM-cihaz kilit modu** sunulur: **strict** (1:1 sabit IMEI), **allowlist** (SIM başına izinli IMEI listesi), **first-use** (ilk takıldığı cihaza otomatik kilit), **tac-lock** (sadece belirli model/üreticiye), **grace-period** (yetkili cihaz değişimi için zaman pencereli — varsayılan 72 saat), **soft** (sadece alarm, reddetme yok). Kilit modu **SIM bazında** (SIM Detay > Cihaz Kilidi sekmesi), **segment bazında** (toplu aksiyon) veya **politika kuralı** olarak (Politika Editörü `device.*` predicate'leri) uygulanabilir. Yetkili cihaz değişimi için yöneticinin yapabildiği "Re-pair" iş akışı mevcuttur — eski/yeni IMEI denetim kaydına yazılır. |
| 13 | Cihaz doğrulama sürecinde IMEI, IMSI ve CID verilerini nasıl kontrol ediyorsunuz? | ✅ | **IMSI doğrulaması** standart RADIUS akışında yapılır; politika motorunda IMSI aralığı, ön ek ve beyaz liste kuralları kurulabilir. **Charging ID (CID)** çağrı detay kayıtlarında saklanır ve raporlanır. **IMEI doğrulaması** üç protokolde eş zamanlı yapılır (Soru 11): yakalanan IMEI, havuz üyeliği (white/grey/black) ve SIM'in `bound_imei` alanıyla karşılaştırılır; kilit moduna göre kabul/red/alarm kararı verilir. Üç kimlik birlikte değerlendirilir; politika DSL'de `imsi`, `device.imei`, `device.tac`, `cid` predicate'leri kombinleyerek karmaşık doğrulama kuralları yazılabilir. |
| 14 | Doğrulanamayan cihazlar için nasıl bir aksiyon alıyorsunuz, raporlama yapılıyor mu? | ✅ | IMSI/MSISDN/IMEI doğrulama başarısızlıklarında: (1) Bağlantı reddedilir ve sebep kodu döner (`device_mismatch`, `imei_blacklist_hit`, `unauthorized_imsi`, `unauthorized_apn` vb.), (2) Politika ihlal kaydı oluşturulur, (3) Kilit moduna göre alarm tetiklenir (strict/blacklist = high; soft/grace-period = info/warning), (4) Kriptografik zincirli denetim kaydı düşer, (5) "Doğrulanamayan Cihazlar" raporu (filo genelinde `pending` ve `mismatch` durumundaki SIM'ler) otomatik üretilir, (6) Aktif oturum varsa anlık olarak uzaktan kesilir (RFC 5176 Disconnect-Message). |
| 15 | Hat kaybolduğunda veya çalındığında hattı uzaktan kapatma imkânı var mı? | ✅ | SIM detay ekranından "Kayıp / Çalıntı Bildir" aksiyonu ile SIM derhal askıya alınır. Aktif oturum varsa, ek bir komut gerekmeden uzaktan bağlantı kesme komutu ağa gönderilir ve kullanıcı düşürülür. Tüm sonraki bağlantı denemeleri otomatik reddedilir. İşlem değiştirilemez denetim kaydına yazılır. |
| 16 | APN ağına bağlı kullanıcıların bağlantısını manuel olarak kesebiliyor musunuz? (RFC5176 desteği var mı?) | ✅ | **Evet, RFC 5176 (Dynamic Authorization Extensions) tam desteklidir.** Tek oturum veya birden fazla oturum tek tıkla kesilebilir. Aktif kullanıcı oturumlarına anlık olarak bant genişliği değişimi (CoA) veya bağlantı sonlandırma (Disconnect) komutu gönderilir. Komutun ağa ulaşıp ulaşmadığı, kabul/red durumu ekranda izlenir. |
| 17 | Farklı operatör, APN veya kullanıcı bazlı gruplamalar nasıl yapılabiliyor? | ✅ | "Segment" yapısıyla; operatör + APN + durum + plan + etiket + özel filtreler birleştirilerek kayıtlı gruplar oluşturulur. Toplu aksiyonlar (askıya alma, politika atama, bildirim gönderme) bu gruplar üzerinden çalışır. SIM listesinde filtre çubuğu ile interaktif olarak da gruplanabilir. |
| 18 | Gruplama yapısı dinamik olarak yönetilebiliyor mu? | ✅ | Gruplar **donmuş bir liste değil, canlı sorgudur**. Bir SIM'in özellikleri değiştiğinde (örn. kota tükendi, durum aktife döndü) ilgili gruba otomatik dahil olur veya çıkar. Etiket bazlı gruplar manuel atamayla, özellik bazlı gruplar otomatik kuralla yönetilir. |
| 19 | Her SIM kart için birden fazla framed-route tanımlayabiliyor musunuz? | ❌ | Mevcut sürümde RADIUS Access-Accept yanıtında **tek IP adresi** dönülmektedir; SIM başına birden fazla rota (Framed-Route, RFC 2865 attribute 22) emit edilmemektedir. Bu özellik talep halinde geliştirme kapsamına alınabilir. |
| 20 | Local IP veya NAS IP üzerinden test/ping işlemleri ile sorun tespiti yapılabiliyor mu? | ✅ | SIM detay ekranında **Tanılama** sekmesi vardır: (1) APN-operatör eşlemesinin doğruluğu, (2) Son kimlik doğrulama hata sebebi, (3) IP tahsis durumu, (4) Operatöre erişim gecikmesi, (5) NAS IP bilgilerinin tutarlılığı tek tıkla otomatik kontrol edilir. Manuel ping/traceroute butonu ile ek test yapılabilir. |
| 21 | APN ile operatör arasındaki bağlantı kesildiğinde sistem nasıl bir bildirim üretiyor? | ✅ | Operatör erişilebilirliği sürekli izlenir (heartbeat). Eşik aşımında: (1) Ana panodaki operatör sağlık kartı kırmızıya döner ve canlı güncellenir, (2) Otomatik alarm kaydı oluşturulur, (3) E-posta / SMS / Telegram / webhook bildirimi gönderilir, (4) Tanımlı yedekleme stratejisi (ret / yedek operatör / kuyruğa al) tetiklenir. |
| 22 | Sistem üzerinde anlık durum monitörü bulunuyor mu? Hangi bilgileri gösteriyor? | ✅ | Ana panoda canlı (saniye bazlı güncellenen) gösterge: aktif oturum sayısı, kimlik doğrulama hızı (auth/sn), throughput (Mbps), top APN ve operatörler, IP havuzu doluluğu, son alarmlar, operatör sağlık durumu (yeşil/sarı/kırmızı), uzaktan kesme komut kuyruğu uzunluğu. |
| 23 | Etkinlik monitörü ile oturum bazlı hangi detayları görebiliyoruz? | ✅ | Canlı oturum ekranında ve oturum detayında: SIM kimliği (ICCID/IMSI/MSISDN), APN, atanan IP, NAS IP, başlangıç zamanı / süre, indirilen ve gönderilen bayt, uygulanan politika kuralları, kota durumu, son kimlik doğrulama sonucu, gönderilen uzaktan kesme komutları geçmişi, denetim olayları zaman çizelgesi. Tek tıkla bağlantı kesme butonu mevcuttur. |
| 24 | Kullanıcı aktiviteleri (bağlantı, veri kullanımı vb.) nasıl kayıt altına alınıyor? | ✅ | Üç ayrı katmanda kayıt tutulur: **(1) Çağrı Detay Kayıtları (CDR)** — tüm bayt sayaçları ve oturum süreleri; **(2) Oturum Kayıtları** — aktif ve sonlanmış oturumların yaşam döngüsü; **(3) Denetim Kayıtları** — SIM bazında durum değiştiren her olay. Geçmiş veri zaman serisi optimizasyonlu veritabanında 90+ gün tutulur (yapılandırılabilir). |
| 25 | Yönetici kullanıcıların yaptığı işlemler (ekleme, silme, güncelleme) loglanıyor mu? | ✅ | Tüm yönetici aksiyonları (SIM, APN, politika, kullanıcı oluşturma / güncelleme / silme) **değiştirilemez denetim kayıtlarına** yazılır: kim yaptı, ne zaman, ne yaptı, önce/sonra farkı, IP ve tarayıcı bilgisi. Kayıtlar **kriptografik zincir** ile bağlanır — sonradan müdahale edilirse anında tespit edilir. Denetim ekranından arama, filtre ve CSV dışa aktarım yapılabilir. KVKK uyumlu maskeleme desteklenir. |
| 26 | Dinamik raporlama özellikleriniz nelerdir? | ✅ | Raporlama modülünde **8 hazır rapor şablonu** sunulur: Kullanım, Maliyet, Anomali, Uyumluluk, Çağrı Detayı, SLA, Kapasite, Roaming. Her rapor: (1) anlık üretilebilir, (2) zamanlanabilir (haftalık / aylık), (3) filtre parametreleriyle (tarih, operatör, APN, segment) özelleştirilebilir, (4) CSV / PDF / JSON formatlarında alınabilir. |
| 27 | APN ve kullanıcı bazlı hangi raporları üretebiliyorsunuz? | ✅ | **APN bazlı:** bayt kullanımı, oturum sayısı, en çok kullanan SIM listesi, kota aşımı listesi, operatör maliyeti, SLA. **Kullanıcı/SIM bazlı:** kullanım grafiği, oturum geçmişi, politika ihlal geçmişi, denetim zaman çizelgesi, maliyet dağılımı. Raporlar operatör × APN × ağ tipi (RAT) gibi kombinasyonlarda pivot edilebilir. |
| 28 | Alert (uyarı) mekanizması nasıl çalışıyor? Hangi durumlarda bildirim gönderiliyor? | ✅ | Alarm kategorileri: **(1) Kota** — %80/%90/%100 eşikleri; **(2) Operatör** — erişilemez, yavaş, SLA ihlali; **(3) Politika** — kural ihlali, uyumsuz tatbikat; **(4) Anomali** — SIM klonlama, ani trafik artışı, kötüye kullanım; **(5) Sistem** — IP havuzu tükendi, kuyruk gecikmesi, veritabanı yavaşlığı. Her alarm önem seviyesi (kritik/uyarı/bilgi) ile ölçeklenir. Sessize alma kuralları ve "kuralı kaydet" özelliğiyle gereksiz tekrar engellenir. |
| 29 | Bu bildirimler e-posta/SMS olarak iletilebiliyor mu? | ✅ | Bildirim kanalları: **E-posta (SMTP), SMS, Telegram, Webhook (HTTP+imza), Uygulama içi.** Türkçe/İngilizce şablon sistemi ve kanal başına hız sınırlaması mevcuttur. Hangi alarmın hangi kanala kimlere gideceği ekrandan yapılandırılır. |
| 30 | Geçmiş alarm ve uyarılar geriye dönük incelenebiliyor mu? | ✅ | Tüm alarmlar zaman damgalı olarak değiştirilemez biçimde saklanır — silinemez. Bildirim ekranında tarih aralığı, önem, kaynak, durum (yeni / onaylandı / çözüldü) filtreleriyle aranır; CSV olarak dışa aktarılır. Saklama süresi yapılandırılabilir (varsayılan 365 gün). Onay / çözme iş akışı ve sorumluya atama desteklenir. |
| 31 | Syslog veya benzeri log yönlendirme sistemlerine entegrasyon var mı? | ✅ | İki yönlü entegrasyon desteklenir: (1) Yerel **RFC 3164 (BSD legacy)** ve **RFC 5424 (modern structured)** syslog iletici — UDP, TCP ve TLS (mTLS opsiyonel) taşıma seçenekleri; her hedef için filtre kuralları (auth/audit/alert/policy/imei/system kategorileri); test bağlantısı butonu; teslimat hatası ve son başarı zaman damgası izleme. (2) **HTTP webhook** ile imzalı (HMAC-SHA256), retry mekanizmalı olay aktarımı. SIEM araçları (Splunk, QRadar, ArcSight, Elastic) ile out-of-the-box uyumluluk; tek bir tenant altında birden fazla hedef tanımlanabilir. |
| 32 | İlk kurulum ve konfigürasyon süreci nasıl ilerliyor? Üretici desteği sağlanıyor mu? | 📋 | **Teknik akış:** Tek komutla konteyner tabanlı kurulum; ardından **5 adımlı yönlendirilmiş kurulum sihirbazı** (Tenant > Operatör > APN > SIM Yükleme > Politika) ile sistem üretime hazır hale gelir. Veritabanı şema migrasyonları ve örnek veriler otomatik uygulanır. **Üretici destek kapsamı** (kurulum, eğitim, devir-teslim) ticari sözleşmede tanımlanır. |
| 33 | Mevcut APN'lerin sisteme aktarımı ilk kurulumda yapılabiliyor mu? | ✅ | Kurulum sihirbazında APN toplu yükleme adımı ile mevcut operatör konfigürasyonları içe aktarılır; SIM listesi de aynı sırada CSV ile yüklenir. Geçiş öncesi simülatör operatör bağlantısıyla deneme (smoke test) yapılarak gerçek operatöre geçmeden doğrulama sağlanır. |
| 34 | Garanti süresi boyunca yeni APN kurulumları yapılabiliyor mu? | 📋 | **Teknik açıdan:** Yeni APN tanımı sistem ayakta iken ekrandan eklenir, kesinti yoktur. Operatör tarafı yapılandırması (peer config, gizli anahtar) operatör konnektörüne yüklenir. **Sözleşme açıdan:** Garanti dahilinde yeni APN ekleme limitleri ticari sözleşmede tanımlanır. |
| 35 | Yazılım güncellemeleri ve yeni sürümler garanti kapsamında mı? | 📋 | **Teknik açıdan:** Sıfır kesinti hedefli (mavi-yeşil) deploy, geriye dönük uyumlu veritabanı migrasyonları, sürüm notları ve sürüm bilgisi her ekranda görüntülenir. **Sözleşme açıdan:** Garanti süresince sürüm yükseltme hakkı ve bakım pencereleri ticari sözleşmede tanımlanır. |
| 36 | Entegrasyonlar için bakım ve destek hizmeti sağlıyor musunuz? | 📋 | **Teknik altyapı hazır:** REST API + WebSocket, imzalı webhook teslimatı (yeniden deneme dahil), operatör konnektörü için açık genişletme noktaları, OpenAPI dokümantasyonu. **Hizmet seviyesi** (Standart Destek / Profesyonel Hizmetler) ticari sözleşmede tanımlanır. |
| 37 | Garanti süresi bittikten sonra sistem lisans gerektirmeden çalışmaya devam ediyor mu? | 📋 | **Teknik açıdan:** Çalışma anında lisans kontrolü yoktur; garanti bitiminde sistem otomatik kapatma yapmaz. **Sözleşme açıdan:** Lisans modeli (perpetual / abonelik / kullanım bazlı) ticari sözleşmede tanımlanır. |
| 38 | Kullanıcı ve hat tanımları tamamen sistem üzerinden yapılabiliyor mu? | ✅ | %100 self-servis: SIM tek tek veya toplu tanımlama, portal kullanıcısı oluşturma, rol atama, 2FA kurulumu, API anahtar üretimi — hepsi web arayüzünden yapılır. Komut satırı veya doğrudan veritabanı müdahalesine ihtiyaç duyulmaz; tüm işlemler denetim kaydına geçer. |
| 39 | Toplu veri ekleme ve dışa aktarma işlemleri nasıl gerçekleştiriliyor? | ✅ | **Ekleme:** CSV ile SIM ve numara aralığı yükleme, JSON toplu API, zamanlanmış rapor / kural toplu oluşturma. **Dışa aktarma:** SIM listesi, çağrı detay kayıtları, denetim kayıtları, alarm geçmişi, raporlar (CSV / PDF) ve KVKK kapsamında kullanıcı verilerinin tek paket halinde indirilmesi. Tüm dışa aktarmalar arka plan işi olarak çalışır, ilerleme ekranda izlenir. |
| 40 | Sistem üzerinde profiling (kullanıcı tespiti) servisi bulunuyor mu? | ✅ | İki katmanlı profilleme motoru sunulur: (1) **Davranışsal anomali tespiti** — SIM klonlama (aynı IMSI farklı NAS), ani trafik artışı, kötüye kullanım kalıbı, coğrafi anomali; SIM/APN/operatör bazlı canlı kullanım profili anlık panoda. (2) **Cihaz parmak izi (IMEI/TAC bazlı) profilleme** — her SIM için tarihsel IMEI gözlem zaman çizelgesi (yakalama protokolü + NAS IP + uyumsuzluk işareti), cihaz değişim sıklığı, anormal cihaz değişim alarmı, üretici/model bazlı (TAC) filo segmentasyonu. İki katman birleştirildiğinde "şüpheli SIM aktivitesi + bilinmeyen cihaz" gibi karmaşık tespitler raporlanabilir. |
| 41 | Bağlanan tüm kullanıcıları otomatik olarak tespit edip raporlayabiliyor musunuz? | ✅ | Bir SIM kimliği doğrulanır doğrulanmaz oturum kaydı oluşur, canlı oturum ekranında anında görünür ve günlük bağlantı raporlarında listelenir. "Aktif Oturumlar" ve "Günlük Bağlantılar" raporları otomatik üretilir. (Tespit IMSI/ICCID üzerindendir; cihaz tanımlama ek modül kapsamında değerlendirilir.) |

---

## Genel Değerlendirme

| Kategori | Adet | Yüzde |
|----------|----:|----:|
| ✅ Var | 33 | %80 |
| 🟡 Kısmi | 1 | %2 |
| ❌ Mevcut Değil | 1 | %2 |
| 📋 Sözleşmeyle Belirlenir | 6 | %15 |
| **Toplam** | **41** | **%100** |

### Güçlü Yönler

- **Çoklu operatör orkestrasyonu** — Birden fazla operatör altında bağımsız APN, IP havuzu, politika ve kota yönetimi
- **AAA protokol yığını** — RADIUS (RFC 2865/2866), Diameter, 5G SBA ve uzaktan oturum kesme (RFC 5176) yerel olarak desteklenir; ek bileşene gerek yoktur
- **M2M Cihaz Güvenliği** — IMEI yakalama (üç protokol), 6 SIM-cihaz kilit modu, IMEI havuz yönetimi (white/grey/black + TAC aralığı), IMEI değişim tespiti ve re-pair iş akışı, davranışsal + cihaz parmak izi profilleme
- **Politika ve Kota Motoru** — Kural tabanlı kota, otomatik askıya alma, throttle, dry-run senaryo testi, `device.*` predicate'leri ile cihaz tabanlı politika
- **Değiştirilemez Denetim** — Kriptografik zincirli, KVKK uyumlu denetim ve veri taşınabilirliği
- **Operasyonel Olgunluk** — Kurulum sihirbazı, toplu yükleme, segment bazlı toplu aksiyon, zamanlanmış raporlar
- **Çoklu Bildirim ve Log Yönlendirme** — E-posta, SMS, Telegram, Webhook, uygulama içi + native syslog (RFC 3164/5424) ile SIEM hazır (Splunk, QRadar, ArcSight, Elastic)

---

### Geliştirme Talebine Açık Konular

1. **SIM başına çoklu rota** — Birden fazla Framed-Route attribute desteği (Soru 19, niche kullanım — talep halinde efor: ~1 hafta)
2. **Faturalandırma modülü** — PDF fatura üretimi ve ödeme entegrasyonu (Soru 3). Mevcut: kullanım/kontör takibi + ERP webhook + CDR export. Önerilen pozisyon: BSS-ready entegrasyon, mevcut faturalandırma yazılımıyla çalışır.
3. **EIR (Equipment Identity Register) Entegrasyonu** — Operatör HSS/AMF üzerinden 3GPP S13/N17 sorgusu (operatör pazarında genişlemek isteyen müşteriler için talep halinde değerlendirilir)

### Sözleşmeyle Belirlenecek Konular

Sorular 10, 32, 34, 35, 36, 37 — kurulum desteği, garanti dahili APN ekleme, sürüm yükseltme, bakım hizmeti, garanti sonrası lisans modeli — **teknik altyapı bu senaryoların tamamını destekler**; ticari kapsam sözleşmede tanımlanacaktır.

---

## Sonraki Adım

Sözleşme öncesi **canlı demo ve POC (Kavram Kanıtı)** kurulumu planlanabilir. Kurulu simülatör operatör bağlantısı sayesinde, gerçek üretim trafiğine geçmeden önce çoklu operatör senaryoları, kota kuralları, IMEI binding modları, alarm akışları, syslog/SIEM entegrasyonu ve raporlama yetenekleri uçtan uca test edilebilir.

*Bu doküman 27 Nisan 2026 tarihli mevcut sürüm temelinde hazırlanmıştır.*
