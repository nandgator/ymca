-- Local-dev-only seed data. Applied automatically after 0001_init.sql via
-- docker-entrypoint-initdb.d (alphabetical order). Do NOT run this against
-- a real deployment — real accounts are provisioned by an actual Secretary/
-- CentralAdmin through the API, not by SQL script.

insert into hostels (id, name) values
    ('11111111-1111-1111-1111-111111111111', 'YMCA Colaba Hostel');

insert into hostel_policies (hostel_id, flat_monthly_fee_paise, non_veg_surcharge_paise, daily_deduction_paise, long_leave_threshold_days, menu_days)
values (
    '11111111-1111-1111-1111-111111111111',
    600000, -- ₹6000.00 flat monthly fee
    5000,   -- ₹50.00 non-veg dinner surcharge
    15000,  -- ₹150.00 deducted per long-leave day
    7,      -- 7+ days = LONG leave
    '{"Poha","Idli/Sambhar","Upma","Paratha","Dosa","Bread Omelette Station","Puri Bhaji"}'
);

insert into optional_items (id, hostel_id, name, price_paise) values
    ('aaaaaaa1-0000-0000-0000-000000000001', '11111111-1111-1111-1111-111111111111', 'Boiled Egg', 1000),
    ('aaaaaaa1-0000-0000-0000-000000000002', '11111111-1111-1111-1111-111111111111', 'Omelette',   2000),
    ('aaaaaaa1-0000-0000-0000-000000000003', '11111111-1111-1111-1111-111111111111', 'Tea',         500),
    ('aaaaaaa1-0000-0000-0000-000000000004', '11111111-1111-1111-1111-111111111111', 'Coffee',     1000),
    ('aaaaaaa1-0000-0000-0000-000000000005', '11111111-1111-1111-1111-111111111111', 'Milk',       1500),
    ('aaaaaaa1-0000-0000-0000-000000000006', '11111111-1111-1111-1111-111111111111', 'Sandwich',   2500);

insert into central_admins (id, staff_id, name, email) values
    ('22222222-2222-2222-2222-222222222222', 'ADMIN-001', 'Central Admin', 'admin@ymca.example');

insert into secretaries (id, hostel_id, staff_id, name, email) values
    ('33333333-3333-3333-3333-333333333333', '11111111-1111-1111-1111-111111111111', 'SEC-001', 'Hostel Secretary', 'secretary@ymca.example');

insert into members (id, hostel_id, member_id, name, email) values
    ('44444444-4444-4444-4444-444444444444', '11111111-1111-1111-1111-111111111111', 'YMCA-2026-0001', 'Demo Member', 'member@ymca.example');

-- To log in as any of these locally: POST /auth/otp/request with the
-- relevant role + login_id + channel EMAIL, then check the backend
-- container logs for the code (ConsoleSender prints it), then POST
-- /auth/otp/verify with that code.
