# Schema-applier image: a thin postgres:16-alpine that bundles schema.sql so the
# startup Job can run `psql -f /schema/schema.sql` against the Postgres service.
# We reuse the official postgres image purely for its bundled psql client.
FROM postgres:16-alpine

COPY schema.sql /schema/schema.sql

# No ENTRYPOINT override: the Job specifies the psql command and connection env.
