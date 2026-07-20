package config_test

import "os"

func init() {
	os.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
}
