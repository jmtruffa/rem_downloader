package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	_ "github.com/lib/pq"
)

// ---------------------- CONFIG ----------------------

var (
	dbUser     = os.Getenv("POSTGRES_USER")
	dbPassword = os.Getenv("POSTGRES_PASSWORD")
	dbHost     = os.Getenv("POSTGRES_HOST")
	dbPort     = os.Getenv("POSTGRES_PORT")
	dbName     = os.Getenv("POSTGRES_DB")
)

const (
	remURL    = "https://www.bcra.gob.ar/archivos/Pdfs/PublicacionesEstadisticas/informes/historico-relevamiento-expectativas-mercado.xlsx"
	sheetName = "Base de Datos Completa"
	headerRow = 2 // 1-indexed: row 2 has the headers
	dataStart = 3 // 1-indexed: data starts at row 3
)

func databaseURL() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		dbUser, dbPassword, dbHost, dbPort, dbName)
}

// ---------------------- DOWNLOAD ----------------------

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("descarga fallida: status %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	fmt.Printf("Descargado: %s (%.2f MB)\n", dest, float64(written)/1024/1024)
	return nil
}

// ---------------------- PARSE HELPERS ----------------------

// cellToString converts an excelize cell value to string.
// Handles the mixed types in "Período" column: datetime, int, string.
func cellToString(f *excelize.File, sheet, axis string) string {
	val, _ := f.GetCellValue(sheet, axis)
	return strings.TrimSpace(val)
}

// cellToFloat converts an excelize cell to *float64 (nil if empty/error).
func cellToFloat(f *excelize.File, sheet, axis string) *float64 {
	val, _ := f.GetCellValue(sheet, axis)
	val = strings.TrimSpace(val)
	if val == "" || strings.ToLower(val) == "n/a" || val == "--" {
		return nil
	}
	// Remove thousand separators if any
	val = strings.ReplaceAll(val, ",", "")
	v, err := strconv.ParseFloat(val, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

// cellToInt converts an excelize cell to *int (nil if empty/error).
func cellToInt(f *excelize.File, sheet, axis string) *int {
	val, _ := f.GetCellValue(sheet, axis)
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	// Handle floats like "39.0" → 39
	if strings.Contains(val, ".") {
		fv, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil
		}
		iv := int(fv)
		return &iv
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return nil
	}
	return &v
}

// parseFechaPronostico parses the "Fecha de pronóstico" column.
// excelize returns dates as strings like "2016-06-30" or serial numbers.
func parseFechaPronostico(f *excelize.File, sheet, axis string) (time.Time, error) {
	raw, _ := f.GetCellValue(sheet, axis)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("fecha vacía")
	}

	// Try common date formats
	formats := []string{
		"2006-01-02",
		"01-02-06",
		"1/2/2006",
		"02/01/2006",
		"2006-01-02 15:04:05",
		// excelize typically returns ISO format
	}
	for _, layout := range formats {
		t, err := time.Parse(layout, raw)
		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("no se pudo parsear fecha: %q", raw)
}

// colName returns the Excel column letter for a 1-indexed column number.
func colName(col int) string {
	name, _ := excelize.ColumnNumberToName(col)
	return name
}

// axis returns the cell reference like "A3", "B3", etc.
func axis(col, row int) string {
	return fmt.Sprintf("%s%d", colName(col), row)
}

// ---------------------- PARSE & INSERT ----------------------

func parseAndInsert(filename string, truncateFirst bool) error {
	fmt.Println("Abriendo archivo XLSX...")
	f, err := excelize.OpenFile(filename)
	if err != nil {
		return fmt.Errorf("error abriendo XLSX: %v", err)
	}
	defer f.Close()

	// Verify sheet exists
	sheets := f.GetSheetList()
	fmt.Printf("Hojas encontradas: %v\n", sheets)

	found := false
	for _, s := range sheets {
		if s == sheetName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no se encontró la hoja %q", sheetName)
	}

	// Get total rows
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("error leyendo filas: %v", err)
	}
	totalRows := len(rows)
	fmt.Printf("Total filas: %d (datos: %d)\n", totalRows, totalRows-dataStart+1)

	// Connect to DB
	db, err := sql.Open("postgres", databaseURL())
	if err != nil {
		return fmt.Errorf("error conectando a DB: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("error ping DB: %v", err)
	}
	fmt.Println("Conectado a PostgreSQL.")

	// Optionally truncate
	if truncateFirst {
		fmt.Println("Truncando tabla rem_data...")
		if _, err := db.Exec("TRUNCATE TABLE rem_data RESTART IDENTITY"); err != nil {
			return fmt.Errorf("error truncando: %v", err)
		}
	}

	// Begin transaction with COPY
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`COPY rem_data
		(fecha_pronostico, variable, referencia, periodo,
		 mediana, promedio, desvio, maximo, minimo,
		 percentil_90, percentil_75, percentil_25, percentil_10,
		 cantidad_participantes)
		FROM STDIN`)
	if err != nil {
		return fmt.Errorf("error preparando COPY: %v", err)
	}

	inserted := 0
	skipped := 0

	for row := dataStart; row <= totalRows; row++ {
		// Column mapping (1-indexed):
		// A(1)=Fecha pronóstico, B(2)=Variable, C(3)=Referencia, D(4)=Período
		// E(5)=Mediana, F(6)=Promedio, G(7)=Desvío, H(8)=Máximo, I(9)=Mínimo
		// J(10)=Percentil 90, K(11)=Percentil 75, L(12)=Percentil 25, M(13)=Percentil 10
		// N(14)=Cantidad participantes

		// Parse fecha de pronóstico
		fechaPron, err := parseFechaPronostico(f, sheetName, axis(1, row))
		if err != nil {
			// Empty row or unparseable date = skip
			skipped++
			continue
		}

		variable := cellToString(f, sheetName, axis(2, row))
		referencia := cellToString(f, sheetName, axis(3, row))
		periodo := cellToString(f, sheetName, axis(4, row))

		if variable == "" {
			skipped++
			continue
		}

		mediana := cellToFloat(f, sheetName, axis(5, row))
		promedio := cellToFloat(f, sheetName, axis(6, row))
		desvio := cellToFloat(f, sheetName, axis(7, row))
		maximo := cellToFloat(f, sheetName, axis(8, row))
		minimo := cellToFloat(f, sheetName, axis(9, row))
		p90 := cellToFloat(f, sheetName, axis(10, row))
		p75 := cellToFloat(f, sheetName, axis(11, row))
		p25 := cellToFloat(f, sheetName, axis(12, row))
		p10 := cellToFloat(f, sheetName, axis(13, row))
		cantPart := cellToInt(f, sheetName, axis(14, row))

		_, err = stmt.Exec(
			fechaPron.Format("2006-01-02"),
			variable,
			referencia,
			periodo,
			mediana,
			promedio,
			desvio,
			maximo,
			minimo,
			p90,
			p75,
			p25,
			p10,
			cantPart,
		)
		if err != nil {
			return fmt.Errorf("error en COPY fila %d: %v", row, err)
		}

		inserted++

		if inserted%1000 == 0 {
			fmt.Printf("  Procesadas %d filas...\n", inserted)
		}
	}

	// Close COPY stream
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("error cerrando COPY: %v", err)
	}

	// Commit
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error en commit: %v", err)
	}

	fmt.Printf("\n✅ Insertadas: %d filas, Saltadas: %d filas\n", inserted, skipped)
	return nil
}

// ---------------------- UPSERT MODE ----------------------

func parseAndUpsert(filename string) error {
	fmt.Println("Abriendo archivo XLSX...")
	f, err := excelize.OpenFile(filename)
	if err != nil {
		return fmt.Errorf("error abriendo XLSX: %v", err)
	}
	defer f.Close()

	found := false
	for _, s := range f.GetSheetList() {
		if s == sheetName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no se encontró la hoja %q", sheetName)
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("error leyendo filas: %v", err)
	}
	totalRows := len(rows)
	fmt.Printf("Total filas: %d (datos: %d)\n", totalRows, totalRows-dataStart+1)

	db, err := sql.Open("postgres", databaseURL())
	if err != nil {
		return fmt.Errorf("error conectando a DB: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("error ping DB: %v", err)
	}
	fmt.Println("Conectado a PostgreSQL.")

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	upsertStmt, err := tx.Prepare(`
		INSERT INTO rem_data
			(fecha_pronostico, variable, referencia, periodo,
			 mediana, promedio, desvio, maximo, minimo,
			 percentil_90, percentil_75, percentil_25, percentil_10,
			 cantidad_participantes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (fecha_pronostico, variable, referencia, periodo)
		DO UPDATE SET
			mediana = EXCLUDED.mediana,
			promedio = EXCLUDED.promedio,
			desvio = EXCLUDED.desvio,
			maximo = EXCLUDED.maximo,
			minimo = EXCLUDED.minimo,
			percentil_90 = EXCLUDED.percentil_90,
			percentil_75 = EXCLUDED.percentil_75,
			percentil_25 = EXCLUDED.percentil_25,
			percentil_10 = EXCLUDED.percentil_10,
			cantidad_participantes = EXCLUDED.cantidad_participantes,
			ingested_at = NOW()
	`)
	if err != nil {
		return fmt.Errorf("error preparando upsert: %v", err)
	}
	defer upsertStmt.Close()

	inserted := 0
	skipped := 0

	for row := dataStart; row <= totalRows; row++ {
		fechaPron, err := parseFechaPronostico(f, sheetName, axis(1, row))
		if err != nil {
			skipped++
			continue
		}

		variable := cellToString(f, sheetName, axis(2, row))
		referencia := cellToString(f, sheetName, axis(3, row))
		periodo := cellToString(f, sheetName, axis(4, row))

		if variable == "" {
			skipped++
			continue
		}

		mediana := cellToFloat(f, sheetName, axis(5, row))
		promedio := cellToFloat(f, sheetName, axis(6, row))
		desvio := cellToFloat(f, sheetName, axis(7, row))
		maximo := cellToFloat(f, sheetName, axis(8, row))
		minimo := cellToFloat(f, sheetName, axis(9, row))
		p90 := cellToFloat(f, sheetName, axis(10, row))
		p75 := cellToFloat(f, sheetName, axis(11, row))
		p25 := cellToFloat(f, sheetName, axis(12, row))
		p10 := cellToFloat(f, sheetName, axis(13, row))
		cantPart := cellToInt(f, sheetName, axis(14, row))

		_, err = upsertStmt.Exec(
			fechaPron.Format("2006-01-02"),
			variable,
			referencia,
			periodo,
			mediana,
			promedio,
			desvio,
			maximo,
			minimo,
			p90,
			p75,
			p25,
			p10,
			cantPart,
		)
		if err != nil {
			return fmt.Errorf("error upsert fila %d: %v", row, err)
		}

		inserted++
		if inserted%1000 == 0 {
			fmt.Printf("  Procesadas %d filas...\n", inserted)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error en commit: %v", err)
	}

	fmt.Printf("\n✅ Upserted: %d filas, Saltadas: %d filas\n", inserted, skipped)
	return nil
}

// ---------------------- MAIN ----------------------

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
Uso: rem_data [opciones]

Descarga e ingesta el archivo histórico del REM (BCRA) en PostgreSQL.

Opciones:
  -file string
        Ruta a un archivo XLSX local. Si se omite, descarga del BCRA.
  -truncate
        Trunca la tabla antes de insertar (carga completa). Default: false.
  -upsert
        Usa INSERT ... ON CONFLICT (upsert) en vez de COPY. Más lento pero
        seguro para re-ingestas incrementales. Default: false.
  -h
        Muestra esta ayuda.

Variables de entorno requeridas:
  POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_HOST, POSTGRES_PORT, POSTGRES_DB

Ejemplos:
  # Carga inicial completa (descarga + truncate + COPY)
  rem_data -truncate

  # Re-ingesta incremental desde archivo local
  rem_data -file ./historico-rem.xlsx -upsert

  # Solo descargar y cargar (append)
  rem_data
`)
	}

	var (
		localFile string
		truncate  bool
		upsert    bool
	)

	flag.StringVar(&localFile, "file", "", "Ruta a archivo XLSX local (si se omite, descarga del BCRA)")
	flag.BoolVar(&truncate, "truncate", false, "Truncar tabla antes de insertar")
	flag.BoolVar(&upsert, "upsert", false, "Usar upsert (ON CONFLICT) en vez de COPY")
	flag.Parse()

	// Validate DB config
	if dbUser == "" || dbPassword == "" || dbHost == "" || dbPort == "" || dbName == "" {
		log.Fatal("Faltan variables de entorno: POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_HOST, POSTGRES_PORT, POSTGRES_DB")
	}

	// Determine file to process
	filename := localFile
	if filename == "" {
		filename = filepath.Join(os.TempDir(), "historico-rem-bcra.xlsx")
		fmt.Printf("Descargando REM histórico desde BCRA...\n")
		if err := downloadFile(remURL, filename); err != nil {
			log.Fatalf("Error descargando: %v", err)
		}
	} else {
		// Verify file exists
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			log.Fatalf("Archivo no encontrado: %s", filename)
		}
		fmt.Printf("Usando archivo local: %s\n", filename)
	}

	// Process
	start := time.Now()

	if upsert {
		fmt.Println("Modo: UPSERT (ON CONFLICT)")
		if err := parseAndUpsert(filename); err != nil {
			log.Fatalf("Error: %v", err)
		}
	} else {
		fmt.Println("Modo: COPY (bulk insert)")
		if err := parseAndInsert(filename, truncate); err != nil {
			log.Fatalf("Error: %v", err)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("⏱️  Tiempo total: %s\n", elapsed.Round(time.Millisecond))
}
