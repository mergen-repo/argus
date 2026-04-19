-- STORY-069 AC-8: Seed notification_templates for 14 event types × 2 locales (tr, en) = 28 rows
-- Turkish strings use proper diacritics (ç ğ ı ö ş ü).
-- Idempotent: ON CONFLICT (event_type, locale) DO UPDATE.

BEGIN;

INSERT INTO notification_templates (event_type, locale, subject, body_text, body_html, created_at, updated_at) VALUES

-- ============================================================
-- welcome
-- ============================================================
(
  'welcome', 'tr',
  '{{ .tenant_name }} platformuna hoş geldiniz, {{ .user_name }}!',
  'Merhaba {{ .user_name }}, {{ .tenant_name }} platformuna başarıyla kayıt oldunuz. Hesabınız artık aktif ve kullanıma hazır. Herhangi bir sorunuz olursa destek ekibimizle iletişime geçebilirsiniz. Güvenli kullanımlar dileriz.',
  '<p>Merhaba <strong>{{ .user_name }}</strong>, <strong>{{ .tenant_name }}</strong> platformuna başarıyla kayıt oldunuz.</p><p>Hesabınız artık aktif ve kullanıma hazır. Herhangi bir sorunuz olursa <a href="{{ .support_url }}">destek ekibimizle</a> iletişime geçebilirsiniz.</p>',
  NOW(), NOW()
),
(
  'welcome', 'en',
  'Welcome to {{ .tenant_name }}, {{ .user_name }}!',
  'Hello {{ .user_name }}, your account on {{ .tenant_name }} has been successfully created. Your account is now active and ready to use. If you have any questions, please contact our support team. We wish you a productive experience.',
  '<p>Hello <strong>{{ .user_name }}</strong>, your account on <strong>{{ .tenant_name }}</strong> has been successfully created.</p><p>Your account is now active and ready to use. If you have any questions, please <a href="{{ .support_url }}">contact our support team</a>.</p>',
  NOW(), NOW()
),

-- ============================================================
-- sim_state_change
-- ============================================================
(
  'sim_state_change', 'tr',
  'SIM Durum Değişikliği: {{ .msisdn }} — {{ .new_state }}',
  '{{ .msisdn }} numaralı SIM kartınızın durumu {{ .event_time }} tarihinde {{ .old_state }} konumundan {{ .new_state }} konumuna değiştirildi. Bu değişikliği siz yapmadıysanız lütfen sistem yöneticinizle iletişime geçin. Değişikliği tetikleyen kural: {{ .policy_name }}.',
  '<p><strong>{{ .msisdn }}</strong> numaralı SIM kartınızın durumu <strong>{{ .event_time }}</strong> tarihinde <strong>{{ .old_state }}</strong> → <strong>{{ .new_state }}</strong> olarak güncellendi.</p><p>Değişikliği tetikleyen kural: {{ .policy_name }}. Bu değişikliği siz yapmadıysanız lütfen sistem yöneticinizle iletişime geçin.</p>',
  NOW(), NOW()
),
(
  'sim_state_change', 'en',
  'SIM State Change: {{ .msisdn }} — {{ .new_state }}',
  'The state of SIM {{ .msisdn }} was changed from {{ .old_state }} to {{ .new_state }} at {{ .event_time }}. The triggering policy was: {{ .policy_name }}. If you did not initiate this change, please contact your system administrator immediately.',
  '<p>The state of SIM <strong>{{ .msisdn }}</strong> was changed from <strong>{{ .old_state }}</strong> to <strong>{{ .new_state }}</strong> at <strong>{{ .event_time }}</strong>.</p><p>Triggering policy: <strong>{{ .policy_name }}</strong>. If you did not initiate this change, please contact your system administrator immediately.</p>',
  NOW(), NOW()
),

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
-- data_portability_ready
-- ============================================================
(
  'data_portability_ready', 'tr',
  'GDPR Veri Dışa Aktarma Arşiviniz Hazır',
  'Talep ettiğiniz GDPR veri dışa aktarma arşivi {{ .event_time }} tarihinde hazırlandı. Arşivi indirmek için aşağıdaki bağlantıyı kullanabilirsiniz. Güvenlik nedeniyle bu bağlantı 72 saat boyunca geçerli olacaktır. Süre dolduktan sonra yeni bir dışa aktarma talebinde bulunmanız gerekmektedir.',
  '<p>GDPR veri dışa aktarma arşiviniz <strong>{{ .event_time }}</strong> tarihinde hazırlandı.</p><p>Arşivi indirmek için <a href="{{ .download_url }}">buraya tıklayın</a>. Bu bağlantı 72 saat boyunca geçerlidir.</p>',
  NOW(), NOW()
),
(
  'data_portability_ready', 'en',
  'Your GDPR Data Portability Archive Is Ready',
  'The GDPR data export archive you requested was prepared at {{ .event_time }}. You can use the link below to download your archive. For security reasons, this link will be valid for 72 hours. After expiry, you will need to submit a new export request.',
  '<p>Your GDPR data export archive was prepared at <strong>{{ .event_time }}</strong>.</p><p><a href="{{ .download_url }}">Click here to download</a> your archive. This link is valid for 72 hours.</p>',
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
-- onboarding_completed
-- ============================================================
(
  'onboarding_completed', 'tr',
  '{{ .tenant_name }} Kurulum Sihirbazı Tamamlandı',
  'Tebrikler, {{ .user_name }}! {{ .tenant_name }} kurulum sihirbazını {{ .event_time }} tarihinde başarıyla tamamladınız. Platformun tüm özelliklerine artık erişebilirsiniz. Başlamak için yönetim panelini ziyaret edebilir veya destek belgelerimizi inceleyebilirsiniz.',
  '<p>Tebrikler, <strong>{{ .user_name }}</strong>! <strong>{{ .tenant_name }}</strong> kurulum sihirbazını <strong>{{ .event_time }}</strong> tarihinde başarıyla tamamladınız.</p><p>Platformun tüm özelliklerine artık erişebilirsiniz. <a href="{{ .url }}">Yönetim paneline gidin</a>.</p>',
  NOW(), NOW()
),
(
  'onboarding_completed', 'en',
  '{{ .tenant_name }} Onboarding Wizard Completed',
  'Congratulations, {{ .user_name }}! You have successfully completed the {{ .tenant_name }} onboarding wizard at {{ .event_time }}. You now have access to all platform features. Visit the management console to get started or review our support documentation.',
  '<p>Congratulations, <strong>{{ .user_name }}</strong>! You successfully completed the <strong>{{ .tenant_name }}</strong> onboarding wizard at <strong>{{ .event_time }}</strong>.</p><p>You now have full access. <a href="{{ .url }}">Go to the management console</a>.</p>',
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
-- session_login
-- ============================================================
(
  'session_login', 'tr',
  'Yeni Oturum Açıldı: {{ .user_name }}',
  '{{ .user_name }} kullanıcısı {{ .event_time }} tarihinde {{ .tenant_name }} platformuna başarıyla giriş yaptı. Bu girişi siz yapmadıysanız, hesabınızın güvenliği tehlikede olabilir; lütfen şifrenizi hemen değiştirin ve destek ekibimize bildirin. Oturum IP adresi: {{ .ip_address }}.',
  '<p><strong>{{ .user_name }}</strong>, <strong>{{ .event_time }}</strong> tarihinde <strong>{{ .tenant_name }}</strong> platformuna giriş yaptı (IP: {{ .ip_address }}).</p><p>Bu girişi siz yapmadıysanız şifrenizi hemen değiştirin ve <a href="{{ .support_url }}">destek ekibimize bildirin</a>.</p>',
  NOW(), NOW()
),
(
  'session_login', 'en',
  'New Session Login: {{ .user_name }}',
  'User {{ .user_name }} successfully logged into the {{ .tenant_name }} platform at {{ .event_time }}. If you did not perform this login, your account may be compromised — please change your password immediately and notify our support team. Session IP address: {{ .ip_address }}.',
  '<p><strong>{{ .user_name }}</strong> logged into <strong>{{ .tenant_name }}</strong> at <strong>{{ .event_time }}</strong> (IP: {{ .ip_address }}).</p><p>If you did not perform this login, <a href="{{ .support_url }}">change your password and notify support</a> immediately.</p>',
  NOW(), NOW()
)

-- ============================================================
-- job.completed
-- ============================================================
,
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
)

ON CONFLICT (event_type, locale) DO UPDATE SET
  subject      = EXCLUDED.subject,
  body_text    = EXCLUDED.body_text,
  body_html    = EXCLUDED.body_html,
  updated_at   = NOW();

COMMIT;
