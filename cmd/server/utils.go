package server

import "github.com/gin-gonic/gin"

// GetEnvGinMode keeps gin verbose on developer machines and quiet elsewhere.
func GetEnvGinMode(env string) string {
	if env == "local" || env == "dev" {
		return gin.DebugMode
	}

	return gin.ReleaseMode
}
