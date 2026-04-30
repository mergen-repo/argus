-- FIX-245 T2 DOWN: Re-insert data_portability_ready notification template rows.
-- Restores the two locale rows (tr, en) that were present before FIX-245.

INSERT INTO notification_templates (event_type, locale, subject, body_text, body_html, created_at, updated_at) VALUES
(
  'data_portability_ready', 'tr',
  'Veri Taşınabilirlik Talebiniz Hazır',
  'Veri taşınabilirlik talebiniz işlendi ve verileriniz indirmeye hazır. İndirme bağlantısına yönetim panelinden ulaşabilirsiniz.',
  '<p>Veri taşınabilirlik talebiniz işlendi. Verilerinizi indirmek için <a href="{{ .url }}">yönetim paneline</a> gidin.</p>',
  NOW(), NOW()
),
(
  'data_portability_ready', 'en',
  'Your Data Portability Request Is Ready',
  'Your data portability request has been processed and your data is ready for download. Access the download link from the management console.',
  '<p>Your data portability request has been processed. <a href="{{ .url }}">Visit the management console</a> to download your data.</p>',
  NOW(), NOW()
)
ON CONFLICT (event_type, locale) DO NOTHING;
