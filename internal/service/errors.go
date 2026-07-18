package service

import "errors"

var (
	ErrNotFound          = errors.New("记录不存在")
	ErrDuplicateCode     = errors.New("编码已存在")
	ErrInvalidStatus     = errors.New("当前状态不允许此操作")
	ErrImmutable         = errors.New("仅草稿状态可编辑")
	ErrBadRequest        = errors.New("请求参数无效")
	ErrInsufficientStock = errors.New("库存不足")
	ErrHasMovements      = errors.New("存在库存流水，不可删除")
)
