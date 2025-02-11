package bot_lambda

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Setenv("AWS_XRAY_SDK_DISABLED", "true")

	m.Run()
}
