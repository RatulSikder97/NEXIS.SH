DROP INDEX IF EXISTS code_embeddings_embedding_ivfflat;
DROP TABLE IF EXISTS code_embeddings;
-- Leave the `vector` extension installed; dropping it would break any other
-- consumer in the future.
