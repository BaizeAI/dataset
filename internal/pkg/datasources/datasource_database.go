package datasources

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BaizeAI/dataset/pkg/log"

	"github.com/sirupsen/logrus"
)

var _ Loader = &ModelDatabaseLoader{}

type ModelDatabaseLoader struct {
	Options Options

	modelDatabaseOptions ModelDatabaseLoaderOptions
}

func NewModelDatabaseLoader(datasourceOptions map[string]string, options Options, secrets Secrets) (*ModelDatabaseLoader, error) {
	res := new(ModelDatabaseLoader)
	parsedOpts, err := res.convertDatabaseOptions(datasourceOptions)
	if err != nil {
		return nil, err
	}

	res.Options = options
	res.modelDatabaseOptions = parsedOpts
	res.modelDatabaseOptions.Username = strings.TrimSpace(secrets.Username)
	res.modelDatabaseOptions.Password = strings.TrimSpace(secrets.Password)

	return res, nil
}

type ModelDatabaseLoaderOptions struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	Host     string   `json:"host"`
	Port     string   `json:"port"`
	Dbname   string   `json:"dbname"`
	Tables   []string `json:"-"`
	Charset  string   `json:"charset"`
}

func (d *ModelDatabaseLoader) convertDatabaseOptions(options map[string]string) (ModelDatabaseLoaderOptions, error) {
	var mdbOptions ModelDatabaseLoaderOptions
	rawTables := strings.TrimSpace(options["tables"])
	if rawTables == "" {
		return mdbOptions, fmt.Errorf("no table specified")
	}
	tables := strings.Split(rawTables, ",")
	if len(tables) == 0 {
		return mdbOptions, fmt.Errorf("no table specified")
	}
	jsonContent, err := json.Marshal(options)
	if err != nil {
		return ModelDatabaseLoaderOptions{}, err
	}
	err = json.Unmarshal(jsonContent, &mdbOptions)
	if err != nil {
		return ModelDatabaseLoaderOptions{}, err
	}
	mdbOptions.Tables = tables
	return mdbOptions, nil
}

func (d *ModelDatabaseLoader) Sync(fromURI string, toPath string) error {
	parsedURL, err := url.Parse(d.Options.URI)
	if err != nil {
		return err
	}
	if parsedURL.Scheme != "database" {
		return fmt.Errorf("invalid scheme %s, only database is supported", parsedURL.Scheme)
	}

	logger := log.WithFields(logrus.Fields{
		"fromURI":          fromURI,
		"type":             TypeDatabase,
		"toPath":           toPath,
		"workingDirectory": d.Options.Root,
	})
	for _, table := range d.modelDatabaseOptions.Tables {
		err := d.sync(logger, table)
		if err != nil {
			return err
		}
	}
	return nil
}
func (d *ModelDatabaseLoader) sync(logger *logrus.Entry, tableName string) error {
	option := d.modelDatabaseOptions
	batchSize := 10000
	dbHost := option.Host
	dbPort := option.Port
	dbUser := option.Username
	dbPass := option.Password
	dbName := option.Dbname
	outputFile := filepath.Join(d.Options.Root, fmt.Sprintf("%s.%s.%s", dbName, tableName, "csv"))
	// 1. Get total row count
	logger.Infof("fetching total row count for table: %s...", tableName)
	totalRows, err := getTotalRows(dbHost, dbPort, dbUser, dbPass, dbName, tableName)
	if err != nil {
		logger.Errorf("error getting total rows: %v", err)
		return err
	}
	logger.Infof("total rows: %d\n", totalRows)

	// 2. Open output file (Create or Truncate)
	f, err := os.Create(outputFile)
	if err != nil {
		logger.Errorf("error creating file: %v", err)
		return err
	}
	defer f.Close()

	// 3. Batch Export
	for offset := 0; offset < totalRows; offset += batchSize {
		currentDone := offset + batchSize
		if currentDone > totalRows {
			currentDone = totalRows
		}
		logger.Infof("export progress: %d / %d ...", currentDone, totalRows)
		query := fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d;", tableName, batchSize, offset)
		skipHeader := offset > 0
		// -N skips headers
		data, err := runMySQL(dbHost, dbPort, dbUser, dbPass, dbName, query, skipHeader)
		if err != nil {
			logger.Errorf("failed to get MySQL data at offset %d: %v", offset, err)
			return err
		}
		s := formatTSVtoCSV(data)
		if _, err := f.WriteString(s); err != nil {
			logger.Errorf("error when writing offset %d to csv: %v", offset, err)
			return err
		}
	}

	logger.Infof("export '%s.%s' to '%s' completed successfully!", dbName, tableName, outputFile)
	return nil
}

// runMySQL executes the mysql command and returns stdout as a string
func runMySQL(host, port, user, pass, db, query string, skipHeader bool) (string, error) {
	args := []string{
		"-h" + host,
		"-P" + port,
		"-u" + user,
		"-D" + db,
		"-e", query,
	}
	if skipHeader {
		args = append(args, "-N")
	}

	cmd := exec.Command("mysql", args...)

	// Use environment variable to hide password warning
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, stderr.String())
	}

	return out.String(), nil
}

// getTotalRows gets the count as an integer
func getTotalRows(host, port, user, pass, db, table string) (int, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s;", table)
	res, err := runMySQL(host, port, user, pass, db, query, true)
	if err != nil {
		return 0, err
	}

	count, err := strconv.Atoi(strings.TrimSpace(res))
	if err != nil {
		return 0, fmt.Errorf("failed to parse count: %v", err)
	}
	return count, nil
}

// formatTSVtoCSV converts Tab-Separated MySQL output to Comma-Separated
func formatTSVtoCSV(tsv string) string {
	if tsv == "" {
		return ""
	}
	reader := csv.NewReader(strings.NewReader(tsv))
	reader.Comma = '\t'
	records, err := reader.ReadAll()
	if err != nil {
		return ""
	}
	var sb strings.Builder
	writer := csv.NewWriter(&sb)
	writer.Comma = ',' // 默认就是逗号
	err = writer.WriteAll(records)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(sb.String())
}
