# Database backup

`lumi-schema-2026-10-01.sql` is the complete `lumi` schema (tables, sequences,
indexes and data) as it stood when lumi-go was undeployed on 2026-10-01. It was
taken from the shared dev Postgres it ran in (Neon, Postgres 17) with:

    pg_dump "$DATABASE_URL" -n lumi --no-owner

Verified by restoring into an empty Postgres 17: 2 conversations, 4 messages,
1 news digest, 2 token-usage rows.

## Restore

    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backups/lumi-schema-2026-10-01.sql

It creates the `lumi` schema, so the target must not already have one. Use
Postgres 17 client tools; pg_dump/psql 16 refuse a 17 server.
