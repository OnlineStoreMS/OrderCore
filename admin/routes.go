package admin

import "github.com/gin-gonic/gin"

func RegisterRoutes(g *gin.RouterGroup, h *Handlers) {
	g.GET("/dashboard", h.Dashboard)

	g.GET("/orders", h.ListOrders)
	g.POST("/orders/manual", h.CreateManual)
	g.POST("/orders/ingest", h.Ingest)
	g.GET("/orders/:id", h.GetOrder)
	g.POST("/orders/:id/allocate", h.Allocate)
	g.POST("/orders/:id/ship", h.Ship)

	g.POST("/sync/kdzs", h.SyncKDZS)
	g.POST("/sync/store", h.SyncStore)

	g.GET("/kdzs/factories", h.ListFactories)

	g.GET("/supplier-bindings", h.ListBindings)
	g.POST("/supplier-bindings", h.CreateBinding)
	g.PUT("/supplier-bindings/:id", h.UpdateBinding)
	g.DELETE("/supplier-bindings/:id", h.DeleteBinding)
}

func RegisterInternalRoutes(g *gin.RouterGroup, h *Handlers) {
	g.POST("/orders/ingest", h.InternalIngest)
}
