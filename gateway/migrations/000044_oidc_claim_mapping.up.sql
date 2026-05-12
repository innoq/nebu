-- Story 11-10: Seed default OIDC claim mapping into server_config.
-- ON CONFLICT DO NOTHING preserves any values already set manually.
-- New installs get Nebu defaults; existing installs keep any manually configured values.
INSERT INTO server_config (key, value, set_at) VALUES
  ('oidc_user_id_claim',      'name',  (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint),
  ('oidc_displayname_claim',  'name',  (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint),
  ('oidc_email_claim',        'email', (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint)
ON CONFLICT (key) DO NOTHING;
