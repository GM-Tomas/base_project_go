-- Sample holdings/platforms for exercising the dashboard locally against a real Postgres.
-- Run with psql or in Supabase SQL editor:
--   psql "$SUPABASE_DB_URL" -v dev_user_id="'<your-auth.users.id>'" -f db/seed/seed-dev.sql

BEGIN;

INSERT INTO platforms (user_id, name, type) VALUES
  (:dev_user_id, 'Balanz', 'Broker'),
  (:dev_user_id, 'Mercado Pago', 'Wallet'),
  (:dev_user_id, 'Banco Galicia', 'Bank'),
  (:dev_user_id, 'Nexo', 'Exchange'),
  (:dev_user_id, 'Binance', 'Exchange')
ON CONFLICT (user_id, name) DO NOTHING;

DELETE FROM holdings WHERE user_id = :dev_user_id;

INSERT INTO holdings (id, user_id, name, asset_class, platform_name, value_usd) VALUES
  (gen_random_uuid(), :dev_user_id, 'Cuenta remunerada ARS', 'Cash',         'Mercado Pago',   6150.00),
  (gen_random_uuid(), :dev_user_id, 'Plazo fijo UVA 90d',    'Fixed Income', 'Banco Galicia', 11300.00),
  (gen_random_uuid(), :dev_user_id, 'AL30D Bonar 2030',      'Fixed Income', 'Balanz',         9100.00),
  (gen_random_uuid(), :dev_user_id, 'S&P 500 Index Fund',    'Index Fund',   'Balanz',         5100.00),
  (gen_random_uuid(), :dev_user_id, 'AAPL',                  'Equity',       'Balanz',         7643.00),
  (gen_random_uuid(), :dev_user_id, 'NVDA',                  'Equity',       'Balanz',        10557.00),
  (gen_random_uuid(), :dev_user_id, 'Bitcoin',                'Crypto',       'Nexo',           9200.00),
  (gen_random_uuid(), :dev_user_id, 'Ethereum',               'Crypto',       'Nexo',           5580.00),
  (gen_random_uuid(), :dev_user_id, 'Bitcoin',                'Crypto',       'Binance',       13616.00),
  (gen_random_uuid(), :dev_user_id, 'USDT (stable)',          'Cash',         'Binance',        6004.00);

COMMIT;
