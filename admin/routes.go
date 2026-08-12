package admin

import "github.com/gin-gonic/gin"

func RegisterRoutes(g *gin.RouterGroup, h *Handlers, sh *SettingsHandlers) {
	g.GET("/dashboard", h.Dashboard)

	g.GET("/orders", h.ListOrders)
	g.POST("/orders/manual", h.CreateManual)
	g.POST("/orders/manual/batch", h.CreateManualBatch)
	g.POST("/orders/manual/parse-address", h.ParseManualAddress)
	g.GET("/orders/manual/products/pim", h.SearchManualPIMProducts)
	g.GET("/orders/manual/products/shop", h.SearchManualShopProducts)
	g.GET("/orders/manual/customers", h.LookupManualCustomer)
	g.GET("/orders/manual/customers/:id/addresses", h.ListManualCustomerAddresses)
	g.GET("/orders/manual/recipients", h.SearchManualRecipients)
	g.POST("/orders/ingest", h.Ingest)
	g.POST("/orders/batch-dropship", h.BatchDropship)
	g.POST("/orders/relink-purchase-order", h.RelinkPurchaseOrder)
	g.POST("/orders/unlink-dropship-po", h.UnlinkDropshipPO)
	g.POST("/orders/decrypt", h.DecryptOrders)
	g.GET("/orders/:id", h.GetOrder)
	g.DELETE("/orders/:id", h.DeleteManualOrder)
	g.POST("/orders/:id/allocate", h.Allocate)
	g.POST("/orders/:id/revoke-allocate", h.RevokeAllocate)
	g.PUT("/orders/:id/remarks", h.UpdateRemarks)
	g.PUT("/orders/:id/payment", h.UpdatePayment)
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

	g.GET("/alloc-settings", h.GetAllocSettings)
	g.PUT("/alloc-settings", h.UpdateAllocSettings)
	g.GET("/sku-supplier-rules", h.ListSkuSupplierRules)
	g.POST("/sku-supplier-rules", h.CreateSkuSupplierRule)
	g.PUT("/sku-supplier-rules/:id", h.UpdateSkuSupplierRule)
	g.DELETE("/sku-supplier-rules/:id", h.DeleteSkuSupplierRule)

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

	g.GET("/manual-order-sources", sh.ListManualOrderSources)
	g.POST("/manual-order-sources", sh.CreateManualOrderSource)
	g.PUT("/manual-order-sources/:id", sh.UpdateManualOrderSource)
	g.DELETE("/manual-order-sources/:id", sh.DeleteManualOrderSource)
}

func RegisterInternalRoutes(g *gin.RouterGroup, h *Handlers) {
	g.POST("/orders/ingest", h.InternalIngest)
}
