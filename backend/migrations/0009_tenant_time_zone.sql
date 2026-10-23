-- 0009_tenant_time_zone — what "today" means for a tenant.
--
-- Source of truth: docs/system-design/09 ADR-119, A3.10, 11.2.
--
-- Z1  `tenant.time_zone` is an IANA zone name. It defaults to UTC, so a tenant
--     that existed before this migration behaves exactly as it did; an
--     association that is not at UTC is provisioned with its own.
--
-- Z2  The name is validated by a trigger, not a CHECK: a CHECK may not read
--     pg_timezone_names, and a CHECK that called a function over it would
--     change meaning when the server's zone database does. The constraint
--     NAME is a contract with Go (internal/organization).
--
-- Z3  Three functions are the only place "today" is derived, so no query
--     invents its own. Stored calendar dates keep their A3.10 representation
--     (a date is a UTC midnight when it must be a timestamp); only the question
--     "which date is it now" moves to the tenant's zone. They are not
--     SECURITY DEFINER, for ADR-115's reason; `tenant` carries no policy, so
--     they read it as whoever calls them. A tenant that does not exist yields
--     NULL, and every comparison against NULL is false: a term check fails
--     closed.
--
-- Z4  The financial year is a pure function of a calendar day, so it can be
--     tested on the days that matter (31 March, 1 April) without waiting for
--     them. It starts in April for every tenant (A3.11, 11.2).
--
-- Forward-only. No Down section — see 0001 and 07.4.

-- +goose Up

ALTER TABLE tenant ADD COLUMN time_zone text NOT NULL DEFAULT 'UTC';

-- +goose StatementBegin
CREATE FUNCTION tenant_time_zone_known() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- Region names and UTC only. pg_timezone_names also lists abbreviations
    -- (EST) and POSIX strings, which mean a fixed offset and silently ignore
    -- daylight saving.
    IF NEW.time_zone !~ '^(UTC|[A-Za-z_]+(/[A-Za-z0-9_+-]+)+)$'
       OR NOT EXISTS (SELECT 1 FROM pg_timezone_names WHERE name = NEW.time_zone) THEN
        RAISE EXCEPTION 'tenant time zone is not an IANA region name'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'tenant_time_zone_known';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd

CREATE TRIGGER tenant_time_zone_known
    BEFORE INSERT OR UPDATE OF time_zone ON tenant
    FOR EACH ROW EXECUTE FUNCTION tenant_time_zone_known();

-- The calendar day it is now, where the tenant is.
CREATE FUNCTION tenant_today(p_tenant uuid) RETURNS date
LANGUAGE sql STABLE AS $$
    SELECT (now() AT TIME ZONE t.time_zone)::date FROM tenant t WHERE t.id = p_tenant
$$;

-- The instant a calendar day began, where the tenant is.
CREATE FUNCTION tenant_day_start(p_tenant uuid, p_day date) RETURNS timestamptz
LANGUAGE sql STABLE AS $$
    SELECT p_day::timestamp AT TIME ZONE t.time_zone FROM tenant t WHERE t.id = p_tenant
$$;

-- The tenant's offset from UTC right now, in seconds east. A client without a
-- zone database turns an instant into the tenant's day with this alone.
CREATE FUNCTION tenant_utc_offset_seconds(p_tenant uuid) RETURNS integer
LANGUAGE sql STABLE AS $$
    SELECT extract(epoch FROM (now() AT TIME ZONE t.time_zone) - (now() AT TIME ZONE 'UTC'))::integer
      FROM tenant t WHERE t.id = p_tenant
$$;

-- '2025-26' for any day from 1 April 2025 to 31 March 2026.
CREATE FUNCTION financial_year(p_day date) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
    SELECT y::text || '-' || lpad(((y + 1) % 100)::text, 2, '0')
      FROM (SELECT CASE WHEN extract(month FROM p_day) >= 4
                        THEN extract(year FROM p_day)::int
                        ELSE extract(year FROM p_day)::int - 1 END AS y) fy
$$;
