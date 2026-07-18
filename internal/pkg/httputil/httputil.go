package httputil

import (
	"errors"
	"net/http"
	"strconv"

	"ordercore/internal/pkg/response"
	"ordercore/internal/service"

	"github.com/gin-gonic/gin"
)

func ParseID(c *gin.Context) (uint64, error) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func ParsePage(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	return page, pageSize
}

func HandleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		response.Fail(c, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrDuplicateCode):
		response.Fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrInsufficientStock), errors.Is(err, service.ErrHasMovements):
		response.Fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrInvalidStatus), errors.Is(err, service.ErrImmutable), errors.Is(err, service.ErrBadRequest):
		response.Fail(c, http.StatusBadRequest, err.Error())
	default:
		// wrap insufficient stock with fmt.Errorf
		if errors.Is(err, service.ErrInsufficientStock) {
			response.Fail(c, http.StatusConflict, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, err.Error())
	}
}
