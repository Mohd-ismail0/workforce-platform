-- River owns river_job and all supporting tables. Preserve the former local
-- queue only when it is empty; never reinterpret or destroy its rows.
--
-- PL/pgSQL does NOT short-circuit an `a AND b` IF condition: PostgreSQL
-- evaluates both operands, so `SELECT 1 FROM public.river_job` runs even when
-- to_regclass('public.river_job') is NULL. On a FRESH database river_job does
-- not exist yet (River's own tables are created by rivermigrate AFTER these
-- migrations), so that EXISTS subquery raised 42P01 and aborted the whole
-- deployment. Nest the checks so the EXISTS only runs when river_job exists.
DO $$
BEGIN
  IF to_regclass('public.river_migration') IS NOT NULL THEN
    RETURN; -- Official River schema already installed; never reinterpret its jobs.
  END IF;
  IF to_regclass('public.river_job') IS NOT NULL THEN
    IF NOT EXISTS (SELECT 1 FROM public.river_job)
       AND to_regclass('public.legacy_workforce_job') IS NULL THEN
      ALTER TABLE public.river_job RENAME TO legacy_workforce_job;
    ELSIF EXISTS (SELECT 1 FROM public.river_job) THEN
      RAISE EXCEPTION 'legacy river_job contains rows; refusing destructive replacement';
    END IF;
  END IF;
END $$;