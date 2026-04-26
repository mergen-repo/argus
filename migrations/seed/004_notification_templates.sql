-- FIX-237 — remove deprecated consumer-voice + Tier 1 templates
DELETE FROM notification_templates
 WHERE event_type IN (
    'welcome','sim_state_change','session_login','onboarding_completed','data_portability_ready'
 );

-- FIX-237: 17 event types × 2 locales (tr, en) = 34 rows
-- Tier 1 consumer-voice templates removed; Tier 2 digest + Tier 3 operational templates added.
-- Turkish strings use proper diacritics (ç ğ ı ö ş ü).
-- Idempotent: ON CONFLICT (event_type, locale) DO UPDATE.

BEGIN;

INSERT INTO notification_templates (event_type, locale, subject, body_text, body_html, created_at, updated_at) VALUES

-- ============================================================
-- operator_degraded
-- ============================================================
(
  'operator_degraded', 'tr',
  'Operatör Uyarısı: {{ .operator_name }} — Servis Kalitesi Düştü',
  '{{ .operator_name }} operatörünün servis kalitesi {{ .event_time }} itibarıyla beklenen eşiğin altına düştü. Bu durum bağlı SIM kartlarınızın veri ve ses hizmetlerini olumsuz etkileyebilir. Teknik ekibimiz sorunu çözmek için çalışmaktadır. Gelişmelerden haberdar edileceksiniz.',
  '<p><strong>{{ .operator_name }}</strong> operatörünün servis kalitesi <strong>{{ .event_time }}</strong> itibarıyla beklenen eşiğin altına düştü.</p><p>Bu durum bağlı SIM kartlarınızın hizmetlerini olumsuz etkileyebilir. Teknik ekibimiz sorunu çözmek için çalışmaktadır.</p>',
  NOW(), NOW()
),
(
  'operator_degraded', 'en',
  'Operator Alert: {{ .operator_name }} — Service Quality Degraded',
  'The service quality of operator {{ .operator_name }} has dropped below the expected threshold as of {{ .event_time }}. This may adversely affect data and voice services on connected SIMs. Our technical team is working to resolve the issue. You will be notified of any updates.',
  '<p>The service quality of operator <strong>{{ .operator_name }}</strong> has dropped below the expected threshold as of <strong>{{ .event_time }}</strong>.</p><p>This may adversely affect connected SIM services. Our technical team is working to resolve the issue.</p>',
  NOW(), NOW()
),

-- ============================================================
-- policy_violation
-- ============================================================
(
  'policy_violation', 'tr',
  'Politika İhlali Tespit Edildi: {{ .policy_name }}',
  '{{ .sim_id }} kimlikli SIM kart {{ .event_time }} tarihinde "{{ .policy_name }}" politikasını ihlal etti. Tanımlı kural eşiği aşıldığından otomatik önlem devreye alındı. İhlal detayları için yönetim panelini inceleyiniz. Tekrarlayan ihlaller hesabınızın kısıtlanmasına yol açabilir.',
  '<p>SIM <strong>{{ .sim_id }}</strong>, <strong>{{ .event_time }}</strong> tarihinde <strong>{{ .policy_name }}</strong> politikasını ihlal etti.</p><p>Otomatik önlem devreye alındı. İhlal detayları için <a href="{{ .url }}">yönetim panelini</a> inceleyiniz.</p>',
  NOW(), NOW()
),
(
  'policy_violation', 'en',
  'Policy Violation Detected: {{ .policy_name }}',
  'SIM {{ .sim_id }} violated the policy "{{ .policy_name }}" at {{ .event_time }}. The defined rule threshold was exceeded and an automatic enforcement action has been applied. Please review the management console for violation details. Repeated violations may result in account restrictions.',
  '<p>SIM <strong>{{ .sim_id }}</strong> violated policy <strong>{{ .policy_name }}</strong> at <strong>{{ .event_time }}</strong>.</p><p>An automatic enforcement action has been applied. Please review the <a href="{{ .url }}">management console</a> for details.</p>',
  NOW(), NOW()
),

-- ============================================================
-- ip_pool_warning
-- ============================================================
(
  'ip_pool_warning', 'tr',
  'IP Havuzu Uyarısı: Yüksek Kullanım Oranı Tespit Edildi',
  'IP havuzunuzun kullanım oranı {{ .event_time }} itibarıyla kritik eşiğe ulaştı. Mevcut kapasitenin büyük bölümü kullanılmış durumda; bu durum yeni oturum açılmasını engelleyebilir. Havuz kapasitesini artırmak veya kullanılmayan IP adreslerini serbest bırakmak için lütfen sistem yöneticinizle görüşün.',
  '<p>IP havuzunuzun kullanım oranı <strong>{{ .event_time }}</strong> itibarıyla kritik eşiğe ulaştı.</p><p>Yeni oturum açılmasının engellenmemesi için havuz kapasitesini artırmanız veya kullanılmayan IP adreslerini serbest bırakmanız önerilir.</p>',
  NOW(), NOW()
),
(
  'ip_pool_warning', 'en',
  'IP Pool Warning: High Utilization Detected',
  'Your IP pool utilization has reached a critical threshold as of {{ .event_time }}. A large portion of available capacity is in use, which may prevent new sessions from being established. Please contact your system administrator to expand pool capacity or release unused IP addresses.',
  '<p>Your IP pool utilization has reached a critical threshold as of <strong>{{ .event_time }}</strong>.</p><p>To prevent new sessions from being blocked, consider expanding pool capacity or releasing unused IP addresses.</p>',
  NOW(), NOW()
),

-- ============================================================
-- anomaly_detected
-- ============================================================
(
  'anomaly_detected', 'tr',
  'Anomali Tespit Edildi: {{ .sim_id }} — Olağandışı Trafik Örüntüsü',
  '{{ .sim_id }} kimlikli SIM kartta {{ .event_time }} tarihinde olağandışı bir trafik örüntüsü tespit edildi. Anomali motoru bu davranışı şüpheli olarak işaretlemiştir. Hesabınızın güvenliğini sağlamak adına ilgili SIM kartı incelemenizi öneririz. Gerekirse kartı geçici olarak askıya alabilirsiniz.',
  '<p>SIM <strong>{{ .sim_id }}</strong> üzerinde <strong>{{ .event_time }}</strong> tarihinde olağandışı bir trafik örüntüsü tespit edildi.</p><p>Anomali motoru bu davranışı şüpheli olarak işaretledi. <a href="{{ .url }}">Detayları incelemek</a> için yönetim paneline gidin.</p>',
  NOW(), NOW()
),
(
  'anomaly_detected', 'en',
  'Anomaly Detected: {{ .sim_id }} — Unusual Traffic Pattern',
  'An unusual traffic pattern was detected on SIM {{ .sim_id }} at {{ .event_time }}. The anomaly detection engine has flagged this behavior as suspicious. We recommend reviewing the affected SIM to ensure account security. You may temporarily suspend the SIM if necessary.',
  '<p>An unusual traffic pattern was detected on SIM <strong>{{ .sim_id }}</strong> at <strong>{{ .event_time }}</strong>.</p><p>The anomaly engine flagged this as suspicious. <a href="{{ .url }}">Review the details</a> in the management console.</p>',
  NOW(), NOW()
),

-- ============================================================
-- kvkk_purge_completed
-- ============================================================
(
  'kvkk_purge_completed', 'tr',
  'KVKK Silme İşlemi Tamamlandı',
  '{{ .tenant_name }} hesabınız kapsamında {{ .event_time }} tarihinde KVKK saklama politikası gereği otomatik veri silme işlemi başarıyla tamamlandı. Silinen kayıtlara artık erişilememektedir. Uyumluluk kaydı oluşturulmuş ve denetim günlüğünüze eklenmiştir. Herhangi bir sorunuz için destek ekibimize başvurabilirsiniz.',
  '<p><strong>{{ .tenant_name }}</strong> hesabınız kapsamında <strong>{{ .event_time }}</strong> tarihinde KVKK saklama politikası gereği otomatik veri silme işlemi tamamlandı.</p><p>Uyumluluk kaydı denetim günlüğünüze eklenmiştir.</p>',
  NOW(), NOW()
),
(
  'kvkk_purge_completed', 'en',
  'KVKK Data Purge Completed',
  'An automated data purge was successfully completed for {{ .tenant_name }} at {{ .event_time }} in accordance with the KVKK retention policy. The purged records are no longer accessible. A compliance record has been generated and added to your audit log. Please contact support if you have any questions.',
  '<p>An automated KVKK data purge was completed for <strong>{{ .tenant_name }}</strong> at <strong>{{ .event_time }}</strong>.</p><p>A compliance record has been added to your audit log.</p>',
  NOW(), NOW()
),

-- ============================================================
-- sms_delivery_failed
-- ============================================================
(
  'sms_delivery_failed', 'tr',
  'SMS Gönderimi Başarısız: {{ .msisdn }}',
  '{{ .msisdn }} numarasına {{ .event_time }} tarihinde gönderilmek istenen SMS mesajı sağlayıcı tarafında başarısız oldu. Hata kodu: {{ .error_code }}. Mesaj otomatik yeniden deneme kuyruğuna alınmıştır. Sorun devam ederse SMS sağlayıcı ayarlarınızı kontrol etmenizi öneririz.',
  '<p><strong>{{ .msisdn }}</strong> numarasına <strong>{{ .event_time }}</strong> tarihinde gönderilmek istenen SMS sağlayıcı tarafında başarısız oldu (Hata: {{ .error_code }}).</p><p>Mesaj yeniden deneme kuyruğuna alındı. Sorun devam ederse SMS ayarlarınızı kontrol edin.</p>',
  NOW(), NOW()
),
(
  'sms_delivery_failed', 'en',
  'SMS Delivery Failed: {{ .msisdn }}',
  'An SMS message to {{ .msisdn }} scheduled at {{ .event_time }} failed at the provider level. Error code: {{ .error_code }}. The message has been placed in the automatic retry queue. If the issue persists, we recommend reviewing your SMS provider configuration.',
  '<p>An SMS to <strong>{{ .msisdn }}</strong> at <strong>{{ .event_time }}</strong> failed at the provider level (Error: {{ .error_code }}).</p><p>The message has been queued for retry. If the issue persists, review your SMS provider configuration.</p>',
  NOW(), NOW()
),

-- ============================================================
-- report_ready
-- ============================================================
(
  'report_ready', 'tr',
  '{{ .report_type }} Raporunuz Hazır',
  'Talep ettiğiniz {{ .report_type }} raporu {{ .event_time }} tarihinde oluşturuldu. Raporu indirmek veya görüntülemek için aşağıdaki bağlantıyı kullanabilirsiniz. Bağlantı 48 saat süreyle geçerli olacaktır. Süre dolduktan sonra raporu yönetim panelindeki geçmiş bölümünden ulaşabilirsiniz.',
  '<p><strong>{{ .report_type }}</strong> raporunuz <strong>{{ .event_time }}</strong> tarihinde hazırlandı.</p><p>Raporu indirmek için <a href="{{ .download_url }}">buraya tıklayın</a>. Bağlantı 48 saat geçerlidir.</p>',
  NOW(), NOW()
),
(
  'report_ready', 'en',
  'Your {{ .report_type }} Report Is Ready',
  'The {{ .report_type }} report you requested was generated at {{ .event_time }}. You can use the link below to download or view the report. The link will be valid for 48 hours. After expiry, you can access the report from the history section in the management console.',
  '<p>Your <strong>{{ .report_type }}</strong> report was generated at <strong>{{ .event_time }}</strong>.</p><p><a href="{{ .download_url }}">Click here to download</a> the report. The link is valid for 48 hours.</p>',
  NOW(), NOW()
),

-- ============================================================
-- webhook_dead_letter
-- ============================================================
(
  'webhook_dead_letter', 'tr',
  'Webhook Ölü Mektup Kuyruğu: {{ .url }}',
  '{{ .url }} adresine yönelik webhook bildirimi tüm yeniden deneme girişimlerinin ardından başarısız oldu ve ölü mektup kuyruğuna alındı. İlk hata zamanı: {{ .event_time }}. Webhook uç noktanızın erişilebilir ve doğru yapılandırılmış olduğunu kontrol edin. Sorunu çözdükten sonra yönetim panelinden yeniden deneyebilirsiniz.',
  '<p>Webhook <strong>{{ .url }}</strong> tüm yeniden denemeler tükendikten sonra ölü mektup kuyruğuna alındı (ilk hata: {{ .event_time }}).</p><p>Uç noktanızın erişilebilir olduğunu doğrulayın ve <a href="{{ .url }}">yönetim panelinden</a> yeniden deneyin.</p>',
  NOW(), NOW()
),
(
  'webhook_dead_letter', 'en',
  'Webhook Dead-Letter: {{ .url }}',
  'The webhook notification to {{ .url }} has failed after all retry attempts and has been moved to the dead-letter queue. First failure time: {{ .event_time }}. Please verify that your webhook endpoint is accessible and correctly configured. After resolving the issue, you can retry from the management console.',
  '<p>Webhook <strong>{{ .url }}</strong> has been moved to the dead-letter queue after all retries were exhausted (first failure: {{ .event_time }}).</p><p>Verify endpoint accessibility and retry from the <a href="{{ .url }}">management console</a>.</p>',
  NOW(), NOW()
),

-- ============================================================
-- ip_released
-- ============================================================
(
  'ip_released', 'tr',
  'IP Adresi Serbest Bırakıldı: {{ .ip_address }}',
  '{{ .ip_address }} IP adresi {{ .event_time }} tarihinde bekleme süresi dolduktan sonra havuza geri döndürüldü ve yeniden atanmaya hazır hale getirildi. Bu adres daha önce {{ .sim_id }} kimlikli SIM karta atanmıştı. İşlem kayıt altına alınmış olup denetim günlüğünüzde görüntüleyebilirsiniz.',
  '<p>IP adresi <strong>{{ .ip_address }}</strong>, <strong>{{ .event_time }}</strong> tarihinde bekleme süresi dolarak havuza geri döndürüldü.</p><p>Bu adres daha önce <strong>{{ .sim_id }}</strong> kimlikli SIM karta atanmıştı. İşlem denetim günlüğünüzde kayıtlıdır.</p>',
  NOW(), NOW()
),
(
  'ip_released', 'en',
  'IP Address Released: {{ .ip_address }}',
  'IP address {{ .ip_address }} was returned to the pool at {{ .event_time }} after the grace period expired and is now available for re-assignment. This address was previously assigned to SIM {{ .sim_id }}. The action has been recorded and is visible in your audit log.',
  '<p>IP address <strong>{{ .ip_address }}</strong> was released back to the pool at <strong>{{ .event_time }}</strong> after the grace period expired.</p><p>Previously assigned to SIM <strong>{{ .sim_id }}</strong>. The action is recorded in your audit log.</p>',
  NOW(), NOW()
),

-- ============================================================
-- job.completed
-- ============================================================
(
  'job.completed', 'tr',
  'Toplu İşlem Tamamlandı: {{.ExtraFields.file_name}}',
  '{{.ExtraFields.file_name}} dosyası için toplu içe aktarma işlemi tamamlandı. Toplam: {{.ExtraFields.total}}, Başarılı: {{.ExtraFields.success_count}}, Başarısız: {{.ExtraFields.fail_count}}. Detaylar için iş geçmişi sayfasını ziyaret edin.',
  '<p><strong>{{.ExtraFields.file_name}}</strong> dosyası için toplu içe aktarma işlemi tamamlandı.</p><p>Toplam: <strong>{{.ExtraFields.total}}</strong>, Başarılı: <strong>{{.ExtraFields.success_count}}</strong>, Başarısız: <strong>{{.ExtraFields.fail_count}}</strong>.</p>',
  NOW(), NOW()
),
(
  'job.completed', 'en',
  'Job Completed: {{.ExtraFields.file_name}}',
  'Bulk import for {{.ExtraFields.file_name}} completed. Total: {{.ExtraFields.total}}, Successful: {{.ExtraFields.success_count}}, Failed: {{.ExtraFields.fail_count}}. Visit the job history page for details.',
  '<p>Bulk import for <strong>{{.ExtraFields.file_name}}</strong> completed.</p><p>Total: <strong>{{.ExtraFields.total}}</strong>, Successful: <strong>{{.ExtraFields.success_count}}</strong>, Failed: <strong>{{.ExtraFields.fail_count}}</strong>.</p>',
  NOW(), NOW()
),

-- ============================================================
-- fleet_mass_offline  (Tier 2 — digest)
-- ============================================================
(
  'fleet_mass_offline', 'tr',
  'Filo uyarısı: {{.offline_count}} SIM çevrimdışı (%{{.offline_pct}})',
  'Son 15 dakikada {{.active_count}} aktif SIM''in {{.offline_count}} tanesi çevrimdışı oldu (%{{.offline_pct}}). Olası bir ağ kesintisi veya toplu oturum düşmesi için panel üzerinden inceleyin. Olay zamanı: {{.event_time}}.',
  '<p><strong>Filo Uyarısı</strong> — <strong>{{.offline_count}}</strong> SIM çevrimdışı (%{{.offline_pct}}) — {{.event_time}}</p><p>Son 15 dakikada <strong>{{.active_count}}</strong> aktif SIM''in <strong>{{.offline_count}}</strong> tanesi çevrimdışı oldu. Panel üzerinden inceleyin.</p>',
  NOW(), NOW()
),
(
  'fleet_mass_offline', 'en',
  'Fleet alert: {{.offline_count}} SIMs offline ({{.offline_pct}}%)',
  'In the last 15 minutes, {{.offline_count}} of {{.active_count}} active SIMs went offline ({{.offline_pct}}%). Investigate via the dashboard. Event time: {{.event_time}}.',
  '<p><strong>Fleet Alert</strong> — <strong>{{.offline_count}}</strong> SIMs offline ({{.offline_pct}}%) — {{.event_time}}</p><p>In the last 15 minutes, <strong>{{.offline_count}}</strong> of <strong>{{.active_count}}</strong> active SIMs went offline. Investigate via the dashboard.</p>',
  NOW(), NOW()
),

-- ============================================================
-- fleet_traffic_spike  (Tier 2 — digest)
-- ============================================================
(
  'fleet_traffic_spike', 'tr',
  'Trafik artışı: {{.ratio}}× taban çizgisi',
  '{{.event_time}} itibarıyla filo trafiği taban çizgisinin {{.ratio}} katına ulaştı. Etkilenen SIM sayısı: {{.sim_count}}. Olası DDoS, kötüye kullanım veya toplu uygulama güncellemesi için panel üzerinden inceleyin.',
  '<p><strong>Trafik Artışı</strong> — {{.ratio}}× taban çizgisi — {{.event_time}}</p><p>Filo trafiği taban çizgisinin <strong>{{.ratio}}</strong> katına ulaştı. Etkilenen SIM: <strong>{{.sim_count}}</strong>. Panel üzerinden inceleyin.</p>',
  NOW(), NOW()
),
(
  'fleet_traffic_spike', 'en',
  'Traffic spike: {{.ratio}}× baseline',
  'Fleet traffic reached {{.ratio}}× the baseline as of {{.event_time}}. Affected SIMs: {{.sim_count}}. Investigate via the dashboard for possible DDoS, abuse, or mass application update.',
  '<p><strong>Traffic Spike</strong> — {{.ratio}}× baseline — {{.event_time}}</p><p>Fleet traffic reached <strong>{{.ratio}}×</strong> the baseline. Affected SIMs: <strong>{{.sim_count}}</strong>. Investigate via the dashboard.</p>',
  NOW(), NOW()
),

-- ============================================================
-- fleet_quota_breach_count  (Tier 2 — digest)
-- ============================================================
(
  'fleet_quota_breach_count', 'tr',
  'Kota aşımları: {{.breach_count}} SIM son 15 dakikada kotasını aştı',
  '{{.event_time}} itibarıyla son 15 dakika içinde {{.breach_count}} SIM kota sınırını aştı. Politika eşiğini veya etkilenen SIM''lerin kotasını gözden geçirin. Detaylar için yönetim panelini inceleyin.',
  '<p><strong>Kota Aşımı</strong> — {{.breach_count}} SIM — {{.event_time}}</p><p>Son 15 dakika içinde <strong>{{.breach_count}}</strong> SIM kota sınırını aştı. Politika eşiğini veya etkilenen SIM''lerin kotasını gözden geçirin.</p>',
  NOW(), NOW()
),
(
  'fleet_quota_breach_count', 'en',
  'Quota breaches: {{.breach_count}} SIMs exceeded quota in 15 min',
  'As of {{.event_time}}, {{.breach_count}} SIMs exceeded their quota limit in the last 15 minutes. Review the policy threshold or the quota allocation for affected SIMs. Check the management console for details.',
  '<p><strong>Quota Breaches</strong> — {{.breach_count}} SIMs — {{.event_time}}</p><p><strong>{{.breach_count}}</strong> SIMs exceeded their quota limit in the last 15 minutes. Review the policy threshold or quota allocation for affected SIMs.</p>',
  NOW(), NOW()
),

-- ============================================================
-- fleet_violation_surge  (Tier 2 — digest)
-- ============================================================
(
  'fleet_violation_surge', 'tr',
  'Politika ihlali artışı: {{.violation_count}} olay ({{.ratio}}× taban çizgisi)',
  '{{.event_time}} itibarıyla son 15 dakikada {{.violation_count}} politika ihlali olayı tespit edildi; bu, taban çizgisinin {{.ratio}} katına karşılık gelmektedir. Sistematik bir uyumsuzluk veya kötüye kullanım için yönetim panelini inceleyin.',
  '<p><strong>İhlal Artışı</strong> — {{.violation_count}} olay ({{.ratio}}×) — {{.event_time}}</p><p>Son 15 dakikada <strong>{{.violation_count}}</strong> politika ihlali tespit edildi (taban çizgisinin <strong>{{.ratio}}</strong> katı). Yönetim panelini inceleyin.</p>',
  NOW(), NOW()
),
(
  'fleet_violation_surge', 'en',
  'Policy violation surge: {{.violation_count}} events ({{.ratio}}× baseline)',
  'As of {{.event_time}}, {{.violation_count}} policy violation events were detected in the last 15 minutes, which is {{.ratio}}× the baseline. Investigate via the management console for systematic non-compliance or abuse.',
  '<p><strong>Violation Surge</strong> — {{.violation_count}} events ({{.ratio}}×) — {{.event_time}}</p><p><strong>{{.violation_count}}</strong> policy violation events in the last 15 minutes ({{.ratio}}× baseline). Investigate via the management console.</p>',
  NOW(), NOW()
),

-- ============================================================
-- bulk_job_completed  (Tier 3 — operational)
-- ============================================================
(
  'bulk_job_completed', 'tr',
  'Toplu iş tamamlandı: {{.job_type}}',
  '{{.job_type}} türündeki {{.bulk_job_id}} toplu iş tamamlandı. Toplam: {{.total_count}}, Başarılı: {{.success_count}}, Başarısız: {{.fail_count}}. Detaylar için iş geçmişi sayfasını ziyaret edin.',
  '<p><strong>Toplu İş Tamamlandı</strong> — {{.job_type}} — {{.bulk_job_id}}</p><p>Toplam: <strong>{{.total_count}}</strong> &nbsp;|&nbsp; Başarılı: <strong>{{.success_count}}</strong> &nbsp;|&nbsp; Başarısız: <strong>{{.fail_count}}</strong></p>',
  NOW(), NOW()
),
(
  'bulk_job_completed', 'en',
  'Bulk job complete: {{.job_type}}',
  'Bulk {{.job_type}} job {{.bulk_job_id}} completed: {{.success_count}}/{{.total_count}} successful, {{.fail_count}} failed. Visit the job history page for details.',
  '<p><strong>Bulk Job Complete</strong> — {{.job_type}} — {{.bulk_job_id}}</p><p>Total: <strong>{{.total_count}}</strong> &nbsp;|&nbsp; Successful: <strong>{{.success_count}}</strong> &nbsp;|&nbsp; Failed: <strong>{{.fail_count}}</strong></p>',
  NOW(), NOW()
),

-- ============================================================
-- bulk_job_failed  (Tier 3 — operational)
-- ============================================================
(
  'bulk_job_failed', 'tr',
  'Toplu iş BAŞARISIZ: {{.job_type}}',
  '{{.job_type}} türündeki {{.bulk_job_id}} toplu iş kritik bir hata nedeniyle başarısız oldu. Hata: {{.error_message}}. İş geçmişi sayfasından detayları inceleyin ve gerekirse yeniden çalıştırın. Olay zamanı: {{.event_time}}.',
  '<p><strong>Toplu İş BAŞARISIZ</strong> — {{.job_type}} — {{.bulk_job_id}}</p><p>Hata: <strong>{{.error_message}}</strong></p><p>İş geçmişi sayfasından detayları inceleyin ve gerekirse yeniden çalıştırın.</p>',
  NOW(), NOW()
),
(
  'bulk_job_failed', 'en',
  'Bulk job FAILED: {{.job_type}}',
  'Bulk {{.job_type}} job {{.bulk_job_id}} failed due to a critical error: {{.error_message}}. Review the job history page for details and re-run if necessary. Event time: {{.event_time}}.',
  '<p><strong>Bulk Job FAILED</strong> — {{.job_type}} — {{.bulk_job_id}}</p><p>Error: <strong>{{.error_message}}</strong></p><p>Review the job history page for details and re-run if necessary.</p>',
  NOW(), NOW()
),

-- ============================================================
-- backup_verify_failed  (Tier 3 — operational)
-- ============================================================
(
  'backup_verify_failed', 'tr',
  'Yedek doğrulama BAŞARISIZ — {{.backup_id}}',
  'URGENT: {{.backup_id}} kimlikli yedek dosyasının bütünlük doğrulaması {{.event_time}} tarihinde başarısız oldu. Hata: {{.error_message}}. Yedek güvenilir değil; lütfen yedek altyapısını derhal kontrol edin ve kurtarılabilir bir yedeğin mevcut olduğunu doğrulayın.',
  '<p><strong>URGENT — Yedek Doğrulama BAŞARISIZ</strong> — {{.backup_id}} — {{.event_time}}</p><p>Hata: <strong>{{.error_message}}</strong></p><p>Yedek güvenilir değil. Yedek altyapısını derhal kontrol edin ve kurtarılabilir bir yedeğin mevcut olduğunu doğrulayın.</p>',
  NOW(), NOW()
),
(
  'backup_verify_failed', 'en',
  'Backup verify FAILED — {{.backup_id}}',
  'URGENT: Integrity verification for backup {{.backup_id}} failed at {{.event_time}}. Error: {{.error_message}}. This backup is not trustworthy — check the backup infrastructure immediately and confirm that a recoverable backup is available.',
  '<p><strong>URGENT — Backup Verify FAILED</strong> — {{.backup_id}} — {{.event_time}}</p><p>Error: <strong>{{.error_message}}</strong></p><p>This backup is not trustworthy. Check the backup infrastructure immediately and confirm a recoverable backup is available.</p>',
  NOW(), NOW()
)

ON CONFLICT (event_type, locale) DO UPDATE SET
  subject      = EXCLUDED.subject,
  body_text    = EXCLUDED.body_text,
  body_html    = EXCLUDED.body_html,
  updated_at   = NOW();

COMMIT;
