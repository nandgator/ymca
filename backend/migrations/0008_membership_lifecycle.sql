-- 0008_membership_lifecycle — what suspending and terminating a membership need
-- that A2.4 did not give them.
--
-- Source of truth: docs/system-design/05.3, A2.4, A3.13, ADR-118.
--
-- L1  `membership.state` was free text, and 05.3's diagram and A2.4's comment
--     disagreed about its last value (TERMINATED against CANCELLED). The state
--     machine is now a CHECK, so a typo is a refusal and not a new state.
--
-- L2  A membership has at most one OPEN suspension. A2.4 allowed any number, so
--     two simultaneous suspensions would make "reinstate" lift one of them and
--     leave the member suspended by a row nobody could name. The constraint's
--     NAME is a contract with Go (internal/membership).
--
-- Forward-only. No Down section — see 0001 and 07.4.

-- +goose Up

ALTER TABLE membership
    ADD CONSTRAINT membership_state_known
    CHECK (state IN ('ACTIVE', 'SUSPENDED', 'LAPSED', 'TERMINATED'));

CREATE UNIQUE INDEX membership_one_open_suspension
    ON membership_suspension (membership_id) WHERE lifted_at IS NULL;
