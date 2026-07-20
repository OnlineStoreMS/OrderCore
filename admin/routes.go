package admin

import "github.com/gin-gonic/gin"

func RegisterRoutes(g *gin.RouterGroup, h *Handlers, sh *SettingsHandlers) {
	g.GET("/dashboard", h.Dashboard)

	g.GET("/orders", h.ListOrders)
	g.POST("/orders/manual", h.CreateManual)
	g.POST("/orders/ingest", h.Ingest)
	g.GET("/orders/:id", h.GetOrder)
	g.POST("/orders/:id/allocate", h.Allocate)
	g.POST("/orders/:id/ship", h.Ship)
	g.POST("/orders/:id/push", sh.PushOrder)

	g.POST("/sync/kdzs", h.SyncKDZS)
	g.POST("/sync/store", h.SyncStore)

	g.GET("/kdzs/factories", h.ListFactories)
	g.GET("/suppliers", h.ListSuppliers)

	g.GET("/supplier-bindings", h.ListBindings)
	g.POST("/supplier-bindings", h.CreateBinding)
	g.PUT("/supplier-bindings/:id", h.UpdateBinding)
	g.DELETE("/supplier-bindings/:id", h.DeleteBinding)

	g.GET("/sync-jobs", sh.ListSyncJobs)
	g.PUT("/sync-jobs/:id", sh.UpdateSyncJob)
	g.POST("/sync-jobs/:id/run", sh.RunSyncJob)

	g.GET("/notification-channels", sh.ListChannels)
	g.POST("/notification-channels", sh.CreateChannel)
	g.PUT("/notification-channels/:id", sh.UpdateChannel)
	g.DELETE("/notification-channels/:id", sh.DeleteChannel)
	g.POST("/notification-channels/:id/test", sh.TestChannel)

	g.GET("/push-rules", sh.ListPushRules)
	g.POST("/push-rules", sh.CreatePushRule)
	g.PUT("/push-rules/:id", sh.UpdatePushRule)
	g.DELETE("/push-rules/:id", sh.DeletePushRule)
	g.GET("/push-logs", sh.ListPushLogs)
}

func RegisterInternalRoutes(g *gin.RouterGroup, h *Handlers) {
	g.POST("/orders/ingest", h.InternalIngest)
}
