package router

import (
	"ordercore/admin"
	adminmw "ordercore/admin/middleware"
	"ordercore/internal/config"
	"ordercore/internal/integration/storecore"
	"ordercore/internal/integration/storesync"
	jwtmgr "ordercore/internal/pkg/jwt"
	"ordercore/internal/repo"
	"ordercore/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Setup(db *gorm.DB, cfg *config.Config) *gin.Engine {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), corsMiddleware(cfg))

	repos := repo.New(db)
	ssClient := storesync.NewClient(cfg.Integrations.StoreSyncAgentAPIURL)
	scClient := storecore.NewClient(cfg.Integrations.StoreCoreAPIURL)
	orderSvc := service.NewOrderService(repos, ssClient, scClient)
	h := admin.NewHandlers(orderSvc)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "ordercore"})
	})

	v1 := r.Group("/api/v1")
	adminGroup := v1.Group("/admin")
	jwtMgr := jwtmgr.NewManager(cfg.Auth.JWTSecret)
	adminGroup.Use(adminmw.AdminAuth(&cfg.Auth, jwtMgr))
	admin.RegisterRoutes(adminGroup, h)

	internalGroup := v1.Group("/internal")
	admin.RegisterInternalRoutes(internalGroup, h)

	return r
}

func corsMiddleware(cfg *config.Config) gin.HandlerFunc {
	origins := cfg.CORS.AllowOrigins
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowed := origin == ""
		for _, o := range origins {
			if o == origin || o == "*" {
				allowed = true
				break
			}
		}
		if allowed && origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
