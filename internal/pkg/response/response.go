package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: 200, Message: "success", Data: data})
}

func Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, Body{Code: 200, Message: "success", Data: data})
}

func Fail(c *gin.Context, httpStatus int, message string) {
	c.JSON(httpStatus, Body{Code: httpStatus, Message: message})
}

func PageResult[T any](list []T, total int64, page, pageSize int) gin.H {
	return gin.H{
		"list":     list,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	}
}
