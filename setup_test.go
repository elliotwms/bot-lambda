package bot_lambda

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("AWS_XRAY_SDK_DISABLED", "true")

	m.Run()
}
