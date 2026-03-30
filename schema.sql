-- Tabla para datos del REM (Relevamiento de Expectativas de Mercado) del BCRA
-- Hoja: "Base de Datos Completa"

CREATE TABLE IF NOT EXISTS rem_data (
    id              SERIAL PRIMARY KEY,
    fecha_pronostico DATE        NOT NULL,   -- Fecha del relevamiento
    variable        TEXT        NOT NULL,   -- Nombre de la variable (ej: "Precios minoristas (IPC nivel general; INDEC)")
    referencia      TEXT        NOT NULL,   -- Unidad/referencia (ej: "var. % mensual", "$/USD", "TNA; %")
    periodo         TEXT        NOT NULL,   -- Período pronosticado (ej: "2026-03-01", "Próx. 12 meses", "Trim. IV-25", "2026")
    mediana         DOUBLE PRECISION,
    promedio        DOUBLE PRECISION,
    desvio          DOUBLE PRECISION,
    maximo          DOUBLE PRECISION,
    minimo          DOUBLE PRECISION,
    percentil_90    DOUBLE PRECISION,
    percentil_75    DOUBLE PRECISION,
    percentil_25    DOUBLE PRECISION,
    percentil_10    DOUBLE PRECISION,
    cantidad_participantes INTEGER,
    ingested_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Índices para consultas típicas
CREATE INDEX IF NOT EXISTS idx_rem_fecha ON rem_data (fecha_pronostico);
CREATE INDEX IF NOT EXISTS idx_rem_variable ON rem_data (variable);
CREATE INDEX IF NOT EXISTS idx_rem_fecha_variable ON rem_data (fecha_pronostico, variable);

-- Unique constraint para evitar duplicados en re-ingestas
CREATE UNIQUE INDEX IF NOT EXISTS idx_rem_unique
    ON rem_data (fecha_pronostico, variable, referencia, periodo);
