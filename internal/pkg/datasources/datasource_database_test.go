package datasources

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewModelDatabaseLoader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		datasourceOpts map[string]string
		options        Options
		secrets        Secrets
		expectError    bool
	}{
		{
			name: "valid database options",
			datasourceOpts: map[string]string{
				"username": "testuser",
				"password": "testpass",
				"host":     "localhost",
				"port":     "3306",
				"dbname":   "testdb",
				"tables":   "table1,table2",
				"charset":  "utf8",
			},
			options: Options{
				Root: "/tmp/test",
				URI:  "database://localhost:3306",
			},
			secrets:     Secrets{Username: "secret_user", Password: "secret_pass"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			loader, err := NewModelDatabaseLoader(tt.datasourceOpts, tt.options, tt.secrets)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, loader)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, loader)

				// Check that secrets are properly set
				assert.Equal(t, tt.secrets.Username, loader.modelDatabaseOptions.Username)
				assert.Equal(t, tt.secrets.Password, loader.modelDatabaseOptions.Password)

				// Check that options are properly set
				assert.Equal(t, tt.options, loader.Options)
			}
		})
	}
}

func TestModelDatabaseLoader_convertDatabaseOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          map[string]string
		expectError    bool
		expectedTables []string
	}{
		{
			name: "valid options with multiple tables",
			input: map[string]string{
				"username": "testuser",
				"password": "testpass",
				"host":     "localhost",
				"port":     "3306",
				"dbName":   "testdb",
				"tables":   "users,orders,products",
				"charset":  "utf8",
			},
			expectError:    false,
			expectedTables: []string{"users", "orders", "products"},
		},
		{
			name: "single table",
			input: map[string]string{
				"username": "testuser",
				"password": "testpass",
				"host":     "localhost",
				"port":     "3306",
				"dbName":   "testdb",
				"tables":   "users",
				"charset":  "utf8",
			},
			expectError:    false,
			expectedTables: []string{"users"},
		},
		{
			name: "no tables specified",
			input: map[string]string{
				"username": "testuser",
				"password": "testpass",
				"host":     "localhost",
				"port":     "3306",
				"dbName":   "testdb",
				"charset":  "utf8",
			},
			expectError:    true,
			expectedTables: []string(nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			loader := &ModelDatabaseLoader{}
			result, err := loader.convertDatabaseOptions(tt.input)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedTables, result.Tables)

				// Check other fields are properly mapped
				assert.Equal(t, tt.input["username"], result.Username)
				assert.Equal(t, tt.input["password"], result.Password)
				assert.Equal(t, tt.input["host"], result.Host)
				assert.Equal(t, tt.input["port"], result.Port)
				assert.Equal(t, tt.input["dbName"], result.Dbname)
				assert.Equal(t, tt.input["charset"], result.Charset)
			}
		})
	}
}

func Test_formatTSVtoCSV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single line TSV",
			input:    "col1\tcol2\tcol3",
			expected: "col1,col2,col3",
		},
		{
			name:     "multiple lines TSV",
			input:    "col1\tcol2\tcol3\nval1\tval2\tval3",
			expected: "col1,col2,col3\nval1,val2,val3",
		},
		{
			name:     "TSV with special characters",
			input:    "name\tage\tcity\nJohn Doe\t25\tNew York\nJane\t30\tLos Angeles",
			expected: "name,age,city\nJohn Doe,25,New York\nJane,30,Los Angeles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatTSVtoCSV(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Mock functions for testing runMySQL and getTotalRows that depend on external MySQL
func Test_runMySQL_Mock(t *testing.T) {
	t.Parallel()

	// Since runMySQL requires an actual MySQL connection, we'll test the error case
	// by using a non-existent host/port combination

	_, err := runMySQL("nonexistent-host", "12345", "user", "pass", "db", "SELECT 1", false)
	assert.Error(t, err)
}

func Test_getTotalRows_Mock(t *testing.T) {
	t.Parallel()

	// Since getTotalRows requires an actual MySQL connection, we'll test the error case
	_, err := getTotalRows("nonexistent-host", "12345", "user", "pass", "db", "table")
	assert.Error(t, err)
}

// Test helper function formatTSVtoCSV with more edge cases
func Test_formatTSVtoCSV_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "TSV with empty values",
			input:    "col1\tcol2\tcol3\nval1\t\tval3",
			expected: "col1,col2,col3\nval1,,val3",
		},
		{
			name:     "TSV with tabs in values",
			input:    "col1\tcol2\tcol3\nval1\tval\t2\tval3",
			expected: "col1,col2,col3\nval1,val,2,val3",
		},
		{
			name:     "TSV with newlines in values",
			input:    "col1\tcol2\tcol3\nval1\tval\n2\tval3",
			expected: "col1,col2,col3\nval1,val\n2,val3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatTSVtoCSV(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
