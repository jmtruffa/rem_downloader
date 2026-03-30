# REM Data Ingestor

Ingestador en Go para el archivo histórico del REM (Relevamiento de Expectativas de Mercado) del BCRA.

Descarga el XLSX desde el BCRA, parsea la hoja "Base de Datos Completa" e inserta en PostgreSQL.

## Setup

```bash
# 1. Crear la tabla
psql -h $POSTGRES_HOST -U $POSTGRES_USER -d $POSTGRES_DB -f schema.sql

# 2. Compilar
go mod tidy
go build -o rem_downloader .
```

## Uso

```bash
# Variables de entorno requeridas
export POSTGRES_USER=...
export POSTGRES_PASSWORD=...
export POSTGRES_HOST=...
export POSTGRES_PORT=5432
export POSTGRES_DB=...

# Carga inicial completa (descarga del BCRA + truncate + COPY bulk)
./rem_downloader -truncate

# Re-ingesta mensual (descarga del BCRA + upsert, no duplica)
./rem_downloader -upsert

# Desde archivo local
./rem_downloader -file ./historico-relevamiento-expectativas-mercado.xlsx -upsert
```

## Modos

| Flag | Método | Velocidad | Uso |
|------|--------|-----------|-----|
| (default) | COPY | Rápido | Append (falla si hay duplicados) |
| `-truncate` | COPY | Rápido | Carga limpia completa |
| `-upsert` | INSERT ON CONFLICT | Más lento | Re-ingesta segura, actualiza existentes |

## Estructura de la tabla

| Columna | Tipo | Descripción |
|---------|------|-------------|
| fecha_pronostico | DATE | Fecha del relevamiento |
| variable | TEXT | Variable (IPC, TC, PIB, etc.) |
| referencia | TEXT | Unidad (var. % mensual, $/USD, TNA; %, etc.) |
| periodo | TEXT | Período pronosticado (fecha, "Próx. 12 meses", "Trim. IV-25", año) |
| mediana | FLOAT | Mediana de pronósticos |
| promedio | FLOAT | Promedio |
| desvio | FLOAT | Desvío estándar |
| maximo | FLOAT | Máximo |
| minimo | FLOAT | Mínimo |
| percentil_90 | FLOAT | Percentil 90 |
| percentil_75 | FLOAT | Percentil 75 |
| percentil_25 | FLOAT | Percentil 25 |
| percentil_10 | FLOAT | Percentil 10 |
| cantidad_participantes | INT | Cantidad de participantes |

## Cron (opcional)

Para actualización mensual automática (el BCRA publica entre el 1 y el 15 de cada mes):

```bash
# Cada día 10 a las 10:00 AM
0 10 10 * * cd /path/to/rem_downloader && ./rem_downloader -upsert >> /var/log/rem_downloader.log 2>&1
```

## Fuente de datos

URL fija del BCRA:
```
https://www.bcra.gob.ar/archivos/Pdfs/PublicacionesEstadisticas/informes/historico-relevamiento-expectativas-mercado.xlsx
```

~7300+ filas, 17 variables, desde junio 2016 hasta el último relevamiento.
