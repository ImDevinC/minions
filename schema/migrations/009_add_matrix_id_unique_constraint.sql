-- Migration: 009_add_matrix_id_unique_constraint.sql
-- Adds unique constraint on matrix_id for ON CONFLICT upsert support

-- Add unique constraint for matrix_id (allows NULL, only unique among non-NULL values)
-- Required for GetOrCreateByMatrixID which uses ON CONFLICT (matrix_id)
ALTER TABLE users ADD CONSTRAINT users_matrix_id_unique UNIQUE (matrix_id);
