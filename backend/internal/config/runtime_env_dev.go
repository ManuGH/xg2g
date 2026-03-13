//go:build dev

package config

func init() {
	runtimeEnvKeys = append(runtimeEnvKeys,
		"XG2G_UI_DEV_PROXY_URL",
		"XG2G_UI_DEV_DIR",
	)
}
