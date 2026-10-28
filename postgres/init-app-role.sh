#!/bin/sh
set -e
# Runs once, on first init of the postgres volume, as POSTGRES_USER.
# Creates the non-owner app role the gateway uses for tenant data, so
# Postgres RLS actually applies to it. Never give this role to an app
# directly; apps talk to the gateway, never to the database.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<EOSQL
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'cloud_app') THEN
    CREATE ROLE cloud_app LOGIN PASSWORD '$APP_DB_PASSWORD';
  END IF;
END
\$\$;
EOSQL
