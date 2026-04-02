-- Down Migration: 009_add_matrix_id_unique_constraint_down.sql
-- Removes unique constraint on matrix_id

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_matrix_id_unique;
