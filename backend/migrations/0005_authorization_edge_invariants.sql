-- 0005_authorization_edge_invariants — ADR-016's three invariants, enforced
-- by the database on every write rather than by whoever remembers to call a
-- checker.
--
-- Source of truth: docs/system-design/A2_data_model.md A2.2;
-- 05_building_block_view/05.1_organization.md §5.1.3; ADR-016, ADR-115.
--
-- 0001 created `authorization_edge` with a comment reading "Cycle detection
-- runs before commit, in a recursive CTE. Depth bounded by policy, default
-- 12. See H4 — not yet written." This is that CTE. Until now the table
-- accepted a cycle, and ADR-016's guarantee — permissions are the union over
-- all paths, and the walk terminates — did not hold.
--
-- Nothing inserted an edge before this migration, so there is no existing
-- row to validate: the trigger is correct from the first edge the system
-- ever writes, which is the one `POST /t/{t}/units` is about to write.
--
-- Forward-only. No Down section — see 0001 and 07.4.

-- +goose Up

-- ─────────────────────────────────────────────────────────────
-- WHY A TRIGGER AND NOT A FUNCTION THE HANDLER CALLS (ADR-115)
--
-- 05.1.3 words invariant 1 as "enforced on every write", and a checker the
-- caller invokes is enforced on every write the caller remembers. The
-- endpoint that exists today is not the only writer this table will ever
-- have — a second-auth-parent endpoint, a bulk import (B5), a repair script
-- and every integration test are all writers, and each would have to
-- re-derive the same CTE or silently skip it.
--
-- It also makes the guarantee testable. A cycle is unreachable through
-- `POST /t/{t}/units` by construction — nothing points at a unit that did
-- not exist a moment ago — so a checker living only in that path could
-- never be observed to fail, which §3 of the handoff names as
-- indistinguishable from a check that cannot fail. Here a direct INSERT
-- exercises it, and the drift test does exactly that.
-- ─────────────────────────────────────────────────────────────

-- +goose StatementBegin
CREATE FUNCTION authorization_edge_invariants() RETURNS trigger
LANGUAGE plpgsql
-- The function is invoked by ymca_app and runs as ymca_app: NOT SECURITY
-- DEFINER, deliberately. Its queries below read authorization_edge and
-- organizational_unit, both under FORCE ROW LEVEL SECURITY, and running as
-- the definer (a superuser) would exempt them from the tenant policy — so
-- the walk would traverse other tenants' edges to decide this tenant's
-- invariant. ADR-108's whole point, in a place it would be easy to lose.
SET search_path = pg_catalog, public
AS $$
DECLARE
    -- A2.2's "depth bounded by policy, default 12", counted in EDGES: at
    -- most 12 auth_parent hops on any path through this edge, which is what
    -- bounds the traversal OpenFGA performs for `admin from auth_parent`.
    --
    -- A constant, not a policy row. A2 says "configured maximum, defaulting
    -- to 12" and nothing here is configurable per tenant; 11.2 carries that.
    max_depth constant int := 12;

    up_depth      int;      -- edges from the new child upward, incl. this one
    down_depth    int;      -- edges from the new child downward
    reaches_child boolean;  -- does the new parent already sit below the child
    parent_ok     boolean;
    child_ok      boolean;
BEGIN
    -- ── The types this trigger knows how to validate ────────────────
    --
    -- A1.2 declares `auth_parent` on resource, programme and
    -- consumption_type as well, and none of them is writable yet. Refusing
    -- them is fail-closed and self-announcing: the migration that makes one
    -- writable must extend the tenant check below rather than inherit an
    -- edge whose endpoints nothing validated. An unbuilt limb is visible in
    -- the code as unbuilt (the same reasoning as internal/authz/validity.go).
    IF NEW.child_type <> 'organizational_unit'
       OR NEW.parent_type NOT IN ('organizational_unit', 'tenant') THEN
        RAISE EXCEPTION
            'authorization_edge: % -> % is not a validated edge type',
            NEW.child_type, NEW.parent_type
            USING ERRCODE = 'check_violation',
                  CONSTRAINT = 'authorization_edge_supported_types';
    END IF;

    -- ── Invariant 2 · both endpoints resolve to this tenant ─────────
    --
    -- The row's own tenant_id is already forced to the session tenant: the
    -- RLS policy has no WITH CHECK, so PostgreSQL reuses its USING
    -- expression for INSERT. What that does NOT say is that the objects the
    -- edge names belong to the same tenant, which is what these two check.
    -- The reads are under RLS, so another tenant's unit is simply not there.
    IF NEW.parent_type = 'tenant' THEN
        parent_ok := NEW.parent_id = NEW.tenant_id;
    ELSE
        SELECT true INTO parent_ok FROM organizational_unit WHERE id = NEW.parent_id;
    END IF;
    SELECT true INTO child_ok FROM organizational_unit WHERE id = NEW.child_id;

    IF NOT coalesce(parent_ok, false) OR NOT coalesce(child_ok, false) THEN
        RAISE EXCEPTION
            'authorization_edge: an endpoint does not resolve to tenant %',
            NEW.tenant_id
            USING ERRCODE = 'check_violation',
                  CONSTRAINT = 'authorization_edge_same_tenant';
    END IF;

    -- ── Invariant 1 · no cycles, part one ───────────────────────────
    --
    -- The one-node cycle. It is separated out because the walk below starts
    -- AT the parent, so a self-edge would be found by it only by accident of
    -- how the seed row is written.
    IF NEW.child_type = NEW.parent_type AND NEW.child_id = NEW.parent_id THEN
        RAISE EXCEPTION 'authorization_edge: % is its own authorization parent',
            NEW.child_id
            USING ERRCODE = 'check_violation',
                  CONSTRAINT = 'authorization_edge_acyclic';
    END IF;

    -- ── The walk up, which answers both remaining invariants ────────
    --
    -- Seeded at the new parent with depth 1 — the edge being inserted — so
    -- up_depth is the number of edges from the new child to the highest
    -- ancestor reachable through this parent.
    --
    -- `depth <= max_depth` is not an optimization. It is what makes this
    -- query terminate if the table ever DOES hold a cycle: an unbounded
    -- recursive CTE over a cyclic graph does not stop, and a guard that
    -- hangs is worse than one that refuses. It walks one level past the
    -- limit so that "too deep" is distinguishable from "exactly at the
    -- limit".
    WITH RECURSIVE up(node_type, node_id, depth) AS (
        SELECT NEW.parent_type, NEW.parent_id, 1
        UNION ALL
        SELECT e.parent_type, e.parent_id, up.depth + 1
          FROM authorization_edge e
          JOIN up ON e.child_type = up.node_type AND e.child_id = up.node_id
         WHERE up.depth <= max_depth
    )
    SELECT max(depth),
           bool_or(node_type = NEW.child_type AND node_id = NEW.child_id)
      INTO up_depth, reaches_child
      FROM up;

    IF coalesce(reaches_child, false) THEN
        RAISE EXCEPTION
            'authorization_edge: % is already an authorization ancestor of %',
            NEW.child_id, NEW.parent_id
            USING ERRCODE = 'check_violation',
                  CONSTRAINT = 'authorization_edge_acyclic';
    END IF;

    -- ── Invariant 3 · path depth ────────────────────────────────────
    --
    -- The new edge can lengthen a path in both directions at once, so the
    -- bound is over the whole path THROUGH it, not over the ancestor chain
    -- alone. Today `POST /t/{t}/units` only ever creates a childless unit,
    -- making down_depth 0 — but an edge between two existing units does not,
    -- and this trigger is the invariant for every writer, not for that one
    -- endpoint.
    WITH RECURSIVE down(node_type, node_id, depth) AS (
        SELECT NEW.child_type, NEW.child_id, 0
        UNION ALL
        SELECT e.child_type, e.child_id, down.depth + 1
          FROM authorization_edge e
          JOIN down ON e.parent_type = down.node_type AND e.parent_id = down.node_id
         WHERE down.depth <= max_depth
    )
    SELECT max(depth) INTO down_depth FROM down;

    IF coalesce(down_depth, 0) + up_depth > max_depth THEN
        RAISE EXCEPTION
            'authorization_edge: path through this edge is % deep, limit is %',
            coalesce(down_depth, 0) + up_depth, max_depth
            USING ERRCODE = 'check_violation',
                  CONSTRAINT = 'authorization_edge_max_depth';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- BEFORE, so the row never reaches the table: A2.2 says detection runs
-- before commit, and 05.1.3 says not by a background job. UPDATE is covered
-- as well as INSERT — moving an edge's endpoints closes a cycle exactly as
-- inserting one does, and nothing in the schema makes the columns immutable.
CREATE TRIGGER authorization_edge_invariants
    BEFORE INSERT OR UPDATE ON authorization_edge
    FOR EACH ROW EXECUTE FUNCTION authorization_edge_invariants();
