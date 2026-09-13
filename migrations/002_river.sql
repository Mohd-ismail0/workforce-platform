-- River owns river_job and all supporting tables. Preserve the former local
-- queue only when it is empty; never reinterpret or destroy its rows.
DO $$
BEGIN
  IF to_regclass('public.river_migration') IS NOT NULL THEN
    RETURN; -- Official River schema already installed; never reinterpret its jobs.
  END IF;
  IF to_regclass('public.river_job') IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM public.river_job)
     AND to_regclass('public.legacy_workforce_job') IS NULL THEN
    ALTER TABLE public.river_job RENAME TO legacy_workforce_job;
  ELSIF to_regclass('public.river_job') IS NOT NULL
     AND EXISTS (SELECT 1 FROM public.river_job) THEN
    RAISE EXCEPTION 'legacy river_job contains rows; refusing destructive replacement';
  END IF;
END $$;