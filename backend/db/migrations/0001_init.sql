-- YMCA Mess Management — initial schema
-- Applied automatically on first container start via
-- docker-entrypoint-initdb.d (see deploy/docker-compose.yml). To apply by
-- hand: psql "$DATABASE_URL" -f 0001_init.sql
--
-- Naming and rules here must match /CONTEXT.md exactly — that file is the
-- glossary, this is its enforcement.

create extension if not exists pgcrypto; -- gen_random_uuid()

create type meal_type as enum ('BREAKFAST', 'DINNER');
create type otp_channel as enum ('EMAIL', 'SMS');
create type subject_type as enum ('MEMBER', 'SECRETARY', 'CENTRAL_ADMIN');

-- ---------------------------------------------------------------------------
-- Hostels & policy
-- ---------------------------------------------------------------------------

create table hostels (
    id         uuid primary key default gen_random_uuid(),
    name       text not null,
    created_at timestamptz not null default now()
);

-- One-to-one with hostels. Every rule that could plausibly differ between
-- sites lives here, set by that hostel's Secretary — nothing pricing- or
-- rule-related is a global constant. See CONTEXT.md.
create table hostel_policies (
    hostel_id                 uuid primary key references hostels(id) on delete cascade,
    flat_monthly_fee_paise    bigint not null default 0 check (flat_monthly_fee_paise >= 0),
    non_veg_surcharge_paise   bigint not null default 0 check (non_veg_surcharge_paise >= 0),
    daily_deduction_paise     bigint not null default 0 check (daily_deduction_paise >= 0),
    long_leave_threshold_days int    not null default 7 check (long_leave_threshold_days >= 1),
    -- 7-element array, index 0 = Monday .. 6 = Sunday. Fixed baseline menu,
    -- never billed — distinct from optional_items below.
    menu_days                 text[7] not null default '{"","","","","","",""}',
    updated_at                timestamptz not null default now()
);

-- A-la-carte breakfast add-ons. Catalog and prices are per hostel.
create table optional_items (
    id         uuid primary key default gen_random_uuid(),
    hostel_id  uuid not null references hostels(id) on delete cascade,
    name       text not null,
    price_paise bigint not null check (price_paise >= 0),
    active     boolean not null default true,
    created_at timestamptz not null default now()
);
create index idx_optional_items_hostel on optional_items(hostel_id) where active;

-- ---------------------------------------------------------------------------
-- Actors — none of these self-register. Rows are inserted offline by a
-- Secretary/CentralAdmin. Login is OTP-only against member_id/staff_id plus
-- a verified email and/or mobile — there is no password_hash column anywhere.
-- ---------------------------------------------------------------------------

create table central_admins (
    id         uuid primary key default gen_random_uuid(),
    staff_id   text not null unique,
    name       text not null,
    email      text,
    mobile     text,
    created_at timestamptz not null default now(),
    constraint central_admins_contact_chk check (email is not null or mobile is not null)
);

create table secretaries (
    id         uuid primary key default gen_random_uuid(),
    hostel_id  uuid not null references hostels(id) on delete cascade,
    staff_id   text not null unique,
    name       text not null,
    email      text,
    mobile     text,
    created_at timestamptz not null default now(),
    constraint secretaries_contact_chk check (email is not null or mobile is not null)
);
create index idx_secretaries_hostel on secretaries(hostel_id);

create table members (
    id         uuid primary key default gen_random_uuid(),
    hostel_id  uuid not null references hostels(id) on delete cascade,
    member_id  text not null unique, -- human-facing login ID e.g. "YMCA-2026-0143"
    name       text not null,
    email      text,
    mobile     text,
    created_at timestamptz not null default now(),
    constraint members_contact_chk check (email is not null or mobile is not null)
);
create index idx_members_hostel on members(hostel_id);

-- ---------------------------------------------------------------------------
-- Entries — after-the-fact, same-day-only, immutable once locked.
-- Application layer enforces the midnight lock; the DB enforces the shape.
-- ---------------------------------------------------------------------------

create table entries (
    id          uuid primary key default gen_random_uuid(),
    member_id   uuid not null references members(id) on delete cascade,
    hostel_id   uuid not null references hostels(id) on delete cascade,
    entry_date  date not null,
    meal_type   meal_type not null,
    -- DINNER only: chosen fresh per entry, never a standing preference.
    non_veg     boolean not null default false,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    unique (member_id, entry_date, meal_type)
);
create index idx_entries_member_month on entries(member_id, entry_date);
create index idx_entries_hostel_month on entries(hostel_id, entry_date);

-- BREAKFAST-only many-to-many: which optional items were chosen on a given
-- entry. A DINNER entry must never have rows here — enforced in the domain
-- layer (ErrOptionalItemOnDinner) since a DB-level cross-table check would
-- need a trigger; kept simple here deliberately.
create table entry_optional_items (
    entry_id         uuid not null references entries(id) on delete cascade,
    optional_item_id uuid not null references optional_items(id),
    primary key (entry_id, optional_item_id)
);

-- ---------------------------------------------------------------------------
-- Leaves — self-registered, no approval step. SHORT vs LONG is derived from
-- duration vs hostel_policies.long_leave_threshold_days at read time, not
-- stored, so changing the threshold later re-classifies historical leaves
-- consistently rather than leaving stale labels behind.
-- ---------------------------------------------------------------------------

create table leaves (
    id         uuid primary key default gen_random_uuid(),
    member_id  uuid not null references members(id) on delete cascade,
    hostel_id  uuid not null references hostels(id) on delete cascade,
    start_date date not null,
    end_date   date not null,
    created_at timestamptz not null default now(),
    constraint leaves_dates_chk check (end_date >= start_date)
);
create index idx_leaves_member_range on leaves(member_id, start_date, end_date);

-- ---------------------------------------------------------------------------
-- Auth — OTP request/verify, then a bearer session token. No passwords.
-- ---------------------------------------------------------------------------

create table otp_codes (
    id            uuid primary key default gen_random_uuid(),
    subject_type  subject_type not null,
    subject_id    uuid not null,
    code_hash     text not null, -- sha256 hex of the 6-digit code, never store plaintext
    channel       otp_channel not null,
    destination   text not null, -- the email/mobile it was sent to, for audit
    attempt_count int not null default 0,
    expires_at    timestamptz not null,
    consumed_at   timestamptz,
    created_at    timestamptz not null default now()
);
create index idx_otp_subject on otp_codes(subject_type, subject_id, created_at desc);

create table sessions (
    id           uuid primary key default gen_random_uuid(),
    subject_type subject_type not null,
    subject_id   uuid not null,
    hostel_id    uuid, -- null for CENTRAL_ADMIN
    token_hash   text not null unique, -- sha256 hex of the bearer token, never store plaintext
    expires_at   timestamptz not null,
    created_at   timestamptz not null default now()
);
create index idx_sessions_token on sessions(token_hash);
