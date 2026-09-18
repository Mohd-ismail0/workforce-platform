package api

import (
	"os"
	"testing"

	"workforce.local/platform/internal/testdb"
)

func TestMain(m *testing.M) {
	testdb.Package = "api"
	os.Exit(testdb.Main(m))
}