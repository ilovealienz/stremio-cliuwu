package download

import (
	"os"
	"testing"
)

// TestMain points every data folder at a temp dir, so no test can touch the
// real config, addons, history or downloads.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "download-test")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA"} {
		os.Setenv(v, dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
