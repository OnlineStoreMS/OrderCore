package router

import (
	"ordercore/admin"
	adminmw "ordercore/admin/middleware"
	"ordercore/internal/config"
	"ordercore/internal/integration/storecore"
	"ordercore/internal/integration/storesync"
	"ordercore/internal/integration/supplycore"
	jwtmgr "ordercore/internal/pkg/jwt"
	"ordercore/internal/repo"
	"ordercore/internal/scheduler"
	"ordercore/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Setup(db *gorm.DB, cfg *config.Config) (*gin.Engine, *scheduler.SyncScheduler) {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), corsMiddleware(cfg))

	repos := repo.New(db)
	ssClient := storesync.NewClient(cfg.Integrations.StoreSyncAgentAPIURL)
	scClient := storecore.NewClient(cfg.Integrations.StoreCoreAPIURL)
	supplyClient := supplycore.NewClient(cfg.Integrations.SupplyCoreAPIURL)
	orderSvc := service.NewOrderService(repos, ssClient, scClient)
	jwtMgr := jwtmgr.NewManager(cfg.Auth.JWTSecret)
	settingsSvc := service.NewSettingsService(repos, orderSvc, jwtMgr)
	h := admin.NewHandlers(orderSvc, supplyClient)
	sh := admin.NewSettingsHandlers(settingsSvc)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "ordercore"})
	})

	v1 := r.Group("/api/v1")
	adminGroup := v1.Group("/admin")
	adminGroup.Use(adminmw.AdminAuth(&cfg.Auth, jwtMgr))
	admin.RegisterRoutes(adminGroup, h, sh)

	internalGroup := v1.Group("/internal")
	admin.RegisterInternalRoutes(internalGroup, h)

	sched := scheduler.NewSyncScheduler(settingsSvc)

	// 分配成功后异步推送给供应商（含 SKU 自动分配）
	orderSvc.SetOnAllocated(func(tenantID, orderID uint64) {
		settingsSvc.PushAllocatedAsync(tenantID, orderID)
	})

	return r, sched
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
